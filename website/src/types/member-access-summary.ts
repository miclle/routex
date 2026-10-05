export type MemberAccessSection<T> =
  | { status: 'available'; items: T[] }
  | { status: 'not_authorized' | 'overflow' | 'unavailable'; items: null }

export type MemberAccessRole = { id: string; name: string; builtin: boolean }
export type MemberAccessTeam = {
  id: string
  name: string
  status: 'active' | 'disabled' | 'archived'
  membership_status: 'active' | 'disabled'
  membership_role: 'owner' | 'member'
}
export type MemberAccessAuthority = { roles: boolean; teams: boolean }
export type MemberAccessSummary = {
  user_id: string
  observed_at: string
  updated_at: string | null
  identity_role: 'admin' | 'member'
  roles: MemberAccessSection<MemberAccessRole>
  teams: MemberAccessSection<MemberAccessTeam>
}
