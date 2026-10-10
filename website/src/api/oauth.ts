import axios, { AxiosHeaders, type AxiosResponse } from 'axios'
import client from './client'
import { validApprovalReason } from './registration-approval'
import type { Session } from '@/types/auth'
import type {
  OAuthMethod,
  OAuthIdentity,
  OAuthConfig,
  OAuthConfigInput,
  OAuthLocalProof,
  OAuthCompletion,
} from '@/types/oauth'

export class OAuthRequestError extends Error {
  constructor(public readonly status: number) {
    super('OAuth request unavailable')
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
const invalid = () => new OAuthRequestError(0)
const validName = (value: unknown): value is string =>
  text(value, 400) && !!value && value.trim() === value && Array.from(value).length <= 100
function https(value: unknown, callback = false): value is string {
  if (
    !text(value, 2048) ||
    !value ||
    /[\s\\]/u.test(value) ||
    value.includes('?') ||
    value.includes('#')
  )
    return false
  try {
    const url = new URL(value)
    return (
      url.protocol === 'https:' &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash &&
      (!callback || url.pathname === '/api/v1/auth/oauth/callback')
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
    if (response.status >= 400) throw new OAuthRequestError(response.status)
    return response
  } catch (error) {
    if (error instanceof OAuthRequestError) throw error
    throw new OAuthRequestError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
function method(value: unknown): value is OAuthMethod {
  return (
    object(value) &&
    fields(value, ['available', 'name']) &&
    typeof value.available === 'boolean' &&
    text(value.name, 400) &&
    (!value.available || validName(value.name))
  )
}
function identity(value: unknown): value is OAuthIdentity {
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
const oauthText = (value: unknown, max: number): value is string =>
  typeof value === 'string' &&
  !/\p{Cs}/u.test(value) &&
  !Array.from(value).some((part) => part.charCodeAt(0) < 32 || part.charCodeAt(0) === 127) &&
  new TextEncoder().encode(value).length <= max
const sameList = (a: string[], b: string[]) =>
  a.length === b.length && a.every((value, index) => value === b[index])
function scopes(value: unknown): value is string[] {
  return (
    Array.isArray(value) &&
    value.length <= 16 &&
    value.every(
      (part) =>
        typeof part === 'string' &&
        part.length >= 1 &&
        part.length <= 128 &&
        /^[\x21\x23-\x5b\x5d-\x7e]+$/.test(part),
    ) &&
    new Set(value).size === value.length &&
    value.reduce((total, part) => total + part.length, 0) <= 1024
  )
}
function path(value: unknown): value is string[] {
  return (
    Array.isArray(value) &&
    value.length >= 1 &&
    value.length <= 16 &&
    value.every((part) => oauthText(part, 128) && !!part) &&
    value.reduce((total, part) => total + new TextEncoder().encode(part).length, 0) <= 1024
  )
}
function configuration(value: Record<string, unknown>) {
  return (
    validName(value.name) &&
    https(value.authorization_url) &&
    https(value.token_url) &&
    https(value.user_info_url) &&
    https(value.callback_url, true) &&
    oauthText(value.client_id, 256) &&
    !!value.client_id &&
    ['client_secret_basic', 'client_secret_post'].includes(value.client_auth_method as string) &&
    scopes(value.scopes) &&
    path(value.subject_path)
  )
}
function config(value: unknown): value is OAuthConfig {
  if (
    !object(value) ||
    !fields(value, [
      'name',
      'authorization_url',
      'token_url',
      'user_info_url',
      'client_auth_method',
      'scopes',
      'subject_path',
      'client_id',
      'callback_url',
      'secret_configured',
      'enabled',
      'verified',
      'mfa_required',
      'review_etag',
    ]) ||
    !['secret_configured', 'enabled', 'verified', 'mfa_required'].every(
      (key) => typeof value[key] === 'boolean',
    ) ||
    !token(value.review_etag)
  )
    return false
  const empty =
    [
      'name',
      'authorization_url',
      'token_url',
      'user_info_url',
      'client_auth_method',
      'client_id',
      'callback_url',
    ].every((key) => value[key] === '') &&
    Array.isArray(value.scopes) &&
    value.scopes.length === 0 &&
    Array.isArray(value.subject_path) &&
    value.subject_path.length === 0 &&
    value.secret_configured === false &&
    value.enabled === false &&
    value.verified === false
  return (
    empty ||
    (configuration(value) &&
      (!value.enabled || value.verified === true) &&
      (!value.verified || value.secret_configured === true))
  )
}
export function validOAuthConfig(value: OAuthConfigInput) {
  return (
    object(value) &&
    fields(value, [
      'name',
      'authorization_url',
      'token_url',
      'user_info_url',
      'client_auth_method',
      'scopes',
      'subject_path',
      'client_id',
      'callback_url',
      'secret_action',
      'client_secret',
      'reason',
    ]) &&
    configuration(value) &&
    validApprovalReason(value.reason) &&
    (value.secret_action === 'keep'
      ? value.client_secret === ''
      : value.secret_action === 'replace' &&
        oauthText(value.client_secret, 4096) &&
        !!value.client_secret)
  )
}
function copyConfig(value: OAuthConfig): OAuthConfig {
  return { ...value, scopes: [...value.scopes], subject_path: [...value.subject_path] }
}
export function copyOAuthInput(value: OAuthConfigInput): OAuthConfigInput {
  return { ...value, scopes: [...value.scopes], subject_path: [...value.subject_path] }
}
export function validOAuthProof(value: OAuthLocalProof, required: boolean) {
  if (
    !object(value) ||
    !fields(value, ['password', 'proof', 'reason']) ||
    typeof value.password !== 'string'
  )
    return false
  const bytes = new TextEncoder().encode(value.password).length
  if (bytes < 12 || bytes > 72 || !validApprovalReason(value.reason) || !object(value.proof))
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
function admitted(etag: string, csrf: string) {
  if (!token(etag) || !csrf) throw invalid()
}
function authorization(response: AxiosResponse): string {
  const value: unknown = response.data
  if (
    response.status !== 200 ||
    !object(value) ||
    !fields(value, ['authorization_url']) ||
    !text(value.authorization_url, 16384) ||
    /[\s\\]/u.test(value.authorization_url)
  )
    throw invalid()
  try {
    const url = new URL(value.authorization_url)
    if (url.protocol !== 'https:' || url.username || url.password || url.hash) throw invalid()
    return value.authorization_url
  } catch {
    throw invalid()
  }
}
export async function getOAuthMethod(signal?: AbortSignal): Promise<OAuthMethod> {
  const response = await request('GET', '/auth/oauth', undefined, undefined, undefined, signal)
  if (response.status !== 200 || !method(response.data)) throw invalid()
  return { ...response.data }
}
export async function startOAuth(signal?: AbortSignal) {
  return authorization(await request('POST', '/auth/oauth/start', {}, undefined, undefined, signal))
}
export async function getOAuthIdentity(signal?: AbortSignal): Promise<OAuthIdentity> {
  const response = await request(
    'GET',
    '/account/identity/oauth',
    undefined,
    undefined,
    undefined,
    signal,
  )
  if (response.status !== 200 || !reviewed(response) || !identity(response.data)) throw invalid()
  return { ...response.data }
}
export async function getOAuthConfig(signal?: AbortSignal): Promise<OAuthConfig> {
  const response = await request(
    'GET',
    '/admin/auth/oauth',
    undefined,
    undefined,
    undefined,
    signal,
  )
  if (response.status !== 200 || !reviewed(response) || !config(response.data)) throw invalid()
  return copyConfig(response.data)
}
export async function saveOAuthConfig(
  etag: string,
  input: OAuthConfigInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<OAuthConfig> {
  admitted(etag, csrf)
  if (!validOAuthConfig(input)) throw invalid()
  const captured = copyOAuthInput(input)
  const response = await request('PUT', '/admin/auth/oauth', captured, csrf, etag, signal)
  if (
    response.status !== 200 ||
    !reviewed(response) ||
    !config(response.data) ||
    response.data.name !== captured.name ||
    response.data.authorization_url !== captured.authorization_url ||
    response.data.token_url !== captured.token_url ||
    response.data.user_info_url !== captured.user_info_url ||
    response.data.client_auth_method !== captured.client_auth_method ||
    !sameList(response.data.scopes, captured.scopes) ||
    !sameList(response.data.subject_path, captured.subject_path) ||
    response.data.client_id !== captured.client_id ||
    response.data.callback_url !== captured.callback_url ||
    !response.data.secret_configured
  )
    throw invalid()
  return copyConfig(response.data)
}
export async function setOAuthStatus(
  etag: string,
  input: { enabled: boolean; reason: string },
  csrf: string,
  signal?: AbortSignal,
): Promise<OAuthConfig> {
  admitted(etag, csrf)
  if (
    !object(input) ||
    !fields(input, ['enabled', 'reason']) ||
    typeof input.enabled !== 'boolean' ||
    !validApprovalReason(input.reason)
  )
    throw invalid()
  const response = await request(
    'PUT',
    '/admin/auth/oauth/status',
    { ...input },
    csrf,
    etag,
    signal,
  )
  if (
    response.status !== 200 ||
    !reviewed(response) ||
    !config(response.data) ||
    response.data.enabled !== input.enabled
  )
    throw invalid()
  return copyConfig(response.data)
}
export async function beginOAuthProof(
  action: 'bind' | 'verify',
  etag: string,
  input: OAuthLocalProof,
  required: boolean,
  csrf: string,
  signal?: AbortSignal,
) {
  admitted(etag, csrf)
  if (!validOAuthProof(input, required)) throw invalid()
  return authorization(
    await request(
      'POST',
      action === 'bind' ? '/account/identity/oauth/bind' : '/admin/auth/oauth/verify',
      { ...input, proof: { ...input.proof } },
      csrf,
      etag,
      signal,
    ),
  )
}
export async function unlinkOAuth(
  etag: string,
  input: OAuthLocalProof,
  required: boolean,
  csrf: string,
  signal?: AbortSignal,
): Promise<OAuthIdentity> {
  admitted(etag, csrf)
  if (!validOAuthProof(input, required)) throw invalid()
  const response = await request(
    'POST',
    '/account/identity/oauth/unlink',
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
export async function readOAuthSession(signal?: AbortSignal): Promise<Session | null> {
  try {
    const response = await request('GET', '/auth/session', undefined, undefined, undefined, signal)
    if (response.status !== 200 || !session(response.data)) throw invalid()
    return { ...response.data, user: { ...response.data.user } }
  } catch (error) {
    if (error instanceof OAuthRequestError && error.status === 401 && !signal?.aborted) return null
    throw error
  }
}
export async function completeOAuth(csrf?: string, signal?: AbortSignal): Promise<OAuthCompletion> {
  const response = await request('POST', '/auth/oauth/complete', {}, csrf, undefined, signal)
  const value: unknown = response.data
  if (response.status === 200 && session(value))
    return { kind: 'session', session: { ...value, user: { ...value.user } } }
  if (
    response.status === 200 &&
    object(value) &&
    fields(value, ['kind']) &&
    ['bound', 'verified'].includes(value.kind as string)
  )
    return { kind: value.kind as 'bound' | 'verified' }
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
      challenge: value as unknown as Extract<OAuthCompletion, { kind: 'challenge' }>['challenge'],
    }
  throw invalid()
}
