import type { ConnectionMetadata } from './connection-metadata'
export interface ConnectionStatus extends ConnectionMetadata {
  enabled: boolean
}
export interface ConnectionStatusInput {
  enabled: boolean
  reason: string
}
export interface ConnectionStatusResult {
  connection: ConnectionStatus
  runtime_applied: true
  changed: boolean
}
