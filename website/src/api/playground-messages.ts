import { t } from '@/i18n'
import type { ChatUsage, MessagesRequest, MessagesResult } from '@/types/playground'
import {
  GatewayError,
  errorMessage,
  maxBufferLength,
  maxOutputLength,
  nativeRequest,
  record,
} from './playground-transport'

function normalizedUsage(raw: Record<string, unknown>): ChatUsage | null {
  const values = [
    'input_tokens',
    'cache_read_input_tokens',
    'cache_creation_input_tokens',
    'output_tokens',
  ].map((field) => raw[field])
  if (
    !values.every((value) => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0)
  )
    return null
  const [input, read, write, output] = values as number[]
  const totalInput = input + read + write,
    total = totalInput + output
  return Number.isSafeInteger(totalInput) && Number.isSafeInteger(total)
    ? { prompt_tokens: totalInput, completion_tokens: output, total_tokens: total }
    : null
}
function completion(reason: string): MessagesResult['messageStatus'] {
  if (reason === 'end_turn' || reason === 'stop_sequence') return 'completed'
  if (reason === 'tool_use' || reason === 'pause_turn') return 'handoff'
  if (reason === 'refusal') return 'refused'
  return 'incomplete'
}
export async function runMessages(
  key: string,
  request: MessagesRequest,
  signal: AbortSignal,
  onUpdate: (value: MessagesResult) => void,
): Promise<MessagesResult> {
  const response = await nativeRequest('/v1/messages', key, signal, request, 'messages')
  let result: MessagesResult = {
    text: '',
    requestId: response.headers.get('X-Request-ID') ?? response.headers.get('request-id') ?? '',
    usage: null,
    finishReason: null,
    messageStatus: 'incomplete',
    nonTextOutput: false,
  }
  onUpdate(result)
  const invalid = () => new GatewayError(() => t('playground:invalidMessages'), result.requestId)
  function publish(text: string) {
    if (text.length > maxOutputLength)
      throw new GatewayError(
        () => t('output_exceeded_this_page_s_2_097_152_4845b'),
        result.requestId,
      )
    result = { ...result, text }
    onUpdate(result)
  }
  if (!request.stream) {
    const payload = record(await response.json())
    if (
      payload.type !== 'message' ||
      !Array.isArray(payload.content) ||
      typeof payload.stop_reason !== 'string' ||
      !payload.stop_reason
    )
      throw invalid()
    const texts: string[] = []
    for (const raw of payload.content) {
      const block = record(raw)
      if (block.type === 'text') {
        if (typeof block.text !== 'string') throw invalid()
        texts.push(block.text)
      } else result.nonTextOutput = true
    }
    result = {
      ...result,
      usage: normalizedUsage(record(payload.usage)),
      finishReason: payload.stop_reason,
      messageStatus: completion(payload.stop_reason),
    }
    publish(texts.join('\n'))
    return result
  }
  if (!response.headers.get('Content-Type')?.includes('text/event-stream') || !response.body)
    throw invalid()
  const blocks = new Map<number, { type: string; text: string; closed: boolean }>()
  let started = false,
    terminal = false,
    reason = '',
    rawUsage: Record<string, unknown> = {},
    previousOutput: unknown
  function text() {
    publish(
      [...blocks.entries()]
        .sort(([a], [b]) => a - b)
        .filter(([, block]) => block.type === 'text')
        .map(([, block]) => block.text)
        .join('\n'),
    )
  }
  function consume(frame: string) {
    const lines = frame.split(/\r?\n/),
      data = lines
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
    const event = record(parsed),
      type = event.type,
      name = lines
        .find((line) => line.startsWith('event:'))
        ?.slice(6)
        .trim()
    if (typeof type !== 'string' || (name && name !== type)) throw invalid()
    if (type === 'error') throw new GatewayError(errorMessage(event, key), result.requestId)
    if (type === 'message_start') {
      const message = record(event.message)
      if (
        started ||
        message.type !== 'message' ||
        !Array.isArray(message.content) ||
        message.content.length
      )
        throw invalid()
      started = true
      rawUsage = record(message.usage)
      previousOutput = rawUsage.output_tokens
      return
    }
    if (type === 'content_block_start') {
      const block = record(event.content_block),
        index = event.index
      if (
        !started ||
        reason ||
        !Number.isSafeInteger(index) ||
        (index as number) < 0 ||
        blocks.has(index as number) ||
        typeof block.type !== 'string'
      )
        throw invalid()
      if (block.type === 'text' && typeof block.text !== 'string') throw invalid()
      blocks.set(index as number, {
        type: block.type,
        text: block.type === 'text' ? (block.text as string) : '',
        closed: false,
      })
      if (block.type !== 'text') result = { ...result, nonTextOutput: true }
      text()
      return
    }
    if (type === 'content_block_delta' || type === 'content_block_stop') {
      const block = blocks.get(event.index as number)
      if (!started || reason || !block || block.closed) throw invalid()
      if (type === 'content_block_stop') {
        block.closed = true
        return
      }
      const delta = record(event.delta)
      if (delta.type === 'text_delta') {
        if (block.type !== 'text' || typeof delta.text !== 'string') throw invalid()
        block.text += delta.text
        text()
      } else if (typeof delta.type !== 'string') throw invalid()
      else result = { ...result, nonTextOutput: true }
      return
    }
    if (type === 'message_delta') {
      if (!started || reason || [...blocks.values()].some((block) => !block.closed)) throw invalid()
      const delta = record(event.delta),
        next = record(event.usage)
      if (delta.stop_reason !== undefined && delta.stop_reason !== null) {
        if (typeof delta.stop_reason !== 'string' || !delta.stop_reason) throw invalid()
        reason = delta.stop_reason
      }
      if (!event.usage || Array.isArray(event.usage) || typeof event.usage !== 'object')
        rawUsage = {}
      else {
        if (Object.hasOwn(next, 'output_tokens')) {
          if (
            typeof previousOutput === 'number' &&
            typeof next.output_tokens === 'number' &&
            next.output_tokens < previousOutput
          )
            throw invalid()
          previousOutput = next.output_tokens
        } else if (reason) next.output_tokens = null
        rawUsage = { ...rawUsage, ...next }
      }
      return
    }
    if (type === 'message_stop') {
      if (!started || !reason || [...blocks.values()].some((block) => !block.closed))
        throw invalid()
      terminal = true
      result = {
        ...result,
        usage: normalizedUsage(rawUsage),
        finishReason: reason,
        messageStatus: completion(reason),
      }
      onUpdate(result)
    }
  }
  const reader = response.body.getReader(),
    decoder = new TextDecoder()
  let buffer = ''
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
