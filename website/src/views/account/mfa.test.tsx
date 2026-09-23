import { act, createElement } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import AuthPage from '@/views/auth'
import AccountMFA from './mfa'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { MFAStatus } from '@/types/mfa'
const qr = vi.hoisted(() => ({ values: [] as string[] }))
vi.mock('qrcode.react', async (importOriginal) => {
  const actual = await importOriginal<typeof import('qrcode.react')>()
  return {
    ...actual,
    QRCodeSVG: (props: Parameters<typeof actual.QRCodeSVG>[0]) => {
      qr.values.push(props.value as string)
      return createElement(actual.QRCodeSVG, props)
    },
  }
})
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let host: HTMLDivElement,
  root: Root,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let status: MFAStatus,
  session: Session,
  requests: InternalAxiosRequestConfig[],
  failures: Record<string, number>,
  expires: number
let hold: Promise<void> | undefined
const original = client.defaults.adapter
const secret = 'JBSWY3DPEHPK3PXP'
const uri = `otpauth://totp/RouteX:test%40example.test?secret=${secret}&issuer=RouteX`
const recovery = 'RXR-AAAAAAAA-BBBBBBBB-CCCCCCCC-DDDDDDDD'
const password = 'test current password'
beforeEach(async () => {
  await i18n.changeLanguage('en')
  qr.values = []
  requests = []
  failures = {}
  expires = 300000
  hold = undefined
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  session = {
    user: { id: 'usr_mfa', name: 'MFA User', email: 'test@example.test', role: 'member' },
    csrf_token: 'csrf-old',
  }
  status = {
    enabled: false,
    enrollment_available: true,
    enrollment_pending: false,
    recovery_codes_remaining: 0,
  }
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: failures[config.url!] || 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/login') {
      response.status = 202
      response.data = {
        mfa_required: true,
        challenge_token: 'challenge-sensitive',
        expires_at: new Date(Date.now() + expires).toISOString(),
        methods: ['totp', 'recovery_code'],
      }
    } else if (config.url === '/auth/session') response.data = session
    else if (config.url === '/auth/registration') response.data = { enabled: false }
    else if (config.url === '/account/mfa') response.data = { ...status }
    else if (config.url === '/account/mfa/enrollment') {
      if (config.method === 'delete') {
        if (response.status < 400) status.enrollment_pending = false
        response.status = failures[config.url] || 204
      } else {
        if (response.status < 400) status.enrollment_pending = expires > 0
        response.data = {
          secret,
          otpauth_uri: uri,
          enrollment_token: 'enrollment-sensitive',
          expires_at: new Date(Date.now() + expires).toISOString(),
        }
      }
    } else if (config.url === '/auth/mfa/verify') {
      if (hold) await hold
      response.data = session
    } else if (config.url?.startsWith('/account/mfa/') && response.status < 400) {
      if (hold) await hold
      session = { ...session, csrf_token: 'csrf-rotated' }
      status = {
        ...status,
        enabled: config.url !== '/account/mfa/disable',
        enrollment_pending: false,
        recovery_codes_remaining: config.url === '/account/mfa/disable' ? 0 : 10,
      }
      response.data =
        config.url === '/account/mfa/disable'
          ? session
          : {
              session,
              recovery_codes: Array.from({ length: 10 }, (_, index) => `${recovery}-${index}`),
            }
    }
    if (response.status >= 400 && !config.validateStatus?.(response.status))
      throw new AxiosError('Fixture', '', config, undefined, response)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
})
async function until(check: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      check()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
async function mount(login = false) {
  cache.setQueryData(sessionKey, login ? null : session)
  router = createMemoryRouter(
    [
      { path: '/login', element: <AuthPage mode="login" /> },
      { path: '/security', element: <AccountMFA /> },
      { path: '/', element: <p>Authenticated workspace</p> },
    ],
    { initialEntries: [login ? '/login' : '/security'] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain(login ? 'Sign in' : 'Add an authenticator'))
}
function button(label: string) {
  const element = [...document.querySelectorAll<HTMLButtonElement>('button, [role="switch"]')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
  expect(element, label).toBeDefined()
  return element!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function fill(name: string, value: string) {
  const input = document.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
  expect(input, name).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
function assertNoSecretCache() {
  const saved = JSON.stringify([
    cache
      .getQueryCache()
      .getAll()
      .map((q) => q.state.data),
    cache
      .getMutationCache()
      .getAll()
      .map((m) => m.state),
  ])
  for (const value of [
    password,
    secret,
    uri,
    recovery,
    'enrollment-sensitive',
    'challenge-sensitive',
    '123456',
  ])
    expect(saved).not.toContain(value)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  for (const key of Object.keys(localStorage))
    expect(localStorage.getItem(key)).not.toContain(secret)
}
async function enroll() {
  await click('Enable two-step verification')
  await fill('mfa_password', password)
  await click('Set up authenticator')
  await until(() =>
    expect(
      document.querySelector('svg[aria-label="Authenticator enrollment QR code"]'),
    ).not.toBeNull(),
  )
}
async function challenge() {
  await mount(true)
  await fill('email', 'test@example.test')
  await fill('password', password)
  await click('Sign in')
  await until(() => expect(host.textContent).toContain('Verify your sign-in'))
}
describe('MFA authentication boundaries', () => {
  it('keeps HTTP202 outside the session/cache and only authenticates after a successful fresh code', async () => {
    await challenge()
    expect(router.state.location.pathname).toBe('/login')
    expect(cache.getQueryData(sessionKey)).toBeNull()
    expect(document.querySelector('input[name="password"]')).toBeNull()
    assertNoSecretCache()
    await fill('code', '123456')
    await click('Verify and sign in')
    await until(() => expect(router.state.location.pathname).toBe('/'))
    expect(cache.getQueryData(sessionKey)).toEqual(session)
    expect(
      JSON.parse(requests.find((request) => request.url === '/auth/mfa/verify')!.data),
    ).toEqual({ challenge_token: 'challenge-sensitive', code: '123456' })
    assertNoSecretCache()
  })
  it('submits a recovery proof only, clears failed input, and retains an unauthenticated challenge on generic401', async () => {
    await challenge()
    await click('Use a recovery code')
    await fill('recovery_code', recovery)
    failures['/auth/mfa/verify'] = 401
    const dispatch = vi.spyOn(window, 'dispatchEvent')
    await click('Verify and sign in')
    await until(() => expect(host.textContent).toContain('Verification failed.'))
    expect((document.querySelector('[name="recovery_code"]') as HTMLInputElement).value).toBe('')
    expect(
      JSON.parse(requests.find((request) => request.url === '/auth/mfa/verify')!.data),
    ).toEqual({ challenge_token: 'challenge-sensitive', recovery_code: recovery })
    expect(dispatch.mock.calls.some(([event]) => event.type === 'routex:session-expired')).toBe(
      false,
    )
    expect(cache.getQueryData(sessionKey)).toBeNull()
    assertNoSecretCache()
    await click('Back to password sign-in')
    expect(host.textContent).not.toContain('Verify your sign-in')
  })
  it('expires challenges and never dispatches a verification from the expired form', async () => {
    expires = -1
    await mount(true)
    await fill('email', 'test@example.test')
    await fill('password', password)
    await click('Sign in')
    await until(() => expect(host.textContent).toContain('This verification has expired.'))
    expect(host.querySelector('[name="code"]')).toBeNull()
    expect(requests.some((request) => request.url === '/auth/mfa/verify')).toBe(false)
    assertNoSecretCache()
  })
  it('prevents duplicate verification while pending and aborts a challenge when navigating away', async () => {
    await challenge()
    await fill('code', '123456')
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    const verify = button('Verify and sign in')
    await act(async () => {
      verify.click()
      verify.click()
    })
    await until(() =>
      expect(requests.filter((request) => request.url === '/auth/mfa/verify')).toHaveLength(1),
    )
    await act(async () => {
      await router.navigate('/security')
    })
    await act(async () => release())
    expect(router.state.location.pathname).toBe('/security')
    assertNoSecretCache()
  })
  it('renders the server URI as a real local QR, preserves secret only during enrollment, and cancels on close', async () => {
    await mount()
    await enroll()
    expect(qr.values).toContain(uri)
    expect(
      document
        .querySelector('svg[aria-label="Authenticator enrollment QR code"] path')
        ?.getAttribute('d'),
    ).toBeTruthy()
    expect((document.querySelector('[aria-label="Setup secret"]') as HTMLInputElement).value).toBe(
      secret,
    )
    expect(document.body.textContent).not.toContain(recovery)
    assertNoSecretCache()
    await click('Cancel')
    await until(() => expect(document.querySelector('[aria-label="Setup secret"]')).toBeNull())
    expect(document.querySelector('svg[aria-label="Authenticator enrollment QR code"]')).toBeNull()
    expect(
      requests.some(
        (request) => request.url === '/account/mfa/enrollment' && request.method === 'delete',
      ),
    ).toBe(true)
    expect(document.querySelector('[name="mfa_password"]')).toBeNull()
    assertNoSecretCache()
  })
  it('shows recovery codes only after verified enable, rotates current CSRF, and clears one-time codes on acknowledgment', async () => {
    await mount()
    await enroll()
    cache.setQueryData(['private-before-rotation'], { stale: true })
    await fill('mfa_code', '123456')
    await click('Confirm enable')
    await until(() => expect(document.body.textContent).toContain('Save your recovery codes'))
    expect(document.body.textContent).toContain(`${recovery}-9`)
    expect(document.querySelector('[aria-label="Setup secret"]')).toBeNull()
    expect((cache.getQueryData(sessionKey) as Session).csrf_token).toBe('csrf-rotated')
    expect(cache.getQueryData(['private-before-rotation'])).toBeUndefined()
    expect(
      JSON.parse(requests.find((request) => request.url === '/account/mfa/enable')!.data),
    ).toEqual({
      current_password: password,
      enrollment_token: 'enrollment-sensitive',
      code: '123456',
    })
    assertNoSecretCache()
    await click('I have saved these codes')
    expect(document.body.textContent).not.toContain(recovery)
    await click('Regenerate recovery codes')
    await fill('mfa_password', password)
    await fill('mfa_code', '654321')
    await click('Replace recovery codes')
    await until(() => expect(document.body.textContent).toContain('Save your recovery codes'))
    expect(
      requests
        .find((request) => request.url === '/account/mfa/recovery-codes')!
        .headers.get('X-CSRF-Token'),
    ).toBe('csrf-rotated')
    assertNoSecretCache()
  })
  it('requires password plus fresh proof for disable, handles local401 and does not cache submitted recovery credentials', async () => {
    await mount()
    status.enabled = true
    status.recovery_codes_remaining = 10
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['account', 'mfa'] })
    })
    await until(() => expect(button('Disable two-step verification')).toBeDefined())
    await click('Disable two-step verification')
    await click('Confirm disable')
    expect(document.body.textContent).toContain('Enter your current password')
    await fill('mfa_password', password)
    await click('Use a recovery code')
    await fill('mfa_code', recovery)
    failures['/account/mfa/disable'] = 401
    const dispatch = vi.spyOn(window, 'dispatchEvent')
    await click('Confirm disable')
    await until(() => expect(document.body.textContent).toContain('Verification failed.'))
    expect(dispatch.mock.calls.some(([event]) => event.type === 'routex:session-expired')).toBe(
      false,
    )
    expect((document.querySelector('[name="mfa_password"]') as HTMLInputElement).value).toBe('')
    expect((document.querySelector('[name="mfa_code"]') as HTMLInputElement).value).toBe('')
    assertNoSecretCache()
    delete failures['/account/mfa/disable']
    await fill('mfa_password', password)
    await fill('mfa_code', recovery)
    await click('Confirm disable')
    await until(() => expect(host.textContent).toContain('Two-step verification is disabled.'))
    expect((cache.getQueryData(sessionKey) as Session).csrf_token).toBe('csrf-rotated')
    expect(document.body.textContent).not.toContain('Save your recovery codes')
  })
  it('dispatches enable once while pending and never shows codes for an uncertain failure', async () => {
    await mount()
    await enroll()
    let release!: () => void
    hold = new Promise<void>((resolve) => {
      release = resolve
    })
    await fill('mfa_code', '123456')
    const form = document.querySelector<HTMLFormElement>(
      'form[aria-label="Enable two-step verification"]',
    )!
    await act(async () => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    expect(requests.filter((request) => request.url === '/account/mfa/enable')).toHaveLength(1)
    expect(document.body.textContent).not.toContain(recovery)
    assertNoSecretCache()
    await act(async () => release())
    await until(() => expect(document.body.textContent).toContain('Save your recovery codes'))
    await click('I have saved these codes')
    hold = undefined
    failures['/account/mfa/recovery-codes'] = 503
    await click('Regenerate recovery codes')
    await fill('mfa_password', password)
    await fill('mfa_code', '654321')
    await click('Replace recovery codes')
    await until(() => expect(document.body.textContent).toContain('Unable to confirm this action.'))
    expect(document.body.textContent).not.toContain(recovery)
    expect(document.querySelector('[name="mfa_password"]')).toBeNull()
    assertNoSecretCache()
  })
  it('clears expired enrollment and reports unavailable key storage without creating a secret', async () => {
    await mount()
    expires = -1
    await click('Enable two-step verification')
    await fill('mfa_password', password)
    await click('Set up authenticator')
    await until(() => expect(host.textContent).toContain('This verification has expired.'))
    expect(document.querySelector('[aria-label="Setup secret"]')).toBeNull()
    assertNoSecretCache()
    status.enrollment_available = false
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['account', 'mfa'] })
    })
    await until(() =>
      expect(button('Enable two-step verification').getAttribute('aria-disabled')).toBe('true'),
    )
    expect(host.textContent).toContain('Contact an administrator.')
  })
  it('keeps canceled secrets cleared on cancellation failure and translates existing validation without leaking state', async () => {
    await mount()
    await click('Enable two-step verification')
    await click('Set up authenticator')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.body.textContent).toContain('请输入当前密码')
    await fill('mfa_password', password)
    await click('设置验证器')
    await until(() => expect(document.querySelector('[aria-label="设置密钥"]')).not.toBeNull())
    failures['/account/mfa/enrollment'] = 503
    await click('取消')
    await until(() => expect(host.textContent).toContain('设置密钥已清除'))
    expect(document.querySelector('[aria-label="设置密钥"]')).toBeNull()
    assertNoSecretCache()
  })
})
