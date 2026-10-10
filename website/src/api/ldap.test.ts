import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  getLDAPConfig,
  getLDAPIdentity,
  getLDAPMethod,
  saveLDAPConfig,
  setLDAPStatus,
  submitLDAPProof,
  unlinkLDAP,
  loginLDAP,
  readLDAPSession,
  validLDAPConfig,
  validLDAPBindingProof,
  LDAPRequestError,
} from './ldap'
import type { LDAPConfig, LDAPConfigInput, LDAPBindingProof } from '@/types/ldap'
const original = client.defaults.adapter,
  etag = 'a'.repeat(64)
const saved: LDAPConfig = {
  name: 'Directory',
  endpoint: 'ldaps://directory.example.test:636',
  bind_dn: 'cn=reader,dc=example,dc=test',
  base_dn: 'dc=example,dc=test',
  user_filter: '(uid={username})',
  identity_attribute: 'entryUUID',
  secret_configured: true,
  mfa_required: false,
  enabled: false,
  verified: false,
  review_etag: etag,
}
const input: LDAPConfigInput = {
  name: saved.name,
  endpoint: saved.endpoint,
  bind_dn: saved.bind_dn,
  base_dn: saved.base_dn,
  user_filter: saved.user_filter,
  identity_attribute: 'entryUUID',
  secret_action: 'keep',
  bind_password: '',
  reason: 'Reviewed exact directory',
}
const proof: LDAPBindingProof = {
  password: 'current-password-123',
  proof: {},
  reason: 'Link existing member',
  username: 'alice',
  directory_password: ' directory password ',
}
let data: unknown, status: number, headers: AxiosHeaders, requests: InternalAxiosRequestConfig[]
beforeEach(() => {
  data = { ...saved }
  status = 200
  requests = []
  headers = new AxiosHeaders({
    ETag: `"${etag}"`,
    'Cache-Control': 'private, no-store',
    'X-Content-Type-Options': 'nosniff',
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { data, status, statusText: '', headers, config }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
describe('LDAP exact bounded wire', () => {
  it('reads reviewed configuration with signal and accepts the empty unconfigured attribute only with empty safe facts', async () => {
    const signal = new AbortController().signal
    expect(await getLDAPConfig(signal)).toEqual(saved)
    expect(requests[0].url).toBe('/admin/auth/ldap')
    expect(requests[0].signal).toBe(signal)
    data = {
      name: '',
      endpoint: '',
      bind_dn: '',
      base_dn: '',
      user_filter: '',
      identity_attribute: '',
      secret_configured: false,
      mfa_required: false,
      enabled: false,
      verified: false,
      review_etag: etag,
    }
    expect((await getLDAPConfig()).identity_attribute).toBe('')
    Object.assign(data as object, { identity_attribute: 'entryUUID' })
    await expect(getLDAPConfig()).rejects.toThrow(LDAPRequestError)
  })
  it.each([
    'extra',
    'null-attribute',
    'missing-attribute',
    'guess-native',
    'enabled-unverified',
    'verified-no-secret',
    'weak-review',
    'wrong-header',
    'no-private',
    'no-store',
    'no-nosniff',
  ])('rejects malformed safe configuration %s', async (kind) => {
    if (kind === 'extra') data = { ...saved, bind_password: 'never exposed' }
    if (kind === 'null-attribute') data = { ...saved, identity_attribute: null }
    if (kind === 'missing-attribute') {
      const copy: Partial<LDAPConfig> = { ...saved }
      delete copy.identity_attribute
      data = copy
    }
    if (kind === 'guess-native') data = { ...saved, identity_attribute: 'uid' }
    if (kind === 'enabled-unverified') data = { ...saved, enabled: true }
    if (kind === 'verified-no-secret') data = { ...saved, verified: true, secret_configured: false }
    if (kind === 'weak-review') data = { ...saved, review_etag: 'weak' }
    if (kind === 'wrong-header') headers.set('ETag', `W/"${etag}"`)
    if (kind === 'no-private') headers.set('Cache-Control', 'no-store')
    if (kind === 'no-store') headers.set('Cache-Control', 'private')
    if (kind === 'no-nosniff') headers.delete('X-Content-Type-Options')
    await expect(getLDAPConfig()).rejects.toThrow(LDAPRequestError)
  })
  it.each([
    'http',
    'path',
    'query',
    'fragment',
    'userinfo',
    'port',
    'attribute',
    'filter',
    'null',
    'secret',
    'unknown',
  ])('rejects invalid configuration input %s before dispatch', async (kind) => {
    const value = { ...input }
    if (kind === 'http') value.endpoint = 'ldap://directory.example.test:389'
    if (kind === 'path') value.endpoint += '/'
    if (kind === 'query') value.endpoint += '?insecure=true'
    if (kind === 'fragment') value.endpoint += '#fragment'
    if (kind === 'userinfo') value.endpoint = 'ldaps://reader:secret@directory.example.test:636'
    if (kind === 'port') value.endpoint = 'ldaps://directory.example.test'
    if (kind === 'attribute') value.identity_attribute = ''
    if (kind === 'filter') value.user_filter = '(uid=*)'
    if (kind === 'null') Object.assign(value, { base_dn: null })
    if (kind === 'secret') value.bind_password = 'not-keep'
    if (kind === 'unknown') Object.assign(value, { auto_create_users: true })
    expect(validLDAPConfig(value)).toBe(false)
    await expect(saveLDAPConfig(etag, value, 'csrf')).rejects.toThrow(LDAPRequestError)
    expect(requests).toHaveLength(0)
  })
  it('sends one exact reviewed scalar copy with current CSRF; keeps service passwords verbatim and validates response tuple', async () => {
    const value = {
      ...input,
      secret_action: 'replace' as const,
      bind_password: ' service password ',
    }
    const signal = new AbortController().signal
    const pending = saveLDAPConfig(etag, value, 'current-csrf', signal)
    value.bind_password = 'changed after dispatch'
    await pending
    expect(requests).toHaveLength(1)
    expect(requests[0].method).toBe('put')
    expect(requests[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('current-csrf')
    expect(requests[0].signal).toBe(signal)
    expect(JSON.parse(requests[0].data)).toEqual({
      ...input,
      secret_action: 'replace',
      bind_password: ' service password ',
    })
    data = { ...saved, base_dn: 'dc=other' }
    await expect(saveLDAPConfig(etag, input, 'csrf')).rejects.toThrow(LDAPRequestError)
  })
  it('strictly decodes identity and exact bind/verify/unlink outcomes without exposing directory facts', async () => {
    data = {
      available: true,
      name: 'Directory',
      bound: false,
      mfa_required: false,
      review_etag: etag,
    }
    expect((await getLDAPIdentity()).bound).toBe(false)
    for (const action of ['bind', 'verify'] as const) {
      data = { kind: action === 'bind' ? 'bound' : 'verified' }
      await submitLDAPProof(action, etag, proof, false, 'csrf')
    }
    expect(requests.slice(1).map((row) => row.url)).toEqual([
      '/account/identity/ldap/bind',
      '/admin/auth/ldap/verify',
    ])
    expect(JSON.parse(requests[1].data)).toEqual(proof)
    data = {
      available: true,
      name: 'Directory',
      bound: false,
      mfa_required: false,
      review_etag: etag,
    }
    const local = { password: proof.password, proof: {}, reason: proof.reason }
    await unlinkLDAP(etag, local, false, 'csrf')
    expect(JSON.parse(requests.at(-1)!.data)).toEqual(local)
    data = { kind: 'bound' }
    await expect(submitLDAPProof('verify', etag, proof, false, 'csrf')).rejects.toThrow(
      LDAPRequestError,
    )
    data = {
      available: true,
      name: 'Directory',
      bound: true,
      mfa_required: false,
      review_etag: etag,
    }
    await expect(unlinkLDAP(etag, local, false, 'csrf')).rejects.toThrow(LDAPRequestError)
  })
  it('requires current local and optional exact native MFA proof independently of directory credentials', () => {
    expect(validLDAPBindingProof(proof, false)).toBe(true)
    expect(validLDAPBindingProof(proof, true)).toBe(false)
    expect(validLDAPBindingProof({ ...proof, proof: { code: '123456' } }, true)).toBe(true)
    expect(
      validLDAPBindingProof({ ...proof, proof: { recovery_code: 'recovery-code' } }, true),
    ).toBe(true)
    expect(
      validLDAPBindingProof(
        {
          ...proof,
          proof: { code: '123456', recovery_code: 'recovery-code' },
        } as unknown as LDAPBindingProof,
        true,
      ),
    ).toBe(false)
    expect(validLDAPBindingProof({ ...proof, password: 'short' }, false)).toBe(false)
    expect(validLDAPBindingProof({ ...proof, username: 'bad\nname' }, false)).toBe(false)
    expect(validLDAPBindingProof({ ...proof, directory_password: '' }, false)).toBe(false)
  })
  it('does not accept status mismatches or automatic verification as an Enable receipt', async () => {
    data = { ...saved, verified: true, enabled: true }
    expect(
      (await setLDAPStatus(etag, { enabled: true, reason: 'Explicit enable' }, 'csrf')).enabled,
    ).toBe(true)
    data = { ...saved, verified: true, enabled: false }
    await expect(
      setLDAPStatus(etag, { enabled: true, reason: 'Explicit enable' }, 'csrf'),
    ).rejects.toThrow(LDAPRequestError)
  })
  it('decodes native Session200 and MFA202 separately; never treats a challenge as a Session', async () => {
    data = {
      user: { id: 'usr_alice', email: 'alice@example.test', name: 'Alice', role: 'member' },
      csrf_token: 'current-csrf',
    }
    expect((await loginLDAP({ username: 'alice', password: ' dir ' })).kind).toBe('session')
    expect(JSON.parse(requests[0].data)).toEqual({ username: 'alice', password: ' dir ' })
    status = 202
    data = {
      mfa_required: true,
      challenge_token: 'a'.repeat(43),
      expires_at: '2099-10-10T00:00:00Z',
      methods: ['totp', 'recovery_code'],
    }
    expect((await loginLDAP({ username: 'alice', password: 'dir' })).kind).toBe('challenge')
    status = 200
    await expect(loginLDAP({ username: 'alice', password: 'dir' })).rejects.toThrow(
      LDAPRequestError,
    )
  })
  it('reads actual Session with normal no-store headers and treats only definitive401 as null', async () => {
    headers.set('Cache-Control', 'no-store')
    data = {
      user: { id: 'usr_alice', email: 'alice@example.test', name: 'Alice', role: 'member' },
      csrf_token: 'csrf',
    }
    expect((await readLDAPSession())?.user.id).toBe('usr_alice')
    status = 401
    expect(await readLDAPSession()).toBeNull()
    status = 503
    await expect(readLDAPSession()).rejects.toThrow(LDAPRequestError)
  })
  it('sanitizes upstream payloads and rejects ignored-abort successful replies without retry', async () => {
    status = 503
    data = { error: 'sensitive directory DN password raw error' }
    await expect(loginLDAP({ username: 'alice', password: 'secret' })).rejects.toMatchObject({
      status: 503,
      message: 'LDAP request unavailable',
    })
    expect(requests).toHaveLength(1)
    status = 200
    data = { available: true, name: 'Directory' }
    const controller = new AbortController()
    controller.abort()
    await expect(getLDAPMethod(controller.signal)).rejects.toThrow()
    expect(requests.length).toBeLessThanOrEqual(2)
  })
})
