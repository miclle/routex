export interface GatewayModel { id: string }
export interface ChatMessage { role: 'system' | 'user' | 'assistant'; content: string }
export interface ChatUsage { prompt_tokens: number; completion_tokens: number; total_tokens: number }
export interface ChatRequest {
  model: string
  messages: ChatMessage[]
  stream: boolean
  temperature: number
  top_p: number
  max_tokens: number
  stream_options?: { include_usage: boolean }
}
export interface ChatResult { text: string; requestId: string; usage: ChatUsage | null; finishReason: string | null }
