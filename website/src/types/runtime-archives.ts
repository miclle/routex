export interface RuntimeArchiveMetadataRecord {
  id: string
  created_at: string
  codec_version: 1
  content_digest: string
  document_bytes: number
}
export interface RuntimeArchiveMetadataPage {
  scope: 'archive_metadata_only'
  observed_at: string
  items: RuntimeArchiveMetadataRecord[]
  next_cursor: string | null
}
export interface RuntimeArchiveMetadataFilter {
  cursor?: string
  limit?: number
}
