import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  getSAMLConfig,
  getSAMLIdentity,
  getSAMLMethod,
  saveSAMLConfig,
  setSAMLStatus,
  validSAMLConfig,
  validSAMLProof,
  beginSAMLProof,
  startSAML,
  unlinkSAML,
  completeSAML,
  readSAMLSession,
  abandonSAML,
  sameSAMLCertificate,
  SAMLRequestError,
} from './saml'
import type { SAMLConfigInput } from '@/types/saml'
const original = client.defaults.adapter,
  etag = 'a'.repeat(64)
const pem = '-----BEGIN CERTIFICATE-----\nAQID\n-----END CERTIFICATE-----\n'
const input: SAMLConfigInput = {
  name: 'Enterprise SAML',
  idp_issuer: 'urn:example:idp',
  sso_url: 'https://identity.example.test/sso',
  sp_entity_id: 'urn:example:routex',
  acs_url: 'https://routex.example.test/api/v1/auth/saml/acs',
  signing_certificate_pem: pem,
  reason: 'Reviewed configuration',
}
const config = {
  name: input.name,
  idp_issuer: input.idp_issuer,
  sso_url: input.sso_url,
  sp_entity_id: input.sp_entity_id,
  acs_url: input.acs_url,
  signing_certificate_pem: input.signing_certificate_pem,
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
  data = config
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
describe('SAML same-origin HTTP contract', () => {
  it('submits exactly six public configuration fields and reason with strong review/CSRF', async () => {
    await saveSAMLConfig(etag, input, 'fresh-csrf')
    expect(requests[0].url).toBe('/admin/auth/saml')
    expect(requests[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    expect(JSON.parse(requests[0].data)).toEqual(input)
    expect(validSAMLConfig(input)).toBe(true)
    data = { ...config, signing_certificate_pem: '\n' + pem.replace(/\n/g, '\r\n') + '\n' }
    await expect(saveSAMLConfig(etag, input, 'csrf')).resolves.toMatchObject({
      idp_issuer: input.idp_issuer,
    })
  })
  it('admits exact absolute entity URNs but only HTTPS SSO and fixed ACS', () => {
    for (const change of [
      { sso_url: 'http://identity.example.test/sso' },
      { sso_url: 'https://id.test/sso?x=1' },
      { acs_url: 'https://routex.example.test/api/v1/auth/saml/callback' },
      { idp_issuer: 'relative' },
      { sp_entity_id: 'urn:example:sp#alias' },
      { name: ' ' },
      { reason: '' },
    ])
      expect(validSAMLConfig({ ...input, ...change })).toBe(false)
  })
  it.each([
    'preamble\n' + pem,
    pem + pem,
    pem + 'extra',
    pem.replace('AQID', 'AQ=I'),
    pem.replace('AQID', 'A'.repeat(25000)),
  ])('rejects malformed single public PEM configuration %s', (signing_certificate_pem) => {
    expect(validSAMLConfig({ ...input, signing_certificate_pem })).toBe(false)
  })
  it('requires private response headers, exact reviewed DTO shape and matching ETag', async () => {
    await expect(getSAMLConfig()).resolves.toEqual(config)
    for (const bad of [
      { ...config, enabled: 'false' },
      { ...config, raw_assertion: 'never' },
      { ...config, review_etag: '0' },
      { ...config, enabled: true },
    ]) {
      data = bad
      await expect(getSAMLConfig()).rejects.toBeInstanceOf(SAMLRequestError)
    }
    data = config
    headers.set('ETag', `"${'b'.repeat(64)}"`)
    await expect(getSAMLConfig()).rejects.toBeInstanceOf(SAMLRequestError)
    headers.set('ETag', `"${etag}"`)
    headers.delete('Cache-Control')
    await expect(getSAMLConfig()).rejects.toBeInstanceOf(SAMLRequestError)
  })
  it('distinguishes initial empty configuration and exact public/self contracts', async () => {
    data = {
      name: '',
      idp_issuer: '',
      sso_url: '',
      sp_entity_id: '',
      acs_url: '',
      signing_certificate_pem: '',
      enabled: false,
      verified: false,
      mfa_required: false,
      review_etag: etag,
    }
    await expect(getSAMLConfig()).resolves.toEqual(data)
    data = { available: false, name: '' }
    await expect(getSAMLMethod()).resolves.toEqual(data)
    data = { ...identity }
    await expect(getSAMLIdentity()).resolves.toEqual(identity)
    data = { ...identity, subject: 'private' }
    await expect(getSAMLIdentity()).rejects.toBeInstanceOf(SAMLRequestError)
  })
  it('never accepts changed response security fields as an exact configuration receipt', async () => {
    data = { ...config, idp_issuer: 'urn:other:idp' }
    await expect(saveSAMLConfig(etag, input, 'csrf')).rejects.toBeInstanceOf(SAMLRequestError)
  })
  it('separates explicit status from real proof starts and never sends return URL or cookie body', async () => {
    data = { ...config, enabled: true, verified: true }
    await expect(
      setSAMLStatus(etag, { enabled: true, reason: 'Enable reviewed method' }, 'csrf'),
    ).resolves.toMatchObject({ enabled: true })
    data = {
      authorization_url: 'https://identity.example.test/sso?SAMLRequest=opaque&RelayState=opaque',
    }
    await startSAML()
    expect(JSON.parse(requests[1].data)).toEqual({})
    await beginSAMLProof('verify', etag, proof, false, 'csrf')
    expect(requests[2].url).toBe('/admin/auth/saml/verify')
    await beginSAMLProof('bind', etag, proof, false, 'csrf')
    expect(requests[3].url).toBe('/account/identity/saml/bind')
    expect(JSON.parse(requests[3].data)).toEqual(proof)
  })
  it('rejects weak authority and malformed native MFA without dispatch', async () => {
    expect(validSAMLProof(proof, false)).toBe(true)
    expect(validSAMLProof(proof, true)).toBe(false)
    expect(
      validSAMLProof(
        { ...proof, proof: { code: '123456', recovery_code: 'extra' } as never },
        true,
      ),
    ).toBe(false)
    await expect(beginSAMLProof('bind', etag, proof, true, 'csrf')).rejects.toBeInstanceOf(
      SAMLRequestError,
    )
    await expect(saveSAMLConfig('W/' + etag, input, 'csrf')).rejects.toBeInstanceOf(
      SAMLRequestError,
    )
    expect(requests).toHaveLength(0)
  })
  it.each([
    'http://id.test',
    'javascript:alert(1)',
    'https://u:p@id.test',
    'https://id.test/#proof',
    'https://id.test/' + 'x'.repeat(8192),
  ])('rejects unsafe authorization URL %s', async (authorization_url) => {
    data = { authorization_url }
    await expect(startSAML()).rejects.toBeInstanceOf(SAMLRequestError)
  })
  it('separates Session200/MFA202/binding receipts and sends only empty completion with fresh CSRF', async () => {
    data = session
    await expect(completeSAML('fresh-csrf')).resolves.toEqual({ kind: 'session', session })
    expect(JSON.parse(requests[0].data)).toEqual({})
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    status = 202
    data = {
      mfa_required: true,
      challenge_token: 'a'.repeat(43),
      expires_at: new Date(Date.now() + 60000).toISOString(),
      methods: ['totp', 'recovery_code'],
    }
    await expect(completeSAML()).resolves.toMatchObject({ kind: 'challenge' })
    status = 200
    data = { kind: 'verified' }
    await expect(completeSAML()).resolves.toEqual({ kind: 'verified' })
    data = { kind: 'bound', subject: 'forbidden' }
    await expect(completeSAML()).rejects.toBeInstanceOf(SAMLRequestError)
  })
  it('reads Session directly, keeps anonymous401 distinct and rejects late canceled proof', async () => {
    data = session
    await expect(readSAMLSession()).resolves.toEqual(session)
    status = 401
    await expect(readSAMLSession()).resolves.toBeNull()
    const controller = new AbortController()
    controller.abort()
    await expect(readSAMLSession(controller.signal)).rejects.toBeInstanceOf(SAMLRequestError)
  })
  it('keeps unlink exact and sanitizes transport errors without retaining assertions or passwords', async () => {
    data = identity
    await expect(unlinkSAML(etag, proof, false, 'csrf')).resolves.toEqual(identity)
    status = 503
    data = { message: 'upstream assertion private' }
    const error = await beginSAMLProof('bind', etag, proof, false, 'csrf').catch((e: unknown) => e)
    expect(error).toBeInstanceOf(SAMLRequestError)
    expect(JSON.stringify(error)).not.toContain(proof.password)
    expect(JSON.stringify(error)).not.toContain('private')
    expect(Object.keys(error as object)).not.toContain('config')
  })
  it('compares only admitted public certificate DER identity across line wrapping', () => {
    expect(sameSAMLCertificate(pem, pem.replace(/\n/g, '\r\n'))).toBe(true)
    expect(sameSAMLCertificate(pem, pem.replace('AQID', 'AQIE'))).toBe(false)
    expect(sameSAMLCertificate('', '')).toBe(false)
    expect(
      validSAMLConfig({ ...input, signing_certificate_pem: pem.replace('AQID', 'AB==') }),
    ).toBe(false)
  })
  it('abandons only through confirmed empty204 with optional fresh CSRF and no proof body', async () => {
    status = 204
    data = ''
    await abandonSAML('fresh-csrf')
    expect(requests[0].url).toBe('/auth/saml/abandon')
    expect(JSON.parse(requests[0].data)).toEqual({})
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    status = 200
    await expect(abandonSAML()).rejects.toBeInstanceOf(SAMLRequestError)
    status = 204
    data = { cleared: true }
    await expect(abandonSAML()).rejects.toBeInstanceOf(SAMLRequestError)
  })
})
