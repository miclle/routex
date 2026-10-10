import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  getGitHubConfig,
  getGitHubIdentity,
  getGitHubMethod,
  saveGitHubConfig,
  setGitHubStatus,
  validGitHubConfig,
  validGitHubProof,
  beginGitHubProof,
  startGitHub,
  unlinkGitHub,
  completeGitHub,
  readGitHubSession,
  abandonGitHub,
  GitHubRequestError,
} from './github'
import type { GitHubConfigInput, GitHubLocalProof } from '@/types/github'
const original = client.defaults.adapter,
  etag = 'a'.repeat(64)
const input: GitHubConfigInput = {
  name: 'GitHub sign-in',
  client_id: 'client-id',
  callback_url: 'https://routex.example.test/api/v1/auth/github/callback',
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
  })
  client.defaults.adapter = async (request) => {
    requests.push(request)
    return { config: request, status, statusText: '', data, headers }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
describe('fixed GitHub application HTTP boundary', () => {
  it('sends only fixed-profile configuration fields with original review and fresh CSRF', async () => {
    await expect(saveGitHubConfig(etag, input, 'fresh-csrf')).resolves.toEqual(config)
    expect(requests).toHaveLength(1)
    expect(requests[0].url).toBe('/admin/auth/github')
    expect(requests[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    expect(JSON.parse(requests[0].data)).toEqual(input)
    expect(validGitHubConfig({ ...input, secret_action: 'keep', client_secret: '' })).toBe(true)
    expect(validGitHubConfig({ ...input, client_secret: ' space retained ' })).toBe(true)
  })
  it.each([
    { client_id: 'with space' },
    { client_id: '' },
    { client_id: 'a'.repeat(257) },
    { callback_url: 'http://routex.example.test/api/v1/auth/github/callback' },
    { callback_url: 'https://routex.example.test/api/v1/auth/oauth/callback' },
    { callback_url: 'https://routex.example.test/api/v1/auth/github/%63allback' },
    { callback_url: 'https://routex.example.test/api/v1/auth/github/callback?' },
    { callback_url: 'https://user@routex.example.test/api/v1/auth/github/callback' },
    { name: ' ' },
    { reason: '' },
    { reason: 'a'.repeat(1025) },
    { client_secret: 'a'.repeat(4097) },
    { client_secret: 'bad\nsecret' },
    { secret_action: 'keep', client_secret: 'must-be-empty' },
  ])('rejects invalid fixed configuration before I/O: %j', async (change) => {
    const invalid = { ...input, ...change } as GitHubConfigInput
    expect(validGitHubConfig(invalid)).toBe(false)
    await expect(saveGitHubConfig(etag, invalid, 'csrf')).rejects.toBeInstanceOf(GitHubRequestError)
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
    await expect(getGitHubConfig()).resolves.toEqual(data)
    for (const invalid of [
      { ...config, secret_configured: false },
      { ...config, enabled: true },
      { ...config, client_id: null },
      { ...config, subject: 'not-public' },
      { ...config, enabled: 'false' },
    ]) {
      data = invalid
      await expect(getGitHubConfig()).rejects.toBeInstanceOf(GitHubRequestError)
    }
    data = config
    headers.set('ETag', `"${'b'.repeat(64)}"`)
    await expect(getGitHubConfig()).rejects.toBeInstanceOf(GitHubRequestError)
    headers.set('ETag', `"${etag}"`)
    headers.delete('Cache-Control')
    await expect(getGitHubConfig()).rejects.toBeInstanceOf(GitHubRequestError)
    headers.set('Cache-Control', 'private, no-store')
    headers.delete('X-Content-Type-Options')
    await expect(getGitHubConfig()).rejects.toBeInstanceOf(GitHubRequestError)
  })
  it('keeps public unavailable blank and retained self binding distinct from availability', async () => {
    data = { available: false, name: '' }
    await expect(getGitHubMethod()).resolves.toEqual(data)
    data = { available: false, name: 'stale' }
    await expect(getGitHubMethod()).rejects.toBeInstanceOf(GitHubRequestError)
    data = { ...identity, available: false, bound: true }
    await expect(getGitHubIdentity()).resolves.toEqual(data)
    expect(requests.at(-1)?.url).toBe('/account/identity/github')
    data = { ...identity, mfa_required: null }
    await expect(getGitHubIdentity()).rejects.toBeInstanceOf(GitHubRequestError)
  })
  it('admits only the exact GitHub authorization host/path and no returned completion URL', async () => {
    data = {
      authorization_url: 'https://github.com/login/oauth/authorize?state=opaque&client_id=exact',
    }
    await expect(startGitHub()).resolves.toBe(
      data && (data as { authorization_url: string }).authorization_url,
    )
    await beginGitHubProof('bind', etag, proof, false, 'csrf')
    expect(requests.at(-1)?.url).toBe('/account/identity/github/bind')
    expect(JSON.parse(requests.at(-1)!.data)).toEqual(proof)
    await beginGitHubProof('verify', etag, proof, false, 'csrf')
    expect(requests.at(-1)?.url).toBe('/admin/auth/github/verify')
    for (const authorization_url of [
      'http://github.com/login/oauth/authorize',
      'https://github.com.evil.test/login/oauth/authorize',
      'https://github.com/settings',
      'https://github.com:444/login/oauth/authorize',
      'https://github.com/login/oauth/authorize#fragment',
      'https://github.com/login/oauth/authorize?' + 'x'.repeat(8192),
    ]) {
      data = { authorization_url }
      await expect(startGitHub()).rejects.toBeInstanceOf(GitHubRequestError)
    }
    data = { kind: 'verified', redirect_url: 'https://evil.test' }
    await expect(completeGitHub('csrf')).rejects.toBeInstanceOf(GitHubRequestError)
  })
  it('requires exact local/native proofs without a fabricated off-mode MFA variant', async () => {
    expect(validGitHubProof(proof, false)).toBe(true)
    expect(validGitHubProof({ ...proof, proof: { code: '123456' } }, false)).toBe(false)
    expect(validGitHubProof({ ...proof, proof: { code: '123456' } }, true)).toBe(true)
    expect(validGitHubProof({ ...proof, proof: { recovery_code: 'recovery-code' } }, true)).toBe(
      true,
    )
    expect(
      validGitHubProof(
        {
          ...proof,
          proof: { code: '123456', recovery_code: 'recovery-code' },
        } as unknown as GitHubLocalProof,
        true,
      ),
    ).toBe(false)
    await expect(
      beginGitHubProof('bind', etag, { ...proof, password: '' }, false, 'csrf'),
    ).rejects.toBeInstanceOf(GitHubRequestError)
    expect(requests).toHaveLength(0)
  })
  it('rejects mismatched save/status/unlink receipts without optimistic success', async () => {
    data = { ...config, client_id: 'other' }
    await expect(saveGitHubConfig(etag, input, 'csrf')).rejects.toBeInstanceOf(GitHubRequestError)
    data = { ...config, enabled: false, verified: true }
    await expect(
      setGitHubStatus(etag, { enabled: true, reason: 'Reviewed enable' }, 'csrf'),
    ).rejects.toBeInstanceOf(GitHubRequestError)
    data = { ...identity, bound: true }
    await expect(unlinkGitHub(etag, proof, false, 'csrf')).rejects.toBeInstanceOf(
      GitHubRequestError,
    )
    data = identity
    await expect(unlinkGitHub(etag, proof, false, 'csrf')).resolves.toEqual(identity)
    expect(requests.at(-1)?.url).toBe('/account/identity/github/unlink')
  })
  it('handles normal Session, bound, verified and native MFA as separate exact completion shapes', async () => {
    data = session
    await expect(completeGitHub()).resolves.toEqual({ kind: 'session', session })
    for (const kind of ['bound', 'verified']) {
      data = { kind }
      await expect(completeGitHub('csrf')).resolves.toEqual({ kind })
    }
    status = 202
    data = {
      mfa_required: true,
      challenge_token: 'a'.repeat(43),
      expires_at: '2026-10-10T00:00:00.123456789Z',
      methods: ['totp', 'recovery_code'],
    }
    await expect(completeGitHub()).resolves.toMatchObject({ kind: 'challenge' })
    data = { ...(data as Record<string, unknown>), methods: ['recovery_code'] }
    await expect(completeGitHub()).rejects.toBeInstanceOf(GitHubRequestError)
    status = 200
    data = { kind: 'challenge' }
    await expect(completeGitHub()).rejects.toBeInstanceOf(GitHubRequestError)
    status = 403
    data = {}
    await expect(completeGitHub('csrf')).rejects.toMatchObject({ status: 403 })
    expect(requests.every((row) => row.url === '/auth/github/complete')).toBe(true)
  })
  it('accepts the existing Session privacy headers without relaxing GitHub response privacy', async () => {
    headers.set('Cache-Control', 'no-store')
    data = session
    await expect(readGitHubSession()).resolves.toEqual(session)
    expect(requests.at(-1)?.url).toBe('/auth/session')
    await expect(completeGitHub('csrf')).rejects.toBeInstanceOf(GitHubRequestError)
    expect(requests.at(-1)?.url).toBe('/auth/github/complete')
    data = config
    await expect(getGitHubConfig()).rejects.toBeInstanceOf(GitHubRequestError)
    data = session
    headers.delete('Cache-Control')
    await expect(readGitHubSession()).rejects.toBeInstanceOf(GitHubRequestError)
    headers.set('Cache-Control', 'no-store')
    headers.delete('X-Content-Type-Options')
    await expect(readGitHubSession()).rejects.toBeInstanceOf(GitHubRequestError)
  })
  it('reads Session transiently and requires exact empty204 abandonment with optional CSRF', async () => {
    data = session
    await expect(readGitHubSession()).resolves.toEqual(session)
    status = 401
    data = {}
    await expect(readGitHubSession()).resolves.toBeNull()
    status = 503
    await expect(readGitHubSession()).rejects.toMatchObject({ status: 503 })
    status = 204
    data = ''
    await expect(abandonGitHub('fresh')).resolves.toBeUndefined()
    expect(requests.at(-1)?.headers.get('X-CSRF-Token')).toBe('fresh')
    expect(JSON.parse(requests.at(-1)!.data)).toEqual({})
    status = 200
    await expect(abandonGitHub()).rejects.toBeInstanceOf(GitHubRequestError)
    status = 204
    data = { kind: 'canceled' }
    await expect(abandonGitHub()).rejects.toBeInstanceOf(GitHubRequestError)
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
    const pending = completeGitHub(undefined, controller.signal)
    controller.abort()
    release()
    await expect(pending).rejects.toBeInstanceOf(GitHubRequestError)
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
  const pending = saveGitHubConfig(etag, draft, 'csrf')
  draft.name = 'Edited after dispatch'
  release()
  await expect(pending).resolves.toEqual(config)
  expect(JSON.parse(requests[0].data).name).toBe(input.name)
})
