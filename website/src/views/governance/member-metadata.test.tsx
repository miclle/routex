import { accessSummaryFixture } from './member-access-summary.fixture'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import MembersPage from './members'
import type { Session } from '@/types/auth'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter,
  revision = 'a'.repeat(64)
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let session: Session,
  permissions: string[],
  requests: InternalAxiosRequestConfig[],
  saved: string,
  canEdit: boolean
let metadataFailure: number, writeFailure: number, parentFailure: number
let intercept: ((config: InternalAxiosRequestConfig) => Promise<unknown> | undefined) | undefined
const metadata = (target = 'usr_target') => ({
  user_id: target,
  name: saved,
  status: 'active',
  can_edit: canEdit,
  etag: revision,
})
function error(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('Controlled failure', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: {},
  })
}
beforeEach(async () => {
  session = {
    user: { id: 'usr_admin', name: 'Admin', email: 'admin@example.invalid', role: 'admin' },
    csrf_token: 'csrf_one',
  }
  permissions = ['members.read', 'members.write']
  requests = []
  saved = 'Target'
  canEdit = true
  metadataFailure = writeFailure = parentFailure = 0
  intercept = undefined
  await i18n.changeLanguage('en')
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
    const target = config.url?.split('/')[3] ?? 'usr_target'
    let data: unknown
    const pending = intercept?.(config)
    if (pending) data = await pending
    else if (config.url === '/auth/session') data = structuredClone(session)
    else if (config.url === '/auth/permissions') data = { permissions: [...permissions] }
    else if (config.url?.endsWith('/metadata')) {
      if (config.method === 'put') {
        if (writeFailure) throw error(config, writeFailure)
        saved = JSON.parse(config.data).name
        data = { ...metadata(target), confirmation: 'current_member_name' }
      } else {
        if (metadataFailure) throw error(config, metadataFailure)
        data = metadata(target)
      }
    } else if (config.url?.startsWith('/admin/members/')) {
      if (parentFailure) throw error(config, parentFailure)
      data = {
        id: target,
        name: saved,
        email: 'target@example.invalid',
        role: 'member',
        role_ids: [],
        last_login_at: null,
        last_login_status: 'historical_unavailable',
        disabled: false,
        offboarded_at: null,
        created_at: '2026-09-23T00:00:00Z',
      }
    } else throw new Error('Unexpected controlled endpoint ' + config.url)
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(config.url?.endsWith('/metadata') ? { ETag: `"${revision}"` } : {}),
      data,
    }
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
})
async function until(check: () => void) {
  await vi.waitFor(async () => {
    await act(async () => {})
    check()
  })
}
async function mount(expectForm = true) {
  router = createMemoryRouter([{ path: '/admin/members/:memberId', element: <MembersPage /> }], {
    initialEntries: ['/admin/members/usr_target?tab=settings'],
  })
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  if (expectForm) await until(() => expect(form()).not.toBeNull())
  else await until(() => expect(requests.some((r) => r.url === '/auth/permissions')).toBe(true))
}
function form() {
  return host.querySelector<HTMLFormElement>(
    'form[aria-label="Member basic information"],form[aria-label="成员基本信息"]',
  )!
}
function nameInput() {
  return form().querySelector<HTMLInputElement>('[name="name"]')!
}
function button(label: string, scope: ParentNode = document) {
  const found = [...scope.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === label,
  )
  expect(found, label).toBeDefined()
  return found!
}
async function click(label: string, scope: ParentNode = document) {
  await act(async () => button(label, scope).click())
}
async function change(input: HTMLInputElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function prepare(name = 'New name', reason = 'Controlled reason') {
  await change(nameInput(), name)
  await click('Save basic information', host)
  const dialog = document.querySelector('[role="dialog"]')!
  await change(dialog.querySelector<HTMLInputElement>('input')!, reason)
  await click('Review current name')
  await until(() => expect(button('Confirm name').disabled).toBe(false))
}
const writes = () => requests.filter((r) => r.method === 'put' && r.url?.endsWith('/metadata'))
function deferred() {
  let resolve!: (v: unknown) => void
  const promise = new Promise<unknown>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
it('uses the approved card with parent email and separate lifecycle/role actions without an extra Session observer', async () => {
  await mount()
  expect(nameInput().value).toBe('Target')
  const settingsTab = button('Settings', host)
  expect(settingsTab.getAttribute('aria-controls')).toBe(
    'member-settings-panel-usr_admin-usr_target',
  )
  expect(host.querySelectorAll('[role="tabpanel"]')).toHaveLength(1)
  expect(form().querySelector<HTMLInputElement>('input:not([name])')?.disabled).toBe(true)
  expect(form().textContent).toContain('Maintain the member name')
  expect(host.textContent).toContain('Save base role')
  expect(host.textContent).toContain('Account access')
  expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(1)
  expect(requests.some((r) => r.url === '/account' || r.url === '/admin/models')).toBe(false)
  await prepare()
  await click('Confirm name')
  await until(() => expect(nameInput().value).toBe('New name'))
  expect(JSON.parse(writes()[0].data)).toEqual({ name: 'New name', reason: 'Controlled reason' })
  expect(writes()[0].headers.get('If-Match')).toBe(`"${revision}"`)
  expect(writes()).toHaveLength(1)
  expect(host.textContent).toContain('Current member name confirmed')
})
it.each([{ grants: ['members.read'] }, { grants: ['members.write'] }])(
  'keeps UI read/write separation for %j',
  async ({ grants }) => {
    permissions = grants
    await mount(grants.includes('members.read'))
    if (grants.includes('members.read')) {
      expect(nameInput().disabled).toBe(true)
      expect(host.textContent).not.toContain('Save basic information')
    } else {
      expect(host.textContent).not.toContain('target@example.invalid')
      expect(requests.some((r) => r.url?.endsWith('/metadata'))).toBe(false)
    }
    expect(writes()).toHaveLength(0)
  },
)
it('honors server protected-target editability despite explicit write permission', async () => {
  canEdit = false
  await mount()
  expect(nameInput().disabled).toBe(true)
  expect(host.textContent).not.toContain('Save basic information')
})
it('keeps exact Unicode names and reason while switching language in the open review', async () => {
  await mount()
  await prepare('😀'.repeat(100), 'é'.repeat(512))
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain('确认成员姓名')
  expect(document.querySelector<HTMLInputElement>('[role="dialog"] input')?.value).toBe(
    'é'.repeat(512),
  )
  expect(nameInput().value).toBe('😀'.repeat(100))
  await click('确认姓名')
  await until(() => expect(writes()).toHaveLength(1))
  expect(JSON.parse(writes()[0].data).name).toBe('😀'.repeat(100))
})
it('validates the reason byte bound without truncating it', async () => {
  await mount()
  await prepare('Valid', 'é'.repeat(513))
  await click('Confirm name')
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain('1024 UTF-8 bytes')
  expect(document.querySelector<HTMLInputElement>('[role="dialog"] input')?.value).toBe(
    'é'.repeat(513),
  )
  expect(writes()).toHaveLength(0)
})
it('retains unsent draft across identical same-ms network renewal and fresh metadata', async () => {
  vi.spyOn(Date, 'now').mockReturnValue(1791158400000)
  await mount()
  await change(nameInput(), 'Unsent draft')
  const old = cache.getQueryState(['auth', 'session'])!,
    pending = deferred()
  intercept = (c) => (c.url === '/auth/session' ? pending.promise : undefined)
  let renewal!: Promise<void>
  await act(async () => {
    renewal = cache.refetchQueries({ queryKey: ['auth', 'session'] })
  })
  await until(() => expect(form()).toBeNull())
  await act(async () => pending.resolve(structuredClone(session)))
  await renewal
  await until(() => expect(nameInput().value).toBe('Unsent draft'))
  const next = cache.getQueryState(['auth', 'session'])!
  expect(next.dataUpdatedAt).toBe(old.dataUpdatedAt)
  expect(next.dataUpdateCount).toBe(old.dataUpdateCount + 1)
  expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2)
  expect(writes()).toHaveLength(0)
})
it('retains original uncertain name/reason/ETag through renewal and rejected retry without rereview or automatic replay', async () => {
  await mount()
  await prepare()
  writeFailure = 503
  await click('Confirm name')
  await until(() => expect(button('Retry original name request').disabled).toBe(false))
  const first = writes()[0]
  saved = 'Intervening name'
  session.csrf_token = 'csrf_two'
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['auth', 'session'] })
  })
  await until(() => expect(button('Retry original name request').disabled).toBe(false))
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain('New name')
  expect(
    [...document.querySelectorAll('button')].some((b) => b.textContent === 'Review current name'),
  ).toBe(false)
  expect(writes()).toHaveLength(1)
  writeFailure = 409
  await click('Retry original name request')
  await until(() =>
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('original review'),
  )
  expect(writes()[1].data).toBe(first.data)
  expect(writes()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
  expect(writes()[1].headers.get('X-CSRF-Token')).toBe('csrf_two')
  writeFailure = 0
  await click('Retry original name request')
  await until(() => expect(nameInput().value).toBe('New name'))
  expect(writes()).toHaveLength(3)
  expect(writes()[2].data).toBe(first.data)
})
it.each(['metadata', 'parent', 'permission'])(
  'hides uncertain review on %s outage without destroying original intent',
  async (kind) => {
    await mount()
    await prepare()
    writeFailure = 503
    await click('Confirm name')
    await until(() => expect(button('Retry original name request').disabled).toBe(false))
    const key =
      kind === 'metadata'
        ? ['member-metadata']
        : kind === 'parent'
          ? ['admin', 'member']
          : ['permissions']
    if (kind === 'metadata') metadataFailure = 403
    else if (kind === 'parent') parentFailure = 403
    else permissions = []
    await act(async () => {
      await cache.invalidateQueries({ queryKey: key })
    })
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(form()).toBeNull()
    expect(writes()).toHaveLength(1)
    metadataFailure = parentFailure = 0
    permissions = ['members.read', 'members.write']
    await act(async () => {
      await cache.invalidateQueries({ queryKey: key })
    })
    await until(() => expect(button('Retry original name request').disabled).toBe(false))
    expect(document.querySelector<HTMLInputElement>('[role="dialog"] input')?.value).toBe(
      'Controlled reason',
    )
    expect(writes()).toHaveLength(1)
  },
)
it.each(['actor', 'target', 'tab', 'unmount'])(
  'destroys obsolete intent and ignores late success on %s change',
  async (kind) => {
    await mount()
    await prepare()
    const pending = deferred()
    intercept = (c) => (c.method === 'put' ? pending.promise : undefined)
    await click('Confirm name')
    const signal = writes()[0].signal
    if (kind === 'actor') {
      session = { ...session, user: { ...session.user, id: 'usr_other' } }
      await act(async () => {
        await cache.refetchQueries({ queryKey: ['auth', 'session'] })
      })
    } else if (kind === 'target')
      await act(async () => {
        await router.navigate('/admin/members/usr_other?tab=settings')
      })
    else if (kind === 'tab')
      await act(async () => {
        await router.navigate('/admin/members/usr_target?tab=keys')
      })
    else await act(async () => root.render(null))
    await act(async () =>
      pending.resolve({ ...metadata(), name: 'New name', confirmation: 'current_member_name' }),
    )
    expect(signal?.aborted).toBe(true)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(host.textContent).not.toContain('Current member name confirmed')
    expect(writes()).toHaveLength(1)
  },
)
it('fences same-event write permission loss before dispatch', async () => {
  await mount()
  await prepare()
  const confirm = button('Confirm name')
  await act(async () => {
    cache.setQueryData(['permissions', 'usr_admin'], ['members.read'])
    confirm.click()
  })
  expect(writes()).toHaveLength(0)
  expect(document.querySelector('[role="dialog"]')).toBeNull()
})
it.each(['Cancel', 'Escape'])(
  'returns %s focus only to the newly reviewed connected Save trigger',
  async (action) => {
    await mount()
    const oldTrigger = button('Save basic information', host)
    await prepare()
    const currentTrigger = button('Save basic information', host)
    expect(oldTrigger.isConnected).toBe(false)
    if (action === 'Cancel') await click('Cancel')
    else
      await act(async () =>
        document.activeElement?.dispatchEvent(
          new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
        ),
      )
    await until(() => expect(document.activeElement).toBe(currentTrigger))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  },
)
it('does not restore an uncertain intent after logout and same-actor login', async () => {
  await mount()
  await prepare()
  writeFailure = 503
  await click('Confirm name')
  await until(() => expect(button('Retry original name request').disabled).toBe(false))
  await act(async () => cache.setQueryData(['auth', 'session'], null))
  expect(form()).toBeNull()
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  await act(async () => cache.setQueryData(['auth', 'session'], structuredClone(session)))
  await until(() => expect(form()).not.toBeNull())
  expect(host.textContent).not.toContain('Resume name confirmation')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(writes()).toHaveLength(1)
})
it('never captures private name intent in a mutation cache', async () => {
  await mount()
  await prepare()
  writeFailure = 503
  await click('Confirm name')
  await until(() => expect(button('Retry original name request').disabled).toBe(false))
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})

it('renders retained labels as text and allows a stricter replacement without inventing identity changes', async () => {
  saved = '<img src=x>\tLegacy'
  await mount()
  expect(nameInput().value).toBe(saved)
  expect(host.querySelector('img')).toBeNull()
  await prepare('Valid replacement', 'Repair retained label')
  await click('Confirm name')
  await until(() => expect(nameInput().value).toBe('Valid replacement'))
  expect(writes()).toHaveLength(1)
})
