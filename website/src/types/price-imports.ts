import type { PricePage, PriceRate, PriceWrite } from './pricing'
export interface PriceImportError {
  row: number
  column: string
  code: string
  message: string
}
export interface PriceImportChange {
  row: number
  provider_model_id: string
  upstream_name: string
  action: 'added' | 'updated' | 'unchanged'
  before: PriceRate | null
  after: PriceRate
  threshold_before: number
  threshold_after: number
  stops_following: boolean
}
export interface PriceImportPreview {
  etag: string
  preview_digest: string
  valid: boolean
  items: PriceWrite['items']
  changes: PriceImportChange[]
  errors: PriceImportError[]
}
export interface PriceImportCommit {
  preview: PriceImportPreview
  catalogue: PricePage
}
export interface SelectedPriceFile {
  name: string
  csv: string
}
