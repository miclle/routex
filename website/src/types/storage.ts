export type StorageAuthAction = 'keep' | 'replace' | 'remove'

export interface StorageAuthInput {
  action: StorageAuthAction
  access_key?: string
  secret_key?: string
}

export interface StorageInput {
  enabled: boolean
  endpoint: string
  region: string
  bucket: string
  prefix: string
  auth: StorageAuthInput
  etag: string
}

export interface StorageRevision {
  id: string
  endpoint: string
  region: string
  bucket: string
  prefix: string
  credentials_configured: boolean
  verified_at: string | null
  created_at: string
}

export interface StorageSettings {
  enabled: boolean
  etag: string
  revision: StorageRevision | null
  revisions: StorageRevision[]
}

export interface StorageStage {
  name: 'put' | 'get' | 'delete'
  status: 'passed' | 'failed'
  duration_ms: number
}

export interface StorageProbe {
  success: boolean
  cleanup_pending: boolean
  stages: StorageStage[]
}

export interface StorageRollbackInput {
  revision_id: string
  etag: string
  enabled: boolean
}
