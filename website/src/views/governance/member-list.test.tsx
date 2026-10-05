import { memberApprovalReview } from './member-approval.fixture'
import {
  memberStateFixture,
  memberStateResultFixture,
  stateReviewETag,
} from './member-state.fixture'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import MembersPage from './members'
import { memberListPage, memberListRow } from './member-list.fixture'
import type { MemberListPage } from '@/types/member-list'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let actor: string,
  permissions: string[],
  requests: InternalAxiosRequestConfig[],
  page: MemberListPage,
  listError: number,
  sessionError: number
const oldAdapter = client.defaults.adapter
let listGate: ReturnType<typeof gate> | undefined,
  sessionGate: ReturnType<typeof gate> | undefined,
  permissionGate: ReturnType<typeof gate> | undefined,
  postGate: ReturnType<typeof gate> | undefined,
  gates: ReturnType<typeof gate>[]
function gate() {
  let release!: () => void
  const promise = new Promise<void>((r) => {
    release = r
  })
  return { promise, release }
}
function controlledGate() {
  const g = gate()
  gates.push(g)
  return g
}
function failure(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('Controlled failure', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: { message: 'Controlled unavailable' },
  })
}
beforeEach(() => {
  actor = 'usr_admin'
  permissions = ['members.read', 'members.write', 'teams.read_all', 'calls.read_all']
  requests = []
  page = memberListPage()
  listError = sessionError = 0
  gates = []
  listGate = sessionGate = permissionGate = postGate = undefined
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown
    if (config.url === '/auth/session') {
      data = {
        user: { id: actor, name: 'Reader', email: 'reader@example.invalid', role: 'admin' },
        csrf_token: 'controlled-csrf',
      }
      if (sessionGate) await sessionGate.promise
      if (sessionError) throw failure(config, sessionError)
    } else if (config.url === '/auth/permissions') {
      data = { permissions: [...permissions] }
      if (permissionGate) await permissionGate.promise
    } else if (config.url === '/admin/members' && config.method === 'get') {
      data = structuredClone(page)
      if (listGate) await listGate.promise
      if (listError) throw failure(config, listError)
    } else if (config.url === '/admin/members/usr_target/approval') {
      if (config.method === 'patch') throw failure(config, 503)
      const review = memberApprovalReview()
      return {
        config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders({ etag: `"${review.review_etag}"` }),
        data: review,
      }
    } else if (config.url === '/admin/members/usr_target/state') {
      data = memberStateFixture(page.items[0], actor, 'admin', permissions)
    } else if (config.url === '/admin/members/usr_target' && config.method === 'patch') {
      data = memberStateResultFixture(
        memberStateFixture(page.items[0], actor, 'admin', permissions),
        JSON.parse(config.data),
      )
      if (postGate) await postGate.promise
    } else throw new Error(`Unexpected controlled request ${config.method} ${config.url}`)
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(
        config.url?.endsWith('/state') || config.method === 'patch'
          ? { etag: `"${stateReviewETag}"`, 'cache-control': 'private, no-store' }
          : {},
      ),
      data,
    }
  }
})
afterEach(async () => {
  for (const g of gates) g.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = oldAdapter
  await i18n.changeLanguage('en')
  vi.restoreAllMocks()
})
async function until(check: () => void) {
  await vi.waitFor(
    async () => {
      await act(async () => {
        check()
      })
    },
    { timeout: 2000 },
  )
}
async function mount() {
  router = createMemoryRouter(
    [
      { path: '/admin/members', element: <MembersPage /> },
      { path: '/admin/members/:memberId', element: <p>Controlled detail</p> },
      { path: '/admin/members/:memberId/offboarding', element: <p>Controlled offboarding</p> },
      { path: '/admin/calls', element: <p>Controlled platform calls</p> },
    ],
    { initialEntries: ['/admin/members'] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain('target@example.invalid'))
}
async function button(text: string) {
  await act(async () => {
    const b = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
      (b) => b.textContent === text,
    )
    expect(b).toBeDefined()
    b!.click()
  })
}
async function menu() {
  await act(async () =>
    document.querySelector<HTMLButtonElement>('[aria-label="Member actions for Target"]')!.click(),
  )
  await until(() => expect(document.querySelector('[role="menu"]')).not.toBeNull())
}
async function item(text: string) {
  await act(async () => {
    const e = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
      (e) => e.textContent === text,
    )
    expect(e).toBeDefined()
    e!.click()
  })
}
async function invalidate(key: unknown[]) {
  await act(async () => {
    void cache.invalidateQueries({ queryKey: key })
  })
}
it('renders exactly eleven approved columns, exact strings and historical login without per-row requests', async () => {
  await mount()
  expect([...host.querySelectorAll('th')].map((e) => e.textContent)).toEqual([
    'Name',
    'Email',
    'Status',
    'Teams',
    'Monthly Tokens used / stored limit',
    'Monthly Budget used / stored limit',
    'Personal API Keys',
    'Recent login',
    'Created',
    'Updated',
    'Actions',
  ])
  expect(host.textContent).toContain('9,007,199,254,740,993 / 9,007,199,254,740,995')
  expect(host.textContent).toContain(
    '1.000000000000000001 USD / 99,999,999,999,999,999.000000000000000001 USD',
  )
  expect(host.textContent).toContain('Historical login time unavailable')
  expect(host.querySelector('[name="role"]')).not.toBeNull()
  expect(requests.filter((r) => r.url === '/admin/members')).toHaveLength(1)
  expect(requests.some((r) => r.url?.match(/overview|teams|limits|keys|roles/))).toBe(false)
})
it('uses the compact menu and opens only generic platform calls with its independent permission', async () => {
  await mount()
  await menu()
  expect(document.querySelector('[role="menu"]')?.textContent).toContain('Review limits')
  expect(document.querySelector('[role="menu"]')?.textContent).not.toContain('Adjust limits')
  await item('Open platform calls')
  expect(router.state.location.pathname).toBe('/admin/calls')
  expect(router.state.location.search).toBe('')
})
it('withholds Team facts and calls action without independent permissions; policy reads do not grant writes', async () => {
  permissions = ['members.read']
  await mount()
  expect(host.textContent).not.toContain('Retained Team')
  expect(host.textContent).toContain('Team read access unavailable')
  await menu()
  expect(document.querySelector('[role="menu"]')?.textContent).not.toContain('Open platform calls')
  expect(document.querySelector('[role="menu"]')?.textContent).not.toContain('Disable')
  expect(document.querySelector('[role="menu"]')?.textContent).toContain('Review limits')
})
it('opens exact Key and limits read destinations without writing', async () => {
  await mount()
  await menu()
  await item('Manage API Keys')
  expect(router.state.location.pathname).toBe('/admin/members/usr_target')
  expect(router.state.location.search).toBe('?tab=keys')
  expect(requests.some((r) => r.method !== 'get')).toBe(false)
})
it('keeps zero, null, unavailable journal and Team overflow distinct', async () => {
  const r = page.items[0]
  r.total_personal_keys = '0'
  r.personal.tokens_month = '0'
  r.personal.money_month = null
  r.personal.currency = null
  r.personal.usage_status = 'unavailable'
  r.personal.usage = null
  r.personal.active_reservations = null
  r.teams = { status: 'overflow', items: null }
  await mount()
  expect(host.textContent).toContain('Unknown / 0')
  expect(host.textContent).toContain('Unknown / Not set')
  expect(host.textContent).toContain('Team summary exceeds query budget')
  expect(host.textContent).not.toContain('No retained Teams')
})
it('displays retained inactive Team context and separate monthly/live/unknown money without conversion', async () => {
  await mount()
  await act(async () =>
    document.querySelector<HTMLButtonElement>('[aria-label="Retained Teams for Target"]')!.focus(),
  )
  await until(() =>
    expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('Archived Team'),
  )
  expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('Disabled membership')
  await act(async () =>
    document
      .querySelector<HTMLButtonElement>(
        '[aria-label="Monthly Personal budget context for Target"]',
      )!
      .focus(),
  )
  await until(() =>
    expect(document.querySelector('[role="tooltip"]')?.textContent).toContain(
      '2.000000000000000003 EUR',
    ),
  )
  expect(document.querySelector('[role="tooltip"]')?.textContent).toContain(
    '0.000000000000000001 USD',
  )
  expect(document.querySelector('[role="tooltip"]')?.textContent).toContain(
    '0.000000000000000003 USD',
  )
  expect(document.querySelector('[role="tooltip"]')?.textContent).toContain(
    'Unknown amount records: 2',
  )
  expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('Asia/Shanghai')
})
it('translates columns, unknown facts and menu live without making another list request', async () => {
  await mount()
  const n = requests.length
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('本月 Token 已用 / 已存限额')
  expect(host.textContent).toContain('历史登录时间不可用')
  await act(async () =>
    document.querySelector<HTMLButtonElement>('[aria-label="Target 的成员操作"]')!.click(),
  )
  await until(() =>
    expect(document.querySelector('[role="menu"]')?.textContent).toContain('打开平台调用记录'),
  )
  expect(requests).toHaveLength(n)
})
it('hides cached rows, portal menus and status confirmation during a list renewal then rejects an error without restoring them', async () => {
  await mount()
  await menu()
  await item('Disable')
  await until(() => expect(document.querySelector('[role="dialog"] textarea')).not.toBeNull())
  expect(document.querySelector('[role="dialog"]')).not.toBeNull()
  const confirm = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === 'Confirm disable',
  )!
  listGate = controlledGate()
  await invalidate(['admin', 'members'])
  await until(() => expect(host.textContent).not.toContain('target@example.invalid'))
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(document.querySelector('[role="menu"]')).toBeNull()
  await act(async () => confirm.click())
  expect(requests.some((r) => r.method === 'patch')).toBe(false)
  listError = 503
  await act(async () => listGate!.release())
  await until(() =>
    expect(host.textContent).toContain(
      'The action failed. Check the service connection and retry.',
    ),
  )
  expect(host.textContent).not.toContain('target@example.invalid')
})
it.each(['session', 'permission'] as const)(
  'hides rows/dialogs during %s renewal and does not restore rejected write intent',
  async (kind) => {
    await mount()
    await menu()
    await item('Disable')
    await until(() => expect(document.querySelector('[role="dialog"] textarea')).not.toBeNull())
    if (kind === 'session') sessionGate = controlledGate()
    else permissionGate = controlledGate()
    await invalidate(kind === 'session' ? ['auth', 'session'] : ['permissions'])
    await until(() => expect(host.textContent).not.toContain('target@example.invalid'))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    if (kind === 'session') {
      await act(async () => sessionGate!.release())
    } else {
      permissions = ['members.read']
      await act(async () => permissionGate!.release())
    }
    await until(() => expect(host.textContent).toContain('target@example.invalid'))
    if (kind === 'permission') expect(document.querySelector('[role="dialog"]')).toBeNull()
    else await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
    expect(requests.some((r) => r.method === 'patch')).toBe(false)
  },
)
it('rejects a late old actor list after a real renewed Session and reads the new actor scope', async () => {
  await mount()
  listGate = controlledGate()
  await invalidate(['admin', 'members'])
  await until(() => expect(host.textContent).not.toContain('target@example.invalid'))
  actor = 'usr_peer'
  page = memberListPage(actor, [
    memberListRow({ name: 'Peer target', id: 'usr_peer_target', email: 'peer@example.invalid' }),
  ])
  await invalidate(['auth', 'session'])
  await until(() => expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2))
  await act(async () => listGate!.release())
  await until(() => expect(host.textContent).toContain('Peer target'))
  expect(host.textContent).not.toContain('target@example.invalid')
  expect(document.querySelector('[aria-label="Member actions for Target"]')).toBeNull()
})
it('blocks a late committed lifecycle result after filter invalidation from restoring old target facts', async () => {
  await mount()
  await menu()
  await item('Disable')
  await until(() => expect(document.querySelector('[role="dialog"] textarea')).not.toBeNull())
  postGate = controlledGate()
  await act(async () => {
    const field = document.querySelector('textarea')!
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
      field,
      'Controlled lifecycle change',
    )
    field.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await button('Confirm disable')
  expect(requests.filter((r) => r.method === 'patch')).toHaveLength(1)
  page = memberListPage('usr_admin', [
    memberListRow({ id: 'usr_new', name: 'New target', email: 'new@example.invalid' }),
  ])
  await act(async () => {
    const input = host.querySelector<HTMLInputElement>('[name="q"]')!
    input.value = 'New'
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
  await until(() => expect(host.textContent).toContain('new@example.invalid'))
  await act(async () => postGate!.release())
  expect(host.textContent).not.toContain('target@example.invalid')
  expect(cache.getQueryData(['admin', 'member', 'usr_admin', 'usr_target', 1])).toBeUndefined()
})
it('validates pagination chain and hides old pages during next-page renewal', async () => {
  page.next_cursor = page.items[0].id
  await mount()
  page = memberListPage('usr_admin', [
    memberListRow({ id: 'usr_z', name: 'Next target', email: 'next@example.invalid' }),
  ])
  listGate = controlledGate()
  await button('Load more members')
  await until(() => expect(host.textContent).not.toContain('target@example.invalid'))
  await act(async () => listGate!.release())
  await until(() => expect(host.textContent).toContain('next@example.invalid'))
  expect(host.textContent).toContain('target@example.invalid')
  expect(requests.filter((r) => r.url === '/admin/members').at(-1)?.params.cursor).toBe(
    'usr_target',
  )
})
it('does not offer ordinary reenable for an offboarded target', async () => {
  page.items[0].registration_approval.admission_eligible = false
  page.items[0].offboarded_at = page.items[0].updated_at
  page.items[0].disabled = true
  await mount()
  await menu()
  expect(document.querySelector('[role="menu"]')?.textContent).not.toContain('Enable')
  expect(document.querySelector('[role="menu"]')?.textContent).toContain('Reactivate')
  expect(document.querySelector('[role="menu"]')?.textContent).not.toContain('Review offboarding')
})

it.each(['Cancel', 'Escape'] as const)(
  'returns list status %s focus to the exact current row menu trigger without dispatch',
  async (close) => {
    await mount()
    const trigger = document.querySelector<HTMLButtonElement>(
      '[aria-label="Member actions for Target"]',
    )!
    await act(async () => trigger.focus())
    await menu()
    await item('Disable')
    await until(() => expect(document.querySelector('[role="dialog"] textarea')).not.toBeNull())
    await act(async () =>
      document.querySelector<HTMLTextAreaElement>('[role="dialog"] textarea')!.focus(),
    )
    expect(document.querySelector('[role="dialog"]')!.contains(document.activeElement)).toBe(true)
    if (close === 'Cancel') await button('Cancel')
    else
      await act(async () =>
        document
          .querySelector('[role="dialog"]')!
          .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
      )
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await until(() => expect(document.activeElement).toBe(trigger))
    expect(trigger.isConnected).toBe(true)
    expect(requests.some((request) => request.method === 'patch')).toBe(false)
  },
)

it.each(['permission', 'actor', 'filter'] as const)(
  'does not restore list status focus to the original trigger after %s scope loss',
  async (change) => {
    await mount()
    const trigger = document.querySelector<HTMLButtonElement>(
      '[aria-label="Member actions for Target"]',
    )!
    await menu()
    await item('Disable')
    await until(() => expect(document.querySelector('[role="dialog"] textarea')).not.toBeNull())
    await act(async () =>
      document.querySelector<HTMLTextAreaElement>('[role="dialog"] textarea')!.focus(),
    )
    const cancel = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
      (element) => element.textContent === 'Cancel',
    )!
    const focus = vi.spyOn(trigger, 'focus')
    await act(async () => {
      if (change === 'permission') {
        permissions = ['members.read', 'teams.read_all', 'calls.read_all']
        cache.setQueryData(['permissions', actor], permissions)
      } else if (change === 'actor') {
        actor = 'usr_other_admin'
        page = memberListPage(actor)
        cache.setQueryData(['auth', 'session'], {
          user: {
            id: actor,
            name: 'Other administrator',
            email: 'other@example.invalid',
            role: 'admin',
          },
          csrf_token: 'controlled-next-csrf',
        })
      } else {
        page = memberListPage(actor, [
          memberListRow({ id: 'usr_filtered', name: 'Filtered target' }),
        ])
        host.querySelector<HTMLInputElement>('[name="q"]')!.value = 'Filtered'
        host
          .querySelector('form')!
          .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      }
      cancel.click()
    })
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    if (change === 'permission') await until(() => expect(trigger.isConnected).toBe(true))
    else await until(() => expect(trigger.isConnected).toBe(false))
    expect(focus).not.toHaveBeenCalled()
    expect(document.activeElement).not.toBe(trigger)
    expect(requests.some((request) => request.method === 'patch')).toBe(false)
  },
)

it('renews list authority for two same-millisecond successful Session reads, never reusing timestamp identity', async () => {
  const fixed = Date.now()
  vi.spyOn(Date, 'now').mockReturnValue(fixed)
  await mount()
  const old = cache.getQueryState(['auth', 'session'])!.dataUpdatedAt
  for (let n = 2; n <= 3; n++) {
    await invalidate(['auth', 'session'])
    await until(() => expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(n))
    await until(() => expect(host.textContent).toContain('target@example.invalid'))
    expect(cache.getQueryState(['auth', 'session'])!.dataUpdatedAt).toBe(old)
    expect(
      cache
        .getQueryCache()
        .findAll({ queryKey: ['admin', 'members'] })
        .some((q) => q.queryKey[3] === n),
    ).toBe(true)
  }
  expect(requests.filter((r) => r.url === '/admin/members')).toHaveLength(3)
})
it('discards an obsolete filtered response and never restores its row menu or selection', async () => {
  await mount()
  listGate = controlledGate()
  await invalidate(['admin', 'members'])
  await until(() => expect(host.textContent).not.toContain('target@example.invalid'))
  page = memberListPage('usr_admin', [
    memberListRow({
      id: 'usr_filtered',
      name: 'Filtered target',
      email: 'filtered@example.invalid',
    }),
  ])
  await act(async () => {
    host.querySelector<HTMLInputElement>('[name="q"]')!.value = 'Filtered'
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
  await until(() => expect(requests.filter((r) => r.url === '/admin/members')).toHaveLength(3))
  await act(async () => listGate!.release())
  await until(() => expect(host.textContent).toContain('filtered@example.invalid'))
  expect(host.textContent).not.toContain('target@example.invalid')
  expect(document.querySelector('[aria-label="Member actions for Target"]')).toBeNull()
  expect(requests.some((r) => r.method === 'patch')).toBe(false)
})
it('removes all list facts when a fresh permission read revokes members.read', async () => {
  await mount()
  permissions = []
  permissionGate = controlledGate()
  await invalidate(['permissions'])
  await until(() => expect(host.textContent).not.toContain('target@example.invalid'))
  await act(async () => permissionGate!.release())
  await until(() => expect(host.textContent).toContain('Access denied'))
  expect(host.querySelector('table')).toBeNull()
  expect(document.querySelector('[role="menu"]')).toBeNull()
})

it('renders recorded login in the existing cell with live selected-language dates and no extra reads', async () => {
  const stamp = '2026-10-03T08:19:00.123456Z'
  page.items[0] = { ...page.items[0], last_login_status: 'recorded', last_login_at: stamp }
  await mount()
  const formatted = (locale: string) =>
    new Intl.DateTimeFormat(locale, {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date(stamp))
  expect(host.querySelector('tbody tr')?.children[7].textContent).toBe(formatted('en-US'))
  const count = requests.length
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.querySelector('tbody tr')?.children[7].textContent).toBe(formatted('zh-CN'))
  expect(requests).toHaveLength(count)
  expect(requests.some((r) => r.url?.includes('sessions') || r.url?.includes('overview'))).toBe(
    false,
  )
})
it.each(['list', 'Session', 'permissions'])(
  'hides recorded login throughout %s renewal or failure',
  async (kind) => {
    const stamp = '2026-10-03T08:19:00Z'
    page.items[0] = { ...page.items[0], last_login_status: 'recorded', last_login_at: stamp }
    await mount()
    const text = new Intl.DateTimeFormat('en-US', {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date(stamp))
    expect(host.textContent).toContain(text)
    const hold = controlledGate()
    if (kind === 'Session') sessionGate = hold
    else if (kind === 'permissions') permissionGate = hold
    else listGate = hold
    const operation =
      kind === 'Session'
        ? ['auth', 'session']
        : kind === 'permissions'
          ? ['permissions']
          : ['admin', 'members']
    await invalidate(operation)
    await until(() => expect(host.textContent).not.toContain(text))
    if (kind === 'list') listError = 503
    else if (kind === 'Session') sessionError = 503
    else {
      // The pending permission read captured the old authority. The next renewed
      // read must still hide retained facts after this one finishes.
      permissions = []
    }
    await act(async () => hold.release())
    if (kind === 'permissions') await invalidate(['permissions'])
    await until(() => expect(cache.isFetching()).toBe(0))
    expect(host.textContent).not.toContain(text)
  },
)
it('never restores a recorded login from an obsolete filtered page', async () => {
  const stamp = '2026-10-03T08:19:00Z'
  page.items[0] = { ...page.items[0], last_login_status: 'recorded', last_login_at: stamp }
  await mount()
  const text = new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(stamp),
  )
  listGate = controlledGate()
  await invalidate(['admin', 'members'])
  await until(() => expect(host.textContent).not.toContain(text))
  page = memberListPage('usr_admin', [
    memberListRow({
      id: 'usr_filtered',
      name: 'Filtered target',
      email: 'filtered@example.invalid',
    }),
  ])
  await act(async () => {
    host.querySelector<HTMLInputElement>('[name="q"]')!.value = 'Filtered'
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
  await until(() => expect(requests.filter((r) => r.url === '/admin/members')).toHaveLength(3))
  await act(async () => listGate!.release())
  await until(() => expect(host.textContent).toContain('filtered@example.invalid'))
  expect(host.textContent).not.toContain(text)
  expect(document.querySelector('[aria-label="Member actions for Target"]')).toBeNull()
})

it('renders retained names as escaped text and gives an empty historical name an accessible fallback', async () => {
  page.items[0].name = '<img src=x onerror=alert(1)>\nretained'
  await mount()
  const link = host.querySelector<HTMLAnchorElement>('a[href="/admin/members/usr_target"]')!
  expect(link.textContent).toContain(page.items[0].name)
  expect(link.querySelector('img')).toBeNull()
  page.items[0].name = ''
  await invalidate(['admin', 'members'])
  await until(() =>
    expect(host.querySelector('a[href="/admin/members/usr_target"]')?.textContent).toContain(
      'target@example.invalid',
    ),
  )
  expect(
    host.querySelector('[aria-label="Member actions for target@example.invalid"]'),
  ).not.toBeNull()
})

it.each(['Cancel', 'Escape', 'Actor replacement'])(
  'retains exact uncertain approval through %s dismissal and same row reopening',
  async (close) => {
    permissions.push('members.approvals.write')
    page.items[0].registration_approval = {
      status: 'pending',
      admission_eligible: false,
    }
    await mount()
    await menu()
    await item('Review registration')
    await until(() => expect(document.querySelector('textarea')).not.toBeNull())
    await act(async () => {
      const input = document.querySelector<HTMLTextAreaElement>('textarea')!
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
        input,
        'Retain exact approval',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await button('Approve application')
    await button('Confirm')
    await until(() => expect(document.body.textContent).toContain('unconfirmed'))
    const writes = () =>
      requests.filter((r) => r.method === 'patch' && r.url?.endsWith('/approval'))
    const original = writes()[0]
    if (close !== 'Escape') await button('Cancel')
    else
      await act(async () =>
        document
          .querySelector('[role="dialog"]')!
          .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
      )
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    if (close === 'Actor replacement') {
      actor = 'usr_replacement'
      page.actor_user_id = actor
      await act(async () =>
        cache.setQueryData(['auth', 'session'], {
          user: { id: actor, name: 'New actor', email: 'new@example.invalid', role: 'admin' },
          csrf_token: 'new-actor-csrf',
        }),
      )
      await until(() =>
        expect(document.querySelector('[aria-label="Member actions for Target"]')).not.toBeNull(),
      )
      await menu()
      await item('Review registration')
      await until(() => expect(document.querySelector('textarea')).not.toBeNull())
      expect(document.querySelector('textarea')!.value).toBe('')
      expect(document.body.textContent).not.toContain('unconfirmed')
      expect(writes()).toHaveLength(1)
      return
    }
    expect(writes()).toHaveLength(1)
    await menu()
    await item('Review registration')
    await until(() => expect(document.body.textContent).toContain('unconfirmed'))
    expect(JSON.parse(original.data).reason).toBe('Retain exact approval')
    expect(document.querySelector('textarea')).toBeNull()
    expect(writes()).toHaveLength(1)
    await button('Retry exact submitted request')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].data).toBe(original.data)
    expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  },
)
