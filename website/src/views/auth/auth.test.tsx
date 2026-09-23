import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import routes from '@/router'
import type { Session } from '@/types/auth'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const session: Session = { user: { id: 'usr_1', email: 'admin@example.com', name: '管理员', role: 'admin' }, csrf_token: 'csrf-test' }
let container: HTMLDivElement
let root: Root
let queryClient: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let initialized: boolean
let authenticated: boolean
let requests: InternalAxiosRequestConfig[]
let fail: Record<string, number>
let pendingLogin: Promise<void> | undefined
const oldAdapter = client.defaults.adapter

beforeEach(() => {
  initialized = true
  authenticated = false
  requests = []
  fail = {}
  pendingLogin = undefined
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const key = `${config.method} ${config.url}`
    if (key === 'post /auth/login') await pendingLogin
    const status = fail[key] || (config.url === '/auth/session' && !authenticated ? 401 : 200)
    const response = { config, status, statusText: '', headers: new AxiosHeaders(), data: {} as unknown }
    if (status >= 400) throw new AxiosError('Request failed', '', config, undefined, response)
    if (key === 'get /setup') response.data = { initialized }
    if (key === 'get /auth/session') response.data = session
    if (key === 'post /setup' || key === 'post /auth/login') {
      initialized = true
      authenticated = true
      response.data = session
    }
    if (key === 'post /auth/logout') authenticated = false
    return response
  }
})

afterEach(async () => {
  await act(async () => { root.unmount() })
  router?.dispose()
  queryClient.clear()
  client.defaults.adapter = oldAdapter
  container.remove()
  vi.restoreAllMocks()
})

async function mount(path = '/') {
  router = createMemoryRouter(routes, { initialEntries: [path] })
  await act(async () => { root.render(<QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider>) })
}

async function until(assertion: () => void) {
  for (let i = 0; i < 60; i++) {
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 10)) })
    try { assertion(); return } catch (error) { if (i === 59) throw error }
  }
}

async function fill(name: string, value: string) {
  const input = container.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
  expect(input).not.toBeNull()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

async function submit() {
  await act(async () => { container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) })
}

async function credentials() {
  await fill('email', 'admin@example.com')
  await fill('password', 'correct horse battery')
}

describe('authentication flows', () => {
  it('routes an empty installation to setup and creates an authenticated administrator', async () => {
    initialized = false
    await mount('/')
    await until(() => expect(container.textContent).toContain('初始化 RouteX'))
    expect(requests.some((r) => r.url === '/auth/session')).toBe(false)
    await fill('name', '管理员')
    await credentials()
    await fill('confirmPassword', 'correct horse battery')
    await submit()
    await until(() => expect(container.textContent).toContain('你好，管理员'))
    expect(JSON.parse(requests.find((r) => r.method === 'post')!.data)).toEqual({ name: '管理员', email: 'admin@example.com', password: 'correct horse battery' })
    expect(router.state.location.pathname).toBe('/')
  })

  it('rejects confirmation mismatches and UTF-8 passwords over 72 bytes without a request', async () => {
    initialized = false
    await mount('/setup')
    await until(() => expect(container.querySelector('form')).not.toBeNull())
    await fill('name', '管理员')
    await credentials()
    await fill('confirmPassword', 'different password')
    await submit()
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('两次输入')
    await fill('password', '密'.repeat(25))
    await fill('confirmPassword', '密'.repeat(25))
    await submit()
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('UTF-8')
    expect(requests.some((r) => r.method === 'post')).toBe(false)
  })

  it('protects home, displays login errors, and ignores external redirect parameters', async () => {
    await mount('/?next=https://evil.example')
    await until(() => expect(container.textContent).toContain('登录控制台'))
    expect(container.textContent).not.toContain('当前账户')
    fail['post /auth/login'] = 401
    await credentials()
    await submit()
    await until(() => expect(container.querySelector('[role="alert"]')?.textContent).toContain('邮箱或密码不正确'))
    expect(router.state.location.pathname).toBe('/login')
    delete fail['post /auth/login']
    await submit()
    await until(() => expect(container.textContent).toContain('你好，管理员'))
    expect(router.state.location.pathname).toBe('/')
  })

  it('disables the form while login is pending and submits only once', async () => {
    let resolveLogin!: () => void
    pendingLogin = new Promise<void>((resolve) => { resolveLogin = resolve })
    await mount('/login')
    await until(() => expect(container.querySelector('form')).not.toBeNull())
    await credentials()
    await submit()
    await until(() => expect(container.querySelector('button')!.disabled).toBe(true))
    expect(container.textContent).toContain('正在提交')
    expect(container.querySelector('fieldset')!.disabled).toBe(true)
    await submit()
    expect(requests.filter((r) => r.url === '/auth/login')).toHaveLength(1)
    await act(async () => { resolveLogin() })
    await until(() => expect(container.textContent).toContain('你好，管理员'))
  })

  it('redirects a concurrent initialization conflict to login with an explanation', async () => {
    initialized = false
    await mount('/setup')
    await until(() => expect(container.querySelector('form')).not.toBeNull())
    await fill('name', '管理员')
    await credentials()
    await fill('confirmPassword', 'correct horse battery')
    initialized = true
    fail['post /setup'] = 409
    await submit()
    await until(() => expect(router.state.location.pathname).toBe('/login'))
    await until(() => expect(container.textContent).toContain('站点已完成初始化'))
  })

  it('restores a session, logs out with CSRF, and removes private cached data', async () => {
    authenticated = true
    queryClient.setQueryData(['private', 'records'], ['sensitive'])
    await mount('/login')
    await until(() => expect(container.textContent).toContain('你好，管理员'))
    const button = [...container.querySelectorAll('button')].find((el) => el.textContent === '退出登录')!
    await act(async () => { button.click() })
    await until(() => expect(container.textContent).toContain('登录控制台'))
    expect(requests.find((r) => r.url === '/auth/logout')?.headers.get('X-CSRF-Token')).toBe('csrf-test')
    expect(queryClient.getQueryData(['private', 'records'])).toBeUndefined()
    expect(window.localStorage.length).toBe(0)
  })

  it('keeps the user signed in and allows retry when logout fails', async () => {
    authenticated = true
    fail['post /auth/logout'] = 503
    await mount()
    await until(() => expect(container.textContent).toContain('你好，管理员'))
    await act(async () => { [...container.querySelectorAll('button')].find((el) => el.textContent === '退出登录')!.click() })
    await until(() => expect(container.querySelector('[role="alert"]')?.textContent).toContain('退出失败'))
    expect(container.textContent).toContain('你好，管理员')
    expect(router.state.location.pathname).toBe('/')
  })

  it('removes protected data when a request reports an expired session', async () => {
    authenticated = true
    await mount()
    await until(() => expect(container.textContent).toContain('你好，管理员'))
    queryClient.setQueryData(['private', 'records'], ['sensitive'])
    authenticated = false
    fail['get /protected'] = 401
    await act(async () => { await client.get('/protected').catch(() => undefined) })
    await until(() => expect(container.textContent).toContain('登录控制台'))
    expect(queryClient.getQueryData(['private', 'records'])).toBeUndefined()
    expect(container.textContent).not.toContain('admin@example.com')
  })

  it('reacts to an expired session found during server revalidation', async () => {
    authenticated = true
    await mount()
    await until(() => expect(container.textContent).toContain('你好，管理员'))
    queryClient.setQueryData(['private', 'records'], ['sensitive'])
    authenticated = false
    await act(async () => { await queryClient.invalidateQueries({ queryKey: ['auth', 'session'] }) })
    await until(() => expect(container.textContent).toContain('登录控制台'))
    expect(queryClient.getQueryData(['private', 'records'])).toBeUndefined()
  })

  it('shows a recoverable status error instead of treating network failure as logout', async () => {
    fail['get /setup'] = 503
    await mount()
    await until(() => expect(container.querySelector('[role="alert"]')?.textContent).toContain('无法确认登录状态'))
    delete fail['get /setup']
    await act(async () => { container.querySelector('button')!.click() })
    await until(() => expect(container.textContent).toContain('登录控制台'))
  })
})
