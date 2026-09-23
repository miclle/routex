// Controlled test/browser fixture; production queries never import this module.
import type { UsageReport, UsageStats } from '@/types/usage'

export function usageFixture(): UsageReport {
  const stats: UsageStats = {
    requests: 2,
    successes: 1,
    errors: 1,
    canceled: 0,
    success_rate: 0.5,
    average_duration_ms: 150,
    tokens: {
      input: { value: null, known: '12', unknown_calls: 1 },
      output: { value: '2', known: '2', unknown_calls: 0 },
      total: { value: null, known: '14', unknown_calls: 1 },
    },
    amounts: [
      { currency: 'USD', amount: '0.123456789012345678', calls: 1 },
      { currency: 'CNY', amount: '0', calls: 1 },
    ],
    unknown_amount_calls: 0,
    pricing_statuses: { priced: 2 },
  }
  const zero: UsageStats = {
    requests: 0,
    successes: 0,
    errors: 0,
    canceled: 0,
    success_rate: null,
    average_duration_ms: null,
    tokens: {
      input: { value: '0', known: '0', unknown_calls: 0 },
      output: { value: '0', known: '0', unknown_calls: 0 },
      total: { value: '0', known: '0', unknown_calls: 0 },
    },
    amounts: [],
    unknown_amount_calls: 0,
    pricing_statuses: {},
  }
  return {
    timezone: 'UTC',
    granularity: 'hour',
    queried_at: '2026-09-23T12:00:00Z',
    latest_completed_at: '2026-09-01T00:00:01Z',
    source: 'persisted_call_records',
    may_lag: true,
    current: {
      from: '2026-09-01T00:00:00Z',
      to: '2026-09-01T02:00:00Z',
      summary: stats,
      trend: [
        { start: '2026-09-01T00:00:00Z', end: '2026-09-01T01:00:00Z', stats },
        { start: '2026-09-01T01:00:00Z', end: '2026-09-01T02:00:00Z', stats: zero },
      ],
      models: [{ id: 'mdl_usage_resource', name: 'Historical model', unknown: false, stats }],
      keys: [{ id: 'key_usage_resource', unknown: false, stats }],
      connections: [{ id: 'con_usage_resource', unknown: false, stats }],
    },
    available_dimensions: ['model', 'key', 'connection'],
  }
}
