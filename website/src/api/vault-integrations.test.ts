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
  VaultError,
} from './vault-integrations'
import {
  configuration,
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
