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
import type { ProviderModelBindings } from '@/types/provider-model-bindings'
import ProviderModelTable from './provider-models'
import ProvidersPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let root: Root, host: HTMLDivElement, cache: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[], permissions: string[], actor: string
let permissionError: boolean, catalogueError: boolean, bindingError: boolean
let bindingPages: Record<string, ProviderModelBindings>
let cataloguePages: Provider[]
let patchFailure: number | null, ignorePatchAbort: boolean
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
  permissionError = catalogueError = bindingError = false
  bindingPages = {
    prv_first: {
      provider_id: 'prv_first',
      items: [
        {
          provider_model_id: 'pmd_alpha',
          connection_id: 'con_primary',
          binding_count: 2,
          models: [
            { id: 'mdl_a', name: 'current/name' },
            { id: 'mdl_b', name: null },
          ],
        },
        {
          provider_model_id: 'pmd_disabled',
          connection_id: 'con_primary',
          binding_count: 1,
          models: [{ id: 'mdl_disabled', name: 'Disabled/stored' }],
        },
        {
          provider_model_id: 'pmd_duplicate',
          connection_id: 'con_secondary',
          binding_count: 0,
          models: [],
        },
        {
          provider_model_id: 'pmd_literal',
          connection_id: 'con_secondary',
          binding_count: 0,
          models: [],
        },
      ],
    },
    prv_second: {
      provider_id: 'prv_second',
      items: [
        {
          provider_model_id: 'pmd_other',
          connection_id: 'con_other',
          binding_count: 1,
          models: [{ id: 'mdl_other', name: 'Other/current' }],
        },
      ],
    },
  }
  cataloguePages = structuredClone(providers)
  patchFailure = null
  ignorePatchAbort = false
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
    else if (config.method === 'patch' && config.url?.startsWith('/admin/provider-models/')) {
      const target = cataloguePages
        .flatMap((provider) =>
          provider.connections.flatMap((connection) => connection.provider_models),
        )
        .find((item) => item.id === config.url!.split('/').at(-1))!
      const input = JSON.parse(config.data)
      const status = patchFailure ?? (target.etag === input.etag ? null : 409)
      if (status)
        throw new AxiosError('Controlled status failure', '', config, undefined, {
          ...response,
          status,
        })
      target.enabled = input.enabled
      target.etag = 'saved-revision'
      response.data = structuredClone(target)
    } else if (config.url === '/admin/providers')
      response.data = { items: structuredClone(cataloguePages) }
    else if (/^\/admin\/providers\/prv_(first|second)\/model-bindings$/.test(config.url!)) {
      response.data = structuredClone(bindingPages[config.url!.split('/')[3]])
      response.headers.set('Cache-Control', 'no-store')
    } else throw new Error(`Unexpected directory request: ${config.url}`)
    if (
      (permissionError && config.url === '/auth/permissions') ||
      (catalogueError && config.url === '/admin/providers') ||
      (bindingError && config.url?.endsWith('/model-bindings'))
    )
      throw new AxiosError('Controlled read failure', '', config, undefined, {
        ...response,
        status: 503,
      })
    if (config.url === holdUrl)
      await new Promise<void>((resolve, reject) => {
        held.push({ release: resolve, signal: config.signal })
        if (!(config.method === 'patch' && ignorePatchAbort))
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
const table = (label = 'Provider models') =>
  host.querySelector<HTMLTableElement>(`table[aria-label="${label}"]`)
const names = (label = 'Provider models') =>
  [...table(label)!.querySelectorAll('tbody tr')]
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
  expect([...row.children].map((item) => item.textContent).slice(4, 7)).toEqual([
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

const bindingKeys = () => catalogueKeys().filter((key) => key[4] === 'model-bindings')
const logicalNames = (label = 'Provider models') =>
  [...table(label)!.querySelectorAll('a[href^="/admin/models/"]')].map((link) => link.textContent)
const bindingUrl = '/admin/providers/prv_first/model-bindings'

it('shows complete current stored Model names and null-ID fallback from one scoped projection', async () => {
  permissions.push('models.read_all')
  await mount()
  await until(() => expect(logicalNames()).toEqual(['current/name', 'mdl_b', 'Disabled/stored']))
  expect(requests.filter((r) => r.url === bindingUrl)).toHaveLength(1)
  expect(requests.some((r) => r.url === '/admin/models' || /^\/admin\/models\//.test(r.url!))).toBe(
    false,
  )
  expect(table()!.querySelector('a[href="/admin/models/mdl_b"]')).not.toBeNull()
  expect(host.textContent).toContain('including disabled Models and zero weights')
})
it('keeps Provider-only rows while binding facts are Unknown and restricted filters cannot apply', async () => {
  await mount()
  expect(names()).toHaveLength(4)
  expect(logicalNames()).toEqual([])
  expect(requests.some((r) => r.url?.endsWith('/model-bindings'))).toBe(false)
  await click('Filter stored Model bindings')
  await until(() => expect(button('Bound')).toBeTruthy())
  expect(button('Bound').getAttribute('aria-disabled')).toBe('true')
  expect(button('Unbound').getAttribute('aria-disabled')).toBe('true')
  await click('Bound')
  expect(names()).toHaveLength(4)
  expect(host.textContent).toContain('Unknown is not Unbound')
})
it('intersects complete Bound/Unbound with literal search, Connection and stored enabled controls', async () => {
  permissions.push('models.read_all')
  await mount()
  await until(() => expect(logicalNames()).toHaveLength(3))
  await select('Filter stored Model bindings', 'Bound')
  expect(names()).toEqual(['Alpha', 'alpha disabled'])
  await select('Filter model enabled state', 'Disabled')
  expect(names()).toEqual(['alpha disabled'])
  await select('Filter stored Model bindings', 'Unbound')
  expect(names()).toEqual(['literal %_[test]'])
  await select('Filter model Connection', 'Secondary')
  await search('%_[test]')
  expect(names()).toEqual(['literal %_[test]'])
})
it('preserves binding filters and current names on live language switching', async () => {
  permissions.push('models.read_all')
  await mount()
  await until(() => expect(logicalNames()).toHaveLength(3))
  await select('Filter stored Model bindings', 'Bound')
  const before = requests.length
  await act(async () => i18n.changeLanguage('zh'))
  expect(table('供应商模型')).not.toBeNull()
  expect(names('供应商模型')).toEqual(['Alpha', 'alpha disabled'])
  expect(host.textContent).toContain('已绑定')
  expect(logicalNames('供应商模型')).toEqual(['current/name', 'mdl_b', 'Disabled/stored'])
  expect(requests).toHaveLength(before)
  await act(async () => i18n.changeLanguage('en'))
  expect(host.textContent).toContain('Bound')
})
it('hides binding names/filter facts during permission renewal and never requests them after Model-read removal', async () => {
  permissions.push('models.read_all')
  await mount()
  await until(() => expect(logicalNames()).toHaveLength(3))
  holdUrl = '/auth/permissions'
  permissions = ['providers.read', 'providers.write']
  await renew(permissionKeys())
  await until(() => expect(held).toHaveLength(1))
  expect(table()).toBeNull()
  holdUrl = null
  await act(async () => held[0].release())
  await until(() => expect(table()).not.toBeNull())
  expect(names()).toHaveLength(4)
  expect(logicalNames()).toEqual([])
  expect(requests.filter((r) => r.url === bindingUrl)).toHaveLength(1)
})
it('keeps only Unknown binding cells during an independent projection renewal or failure', async () => {
  permissions.push('models.read_all')
  await mount()
  await until(() => expect(logicalNames()).toHaveLength(3))
  await select('Filter stored Model bindings', 'Bound')
  holdUrl = bindingUrl
  await renew(bindingKeys())
  await until(() => expect(held).toHaveLength(1))
  expect(table()).not.toBeNull()
  expect(names()).toHaveLength(4)
  expect(logicalNames()).toEqual([])
  holdUrl = null
  await act(async () => held[0].release())
  await until(() => expect(names()).toHaveLength(2))
  bindingError = true
  await renew(bindingKeys())
  await until(() => expect(host.textContent).toContain('Model bindings could not be confirmed'))
  expect(names()).toHaveLength(4)
  expect(logicalNames()).toEqual([])
  bindingError = false
  await click('Refresh Model bindings')
  await until(() => expect(names()).toHaveLength(2))
})
it.each(['missing', 'extra', 'connection'])(
  'rejects a %s catalogue/projection set mismatch before names or filtering',
  async (kind) => {
    permissions.push('models.read_all')
    if (kind === 'missing') bindingPages.prv_first.items.pop()
    if (kind === 'extra')
      bindingPages.prv_first.items.push({
        provider_model_id: 'pmd_z_extra',
        connection_id: 'con_primary',
        binding_count: 0,
        models: [],
      })
    if (kind === 'connection') bindingPages.prv_first.items[0].connection_id = 'con_secondary'
    await mount()
    await until(() => expect(host.textContent).toContain('Provider Model set has changed'))
    expect(names()).toHaveLength(4)
    expect(logicalNames()).toEqual([])
    await click('Filter stored Model bindings')
    await until(() => expect(button('Bound')).toBeTruthy())
    expect(button('Bound').getAttribute('aria-disabled')).toBe('true')
  },
)
it('cancels obsolete binding projection on Provider change and resets the binding filter', async () => {
  permissions.push('models.read_all')
  holdUrl = bindingUrl
  await mount()
  await until(() => expect(held).toHaveLength(1))
  const stale = held[0]
  holdUrl = null
  await act(async () => router.navigate('/admin/providers/prv_second?tab=models'))
  await until(() => expect(logicalNames()).toEqual(['Other/current']))
  expect(stale.signal?.aborted).toBe(true)
  await act(async () => stale.release())
  expect(names()).toEqual(['Other model'])
  expect(logicalNames()).toEqual(['Other/current'])
  expect(button('Filter stored Model bindings').textContent).toBe('All bindings')
})
it('cancels an old actor binding projection and uses new actor/current authority only', async () => {
  permissions.push('models.read_all')
  holdUrl = bindingUrl
  await mount()
  await until(() => expect(held).toHaveLength(1))
  const stale = held[0]
  actor = 'usr_new_actor'
  holdUrl = null
  await renew([sessionKey])
  await until(() => expect(logicalNames()).toHaveLength(3))
  expect(stale.signal?.aborted).toBe(true)
  await act(async () => stale.release())
  expect(cache.getQueryData<{ user: { id: string } }>(sessionKey)?.user.id).toBe('usr_new_actor')
  expect(logicalNames()).toHaveLength(3)
})
it('fetches a new complete projection after catalogue renewal and blocks stale links immediately', async () => {
  permissions.push('models.read_all')
  await mount()
  await until(() => expect(logicalNames()).toHaveLength(3))
  const old = table()!.querySelector<HTMLAnchorElement>('a[href="/admin/models/mdl_a"]')!
  holdUrl = '/admin/providers'
  await act(async () => {
    for (const key of catalogueKeys().filter((key) => key[4] === 'provider-models'))
      void cache.invalidateQueries({ queryKey: key, exact: true })
    old.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    expect(router.state.location.pathname).toBe('/admin/providers/prv_first')
  })
  await until(() => expect(held).toHaveLength(1))
  expect(table()).toBeNull()
  holdUrl = null
  await act(async () => held[0].release())
  await until(() => expect(logicalNames()).toHaveLength(3))
  expect(requests.filter((r) => r.url === bindingUrl)).toHaveLength(2)
})
it('supports keyboard binding selector and restores focus after Escape', async () => {
  permissions.push('models.read_all')
  await mount()
  await until(() => expect(logicalNames()).toHaveLength(3))
  const trigger = button('Filter stored Model bindings')
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

it('refreshes a mismatched catalogue before exactly one fresh complete binding projection', async () => {
  permissions.push('models.read_all')
  bindingPages.prv_first.items.push({
    provider_model_id: 'pmd_z_new',
    connection_id: 'con_primary',
    binding_count: 1,
    models: [{ id: 'mdl_z_new', name: 'New/current' }],
  })
  await mount()
  await until(() => expect(host.textContent).toContain('Provider Model set has changed'))
  expect(logicalNames()).toEqual([])
  expect(names()).toHaveLength(4)
  expect(requests.filter((r) => r.url === bindingUrl)).toHaveLength(1)
  cataloguePages[0].connections[0].provider_models.push(model('pmd_z_new', 'New model', true))
  holdUrl = '/admin/providers'
  await click('Refresh Model bindings')
  await until(() => expect(held).toHaveLength(1))
  expect(table()).toBeNull()
  expect(button('Filter stored Model bindings')).toBeUndefined()
  expect(host.querySelector('a[href="/admin/models/mdl_z_new"]')).toBeNull()
  expect(requests.filter((r) => r.url === bindingUrl)).toHaveLength(1)
  holdUrl = null
  await act(async () => held[0].release())
  await until(() =>
    expect(logicalNames()).toEqual(['current/name', 'mdl_b', 'Disabled/stored', 'New/current']),
  )
  expect(names()).toEqual(['Alpha', 'alpha disabled', 'New model', 'Alpha', 'literal %_[test]'])
  expect(requests.filter((r) => r.url === '/admin/providers')).toHaveLength(2)
  expect(requests.filter((r) => r.url === bindingUrl)).toHaveLength(2)
  await select('Filter stored Model bindings', 'Bound')
  expect(names()).toEqual(['Alpha', 'alpha disabled', 'New model'])
})

const statusWrites = () => requests.filter((request) => request.method === 'patch')
const statusDialog = () => document.querySelector<HTMLElement>('[role="dialog"]')
const alphaActions = () => table()!.querySelectorAll<HTMLButtonElement>('tbody tr button')[0]
async function openAlphaStatus() {
  await act(async () => alphaActions().click())
  await until(() => expect(button('Disable model')).toBeTruthy())
  await click('Disable model')
  await until(() => expect(statusDialog()).not.toBeNull())
}
it('confirms one exact row status-only write and refreshes recorded table and detail queries', async () => {
  await mount()
  cache.setQueryData(
    ['admin', 'providers', actor, 'prv_first', 'pmd_alpha', 'member', 1],
    providers,
  )
  cache.setQueryData(['admin', 'providers'], providers)
  await openAlphaStatus()
  expect(statusWrites()).toHaveLength(0)
  expect(statusDialog()!.textContent).toContain('Existing relationships and prices are retained')
  await click('Confirm disable')
  await until(() => expect(host.textContent).toContain('The model status response was confirmed'))
  expect(statusWrites()).toHaveLength(1)
  const write = statusWrites()[0]
  expect(write.url).toBe('/admin/provider-models/pmd_alpha')
  expect(JSON.parse(write.data)).toEqual({ etag: 'recorded-revision', enabled: false })
  expect(write.headers.get('X-CSRF-Token')).toBe('current-csrf')
  expect(write.headers.get('If-Match')).toBeUndefined()
  await until(() =>
    expect(table()!.querySelector('tbody tr')!.children[4].textContent).toBe('Disabled'),
  )
  expect(
    cache.getQueryState(['admin', 'providers', actor, 'prv_first', 'pmd_alpha', 'member', 1])
      ?.isInvalidated,
  ).toBe(true)
  expect(cache.getQueryState(['admin', 'providers'])?.isInvalidated).toBe(true)
  expect(cataloguePages[0].connections[0].provider_models[1].supports_image_input).toBe(true)
  expect(cataloguePages[0].connections[0].provider_models[1].supports_pdf_input).toBe(true)
})
it('cancels the confirmation without dispatch and restores the exact row trigger', async () => {
  await mount()
  await openAlphaStatus()
  await click('Cancel')
  await until(() => expect(statusDialog()).toBeNull())
  expect(statusWrites()).toHaveLength(0)
  await until(() => expect(document.activeElement).toBe(alphaActions()))
})
it('keeps details available with read-only authority and exposes only scoped bound Model actions', async () => {
  permissions = ['providers.read', 'models.read_all']
  await mount()
  await until(() => expect(logicalNames()).toHaveLength(3))
  await act(async () => alphaActions().click())
  await until(() => expect(button('View model details')).toBeTruthy())
  expect(button('Disable model').getAttribute('aria-disabled')).toBe('true')
  expect(button('Manage current/name')).toBeTruthy()
  expect(button('Manage mdl_b')).toBeTruthy()
  expect(statusWrites()).toHaveLength(0)
  expect(requests.some((request) => request.url === '/admin/models')).toBe(false)
})
it('localizes an open confirmation without changing reviewed identity or submitting', async () => {
  await mount()
  await openAlphaStatus()
  const before = requests.length
  await act(async () => i18n.changeLanguage('zh'))
  expect(statusDialog()!.textContent).toContain('停用模型？')
  expect(statusDialog()!.textContent).toContain('Alpha')
  expect(button('确认停用')).toBeTruthy()
  expect(requests).toHaveLength(before)
  await act(async () => i18n.changeLanguage('en'))
  expect(button('Confirm disable')).toBeTruthy()
})
it('requires explicit conflict review and preserves requested status after current state changes', async () => {
  await mount()
  await openAlphaStatus()
  patchFailure = 409
  cataloguePages[0].connections[0].provider_models[0].etag = 'conflict-revision'
  cataloguePages[0].connections[0].provider_models[0].enabled = false
  await click('Confirm disable')
  await until(() => expect(button('Review current configuration')).toBeTruthy())
  expect(button('Confirm disable').hasAttribute('disabled')).toBe(true)
  await click('Review current configuration')
  expect(button('Confirm disable').hasAttribute('disabled')).toBe(false)
  patchFailure = null
  await click('Confirm disable')
  await until(() => expect(statusWrites()).toHaveLength(2))
  expect(JSON.parse(statusWrites()[1].data)).toEqual({ etag: 'conflict-revision', enabled: false })
})
it('retains identical uncertain request through dismissal, matching refresh, and repeated conflict retry', async () => {
  await mount()
  await openAlphaStatus()
  patchFailure = 503
  await click('Confirm disable')
  await until(() =>
    expect(button('Retry identical status change').hasAttribute('disabled')).toBe(false),
  )
  const original = statusWrites()[0].data
  await click('Cancel')
  expect(statusDialog()).toBeNull()
  const target = cataloguePages[0].connections[0].provider_models[0]
  target.etag = 'unknown-new-revision'
  target.enabled = false
  await renew(catalogueKeys().filter((key) => key[4] === 'provider-models'))
  await until(() => expect(table()).not.toBeNull())
  await act(async () => alphaActions().click())
  await until(() => expect(button('Enable model')).toBeTruthy())
  await click('Enable model')
  await until(() => expect(button('Retry identical status change')).toBeTruthy())
  expect(statusDialog()!.textContent).toContain('Disable model?')
  patchFailure = 409
  await click('Retry identical status change')
  await until(() => expect(statusWrites()).toHaveLength(2))
  expect(statusWrites()[1].data).toBe(original)
  expect(statusDialog()!.textContent).toContain('unconfirmed')
  expect(button('Review current configuration')).toBeUndefined()
  expect(host.textContent).not.toContain('The model status response was confirmed')
})
it('retries an unconfirmed write with fresh same-actor Session authority and the original body', async () => {
  await mount()
  await openAlphaStatus()
  patchFailure = 503
  await click('Confirm disable')
  await until(() =>
    expect(button('Retry identical status change').hasAttribute('disabled')).toBe(false),
  )
  const original = statusWrites()[0].data
  await renew([sessionKey])
  await until(() => expect(statusDialog()).not.toBeNull())
  patchFailure = null
  await click('Retry identical status change')
  await until(() => expect(host.textContent).toContain('The model status response was confirmed'))
  expect(statusWrites()[1].data).toBe(original)
})
it.each(['session', 'permission', 'actor', 'provider', 'unmount'])(
  'rejects a late successful status callback after %s lifetime changes',
  async (kind) => {
    await mount()
    await openAlphaStatus()
    holdUrl = '/admin/provider-models/pmd_alpha'
    ignorePatchAbort = true
    await click('Confirm disable')
    await until(() => expect(held).toHaveLength(1))
    const stale = held[0]
    holdUrl = null
    const before = requests.filter((request) => request.url === '/admin/providers').length
    if (kind === 'unmount') await act(async () => root.render(null))
    else if (kind === 'provider')
      await act(async () => router.navigate('/admin/providers/prv_second?tab=models'))
    else if (kind === 'permission') {
      permissions = ['providers.read']
      await renew(permissionKeys())
    } else {
      if (kind === 'actor') actor = 'usr_changed'
      await renew([sessionKey])
    }
    await until(() => expect(stale.signal?.aborted).toBe(true))
    if (kind !== 'unmount') await until(() => expect(table()).not.toBeNull())
    const afterRenewal = requests.filter((request) => request.url === '/admin/providers').length
    await act(async () => stale.release())
    expect(host.textContent).not.toContain('The model status response was confirmed')
    expect(requests.filter((request) => request.url === '/admin/providers')).toHaveLength(
      afterRenewal,
    )
    expect(afterRenewal).toBeGreaterThanOrEqual(before)
    if (kind === 'session') {
      expect(statusDialog()!.textContent).toContain('unconfirmed')
      expect(button('Retry identical status change').hasAttribute('disabled')).toBe(false)
    }
  },
)
it('enables the exact disabled row without rewriting its image or PDF declarations', async () => {
  await mount()
  const trigger = table()!.querySelectorAll<HTMLButtonElement>('tbody tr button')[1]
  await act(async () => trigger.click())
  await until(() => expect(button('Enable model')).toBeTruthy())
  await click('Enable model')
  await until(() => expect(button('Confirm enable')).toBeTruthy())
  await click('Confirm enable')
  await until(() => expect(host.textContent).toContain('The model status response was confirmed'))
  expect(statusWrites()[0].url).toBe('/admin/provider-models/pmd_disabled')
  expect(JSON.parse(statusWrites()[0].data)).toEqual({ etag: 'recorded-revision', enabled: true })
  const target = cataloguePages[0].connections[0].provider_models[1]
  expect(target.enabled).toBe(true)
  expect(target.supports_image_input).toBe(true)
  expect(target.supports_pdf_input).toBe(true)
})
it('targets a second duplicate upstream name by its immutable row ID', async () => {
  await mount()
  const trigger = table()!.querySelectorAll<HTMLButtonElement>('tbody tr button')[2]
  await act(async () => trigger.click())
  await until(() => expect(button('Disable model')).toBeTruthy())
  await click('Disable model')
  await until(() => expect(button('Confirm disable')).toBeTruthy())
  await click('Confirm disable')
  await until(() => expect(statusWrites()).toHaveLength(1))
  expect(statusWrites()[0].url).toBe('/admin/provider-models/pmd_duplicate')
  expect(cataloguePages[0].connections[0].provider_models[0].enabled).toBe(true)
})
it('blocks dispatch from obsolete confirmation DOM immediately upon Session invalidation', async () => {
  await mount()
  await openAlphaStatus()
  const submit = button('Confirm disable')
  holdUrl = '/auth/session'
  await act(async () => {
    void cache.invalidateQueries({ queryKey: sessionKey, exact: true })
    submit.click()
  })
  await until(() => expect(held).toHaveLength(1))
  expect(statusWrites()).toHaveLength(0)
  expect(statusDialog()).toBeNull()
  holdUrl = null
  await act(async () => held[0].release())
  await until(() => expect(statusDialog()).not.toBeNull())
  await click('Cancel')
  await until(() => expect(document.activeElement).toBe(alphaActions()))
})
it('keeps unbound menus scoped to details and status without an invented joining action', async () => {
  permissions.push('models.read_all')
  await mount()
  await until(() => expect(logicalNames()).toHaveLength(3))
  await act(async () => table()!.querySelectorAll<HTMLButtonElement>('tbody tr button')[2].click())
  await until(() => expect(button('Disable model')).toBeTruthy())
  const menu = document.querySelector('[role="menu"]')!
  expect([...menu.querySelectorAll('[role="menuitem"]')].map((item) => item.textContent)).toEqual([
    'View model details',
    'Disable model',
  ])
  expect(requests.some((request) => request.url === '/admin/models')).toBe(false)
})
it('locks duplicate confirmation before React rerenders', async () => {
  await mount()
  await openAlphaStatus()
  holdUrl = '/admin/provider-models/pmd_alpha'
  const submit = button('Confirm disable')
  await act(async () => {
    submit.click()
    submit.click()
  })
  await until(() => expect(held).toHaveLength(1))
  expect(statusWrites()).toHaveLength(1)
  holdUrl = null
  await act(async () => held[0].release())
  await until(() => expect(statusDialog()).toBeNull())
})
it('does not recreate private query entries after a null Session and late status response', async () => {
  await mount()
  await openAlphaStatus()
  holdUrl = '/admin/provider-models/pmd_alpha'
  ignorePatchAbort = true
  await click('Confirm disable')
  await until(() => expect(held).toHaveLength(1))
  const stale = held[0]
  await act(async () => cache.setQueryData(sessionKey, null))
  await until(() => expect(stale.signal?.aborted).toBe(true))
  await act(async () => cache.removeQueries({ queryKey: ['admin'] }))
  const reads = requests.filter((request) => request.url === '/admin/providers').length
  await act(async () => stale.release())
  // Disabled mounted observers can recreate empty query shells, but no private facts.
  expect(
    cache
      .getQueryCache()
      .findAll({ queryKey: ['admin'] })
      .every((query) => query.state.data === undefined),
  ).toBe(true)
  expect(requests.filter((request) => request.url === '/admin/providers')).toHaveLength(reads)
  expect(statusDialog()).toBeNull()
  expect(table()).toBeNull()
  expect(host.textContent).not.toContain('The model status response was confirmed')
})
it('explicitly leaves lost-response history unknown before reviewing and confirming a separate current-state change', async () => {
  await mount()
  await openAlphaStatus()
  patchFailure = 503
  await click('Confirm disable')
  await until(() =>
    expect(button('Retry identical status change').hasAttribute('disabled')).toBe(false),
  )
  const original = statusWrites()[0].data
  const target = cataloguePages[0].connections[0].provider_models[0]
  target.enabled = false
  target.etag = 'applied-but-lost-revision'
  patchFailure = null
  await click('Retry identical status change')
  await until(() => expect(statusWrites()).toHaveLength(2))
  expect(statusWrites()[1].data).toBe(original)
  expect(statusDialog()!.textContent).toContain('unconfirmed')
  const before = requests.filter((request) => request.url === '/admin/providers').length
  await click('Review a separate status change')
  await until(() => expect(button('Discard retry and review current state')).toBeTruthy())
  expect(requests.filter((request) => request.url === '/admin/providers')).toHaveLength(before + 1)
  expect(statusDialog()!.textContent).toContain('Currently recorded: Disabled')
  expect(statusDialog()!.textContent).toContain('does not cancel or prove the original operation')
  expect(statusWrites()).toHaveLength(2)
  await click('Cancel')
  expect(button('Retry identical status change')).toBeTruthy()
  await click('Retry identical status change')
  await until(() => expect(statusWrites()).toHaveLength(3))
  expect(statusWrites()[2].data).toBe(original)
  await click('Review a separate status change')
  await until(() => expect(button('Discard retry and review current state')).toBeTruthy())
  await click('Discard retry and review current state')
  expect(statusWrites()).toHaveLength(3)
  expect(statusDialog()!.textContent).toContain('The earlier request remains unconfirmed')
  expect(button('Confirm enable')).toBeTruthy()
  await click('Confirm enable')
  await until(() => expect(host.textContent).toContain('The model status response was confirmed'))
  expect(JSON.parse(statusWrites()[3].data)).toEqual({
    etag: 'applied-but-lost-revision',
    enabled: true,
  })
  expect(statusWrites()[3].url).toBe('/admin/provider-models/pmd_alpha')
})
it('does not discard an uncertain intent from obsolete separate-review DOM during read renewal', async () => {
  await mount()
  await openAlphaStatus()
  patchFailure = 503
  await click('Confirm disable')
  await until(() =>
    expect(button('Retry identical status change').hasAttribute('disabled')).toBe(false),
  )
  await click('Review a separate status change')
  await until(() => expect(button('Discard retry and review current state')).toBeTruthy())
  const discard = button('Discard retry and review current state')
  holdUrl = '/admin/providers'
  await act(async () => {
    for (const key of catalogueKeys().filter((key) => key[4] === 'provider-models'))
      void cache.invalidateQueries({ queryKey: key, exact: true })
    discard.click()
  })
  await until(() => expect(held).toHaveLength(1))
  expect(statusDialog()).toBeNull()
  holdUrl = null
  await act(async () => held[0].release())
  await until(() => expect(button('Discard retry and review current state')).toBeTruthy())
  await click('Cancel')
  expect(button('Retry identical status change')).toBeTruthy()
  expect(statusDialog()!.textContent).toContain('unconfirmed')
  expect(statusWrites()).toHaveLength(1)
})

interface CapturedRowFiber {
  return?: CapturedRowFiber
  type?: { name?: string }
  memoizedProps?: { model: ProviderModel; onStatus: () => void }
}
function captureStatusCallback(index: number, id: string) {
  // Exercise a previously captured row callback even while its menu is closed.
  const trigger = table()!.querySelectorAll<HTMLButtonElement>('tbody tr button')[index]
  const key = Object.keys(trigger).find((name) => name.startsWith('__reactFiber$'))!
  let fiber: CapturedRowFiber | undefined = (
    trigger as unknown as Record<string, CapturedRowFiber>
  )[key]
  while (fiber && fiber.type?.name !== 'ProviderModelRowMenu') fiber = fiber.return
  expect(fiber?.memoizedProps?.model.id).toBe(id)
  return fiber!.memoizedProps!.onStatus
}
it('locks captured Cancel synchronously when confirmation dispatches in the same turn', async () => {
  await mount()
  await openAlphaStatus()
  holdUrl = '/admin/provider-models/pmd_alpha'
  ignorePatchAbort = true
  const submit = button('Confirm disable')
  const cancel = button('Cancel')
  await act(async () => {
    submit.click()
    cancel.click()
  })
  await until(() => expect(held).toHaveLength(1))
  expect(statusWrites()).toHaveLength(1)
  expect(statusDialog()!.textContent).toContain('Disable model?')
  expect(held[0].signal?.aborted).not.toBe(true)
  const original = statusWrites()[0].data
  holdUrl = null
  await renew([sessionKey])
  await until(() => expect(held[0].signal?.aborted).toBe(true))
  await until(() => expect(button('Retry identical status change')).toBeTruthy())
  await act(async () => held[0].release())
  expect(statusDialog()!.textContent).toContain('unconfirmed')
  expect(host.textContent).not.toContain('The model status response was confirmed')
  await click('Retry identical status change')
  await until(() => expect(statusWrites()).toHaveLength(2))
  expect(statusWrites()[1].data).toBe(original)
})
it('locks captured other-row callbacks synchronously when confirmation dispatches in the same turn', async () => {
  await mount()
  await openAlphaStatus()
  const changeRow = captureStatusCallback(1, 'pmd_disabled')
  holdUrl = '/admin/provider-models/pmd_alpha'
  ignorePatchAbort = true
  const submit = button('Confirm disable')
  await act(async () => {
    submit.click()
    changeRow()
  })
  await until(() => expect(held).toHaveLength(1))
  expect(statusWrites()).toHaveLength(1)
  expect(statusWrites()[0].url).toBe('/admin/provider-models/pmd_alpha')
  expect(statusDialog()!.textContent).toContain('Disable model?')
  expect(held[0].signal?.aborted).not.toBe(true)
  holdUrl = null
  await act(async () => held[0].release())
  await until(() => expect(host.textContent).toContain('The model status response was confirmed'))
  expect(cataloguePages[0].connections[0].provider_models[1].enabled).toBe(false)
})
it('rejects captured confirmation after a same-turn cancellation closes the target', async () => {
  await mount()
  await openAlphaStatus()
  const submit = button('Confirm disable')
  const cancel = button('Cancel')
  await act(async () => {
    cancel.click()
    submit.click()
  })
  expect(statusWrites()).toHaveLength(0)
  expect(statusDialog()).toBeNull()
})
it('rejects captured confirmation after a same-turn row change replaces its target', async () => {
  await mount()
  await openAlphaStatus()
  const changeRow = captureStatusCallback(1, 'pmd_disabled')
  const submit = button('Confirm disable')
  await act(async () => {
    changeRow()
    submit.click()
  })
  expect(statusWrites()).toHaveLength(0)
  expect(statusDialog()!.textContent).toContain('Enable model?')
  expect(statusDialog()!.textContent).toContain('alpha disabled')
})
