import { act, useEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider, useParams } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import AuthGate from '@/components/app/AuthGate'
import { UncertainIntentProvider } from '@/context/uncertain-intents'
import { sessionKey } from '@/hooks/use-auth'
import { ProviderSettings } from './detail'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
const token = `${'a'.repeat(64)}.${'b'.repeat(64)}`
const nextToken = `${'a'.repeat(64)}.${'c'.repeat(64)}`
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[], permissions: string[], actor: string, csrf: string
let sessionStatus: number, permissionStatus: number, metadataStatus: number, failure: number
let record: { id: string; name: string; etag: string; can_edit: boolean }
let hold: Promise<void> | undefined, readHold: Promise<void> | undefined
let mounts: number, unmounts: number
function Host() {
  const { providerId = 'prv_one' } = useParams()
  useEffect(() => {
    mounts++
    return () => {
      unmounts++
    }
  }, [])
  return <ProviderSettings provider={{ id: providerId, name: 'Supplier', connections: [] }} />
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  requests = []
  permissions = ['providers.read', 'providers.write']
  actor = 'usr_actor'
  csrf = 'csrf-original'
  sessionStatus = permissionStatus = metadataStatus = 200
  failure = 0
  hold = readHold = undefined
  mounts = unmounts = 0
  record = { id: 'prv_one', name: 'Supplier', etag: token, can_edit: true }
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    const reject = (status: number) => {
      throw new AxiosError('Controlled unavailable', '', config, undefined, { ...response, status })
    }
    if (config.url === '/setup') response.data = { initialized: true }
    else if (config.url === '/auth/session') {
      if (sessionStatus !== 200) reject(sessionStatus)
      response.data = {
        user: { id: actor, role: 'admin', name: actor, email: 'actor@example.invalid' },
        csrf_token: csrf,
      }
    } else if (config.url === '/auth/permissions') {
      if (permissionStatus !== 200) reject(permissionStatus)
      response.data = { permissions }
    } else if (config.url?.endsWith('/metadata')) {
      if (config.method === 'put') {
        if (hold) await hold
        if (failure) reject(failure)
        record = { ...record, name: JSON.parse(config.data).name, etag: nextToken }
        response.data = { provider: structuredClone(record), runtime_applied: true, changed: true }
      } else {
        if (readHold) await readHold
        if (metadataStatus !== 200) reject(metadataStatus)
        response.data = { ...record, id: config.url.split('/')[3] }
      }
      response.headers.set('Cache-Control', 'private, no-store')
      response.headers.set('ETag', `"${record.etag}"`)
    } else if (config.url?.endsWith('/quality-policy'))
      response.data = {
        provider_id: 'prv_one',
        enabled: false,
        window_minutes: 60,
        minimum_attempts: 25,
        min_success_rate_bps: 9800,
        max_p95_duration_ms: null,
        etag: 'quality-1',
        updated_at: null,
        updated_by: null,
        update_reason: '',
      }
    else throw new Error(`Unexpected request ${config.url}`)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
  await i18n.changeLanguage('en')
})
async function until(check: () => void) {
  for (let i = 0; i < 120; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      check()
      return
    } catch (error) {
      if (i === 119) throw error
    }
  }
}
function button(label: string) {
  const found = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (x) => x.textContent?.trim() === label,
  )
  expect(found, label).toBeTruthy()
  return found!
}
function input(label: string) {
  const found = [...document.querySelectorAll('label')]
    .find((x) => x.textContent === label)
    ?.querySelector<HTMLInputElement>('input')
  expect(found, label).toBeTruthy()
  return found!
}
async function change(label: string, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      input(label),
      value,
    )
    input(label).dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function mount() {
  router = createMemoryRouter(
    [
      {
        element: (
          <UncertainIntentProvider>
            <AuthGate mode="private" />
          </UncertainIntentProvider>
        ),
        children: [
          { path: '/admin/providers/:providerId', element: <Host /> },
          { path: '/other', element: <p>Other route</p> },
        ],
      },
      { path: '/login', element: <p>Login required</p> },
    ],
    { initialEntries: ['/admin/providers/prv_one?tab=settings'] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain('Provider quality thresholds'))
}
const writes = () => requests.filter((x) => x.method === 'put')
async function prepare() {
  await until(() => expect(input('Provider name').value).toBe('Supplier'))
  await change('Provider name', '  Renamed  ')
  await click('Save changes')
  await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
  await change('Change reason', '  Correct label  ')
}
async function submit() {
  await prepare()
  await click('Confirm name change')
  await until(() => expect(writes()).toHaveLength(1))
}
describe('Provider Settings current name', () => {
  it('places Basic Information before the unchanged quality card without child reads', async () => {
    await mount()
    await until(() => expect(input('Provider name').value).toBe('Supplier'))
    expect(host.textContent!.indexOf('Basic Information')).toBeLessThan(
      host.textContent!.indexOf('Provider quality thresholds'),
    )
    expect(button('Save changes').disabled).toBe(true)
    expect(
      requests.some((x) => x.url?.includes('/models') || x.url?.includes('/connections')),
    ).toBe(false)
  })
})
describe('Provider name authority and immutable current-state retries', () => {
  it('requires independent read permission and never fetches a write-only private record', async () => {
    permissions = ['providers.write', 'system.read', 'system.write']
    await mount()
    await until(() => expect(host.textContent).toContain('Provider read permission'))
    expect(requests.filter((x) => x.url?.endsWith('/metadata'))).toHaveLength(0)
    expect(document.querySelector('input')).toBeNull()
  })
  it('renders read-only names without deriving write permission from quality settings', async () => {
    permissions = ['providers.read', 'system.write']
    record.can_edit = false
    await mount()
    await until(() => expect(input('Provider name').disabled).toBe(true))
    expect(button('Save changes').disabled).toBe(true)
    expect(writes()).toHaveLength(0)
  })
  it('sends only trimmed reviewed name/reason with strong validator and current CSRF after explicit confirmation', async () => {
    await mount()
    await prepare()
    expect(writes()).toHaveLength(0)
    expect(button('Confirm name change').disabled).toBe(false)
    await click('Confirm name change')
    await until(() => expect(host.textContent).toContain('runtime publication are confirmed'))
    expect(writes()[0].data).toBe(JSON.stringify({ name: 'Renamed', reason: 'Correct label' }))
    expect(writes()[0].headers.get('If-Match')).toBe(`"${token}"`)
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-original')
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it.each([409, 503, 500])(
    'retains the first %s request and cannot resolve it by a matching current GET',
    async (status) => {
      failure = status
      await mount()
      await submit()
      const original = writes()[0]
      await until(() => expect(button('Retry original request').disabled).toBe(false))
      record = { ...record, name: 'Renamed', etag: nextToken }
      await act(async () => cache.refetchQueries({ queryKey: ['admin', 'provider-metadata'] }))
      await until(() => expect(button('Retry original request').disabled).toBe(false))
      expect(host.textContent).not.toContain('runtime publication are confirmed')
      expect(input('Provider name').value).toBe('Renamed')
      expect(input('Provider name').disabled).toBe(true)
      failure = 0
      csrf = 'csrf-renewed'
      await act(async () => cache.refetchQueries({ queryKey: sessionKey, exact: true }))
      await until(() => expect(button('Retry original request').disabled).toBe(false))
      await click('Retry original request')
      await until(() => expect(writes()).toHaveLength(2))
      expect(writes()[1].data).toBe(original.data)
      expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
      expect(writes()[1].headers.get('X-CSRF-Token')).toBe('csrf-renewed')
      await until(() => expect(host.textContent).toContain('runtime publication are confirmed'))
    },
  )
  it('recovers only the submitted intent after actual AuthGate Session500 unmount/remount without automatic PUT', async () => {
    failure = 503
    await mount()
    await submit()
    await until(() => expect(button('Retry original request').disabled).toBe(false))
    const original = writes()[0].data
    sessionStatus = 500
    await act(async () => cache.refetchQueries({ queryKey: sessionKey, exact: true }))
    await until(() => expect(unmounts).toBeGreaterThan(0))
    expect(host.textContent).not.toContain('Renamed')
    sessionStatus = 200
    csrf = 'csrf-after-gate'
    await act(async () => cache.refetchQueries({ queryKey: sessionKey, exact: true }))
    await until(() => expect(button('Retry original request').disabled).toBe(false))
    expect(mounts).toBeGreaterThan(1)
    expect(input('Provider name').value).toBe('Renamed')
    expect(writes()).toHaveLength(1)
    failure = 0
    await click('Retry original request')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].data).toBe(original)
    expect(writes()[1].headers.get('If-Match')).toBe(`"${token}"`)
    expect(writes()[1].headers.get('X-CSRF-Token')).toBe('csrf-after-gate')
  })
  it('ignores a held old result after mounted Session renewal and retries the refreshed opaque claim explicitly', async () => {
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await mount()
    await submit()
    csrf = 'csrf-new'
    await act(async () => cache.refetchQueries({ queryKey: sessionKey, exact: true }))
    await act(async () => release())
    await until(() => expect(button('Retry original request').disabled).toBe(false))
    expect(host.textContent).not.toContain('runtime publication are confirmed')
    expect(writes()).toHaveLength(1)
    hold = undefined
    await click('Retry original request')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].data).toBe(writes()[0].data)
    await until(() => expect(host.textContent).toContain('runtime publication are confirmed'))
  })
  it('hides stale private data during renewed permission reads/errors and cannot dispatch with cached authority', async () => {
    await mount()
    await until(() => expect(input('Provider name').value).toBe('Supplier'))
    await change('Provider name', 'Draft')
    permissionStatus = 500
    await act(async () => cache.refetchQueries({ queryKey: ['permissions', actor], exact: true }))
    await until(() =>
      expect(host.textContent).toContain('Provider information could not be loaded'),
    )
    expect(document.querySelector('input')).toBeNull()
    expect(writes()).toHaveLength(0)
    permissionStatus = 200
    permissions = ['providers.read']
    await act(async () => cache.refetchQueries({ queryKey: ['permissions', actor], exact: true }))
    await until(() => expect(input('Provider name').disabled).toBe(true))
    expect(button('Save changes').disabled).toBe(true)
  })
  it('hides cached target metadata while a real renewed GET is pending and preserves an ordinary mounted draft', async () => {
    await mount()
    await until(() => expect(input('Provider name').value).toBe('Supplier'))
    await change('Provider name', 'Draft')
    let release!: () => void
    readHold = new Promise((resolve) => {
      release = resolve
    })
    let fetching!: Promise<void>
    await act(async () => {
      fetching = cache.refetchQueries({ queryKey: ['admin', 'provider-metadata'] })
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    expect(document.querySelector('input')).toBeNull()
    await act(async () => {
      release()
      await fetching
    })
    await until(() => expect(input('Provider name').value).toBe('Draft'))
    expect(writes()).toHaveLength(0)
  })
  it('requires explicit review after current name changes and fresh review after Abandon without undo or success', async () => {
    failure = 409
    await mount()
    await submit()
    await until(() => expect(button('Abandon retained request').disabled).toBe(false))
    record = { ...record, name: 'Other current name', etag: nextToken }
    await act(async () => cache.refetchQueries({ queryKey: ['admin', 'provider-metadata'] }))
    await click('Abandon retained request')
    await until(() => expect(button('Abandon request')).toBeTruthy())
    await click('Abandon request')
    await until(() => expect(button('Review current Provider')).toBeTruthy())
    expect(button('Save changes').disabled).toBe(true)
    expect(host.textContent).not.toContain('runtime publication are confirmed')
    expect(writes()).toHaveLength(1)
    await click('Review current Provider')
    await click('Save changes')
    await change('Change reason', 'New reason')
    failure = 0
    await click('Confirm name change')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].headers.get('If-Match')).toBe(`"${nextToken}"`)
    expect(JSON.parse(writes()[1].data).reason).toBe('New reason')
  })
  it.each(['actor', 'target', 'route', 'expiry', 'logout'])(
    'destroys old intent on definitive %s boundary without stale callback effects',
    async (boundary) => {
      failure = 503
      await mount()
      await submit()
      await until(() => expect(button('Retry original request').disabled).toBe(false))
      await act(async () => {
        if (boundary === 'actor') {
          actor = 'usr_other'
          await cache.refetchQueries({ queryKey: sessionKey, exact: true })
        } else if (boundary === 'target')
          await router.navigate('/admin/providers/prv_two?tab=settings')
        else if (boundary === 'route') await router.navigate('/other')
        else if (boundary === 'expiry') window.dispatchEvent(new Event('routex:session-expired'))
        else cache.setQueryData(sessionKey, null)
      })
      await until(() => expect(document.body.textContent).not.toContain('Retry original request'))
      expect(writes()).toHaveLength(1)
      expect(cache.getMutationCache().getAll()).toHaveLength(0)
    },
  )
  it('switches live to Chinese while preserving exact submitted name/reason and supports Escape without dispatch', async () => {
    await mount()
    await prepare()
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!
    await act(async () =>
      dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(writes()).toHaveLength(0)
    await click('Save changes')
    await until(() => expect(input('Change reason').value).toBe('  Correct label  '))
    await act(async () => i18n.changeLanguage('zh'))
    expect(input('变更原因').value).toBe('  Correct label  ')
    failure = 409
    await click('确认名称变更')
    await until(() => expect(button('重试原始请求').disabled).toBe(false))
    expect(input('供应商名称').value).toBe('Renamed')
    await act(async () => i18n.changeLanguage('en'))
    expect(button('Retry original request')).toBeTruthy()
    expect(writes()).toHaveLength(1)
  })
})

it('hides former submitted content after a fresh exact identity changes and cannot retry it', async () => {
  failure = 503
  await mount()
  await submit()
  await until(() => expect(button('Retry original request').disabled).toBe(false))
  record = { ...record, name: 'New identity label', etag: `${'d'.repeat(64)}.${'c'.repeat(64)}` }
  await act(async () => cache.refetchQueries({ queryKey: ['admin', 'provider-metadata'] }))
  await until(() => expect(input('Provider name').value).toBe('New identity label'))
  expect(button('Retry original request').disabled).toBe(true)
  expect(document.body.textContent).not.toContain('Renamed')
  expect(host.textContent).toContain('identity is no longer current')
  expect(writes()).toHaveLength(1)
  await click('Abandon retained request')
  await click('Abandon request')
  await until(() => expect(button('Review current Provider')).toBeTruthy())
  await click('Review current Provider')
  expect(input('Provider name').value).toBe('New identity label')
})
