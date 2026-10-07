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
import type { AdminOverview, ProviderQualityUnavailableReason } from '@/types/overview'
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
        quality: {
          provider_id: 'prv_1',
          name: 'Provider One',
          window_start: '2026-09-29T06:00:00Z',
          window_end: '2026-09-30T06:00:00Z',
          evaluated_at: '2026-09-30T06:00:00Z',
          latest_completed_at: '2026-09-30T05:59:00Z',
          data_through: '2026-09-30T05:59:00Z',
          requests: 100,
          eligible_attempts: 90,
          excluded_attempts: 10,
          credential_rejected_attempts: 0,
          unknown_attribution_attempts: 0,
          successes: 89,
          success_rate: 89 / 90,
          success_rate_bps: 9888,
          p95_duration_ms: 1500,
          known_duration_attempts: 90,
          unknown_duration_attempts: 0,
          rate_limited_attempts: 1,
          server_error_attempts: 0,
          may_lag: false,
          status: 'healthy',
        },
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
      subject_type: 'provider',
      subject_id: 'prv_1',
      subject_name: 'Provider One',
    },
    {
      id: 'alt_2',
      kind: 'provider_quality_degraded',
      detail_code: 'success_rate_below_threshold',
      severity: 'medium',
      state: 'open',
      occurrence_count: 1,
      first_seen_at: '2026-09-30T05:30:00Z',
      last_seen_at: '2026-09-30T05:30:00Z',
      updated_at: '2026-09-30T05:30:00Z',
      etag: 'alert-2',
      subject_type: 'provider',
      subject_id: 'prv_1',
      subject_name: 'Provider One',
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
      subject_type: 'provider',
      subject_id: 'prv_1',
      subject_name: 'Provider One',
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
let currentSession = session
let sessionGate: Promise<void> | null
let permissionsGate: Promise<void> | null
let settingsWriteGate: Promise<void> | null
let settingsWriteFailureOnce: boolean
let settingsWriteStatusOnce: number | null
let qualityUnavailableReason: ProviderQualityUnavailableReason | null

beforeEach(async () => {
  i18n.addResourceBundle('en', 'notifications', en, true, true)
  i18n.addResourceBundle('zh', 'notifications', zh, true, true)
  await i18n.changeLanguage('en')
  currentSession = session
  sessionGate = null
  permissionsGate = null
  settingsWriteGate = null
  settingsWriteFailureOnce = false
  settingsWriteStatusOnce = null
  settings = {
    in_app_enabled: true,
    external_email: 'alerts@example.test',
    email_high: true,
    email_medium: true,
    etag: 'rev_01j00000000000000000000001',
    updated_at: '2026-09-30T00:00:00Z',
  }
  permissions = ['system.read', 'system.write']
  requests = []
  conflictOnce = false
  settingsReadFailureOnce = false
  settingsReadGate = null
  releaseSettingsRead = null
  qualityUnavailableReason = null
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
    if (config.url === '/auth/session') {
      if (sessionGate) await sessionGate
      response.data = currentSession
    } else if (config.url === '/auth/permissions') {
      if (permissionsGate) await permissionsGate
      response.data = { permissions }
    } else if (config.url === '/admin/overview') {
      const data = structuredClone(overview)
      if (qualityUnavailableReason) {
        data.provider_readiness.items[0].quality = null
        data.provider_readiness.items[0].quality_unavailable_reason = qualityUnavailableReason
      }
      response.data = data
    } else if (config.url === '/notifications') {
      if (!permissions.includes('system.read')) {
        response.data = { items: [], unread_count: 0, next_cursor: null }
      } else if (config.params?.status === 'all' && config.params?.cursor) {
        response.data = {
          unread_count: 1,
          next_cursor: null,
          items: [
            {
              ...structuredClone(notifications.items[0]),
              id: 'ntf_2',
              alert_id: 'alt_route',
              kind: 'route_unavailable',
              detail_code: 'no_candidates',
              read: true,
              read_at: '2026-09-30T05:30:00Z',
              delivery_status: null,
              subject_type: 'model',
              subject_id: 'mdl_1',
              subject_name: 'model-one',
            },
          ],
        }
      } else if (config.params?.status === 'all') {
        response.data = { ...structuredClone(notifications), next_cursor: 'next-notification' }
      } else response.data = structuredClone(notifications)
    } else if (config.url === '/notification-settings' && config.method === 'get') {
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
      if (settingsWriteGate) await settingsWriteGate
      if (settingsWriteStatusOnce !== null) {
        const status = settingsWriteStatusOnce
        settingsWriteStatusOnce = null
        throw new AxiosError('held old request rejected', '', config, undefined, {
          ...response,
          status,
          data: {},
        })
      }
      if (settingsWriteFailureOnce) {
        settingsWriteFailureOnce = false
        throw new AxiosError('unknown save outcome', '', config, undefined, {
          ...response,
          status: 503,
          data: {},
        })
      }
      if (conflictOnce) {
        conflictOnce = false
        settings = {
          ...settings,
          external_email: 'newer@example.test',
          etag: 'rev_01j00000000000000000000002',
        }
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
      settings = { ...settings, ...body, etag: 'rev_01j00000000000000000000003' }
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
    expect(document.body.textContent).toContain('P95 1,500 ms')
    expect(document.body.textContent).toContain('Calls today')
    expect(document.body.textContent).toContain('Tokens today')
    expect(document.body.textContent).toContain('Unknown')
    expect(document.body.textContent).toContain('Platform token trend')
    expect(document.body.textContent).toContain('Top models by token use')
    expect(document.body.textContent).toContain('Runtime publication failed.')
    expect(document.querySelector('table[aria-label="Operational alerts"]')).toBeTruthy()
  })

  it('renders a bounded localized reason when provider quality is unavailable', async () => {
    qualityUnavailableReason = 'query_budget'
    await render(<AdminOverviewPage />)
    await until(() =>
      expect(document.body.textContent).toContain(
        'Quality unavailable: the query budget was exhausted',
      ),
    )
    expect(document.body.textContent).not.toContain('P95 1,500 ms')
    await act(async () => void (await i18n.changeLanguage('zh')))
    expect(document.body.textContent).toContain('质量数据不可用：查询预算已用尽')
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

  it('loads bounded all-notification history by cursor and localizes affected scope', async () => {
    await render(<NotificationMenu />)
    await until(() =>
      expect(cache.getQueryData(notificationsKey('usr_operator', 'unread'))).toBeTruthy(),
    )
    await click('Notifications')
    await click('All')
    await until(() => expect(document.body.textContent).toContain('Provider: Provider One'))
    await click('Load more notifications')
    await until(() => expect(document.body.textContent).toContain('Model: model-one'))
    expect(document.body.textContent).toContain(
      'No eligible provider route was available for the model.',
    )
    const historyRequests = requests.filter(
      (request) => request.url === '/notifications' && request.params?.status === 'all',
    )
    expect(historyRequests).toHaveLength(2)
    expect(historyRequests[1].params?.cursor).toBe('next-notification')
  })

  it('loads the recipient inbox without system.read while the server excludes operational notifications', async () => {
    permissions = []
    await render(<NotificationMenu />)
    await until(() =>
      expect(cache.getQueryData(notificationsKey('usr_operator', 'unread'))).toBeTruthy(),
    )
    expect(requests.some((request) => request.url === '/notifications')).toBe(true)
    await click('Notifications')
    await until(() => expect(document.body.textContent).toContain('No new notifications'))
    expect(document.body.textContent).not.toContain('Runtime publication failed.')
    expect(requests.some((request) => request.url === '/notification-settings')).toBe(false)
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
      etag: 'rev_01j00000000000000000000002',
    })
  })

  it('allows both external email severities off without requiring an email address', async () => {
    await render(<AdminOverviewPage />)
    await until(() => expect(document.body.textContent).toContain('Provider One'))
    await click('Notification settings')
    await until(() => expect(document.querySelector('input[type="email"]')).toBeTruthy())
    await act(async () =>
      document
        .querySelector<HTMLElement>('[role="switch"][aria-label="Email high-severity alerts"]')!
        .click(),
    )
    await act(async () =>
      document
        .querySelector<HTMLElement>('[role="switch"][aria-label="Email medium-severity alerts"]')!
        .click(),
    )
    await setInput(document.querySelector<HTMLInputElement>('input[type="email"]')!, '')
    expect(button('Save settings').disabled).toBe(false)
    await click('Save settings')
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    const write = requests.find(
      (request) => request.url === '/notification-settings' && request.method === 'put',
    )
    expect(JSON.parse(write!.data)).toMatchObject({
      external_email: '',
      email_high: false,
      email_medium: false,
    })
  })

  it('renders localized alert category and bounded Provider scope without repeating its title', async () => {
    await render(<AdminOverviewPage />)
    await until(() => expect(document.body.textContent).toContain('Provider One'))
    const reviewButtons = [...document.querySelectorAll<HTMLButtonElement>('button')].filter(
      (item) => item.textContent?.trim() === 'Review alert',
    )
    expect(reviewButtons).toHaveLength(2)
    await act(async () => reviewButtons[1].click())
    await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain('Provider quality')
    expect(dialog.textContent).toContain('Provider: Provider One')
    expect(
      dialog.textContent?.match(/Provider success rate fell below its threshold\./g),
    ).toHaveLength(1)
    await act(async () => void (await i18n.changeLanguage('zh')))
    expect(dialog.textContent).toContain('供应商质量')
    expect(dialog.textContent).toContain('供应商：Provider One')
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
    expect(document.querySelector('input[type="email"]')).toBeNull()
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
    settings = {
      ...settings,
      external_email: 'newer@example.test',
      etag: 'rev_01j00000000000000000000002',
    }
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['notification-settings', 'usr_operator'] })
    })
    await until(() =>
      expect(
        cache
          .getQueriesData<NotificationSettings>({
            queryKey: ['notification-settings', 'usr_operator'],
          })
          .find(([, value]) => value)?.[1]?.etag,
      ).toBe('rev_01j00000000000000000000002'),
    )
    await click('Save settings')
    await until(() => expect(document.body.textContent).toContain('Settings changed elsewhere'))
    const write = requests.find(
      (request) => request.url === '/notification-settings' && request.method === 'put',
    )
    expect(JSON.parse(write!.data)).toMatchObject({
      external_email: 'draft@example.test',
      etag: 'rev_01j00000000000000000000001',
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

describe('Notification settings fresh authority and captured intent', () => {
  async function openSettings() {
    await render(<AdminOverviewPage />)
    await until(() => expect(document.body.textContent).toContain('Provider One'))
    await click('Notification settings')
    await until(() => expect(document.querySelector('input[type="email"]')).toBeTruthy())
  }
  const writes = () =>
    requests.filter((r) => r.url === '/notification-settings' && r.method === 'put')
  const emailInput = () => document.querySelector<HTMLInputElement>('input[type="email"]')!
  it('does not seed from cached settings and adopts a faster detail only after held fresh permissions', async () => {
    cache.setQueryData(['notification-settings', 'usr_operator'], {
      ...settings,
      external_email: 'unconfirmed@example.test',
    })
    await render(<AdminOverviewPage />)
    await until(() => expect(document.body.textContent).toContain('Provider One'))
    let release!: () => void
    permissionsGate = new Promise<void>((resolve) => {
      release = resolve
    })
    await click('Notification settings')
    await until(() => expect(requests.some((r) => r.url === '/notification-settings')).toBe(true))
    expect(document.querySelector('input[type="email"]')).toBeNull()
    expect(document.body.textContent).not.toContain('unconfirmed@example.test')
    await act(async () => {
      permissionsGate = null
      release()
    })
    await until(() => expect(emailInput()?.value).toBe('alerts@example.test'))
    expect(writes()).toHaveLength(0)
  })
  it('hides drafts during background settings errors and restores the human draft after a fresh read', async () => {
    await openSettings()
    await setInput(emailInput(), 'draft@example.test')
    let release!: () => void
    settingsReadGate = new Promise<void>((resolve) => {
      release = resolve
    })
    const refresh = cache.invalidateQueries({ queryKey: ['notification-settings', 'usr_operator'] })
    await until(() => expect(document.querySelector('input[type="email"]')).toBeNull())
    expect(button('Save settings').disabled).toBe(true)
    settingsReadFailureOnce = true
    await act(async () => {
      settingsReadGate = null
      release()
      await refresh
    })
    await until(() => expect(document.body.textContent).toContain('could not be loaded'))
    expect(document.querySelector('input[type="email"]')).toBeNull()
    await click('Retry')
    await until(() => expect(emailInput()?.value).toBe('draft@example.test'))
  })
  it('hides the draft during renewed Session and shared permission reads without replacing its ETag', async () => {
    await openSettings()
    await setInput(emailInput(), 'draft@example.test')
    for (const key of [sessionKey, ['permissions', 'usr_operator']]) {
      let release!: () => void
      const gate = new Promise<void>((resolve) => {
        release = resolve
      })
      if (key === sessionKey) sessionGate = gate
      else permissionsGate = gate
      const refresh = cache.invalidateQueries({ queryKey: key, exact: true })
      await until(() => expect(document.querySelector('input[type="email"]')).toBeNull())
      expect(button('Save settings').disabled).toBe(true)
      await act(async () => {
        sessionGate = null
        permissionsGate = null
        release()
        await refresh
      })
      await until(() => expect(emailInput()?.value).toBe('draft@example.test'))
    }
    await click('Save settings')
    await until(() => expect(writes()).toHaveLength(1))
    expect(JSON.parse(writes()[0].data).etag).toBe('rev_01j00000000000000000000001')
  })
  it('locks the exact in-flight body and keeps CSRF out of the mutation variables', async () => {
    await openSettings()
    await setInput(emailInput(), 'captured@example.test')
    let release!: () => void
    settingsWriteGate = new Promise<void>((resolve) => {
      release = resolve
    })
    await click('Save settings')
    await until(() => expect(writes()).toHaveLength(1))
    expect(emailInput().disabled).toBe(true)
    expect(document.querySelector('[role="switch"]')!.hasAttribute('data-disabled')).toBe(true)
    const body = JSON.parse(writes()[0].data)
    expect(body).toEqual({
      external_email: 'captured@example.test',
      email_high: true,
      email_medium: true,
      etag: 'rev_01j00000000000000000000001',
    })
    const variables = cache
      .getMutationCache()
      .getAll()
      .map((m) => m.state.variables)
    expect(JSON.stringify(variables)).not.toContain('csrf-overview')
    expect(variables[0]).toMatchObject({ actor: 'usr_operator', body })
    await act(async () => {
      settingsWriteGate = null
      release()
    })
    await until(() => expect(document.querySelector('input[type="email"]')).toBeNull())
  })
  it('does not resolve an uncertain save by incidental refetch and requires explicit review for a new write', async () => {
    await openSettings()
    await setInput(emailInput(), 'captured@example.test')
    settingsWriteFailureOnce = true
    await click('Save settings')
    await until(() => expect(document.body.textContent).toContain('Save outcome is unknown'))
    await until(() => expect(button('Keep my draft').disabled).toBe(false))
    expect(emailInput().disabled).toBe(true)
    expect(button('Save settings').disabled).toBe(true)
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['notification-settings', 'usr_operator'] })
    })
    expect(button('Save settings').disabled).toBe(true)
    expect(writes()).toHaveLength(1)
    await click('Keep my draft')
    expect(emailInput().value).toBe('captured@example.test')
    await click('Save settings')
    await until(() => expect(writes()).toHaveLength(2))
    expect(JSON.parse(writes()[1].data)).toEqual(JSON.parse(writes()[0].data))
  })
  it('ignores an old successful mutation after another actor opens fresh settings', async () => {
    await openSettings()
    await setInput(emailInput(), 'old@example.test')
    let release!: () => void
    settingsWriteGate = new Promise<void>((resolve) => {
      release = resolve
    })
    await click('Save settings')
    await until(() => expect(writes()).toHaveLength(1))
    currentSession = { ...session, user: { ...session.user, id: 'usr_new_actor' } }
    await act(async () => {
      await cache.invalidateQueries({ queryKey: sessionKey, exact: true })
    })
    await until(() => expect(emailInput()?.value).toBe('alerts@example.test'))
    await act(async () => {
      settingsWriteGate = null
      release()
    })
    await until(() => expect(cache.getMutationCache().getAll()[0].state.status).toBe('success'))
    expect(emailInput().value).toBe('alerts@example.test')
    expect(
      cache
        .getQueriesData<NotificationSettings>({
          queryKey: ['notification-settings', 'usr_new_actor'],
        })
        .every(([, data]) => data?.external_email !== 'old@example.test'),
    ).toBe(true)
  })
  it('ignores old callbacks after the dialog is unmounted and reopened', async () => {
    await openSettings()
    let release!: () => void
    settingsWriteGate = new Promise<void>((resolve) => {
      release = resolve
    })
    await click('Save settings')
    await until(() => expect(writes()).toHaveLength(1))
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <span>Other workspace</span>
        </QueryClientProvider>,
      ),
    )
    await render(<AdminOverviewPage />)
    await until(() => expect(document.body.textContent).toContain('Provider One'))
    await click('Notification settings')
    await until(() => expect(emailInput()).toBeTruthy())
    await setInput(emailInput(), 'new-opening@example.test')
    await act(async () => {
      settingsWriteGate = null
      release()
    })
    await until(() => expect(cache.getMutationCache().getAll()[0].state.status).toBe('success'))
    expect(emailInput().value).toBe('new-opening@example.test')
  })
  it('does not let a renewed successful Session certify or erase an older pending save', async () => {
    await openSettings()
    await setInput(emailInput(), 'captured@example.test')
    let release!: () => void
    settingsWriteGate = new Promise<void>((resolve) => {
      release = resolve
    })
    await click('Save settings')
    await until(() => expect(writes()).toHaveLength(1))
    await act(async () => {
      await cache.invalidateQueries({ queryKey: sessionKey, exact: true })
    })
    await until(() => expect(emailInput()?.disabled).toBe(true))
    await act(async () => {
      settingsWriteGate = null
      release()
    })
    await until(() => expect(document.body.textContent).toContain('Save outcome is unknown'))
    await until(() => expect(button('Keep my draft').disabled).toBe(false))
    expect(emailInput().value).toBe('captured@example.test')
    expect(button('Save settings').disabled).toBe(true)
    expect(writes()).toHaveLength(1)
  })
  it('does not expose a draft after write authority is removed during a held renewal', async () => {
    await openSettings()
    await setInput(emailInput(), 'private-draft@example.test')
    let release!: () => void
    permissionsGate = new Promise<void>((resolve) => {
      release = resolve
    })
    const refresh = cache.invalidateQueries({ queryKey: ['permissions', 'usr_operator'] })
    await until(() => expect(document.querySelector('input[type="email"]')).toBeNull())
    permissions = ['system.read']
    await act(async () => {
      permissionsGate = null
      release()
      await refresh
    })
    await until(() => expect(document.body.textContent).not.toContain('Notification settings'))
    expect(document.body.textContent).not.toContain('private-draft@example.test')
    expect(writes()).toHaveLength(0)
  })
  it('never resurrects an old actor draft after an intervening actor whose detail is held', async () => {
    await openSettings()
    await setInput(emailInput(), 'discarded@example.test')
    let release!: () => void
    settingsReadGate = new Promise<void>((resolve) => {
      release = resolve
    })
    currentSession = { ...session, user: { ...session.user, id: 'usr_other_actor' } }
    await act(async () => {
      await cache.invalidateQueries({ queryKey: sessionKey, exact: true })
    })
    await until(() => expect(document.querySelector('input[type="email"]')).toBeNull())
    currentSession = session
    await act(async () => {
      await cache.invalidateQueries({ queryKey: sessionKey, exact: true })
    })
    expect(document.querySelector('input[type="email"]')).toBeNull()
    await act(async () => {
      settingsReadGate = null
      release()
    })
    await until(() => expect(emailInput()?.value).toBe('alerts@example.test'))
    expect(emailInput().value).not.toBe('discarded@example.test')
  })
  it.each(['actor', 'generation'])(
    'does not dispatch a captured intent after live %s/CSRF replacement',
    async (changed) => {
      await openSettings()
      currentSession = {
        ...session,
        user: {
          ...session.user,
          id: changed === 'actor' ? 'usr_changed_before_dispatch' : session.user.id,
        },
        csrf_token: 'different-actor-csrf',
      }
      await act(async () => {
        button('Save settings').click()
        cache.setQueryData(sessionKey, currentSession)
      })
      await until(() => expect(cache.getMutationCache().getAll()[0].state.status).toBe('error'))
      expect(writes()).toHaveLength(0)
      expect(cache.getMutationCache().getAll()[0].state.error?.message).toContain('not dispatched')
      expect(
        JSON.stringify(
          cache
            .getMutationCache()
            .getAll()
            .map((m) => m.state.variables),
        ),
      ).not.toContain('csrf')
    },
  )
})

// Independent actor-lifetime regression; product source is unchanged.
it('independent held save must ignore an old actor lifetime after A-B-A', async () => {
  await render(<AdminOverviewPage />)
  await until(() => expect(document.body.textContent).toContain('Provider One'))
  await click('Notification settings')
  await until(() => expect(document.querySelector('input[type="email"]')).toBeTruthy())
  const input = () => document.querySelector<HTMLInputElement>('input[type="email"]')!
  await setInput(input(), 'old-lifetime@example.test')
  let release!: () => void
  settingsWriteGate = new Promise<void>((resolve) => {
    release = resolve
  })
  await click('Save settings')
  await until(() =>
    expect(
      requests.filter((r) => r.url === '/notification-settings' && r.method === 'put'),
    ).toHaveLength(1),
  )
  cache.setQueryData(['permissions', 'usr_other_actor'], permissions)
  currentSession = { ...session, user: { ...session.user, id: 'usr_other_actor' } }
  await act(async () => {
    await cache.invalidateQueries({ queryKey: sessionKey, exact: true })
  })
  await until(() => expect(input()?.value).toBe('alerts@example.test'))
  currentSession = session
  await act(async () => {
    await cache.invalidateQueries({ queryKey: sessionKey, exact: true })
  })
  await until(() => expect(input()?.value).toBe('alerts@example.test'))
  await setInput(input(), 'new-lifetime@example.test')
  expect(document.body.textContent).not.toContain('Save outcome is unknown')
  await act(async () => {
    settingsWriteGate = null
    release()
  })
  await until(() => expect(cache.getMutationCache().getAll()[0].state.status).toBe('success'))
  expect(input().value).toBe('new-lifetime@example.test')
  expect(document.body.textContent).not.toContain('Save outcome is unknown')
})

it.each([200, 400, 409, 503])(
  'an old actor lifetime HTTP %i cannot change a new A pending intent/cache after A-B-A',
  async (oldStatus) => {
    await render(<AdminOverviewPage />)
    await until(() => expect(document.body.textContent).toContain('Provider One'))
    await click('Notification settings')
    const input = () => document.querySelector<HTMLInputElement>('input[type="email"]')!
    const writes = () =>
      requests.filter((r) => r.url === '/notification-settings' && r.method === 'put')
    await until(() => expect(input()?.value).toBe('alerts@example.test'))
    await setInput(input(), 'old-lifetime@example.test')
    let releaseOld!: () => void
    settingsWriteGate = new Promise<void>((resolve) => {
      releaseOld = resolve
    })
    await click('Save settings')
    await until(() => expect(writes()).toHaveLength(1))
    const oldIntent = cache.getMutationCache().getAll()[0].state.variables as {
      actorLifetime: number
    }
    const opening = cache.getQueriesData({
      queryKey: ['notification-settings', 'usr_operator'],
    })[0][0][2]
    cache.setQueryData(['permissions', 'usr_other_actor'], permissions)
    currentSession = { ...session, user: { ...session.user, id: 'usr_other_actor' } }
    await act(async () => {
      await cache.invalidateQueries({ queryKey: sessionKey, exact: true })
    })
    await until(() => expect(input()?.value).toBe('alerts@example.test'))
    currentSession = session
    await act(async () => {
      await cache.invalidateQueries({ queryKey: sessionKey, exact: true })
    })
    await until(() => expect(input()?.value).toBe('alerts@example.test'))
    expect(input().disabled).toBe(false)
    await setInput(input(), 'new-lifetime@example.test')
    let releaseNew!: () => void
    settingsWriteGate = new Promise<void>((resolve) => {
      releaseNew = resolve
    })
    await click('Save settings')
    await until(() => expect(writes()).toHaveLength(2))
    const newMutation = cache.getMutationCache().getAll()[1]
    const newIntent = newMutation.state.variables as { actorLifetime: number; body: unknown }
    expect(newIntent.actorLifetime).toBeGreaterThan(oldIntent.actorLifetime)
    const currentQuery = cache
      .getQueryCache()
      .findAll({ queryKey: ['notification-settings', 'usr_operator'] })
      .find((q) => q.getObserversCount() > 0)!
    expect(currentQuery.queryKey[2]).toBe(opening)
    const data = currentQuery.state.data
    const beforeGets = requests.filter(
      (r) => r.url === '/notification-settings' && r.method === 'get',
    ).length
    settingsWriteStatusOnce = oldStatus === 200 ? null : oldStatus
    await act(async () => {
      releaseOld()
    })
    await until(() =>
      expect(cache.getMutationCache().getAll()[0].state.status).toBe(
        oldStatus === 200 ? 'success' : 'error',
      ),
    )
    expect(input().value).toBe('new-lifetime@example.test')
    expect(input().disabled).toBe(true)
    expect(newMutation.state.status).toBe('pending')
    expect(newMutation.state.variables).toBe(newIntent)
    expect(JSON.parse(writes()[1].data)).toEqual(newIntent.body)
    expect(currentQuery.state.data).toBe(data)
    expect(currentQuery.state.fetchStatus).toBe('idle')
    expect(
      requests.filter((r) => r.url === '/notification-settings' && r.method === 'get'),
    ).toHaveLength(beforeGets)
    expect(document.body.textContent).not.toContain('Save outcome is unknown')
    expect(document.body.textContent).not.toContain('Settings changed elsewhere')
    await act(async () => {
      settingsWriteGate = null
      releaseNew()
    })
    await until(() => expect(newMutation.state.status).not.toBe('pending'))
  },
)
