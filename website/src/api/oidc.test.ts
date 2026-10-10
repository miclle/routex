import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  beginOIDCProof,
  completeOIDC,
  getOIDCConfig,
  getOIDCIdentity,
  getOIDCMethod,
  OIDCRequestError,
  readOIDCSession,
  saveOIDCConfig,
  setOIDCStatus,
  startOIDC,
  unlinkOIDC,
  validOIDCConfig,
  validOIDCProof,
} from './oidc'
const original = client.defaults.adapter
const etag = 'a'.repeat(64)
const next = 'b'.repeat(64)
const config = {
  name: 'Enterprise',
  issuer: 'https://identity.example.test',
  client_id: 'routex',
  callback_url: 'https://routex.example.test/api/v1/auth/oidc/callback',
  secret_configured: true,
  enabled: false,
  verified: false,
  mfa_required: false,
  review_etag: etag,
}
const identity = {
  available: true,
  name: 'Enterprise',
  bound: false,
  mfa_required: false,
  review_etag: etag,
}
const input = {
  name: config.name,
  issuer: config.issuer,
  client_id: config.client_id,
  callback_url: config.callback_url,
  secret_action: 'replace' as const,
  client_secret: 'operation-local-secret',
  reason: 'Reviewed configuration',
}
const proof = { password: 'local password proof', proof: {}, reason: 'Explicit identity change' }
const session = {
  user: { id: 'usr_alice', name: 'Alice', email: 'alice@example.test', role: 'member' },
  csrf_token: 'current-csrf',
}
let data: unknown, status: number, responseETag: string, requests: InternalAxiosRequestConfig[]
beforeEach(() => {
  data = config
  status = 200
  responseETag = etag
  requests = []
  client.defaults.adapter = async (request) => {
    requests.push(request)
    return {
      config: request,
      status,
      statusText: '',
      data,
      headers: new AxiosHeaders({ ETag: `"${responseETag}"` }),
    }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
describe('OIDC HTTP contract', () => {
  it('keeps secret-bearing exact body and strong If-Match outside errors', async () => {
    data = { ...config, review_etag: next }
    responseETag = next
    await saveOIDCConfig(etag, input, 'fresh-csrf')
    expect(requests).toHaveLength(1)
    expect(requests[0].url).toBe('/admin/auth/oidc')
    expect(requests[0].method).toBe('put')
    expect(requests[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    expect(JSON.parse(requests[0].data)).toEqual(input)
    status = 503
    const error = await saveOIDCConfig(etag, input, 'fresh-csrf').catch((value: unknown) => value)
    expect(error).toBeInstanceOf(OIDCRequestError)
    expect(JSON.stringify(error)).not.toContain(input.client_secret)
    expect(Object.keys(error as object)).not.toContain('config')
  })
  it('rejects missing or mismatched review headers and unknown fields', async () => {
    responseETag = next
    await expect(getOIDCConfig()).rejects.toBeInstanceOf(OIDCRequestError)
    responseETag = etag
    data = { ...config, client_secret: 'must never be returned' }
    await expect(getOIDCConfig()).rejects.toBeInstanceOf(OIDCRequestError)
    data = { ...identity, mfa_required: undefined }
    await expect(getOIDCIdentity()).rejects.toBeInstanceOf(OIDCRequestError)
  })
  it('does not coerce public availability or expose private provider fields', async () => {
    data = { available: true, name: 'Enterprise' }
    await expect(getOIDCMethod()).resolves.toEqual(data)
    for (const bad of [
      { available: 'true', name: 'Enterprise' },
      { available: true, name: '' },
      { available: true, name: 'Enterprise', issuer: config.issuer },
    ]) {
      data = bad
      await expect(getOIDCMethod()).rejects.toBeInstanceOf(OIDCRequestError)
    }
  })
  it('admits only bounded HTTPS authorization navigation and sends no return URL', async () => {
    data = { authorization_url: 'https://identity.example.test/authorize?state=server-state' }
    await expect(startOIDC()).resolves.toBe(
      (data as { authorization_url: string }).authorization_url,
    )
    expect(JSON.parse(requests[0].data)).toEqual({})
    for (const authorization_url of [
      'http://identity.example.test',
      'javascript:alert(1)',
      'https://user:pass@identity.example.test',
      'https://identity.example.test/#token',
    ]) {
      data = { authorization_url }
      await expect(startOIDC()).rejects.toBeInstanceOf(OIDCRequestError)
    }
  })
  it('requires exact local MFA proof shape and preserves explicit self endpoint', async () => {
    expect(validOIDCProof(proof, false)).toBe(true)
    expect(validOIDCProof(proof, true)).toBe(false)
    expect(
      validOIDCProof(
        { ...proof, proof: { code: '123456', recovery_code: 'other' } as never },
        true,
      ),
    ).toBe(false)
    data = { authorization_url: 'https://identity.example.test/authorize?state=bound-state' }
    await beginOIDCProof('bind', etag, proof, false, 'self-csrf')
    expect(requests[0].url).toBe('/account/identity/bind')
    expect(JSON.parse(requests[0].data)).toEqual(proof)
    await expect(beginOIDCProof('verify', etag, proof, true, 'admin-csrf')).rejects.toBeInstanceOf(
      OIDCRequestError,
    )
    expect(requests).toHaveLength(1)
  })
  it('preserves complete safe status and unlink DTOs without claiming login', async () => {
    data = { ...config, enabled: true, verified: true }
    await expect(
      setOIDCStatus(etag, { enabled: true, reason: 'Enable reviewed configuration' }, 'csrf'),
    ).resolves.toMatchObject({ enabled: true })
    expect(JSON.parse(requests[0].data)).toEqual({
      enabled: true,
      reason: 'Enable reviewed configuration',
    })
    data = identity
    await expect(unlinkOIDC(etag, proof, false, 'csrf')).resolves.toEqual(identity)
  })
  it('strictly separates Session, MFA and fixed binding/test completion results', async () => {
    data = session
    await expect(completeOIDC()).resolves.toEqual({ kind: 'session', session })
    data = { kind: 'bound' }
    await expect(completeOIDC('current-csrf')).resolves.toEqual({ kind: 'bound' })
    expect(requests[1].headers.get('X-CSRF-Token')).toBe('current-csrf')
    status = 202
    data = {
      mfa_required: true,
      challenge_token: 'a'.repeat(43),
      expires_at: '2030-01-01T00:00:00.123456789Z',
      methods: ['totp', 'recovery_code'],
    }
    await expect(completeOIDC()).resolves.toMatchObject({ kind: 'challenge' })
    data = { kind: 'bound', authorization_url: 'https://unexpected.example.test/' }
    status = 200
    await expect(completeOIDC()).rejects.toBeInstanceOf(OIDCRequestError)
  })
  it('treats a fresh missing Session as null but errors and ignored-abort results as unavailable', async () => {
    status = 401
    await expect(readOIDCSession()).resolves.toBeNull()
    status = 503
    await expect(readOIDCSession()).rejects.toBeInstanceOf(OIDCRequestError)
    status = 200
    data = session
    const controller = new AbortController()
    controller.abort()
    await expect(readOIDCSession(controller.signal)).rejects.toBeDefined()
  })
  it('keeps the existing 1024-byte trimmed reason and complete config bounds', () => {
    expect(validOIDCConfig(input)).toBe(true)
    expect(validOIDCConfig({ ...input, reason: 'a'.repeat(1024) })).toBe(true)
    for (const bad of [
      { ...input, reason: 'a'.repeat(1025) },
      { ...input, reason: ' padded ' },
      { ...input, client_id: 'two words' },
      { ...input, callback_url: config.callback_url + '?next=/' },
      { ...input, secret_action: 'keep' as const },
    ])
      expect(validOIDCConfig(bad)).toBe(false)
  })
})
