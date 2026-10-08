import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, it, expect } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { searchPublicModelNames } from '@/lib/public-model-references'
import { PublicModelName } from './public-model-name'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  authorized: boolean,
  reserved: string[],
  requests: InternalAxiosRequestConfig[],
  release: (() => void) | null,
  hold: Promise<void> | null,
  fail: boolean
function Harness({
  actor = 'usr_one',
  connectionId = 'con_one',
  authority = '1',
  disabled = false,
  excluded = [],
}: {
  actor?: string
  connectionId?: string
  authority?: string
  disabled?: boolean
  excluded?: string[]
}) {
  const [value, setValue] = useState('gpt-5.2')
  return (
    <PublicModelName
      label="Public name"
      value={value}
      onValueChange={setValue}
      disabled={disabled}
      excludedNames={excluded}
      actor={actor}
      connectionId={connectionId}
      authority={authority}
      fresh={() => authorized}
    />
  )
}
async function render(props: Parameters<typeof Harness>[0] = {}) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Harness {...props} />
      </QueryClientProvider>,
    ),
  )
}
async function until(check: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5))
    })
    try {
      check()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
function input() {
  return host.querySelector('input')!
}
async function open() {
  await act(async () => {
    input().focus()
    input().dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
  })
}
async function type(value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input(), value)
    input().dispatchEvent(new Event('input', { bubbles: true }))
  })
}
function options() {
  return Array.from(document.querySelectorAll('[role="option"]')).map((x) => x.textContent)
}
function pause() {
  hold = new Promise<void>((resolve) => {
    release = resolve
  })
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  authorized = true
  reserved = []
  requests = []
  hold = null
  release = null
  fail = false
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const q = config.params.q
    const rows = searchPublicModelNames(q).map((name) => ({
      name,
      available: !reserved.includes(name),
    }))
    const captured = hold
    await captured
    const response = {
      config,
      status: fail ? 503 : 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: { connection_id: config.url!.split('/')[3], query: q, items: rows },
    }
    if (fail) throw new AxiosError('Unavailable', '', config, undefined, response)
    return response
  }
})
afterEach(async () => {
  release?.()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
})
it('shows only available exact candidates and preserves excluded sibling choices without extra reads', async () => {
  reserved = ['gpt-5.2']
  await render()
  await until(() => expect(requests).toHaveLength(1))
  await open()
  await until(() => expect(options()).toEqual(['gpt-5.2-2025-12-11']))
  expect(input().value).toBe('gpt-5.2')
  await render({ excluded: ['gpt-5.2-2025-12-11'] })
  expect(options()).toEqual([])
  expect(requests).toHaveLength(1)
})
it('failed current reads hide suggestions and retain arbitrary custom text with bilingual guidance', async () => {
  fail = true
  await render()
  await until(() => expect(host.textContent).toContain('Name suggestions are unavailable'))
  await open()
  expect(options()).toEqual([])
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('名称建议暂不可用')
  expect(input().value).toBe('gpt-5.2')
  await type('Custom/name')
  expect(input().value).toBe('Custom/name')
  expect(options()).toEqual([])
})
it('late old-text replies are aborted and cannot restore old candidates', async () => {
  pause()
  await render()
  await until(() => expect(requests).toHaveLength(1))
  await type('claude')
  const old = requests[0]
  hold = null
  release?.()
  await open()
  await until(() => expect(options()).toEqual(['claude-sonnet-4-6']))
  expect(old.signal!.aborted).toBe(true)
  expect(input().value).toBe('claude')
})
it.each([{ actor: 'usr_two' }, { connectionId: 'con_two' }, { authority: '2' }])(
  'new owner/resource/same-owner generation hides prior available facts while its read is pending: %j',
  async (props) => {
    await render()
    await open()
    await until(() => expect(options()).toHaveLength(2))
    pause()
    await render(props)
    expect(options()).toEqual([])
    expect(input().value).toBe('gpt-5.2')
    await until(() => expect(requests).toHaveLength(2))
    expect(requests[1].signal!.aborted).toBe(false)
    authorized = false
    await render(props)
    release?.()
    hold = null
    await until(() => expect(options()).toEqual([]))
    expect(input().value).toBe('gpt-5.2')
  },
)
it('synchronous invalidation cancels an already captured option before React paints', async () => {
  await render()
  await open()
  await until(() => expect(options()).toHaveLength(2))
  const old = document.querySelectorAll<HTMLElement>('[role="option"]')[1]
  pause()
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['model-creation-public-names'] })
    old.click()
  })
  expect(input().value).toBe('gpt-5.2')
  expect(options()).toEqual([])
  release?.()
})
it('lost authority blocks captured option selection without blocking future custom drafts', async () => {
  await render()
  await open()
  await until(() => expect(options()).toHaveLength(2))
  const old = document.querySelectorAll<HTMLElement>('[role="option"]')[1]
  authorized = false
  await act(async () => old.click())
  expect(input().value).toBe('gpt-5.2')
  await render({ disabled: true })
  expect(options()).toEqual([])
  expect(input().disabled).toBe(true)
  authorized = true
  await render()
  await type('Manual/exact')
  expect(input().value).toBe('Manual/exact')
})
it('unmount aborts the owned read; a late result cannot recreate retained private queries', async () => {
  pause()
  await render()
  await until(() => expect(requests).toHaveLength(1))
  await act(async () => root.unmount())
  expect(requests[0].signal!.aborted).toBe(true)
  release?.()
  await act(async () => {
    await new Promise((r) => setTimeout(r, 5))
  })
  expect(cache.getQueryCache().findAll({ queryKey: ['model-creation-public-names'] })).toHaveLength(
    0,
  )
  root = createRoot(host)
})
