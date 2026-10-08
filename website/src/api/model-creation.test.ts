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
  validManualModelName,
} from './model-creation'
import type {
  ModelCreationConnection,
  ModelCreationInput,
  ModelCreationResult,
} from '@/types/model-creation'
const original = client.defaults.adapter
const requestId = '12345678-1234-4234-8234-123456789abc'
const connection: ModelCreationConnection = {
  id: 'con_one',
  provider_id: 'prv_one',
  provider_name: 'Provider',
  name: 'Connection',
  protocol: 'openai_chat',
  adapter: 'native',
  api_version: null,
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
  initial_target: null,
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

describe('guided initial targets', () => {
  it('rejects missing, forged or inconsistent initial choices instead of inventing defaults', async () => {
    for (const choice of [
      undefined,
      { target: 'new', name: 'other' },
      { target: 'existing', model_id: 'MDL_bad', name: 'upstream', initial_weight: 0 },
      { target: 'existing', model_id: 'mdl_old', name: 'other', initial_weight: 0 },
      { target: 'existing', model_id: 'mdl_old', name: 'upstream', initial_weight: 50 },
      { target: 'new', name: 'upstream', secret: 'bad' },
    ]) {
      value = { items: [{ ...pm, initial_target: choice }], next_cursor: null }
      await expect(listModelCreationProviderModels('con_one')).rejects.toThrow(
        'Invalid model creation response',
      )
    }
  })
})

it('accepts only source-exact new/existing choices, preserving null and the authorized off-page target', async () => {
  for (const choice of [
    null,
    { target: 'new', name: 'upstream' },
    { target: 'existing', model_id: 'mdl_offpage', name: 'upstream', initial_weight: 0 },
    { target: 'existing', model_id: 'mdl_offpage', name: 'upstream', initial_weight: 100 },
  ]) {
    value = { items: [{ ...pm, initial_target: choice }], next_cursor: null }
    expect((await listModelCreationProviderModels('con_one')).items[0].initial_target).toEqual(
      choice,
    )
  }
  value = {
    items: [
      {
        ...pm,
        selectable: false,
        blocker_codes: ['provider_model_associated'],
        initial_target: { target: 'new', name: 'upstream' },
      },
    ],
    next_cursor: null,
  }
  await expect(listModelCreationProviderModels('con_one')).rejects.toThrow(
    'Invalid model creation response',
  )
})

describe('manual upstream-name atomic selector', () => {
  const manual = { upstream_name: 'not/discovered', target: 'new' as const, name: 'Public' }
  const manualInput: ModelCreationInput = { ...input, items: [manual] }
  const reviewed = () => ({
    connection,
    items: [
      {
        provider_model_id: '',
        upstream_name: manual.upstream_name,
        target: 'new',
        model_id: null,
        name: 'Public',
        protocol: 'openai_chat',
        initial_weight: 100,
        blocker_codes: [],
        warning_codes: ['credential_coverage_unproven'],
      },
    ],
    review_etag: 'a'.repeat(64),
    observed_at: '2026-10-04T00:00:00Z',
    can_commit: true,
  })
  it('reviews a manual configuration with explicit unproven coverage and no invented identifier', async () => {
    value = reviewed()
    const got = await previewModelCreation('con_one', [manual], 'csrf')
    expect(got.items[0].provider_model_id).toBe('')
    expect(got.items[0].warning_codes).toEqual(['credential_coverage_unproven'])
    expect(JSON.parse(requests[0].data)).toEqual({ items: [manual] })
  })
  it.each([
    undefined,
    [],
    ['credential_coverage_missing'],
    ['credential_coverage_unproven', 'extra'],
  ])('rejects incomplete/forged manual coverage warning %j', async (warning_codes) => {
    value = { ...reviewed(), items: [{ ...reviewed().items[0], warning_codes }] }
    await expect(previewModelCreation('con_one', [manual], 'csrf')).rejects.toThrow()
  })
  it('binds newly assigned IDs to the exact original name through uncertain retry', async () => {
    const r = result()
    const manualRow = { ...row, name: 'Public', manual_upstream_name: manual.upstream_name }
    r.receipt.items = [manualRow]
    r.current_items = [manualRow]
    value = r
    status = 201
    const got = await createModelBatch('con_one', manualInput, 'a'.repeat(64), 'fresh-csrf')
    expect(got.receipt.items[0].manual_upstream_name).toBe(manual.upstream_name)
    expect(JSON.parse(requests[0].data)).toEqual(manualInput)
    for (const replacement of [
      { ...manualRow, manual_upstream_name: 'other' },
      { ...row, name: 'Public' },
    ]) {
      value = {
        ...r,
        receipt: { ...r.receipt, items: [replacement] },
        current_items: [replacement],
      }
      await expect(
        createModelBatch('con_one', manualInput, 'a'.repeat(64), 'new-csrf'),
      ).rejects.toThrow()
    }
  })
  it.each([
    { upstream_name: 'a', provider_model_id: 'pmd_one', target: 'new', name: 'Public' },
    { upstream_name: 'a', provider_model_id: undefined, target: 'new', name: 'Public' },
    { upstream_name: ' a', target: 'new', name: 'Public' },
    { upstream_name: 'a', target: 'new', name: 'Public', credential_ready: true },
    { upstream_name: 'a', target: 'new', name: 'Public', weight: 100 },
  ])('rejects invalid manual union before request %j', async (item) => {
    await expect(
      createModelBatch(
        'con_one',
        { ...input, items: [item] as ModelCreationInput['items'] },
        'a'.repeat(64),
        'csrf',
      ),
    ).rejects.toThrow()
    expect(requests).toHaveLength(0)
  })
  it('retains native name grammar independently from public-name grammar', () => {
    expect(validManualModelName('native/name', connection)).toBe(true)
    expect(
      validManualModelName('native/name', { ...connection, protocol: 'gemini_generate_content' }),
    ).toBe(false)
    expect(
      validManualModelName('native/name', {
        ...connection,
        adapter: 'azure_openai_classic',
        api_version: '2026-01-01',
      }),
    ).toBe(false)
    expect(
      validManualModelName('deployment-1', {
        ...connection,
        adapter: 'azure_openai_classic',
        api_version: '2026-01-01',
      }),
    ).toBe(true)
    for (const name of [' name', 'name ', 'a\n', 'x'.repeat(256), 'bad\ud800'])
      expect(validManualModelName(name, connection)).toBe(false)
  })
})

it('requires every manual original exactly once despite distinct assigned canonical IDs', async () => {
  const two: ModelCreationInput = {
    ...input,
    items: [
      { upstream_name: 'manual-A', target: 'new', name: 'PublicA' },
      { upstream_name: 'manual-B', target: 'new', name: 'PublicB' },
    ],
  }
  const first = { ...row, manual_upstream_name: 'manual-A', name: 'PublicA' }
  const duplicate = {
    ...first,
    provider_model_id: 'pmd_two',
    model_id: 'mdl_two',
    binding_id: 'bnd_two',
  }
  const r = result()
  r.receipt.items = [first, duplicate]
  r.current_items = [first, duplicate]
  value = r
  status = 201
  await expect(createModelBatch('con_one', two, 'a'.repeat(64), 'csrf')).rejects.toThrow(
    'Invalid model creation response',
  )
})

it('correlates reordered manual receipts with every exact original name', async () => {
  const two: ModelCreationInput = {
    ...input,
    items: [
      { upstream_name: 'manual-A', target: 'new', name: 'PublicA' },
      { upstream_name: 'manual-B', target: 'new', name: 'PublicB' },
    ],
  }
  const first = { ...row, manual_upstream_name: 'manual-A', name: 'PublicA' }
  const second = {
    ...row,
    manual_upstream_name: 'manual-B',
    name: 'PublicB',
    provider_model_id: 'pmd_two',
    model_id: 'mdl_two',
    binding_id: 'bnd_two',
  }
  const r = result()
  r.receipt.items = [second, first]
  r.current_items = [second, first]
  value = r
  status = 201
  expect((await createModelBatch('con_one', two, 'a'.repeat(64), 'csrf')).receipt.items).toEqual([
    second,
    first,
  ])
})
