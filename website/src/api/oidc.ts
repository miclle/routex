import axios, { AxiosHeaders, type AxiosResponse } from 'axios'
import client from './client'
import { validApprovalReason } from './registration-approval'
import type { Session } from '@/types/auth'
import type {
  OIDCMethod,
  OIDCIdentity,
  OIDCConfig,
  OIDCConfigInput,
  OIDCLocalProof,
  OIDCCompletion,
} from '@/types/oidc'

export class OIDCRequestError extends Error {
  constructor(public readonly status: number) {
    super('OIDC request unavailable')
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
const invalid = () => new OIDCRequestError(0)
const validName = (value: unknown): value is string =>
  text(value, 400) && !!value && value.trim() === value && Array.from(value).length <= 100
function https(value: unknown, callback = false): value is string {
  if (!text(value, 2048) || !value || value.trim() !== value) return false
  try {
    const url = new URL(value)
    return (
      url.protocol === 'https:' &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash &&
      (!callback || url.pathname === '/api/v1/auth/oidc/callback')
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
    if (response.status >= 400) throw new OIDCRequestError(response.status)
    return response
  } catch (error) {
    if (error instanceof OIDCRequestError) throw error
    throw new OIDCRequestError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
function method(value: unknown): value is OIDCMethod {
  return (
    object(value) &&
    fields(value, ['available', 'name']) &&
    typeof value.available === 'boolean' &&
    text(value.name, 400) &&
    (!value.available || validName(value.name))
  )
}
function identity(value: unknown): value is OIDCIdentity {
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
function config(value: unknown): value is OIDCConfig {
  return (
    object(value) &&
    fields(value, [
      'name',
      'issuer',
      'client_id',
      'callback_url',
      'secret_configured',
      'enabled',
      'verified',
      'mfa_required',
      'review_etag',
    ]) &&
    text(value.name, 400) &&
    text(value.issuer, 2048) &&
    text(value.client_id, 256) &&
    text(value.callback_url, 2048) &&
    ['secret_configured', 'enabled', 'verified', 'mfa_required'].every(
      (key) => typeof value[key] === 'boolean',
    ) &&
    token(value.review_etag) &&
    (!value.enabled || value.verified === true) &&
    (!value.verified ||
      (validName(value.name) &&
        https(value.issuer) &&
        https(value.callback_url, true) &&
        !!value.client_id &&
        value.secret_configured === true))
  )
}
export function validOIDCConfig(value: OIDCConfigInput) {
  return (
    object(value) &&
    fields(value, [
      'name',
      'issuer',
      'client_id',
      'callback_url',
      'secret_action',
      'client_secret',
      'reason',
    ]) &&
    validName(value.name) &&
    https(value.issuer) &&
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
        new TextEncoder().encode(value.client_secret).length <= 4096)
  )
}
export function validOIDCProof(value: OIDCLocalProof, required: boolean) {
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
    !text(value.authorization_url, 16384)
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
export async function getOIDCMethod(signal?: AbortSignal): Promise<OIDCMethod> {
  const response = await request('GET', '/auth/oidc', undefined, undefined, undefined, signal)
  if (response.status !== 200 || !method(response.data)) throw invalid()
  return { ...response.data }
}
export async function startOIDC(signal?: AbortSignal) {
  return authorization(await request('POST', '/auth/oidc/start', {}, undefined, undefined, signal))
}
export async function getOIDCIdentity(signal?: AbortSignal): Promise<OIDCIdentity> {
  const response = await request(
    'GET',
    '/account/identity',
    undefined,
    undefined,
    undefined,
    signal,
  )
  if (response.status !== 200 || !reviewed(response) || !identity(response.data)) throw invalid()
  return { ...response.data }
}
export async function getOIDCConfig(signal?: AbortSignal): Promise<OIDCConfig> {
  const response = await request('GET', '/admin/auth/oidc', undefined, undefined, undefined, signal)
  if (response.status !== 200 || !reviewed(response) || !config(response.data)) throw invalid()
  return { ...response.data }
}
export async function saveOIDCConfig(
  etag: string,
  input: OIDCConfigInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<OIDCConfig> {
  admitted(etag, csrf)
  if (!validOIDCConfig(input)) throw invalid()
  const response = await request('PUT', '/admin/auth/oidc', { ...input }, csrf, etag, signal)
  if (
    response.status !== 200 ||
    !reviewed(response) ||
    !config(response.data) ||
    response.data.name !== input.name ||
    response.data.issuer !== input.issuer ||
    response.data.client_id !== input.client_id ||
    response.data.callback_url !== input.callback_url ||
    !response.data.secret_configured
  )
    throw invalid()
  return { ...response.data }
}
export async function setOIDCStatus(
  etag: string,
  input: { enabled: boolean; reason: string },
  csrf: string,
  signal?: AbortSignal,
): Promise<OIDCConfig> {
  admitted(etag, csrf)
  if (typeof input.enabled !== 'boolean' || !validApprovalReason(input.reason)) throw invalid()
  const response = await request('PUT', '/admin/auth/oidc/status', { ...input }, csrf, etag, signal)
  if (
    response.status !== 200 ||
    !reviewed(response) ||
    !config(response.data) ||
    response.data.enabled !== input.enabled
  )
    throw invalid()
  return { ...response.data }
}
export async function beginOIDCProof(
  action: 'bind' | 'verify',
  etag: string,
  input: OIDCLocalProof,
  required: boolean,
  csrf: string,
  signal?: AbortSignal,
) {
  admitted(etag, csrf)
  if (!validOIDCProof(input, required)) throw invalid()
  return authorization(
    await request(
      'POST',
      action === 'bind' ? '/account/identity/bind' : '/admin/auth/oidc/verify',
      { ...input, proof: { ...input.proof } },
      csrf,
      etag,
      signal,
    ),
  )
}
export async function unlinkOIDC(
  etag: string,
  input: OIDCLocalProof,
  required: boolean,
  csrf: string,
  signal?: AbortSignal,
): Promise<OIDCIdentity> {
  admitted(etag, csrf)
  if (!validOIDCProof(input, required)) throw invalid()
  const response = await request(
    'POST',
    '/account/identity/unlink',
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
export async function readOIDCSession(signal?: AbortSignal): Promise<Session | null> {
  try {
    const response = await request('GET', '/auth/session', undefined, undefined, undefined, signal)
    if (response.status !== 200 || !session(response.data)) throw invalid()
    return { ...response.data, user: { ...response.data.user } }
  } catch (error) {
    if (error instanceof OIDCRequestError && error.status === 401 && !signal?.aborted) return null
    throw error
  }
}
export async function completeOIDC(csrf?: string, signal?: AbortSignal): Promise<OIDCCompletion> {
  const response = await request('POST', '/auth/oidc/complete', {}, csrf, undefined, signal)
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
      challenge: value as unknown as Extract<OIDCCompletion, { kind: 'challenge' }>['challenge'],
    }
  throw invalid()
}
