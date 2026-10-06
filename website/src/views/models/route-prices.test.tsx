import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import { getAdminModel } from '@/api/catalog'
import { getRoutePrices } from '@/api/pricing'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { Model } from '@/types/catalog'
import type { PricePage } from '@/types/pricing'
import i18n from '@/i18n'
import AdminModelsPage from './admin'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let identity: Session,
  permissions: string[],
  models: Record<string, Model>,
  prices: Record<string, PricePage>,
  requests: InternalAxiosRequestConfig[],
  failure: Record<string, number>,
  holds: Record<string, (() => Promise<void>) | undefined>
function model(id = 'mdl_one'): Model {
  return {
    id,
    name: `Private ${id}`,
    status: 'active',
    names: [{ name: `Private ${id}`, is_current: true, expires_at: null }],
    bindings: [
      {
        id: `bnd_${id}`,
        provider_model_id: `pmd_${id}`,
        provider_id: 'prv_one',
        connection_id: 'con_one',
        upstream_name: `upstream-${id}`,
        protocol: 'openai_chat',
        weight: 100,
        ready: true,
      },
    ],
    granted_user_ids: ['usr_one'],
  }
}
function page(target: Model['bindings'][number]): PricePage {
  return {
    etag: 'a'.repeat(64),
    currency: { platform_currency: 'USD', rates: { USD: '1', CNY: '7' } },
    items: [
      {
        id: `prc_${target.provider_model_id}`,
        provider_model_id: target.provider_model_id,
        provider_id: target.provider_id,
        upstream_name: target.upstream_name,
        protocol: target.protocol,
        context_threshold: 128000,
        update_source: 'api',
        follow_repository: false,
        rates: [
          {
            metric: 'INPUT_TOKEN',
            tier: 'base',
            unit: '1M_TOKEN',
            currency: 'USD',
            amount: '0.000000000000000001',
            enabled: true,
          },
          {
            metric: 'OUTPUT_TOKEN',
            tier: 'base',
            unit: '1M_TOKEN',
            currency: 'CNY',
            amount: '0',
            enabled: false,
          },
          {
            metric: 'INPUT_TOKEN',
            tier: 'long_context',
            unit: '1M_TOKEN',
            currency: 'USD',
            amount: '999.000',
            enabled: true,
          },
          {
            metric: 'IMAGE_INPUT',
            tier: 'base',
            unit: '1_IMAGE',
            currency: 'USD',
            amount: '8',
            enabled: true,
          },
        ],
      },
    ],
    next_cursor: '',
  }
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  requests = []
  failure = {}
  holds = {}
  permissions = ['models.read_all', 'prices.read']
  identity = {
    user: { id: 'usr_one', name: 'Reader', email: 'reader@example.invalid', role: 'member' },
    csrf_token: 'csrf-one',
  }
  models = { mdl_one: model(), mdl_two: model('mdl_two') }
  prices = {
    pmd_mdl_one: page(models.mdl_one.bindings[0]),
    pmd_mdl_two: page(models.mdl_two.bindings[0]),
  }
  prices.pmd_mdl_two.items[0].rates[0].amount = '12.000000000000000001'
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } },
  })
  cache.setQueryData(sessionKey, identity)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    await holds[config.url!]?.()
    const response = {
      config,
      status: failure[config.url!] || 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session') response.data = identity
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/admin/models')
      response.data = { items: [structuredClone(models.mdl_two)] }
    else if (config.url?.startsWith('/admin/models/') && config.method === 'get')
      response.data = structuredClone(models[config.url.split('/')[3]])
    else if (config.url?.endsWith('/price'))
      response.data = structuredClone(prices[config.url.split('/')[3]])
    else if (config.url === '/admin/providers') response.data = { items: [] }
    else if (config.url === '/admin/model-grantees')
      response.data = {
        items: [{ id: 'usr_one', name: 'Reader', email: 'reader@example.invalid' }],
      }
    else if (config.method === 'post' || config.method === 'put')
      response.data = structuredClone(models.mdl_one)
    if (response.status >= 400) throw new AxiosError('Denied', '', config, undefined, response)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
})
async function until(assertion: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assertion()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
async function mount(path = '/admin/models/mdl_one') {
  router = createMemoryRouter(
    [
      { path: '/admin/models/:modelId', element: <AdminModelsPage /> },
      { path: '/admin/models', element: <AdminModelsPage /> },
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
async function ready() {
  await mount()
  await until(() =>
    expect(host.textContent).toContain('0.000000000000000001 USD / 1 million tokens'),
  )
}
async function click(text: string) {
  const button = [...host.querySelectorAll('button')].find((item) => item.textContent === text)!
  expect(button).toBeTruthy()
  await act(async () => button.click())
}
async function fill(name: string, value: string) {
  const input = document.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
function priceRequests() {
  return requests.filter((item) => item.url?.endsWith('/price'))
}
function refresh() {
  return click('Refresh Model details')
}

describe('Read-only exact Model route prices', () => {
  it('uses exact detail and per-binding prices with unchanged decimal/currency/unit and live Chinese copy', async () => {
    await ready()
    expect(requests.some((item) => item.url === '/admin/models')).toBe(false)
    expect(
      requests.some(
        (item) =>
          item.url === '/admin/providers' ||
          item.url === '/admin/prices' ||
          item.url === '/admin/model-grantees',
      ),
    ).toBe(false)
    const row = host.querySelector('tbody tr')!
    expect(row.textContent).toContain('0 CNY / 1 million tokens')
    expect(row.textContent).toContain('Disabled')
    expect(row.textContent).not.toContain('999.000')
    expect(row.textContent).not.toContain('1 image')
    expect(host.textContent).toContain('Base input price')
    expect(host.textContent).toContain('Base output price')
    expect(host.querySelectorAll('tbody td')).toHaveLength(7)
    expect(
      priceRequests().every((item) => item.method === 'get' && item.params === undefined),
    ).toBe(true)
    expect(requests.every((item) => item.method === 'get')).toBe(true)
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain('基础输入价格')
    expect(host.textContent).toContain('0.000000000000000001 USD / 100 万 Token')
    expect(host.textContent).toContain('已停用')
  })
  it('distinguishes missing base rates from an explicitly enabled zero and never substitutes long-context prices', async () => {
    prices.pmd_mdl_one.items[0].rates = prices.pmd_mdl_one.items[0].rates.filter(
      (rate) => rate.tier === 'long_context',
    )
    await mount()
    await until(() => expect(host.textContent).toContain('Not configured'))
    expect(host.textContent).not.toContain('999.000')
    prices.pmd_mdl_one.items[0].rates = [
      {
        metric: 'INPUT_TOKEN',
        tier: 'base',
        unit: '1M_TOKEN',
        currency: 'USD',
        amount: '0',
        enabled: true,
      },
    ]
    await refresh()
    await until(() => expect(host.textContent).toContain('0 USD / 1 million tokens'))
    expect(host.textContent).toContain('Not configured')
    expect(host.querySelector('tbody tr')!.textContent).not.toContain('Disabled')
  })
  it('requires prices.read independently and never infers authority from an administrator label', async () => {
    identity.user.role = 'admin'
    permissions = ['models.read_all', 'models.write', 'prices.write']
    await mount()
    await until(() => expect(host.textContent).toContain('Price read permission required'))
    expect(priceRequests()).toHaveLength(0)
    expect(host.textContent).not.toContain('0.000000000000000001')
  })
  it('price permission alone cannot read a Model detail', async () => {
    identity.user.role = 'admin'
    permissions = ['prices.read']
    await mount()
    await until(() => expect(host.textContent).toContain('does not have permission'))
    expect(priceRequests()).toHaveLength(0)
    expect(requests.some((item) => item.url === '/admin/models/mdl_one')).toBe(false)
  })
  it('hides private detail, routes and prices during renewed exact detail reads, then keeps them hidden after denial', async () => {
    await ready()
    let release!: () => void
    const wait = new Promise<void>((resolve) => {
      release = resolve
    })
    holds['/admin/models/mdl_one'] = () => wait
    await refresh()
    await until(() => expect(host.textContent).not.toContain('Private mdl_one'))
    expect(host.textContent).not.toContain('upstream-mdl_one')
    expect(host.textContent).not.toContain('0.000000000000000001')
    failure['/admin/models/mdl_one'] = 403
    await act(async () => release())
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(host.textContent).not.toContain('Private mdl_one')
    expect(host.querySelector('table')).toBeNull()
  })
  it('hides prices during their independent renewal and on404/403 while preserving authorized Model metadata', async () => {
    await ready()
    let release!: () => void
    const wait = new Promise<void>((resolve) => {
      release = resolve
    })
    holds['/admin/provider-models/pmd_mdl_one/price'] = () => wait
    let renewed!: Promise<unknown>
    await act(async () => {
      renewed = cache.refetchQueries({ queryKey: ['admin', 'model-route-prices'] })
    })
    await until(() => expect(host.textContent).toContain('Refreshing price'))
    expect(host.textContent).not.toContain('0.000000000000000001')
    expect(host.textContent).toContain('Private mdl_one')
    failure['/admin/provider-models/pmd_mdl_one/price'] = 404
    await act(async () => release())
    await act(async () => renewed)
    await until(() => expect(host.textContent).toContain('Price unavailable'))
    expect(host.textContent).not.toContain('Not configured')
    expect(host.textContent).not.toContain('0.000000000000000001')
    delete holds['/admin/provider-models/pmd_mdl_one/price']
    failure['/admin/provider-models/pmd_mdl_one/price'] = 403
    await click('Retry price for upstream-mdl_one')
    await until(() => expect(priceRequests().length).toBeGreaterThan(2))
    await until(() => expect(host.textContent).toContain('Price unavailable'))
    expect(requests.filter((item) => item.url === '/admin/models/mdl_one').length).toBeGreaterThan(
      1,
    )
  })
  it('drops cached price authority during permission renewal and never re-fetches after revocation', async () => {
    await ready()
    let release!: () => void
    const wait = new Promise<void>((resolve) => {
      release = resolve
    })
    holds['/auth/permissions'] = () => wait
    let renewed!: Promise<unknown>
    await act(async () => {
      renewed = cache.refetchQueries({ queryKey: ['permissions', identity.user.id] })
    })
    await until(() => expect(host.textContent).not.toContain('0.000000000000000001'))
    permissions = ['models.read_all']
    await act(async () => release())
    await act(async () => renewed)
    await until(() => expect(host.textContent).toContain('Price read permission required'))
    const count = priceRequests().length
    await refresh()
    await until(() => expect(host.textContent).toContain('Private mdl_one'))
    expect(priceRequests()).toHaveLength(count)
  })
  it('reauthorizes exact detail and prices after same-actor Session renewal', async () => {
    await ready()
    let release!: () => void
    const wait = new Promise<void>((resolve) => {
      release = resolve
    })
    holds['/auth/session'] = () => wait
    let renewed!: Promise<unknown>
    await act(async () => {
      renewed = cache.refetchQueries({ queryKey: sessionKey })
    })
    await until(() => expect(host.textContent).not.toContain('Private mdl_one'))
    expect(host.textContent).not.toContain('0.000000000000000001')
    identity = { ...identity, csrf_token: 'csrf-new' }
    await act(async () => release())
    await act(async () => renewed)
    await until(() => expect(host.textContent).toContain('0.000000000000000001 USD'))
    expect(requests.filter((item) => item.url === '/admin/models/mdl_one').length).toBeGreaterThan(
      1,
    )
  })
  it('never restores obsolete detail/prices after target or actor changes', async () => {
    await ready()
    let release!: () => void
    const wait = new Promise<void>((resolve) => {
      release = resolve
    })
    holds['/admin/models/mdl_one'] = () => wait
    await refresh()
    await act(async () => router.navigate('/admin/models/mdl_two'))
    await until(() => expect(host.textContent).toContain('12.000000000000000001'))
    await act(async () => release())
    expect(host.textContent).not.toContain('Private mdl_one')
    expect(host.textContent).not.toContain('0.000000000000000001')
    identity = { ...identity, user: { ...identity.user, id: 'usr_two' } }
    failure['/admin/models/mdl_two'] = 403
    await act(async () => cache.setQueryData(sessionKey, identity))
    await until(() => expect(host.textContent).not.toContain('Private mdl_two'))
    expect(host.textContent).not.toContain('12.000000000000000001')
  })
  it('discards a late price response after the exact Model target switches', async () => {
    let release!: () => void
    const wait = new Promise<void>((resolve) => {
      release = resolve
    })
    holds['/admin/provider-models/pmd_mdl_one/price'] = () => wait
    await mount()
    await until(() => expect(host.textContent).toContain('Refreshing price'))
    await act(async () => router.navigate('/admin/models/mdl_two'))
    await until(() => expect(host.textContent).toContain('12.000000000000000001'))
    await act(async () => release())
    expect(host.textContent).not.toContain('0.000000000000000001')
    expect(host.textContent).not.toContain('Private mdl_one')
  })
  it('retains the existing weight and rename mutations with current CSRF and without pricing writes', async () => {
    permissions = ['models.read_all', 'models.write', 'prices.read']
    await ready()
    await fill('bnd_mdl_one', '100')
    await act(async () =>
      host
        .querySelector('form[aria-label="Provider routing weights"]')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    await until(() => expect(requests.some((item) => item.method === 'put')).toBe(true))
    expect(JSON.parse(requests.find((item) => item.method === 'put')!.data)).toEqual({
      weights: [{ binding_id: 'bnd_mdl_one', weight: 100 }],
    })
    expect(requests.find((item) => item.method === 'put')!.headers.get('X-CSRF-Token')).toBe(
      'csrf-one',
    )
    await until(() => expect(host.textContent).toContain('Private mdl_one'))
    await click('Rename')
    await until(() =>
      expect(document.querySelector('[role="dialog"] input[name="name"]')).not.toBeNull(),
    )
    await fill('name', 'Renamed')
    await act(async () =>
      document
        .querySelector('[role="dialog"] form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    await until(() => expect(requests.some((item) => item.url?.endsWith('/rename'))).toBe(true))
    expect(
      requests
        .filter((item) => item.method !== 'get')
        .every((item) => item.url?.startsWith('/admin/models/')),
    ).toBe(true)
  })
})

describe('Exact Model and route-price API validation', () => {
  it.each(['target', 'duplicate-binding', 'weight', 'names', 'grants'] as const)(
    'rejects malformed %s Model detail',
    async (variant) => {
      const value = models.mdl_one
      if (variant === 'target') value.id = 'mdl_two'
      if (variant === 'duplicate-binding') value.bindings.push({ ...value.bindings[0] })
      if (variant === 'weight') value.bindings[0].weight = NaN
      if (variant === 'names') value.names[0].expires_at = 'bad'
      if (variant === 'grants') value.granted_user_ids.push(value.granted_user_ids[0])
      await expect(getAdminModel('mdl_one')).rejects.toThrow('Invalid Model detail')
    },
  )
  it.each([
    'target',
    'provider',
    'protocol',
    'upstream',
    'amount',
    'unit',
    'duplicate',
    'overflow',
    'cursor',
    'threshold',
  ] as const)(
    'rejects malformed %s price read rather than matching another route',
    async (variant) => {
      const value = prices.pmd_mdl_one,
        price = value.items[0]
      if (variant === 'target') price.provider_model_id = 'pmd_other'
      if (variant === 'provider') price.provider_id = 'prv_other'
      if (variant === 'protocol') price.protocol = 'openai_responses'
      if (variant === 'upstream') price.upstream_name = 'other'
      if (variant === 'amount') price.rates[0].amount = '1e6'
      if (variant === 'unit') price.rates[0].unit = '1_IMAGE'
      if (variant === 'duplicate') price.rates.push({ ...price.rates[0] })
      if (variant === 'overflow') value.items.push({ ...price })
      if (variant === 'cursor') value.next_cursor = 'more'
      if (variant === 'threshold') price.context_threshold = null as unknown as 0
      await expect(getRoutePrices(models.mdl_one.bindings[0])).rejects.toThrow(
        'Invalid route price',
      )
    },
  )
})
