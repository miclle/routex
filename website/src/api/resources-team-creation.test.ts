import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  getTeamCreationContext,
  createTeamWithInitialLimits,
  getTeamCreationOwners,
} from './resources'
import type { TeamCreationContext, TeamCreationIntent } from '@/types/resources'
const original = client.defaults.adapter
let value: unknown, etag: string, requests: InternalAxiosRequestConfig[]
let responseStatus: number
const ctx: TeamCreationContext = {
  review_etag: 'a'.repeat(64),
  default_rule_etag: 'b'.repeat(64),
  platform_currency: 'USD',
  editable_fields: [
    'tokens_5h',
    'tokens_7d',
    'tokens_month',
    'money_month',
    'rpm',
    'tpm',
    'concurrency',
  ],
  default_policy: {
    tokens_5h: null,
    tokens_7d: '9007199254740991',
    tokens_month: '10',
    money_month: '10.000000000000000001',
    currency: 'USD',
    rpm: null,
    tpm: '0',
    concurrency: '3',
  },
}
const intent: TeamCreationIntent = {
  etag: 'a'.repeat(64),
  body: {
    creation_id: '33333333-3333-4333-8333-333333333333',
    name: 'Team',
    description: '',
    owner_ids: ['usr_one'],
  },
}
function receipt() {
  return {
    team: {
      id: 'tea_one',
      name: 'Team',
      description: '',
      status: 'active',
      created_at: '2026-10-06T01:00:00Z',
      model_ids: [],
      members: [
        {
          id: 'tme_one',
          user_id: 'usr_one',
          name: 'Owner',
          email: 'owner@example.invalid',
          role: 'owner',
          status: 'active',
        },
      ],
    },
    receipt: {
      creation_id: intent.body.creation_id,
      team_id: 'tea_one',
      created_at: '2026-10-06T01:00:00Z',
    },
    committed: true,
    runtime_applied: false,
    application_status: 'pending',
  }
}
beforeEach(() => {
  responseStatus = 200
  value = structuredClone(ctx)
  etag = `"${ctx.review_etag}"`
  requests = []
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return {
      config,
      status: responseStatus,
      statusText: '',
      headers: new AxiosHeaders({ ETag: etag }),
      data: value,
    }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
describe('Team reviewed creation transport', () => {
  it('uses purpose path, abort signal and quoted strong review response', async () => {
    const signal = new AbortController().signal
    expect(await getTeamCreationContext(signal)).toEqual(ctx)
    expect(requests[0].url).toBe('/admin/teams/creation-context')
    expect(requests[0].signal).toBe(signal)
  })
  it('retains exact largest supported integer preview and decimal money', async () => {
    const result = await getTeamCreationContext()
    expect(result.default_policy.tokens_7d).toBe('9007199254740991')
    expect(result.default_policy.money_month).toBe('10.000000000000000001')
  })
  it('accepts teams.write-only neutral view and rejects hidden-value leakage', async () => {
    value = { ...ctx, platform_currency: null, editable_fields: [], default_policy: {} }
    expect((await getTeamCreationContext()).default_policy).toEqual({})
    value = {
      ...ctx,
      platform_currency: null,
      editable_fields: [],
      default_policy: { tokens_month: null },
    }
    await expect(getTeamCreationContext()).rejects.toThrow()
  })
  it('preserves unset stored money currency distinct from current currency', async () => {
    value = { ...ctx, default_policy: { ...ctx.default_policy, money_month: null, currency: '' } }
    expect((await getTeamCreationContext()).platform_currency).toBe('USD')
  })
  it.each([
    'weak',
    'mismatch',
    'number',
    'overflow',
    'partial',
    'unknown',
    'duplicate',
    'hiddenCurrency',
  ])('rejects %s context', async (kind) => {
    const invalid = structuredClone(ctx)
    if (kind === 'weak') etag = `W/"${ctx.review_etag}"`
    if (kind === 'mismatch') etag = '"' + 'c'.repeat(64) + '"'
    if (kind === 'number') Object.assign(invalid.default_policy, { rpm: 1 })
    if (kind === 'overflow') invalid.default_policy.rpm = '9007199254740992'
    if (kind === 'partial') invalid.editable_fields = ['rpm']
    if (kind === 'unknown') Object.assign(invalid, { unknown: true })
    if (kind === 'duplicate') invalid.editable_fields.push('rpm')
    if (kind === 'hiddenCurrency') {
      invalid.editable_fields = []
      invalid.default_policy = { currency: 'USD' }
      invalid.platform_currency = null
    }
    value = invalid
    await expect(getTeamCreationContext()).rejects.toThrow()
  })
  it('dispatches exact immutable body/header/current CSRF and confirms committed pending independently', async () => {
    value = receipt()
    const captured = {
      ...intent,
      body: {
        ...intent.body,
        initial_limits: {
          tokens_month: 0,
          money_month: '0.000000000000000001',
          currency: 'USD',
          reason: 'Reviewed',
        },
      },
    }
    const result = await createTeamWithInitialLimits(captured, 'fresh-csrf')
    expect(result.runtime_applied).toBe(false)
    expect(requests[0].url).toBe('/admin/teams')
    expect(JSON.parse(requests[0].data)).toEqual(captured.body)
    expect(requests[0].headers.get('If-Match')).toBe('"' + intent.etag + '"')
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
  })
  it('omits initial object for default copy and accepts null money without currency', async () => {
    value = receipt()
    await createTeamWithInitialLimits(intent, 'csrf')
    expect(JSON.parse(requests[0].data)).not.toHaveProperty('initial_limits')
    await createTeamWithInitialLimits(
      {
        ...intent,
        body: { ...intent.body, initial_limits: { money_month: null, reason: 'Clear' } },
      },
      'csrf',
    )
  })
  it.each([
    { reason: 'Only' },
    { reason: 'Clear', money_month: null, currency: 'USD' },
    { reason: 'Unsafe', rpm: Number.MAX_SAFE_INTEGER + 1 },
    { reason: 'Blank', money_month: '' },
    { reason: 'Null', currency: 'USD' },
    { reason: 'Control\n', rpm: 1 },
  ])('rejects malformed sparse object %j before dispatch', async (limits) => {
    value = receipt()
    await expect(
      createTeamWithInitialLimits(
        { ...intent, body: { ...intent.body, initial_limits: limits } },
        'csrf',
      ),
    ).rejects.toThrow()
    expect(requests).toHaveLength(0)
  })
  it.each(['wrongCreation', 'wrongTeam', 'unproved', 'status', 'unknown', 'privateMember'])(
    'rejects unconfirmed %s response',
    async (kind) => {
      const bad = receipt()
      if (kind === 'wrongCreation') bad.receipt.creation_id = 'other'
      if (kind === 'wrongTeam') bad.team.id = 'tea_other'
      if (kind === 'unproved') bad.runtime_applied = true
      if (kind === 'status') bad.application_status = 'imagined'
      if (kind === 'unknown') Object.assign(bad, { unexpected: true })
      if (kind === 'privateMember') bad.team.members[0].user_id = ' usr_one'
      value = bad
      await expect(createTeamWithInitialLimits(intent, 'csrf')).rejects.toThrow()
    },
  )
  it('uses bounded scoped owner endpoint and rejects duplicate IDs', async () => {
    value = { items: [{ id: 'usr_one', name: 'Owner' }] }
    expect(await getTeamCreationOwners('name')).toHaveLength(1)
    expect(requests[0].url).toBe('/admin/team-member-candidates')
    expect(requests[0].params).toEqual({ q: 'name' })
    value = {
      items: [
        { id: 'usr_one', name: 'Owner' },
        { id: 'usr_one', name: 'Other' },
      ],
    }
    await expect(getTeamCreationOwners('')).rejects.toThrow()
  })
  it.each([201, 200])(
    'accepts real Go UTC receipt and local-offset Team time in an applied %s response without rewriting intent or timestamps',
    async (status) => {
      responseStatus = status
      const applied = receipt()
      applied.receipt.created_at = '2026-10-06T00:36:07.3985Z'
      applied.team.created_at = '2026-10-06T08:36:07.379764+08:00'
      applied.application_status = 'applied'
      applied.runtime_applied = true
      value = applied
      const result = await createTeamWithInitialLimits(intent, 'current-csrf')
      expect(result).toEqual(applied)
      expect(result.receipt.created_at).toBe(applied.receipt.created_at)
      expect(result.team?.created_at).toBe(applied.team.created_at)
      expect(requests).toHaveLength(1)
      expect(requests[0].data).toBe(JSON.stringify(intent.body))
      expect(requests[0].headers.get('If-Match')).toBe('"' + intent.etag + '"')
      expect(requests[0].headers.get('X-CSRF-Token')).toBe('current-csrf')
    },
  )
  it.each([
    '2026-10-06T03:36:07.379764-05:00',
    '2026-10-06T08:36:07+05:45',
    '2024-02-29T23:59:59.123456789-00:30',
    '2026-10-06T08:36:07+23:59',
    '0001-01-01T00:00:00+01:00',
  ])(
    'accepts valid original Gregorian calendar and RFC3339 offset %s without UTC prefix equality',
    async (stamp) => {
      const applied = receipt()
      applied.team.created_at = stamp
      applied.receipt.created_at = stamp
      value = applied
      const result = await createTeamWithInitialLimits(intent, 'csrf')
      expect(result.team?.created_at).toBe(stamp)
      expect(result.receipt.created_at).toBe(stamp)
    },
  )
  it.each([
    '2026-02-30T08:36:07+08:00',
    '2025-02-29T08:36:07+08:00',
    '2026-04-31T08:36:07-05:00',
    '2026-00-01T08:36:07Z',
    '2026-10-00T08:36:07+08:00',
    '0000-01-01T08:36:07+08:00',
    '10000-01-01T08:36:07Z',
    '2026-10-06T24:00:00+08:00',
    '2026-10-06T08:60:07+08:00',
    '2026-10-06T08:36:60+08:00',
    '2026-10-06T08:36:07+24:00',
    '2026-10-06T08:36:07-24:00',
    '2026-10-06T08:36:07+08:60',
    '2026-10-06T08:36:07+8:00',
    '2026-10-06T08:36:07+0800',
    '2026-10-06T08:36:07',
    '2026-10-06T08:36:07.Z',
    '2026-10-06T08:36:07.1234567890+08:00',
    '2026-10-06T08:36:07Z+08:00',
    '2026-10-06T08:36:07+08:00 ',
  ])(
    'rejects invalid calendar/time/fraction/offset %s in either receipt or Team without accepting application',
    async (stamp) => {
      for (const field of ['receipt', 'team'] as const) {
        const applied = receipt()
        applied.application_status = 'applied'
        applied.runtime_applied = true
        applied[field].created_at = stamp
        value = applied
        await expect(createTeamWithInitialLimits(intent, 'csrf')).rejects.toThrow()
      }
    },
  )
  it('rejects impossible receipt dates and falsely applied different owner sets', async () => {
    const invalid = receipt()
    invalid.receipt.created_at = '2026-02-30T01:00:00Z'
    value = invalid
    await expect(createTeamWithInitialLimits(intent, 'csrf')).rejects.toThrow()
    const changed = receipt()
    changed.application_status = 'applied'
    changed.runtime_applied = true
    changed.team.members[0].user_id = 'usr_other'
    value = changed
    await expect(createTeamWithInitialLimits(intent, 'csrf')).rejects.toThrow()
  })
  it('accepts unavailable committed receipt only with null target, and rejects unknown response status', async () => {
    value = { ...receipt(), team: null, application_status: 'unavailable' }
    expect((await createTeamWithInitialLimits(intent, 'csrf')).application_status).toBe(
      'unavailable',
    )
    value = { ...receipt(), team: null, application_status: 'pending' }
    await expect(createTeamWithInitialLimits(intent, 'csrf')).rejects.toThrow()
  })
  it('rejects owner page overflow and oversized or malformed search before any request', async () => {
    value = {
      items: Array.from({ length: 51 }, (_, index) => ({ id: `usr_${index}`, name: 'Owner' })),
    }
    await expect(getTeamCreationOwners('')).rejects.toThrow()
    requests = []
    await expect(getTeamCreationOwners('界'.repeat(67))).rejects.toThrow()
    await expect(getTeamCreationOwners('\ud800')).rejects.toThrow()
    expect(requests).toHaveLength(0)
  })
})
