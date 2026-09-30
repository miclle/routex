export interface QuotaSettings {
  time_zone: string
  etag: string
  activated: boolean
  coverage_start: string | null
  editable: boolean
}
export interface QuotaSettingsInput {
  time_zone: string
  reason: string
}
