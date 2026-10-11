import axios from 'axios'
import { decodeSecuritySession, AuthenticationPolicyError } from './auth-enforcement'
import client from './client'
import type { User } from '@/types/auth'
import type { AccountSession } from '@/types/account'

export async function updateAccount(name: string, csrfToken: string) {
  return (
    await client.patch<User>('/account', { name }, { headers: { 'X-CSRF-Token': csrfToken } })
  ).data
}
export async function changePassword(
  input: { current_password: string; new_password: string },
  csrfToken: string,
  signal?: AbortSignal,
) {
  try {
    const response = await client.post(
      '/account/password',
      { ...input },
      {
        signal,
        headers: { 'X-CSRF-Token': csrfToken },
        validateStatus: () => true,
      },
    )
    if (signal?.aborted || response.status !== 200)
      throw new AuthenticationPolicyError(response.status >= 400 ? response.status : 0)
    return decodeSecuritySession(response.data)
  } catch (error) {
    if (error instanceof AuthenticationPolicyError) throw error
    throw new AuthenticationPolicyError(
      axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0,
    )
  }
}
export async function listAccountSessions(signal?: AbortSignal) {
  return (await client.get<{ items: AccountSession[] }>('/account/sessions', { signal })).data.items
}
export async function revokeAccountSession(id: string, csrfToken: string) {
  await client.delete(`/account/sessions/${encodeURIComponent(id)}`, {
    headers: { 'X-CSRF-Token': csrfToken },
  })
}
