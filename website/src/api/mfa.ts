import axios from 'axios'
import client from './client'
import type { Session } from '@/types/auth'
import type { MFAEnrollment, MFAProof, MFARecovery, MFAStatus } from '@/types/mfa'

export class MFARequestError extends Error {
  constructor(public readonly status: number) {
    super('MFA request failed')
  }
}
// Proof failures are local authentication failures, not proof that the session expired.
// Only retain a status, never an Axios error containing the submitted credential payload.
async function request<T>(
  method: 'post' | 'delete',
  path: string,
  data: unknown,
  csrf?: string,
  signal?: AbortSignal,
): Promise<T> {
  try {
    const response = await client.request<T>({
      method,
      url: path,
      data,
      signal,
      headers: csrf ? { 'X-CSRF-Token': csrf } : undefined,
      validateStatus: (status) => status < 500,
    })
    if (response.status >= 400) throw new MFARequestError(response.status)
    if (method === 'post' && response.status !== 200) throw new MFARequestError(0)
    return response.data
  } catch (error) {
    if (error instanceof MFARequestError) throw error
    throw new MFARequestError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
export async function getMFAStatus(signal?: AbortSignal) {
  return (await client.get<MFAStatus>('/account/mfa', { signal })).data
}
export function beginMFAEnrollment(current_password: string, csrf: string) {
  return request<MFAEnrollment>('post', '/account/mfa/enrollment', { current_password }, csrf)
}
export function cancelMFAEnrollment(csrf: string) {
  return request<void>('delete', '/account/mfa/enrollment', undefined, csrf)
}
export function enableMFA(
  input: { current_password: string; enrollment_token: string; code: string },
  csrf: string,
) {
  return request<MFARecovery>('post', '/account/mfa/enable', input, csrf)
}
export function changeMFA(
  kind: 'disable',
  input: MFAProof & { current_password: string },
  csrf: string,
): Promise<Session>
export function changeMFA(
  kind: 'recovery-codes',
  input: MFAProof & { current_password: string },
  csrf: string,
): Promise<MFARecovery>
export function changeMFA(
  kind: 'disable' | 'recovery-codes',
  input: MFAProof & { current_password: string },
  csrf: string,
) {
  return request<Session | MFARecovery>('post', `/account/mfa/${kind}`, input, csrf)
}
export function verifyMFALogin(challenge_token: string, proof: MFAProof, signal?: AbortSignal) {
  return request<Session>(
    'post',
    '/auth/mfa/verify',
    { challenge_token, ...proof },
    undefined,
    signal,
  )
}
