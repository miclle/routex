import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import AuthenticationEnforcement from './config'
import AuthPage from '@/views/auth'
import AccountMFA from '@/views/account/mfa'
import ReauthenticationPage from './reauthentication'
import { ReauthenticationProvider } from './reauthentication-provider'
import { useReauthentication } from './reauthentication-state'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
const actor = 'usr_' + '0'.repeat(25) + 'a',
  candidate = 'usr_' + '0'.repeat(25) + 'b'
const birth = '2026-10-11T00:00:00.123456Z',
  initial = 'a'.repeat(64),
  selected = 'b'.repeat(64)
let host: HTMLDivElement,
  root: Root,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>,
  requests: InternalAxiosRequestConfig[]
let role: 'admin' | 'member'
let enforced: boolean,
  policyFailure: number,
  writeFailure: number,
  held: Promise<void> | undefined,
  reauthentication: boolean
const session = {
  user: { id: actor, email: 'admin@example.test', name: 'Admin', role: 'admin' },
  csrf_token: 'fresh',
}
const headers = (tag?: string) =>
  new AxiosHeaders({
    'Cache-Control': 'private, no-store',
    'X-Content-Type-Options': 'nosniff',
    ...(tag ? { ETag: `"${tag}"` } : {}),
  })
function view(candidateReview = false) {
  return {
    enforced,
    emergency_admin_id: enforced ? candidate : null,
    emergency_admin_created_at: enforced ? birth : null,
    emergency_mfa_enabled: enforced ? true : null,
    oidc_available: true,
    ldap_available: true,
    review_emergency_admin: candidateReview
      ? { id: candidate, created_at: birth, mfa_enabled: true }
      : null,
    review_etag: candidateReview ? selected : initial,
  }
}
function ReceiptTrigger() {
  const reauthenticate = useReauthentication()
  return (
    <button
      onClick={() => reauthenticate(Array.from({ length: 10 }, (_, i) => 'RXR-PRIVATE-' + i))}
    >
      Committed security change
    </button>
  )
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  role = 'admin'
  enforced = false
  policyFailure = 0
  writeFailure = 0
  held = undefined
  reauthentication = false
  requests = []
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  cache.setQueryData(['auth', 'session'], session)
  cache.setQueryData(['permissions', actor], ['registration.write'])
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let status = 200,
      data: unknown = {},
      tag: string | undefined
    if (config.url === '/auth/session') data = { ...session, user: { ...session.user, role } }
    else if (config.url === '/auth/permissions') data = { permissions: ['registration.write'] }
    else if (config.url === '/auth/policy') {
      status = policyFailure || 200
      data = { enforced, password_login_enabled: !enforced, emergency_entry_enabled: enforced }
    } else if (config.url?.startsWith('/admin/auth/policy')) {
      if (config.method === 'put') {
        const input = JSON.parse(config.data)
        await held
        status = writeFailure || 200
        if (!writeFailure) enforced = input.enforced
        const result = view(input.enforced)
        tag = result.review_etag
        data = {
          ...result,
          confirmation: 'saved',
          runtime_applied: true,
          reauthentication_required: reauthentication,
        }
      } else {
        const result = view(config.url.includes('?'))
        tag = result.review_etag
        data = result
      }
    } else if (config.url === '/auth/registration')
      data = { enabled: true, approval_required: false, allowed_email_domains: [] }
    else if (config.url === '/auth/emergency/login') {
      status = 202
      data = {
        mfa_required: true,
        challenge_token: 'T'.repeat(43),
        expires_at: new Date(Date.now() + 60000).toISOString(),
        methods: ['totp', 'recovery_code'],
      }
    } else if (config.url === '/auth/emergency/mfa/verify') data = session
    else if (config.url === '/account/mfa')
      data = {
        enabled: true,
        enrollment_available: true,
        enrollment_pending: false,
        recovery_codes_remaining: 10,
      }
    else if (config.url === '/account/mfa/recovery-codes') {
      await held
      data = {
        session: null,
        recovery_codes: Array.from({ length: 10 }, (_, i) => 'RXR-COMMITTED-' + i),
        reauthentication_required: true,
      }
    } else if (config.url?.startsWith('/auth/')) data = { available: false, name: '' }
    else if (config.url === '/site')
      data = {
        name: 'RouteX',
        logo_url: '',
        footer: '',
        default_language: 'en',
        service_url: '',
        etag: 0,
      }
    return { config, status, statusText: '', headers: headers(tag), data }
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
  vi.useRealTimers()
})
async function until(check: () => void) {
  for (let n = 0; n < 80; n++) {
    await act(async () => {
      if (vi.isFakeTimers()) await vi.advanceTimersByTimeAsync(5)
      else await new Promise((r) => setTimeout(r, 5))
    })
    try {
      check()
      return
    } catch (e) {
      if (n === 79) throw e
    }
  }
}
async function mount(path = '/settings') {
  router = createMemoryRouter(
    [
      { path: '/mfa', element: <AccountMFA /> },
      { path: '/settings', element: <AuthenticationEnforcement /> },
      { path: '/login', element: <AuthPage mode="login" /> },
      { path: '/login/emergency', element: <AuthPage mode="emergency" /> },
      { path: '/register', element: <AuthPage mode="register" /> },
      { path: '/auth/reauthenticate', element: <ReauthenticationPage /> },
      { path: '/receipt', element: <ReceiptTrigger /> },
      { path: '/', element: <p>Authenticated destination</p> },
    ],
    { initialEntries: [path] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <ReauthenticationProvider router={router}>
          <RouterProvider router={router} />
        </ReauthenticationProvider>
      </QueryClientProvider>,
    ),
  )
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (n) => n.textContent === label,
  )!
}
async function click(label: string) {
  expect(button(label), label).toBeTruthy()
  await act(async () => button(label).click())
  await act(async () => {
    if (vi.isFakeTimers()) await vi.advanceTimersByTimeAsync(0)
    else await new Promise((r) => setTimeout(r, 0))
  })
}
async function fill(selector: string, value: string) {
  const node = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(selector)!
  await act(async () => {
    const proto =
      node instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(node, value)
    node.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
const writes = () => requests.filter((r) => r.method === 'put')
async function prepareEnable() {
  await until(() => expect(button('Configure forced SSO')?.disabled).toBe(false))
  await click('Configure forced SSO')
  await act(async () => document.querySelector<HTMLButtonElement>('[role="switch"]')!.click())
  await fill('input[aria-label="Emergency administrator ID"]', candidate)
  await until(() => expect(button('Review current policy')?.disabled).toBe(false))
  await click('Review current policy')
  await fill('textarea', 'Reviewed emergency designation')
  await click('Review change')
}
it('requires an exact candidate review and explicit confirmation, preserving live Chinese draft', async () => {
  await mount()
  await prepareEnable()
  expect(writes()).toHaveLength(0)
  expect(requests.some((r) => r.url === '/admin/auth/policy?emergency_admin_id=' + candidate)).toBe(
    true,
  )
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.body.textContent).toContain(i18n.t('authEnforcement:confirmTitle'))
  expect(document.querySelector('textarea')?.value).toBe('Reviewed emergency designation')
  await act(async () => i18n.changeLanguage('en'))
  await click('Confirm change')
  await until(() => expect(writes()).toHaveLength(1))
  expect(JSON.parse(writes()[0].data)).toEqual({
    enforced: true,
    emergency_admin_id: candidate,
    reason: 'Reviewed emergency designation',
  })
  expect(writes()[0].headers.get('If-Match')).toBe(`"${selected}"`)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it('disables with fresh no-query review, never the stored designation candidate ETag', async () => {
  enforced = true
  await mount()
  await until(() => expect(button('Configure forced SSO')?.disabled).toBe(false))
  await click('Configure forced SSO')
  await act(async () => document.querySelector<HTMLButtonElement>('[role="switch"]')!.click())
  await until(() => expect(button('Review current policy')?.disabled).toBe(false))
  await click('Review current policy')
  await fill('textarea', 'Explicit policy off')
  await click('Review change')
  await click('Confirm change')
  await until(() => expect(writes()).toHaveLength(1))
  expect(writes()[0].headers.get('If-Match')).toBe(`"${initial}"`)
  expect(JSON.parse(writes()[0].data).emergency_admin_id).toBe('')
})
it('preserves the identical uncertain policy intent and original review on explicit retry', async () => {
  writeFailure = 503
  await mount()
  await prepareEnable()
  await click('Confirm change')
  await until(() => expect(button('Retry original change')).toBeTruthy())
  const first = writes()[0]
  writeFailure = 409
  await click('Retry original change')
  await until(() => expect(writes()).toHaveLength(2))
  expect(writes()[1].data).toBe(first.data)
  expect(writes()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
  expect(document.body.textContent).not.toContain('The authentication policy was saved.')
})
it('ignores a held save after Session renewal and hides all policy facts on authority loss', async () => {
  let release!: () => void
  held = new Promise<void>((r) => {
    release = r
  })
  await mount()
  await prepareEnable()
  await click('Confirm change')
  await until(() => expect(writes()).toHaveLength(1))
  await act(async () =>
    cache.setQueryData(['auth', 'session'], { ...session, csrf_token: 'renewed' }),
  )
  await act(async () => release())
  await until(() => expect(button('Retry original change')).toBeTruthy())
  expect(document.body.textContent).not.toContain('The authentication policy was saved.')
  await act(async () => cache.setQueryData(['permissions', actor], []))
  expect(document.body.textContent).not.toContain(candidate)
  expect(button('Configure forced SSO')).toBeUndefined()
})
it('settles a committed self-revoking save before private cache cleanup without protected reread', async () => {
  reauthentication = true
  cache.setQueryData(['private', 'fact'], 'hidden')
  await mount()
  await prepareEnable()
  await click('Confirm change')
  await until(() => expect(router.state.location.pathname).toBe('/auth/reauthenticate'))
  expect(cache.getQueryData(['auth', 'session'])).toBeNull()
  expect(cache.getQueryData(['private', 'fact'])).toBeUndefined()
  expect(document.body.textContent).toContain('The change was saved.')
  const count = requests.filter((r) => r.url?.startsWith('/admin/auth/policy')).length
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0))
  })
  expect(requests.filter((r) => r.url?.startsWith('/admin/auth/policy'))).toHaveLength(count)
})
it('hides ordinary password, registration and excluded methods while enforced; renewal failure hides stale choices', async () => {
  enforced = true
  await mount('/login')
  await until(() => expect(host.textContent).toContain('Sign in through verified OIDC or LDAP.'))
  expect(document.querySelector('input[name="password"]')).toBeNull()
  expect(document.querySelector('a[href="/register"]')).toBeNull()
  for (const name of ['oauth', 'github', 'google', 'discord', 'telegram', 'saml'])
    expect(requests.some((r) => r.url === '/auth/' + name)).toBe(false)
  expect(document.querySelector('a[href="/login/emergency"]')).not.toBeNull()
  policyFailure = 503
  await act(async () => cache.invalidateQueries({ queryKey: ['auth', 'enforcement'] }))
  await until(() => expect(host.textContent).toContain('Authentication policy is unavailable.'))
  expect(document.querySelector('a[href="/login/emergency"]')).toBeNull()
})
it('keeps emergency202 transient and only installs a Session after native recovery proof', async () => {
  enforced = true
  cache.setQueryData(['auth', 'session'], null)
  await mount('/login/emergency')
  await until(() => expect(document.querySelector('input[name="password"]')).not.toBeNull())
  await fill('[name="email"]', 'admin@example.test')
  await fill('[name="password"]', 'current password')
  await click('Emergency administrator sign-in')
  await until(() => expect(host.textContent).toContain('Verify your sign-in'))
  expect(cache.getQueryData(['auth', 'session'])).toBeNull()
  expect(router.state.location.pathname).toBe('/login/emergency')
  await click('Use a recovery code')
  await fill('[name="recovery_code"]', 'RXR-PRIVATE-PROOF')
  await click('Verify and sign in')
  await until(() => expect(router.state.location.pathname).toBe('/'))
  expect(JSON.parse(requests.find((r) => r.url === '/auth/emergency/mfa/verify')!.data)).toEqual({
    challenge_token: 'T'.repeat(43),
    recovery_code: 'RXR-PRIVATE-PROOF',
  })
  expect(
    JSON.stringify(
      cache
        .getQueryCache()
        .getAll()
        .map((q) => q.state.data),
    ),
  ).not.toContain('RXR-PRIVATE-PROOF')
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it('retains recovery codes only in transient holding state, clears on departure, and never reloads a receipt', async () => {
  cache.setQueryData(['private', 'fact'], 'hidden')
  await mount('/receipt')
  await click('Committed security change')
  await until(() => expect(router.state.location.pathname).toBe('/auth/reauthenticate'))
  expect(host.textContent).toContain('RXR-PRIVATE-0')
  expect(cache.getQueryData(['auth', 'session'])).toBeNull()
  expect(
    JSON.stringify(
      cache
        .getQueryCache()
        .getAll()
        .map((q) => q.state.data),
    ),
  ).not.toContain('RXR-PRIVATE-0')
  expect(JSON.stringify(localStorage)).not.toContain('RXR-PRIVATE-0')
  expect(router.state.location.state).toBeNull()
  await click('Sign in again')
  await until(() => expect(router.state.location.pathname).toBe('/login'))
  await act(async () => router.navigate('/auth/reauthenticate'))
  await until(() => expect(router.state.location.pathname).toBe('/login'))
  expect(host.textContent).not.toContain('RXR-PRIVATE-0')
})

it('shows actual committed MFA recovery codes after immediate Session clearing and requires explicit sign-in', async () => {
  await mount('/mfa')
  await until(() => expect(button('Regenerate recovery codes')).toBeTruthy())
  const originalSessionQuery = cache
    .getQueryCache()
    .find({ queryKey: ['auth', 'session'], exact: true })
  const sessionReadCount = requests.filter((request) => request.url === '/auth/session').length
  await until(() => expect(button('Regenerate recovery codes')).toBeTruthy())
  await click('Regenerate recovery codes')
  await fill('[name="mfa_password"]', 'current password')
  await fill('[name="mfa_code"]', '123456')
  await click('Replace recovery codes')
  await until(() => expect(router.state.location.pathname).toBe('/auth/reauthenticate'))
  expect(host.textContent).toContain('RXR-COMMITTED-0')
  expect(cache.getQueryData(['auth', 'session'])).toBeNull()
  expect(document.querySelectorAll('li')).toHaveLength(10)
  expect(cache.getQueryCache().find({ queryKey: ['auth', 'session'], exact: true })).toBe(
    originalSessionQuery,
  )
  expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(
    sessionReadCount,
  )
  expect(
    JSON.stringify(
      cache
        .getQueryCache()
        .getAll()
        .map((q) => q.state.data),
    ),
  ).not.toContain('RXR-COMMITTED-0')
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  expect(router.state.location.pathname).not.toBe('/login')
  await click('Sign in again')
  await until(() => expect(router.state.location.pathname).toBe('/login'))
  expect(host.textContent).not.toContain('RXR-COMMITTED-0')
})
it('drops held MFA recovery results after a renewed Session without restoring codes or authenticated state', async () => {
  let release!: () => void
  held = new Promise<void>((r) => {
    release = r
  })
  await mount('/mfa')
  await until(() => expect(button('Regenerate recovery codes')).toBeTruthy())
  await click('Regenerate recovery codes')
  await fill('[name="mfa_password"]', 'current password')
  await fill('[name="mfa_code"]', '123456')
  await click('Replace recovery codes')
  await until(() =>
    expect(requests.filter((r) => r.url === '/account/mfa/recovery-codes')).toHaveLength(1),
  )
  const renewed = { ...session, csrf_token: 'new-generation' }
  await act(async () => cache.setQueryData(['auth', 'session'], renewed))
  await act(async () => release())
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0))
  })
  expect(cache.getQueryData(['auth', 'session'])).toEqual(renewed)
  expect(router.state.location.pathname).toBe('/mfa')
  expect(document.body.textContent).not.toContain('RXR-COMMITTED-0')
  expect(document.querySelector('[name="mfa_password"]')).toBeNull()
})

it.each(['member', 'missing-permission'])(
  'does not read or write management policy for %s authority',
  async (condition) => {
    if (condition === 'member') {
      role = 'member'
      cache.setQueryData(['auth', 'session'], { ...session, user: { ...session.user, role } })
    } else cache.setQueryData(['permissions', actor], [])
    await mount()
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })
    expect(requests.some((r) => r.url?.startsWith('/admin/auth/policy'))).toBe(false)
    expect(button('Configure forced SSO')).toBeUndefined()
    expect(writes()).toHaveLength(0)
  },
)
it('clears a holding receipt at expiry and cannot recover it from the direct route', async () => {
  vi.useFakeTimers()
  await mount('/receipt')
  await click('Committed security change')
  await until(() => expect(host.textContent).toContain('RXR-PRIVATE-0'))
  await act(async () => vi.advanceTimersByTimeAsync(300001))
  await until(() => expect(router.state.location.pathname).toBe('/login'))
  expect(host.textContent).not.toContain('RXR-PRIVATE-0')
  vi.useRealTimers()
  await act(async () => router.navigate('/auth/reauthenticate'))
  await until(() => expect(router.state.location.pathname).toBe('/login'))
})
it('destroys a holding receipt when a different authenticated actor replaces the anonymous cache', async () => {
  await mount('/receipt')
  await click('Committed security change')
  await until(() => expect(host.textContent).toContain('RXR-PRIVATE-0'))
  await act(async () =>
    cache.setQueryData(['auth', 'session'], {
      ...session,
      user: { ...session.user, id: candidate },
    }),
  )
  await until(() => expect(router.state.location.pathname).toBe('/login'))
  expect(host.textContent).not.toContain('RXR-PRIVATE-0')
})

// Retain the actual rendered callback to exercise dismissal without relying on
// React to replace a browser event handler before the old closure is invoked.
function retainedConfirmation() {
  const node = button('Confirm change'),
    key = Object.getOwnPropertyNames(node).find((name) => name.startsWith('__reactProps$'))!
  expect(key).toBeDefined()
  const callback = (node as unknown as Record<string, { onClick: () => void }>)[key].onClick
  expect(callback).toBeTypeOf('function')
  return callback
}
it('refuses a retained confirmation after Cancel and admits only a new explicit review', async () => {
  await mount()
  await prepareEnable()
  const prior = retainedConfirmation()
  await click('Cancel')
  await act(async () => prior())
  expect(writes()).toHaveLength(0)
  await fill('textarea', 'A newly reviewed designation')
  await click('Review change')
  await act(async () => prior())
  expect(writes()).toHaveLength(0)
  await click('Confirm change')
  await until(() => expect(writes()).toHaveLength(1))
  expect(JSON.parse(writes()[0].data).reason).toBe('A newly reviewed designation')
})
it('refuses a retained confirmation after its configuration drawer is dismissed', async () => {
  await mount()
  await prepareEnable()
  const prior = retainedConfirmation()
  await click('Cancel')
  const close = document.querySelector<HTMLButtonElement>('button[aria-label="Close"]')!
  expect(close).toBeTruthy()
  await act(async () => close.click())
  await act(async () => prior())
  expect(writes()).toHaveLength(0)
  await prepareEnable()
  await act(async () => prior())
  expect(writes()).toHaveLength(0)
  await click('Confirm change')
  await until(() => expect(writes()).toHaveLength(1))
})

it.each([
  ['uppercase alias', 'usr_' + '0'.repeat(25) + 'B'],
  ['overflow leading digit', 'usr_8' + '0'.repeat(24) + 'b'],
])('does not review or dispatch a %s emergency designation', async (_label, invalidID) => {
  await mount()
  await until(() => expect(button('Configure forced SSO')?.disabled).toBe(false))
  await click('Configure forced SSO')
  await act(async () => document.querySelector<HTMLButtonElement>('[role="switch"]')!.click())
  await fill('input[aria-label="Emergency administrator ID"]', invalidID)
  await fill('textarea', 'Reviewed only after exact candidate admission')
  expect(requests.some((r) => r.url?.includes('emergency_admin_id='))).toBe(false)
  expect(button('Review change')?.disabled).toBe(true)
  expect(writes()).toHaveLength(0)
  // A fresh canonical selection still requires its own reviewed ETag and consent.
  await fill('input[aria-label="Emergency administrator ID"]', candidate)
  await until(() => expect(button('Review current policy')?.disabled).toBe(false))
  await click('Review current policy')
  await click('Review change')
  expect(writes()).toHaveLength(0)
  await click('Confirm change')
  await until(() => expect(writes()).toHaveLength(1))
  expect(JSON.parse(writes()[0].data).emergency_admin_id).toBe(candidate)
  expect(writes()[0].headers.get('If-Match')).toBe(`"${selected}"`)
})

it('reviews only the browser-normalized text value after a pasted newline, with explicit consent still required', async () => {
  await mount()
  await until(() => expect(button('Configure forced SSO')?.disabled).toBe(false))
  await click('Configure forced SSO')
  await act(async () => document.querySelector<HTMLButtonElement>('[role="switch"]')!.click())
  await fill('input[aria-label="Emergency administrator ID"]', candidate + '\n')
  expect(
    document.querySelector<HTMLInputElement>('input[aria-label="Emergency administrator ID"]')!
      .value,
  ).toBe(candidate)
  await until(() => expect(button('Review current policy')?.disabled).toBe(false))
  const reviews = requests.filter((request) => request.url?.includes('emergency_admin_id='))
  expect(reviews).toHaveLength(1)
  expect(reviews[0].url).toBe('/admin/auth/policy?emergency_admin_id=' + candidate)
  expect(reviews[0].url).not.toContain('%0A')
  expect(writes()).toHaveLength(0)
  await click('Review current policy')
  await fill('textarea', 'Reviewed exact browser value')
  await click('Review change')
  expect(writes()).toHaveLength(0)
  await click('Confirm change')
  await until(() => expect(writes()).toHaveLength(1))
  expect(JSON.parse(writes()[0].data).emergency_admin_id).toBe(candidate)
  expect(writes()[0].headers.get('If-Match')).toBe(`"${selected}"`)
})
it('does not restore a cancelled old Session read over the committed anonymous receipt generation', async () => {
  await mount('/receipt')
  let release!: () => void
  const heldRead = new Promise<void>((resolve) => {
    release = resolve
  })
  let read!: Promise<unknown>
  await act(async () => {
    read = cache
      .fetchQuery({
        queryKey: ['auth', 'session'],
        queryFn: async () => {
          await heldRead
          return session
        },
        staleTime: 0,
      })
      .catch(() => undefined)
  })
  const originalSessionQuery = cache
    .getQueryCache()
    .find({ queryKey: ['auth', 'session'], exact: true })
  try {
    await click('Committed security change')
    await until(() => expect(router.state.location.pathname).toBe('/auth/reauthenticate'))
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
    await act(async () => {
      release()
      await read
    })
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
    expect(cache.getQueryCache().find({ queryKey: ['auth', 'session'], exact: true })).toBe(
      originalSessionQuery,
    )
    expect(router.state.location.pathname).toBe('/auth/reauthenticate')
    expect(host.textContent).toContain('RXR-PRIVATE-0')
    await act(async () =>
      cache.setQueryData(['auth', 'session'], { ...session, csrf_token: 'new-authentication' }),
    )
    await until(() => expect(router.state.location.pathname).toBe('/login'))
    expect(host.textContent).not.toContain('RXR-PRIVATE-0')
  } finally {
    release()
    await read
  }
})
