export interface ProviderModelCapacity {
  provider_model_id: string
  protocol: string
  etag: string
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
