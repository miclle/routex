import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
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
let registration: boolean
let failures: Record<string, number>
const oldAdapter = client.defaults.adapter
beforeEach(() => {
  requests = []
  failures = {}
  registration = false
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
    { id: 'rol_admin', name: 'Administrator', builtin: true, permissions },
    { id: 'rol_member', name: 'Member', builtin: true, permissions: [] },
    { id: 'rol_custom', name: 'Provider Reader', builtin: false, permissions: ['providers.read'] },
  ]
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const key = `${config.method} ${config.url}`
    const response = {
      config,
      status: failures[key] ?? 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (response.status >= 400) throw new AxiosError('Failure', '', config, undefined, response)
    if (key === 'get /auth/session') response.data = structuredClone(session)
    if (key === 'get /auth/permissions') response.data = { permissions: [...permissions] }
    if (key === 'get /admin/members')
      response.data = { items: [structuredClone(target)], next_cursor: null }
    if (key === 'get /admin/members/usr_target') response.data = structuredClone(target)
    if (key === 'get /admin/roles')
      response.data = {
        items: structuredClone(roles),
        available_permissions: ['providers.read', 'providers.write', 'members.read'],
      }
    if (key === 'get /auth/registration' || key === 'get /admin/registration')
      response.data = { enabled: registration }
    if (key === 'patch /admin/registration') {
      registration = JSON.parse(config.data).enabled
      response.data = { enabled: registration }
    }
    if (key === 'patch /admin/members/usr_target') {
      Object.assign(target, JSON.parse(config.data))
      response.data = structuredClone(target)
    }
    if (key === 'put /admin/members/usr_target/roles') {
      target.role_ids = JSON.parse(config.data).role_ids
      response.data = structuredClone(target)
    }
    if (key === 'post /admin/members') {
      const data = JSON.parse(config.data)
      target = { ...target, name: data.name, email: data.email, role: data.role }
      response.data = structuredClone(target)
    }
    if (key === 'post /admin/roles') {
      roles.push({ ...JSON.parse(config.data), id: 'rol_new', builtin: false })
      response.data = structuredClone(roles.at(-1))
    }
    if (key === 'post /auth/register') {
      const data = JSON.parse(config.data)
      session = {
        user: { ...target, name: data.name, email: data.email, role: 'member' },
        csrf_token: 'registered-csrf',
      }
      response.data = structuredClone(session)
    }
    if (key === 'get /admin/providers')
      response.data = { items: [{ id: 'prv_1', name: 'Provider', connections: [] }] }
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
async function mount(path: string) {
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
        <RouterProvider router={router} />
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
    const input = document.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
    expect(input).not.toBeNull()
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
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
  it('distinguishes completed offboarding from suspension and offers the review entry', async () => {
    target.disabled = true
    target.offboarded_at = '2026-09-23T01:00:00Z'
    await mount('/admin/members/usr_target?tab=settings')
    await until(() => expect(container.textContent).toContain('Offboarded'))
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
    await click('Disable')
    expect(requests.some((r) => r.method === 'patch')).toBe(false)
    failures['patch /admin/members/usr_target'] = 409
    await click('Confirm disable')
    await until(() =>
      expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull(),
    )
    expect(target.disabled).toBe(false)
    delete failures['patch /admin/members/usr_target']
    await click('Confirm disable')
    await until(() => expect(target.disabled).toBe(true))
  })
  it('replaces explicit custom-role assignments without submitting built-in role IDs', async () => {
    await mount('/admin/members/usr_target?tab=roles')
    await until(() => expect(document.querySelector('input[value="rol_custom"]')).not.toBeNull())
    await act(async () =>
      document.querySelector<HTMLInputElement>('input[value="rol_custom"]')!.click(),
    )
    await submit('Member roles')
    await until(() => expect(target.role_ids).toEqual([]))
    expect(JSON.parse(requests.find((r) => r.method === 'put')!.data)).toEqual({ role_ids: [] })
  })
  it('protects built-ins and submits only chosen custom permissions', async () => {
    await mount('/admin/roles')
    await until(() => expect(container.textContent).toContain('Provider Reader'))
    expect(container.querySelectorAll('tbody tr')[0].textContent).not.toContain('Edit role')
    await click('Create custom role')
    await fill('name', 'Reader')
    await act(async () =>
      document.querySelector<HTMLInputElement>('input[value="members.read"]')!.click(),
    )
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(JSON.parse(requests.find((r) => r.method === 'post')!.data)).toEqual({
      name: 'Reader',
      permissions: ['members.read'],
    })
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
    await submit('Member registration settings')
    await until(() =>
      expect(container.textContent).toContain('Member registration settings saved.'),
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
    await submit('成员注册设置')
    await until(() => expect(container.textContent).toContain('成员注册设置已保存。'))
    expect(registration).toBe(true)
    await act(async () => {
      await i18n.changeLanguage('en')
    })
    expect(container.textContent).toContain('Member registration settings saved.')
    await click('Configure')
    expect(document.querySelector('[role="switch"]')?.getAttribute('aria-checked')).toBe('true')
    expect(requests.filter((request) => request.method === 'patch')).toHaveLength(1)
  })
  it('translates built-in role names and permission rows while preserving custom role names', async () => {
    await mount('/admin/roles')
    await until(() => expect(container.textContent).toContain('Provider Reader'))
    await click('View permissions')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Provider connections')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(container.textContent).toContain('管理员')
    expect(container.textContent).toContain('Provider Reader')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('供应商接入')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('允许动作')
  })
})
