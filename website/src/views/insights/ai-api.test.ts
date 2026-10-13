import { afterEach, expect, it } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from '@/api/client'
import { analyzeInsights, getInsightsModels } from '@/api/insights'
import type { InsightsAnalysis, InsightsAnalysisInput } from '@/types/insights'

type MutableAnalysisFixture = {
  [K in keyof Omit<InsightsAnalysis, 'capture' | 'labels'>]: unknown
} & {
  capture: { [K in keyof InsightsAnalysis['capture']]: unknown }
  labels: {
    label: unknown
    model_id?: unknown
    provider_id?: unknown
    name: unknown
    unknown: unknown
  }[]
  secret?: unknown
}

const original = client.defaults.adapter
const model = 'mdl_01arz3ndektsv4rrffq69g5fav'
const input: InsightsAnalysisInput = {
  report: 'tokens_by_model',
  model_id: model,
  from: '2026-09-01T00:00:00Z',
  to: '2026-09-02T00:00:00Z',
  timezone: 'UTC',
  language: 'en',
}
function result(): InsightsAnalysis {
  return {
    request_id: 'req_01arz3ndektsv4rrffq69g5fav',
    completion: 'completed',
    text: 'M1 has known usage.',
    capture: {
      source: 'persisted_call_records',
      from: input.from,
      to: input.to,
      timezone: input.timezone,
      queried_at: '2026-09-02T01:00:00Z',
      latest_completed_at: null,
      may_lag: true,
    },
    labels: [{ label: 'M1', model_id: 'historical-model', name: '<img src=x>', unknown: false }],
  }
}
function respond(data: unknown) {
  client.defaults.adapter = async (config) => ({
    config,
    data,
    status: 200,
    statusText: '',
    headers: new AxiosHeaders(),
  })
}
afterEach(() => {
  client.defaults.adapter = original
})
it('reads only a bounded unique exact personal-model DTO', async () => {
  respond({ items: [{ model_id: model, name: 'Callable Chat' }] })
  expect(await getInsightsModels(new AbortController().signal)).toEqual([
    { model_id: model, name: 'Callable Chat' },
  ])
})

function providerResult(): InsightsAnalysis {
  const value = result()
  value.capture.source = 'persisted_call_attempts'
  value.labels = [
    { label: 'P1', provider_id: 'prv_historical', name: 'Historical Provider', unknown: false },
  ]
  value.text = 'P1 recorded quality is advisory.'
  return value
}

it('binds Provider advice to attempt capture and a separate private P identity map', async () => {
  respond(providerResult())
  const request = { ...input, report: 'provider_quality' as const, question: '  Explain P1  ' }
  expect(await analyzeInsights(request, 'csrf', new AbortController().signal)).toEqual(
    providerResult(),
  )
})

it.each([
  (v: MutableAnalysisFixture) => {
    v.capture.source = 'persisted_call_records'
  },
  (v: MutableAnalysisFixture) => {
    v.labels[0].label = 'M1'
  },
  (v: MutableAnalysisFixture) => {
    v.labels[0].model_id = 'historical_model'
  },
  (v: MutableAnalysisFixture) => {
    v.labels[0].provider_id = ''
  },
  (v: MutableAnalysisFixture) => {
    v.labels[0].provider_id = 'x'.repeat(129)
  },
  (v: MutableAnalysisFixture) => {
    v.labels[0].provider_id = '\ud800'
  },
  (v: MutableAnalysisFixture) => {
    v.labels[0].unknown = true
  },
  (v: MutableAnalysisFixture) => {
    v.labels[0].label = 'P501'
  },
  (v: MutableAnalysisFixture) => {
    v.labels.push({ ...v.labels[0], label: 'P2' })
  },
])('rejects cross-report and malformed Provider advice before publication', async (change) => {
  const value: MutableAnalysisFixture = providerResult()
  change(value)
  respond(value)
  await expect(
    analyzeInsights({ ...input, report: 'provider_quality' }, 'csrf', new AbortController().signal),
  ).rejects.toThrow()
})

it('rejects Provider sources and identity maps for existing Tokens advice', async () => {
  respond(providerResult())
  await expect(analyzeInsights(input, 'csrf', new AbortController().signal)).rejects.toThrow()
})
it.each([
  { items: [{ model_id: model, name: 'A', secret: 'x' }] },
  { items: [{ model_id: 'MDL_alias', name: 'A' }] },
  {
    items: [
      { model_id: model, name: 'A' },
      { model_id: model, name: 'B' },
    ],
  },
  { items: [], next_cursor: 'unbounded' },
  { items: [{ model_id: model, name: '界'.repeat(342) }] },
])('rejects malformed or broadened model lists', async (data) => {
  respond(data)
  await expect(getInsightsModels(new AbortController().signal)).rejects.toThrow()
})
it('sends only the fixed request with fresh CSRF and validates separate capture metadata', async () => {
  let sent: unknown
  client.defaults.adapter = async (config) => {
    sent = JSON.parse(config.data)
    expect(config.headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    return { config, data: result(), status: 200, statusText: '', headers: new AxiosHeaders() }
  }
  expect(await analyzeInsights(input, 'fresh-csrf', new AbortController().signal)).toEqual(result())
  expect(sent).toEqual(input)
})
it.each(['unknown', 'handoff', 'blocked', 'incomplete'] as const)(
  'accepts native %s only without text',
  async (completion) => {
    respond({ ...result(), completion, text: null })
    expect((await analyzeInsights(input, 'csrf', new AbortController().signal)).completion).toBe(
      completion,
    )
    respond({ ...result(), completion, text: 'fabricated' })
    await expect(analyzeInsights(input, 'csrf', new AbortController().signal)).rejects.toThrow()
  },
)
it.each([
  (value: MutableAnalysisFixture) => {
    value.completion = 'success'
  },
  (value: MutableAnalysisFixture) => {
    value.text = null
  },
  (value: MutableAnalysisFixture) => {
    value.text = ''
  },
  (value: MutableAnalysisFixture) => {
    value.text = ' \t\r\n'
  },
  (value: MutableAnalysisFixture) => {
    value.text = '界'.repeat(5462)
  },
  (value: MutableAnalysisFixture) => {
    value.request_id = 'req_alias'
  },
  (value: MutableAnalysisFixture) => {
    value.capture.from = '2026-09-03T00:00:00Z'
  },
  (value: MutableAnalysisFixture) => {
    value.capture.timezone = 'Asia/Shanghai'
  },
  (value: MutableAnalysisFixture) => {
    value.capture.source = 'live_sql'
  },
  (value: MutableAnalysisFixture) => {
    value.capture.may_lag = 'true'
  },
  (value: MutableAnalysisFixture) => {
    value.capture.queried_at = 'invalid'
  },
  (value: MutableAnalysisFixture) => {
    value.secret = 'secret'
  },
  (value: MutableAnalysisFixture) => {
    value.labels[0].label = 'M501'
  },
  (value: MutableAnalysisFixture) => {
    value.labels.push({ ...value.labels[0] })
  },
  (value: MutableAnalysisFixture) => {
    value.labels[0].name = '界'.repeat(342)
  },
  (value: MutableAnalysisFixture) => {
    value.labels[0].model_id = 'x'.repeat(129)
  },
  (value: MutableAnalysisFixture) => {
    value.labels[0].unknown = null
  },
])('fails closed on invalid answer/native metadata', async (change) => {
  const value: MutableAnalysisFixture = result()
  change(value)
  respond(value)
  await expect(analyzeInsights(input, 'csrf', new AbortController().signal)).rejects.toThrow()
})
it('rejects extra prompt/history/scope inputs before dispatch', async () => {
  let calls = 0
  client.defaults.adapter = async () => {
    calls++
    throw Error('must not dispatch')
  }
  await expect(
    analyzeInsights(
      { ...input, prompt: 'secret question' } as InsightsAnalysisInput,
      'csrf',
      new AbortController().signal,
    ),
  ).rejects.toThrow()
  expect(calls).toBe(0)
})

it('rejects whole-list and whole-label overflow without sampling', async () => {
  respond({ items: Array.from({ length: 501 }, () => ({ model_id: model, name: 'A' })) })
  await expect(getInsightsModels(new AbortController().signal)).rejects.toThrow()
  respond({
    ...result(),
    labels: Array.from({ length: 501 }, (_, index) => ({
      label: 'M' + (index + 1),
      model_id: 'history' + index,
      name: 'Name',
      unknown: false,
    })),
  })
  await expect(analyzeInsights(input, 'csrf', new AbortController().signal)).rejects.toThrow()
})
it('rejects non-200 success statuses rather than treating HTTP success as native proof', async () => {
  client.defaults.adapter = async (config) => ({
    config,
    data: result(),
    status: 202,
    statusText: '',
    headers: new AxiosHeaders(),
  })
  await expect(analyzeInsights(input, 'csrf', new AbortController().signal)).rejects.toThrow()
})

it.each(['  exact question\nwith\ttabs  ', '界'.repeat(341) + 'a', '😀'.repeat(256)])(
  'preserves the exact approved optional question bytes in the fixed request',
  async (question) => {
    let sent: unknown
    client.defaults.adapter = async (config) => {
      sent = JSON.parse(config.data)
      return { config, data: result(), status: 200, statusText: '', headers: new AxiosHeaders() }
    }
    await analyzeInsights({ ...input, question }, 'csrf', new AbortController().signal)
    expect(sent).toEqual({ ...input, question })
  },
)

it.each([
  undefined,
  null,
  '',
  ' \t\n ',
  '界'.repeat(342),
  'a'.repeat(1025),
  'a\rb',
  'a\u0000b',
  'a\u007fb',
  'a\u0085b',
  'a\ud800b',
  '"'.repeat(1024),
])('rejects present invalid or oversized question without dispatch', async (question) => {
  let calls = 0
  client.defaults.adapter = async () => {
    calls++
    throw Error('must not dispatch')
  }
  await expect(
    analyzeInsights(
      { ...input, question } as InsightsAnalysisInput,
      'csrf',
      new AbortController().signal,
    ),
  ).rejects.toThrow()
  expect(calls).toBe(0)
})
