import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/audit'
import zh from '@/i18n/locales/zh/audit'
import { sessionKey } from '@/hooks/use-auth'
import type { AuditRecord } from '@/types/audit'
import AuditPage from './index'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
const session = {
  user: { id: 'usr_auditor', name: 'Auditor', email: 'audit@example.test', role: 'member' },
  csrf_token: 'csrf',
}
const first: AuditRecord = {
  id: 'aud_2',
  actor_id: 'usr_actor',
  actor_name: '<img src=x onerror=alert(1)>',
  action: 'limits.update',
  resource_type: 'user',
  resource_id: 'usr_target',
  created_at: '2026-09-23T03:04:05Z',
  result: 'committed',
  source: null,
  ip: null,
  request_id: null,
  changes: {
    before: { rpm: null, concurrency: 2, ip_mode: 'none', ip_ranges: [] },
    after: { rpm: 0, concurrency: 2, ip_mode: 'allowlist', ip_ranges: ['127.0.0.1/32'] },
    reason: '<script>untrusted reason</script>',
    etag: '"lim_rev"',
  },
}
const second: AuditRecord = {
  ...first,
  id: 'aud_1',
  actor_name: 'Second actor',
  action: 'keys.create',
  resource_type: 'api_key',
  resource_id: 'key_historical',
  changes: null,
}
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  requests: InternalAxiosRequestConfig[],
  permissions: string[],
  failMore: boolean,
  empty: boolean
let entry: AuditRecord
let releaseSlow: (() => void) | undefined
beforeEach(async () => {
  i18n.addResourceBundle('en', 'audit', en, true, true)
  i18n.addResourceBundle('zh', 'audit', zh, true, true)
  requests = []
  entry = structuredClone(first)
  permissions = ['audit.read']
  failMore = false
  empty = false
  releaseSlow = undefined
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
    else if (config.url === '/admin/audit') {
      if (config.params?.q === 'slow')
        await new Promise<void>((resolve) => {
          releaseSlow = resolve
        })
      if (failMore && config.params?.cursor)
        throw new AxiosError('Fixture', '', config, undefined, {
          ...response,
          status: 503,
          data: { code: 503, message: 'Unavailable' },
        })
      response.data = empty
        ? { items: [], next_cursor: null }
        : config.params?.cursor
          ? { items: [second], next_cursor: null }
          : {
              items: [
                {
                  ...entry,
                  actor_name: config.params?.q === 'fast' ? 'Fresh actor' : entry.actor_name,
                },
              ],
              next_cursor: 'aud_2',
            }
    }
    return response
  }
})
afterEach(async () => {
  releaseSlow?.()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
})
async function until(check: () => void) {
  for (let n = 0; n < 100; n++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      check()
      return
    } catch (error) {
      if (n === 99) throw error
    }
  }
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <AuditPage />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(host.textContent).toContain(
      permissions.length ? 'Committed successful' : 'Access denied',
    ),
  )
}
const audits = () => requests.filter((request) => request.url === '/admin/audit')
function button(label: string) {
  const found = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
  expect(found, label).toBeDefined()
  return found!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function search(value: string) {
  const input = host.querySelector<HTMLInputElement>('input')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function select(label: string, value: string) {
  const input = host.querySelector<HTMLSelectElement>(`select[aria-label="${label}"]`)!
  await act(async () => {
    input.value = value
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
describe('read-only audit records', () => {
  it('does not fetch the audit API without the current explicit permission', async () => {
    permissions = []
    await mount()
    expect(audits()).toHaveLength(0)
    expect(host.querySelector('table')).toBeNull()
  })
  it('sends default filters, opens authoritative IDs/dates and renders actor and changes as plaintext', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('usr_target'))
    expect(audits()[0].params).toMatchObject({ range: '7d', category: 'all' })
    expect(audits()[0].signal).toBeDefined()
    await click('Open audit event aud_2')
    const drawer = document.querySelector('[role="dialog"]')!
    expect(drawer.textContent).toContain(first.actor_name)
    expect(drawer.textContent).toContain(first.changes!.reason)
    expect(drawer.textContent).toContain('aud_2')
    expect(drawer.textContent).toContain('usr_actor')
    expect(drawer.textContent).toContain('usr_target')
    expect(drawer.textContent).toContain(new Date(first.created_at).toLocaleString('en-US'))
    expect(drawer.textContent).toContain('"rpm": null')
    expect(drawer.textContent).toContain('"rpm": 0')
    expect(drawer.textContent).toContain('Not recorded')
    expect(drawer.textContent).not.toContain('Success')
    expect(document.querySelector('img')).toBeNull()
    expect(document.querySelector('script')).toBeNull()
    expect(requests.every((request) => request.method === 'get')).toBe(true)
  })
  it('appends cursor pages and resets pagination/selection when filters change', async () => {
    await mount()
    await until(() => expect(button('Load more')).toBeDefined())
    await click('Load more')
    await until(() => expect(host.textContent).toContain('Second actor'))
    expect(audits().at(-1)?.params.cursor).toBe('aud_2')
    await click('Open audit event aud_1')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('key_historical')
    await select('Audit time range', '24h')
    await until(() => expect(audits().at(-1)?.params.range).toBe('24h'))
    expect(audits().at(-1)?.params.cursor).toBeUndefined()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(host.textContent).not.toContain('Second actor')
    await select('Audit category', 'pricing')
    await search('literal_%')
    await until(() =>
      expect(audits().at(-1)?.params).toMatchObject({
        range: '24h',
        category: 'pricing',
        q: 'literal_%',
      }),
    )
  })
  it('aborts stale filter reads and does not display a late response from an older search', async () => {
    await mount()
    await search('slow')
    await until(() => expect(releaseSlow).toBeDefined())
    const slow = audits().find((request) => request.params.q === 'slow')!
    await search('fast')
    await until(() => expect(host.textContent).toContain('Fresh actor'))
    expect(slow.signal?.aborted).toBe(true)
    await act(async () => releaseSlow?.())
    expect(host.textContent).toContain('Fresh actor')
    expect(host.textContent).not.toContain(first.actor_name)
    await click('Clear search')
    await until(() => expect(audits().at(-1)?.params.q).toBeUndefined())
  })
  it('keeps loaded records during a next-page failure and offers a real retry', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('usr_target'))
    failMore = true
    await click('Load more')
    await until(() => expect(button('Retry loading more')).toBeDefined())
    expect(host.textContent).toContain('usr_target')
    failMore = false
    await click('Retry loading more')
    await until(() => expect(host.textContent).toContain('Second actor'))
  })
  it('shows an honest empty state and limits a literal search to100Unicode characters', async () => {
    empty = true
    await mount()
    await until(() => expect(host.textContent).toContain('No matching audit events'))
    await search('😀'.repeat(101))
    await until(() => expect(audits().at(-1)?.params.q).toBe('😀'.repeat(100)))
    expect(host.querySelector('table')).toBeNull()
    expect(host.textContent).not.toContain('Failure')
  })
  it('updates drawer labels and dates while preserving raw identities and open selection', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('usr_target'))
    await click('Open audit event aud_2')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    const drawer = document.querySelector('[role="dialog"]')!
    expect(drawer.textContent).toContain('审计事件详情')
    expect(drawer.textContent).toContain('未记录')
    expect(drawer.textContent).toContain('limits.update')
    expect(drawer.textContent).toContain('aud_2')
    expect(drawer.textContent).toContain(new Date(first.created_at).toLocaleString('zh-CN'))
    expect(host.querySelector('input')!.value).toBe('')
  })
})

it('preserves typed price decimals and source without inventing unavailable metadata', async () => {
  const price = {
    id: 'price_1',
    provider_id: 'prv_1',
    provider_model_id: 'pm_1',
    upstream_name: 'Native',
    protocol: 'openai_chat',
    context_threshold: 0 as const,
    update_source: 'csv',
    follow_repository: false,
    rates: [
      {
        metric: 'INPUT_TOKEN' as const,
        tier: 'base' as const,
        unit: '1M_TOKEN' as const,
        currency: 'USD' as const,
        amount: '12345678901234567890.000000000000000001',
        enabled: true,
      },
    ],
  }
  entry = {
    ...first,
    action: 'prices.update',
    resource_type: 'pricing',
    resource_id: 'platform',
    source: 'csv',
    changes: {
      before: { etag: '"price_old"', items: [] },
      after: { etag: '"price_new"', items: [price] },
    },
  }
  await mount()
  await until(() => expect(host.textContent).toContain('prices.update'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain(price.rates[0].amount)
  expect(drawer.textContent).toContain('csv')
  expect(drawer.textContent).toContain('Not recorded')
  expect(drawer.textContent).not.toContain('203.0.113.')
  expect(drawer.textContent).not.toContain('req_')
})
