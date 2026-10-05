import { memberListPage, memberListRow } from './member-list.fixture'
import { effectiveModelsPage } from './member-effective-models.fixture'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import MembersPage from './members'
import type { MemberOverviewRecord } from '@/types/member-overview'
import { limitFixture } from '@/views/resource-limits/fixture'
import type { Session } from '@/types/auth'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
export function overviewFixture(id = 'usr_target'): MemberOverviewRecord {
  return {
    user_id: id,
    observed_at: '2026-10-04T10:00:00Z',
    platform_currency: 'USD',
    total_personal_keys: '900719925474099312345',
    personal: {
      account_id: 'user:' + id,
      policy_etag: '0',
      tokens_month: '0',
      money_month: '0.123456789012345678',
      currency: 'USD',
      runtime_applied: false,
      usage_status: 'active',
      usage: {
        as_of: '2026-10-04T09:59:00Z',
        time_zone: 'UTC',
        month_start: '2026-10-01T00:00:00Z',
        month_end: '2026-11-01T00:00:00Z',
        covered: false,
        tokens_used: '9007199254740993',
        tokens_held: '2',
        tokens_unknown: '3',
        money_used: { USD: '0.123456789012345678', EUR: '0' },
        money_held: { USD: '0.000000000000000001' },
        money_unknown: '4',
      },
      active_reservations: { tokens_held: '7', money_held: { USD: '0.000000000000000002' } },
    },
  }
}
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let data: MemberOverviewRecord,
  session: Session,
  permissions: string[],
  requests: InternalAxiosRequestConfig[]
let intercept: ((config: InternalAxiosRequestConfig) => Promise<unknown> | undefined) | undefined
let failure: number
let recentLogin: string | null
const original = client.defaults.adapter
beforeEach(async () => {
  data = overviewFixture()
  failure = 0
  recentLogin = null
  intercept = undefined
  requests = []
  permissions = ['members.read']
  session = {
    user: { id: 'usr_reader', name: 'Reader', email: 'reader@example.invalid', role: 'member' },
    csrf_token: 'current-csrf',
  }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  await i18n.changeLanguage('en')
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const deferred = intercept?.(config)
    let value: unknown
    if (deferred) value = await deferred
    else if (config.url === '/auth/session') value = structuredClone(session)
    else if (config.url === '/auth/permissions') value = { permissions }
    else if (config.url?.endsWith('/limits'))
      value = { ...limitFixture(), id: config.url.split('/')[3] }
    else if (config.url === '/admin/members')
      value = memberListPage(session.user.id, [memberListRow()])
    else if (config.url === '/admin/roles')
      value = {
        items: [{ id: 'rol_custom', name: 'Custom', builtin: false, permissions: [] }],
        available_permissions: [],
      }
    else if (config.url?.endsWith('/effective-models'))
      return {
        config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders({ 'cache-control': 'private, no-store' }),
        data: effectiveModelsPage(config.url.split('/')[3], {
          teams: permissions.includes('teams.read_all'),
          providers: permissions.includes('providers.read'),
          prices: permissions.includes('prices.read'),
        }),
      }
    else if (config.url?.endsWith('/overview')) {
      if (failure)
        throw new AxiosError('Denied', '', config, undefined, {
          config,
          status: failure,
          statusText: '',
          headers: new AxiosHeaders(),
          data: {},
        })
      value = structuredClone({ ...data, user_id: config.url.split('/')[3] })
    } else if (config.url?.startsWith('/admin/members/'))
      value = {
        id: config.url.split('/')[3],
        name: config.url.endsWith('usr_other') ? 'Other' : 'Target',
        email: 'target@example.invalid',
        role: 'member',
        role_ids: [],
        offboarded_at: null,
        last_login_at: recentLogin,
        last_login_status: recentLogin ? 'recorded' : 'historical_unavailable',
        disabled: false,
        created_at: '2026-09-23T00:00:00Z',
      }
    else throw new Error('Unexpected endpoint ' + config.url)
    return { config, status: 200, statusText: '', headers: new AxiosHeaders(), data: value }
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
})
async function mount() {
  router = createMemoryRouter([{ path: '/admin/members/:memberId', element: <MembersPage /> }], {
    initialEntries: ['/admin/members/usr_target'],
  })
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => host.textContent!.includes('9,007,199,254,740,993'))
}
async function until(predicate: () => boolean) {
  for (let i = 0; i < 100; i++) {
    if (predicate()) return
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
  }
  expect(predicate()).toBe(true)
}
const overviewReads = () => requests.filter((r) => r.url?.endsWith('/overview')).length
function deferred() {
  let resolve!: (value: unknown) => void
  const promise = new Promise<unknown>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

describe('administrative Member Overview', () => {
  it('shows three exact read-only cards, separate holds and conservative recorded coverage', async () => {
    await mount()
    expect(host.textContent).toContain('9,007,199,254,740,993 / 0')
    expect(host.textContent).toContain('0.123456789012345678 USD')
    expect(host.textContent).toContain('900,719,925,474,099,312,345')
    expect(host.textContent).toContain('Monthly retained holds: 2')
    expect(host.textContent).toContain('Live reserved Tokens: 7')
    expect(host.textContent).toContain('Unknown token records: 3')
    expect(host.textContent).toContain('Unknown amount records: 4')
    expect(host.textContent).toContain('known subtotal')
    expect(host.textContent).toContain('not confirmed')
    expect(host.textContent).toContain('not an active Key count')
    expect(host.textContent).toContain('Access status')
    expect(
      requests.every((r) =>
        [
          '/auth/session',
          '/auth/permissions',
          '/admin/members/usr_target',
          '/admin/members/usr_target/overview',
          '/admin/members/usr_target/effective-models',
        ].includes(r.url!),
      ),
    ).toBe(true)
    expect(host.querySelector('progress')).toBeNull()
    expect(host.querySelector('a[href*=keys]')).toBeNull()
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain('个人预算（已用 / 额度）')
    expect(host.textContent).toContain('月度覆盖不完整')
    expect(host.textContent).toContain('0.123456789012345678 USD')
  })
  it('preserves unknown inactive usage and null limits without converting either to zero', async () => {
    data.personal = {
      ...data.personal,
      tokens_month: null,
      money_month: null,
      currency: null,
      usage_status: 'inactive',
      usage: null,
      active_reservations: null,
    }
    router = createMemoryRouter([{ path: '/admin/members/:memberId', element: <MembersPage /> }], {
      initialEntries: ['/admin/members/usr_target'],
    })
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await until(() => host.textContent!.includes('Monthly usage is inactive'))
    expect(host.textContent).toContain('Unknown / Not set')
    expect(host.textContent).not.toContain('Monthly retained holds: 0')
  })
  it('hides cards on structurally identical same-millisecond network Session success until exact rereads finish', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(1791100000000)
    await mount()
    const baseline = overviewReads(),
      detailBaseline = requests.filter((r) => r.url === '/admin/members/usr_target').length
    const pending = deferred()
    intercept = (config) => (config.url?.endsWith('/overview') ? pending.promise : undefined)
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true })
    })
    await until(() => overviewReads() > baseline)
    expect(host.textContent).not.toContain('900,719,925,474,099,312,345')
    expect(requests.filter((r) => r.url === '/admin/members/usr_target').length).toBeGreaterThan(
      detailBaseline,
    )
    pending.resolve(overviewFixture())
    await until(() => host.textContent!.includes('900,719,925,474,099,312,345'))
  })
  it('manual same-actor CSRF cache replacement keeps cards without another network read', async () => {
    await mount()
    const baseline = overviewReads(),
      sessions = requests.filter((r) => r.url === '/auth/session').length
    await act(async () => {
      cache.setQueryData(['auth', 'session'], { ...session, csrf_token: 'rotated-current' })
    })
    expect(overviewReads()).toBe(baseline)
    expect(requests.filter((r) => r.url === '/auth/session').length).toBe(sessions)
    expect(host.textContent).toContain('900,719,925,474,099,312,345')
  })
  it('aborts and ignores late old-subject cards after a target change', async () => {
    await mount()
    const pending = deferred()
    let signal: AbortSignal | undefined
    intercept = (config) =>
      config.url === '/admin/members/usr_target/overview'
        ? ((signal = config.signal as AbortSignal), pending.promise)
        : undefined
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['admin', 'member-overview'] })
    })
    await until(() => !!signal)
    data = overviewFixture('usr_other')
    data.total_personal_keys = '42'
    await act(async () => {
      await router.navigate('/admin/members/usr_other')
    })
    await until(() => host.textContent!.includes('Other') && host.textContent!.includes('42'))
    expect(signal!.aborted).toBe(true)
    pending.resolve(overviewFixture())
    await act(async () => {
      await Promise.resolve()
    })
    expect(host.textContent).not.toContain('900,719,925,474,099,312,345')
  })
  it('hides cards throughout Session renewal and ignores old actor data', async () => {
    await mount()
    const pending = deferred()
    intercept = (config) => (config.url === '/auth/session' ? pending.promise : undefined)
    await act(async () => {
      void cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true })
    })
    await until(() => !host.textContent!.includes('900,719,925,474,099,312,345'))
    session = { ...session, user: { ...session.user, id: 'usr_second' } }
    data.total_personal_keys = '21'
    pending.resolve(session)
    await until(() => host.textContent!.includes('21'))
    expect(host.textContent).not.toContain('900,719,925,474,099,312,345')
  })
  it('cancels an old-actor overview response and cannot restore it after new actor authority', async () => {
    await mount()
    const pending = deferred()
    let signal: AbortSignal | undefined
    intercept = (config) =>
      config.url?.endsWith('/overview') &&
      cache.getQueryData<Session>(['auth', 'session'])?.user.id === 'usr_reader'
        ? ((signal = config.signal as AbortSignal), pending.promise)
        : undefined
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['admin', 'member-overview'] })
    })
    await until(() => !!signal)
    session = { ...session, user: { ...session.user, id: 'usr_second' } }
    data.total_personal_keys = '19'
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true })
    })
    await until(
      () => host.textContent!.includes('Personal API Keys') && host.textContent!.includes('19'),
    )
    expect(signal!.aborted).toBe(true)
    pending.resolve(overviewFixture())
    await act(async () => {
      await Promise.resolve()
    })
    expect(host.textContent).not.toContain('900,719,925,474,099,312,345')
  })
  it('removes previously readable cards after exact endpoint denies renewed authority', async () => {
    await mount()
    failure = 403
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true })
    })
    await until(() => host.querySelector('[role=alert]') !== null)
    expect(host.textContent).not.toContain('900,719,925,474,099,312,345')
    expect(host.textContent).not.toContain('0.123456789012345678 USD')
  })
  it('retains unsent base-role and role-assignment drafts through permission error/renewal, clears on actor or target switch', async () => {
    session.user.role = 'admin'
    permissions = ['members.read', 'members.write', 'roles.read']
    await mount()
    await act(async () => {
      await router.navigate('/admin/members/usr_target?tab=settings')
    })
    await until(() => !!host.querySelector('select[name="role"]'))
    await act(async () => {
      const select = host.querySelector<HTMLSelectElement>('select[name="role"]')!
      select.value = 'admin'
      select.dispatchEvent(new Event('change', { bubbles: true }))
    })
    const denied = async (config: InternalAxiosRequestConfig) => {
      throw new AxiosError('Denied', '', config, undefined, {
        config,
        status: 403,
        statusText: '',
        headers: new AxiosHeaders(),
        data: {},
      })
    }
    intercept = (config) => (config.url === '/auth/permissions' ? denied(config) : undefined)
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['permissions'] })
    })
    await until(() => !host.querySelector('select[name="role"]'))
    expect(host.querySelector('select[name="role"]')).toBeNull()
    expect(host.textContent).not.toContain('target@example.invalid')
    intercept = undefined
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['permissions'] })
    })
    await until(() => !!host.querySelector('select[name="role"]'))
    expect(host.querySelector<HTMLSelectElement>('select[name="role"]')!.value).toBe('admin')
    await act(async () => {
      await router.navigate('/admin/members/usr_target?tab=roles')
    })
    await until(() => !!host.querySelector('input[value="rol_custom"]'))
    await act(async () => {
      host.querySelector<HTMLInputElement>('input[value="rol_custom"]')!.click()
    })
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true })
    })
    await until(() => !!host.querySelector('input[value="rol_custom"]'))
    expect(host.querySelector<HTMLInputElement>('input[value="rol_custom"]')!.checked).toBe(true)
    await act(async () => {
      await router.navigate('/admin/members/usr_other?tab=settings')
    })
    await until(() => !!host.querySelector('select[name="role"]'))
    expect(host.querySelector<HTMLSelectElement>('select[name="role"]')!.value).toBe('member')
    await act(async () => {
      await router.navigate('/admin/members/usr_target?tab=roles')
    })
    await until(() => !!host.querySelector('input[value="rol_custom"]'))
    expect(host.querySelector<HTMLInputElement>('input[value="rol_custom"]')!.checked).toBe(false)
    await act(async () => {
      host.querySelector<HTMLInputElement>('input[value="rol_custom"]')!.click()
    })
    session = { ...session, user: { ...session.user, id: 'usr_new' } }
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true })
    })
    await until(() => !!host.querySelector('input[value="rol_custom"]'))
    expect(host.querySelector<HTMLInputElement>('input[value="rol_custom"]')!.checked).toBe(false)
  })
  it('preserves ordinary same-actor metadata draft through a failed Session read without showing cached private facts', async () => {
    session.user.role = 'admin'
    permissions = ['members.read', 'members.write']
    await mount()
    await act(async () => {
      await router.navigate('/admin/members/usr_target?tab=settings')
    })
    await until(() => !!host.querySelector('select[name="role"]'))
    await act(async () => {
      const select = host.querySelector<HTMLSelectElement>('select[name="role"]')!
      select.value = 'admin'
      select.dispatchEvent(new Event('change', { bubbles: true }))
    })
    intercept = (config) =>
      config.url === '/auth/session'
        ? Promise.reject(
            new AxiosError('Unavailable', '', config, undefined, {
              config,
              status: 503,
              statusText: '',
              headers: new AxiosHeaders(),
              data: {},
            }),
          )
        : undefined
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true })
    })
    await until(() => !host.querySelector('select[name="role"]'))
    expect(host.textContent).not.toContain('target@example.invalid')
    expect(host.querySelector('select[name="role"]')).toBeNull()
    intercept = undefined
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true })
    })
    await until(() => !!host.querySelector('select[name="role"]'))
    expect(host.querySelector<HTMLSelectElement>('select[name="role"]')!.value).toBe('admin')
  })
  it('opening the member quota editor adds no Session network observer or renewed read', async () => {
    permissions = ['members.read', 'limits.users.write']
    await mount()
    await act(async () => {
      await router.navigate('/admin/members/usr_target?tab=limits')
    })
    await until(() =>
      [...host.querySelectorAll('button')].some((button) => button.textContent === 'Edit limits'),
    )
    const sessions = requests.filter((r) => r.url === '/auth/session').length
    await act(async () => {
      ;[...host.querySelectorAll('button')]
        .find((button) => button.textContent === 'Edit limits')!
        .click()
    })
    expect(host.querySelector('form[aria-label="Proposed policy"]')).not.toBeNull()
    expect(requests.filter((r) => r.url === '/auth/session').length).toBe(sessions)
  })
  it('hides cached directory rows, dialogs and actions during renewal and permission errors', async () => {
    permissions = ['members.read', 'members.write']
    router = createMemoryRouter([{ path: '/admin/members', element: <MembersPage /> }], {
      initialEntries: ['/admin/members'],
    })
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await until(() => host.textContent!.includes('target@example.invalid'))
    await act(async () => {
      ;[...host.querySelectorAll('button')]
        .find((button) => button.textContent === 'Create member')!
        .click()
    })
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    const pending = deferred()
    intercept = (config) => (config.url === '/auth/session' ? pending.promise : undefined)
    await act(async () => {
      void cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true })
    })
    await until(() => !host.textContent!.includes('target@example.invalid'))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(host.querySelector('table')).toBeNull()
    permissions = []
    intercept = undefined
    pending.resolve(session)
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['permissions'] })
    })
    await until(() => host.querySelector('[role="alert"]') !== null)
    expect(host.textContent).not.toContain('target@example.invalid')
    expect(host.querySelector('table')).toBeNull()
  })
  it('uses current CSRF, suppresses duplicate writes and ignores late old-target mutation completion', async () => {
    session.user.role = 'admin'
    permissions = ['members.read', 'members.write']
    await mount()
    await act(async () => {
      await router.navigate('/admin/members/usr_target?tab=settings')
    })
    await until(() => !!host.querySelector('form[aria-label="Base role"]'))
    const pending = deferred()
    intercept = (config) => (config.method === 'patch' ? pending.promise : undefined)
    await act(async () => {
      cache.setQueryData(['auth', 'session'], { ...session, csrf_token: 'latest-csrf' })
    })
    await act(async () => {
      const form = host.querySelector('form[aria-label="Base role"]')!
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    await until(() => requests.some((r) => r.method === 'patch'))
    const writes = requests.filter((r) => r.method === 'patch')
    expect(writes).toHaveLength(1)
    expect(writes[0].headers.get('X-CSRF-Token')).toBe('latest-csrf')
    await act(async () => {
      await router.navigate('/admin/members/usr_other')
    })
    await until(() => host.textContent!.includes('Other'))
    pending.resolve({
      id: 'usr_target',
      name: 'Late private name',
      email: 'old@example.invalid',
      role: 'admin',
      role_ids: [],
      disabled: false,
      created_at: '2026-09-23T00:00:00Z',
    })
    await act(async () => {
      await Promise.resolve()
    })
    expect(host.textContent).not.toContain('Late private name')
    expect(router.state.location.pathname).toBe('/admin/members/usr_other')
    expect(
      cache
        .getQueriesData({ queryKey: ['admin', 'member', 'usr_reader', 'usr_target'] })
        .every(
          ([, value]) =>
            !(
              value &&
              typeof value === 'object' &&
              'name' in value &&
              value.name === 'Late private name'
            ),
        ),
    ).toBe(true)
  })
  it('does not navigate or restore a late creation result after the actor changes', async () => {
    permissions = ['members.read', 'members.write']
    session.user.role = 'admin'
    router = createMemoryRouter(
      [
        { path: '/admin/members', element: <MembersPage /> },
        { path: '/admin/members/:memberId', element: <MembersPage /> },
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
    await until(() => host.textContent!.includes('target@example.invalid'))
    await act(async () => {
      ;[...host.querySelectorAll('button')]
        .find((button) => button.textContent === 'Create member')!
        .click()
    })
    await until(() => !!document.querySelector('[role="dialog"] form'))
    const pending = deferred()
    intercept = (config) => (config.method === 'post' ? pending.promise : undefined)
    await act(async () => {
      const form = document.querySelector<HTMLFormElement>('[role="dialog"] form')!
      for (const [name, value] of [
        ['name', 'Created'],
        ['email', 'created@example.invalid'],
        ['password', 'dummy-password-123'],
      ]) {
        const input = form.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
          input,
          value,
        )
        input.dispatchEvent(new Event('input', { bubbles: true }))
      }
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    await until(() => requests.some((r) => r.method === 'post'))
    session = { ...session, user: { ...session.user, id: 'usr_second' } }
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true })
    })
    await until(() => host.textContent!.includes('target@example.invalid'))
    pending.resolve({
      id: 'usr_created',
      name: 'Late created private',
      email: 'created@example.invalid',
      role: 'member',
      role_ids: [],
      disabled: false,
      created_at: '2026-10-04T00:00:00Z',
    })
    await act(async () => {
      await Promise.resolve()
    })
    expect(router.state.location.pathname).toBe('/admin/members')
    expect(host.textContent).not.toContain('Late created private')
    expect(
      cache.getQueriesData({ queryKey: ['admin', 'member', 'usr_reader', 'usr_created'] }),
    ).toHaveLength(0)
  })
  it('a limits writer or platform role alone cannot fetch member cards', async () => {
    permissions = ['limits.users.write']
    session.user.role = 'admin'
    router = createMemoryRouter([{ path: '/admin/members/:memberId', element: <MembersPage /> }], {
      initialEntries: ['/admin/members/usr_target'],
    })
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await until(() => host.querySelector('[role=alert]') !== null)
    expect(overviewReads()).toBe(0)
  })
})

it('preserves the Overview cards then access information then effective-model table order', async () => {
  await mount()
  await until(() => host.textContent!.includes('controlled-model'))
  const headings = [...host.querySelectorAll('h3')].map((element) => element.textContent)
  expect(headings.indexOf('Access status')).toBeGreaterThan(2)
  expect(headings.indexOf('Effective models')).toBe(headings.indexOf('Access status') + 1)
  expect(host.querySelector('table[aria-label="Member effective models"]')).not.toBeNull()
  expect(host.textContent).toContain('complete authorization union is unknown')
})

it('uses GET-only recorded login in Access status and keeps the effective table after it in both locales', async () => {
  recentLogin = '2026-10-03T08:19:00.123456Z'
  await mount()
  const expected = (locale: string) =>
    new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(
      new Date(recentLogin!),
    )
  expect(host.textContent).toContain(expected('en-US'))
  const count = requests.length
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('最近登录')
  expect(host.textContent).toContain(expected('zh-CN'))
  expect(requests).toHaveLength(count)
  expect(requests.every((r) => r.method === 'get')).toBe(true)
})
it('leaves historical login explicitly unavailable without inferring it from creation or Session', async () => {
  await mount()
  expect(host.textContent).toContain('Historical login time unavailable')
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('历史登录时间不可用')
})
it.each(['detail', 'Session'])(
  'hides recorded login through %s renewed authority and ignores a late old detail',
  async (kind) => {
    recentLogin = '2026-10-03T08:19:00Z'
    await mount()
    const text = new Intl.DateTimeFormat('en-US', {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date(recentLogin))
    const pending = deferred()
    let captured: AbortSignal | undefined
    intercept = (config) =>
      config.url === '/admin/members/usr_target'
        ? ((captured = config.signal as AbortSignal), pending.promise)
        : undefined
    await act(async () => {
      void cache.refetchQueries({
        queryKey:
          kind === 'Session'
            ? ['auth', 'session']
            : kind === 'permission'
              ? ['permissions']
              : ['admin', 'member'],
      })
    })
    await until(() => !!captured && !host.textContent?.includes(text))
    data = overviewFixture('usr_other')
    recentLogin = null
    await act(async () => router.navigate('/admin/members/usr_other'))
    await until(() => host.textContent?.includes('Other') === true)
    expect(captured?.aborted).toBe(true)
    pending.resolve({
      id: 'usr_target',
      name: 'Old',
      email: 'old@example.invalid',
      role: 'member',
      disabled: false,
      offboarded_at: null,
      created_at: '2026-09-23T00:00:00Z',
      role_ids: [],
      last_login_at: '2026-10-03T08:19:00Z',
      last_login_status: 'recorded',
    })
    await act(async () => await Promise.resolve())
    expect(host.textContent).not.toContain(text)
    expect(host.textContent).not.toContain('old@example.invalid')
  },
)

it('hides recorded login on target invalidation even when automatic refetch is suppressed', async () => {
  recentLogin = '2026-10-03T08:19:00Z'
  await mount()
  const text = new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(recentLogin),
  )
  expect(host.textContent).toContain(text)
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'member'], refetchType: 'none' })
  })
  expect(host.textContent).not.toContain(text)
})

it.each(['Session', 'permission'])(
  'hides recorded login synchronously on %s invalidation without an automatic read',
  async (kind) => {
    recentLogin = '2026-10-03T08:19:00Z'
    await mount()
    const text = new Intl.DateTimeFormat('en-US', {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date(recentLogin))
    expect(host.textContent).toContain(text)
    await act(async () => {
      await cache.invalidateQueries({
        queryKey: kind === 'Session' ? ['auth', 'session'] : ['permissions'],
        refetchType: 'none',
      })
    })
    expect(host.textContent).not.toContain(text)
    if (kind === 'permission') permissions = []
    await act(async () => {
      await cache.refetchQueries({
        queryKey: kind === 'Session' ? ['auth', 'session'] : ['permissions'],
      })
    })
    if (kind === 'permission') expect(host.textContent).not.toContain(text)
    else await until(() => host.textContent?.includes(text) === true)
  },
)
it('discards a recorded login when renewed detail fails strict decoding and preserves historical unknown on recovery', async () => {
  recentLogin = '2026-10-03T08:19:00Z'
  await mount()
  const text = new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(recentLogin),
  )
  intercept = (config) =>
    config.url === '/admin/members/usr_target'
      ? Promise.resolve({
          id: 'usr_other',
          last_login_at: recentLogin,
          last_login_status: 'recorded',
        })
      : undefined
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['admin', 'member'] })
  })
  await until(() => !host.textContent?.includes(text))
  expect(host.textContent).not.toContain('Historical login time unavailable')
  intercept = undefined
  recentLogin = null
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['admin', 'member'] })
  })
  await until(() => host.textContent?.includes('Historical login time unavailable') === true)
  expect(host.textContent).not.toContain(text)
})
it('does not reuse old recorded login through two same-millisecond successful Session generations', async () => {
  vi.spyOn(Date, 'now').mockReturnValue(1791100000000)
  recentLogin = '2026-10-03T08:19:00Z'
  await mount()
  const text = new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(recentLogin),
  )
  for (let i = 0; i < 2; i++) {
    const pending = deferred()
    let captured = false
    intercept = (config) =>
      config.url === '/admin/members/usr_target' ? ((captured = true), pending.promise) : undefined
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true })
    })
    await until(() => captured && !host.textContent?.includes(text))
    pending.resolve({
      id: 'usr_target',
      name: 'Target',
      email: 'target@example.invalid',
      role: 'member',
      disabled: false,
      offboarded_at: null,
      created_at: '2026-09-23T00:00:00Z',
      role_ids: [],
      last_login_at: '2026-10-03T08:19:00Z',
      last_login_status: 'recorded',
    })
    await until(() => host.textContent?.includes(text) === true)
  }
  expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(3)
})
