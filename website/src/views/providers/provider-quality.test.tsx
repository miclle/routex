import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import type { Provider } from '@/types/catalog'
import type { ProviderQualityPolicy, ProviderQualitySummary } from '@/types/provider-quality'
import ProvidersPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

const originalAdapter = client.defaults.adapter
const session = {
  user: { id: 'usr_admin', name: 'Admin', email: 'admin@example.test', role: 'admin' },
  csrf_token: 'csrf-provider-quality',
}
const provider: Provider = {
  id: 'prv_quality',
  name: 'Quality Provider',
  enabled: true,
  connections: [
    {
      id: 'con_quality',
      name: 'Primary route',
      enabled: true,
      base_url: 'https://provider.example.test/v1',
      protocol: 'openai_responses',
      credentials: [
        {
          id: 'cre_quality',
          name: 'Primary credential',
          priority: 0,
          enabled: true,
          verification_status: 'verified',
          verified_at: '2026-09-30T00:00:00Z',
        },
      ],
      provider_models: [
        {
          id: 'pm_quality',
          upstream_name: 'quality-model',
          enabled: true,
          supports_image_input: false,
          supports_pdf_input: false,
          etag: 'pm-1',
        },
      ],
    },
  ],
}

let quality: ProviderQualitySummary
let policy: ProviderQualityPolicy
let permissions: string[]
let requests: InternalAxiosRequestConfig[]
let conflictOnce: boolean
let root: Root
let host: HTMLDivElement
let cache: QueryClient

beforeEach(async () => {
  await i18n.changeLanguage('en')
  permissions = ['providers.read', 'providers.write', 'system.read', 'system.write']
  requests = []
  conflictOnce = false
  quality = {
    provider_id: provider.id,
    name: provider.name,
    window_start: '2026-09-29T06:00:00Z',
    window_end: '2026-09-30T06:00:00Z',
    evaluated_at: '2026-09-30T06:01:00Z',
    latest_completed_at: '2026-09-30T05:59:00Z',
    data_through: '2026-09-30T05:59:00Z',
    requests: 120,
    eligible_attempts: 100,
    excluded_attempts: 20,
    credential_rejected_attempts: 0,
    unknown_attribution_attempts: 3,
    successes: 99,
    success_rate: 0.99,
    success_rate_bps: 9900,
    p95_duration_ms: 1500,
    known_duration_attempts: 98,
    unknown_duration_attempts: 2,
    rate_limited_attempts: 4,
    server_error_attempts: 2,
    may_lag: false,
    status: 'healthy',
  }
  policy = {
    provider_id: provider.id,
    enabled: true,
    window_minutes: 60,
    minimum_attempts: 25,
    min_success_rate_bps: 9800,
    max_p95_duration_ms: 2500,
    etag: 'quality-policy-1',
    updated_at: '2026-09-30T05:00:00Z',
    updated_by: 'usr_admin',
    update_reason: 'Initial policy',
  }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  cache.setQueryData(sessionKey, session)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session') response.data = session
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/admin/providers')
      response.data = { items: [structuredClone(provider)] }
    else if (config.url === `/admin/providers/${provider.id}/status`) {
      const etag = `${'a'.repeat(64)}.${'b'.repeat(64)}`
      response.data = {
        id: provider.id,
        name: provider.name,
        enabled: true,
        can_edit: permissions.includes('providers.write'),
        etag,
      }
      response.headers.set('Cache-Control', 'private, no-store')
      response.headers.set('ETag', `"${etag}"`)
    } else if (config.url === `/admin/providers/${provider.id}/quality`)
      response.data = structuredClone(quality)
    else if (
      config.url === `/admin/providers/${provider.id}/quality-policy` &&
      config.method === 'get'
    )
      response.data = structuredClone(policy)
    else if (
      config.url === `/admin/providers/${provider.id}/quality-policy` &&
      config.method === 'put'
    ) {
      const input = JSON.parse(config.data)
      if (conflictOnce) {
        conflictOnce = false
        policy = { ...policy, minimum_attempts: 50, etag: 'quality-policy-2' }
        throw new AxiosError('conflict', '', config, undefined, {
          ...response,
          status: 412,
          data: {},
        })
      }
      policy = { ...policy, ...input, etag: 'quality-policy-3' }
      response.data = structuredClone(policy)
    }
    return response
  }
})

afterEach(async () => {
  await act(async () => root.unmount())
  document.querySelectorAll('[data-base-ui-portal]').forEach((node) => node.remove())
  host.remove()
  cache.clear()
  client.defaults.adapter = originalAdapter
})

async function render(path = `/admin/providers/${provider.id}`) {
  await act(async () => {
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="/admin/providers/:providerId" element={<ProvidersPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })
  await until(() => expect(document.body.textContent).toContain(provider.name))
}

async function until(assertion: () => void) {
  for (let index = 0; index < 80; index += 1) {
    await act(async () => await new Promise((resolve) => setTimeout(resolve, 5)))
    try {
      assertion()
      return
    } catch (error) {
      if (index === 79) throw error
    }
  }
}

function button(name: string) {
  const found = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent?.trim() === name || item.getAttribute('aria-label') === name,
  )
  expect(found, name).toBeDefined()
  return found!
}

async function click(name: string) {
  await act(async () => button(name).click())
}

async function setText(element: HTMLInputElement | HTMLTextAreaElement, value: string) {
  await act(async () => {
    const prototype =
      element instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype
    Object.getOwnPropertyDescriptor(prototype, 'value')!.set!.call(element, value)
    element.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

describe('provider quality workspace', () => {
  it('defaults to the addressable Overview and navigates through the Mockup tab hierarchy', async () => {
    await render()
    await until(() => expect(document.body.textContent).toContain('Observed quality'))
    expect(document.body.textContent).toContain('Upstream attempts')
    expect(document.body.textContent).toContain('Configuration ready')
    expect(button('Overview').getAttribute('aria-selected')).toBe('true')
    await click('Connections 1')
    await until(() =>
      expect(document.body.textContent).toContain('https://provider.example.test/v1'),
    )
    await click('Settings')
    await until(() => expect(document.body.textContent).toContain('Provider quality thresholds'))
  })

  it('renders real quality facts and distinguishes unknown, insufficient and lagging data', async () => {
    quality = {
      ...quality,
      success_rate: null,
      success_rate_bps: null,
      p95_duration_ms: null,
      latest_completed_at: null,
      data_through: null,
      may_lag: true,
      status: 'insufficient_data',
    }
    await render()
    await until(() => expect(document.body.textContent).toContain('Insufficient data'))
    expect(document.body.textContent).toContain('120')
    expect(document.body.textContent).toContain('Unknown')
    expect(document.body.textContent).toContain('No completed attempt observed')
    expect(document.body.textContent).toContain('may lag')
    expect(document.body.textContent).toContain(
      'platform attempts were excluded because historical provider attribution is unavailable',
    )
  })

  it('separates system.read policy visibility from system.write controls', async () => {
    permissions = ['providers.read', 'system.read']
    await render(`/admin/providers/${provider.id}?tab=settings`)
    await until(() => expect(document.body.textContent).toContain('98%'))
    expect(document.body.textContent).not.toContain('Configure thresholds')
    expect(requests.some((request) => request.url?.endsWith('/quality-policy'))).toBe(true)
  })

  it('preserves a policy draft through explicit fresh-ETag conflict review', async () => {
    conflictOnce = true
    await render(`/admin/providers/${provider.id}?tab=settings`)
    await until(() => expect(document.body.textContent).toContain('Configure thresholds'))
    await click('Configure thresholds')
    await until(() => expect(document.querySelector('textarea')).not.toBeNull())
    const attempts = document.querySelectorAll<HTMLInputElement>('input[type="number"]')[1]
    await setText(attempts, '40')
    await setText(document.querySelector('textarea')!, 'Tune the operational sample')
    await click('Save thresholds')
    await until(() => expect(document.body.textContent).toContain('changed elsewhere'))
    expect((document.querySelectorAll('input[type="number"]')[1] as HTMLInputElement).value).toBe(
      '40',
    )
    await click('Keep my draft')
    await click('Save thresholds')
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(document.body.textContent).toContain('Quality thresholds saved.')
    const writes = requests.filter(
      (request) => request.url?.endsWith('/quality-policy') && request.method === 'put',
    )
    expect(writes).toHaveLength(2)
    expect(JSON.parse(writes[1].data)).toMatchObject({
      minimum_attempts: 40,
      reason: 'Tune the operational sample',
      etag: 'quality-policy-2',
    })
  })

  it('switches the provider quality and settings copy live', async () => {
    await render(`/admin/providers/${provider.id}?tab=settings`)
    await until(() => expect(document.body.textContent).toContain('Provider quality thresholds'))
    await act(async () => void (await i18n.changeLanguage('zh')))
    expect(document.body.textContent).toContain('供应商质量阈值')
    expect(document.body.textContent).toContain('配置阈值')
    await click('概览')
    await until(() => expect(document.body.textContent).toContain('观测质量'))
    expect(document.body.textContent).toContain('上游尝试次数')
  })
})
