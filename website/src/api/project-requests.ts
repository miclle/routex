import axios from 'axios'
import client from './client'
import type {
  CreateProjectRequest,
  ProjectRequest,
  ProjectRequestCandidate,
  ProjectRequestDecision,
  ProjectRequestPage,
  ProjectRequestStatus,
  ProjectQuotaContext,
  ProjectQuotaRequestDetail,
  ProjectPolicyRequestDetail,
  ProjectPolicyRequest,
  ProjectRequestLimitsContext,
} from '@/types/project-requests'
const projectPath = (id: string) => `/projects/${encodeURIComponent(id)}`
export async function listProjectRequests(
  id: string,
  status: ProjectRequestStatus | '',
  cursor: string | null,
  signal?: AbortSignal,
) {
  const data = (
    await client.get<ProjectRequestPage>(`${projectPath(id)}/requests`, {
      params: { status: status || undefined, cursor: cursor || undefined, limit: 40 },
      signal,
    })
  ).data
  if (
    !data ||
    !Array.isArray(data.items) ||
    data.items.some((item) => !validRequest(item, id)) ||
    (data.next_cursor != null && typeof data.next_cursor !== 'string')
  )
    throw new Error('Invalid Project request history')
  return data
}
function validRequest(item: ProjectRequest, projectId: string) {
  if (
    !item ||
    item.project_id !== projectId ||
    typeof item.id !== 'string' ||
    typeof item.reason !== 'string' ||
    typeof item.applicant_user_id !== 'string' ||
    typeof item.created_at !== 'string' ||
    !['pending', 'approved', 'rejected', 'withdrawn'].includes(item.status)
  )
    return false
  if (item.kind === 'QUOTA' || item.kind === 'RATE_LIMIT')
    return (
      item.id.length > 0 &&
      item.applicant_user_id.length > 0 &&
      item.reason.trim().length > 0 &&
      Number.isFinite(Date.parse(item.created_at)) &&
      typeof item.baseline_policy_etag === 'string' &&
      item.baseline_policy_etag.length > 0 &&
      (item.kind === 'QUOTA'
        ? validQuota(item.baseline_quota) && validQuotaPatch(item.requested_quota)
        : validRateLimit(item.baseline_rate_limit) &&
          validRateLimit(item.requested_rate_limit, true)) &&
      validApplication(item) &&
      (item.status === 'pending'
        ? item.decided_at === null
        : typeof item.decided_at === 'string' &&
          Number.isFinite(Date.parse(item.decided_at)) &&
          typeof item.decision_actor_id === 'string' &&
          item.decision_actor_id.length > 0) &&
      (item.status !== 'approved' ||
        ((item.kind === 'QUOTA'
          ? validQuota(item.approved_quota)
          : validRateLimit(item.approved_rate_limit)) &&
          typeof item.approved_policy_etag === 'string' &&
          item.approved_policy_etag.length > 0))
    )
  return (
    item.kind === 'MODEL_ACCESS' &&
    Array.isArray(item.requested_model_ids) &&
    Array.isArray(item.baseline_model_ids) &&
    [...item.requested_model_ids, ...item.baseline_model_ids].every(
      (value) => typeof value === 'string',
    )
  )
}
export async function projectRequestCandidates(id: string, q: string, signal?: AbortSignal) {
  return (
    await client.get<{ items: ProjectRequestCandidate[] }>(
      `${projectPath(id)}/request-model-candidates`,
      { params: { q }, signal },
    )
  ).data.items
}
export async function createProjectRequest(
  id: string,
  input: CreateProjectRequest,
  csrf: string,
  reviewETag?: string,
) {
  const data = (
    await client.post<ProjectRequest>(`${projectPath(id)}/requests`, input, {
      headers: { 'X-CSRF-Token': csrf, ...(reviewETag ? { 'If-Match': `"${reviewETag}"` } : {}) },
    })
  ).data
  if (input.kind !== 'MODEL_ACCESS' && (data?.kind !== input.kind || !validRequest(data, id)))
    throw new Error('Invalid Project policy request creation receipt')
  return data
}
export async function decideProjectRequest(
  id: string,
  requestId: string,
  input: ProjectRequestDecision,
  csrf: string,
  reviewETag?: string,
) {
  const data = (
    await client.post<ProjectRequest>(
      `${projectPath(id)}/requests/${encodeURIComponent(requestId)}/decision`,
      input,
      {
        headers: { 'X-CSRF-Token': csrf, ...(reviewETag ? { 'If-Match': `"${reviewETag}"` } : {}) },
      },
    )
  ).data
  if (
    (data?.kind === 'QUOTA' || data?.kind === 'RATE_LIMIT') &&
    (!validRequest(data, id) ||
      data.id !== requestId ||
      data.status !==
        ({ approve: 'approved', reject: 'rejected', withdraw: 'withdrawn' } as const)[input.action])
  )
    throw new Error('Invalid Project policy request decision receipt')
  return data
}
export async function projectQuotaContext(id: string, signal?: AbortSignal) {
  const data = (
    await client.get<ProjectQuotaContext>(`${projectPath(id)}/request-quota-context`, { signal })
  ).data
  if (
    !data ||
    data.project_id !== id ||
    typeof data.review_etag !== 'string' ||
    !/^[a-f0-9]{64}$/.test(data.review_etag) ||
    typeof data.policy_etag !== 'string' ||
    !validQuota(data.current_quota) ||
    typeof data.platform_currency !== 'string' ||
    !/^[A-Z]{3}$/.test(data.platform_currency)
  )
    throw new Error('Invalid Project quota context')
  return data
}
export async function projectRequestLimitsContext(id: string, signal?: AbortSignal) {
  const data = (
    await client.get<ProjectRequestLimitsContext>(`${projectPath(id)}/request-limits-context`, {
      signal,
    })
  ).data
  if (
    !data ||
    data.project_id !== id ||
    typeof data.review_etag !== 'string' ||
    !/^[a-f0-9]{64}$/.test(data.review_etag) ||
    typeof data.policy_etag !== 'string' ||
    !data.policy_etag ||
    !validQuota(data.current_quota) ||
    !validRateLimit(data.current_rate_limit) ||
    typeof data.platform_currency !== 'string' ||
    !/^[A-Z]{3}$/.test(data.platform_currency)
  )
    throw new Error('Invalid Project request limits context')
  return data
}
function validRateLimit(rate: unknown, patch = false) {
  if (!rate || typeof rate !== 'object' || Array.isArray(rate)) return false
  const value = rate as Record<string, unknown>
  const keys = ['rpm', 'tpm', 'concurrency']
  if (Object.keys(value).some((key) => !keys.includes(key))) return false
  if (patch && Object.keys(value).length === 0) return false
  return keys.every((key) =>
    patch && !Object.hasOwn(value, key)
      ? true
      : (!patch && value[key] === null) ||
        (typeof value[key] === 'number' && Number.isSafeInteger(value[key]) && value[key] >= 0),
  )
}
function validApplication(data: unknown, required = false) {
  if (!data || typeof data !== 'object') return false
  const value = data as Record<string, unknown>
  if (
    !required &&
    !Object.hasOwn(value, 'runtime_applied') &&
    !Object.hasOwn(value, 'application_status')
  )
    return true
  return (
    value.status === 'approved' &&
    typeof value.runtime_applied === 'boolean' &&
    typeof value.application_status === 'string' &&
    ['pending', 'applied', 'superseded'].includes(value.application_status) &&
    value.runtime_applied === (value.application_status === 'applied')
  )
}
function validQuota(quota: unknown) {
  if (!quota || typeof quota !== 'object') return false
  const value = quota as Record<string, unknown>
  return (
    (value.tokens_month === null ||
      (typeof value.tokens_month === 'number' &&
        Number.isSafeInteger(value.tokens_month) &&
        value.tokens_month >= 0)) &&
    (value.money_month === null ||
      (typeof value.money_month === 'string' && validAmount(value.money_month))) &&
    typeof value.currency === 'string' &&
    (value.money_month === null || /^[A-Z]{3}$/.test(value.currency))
  )
}
function validAmount(value: string) {
  return /^(0|[1-9][0-9]{0,17})(\.[0-9]{1,18})?$/.test(value)
}
function validQuotaPatch(quota: unknown) {
  if (!quota || typeof quota !== 'object' || Array.isArray(quota)) return false
  const value = quota as Record<string, unknown>
  const tokens = value.tokens_month
  const money = value.money_month
  return (
    (tokens === undefined ||
      (typeof tokens === 'number' && Number.isSafeInteger(tokens) && tokens >= 0)) &&
    (money === undefined
      ? value.currency === undefined
      : typeof money === 'string' &&
        validAmount(money) &&
        typeof value.currency === 'string' &&
        /^[A-Z]{3}$/.test(value.currency)) &&
    (tokens !== undefined || money !== undefined)
  )
}
export async function projectQuotaRequestDetail(
  id: string,
  requestId: string,
  signal?: AbortSignal,
) {
  const data = await projectPolicyRequestDetail(id, requestId, 'QUOTA', signal)
  return data as ProjectQuotaRequestDetail
}
export async function projectPolicyRequestDetail(
  id: string,
  requestId: string,
  kind: ProjectPolicyRequest['kind'],
  signal?: AbortSignal,
) {
  const data = (
    await client.get<ProjectPolicyRequestDetail>(
      `${projectPath(id)}/requests/${encodeURIComponent(requestId)}`,
      { signal },
    )
  ).data
  if (
    !data ||
    data.project_id !== id ||
    data.id !== requestId ||
    data.kind !== kind ||
    !validRequest(data, id) ||
    !(data.kind === 'QUOTA'
      ? validQuota(data.current_quota)
      : validRateLimit(data.current_rate_limit)) ||
    typeof data.current_policy_etag !== 'string' ||
    typeof data.platform_currency !== 'string' ||
    !/^[A-Z]{3}$/.test(data.platform_currency) ||
    (data.status === 'pending' &&
      (typeof data.approval_review_etag !== 'string' ||
        !/^[a-f0-9]{64}$/.test(data.approval_review_etag))) ||
    (data.status === 'approved' && !validApplication(data, true))
  )
    throw new Error(
      kind === 'QUOTA'
        ? 'Invalid Project quota request detail'
        : 'Invalid Project request-limit detail',
    )
  return data
}
export function projectRequestError(error: unknown) {
  const status = axios.isAxiosError(error) ? error.response?.status : undefined
  return status === 409
    ? 'conflict'
    : status === 403 || status === 401
      ? 'denied'
      : status === 400
        ? 'invalid'
        : status === 503
          ? 'unavailable'
          : 'failed'
}
