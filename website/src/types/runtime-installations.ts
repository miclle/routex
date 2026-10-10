export interface RuntimeInstallationRecord {
  id: string
  projection_version: 1
  instance_id: string
  instance_started_at: string
  snapshot_id: string
  routes_published_at: string
  first_observed_at: string
  instance_status: 'online' | 'offline' | 'unknown'
  current_serving_installation_matches: boolean | null
}

export interface RuntimeInstallationsPage {
  scope: 'single_process_gateway_admission'
  observed_at: string
  items: RuntimeInstallationRecord[]
  next_cursor: string | null
}

export interface RuntimeInstallationsFilter {
  instance_id?: string
  cursor?: string
  limit?: number
}
