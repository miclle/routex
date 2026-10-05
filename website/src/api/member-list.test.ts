import { afterEach, expect, it, vi } from 'vitest'
import client from './client'
import { getMembers } from './governance'
import { getMemberList, validateMemberListPage, validateMemberListChain } from './member-list'
import { memberListPage, memberListRow } from '@/views/governance/member-list.fixture'
afterEach(() => vi.restoreAllMocks())
it('reads the bounded list with captured actor, exact filters and signal, retaining precision', async () => {
  const p = memberListPage(),
    signal = new AbortController().signal
  const get = vi.spyOn(client, 'get').mockResolvedValue({ data: p })
  expect(
    await getMemberList({ q: 'literal_%', status: 'active' }, 'usr_admin', null, signal),
  ).toEqual(p)
  expect(get).toHaveBeenCalledWith('/admin/members', {
    params: { q: 'literal_%', status: 'active', cursor: undefined },
    signal,
  })
  expect(p.items[0].personal.money_month).toBe('99999999999999999.000000000000000001')
})
it('leaves the existing offboarding legacy getMembers transport unchanged', async () => {
  const legacy = { items: [{ id: 'usr_target' }], next_cursor: null }
  vi.spyOn(client, 'get').mockResolvedValue({ data: legacy })
  expect(await getMembers({ status: 'active' }, null)).toEqual(legacy)
})
it.each([
  'foreign actor',
  'unsafe id',
  'numeric keys',
  'numeric tokens',
  'negative money',
  'missing live holds',
  'inactive retains usage',
  'login inferred',
  'unknown login status',
  'Team withheld leak',
  'Team alias duplicate',
  'unknown Team state',
  'secret field',
  'unordered',
  'bad cursor',
  'role duplicate',
  'invalid timezone',
  'absent policy fiction',
  'foreign account',
  'offboarded application',
])('rejects %s', (kind) => {
  const p = memberListPage(),
    r = p.items[0]
  switch (kind) {
    case 'foreign actor':
      p.actor_user_id = 'usr_peer'
      break
    case 'unsafe id':
      r.id += ' '
      break
    case 'numeric keys':
      ;(r as unknown as Record<string, unknown>).total_personal_keys = 3
      break
    case 'numeric tokens':
      ;(r.personal as unknown as Record<string, unknown>).tokens_month = 0
      break
    case 'negative money':
      r.personal.usage!.money_used.USD = '-1'
      break
    case 'missing live holds':
      r.personal.active_reservations = null
      break
    case 'inactive retains usage':
      r.personal.usage_status = 'inactive'
      break
    case 'login inferred':
      ;(r as unknown as Record<string, unknown>).last_login_at = r.created_at
      break
    case 'unknown login status':
      ;(r as unknown as Record<string, unknown>).last_login_status = 'known'
      break
    case 'Team withheld leak':
      ;(r.teams as unknown as Record<string, unknown>).status = 'not_authorized'
      break
    case 'Team alias duplicate':
      if (r.teams.items) r.teams.items.push({ ...r.teams.items[0] })
      break
    case 'unknown Team state':
      if (r.teams.items) (r.teams.items[0] as unknown as Record<string, unknown>).status = 'missing'
      break
    case 'secret field':
      ;(r as unknown as Record<string, unknown>).password_hash = 'unexpected'
      break
    case 'unordered':
      p.items.push(memberListRow({ id: 'USR_before' }))
      break
    case 'bad cursor':
      p.next_cursor = 'usr_other'
      break
    case 'role duplicate':
      r.role_ids = ['rol_reader', 'rol_reader']
      break
    case 'invalid timezone':
      r.personal.usage!.time_zone = 'Local'
      break
    case 'absent policy fiction':
      r.personal_policy_stored = false
      break
    case 'foreign account':
      r.personal.account_id = 'user_usr_peer'
      break
    case 'offboarded application':
      r.offboarded_at = r.updated_at
      r.personal.runtime_applied = true
      break
  }
  expect(() => validateMemberListPage(p, 'usr_admin')).toThrow()
})
it.each(['not_authorized', 'overflow', 'unavailable'] as const)(
  'preserves %s as unavailable Team facts, not empty names',
  (status) => {
    const p = memberListPage()
    p.items[0].teams = { status, items: null }
    expect(validateMemberListPage(p, 'usr_admin').items[0].teams.items).toBeNull()
  },
)
it('preserves explicit zero and missing policy without effective/default fiction', () => {
  const p = memberListPage(),
    r = p.items[0]
  r.total_personal_keys = '0'
  r.personal.tokens_month = '0'
  r.personal.money_month = '0'
  r.teams = { status: 'available', items: [] }
  expect(validateMemberListPage(p, 'usr_admin')).toEqual(p)
  r.personal_policy_stored = false
  r.personal.tokens_month = null
  r.personal.money_month = null
  r.personal.currency = null
  r.personal.policy_etag = '0'
  r.personal.usage_status = 'unavailable'
  r.personal.usage = null
  r.personal.active_reservations = null
  expect(validateMemberListPage(p, 'usr_admin')).toEqual(p)
})
it('validates exact ASCII byte ordering and rejects repeats/early terminal chains', () => {
  const a = memberListPage('usr_admin', [memberListRow({ id: 'LegacyA' })])
  a.next_cursor = 'LegacyA'
  const b = memberListPage('usr_admin', [memberListRow({ id: 'Legacy_a' })])
  expect(validateMemberListChain([a, b], 'usr_admin')).toEqual([a, b])
  expect(() => validateMemberListChain([a, a], 'usr_admin')).toThrow()
  a.next_cursor = null
  expect(() => validateMemberListChain([a, b], 'usr_admin')).toThrow()
})
it('rejects oversized page/role/Team budgets without accepting truncation', () => {
  const p = memberListPage()
  p.items = Array.from({ length: 101 }, (_, n) =>
    memberListRow({ id: `usr_${String(n).padStart(3, '0')}` }),
  )
  expect(() => validateMemberListPage(p, 'usr_admin')).toThrow()
  p.items = [memberListRow()]
  p.items[0].role_ids = Array.from({ length: 10001 }, (_, n) => `rol_${n}`)
  expect(() => validateMemberListPage(p, 'usr_admin')).toThrow()
  p.items[0].role_ids = []
  p.items[0].teams = {
    status: 'available',
    items: Array.from({ length: 1001 }, (_, n) => ({
      id: `tea_${n}`,
      name: 'Retained',
      status: 'active',
      membership_status: 'active',
      membership_role: 'member',
    })),
  }
  expect(() => validateMemberListPage(p, 'usr_admin')).toThrow()
})
it('discards an aborted late response', async () => {
  const c = new AbortController()
  vi.spyOn(client, 'get').mockImplementation(async () => {
    c.abort()
    return { data: memberListPage() }
  })
  await expect(getMemberList({}, 'usr_admin', null, c.signal)).rejects.toThrow('cancelled')
})

it('accepts the supported100-by100 role page and retained historical per-target cardinality above100 without truncation', () => {
  const p = memberListPage()
  p.items = Array.from({ length: 100 }, (_, n) => {
    const row = memberListRow({ id: `usr_${String(n).padStart(3, '0')}` })
    row.teams = { status: 'available', items: [] }
    row.role_ids = Array.from({ length: 100 }, (_, r) => `rol_${r}`)
    return row
  })
  expect(
    validateMemberListPage(p, 'usr_admin').items.reduce((n, r) => n + r.role_ids.length, 0),
  ).toBe(10000)
  p.items = [memberListRow()]
  p.items[0].role_ids = Array.from({ length: 101 }, (_, n) => `rol_${n}`)
  expect(validateMemberListPage(p, 'usr_admin').items[0].role_ids).toHaveLength(101)
})
it.each(['usr_admin ', '', 'x'.repeat(31)])(
  'does not dispatch an unsafe captured actor %s',
  async (actor) => {
    const get = vi.spyOn(client, 'get')
    await expect(getMemberList({}, actor, null)).rejects.toThrow()
    expect(get).not.toHaveBeenCalled()
  },
)

it('accepts the server-owned Personal account identifier', () => {
  const page = memberListPage()
  page.items[0].personal.account_id = `user_${page.items[0].id}`
  expect(validateMemberListPage(page, 'usr_admin')).toEqual(page)
})
it('accepts recorded RFC3339 timezone offsets without rewriting timestamps', () => {
  const page = memberListPage()
  page.observed_at = '2026-10-05T09:00:00+08:00'
  page.items[0].created_at = '2026-09-23T08:00:00+08:00'
  page.items[0].updated_at = '2026-10-04T08:00:00+08:00'
  expect(validateMemberListPage(page, 'usr_admin')).toEqual(page)
})
it.each(['Legacy\nlabel', '', ' retained '])(
  'preserves bounded recorded member name %j',
  (name) => {
    const page = memberListPage()
    page.items[0].name = name
    expect(validateMemberListPage(page, 'usr_admin').items[0].name).toBe(name)
  },
)

it.each(['user:usr_target', 'project_usr_target'])(
  'rejects a non-Personal account namespace %s',
  (account) => {
    const page = memberListPage()
    page.items[0].personal.account_id = account
    expect(() => validateMemberListPage(page, 'usr_admin')).toThrow()
  },
)
it('rejects a recorded member name beyond the response bound', () => {
  const page = memberListPage()
  page.items[0].name = '名'.repeat(101)
  expect(() => validateMemberListPage(page, 'usr_admin')).toThrow()
})
