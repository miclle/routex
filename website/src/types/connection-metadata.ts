import type { Connection } from './catalog'

export interface ConnectionMetadata {
  id: string
  provider_id: string
  name: string
  protocol: Connection['protocol']
  base_url: string
  egress_mode: 'default' | 'direct' | 'proxy'
  egress_id: string | null
  etag: string
  can_edit: boolean
}
export interface ConnectionMetadataInput {
  name: string
  reason: string
}
export interface ConnectionMetadataResult {
  connection: ConnectionMetadata
  runtime_applied: true
  changed: boolean
}
