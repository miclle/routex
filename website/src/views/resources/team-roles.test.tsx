import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, describe, it, expect } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import ResourceDetailPage from './detail'
import { teamRolesFixture, roleFixture } from './team-role-fixture'
import type { TeamRole, TeamRoles } from '@/types/team-roles'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let host: HTMLDivElement,
  root: Root,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>,
  requests: InternalAxiosRequestConfig[]
let actor: string,
  csrf: string,
  identityRole: 'member' | 'admin',
  permissions: string[],
  projection: TeamRoles,
  catalogue: TeamRole[],
  failure: Record<string, number>,
  hold: Promise<void> | undefined,
  roleReadHold: Promise<void> | undefined,
  malformed: boolean,
  status: string,
  minimal: boolean
const original = client.defaults.adapter
const session = () => ({
  user: { id: actor, name: 'Member', email: 'member@example.test', role: identityRole },
  csrf_token: csrf,
})
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 60000 }, mutations: { retry: false } },
  })
  requests = []
  actor = 'usr_member'
  csrf = 'csrf-old'
  identityRole = 'member'
  permissions = []
  projection = teamRolesFixture()
  catalogue = [roleFixture(), roleFixture('rol_models')]
  failure = {}
  hold = undefined
  roleReadHold = undefined
  malformed = false
  status = 'active'
  minimal = false
  cache.setQueryData(['auth', 'session'], session())
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      data: {} as unknown,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
    }
    if (config.method === 'put' && hold) await hold
    if (config.method === 'get' && config.url === '/teams/tea_one/roles' && roleReadHold)
      await roleReadHold
    if (failure[`${config.method} ${config.url}`]) {
      response.status = failure[`${config.method} ${config.url}`]
      throw new AxiosError('Fixture failure', '', config, undefined, response)
    }
    if (config.url === '/auth/session') response.data = session()
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/teams/tea_one')
      response.data = {
        id: 'tea_one',
        name: 'Research Team',
        description: 'Evaluation',
        status,
        resource_limit_workspace_only: minimal,
        model_ids: [],
        members: [
          {
            id: 'tmm_one',
            user_id: actor,
            name: 'Member',
            email: 'member@example.test',
            role: 'owner',
            status: 'active',
          },
        ],
        created_at: '2026-10-03T00:00:00Z',
      }
    else if (config.url === '/teams/tea_one/roles') {
      if (config.method === 'put') {
        const body = JSON.parse(config.data)
        const roles = catalogue.filter((role) => body.role_ids.includes(role.id))
        projection = {
          ...projection,
          role_ids: body.role_ids,
          roles,
          effective_team_actions: [...new Set(roles.flatMap((role) => role.team_actions))],
          etag: 'b'.repeat(64),
        }
      }
      response.data =
        malformed && config.method === 'put' ? { team_id: 'tea_one' } : structuredClone(projection)
    } else if (config.url === '/teams/tea_one/role-candidates')
      response.data = {
        items: catalogue.filter((role) =>
          role.name.toLowerCase().includes((config.params.query ?? '').toLowerCase()),
        ),
        next_cursor: null,
        etag: projection.etag,
      }
    else if (config.url === '/teams/tea_one/model-candidates')
      response.data = { items: [{ id: 'mdl_new', name: 'New model' }] }
    else if (config.url === '/teams/tea_one/member-candidates')
      response.data = { items: [{ id: 'usr_new', name: 'New member', email: 'new@example.test' }] }
    else if (config.url?.endsWith('/limits')) response.data = {}
    else if (config.url === '/admin/teams/tea_one' && config.method === 'patch')
      response.data = { id: 'tea_one', name: 'Research Team' }
    else throw new Error(`Unexpected fixture request ${config.url}`)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = original
})
async function until(assert: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
async function mount(tab = 'roles') {
  cache.setQueryData(['auth', 'session'], session())
  router = createMemoryRouter(
    [{ path: '/teams/:resourceId', element: <ResourceDetailPage kind="teams" /> }],
    { initialEntries: [`/teams/tea_one?tab=${tab}`] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain('Research Team'))
}
function button(label: string) {
  const result = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label,
  )
  expect(result, label).toBeDefined()
  return result!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function fill(label: string, value: string) {
  const el = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(
    `[aria-label="${label}"]`,
  )!
  expect(el, label).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      el instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype,
      'value',
    )!.set!.call(el, value)
    el.dispatchEvent(
      new Event(el instanceof HTMLTextAreaElement ? 'change' : 'input', { bubbles: true }),
    )
    if (el instanceof HTMLTextAreaElement) el.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function add() {
  await until(() => expect(host.textContent).toContain('Model maintainer'))
  const input = [...host.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')].find((el) =>
    el.parentElement?.textContent?.includes('Model maintainer'),
  )!
  await act(async () => input.click())
  await click('Add')
  await fill('Change reason', 'Reviewed Team roles')
}
const puts = () => requests.filter((request) => request.method === 'put')
describe('Team role workspace', () => {
  it('reproduces compact assigned roles and effective union without leaking global role permissions', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('Team editor'))
    expect(
      [...host.querySelectorAll('[role="tab"]')].map((tab) => tab.textContent).slice(0, 4),
    ).toEqual(['Overview', 'Members', 'Roles and permissions', 'Model access'])
    expect(host.textContent).toContain('Effective Team permissions')
    expect(host.textContent).not.toContain('Change reason')
    expect(requests.some((request) => request.url?.includes('/role-candidates'))).toBe(false)
    await click('View permissions')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      'Edit this Team and its members',
    )
    expect(cache.getQueryData(['permissions', actor])).toEqual([])
  })
  it('owner identity alone cannot assign roles even when scoped Team editing is allowed', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('Team editor'))
    expect(host.querySelector('input[aria-label="Search roles"]')).toBeNull()
    expect(puts()).toHaveLength(0)
  })
  it('requires actual administrator identity as well as server assignment authority', async () => {
    projection.can_assign_roles = true
    await mount()
    await until(() => expect(host.textContent).toContain('Team editor'))
    expect(requests.some((request) => request.url?.includes('/role-candidates'))).toBe(false)
  })
  it('saves one reviewed complete assignment and reason without claiming runtime publication', async () => {
    identityRole = 'admin'
    projection.can_assign_roles = true
    await mount()
    await add()
    await click('Save Team roles')
    await until(() => expect(host.textContent).toContain('Current Team role assignment is saved'))
    expect(JSON.parse(puts()[0].data)).toEqual({
      role_ids: ['rol_editor', 'rol_models'],
      reason: 'Reviewed Team roles',
    })
    expect(puts()[0].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
    expect(cache.getQueryData(['permissions', actor])).toEqual([])
    expect(requests.some((request) => request.url === '/admin/roles')).toBe(false)
  })
  it('refreshes candidate definitions after saving so another assignment remains available', async () => {
    identityRole = 'admin'
    projection.can_assign_roles = true
    catalogue.push({ ...roleFixture('rol_second'), name: 'Second maintainer' })
    await mount()
    await add()
    await click('Save Team roles')
    await until(() => {
      expect(host.textContent).toContain('Current Team role assignment is saved')
      const option = [...host.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')].find(
        (input) => input.parentElement?.textContent?.includes('Second maintainer'),
      )
      expect(option).toBeDefined()
      expect(option?.closest('fieldset')?.disabled).toBe(false)
      expect(host.textContent).not.toContain('role definitions changed')
    })
    const option = [...host.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')].find(
      (input) => input.parentElement?.textContent?.includes('Second maintainer'),
    )!
    await act(async () => option.click())
    await click('Add')
    await fill('Change reason', 'Reviewed second assignment')
    await click('Save Team roles')
    await until(() => expect(puts()).toHaveLength(2))
    expect(puts()[1].headers.get('If-Match')).toBe(`"${'b'.repeat(64)}"`)
    expect(JSON.parse(puts()[1].data).role_ids).toContain('rol_second')
  })
  it('retains candidate selections and known definitions outside the next bounded search response', async () => {
    identityRole = 'admin'
    projection.can_assign_roles = true
    await mount()
    await add()
    await fill('Search roles', 'absent')
    await until(() => expect(host.querySelectorAll('input[type="checkbox"]')).toHaveLength(0))
    expect(host.querySelector('table')?.textContent).toContain('Model maintainer')
    await click('Save Team roles')
    await until(() => expect(puts()).toHaveLength(1))
  })
  it('preserves unknown original body/header through conflict and same-actor CSRF rotation', async () => {
    identityRole = 'admin'
    projection.can_assign_roles = true
    failure['put /teams/tea_one/roles'] = 503
    await mount()
    await add()
    await click('Save Team roles')
    await until(() => expect(host.textContent).toContain('assignment may already be saved'))
    csrf = 'csrf-new'
    await act(async () => cache.setQueryData(['auth', 'session'], session()))
    failure['put /teams/tea_one/roles'] = 409
    await click('Retry exact assignment')
    await until(() => expect(puts()).toHaveLength(2))
    expect(puts()[1].data).toBe(puts()[0].data)
    expect(puts()[1].headers.get('If-Match')).toBe(puts()[0].headers.get('If-Match'))
    expect(puts()[1].headers.get('X-CSRF-Token')).toBe('csrf-new')
    expect(host.textContent).not.toContain('Current Team role assignment is saved')
    expect(button('Save Team roles').disabled).toBe(true)
  })
  it('requires explicit fresh definition review after a definitive conflict', async () => {
    identityRole = 'admin'
    projection.can_assign_roles = true
    failure['put /teams/tea_one/roles'] = 409
    await mount()
    await add()
    await click('Save Team roles')
    await until(() => expect(host.textContent).toContain('role definitions changed'))
    projection.etag = 'c'.repeat(64)
    catalogue[1] = { ...catalogue[1], team_actions: ['teams.write'] }
    await click('Refresh roles and actions')
    await until(() => expect(button('Use reviewed role definitions').disabled).toBe(false))
    expect(button('Save Team roles').disabled).toBe(true)
    expect(document.querySelector<HTMLTextAreaElement>('[aria-label="Change reason"]')?.value).toBe(
      'Reviewed Team roles',
    )
    await click('Use reviewed role definitions')
    delete failure['put /teams/tea_one/roles']
    await click('Save Team roles')
    await until(() => expect(puts()).toHaveLength(2))
    expect(puts()[1].headers.get('If-Match')).toBe(`"${'c'.repeat(64)}"`)
  })
  it('matching current IDs after definition change cannot resolve an unknown historical operation', async () => {
    identityRole = 'admin'
    projection.can_assign_roles = true
    failure['put /teams/tea_one/roles'] = 503
    await mount()
    await add()
    await click('Save Team roles')
    await until(() => expect(host.textContent).toContain('assignment may already be saved'))
    catalogue[1] = { ...catalogue[1], team_actions: ['teams.write'] }
    projection = {
      ...projection,
      role_ids: ['rol_editor', 'rol_models'],
      roles: [...catalogue],
      effective_team_actions: ['teams.write'],
      etag: 'c'.repeat(64),
    }
    await click('Refresh roles and actions')
    await until(() =>
      expect(host.querySelector('table')?.textContent).toContain('Model maintainer'),
    )
    expect(host.textContent).not.toContain('Current Team role assignment is saved')
    expect(button('Save Team roles').disabled).toBe(true)
    expect(
      [...host.querySelectorAll('button')].some(
        (item) => item.textContent === 'Use reviewed role definitions',
      ),
    ).toBe(false)
    failure['put /teams/tea_one/roles'] = 409
    await click('Retry exact assignment')
    await until(() => expect(puts()).toHaveLength(2))
    expect(puts()[1].headers.get('If-Match')).toBe(puts()[0].headers.get('If-Match'))
    expect(puts()[1].data).toBe(puts()[0].data)
    await until(() => expect(button('Review current assignment').disabled).toBe(false))
    await click('Review current assignment')
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain('rol_models')
    expect(dialog.textContent).toContain('Edit this Team and its members')
    expect(dialog.textContent).toContain('original operation outcome remains unknown')
    expect(puts()).toHaveLength(2)
    await click('Use current assignment and discard draft')
    expect(host.textContent).toContain('The local draft was discarded')
    expect(host.textContent).not.toContain('Current Team role assignment is saved')
    expect(host.querySelector('[aria-label="Change reason"]')).toBeNull()
    expect(puts()).toHaveLength(2)
    delete failure['put /teams/tea_one/roles']
    await click('Remove')
    await fill('Change reason', 'Fresh reviewed assignment')
    await click('Save Team roles')
    await until(() => expect(puts()).toHaveLength(3))
    expect(puts()[2].headers.get('If-Match')).toBe(`"${'c'.repeat(64)}"`)
    expect(JSON.parse(puts()[2].data).reason).toBe('Fresh reviewed assignment')
  })
  it('reads a new authoritative assignment before opening unknown-intent discard review', async () => {
    identityRole = 'admin'
    projection.can_assign_roles = true
    failure['put /teams/tea_one/roles'] = 503
    await mount()
    await add()
    await click('Save Team roles')
    await until(() => expect(host.textContent).toContain('assignment may already be saved'))
    expect(cache.getQueryData<TeamRoles>(['team-roles', actor, 'tea_one'])?.role_ids).toEqual([
      'rol_editor',
    ])
    // The write reached the server, but its response was lost and the local cache is still old.
    projection = {
      ...projection,
      role_ids: ['rol_editor', 'rol_models'],
      roles: [...catalogue],
      effective_team_actions: ['teams.write', 'teams.models.write'],
      etag: 'c'.repeat(64),
    }
    let release!: () => void
    roleReadHold = new Promise<void>((resolve) => {
      release = resolve
    })
    const before = requests.filter(
      (request) => request.method === 'get' && request.url === '/teams/tea_one/roles',
    ).length
    await click('Review current assignment')
    expect(
      requests.filter(
        (request) => request.method === 'get' && request.url === '/teams/tea_one/roles',
      ),
    ).toHaveLength(before + 1)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(puts()).toHaveLength(1)
    await act(async () => release())
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain('rol_models'),
    )
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      'Maintain this Team’s model access',
    )
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      'original operation outcome remains unknown',
    )
    await click('Use current assignment and discard draft')
    expect(host.textContent).toContain('The local draft was discarded')
    expect(host.textContent).not.toContain('Current Team role assignment is saved')
    expect(puts()).toHaveLength(1)
  })
  it('inherited metadata editing never enables lifecycle changes', async () => {
    await mount('settings')
    await until(() =>
      expect(host.querySelector<HTMLInputElement>('input[name="name"]')?.disabled).toBe(false),
    )
    expect(host.querySelector<HTMLSelectElement>('select[name="status"]')?.disabled).toBe(true)
    expect(cache.getQueryData(['permissions', actor])).toEqual([])
  })
  it('the minimal quota workspace does not request Team role or candidate facts', async () => {
    minimal = true
    await mount('roles')
    await until(() =>
      expect(requests.some((request) => request.url?.endsWith('/limits'))).toBe(true),
    )
    expect(
      requests.some(
        (request) => request.url?.endsWith('/roles') || request.url?.includes('/role-candidates'),
      ),
    ).toBe(false)
    expect(
      [...host.querySelectorAll('[role="tab"]')].some(
        (tab) => tab.textContent === 'Roles and permissions',
      ),
    ).toBe(false)
  })
  it('treats malformed 200 as an unknown result and preserves the original retry', async () => {
    identityRole = 'admin'
    projection.can_assign_roles = true
    malformed = true
    await mount()
    await add()
    await click('Save Team roles')
    await until(() => expect(host.textContent).toContain('assignment may already be saved'))
    expect(host.textContent).not.toContain('Current Team role assignment is saved')
    expect(puts()).toHaveLength(1)
  })
  it('refresh denial hides old role rows and scoped action controls', async () => {
    identityRole = 'admin'
    projection.can_assign_roles = true
    await mount()
    await until(() => expect(host.textContent).toContain('Team editor'))
    failure['get /teams/tea_one/roles'] = 404
    await click('Refresh roles and actions')
    await until(() => expect(host.textContent).not.toContain('Team editor'))
    expect(host.textContent).not.toContain('Effective Team permissions')
    expect(host.querySelector('input[aria-label="Search roles"]')).toBeNull()
  })
  it('live Chinese switching retains transient reason and selected roles', async () => {
    identityRole = 'admin'
    projection.can_assign_roles = true
    await mount()
    await add()
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain('团队有效权限')
    expect(document.querySelector<HTMLTextAreaElement>('[aria-label="变更理由"]')?.value).toBe(
      'Reviewed Team roles',
    )
    expect(host.querySelector('table')?.textContent).toContain('Model maintainer')
  })
  it('Team-scoped model maintenance uses only the target candidate endpoint', async () => {
    projection.actor_team_actions = ['teams.models.write']
    await mount('models')
    await until(() => expect(host.textContent).toContain('New model'))
    expect(requests.some((request) => request.url === '/teams/tea_one/model-candidates')).toBe(true)
    expect(
      requests.some((request) => request.url?.includes('/admin/resource-model-candidates')),
    ).toBe(false)
    expect(cache.getQueryData(['permissions', actor])).toEqual([])
  })
  it('Team edit enables member candidates but does not enable platform lifecycle controls', async () => {
    await mount('members')
    await until(() => expect(button('Add member').disabled).toBe(false))
    await click('Add member')
    await until(() => expect(document.body.textContent).toContain('New member'))
    expect(requests.some((request) => request.url === '/teams/tea_one/member-candidates')).toBe(
      true,
    )
    expect(requests.some((request) => request.url === '/admin/team-member-candidates')).toBe(false)
  })
  it('inactive and minimal limit workspaces never fetch scoped role facts', async () => {
    status = 'disabled'
    await mount('settings')
    expect(requests.some((request) => request.url?.endsWith('/roles'))).toBe(false)
    expect(
      [...host.querySelectorAll('[role="tab"]')].some(
        (tab) => tab.textContent === 'Roles and permissions',
      ),
    ).toBe(false)
  })
  it('late assignment completion after actor replacement cannot repopulate old private state', async () => {
    identityRole = 'admin'
    projection.can_assign_roles = true
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await mount()
    await add()
    await click('Save Team roles')
    await until(() => expect(puts()).toHaveLength(1))
    actor = 'usr_other'
    await act(async () => cache.setQueryData(['auth', 'session'], session()))
    cache.removeQueries({ queryKey: ['team-roles', 'usr_member'] })
    await act(async () => release())
    await until(() =>
      expect(cache.getQueryData(['team-roles', 'usr_member', 'tea_one'])).toBeUndefined(),
    )
    expect(host.textContent).not.toContain('Current Team role assignment is saved')
  })
})
