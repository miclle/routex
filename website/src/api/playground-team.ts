import client from './client'
import { readChatResponse } from './playground'
import { GatewayError, record } from './playground-transport'
import { t } from '@/i18n'
import type { ChatRequest, ChatResult, GatewayModel } from '@/types/playground'
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
        model.protocols.length === 1 &&
        model.protocols[0] === 'openai_chat' &&
        model.attachment_scope === 'team' &&
        model.personal_attachments === false &&
        model.attachment_project_id === undefined &&
        Object.keys(record(model.input_capabilities)).length === 1 &&
        Array.isArray(record(model.input_capabilities).openai_chat) &&
        (record(model.input_capabilities).openai_chat as unknown[]).length === 0
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
    !request.messages.every(
      (message) =>
        message &&
        ['system', 'user', 'assistant'].includes(message.role) &&
        typeof message.content === 'string',
    )
  )
    throw new GatewayError(() => t('playground:teamTextOnly'))
  const response = await fetch(`/api/v1/teams/${encodeURIComponent(teamId)}/chat/completions`, {
    method: 'POST',
    credentials: 'same-origin',
    redirect: 'error',
    signal,
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(request),
  })
  if (response.status === 401) window.dispatchEvent(new Event('routex:session-expired'))
  if (!response.ok)
    throw new GatewayError(
      () => t('playground:teamRequestFailed', { status: response.status }),
      response.headers.get('X-Request-ID') ?? '',
      response.status,
    )
  return readChatResponse(response, request, signal, onUpdate)
}
