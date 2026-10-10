import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  beginOAuthProof,
  completeOAuth,
  getOAuthConfig,
  getOAuthIdentity,
  getOAuthMethod,
  OAuthRequestError,
  readOAuthSession,
  saveOAuthConfig,
  setOAuthStatus,
  startOAuth,
  unlinkOAuth,
  validOAuthConfig,
  validOAuthProof,
} from './oauth'
const original = client.defaults.adapter
const etag = 'a'.repeat(64)
const next = 'b'.repeat(64)
const config = {
  name: 'Enterprise',
  authorization_url: 'https://identity.example.test/authorize',
  token_url: 'https://identity.example.test/token',
  user_info_url: 'https://identity.example.test/profile',
  client_auth_method: 'client_secret_basic' as const,
  scopes: ['profile'],
  subject_path: ['account', 'id'],
  client_id: 'routex',
  callback_url: 'https://routex.example.test/api/v1/auth/oauth/callback',
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
  authorization_url: config.authorization_url,
  token_url: config.token_url,
  user_info_url: config.user_info_url,
  client_auth_method: config.client_auth_method,
  scopes: [...config.scopes],
  subject_path: [...config.subject_path],
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
describe('OAuth HTTP contract', () => {
  it('keeps secret-bearing exact body and strong If-Match outside errors', async () => {
    data = { ...config, review_etag: next }
    responseETag = next
    await saveOAuthConfig(etag, input, 'fresh-csrf')
    expect(requests).toHaveLength(1)
    expect(requests[0].url).toBe('/admin/auth/oauth')
    expect(requests[0].method).toBe('put')
    expect(requests[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    expect(JSON.parse(requests[0].data)).toEqual(input)
    status = 503
    const error = await saveOAuthConfig(etag, input, 'fresh-csrf').catch((value: unknown) => value)
    expect(error).toBeInstanceOf(OAuthRequestError)
    expect(JSON.stringify(error)).not.toContain(input.client_secret)
    expect(Object.keys(error as object)).not.toContain('config')
  })
  it('rejects missing or mismatched review headers and unknown fields', async () => {
    responseETag = next
    await expect(getOAuthConfig()).rejects.toBeInstanceOf(OAuthRequestError)
    responseETag = etag
    data = { ...config, client_secret: 'must never be returned' }
    await expect(getOAuthConfig()).rejects.toBeInstanceOf(OAuthRequestError)
    data = { ...identity, mfa_required: undefined }
    await expect(getOAuthIdentity()).rejects.toBeInstanceOf(OAuthRequestError)
  })
  it('does not coerce public availability or expose private provider fields', async () => {
    data = { available: true, name: 'Enterprise' }
    await expect(getOAuthMethod()).resolves.toEqual(data)
    for (const bad of [
      { available: 'true', name: 'Enterprise' },
      { available: true, name: '' },
      { available: true, name: 'Enterprise', authorization_url: config.authorization_url },
    ]) {
      data = bad
      await expect(getOAuthMethod()).rejects.toBeInstanceOf(OAuthRequestError)
    }
  })
  it('admits only bounded HTTPS authorization navigation and sends no return URL', async () => {
    data = { authorization_url: 'https://identity.example.test/authorize?state=server-state' }
    await expect(startOAuth()).resolves.toBe(
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
      await expect(startOAuth()).rejects.toBeInstanceOf(OAuthRequestError)
    }
  })
  it('requires exact local MFA proof shape and preserves explicit self endpoint', async () => {
    expect(validOAuthProof(proof, false)).toBe(true)
    expect(validOAuthProof(proof, true)).toBe(false)
    expect(
      validOAuthProof(
        { ...proof, proof: { code: '123456', recovery_code: 'other' } as never },
        true,
      ),
    ).toBe(false)
    data = { authorization_url: 'https://identity.example.test/authorize?state=bound-state' }
    await beginOAuthProof('bind', etag, proof, false, 'self-csrf')
    expect(requests[0].url).toBe('/account/identity/oauth/bind')
    expect(JSON.parse(requests[0].data)).toEqual(proof)
    await expect(beginOAuthProof('verify', etag, proof, true, 'admin-csrf')).rejects.toBeInstanceOf(
      OAuthRequestError,
    )
    expect(requests).toHaveLength(1)
  })
  it('preserves complete safe status and unlink DTOs without claiming login', async () => {
    data = { ...config, enabled: true, verified: true }
    await expect(
      setOAuthStatus(etag, { enabled: true, reason: 'Enable reviewed configuration' }, 'csrf'),
    ).resolves.toMatchObject({ enabled: true })
    expect(JSON.parse(requests[0].data)).toEqual({
      enabled: true,
      reason: 'Enable reviewed configuration',
    })
    data = identity
    await expect(unlinkOAuth(etag, proof, false, 'csrf')).resolves.toEqual(identity)
  })
  it('strictly separates Session, MFA and fixed binding/test completion results', async () => {
    data = session
    await expect(completeOAuth()).resolves.toEqual({ kind: 'session', session })
    data = { kind: 'bound' }
    await expect(completeOAuth('current-csrf')).resolves.toEqual({ kind: 'bound' })
    expect(requests[1].headers.get('X-CSRF-Token')).toBe('current-csrf')
    status = 202
    data = {
      mfa_required: true,
      challenge_token: 'a'.repeat(43),
      expires_at: '2030-01-01T00:00:00.123456789Z',
      methods: ['totp', 'recovery_code'],
    }
    await expect(completeOAuth()).resolves.toMatchObject({ kind: 'challenge' })
    data = { kind: 'bound', authorization_url: 'https://unexpected.example.test/' }
    status = 200
    await expect(completeOAuth()).rejects.toBeInstanceOf(OAuthRequestError)
  })
  it('treats a fresh missing Session as null but errors and ignored-abort results as unavailable', async () => {
    status = 401
    await expect(readOAuthSession()).resolves.toBeNull()
    status = 503
    await expect(readOAuthSession()).rejects.toBeInstanceOf(OAuthRequestError)
    status = 200
    data = session
    const controller = new AbortController()
    controller.abort()
    await expect(readOAuthSession(controller.signal)).rejects.toBeDefined()
  })
  it('keeps the existing 1024-byte trimmed reason and complete config bounds', () => {
    expect(validOAuthConfig(input)).toBe(true)
    expect(validOAuthConfig({ ...input, reason: 'a'.repeat(1024) })).toBe(true)
    for (const bad of [
      { ...input, reason: 'a'.repeat(1025) },
      { ...input, reason: ' padded ' },
      { ...input, client_id: 'invalid\nclient' },
      { ...input, callback_url: config.callback_url + '?next=/' },
      { ...input, secret_action: 'keep' as const },
    ])
      expect(validOAuthConfig(bad)).toBe(false)
  })
})

it('accepts only the coherent unconfigured empty singleton, never null arrays or a partial path', async () => {
  const empty = {
    ...config,
    name: '',
    authorization_url: '',
    token_url: '',
    user_info_url: '',
    client_auth_method: '',
    client_id: '',
    callback_url: '',
    scopes: [],
    subject_path: [],
    secret_configured: false,
  }
  data = empty
  await expect(getOAuthConfig()).resolves.toEqual(empty)
  for (const bad of [
    { ...empty, scopes: null },
    { ...empty, subject_path: null },
    { ...empty, client_id: 'partial' },
    { ...config, scopes: null },
    { ...config, subject_path: [] },
    { ...config, subject_path: ['account', 0] },
    { ...config, scopes: ['profile', 'profile'] },
    { ...config, client_auth_method: 'automatic' },
    { ...config, enabled: true, verified: false },
    { ...config, token_url: config.token_url + '?query=x' },
  ]) {
    data = bad
    await expect(getOAuthConfig()).rejects.toBeInstanceOf(OAuthRequestError)
  }
})
it('preserves exact object-key paths, ordered scopes and reserved client bytes without numeric or dotted coercion', async () => {
  const value = {
    ...input,
    client_id: 'client +:/&',
    client_secret: 'secret +:/&',
    scopes: ['profile', 'email'],
    subject_path: ['account.id', '0', '身份'],
  }
  expect(validOAuthConfig(value)).toBe(true)
  data = {
    ...config,
    client_id: value.client_id,
    scopes: value.scopes,
    subject_path: value.subject_path,
  }
  await saveOAuthConfig(etag, value, 'csrf')
  expect(JSON.parse(requests[0].data)).toEqual(value)
  for (const patch of [
    { subject_path: 'account.id' },
    { subject_path: ['account', 0] },
    { subject_path: [''] },
    { subject_path: ['x'.repeat(129)] },
    { subject_path: ['\ud800'] },
    { scopes: ['profile', 'profile'] },
    { scopes: ['invalid scope'] },
    { scopes: Array.from({ length: 17 }, (_, i) => `scope${i}`) },
    { client_auth_method: '' },
    { client_auth_method: 'none' },
    { client_secret: 'invalid\nsecret' },
    { authorization_url: 'https://identity.example.test/with space' },
    { token_url: config.token_url + '?' },
    { user_info_url: config.user_info_url + '#' },
    { callback_url: config.callback_url.replace('/oauth/', '/oidc/') },
  ])
    expect(validOAuthConfig({ ...value, ...patch } as never)).toBe(false)
})
it('copies ordered request and response arrays across an in-flight caller mutation', async () => {
  const value = { ...input, scopes: ['profile', 'email'], subject_path: ['account', 'id'] }
  let release!: () => void
  const gate = new Promise<void>((done) => {
    release = done
  })
  const returned = { ...config, scopes: ['profile', 'email'], subject_path: ['account', 'id'] }
  client.defaults.adapter = async (request) => {
    requests.push(request)
    await gate
    return {
      config: request,
      status: 200,
      statusText: '',
      data: returned,
      headers: new AxiosHeaders({ ETag: `"${etag}"` }),
    }
  }
  const pending = saveOAuthConfig(etag, value, 'csrf')
  value.scopes.reverse()
  value.subject_path[1] = 'other'
  release()
  const result = await pending
  expect(JSON.parse(requests[0].data).scopes).toEqual(['profile', 'email'])
  expect(JSON.parse(requests[0].data).subject_path).toEqual(['account', 'id'])
  returned.scopes[0] = 'mutated'
  returned.subject_path[0] = 'mutated'
  expect(result.scopes).toEqual(['profile', 'email'])
  expect(result.subject_path).toEqual(['account', 'id'])
})
it('rejects an acknowledged configuration with changed array order or substituted endpoint', async () => {
  const value = { ...input, scopes: ['profile', 'email'] }
  for (const patch of [
    { scopes: ['email', 'profile'] },
    { subject_path: ['id', 'account'] },
    { user_info_url: 'https://different.example.test/profile' },
    { client_auth_method: 'client_secret_post' },
  ]) {
    data = { ...config, scopes: value.scopes, ...patch }
    await expect(saveOAuthConfig(etag, value, 'csrf')).rejects.toBeInstanceOf(OAuthRequestError)
  }
})
