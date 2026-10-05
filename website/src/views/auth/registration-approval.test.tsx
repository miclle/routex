import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import AuthPage from './index'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const old = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>,
  requests: InternalAxiosRequestConfig[],
  response: unknown,
  status: number
let domains: string[], policyStatus: number, readPause: Promise<void> | undefined
const tick = () => new Promise((r) => setTimeout(r, 0))
async function flush() {
  for (let n = 0; n < 8; n++)
    await act(async () => {
      await tick()
    })
}
async function field(name: string, value: string) {
  const node = host.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(node, value)
    node.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function mount() {
  router = createMemoryRouter(
    [
      { path: '/register', element: <AuthPage mode="register" /> },
      { path: '/login', element: <p>Login destination</p> },
      { path: '/', element: <p>Authenticated destination</p> },
    ],
    { initialEntries: ['/register'] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await flush()
}
async function submit() {
  await field('name', 'New Applicant')
  await field('email', 'new@example.invalid')
  await field('password', 'valid-fixture-password')
  await act(async () =>
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
  await flush()
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  requests = []
  domains = []
  policyStatus = 0
  readPause = undefined
  status = 202
  response = { kind: 'approval_pending' }
  client.defaults.adapter = async (config) => {
    requests.push(config)
    if (config.url === '/auth/registration') {
      await readPause
      if (policyStatus)
        throw new AxiosError('Controlled metadata failure', '', config, undefined, {
          config,
          status: policyStatus,
          statusText: '',
          headers: new AxiosHeaders(),
          data: {},
        })
    }
    if (config.method === 'post' && status === 403)
      throw new AxiosError('Controlled policy denial', '', config, undefined, {
        config,
        status: 403,
        statusText: '',
        headers: new AxiosHeaders(),
        data: {},
      })
    return {
      config,
      status: config.method === 'post' ? status : 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data:
        config.url === '/site'
          ? {
              name: 'RouteX',
              logo_url: '',
              footer: '',
              default_language: 'en',
              service_url: '',
              etag: 0,
            }
          : config.url === '/auth/registration'
            ? { enabled: true, approval_required: true, allowed_email_domains: [...domains] }
            : response,
    }
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = old
})
it('shows confirmed anonymous pending in the existing card without Session, MFA, cache, navigation or polling', async () => {
  cache.setQueryData(['private', 'sentinel'], { unchanged: true })
  await mount()
  await submit()
  expect(router.state.location.pathname).toBe('/register')
  expect(host.textContent).toContain('awaiting administrator approval')
  expect(host.textContent).toContain('not signed in')
  expect(host.querySelector('input[name="password"]')).toBeNull()
  expect(cache.getQueryData(['auth', 'session'])).toBeUndefined()
  expect(cache.getQueryData(['private', 'sentinel'])).toEqual({ unchanged: true })
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  expect(requests.filter((r) => r.method === 'post')).toHaveLength(1)
  expect(requests.some((r) => r.url?.includes('mfa') || r.url?.includes('approval'))).toBe(false)
  await flush()
  expect(requests.filter((r) => r.method === 'post')).toHaveLength(1)
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('等待管理员审批')
  expect(host.textContent).toContain('尚未登录')
  expect(router.state.location.pathname).toBe('/register')
})
it('does not claim pending registration authentication from an unrelated Session cookie', async () => {
  const prior = {
    user: { id: 'usr_other', name: 'Other', email: 'other@example.invalid', role: 'member' },
    csrf_token: 'other-session',
  }
  cache.setQueryData(['auth', 'session'], prior)
  await mount()
  await submit()
  expect(cache.getQueryData(['auth', 'session'])).toEqual(prior)
  expect(host.textContent).toContain('not signed in')
  expect(router.state.location.pathname).toBe('/register')
})
it.each([
  { kind: 'approval_pending', session: { csrf_token: 'secret' } },
  { mfa_required: true, challenge_token: 'secret', expires_at: '2026-10-05T03:00:00Z' },
])('rejects unsafe registration202 without reusing MFA challenge %j', async (data) => {
  response = data
  await mount()
  await submit()
  expect(host.textContent).not.toContain('awaiting administrator approval')
  expect(router.state.location.pathname).toBe('/register')
  expect(cache.getQueryData(['auth', 'session'])).toBeUndefined()
  expect(host.querySelector('input[name="password"]')?.getAttribute('value') ?? '').toBe('')
  expect(requests.filter((r) => r.method === 'post')).toHaveLength(1)
})
it('preserves immediate registration201 Session completion when approval is not required', async () => {
  status = 201
  response = {
    user: { id: 'usr_new', name: 'New Applicant', email: 'new@example.invalid', role: 'member' },
    csrf_token: 'new-session',
  }
  await mount()
  await submit()
  expect(router.state.location.pathname).toBe('/')
  expect(cache.getQueryData(['auth', 'session'])).toEqual(response)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})

it('treats visible permitted domains as guidance while current POST403 remains a generic policy denial without admission', async () => {
  domains = ['example.invalid']
  status = 403
  await mount()
  expect(host.textContent).toContain('Allowed email domains: example.invalid.')
  expect(host.textContent).toContain('does not confirm mailbox ownership')
  await submit()
  expect(host.textContent).toContain(
    'Registration is unavailable for this email under the current policy',
  )
  expect(host.textContent).not.toContain('Registration is closed')
  expect(router.state.location.pathname).toBe('/register')
  expect(cache.getQueryData(['auth', 'session'])).toBeUndefined()
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  expect(requests.filter((r) => r.method === 'post')).toHaveLength(1)
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('当前策略不允许此邮箱注册')
})
it('hides stale public domain hints immediately on invalidation and failed renewal, preserving name/email for fresh recovery', async () => {
  domains = ['example.invalid']
  await mount()
  await field('name', 'Retained Applicant')
  await field('email', 'retained@example.invalid')
  await act(async () =>
    cache.invalidateQueries({ queryKey: ['auth', 'registration'], refetchType: 'none' }),
  )
  await flush()
  expect(host.textContent).not.toContain('Allowed email domains')
  expect(host.querySelector('form')).toBeNull()
  policyStatus = 503
  await act(async () => cache.refetchQueries({ queryKey: ['auth', 'registration'] }))
  await flush()
  expect(host.textContent).not.toContain('example.invalid')
  expect(host.textContent).toContain('Unable to load registration settings')
  policyStatus = 0
  domains = ['fresh.example']
  await act(async () =>
    [...host.querySelectorAll<HTMLButtonElement>('button')]
      .find((node) => node.textContent === 'Retry')!
      .click(),
  )
  await flush()
  expect(host.textContent).toContain('Allowed email domains: fresh.example.')
  expect(host.querySelector<HTMLInputElement>('input[name="name"]')!.value).toBe(
    'Retained Applicant',
  )
  expect(host.querySelector<HTMLInputElement>('input[name="email"]')!.value).toBe(
    'retained@example.invalid',
  )
  expect(requests.filter((r) => r.method === 'post')).toHaveLength(0)
})
it('keeps restricted pending202 anonymous with no stale domain hints or authenticated state', async () => {
  domains = ['example.invalid']
  await mount()
  await submit()
  expect(host.textContent).toContain('awaiting administrator approval')
  expect(host.textContent).not.toContain('Allowed email domains')
  expect(cache.getQueryData(['auth', 'session'])).toBeUndefined()
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  expect(router.state.location.pathname).toBe('/register')
})
