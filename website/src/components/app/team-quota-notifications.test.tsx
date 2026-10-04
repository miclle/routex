import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it } from 'vitest'
import client from '@/api/client'
import { notificationsKey } from '@/api/notifications'
import { sessionKey } from '@/hooks/use-auth'
import i18n from '@/i18n'
import type {
  MonthlyQuotaNotificationSnapshot,
  Notification,
  NotificationsPage,
} from '@/types/notifications'
import { NotificationMenu } from './NotificationMenu'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let host: HTMLDivElement, root: Root, cache: QueryClient
let actor: string, role: 'admin' | 'member', signedIn: boolean
let page: NotificationsPage, nextPage: NotificationsPage, requests: InternalAxiosRequestConfig[]
let getFailure: number, nextFailure: number, readFailure: number
let sessionGate: ReturnType<typeof barrier> | undefined
let csrfOverride: string | undefined
let getResponse: { data: unknown } | undefined
let getGate: ReturnType<typeof barrier> | undefined,
  readGate: ReturnType<typeof barrier> | undefined
function barrier() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  return { promise, release: () => release() }
}
function session() {
  return {
    user: { id: actor, role, name: 'Member', email: 'member@example.test' },
    csrf_token: csrfOverride ?? `csrf-${actor}`,
  }
}
function quota(
  overrides: Partial<MonthlyQuotaNotificationSnapshot> = {},
): MonthlyQuotaNotificationSnapshot {
  return {
    scope_kind: 'team',
    scope_id: 'tem_recorded',
    dimension: 'tokens',
    policy_revision: 'policy-4',
    month_start: '2026-09-01T00:00:00Z',
    month_end: '2026-10-01T00:00:00Z',
    time_zone: 'UTC',
    as_of: '2026-09-17T09:03:00Z',
    limit: '100',
    settled: '100',
    currency: null,
    ...overrides,
  }
}
function notice(snapshot: MonthlyQuotaNotificationSnapshot | undefined = quota()): Notification {
  return {
    id: 'qnt_1',
    quota_observation_id: 'qob_1',
    kind: 'monthly_quota_exhausted',
    detail_code:
      snapshot?.dimension === 'money' ? 'money_month_exhausted' : 'tokens_month_exhausted',
    severity: 'high',
    occurrence_count: 1,
    read: false,
    read_at: null,
    first_seen_at: '2026-09-17T09:03:00Z',
    last_seen_at: '2026-09-17T09:03:00Z',
    ...(snapshot
      ? {
          quota: snapshot,
          subject_type: snapshot.scope_kind,
          subject_id: snapshot.scope_id,
          subject_name: snapshot.scope_kind === 'team' ? 'Recorded Team' : undefined,
        }
      : {}),
  }
}
function httpError(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('rejected', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: { code: status, message: 'server-private detail' },
  })
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  actor = 'usr_member'
  role = 'member'
  signedIn = true
  page = { items: [notice()], unread_count: 1, next_cursor: null }
  nextPage = { items: [], unread_count: 1, next_cursor: null }
  requests = []
  getFailure = 0
  nextFailure = 0
  readFailure = 0
  getResponse = undefined
  getGate = undefined
  sessionGate = undefined
  csrfOverride = undefined
  readGate = undefined
  cache.setQueryData(sessionKey, session())
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session') {
      if (!signedIn) throw httpError(config, 401)
      const capturedSession = session()
      if (sessionGate) await sessionGate.promise
      response.data = capturedSession
    } else if (config.url === '/notifications' && config.method === 'get') {
      const capturedPage = structuredClone(config.params?.cursor ? nextPage : page)
      if (getGate) await getGate.promise
      const failure = config.params?.cursor ? nextFailure : getFailure
      if (failure) throw httpError(config, failure)
      if (getResponse) return { ...response, data: getResponse.data }
      const result = capturedPage
      if (config.params?.status === 'unread')
        result.items = result.items.filter((item) => !item.read)
      response.data = result
    } else if (config.method === 'post' && config.url?.startsWith('/notifications/')) {
      if (readGate) await readGate.promise
      if (readFailure) throw httpError(config, readFailure)
      page.items = page.items.map((item) =>
        config.url === '/notifications/read-all' ||
        config.url === `/notifications/${encodeURIComponent(item.id)}/read`
          ? { ...item, read: true, read_at: '2026-09-17T09:04:00Z' }
          : item,
      )
      page.unread_count = page.items.filter((item) => !item.read).length
    } else throw new Error(`Unexpected notification request ${config.method} ${config.url}`)
    return response
  }
})
afterEach(async () => {
  getGate?.release()
  sessionGate?.release()
  readGate?.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
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
async function mount(validResponse = true) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <NotificationMenu />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    validResponse
      ? expect(cache.getQueryData(notificationsKey(actor, 'unread'))).toBeTruthy()
      : expect(cache.getQueryState(notificationsKey(actor, 'unread'))?.status).toBe('error'),
  )
  await click('Notifications')
  await until(() => expect(document.querySelector('[role="menu"]')).not.toBeNull())
}
function menu() {
  return document.querySelector('[role="menu"]')!
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
function item() {
  return menu().querySelector<HTMLElement>('[role="menuitem"]')!
}
function unreadBadge() {
  return host.querySelector(
    '[aria-label$="unread notification"], [aria-label$="unread notifications"]',
  )
}
async function refresh() {
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['notifications', actor] })
  })
}

it('renders the frozen Team aggregate token snapshot distinctly without fetching a directory or policy', async () => {
  await mount()
  await until(() => expect(menu().textContent).toContain('Team: Recorded Team (tem_recorded)'))
  expect(menu().textContent).toContain('Settled: 100 tokens')
  expect(menu().textContent).toContain('Limit: 100 tokens')
  expect(menu().textContent).toContain('Policy revision: policy-4')
  expect(menu().textContent).not.toContain('Personal quota')
  expect(menu().textContent).not.toContain('Project:')
  expect(menu().textContent).not.toMatch(/remaining|Email|Runtime publication/)
  expect(unreadBadge()?.textContent).toBe('1')
  expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
})
it('preserves exact recorded Team money and currency with live locale dates and scope labels', async () => {
  page.items = [
    notice(
      quota({
        dimension: 'money',
        currency: 'USD',
        limit: '9007199254740993.000000000000000001',
        settled: '9007199254740994.000000000000000002',
      }),
    ),
  ]
  await mount()
  await until(() => expect(menu().textContent).toContain('Team: Recorded Team (tem_recorded)'))
  expect(menu().textContent).toContain('Settled: 9007199254740994.000000000000000002 USD')
  expect(menu().textContent).toContain('Limit: 9007199254740993.000000000000000001 USD')
  expect(menu().textContent).toContain('As of Sep 17, 2026')
  await act(async () => i18n.changeLanguage('zh'))
  expect(menu().textContent).toContain('Team：Recorded Team（tem_recorded）')
  expect(menu().textContent).toContain('已结算：9007199254740994.000000000000000002 USD')
  expect(menu().textContent).toContain('2026年9月')
  expect(menu().textContent).not.toMatch(/converted|remaining|额度剩余/)
  expect(requests.filter((request) => request.url === '/notifications')).toHaveLength(1)
})
it('uses only the exact recorded Team ID when no name snapshot exists', async () => {
  page.items = [{ ...notice(), subject_name: undefined }]
  await mount()
  await until(() => expect(menu().textContent).toContain('Team: tem_recorded'))
  expect(menu().textContent).not.toContain('Recorded Team')
})
it.each([
  { limit: '0', settled: '0' },
  { limit: '9007199254740993', settled: '9007199254740994' },
])('preserves Team token zero/large integer strings %j', async (values) => {
  page.items = [notice(quota(values))]
  await mount()
  await until(() => expect(menu().textContent).toContain(`Settled: ${values.settled} tokens`))
  expect(menu().textContent).toContain(`Limit: ${values.limit} tokens`)
  expect(menu().textContent).not.toMatch(/spent|crossed|remaining|100%/)
})
it.each([
  ['absent snapshot', { quota: undefined }],
  ['absent Team subject', { subject_type: undefined }],
  ['absent Team ID', { subject_id: undefined }],
  ['another Team ID', { subject_id: 'tem_other' }],
  ['Project subject', { subject_type: 'project' }],
  ['negative amount', { quota: quota({ settled: '-1' }) }],
  ['unrecorded money currency', { quota: quota({ dimension: 'money', currency: null }) }],
  ['future scope', { quota: { ...quota(), scope_kind: 'future' } }],
  ['unknown observation time', { quota: quota({ as_of: 'not-recorded' }) }],
])('does not display arbitrary Team facts from a %s snapshot', async (_description, changes) => {
  page.items = [{ ...notice(), ...changes, subject_name: 'Untrusted Team' } as Notification]
  await mount()
  await until(() =>
    expect(menu().textContent).toContain(
      'The monthly quota snapshot was not recorded or is unavailable.',
    ),
  )
  expect(menu().textContent).not.toContain('Untrusted Team')
  expect(menu().textContent).not.toContain('Settled:')
  expect(menu().textContent).not.toContain('Team:')
})
it.each([403, 404, 503])(
  'hides old Team facts and badge during notification refresh and after HTTP %s',
  async (failure) => {
    await mount()
    await until(() => expect(menu().textContent).toContain('Recorded Team'))
    getGate = barrier()
    getFailure = failure
    await refresh()
    await until(() => expect(menu().textContent).toContain('Loading notifications'))
    expect(menu().querySelector('[role="menuitem"]')).toBeNull()
    expect(unreadBadge()).toBeNull()
    expect(button('Mark all read')).toBeUndefined()
    await act(async () => getGate!.release())
    await until(() => expect(menu().textContent).toContain('Notifications could not be loaded.'))
    expect(menu().textContent).not.toContain('Recorded Team')
    expect(menu().textContent).not.toContain('server-private detail')
  },
)
it('hides cached Team facts and read controls while the real Session query renews', async () => {
  await mount()
  await until(() => expect(menu().textContent).toContain('Recorded Team'))
  sessionGate = barrier()
  await act(async () => {
    void cache.invalidateQueries({ queryKey: sessionKey })
  })
  await until(() => expect(menu().textContent).toContain('Loading notifications'))
  expect(menu().querySelector('[role="menuitem"]')).toBeNull()
  expect(unreadBadge()).toBeNull()
  expect(button('Mark all read')).toBeUndefined()
  page = { items: [], unread_count: 0, next_cursor: null }
  await act(async () => sessionGate!.release())
  await until(() => expect(menu().textContent).toContain('No new notifications'))
  expect(menu().textContent).not.toContain('Recorded Team')
  expect(requests.filter((request) => request.method === 'post')).toHaveLength(0)
})
it('never renders an obsolete actor response under a new recipient or adds old Team history', async () => {
  await mount()
  await until(() => expect(menu().textContent).toContain('Recorded Team'))
  getGate = barrier()
  await refresh()
  await until(() => expect(menu().textContent).toContain('Loading notifications'))
  actor = 'usr_new_member'
  page = { items: [], unread_count: 0, next_cursor: null }
  await act(async () => cache.setQueryData(sessionKey, session()))
  await act(async () => getGate!.release())
  await until(() => expect(cache.getQueryData(notificationsKey(actor, 'unread'))).toBeTruthy())
  expect(menu().textContent).not.toContain('Recorded Team')
  expect(unreadBadge()).toBeNull()
  expect(menu().querySelector('[role="menuitem"]')).toBeNull()
})
it('current membership removal hides history and an original authorized rejoin preserves read state', async () => {
  await mount()
  await until(() => expect(item()).not.toBeNull())
  await act(async () => item().click())
  await until(() => expect(page.items[0].read).toBe(true))
  const historical = structuredClone(page.items[0])
  await click('Notifications')
  await click('All')
  await until(() => expect(menu().textContent).toContain('Recorded Team'))
  page = { items: [], unread_count: 0, next_cursor: null }
  await refresh()
  await until(() => expect(menu().textContent).toContain('No notification history'))
  expect(menu().textContent).not.toContain('Recorded Team')
  page = { items: [historical], unread_count: 0, next_cursor: null }
  await refresh()
  await until(() => expect(menu().textContent).toContain('Recorded Team'))
  expect(unreadBadge()).toBeNull()
  const before = requests.filter((request) => request.method === 'post').length
  await act(async () => item().click())
  expect(requests.filter((request) => request.method === 'post')).toHaveLength(before)
})
it.each(['single', 'all'])(
  'sends the exact current %s read operation with CSRF and refreshes denied Team authority',
  async (operation) => {
    await mount()
    await until(() => expect(item()).not.toBeNull())
    readFailure = 403
    page = { items: [], unread_count: 0, next_cursor: null }
    if (operation === 'single') await act(async () => item().click())
    else await click('Mark all read')
    await until(() =>
      expect(requests.filter((request) => request.method === 'post')).toHaveLength(1),
    )
    const request = requests.find((request) => request.method === 'post')!
    expect(request.url).toBe(
      operation === 'single' ? '/notifications/qnt_1/read' : '/notifications/read-all',
    )
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf-usr_member')
    expect(request.params).toBeUndefined()
    await until(() =>
      expect(
        cache.getQueryData<{ pages: NotificationsPage[] }>(notificationsKey(actor, 'unread'))
          ?.pages[0].items,
      ).toHaveLength(0),
    )
    expect(unreadBadge()).toBeNull()
  },
)
it('keeps mark-read unavailable without current CSRF instead of guessing a token', async () => {
  csrfOverride = ''
  cache.setQueryData(sessionKey, session())
  await mount()
  await until(() => expect(menu().textContent).toContain('Recorded Team'))
  expect(button('Mark all read').disabled).toBe(true)
  expect(item().getAttribute('aria-disabled')).toBe('true')
  await click('Mark all read')
  expect(requests.filter((request) => request.method === 'post')).toHaveLength(0)
})
it('loads only server-authorized Team rows with actor/status cursor keys', async () => {
  page.next_cursor = 'team-recipient-cursor'
  nextPage = {
    items: [{ ...notice(), id: 'qnt_next', read: true, subject_name: 'Second Recorded Team' }],
    unread_count: 1,
    next_cursor: null,
  }
  await mount()
  await click('All')
  await until(() => expect(button('Load more notifications')).toBeDefined())
  await click('Load more notifications')
  await until(() => expect(menu().textContent).toContain('Second Recorded Team'))
  const last = requests.filter((request) => request.url === '/notifications').at(-1)!
  expect(last.params).toMatchObject({ status: 'all', cursor: 'team-recipient-cursor' })
  expect(Object.keys(last.params).sort()).toEqual(['cursor', 'status'])
  expect(cache.getQueryData(notificationsKey(actor, 'all'))).toBeTruthy()
})

it('ignores an older Team inbox response after same-actor Session reauthorization succeeds', async () => {
  await mount()
  await until(() => expect(menu().textContent).toContain('Recorded Team'))
  getGate = barrier()
  const oldInbox = getGate
  await refresh()
  await until(() => expect(menu().textContent).toContain('Loading notifications'))
  sessionGate = barrier()
  await act(async () => {
    void cache.invalidateQueries({ queryKey: sessionKey })
  })
  await until(() => expect(cache.getQueryState(sessionKey)?.fetchStatus).toBe('fetching'))
  page = { items: [], unread_count: 0, next_cursor: null }
  getGate = undefined
  await act(async () => sessionGate!.release())
  await until(() => expect(menu().textContent).toContain('No new notifications'))
  await act(async () => oldInbox.release())
  expect(menu().textContent).not.toContain('Recorded Team')
  expect(unreadBadge()).toBeNull()
  expect(menu().querySelector('[role="menuitem"]')).toBeNull()
})

it('hides the Team snapshot after current Session authorization fails', async () => {
  await mount()
  await until(() => expect(menu().textContent).toContain('Recorded Team'))
  signedIn = false
  await act(async () => {
    void cache.invalidateQueries({ queryKey: sessionKey })
  })
  await until(() =>
    expect(menu().textContent).toContain('Notifications are unavailable for the current session.'),
  )
  expect(menu().textContent).not.toContain('Recorded Team')
  expect(unreadBadge()).toBeNull()
  expect(button('Mark all read')).toBeUndefined()
})

it('does not infer Team recipient visibility from an administrator role', async () => {
  role = 'admin'
  page = { items: [], unread_count: 0, next_cursor: null }
  cache.setQueryData(sessionKey, session())
  await mount()
  await until(() => expect(menu().textContent).toContain('No new notifications'))
  expect(menu().textContent).not.toContain('Recorded Team')
  expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
})
