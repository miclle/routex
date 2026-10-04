import { afterEach, expect, it, vi } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from './client'
import {
  deleteTeamAttachment,
  getTeamAttachment,
  uploadTeamAttachment,
  getTeamModels,
  runTeamChat,
  runTeamResponses,
  runTeamMessages,
  runTeamGemini,
} from './playground-team'
import {
  buildChatAttachmentContent,
  buildResponsesAttachmentContent,
  buildMessagesAttachmentContent,
  buildGeminiAttachmentParts,
} from '@/lib/playground-attachments'
import type { TeamAttachment } from '@/types/attachments'
const original = client.defaults.adapter
const attachment: TeamAttachment = {
  id: 'obj_01k6kwwwwwwwwwwwwwwwwwwwww',
  name: 'private.png',
  mime: 'image/png',
  size: 3,
  state: 'ready',
  created_at: '2026-10-04T00:00:00Z',
  expires_at: '2026-10-04T01:00:00Z',
  attachment_team_id: 'tea_one',
  attachment_membership_id: 'tmb_one',
  creator_user_id: 'usr_one',
}
function reply(data: unknown) {
  const adapter = vi.fn(async (config) => ({
    data,
    config,
    status: 200,
    statusText: 'OK',
    headers: new AxiosHeaders(),
  }))
  client.defaults.adapter = adapter
  return adapter
}
afterEach(() => {
  client.defaults.adapter = original
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
it('uploads only through the explicit Team path with transient CSRF, file and cancellation', async () => {
  const adapter = reply(attachment),
    abort = new AbortController(),
    file = new File(['png'], 'private.png', { type: 'image/png' })
  expect(await uploadTeamAttachment('tea_one', file, 'current-csrf', abort.signal)).toEqual(
    attachment,
  )
  const config = adapter.mock.calls[0][0]
  expect(config.url).toBe('/teams/tea_one/attachments')
  expect(config.signal).toBe(abort.signal)
  expect(config.data.get('file').name).toBe(file.name)
  expect(config.headers.get('X-CSRF-Token')).toBe('current-csrf')
  for (const name of ['Authorization', 'x-api-key', 'x-goog-api-key'])
    expect(config.headers.has(name)).toBe(false)
  expect(config.params).toBeUndefined()
})
it('reads and deletes one exact Team object without a Key or context query', async () => {
  const adapter = reply(attachment)
  await getTeamAttachment('tea_one', attachment.id)
  await deleteTeamAttachment('tea_one', attachment.id, 'rotated-csrf')
  expect(adapter.mock.calls.map(([config]) => [config.method, config.url])).toEqual([
    ['get', '/teams/tea_one/attachments/' + attachment.id],
    ['delete', '/teams/tea_one/attachments/' + attachment.id],
  ])
  expect(adapter.mock.calls[0][0].headers.has('X-CSRF-Token')).toBe(false)
  expect(adapter.mock.calls[1][0].headers.get('X-CSRF-Token')).toBe('rotated-csrf')
})
it.each([
  { attachment_team_id: 'tea_other' },
  { attachment_membership_id: '' },
  { creator_user_id: '' },
  { expires_at: 'invalid' },
  { state: 'uploading' },
  { mime: 'audio/mpeg' },
  { size: 0 },
])('rejects unusable upload metadata %j', async (change) => {
  reply({ ...attachment, ...change })
  await expect(
    uploadTeamAttachment('tea_one', new File(['x'], 'private.png'), 'csrf'),
  ).rejects.toThrow()
})
it('rejects metadata for a different exact object', async () => {
  reply({ ...attachment, id: 'obj_other' })
  await expect(getTeamAttachment('tea_one', attachment.id)).rejects.toThrow()
})
it.each(['openai_chat', 'openai_responses', 'anthropic_messages', 'gemini_generate_content'])(
  'accepts coherent Team %s media discovery',
  async (protocol) => {
    const model = {
      id: 'model-one',
      model_id: 'mdl_one',
      protocols: [protocol],
      input_capabilities: { [protocol]: ['image', 'pdf'] },
      attachment_scope: 'team',
      personal_attachments: false,
      attachment_team_id: 'tea_one',
      attachment_membership_id: 'tmb_one',
    }
    reply({ object: 'list', data: [model] })
    expect(await getTeamModels('tea_one', new AbortController().signal)).toEqual([model])
  },
)
it.each([
  { attachment_team_id: 'tea_other' },
  { attachment_membership_id: undefined },
  { input_capabilities: { openai_chat: ['image', 'image'] } },
  { input_capabilities: { openai_chat: ['audio'] } },
])('rejects incoherent capability context %j', async (change) => {
  reply({
    object: 'list',
    data: [
      {
        id: 'model-one',
        model_id: 'mdl_one',
        protocols: ['openai_chat'],
        input_capabilities: { openai_chat: ['image'] },
        attachment_scope: 'team',
        personal_attachments: false,
        attachment_team_id: 'tea_one',
        attachment_membership_id: 'tmb_one',
        ...change,
      },
    ],
  })
  await expect(getTeamModels('tea_one', new AbortController().signal)).rejects.toThrow()
})
const cases = [
  [
    'openai_chat',
    async (item: TeamAttachment) =>
      runTeamChat(
        'tea_one',
        'csrf',
        {
          model: 'model-one',
          messages: [{ role: 'user', content: buildChatAttachmentContent('Describe', [item]) }],
          stream: false,
          temperature: 0.7,
          top_p: 1,
          max_completion_tokens: 2048,
        },
        new AbortController().signal,
        vi.fn(),
      ),
    'chat/completions',
  ],
  [
    'openai_responses',
    async (item: TeamAttachment) =>
      runTeamResponses(
        'tea_one',
        'csrf',
        {
          model: 'model-one',
          input: [{ role: 'user', content: buildResponsesAttachmentContent('Describe', [item]) }],
          stream: false,
          temperature: 0.7,
          top_p: 1,
          max_output_tokens: 2048,
        },
        new AbortController().signal,
        vi.fn(),
      ),
    'responses',
  ],
  [
    'anthropic_messages',
    async (item: TeamAttachment) =>
      runTeamMessages(
        'tea_one',
        'csrf',
        {
          model: 'model-one',
          messages: [{ role: 'user', content: buildMessagesAttachmentContent('Describe', [item]) }],
          stream: false,
          temperature: 0.7,
          top_p: 1,
          max_tokens: 2048,
        },
        new AbortController().signal,
        vi.fn(),
      ),
    'messages',
  ],
  [
    'gemini_generate_content',
    async (item: TeamAttachment) =>
      runTeamGemini(
        'tea_one',
        'csrf',
        {
          model: 'model-one',
          contents: [{ role: 'user', parts: buildGeminiAttachmentParts('Describe', [item]) }],
          stream: false,
          generationConfig: { temperature: 0.7, topP: 1, maxOutputTokens: 2048, candidateCount: 1 },
        },
        new AbortController().signal,
        vi.fn(),
      ),
    'models/model-one:generateContent',
  ],
] as const
it.each(cases)(
  'forwards canonical managed references using only Team %s transport',
  async (_protocol, run, path) => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('{}', { status: 400 }))
    vi.stubGlobal('fetch', fetchMock)
    await expect(run(attachment)).rejects.toThrow()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, config] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/v1/teams/tea_one/' + path)
    expect(config.credentials).toBe('same-origin')
    expect(config.headers['X-CSRF-Token']).toBe('csrf')
    expect(config.body).toContain('routex://attachments/' + attachment.id)
    expect(config.headers.Authorization).toBeUndefined()
  },
)
it.each(cases)(
  'keeps malformed short attachment references out of %s dispatch',
  async (_protocol, run) => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    await expect(run({ ...attachment, id: 'obj_private' })).rejects.toThrow()
    expect(fetchMock).not.toHaveBeenCalled()
  },
)

it.each(cases)(
  'forwards only canonical PDF references through %s native document positions',
  async (_protocol, run) => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('{}', { status: 400 }))
    vi.stubGlobal('fetch', fetchMock)
    await expect(
      run({ ...attachment, name: 'private.pdf', mime: 'application/pdf' }),
    ).rejects.toThrow()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][1].body).toContain('routex://attachments/' + attachment.id)
  },
)
it('rejects a mixed membership generation across one model discovery response', async () => {
  const model = {
    id: 'one',
    model_id: 'mdl_one',
    protocols: ['openai_chat'],
    input_capabilities: { openai_chat: ['image'] },
    attachment_scope: 'team',
    personal_attachments: false,
    attachment_team_id: 'tea_one',
    attachment_membership_id: 'tmb_one',
  }
  reply({
    object: 'list',
    data: [
      model,
      { ...model, id: 'two', model_id: 'mdl_two', attachment_membership_id: 'tmb_rejoined' },
    ],
  })
  await expect(getTeamModels('tea_one', new AbortController().signal)).rejects.toThrow()
})
