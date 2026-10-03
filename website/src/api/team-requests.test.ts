import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  compareTarget,
  createTeamRequest,
  decideTeamRequest,
  listTeamRequests,
  teamRequestContext,
  teamRequestDetail,
  validTarget,
} from './team-requests'
import { contextFixture, requestFixture } from '@/views/team-requests/fixture'
const originalAdapter = client.defaults.adapter
let value: unknown, requests: InternalAxiosRequestConfig[]
beforeEach(() => {
  requests = []
  value = null
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { config, data: value, status: 200, statusText: '', headers: new AxiosHeaders() }
  }
})
afterEach(() => {
  client.defaults.adapter = originalAdapter
})
const decision = {
  decision_id: '00000000-0000-4000-8000-000000000002',
  step_id: 'qst_owner',
  action: 'approve' as const,
  reason: '',
}
describe('Team quota request API', () => {
  it('preserves exact target strings and finite safe policy bounds', () => {
    expect(validTarget('9007199254740991', 'tokens')).toBe(true)
    expect(validTarget('9007199254740992', 'tokens')).toBe(false)
    expect(validTarget('1e3', 'tokens')).toBe(false)
    expect(validTarget('0001', 'tokens')).toBe(false)
    expect(validTarget('999999999999999999.999999999999999999', 'money')).toBe(true)
    expect(
      compareTarget(
        '123456789012345678.000000000000000002',
        '123456789012345678.000000000000000001',
        'money',
      ),
    ).toBe(1)
  })
  it.each([
    { team_id: 'tea_other' },
    { dimension: 'money' },
    { etag: 'weak' },
    { month_end: '2026-09-01T00:00:00Z' },
    { member_effective: 0 },
    { currency: 'USD' },
  ])('rejects malformed/cross-scope context %j', async (patch) => {
    value = { ...contextFixture(), ...patch }
    await expect(teamRequestContext('tea_one', 'tokens')).rejects.toThrow()
  })
  it('accepts explicit inherited/unbounded values without fabricating zero', async () => {
    value = {
      ...contextFixture(),
      member_stored: null,
      member_effective: null,
      eligible: false,
      blockers: ['member_unbounded'],
    }
    await expect(teamRequestContext('tea_one', 'tokens')).resolves.toMatchObject({
      member_effective: null,
    })
  })
  it('validates own history scope and every cursor/page before caching', async () => {
    value = { items: [requestFixture()], total: 1, next_cursor: null }
    await expect(
      listTeamRequests(false, 'my', 'usr_other', { status: '', dimension: '' }, null),
    ).rejects.toThrow()
    value = { items: [undefined], total: 1, next_cursor: null }
    await expect(
      listTeamRequests(false, 'my', 'usr_member', { status: '', dimension: '' }, null),
    ).rejects.toThrow()
    value = { items: [], total: 0, next_cursor: 'repeat' }
    await expect(
      listTeamRequests(false, 'my', 'usr_member', { status: '', dimension: '' }, 'repeat'),
    ).rejects.toThrow()
  })
  it('never grants administrative actions in a read-only detail', async () => {
    value = requestFixture()
    await expect(teamRequestDetail('tqr_one', true)).rejects.toThrow()
    value = { ...requestFixture(), allowed_actions: [], approval_preview: null }
    await expect(teamRequestDetail('tqr_one', true)).resolves.toMatchObject({ allowed_actions: [] })
    expect(requests[0].url).toBe('/admin/quota-requests/tqr_one')
  })
  it.each([
    { workspace_available: undefined },
    { approval_preview: undefined },
    {
      approval_preview: {
        member_before: '10',
        member_after: 20,
        team_before: '100',
        team_after: '100',
        escalates: false,
      },
    },
    {
      approval_preview: {
        member_before: '10',
        member_after: '20',
        team_before: '100',
        team_after: '100',
        escalates: 'yes',
      },
    },
  ])('rejects malformed server approval preview/navigation %j', async (patch) => {
    value = { ...requestFixture(), ...patch }
    await expect(teamRequestDetail('tqr_one', false)).rejects.toThrow()
  })
  it('binds creation receipts to original UUID/actor/Team/target/reason', async () => {
    const body = {
      request_id: '00000000-0000-4000-8000-000000000001',
      dimension: 'money' as const,
      target_value: '20.000',
      reason: 'Exact request',
    }
    value = { ...requestFixture('money'), reason: body.reason }
    await expect(
      createTeamRequest('tea_one', 'usr_member', body, 'e'.repeat(64), 'csrf-current'),
    ).resolves.toMatchObject({ target_value: '20' })
    expect(JSON.parse(requests[0].data)).toEqual(body)
    expect(requests[0].headers.get('If-Match')).toBe(`"${'e'.repeat(64)}"`)
    value = {
      ...requestFixture('money'),
      request_id: '00000000-0000-4000-8000-000000000003',
      reason: body.reason,
    }
    await expect(
      createTeamRequest('tea_one', 'usr_member', body, 'e'.repeat(64), 'csrf-current'),
    ).rejects.toThrow()
  })
  it('accepts saved owner approval while the request advances to admin without claiming applied', async () => {
    const saved_step = {
      ...requestFixture().steps[0],
      status: 'approved' as const,
      actor_id: 'usr_owner',
      actor_name: 'Owner',
      acted_at: '2026-10-03T01:00:00Z',
    }
    const request = {
      ...requestFixture(),
      status: 'pending_quota_admin',
      current_step_id: 'qst_admin',
      steps: [
        saved_step,
        {
          ...requestFixture().steps[0],
          id: 'qst_admin',
          stage: 'quota_admin',
          entered_at: '2026-10-03T01:00:00Z',
        },
      ],
    }
    value = { ...decision, committed: true, saved_step, request }
    await expect(
      decideTeamRequest('tqr_one', 'usr_owner', decision, 'f'.repeat(64), 'csrf'),
    ).resolves.toMatchObject({ request: { status: 'pending_quota_admin', application: null } })
    value = { ...(value as object), saved_step: { ...saved_step, actor_id: 'usr_other' } }
    await expect(
      decideTeamRequest('tqr_one', 'usr_owner', decision, 'f'.repeat(64), 'csrf'),
    ).rejects.toThrow()
  })
  it('rejects contradictory application claims and malformed saved-step outcomes', async () => {
    value = {
      ...requestFixture(),
      status: 'approved',
      current_step_id: null,
      resolved_at: '2026-10-03T01:00:00Z',
      allowed_actions: [],
      application: { runtime_applied: true, application_status: 'pending' },
    }
    await expect(teamRequestDetail('tqr_one', false)).rejects.toThrow()
    value = {
      ...decision,
      committed: true,
      saved_step: requestFixture().steps[0],
      request: requestFixture(),
    }
    await expect(
      decideTeamRequest('tqr_one', 'usr_owner', decision, 'f'.repeat(64), 'csrf'),
    ).rejects.toThrow()
  })
})
