import { afterEach, expect, it } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from '@/api/client'
import {
  getProviderQualityReport,
  parseProviderQuality,
  exportProviderQuality,
} from '@/api/insights-provider-quality'
import type { ProviderQualityReport } from '@/types/insights'
import { qualityRange, qualityFixture } from './provider-quality-fixture'

const original = client.defaults.adapter
afterEach(() => {
  client.defaults.adapter = original
})
it('requires exact selected range and closed complete attempt aggregate shapes', () => {
  expect(parseProviderQuality(qualityFixture(), qualityRange)).toEqual(qualityFixture())
  const mutations: ((value: ProviderQualityReport) => void)[] = [
    (value) => {
      value.unknown_attribution = 0
    },
    (value) => {
      value.summary.success_rate_bps = 10000
    },
    (value) => {
      value.providers.push({ ...value.providers[0] })
    },
    (value) => {
      value.summary.eligible = 3
    },
    (value) => {
      value.providers[0].known_duration = 2
    },
    (value) => {
      value.summary.p95_duration_ms = null
    },
    (value) => {
      value.total_attempts = 100001
    },
    (value) => {
      value.from = '2026-08-01T00:00:00Z'
    },
    (value) => {
      Object.assign(value, { raw_calls: [] })
    },
    (value) => {
      Object.assign(value.providers[0], { credential_id: 'private' })
    },
    (value) => {
      value.source = 'persisted_call_records' as never
    },
  ]
  for (const mutate of mutations) {
    const value = qualityFixture()
    mutate(value)
    expect(() => parseProviderQuality(value, qualityRange)).toThrow()
  }
  const unknown = qualityFixture()
  unknown.total_attempts = 1
  unknown.unknown_attribution = 1
  unknown.providers = []
  unknown.summary = {
    attempts: 0,
    eligible: 0,
    excluded: 0,
    credential_rejected: 0,
    successes: 0,
    success_rate_bps: null,
    rate_limited: 0,
    server_errors: 0,
    known_duration: 0,
    unknown_duration: 0,
    p95_duration_ms: null,
    average_duration_ms: null,
  }
  expect(parseProviderQuality(unknown, qualityRange).summary.success_rate_bps).toBeNull()
})
it('sends only the closed range, never catalog or actor selectors, and rejects invalid ranges before HTTP', async () => {
  const requests: unknown[] = []
  client.defaults.adapter = async (config) => {
    requests.push(config.params)
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: qualityFixture(),
    }
  }
  await getProviderQualityReport(qualityRange, new AbortController().signal)
  expect(requests).toEqual([qualityRange])
  for (const input of [
    { ...qualityRange, provider_id: 'prv_other' },
    { ...qualityRange, to: qualityRange.from },
    { ...qualityRange, timezone: 'Local' },
    { ...qualityRange, from: 'yesterday' },
  ])
    await expect(getProviderQualityReport(input, new AbortController().signal)).rejects.toThrow()
  expect(requests).toHaveLength(1)
})
it('reuses bounded fresh server CSV capture and refuses malformed MIME or canceled replies', async () => {
  let mime = 'text/csv; charset=utf-8'
  const controller = new AbortController()
  client.defaults.adapter = async (config) => ({
    config,
    status: 200,
    statusText: '',
    headers: new AxiosHeaders({ 'content-type': mime }),
    data: new Blob(['source,attempts\npersisted_call_attempts,5']),
  })
  expect((await exportProviderQuality(qualityRange, controller.signal)).size).toBeGreaterThan(0)
  mime = 'application/json'
  await expect(exportProviderQuality(qualityRange, controller.signal)).rejects.toThrow()
  controller.abort()
  await expect(getProviderQualityReport(qualityRange, controller.signal)).rejects.toThrow()
})

it('accepts equivalent offset normalization but refuses a one-nanosecond drift', () => {
  const input = {
    ...qualityRange,
    from: '2026-09-01T08:00:00.123456789+08:00',
    to: '2026-09-02T08:00:00+08:00',
  }
  const value = {
    ...qualityFixture(),
    from: '2026-09-01T00:00:00.123456789Z',
    to: '2026-09-02T00:00:00Z',
  }
  expect(parseProviderQuality(value, input).from).toBe(value.from)
  expect(() =>
    parseProviderQuality({ ...value, from: '2026-09-01T00:00:00.123456788Z' }, input),
  ).toThrow()
  expect(() => parseProviderQuality({ ...value, latest_completed_at: value.to }, input)).toThrow()
})

it('requires the nullable finite mean with exact known eligible sample coverage and preserves fractional or zero values', () => {
  for (const average of [0, 0.5, 100.125, Number.MAX_SAFE_INTEGER, 2 ** 63]) {
    const value = qualityFixture()
    value.summary.average_duration_ms = value.providers[0].average_duration_ms = average
    expect(parseProviderQuality(value, qualityRange).summary.average_duration_ms).toBe(average)
  }
  // A large upper tail can make the mean exceed P95; this is not an invalid report.
  const upperTail = qualityFixture()
  upperTail.summary.average_duration_ms = upperTail.providers[0].average_duration_ms = 200
  expect(parseProviderQuality(upperTail, qualityRange).summary.average_duration_ms).toBe(200)
  for (const average of [undefined, null, -1, NaN, Infinity, '1.5', 2 ** 63 + 4096]) {
    const value = qualityFixture()
    Object.assign(value.summary, { average_duration_ms: average })
    expect(() => parseProviderQuality(value, qualityRange)).toThrow()
    const row = qualityFixture()
    Object.assign(row.providers[0], { average_duration_ms: average })
    expect(() => parseProviderQuality(row, qualityRange)).toThrow()
  }
  const unknown = qualityFixture()
  for (const row of [unknown.summary, ...unknown.providers]) {
    row.known_duration = 0
    row.unknown_duration = row.eligible
    row.p95_duration_ms = row.average_duration_ms = null
  }
  expect(parseProviderQuality(unknown, qualityRange).summary.average_duration_ms).toBeNull()
  unknown.providers[0].average_duration_ms = 0
  expect(() => parseProviderQuality(unknown, qualityRange)).toThrow()
})

it('accepts a valid int64-duration upper tail whose mean exceeds the safe-integer bound while P95 stays zero', () => {
  const value = qualityFixture()
  value.total_attempts = 100
  value.unknown_attribution = 0
  for (const row of [value.summary, ...value.providers]) {
    Object.assign(row, {
      attempts: 100,
      eligible: 100,
      excluded: 0,
      credential_rejected: 0,
      successes: 100,
      success_rate_bps: 10000,
      rate_limited: 0,
      server_errors: 0,
      known_duration: 100,
      unknown_duration: 0,
      p95_duration_ms: 0,
      average_duration_ms: 2 ** 63 / 100,
    })
  }
  expect(value.summary.average_duration_ms).toBeGreaterThan(Number.MAX_SAFE_INTEGER)
  expect(parseProviderQuality(value, qualityRange).summary.average_duration_ms).toBe(2 ** 63 / 100)
})
