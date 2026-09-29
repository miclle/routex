export type SystemInstanceStatus = 'online' | 'offline'
export type SystemInstanceRole = 'combined'

export interface SystemResourceSample {
  scope: string
  used: number | null
  total: number | null
  unit: string
  percent: number | null
}

export interface SystemInstanceResources {
  cpu: SystemResourceSample | null
  memory: SystemResourceSample | null
  storage: SystemResourceSample | null
}

export interface SystemInstance {
  id: string
  name: string
  hostname: string
  status: SystemInstanceStatus
  cleanup_eligible: boolean
  role: SystemInstanceRole
  version: string
  commit: string
  build_time: string
  go_version: string
  os: string
  arch: string
  started_at: string
  last_heartbeat_at: string
  heartbeat_revision: number
  stopped_at: string | null
  resources: SystemInstanceResources
}

export interface SystemInstancesPage {
  items: SystemInstance[]
  observed_at: string
  heartbeat_interval_seconds: number
  lease_duration_seconds: number
  cleanup_after_seconds: number
}

export type SystemJobCode = 'runtime_publication' | 'call_record_delivery' | 'storage_cleanup'
export type SystemJobStatus = 'running' | 'completed' | 'failed'

export type SystemJobDetailCode =
  | ''
  | 'published'
  | 'database_unavailable'
  | 'invalid_configuration'
  | 'publication_failed'
  | 'delivered'
  | 'canceled'
  | 'buffer_read_failed'
  | 'invalid_fact'
  | 'identity_mismatch'
  | 'persistence_failed'
  | 'acknowledge_failed'
  | 'cleaned'
  | 'claim_failed'
  | 'delete_failed'
  | 'state_update_failed'
  | 'executor_lost'

export interface SystemJob {
  id: string
  code: SystemJobCode
  status: SystemJobStatus
  progress: number | null
  executor_id: string
  items_total: number
  items_completed: number
  detail_code: SystemJobDetailCode | string
  started_at: string
  updated_at: string
  completed_at: string | null
}

export interface SystemJobsPage {
  items: SystemJob[]
  observed_at: string
}

export interface SystemInstanceCleanupCandidate {
  id: string
  revision: number
}

export interface SystemInstanceCleanupInput {
  instances: SystemInstanceCleanupCandidate[]
}

export interface SystemInstanceCleanupResult {
  cleaned_ids: string[]
  cleaned_count: number
}
