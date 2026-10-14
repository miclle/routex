export interface DeliveryControllerConfiguration {
  schema_version: 1
  slot_id: string
  descriptor_revision: string
  key_id: string
  configuration_digest: string
  agent_configuration: string
  jwt_file: string
  key_file: string
}
export interface DeliveryControllerReview {
  configuration: DeliveryControllerConfiguration
  review_etag: string
}
export interface DeliverySlotSummary {
  id: string
  birth: string
  profile_id: string
  owner_kind: 'personal' | 'project'
  owner_id: string
  descriptor_revision: string
  configuration_status: 'not_reviewed'
}
export interface DeliveryControllerSummary {
  id: string
  birth: string
  name: string
  slot_id: string
  expires_at: string
  revoked_at: string | null
  lifecycle_status: 'active' | 'expired' | 'revoked'
  configuration_status: 'not_reviewed'
}
export interface DeliveryControllerPage<T> {
  items: T[]
  next_cursor: string
}
export interface DeliveryControllerReport {
  request_id: string
  descriptor_revision: string
  configuration_digest: string
  agent_state: 'unknown' | 'running' | 'failed'
  output_state: 'unknown' | 'written' | 'failed'
  recorded_at: string
  evidence_kind: 'controller_report'
}
export interface DeliveryControllerDetail {
  id: string
  name: string
  slot_id: string
  owner_kind: 'personal' | 'project'
  owner_id: string
  expires_at: string
  revoked_at: string | null
  assigned_descriptor_revision: string
  assigned_configuration_digest: string
  current_configuration: DeliveryControllerConfiguration | null
  configuration_status: 'assigned' | 'review_required' | 'unavailable' | 'revoked' | 'expired'
  last_acknowledgement: DeliveryControllerReport | null
  review_etag: string
}
export interface DeliveryControllerEnrollmentInput {
  request_id: string
  name: string
  expires_at: string
  opt_in: true
  descriptor_revision: string
  configuration_digest: string
  reason: string
}
export interface DeliveryControllerAssignmentInput {
  request_id: string
  opt_in: true
  descriptor_revision: string
  configuration_digest: string
  reason: string
}
export interface DeliveryControllerEnrollmentResult {
  controller: DeliveryControllerDetail
  bearer: string | null
  secret_status: 'display_once' | 'not_recoverable'
}
export interface DeliveryControllerEnrollmentReceipt {
  request_id: string
  intent_digest: string
  enrollment_review: string
  controller: DeliveryControllerSummary
  bearer: null
  secret_status: 'not_recoverable'
}
