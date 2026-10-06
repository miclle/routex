import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider, useParams } from 'react-router'
import { AxiosError, AxiosHeaders, CanceledError, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey, useSession } from '@/hooks/use-auth'
import type { Provider, ProviderModel } from '@/types/catalog'
import ProviderModelTable from './provider-models'
import ProvidersPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let root: Root, host: HTMLDivElement, cache: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[], permissions: string[], actor: string
let permissionError: boolean, catalogueError: boolean
let holdUrl: string | null,
  held: { release: () => void; signal: InternalAxiosRequestConfig['signal'] }[]
const add = vi.fn()
const model = (
  id: string,
  name: string,
  enabled: boolean,
  image = false,
  pdf = false,
): ProviderModel => ({
  id,
  upstream_name: name,
  enabled,
  supports_image_input: image,
  supports_pdf_input: pdf,
  etag: 'recorded-revision',
})
const providers: Provider[] = [
  {
    id: 'prv_first',
    name: 'First provider',
    connections: [
      {
        id: 'con_primary',
        name: 'Primary',
        base_url: 'https://one.example.invalid',
        protocol: 'openai_chat',
        credentials: [],
        provider_models: [
          model('pmd_alpha', 'Alpha', true),
          model('pmd_disabled', 'alpha disabled', false, true, true),
        ],
      },
      {
        id: 'con_secondary',
        name: 'Secondary',
        base_url: 'https://two.example.invalid',
        protocol: 'openai_responses',
        credentials: [],
        provider_models: [
          model('pmd_duplicate', 'Alpha', true, true),
          model('pmd_literal', 'literal %_[test]', false, false, true),
        ],
      },
    ],
  },
  {
    id: 'prv_second',
    name: 'Second provider',
    connections: [
      {
        id: 'con_other',
        name: 'Other',
        base_url: 'https://other.example.invalid',
        protocol: 'gemini_generate_content',
        credentials: [],
        provider_models: [model('pmd_other', 'Other model', true)],
      },
    ],
  },
]
function Host() {
  const session = useSession()
  const { providerId = '' } = useParams()
  return <ProviderModelTable providerId={providerId} session={session} onAdd={add} />
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_first'
  permissions = ['providers.read', 'providers.write']
  requests = []
  permissionError = catalogueError = false
  holdUrl = null
  held = []
  add.mockClear()
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    // Capture before waiting, to make stale-reply fencing observable.
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session')
      response.data = {
        user: { id: actor, name: actor, role: 'member', email: 'actor@example.invalid' },
        csrf_token: 'current-csrf',
      }
    else if (config.url === '/auth/permissions') response.data = { permissions: [...permissions] }
    else if (config.url === '/admin/providers')
      response.data = { items: structuredClone(providers) }
    else throw new Error(`Unexpected directory request: ${config.url}`)
    if (
      (permissionError && config.url === '/auth/permissions') ||
      (catalogueError && config.url === '/admin/providers')
    )
      throw new AxiosError('Controlled read failure', '', config, undefined, {
        ...response,
        status: 503,
      })
    if (config.url === holdUrl)
      await new Promise<void>((resolve, reject) => {
        held.push({ release: resolve, signal: config.signal })
        config.signal?.addEventListener?.(
          'abort',
          () => reject(new CanceledError(undefined, config)),
          { once: true },
        )
      })
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  await i18n.changeLanguage('en')
})
async function until(assertion: () => void) {
  for (let i = 0; i < 120; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assertion()
      return
    } catch (error) {
      if (i === 119) throw error
    }
  }
}
async function mount(fullPage = false, ready = true) {
  router = createMemoryRouter(
    [{ path: '/admin/providers/:providerId', element: fullPage ? <ProvidersPage /> : <Host /> }],
    { initialEntries: ['/admin/providers/prv_first?tab=models'] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  if (ready) await until(() => expect(table()).not.toBeNull())
}
const table = () => host.querySelector<HTMLTableElement>('table[aria-label="Provider models"]')
const names = () =>
  [...table()!.querySelectorAll('tbody tr')]
    .filter((row) => row.children.length > 1)
    .map((row) => row.children[0].textContent)
const button = (label: string) =>
  [...document.querySelectorAll<HTMLElement>('button,[role="menuitem"]')].find(
    (item) => item.getAttribute('aria-label') === label || item.textContent === label,
  )!
async function click(label: string) {
  await act(async () => button(label).click())
}
async function select(label: string, option: string) {
  await click(label)
  await until(() => expect(button(option)).toBeTruthy())
  await click(option)
}
async function search(value: string, label = 'Search model identifiers') {
  const input = host.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
const permissionKeys = () =>
  cache
    .getQueryCache()
    .getAll()
    .filter((query) => query.queryKey[0] === 'permissions')
    .map((query) => query.queryKey)
const catalogueKeys = () =>
  cache
    .getQueryCache()
    .getAll()
    .filter((query) => query.queryKey[0] === 'admin' && query.queryKey[1] === 'providers')
    .map((query) => query.queryKey)
async function renew(keys: readonly (readonly unknown[])[]) {
  await act(async () => {
    for (const key of keys) void cache.invalidateQueries({ queryKey: key, exact: true })
  })
}

it('intersects literal identifier, exact Connection and enabled filters without directory or writes', async () => {
  await mount()
  expect(names()).toEqual(['Alpha', 'alpha disabled', 'Alpha', 'literal %_[test]'])
  await search('  ALPHA  ')
  await select('Filter model Connection', 'Secondary')
  await select('Filter model enabled state', 'Enabled')
  expect(names()).toEqual(['Alpha'])
  expect(table()!.querySelector('tbody a')?.getAttribute('href')).toBe(
    '/admin/providers/prv_first/models/pmd_duplicate',
  )
  expect(requests.every((request) => ['get'].includes(request.method!))).toBe(true)
  expect(new Set(requests.map((request) => request.url))).toEqual(
    new Set(['/auth/session', '/auth/permissions', '/admin/providers']),
  )
})
it('treats wildcard and regex-looking text literally and preserves explicit empty results', async () => {
  await mount()
  await search('%_[test]')
  expect(names()).toEqual(['literal %_[test]'])
  await select('Filter model enabled state', 'Enabled')
  expect(names()).toEqual([])
  expect(host.textContent).toContain('No matching provider models.')
})
it('renders independent stored enabled/image/PDF declarations without readiness claims', async () => {
  await mount()
  const row = [...table()!.querySelectorAll('tbody tr')].find((item) =>
    item.textContent?.includes('alpha disabled'),
  )!
  expect([...row.children].map((item) => item.textContent).slice(3)).toEqual([
    'Disabled',
    'Declared',
    'Declared',
  ])
  expect(host.textContent).toContain(
    'They do not confirm routing availability, capacity or valid prices.',
  )
})
it('preserves filters on live Chinese switch and keeps native protocol and IDs unchanged', async () => {
  await mount()
  await search('Alpha')
  await select('Filter model Connection', 'Secondary')
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.querySelector<HTMLInputElement>('input[aria-label="搜索模型标识"]')?.value).toBe(
    'Alpha',
  )
  expect(host.querySelector('table')?.querySelector('tbody a')?.getAttribute('href')).toBe(
    '/admin/providers/prv_first/models/pmd_duplicate',
  )
  expect(host.textContent).toContain('图片输入声明')
  expect(host.textContent).toContain('Secondary')
})
it('resets controls on Provider target change and omits Connection selector for a single Connection', async () => {
  await mount()
  await search('missing')
  await select('Filter model Connection', 'Secondary')
  await act(async () => router.navigate('/admin/providers/prv_second?tab=models'))
  await until(() => expect(names()).toEqual(['Other model']))
  expect(host.querySelector<HTMLInputElement>('input[type="search"]')?.value).toBe('')
  expect(button('Filter model Connection')).toBeUndefined()
})
it('keeps independent read/write permissions and binds Add to exact stored Connection', async () => {
  permissions = ['providers.read']
  await mount()
  expect((button('Add manually · Primary') as HTMLButtonElement).disabled).toBe(true)
  await click('Add manually · Primary')
  expect(add).not.toHaveBeenCalled()
  permissions = ['providers.read', 'providers.write']
  await renew(permissionKeys())
  await until(() =>
    expect((button('Add manually · Secondary') as HTMLButtonElement).disabled).toBe(false),
  )
  await click('Add manually · Secondary')
  expect(add).toHaveBeenCalledExactlyOnceWith('con_secondary')
})
it('does not fetch a Provider catalogue for write-only authority', async () => {
  permissions = ['providers.write']
  await mount(false, false)
  await until(() => expect(host.textContent).toContain('does not have permission to read'))
  expect(table()).toBeNull()
  expect(requests.some((request) => request.url === '/admin/providers')).toBe(false)
})
it('hides cached rows/actions during renewed permissions and keeps them hidden after denial', async () => {
  await mount()
  holdUrl = '/auth/permissions'
  permissions = []
  await renew(permissionKeys())
  await until(() => expect(held).toHaveLength(1))
  expect(table()).toBeNull()
  expect(button('Add manually · Primary')).toBeUndefined()
  holdUrl = null
  await act(async () => held[0].release())
  await until(() => expect(host.textContent).toContain('does not have permission to read'))
  expect(table()).toBeNull()
})
it('hides rows during Session renewal and resets controls for a different actor', async () => {
  await mount()
  await search('missing')
  holdUrl = '/auth/session'
  actor = 'usr_second'
  await renew([sessionKey])
  await until(() => expect(held).toHaveLength(1))
  expect(table()).toBeNull()
  holdUrl = null
  await act(async () => held[0].release())
  await until(() => expect(names()).toHaveLength(4))
  expect(host.querySelector<HTMLInputElement>('input[type="search"]')?.value).toBe('')
})
it('cancels an obsolete catalogue request when Provider target changes and never restores its reply', async () => {
  holdUrl = '/admin/providers'
  await mount(false, false)
  await until(() => expect(held).toHaveLength(1))
  const obsolete = held[0]
  holdUrl = null
  await act(async () => router.navigate('/admin/providers/prv_second?tab=models'))
  await until(() => expect(names()).toEqual(['Other model']))
  expect(obsolete.signal?.aborted).toBe(true)
  await act(async () => obsolete.release())
  expect(names()).toEqual(['Other model'])
})
it.each(['permission', 'catalogue'])(
  'hides cached private rows after a %s read failure',
  async (kind) => {
    await mount()
    if (kind === 'permission') permissionError = true
    else catalogueError = true
    await renew(kind === 'permission' ? permissionKeys() : catalogueKeys())
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(table()).toBeNull()
    expect(button('Add manually · Primary')).toBeUndefined()
  },
)
it('uses the actual Provider Models tab with the existing Add and detail hierarchy', async () => {
  await mount(true)
  expect(names()).toHaveLength(4)
  expect(host.querySelector('a[href="/admin/providers/prv_first/models/pmd_alpha"]')).not.toBeNull()
  await click('Add manually · Primary')
  await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
  expect(document.body.textContent).toContain('Add upstream model')
})
it('cancels obsolete reads on actor change and does not expose a stale reply', async () => {
  holdUrl = '/admin/providers'
  await mount(false, false)
  await until(() => expect(held).toHaveLength(1))
  const obsolete = held[0]
  holdUrl = null
  actor = 'usr_new_actor'
  await renew([sessionKey])
  await until(() => expect(table()).not.toBeNull())
  expect(obsolete.signal?.aborted).toBe(true)
  await act(async () => obsolete.release())
  expect(cache.getQueryData<{ user: { id: string } }>(sessionKey)?.user.id).toBe('usr_new_actor')
  expect(names()).toHaveLength(4)
})
it('destroys visible rows on definitive null Session without extra catalogue reads', async () => {
  await mount()
  const count = requests.filter((request) => request.url === '/admin/providers').length
  await act(async () => cache.setQueryData(sessionKey, null))
  expect(table()).toBeNull()
  expect(button('Add manually · Primary')).toBeUndefined()
  expect(requests.filter((request) => request.url === '/admin/providers')).toHaveLength(count)
})
it('returns keyboard focus to the local filter trigger after Escape', async () => {
  await mount()
  const trigger = button('Filter model enabled state')
  await act(async () => {
    trigger.focus()
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
  })
  await until(() => expect(document.querySelector('[role="menu"]')).not.toBeNull())
  await act(async () =>
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
  )
  await until(() => expect(document.querySelector('[role="menu"]')).toBeNull())
  await until(() => expect(document.activeElement).toBe(trigger))
})
