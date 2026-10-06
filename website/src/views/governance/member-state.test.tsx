import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider, useParams } from 'react-router'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import AuthGate from '@/components/app/AuthGate'
import type { Session } from '@/types/auth'
import type { MemberStateRecord } from '@/types/member-state'
import MemberState from './member-state'
import {
  memberStateFixture,
  memberStateResultFixture,
  stateReviewETag,
} from './member-state.fixture'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const old = client.defaults.adapter
let host: HTMLDivElement,
  root: Root,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[],
  actor: string,
  actorRole: 'admin' | 'member',
  csrf: string,
  permissions: string[],
  state: MemberStateRecord,
  patchStatus: number,
  readStatus: number,
  commitFailure: boolean
let gates: Map<string, { promise: Promise<void>; release: () => void }>
function pause(key: string) {
  let release!: () => void
  const promise = new Promise<void>((r) => {
    release = r
  })
  const g = { promise, release }
  gates.set(key, g)
  return g
}
const failure = (c: InternalAxiosRequestConfig, status: number) =>
  new AxiosError('Controlled rejection', '', c, undefined, {
    config: c,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: {},
  })
function Harness() {
  const { target = 'usr_target' } = useParams(),
    generation = useSessionGeneration()
  const session = useQuery({
      queryKey: ['auth', 'session'],
      queryFn: async ({ signal }) => (await client.get<Session>('/auth/session', { signal })).data,
      retry: false,
      staleTime: 0,
    }),
    a = session.data?.user.id ?? ''
  const grants = useQuery({
    queryKey: ['permissions', a],
    queryFn: async ({ signal }) =>
      (await client.get<{ permissions: string[] }>('/auth/permissions', { signal })).data
        .permissions,
    enabled: !!a,
    retry: false,
    staleTime: 0,
  })
  const contextQueryKey = ['admin', 'member', a, target, generation]
  const context = useQuery({
    queryKey: contextQueryKey,
    queryFn: async ({ signal }) => (await client.get(`/admin/members/${target}`, { signal })).data,
    enabled: !!a,
    retry: false,
    staleTime: 0,
  })
  return (
    <MemberState
      actor={a}
      target={target}
      generation={generation}
      ready={
        !!session.data &&
        session.isSuccess &&
        !session.isFetching &&
        grants.isSuccess &&
        !grants.isFetching &&
        context.isSuccess &&
        !context.isFetching
      }
      contextKind="detail"
      contextQueryKey={contextQueryKey}
      owner="settings"
      mode="settings"
    >
      <section aria-label="Independent offboarding">Separate offboarding</section>
    </MemberState>
  )
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_admin'
  actorRole = 'admin'
  csrf = 'csrf-first'
  permissions = ['members.read', 'members.write']
  state = memberStateFixture({
    id: 'usr_target',
    name: 'Target',
    role: 'member',
    disabled: false,
    offboarded_at: null,
  })
  patchStatus = readStatus = 0
  commitFailure = false
  gates = new Map()
  requests = []
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown, etag: string | undefined
    if (config.url === '/setup') data = { initialized: true }
    else if (config.url === '/auth/session')
      data = {
        user: { id: actor, name: 'Actor', email: 'actor@example.invalid', role: actorRole },
        csrf_token: csrf,
      }
    else if (config.url === '/auth/permissions') data = { permissions: [...permissions] }
    else if (config.url?.endsWith('/state')) {
      if (readStatus) throw failure(config, readStatus)
      data = {
        ...state,
        user_id: config.url.split('/')[3],
        can_change_base_role: permissions.includes('members.write') && actorRole === 'admin',
        can_change_status:
          permissions.includes('members.write') &&
          (actorRole === 'admin' ||
            (config.url.split('/')[3] !== actor && state.base_role !== 'admin')),
      }
      etag = state.etag
    } else if (config.method === 'patch') {
      const input = JSON.parse(config.data)
      if (!patchStatus || commitFailure) {
        const { confirmation, effect, ...updated } = memberStateResultFixture(
          state,
          input,
          'b'.repeat(64),
        )
        void confirmation
        void effect
        state = updated
        state.user_id = config.url!.split('/')[3]
      }
      if (patchStatus) throw failure(config, patchStatus)
      data = memberStateResultFixture(state, input, state.etag)
      etag = state.etag
    } else if (/^\/admin\/members\/[^/]+$/.test(config.url ?? ''))
      data = {
        id: config.url!.split('/')[3],
        name: state.name,
        email: 'target@example.invalid',
        role: state.base_role,
        disabled: state.disabled,
        offboarded_at: state.offboarded_at,
        role_ids: [],
        created_at: '2026-09-23T00:00:00Z',
        registration_approval: { status: 'not_required', admission_eligible: false },
        last_login_at: null,
        last_login_status: 'historical_unavailable',
      }
    else throw new Error('Unexpected endpoint ' + config.url)
    const captured = structuredClone(data)
    if (gates.has(`${config.method} ${config.url}`))
      await gates.get(`${config.method} ${config.url}`)!.promise
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(
        etag ? { etag: `"${etag}"`, 'cache-control': 'private, no-store' } : {},
      ),
      data: captured,
    }
  }
})
afterEach(async () => {
  for (const g of gates.values()) g.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = old
  vi.restoreAllMocks()
})
async function until(check: () => void) {
  await vi.waitFor(
    async () => {
      await act(async () => {
        await new Promise((r) => setTimeout(r, 0))
      })
      check()
    },
    { timeout: 4000, interval: 10 },
  )
}
async function mount(auth = false, target = 'usr_target') {
  router = createMemoryRouter(
    auth
      ? [
          {
            element: <AuthGate mode="private" />,
            children: [{ path: '/controlled/:target', element: <Harness /> }],
          },
          { path: '/login', element: <p>Signed out safely</p> },
        ]
      : [{ path: '/controlled/:target', element: <Harness /> }],
    { initialEntries: [`/controlled/${target}`] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(host.textContent).toContain(
      state.account_access_runtime_applied
        ? 'Account lifecycle gate applied'
        : 'Account lifecycle application unknown',
    ),
  )
}
async function click(text: string) {
  await act(async () => {
    const button = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
      (b) => b.textContent === text,
    )
    expect(button).toBeDefined()
    button!.click()
  })
}
async function reason(value = 'Controlled state change') {
  await act(async () => {
    const textarea = document.querySelector('textarea')!
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
      textarea,
      value,
    )
    textarea.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function role(value: 'admin' | 'member') {
  await act(async () => {
    const select = host.querySelector<HTMLSelectElement>('select[name="role"]')!
    select.value = value
    select.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function renew(key: readonly unknown[]) {
  await act(async () => {
    void cache.invalidateQueries({ queryKey: key })
  })
}
it('keeps original Settings card hierarchy and independent read-only fields without directories or extra Session observers', async () => {
  await mount()
  expect([...host.querySelectorAll('h3')].map((n) => n.textContent)).toEqual([
    'Base role',
    'Account access',
  ])
  expect(host.textContent!.indexOf('Separate offboarding')).toBeGreaterThan(
    host.textContent!.indexOf('Base role'),
  )
  expect(host.textContent!.indexOf('Account access')).toBeGreaterThan(
    host.textContent!.indexOf('Separate offboarding'),
  )
  expect(requests.filter((r) => r.url?.endsWith('/state'))).toHaveLength(1)
  expect(
    requests.some(
      (r) => r.url?.includes('/roles') || r.url?.includes('/keys') || r.url?.includes('/teams'),
    ),
  ).toBe(false)
  expect(
    cache
      .getQueryCache()
      .find({ queryKey: ['auth', 'session'] })!
      .getObserversCount(),
  ).toBe(1)
})
it('requires an explicit base-role confirmation and exact reason/IfMatch/current CSRF', async () => {
  await mount()
  await role('admin')
  await click('Save base role')
  expect(requests.some((r) => r.method === 'patch')).toBe(false)
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
    'does not enable the account',
  )
  await reason()
  await click('Confirm base identity')
  await until(() => expect(requests.filter((r) => r.method === 'patch')).toHaveLength(1))
  const request = requests.find((r) => r.method === 'patch')!
  expect(JSON.parse(request.data)).toEqual({ role: 'admin', reason: 'Controlled state change' })
  expect(request.headers.get('If-Match')).toBe(`"${stateReviewETag}"`)
  expect(request.headers.get('X-CSRF-Token')).toBe(csrf)
  await until(() => expect(document.body.textContent).toContain('current base identity'))
})
it('reactivation preserves historical offboarding without promising old credential/explicit-role restoration', async () => {
  state = {
    ...state,
    disabled: true,
    status: 'offboarded',
    offboarded_at: '2026-10-05T00:00:00Z',
    activation_mode: 'reactivate',
  }
  await mount()
  await click('Reactivate')
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
    'Historical offboarding remains',
  )
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
    'explicit role assignments are not restored',
  )
  await reason()
  await click('Confirm reactivation')
  await until(() => expect(requests.filter((r) => r.method === 'patch')).toHaveLength(1))
  expect(JSON.parse(requests.find((r) => r.method === 'patch')!.data)).toEqual({
    disabled: false,
    reason: 'Controlled state change',
  })
  expect(state.offboarded_at).toBeNull()
})
it('ordinary disabled access uses Enable rather than reactivation and confirms only current account application', async () => {
  state = { ...state, disabled: true, status: 'disabled', activation_mode: 'enable' }
  await mount()
  expect(
    [...host.querySelectorAll('button')].some((button) => button.textContent === 'Reactivate'),
  ).toBe(false)
  await click('Enable')
  expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain(
    'Historical offboarding remains',
  )
  await reason()
  await click('Confirm enable')
  await until(() => expect(document.body.textContent).toContain('current account access'))
  expect(state.disabled).toBe(false)
  expect(state.offboarded_at).toBeNull()
  expect(state.account_access_runtime_applied).toBe(true)
  expect(requests.filter((request) => request.method === 'patch')).toHaveLength(1)
})
it('displays unknown account application separately while allowing a reviewed role-only current DB effect', async () => {
  state.account_access_runtime_applied = false
  await mount(false)
  await until(() => expect(host.textContent).toContain('Account lifecycle application unknown'))
  await role('admin')
  await click('Save base role')
  await reason()
  await click('Confirm base identity')
  await until(() => expect(document.body.textContent).toContain('current base identity'))
  expect(state.account_access_runtime_applied).toBe(false)
})
it('read-only member sees retained configuration without edit controls', async () => {
  actorRole = 'member'
  permissions = ['members.read']
  await mount()
  expect(host.querySelector('select')!.disabled).toBe(true)
  expect(
    [...host.querySelectorAll('button')].some(
      (b) => b.textContent === 'Disable' || b.textContent === 'Save base role',
    ),
  ).toBe(false)
  expect(requests.some((r) => r.method === 'patch')).toBe(false)
})
it('delegated writer can review another ordinary target status but cannot edit base identity or self/admin access', async () => {
  actorRole = 'member'
  await mount()
  expect(host.querySelector('select')!.disabled).toBe(true)
  await click('Disable')
  expect(document.querySelector('[role="dialog"]')).not.toBeNull()
  await reason()
  await click('Confirm disable')
  await until(() => expect(state.disabled).toBe(true))
  expect(
    requests.some((r) => r.method === 'patch' && Object.hasOwn(JSON.parse(r.data), 'role')),
  ).toBe(false)
})
it('invalid reason stays unchanged and prevents dispatch', async () => {
  await mount()
  await click('Disable')
  await reason(' trailing ')
  await click('Confirm disable')
  expect(document.querySelector('textarea')?.value).toBe(' trailing ')
  expect(document.body.textContent).toContain('Enter a reason')
  expect(requests.some((r) => r.method === 'patch')).toBe(false)
})
it('matching GET does not confirm committed uncertainty; rejected retry retains original tuple and fresh CSRF reconciliation is current-only', async () => {
  await mount()
  await click('Disable')
  await reason()
  patchStatus = 503
  commitFailure = true
  await click('Confirm disable')
  await until(() =>
    expect(document.body.textContent).toContain('original state request is unconfirmed'),
  )
  const first = requests.find((r) => r.method === 'patch')!
  commitFailure = false
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'member-state'] })
  })
  expect(requests.filter((r) => r.method === 'patch')).toHaveLength(1)
  expect(document.body.textContent).not.toContain('Current member state confirmed')
  patchStatus = 409
  await click('Retry original state request')
  await until(() => expect(requests.filter((r) => r.method === 'patch')).toHaveLength(2))
  expect(document.body.textContent).toContain('unconfirmed')
  csrf = 'csrf-renewed'
  await act(async () =>
    cache.setQueryData(['auth', 'session'], {
      user: { id: actor, name: 'Actor', email: 'actor@example.invalid', role: actorRole },
      csrf_token: csrf,
    }),
  )
  await until(() => expect(document.body.textContent).toContain('Retry original state request'))
  patchStatus = 0
  await click('Retry original state request')
  await until(() => expect(requests.filter((r) => r.method === 'patch')).toHaveLength(3))
  const writes = requests.filter((r) => r.method === 'patch')
  expect(
    writes.every(
      (r) => r.data === first.data && r.headers.get('If-Match') === first.headers.get('If-Match'),
    ),
  ).toBe(true)
  expect(writes.at(-1)!.headers.get('X-CSRF-Token')).toBe('csrf-renewed')
  await until(() =>
    expect(document.body.textContent).toContain('does not prove the original operation'),
  )
})
it('first committed 409 retains its exact intent through fresh reads, Escape, Cancel and current-only retry', async () => {
  await mount()
  await click('Disable')
  await reason('First 409 retained reason')
  patchStatus = 409
  commitFailure = true
  await click('Confirm disable')
  await until(() => expect(document.body.textContent).toContain('unconfirmed'))
  const first = requests.find((r) => r.method === 'patch')!
  commitFailure = false
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'member-state'] })
  })
  await until(() => expect(document.body.textContent).toContain('Retry original state request'))
  expect(state.disabled).toBe(true)
  expect(document.body.textContent).not.toContain('Current member state confirmed')
  expect(document.querySelector('textarea')?.value).toBe('First 409 retained reason')
  expect(document.querySelector('textarea')?.disabled).toBe(true)
  await act(async () =>
    document
      .querySelector('[role="dialog"]')!
      .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
  )
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  await click('Resume state confirmation')
  await click('Cancel')
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  expect(requests.filter((r) => r.method === 'patch')).toHaveLength(1)
  await click('Resume state confirmation')
  csrf = 'csrf-first409-renewed'
  await act(async () =>
    cache.setQueryData(['auth', 'session'], {
      user: { id: actor, name: 'Actor', email: 'actor@example.invalid', role: actorRole },
      csrf_token: csrf,
    }),
  )
  await until(() => expect(document.body.textContent).toContain('Retry original state request'))
  patchStatus = 0
  await click('Retry original state request')
  await until(() =>
    expect(document.body.textContent).toContain('does not prove the original operation'),
  )
  const writes = requests.filter((r) => r.method === 'patch')
  expect(writes).toHaveLength(2)
  expect(writes[1].data).toBe(first.data)
  expect(writes[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
  expect(writes[1].headers.get('X-CSRF-Token')).toBe('csrf-first409-renewed')
})
it('a first409 intent survives failed renewed reads and its obsolete Abandon button cannot clear it', async () => {
  await mount()
  await click('Disable')
  await reason('Retained through failed read')
  patchStatus = 409
  await click('Confirm disable')
  await until(() => expect(document.body.textContent).toContain('unconfirmed'))
  const first = requests.find((r) => r.method === 'patch')!
  const abandon = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === 'Abandon original state request',
  )!
  readStatus = 503
  await act(async () => cache.invalidateQueries({ queryKey: ['admin', 'member-state'] }))
  await until(() =>
    expect(document.body.textContent).toContain('Current write authority is unavailable'),
  )
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(document.body.textContent).not.toContain('Retained through failed read')
  await act(async () => abandon.click())
  readStatus = 0
  await act(async () => cache.invalidateQueries({ queryKey: ['admin', 'member-state'] }))
  await until(() => expect(document.body.textContent).toContain('Retry original state request'))
  expect(document.querySelector('textarea')?.value).toBe('Retained through failed read')
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.body.textContent).toContain('放弃原始状态请求')
  expect(document.body.textContent).toContain('原始结果仍未知')
  expect(document.body.textContent).not.toContain('memberState.')
  await act(async () => i18n.changeLanguage('en'))
  patchStatus = 0
  await click('Retry original state request')
  await until(() => expect(requests.filter((r) => r.method === 'patch')).toHaveLength(2))
  expect(requests.filter((r) => r.method === 'patch')[1].data).toBe(first.data)
  expect(requests.filter((r) => r.method === 'patch')[1].headers.get('If-Match')).toBe(
    first.headers.get('If-Match'),
  )
})
it('conflict preserves role/reason and requires explicit current label/state review before a new request', async () => {
  await mount()
  await role('admin')
  await click('Save base role')
  await reason()
  patchStatus = 409
  state = { ...state, name: 'Renamed target', etag: 'd'.repeat(64) }
  await click('Confirm base identity')
  await until(() =>
    expect(document.body.textContent).toContain('original request remains unconfirmed'),
  )
  expect(host.querySelector('select')?.value).toBe('admin')
  expect(document.querySelector('textarea')?.value).toBe('Controlled state change')
  expect(requests.filter((r) => r.method === 'patch')).toHaveLength(1)
  expect(document.body.textContent).not.toContain('Review current member state')
  expect(document.body.textContent).toContain('original outcome remains unknown')
  await click('Abandon original state request')
  expect(document.querySelector('textarea')?.value).toBe('Controlled state change')
  const pendingConfirm = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (button) => button.textContent === 'Confirm base identity',
  )!
  expect(pendingConfirm.disabled).toBe(true)
  await click('Review current member state')
  patchStatus = 0
  await click('Confirm base identity')
  await until(() => expect(requests.filter((r) => r.method === 'patch')).toHaveLength(2))
  expect(
    requests
      .filter((r) => r.method === 'patch')
      .at(-1)!
      .headers.get('If-Match'),
  ).toBe('"' + 'd'.repeat(64) + '"')
})
it('same-ms Session renewal hides the dialog synchronously and preserves the original uncertain body', async () => {
  vi.spyOn(Date, 'now').mockReturnValue(new Date('2026-10-05T01:00:00Z').getTime())
  await mount()
  await click('Disable')
  await reason()
  patchStatus = 503
  await click('Confirm disable')
  await until(() => expect(document.body.textContent).toContain('unconfirmed'))
  const first = requests.find((r) => r.method === 'patch')!,
    pending = pause('get /auth/session')
  await renew(['auth', 'session'])
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  await act(async () => pending.release())
  await until(() => expect(document.body.textContent).toContain('Retry original state request'))
  await click('Retry original state request')
  await until(() => expect(requests.filter((r) => r.method === 'patch')).toHaveLength(2))
  expect(requests.filter((r) => r.method === 'patch').at(-1)!.data).toBe(first.data)
})
it('read error and synchronous invalidation remove private modal content and obsolete callbacks cannot dispatch', async () => {
  await mount()
  await click('Disable')
  await reason()
  const button = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === 'Confirm disable',
  )!
  readStatus = 503
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['admin', 'member-state'] })
    button.click()
  })
  await until(() => expect(document.body.textContent).toContain('Member state could not be read.'))
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(requests.some((r) => r.method === 'patch')).toBe(false)
})
it('late old-target PATCH never restores original success/private name', async () => {
  await mount()
  await click('Disable')
  await reason()
  const pending = pause('patch /admin/members/usr_target')
  await click('Confirm disable')
  await until(() => expect(requests.some((r) => r.method === 'patch')).toBe(true))
  state = {
    ...state,
    name: 'Other',
    user_id: 'usr_other',
    etag: 'e'.repeat(64),
    disabled: false,
    status: 'active',
    activation_mode: null,
  }
  await act(async () => router.navigate('/controlled/usr_other'))
  await act(async () => pending.release())
  await until(() =>
    expect(host.textContent).toContain(
      state.account_access_runtime_applied
        ? 'Account lifecycle gate applied'
        : 'Account lifecycle application unknown',
    ),
  )
  expect(document.body.textContent).not.toContain('Current member state confirmed')
  expect(document.body.textContent).not.toContain('Target')
})
it('duplicate same-batch confirmations send one immutable request', async () => {
  await mount()
  await click('Disable')
  await reason()
  const pending = pause('patch /admin/members/usr_target')
  const button = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === 'Confirm disable',
  )!
  await act(async () => {
    button.click()
    button.click()
  })
  await until(() => expect(requests.filter((r) => r.method === 'patch')).toHaveLength(1))
  const abandon = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === 'Abandon original state request',
  )!
  expect(abandon.disabled).toBe(true)
  await act(async () => abandon.click())
  expect(document.body.textContent).toContain('Retry original state request')
  expect(requests.filter((r) => r.method === 'patch')).toHaveLength(1)
  await act(async () => pending.release())
})
it('self-demotion403 hides review and retains uncertainty until legitimate role authority returns', async () => {
  state = { ...state, user_id: actor, base_role: 'admin' }
  await mount(false, actor)
  await role('member')
  await click('Save base role')
  await reason()
  patchStatus = 403
  commitFailure = true
  actorRole = 'member'
  await click('Confirm base identity')
  await until(() =>
    expect(document.body.textContent).toContain('Current write authority is unavailable'),
  )
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(document.body.textContent).not.toContain('Current member state confirmed')
  const first = requests.find((r) => r.method === 'patch')!
  actorRole = 'admin'
  patchStatus = 0
  commitFailure = false
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['auth', 'session'] })
  })
  await until(() => expect(document.body.textContent).toContain('Retry original state request'))
  await click('Retry original state request')
  await until(() => expect(requests.filter((r) => r.method === 'patch')).toHaveLength(2))
  expect(requests.filter((r) => r.method === 'patch').at(-1)!.data).toBe(first.data)
})
it('self-disable401 uses existing AuthGate cleanup, preserves public site and never treats logout as original success', async () => {
  state = { ...state, user_id: actor, base_role: 'admin' }
  cache.setQueryData(['site'], { name: 'Public site' })
  cache.setQueryData(['private-fixture'], { name: 'private' })
  await mount(true, actor)
  await click('Disable')
  await reason()
  patchStatus = 401
  commitFailure = true
  await click('Confirm disable')
  await until(() => expect(host.textContent).toContain('Signed out safely'))
  expect(cache.getQueryData(['site'])).toEqual({ name: 'Public site' })
  expect(cache.getQueryData(['private-fixture'])).toBeUndefined()
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(requests.filter((r) => r.method === 'patch')).toHaveLength(1)
})
it('live Chinese switching preserves an original reason and changes reactivation guidance', async () => {
  state = {
    ...state,
    disabled: true,
    status: 'offboarded',
    offboarded_at: '2026-10-05T00:00:00Z',
    activation_mode: 'reactivate',
  }
  await mount()
  await click('Reactivate')
  await reason()
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.querySelector('textarea')?.value).toBe('Controlled state change')
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain('历史离岗记录保留')
  expect(document.body.textContent).not.toContain('memberState.')
})

it('Escape returns focus only to the connected current trigger and preserves the unsent draft/reason', async () => {
  await mount()
  await role('admin')
  const trigger = [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === 'Save base role',
  )!
  await act(async () => trigger.focus())
  await click('Save base role')
  await reason('Unsent reason')
  await act(async () =>
    document
      .querySelector('[role="dialog"]')!
      .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
  )
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  expect(document.activeElement).toBe(trigger)
  expect(host.querySelector('select')!.value).toBe('admin')
  await click('Save base role')
  expect(document.querySelector('textarea')!.value).toBe('Unsent reason')
  expect(requests.some((r) => r.method === 'patch')).toBe(false)
})
it('closing and reopening after unchanged-proof continuity conflict cannot bypass explicit review', async () => {
  await mount()
  await role('admin')
  await click('Save base role')
  await reason()
  patchStatus = 409
  await click('Confirm base identity')
  await until(() => expect(document.body.textContent).toContain('continuity changed'))
  await act(async () =>
    document
      .querySelector('[role="dialog"]')!
      .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
  )
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  await click('Resume state confirmation')
  expect(document.body.textContent).toContain('Retry original state request')
  expect(document.body.textContent).not.toContain('Review current member state')
  expect(document.querySelector('textarea')?.value).toBe('Controlled state change')
  await click('Abandon original state request')
  const confirm = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === 'Confirm base identity',
  )!
  expect(confirm.disabled).toBe(true)
  await click('Review current member state')
  await until(() => {
    const currentConfirm = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
      (button) => button.textContent === 'Confirm base identity',
    )
    expect(currentConfirm?.disabled).toBe(false)
  })
  expect(requests.filter((r) => r.method === 'patch')).toHaveLength(1)
})
