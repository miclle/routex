import { decodeSecuritySession } from './auth-enforcement'
import type { SecuritySessionResult } from '@/types/auth-enforcement'
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
    if (signal?.aborted) throw new MFARequestError(0)
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
export function beginMFAEnrollment(current_password: string, csrf: string, signal?: AbortSignal) {
  return request<MFAEnrollment>(
    'post',
    '/account/mfa/enrollment',
    { current_password },
    csrf,
    signal,
  )
}
export function cancelMFAEnrollment(csrf: string, signal?: AbortSignal) {
  return request<void>('delete', '/account/mfa/enrollment', undefined, csrf, signal)
}
export function enableMFA(
  input: { current_password: string; enrollment_token: string; code: string },
  csrf: string,
  signal?: AbortSignal,
) {
  return request<unknown>('post', '/account/mfa/enable', input, csrf, signal).then(
    decodeMFARecovery,
  )
}
export function changeMFA(
  kind: 'disable',
  input: MFAProof & { current_password: string },
  csrf: string,
  signal?: AbortSignal,
): Promise<SecuritySessionResult>
export function changeMFA(
  kind: 'recovery-codes',
  input: MFAProof & { current_password: string },
  csrf: string,
  signal?: AbortSignal,
): Promise<MFARecovery>
export function changeMFA(
  kind: 'disable' | 'recovery-codes',
  input: MFAProof & { current_password: string },
  csrf: string,
  signal?: AbortSignal,
) {
  return request<unknown>('post', `/account/mfa/${kind}`, input, csrf, signal).then((value) =>
    kind === 'disable' ? decodeSecuritySession(value) : decodeMFARecovery(value),
  )
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

export function decodeMFARecovery(value: unknown): MFARecovery {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new MFARequestError(0)
  const v = value as Record<string, unknown>
  const reauthentication = v.reauthentication_required === true
  const keys = reauthentication
    ? ['session', 'recovery_codes', 'reauthentication_required']
    : ['session', 'recovery_codes']
  if (
    Object.keys(v).length !== keys.length ||
    !keys.every((key) => Object.hasOwn(v, key)) ||
    !Array.isArray(v.recovery_codes) ||
    v.recovery_codes.length !== 10 ||
    !v.recovery_codes.every(
      (code) =>
        typeof code === 'string' &&
        code.length > 0 &&
        code.length <= 128 &&
        !/[\p{Cc}\p{Cs}]/u.test(code),
    ) ||
    new Set(v.recovery_codes).size !== v.recovery_codes.length
  )
    throw new MFARequestError(0)
  const codes = [...v.recovery_codes] as string[]
  if (reauthentication) {
    if (v.session !== null) throw new MFARequestError(0)
    return { session: null, recovery_codes: codes, reauthentication_required: true }
  }
  const session = decodeSecuritySession(v.session)
  if ('reauthentication_required' in session) throw new MFARequestError(0)
  return { session, recovery_codes: codes }
}
