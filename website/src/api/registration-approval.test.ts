import { afterEach, expect, it, vi } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from './client'
import {
  decodeRegistrationResult,
  isRegistrationApprovalSummary,
  validateMemberApproval,
  getMemberApproval,
  decideMemberApproval,
  getRegistrationPolicy,
  getRegistrationPolicyReview,
  setRegistrationPolicy,
  validApprovalReason,
} from './registration-approval'
import { memberApprovalReview, approvalETag } from '@/views/governance/member-approval.fixture'
const session = {
  user: { id: 'usr_new', email: 'new@example.invalid', name: 'New', role: 'member' },
  csrf_token: 'transient',
}
afterEach(() => vi.restoreAllMocks())
it('distinguishes exact anonymous pending202 from immediate201 without Session metadata', () => {
  expect(decodeRegistrationResult(202, { kind: 'approval_pending' })).toEqual({
    kind: 'approval_pending',
  })
  expect(decodeRegistrationResult(201, session)).toEqual({ kind: 'session', session })
})
it.each([
  [200, session],
  [202, session],
  [201, { kind: 'approval_pending' }],
  [202, { kind: 'approval_pending', user_id: 'usr_new' }],
  [202, { kind: 'approval_pending', application_id: 'rap_new' }],
  [202, { kind: 'approval_pending', email: 'new@example.invalid' }],
  [202, { kind: 'approval_pending', csrf_token: 'secret' }],
  [202, { kind: 'approval_pending', session }],
  [202, { mfa_required: true, challenge_token: 'secret', expires_at: '2026-10-05T00:00:00Z' }],
  [201, { ...session, user: { ...session.user, password_hash: 'secret' } }],
  [201, { ...session, user: { ...session.user, last_login_at: '2026-10-05T00:00:00Z' } }],
  [201, { ...session, user: { ...session.user, registration_approval: { status: 'approved' } } }],
  [201, { ...session, csrf_token: '' }],
])('rejects non-registration response %j %j', (status, data) =>
  expect(() => decodeRegistrationResult(status as number, data)).toThrow(),
)
it.each(['not_required', 'pending', 'approved', 'rejected', 'unknown'])(
  'requires bounded summary for %s',
  (status) => {
    expect(isRegistrationApprovalSummary({ status, admission_eligible: false })).toBe(true)
    expect(isRegistrationApprovalSummary({ status })).toBe(false)
    expect(
      isRegistrationApprovalSummary({
        status,
        admission_eligible: false,
        application_id: 'rap_private',
      }),
    ).toBe(false)
    expect(isRegistrationApprovalSummary({ status, admission_eligible: true })).toBe(
      ['not_required', 'approved'].includes(status),
    )
  },
)
it('preserves server-owned action booleans and distinguishes approved-disabled published denial', () => {
  const p = memberApprovalReview({ disabled: true, identity_role: 'admin' })
  expect(validateMemberApproval(p, 'usr_target')).toEqual(p)
  p.approval_status = 'approved'
  p.application = {
    ...p.application!,
    state: 'approved',
    decided_at: '2026-10-05T02:00:00Z',
    decision_actor_id: 'usr_admin',
    decision_reason: 'Reviewed identity',
  }
  p.can_approve = false
  p.can_reject = false
  expect(validateMemberApproval(p, 'usr_target').admission_eligible).toBe(false)
  expect(validateMemberApproval(p, 'usr_target').runtime_applied).toBe(true)
})
it.each([
  'missing',
  'extra',
  'foreign',
  'unknown',
  'application alias',
  'pending decision',
  'terminal actions',
  'offboarded approve',
  'ineligible true',
  'impossible date',
  'invalid etag',
])('rejects %s private review', (kind) => {
  const p: Record<string, unknown> = memberApprovalReview() as unknown as Record<string, unknown>
  if (kind === 'missing') delete p.can_reject
  if (kind === 'extra') p.session = session
  if (kind === 'foreign') p.user_id = 'USR_target'
  if (kind === 'unknown') p.approval_status = 'unknown'
  if (kind === 'application alias')
    p.application = { ...(p.application as object), state: 'approved' }
  if (kind === 'pending decision')
    p.application = { ...(p.application as object), decided_at: '2026-10-05T02:00:00Z' }
  if (kind === 'terminal actions') {
    p.approval_status = 'not_required'
    p.application = null
  }
  if (kind === 'offboarded approve') p.offboarded_at = '2026-10-05T02:00:00Z'
  if (kind === 'ineligible true') p.admission_eligible = true
  if (kind === 'impossible date')
    p.application = { ...(p.application as object), created_at: '2026-02-30T00:00:00Z' }
  if (kind === 'invalid etag') p.review_etag = '0'
  expect(() => validateMemberApproval(p, 'usr_target')).toThrow()
})
it('keeps retained offboarded pending reject independently available', () => {
  const p = memberApprovalReview({
    offboarded_at: '2026-10-05T01:00:00+00:00',
    can_approve: false,
    can_reject: true,
  })
  expect(validateMemberApproval(p, 'usr_target')).toEqual(p)
})
it('checks exact target and strong review response headers', async () => {
  const p = memberApprovalReview(),
    signal = new AbortController().signal
  const get = vi.spyOn(client, 'get').mockResolvedValue({
    status: 200,
    data: p,
    headers: new AxiosHeaders({ ETag: `"${approvalETag}"` }),
  })
  expect(await getMemberApproval('usr_target', signal)).toEqual(p)
  expect(get).toHaveBeenCalledWith('/admin/members/usr_target/approval', { signal })
  get.mockResolvedValue({
    status: 200,
    data: p,
    headers: new AxiosHeaders({ ETag: `W/"${approvalETag}"` }),
  })
  await expect(getMemberApproval('usr_target')).rejects.toThrow()
})
it('submits only reviewed decision/reason with exact captured application receipt and current CSRF', async () => {
  const p = {
    confirmation: 'current_account_approval',
    user_id: 'usr_target',
    application_id: 'rap_target',
    decision: 'approved',
    admission_eligible: false,
    runtime_applied: true,
  }
  const patch = vi.spyOn(client, 'patch').mockResolvedValue({ status: 200, data: p })
  expect(
    await decideMemberApproval(
      'usr_target',
      'rap_target',
      approvalETag,
      { decision: 'approve', reason: 'Reviewed disabled administrator' },
      'current',
    ),
  ).toEqual(p)
  expect(patch).toHaveBeenCalledWith(
    '/admin/members/usr_target/approval',
    { decision: 'approve', reason: 'Reviewed disabled administrator' },
    { signal: undefined, headers: { 'If-Match': `"${approvalETag}"`, 'X-CSRF-Token': 'current' } },
  )
})
it.each([
  'extra',
  'foreign user',
  'foreign application',
  'different decision',
  'unpublished',
  'ineligible rejection',
])('rejects %s confirmation', async (kind) => {
  const p: Record<string, unknown> = {
    confirmation: 'current_account_approval',
    user_id: 'usr_target',
    application_id: 'rap_target',
    decision: 'rejected',
    admission_eligible: false,
    runtime_applied: true,
  }
  if (kind === 'extra') p.review_etag = approvalETag
  if (kind === 'foreign user') p.user_id = 'USR_target'
  if (kind === 'foreign application') p.application_id = 'rap_other'
  if (kind === 'different decision') p.decision = 'approved'
  if (kind === 'unpublished') p.runtime_applied = false
  if (kind === 'ineligible rejection') p.admission_eligible = true
  vi.spyOn(client, 'patch').mockResolvedValue({ status: 200, data: p })
  await expect(
    decideMemberApproval(
      'usr_target',
      'rap_target',
      approvalETag,
      { decision: 'reject', reason: 'Reviewed' },
      'current',
    ),
  ).rejects.toThrow()
})
it('keeps public policy facts separate from private complete replacement and matching current confirmation', async () => {
  const policy = { enabled: false, approval_required: true, allowed_email_domains: [] },
    privatePolicy = { ...policy, review_etag: approvalETag }
  const get = vi
    .spyOn(client, 'get')
    .mockResolvedValueOnce({ status: 200, data: policy })
    .mockResolvedValueOnce({
      status: 200,
      data: privatePolicy,
      headers: new AxiosHeaders({ ETag: `"${approvalETag}"` }),
    })
  expect(await getRegistrationPolicy()).toEqual(policy)
  expect(await getRegistrationPolicyReview()).toEqual(privatePolicy)
  expect(get.mock.calls.map((args) => args[0])).toEqual([
    '/auth/registration',
    '/admin/registration',
  ])
  const patch = vi.spyOn(client, 'patch').mockResolvedValue({
    status: 200,
    data: { ...privatePolicy, confirmation: 'current_registration_policy' },
    headers: new AxiosHeaders({ ETag: `"${approvalETag}"` }),
  })
  await setRegistrationPolicy(
    approvalETag,
    { ...policy, reason: 'Close new registration' },
    'current',
  )
  expect(patch).toHaveBeenCalledWith(
    '/admin/registration',
    { ...policy, reason: 'Close new registration' },
    { signal: undefined, headers: { 'If-Match': `"${approvalETag}"`, 'X-CSRF-Token': 'current' } },
  )
})
it.each(['missing approval', 'private public leak', 'numeric', 'weak header', 'mismatched header'])(
  'rejects %s policy',
  async (kind) => {
    const data: Record<string, unknown> = {
      enabled: true,
      approval_required: false,
      allowed_email_domains: [],
    }
    let header = `"${approvalETag}"`
    if (kind === 'missing approval') delete data.approval_required
    if (kind === 'private public leak') data.review_etag = approvalETag
    if (kind === 'numeric') data.enabled = 1
    if (kind === 'weak header' || kind === 'mismatched header') {
      data.review_etag = approvalETag
      header = kind === 'weak header' ? `W/"${approvalETag}"` : `"${'b'.repeat(64)}"`
    }
    vi.spyOn(client, 'get').mockResolvedValue({
      status: 200,
      data,
      headers: new AxiosHeaders({ ETag: header }),
    })
    await expect(
      kind === 'weak header' || kind === 'mismatched header'
        ? getRegistrationPolicyReview()
        : getRegistrationPolicy(),
    ).rejects.toThrow()
  },
)
it.each(['', ' leading', 'trailing ', 'line\nbreak', '😀'.repeat(257)])(
  'rejects invalid reason without request %j',
  async (reason) => {
    const patch = vi.spyOn(client, 'patch')
    expect(validApprovalReason(reason)).toBe(false)
    await expect(
      decideMemberApproval(
        'usr_target',
        'rap_target',
        approvalETag,
        { decision: 'approve', reason },
        'current',
      ),
    ).rejects.toThrow()
    expect(patch).not.toHaveBeenCalled()
  },
)

it('requires mandatory private-free list/detail summaries without changing ordinary Member or Session DTOs', async () => {
  const { validateMemberDetail } = await import('./member-recent-login')
  const { validateMemberListPage } = await import('./member-list')
  const { memberListPage } = await import('@/views/governance/member-list.fixture')
  const list = memberListPage(),
    row = list.items[0]
  const {
    updated_at,
    total_personal_keys,
    personal_policy_stored,
    personal,
    teams,
    handover_plan_recorded,
    ...detail
  } = row
  void updated_at
  void total_personal_keys
  void personal_policy_stored
  void personal
  void teams
  void handover_plan_recorded
  expect(validateMemberDetail(detail, row.id).registration_approval).toEqual(
    row.registration_approval,
  )
  expect(validateMemberListPage(list, 'usr_admin')).toEqual(list)
  const missing = { ...detail } as Record<string, unknown>
  delete missing.registration_approval
  expect(() => validateMemberDetail(missing, row.id)).toThrow()
  const malformed = structuredClone(list)
  delete (malformed.items[0] as unknown as Record<string, unknown>).registration_approval
  expect(() => validateMemberListPage(malformed, 'usr_admin')).toThrow()
  row.registration_approval = { status: 'pending', admission_eligible: false }
  expect(validateMemberListPage(list, 'usr_admin')).toEqual(list)
  row.registration_approval = { status: 'approved', admission_eligible: true }
  row.disabled = true
  expect(() => validateMemberListPage(list, 'usr_admin')).toThrow()
  expect(() =>
    validateMemberDetail(
      {
        ...detail,
        disabled: true,
        registration_approval: { status: 'approved', admission_eligible: true },
      },
      row.id,
    ),
  ).toThrow()
})
