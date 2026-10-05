import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { Session } from '@/types/auth'
import type { MemberDetail } from '@/types/member-recent-login'
import type { MemberAccessSummary as Page } from '@/types/member-access-summary'
import MemberAccessSummary, { MemberAccessRoleSummary } from './member-access-summary'
import { useMemberAccessSummary } from './member-access-summary-read'
import { accessSummaryFixture } from './member-access-summary.fixture'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
let host: HTMLDivElement, root: Root, cache: QueryClient
let actor: string, target: string, generation: number, permissions: string[]
let requests: InternalAxiosRequestConfig[], failure: number, gates: Gate[], gate: Gate | undefined
let transform: ((page: Page) => void) | undefined
let subject: MemberDetail
const subjectKey = () => ['admin', 'member', actor, target, generation]
const session = (): Session => ({
  user: { id: actor, name: 'Reader', email: 'reader@example.invalid', role: 'member' },
  csrf_token: 'current-csrf',
})
type Gate = { promise: Promise<void>; release: () => void }
function deferred() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  const result = { promise, release }
  gates.push(result)
  return result
}
function seed() {
  cache.setQueryData(['auth', 'session'], session())
  cache.setQueryData(['permissions', actor], [...permissions])
  subject = {
    id: target,
    name: 'Target',
    email: 'target@example.invalid',
    role: 'member',
    role_ids: [],
    disabled: false,
    offboarded_at: null,
    created_at: '2026-10-01T01:02:03Z',
    registration_approval: { status: 'not_required', admission_eligible: false },
    last_login_at: '2026-10-02T02:03:04.123456Z',
    last_login_status: 'recorded',
  }
  cache.setQueryData(subjectKey(), subject)
}
beforeEach(async () => {
  actor = 'usr_reader'
  target = 'usr_target'
  generation = 1
  permissions = ['members.read', 'roles.read', 'teams.read_all']
  failure = 0
  transform = undefined
  gates = []
  gate = undefined
  requests = []
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  seed()
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  await i18n.changeLanguage('en')
  client.defaults.adapter = async (config) => {
    requests.push(config)
    if (!config.url?.endsWith('/access')) throw new Error('Unexpected endpoint')
    const flags = cache.getQueryData<string[]>(['permissions', actor]) ?? []
    const page = accessSummaryFixture(config.url.split('/')[3], {
      roles: flags.includes('roles.read'),
      teams: flags.includes('teams.read_all'),
    })
    transform?.(page)
    const wait = gate
    if (wait) await wait.promise
    if (failure)
      throw new AxiosError('Controlled unavailable', '', config, undefined, {
        config,
        status: failure,
        statusText: '',
        headers: new AxiosHeaders(),
        data: { message: 'Unavailable' },
      })
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ 'cache-control': 'private, no-store' }),
      data: page,
    }
  }
})
afterEach(async () => {
  gates.forEach((g) => g.release())
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
})
function Harness({ ready }: { ready: boolean }) {
  const read = useMemberAccessSummary({
    actor,
    target,
    generation,
    ready,
    targetQueryKey: subjectKey(),
  })
  return (
    <>
      <p data-header>
        <MemberAccessRoleSummary read={read} />
      </p>
      <MemberAccessSummary read={read} member={subject} />
    </>
  )
}
async function render(ready = true) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Harness ready={ready} />
      </QueryClientProvider>,
    ),
  )
}
const until = (fn: () => void) =>
  vi.waitFor(
    async () => {
      await act(async () => {})
      fn()
    },
    { timeout: 4000, interval: 10 },
  )
async function mount() {
  await render()
  await until(() => expect(host.textContent).toContain('Recorded Team'))
}
const absent = () => {
  expect(document.body.textContent).not.toContain('Recorded Team')
  expect(document.body.textContent).not.toContain('Recorded role')
  expect(host.querySelector('dl')).toBeNull()
  expect(document.querySelector('[role=tooltip]')).toBeNull()
}
it('renders exactly the approved eight cells, dates and retained facts with one resource read and native0', async () => {
  await mount()
  expect([...host.querySelectorAll('dt')].map((cell) => cell.textContent)).toEqual([
    'Roles',
    'Teams',
    'Recent login',
    'Created',
    'Updated',
    'Status',
    'Registration approval',
    'Current admission',
  ])
  expect(host.textContent).toContain('Member · Recorded role')
  expect(host.textContent).toContain('Oct 2, 2026')
  expect(host.textContent).toContain('Oct 3, 2026')
  expect(requests).toHaveLength(1)
  expect(requests[0].method).toBe('get')
  expect(requests[0].url).toBe('/admin/members/usr_target/access')
  expect(
    cache
      .getQueryCache()
      .find({ queryKey: ['auth', 'session'] })
      ?.getObserversCount(),
  ).toBe(0)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  expect(host.querySelector('[href]')).toBeNull()
})
it('discloses retained inactive context with keyboard focus and restores no old tooltip after invalidation', async () => {
  await mount()
  const trigger = host.querySelector<HTMLButtonElement>(
    'button[aria-label="Retained Team membership details"]',
  )!
  await act(async () => trigger.focus())
  await until(() =>
    expect(document.querySelector('[role=tooltip]')?.textContent).toContain('Retained Team'),
  )
  expect(document.querySelector('[role=tooltip]')?.textContent).toContain('Archived')
  const key = cache
    .getQueryCache()
    .findAll({ queryKey: ['admin', 'member-access-summary'] })[0].queryKey
  await act(async () => {
    void cache.invalidateQueries({ queryKey: key, refetchType: 'none' })
  })
  absent()
  await act(async () => trigger.dispatchEvent(new FocusEvent('focusin', { bubbles: true })))
  absent()
})
it.each(['roles', 'teams'] as const)(
  'keeps %s permission independent and removes hidden metadata before rendering renewed facts',
  async (section) => {
    await mount()
    permissions = permissions.filter(
      (p) => p !== (section === 'roles' ? 'roles.read' : 'teams.read_all'),
    )
    gate = deferred()
    await act(async () => {
      cache.setQueryData(['permissions', actor], [...permissions])
    })
    absent()
    gate.release()
    gate = undefined
    await until(() => expect(host.querySelector('dl')).not.toBeNull())
    expect(host.textContent).toContain('Not authorized')
    expect(host.textContent).not.toContain(section === 'roles' ? 'Recorded role' : 'Recorded Team')
  },
)
it('keeps known empty, overflow, unavailable and historical null separate without fabricated counts', async () => {
  transform = (page) => {
    page.roles = { status: 'overflow', items: null }
    page.teams = { status: 'available', items: [] }
    page.updated_at = null
  }
  subject = { ...subject, last_login_status: 'historical_unavailable', last_login_at: null }
  cache.setQueryData(subjectKey(), subject)
  await render()
  await until(() => expect(host.textContent).toContain('Summary exceeds the supported limit'))
  expect(host.textContent).toContain('No Teams')
  expect(host.textContent).toContain('Historical login time unavailable')
  expect(host.textContent).toContain('Unknown')
  expect(host.querySelector('button[aria-label="Retained Team membership details"]')).toBeNull()
  transform = (page) => {
    page.roles = { status: 'unavailable', items: null }
    page.teams = { status: 'unavailable', items: null }
  }
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['admin', 'member-access-summary'] })
  })
  expect(host.textContent).toContain('Summary unavailable')
  expect(host.textContent).not.toContain('No Teams')
})
it('escapes exact historical empty/control/HTML names without inventing labels or markup', async () => {
  transform = (page) => {
    if (page.roles.status === 'available') page.roles.items[0].name = '<b>recorded</b>\n'
    if (page.teams.status === 'available') page.teams.items[0].name = ''
  }
  await render()
  await until(() => expect(host.textContent).toContain('<b>recorded</b>'))
  expect(host.querySelector('b')).toBeNull()
  expect(host.textContent).not.toContain('No Teams')
})
it('switches live Chinese labels and dates while preserving exact server strings', async () => {
  await mount()
  await act(async () => i18n.changeLanguage('zh'))
  expect([...host.querySelectorAll('dt')].map((cell) => cell.textContent)).toEqual([
    '角色',
    '所属团队',
    '最近登录',
    '创建时间',
    '更新时间',
    '状态',
    '注册审批',
    '当前准入',
  ])
  expect(host.textContent).toContain('Recorded role')
  expect(host.textContent).toContain('Recorded Team')
  expect(host.textContent).toContain('2026年10月2日')
  expect(host.textContent).not.toContain('memberAccess.')
})
it.each(['session', 'permissions', 'target', 'own'] as const)(
  'synchronously hides all cells on %s invalidation without an observer refetch',
  async (which) => {
    await mount()
    const key =
      which === 'session'
        ? ['auth', 'session']
        : which === 'permissions'
          ? ['permissions', actor]
          : which === 'target'
            ? subjectKey()
            : ['admin', 'member-access-summary']
    await act(async () => {
      void cache.invalidateQueries({ queryKey: key, refetchType: 'none' })
    })
    absent()
  },
)
it.each(['session', 'permissions', 'target'] as const)(
  'hides old %s authority errors and recovering refetches',
  async (which) => {
    await mount()
    const key =
      which === 'session'
        ? ['auth', 'session']
        : which === 'permissions'
          ? ['permissions', actor]
          : subjectKey()
    const query = cache.getQueryCache().find({ queryKey: key })!
    await act(async () => {
      query.setState({ status: 'error', error: new Error('unavailable') })
    })
    absent()
    await act(async () => {
      query.setState({ status: 'success', error: null, fetchStatus: 'fetching' })
    })
    absent()
  },
)
it('hides private facts on a resource refresh/error and renders localized retry without stale data', async () => {
  await mount()
  gate = deferred()
  failure = 503
  let pending!: Promise<unknown>
  await act(async () => {
    pending = cache.refetchQueries({ queryKey: ['admin', 'member-access-summary'] })
  })
  absent()
  gate.release()
  gate = undefined
  await act(async () => {
    await pending
  })
  absent()
  expect(host.textContent).toContain('The action failed.')
  failure = 0
  await act(async () => host.querySelector<HTMLButtonElement>('button')!.click())
  await until(() => expect(host.textContent).toContain('Recorded Team'))
})
it('does not let obsolete same-millisecond Session responses restore names or timestamps', async () => {
  vi.spyOn(Date, 'now').mockReturnValue(1234)
  await mount()
  gate = deferred()
  transform = (page) => {
    if (page.roles.status === 'available') page.roles.items[0].name = 'Obsolete Session role'
  }
  await act(async () => {
    void cache.refetchQueries({ queryKey: ['admin', 'member-access-summary'] })
  })
  const old = gate
  transform = (page) => {
    if (page.roles.status === 'available') page.roles.items[0].name = 'Renewed Session role'
  }
  await act(async () => {
    cache.setQueryData(['auth', 'session'], { ...session(), csrf_token: 'renewed-csrf' })
  })
  absent()
  old.release()
  gate = undefined
  await until(() => expect(host.textContent).toContain('Renewed Session role'))
  expect(host.textContent).not.toContain('Obsolete Session role')
  expect(requests.length).toBeGreaterThanOrEqual(3)
})
it('discards late actor/target responses and aborts the old resource read', async () => {
  gate = deferred()
  transform = (page) => {
    if (page.roles.status === 'available') page.roles.items[0].name = 'Obsolete target role'
  }
  await render()
  await until(() => expect(requests).toHaveLength(1))
  const old = gate,
    signal = requests[0].signal
  actor = 'usr_other'
  target = 'usr_peer'
  generation++
  seed()
  transform = (page) => {
    if (page.roles.status === 'available') page.roles.items[0].name = 'Fresh peer role'
  }
  gate = undefined
  await render()
  expect(signal?.aborted).toBe(true)
  old.release()
  await until(() => expect(host.textContent).toContain('Recorded Team'))
  expect(host.textContent).toContain('Fresh peer role')
  expect(host.textContent).not.toContain('Obsolete target role')
  expect(
    cache
      .getQueryCache()
      .findAll({ queryKey: ['admin', 'member-access-summary', 'usr_reader', 'usr_target', 1] })
      .every((query) => query.state.data === undefined),
  ).toBe(true)
  expect(requests.at(-1)?.url).toBe('/admin/members/usr_peer/access')
})
it('denied parent readiness or members permission issues no resource request', async () => {
  await render(false)
  absent()
  expect(requests).toHaveLength(0)
  cache.setQueryData(['permissions', actor], ['roles.read', 'teams.read_all'])
  await render()
  absent()
  expect(requests).toHaveLength(0)
})
it('rejects an identity-role mismatch without showing otherwise valid resource facts', async () => {
  transform = (page) => {
    page.identity_role = 'admin'
  }
  await render()
  await until(() => expect(host.textContent).toContain('The action failed.'))
  absent()
})
