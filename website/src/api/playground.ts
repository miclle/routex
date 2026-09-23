export { runGemini } from './playground-gemini'
import { t } from '@/i18n'
import type {
  ChatRequest,
  ChatResult,
  GatewayModel,
  ResponsesRequest,
  ResponsesResult,
  ResponseStatus,
} from '@/types/playground'

import {
  GatewayError,
  maxBufferLength,
  maxOutputLength,
  record,
  usage,
  errorMessage,
  nativeRequest,
} from './playground-transport'
export { GatewayError } from './playground-transport'
export { runMessages } from './playground-messages'

export async function getGatewayModels(key: string, signal: AbortSignal): Promise<GatewayModel[]> {
  const response = await nativeRequest('/v1/models', key, signal)
  const body = record(await response.json())
  if (
    !Array.isArray(body.data) ||
    !body.data.every((item) => {
      const model = record(item)
      return (
        typeof model.id === 'string' &&
        (model.protocols === undefined ||
          (Array.isArray(model.protocols) &&
            model.protocols.every((protocol) => typeof protocol === 'string')))
      )
    })
  )
    throw new GatewayError(
      () => t('the_model_list_format_is_invalid_b03c8'),
      response.headers.get('X-Request-ID') ?? '',
    )
  return body.data as GatewayModel[]
}

export async function runChat(
  key: string,
  request: ChatRequest,
  signal: AbortSignal,
  onUpdate: (result: ChatResult) => void,
): Promise<ChatResult> {
  const response = await nativeRequest('/v1/chat/completions', key, signal, request)
  let result: ChatResult = {
    text: '',
    requestId: response.headers.get('X-Request-ID') ?? '',
    usage: null,
    finishReason: null,
  }
  onUpdate(result)
  function consume(body: unknown, streaming: boolean) {
    const payload = record(body)
    if (payload.error) throw new GatewayError(errorMessage(payload, key), result.requestId)
    const choices = Array.isArray(payload.choices) ? payload.choices : []
    const choice = record(choices.find((item) => record(item).index === 0) ?? choices[0])
    const message = record(streaming ? choice.delta : choice.message)
    const content =
      typeof message.content === 'string'
        ? message.content
        : typeof message.refusal === 'string'
          ? message.refusal
          : ''
    const text = streaming ? result.text + content : content
    if (text.length > maxOutputLength)
      throw new GatewayError(
        () => t('output_exceeded_this_page_s_2_097_152_4845b'),
        result.requestId,
      )
    result = {
      ...result,
      text,
      usage: usage(payload.usage) ?? result.usage,
      finishReason:
        typeof choice.finish_reason === 'string' ? choice.finish_reason : result.finishReason,
    }
    onUpdate(result)
  }
  if (!request.stream) {
    const payload: unknown = await response.json()
    if (!Array.isArray(record(payload).choices))
      throw new GatewayError(
        () => t('the_gateway_returned_an_invalid_chat_response_d03e2'),
        result.requestId,
      )
    consume(payload, false)
    return result
  }
  if (!response.headers.get('Content-Type')?.includes('text/event-stream') || !response.body)
    throw new GatewayError(
      () => t('the_gateway_did_not_return_a_valid_streaming_833c9'),
      result.requestId,
    )
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let completed = false
  function event(block: string) {
    const data = block
      .split(/\r?\n/)
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice(5).replace(/^ /, ''))
      .join('\n')
    if (!data) return
    if (data.trim() === '[DONE]') {
      completed = true
      return
    }
    let payload: unknown
    try {
      payload = JSON.parse(data)
    } catch {
      throw new GatewayError(
        () => t('the_stream_contained_invalid_data_received_content_has_e988e'),
        result.requestId,
      )
    }
    consume(payload, true)
  }
  try {
    while (!completed) {
      const { done, value } = await reader.read()
      buffer += done ? decoder.decode() : decoder.decode(value, { stream: true })
      let boundary = /\r?\n\r?\n/.exec(buffer)
      while (boundary && !completed) {
        if (boundary.index > maxBufferLength)
          throw new GatewayError(
            () => t('a_stream_event_exceeded_the_size_limit_reading_08c0d'),
            result.requestId,
          )
        event(buffer.slice(0, boundary.index))
        buffer = buffer.slice(boundary.index + boundary[0].length)
        boundary = /\r?\n\r?\n/.exec(buffer)
      }
      if (buffer.length > maxBufferLength)
        throw new GatewayError(
          () => t('a_stream_event_exceeded_the_size_limit_reading_08c0d'),
          result.requestId,
        )
      if (done && !completed)
        throw new GatewayError(
          () => t('the_stream_ended_early_received_content_has_been_ca485'),
          result.requestId,
        )
    }
    return result
  } finally {
    await reader.cancel().catch(() => undefined)
    reader.releaseLock()
  }
}

// Responses has its own native terminal events; Chat's [DONE] is never sufficient.
export async function runResponses(
  key: string,
  request: ResponsesRequest,
  signal: AbortSignal,
  onUpdate: (result: ResponsesResult) => void,
): Promise<ResponsesResult> {
  const response = await nativeRequest('/v1/responses', key, signal, request)
  let result: ResponsesResult = {
    text: '',
    requestId: response.headers.get('X-Request-ID') ?? '',
    usage: null,
    finishReason: null,
    responseStatus: null,
    nonTextOutput: false,
  }
  onUpdate(result)
  const parts = new Map<string, { output: number; content: number; text: string }>()
  function invalid() {
    return new GatewayError(() => t('playground:invalidResponse'), result.requestId)
  }
  function publish(text: string) {
    if (text.length > maxOutputLength)
      throw new GatewayError(
        () => t('output_exceeded_this_page_s_2_097_152_4845b'),
        result.requestId,
      )
    result = { ...result, text }
    onUpdate(result)
  }
  function finish(body: unknown, terminal?: string) {
    const payload = record(body)
    const status = payload.status
    if (
      !['completed', 'failed', 'incomplete', 'queued', 'in_progress'].includes(String(status)) ||
      !Array.isArray(payload.output) ||
      (terminal && terminal !== `response.${status}`)
    )
      throw invalid()
    const text: string[] = []
    let nonTextOutput = false
    for (const raw of payload.output) {
      const item = record(raw)
      if (item.type !== 'message') {
        nonTextOutput = true
        continue
      }
      if (!Array.isArray(item.content)) throw invalid()
      for (const rawContent of item.content) {
        const content = record(rawContent)
        if (content.type === 'output_text') {
          if (typeof content.text !== 'string') throw invalid()
          text.push(content.text)
        } else if (content.type === 'refusal') {
          if (typeof content.refusal !== 'string') throw invalid()
          text.push(content.refusal)
        } else nonTextOutput = true
      }
    }
    const nativeUsage = record(payload.usage)
    const final = ['completed', 'failed', 'incomplete'].includes(String(status))
    result = {
      ...result,
      responseStatus: status as ResponseStatus,
      finishReason:
        typeof record(payload.incomplete_details).reason === 'string'
          ? (record(payload.incomplete_details).reason as string)
          : String(status),
      nonTextOutput,
      usage:
        final &&
        ['input_tokens', 'output_tokens', 'total_tokens'].every((field) =>
          Number.isSafeInteger(nativeUsage[field]),
        )
          ? usage({
              prompt_tokens: nativeUsage.input_tokens,
              completion_tokens: nativeUsage.output_tokens,
              total_tokens: nativeUsage.total_tokens,
            })
          : null,
    }
    publish(text.join('\n'))
  }
  if (!request.stream) {
    finish(await response.json())
    return result
  }
  if (!response.headers.get('Content-Type')?.includes('text/event-stream') || !response.body)
    throw new GatewayError(
      () => t('the_gateway_did_not_return_a_valid_streaming_833c9'),
      result.requestId,
    )
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = '',
    terminal = false
  function consume(block: string) {
    const lines = block.split(/\r?\n/)
    const data = lines
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice(5).replace(/^ /, ''))
      .join('\n')
    if (!data) return
    let parsed: unknown
    try {
      parsed = JSON.parse(data)
    } catch {
      throw invalid()
    }
    const event = record(parsed)
    const type = event.type
    const name = lines
      .find((line) => line.startsWith('event:'))
      ?.slice(6)
      .trim()
    if (typeof type !== 'string' || (name && name !== type)) throw invalid()
    if (type === 'error' || event.error)
      throw new GatewayError(
        errorMessage(event.error ? event : { error: event }, key),
        result.requestId,
      )
    if (['response.completed', 'response.failed', 'response.incomplete'].includes(type)) {
      finish(event.response, type)
      terminal = true
      return
    }
    if (type === 'response.output_text.delta' || type === 'response.refusal.delta') {
      if (
        typeof event.delta !== 'string' ||
        !Number.isSafeInteger(event.output_index) ||
        !Number.isSafeInteger(event.content_index) ||
        (event.output_index as number) < 0 ||
        (event.content_index as number) < 0
      )
        throw invalid()
      const partKey = `${event.output_index}:${event.content_index}`
      const previous = parts.get(partKey)
      parts.set(partKey, {
        output: event.output_index as number,
        content: event.content_index as number,
        text: (previous?.text ?? '') + event.delta,
      })
      publish(
        [...parts.values()]
          .sort((a, b) => a.output - b.output || a.content - b.content)
          .map((part) => part.text)
          .join('\n'),
      )
    } else if (!type.startsWith('response.')) throw invalid()
  }
  try {
    while (!terminal) {
      const { done, value } = await reader.read()
      buffer += done ? decoder.decode() : decoder.decode(value, { stream: true })
      let boundary = /\r?\n\r?\n/.exec(buffer)
      while (boundary && !terminal) {
        if (boundary.index > maxBufferLength) throw invalid()
        consume(buffer.slice(0, boundary.index))
        buffer = buffer.slice(boundary.index + boundary[0].length)
        boundary = /\r?\n\r?\n/.exec(buffer)
      }
      if (buffer.length > maxBufferLength) throw invalid()
      if (done && !terminal)
        throw new GatewayError(
          () => t('the_stream_ended_early_received_content_has_been_ca485'),
          result.requestId,
        )
    }
    return result
  } finally {
    await reader.cancel().catch(() => undefined)
    reader.releaseLock()
  }
}
