import {
  memberStateFixture,
  memberStateResultFixture,
  stateReviewETag,
} from './member-state.fixture'
import { rolesWorkspace, roleSummary } from './member-roles.fixture'
import { accessSummaryFixture } from './member-access-summary.fixture'
import { memberListPage, memberListRow } from './member-list.fixture'
import { limitFixture } from '@/views/resource-limits/fixture'
import { act, StrictMode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { MutationCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type AxiosAdapter, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import MembersPage from './members'
import RolesPage from './roles'
import RegistrationPage from './registration'
import AuthPage from '@/views/auth'
import AppShell from '@/components/app/AppShell'
import ProvidersPage from '@/views/providers'
import type { Member, PlatformRole } from '@/types/governance'
import type { Session } from '@/types/auth'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root
let container: HTMLDivElement
let cache: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[]
let permissions: string[]
let session: Session
let target: Member
let roles: PlatformRole[]
let roleETag: string
let registrationETag: string
let registration: boolean
let failures: Record<string, number>
const oldAdapter = client.defaults.adapter
const roleDefinitionAvailablePermissions = [
  'announcements.write',
  'audit.read',
  'calls.read_all',
  'egress.read',
  'egress.test',
  'egress.write',
  'limits.settings.write',
  'limits.users.write',
  'members.keys.disable',
  'members.models.write',
  'members.read',
  'members.write',
  'models.read_all',
  'models.write',
  'prices.read',
  'prices.write',
  'projects.limits.write',
  'projects.models.write',
  'projects.read_all',
  'projects.write',
  'providers.read',
  'providers.write',
  'roles.read',
  'secrets.read',
  'secrets.rotate',
  'site.write',
  'smtp.read',
  'smtp.test',
  'smtp.write',
  'storage.read',
  'storage.test',
  'storage.write',
  'system.read',
  'system.write',
  'teams.models.write',
  'teams.money.write',
  'teams.quota_requests.read_all',
  'teams.rates.write',
  'teams.read_all',
  'teams.tokens.write',
  'teams.write',
]
beforeEach(() => {
  requests = []
  failures = {}
  roleETag = 'a'.repeat(64)
  registration = false
  registrationETag = 'a'.repeat(64)
  permissions = [
    'members.read',
    'members.write',
    'roles.read',
    'roles.write',
    'registration.write',
    'providers.read',
    'providers.write',
  ]
  session = {
    user: { id: 'usr_admin', name: 'Admin', email: 'admin@example.invalid', role: 'admin' },
    csrf_token: 'csrf',
  }
  target = {
    id: 'usr_target',
    name: 'Target',
    email: 'target@example.invalid',
    role: 'member',
    disabled: false,
    created_at: '2026-09-23T00:00:00Z',
    role_ids: ['rol_custom'],
  }
  roles = [
    {
      id: 'rol_admin',
      name: 'Administrator',
      description: '',
      builtin: true,
      assignment_kind: 'intrinsic',
      permissions,
    },
    {
      id: 'rol_member',
      name: 'Member',
      description: '',
      builtin: true,
      assignment_kind: 'intrinsic',
      permissions: [],
    },
    {
      id: 'rol_custom',
      name: 'Provider Reader',
      description: 'Read provider records',
      builtin: false,
      assignment_kind: 'explicit',
      permissions: ['providers.read'],
    },
  ]
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
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
      data.identity_role = target.role
      if (data.roles.status === 'available')
        data.roles.items = roles
          .filter((role) => target.role_ids.includes(role.id))
          .map(({ id, name, builtin }) => ({ id, name, builtin }))
          .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
      if (data.teams.status === 'available') data.teams.items = []
      return {
        config: config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders({ 'cache-control': 'private, no-store' }),
        data,
      }
    }
    const key = `${config.method} ${config.url}`
    const response = {
      config,
      status: failures[key] ?? 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (response.status >= 400) throw new AxiosError('Failure', '', config, undefined, response)
    if (key === 'get /notifications') response.data = { items: [], unread_count: 0 }
    if (key === 'get /auth/session') response.data = structuredClone(session)
    if (key === 'get /auth/permissions') response.data = { permissions: [...permissions] }
    if (key === 'get /admin/members')
      response.data = memberListPage(session.user.id, [memberListRow(structuredClone(target))])
    if (key === 'get /admin/members/usr_target')
      response.data = {
        ...structuredClone(target),
        offboarded_at: target.offboarded_at ?? null,
        registration_approval: { status: 'not_required', admission_eligible: false },
        last_login_at: null,
        last_login_status: 'historical_unavailable',
      }
    if (key === 'get /admin/members/usr_target/offboarding')
      response.data = {
        user_id: target.id,
        disabled: target.disabled,
        offboarded_at: target.offboarded_at ?? null,
        last_administrator: false,
        inventory_version: 'a'.repeat(64),
        personal_keys: [],
        projects: [],
        teams: [],
        cases: [],
      }
    if (key === 'get /admin/members/usr_target/state') {
      response.data = memberStateFixture(target, session.user.id, session.user.role, permissions)
      response.headers = new AxiosHeaders({
        etag: `"${stateReviewETag}"`,
        'cache-control': 'private, no-store',
      })
    }
    if (key === 'get /admin/members/usr_target/roles') {
      const page = rolesWorkspace(target.id)
      page.etag = roleETag
      page.identity_role = target.role
      page.subject_status = target.offboarded_at
        ? 'offboarded'
        : target.disabled
          ? 'disabled'
          : 'active'
      page.permission_use = page.subject_status === 'active' ? 'active' : 'inactive'
      const summarize = (role: PlatformRole) => ({
        ...roleSummary(role.id, role.name),
        builtin: role.builtin,
        assignment_kind: role.assignment_kind,
        permission_count: role.permissions.length,
      })
      page.builtin_role = summarize(roles.find((role) => role.id === `rol_${target.role}`)!)
      page.assigned_roles = roles
        .filter((role) => target.role_ids.includes(role.id))
        .map(summarize)
        .sort((a, b) => (a.id < b.id ? -1 : 1))
      page.effective_permissions = [
        ...new Set(
          roles
            .filter((role) => role.id === page.builtin_role.id || target.role_ids.includes(role.id))
            .flatMap((role) => role.permissions),
        ),
      ].sort()
      response.data = page
      response.headers = new AxiosHeaders({
        etag: `"${roleETag}"`,
        'cache-control': 'private, no-store',
      })
    }
    if (key === 'get /admin/members/usr_target/roles/candidates') {
      response.data = {
        items: roles
          .filter((role) => !role.builtin)
          .map((role) => ({
            ...roleSummary(role.id, role.name),
            permission_count: role.permissions.length,
          }))
          .sort((a, b) => (a.id < b.id ? -1 : 1)),
        next_cursor: null,
        etag: roleETag,
      }
      response.headers = new AxiosHeaders({
        etag: `"${roleETag}"`,
        'cache-control': 'private, no-store',
      })
    }
    if (config.method === 'get' && /^\/admin\/roles\/[^/]+$/.test(config.url ?? '')) {
      const id = config.url!.split('/')[3]
      const role = roles.find((row) => row.id === id)
      if (!role)
        throw new AxiosError('Missing exact Role', '', config, undefined, {
          ...response,
          status: 404,
        })
      response.data = {
        id: role.id,
        name: role.name,
        description: role.description ?? '',
        builtin: role.builtin,
        assignment_kind: role.assignment_kind,
        permissions: [...role.permissions].sort(),
        available_permissions: [...roleDefinitionAvailablePermissions],
        definition_etag: roleETag,
        identity_etag: 'b'.repeat(64),
        review_etag: roleETag,
        can_edit: !role.builtin && session.user.role === 'admin',
      }
      response.headers = new AxiosHeaders({
        etag: `"${roleETag}"`,
        'cache-control': 'private, no-store',
      })
    }
    if (key === 'get /admin/roles')
      response.data = {
        items: structuredClone(roles),
        available_permissions: ['providers.read', 'providers.write', 'members.read'],
      }
    if (key === 'get /auth/registration')
      response.data = { enabled: registration, approval_required: false, allowed_email_domains: [] }
    if (key === 'get /admin/registration') {
      response.data = {
        enabled: registration,
        approval_required: false,
        allowed_email_domains: [],
        review_etag: registrationETag,
      }
      response.headers.set('ETag', `"${registrationETag}"`)
    }
    if (key === 'patch /admin/registration') {
      const input = JSON.parse(config.data)
      expect(input).toEqual({
        enabled: true,
        approval_required: false,
        allowed_email_domains: [],
        reason: 'Reviewed new registration policy',
      })
      expect(config.headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
      registration = input.enabled
      registrationETag = 'b'.repeat(64)
      response.data = {
        confirmation: 'current_registration_policy',
        enabled: registration,
        approval_required: false,
        allowed_email_domains: [],
        review_etag: 'b'.repeat(64),
      }
      response.headers.set('ETag', `"${'b'.repeat(64)}"`)
    }
    if (key === 'patch /admin/members/usr_target') {
      const input = JSON.parse(config.data)
      Object.assign(
        target,
        Object.hasOwn(input, 'role') ? { role: input.role } : { disabled: input.disabled },
      )
      response.data = memberStateResultFixture(
        memberStateFixture(target, session.user.id, session.user.role, permissions),
        JSON.parse(config.data),
      )
      response.headers = new AxiosHeaders({
        etag: `"${stateReviewETag}"`,
        'cache-control': 'private, no-store',
      })
    }
    if (key === 'put /admin/members/usr_target/roles') {
      target.role_ids = JSON.parse(config.data).role_ids
      roleETag = 'c'.repeat(64)
      response.data = {
        user_id: target.id,
        role_ids: [...target.role_ids],
        etag: roleETag,
        confirmation: 'current_member_roles',
        effect: 'current_database',
      }
      response.headers = new AxiosHeaders({
        etag: `"${roleETag}"`,
        'cache-control': 'private, no-store',
      })
    }
    if (key === 'post /admin/members') {
      const data = JSON.parse(config.data)
      target = { ...target, name: data.name, email: data.email, role: data.role }
      response.data = structuredClone(target)
    }
    if (key === 'post /admin/roles') {
      roles.push({
        ...JSON.parse(config.data),
        id: 'rol_new',
        builtin: false,
        assignment_kind: 'explicit',
      })
      response.data = structuredClone(roles.at(-1))
    }
    if (key === 'post /auth/register') {
      response.status = 201
      const data = JSON.parse(config.data)
      session = {
        user: { id: target.id, name: data.name, email: data.email, role: 'member' },
        csrf_token: 'registered-csrf',
      }
      response.data = structuredClone(session)
    }
    if (key === 'get /admin/providers')
      response.data = { items: [{ id: 'prv_1', name: 'Provider', connections: [] }] }
    if (config.url?.endsWith('/limits'))
      response.data = { ...limitFixture(), id: config.url.split('/')[3] }
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  client.defaults.adapter = oldAdapter
  container.remove()
})
async function until(assert: () => void) {
  for (let i = 0; i < 70; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 69) throw error
    }
  }
}
async function mount(path: string, strict = false) {
  router = createMemoryRouter(
    [
      { path: '/admin/members', element: <MembersPage /> },
      { path: '/admin/members/:memberId', element: <MembersPage /> },
      { path: '/admin/roles', element: <RolesPage /> },
      { path: '/admin/auth', element: <RegistrationPage /> },
      { path: '/login', element: <AuthPage mode="login" /> },
      { path: '/register', element: <AuthPage mode="register" /> },
      {
        path: '/',
        element: <AppShell />,
        children: [
          { index: true, element: <p>Signed in workspace</p> },
          { path: 'admin/providers', element: <ProvidersPage /> },
        ],
      },
    ],
    { initialEntries: [path] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        {strict ? (
          <StrictMode>
            <RouterProvider router={router} />
          </StrictMode>
        ) : (
          <RouterProvider router={router} />
        )}
      </QueryClientProvider>,
    ),
  )
}
async function click(text: string) {
  await act(async () => {
    const button = [...document.querySelectorAll('button')].find(
      (item) => item.textContent === text,
    )
    expect(button).toBeDefined()
    button!.click()
  })
}
async function fill(name: string, value: string) {
  await act(async () => {
    const input = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(
      `input[name="${name}"], textarea[name="${name}"]`,
    )!
    expect(input).not.toBeNull()
    Object.getOwnPropertyDescriptor(
      input instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype,
      'value',
    )!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function submit(label?: string) {
  await act(async () => {
    const form = document.querySelector(
      label ? `form[aria-label="${label}"]` : '[role="dialog"] form',
    )!
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}

describe('member governance', () => {
  it('requires limits.users.write for member aggregate changes, independently of members.write', async () => {
    permissions = ['members.read', 'members.write']
    await mount('/admin/members/usr_target?tab=limits')
    await until(() => expect(container.textContent).toContain('user_usr_fixture'))
    expect(container.textContent).not.toContain('Edit limits')
    permissions = ['members.read', 'limits.users.write']
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['permissions'] })
    })
    await until(() => expect(container.textContent).toContain('Edit limits'))
    await click('Edit limits')
    expect(container.querySelector('form[aria-label="Proposed policy"]')).not.toBeNull()
  })

  it('distinguishes completed offboarding from suspension and offers the review entry', async () => {
    target.disabled = true
    target.offboarded_at = '2026-09-23T01:00:00Z'
    await mount('/admin/members/usr_target?tab=settings')
    await until(() => expect(container.textContent).toContain('Offboarded'))
    await until(() =>
      expect(
        [...container.querySelectorAll('button')].some(
          (button) => button.textContent === 'Review offboarding',
        ),
      ).toBe(true),
    )
    expect(container.textContent).not.toContain('Edit limits')
    expect(
      [...container.querySelectorAll('button')].some(
        (button) => button.textContent === 'Review offboarding',
      ),
    ).toBe(true)
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(container.textContent).toContain('已离职')
    expect(container.textContent).toContain('查看离职交接')
  })

  it('does not fetch member data when effective permissions deny access', async () => {
    permissions = []
    await mount('/admin/members')
    await until(() =>
      expect(container.textContent).toContain(
        'Your account does not have permission to access this page.',
      ),
    )
    expect(requests.some((r) => r.url === '/admin/members')).toBe(false)
  })
  it('uses effective permissions for delegated admin navigation and read-only catalog access', async () => {
    session.user.role = 'member'
    permissions = ['providers.read']
    await mount('/admin/providers')
    await until(() => expect(container.querySelector('nav')?.textContent).toContain('Providers'))
    expect(container.querySelector('nav')?.textContent).not.toContain('Roles and permissions')
    expect(
      [...container.querySelectorAll('button')].find((b) => b.textContent === 'Add provider')
        ?.disabled,
    ).toBe(true)
  })
  it('creates an ordinary member with CSRF and clears the submitted password from mutation caches', async () => {
    await mount('/admin/members')
    await until(() => expect(container.textContent).toContain('Target'))
    await click('Create member')
    await fill('name', 'Created')
    await fill('email', 'created@example.invalid')
    await fill('password', 'fixture-initial-password')
    await submit()
    await until(() => expect(router.state.location.pathname).toBe('/admin/members/usr_target'))
    const request = requests.find((r) => r.method === 'post')!
    expect(JSON.parse(request.data).role).toBe('member')
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf')
    await until(() =>
      expect(
        JSON.stringify(
          cache
            .getMutationCache()
            .getAll()
            .map((m) => m.state),
        ),
      ).not.toContain('fixture-initial-password'),
    )
  })
  it('keeps the initial member password outside mutation caches while pending and after rejection', async () => {
    await mount('/admin/members')
    await until(() => expect(container.textContent).toContain('Target'))
    const adapter = client.defaults.adapter as AxiosAdapter
    let release!: () => void
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    failures['post /admin/members'] = 409
    client.defaults.adapter = async (config) => {
      if (config.method === 'post' && config.url === '/admin/members') await gate
      return adapter(config)
    }
    try {
      await click('Create member')
      await fill('name', 'Created')
      await fill('email', 'created@example.invalid')
      await fill('password', 'fixture-pending-initial-password')
      await submit()
      expect(
        JSON.stringify(
          cache
            .getMutationCache()
            .getAll()
            .map((item) => item.state),
        ),
      ).not.toContain('fixture-pending-initial-password')
      expect(
        document.querySelector<HTMLInputElement>('[role="dialog"] input[name="password"]')?.value,
      ).toBe('')
      await act(async () => release())
      await until(() =>
        expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull(),
      )
      expect(
        JSON.stringify(
          cache
            .getMutationCache()
            .getAll()
            .map((item) => item.state),
        ),
      ).not.toContain('fixture-pending-initial-password')
      expect(document.querySelector('[role="dialog"] [role="alert"]')?.textContent).not.toContain(
        'fixture-pending-initial-password',
      )
    } finally {
      await act(async () => release())
    }
  })
  it('does not offer administrator creation to delegated member managers', async () => {
    session.user.role = 'member'
    permissions = ['members.read', 'members.write']
    await mount('/admin/members')
    await until(() => expect(container.textContent).toContain('Target'))
    await click('Create member')
    expect(document.querySelector('[role="dialog"] select[name="role"]')).toBeNull()
  })
  it('requires confirmation before suspension and preserves the form on continuity errors', async () => {
    await mount('/admin/members')
    await until(() => expect(container.textContent).toContain('Target'))
    await act(async () => {
      document.querySelector<HTMLButtonElement>('[aria-label="Member actions for Target"]')!.click()
    })
    await until(() => expect(document.querySelector('[role="menuitem"]')).not.toBeNull())
    await act(async () => {
      ;[...document.querySelectorAll<HTMLElement>('[role="menuitem"]')]
        .find((item) => item.textContent === 'Disable')!
        .click()
    })
    expect(requests.some((r) => r.method === 'patch')).toBe(false)
    await until(() => expect(document.querySelector('[role="dialog"] textarea')).not.toBeNull())
    await act(async () => {
      const field = document.querySelector('textarea')!
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
        field,
        'Controlled lifecycle change',
      )
      field.dispatchEvent(new Event('input', { bubbles: true }))
    })
    failures['patch /admin/members/usr_target'] = 409
    await click('Confirm disable')
    await until(() =>
      expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull(),
    )
    expect(target.disabled).toBe(false)
    delete failures['patch /admin/members/usr_target']
    expect(document.querySelector('textarea')?.value).toBe('Controlled lifecycle change')
    await click('Abandon original state request')
    expect(document.querySelector('textarea')?.value).toBe('Controlled lifecycle change')
    expect(requests.filter((r) => r.method === 'patch')).toHaveLength(1)
    await click('Review current member state')
    await click('Confirm disable')
    await until(() => expect(target.disabled).toBe(true))
  })
  it('replaces explicit custom-role assignments without submitting built-in role IDs', async () => {
    await mount('/admin/members/usr_target?tab=roles')
    await until(() =>
      expect(
        document.querySelector<HTMLButtonElement>('[aria-label="Remove Provider Reader"]')
          ?.disabled,
      ).toBe(false),
    )
    await act(async () =>
      document.querySelector<HTMLButtonElement>('[aria-label="Remove Provider Reader"]')!.click(),
    )
    expect(requests.some((request) => request.method === 'put')).toBe(false)
    await click('Save member roles')
    await act(async () => {
      const input = document.querySelector<HTMLTextAreaElement>('textarea[aria-label="Reason"]')!
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
        input,
        'Controlled role replacement',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await submit('Confirm member roles')
    await until(() => expect(target.role_ids).toEqual([]))
    const request = requests.find((r) => r.method === 'put')!
    expect(JSON.parse(request.data)).toEqual({
      role_ids: [],
      role_definitions: [],
      builtin_definition_etag: 'b'.repeat(64),
      reason: 'Controlled role replacement',
    })
    expect(request.headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
    expect(requests.some((request) => request.url === '/admin/roles')).toBe(false)
  })
  it('protects built-ins and submits only chosen custom permissions', async () => {
    await mount('/admin/roles')
    await until(() => expect(container.textContent).toContain('Provider Reader'))
    expect(container.querySelectorAll('tbody tr')[0].textContent).not.toContain('Edit role')
    await click('Create custom role')
    await fill('name', 'Reader')
    await fill('description', 'Read member records')
    await act(async () =>
      document.querySelector<HTMLInputElement>('input[value="members.read"]')!.click(),
    )
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(JSON.parse(requests.find((r) => r.method === 'post')!.data)).toEqual({
      name: 'Reader',
      description: 'Read member records',
      permissions: ['members.read'],
    })
  })
  it('creates only explicitly selected resource groups and keeps selection during live language switching', async () => {
    await mount('/admin/roles')
    await until(() => expect(container.textContent).toContain('Provider Reader'))
    await click('Create custom role')
    await fill('name', 'Grouped reader')
    await fill('description', 'Read members and providers')
    const toggle = () =>
      document.querySelector<HTMLInputElement>(
        'input[aria-label="' +
          i18n.t('roles.selectAll', {
            ns: 'governance',
            resource: i18n.t('resources.providers', { ns: 'governance' }),
          }) +
          '"]',
      )!
    const read = () => document.querySelector<HTMLInputElement>('input[value="providers.read"]')!
    expect(toggle()).not.toBeNull()
    expect(toggle().checked).toBe(false)
    expect(toggle().indeterminate).toBe(false)
    await act(async () => read().click())
    expect(toggle().indeterminate).toBe(true)
    await act(async () =>
      document.querySelector<HTMLInputElement>('input[value="members.read"]')!.click(),
    )
    await act(async () => toggle().click())
    expect(toggle().checked).toBe(true)
    expect(toggle().indeterminate).toBe(false)
    await act(async () => i18n.changeLanguage('zh'))
    expect(toggle().getAttribute('aria-label')).toBe('全选供应商接入动作')
    expect(toggle().checked).toBe(true)
    await act(async () => toggle().click())
    expect(read().checked).toBe(false)
    expect(document.querySelector<HTMLInputElement>('input[value="members.read"]')!.checked).toBe(
      true,
    )
    await act(async () => toggle().click())
    await act(async () => i18n.changeLanguage('en'))
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(JSON.parse(requests.find((r) => r.method === 'post')!.data)).toEqual({
      name: 'Grouped reader',
      description: 'Read members and providers',
      permissions: ['members.read', 'providers.read', 'providers.write'],
    })
  })
  it('summarizes recorded resources and actions with singular and empty counts', async () => {
    roles[0].permissions = ['egress.read', 'egress.test', 'secrets.rotate']
    await mount('/admin/roles')
    await until(() => expect(container.textContent).toContain('2 resources · 3 actions'))
    expect(container.textContent).toContain('1 resource · 1 action')
    expect(container.textContent).toContain('0 resources · 0 actions')
    await act(async () => i18n.changeLanguage('zh'))
    expect(container.textContent).toContain('2 个资源 · 3 个动作')
    expect(container.textContent).toContain('1 个资源 · 1 个动作')
  })
  it('localizes supported permission groups and actions without translating stored codes', async () => {
    roles[0].permissions = [
      'egress.test',
      'limits.settings.write',
      'secrets.rotate',
      'smtp.test',
      'storage.read',
      'teams.tokens.write',
    ]
    await mount('/admin/roles')
    await until(() => expect(container.textContent).toContain('6 resources · 6 actions'))
    await click('View permissions')
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Rotate secrets'),
    )
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Managed egress')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Run diagnostics')
    await act(async () => i18n.changeLanguage('zh'))
    for (const value of [
      '出口管理',
      '轮换敏感信息',
      '配额设置',
      '存储',
      '邮件发送',
      '管理 Token 限额',
    ]) {
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain(value)
    }
    expect(roles[0].permissions).toEqual([
      'egress.test',
      'limits.settings.write',
      'secrets.rotate',
      'smtp.test',
      'storage.read',
      'teams.tokens.write',
    ])
    expect(requests.some((r) => r.method === 'put' || r.method === 'post')).toBe(false)
  })
  it('renders only authoritative retained member counts with zero, singular and live localization', async () => {
    roles[0].member_count = 12
    roles[1].member_count = 0
    roles[2].member_count = 1
    await mount('/admin/roles')
    await until(() => expect(container.textContent).toContain('12 members'))
    const rows = () => [...container.querySelectorAll<HTMLTableRowElement>('tbody tr')]
    expect(rows().map((row) => row.cells[2].textContent)).toEqual([
      '12 members',
      '0 members',
      '1 member',
    ])
    expect(container.querySelector('table')?.getAttribute('aria-describedby')).toBe(
      'role-member-count-help',
    )
    expect(container.querySelector('#role-member-count-help')?.textContent).toContain(
      'including inactive, pending and rejected accounts',
    )
    expect(container.querySelector('#role-member-count-help')?.textContent).toContain(
      'Team assignments are excluded',
    )
    expect(requests.filter((r) => r.method === 'get').map((r) => r.url)).not.toContain(
      '/admin/members',
    )
    expect(requests.filter((r) => r.url === '/admin/roles')).toHaveLength(1)
    await act(async () => i18n.changeLanguage('zh'))
    expect(rows().map((row) => row.cells[2].textContent)).toEqual(['12 人', '0 人', '1 人'])
    expect([...container.querySelectorAll('th')].map((node) => node.textContent)).toContain('成员')
    expect(container.querySelector('#role-member-count-help')?.textContent).toContain(
      '不计团队范围的角色分配',
    )
  })
  it('renders missing and invalid member counts as unknown and never derives them from roles or permissions', async () => {
    roles[1].member_count = -1
    roles[2].member_count = Number.MAX_SAFE_INTEGER + 1
    await mount('/admin/roles')
    await until(() => expect(container.textContent).toContain('Provider Reader'))
    const counts = () =>
      [...container.querySelectorAll<HTMLTableRowElement>('tbody tr')].map(
        (row) => row.cells[2].textContent,
      )
    expect(counts()).toEqual(['Unknown', 'Unknown', 'Unknown'])
    await act(async () => i18n.changeLanguage('zh'))
    expect(counts()).toEqual(['未知', '未知', '未知'])
  })
  it('hides retained counts during list renewal and failure, then displays only fresh authorized counts', async () => {
    roles[0].member_count = 12
    roles[1].member_count = 0
    roles[2].member_count = 7
    await mount('/admin/roles')
    await until(() => expect(container.textContent).toContain('12 members'))
    const adapter = client.defaults.adapter as AxiosAdapter
    let release!: () => void
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    client.defaults.adapter = async (config) => {
      if (config.url === '/admin/roles') await gate
      return adapter(config)
    }
    failures['get /admin/roles'] = 503
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['admin', 'roles'] })
    })
    expect(container.querySelector('tbody')?.textContent).toBe('')
    await act(async () => release())
    await until(() => expect(container.querySelector('[role=alert]')).not.toBeNull())
    expect(container.querySelector('tbody')?.textContent).toBe('')
    delete failures['get /admin/roles']
    roles[0].member_count = 13
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['admin', 'roles'] })
    })
    await until(() => expect(container.textContent).toContain('13 members'))
    expect(container.textContent).not.toContain('12 members')
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['permissions'], refetchType: 'none' })
    })
    expect(container.querySelector('tbody')?.textContent).toBe('')
    expect(requests.some((r) => r.url === '/admin/members')).toBe(false)
  })
  it('surfaces assigned-role deletion conflicts without removing a role', async () => {
    await mount('/admin/roles')
    await until(() => expect(container.textContent).toContain('Provider Reader'))
    await click('Delete role')
    failures['delete /admin/roles/rol_custom'] = 409
    await click('Confirm deletion')
    await until(() =>
      expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull(),
    )
    expect(container.textContent).toContain('Provider Reader')
  })
  it('saves registration policy from the configuration drawer using CSRF', async () => {
    await mount('/admin/auth')
    await until(() => expect(container.textContent).toContain('Not enabled'))
    await click('Configure')
    await act(async () => document.querySelector<HTMLElement>('[role="switch"]')!.click())
    await fillApprovalReason()
    await submit('Member registration settings')
    await click('Confirm')
    await until(() =>
      expect(container.textContent).toContain(
        'Current registration policy confirmed. Existing accounts and applications are unchanged.',
      ),
    )
    expect(registration).toBe(true)
    expect(requests.find((r) => r.method === 'patch')?.headers.get('X-CSRF-Token')).toBe('csrf')
  })
  it('hides closed registration and blocks direct registration without a write', async () => {
    await mount('/register')
    await until(() =>
      expect(container.textContent).toContain(
        'Registration is not available. Contact an administrator.',
      ),
    )
    expect(container.querySelector('form')).toBeNull()
    expect(requests.some((r) => r.method === 'post')).toBe(false)
  })
  it('registers through the public form and clears previous account caches', async () => {
    registration = true
    cache.setQueryData(['private', 'old'], ['old-data'])
    await mount('/register')
    await until(() => expect(container.querySelector('form')).not.toBeNull())
    await fill('name', 'New User')
    await fill('email', 'new@example.invalid')
    await fill('password', 'registration-fixture-password')
    await submit('Register')
    await until(() => expect(router.state.location.pathname).toBe('/'))
    expect(cache.getQueryData(['private', 'old'])).toBeUndefined()
    expect(cache.getQueryData<Session>(['auth', 'session'])?.user.role).toBe('member')
    expect(JSON.parse(requests.find((r) => r.url === '/auth/register')!.data)).not.toHaveProperty(
      'role',
    )
  })
  it('translates member validation and dialog labels live without losing form input', async () => {
    await mount('/admin/members')
    await until(() => expect(container.textContent).toContain('Target'))
    await click('Create member')
    await fill('name', 'Draft member')
    await fill('password', 'short')
    await submit()
    expect(document.querySelector('[role="dialog"] [role="alert"]')?.textContent).toBe(
      'Passwords must contain 12–72 UTF-8 bytes.',
    )
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('创建成员')
    expect(document.querySelector('[role="dialog"] [role="alert"]')?.textContent).toBe(
      '密码须为 12–72 个 UTF-8 字节。',
    )
    expect(document.querySelector<HTMLInputElement>('input[name="name"]')?.value).toBe(
      'Draft member',
    )
    expect(requests.some((request) => request.method === 'post')).toBe(false)
    await act(async () => {
      await i18n.changeLanguage('en')
    })
    expect(document.querySelector('[role="dialog"] [role="alert"]')?.textContent).toBe(
      'Passwords must contain 12–72 UTF-8 bytes.',
    )
  })
  it('keeps the registration draft through language switches and translates an existing success notice', async () => {
    await mount('/admin/auth')
    await until(() => expect(container.textContent).toContain('Not enabled'))
    await click('Configure')
    await act(async () => {
      document.querySelector<HTMLElement>('[role="switch"]')!.click()
      await i18n.changeLanguage('zh')
    })
    expect(document.querySelector('[role="switch"]')?.getAttribute('aria-checked')).toBe('true')
    expect(document.querySelector('[role="switch"]')?.getAttribute('aria-label')).toBe(
      '开放邮箱注册',
    )
    await fillApprovalReason()
    await submit('成员注册设置')
    await click('确认')
    await until(() =>
      expect(container.textContent).toContain('已确认当前注册策略。现有账号和申请保持原状态。'),
    )
    expect(registration).toBe(true)
    await act(async () => {
      await i18n.changeLanguage('en')
    })
    expect(container.textContent).toContain(
      'Current registration policy confirmed. Existing accounts and applications are unchanged.',
    )
    await click('Configure')
    expect(document.querySelector('[role="switch"]')?.getAttribute('aria-checked')).toBe('true')
    expect(requests.filter((request) => request.method === 'patch')).toHaveLength(1)
  })
  it('translates built-in role names and permission rows while preserving custom role names', async () => {
    await mount('/admin/roles')
    await until(() => expect(container.textContent).toContain('Provider Reader'))
    await click('View permissions')
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
        'Provider connections',
      ),
    )
    expect(requests.some((request) => request.url === '/admin/roles/rol_admin')).toBe(true)
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(container.textContent).toContain('管理员')
    expect(container.textContent).toContain('Provider Reader')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('供应商接入')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('允许动作')
  })
})

describe('Member detail fresh private authority', () => {
  it('hides cached member details and actions after a renewed target read is rejected', async () => {
    await mount('/admin/members/usr_target?tab=settings')
    await until(() => expect(container.textContent).toContain('target@example.invalid'))
    failures['get /admin/members/usr_target'] = 403
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['admin', 'member'] })
    })
    await until(() => expect(container.querySelector('[role="alert"]')).not.toBeNull())
    expect(container.textContent).not.toContain('target@example.invalid')
    expect(container.textContent).not.toContain('Target')
  })
  it('does not reuse another actor member detail after an account switch', async () => {
    await mount('/admin/members/usr_target')
    await until(() => expect(container.textContent).toContain('target@example.invalid'))
    failures['get /admin/members/usr_target'] = 403
    session = { ...session, user: { ...session.user, id: 'usr_next' } }
    await act(async () => cache.setQueryData(['auth', 'session'], structuredClone(session)))
    await until(() => expect(container.querySelector('[role="alert"]')).not.toBeNull())
    expect(container.textContent).not.toContain('target@example.invalid')
    expect(requests.filter((request) => request.url === '/admin/members/usr_target')).toHaveLength(
      2,
    )
  })
})

async function fillApprovalReason() {
  const field = document.querySelector<HTMLTextAreaElement>('textarea')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
      field,
      'Reviewed new registration policy',
    )
    field.dispatchEvent(new Event('input', { bubbles: true }))
    field.dispatchEvent(new Event('change', { bubbles: true }))
  })
}

it('shows only recorded role descriptions or localized absence and creates an exact trimmed multiline scope', async () => {
  await mount('/admin/roles')
  await until(() => expect(container.textContent).toContain('Read provider records'))
  expect(container.textContent).toContain('Not provided')
  await click('Create custom role')
  await fill('name', 'Business role')
  const description = document.querySelector<HTMLTextAreaElement>('textarea[name=description]')!
  expect(description.rows).toBe(2)
  await fill('description', '  Business scope\n审批职责  ')
  await act(async () => i18n.changeLanguage('zh'))
  expect(description.value).toBe('  Business scope\n审批职责  ')
  expect(description.getAttribute('aria-label')).toBe('角色说明')
  await submit()
  await until(() => expect(document.querySelector('[role=dialog]')).toBeNull())
  expect(JSON.parse(requests.find((r) => r.method === 'post')!.data)).toEqual({
    name: 'Business role',
    description: 'Business scope\n审批职责',
    permissions: [],
  })
})

it('does not dispatch custom role creation with empty, over-byte-bound or control-bearing description', async () => {
  await mount('/admin/roles')
  await until(() => expect(container.textContent).toContain('Provider Reader'))
  await click('Create custom role')
  await fill('name', 'Business role')
  for (const value of ['', '  ', 'é'.repeat(1001), 'Business\tScope']) {
    await fill('description', value)
    await submit()
    expect(requests.filter((r) => r.method === 'post')).toHaveLength(0)
  }
  await fill('description', 'é'.repeat(1000))
  await submit()
  await until(() => expect(requests.filter((r) => r.method === 'post')).toHaveLength(1))
  expect(JSON.parse(requests.find((r) => r.method === 'post')!.data).description).toBe(
    'é'.repeat(1000),
  )
})

it('renders exact FEFF descriptions and preserves them when creating from a bilingual draft', async () => {
  const recorded = '\uFEFFRecorded scope\n审批职责\uFEFF'
  roles[2].description = recorded
  await mount('/admin/roles')
  await until(() => expect(container.textContent).toContain(recorded))
  expect(container.textContent).toContain('Not provided')
  await click('Create custom role')
  await fill('name', 'Business role')
  const captured = '\uFEFFNew scope\n审批职责\uFEFF'
  await fill('description', ' \u00a0' + captured + '\u3000 ')
  await act(async () => i18n.changeLanguage('zh'))
  expect(
    document
      .querySelector<HTMLTextAreaElement>('textarea[name=description]')!
      .getAttribute('aria-label'),
  ).toBe('角色说明')
  await submit()
  await until(() => expect(requests.filter((request) => request.method === 'post')).toHaveLength(1))
  expect(JSON.parse(requests.find((request) => request.method === 'post')!.data)).toEqual({
    name: 'Business role',
    description: captured,
    permissions: [],
  })
})
it('preserves FEFF in new descriptions without treating it as an absent draft', async () => {
  await mount('/admin/roles')
  await until(() => expect(container.textContent).toContain('Provider Reader'))
  await click('Create custom role')
  await fill('name', 'Format role')
  await fill('description', '\uFEFF')
  await submit()
  await until(() => expect(requests.filter((request) => request.method === 'post')).toHaveLength(1))
  expect(JSON.parse(requests.find((request) => request.method === 'post')!.data).description).toBe(
    '\uFEFF',
  )
})

async function roleActor(id: string, csrf: string) {
  session = { ...session, user: { ...session.user, id }, csrf_token: csrf }
  await act(async () => {
    cache.setQueryData(['permissions', id], [...permissions])
    cache.setQueryData(['auth', 'session'], structuredClone(session))
  })
  await until(() => expect(cache.isFetching()).toBe(0))
  await until(() => expect(container.textContent).toContain('Provider Reader'))
}
async function createRoleDraft(name: string) {
  await click('Create custom role')
  await fill('name', name)
  await fill('description', `${name} description`)
}
function roleDialogName() {
  return document.querySelector<HTMLInputElement>('[role="dialog"] input[name="name"]')
}
it('role shell refuses a queued old actor form before obtaining the new actor CSRF', async () => {
  let release!: () => void
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  cache = new QueryClient({
    mutationCache: new MutationCache({ onMutate: async () => gate }),
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  await mount('/admin/roles')
  await until(() => expect(container.textContent).toContain('Provider Reader'))
  await createRoleDraft('Queued A')
  await submit()
  await roleActor('usr_peer', 'csrf-peer')
  await act(async () => {
    release()
  })
  await until(() => expect(cache.isMutating()).toBe(0))
  expect(requests.filter((r) => r.method === 'post' && r.url === '/admin/roles')).toHaveLength(0)
  expect(roleDialogName()).toBeNull()
})
it.each([200, 400, 409, 503])(
  'role shell isolates held old %s response from an A-B-A new draft',
  async (status) => {
    await mount('/admin/roles')
    await until(() => expect(container.textContent).toContain('Provider Reader'))
    const adapter = client.defaults.adapter as AxiosAdapter
    let release!: () => void
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    let captured: InternalAxiosRequestConfig | undefined
    client.defaults.adapter = async (config) => {
      if (config.method === 'post' && config.url === '/admin/roles') {
        captured = config
        await gate
        const response = { config, status, statusText: '', headers: new AxiosHeaders(), data: {} }
        if (status >= 400)
          throw new AxiosError('Old role response', '', config, undefined, response)
        return response
      }
      return adapter(config)
    }
    await createRoleDraft('Original A')
    await submit()
    await until(() => expect(captured).toBeDefined())
    const submitted = captured!.data
    await roleActor('usr_peer', 'csrf-peer')
    await roleActor('usr_admin', 'csrf-new-A')
    await createRoleDraft('Fresh A')
    const reads = requests.filter((r) => r.method === 'get' && r.url === '/admin/roles').length
    await act(async () => {
      release()
    })
    await until(() => expect(cache.isMutating()).toBe(0))
    expect(roleDialogName()?.value).toBe('Fresh A')
    expect(document.querySelector<HTMLTextAreaElement>('[role="dialog"] textarea')?.value).toBe(
      'Fresh A description',
    )
    expect(document.querySelector('[role="dialog"] [role="alert"]')).toBeNull()
    expect(requests.filter((r) => r.method === 'get' && r.url === '/admin/roles')).toHaveLength(
      reads,
    )
    expect(captured!.data).toBe(submitted)
    expect(captured!.headers.get('X-CSRF-Token')).toBe('csrf')
  },
)
it.each([200, 503])(
  'role shell isolates held delete %s from a reopened creation',
  async (status) => {
    await mount('/admin/roles')
    await until(() => expect(container.textContent).toContain('Provider Reader'))
    const adapter = client.defaults.adapter as AxiosAdapter
    let release!: () => void
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    let started = false
    client.defaults.adapter = async (config) => {
      if (config.method === 'delete') {
        started = true
        await gate
        const response = { config, status, statusText: '', headers: new AxiosHeaders(), data: {} }
        if (status >= 400)
          throw new AxiosError('Old delete response', '', config, undefined, response)
        return response
      }
      return adapter(config)
    }
    await click('Delete role')
    await click('Confirm deletion')
    await until(() => expect(started).toBe(true))
    await roleActor('usr_peer', 'csrf-peer')
    await createRoleDraft('Peer fresh')
    await act(async () => {
      release()
    })
    await until(() => expect(cache.isMutating()).toBe(0))
    expect(roleDialogName()?.value).toBe('Peer fresh')
  },
)

it('role shell preserves same-owner drafts through fresh Session reads and marks a renewed in-flight success unconfirmed', async () => {
  await mount('/admin/roles')
  await until(() => expect(container.textContent).toContain('Provider Reader'))
  await createRoleDraft('Session draft')
  session.csrf_token = 'renewed-before'
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['auth', 'session'] })
  })
  await until(() => expect(cache.isFetching()).toBe(0))
  expect(roleDialogName()?.value).toBe('Session draft')
  const adapter = client.defaults.adapter as AxiosAdapter
  let release!: () => void
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  let submitted: InternalAxiosRequestConfig | undefined
  client.defaults.adapter = async (config) => {
    if (config.method === 'post' && config.url === '/admin/roles') {
      submitted = config
      await gate
      return { config, status: 201, statusText: '', headers: new AxiosHeaders(), data: {} }
    }
    return adapter(config)
  }
  await submit()
  await until(() => expect(submitted).toBeDefined())
  session.csrf_token = 'renewed-after'
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['auth', 'session'] })
  })
  await until(() => expect(cache.isFetching()).toBe(0))
  await act(async () => {
    release()
  })
  await until(() => expect(cache.isMutating()).toBe(0))
  expect(roleDialogName()?.value).toBe('Session draft')
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
    'operation is unconfirmed',
  )
  expect(submitted!.headers.get('X-CSRF-Token')).toBe('renewed-before')
  const state = JSON.stringify(
    cache
      .getMutationCache()
      .getAll()
      .map((m) => m.state),
  )
  expect(state).not.toContain('renewed-before')
  expect(state).not.toContain('renewed-after')
})
it('role shell retains exact uncertain creation without refresh replay, switches language, and requires explicit local abandonment', async () => {
  await mount('/admin/roles', true)
  await until(() => expect(container.textContent).toContain('Provider Reader'))
  await createRoleDraft('Unknown original')
  failures['post /admin/roles'] = 503
  await submit()
  await until(() =>
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      'operation is unconfirmed',
    ),
  )
  const posts = requests.filter((r) => r.method === 'post')
  expect(posts).toHaveLength(1)
  expect(JSON.parse(posts[0].data).name).toBe('Unknown original')
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'roles'] })
  })
  await until(() => expect(cache.isFetching()).toBe(0))
  expect(roleDialogName()?.value).toBe('Unknown original')
  expect(roleDialogName()?.disabled).toBe(false)
  expect(roleDialogName()?.closest('fieldset')?.disabled).toBe(true)
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
    '已提交的角色操作结果尚未确认',
  )
  expect(roleDialogName()?.value).toBe('Unknown original')
  expect(requests.filter((r) => r.method === 'post')).toHaveLength(1)
  await click('放弃本地意图（不会取消操作）')
  expect(roleDialogName()?.closest('fieldset')?.disabled).toBe(false)
  expect(roleDialogName()?.value).toBe('Unknown original')
  expect(requests.filter((r) => r.method === 'post')).toHaveLength(1)
})
it('role shell rejects queued Session renewal without dispatch and retains an editable same-owner draft', async () => {
  let release!: () => void
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  cache = new QueryClient({
    mutationCache: new MutationCache({ onMutate: async () => gate }),
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  await mount('/admin/roles')
  await until(() => expect(container.textContent).toContain('Provider Reader'))
  await createRoleDraft('Not dispatched')
  await submit()
  session.csrf_token = 'different-session'
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['auth', 'session'] })
  })
  await until(() => expect(cache.isFetching()).toBe(0))
  await act(async () => {
    release()
  })
  await until(() => expect(cache.isMutating()).toBe(0))
  expect(requests.filter((r) => r.method === 'post')).toHaveLength(0)
  expect(roleDialogName()?.value).toBe('Not dispatched')
  expect(roleDialogName()?.closest('fieldset')?.disabled).toBe(false)
  expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain(
    'operation is unconfirmed',
  )
})

it.each([201, 503])(
  'role shell leaves a new pending A intent intact when old A returns %s',
  async (status) => {
    await mount('/admin/roles')
    await until(() => expect(container.textContent).toContain('Provider Reader'))
    const adapter = client.defaults.adapter as AxiosAdapter
    const releases: (() => void)[] = []
    const captured: InternalAxiosRequestConfig[] = []
    client.defaults.adapter = async (config) => {
      if (config.method !== 'post' || config.url !== '/admin/roles') return adapter(config)
      const index = captured.push(config) - 1
      await new Promise<void>((resolve) => {
        releases[index] = resolve
      })
      const response = {
        config,
        status: index === 0 ? status : 201,
        statusText: '',
        headers: new AxiosHeaders(),
        data: {},
      }
      if (response.status >= 400)
        throw new AxiosError('Old pending response', '', config, undefined, response)
      return response
    }
    await createRoleDraft('First A')
    await submit()
    await until(() => expect(captured).toHaveLength(1))
    await roleActor('usr_peer', 'csrf-peer')
    await roleActor('usr_admin', 'new-A-csrf')
    await createRoleDraft('Second A')
    await submit()
    await until(() => expect(captured).toHaveLength(2))
    const original = captured.map((x) => x.data)
    await act(async () => {
      releases[0]()
    })
    await until(() => expect(cache.isMutating()).toBe(1))
    expect(roleDialogName()?.value).toBe('Second A')
    expect(roleDialogName()?.closest('fieldset')?.disabled).toBe(true)
    expect(document.querySelector('[role="dialog"] [role="alert"]')).toBeNull()
    expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain(
      'operation is unconfirmed',
    )
    expect(captured.map((x) => x.data)).toEqual(original)
    expect(captured[1].headers.get('X-CSRF-Token')).toBe('new-A-csrf')
    await act(async () => {
      releases[1]()
    })
    await until(() => expect(cache.isMutating()).toBe(0))
    expect(roleDialogName()).toBeNull()
  },
)
