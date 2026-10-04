import { afterEach, expect, it, vi } from 'vitest'
import client from './client'
import { getMemberTeams, validateMemberTeamsPage, validMemberTeamsCursor } from './member-teams'
import {
  memberTeamsPage,
  memberTeamsTarget,
  memberTeamRow,
  emptyMemberTeamPolicy,
} from '@/views/governance/member-teams.fixture'
afterEach(() => vi.restoreAllMocks())
it('reads only the exact member page with a bounded limit and signal, preserving precise facts', async () => {
  const page = memberTeamsPage(),
    signal = new AbortController().signal
  const get = vi.spyOn(client, 'get').mockResolvedValue({ data: page })
  expect(await getMemberTeams(memberTeamsTarget, null, signal)).toEqual(page)
  expect(get).toHaveBeenCalledWith(`/admin/members/${memberTeamsTarget}/teams`, {
    params: { limit: 20 },
    signal,
  })
})
it.each([
  'foreign target',
  'case Team',
  'space relationship',
  'future join',
  'invalid join',
  'numeric tokens',
  'negative money',
  'currency mismatch',
  'missing live holds',
  'inactive usage',
  'invalid status',
  'duplicate Team',
  'duplicate membership',
  'oversized',
  'invalid cursor',
  'absent record with policy',
  'invalid timezone',
])('rejects %s without coercion', (kind) => {
  const p = memberTeamsPage(),
    r = p.items[0]
  switch (kind) {
    case 'foreign target':
      p.user_id = 'usr_01dddddddddddddddddddddddd'
      break
    case 'case Team':
      r.id = r.id.toUpperCase()
      break
    case 'space relationship':
      r.membership_id += ' '
      break
    case 'future join':
      r.joined_at = '2027-01-01T00:00:00Z'
      break
    case 'invalid join':
      r.joined_at = 'bad'
      break
    case 'numeric tokens':
      ;(r.limits.stored as unknown as Record<string, unknown>).tokens_month = 0
      break
    case 'negative money':
      r.limits.usage!.money_used.USD = '-1'
      break
    case 'currency mismatch':
      r.limits.stored.currency = null
      break
    case 'missing live holds':
      r.limits.active_reservations = null
      break
    case 'inactive usage':
      r.limits.usage_status = 'inactive'
      break
    case 'invalid status':
      ;(r as unknown as Record<string, unknown>).status = 'missing'
      break
    case 'duplicate Team':
      p.items.push({ ...r, membership_id: memberTeamRow('d').membership_id })
      break
    case 'duplicate membership':
      p.items.push({ ...r, id: memberTeamRow('d').id })
      break
    case 'oversized':
      p.items = Array(21).fill(r)
      break
    case 'invalid cursor':
      p.next_cursor = 'bad='
      break
    case 'absent record with policy':
      r.limits.policy_recorded = false
      break
    case 'invalid timezone':
      r.limits.usage!.time_zone = 'Local'
      break
  }
  expect(() => validateMemberTeamsPage(p, memberTeamsTarget)).toThrow()
})
it('preserves absent policy, inactive journal and historical unknown joins', () => {
  const p = memberTeamsPage(),
    l = p.items[0].limits
  l.policy_recorded = false
  l.policy_etag = '0'
  l.stored = emptyMemberTeamPolicy()
  l.usage_status = 'inactive'
  l.usage = null
  l.active_reservations = null
  expect(validateMemberTeamsPage(p, memberTeamsTarget)).toEqual(p)
})
it('rejects overlapping rows and cursor loops across pages', () => {
  const p = memberTeamsPage()
  p.next_cursor = btoa('canonical')
  expect(validMemberTeamsCursor(p.next_cursor)).toBe(true)
  expect(() => validateMemberTeamsPage(memberTeamsPage(), memberTeamsTarget, [p])).toThrow()
  const next = memberTeamsPage([memberTeamRow('d')])
  next.next_cursor = p.next_cursor
  expect(() => validateMemberTeamsPage(next, memberTeamsTarget, [p])).toThrow()
})
it.each([memberTeamsTarget.toUpperCase(), memberTeamsTarget + ' ', 'usr_bad'])(
  'does not dispatch unsafe target %s',
  async (target) => {
    const get = vi.spyOn(client, 'get')
    await expect(getMemberTeams(target, null)).rejects.toThrow()
    expect(get).not.toHaveBeenCalled()
  },
)
