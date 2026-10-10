import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import routes from '@/router'
import OIDCConfiguration from './config'
import OIDCAccount from './account'
import OIDCComplete from './complete'
import OIDCLoginButton from './login-button'
import type { Session } from '@/types/auth'
import type { OIDCConfig, OIDCIdentity } from '@/types/oidc'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const adapter = client.defaults.adapter
const etag = 'a'.repeat(64)
let host: HTMLDivElement,
  root: Root,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let session: Session | null, grants: string[], config: OIDCConfig, identity: OIDCIdentity
let unlinkSessionLoss: boolean
let configSessionLoss: 'save' | 'status' | null
let sessionReplies: number
let heldSession: { promise: Promise<void>; release: () => void } | undefined
let requests: InternalAxiosRequestConfig[],
  failures: Record<string, number>,
  completeResult: unknown,
  completeStatus: number
let held: { path: string; promise: Promise<void>; release: () => void } | undefined
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
    name: 'Enterprise',
    issuer: 'https://identity.example.test',
    client_id: 'routex',
    callback_url: 'https://routex.example.test/api/v1/auth/oidc/callback',
    secret_configured: true,
    enabled: false,
    verified: false,
    mfa_required: false,
    review_etag: etag,
  }
  identity = {
    available: true,
    name: 'Enterprise',
    bound: false,
    mfa_required: false,
    review_etag: etag,
  }
  requests = []
  failures = {}
  unlinkSessionLoss = false
  configSessionLoss = null
  sessionReplies = 0
  heldSession = undefined
  completeResult = { kind: 'bound' }
  completeStatus = 200
  held = undefined
  client.defaults.adapter = async (request) => {
    requests.push(request)
    const path = request.url!
    if (held?.path === path && request.method !== 'get') await held.promise
    let status = failures[path] ?? 200
    let data: unknown = {}
    if (path === '/auth/session') {
      data = session ? structuredClone(session) : null
      if (!data) status = 401
      if (heldSession) await heldSession.promise
      sessionReplies++
    } else if (path === '/auth/permissions') data = { permissions: grants }
    else if (path === '/auth/oidc') data = { available: true, name: 'Enterprise' }
    else if (path === '/admin/auth/oidc') {
      if (request.method === 'put' && status === 200) {
        const body = JSON.parse(request.data)
        config = {
          ...config,
          name: body.name,
          issuer: body.issuer,
          client_id: body.client_id,
          callback_url: body.callback_url,
          review_etag: 'b'.repeat(64),
        }
        if (configSessionLoss === 'save') session = null
      }
      data = { ...config }
    } else if (path === '/account/identity') data = { ...identity }
    else if (path === '/account/identity/unlink') {
      identity = { ...identity, bound: false, review_etag: 'b'.repeat(64) }
      data = identity
      if (unlinkSessionLoss) session = null
    } else if (path === '/admin/auth/oidc/status') {
      const body = JSON.parse(request.data)
      config = { ...config, enabled: body.enabled, review_etag: 'b'.repeat(64) }
      data = config
      if (configSessionLoss === 'status') session = null
    } else if (path === '/auth/oidc/complete') {
      data = completeResult
      status = failures[path] ?? completeStatus
    } else if (path === '/auth/mfa/verify')
      data = session ?? {
        user: { id: 'usr_alice', name: 'Alice', email: 'alice@example.test', role: 'member' },
        csrf_token: 'new-csrf',
      }
    else if (
      ['/auth/oidc/start', '/account/identity/bind', '/admin/auth/oidc/verify'].includes(path)
    )
      data = { authorization_url: 'https://identity.example.test/authorize?state=server-state' }
    const response = {
      config: request,
      status,
      statusText: '',
      data,
      headers: new AxiosHeaders({
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
  client.defaults.adapter = adapter
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
  for (let count = 0; count < 100; count++) {
    if (check()) return
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
  }
  expect(check()).toBe(true)
}
function button(text: string) {
  const element = Array.from(document.querySelectorAll('button')).find(
    (value) => value.textContent === text,
  )
  expect(element, text).toBeDefined()
  return element!
}
async function click(text: string) {
  await act(async () => button(text).click())
}
function input(label: string, value: string) {
  const element = Array.from(document.querySelectorAll('label'))
    .find((item) => item.textContent === label)
    ?.querySelector('input')
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
const writes = () => requests.filter((request) => request.method !== 'get')
async function openConfig() {
  await wait(() => document.body.textContent!.includes('Configure enterprise SSO'))
  await click('Configure enterprise SSO')
}
async function prepareNameSave() {
  await openConfig()
  await act(async () => {
    input('Display name', 'Enterprise renamed')
    input('Reason', 'Reviewed display name only')
  })
  await click('Review configuration')
}
describe('OIDC existing compositions and lifetimes', () => {
  it('requires independent intrinsic administrator and fresh registration authority before configuration reads', async () => {
    session!.user.role = 'member'
    await mount(<OIDCConfiguration />)
    await wait(() => requests.some((request) => request.url === '/auth/permissions'))
    expect(requests.some((request) => request.url === '/admin/auth/oidc')).toBe(false)
    expect(document.body.textContent).not.toContain('Configure enterprise SSO')
  })
  it('requires registration.write even for an intrinsic administrator', async () => {
    grants = []
    await mount(<OIDCConfiguration />)
    await wait(() => requests.some((request) => request.url === '/auth/permissions'))
    expect(requests.some((request) => request.url === '/admin/auth/oidc')).toBe(false)
    expect(writes()).toHaveLength(0)
  })
  it('confirms a name-only exact reviewed write without a secret, binding reset or automatic Enable/Verify', async () => {
    config.verified = true
    await mount(<OIDCConfiguration />)
    await prepareNameSave()
    expect(writes()).toHaveLength(0)
    expect(document.body.textContent).toContain('Changing only the display name preserves')
    await click('Confirm')
    await wait(() => writes().length === 1)
    const request = writes()[0]
    expect(request.url).toBe('/admin/auth/oidc')
    expect(request.headers.get('If-Match')).toBe(`"${etag}"`)
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf-current')
    expect(JSON.parse(request.data)).toEqual({
      name: 'Enterprise renamed',
      issuer: config.issuer,
      client_id: config.client_id,
      callback_url: config.callback_url,
      secret_action: 'keep',
      client_secret: '',
      reason: 'Reviewed display name only',
    })
    await wait(() => document.body.textContent!.includes('Current configuration saved.'))
    expect(config.verified).toBe(true)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('retains unknown exact save intent across current review and refuses to substitute a new token on retry', async () => {
    await mount(<OIDCConfiguration />)
    await prepareNameSave()
    failures['/admin/auth/oidc'] = 503
    await click('Confirm')
    await wait(() => document.body.textContent!.includes('The submitted outcome is unknown'))
    config.review_etag = 'c'.repeat(64)
    delete failures['/admin/auth/oidc']
    await click('Read current facts')
    await wait(() => document.body.textContent!.includes('Retry original request'))
    failures['/admin/auth/oidc'] = 409
    await click('Retry original request')
    await wait(() => writes().length === 2)
    expect(writes()[0].data).toBe(writes()[1].data)
    expect(writes()[1].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(document.body.textContent).toContain(
      'Reading matching current facts does not prove the original operation',
    )
  })
  it('synchronously fences captured close and duplicate confirmation while the save is held', async () => {
    const release = hold('/admin/auth/oidc')
    await mount(<OIDCConfiguration />)
    await prepareNameSave()
    const confirm = button('Confirm')
    const close = document.querySelector<HTMLButtonElement>('[aria-label="Close"]')!
    await act(async () => {
      confirm.click()
      close.click()
      confirm.click()
    })
    await wait(() => writes().length === 1)
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    release()
  })
  it('rejects an ignored-abort late name response after actor change without resurrecting old private reads', async () => {
    const release = hold('/admin/auth/oidc')
    await mount(<OIDCConfiguration />)
    await prepareNameSave()
    await click('Confirm')
    await wait(() => writes().length === 1)
    await act(async () => {
      session = { ...session!, user: { ...session!.user, id: 'usr_bob', role: 'member' } }
      cache.setQueryData(['auth', 'session'], session)
      cache.removeQueries({ queryKey: ['oidc'] })
    })
    release()
    await act(async () => {
      await Promise.resolve()
    })
    expect(document.body.textContent).not.toContain('Current configuration saved.')
    expect(
      cache
        .getQueryCache()
        .getAll()
        .some(
          (query) =>
            query.queryKey[0] === 'oidc' &&
            query.queryKey.includes('usr_alice') &&
            query.state.data !== undefined,
        ),
    ).toBe(false)
  })
  it('lets a read-authorized ordinary member review self-binding without registration.write and changes language live', async () => {
    session!.user.role = 'member'
    grants = []
    await mount(<OIDCAccount />)
    await wait(() => document.body.textContent!.includes('Link identity'))
    await click('Link identity')
    expect(document.body.textContent).toContain('Email is not used to link accounts')
    await act(async () => {
      input('Current password', 'local password proof')
      input('Reason', 'Explicit self link')
      await i18n.changeLanguage('zh')
    })
    expect(document.body.textContent).toContain('关联身份')
    expect(document.querySelector<HTMLInputElement>('input[type="password"]')?.value).toBe(
      'local password proof',
    )
    expect(writes()).toHaveLength(0)
  })
  it('clears and hides self identity on expiry before a captured callback can dispatch', async () => {
    await mount(<OIDCAccount />)
    await wait(() => document.body.textContent!.includes('Link identity'))
    await click('Link identity')
    await act(async () => {
      input('Current password', 'local password proof')
      input('Reason', 'Explicit self link')
    })
    await click('Review change')
    const confirm = button('Confirm')
    await act(async () => {
      window.dispatchEvent(new Event('routex:session-expired'))
      confirm.click()
    })
    expect(writes()).toHaveLength(0)
    expect(document.querySelector('input[type="password"]')).toBeNull()
    expect(document.body.textContent).not.toContain('Not linked')
  })
  it('refreshes the real Session after unlink and signs out a revoked OIDC caller', async () => {
    identity.bound = true
    unlinkSessionLoss = true
    await mount(<OIDCAccount />)
    await wait(() => document.body.textContent!.includes('Unlink identity'))
    await click('Unlink identity')
    await act(async () => {
      input('Current password', 'local password proof')
      input('Reason', 'Remove this binding')
    })
    await click('Review change')
    expect(writes()).toHaveLength(0)
    await click('Confirm')
    await wait(() => router.state.location.pathname === '/login')
    expect(writes()).toHaveLength(1)
    expect(writes()[0].url).toBe('/account/identity/unlink')
    expect(writes()[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(JSON.parse(writes()[0].data)).toEqual({
      password: 'local password proof',
      proof: {},
      reason: 'Remove this binding',
    })
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
    expect(
      cache
        .getQueryCache()
        .getAll()
        .some((query) => query.queryKey[0] === 'oidc' && query.state.data !== undefined),
    ).toBe(false)
  })
  it('retains exact uncertain config after same-owner renewal and never accepts the late response as saved', async () => {
    const release = hold('/admin/auth/oidc')
    await mount(<OIDCConfiguration />)
    await prepareNameSave()
    await click('Confirm')
    await wait(() => writes().length === 1)
    await act(async () => {
      session = { ...session!, csrf_token: 'renewed-csrf' }
      cache.setQueryData(['auth', 'session'], session)
    })
    release()
    await wait(() => document.body.textContent!.includes('Retry original request'))
    expect(document.body.textContent).not.toContain('Current configuration saved.')
    failures['/admin/auth/oidc'] = 409
    await click('Retry original request')
    await wait(() => writes().length === 2)
    expect(writes()[1].data).toBe(writes()[0].data)
    expect(writes()[1].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(writes()[1].headers.get('X-CSRF-Token')).toBe('renewed-csrf')
  })
  it('shows security-reset consequences only for security fields and translates the open confirmation', async () => {
    await mount(<OIDCConfiguration />)
    await openConfig()
    await act(async () => {
      input('Issuer URL', 'https://new-identity.example.test')
      input('Reason', 'Change identity authority')
    })
    await click('Review configuration')
    expect(document.body.textContent).toContain(
      'removes existing bindings and revokes dependent OIDC Sessions',
    )
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('移除现有关联')
    expect(document.body.textContent).toContain('确认身份变更')
    expect(writes()).toHaveLength(0)
  })
  it('keeps the production completion route outside login/private redirect gates for an authenticated caller', async () => {
    cache.setQueryData(['auth', 'session'], session)
    router = createMemoryRouter(routes, { initialEntries: ['/auth/oidc/complete'] })
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await wait(() =>
      Array.from(document.querySelectorAll('button')).some(
        (element) => element.textContent === 'Continue',
      ),
    )
    expect(router.state.location.pathname).toBe('/auth/oidc/complete')
    expect(requests.some((request) => request.url === '/auth/session')).toBe(false)
    expect(writes()).toHaveLength(0)
  })
  it('does not auto-consume completion and reauthorizes the original current Session before fixed bind navigation', async () => {
    await mount(<OIDCComplete />, '/auth/oidc/complete')
    expect(writes()).toHaveLength(0)
    await click('Continue')
    await wait(() => router.state.location.pathname === '/account/security')
    expect(writes()).toHaveLength(1)
    expect(writes()[0].url).toBe('/auth/oidc/complete')
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-current')
    expect(JSON.parse(writes()[0].data)).toEqual({})
    expect(requests.findIndex((request) => request.url === '/auth/session')).toBeGreaterThanOrEqual(
      0,
    )
    expect(requests.findIndex((request) => request.url === '/auth/session')).toBeLessThan(
      requests.findIndex((request) => request.url === '/auth/oidc/complete'),
    )
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('keeps MFA202 transient and never navigates or installs a Session before the real proof', async () => {
    session = null
    completeStatus = 202
    completeResult = {
      mfa_required: true,
      challenge_token: 'a'.repeat(43),
      expires_at: new Date(Date.now() + 60000).toISOString(),
      methods: ['totp', 'recovery_code'],
    }
    cache.setQueryData(['auth', 'session'], null)
    await mount(<OIDCComplete />, '/auth/oidc/complete')
    await click('Continue')
    await wait(() => document.querySelector('input[name="code"]') !== null)
    expect(router.state.location.pathname).toBe('/auth/oidc/complete')
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((query) => query.state.data),
      ),
    ).not.toContain('challenge_token')
    await act(async () => {
      const element = document.querySelector<HTMLInputElement>('input[name="code"]')!
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        element,
        '123456',
      )
      element.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () =>
      document
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    await wait(() => router.state.location.pathname === '/')
    expect(requests.filter((request) => request.url === '/auth/mfa/verify')).toHaveLength(1)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('rejects a completion response after same-owner Session renewal and never replays the consumed request', async () => {
    const release = hold('/auth/oidc/complete')
    await mount(<OIDCComplete />, '/auth/oidc/complete')
    await click('Continue')
    await wait(() => writes().length === 1)
    await act(async () =>
      cache.setQueryData(['auth', 'session'], { ...session!, csrf_token: 'renewed-csrf' }),
    )
    release()
    await act(async () => {
      await Promise.resolve()
    })
    expect(router.state.location.pathname).toBe('/auth/oidc/complete')
    expect(document.body.textContent).toContain('Unable to confirm this completion')
    expect(writes()).toHaveLength(1)
    expect(
      Array.from(document.querySelectorAll('button')).some(
        (element) => element.textContent === 'Continue',
      ),
    ).toBe(false)
  })
  it('does not install or navigate from an unmounted completion response', async () => {
    session = null
    cache.setQueryData(['auth', 'session'], null)
    completeResult = {
      user: { id: 'usr_alice', name: 'Alice', email: 'alice@example.test', role: 'member' },
      csrf_token: 'new-csrf',
    }
    const release = hold('/auth/oidc/complete')
    await mount(<OIDCComplete />, '/auth/oidc/complete')
    await click('Continue')
    await wait(() => writes().length === 1)
    await act(async () => router.navigate('/login'))
    release()
    await act(async () => {
      await Promise.resolve()
    })
    expect(router.state.location.pathname).toBe('/login')
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
  })
  it('prevents enterprise start when the existing password form owns the synchronous dispatch lock', async () => {
    session = null
    await mount(<OIDCLoginButton acquire={() => null} />)
    await wait(() => document.body.textContent!.includes('Continue with Enterprise'))
    await click('Continue with Enterprise')
    expect(writes()).toHaveLength(0)
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('使用 Enterprise 登录')
  })
})

async function prepareStatusSave() {
  await openConfig()
  await act(async () => {
    const section = Array.from(document.querySelectorAll('section')).find((element) =>
      element.textContent?.includes('Sign-in availability'),
    )!
    const element = section.querySelector('input')!
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      element,
      'Explicit configuration disable',
    )
    element.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await click('Review Disable')
}
function holdSessionRead() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  heldSession = { promise, release }
  return release
}
it.each(['save', 'status'] as const)(
  'settles a revoked OIDC caller after successful %s without retaining private caches',
  async (kind) => {
    configSessionLoss = kind
    config.enabled = true
    config.verified = true
    cache.setQueryData(['unrelated-private'], { recorded: true })
    await mount(<OIDCConfiguration />)
    if (kind === 'save') {
      await openConfig()
      await act(async () => {
        input('Issuer URL', 'https://replacement.example.test')
        input('Reason', 'Explicit authority replacement')
      })
      await click('Review configuration')
    } else await prepareStatusSave()
    await click('Confirm')
    await wait(() => router.state.location.pathname === '/login')
    expect(writes()).toHaveLength(1)
    expect(writes()[0].url).toBe(kind === 'save' ? '/admin/auth/oidc' : '/admin/auth/oidc/status')
    expect(writes()[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(2)
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
    expect(cache.getQueryData(['unrelated-private'])).toBeUndefined()
    expect(
      cache
        .getQueryCache()
        .getAll()
        .some((query) => query.queryKey[0] === 'oidc' && query.state.data !== undefined),
    ).toBe(false)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    expect(document.body.textContent).not.toContain('Current configuration saved.')
  },
)
it('keeps a retained local caller only after fresh Session settlement of the exact successful save', async () => {
  await mount(<OIDCConfiguration />)
  await prepareNameSave()
  await click('Confirm')
  await wait(() => document.body.textContent!.includes('Current configuration saved.'))
  expect(router.state.location.pathname).toBe('/test')
  expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(2)
  expect(cache.getQueryData(['auth', 'session'])).toEqual(session)
  expect(writes()).toHaveLength(1)
  expect(JSON.parse(writes()[0].data).name).toBe('Enterprise renamed')
  expect(document.body.textContent).not.toContain('Retry original request')
})
it('installs a fresh renewed local Session only after settling the acknowledged write and clearing old private facts', async () => {
  await mount(<OIDCConfiguration />)
  await prepareNameSave()
  cache.setQueryData(['unrelated-private'], { recorded: true })
  session = { ...session!, csrf_token: 'fresh-settled-csrf' }
  await click('Confirm')
  await wait(
    () => cache.getQueryData<Session>(['auth', 'session'])?.csrf_token === 'fresh-settled-csrf',
  )
  expect(router.state.location.pathname).toBe('/test')
  expect(cache.getQueryData(['unrelated-private'])).toBeUndefined()
  expect(writes()).toHaveLength(1)
  expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-current')
  expect(document.body.textContent).not.toContain('The submitted outcome is unknown')
  expect(document.body.textContent).not.toContain('Retry original request')
})
it.each(['actor', 'session', 'unmount'] as const)(
  'ignores an obsolete null Session settlement after %s change without clearing current private state',
  async (change) => {
    configSessionLoss = 'save'
    await mount(<OIDCConfiguration />)
    await prepareNameSave()
    const release = holdSessionRead()
    await click('Confirm')
    await wait(() => requests.filter((request) => request.url === '/auth/session').length === 2)
    const next: Session = {
      user: {
        id: change === 'actor' ? 'usr_bob' : 'usr_alice',
        name: 'Alice',
        email: 'alice@example.test',
        role: change === 'actor' ? 'member' : 'admin',
      },
      csrf_token: 'new-owner-csrf',
    }
    await act(async () => {
      if (change === 'unmount') await router.navigate('/account/security')
      else cache.setQueryData(['auth', 'session'], next)
      cache.setQueryData(['current-private'], { current: true })
    })
    release()
    await wait(() => sessionReplies === 2)
    expect(router.state.location.pathname).toBe(
      change === 'unmount' ? '/account/security' : '/test',
    )
    expect(cache.getQueryData(['current-private'])).toEqual({ current: true })
    if (change !== 'unmount') expect(cache.getQueryData(['auth', 'session'])).toEqual(next)
    expect(document.body.textContent).not.toContain('Current configuration saved.')
    expect(writes()).toHaveLength(1)
  },
)
it('retains the original intent when acknowledged write Session settlement is unavailable', async () => {
  await mount(<OIDCConfiguration />)
  await prepareNameSave()
  failures['/auth/session'] = 503
  await click('Confirm')
  await wait(() => cache.getQueryState(['auth', 'session'])?.status === 'error')
  expect(document.body.textContent).not.toContain('Configure enterprise SSO')
  expect(document.body.textContent).not.toContain('Current configuration saved.')
  delete failures['/auth/session']
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['auth', 'session'] })
  })
  await wait(() => document.body.textContent!.includes('Retry original request'))
  expect(document.body.textContent).toContain('The submitted outcome is unknown')
  expect(writes()).toHaveLength(1)
  expect(writes()[0].headers.get('If-Match')).toBe(`"${etag}"`)
})

it.each(['actor', 'session', 'expiry', 'error', 'unmount'] as const)(
  'discards a held initial completion Session read after %s without restoring shared identity or dispatching',
  async (change) => {
    const prior = structuredClone(session!)
    cache.setQueryData(['auth', 'session'], prior)
    const release = holdSessionRead()
    await mount(<OIDCComplete />, '/auth/oidc/complete')
    expect(requests.filter((request) => request.url?.startsWith('/auth/'))).toHaveLength(0)
    await click('Continue')
    await wait(() => requests.filter((request) => request.url === '/auth/session').length === 1)
    const next: Session | null =
      change === 'expiry'
        ? null
        : {
            user: {
              ...prior.user,
              id: change === 'actor' ? 'usr_bob' : prior.user.id,
              name: change === 'actor' ? 'Bob' : prior.user.name,
            },
            csrf_token: 'new-current-csrf',
          }
    await act(async () => {
      if (change === 'unmount') await router.navigate('/account/security')
      if (change === 'error') {
        cache
          .getQueryCache()
          .find({ queryKey: ['auth', 'session'] })!
          .setState({
            status: 'error',
            error: new Error('Session unavailable'),
            errorUpdateCount: 1,
          })
      } else cache.setQueryData(['auth', 'session'], next)
      if (change === 'expiry') window.dispatchEvent(new Event('routex:session-expired'))
      cache.setQueryData(['current-private'], { current: true })
    })
    release()
    await wait(() => sessionReplies === 1)
    expect(writes()).toHaveLength(0)
    expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(1)
    expect(cache.getQueryData(['current-private'])).toEqual({ current: true })
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    if (change === 'error') {
      expect(cache.getQueryData(['auth', 'session'])).toEqual(prior)
      expect(cache.getQueryState(['auth', 'session'])?.status).toBe('error')
    } else expect(cache.getQueryData(['auth', 'session'])).toEqual(next)
    expect(router.state.location.pathname).toBe(
      change === 'unmount' ? '/account/security' : '/auth/oidc/complete',
    )
    if (change !== 'unmount') {
      expect(document.body.textContent).toContain('Unable to confirm this completion')
      expect(
        Array.from(document.querySelectorAll('button')).some(
          (element) => element.textContent === 'Continue',
        ),
      ).toBe(false)
    }
  },
)
it('installs only the confirmed login Session after a transient fresh anonymous read', async () => {
  session = null
  const authenticated: Session = {
    user: { id: 'usr_alice', name: 'Alice', email: 'alice@example.test', role: 'member' },
    csrf_token: 'confirmed-login-csrf',
  }
  completeResult = authenticated
  cache.setQueryData(['current-private'], { old: true })
  await mount(<OIDCComplete />, '/auth/oidc/complete')
  expect(requests.filter((request) => request.url?.startsWith('/auth/'))).toHaveLength(0)
  expect(cache.getQueryData(['auth', 'session'])).toBeUndefined()
  await click('Continue')
  await wait(() => router.state.location.pathname === '/')
  expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(1)
  expect(writes()).toHaveLength(1)
  expect(writes()[0].url).toBe('/auth/oidc/complete')
  expect(writes()[0].headers.get('X-CSRF-Token')).toBeUndefined()
  expect(JSON.parse(writes()[0].data)).toEqual({})
  expect(cache.getQueryData(['auth', 'session'])).toEqual(authenticated)
  expect(cache.getQueryData(['current-private'])).toBeUndefined()
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it('uses fresh original binding authority without publishing the preliminary Session read', async () => {
  cache.setQueryData(['current-private'], { current: true })
  await mount(<OIDCComplete />, '/auth/oidc/complete')
  expect(requests.filter((request) => request.url?.startsWith('/auth/'))).toHaveLength(0)
  await click('Continue')
  await wait(() => router.state.location.pathname === '/account/security')
  expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(1)
  expect(writes()).toHaveLength(1)
  expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-current')
  expect(JSON.parse(writes()[0].data)).toEqual({})
  expect(cache.getQueryData(['auth', 'session'])).toBeUndefined()
  expect(cache.getQueryData(['current-private'])).toEqual({ current: true })
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
