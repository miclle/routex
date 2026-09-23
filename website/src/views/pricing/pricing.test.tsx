import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { PricePage } from '@/types/pricing'
import ModelPriceTable from './model-price-table'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, container: HTMLDivElement, cache: QueryClient, page: PricePage
let permissions: string[], requests: InternalAxiosRequestConfig[], failure: number
const originalAdapter = client.defaults.adapter
beforeEach(async () => {
  await i18n.changeLanguage('en')
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  page = {
    etag: 'initial-etag',
    currency: { platform_currency: 'USD', rates: { USD: '1' } },
    items: [
      {
        id: 'prc_one',
        provider_model_id: 'pmo_one',
        provider_id: 'prv_one',
        upstream_name: 'text-model',
        protocol: 'openai_chat',
        context_threshold: 0,
        update_source: 'api',
        follow_repository: false,
        rates: [
          {
            id: 'rat_input',
            metric: 'INPUT_TOKEN',
            tier: 'base',
            unit: '1M_TOKEN',
            amount: '1.000000000000000001',
            currency: 'USD',
            enabled: true,
          },
        ],
      },
    ],
  }
  permissions = ['prices.read', 'prices.write']
  requests = []
  failure = 0
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
      response.data = { user: { id: 'usr_prices', role: 'member' }, csrf_token: 'csrf-prices' }
    if (config.url === '/auth/permissions') response.data = { permissions }
    if (config.url === '/admin/prices') {
      if (config.method === 'put') {
        if (failure) {
          response.status = failure
          throw new AxiosError('Failure', '', config, undefined, response)
        }
        const rate = JSON.parse(config.data).items[0].rates[0]
        const existing = page.items[0].rates.find(
          (item) => item.metric === rate.metric && item.tier === rate.tier,
        )
        if (existing) Object.assign(existing, rate)
        else page.items[0].rates.push({ id: 'rat_new', ...rate })
        page.etag = 'saved-etag'
      }
      response.data = structuredClone(page)
    }
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  container.remove()
  client.defaults.adapter = originalAdapter
  await i18n.changeLanguage('en')
})
async function until(check: () => void) {
  for (let i = 0; i < 60; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      check()
      return
    } catch (error) {
      if (i === 59) throw error
    }
  }
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <ModelPriceTable modelId="pmo_one" modelName="text-model" />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(container.textContent).toContain('1.000000000000000001'))
}
function button(text: string) {
  const found = [...document.querySelectorAll('button')].find((item) => item.textContent === text)
  expect(found, text).toBeDefined()
  return found!
}
async function click(text: string) {
  await act(async () => button(text).click())
}
const amountInput = () =>
  document.querySelector<HTMLInputElement>('[role="dialog"] input[inputmode="decimal"]')!
async function amount(value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      amountInput(),
      value,
    )
    amountInput().dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function submit() {
  await act(async () =>
    document
      .querySelector('[role="dialog"] form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
}
const writes = () => requests.filter((item) => item.method === 'put')
describe('current model price editing', () => {
  it('reads only the selected model and respects delegated read-only permission', async () => {
    permissions = ['prices.read']
    await render()
    expect(requests.find((item) => item.url === '/admin/prices')?.params).toEqual({
      provider_model_id: 'pmo_one',
    })
    expect(document.body.textContent).not.toContain('Add charge component')
    expect(
      [...container.querySelectorAll('button')].some((item) => item.textContent === 'Edit price'),
    ).toBe(false)
  })
  it('preserves decimal strings and sends explicit free and disabled values with ETag and CSRF', async () => {
    await render()
    await click('Edit price')
    expect(amountInput().value).toBe('1.000000000000000001')
    await amount('0')
    await act(async () => (document.querySelector('[role="switch"]') as HTMLElement).click())
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(writes()).toHaveLength(1)
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-prices')
    expect(JSON.parse(writes()[0].data)).toEqual({
      etag: 'initial-etag',
      items: [
        {
          provider_model_id: 'pmo_one',
          context_threshold: 0,
          rates: [
            {
              metric: 'INPUT_TOKEN',
              tier: 'base',
              unit: '1M_TOKEN',
              currency: 'USD',
              amount: '0',
              enabled: false,
            },
          ],
        },
      ],
    })
  })
  it('rejects duplicate components and unsupported numeric syntax before writing', async () => {
    await render()
    await click('Add charge component')
    await amount('1e3')
    await submit()
    expect(document.body.textContent).toContain('Enter a nonnegative decimal')
    await amount('2')
    await submit()
    expect(document.body.textContent).toContain('already exist')
    expect(writes()).toHaveLength(0)
  })
  it('requires explicit reload and review after a stale ETag without losing the draft', async () => {
    await render()
    await click('Edit price')
    await amount('2.000000000000000001')
    failure = 409
    await submit()
    await until(() => expect(document.body.textContent).toContain('Prices changed while'))
    expect(button('Save price').disabled).toBe(true)
    await submit()
    expect(writes()).toHaveLength(1)
    page.etag = 'new-etag'
    page.items[0].rates[0].amount = '3'
    failure = 0
    await click('Reload and review')
    await until(() => expect(button('Save price').disabled).toBe(false))
    expect(amountInput().value).toBe('2.000000000000000001')
    await submit()
    await until(() => expect(writes()).toHaveLength(2))
    expect(JSON.parse(writes()[1].data).etag).toBe('new-etag')
  })
  it('translates validation while retaining the exact draft', async () => {
    await render()
    await click('Edit price')
    await amount('-1')
    await submit()
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('请输入非负十进制数')
    expect(amountInput().value).toBe('-1')
    expect(button('保存价格')).toBeDefined()
  })
  it('keeps conversion validation failures visible without closing the draft', async () => {
    await render()
    await click('Edit price')
    await amount('5')
    failure = 422
    await submit()
    await until(() =>
      expect(document.body.textContent).toContain('currency conversion is unavailable'),
    )
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    expect(page.items[0].rates[0].amount).toBe('1.000000000000000001')
  })
})
