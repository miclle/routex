import { afterEach, expect, it, vi } from 'vitest'
import client from './client'
import {
  getMemberModels,
  setMemberModels,
  validateMemberModels,
  validateMemberModelsWrite,
  validMemberModelReason,
} from './member-models'
import { modelsFixture, modelsTarget, secondModel } from '@/views/governance/member-models.fixture'
const original = client.defaults.adapter
afterEach(() => {
  client.defaults.adapter = original
  vi.restoreAllMocks()
})
it('retains exact zero, 18-place values, null metadata and four native protocols', () => {
  const x = modelsFixture()
  x.personal_models[0].protocols = [
    'openai_chat',
    'openai_responses',
    'anthropic_messages',
    'gemini_generate_content',
  ]
  expect(validateMemberModels(x, modelsTarget)).toEqual(x)
  expect(x.personal_models[0].output_price.rate!.amount).toBe('9007199254740993.000000000000000001')
})
it.each(['unauthorized', 'unavailable', 'missing', 'heterogeneous'])(
  'accepts %s price only without an amount',
  (state) => {
    const x = modelsFixture()
    x.personal_models[0].input_price = {
      state,
      rate: null,
    } as (typeof x.personal_models)[0]['input_price']
    expect(() => validateMemberModels(x, modelsTarget)).not.toThrow()
    Object.assign(x.personal_models[0].input_price, {
      rate: { amount: '0', unit: '1M_TOKEN', currency: 'USD' },
    })
    expect(() => validateMemberModels(x, modelsTarget)).toThrow()
  },
)
it.each([
  [
    'wrong target',
    (x: ReturnType<typeof modelsFixture>) => {
      x.user_id = 'other'
    },
  ],
  [
    'forged type',
    (x) => {
      Object.assign(x.personal_models[0], { type: 'chat' })
    },
  ],
  [
    'forged update',
    (x) => {
      Object.assign(x.personal_models[0], { updated_at: x.observed_at })
    },
  ],
  [
    'duplicate table identity',
    (x) => {
      x.available_models[0].id = x.personal_models[0].id
    },
  ],
  [
    'invalid native protocol',
    (x) => {
      Object.assign(x.personal_models[0], { protocols: ['chat'] })
    },
  ],
  [
    'unknown runtime as zero',
    (x) => {
      Object.assign(x, { application_status: 'unavailable', runtime_applied: false })
    },
  ],
  [
    'ready without protocol',
    (x) => {
      x.personal_models[0].protocols = []
    },
  ],
  [
    'inactive selectable',
    (x) => {
      x.available_models[0].status = 'disabled'
    },
  ],
  [
    'malformed currency',
    (x) => {
      Object.assign(x.personal_models[0].input_price.rate!, { currency: 'CAD' })
    },
  ],
  [
    'rounded amount',
    (x) => {
      Object.assign(x.personal_models[0].input_price.rate!, { amount: 0 })
    },
  ],
  [
    'noncanonical decimal',
    (x) => {
      x.personal_models[0].input_price.rate!.amount = '0.00'
    },
  ],
  [
    'readonly candidates',
    (x) => {
      x.can_edit = false
    },
  ],
] as [string, (x: ReturnType<typeof modelsFixture>) => void][])('rejects %s', (_name, mutate) => {
  const x = modelsFixture()
  mutate(x)
  expect(() => validateMemberModels(x, modelsTarget)).toThrow()
})
it('rejects an oversized complete base rather than truncating', () => {
  const x = modelsFixture()
  x.available_models = Array.from({ length: 1000 }, (_, i) => ({
    ...x.available_models[0],
    id: `mdl_${String(i).padStart(4, '0')}`,
  }))
  expect(() => validateMemberModels(x, modelsTarget)).toThrow()
})
it('unavailable runtime is nullable and readonly keeps retained rows', () => {
  const x = modelsFixture()
  Object.assign(x, {
    runtime_applied: null,
    application_status: 'unavailable',
    can_edit: false,
    available_models: [],
  })
  expect(validateMemberModels(x, modelsTarget).runtime_applied).toBeNull()
})
it('only confirms exact current set with runtime true, never a historical receipt', () => {
  const x = {
    user_id: modelsTarget,
    model_ids: [secondModel],
    etag: 'b'.repeat(64),
    runtime_applied: true,
    confirmation: 'current_model_grants',
  }
  expect(validateMemberModelsWrite(x, modelsTarget, [secondModel])).toEqual(x)
  for (const bad of [
    { ...x, runtime_applied: false },
    { ...x, confirmation: 'committed' },
    { ...x, model_ids: [] },
    { ...x, user_id: 'other' },
  ])
    expect(() => validateMemberModelsWrite(bad, modelsTarget, [secondModel])).toThrow()
})
it('reason validates bytes, controls, surrogate and trim-exact intent', () => {
  expect(validMemberModelReason('理由')).toBe(true)
  for (const s of ['', ' reason ', 'a\n', '\ud800', '界'.repeat(342)])
    expect(validMemberModelReason(s)).toBe(false)
})
it('uses only exact target, strong matching ETags, CSRF and cancellation signal', async () => {
  const calls: unknown[] = []
  const x = modelsFixture(),
    stop = new AbortController()
  client.defaults.adapter = async (config) => {
    calls.push(config)
    return {
      config,
      data:
        config.method === 'get'
          ? x
          : {
              user_id: modelsTarget,
              model_ids: [],
              etag: 'b'.repeat(64),
              runtime_applied: true,
              confirmation: 'current_model_grants',
            },
      status: 200,
      statusText: '',
      headers: { etag: `"${config.method === 'get' ? x.etag : 'b'.repeat(64)}"` },
    }
  }
  await getMemberModels(modelsTarget, stop.signal)
  await setMemberModels(
    modelsTarget,
    x.etag,
    { model_ids: [], reason: 'Remove all' },
    'current-csrf',
    stop.signal,
  )
  expect(calls).toHaveLength(2)
  expect(calls[1]).toMatchObject({
    url: `/admin/members/${modelsTarget}/models`,
    method: 'put',
    signal: stop.signal,
  })
  expect((calls[1] as { headers: Record<string, string> }).headers['If-Match']).toBe(`"${x.etag}"`)
  client.defaults.adapter = async (config) => ({
    config,
    data: x,
    status: 200,
    statusText: '',
    headers: { etag: `W/"${x.etag}"` },
  })
  await expect(getMemberModels(modelsTarget)).rejects.toThrow()
})

it('preserves 128-byte public names and Unicode Provider labels without truncation', () => {
  const x = modelsFixture()
  x.personal_models[0].name = 'a'.repeat(128)
  x.personal_models[0].providers = ['😀'.repeat(100)]
  expect(validateMemberModels(x, modelsTarget)).toEqual(x)
  for (const name of ['a'.repeat(129), '\ud800', ' name ', 'name\n']) {
    x.personal_models[0].name = name
    expect(() => validateMemberModels(x, modelsTarget)).toThrow()
  }
})

it('Provider labels use the server code-point boundary and keep exact content', () => {
  const x = modelsFixture()
  for (const name of ['😀'.repeat(101), '\ud800', ' name ', 'name\n']) {
    x.personal_models[0].providers = [name]
    expect(() => validateMemberModels(x, modelsTarget)).toThrow()
  }
})
