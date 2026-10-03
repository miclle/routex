import { isAxiosError } from 'axios'
import client from './client'
import type {
  TeamModelCandidate,
  TeamModelDecisionIntent,
  TeamModelDecisionReceipt,
  TeamModelRequest,
  TeamModelRequestDetail,
  TeamModelRequestIntent,
  TeamModelRequestPage,
  TeamModelRequestStatus,
  TeamModelRequestTeamPage,
  TeamModelWorkspace,
} from '@/types/team-model-requests'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const id = (value: unknown) => typeof value === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(value)
const uuid = (value: unknown) =>
  typeof value === 'string' &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value)
const etag = (value: unknown) => typeof value === 'string' && /^[0-9a-f]{64}$/.test(value)
const time = (value: unknown) => typeof value === 'string' && Number.isFinite(Date.parse(value))
const text = (value: unknown) => typeof value === 'string'
const cursor = (value: unknown) =>
  value === null || (typeof value === 'string' && /^[A-Za-z0-9_-]{1,512}$/.test(value))
const integer = (value: unknown) => Number.isSafeInteger(value) && Number(value) >= 0
const actions = ['approve', 'reject', 'withdraw']
const statuses = ['pending', 'approved', 'rejected', 'withdrawn', 'cancelled']
function invalid(): never {
  throw new Error('Invalid Team model request response')
}
export function validTeamModelReason(value: string, required = true) {
  return (
    (!required || !!value.trim()) &&
    new TextEncoder().encode(value).length <= 1024 &&
    !/\p{Cc}/u.test(value)
  )
}
export function teamModelOutcomeUnknown(error: unknown) {
  return !isAxiosError(error) || !error.response || error.response.status >= 500
}
const teamPath = (team: string) => `/teams/${encodeURIComponent(team)}`
const scope = (team?: string) =>
  team ? `${teamPath(team)}/model-requests` : '/team-model-requests'
function candidate(value: unknown, team: string, model?: string): TeamModelCandidate {
  if (
    !object(value) ||
    value.team_id !== team ||
    !id(value.id) ||
    (model && value.id !== model) ||
    !text(value.name) ||
    value.status !== 'active' ||
    !time(value.created_at) ||
    !Array.isArray(value.protocols) ||
    value.protocols.some((item) => !text(item)) ||
    new Set(value.protocols).size !== value.protocols.length ||
    !object(value.input_capabilities) ||
    Object.values(value.input_capabilities).some(
      (items) => !Array.isArray(items) || items.some((item) => !['image', 'pdf'].includes(item)),
    ) ||
    typeof value.team_granted !== 'boolean' ||
    typeof value.pending_request !== 'boolean' ||
    !(value.own_pending_request_id === null || id(value.own_pending_request_id)) ||
    (!value.pending_request && value.own_pending_request_id !== null) ||
    !etag(value.review_etag)
  )
    invalid()
  return value as unknown as TeamModelCandidate
}
function record(value: unknown, actor: string, team?: string, expected?: string): TeamModelRequest {
  if (
    !object(value) ||
    !id(value.id) ||
    (expected && value.id !== expected) ||
    !uuid(value.request_id) ||
    !id(value.team_id) ||
    (team && value.team_id !== team) ||
    !text(value.team_name) ||
    !id(value.applicant_user_id) ||
    (!team && value.applicant_user_id !== actor) ||
    !id(value.applicant_membership_id) ||
    !text(value.applicant_name) ||
    !id(value.model_id) ||
    !text(value.model_name) ||
    !text(value.reason) ||
    !validTeamModelReason(value.reason) ||
    !statuses.includes(String(value.status)) ||
    !time(value.created_at) ||
    !time(value.updated_at) ||
    !(value.resolved_at === null || time(value.resolved_at)) ||
    !(value.cancelled_reason === null || text(value.cancelled_reason))
  )
    invalid()
  if (
    (value.status === 'pending' && (value.resolved_at !== null || value.decision !== null)) ||
    (value.status !== 'pending' && value.resolved_at === null)
  )
    invalid()
  if (value.decision !== null) {
    const decision = value.decision
    if (
      !object(decision) ||
      !uuid(decision.decision_id) ||
      !id(decision.actor_id) ||
      !text(decision.actor_name) ||
      !actions.includes(String(decision.action)) ||
      !text(decision.reason) ||
      !validTeamModelReason(decision.reason, decision.action === 'reject') ||
      !time(decision.decided_at) ||
      (
        { approve: 'approved', reject: 'rejected', withdraw: 'withdrawn' } as Record<string, string>
      )[String(decision.action)] !== value.status
    )
      invalid()
  } else if (['approved', 'rejected', 'withdrawn'].includes(String(value.status))) invalid()
  return value as unknown as TeamModelRequest
}
function application(value: Record<string, unknown>, status: string) {
  if (
    ![value.current_membership_matches, value.current_granted, value.runtime_applied].every(
      (item) => item === null || typeof item === 'boolean',
    ) ||
    !['pending', 'applied', 'superseded', 'unavailable'].includes(String(value.application_status))
  )
    invalid()
  if (value.application_status === 'unavailable') {
    if (
      value.current_membership_matches !== null ||
      value.current_granted !== null ||
      value.runtime_applied !== null
    )
      invalid()
  } else if (
    value.current_granted === null ||
    value.runtime_applied === null ||
    value.current_membership_matches === null ||
    value.runtime_applied !== (value.application_status === 'applied') ||
    (value.runtime_applied && !value.current_granted) ||
    (status !== 'approved' && (value.runtime_applied || value.application_status !== 'pending'))
  )
    invalid()
}
function resource(value: unknown, expected: string) {
  return (
    object(value) &&
    value.id === expected &&
    text(value.name) &&
    ['active', 'disabled', 'archived'].includes(String(value.status))
  )
}
function detail(
  value: unknown,
  actor: string,
  team?: string,
  expected?: string,
): TeamModelRequestDetail {
  const row = record(value, actor, team, expected)
  if (
    !object(value) ||
    !etag(value.review_etag) ||
    !Array.isArray(value.allowed_actions) ||
    value.allowed_actions.some((item) => !actions.includes(item)) ||
    new Set(value.allowed_actions).size !== value.allowed_actions.length ||
    (row.status !== 'pending' && value.allowed_actions.length) ||
    !(value.current_team === null || resource(value.current_team, row.team_id)) ||
    !(value.current_model === null || resource(value.current_model, row.model_id))
  )
    invalid()
  application(value, row.status)
  if (
    value.application_status === 'unavailable' &&
    (value.current_team !== null || value.current_model !== null)
  )
    invalid()
  return value as unknown as TeamModelRequestDetail
}
export async function listTeamModelRequestTeams(
  q: string,
  next: string | null,
  signal?: AbortSignal,
): Promise<TeamModelRequestTeamPage> {
  const value = (
    await client.get<unknown>('/team-model-request-teams', {
      params: { q: q || undefined, cursor: next || undefined, limit: 50 },
      signal,
    })
  ).data
  if (
    !object(value) ||
    !Array.isArray(value.items) ||
    value.items.length > 50 ||
    !cursor(value.next_cursor) ||
    value.items.some(
      (item) => !object(item) || !id(item.id) || !text(item.name) || !id(item.membership_id),
    ) ||
    new Set(value.items.map((item) => item.id)).size !== value.items.length
  )
    invalid()
  return value as unknown as TeamModelRequestTeamPage
}
export async function listTeamModelCandidates(
  team: string,
  q: string,
  next: string | null,
  signal?: AbortSignal,
) {
  const value = (
    await client.get<unknown>(`${teamPath(team)}/model-request-candidates`, {
      params: { q: q || undefined, cursor: next || undefined, limit: 50 },
      signal,
    })
  ).data
  if (
    !object(value) ||
    !Array.isArray(value.items) ||
    value.items.length > 50 ||
    !cursor(value.next_cursor)
  )
    invalid()
  const items = value.items.map((item) => candidate(item, team))
  if (new Set(items.map((item) => item.id)).size !== items.length) invalid()
  return { items, next_cursor: value.next_cursor as string | null }
}
export async function getTeamModelCandidate(team: string, model: string, signal?: AbortSignal) {
  return candidate(
    (
      await client.get<unknown>(
        `${teamPath(team)}/model-request-candidates/${encodeURIComponent(model)}`,
        { signal },
      )
    ).data,
    team,
    model,
  )
}
export async function listTeamModelRequests(
  actor: string,
  team: string | undefined,
  status: TeamModelRequestStatus | '',
  next: string | null,
  filters: { teamID?: string; modelID?: string } = {},
  signal?: AbortSignal,
): Promise<TeamModelRequestPage> {
  const value = (
    await client.get<unknown>(scope(team), {
      params: {
        status: status || undefined,
        cursor: next || undefined,
        limit: 50,
        team_id: team ? undefined : filters.teamID,
        model_id: filters.modelID,
      },
      signal,
    })
  ).data
  if (
    !object(value) ||
    !Array.isArray(value.items) ||
    value.items.length > 50 ||
    !integer(value.total) ||
    Number(value.total) < value.items.length ||
    !cursor(value.next_cursor)
  )
    invalid()
  const items = value.items.map((item) => record(item, actor, team))
  if (
    items.some(
      (item) =>
        (filters.teamID && item.team_id !== filters.teamID) ||
        (filters.modelID && item.model_id !== filters.modelID),
    )
  )
    invalid()
  if (new Set(items.map((item) => item.id)).size !== items.length) invalid()
  return { items, total: value.total as number, next_cursor: value.next_cursor as string | null }
}
export async function getTeamModelRequest(
  actor: string,
  team: string | undefined,
  request: string,
  signal?: AbortSignal,
) {
  return detail(
    (await client.get<unknown>(`${scope(team)}/${encodeURIComponent(request)}`, { signal })).data,
    actor,
    team,
    request,
  )
}
export async function createTeamModelRequest(
  actor: string,
  intent: TeamModelRequestIntent,
  csrf: string,
) {
  const value = detail(
    (
      await client.post<unknown>('/team-model-requests', intent.body, {
        headers: { 'X-CSRF-Token': csrf, 'If-Match': `"${intent.etag}"` },
      })
    ).data,
    actor,
  )
  if (
    value.request_id !== intent.body.request_id ||
    value.team_id !== intent.body.team_id ||
    value.model_id !== intent.body.model_id ||
    value.reason !== intent.body.reason
  )
    invalid()
  return value
}
export async function decideTeamModelRequest(
  actor: string,
  team: string | undefined,
  request: string,
  intent: TeamModelDecisionIntent,
  csrf: string,
): Promise<TeamModelDecisionReceipt> {
  const value = (
    await client.post<unknown>(
      `${scope(team)}/${encodeURIComponent(request)}/decision`,
      intent.body,
      { headers: { 'X-CSRF-Token': csrf, 'If-Match': `"${intent.etag}"` } },
    )
  ).data
  if (!object(value) || value.committed !== true || value.decision_id !== intent.body.decision_id)
    invalid()
  const saved = record(value.saved_request, actor, team, request)
  if (
    !saved.decision ||
    saved.decision.decision_id !== intent.body.decision_id ||
    saved.decision.actor_id !== actor ||
    saved.decision.action !== intent.body.action ||
    saved.decision.reason !== intent.body.reason
  )
    invalid()
  application(value, saved.status)
  return value as unknown as TeamModelDecisionReceipt
}
export async function getTeamModelWorkspace(
  team: string,
  signal?: AbortSignal,
): Promise<TeamModelWorkspace> {
  const value = (await client.get<unknown>(`${teamPath(team)}/model-request-workspace`, { signal }))
    .data
  if (
    !object(value) ||
    value.team_id !== team ||
    !text(value.name) ||
    !['active', 'disabled', 'archived'].includes(String(value.status)) ||
    value.can_review_requests !== true ||
    !Array.isArray(value.models) ||
    value.models.length > 1000 ||
    value.model_count !== value.models.length ||
    value.models.some(
      (item) => !object(item) || !id(item.id) || !resource(item, String(item.id)),
    ) ||
    new Set(value.models.map((item) => item.id)).size !== value.models.length
  )
    invalid()
  return value as unknown as TeamModelWorkspace
}
