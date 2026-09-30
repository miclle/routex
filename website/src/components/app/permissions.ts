export type PermissionRequirement = string | readonly string[]

export function allowsPermission(
  requirement: PermissionRequirement,
  can: (permission: string) => boolean,
) {
  return typeof requirement === 'string' ? can(requirement) : requirement.some(can)
}
