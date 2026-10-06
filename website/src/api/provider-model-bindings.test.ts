import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, expect, it } from 'vitest'
import client from './client'
import { getProviderModelBindings, validateProviderModelBindings } from './provider-model-bindings'

const original = client.defaults.adapter
const page = () => ({
  provider_id: 'prv_exact',
  items: [
    {
      provider_model_id: 'pmd_a',
      connection_id: 'con_exact',
      binding_count: 2,
      models: [
        { id: 'mdl_a', name: 'Exact:Current/name' },
        { id: 'mdl_b', name: null },
      ],
    },
    { provider_model_id: 'pmd_b', connection_id: 'con_exact', binding_count: 0, models: [] },
  ],
})
afterEach(() => {
  client.defaults.adapter = original
})
it('retains complete zero, many and nullable current labels without amount/readiness inference', () => {
  expect(validateProviderModelBindings(page(), 'prv_exact')).toEqual(page())
  expect(
    validateProviderModelBindings({ provider_id: 'prv_exact', items: [] }, 'prv_exact').items,
  ).toEqual([])
})
const invalidCases: [string, (value: ReturnType<typeof page>) => unknown][] = [
  ['wrong Provider', (v) => ({ ...v, provider_id: 'prv_other' })],
  ['case Provider alias', (v) => ({ ...v, provider_id: 'PRV_exact' })],
  ['extra envelope', (v) => ({ ...v, etag: 'private' })],
  ['missing envelope', (v) => ({ items: v.items })],
  ['null items', (v) => ({ ...v, items: null })],
  ['non-array items', (v) => ({ ...v, items: {} })],
  ['duplicate PM', (v) => ({ ...v, items: [v.items[0], v.items[0]] })],
  ['unsorted PM', (v) => ({ ...v, items: [...v.items].reverse() })],
  ['unsafe PM', (v) => ({ ...v, items: [{ ...v.items[0], provider_model_id: 'pmd_a ' }] })],
  ['wrong PM prefix', (v) => ({ ...v, items: [{ ...v.items[0], provider_model_id: 'mdl_a' }] })],
  [
    'wrong Connection prefix',
    (v) => ({ ...v, items: [{ ...v.items[0], connection_id: 'pmd_a' }] }),
  ],
  ['extra row authority', (v) => ({ ...v, items: [{ ...v.items[0], ready: true }] })],
  ['string count', (v) => ({ ...v, items: [{ ...v.items[0], binding_count: '2' }] })],
  ['negative count', (v) => ({ ...v, items: [{ ...v.items[0], binding_count: -1 }] })],
  ['fractional count', (v) => ({ ...v, items: [{ ...v.items[0], binding_count: 1.5 }] })],
  ['unknown count', (v) => ({ ...v, items: [{ ...v.items[0], binding_count: null }] })],
  ['count mismatch', (v) => ({ ...v, items: [{ ...v.items[0], binding_count: 1 }] })],
  [
    'unsafe count',
    (v) => ({ ...v, items: [{ ...v.items[0], binding_count: Number.MAX_SAFE_INTEGER + 1 }] }),
  ],
  ['null models', (v) => ({ ...v, items: [{ ...v.items[0], models: null }] })],
  [
    'duplicate Model',
    (v) => ({
      ...v,
      items: [{ ...v.items[0], models: [v.items[0].models[0], v.items[0].models[0]] }],
    }),
  ],
  [
    'unsorted Model',
    (v) => ({ ...v, items: [{ ...v.items[0], models: [...v.items[0].models].reverse() }] }),
  ],
  [
    'wrong Model prefix',
    (v) => ({
      ...v,
      items: [
        {
          ...v.items[0],
          models: [
            { id: 'pmd_a', name: null },
            { id: 'mdl_b', name: null },
          ],
        },
      ],
    }),
  ],
  [
    'private Model metadata',
    (v) => ({
      ...v,
      items: [
        {
          ...v.items[0],
          models: [{ ...v.items[0].models[0], birth: 'private' }, v.items[0].models[1]],
        },
      ],
    }),
  ],
  [
    'empty current name',
    (v) => ({
      ...v,
      items: [
        {
          ...v.items[0],
          models: [
            { id: 'mdl_a', name: '' },
            { id: 'mdl_b', name: null },
          ],
        },
      ],
    }),
  ],
  [
    'array-coerced name',
    (v) => ({
      ...v,
      items: [
        {
          ...v.items[0],
          models: [
            { id: 'mdl_a', name: ['name'] },
            { id: 'mdl_b', name: null },
          ],
        },
      ],
    }),
  ],
  [
    'control current name',
    (v) => ({
      ...v,
      items: [
        {
          ...v.items[0],
          models: [
            { id: 'mdl_a', name: 'name\n' },
            { id: 'mdl_b', name: null },
          ],
        },
      ],
    }),
  ],
  [
    'boundary current name',
    (v) => ({
      ...v,
      items: [
        {
          ...v.items[0],
          models: [
            { id: 'mdl_a', name: ' name' },
            { id: 'mdl_b', name: null },
          ],
        },
      ],
    }),
  ],
  [
    'long current name',
    (v) => ({
      ...v,
      items: [
        {
          ...v.items[0],
          models: [
            { id: 'mdl_a', name: 'a'.repeat(129) },
            { id: 'mdl_b', name: null },
          ],
        },
      ],
    }),
  ],
]
it.each(invalidCases)(
  'rejects malformed/incomplete %s instead of returning partial binding facts',
  (_, corrupt) => {
    expect(() => validateProviderModelBindings(corrupt(page()), 'prv_exact')).toThrow()
  },
)
it('retains safe historical identifiers without a generated-ULID-only restriction', () => {
  const value = {
    provider_id: 'prv_',
    items: [
      {
        provider_model_id: 'pmd_',
        connection_id: 'con_',
        binding_count: 1,
        models: [{ id: 'mdl_', name: null }],
      },
    ],
  }
  expect(validateProviderModelBindings(value, 'prv_')).toEqual(value)
})
it('allows one Model to be stored against multiple PMs without deduplicating relationships', () => {
  const value = page()
  value.items[1] = {
    ...value.items[1],
    binding_count: 1,
    models: [{ id: 'mdl_a', name: 'Exact:Current/name' }],
  }
  expect(validateProviderModelBindings(value, 'prv_exact')).toEqual(value)
})
it('accepts exact catalogue/binding limits and rejects either sentinel and aggregate overflow', () => {
  const models = Array.from({ length: 10000 }, (_, n) => ({
    id: `mdl_${String(n).padStart(5, '0')}`,
    name: null,
  }))
  const value = {
    provider_id: 'prv_exact',
    items: [
      { provider_model_id: 'pmd_a', connection_id: 'con_exact', binding_count: 10000, models },
    ],
  }
  expect(validateProviderModelBindings(value, 'prv_exact').items[0].models).toHaveLength(10000)
  expect(() =>
    validateProviderModelBindings(
      {
        ...value,
        items: [
          ...value.items,
          {
            provider_model_id: 'pmd_b',
            connection_id: 'con_exact',
            binding_count: 1,
            models: [{ id: 'mdl_a', name: null }],
          },
        ],
      },
      'prv_exact',
    ),
  ).toThrow()
  const items = Array.from({ length: 10000 }, (_, n) => ({
    provider_model_id: `pmd_${String(n).padStart(5, '0')}`,
    connection_id: 'con_exact',
    binding_count: 0,
    models: [],
  }))
  expect(
    validateProviderModelBindings({ provider_id: 'prv_exact', items }, 'prv_exact').items,
  ).toHaveLength(10000)
  expect(() =>
    validateProviderModelBindings(
      {
        provider_id: 'prv_exact',
        items: [
          ...items,
          { provider_model_id: 'pmd_z', connection_id: 'con_exact', binding_count: 0, models: [] },
        ],
      },
      'prv_exact',
    ),
  ).toThrow()
})
it('uses only the exact scoped no-query GET and forwards the abort signal', async () => {
  const seen: InternalAxiosRequestConfig[] = []
  client.defaults.adapter = async (config) => {
    seen.push(config)
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders({ 'Cache-Control': 'no-store' }),
      data: page(),
    }
  }
  const controller = new AbortController()
  expect(await getProviderModelBindings('prv_exact', controller.signal)).toEqual(page())
  expect(seen).toHaveLength(1)
  expect(seen[0].url).toBe('/admin/providers/prv_exact/model-bindings')
  expect(seen[0].signal).toBe(controller.signal)
  expect(seen[0].params).toBeUndefined()
})
it.each([undefined, 'public,max-age=60', 'no-cache', ['no-store']])(
  'rejects absent/invalid privacy metadata %s',
  async (control) => {
    client.defaults.adapter = async (config) => ({
      config,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders(control === undefined ? {} : { 'Cache-Control': control }),
      data: page(),
    })
    await expect(getProviderModelBindings('prv_exact')).rejects.toThrow()
  },
)
it.each(['prv_bad ', 'PRV_exact', 'con_exact', 'prv_' + 'x'.repeat(27)])(
  'rejects unsafe target %s before any request',
  async (target) => {
    let calls = 0
    client.defaults.adapter = async () => {
      calls++
      throw new Error('must not fetch')
    }
    await expect(getProviderModelBindings(target)).rejects.toThrow()
    expect(calls).toBe(0)
  },
)
