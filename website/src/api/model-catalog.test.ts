import { afterEach, beforeEach, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { getModelCatalogRecord, listModelCatalog } from './model-catalog'
import type { ModelCatalogRecord } from '@/types/model-catalog'
const original = client.defaults.adapter
let data: unknown, requests: InternalAxiosRequestConfig[]
function fixture(): ModelCatalogRecord {
  return {
    id: 'mdl_one',
    name: 'model-one',
    status: 'active',
    created_at: '2026-09-01T00:00:00Z',
    protocols: ['openai_chat'],
    input_capabilities: { openai_chat: ['image', 'pdf'] },
    input_price: { state: 'unauthorized', rate: null },
    output_price: { state: 'unauthorized', rate: null },
    personal_available: false,
    sources: [
      {
        type: 'team',
        team_id: 'tem_legacy',
        team_name: 'Exact Team',
        invocation_supported: false,
        invocation_protocols: ['openai_responses'],
      },
    ],
  }
}
beforeEach(() => {
  data = fixture()
  requests = []
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { config, data, headers: new AxiosHeaders(), status: 200, statusText: '' }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
it('reads exact self-scoped detail and independent Team protocols with cancellation and no directory or credentials', async () => {
  const signal = new AbortController().signal
  expect(await getModelCatalogRecord('mdl_one', signal)).toEqual(fixture())
  expect(requests).toHaveLength(1)
  expect(requests[0].url).toBe('/model-catalog/mdl_one')
  expect(requests[0].signal).toBe(signal)
  expect(requests[0].params).toBeUndefined()
  expect(requests[0].headers.has('Authorization')).toBe(false)
})
it('validates a complete bounded list and rejects duplicate exact model identities', async () => {
  data = { items: [fixture()] }
  expect(await listModelCatalog()).toEqual([fixture()])
  data = { items: [fixture(), fixture()] }
  await expect(listModelCatalog()).rejects.toThrow('Invalid model catalogue response')
})
it.each([
  [
    'wrong target',
    (r: ModelCatalogRecord) => {
      r.id = 'mdl_other'
    },
  ],
  [
    'unsafe Team identity',
    (r: ModelCatalogRecord) => {
      r.sources[0].team_id = '../teams'
    },
  ],
  [
    'ambiguous Team identity',
    (r: ModelCatalogRecord) => {
      r.sources.push(structuredClone(r.sources[0]))
    },
  ],
  [
    'bad protocol structure',
    (r: ModelCatalogRecord) => {
      ;(r.sources[0] as { invocation_protocols: unknown }).invocation_protocols = 'openai_chat'
    },
  ],
  [
    'borrowed Personal authority',
    (r: ModelCatalogRecord) => {
      r.sources[0].invocation_supported = true
    },
  ],
  [
    'bad capability',
    (r: ModelCatalogRecord) => {
      r.input_capabilities.openai_chat = ['audio' as 'image']
    },
  ],
  [
    'missing capabilities',
    (r: ModelCatalogRecord) => {
      Reflect.deleteProperty(r, 'input_capabilities')
    },
  ],
  [
    'missing timestamp',
    (r: ModelCatalogRecord) => {
      r.created_at = ''
    },
  ],
  [
    'disabled Model',
    (r: ModelCatalogRecord) => {
      r.status = 'disabled' as 'active'
    },
  ],
  [
    'missing source name',
    (r: ModelCatalogRecord) => {
      r.sources[0].team_name = ''
    },
  ],
])('fails closed for %s', async (_name, change) => {
  const record = fixture()
  change(record)
  data = record
  await expect(getModelCatalogRecord('mdl_one')).rejects.toThrow()
})
it('preserves future protocol metadata and legacy unsupported Team sources without inventing Chat support', async () => {
  const r = fixture()
  r.protocols = ['future_native', 'openai_chat', 'openai_chat']
  Reflect.deleteProperty(r.sources[0], 'invocation_protocols')
  data = r
  expect(await getModelCatalogRecord('mdl_one')).toEqual(r)
})
it('discards unexpected private properties instead of retaining them in catalogue caches', async () => {
  data = { ...fixture(), secret: 'not-a-catalogue-field' }
  expect(await getModelCatalogRecord('mdl_one')).not.toHaveProperty('secret')
})
it('rejects malformed and oversized complete lists without returning a partial catalogue', async () => {
  for (const items of [null, 'models', Array.from({ length: 1001 }, fixture)]) {
    data = { items }
    await expect(listModelCatalog()).rejects.toThrow()
  }
})

it('keeps Personal source identity distinct from an exact safe legacy Team ID', async () => {
  const r = fixture()
  r.sources[0].team_id = 'personal'
  r.sources.push({ type: 'personal', team_id: null, team_name: null, invocation_supported: true })
  data = r
  expect(await getModelCatalogRecord('mdl_one')).toEqual(r)
})

it.each(['unauthorized', 'unavailable', 'missing', 'heterogeneous'] as const)(
  'preserves the exact %s state without inventing a price or additional request',
  async (state) => {
    const row = fixture()
    row.input_price = row.output_price = { state, rate: null }
    data = { items: [row] }
    expect((await listModelCatalog())[0].input_price).toEqual({ state, rate: null })
    expect(requests.map((request) => request.url)).toEqual(['/model-catalog'])
  },
)
it.each(['priced', 'disabled'] as const)(
  'retains exact %s decimal strings including zero and maximum precision',
  async (state) => {
    const row = fixture()
    row.input_price = { state, rate: { amount: '0', unit: '1M_TOKEN', currency: 'USD' } }
    row.output_price = {
      state,
      rate: { amount: '999999999999999999.123456789012345678', unit: '1M_TOKEN', currency: 'CNY' },
    }
    data = row
    expect(await getModelCatalogRecord(row.id)).toEqual(row)
  },
)
it.each([
  ['absent', undefined],
  ['null', null],
  ['unknown state', { state: 'estimated', rate: null }],
  [
    'unauthorized with a rate',
    { state: 'unauthorized', rate: { amount: '1', unit: '1M_TOKEN', currency: 'USD' } },
  ],
  ['priced without a rate', { state: 'priced', rate: null }],
  ['disabled without a rate', { state: 'disabled', rate: null }],
  ['number amount', { state: 'priced', rate: { amount: 0.1, unit: '1M_TOKEN', currency: 'USD' } }],
  [
    'rounded canonical zero',
    { state: 'priced', rate: { amount: '0.00', unit: '1M_TOKEN', currency: 'USD' } },
  ],
  [
    'scientific amount',
    { state: 'priced', rate: { amount: '1e-8', unit: '1M_TOKEN', currency: 'USD' } },
  ],
  [
    'negative amount',
    { state: 'priced', rate: { amount: '-1', unit: '1M_TOKEN', currency: 'USD' } },
  ],
  ['leading zero', { state: 'priced', rate: { amount: '01', unit: '1M_TOKEN', currency: 'USD' } }],
  [
    'excess integer precision',
    { state: 'priced', rate: { amount: '1000000000000000000', unit: '1M_TOKEN', currency: 'USD' } },
  ],
  [
    'excess fraction precision',
    {
      state: 'priced',
      rate: { amount: '0.1234567890123456789', unit: '1M_TOKEN', currency: 'USD' },
    },
  ],
  ['wrong unit', { state: 'priced', rate: { amount: '1', unit: '1_TOKEN', currency: 'USD' } }],
  [
    'unknown currency',
    { state: 'priced', rate: { amount: '1', unit: '1M_TOKEN', currency: 'CAD' } },
  ],
  [
    'unrecorded supplier',
    {
      state: 'priced',
      rate: { amount: '1', unit: '1M_TOKEN', currency: 'USD', supplier: 'private' },
    },
  ],
  ['extra private cell field', { state: 'missing', rate: null, supplier: 'private' }],
])(
  'rejects %s price data in both list and detail before caching a partial record',
  async (_label, price) => {
    data = { ...fixture(), input_price: price }
    await expect(getModelCatalogRecord('mdl_one')).rejects.toThrow(
      'Invalid model catalogue response',
    )
    data = { items: [fixture(), { ...fixture(), id: 'mdl_other', output_price: price }] }
    await expect(listModelCatalog()).rejects.toThrow('Invalid model catalogue response')
  },
)
