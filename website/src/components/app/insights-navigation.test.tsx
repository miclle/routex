import { act, isValidElement, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders } from 'axios'
import { createMemoryRouter, matchRoutes, RouterProvider } from 'react-router'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import routes from '@/router'
import { sessionKey } from '@/hooks/use-auth'
import AppShell from './AppShell'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let host: HTMLDivElement, root: Root, cache: QueryClient
let router: ReturnType<typeof createMemoryRouter> | undefined
let permissions: string[], requests: string[], actor: string
let role: 'admin' | 'member'
let permissionFailure: boolean
const originalAdapter = client.defaults.adapter
function deferred() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  return { promise, release }
}
let permissionHold: ReturnType<typeof deferred> | undefined
let sessionHold: ReturnType<typeof deferred> | undefined
const sessionValue = () => ({
  user: { id: actor, role, name: 'Report reader', email: 'reader@example.invalid' },
  csrf_token: 'csrf-report-reader',
})
beforeEach(() => {
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  actor = 'usr_insights'
  role = 'member'
  permissions = []
  requests = []
  permissionFailure = false
  permissionHold = undefined
  sessionHold = undefined
  router = undefined
  client.defaults.adapter = async (config) => {
    requests.push(config.url ?? '')
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session') {
      const value = sessionValue()
      await sessionHold?.promise
      response.data = value
    } else if (config.url === '/auth/permissions') {
      const captured = [...permissions]
      const failure = permissionFailure
      await permissionHold?.promise
      if (failure)
        throw new AxiosError('Permissions unavailable', '', config, undefined, {
          ...response,
          status: 503,
        })
      response.data = { permissions: captured }
    } else if (config.url === '/site')
      response.data = {
        name: 'RouteX',
        service_url: '',
        logo_url: '',
        footer: '',
        default_language: 'en',
        etag: 'site',
        updated_at: '2026-10-01T00:00:00Z',
      }
    else if (config.url === '/announcements') response.data = { items: [] }
    else if (config.url === '/notifications') response.data = { items: [], unread_count: 0 }
    else throw new Error('Unexpected request: ' + config.url)
    return response
  }
})
afterEach(async () => {
  permissionHold?.release()
  sessionHold?.release()
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
})
async function until(assertion: () => void) {
  for (let attempt = 0; attempt < 100; attempt++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assertion()
      return
    } catch (error) {
      if (attempt === 99) throw error
    }
  }
}
const insightLinks = () => [
  ...host.querySelectorAll<HTMLAnchorElement>('a[href="/admin/insights"]'),
]
async function shell() {
  router = createMemoryRouter(
    [
      {
        path: '/',
        element: <AppShell />,
        children: [{ path: 'admin/insights', element: <p>Report workspace</p> }],
      },
    ],
    { initialEntries: ['/admin/insights'] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router!} />
      </QueryClientProvider>,
    ),
  )
  await until(() => {
    expect(cache.getQueryData(['permissions', actor])).toEqual(permissions)
    expect(cache.isFetching()).toBe(0)
  })
}

describe('operations analysis route and navigation', () => {
  it('registers the exact report route inside the private application shell', () => {
    const matches = matchRoutes(routes, '/admin/insights')!
    expect(matches.at(-1)?.route.path).toBe('admin/insights')
    expect(matches.at(-1)?.route.lazy).toBeTypeOf('function')
    expect(
      matches.some(
        (match) => isValidElement(match.route.element) && match.route.element.type === AppShell,
      ),
    ).toBe(true)
    const privateWrapper = matches[0].route.element
    if (!isValidElement<{ children: ReactNode }>(privateWrapper))
      throw new Error('Missing private route wrapper')
    const gate = privateWrapper.props.children
    if (!isValidElement<{ mode: string }>(gate)) throw new Error('Missing authentication gate')
    expect(gate.props.mode).toBe('private')
  })
  it('shows the entry to a non-administrator with only platform call-read authority', async () => {
    permissions = ['calls.read_all']
    await shell()
    await until(() => expect(insightLinks()).toHaveLength(1))
    expect(insightLinks()[0].textContent).toContain('Operations analysis')
    expect(insightLinks()[0].getAttribute('aria-current')).toBe('page')
    expect(host.querySelector('header h1')?.textContent).toBe('Operations analysis')
    expect(
      requests.every((url) =>
        [
          '/auth/session',
          '/auth/permissions',
          '/site',
          '/announcements',
          '/notifications',
        ].includes(url),
      ),
    ).toBe(true)
  })
  it.each([
    { memberRole: 'member' as const, granted: [] },
    { memberRole: 'admin' as const, granted: ['system.read', 'models.read_all'] },
  ])(
    'does not substitute $memberRole status or unrelated permissions',
    async ({ memberRole, granted }) => {
      role = memberRole
      permissions = granted
      await shell()
      expect(insightLinks()).toHaveLength(0)
    },
  )
  it('updates the entry and page title when the language changes', async () => {
    permissions = ['calls.read_all']
    await shell()
    await until(() => expect(insightLinks()).toHaveLength(1))
    await act(async () => i18n.changeLanguage('zh'))
    expect(insightLinks()[0].textContent).toContain('运营分析')
    expect(host.querySelector('header h1')?.textContent).toBe('运营分析')
  })
  it('hides cached access during permission renewal and after its failure until a successful retry', async () => {
    permissions = ['calls.read_all']
    await shell()
    permissionHold = deferred()
    permissionFailure = true
    let pending!: Promise<void>
    await act(async () => {
      pending = cache.refetchQueries({ queryKey: ['permissions', actor] })
    })
    await until(() => expect(insightLinks()).toHaveLength(0))
    permissionHold.release()
    await act(async () => pending)
    expect(cache.getQueryData(['permissions', actor])).toEqual(['calls.read_all'])
    expect(insightLinks()).toHaveLength(0)
    permissionHold = undefined
    permissionFailure = false
    await act(async () => cache.refetchQueries({ queryKey: ['permissions', actor] }))
    await until(() => expect(insightLinks()).toHaveLength(1))
  })
  it('hides cached access throughout same-actor Session renewal', async () => {
    permissions = ['calls.read_all']
    await shell()
    sessionHold = deferred()
    let pending!: Promise<void>
    await act(async () => {
      pending = cache.refetchQueries({ queryKey: sessionKey })
    })
    await until(() => expect(insightLinks()).toHaveLength(0))
    sessionHold.release()
    await act(async () => pending)
    await until(() => expect(insightLinks()).toHaveLength(1))
  })
  it('does not restore an old actor entry from a held permission response', async () => {
    permissions = ['calls.read_all']
    await shell()
    permissionHold = deferred()
    const held = permissionHold
    let pending!: Promise<void>
    await act(async () => {
      pending = cache.refetchQueries({ queryKey: ['permissions', actor] })
    })
    await until(() => expect(insightLinks()).toHaveLength(0))
    actor = 'usr_next_reader'
    permissions = []
    permissionHold = undefined
    await act(async () => {
      cache.setQueryData(sessionKey, sessionValue())
    })
    await until(() => expect(cache.getQueryData(['permissions', actor])).toEqual([]))
    held.release()
    await act(async () => pending)
    expect(insightLinks()).toHaveLength(0)
  })
})
