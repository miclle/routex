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
import type { ModelLifecycleReview } from '@/types/model-lifecycle'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/catalog'
import zh from '@/i18n/locales/zh/catalog'
import AdminModelsPage from './admin'
import { detailFixture, versionID } from './routing-weight-history-fixtures'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
const endpoint = '/admin/models/mdl_one/lifecycle'
const birth = '2026-01-01T00:00:00Z'
const etag = 'a'.repeat(64) + '.' + 'b'.repeat(64)
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let identity: Session,
  permissions: string[],
  requests: InternalAxiosRequestConfig[],
  failure: Record<string, number>,
  holds: Record<string, (() => Promise<void>) | undefined>
let models: Record<string, Model>, reviews: Record<string, ModelLifecycleReview>, applied: boolean
let commitOnFailure: boolean
const model = (id = 'mdl_one'): Model => ({
  id,
  name: id === 'mdl_one' ? 'Current' : 'Second',
  status: 'active',
  created_at: birth,
  names: [],
  bindings: [],
  granted_user_ids: ['usr_grant'],
})
const review = (id = 'mdl_one'): ModelLifecycleReview => ({
  model_id: id,
  current_name: id === 'mdl_one' ? 'Current' : 'Second',
  created_at: birth,
  status: 'active',
  observed_at: '2026-10-12T00:00:00Z',
  etag,
  can_change: true,
  runtime_applied: true,
})
function body(config: InternalAxiosRequestConfig) {
  return typeof config.data === 'string' ? JSON.parse(config.data) : config.data
}
const writes = () =>
  requests.filter((request) => request.method === 'put' && request.url?.endsWith('/lifecycle'))
function deferred() {
  let release!: () => void
  const promise = new Promise<void>((r) => {
    release = r
  })
  return { promise, release }
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  identity = {
    user: { id: 'usr_one', name: 'Reader', email: 'reader@example.invalid', role: 'member' },
    csrf_token: 'csrf-one',
  }
  permissions = ['models.read_all', 'models.write']
  requests = []
  failure = {}
  holds = {}
  applied = true
  commitOnFailure = false
  models = { mdl_one: model(), mdl_two: model('mdl_two') }
  reviews = { mdl_one: review(), mdl_two: review('mdl_two') }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } },
  })
  cache.setQueryData(sessionKey, identity)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const status = failure[config.method + ':' + config.url] || failure[config.url!] || 200
    let data: unknown
    if (config.url === '/auth/session') data = structuredClone(identity)
    else if (config.url === '/auth/permissions') data = { permissions: [...permissions] }
    else if (config.url?.endsWith('/lifecycle')) {
      const id = config.url.split('/')[3]
      if (config.method === 'put' && (status === 200 || commitOnFailure)) {
        models[id].status = body(config).status
        reviews[id] = {
          ...reviews[id],
          status: body(config).status,
          etag: 'a'.repeat(64) + '.' + 'c'.repeat(64),
          runtime_applied: applied,
        }
      }
      const value = {
        ...structuredClone(reviews[id]),
        can_change:
          reviews[id].status !== 'archived' &&
          reviews[id].can_change &&
          permissions.includes('models.write'),
      }
      data =
        config.method === 'get' ? value : { model: value, changed: true, runtime_applied: applied }
    } else if (config.url === '/admin/providers') data = { items: [] }
    else if (config.url?.includes('/weight-versions?'))
      data = {
        model_id: config.url.split('/')[3],
        items: [
          {
            ...detailFixture().version,
            model_id: config.url.split('/')[3],
            model_created_at: birth,
          },
        ],
        next_cursor: null,
      }
    else if (config.url === '/admin/models')
      data = { items: Object.values(structuredClone(models)) }
    else if (config.url?.startsWith('/admin/models/'))
      data = structuredClone(models[config.url.split('/')[3]])
    else throw new Error('Unexpected endpoint: ' + config.url)
    await holds[config.method + ':' + config.url]?.()
    const response = {
      config,
      status,
      statusText: '',
      headers: new AxiosHeaders({ 'Cache-Control': 'private, no-store' }),
      data,
    }
    if (status >= 400) throw new AxiosError('Denied', '', config, undefined, response)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = original
})
async function until(assertion: () => void) {
  for (let i = 0; i < 200; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5))
    })
    try {
      assertion()
      return
    } catch (error) {
      if (i === 199) throw error
    }
  }
}
async function mount(path = '/admin/models/mdl_one') {
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
  await until(() => expect(host.textContent).toContain('Current'))
}
const dialog = () => document.querySelector('[role="dialog"]')
function button(text: string, scope: ParentNode = document) {
  const found = [...scope.querySelectorAll('button')].find((item) => item.textContent === text)
  expect(found).toBeTruthy()
  return found!
}
async function click(text: string, scope?: ParentNode) {
  await act(async () => button(text, scope).click())
}
async function open() {
  await click(en.modelLifecycle.action)
  await until(() => expect(dialog()?.textContent).toContain(en.modelLifecycle.observed))
}
async function fill(value = 'Keep grants and history') {
  const input = dialog()!.querySelector<HTMLInputElement>('input')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function confirm(reason = 'Keep grants and history') {
  await fill(reason)
  await click(
    reviews.mdl_one.status === 'active' ? en.modelLifecycle.disable : en.modelLifecycle.enable,
  )
  await click(en.modelLifecycle.confirm)
}
async function refreshPermissions() {
  await act(async () => cache.refetchQueries({ queryKey: ['permissions', identity.user.id] }))
}
describe('reversible Model lifecycle review', () => {
  it('uses the existing list action cell without reading lifecycle per row', async () => {
    await mount('/admin/models')
    expect(requests.some((request) => request.url?.endsWith('/lifecycle'))).toBe(false)
    const trigger = host.querySelector<HTMLButtonElement>(
      'button[aria-label="Actions for Current"]',
    )!
    await act(async () => trigger.click())
    const item = document.querySelector<HTMLElement>('[role="menuitem"]')!
    expect(item.textContent).toBe(en.modelLifecycle.action)
    await act(async () => item.click())
    await until(() => expect(dialog()?.textContent).toContain(en.modelLifecycle.observed))
    expect(requests.filter((request) => request.url?.endsWith('/lifecycle'))).toHaveLength(1)
  })
  it.each(['active', 'disabled'] as const)(
    'reviews and confirms only the opposite status from %s without changing grants',
    async (status) => {
      models.mdl_one.status = status
      reviews.mdl_one.status = status
      await mount()
      await open()
      await confirm()
      await until(() => expect(writes()).toHaveLength(1))
      expect(body(writes()[0])).toEqual({
        status: status === 'active' ? 'disabled' : 'active',
        reason: 'Keep grants and history',
      })
      expect(writes()[0].headers.get('If-Match')).toBe('"' + etag + '"')
      expect(models.mdl_one.granted_user_ids).toEqual(['usr_grant'])
      expect(cache.getMutationCache().getAll()).toHaveLength(0)
      await until(() => expect(dialog()?.textContent).toContain(en.modelLifecycle.runtimeApplied))
      expect(dialog()?.textContent).toContain(en.modelLifecycle.boundary)
    },
  )
  it('keeps independent read-only permission and archived status read-only', async () => {
    permissions = ['models.read_all']
    models.mdl_one.status = 'archived'
    reviews.mdl_one.status = 'archived'
    reviews.mdl_one.can_change = false
    await mount()
    await open()
    expect(dialog()?.textContent).toContain(en.modelLifecycle.archived)
    expect(dialog()!.querySelector('input')).toBeNull()
    expect(writes()).toHaveLength(0)
    expect(requests.some((request) => request.url === '/admin/providers')).toBe(false)
  })
  it('offers archive only for disabled Models and requires localized terminal confirmation', async () => {
    models.mdl_one.status = 'disabled'
    reviews.mdl_one.status = 'disabled'
    await mount()
    await open()
    await fill('Retain stable identities')
    await click(en.modelLifecycle.archive)
    expect(writes()).toHaveLength(0)
    expect(dialog()?.textContent).toContain(en.modelLifecycle.archiveWarning)
    await act(async () => i18n.changeLanguage('zh'))
    expect(dialog()?.textContent).toContain(zh.modelLifecycle.archiveConfirmTitle)
    expect(dialog()?.textContent).toContain(zh.modelLifecycle.archiveWarning)
    expect(dialog()?.textContent).toContain('Retain stable identities')
    await click(zh.modelLifecycle.confirmArchive)
    await until(() => expect(models.mdl_one.status).toBe('archived'))
    expect(body(writes()[0])).toEqual({ status: 'archived', reason: 'Retain stable identities' })
    expect(writes()[0].headers.get('If-Match')).toBe('"' + etag + '"')
    await until(() => expect(dialog()?.textContent).toContain(zh.modelLifecycle.archived))
    expect(dialog()?.textContent).not.toContain(zh.modelLifecycle.enable)
    expect(models.mdl_one.granted_user_ids).toEqual(['usr_grant'])
  })
  it('does not offer an active-to-archive shortcut', async () => {
    await mount()
    await open()
    expect(dialog()?.textContent).not.toContain(en.modelLifecycle.archive)
    expect(writes()).toHaveLength(0)
  })
  it('retains unknown archive after current archived GET and requires independent write authority for exact retry', async () => {
    models.mdl_one.status = 'disabled'
    reviews.mdl_one.status = 'disabled'
    await mount()
    await open()
    await fill('Original archive')
    commitOnFailure = true
    failure['put:' + endpoint] = 503
    await click(en.modelLifecycle.archive)
    await click(en.modelLifecycle.confirmArchive)
    await until(() => expect(dialog()?.textContent).toContain(en.modelLifecycle.unknown))
    const original = writes()[0]
    delete failure['put:' + endpoint]
    await click(en.modelLifecycle.review)
    await until(() => expect(button(en.modelLifecycle.retry).disabled).toBe(false))
    expect(dialog()?.textContent).toContain('Original archive')
    expect(writes()).toHaveLength(1)
    permissions = ['models.read_all']
    await refreshPermissions()
    await until(() => expect(button(en.modelLifecycle.retry).disabled).toBe(true))
    expect(dialog()?.textContent).toContain('Original archive')
    permissions.push('models.write')
    await refreshPermissions()
    await until(() => expect(button(en.modelLifecycle.retry).disabled).toBe(false))
    await click(en.modelLifecycle.retry)
    await until(() => expect(writes()).toHaveLength(2))
    expect(body(writes()[1])).toEqual(body(original))
    expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    await until(() => expect(dialog()?.textContent).toContain(en.modelLifecycle.archived))
  })
  it('keeps archive retry reachable after Session renewal while the obsolete response is unsettled', async () => {
    models.mdl_one.status = 'disabled'
    reviews.mdl_one.status = 'disabled'
    await mount()
    await open()
    await fill('Archive across renewal')
    const obsolete = deferred()
    holds['put:' + endpoint] = () => obsolete.promise
    await click(en.modelLifecycle.archive)
    await click(en.modelLifecycle.confirmArchive)
    await until(() => expect(writes()).toHaveLength(1))
    identity = { ...identity, csrf_token: 'archive-fresh-csrf' }
    await act(async () => cache.refetchQueries({ queryKey: sessionKey, exact: true }))
    await until(() => expect(button(en.modelLifecycle.retry).disabled).toBe(false))
    expect(models.mdl_one.status).toBe('archived')
    delete holds['put:' + endpoint]
    await click(en.modelLifecycle.retry)
    await until(() => expect(writes()).toHaveLength(2))
    expect(body(writes()[1])).toEqual(body(writes()[0]))
    expect(writes()[1].headers.get('If-Match')).toBe(writes()[0].headers.get('If-Match'))
    expect(writes()[1].headers.get('X-CSRF-Token')).toBe('archive-fresh-csrf')
    await until(() => expect(dialog()?.textContent).toContain(en.modelLifecycle.archived))
    await act(async () => {
      obsolete.release()
      await obsolete.promise
    })
    expect(dialog()?.textContent).not.toContain('Archive across renewal')
    expect(dialog()?.textContent).not.toContain(en.modelLifecycle.retry)
    expect(writes()).toHaveLength(2)
  })
  it('drops unknown archive intent on actor change rather than giving a new actor an archive retry', async () => {
    models.mdl_one.status = 'disabled'
    reviews.mdl_one.status = 'disabled'
    await mount()
    await open()
    await fill('Prior actor archive')
    commitOnFailure = true
    failure['put:' + endpoint] = 503
    await click(en.modelLifecycle.archive)
    await click(en.modelLifecycle.confirmArchive)
    await until(() => expect(dialog()?.textContent).toContain('Prior actor archive'))
    identity = { ...identity, user: { ...identity.user, id: 'usr_two' }, csrf_token: 'csrf-two' }
    await act(async () => cache.setQueryData(sessionKey, identity))
    await until(() => expect(dialog()).toBeNull())
    await open()
    expect(dialog()?.textContent).toContain(en.modelLifecycle.archived)
    expect(dialog()?.textContent).not.toContain('Prior actor archive')
    expect(dialog()?.textContent).not.toContain(en.modelLifecycle.retry)
    expect(writes()).toHaveLength(1)
  })
  it('keeps archived Model editors inert while retained names, detail reads and weight history remain available', async () => {
    permissions.push('providers.read')
    models.mdl_one.names = [
      { name: 'Old/name', is_current: false, expires_at: '2030-01-01T00:00:00Z' },
    ]
    models.mdl_one.bindings = [
      {
        id: 'bnd_one',
        provider_model_id: 'pmd_one',
        provider_id: 'prv_one',
        connection_id: 'con_one',
        upstream_name: 'native',
        protocol: 'openai_chat',
        weight: 100,
        ready: true,
      },
    ]
    await mount()
    await click(en.adminModels.rename)
    expect(dialog()).toBeTruthy()
    models.mdl_one.status = 'archived'
    reviews.mdl_one.status = 'archived'
    reviews.mdl_one.can_change = false
    await act(async () => cache.refetchQueries({ queryKey: ['admin', 'models', 'detail'] }))
    await until(() => expect(dialog()).toBeNull())
    for (const label of [
      en.adminModels.rename,
      en.adminModels.addBinding,
      en.adminModels.grant,
      en.adminModels.saveWeights,
    ]) {
      expect(button(label).disabled).toBe(true)
      await click(label)
    }
    const alias = host.querySelector<HTMLButtonElement>(
      'button[aria-label="Review early stop for Old/name"]',
    )!
    expect(alias.disabled).toBe(true)
    expect(host.textContent).toContain('Old/name')
    expect(host.querySelector<HTMLInputElement>('input[type="number"]')!.disabled).toBe(true)
    expect(requests.some((request) => ['post', 'put'].includes(request.method!))).toBe(false)
    expect(button(en.weightHistory.action).disabled).toBe(false)
    await click(en.weightHistory.action)
    await until(() => expect(dialog()?.textContent).toContain(versionID))
    expect(
      requests.some(
        (request) => request.method === 'get' && request.url?.includes('/weight-versions?'),
      ),
    ).toBe(true)
    expect(requests.some((request) => ['post', 'put'].includes(request.method!))).toBe(false)
  })
  it('hides all lifecycle facts when read permission is revoked even when write remains', async () => {
    await mount()
    await open()
    permissions = ['models.write']
    await refreshPermissions()
    await until(() => expect(dialog()).toBeNull())
    expect(host.textContent).not.toContain(en.modelLifecycle.observed)
    expect(writes()).toHaveLength(0)
  })
  it('reads without write authority and blocks confirmation after write permission renewal', async () => {
    await mount()
    await open()
    await fill()
    await click(en.modelLifecycle.disable)
    permissions = ['models.read_all']
    await refreshPermissions()
    await until(() => expect(dialog()?.textContent).toContain(en.modelLifecycle.readOnly))
    expect(dialog()!.querySelector<HTMLInputElement>('input')!.disabled).toBe(true)
    expect(writes()).toHaveLength(0)
    expect(requests.some((request) => request.url?.endsWith('/lifecycle'))).toBe(true)
  })
  it.each([' ', 'x'.repeat(1025), 'bad\u0001reason'])(
    'rejects invalid reason without dispatch: %s',
    async (reason) => {
      await mount()
      await open()
      await fill(reason)
      await click(en.modelLifecycle.disable)
      expect(dialog()?.textContent).toContain(en.modelLifecycle.reasonError)
      expect(writes()).toHaveLength(0)
    },
  )
  it('hides recorded facts during refresh and after read failure, without restoring an old response', async () => {
    await mount()
    await open()
    const held = deferred()
    holds['get:' + endpoint] = () => held.promise
    await click(en.modelLifecycle.review)
    await until(() => expect(dialog()?.textContent).not.toContain(en.modelLifecycle.observed))
    failure['get:' + endpoint] = 503
    held.release()
    delete holds['get:' + endpoint]
    await until(() => expect(dialog()?.textContent).toContain(en.modelLifecycle.observed))
    await click(en.modelLifecycle.review)
    await until(() => expect(dialog()?.textContent).not.toContain(en.modelLifecycle.observed))
    expect(writes()).toHaveLength(0)
  })
  it('rejects a fresh review for a reborn same-ID Model', async () => {
    reviews.mdl_one.created_at = '2026-02-02T00:00:00Z'
    await mount()
    await click(en.modelLifecycle.action)
    await until(() => expect(dialog()?.querySelector('[role="alert"]')).toBeTruthy())
    expect(dialog()?.textContent).not.toContain(en.modelLifecycle.observed)
    expect(writes()).toHaveLength(0)
  })
  it('preserves reason through live language switching without fetching or writing', async () => {
    await mount()
    await open()
    await fill('Exact reason')
    const count = requests.length
    await act(async () => i18n.changeLanguage('zh'))
    expect(dialog()?.textContent).toContain(zh.modelLifecycle.title)
    expect(dialog()?.textContent).toContain(zh.modelLifecycle.boundary)
    expect(dialog()!.querySelector<HTMLInputElement>('input')!.value).toBe('Exact reason')
    expect(requests).toHaveLength(count)
  })
  it.each([409, 403])(
    'retains a conceptually committed first write rejected with %s until exact explicit retry',
    async (status) => {
      await mount()
      await open()
      commitOnFailure = true
      failure['put:' + endpoint] = status
      await confirm('Original committed intent')
      await until(() => expect(dialog()?.textContent).toContain('Original committed intent'))
      await until(() => expect(button(en.modelLifecycle.retry).disabled).toBe(true))
      expect(models.mdl_one.status).toBe('disabled')
      expect(dialog()?.textContent).toContain(
        status === 409 ? en.modelLifecycle.conflict : en.modelLifecycle.unknown,
      )
      const original = writes()[0]
      delete failure['put:' + endpoint]
      await click(en.modelLifecycle.review)
      await until(() => expect(button(en.modelLifecycle.retry).disabled).toBe(false))
      expect(dialog()?.textContent).toContain('Original committed intent')
      expect(writes()).toHaveLength(1)
      await click(en.modelLifecycle.retry)
      await until(() => expect(writes()).toHaveLength(2))
      expect(body(writes()[1])).toEqual(body(original))
      expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
      expect(writes()[1].headers.get('X-CSRF-Token')).toBe('csrf-one')
    },
  )
  it('keeps original uncertainty through refresh, dismissal and rejected identical retries', async () => {
    await mount()
    await open()
    failure['put:' + endpoint] = 503
    await confirm('Original reason')
    await until(() => expect(dialog()?.textContent).toContain(en.modelLifecycle.unknown))
    const original = writes()[0]
    await act(async () =>
      document.querySelector<HTMLButtonElement>('button[aria-label="Close"]')!.click(),
    )
    expect(dialog()).toBeNull()
    await open()
    expect(dialog()?.textContent).toContain('Original reason')
    await click(en.modelLifecycle.review)
    await until(() => expect(dialog()?.textContent).toContain(en.modelLifecycle.observed))
    expect(writes()).toHaveLength(1)
    failure['put:' + endpoint] = 409
    await click(en.modelLifecycle.retry)
    await until(() => expect(writes()).toHaveLength(2))
    await until(() => expect(dialog()?.textContent).toContain(en.modelLifecycle.conflict))
    expect(button(en.modelLifecycle.retry).disabled).toBe(true)
    expect(dialog()?.textContent).toContain('Original reason')
    delete failure['put:' + endpoint]
    await click(en.modelLifecycle.review)
    await until(() => expect(button(en.modelLifecycle.retry).disabled).toBe(false))
    expect(writes()).toHaveLength(2)
    await click(en.modelLifecycle.retry)
    await until(() => expect(writes()).toHaveLength(3))
    for (const request of writes()) {
      expect(body(request)).toEqual(body(original))
      expect(request.headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    }
  })
  it('does not resolve unconfirmed runtime application from a later GET', async () => {
    applied = false
    await mount()
    await open()
    await confirm()
    await until(() => expect(dialog()?.textContent).toContain(en.modelLifecycle.retry))
    reviews.mdl_one.runtime_applied = true
    await click(en.modelLifecycle.review)
    await until(() => expect(dialog()?.textContent).toContain(en.modelLifecycle.runtimeApplied))
    expect(dialog()?.textContent).toContain(en.modelLifecycle.retry)
    expect(writes()).toHaveLength(1)
    applied = true
    await click(en.modelLifecycle.retry)
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].headers.get('If-Match')).toBe(writes()[0].headers.get('If-Match'))
  })
  it('releases an obsolete unsettled write on renewal and fences its late completion from the new exact retry', async () => {
    await mount()
    await open()
    const obsolete = deferred()
    holds['put:' + endpoint] = () => obsolete.promise
    await confirm('Renewed exact intent')
    await until(() => expect(writes()).toHaveLength(1))
    identity = { ...identity, csrf_token: 'csrf-renewed' }
    await act(async () => cache.refetchQueries({ queryKey: sessionKey, exact: true }))
    await until(() => expect(button(en.modelLifecycle.retry).disabled).toBe(false))
    expect(writes()[0].signal?.aborted).toBe(true)
    expect(dialog()?.textContent).toContain('Renewed exact intent')
    // The first adapter deliberately remains unsettled after its signal is aborted.
    const retry = deferred()
    applied = false
    holds['put:' + endpoint] = () => retry.promise
    await click(en.modelLifecycle.retry)
    await until(() => expect(writes()).toHaveLength(2))
    expect(body(writes()[1])).toEqual(body(writes()[0]))
    expect(writes()[1].headers.get('If-Match')).toBe(writes()[0].headers.get('If-Match'))
    expect(writes()[1].headers.get('X-CSRF-Token')).toBe('csrf-renewed')
    expect(button(en.modelLifecycle.submitting).disabled).toBe(true)
    await act(async () => {
      obsolete.release()
      await obsolete.promise
    })
    expect(button(en.modelLifecycle.submitting).disabled).toBe(true)
    expect(document.querySelector<HTMLButtonElement>('button[aria-label="Close"]')!.disabled).toBe(
      true,
    )
    expect(dialog()?.textContent).toContain('Renewed exact intent')
    expect(dialog()?.textContent).not.toContain(en.modelLifecycle.responseConfirmed)
    delete holds['put:' + endpoint]
    retry.release()
    await until(() => expect(button(en.modelLifecycle.retry).disabled).toBe(false))
    expect(dialog()?.textContent).toContain(en.modelLifecycle.runtimeUnknown)
    expect(dialog()?.textContent).toContain('Renewed exact intent')
    expect(dialog()?.textContent).not.toContain(en.modelLifecycle.responseConfirmed)
    expect(writes()).toHaveLength(2)
  })
  it('discards held response facts after permission revocation but retains the exact dispatched intent', async () => {
    await mount()
    await open()
    const held = deferred()
    holds['put:' + endpoint] = () => held.promise
    await confirm('Permission changed')
    await until(() => expect(writes()).toHaveLength(1))
    permissions = ['models.read_all']
    await refreshPermissions()
    held.release()
    delete holds['put:' + endpoint]
    await until(() => expect(dialog()?.textContent).toContain('Permission changed'))
    expect(dialog()?.textContent).not.toContain(en.modelLifecycle.responseConfirmed)
    expect(button(en.modelLifecycle.retry).disabled).toBe(true)
    expect(writes()).toHaveLength(1)
  })
  it('cannot restore a held private read after target navigation', async () => {
    await mount()
    const held = deferred()
    holds['get:' + endpoint] = () => held.promise
    await click(en.modelLifecycle.action)
    await until(() => expect(requests.some((request) => request.url === endpoint)).toBe(true))
    await act(async () => router.navigate('/admin/models/mdl_two'))
    held.release()
    delete holds['get:' + endpoint]
    await until(() => expect(host.textContent).toContain('Second'))
    await open()
    expect(dialog()?.textContent).toContain('Second')
    expect([...dialog()!.querySelectorAll('dd')].map((item) => item.textContent)).toEqual(
      expect.arrayContaining(['Second', 'mdl_two']),
    )
    expect([...dialog()!.querySelectorAll('dd')].map((item) => item.textContent)).not.toContain(
      'Current',
    )
    expect(writes()).toHaveLength(0)
  })
  it('discards a held result after target change and cannot recreate another private review', async () => {
    await mount()
    await open()
    const held = deferred()
    holds['put:' + endpoint] = () => held.promise
    await confirm('Old target')
    await until(() => expect(writes()).toHaveLength(1))
    await act(async () => router.navigate('/admin/models/mdl_two'))
    await until(() => expect(host.textContent).toContain('Second'))
    held.release()
    delete holds['put:' + endpoint]
    await open()
    expect(dialog()?.textContent).toContain('Second')
    expect(dialog()?.textContent).not.toContain('Old target')
    expect(writes()).toHaveLength(1)
  })
  it('suppresses held review and result on actor change', async () => {
    await mount()
    await open()
    const held = deferred()
    holds['put:' + endpoint] = () => held.promise
    await confirm('Old actor')
    await until(() => expect(writes()).toHaveLength(1))
    identity = { ...identity, user: { ...identity.user, id: 'usr_two' }, csrf_token: 'csrf-two' }
    await act(async () => cache.setQueryData(sessionKey, identity))
    held.release()
    delete holds['put:' + endpoint]
    await until(() => expect(host.textContent).toContain('Current'))
    expect(dialog()).toBeNull()
    await open()
    expect(dialog()?.textContent).not.toContain('Old actor')
    expect(writes()).toHaveLength(1)
  })
})
