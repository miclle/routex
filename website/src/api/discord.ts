import axios, { AxiosHeaders, type AxiosResponse } from 'axios'
import client from './client'
import { validApprovalReason } from './registration-approval'
import type { Session } from '@/types/auth'
import type {
  DiscordMethod,
  DiscordIdentity,
  DiscordConfig,
  DiscordConfigInput,
  DiscordLocalProof,
  DiscordCompletion,
} from '@/types/discord'

export class DiscordRequestError extends Error {
  constructor(public readonly status: number) {
    super('Discord request unavailable')
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
const invalid = () => new DiscordRequestError(0)
const canonicalClientID = (value: unknown): value is string =>
  typeof value === 'string' &&
  value.length > 0 &&
  value.length <= 20 &&
  value[0] !== '0' &&
  !/[^0-9]/.test(value) &&
  (value.length < 20 || value <= '18446744073709551615')
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
      (!callback || url.pathname === '/api/v1/auth/discord/callback')
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
    if (response.status >= 400) throw new DiscordRequestError(response.status)
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
    if (error instanceof DiscordRequestError) throw error
    throw new DiscordRequestError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
function method(value: unknown): value is DiscordMethod {
  return (
    object(value) &&
    fields(value, ['available', 'name']) &&
    typeof value.available === 'boolean' &&
    text(value.name, 400) &&
    (value.available ? validName(value.name) : value.name === '')
  )
}
function identity(value: unknown): value is DiscordIdentity {
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
function config(value: unknown): value is DiscordConfig {
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
      canonicalClientID(value.client_id) &&
      https(value.callback_url, true) &&
      value.secret_configured === true &&
      (!value.enabled || value.verified === true))
  )
}
export function validDiscordConfig(value: DiscordConfigInput) {
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
    canonicalClientID(value.client_id) &&
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
export function validDiscordProof(value: DiscordLocalProof, required: boolean) {
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
      'code_challenge',
      'code_challenge_method',
    ]
    const params = url.searchParams
    if (
      !value.authorization_url.startsWith('https://discord.com/oauth2/authorize?') ||
      url.origin !== 'https://discord.com' ||
      url.pathname !== '/oauth2/authorize' ||
      url.username ||
      url.password ||
      url.hash ||
      Array.from(params.keys()).length !== keys.length ||
      !keys.every((key) => params.getAll(key).length === 1) ||
      params.get('response_type') !== 'code' ||
      params.get('scope') !== 'identify' ||
      params.get('code_challenge_method') !== 'S256' ||
      params.get('state')?.length !== 43 ||
      !/^[A-Za-z0-9_-]{43}$/.test(params.get('state') ?? '') ||
      params.get('code_challenge')?.length !== 43 ||
      !/^[A-Za-z0-9_-]{43}$/.test(params.get('code_challenge') ?? '') ||
      !canonicalClientID(params.get('client_id')) ||
      !https(params.get('redirect_uri'), true)
    )
      throw invalid()
    return value.authorization_url
  } catch {
    throw invalid()
  }
}
export async function getDiscordMethod(signal?: AbortSignal): Promise<DiscordMethod> {
  const response = await request('GET', '/auth/discord', undefined, undefined, undefined, signal)
  if (response.status !== 200 || !method(response.data)) throw invalid()
  return { ...response.data }
}
export async function startDiscord(signal?: AbortSignal) {
  return authorization(
    await request('POST', '/auth/discord/start', {}, undefined, undefined, signal),
  )
}
export async function getDiscordIdentity(signal?: AbortSignal): Promise<DiscordIdentity> {
  const response = await request(
    'GET',
    '/account/identity/discord',
    undefined,
    undefined,
    undefined,
    signal,
  )
  if (response.status !== 200 || !reviewed(response) || !identity(response.data)) throw invalid()
  return { ...response.data }
}
export async function getDiscordConfig(signal?: AbortSignal): Promise<DiscordConfig> {
  const response = await request(
    'GET',
    '/admin/auth/discord',
    undefined,
    undefined,
    undefined,
    signal,
  )
  if (response.status !== 200 || !reviewed(response) || !config(response.data)) throw invalid()
  return { ...response.data }
}
export async function saveDiscordConfig(
  etag: string,
  input: DiscordConfigInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<DiscordConfig> {
  admitted(etag, csrf)
  if (!validDiscordConfig(input)) throw invalid()
  const captured = { ...input }
  const response = await request('PUT', '/admin/auth/discord', captured, csrf, etag, signal)
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
export async function setDiscordStatus(
  etag: string,
  input: { enabled: boolean; reason: string },
  csrf: string,
  signal?: AbortSignal,
): Promise<DiscordConfig> {
  admitted(etag, csrf)
  if (
    !object(input) ||
    !fields(input, ['enabled', 'reason']) ||
    typeof input.enabled !== 'boolean' ||
    !validApprovalReason(input.reason)
  )
    throw invalid()
  const captured = { ...input }
  const response = await request('PUT', '/admin/auth/discord/status', captured, csrf, etag, signal)
  if (
    response.status !== 200 ||
    !reviewed(response) ||
    !config(response.data) ||
    response.data.enabled !== captured.enabled
  )
    throw invalid()
  return { ...response.data }
}
export async function beginDiscordProof(
  action: 'bind' | 'verify',
  etag: string,
  input: DiscordLocalProof,
  required: boolean,
  csrf: string,
  signal?: AbortSignal,
) {
  admitted(etag, csrf)
  if (!validDiscordProof(input, required)) throw invalid()
  return authorization(
    await request(
      'POST',
      action === 'bind' ? '/account/identity/discord/bind' : '/admin/auth/discord/verify',
      { ...input, proof: { ...input.proof } },
      csrf,
      etag,
      signal,
    ),
  )
}
export async function unlinkDiscord(
  etag: string,
  input: DiscordLocalProof,
  required: boolean,
  csrf: string,
  signal?: AbortSignal,
): Promise<DiscordIdentity> {
  admitted(etag, csrf)
  if (!validDiscordProof(input, required)) throw invalid()
  const response = await request(
    'POST',
    '/account/identity/discord/unlink',
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
export async function readDiscordSession(signal?: AbortSignal): Promise<Session | null> {
  try {
    const response = await request('GET', '/auth/session', undefined, undefined, undefined, signal)
    if (response.status !== 200 || !session(response.data)) throw invalid()
    return { ...response.data, user: { ...response.data.user } }
  } catch (error) {
    if (error instanceof DiscordRequestError && error.status === 401 && !signal?.aborted)
      return null
    throw error
  }
}
export async function completeDiscord(
  csrf?: string,
  signal?: AbortSignal,
): Promise<DiscordCompletion> {
  const response = await request('POST', '/auth/discord/complete', {}, csrf, undefined, signal)
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
      challenge: value as unknown as Extract<DiscordCompletion, { kind: 'challenge' }>['challenge'],
    }
  throw invalid()
}

export async function abandonDiscord(csrf?: string, signal?: AbortSignal): Promise<void> {
  const response = await request('POST', '/auth/discord/abandon', {}, csrf, undefined, signal)
  if (
    response.status !== 204 ||
    (response.data !== '' && response.data !== undefined && response.data !== null)
  )
    throw invalid()
}
