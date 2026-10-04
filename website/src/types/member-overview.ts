import type { MonthlyAccount } from './overview'

export interface MemberOverviewRecord {
  user_id: string
  observed_at: string
  platform_currency: string
  personal: MonthlyAccount
  total_personal_keys: string
}
