import type { QuotaInsightsInput, QuotaInsightsReport } from '@/types/insights-quota'
export const quotaActor = 'usr_01arz3ndektsv4rrffq69g5fav'
export const quotaProject = 'prj_01arz3ndektsv4rrffq69g5fav'
export const teamQuotaInput: QuotaInsightsInput = {
  kind: 'team_quota',
  dimension: 'tokens',
  scope: { kind: 'readable_teams', id: '' },
  cursor: null,
}
export const keyQuotaInput: QuotaInsightsInput = {
  kind: 'key_quota',
  dimension: 'tokens',
  scope: { kind: 'member', id: quotaActor },
  cursor: null,
}
export function quotaFixture(input: QuotaInsightsInput = teamQuotaInput): QuotaInsightsReport {
  return {
    kind: input.kind,
    dimension: input.dimension,
    currency: input.dimension === 'tokens' ? '' : 'USD',
    scope: { ...input.scope },
    as_of: '2026-10-12T12:00:00.123456789Z',
    coverage_start: '2026-09-01T00:00:00Z',
    time_zone: 'UTC',
    baseline_from: '2026-10-05T12:00:00.123456789Z',
    baseline_to: '2026-10-12T12:00:00.123456789Z',
    month_from: '2026-10-01T00:00:00Z',
    month_reset: '2026-11-01T00:00:00Z',
    baseline_basis: 'settled_admissions_168h',
    next_cursor: null,
    items: [
      {
        id: (input.kind === 'team_quota' ? 'tea_' : 'key_') + '01arz3ndektsv4rrffq69g5fav',
        name: 'Reviewed quota account',
        status: 'active',
        shared_key_count: input.kind === 'key_quota' ? 2 : 0,
        local_cap: '100',
        behavior: 'stop',
        month_settled: '80',
        baseline_settled: '70',
        baseline_held: '0',
        month_held: '0',
        active_held: '5',
        month_unknown: '0',
        baseline_unknown: '0',
        month_covered: true,
        baseline_covered: true,
        utilization_percent: '80.00',
        level: 'near',
        estimate_at: '2026-10-14T12:00:00.123456789Z',
        estimate_status: 'estimated',
        estimate_within_seven_days: true,
      },
    ],
  }
}
