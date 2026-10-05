import type { MemberAccessAuthority, MemberAccessSummary } from '@/types/member-access-summary'

export function accessSummaryFixture(
  userID = 'usr_target',
  authority: MemberAccessAuthority = { roles: true, teams: true },
): MemberAccessSummary {
  return {
    user_id: userID,
    observed_at: '2026-10-05T02:00:00.123456789Z',
    updated_at: '2026-10-03T03:04:05.123456Z',
    identity_role: 'member',
    roles: authority.roles
      ? {
          status: 'available',
          items: [{ id: 'rol_selected', name: 'Recorded role', builtin: false }],
        }
      : { status: 'not_authorized', items: null },
    teams: authority.teams
      ? {
          status: 'available',
          items: [
            {
              id: 'tea_active',
              name: 'Recorded Team',
              status: 'active',
              membership_status: 'active',
              membership_role: 'member',
            },
            {
              id: 'tea_retained',
              name: 'Retained Team',
              status: 'archived',
              membership_status: 'disabled',
              membership_role: 'owner',
            },
          ],
        }
      : { status: 'not_authorized', items: null },
  }
}
