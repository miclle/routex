import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { getUsage, getUsageTeams, isPersonalUsageReport } from './usage'
import { teamUsageFixture, usageFixture } from '@/views/usage/fixture'
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

describe('recorded caller member-count extension', () => {
  function marked(team = false) {
    const report = team ? teamUsageFixture('tea_one') : usageFixture()
    report.member_count_basis = 'distinct_recorded_actors'
    report.current.models[0].members = { value: 2, known: 2, unknown_calls: 0 }
    return report
  }
  it.each([{}, { teamId: 'tea_one' }, { projectId: 'prj_one' }, { admin: true }])(
    'preserves current and previous exact counts without changing scope %j',
    async (scope) => {
      const report = marked(!!scope.teamId)
      report.previous = structuredClone(report.current)
      report.previous.models[0].members = { value: null, known: 1, unknown_calls: 1 }
      body = report
      expect(await getUsage(scope, defaultUsageFilters)).toBe(report)
      expect(requests).toHaveLength(1)
      expect(requests[0].url).toBe(
        scope.teamId
          ? '/teams/tea_one/usage'
          : scope.projectId
            ? '/projects/prj_one/usage'
            : scope.admin
              ? '/admin/usage'
              : '/usage',
      )
    },
  )
  it('retains legacy unmarked reports and refuses previous in the specialized Personal guard', async () => {
    body = usageFixture()
    expect(await getUsage({}, defaultUsageFilters)).toBe(body)
    const report = marked()
    // The monthly/Home guard still rejects unrelated dimensions and comparison periods.
    delete report.current.providers
    delete report.current.provider_models
    delete report.current.connections
    report.available_dimensions = ['model', 'key']
    expect(isPersonalUsageReport(report)).toBe(true)
    report.previous = structuredClone(report.current)
    expect(isPersonalUsageReport(report)).toBe(false)
    body = report
    expect(await getUsage({}, defaultUsageFilters)).toBe(report)
  })
  it.each([
    ['negative', { value: -1, known: -1, unknown_calls: 0 }],
    ['fractional', { value: 0.5, known: 0.5, unknown_calls: 0 }],
    ['string', { value: '1', known: 1, unknown_calls: 0 }],
    ['boolean', { value: true, known: 1, unknown_calls: 0 }],
    ['unsafe', { value: 9007199254740992, known: 9007199254740992, unknown_calls: 0 }],
    ['over requests', { value: 3, known: 3, unknown_calls: 0 }],
    ['sum over requests', { value: null, known: 2, unknown_calls: 1 }],
    ['unknown reported complete', { value: 1, known: 1, unknown_calls: 1 }],
    ['complete reported null', { value: null, known: 1, unknown_calls: 0 }],
    ['unknown negative', { value: null, known: 1, unknown_calls: -1 }],
    ['missing field', { value: 1, known: 1 }],
    ['identity disclosure', { value: 1, known: 1, unknown_calls: 0, user_ids: ['usr_private'] }],
  ])('rejects malformed %s counts in all scopes', async (_name, counts) => {
    for (const scope of [{}, { teamId: 'tea_one' }, { projectId: 'prj_one' }, { admin: true }]) {
      const report = marked(!!scope.teamId)
      Object.assign(report.current.models[0], { members: counts })
      body = report
      await expect(getUsage(scope, defaultUsageFilters)).rejects.toThrow(
        'Invalid usage member counts',
      )
    }
  })
  it.each(['current', 'previous'])(
    'requires every marked %s Model group to carry counts',
    async (period) => {
      const report = marked()
      report.previous = structuredClone(report.current)
      delete (period === 'current' ? report.current : report.previous).models[0].members
      body = report
      await expect(getUsage({}, defaultUsageFilters)).rejects.toThrow()
    },
  )
  it.each(['keys', 'providers', 'provider_models', 'connections', 'trend', 'summary'])(
    'rejects counts on non-Model %s',
    async (field) => {
      const report = marked()
      const period = report.current as unknown as Record<string, unknown>
      const target = field === 'summary' ? period[field] : (period[field] as unknown[])[0]
      Object.assign(target as object, { members: { value: 1, known: 1, unknown_calls: 0 } })
      body = report
      await expect(getUsage({ admin: true }, defaultUsageFilters)).rejects.toThrow()
    },
  )
  it.each([null, 'current_grants', true])('rejects invalid basis %j', async (basis) => {
    body = { ...marked(), member_count_basis: basis }
    await expect(getUsage({}, defaultUsageFilters)).rejects.toThrow()
  })
  it('rejects malformed counts even on an unmarked legacy report', async () => {
    const report = marked()
    delete report.member_count_basis
    Object.assign(report.current.models[0], { members: { value: 1, known: 2, unknown_calls: 0 } })
    body = report
    await expect(getUsage({}, defaultUsageFilters)).rejects.toThrow()
  })
})
