import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { AxiosError, AxiosHeaders } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { ModelCatalogRecord } from '@/types/model-catalog'
import ModelAccess from './model-access'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const adapter = client.defaults.adapter
const clipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
const writeText = vi.fn()
let host: HTMLDivElement, root: Root, cache: QueryClient
let current: boolean, status: number, requests: string[]
let wait: Promise<void> | undefined
const releases: (() => void)[] = []
const model: ModelCatalogRecord = {
  id: 'mdl_polish',
  name: 'model-<safe>',
  status: 'active',
  created_at: '2026-09-01T00:00:00Z',
  protocols: ['openai_chat', 'openai_responses', 'anthropic_messages', 'gemini_generate_content'],
  input_capabilities: {},
  input_price: { state: 'unauthorized', rate: null },
  output_price: { state: 'unauthorized', rate: null },
  personal_available: true,
  sources: [
    { type: 'personal', team_id: null, team_name: null, invocation_supported: true },
    {
      type: 'team',
      team_id: 'tea_polish',
      team_name: 'Exact Team',
      invocation_supported: false,
      invocation_protocols: ['openai_chat', 'openai_responses'],
    },
  ],
}
function deferred() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  releases.push(release)
  return { promise, release }
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  model.name = 'sdk-model'
  model.protocols = [
    'openai_chat',
    'openai_responses',
    'anthropic_messages',
    'gemini_generate_content',
  ]
  model.sources = [
    { type: 'personal', team_id: null, team_name: null, invocation_supported: true },
    {
      type: 'team',
      team_id: 'tea_polish',
      team_name: 'Exact Team',
      invocation_supported: false,
      invocation_protocols: ['openai_chat', 'openai_responses'],
    },
  ]
  model.personal_available = true
  current = true
  status = 200
  requests = []
  wait = undefined
  releases.length = 0
  writeText.mockReset().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.defaults.adapter = async (config) => {
    requests.push(config.url!)
    if (config.url !== '/model-catalog/mdl_polish') throw new Error('Unexpected read')
    const captured = status
    if (wait) await wait
    const response = {
      config,
      data: structuredClone(model),
      status: captured,
      statusText: '',
      headers: new AxiosHeaders(),
    }
    if (captured !== 200) throw new AxiosError('rejected', '', config, undefined, response)
    return response
  }
})
afterEach(async () => {
  releases.forEach((release) => release())
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = adapter
  if (clipboard) Object.defineProperty(navigator, 'clipboard', clipboard)
  else Reflect.deleteProperty(navigator, 'clipboard')
  await i18n.changeLanguage('en')
})
async function until(check: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
    try {
      check()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (node) => node.textContent === label || node.getAttribute('aria-label') === label,
  )!
}
function dialog() {
  return document.querySelector('[role="dialog"]')!
}
async function choose(value: string, label = 'Example access source') {
  await act(async () => {
    const select = dialog().querySelector<HTMLSelectElement>(`select[aria-label="${label}"]`)!
    select.value = value
    select.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function ready() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter>
          <ModelAccess
            actorID="usr_polish"
            modelID="mdl_polish"
            isCurrent={() => current}
            onClose={() => root.render(null)}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(dialog().querySelector('h2.break-words')?.textContent).toBe(model.name))
  await choose('personal')
}

function sdk() {
  return dialog()?.querySelector<HTMLElement>(
    'section[aria-label="Official SDK guidance"], section[aria-label="官方 SDK 使用指引"]',
  )
}
it('adds a separate official SDK card after the existing request example without additional reads', async () => {
  await ready()
  expect(sdk()).toBeTruthy()
  expect(sdk()!.textContent).toContain('OpenAI Python')
  expect(sdk()!.textContent).toContain('chat.completions')
  expect(sdk()!.textContent).toContain(`${window.location.origin}/v1`)
  expect(sdk()!.textContent).toContain('sdk-model')
  const sections = [...dialog().querySelectorAll('section')]
  expect(sections.indexOf(sdk()!)).toBe(sections.findIndex((node) => node.querySelector('pre')) + 1)
  expect(requests).toEqual(['/model-catalog/mdl_polish'])
})

it.each([
  [
    'openai_chat',
    'chat.completions',
    'https://github.com/openai/openai-python',
    'Authorization: Bearer $ROUTEX_API_KEY',
  ],
  [
    'openai_responses',
    'use responses',
    'https://github.com/openai/openai-python',
    'Authorization: Bearer $ROUTEX_API_KEY',
  ],
  [
    'anthropic_messages',
    'Anthropic Python',
    'https://github.com/anthropics/anthropic-sdk-python',
    'x-api-key: $ROUTEX_API_KEY',
  ],
  [
    'gemini_generate_content',
    'Google Gen AI Python',
    'https://github.com/googleapis/python-genai',
    'x-goog-api-key: $ROUTEX_API_KEY',
  ],
])(
  'binds %s guidance to the selected native protocol, exact Model and authentication',
  async (protocol, text, url, authentication) => {
    await ready()
    await choose(protocol, 'Protocol type')
    expect(sdk()!.textContent).toContain(text)
    expect(sdk()!.textContent).toContain('sdk-model')
    expect(sdk()!.textContent).toContain(authentication)
    expect(sdk()!.querySelector('a')!.getAttribute('href')).toBe(url)
    expect(sdk()!.textContent).toContain('does not verify SDK compatibility')
    expect(requests).toHaveLength(1)
  },
)
it.each(['anthropic_messages', 'gemini_generate_content'])(
  'uses the unversioned SDK origin for %s while preserving the native Base URL',
  async (protocol) => {
    await ready()
    await choose(protocol, 'Protocol type')
    const nativeVersion = protocol === 'anthropic_messages' ? 'v1' : 'v1beta'
    const guidance = sdk()!.textContent!
    expect(guidance).toContain(`base_url as ${window.location.origin} and`)
    expect(guidance).not.toContain(`base_url as ${window.location.origin}/${nativeVersion}`)
    expect(button('Copy Base URL').parentElement!.querySelector('code')!.textContent).toBe(
      `${window.location.origin}/${nativeVersion}`,
    )
    expect(guidance).toContain(
      protocol === 'anthropic_messages' ? 'appends /v1/messages' : 'api_version as v1beta',
    )
  },
)
it('keeps Team Session guidance separate from Personal SDK configuration and preserves its standalone example', async () => {
  await ready()
  await choose('team:tea_polish')
  const original = dialog().querySelector('pre')!.textContent
  expect(sdk()!.textContent).toContain('current Session cookie and CSRF token')
  expect(sdk()!.textContent).toContain('do not replace the Team endpoint')
  expect(sdk()!.textContent).not.toContain('configure the OpenAI Python')
  expect(sdk()!.textContent).not.toContain('$ROUTEX_API_KEY')
  expect(original).toContain('/api/v1/teams/tea_polish')
  expect(original).toContain('csrf_token')
  expect(button('Copy authentication header template')).toBeUndefined()
  expect(requests).toHaveLength(1)
})
it('switches SDK copy EN→ZH→EN without changing the selected protocol, source, request or clipboard actions', async () => {
  await ready()
  await choose('anthropic_messages', 'Protocol type')
  const original = dialog().querySelector('pre')!.textContent
  await act(async () => i18n.changeLanguage('zh'))
  expect(sdk()!.textContent).toContain('官方 SDK 使用指引')
  expect(sdk()!.textContent).toContain('请勿在 base_url 中再添加 /v1')
  expect(dialog().querySelector('pre')!.textContent).toBe(original)
  expect(sdk()!.querySelector('a')!.textContent).toContain('官方 SDK 文档')
  await act(async () => i18n.changeLanguage('en'))
  expect(sdk()!.textContent).toContain('Official SDK guidance')
  expect(dialog().querySelector('pre')!.textContent).toBe(original)
  expect(requests).toHaveLength(1)
  expect(writeText).not.toHaveBeenCalled()
})
it('waits for an explicit source choice and hides unavailable, removed and unsafe protocol guidance', async () => {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter>
          <ModelAccess
            actorID="usr_polish"
            modelID="mdl_polish"
            isCurrent={() => current}
            onClose={() => {}}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(dialog().textContent).toContain('sdk-model'))
  expect(sdk()).toBeNull()
  await choose('personal')
  expect(sdk()).toBeTruthy()
  model.sources = []
  await act(async () => button('Refresh details').click())
  await until(() => expect(dialog().textContent).toContain('No access source'))
  expect(sdk()).toBeNull()
})
it.each([200, 403])(
  'hides SDK guidance during held renewal and after an unavailable route or HTTP %s',
  async (nextStatus) => {
    await ready()
    const held = deferred()
    wait = held.promise
    status = nextStatus
    model.protocols = []
    await act(async () => button('Refresh details').click())
    await until(() => {
      expect(
        cache.getQueryState(['model-catalog', 'detail', 'usr_polish', 'mdl_polish', 0])!
          .fetchStatus,
      ).toBe('fetching')
      expect(requests).toHaveLength(2)
      expect(dialog().textContent).toContain('Checking current model access…')
    })
    expect(sdk()).toBeNull()
    held.release()
    await until(() =>
      expect(
        cache.getQueryState(['model-catalog', 'detail', 'usr_polish', 'mdl_polish', 0])!
          .fetchStatus,
      ).toBe('idle'),
    )
    if (nextStatus === 403)
      await until(() => expect(dialog().querySelector('[role="alert"]')).not.toBeNull())
    expect(sdk()).toBeNull()
  },
)
it('blocks a captured official link on same-event authority loss and hides guidance after authority renewal', async () => {
  await ready()
  const link = sdk()!.querySelector('a')!
  current = false
  const click = new MouseEvent('click', { bubbles: true, cancelable: true })
  await act(async () => link.dispatchEvent(click))
  expect(click.defaultPrevented).toBe(true)
  await act(async () => i18n.changeLanguage('zh'))
  expect(sdk()).toBeNull()
  expect(requests).toHaveLength(1)
})
it('hides old SDK guidance on actor replacement and while the replacement actor read is held', async () => {
  await ready()
  const held = deferred()
  wait = held.promise
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter>
          <ModelAccess
            actorID="usr_peer"
            modelID="mdl_polish"
            isCurrent={() => current}
            onClose={() => {}}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  expect(sdk()).toBeNull()
  model.sources = []
  held.release()
  await until(() =>
    expect(
      cache.getQueryState(['model-catalog', 'detail', 'usr_peer', 'mdl_polish', 0])!.fetchStatus,
    ).toBe('idle'),
  )
  expect(sdk()).toBeNull()
})
it('renders an exact model label as text and hides SDK guidance for an unsafe Gemini model path', async () => {
  model.name = 'model-<safe>'
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter>
          <ModelAccess
            actorID="usr_polish"
            modelID="mdl_polish"
            isCurrent={() => current}
            onClose={() => {}}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(dialog().textContent).toContain('model-<safe>'))
  await choose('personal')
  expect(sdk()!.textContent).toContain('model-<safe>')
  expect(sdk()!.querySelector('safe')).toBeNull()
  await choose('gemini_generate_content', 'Protocol type')
  expect(sdk()).toBeNull()
})

it('does not silently switch SDK guidance when the selected protocol is removed on refresh', async () => {
  await ready()
  await choose('openai_responses', 'Protocol type')
  expect(sdk()!.textContent).toContain('use responses')
  model.protocols = ['openai_chat']
  await act(async () => button('Refresh details').click())
  await until(() =>
    expect(dialog().textContent).toContain('Select a currently ready protocol explicitly.'),
  )
  expect(sdk()).toBeNull()
  await choose('openai_chat', 'Protocol type')
  expect(sdk()!.textContent).toContain('chat.completions')
  expect(requests).toHaveLength(2)
})
it('hides SDK guidance when Personal routing is unavailable or the drawer is no longer visible', async () => {
  await ready()
  model.personal_available = false
  await act(async () => button('Refresh details').click())
  await until(() =>
    expect(dialog().textContent).toContain('Personal inference is not currently available'),
  )
  expect(sdk()).toBeNull()
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter>
          <ModelAccess
            actorID="usr_polish"
            modelID="mdl_polish"
            visible={false}
            isCurrent={() => current}
            onClose={() => {}}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  expect(sdk()).toBeNull()
})
