import client from './client'
import type { UsageFilters, UsageReport, UsageScope, UsageTeams } from '@/types/usage'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const keys = (value: Record<string, unknown>, allowed: string[]) =>
  Object.keys(value).every((key) => allowed.includes(key))
const integer = (value: unknown) => Number.isSafeInteger(value) && (value as number) >= 0
const decimal = (value: unknown) => typeof value === 'string' && /^\d+(?:\.\d+)?$/.test(value)
const stamp = (value: unknown) => typeof value === 'string' && Number.isFinite(Date.parse(value))
function timezone(value: unknown) {
  if (typeof value !== 'string' || !value || value === 'Local') return false
  try {
    new Intl.DateTimeFormat('en', { timeZone: value }).format()
    return true
  } catch {
    return false
  }
}
const teamID = (value: unknown): value is string =>
  typeof value === 'string' && /^tea_[A-Za-z0-9_-]{1,60}$/.test(value)
function count(value: unknown) {
  return (
    object(value) &&
    keys(value, ['value', 'known', 'unknown_calls']) &&
    (value.value === null || (typeof value.value === 'string' && /^\d+$/.test(value.value))) &&
    typeof value.known === 'string' &&
    /^\d+$/.test(value.known) &&
    integer(value.unknown_calls)
  )
}
function stats(value: unknown) {
  if (
    !object(value) ||
    !keys(value, [
      'requests',
      'successes',
      'errors',
      'canceled',
      'success_rate',
      'average_duration_ms',
      'tokens',
      'amounts',
      'unknown_amount_calls',
      'pricing_statuses',
    ])
  )
    return false
  const tokens = value.tokens
  return (
    ['requests', 'successes', 'errors', 'canceled', 'unknown_amount_calls'].every((key) =>
      integer(value[key]),
    ) &&
    (value.success_rate === null ||
      (typeof value.success_rate === 'number' &&
        Number.isFinite(value.success_rate) &&
        value.success_rate >= 0 &&
        value.success_rate <= 1)) &&
    (value.average_duration_ms === null ||
      (typeof value.average_duration_ms === 'number' &&
        Number.isFinite(value.average_duration_ms) &&
        value.average_duration_ms >= 0)) &&
    object(tokens) &&
    keys(tokens, ['input', 'output', 'total']) &&
    ['input', 'output', 'total'].every((key) => count(tokens[key])) &&
    Array.isArray(value.amounts) &&
    value.amounts.every(
      (amount) =>
        object(amount) &&
        keys(amount, ['currency', 'amount', 'calls']) &&
        typeof amount.currency === 'string' &&
        !!amount.currency &&
        decimal(amount.amount) &&
        integer(amount.calls),
    ) &&
    object(value.pricing_statuses) &&
    Object.values(value.pricing_statuses).every(integer)
  )
}
function period(value: unknown) {
  if (
    !object(value) ||
    !keys(value, ['from', 'to', 'summary', 'trend', 'models', 'keys']) ||
    !stamp(value.from) ||
    !stamp(value.to) ||
    Date.parse(value.from as string) >= Date.parse(value.to as string)
  )
    return false
  return (
    stats(value.summary) &&
    Array.isArray(value.keys) &&
    value.keys.length === 0 &&
    Array.isArray(value.models) &&
    value.models.every(
      (group) =>
        object(group) &&
        keys(group, ['id', 'name', 'unknown', 'stats']) &&
        typeof group.id === 'string' &&
        (group.name === undefined || typeof group.name === 'string') &&
        typeof group.unknown === 'boolean' &&
        stats(group.stats),
    ) &&
    new Set(value.models.map((group) => group.id)).size === value.models.length &&
    Array.isArray(value.trend) &&
    value.trend.every(
      (bucket) =>
        object(bucket) &&
        keys(bucket, ['start', 'end', 'stats']) &&
        stamp(bucket.start) &&
        stamp(bucket.end) &&
        Date.parse(bucket.start as string) < Date.parse(bucket.end as string) &&
        stats(bucket.stats),
    )
  )
}
function teamReport(value: unknown, team: string): value is UsageReport {
  return (
    object(value) &&
    keys(value, [
      'team_id',
      'timezone',
      'granularity',
      'queried_at',
      'latest_completed_at',
      'source',
      'may_lag',
      'current',
      'previous',
      'available_dimensions',
    ]) &&
    value.team_id === team &&
    timezone(value.timezone) &&
    ['hour', 'day', 'week', 'month'].includes(value.granularity as string) &&
    stamp(value.queried_at) &&
    (value.latest_completed_at === null || stamp(value.latest_completed_at)) &&
    value.source === 'persisted_call_records' &&
    typeof value.may_lag === 'boolean' &&
    Array.isArray(value.available_dimensions) &&
    value.available_dimensions.length === 1 &&
    value.available_dimensions[0] === 'model' &&
    period(value.current) &&
    (value.previous === undefined || period(value.previous))
  )
}
export async function getUsage(
  scope: UsageScope,
  filters: UsageFilters,
  signal?: AbortSignal,
): Promise<UsageReport> {
  if (scope.teamId && (scope.admin || scope.projectId || !teamID(scope.teamId)))
    throw new Error('Invalid Team usage scope')
  if (
    scope.teamId &&
    [
      'key_id',
      'user_id',
      'project_id',
      'team_id',
      'provider_id',
      'provider_model_id',
      'connection_id',
    ].some((key) => filters[key as keyof UsageFilters] !== undefined)
  )
    throw new Error('Invalid Team usage filters')
  const value: unknown = (
    await client.get(
      scope.teamId
        ? `/teams/${encodeURIComponent(scope.teamId)}/usage`
        : scope.projectId
          ? `/projects/${encodeURIComponent(scope.projectId)}/usage`
          : scope.admin
            ? '/admin/usage'
            : '/usage',
      { params: filters, signal },
    )
  ).data
  if (scope.teamId && !teamReport(value, scope.teamId)) throw new Error('Invalid Team usage report')
  return value as UsageReport
}
export async function getUsageTeams(
  cursor: string | null,
  signal?: AbortSignal,
): Promise<UsageTeams> {
  const value: unknown = (
    await client.get('/teams', {
      params: { status: 'active', cursor: cursor ?? undefined },
      signal,
    })
  ).data
  if (
    !object(value) ||
    !Array.isArray(value.items) ||
    value.items.length > 100 ||
    !value.items.every(
      (team) =>
        object(team) &&
        teamID(team.id) &&
        typeof team.name === 'string' &&
        !!team.name &&
        team.status === 'active',
    ) ||
    new Set(value.items.map((team) => team.id)).size !== value.items.length ||
    !(
      value.next_cursor === null ||
      (typeof value.next_cursor === 'string' && !!value.next_cursor && value.next_cursor !== cursor)
    )
  )
    throw new Error('Invalid usage Teams')
  return {
    items: value.items.map((team) => ({ id: team.id as string, name: team.name as string })),
    next_cursor: value.next_cursor as string | null,
  }
}

// Reuse the protocol-neutral guards without broadening Team report dimensions.
export function isPersonalUsageReport(value: unknown): value is UsageReport {
  function groups(value: unknown) {
    return (
      Array.isArray(value) &&
      value.length <= 500 &&
      value.every(
        (group) =>
          object(group) &&
          keys(group, ['id', 'name', 'unknown', 'stats']) &&
          typeof group.id === 'string' &&
          (group.name === undefined || typeof group.name === 'string') &&
          typeof group.unknown === 'boolean' &&
          group.unknown === (group.id === '') &&
          coherentStats(group.stats),
      ) &&
      new Set(value.map((group) => group.id)).size === value.length
    )
  }
  function coherentStats(value: unknown): value is import('@/types/usage').UsageStats {
    if (!stats(value)) return false
    const row = value as import('@/types/usage').UsageStats
    if (
      row.requests !== row.successes + row.errors + row.canceled ||
      row.unknown_amount_calls > row.requests ||
      (row.requests === 0
        ? row.success_rate !== null || row.average_duration_ms !== null
        : row.success_rate === null ||
          row.average_duration_ms === null ||
          Math.abs(row.success_rate - row.successes / row.requests) > 1e-12)
    )
      return false
    for (const count of Object.values(row.tokens)) {
      if (
        !/^(0|[1-9]\d*)$/.test(count.known) ||
        count.unknown_calls > row.requests ||
        (count.unknown_calls === 0 ? count.value !== count.known : count.value !== null)
      )
        return false
    }
    return (
      BigInt(row.tokens.total.known) ===
        BigInt(row.tokens.input.known) + BigInt(row.tokens.output.known) &&
      new Set(row.amounts.map((amount) => amount.currency)).size === row.amounts.length &&
      row.amounts.every((amount) => amount.calls <= row.requests)
    )
  }
  function personalPeriod(value: unknown) {
    if (
      !object(value) ||
      !keys(value, ['from', 'to', 'summary', 'trend', 'models', 'keys']) ||
      !stamp(value.from) ||
      !stamp(value.to) ||
      Date.parse(value.from as string) >= Date.parse(value.to as string) ||
      !coherentStats(value.summary) ||
      !groups(value.models) ||
      !groups(value.keys) ||
      !Array.isArray(value.trend) ||
      value.trend.length === 0 ||
      value.trend.length > 1000
    )
      return false
    let requests = 0,
      known = 0n,
      unknown = 0
    for (let index = 0; index < value.trend.length; index++) {
      const bucket = value.trend[index]
      if (
        !object(bucket) ||
        !keys(bucket, ['start', 'end', 'stats']) ||
        !stamp(bucket.start) ||
        !stamp(bucket.end) ||
        Date.parse(bucket.start as string) >= Date.parse(bucket.end as string) ||
        Date.parse(bucket.start as string) >= Date.parse(value.to as string) ||
        Date.parse(bucket.end as string) <= Date.parse(value.from as string) ||
        !coherentStats(bucket.stats) ||
        (index > 0 && bucket.start !== value.trend[index - 1].end)
      )
        return false
      requests += bucket.stats.requests
      known += BigInt(bucket.stats.tokens.total.known)
      unknown += bucket.stats.tokens.total.unknown_calls
    }
    return (
      Date.parse(value.trend[0].start) <= Date.parse(value.from as string) &&
      Date.parse(value.trend.at(-1).end) >= Date.parse(value.to as string) &&
      requests === value.summary.requests &&
      known.toString() === value.summary.tokens.total.known &&
      unknown === value.summary.tokens.total.unknown_calls
    )
  }
  return (
    object(value) &&
    keys(value, [
      'timezone',
      'granularity',
      'queried_at',
      'latest_completed_at',
      'source',
      'may_lag',
      'current',
      'available_dimensions',
    ]) &&
    timezone(value.timezone) &&
    ['hour', 'day', 'week', 'month'].includes(value.granularity as string) &&
    stamp(value.queried_at) &&
    (value.latest_completed_at === null || stamp(value.latest_completed_at)) &&
    value.source === 'persisted_call_records' &&
    typeof value.may_lag === 'boolean' &&
    Array.isArray(value.available_dimensions) &&
    value.available_dimensions.length === 2 &&
    new Set(value.available_dimensions).size === 2 &&
    value.available_dimensions.includes('model') &&
    value.available_dimensions.includes('key') &&
    personalPeriod(value.current)
  )
}
