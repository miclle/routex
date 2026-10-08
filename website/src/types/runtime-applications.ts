export interface RoutingApplicationRecord {
  id: string
  instance_id: string
  instance_started_at: string
  snapshot_id: string
  published_at: string
  applied_at: string
  instance_status: 'online' | 'offline' | 'unknown'
  current_serving_snapshot_matches: boolean | null
}

export interface RoutingApplicationsPage {
  scope: 'routing_only'
  observed_at: string
  items: RoutingApplicationRecord[]
  next_cursor: string | null
}

export interface RoutingApplicationsFilter {
  instance_id?: string
  cursor?: string
  limit?: number
}
