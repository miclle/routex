import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PlaygroundPage from './index'
import i18n from '@/i18n'
import {
  GatewayError,
  getGatewayModels,
  runChat,
  runResponses,
  runMessages,
  runGemini,
} from '@/api/playground'
import { deleteAttachment, uploadAttachment } from '@/api/attachments'

vi.mock('@/api/playground', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/playground')>()),
  getGatewayModels: vi.fn(),
  runChat: vi.fn(),
  runResponses: vi.fn(),
  runMessages: vi.fn(),
  runGemini: vi.fn(),
}))
vi.mock('@/api/attachments', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/attachments')>()),
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
let root: Root
let container: HTMLDivElement
beforeEach(async () => {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  vi.mocked(getGatewayModels).mockResolvedValue([{ id: 'key-authorized-model' }])
  vi.mocked(runChat).mockImplementation(async (_key, _request, _signal, update) => {
    const result = {
      text: 'Real response',
      requestId: 'req_ui',
      usage: { prompt_tokens: 2, completion_tokens: 3, total_tokens: 5 },
      finishReason: 'stop',
    }
    update(result)
    return result
  })
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
  await act(async () => {
    root.render(<PlaygroundPage />)
  })
})
afterEach(async () => {
  await act(async () => {
    root.unmount()
  })
  container.remove()
  vi.resetAllMocks()
})
function button(text: string) {
  return [...container.querySelectorAll('button')].find((el) => el.textContent === text)!
}
async function click(text: string) {
  await act(async () => {
    button(text).click()
  })
}
async function fill(name: string, value: string) {
  const input = container.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[name="${name}"]`)!
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
async function ready() {
  await fill('api_key', 'rx_transient')
  await click('Verify and load models')
  await fill('prompt', 'Hello')
}
async function submit() {
  await act(async () => {
    container
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}
async function chooseFiles(files: File[]) {
  const input = container.querySelector<HTMLInputElement>('input[type="file"]')!
  await act(async () => {
    Object.defineProperty(input, 'files', { configurable: true, value: files })
    input.dispatchEvent(new Event('change', { bubbles: true }))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

const image = () => new File(['png'], 'diagram.png', { type: 'image/png' })
const pdf = () => new File(['%PDF-1\n%%EOF'], 'report.pdf', { type: 'application/pdf' })
const maxAttachmentBytesForTest = 2 << 20

describe('Playground user behavior', () => {
  it('requires Key verification, sends native parameters, and carries successful chat context forward', async () => {
    expect(button('Send message').disabled).toBe(true)
    await ready()
    await submit()
    expect(container.textContent).toContain('Real response')
    expect(container.textContent).toContain('req_ui')
    expect(container.textContent).toContain('Input 2 · Output 3 · Total 5 Tokens')
    const [key, request] = vi.mocked(runChat).mock.calls[0]
    expect(key).toBe('rx_transient')
    expect(request).toMatchObject({
      model: 'key-authorized-model',
      stream: true,
      max_completion_tokens: 2048,
      stream_options: { include_usage: true },
      messages: [{ role: 'user', content: 'Hello' }],
    })
    await fill('prompt', 'Continue')
    await submit()
    expect(vi.mocked(runChat).mock.calls[1][1].messages).toEqual([
      { role: 'user', content: 'Hello' },
      { role: 'assistant', content: 'Real response' },
      { role: 'user', content: 'Continue' },
    ])
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
    await click('Clear key')
    expect(container.querySelector<HTMLInputElement>('[name="api_key"]')!.value).toBe('')
    expect(container.textContent).not.toContain('Real response')
  })
  it('can select ordinary responses and displays gateway failures with the request ID', async () => {
    await ready()
    await act(async () => {
      container.querySelector<HTMLInputElement>('[name="stream"]')!.click()
    })
    vi.mocked(runChat).mockRejectedValue(new GatewayError('Model access denied', 'req_denied', 403))
    await submit()
    expect(vi.mocked(runChat).mock.calls[0][1].stream).toBe(false)
    expect(container.textContent).toContain('Model access denied')
    expect(container.textContent).toContain('req_denied')
    expect(button('Clear conversation').disabled).toBe(false)
  })
  it('uses the Mockup Enter-to-send and Shift+Enter newline contract', async () => {
    await ready()
    const prompt = container.querySelector<HTMLTextAreaElement>('[name="prompt"]')!
    expect(prompt.placeholder).toBe('Enter a message; Enter to send, Shift+Enter for a new line')
    await act(async () => {
      prompt.dispatchEvent(
        new KeyboardEvent('keydown', {
          key: 'Enter',
          shiftKey: true,
          bubbles: true,
          cancelable: true,
        }),
      )
    })
    expect(runChat).not.toHaveBeenCalled()
    await act(async () => {
      prompt.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }),
      )
    })
    expect(runChat).toHaveBeenCalledOnce()
  })

  it('localizes pending uploads and cleans an object returned after navigation', async () => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      {
        id: 'image-model',
        protocols: ['openai_chat'],
        personal_attachments: true,
        input_capabilities: { openai_chat: ['image'] },
      },
    ])
    let finishUpload!: (attachment: Awaited<ReturnType<typeof uploadAttachment>>) => void
    vi.mocked(uploadAttachment).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishUpload = resolve
        }),
    )
    await ready()
    await chooseFiles([image(), new File(['second'], 'second.png', { type: 'image/png' })])
    expect(container.querySelector('[aria-label="diagram.png: Uploading"]')).not.toBeNull()
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(container.querySelector('[aria-label="diagram.png：正在上传"]')).not.toBeNull()
    await act(async () => {
      root.render(null)
    })
    await act(async () => {
      finishUpload({
        id: 'obj_late_upload',
        name: 'diagram.png',
        mime: 'image/png',
        size: 3,
        state: 'ready',
        created_at: '2026-09-29T12:00:00Z',
      })
      await Promise.resolve()
    })
    expect(deleteAttachment).toHaveBeenCalledWith('obj_late_upload', 'csrf-playground')
    expect(uploadAttachment).toHaveBeenCalledOnce()
  })

  it('clears a stale attachment validation error after a valid upload', async () => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      {
        id: 'image-model',
        protocols: ['openai_chat'],
        personal_attachments: true,
        input_capabilities: { openai_chat: ['image'] },
      },
    ])
    await ready()
    await chooseFiles([pdf()])
    expect(container.textContent).toContain('Choose a supported PNG, JPEG or PDF')
    await chooseFiles([image()])
    expect(container.textContent).not.toContain('Choose a supported PNG, JPEG or PDF')
    expect(container.textContent).toContain('diagram.png')
  })
  it.each([
    [
      'openai_chat',
      [
        { type: 'text', text: 'Hello' },
        {
          type: 'image_url',
          image_url: { url: 'routex://attachments/obj_diagram_png' },
        },
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
        { type: 'input_text', text: 'Hello' },
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
        { type: 'text', text: 'Hello' },
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
        { text: 'Hello' },
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
    'uploads transient files and sends native %s attachment references',
    async (protocol, expectedContent) => {
      vi.mocked(getGatewayModels).mockResolvedValue([
        {
          id: 'media-model',
          protocols: [protocol],
          personal_attachments: true,
          input_capabilities: { [protocol]: ['image', 'pdf'] },
        },
      ])
      vi.mocked(runResponses).mockResolvedValue({
        text: 'Response answer',
        requestId: 'req_responses',
        usage: null,
        finishReason: 'completed',
        responseStatus: 'completed',
        nonTextOutput: false,
      })
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
      const picker = container.querySelector<HTMLButtonElement>(
        'button[aria-label="Attach images or PDFs"]',
      )!
      expect(picker.disabled).toBe(false)
      expect(container.querySelector<HTMLInputElement>('input[type="file"]')!.accept).toBe(
        'image/png,image/jpeg,application/pdf',
      )
      await chooseFiles([image(), pdf()])
      expect(container.textContent).toContain('diagram.png')
      expect(container.textContent).toContain('report.pdf')
      expect(button('Get code').disabled).toBe(true)

      await submit()

      const request =
        protocol === 'openai_chat'
          ? vi.mocked(runChat).mock.calls[0][1].messages.at(-1)?.content
          : protocol === 'openai_responses'
            ? vi.mocked(runResponses).mock.calls[0][1].input.at(-1)?.content
            : protocol === 'anthropic_messages'
              ? vi.mocked(runMessages).mock.calls[0][1].messages.at(-1)?.content
              : vi.mocked(runGemini).mock.calls[0][1].contents.at(-1)?.parts
      expect(request).toEqual(expectedContent)
      expect(container.textContent).toContain('diagram.png · report.pdf')
      expect(uploadAttachment).toHaveBeenCalledTimes(2)
      expect(deleteAttachment).toHaveBeenCalledTimes(2)
      expect(localStorage.length).toBe(0)
      expect(sessionStorage.length).toBe(0)
    },
  )
  it('keeps Project-key and missing-capability attachment controls disabled', async () => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      {
        id: 'project-model',
        protocols: ['openai_chat'],
        personal_attachments: false,
        input_capabilities: { openai_chat: ['image', 'pdf'] },
      },
    ])
    await ready()
    expect(
      container.querySelector<HTMLButtonElement>(
        'button[aria-label="Attachments require a personal API key."]',
      )!.disabled,
    ).toBe(true)
    expect(uploadAttachment).not.toHaveBeenCalled()
  })

  it('validates, deduplicates, removes, localizes, and cleans transient draft files', async () => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      {
        id: 'image-model',
        protocols: ['openai_chat'],
        personal_attachments: true,
        input_capabilities: { openai_chat: ['image'] },
      },
    ])
    await ready()

    await chooseFiles([pdf()])
    expect(container.textContent).toContain('Choose a supported PNG, JPEG or PDF')
    expect(uploadAttachment).not.toHaveBeenCalled()

    vi.mocked(uploadAttachment).mockResolvedValueOnce({
      id: 'obj_server_gif',
      name: 'spoofed.png',
      mime: 'image/gif',
      size: 8,
      state: 'ready',
      created_at: '2026-09-29T12:00:00Z',
    })
    await chooseFiles([new File(['spoof'], 'spoofed.png', { type: 'image/png' })])
    expect(deleteAttachment).toHaveBeenCalledWith('obj_server_gif', 'csrf-playground')
    expect(container.textContent).not.toContain('spoofed.png')

    await chooseFiles([
      new File([new Uint8Array(maxAttachmentBytesForTest + 1)], 'large.png', {
        type: 'image/png',
      }),
    ])
    expect(container.textContent).toContain('must be between 1 byte and 2 MiB')
    expect(uploadAttachment).toHaveBeenCalledOnce()

    const files = Array.from(
      { length: 5 },
      (_, index) => new File(['png'], `file-${index}.png`, { type: 'image/png' }),
    )
    await chooseFiles(files)
    expect(uploadAttachment).toHaveBeenCalledTimes(5)
    expect(container.textContent).toContain('up to four files')

    await chooseFiles([new File(['again'], 'file-0.png', { type: 'image/png' })])
    expect(uploadAttachment).toHaveBeenCalledTimes(5)
    expect(container.textContent).toContain('already attached')

    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[aria-label="Remove file-0.png"]')!.click()
      await Promise.resolve()
    })
    expect(deleteAttachment).toHaveBeenCalledTimes(2)
    expect(container.textContent).not.toContain('file-0.png')

    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(container.textContent).toContain('file-1.png')
    expect(container.querySelector('button[aria-label="移除 file-1.png"]')).not.toBeNull()
    expect(container.querySelector<HTMLTextAreaElement>('[name="prompt"]')!.value).toBe('Hello')

    await fill('api_key', 'rx_replaced')
    expect(deleteAttachment).toHaveBeenCalledTimes(5)
    expect(container.textContent).not.toContain('file-1.png')
    expect(JSON.stringify(localStorage) + JSON.stringify(sessionStorage)).not.toContain('obj_')
  })
  it('shows partial stream content, cancels the request, and excludes the cancelled turn from context', async () => {
    vi.mocked(runChat).mockImplementation(
      (_key, _request, signal, update) =>
        new Promise((_resolve, reject) => {
          update({
            text: 'Partial stream',
            requestId: 'req_stream',
            usage: null,
            finishReason: null,
          })
          signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
        }),
    )
    await ready()
    await submit()
    expect(container.textContent).toContain('Partial stream')
    expect(button('Request in progress…').disabled).toBe(true)
    await click('Stop generation')
    expect(container.textContent).toContain('Stopped')
    expect(container.textContent).toContain('Partial stream')
    vi.mocked(runChat).mockResolvedValue({
      text: 'New answer',
      requestId: 'req_new',
      usage: null,
      finishReason: 'stop',
    })
    await fill('prompt', 'Try again')
    await submit()
    expect(vi.mocked(runChat).mock.calls[1][1].messages).toEqual([
      { role: 'user', content: 'Try again' },
    ])
  })
  it('shows recoverable Key verification errors without enabling send', async () => {
    vi.mocked(getGatewayModels).mockRejectedValue(new GatewayError('Key is revoked', '', 401))
    await fill('api_key', 'revoked-key')
    await click('Verify and load models')
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('Key is revoked')
    expect(button('Send message').disabled).toBe(true)
    expect(button('Verify and load models').disabled).toBe(false)
  })
  it('aborts an active request when leaving the page', async () => {
    let requestSignal: AbortSignal | undefined
    vi.mocked(runChat).mockImplementation((_key, _request, signal) => {
      requestSignal = signal
      return new Promise((_resolve, reject) => {
        signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
      })
    })
    await ready()
    await submit()
    await act(async () => {
      root.render(null)
    })
    expect(requestSignal?.aborted).toBe(true)
  })
})

it('offers eligible native protocols and excludes explicitly unusable models', async () => {
  vi.mocked(getGatewayModels).mockResolvedValue([
    { id: 'responses-only', protocols: ['openai_responses'] },
    { id: 'unavailable', protocols: [] },
    { id: 'both', protocols: ['openai_chat', 'openai_responses'] },
  ])
  await fill('api_key', 'rx_transient')
  await click('Verify and load models')
  const options = [...container.querySelectorAll('option')].map((option) => option.value)
  expect(options).toContain('both')
  expect(options).toContain('responses-only')
  expect(options).not.toContain('unavailable')
})

async function select(name: string, value: string) {
  await act(async () => {
    const input = container.querySelector<HTMLSelectElement>(`select[name="${name}"]`)!
    input.value = value
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
it('uses selected native Responses parameters and resets history when changing protocols', async () => {
  vi.mocked(getGatewayModels).mockResolvedValue([
    { id: 'native', protocols: ['openai_chat', 'openai_responses'] },
  ])
  vi.mocked(runResponses).mockResolvedValue({
    text: 'Response text',
    requestId: 'req_native',
    usage: null,
    finishReason: 'completed',
    responseStatus: 'completed',
    nonTextOutput: false,
  })
  await ready()
  await select('protocol', 'openai_responses')
  await fill('system', 'Be precise')
  await submit()
  expect(vi.mocked(runChat)).not.toHaveBeenCalled()
  expect(vi.mocked(runResponses).mock.calls[0][1]).toEqual({
    model: 'native',
    input: [{ role: 'user', content: 'Hello' }],
    instructions: 'Be precise',
    temperature: 0.7,
    top_p: 1,
    max_output_tokens: 2048,
    stream: true,
  })
  expect(container.textContent).toContain('POST /v1/responses')
  await fill('prompt', 'Continue')
  await submit()
  expect(vi.mocked(runResponses).mock.calls[1][1].input).toEqual([
    { role: 'user', content: 'Hello' },
    { role: 'assistant', content: 'Response text' },
    { role: 'user', content: 'Continue' },
  ])
  await select('protocol', 'openai_chat')
  expect(container.textContent).not.toContain('Response text')
  expect(container.textContent).toContain('POST /v1/chat/completions')
})
it.each(['incomplete', 'failed', 'queued'] as const)(
  'keeps Responses %s outside completed conversation history',
  async (status) => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      { id: 'native', protocols: ['openai_responses'] },
    ])
    vi.mocked(runResponses).mockResolvedValue({
      text: 'Partial native',
      requestId: 'req_native',
      usage: null,
      finishReason: status,
      responseStatus: status,
      nonTextOutput: false,
    })
    await ready()
    await submit()
    expect(container.textContent).toContain(
      status === 'queued'
        ? 'Accepted, not completed'
        : status === 'failed'
          ? 'Call failed'
          : 'Incomplete',
    )
    expect(container.textContent).toContain('Partial native')
    await fill('prompt', 'Retry')
    await submit()
    expect(vi.mocked(runResponses).mock.calls[1][1].input).toEqual([
      { role: 'user', content: 'Retry' },
    ])
    expect(runChat).not.toHaveBeenCalled()
  },
)

it('switches native labels without losing draft or protocol and prevents duplicate dispatch', async () => {
  vi.mocked(getGatewayModels).mockResolvedValue([{ id: 'native', protocols: ['openai_responses'] }])
  let release!: () => void
  vi.mocked(runResponses).mockImplementation(async (_key, _request, _signal, update) => {
    const result = {
      text: 'Native partial',
      requestId: 'req_native',
      usage: null,
      finishReason: null,
      responseStatus: null,
      nonTextOutput: false,
    }
    update(result)
    await new Promise<void>((resolve) => {
      release = resolve
    })
    return { ...result, responseStatus: 'completed' }
  })
  await ready()
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(container.textContent).toContain('协议类型')
  expect(container.querySelector<HTMLTextAreaElement>('[name="prompt"]')!.value).toBe('Hello')
  expect(container.querySelector<HTMLSelectElement>('[name="protocol"]')!.value).toBe(
    'openai_responses',
  )
  await act(async () => {
    const form = container.querySelector('form')!
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
  expect(runResponses).toHaveBeenCalledTimes(1)
  await act(async () => release())
  expect(container.textContent).toContain('Native partial')
  expect(document.documentElement.lang).toBe('zh')
  expect(JSON.stringify(localStorage)).not.toContain('rx_transient')
})

it('sends Messages-only models through the native endpoint with top-level system and no Chat fallback', async () => {
  vi.mocked(getGatewayModels).mockResolvedValue([
    { id: 'messages-native', protocols: ['anthropic_messages'] },
  ])
  vi.mocked(runMessages).mockResolvedValue({
    text: 'Messages answer',
    requestId: 'req_messages',
    usage: { prompt_tokens: 2, completion_tokens: 3, total_tokens: 5 },
    finishReason: 'end_turn',
    messageStatus: 'completed',
    nonTextOutput: false,
  })
  await ready()
  await fill('system', 'Be precise')
  await fill('max_tokens', '0')
  await submit()
  expect(container.textContent).toContain('Anthropic Messages')
  expect(container.textContent).toContain('POST /v1/messages')
  expect(vi.mocked(runMessages).mock.calls[0][1]).toEqual({
    model: 'messages-native',
    messages: [{ role: 'user', content: 'Hello' }],
    system: 'Be precise',
    stream: true,
    temperature: 0.7,
    top_p: 1,
    max_tokens: 0,
  })
  expect(runChat).not.toHaveBeenCalled()
  expect(runResponses).not.toHaveBeenCalled()
  await fill('prompt', 'Continue')
  await submit()
  expect(vi.mocked(runMessages).mock.calls[1][1].messages).toHaveLength(3)
  expect(JSON.stringify(localStorage)).not.toContain('rx_transient')
})
it.each(['handoff', 'refused', 'incomplete'] as const)(
  'keeps Messages %s explicit and out of follow-up history',
  async (status) => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      { id: 'messages-native', protocols: ['anthropic_messages'] },
    ])
    vi.mocked(runMessages).mockResolvedValue({
      text: 'Partial native',
      requestId: 'req_messages',
      usage: null,
      finishReason: 'tool_use',
      messageStatus: status,
      nonTextOutput: false,
    })
    await ready()
    await submit()
    expect(container.textContent).toContain(
      status === 'handoff' ? 'Action required' : status === 'refused' ? 'Refused' : 'Incomplete',
    )
    await fill('prompt', 'Retry')
    await submit()
    expect(vi.mocked(runMessages).mock.calls[1][1].messages).toEqual([
      { role: 'user', content: 'Retry' },
    ])
  },
)

it('runs Gemini-only models with native contents, system instructions and successful inline model history', async () => {
  vi.mocked(getGatewayModels).mockResolvedValue([
    { id: 'gemini-native', protocols: ['gemini_generate_content'] },
  ])
  vi.mocked(runGemini).mockResolvedValue({
    text: 'Gemini answer',
    requestId: 'req_gemini',
    usage: { prompt_tokens: 2, completion_tokens: 3, total_tokens: 5 },
    finishReason: 'STOP',
    generationStatus: 'completed',
    nonTextOutput: false,
  })
  await ready()
  await fill('system', 'Be precise')
  await submit()
  expect(container.textContent).toContain('Gemini Generate Content')
  expect(container.textContent).toContain('/v1beta/models/gemini-native:streamGenerateContent')
  expect(vi.mocked(runGemini).mock.calls[0][1]).toEqual({
    model: 'gemini-native',
    stream: true,
    contents: [{ role: 'user', parts: [{ text: 'Hello' }] }],
    systemInstruction: { parts: [{ text: 'Be precise' }] },
    generationConfig: { temperature: 0.7, topP: 1, maxOutputTokens: 2048, candidateCount: 1 },
  })
  expect(runChat).not.toHaveBeenCalled()
  expect(runResponses).not.toHaveBeenCalled()
  expect(runMessages).not.toHaveBeenCalled()
  await fill('prompt', 'Continue')
  await submit()
  expect(vi.mocked(runGemini).mock.calls[1][1].contents).toEqual([
    { role: 'user', parts: [{ text: 'Hello' }] },
    { role: 'model', parts: [{ text: 'Gemini answer' }] },
    { role: 'user', parts: [{ text: 'Continue' }] },
  ])
  await act(async () => container.querySelector<HTMLInputElement>('[name="stream"]')!.click())
  expect(container.textContent).toContain('/v1beta/models/gemini-native:generateContent')
  await fill('prompt', 'Ordinary')
  await submit()
  expect(vi.mocked(runGemini).mock.calls[2][1].stream).toBe(false)
  expect(JSON.stringify(localStorage)).not.toContain('rx_transient')
})

it.each(['incomplete', 'refused', 'handoff'] as const)(
  'excludes Gemini %s from later history',
  async (status) => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      { id: 'gemini-native', protocols: ['gemini_generate_content'] },
    ])
    vi.mocked(runGemini).mockResolvedValue({
      text: 'Partial Gemini',
      requestId: 'req_gemini',
      usage: null,
      finishReason: 'MAX_TOKENS',
      generationStatus: status,
      nonTextOutput: false,
    })
    await ready()
    await submit()
    await fill('prompt', 'Retry')
    await submit()
    expect(vi.mocked(runGemini).mock.calls[1][1].contents).toEqual([
      { role: 'user', parts: [{ text: 'Retry' }] },
    ])
    expect(runChat).not.toHaveBeenCalled()
  },
)

it('localizes the Gemini alias requirement without substituting a protocol or losing the draft', async () => {
  vi.mocked(getGatewayModels).mockResolvedValue([
    { id: 'vendor/model', protocols: ['gemini_generate_content'] },
  ])
  await ready()
  expect(container.textContent).toContain('compatible public name')
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(container.textContent).toContain('兼容的公开名称')
  expect(container.querySelector<HTMLTextAreaElement>('[name="prompt"]')!.value).toBe('Hello')
  expect(container.querySelector<HTMLSelectElement>('[name="protocol"]')!.value).toBe(
    'gemini_generate_content',
  )
})

it('captures actual current settings, completed history and draft without the entered Key', async () => {
  expect(button('Get code').disabled).toBe(true)
  await ready()
  await submit()
  await fill('prompt', 'Current draft')
  await fill('system', "System\nwith apostrophe: don't")
  await fill('temperature', '0.4')
  await fill('top_p', '0.8')
  await fill('max_tokens', '64')
  await act(async () => container.querySelector<HTMLInputElement>('[name="stream"]')!.click())
  await click('Get code')
  const snippet = document.querySelector('pre')!.textContent!
  expect(snippet).toContain('Current draft')
  expect(snippet).toContain('Real response')
  expect(snippet).toContain('"temperature": 0.4')
  expect(snippet).toContain('"top_p": 0.8')
  expect(snippet).toContain('"max_completion_tokens": 64')
  expect(snippet).not.toContain('"max_tokens": 64')
  expect(snippet).toContain('"stream": false')
  expect(snippet).not.toContain('rx_transient')
  expect(snippet).toContain('$ROUTEX_API_KEY')
  expect(runChat).toHaveBeenCalledTimes(1)
})
it('uses an explicit message placeholder when the current draft is empty', async () => {
  await ready()
  await fill('prompt', '')
  await click('Get code')
  expect(document.querySelector('pre')!.textContent).toContain('REPLACE WITH YOUR MESSAGE')
  expect(runChat).not.toHaveBeenCalled()
})
