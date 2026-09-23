import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import client from '@/api/client'
import routes from '@/router'
import type { CurrencyPage } from '@/types/currency'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, host: HTMLDivElement, cache: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let page: CurrencyPage, permissions: string[], requests: InternalAxiosRequestConfig[]
let failure: number, hold: Promise<void> | undefined
const originalAdapter = client.defaults.adapter
beforeEach(() => {
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  page = {
    etag: 'initial',
    currency: { platform_currency: 'USD', rates: { USD: '1', EUR: '1.050000000000000001' } },
    required_currencies: ['USD', 'EUR'],
  }
  permissions = ['prices.read', 'prices.write', 'providers.read', 'registration.write']
  requests = []
  failure = 0
  hold = undefined
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/setup') response.data = { initialized: true }
    else if (config.url === '/auth/session')
      response.data = {
        user: {
          id: 'usr_currency',
          name: 'Currency Manager',
          email: 'currency@example.test',
          role: 'member',
        },
        csrf_token: 'csrf-currency',
      }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/admin/prices/currency') {
      if (config.method === 'put') {
        if (hold) await hold
        if (failure) {
          response.status = failure
          throw new AxiosError('Fixture error', '', config, undefined, response)
        }
        const body = JSON.parse(config.data)
        page = { ...page, etag: 'saved', currency: body.currency }
        response.data = { etag: page.etag, currency: page.currency, items: [] }
      } else response.data = structuredClone(page)
    }
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  client.defaults.adapter = originalAdapter
  host.remove()
})
async function until(assert: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 10))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
async function mount() {
  router = createMemoryRouter(routes, { initialEntries: ['/admin/currency'] })
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(host.textContent).toContain(
      permissions.includes('prices.read') ? 'Current platform currency' : 'Access denied',
    ),
  )
}
function button(label: string) {
  const result = [...document.querySelectorAll('button')].find((item) => item.textContent === label)
  expect(result, label).toBeDefined()
  return result!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
function rate(currency: string) {
  return host.querySelector<HTMLInputElement>(`input[aria-label="${currency} exchange rate"]`)!
}
async function fill(currency: string, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      rate(currency),
      value,
    )
    rate(currency).dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function changeCurrency(value: string) {
  await act(async () => {
    const select = host.querySelector<HTMLSelectElement>('form select')!
    select.value = value
    select.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
const writes = () => requests.filter((item) => item.method === 'put')
describe('platform currency configuration', () => {
  it('reads complete currency metadata without writing, orders navigation, and enforces read permission', async () => {
    await mount()
    expect(writes()).toHaveLength(0)
    expect(requests.filter((r) => r.url?.startsWith('/admin/prices')).map((r) => r.url)).toEqual([
      '/admin/prices/currency',
    ])
    expect(rate('USD').disabled).toBe(true)
    expect(rate('USD').value).toBe('1')
    expect(button('Save configuration').disabled).toBe(true)
    const navigation = host.querySelector('nav')!.textContent!
    expect(navigation.indexOf('Service access')).toBeLessThan(
      navigation.indexOf('Prices and exchange rates'),
    )
    expect(navigation.indexOf('Prices and exchange rates')).toBeLessThan(
      navigation.indexOf('System administration'),
    )
  })
  it('never fetches currency metadata without read access and shows no editing controls for a reader', async () => {
    permissions = []
    await mount()
    expect(requests.some((r) => r.url === '/admin/prices/currency')).toBe(false)
    permissions = ['prices.read']
    await act(async () => {
      cache.setQueryData(['permissions', 'usr_currency'], permissions)
    })
    await until(() => expect(rate('EUR')).not.toBeNull())
    expect(host.querySelector('fieldset')!.disabled).toBe(true)
    expect(host.textContent).not.toContain('Save configuration')
    expect(writes()).toHaveLength(0)
  })
  it('clears other rates on currency change, requires every enabled currency, and confirms exact decimal values', async () => {
    await mount()
    await changeCurrency('CNY')
    expect(rate('CNY').value).toBe('1')
    expect(rate('CNY').disabled).toBe(true)
    expect(rate('EUR').value).toBe('')
    expect(rate('USD').value).toBe('')
    await click('Save configuration')
    expect(host.textContent).toContain('Configure all currencies')
    expect(writes()).toHaveLength(0)
    await fill('USD', '7.123456789012345678')
    await fill('EUR', '8.2')
    await click('Save configuration')
    expect(document.querySelector('[role="dialog"]')!.textContent).toContain('USD → CNY')
    expect(document.querySelector('[role="dialog"]')!.textContent).toContain(
      'Historical amounts and original model prices will not change',
    )
    expect(writes()).toHaveLength(0)
    await click('Confirm save')
    await until(() => expect(host.textContent).toContain('Currency configuration saved.'))
    expect(JSON.parse(writes()[0].data)).toEqual({
      etag: 'initial',
      currency: {
        platform_currency: 'CNY',
        rates: { USD: '7.123456789012345678', CNY: '1', EUR: '8.2' },
      },
    })
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-currency')
  })
  it('rejects zero, negative, exponent, and excessive precision without writing', async () => {
    await mount()
    for (const invalid of ['0', '-1', '1e2', '0.0000000000000000001', '1234567890123456789']) {
      await fill('EUR', invalid)
      await click('Save configuration')
      expect(host.textContent).toContain('Enter a positive decimal')
      expect(document.querySelector('[role="dialog"]')).toBeNull()
    }
    expect(writes()).toHaveLength(0)
  })
  it('requires conflict review, retains drafts, and adopts current requirements atomically', async () => {
    await mount()
    await fill('EUR', '2.123456789012345678')
    failure = 409
    await click('Save configuration')
    await click('Confirm save')
    await until(() => expect(host.textContent).toContain('The price catalogue changed.'))
    expect(button('Save configuration').disabled).toBe(true)
    page = {
      etag: 'new',
      currency: {
        platform_currency: 'JPY',
        rates: { JPY: '1', USD: '140', EUR: '150', GBP: '175' },
      },
      required_currencies: ['USD', 'EUR', 'GBP'],
    }
    failure = 0
    await click('Reload and review')
    await until(() => expect(host.textContent).toContain('Current requirements loaded.'))
    expect(rate('EUR').value).toBe('2.123456789012345678')
    expect(host.textContent).toContain('Current platform currency: JPY')
    await click('Save configuration')
    expect(host.textContent).toContain('Configure all currencies')
    await fill('GBP', '1.3')
    await click('Save configuration')
    await click('Confirm save')
    await until(() => expect(writes()).toHaveLength(2))
    expect(JSON.parse(writes()[1].data).etag).toBe('new')
  })
  it('does not silently mix background catalogue generations with a draft or open confirmation', async () => {
    await mount()
    await fill('EUR', '2')
    await click('Save configuration')
    page = {
      ...page,
      etag: 'background',
      currency: { platform_currency: 'CNY', rates: { CNY: '1' } },
      required_currencies: ['GBP'],
    }
    await act(async () => {
      cache.setQueryData(['admin', 'pricing-currency'], structuredClone(page))
    })
    await until(() => expect(button('Confirm save').disabled).toBe(true))
    expect(host.textContent).toContain('Current platform currency: USD')
    expect(rate('EUR').value).toBe('2')
    await click('Cancel')
    await click('Reload and review')
    await until(() => expect(host.textContent).toContain('Current platform currency: CNY'))
    expect(rate('EUR').value).toBe('2')
    expect(writes()).toHaveLength(0)
  })
  it('prevents duplicate pending confirmation writes and preserves translated drafts', async () => {
    await mount()
    await fill('EUR', '2.5')
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.querySelector<HTMLInputElement>('input[aria-label="EUR 汇率"]')!.value).toBe('2.5')
    expect(host.textContent).toContain('新的调用使用新的换算配置')
    await act(async () => i18n.changeLanguage('en'))
    await click('Save configuration')
    let release!: () => void
    hold = new Promise<void>((resolve) => {
      release = resolve
    })
    const confirm = button('Confirm save')
    await act(async () => {
      confirm.click()
      confirm.click()
    })
    await until(() => expect(writes()).toHaveLength(1))
    expect(confirm.disabled).toBe(true)
    await act(async () => release())
    await until(() => expect(host.textContent).toContain('Currency configuration saved.'))
    expect(writes()).toHaveLength(1)
  })
})
