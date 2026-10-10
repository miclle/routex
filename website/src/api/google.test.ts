import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  getGoogleConfig,
  getGoogleIdentity,
  getGoogleMethod,
  saveGoogleConfig,
  setGoogleStatus,
  validGoogleConfig,
  validGoogleProof,
  beginGoogleProof,
  startGoogle,
  unlinkGoogle,
  completeGoogle,
  readGoogleSession,
  abandonGoogle,
  GoogleRequestError,
} from './google'
import type { GoogleConfigInput, GoogleLocalProof } from '@/types/google'
const original = client.defaults.adapter,
  etag = 'a'.repeat(64)
const input: GoogleConfigInput = {
  name: 'Google sign-in',
  client_id: 'client-id',
  callback_url: 'https://routex.example.test/api/v1/auth/google/callback',
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
  const url = new URL('https://accounts.google.com/o/oauth2/v2/auth')
  url.search = new URLSearchParams({
    response_type: 'code',
    client_id: 'exact-client',
    redirect_uri: 'https://routex.example.test/api/v1/auth/google/callback',
    scope: 'openid profile',
    state: 's'.repeat(43),
    nonce: 'n'.repeat(43),
    code_challenge: 'c'.repeat(43),
    code_challenge_method: 'S256',
  }).toString()
  return url.href
}
describe('fixed Google application HTTP boundary', () => {
  it('sends only fixed-profile configuration fields with original review and fresh CSRF', async () => {
    await expect(saveGoogleConfig(etag, input, 'fresh-csrf')).resolves.toEqual(config)
    expect(requests).toHaveLength(1)
    expect(requests[0].url).toBe('/admin/auth/google')
    expect(requests[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    expect(JSON.parse(requests[0].data)).toEqual(input)
    expect(validGoogleConfig({ ...input, secret_action: 'keep', client_secret: '' })).toBe(true)
    expect(validGoogleConfig({ ...input, client_secret: ' space retained ' })).toBe(true)
  })
  it.each([
    { client_id: 'with space' },
    { client_id: '' },
    { client_id: 'a'.repeat(257) },
    { callback_url: 'http://routex.example.test/api/v1/auth/google/callback' },
    { callback_url: 'https://routex.example.test/api/v1/auth/oauth/callback' },
    { callback_url: 'https://routex.example.test/api/v1/auth/google/%63allback' },
    { callback_url: 'https://routex.example.test/api/v1/auth/google/callback?' },
    { callback_url: 'https://user@routex.example.test/api/v1/auth/google/callback' },
    { name: ' ' },
    { reason: '' },
    { reason: 'a'.repeat(1025) },
    { client_secret: 'a'.repeat(4097) },
    { client_secret: 'bad\nsecret' },
    { secret_action: 'keep', client_secret: 'must-be-empty' },
  ])('rejects invalid fixed configuration before I/O: %j', async (change) => {
    const invalid = { ...input, ...change } as GoogleConfigInput
    expect(validGoogleConfig(invalid)).toBe(false)
    await expect(saveGoogleConfig(etag, invalid, 'csrf')).rejects.toBeInstanceOf(GoogleRequestError)
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
    await expect(getGoogleConfig()).resolves.toEqual(data)
    for (const invalid of [
      { ...config, secret_configured: false },
      { ...config, enabled: true },
      { ...config, client_id: null },
      { ...config, subject: 'not-public' },
      { ...config, enabled: 'false' },
    ]) {
      data = invalid
      await expect(getGoogleConfig()).rejects.toBeInstanceOf(GoogleRequestError)
    }
    data = config
    headers.set('ETag', `"${'b'.repeat(64)}"`)
    await expect(getGoogleConfig()).rejects.toBeInstanceOf(GoogleRequestError)
    headers.set('ETag', `"${etag}"`)
    headers.delete('Cache-Control')
    await expect(getGoogleConfig()).rejects.toBeInstanceOf(GoogleRequestError)
    headers.set('Cache-Control', 'private, no-store')
    headers.delete('X-Content-Type-Options')
    await expect(getGoogleConfig()).rejects.toBeInstanceOf(GoogleRequestError)
  })
  it('keeps public unavailable blank and retained self binding distinct from availability', async () => {
    data = { available: false, name: '' }
    await expect(getGoogleMethod()).resolves.toEqual(data)
    data = { available: false, name: 'stale' }
    await expect(getGoogleMethod()).rejects.toBeInstanceOf(GoogleRequestError)
    data = { ...identity, available: false, bound: true }
    await expect(getGoogleIdentity()).resolves.toEqual(data)
    expect(requests.at(-1)?.url).toBe('/account/identity/google')
    data = { ...identity, mfa_required: null }
    await expect(getGoogleIdentity()).rejects.toBeInstanceOf(GoogleRequestError)
  })
  it('admits only the exact Google authorization host/path and no returned completion URL', async () => {
    data = {
      authorization_url: validAuthorization(),
    }
    await expect(startGoogle()).resolves.toBe(
      data && (data as { authorization_url: string }).authorization_url,
    )
    await beginGoogleProof('bind', etag, proof, false, 'csrf')
    expect(requests.at(-1)?.url).toBe('/account/identity/google/bind')
    expect(JSON.parse(requests.at(-1)!.data)).toEqual(proof)
    await beginGoogleProof('verify', etag, proof, false, 'csrf')
    expect(requests.at(-1)?.url).toBe('/admin/auth/google/verify')
    for (const authorization_url of [
      'http://accounts.google.com/o/oauth2/v2/auth',
      'https://accounts.google.com.evil.test/o/oauth2/v2/auth',
      'https://accounts.google.com/settings',
      'https://accounts.google.com:444/o/oauth2/v2/auth',
      'https://accounts.google.com/o/oauth2/v2/auth#fragment',
      'https://accounts.google.com/o/oauth2/v2/auth?' + 'x'.repeat(8192),
    ]) {
      data = { authorization_url }
      await expect(startGoogle()).rejects.toBeInstanceOf(GoogleRequestError)
    }
    data = { kind: 'verified', redirect_url: 'https://evil.test' }
    await expect(completeGoogle('csrf')).rejects.toBeInstanceOf(GoogleRequestError)
  })
  it('requires exact local/native proofs without a fabricated off-mode MFA variant', async () => {
    expect(validGoogleProof(proof, false)).toBe(true)
    expect(validGoogleProof({ ...proof, proof: { code: '123456' } }, false)).toBe(false)
    expect(validGoogleProof({ ...proof, proof: { code: '123456' } }, true)).toBe(true)
    expect(validGoogleProof({ ...proof, proof: { recovery_code: 'recovery-code' } }, true)).toBe(
      true,
    )
    expect(
      validGoogleProof(
        {
          ...proof,
          proof: { code: '123456', recovery_code: 'recovery-code' },
        } as unknown as GoogleLocalProof,
        true,
      ),
    ).toBe(false)
    await expect(
      beginGoogleProof('bind', etag, { ...proof, password: '' }, false, 'csrf'),
    ).rejects.toBeInstanceOf(GoogleRequestError)
    expect(requests).toHaveLength(0)
  })
  it('rejects mismatched save/status/unlink receipts without optimistic success', async () => {
    data = { ...config, client_id: 'other' }
    await expect(saveGoogleConfig(etag, input, 'csrf')).rejects.toBeInstanceOf(GoogleRequestError)
    data = { ...config, enabled: false, verified: true }
    await expect(
      setGoogleStatus(etag, { enabled: true, reason: 'Reviewed enable' }, 'csrf'),
    ).rejects.toBeInstanceOf(GoogleRequestError)
    data = { ...identity, bound: true }
    await expect(unlinkGoogle(etag, proof, false, 'csrf')).rejects.toBeInstanceOf(
      GoogleRequestError,
    )
    data = identity
    await expect(unlinkGoogle(etag, proof, false, 'csrf')).resolves.toEqual(identity)
    expect(requests.at(-1)?.url).toBe('/account/identity/google/unlink')
  })
  it('handles normal Session, bound, verified and native MFA as separate exact completion shapes', async () => {
    data = session
    await expect(completeGoogle()).resolves.toEqual({ kind: 'session', session })
    for (const kind of ['bound', 'verified']) {
      data = { kind }
      await expect(completeGoogle('csrf')).resolves.toEqual({ kind })
    }
    status = 202
    data = {
      mfa_required: true,
      challenge_token: 'a'.repeat(43),
      expires_at: '2026-10-10T00:00:00.123456789Z',
      methods: ['totp', 'recovery_code'],
    }
    await expect(completeGoogle()).resolves.toMatchObject({ kind: 'challenge' })
    data = { ...(data as Record<string, unknown>), methods: ['recovery_code'] }
    await expect(completeGoogle()).rejects.toBeInstanceOf(GoogleRequestError)
    status = 200
    data = { kind: 'challenge' }
    await expect(completeGoogle()).rejects.toBeInstanceOf(GoogleRequestError)
    status = 403
    data = {}
    await expect(completeGoogle('csrf')).rejects.toMatchObject({ status: 403 })
    expect(requests.every((row) => row.url === '/auth/google/complete')).toBe(true)
  })
  it('accepts the existing Session privacy headers without relaxing Google response privacy', async () => {
    headers.set('Cache-Control', 'no-store')
    data = session
    await expect(readGoogleSession()).resolves.toEqual(session)
    expect(requests.at(-1)?.url).toBe('/auth/session')
    await expect(completeGoogle('csrf')).rejects.toBeInstanceOf(GoogleRequestError)
    expect(requests.at(-1)?.url).toBe('/auth/google/complete')
    data = config
    await expect(getGoogleConfig()).rejects.toBeInstanceOf(GoogleRequestError)
    data = session
    headers.delete('Cache-Control')
    await expect(readGoogleSession()).rejects.toBeInstanceOf(GoogleRequestError)
    headers.set('Cache-Control', 'no-store')
    headers.delete('X-Content-Type-Options')
    await expect(readGoogleSession()).rejects.toBeInstanceOf(GoogleRequestError)
  })
  it('reads Session transiently and requires exact empty204 abandonment with optional CSRF', async () => {
    data = session
    await expect(readGoogleSession()).resolves.toEqual(session)
    status = 401
    data = {}
    await expect(readGoogleSession()).resolves.toBeNull()
    status = 503
    await expect(readGoogleSession()).rejects.toMatchObject({ status: 503 })
    status = 204
    data = ''
    await expect(abandonGoogle('fresh')).resolves.toBeUndefined()
    expect(requests.at(-1)?.headers.get('X-CSRF-Token')).toBe('fresh')
    expect(JSON.parse(requests.at(-1)!.data)).toEqual({})
    status = 200
    await expect(abandonGoogle()).rejects.toBeInstanceOf(GoogleRequestError)
    status = 204
    data = { kind: 'canceled' }
    await expect(abandonGoogle()).rejects.toBeInstanceOf(GoogleRequestError)
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
    const pending = completeGoogle(undefined, controller.signal)
    controller.abort()
    release()
    await expect(pending).rejects.toBeInstanceOf(GoogleRequestError)
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
  const pending = saveGoogleConfig(etag, draft, 'csrf')
  draft.name = 'Edited after dispatch'
  release()
  await expect(pending).resolves.toEqual(config)
  expect(JSON.parse(requests[0].data).name).toBe(input.name)
})

it.each([
  'email',
  'offline_access',
  'prompt',
  'missing-nonce',
  'duplicate-state',
  'escaped-duplicate',
  'plain',
  'github-callback',
  'extra-query',
])('rejects Google authorization profile drift before browser navigation: %s', async (mode) => {
  const url = new URL(validAuthorization())
  if (mode === 'email') url.searchParams.set('scope', 'openid profile email')
  if (mode === 'offline_access') url.searchParams.set('scope', 'openid profile offline_access')
  if (mode === 'prompt') url.searchParams.set('prompt', 'consent')
  if (mode === 'missing-nonce') url.searchParams.delete('nonce')
  if (mode === 'duplicate-state') url.searchParams.append('state', 's'.repeat(43))
  if (mode === 'escaped-duplicate') url.search += '&%73tate=' + 's'.repeat(43)
  if (mode === 'plain') url.searchParams.set('code_challenge_method', 'plain')
  if (mode === 'github-callback')
    url.searchParams.set('redirect_uri', 'https://routex.example.test/api/v1/auth/github/callback')
  if (mode === 'extra-query') url.searchParams.set('access_type', 'offline')
  data = { authorization_url: url.href }
  await expect(startGoogle()).rejects.toBeInstanceOf(GoogleRequestError)
  expect(requests).toHaveLength(1)
  expect(requests[0].url).toBe('/auth/google/start')
})
it('requires no-referrer for Google responses while preserving the exact Session header exception', async () => {
  headers.delete('Referrer-Policy')
  data = config
  await expect(getGoogleConfig()).rejects.toBeInstanceOf(GoogleRequestError)
  data = session
  headers.set('Cache-Control', 'no-store')
  await expect(readGoogleSession()).resolves.toEqual(session)
})
