import type { Member } from '@/types/governance'
import type { MemberStateInput, MemberStateRecord, MemberStateResult } from '@/types/member-state'
export const stateReviewETag = 'a'.repeat(64)
export function memberStateFixture(
  target: Pick<Member, 'id' | 'name' | 'role' | 'disabled' | 'offboarded_at'>,
  actor = 'usr_admin',
  role: 'admin' | 'member' = 'admin',
  permissions = ['members.read', 'members.write'],
  etag = stateReviewETag,
): MemberStateRecord {
  const status = target.offboarded_at ? 'offboarded' : target.disabled ? 'disabled' : 'active'
  const write = permissions.includes('members.write')
  return {
    user_id: target.id,
    name: target.name,
    base_role: target.role,
    disabled: target.disabled,
    offboarded_at: target.offboarded_at ?? null,
    status,
    can_change_base_role: write && role === 'admin',
    can_change_status:
      write && (role === 'admin' || (target.role !== 'admin' && target.id !== actor)),
    activation_mode: status === 'active' ? null : status === 'offboarded' ? 'reactivate' : 'enable',
    etag,
    account_access_runtime_applied: true,
  }
}
export function memberStateResultFixture(
  state: MemberStateRecord,
  input: MemberStateInput,
  etag = state.etag,
): MemberStateResult {
  const changed = {
    ...state,
    etag,
    ...('role' in input
      ? { base_role: input.role }
      : { disabled: input.disabled, ...(!input.disabled ? { offboarded_at: null } : {}) }),
  }
  const status = changed.offboarded_at ? 'offboarded' : changed.disabled ? 'disabled' : 'active'
  return {
    ...changed,
    status,
    activation_mode: status === 'active' ? null : status === 'offboarded' ? 'reactivate' : 'enable',
    confirmation: 'current_member_state',
    effect: 'role' in input ? 'current_base_identity' : 'current_account_access',
  }
}
