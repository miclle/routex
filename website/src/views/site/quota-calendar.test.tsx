import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import client from '@/api/client'
import { quotaSettingsKey } from '@/api/quota-settings'
import type { QuotaSettings } from '@/types/quota-settings'
import SitePage from './index'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let root: Root, host: HTMLDivElement, cache: QueryClient
let settings: QuotaSettings, permissions: string[], requests: InternalAxiosRequestConfig[]
let failure: number, readFailure: boolean, hold: Promise<void> | undefined
let returned: QuotaSettings | undefined
beforeEach(async () => {
  await i18n.changeLanguage('en')
  settings = {
    time_zone: 'UTC',
    etag: 'initial',
    activated: false,
    coverage_start: null,
    editable: true,
  }
  permissions = ['system.read', 'site.write', 'limits.settings.write']
  requests = []
  failure = 0
  readFailure = false
  hold = undefined
  returned = undefined
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
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
      response.data = { user: { id: 'usr_calendar', role: 'member' }, csrf_token: 'calendar-csrf' }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/site')
      response.data = {
        name: 'RouteX',
        service_url: '',
        logo_url: '',
        footer: '',
        default_language: 'en',
        etag: 'site1',
        updated_at: '2026-09-30T00:00:00Z',
      }
    else if (config.url === '/admin/quota-settings') {
      if (config.method === 'put') {
        if (hold) await hold
        if (failure)
          throw new AxiosError('fixture', '', config, undefined, { ...response, status: failure })
        settings = { ...settings, time_zone: JSON.parse(config.data).time_zone, etag: 'saved' }
      } else if (readFailure)
        throw new AxiosError('fixture', '', config, undefined, { ...response, status: 503 })
      response.data = structuredClone(config.method === 'put' && returned ? returned : settings)
    } else throw new Error(`Unexpected fixture URL ${config.url}`)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
})
async function until(assert: () => void) {
  for (let index = 0; index < 100; index++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      assert()
      return
    } catch (error) {
      if (index === 99) throw error
    }
  }
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <SitePage />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(host.textContent).toContain(permissions.length ? 'Quota calendar' : 'Access denied'),
  )
  if (permissions.length)
    await until(() => expect(host.querySelector('#quota-calendar-zone')).not.toBeNull())
}
function button(label: string) {
  const found = [...document.querySelectorAll('button')].find(
    (element) => element.textContent === label,
  )
  expect(found, label).toBeDefined()
  return found!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function fill(name: string, value: string) {
  const element = host.querySelector<HTMLInputElement>(`[name="${name}"]`)!
  expect(element, name).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, value)
    element.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function draft(zone = 'Asia/Shanghai', reason = 'Organization calendar') {
  await fill('time_zone', zone)
  await fill('calendar_reason', reason)
}
async function submit() {
  await click('Review calendar change')
  expect(document.querySelector('[role="dialog"]')).not.toBeNull()
  await click('Apply calendar change')
}
const writes = () => requests.filter((request) => request.method === 'put')

describe('Installation quota calendar', () => {
  it('allows a calendar writer without querying or exposing the site editor', async () => {
    permissions = ['limits.settings.write']
    await mount()
    expect(host.querySelector('[name="name"]')).toBeNull()
    expect(requests.some((request) => request.url === '/site')).toBe(false)
    expect(button('Review calendar change').disabled).toBe(true)
    expect(host.querySelector<HTMLInputElement>('[name="time_zone"]')!.matches(':disabled')).toBe(
      false,
    )
  })
  it('denies access before querying and keeps read-only permissions independent from site writes', async () => {
    permissions = []
    await mount()
    expect(requests.some((request) => request.url === '/admin/quota-settings')).toBe(false)
    expect(requests.some((request) => request.url === '/site')).toBe(false)
    permissions = ['system.read', 'site.write']
    await act(async () => cache.invalidateQueries({ queryKey: ['permissions'] }))
    await until(() => expect(host.querySelector('#quota-calendar-zone')).not.toBeNull())
    expect(host.querySelector<HTMLInputElement>('[name="time_zone"]')!.matches(':disabled')).toBe(
      true,
    )
    expect(host.querySelector('[name="calendar_reason"]')).toBeNull()
    expect(document.body.textContent).not.toContain('Review calendar change')
    expect(writes()).toHaveLength(0)
  })
  it('uses server editable=false even when accounting has not activated', async () => {
    settings.editable = false
    await mount()
    expect(host.textContent).toContain('Not activated; historical quota usage is unknown')
    expect(host.textContent).toContain('server has locked calendar changes')
    expect(button('Review calendar change').disabled).toBe(true)
    expect(host.querySelector<HTMLInputElement>('[name="time_zone"]')!.matches(':disabled')).toBe(
      true,
    )
    expect(writes()).toHaveLength(0)
  })
  it('requires confirmation and sends the reviewed revision, exact trimmed values and CSRF only once', async () => {
    await mount()
    await draft('  Asia/Shanghai  ', '  Organization calendar  ')
    await click('Review calendar change')
    expect(writes()).toHaveLength(0)
    expect(document.body.textContent).toContain('Proposed calendar: Asia/Shanghai')
    expect(document.body.textContent).toContain('cannot be changed after accounting starts')
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await act(async () => {
      button('Apply calendar change').click()
      button('Apply calendar change').click()
    })
    await until(() => expect(writes()).toHaveLength(1))
    expect(JSON.parse(writes()[0].data)).toEqual({
      time_zone: 'Asia/Shanghai',
      reason: 'Organization calendar',
    })
    expect(writes()[0].headers.get('If-Match')).toBe('"initial"')
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('calendar-csrf')
    expect(document.querySelector<HTMLButtonElement>('[aria-label="Close"]')!.disabled).toBe(true)
    await act(async () => release())
    await until(() => expect(host.textContent).toContain('runtime application confirmed'))
    expect(host.textContent).toContain('Existing usage was not reset')
    expect(host.querySelector<HTMLInputElement>('[name="calendar_reason"]')!.value).toBe('')
  })
  it('bounds UTF-8 fields and lets the server validate supported zones without browser filtering', async () => {
    await mount()
    for (const [zone, reason] of [
      ['x'.repeat(101), 'review'],
      ['😀'.repeat(26), 'review'],
      ['UTC2', ''],
      ['UTC2', '😀'.repeat(501)],
    ]) {
      await draft(zone, reason)
      await click('Review calendar change')
      expect(host.textContent).toContain('1–2,000 UTF-8 bytes')
      expect(document.querySelector('[role="dialog"]')).toBeNull()
    }
    expect(writes()).toHaveLength(0)
    await draft('Unsupported/ServerZone')
    failure = 400
    await submit()
    await until(() => expect(host.textContent).toContain('Enter a supported time zone'))
    expect(JSON.parse(writes()[0].data).time_zone).toBe('Unsupported/ServerZone')
    expect(host.querySelector<HTMLInputElement>('[name="time_zone"]')!.value).toBe(
      'Unsupported/ServerZone',
    )
  })
  it('retains the draft after conflict and requires explicit review before using a fresh ETag', async () => {
    await mount()
    await draft()
    failure = 409
    await submit()
    await until(() => expect(host.textContent).toContain('calendar changed or accounting began'))
    expect(button('Review calendar change').disabled).toBe(true)
    settings = { ...settings, time_zone: 'Europe/London', etag: 'other' }
    failure = 0
    await click('Reload and review calendar')
    await until(() => expect(button('Review calendar change').disabled).toBe(false))
    expect(host.textContent).toContain('Reviewed calendar: Europe/London')
    expect(host.querySelector<HTMLInputElement>('[name="time_zone"]')!.value).toBe('Asia/Shanghai')
    expect(host.querySelector<HTMLInputElement>('[name="calendar_reason"]')!.value).toBe(
      'Organization calendar',
    )
    await submit()
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].headers.get('If-Match')).toBe('"other"')
  })
  it('blocks an activation race in the confirmation dialog, retains the draft, and locks after reload', async () => {
    await mount()
    await draft()
    await click('Review calendar change')
    settings = {
      ...settings,
      activated: true,
      editable: false,
      coverage_start: '2026-09-30T08:00:00Z',
    }
    await act(async () => cache.setQueryData(quotaSettingsKey, structuredClone(settings)))
    await until(() => expect(button('Apply calendar change').disabled).toBe(true))
    expect(writes()).toHaveLength(0)
    await click('Cancel calendar change')
    await click('Reload and review calendar')
    await until(() => expect(host.textContent).toContain('Changes locked by the server'))
    expect(button('Review calendar change').disabled).toBe(true)
    expect(host.querySelector<HTMLInputElement>('[name="time_zone"]')!.value).toBe('Asia/Shanghai')
    expect(host.textContent).toContain('Accounting coverage began')
  })
  it('locks uncertain drafts and retries identical publication intent even if accounting activates', async () => {
    await mount()
    await draft()
    failure = 503
    await submit()
    await until(() => expect(host.textContent).toContain('runtime application is unconfirmed'))
    expect(host.textContent).not.toContain('runtime application confirmed')
    expect(host.querySelector<HTMLInputElement>('[name="time_zone"]')!.matches(':disabled')).toBe(
      true,
    )
    settings = {
      ...settings,
      time_zone: 'Asia/Shanghai',
      etag: 'stored',
      activated: true,
      editable: false,
    }
    await act(async () => cache.setQueryData(quotaSettingsKey, structuredClone(settings)))
    failure = 0
    await click('Retry calendar application')
    await until(() => expect(host.textContent).toContain('runtime application confirmed'))
    expect(writes()).toHaveLength(2)
    expect(writes()[1].data).toBe(writes()[0].data)
    expect(writes()[1].headers.get('If-Match')).toBe('"initial"')
  })
  it('preserves recovery state on failed reload and switches existing errors without clearing drafts', async () => {
    await mount()
    await draft()
    failure = 503
    await submit()
    await until(() => expect(host.textContent).toContain('runtime application is unconfirmed'))
    readFailure = true
    await click('Reload and review calendar')
    await until(() => expect(host.textContent).toContain('recovery state are retained'))
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(host.textContent).toContain('配额日历')
    expect(host.textContent).toContain('尚未确认运行时应用')
    expect(host.querySelector<HTMLInputElement>('[name="time_zone"]')!.value).toBe('Asia/Shanghai')
    expect(host.querySelector<HTMLInputElement>('[name="time_zone"]')!.matches(':disabled')).toBe(
      true,
    )
    expect(button('重试日历应用')).toBeDefined()
    readFailure = false
    await click('重新加载并核对日历')
    await until(() =>
      expect(host.querySelector<HTMLInputElement>('[name="time_zone"]')!.matches(':disabled')).toBe(
        false,
      ),
    )
    expect(host.querySelector<HTMLInputElement>('[name="time_zone"]')!.value).toBe('Asia/Shanghai')
  })
})

describe('Quota calendar publication safeguards', () => {
  it('does not report success if the receipt contains a different calendar generation', async () => {
    await mount()
    await draft()
    returned = { ...settings, time_zone: 'Europe/London', etag: 'other' }
    await submit()
    await until(() => expect(host.textContent).toContain('calendar changed or accounting began'))
    expect(host.textContent).not.toContain('runtime application confirmed')
    expect(host.querySelector<HTMLInputElement>('[name="time_zone"]')!.value).toBe('Asia/Shanghai')
    expect(button('Review calendar change').disabled).toBe(true)
  })
  it('blocks confirmation if write permission is revoked without removing read access', async () => {
    await mount()
    await draft()
    await click('Review calendar change')
    permissions = ['system.read']
    await act(async () => cache.setQueryData(['permissions', 'usr_calendar'], permissions))
    await until(() => expect(button('Apply calendar change').disabled).toBe(true))
    expect(writes()).toHaveLength(0)
    await click('Cancel calendar change')
    expect(host.querySelector<HTMLInputElement>('[name="time_zone"]')!.matches(':disabled')).toBe(
      true,
    )
  })
})

describe('Uncertain calendar retry rejection', () => {
  it('keeps the original locked intent after a 503 then 403, requiring reconciliation or an identical successful retry', async () => {
    await mount()
    await draft('Asia/Shanghai', 'Original intent')
    failure = 503
    await submit()
    await until(() => expect(host.textContent).toContain('runtime application is unconfirmed'))
    failure = 403
    await click('Retry calendar application')
    await until(() => expect(writes()).toHaveLength(2))
    expect(host.querySelector<HTMLInputElement>('[name="time_zone"]')!.matches(':disabled')).toBe(
      true,
    )
    expect(
      host.querySelector<HTMLInputElement>('[name="calendar_reason"]')!.matches(':disabled'),
    ).toBe(true)
    expect(button('Review calendar change').disabled).toBe(true)
    expect(host.textContent).toContain('runtime application is unconfirmed')
    failure = 0
    await click('Retry calendar application')
    await until(() => expect(host.textContent).toContain('runtime application confirmed'))
    expect(writes()).toHaveLength(3)
    for (const request of writes()) {
      expect(request.data).toBe(writes()[0].data)
      expect(request.headers.get('If-Match')).toBe('"initial"')
    }
  })
})
