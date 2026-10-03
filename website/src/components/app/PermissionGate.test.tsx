import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import { PermissionGate } from './PermissionGate'
import AppShell from './AppShell'
import type { PermissionRequirement } from './permissions'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let host: HTMLDivElement, root: Root, cache: QueryClient
let permissions: string[], requests: InternalAxiosRequestConfig[], permissionFailure: boolean
let router: ReturnType<typeof createMemoryRouter> | undefined
const originalAdapter = client.defaults.adapter

beforeEach(() => {
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  permissions = []
  requests = []
  permissionFailure = false
  router = undefined
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
        user: { id: 'usr_gate', role: 'admin', name: 'Gate', email: 'gate@example.invalid' },
        csrf_token: 'csrf-gate',
      }
    else if (config.url === '/auth/permissions') {
      if (permissionFailure)
        throw new AxiosError('Permissions unavailable', '', config, undefined, {
          ...response,
          status: 503,
        })
      response.data = { permissions }
    } else if (config.url === '/protected') response.data = { message: 'Protected quota content' }
    else if (config.url === '/site')
      response.data = {
        name: 'RouteX',
        service_url: '',
        logo_url: '',
        footer: '',
        default_language: 'en',
        etag: 'site',
        updated_at: '2026-09-30T08:00:00Z',
      }
    else if (config.url === '/announcements') response.data = { items: [] }
    else if (config.url === '/notifications') response.data = { items: [], unread_count: 0 }
    else throw new Error(`Unexpected request: ${config.url}`)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
})
async function until(assert: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
function ProtectedContent() {
  const query = useQuery({
    queryKey: ['protected'],
    queryFn: async () => (await client.get<{ message: string }>('/protected')).data,
  })
  return <p>{query.data?.message}</p>
}
async function gate(permission: PermissionRequirement) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <PermissionGate permission={permission}>
          <ProtectedContent />
        </PermissionGate>
      </QueryClientProvider>,
    ),
  )
}
async function shell(path = '/admin/system-info') {
  router = createMemoryRouter(
    [
      {
        path: '/',
        element: <AppShell />,
        children: [
          { index: true, element: <p>Workspace</p> },
          { path: 'admin/system-info', element: <p>System workspace</p> },
        ],
      },
    ],
    { initialEntries: [path] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router!} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(cache.getQueryData(['permissions', 'usr_gate'])).toEqual(permissions))
}
const adminLinks = () =>
  [...host.querySelectorAll<HTMLAnchorElement>('nav[aria-label="Main navigation"] a')].map((link) =>
    link.getAttribute('href'),
  )

describe('permission alternatives', () => {
  it.each([
    ['system reader', ['system.read']],
    ['quota settings writer', ['limits.settings.write']],
    ['both authorities', ['system.read', 'limits.settings.write']],
  ])('allows %s and mounts protected queries only after authorization', async (_name, granted) => {
    permissions = granted
    await gate(['system.read', 'limits.settings.write'])
    await until(() => expect(host.textContent).toContain('Protected quota content'))
    expect(requests.filter((item) => item.url === '/protected')).toHaveLength(1)
  })
  it.each([
    ['neither authority', ['site.write']],
    ['no authorities', []],
  ])('denies %s without querying protected resources', async (_name, granted) => {
    permissions = granted
    await gate(['system.read', 'limits.settings.write'])
    await until(() => expect(host.textContent).toContain('does not have permission'))
    expect(requests.some((item) => item.url === '/protected')).toBe(false)
  })
  it('denies an empty alternative list even to an administrator with system permission', async () => {
    permissions = ['system.read']
    await gate([])
    await until(() => expect(host.textContent).toContain('does not have permission'))
    expect(requests.some((item) => item.url === '/protected')).toBe(false)
  })
  it('preserves single permission semantics', async () => {
    permissions = ['limits.settings.write']
    await gate('system.read')
    await until(() => expect(host.textContent).toContain('does not have permission'))
    expect(requests.some((item) => item.url === '/protected')).toBe(false)
    await act(async () => cache.setQueryData(['permissions', 'usr_gate'], ['system.read']))
    await until(() => expect(host.textContent).toContain('Protected quota content'))
  })
  it('keeps the site editor inaccessible to a quota-settings-only writer', async () => {
    permissions = ['limits.settings.write']
    await gate('site.write')
    await until(() => expect(host.textContent).toContain('does not have permission'))
    expect(requests.some((item) => item.url === '/protected')).toBe(false)
  })
  it('keeps permission failures closed until a successful explicit retry', async () => {
    permissions = ['limits.settings.write']
    permissionFailure = true
    await gate(['system.read', 'limits.settings.write'])
    await until(() => expect(host.querySelector('button')?.textContent).toBe('Retry'))
    expect(requests.some((item) => item.url === '/protected')).toBe(false)
    permissionFailure = false
    await act(async () => host.querySelector('button')!.click())
    await until(() => expect(host.textContent).toContain('Protected quota content'))
  })
})

describe('quota settings and system information navigation', () => {
  it('shows default limits and quota calendar links to a quota-settings-only writer', async () => {
    permissions = ['limits.settings.write']
    await shell()
    await until(() => expect(adminLinks()).toContain('/admin/system-info'))
    expect(adminLinks()).toEqual(['/', '/admin/limits', '/admin/system-info'])
    await until(() => expect(requests.some((item) => item.url === '/notifications')).toBe(true))
  })
  it('keeps existing system-reader links independently accessible', async () => {
    permissions = ['system.read']
    await shell()
    await until(() => expect(adminLinks()).toContain('/admin/system-info'))
    expect(adminLinks()).toContain('/admin/limits')
    expect(adminLinks()).toContain('/admin/system-status')
    expect(adminLinks()).toContain('/admin/system-announcements')
    expect(adminLinks()).not.toContain('/admin/storage')
  })
  it('omits system information when neither authority is granted', async () => {
    permissions = ['site.write']
    await shell()
    expect(adminLinks()).not.toContain('/admin/system-info')
    expect(adminLinks()).not.toContain('/admin/limits')
  })
  it('provides a workspace management entry to default limits and removes both links after revocation', async () => {
    permissions = ['limits.settings.write']
    await shell('/')
    await until(() =>
      expect(
        host.querySelector('nav[aria-label="Account navigation"] a[href="/admin/limits"]'),
      ).not.toBeNull(),
    )
    await act(async () => cache.setQueryData(['permissions', 'usr_gate'], []))
    await until(() => {
      expect(host.querySelector('a[href="/admin/limits"]')).toBeNull()
      expect(host.querySelector('a[href="/admin/system-info"]')).toBeNull()
    })
  })
})
