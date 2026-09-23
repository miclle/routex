import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import AccountPage from './index'
import client from '@/api/client'
import type { AccountSession } from '@/types/account'
import type { Session } from '@/types/auth'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root
let container: HTMLDivElement
let cache: QueryClient
let requests: InternalAxiosRequestConfig[]
let sessions: AccountSession[]
let session: Session
let failures: Record<string, number>
let active: boolean
const originalAdapter = client.defaults.adapter
beforeEach(() => {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  requests = []
  failures = {}
  active = true
  session = { user: { id: 'usr_1', name: 'User', email: 'user@example.com', role: 'member' }, csrf_token: 'old-csrf' }
  sessions = [{ id: 'ses_current', current: true, created_at: '2026-09-23T00:00:00Z', expires_at: '2026-09-30T00:00:00Z' }, { id: 'ses_other', current: false, created_at: '2026-09-22T00:00:00Z', expires_at: '2026-09-29T00:00:00Z' }]
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const route = `${config.method} ${config.url}`
    const response = { config, status: failures[route] || (route === 'get /auth/session' && !active ? 401 : 200), statusText: '', headers: new AxiosHeaders(), data: {} as unknown }
    if (response.status >= 400) throw new AxiosError('Failure', '', config, undefined, response)
    if (route === 'get /auth/session') response.data = structuredClone(session)
    if (route === 'get /account/sessions') response.data = { items: structuredClone(sessions) }
    if (route === 'patch /account') { session.user.name = JSON.parse(config.data).name; response.data = structuredClone(session.user) }
    if (route === 'post /account/password') { session.csrf_token = 'new-csrf'; response.data = structuredClone(session); sessions = [{ ...sessions[0], id: 'ses_new' }] }
    if (config.method === 'delete' && config.url?.endsWith('/ses_current')) active = false
    if (config.method === 'delete') sessions = sessions.filter((item) => !config.url!.endsWith(item.id))
    return response
  }
})
afterEach(async () => { await act(async () => { root.unmount() }); cache.clear(); client.defaults.adapter = originalAdapter; container.remove() })
async function until(assert: () => void) { for (let i = 0; i < 60; i++) { await act(async () => { await new Promise((r) => setTimeout(r, 10)) }); try { assert(); return } catch (error) { if (i === 59) throw error } } }
async function render(security = true) { await act(async () => { root.render(<QueryClientProvider client={cache}><MemoryRouter initialEntries={['/account']}><Routes><Route path="/account" element={<AccountPage security={security} />} /><Route path="/login" element={<p>Login page</p>} /></Routes></MemoryRouter></QueryClientProvider>) }); await until(() => expect(container.textContent).toContain(security ? 'ses_other' : '基本信息')) }
async function fill(name: string, value: string) { const input = document.querySelector<HTMLInputElement>(`input[name="${name}"]`)!; await act(async () => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value); input.dispatchEvent(new Event('input', { bubbles: true })) }) }
async function submit(label: string) { await act(async () => { document.querySelector(`form[aria-label="${label}"]`)!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) }) }
async function click(text: string) { await act(async () => { [...document.querySelectorAll('button')].find((item) => item.textContent === text)!.click() }) }

describe('account settings', () => {
  it('updates the profile using CSRF and refreshes the session user', async () => {
    await render(false)
    await fill('name', 'New name')
    await submit('个人资料')
    await until(() => expect(container.textContent).toContain('个人资料已更新'))
    expect(cache.getQueryData<Session>(['auth', 'session'])?.user.name).toBe('New name')
    expect(requests.find((r) => r.method === 'patch')?.headers.get('X-CSRF-Token')).toBe('old-csrf')
  })
  it('validates confirmation, rotates session state, clears password inputs and mutation data', async () => {
    await render()
    await click('修改密码')
    await fill('current_password', 'old-password-value')
    await fill('new_password', 'new-password-value')
    await fill('confirm_password', 'different-password')
    await submit('修改密码')
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('两次输入的新密码不一致')
    expect(requests.some((r) => r.url === '/account/password')).toBe(false)
    await fill('confirm_password', 'new-password-value')
    await submit('修改密码')
    await until(() => expect(container.textContent).toContain('密码已更新'))
    expect(cache.getQueryData<Session>(['auth', 'session'])?.csrf_token).toBe('new-csrf')
    expect(document.querySelector<HTMLInputElement>('[name="current_password"]')!.value).toBe('')
    expect(document.querySelector<HTMLInputElement>('[name="new_password"]')!.value).toBe('')
    await until(() => expect(JSON.stringify(cache.getMutationCache().getAll().map((mutation) => mutation.state))).not.toContain('new-password-value'))
    expect(container.textContent).not.toContain('ses_other')
  })
  it('keeps incorrect-current-password errors recoverable without logging out', async () => {
    failures['post /account/password'] = 400
    await render()
    await click('修改密码')
    await fill('current_password', 'wrong-current-password')
    await fill('new_password', 'new-password-value')
    await fill('confirm_password', 'new-password-value')
    await submit('修改密码')
    await until(() => expect(document.querySelector('[role="alert"]')).not.toBeNull())
    expect(container.textContent).toContain('安全设置')
    expect(cache.getQueryData<Session>(['auth', 'session'])?.csrf_token).toBe('old-csrf')
  })
  it('revokes another session after confirmation and preserves the current session', async () => {
    await render()
    await act(async () => { [...container.querySelectorAll('button')].filter((item) => item.textContent === '撤销会话')[1].click() })
    await click('确认撤销')
    await until(() => expect(container.textContent).not.toContain('ses_other'))
    expect(container.textContent).toContain('ses_current')
    expect(requests.find((r) => r.method === 'delete')?.url).toBe('/account/sessions/ses_other')
  })
  it('clears private caches and returns to login when revoking the current session', async () => {
    await render()
    cache.setQueryData(['private', 'items'], ['secret-data'])
    await click('撤销会话')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('这是当前会话')
    await click('确认撤销')
    await until(() => expect(container.textContent).toContain('Login page'))
    expect(cache.getQueryData(['private', 'items'])).toBeUndefined()
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
  })
})
