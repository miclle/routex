import type { MemberRoleSummary, MemberRolesWorkspace } from '@/types/member-roles'

export const roleReviewETag = 'a'.repeat(64)
export const roleDefinitionETag = 'b'.repeat(64)
export function roleSummary(id = 'rol_custom', name = 'Recorded custom role'): MemberRoleSummary {
  return {
    id,
    name,
    builtin: false,
    assignment_kind: 'explicit',
    permission_count: 1,
    definition_etag: roleDefinitionETag,
  }
}
export function rolesWorkspace(target = 'usr_target'): MemberRolesWorkspace {
  return {
    user_id: target,
    observed_at: '2026-10-05T03:04:05.123456789Z',
    identity_role: 'member',
    subject_status: 'active',
    builtin_role: {
      ...roleSummary('rol_member', 'Member'),
      builtin: true,
      assignment_kind: 'intrinsic',
      permission_count: 0,
    },
    assigned_roles: [roleSummary()],
    effective_permissions: ['providers.read'],
    permission_use: 'active',
    etag: roleReviewETag,
    can_edit: true,
    edit_blockers: [],
    candidate_status: 'available',
  }
}
