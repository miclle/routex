export interface GatewayModel {
  id: string
  protocols?: string[]
}
export interface ChatMessage {
  role: 'system' | 'user' | 'assistant'
  content: string
}
export interface ChatUsage {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
}
export interface ChatRequest {
  model: string
  messages: ChatMessage[]
  stream: boolean
  temperature: number
  top_p: number
  max_tokens: number
  stream_options?: { include_usage: boolean }
}
export interface ChatResult {
  text: string
  requestId: string
  usage: ChatUsage | null
  finishReason: string | null
}

export type PlaygroundProtocol = 'openai_chat' | 'openai_responses'
export interface ResponsesRequest {
  model: string
  input: { role: 'user' | 'assistant'; content: string }[]
  instructions?: string
  stream: boolean
  temperature: number
  top_p: number
  max_output_tokens: number
}
export type ResponseStatus = 'completed' | 'failed' | 'incomplete' | 'queued' | 'in_progress'
export interface ResponsesResult extends ChatResult {
  responseStatus: ResponseStatus | null
  nonTextOutput: boolean
}
