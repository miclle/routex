export type EgressMode = 'default' | 'direct' | 'proxy'
export interface EgressDiagnostic {
  transport_ok: boolean
  api_ok: boolean
  http_status?: number
  duration_ms: number
  stale?: boolean
  stages: { stage: string; status: string; duration_ms?: number; code?: string }[]
}
export interface Egress {
  id: string
  name: string
  kind: 'socks5' | 'https'
  host: string
  port: number
  enabled: boolean
  etag: string
  auth_configured: boolean
  last_checked_at: string | null
  last_diagnostic: EgressDiagnostic | null
  providers: { id: string; name: string }[]
}
export interface EgressInput {
  name: string
  kind: Egress['kind']
  host: string
  port: number
  enabled: boolean
  etag?: string
  auth: { action: 'keep' | 'replace' | 'remove'; username?: string; password?: string }
  test_target_base_url?: string
}
export interface EgressDefault {
  egress_id: string | null
  etag: string
}
export interface ConnectionEgress {
  connection_id: string
  mode: EgressMode
  egress_id: string | null
  etag: string
}
