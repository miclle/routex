import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider, QueryObserver } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import LDAPConfiguration from './config'
import LDAPAccount from './account'
import AuthPage from '@/views/auth'
import type { Session } from '@/types/auth'
import type { LDAPConfig, LDAPIdentity } from '@/types/ldap'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter,
  etag = 'a'.repeat(64)
let host: HTMLDivElement,
  root: Root,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let session: Session | null,
  grants: string[],
  config: LDAPConfig,
  identity: LDAPIdentity,
  requests: InternalAxiosRequestConfig[],
  failures: Record<string, number>
let held: { path: string; promise: Promise<void>; release: () => void } | undefined
let heldSession: { promise: Promise<void>; release: () => void } | undefined
let sessionLoss: boolean, loginStatus: number, loginData: unknown
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  session = {
    user: { id: 'usr_alice', name: 'Alice', email: 'alice@example.test', role: 'admin' },
    csrf_token: 'csrf-current',
  }
  grants = ['registration.write']
  config = {
    name: 'Directory',
    endpoint: 'ldaps://directory.example.test:636',
    bind_dn: 'cn=service,dc=example,dc=test',
    base_dn: 'dc=example,dc=test',
    user_filter: '(uid={username})',
    identity_attribute: 'entryUUID',
    secret_configured: true,
    mfa_required: false,
    enabled: false,
    verified: false,
    review_etag: etag,
  }
  identity = {
    available: true,
    name: 'Directory',
    bound: false,
    mfa_required: false,
    review_etag: etag,
  }
  requests = []
  failures = {}
  held = undefined
  heldSession = undefined
  sessionLoss = false
  loginStatus = 200
  loginData = {
    user: { id: 'usr_alice', name: 'Alice', email: 'alice@example.test', role: 'member' },
    csrf_token: 'ldap-csrf',
  }
  client.defaults.adapter = async (request) => {
    requests.push(request)
    const path = request.url!
    if (held?.path === path && request.method !== 'get') await held.promise
    let status = failures[path] ?? 200,
      data: unknown = {}
    if (path === '/auth/session') {
      data = session ? structuredClone(session) : null
      if (!data) status = 401
      if (heldSession) await heldSession.promise
    } else if (path === '/auth/permissions') data = { permissions: grants }
    else if (path === '/site')
      data = {
        name: 'RouteX',
        logo_url: '',
        footer: '',
        default_language: 'en',
        service_url: '',
        etag: '0',
        updated_at: '2026-10-10T00:00:00Z',
      }
    else if (path === '/auth/registration')
      data = { enabled: false, approval_required: false, allowed_email_domains: [] }
    else if (path === '/auth/oidc') data = { available: true, name: 'Existing OIDC' }
    else if (path === '/auth/oauth') data = { available: true, name: 'Existing OAuth' }
    else if (path === '/auth/ldap') data = { available: true, name: 'Directory' }
    else if (path === '/auth/ldap/login') {
      status = failures[path] ?? loginStatus
      data = loginData
    } else if (path === '/auth/login') data = loginData
    else if (path === '/auth/mfa/verify') data = loginData
    else if (path === '/admin/auth/ldap') {
      if (request.method === 'put' && status === 200) {
        const body = JSON.parse(request.data)
        const security =
          ['endpoint', 'bind_dn', 'base_dn', 'user_filter', 'identity_attribute'].some(
            (key) => body[key] !== config[key as keyof LDAPConfig],
          ) || body.secret_action === 'replace'
        config = {
          ...config,
          name: body.name,
          endpoint: body.endpoint,
          bind_dn: body.bind_dn,
          base_dn: body.base_dn,
          user_filter: body.user_filter,
          identity_attribute: body.identity_attribute,
          review_etag: 'b'.repeat(64),
          ...(security ? { enabled: false, verified: false } : {}),
        }
        if (sessionLoss) session = null
      }
      data = { ...config }
    } else if (path === '/admin/auth/ldap/status') {
      if (status === 200) {
        config = {
          ...config,
          enabled: JSON.parse(request.data).enabled,
          review_etag: 'b'.repeat(64),
        }
        if (sessionLoss) session = null
      }
      data = { ...config }
    } else if (path === '/admin/auth/ldap/verify') {
      data = { kind: 'verified' }
      if (status === 200) {
        config = { ...config, verified: true, review_etag: 'b'.repeat(64) }
        identity = { ...identity, bound: true }
        if (sessionLoss) session = null
      }
    } else if (path === '/account/identity/ldap') data = { ...identity }
    else if (path === '/account/identity/ldap/bind') {
      data = { kind: 'bound' }
      if (status === 200) {
        identity = { ...identity, bound: true, review_etag: 'b'.repeat(64) }
        if (sessionLoss) session = null
      }
    } else if (path === '/account/identity/ldap/unlink') {
      if (status === 200) {
        identity = { ...identity, bound: false, review_etag: 'b'.repeat(64) }
        if (sessionLoss) session = null
      }
      data = { ...identity }
    }
    const response = {
      config: request,
      status,
      statusText: '',
      data,
      headers: new AxiosHeaders({
        'Cache-Control': 'private, no-store',
        'X-Content-Type-Options': 'nosniff',
        ETag: `"${path.startsWith('/account/') ? identity.review_etag : config.review_etag}"`,
      }),
    }
    if (path === '/auth/session' && status >= 400)
      throw new AxiosError('Session unavailable', undefined, request, undefined, response)
    return response
  }
})
afterEach(async () => {
  held?.release()
  heldSession?.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  document.querySelectorAll('[data-base-ui-portal]').forEach((element) => element.remove())
  client.defaults.adapter = original
})
async function mount(node: ReactNode, path = '/test') {
  router = createMemoryRouter(
    [
      { path, element: node },
      { path: '/', element: <p>Workspace</p> },
      { path: '/login', element: <p>Local sign-in</p> },
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
async function wait(check: () => boolean) {
  for (let count = 0; count < 100; count++) {
    if (check()) return
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
  }
  expect(check()).toBe(true)
}
function button(label: string) {
  const value = Array.from(document.querySelectorAll('button')).find(
    (item) => item.textContent === label,
  )
  expect(value, label).toBeDefined()
  return value!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
function input(label: string, value: string, index = 0) {
  const labels = Array.from(document.querySelectorAll('label')).filter(
    (item) => item.textContent === label,
  )
  const element = labels[index]?.querySelector('input')
  expect(element, label).toBeDefined()
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, value)
  element!.dispatchEvent(new Event('input', { bubbles: true }))
}
function hold(path: string) {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  held = { path, promise, release }
  return release
}
function holdSession() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  heldSession = { promise, release }
  return release
}
const writes = () => requests.filter((row) => row.method !== 'get')
async function openConfig() {
  await wait(() => document.body.textContent!.includes('Configure LDAP'))
  await click('Configure LDAP')
}
async function nameSave() {
  await openConfig()
  await act(async () => {
    input('Display name', 'Directory renamed')
    input('Reason', 'Reviewed display name', 0)
  })
  await click('Review configuration')
}
async function directoryProof(configure = false) {
  await act(async () => {
    input('Directory username', 'alice')
    input('Directory password', ' directory-proof ')
    input('Current local RouteX password', 'current-password-123')
    input('Reason', 'Reviewed link', configure ? 1 : 0)
  })
}
async function prepareBind() {
  await wait(() => document.body.textContent!.includes('Link directory identity'))
  await click('Link directory identity')
  await directoryProof()
  await click('Review change')
}
async function loginForm() {
  await wait(() => document.body.textContent!.includes('Sign in with Directory'))
  await click('Sign in with Directory')
  await act(async () => {
    input('Directory username', 'alice')
    input('Directory password', ' directory-proof ')
  })
}
describe('LDAP current actor, reviewed directory and transient proof compositions', () => {
  it.each(['member', 'missing-write'])(
    'requires intrinsic administrator and independent registration.write: %s',
    async (kind) => {
      if (kind === 'member') session!.user.role = 'member'
      else grants = []
      await mount(<LDAPConfiguration />)
      await wait(() => requests.some((row) => row.url === '/auth/permissions'))
      expect(requests.some((row) => row.url === '/admin/auth/ldap')).toBe(false)
      expect(writes()).toHaveLength(0)
    },
  )
  it('saves a name-only review without changing directory proof, binding or enablement and settles actual Session', async () => {
    config.verified = true
    await mount(<LDAPConfiguration />)
    await nameSave()
    expect(writes()).toHaveLength(0)
    expect(document.body.textContent).toContain('Changing only the display name preserves')
    await click('Confirm')
    await wait(() => document.body.textContent!.includes('Current configuration saved.'))
    expect(writes()).toHaveLength(1)
    expect(writes()[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-current')
    expect(JSON.parse(writes()[0].data)).toMatchObject({
      name: 'Directory renamed',
      identity_attribute: 'entryUUID',
      secret_action: 'keep',
      bind_password: '',
    })
    expect(config.verified).toBe(true)
    expect(config.enabled).toBe(false)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('has no guessed attribute on unconfigured state and keeps omitted provisioning controls absent', async () => {
    config = {
      ...config,
      name: '',
      endpoint: '',
      bind_dn: '',
      base_dn: '',
      user_filter: '',
      identity_attribute: '',
      secret_configured: false,
    }
    await mount(<LDAPConfiguration />)
    await openConfig()
    expect(document.querySelector('select')!.value).toBe('')
    expect(document.body.textContent).toContain('Automatic provisioning')
    expect(document.querySelector('input[type="checkbox"]')).toBeNull()
    expect(writes()).toHaveLength(0)
  })
  it('verifies and links the current administrator with separate local/directory proof, without automatically enabling', async () => {
    await mount(<LDAPConfiguration />)
    await openConfig()
    await directoryProof(true)
    await click('Verify directory and link this account')
    expect(writes()).toHaveLength(0)
    await click('Confirm')
    await wait(() =>
      document.body.textContent!.includes('Current directory configuration verified'),
    )
    expect(writes()).toHaveLength(1)
    expect(writes()[0].url).toBe('/admin/auth/ldap/verify')
    expect(JSON.parse(writes()[0].data)).toEqual({
      username: 'alice',
      directory_password: ' directory-proof ',
      password: 'current-password-123',
      proof: {},
      reason: 'Reviewed link',
    })
    expect(config.enabled).toBe(false)
    expect(identity.bound).toBe(true)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('requires a separate reviewed Enable and retains the reason through live language change', async () => {
    config.verified = true
    await mount(<LDAPConfiguration />)
    await openConfig()
    await act(async () => input('Reason', 'Explicit enable', 2))
    await click('Review Enable')
    expect(writes()).toHaveLength(0)
    const reads = requests.filter((row) => row.method === 'get').length
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('确认目录身份变更')
    expect(document.body.textContent).toContain('Explicit enable')
    expect(requests.filter((row) => row.method === 'get')).toHaveLength(reads)
    await click('确认')
    await wait(() => writes().length === 1)
    expect(writes()[0].url).toBe('/admin/auth/ldap/status')
    expect(JSON.parse(writes()[0].data)).toEqual({ enabled: true, reason: 'Explicit enable' })
  })
  it('retains exact uncertain configuration intent and original ETag through current review and explicit retry', async () => {
    await mount(<LDAPConfiguration />)
    await nameSave()
    failures['/admin/auth/ldap'] = 503
    await click('Confirm')
    await wait(() => document.body.textContent!.includes('The submitted outcome is unknown'))
    config.review_etag = 'c'.repeat(64)
    delete failures['/admin/auth/ldap']
    await click('Read current facts')
    await wait(() =>
      cache
        .getQueryCache()
        .findAll({ queryKey: ['ldap', 'config'] })
        .some(
          (query) =>
            query.state.status === 'success' &&
            query.state.fetchStatus === 'idle' &&
            !query.state.isInvalidated &&
            !query.state.error &&
            (query.state.data as LDAPConfig | undefined)?.review_etag === config.review_etag,
        ),
    )
    await wait(() =>
      Array.from(document.querySelectorAll('button')).some(
        (item) => item.textContent === 'Retry original request' && !item.disabled,
      ),
    )
    expect(writes()).toHaveLength(1)
    failures['/admin/auth/ldap'] = 409
    await click('Retry original request')
    await wait(() => writes().length === 2)
    expect(writes()[1].data).toBe(writes()[0].data)
    expect(writes()[1].headers.get('If-Match')).toBe(`"${etag}"`)
  })
  it('never offers proof replay after an uncertain verify; a fresh read cannot prove its historical outcome', async () => {
    failures['/admin/auth/ldap/verify'] = 503
    await mount(<LDAPConfiguration />)
    await openConfig()
    await directoryProof(true)
    await click('Verify directory and link this account')
    await click('Confirm')
    await wait(() => document.body.textContent!.includes('The submitted outcome is unknown'))
    expect(document.body.textContent).not.toContain('Retry original request')
    await click('Read current facts')
    await wait(() =>
      document.body.textContent!.includes('Reading matching current facts does not prove'),
    )
    expect(writes()).toHaveLength(1)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it.each(['actor', 'renewal', 'permission', 'resource', 'expiry', 'unmount'])(
    'fences ignored-abort held configuration on %s',
    async (kind) => {
      const release = hold('/admin/auth/ldap')
      await mount(<LDAPConfiguration />)
      await nameSave()
      await click('Confirm')
      await wait(() => writes().length === 1)
      const dispatched = writes()[0]
      await act(async () => {
        if (kind === 'actor') {
          session = { ...session!, user: { ...session!.user, id: 'usr_bob', role: 'member' } }
          cache.setQueryData(['auth', 'session'], session)
        }
        if (kind === 'renewal') {
          session = { ...session!, csrf_token: 'renewed-csrf' }
          cache.setQueryData(['auth', 'session'], session)
        }
        if (kind === 'permission') cache.setQueryData(['permissions', 'usr_alice'], [])
        if (kind === 'resource')
          cache.setQueriesData<LDAPConfig>({ queryKey: ['ldap', 'config'] }, (old) =>
            old ? { ...old, review_etag: 'c'.repeat(64) } : old,
          )
        if (kind === 'expiry') window.dispatchEvent(new Event('routex:session-expired'))
        if (kind === 'unmount') root.render(<p>Unmounted</p>)
      })
      expect(dispatched.signal?.aborted).toBe(true)
      await act(async () => release())
      expect(document.body.textContent).not.toContain('Current configuration saved.')
      expect(writes()).toHaveLength(1)
    },
  )
  it('blocks duplicate confirmation and captured dismissal while a directory proof is pending', async () => {
    const release = hold('/account/identity/ldap/bind')
    await mount(<LDAPAccount />)
    await prepareBind()
    const confirm = button('Confirm')
    await act(async () => {
      confirm.click()
      confirm.click()
      document.querySelector<HTMLButtonElement>('[aria-label="Close"]')!.click()
    })
    await wait(() => writes().length === 1)
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    await act(async () => release())
  })
  it('keeps native MFA proof independent and changes every visible proof label without losing ordinary input', async () => {
    identity.mfa_required = true
    await mount(<LDAPAccount />)
    await wait(() => document.body.textContent!.includes('Link directory identity'))
    await click('Link directory identity')
    await directoryProof()
    await act(async () => input('6-digit verification code', '123456'))
    const reads = requests.filter((row) => row.method === 'get').length
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('当前本地 RouteX 密码')
    expect(document.body.textContent).toContain('目录密码')
    expect(document.body.textContent).toContain('6 位验证码')
    expect(requests.filter((row) => row.method === 'get')).toHaveLength(reads)
    await click('审查变更')
    await click('确认')
    await wait(() => writes().length === 1)
    expect(JSON.parse(writes()[0].data).proof).toEqual({ code: '123456' })
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('keeps current verification errors generic and requires fresh local proof after an explicit unknown-proof abandonment', async () => {
    failures['/account/identity/ldap/bind'] = 503
    await mount(<LDAPAccount />)
    await prepareBind()
    await click('Confirm')
    await wait(() => document.body.textContent!.includes('The submitted outcome is unknown'))
    expect(document.body.textContent).not.toContain('Retry original request')
    await click('Abandon original request')
    await wait(() => document.body.textContent!.includes('Original request abandoned'))
    expect(
      document.querySelector<HTMLInputElement>('input[autocomplete="current-password"]')?.value ??
        '',
    ).toBe('')
    expect(writes()).toHaveLength(1)
  })
  it('settles exact self-bind with real Session read, clears secrets and never caches proof', async () => {
    await mount(<LDAPAccount />)
    await prepareBind()
    await click('Confirm')
    await wait(() => document.body.textContent!.includes('Current directory identity linked.'))
    expect(writes()).toHaveLength(1)
    expect(writes()[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(document.querySelector('input[type="password"]')).toBeNull()
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it.each(['bind', 'unlink', 'verify', 'disable'])(
    'settles actual revoked Session after %s without preserving private cache',
    async (kind) => {
      sessionLoss = true
      if (kind === 'bind') {
        await mount(<LDAPAccount />)
        await prepareBind()
      }
      if (kind === 'unlink') {
        identity.bound = true
        await mount(<LDAPAccount />)
        await wait(() => document.body.textContent!.includes('Unlink directory identity'))
        await click('Unlink directory identity')
        await act(async () => {
          input('Current local RouteX password', 'current-password-123')
          input('Reason', 'Reviewed unlink')
        })
        await click('Review change')
      }
      if (kind === 'verify') {
        await mount(<LDAPConfiguration />)
        await openConfig()
        await directoryProof(true)
        await click('Verify directory and link this account')
      }
      if (kind === 'disable') {
        config.enabled = true
        config.verified = true
        await mount(<LDAPConfiguration />)
        await openConfig()
        await act(async () => input('Reason', 'Explicit disable', 2))
        await click('Review Disable')
      }
      cache.setQueryData(['private', 'retained'], { private: 'old' })
      await click('Confirm')
      await wait(() => router.state.location.pathname === '/login')
      expect(cache.getQueryData(['auth', 'session'])).toBeNull()
      expect(cache.getQueryData(['private', 'retained'])).toBeUndefined()
      expect(writes()).toHaveLength(1)
    },
  )
  it('does not install a held late settlement after same-owner Session renewal', async () => {
    await mount(<LDAPAccount />)
    await prepareBind()
    const release = holdSession()
    await click('Confirm')
    await wait(() => writes().length === 1)
    await act(async () => {
      session = { ...session!, csrf_token: 'external-renewed' }
      cache.setQueryData(['auth', 'session'], session)
    })
    await act(async () => release())
    expect(cache.getQueryData<Session>(['auth', 'session'])?.csrf_token).toBe('external-renewed')
    expect(document.body.textContent).not.toContain('Current directory identity linked.')
  })
  it('uses native MFA202 without authenticated navigation and completes only after the native proof endpoint', async () => {
    session = null
    loginStatus = 202
    loginData = {
      mfa_required: true,
      challenge_token: 'a'.repeat(43),
      expires_at: new Date(Date.now() + 300_000).toISOString(),
      methods: ['totp', 'recovery_code'],
    }
    cache.setQueryData(['auth', 'session'], null)
    await mount(<AuthPage mode="login" />)
    await loginForm()
    await click('Sign in to directory')
    await wait(() => document.querySelector('input[autocomplete="one-time-code"]') !== null)
    expect(router.state.location.pathname).toBe('/test')
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
    expect(writes()).toHaveLength(1)
    loginData = {
      user: { id: 'usr_alice', name: 'Alice', email: 'alice@example.test', role: 'member' },
      csrf_token: 'mfa-csrf',
    }
    const code = document.querySelector<HTMLInputElement>('input[autocomplete="one-time-code"]')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        code,
        '123456',
      )
      code.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () =>
      code.form!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    await wait(() => router.state.location.pathname === '/')
    expect(writes().map((row) => row.url)).toEqual(['/auth/ldap/login', '/auth/mfa/verify'])
  })
  it('installs only the Session200 result from one real native login while preserving independent local/OIDC/OAuth entrypoints', async () => {
    session = null
    cache.setQueryData(['auth', 'session'], null)
    await mount(<AuthPage mode="login" />)
    await wait(() => document.body.textContent!.includes('Continue with Existing OIDC'))
    expect(document.body.textContent).toContain('Continue with Existing OAuth')
    expect(document.querySelector('input[name="email"]')).not.toBeNull()
    await loginForm()
    await click('Sign in to directory')
    await wait(() => router.state.location.pathname === '/')
    expect(cache.getQueryData<Session>(['auth', 'session'])?.csrf_token).toBe('ldap-csrf')
    expect(writes()).toHaveLength(1)
  })
  it('preserves native local login independently of available LDAP', async () => {
    session = null
    cache.setQueryData(['auth', 'session'], null)
    await mount(<AuthPage mode="login" />)
    await wait(() => document.querySelector('input[name="email"]') !== null)
    const form = document.querySelector<HTMLInputElement>('input[name="email"]')!.form!
    await act(async () => {
      for (const [name, value] of [
        ['email', 'alice@example.test'],
        ['password', 'current-password-123'],
      ]) {
        const field = form.querySelector<HTMLInputElement>(`[name="${name}"]`)!
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
          field,
          value,
        )
        field.dispatchEvent(new Event('input', { bubbles: true }))
      }
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    await wait(() => router.state.location.pathname === '/')
    expect(writes().map((row) => row.url)).toEqual(['/auth/login'])
  })
  it('does not replay unknown native LDAP login and clears transient passwords on live language/close', async () => {
    session = null
    cache.setQueryData(['auth', 'session'], null)
    failures['/auth/ldap/login'] = 503
    await mount(<AuthPage mode="login" />)
    await loginForm()
    const reads = requests.filter((row) => row.method === 'get').length
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('目录登录：Directory')
    expect(requests.filter((row) => row.method === 'get')).toHaveLength(reads)
    await click('登录目录')
    await wait(() => document.body.textContent!.includes('登录结果未知'))
    expect(
      document.querySelector<HTMLInputElement>('input[autocomplete="current-password"]')?.value ??
        '',
    ).not.toBe(' directory-proof ')
    expect(writes()).toHaveLength(1)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('fences a held directory login if a valid current Session appears before reply', async () => {
    session = null
    cache.setQueryData(['auth', 'session'], null)
    const release = hold('/auth/ldap/login')
    await mount(<AuthPage mode="login" />)
    await loginForm()
    await click('Sign in to directory')
    await wait(() => writes().length === 1)
    const current = {
      user: { id: 'usr_bob', name: 'Bob', email: 'bob@example.test', role: 'member' as const },
      csrf_token: 'other-csrf',
    }
    await act(async () => cache.setQueryData(['auth', 'session'], current))
    expect(writes()[0].signal?.aborted).toBe(true)
    await act(async () => release())
    expect(cache.getQueryData(['auth', 'session'])).toEqual(current)
    expect(router.state.location.pathname).toBe('/test')
  })
})

// Capture the actual rendered callback rather than dispatching a DOM click after
// React has detached the button; this exercises admission from retained closures.
function capturedConfirm() {
  const element = button('Confirm')
  const key = Object.getOwnPropertyNames(element).find((name) => name.startsWith('__reactProps$'))
  expect(key).toBeDefined()
  const props = (element as unknown as Record<string, { onClick?: () => void }>)[key!]
  expect(props.onClick).toBeTypeOf('function')
  return { element, invoke: props.onClick! }
}
it.each(['account', 'config'] as const)(
  'rejects a captured %s proof Confirm after synchronous cancellation and a separate new review',
  async (scope) => {
    if (scope === 'account') {
      await mount(<LDAPAccount />)
      await prepareBind()
    } else {
      await mount(<LDAPConfiguration />)
      await openConfig()
      await directoryProof(true)
      await click('Verify directory and link this account')
    }
    const captured = capturedConfirm()
    const close = captured.element
      .closest('[role="dialog"]')!
      .querySelector<HTMLButtonElement>('[aria-label="Close"]')!
    await act(async () => {
      close.click()
      captured.invoke()
    })
    expect(writes()).toHaveLength(0)
    if (scope === 'account') await click('Review change')
    else await click('Verify directory and link this account')
    await act(async () => captured.invoke())
    expect(writes()).toHaveLength(0)
    await click('Confirm')
    await wait(() => writes().length === 1)
    expect(writes()[0].url).toBe(
      scope === 'account' ? '/account/identity/ldap/bind' : '/admin/auth/ldap/verify',
    )
  },
)
it.each(['account', 'config'] as const)(
  'rejects a captured %s proof Confirm after unmount even while another observer retains genuine read authority',
  async (scope) => {
    if (scope === 'account') {
      await mount(<LDAPAccount />)
      await prepareBind()
    } else {
      await mount(<LDAPConfiguration />)
      await openConfig()
      await directoryProof(true)
      await click('Verify directory and link this account')
    }
    const captured = capturedConfirm()
    const query = cache
      .getQueryCache()
      .findAll({ queryKey: ['ldap', scope === 'account' ? 'identity' : 'config'] })
      .find((item) => item.state.status === 'success' && !item.state.isInvalidated)!
    expect(query).toBeDefined()
    const data = query.state.data
    const observer = new QueryObserver(cache, { queryKey: query.queryKey, enabled: false })
    const unsubscribe = observer.subscribe(() => {})
    try {
      await act(async () => root.render(<p>Unmounted</p>))
      expect(cache.getQueryState(query.queryKey)?.status).toBe('success')
      expect(cache.getQueryData(query.queryKey)).toBe(data)
      expect(cache.getQueryData<Session>(['auth', 'session'])?.user.id).toBe('usr_alice')
      await act(async () => captured.invoke())
      expect(writes()).toHaveLength(0)
    } finally {
      unsubscribe()
    }
  },
)
