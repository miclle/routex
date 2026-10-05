import client from './client'
import { AxiosHeaders, type AxiosResponse } from 'axios'
import type { Session, SetupInput } from '@/types/auth'
import type {
  RegistrationApprovalSummary,
  RegistrationPolicy,
  RegistrationPolicyReview,
  RegistrationPolicyInput,
  RegistrationPolicyResult,
  MemberApprovalReview,
  MemberApprovalInput,
  MemberApprovalResult,
} from '@/types/registration-approval'
const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const fields = (v: Record<string, unknown>, names: string[]) =>
  Object.keys(v).length === names.length && names.every((n) => Object.hasOwn(v, n))
const id = (v: unknown): v is string => typeof v === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(v)
const etag = (v: unknown): v is string => typeof v === 'string' && /^[a-f0-9]{64}$/.test(v)
const text = (v: unknown, max: number): v is string =>
  typeof v === 'string' && !/\p{Cs}/u.test(v) && new TextEncoder().encode(v).length <= max
const stamp = (v: unknown): v is string =>
  typeof v === 'string' &&
  /^(?!0000)\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(v) &&
  !/^0001-01-01T00:00:00(?:\.0{1,9})?Z$/.test(v) &&
  Number.isFinite(Date.parse(v)) &&
  new Date(v).toISOString().slice(0, 19) === v.slice(0, 19)
const retained = (v: unknown): v is string =>
  typeof v === 'string' && Number.isFinite(Date.parse(v))
const invalid = () => new Error('Registration approval response unavailable')
export function validApprovalReason(v: string) {
  return !!v && v === v.trim() && text(v, 1024) && !/\p{Cc}/u.test(v)
}
export function isRegistrationApprovalSummary(v: unknown): v is RegistrationApprovalSummary {
  return (
    object(v) &&
    fields(v, ['status', 'admission_eligible']) &&
    ['not_required', 'pending', 'approved', 'rejected', 'unknown'].includes(v.status as string) &&
    typeof v.admission_eligible === 'boolean' &&
    (!v.admission_eligible || v.status === 'not_required' || v.status === 'approved')
  )
}
function header(response: AxiosResponse, name: string) {
  return response.headers instanceof AxiosHeaders
    ? response.headers.get(name)
    : response.headers[name.toLowerCase()]
}
function reviewed(response: AxiosResponse) {
  return (
    response.status === 200 &&
    object(response.data) &&
    etag(response.data.review_etag) &&
    header(response, 'ETag') === `"${response.data.review_etag}"`
  )
}
function policy(v: unknown, admin: boolean, result = false) {
  return (
    object(v) &&
    fields(v, [
      'enabled',
      'approval_required',
      ...(admin ? ['review_etag'] : []),
      ...(result ? ['confirmation'] : []),
    ]) &&
    typeof v.enabled === 'boolean' &&
    typeof v.approval_required === 'boolean' &&
    (!admin || etag(v.review_etag)) &&
    (!result || v.confirmation === 'current_registration_policy')
  )
}
export async function getRegistrationPolicy(signal?: AbortSignal): Promise<RegistrationPolicy> {
  const r = await client.get('/auth/registration', { signal })
  if (signal?.aborted || r.status !== 200 || !policy(r.data, false)) throw invalid()
  return r.data
}
export async function getRegistrationPolicyReview(
  signal?: AbortSignal,
): Promise<RegistrationPolicyReview> {
  const r = await client.get('/admin/registration', { signal })
  if (signal?.aborted || !reviewed(r) || !policy(r.data, true)) throw invalid()
  return r.data
}
export async function setRegistrationPolicy(
  review: string,
  input: RegistrationPolicyInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<RegistrationPolicyResult> {
  if (
    !etag(review) ||
    !csrf ||
    !validApprovalReason(input.reason) ||
    typeof input.enabled !== 'boolean' ||
    typeof input.approval_required !== 'boolean'
  )
    throw invalid()
  const r = await client.patch(
    '/admin/registration',
    { enabled: input.enabled, approval_required: input.approval_required, reason: input.reason },
    { signal, headers: { 'If-Match': `"${review}"`, 'X-CSRF-Token': csrf } },
  )
  if (
    signal?.aborted ||
    !reviewed(r) ||
    !policy(r.data, true, true) ||
    r.data.enabled !== input.enabled ||
    r.data.approval_required !== input.approval_required
  )
    throw invalid()
  return r.data
}
export type RegistrationResult =
  { kind: 'session'; session: Session } | { kind: 'approval_pending' }
export function decodeRegistrationResult(status: number, data: unknown): RegistrationResult {
  if (status === 202 && object(data) && fields(data, ['kind']) && data.kind === 'approval_pending')
    return { kind: 'approval_pending' }
  if (
    status !== 201 ||
    !object(data) ||
    !fields(data, ['user', 'csrf_token']) ||
    !object(data.user) ||
    !fields(data.user, ['id', 'email', 'name', 'role']) ||
    !id(data.user.id) ||
    !text(data.user.email, 254) ||
    !data.user.email ||
    !text(data.user.name, 65536) ||
    !['admin', 'member'].includes(data.user.role as string) ||
    typeof data.csrf_token !== 'string' ||
    !data.csrf_token
  )
    throw invalid()
  return { kind: 'session', session: data as unknown as Session }
}
export async function registerLocal(
  input: SetupInput,
  signal?: AbortSignal,
): Promise<RegistrationResult> {
  const r = await client.post('/auth/register', input, { signal })
  if (signal?.aborted) throw invalid()
  return decodeRegistrationResult(r.status, r.data)
}
function path(target: string) {
  if (!id(target)) throw invalid()
  return `/admin/members/${encodeURIComponent(target)}/approval`
}
export function validateMemberApproval(v: unknown, target: string): MemberApprovalReview {
  const keys = [
    'user_id',
    'name',
    'identity_role',
    'disabled',
    'offboarded_at',
    'approval_status',
    'application',
    'can_approve',
    'can_reject',
    'admission_eligible',
    'runtime_applied',
    'review_etag',
  ]
  if (
    !id(target) ||
    !object(v) ||
    !fields(v, keys) ||
    v.user_id !== target ||
    !text(v.name, 65536) ||
    !['admin', 'member'].includes(v.identity_role as string) ||
    typeof v.disabled !== 'boolean' ||
    !(v.offboarded_at === null || retained(v.offboarded_at)) ||
    !['not_required', 'pending', 'approved', 'rejected'].includes(v.approval_status as string) ||
    !['can_approve', 'can_reject', 'admission_eligible', 'runtime_applied'].every(
      (k) => typeof v[k] === 'boolean',
    ) ||
    !etag(v.review_etag)
  )
    throw invalid()
  if (v.approval_status === 'not_required') {
    if (v.application !== null || v.can_approve || v.can_reject) throw invalid()
  } else {
    const a = v.application
    if (
      !object(a) ||
      !fields(a, [
        'id',
        'state',
        'created_at',
        'decided_at',
        'decision_actor_id',
        'decision_reason',
      ]) ||
      !id(a.id) ||
      a.state !== v.approval_status ||
      !stamp(a.created_at)
    )
      throw invalid()
    if (a.state === 'pending') {
      if (
        a.decided_at !== null ||
        a.decision_actor_id !== null ||
        a.decision_reason !== null ||
        v.admission_eligible ||
        (v.offboarded_at !== null && v.can_approve)
      )
        throw invalid()
    } else if (
      !stamp(a.decided_at) ||
      !id(a.decision_actor_id) ||
      typeof a.decision_reason !== 'string' ||
      !validApprovalReason(a.decision_reason) ||
      v.can_approve ||
      v.can_reject
    )
      throw invalid()
  }
  if (
    v.admission_eligible &&
    (v.disabled ||
      v.offboarded_at !== null ||
      !['approved', 'not_required'].includes(v.approval_status as string))
  )
    throw invalid()
  return v as unknown as MemberApprovalReview
}
export async function getMemberApproval(
  target: string,
  signal?: AbortSignal,
): Promise<MemberApprovalReview> {
  const r = await client.get(path(target), { signal })
  if (signal?.aborted || !reviewed(r)) throw invalid()
  return validateMemberApproval(r.data, target)
}
export async function decideMemberApproval(
  target: string,
  application: string,
  review: string,
  input: MemberApprovalInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<MemberApprovalResult> {
  if (
    !id(application) ||
    !etag(review) ||
    !validApprovalReason(input.reason) ||
    !['approve', 'reject'].includes(input.decision) ||
    !csrf
  )
    throw invalid()
  const r = await client.patch(
    path(target),
    { decision: input.decision, reason: input.reason },
    { signal, headers: { 'If-Match': `"${review}"`, 'X-CSRF-Token': csrf } },
  )
  const v: unknown = r.data
  if (
    signal?.aborted ||
    r.status !== 200 ||
    !object(v) ||
    !fields(v, [
      'confirmation',
      'user_id',
      'application_id',
      'decision',
      'admission_eligible',
      'runtime_applied',
    ]) ||
    v.confirmation !== 'current_account_approval' ||
    v.user_id !== target ||
    v.application_id !== application ||
    v.decision !== (input.decision === 'approve' ? 'approved' : 'rejected') ||
    typeof v.admission_eligible !== 'boolean' ||
    v.runtime_applied !== true ||
    (v.decision === 'rejected' && v.admission_eligible)
  )
    throw invalid()
  return v as unknown as MemberApprovalResult
}
