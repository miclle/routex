import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { Credential, Provider } from '@/types/catalog'
import { sessionKey } from '@/hooks/use-auth'
import { MenuItem } from '@/components/ui/menu'
import ProvidersPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[], permissions: string[], providers: Provider[]
let hold: Promise<void> | undefined
let diagnosticHold: Promise<void> | undefined, releaseDiagnostic: (() => void) | undefined
let diagnosticSignal: AbortSignal | undefined
const diagnosticToken = `${'c'.repeat(64)}.${'d'.repeat(64)}`
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
  diagnosticHold = undefined
  releaseDiagnostic = undefined
  diagnosticSignal = undefined
  providers = [
    {
      id: 'prv_first',
      name: 'First provider',
      connections: [
        {
          id: 'con_primary',
          enabled: true,
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
          enabled: true,
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
          enabled: true,
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
    else if (config.url?.endsWith('/credential-attempt-statistics')) {
      const provider = providers.find((row) => row.id === config.url?.split('/')[3])!
      response.headers.set('Cache-Control', 'private, no-store')
      response.data = {
        provider_id: provider.id,
        observed_at: '2026-10-09T01:02:03Z',
        attempt_limit: 100,
        recorded_only: true,
        items: (config.params as URLSearchParams).getAll('credential_id').map((id) => ({
          credential_id: id,
          connection_id: provider.connections.find((row) =>
            row.credentials.some((credential) => credential.id === id),
          )!.id,
          inspected_attempts: 0,
          has_more: false,
          last_attempt: { state: 'no_records', completed_at: null },
          failure_streak: { state: 'no_records', count: null, lower_bound: 0 },
          recent_error: { state: 'no_records', code: null, completed_at: null },
        })),
      }
    } else if (config.url?.startsWith('/admin/connections/') && config.url.endsWith('/metadata')) {
      const connectionId = config.url.split('/')[3]
      const connection = providers
        .flatMap((row) => row.connections)
        .find((row) => row.id === connectionId)!
      response.headers.set('Cache-Control', 'private, no-store')
      response.headers.set('ETag', `"${diagnosticToken}"`)
      response.data = {
        id: connectionId,
        provider_id: 'prv_first',
        name: connection.name,
        adapter: 'native',
        api_version: null,
        protocol: connection.protocol,
        base_url: connection.base_url,
        egress_mode: 'direct',
        egress_id: null,
        etag: diagnosticToken,
        can_edit: true,
        transport_generation: '0',
        can_edit_transport: false,
        transport_locked: false,
      }
    } else if (config.url?.startsWith('/admin/connections/') && config.url.endsWith('/test')) {
      diagnosticSignal = config.signal as AbortSignal
      if (diagnosticHold) await diagnosticHold
      response.headers.set('Cache-Control', 'private, no-store')
      response.data = {
        connection_id: config.url.split('/')[3],
        credential_id: JSON.parse(config.data).credential_id,
        outcome: 'passed',
        scope: 'model_discovery',
        discovered_model_count: 0,
        checked_at: '2026-10-09T01:02:03Z',
      }
    } else if (config.url?.startsWith('/admin/credentials/')) {
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
  releaseDiagnostic?.()
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
    await until(() => expect(document.querySelectorAll('[role="menuitem"]')).toHaveLength(6))
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
it('adds Azure coverage review to the existing credential row menu with independent read authority; native rows remain unchanged', async () => {
  permissions = ['providers.read']
  providers[0].connections[0].adapter = 'azure_openai_classic'
  providers[0].connections[0].api_version = '2024-10-21'
  await mount()
  await act(async () => table().querySelector<HTMLButtonElement>('button')!.click())
  await until(() =>
    expect(
      [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].some(
        (item) => item.textContent === 'Review deployment coverage',
      ),
    ).toBe(true),
  )
  const item = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
    (item) => item.textContent === 'Review deployment coverage',
  )!
  expect(item.getAttribute('aria-disabled')).not.toBe('true')
  expect(
    [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')]
      .filter((n) => n !== item)
      .every((n) => n.getAttribute('aria-disabled') === 'true'),
  ).toBe(true)
  expect(writes()).toHaveLength(0)
})

async function openCredentialTest(name: string) {
  const trigger = host.querySelector<HTMLButtonElement>(`button[aria-label="Actions for ${name}"]`)!
  expect(trigger).toBeTruthy()
  await act(async () => trigger.click())
  await until(() => expect(document.querySelector('[role="menuitem"]')).not.toBeNull())
  const action = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
    (row) => row.textContent === 'Test connection',
  )!
  expect(action).toBeTruthy()
  return { trigger, action }
}
const diagnosticPosts = () => requests.filter((request) => request.url?.endsWith('/test'))
it('opens a Credential-row test locked to its exact Connection and pending disabled record', async () => {
  await mount()
  const { action, trigger } = await openCredentialTest('Alpha Pending')
  await act(async () => action.click())
  await until(() =>
    expect(document.querySelector('[aria-label="Selected credential"]')).not.toBeNull(),
  )
  expect(document.querySelector('[aria-label="Selected credential"]')?.textContent).toContain(
    'crd_pending',
  )
  expect(document.querySelector('[aria-label="Credential to test"]')).toBeNull()
  expect(diagnosticPosts()).toHaveLength(0)
  const run = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (row) => row.textContent === 'Run new test',
  )!
  await act(async () => {
    run.click()
    run.click()
  })
  await until(() => expect(document.body.textContent).toContain('This test passed.'))
  expect(diagnosticPosts()).toHaveLength(1)
  expect(diagnosticPosts()[0].url).toBe('/admin/connections/con_primary/test')
  expect(JSON.parse(diagnosticPosts()[0].data)).toEqual({ credential_id: 'crd_pending' })
  expect(diagnosticPosts()[0].headers.get('If-Match')).toBe(`"${diagnosticToken}"`)
  expect(diagnosticPosts()[0].headers.get('X-CSRF-Token')).toBe('credential-csrf')
  expect(writes()).toHaveLength(1)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.body.textContent).toContain('本次测试通过。')
  expect(document.querySelector('[aria-label="已选择的凭证"]')?.textContent).toContain(
    'crd_pending',
  )
  const close = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (row) => row.textContent === '关闭',
  )!
  await act(async () => close.click())
  await until(() => expect(document.activeElement).toBe(trigger))
})
it('keeps identical names on distinct Connections bound to the clicked Credential row', async () => {
  providers[0].connections[1].credentials[0].name = 'Alpha Pending'
  await mount()
  await filter('Credential connection filter', 'con_secondary')
  const { action } = await openCredentialTest('Alpha Pending')
  await act(async () => action.click())
  await until(() =>
    expect(document.querySelector('[aria-label="Selected credential"]')).not.toBeNull(),
  )
  const run = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (row) => row.textContent === 'Run new test',
  )!
  await act(async () => run.click())
  await until(() => expect(document.body.textContent).toContain('This test passed.'))
  expect(diagnosticPosts()).toHaveLength(1)
  expect(diagnosticPosts()[0].url).toBe('/admin/connections/con_secondary/test')
  expect(JSON.parse(diagnosticPosts()[0].data)).toEqual({ credential_id: 'crd_secondary' })
})
it.each(['providers.read', 'providers.write'])(
  'independently denies the Credential diagnostic without %s',
  async (denied) => {
    permissions = permissions.filter((permission) => permission !== denied)
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    if (denied === 'providers.read') {
      await until(() => expect(host.textContent).toContain('permission'))
      expect(document.querySelector('[aria-label="Selected credential"]')).toBeNull()
    } else {
      await until(() => expect(host.querySelector('table')).not.toBeNull())
      const { action } = await openCredentialTest('Alpha Pending')
      expect(action.getAttribute('aria-disabled')).toBe('true')
      await act(async () => action.click())
      expect(document.querySelector('[aria-label="Selected credential"]')).toBeNull()
    }
    expect(diagnosticPosts()).toHaveLength(0)
  },
)
it('aborts a held Credential-row result on Session renewal without recreating private test facts', async () => {
  diagnosticHold = new Promise((resolve) => {
    releaseDiagnostic = resolve
  })
  await mount()
  const { action } = await openCredentialTest('Alpha Pending')
  await act(async () => action.click())
  await until(() =>
    expect(document.querySelector('[aria-label="Selected credential"]')).not.toBeNull(),
  )
  const run = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (row) => row.textContent === 'Run new test',
  )!
  await act(async () => run.click())
  await until(() => expect(diagnosticSignal).toBeTruthy())
  await act(async () =>
    cache.setQueryData(sessionKey, {
      user: { id: 'usr_credentials', role: 'member' },
      csrf_token: 'renewed',
    }),
  )
  expect(diagnosticSignal?.aborted).toBe(true)
  await act(async () => releaseDiagnostic?.())
  expect(document.querySelector('[aria-label="Selected credential"]')).toBeNull()
  expect(document.body.textContent).not.toContain('This test passed.')
  expect(diagnosticPosts()).toHaveLength(1)
  expect(
    JSON.stringify(
      cache
        .getQueryCache()
        .getAll()
        .map((query) => query.state.data),
    ),
  ).not.toContain('checked_at')
})

function captureCredentialTestAction(action: HTMLElement) {
  interface Fiber {
    type: unknown
    return: Fiber | null
    memoizedProps: { onClick?: () => void }
  }
  const key = Object.keys(action).find((value) => value.startsWith('__reactFiber$'))!
  let fiber = (action as unknown as Record<string, Fiber>)[key]
  while (fiber && fiber.type !== MenuItem) fiber = fiber.return!
  expect(fiber?.memoizedProps.onClick).toBeTypeOf('function')
  return fiber.memoizedProps.onClick!
}
it.each(['session', 'permission', 'credential', 'expiry'])(
  'denies a captured Credential-row action in the same turn as %s loss',
  async (boundary) => {
    await mount()
    const { action } = await openCredentialTest('Alpha Pending')
    const invoke = captureCredentialTestAction(action)
    await act(async () => {
      if (boundary === 'expiry') window.dispatchEvent(new Event('routex:session-expired'))
      else if (boundary === 'permission')
        cache.setQueryData(['permissions', 'usr_credentials'], ['providers.read'])
      else if (boundary === 'credential') {
        const next = structuredClone(providers)
        next[0].connections[0].credentials = next[0].connections[0].credentials.filter(
          (row) => row.id !== 'crd_pending',
        )
        cache.setQueryData(['admin', 'providers'], next)
      } else
        cache.setQueryData(sessionKey, {
          user: { id: 'usr_credentials', role: 'member' },
          csrf_token: 'renewed',
        })
      invoke()
    })
    expect(document.querySelector('[aria-label="Selected credential"]')).toBeNull()
    expect(diagnosticPosts()).toHaveLength(0)
    expect(requests.some((request) => request.url?.endsWith('/metadata'))).toBe(false)
  },
)
