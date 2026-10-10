import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  getDiscordConfig,
  getDiscordIdentity,
  getDiscordMethod,
  saveDiscordConfig,
  setDiscordStatus,
  validDiscordConfig,
  validDiscordProof,
  beginDiscordProof,
  startDiscord,
  unlinkDiscord,
  completeDiscord,
  readDiscordSession,
  abandonDiscord,
  DiscordRequestError,
} from './discord'
import type { DiscordConfigInput, DiscordLocalProof } from '@/types/discord'
const original = client.defaults.adapter,
  etag = 'a'.repeat(64)
const input: DiscordConfigInput = {
  name: 'Discord sign-in',
  client_id: '9007199254740993',
  callback_url: 'https://routex.example.test/api/v1/auth/discord/callback',
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
  const url = new URL('https://discord.com/oauth2/authorize')
  url.search = new URLSearchParams({
    response_type: 'code',
    client_id: '9007199254740993',
    redirect_uri: 'https://routex.example.test/api/v1/auth/discord/callback',
    scope: 'identify',
    state: 's'.repeat(43),
    code_challenge: 'c'.repeat(43),
    code_challenge_method: 'S256',
  }).toString()
  return url.href
}
describe('fixed Discord application HTTP boundary', () => {
  it('sends only fixed-profile configuration fields with original review and fresh CSRF', async () => {
    await expect(saveDiscordConfig(etag, input, 'fresh-csrf')).resolves.toEqual(config)
    expect(requests).toHaveLength(1)
    expect(requests[0].url).toBe('/admin/auth/discord')
    expect(requests[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    expect(JSON.parse(requests[0].data)).toEqual(input)
    expect(validDiscordConfig({ ...input, secret_action: 'keep', client_secret: '' })).toBe(true)
    expect(validDiscordConfig({ ...input, client_secret: ' space retained ' })).toBe(true)
  })
  it.each([
    { client_id: 'with space' },
    { client_id: '' },
    { client_id: 'a'.repeat(257) },
    { client_id: '0' },
    { client_id: '01' },
    { client_id: '18446744073709551616' },
    { client_id: '١' },
    { client_id: '1\n' },
    { client_id: '1\u2028' },
    { callback_url: 'http://routex.example.test/api/v1/auth/discord/callback' },
    { callback_url: 'https://routex.example.test/api/v1/auth/oauth/callback' },
    { callback_url: 'https://routex.example.test/api/v1/auth/discord/%63allback' },
    { callback_url: 'https://routex.example.test/api/v1/auth/discord/callback?' },
    { callback_url: 'https://user@routex.example.test/api/v1/auth/discord/callback' },
    { name: ' ' },
    { reason: '' },
    { reason: 'a'.repeat(1025) },
    { client_secret: 'a'.repeat(4097) },
    { client_secret: 'bad\nsecret' },
    { secret_action: 'keep', client_secret: 'must-be-empty' },
  ])('rejects invalid fixed configuration before I/O: %j', async (change) => {
    const invalid = { ...input, ...change } as DiscordConfigInput
    expect(validDiscordConfig(invalid)).toBe(false)
    await expect(saveDiscordConfig(etag, invalid, 'csrf')).rejects.toBeInstanceOf(
      DiscordRequestError,
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
    await expect(getDiscordConfig()).resolves.toEqual(data)
    for (const invalid of [
      { ...config, secret_configured: false },
      { ...config, enabled: true },
      { ...config, client_id: null },
      { ...config, subject: 'not-public' },
      { ...config, enabled: 'false' },
    ]) {
      data = invalid
      await expect(getDiscordConfig()).rejects.toBeInstanceOf(DiscordRequestError)
    }
    data = config
    headers.set('ETag', `"${'b'.repeat(64)}"`)
    await expect(getDiscordConfig()).rejects.toBeInstanceOf(DiscordRequestError)
    headers.set('ETag', `"${etag}"`)
    headers.delete('Cache-Control')
    await expect(getDiscordConfig()).rejects.toBeInstanceOf(DiscordRequestError)
    headers.set('Cache-Control', 'private, no-store')
    headers.delete('X-Content-Type-Options')
    await expect(getDiscordConfig()).rejects.toBeInstanceOf(DiscordRequestError)
  })
  it('keeps public unavailable blank and retained self binding distinct from availability', async () => {
    data = { available: false, name: '' }
    await expect(getDiscordMethod()).resolves.toEqual(data)
    data = { available: false, name: 'stale' }
    await expect(getDiscordMethod()).rejects.toBeInstanceOf(DiscordRequestError)
    data = { ...identity, available: false, bound: true }
    await expect(getDiscordIdentity()).resolves.toEqual(data)
    expect(requests.at(-1)?.url).toBe('/account/identity/discord')
    data = { ...identity, mfa_required: null }
    await expect(getDiscordIdentity()).rejects.toBeInstanceOf(DiscordRequestError)
  })
  it('admits only the exact Discord authorization host/path and no returned completion URL', async () => {
    data = {
      authorization_url: validAuthorization(),
    }
    await expect(startDiscord()).resolves.toBe(
      data && (data as { authorization_url: string }).authorization_url,
    )
    await beginDiscordProof('bind', etag, proof, false, 'csrf')
    expect(requests.at(-1)?.url).toBe('/account/identity/discord/bind')
    expect(JSON.parse(requests.at(-1)!.data)).toEqual(proof)
    await beginDiscordProof('verify', etag, proof, false, 'csrf')
    expect(requests.at(-1)?.url).toBe('/admin/auth/discord/verify')
    for (const authorization_url of [
      'http://discord.com/oauth2/authorize',
      'https://discord.com.evil.test/oauth2/authorize',
      'https://discord.com/settings',
      'https://discord.com:444/oauth2/authorize',
      'https://discord.com:443/oauth2/authorize',
      'https://discord.com/oauth2/%61uthorize',
      'https://discord.com/oauth2/authorize#fragment',
      'https://discord.com/oauth2/authorize?' + 'x'.repeat(8192),
    ]) {
      data = { authorization_url }
      await expect(startDiscord()).rejects.toBeInstanceOf(DiscordRequestError)
    }
    data = { kind: 'verified', redirect_url: 'https://evil.test' }
    await expect(completeDiscord('csrf')).rejects.toBeInstanceOf(DiscordRequestError)
  })
  it('requires exact local/native proofs without a fabricated off-mode MFA variant', async () => {
    expect(validDiscordProof(proof, false)).toBe(true)
    expect(validDiscordProof({ ...proof, proof: { code: '123456' } }, false)).toBe(false)
    expect(validDiscordProof({ ...proof, proof: { code: '123456' } }, true)).toBe(true)
    expect(validDiscordProof({ ...proof, proof: { recovery_code: 'recovery-code' } }, true)).toBe(
      true,
    )
    expect(
      validDiscordProof(
        {
          ...proof,
          proof: { code: '123456', recovery_code: 'recovery-code' },
        } as unknown as DiscordLocalProof,
        true,
      ),
    ).toBe(false)
    await expect(
      beginDiscordProof('bind', etag, { ...proof, password: '' }, false, 'csrf'),
    ).rejects.toBeInstanceOf(DiscordRequestError)
    expect(requests).toHaveLength(0)
  })
  it('rejects mismatched save/status/unlink receipts without optimistic success', async () => {
    data = { ...config, client_id: 'other' }
    await expect(saveDiscordConfig(etag, input, 'csrf')).rejects.toBeInstanceOf(DiscordRequestError)
    data = { ...config, enabled: false, verified: true }
    await expect(
      setDiscordStatus(etag, { enabled: true, reason: 'Reviewed enable' }, 'csrf'),
    ).rejects.toBeInstanceOf(DiscordRequestError)
    data = { ...identity, bound: true }
    await expect(unlinkDiscord(etag, proof, false, 'csrf')).rejects.toBeInstanceOf(
      DiscordRequestError,
    )
    data = identity
    await expect(unlinkDiscord(etag, proof, false, 'csrf')).resolves.toEqual(identity)
    expect(requests.at(-1)?.url).toBe('/account/identity/discord/unlink')
  })
  it('handles normal Session, bound, verified and native MFA as separate exact completion shapes', async () => {
    data = session
    await expect(completeDiscord()).resolves.toEqual({ kind: 'session', session })
    for (const kind of ['bound', 'verified']) {
      data = { kind }
      await expect(completeDiscord('csrf')).resolves.toEqual({ kind })
    }
    status = 202
    data = {
      mfa_required: true,
      challenge_token: 'a'.repeat(43),
      expires_at: '2026-10-10T00:00:00.123456789Z',
      methods: ['totp', 'recovery_code'],
    }
    await expect(completeDiscord()).resolves.toMatchObject({ kind: 'challenge' })
    data = { ...(data as Record<string, unknown>), methods: ['recovery_code'] }
    await expect(completeDiscord()).rejects.toBeInstanceOf(DiscordRequestError)
    status = 200
    data = { kind: 'challenge' }
    await expect(completeDiscord()).rejects.toBeInstanceOf(DiscordRequestError)
    status = 403
    data = {}
    await expect(completeDiscord('csrf')).rejects.toMatchObject({ status: 403 })
    expect(requests.every((row) => row.url === '/auth/discord/complete')).toBe(true)
  })
  it('accepts the existing Session privacy headers without relaxing Discord response privacy', async () => {
    headers.set('Cache-Control', 'no-store')
    data = session
    await expect(readDiscordSession()).resolves.toEqual(session)
    expect(requests.at(-1)?.url).toBe('/auth/session')
    await expect(completeDiscord('csrf')).rejects.toBeInstanceOf(DiscordRequestError)
    expect(requests.at(-1)?.url).toBe('/auth/discord/complete')
    data = config
    await expect(getDiscordConfig()).rejects.toBeInstanceOf(DiscordRequestError)
    data = session
    headers.delete('Cache-Control')
    await expect(readDiscordSession()).rejects.toBeInstanceOf(DiscordRequestError)
    headers.set('Cache-Control', 'no-store')
    headers.delete('X-Content-Type-Options')
    await expect(readDiscordSession()).rejects.toBeInstanceOf(DiscordRequestError)
  })
  it('reads Session transiently and requires exact empty204 abandonment with optional CSRF', async () => {
    data = session
    await expect(readDiscordSession()).resolves.toEqual(session)
    status = 401
    data = {}
    await expect(readDiscordSession()).resolves.toBeNull()
    status = 503
    await expect(readDiscordSession()).rejects.toMatchObject({ status: 503 })
    status = 204
    data = ''
    await expect(abandonDiscord('fresh')).resolves.toBeUndefined()
    expect(requests.at(-1)?.headers.get('X-CSRF-Token')).toBe('fresh')
    expect(JSON.parse(requests.at(-1)!.data)).toEqual({})
    status = 200
    await expect(abandonDiscord()).rejects.toBeInstanceOf(DiscordRequestError)
    status = 204
    data = { kind: 'canceled' }
    await expect(abandonDiscord()).rejects.toBeInstanceOf(DiscordRequestError)
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
    const pending = completeDiscord(undefined, controller.signal)
    controller.abort()
    release()
    await expect(pending).rejects.toBeInstanceOf(DiscordRequestError)
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
  const pending = saveDiscordConfig(etag, draft, 'csrf')
  draft.name = 'Edited after dispatch'
  release()
  await expect(pending).resolves.toEqual(config)
  expect(JSON.parse(requests[0].data).name).toBe(input.name)
})

it.each([
  'email',
  'offline_access',
  'prompt',
  'missing-state',
  'duplicate-state',
  'escaped-duplicate',
  'plain',
  'github-callback',
  'extra-query',
])('rejects Discord authorization profile drift before browser navigation: %s', async (mode) => {
  const url = new URL(validAuthorization())
  if (mode === 'email') url.searchParams.set('scope', 'identify email')
  if (mode === 'offline_access') url.searchParams.set('scope', 'identify offline_access')
  if (mode === 'prompt') url.searchParams.set('prompt', 'consent')
  if (mode === 'missing-state') url.searchParams.delete('state')
  if (mode === 'duplicate-state') url.searchParams.append('state', 's'.repeat(43))
  if (mode === 'escaped-duplicate') url.search += '&%73tate=' + 's'.repeat(43)
  if (mode === 'plain') url.searchParams.set('code_challenge_method', 'plain')
  if (mode === 'github-callback')
    url.searchParams.set('redirect_uri', 'https://routex.example.test/api/v1/auth/github/callback')
  if (mode === 'extra-query') url.searchParams.set('access_type', 'offline')
  data = { authorization_url: url.href }
  await expect(startDiscord()).rejects.toBeInstanceOf(DiscordRequestError)
  expect(requests).toHaveLength(1)
  expect(requests[0].url).toBe('/auth/discord/start')
})
it('requires no-referrer for Discord responses while preserving the exact Session header exception', async () => {
  headers.delete('Referrer-Policy')
  data = config
  await expect(getDiscordConfig()).rejects.toBeInstanceOf(DiscordRequestError)
  data = session
  headers.set('Cache-Control', 'no-store')
  await expect(readDiscordSession()).resolves.toEqual(session)
})

it('preserves exact large and maximum decimal client IDs and rejects numeric safe facts', async () => {
  for (const client_id of ['1', '9007199254740993', '18446744073709551615']) {
    const captured = { ...input, client_id }
    data = { ...config, client_id }
    expect(validDiscordConfig(captured)).toBe(true)
    await expect(saveDiscordConfig(etag, captured, 'fresh-csrf')).resolves.toEqual(data)
    expect(JSON.parse(requests.at(-1)!.data).client_id).toBe(client_id)
  }
  for (const client_id of [0, 9007199254740992, '0', '01', '18446744073709551616']) {
    data = { ...config, client_id }
    await expect(getDiscordConfig()).rejects.toBeInstanceOf(DiscordRequestError)
  }
})
it.each([
  'nonce',
  'client-zero',
  'client-overflow',
  'numeric-looking-scope',
  'state-newline',
  'challenge-newline',
])('rejects Discord-only authorization drift before navigation: %s', async (mode) => {
  const url = new URL(validAuthorization())
  if (mode === 'state-newline') url.searchParams.set('state', 's'.repeat(43) + '\n')
  if (mode === 'challenge-newline') url.searchParams.set('code_challenge', 'c'.repeat(43) + '\n')
  if (mode === 'nonce') url.searchParams.set('nonce', 'n'.repeat(43))
  if (mode === 'client-zero') url.searchParams.set('client_id', '0')
  if (mode === 'client-overflow') url.searchParams.set('client_id', '18446744073709551616')
  if (mode === 'numeric-looking-scope') url.searchParams.set('scope', 'identify guilds')
  data = { authorization_url: url.href }
  await expect(startDiscord()).rejects.toBeInstanceOf(DiscordRequestError)
  expect(requests).toHaveLength(1)
})
