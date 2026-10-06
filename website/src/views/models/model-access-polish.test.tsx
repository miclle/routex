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
  model.name = 'model-<safe>'
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
function displayed(label: string) {
  const copy = button(label)
  return copy.parentElement!.querySelector('code')!.textContent
}
it('copies the exact displayed Base URL and non-secret authentication template without a write request', async () => {
  await ready()
  expect(displayed('Copy Base URL')).toBe(`${window.location.origin}/v1`)
  expect(displayed('Copy authentication header template')).toBe(
    'Authorization: Bearer $ROUTEX_API_KEY',
  )
  await act(async () => button('Copy Base URL').click())
  await act(async () => button('Copy authentication header template').click())
  expect(writeText.mock.calls).toEqual([
    [displayed('Copy Base URL')],
    [displayed('Copy authentication header template')],
  ])
  expect(requests).toEqual(['/model-catalog/mdl_polish'])
})
it('copies the exact Team Base URL and never offers a fake Team Key header', async () => {
  await ready()
  await choose('team:tea_polish')
  expect(button('Copy authentication header template')).toBeUndefined()
  expect(displayed('Copy Base URL')).toBe(`${window.location.origin}/api/v1/teams/tea_polish`)
  await act(async () => button('Copy Base URL').click())
  expect(writeText).toHaveBeenCalledWith(displayed('Copy Base URL'))
  expect(dialog().textContent).not.toContain('Authorization: Bearer $ROUTEX_API_KEY')
})
it('updates accessible copy labels and current feedback live in Chinese without changing copied bytes', async () => {
  await ready()
  const text = displayed('Copy Base URL')
  const example = dialog().querySelector('pre')!.textContent
  await act(async () => button('Copy Base URL').click())
  await act(async () => i18n.changeLanguage('zh'))
  expect(button('复制 Base URL')).toBeDefined()
  expect(displayed('复制 Base URL')).toBe(text)
  expect(dialog().querySelector('pre')!.textContent).toBe(example)
  expect(dialog().textContent).toContain('已复制。')
  button('复制 Base URL').focus()
  expect(document.activeElement).toBe(button('复制 Base URL'))
  expect(button('复制 Base URL').getAttribute('type')).toBe('button')
})

it.each([
  ['openai_chat', '/v1', 'Authorization: Bearer $ROUTEX_API_KEY'],
  ['openai_responses', '/v1', 'Authorization: Bearer $ROUTEX_API_KEY'],
  ['anthropic_messages', '/v1', 'x-api-key: $ROUTEX_API_KEY'],
  ['gemini_generate_content', '/v1beta', 'x-goog-api-key: $ROUTEX_API_KEY'],
])(
  'preserves highlighted %s example text and exact protocol templates',
  async (protocol, path, auth) => {
    if (protocol === 'gemini_generate_content') model.name = 'safe-gemini-model'
    await ready()
    await choose(protocol, 'Protocol type')
    expect(displayed('Copy Base URL')).toBe(window.location.origin + path)
    expect(displayed('Copy authentication header template')).toBe(auth)
    const pre = dialog().querySelector('pre')!
    expect(pre.querySelector('span.font-semibold')?.textContent).toBe('curl')
    expect(pre.querySelector('script')).toBeNull()
    expect(pre.textContent).toContain(model.name)
    await act(async () => button('Copy').click())
    expect(writeText).toHaveBeenCalledWith(pre.textContent)
  },
)
it('shows localized clipboard rejection without a success notice or a control-plane write', async () => {
  await ready()
  writeText.mockRejectedValue(new Error('private clipboard failure'))
  await act(async () => button('Copy authentication header template').click())
  expect(dialog().textContent).toContain('Could not copy. Select the text manually.')
  expect(dialog().textContent).not.toContain('private clipboard failure')
  expect(dialog().textContent).not.toContain('Copied.')
  await act(async () => i18n.changeLanguage('zh'))
  expect(dialog().textContent).toContain('复制失败，请手动选择文本。')
  expect(requests).toEqual(['/model-catalog/mdl_polish'])
})
it.each(['source', 'protocol'])(
  'ignores a late Base URL clipboard success after changing %s',
  async (change) => {
    await ready()
    const pending = deferred()
    writeText.mockReturnValue(pending.promise)
    await act(async () => button('Copy Base URL').click())
    if (change === 'source') await choose('team:tea_polish')
    else await choose('anthropic_messages', 'Protocol type')
    await act(async () => pending.release())
    expect(dialog().textContent).not.toContain('Copied.')
  },
)
it('blocks captured copy actions synchronously when current actor authority is lost', async () => {
  await ready()
  const base = button('Copy Base URL'),
    auth = button('Copy authentication header template')
  await act(async () => {
    current = false
    base.click()
    auth.click()
  })
  expect(writeText).not.toHaveBeenCalled()
})
it.each([200, 403])(
  'hides stale fields during detail renewal and ignores late clipboard completion after status %s',
  async (nextStatus) => {
    await ready()
    const copying = deferred()
    writeText.mockReturnValue(copying.promise)
    await act(async () => button('Copy Base URL').click())
    const oldAuth = button('Copy authentication header template')
    const reading = deferred()
    wait = reading.promise
    status = nextStatus
    let renewal!: Promise<void>
    await act(async () => {
      renewal = cache.refetchQueries({
        queryKey: ['model-catalog', 'detail', 'usr_polish', 'mdl_polish', 0],
      })
      oldAuth.click()
    })
    await until(() => expect(button('Copy Base URL')).toBeUndefined())
    expect(writeText).toHaveBeenCalledTimes(1)
    await act(async () => copying.release())
    await act(async () => {
      reading.release()
      await renewal
    })
    expect(dialog().textContent).not.toContain('Copied.')
    if (nextStatus === 403) expect(dialog().querySelector('pre')).toBeNull()
  },
)
it('retains only the latest copy feedback when an earlier clipboard request completes late', async () => {
  await ready()
  const first = deferred()
  writeText.mockReturnValueOnce(first.promise).mockRejectedValueOnce(new Error('denied'))
  await act(async () => button('Copy Base URL').click())
  await act(async () => button('Copy authentication header template').click())
  await act(async () => first.release())
  expect(dialog().textContent).toContain('Could not copy. Select the text manually.')
  expect(dialog().textContent).not.toContain('Copied.')
})
it('supports focusable keyboard-activated copy and Escape dismissal without late feedback', async () => {
  await ready()
  const pending = deferred()
  writeText.mockReturnValue(pending.promise)
  const control = button('Copy Base URL')
  control.focus()
  expect(document.activeElement).toBe(control)
  await act(async () =>
    control.dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 0 })),
  )
  expect(writeText).toHaveBeenCalledTimes(1)
  await act(async () =>
    control.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'Escape' })),
  )
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  await act(async () => pending.release())
  expect(document.body.textContent).not.toContain('Copied.')
})
it('ignores a rejected clipboard request after the drawer unmounts', async () => {
  await ready()
  let reject!: (error: Error) => void
  writeText.mockReturnValue(
    new Promise<void>((_, fail) => {
      reject = fail
    }),
  )
  await act(async () => button('Copy authentication header template').click())
  await act(async () => root.render(null))
  await act(async () => reject(new Error('clipboard denied')))
  expect(document.body.textContent).not.toContain('Could not copy.')
})

it('does not restore old success feedback after an identical successful detail renewal', async () => {
  await ready()
  await act(async () => button('Copy Base URL').click())
  expect(dialog().textContent).toContain('Copied.')
  const reading = deferred()
  wait = reading.promise
  let renewal!: Promise<void>
  await act(async () => {
    renewal = cache.refetchQueries({
      queryKey: ['model-catalog', 'detail', 'usr_polish', 'mdl_polish', 0],
    })
  })
  await until(() => expect(button('Copy Base URL')).toBeUndefined())
  await act(async () => {
    reading.release()
    await renewal
  })
  await until(() => expect(button('Copy Base URL')).toBeDefined())
  expect(dialog().textContent).not.toContain('Copied.')
})
it('preserves the highlighted Team standalone authentication example and its exact clipboard text', async () => {
  await ready()
  await choose('team:tea_polish')
  const pre = dialog().querySelector('pre')!
  expect(pre.querySelector('span.font-semibold')?.textContent).toBe('python3')
  expect(pre.textContent).toContain('ROUTEX_TEAM_EXAMPLE')
  expect(pre.textContent).not.toContain('ROUTEX_API_KEY')
  await act(async () => button('Copy').click())
  expect(writeText).toHaveBeenCalledWith(pre.textContent)
  expect(requests).toEqual(['/model-catalog/mdl_polish'])
})

it.each(['source', 'protocol'])(
  'keeps captured controls bound to the freshly rendered %s selection',
  async (change) => {
    await ready()
    const oldCopy = button('Copy authentication header template')
    await act(async () => {
      const select = dialog().querySelector<HTMLSelectElement>(
        `select[aria-label="${change === 'source' ? 'Example access source' : 'Protocol type'}"]`,
      )!
      select.value = change === 'source' ? 'team:tea_polish' : 'anthropic_messages'
      select.dispatchEvent(new Event('change', { bubbles: true }))
      oldCopy.click()
    })
    if (change === 'source') expect(writeText).not.toHaveBeenCalled()
    else expect(writeText.mock.calls).toEqual([['x-api-key: $ROUTEX_API_KEY']])
  },
)

it('renders adversarial model text as inert highlighted text and copies it without HTML decoding', async () => {
  model.name = 'model-<script>alert(1)</script>'
  await ready()
  const pre = dialog().querySelector('pre')!
  expect(pre.querySelector('script')).toBeNull()
  expect(pre.textContent).toContain(model.name)
  expect(pre.innerHTML).toContain('&lt;script&gt;')
  await act(async () => button('Copy').click())
  expect(writeText.mock.calls).toEqual([[pre.textContent]])
})
