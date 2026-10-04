import type { PriceCurrency } from '@/types/pricing'

export type MemberModelProtocol =
  'openai_chat' | 'openai_responses' | 'anthropic_messages' | 'gemini_generate_content'

export interface MemberModelBaseRate {
  amount: string
  unit: '1M_TOKEN'
  currency: PriceCurrency
}
export type MemberModelPriceCell =
  | { state: 'priced' | 'disabled'; rate: MemberModelBaseRate }
  | { state: 'unauthorized' | 'unavailable' | 'missing' | 'heterogeneous'; rate: null }

export interface MemberModelRow {
  id: string
  name: string
  status: 'active' | 'disabled' | 'archived'
  type: null
  providers: string[] | null
  protocols: MemberModelProtocol[]
  availability: 'ready' | 'unavailable' | 'unknown'
  input_price: MemberModelPriceCell
  output_price: MemberModelPriceCell
  created_at: string
  updated_at: null
  selectable: boolean
}
export type MemberModelsApplication =
  | { runtime_applied: true; application_status: 'applied' }
  | { runtime_applied: false; application_status: 'not_applied' }
  | { runtime_applied: null; application_status: 'unavailable' }

export type MemberModelsWorkspace = MemberModelsApplication & {
  user_id: string
  observed_at: string
  etag: string
  can_edit: boolean
  personal_models: MemberModelRow[]
  available_models: MemberModelRow[]
}
export interface MemberModelsWriteInput {
  model_ids: string[]
  reason: string
}
export interface MemberModelsWriteResult {
  user_id: string
  model_ids: string[]
  etag: string
  runtime_applied: true
  confirmation: 'current_model_grants'
}
