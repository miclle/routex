import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from './client'
import {
  createModelBatch,
  getModelCreationContext,
  getModelCreationReceipt,
  listModelCreationConnections,
  listModelCreationProviderModels,
  listModelCreationTargets,
  modelCreationOutcomeUnknown,
  previewModelCreation,
  validModelCreationName,
  validModelCreationReason,
} from './model-creation'
import type { ModelCreationInput, ModelCreationResult } from '@/types/model-creation'
const original = client.defaults.adapter
const requestId = '12345678-1234-4234-8234-123456789abc'
const connection = {
  id: 'con_one',
  provider_id: 'prv_one',
  provider_name: 'Provider',
  name: 'Connection',
  protocol: 'openai_chat',
  base_url: 'https://example.invalid/v1',
}
const pm = {
  id: 'pmd_one',
  upstream_name: 'upstream',
  disabled: false,
  input_capabilities: ['image'],
  credential_ready: true,
  selectable: true,
  blocker_codes: [],
}
const target = {
  id: 'mdl_old',
  name: 'Existing',
  initial_weight: 0,
  selectable: true,
  blocker_codes: [],
}
const input: ModelCreationInput = {
  request_id: requestId,
  reason: 'reviewed reason',
  items: [{ provider_model_id: 'pmd_one', target: 'new', name: 'Exact/Name' }],
}
const row = {
  provider_model_id: 'pmd_one',
  model_id: 'mdl_new',
  binding_id: 'bnd_one',
  created_model: true,
  name: 'Exact/Name',
  protocol: 'openai_chat' as const,
  weight: 100,
}
function result(): ModelCreationResult {
  return {
    receipt: {
      request_id: requestId,
      connection_id: 'con_one',
      created_at: '2026-10-04T00:00:00Z',
      items: [row],
    },
    committed: true,
    changed: true,
    current_items: [row],
    runtime_applied: true,
    application_status: 'applied',
  }
}
let value: unknown, requests: InternalAxiosRequestConfig[], status: number
beforeEach(() => {
  requests = []
  status = 200
  value = result()
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return {
      config,
      data: structuredClone(value),
      status,
      statusText: '',
      headers: new AxiosHeaders(),
    }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
describe('bounded model creation transport', () => {
  it('uses only exact Connection path, current CSRF, original UUID/body and strong review', async () => {
    status = 201
    await createModelBatch('con_one', input, 'a'.repeat(64), 'fresh-csrf')
    expect(requests[0].url).toBe('/admin/connections/con_one/model-creation')
    expect(JSON.parse(requests[0].data)).toEqual(input)
    expect(requests[0].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    expect(requests[0].headers.get('Authorization')).toBeUndefined()
    expect(Object.keys(JSON.parse(requests[0].data))).toEqual(['request_id', 'reason', 'items'])
  })
  it('reads historical receipt and current changed weights separately', async () => {
    const r = result()
    r.current_items![0] = { ...row, name: 'Renamed', weight: 50 }
    r.runtime_applied = false
    r.application_status = 'superseded'
    value = r
    const answer = await getModelCreationReceipt(requestId)
    expect(answer.receipt.items[0].weight).toBe(100)
    expect(answer.current_items![0].weight).toBe(50)
    expect(requests[0].method).toBe('get')
  })
  it('accepts unavailable current facts without inventing zero', async () => {
    value = {
      ...result(),
      current_items: null,
      runtime_applied: false,
      application_status: 'unavailable',
    }
    expect((await getModelCreationReceipt(requestId)).current_items).toBeNull()
  })
  it('validates bounded independent discovery pages', async () => {
    value = { items: [connection, { ...connection, id: 'con_two' }], next_cursor: 'con_next' }
    await listModelCreationConnections({ q: 'literal%_', cursor: 'con_prev', limit: 50 })
    expect(requests[0].params).toEqual({ q: 'literal%_', cursor: 'con_prev', limit: 50 })
    value = { connection, can_create: false, observed_at: '2026-10-04T00:00:00Z' }
    expect((await getModelCreationContext('con_one')).can_create).toBe(false)
    value = { items: [pm], next_cursor: null }
    await listModelCreationProviderModels('con_one')
    value = { items: [target], next_cursor: null }
    await listModelCreationTargets('con_one')
    expect(requests.map((x) => x.url)).not.toContain('/admin/providers')
  })
  it('posts a read-only preview with no reason, UUID, weight or grant fields', async () => {
    value = {
      connection,
      items: [
        {
          provider_model_id: 'pmd_one',
          upstream_name: 'upstream',
          target: 'new',
          model_id: null,
          name: 'Exact/Name',
          protocol: 'openai_chat',
          initial_weight: 100,
          blocker_codes: [],
        },
      ],
      review_etag: 'a'.repeat(64),
      observed_at: '2026-10-04T00:00:00Z',
      can_commit: true,
    }
    await previewModelCreation('con_one', input.items, 'csrf')
    expect(JSON.parse(requests[0].data)).toEqual({ items: input.items })
  })
  it.each([0, 51, 1.5])('rejects invalid page limit %s before dispatch', async (limit) => {
    await expect(listModelCreationConnections({ limit })).rejects.toThrow()
    expect(requests).toHaveLength(0)
  })
  it.each([
    { q: '' },
    { q: '中'.repeat(67) },
    { cursor: 'pmd_foreign' },
    { cursor: 'con_' + 'a'.repeat(27) },
  ])('rejects invalid bounded selector %j', async (f) => {
    await expect(listModelCreationConnections(f)).rejects.toThrow()
    expect(requests).toHaveLength(0)
  })
  it.each(['not-uuid', '12345678-1234-1234-8234-123456789abc'])(
    'rejects receipt ID %s before dispatch',
    async (id) => {
      await expect(getModelCreationReceipt(id)).rejects.toThrow()
      expect(requests).toHaveLength(0)
    },
  )
  it.each([
    { receipt: { ...result().receipt, request_id: '87654321-1234-4234-8234-123456789abc' } },
    { receipt: { ...result().receipt, connection_id: 'con_wrong' } },
    { receipt: { ...result().receipt, items: [{ ...row, name: 'wrong' }] } },
    { runtime_applied: false },
    { current_items: null },
    { current_items: [{ ...row, binding_id: 'bnd_wrong' }] },
    { committed: false },
    { application_status: ['superseded'], runtime_applied: false },
    { receipt: { ...result().receipt, items: [row, row] } },
  ])('rejects malformed or mismatched creation response %j', async (changes) => {
    value = { ...result(), ...changes }
    await expect(createModelBatch('con_one', input, 'a'.repeat(64), 'csrf')).rejects.toThrow(
      'Invalid model creation response',
    )
  })
  it('rejects unexpected success status as uncertain', async () => {
    status = 202
    await expect(createModelBatch('con_one', input, 'a'.repeat(64), 'csrf')).rejects.toThrow()
    expect(modelCreationOutcomeUnknown(new Error())).toBe(true)
  })
  it('rejects mismatched exact preview identity', async () => {
    value = {
      connection,
      items: [
        {
          provider_model_id: 'pmd_other',
          upstream_name: 'upstream',
          target: 'new',
          model_id: null,
          name: 'Exact/Name',
          protocol: 'openai_chat',
          initial_weight: 100,
          blocker_codes: [],
        },
      ],
      review_etag: 'a'.repeat(64),
      observed_at: '2026-10-04T00:00:00Z',
      can_commit: true,
    }
    await expect(previewModelCreation('con_one', input.items, 'csrf')).rejects.toThrow()
  })
  it.each([
    { items: [pm, pm], next_cursor: null },
    { items: [{ ...pm, selectable: true, disabled: true }], next_cursor: null },
    { items: [pm], next_cursor: 'con_wrong' },
    { items: [{ ...pm, blocker_codes: ['Unsafe text'] }], next_cursor: null },
  ])('rejects unsafe ProviderModel pages %j', async (v) => {
    value = v
    await expect(listModelCreationProviderModels('con_one')).rejects.toThrow()
  })
  it('preserves exact raw UTF8 reason bounds and name grammar', () => {
    expect(validModelCreationReason('中'.repeat(341))).toBe(true)
    expect(validModelCreationReason('中'.repeat(342))).toBe(false)
    expect(validModelCreationReason('  ')).toBe(false)
    expect(validModelCreationReason('reason\n')).toBe(false)
    expect(validModelCreationReason('bad\ud800')).toBe(false)
    expect(validModelCreationName('Exact/Name')).toBe(true)
    expect(validModelCreationName('x'.repeat(128))).toBe(true)
    expect(validModelCreationName('x'.repeat(129))).toBe(false)
    expect(validModelCreationName('Name\n')).toBe(false)
  })
  it.each(
    [
      [],
      [input.items[0], input.items[0]],
      [{ provider_model_id: 'pmd_one', target: 'new', name: 'bad space' }],
      [{ provider_model_id: 'pmd_one', target: 'existing', model_id: 'mdl_old', name: 'extra' }],
      [
        { provider_model_id: 'pmd_one', target: 'new', name: 'Same' },
        { provider_model_id: 'pmd_two', target: 'new', name: 'Same' },
      ],
      [
        { provider_model_id: 'pmd_one', target: 'existing', model_id: 'mdl_old' },
        { provider_model_id: 'pmd_two', target: 'existing', model_id: 'mdl_old' },
      ],
    ].map((items) => ({ items })),
  )('rejects invalid selected union before dispatch %j', async ({ items }) => {
    await expect(
      createModelBatch(
        'con_one',
        { ...input, items: items as ModelCreationInput['items'] },
        'a'.repeat(64),
        'csrf',
      ),
    ).rejects.toThrow()
    expect(requests).toHaveLength(0)
  })
})
