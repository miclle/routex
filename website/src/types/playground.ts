export type PlaygroundProtocol =
  'openai_chat' | 'openai_responses' | 'anthropic_messages' | 'gemini_generate_content'
export type InputCapability = 'image' | 'pdf'
export type AttachmentScope = 'user' | 'project'
export interface GatewayModel {
  id: string
  attachment_scope?: AttachmentScope
  attachment_project_id?: string
  personal_attachments?: boolean
  protocols?: PlaygroundProtocol[]
  input_capabilities?: Partial<Record<PlaygroundProtocol, InputCapability[]>>
}
export interface ChatMessage {
  role: 'system' | 'user' | 'assistant'
  content: string
}
export interface ChatTextContentBlock {
  type: 'text'
  text: string
}
export interface ChatImageContentBlock {
  type: 'image_url'
  image_url: { url: string }
}
export interface ChatFileContentBlock {
  type: 'file'
  file: { file_data: string; filename: string }
}
export type ChatCurrentTurnContent = (
  ChatTextContentBlock | ChatImageContentBlock | ChatFileContentBlock
)[]
export interface ChatCurrentTurnMessage {
  role: 'user'
  content: ChatCurrentTurnContent
}
export interface ChatUsage {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
}
export interface ChatRequest {
  model: string
  messages: (ChatMessage | ChatCurrentTurnMessage)[]
  stream: boolean
  temperature: number
  top_p: number
  max_completion_tokens: number
  stream_options?: { include_usage: boolean }
}
export interface ChatResult {
  text: string
  requestId: string
  usage: ChatUsage | null
  finishReason: string | null
}

export interface ResponsesHistoryItem {
  role: 'user' | 'assistant'
  content: string
}
export interface ResponsesTextContentBlock {
  type: 'input_text'
  text: string
}
export interface ResponsesImageContentBlock {
  type: 'input_image'
  image_url: string
}
export interface ResponsesFileContentBlock {
  type: 'input_file'
  file_data: string
  filename: string
}
export type ResponsesCurrentTurnContent = (
  ResponsesTextContentBlock | ResponsesImageContentBlock | ResponsesFileContentBlock
)[]
export interface ResponsesCurrentTurnItem {
  role: 'user'
  content: ResponsesCurrentTurnContent
}
export interface ResponsesRequest {
  model: string
  input: (ResponsesHistoryItem | ResponsesCurrentTurnItem)[]
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

export interface MessagesHistoryMessage {
  role: 'user' | 'assistant'
  content: string
}
export interface MessagesTextBlock {
  type: 'text'
  text: string
}
export interface MessagesImageBlock {
  type: 'image'
  source: {
    type: 'base64'
    media_type: 'image/png' | 'image/jpeg'
    data: string
  }
}
export interface MessagesDocumentBlock {
  type: 'document'
  source: {
    type: 'base64'
    media_type: 'application/pdf'
    data: string
  }
}
export type MessagesCurrentTurnContent = (
  MessagesTextBlock | MessagesImageBlock | MessagesDocumentBlock
)[]
export interface MessagesCurrentTurnMessage {
  role: 'user'
  content: MessagesCurrentTurnContent
}
export interface MessagesRequest {
  model: string
  messages: (MessagesHistoryMessage | MessagesCurrentTurnMessage)[]
  system?: string
  stream: boolean
  temperature: number
  top_p: number
  max_tokens: number
}
export interface MessagesResult extends ChatResult {
  messageStatus: 'completed' | 'incomplete' | 'handoff' | 'refused'
  nonTextOutput: boolean
}

export interface GeminiTextPart {
  text: string
}
export interface GeminiInlineDataPart {
  inlineData: {
    mimeType: 'image/png' | 'image/jpeg' | 'application/pdf'
    data: string
  }
}
export type GeminiPart = GeminiTextPart | GeminiInlineDataPart
export interface GeminiRequest {
  model: string
  stream: boolean
  contents: { role: 'user' | 'model'; parts: GeminiPart[] }[]
  systemInstruction?: { parts: { text: string }[] }
  generationConfig: {
    temperature: number
    topP: number
    maxOutputTokens: number
    candidateCount: 1
  }
}
export interface GeminiResult extends ChatResult {
  generationStatus: 'completed' | 'incomplete' | 'handoff' | 'refused'
  nonTextOutput: boolean
}
