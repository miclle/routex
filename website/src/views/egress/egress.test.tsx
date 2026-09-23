import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/egress'
import zh from '@/i18n/locales/zh/egress'
import { sessionKey } from '@/hooks/use-auth'
import { saveEgress, EgressError, egressSelection } from '@/api/egress'
import type { Egress, EgressDiagnostic } from '@/types/egress'
import EgressPage from './index'
import EgressEditor from './editor'
import { ConnectionEgressControl } from './connection'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
const session = {
  user: { id: 'usr_a', name: 'Admin', email: 'admin@example.test', role: 'admin' },
  csrf_token: 'csrf',
}
const probe: EgressDiagnostic = {
  transport_ok: true,
  api_ok: false,
  http_status: 401,
  duration_ms: 31,
  stages: [
    { stage: 'target_dns', status: 'passed', duration_ms: 2 },
    { stage: 'proxy_auth', status: 'not_applicable' },
    { stage: 'target_tls', status: 'passed', duration_ms: 12 },
    { stage: 'api', status: 'failed', code: 'http_status', duration_ms: 17 },
  ],
}
const seed: Egress = {
  id: 'egr_a',
  name: 'Outbound A',
  host: 'proxy.example.test',
  port: 1080,
  kind: 'socks5',
  enabled: true,
  etag: 'rev_1',
  auth_configured: true,
  last_checked_at: null,
  last_diagnostic: null,
  providers: [],
}
let diagnosticResult: EgressDiagnostic
let row: Egress,
  root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  requests: InternalAxiosRequestConfig[],
  permissions: string[],
  failure: number,
  defaultID: string | null,
  release: (() => void) | undefined,
  hold: boolean
beforeEach(() => {
  i18n.addResourceBundle('en', 'egress', en, true, true)
  i18n.addResourceBundle('zh', 'egress', zh, true, true)
  diagnosticResult = structuredClone(probe)
  row = structuredClone(seed)
  requests = []
  permissions = ['egress.read', 'egress.write', 'egress.test', 'providers.read', 'providers.write']
  failure = 0
  defaultID = 'egr_a'
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
    else if (
      config.method === 'get' &&
      (config.url === '/admin/egresses' || config.url === '/admin/egress-options')
    )
      response.data = { items: [row] }
    else if (config.method === 'get' && config.url === '/admin/egress-default')
      response.data = { etag: 'default_rev', egress_id: defaultID }
    else if (config.method === 'get' && config.url === '/admin/providers')
      response.data = { items: [{ connections: [connection()] }] }
    else if (config.method !== 'get') {
      if (hold)
        await new Promise<void>((resolve) => {
          release = resolve
        })
      if (failure)
        throw new AxiosError('raw secret should not survive', '', config, undefined, {
          ...response,
          status: failure,
          data: { message: 'raw unsafe message' },
        })
      if (config.url?.endsWith('/test')) response.data = diagnosticResult
      else if (config.url?.endsWith('/egress'))
        response.data = { ...JSON.parse(config.data), connection_id: 'con_a', etag: 'rev_next' }
      else if (config.url === '/admin/egress-default') {
        defaultID = JSON.parse(config.data).egress_id
        response.data = { etag: 'default_next', egress_id: defaultID }
      } else
        response.data = { ...row, ...JSON.parse(config.data), etag: 'rev_next', auth: undefined }
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
const connection = () => ({
  id: 'con_a',
  name: 'Native',
  base_url: 'https://api.example.test/v1',
  protocol: 'openai_chat' as const,
  credentials: [],
  provider_models: [],
  egress_mode: 'default' as const,
  egress_id: null,
  etag: row.etag,
})
async function until(check: () => void) {
  await vi.waitFor(async () => {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 1))
    })
    check()
  })
}
async function mount(node: ReactNode = <EgressPage />) {
  await act(async () =>
    root.render(<QueryClientProvider client={cache}>{node}</QueryClientProvider>),
  )
  await until(() => expect(cache.getQueryData(['permissions', 'usr_a'])).toEqual(permissions))
}
function button(label: string) {
  const item = [...document.querySelectorAll<HTMLButtonElement>('button,[role="menuitem"]')].find(
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
async function submit() {
  await act(async () => {
    document
      .querySelector('[role="dialog"] form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}
const writes = () => requests.filter((request) => request.method !== 'get')
const editor = () => mount(<EgressEditor initial={row} onClose={() => {}} onSaved={() => {}} />)
describe('egress interface', () => {
  it('denies access without issuing resource requests', async () => {
    permissions = []
    await mount()
    expect(host.textContent).toContain('Access denied')
    expect(requests.some((request) => request.url?.startsWith('/admin/egress'))).toBe(false)
  })
  it('shows measured facts and disables write/test actions for read-only users', async () => {
    permissions = ['egress.read']
    await mount()
    await until(() => expect(host.textContent).toContain('Outbound A'))
    expect(host.textContent).toContain('Not checked')
    expect(host.textContent).not.toContain('just now')
    expect(host.querySelector('button')?.textContent).not.toBe('Add network egress')
    await click('Actions for Outbound A')
    expect(button('Test connection').getAttribute('aria-disabled')).toBe('true')
    expect(button('Edit network egress').getAttribute('aria-disabled')).toBe('true')
  })
  it('requires a fresh transport test before create and invalidates it when connection fields change', async () => {
    await mount()
    await click('Add network egress')
    await input('egress_name', 'New endpoint')
    await input('host', 'proxy.example.test')
    await input('target', 'https://api.example.test/v1')
    expect(button('Save configuration').disabled).toBe(true)
    await click('Test connection')
    expect(document.body.textContent).toContain('API request did not succeed')
    expect(document.body.textContent).toContain('HTTP 401')
    expect(button('Save configuration').disabled).toBe(false)
    await input('port', '1081')
    expect(button('Save configuration').disabled).toBe(true)
    await click('Test connection')
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    const saved = writes().find((request) => request.url === '/admin/egresses')!
    expect(JSON.parse(saved.data)).toMatchObject({
      name: 'New endpoint',
      port: 1081,
      auth: { action: 'remove' },
    })
    expect(saved.headers.get('X-CSRF-Token')).toBe('csrf')
  })
  it('renames with auth keep without reading credentials or forcing a transport test', async () => {
    await editor()
    expect(document.querySelector('[name="proxy_password"]')).toBeNull()
    await input('egress_name', 'Renamed')
    await submit()
    expect(writes()).toHaveLength(1)
    expect(writes()[0].method).toBe('patch')
    expect(JSON.parse(writes()[0].data)).toMatchObject({
      etag: 'rev_1',
      auth: { action: 'keep' },
      name: 'Renamed',
    })
    expect(JSON.stringify(cache.getMutationCache().getAll())).not.toContain('password')
  })
  it.each(['replace', 'remove'])(
    'sends explicit authentication %s only after a new test',
    async (action) => {
      await editor()
      await select('auth_action', action)
      if (action === 'replace') {
        await input('proxy_username', 'local_user')
        await input('proxy_password', 'local_secret')
      }
      await input('target', 'https://api.example.test/v1')
      expect(button('Save configuration').disabled).toBe(true)
      await click('Test connection')
      await submit()
      const payload = JSON.parse(writes().at(-1)!.data)
      expect(payload.auth).toEqual(
        action === 'replace'
          ? { action, username: 'local_user', password: 'local_secret' }
          : { action },
      )
      expect(cache.getMutationCache().getAll()).toHaveLength(0)
      expect(JSON.stringify(localStorage) + JSON.stringify(sessionStorage)).not.toContain(
        'local_secret',
      )
    },
  )
  it('preserves draft on conflict, explicitly reloads revision and requires a fresh transport test', async () => {
    await editor()
    await input('host', 'new.example.test')
    await input('target', 'https://api.example.test/v1')
    await click('Test connection')
    failure = 409
    await submit()
    expect(document.body.textContent).toContain('draft is preserved')
    expect(button('Save configuration').disabled).toBe(true)
    row = { ...row, etag: 'rev_2', host: 'concurrent.example.test' }
    failure = 0
    await click('Reload and review')
    expect(document.querySelector<HTMLInputElement>('[name="host"]')!.value).toBe(
      'new.example.test',
    )
    expect(document.body.textContent).toContain('concurrent.example.test')
    expect(button('Save configuration').disabled).toBe(true)
    await click('Test connection')
    await submit()
    expect(JSON.parse(writes().at(-1)!.data).etag).toBe('rev_2')
  })
  it('guards duplicate test dispatch and does not retain credential-bearing Axios errors', async () => {
    await editor()
    await select('auth_action', 'replace')
    await input('proxy_username', 'u')
    await input('proxy_password', 'sensitive')
    await input('target', 'https://api.example.test/v1')
    hold = true
    await click('Test connection')
    expect(document.body.textContent).not.toContain('Saving…')
    await click('Testing…')
    expect(writes()).toHaveLength(1)
    await act(async () => release?.())
    hold = false
    failure = 422
    await submit()
    expect(document.body.textContent).not.toContain('raw unsafe message')
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('renders real saved diagnostics including skipped/not-applicable without inventing stage success', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('Outbound A'))
    await click('Actions for Outbound A')
    await click('Test connection')
    await input('diagnostic_target', 'https://api.example.test/v1')
    await submit()
    expect(document.body.textContent).toContain('Proxy authentication')
    expect(document.body.textContent).toContain('Not applicable')
    expect(document.body.textContent).toContain('HTTP 401')
    expect(JSON.parse(writes()[0].data)).toEqual({
      etag: 'rev_1',
      target_base_url: 'https://api.example.test/v1',
    })
  })
  it('stores a null platform default for direct without changing explicit connection semantics', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('Platform default egress: Outbound A'))
    await click('Platform default egress: Outbound A')
    await select('default_egress', '')
    await submit()
    expect(JSON.parse(writes()[0].data)).toEqual({ etag: 'default_rev', egress_id: null })
    expect(egressSelection('default')).toEqual({ egress_mode: 'default', egress_id: null })
    expect(egressSelection('direct')).toEqual({ egress_mode: 'direct', egress_id: null })
  })
  it.each(['default', 'direct', 'proxy:egr_a'])(
    'submits distinct Connection %s payload with revision and CSRF',
    async (value) => {
      await mount(<ConnectionEgressControl connection={connection()} />)
      await click('Use platform default')
      await until(() =>
        expect(
          document.querySelector<HTMLSelectElement>('[name="egress_selection"]')!.disabled,
        ).toBe(false),
      )
      await select('egress_selection', value)
      await submit()
      expect(writes()[0].method).toBe('patch')
      expect(JSON.parse(writes()[0].data)).toEqual({
        etag: 'rev_1',
        mode: value.startsWith('proxy:') ? 'proxy' : value,
        egress_id: value.startsWith('proxy:') ? 'egr_a' : null,
      })
      expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf')
    },
  )
  it('switches localized form labels without discarding secret input or reviewed transport result', async () => {
    await editor()
    await select('auth_action', 'replace')
    await input('proxy_username', 'u')
    await input('proxy_password', 'transient')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.body.textContent).toContain('替换鉴权')
    expect(document.querySelector<HTMLInputElement>('[name="proxy_password"]')!.value).toBe(
      'transient',
    )
    expect(localStorage.getItem('transient')).toBeNull()
  })
  it('sanitizes API failure objects before they can reach page state', async () => {
    failure = 503
    let caught: unknown
    try {
      await saveEgress(
        'egr_a',
        {
          name: 'x',
          kind: 'socks5',
          host: 'host',
          port: 1,
          enabled: true,
          auth: { action: 'replace', username: 'u', password: 'never_cache' },
        },
        'csrf',
      )
    } catch (error) {
      caught = error
    }
    expect(caught).toBeInstanceOf(EgressError)
    expect(caught).toMatchObject({ status: 503 })
    expect(JSON.stringify(caught)).not.toContain('never_cache')
    expect(caught).not.toHaveProperty('config')
  })
})

it('does not treat a stale diagnostic as validation and clears credentials when the dialog closes', async () => {
  await mount()
  await click('Add network egress')
  await input('egress_name', 'Temporary')
  await input('host', 'proxy.example.test')
  await input('target', 'https://api.example.test/v1')
  await select('auth_action', 'replace')
  await input('proxy_username', 'u')
  await input('proxy_password', 'secret_only_here')
  diagnosticResult = { ...probe, stale: true }
  await click('Test connection')
  expect(button('Save configuration').disabled).toBe(true)
  expect(document.body.textContent).toContain('result was not saved')
  await click('Cancel')
  await click('Add network egress')
  await select('auth_action', 'replace')
  expect(document.querySelector<HTMLInputElement>('[name="proxy_password"]')!.value).toBe('')
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it('separates write permission from test permission', async () => {
  permissions = ['egress.read', 'egress.write']
  await editor()
  await input('egress_name', 'Rename permitted')
  expect(button('Save configuration').disabled).toBe(false)
  await input('host', 'changed.example.test')
  await input('target', 'https://api.example.test/v1')
  expect(button('Test connection').disabled).toBe(true)
  expect(button('Save configuration').disabled).toBe(true)
  expect(document.body.textContent).toContain('require permission to test')
})
it('keeps a Connection draft through revision review instead of silently overwriting', async () => {
  await mount(<ConnectionEgressControl connection={connection()} />)
  await click('Use platform default')
  await until(() =>
    expect(document.querySelector<HTMLSelectElement>('[name="egress_selection"]')!.disabled).toBe(
      false,
    ),
  )
  await select('egress_selection', 'direct')
  failure = 409
  await submit()
  expect(button('Save configuration').disabled).toBe(true)
  row.etag = 'rev_2'
  failure = 0
  await click('Reload and review')
  expect(document.querySelector<HTMLSelectElement>('[name="egress_selection"]')!.value).toBe(
    'direct',
  )
  await submit()
  expect(JSON.parse(writes().at(-1)!.data)).toEqual({
    mode: 'direct',
    egress_id: null,
    etag: 'rev_2',
  })
})

it('reconciles an uncertain create by closing secret entry and reloading actual rows without a success claim', async () => {
  await mount()
  await click('Add network egress')
  await input('egress_name', 'Uncertain')
  await input('host', 'proxy.example.test')
  await input('target', 'https://api.example.test/v1')
  await click('Test connection')
  failure = 503
  await submit()
  expect(document.body.textContent).toContain('outcome is uncertain')
  expect(button('Save configuration').disabled).toBe(true)
  const reads = requests.filter(
    (request) => request.url === '/admin/egresses' && request.method === 'get',
  ).length
  failure = 0
  await click('Reload and review')
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  await until(() =>
    expect(
      requests.filter((request) => request.url === '/admin/egresses' && request.method === 'get')
        .length,
    ).toBeGreaterThan(reads),
  )
  expect(host.textContent).not.toContain('Configuration saved')
})
