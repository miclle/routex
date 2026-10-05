import type { MemberApprovalReview } from '@/types/registration-approval'
export const approvalETag = 'a'.repeat(64)
export function memberApprovalReview(
  overrides: Partial<MemberApprovalReview> = {},
): MemberApprovalReview {
  return {
    user_id: 'usr_target',
    name: 'Target',
    identity_role: 'member',
    disabled: false,
    offboarded_at: null,
    approval_status: 'pending',
    application: {
      id: 'rap_target',
      state: 'pending',
      created_at: '2026-10-05T01:00:00Z',
      decided_at: null,
      decision_actor_id: null,
      decision_reason: null,
    },
    can_approve: true,
    can_reject: true,
    admission_eligible: false,
    runtime_applied: true,
    review_etag: approvalETag,
    ...overrides,
  }
}
