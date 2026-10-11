import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  getTelegramConfig,
  getTelegramIdentity,
  getTelegramMethod,
  saveTelegramConfig,
  setTelegramStatus,
  validTelegramConfig,
  validTelegramProof,
  beginTelegramProof,
  startTelegram,
  unlinkTelegram,
  completeTelegram,
  readTelegramSession,
  abandonTelegram,
  TelegramRequestError,
} from './telegram'
import type { TelegramConfigInput, TelegramLocalProof } from '@/types/telegram'
const original = client.defaults.adapter,
  etag = 'a'.repeat(64)
const input: TelegramConfigInput = {
  name: 'Telegram sign-in',
  client_id: 'client-id',
  callback_url: 'https://routex.example.test/api/v1/auth/telegram/callback',
  secret_action: 'replace',
  client_secret: 'transient-secret',
  reason: 'Reviewed configuration',
}
const config = {
  name: input.name,
  client_id: input.client_id,
  callback_url: input.callback_url,
  secret_configured: true,
  enabled: false,
  verified: false,
  mfa_required: false,
  review_etag: etag,
}
const identity = {
  available: true,
  name: input.name,
  bound: false,
  mfa_required: false,
  review_etag: etag,
}
const proof = { password: 'local-password-proof', proof: {}, reason: 'Reviewed link' }
const session = {
  user: { id: 'usr_alice', name: 'Alice', email: 'alice@example.test', role: 'member' },
  csrf_token: 'csrf-current',
}
let data: unknown, status: number, headers: AxiosHeaders, requests: InternalAxiosRequestConfig[]
beforeEach(() => {
  data = { ...config }
  status = 200
  requests = []
  headers = new AxiosHeaders({
    ETag: `"${etag}"`,
    'Cache-Control': 'private, no-store',
    'X-Content-Type-Options': 'nosniff',
    'Referrer-Policy': 'no-referrer',
  })
  client.defaults.adapter = async (request) => {
    requests.push(request)
    return { config: request, status, statusText: '', data, headers }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
function validAuthorization() {
  const url = new URL('https://oauth.telegram.org/auth')
  url.search = new URLSearchParams({
    response_type: 'code',
    client_id: 'exact-client',
    redirect_uri: 'https://routex.example.test/api/v1/auth/telegram/callback',
    scope: 'openid',
    state: 's'.repeat(43),
    nonce: 'n'.repeat(43),
    code_challenge: 'c'.repeat(43),
    code_challenge_method: 'S256',
  }).toString()
  return url.href
}
describe('fixed Telegram application HTTP boundary', () => {
  it('sends only fixed-profile configuration fields with original review and fresh CSRF', async () => {
    await expect(saveTelegramConfig(etag, input, 'fresh-csrf')).resolves.toEqual(config)
    expect(requests).toHaveLength(1)
    expect(requests[0].url).toBe('/admin/auth/telegram')
    expect(requests[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    expect(JSON.parse(requests[0].data)).toEqual(input)
    expect(validTelegramConfig({ ...input, secret_action: 'keep', client_secret: '' })).toBe(true)
    expect(validTelegramConfig({ ...input, client_secret: ' space retained ' })).toBe(true)
  })
  it.each([
    { client_id: 'with space' },
    { bot_token: 'not-an-oidc-client-secret' },
    { bot_name: 'not-an-identity' },
    { client_id: '' },
    { client_id: 'a'.repeat(257) },
    { callback_url: 'http://routex.example.test/api/v1/auth/telegram/callback' },
    { callback_url: 'https://routex.example.test/api/v1/auth/oauth/callback' },
    { callback_url: 'https://routex.example.test/api/v1/auth/telegram/%63allback' },
    { callback_url: 'https://routex.example.test/api/v1/auth/telegram/callback?' },
    { callback_url: 'https://user@routex.example.test/api/v1/auth/telegram/callback' },
    { name: ' ' },
    { reason: '' },
    { reason: 'a'.repeat(1025) },
    { client_secret: 'a'.repeat(4097) },
    { client_secret: 'bad\nsecret' },
    { secret_action: 'keep', client_secret: 'must-be-empty' },
  ])('rejects invalid fixed configuration before I/O: %j', async (change) => {
    const invalid = { ...input, ...change } as TelegramConfigInput
    expect(validTelegramConfig(invalid)).toBe(false)
    await expect(saveTelegramConfig(etag, invalid, 'csrf')).rejects.toBeInstanceOf(
      TelegramRequestError,
    )
    expect(requests).toHaveLength(0)
  })
  it('decodes only coherent initial or configured safe facts and matching response privacy/review', async () => {
    data = {
      name: '',
      client_id: '',
      callback_url: '',
      secret_configured: false,
      enabled: false,
      verified: false,
      mfa_required: true,
      review_etag: etag,
    }
    await expect(getTelegramConfig()).resolves.toEqual(data)
    for (const invalid of [
      { ...config, secret_configured: false },
      { ...config, enabled: true },
      { ...config, client_id: null },
      { ...config, subject: 'not-public' },
      { ...config, enabled: 'false' },
    ]) {
      data = invalid
      await expect(getTelegramConfig()).rejects.toBeInstanceOf(TelegramRequestError)
    }
    data = config
    headers.set('ETag', `"${'b'.repeat(64)}"`)
    await expect(getTelegramConfig()).rejects.toBeInstanceOf(TelegramRequestError)
    headers.set('ETag', `"${etag}"`)
    headers.delete('Cache-Control')
    await expect(getTelegramConfig()).rejects.toBeInstanceOf(TelegramRequestError)
    headers.set('Cache-Control', 'private, no-store')
    headers.delete('X-Content-Type-Options')
    await expect(getTelegramConfig()).rejects.toBeInstanceOf(TelegramRequestError)
  })
  it('keeps public unavailable blank and retained self binding distinct from availability', async () => {
    data = { available: false, name: '' }
    await expect(getTelegramMethod()).resolves.toEqual(data)
    data = { available: false, name: 'stale' }
    await expect(getTelegramMethod()).rejects.toBeInstanceOf(TelegramRequestError)
    data = { ...identity, available: false, bound: true }
    await expect(getTelegramIdentity()).resolves.toEqual(data)
    expect(requests.at(-1)?.url).toBe('/account/identity/telegram')
    data = { ...identity, mfa_required: null }
    await expect(getTelegramIdentity()).rejects.toBeInstanceOf(TelegramRequestError)
  })
  it('admits only the exact Telegram authorization host/path and no returned completion URL', async () => {
    data = {
      authorization_url: validAuthorization(),
    }
    await expect(startTelegram()).resolves.toBe(
      data && (data as { authorization_url: string }).authorization_url,
    )
    await beginTelegramProof('bind', etag, proof, false, 'csrf')
    expect(requests.at(-1)?.url).toBe('/account/identity/telegram/bind')
    expect(JSON.parse(requests.at(-1)!.data)).toEqual(proof)
    await beginTelegramProof('verify', etag, proof, false, 'csrf')
    expect(requests.at(-1)?.url).toBe('/admin/auth/telegram/verify')
    for (const authorization_url of [
      'http://oauth.telegram.org/auth',
      'https://accounts.google.com/o/oauth2/v2/auth',
      'https://discord.com/oauth2/authorize',
      'https://oauth.telegram.org.evil.test/auth',
      'https://oauth.telegram.org/settings',
      'https://oauth.telegram.org:444/auth',
      'https://oauth.telegram.org/auth#fragment',
      'https://oauth.telegram.org/auth?' + 'x'.repeat(8192),
    ]) {
      data = { authorization_url }
      await expect(startTelegram()).rejects.toBeInstanceOf(TelegramRequestError)
    }
    data = { kind: 'verified', redirect_url: 'https://evil.test' }
    await expect(completeTelegram('csrf')).rejects.toBeInstanceOf(TelegramRequestError)
  })
  it('requires exact local/native proofs without a fabricated off-mode MFA variant', async () => {
    expect(validTelegramProof(proof, false)).toBe(true)
    expect(validTelegramProof({ ...proof, proof: { code: '123456' } }, false)).toBe(false)
    expect(validTelegramProof({ ...proof, proof: { code: '123456' } }, true)).toBe(true)
    expect(validTelegramProof({ ...proof, proof: { recovery_code: 'recovery-code' } }, true)).toBe(
      true,
    )
    expect(
      validTelegramProof(
        {
          ...proof,
          proof: { code: '123456', recovery_code: 'recovery-code' },
        } as unknown as TelegramLocalProof,
        true,
      ),
    ).toBe(false)
    await expect(
      beginTelegramProof('bind', etag, { ...proof, password: '' }, false, 'csrf'),
    ).rejects.toBeInstanceOf(TelegramRequestError)
    expect(requests).toHaveLength(0)
  })
  it('rejects mismatched save/status/unlink receipts without optimistic success', async () => {
    data = { ...config, client_id: 'other' }
    await expect(saveTelegramConfig(etag, input, 'csrf')).rejects.toBeInstanceOf(
      TelegramRequestError,
    )
    data = { ...config, enabled: false, verified: true }
    await expect(
      setTelegramStatus(etag, { enabled: true, reason: 'Reviewed enable' }, 'csrf'),
    ).rejects.toBeInstanceOf(TelegramRequestError)
    data = { ...identity, bound: true }
    await expect(unlinkTelegram(etag, proof, false, 'csrf')).rejects.toBeInstanceOf(
      TelegramRequestError,
    )
    data = identity
    await expect(unlinkTelegram(etag, proof, false, 'csrf')).resolves.toEqual(identity)
    expect(requests.at(-1)?.url).toBe('/account/identity/telegram/unlink')
  })
  it('handles normal Session, bound, verified and native MFA as separate exact completion shapes', async () => {
    data = session
    await expect(completeTelegram()).resolves.toEqual({ kind: 'session', session })
    for (const kind of ['bound', 'verified']) {
      data = { kind }
      await expect(completeTelegram('csrf')).resolves.toEqual({ kind })
    }
    status = 202
    data = {
      mfa_required: true,
      challenge_token: 'a'.repeat(43),
      expires_at: '2026-10-10T00:00:00.123456789Z',
      methods: ['totp', 'recovery_code'],
    }
    await expect(completeTelegram()).resolves.toMatchObject({ kind: 'challenge' })
    data = { ...(data as Record<string, unknown>), methods: ['recovery_code'] }
    await expect(completeTelegram()).rejects.toBeInstanceOf(TelegramRequestError)
    status = 200
    data = { kind: 'challenge' }
    await expect(completeTelegram()).rejects.toBeInstanceOf(TelegramRequestError)
    status = 403
    data = {}
    await expect(completeTelegram('csrf')).rejects.toMatchObject({ status: 403 })
    expect(requests.every((row) => row.url === '/auth/telegram/complete')).toBe(true)
  })
  it('accepts the existing Session privacy headers without relaxing Telegram response privacy', async () => {
    headers.set('Cache-Control', 'no-store')
    data = session
    await expect(readTelegramSession()).resolves.toEqual(session)
    expect(requests.at(-1)?.url).toBe('/auth/session')
    await expect(completeTelegram('csrf')).rejects.toBeInstanceOf(TelegramRequestError)
    expect(requests.at(-1)?.url).toBe('/auth/telegram/complete')
    data = config
    await expect(getTelegramConfig()).rejects.toBeInstanceOf(TelegramRequestError)
    data = session
    headers.delete('Cache-Control')
    await expect(readTelegramSession()).rejects.toBeInstanceOf(TelegramRequestError)
    headers.set('Cache-Control', 'no-store')
    headers.delete('X-Content-Type-Options')
    await expect(readTelegramSession()).rejects.toBeInstanceOf(TelegramRequestError)
  })
  it('reads Session transiently and requires exact empty204 abandonment with optional CSRF', async () => {
    data = session
    await expect(readTelegramSession()).resolves.toEqual(session)
    status = 401
    data = {}
    await expect(readTelegramSession()).resolves.toBeNull()
    status = 503
    await expect(readTelegramSession()).rejects.toMatchObject({ status: 503 })
    status = 204
    data = ''
    await expect(abandonTelegram('fresh')).resolves.toBeUndefined()
    expect(requests.at(-1)?.headers.get('X-CSRF-Token')).toBe('fresh')
    expect(JSON.parse(requests.at(-1)!.data)).toEqual({})
    status = 200
    await expect(abandonTelegram()).rejects.toBeInstanceOf(TelegramRequestError)
    status = 204
    data = { kind: 'canceled' }
    await expect(abandonTelegram()).rejects.toBeInstanceOf(TelegramRequestError)
  })
  it('does not turn an ignored-abort response into a successful proof receipt', async () => {
    let release!: () => void
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    client.defaults.adapter = async (request) => {
      requests.push(request)
      await gate
      return { config: request, status: 200, statusText: '', data: session, headers }
    }
    const controller = new AbortController()
    const pending = completeTelegram(undefined, controller.signal)
    controller.abort()
    release()
    await expect(pending).rejects.toBeInstanceOf(TelegramRequestError)
    expect(requests).toHaveLength(1)
  })
})

it('compares a save receipt to captured input even if the caller edits its public draft in flight', async () => {
  let release!: () => void
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  client.defaults.adapter = async (request) => {
    requests.push(request)
    await gate
    return { config: request, status: 200, statusText: '', data: config, headers }
  }
  const draft = { ...input }
  const pending = saveTelegramConfig(etag, draft, 'csrf')
  draft.name = 'Edited after dispatch'
  release()
  await expect(pending).resolves.toEqual(config)
  expect(JSON.parse(requests[0].data).name).toBe(input.name)
})

it.each([
  'email',
  'profile',
  'offline_access',
  'prompt',
  'missing-nonce',
  'duplicate-state',
  'escaped-duplicate',
  'plain',
  'github-callback',
  'extra-query',
])('rejects Telegram authorization profile drift before browser navigation: %s', async (mode) => {
  const url = new URL(validAuthorization())
  if (mode === 'email') url.searchParams.set('scope', 'openid email')
  if (mode === 'profile') url.searchParams.set('scope', 'openid profile')
  if (mode === 'offline_access') url.searchParams.set('scope', 'openid offline_access')
  if (mode === 'prompt') url.searchParams.set('prompt', 'consent')
  if (mode === 'missing-nonce') url.searchParams.delete('nonce')
  if (mode === 'duplicate-state') url.searchParams.append('state', 's'.repeat(43))
  if (mode === 'escaped-duplicate') url.search += '&%73tate=' + 's'.repeat(43)
  if (mode === 'plain') url.searchParams.set('code_challenge_method', 'plain')
  if (mode === 'github-callback')
    url.searchParams.set('redirect_uri', 'https://routex.example.test/api/v1/auth/github/callback')
  if (mode === 'extra-query') url.searchParams.set('access_type', 'offline')
  data = { authorization_url: url.href }
  await expect(startTelegram()).rejects.toBeInstanceOf(TelegramRequestError)
  expect(requests).toHaveLength(1)
  expect(requests[0].url).toBe('/auth/telegram/start')
})
it('requires no-referrer for Telegram responses while preserving the exact Session header exception', async () => {
  headers.delete('Referrer-Policy')
  data = config
  await expect(getTelegramConfig()).rejects.toBeInstanceOf(TelegramRequestError)
  data = session
  headers.set('Cache-Control', 'no-store')
  await expect(readTelegramSession()).resolves.toEqual(session)
})
