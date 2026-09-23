import client from './client'
import type { Member, MemberFilters, MemberList, RoleList } from '@/types/governance'
import type { Session, SetupInput } from '@/types/auth'
export async function getPermissions(signal?: AbortSignal) { return (await client.get<{ permissions: string[] }>('/auth/permissions', { signal })).data.permissions }
export async function getRegistration(admin = false) { return (await client.get<{ enabled: boolean }>(admin ? '/admin/registration' : '/auth/registration')).data }
export async function register(input: SetupInput) { return (await client.post<Session>('/auth/register', input)).data }
export async function getMembers(filters: MemberFilters, cursor: string | null, signal?: AbortSignal) { return (await client.get<MemberList>('/admin/members', { params: { ...filters, cursor: cursor || undefined }, signal })).data }
export async function getMember(id: string, signal?: AbortSignal) { return (await client.get<Member>(`/admin/members/${id}`, { signal })).data }
export async function getRoles(signal?: AbortSignal) { return (await client.get<RoleList>('/admin/roles', { signal })).data }
