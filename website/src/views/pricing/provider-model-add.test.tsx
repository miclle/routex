import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import { Button } from '@/components/ui/button'
import ProviderModelPage from './provider-model'

vi.mock('./model-price-table', () => ({ default: () => null }))
vi.mock('./provider-model-state', () => ({ default: () => null }))
vi.mock('./provider-model-capacity', () => ({ default: () => null }))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

let root: Root, host: HTMLDivElement, cache: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[]
let actor: string, role: string, csrf: string, connectionId: string
let permissions: string[], bound: boolean, removed: boolean, modelFailure: number
const original = client.defaults.adapter

beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  requests = []
  actor = 'usr_reviewer'
  role = 'admin'
  csrf = 'csrf-current'
  connectionId = 'con_exact'
  permissions = ['providers.read', 'models.read_all']
  bound = false
  removed = false
  modelFailure = 0
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session')
      response.data = {
        user: { id: actor, name: 'Reviewer', email: 'reviewer@example.test', role },
        csrf_token: csrf,
      }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/admin/providers')
      response.data = {
        items: [
          {
            id: 'prv_one',
            name: 'Provider',
            connections: [
              {
                id: 'con_duplicate',
                name: 'Connection',
                protocol: 'openai_chat',
                base_url: 'https://example.test',
                credentials: [],
                provider_models: [],
              },
              {
                id: connectionId,
                name: 'Connection',
                protocol: 'openai_chat',
                enabled: false,
                base_url: 'https://example.test',
                credentials: [],
                provider_models: removed
                  ? []
                  : [
                      {
                        id: 'pmo_one',
                        upstream_name: 'Exact source',
                        enabled: false,
                        supports_image_input: false,
                        supports_pdf_input: false,
                        etag: '0',
                      },
                    ],
              },
            ],
          },
        ],
      }
    else if (config.url === '/admin/models') {
      if (modelFailure) {
        response.status = modelFailure
        throw new AxiosError('model list unavailable', '', config, undefined, response)
      }
      response.data = {
        items: bound
          ? [
              {
                id: 'mdl_existing',
                name: 'Existing target',
                status: 'active',
                names: [],
                granted_user_ids: [],
                bindings: [
                  {
                    id: 'bnd_one',
                    provider_model_id: 'pmo_one',
                    provider_id: 'prv_one',
                    connection_id: connectionId,
                    upstream_name: 'Exact source',
                    protocol: 'openai_chat',
                    weight: 0,
                    ready: false,
                  },
                ],
              },
            ]
          : [],
      }
    } else throw new Error(`Unexpected request ${config.method} ${config.url}`)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  await i18n.changeLanguage('en')
})
async function until(assert: () => void) {
  for (let index = 0; index < 100; index++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      assert()
      return
    } catch (error) {
      if (index === 99) throw error
    }
  }
}
async function mount() {
  router = createMemoryRouter(
    [
      { path: '/admin/providers/:providerId/models/:modelId', element: <ProviderModelPage /> },
      { path: '/admin/models/new', element: <p>Guided review</p> },
      { path: '/other', element: <p>Other</p> },
    ],
    { initialEntries: ['/admin/providers/prv_one/models/pmo_one'] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain('Exact source'))
  await until(() => expect(cache.isFetching()).toBe(0))
}
function control(label = 'Add to model') {
  return [...host.querySelectorAll('button')].find((item) => item.textContent === label)
}
function capture() {
  interface Fiber {
    type: unknown
    return: Fiber | null
    memoizedProps: { onClick?: () => void }
  }
  const button = control()!
  expect(button).toBeTruthy()
  const key = Object.keys(button).find((value) => value.startsWith('__reactFiber$'))!
  let fiber = (button as unknown as Record<string, Fiber>)[key]
  while (fiber && fiber.type !== Button) fiber = fiber.return!
  expect(fiber?.memoizedProps.onClick).toBeTypeOf('function')
  return fiber.memoizedProps.onClick!
}
const entered = () => router.state.location.pathname === '/admin/models/new'
const writes = () => requests.filter((request) => request.method !== 'get')
const targetQuery = (kind: string) => (query: { queryKey: readonly unknown[] }) =>
  kind === 'Session'
    ? JSON.stringify(query.queryKey) === JSON.stringify(sessionKey)
    : kind === 'permissions'
      ? query.queryKey[0] === 'permissions'
      : query.queryKey[0] === 'admin' && query.queryKey[1] === kind

describe('Unbound provider-model guided entry', () => {
  it.each([
    ['independent readers', ['providers.read', 'models.read_all'], true],
    ['Model writer', ['providers.read', 'models.read_all', 'models.write'], true],
    ['Provider writer without Model read', ['providers.read', 'providers.write'], false],
    ['Model writer without Model read', ['providers.read', 'models.write'], false],
  ])('preserves review/submit separation for %s', async (_, grants, allowed) => {
    permissions = grants as string[]
    await mount()
    expect(!!control()).toBe(allowed)
    if (allowed) {
      await act(async () => control()!.click())
      expect(router.state.location.search).toBe('?connectionId=con_exact')
    } else {
      expect(requests.some((request) => request.url === '/admin/models')).toBe(false)
      expect(host.textContent).not.toContain('This provider model is not bound')
    }
    expect(entered()).toBe(allowed)
    expect(writes()).toHaveLength(0)
    expect(
      requests.some((request) => /verify|discover|grant|creation/.test(request.url ?? '')),
    ).toBe(false)
  })
  it('keeps exact source identity with duplicate names and disabled Connection/model facts', async () => {
    connectionId = 'con_exact/?#&= value'
    await mount()
    expect(host.textContent).toContain('Disabled')
    await act(async () => control()!.click())
    expect([...new URLSearchParams(router.state.location.search)]).toEqual([
      ['connectionId', connectionId],
    ])
    expect(router.state.location.search).toBe(`?connectionId=${encodeURIComponent(connectionId)}`)
    expect(writes()).toHaveLength(0)
  })
  it('preserves bound-target management instead of presenting an unbound entry', async () => {
    bound = true
    await mount()
    expect(control()).toBeUndefined()
    expect(host.querySelector('a[href="/admin/models/mdl_existing"]')?.textContent).toBe(
      'Manage Existing target',
    )
    expect(host.textContent).not.toContain('This provider model is not bound')
  })
  it('does not expose Provider details or fetch their catalogue without Provider read', async () => {
    permissions = ['models.read_all', 'models.write']
    router = createMemoryRouter([{ path: '*', element: <ProviderModelPage /> }])
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await until(() =>
      expect(requests.some((request) => request.url === '/auth/permissions')).toBe(true),
    )
    await until(() => expect(cache.isFetching()).toBe(0))
    expect(control()).toBeUndefined()
    expect(requests.some((request) => request.url === '/admin/providers')).toBe(false)
  })
  it('matches the guided creation administrator Session gate independently from permission strings', async () => {
    role = 'member'
    await mount()
    expect(control()).toBeUndefined()
    expect(host.textContent).toContain('This provider model is not bound')
  })
  it.each(['Session', 'permissions', 'providers', 'models'])(
    'hides unknown unbound state and synchronously rejects the old action on %s invalidation',
    async (kind) => {
      await mount()
      const invoke = capture()
      await act(async () => {
        await cache.invalidateQueries({ predicate: targetQuery(kind), refetchType: 'none' })
        invoke()
      })
      expect(entered()).toBe(false)
      expect(control()).toBeUndefined()
      expect(host.textContent).not.toContain('This provider model is not bound')
      expect(writes()).toHaveLength(0)
    },
  )
  it.each([
    'actor',
    'renewal',
    'target',
    'unmount',
    'expiry',
    'removed source',
    'new binding',
    'Model read revoked',
    'Model read error',
  ])('rejects a captured action after %s changes the review lifetime', async (kind) => {
    await mount()
    const invoke = capture()
    await act(async () => {
      if (kind === 'actor') {
        actor = 'usr_other'
        await cache.invalidateQueries({ queryKey: sessionKey })
      } else if (kind === 'renewal') {
        csrf = 'csrf-renewed'
        await cache.invalidateQueries({ queryKey: sessionKey })
      } else if (kind === 'target')
        await router.navigate('/admin/providers/prv_other/models/pmo_other')
      else if (kind === 'unmount') await router.navigate('/other')
      else if (kind === 'expiry') window.dispatchEvent(new Event('routex:session-expired'))
      else if (kind === 'removed source') {
        removed = true
        await cache.invalidateQueries({ predicate: targetQuery('providers') })
      } else if (kind === 'new binding') {
        bound = true
        await cache.invalidateQueries({ predicate: targetQuery('models') })
      } else if (kind === 'Model read revoked') {
        permissions = ['providers.read']
        await cache.invalidateQueries({ predicate: targetQuery('permissions') })
      } else {
        modelFailure = 503
        await cache.invalidateQueries({ predicate: targetQuery('models') })
      }
    })
    await act(async () => invoke())
    expect(entered()).toBe(false)
    expect(writes()).toHaveLength(0)
    if (kind === 'Model read error')
      expect(host.textContent).not.toContain('This provider model is not bound')
  })
  it('allows a newly authorized action after renewal while rejecting its captured predecessor', async () => {
    await mount()
    const invoke = capture()
    await act(async () => {
      csrf = 'csrf-renewed'
      await cache.invalidateQueries({ queryKey: sessionKey })
    })
    await until(() => expect(control()).toBeTruthy())
    await act(async () => invoke())
    expect(entered()).toBe(false)
    await act(async () => control()!.click())
    expect(entered()).toBe(true)
    expect(writes()).toHaveLength(0)
  })
  it('switches the existing notice and action live without more reads or mutations', async () => {
    await mount()
    const count = requests.length
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain('该供应商模型尚未接入对外模型')
    expect(host.textContent).toContain('从添加模型流程选择当前接入')
    expect(control('添加到模型')).toBeTruthy()
    expect(requests).toHaveLength(count)
    await act(async () => control('添加到模型')!.click())
    expect(router.state.location.search).toBe('?connectionId=con_exact')
    expect(writes()).toHaveLength(0)
  })
})
