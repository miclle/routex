export interface ProviderModelCapacity {
  provider_model_id: string
  protocol: string
  etag: string
  revision: string
  transport_current: boolean
  configured: boolean
  max_input_tokens: number
  max_output_tokens: number
  evidence: string
  updated_at: string
}

export interface ProviderModelCapacityInput {
  max_input_tokens: number
  max_output_tokens: number
  evidence: string
  reason: string
}
