import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider, QueryObserver } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosHeaders, type InternalAxiosRequestConfig, type AxiosAdapter } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import routes from '@/router'
import GoogleConfiguration from './config'
import GoogleAccount from './account'
import GoogleComplete from './complete'
import AuthPage from '@/views/auth'
import GitHubConfiguration from '@/views/github/config'
import enGoogle from '@/i18n/locales/en/google'
import zhGoogle from '@/i18n/locales/zh/google'
import enGitHub from '@/i18n/locales/en/github'
import type { Session } from '@/types/auth'
import type { GoogleConfig, GoogleIdentity } from '@/types/google'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter,
  etag = 'a'.repeat(64)
let host: HTMLDivElement,
  root: Root,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let session: Session | null, grants: string[], config: GoogleConfig, identity: GoogleIdentity
let requests: InternalAxiosRequestConfig[], failures: Record<string, number>
let completion: unknown, completionStatus: number, loseSession: boolean
let held: { path: string; promise: Promise<void>; release: () => void } | undefined
let sessionHold: { promise: Promise<void>; release: () => void } | undefined
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
  requests = []
  failures = {}
  held = undefined
  sessionHold = undefined
  loseSession = false
  config = {
    name: 'Google sign-in',
    client_id: 'client-id',
    callback_url: 'https://routex.example.test/api/v1/auth/google/callback',
    secret_configured: true,
    enabled: false,
    verified: false,
    mfa_required: false,
    review_etag: etag,
  }
  identity = {
    available: true,
    name: config.name,
    bound: false,
    mfa_required: false,
    review_etag: etag,
  }
  completion = { kind: 'bound' }
  completionStatus = 200
  client.defaults.adapter = async (request) => {
    requests.push(request)
    const path = request.url!
    if (held?.path === path && request.method !== 'get') await held.promise
    let status = failures[path] ?? 200,
      data: unknown = {}
    if (path === '/auth/session') {
      data = session ? structuredClone(session) : null
      if (!data) status = 401
      if (sessionHold) await sessionHold.promise
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
    else if (
      ['/auth/oidc', '/auth/oauth', '/auth/ldap', '/auth/saml', '/auth/github'].includes(path)
    )
      data = { available: false, name: '' }
    else if (path === '/auth/google') data = { available: true, name: config.name }
    else if (path === '/admin/auth/google') {
      if (request.method === 'put' && status === 200) {
        const body = JSON.parse(request.data)
        const security =
          body.client_id !== config.client_id ||
          body.callback_url !== config.callback_url ||
          body.secret_action === 'replace'
        config = {
          ...config,
          name: body.name,
          client_id: body.client_id,
          callback_url: body.callback_url,
          secret_configured: true,
          review_etag: 'b'.repeat(64),
          ...(security ? { enabled: false, verified: false } : {}),
        }
        if (loseSession) session = null
      }
      data = { ...config }
    } else if (path === '/admin/auth/google/status') {
      if (status === 200) {
        config = {
          ...config,
          enabled: JSON.parse(request.data).enabled,
          review_etag: 'b'.repeat(64),
        }
        if (loseSession) session = null
      }
      data = { ...config }
    } else if (path === '/account/identity/google') data = { ...identity }
    else if (path === '/account/identity/google/unlink') {
      if (status === 200) {
        identity = { ...identity, bound: false, review_etag: 'b'.repeat(64) }
        if (loseSession) session = null
      }
      data = { ...identity }
    } else if (path === '/auth/google/complete') {
      data = completion
      status = failures[path] ?? completionStatus
    } else if (path === '/auth/google/abandon') {
      status = failures[path] ?? 204
      data = ''
    } else if (path === '/auth/mfa/verify')
      data = {
        user: { id: 'usr_alice', name: 'Alice', email: 'alice@example.test', role: 'member' },
        csrf_token: 'new-csrf',
      }
    else if (
      ['/auth/google/start', '/admin/auth/google/verify', '/account/identity/google/bind'].includes(
        path,
      )
    ) {
      // Deliberately reject navigation after proving the real request was sent.
      // Success navigation and Lax correlation cookie semantics require real browser acceptance.
      status = failures[path] ?? 503
    }
    return {
      config: request,
      status,
      statusText: '',
      data,
      headers: new AxiosHeaders({
        'Cache-Control': 'private, no-store',
        'X-Content-Type-Options': 'nosniff',
        'Referrer-Policy': 'no-referrer',
        ETag: `"${path.startsWith('/account/') ? identity.review_etag : config.review_etag}"`,
      }),
    }
  }
})
afterEach(async () => {
  held?.release()
  sessionHold?.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  document.querySelectorAll('[data-base-ui-portal]').forEach((node) => node.remove())
  client.defaults.adapter = original
})
async function mount(node: ReactNode, path = '/test') {
  router = createMemoryRouter(
    [
      { path, element: node },
      { path: '/', element: <p>Workspace</p> },
      { path: '/login', element: <p>Local sign-in</p> },
      { path: '/account/security', element: <p>Security destination</p> },
      { path: '/admin/auth', element: <p>Authentication destination</p> },
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
  for (let n = 0; n < 100; n++) {
    if (check()) return
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
  }
  expect(check()).toBe(true)
}
const findButton = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((node) => node.textContent === label)
function button(label: string) {
  const node = findButton(label)
  expect(node, label).toBeDefined()
  return node!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
function input(label: string, value: string, index = 0) {
  const labels = Array.from(document.querySelectorAll('label')).filter(
    (node) => node.textContent === label,
  )
  const node = labels[index]?.querySelector('input,textarea')
  expect(node, label).toBeDefined()
  const proto =
    node instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype
  Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(node, value)
  node!.dispatchEvent(new Event('input', { bubbles: true }))
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
  sessionHold = { promise, release }
  return release
}
const writes = () => requests.filter((request) => request.method !== 'get')
async function openConfig() {
  await wait(() => !!findButton('Configure Google sign-in'))
  await click('Configure Google sign-in')
}
async function nameSave() {
  await openConfig()
  await act(async () => {
    input('Display name', 'Renamed Google')
    input('Reason', 'Reviewed name only')
  })
  await click('Review configuration')
}
async function localProof(index = 0) {
  await act(async () => {
    input('Current password', 'current-password-123')
    input('Reason', 'Reviewed identity change', index)
  })
}
async function prepareAccount() {
  await wait(() => !!findButton(identity.bound ? 'Unlink identity' : 'Link identity'))
  await click(identity.bound ? 'Unlink identity' : 'Link identity')
  await localProof()
  await click('Review change')
}
function capturedConfirm() {
  const node = button('Confirm'),
    key = Object.getOwnPropertyNames(node).find((name) => name.startsWith('__reactProps$'))!
  expect(key).toBeDefined()
  const callback = (node as unknown as Record<string, { onClick: () => void }>)[key].onClick
  expect(callback).toBeTypeOf('function')
  return { node, callback }
}
describe('Google application composition and authority', () => {
  it.each(['member', 'missing-permission'] as const)('denies config reads for %s', async (mode) => {
    if (mode === 'member') session!.user.role = 'member'
    else grants = []
    await mount(<GoogleConfiguration />)
    await wait(() => requests.some((row) => row.url === '/auth/permissions'))
    expect(requests.some((row) => row.url === '/admin/auth/google')).toBe(false)
  })
  it('uses current strong review and no mutation cache, then settles the real Session after name save', async () => {
    await mount(<GoogleConfiguration />)
    await nameSave()
    expect(writes()).toHaveLength(0)
    await click('Confirm')
    await wait(
      () =>
        writes().length === 1 && requests.filter((row) => row.url === '/auth/session').length >= 2,
    )
    expect(JSON.parse(writes()[0].data)).toMatchObject({
      name: 'Renamed Google',
      reason: 'Reviewed name only',
    })
    expect(writes()[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-current')
    expect(
      writes().some(
        (row) => row.url === '/admin/auth/google/verify' || row.url === '/admin/auth/google/status',
      ),
    ).toBe(false)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('retains exact uncertain save body and review token after reading current facts', async () => {
    await mount(<GoogleConfiguration />)
    await nameSave()
    failures['/admin/auth/google'] = 503
    await click('Confirm')
    await wait(
      () =>
        !!findButton('Retry original request') && !findButton('Retry original request')!.disabled,
    )
    const body = writes()[0].data
    delete failures['/admin/auth/google']
    await click('Read current facts')
    await wait(
      () =>
        !!findButton('Retry original request') && !findButton('Retry original request')!.disabled,
    )
    failures['/admin/auth/google'] = 409
    await click('Retry original request')
    await wait(() => writes().length === 2)
    expect(writes()[1].data).toBe(body)
    expect(writes()[1].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(document.body.textContent).not.toContain('Current configuration saved.')
  })
  it('translates an open security confirmation without clearing public configuration or changing request', async () => {
    await mount(<GoogleConfiguration />)
    await openConfig()
    await act(async () => {
      input('Client ID', 'changed-client')
      input('Reason', 'Reviewed client change')
    })
    await click('Review configuration')
    expect(document.body.textContent).toContain('removes existing bindings')
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('确认身份变更')
    expect(document.body.textContent).toContain('移除现有关联')
    expect(
      Array.from(document.querySelectorAll('input')).some(
        (node) => node.value === 'Google sign-in',
      ),
    ).toBe(true)
    expect(writes()).toHaveLength(0)
  })
  it('keeps verification and explicit Enable distinct, and invalidates a revoked caller Session after disable', async () => {
    config = { ...config, enabled: true, verified: true }
    loseSession = true
    await mount(<GoogleConfiguration />)
    await openConfig()
    await act(async () => input('Reason', 'Reviewed disable', 2))
    await act(async () => document.querySelector<HTMLButtonElement>('[role="switch"]')!.click())
    await click('Confirm')
    await wait(() => router.state.location.pathname === '/login')
    expect(writes()).toHaveLength(1)
    expect(writes()[0].url).toBe('/admin/auth/google/status')
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
  })
  it('requires a reviewed local/native proof start without treating failure as linked or enabling', async () => {
    await mount(<GoogleAccount />)
    await prepareAccount()
    await click('Confirm')
    await wait(() => writes().length === 1)
    expect(writes()[0].url).toBe('/account/identity/google/bind')
    expect(JSON.parse(writes()[0].data)).toEqual({
      password: 'current-password-123',
      proof: {},
      reason: 'Reviewed identity change',
    })
    await wait(() => document.body.textContent!.includes('submitted outcome is unknown'))
    expect(document.body.textContent).not.toContain('Current identity linked.')
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('unlinks the reviewed exact binding and settles null Session before navigation', async () => {
    identity.bound = true
    loseSession = true
    await mount(<GoogleAccount />)
    await prepareAccount()
    await click('Confirm')
    await wait(() => router.state.location.pathname === '/login')
    expect(writes()[0].url).toBe('/account/identity/google/unlink')
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
    expect(writes()).toHaveLength(1)
  })
  it.each(['account', 'config'] as const)(
    'rejects retained %s Confirm after Cancel and a different current review',
    async (scope) => {
      if (scope === 'account') {
        await mount(<GoogleAccount />)
        await prepareAccount()
      } else {
        await mount(<GoogleConfiguration />)
        await openConfig()
        await localProof(1)
        await click('Verify callback and link this account')
      }
      const old = capturedConfirm(),
        close = old.node
          .closest('[role="dialog"]')!
          .querySelector<HTMLButtonElement>('[aria-label="Close"]')!
      await act(async () => {
        close.click()
        old.callback()
      })
      expect(writes()).toHaveLength(0)
      await click(scope === 'account' ? 'Review change' : 'Verify callback and link this account')
      await act(async () => old.callback())
      expect(writes()).toHaveLength(0)
      await click('Confirm')
      await wait(() => writes().length === 1)
    },
  )
  it.each(['account', 'config'] as const)(
    'rejects retained %s Confirm after unmount despite retained real query authority',
    async (scope) => {
      if (scope === 'account') {
        await mount(<GoogleAccount />)
        await prepareAccount()
      } else {
        await mount(<GoogleConfiguration />)
        await openConfig()
        await localProof(1)
        await click('Verify callback and link this account')
      }
      const old = capturedConfirm(),
        query = cache
          .getQueryCache()
          .findAll({ queryKey: ['google', scope === 'account' ? 'identity' : 'config'] })
          .find((row) => row.state.status === 'success' && !row.state.isInvalidated)!
      const observer = new QueryObserver(cache, { queryKey: query.queryKey, enabled: false }),
        unsubscribe = observer.subscribe(() => {})
      try {
        await act(async () => root.render(<p>Unmounted</p>))
        expect(cache.getQueryState(query.queryKey)?.status).toBe('success')
        await act(async () => old.callback())
        expect(writes()).toHaveLength(0)
      } finally {
        unsubscribe()
      }
    },
  )
  it('aborts held configuration completion on external actor renewal and never restores private state', async () => {
    const release = hold('/admin/auth/google')
    await mount(<GoogleConfiguration />)
    await nameSave()
    await click('Confirm')
    await wait(() => writes().length === 1)
    const next = { ...session!, user: { ...session!.user, id: 'usr_bob' }, csrf_token: 'bob-csrf' }
    await act(async () => cache.setQueryData(['auth', 'session'], next))
    expect(writes()[0].signal?.aborted).toBe(true)
    await act(async () => release())
    expect(cache.getQueryData(['auth', 'session'])).toEqual(next)
    expect(document.body.textContent).not.toContain('Current configuration saved.')
  })
  it('retains a duplicate-operation lock while the exact reviewed command is pending', async () => {
    const release = hold('/admin/auth/google')
    await mount(<GoogleConfiguration />)
    await nameSave()
    const retained = capturedConfirm()
    await act(async () => {
      retained.callback()
      retained.callback()
    })
    await wait(() => writes().length === 1)
    expect(writes()).toHaveLength(1)
    await act(async () => release())
    expect(writes()).toHaveLength(1)
  })
  it('aborts a pending sign-in start when the current public method changes without replay', async () => {
    session = null
    cache.setQueryData(['auth', 'session'], null)
    const release = hold('/auth/google/start')
    await mount(<AuthPage mode="login" />)
    await wait(() => !!findButton('Continue with Google sign-in'))
    await click('Continue with Google sign-in')
    await wait(() => writes().length === 1)
    await act(async () =>
      cache.setQueryData(['google', 'public'], { available: true, name: 'Renewed method' }),
    )
    expect(writes()[0].signal?.aborted).toBe(true)
    await act(async () => release())
    expect(router.state.location.pathname).toBe('/test')
    expect(writes()).toHaveLength(1)
  })
  it('adds Google to the existing local login card without auto-start or changing local/other-method inputs', async () => {
    session = null
    cache.setQueryData(['auth', 'session'], null)
    await mount(<AuthPage mode="login" />)
    await wait(() => !!findButton('Continue with Google sign-in'))
    expect(document.querySelector('input[name="email"]')).not.toBeNull()
    expect(writes()).toHaveLength(0)
    await click('Continue with Google sign-in')
    await wait(() => writes().length === 1)
    expect(writes()[0].url).toBe('/auth/google/start')
    expect(JSON.parse(writes()[0].data)).toEqual({})
  })
})
describe('Google manual clean completion', () => {
  it('registers the clean route outside AuthGate and never reads/consumes on mount', async () => {
    cache.setQueryData(['auth', 'session'], session)
    router = createMemoryRouter(routes, { initialEntries: ['/auth/google/complete'] })
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await wait(() => !!findButton('Continue'))
    expect(router.state.location.pathname).toBe('/auth/google/complete')
    expect(writes()).toHaveLength(0)
    expect(requests.some((row) => row.url === '/auth/session')).toBe(false)
  })
  it('manually reads fresh Session, posts empty completion once with fresh CSRF, then settles binding success', async () => {
    await mount(<GoogleComplete />, '/auth/google/complete')
    expect(writes()).toHaveLength(0)
    await click('Continue')
    await wait(() => router.state.location.pathname === '/account/security')
    expect(writes()).toHaveLength(1)
    expect(writes()[0].url).toBe('/auth/google/complete')
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-current')
    expect(JSON.parse(writes()[0].data)).toEqual({})
    expect(requests.filter((row) => row.url === '/auth/session')).toHaveLength(2)
  })
  it('keeps native MFA202 transient and waits for a real proof before Session installation/navigation', async () => {
    session = null
    cache.setQueryData(['auth', 'session'], null)
    completionStatus = 202
    completion = {
      mfa_required: true,
      challenge_token: 'a'.repeat(43),
      expires_at: new Date(Date.now() + 60000).toISOString(),
      methods: ['totp', 'recovery_code'],
    }
    await mount(<GoogleComplete />, '/auth/google/complete')
    await click('Continue')
    await wait(() => !!document.querySelector('input[name="code"]'))
    expect(router.state.location.pathname).toBe('/auth/google/complete')
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((row) => row.state.data),
      ),
    ).not.toContain('challenge_token')
    await act(async () => {
      const node = document.querySelector<HTMLInputElement>('input[name="code"]')!
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        node,
        '123456',
      )
      node.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () =>
      document
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    await wait(() => router.state.location.pathname === '/')
    expect(writes().map((row) => row.url)).toEqual(['/auth/google/complete', '/auth/mfa/verify'])
  })
  it.each(['renewal', 'removal', 'expiry', 'unmount'] as const)(
    'fences held pre-completion Session read after %s',
    async (mode) => {
      cache.setQueryData(['auth', 'session'], session)
      const release = holdSession()
      await mount(<GoogleComplete />, '/auth/google/complete')
      await click('Continue')
      await wait(() => requests.some((row) => row.url === '/auth/session'))
      await act(async () => {
        if (mode === 'renewal')
          cache.setQueryData(['auth', 'session'], { ...session!, csrf_token: 'renewed' })
        else if (mode === 'removal') cache.removeQueries({ queryKey: ['auth', 'session'] })
        else if (mode === 'expiry') window.dispatchEvent(new Event('routex:session-expired'))
        else root.render(<p>Unmounted</p>)
      })
      await act(async () => release())
      expect(writes()).toHaveLength(0)
      expect(router.state.location.pathname).toBe('/auth/google/complete')
    },
  )
  it('retains uncertain completion without replay and clears browser proofs only after a confirmed abandonment204', async () => {
    failures['/auth/google/complete'] = 503
    failures['/auth/google/abandon'] = 503
    await mount(<GoogleComplete />, '/auth/google/complete')
    await click('Continue')
    await wait(() => !!findButton('Return to sign-in'))
    expect(writes()).toHaveLength(1)
    await click('Return to sign-in')
    await wait(() => writes().length === 2 && !findButton('Return to sign-in')?.disabled)
    expect(router.state.location.pathname).toBe('/auth/google/complete')
    expect(writes()[1].url).toBe('/auth/google/abandon')
    failures['/auth/google/abandon'] = 204
    await click('Return to sign-in')
    await wait(() => router.state.location.pathname === '/login')
    expect(writes().filter((row) => row.url === '/auth/google/complete')).toHaveLength(1)
    expect(writes()[2].headers.get('X-CSRF-Token')).toBe('csrf-current')
    expect(JSON.parse(writes()[2].data)).toEqual({})
  })
  it('clears an expired native challenge without automatic proof abandonment or sign-in replay', async () => {
    session = null
    cache.setQueryData(['auth', 'session'], null)
    completionStatus = 202
    completion = {
      mfa_required: true,
      challenge_token: 'a'.repeat(43),
      expires_at: new Date(Date.now() - 1000).toISOString(),
      methods: ['totp', 'recovery_code'],
    }
    await mount(<GoogleComplete />, '/auth/google/complete')
    await click('Continue')
    await wait(() => document.body.textContent!.includes('verification challenge expired'))
    expect(router.state.location.pathname).toBe('/auth/google/complete')
    expect(writes().map((row) => row.url)).toEqual(['/auth/google/complete'])
    expect(document.querySelector('input[name="code"]')).toBeNull()
  })
  it('aborts a held abandonment when current Session authority changes', async () => {
    failures['/auth/google/complete'] = 503
    cache.setQueryData(['auth', 'session'], session)
    await mount(<GoogleComplete />, '/auth/google/complete')
    await click('Continue')
    await wait(() => !!findButton('Return to sign-in'))
    const release = hold('/auth/google/abandon')
    await click('Return to sign-in')
    await wait(() => writes().length === 2)
    await act(async () =>
      cache.setQueryData(['auth', 'session'], { ...session!, csrf_token: 'renewed' }),
    )
    expect(writes()[1].signal?.aborted).toBe(true)
    await act(async () => release())
    expect(router.state.location.pathname).toBe('/auth/google/complete')
    expect(writes()).toHaveLength(2)
  })
  it('rejects an unexpected Session login result when a current Session was already read', async () => {
    completion = { user: { ...session!.user, id: 'usr_other' }, csrf_token: 'switched' }
    await mount(<GoogleComplete />, '/auth/google/complete')
    await click('Continue')
    await wait(() => !!findButton('Return to sign-in'))
    expect(router.state.location.pathname).toBe('/auth/google/complete')
    expect(cache.getQueryData(['auth', 'session'])).toBeUndefined()
  })
})

it('does not guess verifier purpose or block a member bound completion without management permission', async () => {
  session = { ...session!, user: { ...session!.user, role: 'member' } }
  grants = []
  await mount(<GoogleComplete />, '/auth/google/complete')
  await click('Continue')
  await wait(() => router.state.location.pathname === '/account/security')
  expect(writes().map((row) => row.url)).toEqual(['/auth/google/complete'])
  expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-current')
})
it('keeps a verifier completion403 generic without verified navigation or automatic replay', async () => {
  grants = []
  failures['/auth/google/complete'] = 403
  await mount(<GoogleComplete />, '/auth/google/complete')
  await click('Continue')
  await wait(() => !!findButton('Return to sign-in'))
  expect(router.state.location.pathname).toBe('/auth/google/complete')
  expect(writes().map((row) => row.url)).toEqual(['/auth/google/complete'])
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it('retains verification-start uncertainty until explicit browser proof abandonment is confirmed', async () => {
  await mount(<GoogleConfiguration />)
  await openConfig()
  await localProof(1)
  await click('Verify callback and link this account')
  await click('Confirm')
  await wait(() => writes().length === 1)
  await wait(() => !!findButton('Abandon original request'))
  await click('Abandon original request')
  await wait(() => writes().length === 2)
  expect(writes().map((row) => row.url)).toEqual([
    '/admin/auth/google/verify',
    '/auth/google/abandon',
  ])
  expect(JSON.parse(writes()[1].data)).toEqual({})
  expect(writes()[1].headers.get('X-CSRF-Token')).toBe('csrf-current')
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})

it('does not restore configuration after a same-admin permission renewal during save', async () => {
  const release = hold('/admin/auth/google')
  await mount(<GoogleConfiguration />)
  await nameSave()
  await click('Confirm')
  await wait(() => writes().length === 1)
  grants = []
  await act(async () => cache.setQueryData(['permissions', session!.user.id], []))
  expect(writes()[0].signal?.aborted).toBe(true)
  expect(document.body.textContent).not.toContain('Configure Google sign-in')
  await act(async () => release())
  expect(document.body.textContent).not.toContain('Current configuration saved.')
  expect(writes()).toHaveLength(1)
})

it('shares only reviewed view mechanics while preserving independent GitHub and Google drafts and request lifetimes', async () => {
  const originalAdapter = client.defaults.adapter as AxiosAdapter
  client.defaults.adapter = async (request) => {
    if (request.url === '/admin/auth/github' && request.method === 'get') {
      requests.push(request)
      return {
        config: request,
        status: 200,
        statusText: '',
        data: {
          ...config,
          name: 'GitHub sign-in',
          callback_url: 'https://routex.example.test/api/v1/auth/github/callback',
        },
        headers: new AxiosHeaders({
          ETag: `"${etag}"`,
          'Cache-Control': 'private, no-store',
          'X-Content-Type-Options': 'nosniff',
        }),
      }
    }
    return originalAdapter(request)
  }
  await mount(
    <>
      <GitHubConfiguration />
      <GoogleConfiguration />
    </>,
  )
  await wait(
    () => !!findButton('Configure GitHub sign-in') && !!findButton('Configure Google sign-in'),
  )
  const release = hold('/admin/auth/google')
  await nameSave()
  await click('Confirm')
  await wait(() => writes().length === 1)
  const githubQuery = cache
    .getQueryCache()
    .findAll({ queryKey: ['github', 'config'] })
    .find((query) => query.state.status === 'success')!
  await act(async () =>
    cache.setQueryData(githubQuery.queryKey, {
      ...(githubQuery.state.data as object),
      name: 'New GitHub name',
    }),
  )
  expect(writes()[0].url).toBe('/admin/auth/google')
  expect(writes()[0].signal?.aborted).toBe(false)
  expect(JSON.parse(writes()[0].data).name).toBe('Renamed Google')
  await act(async () => release())
  await wait(() => document.body.textContent!.includes('Current configuration saved.'))
  expect(
    requests.filter((request) => request.method !== 'get' && request.url?.includes('github')),
  ).toHaveLength(0)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it('copies setup origin and the saved callback independently without changing callback authority or dispatching a proof', async () => {
  const old = Object.getOwnPropertyDescriptor(navigator, 'clipboard'),
    copied: string[] = []
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: {
      writeText: async (text: string) => {
        copied.push(text)
      },
    },
  })
  try {
    await mount(<GoogleConfiguration />)
    await openConfig()
    expect(document.body.textContent).toContain('Google web client setup')
    await act(async () =>
      input('Callback URL', 'https://other.example.test/api/v1/auth/google/callback'),
    )
    await click('Copy Google origin URL')
    await click('Copy saved callback URL')
    expect(copied).toEqual([window.location.origin, config.callback_url])
    expect(writes()).toHaveLength(0)
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('Google Web 客户端设置')
    expect(document.body.textContent).toContain('复制 Google 来源 URL')
    expect(document.querySelector('[role="switch"]')?.getAttribute('aria-label')).toBe(
      '启用 Google 登录',
    )
    expect(copied).toHaveLength(2)
  } finally {
    if (old) Object.defineProperty(navigator, 'clipboard', old)
    else Reflect.deleteProperty(navigator, 'clipboard')
  }
})
it('preserves every shared key with paired English and Chinese Google copy and exact interpolation', () => {
  expect(Object.keys(enGoogle).sort()).toEqual(Object.keys(zhGoogle).sort())
  for (const key of Object.keys(enGitHub)) expect(Object.hasOwn(enGoogle, key)).toBe(true)
  for (const key of Object.keys(enGoogle) as (keyof typeof enGoogle)[]) {
    expect(enGoogle[key].trim()).not.toBe('')
    expect(zhGoogle[key].trim()).not.toBe('')
    const fields = (value: string) =>
      [...value.matchAll(/{{\s*([^}]+)\s*}}/g)].map((match) => match[1]).sort()
    expect(fields(enGoogle[key])).toEqual(fields(zhGoogle[key]))
  }
})
