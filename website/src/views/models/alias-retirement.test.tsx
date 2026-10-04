import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { getModelAliasRetirement, retireModelAlias, validModelAliasReason } from '@/api/catalog'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { Model, ModelAliasRetirementReview } from '@/types/catalog'
import i18n from '@/i18n'
import AdminModelsPage from './admin'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let identity: Session,
  permissions: string[],
  requests: InternalAxiosRequestConfig[],
  failure: Record<string, number>
let holds: Record<string, (() => Promise<void>) | undefined>,
  models: Record<string, Model>,
  reviews: Record<string, ModelAliasRetirementReview>
let postRuntime: boolean, malformed: ((value: unknown) => unknown) | null
const endpoint = '/admin/models/mdl_one/alias-retirement'
function model(id = 'mdl_one'): Model {
  return {
    id,
    name: 'Current',
    status: 'active',
    names: [
      { name: 'Current', is_current: true, expires_at: null },
      { name: 'Alias/Case', is_current: false, expires_at: '2030-01-01T00:00:00Z' },
      { name: 'alias/case', is_current: false, expires_at: '2031-01-01T00:00:00Z' },
      { name: 'no-deadline', is_current: false, expires_at: null },
    ],
    bindings: [],
    granted_user_ids: [],
  }
}
function review(name = 'Alias/Case', id = 'mdl_one'): ModelAliasRetirementReview {
  return {
    model_id: id,
    current_name: 'Current',
    alias: {
      name,
      is_current: name === 'Current',
      expires_at: name === 'Current' || name === 'no-deadline' ? null : '2030-01-01T00:00:00Z',
    },
    state: name === 'Current' ? 'current' : name === 'no-deadline' ? 'retired' : 'compatibility',
    observed_at: '2026-10-04T00:00:00Z',
    etag: 'a'.repeat(64),
    can_retire: name !== 'Current' && name !== 'no-deadline',
    runtime_applied: true,
  }
}
function body(config: InternalAxiosRequestConfig) {
  return typeof config.data === 'string' ? JSON.parse(config.data) : config.data
}
function posts() {
  return requests.filter((r) => r.method === 'post' && r.url?.endsWith('/alias-retirement'))
}
function reviewRequests() {
  return requests.filter((r) => r.method === 'get' && r.url?.endsWith('/alias-retirement'))
}
function deferred() {
  let release!: () => void
  const promise = new Promise<void>((r) => {
    release = r
  })
  return { promise, release }
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  requests = []
  failure = {}
  holds = {}
  postRuntime = true
  malformed = null
  permissions = ['models.read_all', 'models.write']
  identity = {
    user: { id: 'usr_one', name: 'Reader', email: 'reader@example.invalid', role: 'member' },
    csrf_token: 'csrf-one',
  }
  models = {
    mdl_one: model(),
    mdl_two: {
      ...model('mdl_two'),
      name: 'Second current',
      names: [
        { name: 'Second current', is_current: true, expires_at: null },
        { name: 'Second-alias', is_current: false, expires_at: '2030-01-01T00:00:00Z' },
      ],
    },
  }
  reviews = {
    'Alias/Case': review(),
    'alias/case': review('alias/case'),
    'no-deadline': review('no-deadline'),
    Current: review('Current'),
    'Second-alias': { ...review('Second-alias', 'mdl_two'), current_name: 'Second current' },
  }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } },
  })
  cache.setQueryData(sessionKey, identity)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    await holds[config.url!]?.()
    const response = {
      config,
      status: failure[`${config.method}:${config.url}`] || failure[config.url!] || 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session') response.data = structuredClone(identity)
    else if (config.url === '/auth/permissions') response.data = { permissions: [...permissions] }
    else if (config.url?.endsWith('/alias-retirement')) {
      const name = config.method === 'get' ? config.params.name : body(config).name
      if (config.method === 'post' && response.status === 200) {
        reviews[name] = {
          ...reviews[name],
          state: 'retired',
          alias: { ...reviews[name].alias, expires_at: '2026-10-04T00:00:00Z' },
          etag: 'b'.repeat(64),
          can_retire: false,
          runtime_applied: postRuntime,
        }
        const target = models[reviews[name].model_id]
        target.names = target.names.map((n) =>
          n.name === name ? { ...n, expires_at: reviews[name].alias.expires_at } : n,
        )
      }
      const data = {
        ...structuredClone(reviews[name]),
        can_retire: reviews[name].can_retire && permissions.includes('models.write'),
      }
      response.data =
        config.method === 'get'
          ? data
          : { alias: data, retired: true, changed: true, runtime_applied: postRuntime }
      if (malformed) response.data = malformed(response.data)
    } else if (config.url?.startsWith('/admin/models/'))
      response.data = structuredClone(models[config.url.split('/')[3]])
    else throw new Error('Unexpected endpoint: ' + config.url)
    if (response.status >= 400) throw new AxiosError('Denied', '', config, undefined, response)
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
})
async function until(assertion: () => void) {
  for (let i = 0; i < 200; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5))
    })
    try {
      assertion()
      return
    } catch (e) {
      if (i === 199) throw e
    }
  }
}
async function mount() {
  router = createMemoryRouter([{ path: '/admin/models/:modelId', element: <AdminModelsPage /> }], {
    initialEntries: ['/admin/models/mdl_one'],
  })
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain('Compatibility names and history'))
}
function dialog() {
  return document.querySelector('[role="dialog"]')!
}
async function click(text: string, scope: ParentNode = document) {
  const b = [...scope.querySelectorAll('button')].find((x) => x.textContent === text)!
  expect(b).toBeTruthy()
  await act(async () => b.click())
}
async function open(name = 'Alias/Case') {
  const b = [...host.querySelectorAll('button')].find(
    (button) => button.getAttribute('aria-label') === `Review early stop for ${name}`,
  )!
  expect(b).toBeTruthy()
  await act(async () => b.click())
  await until(() => expect(dialog()?.textContent).toContain('Server observation'))
}
async function fill(value: string) {
  const input = document.querySelector<HTMLInputElement>('input[name="alias_reason"]')!
  expect(input).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function confirm(reason = 'Migrate callers') {
  await fill(reason)
  await click('Stop compatibility early')
  await until(() => expect(dialog().textContent).toContain('Stop this compatibility name?'))
  await click('Confirm early stop')
}
async function refreshPermissions() {
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['permissions', identity.user.id] })
  })
}

describe('Exact Model alias early stop', () => {
  it('preserves the compact card, reads only selected exact name and confirms a scoped server-applied target', async () => {
    await mount()
    expect(reviewRequests()).toHaveLength(0)
    expect(host.textContent).toContain('Recorded name')
    expect(host.textContent).toContain('No callable deadline recorded')
    await open()
    expect(reviewRequests()[0].params).toEqual({ name: 'Alias/Case' })
    await confirm()
    await until(() =>
      expect(document.body.textContent).toContain('This response confirmed a non-callable name'),
    )
    expect(posts()).toHaveLength(1)
    expect(body(posts()[0])).toEqual({ name: 'Alias/Case', reason: 'Migrate callers' })
    expect(posts()[0].headers.get('If-Match')).toBe('"' + 'a'.repeat(64) + '"')
    expect(posts()[0].headers.get('X-CSRF-Token')).toBe('csrf-one')
    expect(document.body.textContent).toContain('not proof of the original operation')
    expect(models.mdl_one.name).toBe('Current')
    expect(models.mdl_one.names.some((n) => n.name === 'Alias/Case')).toBe(true)
    expect(
      requests.every(
        (r) =>
          r.url === '/auth/session' ||
          r.url === '/auth/permissions' ||
          r.url?.startsWith('/admin/models/mdl_one'),
      ),
    ).toBe(true)
  })
  it('uses server state even when the browser clock says the recorded deadline is past', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(Date.parse('2040-01-01T00:00:00Z'))
    await mount()
    await open()
    expect(dialog().textContent).toContain('Compatibility period active')
    expect((dialog().querySelector('button[type="submit"]') as HTMLButtonElement).disabled).toBe(
      false,
    )
  })
  it('keeps read and write independent and never fetches privileged directories', async () => {
    permissions = ['models.read_all']
    await mount()
    await open()
    expect(dialog().textContent).toContain('cannot currently be stopped')
    expect((dialog().querySelector('input') as HTMLInputElement).disabled).toBe(true)
    expect(posts()).toHaveLength(0)
  })
  it('reviews null-deadline historical names as non-callable and never offers to stop the current name', async () => {
    await mount()
    expect(host.querySelector('button[aria-label="Review early stop for Current"]')).toBeNull()
    await open('no-deadline')
    expect(dialog().textContent).toContain('Non-callable; name reserved')
    expect(dialog().textContent).toContain('No callable deadline recorded')
    expect((dialog().querySelector('button[type="submit"]') as HTMLButtonElement).disabled).toBe(
      true,
    )
  })
  it('switches English to Chinese without losing the reason or exact selected name', async () => {
    await mount()
    await open()
    await fill('Caller migration')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(dialog().textContent).toContain('提前停用模型兼容名称')
    expect((dialog().querySelector('input') as HTMLInputElement).value).toBe('Caller migration')
    expect(dialog().textContent).toContain('Alias/Case')
    expect(document.body.textContent).not.toContain('aliasRetirement.')
  })
  it.each([' '.repeat(4), 'x'.repeat(1025), 'control\u0001reason', '😀'.repeat(257)])(
    'validates raw reason without dispatch: %s',
    async (reason) => {
      await mount()
      await open()
      await fill(reason)
      await click('Stop compatibility early')
      expect(dialog().textContent).toContain('1–1,024 UTF-8 bytes')
      expect(posts()).toHaveLength(0)
    },
  )
  it('clears only obsolete local reason validation when valid confirmation is prepared, without dispatch', async () => {
    await mount()
    await open()
    await click('Stop compatibility early')
    expect(dialog().textContent).toContain(
      'Enter a reason of 1–1,024 UTF-8 bytes without control characters.',
    )
    expect(posts()).toHaveLength(0)
    await fill('Controlled compatibility alias Early stop')
    await click('Stop compatibility early')
    await until(() => expect(dialog().textContent).toContain('Stop this compatibility name?'))
    expect(dialog().textContent).toContain('Controlled compatibility alias Early stop')
    expect(dialog().textContent).not.toContain(
      'Enter a reason of 1–1,024 UTF-8 bytes without control characters.',
    )
    expect(dialog().querySelector('[role="alert"]')).toBeNull()
    expect(posts()).toHaveLength(0)
  })
  it('retains conflict drafts but requires explicit refreshed review before another confirmation', async () => {
    await mount()
    await open()
    failure[`post:${endpoint}`] = 409
    await confirm('Keep this reason')
    await until(() => expect(dialog().textContent).toContain('configuration changed'))
    expect((dialog().querySelector('input') as HTMLInputElement).value).toBe('Keep this reason')
    expect((dialog().querySelector('button[type="submit"]') as HTMLButtonElement).disabled).toBe(
      true,
    )
    delete failure[`post:${endpoint}`]
    reviews['Alias/Case'].etag = 'c'.repeat(64)
    await click('Review current name state')
    await until(() =>
      expect((dialog().querySelector('button[type="submit"]') as HTMLButtonElement).disabled).toBe(
        false,
      ),
    )
    await click('Stop compatibility early')
    await click('Confirm early stop')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].headers.get('If-Match')).toBe('"' + 'c'.repeat(64) + '"')
  })
  it('keeps original unknown intent through current GET and rejected original retries, without automatic replay', async () => {
    await mount()
    await open()
    failure[endpoint] = 503
    await confirm('Original reason')
    await until(() => expect(dialog().textContent).toContain('request result is uncertain'))
    const original = posts()[0]
    delete failure[endpoint]
    reviews['Alias/Case'] = {
      ...reviews['Alias/Case'],
      state: 'retired',
      can_retire: false,
      etag: 'f'.repeat(64),
      alias: { ...reviews['Alias/Case'].alias, expires_at: '2026-10-04T00:00:00Z' },
    }
    await click('Review current name state')
    await until(() => expect(dialog().textContent).toContain('Non-callable; name reserved'))
    expect(dialog().textContent).toContain('Retry original stop request')
    expect(posts()).toHaveLength(1)
    for (const status of [403, 409]) {
      failure[`post:${endpoint}`] = status
      await click('Retry original stop request')
      await until(() =>
        expect((document.querySelector('button') as HTMLButtonElement).disabled).toBe(false),
      )
      await until(() => expect(posts()).toHaveLength(status === 403 ? 2 : 3))
      await until(() => expect(dialog()?.textContent).toContain('Original reason'))
    }
    delete failure[`post:${endpoint}`]
    await until(() => expect(dialog()?.textContent).toContain('Retry original stop request'))
    await click('Retry original stop request')
    await until(() => expect(document.body.textContent).toContain('This response confirmed'))
    for (const request of posts()) {
      expect(body(request)).toEqual(body(original))
      expect(request.headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    }
    expect(posts()).toHaveLength(4)
  })
  it('does not complete a saved non-callable result with unconfirmed runtime and retries original ETag', async () => {
    await mount()
    await open()
    postRuntime = false
    await confirm()
    await until(() => expect(dialog().textContent).toContain('runtime application is unconfirmed'))
    expect(dialog().textContent).toContain('Retry original stop request')
    postRuntime = true
    await click('Retry original stop request')
    await until(() => expect(document.body.textContent).toContain('This response confirmed'))
    expect(posts()[1].headers.get('If-Match')).toBe(posts()[0].headers.get('If-Match'))
  })
  it('retains a dismissed unknown intent and cannot switch to another alias until it is resolved', async () => {
    await mount()
    await open()
    failure[endpoint] = 503
    await confirm('Captured reason')
    await until(() => expect(dialog().textContent).toContain('request result is uncertain'))
    const close = document.querySelector<HTMLButtonElement>('button[aria-label="Close"]')!
    await act(async () => close.click())
    await until(() => expect(dialog()).toBeNull())
    expect(
      [...host.querySelectorAll('button')].find(
        (button) => button.getAttribute('aria-label') === 'Review early stop for alias/case',
      )!.disabled,
    ).toBe(true)
    delete failure[endpoint]
    await open()
    expect(dialog().textContent).toContain('Captured reason')
    expect(dialog().textContent).toContain('Retry original stop request')
    expect(posts()).toHaveLength(1)
  })
  it('uses current manual CSRF replacement without treating it as Session renewal', async () => {
    await mount()
    await open()
    await fill('Preserved')
    const reads = reviewRequests().length
    identity = { ...identity, csrf_token: 'csrf-current' }
    await act(async () => cache.setQueryData(sessionKey, identity))
    expect((dialog().querySelector('input') as HTMLInputElement).value).toBe('Preserved')
    expect(reviewRequests()).toHaveLength(reads)
    await click('Stop compatibility early')
    await click('Confirm early stop')
    await until(() => expect(posts()).toHaveLength(1))
    expect(posts()[0].headers.get('X-CSRF-Token')).toBe('csrf-current')
  })
  it('reauthorizes identical same-millisecond network Session reads with one observer while preserving draft', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(Date.now())
    await mount()
    await open()
    await fill('Retained across renewal')
    const before = requests.filter((r) => r.url === '/auth/session').length
    const held = deferred()
    holds['/admin/models/mdl_one'] = () => held.promise
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey })
    })
    await until(() => expect(host.textContent).not.toContain('Alias/Case'))
    expect(dialog()).toBeNull()
    expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(before + 1)
    delete holds['/admin/models/mdl_one']
    await act(async () => held.release())
    await until(() => expect(dialog()?.textContent).toContain('Server observation'))
    expect((dialog().querySelector('input') as HTMLInputElement).value).toBe(
      'Retained across renewal',
    )
    expect(posts()).toHaveLength(0)
  })
  it('hides private facts during permission renewal and retains unknown intent after read failure', async () => {
    await mount()
    await open()
    failure[endpoint] = 503
    await confirm('Uncertain original')
    await until(() => expect(dialog().textContent).toContain('request result is uncertain'))
    delete failure[endpoint]
    permissions = []
    await refreshPermissions()
    await until(() => expect(dialog()).toBeNull())
    expect(host.textContent).not.toContain('Alias/Case')
    expect(posts()).toHaveLength(1)
    permissions = ['models.read_all', 'models.write']
    await refreshPermissions()
    await until(() => expect(dialog()?.textContent).toContain('Retry original stop request'))
    expect(dialog().textContent).toContain('Uncertain original')
    expect(posts()).toHaveLength(1)
  })
  it('ignores late source-target and actor answers, destroys old sensitive state and never replays', async () => {
    await mount()
    await open()
    await fill('Old private reason')
    const held = deferred()
    holds[endpoint] = () => held.promise
    await click('Review current name state')
    await act(async () => {
      await router.navigate('/admin/models/mdl_two')
    })
    await until(() => expect(host.textContent).toContain('Second-alias'))
    await act(async () => held.release())
    expect(document.body.textContent).not.toContain('Old private reason')
    expect(document.body.textContent).not.toContain('Alias/Case')
    expect(posts()).toHaveLength(0)
    identity = { ...identity, user: { ...identity.user, id: 'usr_two' } }
    await act(async () => cache.setQueryData(sessionKey, identity))
    await until(() => expect(host.textContent).toContain('Second-alias'))
    expect(posts()).toHaveLength(0)
  })
  it('blocks an old confirmation click immediately after identical network Session completion', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(Date.now())
    await mount()
    await open()
    await fill('No stale dispatch')
    await click('Stop compatibility early')
    const old = [...dialog().querySelectorAll('button')].find(
      (b) => b.textContent === 'Confirm early stop',
    )!
    await act(async () => {
      await cache.fetchQuery({
        queryKey: sessionKey,
        queryFn: async () => structuredClone(identity),
        staleTime: 0,
      })
      expect(old.isConnected).toBe(false)
      old.click()
    })
    expect(posts()).toHaveLength(0)
    await until(() => expect(dialog()?.textContent).toContain('Server observation'))
    expect((dialog().querySelector('input') as HTMLInputElement).value).toBe('No stale dispatch')
  })
  it('blocks an old confirmation immediately when current write permission is removed', async () => {
    await mount()
    await open()
    await fill('No permission replay')
    await click('Stop compatibility early')
    const old = [...dialog().querySelectorAll('button')].find(
      (b) => b.textContent === 'Confirm early stop',
    )!
    const permissionQuery = cache
      .getQueryCache()
      .findAll({ queryKey: ['permissions', identity.user.id] })
      .find((q) => q.getObserversCount() > 0)!
    permissions = ['models.read_all']
    await act(async () => {
      cache.setQueryData(permissionQuery.queryKey, permissions)
      old.click()
    })
    expect(posts()).toHaveLength(0)
  })
  it('reauthorizes exact Model and permission reads even when successful timestamps are identical', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(Date.now())
    await mount()
    await open()
    await fill('Retained exact review')
    const first = reviewRequests().length
    reviews['Alias/Case'].etag = 'd'.repeat(64)
    await act(async () => {
      await cache.refetchQueries({
        queryKey: ['admin', 'models', 'detail', identity.user.id, 'mdl_one'],
      })
    })
    await until(() => expect(reviewRequests().length).toBeGreaterThan(first))
    await until(() => expect(dialog()?.textContent).toContain('Server observation'))
    expect((dialog().querySelector('input') as HTMLInputElement).value).toBe(
      'Retained exact review',
    )
    const before = reviewRequests().length
    await refreshPermissions()
    await until(() => expect(reviewRequests().length).toBeGreaterThan(before))
    expect(posts()).toHaveLength(0)
  })
  it('hides private dialog during a held permission read and restores draft only after fresh authority', async () => {
    await mount()
    await open()
    await fill('Private reason')
    const held = deferred()
    holds['/auth/permissions'] = () => held.promise
    let renewed!: Promise<void>
    await act(async () => {
      renewed = cache.refetchQueries({ queryKey: ['permissions', identity.user.id] })
    })
    await until(() => expect(dialog()).toBeNull())
    expect(document.body.textContent).not.toContain('Private reason')
    expect(host.textContent).not.toContain('Alias/Case')
    delete holds['/auth/permissions']
    await act(async () => {
      held.release()
      await renewed
    })
    await until(() => expect(dialog()?.textContent).toContain('Server observation'))
    expect((dialog().querySelector('input') as HTMLInputElement).value).toBe('Private reason')
  })
  it('keeps an unknown intent hidden through exact detail 403 and restores only the original retry', async () => {
    await mount()
    await open()
    failure[endpoint] = 503
    await confirm('Immutable reason')
    await until(() => expect(dialog().textContent).toContain('request result is uncertain'))
    delete failure[endpoint]
    failure['/admin/models/mdl_one'] = 403
    await act(async () => {
      await cache.refetchQueries({
        queryKey: ['admin', 'models', 'detail', identity.user.id, 'mdl_one'],
      })
    })
    await until(() => expect(dialog()).toBeNull())
    expect(document.body.textContent).not.toContain('Immutable reason')
    expect(posts()).toHaveLength(1)
    delete failure['/admin/models/mdl_one']
    await act(async () => {
      await cache.refetchQueries({
        queryKey: ['admin', 'models', 'detail', identity.user.id, 'mdl_one'],
      })
    })
    await until(() => expect(dialog()?.textContent).toContain('Retry original stop request'))
    expect(dialog().textContent).toContain('Immutable reason')
    expect(posts()).toHaveLength(1)
  })
  it('aborts a pending write on Session renewal and ignores late success while retaining original intent', async () => {
    await mount()
    await open()
    await fill('One exact request')
    await click('Stop compatibility early')
    const held = deferred()
    holds[endpoint] = () => held.promise
    await click('Confirm early stop')
    expect(posts()).toHaveLength(1)
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey })
    })
    delete holds[endpoint]
    await act(async () => held.release())
    await until(() => expect(dialog()?.textContent).toContain('Retry original stop request'))
    expect(document.body.textContent).not.toContain('This response confirmed')
    expect(posts()).toHaveLength(1)
    expect(dialog().textContent).toContain('One exact request')
  })
  it('locks duplicate confirmation synchronously and ignores late write after target change', async () => {
    await mount()
    await open()
    await fill('Single intent')
    await click('Stop compatibility early')
    const held = deferred()
    holds[endpoint] = () => held.promise
    const confirmButton = [...dialog().querySelectorAll('button')].find(
      (b) => b.textContent === 'Confirm early stop',
    )!
    await act(async () => {
      confirmButton.click()
      confirmButton.click()
    })
    expect(posts()).toHaveLength(1)
    await act(async () => {
      await router.navigate('/admin/models/mdl_two')
    })
    delete holds[endpoint]
    await act(async () => held.release())
    await until(() => expect(host.textContent).toContain('Second-alias'))
    expect(document.body.textContent).not.toContain('This response confirmed')
    expect(posts()).toHaveLength(1)
  })
})

describe('Alias API boundary', () => {
  it('preserves raw reason byte limits and exact case/slash names', async () => {
    expect(validModelAliasReason('😀'.repeat(256))).toBe(true)
    expect(validModelAliasReason('x'.repeat(1024))).toBe(true)
    expect(validModelAliasReason('x'.repeat(1025))).toBe(false)
    expect(validModelAliasReason('\ud800')).toBe(false)
    await getModelAliasRetirement('mdl_one', 'alias/case')
    expect(reviewRequests()[0].params).toEqual({ name: 'alias/case' })
    await expect(getModelAliasRetirement('mdl_one', ' Alias/Case')).rejects.toThrow(
      'Invalid Model alias target',
    )
    await expect(
      retireModelAlias('mdl_one', { name: 'Alias/Case', reason: 'x' }, 'A'.repeat(64), 'csrf'),
    ).rejects.toThrow('Invalid Model alias retirement intent')
  })
  it.each([
    'target',
    'name',
    'etag',
    'date',
    'state',
    'can-retire',
    'runtime',
    'null',
    'unknown-field',
  ] as const)('rejects malformed review %s', async (variant) => {
    malformed = (value) => {
      const v = structuredClone(value) as ModelAliasRetirementReview
      if (variant === 'target') v.model_id = 'mdl_other'
      if (variant === 'name') v.alias.name = 'Other'
      if (variant === 'etag') v.etag = 'A'.repeat(64)
      if (variant === 'date') v.observed_at = 'yesterday'
      if (variant === 'state') v.state = 'current'
      if (variant === 'can-retire') {
        v.state = 'retired'
        v.can_retire = true
      }
      if (variant === 'runtime') v.runtime_applied = 'yes' as unknown as boolean
      if (variant === 'unknown-field') return { ...v, unexpected_secret: 'never-cache' }
      if (variant === 'null') return null
      return v
    }
    await expect(getModelAliasRetirement('mdl_one', 'Alias/Case')).rejects.toThrow(
      'Invalid Model alias review response',
    )
  })
  it('rejects a write without matching retired/runtime evidence', async () => {
    malformed = (value) => ({ ...(value as object), runtime_applied: false })
    await expect(
      retireModelAlias('mdl_one', { name: 'Alias/Case', reason: 'x' }, 'a'.repeat(64), 'csrf'),
    ).rejects.toThrow('Invalid Model alias retirement response')
  })
})
