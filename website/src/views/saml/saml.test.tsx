import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider, QueryObserver } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import routes from '@/router'
import SAMLConfiguration from './config'
import SAMLAccount from './account'
import SAMLComplete from './complete'
import AuthPage from '@/views/auth'
import type { Session } from '@/types/auth'
import type { SAMLConfig, SAMLIdentity } from '@/types/saml'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter,
  etag = 'a'.repeat(64)
let host: HTMLDivElement,
  root: Root,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let session: Session | null, grants: string[], config: SAMLConfig, identity: SAMLIdentity
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
    name: 'Enterprise SAML',
    idp_issuer: 'urn:example:idp',
    sso_url: 'https://id.example.test/sso',
    sp_entity_id: 'urn:example:routex',
    acs_url: 'https://routex.example.test/api/v1/auth/saml/acs',
    signing_certificate_pem: '-----BEGIN CERTIFICATE-----\nAQID\n-----END CERTIFICATE-----\n',
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
    else if (['/auth/oidc', '/auth/oauth', '/auth/ldap'].includes(path))
      data = { available: false, name: '' }
    else if (path === '/auth/saml') data = { available: true, name: config.name }
    else if (path === '/admin/auth/saml') {
      if (request.method === 'put' && status === 200) {
        const body = JSON.parse(request.data)
        const security = (
          ['idp_issuer', 'sso_url', 'sp_entity_id', 'acs_url', 'signing_certificate_pem'] as const
        ).some((key) => body[key] !== config[key])
        config = {
          ...config,
          ...body,
          review_etag: 'b'.repeat(64),
          ...(security ? { enabled: false, verified: false } : {}),
        }
        delete (config as unknown as Record<string, unknown>).reason
        if (loseSession) session = null
      }
      data = { ...config }
    } else if (path === '/admin/auth/saml/status') {
      if (status === 200) {
        config = {
          ...config,
          enabled: JSON.parse(request.data).enabled,
          review_etag: 'b'.repeat(64),
        }
        if (loseSession) session = null
      }
      data = { ...config }
    } else if (path === '/account/identity/saml') data = { ...identity }
    else if (path === '/account/identity/saml/unlink') {
      if (status === 200) {
        identity = { ...identity, bound: false, review_etag: 'b'.repeat(64) }
        if (loseSession) session = null
      }
      data = { ...identity }
    } else if (path === '/auth/saml/complete') {
      data = completion
      status = failures[path] ?? completionStatus
    } else if (path === '/auth/saml/abandon') {
      status = failures[path] ?? 204
      data = ''
    } else if (path === '/auth/mfa/verify')
      data = {
        user: { id: 'usr_alice', name: 'Alice', email: 'alice@example.test', role: 'member' },
        csrf_token: 'new-csrf',
      }
    else if (
      ['/auth/saml/start', '/admin/auth/saml/verify', '/account/identity/saml/bind'].includes(path)
    ) {
      // Deliberately reject navigation after proving the real request was sent.
      // Success navigation and Strict cookie semantics require real browser acceptance.
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
  await wait(() => !!findButton('Configure SAML'))
  await click('Configure SAML')
}
async function nameSave() {
  await openConfig()
  await act(async () => {
    input('Display name', 'Renamed SAML')
    input('Reason', 'Reviewed name only')
  })
  await click('Review configuration')
}
async function localProof(index = 0) {
  await act(async () => {
    input('Current local RouteX password', 'current-password-123')
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
describe('SAML application composition and authority', () => {
  it.each(['member', 'missing-permission'] as const)('denies config reads for %s', async (mode) => {
    if (mode === 'member') session!.user.role = 'member'
    else grants = []
    await mount(<SAMLConfiguration />)
    await wait(() => requests.some((row) => row.url === '/auth/permissions'))
    expect(requests.some((row) => row.url === '/admin/auth/saml')).toBe(false)
  })
  it('uses current strong review and no mutation cache, then settles the real Session after name save', async () => {
    await mount(<SAMLConfiguration />)
    await nameSave()
    expect(writes()).toHaveLength(0)
    await click('Confirm')
    await wait(
      () =>
        writes().length === 1 && requests.filter((row) => row.url === '/auth/session').length >= 2,
    )
    expect(JSON.parse(writes()[0].data)).toMatchObject({
      name: 'Renamed SAML',
      reason: 'Reviewed name only',
    })
    expect(writes()[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-current')
    expect(
      writes().some(
        (row) => row.url === '/admin/auth/saml/verify' || row.url === '/admin/auth/saml/status',
      ),
    ).toBe(false)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('retains exact uncertain save body and review token after reading current facts', async () => {
    await mount(<SAMLConfiguration />)
    await nameSave()
    failures['/admin/auth/saml'] = 503
    await click('Confirm')
    await wait(
      () =>
        !!findButton('Retry original request') && !findButton('Retry original request')!.disabled,
    )
    const body = writes()[0].data
    delete failures['/admin/auth/saml']
    await click('Read current facts')
    await wait(
      () =>
        !!findButton('Retry original request') && !findButton('Retry original request')!.disabled,
    )
    failures['/admin/auth/saml'] = 409
    await click('Retry original request')
    await wait(() => writes().length === 2)
    expect(writes()[1].data).toBe(body)
    expect(writes()[1].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(document.body.textContent).not.toContain('Current configuration saved.')
  })
  it('translates an open security confirmation without clearing public configuration or changing request', async () => {
    await mount(<SAMLConfiguration />)
    await openConfig()
    await act(async () => {
      input('IdP Entity ID', 'urn:example:changed')
      input('Reason', 'Reviewed IdP change')
    })
    await click('Review configuration')
    expect(document.body.textContent).toContain('removes SAML bindings')
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('确认身份变更')
    expect(document.body.textContent).toContain('移除 SAML 关联')
    expect((document.querySelector('input') as HTMLInputElement).value).toBe('Enterprise SAML')
    expect(writes()).toHaveLength(0)
  })
  it('keeps verification and explicit Enable distinct, and invalidates a revoked caller Session after disable', async () => {
    config = { ...config, enabled: true, verified: true }
    loseSession = true
    await mount(<SAMLConfiguration />)
    await openConfig()
    await act(async () => input('Reason', 'Reviewed disable', 2))
    await click('Review Disable')
    await click('Confirm')
    await wait(() => router.state.location.pathname === '/login')
    expect(writes()).toHaveLength(1)
    expect(writes()[0].url).toBe('/admin/auth/saml/status')
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
  })
  it('requires a reviewed local/native proof start without treating failure as linked or enabling', async () => {
    await mount(<SAMLAccount />)
    await prepareAccount()
    await click('Confirm')
    await wait(() => writes().length === 1)
    expect(writes()[0].url).toBe('/account/identity/saml/bind')
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
    await mount(<SAMLAccount />)
    await prepareAccount()
    await click('Confirm')
    await wait(() => router.state.location.pathname === '/login')
    expect(writes()[0].url).toBe('/account/identity/saml/unlink')
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
    expect(writes()).toHaveLength(1)
  })
  it.each(['account', 'config'] as const)(
    'rejects retained %s Confirm after Cancel and a different current review',
    async (scope) => {
      if (scope === 'account') {
        await mount(<SAMLAccount />)
        await prepareAccount()
      } else {
        await mount(<SAMLConfiguration />)
        await openConfig()
        await localProof(1)
        await click('Verify signed callback and link this account')
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
      await click(
        scope === 'account' ? 'Review change' : 'Verify signed callback and link this account',
      )
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
        await mount(<SAMLAccount />)
        await prepareAccount()
      } else {
        await mount(<SAMLConfiguration />)
        await openConfig()
        await localProof(1)
        await click('Verify signed callback and link this account')
      }
      const old = capturedConfirm(),
        query = cache
          .getQueryCache()
          .findAll({ queryKey: ['saml', scope === 'account' ? 'identity' : 'config'] })
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
    const release = hold('/admin/auth/saml')
    await mount(<SAMLConfiguration />)
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
    const release = hold('/admin/auth/saml')
    await mount(<SAMLConfiguration />)
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
    const release = hold('/auth/saml/start')
    await mount(<AuthPage mode="login" />)
    await wait(() => !!findButton('Continue with Enterprise SAML'))
    await click('Continue with Enterprise SAML')
    await wait(() => writes().length === 1)
    await act(async () =>
      cache.setQueryData(['saml', 'public'], { available: true, name: 'Renewed method' }),
    )
    expect(writes()[0].signal?.aborted).toBe(true)
    await act(async () => release())
    expect(router.state.location.pathname).toBe('/test')
    expect(writes()).toHaveLength(1)
  })
  it('adds SAML to the existing local login card without auto-start or changing local/other-method inputs', async () => {
    session = null
    cache.setQueryData(['auth', 'session'], null)
    await mount(<AuthPage mode="login" />)
    await wait(() => !!findButton('Continue with Enterprise SAML'))
    expect(document.querySelector('input[name="email"]')).not.toBeNull()
    expect(writes()).toHaveLength(0)
    await click('Continue with Enterprise SAML')
    await wait(() => writes().length === 1)
    expect(writes()[0].url).toBe('/auth/saml/start')
    expect(JSON.parse(writes()[0].data)).toEqual({})
  })
})
describe('SAML manual clean completion', () => {
  it('registers the clean route outside AuthGate and never reads/consumes on mount', async () => {
    cache.setQueryData(['auth', 'session'], session)
    router = createMemoryRouter(routes, { initialEntries: ['/auth/saml/complete'] })
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await wait(() => !!findButton('Continue'))
    expect(router.state.location.pathname).toBe('/auth/saml/complete')
    expect(writes()).toHaveLength(0)
    expect(requests.some((row) => row.url === '/auth/session')).toBe(false)
  })
  it('manually reads fresh Session, posts empty completion once with fresh CSRF, then settles binding success', async () => {
    await mount(<SAMLComplete />, '/auth/saml/complete')
    expect(writes()).toHaveLength(0)
    await click('Continue')
    await wait(() => router.state.location.pathname === '/account/security')
    expect(writes()).toHaveLength(1)
    expect(writes()[0].url).toBe('/auth/saml/complete')
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
    await mount(<SAMLComplete />, '/auth/saml/complete')
    await click('Continue')
    await wait(() => !!document.querySelector('input[name="code"]'))
    expect(router.state.location.pathname).toBe('/auth/saml/complete')
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
    expect(writes().map((row) => row.url)).toEqual(['/auth/saml/complete', '/auth/mfa/verify'])
  })
  it.each(['renewal', 'removal', 'expiry', 'unmount'] as const)(
    'fences held pre-completion Session read after %s',
    async (mode) => {
      cache.setQueryData(['auth', 'session'], session)
      const release = holdSession()
      await mount(<SAMLComplete />, '/auth/saml/complete')
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
      expect(router.state.location.pathname).toBe('/auth/saml/complete')
    },
  )
  it('retains uncertain completion without replay and clears browser proofs only after a confirmed abandonment204', async () => {
    failures['/auth/saml/complete'] = 503
    failures['/auth/saml/abandon'] = 503
    await mount(<SAMLComplete />, '/auth/saml/complete')
    await click('Continue')
    await wait(() => !!findButton('Return to sign-in'))
    expect(writes()).toHaveLength(1)
    await click('Return to sign-in')
    await wait(() => writes().length === 2 && !findButton('Return to sign-in')?.disabled)
    expect(router.state.location.pathname).toBe('/auth/saml/complete')
    expect(writes()[1].url).toBe('/auth/saml/abandon')
    failures['/auth/saml/abandon'] = 204
    await click('Return to sign-in')
    await wait(() => router.state.location.pathname === '/login')
    expect(writes().filter((row) => row.url === '/auth/saml/complete')).toHaveLength(1)
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
    await mount(<SAMLComplete />, '/auth/saml/complete')
    await click('Continue')
    await wait(() => document.body.textContent!.includes('verification challenge expired'))
    expect(router.state.location.pathname).toBe('/auth/saml/complete')
    expect(writes().map((row) => row.url)).toEqual(['/auth/saml/complete'])
    expect(document.querySelector('input[name="code"]')).toBeNull()
  })
  it('aborts a held abandonment when current Session authority changes', async () => {
    failures['/auth/saml/complete'] = 503
    cache.setQueryData(['auth', 'session'], session)
    await mount(<SAMLComplete />, '/auth/saml/complete')
    await click('Continue')
    await wait(() => !!findButton('Return to sign-in'))
    const release = hold('/auth/saml/abandon')
    await click('Return to sign-in')
    await wait(() => writes().length === 2)
    await act(async () =>
      cache.setQueryData(['auth', 'session'], { ...session!, csrf_token: 'renewed' }),
    )
    expect(writes()[1].signal?.aborted).toBe(true)
    await act(async () => release())
    expect(router.state.location.pathname).toBe('/auth/saml/complete')
    expect(writes()).toHaveLength(2)
  })
  it('rejects an unexpected Session login result when a current Session was already read', async () => {
    completion = { user: { ...session!.user, id: 'usr_other' }, csrf_token: 'switched' }
    await mount(<SAMLComplete />, '/auth/saml/complete')
    await click('Continue')
    await wait(() => !!findButton('Return to sign-in'))
    expect(router.state.location.pathname).toBe('/auth/saml/complete')
    expect(cache.getQueryData(['auth', 'session'])).toBeUndefined()
  })
})
