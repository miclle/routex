import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, useLocation } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import type { ModelCatalogRecord } from '@/types/model-catalog'
import type { Session } from '@/types/auth'
import ModelsPage from './index'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
const clipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
const writeText = vi.fn()
let root: Root, host: HTMLDivElement, cache: QueryClient
let actor: string, models: ModelCatalogRecord[], requests: InternalAxiosRequestConfig[]
let listStatus: number, detailStatus: number
let detailWait: Promise<void> | undefined, listWait: Promise<void> | undefined
const releases: (() => void)[] = []
function fixture(): ModelCatalogRecord {
  return {
    id: 'mdl_native',
    name: 'native-model',
    status: 'active',
    created_at: '2026-09-01T00:00:00Z',
    protocols: ['openai_chat'],
    input_capabilities: {},
    personal_available: true,
    sources: [
      { type: 'personal', team_id: null, team_name: null, invocation_supported: true },
      {
        type: 'team',
        team_id: 'tem_legacy',
        team_name: 'Engineering',
        invocation_supported: false,
        invocation_protocols: ['openai_responses'],
      },
      {
        type: 'team',
        team_id: 'tea_second',
        team_name: 'Research',
        invocation_supported: false,
        invocation_protocols: ['gemini_generate_content'],
      },
    ],
  }
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
  vi.spyOn(Date, 'now').mockReturnValue(1_800_000_000_000)
  await i18n.changeLanguage('en')
  actor = 'usr_one'
  models = [fixture()]
  requests = []
  listStatus = detailStatus = 200
  detailWait = listWait = undefined
  releases.length = 0
  writeText.mockReset().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    // Capture facts when the request begins, even if the transport ignores abort.
    let data: unknown,
      status = 200
    const wait =
      config.url === '/model-catalog'
        ? listWait
        : config.url?.startsWith('/model-catalog/')
          ? detailWait
          : undefined
    if (config.url === '/auth/session')
      data = {
        user: { id: actor, role: 'member', name: 'Member', email: 'private@example.test' },
        csrf_token: 'live-browser-csrf',
      }
    else if (config.url === '/model-catalog') {
      data = { items: structuredClone(models) }
      status = listStatus
    } else if (config.url?.startsWith('/model-catalog/')) {
      data = structuredClone(
        models.find((model) => model.id === config.url!.slice('/model-catalog/'.length)),
      )
      status = detailStatus
    } else throw new Error(`Unexpected request ${config.url}`)
    if (wait) await wait
    const response = { config, data, status, statusText: '', headers: new AxiosHeaders() }
    if (status !== 200)
      throw new AxiosError('rejected', '', config, undefined, {
        ...response,
        data: { message: 'sanitized failure' },
      })
    return response
  }
})
afterEach(async () => {
  releases.forEach((release) => release())
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  if (clipboard) Object.defineProperty(navigator, 'clipboard', clipboard)
  else Reflect.deleteProperty(navigator, 'clipboard')
  vi.restoreAllMocks()
  await i18n.changeLanguage('en')
})
async function until(assert: () => void) {
  for (let n = 0; n < 100; n++) {
    await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
    try {
      assert()
      return
    } catch (error) {
      if (n === 99) throw error
    }
  }
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )!
}
function drawer() {
  return document.querySelector('[role="dialog"]')!
}
function example() {
  return drawer()?.querySelector('pre')?.textContent
}
function count(url: string) {
  return requests.filter((request) => request.url === url).length
}
function CatalogueHarness() {
  const location = useLocation()
  return (
    <>
      <ModelsPage />
      <output data-testid="route">
        {location.pathname}
        {location.search}
      </output>
    </>
  )
}
async function ready() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter>
          <CatalogueHarness />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(button('Open API access for native-model')).toBeDefined())
  await act(async () => button('Open API access for native-model').click())
  await until(() =>
    expect(drawer().querySelector('h2.break-words')?.textContent).toBe('native-model'),
  )
}
async function choose(source: string, label = 'Example access source') {
  await act(async () => {
    const select = drawer().querySelector<HTMLSelectElement>(`select[aria-label="${label}"]`)!
    select.value = source
    select.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
it.each([
  ['openai_chat', '/chat/completions'],
  ['openai_responses', '/responses'],
  ['anthropic_messages', '/messages'],
  ['gemini_generate_content', '/models/native-model:generateContent'],
])(
  'offers the exact Team %s standalone request without Personal or browser secrets and without dispatch',
  async (protocol, path) => {
    models[0].sources[1] = {
      ...models[0].sources[1],
      invocation_protocols: [protocol],
    } as ModelCatalogRecord['sources'][number]
    await ready()
    expect(example()).toBeUndefined()
    expect(button('Copy').disabled).toBe(true)
    await choose('team:tem_legacy')
    expect(
      drawer().querySelector<HTMLSelectElement>('select[aria-label="Protocol type"]')?.value,
    ).toBe(protocol)
    expect(example()).toContain(`/api/v1/teams/tem_legacy${path}`)
    expect(example()).toContain('ROUTEX_EMAIL')
    expect(example()).toContain('ROUTEX_PASSWORD')
    expect(example()).toContain('Two-step login challenge required; no inference was sent.')
    expect(example()).not.toContain('ROUTEX_API_KEY')
    expect(example()).not.toContain('live-browser-csrf')
    expect(example()).not.toContain('private@example.test')
    expect(example()).not.toContain('Authorization: Bearer')
    expect(drawer().querySelector('a[href="/keys"]')).toBeNull()
    expect(drawer().textContent).toContain('cURL and Python 3 (standard library)')
    expect(button('Copy').disabled).toBe(false)
    await act(async () => button('Copy').click())
    expect(writeText).toHaveBeenCalledWith(example())
    expect(requests.every((request) => request.method === 'get')).toBe(true)
    expect(new Set(requests.map((request) => request.url))).toEqual(
      new Set(['/auth/session', '/model-catalog', '/model-catalog/mdl_native']),
    )
  },
)
it('keeps Personal examples separate and derives each exact Team protocol independently', async () => {
  await ready()
  await choose('personal')
  expect(example()).toContain('/v1/chat/completions')
  expect(example()).toContain('ROUTEX_API_KEY')
  await choose('team:tem_legacy')
  expect(example()).toContain('/api/v1/teams/tem_legacy/responses')
  expect(example()).not.toContain('/chat/completions')
  await choose('team:tea_second')
  expect(example()).toContain('/api/v1/teams/tea_second/models/native-model:generateContent')
  expect(example()).not.toContain('tem_legacy')
  expect(example()).not.toContain('ROUTEX_API_KEY')
})
it('switches source labels and dependency guidance live to Chinese without changing captured public identities', async () => {
  await ready()
  await choose('team:tem_legacy')
  const before = example()
  await act(async () => i18n.changeLanguage('zh'))
  expect(drawer().querySelector('select[aria-label="示例访问来源"]')).not.toBeNull()
  expect(drawer().textContent).toContain('Team：Engineering')
  expect(drawer().textContent).toContain('需要 cURL 和 Python 3（标准库）')
  expect(example()).toBe(before)
})
it('revalidates list and selected detail for structurally identical same-millisecond Session success and cancels obsolete reads', async () => {
  await ready()
  await choose('team:tem_legacy')
  const old = cache.getQueryState(sessionKey)!,
    oldCopy = button('Copy')
  const beforeList = count('/model-catalog'),
    beforeDetail = count('/model-catalog/mdl_native')
  const hold = deferred()
  listWait = detailWait = hold.promise
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
    oldCopy.click()
  })
  const renewed = cache.getQueryState(sessionKey)!
  expect(renewed.data).toBe(old.data)
  expect(renewed.dataUpdatedAt).toBe(old.dataUpdatedAt)
  expect(renewed.dataUpdateCount).toBe(old.dataUpdateCount + 1)
  await until(() => {
    expect(count('/model-catalog')).toBe(beforeList + 1)
    expect(count('/model-catalog/mdl_native')).toBe(beforeDetail + 1)
  })
  expect(example()).toBeUndefined()
  expect(host.querySelector('article')).toBeNull()
  expect(writeText).not.toHaveBeenCalled()
  const obsolete = requests
    .filter(
      (request) => request.url === '/model-catalog' || request.url === '/model-catalog/mdl_native',
    )
    .slice(-2)
  models[0].sources = models[0].sources.filter(
    (source) => source.type !== 'team' || source.team_id !== 'tem_legacy',
  )
  listWait = detailWait = undefined
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await until(() =>
    expect(drawer().textContent).toContain('The selected access source is not currently available'),
  )
  expect(obsolete.every((request) => request.signal?.aborted)).toBe(true)
  await act(async () => hold.release())
  expect(example()).toBeUndefined()
  expect(button('Copy').disabled).toBe(true)
  expect(
    drawer().querySelector('select[aria-label="Example access source"]')?.getAttribute('value'),
  ).not.toBe('personal')
  expect(drawer().querySelector('a[href^="/playground?team=tem_legacy"]')).toBeNull()
  expect(count('/auth/session')).toBe(3)
})
it('does not invalidate current source authority on a manual same-actor CSRF cache replacement', async () => {
  await ready()
  await choose('team:tem_legacy')
  const before = requests.length,
    text = example()
  await act(async () =>
    cache.setQueryData<Session>(sessionKey, (previous) => ({
      ...previous!,
      csrf_token: 'rotated-current-csrf',
    })),
  )
  expect(requests).toHaveLength(before)
  expect(example()).toBe(text)
  expect(button('Copy').disabled).toBe(false)
  await act(async () => button('Copy').click())
  expect(writeText).toHaveBeenCalledWith(text)
  expect(text).not.toContain('rotated-current-csrf')
})
it.each([403, 404])(
  'hides detail and copy after current source rejection %s and never treats a late clipboard result as current',
  async (status) => {
    await ready()
    await choose('team:tem_legacy')
    const copy = deferred()
    writeText.mockReturnValue(copy.promise)
    await act(async () => button('Copy').click())
    detailStatus = status
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey })
    })
    await until(() => expect(drawer().querySelector('[role="alert"]')).not.toBeNull())
    expect(example()).toBeUndefined()
    expect(drawer().textContent).not.toContain('Engineering')
    await act(async () => copy.release())
    expect(drawer().textContent).not.toContain('Copied.')
  },
)
it('clears prior actor ownership and ignores late detail when the Session actor changes', async () => {
  await ready()
  await choose('team:tem_legacy')
  const hold = deferred()
  detailWait = hold.promise
  await act(async () => button('Refresh details').click())
  const obsolete = requests.filter((request) => request.url === '/model-catalog/mdl_native').at(-1)!
  actor = 'usr_two'
  models = []
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await until(() => expect(obsolete.signal?.aborted).toBe(true))
  await act(async () => hold.release())
  expect(example()).toBeUndefined()
  expect(document.body.textContent).not.toContain('Engineering')
  expect(
    cache
      .getQueriesData({ queryKey: ['model-catalog', 'detail', 'usr_two'] })
      .every(([, data]) => !data),
  ).toBe(true)
})
it('blocks synchronous old Copy actions at Session fetch start and on a failed Session read', async () => {
  await ready()
  await choose('team:tem_legacy')
  const oldCopy = button('Copy')
  client.defaults.adapter = async (config) => {
    if (config.url === '/auth/session') throw new Error('Session unavailable')
    throw new Error('No catalogue read is authorized')
  }
  await act(async () => {
    const renewal = cache.refetchQueries({ queryKey: sessionKey })
    oldCopy.click()
    await renewal
  })
  await until(() => expect(example()).toBeUndefined())
  expect(writeText).not.toHaveBeenCalled()
  expect(document.body.textContent).not.toContain('Engineering')
})

it('retains the exact selected protocol after renewal and requires explicit review when it is no longer ready', async () => {
  await ready()
  await choose('team:tem_legacy')
  expect(example()).toContain('/responses')
  models[0].sources[1] = {
    ...models[0].sources[1],
    invocation_protocols: ['openai_chat'],
  } as ModelCatalogRecord['sources'][number]
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await until(() =>
    expect(drawer().textContent).toContain('Select a currently ready protocol explicitly.'),
  )
  expect(example()).toBeUndefined()
  expect(button('Copy').disabled).toBe(true)
  await choose('openai_chat', 'Protocol type')
  expect(example()).toContain('/api/v1/teams/tem_legacy/chat/completions')
})
it('does not publish a late copy notice after switching the exact Team source or protocol', async () => {
  await ready()
  await choose('team:tem_legacy')
  const hold = deferred()
  writeText.mockReturnValue(hold.promise)
  await act(async () => button('Copy').click())
  await choose('team:tea_second')
  await act(async () => hold.release())
  expect(drawer().textContent).not.toContain('Copied.')
  expect(example()).toContain('/api/v1/teams/tea_second/models/native-model:generateContent')
})

it('guards a captured Team navigation action synchronously during renewed Session authority and permits it after fresh exact detail', async () => {
  await ready()
  await choose('team:tem_legacy')
  const oldLink = drawer().querySelector<HTMLAnchorElement>(
    'a[href^="/playground?team=tem_legacy"]',
  )!
  const hold = deferred()
  detailWait = listWait = hold.promise
  await act(async () => {
    const renewal = cache.refetchQueries({ queryKey: sessionKey })
    expect(oldLink.isConnected).toBe(true)
    oldLink.click()
    await renewal
  })
  expect(host.querySelector('[data-testid="route"]')?.textContent).toBe('/')
  detailWait = listWait = undefined
  await act(async () => hold.release())
  await until(() => expect(example()).toContain('/api/v1/teams/tem_legacy/responses'))
  await act(async () =>
    drawer().querySelector<HTMLAnchorElement>('a[href^="/playground?team=tem_legacy"]')!.click(),
  )
  expect(host.querySelector('[data-testid="route"]')?.textContent).toBe(
    '/playground?team=tem_legacy&model=mdl_native',
  )
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})
