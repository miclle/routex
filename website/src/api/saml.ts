import axios, { AxiosHeaders, type AxiosResponse } from 'axios'
import client from './client'
import { validApprovalReason } from './registration-approval'
import type { Session } from '@/types/auth'
import type {
  SAMLMethod,
  SAMLIdentity,
  SAMLConfig,
  SAMLConfigInput,
  SAMLProofInput,
  SAMLCompletion,
} from '@/types/saml'

export class SAMLRequestError extends Error {
  constructor(public readonly status: number) {
    super('SAML request unavailable')
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
const invalid = () => new SAMLRequestError(0)
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
      (!callback || url.pathname === '/api/v1/auth/saml/acs')
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
    const control = String(header(response, 'Cache-Control') ?? '').toLowerCase()
    if (
      !control
        .split(',')
        .map((part) => part.trim())
        .includes('no-store') ||
      header(response, 'X-Content-Type-Options') !== 'nosniff'
    )
      throw invalid()
    if (response.status >= 400) throw new SAMLRequestError(response.status)
    return response
  } catch (error) {
    if (error instanceof SAMLRequestError) throw error
    throw new SAMLRequestError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
function method(value: unknown): value is SAMLMethod {
  return (
    object(value) &&
    fields(value, ['available', 'name']) &&
    typeof value.available === 'boolean' &&
    text(value.name, 400) &&
    (!value.available || validName(value.name))
  )
}
function identity(value: unknown): value is SAMLIdentity {
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
function identifier(value: unknown): value is string {
  if (!text(value, 2048) || !value || /[\s\\]/u.test(value) || value.includes('#')) return false
  try {
    const url = new URL(value)
    return !!url.protocol && !url.username && !url.password && !url.hash
  } catch {
    return false
  }
}
// Public certificate syntax only; X.509 trust, key strength and validity remain server-owned.
function certificate(value: unknown): string | null {
  if (typeof value !== 'string' || new TextEncoder().encode(value).length > 24576) return null
  const match =
    /^\s*-----BEGIN CERTIFICATE-----[\r\n]+([A-Za-z0-9+/=\r\n]+)-----END CERTIFICATE-----\s*$/.exec(
      value,
    )
  if (!match) return null
  const encoded = match[1].replace(/[\r\n]/g, '')
  if (
    !encoded ||
    encoded.length > 21848 ||
    !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encoded)
  )
    return null
  try {
    const decoded = atob(encoded)
    return decoded.length <= 16384 && btoa(decoded) === encoded ? encoded : null
  } catch {
    return null
  }
}
export function sameSAMLCertificate(left: string, right: string) {
  const admitted = certificate(left)
  return admitted !== null && admitted === certificate(right)
}
const configurationKeys = [
  'name',
  'idp_issuer',
  'sso_url',
  'sp_entity_id',
  'acs_url',
  'signing_certificate_pem',
] as const
function configuration(value: Record<string, unknown>) {
  return (
    validName(value.name) &&
    identifier(value.idp_issuer) &&
    https(value.sso_url) &&
    identifier(value.sp_entity_id) &&
    https(value.acs_url, true) &&
    certificate(value.signing_certificate_pem) !== null
  )
}
function config(value: unknown): value is SAMLConfig {
  if (
    !object(value) ||
    !fields(value, [...configurationKeys, 'enabled', 'verified', 'mfa_required', 'review_etag']) ||
    !['enabled', 'verified', 'mfa_required'].every((key) => typeof value[key] === 'boolean') ||
    !token(value.review_etag)
  )
    return false
  const empty =
    configurationKeys.every((key) => value[key] === '') &&
    value.enabled === false &&
    value.verified === false
  return empty || (configuration(value) && (!value.enabled || value.verified === true))
}
export function validSAMLConfig(value: SAMLConfigInput) {
  return (
    object(value) &&
    fields(value, [...configurationKeys, 'reason']) &&
    configuration(value) &&
    validApprovalReason(value.reason)
  )
}
export function copySAMLInput(value: SAMLConfigInput): SAMLConfigInput {
  return { ...value }
}
function copyConfig(value: SAMLConfig): SAMLConfig {
  return { ...value }
}
export function validSAMLProof(value: SAMLProofInput, required: boolean) {
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
    if (url.protocol !== 'https:' || url.username || url.password || url.hash) throw invalid()
    return value.authorization_url
  } catch {
    throw invalid()
  }
}
export async function getSAMLMethod(signal?: AbortSignal): Promise<SAMLMethod> {
  const response = await request('GET', '/auth/saml', undefined, undefined, undefined, signal)
  if (response.status !== 200 || !method(response.data)) throw invalid()
  return { ...response.data }
}
export async function startSAML(signal?: AbortSignal) {
  return authorization(await request('POST', '/auth/saml/start', {}, undefined, undefined, signal))
}
export async function getSAMLIdentity(signal?: AbortSignal): Promise<SAMLIdentity> {
  const response = await request(
    'GET',
    '/account/identity/saml',
    undefined,
    undefined,
    undefined,
    signal,
  )
  if (response.status !== 200 || !reviewed(response) || !identity(response.data)) throw invalid()
  return { ...response.data }
}
export async function getSAMLConfig(signal?: AbortSignal): Promise<SAMLConfig> {
  const response = await request('GET', '/admin/auth/saml', undefined, undefined, undefined, signal)
  if (response.status !== 200 || !reviewed(response) || !config(response.data)) throw invalid()
  return copyConfig(response.data)
}
export async function saveSAMLConfig(
  etag: string,
  input: SAMLConfigInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<SAMLConfig> {
  admitted(etag, csrf)
  if (!validSAMLConfig(input)) throw invalid()
  const captured = copySAMLInput(input)
  const response = await request('PUT', '/admin/auth/saml', captured, csrf, etag, signal)
  if (
    response.status !== 200 ||
    !reviewed(response) ||
    !config(response.data) ||
    response.data.name !== captured.name ||
    response.data.idp_issuer !== captured.idp_issuer ||
    response.data.sso_url !== captured.sso_url ||
    response.data.sp_entity_id !== captured.sp_entity_id ||
    response.data.acs_url !== captured.acs_url ||
    certificate(response.data.signing_certificate_pem) !==
      certificate(captured.signing_certificate_pem)
  )
    throw invalid()
  return copyConfig(response.data)
}
export async function setSAMLStatus(
  etag: string,
  input: { enabled: boolean; reason: string },
  csrf: string,
  signal?: AbortSignal,
): Promise<SAMLConfig> {
  admitted(etag, csrf)
  if (
    !object(input) ||
    !fields(input, ['enabled', 'reason']) ||
    typeof input.enabled !== 'boolean' ||
    !validApprovalReason(input.reason)
  )
    throw invalid()
  const response = await request('PUT', '/admin/auth/saml/status', { ...input }, csrf, etag, signal)
  if (
    response.status !== 200 ||
    !reviewed(response) ||
    !config(response.data) ||
    response.data.enabled !== input.enabled
  )
    throw invalid()
  return copyConfig(response.data)
}
export async function beginSAMLProof(
  action: 'bind' | 'verify',
  etag: string,
  input: SAMLProofInput,
  required: boolean,
  csrf: string,
  signal?: AbortSignal,
) {
  admitted(etag, csrf)
  if (!validSAMLProof(input, required)) throw invalid()
  return authorization(
    await request(
      'POST',
      action === 'bind' ? '/account/identity/saml/bind' : '/admin/auth/saml/verify',
      { ...input, proof: { ...input.proof } },
      csrf,
      etag,
      signal,
    ),
  )
}
export async function unlinkSAML(
  etag: string,
  input: SAMLProofInput,
  required: boolean,
  csrf: string,
  signal?: AbortSignal,
): Promise<SAMLIdentity> {
  admitted(etag, csrf)
  if (!validSAMLProof(input, required)) throw invalid()
  const response = await request(
    'POST',
    '/account/identity/saml/unlink',
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
export async function readSAMLSession(signal?: AbortSignal): Promise<Session | null> {
  try {
    const response = await request('GET', '/auth/session', undefined, undefined, undefined, signal)
    if (response.status !== 200 || !session(response.data)) throw invalid()
    return { ...response.data, user: { ...response.data.user } }
  } catch (error) {
    if (error instanceof SAMLRequestError && error.status === 401 && !signal?.aborted) return null
    throw error
  }
}
export async function completeSAML(csrf?: string, signal?: AbortSignal): Promise<SAMLCompletion> {
  const response = await request('POST', '/auth/saml/complete', {}, csrf, undefined, signal)
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
      challenge: value as unknown as Extract<SAMLCompletion, { kind: 'challenge' }>['challenge'],
    }
  throw invalid()
}

export async function abandonSAML(csrf?: string, signal?: AbortSignal): Promise<void> {
  const response = await request('POST', '/auth/saml/abandon', {}, csrf, undefined, signal)
  if (
    response.status !== 204 ||
    (response.data !== '' && response.data !== undefined && response.data !== null)
  )
    throw invalid()
}
