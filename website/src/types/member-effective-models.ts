import type { MemberModelRow, MemberModelProtocol } from './member-models'

export type MemberEffectiveModelSource = {
  kind: 'personal' | 'team'
  team_id: string | null
  team_name: string | null
  protocols: MemberModelProtocol[]
  availability: 'ready' | 'unavailable' | 'unknown'
}
export type MemberEffectiveModelRow = Omit<MemberModelRow, 'selectable'> & {
  sources: MemberEffectiveModelSource[]
}
export type MemberEffectiveModelsPage = {
  user_id: string
  observed_at: string
  subject_status: 'active' | 'disabled' | 'offboarded'
  team_enrichment: 'included' | 'not_authorized'
  union_completeness: 'complete' | 'unknown'
  items: MemberEffectiveModelRow[]
}
export type MemberEffectiveModelsAuthority = {
  teams: boolean
  providers: boolean
  prices: boolean
}
