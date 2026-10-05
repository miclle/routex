import { afterEach, expect, it, vi } from 'vitest'
import client from './client'
import { getMember, getMembers } from './governance'
import { getMemberDetail, isMemberRecentLogin, validateMemberDetail } from './member-recent-login'
import { validateMemberListPage } from './member-list'
import { memberListPage } from '@/views/governance/member-list.fixture'
const detail = () => ({
  id: 'legacy_user',
  email: 'member@example.invalid',
  name: 'Retained Member',
  role: 'member',
  disabled: false,
  offboarded_at: null,
  created_at: '2026-01-01T00:00:00Z',
  role_ids: [],
  registration_approval: { status: 'not_required', admission_eligible: false },
  last_login_at: null,
  last_login_status: 'historical_unavailable',
})
afterEach(() => vi.restoreAllMocks())
it.each(['2000-02-29T01:02:03Z', '2026-10-05T01:02:03.1Z', '2026-10-05T01:02:03.123456Z'])(
  'accepts exact persisted UTC time %s without inference or clock checks',
  (stamp) => {
    vi.spyOn(Date, 'now').mockReturnValue(0)
    const row = { ...detail(), last_login_status: 'recorded', last_login_at: stamp }
    expect(validateMemberDetail(row, 'legacy_user')).toEqual(row)
    const page = memberListPage()
    page.items[0] = { ...page.items[0], last_login_status: 'recorded', last_login_at: stamp }
    expect(validateMemberListPage(page, 'usr_admin')).toEqual(page)
  },
)
it('keeps historical absence valid forever and preserves disabled/offboarded recorded history', () => {
  expect(validateMemberDetail(detail(), 'legacy_user').last_login_at).toBeNull()
  const row = {
    ...detail(),
    disabled: true,
    offboarded_at: '2026-10-04T00:00:00Z',
    last_login_status: 'recorded',
    last_login_at: '2026-10-03T00:00:00Z',
  }
  expect(validateMemberDetail(row, 'legacy_user')).toEqual(row)
})
it.each([
  undefined,
  0,
  '',
  '0000-01-01T00:00:00Z',
  '0001-01-01T00:00:00Z',
  '0001-01-01T00:00:00.000000Z',
  '2025-02-29T00:00:00Z',
  '2026-02-30T00:00:00Z',
  '2026-13-01T00:00:00Z',
  '2026-01-01T24:00:00Z',
  '2026-10-05T01:02:03.1234567Z',
  '2026-10-05T01:02:03+00:00',
  '2026-10-05',
])('rejects recorded invalid time %s', (stamp) => {
  const row = { ...detail(), last_login_status: 'recorded', last_login_at: stamp }
  expect(isMemberRecentLogin(row)).toBe(false)
  expect(() => validateMemberDetail(row, 'legacy_user')).toThrow()
  const page = memberListPage()
  Object.assign(page.items[0], row, { id: 'usr_target' })
  expect(() => validateMemberListPage(page, 'usr_admin')).toThrow()
})
it.each([
  { last_login_status: 'recorded', last_login_at: null },
  { last_login_status: 'historical_unavailable', last_login_at: '2026-10-05T00:00:00Z' },
  { last_login_status: 'never', last_login_at: null },
  { last_login_status: undefined, last_login_at: null },
])('rejects contradictory or unknown login state %j', (state) => {
  expect(() => validateMemberDetail({ ...detail(), ...state }, 'legacy_user')).toThrow()
  const page = memberListPage()
  Object.assign(page.items[0], state)
  expect(() => validateMemberListPage(page, 'usr_admin')).toThrow()
})
it.each([
  'secret',
  'foreign',
  'duplicate role',
  'missing offboarding',
  'unsafe role',
  'large roles',
])('rejects %s detail proof', (kind) => {
  const row: Record<string, unknown> = detail()
  if (kind === 'secret') row.password_hash = 'unexpected'
  if (kind === 'foreign') row.id = 'LEGACY_user'
  if (kind === 'duplicate role') row.role_ids = ['role_a', 'role_a']
  if (kind === 'missing offboarding') delete row.offboarded_at
  if (kind === 'unsafe role') row.role_ids = [' role_a']
  if (kind === 'large roles') row.role_ids = Array.from({ length: 10001 }, (_, i) => `role_${i}`)
  expect(() => validateMemberDetail(row, 'legacy_user')).toThrow()
})
it('retains complete historical roles beyond new-write limit without truncation', () => {
  const row = { ...detail(), role_ids: Array.from({ length: 101 }, (_, i) => `role_${i}`) }
  expect(validateMemberDetail(row, 'legacy_user').role_ids).toHaveLength(101)
})
it('uses only the existing detail GET and passes AbortSignal before decoding', async () => {
  const signal = new AbortController().signal,
    get = vi.spyOn(client, 'get').mockResolvedValue({ data: detail() })
  expect(await getMemberDetail('legacy_user', signal)).toEqual(detail())
  expect(get).toHaveBeenCalledExactlyOnceWith('/admin/members/legacy_user', { signal })
  await expect(getMemberDetail(' legacy_user', signal)).rejects.toThrow()
  expect(get).toHaveBeenCalledTimes(1)
})
it('preserves existing legacy detail/list transports and does not use write responses as login proof', async () => {
  const legacy = { id: 'legacy_user' }
  const get = vi.spyOn(client, 'get').mockResolvedValue({ data: legacy })
  expect(await getMember('legacy_user')).toEqual(legacy)
  expect(await getMembers({}, null)).toEqual(legacy)
  expect(get).toHaveBeenCalledTimes(2)
  await expect(getMemberDetail('legacy_user')).rejects.toThrow()
})

it.each(['0001-01-02T00:00:00Z', '0001-01-01T00:00:00.000001Z'])(
  'preserves a legitimate nonzero early timestamp %s',
  (stamp) => {
    expect(isMemberRecentLogin({ last_login_status: 'recorded', last_login_at: stamp })).toBe(true)
    expect(
      validateMemberDetail(
        { ...detail(), last_login_status: 'recorded', last_login_at: stamp },
        'legacy_user',
      ).last_login_at,
    ).toBe(stamp)
  },
)

it.each(['', 'Retained\n\u0000control', '界'.repeat(100)])(
  'preserves historical name %j without applying creation validation',
  (name) => {
    const row = { ...detail(), name }
    expect(validateMemberDetail(row, 'legacy_user')).toEqual(row)
  },
)
it.each(['界'.repeat(101), 0, null])('retains the read-name bound for %j', (name) => {
  expect(() => validateMemberDetail({ ...detail(), name }, 'legacy_user')).toThrow()
})
it.each(['', 'a'.repeat(255), null])(
  'retains the nonempty bounded email contract for %j',
  (email) => {
    expect(() => validateMemberDetail({ ...detail(), email }, 'legacy_user')).toThrow()
  },
)
it.each([
  '2026-10-05T08:19:00+08:00',
  '2026-10-05T00:19:00-07:30',
  '2026-10-05T00:19:00.123456789Z',
  '2000-02-29T23:59:59.123456789+23:59',
  '0001-01-02T00:00:00.000000001+00:00',
])('preserves calendar-valid retained timestamps %s without normalization', (stamp) => {
  const row = { ...detail(), created_at: stamp, offboarded_at: stamp }
  expect(validateMemberDetail(row, 'legacy_user')).toEqual(row)
})
it.each([
  '2025-02-29T00:00:00+08:00',
  '2026-02-30T00:00:00-07:00',
  '2026-04-31T00:00:00Z',
  '2026-00-01T00:00:00Z',
  '2026-13-01T00:00:00Z',
  '2026-10-05T24:00:00Z',
  '2026-10-05T00:60:00Z',
  '2026-10-05T00:00:60Z',
  '2026-10-05T00:00:00+24:00',
  '2026-10-05T00:00:00+00:60',
  '2026-10-05T00:00:00.1234567890Z',
  '2026-10-05T00:00:00+0800',
  '0000-01-01T00:00:00Z',
])('rejects invalid retained calendar/offset %s for both fields', (stamp) => {
  expect(() => validateMemberDetail({ ...detail(), created_at: stamp }, 'legacy_user')).toThrow()
  expect(() => validateMemberDetail({ ...detail(), offboarded_at: stamp }, 'legacy_user')).toThrow()
})
it('does not relax recorded-login proof while accepting retained offsets', () => {
  const row = { ...detail(), created_at: '2026-10-05T08:00:00+08:00' }
  expect(validateMemberDetail(row, 'legacy_user').last_login_status).toBe('historical_unavailable')
  for (const stamp of ['2026-10-05T08:00:00+08:00', '2026-10-05T00:00:00.123456789Z']) {
    expect(() =>
      validateMemberDetail(
        { ...row, last_login_status: 'recorded', last_login_at: stamp },
        'legacy_user',
      ),
    ).toThrow()
    expect(isMemberRecentLogin({ last_login_status: 'recorded', last_login_at: stamp })).toBe(false)
  }
})
