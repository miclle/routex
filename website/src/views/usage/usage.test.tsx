import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/usage'
import zh from '@/i18n/locales/zh/usage'
import client from '@/api/client'
import type { UsageCount, UsageReport, UsageStats } from '@/types/usage'
import UsagePage, { ProjectUsagePanel } from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, container: HTMLDivElement, cache: QueryClient
let requests: InternalAxiosRequestConfig[],
  allowed: boolean,
  errorCode: number,
  empty: boolean,
  comparison: boolean
const originalAdapter = client.defaults.adapter
const counter = (value: string | null, known = '12'): UsageCount => ({
  value,
  known: value ?? known,
  unknown_calls: value === null ? 1 : 0,
})
const stats: UsageStats = {
  requests: 2,
  successes: 1,
  errors: 1,
  canceled: 0,
  success_rate: 0.5,
  average_duration_ms: 150,
  tokens: { input: counter(null), output: counter('2'), total: counter(null, '14') },
  amounts: [
    { currency: 'CNY', amount: '0', calls: 1 },
    { currency: 'USD', amount: '12345678901234567890.000000000000000001', calls: 1 },
  ],
  unknown_amount_calls: 0,
  pricing_statuses: { priced: 2 },
}
function fixture(): UsageReport {
  const zero = {
    ...stats,
    requests: 0,
    successes: 0,
    errors: 0,
    success_rate: null,
    average_duration_ms: null,
    tokens: { input: counter('0'), output: counter('0'), total: counter('0') },
    amounts: [],
    pricing_statuses: {},
  }
  const summary = empty ? zero : stats
  const period = {
    from: '2026-09-01T00:00:00Z',
    to: '2026-09-01T02:00:00Z',
    summary,
    trend: [
      { start: '2026-09-01T00:00:00Z', end: '2026-09-01T01:00:00Z', stats: summary },
      { start: '2026-09-01T01:00:00Z', end: '2026-09-01T02:00:00Z', stats: zero },
    ],
    models: empty
      ? []
      : [{ id: 'mdl_historical', name: 'Historical model', unknown: false, stats }],
    keys: empty ? [] : [{ id: 'key_rotated', unknown: false, stats }],
    connections: [{ id: 'con_historical', unknown: false, stats }],
  }
  return {
    timezone: 'UTC',
    granularity: 'hour',
    queried_at: '2026-09-23T12:00:00Z',
    latest_completed_at: empty ? null : '2026-09-01T00:00:01Z',
    source: 'persisted_call_records',
    may_lag: true,
    current: period,
    previous: comparison
      ? {
          ...period,
          from: '2026-08-31T22:00:00Z',
          to: '2026-09-01T00:00:00Z',
          trend: period.trend.map((b, index) => ({
            ...b,
            start: `2026-08-31T${22 + index}:00:00Z`,
            end: index === 0 ? '2026-08-31T23:00:00Z' : '2026-09-01T00:00:00Z',
          })),
        }
      : undefined,
    available_dimensions: ['model', 'key', 'connection'],
  }
}
beforeEach(async () => {
  i18n.addResourceBundle('en', 'usage', en, true, true)
  i18n.addResourceBundle('zh', 'usage', zh, true, true)
  await i18n.changeLanguage('en')
  requests = []
  allowed = true
  errorCode = 0
  empty = false
  comparison = false
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session') {
      response.data = { user: { id: 'usr_self', role: 'member' }, csrf_token: 'csrf' }
      return response
    }
    if (config.url === '/auth/permissions') {
      response.data = { permissions: allowed ? ['calls.read_all'] : [] }
      return response
    }
    if (errorCode) {
      response.status = errorCode
      throw new AxiosError('fixture query failed', '', config, undefined, response)
    }
    comparison = config.params?.compare === true
    response.data = fixture()
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  client.defaults.adapter = originalAdapter
  container.remove()
})
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 25))
  })
}
async function render(page = <UsagePage />) {
  await act(async () =>
    root.render(<QueryClientProvider client={cache}>{page}</QueryClientProvider>),
  )
  await settle()
  await settle()
}
function usageRequests() {
  return requests.filter((request) => request.url?.endsWith('/usage'))
}
async function submit() {
  await act(async () =>
    container
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
  await settle()
}
async function input(name: string, value: string) {
  const field = container.querySelector<HTMLInputElement>(`[name="${name}"]`)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(field, value)
    field.dispatchEvent(new Event('input', { bubbles: true }))
    field.dispatchEvent(new Event('change', { bubbles: true }))
  })
}

describe('usage reports', () => {
  it('shows exact currencies and unknown coverage without exposing admin dimensions', async () => {
    await render()
    expect(usageRequests()[0].url).toBe('/usage')
    expect(usageRequests()[0].signal).toBeDefined()
    expect(container.textContent).toContain('Unknown')
    expect(container.textContent).toContain('Known subtotal: 14')
    expect(container.textContent).toContain('USD 12,345,678,901,234,567,890.000000000000000001')
    expect(container.textContent).toContain('CNY 0')
    expect(container.textContent).not.toContain('con_historical')
    expect(container.querySelector('[name="user_id"]')).toBeNull()
    expect(container.textContent).not.toContain('Provider usage')
    expect(container.querySelector('svg[aria-labelledby]')).not.toBeNull()
    expect(container.textContent).toContain('key_rotated')
  })
  it.each(['openai_responses', 'anthropic_messages'])(
    'submits real %s model/key/timezone/compare filters and renders previous data',
    async (protocolValue) => {
      await render()
      await input('key_id', 'key_rotated')
      await input('model_id', 'mdl_historical')
      await input('timezone', 'Asia/Shanghai')
      await act(async () => {
        const protocol = container.querySelector<HTMLSelectElement>('[name="protocol"]')!
        protocol.value = protocolValue
        protocol.dispatchEvent(new Event('change', { bubbles: true }))
      })
      await act(async () => container.querySelector<HTMLButtonElement>('[role="switch"]')!.click())
      await submit()
      expect(usageRequests().at(-1)?.params).toMatchObject({
        key_id: 'key_rotated',
        model_id: 'mdl_historical',
        timezone: 'Asia/Shanghai',
        protocol: protocolValue,
        compare: true,
      })
      expect(container.querySelector('[data-series="previous"]')).not.toBeNull()
      expect(container.textContent).toContain('Previous period')
    },
  )
  it('isolates Project cache and requests from personal attribution', async () => {
    await render(<ProjectUsagePanel projectId="prj_owned" />)
    expect(usageRequests()[0].url).toBe('/projects/prj_owned/usage')
    expect(usageRequests().some((request) => request.url === '/usage')).toBe(false)
    expect(cache.getQueryCache().findAll({ queryKey: ['usage'] })[0].queryKey).toContainEqual([
      'project',
      'prj_owned',
    ])
    expect(container.querySelector('[name="user_id"]')).toBeNull()
    errorCode = 404
    await render(<ProjectUsagePanel projectId="prj_next" />)
    expect(usageRequests().at(-1)?.url).toBe('/projects/prj_next/usage')
    expect(container.textContent).not.toContain('key_rotated')
  })
  it('denies admin reads before querying and exposes only supported administrative dimensions', async () => {
    allowed = false
    await render(<UsagePage admin />)
    expect(usageRequests()).toHaveLength(0)
    expect(container.querySelector('[role="alert"]')).not.toBeNull()
    allowed = true
    await act(async () => cache.invalidateQueries({ queryKey: ['permissions'] }))
    await settle()
    await settle()
    expect(usageRequests()[0]?.url).toBe('/admin/usage')
    expect(container.querySelector('[name="connection_id"]')).not.toBeNull()
  })
  it('handles complete-report overflow and recovers without fabricating totals', async () => {
    errorCode = 422
    await render()
    expect(container.textContent).toContain('complete-report limit')
    expect(container.querySelector('svg[aria-labelledby]')).toBeNull()
    errorCode = 0
    const retry = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'Retry',
    )!
    await act(async () => retry.click())
    await settle()
    expect(container.textContent).toContain('Known subtotal: 14')
    errorCode = 403
    const refresh = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'Refresh',
    )!
    await act(async () => refresh.click())
    await settle()
    expect(container.textContent).toContain('no longer have access')
    expect(container.textContent).not.toContain('key_rotated')
  })
  it('localizes empty reports and keeps unknown rates distinct from zero', async () => {
    empty = true
    await i18n.changeLanguage('zh')
    await render()
    expect(container.textContent).toContain('当前范围和筛选条件下没有调用记录')
    expect(container.textContent).toContain('未知')
    expect(container.textContent).toContain('暂无已知费用')
    expect(container.textContent).not.toContain('NaN')
    expect(container.textContent).not.toContain('pricingStatuses.')
  })
})
