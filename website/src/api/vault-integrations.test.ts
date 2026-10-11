import { afterEach, describe, expect, it, vi } from 'vitest'
import client from './client'
import {
  getVaultIntegrations,
  getVaultIntegration,
  getVaultProbe,
  parseVaultIntegration,
  parseVaultPage,
  parseVaultProbe,
  runVaultProbe,
  saveVaultIntegration,
  validVaultDescriptor,
  validVaultName,
  validVaultReason,
  validVaultToken,
  validVaultCertificateAuth,
  sameVaultAuthReplacement,
  VaultError,
} from './vault-integrations'
import {
  configuration,
  certificateReplacement,
  certificatePEM,
  privateKeyPEM,
  certificateVault,
  probe,
  vault,
  vaultETag,
  vaultID,
  requestID,
} from '@/views/secrets/vault-fixtures'
afterEach(() => vi.restoreAllMocks())
describe('Vault safe strict boundary', () => {
  it('preserves separate stage facts, exact strings and nullable terminal cursor', () => {
    expect(parseVaultIntegration(vault())).toEqual(vault())
    expect(parseVaultProbe(probe())).toEqual(probe())
    expect(
      parseVaultPage({
        items: [vault()],
        next_cursor: null,
        review_etag: vaultETag,
        can_write: true,
        can_test: false,
      }).next_cursor,
    ).toBeNull()
  })
  it.each(['secret', 'id', 'revision', 'token', 'foreign-probe'])(
    'rejects malformed/private integration %s',
    (kind) => {
      const value = vault()
      if (kind === 'secret') Object.assign(value, { secret: 'hidden' })
      if (kind === 'id') value.id = value.id.toUpperCase()
      if (kind === 'revision') value.revision_id = 'wrong'
      if (kind === 'token') Object.assign(value.writer_auth, { token: 'hidden' })
      if (kind === 'foreign-probe') {
        value.last_probe = probe()
        value.last_probe.integration_id = 'vlt_01k00000000000000000000001'
      }
      expect(() => parseVaultIntegration(value)).toThrow(VaultError)
    },
  )
  it.each(['missing', 'duplicate', 'null-rate', 'numeric-duration', 'cleanup', 'id', 'extra'])(
    'rejects probe/page deviations %s',
    (kind) => {
      const value = probe()
      if (kind === 'missing') {
        expect(() =>
          parseVaultPage({ items: [], review_etag: vaultETag, can_write: true, can_test: true }),
        ).toThrow()
        return
      }
      if (kind === 'duplicate') {
        expect(() =>
          parseVaultPage({
            items: [vault(), vault()],
            next_cursor: null,
            review_etag: vaultETag,
            can_write: true,
            can_test: true,
          }),
        ).toThrow()
        return
      }
      if (kind === 'null-rate') Object.assign(value.write, { duration_ms: null })
      if (kind === 'numeric-duration') Object.assign(value.write, { duration_ms: 1 })
      if (kind === 'cleanup') value.cleanup.state = 'acknowledged'
      if (kind === 'id') value.id = 'jwo_bad'
      if (kind === 'extra') Object.assign(value, { marker: 'hidden' })
      expect(() => parseVaultProbe(value)).toThrow(VaultError)
    },
  )
  it('keeps completed and cleanup ownership distinct from Read success', () => {
    const value = probe()
    value.state = 'completed'
    value.write.succeeded = false
    value.write.failure = { stage: 'write', code: 'invalid_response', http_status: 200 }
    value.cleanup = {
      state: 'acknowledged',
      observation: { attempted: true, succeeded: true, duration_ms: '1', failure: null },
    }
    expect(parseVaultProbe(value).read.succeeded).toBe(false)
    expect(parseVaultProbe(value).write.succeeded).toBe(false)
  })
  it.each(['', 'has space', '\n', 'é', 'x'.repeat(4097)])('rejects invalid Token %s', (value) =>
    expect(validVaultToken(value)).toBe(false),
  )
  it('uses Go whitespace boundaries and preserves FEFF and exact Token bytes', () => {
    expect(validVaultName('\ufeffName')).toBe(true)
    expect(validVaultReason('\ufeffReason')).toBe(true)
    expect(validVaultName('\u0085Name')).toBe(false)
    expect(validVaultReason('reason\n')).toBe(false)
    expect(validVaultToken('x'.repeat(4096))).toBe(true)
  })
  it.each(['', '../bad', 'bad.', '/bad', 'bad//part', 'bad/', 'bad%2Fpart', 'bad space'])(
    'rejects noncanonical descriptor path %s',
    (prefix) => expect(validVaultDescriptor({ ...vault().descriptor, prefix })).toBe(false),
  )
  it.each([
    'https://vault.test/base%2Fpath',
    'https://vault.test/a/../b',
    'https://vault.test/base?query=1',
    'https://user:password@vault.test',
    'https://vault.test//base',
  ])('rejects unsafe base syntax %s', (endpoint) =>
    expect(validVaultDescriptor({ ...vault().descriptor, endpoint })).toBe(false),
  )
  it('rejects duplicate Tokens/keep creation/extraneous auth before dispatch', async () => {
    const spy = vi.spyOn(client, 'request')
    const value = configuration()
    value.input.reader_auth = value.input.writer_auth
    await expect(saveVaultIntegration(value, 'c'.repeat(64))).rejects.toThrow()
    value.input.reader_auth = { action: 'remove' }
    Object.assign(value.input.writer_auth, { extra: true })
    await expect(saveVaultIntegration(value, 'c'.repeat(64))).rejects.toThrow()
    delete value.id
    value.input.writer_auth = { action: 'keep' }
    await expect(saveVaultIntegration(value, 'c'.repeat(64))).rejects.toThrow()
    expect(spy).not.toHaveBeenCalled()
  })
  it('forwards exact intent and current CSRF, returns only safe committed receipt', async () => {
    const input = configuration()
    const spy = vi.spyOn(client, 'request').mockResolvedValue({
      status: 200,
      data: {
        request_id: requestID,
        integration_id: vaultID,
        revision_id: vault().revision_id,
        committed: true,
        changed: true,
      },
      headers: {},
    })
    await saveVaultIntegration(input, 'd'.repeat(64))
    expect(spy).toHaveBeenCalledWith(
      expect.objectContaining({
        method: 'put',
        data: input.input,
        headers: { 'If-Match': `"${vaultETag}"`, 'X-CSRF-Token': 'd'.repeat(64) },
      }),
    )
  })
  it('requires exact target/header review on GETs and one explicit probe command', async () => {
    const spy = vi
      .spyOn(client, 'request')
      .mockResolvedValue({ status: 200, data: vault(), headers: { etag: `W/"${vaultETag}"` } })
    await expect(getVaultIntegration(vaultID)).rejects.toThrow()
    spy.mockResolvedValue({
      status: 200,
      data: {
        items: [],
        next_cursor: null,
        review_etag: vaultETag,
        can_write: true,
        can_test: true,
      },
      headers: { etag: `"${vaultETag}"` },
    })
    await getVaultIntegrations()
    spy.mockResolvedValue({ status: 200, data: probe(), headers: { etag: `"${vaultETag}"` } })
    await getVaultProbe(vaultID, probe().id)
    await runVaultProbe(
      {
        id: vaultID,
        action: 'read',
        probe_id: probe().id,
        etag: vaultETag,
        input: { request_id: requestID, reason: 'Read owned probe' },
      },
      'd'.repeat(64),
    )
    expect(spy.mock.calls.at(-1)?.[0]).toMatchObject({
      method: 'post',
      url: `/admin/secrets/integrations/${vaultID}/probes/${probe().id}/read`,
    })
  })
})
it('failure without HTTP response is numeric zero and cannot become a success', () => {
  const value = probe()
  value.write = {
    attempted: false,
    succeeded: false,
    duration_ms: '0',
    failure: { stage: 'prepare', code: 'invalid_tokens', http_status: 0 },
  }
  expect(parseVaultProbe(value).write.failure?.http_status).toBe(0)
  Object.assign(value.write.failure!, { http_status: null })
  expect(() => parseVaultProbe(value)).toThrow(VaultError)
})
it('a Write response must bind the original UUID, while a Read result keeps the original plan UUID', async () => {
  const spy = vi
    .spyOn(client, 'request')
    .mockResolvedValue({ status: 200, data: probe(), headers: {} })
  await expect(
    runVaultProbe(
      {
        id: vaultID,
        action: 'write',
        etag: vaultETag,
        input: { request_id: '10000000-0000-4000-8000-000000000002', reason: 'Write review' },
      },
      'd'.repeat(64),
    ),
  ).rejects.toThrow(VaultError)
  await expect(
    runVaultProbe(
      {
        id: vaultID,
        action: 'read',
        probe_id: probe().id,
        etag: vaultETag,
        input: { request_id: '10000000-0000-4000-8000-000000000002', reason: 'Read review' },
      },
      'd'.repeat(64),
    ),
  ).resolves.toMatchObject({ request_id: requestID })
  expect(spy).toHaveBeenCalledTimes(2)
})

it('accepts retained historical probe facts only for the same exact integration', () => {
  const value = vault()
  value.last_probe = probe()
  value.revision_id = 'vlr_01k00000000000000000000001'
  expect(parseVaultIntegration(value).last_probe).toEqual(probe())
  value.last_probe.integration_id = 'vlt_01k00000000000000000000001'
  expect(() => parseVaultIntegration(value)).toThrow(VaultError)
})
it.each([
  'arbitrary',
  vaultID.toUpperCase(),
  vaultID + ' ',
  vaultID.replace('vlt_', 'vlr_'),
  'vlt_81k00000000000000000000000',
])('rejects noncanonical cursor %s before it can become another list request', (cursor) => {
  expect(() =>
    parseVaultPage({
      items: [vault()],
      next_cursor: cursor,
      review_etag: vaultETag,
      can_write: true,
      can_test: true,
    }),
  ).toThrow(VaultError)
})
it('retains exact canonical cursor and rejects malformed outgoing cursors without dispatch', async () => {
  expect(
    parseVaultPage({
      items: [vault()],
      next_cursor: vaultID,
      review_etag: vaultETag,
      can_write: true,
      can_test: true,
    }).next_cursor,
  ).toBe(vaultID)
  const spy = vi.spyOn(client, 'request')
  await expect(getVaultIntegrations('arbitrary')).rejects.toThrow(VaultError)
  expect(spy).not.toHaveBeenCalled()
})

describe('saved AppRole authentication', () => {
  it('decodes only the recorded method and configured state for each identity', () => {
    const value = vault()
    Object.assign(value.writer_auth, { method: 'approle' })
    expect(parseVaultIntegration(value).writer_auth).toEqual({
      method: 'approle',
      configured: true,
    })
  })
  it('sends the complete replacement tuple unchanged with the reviewed headers and signal', async () => {
    const intent = configuration()
    Object.assign(intent.input, {
      writer_auth: {
        action: 'replace',
        method: 'approle',
        auth_mount: 'custom/team',
        role_id: 'writer-role',
        secret_id: 'writer-secret-id',
      },
    })
    const signal = new AbortController().signal
    const spy = vi.spyOn(client, 'request').mockResolvedValue({
      status: 200,
      data: {
        request_id: requestID,
        integration_id: vaultID,
        revision_id: vault().revision_id,
        committed: true,
        changed: true,
      },
    })
    await saveVaultIntegration(intent, 'c'.repeat(64), signal)
    expect(spy).toHaveBeenCalledOnce()
    expect(spy.mock.calls[0][0]).toMatchObject({
      method: 'put',
      url: '/admin/secrets/integrations/' + vaultID,
      data: intent.input,
      signal,
      headers: { 'If-Match': '"' + vaultETag + '"', 'X-CSRF-Token': 'c'.repeat(64) },
    })
  })
})

describe('AppRole strict replacement and privacy', () => {
  const appRole = () => ({
    action: 'replace',
    method: 'approle',
    auth_mount: 'custom/team',
    role_id: 'writer-role',
    secret_id: 'writer-secret-id',
  })
  it.each([
    'missing-role',
    'missing-secret',
    'missing-mount',
    'null-role',
    'numeric-secret',
    'token-mixed',
    'unknown-method',
    'case-method',
    'null-method',
    'keep-fields',
    'remove-fields',
    'empty-segment',
    'traversal',
    'trailing-dot',
    'leading-slash',
    'long-mount',
    'space-role',
    'long-secret',
    'duplicate-tuple',
  ])('rejects %s before HTTP without exposing material', async (fault) => {
    const intent = configuration()
    const value: Record<string, unknown> = appRole()
    if (fault === 'missing-role') delete value.role_id
    if (fault === 'missing-secret') delete value.secret_id
    if (fault === 'missing-mount') delete value.auth_mount
    if (fault === 'null-role') value.role_id = null
    if (fault === 'numeric-secret') value.secret_id = 123
    if (fault === 'token-mixed') value.token = 'not-public'
    if (fault === 'unknown-method') value.method = 'tls'
    if (fault === 'case-method') value.method = 'AppRole'
    if (fault === 'null-method') value.method = null
    if (fault === 'keep-fields') value.action = 'keep'
    if (fault === 'remove-fields') value.action = 'remove'
    if (fault === 'empty-segment') value.auth_mount = 'custom//team'
    if (fault === 'traversal') value.auth_mount = 'custom/../team'
    if (fault === 'trailing-dot') value.auth_mount = 'custom/team.'
    if (fault === 'leading-slash') value.auth_mount = '/approle'
    if (fault === 'long-mount') value.auth_mount = 'a'.repeat(129)
    if (fault === 'space-role') value.role_id = 'bad role'
    if (fault === 'long-secret') value.secret_id = 's'.repeat(4097)
    Object.assign(intent.input, { writer_auth: value })
    if (fault === 'duplicate-tuple')
      Object.assign(intent.input, { reader_auth: structuredClone(value) })
    const spy = vi.spyOn(client, 'request')
    await expect(saveVaultIntegration(intent, 'c'.repeat(64))).rejects.toThrow(
      'Vault integration request failed',
    )
    expect(spy).not.toHaveBeenCalled()
  })
  it.each(['token', 'role_id', 'secret_id', 'auth_mount', 'login_token'])(
    'rejects returned auth material %s',
    (key) => {
      const value = vault()
      Object.assign(value.reader_auth, { method: 'approle', [key]: 'not-public' })
      expect(() => parseVaultIntegration(value)).toThrow(VaultError)
    },
  )
  it('rejects noncanonical unconfigured AppRole metadata', () => {
    const value = vault()
    Object.assign(value.reader_auth, { method: 'approle', configured: false })
    expect(() => parseVaultIntegration(value)).toThrow(VaultError)
  })
  it('preserves explicit Token compatibility and never infers mixed-method principal equality', async () => {
    const spy = vi.spyOn(client, 'request').mockResolvedValue({
      status: 200,
      data: {
        request_id: requestID,
        integration_id: vaultID,
        revision_id: vault().revision_id,
        committed: true,
        changed: false,
      },
    })
    const intent = configuration()
    Object.assign(intent.input.writer_auth, { method: 'token' })
    await saveVaultIntegration(intent, 'c'.repeat(64))
    expect(spy.mock.calls[0][0].data).toMatchObject({
      writer_auth: {
        action: 'replace',
        method: 'token',
        token: 'writer-secret',
      },
    })
    Object.assign(intent.input, {
      writer_auth: appRole(),
      reader_auth: { action: 'replace', token: 'writer-secret-id' },
    })
    await saveVaultIntegration(intent, 'c'.repeat(64))
    expect(spy).toHaveBeenCalledTimes(2)
  })
  it('keeps preparation login failure separate from unattempted KV facts', () => {
    const value = probe()
    value.state = 'interrupted'
    value.version = null
    value.write = {
      attempted: false,
      succeeded: false,
      duration_ms: '10',
      failure: { stage: 'prepare', code: 'auth_expired', http_status: 0 },
    }
    expect(parseVaultProbe(value).write).toEqual(value.write)
    value.write.failure!.code = 'invalid_auth'
    expect(parseVaultProbe(value).write.succeeded).toBe(false)
  })
})

describe('saved certificate authentication boundary', () => {
  it('accepts only recorded certificate method/configuration and rejects every material echo', () => {
    expect(parseVaultIntegration(certificateVault()).writer_auth).toEqual({
      method: 'certificate',
      configured: true,
    })
    for (const field of [
      'certificate_pem',
      'private_key_pem',
      'server_ca_pem',
      'certificate_role',
      'auth_mount',
      'expires_at',
    ]) {
      const value = certificateVault()
      Object.assign(value.writer_auth, { [field]: 'private-response-material' })
      expect(() => parseVaultIntegration(value)).toThrow(VaultError)
    }
    const value = certificateVault()
    value.writer_auth.configured = false
    expect(() => parseVaultIntegration(value)).toThrow(VaultError)
  })
  it('dispatches the exact complete certificate body once and returns only the safe receipt', async () => {
    const intent = configuration()
    intent.input.writer_auth = { ...certificateReplacement(), server_ca_pem: certificatePEM }
    const signal = new AbortController().signal
    const spy = vi.spyOn(client, 'request').mockResolvedValue({
      status: 200,
      data: {
        request_id: requestID,
        integration_id: vaultID,
        revision_id: vault().revision_id,
        committed: true,
        changed: true,
      },
    })
    const result = await saveVaultIntegration(intent, 'c'.repeat(64), signal)
    expect(spy).toHaveBeenCalledOnce()
    expect(spy.mock.calls[0][0]).toMatchObject({
      data: intent.input,
      signal,
      headers: { 'If-Match': '"' + vaultETag + '"', 'X-CSRF-Token': 'c'.repeat(64) },
    })
    expect(JSON.stringify(result)).not.toContain('PEM')
    expect(JSON.stringify(result)).not.toContain('YQ==')
  })
  it.each([
    'missing-certificate',
    'missing-key',
    'missing-role',
    'missing-mount',
    'null-ca',
    'token-mixed',
    'approle-mixed',
    'unknown-method',
    'case-method',
    'keep-fields',
    'remove-fields',
    'role-path',
    'role-traversal',
    'role-space',
    'role-long',
    'mount-traversal',
    'certificate-long',
    'key-long',
    'ca-long',
    'tuple-long',
    'malformed-pem',
    'encrypted-key',
    'unicode-pem',
    'trailing-pem-data',
    'http',
    'duplicate',
  ])('rejects %s before HTTP with a sanitized error', async (fault) => {
    const value: Record<string, unknown> = certificateReplacement()
    const intent = configuration()
    if (fault === 'missing-certificate') delete value.certificate_pem
    if (fault === 'missing-key') delete value.private_key_pem
    if (fault === 'missing-role') delete value.certificate_role
    if (fault === 'missing-mount') delete value.auth_mount
    if (fault === 'null-ca') value.server_ca_pem = null
    if (fault === 'token-mixed') value.token = 'private-token'
    if (fault === 'approle-mixed') value.role_id = 'private-role'
    if (fault === 'unknown-method') value.method = 'tls'
    if (fault === 'case-method') value.method = 'Certificate'
    if (fault === 'keep-fields') value.action = 'keep'
    if (fault === 'remove-fields') value.action = 'remove'
    if (fault === 'role-path') value.certificate_role = 'role/path'
    if (fault === 'role-traversal') value.certificate_role = '..'
    if (fault === 'role-space') value.certificate_role = 'role name'
    if (fault === 'role-long') value.certificate_role = 'r'.repeat(129)
    if (fault === 'mount-traversal') value.auth_mount = 'cert/../team'
    if (fault === 'certificate-long') value.certificate_pem = 'c'.repeat(16385)
    if (fault === 'key-long') value.private_key_pem = 'k'.repeat(16385)
    if (fault === 'ca-long') value.server_ca_pem = 'a'.repeat(16385)
    if (fault === 'tuple-long') {
      value.certificate_pem =
        '-----BEGIN CERTIFICATE-----\n' + 'A'.repeat(16300) + '\n-----END CERTIFICATE-----\n'
      value.private_key_pem =
        '-----BEGIN PRIVATE KEY-----\n' + 'A'.repeat(16300) + '\n-----END PRIVATE KEY-----\n'
      value.server_ca_pem = certificatePEM
    }
    if (fault === 'malformed-pem') value.certificate_pem = 'private-material'
    if (fault === 'encrypted-key')
      value.private_key_pem = privateKeyPEM.replaceAll('PRIVATE KEY', 'ENCRYPTED PRIVATE KEY')
    if (fault === 'unicode-pem') value.certificate_pem = '\u00a0' + certificatePEM
    if (fault === 'trailing-pem-data') value.certificate_pem = certificatePEM + 'private-tail'
    if (fault === 'http') intent.input.descriptor.endpoint = 'http://vault.test'
    Object.assign(intent.input, { writer_auth: value })
    if (fault === 'duplicate') Object.assign(intent.input, { reader_auth: structuredClone(value) })
    const spy = vi.spyOn(client, 'request')
    await expect(saveVaultIntegration(intent, 'c'.repeat(64))).rejects.toThrow(
      'Vault integration request failed',
    )
    expect(spy).not.toHaveBeenCalled()
  })
  it('bounds exact PEM and canonical private encoding separately and preserves optional system trust', () => {
    const value = certificateReplacement()
    delete value.server_ca_pem
    expect(validVaultCertificateAuth(value)).toBe(true)
    expect(sameVaultAuthReplacement(value, { ...value, server_ca_pem: '' })).toBe(true)
    expect(sameVaultAuthReplacement(value, { ...value, certificate_role: 'reader-role' })).toBe(
      false,
    )
    expect(sameVaultAuthReplacement(value, { action: 'replace', token: privateKeyPEM })).toBe(false)
    const prefix = '-----BEGIN CERTIFICATE-----\n',
      suffix = '\n-----END CERTIFICATE-----\n'
    value.certificate_pem = prefix + 'A'.repeat(16384 - prefix.length - suffix.length) + suffix
    expect(validVaultCertificateAuth(value)).toBe(true)
    value.certificate_pem += '\n'
    expect(validVaultCertificateAuth(value)).toBe(false)
  })
  it('rejects returned saved certificate material instead of caching or returning it', async () => {
    const intent = configuration()
    intent.input.writer_auth = certificateReplacement()
    vi.spyOn(client, 'request').mockResolvedValue({
      status: 200,
      data: {
        request_id: requestID,
        integration_id: vaultID,
        revision_id: vault().revision_id,
        committed: true,
        changed: true,
        private_key_pem: privateKeyPEM,
      },
    })
    await expect(saveVaultIntegration(intent, 'c'.repeat(64))).rejects.toThrow(VaultError)
  })
})

it('rejects an oversized complete config even when each certificate tuple is individually bounded', async () => {
  const intent = configuration()
  const largeCertificate =
    '-----BEGIN CERTIFICATE-----\n' + 'A'.repeat(16000) + '\n-----END CERTIFICATE-----\n'
  const writer = {
    ...certificateReplacement(),
    certificate_pem: largeCertificate,
    server_ca_pem: largeCertificate,
  }
  const reader = { ...writer, certificate_role: 'reader-role' }
  expect(validVaultCertificateAuth(writer)).toBe(true)
  expect(validVaultCertificateAuth(reader)).toBe(true)
  intent.input.writer_auth = writer
  intent.input.reader_auth = reader
  intent.input.name = 'N'.repeat(100)
  intent.input.reason = 'R'.repeat(1000)
  const spy = vi.spyOn(client, 'request')
  await expect(saveVaultIntegration(intent, 'c'.repeat(64))).rejects.toThrow(VaultError)
  expect(spy).not.toHaveBeenCalled()
})
