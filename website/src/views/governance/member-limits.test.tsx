import { accessSummaryFixture } from './member-access-summary.fixture'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import MembersPage from './members'
import type { LimitRecord } from '@/types/resource-limits'
import type { DefaultLimitResetContext } from '@/types/default-limits'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let actor: string,
  csrf: string,
  permissions: string[],
  record: LimitRecord,
  requests: InternalAxiosRequestConfig[]
let putStatus: number,
  sessionStatus: number,
  permissionStatus: number,
  policyStatus: number,
  resetStatus: number
type Gate = { promise: Promise<void>; release: () => void }
let subjectDisabled: boolean,
  enforced: boolean,
  sessionGate: Gate | undefined,
  putGate: Gate | undefined,
  gates: Gate[]
const originalAdapter = client.defaults.adapter
function deferred(): Gate {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  const gate = { promise, release }
  gates.push(gate)
  return gate
}
function fail(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('Controlled unavailable response', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: { message: 'Unavailable' },
  })
}
function resetContext(): DefaultLimitResetContext {
  return {
    kind: 'user',
    id: record.id,
    etag: 'c'.repeat(64),
    editable: true,
    applied_default_etag: null,
    limit: record,
    default_rule: {
      kind: 'user',
      etag: 'd'.repeat(64),
      rule_etag: 'e'.repeat(64),
      editable: true,
      platform_currency: 'USD',
      updated_at: '2026-10-04T00:00:00Z',
      policy: {
        tokens_5h: null,
        tokens_7d: null,
        tokens_month: 500,
        tpm: null,
        money_month: '10.123456789012345678',
        currency: 'USD',
        rpm: null,
        concurrency: null,
      },
    },
  }
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_actor'
  csrf = 'first-csrf'
  permissions = ['members.read', 'limits.users.write']
  putStatus = sessionStatus = permissionStatus = policyStatus = resetStatus = 0
  subjectDisabled = false
  enforced = true
  gates = []
  requests = []
  sessionGate = putGate = undefined
  const policy = {
    tokens_5h: null,
    tokens_7d: null,
    tokens_month: 0,
    tpm: null,
    money_month: '0.123456789012345678',
    currency: 'USD',
    rpm: null,
    concurrency: null,
    ip_mode: 'none' as const,
    ip_ranges: [],
  }
  record = {
    kind: 'user',
    id: 'usr_target',
    account_id: 'user_usr_target',
    etag: 'a'.repeat(64),
    platform_currency: 'USD',
    stored: policy,
    effective: policy,
    ip_policies: [policy],
    quota_usage: null,
    rpm_used: null,
    active: null,
    enforced: true,
  }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    if (config.method === 'get' && config.url?.endsWith('/access')) {
      const data = accessSummaryFixture(config.url.split('/')[3], {
        roles: permissions.includes('roles.read'),
        teams: permissions.includes('teams.read_all'),
      })
      if (data.roles.status === 'available') data.roles.items = []
      if (data.teams.status === 'available') data.teams.items = []
      return {
        config: config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders({ 'cache-control': 'private, no-store' }),
        data,
      }
    }
    let data: unknown
    if (config.url === '/auth/session') {
      if (sessionStatus) throw fail(config, sessionStatus)
      data = { user: { id: actor, name: 'Reader', role: 'admin' }, csrf_token: csrf }
      if (sessionGate) await sessionGate.promise
    } else if (config.url === '/auth/permissions') {
      if (permissionStatus) throw fail(config, permissionStatus)
      data = { permissions: [...permissions] }
    } else if (config.url?.endsWith('/limits/default-reset')) {
      const review = resetContext()
      if (config.method === 'post') {
        if (putGate) await putGate.promise
        if (resetStatus) throw fail(config, resetStatus)
        const policy = { ...review.default_rule.policy, ip_mode: 'none', ip_ranges: [] }
        record = {
          ...record,
          stored: policy,
          effective: policy,
          ip_policies: [policy],
        } as LimitRecord
        data = {
          kind: 'user',
          id: record.id,
          saved: true,
          default_reset_etag: review.etag,
          applied_default_etag: review.default_rule.rule_etag,
          runtime_applied: true,
          limit: record,
        }
      } else data = review
    } else if (config.url?.endsWith('/limits')) {
      if (config.method === 'put') {
        if (putGate) await putGate.promise
        if (putStatus) throw fail(config, putStatus)
        const policy = JSON.parse(config.data)
        delete policy.reason
        record = {
          ...record,
          stored: policy,
          effective: policy,
          ip_policies: [policy],
          etag: 'b'.repeat(64),
          enforced,
        }
      } else if (policyStatus) throw fail(config, policyStatus)
      data = { ...record, id: config.url.split('/')[3] }
    } else if (/^\/admin\/members\/[^/]+$/.test(config.url ?? ''))
      data = {
        id: config.url!.split('/')[3],
        name: 'Controlled target',
        email: 'target@example.invalid',
        role: 'member',
        role_ids: [],
        last_login_at: null,
        last_login_status: 'historical_unavailable',
        disabled: subjectDisabled,
        offboarded_at: null,
        created_at: '2026-10-04T00:00:00Z',
      }
    else throw new Error(`Unexpected endpoint ${config.url}`)
    return {
      config,
      data: structuredClone(data),
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
    }
  }
  router = createMemoryRouter([{ path: '/admin/members/:memberId', element: <MembersPage /> }], {
    initialEntries: ['/admin/members/usr_target?tab=limits'],
  })
})
afterEach(async () => {
  gates.forEach((g) => g.release())
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
})
async function until(assertion: () => void) {
  await vi.waitFor(
    async () => {
      await act(async () => {})
      assertion()
    },
    { timeout: 4000, interval: 10 },
  )
}
const button = (label: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent?.trim() === label,
  )
async function click(label: string) {
  await until(() => expect(button(label)).toBeTruthy())
  await act(async () => button(label)!.click())
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
}
async function render() {
  await mount()
  await until(() => expect(host.textContent).toContain('0.123456789012345678 USD'))
}
async function input(label: string, value: string) {
  const el = document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
  expect(el).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value)
    el.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function edit() {
  await click('Edit limits')
  await until(() => expect(button('Save limits')).toBeTruthy())
}
async function draft() {
  await input('Monthly token quota', '33')
  await input('Monthly budget', '10.000000000000000001')
  await input('Reason for change', 'Original reason')
}
const puts = () => requests.filter((r) => r.method === 'put')
const resets = () => requests.filter((r) => r.method === 'post')
async function uncertain() {
  await render()
  await edit()
  await draft()
  putStatus = 503
  await click('Save limits')
  await until(() => expect(button('Retry application')).toBeTruthy())
}

it('has an addressable Limits tab separate from Settings with exact zero/null/money and no implicit write', async () => {
  await mount()
  await until(() => expect(button('Budgets, quotas and limits')).toBeTruthy())
  await until(() => expect(host.textContent).toContain('0.123456789012345678 USD'))
  expect(host.textContent).toContain('Quota usage is unavailable')
  expect(host.textContent).toContain('Unrestricted')
  expect(puts()).toHaveLength(0)
  await act(async () => router.navigate('/admin/members/usr_target?tab=settings'))
  await until(() => expect(host.textContent).toContain('Basic information'))
  expect(host.textContent).not.toContain('Resource limits')
  await act(async () => router.navigate('/admin/members/usr_target?tab=limits'))
  await until(() => expect(button('Edit limits')).toBeTruthy())
})
it.each([{ grants: ['members.read'] }, { grants: ['members.read', 'members.write'] }])(
  'read authority $grants never gains policy write authority',
  async ({ grants }) => {
    permissions = grants
    await render()
    expect(button('Edit limits')).toBeUndefined()
    expect(button('Restore defaults')).toBeUndefined()
    expect(puts()).toHaveLength(0)
  },
)
it('genuine same-millisecond renewal preserves a distinct unsent draft without another Session observer', async () => {
  vi.spyOn(Date, 'now').mockReturnValue(1800000000000)
  await render()
  await edit()
  await draft()
  sessionGate = deferred()
  csrf = 'renewed'
  let pending!: Promise<void>
  await act(async () => {
    pending = cache.refetchQueries({ queryKey: ['auth', 'session'] })
  })
  await until(() => expect(document.querySelector('[aria-label="Reason for change"]')).toBeNull())
  sessionGate.release()
  await act(async () => pending)
  await until(() =>
    expect(
      document.querySelector<HTMLInputElement>('[aria-label="Reason for change"]')?.value,
    ).toBe('Original reason'),
  )
  expect(
    document.querySelector<HTMLInputElement>('[aria-label="Monthly token quota"]')!.value,
  ).toBe('33')
  expect(puts()).toHaveLength(0)
  expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2)
})
it.each(['session', 'permission', 'policy'])(
  '%s outage hides policy facts and preserves an immutable uncertain retry',
  async (kind) => {
    await uncertain()
    const first = puts()[0]
    if (kind === 'session') sessionStatus = 503
    if (kind === 'permission') permissionStatus = 503
    if (kind === 'policy') policyStatus = 503
    const queryKey =
      kind === 'session'
        ? ['auth', 'session']
        : kind === 'permission'
          ? ['permissions']
          : ['resource-limits']
    await act(async () => cache.refetchQueries({ queryKey }))
    await until(() => expect(button('Retry application')).toBeUndefined())
    expect(host.textContent).not.toContain('0.123456789012345678 USD')
    sessionStatus = permissionStatus = policyStatus = 0
    csrf = 'renewed'
    await act(async () => cache.refetchQueries({ queryKey: ['permissions'] }))
    await act(async () => cache.refetchQueries({ queryKey: ['auth', 'session'] }))
    await until(() => expect(button('Retry application')).toBeTruthy())
    putStatus = 409
    await click('Retry application')
    await until(() => expect(puts()).toHaveLength(2))
    expect(puts()[1].data).toBe(first.data)
    expect(puts()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
    expect(puts()[1].headers.get('X-CSRF-Token')).toBe('renewed')
    await until(() => expect(button('Retry application')?.disabled).toBe(false))
    expect(button('Reload current policy')).toBeUndefined()
  },
)
it('fresh currency conflict keeps exact input and requires explicit review before a new request', async () => {
  await render()
  await edit()
  await draft()
  record = { ...record, platform_currency: 'EUR', etag: 'f'.repeat(64) }
  await act(async () => cache.refetchQueries({ queryKey: ['resource-limits'] }))
  await until(() => expect(button('Save limits')?.disabled).toBe(true))
  expect(document.querySelector<HTMLInputElement>('[aria-label="Monthly budget"]')!.value).toBe(
    '10.000000000000000001',
  )
  await click('Reload current policy')
  await until(() => expect(button('Save limits')?.disabled).toBe(false))
  await click('Save limits')
  await until(() => expect(puts()).toHaveLength(1))
  expect(JSON.parse(puts()[0].data).currency).toBe('EUR')
})
it('renewal aborts an in-flight write and cannot accept its late success as confirmation', async () => {
  await render()
  await edit()
  await draft()
  putGate = deferred()
  await click('Save limits')
  await until(() => expect(puts()).toHaveLength(1))
  const first = puts()[0]
  sessionGate = deferred()
  let pending!: Promise<void>
  await act(async () => {
    pending = cache.refetchQueries({ queryKey: ['auth', 'session'] })
  })
  await until(() => expect(first.signal!.aborted).toBe(true))
  putGate.release()
  sessionGate.release()
  await act(async () => pending)
  await until(() => expect(button('Retry application')).toBeTruthy())
  expect(host.textContent).not.toContain('Limits saved and applied.')
  expect(puts()).toHaveLength(1)
})
it.each(['actor', 'target', 'tab'])(
  '%s change destroys the old local draft and uncertain intent',
  async (kind) => {
    await uncertain()
    if (kind === 'actor') {
      actor = 'usr_second'
      await act(async () => cache.refetchQueries({ queryKey: ['auth', 'session'] }))
    } else if (kind === 'target')
      await act(async () => router.navigate('/admin/members/usr_second?tab=limits'))
    else {
      await act(async () => router.navigate('/admin/members/usr_target?tab=settings'))
      await act(async () => router.navigate('/admin/members/usr_target?tab=limits'))
    }
    await until(() => expect(button('Edit limits')).toBeTruthy())
    expect(button('Retry application')).toBeUndefined()
    expect(puts()).toHaveLength(1)
  },
)
it('disabled subject stays readable without any direct edit or restore action', async () => {
  subjectDisabled = true
  await render()
  expect(button('Edit limits')).toBeUndefined()
  expect(button('Restore defaults')).toBeUndefined()
})
it('pending runtime application remains uncertain rather than reporting enforcement', async () => {
  await render()
  await edit()
  await draft()
  enforced = false
  await click('Save limits')
  await until(() => expect(button('Retry application')).toBeTruthy())
  expect(host.textContent).not.toContain('Limits saved and applied.')
})
it('live Chinese switch preserves unsent exact money and reason', async () => {
  await render()
  await edit()
  await draft()
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('预算、配额与限制')
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].some(
      (el) => el.value === 'Original reason',
    ),
  ).toBe(true)
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].some(
      (el) => el.value === '10.000000000000000001',
    ),
  ).toBe(true)
})
it('default restoration retains its original reviewed request across renewed authority and a rejected retry', async () => {
  await render()
  await click('Restore defaults')
  await until(() => expect(button('Confirm restoration')).toBeTruthy())
  await input('Reason', 'Original restore reason')
  resetStatus = 503
  await click('Confirm restoration')
  await until(() => expect(button('Retry original request')).toBeTruthy())
  const first = resets()[0]
  sessionGate = deferred()
  csrf = 'new-csrf'
  let pending!: Promise<void>
  await act(async () => {
    pending = cache.refetchQueries({ queryKey: ['auth', 'session'] })
  })
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  sessionGate.release()
  await act(async () => pending)
  await until(() => expect(button('Retry original request')).toBeTruthy())
  resetStatus = 409
  await click('Retry original request')
  await until(() => expect(resets()).toHaveLength(2))
  expect(resets()[1].data).toBe(first.data)
  expect(resets()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
  expect(resets()[1].headers.get('X-CSRF-Token')).toBe('new-csrf')
  expect(puts()).toHaveLength(0)
})

it.each(['session', 'permissions', 'policy', 'target'])(
  'same-batch %s read renewal blocks stale policy confirmation before paint',
  async (kind) => {
    await render()
    await edit()
    await draft()
    const save = button('Save limits')!
    await act(async () => {
      void cache.refetchQueries({
        queryKey:
          kind === 'session'
            ? ['auth', 'session']
            : kind === 'permissions'
              ? ['permissions']
              : kind === 'policy'
                ? ['resource-limits']
                : ['admin', 'member'],
      })
      save.click()
    })
    expect(puts()).toHaveLength(0)
  },
)
it('write revocation preserves the original uncertain intent without exposing an edit action', async () => {
  await uncertain()
  const original = puts()[0]
  permissions = ['members.read']
  await act(async () => cache.refetchQueries({ queryKey: ['permissions'] }))
  await until(() => expect(button('Retry application')).toBeUndefined())
  expect(button('Edit limits')).toBeUndefined()
  expect(puts()).toHaveLength(1)
  permissions = ['members.read', 'limits.users.write']
  await act(async () => cache.refetchQueries({ queryKey: ['permissions'] }))
  await until(() => expect(button('Retry application')).toBeTruthy())
  putStatus = 409
  await click('Retry application')
  await until(() => expect(puts()).toHaveLength(2))
  expect(puts()[1].data).toBe(original.data)
  expect(puts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
})
it('late default-restoration response cannot confirm application after authority renewal', async () => {
  await render()
  await click('Restore defaults')
  await until(() => expect(button('Confirm restoration')).toBeTruthy())
  await input('Reason', 'Original guarded restoration')
  putGate = deferred()
  await click('Confirm restoration')
  await until(() => expect(resets()).toHaveLength(1))
  const original = resets()[0]
  sessionGate = deferred()
  let renewal!: Promise<void>
  await act(async () => {
    renewal = cache.refetchQueries({ queryKey: ['auth', 'session'] })
  })
  await until(() => expect(original.signal!.aborted).toBe(true))
  putGate.release()
  sessionGate.release()
  await act(async () => renewal)
  await until(() => expect(button('Retry original request')).toBeTruthy())
  expect(host.textContent).not.toContain('Current defaults restored and applied.')
  expect(resets()).toHaveLength(1)
})

it('same-batch default reset review refresh blocks stale confirmation before paint', async () => {
  await render()
  await click('Restore defaults')
  await until(() => expect(button('Confirm restoration')).toBeTruthy())
  await input('Reason', 'Original reviewed restoration')
  const confirm = button('Confirm restoration')!
  await act(async () => {
    void cache.refetchQueries({ queryKey: ['default-reset'] })
    confirm.click()
  })
  expect(resets()).toHaveLength(0)
})

it('reset review renewal cannot accept an already pending response as current confirmation', async () => {
  await render()
  await click('Restore defaults')
  await until(() => expect(button('Confirm restoration')).toBeTruthy())
  await input('Reason', 'Original guarded reset review')
  putGate = deferred()
  await click('Confirm restoration')
  await until(() => expect(resets()).toHaveLength(1))
  await act(async () => cache.refetchQueries({ queryKey: ['default-reset'] }))
  await act(async () => putGate!.release())
  await until(() => expect(button('Retry original request')).toBeTruthy())
  expect(host.textContent).not.toContain('Current defaults restored and applied.')
  expect(resets()).toHaveLength(1)
})
