import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { notificationsKey } from '@/api/notifications'
import { sessionKey } from '@/hooks/use-auth'
import i18n from '@/i18n'
import type { Notification, NotificationsPage } from '@/types/notifications'
import { NotificationMenu } from './NotificationMenu'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
const digest = 'A'.repeat(52)
let host: HTMLDivElement, root: Root, cache: QueryClient
let actor: string, csrf: string, page: NotificationsPage, requests: InternalAxiosRequestConfig[]
let inbox: (config: InternalAxiosRequestConfig) => Promise<unknown>
let gates: ReturnType<typeof deferred>[], sessionGate: ReturnType<typeof deferred> | undefined
let readFailure: number
function deferred() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  return { promise, release: () => release() }
}
function memberNotice(): Notification {
  return {
    id: 'qni_member',
    kind: 'monthly_quota_exhausted',
    quota_observation_id: 'qob_member',
    detail_code: 'money_month_exhausted',
    severity: 'high',
    occurrence_count: 1,
    read: false,
    read_at: null,
    first_seen_at: '2026-10-04T12:00:00Z',
    last_seen_at: '2026-10-04T12:00:00Z',
    subject_type: 'team_member',
    subject_id: digest,
    subject_name: 'Recorded Team',
    quota: {
      scope_kind: 'team_member',
      scope_id: digest,
      team_id: 'tem_recorded',
      member_user_id: 'usr_member',
      dimension: 'money',
      policy_revision: 'lim_member',
      month_start: '2026-10-01T00:00:00Z',
      month_end: '2026-11-01T00:00:00Z',
      as_of: '2026-10-04T12:00:00Z',
      time_zone: 'UTC',
      limit: '1.000000000000000001',
      settled: '9007199254740993.123456789012345678',
      currency: 'USD',
    },
  }
}
function session() {
  return {
    user: { id: actor, name: 'Member', role: 'member' as const, email: 'member@example.invalid' },
    csrf_token: csrf,
  }
}
function rejected(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('rejected', '', config, undefined, {
    status,
    statusText: '',
    config,
    headers: new AxiosHeaders(),
    data: { message: 'private server diagnostic' },
  })
}
beforeEach(async () => {
  vi.spyOn(Date, 'now').mockReturnValue(1_800_000_000_000)
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  actor = 'usr_member'
  csrf = 'csrf-current'
  page = { items: [memberNotice()], unread_count: 1, next_cursor: null }
  requests = []
  gates = []
  sessionGate = undefined
  readFailure = 0
  inbox = async () => structuredClone(page)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown
    if (config.url === '/auth/session') {
      const captured = session()
      if (sessionGate) await sessionGate.promise
      data = captured
    } else if (config.url === '/notifications') data = await inbox(config)
    else if (config.method === 'post' && config.url?.startsWith('/notifications/')) {
      if (readFailure) throw rejected(config, readFailure)
      page.items = page.items.map((item) =>
        config.url === '/notifications/read-all' || config.url === `/notifications/${item.id}/read`
          ? { ...item, read: true, read_at: '2026-10-04T12:01:00Z' }
          : item,
      )
      page.unread_count = page.items.filter((item) => !item.read).length
      data = {}
    } else throw new Error(`Unexpected member quota request ${config.method} ${config.url}`)
    return { data, config, status: 200, statusText: '', headers: new AxiosHeaders() }
  }
})
afterEach(async () => {
  gates.forEach((gate) => gate.release())
  sessionGate?.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
  await i18n.changeLanguage('en')
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
    (element) => element.textContent === label || element.getAttribute('aria-label') === label,
  )
}
async function click(label: string) {
  await act(async () => button(label)!.click())
}
function menu() {
  return document.querySelector('[role="menu"]')!
}
function badge() {
  return host.querySelector(
    '[aria-label$="unread notification"], [aria-label$="unread notifications"]',
  )
}
async function mount(valid = true) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <NotificationMenu />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(cache.getQueryState(notificationsKey(actor, 'unread'))?.status).toBe(
      valid ? 'success' : 'error',
    ),
  )
  await click('Notifications')
  await until(() => expect(menu()).not.toBeNull())
}
async function refresh() {
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['notifications', actor] })
  })
}

it('renders your recorded member quota independently of aggregate without directory or policy reads', async () => {
  const member = memberNotice()
  const aggregate: Notification = {
    ...member,
    id: 'qni_aggregate',
    subject_type: 'team',
    subject_id: 'tem_recorded',
    quota: {
      ...member.quota!,
      scope_kind: 'team',
      scope_id: 'tem_recorded',
      team_id: undefined,
      member_user_id: undefined,
      limit: '100',
      settled: '100',
    },
  }
  page = { items: [member, aggregate], unread_count: 2, next_cursor: null }
  await mount()
  expect(menu().textContent).toContain('Your member quota in Recorded Team (tem_recorded)')
  expect(menu().textContent).toContain('Team: Recorded Team (tem_recorded)')
  expect(menu().textContent).toContain('Settled: 9007199254740993.123456789012345678 USD')
  expect(menu().textContent).toContain('Limit: 1.000000000000000001 USD')
  expect(menu().textContent).toContain('Limit: 100 USD')
  expect(menu().textContent).not.toContain(digest)
  expect(menu().textContent).not.toMatch(/remaining|Personal quota|Email|memberships/)
  expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
})
it('switches live English/Chinese member copy without losing exact recorded money or issuing another read', async () => {
  await mount()
  await act(async () => i18n.changeLanguage('zh'))
  expect(menu().textContent).toContain('您在 Recorded Team（tem_recorded）的成员额度')
  expect(menu().textContent).toContain('已结算：9007199254740993.123456789012345678 USD')
  expect(menu().textContent).toContain('上限：1.000000000000000001 USD')
  expect(menu().textContent).toContain('2026年10月')
  expect(requests.filter((request) => request.url === '/notifications')).toHaveLength(1)
})
it('uses the recorded Team ID alone when its name is unavailable, and preserves known token zero', async () => {
  const notice = memberNotice()
  notice.subject_name = undefined
  notice.detail_code = 'tokens_month_exhausted'
  notice.quota = { ...notice.quota!, dimension: 'tokens', limit: '0', settled: '0', currency: null }
  page.items = [notice]
  await mount()
  expect(menu().textContent).toContain('Your member quota in Team tem_recorded')
  expect(menu().textContent).toContain('Settled: 0 tokens')
  expect(menu().textContent).toContain('Limit: 0 tokens')
  expect(menu().textContent).not.toContain(digest)
})
it('renders only generic unavailable copy for a future scope instead of foreign member facts', async () => {
  const notice = memberNotice()
  page.items = [
    {
      ...notice,
      subject_type: 'future_scope',
      subject_id: 'opaque-future',
      subject_name: 'Untrusted member',
      quota: { ...notice.quota!, scope_kind: 'future_scope', scope_id: 'opaque-future' },
    } as unknown as Notification,
  ]
  await mount()
  expect(menu().textContent).toContain(
    'The monthly quota snapshot was not recorded or is unavailable.',
  )
  expect(menu().textContent).not.toContain('Untrusted member')
  expect(menu().textContent).not.toContain('Settled:')
  expect(menu().textContent).not.toContain('Your member quota')
})
it.each(['other_actor', 'wrong_subject', 'missing_team', 'bad_amount'])(
  'fails closed for malformed private snapshot %s without private facts or read controls',
  async (mode) => {
    const notice = memberNotice()
    switch (mode) {
      case 'other_actor':
        notice.quota!.member_user_id = 'usr_other'
        break
      case 'wrong_subject':
        notice.subject_id = 'B'.repeat(51) + 'A'
        break
      case 'missing_team':
        notice.quota!.team_id = undefined
        break
      case 'bad_amount':
        notice.quota!.settled = '1e18'
        break
    }
    notice.subject_name = 'Untrusted private Team'
    page.items = [notice]
    await mount(false)
    expect(menu().textContent).toContain('Notifications could not be loaded.')
    expect(menu().textContent).not.toContain('Untrusted private Team')
    expect(menu().querySelector('[role="menuitem"]')).toBeNull()
    expect(badge()).toBeNull()
    expect(button('Mark all read')).toBeUndefined()
    expect(requests.filter((request) => request.method === 'post')).toHaveLength(0)
  },
)
it('hides and ignores prior member facts on a genuine structurally-equal same-ms Session renewal', async () => {
  await mount()
  const oldGate = deferred()
  gates.push(oldGate)
  let oldSignal: AbortSignal | undefined
  const old = structuredClone(page)
  inbox = async (config) => {
    oldSignal = config.signal as AbortSignal
    await oldGate.promise
    return old
  }
  await refresh()
  await until(() => expect(menu().textContent).toContain('Loading notifications'))
  inbox = async () => ({ items: [], unread_count: 0, next_cursor: null })
  const prior = cache.getQueryState(sessionKey)!
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await until(() => expect(menu().textContent).toContain('No new notifications'))
  const next = cache.getQueryState(sessionKey)!
  expect(next.data).toBe(prior.data)
  expect(next.dataUpdatedAt).toBe(prior.dataUpdatedAt)
  expect(next.dataUpdateCount).toBe(prior.dataUpdateCount + 1)
  expect(oldSignal?.aborted).toBe(true)
  await act(async () => oldGate.release())
  await settle()
  expect(menu().textContent).not.toContain('Recorded Team')
  expect(badge()).toBeNull()
  expect(button('Mark all read')).toBeUndefined()
  expect(menu().querySelector('[role="menuitem"]')).toBeNull()
  expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(2)
  expect(requests.filter((request) => request.method === 'post')).toHaveLength(0)
})
it('does not restore an old recipient response after an actor change', async () => {
  await mount()
  const gate = deferred()
  gates.push(gate)
  const original = structuredClone(page)
  inbox = async () => {
    await gate.promise
    return original
  }
  await refresh()
  await until(() => expect(menu().textContent).toContain('Loading notifications'))
  actor = 'usr_other'
  csrf = 'csrf-other'
  inbox = async () => ({ items: [], unread_count: 0, next_cursor: null })
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await until(() =>
    expect(cache.getQueryState(notificationsKey(actor, 'unread'))?.status).toBe('success'),
  )
  await act(async () => gate.release())
  await settle()
  expect(menu().textContent).not.toContain('Recorded Team')
  expect(badge()).toBeNull()
  expect(requests.filter((request) => request.method === 'post')).toHaveLength(0)
})
it.each(['single', 'all'])(
  'uses only current-recipient %s read intent and fresh CSRF, then hides revoked authority',
  async (operation) => {
    await mount()
    csrf = 'csrf-renewed'
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey })
    })
    await until(() => expect(menu().textContent).toContain('Recorded Team'))
    readFailure = 403
    page = { items: [], unread_count: 0, next_cursor: null }
    if (operation === 'single')
      await act(async () => menu().querySelector<HTMLElement>('[role="menuitem"]')!.click())
    else await click('Mark all read')
    await until(() =>
      expect(requests.filter((request) => request.method === 'post')).toHaveLength(1),
    )
    const sent = requests.find((request) => request.method === 'post')!
    expect(sent.url).toBe(
      operation === 'single' ? '/notifications/qni_member/read' : '/notifications/read-all',
    )
    expect(sent.headers.get('X-CSRF-Token')).toBe('csrf-renewed')
    expect(sent.params).toBeUndefined()
    await until(() =>
      expect(
        cache.getQueryData<{ pages: NotificationsPage[] }>(notificationsKey(actor, 'unread'))
          ?.pages[0].items,
      ).toHaveLength(0),
    )
    if (!menu()) await click('Notifications')
    expect(menu().textContent).not.toContain('Recorded Team')
    expect(badge()).toBeNull()
    expect(menu().textContent).not.toContain('private server diagnostic')
  },
)
it('rechecks membership during Session renewal and restores only server-provided read history after rejoin', async () => {
  const original = memberNotice()
  original.read = true
  original.read_at = '2026-10-04T12:01:00Z'
  page = { items: [original], unread_count: 0, next_cursor: null }
  await mount()
  await click('All')
  await until(() => expect(menu().textContent).toContain('Recorded Team'))
  sessionGate = deferred()
  await act(async () => {
    void cache.refetchQueries({ queryKey: sessionKey })
  })
  await until(() => expect(menu().textContent).toContain('Loading notifications'))
  expect(menu().querySelector('[role="menuitem"]')).toBeNull()
  expect(badge()).toBeNull()
  page = { items: [], unread_count: 0, next_cursor: null }
  await act(async () => sessionGate!.release())
  sessionGate = undefined
  await until(() => expect(menu().textContent).toContain('No notification history'))
  page = { items: [original], unread_count: 0, next_cursor: null }
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await until(() => expect(menu().textContent).toContain('Recorded Team'))
  await act(async () => menu().querySelector<HTMLElement>('[role="menuitem"]')!.click())
  expect(badge()).toBeNull()
  expect(requests.filter((request) => request.method === 'post')).toHaveLength(0)
  expect(
    cache.getQueryData<{ pages: NotificationsPage[] }>(notificationsKey(actor, 'all'))?.pages[0]
      .items[0].read_at,
  ).toBe(original.read_at)
})

it.each(['single', 'all'])(
  'marks only the current recipient member notice %s and retains its recorded snapshot in history',
  async (operation) => {
    await mount()
    if (operation === 'single')
      await act(async () => menu().querySelector<HTMLElement>('[role="menuitem"]')!.click())
    else await click('Mark all read')
    await until(() => expect(page.items[0].read).toBe(true))
    await until(() =>
      expect(
        cache.getQueryData<{ pages: NotificationsPage[] }>(notificationsKey(actor, 'unread'))
          ?.pages[0].unread_count,
      ).toBe(0),
    )
    if (!menu()) await click('Notifications')
    await click('All')
    await until(() =>
      expect(menu().textContent).toContain('Your member quota in Recorded Team (tem_recorded)'),
    )
    const recorded = cache.getQueryData<{ pages: NotificationsPage[] }>(
      notificationsKey(actor, 'all'),
    )!.pages[0].items[0]
    expect(recorded.read_at).toBe('2026-10-04T12:01:00Z')
    expect(recorded.quota).toEqual(memberNotice().quota)
    expect(badge()).toBeNull()
    const sent = requests.filter((request) => request.method === 'post')
    expect(sent).toHaveLength(1)
    expect(sent[0].url).toBe(
      operation === 'single' ? '/notifications/qni_member/read' : '/notifications/read-all',
    )
    expect(sent[0].headers.get('X-CSRF-Token')).toBe('csrf-current')
    expect(sent[0].params).toBeUndefined()
  },
)
