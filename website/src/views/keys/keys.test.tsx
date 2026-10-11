import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/keyDeliveryOwner'
import zh from '@/i18n/locales/zh/keyDeliveryOwner'
import type { PersonalKey } from '@/types/catalog'
import KeysPage from './index'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
const suffix = '01m36yee4gkbns18pfcqqc75a3'
const bearer = 'rx_once_owner_manual_secret'
const review = 'a'.repeat(64)
let root: Root,
  container: HTMLDivElement,
  cache: QueryClient,
  keys: PersonalKey[],
  requests: InternalAxiosRequestConfig[]
let held: { promise: Promise<void>; release: () => void } | null
const fixture = (): PersonalKey => ({
  id: 'key_' + suffix,
  name: 'Personal application',
  prefix: 'rx_masked',
  status: 'active',
  model_ids: ['mdl_' + suffix],
  expires_at: null,
  created_at: '2026-10-11T00:00:00Z',
  replaces_key_id: null,
  delivery_expires_at: null,
})
beforeEach(async () => {
  await i18n.changeLanguage('en')
  i18n.addResourceBundle('en', 'keyDeliveryOwner', en, true, true)
  i18n.addResourceBundle('zh', 'keyDeliveryOwner', zh, true, true)
  root = createRoot((container = document.createElement('div')))
  document.body.append(container)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  keys = []
  requests = []
  held = null
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session')
      response.data = {
        user: { id: 'usr_' + suffix, role: 'member', name: 'Member', email: 'member@example.test' },
        csrf_token: 'b'.repeat(64),
      }
    else if (config.url === '/models')
      response.data = {
        items: [
          {
            id: 'mdl_' + suffix,
            name: 'Model',
            status: 'active',
            names: [],
            bindings: [],
            granted_user_ids: [],
          },
        ],
      }
    else if (config.url === '/keys' && config.method === 'get')
      response.data = { items: structuredClone(keys) }
    else if (config.url === '/keys/delivery-context') {
      response.data = {
        allowed_modes: ['manual', 'managed_vault'],
        default_mode: 'manual',
        managed_available: false,
        failure_code: 'not_configured',
        review_etag: review,
      }
      response.headers.set('etag', '"' + review + '"')
    } else if (config.url === '/keys' && config.method === 'post') {
      if (held) await held.promise
      const item = {
        ...fixture(),
        ...JSON.parse(config.data),
        status: 'pending' as const,
        delivery_mode: 'manual' as const,
      }
      keys = [item]
      response.data = { key: item, secret: bearer }
    } else if (config.url?.endsWith('/rotate')) {
      const item = {
        ...fixture(),
        id: 'key_01m36yee4gkbns18pfcqqc75a4',
        status: 'pending' as const,
        replaces_key_id: fixture().id,
        delivery_mode: 'manual' as const,
      }
      keys.unshift(item)
      response.data = { key: item, secret: bearer }
    } else if (config.url?.endsWith('/confirm')) {
      keys[0].status = 'active'
      response.data = keys[0]
    } else if (config.method === 'delete') {
      keys[0].status = 'revoked'
      response.status = 204
    } else throw new Error('Unexpected owner request')
    return response
  }
})
afterEach(async () => {
  held?.release()
  await act(async () => root.unmount())
  cache.clear()
  container.remove()
  client.defaults.adapter = originalAdapter
  await i18n.changeLanguage('en')
})
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <KeysPage />
      </QueryClientProvider>,
    ),
  )
}
async function until(check: () => void) {
  for (let i = 0; i < 60; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      check()
      return
    } catch (error) {
      if (i === 59) throw error
    }
  }
}
function button(text: string) {
  const item = [...document.querySelectorAll('button')].find((item) => item.textContent === text)
  expect(item, text).toBeDefined()
  return item!
}
async function click(text: string) {
  await act(async () => button(text).click())
}
async function fill(name: string, value: string) {
  await act(async () => {
    const item = document.querySelector<HTMLInputElement>('input[name="' + name + '"]')!
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(item, value)
    item.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function create() {
  await render()
  await until(() => expect(button('Create key').disabled).toBe(false))
  await click('Create key')
  await until(() => expect(document.querySelector('input[name="name"]')).not.toBeNull())
  await fill('name', 'Personal application')
  await act(async () =>
    document.querySelector<HTMLInputElement>('input[name="model_ids"]')!.click(),
  )
  await act(async () =>
    document
      .querySelector('[role="dialog"] form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
}
function privateCache() {
  return JSON.stringify({
    queries: cache
      .getQueryCache()
      .getAll()
      .map((q) => q.state),
    mutations: cache
      .getMutationCache()
      .getAll()
      .map((m) => m.state),
  })
}
describe('Personal Key delivery composition', () => {
  it('preserves manual one-time display and explicit confirmation without caching the bearer', async () => {
    await create()
    await until(() => expect(document.body.textContent).toContain(bearer))
    expect(privateCache()).not.toContain(bearer)
    expect(JSON.stringify(localStorage)).not.toContain(bearer)
    expect(JSON.stringify(sessionStorage)).not.toContain(bearer)
    expect(button('Confirm and enable').disabled).toBe(true)
    await act(async () =>
      document.querySelector<HTMLInputElement>('[role="dialog"] input[type="checkbox"]')!.click(),
    )
    await click('Confirm and enable')
    await until(() => expect(document.body.textContent).not.toContain(bearer))
    expect(keys[0].status).toBe('active')
    expect(privateCache()).not.toContain(bearer)
    expect(
      JSON.parse(requests.find((x) => x.method === 'post' && x.url === '/keys')!.data),
    ).toEqual({ name: 'Personal application', model_ids: ['mdl_' + suffix], expires_at: null })
  })
  it('retains legacy missing delivery mode as Unknown and preserves supported manual rotation without claiming managed eligibility', async () => {
    keys = [fixture()]
    await render()
    await until(() => expect(container.textContent).toContain('Unknown delivery method'))
    await click('Rotate')
    await until(() => expect(document.body.textContent).toContain(bearer))
    expect(requests.filter((x) => x.url === '/keys/delivery-context')).toHaveLength(1)
    expect(requests.filter((x) => x.url?.endsWith('/rotate'))).toHaveLength(1)
    expect(requests.some((x) => x.url?.includes('managed'))).toBe(false)
    expect(keys.find((x) => x.id === fixture().id)?.status).toBe('active')
    expect(requests.some((x) => x.url?.endsWith('/complete-rotation'))).toBe(false)
  })
  it('opens managed rotation review inline without calling the legacy secret-returning endpoint', async () => {
    keys = [{ ...fixture(), delivery_mode: 'managed_vault' }]
    await render()
    await until(() => expect(container.textContent).toContain('Managed Vault delivery'))
    await click('Rotate')
    await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
    expect(requests.some((x) => x.url?.endsWith('/rotate'))).toBe(false)
    expect(document.body.textContent).not.toContain(bearer)
    expect(document.querySelector('input[name="name"]')).toBeNull()
  })
  it('clears the manual secret on same-owner Session renewal and rejects late old-generation issuance', async () => {
    await create()
    await until(() => expect(document.body.textContent).toContain(bearer))
    await act(async () =>
      cache.setQueryData(['auth', 'session'], (value: any) => ({
        ...value,
        csrf_token: 'c'.repeat(64),
      })),
    )
    expect(document.body.textContent).not.toContain(bearer)
    await click('Create key')
    await until(() => expect(document.querySelector('input[name="name"]')).not.toBeNull())
    await fill('name', 'Held')
    await act(async () =>
      document.querySelector<HTMLInputElement>('input[name="model_ids"]')!.click(),
    )
    let release!: () => void
    held = {
      promise: new Promise<void>((resolve) => {
        release = resolve
      }),
      release: () => release(),
    }
    await act(async () =>
      document
        .querySelector('[role="dialog"] form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    await until(() =>
      expect(requests.filter((x) => x.method === 'post' && x.url === '/keys')).toHaveLength(2),
    )
    await act(async () =>
      cache.setQueryData(['auth', 'session'], (value: any) => ({
        ...value,
        csrf_token: 'd'.repeat(64),
      })),
    )
    held.release()
    await act(async () => {
      await Promise.resolve()
    })
    expect(document.body.textContent).not.toContain(bearer)
    expect(privateCache()).not.toContain(bearer)
  })
})
