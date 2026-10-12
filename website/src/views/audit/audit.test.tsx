import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type AxiosAdapter, type InternalAxiosRequestConfig } from 'axios'
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
      const requestedEntry = structuredClone(entry)
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
                  ...requestedEntry,
                  actor_name:
                    config.params?.q === 'fast' ? 'Fresh actor' : requestedEntry.actor_name,
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
    expect(drawer.textContent).toContain(
      first.changes && 'reason' in first.changes ? first.changes.reason : '',
    )
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
  it('filters and renders the allowlisted system cleanup audit schema', async () => {
    entry = {
      ...first,
      action: 'system.instance.cleanup',
      resource_type: 'system_instance',
      resource_id: 'ins_retired',
      changes: {
        revision: 7,
      },
    }
    await mount()
    await until(() => expect(host.textContent).toContain('1 offline instance registration retired'))
    await select('Audit category', 'system')
    await until(() => expect(audits().at(-1)?.params.category).toBe('system'))
    await click('Open audit event aud_2')
    const drawer = document.querySelector('[role="dialog"]')!
    expect(drawer.textContent).toContain('ins_retired at revision 7')
    expect(drawer.textContent).not.toContain('undefined')
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

it('renders the typed OIDC reason in English and Chinese without invented before/after or markup', async () => {
  await i18n.changeLanguage('en')
  const reason = '<img src=x onerror=alert(1)> Review corporate access 原因'
  entry = {
    ...first,
    action: 'account.oidc.bind',
    resource_type: 'oidc_binding',
    resource_id: 'oib_retained',
    changes: { kind: 'oidc_identity', reason },
  }
  await mount()
  await until(() => expect(host.textContent).toContain(`Reason: ${reason}`))
  await select('Audit category', 'identity')
  await until(() => expect(audits().at(-1)?.params.category).toBe('identity'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain(`Reason: ${reason}`)
  expect(drawer.textContent).toContain('oib_retained')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('oidc_identity')
  expect(document.querySelector('img')).toBeNull()
  expect(document.querySelector('script')).toBeNull()
  const reads = audits().length
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(drawer.textContent).toContain(`原因: ${reason}`)
  expect(drawer.textContent).toContain('account.oidc.bind')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(host.textContent).toContain(`原因: ${reason}`)
  expect(audits()).toHaveLength(reads)
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('keeps an OIDC event with unprojected details explicitly not recorded', async () => {
  await i18n.changeLanguage('en')
  entry = {
    ...first,
    action: 'identity.oidc.verify',
    resource_type: 'oidc_provider',
    resource_id: 'oidc',
    changes: null,
  }
  await mount()
  await until(() => expect(host.textContent).toContain('identity.oidc.verify'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain('Not recorded')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('verified successfully')
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('renders the typed OAuth reason in English and Chinese without invented before/after or markup', async () => {
  await i18n.changeLanguage('en')
  const reason = '<img src=x onerror=alert(1)> Review corporate access 原因'
  entry = {
    ...first,
    action: 'account.oauth.bind',
    resource_type: 'oauth_binding',
    resource_id: 'oab_retained',
    changes: { kind: 'oauth_identity', reason },
  }
  await mount()
  await until(() => expect(host.textContent).toContain(`Reason: ${reason}`))
  await select('Audit category', 'identity')
  await until(() => expect(audits().at(-1)?.params.category).toBe('identity'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain(`Reason: ${reason}`)
  expect(drawer.textContent).toContain('oab_retained')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('oauth_identity')
  expect(document.querySelector('img')).toBeNull()
  expect(document.querySelector('script')).toBeNull()
  const reads = audits().length
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(drawer.textContent).toContain(`原因: ${reason}`)
  expect(drawer.textContent).toContain('account.oauth.bind')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(host.textContent).toContain(`原因: ${reason}`)
  expect(audits()).toHaveLength(reads)
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('keeps an OAuth event with unprojected details explicitly not recorded', async () => {
  await i18n.changeLanguage('en')
  entry = {
    ...first,
    action: 'identity.oauth.verify',
    resource_type: 'oauth_provider',
    resource_id: 'oauth',
    changes: null,
  }
  await mount()
  await until(() => expect(host.textContent).toContain('identity.oauth.verify'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain('Not recorded')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('verified successfully')
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('renders the typed LDAP reason in English and Chinese without invented before/after or markup', async () => {
  await i18n.changeLanguage('en')
  const reason = '<img src=x onerror=alert(1)> Review corporate access 原因'
  entry = {
    ...first,
    action: 'account.ldap.bind',
    resource_type: 'ldap_binding',
    resource_id: 'ldb_retained',
    changes: { kind: 'ldap_identity', reason },
  }
  await mount()
  await until(() => expect(host.textContent).toContain(`Reason: ${reason}`))
  await select('Audit category', 'identity')
  await until(() => expect(audits().at(-1)?.params.category).toBe('identity'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain(`Reason: ${reason}`)
  expect(drawer.textContent).toContain('ldb_retained')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('ldap_identity')
  expect(document.querySelector('img')).toBeNull()
  expect(document.querySelector('script')).toBeNull()
  const reads = audits().length
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(drawer.textContent).toContain(`原因: ${reason}`)
  expect(drawer.textContent).toContain('account.ldap.bind')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(host.textContent).toContain(`原因: ${reason}`)
  expect(audits()).toHaveLength(reads)
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('keeps an LDAP event with unprojected details explicitly not recorded', async () => {
  await i18n.changeLanguage('en')
  entry = {
    ...first,
    action: 'identity.ldap.verify',
    resource_type: 'ldap_provider',
    resource_id: 'ldap',
    changes: null,
  }
  await mount()
  await until(() => expect(host.textContent).toContain('identity.ldap.verify'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain('Not recorded')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('verified successfully')
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('renders the typed SAML reason in English and Chinese without invented before/after or markup', async () => {
  await i18n.changeLanguage('en')
  const reason = '<img src=x onerror=alert(1)> Review corporate access 原因'
  entry = {
    ...first,
    action: 'account.saml.bind',
    resource_type: 'saml_binding',
    resource_id: 'smb_retained',
    changes: { kind: 'saml_identity', reason },
  }
  await mount()
  await until(() => expect(host.textContent).toContain(`Reason: ${reason}`))
  await select('Audit category', 'identity')
  await until(() => expect(audits().at(-1)?.params.category).toBe('identity'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain(`Reason: ${reason}`)
  expect(drawer.textContent).toContain('smb_retained')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('saml_identity')
  expect(document.querySelector('img')).toBeNull()
  expect(document.querySelector('script')).toBeNull()
  const reads = audits().length
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(drawer.textContent).toContain(`原因: ${reason}`)
  expect(drawer.textContent).toContain('account.saml.bind')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(host.textContent).toContain(`原因: ${reason}`)
  expect(audits()).toHaveLength(reads)
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('keeps an SAML event with unprojected details explicitly not recorded', async () => {
  await i18n.changeLanguage('en')
  entry = {
    ...first,
    action: 'identity.saml.verify',
    resource_type: 'saml_provider',
    resource_id: 'saml',
    changes: null,
  }
  await mount()
  await until(() => expect(host.textContent).toContain('identity.saml.verify'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain('Not recorded')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('verified successfully')
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('renders the typed GitHub reason in English and Chinese without invented before/after or markup', async () => {
  await i18n.changeLanguage('en')
  const reason = '<img src=x onerror=alert(1)> Review corporate access 原因'
  entry = {
    ...first,
    action: 'account.github.bind',
    resource_type: 'named_identity_binding',
    resource_id: 'nib_retained',
    changes: { kind: 'github_identity', reason },
  }
  await mount()
  await until(() => expect(host.textContent).toContain(`Reason: ${reason}`))
  await select('Audit category', 'identity')
  await until(() => expect(audits().at(-1)?.params.category).toBe('identity'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain(`Reason: ${reason}`)
  expect(drawer.textContent).toContain('nib_retained')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('github_identity')
  expect(document.querySelector('img')).toBeNull()
  expect(document.querySelector('script')).toBeNull()
  const reads = audits().length
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(drawer.textContent).toContain(`原因: ${reason}`)
  expect(drawer.textContent).toContain('account.github.bind')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(host.textContent).toContain(`原因: ${reason}`)
  expect(audits()).toHaveLength(reads)
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('keeps an GitHub event with unprojected details explicitly not recorded', async () => {
  await i18n.changeLanguage('en')
  entry = {
    ...first,
    action: 'identity.github.verify',
    resource_type: 'named_identity_provider',
    resource_id: 'github',
    changes: null,
  }
  await mount()
  await until(() => expect(host.textContent).toContain('identity.github.verify'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain('Not recorded')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('verified successfully')
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('renders the typed Google reason in English and Chinese without invented before/after or markup', async () => {
  await i18n.changeLanguage('en')
  const reason = '<img src=x onerror=alert(1)> Review corporate access 原因'
  entry = {
    ...first,
    action: 'account.google.bind',
    resource_type: 'named_identity_binding',
    resource_id: 'nib_retained',
    changes: { kind: 'google_identity', reason },
  }
  await mount()
  await until(() => expect(host.textContent).toContain(`Reason: ${reason}`))
  await select('Audit category', 'identity')
  await until(() => expect(audits().at(-1)?.params.category).toBe('identity'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain(`Reason: ${reason}`)
  expect(drawer.textContent).toContain('nib_retained')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('google_identity')
  expect(document.querySelector('img')).toBeNull()
  expect(document.querySelector('script')).toBeNull()
  const reads = audits().length
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(drawer.textContent).toContain(`原因: ${reason}`)
  expect(drawer.textContent).toContain('account.google.bind')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(host.textContent).toContain(`原因: ${reason}`)
  expect(audits()).toHaveLength(reads)
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('keeps a Google event with unprojected details explicitly not recorded', async () => {
  await i18n.changeLanguage('en')
  entry = {
    ...first,
    action: 'identity.google.verify',
    resource_type: 'named_identity_provider',
    resource_id: 'google',
    changes: null,
  }
  await mount()
  await until(() => expect(host.textContent).toContain('identity.google.verify'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain('Not recorded')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('verified successfully')
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('renders the typed Discord reason in English and Chinese without invented before/after or markup', async () => {
  await i18n.changeLanguage('en')
  const reason = '<img src=x onerror=alert(1)> Review corporate access 原因'
  entry = {
    ...first,
    action: 'account.discord.bind',
    resource_type: 'named_identity_binding',
    resource_id: 'nib_retained',
    changes: { kind: 'discord_identity', reason },
  }
  await mount()
  await until(() => expect(host.textContent).toContain(`Reason: ${reason}`))
  await select('Audit category', 'identity')
  await until(() => expect(audits().at(-1)?.params.category).toBe('identity'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain(`Reason: ${reason}`)
  expect(drawer.textContent).toContain('nib_retained')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('discord_identity')
  expect(document.querySelector('img')).toBeNull()
  expect(document.querySelector('script')).toBeNull()
  const reads = audits().length
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(drawer.textContent).toContain(`原因: ${reason}`)
  expect(drawer.textContent).toContain('account.discord.bind')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(host.textContent).toContain(`原因: ${reason}`)
  expect(audits()).toHaveLength(reads)
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('keeps a Discord event with unprojected details explicitly not recorded', async () => {
  await i18n.changeLanguage('en')
  entry = {
    ...first,
    action: 'identity.discord.verify',
    resource_type: 'named_identity_provider',
    resource_id: 'discord',
    changes: null,
  }
  await mount()
  await until(() => expect(host.textContent).toContain('identity.discord.verify'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain('Not recorded')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('verified successfully')
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('does not render extra Discord identity fields beyond the typed reason', async () => {
  const reason = 'Reviewed Discord link'
  entry = {
    ...first,
    action: 'account.discord.bind',
    resource_type: 'named_identity_binding',
    resource_id: 'nib_retained',
    changes: {
      kind: 'discord_identity',
      reason,
      subject: 'private-remote-subject',
      token: 'private-token',
    } as unknown as AuditRecord['changes'],
  }
  await mount()
  await until(() => expect(host.textContent).toContain(`Reason: ${reason}`))
  await click('Open audit event aud_2')
  expect(document.body.textContent).not.toContain('private-remote-subject')
  expect(document.body.textContent).not.toContain('private-token')
  expect(document.querySelector('[role="dialog"] pre')).toBeNull()
})

it('renders the typed Telegram reason in English and Chinese without invented before/after or markup', async () => {
  await i18n.changeLanguage('en')
  const reason = '<img src=x onerror=alert(1)> Review corporate access 原因'
  entry = {
    ...first,
    action: 'account.telegram.bind',
    resource_type: 'named_identity_binding',
    resource_id: 'nib_retained',
    changes: { kind: 'telegram_identity', reason },
  }
  await mount()
  await until(() => expect(host.textContent).toContain(`Reason: ${reason}`))
  await select('Audit category', 'identity')
  await until(() => expect(audits().at(-1)?.params.category).toBe('identity'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain(`Reason: ${reason}`)
  expect(drawer.textContent).toContain('nib_retained')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('telegram_identity')
  expect(document.querySelector('img')).toBeNull()
  expect(document.querySelector('script')).toBeNull()
  const reads = audits().length
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(drawer.textContent).toContain(`原因: ${reason}`)
  expect(drawer.textContent).toContain('account.telegram.bind')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(host.textContent).toContain(`原因: ${reason}`)
  expect(audits()).toHaveLength(reads)
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('keeps a Telegram event with unprojected details explicitly not recorded', async () => {
  await i18n.changeLanguage('en')
  entry = {
    ...first,
    action: 'identity.telegram.verify',
    resource_type: 'named_identity_provider',
    resource_id: 'telegram',
    changes: null,
  }
  await mount()
  await until(() => expect(host.textContent).toContain('identity.telegram.verify'))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain('Not recorded')
  expect(drawer.querySelector('pre')).toBeNull()
  expect(drawer.textContent).not.toContain('undefined')
  expect(drawer.textContent).not.toContain('verified successfully')
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})

it('does not render extra Telegram identity fields beyond the typed reason', async () => {
  const reason = 'Reviewed Telegram link'
  entry = {
    ...first,
    action: 'account.telegram.bind',
    resource_type: 'named_identity_binding',
    resource_id: 'nib_retained',
    changes: {
      kind: 'telegram_identity',
      reason,
      subject: 'private-remote-subject',
      token: 'private-token',
    } as unknown as AuditRecord['changes'],
  }
  await mount()
  await until(() => expect(host.textContent).toContain(`Reason: ${reason}`))
  await click('Open audit event aud_2')
  expect(document.body.textContent).not.toContain('private-remote-subject')
  expect(document.body.textContent).not.toContain('private-token')
  expect(document.querySelector('[role="dialog"] pre')).toBeNull()
})

const commandID = '6f739b12-12ce-4a20-8da7-000000000001'
it.each([
  ['claimed', 'runtime.publication.claim', 'Claim recorded', '已记录认领'],
  [
    'refresh_returned_nil',
    'runtime.publication.finalize',
    'Refresh returned without error',
    '刷新返回且无错误',
  ],
  [
    'refresh_returned_error',
    'runtime.publication.finalize',
    'Refresh returned an error',
    '刷新返回错误',
  ],
] as const)(
  'renders historical publication %s separately from committed result and transport metadata',
  async (outcome, action, english, chinese) => {
    const reason = '<script>Historical reason 原因</script>'
    entry = {
      ...first,
      action,
      resource_type: 'system_instance',
      resource_id: 'ins_retained',
      changes: { kind: 'runtime_publication', command_request_id: commandID, reason, outcome },
    }
    await mount()
    await until(() => expect(host.textContent).toContain(english))
    await click('Open audit event aud_2')
    const drawer = document.querySelector('[role="dialog"]')!
    expect(drawer.textContent).toContain(`Publication command request ID: ${commandID}`)
    expect(drawer.textContent).toContain('Related request IDNot recorded')
    expect(drawer.textContent).toContain('ResultCommitted')
    expect(drawer.textContent).toContain('Historical command event only.')
    expect(drawer.textContent).toContain(reason)
    expect(drawer.querySelector('pre')).toBeNull()
    expect(drawer.querySelector('script')).toBeNull()
    expect(drawer.querySelector('a')).toBeNull()
    const reads = audits().length
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(drawer.textContent).toContain(chinese)
    expect(drawer.textContent).toContain(commandID)
    expect(drawer.textContent).toContain('不表示当前命令状态')
    expect(audits()).toHaveLength(reads)
    expect(requests.every((request) => request.method === 'get')).toBe(true)
  },
)

it('keeps malformed publication details unavailable without fetching a receipt', async () => {
  entry = {
    ...first,
    action: 'runtime.publication.finalize',
    resource_type: 'system_instance',
    changes: null,
  }
  await mount()
  await until(() => expect(host.textContent).toContain(entry.action))
  await click('Open audit event aud_2')
  const drawer = document.querySelector('[role="dialog"]')!
  expect(drawer.textContent).toContain('Not recorded')
  expect(drawer.textContent).not.toContain('Refresh returned')
  expect(requests.some((request) => request.url?.includes('publication-commands'))).toBe(false)
})

it('hides selected historical facts during same-actor Session renewal and rejects a retained row callback', async () => {
  entry = {
    ...first,
    changes: {
      kind: 'runtime_publication',
      command_request_id: commandID,
      reason: 'Recorded original reason',
      outcome: 'claimed',
    },
  }
  await mount()
  await until(() => expect(host.textContent).toContain('Recorded original reason'))
  const row = host.querySelector<HTMLTableRowElement>('tbody tr')!
  const propsKey = Object.getOwnPropertyNames(row).find((key) => key.startsWith('__reactProps$'))!
  const retained = (row as unknown as Record<string, { onClick: () => void }>)[propsKey].onClick
  await click('Open audit event aud_2')
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain(commandID)
  let release: (() => void) | undefined
  const adapter = client.defaults.adapter
  client.defaults.adapter = async (config) => {
    if (config.url === '/auth/session')
      await new Promise<void>((resolve) => {
        release = resolve
      })
    return (adapter as AxiosAdapter)(config)
  }
  let renewal: Promise<unknown> | undefined
  await act(async () => {
    renewal = cache.refetchQueries({ queryKey: sessionKey })
  })
  await until(() => expect(release).toBeDefined())
  expect(host.textContent).not.toContain('Recorded original reason')
  expect(document.querySelector('[role="dialog"]')?.textContent ?? '').not.toContain(commandID)
  const reads = audits().length
  await act(async () => retained())
  expect(audits()).toHaveLength(reads)
  await act(async () => {
    release?.()
    await renewal
  })
  await until(() => expect(host.textContent).toContain('Recorded original reason'))
  expect(document.querySelector('[role="dialog"]')).toBeNull()
})

it('rejects an obsolete audit response after permission revocation', async () => {
  await mount()
  await search('slow')
  await until(() => expect(releaseSlow).toBeDefined())
  await act(async () => {
    cache.setQueryData(['permissions', session.user.id], [])
  })
  await until(() => expect(host.textContent).toContain('Access denied'))
  await act(async () => {
    releaseSlow?.()
  })
  expect(host.textContent).not.toContain(first.actor_name)
  expect(document.querySelector('[role="dialog"]')).toBeNull()
})

it('scopes renewed actor reads separately and rejects the prior actor response', async () => {
  await mount()
  await search('slow')
  await until(() => expect(releaseSlow).toBeDefined())
  const oldRead = audits().at(-1)!
  entry = { ...second, actor_name: 'New authorized actor' }
  await act(async () => {
    cache.setQueryData(sessionKey, { ...session, user: { ...session.user, id: 'usr_new_actor' } })
  })
  await until(() => expect(host.textContent).toContain('New authorized actor'))
  expect(oldRead.signal?.aborted).toBe(true)
  await act(async () => {
    releaseSlow?.()
  })
  expect(host.textContent).not.toContain(first.actor_name)
  expect(host.textContent).toContain('New authorized actor')
})

it.each(['/auth/session', '/auth/permissions'])(
  'hides facts and the selected drawer when %s renewal fails',
  async (path) => {
    entry = {
      ...first,
      changes: {
        kind: 'runtime_publication',
        command_request_id: commandID,
        reason: 'Retained historical reason',
        outcome: 'claimed',
      },
    }
    await mount()
    await until(() => expect(host.textContent).toContain('Retained historical reason'))
    await click('Open audit event aud_2')
    const adapter = client.defaults.adapter as AxiosAdapter
    client.defaults.adapter = async (config) => {
      if (config.url === path)
        throw new AxiosError('Fixture', '', config, undefined, {
          config,
          status: 503,
          statusText: 'Unavailable',
          headers: new AxiosHeaders(),
          data: { code: 503, message: 'Unavailable' },
        })
      return adapter(config)
    }
    await act(async () => {
      await cache.refetchQueries({
        queryKey: path === '/auth/session' ? sessionKey : ['permissions', session.user.id],
      })
    })
    expect(host.textContent).not.toContain('Retained historical reason')
    expect(document.querySelector('[role="dialog"]')?.textContent ?? '').not.toContain(commandID)
    expect(host.querySelector('table')).toBeNull()
  },
)

it('does not dispatch a retained pagination callback after synchronous permission invalidation', async () => {
  await mount()
  await until(() => expect(host.textContent).toContain(first.actor_name))
  const more = button('Load more')
  const key = Object.getOwnPropertyNames(more).find((key) => key.startsWith('__reactProps$'))!
  const retained = (more as unknown as Record<string, { onClick: () => void }>)[key].onClick
  const reads = audits().length
  await act(async () => {
    void cache.invalidateQueries({
      queryKey: ['permissions', session.user.id],
      refetchType: 'none',
    })
    retained()
  })
  expect(audits()).toHaveLength(reads)
  expect(host.querySelector('table')).toBeNull()
})
