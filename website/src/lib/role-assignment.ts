import type { RoleAssignmentKind } from '@/types/governance'

const intrinsic = ['rol_admin', 'rol_member'] as const
const dutyNames = {
  rol_procurement: 'common.procurement',
  rol_finance: 'common.finance',
  rol_operations: 'common.operations',
} as const

type ClassifiedRole = { id?: unknown; builtin?: unknown; assignment_kind?: unknown }
export function validRoleAssignment(role: ClassifiedRole): boolean {
  if (typeof role.id !== 'string' || typeof role.builtin !== 'boolean') return false
  if (intrinsic.some((id) => id === role.id))
    return role.builtin && role.assignment_kind === 'intrinsic'
  if (Object.hasOwn(dutyNames, role.id)) return role.builtin && role.assignment_kind === 'explicit'
  return !role.builtin && role.assignment_kind === 'explicit'
}

export function builtinRoleNameKey(role: {
  id: string
  builtin: boolean
  assignment_kind: RoleAssignmentKind
}) {
  if (!validRoleAssignment(role) || !role.builtin) return null
  if (role.id === 'rol_admin') return 'common.admin' as const
  if (role.id === 'rol_member') return 'common.member' as const
  return dutyNames[role.id as keyof typeof dutyNames] ?? null
}
