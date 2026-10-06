import client from './client'
import { validRecordedRoleDescription } from './role-definition'
import type { Member, MemberFilters, MemberList, RoleList } from '@/types/governance'
export { registerLocal as register } from './registration-approval'
import { getRegistrationPolicy, getRegistrationPolicyReview } from './registration-approval'
export async function getPermissions(signal?: AbortSignal) {
  return (await client.get<{ permissions: string[] }>('/auth/permissions', { signal })).data
    .permissions
}
export function getRegistration(admin?: false): ReturnType<typeof getRegistrationPolicy>
export function getRegistration(admin: true): ReturnType<typeof getRegistrationPolicyReview>
export function getRegistration(admin = false) {
  return admin ? getRegistrationPolicyReview() : getRegistrationPolicy()
}
export async function getMembers(
  filters: MemberFilters,
  cursor: string | null,
  signal?: AbortSignal,
) {
  return (
    await client.get<MemberList>('/admin/members', {
      params: { ...filters, cursor: cursor || undefined },
      signal,
    })
  ).data
}
export async function getMember(id: string, signal?: AbortSignal) {
  return (await client.get<Member>(`/admin/members/${id}`, { signal })).data
}
export async function getRoles(signal?: AbortSignal): Promise<RoleList> {
  const data = (await client.get<RoleList>('/admin/roles', { signal })).data
  if (data.items.some((role) => !validRecordedRoleDescription(role.description)))
    throw new Error('Invalid recorded Role description')
  return {
    ...data,
    items: data.items.map((role) => ({
      ...role,
      description: role.description ?? '',
      member_count:
        typeof role.member_count === 'number' &&
        Number.isSafeInteger(role.member_count) &&
        role.member_count >= 0
          ? role.member_count
          : null,
    })),
  }
}
