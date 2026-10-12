import type { ProviderQualityRange, ProviderQualityReport } from '@/types/insights'
export const qualityRange: ProviderQualityRange = {
  from: '2026-09-01T00:00:00Z',
  to: '2026-09-02T00:00:00Z',
  timezone: 'UTC',
}
export function qualityFixture(): ProviderQualityReport {
  const counts = {
    attempts: 4,
    eligible: 2,
    excluded: 1,
    credential_rejected: 1,
    successes: 1,
    success_rate_bps: 5000,
    rate_limited: 1,
    server_errors: 0,
    known_duration: 1,
    unknown_duration: 1,
    p95_duration_ms: 150,
  }
  return {
    ...qualityRange,
    source: 'persisted_call_attempts',
    queried_at: '2026-09-03T00:00:00Z',
    latest_completed_at: '2026-09-01T12:00:00Z',
    may_lag: true,
    total_attempts: 5,
    unknown_attribution: 1,
    summary: { ...counts },
    providers: [{ provider_id: 'prv_recorded', name: 'Historical Provider', ...counts }],
  }
}
