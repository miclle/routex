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
