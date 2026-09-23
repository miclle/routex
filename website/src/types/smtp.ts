export interface SMTPTest {
  request_id: string
  config_etag: string
  status: 'pending' | 'accepted' | 'failed' | 'unknown'
  code?: string
  duration_ms: number
  stages: { name: string; status: 'passed' | 'failed'; duration_ms: number; code?: string }[]
  started_at: string
  completed_at: string | null
}
export interface SMTPSettings {
  enabled: boolean
  host: string
  port: number
  security: 'STARTTLS' | 'SSL_TLS' | 'NONE'
  auth_configured: boolean
  sender_name: string
  sender_email: string
  reply_to: string
  etag: string
  updated_at: string
  last_test: SMTPTest | null
}
export interface SMTPInput {
  enabled: boolean
  host: string
  port: number
  security: SMTPSettings['security']
  auth: { action: 'keep' | 'replace' | 'remove'; username?: string; password?: string }
  etag: string
}
export interface SMTPSenderInput {
  sender_name: string
  sender_email: string
  reply_to: string
  etag: string
}
export interface SMTPTestInput {
  recipient: string
  request_id: string
  etag: string
}
