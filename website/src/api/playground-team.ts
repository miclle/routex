import client from './client'
import { readChatResponse, readResponsesResponse } from './playground'
import { readMessagesResponse } from './playground-messages'
import { readGeminiResponse } from './playground-gemini'
import { isGeminiModelName } from '@/lib/protocols'
import { GatewayError, record } from './playground-transport'
import { t } from '@/i18n'
import type {
  ChatRequest,
  ChatResult,
  GatewayModel,
  PlaygroundProtocol,
  ResponsesRequest,
  ResponsesResult,
  MessagesRequest,
  MessagesResult,
  GeminiRequest,
  GeminiResult,
} from '@/types/playground'
import type { ResourceList } from '@/types/resources'

export async function getOwnActiveTeams(cursor: string | null, signal: AbortSignal) {
  const body = (
    await client.get<ResourceList>('/teams', {
      params: { status: 'active', cursor: cursor ?? undefined },
      signal,
    })
  ).data
  if (
    !Array.isArray(body?.items) ||
    !body.items.every(
      (item) =>
        item &&
        typeof item.id === 'string' &&
        typeof item.name === 'string' &&
        item.status === 'active',
    ) ||
    (body.next_cursor !== null &&
      (typeof body.next_cursor !== 'string' || body.next_cursor === cursor))
  )
    throw new GatewayError(() => t('playground:teamInvalidModels'))
  return body
}

export async function getTeamModels(teamId: string, signal: AbortSignal): Promise<GatewayModel[]> {
  const body: unknown = (
    await client.get(`/teams/${encodeURIComponent(teamId)}/inference-models`, { signal })
  ).data
  const data = record(body).data
  if (
    record(body).object !== 'list' ||
    !Array.isArray(data) ||
    !data.every((item) => {
      const model = record(item)
      return (
        typeof model.id === 'string' &&
        !!model.id.trim() &&
        typeof model.model_id === 'string' &&
        !!model.model_id.trim() &&
        Array.isArray(model.protocols) &&
        model.protocols.length > 0 &&
        model.protocols.every(
          (protocol: unknown, index: number, values: unknown[]) =>
            [
              'openai_chat',
              'openai_responses',
              'anthropic_messages',
              'gemini_generate_content',
            ].includes(String(protocol)) && values.indexOf(protocol) === index,
        ) &&
        model.attachment_scope === 'team' &&
        model.personal_attachments === false &&
        model.attachment_project_id === undefined &&
        Object.keys(record(model.input_capabilities)).length === model.protocols.length &&
        model.protocols.every(
          (protocol: string) =>
            Array.isArray(record(model.input_capabilities)[protocol]) &&
            (record(model.input_capabilities)[protocol] as unknown[]).length === 0,
        )
      )
    }) ||
    new Set(data.map((item) => record(item).id)).size !== data.length ||
    new Set(data.map((item) => record(item).model_id)).size !== data.length
  )
    throw new GatewayError(() => t('playground:teamInvalidModels'))
  return data as GatewayModel[]
}

export async function runTeamChat(
  teamId: string,
  csrf: string,
  request: ChatRequest,
  signal: AbortSignal,
  onUpdate: (result: ChatResult) => void,
) {
  if (
    !csrf ||
    !request.model.trim() ||
    !Array.isArray(request.messages) ||
    request.messages.length === 0 ||
    !request.messages.every(
      (message) =>
        message &&
        ['system', 'user', 'assistant'].includes(message.role) &&
        typeof message.content === 'string',
    )
  )
    throw new GatewayError(() => t('playground:teamTextOnly'))
  const response = await teamRequest(teamId, csrf, 'chat/completions', request, signal)
  return readChatResponse(response, request, signal, onUpdate)
}

export function teamInferencePath(
  teamId: string,
  protocol: PlaygroundProtocol,
  model: string,
  stream: boolean,
) {
  const base = `/api/v1/teams/${encodeURIComponent(teamId)}`
  if (protocol === 'gemini_generate_content')
    return isGeminiModelName(model)
      ? `${base}/models/${encodeURIComponent(model)}:${stream ? 'streamGenerateContent?alt=sse' : 'generateContent'}`
      : '—'
  return `${base}/${protocol === 'openai_chat' ? 'chat/completions' : protocol === 'openai_responses' ? 'responses' : 'messages'}`
}

async function teamRequest(
  teamId: string,
  csrf: string,
  action: string,
  body: unknown,
  signal: AbortSignal,
) {
  if (!csrf || !/^[A-Za-z0-9_-]{1,30}$/.test(teamId))
    throw new GatewayError(() => t('playground:teamTextOnly'))
  signal.throwIfAborted()
  const response = await fetch(`/api/v1/teams/${encodeURIComponent(teamId)}/${action}`, {
    method: 'POST',
    credentials: 'same-origin',
    redirect: 'error',
    signal,
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': csrf,
      ...(action === 'messages' ? { 'anthropic-version': '2023-06-01' } : {}),
    },
    body: JSON.stringify(body),
  })
  if (response.status === 401) window.dispatchEvent(new Event('routex:session-expired'))
  if (!response.ok)
    throw new GatewayError(
      () => t('playground:teamRequestFailed', { status: response.status }),
      response.headers.get('X-Request-ID') ?? '',
      response.status,
    )
  return response
}

function textItems(items: unknown) {
  return (
    Array.isArray(items) &&
    items.length > 0 &&
    items.every(
      (item) =>
        item &&
        ['user', 'assistant'].includes(record(item).role as string) &&
        typeof record(item).content === 'string',
    )
  )
}
export async function runTeamResponses(
  teamId: string,
  csrf: string,
  request: ResponsesRequest,
  signal: AbortSignal,
  onUpdate: (result: ResponsesResult) => void,
) {
  if (
    !request.model.trim() ||
    !textItems(request.input) ||
    (request.instructions !== undefined && typeof request.instructions !== 'string')
  )
    throw new GatewayError(() => t('playground:teamTextOnly'))
  return readResponsesResponse(
    await teamRequest(teamId, csrf, 'responses', request, signal),
    request,
    signal,
    onUpdate,
  )
}
export async function runTeamMessages(
  teamId: string,
  csrf: string,
  request: MessagesRequest,
  signal: AbortSignal,
  onUpdate: (result: MessagesResult) => void,
) {
  if (
    !request.model.trim() ||
    !textItems(request.messages) ||
    (request.system !== undefined && typeof request.system !== 'string')
  )
    throw new GatewayError(() => t('playground:teamTextOnly'))
  return readMessagesResponse(
    await teamRequest(teamId, csrf, 'messages', request, signal),
    request,
    signal,
    onUpdate,
  )
}
export async function runTeamGemini(
  teamId: string,
  csrf: string,
  request: GeminiRequest,
  signal: AbortSignal,
  onUpdate: (result: GeminiResult) => void,
) {
  if (!isGeminiModelName(request.model))
    throw new GatewayError(() => t('playground:geminiAliasRequired'))
  const textParts = (parts: unknown) =>
    Array.isArray(parts) &&
    parts.length > 0 &&
    parts.every(
      (part) => part && Object.keys(part).length === 1 && typeof record(part).text === 'string',
    )
  if (
    !Array.isArray(request.contents) ||
    request.contents.length === 0 ||
    !request.contents.every(
      (item) => item && ['user', 'model'].includes(item.role) && textParts(item.parts),
    ) ||
    (request.systemInstruction !== undefined && !textParts(request.systemInstruction.parts)) ||
    request.generationConfig.candidateCount !== 1
  )
    throw new GatewayError(() => t('playground:teamTextOnly'))
  const { model, stream, ...body } = request
  return readGeminiResponse(
    await teamRequest(
      teamId,
      csrf,
      `models/${encodeURIComponent(model)}:${stream ? 'streamGenerateContent?alt=sse' : 'generateContent'}`,
      body,
      signal,
    ),
    request,
    signal,
    onUpdate,
  )
}
