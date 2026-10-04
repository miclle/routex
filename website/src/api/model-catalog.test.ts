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
