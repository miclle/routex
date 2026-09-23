import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import KeysPage from '@/views/keys'
import ProvidersPage from '@/views/providers'
import AdminModelsPage from '@/views/models/admin'
import client from '@/api/client'
import type { Model, PersonalKey, Provider } from '@/types/catalog'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root
let container: HTMLDivElement
let cache: QueryClient
let role: 'admin' | 'member'
let requests: InternalAxiosRequestConfig[]
let failures: Record<string, number>
let keys: PersonalKey[]
let provider: Provider
let model: Model
const secret = 'rx_test_one_time_secret'
const originalAdapter = client.defaults.adapter
const makeKey = (status: PersonalKey['status'] = 'pending'): PersonalKey => ({ id: 'key_1', name: 'Test Key', prefix: 'rx_test', status, model_ids: ['mdl_1'], expires_at: null, created_at: '2026-09-23T00:00:00Z', replaces_key_id: null, delivery_expires_at: '2026-09-23T00:10:00Z' })

beforeEach(() => {
  role = 'admin'
  requests = []
  failures = {}
  keys = []
  provider = { id: 'prv_1', name: 'Provider', connections: [{ id: 'con_1', name: 'Primary', base_url: 'https://api.example.com/v1', protocol: 'openai_chat', credentials: [{ id: 'cre_1', name: 'Credential', priority: 0, enabled: false, verification_status: 'pending', verified_at: null }], provider_models: [{ id: 'pm_1', upstream_name: 'upstream-model' }] }] }
  model = { id: 'mdl_1', name: 'Model', status: 'active', names: [{ name: 'Model', is_current: true, expires_at: null }], bindings: [{ id: 'bind_1', provider_model_id: 'pm_1', provider_id: 'prv_1', connection_id: 'con_1', upstream_name: 'upstream-model', protocol: 'openai_chat', weight: 100, ready: true }], granted_user_ids: ['usr_1'] }
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const path = config.url!
    const route = `${config.method} ${path}`
    const response = { config, status: failures[route] || 200, statusText: '', headers: new AxiosHeaders(), data: {} as unknown }
    if (response.status >= 400) throw new AxiosError('Request failed', '', config, undefined, response)
    if (route === 'get /auth/session') response.data = { user: { id: 'usr_1', name: 'User', email: 'user@example.com', role }, csrf_token: 'csrf' }
    if (route === 'get /keys') response.data = { items: structuredClone(keys) }
    if (route === 'get /models') response.data = { items: [{ id: 'mdl_1', name: 'Model', status: 'active', protocol: 'openai_chat' }] }
    if (route === 'get /admin/providers') response.data = { items: [structuredClone(provider)] }
    if (route === 'get /admin/models') response.data = { items: [structuredClone(model)] }
    if (route === 'get /admin/model-grantees') response.data = { items: [{ id: 'usr_1', name: 'User', email: 'user@example.com' }, { id: 'usr_2', name: 'Second', email: 'second@example.com' }] }
    if (route === 'post /keys' || route === 'post /keys/key_1/rotate') { keys.push(makeKey()); response.data = { key: makeKey(), secret } }
    if (route === 'post /keys/key_1/confirm') { keys[0].status = 'active'; response.data = keys[0] }
    if (route === 'delete /keys/key_1') keys[0].status = 'revoked'
    if (route === 'post /admin/credentials/cre_1/verify') { provider.connections[0].credentials[0].verification_status = 'verified'; response.data = { verified: true, discovered_models: 1 } }
    if (route === 'patch /admin/credentials/cre_1') provider.connections[0].credentials[0].enabled = JSON.parse(config.data).enabled
    return response
  }
})
afterEach(async () => {
  await act(async () => { root.unmount() })
  cache.clear()
  client.defaults.adapter = originalAdapter
  container.remove()
})
async function render(ui: ReactNode) { await act(async () => { root.render(<QueryClientProvider client={cache}>{ui}</QueryClientProvider>) }) }
async function until(assert: () => void) {
  for (let i = 0; i < 60; i++) {
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 10)) })
    try { assert(); return } catch (error) { if (i === 59) throw error }
  }
}
function button(text: string) { const found = [...document.querySelectorAll('button')].find((b) => b.textContent === text); expect(found).toBeDefined(); return found! }
async function click(text: string) { await act(async () => { button(text).click() }) }
async function fill(name: string, value: string) {
  const input = document.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
  expect(input).not.toBeNull()
  await act(async () => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value); input.dispatchEvent(new Event('input', { bubbles: true })) })
}
async function submit() { await act(async () => { document.querySelector('[role="dialog"] form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) }) }
async function createDelivery() {
  await render(<KeysPage />)
  await until(() => expect(document.body.textContent).toContain('暂无数据'))
  await click('创建 Key')
  await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
  await fill('name', 'Test Key')
  await act(async () => { document.querySelector<HTMLInputElement>('input[value="mdl_1"]')!.click() })
  await submit()
  await until(() => expect(document.body.textContent).toContain(secret))
}

describe('catalog and Key workflows', () => {
  it('keeps one-time secrets out of caches and activates only after delivery confirmation', async () => {
    await createDelivery()
    expect(button('确认并启用').disabled).toBe(true)
    expect(JSON.stringify(cache.getMutationCache().getAll().map((m) => m.state))).not.toContain(secret)
    expect(JSON.stringify(cache.getQueryCache().getAll().map((q) => q.state))).not.toContain(secret)
    expect(localStorage.length).toBe(0)
    await act(async () => { document.querySelector<HTMLInputElement>('[role="dialog"] input[type="checkbox"]')!.click() })
    await click('确认并启用')
    await until(() => expect(document.body.textContent).not.toContain(secret))
    expect(keys[0].status).toBe('active')
    expect(requests.find((r) => r.url === '/keys/key_1/confirm')?.headers.get('X-CSRF-Token')).toBe('csrf')
    expect(JSON.parse(requests.find((r) => r.method === 'post' && r.url === '/keys')!.data)).toEqual({ name: 'Test Key', model_ids: ['mdl_1'], expires_at: null })
  })
  it('revokes an unconfirmed Key when the delivery dialog is dismissed', async () => {
    await createDelivery()
    await act(async () => { document.querySelector<HTMLButtonElement>('[role="dialog"] button[aria-label="关闭"]')!.click() })
    await until(() => expect(document.body.textContent).not.toContain(secret))
    expect(keys[0].status).toBe('revoked')
  })
  it('retains the secret and offers retry when cancellation fails', async () => {
    await createDelivery()
    failures['delete /keys/key_1'] = 503
    await click('取消并撤销')
    await until(() => expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull())
    expect(document.body.textContent).toContain(secret)
    delete failures['delete /keys/key_1']
    await click('取消并撤销')
    await until(() => expect(document.body.textContent).not.toContain(secret))
  })
  it('does not query administrative data for a member', async () => {
    role = 'member'
    await render(<ProvidersPage />)
    await until(() => expect(document.body.textContent).toContain('无权访问'))
    expect(requests.filter((r) => r.url?.startsWith('/admin'))).toHaveLength(0)
  })
  it('verifies a credential before allowing a separate enable action', async () => {
    await render(<ProvidersPage />)
    await until(() => expect(document.body.textContent).toContain('Credential'))
    expect(button('启用').disabled).toBe(true)
    await click('验证')
    await until(() => expect(button('启用').disabled).toBe(false))
    expect(provider.connections[0].credentials[0].enabled).toBe(false)
    await click('启用')
    await until(() => expect(document.body.textContent).toContain('已启用'))
    expect(requests.find((r) => r.method === 'patch')?.headers.get('X-CSRF-Token')).toBe('csrf')
  })
  it('keeps provider form errors recoverable and sends the agreed connection contract', async () => {
    await render(<ProvidersPage />)
    await until(() => expect(document.body.textContent).toContain('Provider'))
    await click('添加供应商')
    await fill('name', 'Another Provider')
    await fill('connection_name', 'API')
    await fill('base_url', 'https://api.example.com/v1')
    await fill('credential_name', 'Primary credential')
    await fill('secret', 'upstream_secret')
    failures['post /admin/providers'] = 400
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull())
    expect(JSON.parse(requests.find((r) => r.method === 'post')!.data)).toEqual({ name: 'Another Provider', connection_name: 'API', base_url: 'https://api.example.com/v1', protocol: 'openai_chat', credential_name: 'Primary credential', secret: 'upstream_secret' })
    delete failures['post /admin/providers']
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await until(() => expect(JSON.stringify(cache.getMutationCache().getAll().map((m) => m.state))).not.toContain('upstream_secret'))
  })
  it('sends all binding weights and shows server validation failures', async () => {
    await render(<AdminModelsPage />)
    await until(() => expect(document.body.textContent).toContain('upstream-model'))
    await click('调整权重')
    await fill('bind_1', '50')
    failures['put /admin/models/mdl_1/weights'] = 400
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull())
    expect(JSON.parse(requests.find((r) => r.method === 'put')!.data)).toEqual({ weights: [{ binding_id: 'bind_1', weight: 50 }] })
  })
  it('updates explicit grants without granting every administrator implicitly', async () => {
    await render(<AdminModelsPage />)
    await until(() => expect(document.body.textContent).toContain('upstream-model'))
    await click('授权')
    await until(() => expect(document.querySelector('input[value="usr_2"]')).not.toBeNull())
    await act(async () => { document.querySelector<HTMLInputElement>('input[value="usr_1"]')!.click(); document.querySelector<HTMLInputElement>('input[value="usr_2"]')!.click() })
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(JSON.parse(requests.find((r) => r.method === 'put')!.data)).toEqual({ user_ids: ['usr_2'] })
  })
})
