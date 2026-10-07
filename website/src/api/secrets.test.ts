import { afterEach, describe, expect, it, vi } from 'vitest'
import client from './client'
import {
  applySecretIntent,
  getSecretStore,
  parseSecretResult,
  parseSecretStore,
  SecretError,
  validSecretReason,
} from './secrets'
import { job, receipt, rotationId, store } from '@/views/secrets/fixtures'
import type { SecretIntent } from '@/types/secrets'
const intent: SecretIntent = {
  action: 'start',
  etag: 'a'.repeat(64),
  input: {
    request_id: '10000000-0000-4000-8000-000000000001',
    target_key_id: 'next',
    reason: 'Reviewed root rotation',
  },
}
afterEach(() => vi.restoreAllMocks())
describe('strict private Secret Store boundary', () => {
  it('preserves exact epoch/domain counts, nullable proof and unknown blocker codes', () => {
    const value = store()
    value.rotation = job()
    value.rotation.blocker_codes = ['future_blocker']
    value.process = {
      id: null,
      verified: false,
      policy_epoch: null,
      snapshot_id: null,
      verified_at: null,
    }
    expect(parseSecretStore(value)).toEqual(value)
  })
  it.each([
    [
      'numeric epoch',
      (v: ReturnType<typeof store>) => {
        Object.assign(v.policy, { epoch: Number('9007199254740993') })
      },
    ],
    [
      'leading zero',
      (v: ReturnType<typeof store>) => {
        v.policy.epoch = '01'
      },
    ],
    [
      'overflow',
      (v: ReturnType<typeof store>) => {
        v.policy.epoch = '18446744073709551616'
      },
    ],
    [
      'secret material',
      (v: ReturnType<typeof store>) => {
        Object.assign(v, { key: 'sensitive' })
      },
    ],
    [
      'nested ciphertext',
      (v: ReturnType<typeof store>) => {
        Object.assign(v.keys[0], { ciphertext: 'sensitive' })
      },
    ],
    [
      'duplicate slot',
      (v: ReturnType<typeof store>) => {
        v.keys.push(v.keys[0])
      },
    ],
    [
      'missing mode',
      (v: ReturnType<typeof store>) => {
        delete (v as Partial<typeof v>).mode
      },
    ],
    [
      'bad root ID',
      (v: ReturnType<typeof store>) => {
        v.keys[0].id = ' old'
      },
    ],
  ])('rejects %s', (_, change) => {
    const value = store()
    change(value)
    expect(() => parseSecretStore(value)).toThrow(SecretError)
  })
  it.each(['missing', 'duplicate', 'order', 'numeric', 'unknown-action', 'invalid-date'])(
    'rejects invalid rotation %s',
    (kind) => {
      const value = store()
      value.rotation = job()
      if (kind === 'missing') value.rotation.domains.pop()
      if (kind === 'duplicate') value.rotation.domains[1] = value.rotation.domains[0]
      if (kind === 'order') value.rotation.domains.reverse()
      if (kind === 'numeric') Object.assign(value.rotation.domains[0], { scanned: 0 })
      if (kind === 'unknown-action') Object.assign(value.rotation, { allowed_actions: ['destroy'] })
      if (kind === 'invalid-date') value.rotation.observation_eligible_at = 'tomorrow'
      expect(() => parseSecretStore(value)).toThrow(SecretError)
    },
  )
  it('accepts historical null job and independently unavailable current application', () => {
    const value = receipt(intent)
    value.rotation = null
    value.application_status = 'unavailable'
    value.publication_applied = false
    value.write_policy_applied = false
    expect(parseSecretResult(value, intent)).toEqual(value)
  })
  it.each(['UUID', 'action', 'rotation', 'uncommitted', 'extra'])(
    'rejects unrelated receipt %s',
    (kind) => {
      const value = receipt(intent)
      if (kind === 'UUID') value.receipt.request_id = '20000000-0000-4000-8000-000000000001'
      if (kind === 'action') value.receipt.action = 'retire'
      if (kind === 'rotation') value.rotation!.id = 'srt_01k00000000000000000000001'
      if (kind === 'uncommitted') Object.assign(value, { committed: false })
      if (kind === 'extra') Object.assign(value.receipt, { secret: 'sensitive' })
      expect(() => parseSecretResult(value, intent)).toThrow(SecretError)
    },
  )
  it('uses exact scoped GET with signal and validates addressed rotation', async () => {
    const get = vi
      .spyOn(client, 'get')
      .mockResolvedValue({ status: 200, data: { ...store(), rotation: job() } })
    const signal = new AbortController().signal
    await getSecretStore(rotationId, signal)
    expect(get).toHaveBeenCalledExactlyOnceWith(`/admin/secrets/rotations/${rotationId}`, {
      signal,
    })
    await expect(getSecretStore('bad', signal)).rejects.toThrow(SecretError)
    get.mockResolvedValue({ status: 200, data: store() })
    await expect(getSecretStore(rotationId)).rejects.toThrow(SecretError)
  })
  it.each(['start', 'resume', 'retire', 'rollback'] as const)(
    'sends one %s with captured strong ETag/current CSRF and no key headers',
    async (action) => {
      const captured: SecretIntent =
        action === 'start'
          ? intent
          : {
              action,
              rotationId,
              etag: intent.etag,
              input: { request_id: intent.input.request_id, reason: intent.input.reason },
            }
      const post = vi
        .spyOn(client, 'post')
        .mockResolvedValue({ status: action === 'start' ? 201 : 200, data: receipt(captured) })
      const signal = new AbortController().signal
      await applySecretIntent(captured, 'b'.repeat(64), signal)
      expect(post).toHaveBeenCalledExactlyOnceWith(
        action === 'start'
          ? '/admin/secrets/rotations'
          : `/admin/secrets/rotations/${rotationId}/${action}`,
        captured.input,
        { signal, headers: { 'If-Match': `"${captured.etag}"`, 'X-CSRF-Token': 'b'.repeat(64) } },
      )
    },
  )
  it('rejects invalid intent before transport', async () => {
    const post = vi.spyOn(client, 'post')
    await expect(applySecretIntent({ ...intent, etag: 'weak' }, 'b'.repeat(64))).rejects.toThrow(
      SecretError,
    )
    expect(post).not.toHaveBeenCalled()
  })
  it('drops Axios payload/config details and never retries', async () => {
    const post = vi.spyOn(client, 'post').mockRejectedValue({
      isAxiosError: true,
      response: { status: 409, data: { key: 'secret' } },
      config: { data: 'private' },
    })
    await expect(applySecretIntent(intent, 'b'.repeat(64))).rejects.toMatchObject({
      status: 409,
      message: 'Secret store request failed',
    })
    expect(post).toHaveBeenCalledTimes(1)
  })
  it('reason uses Unicode characters rather than UTF8 bytes; rejects controls/unpaired surrogates/trim changes', () => {
    expect(validSecretReason('界'.repeat(1000))).toBe(true)
    expect(validSecretReason('😀'.repeat(1000))).toBe(true)
    for (const reason of ['', ' x', 'x ', 'x\n', 'x\u0000', '\ud800', '界'.repeat(1001)])
      expect(validSecretReason(reason)).toBe(false)
  })
})
it('rejects a verified process without a complete current generation proof', () => {
  const value = store()
  value.process.snapshot_id = null
  expect(() => parseSecretStore(value)).toThrow(SecretError)
  value.process.snapshot_id = 'snapshot'
  value.process.policy_epoch = '1'
  expect(() => parseSecretStore(value)).toThrow(SecretError)
})
it('rejects an applied response without both application proofs', () => {
  const value = receipt(intent)
  value.publication_applied = false
  expect(() => parseSecretResult(value, intent)).toThrow(SecretError)
  expect(validSecretReason(null)).toBe(false)
})

it('keeps historical five-domain coverage independent from current seven-domain inventory', () => {
  const value = store()
  value.rotation = job()
  expect(parseSecretStore(value).rotation?.domains).toHaveLength(5)
  value.rotation.inventory_version = 2
  value.rotation.domains.push(
    ...(['vault_writer_auth', 'vault_reader_auth'] as const).map((code) => ({
      code,
      coverage: 'not_scanned' as const,
      scanned: null,
      rewrapped: null,
      already_target: null,
      deleted: null,
      changed: null,
      blocked: null,
    })),
  )
  expect(parseSecretStore(value).rotation?.domains).toHaveLength(7)
  expect(parseSecretStore(value).rotation?.domains[5].scanned).toBeNull()
  value.rotation.domains[5].scanned = '0'
  expect(() => parseSecretStore(value)).toThrow(SecretError)
})
it('rejects unknown/missing inventory versions and five/seven aliasing', () => {
  const value = store()
  Object.assign(value, { inventory_version: 1 })
  expect(() => parseSecretStore(value)).toThrow()
  value.inventory_version = 2
  value.rotation = job()
  value.rotation.inventory_version = 2
  expect(() => parseSecretStore(value)).toThrow()
  value.rotation.inventory_version = 1
  Object.assign(value.rotation.domains[0], { coverage: 'not_scanned' })
  expect(() => parseSecretStore(value)).toThrow()
})
