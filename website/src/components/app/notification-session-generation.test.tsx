import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { sessionKey } from '@/hooks/use-auth'
import i18n from '@/i18n'
import type { NotificationsPage } from '@/types/notifications'
import { NotificationMenu } from './NotificationMenu'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
const session = {
  user: { id: 'usr_member', role: 'member' as const, name: 'Member', email: 'member@example.test' },
  csrf_token: 'csrf-original',
}
const oldPage: NotificationsPage = {
  items: [
    {
      id: 'qnt_old',
      kind: 'monthly_quota_exhausted',
      detail_code: 'tokens_month_exhausted',
      severity: 'high',
      occurrence_count: 1,
      read: false,
      read_at: null,
      first_seen_at: '2026-09-17T09:03:00Z',
      last_seen_at: '2026-09-17T09:03:00Z',
      subject_type: 'team',
      subject_id: 'tem_recorded',
      subject_name: 'Private recorded Team',
      quota: {
        scope_kind: 'team',
        scope_id: 'tem_recorded',
        dimension: 'tokens',
        policy_revision: 'p1',
        month_start: '2026-09-01T00:00:00Z',
        month_end: '2026-10-01T00:00:00Z',
        time_zone: 'UTC',
        as_of: '2026-09-17T09:03:00Z',
        limit: '5',
        settled: '5',
        currency: null,
      },
    },
  ],
  unread_count: 1,
  next_cursor: null,
}
function deferred() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  return { promise, release: () => release() }
}
let host: HTMLDivElement, root: Root, cache: QueryClient
let requests: InternalAxiosRequestConfig[]
let gates: ReturnType<typeof deferred>[]
let inbox: (config: InternalAxiosRequestConfig) => Promise<NotificationsPage>
beforeEach(async () => {
  vi.spyOn(Date, 'now').mockReturnValue(1_800_000_000_000)
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  requests = []
  gates = []
  inbox = async () => structuredClone(oldPage)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown
    if (config.url === '/auth/session') data = structuredClone(session)
    else if (config.url === '/notifications') data = await inbox(config)
    else if (config.method === 'post' && config.url?.startsWith('/notifications/')) data = {}
    else throw new Error('Unexpected request')
    return { data, config, headers: new AxiosHeaders(), status: 200, statusText: '' }
  }
})
afterEach(async () => {
  gates.forEach((gate) => gate.release())
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
})
async function settle() {
  await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
}
async function until(assertion: () => void) {
  for (let attempt = 0; attempt < 100; attempt++) {
    await settle()
    try {
      assertion()
      return
    } catch (error) {
      if (attempt === 99) throw error
    }
  }
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <NotificationMenu />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(host.querySelector('[aria-label="1 unread notification"]')).not.toBeNull(),
  )
  await act(async () => button('Notifications')!.click())
  await until(() => expect(document.body.textContent).toContain('Private recorded Team'))
  expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(1)
}
async function renew() {
  const old = cache.getQueryState(sessionKey)!
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await settle()
  const current = cache.getQueryState(sessionKey)!
  expect(current.data).toBe(old.data)
  expect(current.dataUpdatedAt).toBe(old.dataUpdatedAt)
  expect(current.dataUpdateCount).toBe(old.dataUpdateCount + 1)
  expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(2)
}
it('rechecks the inbox and hides old rows, count and actions on an identical same-ms Session GET', async () => {
  await mount()
  const gate = deferred()
  gates.push(gate)
  inbox = async () => {
    await gate.promise
    return { items: [], unread_count: 0, next_cursor: null }
  }
  await renew()
  expect(document.body.textContent).not.toContain('Private recorded Team')
  expect(host.querySelector('[aria-label="1 unread notification"]')).toBeNull()
  expect(button('Mark all read')).toBeUndefined()
  expect(document.querySelector('[role="menuitem"]')).toBeNull()
  expect(requests.filter((request) => request.url === '/notifications')).toHaveLength(2)
  await act(async () => gate.release())
  await until(() => expect(document.body.textContent).toContain('No new notifications'))
  expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(2)
  expect(requests.filter((request) => request.method === 'post')).toHaveLength(0)
})
it('aborts and ignores an older recipient response after a fast unchanged Session renewal', async () => {
  await mount()
  const gate = deferred()
  gates.push(gate)
  let oldSignal: AbortSignal | undefined
  inbox = async (config) => {
    oldSignal = config.signal as AbortSignal
    await gate.promise
    return structuredClone(oldPage)
  }
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['notifications', session.user.id] })
  })
  await until(() => expect(oldSignal).toBeDefined())
  inbox = async () => ({ items: [], unread_count: 0, next_cursor: null })
  await renew()
  expect(oldSignal!.aborted).toBe(true)
  await act(async () => gate.release())
  await until(() => expect(document.body.textContent).toContain('No new notifications'))
  expect(document.body.textContent).not.toContain('Private recorded Team')
  expect(button('Mark all read')).toBeUndefined()
  expect(host.querySelector('[aria-label="1 unread notification"]')).toBeNull()
  expect(requests.filter((request) => request.url === '/notifications')).toHaveLength(3)
})
it('keeps the inbox on manual same-actor CSRF replacement and dispatches only the new token', async () => {
  await mount()
  await act(async () => {
    cache.setQueryData(sessionKey, { ...session, csrf_token: 'csrf-replaced' })
  })
  await settle()
  expect(document.body.textContent).toContain('Private recorded Team')
  expect(requests.filter((request) => request.url === '/notifications')).toHaveLength(1)
  await act(async () => button('Mark all read')!.click())
  await until(() =>
    expect(requests.some((request) => request.url === '/notifications/read-all')).toBe(true),
  )
  expect(
    requests
      .find((request) => request.url === '/notifications/read-all')!
      .headers.get('X-CSRF-Token'),
  ).toBe('csrf-replaced')
  expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(1)
})
