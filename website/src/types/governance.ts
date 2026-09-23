export interface Member { id: string; email: string; name: string; role: 'admin' | 'member'; disabled: boolean; created_at: string; role_ids: string[] }
export interface PlatformRole { id: string; name: string; builtin: boolean; permissions: string[] }
export interface RoleList { items: PlatformRole[]; available_permissions: string[] }
export interface MemberFilters { q?: string; status?: string; role?: string }
export interface MemberList { items: Member[]; next_cursor: string | null }
