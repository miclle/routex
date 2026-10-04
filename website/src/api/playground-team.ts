import axios from 'axios'
import { AttachmentError } from './attachments'
import type { TeamAttachment } from '@/types/attachments'
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
        (model.attachment_team_id === undefined || model.attachment_team_id === teamId) &&
        (model.attachment_membership_id === undefined ||
          validIdentity(model.attachment_membership_id)) &&
        (model.attachment_team_id === undefined) ===
          (model.attachment_membership_id === undefined) &&
        (model.protocols.every(
          (protocol: string) =>
            (record(model.input_capabilities)[protocol] as unknown[] | undefined)?.length === 0,
        ) ||
          (model.attachment_team_id === teamId && validIdentity(model.attachment_membership_id))) &&
        Object.keys(record(model.input_capabilities)).length === model.protocols.length &&
        model.protocols.every(
          (protocol: string) =>
            Array.isArray(record(model.input_capabilities)[protocol]) &&
            (record(model.input_capabilities)[protocol] as unknown[]).every(
              (value, index, values) =>
                ['image', 'pdf'].includes(String(value)) && values.indexOf(value) === index,
            ),
        )
      )
    }) ||
    new Set(data.map((item) => record(item).id)).size !== data.length ||
    new Set(data.map((item) => record(item).model_id)).size !== data.length ||
    new Set(data.map((item) => record(item).attachment_membership_id)).size > 1
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
        (typeof message.content === 'string' ||
          (message.role === 'user' && mediaContent(message.content, 'openai_chat'))),
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
  if (response.status === 401) await confirmTeamSessionExpiry(signal)
  if (!response.ok)
    throw new GatewayError(
      () => t('playground:teamRequestFailed', { status: response.status }),
      response.headers.get('X-Request-ID') ?? '',
      response.status,
    )
  return response
}

async function confirmTeamSessionExpiry(signal: AbortSignal) {
  if (signal.aborted) return
  const probe = new AbortController()
  const abort = () => probe.abort()
  signal.addEventListener('abort', abort, { once: true })
  const timeout = setTimeout(abort, 5_000)
  try {
    // Native protocols retain upstream authentication errors. Only the current
    // Session endpoint can confirm expiry; its private body is never consumed.
    const response = await fetch('/api/v1/auth/session', {
      method: 'GET',
      credentials: 'same-origin',
      redirect: 'error',
      cache: 'no-store',
      signal: probe.signal,
    })
    void response.body?.cancel().catch(() => {})
    if (response.status === 401 && !signal.aborted && !probe.signal.aborted)
      window.dispatchEvent(new Event('routex:session-expired'))
  } catch {
    // An unavailable or canceled confirmation never replaces the original
    // native error or establishes that the Session has expired.
  } finally {
    clearTimeout(timeout)
    signal.removeEventListener('abort', abort)
  }
}

function managedReference(value: unknown) {
  return (
    typeof value === 'string' &&
    /^routex:\/\/attachments\/obj_[0-7][0-9a-hjkmnp-tv-z]{25}$/.test(value)
  )
}
function exactFields(value: unknown, fields: string[]) {
  const keys = Object.keys(record(value))
  return keys.length === fields.length && keys.every((key) => fields.includes(key))
}
function mediaContent(value: unknown, protocol: PlaygroundProtocol) {
  return (
    Array.isArray(value) &&
    value.length > 0 &&
    value.every((raw) => {
      const block = record(raw)
      if (protocol === 'gemini_generate_content') {
        if (exactFields(block, ['text'])) return typeof block.text === 'string'
        const data = record(block.inlineData)
        return (
          exactFields(block, ['inlineData']) &&
          exactFields(data, ['mimeType', 'data']) &&
          ['image/png', 'image/jpeg', 'application/pdf'].includes(String(data.mimeType)) &&
          managedReference(data.data)
        )
      }
      const textType = protocol === 'openai_responses' ? 'input_text' : 'text'
      if (block.type === textType)
        return exactFields(block, ['type', 'text']) && typeof block.text === 'string'
      if (protocol === 'openai_chat') {
        if (block.type === 'image_url')
          return (
            exactFields(block, ['type', 'image_url']) &&
            exactFields(block.image_url, ['url']) &&
            managedReference(record(block.image_url).url)
          )
        const file = record(block.file)
        return (
          block.type === 'file' &&
          exactFields(block, ['type', 'file']) &&
          exactFields(file, ['file_data', 'filename']) &&
          managedReference(file.file_data) &&
          typeof file.filename === 'string'
        )
      }
      if (protocol === 'openai_responses') {
        if (block.type === 'input_image')
          return exactFields(block, ['type', 'image_url']) && managedReference(block.image_url)
        return (
          block.type === 'input_file' &&
          exactFields(block, ['type', 'file_data', 'filename']) &&
          managedReference(block.file_data) &&
          typeof block.filename === 'string'
        )
      }
      const data = record(block.source)
      return (
        ['image', 'document'].includes(String(block.type)) &&
        exactFields(block, ['type', 'source']) &&
        exactFields(data, ['type', 'media_type', 'data']) &&
        data.type === 'base64' &&
        (block.type === 'document'
          ? data.media_type === 'application/pdf'
          : ['image/png', 'image/jpeg'].includes(String(data.media_type))) &&
        managedReference(data.data)
      )
    })
  )
}

function textItems(items: unknown, protocol: PlaygroundProtocol) {
  return (
    Array.isArray(items) &&
    items.length > 0 &&
    items.every(
      (item) =>
        item &&
        ['user', 'assistant'].includes(record(item).role as string) &&
        (typeof record(item).content === 'string' ||
          (record(item).role === 'user' && mediaContent(record(item).content, protocol))),
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
    !textItems(request.input, 'openai_responses') ||
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
    !textItems(request.messages, 'anthropic_messages') ||
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
      (item) =>
        item &&
        ['user', 'model'].includes(item.role) &&
        (textParts(item.parts) ||
          (item.role === 'user' && mediaContent(item.parts, 'gemini_generate_content'))),
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

function validIdentity(value: unknown): value is string {
  return typeof value === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(value)
}
function validTeamAttachment(value: unknown, teamId: string, id?: string): value is TeamAttachment {
  const item = record(value)
  return (
    managedReference(`routex://attachments/${String(item.id)}`) &&
    (!id || item.id === id) &&
    typeof item.name === 'string' &&
    !!item.name.trim() &&
    ['image/png', 'image/jpeg', 'application/pdf'].includes(String(item.mime)) &&
    typeof item.size === 'number' &&
    Number.isSafeInteger(item.size) &&
    item.size > 0 &&
    ['uploading', 'ready', 'delete_pending', 'deleted'].includes(String(item.state)) &&
    typeof item.created_at === 'string' &&
    Number.isFinite(Date.parse(item.created_at)) &&
    item.attachment_team_id === teamId &&
    validIdentity(item.attachment_membership_id) &&
    validIdentity(item.creator_user_id) &&
    typeof item.expires_at === 'string' &&
    Number.isFinite(Date.parse(item.expires_at))
  )
}
async function teamAttachmentRequest(
  method: 'get' | 'post' | 'delete',
  teamId: string,
  id?: string,
  data?: FormData,
  csrf?: string,
  signal?: AbortSignal,
): Promise<TeamAttachment> {
  if (
    !validIdentity(teamId) ||
    (id !== undefined && !managedReference(`routex://attachments/${id}`)) ||
    (method !== 'get' && !csrf)
  )
    throw new AttachmentError(0)
  try {
    const path = `/teams/${encodeURIComponent(teamId)}/attachments${id ? '/' + encodeURIComponent(id) : ''}`
    const response = await client.request<unknown>({
      method,
      url: path,
      data,
      signal,
      ...(csrf ? { headers: { 'X-CSRF-Token': csrf } } : {}),
    })
    if (
      !validTeamAttachment(response.data, teamId, id) ||
      (method === 'post' && response.data.state !== 'ready')
    )
      throw new AttachmentError(0)
    return response.data
  } catch (error) {
    if (error instanceof AttachmentError) throw error
    throw new AttachmentError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
export function uploadTeamAttachment(
  teamId: string,
  file: File,
  csrf: string,
  signal?: AbortSignal,
) {
  const data = new FormData()
  data.append('file', file, file.name)
  return teamAttachmentRequest('post', teamId, undefined, data, csrf, signal)
}
export function getTeamAttachment(teamId: string, id: string, signal?: AbortSignal) {
  return teamAttachmentRequest('get', teamId, id, undefined, undefined, signal)
}
export function deleteTeamAttachment(
  teamId: string,
  id: string,
  csrf: string,
  signal?: AbortSignal,
) {
  return teamAttachmentRequest('delete', teamId, id, undefined, csrf, signal)
}
