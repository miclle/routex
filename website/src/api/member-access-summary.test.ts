import { afterEach, expect, it, vi } from 'vitest'
import client from './client'
import { getMemberAccessSummary, validateMemberAccessSummary } from './member-access-summary'
import { accessSummaryFixture } from '@/views/governance/member-access-summary.fixture'

const flags = { roles: true, teams: true }
afterEach(() => vi.restoreAllMocks())
it('reads exactly the retained resource with one private GET and abort signal', async () => {
  const signal = new AbortController().signal
  const spy = vi.spyOn(client, 'get').mockResolvedValue({
    data: accessSummaryFixture(),
    headers: { 'cache-control': 'private, no-store' },
  })
  expect(await getMemberAccessSummary('usr_target', flags, signal)).toEqual(accessSummaryFixture())
  expect(spy).toHaveBeenCalledExactlyOnceWith('/admin/members/usr_target/access', { signal })
})
it.each(['../target', '', '目标', 'usr_target ', 'a'.repeat(31)])(
  'rejects unsafe target %s before transport',
  async (target) => {
    const spy = vi.spyOn(client, 'get')
    await expect(getMemberAccessSummary(target, flags)).rejects.toThrow()
    expect(spy).not.toHaveBeenCalled()
  },
)
it('preserves safe legacy identities, historical names and complete supported bounds', () => {
  const page = accessSummaryFixture('Legacy-1')
  page.roles = {
    status: 'available',
    items: Array.from({ length: 10000 }, (_, i) => ({
      id: `R${String(i).padStart(5, '0')}`,
      name: i === 0 ? '' : '<b>stored</b>\n\u0000',
      builtin: false,
    })),
  }
  page.teams = {
    status: 'available',
    items: Array.from({ length: 1000 }, (_, i) => ({
      id: `T${String(i).padStart(4, '0')}`,
      name: '名'.repeat(100),
      status: 'active',
      membership_status: 'active',
      membership_role: 'member',
    })),
  }
  expect(validateMemberAccessSummary(page, 'Legacy-1', flags)).toEqual(page)
})
it.each([false, true])('keeps Role and Team disclosure independent, roles:%s', (roles) => {
  const authority = { roles, teams: !roles }
  const page = accessSummaryFixture('usr_target', authority)
  expect(validateMemberAccessSummary(page, 'usr_target', authority)).toEqual(page)
})
it.each(['overflow', 'unavailable'] as const)(
  'retains whole-section %s and known empty distinctly',
  (status) => {
    const page = accessSummaryFixture()
    page.roles = { status, items: null }
    page.teams = { status: 'available', items: [] }
    page.updated_at = null
    expect(validateMemberAccessSummary(page, 'usr_target', flags)).toEqual(page)
  },
)
it('does not infer clock relationships or discard valid year-one nonzero instants', () => {
  const page = accessSummaryFixture()
  page.updated_at = '0001-01-01T00:00:00.000000001Z'
  page.observed_at = '0001-01-01T00:00:01Z'
  expect(validateMemberAccessSummary(page, 'usr_target', flags)).toEqual(page)
  page.updated_at = '2027-01-01T00:00:00Z'
  expect(validateMemberAccessSummary(page, 'usr_target', flags)).toEqual(page)
})
it.each([
  '0001-01-01T00:00:00Z',
  '0001-01-01T00:00:00.000000000Z',
  '0000-01-01T00:00:00Z',
  '2026-02-30T00:00:00Z',
  '2026-10-05T24:00:00Z',
  '2026-10-05T02:00:00+00:00',
  '2026-10-05T02:00:00.1234567890Z',
])('rejects malformed/zero calendar instant %s', (stamp) => {
  const page = accessSummaryFixture()
  page.observed_at = stamp
  expect(() => validateMemberAccessSummary(page, 'usr_target', flags)).toThrow()
  page.observed_at = accessSummaryFixture().observed_at
  page.updated_at = stamp
  expect(() => validateMemberAccessSummary(page, 'usr_target', flags)).toThrow()
})
it.each([
  ['foreign target', (p: any) => (p.user_id = 'usr_peer')],
  ['extra metadata', (p: any) => (p.count = 2)],
  ['missing updated', (p: any) => delete p.updated_at],
  ['invalid identity role', (p: any) => (p.identity_role = 'operator')],
  ['extra Role permissions', (p: any) => (p.roles.items[0].permissions = [])],
  ['extra Role revision', (p: any) => (p.roles.items[0].revision = 'r')],
  ['extra Team relationship', (p: any) => (p.teams.items[0].membership_id = 'tmm_hidden')],
  ['too long historical Role name', (p: any) => (p.roles.items[0].name = '名'.repeat(101))],
  ['invalid UTF16', (p: any) => (p.roles.items[0].name = '\ud800')],
  ['duplicate Role', (p: any) => p.roles.items.push(p.roles.items[0])],
  ['unsorted Teams', (p: any) => p.teams.items.reverse()],
  ['invalid Team lifecycle', (p: any) => (p.teams.items[0].status = 'pending')],
  ['invalid relationship role', (p: any) => (p.teams.items[0].membership_role = 'admin')],
  ['unknown section state', (p: any) => (p.roles.status = 'partial')],
  ['partial overflow', (p: any) => (p.teams.status = 'overflow')],
  ['null available', (p: any) => (p.roles.items = null)],
  [
    'unauthorized under granted authority',
    (p: any) => (p.roles = { status: 'not_authorized', items: null }),
  ],
  [
    'role overflow',
    (p: any) =>
      (p.roles.items = Array.from({ length: 10001 }, (_, i) => ({
        id: `r${i}`,
        name: 'Role',
        builtin: false,
      }))),
  ],
  [
    'team overflow',
    (p: any) =>
      (p.teams.items = Array.from({ length: 1001 }, (_, i) => ({
        id: `t${i}`,
        name: 'Team',
        status: 'active',
        membership_status: 'active',
        membership_role: 'member',
      }))),
  ],
])('rejects %s without partial facts', (_label, change) => {
  const page = accessSummaryFixture()
  change(page)
  expect(() => validateMemberAccessSummary(page, 'usr_target', flags)).toThrow()
})
it.each(['roles', 'teams'] as const)('rejects denied %s names/IDs and hidden counts', (section) => {
  const authority = { roles: false, teams: false }
  const page = accessSummaryFixture('usr_target', authority)
  Object.assign(page[section], { count: 0 })
  expect(() => validateMemberAccessSummary(page, 'usr_target', authority)).toThrow()
  delete (page[section] as any).count
  ;(page[section] as any).items = []
  expect(() => validateMemberAccessSummary(page, 'usr_target', authority)).toThrow()
})
it('rejects cacheable responses, missing headers and preserves HTTP errors', async () => {
  const spy = vi
    .spyOn(client, 'get')
    .mockResolvedValue({ data: accessSummaryFixture(), headers: {} })
  await expect(getMemberAccessSummary('usr_target', flags)).rejects.toThrow('private')
  spy.mockResolvedValue({ data: accessSummaryFixture(), headers: { 'cache-control': 'public' } })
  await expect(getMemberAccessSummary('usr_target', flags)).rejects.toThrow('private')
  spy.mockRejectedValue(new Error('controlled unavailable'))
  await expect(getMemberAccessSummary('usr_target', flags)).rejects.toThrow(
    'controlled unavailable',
  )
})
