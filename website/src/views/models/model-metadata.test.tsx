import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { Model } from '@/types/catalog'
import i18n from '@/i18n'
import AdminModelsPage from './admin'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let host: HTMLDivElement,
  root: Root,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let identity: Session,
  permissions: string[],
  models: Model[],
  requests: InternalAxiosRequestConfig[],
  monthlyStatus: number
let holds: Record<string, (() => Promise<void>) | undefined>, counts: Record<string, string>
let ignoreCancellation: boolean, signals: AbortSignal[]
function row(id: string, recorded = true): Model {
  return {
    id,
    name: 'Private ' + id,
    status: 'active',
    names: [{ name: 'Private ' + id, is_current: true, expires_at: null }],
    bindings: [
      {
        id: 'bnd_' + id,
        provider_model_id: 'pmd_' + id,
        provider_id: 'prv_one',
        connection_id: 'con_one',
        upstream_name: 'native-one',
        protocol: 'openai_chat',
        weight: 100,
        ready: true,
      },
    ],
    granted_user_ids: ['usr_one'],
    ...(recorded
      ? { created_at: '2026-01-02T03:04:05Z', config_updated_at: '2026-10-02T03:04:05.123456Z' }
      : {}),
  }
}
function report(ids: string[]) {
  return {
    items: ids.map((model_id) => ({ model_id, requests: counts[model_id] ?? '0' })),
    period_from: '2026-10-01T00:00:00Z',
    period_to: '2026-10-08T01:02:03.123456789Z',
    as_of: '2026-10-08T01:02:03.123456789Z',
    timezone: 'UTC',
    source: 'persisted_call_records',
    may_lag: true,
  }
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  identity = {
    user: { id: 'usr_actor', name: 'Reader', email: 'reader@example.invalid', role: 'member' },
    csrf_token: 'csrf-one',
  }
  permissions = ['models.read_all', 'calls.read_all', 'providers.read']
  models = [row('mdl_one'), row('mdl_two', false)]
  requests = []
  monthlyStatus = 200
  holds = {}
  ignoreCancellation = false
  signals = []
  counts = { mdl_one: '7', mdl_two: '0' }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } },
  })
  cache.setQueryData(sessionKey, identity)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown = {}
    let status = 200
    const url = config.url!
    if (url === '/auth/session') data = structuredClone(identity)
    else if (url === '/auth/permissions') data = { permissions: [...permissions] }
    else if (url === '/admin/models') data = { items: structuredClone(models) }
    else if (url.startsWith('/admin/models/') && config.method === 'get')
      data = structuredClone(models.find((item) => item.id === url.split('/')[3]))
    else if (url === '/admin/providers')
      data = { items: [{ id: 'prv_one', name: 'Provider One', connections: [] }] }
    else if (url.startsWith('/admin/model-monthly-requests?')) {
      status = monthlyStatus
      data = report(new URLSearchParams(url.split('?')[1]).getAll('model_id'))
    }
    if (url.startsWith('/admin/model-monthly-requests?') && config.signal) {
      signals.push(config.signal as AbortSignal)
      if (ignoreCancellation) config.signal = undefined
    }
    await (
      holds[url] ?? (url.startsWith('/admin/model-monthly-requests?') ? holds.monthly : undefined)
    )?.()
    const response = { config, status, statusText: '', headers: new AxiosHeaders(), data }
    if (status >= 400) throw new AxiosError('Denied', '', config, undefined, response)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
})
async function until(check: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      check()
      return
    } catch (e) {
      if (i === 99) throw e
    }
  }
}
async function mount(path = '/admin/models') {
  router = createMemoryRouter(
    [
      { path: '/admin/models', element: <AdminModelsPage /> },
      { path: '/admin/models/:modelId', element: <AdminModelsPage /> },
    ],
    { initialEntries: [path] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.querySelector('thead, dl')).not.toBeNull())
}
function monthly() {
  return requests.filter((r) => r.url?.startsWith('/admin/model-monthly-requests?'))
}
async function search(value: string) {
  const input = host.querySelector<HTMLInputElement>('input')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
describe('Administrative Model recorded metadata and independent monthly facts', () => {
  it('formats the declared UTC monthly interval accurately with live EN/ZH copy in a non-UTC browser', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('Persisted call records'))
    const from = new Date('2026-10-01T00:00:00Z')
    const asOf = new Date('2026-10-08T01:02:03.123456789Z')
    for (const [language, locale] of [
      ['en', 'en-US'],
      ['zh', 'zh-CN'],
    ]) {
      await act(async () => i18n.changeLanguage(language))
      const note = host.querySelector('[role="note"]')!.textContent!
      expect(note).toContain(from.toLocaleString(locale, { timeZone: 'UTC' }))
      expect(note).toContain(asOf.toLocaleString(locale, { timeZone: 'UTC' }))
      if (Intl.DateTimeFormat().resolvedOptions().timeZone !== 'UTC') {
        expect(note).not.toContain(from.toLocaleString(locale))
        expect(note).not.toContain(asOf.toLocaleString(locale))
      }
      expect(host.textContent).toContain(
        new Date('2026-10-02T03:04:05.123456Z').toLocaleString(locale),
      )
    }
  })

  it('retains the approved nine-column list and eight-cell detail with one exact batch and locale dates', async () => {
    await mount()
    await until(() => expect(host.querySelectorAll('thead th')).toHaveLength(9))
    expect([...host.querySelectorAll('thead th')].map((th) => th.textContent)).toEqual([
      'Public model name',
      'Protocol type',
      'Capability type',
      'Provider',
      'Status',
      'Granted members',
      'Monthly requests',
      'Updated',
      'Actions',
    ])
    await until(() => expect(monthly()).toHaveLength(1))
    expect(new URLSearchParams(monthly()[0].url!.split('?')[1]).getAll('model_id')).toEqual([
      'mdl_one',
      'mdl_two',
    ])
    expect(host.querySelector('tbody tr')!.textContent).toContain('7')
    expect(host.querySelectorAll('tbody tr')[1].textContent).toContain('0')
    expect(host.textContent).toContain('Persisted call records')
    expect(host.textContent).toContain('may lag')
    expect(host.textContent).toContain(
      new Date('2026-10-02T03:04:05.123456Z').toLocaleString('en-US'),
    )
    expect(host.querySelectorAll('tbody tr')[1].textContent).toContain('Unknown')
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain('能力类型')
    expect(host.textContent).toContain(
      new Date('2026-10-02T03:04:05.123456Z').toLocaleString('zh-CN'),
    )
    expect(monthly()).toHaveLength(1)
    await act(async () => router.navigate('/admin/models/mdl_one'))
    await until(() => expect(host.querySelectorAll('dl > div')).toHaveLength(8))
    expect([...host.querySelectorAll('dl dt')].map((dt) => dt.textContent)).toEqual([
      '模型 ID',
      '协议类型',
      '能力类型',
      '供应关系',
      '已授权成员',
      '本月请求',
      '创建时间',
      '更新时间',
    ])
    await until(() => expect(monthly()).toHaveLength(2))
    expect(new URLSearchParams(monthly()[1].url!.split('?')[1]).getAll('model_id')).toEqual([
      'mdl_one',
    ])
    expect(requests.every((r) => r.method === 'get')).toBe(true)
  })
  it('never reads statistics without the independent call permission', async () => {
    permissions = ['models.read_all', 'providers.read']
    await mount()
    expect(monthly()).toHaveLength(0)
    expect(host.querySelectorAll('thead th')).toHaveLength(9)
    expect(host.textContent).toContain('Private mdl_one')
    expect(host.textContent).not.toContain('Persisted call records')
    expect(host.querySelector('tbody tr')!.textContent).toContain('Unknown')
  })
  it.each([403, 422, 500])(
    'keeps catalogue and dates through whole-report failure %s and explicit retry',
    async (status) => {
      monthlyStatus = status
      await mount()
      await until(() =>
        expect(host.textContent).toContain('The complete monthly request query is unavailable'),
      )
      expect(host.textContent).toContain('Private mdl_one')
      expect(host.textContent).toContain(
        new Date('2026-10-02T03:04:05.123456Z').toLocaleString('en-US'),
      )
      expect(host.querySelector('tbody tr')!.textContent).toContain('Unavailable')
      monthlyStatus = 200
      counts.mdl_one = '0'
      await act(async () =>
        [...host.querySelectorAll('button')]
          .find((b) => b.textContent === 'Refresh monthly requests')!
          .click(),
      )
      await until(() => expect(host.textContent).toContain('Persisted call records'))
      expect(host.querySelector('tbody tr')!.querySelector('.tabular-nums')!.textContent).toBe('0')
    },
  )
  it('requests one visible subset, clears filtered facts and makes no empty batch', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('Persisted call records'))
    await search('mdl_two')
    await until(() => expect(monthly()).toHaveLength(2))
    expect(new URLSearchParams(monthly()[1].url!.split('?')[1]).getAll('model_id')).toEqual([
      'mdl_two',
    ])
    await search('no matching model')
    await until(() => expect(host.querySelectorAll('tbody tr')).toHaveLength(0))
    expect(monthly()).toHaveLength(2)
    expect(host.textContent).not.toContain('Persisted call records')
  })
  it.each(['session', 'permissions', 'catalogue'])(
    'aborts and rejects held private facts across %s renewal even if transport ignores cancellation',
    async (scope) => {
      await mount()
      await until(() => expect(host.textContent).toContain('Persisted call records'))
      ignoreCancellation = true
      counts.mdl_one = '777'
      let releaseOld!: () => void
      holds.monthly = () =>
        new Promise<void>((resolve) => {
          releaseOld = resolve
        })
      await act(async () => {
        void cache.refetchQueries({ queryKey: ['admin', 'model-monthly-requests'] })
      })
      await until(() => expect(releaseOld).toBeTypeOf('function'))
      const oldSignal = signals.at(-1)!
      const key =
        scope === 'session'
          ? sessionKey
          : scope === 'permissions'
            ? cache
                .getQueryCache()
                .findAll()
                .find((q) => q.queryKey.includes('permissions'))!.queryKey
            : cache
                .getQueryCache()
                .findAll()
                .find(
                  (q) =>
                    q.queryKey[0] === 'admin' &&
                    q.queryKey[1] === 'models' &&
                    q.queryKey[2] === 'list',
                )!.queryKey
      const url =
        scope === 'session'
          ? '/auth/session'
          : scope === 'permissions'
            ? '/auth/permissions'
            : '/admin/models'
      let releaseAuthority!: () => void
      holds[url] = () =>
        new Promise<void>((resolve) => {
          releaseAuthority = resolve
        })
      await act(async () => {
        void cache.refetchQueries({ queryKey: key, exact: true })
      })
      await until(() => expect(releaseAuthority).toBeTypeOf('function'))
      expect(host.textContent).not.toContain('Persisted call records')
      await until(() => expect(oldSignal.aborted).toBe(true))
      delete holds.monthly
      delete holds[url]
      counts.mdl_one = '5'
      await act(async () => releaseAuthority())
      await until(() => expect(host.textContent).toContain('Persisted call records'))
      await act(async () => releaseOld())
      expect(host.textContent).not.toContain('777')
      expect(host.querySelector('tbody tr')!.querySelector('.tabular-nums')!.textContent).toBe('5')
    },
  )
  it('hides counts on own-query invalidation and preserves the catalogue', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('Persisted call records'))
    await act(async () =>
      cache.invalidateQueries({
        queryKey: ['admin', 'model-monthly-requests'],
        refetchType: 'none',
      }),
    )
    expect(host.textContent).toContain('Private mdl_one')
    expect(host.textContent).not.toContain('Persisted call records')
    expect(host.querySelector('tbody tr')!.querySelector('.tabular-nums')!.textContent).toBe(
      'Unknown',
    )
  })
  it('fresh revocation removes private counts without removing authorized catalogue metadata', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('Persisted call records'))
    permissions = ['models.read_all', 'providers.read']
    const key = cache
      .getQueryCache()
      .findAll()
      .find((q) => q.queryKey.includes('permissions'))!.queryKey
    await act(async () => cache.refetchQueries({ queryKey: key, exact: true }))
    expect(host.textContent).toContain('Private mdl_one')
    expect(host.textContent).not.toContain('Persisted call records')
    expect(host.querySelector('tbody tr')!.querySelector('.tabular-nums')!.textContent).toBe(
      'Unknown',
    )
    expect(monthly()).toHaveLength(1)
  })
  it('does not query an empty catalogue or truncate an unsupported batch', async () => {
    models = []
    await mount()
    expect(monthly()).toHaveLength(0)
    models = Array.from({ length: 501 }, (_, i) => row('mdl_' + i))
    const key = cache
      .getQueryCache()
      .findAll()
      .find(
        (q) => q.queryKey[0] === 'admin' && q.queryKey[1] === 'models' && q.queryKey[2] === 'list',
      )!.queryKey
    await act(async () => cache.refetchQueries({ queryKey: key, exact: true }))
    await until(() => expect(host.querySelectorAll('tbody tr')).toHaveLength(501))
    expect(monthly()).toHaveLength(0)
    expect(host.textContent).toContain('The complete monthly request query is unavailable')
  })
  it('hides cached rows on catalogue invalidation and requires another authorized read', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('Persisted call records'))
    const key = cache
      .getQueryCache()
      .findAll()
      .find((q) => q.queryKey[2] === 'list')!.queryKey
    await act(async () =>
      cache.invalidateQueries({ queryKey: key, exact: true, refetchType: 'none' }),
    )
    expect(host.textContent).not.toContain('Private mdl_one')
    expect(host.textContent).not.toContain('Persisted call records')
    await act(async () => cache.refetchQueries({ queryKey: key, exact: true }))
    await until(() => expect(host.textContent).toContain('Persisted call records'))
  })
  it('discards a held old-target result after navigation, without restoring its private count', async () => {
    await mount('/admin/models/mdl_one')
    await until(() => expect(host.textContent).toContain('Persisted call records'))
    ignoreCancellation = true
    counts.mdl_one = '777'
    let release!: () => void
    holds.monthly = () =>
      new Promise<void>((resolve) => {
        release = resolve
      })
    await act(async () => {
      void cache.refetchQueries({ queryKey: ['admin', 'model-monthly-requests'] })
    })
    await until(() => expect(release).toBeTypeOf('function'))
    const oldSignal = signals.at(-1)!
    delete holds.monthly
    counts.mdl_two = '9'
    await act(async () => router.navigate('/admin/models/mdl_two'))
    await until(() => expect(host.textContent).toContain('Persisted call records'))
    expect(host.textContent).toContain('Private mdl_two')
    await until(() => expect(oldSignal.aborted).toBe(true))
    const before = requests.length
    await act(async () => release())
    expect(host.textContent).not.toContain('777')
    expect(host.textContent).not.toContain('Private mdl_one')
    expect(requests).toHaveLength(before)
    expect(host.querySelector('.tabular-nums')!.textContent).toBe('9')
  })
  it('discards a held previous-actor result after actor change', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('Persisted call records'))
    ignoreCancellation = true
    counts.mdl_one = '777'
    let release!: () => void
    holds.monthly = () =>
      new Promise<void>((resolve) => {
        release = resolve
      })
    await act(async () => {
      void cache.refetchQueries({ queryKey: ['admin', 'model-monthly-requests'] })
    })
    await until(() => expect(release).toBeTypeOf('function'))
    const oldSignal = signals.at(-1)!
    delete holds.monthly
    identity = {
      ...identity,
      user: { ...identity.user, id: 'usr_other' },
      csrf_token: 'csrf-other',
    }
    permissions = ['models.read_all']
    await act(async () => cache.setQueryData(sessionKey, identity))
    await until(() => expect(host.textContent).toContain('Private mdl_one'))
    await until(() => expect(oldSignal.aborted).toBe(true))
    await act(async () => release())
    expect(host.textContent).not.toContain('777')
    expect(host.textContent).not.toContain('Persisted call records')
    expect(host.querySelector('.tabular-nums')!.textContent).toBe('Unknown')
  })
  it('aborts unmounted work and never recreates private report cache from a late result', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('Persisted call records'))
    ignoreCancellation = true
    let release!: () => void
    holds.monthly = () =>
      new Promise<void>((resolve) => {
        release = resolve
      })
    await act(async () => {
      void cache.refetchQueries({ queryKey: ['admin', 'model-monthly-requests'] })
    })
    await until(() => expect(release).toBeTypeOf('function'))
    const oldSignal = signals.at(-1)!
    await act(async () => root.render(<div>Another workspace</div>))
    await until(() => expect(oldSignal.aborted).toBe(true))
    cache.removeQueries({ queryKey: ['admin', 'model-monthly-requests'] })
    await act(async () => release())
    expect(host.textContent).toBe('Another workspace')
    expect(
      cache.getQueryCache().findAll({ queryKey: ['admin', 'model-monthly-requests'] }),
    ).toHaveLength(0)
  })
})
