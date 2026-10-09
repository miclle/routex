import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider, useParams } from 'react-router'
import { AxiosError, AxiosHeaders, CanceledError, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import AuthGate from '@/components/app/AuthGate'
import { UncertainIntentProvider } from '@/context/uncertain-intents'
import { sessionKey } from '@/hooks/use-auth'
import type { ProviderStatus } from '@/types/provider-status'
import ProviderStatusCard from './provider-status'
import { ProviderSettings } from './detail'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
const token = `${'a'.repeat(64)}.${'b'.repeat(64)}`
const next = `${'a'.repeat(64)}.${'c'.repeat(64)}`
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[], permissions: string[], actor: string, csrf: string
let records: Record<string, ProviderStatus>,
  sessionFailure: number,
  permissionFailure: number,
  readFailure: number,
  writeFailure: number
let applyBeforeFailure: boolean, runtimeApplied: boolean, fullSettings: boolean
let holdUrl: string | null, holdMethod: string | null
let held: { release: () => void; config: InternalAxiosRequestConfig }[]
function Host() {
  const { providerId = '' } = useParams()
  return fullSettings ? (
    <ProviderSettings provider={{ id: providerId, name: 'Provider', connections: [] }} />
  ) : (
    <ProviderStatusCard providerId={providerId} />
  )
}
function currentRecord(id: string) {
  const value = structuredClone(records[id])
  value.can_edit = value.can_edit && permissions.includes('providers.write')
  if (actor !== 'usr_one') value.etag = `${'e'.repeat(64)}.${value.etag.slice(65)}`
  return value
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  requests = []
  permissions = ['providers.read', 'providers.write']
  actor = 'usr_one'
  csrf = 'csrf-original'
  sessionFailure = permissionFailure = readFailure = writeFailure = 0
  applyBeforeFailure = fullSettings = false
  runtimeApplied = true
  holdUrl = holdMethod = null
  held = []
  records = {
    prv_one: { id: 'prv_one', name: 'First Provider', enabled: true, can_edit: true, etag: token },
    prv_two: {
      id: 'prv_two',
      name: 'Second Provider',
      enabled: false,
      can_edit: true,
      etag: `${'d'.repeat(64)}.${'b'.repeat(64)}`,
    },
  }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      data: {} as unknown,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
    }
    let failure = 0
    if (config.url === '/setup') response.data = { initialized: true }
    else if (config.url === '/auth/session') {
      response.data = {
        user: { id: actor, role: 'admin', name: actor, email: 'actor@example.invalid' },
        csrf_token: csrf,
      }
      failure = sessionFailure
    } else if (config.url === '/auth/permissions') {
      response.data = { permissions: [...permissions] }
      failure = permissionFailure
    } else if (config.url?.endsWith('/status')) {
      const id = config.url.split('/')[3]
      let value = currentRecord(id)
      if (config.method === 'put') {
        const input = JSON.parse(config.data)
        const reviewed = config.headers.get('If-Match')
        failure =
          writeFailure ||
          (reviewed !== `"${value.etag}"` && value.enabled !== input.enabled ? 409 : 0)
        const changed = value.enabled !== input.enabled
        if (!failure || applyBeforeFailure) {
          records[id] = {
            ...records[id],
            enabled: input.enabled,
            etag: `${value.etag.slice(0, 64)}.${'c'.repeat(64)}`,
          }
          value = currentRecord(id)
        }
        response.data = { provider: value, runtime_applied: runtimeApplied, changed }
      } else {
        response.data = value
        failure = readFailure
      }
      response.headers.set('Cache-Control', 'private, no-store')
      response.headers.set('ETag', `"${value.etag}"`)
    } else if (config.url?.endsWith('/metadata')) {
      const id = config.url.split('/')[3]
      if (config.method === 'put') {
        records[id].name = JSON.parse(config.data).name
        records[id].etag = next
      }
      const { enabled, ...value } = currentRecord(id)
      expect(typeof enabled).toBe('boolean')
      response.data =
        config.method === 'put' ? { provider: value, runtime_applied: true, changed: true } : value
      response.headers.set('Cache-Control', 'private, no-store')
      response.headers.set('ETag', `"${value.etag}"`)
    } else if (config.url === '/admin/providers') response.data = { items: [] }
    else throw new Error(`Unexpected request ${config.url}`)
    if (config.url === holdUrl && (!holdMethod || config.method === holdMethod)) {
      await new Promise<void>((resolve, reject) => {
        held.push({ release: resolve, config })
        if (config.method !== 'put')
          config.signal?.addEventListener?.(
            'abort',
            () => reject(new CanceledError(undefined, config)),
            { once: true },
          )
      })
    }
    if (failure)
      throw new AxiosError('Controlled unavailable', '', config, undefined, {
        ...response,
        status: failure,
      })
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
  await i18n.changeLanguage('en')
})
async function until(assertion: () => void) {
  for (let i = 0; i < 140; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assertion()
      return
    } catch (error) {
      if (i === 139) throw error
    }
  }
}
const findButton = (label: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (button) => button.textContent?.trim() === label || button.getAttribute('aria-label') === label,
  )
const button = (label: string) => {
  const value = findButton(label)
  expect(value, label).toBeTruthy()
  return value!
}
const dialog = () => document.querySelector<HTMLElement>('[role="dialog"]')
const input = (label: string) => {
  const value = [...document.querySelectorAll('label')]
    .find((node) => node.textContent === label)
    ?.querySelector<HTMLInputElement>('input')
  expect(value, label).toBeTruthy()
  return value!
}
async function click(label: string) {
  await act(async () => button(label).click())
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
async function mount(settings = false, ready = true) {
  fullSettings = settings
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
  if (ready) await until(() => expect(button('Disable Provider')).toBeTruthy())
}
const writes = () =>
  requests.filter((request) => request.method === 'put' && request.url?.endsWith('/status'))
const statusKeys = () =>
  cache
    .getQueryCache()
    .getAll()
    .filter((query) => query.queryKey[1] === 'provider-status')
    .map((query) => query.queryKey)
const permissionKeys = () =>
  cache
    .getQueryCache()
    .getAll()
    .filter((query) => query.queryKey[0] === 'permissions')
    .map((query) => query.queryKey)
async function renew(keys: readonly (readonly unknown[])[]) {
  await act(async () => {
    for (const key of keys) void cache.invalidateQueries({ queryKey: key, exact: true })
  })
}
async function prepare(reason = '  Planned maintenance  ') {
  await click('Disable Provider')
  await until(() => expect(dialog()).not.toBeNull())
  await change('Status change reason', reason)
}
async function submit() {
  await prepare()
  await click('Confirm Provider disable')
  await until(() => expect(writes()).toHaveLength(1))
}

it('places actual Provider status after Basic Information and before the retained quality policy card', async () => {
  await mount(true)
  expect([...host.querySelectorAll('h3')].map((heading) => heading.textContent)).toEqual([
    'Basic Information',
    'Provider status',
    'Provider quality thresholds',
  ])
  expect(host.textContent).toContain('Recorded as enabled for eligible routing')
  expect(
    requests.some(
      (request) => request.url?.includes('/models') || request.url?.includes('/connections'),
    ),
  ).toBe(false)
})
it('requires independent read authority and never treats write-only permission as a status read grant', async () => {
  permissions = ['providers.write']
  await mount(false, false)
  await until(() =>
    expect(host.textContent).toContain('Current Provider read permission is required'),
  )
  expect(requests.some((request) => request.url?.endsWith('/status'))).toBe(false)
  expect(findButton('Disable Provider')).toBeUndefined()
})
it.each(['permission', 'projection'])(
  'keeps status visible while %s denies writes',
  async (kind) => {
    if (kind === 'permission') permissions = ['providers.read']
    else records.prv_one.can_edit = false
    await mount()
    expect(button('Disable Provider').disabled).toBe(true)
    expect(host.textContent).toContain('Recorded as enabled')
    expect(writes()).toHaveLength(0)
  },
)
it('requires a valid reason and explicit confirmation, sends exact status-only review, and confirms only current local application', async () => {
  await mount()
  await click('Disable Provider')
  await until(() => expect(dialog()).not.toBeNull())
  expect(button('Confirm Provider disable').disabled).toBe(true)
  await change('Status change reason', '  Planned maintenance  ')
  expect(writes()).toHaveLength(0)
  cache.setQueryData(['admin', 'provider-metadata', actor, 'prv_one', 'inactive'], {
    private: true,
  })
  cache.setQueryData(['admin', 'providers'], [])
  await click('Confirm Provider disable')
  await until(() =>
    expect(host.textContent).toContain(
      'Current Provider status and local runtime application are confirmed',
    ),
  )
  expect(writes()).toHaveLength(1)
  expect(writes()[0].url).toBe('/admin/providers/prv_one/status')
  expect(JSON.parse(writes()[0].data)).toEqual({ enabled: false, reason: 'Planned maintenance' })
  expect(writes()[0].headers.get('If-Match')).toBe(`"${token}"`)
  expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-original')
  expect(host.textContent).toContain('original operation history is not established')
  expect(
    cache.getQueryState(['admin', 'provider-metadata', actor, 'prv_one', 'inactive'])
      ?.isInvalidated,
  ).toBe(true)
  expect(cache.getQueryState(['admin', 'providers'])?.isInvalidated).toBe(true)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it.each(['', '\n', '\ud800', '中'.repeat(342)])(
  'rejects invalid reason without dispatch %j',
  async (value) => {
    await mount()
    await prepare(value)
    expect(button('Confirm Provider disable').disabled).toBe(true)
    expect(writes()).toHaveLength(0)
  },
)
it('preserves open confirmation and draft across live English/Chinese switching without requests', async () => {
  await mount()
  await prepare('Exact draft')
  const before = requests.length
  await act(async () => i18n.changeLanguage('zh'))
  expect(dialog()!.textContent).toContain('停用供应商？')
  expect(input('状态变更理由').value).toBe('Exact draft')
  expect(button('确认停用供应商')).toBeTruthy()
  expect(requests).toHaveLength(before)
  await act(async () => i18n.changeLanguage('en'))
  expect(input('Status change reason').value).toBe('Exact draft')
})
it('cancels a reviewed but undispatched change and restores current action focus', async () => {
  await mount()
  await prepare()
  await click('Cancel')
  await until(() => expect(dialog()).toBeNull())
  expect(writes()).toHaveLength(0)
  await until(() => expect(document.activeElement).toBe(button('Disable Provider')))
})
it.each([409, 412])(
  'retains desired status/reason after initial conflict %s and requires explicit fresh revision review',
  async (status) => {
    await mount()
    await prepare('Retained reason')
    records.prv_one.etag = next
    writeFailure = status
    await click('Confirm Provider disable')
    await until(() => expect(button('Review current Provider configuration')).toBeTruthy())
    expect(button('Confirm Provider disable').disabled).toBe(true)
    expect(input('Status change reason').value).toBe('Retained reason')
    await click('Review current Provider configuration')
    expect(button('Confirm Provider disable').disabled).toBe(false)
    writeFailure = 0
    await click('Confirm Provider disable')
    await until(() => expect(writes()).toHaveLength(2))
    expect(JSON.parse(writes()[1].data)).toEqual({ enabled: false, reason: 'Retained reason' })
    expect(writes()[1].headers.get('If-Match')).toBe(`"${next}"`)
  },
)
it('requires explicit review if fresh name/status revision changes before confirmation', async () => {
  await mount()
  await prepare('Reason')
  records.prv_one.name = 'New recorded name'
  records.prv_one.etag = next
  await renew(statusKeys())
  await until(() => expect(dialog()).not.toBeNull())
  expect(dialog()!.textContent).toContain('New recorded name')
  expect(button('Confirm Provider disable').disabled).toBe(true)
  expect(writes()).toHaveLength(0)
  await click('Review current Provider configuration')
  await click('Confirm Provider disable')
  await until(() => expect(writes()).toHaveLength(1))
  expect(writes()[0].headers.get('If-Match')).toBe(`"${next}"`)
})
it.each([500, 503])(
  'retains identical unconfirmed request %s through dismissal and matching GET without historical-success claims',
  async (failure) => {
    await mount()
    writeFailure = failure
    applyBeforeFailure = true
    await submit()
    await until(() => expect(button('Retry identical Provider status change').disabled).toBe(false))
    const original = writes()[0]
    await click('Cancel')
    expect(dialog()).toBeNull()
    await renew(statusKeys())
    await until(() => expect(button('Review unconfirmed Provider change')).toBeTruthy())
    expect(host.textContent).toContain('remains unconfirmed')
    expect(host.textContent).not.toContain(
      'Current Provider status and local runtime application are confirmed',
    )
    writeFailure = 409
    await click('Retry identical Provider status change')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].data).toBe(original.data)
    expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(host.textContent).toContain('remains unconfirmed')
    expect(findButton('Review current Provider configuration')).toBeUndefined()
  },
)
it('reconciles a lost response by identical retry200 only as current local state, preserving original-history distinction', async () => {
  await mount()
  writeFailure = 503
  applyBeforeFailure = true
  await submit()
  await until(() => expect(button('Retry identical Provider status change').disabled).toBe(false))
  const original = writes()[0]
  await click('Cancel')
  await renew(statusKeys())
  await until(() => expect(button('Review unconfirmed Provider change')).toBeTruthy())
  writeFailure = 0
  await click('Retry identical Provider status change')
  await until(() =>
    expect(host.textContent).toContain(
      'Current Provider status and local runtime application are confirmed',
    ),
  )
  expect(writes()[1].data).toBe(original.data)
  expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  expect(host.textContent).toContain('original operation history is not established')
})
it('retains postcommit uncertainty when a later opposite change makes the original retry conflict', async () => {
  await mount()
  writeFailure = 503
  applyBeforeFailure = true
  await submit()
  await until(() => expect(button('Retry identical Provider status change').disabled).toBe(false))
  const original = writes()[0]
  records.prv_one.enabled = true
  records.prv_one.etag = `${'a'.repeat(64)}.${'f'.repeat(64)}`
  applyBeforeFailure = false
  writeFailure = 0
  await click('Cancel')
  await renew(statusKeys())
  await until(() => expect(button('Retry identical Provider status change').disabled).toBe(false))
  await click('Retry identical Provider status change')
  await until(() => expect(writes()).toHaveLength(2))
  expect(writes()[1].data).toBe(original.data)
  expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  expect(host.textContent).toContain('remains unconfirmed')
  expect(host.textContent).not.toContain('local runtime application are confirmed')
  await click('Review a separate Provider change')
  await until(() => expect(button('Discard retry and review a separate change')).toBeTruthy())
  expect(dialog()!.textContent).toContain('First Provider is currently recorded as Enabled')
  await click('Discard retry and review a separate change')
  expect(input('Status change reason').value).toBe('')
  await change('Status change reason', 'Separate current operation')
  await click('Confirm Provider disable')
  await until(() => expect(writes()).toHaveLength(3))
  expect(writes()[2].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}.${'f'.repeat(64)}"`)
  expect(JSON.parse(writes()[2].data)).toEqual({
    enabled: false,
    reason: 'Separate current operation',
  })
})
it('starts a separate change only after refreshed current facts, explicit discard, fresh reason and another confirmation', async () => {
  await mount()
  writeFailure = 503
  applyBeforeFailure = true
  await submit()
  await until(() => expect(button('Retry identical Provider status change').disabled).toBe(false))
  await click('Cancel')
  await click('Review a separate Provider change')
  await until(() => expect(button('Discard retry and review a separate change')).toBeTruthy())
  expect(dialog()!.textContent).toContain('First Provider is currently recorded as Disabled')
  expect(writes()).toHaveLength(1)
  await click('Cancel')
  expect(button('Retry identical Provider status change')).toBeTruthy()
  await click('Review a separate Provider change')
  await until(() => expect(button('Discard retry and review a separate change')).toBeTruthy())
  await click('Discard retry and review a separate change')
  expect(button('Confirm Provider enable').disabled).toBe(true)
  expect(writes()).toHaveLength(1)
  expect(host.textContent).toContain('earlier request remains unconfirmed')
  await change('Status change reason', 'Separate operation')
  writeFailure = 0
  await click('Confirm Provider enable')
  await until(() => expect(writes()).toHaveLength(2))
  expect(JSON.parse(writes()[1].data)).toEqual({ enabled: true, reason: 'Separate operation' })
  expect(writes()[1].headers.get('If-Match')).toBe(`"${next}"`)
})
it('retains original request across real same-actor Gate500 unmount/remount and retries using fresh CSRF', async () => {
  await mount()
  writeFailure = 503
  await submit()
  await until(() => expect(button('Retry identical Provider status change').disabled).toBe(false))
  const original = writes()[0]
  sessionFailure = 500
  await renew([sessionKey])
  await until(() => expect(host.textContent).toContain('Unable to check your session'))
  expect(dialog()).toBeNull()
  expect(findButton('Review unconfirmed Provider change')).toBeUndefined()
  sessionFailure = 0
  csrf = 'csrf-renewed'
  await click('Retry')
  await until(() => expect(button('Review unconfirmed Provider change')).toBeTruthy())
  writeFailure = 0
  await click('Retry identical Provider status change')
  await until(() => expect(writes()).toHaveLength(2))
  expect(writes()[1].data).toBe(original.data)
  expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  expect(writes()[1].headers.get('X-CSRF-Token')).toBe('csrf-renewed')
})
it.each(['session', 'permission', 'actor', 'provider', 'expiry', 'unmount'])(
  'rejects obsolete successful callbacks and private query refresh after %s changes',
  async (kind) => {
    await mount()
    await prepare()
    holdUrl = '/admin/providers/prv_one/status'
    holdMethod = 'put'
    await click('Confirm Provider disable')
    await until(() => expect(held).toHaveLength(1))
    const operation = held[0]
    holdUrl = holdMethod = null
    if (kind === 'unmount') await act(async () => root.render(null))
    else if (kind === 'provider')
      await act(async () => router.navigate('/admin/providers/prv_two?tab=settings'))
    else if (kind === 'expiry')
      await act(async () => window.dispatchEvent(new Event('routex:session-expired')))
    else if (kind === 'permission') {
      permissions = ['providers.read']
      await renew(permissionKeys())
    } else {
      if (kind === 'actor') actor = 'usr_other'
      csrf = 'csrf-renewed'
      await renew([sessionKey])
    }
    await until(() => expect(operation.config.signal?.aborted).toBe(true))
    if (kind === 'session')
      await until(() => expect(button('Retry identical Provider status change')).toBeTruthy())
    const before = requests.length
    await act(async () => operation.release())
    expect(host.textContent).not.toContain(
      'Current Provider status and local runtime application are confirmed',
    )
    expect(requests).toHaveLength(before)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    if (kind === 'provider') {
      await until(() => expect(button('Enable Provider')).toBeTruthy())
      expect(host.textContent).not.toContain('First Provider')
    }
    if (kind === 'actor') {
      await until(() => expect(button('Enable Provider')).toBeTruthy())
      expect(findButton('Retry identical Provider status change')).toBeUndefined()
    }
  },
)
it('hides facts/action/dialog during pending status reads and blocks captured confirmation before React renders', async () => {
  await mount()
  await prepare()
  const confirm = button('Confirm Provider disable')
  holdUrl = '/admin/providers/prv_one/status'
  holdMethod = 'get'
  await act(async () => {
    for (const key of statusKeys()) void cache.invalidateQueries({ queryKey: key, exact: true })
    confirm.click()
  })
  await until(() => expect(held).toHaveLength(1))
  expect(writes()).toHaveLength(0)
  expect(dialog()).toBeNull()
  expect(host.textContent).not.toContain('Recorded as enabled')
  holdUrl = holdMethod = null
  await act(async () => held[0].release())
  await until(() => expect(dialog()).not.toBeNull())
  expect(input('Status change reason').value).toBe('  Planned maintenance  ')
})
it('hides cached facts when permissions fail and requires a new authorized resource read after recovery', async () => {
  await mount()
  permissionFailure = 503
  await renew(permissionKeys())
  await until(() => expect(findButton('Disable Provider')).toBeUndefined())
  expect(host.textContent).not.toContain('Recorded as enabled')
  permissionFailure = 0
  await renew(permissionKeys())
  await until(() => expect(button('Disable Provider')).toBeTruthy())
  expect(
    requests.filter((request) => request.method === 'get' && request.url?.endsWith('/status'))
      .length,
  ).toBeGreaterThanOrEqual(2)
})
it('requires exact new-identity review before replacing an uncertain original request', async () => {
  await mount()
  writeFailure = 503
  await submit()
  await until(() => expect(button('Retry identical Provider status change').disabled).toBe(false))
  await click('Cancel')
  records.prv_one.etag = `${'d'.repeat(64)}.${'b'.repeat(64)}`
  await renew(statusKeys())
  await until(() => expect(host.textContent).toContain('reviewed Provider identity changed'))
  expect(button('Retry identical Provider status change').disabled).toBe(true)
  expect(writes()).toHaveLength(1)
})
it('locks captured Cancel and duplicate confirmation synchronously before dispatch', async () => {
  await mount()
  await prepare()
  holdUrl = '/admin/providers/prv_one/status'
  holdMethod = 'put'
  const submit = button('Confirm Provider disable'),
    cancel = button('Cancel')
  await act(async () => {
    submit.click()
    cancel.click()
    submit.click()
  })
  await until(() => expect(held).toHaveLength(1))
  expect(writes()).toHaveLength(1)
  expect(dialog()!.textContent).toContain('Disable Provider?')
  expect(held[0].config.signal?.aborted).not.toBe(true)
  holdUrl = holdMethod = null
  await act(async () => held[0].release())
  await until(() => expect(dialog()).toBeNull())
})
it('rejects captured confirmation when Cancel wins the same event turn', async () => {
  await mount()
  await prepare()
  const submit = button('Confirm Provider disable'),
    cancel = button('Cancel')
  await act(async () => {
    cancel.click()
    submit.click()
  })
  expect(writes()).toHaveLength(0)
  expect(dialog()).toBeNull()
})
it('does not report enforcement when a saved response lacks authoritative local application', async () => {
  await mount()
  runtimeApplied = false
  await submit()
  await until(() => expect(button('Retry identical Provider status change').disabled).toBe(false))
  expect(host.textContent).not.toContain(
    'Current Provider status and local runtime application are confirmed',
  )
  expect(host.textContent).toContain('remains unconfirmed')
})
it('keeps exact reviewed reason outside query/mutation caches and browser storage', async () => {
  await mount()
  const storage = vi.spyOn(Storage.prototype, 'setItem')
  await prepare('STATUS_REASON_UNCACHED')
  writeFailure = 503
  await click('Confirm Provider disable')
  await until(() => expect(writes()).toHaveLength(1))
  expect(
    JSON.stringify(
      cache
        .getQueryCache()
        .getAll()
        .map((query) => query.state.data),
    ),
  ).not.toContain('STATUS_REASON_UNCACHED')
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  expect(storage).not.toHaveBeenCalled()
})
it('refreshes the status name/review after an authorized metadata save sharing the same Provider revision', async () => {
  await mount(true)
  await change('Provider name', 'Renamed Provider')
  await click('Save changes')
  await until(() => expect(dialog()).not.toBeNull())
  await change('Change reason', 'Name correction')
  const before = requests.filter(
    (request) => request.method === 'get' && request.url?.endsWith('/status'),
  ).length
  await click('Confirm name change')
  await until(() =>
    expect(
      requests.filter((request) => request.method === 'get' && request.url?.endsWith('/status')),
    ).toHaveLength(before + 1),
  )
  await until(() => expect(button('Disable Provider')).toBeTruthy())
  await prepare('Status after rename')
  expect(dialog()!.textContent).toContain('Renamed Provider')
  await click('Confirm Provider disable')
  await until(() => expect(writes()).toHaveLength(1))
  expect(writes()[0].headers.get('If-Match')).toBe(`"${next}"`)
})
