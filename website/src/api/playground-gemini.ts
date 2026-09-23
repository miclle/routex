import { t } from '@/i18n'
import { isGeminiModelName } from '@/lib/protocols'
import type { ChatUsage, GeminiRequest, GeminiResult } from '@/types/playground'
import {
  GatewayError,
  errorMessage,
  maxBufferLength,
  maxOutputLength,
  nativeRequest,
  record,
} from './playground-transport'

function field(object: Record<string, unknown>, camel: string, snake: string) {
  return Object.hasOwn(object, camel) && Object.hasOwn(object, snake)
    ? undefined
    : (object[camel] ?? object[snake])
}
function counter(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
}
function normalizedUsage(value: unknown): ChatUsage | null {
  const raw = record(value)
  const input = field(raw, 'promptTokenCount', 'prompt_token_count')
  const candidates = field(raw, 'candidatesTokenCount', 'candidates_token_count')
  const thoughts = field(raw, 'thoughtsTokenCount', 'thoughts_token_count')
  const reported = field(raw, 'totalTokenCount', 'total_token_count')
  if (!counter(input) || !counter(candidates) || !counter(thoughts)) return null
  const output = candidates + thoughts,
    total = input + output
  if (!counter(output) || !counter(total)) return null
  if (
    (Object.hasOwn(raw, 'totalTokenCount') || Object.hasOwn(raw, 'total_token_count')) &&
    (!counter(reported) || reported !== total)
  )
    return null
  return { prompt_tokens: input, completion_tokens: output, total_tokens: total }
}
export async function runGemini(
  key: string,
  request: GeminiRequest,
  signal: AbortSignal,
  onUpdate: (value: GeminiResult) => void,
): Promise<GeminiResult> {
  if (!isGeminiModelName(request.model))
    throw new GatewayError(() => t('playground:geminiAliasRequired'))
  const { model, stream, ...body } = request
  const response = await nativeRequest(
    `/v1beta/models/${encodeURIComponent(model)}:${stream ? 'streamGenerateContent?alt=sse' : 'generateContent'}`,
    key,
    signal,
    body,
    'gemini',
  )
  let result: GeminiResult = {
    text: '',
    requestId: response.headers.get('X-Request-ID') ?? '',
    usage: null,
    finishReason: null,
    generationStatus: 'incomplete',
    nonTextOutput: false,
  }
  onUpdate(result)
  let terminal = false,
    blocked = false,
    seenCandidate = false,
    finalUsage: ChatUsage | null = null,
    tool = false
  const invalid = () => new GatewayError(() => t('playground:invalidGemini'), result.requestId)
  function consume(raw: unknown) {
    const payload = record(raw)
    if (payload.error) throw new GatewayError(errorMessage(payload, key), result.requestId)
    if (payload.candidates !== undefined && !Array.isArray(payload.candidates)) throw invalid()
    const candidates = (payload.candidates ?? []) as unknown[]
    if (candidates.length > 1) throw invalid()
    for (const value of candidates) {
      const candidate = record(value)
      if (terminal || blocked || (candidate.index !== undefined && candidate.index !== 0))
        throw invalid()
      seenCandidate = true
      const content = record(candidate.content)
      if (content.parts !== undefined && !Array.isArray(content.parts)) throw invalid()
      for (const rawPart of (content.parts ?? []) as unknown[]) {
        const part = record(rawPart)
        if (typeof part.text === 'string' && part.thought !== true)
          result = { ...result, text: result.text + part.text }
        else if (part.text !== undefined && typeof part.text !== 'string') throw invalid()
        if (part.thought === true || Object.keys(part).some((name) => name !== 'text'))
          result = { ...result, nonTextOutput: true }
        if (part.functionCall !== undefined || part.function_call !== undefined) tool = true
      }
      if (candidate.finishReason !== undefined) {
        if (typeof candidate.finishReason !== 'string') throw invalid()
        if (candidate.finishReason && candidate.finishReason !== 'FINISH_REASON_UNSPECIFIED') {
          terminal = true
          result = { ...result, finishReason: candidate.finishReason }
        }
      }
    }
    const reason = record(payload.promptFeedback).blockReason
    if (
      !candidates.length &&
      typeof reason === 'string' &&
      reason &&
      reason !== 'BLOCK_REASON_UNSPECIFIED'
    ) {
      if (blocked || seenCandidate) throw invalid()
      terminal = blocked = true
      result = { ...result, finishReason: reason }
    }
    if (Object.hasOwn(payload, 'usageMetadata') || Object.hasOwn(payload, 'usage_metadata')) {
      const next = terminal
        ? normalizedUsage(field(payload, 'usageMetadata', 'usage_metadata'))
        : null
      if (
        finalUsage &&
        next &&
        (next.prompt_tokens < finalUsage.prompt_tokens ||
          next.completion_tokens < finalUsage.completion_tokens)
      )
        throw invalid()
      finalUsage = next
    }
    if (result.text.length > maxOutputLength) throw invalid()
    // Usage and completion stay unpublished until the transport reaches clean EOF.
    onUpdate(result)
  }
  function finish() {
    signal.throwIfAborted()
    if (!terminal) throw invalid()
    const reason = result.finishReason
    result = {
      ...result,
      usage: finalUsage,
      generationStatus: blocked
        ? 'refused'
        : tool
          ? 'handoff'
          : reason === 'STOP'
            ? 'completed'
            : [
                  'SAFETY',
                  'RECITATION',
                  'BLOCKLIST',
                  'PROHIBITED_CONTENT',
                  'SPII',
                  'IMAGE_SAFETY',
                ].includes(reason ?? '')
              ? 'refused'
              : 'incomplete',
    }
    onUpdate(result)
    return result
  }
  if (!stream) {
    consume(await response.json())
    return finish()
  }
  if (!response.headers.get('Content-Type')?.includes('text/event-stream') || !response.body)
    throw invalid()
  const reader = response.body.getReader(),
    decoder = new TextDecoder('utf-8', { fatal: true })
  let buffer = ''
  function frame(raw: string) {
    const data = raw
      .split(/\r?\n/)
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
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw invalid()
    consume(parsed)
  }
  try {
    while (true) {
      signal.throwIfAborted()
      const { done, value } = await reader.read()
      signal.throwIfAborted()
      buffer += done ? decoder.decode() : decoder.decode(value, { stream: true })
      let boundary = /\r?\n\r?\n/.exec(buffer)
      while (boundary) {
        if (boundary.index > maxBufferLength) throw invalid()
        frame(buffer.slice(0, boundary.index))
        buffer = buffer.slice(boundary.index + boundary[0].length)
        boundary = /\r?\n\r?\n/.exec(buffer)
      }
      if (buffer.length > maxBufferLength) throw invalid()
      if (done) {
        if (buffer.trim()) frame(buffer)
        return finish()
      }
    }
  } finally {
    await reader.cancel().catch(() => undefined)
    reader.releaseLock()
  }
}
