import client from './client'
import type { Session, User } from '@/types/auth'
import type { AccountSession } from '@/types/account'

export async function updateAccount(name: string, csrfToken: string) {
  return (await client.patch<User>('/account', { name }, { headers: { 'X-CSRF-Token': csrfToken } })).data
}
export async function changePassword(input: { current_password: string; new_password: string }, csrfToken: string) {
  return (await client.post<Session>('/account/password', input, { headers: { 'X-CSRF-Token': csrfToken } })).data
}
export async function listAccountSessions(signal?: AbortSignal) {
  return (await client.get<{ items: AccountSession[] }>('/account/sessions', { signal })).data.items
}
export async function revokeAccountSession(id: string, csrfToken: string) {
  await client.delete(`/account/sessions/${encodeURIComponent(id)}`, { headers: { 'X-CSRF-Token': csrfToken } })
}
