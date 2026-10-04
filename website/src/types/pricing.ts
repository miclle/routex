export const metrics = [
  'INPUT_TOKEN',
  'OUTPUT_TOKEN',
  'CACHE_READ_TOKEN',
  'CACHE_WRITE_TOKEN',
  'IMAGE_INPUT',
  'PDF_INPUT',
] as const
export const units = ['1M_TOKEN', '1_IMAGE', '1_PDF'] as const
export const currencies = ['USD', 'CNY', 'EUR', 'GBP', 'JPY', 'HKD', 'SGD'] as const
export type PriceMetric = (typeof metrics)[number]
export type PriceUnit = (typeof units)[number]
export type PriceCurrency = (typeof currencies)[number]
export const metricUnits: Record<PriceMetric, PriceUnit> = {
  INPUT_TOKEN: '1M_TOKEN',
  OUTPUT_TOKEN: '1M_TOKEN',
  CACHE_READ_TOKEN: '1M_TOKEN',
  CACHE_WRITE_TOKEN: '1M_TOKEN',
  IMAGE_INPUT: '1_IMAGE',
  PDF_INPUT: '1_PDF',
}
export function isMediaPriceMetric(metric: PriceMetric) {
  return metric === 'IMAGE_INPUT' || metric === 'PDF_INPUT'
}
export interface PriceRate {
  id?: string
  metric: PriceMetric
  tier: 'base' | 'long_context'
  unit: PriceUnit
  currency: PriceCurrency
  amount: string
  enabled: boolean
}
export interface ModelPrice {
  id: string
  provider_model_id: string
  provider_id: string
  upstream_name: string
  protocol: string
  context_threshold: 0 | 128000 | 200000
  update_source: string
  follow_repository: boolean
  rate_sources?: Record<string, import('./repository-prices').RateSource>
  context_threshold_source?: { kind: 'custom' | 'repository'; source_model_key: string | null }
  rates: PriceRate[]
}
export interface PricePage {
  etag: string
  currency: { platform_currency: PriceCurrency; rates: Record<string, string> }
  items: ModelPrice[]
  next_cursor?: string
}
export interface PriceWrite {
  etag: string
  items: {
    provider_model_id: string
    context_threshold: ModelPrice['context_threshold']
    rates: Omit<PriceRate, 'id'>[]
  }[]
}
