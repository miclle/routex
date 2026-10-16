import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from './client'
import {
  addRoutingBinding,
  getRoutingWeightReview,
  saveReviewedRoutingWeights,
  getRoutingCandidate,
  decodeRoutingSupply,
  listRoutingCandidates,
  listRoutingProviders,
} from './model-routing'
const original = client.defaults.adapter
let data: unknown
let status: number
let etag: string | undefined
let calls: InternalAxiosRequestConfig[]
const supply = {
  provider_name: 'Exact Provider',
  connection_name: 'Recorded Connection',
  verification_covered: true,
  configured_available: true,
}
const candidate = {
  id: 'pmd_one',
  provider_id: 'prv_one',
  connection_id: 'con_one',
  upstream_name: 'Literal%_Model',
  protocol: 'openai_chat',
  ...supply,
  selectable: true,
  review_etag: 'a'.repeat(64),
  prices: {
    input: [{ amount: '0.123456789123456789', currency: 'USD', enabled: true }],
    output: [],
  },
}
beforeEach(() => {
  calls = []
  status = 200
  etag = undefined
  data = { items: [structuredClone(candidate)], next_cursor: null }
  client.defaults.adapter = async (config) => {
    calls.push(config)
    return { config, status, statusText: '', headers: new AxiosHeaders(etag ? { etag } : {}), data }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
describe('protocol-scoped candidate wire', () => {
  it('reads exact target/protocol/literal filters and preserves decimal strings', async () => {
    const got = await listRoutingCandidates('mdl_one', 'openai_chat', 'prv_one', {
      q: '%_Exact',
      limit: 50,
      cursor: 'pmd_before',
    })
    expect(calls[0].url).toBe('/admin/models/mdl_one/routing-candidates')
    expect(calls[0].params).toEqual({
      protocol: 'openai_chat',
      provider_id: 'prv_one',
      q: '%_Exact',
      limit: 50,
      cursor: 'pmd_before',
    })
    expect(got.items[0].prices?.input[0].amount).toBe('0.123456789123456789')
  })
  it('preserves unavailable and denied price facts independently', async () => {
    data = {
      items: [
        {
          ...candidate,
          verification_covered: false,
          configured_available: true,
          selectable: false,
          prices: null,
        },
      ],
      next_cursor: null,
    }
    expect(
      (await listRoutingCandidates('mdl_one', 'openai_chat', 'prv_one')).items[0].prices,
    ).toBeNull()
  })
  it.each([
    'id',
    'provider_id',
    'connection_id',
    'upstream_name',
    'protocol',
    'provider_name',
    'connection_name',
    'verification_covered',
    'configured_available',
    'selectable',
    'review_etag',
    'prices',
  ])('rejects omitted %s', async (key) => {
    const row = { ...candidate } as Record<string, unknown>
    delete row[key]
    data = { items: [row], next_cursor: null }
    await expect(listRoutingCandidates('mdl_one', 'openai_chat', 'prv_one')).rejects.toThrow()
  })
  it.each([
    { id: 'PMD_one' },
    { id: 'pmd_one\n' },
    { provider_id: 'prv_other' },
    { connection_id: 'con_one ' },
    { protocol: 'openai_responses' },
    { verification_covered: 1 },
    { configured_available: 'true' },
    { selectable: true, verification_covered: false },
    { review_etag: '"' + 'a'.repeat(64) + '"' },
    { provider_name: 'Private\nline' },
    { connection_name: '' },
    { extra: 'secret' },
    { prices: { input: [{ amount: 1, currency: 'USD', enabled: true }], output: [] } },
    { prices: { input: [{ amount: '01', currency: 'USD', enabled: true }], output: [] } },
    { prices: { input: [], output: [], token: 'no' } },
    { prices: { input: [{ amount: '1', currency: 'usd', enabled: true }], output: [] } },
    { prices: { input: [{ amount: '1', currency: 'USD', enabled: 1 }], output: [] } },
    {
      prices: {
        input: [
          { amount: '1', currency: 'USD', enabled: true },
          { amount: '2', currency: 'USD', enabled: true },
        ],
        output: [],
      },
    },
  ])('rejects malformed/aliased/unauthorized enrichment %j', async (patch) => {
    data = { items: [{ ...candidate, ...patch }], next_cursor: null }
    await expect(listRoutingCandidates('mdl_one', 'openai_chat', 'prv_one')).rejects.toThrow()
  })
  it.each([
    { items: [candidate, candidate], next_cursor: null },
    { items: [candidate], next_cursor: 'pmd_other' },
    { items: Array.from({ length: 21 }, () => candidate), next_cursor: null },
    { items: [], next_cursor: null, extra: true },
  ])('rejects inconsistent bounded page %j', async (page) => {
    data = page
    await expect(listRoutingCandidates('mdl_one', 'openai_chat', 'prv_one')).rejects.toThrow()
  })
  it('accepts all four canonical protocol pages', async () => {
    for (const protocol of [
      'openai_chat',
      'openai_responses',
      'anthropic_messages',
      'gemini_generate_content',
    ] as const) {
      data = { items: [{ ...candidate, protocol }], next_cursor: null }
      expect((await listRoutingCandidates('mdl_one', protocol, 'prv_one')).items[0].protocol).toBe(
        protocol,
      )
    }
  })
  it('bounds providers and sends no mutation for reads', async () => {
    data = { items: [{ id: 'prv_one', name: 'Recorded supplier' }], next_cursor: null }
    await listRoutingProviders('mdl_one', 'openai_chat')
    expect(calls[0].method).toBe('get')
    expect(calls[0].headers.get('X-CSRF-Token')).toBeUndefined()
  })
  it.each([
    { q: 'x'.repeat(201) },
    { q: 'line\n' },
    { cursor: 'pmd_one ' },
    { limit: 0 },
    { limit: 51 },
    { limit: 1.5 },
  ])('rejects malformed query before HTTP %j', async (filter) => {
    await expect(
      listRoutingCandidates('mdl_one', 'openai_chat', 'prv_one', filter),
    ).rejects.toThrow()
    expect(calls).toHaveLength(0)
  })
  it('validates strict nested supply independently', () => {
    expect(decodeRoutingSupply(supply)).toEqual(supply)
    expect(() => decodeRoutingSupply({ ...supply, ready: true })).toThrow()
  })
  it('requires exact new zero-weight binding in 201 and sends captured review with CSRF', async () => {
    status = 201
    data = {
      id: 'mdl_one',
      name: 'Public',
      status: 'active',
      names: [],
      granted_user_ids: [],
      bindings: [
        {
          id: 'bnd_one',
          provider_model_id: 'pmd_one',
          provider_id: 'prv_one',
          connection_id: 'con_one',
          upstream_name: 'Exact',
          protocol: 'openai_chat',
          weight: 0,
          ready: true,
          supply,
        },
      ],
    }
    const intent = {
      provider_model_id: 'pmd_one',
      protocol: 'openai_chat' as const,
      review_etag: 'a'.repeat(64),
    }
    await addRoutingBinding('mdl_one', intent, 'fresh-csrf')
    expect(JSON.parse(calls[0].data)).toEqual(intent)
    expect(calls[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    status = 200
    await expect(addRoutingBinding('mdl_one', intent, 'fresh-csrf')).rejects.toThrow()
    status = 201
    ;(data as { bindings: { weight: number }[] }).bindings[0].weight = 100
    await expect(addRoutingBinding('mdl_one', intent, 'fresh-csrf')).rejects.toThrow()
  })
})

const exactReview = () => ({
  model_id: 'mdl_one',
  model_name: 'Public/model',
  candidate: structuredClone(candidate),
  routes: [
    {
      id: 'bnd_old',
      provider_id: 'prv_old',
      provider_name: 'Old supplier',
      connection_id: 'con_old',
      connection_name: 'Old connection',
      provider_model_id: 'pmd_old',
      upstream_name: 'old',
      protocol: 'openai_chat',
      weight: 100,
    },
  ],
})
describe('exact source routing review', () => {
  it('uses exact resource identity without name or pagination and preserves stored weights', async () => {
    data = exactReview()
    const signal = new AbortController().signal
    const got = await getRoutingCandidate('mdl_one', 'openai_chat', 'prv_one', 'pmd_one', signal)
    expect(calls[0].url).toBe('/admin/models/mdl_one/routing-candidates/pmd_one')
    expect(calls[0].params).toEqual({ protocol: 'openai_chat', provider_id: 'prv_one' })
    expect(calls[0].signal).toBe(signal)
    expect(got.routes[0].weight).toBe(100)
    expect(got.candidate.prices?.input[0].amount).toBe('0.123456789123456789')
  })
  it.each([
    { model_id: 'mdl_other' },
    { model_name: '' },
    { routes: null },
    { extra: true },
    { candidate: { ...candidate, id: 'pmd_other' } },
    { candidate: { ...candidate, protocol: 'openai_responses' } },
    { routes: Array.from({ length: 201 }, () => exactReview().routes[0]) },
    { routes: [{ ...exactReview().routes[0], weight: 101 }] },
    { routes: [{ ...exactReview().routes[0], provider_id: 'prv_one' }] },
    { routes: [{ ...exactReview().routes[0], provider_model_id: 'pmd_one' }] },
    { routes: [{ ...exactReview().routes[0], connection_name: 'private\nvalue' }] },
    { routes: [exactReview().routes[0], exactReview().routes[0]] },
  ])('rejects mismatched/unbounded/private review %j', async (patch) => {
    data = { ...exactReview(), ...patch }
    await expect(
      getRoutingCandidate('mdl_one', 'openai_chat', 'prv_one', 'pmd_one'),
    ).rejects.toThrow()
  })
})

function weightReview() {
  return {
    review_etag: 'b'.repeat(64),
    runtime_applied: false,
    model: {
      id: 'mdl_one',
      name: 'Recorded',
      status: 'active',
      names: [],
      granted_user_ids: [],
      bindings: [
        {
          id: 'bnd_one',
          provider_model_id: 'pmd_one',
          provider_id: 'prv_one',
          connection_id: 'con_one',
          upstream_name: 'Recorded upstream',
          protocol: 'openai_chat',
          weight: 100,
          ready: true,
        },
      ],
    },
  }
}
describe('reviewed complete weights', () => {
  it('uses separate read and strong reviewed write, with exact full-set response checking', async () => {
    data = weightReview()
    etag = `"${'b'.repeat(64)}"`
    expect((await getRoutingWeightReview('mdl_one')).runtime_applied).toBe(false)
    const weights = [{ binding_id: 'bnd_one', weight: 100 }]
    await saveReviewedRoutingWeights('mdl_one', weights, 'a'.repeat(64), 'csrf')
    expect(calls[1].url).toBe('/admin/models/mdl_one/weights/reviewed')
    expect(calls[1].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
    expect(calls[1].headers.get('X-CSRF-Token')).toBe('csrf')
    expect(JSON.parse(calls[1].data)).toEqual({ weights })
    await expect(
      saveReviewedRoutingWeights(
        'mdl_one',
        [{ binding_id: 'bnd_one', weight: 0 }],
        'a'.repeat(64),
        'csrf',
      ),
    ).rejects.toThrow()
  })
  it.each([
    'missing-header',
    'weak-header',
    'wrong-model',
    'foreign-binding',
    'unbounded',
    'unknown-runtime',
    'extra-field',
  ])('rejects %s in current-state weight proof', async (kind) => {
    const value = weightReview()
    data = value
    etag = `"${'b'.repeat(64)}"`
    if (kind === 'missing-header') etag = undefined
    else if (kind === 'weak-header') etag = `W/${etag}`
    else if (kind === 'wrong-model') value.model.id = 'mdl_other'
    else if (kind === 'foreign-binding') value.model.bindings[0].connection_id = 'private'
    else if (kind === 'unbounded')
      value.model.bindings = Array.from({ length: 201 }, () => value.model.bindings[0])
    else if (kind === 'unknown-runtime') data = { ...value, runtime_applied: null }
    else data = { ...value, secret: 'private' }
    await expect(getRoutingWeightReview('mdl_one')).rejects.toThrow()
  })
})
