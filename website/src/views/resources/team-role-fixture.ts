import type { TeamRole, TeamRoles } from '@/types/team-roles'
export const roleFixture = (id = 'rol_editor'): TeamRole => ({
  id,
  name: id === 'rol_editor' ? 'Team editor' : 'Model maintainer',
  builtin: false,
  team_actions: id === 'rol_editor' ? ['teams.write'] : ['teams.models.write'],
})
export const teamRolesFixture = (): TeamRoles => ({
  team_id: 'tea_one',
  role_ids: ['rol_editor'],
  roles: [roleFixture()],
  effective_team_actions: ['teams.write'],
  actor_team_actions: ['teams.write'],
  can_assign_roles: false,
  etag: 'a'.repeat(64),
})
