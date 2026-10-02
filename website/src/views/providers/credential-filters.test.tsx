import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { Credential, Provider } from '@/types/catalog'
import ProvidersPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[], permissions: string[], providers: Provider[]
let hold: Promise<void> | undefined
const credential = (
  id: string,
  name: string,
  verification_status: Credential['verification_status'],
  enabled: boolean,
): Credential => ({
  id,
  name,
  verification_status,
  enabled,
  priority: 5,
  verified_at: verification_status === 'verified' ? '2026-09-30T08:00:00Z' : null,
})
beforeEach(async () => {
  await i18n.changeLanguage('en')
  permissions = ['providers.read', 'providers.write']
  requests = []
  hold = undefined
  providers = [
    {
      id: 'prv_first',
      name: 'First provider',
      connections: [
        {
          id: 'con_primary',
          name: 'Primary',
          base_url: 'https://primary.example.invalid',
          protocol: 'openai_chat',
          provider_models: [],
          credentials: [
            credential('crd_primary', 'Alpha.Primary', 'verified', true),
            credential('crd_pending', 'Alpha Pending', 'pending', false),
            credential('crd_failed', 'Alpha Failed', 'failed', false),
          ],
        },
        {
          id: 'con_secondary',
          name: 'Secondary',
          base_url: 'https://secondary.example.invalid',
          protocol: 'openai_responses',
          provider_models: [],
          credentials: [
            credential('crd_secondary', 'alpha secondary', 'verified', false),
            credential('crd_beta', 'Beta Active', 'verified', true),
            credential('crd_literal', 'literal [test]', 'pending', false),
          ],
        },
      ],
    },
    {
      id: 'prv_second',
      name: 'Second provider',
      connections: [
        {
          id: 'con_other',
          name: 'Other',
          base_url: 'https://other.example.invalid',
          protocol: 'anthropic_messages',
          provider_models: [],
          credentials: [credential('crd_other', 'Other credential', 'failed', false)],
        },
      ],
    },
  ]
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  router = createMemoryRouter(
    [{ path: '/admin/providers/:providerId', element: <ProvidersPage /> }],
    { initialEntries: ['/admin/providers/prv_first?tab=credentials'] },
  )
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
      response.data = {
        user: { id: 'usr_credentials', name: 'Credential manager', role: 'member' },
        csrf_token: 'credential-csrf',
      }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/admin/providers')
      response.data = { items: structuredClone(providers) }
    else if (config.url === '/admin/models') response.data = { items: [] }
    else if (config.url?.startsWith('/admin/credentials/')) {
      if (hold) await hold
      if (config.url.endsWith('/verify')) response.data = { verified: true, discovered_models: 2 }
      else {
        const id = config.url.split('/').at(-1)
        const row = providers
          .flatMap((provider) =>
            provider.connections.flatMap((connection) => connection.credentials),
          )
          .find((item) => item.id === id)!
        row.enabled = JSON.parse(config.data).enabled
        response.data = row
      }
    } else throw new Error(`Unexpected request: ${config.url}`)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  await i18n.changeLanguage('en')
})
async function until(assert: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(host.querySelector('table[aria-label="Provider credentials"]')).not.toBeNull(),
  )
}
const table = () => host.querySelector<HTMLTableElement>('table')!
const names = () =>
  [...table().querySelectorAll('tbody tr')]
    .filter((row) => row.children.length > 1)
    .map((row) => row.children[0].textContent)
async function search(value: string, label = 'Search credential names') {
  const control = host.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(control, value)
    control.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function filter(label: string, value: string) {
  const control = host.querySelector<HTMLSelectElement>(`select[aria-label="${label}"]`)!
  await act(async () => {
    control.value = value
    control.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
const writes = () => requests.filter((request) => request.method !== 'get')

describe('provider credential filters', () => {
  it('intersects literal trimmed name, exact connection, verification and enabled filters', async () => {
    await mount()
    expect(names()).toHaveLength(6)
    await search('  ALPHA  ')
    expect(names()).toEqual(['Alpha.Primary', 'Alpha Pending', 'Alpha Failed', 'alpha secondary'])
    await filter('Credential connection filter', 'con_secondary')
    await filter('Credential verification filter', 'verified')
    await filter('Credential enabled filter', 'disabled')
    expect(names()).toEqual(['alpha secondary'])
    expect(host.textContent).toContain('1 of 6 credentials')
    await filter('Credential enabled filter', 'enabled')
    expect(names()).toEqual([])
    expect(host.textContent).toContain('No credentials match these filters.')
    expect(host.textContent).toContain('0 of 6 credentials')
    await filter('Credential connection filter', 'all')
    expect(names()).toEqual(['Alpha.Primary'])
    expect(writes()).toHaveLength(0)
  })
  it('treats punctuation literally and exposes pending and failed states independently', async () => {
    await mount()
    await search('[test]')
    expect(names()).toEqual(['literal [test]'])
    await search('')
    await filter('Credential verification filter', 'pending')
    expect(names()).toEqual(['Alpha Pending', 'literal [test]'])
    await filter('Credential verification filter', 'failed')
    expect(names()).toEqual(['Alpha Failed'])
    await filter('Credential verification filter', 'verified')
    await filter('Credential enabled filter', 'enabled')
    expect(names()).toEqual(['Alpha.Primary', 'Beta Active'])
  })
  it('renders real nullable verification timestamps using the current selected locale', async () => {
    await mount()
    const date = '2026-09-30T08:00:00Z'
    expect(table().querySelector('time')?.getAttribute('datetime')).toBe(date)
    expect(table().querySelector('time')?.textContent).toBe(new Date(date).toLocaleString('en-US'))
    expect(table().textContent).toContain('Not recorded')
    expect(table().textContent).not.toContain('API Key')
    await act(async () => i18n.changeLanguage('zh'))
    expect(table().querySelector('time')?.textContent).toBe(new Date(date).toLocaleString('zh-CN'))
    expect(table().textContent).toContain('未记录')
    expect(table().textContent).toContain('验证时间')
  })
  it('preserves filter drafts through live localization and translates the empty state', async () => {
    await mount()
    await search('  ALPHA  ')
    await filter('Credential connection filter', 'con_secondary')
    await filter('Credential verification filter', 'failed')
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.querySelector('[role="group"][aria-label="凭证筛选"]')).not.toBeNull()
    expect(host.textContent).toContain('没有符合筛选条件的凭证。')
    expect(host.querySelector<HTMLInputElement>('[aria-label="搜索凭证名称"]')?.value).toBe(
      '  ALPHA  ',
    )
    expect(host.querySelector<HTMLSelectElement>('[aria-label="凭证所属接入筛选"]')?.value).toBe(
      'con_secondary',
    )
    await filter('凭证验证状态筛选', 'verified')
    expect(names()).toEqual(['alpha secondary'])
    expect(host.textContent).toContain('6 个凭证中显示 1 个')
  })
  it('resets all filters when navigating to another provider', async () => {
    await mount()
    await search('Alpha')
    await filter('Credential connection filter', 'con_secondary')
    await filter('Credential verification filter', 'verified')
    await filter('Credential enabled filter', 'enabled')
    await act(async () => router.navigate('/admin/providers/prv_second?tab=credentials'))
    await until(() => expect(names()).toEqual(['Other credential']))
    expect(
      host.querySelector<HTMLInputElement>('[aria-label="Search credential names"]')?.value,
    ).toBe('')
    expect(host.querySelector('[aria-label="Credential connection filter"]')).toBeNull()
    expect(
      host.querySelector<HTMLSelectElement>('[aria-label="Credential verification filter"]')?.value,
    ).toBe('all')
    expect(
      host.querySelector<HTMLSelectElement>('[aria-label="Credential enabled filter"]')?.value,
    ).toBe('all')
    expect(writes()).toHaveLength(0)
  })
  it('keeps verification and enable mutations attached to the exact filtered credential with CSRF', async () => {
    await mount()
    await search('alpha secondary')
    await act(async () => table().querySelector<HTMLButtonElement>('button')!.click())
    const action = (label: string) =>
      [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
        (item) => item.textContent === label,
      )!
    await until(() => expect(action('Verify')).toBeDefined())
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await act(async () => action('Verify').click())
    await until(() => expect(writes()).toHaveLength(1))
    expect(writes()[0].url).toBe('/admin/credentials/crd_secondary/verify')
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('credential-csrf')
    await act(async () => table().querySelector<HTMLButtonElement>('button')!.click())
    await until(() => expect(action('Enable')).toBeDefined())
    expect(action('Enable').getAttribute('aria-disabled')).toBe('true')
    await act(async () => release())
    await until(() => expect(action('Enable').getAttribute('aria-disabled')).not.toBe('true'))
    expect(writes()).toHaveLength(1)
    hold = undefined
    await act(async () => action('Enable').click())
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].url).toBe('/admin/credentials/crd_secondary')
    expect(writes()[1].method).toBe('patch')
    expect(JSON.parse(writes()[1].data)).toEqual({ enabled: true })
    expect(writes()[1].headers.get('X-CSRF-Token')).toBe('credential-csrf')
  })
  it('supports read-only filtering while keeping all credential writes disabled', async () => {
    permissions = ['providers.read']
    await mount()
    await search('alpha')
    await filter('Credential verification filter', 'verified')
    expect(names()).toEqual(['Alpha.Primary', 'alpha secondary'])
    expect(table().querySelector<HTMLButtonElement>('button')?.getAttribute('aria-label')).toBe(
      'Actions for Alpha.Primary',
    )
    await act(async () => table().querySelector<HTMLButtonElement>('button')!.click())
    await until(() => expect(document.querySelectorAll('[role="menuitem"]')).toHaveLength(5))
    expect(
      [...document.querySelectorAll('[role="menuitem"]')].every(
        (item) => item.getAttribute('aria-disabled') === 'true',
      ),
    ).toBe(true)
    await act(async () => {
      document.querySelectorAll<HTMLElement>('[role="menuitem"]').forEach((item) => item.click())
    })
    expect(
      [...host.querySelectorAll('button')]
        .filter((button) => button.textContent?.startsWith('Add credential'))
        .every((button) => button.disabled),
    ).toBe(true)
    expect(writes()).toHaveLength(0)
  })
})
