import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import { getRoleDefinition, setRoleDefinition } from '@/api/role-definition'
import type { RoleDefinition, RoleDefinitionInput } from '@/types/role-definition'
import { RoleDefinitionEditor } from './role-definition'
import RolesPage from './roles'
import { getRoles, getPermissions } from '@/api/governance'
import { getSession } from '@/api/auth'

vi.mock('@/api/role-definition', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/role-definition')>()),
  getRoleDefinition: vi.fn(),
  setRoleDefinition: vi.fn(),
}))
vi.mock('@/api/governance', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/governance')>()),
  getRoles: vi.fn(),
  getPermissions: vi.fn(),
}))
vi.mock('@/api/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/auth')>()),
  getSession: vi.fn(),
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const review = 'a'.repeat(64),
  identity = 'b'.repeat(64),
  changed = 'c'.repeat(64)
const fixture = (): RoleDefinition => ({
  id: 'rol_custom',
  name: 'Original role',
  description: 'Original business scope',
  builtin: false,
  assignment_kind: 'explicit',
  permissions: ['providers.read'],
  available_permissions: ['models.read_all', 'providers.read'],
  definition_etag: changed,
  identity_etag: identity,
  review_etag: review,
  can_edit: true,
})
const session = (actor = 'usr_admin', csrf = 'csrf-first', role: 'admin' | 'member' = 'admin') => ({
  user: { id: actor, name: 'Actor', email: 'actor@example.invalid', role },
  csrf_token: csrf,
})
let host: HTMLDivElement, root: Root, cache: QueryClient, page: RoleDefinition
let mode: 'edit' | 'view',
  open: boolean,
  epoch: number,
  ready: boolean,
  actor: string,
  target: string
let requests: { target: string; etag: string; input: RoleDefinitionInput; csrf: string }[]
let failures: number[], gate: { promise: Promise<void>; release: () => void } | null
let readFailure: boolean, readGate: { promise: Promise<void>; release: () => void } | null
let trigger: HTMLButtonElement, returns: number
const context = () => ['admin', 'roles', actor, 'controlled-parent']
function pause() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  return { promise, release }
}
function tree() {
  return (
    <QueryClientProvider client={cache}>
      <RoleDefinitionEditor
        key={`${actor}:${target}`}
        actor={actor}
        target={target}
        generation="parent-generation"
        ready={ready}
        open={open}
        mode={mode}
        openEpoch={epoch}
        contextQueryKey={context()}
        onClose={() => {
          open = false
          root.render(tree())
        }}
        returnFocus={() => {
          returns++
          return trigger.isConnected ? trigger : false
        }}
        resourceLabel={(value) => value}
        permissionLabel={(value) => value}
        renderPermissions={(permissions) => (
          <ul aria-label="Reviewed permissions">
            {permissions.map((value) => (
              <li key={value}>{value}</li>
            ))}
          </ul>
        )}
      />
    </QueryClientProvider>
  )
}
async function draw() {
  await act(async () => root.render(tree()))
  await settle()
}
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 10))
  })
}
const label = (key: string) => i18n.t(key, { ns: 'governance' })
function button(key: string) {
  const value = [...document.querySelectorAll('button')].find(
    (node) => node.textContent === label(key),
  )
  if (!value) throw new Error(`Missing button ${key}`)
  return value
}
async function click(key: string) {
  await act(async () => button(key).click())
  await settle()
}
async function edit(selector: string, value: string) {
  const node = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(selector)!
  expect(node).not.toBeNull()
  const prototype =
    node instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype
  await act(async () => {
    Object.getOwnPropertyDescriptor(prototype, 'value')!.set!.call(node, value)
    node.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function send() {
  await edit('input:not([type=checkbox])', 'Reviewed role')
  await edit('textarea[name=reason]', 'Reviewed definition')
  await click('roleDefinition.reviewSave')
  await click('roleDefinition.confirm')
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
  requests = []
  failures = []
  page = fixture()
  gate = readGate = null
  readFailure = false
  open = ready = true
  epoch = 1
  actor = 'usr_admin'
  target = 'rol_custom'
  returns = 0
  mode = 'edit'
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  cache.setQueryData(['auth', 'session'], session())
  cache.setQueryData(['permissions', actor], ['roles.read'])
  cache.setQueryData(context(), {
    items: [
      {
        id: target,
        name: page.name,
        builtin: false,
        assignment_kind: 'explicit',
        permissions: ['providers.read'],
      },
    ],
  })
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  trigger = document.createElement('button')
  trigger.textContent = 'Original row trigger'
  document.body.append(trigger)
  vi.mocked(getSession).mockImplementation(async () => session(actor))
  vi.mocked(getPermissions).mockResolvedValue(['roles.read'])
  vi.mocked(getRoles).mockImplementation(async () => ({
    items: [
      {
        id: 'rol_custom',
        name: 'Original role',
        builtin: false,
        assignment_kind: 'explicit',
        permissions: ['providers.read'],
      },
      {
        id: 'rol_admin',
        name: 'Admin',
        builtin: true,
        assignment_kind: 'intrinsic',
        permissions: ['roles.read'],
      },
    ],
    available_permissions: ['providers.read'],
  }))
  vi.mocked(getRoleDefinition).mockImplementation(async (_target, signal) => {
    if (readGate) await readGate.promise
    if (signal?.aborted) throw new Error('Aborted read')
    if (readFailure) throw new Error('Controlled read failure')
    return {
      ...page,
      permissions: [...page.permissions],
      available_permissions: [...page.available_permissions],
    }
  })
  vi.mocked(setRoleDefinition).mockImplementation(async (id, etag, input, csrf) => {
    requests.push({ target: id, etag, input: structuredClone(input), csrf })
    if (gate) await gate.promise
    const status = failures.shift()
    if (status) throw { response: { status } }
    return {
      id,
      name: input.name,
      description: input.description,
      permissions: [...input.permissions],
      identity_etag: input.identity_etag,
      etag: changed,
      confirmation: 'current_role_definition',
      effect: 'current_database',
    }
  })
})
afterEach(async () => {
  gate?.release()
  readGate?.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  trigger.remove()
  vi.restoreAllMocks()
})
it('reads only the exact Role and never puts a reviewed draft into a mutation cache', async () => {
  await draw()
  expect(getRoleDefinition).toHaveBeenCalledWith(target, expect.any(AbortSignal))
  expect(document.querySelector('input:not([type=checkbox])')?.getAttribute('value')).toBe(
    page.name,
  )
  expect(cache.getMutationCache().getAll()).toEqual([])
  expect(setRoleDefinition).not.toHaveBeenCalled()
})
it('requires full explicit confirmation and confirms only the exact current stored definition', async () => {
  await draw()
  await edit('input:not([type=checkbox])', 'Reviewed role')
  await edit('textarea[name=reason]', 'Reviewed definition')
  await click('roleDefinition.reviewSave')
  expect(requests).toEqual([])
  expect(document.body.textContent).toContain(label('roleDefinition.confirmHelp'))
  await click('roleDefinition.confirm')
  expect(requests).toEqual([
    {
      target,
      etag: review,
      csrf: 'csrf-first',
      input: {
        name: 'Reviewed role',
        description: 'Original business scope',
        permissions: ['providers.read'],
        identity_etag: identity,
        reason: 'Reviewed definition',
      },
    },
  ])
  expect(cache.getMutationCache().getAll()).toEqual([])
})
it('releases the same-operation busy latch for503→409→success without artificial cache changes', async () => {
  failures = [503, 409]
  await draw()
  await send()
  expect(button('roleDefinition.retry').disabled).toBe(false)
  await click('roleDefinition.retry')
  expect(button('roleDefinition.retry').disabled).toBe(false)
  expect(document.body.textContent).toContain(label('roleDefinition.conflict'))
  await click('roleDefinition.retry')
  expect(requests).toHaveLength(3)
  expect(requests[1]).toEqual(requests[0])
  expect(requests[2]).toEqual(requests[0])
})
it('an initial409 preserves exact uncertain intent; matching GET never reports success', async () => {
  failures = [409]
  await draw()
  await send()
  page = { ...page, name: 'Reviewed role' }
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'role-definition'] })
  })
  await settle()
  expect(button('roleDefinition.retry')).toBeDefined()
  expect(document.body.textContent).not.toContain(label('roleDefinition.confirmed'))
  expect(document.querySelector<HTMLTextAreaElement>('textarea[name=reason]')!.value).toBe(
    'Reviewed definition',
  )
  expect(requests).toHaveLength(1)
})
it('Cancel and same-target reopen retain original birth/review/body through a fresh review', async () => {
  failures = [503, 409]
  await draw()
  await send()
  await click('roleDefinition.cancel')
  expect(document.querySelector('[role=dialog]')).toBeNull()
  page = { ...page, identity_etag: changed, review_etag: changed, name: 'Changed server role' }
  open = true
  epoch++
  await draw()
  await click('roleDefinition.retry')
  expect(requests[1]).toEqual(requests[0])
  expect(document.body.textContent).toContain(label('roleDefinition.conflict'))
})
it('explicit abandonment retains the draft and requires explicit current review before a new request', async () => {
  failures = [503]
  await draw()
  await send()
  await click('roleDefinition.abandon')
  await click('roleDefinition.confirmAbandon')
  expect(document.querySelector<HTMLInputElement>('input:not([type=checkbox])')!.value).toBe(
    'Reviewed role',
  )
  expect(button('roleDefinition.reviewSave').disabled).toBe(true)
  page = { ...page, review_etag: changed }
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'role-definition'] })
  })
  await settle()
  await click('roleDefinition.reviewCurrent')
  await click('roleDefinition.reviewSave')
  await click('roleDefinition.confirm')
  expect(requests[1].etag).toBe(changed)
  expect(requests[1].input).toEqual(requests[0].input)
})
it('fresh CSRF is used while an immutable original tuple survives renewed same-owner Session', async () => {
  failures = [503]
  await draw()
  await send()
  await act(async () => {
    cache.setQueryData(['auth', 'session'], session(actor, 'csrf-renewed'))
  })
  await settle()
  await click('roleDefinition.retry')
  expect(requests[1].csrf).toBe('csrf-renewed')
  expect({ ...requests[1], csrf: requests[0].csrf }).toEqual(requests[0])
})
it('duplicate submits stay locked until a pending request actually settles', async () => {
  gate = pause()
  failures = [503]
  await draw()
  await send()
  expect(requests).toHaveLength(1)
  expect(button('roleDefinition.retry').disabled).toBe(true)
  button('roleDefinition.retry').click()
  expect(requests).toHaveLength(1)
  await act(async () => gate!.release())
  await settle()
  expect(button('roleDefinition.retry').disabled).toBe(false)
})
it.each(['permission', 'list', 'session'] as const)(
  'renewed %s hides all private content and portals synchronously',
  async (kind) => {
    await draw()
    await edit('textarea[name=reason]', 'Draft reason')
    await click('roleDefinition.reviewSave')
    const key =
      kind === 'permission'
        ? ['permissions', actor]
        : kind === 'session'
          ? ['auth', 'session']
          : context()
    const waiting = pause()
    let read!: Promise<unknown>
    await act(async () => {
      read = cache.fetchQuery({
        queryKey: key,
        queryFn: () =>
          waiting.promise.then(() =>
            kind === 'permission'
              ? ['roles.read']
              : kind === 'session'
                ? session()
                : { items: [{ id: target }] },
          ),
      })
    })
    expect(document.querySelector('[role=dialog]')).toBeNull()
    expect(document.body.textContent).not.toContain('Draft reason')
    expect(requests).toHaveLength(0)
    await act(async () => {
      waiting.release()
      await read
    })
    await settle()
  },
)
it('same-millisecond invalidation hides old facts and a late success never resolves its intent', async () => {
  gate = pause()
  await draw()
  await send()
  const clock = vi.spyOn(Date, 'now').mockReturnValue(1780000000000)
  await act(async () => {
    void cache.invalidateQueries({ queryKey: context(), refetchType: 'none' })
  })
  expect(document.querySelector('[role=dialog]')).toBeNull()
  await act(async () => gate!.release())
  await settle()
  await act(async () => {
    cache.setQueryData(context(), { items: [{ id: target }] })
  })
  await draw()
  expect(button('roleDefinition.retry')).toBeDefined()
  expect(document.body.textContent).not.toContain(label('roleDefinition.confirmed'))
  clock.mockRestore()
})
it('read error hides old definition; recovery keeps the original uncertain request', async () => {
  failures = [503]
  await draw()
  await send()
  readFailure = true
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'role-definition'] })
  })
  await settle()
  expect(document.querySelector('textarea[name=reason]')).toBeNull()
  expect(document.body.textContent).not.toContain('Reviewed definition')
  readFailure = false
  await click('roleDefinition.refresh')
  expect(button('roleDefinition.retry')).toBeDefined()
  expect(document.querySelector<HTMLTextAreaElement>('textarea[name=reason]')!.value).toBe(
    'Reviewed definition',
  )
})
it('actor or target changes cannot inherit another scope’s retry intent', async () => {
  failures = [503]
  await draw()
  await send()
  actor = 'usr_other'
  target = 'rol_other'
  page = { ...fixture(), id: target, name: 'Other role' }
  cache.setQueryData(['auth', 'session'], session(actor))
  cache.setQueryData(['permissions', actor], ['roles.read'])
  cache.setQueryData(context(), { items: [{ id: target }] })
  await draw()
  expect(document.querySelector<HTMLInputElement>('input:not([type=checkbox])')!.value).toBe(
    'Other role',
  )
  expect(
    [...document.querySelectorAll('button')].some(
      (node) => node.textContent === label('roleDefinition.retry'),
    ),
  ).toBe(false)
  expect(requests).toHaveLength(1)
})
it('delegated roles.write cannot replace intrinsic admin; unknown recorded aliases require explicit removal', async () => {
  page.permissions = ['projects.models.WRITE', 'providers.read']
  await draw()
  expect(document.body.textContent).toContain('projects.models.WRITE')
  await edit('textarea[name=reason]', 'Reviewed definition')
  expect(button('roleDefinition.reviewSave').disabled).toBe(true)
  const unknown = [...document.querySelectorAll('label')]
    .find((node) => node.textContent?.includes('projects.models.WRITE'))!
    .querySelector('input')!
  await act(async () => unknown.click())
  await settle()
  expect(button('roleDefinition.reviewSave').disabled).toBe(false)
  await act(async () => {
    cache.setQueryData(['auth', 'session'], session(actor, 'csrf-member', 'member'))
    cache.setQueryData(['permissions', actor], ['roles.read', 'roles.write'])
  })
  await settle()
  expect(button('roleDefinition.reviewSave').disabled).toBe(true)
  expect(setRoleDefinition).not.toHaveBeenCalled()
})
it('live Chinese switching preserves name/reason and the immutable original request', async () => {
  failures = [503]
  await draw()
  await send()
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(button('roleDefinition.retry').textContent).toBe('重试原定义请求')
  expect(document.querySelector<HTMLInputElement>('input:not([type=checkbox])')!.value).toBe(
    'Reviewed role',
  )
  await click('roleDefinition.retry')
  expect(requests[1]).toEqual(requests[0])
})

it('builtin fresh View is read-only and exposes no replacement controls', async () => {
  target = 'rol_admin'
  mode = 'view'
  page = { ...fixture(), id: target, builtin: true, assignment_kind: 'intrinsic', can_edit: false }
  cache.setQueryData(context(), { items: [{ id: target, builtin: true }] })
  await draw()
  expect(getRoleDefinition).toHaveBeenCalledWith(target, expect.any(AbortSignal))
  expect(document.body.textContent).toContain('providers.read')
  expect(document.querySelector('textarea[name=reason]')).toBeNull()
  expect(setRoleDefinition).not.toHaveBeenCalled()
})
it('Escape hides the editor without discarding uncertain intent and restores only its connected row trigger', async () => {
  failures = [503]
  await draw()
  await send()
  await act(async () => {
    document
      .querySelector('[role=dialog]')!
      .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  })
  await settle()
  expect(document.querySelector('[role=dialog]')).toBeNull()
  expect(returns).toBeGreaterThan(0)
  open = true
  epoch++
  await draw()
  expect(button('roleDefinition.retry')).toBeDefined()
  trigger.remove()
  await click('roleDefinition.cancel')
  expect(document.activeElement).not.toBe(trigger)
})
it('parent table preserves Create/Delete and scopes custom Edit to fresh GET rather than the legacy public PUT', async () => {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RolesPage />
      </QueryClientProvider>,
    ),
  )
  await settle()
  await settle()
  expect([...document.querySelectorAll('thead th')].map((node) => node.textContent)).toEqual([
    label('common.role'),
    label('common.type'),
    label('roles.members'),
    label('roles.permissions'),
    label('common.actions'),
  ])
  expect(button('roles.create')).toBeDefined()
  const edits = [...document.querySelectorAll('button')].filter(
    (node) => node.textContent === label('roles.edit'),
  )
  const deletes = [...document.querySelectorAll('button')].filter(
    (node) => node.textContent === label('roles.delete'),
  )
  expect(edits).toHaveLength(1)
  expect(deletes).toHaveLength(1)
  await act(async () => edits[0].click())
  await settle()
  expect(getRoleDefinition).toHaveBeenCalledWith('rol_custom', expect.any(AbortSignal))
  expect(setRoleDefinition).not.toHaveBeenCalled()
  expect(document.querySelector('textarea[name=reason]')).not.toBeNull()
})
it('parent hides cached rows/actions/dialogs on read renewal and denies stale dispatch/focus', async () => {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RolesPage />
      </QueryClientProvider>,
    ),
  )
  await settle()
  await settle()
  await click('roles.edit')
  await edit('textarea[name=reason]', 'Private draft')
  const staleSave = button('roleDefinition.reviewSave')
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['permissions', actor], refetchType: 'none' })
  })
  expect(document.querySelector('[role=dialog]')).toBeNull()
  expect(document.querySelector('tbody')?.textContent).not.toContain('Original role')
  await act(async () => staleSave.click())
  expect(setRoleDefinition).not.toHaveBeenCalled()
  expect(document.activeElement?.textContent).not.toBe('Edit')
})

it.each([401, 403])(
  'a dispatched %s denial hides authority but preserves the original intent after fresh recovery',
  async (status) => {
    failures = [status]
    await draw()
    await send()
    expect(document.querySelector('[role=dialog]')).toBeNull()
    await act(async () => {
      cache.setQueryData(['auth', 'session'], session(actor, 'csrf-recovered'))
      cache.setQueryData(['permissions', actor], ['roles.read'])
    })
    await settle()
    expect(button('roleDefinition.retry')).toBeDefined()
    await click('roleDefinition.retry')
    expect(requests[1].input).toEqual(requests[0].input)
    expect(requests[1].etag).toBe(requests[0].etag)
    expect(requests[1].csrf).toBe('csrf-recovered')
  },
)
it('an obsolete Role read cannot restore another target or dispatch from a detached control', async () => {
  readGate = pause()
  await draw()
  target = 'rol_other'
  page = { ...fixture(), id: target, name: 'Other role' }
  cache.setQueryData(context(), { items: [{ id: target }] })
  await draw()
  await act(async () => readGate!.release())
  await settle()
  expect(document.body.textContent).not.toContain('Original role')
  expect(document.querySelector<HTMLInputElement>('input:not([type=checkbox])')!.value).toBe(
    'Other role',
  )
  expect(setRoleDefinition).not.toHaveBeenCalled()
})

async function openWriteConfirmation() {
  await edit('input:not([type=checkbox])', 'Reviewed role')
  await edit('textarea[name=reason]', 'Reviewed definition')
  await click('roleDefinition.reviewSave')
  expect(requests).toEqual([])
}
it('fresh generation changes disable an open confirmation and explain the required review without losing its draft', async () => {
  await draw()
  await openWriteConfirmation()
  expect(button('roleDefinition.confirm').disabled).toBe(false)
  page = { ...page, name: 'Concurrent stored role', review_etag: changed, definition_etag: changed }
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'role-definition'] })
  })
  await settle()
  expect(button('roleDefinition.confirm').disabled).toBe(true)
  expect(document.querySelector('[role=dialog] [role=alert]')?.textContent).toBe(
    label('roleDefinition.reviewChanged'),
  )
  expect(document.querySelector('[role=dialog]')?.textContent).toContain('Reviewed role')
  expect(document.querySelector('[role=dialog]')?.textContent).toContain('Reviewed definition')
  await act(async () => button('roleDefinition.confirm').click())
  expect(requests).toEqual([])
  await click('roleDefinition.cancel')
  expect(document.querySelector<HTMLTextAreaElement>('textarea[name=reason]')!.value).toBe(
    'Reviewed definition',
  )
  expect(button('roleDefinition.reviewSave').disabled).toBe(true)
  await click('roleDefinition.reviewCurrent')
  await click('roleDefinition.reviewSave')
  expect(button('roleDefinition.confirm').disabled).toBe(false)
  await click('roleDefinition.confirm')
  expect(requests).toHaveLength(1)
  expect(requests[0].etag).toBe(changed)
  expect(requests[0].input.name).toBe('Reviewed role')
  expect(requests[0].input.reason).toBe('Reviewed definition')
})
it.each(['builtin', 'writer', 'delegated-writer', 'invalid-draft'] as const)(
  'fresh %s changes disable an open confirmation instead of showing an enabled no-op',
  async (boundary) => {
    await draw()
    await openWriteConfirmation()
    if (boundary === 'builtin') page = { ...page, builtin: true, can_edit: false }
    if (boundary === 'writer') page = { ...page, can_edit: false }
    if (boundary === 'delegated-writer') {
      await act(async () => {
        cache.setQueryData(['auth', 'session'], session(actor, 'csrf-delegated', 'member'))
        cache.setQueryData(['permissions', actor], ['roles.read', 'roles.write'])
      })
    }
    if (boundary === 'invalid-draft') page = { ...page, available_permissions: ['models.read_all'] }
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['admin', 'role-definition'] })
    })
    await settle()
    expect(button('roleDefinition.confirm').disabled).toBe(true)
    await act(async () => button('roleDefinition.confirm').click())
    expect(requests).toEqual([])
  },
)
it('local abandonment remains available after fresh writer loss and does not claim a saved result', async () => {
  failures = [503]
  await draw()
  await send()
  await click('roleDefinition.abandon')
  page = { ...page, can_edit: false, review_etag: changed }
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'role-definition'] })
  })
  await settle()
  expect(button('roleDefinition.confirmAbandon').disabled).toBe(false)
  await click('roleDefinition.confirmAbandon')
  expect(document.querySelector<HTMLTextAreaElement>('textarea[name=reason]')!.value).toBe(
    'Reviewed definition',
  )
  expect(requests).toHaveLength(1)
  expect(document.body.textContent).toContain(label('roleDefinition.abandoned'))
  expect(document.body.textContent).not.toContain(label('roleDefinition.confirmed'))
})

function groupToggle(resource: string) {
  return document.querySelector<HTMLInputElement>(
    `input[aria-label="${i18n.t('roles.selectAll', { ns: 'governance', resource })}"]`,
  )!
}
it('group selection preserves other resources and unknown historical codes through full and partial states', async () => {
  page.permissions = ['legacy.READ', 'models.read_all', 'providers.LEGACY', 'providers.read']
  page.available_permissions = ['models.read_all', 'providers.read', 'providers.write']
  await draw()
  expect(groupToggle('providers')).not.toBeNull()
  expect(groupToggle('providers').checked).toBe(false)
  expect(groupToggle('providers').indeterminate).toBe(true)
  expect(groupToggle('legacy').disabled).toBe(true)
  await act(async () => groupToggle('providers').click())
  expect(groupToggle('providers').checked).toBe(true)
  expect(groupToggle('providers').indeterminate).toBe(false)
  const checked = () =>
    [...document.querySelectorAll<HTMLInputElement>('input[type=checkbox]:checked')].map(
      (node) => node.closest('label')!.textContent,
    )
  expect(checked().join('|')).toContain('providers.LEGACY')
  expect(checked().join('|')).toContain('models.read_all')
  expect(checked().join('|')).toContain('legacy.READ')
  expect(checked().join('|')).toContain('providers.write')
  await act(async () => groupToggle('providers').click())
  expect(groupToggle('providers').checked).toBe(false)
  expect(groupToggle('providers').indeterminate).toBe(false)
  expect(checked().join('|')).toContain('providers.LEGACY')
  expect(checked().join('|')).toContain('models.read_all')
  expect(checked().join('|')).toContain('legacy.READ')
  expect(checked().join('|')).not.toContain('providers.read')
  expect(checked().join('|')).not.toContain('providers.write')
  expect(requests).toEqual([])
})
it('group draft requires review and locks its exact selection through busy and every failed retry', async () => {
  page.available_permissions = ['models.read_all', 'providers.read', 'providers.write']
  await draw()
  await act(async () => groupToggle('providers').click())
  expect(requests).toEqual([])
  failures = [503, 409]
  gate = pause()
  await send()
  expect(requests).toHaveLength(1)
  expect(requests[0].input.permissions).toEqual(['providers.read', 'providers.write'])
  // Dispatch returns to the editor; its retained intent is busy and cannot be edited or replayed.
  expect(groupToggle('providers').matches(':disabled')).toBe(true)
  expect(button('roleDefinition.retry').disabled).toBe(true)
  await act(async () => button('roleDefinition.retry').click())
  expect(requests).toHaveLength(1)
  gate.release()
  gate = null
  await settle()
  expect(groupToggle('providers').matches(':disabled')).toBe(true)
  await act(async () => groupToggle('providers').click())
  await click('roleDefinition.retry')
  expect(groupToggle('providers').matches(':disabled')).toBe(true)
  await click('roleDefinition.retry')
  expect(requests).toHaveLength(3)
  for (const request of requests) {
    expect(request.etag).toBe(review)
    expect(request.input).toEqual(requests[0].input)
  }
})
it('group controls hide during renewed reads and cannot grant delegated writer authority', async () => {
  page.available_permissions = ['models.read_all', 'providers.read', 'providers.write']
  await draw()
  const oldToggle = groupToggle('providers')
  readGate = pause()
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['admin', 'role-definition'] })
  })
  await settle()
  expect(groupToggle('providers')).toBeNull()
  await act(async () => oldToggle.click())
  readGate.release()
  readGate = null
  await settle()
  expect(groupToggle('providers').indeterminate).toBe(true)
  await act(async () => {
    cache.setQueryData(['auth', 'session'], session(actor, 'csrf-delegated', 'member'))
    cache.setQueryData(['permissions', actor], ['roles.read', 'roles.write'])
  })
  await draw()
  expect(groupToggle('providers').matches(':disabled')).toBe(true)
  await act(async () => groupToggle('providers').click())
  expect(requests).toEqual([])
})

it('requires an explicit description for a legacy empty role without inventing recorded text', async () => {
  page.description = ''
  await draw()
  await edit('textarea[name=reason]', 'Reviewed scope')
  expect(button('roleDefinition.reviewSave').disabled).toBe(true)
  const field = document.querySelector<HTMLTextAreaElement>('textarea[name=description]')!
  expect(field.value).toBe('')
  expect(field.rows).toBe(2)
  await edit('textarea[name=description]', 'Business scope\nSecond line')
  expect(button('roleDefinition.reviewSave').disabled).toBe(false)
  await edit('textarea[name=description]', 'é'.repeat(1001))
  expect(button('roleDefinition.reviewSave').disabled).toBe(true)
  await edit('textarea[name=description]', 'Business\tScope')
  expect(button('roleDefinition.reviewSave').disabled).toBe(true)
  expect(setRoleDefinition).not.toHaveBeenCalled()
})

it('captures trimmed multiline description once and retains it through first conflict, changed reads, renewal and language switching', async () => {
  failures = [409, 412]
  await draw()
  await edit('textarea[name=description]', '  Exact business scope\n中文业务  ')
  await send()
  const original = structuredClone(requests[0])
  expect(original.input.description).toBe('Exact business scope\n中文业务')
  page = { ...page, description: 'Different current scope', review_etag: changed }
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'role-definition'] })
    cache.setQueryData(['auth', 'session'], session(actor, 'csrf-current'))
    await i18n.changeLanguage('zh')
  })
  await settle()
  expect(document.querySelector<HTMLTextAreaElement>('textarea[name=description]')!.value).toBe(
    '  Exact business scope\n中文业务  ',
  )
  await click('roleDefinition.retry')
  expect(requests[1]).toEqual({ ...original, csrf: 'csrf-current' })
  expect(button('roleDefinition.retry')).toBeDefined()
  await click('roleDefinition.retry')
  expect(requests[2]).toEqual({ ...original, csrf: 'csrf-current' })
  expect(requests).toHaveLength(3)
})

it('shows recorded builtin absence in both languages without an edit description control', async () => {
  target = 'rol_admin'
  page = {
    ...page,
    id: target,
    builtin: true,
    assignment_kind: 'intrinsic',
    can_edit: false,
    description: '',
  }
  cache.setQueryData(context(), {
    items: [{ id: target, builtin: true, assignment_kind: 'intrinsic' }],
  })
  mode = 'view'
  await draw()
  expect(document.body.textContent).toContain('Not provided')
  expect(document.querySelector('textarea[name=description]')).toBeNull()
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.body.textContent).toContain('未提供')
  expect(setRoleDefinition).not.toHaveBeenCalled()
})

it('preserves recorded FEFF description in name-only edits, confirmation and exact conflict retries', async () => {
  page.description = '\uFEFFRecorded scope\n审批职责\uFEFF'
  const recorded = page.description
  failures = [409, 412]
  await draw()
  expect(document.querySelector<HTMLTextAreaElement>('textarea[name=description]')!.value).toBe(
    recorded,
  )
  await edit('input:not([type=checkbox])', 'Name-only edit')
  await edit('textarea[name=reason]', 'Reviewed name change')
  await click('roleDefinition.reviewSave')
  expect(document.querySelector('[role=dialog]')!.textContent).toContain(recorded)
  await click('roleDefinition.confirm')
  expect(requests).toHaveLength(1)
  expect(requests[0].input.description).toBe(recorded)
  expect(requests[0].input.name).toBe('Name-only edit')
  const original = structuredClone(requests[0])
  page = { ...page, description: 'Different current scope', review_etag: changed }
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'role-definition'] })
    cache.setQueryData(['auth', 'session'], session(actor, 'csrf-renewed'))
    await i18n.changeLanguage('zh')
  })
  await settle()
  expect(document.querySelector<HTMLTextAreaElement>('textarea[name=description]')!.value).toBe(
    recorded,
  )
  expect(
    document
      .querySelector<HTMLTextAreaElement>('textarea[name=description]')!
      .getAttribute('aria-label'),
  ).toBe('角色说明')
  await click('roleDefinition.retry')
  await click('roleDefinition.retry')
  expect(requests).toHaveLength(3)
  for (const request of requests.slice(1))
    expect(request).toEqual({ ...original, csrf: 'csrf-renewed' })
})
it('trims only Go whitespace from a description draft while preserving boundary FEFF and LF', async () => {
  await draw()
  const captured = '\uFEFFScope\n审批职责\uFEFF'
  await edit('textarea[name=description]', ' \u00a0' + captured + '\u3000 ')
  await send()
  expect(requests[0].input.description).toBe(captured)
})

it.each([
  ['rol_procurement', 'Procurement', '采购'],
  ['rol_finance', 'Finance', '财务'],
  ['rol_operations', 'Operations', '运维'],
])(
  'localizes the immutable %s view title live without translating recorded description or issuing writes',
  async (id, name, translated) => {
    target = id
    mode = 'view'
    page = { ...fixture(), id, name, builtin: true, assignment_kind: 'explicit', can_edit: false }
    cache.setQueryData(context(), { items: [{ ...page }] })
    await draw()
    await vi.waitFor(() =>
      expect(document.querySelector('[role="dialog"] h2')?.textContent).toBe(`${name} permissions`),
    )
    expect(document.body.textContent).toContain('Original business scope')
    const reads = vi.mocked(getRoleDefinition).mock.calls.length
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.querySelector('[role="dialog"] h2')?.textContent).toBe(`${translated}权限`)
    expect(document.body.textContent).toContain('Original business scope')
    expect(document.querySelector('[role="dialog"] textarea')).toBeNull()
    expect(vi.mocked(getRoleDefinition).mock.calls).toHaveLength(reads)
    expect(requests).toHaveLength(0)
    await act(async () => i18n.changeLanguage('en'))
    expect(document.querySelector('[role="dialog"] h2')?.textContent).toBe(`${name} permissions`)
  },
)

it('keeps the recorded same-named custom Role view title when language changes', async () => {
  mode = 'view'
  page = { ...fixture(), name: 'Finance' }
  await draw()
  await vi.waitFor(() =>
    expect(document.querySelector('[role="dialog"] h2')?.textContent).toBe('Finance permissions'),
  )
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.querySelector('[role="dialog"] h2')?.textContent).toBe('Finance权限')
  expect(requests).toHaveLength(0)
})
