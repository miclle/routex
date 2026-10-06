export interface Member {
  id: string
  email: string
  name: string
  role: 'admin' | 'member'
  disabled: boolean
  offboarded_at?: string | null
  created_at: string
  role_ids: string[]
}
export interface PlatformRole {
  id: string
  name: string
  description?: string
  builtin: boolean
  permissions: string[]
  member_count?: number | null
}
export interface RoleList {
  items: PlatformRole[]
  available_permissions: string[]
}
export interface MemberFilters {
  q?: string
  status?: string
  role?: string
}
export interface MemberList {
  items: Member[]
  next_cursor: string | null
}
