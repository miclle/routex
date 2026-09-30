import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { notificationsKey } from '@/api/notifications'
import { NotificationMenu } from '@/components/app/NotificationMenu'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/notifications'
import zh from '@/i18n/locales/zh/notifications'
import { sessionKey } from '@/hooks/use-auth'
import type { AdminOverview } from '@/types/overview'
import type { NotificationSettings, NotificationsPage } from '@/types/notifications'
import AdminOverviewPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

const originalAdapter = client.defaults.adapter
const session = {
  user: { id: 'usr_operator', name: 'Operator', email: 'operator@example.test', role: 'admin' },
  csrf_token: 'csrf-overview',
}
const overview: AdminOverview = {
  observed_at: '2026-09-30T06:00:00Z',
  today: {
    calls: 82400,
    tokens: { value: null, known: '14800000', unknown_calls: 2 },
    success_rate: 0.9984,
    active_principals: 286,
  },
  token_trend: [
    { date: '2026-09-29T00:00:00Z', tokens: { value: '1200', known: '1200', unknown_calls: 0 } },
    { date: '2026-09-30T00:00:00Z', tokens: { value: null, known: '800', unknown_calls: 1 } },
  ],
  provider_readiness: {
    providers: 1,
    connections: 2,
    ready_connections: 1,
    unready_connections: 1,
    items: [
      {
        provider_id: 'prv_1',
        name: 'Provider One',
        connection_count: 2,
        ready_connection_count: 1,
        credential_count: 2,
        model_count: 3,
        status: 'degraded',
      },
    ],
  },
  top_models: [
    {
      id: 'mdl_1',
      name: 'model-one',
      calls: 12,
      tokens: { value: '1000', known: '1000', unknown_calls: 0 },
    },
  ],
  alerts: [
    {
      id: 'alt_1',
      kind: 'system_job_failure',
      detail_code: 'publication_failed',
      severity: 'high',
      state: 'open',
      occurrence_count: 2,
      first_seen_at: '2026-09-30T04:00:00Z',
      last_seen_at: '2026-09-30T05:00:00Z',
      updated_at: '2026-09-30T05:00:00Z',
      etag: 'alert-1',
    },
  ],
}
const notifications: NotificationsPage = {
  unread_count: 1,
  next_cursor: null,
  items: [
    {
      id: 'ntf_1',
      alert_id: 'alt_1',
      kind: 'system_job_failure',
      detail_code: 'publication_failed',
      severity: 'high',
      occurrence_count: 2,
      read: false,
      first_seen_at: '2026-09-30T04:00:00Z',
      last_seen_at: '2026-09-30T05:00:00Z',
      read_at: null,
      delivery_status: 'accepted',
      delivery_code: 'relay_accepted',
      delivery_attempts: 1,
      delivery_updated_at: '2026-09-30T05:01:00Z',
    },
  ],
}
let settings: NotificationSettings
let permissions: string[]
let root: Root
let host: HTMLDivElement
let cache: QueryClient
let requests: InternalAxiosRequestConfig[]
let conflictOnce: boolean
let settingsReadFailureOnce: boolean
let settingsReadGate: Promise<void> | null
let releaseSettingsRead: (() => void) | null

beforeEach(async () => {
  i18n.addResourceBundle('en', 'notifications', en, true, true)
  i18n.addResourceBundle('zh', 'notifications', zh, true, true)
  await i18n.changeLanguage('en')
  settings = {
    in_app_enabled: true,
    external_email: 'alerts@example.test',
    email_high: true,
    email_medium: true,
    etag: 'settings-1',
    updated_at: '2026-09-30T00:00:00Z',
  }
  permissions = ['system.read', 'system.write']
  requests = []
  conflictOnce = false
  settingsReadFailureOnce = false
  settingsReadGate = null
  releaseSettingsRead = null
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
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
    else if (config.url === '/admin/overview') response.data = structuredClone(overview)
    else if (config.url === '/notifications') response.data = structuredClone(notifications)
    else if (config.url === '/notification-settings' && config.method === 'get') {
      if (settingsReadGate) await settingsReadGate
      if (settingsReadFailureOnce) {
        settingsReadFailureOnce = false
        throw new AxiosError('settings unavailable', '', config, undefined, {
          ...response,
          status: 503,
          data: {},
        })
      }
      response.data = structuredClone(settings)
    } else if (config.url === '/notification-settings' && config.method === 'put') {
      const body = JSON.parse(config.data)
      if (conflictOnce) {
        conflictOnce = false
        settings = { ...settings, external_email: 'newer@example.test', etag: 'settings-2' }
        throw new AxiosError('conflict', '', config, undefined, {
          ...response,
          status: 412,
          data: {},
        })
      }
      if (body.etag !== settings.etag) {
        throw new AxiosError('conflict', '', config, undefined, {
          ...response,
          status: 412,
          data: {},
        })
      }
      settings = { ...settings, ...body, etag: 'settings-3' }
      response.data = structuredClone(settings)
    } else if (config.url?.endsWith('/read')) {
      response.data = {}
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

async function until(check: () => void) {
  await vi.waitFor(async () => {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 1))
    })
    check()
  })
}

async function render(node: React.ReactNode) {
  await act(async () => {
    root.render(<QueryClientProvider client={cache}>{node}</QueryClientProvider>)
    await Promise.resolve()
  })
  await until(() => expect(cache.getQueryState(sessionKey)?.fetchStatus).toBe('idle'))
}

function button(label: string) {
  const item = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (value) => value.textContent?.trim() === label || value.getAttribute('aria-label') === label,
  )
  expect(item, label).toBeDefined()
  return item!
}

async function click(label: string) {
  await act(async () => button(label).click())
}

async function setInput(input: HTMLInputElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

describe('F23 operations overview and notifications', () => {
  it('renders the Mockup hierarchy from real response states without inventing unknown totals', async () => {
    await render(<AdminOverviewPage />)
    await until(() => expect(document.body.textContent).toContain('Provider One'))
    expect(document.body.textContent).toContain('Calls today')
    expect(document.body.textContent).toContain('Tokens today')
    expect(document.body.textContent).toContain('Unknown')
    expect(document.body.textContent).toContain('Platform token trend')
    expect(document.body.textContent).toContain('Top models by token use')
    expect(document.body.textContent).toContain('Runtime publication failed.')
    expect(document.querySelector('table[aria-label="Operational alerts"]')).toBeTruthy()
  })

  it('gates the administration overview with system.read', async () => {
    permissions = []
    await render(<AdminOverviewPage />)
    await until(() => expect(document.body.textContent).toContain('Access denied'))
    expect(requests.some((request) => request.url === '/admin/overview')).toBe(false)
  })

  it('keeps alert settings and state writes hidden from read-only operators', async () => {
    permissions = ['system.read']
    await render(<AdminOverviewPage />)
    await until(() => expect(document.body.textContent).toContain('Provider One'))
    expect(document.body.textContent).not.toContain('Notification settings')
    await click('Review alert')
    await until(() => expect(document.body.textContent).toContain('Alert details'))
    expect(document.body.textContent).not.toContain('Mark handling')
    expect(document.body.textContent).not.toContain('Resolve')
  })

  it('uses a recipient-scoped query key and marks a real notification read', async () => {
    await render(<NotificationMenu />)
    await until(() =>
      expect(cache.getQueryData(notificationsKey('usr_operator', 'unread'))).toBeTruthy(),
    )
    await click('Notifications')
    await until(() => expect(document.body.textContent).toContain('Runtime publication failed.'))
    expect(document.body.textContent).toContain(
      'SMTP relay accepted the email; inbox delivery is not confirmed',
    )
    const item = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((value) =>
      value.textContent?.includes('Runtime publication failed.'),
    )
    expect(item).toBeDefined()
    await act(async () => item!.click())
    await until(() =>
      expect(requests.some((request) => request.url === '/notifications/ntf_1/read')).toBe(true),
    )
  })

  it('keeps the bell empty without fetching notifications when system.read is absent', async () => {
    permissions = []
    await render(<NotificationMenu />)
    await until(() =>
      expect(requests.some((request) => request.url === '/auth/permissions')).toBe(true),
    )
    expect(requests.some((request) => request.url === '/notifications')).toBe(false)
    await click('Notifications')
    await until(() => expect(document.body.textContent).toContain('No new notifications'))
  })

  it('preserves the draft until an ETag conflict is explicitly reviewed', async () => {
    conflictOnce = true
    await render(<AdminOverviewPage />)
    await until(() => expect(document.body.textContent).toContain('Provider One'))
    await click('Notification settings')
    await until(() => expect(document.querySelector('input[type="email"]')).toBeTruthy())
    await setInput(
      document.querySelector<HTMLInputElement>('input[type="email"]')!,
      'draft@example.test',
    )
    await click('Save settings')
    await until(() => expect(document.body.textContent).toContain('Settings changed elsewhere'))
    expect((document.querySelector('input[type="email"]') as HTMLInputElement).value).toBe(
      'draft@example.test',
    )
    await click('Keep my draft')
    await click('Save settings')
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    const writes = requests.filter(
      (request) => request.url === '/notification-settings' && request.method === 'put',
    )
    expect(writes).toHaveLength(2)
    expect(JSON.parse(writes[1].data)).toMatchObject({
      external_email: 'draft@example.test',
      email_high: true,
      email_medium: true,
      etag: 'settings-2',
    })
  })

  it('waits for fresh settings before enabling conflict review', async () => {
    conflictOnce = true
    await render(<AdminOverviewPage />)
    await until(() => expect(document.body.textContent).toContain('Provider One'))
    await click('Notification settings')
    await until(() => expect(document.querySelector('input[type="email"]')).toBeTruthy())
    await setInput(
      document.querySelector<HTMLInputElement>('input[type="email"]')!,
      'draft@example.test',
    )
    settingsReadGate = new Promise<void>((resolve) => {
      releaseSettingsRead = resolve
    })
    await click('Save settings')
    await until(() =>
      expect(document.body.textContent).toContain('Loading the latest saved settings…'),
    )
    expect(button('Use latest').disabled).toBe(true)
    expect(button('Keep my draft').disabled).toBe(true)
    await act(async () => releaseSettingsRead?.())
    await until(() => expect(button('Keep my draft').disabled).toBe(false))
  })

  it('recovers when loading the latest settings after a conflict fails', async () => {
    conflictOnce = true
    await render(<AdminOverviewPage />)
    await until(() => expect(document.body.textContent).toContain('Provider One'))
    await click('Notification settings')
    await until(() => expect(document.querySelector('input[type="email"]')).toBeTruthy())
    await setInput(
      document.querySelector<HTMLInputElement>('input[type="email"]')!,
      'draft@example.test',
    )
    settingsReadFailureOnce = true
    await click('Save settings')
    await until(() =>
      expect(document.body.textContent).toContain('The latest saved settings could not be loaded.'),
    )
    expect(button('Use latest').disabled).toBe(true)
    expect(button('Keep my draft').disabled).toBe(true)
    expect((document.querySelector('input[type="email"]') as HTMLInputElement).value).toBe(
      'draft@example.test',
    )
    await click('Retry')
    await until(() => expect(button('Keep my draft').disabled).toBe(false))
    expect(document.body.textContent).toContain('Latest saved email: newer@example.test')
  })

  it('keeps the draft ETag when settings refetch in the background', async () => {
    await render(<AdminOverviewPage />)
    await until(() => expect(document.body.textContent).toContain('Provider One'))
    await click('Notification settings')
    await until(() => expect(document.querySelector('input[type="email"]')).toBeTruthy())
    await setInput(
      document.querySelector<HTMLInputElement>('input[type="email"]')!,
      'draft@example.test',
    )
    settings = { ...settings, external_email: 'newer@example.test', etag: 'settings-2' }
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['notification-settings', 'usr_operator'] })
    })
    await until(() =>
      expect(
        cache.getQueryData<NotificationSettings>(['notification-settings', 'usr_operator'])?.etag,
      ).toBe('settings-2'),
    )
    await click('Save settings')
    await until(() => expect(document.body.textContent).toContain('Settings changed elsewhere'))
    const write = requests.find(
      (request) => request.url === '/notification-settings' && request.method === 'put',
    )
    expect(JSON.parse(write!.data)).toMatchObject({
      external_email: 'draft@example.test',
      etag: 'settings-1',
    })
  })

  it('switches the visible overview and notification copy to Chinese', async () => {
    await render(<AdminOverviewPage />)
    await until(() => expect(document.body.textContent).toContain('Provider One'))
    await act(async () => void (await i18n.changeLanguage('zh')))
    expect(document.body.textContent).toContain('平台 Token 趋势')
    expect(document.body.textContent).toContain('运营告警')
    expect(document.body.textContent).toContain('运行时发布失败。')
  })

  it('keeps English and Chinese interpolation keys aligned', () => {
    function entries(value: object, prefix = ''): string[] {
      return Object.entries(value)
        .flatMap(([key, item]) =>
          typeof item === 'object'
            ? entries(item, `${prefix}${key}.`)
            : [`${prefix}${key}:${(String(item).match(/{{.*?}}/g) ?? []).sort().join(',')}`],
        )
        .sort()
    }
    expect(entries(en)).toEqual(entries(zh))
  })
})
