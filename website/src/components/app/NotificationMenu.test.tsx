import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import { notificationsKey } from '@/api/notifications'
import { sessionKey } from '@/hooks/use-auth'
import i18n from '@/i18n'
import type {
  MonthlyQuotaNotificationSnapshot,
  MonthlyQuotaWarningSnapshot,
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
    csrf_token: `csrf-${actor}`,
  }
}
function quota(
  overrides: Partial<MonthlyQuotaNotificationSnapshot> = {},
): MonthlyQuotaNotificationSnapshot {
  return {
    scope_kind: 'user',
    scope_id: 'usr_member',
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
    ...(snapshot ? { quota: snapshot } : {}),
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
      response.data = session()
    } else if (config.url === '/notifications' && config.method === 'get') {
      if (getGate) await getGate.promise
      const failure = config.params?.cursor ? nextFailure : getFailure
      if (failure) throw httpError(config, failure)
      if (getResponse) return { ...response, data: getResponse.data }
      const result = structuredClone(config.params?.cursor ? nextPage : page)
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

describe('Recipient monthly quota inbox', () => {
  it.each([{}, { items: [null], unread_count: 1 }, { items: [], unread_count: '4' }])(
    'shows a recoverable error for a malformed initial inbox response %#',
    async (data) => {
      getResponse = { data }
      await mount(false)
      expect(menu().textContent).toContain('Notifications could not be loaded.')
      expect(menu().textContent).not.toContain('No new notifications')
      expect(menu().querySelector('[role="menuitem"]')).toBeNull()
      expect(unreadBadge()).toBeNull()
      expect(button('Mark all read')).toBeUndefined()
      getResponse = undefined
      await click('Retry')
      await until(() => expect(menu().textContent).toContain('Settled: 100 tokens'))
    },
  )

  it.each(['refresh', 'pagination'])(
    'hides every cached fact after a malformed %s response',
    async (operation) => {
      page.next_cursor = 'next-page'
      await mount()
      await until(() => expect(menu().textContent).toContain('Settled: 100 tokens'))
      getResponse = { data: { items: [undefined], unread_count: 99 } }
      if (operation === 'pagination') await click('Load more notifications')
      else await refresh()
      await until(() => expect(menu().querySelector('[role="alert"]')).not.toBeNull())
      expect(menu().querySelector('[role="menuitem"]')).toBeNull()
      expect(menu().textContent).not.toContain('Settled: 100 tokens')
      expect(menu().textContent).not.toContain('No new notifications')
      expect(unreadBadge()).toBeNull()
      expect(button('Mark all read')).toBeUndefined()
    },
  )

  it('allows ordinary members to fetch their inbox without operational/settings/SMTP authority or queries', async () => {
    await mount()
    await until(() => expect(menu().textContent).toContain('Monthly token limit reached.'))
    expect(menu().textContent).toContain('Personal quota')
    expect(menu().textContent).toContain('Settled: 100 tokens')
    expect(menu().textContent).toContain('Limit: 100 tokens')
    expect(menu().textContent).toContain('Recorded month:')
    expect(menu().textContent).toContain('Calendar time zone: UTC')
    expect(menu().textContent).toContain('As of Sep 17, 2026')
    expect(menu().textContent).toContain('Policy revision: policy-4')
    expect(menu().textContent).not.toContain('Email')
    expect(menu().textContent).not.toContain('remaining')
    expect(menu().textContent).not.toContain('Runtime publication')
    expect(unreadBadge()?.textContent).toBe('1')
    expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
    expect(cache.getQueryData(notificationsKey('usr_member', 'unread'))).toBeTruthy()
  })

  it('shows immutable Project scope and precise money without a global resource or currency lookup', async () => {
    page.items = [
      {
        ...notice(
          quota({
            scope_kind: 'project',
            scope_id: 'prj_recorded',
            dimension: 'money',
            currency: 'USD',
            limit: '9007199254740993.00000001',
            settled: '9007199254740994.00000002',
          }),
        ),
        subject_type: 'project',
        subject_id: 'prj_recorded',
        subject_name: 'Recorded Project',
      },
    ]
    await mount()
    await until(() =>
      expect(menu().textContent).toContain('Project: Recorded Project (prj_recorded)'),
    )
    expect(menu().textContent).toContain('Settled: 9007199254740994.00000002 USD')
    expect(menu().textContent).toContain('Limit: 9007199254740993.00000001 USD')
    expect(menu().textContent).not.toContain('%')
    expect(menu().textContent).not.toContain('converted')
    expect(
      requests.every((request) => ['/auth/session', '/notifications'].includes(request.url!)),
    ).toBe(true)
  })

  it('preserves large token integers and a recorded zero limit without claiming spending or a crossing', async () => {
    page.items = [
      notice(quota({ limit: '9007199254740993', settled: '9007199254740994' })),
      { ...notice(quota({ limit: '0', settled: '0' })), id: 'qnt_zero' },
    ]
    page.unread_count = 2
    await mount()
    await until(() => expect(menu().textContent).toContain('Settled: 9007199254740994 tokens'))
    expect(menu().textContent).toContain('Limit: 9007199254740993 tokens')
    expect(menu().textContent).toContain('Settled: 0 tokens')
    expect(menu().textContent).toContain('Limit: 0 tokens')
    expect(menu().textContent).not.toMatch(/spent|crossed|100%|remaining/)
  })

  it.each([
    ['absent', { ...notice(), quota: undefined }],
    ['missing amounts', { ...notice(), quota: { ...quota(), settled: undefined } }],
    ['wrong dimension', { ...notice(), quota: { ...quota(), dimension: 'future' } }],
    ['money without currency', { ...notice(quota({ dimension: 'money', currency: null })) }],
    [
      'contradictory native detail',
      { ...notice(), quota: quota({ dimension: 'money', currency: 'USD' }) },
    ],
    ['empty calendar interval', notice(quota({ month_end: '2026-09-01T00:00:00Z' }))],
    ['reversed calendar interval', notice(quota({ month_end: '2026-08-01T00:00:00Z' }))],
    ['observation before calendar', notice(quota({ as_of: '2026-08-31T23:59:59Z' }))],
    ['observation at exclusive calendar end', notice(quota({ as_of: '2026-10-01T00:00:00Z' }))],
    ['contradictory scope type', { ...notice(), subject_type: 'project' }],
    [
      'contradictory Project scope identity',
      {
        ...notice(quota({ scope_kind: 'project', scope_id: 'prj_expected' })),
        subject_type: 'project',
        subject_id: 'prj_other',
        subject_name: 'Wrong Project',
      },
    ],
  ])('leaves %s snapshot facts unavailable rather than synthesizing values', async (_, record) => {
    page.items = [record as Notification]
    await mount()
    await until(() =>
      expect(menu().textContent).toContain(
        'The monthly quota snapshot was not recorded or is unavailable',
      ),
    )
    expect(menu().textContent).not.toContain('Settled:')
    expect(menu().textContent).not.toContain('Limit:')
    expect(menu().textContent).not.toContain('Recorded month:')
    expect(menu().textContent).not.toContain('Wrong Project')
  })

  it('uses neutral fallback copy for future notification kinds and does not render arbitrary event metadata', async () => {
    page.items = [
      {
        ...notice(),
        kind: 'future_notification',
        detail_code: 'untrusted_detail',
        subject_name: 'Unrelated arbitrary name',
        last_seen_at: 'not-a-recorded-date',
      },
    ]
    await mount()
    await until(() => expect(menu().textContent).toContain('A notification requires attention.'))
    expect(menu().textContent).not.toContain('operational event')
    expect(menu().textContent).not.toContain('Unrelated arbitrary name')
    expect(menu().textContent).not.toContain('Settled:')
    expect(menu().textContent).toContain('Unknown time')
  })

  it('keeps native operational subjects, severity and qualified SMTP acceptance for server-authorized operators', async () => {
    role = 'admin'
    page.items = [
      {
        ...notice(),
        kind: 'system_job_failure',
        detail_code: 'publication_failed',
        alert_id: 'alt_1',
        quota: undefined,
        subject_type: 'provider',
        subject_id: 'prv_1',
        subject_name: 'Provider One',
        delivery_status: 'accepted',
      },
    ]
    await mount()
    await until(() => expect(menu().textContent).toContain('Runtime publication failed.'))
    expect(menu().textContent).toContain('Provider: Provider One')
    expect(menu().textContent).toContain(
      'SMTP relay accepted the email; inbox delivery is not confirmed',
    )
    expect(menu().querySelector('[aria-label="High severity"]')).not.toBeNull()
    expect(menu().textContent).not.toContain('Personal quota')
  })

  it.each([403, 404, 503])(
    'hides all cached rows and badge during refresh and after a %s authorization/delivery error',
    async (status) => {
      page.items = [
        {
          ...notice(quota({ scope_kind: 'project', scope_id: 'prj_private' })),
          subject_type: 'project',
          subject_id: 'prj_private',
          subject_name: 'Private Project',
        },
      ]
      await mount()
      await until(() => expect(menu().textContent).toContain('Private Project'))
      getGate = barrier()
      getFailure = status
      await refresh()
      await until(() => expect(menu().textContent).toContain('Loading notifications'))
      expect(menu().querySelector('[role="menuitem"]')).toBeNull()
      expect(unreadBadge()).toBeNull()
      expect(button('Mark all read')).toBeUndefined()
      expect(menu().textContent).not.toContain('Private Project')
      await act(async () => getGate!.release())
      await until(() => expect(menu().textContent).toContain('Notifications could not be loaded.'))
      expect(menu().querySelector('[role="menuitem"]')).toBeNull()
      expect(unreadBadge()).toBeNull()
      expect(menu().textContent).not.toContain('server-private detail')
      expect(menu().textContent).not.toContain('Private Project')
    },
  )

  it('removes a Project notice after the server reauthorizes a revoked manager as an empty inbox', async () => {
    page.items = [notice(quota({ scope_kind: 'project', scope_id: 'prj_revoked' }))]
    await mount()
    await until(() => expect(menu().textContent).toContain('Project: prj_revoked'))
    page = { items: [], unread_count: 0, next_cursor: null }
    await refresh()
    await until(() => expect(menu().textContent).toContain('No new notifications'))
    expect(menu().textContent).not.toContain('prj_revoked')
    expect(unreadBadge()).toBeNull()
    expect(button('Mark all read')).toBeUndefined()
  })

  it('retains actor/status cursor scopes and hides old rows when page authorization fails', async () => {
    page.next_cursor = 'recipient-bound-cursor'
    nextPage = {
      items: [
        {
          ...notice(quota({ scope_kind: 'project', scope_id: 'prj_next' })),
          id: 'qnt_next',
          read: true,
        },
      ],
      next_cursor: null,
      unread_count: 1,
    }
    await mount()
    await click('All')
    await until(() => expect(button('Load more notifications')).toBeDefined())
    getGate = barrier()
    nextFailure = 403
    await click('Load more notifications')
    await until(() => expect(menu().textContent).toContain('Loading notifications'))
    expect(menu().querySelector('[role="menuitem"]')).toBeNull()
    expect(unreadBadge()).toBeNull()
    await act(async () => getGate!.release())
    await until(() =>
      expect(menu().textContent).toContain('More notifications could not be loaded.'),
    )
    expect(menu().querySelector('[role="menuitem"]')).toBeNull()
    expect(unreadBadge()).toBeNull()
    const get = requests.filter(
      (request) => request.url === '/notifications' && request.params?.status === 'all',
    )
    expect(get).toHaveLength(2)
    expect(get[1].params?.cursor).toBe('recipient-bound-cursor')
    page = { items: [], next_cursor: null, unread_count: 0 }
    nextFailure = 0
    await click('Retry')
    await until(() => expect(menu().textContent).toContain('No notification history'))
  })

  it('loads a successful next page with server-authorized Project facts', async () => {
    page.next_cursor = 'recipient-bound-cursor'
    nextPage = {
      items: [
        {
          ...notice(
            quota({
              scope_kind: 'project',
              scope_id: 'prj_next',
              dimension: 'money',
              currency: 'CNY',
              limit: '0.10000000',
              settled: '0.10000000',
            }),
          ),
          id: 'qnt_next',
          read: true,
        },
      ],
      next_cursor: null,
      unread_count: 1,
    }
    await mount()
    await click('All')
    await until(() => expect(button('Load more notifications')).toBeDefined())
    await click('Load more notifications')
    await until(() => expect(menu().textContent).toContain('Project: prj_next'))
    expect(menu().textContent).toContain('Settled: 0.10000000 CNY')
    expect(cache.getQueryData(notificationsKey(actor, 'all'))).toBeTruthy()
    expect(menu().querySelectorAll('[role="menuitem"]')).toHaveLength(2)
  })

  it.each(['single', 'all'])(
    'submits the exact %s read intent with CSRF and reauthorizes a rejected operation',
    async (operation) => {
      await mount()
      await until(() => expect(item()).not.toBeNull())
      readFailure = 403
      page = { items: [], unread_count: 0, next_cursor: null }
      if (operation === 'single') {
        await act(async () => item().click())
        await click('Notifications')
      } else await click('Mark all read')
      await until(() =>
        expect(menu().textContent).toContain('The notification could not be marked as read.'),
      )
      await until(() => expect(menu().textContent).toContain('No new notifications'))
      const post = requests.find((request) => request.method === 'post')!
      expect(post.url).toBe(
        operation === 'single' ? '/notifications/qnt_1/read' : '/notifications/read-all',
      )
      expect(post.headers.get('X-CSRF-Token')).toBe('csrf-usr_member')
      expect(requests.filter((request) => request.url === '/notifications')).toHaveLength(2)
      expect(menu().querySelector('[role="menuitem"]')).toBeNull()
      expect(unreadBadge()).toBeNull()
    },
  )

  it('does not refresh another actor scope when a late failed read finishes', async () => {
    await mount()
    await until(() => expect(item()).not.toBeNull())
    readGate = barrier()
    readFailure = 403
    await act(async () => item().click())
    const post = requests.find((request) => request.method === 'post')!
    actor = 'usr_other'
    page = {
      items: [{ ...notice(quota({ scope_id: actor })), id: 'qnt_other' }],
      unread_count: 1,
      next_cursor: null,
    }
    await act(async () => cache.setQueryData(sessionKey, session()))
    await until(() => expect(cache.getQueryData(notificationsKey(actor, 'unread'))).toBeTruthy())
    await click('Notifications')
    await until(() => expect(menu().querySelector('[role="menuitem"]')).not.toBeNull())
    expect(item().getAttribute('aria-disabled')).not.toBe('true')
    const before = requests.filter((request) => request.url === '/notifications').length
    await act(async () => readGate!.release())
    await act(async () => new Promise((resolve) => setTimeout(resolve, 20)))
    expect(requests.filter((request) => request.url === '/notifications')).toHaveLength(before)
    expect(post.headers.get('X-CSRF-Token')).toBe('csrf-usr_member')
    expect(menu().textContent).not.toContain('The notification could not be marked as read.')
    expect(menu().querySelectorAll('[role="menuitem"]')).toHaveLength(1)
  })

  it('clears visible cached inbox content after session loss', async () => {
    await mount()
    await until(() => expect(menu().textContent).toContain('Settled: 100 tokens'))
    signedIn = false
    await act(async () => cache.invalidateQueries({ queryKey: sessionKey }))
    await until(() =>
      expect(menu().textContent).toContain(
        'Notifications are unavailable for the current session.',
      ),
    )
    expect(menu().querySelector('[role="menuitem"]')).toBeNull()
    expect(unreadBadge()).toBeNull()
    expect(button('Mark all read')).toBeUndefined()
  })

  it('starts in English and updates immutable facts and controls live in Chinese without changing selected history', async () => {
    page.items = [
      {
        ...notice(
          quota({
            scope_kind: 'project',
            scope_id: 'prj_language',
            dimension: 'money',
            currency: 'USD',
            limit: '0.10000000',
            settled: '0.10000000',
          }),
        ),
        subject_type: 'project',
        subject_id: 'prj_language',
        subject_name: 'Project Name',
      },
    ]
    await mount()
    await click('All')
    await until(() => expect(menu().textContent).toContain('Monthly money limit reached.'))
    await act(async () => i18n.changeLanguage('zh'))
    expect(menu().textContent).toContain('月度金额已达到上限。')
    expect(menu().textContent).toContain('已结算：0.10000000 USD')
    expect(menu().textContent).toContain('上限：0.10000000 USD')
    expect(menu().textContent).toContain('Project：Project Name（prj_language）')
    expect(menu().textContent).toContain('2026年9月')
    expect(menu().textContent).toContain('额度日历时区：UTC')
    expect(button('全部').getAttribute('aria-pressed')).toBe('true')
    expect(requests.filter((request) => request.url === '/notifications')).toHaveLength(2)
  })
})

function warning(snapshot: Partial<MonthlyQuotaWarningSnapshot> = {}): Notification {
  const value: MonthlyQuotaWarningSnapshot = {
    ...quota({ settled: '84' }),
    scope_kind: 'user',
    level: 'near',
    threshold: 80,
    threshold_generation: 'personal-monthly-80-90-v1',
    ...snapshot,
  }
  return {
    ...notice(),
    id: 'qwi_1',
    quota: undefined,
    quota_observation_id: undefined,
    quota_warning_observation_id: 'qwo_1',
    quota_warning: value,
    kind: 'monthly_quota_warning',
    detail_code: `${value.dimension}_month_${value.level}`,
    severity: value.level === 'near' ? 'medium' : 'high',
    subject_type: 'user',
    subject_id: 'usr_member',
  }
}
it('renders the fixed personal near warning and switches labels without percentage or current-policy inference', async () => {
  page.items = [warning()]
  await mount()
  await until(() => expect(menu().textContent).toContain('Recorded warning threshold: 80%'))
  expect(menu().textContent).toContain('Personal monthly token warning recorded.')
  expect(menu().textContent).toContain('Settled: 84 tokens')
  expect(menu().textContent).toContain('Limit: 100 tokens')
  expect(menu().textContent).toContain('Calendar time zone: UTC')
  expect(menu().textContent).toContain('As of Sep 17, 2026')
  expect(menu().textContent).toContain('Policy revision: policy-4')
  expect(menu().textContent).toContain('Fixed settled-usage observation')
  expect(menu().textContent).not.toContain('84%')
  expect(menu().textContent).not.toContain('16 tokens')
  expect(menu().querySelector('[aria-label="Medium severity"]')).not.toBeNull()
  expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
  await act(async () => i18n.changeLanguage('zh'))
  expect(menu().textContent).toContain('记录的预警阈值：80%')
  expect(menu().textContent).toContain('已结算：84 Token')
  expect(menu().textContent).toContain('不代表当前剩余额度')
  expect(menu().querySelector('[aria-label="中级别"]')).not.toBeNull()
})
it('shows critical historical money exactly beside exhaustion, Team and unknown inbox records', async () => {
  page.items = [
    warning({
      dimension: 'money',
      currency: 'USD',
      level: 'critical',
      threshold: 90,
      limit: '9007199254740993.123456789012345678',
      settled: '8907199254740993.123456789012345678',
    }),
    notice(),
    {
      ...notice(quota({ scope_kind: 'team', scope_id: 'tem_recorded' })),
      id: 'qnt_team',
      subject_type: 'team',
      subject_id: 'tem_recorded',
      subject_name: 'Recorded Team',
    },
    { ...notice(), id: 'future_1', kind: 'future_kind', detail_code: 'future' },
  ]
  page.unread_count = 4
  await mount()
  await until(() => expect(menu().querySelectorAll('[role="menuitem"]')).toHaveLength(4))
  expect(menu().textContent).toContain('Critical personal monthly money warning recorded.')
  expect(menu().textContent).toContain('Recorded warning threshold: 90%')
  expect(menu().textContent).toContain('Settled: 8907199254740993.123456789012345678 USD')
  expect(menu().textContent).toContain('Limit: 9007199254740993.123456789012345678 USD')
  expect(menu().textContent).toContain('Monthly token limit reached.')
  expect(menu().textContent).toContain('Team: Recorded Team (tem_recorded)')
  expect(menu().textContent).toContain('A notification requires attention.')
  expect(menu().textContent).not.toContain('98.88%')
  expect(
    requests.every((request) => ['/auth/session', '/notifications'].includes(request.url!)),
  ).toBe(true)
  await act(async () => i18n.changeLanguage('zh'))
  expect(menu().textContent).toContain('已记录个人月度金额严重预警。')
  expect(menu().textContent).toContain('8907199254740993.123456789012345678 USD')
})
it.each([
  ['missing', undefined],
  ['unknown threshold', { ...warning().quota_warning, threshold: 85 }],
  ['unknown usage', { ...warning().quota_warning, settled: null }],
  ['wrong owner', { ...warning().quota_warning, scope_id: 'usr_other' }],
])(
  'leaves %s warning unavailable instead of inventing a percentage or private snapshot',
  async (_, value) => {
    page.items = [{ ...warning(), quota_warning: value } as Notification]
    await mount()
    await until(() =>
      expect(menu().textContent).toContain(
        'The monthly quota snapshot was not recorded or is unavailable.',
      ),
    )
    expect(menu().textContent).not.toContain('Recorded warning threshold:')
    expect(menu().textContent).not.toContain('Settled:')
    expect(menu().textContent).not.toContain('Limit:')
    await act(async () => i18n.changeLanguage('zh'))
    expect(menu().textContent).toContain('月度额度快照未记录或不可用。')
  },
)
it('hides warning snapshot during renewed reads and does not revive it after denial', async () => {
  page.items = [warning()]
  await mount()
  await until(() => expect(menu().textContent).toContain('Recorded warning threshold: 80%'))
  getGate = barrier()
  getFailure = 403
  await refresh()
  await until(() => expect(menu().textContent).toContain('Loading notifications'))
  expect(menu().textContent).not.toContain('Recorded warning threshold:')
  expect(unreadBadge()).toBeNull()
  await act(async () => getGate!.release())
  await until(() => expect(menu().textContent).toContain('Notifications could not be loaded.'))
  expect(menu().textContent).not.toContain('84 tokens')
  expect(button('Mark all read')).toBeUndefined()
})
it('marks the exact warning inbox identity read with current Session CSRF and retains immutable history', async () => {
  page.items = [warning()]
  await mount()
  await until(() =>
    expect(menu().textContent).toContain('Personal monthly token warning recorded.'),
  )
  await act(async () => item().click())
  await until(() =>
    expect(requests.some((request) => request.url === '/notifications/qwi_1/read')).toBe(true),
  )
  const request = requests.find((request) => request.url === '/notifications/qwi_1/read')!
  expect(request.headers.get('X-CSRF-Token')).toBe('csrf-usr_member')
  await until(() => expect(page.items[0].read).toBe(true))
  await click('Notifications')
  await until(() => expect(menu()).not.toBeNull())
  await click('All')
  await until(() => expect(menu().textContent).toContain('Recorded warning threshold: 80%'))
  expect(menu().textContent).toContain('Settled: 84 tokens')
})

it('does not turn contradictory warning metadata into an operational subject or SMTP delivery claim', async () => {
  page.items = [
    {
      ...warning(),
      alert_id: 'alt_wrong',
      subject_type: 'provider',
      subject_id: 'prv_private',
      subject_name: 'Untrusted Provider',
      delivery_status: 'accepted',
    },
  ]
  await mount()
  await until(() =>
    expect(menu().textContent).toContain(
      'The monthly quota snapshot was not recorded or is unavailable.',
    ),
  )
  expect(menu().textContent).not.toContain('Untrusted Provider')
  expect(menu().textContent).not.toContain('SMTP')
  expect(menu().textContent).not.toContain('Email')
  expect(menu().textContent).not.toContain('Settled:')
})

function teamWarning(snapshot: Partial<MonthlyQuotaWarningSnapshot> = {}): Notification {
  return {
    ...warning({
      scope_kind: 'team',
      scope_id: 'tem_recorded',
      threshold_generation: 'team-monthly-80-90-v1',
      ...snapshot,
    }),
    id: 'twi_1',
    quota_warning_observation_id: 'two_1',
    subject_type: 'team',
    subject_id: 'tem_recorded',
    subject_name: 'Recorded Team',
  }
}

describe('Recorded Team monthly warning menu', () => {
  it.each([
    {
      dimension: 'tokens',
      level: 'near',
      threshold: 80,
      currency: null,
      en: 'Team monthly token warning recorded.',
      zh: '已记录 Team 月度 Token 预警。',
    },
    {
      dimension: 'tokens',
      level: 'critical',
      threshold: 90,
      currency: null,
      en: 'Critical Team monthly token warning recorded.',
      zh: '已记录 Team 月度 Token 严重预警。',
    },
    {
      dimension: 'money',
      level: 'near',
      threshold: 80,
      currency: 'USD',
      en: 'Team monthly money warning recorded.',
      zh: '已记录 Team 月度金额预警。',
    },
    {
      dimension: 'money',
      level: 'critical',
      threshold: 90,
      currency: 'USD',
      en: 'Critical Team monthly money warning recorded.',
      zh: '已记录 Team 月度金额严重预警。',
    },
  ] as const)(
    'renders $dimension/$level recorded facts and switches language live',
    async (value) => {
      const { en, zh, ...snapshot } = value
      const money = snapshot.dimension === 'money'
      const settled = money ? '8907199254740993.123456789012345678' : '93'
      const limit = money ? '9007199254740993.123456789012345678' : '100'
      page.items = [teamWarning({ ...snapshot, settled, limit })]
      await mount()
      await until(() => expect(menu().textContent).toContain(en))
      expect(menu().textContent).toContain('Team: Recorded Team (tem_recorded)')
      expect(menu().textContent).toContain(`Recorded warning threshold: ${snapshot.threshold}%`)
      expect(menu().textContent).toContain(`Settled: ${settled} ${money ? 'USD' : 'tokens'}`)
      expect(menu().textContent).toContain(`Limit: ${limit} ${money ? 'USD' : 'tokens'}`)
      expect(menu().textContent).toContain('Recorded month: Sep 1, 2026')
      expect(menu().textContent).toContain('Calendar time zone: UTC')
      expect(menu().textContent).toContain('As of Sep 17, 2026')
      expect(menu().textContent).toContain('Policy revision: policy-4')
      expect(menu().textContent).toContain('Fixed settled-usage observation')
      expect(menu().textContent).not.toContain('93%')
      expect(menu().textContent).not.toContain('7 tokens')
      expect(menu().textContent).not.toContain('Personal quota')
      expect(menu().textContent).not.toContain('Email')
      expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain(zh)
      expect(menu().textContent).toContain('Recorded Team')
      expect(menu().textContent).toContain('tem_recorded')
      expect(menu().textContent).toContain(`记录的预警阈值：${snapshot.threshold}%`)
      expect(menu().textContent).toContain(`已结算：${settled} ${money ? 'USD' : 'Token'}`)
      expect(menu().textContent).toContain('不代表当前剩余额度')
      expect(menu().querySelector('[aria-label="通知历史筛选"]')).not.toBeNull()
    },
  )

  it.each([undefined, null, '', '   '])(
    'falls back to exact Team ID for absent recorded name %#',
    async (subject_name) => {
      page.items = [{ ...teamWarning(), subject_name }]
      await mount()
      await until(() => expect(menu().textContent).toContain('Team: tem_recorded'))
      expect(menu().textContent).not.toContain('Recorded Team')
      expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain('tem_recorded')
    },
  )

  it('marks only the exact Team inbox identity read and retains immutable mixed history', async () => {
    const original = teamWarning({
      dimension: 'money',
      currency: 'USD',
      level: 'critical',
      threshold: 90,
      limit: '1.000000000000000001',
      settled: '0.900000000000000001',
    })
    page.items = [original, warning()]
    page.unread_count = 2
    await mount()
    await until(() =>
      expect(menu().textContent).toContain('Critical Team monthly money warning recorded.'),
    )
    const teamItem = [...menu().querySelectorAll<HTMLElement>('[role="menuitem"]')].find((row) =>
      row.textContent?.includes('Team: Recorded Team'),
    )!
    await act(async () => teamItem.click())
    await until(() => expect(page.items[0].read).toBe(true))
    const read = requests.filter((request) => request.method === 'post')
    expect(read).toHaveLength(1)
    expect(read[0].url).toBe('/notifications/twi_1/read')
    expect(read[0].headers.get('X-CSRF-Token')).toBe('csrf-usr_member')
    expect(page.items[0].quota_warning).toEqual(original.quota_warning)
    expect(page.items[1].read).toBe(false)
    await click('Notifications')
    await until(() => expect(menu()).not.toBeNull())
    await click('All')
    await until(() => expect(menu().querySelectorAll('[role="menuitem"]')).toHaveLength(2))
    expect(menu().textContent).toContain('Settled: 0.900000000000000001 USD')
    expect(menu().textContent).toContain('Personal monthly token warning recorded.')
    await act(async () => i18n.changeLanguage('zh'))
    expect(button('全部')?.getAttribute('aria-pressed')).toBe('true')
    expect(menu().textContent).toContain('已记录 Team 月度金额严重预警。')
    expect(menu().textContent).toContain('0.900000000000000001 USD')
    expect(page.items[0].subject_name).toBe('Recorded Team')
    expect(page.items[0].quota_warning_observation_id).toBe('two_1')
  })

  it.each([
    ['wrong Team subject', { subject_id: 'tem_other' }],
    ['aliased Team subject', { subject_id: 'TEM_RECORDED' }],
    [
      'Personal generation',
      {
        quota_warning: {
          ...teamWarning().quota_warning,
          threshold_generation: 'personal-monthly-80-90-v1',
        },
      },
    ],
    ['unknown settled usage', { quota_warning: { ...teamWarning().quota_warning, settled: null } }],
    ['unsupported threshold', { quota_warning: { ...teamWarning().quota_warning, threshold: 85 } }],
    [
      'borrowed operational identity',
      { subject_type: 'provider', subject_name: 'Private Provider', delivery_status: 'accepted' },
    ],
  ])(
    'renders localized unavailable for %s without exposing captured Team facts',
    async (_, changes) => {
      page.items = [{ ...teamWarning(), ...changes } as Notification]
      await mount()
      await until(() =>
        expect(menu().textContent).toContain(
          'The monthly quota snapshot was not recorded or is unavailable.',
        ),
      )
      expect(menu().textContent).toContain('A monthly quota warning was recorded.')
      expect(menu().textContent).not.toContain('Recorded Team')
      expect(menu().textContent).not.toContain('tem_recorded')
      expect(menu().textContent).not.toContain('Private Provider')
      expect(menu().textContent).not.toContain('Settled:')
      expect(menu().textContent).not.toContain('Recorded warning threshold:')
      expect(menu().textContent).not.toContain('Email')
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain('月度额度快照未记录或不可用。')
      expect(menu().textContent).toContain('已记录月度额度预警。')
    },
  )

  it('hides Team snapshots during recipient renewal and cannot restore denied facts', async () => {
    page.items = [teamWarning()]
    await mount()
    await until(() => expect(menu().textContent).toContain('Team: Recorded Team'))
    getGate = barrier()
    getFailure = 403
    await refresh()
    await until(() => expect(menu().textContent).toContain('Loading notifications'))
    expect(menu().textContent).not.toContain('Recorded Team')
    expect(menu().textContent).not.toContain('93 tokens')
    expect(unreadBadge()).toBeNull()
    await act(async () => getGate!.release())
    await until(() => expect(menu().textContent).toContain('Notifications could not be loaded.'))
    expect(menu().textContent).not.toContain('Recorded Team')
    expect(button('Mark all read')).toBeUndefined()
  })
})

function projectWarning(snapshot: Partial<MonthlyQuotaWarningSnapshot> = {}): Notification {
  return {
    ...warning({
      scope_kind: 'project',
      scope_id: 'prj_recorded',
      threshold_generation: 'project-monthly-80-90-v1',
      ...snapshot,
    }),
    id: 'pwi_1',
    quota_warning_observation_id: 'pwo_1',
    subject_type: 'project',
    subject_id: 'prj_recorded',
    subject_name: 'Recorded Project',
  }
}

describe('Recorded Project monthly warning menu', () => {
  it.each([
    {
      dimension: 'tokens',
      level: 'near',
      threshold: 80,
      currency: null,
      en: 'Project monthly token warning recorded.',
      zh: '已记录 Project 月度 Token 预警。',
    },
    {
      dimension: 'tokens',
      level: 'critical',
      threshold: 90,
      currency: null,
      en: 'Critical Project monthly token warning recorded.',
      zh: '已记录 Project 月度 Token 严重预警。',
    },
    {
      dimension: 'money',
      level: 'near',
      threshold: 80,
      currency: 'USD',
      en: 'Project monthly money warning recorded.',
      zh: '已记录 Project 月度金额预警。',
    },
    {
      dimension: 'money',
      level: 'critical',
      threshold: 90,
      currency: 'USD',
      en: 'Critical Project monthly money warning recorded.',
      zh: '已记录 Project 月度金额严重预警。',
    },
  ] as const)(
    'renders $dimension/$level recorded facts and switches language live',
    async (value) => {
      const { en, zh, ...snapshot } = value
      const money = snapshot.dimension === 'money'
      const settled = money ? '8907199254740993.123456789012345678' : '93'
      const limit = money ? '9007199254740993.123456789012345678' : '100'
      page.items = [projectWarning({ ...snapshot, settled, limit })]
      await mount()
      await until(() => expect(menu().textContent).toContain(en))
      expect(menu().textContent).toContain('Project: Recorded Project (prj_recorded)')
      expect(menu().textContent).toContain(`Recorded warning threshold: ${snapshot.threshold}%`)
      expect(menu().textContent).toContain(`Settled: ${settled} ${money ? 'USD' : 'tokens'}`)
      expect(menu().textContent).toContain(`Limit: ${limit} ${money ? 'USD' : 'tokens'}`)
      expect(menu().textContent).toContain('Recorded month: Sep 1, 2026')
      expect(menu().textContent).toContain('Calendar time zone: UTC')
      expect(menu().textContent).toContain('As of Sep 17, 2026')
      expect(menu().textContent).toContain('Policy revision: policy-4')
      expect(menu().textContent).toContain('Fixed settled-usage observation')
      expect(menu().textContent).not.toContain('93%')
      expect(menu().textContent).not.toContain('7 tokens')
      expect(menu().textContent).not.toContain('Personal quota')
      expect(menu().textContent).not.toContain('Email')
      expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain(zh)
      expect(menu().textContent).toContain('Recorded Project')
      expect(menu().textContent).toContain('prj_recorded')
      expect(menu().textContent).toContain(`记录的预警阈值：${snapshot.threshold}%`)
      expect(menu().textContent).toContain(`已结算：${settled} ${money ? 'USD' : 'Token'}`)
      expect(menu().textContent).toContain('不代表当前剩余额度')
      expect(menu().querySelector('[aria-label="通知历史筛选"]')).not.toBeNull()
    },
  )

  it.each([undefined, null, '', '   '])(
    'falls back to exact Project ID for absent recorded name %#',
    async (subject_name) => {
      page.items = [{ ...projectWarning(), subject_name }]
      await mount()
      await until(() => expect(menu().textContent).toContain('Project: prj_recorded'))
      expect(menu().textContent).not.toContain('Recorded Project')
      expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain('prj_recorded')
    },
  )

  it('marks only the exact Project inbox identity read and retains immutable mixed history', async () => {
    const original = projectWarning({
      dimension: 'money',
      currency: 'USD',
      level: 'critical',
      threshold: 90,
      limit: '1.000000000000000001',
      settled: '0.900000000000000001',
    })
    page.items = [original, warning(), teamWarning()]
    page.unread_count = 3
    await mount()
    await until(() =>
      expect(menu().textContent).toContain('Critical Project monthly money warning recorded.'),
    )
    const projectItem = [...menu().querySelectorAll<HTMLElement>('[role="menuitem"]')].find((row) =>
      row.textContent?.includes('Project: Recorded Project'),
    )!
    await act(async () => projectItem.click())
    await until(() => expect(page.items[0].read).toBe(true))
    const read = requests.filter((request) => request.method === 'post')
    expect(read).toHaveLength(1)
    expect(read[0].url).toBe('/notifications/pwi_1/read')
    expect(read[0].headers.get('X-CSRF-Token')).toBe('csrf-usr_member')
    expect(page.items[0].quota_warning).toEqual(original.quota_warning)
    expect(page.items[1].read).toBe(false)
    expect(page.items[2].read).toBe(false)
    await click('Notifications')
    await until(() => expect(menu()).not.toBeNull())
    await click('All')
    await until(() => expect(menu().querySelectorAll('[role="menuitem"]')).toHaveLength(3))
    expect(menu().textContent).toContain('Settled: 0.900000000000000001 USD')
    expect(menu().textContent).toContain('Personal monthly token warning recorded.')
    expect(menu().textContent).toContain('Team monthly token warning recorded.')
    expect(menu().textContent).toContain('Team: Recorded Team (tem_recorded)')
    await act(async () => i18n.changeLanguage('zh'))
    expect(button('全部')?.getAttribute('aria-pressed')).toBe('true')
    expect(menu().textContent).toContain('已记录 Project 月度金额严重预警。')
    expect(menu().textContent).toContain('0.900000000000000001 USD')
    expect(page.items[0].subject_name).toBe('Recorded Project')
    expect(page.items[0].quota_warning_observation_id).toBe('pwo_1')
  })

  it.each([
    ['wrong Project subject', { subject_id: 'prj_other' }],
    ['aliased Project subject', { subject_id: 'PRJ_RECORDED' }],
    ['Team inbox identity', { id: 'twi_1' }],
    [
      'Team generation',
      {
        quota_warning: {
          ...projectWarning().quota_warning,
          threshold_generation: 'team-monthly-80-90-v1',
        },
      },
    ],
    [
      'Personal generation',
      {
        quota_warning: {
          ...projectWarning().quota_warning,
          threshold_generation: 'personal-monthly-80-90-v1',
        },
      },
    ],
    [
      'unknown settled usage',
      { quota_warning: { ...projectWarning().quota_warning, settled: null } },
    ],
    [
      'unsupported threshold',
      { quota_warning: { ...projectWarning().quota_warning, threshold: 85 } },
    ],
    [
      'borrowed operational identity',
      { subject_type: 'provider', subject_name: 'Private Provider', delivery_status: 'accepted' },
    ],
  ])(
    'renders localized unavailable for %s without exposing captured Project facts',
    async (_, changes) => {
      page.items = [{ ...projectWarning(), ...changes } as Notification]
      await mount()
      await until(() =>
        expect(menu().textContent).toContain(
          'The monthly quota snapshot was not recorded or is unavailable.',
        ),
      )
      expect(menu().textContent).toContain('A monthly quota warning was recorded.')
      expect(menu().textContent).not.toContain('Recorded Project')
      expect(menu().textContent).not.toContain('prj_recorded')
      expect(menu().textContent).not.toContain('Private Provider')
      expect(menu().textContent).not.toContain('Settled:')
      expect(menu().textContent).not.toContain('Recorded warning threshold:')
      expect(menu().textContent).not.toContain('Email')
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain('月度额度快照未记录或不可用。')
      expect(menu().textContent).toContain('已记录月度额度预警。')
    },
  )

  it('hides Project snapshots during recipient renewal and cannot restore denied facts', async () => {
    page.items = [projectWarning()]
    await mount()
    await until(() => expect(menu().textContent).toContain('Project: Recorded Project'))
    getGate = barrier()
    getFailure = 403
    await refresh()
    await until(() => expect(menu().textContent).toContain('Loading notifications'))
    expect(menu().textContent).not.toContain('Recorded Project')
    expect(menu().textContent).not.toContain('93 tokens')
    expect(unreadBadge()).toBeNull()
    await act(async () => getGate!.release())
    await until(() => expect(menu().textContent).toContain('Notifications could not be loaded.'))
    expect(menu().textContent).not.toContain('Recorded Project')
    expect(button('Mark all read')).toBeUndefined()
  })
})

function memberWarning(snapshot: Partial<MonthlyQuotaWarningSnapshot> = {}): Notification {
  return {
    ...warning({
      scope_kind: 'team_member',
      scope_id: 'A'.repeat(51) + 'Q',
      threshold_generation: 'team-member-monthly-80-90-v1',
      ...snapshot,
    }),
    id: 'mwi_1',
    quota_warning_observation_id: 'mwo_1',
    subject_type: 'team_member',
    subject_id: 'A'.repeat(51) + 'Q',
    subject_name: 'Recorded Team',
  }
}

describe('Recorded Team member monthly warning menu', () => {
  it.each([
    {
      dimension: 'tokens',
      level: 'near',
      threshold: 80,
      currency: null,
      en: 'Your Team member monthly token warning recorded.',
      zh: '已记录你的 Team 成员月度 Token 预警。',
    },
    {
      dimension: 'tokens',
      level: 'critical',
      threshold: 90,
      currency: null,
      en: 'Critical warning for your Team member monthly tokens recorded.',
      zh: '已记录你的 Team 成员月度 Token 严重预警。',
    },
    {
      dimension: 'money',
      level: 'near',
      threshold: 80,
      currency: 'USD',
      en: 'Your Team member monthly money warning recorded.',
      zh: '已记录你的 Team 成员月度金额预警。',
    },
    {
      dimension: 'money',
      level: 'critical',
      threshold: 90,
      currency: 'USD',
      en: 'Critical warning for your Team member monthly money recorded.',
      zh: '已记录你的 Team 成员月度金额严重预警。',
    },
  ] as const)(
    'renders $dimension/$level recorded facts and switches language live',
    async (value) => {
      const { en, zh, ...snapshot } = value
      const money = snapshot.dimension === 'money'
      const settled = money ? '8907199254740993.123456789012345678' : '93'
      const limit = money ? '9007199254740993.123456789012345678' : '100'
      page.items = [memberWarning({ ...snapshot, settled, limit })]
      await mount()
      await until(() => expect(menu().textContent).toContain(en))
      expect(menu().textContent).toContain('Your member quota in Recorded Team')
      expect(menu().textContent).not.toContain('A'.repeat(51) + 'Q')
      expect(menu().textContent).not.toContain('Team:')
      expect(menu().textContent).not.toContain('undefined')
      expect(menu().textContent).toContain(`Recorded warning threshold: ${snapshot.threshold}%`)
      expect(menu().textContent).toContain(`Settled: ${settled} ${money ? 'USD' : 'tokens'}`)
      expect(menu().textContent).toContain(`Limit: ${limit} ${money ? 'USD' : 'tokens'}`)
      expect(menu().textContent).toContain('Recorded month: Sep 1, 2026')
      expect(menu().textContent).toContain('Calendar time zone: UTC')
      expect(menu().textContent).toContain('As of Sep 17, 2026')
      expect(menu().textContent).toContain('Policy revision: policy-4')
      expect(menu().textContent).toContain('Fixed settled-usage observation')
      expect(menu().textContent).not.toContain('93%')
      expect(menu().textContent).not.toContain('7 tokens')
      expect(menu().textContent).not.toContain('Personal quota')
      expect(menu().textContent).not.toContain('Email')
      expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain(zh)
      expect(menu().textContent).toContain('你在 Recorded Team 的成员额度')
      expect(menu().textContent).not.toContain('A'.repeat(51) + 'Q')
      expect(menu().textContent).toContain(`记录的预警阈值：${snapshot.threshold}%`)
      expect(menu().textContent).toContain(`已结算：${settled} ${money ? 'USD' : 'Token'}`)
      expect(menu().textContent).toContain('不代表当前剩余额度')
      expect(menu().querySelector('[aria-label="通知历史筛选"]')).not.toBeNull()
    },
  )

  it.each([undefined, null, '', '   '])(
    'uses self-scope fallback for an absent recorded Team name %#',
    async (subject_name) => {
      page.items = [{ ...memberWarning(), subject_name }]
      await mount()
      await until(() => expect(menu().textContent).toContain('Your Team member quota'))
      expect(menu().textContent).not.toContain('Recorded Team')
      expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain('你的 Team 成员额度')
      expect(menu().textContent).not.toContain('A'.repeat(51) + 'Q')
    },
  )

  it('marks only the exact Team member inbox identity read and retains immutable mixed history', async () => {
    const original = memberWarning({
      dimension: 'money',
      currency: 'USD',
      level: 'critical',
      threshold: 90,
      limit: '1.000000000000000001',
      settled: '0.900000000000000001',
    })
    page.items = [original, warning(), teamWarning(), projectWarning()]
    page.unread_count = 4
    await mount()
    await until(() =>
      expect(menu().textContent).toContain(
        'Critical warning for your Team member monthly money recorded.',
      ),
    )
    const memberItem = [...menu().querySelectorAll<HTMLElement>('[role="menuitem"]')].find((row) =>
      row.textContent?.includes('Your member quota in Recorded Team'),
    )!
    await act(async () => memberItem.click())
    await until(() => expect(page.items[0].read).toBe(true))
    const read = requests.filter((request) => request.method === 'post')
    expect(read).toHaveLength(1)
    expect(read[0].url).toBe('/notifications/mwi_1/read')
    expect(read[0].headers.get('X-CSRF-Token')).toBe('csrf-usr_member')
    expect(page.items[0].quota_warning).toEqual(original.quota_warning)
    expect(page.items[1].read).toBe(false)
    expect(page.items[2].read).toBe(false)
    expect(page.items[3].read).toBe(false)
    await click('Notifications')
    await until(() => expect(menu()).not.toBeNull())
    await click('All')
    await until(() => expect(menu().querySelectorAll('[role="menuitem"]')).toHaveLength(4))
    expect(menu().textContent).toContain('Settled: 0.900000000000000001 USD')
    expect(menu().textContent).toContain('Personal monthly token warning recorded.')
    expect(menu().textContent).toContain('Team monthly token warning recorded.')
    expect(menu().textContent).toContain('Team: Recorded Team (tem_recorded)')
    expect(menu().textContent).toContain('Project: Recorded Project (prj_recorded)')
    await act(async () => i18n.changeLanguage('zh'))
    expect(button('全部')?.getAttribute('aria-pressed')).toBe('true')
    expect(menu().textContent).toContain('已记录你的 Team 成员月度金额严重预警。')
    expect(menu().textContent).toContain('0.900000000000000001 USD')
    expect(page.items[0].subject_name).toBe('Recorded Team')
    expect(page.items[0].quota_warning_observation_id).toBe('mwo_1')
  })

  it.each([
    ['wrong Team member subject', { subject_id: 'other_digest' }],
    ['aliased member subject', { subject_id: 'a'.repeat(51) + 'q' }],
    [
      'noncanonical digest',
      { quota_warning: { ...memberWarning().quota_warning, scope_id: 'A'.repeat(51) + 'B' } },
    ],
    [
      'borrowed legacy Team ID',
      { quota_warning: { ...memberWarning().quota_warning, team_id: 'tem_private' } },
    ],
    [
      'Project generation',
      {
        quota_warning: {
          ...memberWarning().quota_warning,
          threshold_generation: 'project-monthly-80-90-v1',
        },
      },
    ],
    ['Team inbox identity', { id: 'twi_1' }],
    [
      'Team generation',
      {
        quota_warning: {
          ...memberWarning().quota_warning,
          threshold_generation: 'team-monthly-80-90-v1',
        },
      },
    ],
    [
      'Personal generation',
      {
        quota_warning: {
          ...memberWarning().quota_warning,
          threshold_generation: 'personal-monthly-80-90-v1',
        },
      },
    ],
    [
      'unknown settled usage',
      { quota_warning: { ...memberWarning().quota_warning, settled: null } },
    ],
    [
      'unsupported threshold',
      { quota_warning: { ...memberWarning().quota_warning, threshold: 85 } },
    ],
    [
      'borrowed operational identity',
      { subject_type: 'provider', subject_name: 'Private Provider', delivery_status: 'accepted' },
    ],
  ])(
    'renders localized unavailable for %s without exposing captured Team member facts',
    async (_, changes) => {
      page.items = [{ ...memberWarning(), ...changes } as Notification]
      await mount()
      await until(() =>
        expect(menu().textContent).toContain(
          'The monthly quota snapshot was not recorded or is unavailable.',
        ),
      )
      expect(menu().textContent).toContain('A monthly quota warning was recorded.')
      expect(menu().textContent).not.toContain('Recorded Team')
      expect(menu().textContent).not.toContain('A'.repeat(51) + 'Q')
      expect(menu().textContent).not.toContain('Private Provider')
      expect(menu().textContent).not.toContain('Settled:')
      expect(menu().textContent).not.toContain('Recorded warning threshold:')
      expect(menu().textContent).not.toContain('Email')
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain('月度额度快照未记录或不可用。')
      expect(menu().textContent).toContain('已记录月度额度预警。')
    },
  )

  it('hides Team member snapshots during recipient renewal and cannot restore denied facts', async () => {
    page.items = [memberWarning()]
    await mount()
    await until(() => expect(menu().textContent).toContain('Your member quota in Recorded Team'))
    getGate = barrier()
    getFailure = 403
    await refresh()
    await until(() => expect(menu().textContent).toContain('Loading notifications'))
    expect(menu().textContent).not.toContain('Recorded Team')
    expect(menu().textContent).not.toContain('93 tokens')
    expect(unreadBadge()).toBeNull()
    await act(async () => getGate!.release())
    await until(() => expect(menu().textContent).toContain('Notifications could not be loaded.'))
    expect(menu().textContent).not.toContain('Recorded Team')
    expect(button('Mark all read')).toBeUndefined()
  })
})

function personalKeyWarning(snapshot: Partial<MonthlyQuotaWarningSnapshot> = {}): Notification {
  return {
    ...warning({
      scope_kind: 'personal_key',
      scope_id: 'key_original',
      threshold_generation: 'personal-key-monthly-80-90-v1',
      ...snapshot,
    }),
    id: 'kwi_1',
    quota_warning_observation_id: 'kwo_1',
    subject_type: 'personal_key',
    subject_id: 'key_original',
    subject_name: 'Original recorded Key',
  }
}

describe('Recorded Personal Key shared-account monthly warning menu', () => {
  it.each([
    {
      dimension: 'tokens',
      level: 'near',
      threshold: 80,
      currency: null,
      en: 'Personal Key monthly token warning recorded.',
      zh: '已记录个人 Key 月度 Token 预警。',
    },
    {
      dimension: 'tokens',
      level: 'critical',
      threshold: 90,
      currency: null,
      en: 'Critical Personal Key monthly token warning recorded.',
      zh: '已记录个人 Key 月度 Token 严重预警。',
    },
    {
      dimension: 'money',
      level: 'near',
      threshold: 80,
      currency: 'USD',
      en: 'Personal Key monthly money warning recorded.',
      zh: '已记录个人 Key 月度金额预警。',
    },
    {
      dimension: 'money',
      level: 'critical',
      threshold: 90,
      currency: 'USD',
      en: 'Critical Personal Key monthly money warning recorded.',
      zh: '已记录个人 Key 月度金额严重预警。',
    },
  ] as const)(
    'renders $dimension/$level recorded facts and switches language live',
    async (value) => {
      const { en, zh, ...snapshot } = value
      const money = snapshot.dimension === 'money'
      const settled = money ? '8907199254740993.123456789012345678' : '93'
      const limit = money ? '9007199254740993.123456789012345678' : '100'
      page.items = [personalKeyWarning({ ...snapshot, settled, limit })]
      await mount()
      await until(() => expect(menu().textContent).toContain(en))
      expect(menu().textContent).toContain(
        'Personal Key shared rotation quota: Original recorded Key (original Key key_original)',
      )
      expect(menu().textContent).toContain(`Recorded warning threshold: ${snapshot.threshold}%`)
      expect(menu().textContent).toContain(`Settled: ${settled} ${money ? 'USD' : 'tokens'}`)
      expect(menu().textContent).toContain(`Limit: ${limit} ${money ? 'USD' : 'tokens'}`)
      expect(menu().textContent).toContain('Recorded month: Sep 1, 2026')
      expect(menu().textContent).toContain('Calendar time zone: UTC')
      expect(menu().textContent).toContain('As of Sep 17, 2026')
      expect(menu().textContent).toContain('Policy revision: policy-4')
      expect(menu().textContent).toContain('Fixed settled-usage observation')
      expect(menu().textContent).not.toContain('93%')
      expect(menu().textContent).not.toContain('7 tokens')
      expect(menu().textContent).not.toContain('Personal quota')
      expect(menu().textContent).not.toContain('Email')
      expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain(zh)
      expect(menu().textContent).toContain('Original recorded Key')
      expect(menu().textContent).toContain('key_original')
      expect(menu().textContent).toContain(`记录的预警阈值：${snapshot.threshold}%`)
      expect(menu().textContent).toContain(`已结算：${settled} ${money ? 'USD' : 'Token'}`)
      expect(menu().textContent).toContain('不代表当前剩余额度')
      expect(menu().querySelector('[aria-label="通知历史筛选"]')).not.toBeNull()
    },
  )

  it.each([undefined, null, '', '   '])(
    'falls back to exact original root Key ID for absent recorded original root name %#',
    async (subject_name) => {
      page.items = [{ ...personalKeyWarning(), subject_name }]
      await mount()
      await until(() =>
        expect(menu().textContent).toContain(
          'Personal Key shared rotation quota: original Key key_original',
        ),
      )
      expect(menu().textContent).not.toContain('Original recorded Key')
      expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain('key_original')
    },
  )

  it('marks only the exact original root inbox identity read and retains immutable mixed history', async () => {
    const original = personalKeyWarning({
      dimension: 'money',
      currency: 'USD',
      level: 'critical',
      threshold: 90,
      limit: '1.000000000000000001',
      settled: '0.900000000000000001',
    })
    page.items = [original, warning(), teamWarning()]
    page.unread_count = 3
    await mount()
    await until(() =>
      expect(menu().textContent).toContain('Critical Personal Key monthly money warning recorded.'),
    )
    const rootItem = [...menu().querySelectorAll<HTMLElement>('[role="menuitem"]')].find((row) =>
      row.textContent?.includes('Personal Key shared rotation quota: Original recorded Key'),
    )!
    await act(async () => rootItem.click())
    await until(() => expect(page.items[0].read).toBe(true))
    const read = requests.filter((request) => request.method === 'post')
    expect(read).toHaveLength(1)
    expect(read[0].url).toBe('/notifications/kwi_1/read')
    expect(read[0].headers.get('X-CSRF-Token')).toBe('csrf-usr_member')
    expect(page.items[0].quota_warning).toEqual(original.quota_warning)
    expect(page.items[1].read).toBe(false)
    expect(page.items[2].read).toBe(false)
    await click('Notifications')
    await until(() => expect(menu()).not.toBeNull())
    await click('All')
    await until(() => expect(menu().querySelectorAll('[role="menuitem"]')).toHaveLength(3))
    expect(menu().textContent).toContain('Settled: 0.900000000000000001 USD')
    expect(menu().textContent).toContain('Personal monthly token warning recorded.')
    expect(menu().textContent).toContain('Team monthly token warning recorded.')
    expect(menu().textContent).toContain('Team: Recorded Team (tem_recorded)')
    await act(async () => i18n.changeLanguage('zh'))
    expect(button('全部')?.getAttribute('aria-pressed')).toBe('true')
    expect(menu().textContent).toContain('已记录个人 Key 月度金额严重预警。')
    expect(menu().textContent).toContain('0.900000000000000001 USD')
    expect(page.items[0].subject_name).toBe('Original recorded Key')
    expect(page.items[0].quota_warning_observation_id).toBe('kwo_1')
  })

  it.each([
    ['wrong root subject', { subject_id: 'key_other' }],
    ['aliased root subject', { subject_id: 'KEY_ORIGINAL' }],
    ['Team inbox identity', { id: 'twi_1' }],
    [
      'Team generation',
      {
        quota_warning: {
          ...personalKeyWarning().quota_warning,
          threshold_generation: 'team-monthly-80-90-v1',
        },
      },
    ],
    [
      'Personal generation',
      {
        quota_warning: {
          ...personalKeyWarning().quota_warning,
          threshold_generation: 'personal-monthly-80-90-v1',
        },
      },
    ],
    [
      'unknown settled usage',
      { quota_warning: { ...personalKeyWarning().quota_warning, settled: null } },
    ],
    [
      'unsupported threshold',
      { quota_warning: { ...personalKeyWarning().quota_warning, threshold: 85 } },
    ],
    [
      'borrowed operational identity',
      { subject_type: 'provider', subject_name: 'Private Provider', delivery_status: 'accepted' },
    ],
  ])(
    'renders localized unavailable for %s without exposing captured original root facts',
    async (_, changes) => {
      page.items = [{ ...personalKeyWarning(), ...changes } as Notification]
      await mount()
      await until(() =>
        expect(menu().textContent).toContain(
          'The monthly quota snapshot was not recorded or is unavailable.',
        ),
      )
      expect(menu().textContent).toContain('A monthly quota warning was recorded.')
      expect(menu().textContent).not.toContain('Original recorded Key')
      expect(menu().textContent).not.toContain('key_original')
      expect(menu().textContent).not.toContain('Private Provider')
      expect(menu().textContent).not.toContain('Settled:')
      expect(menu().textContent).not.toContain('Recorded warning threshold:')
      expect(menu().textContent).not.toContain('Email')
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain('月度额度快照未记录或不可用。')
      expect(menu().textContent).toContain('已记录月度额度预警。')
    },
  )

  it('hides original root snapshots during recipient renewal and cannot restore denied facts', async () => {
    page.items = [personalKeyWarning()]
    await mount()
    await until(() =>
      expect(menu().textContent).toContain(
        'Personal Key shared rotation quota: Original recorded Key',
      ),
    )
    getGate = barrier()
    getFailure = 403
    await refresh()
    await until(() => expect(menu().textContent).toContain('Loading notifications'))
    expect(menu().textContent).not.toContain('Original recorded Key')
    expect(menu().textContent).not.toContain('93 tokens')
    expect(unreadBadge()).toBeNull()
    await act(async () => getGate!.release())
    await until(() => expect(menu().textContent).toContain('Notifications could not be loaded.'))
    expect(menu().textContent).not.toContain('Original recorded Key')
    expect(button('Mark all read')).toBeUndefined()
  })
})

it('marks all mixed warning scopes read without changing original root history', async () => {
  const original = personalKeyWarning({
    dimension: 'money',
    currency: 'USD',
    limit: '1.000000000000000001',
    settled: '0.800000000000000001',
  })
  page.items = [original, warning(), teamWarning(), projectWarning(), memberWarning()]
  page.unread_count = 5
  const before = structuredClone(page.items)
  await mount()
  await until(() => expect(menu().querySelectorAll('[role="menuitem"]')).toHaveLength(5))
  await click('Mark all read')
  await until(() => expect(page.items.every((row) => row.read)).toBe(true))
  const writes = requests.filter((request) => request.method === 'post')
  expect(writes).toHaveLength(1)
  expect(writes[0].url).toBe('/notifications/read-all')
  expect(writes[0].headers.get('X-CSRF-Token')).toBe('csrf-usr_member')
  expect(
    page.items.map((row, index) => ({
      ...row,
      read: before[index].read,
      read_at: before[index].read_at,
    })),
  ).toEqual(before)
  await until(() => expect(menu().textContent).toContain('No new notifications'))
  await click('All')
  await until(() => expect(menu().querySelectorAll('[role="menuitem"]')).toHaveLength(5))
  expect(menu().textContent).toContain('Original recorded Key (original Key key_original)')
  expect(menu().textContent).toContain('0.800000000000000001 USD')
  expect(menu().textContent).toContain('Your member quota in Recorded Team')
  expect(
    requests.every(
      (request) => request.url === '/auth/session' || request.url?.startsWith('/notifications'),
    ),
  ).toBe(true)
})

function projectKeyWarning(snapshot: Partial<MonthlyQuotaWarningSnapshot> = {}): Notification {
  return {
    ...warning({
      scope_kind: 'project_key',
      scope_id: 'pky_original',
      threshold_generation: 'project-key-monthly-80-90-v1',
      ...snapshot,
    }),
    id: 'jwi_1',
    quota_warning_observation_id: 'jwo_1',
    subject_type: 'project_key',
    subject_id: 'pky_original',
    subject_name: 'Original recorded Key',
  }
}

describe('Recorded Project Key shared-account monthly warning menu', () => {
  it.each([
    {
      dimension: 'tokens',
      level: 'near',
      threshold: 80,
      currency: null,
      en: 'Project Key monthly token warning recorded.',
      zh: '已记录Project Key 月度 Token 预警。',
    },
    {
      dimension: 'tokens',
      level: 'critical',
      threshold: 90,
      currency: null,
      en: 'Critical Project Key monthly token warning recorded.',
      zh: '已记录Project Key 月度 Token 严重预警。',
    },
    {
      dimension: 'money',
      level: 'near',
      threshold: 80,
      currency: 'USD',
      en: 'Project Key monthly money warning recorded.',
      zh: '已记录Project Key 月度金额预警。',
    },
    {
      dimension: 'money',
      level: 'critical',
      threshold: 90,
      currency: 'USD',
      en: 'Critical Project Key monthly money warning recorded.',
      zh: '已记录Project Key 月度金额严重预警。',
    },
  ] as const)(
    'renders $dimension/$level recorded facts and switches language live',
    async (value) => {
      const { en, zh, ...snapshot } = value
      const money = snapshot.dimension === 'money'
      const settled = money ? '8907199254740993.123456789012345678' : '93'
      const limit = money ? '9007199254740993.123456789012345678' : '100'
      page.items = [projectKeyWarning({ ...snapshot, settled, limit })]
      await mount()
      await until(() => expect(menu().textContent).toContain(en))
      expect(menu().textContent).toContain(
        'Project Key shared rotation quota: Original recorded Key (original Key pky_original)',
      )
      expect(menu().textContent).toContain(`Recorded warning threshold: ${snapshot.threshold}%`)
      expect(menu().textContent).toContain(`Settled: ${settled} ${money ? 'USD' : 'tokens'}`)
      expect(menu().textContent).toContain(`Limit: ${limit} ${money ? 'USD' : 'tokens'}`)
      expect(menu().textContent).toContain('Recorded month: Sep 1, 2026')
      expect(menu().textContent).toContain('Calendar time zone: UTC')
      expect(menu().textContent).toContain('As of Sep 17, 2026')
      expect(menu().textContent).toContain('Policy revision: policy-4')
      expect(menu().textContent).toContain('Fixed settled-usage observation')
      expect(menu().textContent).not.toContain('93%')
      expect(menu().textContent).not.toContain('7 tokens')
      expect(menu().textContent).not.toContain('Personal quota')
      expect(menu().textContent).not.toContain('Email')
      expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain(zh)
      expect(menu().textContent).toContain('Original recorded Key')
      expect(menu().textContent).toContain('pky_original')
      expect(menu().textContent).toContain(`记录的预警阈值：${snapshot.threshold}%`)
      expect(menu().textContent).toContain(`已结算：${settled} ${money ? 'USD' : 'Token'}`)
      expect(menu().textContent).toContain('不代表当前剩余额度')
      expect(menu().querySelector('[aria-label="通知历史筛选"]')).not.toBeNull()
    },
  )

  it.each([undefined, null, '', '   '])(
    'falls back to exact original root Key ID for absent recorded original root name %#',
    async (subject_name) => {
      page.items = [{ ...projectKeyWarning(), subject_name }]
      await mount()
      await until(() =>
        expect(menu().textContent).toContain(
          'Project Key shared rotation quota: original Key pky_original',
        ),
      )
      expect(menu().textContent).not.toContain('Original recorded Key')
      expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/notifications'])
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain('pky_original')
    },
  )

  it('marks only the exact original root inbox identity read and retains immutable mixed history', async () => {
    const original = projectKeyWarning({
      dimension: 'money',
      currency: 'USD',
      level: 'critical',
      threshold: 90,
      limit: '1.000000000000000001',
      settled: '0.900000000000000001',
    })
    page.items = [original, warning(), teamWarning()]
    page.unread_count = 3
    await mount()
    await until(() =>
      expect(menu().textContent).toContain('Critical Project Key monthly money warning recorded.'),
    )
    const rootItem = [...menu().querySelectorAll<HTMLElement>('[role="menuitem"]')].find((row) =>
      row.textContent?.includes('Project Key shared rotation quota: Original recorded Key'),
    )!
    await act(async () => rootItem.click())
    await until(() => expect(page.items[0].read).toBe(true))
    const read = requests.filter((request) => request.method === 'post')
    expect(read).toHaveLength(1)
    expect(read[0].url).toBe('/notifications/jwi_1/read')
    expect(read[0].headers.get('X-CSRF-Token')).toBe('csrf-usr_member')
    expect(page.items[0].quota_warning).toEqual(original.quota_warning)
    expect(page.items[1].read).toBe(false)
    expect(page.items[2].read).toBe(false)
    await click('Notifications')
    await until(() => expect(menu()).not.toBeNull())
    await click('All')
    await until(() => expect(menu().querySelectorAll('[role="menuitem"]')).toHaveLength(3))
    expect(menu().textContent).toContain('Settled: 0.900000000000000001 USD')
    expect(menu().textContent).toContain('Personal monthly token warning recorded.')
    expect(menu().textContent).toContain('Team monthly token warning recorded.')
    expect(menu().textContent).toContain('Team: Recorded Team (tem_recorded)')
    await act(async () => i18n.changeLanguage('zh'))
    expect(button('全部')?.getAttribute('aria-pressed')).toBe('true')
    expect(menu().textContent).toContain('已记录Project Key 月度金额严重预警。')
    expect(menu().textContent).toContain('0.900000000000000001 USD')
    expect(page.items[0].subject_name).toBe('Original recorded Key')
    expect(page.items[0].quota_warning_observation_id).toBe('jwo_1')
  })

  it.each([
    ['wrong root subject', { subject_id: 'pky_other' }],
    ['aliased root subject', { subject_id: 'PKY_ORIGINAL' }],
    ['Team inbox identity', { id: 'twi_1' }],
    ['Personal Key inbox identity', { id: 'kwi_1' }],
    ['Personal Key subject identity', { subject_type: 'personal_key' }],
    [
      'Personal Key generation',
      {
        quota_warning: {
          ...projectKeyWarning().quota_warning,
          threshold_generation: 'personal-key-monthly-80-90-v1',
        },
      },
    ],
    [
      'Team generation',
      {
        quota_warning: {
          ...projectKeyWarning().quota_warning,
          threshold_generation: 'team-monthly-80-90-v1',
        },
      },
    ],
    [
      'Personal generation',
      {
        quota_warning: {
          ...projectKeyWarning().quota_warning,
          threshold_generation: 'personal-monthly-80-90-v1',
        },
      },
    ],
    [
      'unknown settled usage',
      { quota_warning: { ...projectKeyWarning().quota_warning, settled: null } },
    ],
    [
      'unsupported threshold',
      { quota_warning: { ...projectKeyWarning().quota_warning, threshold: 85 } },
    ],
    [
      'borrowed operational identity',
      { subject_type: 'provider', subject_name: 'Private Provider', delivery_status: 'accepted' },
    ],
  ])(
    'renders localized unavailable for %s without exposing captured original root facts',
    async (_, changes) => {
      page.items = [{ ...projectKeyWarning(), ...changes } as Notification]
      await mount()
      await until(() =>
        expect(menu().textContent).toContain(
          'The monthly quota snapshot was not recorded or is unavailable.',
        ),
      )
      expect(menu().textContent).toContain('A monthly quota warning was recorded.')
      expect(menu().textContent).not.toContain('Original recorded Key')
      expect(menu().textContent).not.toContain('pky_original')
      expect(menu().textContent).not.toContain('Private Provider')
      expect(menu().textContent).not.toContain('Settled:')
      expect(menu().textContent).not.toContain('Recorded warning threshold:')
      expect(menu().textContent).not.toContain('Email')
      await act(async () => i18n.changeLanguage('zh'))
      expect(menu().textContent).toContain('月度额度快照未记录或不可用。')
      expect(menu().textContent).toContain('已记录月度额度预警。')
    },
  )

  it('hides original root snapshots during recipient renewal and cannot restore denied facts', async () => {
    page.items = [projectKeyWarning()]
    await mount()
    await until(() =>
      expect(menu().textContent).toContain(
        'Project Key shared rotation quota: Original recorded Key',
      ),
    )
    getGate = barrier()
    getFailure = 403
    await refresh()
    await until(() => expect(menu().textContent).toContain('Loading notifications'))
    expect(menu().textContent).not.toContain('Original recorded Key')
    expect(menu().textContent).not.toContain('93 tokens')
    expect(unreadBadge()).toBeNull()
    await act(async () => getGate!.release())
    await until(() => expect(menu().textContent).toContain('Notifications could not be loaded.'))
    expect(menu().textContent).not.toContain('Original recorded Key')
    expect(button('Mark all read')).toBeUndefined()
  })
})

it('marks all mixed warning scopes read without changing original Project root history', async () => {
  const original = projectKeyWarning({
    dimension: 'money',
    currency: 'USD',
    limit: '1.000000000000000001',
    settled: '0.800000000000000001',
  })
  page.items = [
    original,
    personalKeyWarning(),
    warning(),
    teamWarning(),
    projectWarning(),
    memberWarning(),
  ]
  page.unread_count = 6
  const before = structuredClone(page.items)
  await mount()
  await until(() => expect(menu().querySelectorAll('[role="menuitem"]')).toHaveLength(6))
  await click('Mark all read')
  await until(() => expect(page.items.every((row) => row.read)).toBe(true))
  const writes = requests.filter((request) => request.method === 'post')
  expect(writes).toHaveLength(1)
  expect(writes[0].url).toBe('/notifications/read-all')
  expect(writes[0].headers.get('X-CSRF-Token')).toBe('csrf-usr_member')
  expect(
    page.items.map((row, index) => ({
      ...row,
      read: before[index].read,
      read_at: before[index].read_at,
    })),
  ).toEqual(before)
  await until(() => expect(menu().textContent).toContain('No new notifications'))
  await click('All')
  await until(() => expect(menu().querySelectorAll('[role="menuitem"]')).toHaveLength(6))
  expect(menu().textContent).toContain('Original recorded Key (original Key pky_original)')
  expect(menu().textContent).toContain('0.800000000000000001 USD')
  expect(menu().textContent).toContain('Your member quota in Recorded Team')
  expect(menu().textContent).toContain(
    'Personal Key shared rotation quota: Original recorded Key (original Key key_original)',
  )
  expect(
    requests.every(
      (request) => request.url === '/auth/session' || request.url?.startsWith('/notifications'),
    ),
  ).toBe(true)
})
