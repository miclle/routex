import type {
  ModelWeightDetail,
  ModelWeightReview,
  ModelWeightRollbackInput,
  ModelWeightRollbackResult,
} from '@/types/model-weight-history'
export const modelID = 'mdl_history'
export const birth = '2026-10-01T01:02:03.123456Z'
export const versionID = 'mwv_0' + '1'.repeat(25)
export const savedID = 'mwv_0' + '2'.repeat(25)
export const requestID = '12345678-1234-4123-8123-123456789abc'
export const reviewETag = 'a'.repeat(64)
export function detailFixture(): ModelWeightDetail {
  return {
    version: {
      version_id: versionID,
      model_id: modelID,
      model_created_at: birth,
      captured_at: birth,
      source: 'observed_baseline',
      parent_version_id: null,
      rollback_version_id: null,
      actor_id: 'usr_history',
      reason: null,
      binding_count: 2,
      valid_weight_set: true,
    },
    weights: [0, 100].map((weight, i) => ({
      binding_id: 'bnd_' + (i ? 'b' : 'a'),
      binding_created_at: birth,
      provider_model_id: 'pmd_' + (i ? 'B' : 'A'),
      provider_model_created_at: birth,
      connection_id: 'con_recorded',
      connection_created_at: birth,
      provider_id: 'prv_recorded',
      provider_created_at: birth,
      protocol: 'openai_chat',
      weight,
    })),
  }
}
export function reviewFixture(): ModelWeightReview {
  const proposed = detailFixture().weights
  return {
    model_id: modelID,
    version_id: versionID,
    current_version_id: savedID,
    current_weights: proposed.map((x) => ({ ...x, weight: 100 - x.weight })),
    proposed_weights: proposed,
    eligible: true,
    can_rollback: true,
    blocker_codes: [],
    review_etag: reviewETag,
    observed_at: birth,
  }
}
export function resultFixture(
  input: ModelWeightRollbackInput = {
    version_id: versionID,
    request_id: requestID,
    reason: 'Restore reviewed complete set',
  },
): ModelWeightRollbackResult {
  return {
    receipt: {
      ...input,
      model_id: modelID,
      source_version_id: versionID,
      saved_version_id: savedID,
      effect: 'changed',
      created_at: birth,
    },
    application_status: 'applied',
    runtime_applied: true,
  }
}
