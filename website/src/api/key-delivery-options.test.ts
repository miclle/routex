import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getVaultIntegrations } from './vault-integrations'
import { getVaultDeliveryOptions, KeyDeliveryOptionsError } from './key-delivery-options'
import type { VaultIntegration, VaultPage } from '@/types/vault-integrations'

vi.mock('./vault-integrations', () => ({ getVaultIntegrations: vi.fn() }))
const read = vi.mocked(getVaultIntegrations)
const review = 'a'.repeat(64) + '.' + 'b'.repeat(64)
function item(id: string): VaultIntegration {
  return {
    id,
    name: 'Application Vault',
    revision_id: 'revision',
    descriptor: {
      endpoint: 'https://vault.example.test',
      namespace: 'production',
      mount: 'kv',
      prefix: 'internal',
      data_field: 'value',
    },
    writer_auth: { method: 'token', configured: true },
    reader_auth: { method: 'token', configured: true },
    review_etag: review,
    can_write: false,
    can_test: false,
    last_probe: null,
  }
}
const page = (items: VaultIntegration[], next_cursor: string | null = null): VaultPage => ({
  items,
  next_cursor,
  review_etag: review,
  can_write: false,
  can_test: false,
})
beforeEach(() => read.mockReset())
describe('delivery Vault destination choices', () => {
  it('loads one coherent bounded catalogue and retains only reviewed display metadata', async () => {
    const signal = new AbortController().signal
    read.mockResolvedValueOnce(page([item('first')], 'first'))
    read.mockResolvedValueOnce(page([item('second')]))
    const result = await getVaultDeliveryOptions(signal)
    expect(read.mock.calls).toEqual([
      ['', signal],
      ['first', signal],
    ])
    expect(result).toEqual(
      ['first', 'second'].map((id) => ({
        id,
        name: 'Application Vault',
        endpoint: 'https://vault.example.test',
        namespace: 'production',
        mount: 'kv',
      })),
    )
    expect(
      result.some((row) => 'writer_auth' in row || 'prefix' in row || 'last_probe' in row),
    ).toBe(false)
  })
  it('rejects a catalogue change instead of returning partial choices', async () => {
    read.mockResolvedValueOnce(page([item('first')], 'first'))
    read.mockResolvedValueOnce({
      ...page([item('second')]),
      review_etag: 'c'.repeat(64) + '.' + 'd'.repeat(64),
    })
    await expect(getVaultDeliveryOptions()).rejects.toMatchObject({ kind: 'changed' })
  })
  it.each(['duplicate', 'loop'])('rejects %s pages before further reads', async (kind) => {
    read.mockResolvedValueOnce(page([item('first')], 'first'))
    read.mockResolvedValueOnce(
      kind === 'duplicate' ? page([item('first')]) : page([item('second')], 'first'),
    )
    await expect(getVaultDeliveryOptions()).rejects.toMatchObject({ kind: 'changed' })
    expect(read).toHaveBeenCalledTimes(2)
  })
  it('rejects overflow after the fixed page budget without returning a partial catalogue', async () => {
    for (let n = 0; n < 10; n++) read.mockResolvedValueOnce(page([item('item-' + n)], 'item-' + n))
    await expect(getVaultDeliveryOptions()).rejects.toMatchObject({ kind: 'overflow' })
    expect(read).toHaveBeenCalledTimes(10)
  })
  it('rejects a late page after authority cancellation and makes no subsequent read', async () => {
    const controller = new AbortController()
    read.mockImplementationOnce(async () => {
      controller.abort()
      return page([item('first')], 'first')
    })
    await expect(getVaultDeliveryOptions(controller.signal)).rejects.toMatchObject({
      kind: 'unavailable',
    })
    expect(read).toHaveBeenCalledTimes(1)
    await expect(getVaultDeliveryOptions(controller.signal)).rejects.toThrow(
      KeyDeliveryOptionsError,
    )
    expect(read).toHaveBeenCalledTimes(1)
  })
  it('drops transport detail from a failed page instead of exposing a partial catalogue', async () => {
    read.mockResolvedValueOnce(page([item('first')], 'first'))
    read.mockRejectedValueOnce({ message: 'must-not-render', config: { token: 'must-not-render' } })
    await expect(getVaultDeliveryOptions()).rejects.toEqual(
      new KeyDeliveryOptionsError('unavailable'),
    )
  })
  it('accepts a genuinely empty authorized catalogue', async () => {
    read.mockResolvedValueOnce(page([]))
    expect(await getVaultDeliveryOptions()).toEqual([])
  })
})
