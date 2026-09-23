import type { PriceCurrency, PricePage } from './pricing'
export interface CurrencyPage {
  etag: string
  currency: PricePage['currency']
  required_currencies: PriceCurrency[]
}
