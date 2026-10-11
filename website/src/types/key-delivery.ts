import type { PersonalKey } from './catalog'
import type { ProjectKey } from './project-keys'

export type KeyDeliveryMode = 'manual' | 'managed_vault'
export type KeyDeliveryOwner = { kind: 'personal' } | { kind: 'project'; projectId: string }
export interface KeyDeliveryContext {
  allowed_modes: KeyDeliveryMode[]
  default_mode: KeyDeliveryMode
  managed_available: boolean
  failure_code: string | null
  review_etag: string
}
export interface KeyDeliveryPolicy extends KeyDeliveryContext {
  integration_id: string
  writer_identity_id: string
  provisioner_identity_id: string
  template_configured: boolean
}
export interface KeyDeliveryPolicyInput {
  allowed_modes: KeyDeliveryMode[]
  default_mode: KeyDeliveryMode
  integration_id: string
  writer_identity_id: string
  provisioner_identity_id: string
  reason: string
}
export interface KeyDeliveryIdentity {
  id: string
  purpose: 'writer' | 'provisioner'
  configured: boolean
  review_etag: string
}
export interface KeyDeliveryIdentityInput {
  purpose: 'writer' | 'provisioner'
  integration_id: string
  token: string
  reason: string
}
export interface KeyCallerTemplateInput {
  auth_mount: string
  issuer: string
  jwks_url: string
  audience: string
  allowed_subjects: string[]
  token_ttl_seconds: number
  token_max_ttl_seconds: number
  reason: string
}
export interface KeyCallerTemplate extends Omit<KeyCallerTemplateInput, 'reason'> {
  verified: boolean
  review_etag: string
}
export interface KeyDeliveryPrincipal {
  method: 'jwt'
  subject: string
}
export interface KeyDeliveryResolveInput {
  request_id: string
  consumer_id: string
  application_name: string
  environment: string
  principal: KeyDeliveryPrincipal
  reason: string
}
export interface KeyDeliveryTarget {
  id: string
  consumer_id: string
  application_name: string
  environment: string
  status: 'pending' | 'ready' | 'error'
  control_verified: boolean
  isolation: {
    status: 'missing' | 'operator_attested' | 'revoked' | 'stale'
    revision: string | null
    recorded_at: string | null
  }
  failure_code: string | null
  review_etag: string
}
export interface KeyDeliveryTargetSummary extends KeyDeliveryTarget {
  owner_kind: 'personal' | 'project'
  owner_id: string
}
export interface KeyDeliveryTargetPage {
  items: KeyDeliveryTargetSummary[]
  next_cursor: string | null
}
export interface KeyDeliverySecurityTuple {
  profile_id: string
  owner_kind: 'personal' | 'project'
  owner_id: string
  owner_birth: string
  owner_digest: string
  consumer_id: string
  environment: string
  profile_config_revision: string
  profile_security_revision: string
  integration_id: string
  integration_birth: string
  integration_revision: string
  vault_revision_id: string
  vault_revision_birth: string
  infrastructure_security_revision: string
  writer_identity_id: string
  writer_identity_birth: string
  writer_generation: string
  provisioner_identity_id: string
  provisioner_identity_birth: string
  provisioner_generation: string
  template_id: string
  template_birth: string
  template_security_revision: string
  auth_mount: string
  issuer: string
  jwks_url: string
  audience: string
  trust_security_revision: string
  profile_birth: string
  principal_id: string
  principal_birth: string
  principal_security_revision: string
  method: 'jwt'
  subject: string
}
export interface KeyDeliveryTargetReview extends KeyDeliveryTarget {
  security_tuple: KeyDeliverySecurityTuple
}
export interface ManagedKeyInput {
  name: string
  model_ids: string[]
  expires_at: string | null
  request_id: string
  reason: string
  delivery: {
    consumer_id: string
    environment: string
    principal: KeyDeliveryPrincipal
    target_review: string
  }
}
export interface KeyDeliveryDescriptor {
  schema_version: 1
  key_id: string
  profile_id: string
  principal_id: string
  slot_id: string
  descriptor_revision: string
  endpoint: string
  namespace: string
  mount: string
  path: string
  data_field: string
  version: 1
  method: 'jwt'
  auth_mount: string
  role: string
  coordinator: 'not_configured'
  application_status: 'unknown'
}
export interface KeyDeliveryReceipt {
  request_id: string
  key_id: string
  mode: 'managed_vault'
  delivery_status: 'pending' | 'acknowledged' | 'unknown' | 'not_attempted'
  key_status: PersonalKey['status']
  runtime_status: 'pending' | 'confirmed' | 'unknown'
  replacement_call_status: 'not_applicable' | 'not_recorded' | 'completed'
  failure_code: string | null
  descriptor: KeyDeliveryDescriptor | null
  review_etag: string
}
export interface ManagedKeyResult {
  key: (Omit<PersonalKey, 'delivery_mode'> | Omit<ProjectKey, 'delivery_mode'>) & {
    delivery_mode: 'managed_vault'
  }
  delivery: KeyDeliveryReceipt
}
