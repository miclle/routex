import axios, { AxiosHeaders, type AxiosResponse } from 'axios'
import client from './client'
import { validApprovalReason } from './registration-approval'
import type { Session } from '@/types/auth'
import type {
  GoogleMethod,
  GoogleIdentity,
  GoogleConfig,
  GoogleConfigInput,
  GoogleLocalProof,
  GoogleCompletion,
} from '@/types/google'

export class GoogleRequestError extends Error {
  constructor(public readonly status: number) {
    super('Google request unavailable')
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
const invalid = () => new GoogleRequestError(0)
const asciiControl = (value: string) =>
  Array.from(value).some(
    (character) => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127,
  )
const validName = (value: unknown): value is string =>
  text(value, 400) && !!value && value.trim() === value && Array.from(value).length <= 100
function https(value: unknown, callback = false): value is string {
  if (
    !text(value, 2048) ||
    !value ||
    /[\s\\]/u.test(value) ||
    value.includes('%') ||
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
      (!callback || url.pathname === '/api/v1/auth/google/callback')
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
    if (response.status >= 400) throw new GoogleRequestError(response.status)
    const privacy = String(header(response, 'Cache-Control') ?? '')
      .toLowerCase()
      .split(',')
      .map((part) => part.trim())
    if (
      (url !== '/auth/session' && !privacy.includes('private')) ||
      !privacy.includes('no-store') ||
      (url !== '/auth/session' &&
        String(header(response, 'Referrer-Policy') ?? '').toLowerCase() !== 'no-referrer') ||
      String(header(response, 'X-Content-Type-Options') ?? '').toLowerCase() !== 'nosniff'
    )
      throw invalid()
    return response
  } catch (error) {
    if (error instanceof GoogleRequestError) throw error
    throw new GoogleRequestError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
function method(value: unknown): value is GoogleMethod {
  return (
    object(value) &&
    fields(value, ['available', 'name']) &&
    typeof value.available === 'boolean' &&
    text(value.name, 400) &&
    (value.available ? validName(value.name) : value.name === '')
  )
}
function identity(value: unknown): value is GoogleIdentity {
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
function config(value: unknown): value is GoogleConfig {
  if (
    !object(value) ||
    !fields(value, [
      'name',
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
    value.name === '' &&
    value.client_id === '' &&
    value.callback_url === '' &&
    !value.secret_configured &&
    !value.enabled &&
    !value.verified
  return (
    empty ||
    (validName(value.name) &&
      text(value.client_id, 256) &&
      !!value.client_id &&
      !/\s/u.test(value.client_id) &&
      https(value.callback_url, true) &&
      value.secret_configured === true &&
      (!value.enabled || value.verified === true))
  )
}
export function validGoogleConfig(value: GoogleConfigInput) {
  return (
    object(value) &&
    fields(value, [
      'name',
      'client_id',
      'callback_url',
      'secret_action',
      'client_secret',
      'reason',
    ]) &&
    validName(value.name) &&
    https(value.callback_url, true) &&
    text(value.client_id, 256) &&
    !!value.client_id &&
    !/\s/u.test(value.client_id) &&
    validApprovalReason(value.reason) &&
    (value.secret_action === 'keep'
      ? value.client_secret === ''
      : value.secret_action === 'replace' &&
        typeof value.client_secret === 'string' &&
        !!value.client_secret &&
        !/\p{Cs}/u.test(value.client_secret) &&
        !asciiControl(value.client_secret) &&
        new TextEncoder().encode(value.client_secret).length <= 4096)
  )
}
export function validGoogleProof(value: GoogleLocalProof, required: boolean) {
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
    !text(value.authorization_url, 8192) ||
    /[\s\\]/u.test(value.authorization_url)
  )
    throw invalid()
  try {
    const url = new URL(value.authorization_url)
    const keys = [
      'response_type',
      'client_id',
      'redirect_uri',
      'scope',
      'state',
      'nonce',
      'code_challenge',
      'code_challenge_method',
    ]
    const params = url.searchParams
    if (
      url.origin !== 'https://accounts.google.com' ||
      url.pathname !== '/o/oauth2/v2/auth' ||
      url.username ||
      url.password ||
      url.hash ||
      Array.from(params.keys()).length !== keys.length ||
      !keys.every((key) => params.getAll(key).length === 1) ||
      params.get('response_type') !== 'code' ||
      params.get('scope') !== 'openid profile' ||
      params.get('code_challenge_method') !== 'S256' ||
      !/^[A-Za-z0-9._~-]{32,128}$/.test(params.get('state') ?? '') ||
      !/^[A-Za-z0-9._~-]{32,128}$/.test(params.get('nonce') ?? '') ||
      !/^[A-Za-z0-9_-]{43}$/.test(params.get('code_challenge') ?? '') ||
      !text(params.get('client_id'), 256) ||
      !params.get('client_id') ||
      /\s/u.test(params.get('client_id')!) ||
      !https(params.get('redirect_uri'), true)
    )
      throw invalid()
    return value.authorization_url
  } catch {
    throw invalid()
  }
}
export async function getGoogleMethod(signal?: AbortSignal): Promise<GoogleMethod> {
  const response = await request('GET', '/auth/google', undefined, undefined, undefined, signal)
  if (response.status !== 200 || !method(response.data)) throw invalid()
  return { ...response.data }
}
export async function startGoogle(signal?: AbortSignal) {
  return authorization(
    await request('POST', '/auth/google/start', {}, undefined, undefined, signal),
  )
}
export async function getGoogleIdentity(signal?: AbortSignal): Promise<GoogleIdentity> {
  const response = await request(
    'GET',
    '/account/identity/google',
    undefined,
    undefined,
    undefined,
    signal,
  )
  if (response.status !== 200 || !reviewed(response) || !identity(response.data)) throw invalid()
  return { ...response.data }
}
export async function getGoogleConfig(signal?: AbortSignal): Promise<GoogleConfig> {
  const response = await request(
    'GET',
    '/admin/auth/google',
    undefined,
    undefined,
    undefined,
    signal,
  )
  if (response.status !== 200 || !reviewed(response) || !config(response.data)) throw invalid()
  return { ...response.data }
}
export async function saveGoogleConfig(
  etag: string,
  input: GoogleConfigInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<GoogleConfig> {
  admitted(etag, csrf)
  if (!validGoogleConfig(input)) throw invalid()
  const captured = { ...input }
  const response = await request('PUT', '/admin/auth/google', captured, csrf, etag, signal)
  if (
    response.status !== 200 ||
    !reviewed(response) ||
    !config(response.data) ||
    response.data.name !== captured.name ||
    response.data.client_id !== captured.client_id ||
    response.data.callback_url !== captured.callback_url ||
    !response.data.secret_configured
  )
    throw invalid()
  return { ...response.data }
}
export async function setGoogleStatus(
  etag: string,
  input: { enabled: boolean; reason: string },
  csrf: string,
  signal?: AbortSignal,
): Promise<GoogleConfig> {
  admitted(etag, csrf)
  if (
    !object(input) ||
    !fields(input, ['enabled', 'reason']) ||
    typeof input.enabled !== 'boolean' ||
    !validApprovalReason(input.reason)
  )
    throw invalid()
  const captured = { ...input }
  const response = await request('PUT', '/admin/auth/google/status', captured, csrf, etag, signal)
  if (
    response.status !== 200 ||
    !reviewed(response) ||
    !config(response.data) ||
    response.data.enabled !== captured.enabled
  )
    throw invalid()
  return { ...response.data }
}
export async function beginGoogleProof(
  action: 'bind' | 'verify',
  etag: string,
  input: GoogleLocalProof,
  required: boolean,
  csrf: string,
  signal?: AbortSignal,
) {
  admitted(etag, csrf)
  if (!validGoogleProof(input, required)) throw invalid()
  return authorization(
    await request(
      'POST',
      action === 'bind' ? '/account/identity/google/bind' : '/admin/auth/google/verify',
      { ...input, proof: { ...input.proof } },
      csrf,
      etag,
      signal,
    ),
  )
}
export async function unlinkGoogle(
  etag: string,
  input: GoogleLocalProof,
  required: boolean,
  csrf: string,
  signal?: AbortSignal,
): Promise<GoogleIdentity> {
  admitted(etag, csrf)
  if (!validGoogleProof(input, required)) throw invalid()
  const response = await request(
    'POST',
    '/account/identity/google/unlink',
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
export async function readGoogleSession(signal?: AbortSignal): Promise<Session | null> {
  try {
    const response = await request('GET', '/auth/session', undefined, undefined, undefined, signal)
    if (response.status !== 200 || !session(response.data)) throw invalid()
    return { ...response.data, user: { ...response.data.user } }
  } catch (error) {
    if (error instanceof GoogleRequestError && error.status === 401 && !signal?.aborted) return null
    throw error
  }
}
export async function completeGoogle(
  csrf?: string,
  signal?: AbortSignal,
): Promise<GoogleCompletion> {
  const response = await request('POST', '/auth/google/complete', {}, csrf, undefined, signal)
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
      challenge: value as unknown as Extract<GoogleCompletion, { kind: 'challenge' }>['challenge'],
    }
  throw invalid()
}

export async function abandonGoogle(csrf?: string, signal?: AbortSignal): Promise<void> {
  const response = await request('POST', '/auth/google/abandon', {}, csrf, undefined, signal)
  if (
    response.status !== 204 ||
    (response.data !== '' && response.data !== undefined && response.data !== null)
  )
    throw invalid()
}
