import type { ChatRequest, ChatResult, ChatUsage, GatewayModel } from '@/types/playground'

export class GatewayError extends Error {
  constructor(message: string, public readonly requestId = '', public readonly status = 0) { super(message) }
}
const maxBufferLength = 1_048_576
const maxOutputLength = 2_097_152
function record(value: unknown): Record<string, unknown> { return value !== null && typeof value === 'object' ? value as Record<string, unknown> : {} }
function usage(value: unknown): ChatUsage | null {
  const data = record(value)
  return ['prompt_tokens', 'completion_tokens', 'total_tokens'].every((name) => typeof data[name] === 'number' && Number.isFinite(data[name]) && (data[name] as number) >= 0)
    ? data as unknown as ChatUsage : null
}
function errorMessage(body: unknown, key: string, status = 0) {
  const message = record(record(body).error).message
  if (typeof message === 'string' && message.trim()) return message.split(key).join('[REDACTED]').slice(0, 500)
  if (status === 401) return 'API Key 无效、未启用或已过期。'
  if (status === 403) return '此 Key 没有调用该模型的权限。'
  if (status === 429) return '请求超过限制，请稍后重试。'
  return status ? `网关请求失败（HTTP ${status}）。` : '网关返回了无效的响应。'
}
async function nativeRequest(path: string, key: string, signal: AbortSignal, body?: unknown) {
  const response = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    credentials: 'omit',
    redirect: 'error',
    headers: { Authorization: `Bearer ${key}`, ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
  if (!response.ok) {
    const payload: unknown = await response.json().catch(() => null)
    throw new GatewayError(errorMessage(payload, key, response.status), response.headers.get('X-Request-ID') ?? '', response.status)
  }
  return response
}
export async function getGatewayModels(key: string, signal: AbortSignal): Promise<GatewayModel[]> {
  const response = await nativeRequest('/v1/models', key, signal)
  const body = record(await response.json())
  if (!Array.isArray(body.data) || !body.data.every((item) => typeof record(item).id === 'string')) throw new GatewayError('模型列表格式无效。', response.headers.get('X-Request-ID') ?? '')
  return body.data as GatewayModel[]
}

export async function runChat(key: string, request: ChatRequest, signal: AbortSignal, onUpdate: (result: ChatResult) => void): Promise<ChatResult> {
  const response = await nativeRequest('/v1/chat/completions', key, signal, request)
  let result: ChatResult = { text: '', requestId: response.headers.get('X-Request-ID') ?? '', usage: null, finishReason: null }
  onUpdate(result)
  function consume(body: unknown, streaming: boolean) {
    const payload = record(body)
    if (payload.error) throw new GatewayError(errorMessage(payload, key), result.requestId)
    const choices = Array.isArray(payload.choices) ? payload.choices : []
    const choice = record(choices.find((item) => record(item).index === 0) ?? choices[0])
    const message = record(streaming ? choice.delta : choice.message)
    const content = typeof message.content === 'string' ? message.content : typeof message.refusal === 'string' ? message.refusal : ''
    const text = streaming ? result.text + content : content
    if (text.length > maxOutputLength) throw new GatewayError('输出超过当前页面的 2,097,152 个字符容量，已停止读取。', result.requestId)
    result = { ...result, text, usage: usage(payload.usage) ?? result.usage, finishReason: typeof choice.finish_reason === 'string' ? choice.finish_reason : result.finishReason }
    onUpdate(result)
  }
  if (!request.stream) {
    const payload: unknown = await response.json()
    if (!Array.isArray(record(payload).choices)) throw new GatewayError('网关返回的聊天响应格式无效。', result.requestId)
    consume(payload, false)
    return result
  }
  if (!response.headers.get('Content-Type')?.includes('text/event-stream') || !response.body) throw new GatewayError('网关没有返回有效的流式响应。', result.requestId)
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let completed = false
  function event(block: string) {
    const data = block.split(/\r?\n/).filter((line) => line.startsWith('data:')).map((line) => line.slice(5).replace(/^ /, '')).join('\n')
    if (!data) return
    if (data.trim() === '[DONE]') { completed = true; return }
    let payload: unknown
    try { payload = JSON.parse(data) } catch { throw new GatewayError('流式响应包含无效的数据，已保留收到的内容。', result.requestId) }
    consume(payload, true)
  }
  try {
    while (!completed) {
      const { done, value } = await reader.read()
      buffer += done ? decoder.decode() : decoder.decode(value, { stream: true })
      let boundary = /\r?\n\r?\n/.exec(buffer)
      while (boundary && !completed) {
        if (boundary.index > maxBufferLength) throw new GatewayError('单个流事件过大，已停止读取。', result.requestId)
        event(buffer.slice(0, boundary.index))
        buffer = buffer.slice(boundary.index + boundary[0].length)
        boundary = /\r?\n\r?\n/.exec(buffer)
      }
      if (buffer.length > maxBufferLength) throw new GatewayError('单个流事件过大，已停止读取。', result.requestId)
      if (done && !completed) throw new GatewayError('响应流提前结束，已保留收到的内容；请重新发送。', result.requestId)
    }
    return result
  } finally {
    await reader.cancel().catch(() => undefined)
    reader.releaseLock()
  }
}
