import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { CallableModel } from '@/types/catalog'
import ModelsPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
const clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
let root: Root, host: HTMLDivElement, cache: QueryClient
let models: CallableModel[], requests: InternalAxiosRequestConfig[]
const writeText = vi.fn<(value: string) => Promise<void>>()

function model(
  name: string,
  protocols?: string[],
  status: CallableModel['status'] = 'active',
): CallableModel {
  return {
    id: `mdl_${name}`,
    name,
    status,
    protocol: 'openai_chat',
    ...(protocols === undefined ? {} : { protocols }),
  }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  models = []
  requests = []
  writeText.mockReset().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    if (config.url !== '/models') throw new Error(`Unexpected request ${config.url}`)
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: { items: models },
    }
  }
})

afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  if (clipboardDescriptor) Object.defineProperty(navigator, 'clipboard', clipboardDescriptor)
  else Reflect.deleteProperty(navigator, 'clipboard')
  await i18n.changeLanguage('en')
})

async function until(assert: () => void) {
  for (let attempt = 0; attempt < 100; attempt++) {
    await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
    try {
      assert()
      return
    } catch (error) {
      if (attempt === 99) throw error
    }
  }
}

async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter>
          <ModelsPage />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.querySelector('[aria-label^="Open API access"]')).not.toBeNull())
}

function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )!
}

async function open(name: string) {
  await act(async () => button(`Open API access for ${name}`).click())
  await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
}

function statistic(label: string) {
  const region = host.querySelector('[aria-label="Model catalogue statistics"]')!
  const item = [...region.children].find((child) => child.querySelector('p')?.textContent === label)
  return item?.lastElementChild?.textContent
}

describe('Member model availability and native examples', () => {
  it('counts the full catalogue separately from active models with supported eligible protocols', async () => {
    models = [
      model('multi', ['openai_chat', 'openai_responses']),
      model('messages', ['anthropic_messages']),
      model('gemini', ['gemini_generate_content']),
      model('unavailable', []),
      model('future', ['future_native']),
      model('disabled', ['openai_chat'], 'disabled'),
      model('archived', ['openai_chat'], 'archived'),
    ]
    await mount()
    expect(statistic('Total models')).toBe('7')
    expect(statistic('Available models')).toBe('3')
    expect(statistic('Protocol type')).toBe('4')
    const search = host.querySelector<HTMLInputElement>('input[type="search"]')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        search,
        'unavailable',
      )
      search.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(host.querySelectorAll('[aria-label^="Open API access"]')).toHaveLength(1)
    expect(statistic('Total models')).toBe('7')
    expect(statistic('Available models')).toBe('3')
  })

  it.each([
    ['explicit empty protocols', model('unavailable', [])],
    ['unknown protocols', model('future', ['future_native'])],
    ['disabled with stale protocols', model('disabled', ['openai_chat'], 'disabled')],
    ['archived with stale protocols', model('archived', ['openai_chat'], 'archived')],
  ])('never fabricates an example or Key availability for %s', async (_, item) => {
    models = [item]
    await mount()
    expect(statistic('Total models')).toBe('1')
    expect(statistic('Available models')).toBe('0')
    expect(statistic('Protocol type')).toBe('0')
    await open(item.name)
    const drawer = document.querySelector('[role="dialog"]')!
    expect(drawer.textContent).toContain('No supported inference protocol is currently available')
    expect(drawer.textContent).toContain(
      'Creating a Key does not make unavailable routes callable.',
    )
    expect(drawer.querySelector('pre')).toBeNull()
    expect(drawer.textContent).not.toContain('curl')
    expect(drawer.textContent).not.toContain(`${window.location.origin}/v1`)
    expect(drawer.querySelector('a[href="/keys"]')).toBeNull()
    expect(button('Copy').disabled).toBe(true)
    await act(async () => button('Copy').click())
    expect(writeText).not.toHaveBeenCalled()
    expect(requests.every((request) => request.url === '/models' && request.method === 'get')).toBe(
      true,
    )
  })

  it('keeps the known legacy protocol fallback only when protocols are absent', async () => {
    models = [model('legacy'), { ...model('legacy-future'), protocol: 'future_native' }]
    await mount()
    expect(statistic('Available models')).toBe('1')
    await open('legacy')
    expect(document.querySelector('pre')?.textContent).toContain('/v1/chat/completions')
    expect(button('Copy').disabled).toBe(false)
  })

  it.each([
    ['openai_chat', '/v1/chat/completions', 'Authorization: Bearer $ROUTEX_API_KEY', '"messages"'],
    [
      'openai_responses',
      '/v1/responses',
      'Authorization: Bearer $ROUTEX_API_KEY',
      '"input":"Hello"',
    ],
    ['anthropic_messages', '/v1/messages', 'x-api-key: $ROUTEX_API_KEY', '"max_tokens":1024'],
    [
      'gemini_generate_content',
      '/v1beta/models/native-model:generateContent',
      'x-goog-api-key: $ROUTEX_API_KEY',
      '"contents"',
    ],
  ])('preserves and copies the valid %s native example', async (protocol, path, header, body) => {
    models = [model('native-model', [protocol])]
    await mount()
    await open('native-model')
    const example = document.querySelector('pre')!.textContent!
    expect(example).toContain(`curl ${window.location.origin}${path}`)
    expect(example).toContain(header)
    expect(example).toContain(body)
    if (protocol === 'anthropic_messages')
      expect(example).toContain('anthropic-version: 2023-06-01')
    expect(button('Copy').disabled).toBe(false)
    await act(async () => button('Copy').click())
    expect(writeText).toHaveBeenCalledExactlyOnceWith(example)
    expect(document.body.textContent).toContain('Copied.')
  })

  it('filters unknown protocols and duplicate options without selecting a fallback for them', async () => {
    models = [model('mixed', ['future_native', 'openai_responses', 'openai_chat', 'openai_chat'])]
    await mount()
    await open('mixed')
    const select = document.querySelector<HTMLSelectElement>('select')!
    expect([...select.options].map((option) => option.value)).toEqual([
      'openai_responses',
      'openai_chat',
    ])
    await act(async () => {
      select.value = 'openai_responses'
      select.dispatchEvent(new Event('change', { bubbles: true }))
    })
    expect(document.querySelector('pre')?.textContent).toContain('/v1/responses')
    expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('future_native')
  })

  it('preserves the Gemini public-name guard and disables copying an unsafe path', async () => {
    models = [model('unsafe/name', ['gemini_generate_content'])]
    await mount()
    await open('unsafe/name')
    expect(document.body.textContent).toContain('Gemini requires a public name or active alias')
    expect(document.querySelector('pre')).toBeNull()
    expect(button('Copy').disabled).toBe(true)
    expect(writeText).not.toHaveBeenCalled()
  })

  it.each([
    ['removed eligibility', [] as string[], 'active' as const],
    ['disabled model', ['openai_chat'], 'disabled' as const],
  ])(
    'updates an open drawer after a catalogue refresh reports %s',
    async (_, protocols, status) => {
      models = [model('current-model', ['openai_chat'])]
      await mount()
      await open('current-model')
      expect(document.querySelector('pre')?.textContent).toContain('/v1/chat/completions')
      expect(button('Copy').disabled).toBe(false)

      models = [model('current-model', protocols, status)]
      await act(async () => cache.invalidateQueries({ queryKey: ['models'] }))
      await until(() => expect(statistic('Available models')).toBe('0'))

      const drawer = document.querySelector('[role="dialog"]')!
      expect(drawer.textContent).toContain('current-model API access')
      expect(drawer.textContent).toContain('No supported inference protocol is currently available')
      expect(drawer.querySelector('pre')).toBeNull()
      expect(drawer.querySelector('a[href="/keys"]')).toBeNull()
      expect(button('Copy').disabled).toBe(true)
      expect(writeText).not.toHaveBeenCalled()
      expect(requests.map((request) => request.url)).toEqual(['/models', '/models'])
    },
  )

  it('starts in English and updates unavailable guidance live without changing the selected model', async () => {
    models = [model('unavailable', [])]
    await mount()
    await open('unavailable')
    expect(document.body.textContent).toContain(
      'No supported inference protocol is currently available',
    )
    await act(async () => i18n.changeLanguage('zh'))
    const drawer = document.querySelector('[role="dialog"]')!
    expect(drawer.textContent).toContain('unavailable API 接入')
    expect(drawer.textContent).toContain('此模型当前没有可用的受支持推理协议')
    expect(drawer.textContent).toContain('创建 Key 不会让不可用路由变得可调用')
    expect(button('复制').disabled).toBe(true)
    expect(drawer.querySelector('pre')).toBeNull()
    expect(requests).toHaveLength(1)
  })
})
