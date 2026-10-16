import type { Connection } from './catalog'

export interface ArchiveConnectionFields {
  base_url: string
  protocol: Connection['protocol']
  adapter: 'native' | 'azure_openai_classic'
  api_version: string | null
  egress_mode: '' | 'default' | 'direct' | 'proxy'
  egress_id: string | null
}

export interface RuntimeArchiveRollbackPreview {
  scope: 'gateway_routing_configuration'
  archive_id: string
  archive_created_at: string
  observed_at: string
  can_apply: boolean
  blocker_codes: string[]
  connections: {
    connection_id: string
    name: string
    enabled: boolean
    before: ArchiveConnectionFields
    after: ArchiveConnectionFields
    requires_verification: true
  }[]
  declarations: {
    provider_model_id: string
    before: { image_input: boolean; pdf_input: boolean }
    after: { image_input: boolean; pdf_input: boolean }
  }[]
  weights: {
    binding_id: string
    model_id: string
    provider_model_id: string
    protocol: Connection['protocol']
    before: number
    after: number
  }[]
  etag: string
}

export interface RuntimeArchiveRollbackInput {
  request_id: string
  reason: string
}

export interface RuntimePublicationAcknowledgement {
  scope: 'single_process_gateway_admission'
  installation_id: string
  instance_id: string
  instance_started_at: string
  snapshot_id: string
  routes_published_at: string
  acknowledged_at: string
}

export interface RuntimeArchiveRollbackReceipt {
  scope: 'gateway_routing_configuration'
  request_id: string
  archive_id: string
  archive_created_at: string
  applied_at: string
  changed: boolean
  reason: string
  publication: {
    request_id: string
    actor_id: string
    reason: string
    claim_instance_id: string
    claim_instance_started_at: string
    claimed_at: string
    finalized_at: string | null
    status: 'unknown' | 'pending' | 'refresh_returned_nil' | 'refresh_returned_error'
    failure_code: 'runtime_refresh_failed' | null
    acknowledgement: RuntimePublicationAcknowledgement | null
  }
  runtime_applied: boolean
  superseded: boolean
}
