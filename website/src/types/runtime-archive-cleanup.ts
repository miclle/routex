export interface RuntimeArchiveCleanupPreview {
  scope: 'archive_storage_only'
  archive_ids: string[]
  retained_count: number
  encrypted_bytes: number
  selected_count: number
  selected_encrypted_bytes: number
  version_cap: 100
  encrypted_byte_cap: 268435456
  etag: string
}

export interface RuntimeArchiveCleanupInput {
  archive_ids: string[]
  request_id: string
  reason: string
}

export interface RuntimeArchiveCleanupReceipt {
  scope: 'archive_storage_only'
  request_id: string
  archive_ids: string[]
  deleted_count: number
  deleted_encrypted_bytes: number
  completed_at: string
}
