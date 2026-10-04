import type { PriceRate } from './pricing'
export interface RepositoryMapping {
  provider_model_id: string
  source_model_key: string
}
export interface RateSource {
  kind: 'custom' | 'repository'
  source_model_key: string | null
  source_rate_key: string | null
}
export interface RepositoryConfig {
  review_etag: string
  enabled: boolean
  can_write: boolean
  source: {
    id: 'routex-repository'
    digest: string
    model_count: number
    models: {
      key: string
      provider_key: string
      model: string
      protocol: string
      context_threshold: number
    }[]
  }
  mappings: RepositoryMapping[]
  last_attempt_at: string | null
  last_success_at: string | null
  last_result: string | null
}
export interface RepositorySelection {
  mode: 'sync' | 'restore'
  provider_model_ids: string[]
  rate_ids: string[]
}
export interface RepositoryPreview {
  review_etag: string
  source_digest: string
  preview_digest: string
  mode: RepositorySelection['mode']
  valid: boolean
  changes: {
    provider_model_id: string
    rate_id: string | null
    source_model_key: string
    source_rate_key: string
    action: 'added' | 'updated' | 'unchanged' | 'protected_custom'
    before: PriceRate | null
    after: PriceRate | null
    before_source: RateSource
    after_source: RateSource
    threshold_before: number
    threshold_after: number
  }[]
  errors: RepositoryIssue[]
  warnings: RepositoryIssue[]
}
export interface RepositoryIssue {
  provider_model_id: string
  rate_id: string | null
  code: string
  message: string
}
export interface RepositoryResult {
  receipt: {
    request_id: string
    source_digest: string
    mode: 'configure' | 'sync' | 'restore'
    created_at: string
  }
  committed: true
  runtime_applied: boolean
  configuration_applied: boolean
  application_status: 'applied' | 'pending' | 'superseded' | 'unavailable'
  configuration: RepositoryConfig | null
}
export interface RepositoryConfigInput {
  request_id: string
  enabled: boolean
  mappings: RepositoryMapping[]
  reason: string
}
export interface RepositoryApplyInput {
  request_id: string
  preview_digest: string
  selection: RepositorySelection
  reason: string
}
export type RepositoryIntent =
  | { kind: 'configure'; etag: string; sourceDigest: string; input: RepositoryConfigInput }
  | { kind: 'apply'; etag: string; sourceDigest: string; input: RepositoryApplyInput }
export interface RepositoryCandidates {
  items: { provider_model_id: string; upstream_name: string; protocol: string }[]
  next_cursor: string | null
}
