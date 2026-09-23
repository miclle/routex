export interface OffboardingPerson {
  user_id: string
  name: string
  disabled: boolean
  role: string
  status: string
}
export interface OffboardingResource {
  id: string
  name: string
  status: string
  requires_successor: boolean
  people: OffboardingPerson[]
}
export interface OffboardingAssignments {
  project_assignments: { project_id: string; manager_user_ids: string[] }[]
  team_assignments: { team_id: string; owner_user_ids: string[]; add_member_user_ids: string[] }[]
}
export interface OffboardingCase {
  id: string
  request_id: string
  user_id: string
  actor_id: string
  mode: 'planned' | 'emergency'
  status: 'ready_to_complete' | 'completed'
  reason: string
  planned_at: string | null
  created_at: string
  completed_at: string | null
  completed_by: string
  inventory_version: string
  assignments: OffboardingAssignments
}
export interface OffboardingInventory {
  user_id: string
  offboarded_at: string | null
  disabled: boolean
  last_administrator: boolean
  inventory_version: string
  personal_keys: { id: string; name: string; prefix: string; status: string }[]
  projects: OffboardingResource[]
  teams: OffboardingResource[]
  cases: OffboardingCase[]
}
export interface OffboardingPlan extends OffboardingAssignments {
  request_id: string
  inventory_version: string
  planned_at: string
  reason: string
}
export interface EmergencyOffboarding {
  request_id: string
  reason: string
  team_assignments: OffboardingAssignments['team_assignments']
}
