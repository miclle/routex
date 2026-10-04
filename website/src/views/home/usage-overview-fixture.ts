// Controlled test fixture; production does not import this module.
import type { UsageReport, UsageStats } from '@/types/usage'
export function homeUsageFixture(empty = false): UsageReport {
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
  const stats: UsageStats = empty
    ? zero
    : {
        requests: 3,
        successes: 1,
        errors: 1,
        canceled: 1,
        success_rate: 1 / 3,
        average_duration_ms: 100,
        tokens: {
          input: { value: '9007199254740991', known: '9007199254740991', unknown_calls: 0 },
          output: { value: '2', known: '2', unknown_calls: 0 },
          total: { value: '9007199254740993', known: '9007199254740993', unknown_calls: 0 },
        },
        amounts: [{ currency: 'USD', amount: '0.123456789012345678', calls: 1 }],
        unknown_amount_calls: 2,
        pricing_statuses: { priced: 1, unpriced: 2 },
      }
  return {
    timezone: 'UTC',
    granularity: 'day',
    queried_at: '2026-10-04T12:00:00Z',
    latest_completed_at: empty ? null : '2026-10-04T11:00:00Z',
    source: 'persisted_call_records',
    may_lag: true,
    available_dimensions: ['model', 'key'],
    current: {
      from: '2026-09-04T12:00:00Z',
      to: '2026-10-04T12:00:00Z',
      summary: structuredClone(stats),
      trend: Array.from({ length: 31 }, (_, i) => ({
        start: new Date(Date.UTC(2026, 8, 4 + i)).toISOString(),
        end: new Date(Date.UTC(2026, 8, 5 + i)).toISOString(),
        stats: structuredClone(i === 0 ? stats : zero),
      })),
      models: empty
        ? []
        : [
            {
              id: 'mdl_history',
              name: 'Historical model',
              unknown: false,
              stats: structuredClone(stats),
            },
          ],
      keys: empty ? [] : [{ id: 'key_revoked', unknown: false, stats: structuredClone(stats) }],
    },
  }
}
