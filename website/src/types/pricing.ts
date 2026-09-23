export const metrics = [
  'INPUT_TOKEN',
  'OUTPUT_TOKEN',
  'CACHE_READ_TOKEN',
  'CACHE_WRITE_TOKEN',
] as const
export const currencies = ['USD', 'CNY', 'EUR', 'GBP', 'JPY', 'HKD', 'SGD'] as const
export type PriceMetric = (typeof metrics)[number]
export type PriceCurrency = (typeof currencies)[number]
export interface PriceRate {
  id?: string
  metric: PriceMetric
  tier: 'base' | 'long_context'
  unit: '1M_TOKEN'
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
