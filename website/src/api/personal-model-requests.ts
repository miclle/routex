import { isAxiosError } from 'axios'
import client from './client'
import type {
  PersonalModelCandidate,
  PersonalModelDecisionIntent,
  PersonalModelDecisionReceipt,
  PersonalModelRequest,
  PersonalModelRequestDetail,
  PersonalModelRequestIntent,
  PersonalModelRequestPage,
  PersonalModelRequestStatus,
  PersonalModelWorkspace,
} from '@/types/personal-model-requests'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const id = (value: unknown) => typeof value === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(value)
const uuid = (value: unknown) =>
  typeof value === 'string' &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value)
const etag = (value: unknown) => typeof value === 'string' && /^[0-9a-f]{64}$/.test(value)
const time = (value: unknown) => typeof value === 'string' && Number.isFinite(Date.parse(value))
const text = (value: unknown) => typeof value === 'string'
const integer = (value: unknown) => Number.isSafeInteger(value) && Number(value) >= 0
const cursor = (value: unknown) =>
  value === null || (typeof value === 'string' && /^[A-Za-z0-9_-]{1,512}$/.test(value))
const actions = ['approve', 'reject', 'withdraw']
const statuses = ['pending', 'approved', 'rejected', 'withdrawn', 'cancelled']
function invalid(): never {
  throw new Error('Invalid Personal model request response')
}
export function validPersonalModelReason(value: string, required = true) {
  const reason = value.trim()
  return (
    (!required || !!reason) &&
    new TextEncoder().encode(value).length <= 1024 &&
    !/\p{Cc}/u.test(value)
  )
}
export function personalModelOutcomeUnknown(error: unknown) {
  return !isAxiosError(error) || !error.response || error.response.status >= 500
}
function candidate(value: unknown, expected?: string): PersonalModelCandidate {
  if (
    !object(value) ||
    !id(value.id) ||
    (expected && value.id !== expected) ||
    !text(value.name) ||
    value.status !== 'active' ||
    !time(value.created_at) ||
    !Array.isArray(value.protocols) ||
    value.protocols.some((item) => typeof item !== 'string') ||
    new Set(value.protocols).size !== value.protocols.length ||
    !object(value.input_capabilities) ||
    Object.values(value.input_capabilities).some(
      (items) => !Array.isArray(items) || items.some((item) => !['image', 'pdf'].includes(item)),
    ) ||
    typeof value.personal_granted !== 'boolean' ||
    !(value.pending_request_id === null || id(value.pending_request_id)) ||
    !etag(value.review_etag)
  )
    invalid()
  return value as unknown as PersonalModelCandidate
}
function record(value: unknown, expected?: string, owner?: string): PersonalModelRequest {
  if (
    !object(value) ||
    !id(value.id) ||
    (expected && value.id !== expected) ||
    !uuid(value.request_id) ||
    !id(value.applicant_user_id) ||
    (owner && value.applicant_user_id !== owner) ||
    !text(value.applicant_name) ||
    !id(value.model_id) ||
    !text(value.model_name) ||
    !text(value.reason) ||
    !validPersonalModelReason(value.reason) ||
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
    const d = value.decision
    if (
      !object(d) ||
      !uuid(d.decision_id) ||
      !id(d.actor_id) ||
      !text(d.actor_name) ||
      !actions.includes(String(d.action)) ||
      !text(d.reason) ||
      !validPersonalModelReason(d.reason, d.action === 'reject') ||
      !time(d.decided_at) ||
      (
        { approve: 'approved', reject: 'rejected', withdraw: 'withdrawn' } as Record<string, string>
      )[String(d.action)] !== value.status
    )
      invalid()
  } else if (['approved', 'rejected', 'withdrawn'].includes(String(value.status))) invalid()
  return value as unknown as PersonalModelRequest
}
function application(value: Record<string, unknown>, status: string) {
  if (
    typeof value.current_granted !== 'boolean' ||
    typeof value.runtime_applied !== 'boolean' ||
    !['pending', 'applied', 'superseded'].includes(String(value.application_status)) ||
    value.runtime_applied !== (value.application_status === 'applied') ||
    (value.runtime_applied && !value.current_granted) ||
    (status !== 'approved' && (value.runtime_applied || value.application_status !== 'pending'))
  )
    invalid()
}
function detail(value: unknown, expected?: string, owner?: string): PersonalModelRequestDetail {
  const request = record(value, expected, owner)
  if (
    !object(value) ||
    !etag(value.review_etag) ||
    !Array.isArray(value.allowed_actions) ||
    value.allowed_actions.some((action) => !actions.includes(action)) ||
    new Set(value.allowed_actions).size !== value.allowed_actions.length ||
    (request.status !== 'pending' && value.allowed_actions.length)
  )
    invalid()
  if (
    value.current_model !== null &&
    (!object(value.current_model) ||
      value.current_model.id !== request.model_id ||
      !text(value.current_model.name) ||
      !['active', 'disabled', 'archived'].includes(String(value.current_model.status)))
  )
    invalid()
  application(value, request.status)
  return value as unknown as PersonalModelRequestDetail
}
const scope = (owner?: string) =>
  owner ? `/admin/members/${encodeURIComponent(owner)}/model-requests` : '/personal-model-requests'
export async function listPersonalModelCandidates(
  q: string,
  next: string | null,
  signal?: AbortSignal,
) {
  const value = (
    await client.get<unknown>('/model-access-candidates', {
      params: { q: q || undefined, cursor: next || undefined, limit: 50 },
      signal,
    })
  ).data
  if (
    !object(value) ||
    !Array.isArray(value.items) ||
    value.items.length > 50 ||
    !(value.next_cursor === null || id(value.next_cursor))
  )
    invalid()
  const items = value.items.map((item) => candidate(item))
  if (new Set(items.map((item) => item.id)).size !== items.length) invalid()
  return { items, next_cursor: value.next_cursor as string | null }
}
export async function getPersonalModelCandidate(modelID: string, signal?: AbortSignal) {
  return candidate(
    (
      await client.get<unknown>(`/model-access-candidates/${encodeURIComponent(modelID)}`, {
        signal,
      })
    ).data,
    modelID,
  )
}
export async function listPersonalModelRequests(
  actor: string,
  owner: string | undefined,
  status: PersonalModelRequestStatus | '',
  next: string | null,
  signal?: AbortSignal,
): Promise<PersonalModelRequestPage> {
  const value = (
    await client.get<unknown>(scope(owner), {
      params: { status: status || undefined, cursor: next || undefined, limit: 50 },
      signal,
    })
  ).data
  if (
    !object(value) ||
    !Array.isArray(value.items) ||
    value.items.length > 50 ||
    !integer(value.total) ||
    !cursor(value.next_cursor)
  )
    invalid()
  const items = value.items.map((item) => record(item, undefined, owner ?? actor))
  if (
    new Set(items.map((item) => item.id)).size !== items.length ||
    Number(value.total) < items.length
  )
    invalid()
  return { items, total: value.total as number, next_cursor: value.next_cursor as string | null }
}
export async function getPersonalModelRequest(
  actor: string,
  owner: string | undefined,
  requestID: string,
  signal?: AbortSignal,
) {
  return detail(
    (await client.get<unknown>(`${scope(owner)}/${encodeURIComponent(requestID)}`, { signal }))
      .data,
    requestID,
    owner ?? actor,
  )
}
export async function createPersonalModelRequest(
  actor: string,
  intent: PersonalModelRequestIntent,
  csrf: string,
) {
  const value = detail(
    (
      await client.post<unknown>('/personal-model-requests', intent.body, {
        headers: { 'X-CSRF-Token': csrf, 'If-Match': `"${intent.etag}"` },
      })
    ).data,
    undefined,
    actor,
  )
  if (
    value.request_id !== intent.body.request_id ||
    value.model_id !== intent.body.model_id ||
    value.reason !== intent.body.reason
  )
    invalid()
  return value
}
export async function decidePersonalModelRequest(
  actor: string,
  owner: string | undefined,
  requestID: string,
  intent: PersonalModelDecisionIntent,
  csrf: string,
  signal?: AbortSignal,
): Promise<PersonalModelDecisionReceipt> {
  const value = (
    await client.post<unknown>(
      `${scope(owner)}/${encodeURIComponent(requestID)}/decision`,
      intent.body,
      { signal, headers: { 'X-CSRF-Token': csrf, 'If-Match': `"${intent.etag}"` } },
    )
  ).data
  if (!object(value) || value.committed !== true || value.decision_id !== intent.body.decision_id)
    invalid()
  const saved = record(value.saved_request, requestID, owner ?? actor)
  if (
    !saved.decision ||
    saved.decision.decision_id !== intent.body.decision_id ||
    saved.decision.actor_id !== actor ||
    saved.decision.action !== intent.body.action ||
    saved.decision.reason !== intent.body.reason
  )
    invalid()
  application(value, saved.status)
  return value as unknown as PersonalModelDecisionReceipt
}
export async function getPersonalModelWorkspace(
  owner: string,
  signal?: AbortSignal,
): Promise<PersonalModelWorkspace> {
  const value = (
    await client.get<unknown>(
      `/admin/members/${encodeURIComponent(owner)}/model-access-workspace`,
      { signal },
    )
  ).data
  if (
    !object(value) ||
    value.user_id !== owner ||
    !Array.isArray(value.models) ||
    value.models.length > 1000 ||
    !integer(value.model_count) ||
    value.model_count !== value.models.length ||
    typeof value.can_review_requests !== 'boolean' ||
    value.models.some(
      (model) =>
        !object(model) ||
        !id(model.id) ||
        !text(model.name) ||
        !['active', 'disabled', 'archived'].includes(String(model.status)),
    ) ||
    new Set(value.models.map((model) => model.id)).size !== value.models.length
  )
    invalid()
  return value as unknown as PersonalModelWorkspace
}
