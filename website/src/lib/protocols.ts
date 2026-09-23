import type { CallableModel } from '@/types/catalog'

export function protocolLabel(protocol: string) {
  if (protocol === 'gemini_generate_content') return 'Gemini Generate Content'
  if (protocol === 'anthropic_messages') return 'Anthropic Messages'
  if (protocol === 'openai_chat') return 'OpenAI Chat'
  if (protocol === 'openai_responses') return 'OpenAI Responses'
  return protocol
}

export function modelProtocols(model: CallableModel) {
  return model.protocols ?? [model.protocol]
}

export function protocolLabels(protocols: string[]) {
  return [...new Set(protocols)].map(protocolLabel).join(', ')
}

export function isGeminiModelName(name: string) {
  return /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(name)
}
