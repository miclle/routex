import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { getUsage, getUsageTeams } from './usage'
import { teamUsageFixture } from '@/views/usage/fixture'
import { defaultUsageFilters } from '@/views/usage/filter-state'
const original = client.defaults.adapter
let body: unknown, requests: InternalAxiosRequestConfig[]
beforeEach(() => {
  body = teamUsageFixture('tea_one')
  requests = []
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { config, data: body, status: 200, statusText: '', headers: new AxiosHeaders() }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
describe('Team aggregate usage transport', () => {
  it('uses only the exact resource route and preserves known decimal strings and unknown counters', async () => {
    const signal = new AbortController().signal
    const report = await getUsage({ teamId: 'tea_one' }, defaultUsageFilters, signal)
    expect(requests[0].url).toBe('/teams/tea_one/usage')
    expect(requests[0].signal).toBe(signal)
    expect(report.current.summary.tokens.total.value).toBeNull()
    expect(report.current.summary.amounts[0].amount).toBe('0.123456789012345678')
    expect(report.current.keys).toEqual([])
  })
  it.each([
    'key_id',
    'user_id',
    'project_id',
    'team_id',
    'provider_id',
    'provider_model_id',
    'connection_id',
  ])('rejects foreign scope filter %s before dispatch', async (field) => {
    await expect(
      getUsage({ teamId: 'tea_one' }, { ...defaultUsageFilters, [field]: 'id_other' }),
    ).rejects.toThrow()
    expect(requests).toHaveLength(0)
  })
  it.each([
    { teamId: 'tea_one', admin: true },
    { teamId: 'tea_one', projectId: 'prj_one' },
    { teamId: 'tea_../other' },
  ])('rejects ambiguous or unsafe scopes %j', async (scope) => {
    await expect(getUsage(scope, defaultUsageFilters)).rejects.toThrow()
    expect(requests).toHaveLength(0)
  })
  it.each(['crossTeam', 'Key', 'provider', 'member', 'malformed', 'unknownDimension', 'badCounts'])(
    'rejects an untrusted %s report',
    async (kind) => {
      const value = teamUsageFixture('tea_one')
      if (kind === 'crossTeam') value.team_id = 'tea_other'
      if (kind === 'Key')
        value.current.keys = [{ id: 'key_private', unknown: false, stats: value.current.summary }]
      if (kind === 'provider') value.current.providers = []
      if (kind === 'member') Object.assign(value, { user_id: 'usr_private' })
      if (kind === 'malformed') Object.assign(value, { current: { summary: {} } })
      if (kind === 'unknownDimension') value.available_dimensions = ['model', 'member']
      if (kind === 'badCounts') value.current.summary.tokens.total.known = 'invalid'
      body = value
      await expect(getUsage({ teamId: 'tea_one' }, defaultUsageFilters)).rejects.toThrow()
    },
  )
  it('retains platform provider dimensions and an explicit Team filter for authorized admin reports', async () => {
    await getUsage(
      { admin: true },
      { ...defaultUsageFilters, team_id: 'tea_one', provider_id: 'prv_one' },
    )
    expect(requests[0].url).toBe('/admin/usage')
    expect(requests[0].params.team_id).toBe('tea_one')
  })
  it('retains counters and money beyond JavaScript numeric precision', async () => {
    const value = teamUsageFixture('tea_one')
    value.current.summary.tokens.total.known = '900719925474099312345'
    value.current.summary.amounts[0].amount = '12345678901234567890.000000000000000001'
    body = value
    const report = await getUsage({ teamId: 'tea_one' }, defaultUsageFilters)
    expect(report.current.summary.tokens.total.known).toBe('900719925474099312345')
    expect(report.current.summary.amounts[0].amount).toBe('12345678901234567890.000000000000000001')
  })
  it('reads paginated own active Teams with no directory call or empty cursor', async () => {
    body = {
      items: [
        { id: 'tea_one', name: 'Research', status: 'active', members: [{ user_id: 'usr_hidden' }] },
      ],
      next_cursor: 'next',
    }
    await expect(getUsageTeams(null)).resolves.toEqual({
      items: [{ id: 'tea_one', name: 'Research' }],
      next_cursor: 'next',
    })
    expect(client.getUri(requests[0])).toBe('/api/v1/teams?status=active')
    body = { items: [], next_cursor: null }
    await getUsageTeams('next')
    expect(requests[1].params.cursor).toBe('next')
  })
  it.each([
    { items: [undefined], next_cursor: null },
    { items: [{ id: 'tea_one', name: 'Research', status: 'disabled' }], next_cursor: null },
    { items: [], next_cursor: 'repeat' },
  ])('rejects malformed own Team page %j', async (value) => {
    body = value
    await expect(getUsageTeams('repeat')).rejects.toThrow()
  })
})
