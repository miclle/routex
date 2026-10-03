import client from './client'
import type { ResourceList } from '@/types/resources'
import type {
  CreateTeamRequest,
  TeamApprovalStep,
  TeamDecisionReceipt,
  TeamQuotaContext,
  TeamQuotaDimension,
  TeamQuotaRequest,
  TeamRequestDecision,
  TeamRequestDetail,
  TeamRequestFilters,
  TeamRequestPage,
} from '@/types/team-requests'
const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const date = (value: unknown) => typeof value === 'string' && Number.isFinite(Date.parse(value))
const text = (value: unknown) => typeof value === 'string' && value.length > 0
const etag = (value: unknown) => typeof value === 'string' && /^[a-f0-9]{64}$/.test(value)
const uuid = (value: unknown) =>
  typeof value === 'string' &&
  /^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/.test(value)
const statuses = [
  'pending_team_owner',
  'pending_quota_admin',
  'approved',
  'rejected',
  'withdrawn',
  'cancelled',
]
const actions = ['approve', 'reject', 'withdraw']
export function validTarget(value: unknown, dimension: TeamQuotaDimension) {
  if (typeof value !== 'string') return false
  return dimension === 'tokens'
    ? /^(0|[1-9][0-9]*)$/.test(value) && BigInt(value) <= 9007199254740991n
    : /^(0|[1-9][0-9]{0,17})(\.[0-9]{1,18})?$/.test(value)
}
export function compareTarget(left: string, right: string, dimension: TeamQuotaDimension) {
  const scaled = (value: string) =>
    dimension === 'tokens'
      ? BigInt(value)
      : BigInt(value.split('.')[0] + (value.split('.')[1] ?? '').padEnd(18, '0'))
  return scaled(left) > scaled(right) ? 1 : scaled(left) < scaled(right) ? -1 : 0
}
function validUsage(value: unknown) {
  if (value === null) return true
  if (
    !object(value) ||
    typeof value.activated !== 'boolean' ||
    typeof value.time_zone !== 'string' ||
    !(value.as_of === null || date(value.as_of)) ||
    !(value.coverage_start === null || date(value.coverage_start))
  )
    return false
  try {
    new Intl.DateTimeFormat('en', { timeZone: value.time_zone })
  } catch {
    return false
  }
  return ['active', 'minute', 'five_hours', 'seven_days', 'month'].every((field) => {
    const window = value[field]
    return (
      window === null ||
      (object(window) &&
        typeof window.covered === 'boolean' &&
        ['tokens_used', 'tokens_held', 'tokens_unknown', 'money_unknown'].every(
          (key) =>
            typeof window[key] === 'number' &&
            Number.isFinite(window[key]) &&
            Number(window[key]) >= 0,
        ) &&
        ['money_used', 'money_held'].every(
          (key) =>
            object(window[key]) &&
            Object.entries(window[key] as Record<string, unknown>).every(
              ([currency, amount]) =>
                /^[A-Z]{3}$/.test(currency) &&
                typeof amount === 'string' &&
                /^(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(amount),
            ),
        ))
    )
  })
}
function validContext(
  value: unknown,
  teamId: string,
  dimension: TeamQuotaDimension,
): value is TeamQuotaContext {
  if (
    !object(value) ||
    value.team_id !== teamId ||
    value.dimension !== dimension ||
    !etag(value.etag) ||
    !text(value.member_policy_etag) ||
    !text(value.team_policy_etag) ||
    !text(value.calendar_etag) ||
    !text(value.pricing_etag) ||
    typeof value.eligible !== 'boolean' ||
    !Array.isArray(value.blockers) ||
    value.blockers.some((item) => !text(item)) ||
    typeof value.platform_currency !== 'string' ||
    !/^[A-Z]{3}$/.test(value.platform_currency) ||
    !(dimension === 'tokens'
      ? value.currency === null
      : typeof value.currency === 'string' && /^[A-Z]{3}$/.test(value.currency)) ||
    !date(value.month_start) ||
    !date(value.month_end) ||
    Date.parse(String(value.month_end)) <= Date.parse(String(value.month_start)) ||
    !date(value.resource_created_at) ||
    typeof value.time_zone !== 'string' ||
    !validUsage(value.member_usage) ||
    !validUsage(value.team_usage)
  )
    return false
  try {
    new Intl.DateTimeFormat('en', { timeZone: value.time_zone })
  } catch {
    return false
  }
  return ['member_stored', 'member_effective', 'team_stored', 'team_effective'].every(
    (field) => value[field] === null || validTarget(value[field], dimension),
  )
}
function validStep(value: unknown): value is TeamApprovalStep {
  return (
    object(value) &&
    text(value.id) &&
    ['team_owner', 'quota_admin'].includes(String(value.stage)) &&
    ['pending', 'approved', 'rejected', 'withdrawn', 'cancelled'].includes(String(value.status)) &&
    date(value.entered_at) &&
    (value.status === 'pending'
      ? value.acted_at === null && value.actor_id === null
      : date(value.acted_at)) &&
    (value.actor_id === null || text(value.actor_id)) &&
    (value.actor_name === null || typeof value.actor_name === 'string') &&
    typeof value.reason === 'string'
  )
}
function validRequest(value: unknown): value is TeamQuotaRequest {
  if (
    !object(value) ||
    !text(value.id) ||
    !uuid(value.request_id) ||
    !text(value.team_id) ||
    typeof value.team_name !== 'string' ||
    !text(value.applicant_user_id) ||
    typeof value.applicant_name !== 'string' ||
    !['tokens', 'money'].includes(String(value.dimension)) ||
    !validTarget(value.target_value, value.dimension as TeamQuotaDimension) ||
    !(value.dimension === 'tokens'
      ? value.currency === null
      : typeof value.currency === 'string' && /^[A-Z]{3}$/.test(value.currency)) ||
    typeof value.reason !== 'string' ||
    !value.reason.trim() ||
    !statuses.includes(String(value.status)) ||
    !date(value.created_at) ||
    !date(value.updated_at) ||
    !validContext(
      value.submitted_snapshot,
      String(value.team_id),
      value.dimension as TeamQuotaDimension,
    ) ||
    !Array.isArray(value.steps) ||
    !value.steps.length ||
    value.steps.length > 2 ||
    !value.steps.every(validStep) ||
    new Set(value.steps.map((step) => step.id)).size !== value.steps.length ||
    !(
      value.escalation_reason === null ||
      ['no_eligible_owner', 'target_exceeds_team', 'owner_unavailable'].includes(
        String(value.escalation_reason),
      )
    )
  )
    return false
  const pending = String(value.status).startsWith('pending_')
  return (
    (pending
      ? value.resolved_at === null &&
        value.steps.some(
          (step) =>
            step.id === value.current_step_id &&
            step.status === 'pending' &&
            step.stage === (value.status === 'pending_team_owner' ? 'team_owner' : 'quota_admin'),
        )
      : value.current_step_id === null && date(value.resolved_at)) &&
    value.currency === value.submitted_snapshot.currency
  )
}
function validPreview(value: unknown, record: TeamQuotaRequest) {
  return (
    value === null ||
    (object(value) &&
      typeof value.escalates === 'boolean' &&
      ['member_before', 'team_before', 'team_after'].every(
        (field) => value[field] === null || validTarget(value[field], record.dimension),
      ) &&
      validTarget(value.member_after, record.dimension))
  )
}
function validDetail(value: unknown, id: string, admin: boolean): value is TeamRequestDetail {
  if (
    !validRequest(value) ||
    value.id !== id ||
    !object(value) ||
    !etag(value.etag) ||
    typeof value.workspace_available !== 'boolean' ||
    (!value.status.startsWith('pending_') && value.workspace_available) ||
    !validPreview(value.approval_preview, value) ||
    (admin && value.approval_preview !== null) ||
    !(
      value.current_context === null ||
      validContext(value.current_context, value.team_id, value.dimension)
    ) ||
    !Array.isArray(value.allowed_actions) ||
    new Set(value.allowed_actions).size !== value.allowed_actions.length ||
    value.allowed_actions.some((action) => !actions.includes(action)) ||
    (!value.status.startsWith('pending_') && value.allowed_actions.length > 0) ||
    (admin && value.allowed_actions.length)
  )
    return false
  const app = value.application
  return value.status === 'approved'
    ? object(app) &&
        typeof app.runtime_applied === 'boolean' &&
        ['pending', 'applied', 'superseded'].includes(String(app.application_status)) &&
        app.runtime_applied === (app.application_status === 'applied')
    : app === null
}
const rootPath = (admin: boolean) => (admin ? '/admin/quota-requests' : '/quota-requests')
export async function requestTeams(cursor: string | null, signal?: AbortSignal) {
  const data = (
    await client.get<ResourceList>('/teams', {
      params: { status: 'active', cursor: cursor ?? undefined },
      signal,
    })
  ).data
  if (
    !Array.isArray(data?.items) ||
    data.items.some(
      (item) =>
        !item || !text(item.id) || typeof item.name !== 'string' || item.status !== 'active',
    ) ||
    !(data.next_cursor === null || text(data.next_cursor)) ||
    (cursor && cursor === data.next_cursor)
  )
    throw new Error('Invalid own Team list')
  return data
}
export async function teamRequestContext(
  teamId: string,
  dimension: TeamQuotaDimension,
  signal?: AbortSignal,
) {
  const data: unknown = (
    await client.get(`/teams/${encodeURIComponent(teamId)}/quota-request-context`, {
      params: { dimension },
      signal,
    })
  ).data
  if (!validContext(data, teamId, dimension)) throw new Error('Invalid Team quota context')
  return data
}
export async function listTeamRequests(
  admin: boolean,
  view: 'my' | 'pending',
  actor: string,
  filters: TeamRequestFilters,
  cursor: string | null,
  signal?: AbortSignal,
) {
  const data: unknown = (
    await client.get(rootPath(admin), {
      params: {
        ...(admin ? {} : { view }),
        ...filters,
        status: filters.status || undefined,
        dimension: filters.dimension || undefined,
        cursor: cursor ?? undefined,
        limit: 25,
      },
      signal,
    })
  ).data
  if (
    !object(data) ||
    !Array.isArray(data.items) ||
    !data.items.every(
      (item) =>
        validRequest(item) &&
        (admin || view !== 'my' || item.applicant_user_id === actor) &&
        (admin || view !== 'pending' || item.applicant_user_id !== actor) &&
        (!filters.status || item.status === filters.status) &&
        (!filters.dimension || item.dimension === filters.dimension) &&
        (!filters.team_id || item.team_id === filters.team_id),
    ) ||
    !Number.isSafeInteger(data.total) ||
    Number(data.total) < data.items.length ||
    !(data.next_cursor === null || text(data.next_cursor)) ||
    (cursor && cursor === data.next_cursor)
  )
    throw new Error('Invalid Team request history')
  return data as unknown as TeamRequestPage
}
export async function teamRequestDetail(id: string, admin: boolean, signal?: AbortSignal) {
  const data: unknown = (
    await client.get(`${rootPath(admin)}/${encodeURIComponent(id)}`, { signal })
  ).data
  if (!validDetail(data, id, admin)) throw new Error('Invalid Team request detail')
  return data
}
export async function createTeamRequest(
  teamId: string,
  actor: string,
  body: CreateTeamRequest,
  reviewedETag: string,
  csrf: string,
) {
  const data: unknown = (
    await client.post(`/teams/${encodeURIComponent(teamId)}/quota-requests`, body, {
      headers: { 'If-Match': `"${reviewedETag}"`, 'X-CSRF-Token': csrf },
    })
  ).data
  if (
    !validRequest(data) ||
    data.request_id !== body.request_id ||
    data.team_id !== teamId ||
    data.applicant_user_id !== actor ||
    data.dimension !== body.dimension ||
    compareTarget(data.target_value, body.target_value, body.dimension) !== 0 ||
    data.reason !== body.reason
  )
    throw new Error('Invalid Team request creation receipt')
  return data
}
export async function decideTeamRequest(
  id: string,
  actor: string,
  body: TeamRequestDecision,
  reviewedETag: string,
  csrf: string,
) {
  const data: unknown = (
    await client.post(`/quota-requests/${encodeURIComponent(id)}/decision`, body, {
      headers: { 'If-Match': `"${reviewedETag}"`, 'X-CSRF-Token': csrf },
    })
  ).data
  if (
    !object(data) ||
    data.decision_id !== body.decision_id ||
    data.step_id !== body.step_id ||
    data.action !== body.action ||
    data.committed !== true ||
    !validStep(data.saved_step) ||
    data.saved_step.id !== body.step_id ||
    data.saved_step.actor_id !== actor ||
    data.saved_step.reason !== body.reason ||
    data.saved_step.status !==
      ({ approve: 'approved', reject: 'rejected', withdraw: 'withdrawn' } as const)[body.action] ||
    !validDetail(data.request, id, false)
  )
    throw new Error('Invalid Team decision receipt')
  return data as unknown as TeamDecisionReceipt
}
