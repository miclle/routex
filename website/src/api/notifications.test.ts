import { afterEach, describe, expect, it, vi } from 'vitest'
import client from './client'
import { getNotifications, recordedMonthlyQuotaWarning } from './notifications'
import type { Notification, MonthlyQuotaWarningSnapshot } from '@/types/notifications'

const item = {
  id: 'qni_1',
  kind: 'monthly_quota_exhausted',
  detail_code: 'tokens_month_exhausted',
  severity: 'high',
  occurrence_count: 1,
  read: false,
  read_at: null,
  first_seen_at: '2026-10-01T00:00:00Z',
  last_seen_at: '2026-10-01T00:00:00Z',
}
afterEach(() => vi.restoreAllMocks())

describe('Notification response boundary', () => {
  it.each([
    null,
    {},
    { items: null, unread_count: 0 },
    { items: {}, unread_count: 0 },
    { items: [null], unread_count: 1 },
    { items: [undefined], unread_count: 1 },
    { items: [item, {}], unread_count: 2 },
    { items: [{ ...item, subject_name: {} }], unread_count: 1 },
    { items: [], unread_count: '1' },
    { items: [], unread_count: -1 },
    { items: [], unread_count: 1.5 },
    { items: [], unread_count: Number.MAX_SAFE_INTEGER + 1 },
    { items: [], unread_count: 0, next_cursor: {} },
  ])('rejects a malformed successful response %#', async (data) => {
    vi.spyOn(client, 'get').mockResolvedValueOnce({ data })
    await expect(getNotifications('unread')).rejects.toThrow('Invalid notification response')
  })

  it('preserves exact snapshot strings and unknown notification kinds while normalizing omitted cursors', async () => {
    const snapshot = { limit: '9007199254740993', settled: '9007199254740994' }
    const data = {
      items: [{ ...item, kind: 'future_kind', quota: snapshot }],
      unread_count: 1,
    }
    const get = vi.spyOn(client, 'get').mockResolvedValueOnce({ data })
    const controller = new AbortController()
    expect(await getNotifications('all', 'scoped-cursor', controller.signal)).toEqual({
      ...data,
      next_cursor: null,
    })
    expect(get).toHaveBeenCalledWith('/notifications', {
      params: { status: 'all', cursor: 'scoped-cursor' },
      signal: controller.signal,
    })
  })

  it('accepts the real empty inbox response without inventing an unread count', async () => {
    vi.spyOn(client, 'get').mockResolvedValueOnce({ data: { items: [], unread_count: 0 } })
    expect(await getNotifications('unread')).toEqual({
      items: [],
      unread_count: 0,
      next_cursor: null,
    })
  })
})

function memberNotice(changes: Record<string, unknown> = {}) {
  const digest = 'A'.repeat(52)
  return {
    ...item,
    subject_type: 'team_member',
    subject_id: digest,
    subject_name: 'Recorded Team',
    quota: {
      scope_kind: 'team_member',
      scope_id: digest,
      team_id: 'tem_recorded',
      member_user_id: 'usr_member',
      dimension: 'money',
      policy_revision: 'policy-4',
      month_start: '2026-10-01T00:00:00Z',
      month_end: '2026-11-01T00:00:00Z',
      as_of: '2026-10-04T12:00:00Z',
      time_zone: 'UTC',
      limit: '1.000000000000000001',
      settled: '9007199254740993.123456789012345678',
      currency: 'USD',
    },
    detail_code: 'money_month_exhausted',
    ...changes,
  }
}

describe('Private member quota snapshot boundary', () => {
  it('preserves exact recorded strings and binds the current recipient only in memory', async () => {
    const data = { items: [memberNotice()], unread_count: 1 }
    const get = vi.spyOn(client, 'get').mockResolvedValueOnce({ data })
    const controller = new AbortController()
    expect(
      await getNotifications('all', 'bounded-cursor', controller.signal, 'usr_member'),
    ).toEqual({ ...data, next_cursor: null })
    expect(get).toHaveBeenCalledWith('/notifications', {
      params: { status: 'all', cursor: 'bounded-cursor' },
      signal: controller.signal,
    })
  })
  it.each([
    ['missing Team proof', { team_id: undefined }],
    ['nontext Team proof', { team_id: {} }],
    ['unsafe Team proof', { team_id: 'tem_recorded/' }],
    ['trailing Team proof', { team_id: 'tem_recorded ' }],
    ['missing User proof', { member_user_id: undefined }],
    ['another recipient', { member_user_id: 'usr_other' }],
    ['recipient alias', { member_user_id: 'USR_MEMBER' }],
    ['recipient trailing space', { member_user_id: 'usr_member ' }],
    ['short digest', { scope_id: 'A'.repeat(51) }],
    ['long digest', { scope_id: 'A'.repeat(53) }],
    ['casefolded digest', { scope_id: 'a'.repeat(52) }],
    ['noncanonical digest padding', { scope_id: 'A'.repeat(51) + 'B' }],
    ['negative settled', { settled: '-1' }],
    ['exponent amount', { settled: '1e3' }],
    ['numeric amount', { settled: 1 }],
    ['fraction precision overflow', { settled: '0.1234567890123456789' }],
    ['limit size overflow', { limit: '9'.repeat(41) }],
    ['missing currency', { currency: null }],
    ['currency alias', { currency: 'usd' }],
    ['currency control', { currency: 'US\n' }],
    ['missing revision', { policy_revision: '' }],
    ['unsafe revision', { policy_revision: 'policy/' }],
    ['unknown observation', { as_of: '' }],
    ['observation at exclusive end', { as_of: '2026-11-01T00:00:00Z' }],
    ['backwards window', { month_end: '2026-09-01T00:00:00Z' }],
    ['unknown calendar', { time_zone: 'not/a/calendar' }],
  ])('rejects %s without caching or rendering private member facts', async (_name, changes) => {
    const original = memberNotice()
    const notice = { ...original, quota: { ...original.quota, ...changes } }
    vi.spyOn(client, 'get').mockResolvedValueOnce({ data: { items: [notice], unread_count: 1 } })
    await expect(getNotifications('unread', undefined, undefined, 'usr_member')).rejects.toThrow(
      'Invalid notification response',
    )
  })
  it.each([
    { subject_type: undefined },
    { subject_type: 'team' },
    { subject_id: 'B'.repeat(51) + 'A' },
    { subject_name: 'secret\nTeam' },
    { subject_name: '\ud800' },
    { subject_name: '名'.repeat(34) },
    { severity: 'medium' },
    { occurrence_count: 2 },
    { alert_id: 'alert_borrowed' },
    { delivery_status: 'accepted' },
  ])('rejects a mismatched member subject or unrelated event attributes %j', async (changes) => {
    vi.spyOn(client, 'get').mockResolvedValueOnce({
      data: { items: [memberNotice(changes)], unread_count: 1 },
    })
    await expect(getNotifications('unread', undefined, undefined, 'usr_member')).rejects.toThrow(
      'Invalid notification response',
    )
  })
  it('requires an exact captured actor even when a member snapshot is otherwise valid', async () => {
    const get = vi
      .spyOn(client, 'get')
      .mockResolvedValue({ data: { items: [memberNotice()], unread_count: 1 } })
    for (const actor of [undefined, '', 'usr_other', 'USR_MEMBER', 'usr_member '])
      await expect(getNotifications('unread', undefined, undefined, actor)).rejects.toThrow(
        'Invalid notification response',
      )
    expect(get).toHaveBeenCalledTimes(5)
  })
  it.each(['0', '9007199254740993', '9223372036854775807'])(
    'accepts authoritative token string %s with no number conversion',
    async (amount) => {
      const original = memberNotice()
      const notice = {
        ...original,
        detail_code: 'tokens_month_exhausted',
        quota: {
          ...original.quota,
          dimension: 'tokens',
          limit: amount,
          settled: amount,
          currency: null,
        },
      }
      vi.spyOn(client, 'get').mockResolvedValueOnce({ data: { items: [notice], unread_count: 1 } })
      expect(
        (await getNotifications('unread', undefined, undefined, 'usr_member')).items[0].quota
          ?.settled,
      ).toBe(amount)
    },
  )
  it.each(['01', '9223372036854775808', '1.0'])(
    'rejects malformed/out-of-range token string %s',
    async (amount) => {
      const original = memberNotice()
      const notice = {
        ...original,
        detail_code: 'tokens_month_exhausted',
        quota: {
          ...original.quota,
          dimension: 'tokens',
          limit: amount,
          settled: amount,
          currency: null,
        },
      }
      vi.spyOn(client, 'get').mockResolvedValueOnce({ data: { items: [notice], unread_count: 1 } })
      await expect(getNotifications('unread', undefined, undefined, 'usr_member')).rejects.toThrow(
        'Invalid notification response',
      )
    },
  )
})

function warningNotice(overrides: Partial<MonthlyQuotaWarningSnapshot> = {}): Notification {
  const warning: MonthlyQuotaWarningSnapshot = {
    scope_kind: 'user',
    scope_id: 'usr_member',
    dimension: 'tokens',
    policy_revision: 'policy-4',
    month_start: '2026-10-01T00:00:00Z',
    month_end: '2026-11-01T00:00:00Z',
    as_of: '2026-10-04T12:00:00Z',
    time_zone: 'UTC',
    limit: '100',
    settled: '80',
    currency: null,
    level: 'near',
    threshold: 80,
    threshold_generation: 'personal-monthly-80-90-v1',
    ...overrides,
  }
  return {
    ...item,
    id: 'qwi_1',
    quota_warning_observation_id: 'qwo_1',
    quota_warning: warning,
    kind: 'monthly_quota_warning',
    detail_code: `${warning.dimension}_month_${warning.level}`,
    severity: warning.level === 'near' ? 'medium' : 'high',
    subject_type: 'user',
    subject_id: 'usr_member',
  }
}
it.each([
  { level: 'near', threshold: 80 },
  { level: 'critical', threshold: 90 },
] as const)(
  'preserves authoritative $level/$threshold warning without computing a percentage',
  async ({ level, threshold }) => {
    const notification = warningNotice({
      level,
      threshold,
      settled: level === 'near' ? '84' : '93',
    })
    expect(recordedMonthlyQuotaWarning(notification, 'usr_member')).toBe(notification.quota_warning)
    vi.spyOn(client, 'get').mockResolvedValueOnce({
      data: { items: [notification], unread_count: 1 },
    })
    expect((await getNotifications('unread', null, undefined, 'usr_member')).items[0]).toEqual(
      notification,
    )
  },
)

function teamWarningNotice(overrides: Partial<MonthlyQuotaWarningSnapshot> = {}): Notification {
  const original = warningNotice({
    scope_kind: 'team',
    scope_id: 'tem_recorded',
    threshold_generation: 'team-monthly-80-90-v1',
    ...overrides,
  })
  return {
    ...original,
    id: 'twi_1',
    quota_warning_observation_id: 'two_1',
    subject_type: 'team',
    subject_id: 'tem_recorded',
    subject_name: 'Recorded Team',
  }
}

describe('Recorded Team monthly warning boundary', () => {
  it.each([
    { dimension: 'tokens', level: 'near', threshold: 80, currency: null },
    { dimension: 'tokens', level: 'critical', threshold: 90, currency: null },
    { dimension: 'money', level: 'near', threshold: 80, currency: 'USD' },
    { dimension: 'money', level: 'critical', threshold: 90, currency: 'USD' },
  ] as const)(
    'accepts the authorized Team $dimension/$level snapshot for a distinct recipient',
    async (snapshot) => {
      const notification = teamWarningNotice({
        ...snapshot,
        limit: snapshot.dimension === 'money' ? '9007199254740993.123456789012345678' : '100',
        settled: snapshot.dimension === 'money' ? '8907199254740993.123456789012345678' : '93',
      })
      const before = structuredClone(notification)
      expect(recordedMonthlyQuotaWarning(notification, 'usr_current')).toBe(
        notification.quota_warning,
      )
      const get = vi.spyOn(client, 'get').mockResolvedValueOnce({
        data: { items: [notification], unread_count: 1, next_cursor: 'recipient-cursor' },
      })
      expect(
        (await getNotifications('all', 'prior-cursor', undefined, 'usr_current')).items[0],
      ).toEqual(before)
      expect(get).toHaveBeenCalledWith('/notifications', {
        params: { status: 'all', cursor: 'prior-cursor' },
        signal: undefined,
      })
      expect(notification).toEqual(before)
    },
  )

  it.each([undefined, null, '', 'Original historical Team', '名'.repeat(100)])(
    'preserves the optional recorded Team name without an invented byte cap %#',
    (subject_name) => {
      const notification = { ...teamWarningNotice(), subject_name }
      expect(recordedMonthlyQuotaWarning(notification, 'usr_current')).toBe(
        notification.quota_warning,
      )
    },
  )

  it.each([
    ['Personal inbox', { id: 'qwi_1' }],
    ['Personal observation', { quota_warning_observation_id: 'qwo_1' }],
    ['subject alias', { subject_id: 'TEM_RECORDED' }],
    ['subject trailing space', { subject_id: 'tem_recorded ' }],
    ['wrong subject', { subject_id: 'tem_other' }],
    ['missing subject', { subject_id: undefined }],
    ['wrong subject kind', { subject_type: 'user' }],
    ['missing subject kind', { subject_type: undefined }],
    ['unsafe name', { subject_name: 'Recorded\nTeam' }],
    ['invalid name text', { subject_name: '\ud800' }],
    ['borrowed alert', { alert_id: 'alt_private' }],
    ['borrowed delivery', { delivery_status: 'accepted' }],
  ])('keeps %s unavailable instead of exposing a Team snapshot', (_, changes) => {
    expect(
      recordedMonthlyQuotaWarning(
        { ...teamWarningNotice(), ...changes } as Notification,
        'usr_current',
      ),
    ).toBeUndefined()
  })

  it.each([
    ['Personal generation', { threshold_generation: 'personal-monthly-80-90-v1' }],
    ['unknown generation', { threshold_generation: 'team-monthly-future' }],
    ['child scope', { scope_kind: 'team_member' }],
    ['Project scope', { scope_kind: 'project' }],
    ['unsafe Team ID', { scope_id: 'tem_recorded/' }],
    ['trailing Team ID', { scope_id: 'tem_recorded ' }],
    ['case alias Team ID', { scope_id: 'TEM_RECORDED' }],
    ['numeric Team ID', { scope_id: 1 }],
    ['unknown threshold', { threshold: 85 }],
    ['contradictory level', { level: 'critical' }],
    ['unknown settled usage', { settled: null }],
    ['numeric settled usage', { settled: 80 }],
    ['token overflow', { settled: '9223372036854775808' }],
    ['zero stored limit', { limit: '0' }],
    ['currency on tokens', { currency: 'USD' }],
    ['unknown calendar', { time_zone: 'Unknown/Calendar' }],
    ['exclusive month end', { as_of: '2026-11-01T00:00:00Z' }],
  ])('keeps %s unavailable without estimating usage or current policy', (_, changes) => {
    const notification = teamWarningNotice()
    notification.quota_warning = {
      ...notification.quota_warning,
      ...changes,
    } as MonthlyQuotaWarningSnapshot
    expect(recordedMonthlyQuotaWarning(notification, 'usr_current')).toBeUndefined()
  })

  it('requires a safe current recipient without treating Team scope as recipient identity', () => {
    for (const recipient of ['', 'usr_current ', 'usr_current/'])
      expect(recordedMonthlyQuotaWarning(teamWarningNotice(), recipient)).toBeUndefined()
    expect(recordedMonthlyQuotaWarning(teamWarningNotice(), 'usr_current')).toBeDefined()
  })

  it('does not admit Team identity or generation into a Personal snapshot', () => {
    expect(
      recordedMonthlyQuotaWarning(
        warningNotice({ threshold_generation: 'team-monthly-80-90-v1' }),
        'usr_member',
      ),
    ).toBeUndefined()
    expect(
      recordedMonthlyQuotaWarning({ ...warningNotice(), id: 'twi_1' }, 'usr_member'),
    ).toBeUndefined()
  })
})
it('preserves recorded money precision and denomination without floating-point conversion', () => {
  const notification = warningNotice({
    dimension: 'money',
    currency: 'USD',
    limit: '9007199254740993.123456789012345678',
    settled: '8007199254740993.123456789012345678',
  })
  expect(recordedMonthlyQuotaWarning(notification, 'usr_member')).toEqual(
    notification.quota_warning,
  )
})
it.each([
  ['missing', { quota_warning: undefined }],
  ['wrong owner', { quota_warning: { ...warningNotice().quota_warning, scope_id: 'usr_other' } }],
  ['wrong subject', { subject_id: 'usr_other' }],
  ['wrong scope', { subject_type: 'project' }],
  ['wrong inbox identity', { id: 'qnt_1' }],
  ['wrong observation identity', { quota_warning_observation_id: 'qob_1' }],
  ['contradictory severity', { severity: 'high' }],
  ['contradictory detail', { detail_code: 'money_month_near' }],
  ['operational alert', { alert_id: 'alt_1' }],
  ['external delivery', { delivery_status: 'accepted' }],
  ['exhaustion snapshot', { quota: { limit: '100' } }],
  ['exhaustion observation', { quota_observation_id: 'qob_1' }],
  ['unknown level', { quota_warning: { ...warningNotice().quota_warning, level: 'future' } }],
  ['wrong threshold', { quota_warning: { ...warningNotice().quota_warning, threshold: 90 } }],
  [
    'unknown generation',
    { quota_warning: { ...warningNotice().quota_warning, threshold_generation: 'future-v2' } },
  ],
  ['numeric amount', { quota_warning: { ...warningNotice().quota_warning, settled: 84 } }],
  ['unknown token amount', { quota_warning: { ...warningNotice().quota_warning, settled: null } }],
  [
    'oversized integer',
    { quota_warning: { ...warningNotice().quota_warning, settled: '9223372036854775808' } },
  ],
  ['zero limit', { quota_warning: { ...warningNotice().quota_warning, limit: '0' } }],
  ['token denomination', { quota_warning: { ...warningNotice().quota_warning, currency: 'USD' } }],
  [
    'unknown calendar',
    { quota_warning: { ...warningNotice().quota_warning, time_zone: 'Invalid/Zone' } },
  ],
  [
    'exclusive end',
    { quota_warning: { ...warningNotice().quota_warning, as_of: '2026-11-01T00:00:00Z' } },
  ],
  [
    'reversed interval',
    { quota_warning: { ...warningNotice().quota_warning, month_end: '2026-09-01T00:00:00Z' } },
  ],
])('keeps %s warning snapshot unavailable without exposing its values', (_, changes) => {
  const notification = { ...warningNotice(), ...changes } as Notification
  expect(recordedMonthlyQuotaWarning(notification, 'usr_member')).toBeUndefined()
})
it('requires exact current recipient and never borrows a previous owner snapshot', () => {
  expect(recordedMonthlyQuotaWarning(warningNotice(), 'usr_other')).toBeUndefined()
  expect(recordedMonthlyQuotaWarning(warningNotice(), '')).toBeUndefined()
})
it.each([
  { quota_warning_observation_id: {} },
  { quota_warning: [] },
  { quota_warning: 'private' },
])(
  'rejects malformed top-level warning metadata rather than treating it as an empty inbox %#',
  async (changes) => {
    vi.spyOn(client, 'get').mockResolvedValueOnce({
      data: { items: [{ ...warningNotice(), ...changes }], unread_count: 1 },
    })
    await expect(getNotifications('unread', null, undefined, 'usr_member')).rejects.toThrow(
      'Invalid notification response',
    )
  },
)
