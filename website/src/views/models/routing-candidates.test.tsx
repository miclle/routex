import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes, Link } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import AdminModelsPage from './admin'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const adapter = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  requests: InternalAxiosRequestConfig[],
  permissions: string[],
  session: Session
let wait: Promise<void> | null,
  fail: number,
  modelName: string,
  rows: unknown[],
  next: string | null
const candidate = {
  id: 'pmd_new',
  provider_id: 'prv_new',
  connection_id: 'con_new',
  provider_name: 'Exact supplier',
  connection_name: 'Recorded Connection',
  upstream_name: 'literal%_upstream',
  protocol: 'openai_chat',
  verification_covered: true,
  configured_available: true,
  selectable: true,
  review_etag: 'a'.repeat(64),
  prices: null,
}
const model = () => ({
  id: 'mdl_one',
  name: modelName,
  status: 'active',
  names: [{ name: modelName, is_current: true, expires_at: null }],
  granted_user_ids: [],
  bindings: [
    {
      id: 'bnd_old',
      provider_model_id: 'pmd_old',
      provider_id: 'prv_old',
      connection_id: 'con_old',
      upstream_name: 'old model',
      protocol: 'openai_chat',
      weight: 100,
      ready: true,
    },
  ],
})
beforeEach(async () => {
  await i18n.changeLanguage('en')
  requests = []
  permissions = ['models.read_all', 'providers.read', 'models.write']
  session = {
    user: { id: 'usr_one', role: 'admin', name: 'Admin', email: 'a@example.invalid' },
    csrf_token: 'csrf-one',
  }
  wait = null
  fail = 0
  modelName = 'Public Model'
  rows = [structuredClone(candidate)]
  next = null
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown,
      status = 200
    if (config.url === '/auth/session') data = structuredClone(session)
    else if (config.url === '/auth/permissions') data = { permissions: [...permissions] }
    else if (config.url === '/admin/providers') data = { items: [] }
    else if (config.url === '/admin/models/mdl_one') data = model()
    else if (config.url === '/admin/models/mdl_two')
      data = { ...model(), id: 'mdl_two', name: 'Other' }
    else if (config.url === '/admin/models/mdl_one/routing-providers')
      data = { items: [{ id: 'prv_new', name: 'Exact supplier' }], next_cursor: null }
    else if (config.url === '/admin/models/mdl_one/routing-candidates')
      data = { items: structuredClone(rows), next_cursor: next }
    else if (config.url === '/admin/models/mdl_one/bindings' && config.method === 'post') {
      if (wait) await wait
      const dispatched = JSON.parse(config.data) as { provider_model_id: string; protocol: string }
      status = fail || 201
      data = {
        ...model(),
        bindings: [
          ...model().bindings,
          {
            id: 'bnd_new',
            provider_model_id: dispatched.provider_model_id,
            provider_id: 'prv_new',
            connection_id: 'con_new',
            upstream_name: 'literal%_upstream',
            protocol: dispatched.protocol,
            weight: 0,
            ready: true,
          },
        ],
      }
    } else throw new Error(`Unexpected scoped request ${config.method} ${config.url}`)
    const response = { config, status, statusText: '', headers: new AxiosHeaders(), data }
    if (status >= 400) throw new AxiosError('Controlled failure', '', config, undefined, response)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = adapter
  await i18n.changeLanguage('en')
})
async function until(fn: () => void) {
  let failure: unknown
  for (let i = 0; i < 120; i++) {
    try {
      fn()
      return
    } catch (e) {
      failure = e
    }
    await act(async () => new Promise((r) => setTimeout(r, 5)))
  }
  throw failure
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter initialEntries={['/admin/models/mdl_one']}>
          <Link to="/admin/models/mdl_two">Other target</Link>
          <Routes>
            <Route path="/admin/models/:modelId" element={<AdminModelsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.querySelector('form')).not.toBeNull())
}
function button(text: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === text,
  )!
}
function dialog() {
  return document.querySelector<HTMLElement>('[role="dialog"]')
}
async function open() {
  const btn = host.querySelector<HTMLButtonElement>(
    '[aria-label="Add OpenAI Chat Provider route"]',
  )!
  expect(btn).not.toBeNull()
  await act(async () => btn.click())
  await until(() => expect(dialog()?.querySelector('select[aria-label="Provider"]')).not.toBeNull())
  await until(() =>
    expect([...dialog()!.querySelectorAll('option')].some((o) => o.value === 'prv_new')).toBe(true),
  )
}
async function selectProvider() {
  const el = dialog()!.querySelector<HTMLSelectElement>('select[aria-label="Provider"]')!
  await act(async () => {
    el.value = 'prv_new'
    el.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await until(() => expect(dialog()?.querySelector('table tbody tr')).not.toBeNull())
}
async function choose() {
  await act(async () => dialog()!.querySelector<HTMLInputElement>('input[type="radio"]')!.click())
}
async function submit() {
  await act(async () =>
    dialog()!
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
}
function writes() {
  return requests.filter((r) => r.method === 'post')
}
function deferred() {
  let resolve!: () => void
  const promise = new Promise<void>((r) => (resolve = r))
  return { promise, resolve }
}
describe('protocol-scoped routing candidate composition', () => {
  it('uses recorded Connection facts and optional price denial, inserts zero without rewriting weights', async () => {
    await mount()
    await open()
    await selectProvider()
    expect(dialog()!.textContent).toContain('Recorded Connection')
    expect(dialog()!.textContent).toContain('Price read permission required')
    await choose()
    await submit()
    await until(() => expect(dialog()).toBeNull())
    expect(writes()).toHaveLength(1)
    expect(JSON.parse(writes()[0].data)).toEqual({
      provider_model_id: 'pmd_new',
      protocol: 'openai_chat',
      review_etag: 'a'.repeat(64),
    })
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-one')
    expect(requests.some((r) => r.method === 'put')).toBe(false)
  })
  it('permits candidate review without models.write and blocks insertion', async () => {
    permissions = permissions.filter((p) => p !== 'models.write')
    await mount()
    await open()
    await selectProvider()
    await choose()
    await submit()
    expect(writes()).toHaveLength(0)
    expect(dialog()!.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled).toBe(true)
  })
  it('does not read candidates or providers when independently denied', async () => {
    permissions = permissions.filter((p) => p !== 'providers.read')
    await mount()
    expect(
      host.querySelector<HTMLButtonElement>('[aria-label="Add OpenAI Chat Provider route"]')!
        .disabled,
    ).toBe(true)
    expect(requests.some((r) => r.url?.includes('routing-') || r.url === '/admin/providers')).toBe(
      false,
    )
  })
  it('marks configured but unverified candidates unavailable for selection', async () => {
    rows = [{ ...candidate, verification_covered: false, selectable: false }]
    await mount()
    await open()
    await selectProvider()
    expect(dialog()!.querySelector<HTMLInputElement>('input[type="radio"]')!.disabled).toBe(true)
    expect(dialog()!.textContent).toContain('Not covered')
    expect(dialog()!.textContent).toContain('Available by configuration')
  })
  it('keeps off-page selection and sends the original review proof', async () => {
    rows = Array.from({ length: 20 }, (_, i) => ({ ...candidate, id: `pmd_${i}` }))
    next = 'pmd_19'
    await mount()
    await open()
    await selectProvider()
    await choose()
    rows = [{ ...candidate, id: 'pmd_later' }]
    next = null
    await act(async () => button('Next model page').click())
    await until(() => expect(dialog()!.querySelectorAll('tbody tr')).toHaveLength(1))
    expect(dialog()!.textContent).toContain('literal%_upstream')
    await submit()
    await until(() => expect(writes()).toHaveLength(1))
    expect(JSON.parse(writes()[0].data).provider_model_id).toBe('pmd_0')
  })
  it.each(['session', 'permission', 'target', 'candidates'])(
    'synchronously blocks captured submit on same-turn %s invalidation',
    async (kind) => {
      await mount()
      await open()
      await selectProvider()
      await choose()
      const form = dialog()!.querySelector('form')!
      await act(async () => {
        const q = cache
          .getQueryCache()
          .findAll()
          .find((q) =>
            kind === 'session'
              ? JSON.stringify(q.queryKey) === JSON.stringify(sessionKey)
              : kind === 'permission'
                ? q.queryKey[0] === 'permissions'
                : kind === 'target'
                  ? q.queryKey[2] === 'detail'
                  : q.queryKey[1] === 'model-routing-candidates',
          )!
        expect(q).toBeTruthy()
        void cache.invalidateQueries({ queryKey: q.queryKey, exact: true, refetchType: 'none' })
        form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      })
      expect(writes()).toHaveLength(0)
    },
  )
  it.each([201, 503])(
    'retains immutable uncertain insertion after same-owner Session renewal with late %s',
    async (status) => {
      await mount()
      await open()
      await selectProvider()
      await choose()
      const held = deferred()
      wait = held.promise
      fail = status === 503 ? 503 : 0
      await submit()
      expect(writes()).toHaveLength(1)
      session = { ...session, csrf_token: 'fresh-renewed' }
      await act(async () => cache.refetchQueries({ queryKey: sessionKey, exact: true }))
      await until(() => expect(dialog()?.textContent).toContain('uncertain'))
      held.resolve()
      await act(async () => new Promise((r) => setTimeout(r, 10)))
      expect(dialog()).not.toBeNull()
      wait = null
      fail = 0
      await until(() => expect(dialog()!.querySelector('input[type="radio"]')).not.toBeNull())
      await submit()
      await until(() => expect(writes()).toHaveLength(2))
      expect(writes()[1].data).toBe(writes()[0].data)
      expect(writes()[1].headers.get('X-CSRF-Token')).toBe('fresh-renewed')
    },
  )
  it('retains 409 intent through explicit review and GET cannot report insertion success', async () => {
    fail = 409
    await mount()
    await open()
    await selectProvider()
    await choose()
    await submit()
    await until(() => expect(dialog()!.textContent).toContain('uncertain'))
    await act(async () => button('Review current candidates').click())
    await until(() => expect(dialog()!.querySelector('input[type="radio"]')).not.toBeNull())
    expect(dialog()).not.toBeNull()
    expect(writes()).toHaveLength(1)
    fail = 0
    await submit()
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].data).toBe(writes()[0].data)
  })
  it.each(['actor', 'target', 'unmount'])(
    'discards held obsolete success after %s destruction',
    async (kind) => {
      await mount()
      await open()
      await selectProvider()
      await choose()
      const held = deferred()
      wait = held.promise
      await submit()
      await act(async () => {
        if (kind === 'actor') {
          session = { ...session, user: { ...session.user, id: 'usr_other' } }
          await cache.refetchQueries({ queryKey: sessionKey, exact: true })
        } else if (kind === 'target') host.querySelector<HTMLAnchorElement>('a')!.click()
        else root.render(null)
      })
      held.resolve()
      await act(async () => new Promise((r) => setTimeout(r, 10)))
      expect(dialog()).toBeNull()
      expect(writes()).toHaveLength(1)
    },
  )
  it('updates EN/ZH copy without changing selected draft', async () => {
    await mount()
    await open()
    await selectProvider()
    await choose()
    await act(async () => i18n.changeLanguage('zh'))
    expect(dialog()!.textContent).toContain('已记录')
    expect(dialog()!.querySelector<HTMLInputElement>('input[type="radio"]')!.checked).toBe(true)
    expect(dialog()!.textContent).toContain('Recorded Connection')
  })
  it.each(['permission', 'target', 'candidates'])(
    'preserves held insertion as uncertain after renewed %s read',
    async (kind) => {
      await mount()
      await open()
      await selectProvider()
      await choose()
      const held = deferred()
      wait = held.promise
      await submit()
      const query = cache
        .getQueryCache()
        .findAll()
        .find((q) =>
          kind === 'permission'
            ? q.queryKey[0] === 'permissions'
            : kind === 'target'
              ? q.queryKey[2] === 'detail'
              : q.queryKey[1] === 'model-routing-candidates',
        )!
      await act(async () => {
        await cache.refetchQueries({ queryKey: query.queryKey, exact: true })
      })
      held.resolve()
      await until(() => expect(dialog()?.textContent).toContain('uncertain'))
      expect(writes()).toHaveLength(1)
    },
  )
  it('hides recorded candidate fields during permission invalidation and never resurrects them on denied reads', async () => {
    await mount()
    await open()
    await selectProvider()
    expect(dialog()!.textContent).toContain('Recorded Connection')
    permissions = ['models.read_all']
    const query = cache
      .getQueryCache()
      .findAll()
      .find((q) => q.queryKey[0] === 'permissions')!
    await act(async () => {
      await cache.invalidateQueries({ queryKey: query.queryKey, exact: true })
    })
    await until(() => expect(dialog()).toBeNull())
    expect(document.body.textContent).not.toContain('Recorded Connection')
    expect(writes()).toHaveLength(0)
  })
})
