import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import type { Provider } from '@/types/catalog'
import type { CredentialAttemptStatisticsItem } from '@/types/credential-attempt-statistics'
import ProvidersPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let mounted: boolean, actor: string, permissions: string[], providers: Provider[]
let requests: InternalAxiosRequestConfig[], items: Map<string, CredentialAttemptStatisticsItem>
let hold: Promise<void> | undefined, release: (() => void) | undefined, fail: number
const observed = '2026-10-09T01:02:03Z'
function item(id: string): CredentialAttemptStatisticsItem {
  return {
    credential_id: id,
    connection_id: 'con_exact',
    inspected_attempts: 0,
    has_more: false,
    last_attempt: { state: 'no_records', completed_at: null },
    failure_streak: { state: 'no_records', count: null, lower_bound: 0 },
    recent_error: { state: 'no_records', code: null, completed_at: null },
  }
}
function rows(count: number) {
  return Array.from({ length: count }, (_, index) => ({
    id: `crd_${index}`,
    name: `Credential ${index}`,
    priority: 5,
    enabled: false,
    verification_status: 'pending' as const,
    verified_at: null,
  }))
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_reader'
  permissions = ['providers.read']
  requests = []
  items = new Map()
  hold = undefined
  release = undefined
  fail = 0
  providers = [
    {
      id: 'prv_one',
      name: 'Provider One',
      connections: [
        {
          id: 'con_exact',
          name: 'Connection',
          enabled: false,
          protocol: 'openai_chat',
          base_url: 'https://example.invalid',
          provider_models: [],
          credentials: rows(5),
        },
      ],
    },
    { id: 'prv_two', name: 'Provider Two', connections: [] },
  ]
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  mounted = true
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  router = createMemoryRouter(
    [
      { path: '/admin/providers/:providerId', element: <ProvidersPage /> },
      { path: '/other', element: <p>Other</p> },
    ],
    { initialEntries: ['/admin/providers/prv_one?tab=credentials'] },
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
        user: { id: actor, name: 'Reader', role: 'member' },
        csrf_token: 'csrf-current',
      }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/admin/providers')
      response.data = { items: structuredClone(providers) }
    else if (config.url?.endsWith('/credential-attempt-statistics')) {
      const ids = (config.params as URLSearchParams).getAll('credential_id')
      response.headers.set('Cache-Control', 'private, no-store')
      response.data = {
        provider_id: config.url.split('/')[3],
        observed_at: observed,
        attempt_limit: 100,
        recorded_only: true,
        items: ids.map((id) => structuredClone(items.get(id) ?? item(id))),
      }
      if (hold) await hold // Deliberately ignores abort to exercise the consumer fence.
      if (fail) {
        response.status = fail
        throw new AxiosError('statistics unavailable', '', config, undefined, response)
      }
    } else throw new Error(`Unexpected request ${config.method} ${config.url}`)
    return response
  }
})
afterEach(async () => {
  release?.()
  if (mounted) await act(async () => root.unmount())
  router.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  await i18n.changeLanguage('en')
})
async function until(assert: () => void) {
  for (let index = 0; index < 100; index++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
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
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(host.querySelector('table[aria-label="Provider credentials"]')).not.toBeNull(),
  )
}
const reads = () =>
  requests.filter((request) => request.url?.endsWith('/credential-attempt-statistics'))
const row = (name: string) =>
  [...host.querySelectorAll('tbody tr')].find((item) => item.children[0]?.textContent === name)!
const cells = (name: string) => [...row(name).querySelectorAll('td')]
const button = (label: string) =>
  [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label,
  )!
function recordError(id = 'crd_0') {
  items.set(id, {
    ...item(id),
    inspected_attempts: 3,
    last_attempt: { state: 'recorded', completed_at: '2026-10-09T01:02:02.5Z' },
    failure_streak: { state: 'exact', count: 3, lower_bound: 3 },
    recent_error: {
      state: 'recorded',
      code: 'upstream_timeout',
      completed_at: '2026-10-09T01:02:02Z',
    },
  })
}
describe('Credential recorded inference statistics in the existing table', () => {
  it('loads exact visible IDs with independent read permission and leaves Verify/Test/status untouched', async () => {
    recordError()
    await mount()
    await until(() => expect(row('Credential 0').textContent).toContain('upstream_timeout'))
    expect(reads()).toHaveLength(1)
    expect(reads()[0].url).toBe('/admin/providers/prv_one/credential-attempt-statistics')
    expect((reads()[0].params as URLSearchParams).getAll('credential_id')).toEqual([
      'crd_0',
      'crd_1',
      'crd_2',
      'crd_3',
      'crd_4',
    ])
    expect(cells('Credential 0')[3].textContent).toBe('Pending verification')
    expect(cells('Credential 0')[4].textContent).toBe('Not recorded')
    expect(cells('Credential 0')[5].querySelector('time')?.dateTime).toBe('2026-10-09T01:02:02.5Z')
    expect(host.querySelector('th[title]')?.textContent).toBe('Last recorded inference attempt')
    expect(host.querySelector('th[title]')?.getAttribute('title')).toContain(
      'Unrecorded use is excluded',
    )
    expect(cells('Credential 0')[6].textContent).toBe('3')
    expect(cells('Credential 0')[8].textContent).toBe('Disabled')
    expect(requests.every((request) => request.method === 'get')).toBe(true)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    expect(host.textContent).toContain('Verify and Test connection are separate.')
  })
  it('distinguishes no records, exact zero, clipped count and unknown boundary while retaining an earlier recent error', async () => {
    items.set('crd_1', {
      ...item('crd_1'),
      inspected_attempts: 2,
      last_attempt: { state: 'recorded', completed_at: observed },
      failure_streak: { state: 'exact', count: 0, lower_bound: 0 },
      recent_error: { state: 'recorded', code: null, completed_at: '2026-10-09T01:02:02Z' },
    })
    items.set('crd_2', {
      ...item('crd_2'),
      inspected_attempts: 100,
      has_more: true,
      last_attempt: { state: 'recorded', completed_at: observed },
      failure_streak: { state: 'lower_bound', count: null, lower_bound: 100 },
      recent_error: {
        state: 'recorded',
        code: 'upstream_error',
        completed_at: '2026-10-09T01:02:02Z',
      },
    })
    items.set('crd_3', {
      ...item('crd_3'),
      inspected_attempts: 3,
      last_attempt: { state: 'unknown', completed_at: null },
      failure_streak: { state: 'unknown', count: null, lower_bound: 2 },
      recent_error: { state: 'unknown', code: null, completed_at: null },
    })
    items.set('crd_4', {
      ...item('crd_4'),
      inspected_attempts: 1,
      last_attempt: { state: 'recorded', completed_at: observed },
      failure_streak: { state: 'exact', count: 0, lower_bound: 0 },
      recent_error: { state: 'none', code: null, completed_at: null },
    })
    await mount()
    await until(() => expect(cells('Credential 2')[6].textContent).toBe('100+'))
    expect(cells('Credential 0')[6].textContent).toBe('No recorded attempts')
    expect(cells('Credential 1')[6].textContent).toBe('0')
    expect(cells('Credential 1')[7].textContent).toContain('Unknown error code')
    expect(cells('Credential 3')[6].textContent).toBe('Unknown (at least 2)')
    expect(cells('Credential 3')[7].textContent).toBe('Unknown')
    expect(cells('Credential 4')[7].textContent).toBe('No recorded error')
    expect(cells('Credential 0')[5].textContent).toBe('No recorded attempts')
    expect(cells('Credential 3')[5].textContent).toBe('Unknown')
    expect(cells('Credential 1')[5].querySelector('time')?.dateTime).toBe(observed)
    expect(cells('Credential 1')[7].querySelector('time')?.dateTime).toBe('2026-10-09T01:02:02Z')
  })
  it('limits each page to twenty exact IDs and resets pagination when a filter changes', async () => {
    providers[0].connections[0].credentials = rows(21)
    await mount()
    await until(() => expect(reads()).toHaveLength(1))
    expect((reads()[0].params as URLSearchParams).getAll('credential_id')).toEqual(
      rows(20).map((row) => row.id),
    )
    expect(host.querySelectorAll('tbody tr')).toHaveLength(20)
    await act(async () => button('Next').click())
    await until(() => expect(reads()).toHaveLength(2))
    expect((reads()[1].params as URLSearchParams).getAll('credential_id')).toEqual(['crd_20'])
    const search = host.querySelector<HTMLInputElement>('[aria-label="Search credential names"]')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        search,
        'Credential 1',
      )
      search.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await until(() => expect(reads()).toHaveLength(3))
    expect((reads()[2].params as URLSearchParams).getAll('credential_id')).toEqual([
      'crd_1',
      ...rows(20)
        .slice(10)
        .map((row) => row.id),
    ])
    expect(host.textContent).not.toContain('Page 2')
  })
  it('does not grant read access to a write-only actor or dispatch statistics for denied reads', async () => {
    permissions = ['providers.write']
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(host.textContent).toContain('permission'))
    expect(reads()).toHaveLength(0)
    expect(host.querySelector('table')).toBeNull()
  })
  it('localizes existing data and timestamps live without changing filters or recorded codes', async () => {
    recordError()
    await mount()
    await until(() => expect(row('Credential 0').textContent).toContain('upstream_timeout'))
    expect(cells('Credential 0')[5].querySelector('time')?.textContent).toBe(
      new Date('2026-10-09T01:02:02.5Z').toLocaleString('en-US'),
    )
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain('连续失败（已记录）')
    expect(host.textContent).toContain('最近错误（已记录）')
    expect(host.querySelector('th[title]')?.textContent).toBe('最近已记录的推理尝试')
    expect(host.querySelector('th[title]')?.getAttribute('title')).toContain('不包含尚未记录的使用')
    expect(cells('Credential 0')[5].querySelector('time')?.textContent).toBe(
      new Date('2026-10-09T01:02:02.5Z').toLocaleString('zh-CN'),
    )
    expect(row('Credential 0').textContent).toContain('upstream_timeout')
    expect(cells('Credential 0')[7].querySelector('time')?.textContent).toBe(
      new Date('2026-10-09T01:02:02Z').toLocaleString('zh-CN'),
    )
    expect(reads()).toHaveLength(1)
  })
  it('hides old statistics during refresh and failure, then explicitly retries the read', async () => {
    recordError()
    await mount()
    await until(() => expect(row('Credential 0').textContent).toContain('upstream_timeout'))
    fail = 503
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['admin', 'credential-attempt-statistics'] })
    })
    await until(() =>
      expect(host.textContent).toContain('Recorded attempt statistics are unavailable.'),
    )
    expect(host.textContent).not.toContain('upstream_timeout')
    expect(cells('Credential 0')[6].textContent).toBe('Unknown')
    expect(cells('Credential 0')[5].textContent).toBe('Unknown')
    expect(cells('Credential 0')[5].querySelector('time')).toBeNull()
    fail = 0
    await act(async () => button('Refresh recorded attempts').click())
    await until(() => expect(row('Credential 0').textContent).toContain('upstream_timeout'))
    expect(requests.every((request) => request.method === 'get')).toBe(true)
  })
  it('renews the same reader with fresh facts while rejecting the old held generation', async () => {
    recordError()
    hold = new Promise<void>((resolve) => {
      release = resolve
    })
    await mount()
    await until(() => expect(reads()).toHaveLength(1))
    const initial = reads()[0]
    const oldKeys = cache
      .getQueryCache()
      .getAll()
      .filter((query) => query.queryKey[1] === 'credential-attempt-statistics')
      .map((query) => query.queryHash)
    hold = undefined
    items.set('crd_0', {
      ...item('crd_0'),
      inspected_attempts: 2,
      last_attempt: { state: 'recorded', completed_at: observed },
      failure_streak: { state: 'exact', count: 2, lower_bound: 2 },
      recent_error: {
        state: 'recorded',
        code: 'upstream_error',
        completed_at: '2026-10-09T01:02:02Z',
      },
    })
    await act(async () => {
      cache.setQueryData(sessionKey, {
        user: { id: actor, name: 'Reader', role: 'member' },
        csrf_token: 'renewed',
      })
    })
    await until(() => expect(row('Credential 0').textContent).toContain('upstream_error'))
    expect(reads()).toHaveLength(2)
    expect(initial.signal?.aborted).toBe(true)
    await act(async () => release?.())
    await until(() =>
      expect(
        cache
          .getQueryCache()
          .getAll()
          .some((query) => oldKeys.includes(query.queryHash)),
      ).toBe(false),
    )
    expect(cells('Credential 0')[6].textContent).toBe('2')
    expect(cells('Credential 0')[5].querySelector('time')?.dateTime).toBe(observed)
    expect(host.textContent).not.toContain('upstream_timeout')
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((query) => query.state.data),
      ),
    ).not.toContain('upstream_timeout')
  })
  it.each([
    'session',
    'actor',
    'provider',
    'permission',
    'credential',
    'list_error',
    'expiry',
    'unmount',
  ])(
    'discards an ignored-abort old response after %s loss without resurrecting its private cache',
    async (boundary) => {
      recordError()
      hold = new Promise<void>((resolve) => {
        release = resolve
      })
      await mount()
      await until(() => expect(reads()).toHaveLength(1))
      const initial = reads()[0]
      const oldKeys = cache
        .getQueryCache()
        .getAll()
        .filter((query) => query.queryKey[1] === 'credential-attempt-statistics')
        .map((query) => query.queryHash)
      await act(async () => {
        if (boundary === 'unmount') {
          root.unmount()
          mounted = false
        } else if (boundary === 'provider')
          await router.navigate('/admin/providers/prv_two?tab=credentials')
        else if (boundary === 'expiry') window.dispatchEvent(new Event('routex:session-expired'))
        else if (boundary === 'list_error')
          cache
            .getQueryCache()
            .find({ queryKey: ['admin', 'providers'], exact: true })!
            .setState({ status: 'error', error: new Error('controlled list error') })
        else if (boundary === 'credential') {
          const next = structuredClone(providers)
          next[0].connections[0].credentials = []
          cache.setQueryData(['admin', 'providers'], next)
        } else if (boundary === 'permission') cache.setQueryData(['permissions', actor], [])
        else {
          permissions = []
          if (boundary === 'actor') actor = 'usr_other'
          cache.setQueryData(sessionKey, {
            user: { id: actor, role: 'member' },
            csrf_token: 'renewed',
          })
          cache.setQueryData(['permissions', actor], [])
        }
      })
      await until(() => expect(initial.signal?.aborted).toBe(true))
      await act(async () => release?.())
      await until(() =>
        expect(
          cache
            .getQueryCache()
            .getAll()
            .some((query) => oldKeys.includes(query.queryHash)),
        ).toBe(false),
      )
      expect(host.textContent).not.toContain('upstream_timeout')
      expect(
        JSON.stringify(
          cache
            .getQueryCache()
            .getAll()
            .map((query) => query.state.data),
        ),
      ).not.toContain('2026-10-09T01:02:02.5Z')
      expect(
        JSON.stringify(
          cache
            .getQueryCache()
            .getAll()
            .map((query) => query.state.data),
        ),
      ).not.toContain('upstream_timeout')
    },
  )
})
