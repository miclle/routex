import { afterEach, expect, it } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from '@/api/client'
import { analyzeKeyQuotaInsights } from '@/api/insights'
import type { InsightsKeyQuotaAnalysis, InsightsKeyQuotaInput } from '@/types/insights'
import { quotaFixture, keyQuotaInput, quotaActor, quotaProject } from './quota-fixture'
const original = client.defaults.adapter
const keyAIModel = 'mdl_01arz3ndektsv4rrffq69g5fav'
const input: InsightsKeyQuotaInput = {
  report: 'key_quota_tokens',
  model_id: keyAIModel,
  language: 'en',
  scope_kind: 'member',
  scope_id: quotaActor,
  cursor: 'cmV2aWV3ZWQ',
  limit: 20,
}
function result(): InsightsKeyQuotaAnalysis {
  const report = quotaFixture(keyQuotaInput)
  return {
    request_id: 'req_01arz3ndektsv4rrffq69g5fav',
    completion: 'completed',
    text: 'K1 retains unknown coverage.',
    key_quota_capture: {
      source: 'local_quota_journal',
      scope_kind: input.scope_kind,
      scope_id: input.scope_id,
      queried_at: '2026-10-14T12:00:00Z',
      cursor: input.cursor,
      limit: 20,
      has_more: true,
      timezone: report.time_zone,
      as_of: report.as_of,
      coverage_start: report.coverage_start,
      baseline_from: report.baseline_from,
      baseline_to: report.baseline_to,
      month_from: report.month_from,
      month_reset: report.month_reset,
      baseline_basis: report.baseline_basis,
    },
    labels: [
      {
        label: 'K1',
        key_id: report.items[0].id,
        name: '<script>private Key</script>',
        unknown: false,
      },
    ],
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
it('sends only the exact reviewed page and question with current CSRF, no range or statistics', async () => {
  const question = '  Explain K1 without changing policy  '
  client.defaults.adapter = async (config) => {
    expect(config.url).toBe('/admin/insights/analyze')
    expect(JSON.parse(config.data)).toEqual({ ...input, question })
    expect(config.headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    return { config, data: result(), status: 200, statusText: '', headers: new AxiosHeaders() }
  }
  expect(
    await analyzeKeyQuotaInsights(
      { ...input, question },
      'fresh-csrf',
      new AbortController().signal,
    ),
  ).toEqual(result())
})
it.each(['unknown', 'completed', 'handoff', 'blocked', 'incomplete'] as const)(
  'preserves native %s without inventing a completed answer',
  async (completion) => {
    const value = result()
    value.completion = completion
    value.text = completion === 'completed' ? 'K1 current-page advice' : null
    respond(value)
    expect(
      (await analyzeKeyQuotaInsights(input, 'csrf', new AbortController().signal)).completion,
    ).toBe(completion)
  },
)
it.each([
  (r: Record<string, unknown>) => {
    r.capture = { source: 'persisted_call_records' }
  },
  (r: Record<string, unknown>) => {
    r.secret = 'private'
  },
  (r: Record<string, unknown>) => {
    ;(r.key_quota_capture as Record<string, unknown>).cursor = 'other'
  },
  (r: Record<string, unknown>) => {
    ;(r.key_quota_capture as Record<string, unknown>).limit = 1
  },
  (r: Record<string, unknown>) => {
    ;(r.key_quota_capture as Record<string, unknown>).source = 'browser'
  },
  (r: Record<string, unknown>) => {
    ;(r.key_quota_capture as Record<string, unknown>).as_of = '2026-10-14'
  },
  (r: Record<string, unknown>) => {
    ;(r.key_quota_capture as Record<string, unknown>).timezone = 'Local'
  },
  (r: Record<string, unknown>) => {
    ;(r.key_quota_capture as Record<string, unknown>).has_more = 'false'
  },
  (r: Record<string, unknown>) => {
    ;(r.labels as Record<string, unknown>[])[0].key_id = 'KEY_01arz3ndektsv4rrffq69g5fav'
  },
  (r: Record<string, unknown>) => {
    ;(r.labels as Record<string, unknown>[])[0].provider_id = 'prv_private'
  },
  (r: Record<string, unknown>) => {
    ;(r.labels as Record<string, unknown>[])[0].label = 'M1'
  },
  (r: Record<string, unknown>) => {
    ;(r.labels as Record<string, unknown>[]).push({
      ...(r.labels as Record<string, unknown>[])[0],
      label: 'K2',
    })
  },
  (r: Record<string, unknown>) => {
    r.completion = 'handoff'
  },
  (r: Record<string, unknown>) => {
    r.text = 'x'.repeat(16 * 1024 + 1)
  },
])('rejects malformed, cross-kind or mismatched current-page results', async (mutate) => {
  const value = structuredClone(result()) as unknown as Record<string, unknown>
  mutate(value)
  respond(value)
  await expect(
    analyzeKeyQuotaInsights(input, 'csrf', new AbortController().signal),
  ).rejects.toThrow()
})
it('rejects a mixed nanosecond observation frame rather than displaying incoherent quota facts', async () => {
  const value = result()
  value.key_quota_capture.baseline_to = '2026-10-12T12:00:00.123456788Z'
  respond(value)
  await expect(
    analyzeKeyQuotaInsights(input, 'csrf', new AbortController().signal),
  ).rejects.toThrow()
})
it('preserves unavailable calendar facts and an empty authorized page without fake zero facts', async () => {
  const value = result()
  value.labels = []
  value.key_quota_capture.has_more = false
  for (const field of [
    'as_of',
    'coverage_start',
    'baseline_from',
    'baseline_to',
    'month_from',
    'month_reset',
  ] as const)
    value.key_quota_capture[field] = null
  respond(value)
  expect(await analyzeKeyQuotaInsights(input, 'csrf', new AbortController().signal)).toEqual(value)
})
it.each([
  { ...input, limit: 0 },
  { ...input, limit: 51 },
  { ...input, cursor: 'private cursor' },
  { ...input, from: '2026-10-01T00:00:00Z' },
  { ...input, statistics: {} },
  { ...input, language: 'fr' },
  { ...input, scope_kind: 'team' },
  { ...input, scope_id: quotaProject },
  { ...input, scope_id: quotaActor.toUpperCase() },
  { ...input, key_id: 'key_01arz3ndektsv4rrffq69g5fav' },
])('rejects alternate input before HTTP', async (bad) => {
  let dispatched = false
  client.defaults.adapter = async () => {
    dispatched = true
    throw Error('unexpected')
  }
  await expect(
    analyzeKeyQuotaInsights(bad as InsightsKeyQuotaInput, 'csrf', new AbortController().signal),
  ).rejects.toThrow()
  expect(dispatched).toBe(false)
})

it('accepts Project roots only with the exact reviewed Project scope and canonical pky IDs', async () => {
  const projectInput = { ...input, scope_kind: 'project' as const, scope_id: quotaProject }
  const value = result()
  value.key_quota_capture.scope_kind = 'project'
  value.key_quota_capture.scope_id = quotaProject
  value.labels[0].key_id = 'pky_01arz3ndektsv4rrffq69g5fav'
  respond(value)
  expect(await analyzeKeyQuotaInsights(projectInput, 'csrf', new AbortController().signal)).toEqual(
    value,
  )
  value.labels[0].key_id = 'key_01arz3ndektsv4rrffq69g5fav'
  respond(value)
  await expect(
    analyzeKeyQuotaInsights(projectInput, 'csrf', new AbortController().signal),
  ).rejects.toThrow()
})
it.each(['scope_kind', 'scope_id'] as const)(
  'rejects a response from another reviewed %s',
  async (field) => {
    const value = result()
    if (field === 'scope_kind') value.key_quota_capture.scope_kind = 'project'
    else value.key_quota_capture.scope_id = 'usr_01arz3ndektsv4rrffq69g5faw'
    respond(value)
    await expect(
      analyzeKeyQuotaInsights(input, 'csrf', new AbortController().signal),
    ).rejects.toThrow()
  },
)
