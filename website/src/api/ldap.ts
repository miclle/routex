import axios, { AxiosHeaders, type AxiosResponse } from 'axios'
import client from './client'
import { validApprovalReason } from './registration-approval'
import type { Session } from '@/types/auth'
import type {
  LDAPMethod,
  LDAPIdentity,
  LDAPConfig,
  LDAPConfigInput,
  LDAPLocalProof,
  LDAPBindingProof,
  LDAPLoginResult,
} from '@/types/ldap'

export class LDAPRequestError extends Error {
  constructor(public readonly status: number) {
    super('LDAP request unavailable')
  }
}
const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const fields = (value: Record<string, unknown>, keys: string[]) =>
  Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key))
const token = (value: unknown): value is string =>
  typeof value === 'string' && /^[a-f0-9]{64}$/.test(value)
const text = (value: unknown, max: number): value is string =>
  typeof value === 'string' &&
  !/[\p{Cc}\p{Cs}]/u.test(value) &&
  new TextEncoder().encode(value).length <= max
const invalid = () => new LDAPRequestError(0)
const validName = (value: unknown): value is string =>
  text(value, 400) && !!value && value.trim() === value && Array.from(value).length <= 100
function ldaps(value: unknown): value is string {
  if (!text(value, 2048) || !value || /[\s\\]/u.test(value)) return false
  const parts = /^ldaps:\/\/(\[[^\]/%]+\]|[^/:?#@%]+):([1-9][0-9]{0,4})$/.exec(value)
  if (!parts || Number(parts[2]) > 65535) return false
  try {
    const parsed = new URL(value)
    return (
      parsed.protocol === 'ldaps:' &&
      !parsed.username &&
      !parsed.password &&
      !parsed.pathname &&
      !parsed.search &&
      !parsed.hash
    )
  } catch {
    return false
  }
}
function header(response: AxiosResponse, name: string) {
  return response.headers instanceof AxiosHeaders
    ? response.headers.get(name)
    : response.headers[name.toLowerCase()]
}
function reviewed(response: AxiosResponse) {
  return (
    object(response.data) &&
    token(response.data.review_etag) &&
    header(response, 'ETag') === `"${response.data.review_etag}"`
  )
}
async function request(
  method: 'GET' | 'POST' | 'PUT',
  url: string,
  data?: unknown,
  csrf?: string,
  etag?: string,
  signal?: AbortSignal,
  privateResponse = true,
) {
  try {
    const response = await client.request({
      method,
      url,
      data,
      signal,
      headers: {
        ...(csrf ? { 'X-CSRF-Token': csrf } : {}),
        ...(etag ? { 'If-Match': `"${etag}"` } : {}),
      },
      // Sensitive anonymous failures must not enter the shared Session-expiry interceptor.
      validateStatus: () => true,
    })
    if (signal?.aborted) throw invalid()
    if (response.status >= 400) throw new LDAPRequestError(response.status)
    const control = String(header(response, 'Cache-Control') ?? '')
      .toLowerCase()
      .split(',')
      .map((part) => part.trim())
    if (
      (privateResponse && !control.includes('private')) ||
      !control.includes('no-store') ||
      header(response, 'X-Content-Type-Options') !== 'nosniff'
    )
      throw invalid()
    return response
  } catch (error) {
    if (error instanceof LDAPRequestError) throw error
    throw new LDAPRequestError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
function method(value: unknown): value is LDAPMethod {
  return (
    object(value) &&
    fields(value, ['available', 'name']) &&
    typeof value.available === 'boolean' &&
    text(value.name, 400) &&
    (!value.available || validName(value.name))
  )
}
function identity(value: unknown): value is LDAPIdentity {
  return (
    object(value) &&
    fields(value, ['available', 'name', 'bound', 'mfa_required', 'review_etag']) &&
    typeof value.available === 'boolean' &&
    text(value.name, 400) &&
    typeof value.bound === 'boolean' &&
    typeof value.mfa_required === 'boolean' &&
    token(value.review_etag) &&
    (!value.available || validName(value.name))
  )
}
const bytes = (value: string) => new TextEncoder().encode(value).length
const password = (value: unknown, min: number, max: number): value is string =>
  typeof value === 'string' && !/\p{Cs}/u.test(value) && bytes(value) >= min && bytes(value) <= max
const attribute = (value: unknown) => value === 'entryUUID' || value === 'objectGUID'
function configuration(value: Record<string, unknown>) {
  return (
    validName(value.name) &&
    ldaps(value.endpoint) &&
    text(value.bind_dn, 2048) &&
    !!value.bind_dn &&
    text(value.base_dn, 2048) &&
    !!value.base_dn &&
    text(value.user_filter, 4096) &&
    !!value.user_filter &&
    value.user_filter.split('{username}').length === 2 &&
    attribute(value.identity_attribute)
  )
}
function config(value: unknown): value is LDAPConfig {
  if (
    !object(value) ||
    !fields(value, [
      'name',
      'endpoint',
      'bind_dn',
      'base_dn',
      'user_filter',
      'identity_attribute',
      'secret_configured',
      'mfa_required',
      'enabled',
      'verified',
      'review_etag',
    ]) ||
    !['secret_configured', 'mfa_required', 'enabled', 'verified'].every(
      (key) => typeof value[key] === 'boolean',
    ) ||
    !token(value.review_etag)
  )
    return false
  const empty =
    ['name', 'endpoint', 'bind_dn', 'base_dn', 'user_filter', 'identity_attribute'].every(
      (key) => value[key] === '',
    ) &&
    !value.secret_configured &&
    !value.enabled &&
    !value.verified
  return (
    empty ||
    (configuration(value) &&
      (!value.enabled || value.verified === true) &&
      (!value.verified || value.secret_configured === true))
  )
}
export function validLDAPConfig(value: LDAPConfigInput) {
  return (
    object(value) &&
    fields(value, [
      'name',
      'endpoint',
      'bind_dn',
      'base_dn',
      'user_filter',
      'identity_attribute',
      'secret_action',
      'bind_password',
      'reason',
    ]) &&
    configuration(value) &&
    validApprovalReason(value.reason) &&
    (value.secret_action === 'keep'
      ? value.bind_password === ''
      : value.secret_action === 'replace' && password(value.bind_password, 1, 4096))
  )
}
export function copyLDAPInput(value: LDAPConfigInput): LDAPConfigInput {
  return { ...value }
}
export function validLDAPProof(value: LDAPLocalProof, required: boolean) {
  if (
    !object(value) ||
    !fields(value, ['password', 'proof', 'reason']) ||
    !password(value.password, 12, 72) ||
    !validApprovalReason(value.reason) ||
    !object(value.proof)
  )
    return false
  if (!required) return fields(value.proof, [])
  return (
    (fields(value.proof, ['code']) &&
      typeof value.proof.code === 'string' &&
      /^\d{6}$/.test(value.proof.code)) ||
    (fields(value.proof, ['recovery_code']) &&
      text(value.proof.recovery_code, 128) &&
      !!value.proof.recovery_code &&
      value.proof.recovery_code.trim() === value.proof.recovery_code)
  )
}
export function validLDAPLogin(value: { username: string; password: string }) {
  return (
    object(value) &&
    fields(value, ['username', 'password']) &&
    text(value.username, 256) &&
    !!value.username &&
    password(value.password, 1, 4096)
  )
}
export function validLDAPBindingProof(value: LDAPBindingProof, required: boolean) {
  if (
    !object(value) ||
    !fields(value, ['password', 'proof', 'reason', 'username', 'directory_password'])
  )
    return false
  return (
    validLDAPProof(
      { password: value.password, proof: value.proof, reason: value.reason },
      required,
    ) && validLDAPLogin({ username: value.username, password: value.directory_password })
  )
}
function admitted(etag: string, csrf: string) {
  if (!token(etag) || !csrf) throw invalid()
}
export async function getLDAPMethod(signal?: AbortSignal): Promise<LDAPMethod> {
  const response = await request('GET', '/auth/ldap', undefined, undefined, undefined, signal)
  if (response.status !== 200 || !method(response.data)) throw invalid()
  return { ...response.data }
}
export async function getLDAPIdentity(signal?: AbortSignal): Promise<LDAPIdentity> {
  const response = await request(
    'GET',
    '/account/identity/ldap',
    undefined,
    undefined,
    undefined,
    signal,
  )
  if (response.status !== 200 || !reviewed(response) || !identity(response.data)) throw invalid()
  return { ...response.data }
}
export async function getLDAPConfig(signal?: AbortSignal): Promise<LDAPConfig> {
  const response = await request('GET', '/admin/auth/ldap', undefined, undefined, undefined, signal)
  if (response.status !== 200 || !reviewed(response) || !config(response.data)) throw invalid()
  return { ...response.data }
}
export async function saveLDAPConfig(
  etag: string,
  input: LDAPConfigInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<LDAPConfig> {
  admitted(etag, csrf)
  if (!validLDAPConfig(input)) throw invalid()
  const captured = { ...input }
  const response = await request('PUT', '/admin/auth/ldap', captured, csrf, etag, signal)
  if (
    response.status !== 200 ||
    !reviewed(response) ||
    !config(response.data) ||
    !['name', 'endpoint', 'bind_dn', 'base_dn', 'user_filter', 'identity_attribute'].every(
      (key) => response.data[key] === captured[key as keyof LDAPConfigInput],
    ) ||
    !response.data.secret_configured
  )
    throw invalid()
  return { ...response.data }
}
export async function setLDAPStatus(
  etag: string,
  input: { enabled: boolean; reason: string },
  csrf: string,
  signal?: AbortSignal,
): Promise<LDAPConfig> {
  admitted(etag, csrf)
  if (
    !object(input) ||
    !fields(input, ['enabled', 'reason']) ||
    typeof input.enabled !== 'boolean' ||
    !validApprovalReason(input.reason)
  )
    throw invalid()
  const captured = { ...input }
  const response = await request('PUT', '/admin/auth/ldap/status', captured, csrf, etag, signal)
  if (
    response.status !== 200 ||
    !reviewed(response) ||
    !config(response.data) ||
    response.data.enabled !== captured.enabled
  )
    throw invalid()
  return { ...response.data }
}
export async function submitLDAPProof(
  action: 'bind' | 'verify',
  etag: string,
  input: LDAPBindingProof,
  required: boolean,
  csrf: string,
  signal?: AbortSignal,
): Promise<{ kind: 'bound' | 'verified' }> {
  admitted(etag, csrf)
  if (!validLDAPBindingProof(input, required)) throw invalid()
  const response = await request(
    'POST',
    action === 'bind' ? '/account/identity/ldap/bind' : '/admin/auth/ldap/verify',
    { ...input, proof: { ...input.proof } },
    csrf,
    etag,
    signal,
  )
  if (
    response.status !== 200 ||
    !object(response.data) ||
    !fields(response.data, ['kind']) ||
    response.data.kind !== (action === 'bind' ? 'bound' : 'verified')
  )
    throw invalid()
  return { kind: action === 'bind' ? 'bound' : 'verified' }
}
export async function unlinkLDAP(
  etag: string,
  input: LDAPLocalProof,
  required: boolean,
  csrf: string,
  signal?: AbortSignal,
): Promise<LDAPIdentity> {
  admitted(etag, csrf)
  if (!validLDAPProof(input, required)) throw invalid()
  const response = await request(
    'POST',
    '/account/identity/ldap/unlink',
    { ...input, proof: { ...input.proof } },
    csrf,
    etag,
    signal,
  )
  if (
    response.status !== 200 ||
    !reviewed(response) ||
    !identity(response.data) ||
    response.data.bound
  )
    throw invalid()
  return { ...response.data }
}
function session(value: unknown): value is Session {
  return (
    object(value) &&
    fields(value, ['user', 'csrf_token']) &&
    text(value.csrf_token, 4096) &&
    !!value.csrf_token &&
    object(value.user) &&
    fields(value.user, ['id', 'email', 'name', 'role']) &&
    text(value.user.id, 30) &&
    /^usr_[a-zA-Z0-9]+$/.test(value.user.id) &&
    text(value.user.email, 254) &&
    !!value.user.email &&
    text(value.user.name, 65536) &&
    ['admin', 'member'].includes(value.user.role as string)
  )
}
export async function readLDAPSession(signal?: AbortSignal): Promise<Session | null> {
  try {
    const response = await request(
      'GET',
      '/auth/session',
      undefined,
      undefined,
      undefined,
      signal,
      false,
    )
    if (response.status !== 200 || !session(response.data)) throw invalid()
    return { ...response.data, user: { ...response.data.user } }
  } catch (error) {
    if (error instanceof LDAPRequestError && error.status === 401 && !signal?.aborted) return null
    throw error
  }
}
export async function loginLDAP(
  input: { username: string; password: string },
  signal?: AbortSignal,
): Promise<LDAPLoginResult> {
  if (!validLDAPLogin(input)) throw invalid()
  const response = await request(
    'POST',
    '/auth/ldap/login',
    { ...input },
    undefined,
    undefined,
    signal,
  )
  const value: unknown = response.data
  if (response.status === 200 && session(value))
    return { kind: 'session', session: { ...value, user: { ...value.user } } }
  if (
    response.status === 202 &&
    object(value) &&
    fields(value, ['mfa_required', 'challenge_token', 'expires_at', 'methods']) &&
    value.mfa_required === true &&
    typeof value.challenge_token === 'string' &&
    /^[a-zA-Z0-9_-]{43}$/.test(value.challenge_token) &&
    typeof value.expires_at === 'string' &&
    /^(?!0000)\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value.expires_at) &&
    Number.isFinite(Date.parse(value.expires_at)) &&
    new Date(value.expires_at).toISOString().slice(0, 19) === value.expires_at.slice(0, 19) &&
    Array.isArray(value.methods) &&
    value.methods.length === 2 &&
    value.methods[0] === 'totp' &&
    value.methods[1] === 'recovery_code'
  )
    return {
      kind: 'challenge',
      challenge: value as unknown as Extract<LDAPLoginResult, { kind: 'challenge' }>['challenge'],
    }
  throw invalid()
}
