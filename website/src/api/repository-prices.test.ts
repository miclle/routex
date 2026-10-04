import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  applyRepositoryIntent,
  getRepositoryCandidates,
  getRepositoryConfig,
  getRepositoryReceipt,
  parseRepositoryConfig,
  parseRepositoryPreview,
  parseRepositoryResult,
  previewRepositoryPrices,
  validRepositoryReason,
  validateRepositorySelection,
} from './repository-prices'
import {
  configuration,
  differences,
  digest,
  etag,
  intent,
  result,
  selection,
} from '@/views/price-imports/repository/fixtures'
const original = client.defaults.adapter
let calls: InternalAxiosRequestConfig[], response: unknown, status: number
beforeEach(() => {
  calls = []
  response = configuration()
  status = 200
  client.defaults.adapter = async (config) => {
    calls.push(config)
    const value = { config, status, statusText: '', headers: new AxiosHeaders(), data: response }
    if (status >= 400) throw new AxiosError('private raw response', '', config, undefined, value)
    return value
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
describe('repository source transport and structural boundaries', () => {
  it('reads only scoped configuration without a directory or secret', async () => {
    expect(await getRepositoryConfig()).toEqual(configuration())
    expect(calls.map((c) => c.url)).toEqual(['/admin/prices/repository'])
  })
  it.each(['enabled', 'can_write', 'mappings', 'source', 'review_etag', 'last_success_at'])(
    'rejects missing %s',
    (key) => {
      const value = configuration() as unknown as Record<string, unknown>
      delete value[key]
      expect(() => parseRepositoryConfig(value)).toThrow()
    },
  )
  it.each([
    ['enabled', null],
    ['enabled', 'true'],
    ['review_etag', 'a'.repeat(32)],
    ['last_result', 'PRIVATE DATA'],
    ['last_success_at', 'yesterday'],
    ['mappings', null],
  ])('rejects malformed %s', (key, value) => {
    expect(() => parseRepositoryConfig({ ...configuration(), [key]: value })).toThrow()
  })
  it('rejects extra private configuration fields, duplicate mappings and ambiguous source identities', () => {
    expect(() => parseRepositoryConfig({ ...configuration(), secret: 'private' })).toThrow()
    const value = configuration()
    value.mappings.push(value.mappings[0])
    expect(() => parseRepositoryConfig(value)).toThrow()
    const other = configuration()
    other.source.models.push(other.source.models[0])
    other.source.model_count++
    expect(() => parseRepositoryConfig(other)).toThrow()
  })
  it('accepts truthful empty shipped source and an orphan mapping without inventing prices', () => {
    const value = configuration()
    value.source.models = []
    value.source.model_count = 0
    expect(parseRepositoryConfig(value).source.models).toEqual([])
  })
  it.each(['', ' reason', 'reason ', 'line\nreason', 'bad\u0000', '\ud800', 'x'.repeat(1001)])(
    'rejects invalid reason %j',
    (value) => {
      expect(validRepositoryReason(value)).toBe(false)
    },
  )
  it('uses Unicode characters for the reason boundary', () => {
    expect(validRepositoryReason('😀'.repeat(1000))).toBe(true)
  })
  it.each([
    { ...selection, provider_model_ids: [] },
    { ...selection, provider_model_ids: ['pmo_one', 'pmo_one'] },
    { ...selection, provider_model_ids: ['../private'] },
    { ...selection, rate_ids: ['rat_one'] },
    { mode: 'restore', provider_model_ids: ['pmo_one'], rate_ids: [] },
    { ...selection, provider_model_ids: Array.from({ length: 21 }, (_, i) => `pmo_${i}`) },
  ])('rejects ambiguous or expanded selectors', (value) => {
    expect(() => validateRepositorySelection(value)).toThrow()
  })
  it('preserves exact zero, disabled state and eighteen decimal places in server differences', () => {
    const value = parseRepositoryPreview(differences(), selection)
    expect(value.changes[0].before?.amount).toBe('0')
    expect(value.changes[0].before?.enabled).toBe(false)
    expect(value.changes[0].after?.amount).toBe('1.234567890123456789')
  })
  it.each([
    'provider_model_id',
    'rate_id',
    'source_model_key',
    'source_rate_key',
    'before_source',
    'after_source',
  ])('rejects corrupted difference %s', (key) => {
    const value = differences()
    ;(value.changes[0] as unknown as Record<string, unknown>)[key] = key.includes('source')
      ? {}
      : 'another'
    expect(() => parseRepositoryPreview(value, selection)).toThrow()
  })
  it('rejects rates outside explicit restore selection and inconsistent provenance', () => {
    const selected = {
      mode: 'restore' as const,
      provider_model_ids: ['pmo_one'],
      rate_ids: ['rat_other'],
    }
    expect(() => parseRepositoryPreview(differences(selected), selected)).toThrow()
    const value = differences()
    value.changes[0].before_source.source_model_key = 'provider/model'
    expect(() => parseRepositoryPreview(value, selection)).toThrow()
  })
  it('rejects false validity/digest combinations and valid errors', () => {
    expect(() => parseRepositoryPreview({ ...differences(), valid: false }, selection)).toThrow()
    expect(() =>
      parseRepositoryPreview({ ...differences(), preview_digest: '' }, selection),
    ).toThrow()
    expect(() =>
      parseRepositoryPreview(
        {
          ...differences(),
          errors: [
            { provider_model_id: 'pmo_one', rate_id: null, code: 'missing', message: 'Missing' },
          ],
        },
        selection,
      ),
    ).toThrow()
  })
  it('bounds q in UTF8 bytes and cursor by exact resource ID without dispatch', async () => {
    await expect(getRepositoryCandidates('中'.repeat(67), null)).rejects.toThrow()
    await expect(getRepositoryCandidates('', 'a'.repeat(31))).rejects.toThrow()
    expect(calls).toHaveLength(0)
  })
  it('uses a bounded candidate page and normalizes only empty terminal cursor', async () => {
    response = {
      items: [
        { provider_model_id: 'pmo_one', upstream_name: 'Real model', protocol: 'openai_chat' },
      ],
      next_cursor: '',
    }
    expect((await getRepositoryCandidates('literal_%', 'pmo_previous')).next_cursor).toBeNull()
    expect(calls[0].params).toEqual({ q: 'literal_%', cursor: 'pmo_previous', limit: 20 })
    response = { items: [], next_cursor: null }
    await expect(getRepositoryCandidates('', null)).rejects.toThrow()
  })
  it('previews only captured selectors with current CSRF, never price bytes', async () => {
    response = differences()
    await previewRepositoryPrices(selection, 'csrf-current')
    expect(JSON.parse(calls[0].data)).toEqual(selection)
    expect(calls[0].headers.get('X-CSRF-Token')).toBe('csrf-current')
    expect(calls[0].headers.has('Authorization')).toBe(false)
  })
  it.each([200, 201])(
    'preserves durable configure UUID/body/strong review with status %d',
    async (value) => {
      const captured = intent()
      response = result(captured)
      status = value
      await applyRepositoryIntent(captured, 'csrf-current')
      expect(JSON.parse(calls[0].data)).toEqual(captured.input)
      expect(calls[0].headers.get('If-Match')).toBe(`"${etag}"`)
      expect(calls[0].method).toBe('put')
    },
  )
  it('applies captured digest and selectors without amount transformation', async () => {
    const captured = {
      kind: 'apply' as const,
      etag,
      sourceDigest: digest,
      input: {
        request_id: intent().input.request_id,
        preview_digest: 'c'.repeat(64),
        selection,
        reason: 'Explicit application',
      },
    }
    response = result(captured)
    await applyRepositoryIntent(captured, 'csrf-current')
    expect(calls[0].url).toBe('/admin/prices/repository/apply')
    expect(JSON.parse(calls[0].data)).toEqual(captured.input)
  })
  it.each([401, 403, 404, 409, 422, 500])(
    'does not replay rejected status %d or expose raw HTTP data',
    async (value) => {
      status = value
      await expect(applyRepositoryIntent(intent(), 'csrf-current')).rejects.toMatchObject({
        status: value,
        message: 'Repository pricing request failed',
      })
      expect(calls).toHaveLength(1)
    },
  )
  it('reads exact authorized receipt without replaying mutation', async () => {
    response = result()
    await getRepositoryReceipt(intent())
    expect(calls).toHaveLength(1)
    expect(calls[0].method).toBe('get')
    expect(calls[0].url).toContain(`/receipts/${intent().input.request_id}`)
  })
  it('rejects wrong receipt owner intent, mode and publication claim', () => {
    for (const value of [
      {
        ...result(),
        receipt: { ...result().receipt, request_id: '22222222-2222-4222-8222-222222222222' },
      },
      { ...result(), runtime_applied: true },
      { ...result(), receipt: { ...result().receipt, source_digest: 'd'.repeat(64) } },
      { ...result(), configuration_applied: false },
    ])
      expect(() => parseRepositoryResult(value, intent())).toThrow()
  })
  it('retains historical superseded receipt without claiming current configuration', () => {
    expect(
      parseRepositoryResult(
        { ...result(), application_status: 'superseded', configuration_applied: false },
        intent(),
      ).committed,
    ).toBe(true)
  })
})
