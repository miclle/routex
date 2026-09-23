export interface ProjectKey {
  id: string
  project_id: string
  creator_id: string
  name: string
  prefix: string
  status: 'pending' | 'active' | 'disabled' | 'revoked'
  model_ids: string[]
  expires_at: string | null
  created_at: string
  replaces_key_id: string | null
  delivery_expires_at: string | null
  delivery_mode: 'manual'
}
export interface ProjectKeyPage {
  items: ProjectKey[]
  next_cursor: string | null
}
export interface ProjectKeyDelivery {
  key: ProjectKey
  secret: string
}
export interface CreateProjectKey {
  name: string
  model_ids: string[]
  expires_at: string | null
  delivery_mode: 'manual'
}
