export type ResourceKind = 'teams' | 'projects'
export type ResourceStatus = 'active' | 'disabled' | 'archived'
export interface ResourcePerson {
  id: string
  user_id: string
  name: string
  email: string
  role?: 'owner' | 'member'
  status?: 'active' | 'disabled'
}
export interface ResourceRecord {
  request_workspace_only?: boolean
  resource_limit_workspace_only?: boolean
  id: string
  name: string
  description: string
  status: ResourceStatus
  created_at: string
  model_ids: string[]
  members?: ResourcePerson[]
  managers?: ResourcePerson[]
  creator_id?: string
}
export interface ResourceList {
  items: ResourceRecord[]
  next_cursor: string | null
}
export interface ResourceCandidate {
  id: string
  name: string
  email?: string
}
export interface ResourceFilters {
  q: string
  status: string
}

export interface ProjectCreationInput {
  name: string
  description: string
  manager_ids?: string[]
}

export interface ProjectMonthlyQuota {
  tokens_month: number | null
  money_month: string | null
  currency: string
  platform_currency: string
  policy_etag: string
  usage: null | {
    as_of: string
    time_zone: string
    month_start: string
    month_end: string
    covered: boolean
    tokens_used: string
    tokens_held: string
    tokens_unknown: number
    money_used: Record<string, string>
    money_held: Record<string, string>
    money_unknown: number
  }
}
export interface ProjectOverviewRecord {
  project_id: string
  observed_at: string
  counts: {
    managers: number
    models: number
    active_keys: number | null
    pending_requests: number | null
  }
  last_call_at: string | null
  calls_available: boolean
  monthly_quota: ProjectMonthlyQuota | null
  activities: {
    id: string
    kind:
      | 'project_created'
      | 'project_updated'
      | 'managers_changed'
      | 'models_changed'
      | 'limits_changed'
    created_at: string
    actor_name: string | null
    status: 'committed'
  }[]
}
