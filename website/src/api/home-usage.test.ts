import { afterEach, beforeEach, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { getHomeUsage, homeUsageKey } from './home-usage'
import { homeUsageFixture } from '@/views/home/usage-overview-fixture'
import type { UsageReport } from '@/types/usage'
let data: UsageReport, requests: InternalAxiosRequestConfig[]
const original = client.defaults.adapter
beforeEach(() => {
  data = homeUsageFixture()
  requests = []
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { config, data, headers: new AxiosHeaders(), status: 200, statusText: '' }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
it('reads only server-anchored Personal 30d with exact settings and cancellation', async () => {
  const signal = new AbortController().signal
  const result = await getHomeUsage(signal)
  expect(requests).toHaveLength(1)
  expect(requests[0].url).toBe('/usage')
  expect(requests[0].params).toEqual({
    period: '30d',
    timezone: 'UTC',
    granularity: 'day',
    compare: false,
  })
  expect(requests[0].signal).toBe(signal)
  expect(requests[0].headers.has('Authorization')).toBe(false)
  expect(result.current.summary.tokens.total.value).toBe('9007199254740993')
  expect(result.current.summary.amounts[0].amount).toBe('0.123456789012345678')
  expect(result.current.summary.success_rate).toBe(1 / 3)
  expect(result.current.trend).toHaveLength(31)
  expect(homeUsageKey('actor', 2)).toEqual(['home-usage', 'actor', 'personal', 2, '30d', 'UTC'])
})
it.each([
  [
    'Team expansion',
    (r: UsageReport) => {
      r.team_id = 'tea_other'
    },
  ],
  [
    'admin dimensions',
    (r: UsageReport) => {
      r.available_dimensions.push('provider')
    },
  ],
  [
    'private topology',
    (r: UsageReport) => {
      r.current.providers = []
    },
  ],
  [
    'comparison',
    (r: UsageReport) => {
      r.previous = structuredClone(r.current)
    },
  ],
  [
    'wrong range',
    (r: UsageReport) => {
      r.current.from = '2026-09-05T12:00:00Z'
    },
  ],
  [
    'wrong timezone',
    (r: UsageReport) => {
      r.timezone = 'Asia/Shanghai'
    },
  ],
  [
    'wrong granularity',
    (r: UsageReport) => {
      r.granularity = 'hour'
    },
  ],
  [
    'wrong source',
    (r: UsageReport) => {
      r.source = 'live' as UsageReport['source']
    },
  ],
  [
    'unsafe request count',
    (r: UsageReport) => {
      r.current.summary.requests = Number.MAX_SAFE_INTEGER + 1
    },
  ],
  [
    'incoherent statuses',
    (r: UsageReport) => {
      r.current.summary.canceled = 0
    },
  ],
  [
    'changed rate denominator',
    (r: UsageReport) => {
      r.current.summary.success_rate = 0.5
    },
  ],
  [
    'nonfinite rate',
    (r: UsageReport) => {
      r.current.summary.success_rate = Infinity
    },
  ],
  [
    'invalid token',
    (r: UsageReport) => {
      r.current.summary.tokens.total.known = '1e12'
    },
  ],
  [
    'unknown disguised as zero',
    (r: UsageReport) => {
      r.current.summary.tokens.total.unknown_calls = 1
    },
  ],
  [
    'sum mismatch',
    (r: UsageReport) => {
      r.current.summary.tokens.total.value = r.current.summary.tokens.total.known = '5'
    },
  ],
  [
    'duplicate group',
    (r: UsageReport) => {
      r.current.models.push(structuredClone(r.current.models[0]))
    },
  ],
  [
    'bad identity',
    (r: UsageReport) => {
      r.current.models[0].unknown = true
    },
  ],
  [
    'duplicate bucket',
    (r: UsageReport) => {
      r.current.trend.splice(1, 0, structuredClone(r.current.trend[0]))
    },
  ],
  [
    'missing bucket',
    (r: UsageReport) => {
      r.current.trend.splice(3, 1)
    },
  ],
  [
    'partial total',
    (r: UsageReport) => {
      r.current.trend[0].stats = structuredClone(r.current.trend[1].stats)
    },
  ],
  [
    'missing end bucket',
    (r: UsageReport) => {
      r.current.trend.pop()
    },
  ],
] as const)('rejects %s before exposing Home facts', async (_name, corrupt) => {
  corrupt(data)
  await expect(getHomeUsage()).rejects.toThrow('Invalid Personal overview usage')
})
it('retains valid empty reports as known zero with unknown success rate', async () => {
  data = homeUsageFixture(true)
  expect((await getHomeUsage()).current.summary.success_rate).toBeNull()
})
it('preserves incomplete coverage as null and exact known subtotal', async () => {
  const rows = [
    data.current.summary,
    data.current.trend[0].stats,
    data.current.models[0].stats,
    data.current.keys[0].stats,
  ]
  rows.forEach((row) => {
    row.tokens.input.value = null
    row.tokens.input.unknown_calls = 1
    row.tokens.total.value = null
    row.tokens.total.unknown_calls = 1
  })
  expect((await getHomeUsage()).current.summary.tokens.total).toEqual({
    value: null,
    known: '9007199254740993',
    unknown_calls: 1,
  })
})
