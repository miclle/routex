import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { saveSMTP, SMTPError, validMailbox } from '@/api/smtp'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/smtp'
import zh from '@/i18n/locales/zh/smtp'
import { sessionKey } from '@/hooks/use-auth'
import type { SMTPSettings, SMTPTest } from '@/types/smtp'
import SMTPPage from './index'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
const session = {
  user: { id: 'usr_a', name: 'Admin', email: 'admin@example.test', role: 'admin' },
  csrf_token: 'csrf',
}
const seed: SMTPSettings = {
  enabled: true,
  host: 'smtp.example.test',
  port: 587,
  security: 'STARTTLS',
  auth_configured: true,
  sender_name: 'RouteX',
  sender_email: 'sender@example.test',
  reply_to: '',
  etag: 'revision_1',
  updated_at: '2026-09-23T00:00:00Z',
  last_test: null,
}
const outcome: SMTPTest = {
  request_id: 'test-request-123456',
  config_etag: 'revision_1',
  status: 'accepted',
  duration_ms: 34,
  stages: [{ name: 'acceptance', status: 'passed', duration_ms: 12 }],
  started_at: '2026-09-23T00:00:00Z',
  completed_at: '2026-09-23T00:00:01Z',
}
let row: SMTPSettings,
  result: SMTPTest,
  root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  requests: InternalAxiosRequestConfig[],
  permissions: string[],
  failure: number,
  hold: boolean,
  release: (() => void) | undefined
beforeEach(async () => {
  i18n.addResourceBundle('en', 'smtp', en, true, true)
  i18n.addResourceBundle('zh', 'smtp', zh, true, true)
  await i18n.changeLanguage('en')
  row = structuredClone(seed)
  result = structuredClone(outcome)
  requests = []
  permissions = ['smtp.read', 'smtp.write', 'smtp.test']
  failure = 0
  hold = false
  release = undefined
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
    else if (config.method === 'get') response.data = structuredClone(row)
    else {
      if (hold)
        await new Promise<void>((resolve) => {
          release = resolve
        })
      if (failure)
        throw new AxiosError('secret raw error', '', config, undefined, {
          ...response,
          status: failure,
          data: { message: 'unsafe raw error' },
        })
      const body = JSON.parse(config.data)
      if (config.url === '/admin/smtp/test')
        response.data = { ...result, request_id: body.request_id }
      else {
        row = { ...row, ...body, etag: 'revision_next' }
        delete (row as unknown as Record<string, unknown>).auth
        response.data = row
      }
    }
    return response
  }
})
afterEach(async () => {
  release?.()
  await act(async () => root.unmount())
  host.remove()
  cache.clear()
  client.defaults.adapter = original
})
async function until(check: () => void) {
  await vi.waitFor(async () => {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 1))
    })
    check()
  })
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <SMTPPage />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(cache.getQueryData(['permissions', 'usr_a'])).toEqual(permissions))
  if (permissions.includes('smtp.read'))
    await until(() => expect(document.body.textContent).toContain('smtp.example.test:587'))
}
function button(label: string) {
  const item = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
  expect(item, label).toBeDefined()
  return item!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function input(name: string, value: string) {
  const field = document.querySelector<HTMLInputElement>(`[name="${name}"]`)!
  expect(field).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(field, value)
    field.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function select(name: string, value: string) {
  await act(async () => {
    const field = document.querySelector<HTMLSelectElement>(`select[name="${name}"]`)!
    field.value = value
    field.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function submit(label = 'Configure self-hosted SMTP') {
  await act(async () => {
    document
      .querySelector(`form[aria-label="${label}"]`)!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}
const writes = () => requests.filter((request) => request.method !== 'get')
const payload = () => JSON.parse(writes().at(-1)!.data)
describe('SMTP settings', () => {
  it('does not show an older accepted result as a new uncertain test outcome', async () => {
    row.last_test = structuredClone(outcome)
    await mount()
    await click('Configure self-hosted SMTP')
    await input('recipient', 'qa@example.test')
    failure = 503
    await submit('Send test email')
    const drawer = document.querySelector('[role="dialog"]')!
    expect(drawer.textContent).toContain('delivery outcome is uncertain')
    expect(drawer.textContent).not.toContain('Accepted by SMTP relay')
  })

  it('keeps both locale keys and interpolations aligned', () => {
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

  it('drops an unsaved authentication replacement when the drawer closes', async () => {
    await mount()
    await click('Configure self-hosted SMTP')
    await select('auth_action', 'replace')
    await input('smtp_username', 'unsaved-user')
    await input('smtp_password', 'unsaved-password')
    await click('Close')
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await click('Configure self-hosted SMTP')
    await select('auth_action', 'replace')
    expect((document.querySelector('[name="smtp_password"]') as HTMLInputElement).value).toBe('')
    expect((document.querySelector('[name="smtp_username"]') as HTMLInputElement).value).toBe('')
    expect(writes()).toHaveLength(0)
  })
  it('blocks new tests while a writable server draft is unsaved or SMTP is disabled', async () => {
    await mount()
    await click('Configure self-hosted SMTP')
    await input('recipient', 'qa@example.test')
    await input('smtp_host', 'draft.example.test')
    expect(button('Send test email').disabled).toBe(true)
    await submit('Send test email')
    expect(writes()).toHaveLength(0)
    row.enabled = false
    await click('Close')
    await until(() =>
      expect(cache.getQueryData<SMTPSettings>(['admin', 'smtp'])?.enabled).toBe(false),
    )
    await click('Configure self-hosted SMTP')
    await input('recipient', 'qa@example.test')
    expect(button('Send test email').disabled).toBe(true)
    expect(document.body.textContent).toContain('Save and enable SMTP')
  })
  it('preserves the exact keep action without credential fields when disabling SMTP', async () => {
    await mount()
    await click('Configure self-hosted SMTP')
    await act(async () => (document.querySelector('[role="switch"]') as HTMLButtonElement).click())
    await submit()
    expect(payload()).toMatchObject({ enabled: false, auth: { action: 'keep' } })
    expect(Object.keys(payload().auth)).toEqual(['action'])
  })

  it('denies read without fetching settings', async () => {
    permissions = []
    await mount()
    expect(document.body.textContent).toContain('Access denied')
    expect(requests.some((item) => item.url === '/admin/smtp')).toBe(false)
  })
  it('keeps read-only configuration visible and separates test authority', async () => {
    permissions = ['smtp.read', 'smtp.test']
    await mount()
    await click('Configure self-hosted SMTP')
    expect(document.querySelector('fieldset')!.disabled).toBe(true)
    expect(document.body.textContent).not.toContain('Save SMTP settings')
    await input('recipient', 'qa@example.test')
    await submit('Send test email')
    expect(writes()).toHaveLength(1)
    expect(writes()[0].url).toBe('/admin/smtp/test')
  })
  it('requires explicit auth replacement after endpoint changes and clears secret on close', async () => {
    await mount()
    await click('Configure self-hosted SMTP')
    await input('smtp_host', 'other.example.test')
    expect(button('Save SMTP settings').disabled).toBe(true)
    expect(document.body.textContent).toContain('saved credentials cannot be reused')
    await select('auth_action', 'replace')
    await input('smtp_username', 'new-user')
    await input('smtp_password', 'local-secret')
    await submit()
    expect(payload()).toMatchObject({
      host: 'other.example.test',
      auth: { action: 'replace', username: 'new-user', password: 'local-secret' },
      etag: 'revision_1',
    })
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf')
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await click('Configure self-hosted SMTP')
    expect(document.querySelector('[name="smtp_password"]')).toBeNull()
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((q) => q.state.data),
      ),
    ).not.toContain('local-secret')
  })
  it('supports explicit removal and rejects unencrypted retained authentication', async () => {
    await mount()
    await click('Configure self-hosted SMTP')
    await select('security', 'NONE')
    expect(button('Save SMTP settings').disabled).toBe(true)
    await select('auth_action', 'remove')
    await submit()
    expect(payload()).toMatchObject({ security: 'NONE', auth: { action: 'remove' } })
  })
  it('saves sender separately and requires a sender name', async () => {
    await mount()
    await click('Configure system sender')
    await input('sender_name', '')
    await submit('Configure system sender')
    expect(writes()).toHaveLength(0)
    await input('sender_name', 'Operations')
    await input('reply_to', 'reply@example.test')
    await submit('Configure system sender')
    expect(writes()[0].url).toBe('/admin/smtp/sender')
    expect(payload()).toEqual({
      sender_name: 'Operations',
      sender_email: 'sender@example.test',
      reply_to: 'reply@example.test',
      etag: 'revision_1',
    })
  })
  it('preserves drafts across conflict review and uses the reviewed revision', async () => {
    await mount()
    await click('Configure self-hosted SMTP')
    await input('smtp_host', 'new.example.test')
    await select('auth_action', 'remove')
    failure = 409
    await submit()
    expect(button('Save SMTP settings').disabled).toBe(true)
    row = { ...row, etag: 'revision_2', host: 'changed.example.test' }
    failure = 0
    await click('Reload and review')
    expect((document.querySelector('[name="smtp_host"]') as HTMLInputElement).value).toBe(
      'new.example.test',
    )
    expect(document.body.textContent).toContain('changed.example.test:587')
    await submit()
    expect(payload().etag).toBe('revision_2')
  })
  it('retains saved state on verification failure and sanitizes credential errors', async () => {
    await mount()
    await click('Configure self-hosted SMTP')
    await select('auth_action', 'remove')
    failure = 422
    await submit()
    expect(document.body.textContent).toContain('saved configuration was not replaced')
    expect(document.body.textContent).not.toContain('unsafe raw error')
    expect(row.etag).toBe('revision_1')
    const error = await saveSMTP(
      {
        ...seed,
        auth: { action: 'replace', username: 'secret-user', password: 'secret-password' },
      },
      'csrf',
    ).catch((error) => error)
    expect(error).toBeInstanceOf(SMTPError)
    expect(JSON.stringify(error)).not.toContain('secret')
  })
  it('uses only a fixed test recipient and reports relay acceptance honestly', async () => {
    await mount()
    await click('Configure self-hosted SMTP')
    expect(writes()).toHaveLength(0)
    await input('recipient', 'qa@example.test')
    await submit('Send test email')
    expect(Object.keys(payload()).sort()).toEqual(['etag', 'recipient', 'request_id'])
    expect(payload().request_id).toMatch(/^[A-Za-z0-9_-]{16,80}$/)
    expect(document.body.textContent).toContain('Accepted by SMTP relay')
    expect(document.body.textContent).toContain('does not confirm inbox delivery')
    expect(document.body.textContent).toContain('34 ms')
    expect(document.body.textContent).not.toContain('SMTP greeting')
  })
  it('preserves uncertain request identity and prevents double submit', async () => {
    await mount()
    await click('Configure self-hosted SMTP')
    await input('recipient', 'qa@example.test')
    hold = true
    failure = 503
    await submit('Send test email')
    await submit('Send test email')
    expect(writes()).toHaveLength(1)
    const first = payload()
    await act(async () => release?.())
    await until(() => expect(document.body.textContent).toContain('delivery outcome is uncertain'))
    failure = 0
    hold = false
    result.status = 'unknown'
    await click('Check existing result')
    expect(payload()).toEqual(first)
    expect(document.body.textContent).toContain('Unknown outcome')
    expect(document.body.textContent).toContain('do not resend automatically')
    expect((document.querySelector('[name="recipient"]') as HTMLInputElement).disabled).toBe(true)
    expect(writes()).toHaveLength(2)
  })
  it('blocks new sends with unsaved drafts and respects test-only permission', async () => {
    permissions = ['smtp.read', 'smtp.write']
    await mount()
    await click('Configure self-hosted SMTP')
    await input('smtp_host', 'new.example.test')
    expect(button('Send test email').disabled).toBe(true)
    await submit('Send test email')
    expect(writes()).toHaveLength(0)
  })
  it('shows cooldown without automatic retry and can reload stale test revisions', async () => {
    await mount()
    await click('Configure self-hosted SMTP')
    await input('recipient', 'qa@example.test')
    failure = 429
    await submit('Send test email')
    expect(document.body.textContent).toContain('30-second test cooldown')
    expect(writes()).toHaveLength(1)
    failure = 409
    await click('Check existing result')
    row.etag = 'revision_2'
    failure = 0
    await click('Reload and review')
    await submit('Send test email')
    expect(payload().etag).toBe('revision_2')
  })
  it('switches languages without losing local drafts and rejects mailbox lists', async () => {
    await mount()
    await click('Configure self-hosted SMTP')
    await input('recipient', 'a@example.test,b@example.test')
    await submit('Send test email')
    expect(writes()).toHaveLength(0)
    await input('smtp_host', 'draft.example.test')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.body.textContent).toContain(zh.binding)
    expect((document.querySelector('[name="smtp_host"]') as HTMLInputElement).value).toBe(
      'draft.example.test',
    )
    expect(validMailbox('Name <mail@example.test>')).toBe(false)
    expect(validMailbox('用户@example.test')).toBe(false)
    expect(validMailbox('a'.repeat(250) + '@example.test')).toBe(false)
    expect(validMailbox('local@localhost')).toBe(true)
  })
})
