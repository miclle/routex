import type {
  TeamModelCandidate,
  TeamModelRequestDetail,
  TeamModelRequestTeam,
  TeamModelWorkspace,
} from '@/types/team-model-requests'
export const teamModelTime = '2026-10-04T00:00:00Z'
export const teamModelETag = 'a'.repeat(64)
export const teamModelUUID = '11111111-1111-4111-8111-111111111111'
export function teamModelTeam(id = 'tea_zero', membership = 'tmm_first'): TeamModelRequestTeam {
  return {
    id,
    name: id === 'tea_zero' ? 'Zero grants Team' : 'Other Team',
    membership_id: membership,
  }
}
export function teamModelCandidate(team = 'tea_zero', model = 'mdl_model'): TeamModelCandidate {
  return {
    id: model,
    name: 'Model',
    status: 'active',
    created_at: teamModelTime,
    protocols: ['openai_chat'],
    input_capabilities: {},
    team_id: team,
    team_granted: false,
    pending_request: false,
    own_pending_request_id: null,
    review_etag: teamModelETag,
  }
}
export function teamModelDetail(team = 'tea_zero'): TeamModelRequestDetail {
  return {
    id: 'tmr_request',
    request_id: teamModelUUID,
    team_id: team,
    team_name: 'Recorded Team',
    applicant_user_id: 'usr_owner',
    applicant_name: 'Recorded applicant',
    applicant_membership_id: 'tmm_first',
    model_id: 'mdl_model',
    model_name: 'Recorded Model',
    reason: 'Original Team need',
    status: 'pending',
    created_at: teamModelTime,
    updated_at: teamModelTime,
    resolved_at: null,
    cancelled_reason: null,
    decision: null,
    current_team: { id: team, name: 'Current Team', status: 'active' },
    current_model: { id: 'mdl_model', name: 'Model', status: 'active' },
    current_membership_matches: true,
    current_granted: false,
    runtime_applied: false,
    application_status: 'pending',
    review_etag: teamModelETag,
    allowed_actions: ['approve', 'reject', 'withdraw'],
  }
}
export function teamModelWorkspace(team = 'tea_zero'): TeamModelWorkspace {
  return {
    team_id: team,
    name: 'Current Team',
    status: 'active',
    models: [],
    model_count: 0,
    can_review_requests: true,
  }
}
