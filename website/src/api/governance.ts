import client from './client'
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
export async function getRoles(signal?: AbortSignal) {
  return (await client.get<RoleList>('/admin/roles', { signal })).data
}
