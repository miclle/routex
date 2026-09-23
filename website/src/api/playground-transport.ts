import { t } from '@/i18n'
import type { ChatUsage } from '@/types/playground'

export class GatewayError extends Error {
  constructor(
    message: string | (() => string),
    public readonly requestId = '',
    public readonly status = 0,
  ) {
    super(typeof message === 'function' ? message() : message)
    if (typeof message === 'function') Object.defineProperty(this, 'message', { get: message })
  }
}
export const maxBufferLength = 1_048_576
export const maxOutputLength = 2_097_152
export function record(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === 'object' ? (value as Record<string, unknown>) : {}
}
export function usage(value: unknown): ChatUsage | null {
  const data = record(value)
  return ['prompt_tokens', 'completion_tokens', 'total_tokens'].every(
    (name) =>
      typeof data[name] === 'number' && Number.isFinite(data[name]) && (data[name] as number) >= 0,
  )
    ? (data as unknown as ChatUsage)
    : null
}
export function errorMessage(body: unknown, key: string, status = 0) {
  const message = record(record(body).error).message
  if (typeof message === 'string' && message.trim())
    return message.split(key).join('[REDACTED]').slice(0, 500)
  if (status === 401) return () => t('the_api_key_is_invalid_disabled_or_expired_1240a')
  if (status === 403) return () => t('this_key_does_not_have_permission_to_call_8d536')
  if (status === 429) return () => t('the_request_exceeds_a_limit_try_again_later_aeea3')
  return () =>
    status
      ? t('gateway_request_failed_http_value_56d8d', { v0: status })
      : t('the_gateway_returned_an_invalid_response_59eeb')
}
export async function nativeRequest(
  path: string,
  key: string,
  signal: AbortSignal,
  body?: unknown,
  authentication: 'bearer' | 'messages' | 'gemini' = 'bearer',
) {
  const response = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    credentials: 'omit',
    redirect: 'error',
    headers: {
      ...(authentication === 'gemini'
        ? { 'x-goog-api-key': key }
        : authentication === 'messages'
          ? { 'x-api-key': key, 'anthropic-version': '2023-06-01' }
          : { Authorization: `Bearer ${key}` }),
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
  if (!response.ok) {
    const payload: unknown = await response.json().catch(() => null)
    throw new GatewayError(
      errorMessage(payload, key, response.status),
      response.headers.get('X-Request-ID') ?? '',
      response.status,
    )
  }
  return response
}
