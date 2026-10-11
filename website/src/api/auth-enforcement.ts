import axios, { AxiosHeaders, type AxiosResponse } from 'axios'
import client from './client'
import { validApprovalReason } from './registration-approval'
import type { Session, LoginInput } from '@/types/auth'
import type { MFAChallenge, MFAProof } from '@/types/mfa'
import type {
  AuthenticationPolicy,
  AuthenticationPolicyReview,
  AuthenticationPolicyInput,
  AuthenticationPolicyReceipt,
  SecuritySessionResult,
} from '@/types/auth-enforcement'
export class AuthenticationPolicyError extends Error {
  constructor(public readonly status: number) {
    super('Authentication policy request failed')
  }
}
const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const fields = (v: Record<string, unknown>, names: string[]) =>
  Object.keys(v).length === names.length && names.every((n) => Object.hasOwn(v, n))
const etag = (v: unknown): v is string => typeof v === 'string' && /^[a-f0-9]{64}$/.test(v)
const id = (v: unknown): v is string =>
  typeof v === 'string' && /^usr_[0-7][0-9a-hjkmnp-tv-z]{25}$(?![\s\S])/.test(v)
const text = (v: unknown, max: number): v is string =>
  typeof v === 'string' && !/[\p{Cc}\p{Cs}]/u.test(v) && new TextEncoder().encode(v).length <= max
const instant = (v: unknown): v is string =>
  typeof v === 'string' &&
  /^(?!000[01]-)\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,6})?Z$/.test(v) &&
  Number.isFinite(Date.parse(v)) &&
  new Date(v).toISOString().slice(0, 19) === v.slice(0, 19)
const invalid = () => new AuthenticationPolicyError(0)
export function decodeAuthenticationPolicy(v: unknown): AuthenticationPolicy {
  if (
    !object(v) ||
    !fields(v, ['enforced', 'password_login_enabled', 'emergency_entry_enabled']) ||
    typeof v.enforced !== 'boolean' ||
    v.password_login_enabled !== !v.enforced ||
    v.emergency_entry_enabled !== v.enforced
  )
    throw invalid()
  return {
    enforced: v.enforced,
    password_login_enabled: !v.enforced,
    emergency_entry_enabled: v.enforced,
  }
}
function review(
  v: unknown,
  receipt = false,
): AuthenticationPolicyReview | AuthenticationPolicyReceipt {
  if (
    !object(v) ||
    !fields(v, [
      'enforced',
      'emergency_admin_id',
      'emergency_admin_created_at',
      'emergency_mfa_enabled',
      'oidc_available',
      'ldap_available',
      'review_etag',
      'review_emergency_admin',
      ...(receipt ? ['confirmation', 'runtime_applied', 'reauthentication_required'] : []),
    ]) ||
    typeof v.enforced !== 'boolean' ||
    !etag(v.review_etag) ||
    typeof v.oidc_available !== 'boolean' ||
    typeof v.ldap_available !== 'boolean' ||
    !(v.emergency_mfa_enabled === null || typeof v.emergency_mfa_enabled === 'boolean') ||
    (v.enforced
      ? !id(v.emergency_admin_id) || !instant(v.emergency_admin_created_at)
      : v.emergency_admin_id !== null || v.emergency_admin_created_at !== null) ||
    (receipt &&
      (v.confirmation !== 'saved' ||
        typeof v.runtime_applied !== 'boolean' ||
        typeof v.reauthentication_required !== 'boolean'))
  )
    throw invalid()
  const candidate = v.review_emergency_admin
  if (
    candidate !== null &&
    (!object(candidate) ||
      !fields(candidate, ['id', 'created_at', 'mfa_enabled']) ||
      !id(candidate.id) ||
      !instant(candidate.created_at) ||
      typeof candidate.mfa_enabled !== 'boolean')
  )
    throw invalid()
  return { ...v } as unknown as AuthenticationPolicyReview | AuthenticationPolicyReceipt
}
export function decodeSecuritySession(v: unknown): SecuritySessionResult {
  if (object(v) && fields(v, ['reauthentication_required']) && v.reauthentication_required === true)
    return { reauthentication_required: true }
  if (
    !object(v) ||
    !fields(v, ['user', 'csrf_token']) ||
    !object(v.user) ||
    !fields(v.user, ['id', 'email', 'name', 'role']) ||
    !text(v.user.id, 30) ||
    !v.user.id ||
    !text(v.user.email, 254) ||
    !text(v.user.name, 65536) ||
    !['admin', 'member'].includes(v.user.role as string) ||
    !text(v.csrf_token, 256) ||
    !v.csrf_token
  )
    throw invalid()
  return v as unknown as Session
}
function header(r: AxiosResponse, name: string) {
  return r.headers instanceof AxiosHeaders ? r.headers.get(name) : r.headers[name.toLowerCase()]
}
function privateSuccess(r: AxiosResponse) {
  return (
    ['private', 'no-store'].every((expected) =>
      String(header(r, 'Cache-Control') ?? '')
        .split(',')
        .some((v) => v.trim().toLowerCase() === expected),
    ) && header(r, 'X-Content-Type-Options') === 'nosniff'
  )
}
async function request(
  method: 'get' | 'post' | 'put',
  url: string,
  data?: unknown,
  csrf?: string,
  token?: string,
  signal?: AbortSignal,
) {
  try {
    const r = await client.request({
      method,
      url,
      data,
      signal,
      validateStatus: () => true,
      headers: {
        ...(csrf ? { 'X-CSRF-Token': csrf } : {}),
        ...(token ? { 'If-Match': `"${token}"` } : {}),
      },
    })
    if (signal?.aborted) throw invalid()
    if (r.status >= 400) throw new AuthenticationPolicyError(r.status)
    if (!privateSuccess(r)) throw invalid()
    return r
  } catch (e) {
    if (e instanceof AuthenticationPolicyError) throw e
    throw new AuthenticationPolicyError(axios.isAxiosError(e) ? (e.response?.status ?? 0) : 0)
  }
}
export async function getAuthenticationPolicy(signal?: AbortSignal) {
  const r = await request('get', '/auth/policy', undefined, undefined, undefined, signal)
  if (r.status !== 200) throw invalid()
  return decodeAuthenticationPolicy(r.data)
}
export async function getAuthenticationPolicyReview(signal?: AbortSignal, candidate?: string) {
  if (candidate !== undefined && !id(candidate)) throw invalid()
  const r = await request(
    'get',
    `/admin/auth/policy${candidate ? `?emergency_admin_id=${encodeURIComponent(candidate)}` : ''}`,
    undefined,
    undefined,
    undefined,
    signal,
  )
  const v = review(r.data)
  if (
    r.status !== 200 ||
    header(r, 'ETag') !== `"${v.review_etag}"` ||
    (candidate ? v.review_emergency_admin?.id !== candidate : v.review_emergency_admin !== null)
  )
    throw invalid()
  return v as AuthenticationPolicyReview
}
export async function saveAuthenticationPolicy(
  input: AuthenticationPolicyInput,
  token: string,
  csrf: string,
  signal?: AbortSignal,
) {
  if (
    !object(input) ||
    !fields(input, ['enforced', 'emergency_admin_id', 'reason']) ||
    typeof input.enforced !== 'boolean' ||
    (input.enforced ? !id(input.emergency_admin_id) : input.emergency_admin_id !== '') ||
    !validApprovalReason(input.reason) ||
    !etag(token) ||
    !csrf
  )
    throw invalid()
  const r = await request('put', '/admin/auth/policy', { ...input }, csrf, token, signal)
  const v = review(r.data, true) as AuthenticationPolicyReceipt
  if (
    r.status !== 200 ||
    header(r, 'ETag') !== `"${v.review_etag}"` ||
    v.enforced !== input.enforced ||
    v.emergency_admin_id !== (input.enforced ? input.emergency_admin_id : null) ||
    (input.enforced
      ? v.review_emergency_admin?.id !== input.emergency_admin_id
      : v.review_emergency_admin !== null)
  )
    throw invalid()
  return v
}
export async function beginEmergencyLogin(
  input: LoginInput,
  signal?: AbortSignal,
): Promise<MFAChallenge> {
  if (
    !object(input) ||
    !fields(input, ['email', 'password']) ||
    !text(input.email, 254) ||
    !input.email ||
    typeof input.password !== 'string' ||
    /\p{Cs}/u.test(input.password) ||
    new TextEncoder().encode(input.password).length < 12 ||
    new TextEncoder().encode(input.password).length > 72
  )
    throw invalid()
  const r = await request(
    'post',
    '/auth/emergency/login',
    { ...input },
    undefined,
    undefined,
    signal,
  )
  const v: unknown = r.data
  if (
    r.status !== 202 ||
    !object(v) ||
    !fields(v, ['mfa_required', 'challenge_token', 'expires_at', 'methods']) ||
    v.mfa_required !== true ||
    typeof v.challenge_token !== 'string' ||
    !/^[A-Za-z0-9_-]{43}$/.test(v.challenge_token) ||
    !instant(v.expires_at) ||
    Date.parse(v.expires_at) <= Date.now() ||
    !Array.isArray(v.methods) ||
    v.methods.length !== 2 ||
    v.methods[0] !== 'totp' ||
    v.methods[1] !== 'recovery_code'
  )
    throw invalid()
  return {
    mfa_required: true,
    challenge_token: v.challenge_token,
    expires_at: v.expires_at,
    methods: ['totp', 'recovery_code'],
  }
}
export async function completeEmergencyLogin(
  challenge_token: string,
  proof: MFAProof,
  signal?: AbortSignal,
): Promise<Session> {
  if (
    !/^[A-Za-z0-9_-]{43}$/.test(challenge_token) ||
    !object(proof) ||
    !(
      (fields(proof, ['code']) && typeof proof.code === 'string' && /^\d{6}$/.test(proof.code)) ||
      (fields(proof, ['recovery_code']) &&
        typeof proof.recovery_code === 'string' &&
        proof.recovery_code.length > 0 &&
        proof.recovery_code.length <= 128 &&
        text(proof.recovery_code, 128))
    )
  )
    throw invalid()
  const r = await request(
    'post',
    '/auth/emergency/mfa/verify',
    { challenge_token, ...proof },
    undefined,
    undefined,
    signal,
  )
  if (r.status !== 200) throw invalid()
  const v = decodeSecuritySession(r.data)
  if ('reauthentication_required' in v) throw invalid()
  return v
}
