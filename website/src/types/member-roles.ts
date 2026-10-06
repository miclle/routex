import type { RoleAssignmentKind } from './governance'

export interface MemberRoleSummary {
  id: string
  name: string
  builtin: boolean
  assignment_kind: RoleAssignmentKind
  permission_count: number
  definition_etag: string
}
export type MemberRoleBlocker =
  | 'not_platform_admin'
  | 'offboarded'
  | 'assignment_audit_bound'
  | 'candidate_catalogue_bound'
  | 'definition_unavailable'
export interface MemberRolesWorkspace {
  user_id: string
  observed_at: string
  identity_role: 'admin' | 'member'
  subject_status: 'active' | 'disabled' | 'offboarded'
  builtin_role: MemberRoleSummary
  assigned_roles: MemberRoleSummary[]
  effective_permissions: string[]
  permission_use: 'active' | 'inactive'
  etag: string
  can_edit: boolean
  edit_blockers: MemberRoleBlocker[]
  candidate_status: 'available' | 'not_authorized' | 'overflow' | 'unavailable'
}
export interface MemberRoleCandidatePage {
  items: MemberRoleSummary[]
  next_cursor: string | null
  etag: string
}
export interface MemberRoleDetail {
  user_id: string
  role: MemberRoleSummary
  permissions: string[]
  etag: string
}
export interface MemberRolesInput {
  role_ids: string[]
  role_definitions: { id: string; etag: string }[]
  builtin_definition_etag: string
  reason: string
}
export interface MemberRolesResult {
  user_id: string
  role_ids: string[]
  etag: string
  confirmation: 'current_member_roles'
  effect: 'current_database'
}
