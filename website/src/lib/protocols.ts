import type { CallableModel } from '@/types/catalog'

export function protocolLabel(protocol: string) {
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
