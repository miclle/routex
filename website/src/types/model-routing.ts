export const routingProtocols = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_generate_content',
] as const
export type RoutingProtocol = (typeof routingProtocols)[number]
export interface RoutingSupply {
  provider_name: string
  connection_name: string
  verification_covered: boolean
  configured_available: boolean
}
export interface RoutingProvider {
  id: string
  name: string
}
export interface RoutingPrice {
  amount: string
  currency: string
  enabled: boolean
}
export interface RoutingPrices {
  input: RoutingPrice[]
  output: RoutingPrice[]
}
export interface RoutingCandidate extends RoutingSupply {
  prices: RoutingPrices | null
  id: string
  provider_id: string
  connection_id: string
  upstream_name: string
  protocol: RoutingProtocol
  selectable: boolean
  review_etag: string
}
export interface RoutingPage<T> {
  items: T[]
  next_cursor: string | null
}
export interface RoutingFilter {
  q?: string
  cursor?: string
  limit?: number
}
export interface RoutingBindingInput {
  provider_model_id: string
  protocol: RoutingProtocol
  review_etag: string
}
