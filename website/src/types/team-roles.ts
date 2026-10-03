export type TeamAction = 'teams.write' | 'teams.models.write'
export interface TeamRole {
  id: string
  name: string
  builtin: boolean
  team_actions: TeamAction[]
}
export interface TeamRoles {
  team_id: string
  role_ids: string[]
  roles: TeamRole[]
  effective_team_actions: TeamAction[]
  actor_team_actions: TeamAction[]
  etag: string
  can_assign_roles: boolean
}
