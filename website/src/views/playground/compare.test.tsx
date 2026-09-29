import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  GatewayError,
  getGatewayModels,
  runChat,
  runResponses,
  runMessages,
  runGemini,
} from '@/api/playground'
import { AttachmentError, deleteAttachment, uploadAttachment } from '@/api/attachments'
import i18n from '@/i18n'
import CompareWorkbench from './compare'
import PlaygroundPage from './index'
vi.mock('@/api/playground', async (original) => ({
  ...(await original<typeof import('@/api/playground')>()),
  getGatewayModels: vi.fn(),
  runChat: vi.fn(),
  runResponses: vi.fn(),
  runMessages: vi.fn(),
  runGemini: vi.fn(),
}))
vi.mock('@/api/attachments', async (original) => ({
  ...(await original<typeof import('@/api/attachments')>()),
  uploadAttachment: vi.fn(),
  deleteAttachment: vi.fn(),
}))
vi.mock('@/hooks/use-auth', () => ({
  useSession: () => ({
    data: {
      user: { id: 'usr_playground', role: 'member' },
      csrf_token: 'csrf-playground',
    },
  }),
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, host: HTMLDivElement
const chatResult = {
  text: 'Chat answer',
  requestId: 'req_chat',
  usage: { prompt_tokens: 2, completion_tokens: 3, total_tokens: 5 },
  finishReason: 'stop',
}
const nativeResult = {
  text: 'Native answer',
  requestId: 'req_native',
  usage: { prompt_tokens: 7, completion_tokens: 8, total_tokens: 15 },
  finishReason: 'completed',
  responseStatus: 'completed' as const,
  nonTextOutput: false,
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  vi.mocked(getGatewayModels).mockResolvedValue([
    { id: 'chat-a', protocols: ['openai_chat'] },
    { id: 'native-b', protocols: ['openai_responses'] },
    { id: 'both-c', protocols: ['openai_chat', 'openai_responses'] },
    { id: 'unavailable', protocols: [] },
  ])
  vi.mocked(runChat).mockResolvedValue(chatResult)
  vi.mocked(runResponses).mockResolvedValue(nativeResult)
  vi.mocked(uploadAttachment).mockImplementation(async (file) => ({
    id: `obj_${file.name.replaceAll(/[^a-z0-9]/gi, '_')}`,
    name: file.name,
    mime: file.type,
    size: file.size,
    state: 'ready',
    created_at: '2026-09-29T12:00:00Z',
  }))
  vi.mocked(deleteAttachment).mockImplementation(async (id) => ({
    id,
    name: 'deleted',
    mime: 'image/png',
    size: 1,
    state: 'delete_pending',
    created_at: '2026-09-29T12:00:00Z',
  }))
  await act(async () => root.render(<CompareWorkbench />))
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.resetAllMocks()
})
function button(label: string) {
  const found = [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
  expect(found, label).toBeDefined()
  return found!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function fill(name: string, value: string) {
  const input = host.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[name="${name}"]`)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      input instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype,
      'value',
    )!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function select(label: string, value: string) {
  const input = host.querySelector<HTMLSelectElement>(`select[aria-label="${label}"]`)!
  await act(async () => {
    input.value = value
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function ready() {
  await fill('comparison_key', 'rx_comparison_only')
  await click('Verify and load models')
}
async function send(text = 'Same prompt') {
  await fill('comparison_prompt', text)
  await act(async () => {
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}
function lane(n: number) {
  return host.querySelector<HTMLElement>(`section[aria-label="Comparison ${n}"]`)!
}
async function chooseFiles(files: File[]) {
  const input = host.querySelector<HTMLInputElement>('input[type="file"]')!
  await act(async () => {
    Object.defineProperty(input, 'files', { configurable: true, value: files })
    input.dispatchEvent(new Event('change', { bubbles: true }))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}
const image = (name = 'diagram.png') => new File(['png'], name, { type: 'image/png' })
const pdf = (name = 'report.pdf') => new File(['%PDF-1\n%%EOF'], name, { type: 'application/pdf' })
describe('model comparison workbench', () => {
  it('requires verification and keeps two to four independent model columns', async () => {
    expect(host.querySelectorAll('section')).toHaveLength(2)
    expect(button('Add comparison').disabled).toBe(true)
    await ready()
    expect(host.querySelector('option[value="unavailable"]')).toBeNull()
    await click('Add comparison')
    await click('Add comparison')
    expect(host.querySelectorAll('section')).toHaveLength(4)
    expect(button('Add comparison').disabled).toBe(true)
    await click('Remove comparison 4')
    await click('Remove comparison 3')
    expect(host.querySelectorAll('section')).toHaveLength(2)
    expect(host.querySelector('[aria-label^="Remove comparison"]')).toBeNull()
  })
  it('dispatches identical captured text to native protocols and isolates successful histories', async () => {
    await ready()
    await send()
    expect(vi.mocked(runChat).mock.calls[0][1]).toMatchObject({
      model: 'chat-a',
      messages: [{ role: 'user', content: 'Same prompt' }],
      max_completion_tokens: 2048,
      stream: true,
      stream_options: { include_usage: true },
    })
    expect(vi.mocked(runResponses).mock.calls[0][1]).toMatchObject({
      model: 'native-b',
      input: [{ role: 'user', content: 'Same prompt' }],
      max_output_tokens: 2048,
      stream: true,
    })
    expect(lane(1).textContent).toContain('req_chat')
    expect(lane(1).textContent).toContain('Total 5 Tokens')
    expect(lane(2).textContent).toContain('req_native')
    expect(lane(2).textContent).toContain('Total 15 Tokens')
    await send('Follow-up')
    expect(vi.mocked(runChat).mock.calls[1][1].messages).toEqual([
      { role: 'user', content: 'Same prompt' },
      { role: 'assistant', content: 'Chat answer' },
      { role: 'user', content: 'Follow-up' },
    ])
    expect(vi.mocked(runResponses).mock.calls[1][1].input).toEqual([
      { role: 'user', content: 'Same prompt' },
      { role: 'assistant', content: 'Native answer' },
      { role: 'user', content: 'Follow-up' },
    ])
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
  })
  it('keeps a failed lane independent and excludes its incomplete output from later history', async () => {
    vi.mocked(runChat).mockRejectedValueOnce(new GatewayError('Rejected', 'req_failure', 429))
    await ready()
    await send()
    expect(lane(1).textContent).toContain('Rejected')
    expect(lane(2).textContent).toContain('Native answer')
    await send('Retry')
    expect(vi.mocked(runChat).mock.calls[1][1].messages).toEqual([
      { role: 'user', content: 'Retry' },
    ])
    expect(vi.mocked(runResponses).mock.calls[1][1].input).toHaveLength(3)
    vi.mocked(runResponses).mockResolvedValueOnce({
      ...nativeResult,
      responseStatus: 'incomplete',
      text: 'Incomplete native',
    })
    await send('Incomplete')
    expect(lane(2).textContent).toContain('Incomplete')
    await send('After incomplete')
    expect(
      vi.mocked(runResponses).mock.calls[3][1].input.map((item) => item.content),
    ).not.toContain('Incomplete native')
  })
  it('prevents double dispatch and stops one stream without aborting its sibling', async () => {
    let firstSignal: AbortSignal | undefined,
      secondSignal: AbortSignal | undefined,
      complete!: () => void
    vi.mocked(runChat).mockImplementation(
      (_key, _request, signal, update) =>
        new Promise((_resolve, reject) => {
          firstSignal = signal
          update({ ...chatResult, text: 'Partial chat', usage: null })
          signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
        }),
    )
    vi.mocked(runResponses).mockImplementation(async (_key, _request, signal, update) => {
      secondSignal = signal
      update({ ...nativeResult, text: 'Partial native', responseStatus: null, usage: null })
      await new Promise<void>((resolve) => {
        complete = resolve
      })
      return nativeResult
    })
    await ready()
    await fill('comparison_prompt', 'Capture once')
    await act(async () => {
      const form = host.querySelector('form')!
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    expect(runChat).toHaveBeenCalledTimes(1)
    expect(runResponses).toHaveBeenCalledTimes(1)
    await click('Stop comparison 1')
    expect(firstSignal?.aborted).toBe(true)
    expect(secondSignal?.aborted).toBe(false)
    expect(lane(1).textContent).toContain('Stopped')
    expect(lane(1).textContent).toContain('Partial chat')
    await act(async () => complete())
    expect(lane(2).textContent).toContain('Native answer')
  })
  it('resets only the changed model/protocol history and clears all secrets on key reset', async () => {
    await ready()
    await send()
    await select('Comparison model 1', 'both-c')
    expect(lane(1).textContent).not.toContain('Chat answer')
    expect(lane(2).textContent).toContain('Native answer')
    await select('Comparison protocol 1', 'openai_responses')
    await send('Native now')
    expect(vi.mocked(runResponses).mock.calls.at(-2)?.[1]).toMatchObject({
      model: 'both-c',
      input: [{ role: 'user', content: 'Native now' }],
    })
    await click('Clear key')
    expect(host.querySelector<HTMLInputElement>('[name="comparison_key"]')!.value).toBe('')
    expect(host.textContent).not.toContain('Native answer')
    expect(button('Send to all models').disabled).toBe(true)
  })
  it('keeps one shared composer picker and gates it by the selected-lane capability intersection', async () => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      {
        id: 'image-and-pdf',
        protocols: ['openai_chat', 'openai_responses'],
        personal_attachments: true,
        input_capabilities: {
          openai_chat: ['image', 'pdf'],
          openai_responses: ['image', 'pdf'],
        },
      },
      {
        id: 'image-only',
        protocols: ['openai_chat'],
        personal_attachments: true,
        input_capabilities: { openai_chat: ['image'] },
      },
      {
        id: 'project-media',
        protocols: ['openai_chat'],
        personal_attachments: false,
        input_capabilities: { openai_chat: ['image', 'pdf'] },
      },
    ])
    await ready()

    expect(host.querySelectorAll('input[type="file"]')).toHaveLength(1)
    expect(host.querySelector('section input[type="file"]')).toBeNull()
    const picker = host.querySelector<HTMLInputElement>('input[type="file"]')!
    expect(picker.disabled).toBe(false)
    expect(picker.accept).toBe('image/png,image/jpeg')

    await chooseFiles([pdf()])
    expect(host.textContent).toContain('Choose a supported PNG, JPEG or PDF')
    expect(uploadAttachment).not.toHaveBeenCalled()
    await chooseFiles([image()])
    expect(host.textContent).toContain('diagram.png')

    await select('Comparison model 2', 'project-media')
    expect(picker.disabled).toBe(true)
    expect(deleteAttachment).toHaveBeenCalledWith('obj_diagram_png', 'csrf-playground')
  })

  it.each([
    [
      'openai_chat',
      [
        { type: 'text', text: 'Shared media' },
        { type: 'image_url', image_url: { url: 'routex://attachments/obj_diagram_png' } },
        {
          type: 'file',
          file: {
            file_data: 'routex://attachments/obj_report_pdf',
            filename: 'report.pdf',
          },
        },
      ],
    ],
    [
      'openai_responses',
      [
        { type: 'input_text', text: 'Shared media' },
        { type: 'input_image', image_url: 'routex://attachments/obj_diagram_png' },
        {
          type: 'input_file',
          file_data: 'routex://attachments/obj_report_pdf',
          filename: 'report.pdf',
        },
      ],
    ],
    [
      'anthropic_messages',
      [
        { type: 'text', text: 'Shared media' },
        {
          type: 'image',
          source: {
            type: 'base64',
            media_type: 'image/png',
            data: 'routex://attachments/obj_diagram_png',
          },
        },
        {
          type: 'document',
          source: {
            type: 'base64',
            media_type: 'application/pdf',
            data: 'routex://attachments/obj_report_pdf',
          },
        },
      ],
    ],
    [
      'gemini_generate_content',
      [
        { text: 'Shared media' },
        {
          inlineData: {
            mimeType: 'image/png',
            data: 'routex://attachments/obj_diagram_png',
          },
        },
        {
          inlineData: {
            mimeType: 'application/pdf',
            data: 'routex://attachments/obj_report_pdf',
          },
        },
      ],
    ],
  ] as const)(
    'sends the shared attachment draft to every %s lane with native payloads',
    async (protocol, expectedContent) => {
      const firstModel = protocol === 'gemini_generate_content' ? 'gemini-media-a' : 'media-a'
      const secondModel = protocol === 'gemini_generate_content' ? 'gemini-media-b' : 'media-b'
      vi.mocked(getGatewayModels).mockResolvedValue([
        {
          id: firstModel,
          protocols: [protocol],
          personal_attachments: true,
          input_capabilities: { [protocol]: ['image', 'pdf'] },
        },
        {
          id: secondModel,
          protocols: [protocol],
          personal_attachments: true,
          input_capabilities: { [protocol]: ['image', 'pdf'] },
        },
      ])
      vi.mocked(runMessages).mockResolvedValue({
        text: 'Messages answer',
        requestId: 'req_messages',
        usage: null,
        finishReason: 'end_turn',
        messageStatus: 'completed',
        nonTextOutput: false,
      })
      vi.mocked(runGemini).mockResolvedValue({
        text: 'Gemini answer',
        requestId: 'req_gemini',
        usage: null,
        finishReason: 'STOP',
        generationStatus: 'completed',
        nonTextOutput: false,
      })

      await ready()
      await chooseFiles([image(), pdf()])
      expect(
        [
          ...host.querySelectorAll<HTMLButtonElement>(
            'button[aria-label^="Get code for comparison"]',
          ),
        ].every((item) => item.disabled),
      ).toBe(true)
      await send('Shared media')

      const requests =
        protocol === 'openai_chat'
          ? vi.mocked(runChat).mock.calls.map((call) => call[1].messages.at(-1)?.content)
          : protocol === 'openai_responses'
            ? vi.mocked(runResponses).mock.calls.map((call) => call[1].input.at(-1)?.content)
            : protocol === 'anthropic_messages'
              ? vi.mocked(runMessages).mock.calls.map((call) => call[1].messages.at(-1)?.content)
              : vi.mocked(runGemini).mock.calls.map((call) => call[1].contents.at(-1)?.parts)
      expect(requests).toEqual([expectedContent, expectedContent])
      expect(lane(1).textContent).toContain('diagram.png · report.pdf')
      expect(lane(2).textContent).toContain('diagram.png · report.pdf')
      expect(deleteAttachment).toHaveBeenCalledTimes(2)
      expect(localStorage.length).toBe(0)
      expect(sessionStorage.length).toBe(0)
      expect(JSON.stringify(localStorage) + JSON.stringify(sessionStorage)).not.toContain(
        'rx_comparison_only',
      )
      expect(JSON.stringify(localStorage) + JSON.stringify(sessionStorage)).not.toContain('obj_')
    },
  )

  it('keeps successful uploads after a sibling upload failure and removes shared chips', async () => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      {
        id: 'media-a',
        protocols: ['openai_chat'],
        personal_attachments: true,
        input_capabilities: { openai_chat: ['image'] },
      },
      {
        id: 'media-b',
        protocols: ['openai_chat'],
        personal_attachments: true,
        input_capabilities: { openai_chat: ['image'] },
      },
    ])
    vi.mocked(uploadAttachment).mockRejectedValueOnce(new AttachmentError(503))
    await ready()
    await chooseFiles([image('unavailable.png'), image('kept.png')])

    expect(host.querySelector('[role="alert"]')?.textContent).toContain(
      'Attachment storage is temporarily unavailable.',
    )
    expect(host.textContent).not.toContain('unavailable.png')
    expect(host.textContent).toContain('kept.png')
    await act(async () => {
      host.querySelector<HTMLButtonElement>('button[aria-label="Remove kept.png"]')!.click()
      await Promise.resolve()
    })
    expect(deleteAttachment).toHaveBeenCalledWith('obj_kept_png', 'csrf-playground')
    expect(host.textContent).not.toContain('kept.png')
  })

  it('invalidates a shared draft on model, protocol, lane-set and key changes', async () => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      {
        id: 'both-a',
        protocols: ['openai_chat', 'openai_responses'],
        personal_attachments: true,
        input_capabilities: {
          openai_chat: ['image'],
          openai_responses: ['image'],
        },
      },
      {
        id: 'chat-b',
        protocols: ['openai_chat'],
        personal_attachments: true,
        input_capabilities: { openai_chat: ['image'] },
      },
      {
        id: 'chat-c',
        protocols: ['openai_chat'],
        personal_attachments: true,
        input_capabilities: { openai_chat: ['image'] },
      },
    ])
    await ready()

    await chooseFiles([image('model.png')])
    await select('Comparison model 1', 'chat-c')
    expect(deleteAttachment).toHaveBeenCalledWith('obj_model_png', 'csrf-playground')
    expect(host.textContent).not.toContain('model.png')

    await select('Comparison model 1', 'both-a')
    await chooseFiles([image('protocol.png')])
    await select('Comparison protocol 1', 'openai_responses')
    expect(deleteAttachment).toHaveBeenCalledWith('obj_protocol_png', 'csrf-playground')
    expect(host.textContent).not.toContain('protocol.png')

    await chooseFiles([image('add.png')])
    await click('Add comparison')
    expect(deleteAttachment).toHaveBeenCalledWith('obj_add_png', 'csrf-playground')
    expect(host.textContent).not.toContain('add.png')

    await chooseFiles([image('remove.png')])
    await click('Remove comparison 3')
    expect(deleteAttachment).toHaveBeenCalledWith('obj_remove_png', 'csrf-playground')
    expect(host.textContent).not.toContain('remove.png')

    await chooseFiles([image('key.png')])
    await fill('comparison_key', 'rx_replaced')
    expect(deleteAttachment).toHaveBeenCalledWith('obj_key_png', 'csrf-playground')
    expect(host.textContent).not.toContain('key.png')
  })

  it('keeps shared objects until every lane settles, including independent cancellation', async () => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      {
        id: 'media-a',
        protocols: ['openai_chat'],
        personal_attachments: true,
        input_capabilities: { openai_chat: ['image'] },
      },
      {
        id: 'media-b',
        protocols: ['openai_chat'],
        personal_attachments: true,
        input_capabilities: { openai_chat: ['image'] },
      },
    ])
    let firstSignal: AbortSignal | undefined
    let finishSecond!: () => void
    vi.mocked(runChat).mockImplementation((_key, request, signal) => {
      if (request.model === 'media-a') {
        firstSignal = signal
        return new Promise((_resolve, reject) => {
          signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
        })
      }
      return new Promise((resolve) => {
        finishSecond = () => resolve(chatResult)
      })
    })
    await ready()
    await chooseFiles([image('shared.png')])
    await send('Cancel one')
    await click('Stop comparison 1')

    expect(firstSignal?.aborted).toBe(true)
    expect(lane(1).textContent).toContain('Stopped')
    expect(deleteAttachment).not.toHaveBeenCalled()
    await act(async () => finishSecond())
    expect(lane(2).textContent).toContain('Chat answer')
    expect(deleteAttachment).toHaveBeenCalledWith('obj_shared_png', 'csrf-playground')
  })

  it('allows the next send while settled attachment deletion is still pending', async () => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      {
        id: 'media-a',
        protocols: ['openai_chat'],
        personal_attachments: true,
        input_capabilities: { openai_chat: ['image'] },
      },
      {
        id: 'media-b',
        protocols: ['openai_chat'],
        personal_attachments: true,
        input_capabilities: { openai_chat: ['image'] },
      },
    ])
    await ready()
    await chooseFiles([image('pending-delete.png')])
    vi.mocked(deleteAttachment).mockImplementation(() => new Promise(() => undefined))

    await send('First shared prompt')
    expect(runChat).toHaveBeenCalledTimes(2)
    expect(deleteAttachment).toHaveBeenCalledWith('obj_pending_delete_png', 'csrf-playground')

    await send('Second prompt')
    expect(runChat).toHaveBeenCalledTimes(4)
    expect(vi.mocked(runChat).mock.calls[2][1].messages.at(-1)).toEqual({
      role: 'user',
      content: 'Second prompt',
    })
    expect(vi.mocked(runChat).mock.calls[3][1].messages.at(-1)).toEqual({
      role: 'user',
      content: 'Second prompt',
    })
  })

  it('cleans ready and late shared uploads when the comparison tab exits', async () => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      {
        id: 'media-a',
        protocols: ['openai_chat'],
        personal_attachments: true,
        input_capabilities: { openai_chat: ['image'] },
      },
      {
        id: 'media-b',
        protocols: ['openai_chat'],
        personal_attachments: true,
        input_capabilities: { openai_chat: ['image'] },
      },
    ])
    await act(async () => root.render(<PlaygroundPage />))
    await click('Model comparison')
    await ready()
    await chooseFiles([image('ready.png')])
    let finishUpload!: (attachment: Awaited<ReturnType<typeof uploadAttachment>>) => void
    vi.mocked(uploadAttachment).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishUpload = resolve
        }),
    )
    await chooseFiles([image('late.png'), image('not-started.png')])
    expect(vi.mocked(uploadAttachment).mock.calls.map(([file]) => file.name)).toEqual([
      'ready.png',
      'late.png',
    ])
    await click('Model conversation')

    expect(deleteAttachment).toHaveBeenCalledWith('obj_ready_png', 'csrf-playground')
    await act(async () => {
      finishUpload({
        id: 'obj_late_upload',
        name: 'late.png',
        mime: 'image/png',
        size: 3,
        state: 'ready',
        created_at: '2026-09-29T12:00:00Z',
      })
      await Promise.resolve()
    })
    expect(deleteAttachment).toHaveBeenCalledWith('obj_late_upload', 'csrf-playground')
    expect(vi.mocked(uploadAttachment).mock.calls.map(([file]) => file.name)).toEqual([
      'ready.png',
      'late.png',
    ])
    await click('Model comparison')
    expect(host.querySelector<HTMLInputElement>('[name="comparison_key"]')!.value).toBe('')
    expect(host.textContent).not.toContain('ready.png')
    expect(host.textContent).not.toContain('late.png')
    expect(host.textContent).not.toContain('not-started.png')
  })
  it('aborts hidden workbench requests and discards its key when switching tabs', async () => {
    await act(async () => root.render(<PlaygroundPage />))
    await click('Model comparison')
    let signal: AbortSignal | undefined
    vi.mocked(runChat).mockImplementation(
      (_key, _request, current) =>
        new Promise((_resolve, reject) => {
          signal = current
          current.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
        }),
    )
    await ready()
    await send()
    await click('Model conversation')
    expect(signal?.aborted).toBe(true)
    await click('Model comparison')
    expect(host.querySelector<HTMLInputElement>('[name="comparison_key"]')!.value).toBe('')
  })
  it('updates comparison accessibility and status copy without losing the shared draft', async () => {
    await ready()
    await fill('comparison_prompt', 'Keep this draft')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(host.querySelector('[aria-label="对比模型 1"]')).not.toBeNull()
    expect(host.textContent).toContain('添加对比')
    expect(host.querySelector<HTMLTextAreaElement>('[name="comparison_prompt"]')!.value).toBe(
      'Keep this draft',
    )
    expect(JSON.stringify(localStorage)).not.toContain('rx_comparison_only')
  })
})

it('runs a native Messages lane alongside Chat and preserves sibling success after handoff', async () => {
  vi.mocked(getGatewayModels).mockResolvedValue([
    { id: 'chat-a', protocols: ['openai_chat'] },
    { id: 'messages-b', protocols: ['anthropic_messages'] },
  ])
  vi.mocked(runMessages).mockResolvedValue({
    text: 'Tool handoff',
    requestId: 'req_messages',
    usage: { prompt_tokens: 6, completion_tokens: 4, total_tokens: 10 },
    finishReason: 'tool_use',
    messageStatus: 'handoff',
    nonTextOutput: true,
  })
  await ready()
  await send('Native comparison')
  expect(lane(2).textContent).toContain('Anthropic Messages')
  expect(lane(2).textContent).toContain('/v1/messages')
  expect(lane(2).textContent).toContain('Action required')
  expect(lane(2).textContent).toContain('req_messages')
  expect(lane(2).textContent).toContain('Total 10 Tokens')
  expect(lane(1).textContent).toContain('Chat answer')
  expect(vi.mocked(runMessages).mock.calls[0][1]).toMatchObject({
    model: 'messages-b',
    messages: [{ role: 'user', content: 'Native comparison' }],
    max_tokens: 2048,
    stream: true,
  })
  await send('Next')
  expect(vi.mocked(runMessages).mock.calls[1][1].messages).toEqual([
    { role: 'user', content: 'Next' },
  ])
  expect(vi.mocked(runChat).mock.calls[1][1].messages).toHaveLength(3)
})

it('runs Gemini alongside Chat with native model roles and isolates Gemini cancellation', async () => {
  vi.mocked(getGatewayModels).mockResolvedValue([
    { id: 'chat-a', protocols: ['openai_chat'] },
    { id: 'gemini-b', protocols: ['gemini_generate_content'] },
  ])
  let currentSignal: AbortSignal | undefined
  vi.mocked(runGemini).mockImplementation(
    (_key, _request, signal, update) =>
      new Promise((_resolve, reject) => {
        currentSignal = signal
        update({
          text: 'Partial Gemini',
          requestId: 'req_gemini',
          usage: null,
          finishReason: null,
          generationStatus: 'incomplete',
          nonTextOutput: false,
        })
        signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
      }),
  )
  await ready()
  await send('Shared')
  expect(lane(1).textContent).toContain('Chat answer')
  expect(lane(2).textContent).toContain('Partial Gemini')
  expect(lane(2).textContent).toContain('/v1beta/models/gemini-b:streamGenerateContent')
  expect(vi.mocked(runGemini).mock.calls[0][1]).toEqual({
    model: 'gemini-b',
    stream: true,
    contents: [{ role: 'user', parts: [{ text: 'Shared' }] }],
    generationConfig: { temperature: 0.7, topP: 1, maxOutputTokens: 2048, candidateCount: 1 },
  })
  await click('Stop comparison 2')
  expect(currentSignal?.aborted).toBe(true)
  expect(lane(2).textContent).toContain('Stopped')
  expect(lane(1).textContent).toContain('Chat answer')
  vi.mocked(runGemini).mockResolvedValue({
    text: 'Complete Gemini',
    requestId: 'req_next',
    usage: null,
    finishReason: 'STOP',
    generationStatus: 'completed',
    nonTextOutput: false,
  })
  await send('Next')
  expect(vi.mocked(runGemini).mock.calls[1][1].contents).toEqual([
    { role: 'user', parts: [{ text: 'Next' }] },
  ])
  expect(vi.mocked(runChat).mock.calls[1][1].messages).toHaveLength(3)
  await send('Continue')
  expect(vi.mocked(runGemini).mock.calls[2][1].contents[1]).toEqual({
    role: 'model',
    parts: [{ text: 'Complete Gemini' }],
  })
  expect(runResponses).not.toHaveBeenCalled()
  expect(runMessages).not.toHaveBeenCalled()
})

it('captures only the selected comparison lane native request, successful history and shared draft', async () => {
  await ready()
  await send('Shared history')
  await fill('comparison_prompt', 'Next draft')
  await click('Get code for comparison 2')
  const code = document.querySelector('pre')!.textContent!
  expect(code).toContain('/v1/responses')
  expect(code).toContain('native-b')
  expect(code).toContain('Native answer')
  expect(code).toContain('Next draft')
  expect(code).not.toContain('Chat answer')
  expect(code).not.toContain('rx_comparison_only')
  expect(code).toContain('"max_output_tokens": 2048')
  expect(runResponses).toHaveBeenCalledTimes(1)
})
