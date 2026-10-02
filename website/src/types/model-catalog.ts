export const modelCatalogProtocols = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_generate_content',
] as const

export type ModelInputCapability = 'image' | 'pdf'

export type ModelAccessSource =
  | {
      type: 'personal'
      team_id: null
      team_name: null
      invocation_supported: true
    }
  | {
      type: 'team'
      team_id: string
      team_name: string
      invocation_supported: false
      invocation_protocols?: string[]
    }

export interface ModelCatalogRecord {
  id: string
  name: string
  status: 'active'
  created_at: string
  protocols: string[]
  input_capabilities: Record<string, ModelInputCapability[]>
  personal_available: boolean
  sources: ModelAccessSource[]
}
