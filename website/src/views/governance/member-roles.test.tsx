import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import MembersPage from './members'
import { accessSummaryFixture } from './member-access-summary.fixture'
import { roleSummary, rolesWorkspace, roleReviewETag } from './member-roles.fixture'
import type { MemberRolesWorkspace } from '@/types/member-roles'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
let host: HTMLDivElement,
  root: Root,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let calls: InternalAxiosRequestConfig[],
  permissions: string[],
  actor: string,
  actorRole: 'admin' | 'member',
  csrf: string
let page: MemberRolesWorkspace,
  candidates: ReturnType<typeof roleSummary>[],
  putStatus: number,
  getStatus: number,
  detailStatus: number
let gate: { promise: Promise<void>; release: () => void } | null,
  gatedPath: string,
  gates: (() => void)[]
const failure = (config: InternalAxiosRequestConfig, status: number) =>
  new AxiosError('Controlled failure', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: {},
  })
function pause(path: string) {
  let release!: () => void
  const promise = new Promise<void>((r) => {
    release = r
  })
  gate = { promise, release }
  gatedPath = path
  gates.push(release)
  return gate
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_admin'
  actorRole = 'admin'
  csrf = 'csrf-first'
  permissions = ['members.read', 'roles.read', 'members.write']
  calls = []
  page = rolesWorkspace()
  candidates = [roleSummary('rol_candidate', 'Candidate role')]
  putStatus = getStatus = detailStatus = 0
  gate = null
  gatedPath = ''
  gates = []
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.defaults.adapter = async (config) => {
    calls.push(config)
    let data: unknown, etag: string | undefined
    const target = config.url?.split('/')[3] ?? 'usr_target'
    if (config.url === '/auth/session')
      data = {
        user: { id: actor, name: 'Current actor', email: 'actor@example.invalid', role: actorRole },
        csrf_token: csrf,
      }
    else if (config.url === '/auth/permissions') data = { permissions: [...permissions] }
    else if (/^\/admin\/members\/[^/]+$/.test(config.url ?? ''))
      data = {
        id: target,
        name: 'Target',
        email: 'target@example.invalid',
        role: page.identity_role,
        disabled: page.subject_status === 'disabled',
        offboarded_at: page.subject_status === 'offboarded' ? '2026-10-04T00:00:00Z' : null,
        created_at: '2026-10-01T00:00:00Z',
        role_ids: page.assigned_roles.map((r) => r.id),
        registration_approval: { status: 'not_required', admission_eligible: false },
        last_login_at: null,
        last_login_status: 'historical_unavailable',
      }
    else if (config.url?.endsWith('/access')) {
      data = accessSummaryFixture(target, {
        roles: permissions.includes('roles.read'),
        teams: false,
      })
      ;(data as ReturnType<typeof accessSummaryFixture>).identity_role = page.identity_role
      if ((data as ReturnType<typeof accessSummaryFixture>).roles.status === 'available')
        (data as ReturnType<typeof accessSummaryFixture>).roles.items = []
    } else if (config.url?.endsWith('/roles') && config.method === 'put') {
      if (gate && gatedPath === 'put') await gate.promise
      if (putStatus) throw failure(config, putStatus)
      const body = JSON.parse(config.data),
        rows = new Map([...page.assigned_roles, ...candidates].map((r) => [r.id, r]))
      page = {
        ...page,
        etag: 'c'.repeat(64),
        assigned_roles: body.role_ids.map((id: string) => rows.get(id)!),
        effective_permissions: body.role_ids.length ? ['models.write'] : [],
      }
      data = {
        user_id: target,
        role_ids: body.role_ids,
        etag: page.etag,
        confirmation: 'current_member_roles',
        effect: 'current_database',
      }
      etag = page.etag
    } else if (config.url?.endsWith('/roles/candidates')) {
      const q = config.params?.q ?? ''
      const rows = candidates.filter((r) => r.name.includes(q))
      data = { items: rows, next_cursor: null, etag: page.etag }
      etag = page.etag
    } else if (config.url?.endsWith('/roles')) {
      if (getStatus) throw failure(config, getStatus)
      data = structuredClone({
        ...page,
        user_id: target,
        ...(actorRole === 'member'
          ? {
              can_edit: false,
              edit_blockers: ['not_platform_admin'],
              candidate_status: 'not_authorized',
            }
          : {}),
      })
      etag = page.etag
    } else if (config.url?.includes('/roles/')) {
      if (detailStatus) throw failure(config, detailStatus)
      const id = config.url.split('/').at(-1),
        row = [page.builtin_role, ...page.assigned_roles, ...candidates].find((r) => r.id === id)!
      data = {
        user_id: target,
        role: row,
        permissions: row.permission_count ? ['providers.read'] : [],
        etag: page.etag,
      }
      etag = page.etag
    } else throw new Error('Unexpected endpoint ' + config.url)
    const captured = structuredClone(data)
    if (gate && gatedPath === config.url) await gate.promise
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({
        'cache-control': 'private, no-store',
        ...(etag ? { etag: `"${etag}"` } : {}),
      }),
      data: captured,
    }
  }
  router = createMemoryRouter([{ path: '/admin/members/:memberId', element: <MembersPage /> }], {
    initialEntries: ['/admin/members/usr_target?tab=roles'],
  })
})
afterEach(async () => {
  gates.forEach((r) => r())
  await act(async () => root.unmount())
  router.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
})
const until = (fn: () => void) =>
  vi.waitFor(
    async () => {
      await act(async () => {})
      fn()
    },
    { timeout: 4000, interval: 10 },
  )
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain('Recorded custom role'))
}
async function click(text: string) {
  const button = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === text,
  )!
  expect(button).toBeTruthy()
  await act(async () => button.click())
}
async function reason(text = 'Controlled roles change') {
  const node = document.querySelector<HTMLTextAreaElement>('textarea[aria-label="Reason"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(node, text)
    node.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function remove() {
  await until(() =>
    expect(
      host.querySelector<HTMLButtonElement>('[aria-label="Remove Recorded custom role"]')?.disabled,
    ).toBe(false),
  )
  await act(async () =>
    host.querySelector<HTMLButtonElement>('[aria-label="Remove Recorded custom role"]')!.click(),
  )
}
async function openConfirm() {
  await remove()
  await click('Save member roles')
  await until(() => expect(document.querySelector('[role="dialog"] textarea')).not.toBeNull())
}

it('renders assigned-only four columns, immutable builtin and scoped resources without automatic definition/global reads', async () => {
  await mount()
  const table = host.querySelector('table[aria-label="Assigned member roles"]')!
  expect([...table.querySelectorAll('th')].map((n) => n.textContent)).toEqual([
    'Role',
    'Type',
    'Permissions',
    'Actions',
  ])
  expect(table.querySelectorAll('tbody tr')).toHaveLength(2)
  expect(table.textContent).not.toContain('Candidate role')
  expect(table.querySelector('[aria-label="Remove Member"]')).toBeNull()
  expect(host.textContent).toContain('Effective member permissions')
  expect(calls.some((c) => c.url === '/admin/roles' || c.url === '/admin/teams')).toBe(false)
  expect(calls.filter((c) => c.url?.includes('/roles/rol_'))).toHaveLength(0)
  expect(calls.every((c) => c.method === 'get')).toBe(true)
})
it('reads full permissions only for explicit exact role Dialog and closes on Escape', async () => {
  await mount()
  await act(async () =>
    host
      .querySelector<HTMLButtonElement>(
        '[aria-label="View full permissions for Recorded custom role"]',
      )!
      .click(),
  )
  await until(() =>
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Allowed actions'),
  )
  const request = calls.find((c) => c.url?.endsWith('/roles/rol_custom'))!
  expect(request.headers.get('If-Match')).toBe(`"${roleReviewETag}"`)
  expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('Candidate role')
  await act(async () =>
    document.activeElement?.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    ),
  )
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
})
it('local Remove preserves saved union, empty custom replacement and exact original reason/proofs', async () => {
  await mount()
  await openConfirm()
  expect(host.textContent).toContain('Provider connections')
  expect(host.textContent).toContain('View')
  expect(calls.some((c) => c.method === 'put')).toBe(false)
  await reason()
  await click('Confirm roles')
  await until(() => expect(page.assigned_roles).toEqual([]))
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  const request = calls.find((c) => c.method === 'put')!
  expect(JSON.parse(request.data)).toEqual({
    role_ids: [],
    role_definitions: [],
    builtin_definition_etag: 'b'.repeat(64),
    reason: 'Controlled roles change',
  })
  expect(request.headers.get('If-Match')).toBe(`"${roleReviewETag}"`)
  expect(host.textContent).toContain('Base identity')
})
it('requires reason without trimming user draft or dispatch', async () => {
  await mount()
  await openConfirm()
  await reason(' trailing ')
  await click('Confirm roles')
  expect(calls.some((c) => c.method === 'put')).toBe(false)
  expect(document.querySelector('textarea')?.value).toBe(' trailing ')
  expect(document.body.textContent).toContain('Enter a reason')
})
it('releases the pending latch after uncertain and rejected retries without refreshing authority or workspace', async () => {
  await mount()
  await openConfirm()
  await reason()
  const reads = () => calls.filter((c) => c.method === 'get').length
  const beforeReads = reads()
  const beforeSession = cache.getQueryState(['auth', 'session'])!.dataUpdateCount
  const enabledRetry = () => {
    const button = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
      (b) => b.textContent === 'Retry original role request',
    )!
    expect(button).toBeTruthy()
    expect(button.disabled).toBe(false)
  }
  for (const [index, status] of [503, 409, 0].entries()) {
    putStatus = status
    const pending = pause('put')
    const button = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
      (b) => b.textContent === (index === 0 ? 'Confirm roles' : 'Retry original role request'),
    )!
    await act(async () => button.click())
    expect(button.disabled).toBe(true)
    await act(async () => button.click())
    expect(calls.filter((c) => c.method === 'put')).toHaveLength(index + 1)
    await act(async () => pending.release())
    gate = null
    if (status) {
      await until(enabledRetry)
      expect(document.body.textContent).toContain('original role request is unconfirmed')
      expect(reads()).toBe(beforeReads)
      expect(cache.getQueryState(['auth', 'session'])!.dataUpdateCount).toBe(beforeSession)
    }
  }
  const writes = calls.filter((c) => c.method === 'put')
  expect(writes).toHaveLength(3)
  expect(writes.every((c) => c.data === writes[0].data)).toBe(true)
  expect(writes.every((c) => c.headers.get('If-Match') === writes[0].headers.get('If-Match'))).toBe(
    true,
  )
  expect(writes.every((c) => c.headers.get('X-CSRF-Token') === 'csrf-first')).toBe(true)
  await until(() =>
    expect(document.body.textContent).toContain('does not prove the original operation'),
  )
})
it('retains immutable uncertain tuple through matching GET and rejected retry then confirms current state only', async () => {
  await mount()
  await openConfirm()
  await reason()
  putStatus = 503
  await click('Confirm roles')
  await until(() =>
    expect(document.body.textContent).toContain('original role request is unconfirmed'),
  )
  const first = calls.find((c) => c.method === 'put')!
  page = { ...page, etag: 'd'.repeat(64), assigned_roles: [] }
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'member-roles'] })
  })
  expect(calls.filter((c) => c.method === 'put')).toHaveLength(1)
  expect(document.body.textContent).toContain('unconfirmed')
  putStatus = 409
  await click('Retry original role request')
  await until(() => expect(calls.filter((c) => c.method === 'put')).toHaveLength(2))
  expect(document.body.textContent).toContain('unconfirmed')
  putStatus = 0
  csrf = 'csrf-new'
  await act(async () => {
    cache.setQueryData(['auth', 'session'], {
      user: { id: actor, name: 'Admin', role: 'admin' },
      csrf_token: csrf,
    })
  })
  await until(() =>
    expect(
      [...document.querySelectorAll<HTMLButtonElement>('button')].some(
        (b) => b.textContent === 'Retry original role request' && !b.disabled,
      ),
    ).toBe(true),
  )
  await click('Retry original role request')
  await until(() => expect(calls.filter((c) => c.method === 'put')).toHaveLength(3))
  const writes = calls.filter((c) => c.method === 'put')
  expect(
    writes.every(
      (c) => c.data === first.data && c.headers.get('If-Match') === first.headers.get('If-Match'),
    ),
  ).toBe(true)
  expect(writes.at(-1)!.headers.get('X-CSRF-Token')).toBe('csrf-new')
  await until(() =>
    expect(document.body.textContent).toContain('does not prove the original operation'),
  )
})
it('fresh read-only actor cannot edit or fetch candidates', async () => {
  actorRole = 'member'
  await mount()
  expect(host.textContent).toContain('Only a current platform administrator')
  expect(
    host.querySelector('[aria-label="Remove Recorded custom role"]')?.hasAttribute('disabled'),
  ).toBe(true)
  expect(calls.some((c) => c.url?.endsWith('/candidates') || c.method === 'put')).toBe(false)
})
it.each(['members.read', 'roles.read'])(
  'denies reads independently without %s',
  async (permission) => {
    permissions = permissions.filter((p) => p !== permission)
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(host.textContent).toMatch(/unavailable|Access denied/))
    expect(calls.some((c) => c.url?.endsWith('/roles') || c.url?.includes('/roles/'))).toBe(false)
    expect(document.body.textContent).not.toContain('Recorded custom role')
  },
)
it('disabled target is editable and configured union is explicitly inactive', async () => {
  page.subject_status = 'disabled'
  page.permission_use = 'inactive'
  await mount()
  await remove()
  expect(host.textContent).toContain('do not grant current access')
  expect(host.textContent).toContain('Save member roles')
})
it('offboarded target and1001 historical assignments stay read-only without partial rows', async () => {
  page.subject_status = 'offboarded'
  page.permission_use = 'inactive'
  page.can_edit = false
  page.edit_blockers = ['offboarded']
  await mount()
  expect(host.textContent).toContain('Offboarded members are read-only')
  expect(calls.some((c) => c.url?.endsWith('/candidates'))).toBe(false)
})
it('ordinary conflict keeps draft/reason and requires explicit new review instead of automatic retry', async () => {
  await mount()
  await openConfirm()
  await reason()
  page = { ...page, etag: 'd'.repeat(64) }
  putStatus = 409
  await click('Confirm roles')
  await until(() => expect(host.textContent).toContain('Review current roles'))
  expect(calls.filter((c) => c.method === 'put')).toHaveLength(1)
  await click('Review current roles')
  await until(() => expect(host.textContent).not.toContain('draft uses an older review'))
  putStatus = 0
  await click('Save member roles')
  expect(document.querySelector('textarea')?.value).toBe('Controlled roles change')
  await click('Confirm roles')
  await until(() => expect(calls.filter((c) => c.method === 'put')).toHaveLength(2))
  expect(
    calls
      .filter((c) => c.method === 'put')
      .at(-1)!
      .headers.get('If-Match'),
  ).toBe(`"${'d'.repeat(64)}"`)
})
it('synchronously hides workspace and open permission portal on invalidation/error', async () => {
  await mount()
  await act(async () =>
    host
      .querySelector<HTMLButtonElement>(
        '[aria-label="View full permissions for Recorded custom role"]',
      )!
      .click(),
  )
  await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
  getStatus = 503
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['admin', 'member-roles'], refetchType: 'none' })
  })
  expect(document.body.textContent).not.toContain('Recorded custom role')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['admin', 'member-roles'] })
  })
  expect(document.body.textContent).not.toContain('Recorded custom role')
  expect(calls.some((c) => c.method === 'put')).toBe(false)
})
it('rejects late definition after target change and clears operation on tab exit', async () => {
  await mount()
  const held = pause('/admin/members/usr_target/roles/rol_custom')
  await act(async () =>
    host
      .querySelector<HTMLButtonElement>(
        '[aria-label="View full permissions for Recorded custom role"]',
      )!
      .click(),
  )
  await until(() => expect(calls.some((c) => c.url === gatedPath)).toBe(true))
  await act(async () => router.navigate('/admin/members/usr_other?tab=roles'))
  held.release()
  await until(() =>
    expect(calls.some((c) => c.url === '/admin/members/usr_other/roles')).toBe(true),
  )
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(calls.find((c) => c.url === gatedPath)?.signal?.aborted).toBe(true)
})
it('same-ms Session renewal hides rows and retains same-owner uncertain intent without automatic write', async () => {
  vi.spyOn(Date, 'now').mockReturnValue(1791172800000)
  await mount()
  await openConfirm()
  await reason()
  putStatus = 503
  await click('Confirm roles')
  await until(() => expect(document.body.textContent).toContain('unconfirmed'))
  const held = pause('/auth/session')
  const stamp = cache.getQueryState(['auth', 'session'])!.dataUpdatedAt
  await act(async () => {
    void cache.fetchQuery({
      queryKey: ['auth', 'session'],
      queryFn: async () => {
        await held.promise
        return { user: { id: actor, name: 'Renewed', role: actorRole }, csrf_token: 'renewed-csrf' }
      },
    })
  })
  expect(document.body.textContent).not.toContain('Recorded custom role')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  held.release()
  await until(() => expect(document.body.textContent).toContain('unconfirmed'))
  expect(calls.filter((c) => c.method === 'put')).toHaveLength(1)
  expect(cache.getQueryState(['auth', 'session'])!.dataUpdateCount).toBeGreaterThan(1)
  expect(cache.getQueryState(['auth', 'session'])!.dataUpdatedAt).toBe(stamp)
})
it('live Chinese switching preserves reason and updates confirmation copy', async () => {
  await mount()
  await openConfirm()
  await reason()
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.querySelector('textarea')?.value).toBe('Controlled roles change')
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain('确认成员角色')
  expect(document.body.textContent).not.toContain('memberRoles.')
})

it('multi-select Add preserves choices beyond literal search pages and saves exact reviewed definitions once', async () => {
  await mount()
  await until(() =>
    expect(
      host.querySelector<HTMLInputElement>('input[aria-label="Search roles to add"]')?.disabled,
    ).toBe(false),
  )
  const input = host.querySelector<HTMLInputElement>('input[aria-label="Search roles to add"]')!
  await act(async () => {
    input.focus()
    input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
  })
  await until(() =>
    expect(document.querySelector('[role="option"]')?.textContent).toContain('Candidate role'),
  )
  await act(async () => document.querySelector<HTMLElement>('[role="option"]')!.click())
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      input,
      'no%_match',
    )
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await until(() =>
    expect(calls.some((c) => c.url?.endsWith('/candidates') && c.params.q === 'no%_match')).toBe(
      true,
    ),
  )
  await until(() =>
    expect(
      [...host.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent === 'Add')
        ?.disabled,
    ).toBe(false),
  )
  await click('Add')
  const table = host.querySelector('table[aria-label="Assigned member roles"]')!
  expect(table.textContent).toContain('Candidate role')
  expect(table.querySelectorAll('tbody tr')).toHaveLength(3)
  expect(calls.some((c) => c.method === 'put')).toBe(false)
  expect(host.textContent).toContain('Provider connections')
  expect(host.textContent).not.toContain('Manage models')
  await click('Save member roles')
  await reason()
  await click('Confirm roles')
  await until(() => expect(calls.filter((c) => c.method === 'put')).toHaveLength(1))
  const body = JSON.parse(calls.find((c) => c.method === 'put')!.data)
  expect(body.role_ids).toEqual(['rol_candidate', 'rol_custom'])
  expect(body.role_definitions).toEqual([
    { id: 'rol_candidate', etag: 'b'.repeat(64) },
    { id: 'rol_custom', etag: 'b'.repeat(64) },
  ])
})
it('oversize UTF8 search remains correctable and obsolete same-batch Add cannot stage old candidates', async () => {
  await mount()
  await until(() =>
    expect(
      host.querySelector<HTMLInputElement>('input[aria-label="Search roles to add"]')?.disabled,
    ).toBe(false),
  )
  const input = host.querySelector<HTMLInputElement>('input[aria-label="Search roles to add"]')!
  await act(async () => {
    input.focus()
    input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
  })
  await until(() =>
    expect(document.querySelector('[role="option"]')?.textContent).toContain('Candidate role'),
  )
  await act(async () => document.querySelector<HTMLElement>('[role="option"]')!.click())
  const add = [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === 'Add',
  )!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      input,
      '中'.repeat(67),
    )
    input.dispatchEvent(new Event('input', { bubbles: true }))
    add.click()
  })
  expect(host.textContent).toContain('at most 200 UTF-8 bytes')
  expect(input.disabled).toBe(false)
  expect(host.querySelector('table')!.textContent).not.toContain('Candidate role')
  expect(calls.some((c) => c.url?.endsWith('/candidates') && c.params.q === '中'.repeat(67))).toBe(
    false,
  )
  const pending = pause('/admin/members/usr_target/roles/candidates')
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      input,
      'Candidate',
    )
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await until(() =>
    expect(calls.some((c) => c.url?.endsWith('/candidates') && c.params.q === 'Candidate')).toBe(
      true,
    ),
  )
  expect(input.disabled).toBe(false)
  expect(document.querySelector('[role="option"]')).toBeNull()
  expect(add.disabled).toBe(true)
  await act(async () => pending.release())
  await until(() => expect(add.disabled).toBe(false))
  await click('Add')
  expect(host.querySelector('table')!.textContent).toContain('Candidate role')
  expect(calls.some((c) => c.method === 'put')).toBe(false)
})
it('candidate read failure clears private options while literal search remains editable for recovery', async () => {
  const adapter = client.defaults.adapter
  if (typeof adapter !== 'function') throw new Error('Fixture adapter missing')
  client.defaults.adapter = async (config) => {
    if (config.url?.endsWith('/roles/candidates') && config.params.q === 'failed literal') {
      calls.push(config)
      throw failure(config, 503)
    }
    return adapter(config)
  }
  await mount()
  const input = host.querySelector<HTMLInputElement>('input[aria-label="Search roles to add"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      input,
      'failed literal',
    )
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await until(() => expect(host.textContent).toContain('Role candidates could not be read.'))
  expect(input.disabled).toBe(false)
  expect(document.querySelector('[role="option"]')).toBeNull()
  expect(
    [...host.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent === 'Add')
      ?.disabled,
  ).toBe(true)
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      input,
      'Candidate',
    )
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.focus()
    input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
  })
  await until(() =>
    expect(document.querySelector('[role="option"]')?.textContent).toContain('Candidate role'),
  )
  expect(input.value).toBe('Candidate')
  expect(calls.some((c) => c.method === 'put')).toBe(false)
})
it('1001 retained assignments remain fully visible with typed audit blocker and no candidate reads', async () => {
  page.assigned_roles = Array.from({ length: 1001 }, (_, i) =>
    roleSummary(
      `legacy_${String(i).padStart(4, '0')}`,
      i === 0 ? 'Recorded custom role' : `Retained ${i}`,
    ),
  )
  page.can_edit = false
  page.edit_blockers = ['assignment_audit_bound']
  await mount()
  expect(
    host.querySelector('table[aria-label="Assigned member roles"]')!.querySelectorAll('tbody tr'),
  ).toHaveLength(1002)
  expect(host.textContent).toContain('complete audit bound')
  expect(calls.some((c) => c.url?.endsWith('/candidates') || c.method === 'put')).toBe(false)
})
it('late PUT after target switch cannot restore private success or clear the new target draft', async () => {
  await mount()
  await openConfirm()
  await reason()
  const held = pause('put')
  await click('Confirm roles')
  await until(() => expect(calls.some((c) => c.method === 'put')).toBe(true))
  await act(async () => router.navigate('/admin/members/usr_other?tab=roles'))
  held.release()
  await until(() =>
    expect(calls.some((c) => c.url === '/admin/members/usr_other/roles')).toBe(true),
  )
  expect(document.body.textContent).not.toContain('Current member roles confirmed')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
})
it('independent Role read revocation closes portals synchronously and obsolete callbacks cannot write', async () => {
  await mount()
  await act(async () =>
    host
      .querySelector<HTMLButtonElement>(
        '[aria-label="View full permissions for Recorded custom role"]',
      )!
      .click(),
  )
  await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
  permissions = ['members.read', 'members.write']
  await act(async () => cache.setQueryData(['permissions', actor], permissions))
  expect(document.body.textContent).not.toContain('Recorded custom role')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(calls.some((c) => c.method === 'put')).toBe(false)
})
it('leaving the addressable Roles tab destroys original uncertainty instead of carrying it into another tab', async () => {
  await mount()
  await openConfirm()
  await reason()
  putStatus = 503
  await click('Confirm roles')
  await until(() => expect(document.body.textContent).toContain('unconfirmed'))
  await act(async () => router.navigate('/admin/members/usr_target?tab=settings'))
  expect(document.body.textContent).not.toContain('original role request')
  expect(document.querySelector('textarea[aria-label="Reason"]')).toBeNull()
})

it('duplicate submits dispatch one complete immutable request while pending', async () => {
  await mount()
  await openConfirm()
  await reason()
  const held = pause('put')
  const form = document.querySelector('[role="dialog"] form')!
  await act(async () => {
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
  expect(calls.filter((c) => c.method === 'put')).toHaveLength(1)
  held.release()
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
})
it('late old-actor workspace never restores earlier role names after an actor switch', async () => {
  await mount()
  const held = pause('/admin/members/usr_target/roles')
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['admin', 'member-roles'] })
  })
  await until(() =>
    expect(calls.filter((c) => c.url === '/admin/members/usr_target/roles')).toHaveLength(2),
  )
  expect(document.body.textContent).not.toContain('Recorded custom role')
  actor = 'usr_next'
  page.assigned_roles = [roleSummary('rol_custom', 'Current actor role')]
  await act(async () => {
    await cache.fetchQuery({
      queryKey: ['auth', 'session'],
      queryFn: async () => ({
        user: { id: actor, name: 'Next actor', role: actorRole },
        csrf_token: 'new-actor-csrf',
      }),
    })
  })
  held.release()
  gate = null
  await until(() => expect(document.body.textContent).toContain('Current actor role'))
  expect(document.body.textContent).not.toContain('Recorded custom role')
  expect(calls.some((c) => c.method === 'put')).toBe(false)
})

it('changed selected and builtin definitions reject original reconciliation without replacing immutable proofs', async () => {
  await mount()
  await until(
    () =>
      host.querySelector<HTMLInputElement>('input[aria-label="Search roles to add"]')?.disabled ===
      false,
  )
  const input = host.querySelector<HTMLInputElement>('input[aria-label="Search roles to add"]')!
  await act(async () => {
    input.focus()
    input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
  })
  await until(() => !!document.querySelector('[role="option"]'))
  await act(async () => document.querySelector<HTMLElement>('[role="option"]')!.click())
  await click('Add')
  await click('Save member roles')
  await reason()
  putStatus = 503
  await click('Confirm roles')
  await until(() => expect(document.body.textContent).toContain('unconfirmed'))
  const first = calls.find((c) => c.method === 'put')!
  page = {
    ...page,
    etag: 'd'.repeat(64),
    assigned_roles: [
      { ...roleSummary('rol_candidate', 'Candidate role'), definition_etag: 'e'.repeat(64) },
      { ...roleSummary(), definition_etag: 'e'.repeat(64) },
    ],
    builtin_role: { ...page.builtin_role, definition_etag: 'f'.repeat(64) },
  }
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'member-roles'] })
  })
  expect(calls.filter((c) => c.method === 'put')).toHaveLength(1)
  putStatus = 409
  await click('Retry original role request')
  await until(() => expect(calls.filter((c) => c.method === 'put')).toHaveLength(2))
  const retry = calls.filter((c) => c.method === 'put')[1]
  expect(retry.data).toBe(first.data)
  expect(retry.headers.get('If-Match')).toBe(first.headers.get('If-Match'))
  expect(
    JSON.parse(retry.data).role_definitions.every(
      (proof: { etag: string }) => proof.etag === 'b'.repeat(64),
    ),
  ).toBe(true)
  expect(JSON.parse(retry.data).builtin_definition_etag).toBe('b'.repeat(64))
  expect(document.body.textContent).toContain('unconfirmed')
  expect(document.body.textContent).not.toContain('Current member roles confirmed')
})

it('duty classification displays immutable Finance as an explicitly removable assignment, preserving intrinsic Member', async () => {
  Object.assign(page.builtin_role, { assignment_kind: 'intrinsic' })
  page.assigned_roles = [
    roleSummary(),
    { ...roleSummary('rol_finance', 'Finance'), builtin: true, assignment_kind: 'explicit' },
  ]
  await mount()
  expect(host.textContent).toContain('Finance')
  expect(host.textContent).toContain('Member')
  const remove = host.querySelector<HTMLButtonElement>('button[aria-label="Remove Finance"]')!
  expect(remove.disabled).toBe(false)
  expect(host.querySelector('button[aria-label="Remove Member"]')).toBeNull()
  await act(async () => remove.click())
  expect(host.textContent).not.toContain('Finance')
  expect(calls.filter((r) => r.method === 'put')).toHaveLength(0)
  await click('Save member roles')
  await reason('Reviewed Finance removal')
  expect(calls.filter((r) => r.method === 'put')).toHaveLength(0)
  await click('Confirm roles')
  await until(() => expect(calls.filter((r) => r.method === 'put')).toHaveLength(1))
  const write = calls.find((r) => r.method === 'put')!
  expect(write.headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
  expect(JSON.parse(write.data)).toEqual({
    role_ids: ['rol_custom'],
    role_definitions: [{ id: 'rol_custom', etag: 'b'.repeat(64) }],
    builtin_definition_etag: 'b'.repeat(64),
    reason: 'Reviewed Finance removal',
  })
  expect(page.builtin_role.id).toBe('rol_member')
  expect(page.assigned_roles.map((r) => r.id)).toEqual(['rol_custom'])
})

it('duty names switch live while a same-named custom assignment keeps its recorded name', async () => {
  page.assigned_roles = [
    roleSummary(),
    { ...roleSummary('rol_finance', 'Finance'), builtin: true, assignment_kind: 'explicit' },
    roleSummary('rol_same_name', 'Finance'),
  ]
  await mount()
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.querySelector('button[aria-label="移除 财务"]')).not.toBeNull()
  expect(host.querySelector('button[aria-label="移除 Finance"]')).not.toBeNull()
  expect(calls.filter((r) => r.method === 'put')).toHaveLength(0)
})

it('localizes duty picker options and selected chips live, retaining a same-named custom Role and exact assignment IDs', async () => {
  candidates = [
    { ...roleSummary('rol_finance', 'Finance'), builtin: true, assignment_kind: 'explicit' },
    roleSummary('rol_same_name', 'Finance'),
  ]
  await mount()
  await until(() =>
    expect(
      host.querySelector<HTMLButtonElement>('button[aria-label="Search roles to add"]')?.disabled,
    ).toBe(false),
  )
  await act(async () =>
    host.querySelector<HTMLButtonElement>('button[aria-label="Search roles to add"]')!.click(),
  )
  await until(() => expect(document.querySelectorAll('[role="option"]')).toHaveLength(2))
  const reads = calls.filter((r) => r.url?.endsWith('/candidates')).length
  await act(async () => i18n.changeLanguage('zh'))
  const options = [...document.querySelectorAll<HTMLElement>('[role="option"]')]
  expect(options.map((r) => r.textContent)).toEqual(['财务', 'Finance'])
  await act(async () => options.find((r) => r.textContent === '财务')!.click())
  await until(() =>
    expect(host.querySelector('button[aria-label="移除已选角色 财务"]')).not.toBeNull(),
  )
  await act(async () => i18n.changeLanguage('en'))
  expect(host.querySelector('button[aria-label="Remove selected role Finance"]')).not.toBeNull()
  expect(calls.filter((r) => r.url?.endsWith('/candidates'))).toHaveLength(reads)
  expect(calls.filter((r) => r.method === 'put')).toHaveLength(0)
  await click('Add')
  expect(host.querySelector('button[aria-label="Remove Finance"]')).not.toBeNull()
  await click('Save member roles')
  await reason('Explicit Finance duty assignment')
  await click('Confirm roles')
  await until(() => expect(calls.filter((r) => r.method === 'put')).toHaveLength(1))
  const write = calls.find((r) => r.method === 'put')!
  expect(JSON.parse(write.data).role_ids).toEqual(['rol_custom', 'rol_finance'])
  expect(JSON.parse(write.data).role_definitions).toEqual([
    { id: 'rol_custom', etag: 'b'.repeat(64) },
    { id: 'rol_finance', etag: 'b'.repeat(64) },
  ])
  expect(write.headers.get('If-Match')).toBe(`"${roleReviewETag}"`)
  expect(page.builtin_role.id).toBe('rol_member')
})
