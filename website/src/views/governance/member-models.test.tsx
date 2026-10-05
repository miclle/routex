import { accessSummaryFixture } from './member-access-summary.fixture'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import Members from './members'
import {
  modelsFixture,
  modelsActor,
  modelsTarget,
  modelRow,
  firstModel,
  secondModel,
} from './member-models.fixture'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>,
  calls: InternalAxiosRequestConfig[],
  permissions: string[],
  actor: string,
  csrf: string,
  page: ReturnType<typeof modelsFixture>,
  putStatus: number,
  getStatus: number,
  subjectDisabled: boolean
let sessionGate: ReturnType<typeof gate> | null,
  putGate: ReturnType<typeof gate> | null,
  listGate: ReturnType<typeof gate> | null,
  gates: ReturnType<typeof gate>[]
const original = client.defaults.adapter
function gate(): { release: () => void; promise: Promise<void> } {
  let release!: () => void
  const promise = new Promise<void>((r) => {
    release = r
  })
  const g = { release, promise }
  gates.push(g)
  return g
}
function fail(c: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('Controlled failure', '', c, undefined, {
    config: c,
    data: { message: 'Controlled failure' },
    headers: new AxiosHeaders(),
    status,
    statusText: '',
  })
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  calls = []
  permissions = ['members.read', 'members.models.write', 'providers.read', 'prices.read']
  actor = modelsActor
  csrf = 'csrf-first'
  page = modelsFixture()
  putStatus = getStatus = 0
  subjectDisabled = false
  sessionGate = putGate = listGate = null
  gates = []
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.defaults.adapter = async (c) => {
    calls.push(c)
    if (c.method === 'get' && c.url?.endsWith('/access')) {
      const data = accessSummaryFixture(c.url.split('/')[3], {
        roles: permissions.includes('roles.read'),
        teams: permissions.includes('teams.read_all'),
      })
      if (data.roles.status === 'available') data.roles.items = []
      if (data.teams.status === 'available') data.teams.items = []
      return {
        config: c,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders({ 'cache-control': 'private, no-store' }),
        data,
      }
    }
    let data: unknown,
      etag = page.etag
    if (c.url === '/auth/session') {
      data = { user: { id: actor, name: 'Reader', role: 'admin' }, csrf_token: csrf }
      if (sessionGate) await sessionGate.promise
    } else if (c.url === '/auth/permissions') data = { permissions: [...permissions] }
    else if (/^\/admin\/members\/[^/]+$/.test(c.url ?? ''))
      data = {
        id: c.url!.split('/')[3],
        name: 'Target',
        email: 'target@example.invalid',
        role: 'member',
        role_ids: [],
        last_login_at: null,
        last_login_status: 'historical_unavailable',
        disabled: subjectDisabled,
        offboarded_at: null,
        created_at: '2026-10-04T00:00:00Z',
      }
    else if (c.url?.endsWith('/models')) {
      if (c.method === 'put') {
        if (putGate) await putGate.promise
        if (putStatus) throw fail(c, putStatus)
        const body = JSON.parse(c.data)
        etag = 'b'.repeat(64)
        page = {
          ...page,
          etag,
          personal_models: body.model_ids.map((id: string) => ({
            ...modelRow(id, id === firstModel ? 'recorded-model' : 'available-model'),
            selectable: false,
          })),
          available_models: [],
        }
        data = {
          user_id: modelsTarget,
          model_ids: body.model_ids,
          etag,
          runtime_applied: true,
          confirmation: 'current_model_grants',
        }
      } else {
        data = structuredClone(page)
        if (listGate) await listGate.promise
        if (getStatus) throw fail(c, getStatus)
      }
    } else if (c.url?.endsWith('/model-requests')) data = { items: [], total: 0, next_cursor: null }
    else throw new Error(`Unexpected endpoint ${c.url}`)
    return {
      config: c,
      data,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ etag: `"${etag}"` }),
    }
  }
  router = createMemoryRouter([{ path: '/admin/members/:memberId', element: <Members /> }], {
    initialEntries: [`/admin/members/${modelsTarget}?tab=models`],
  })
})
afterEach(async () => {
  gates.forEach((g) => g.release())
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
})
async function settle() {
  for (let i = 0; i < 8; i++)
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await settle()
}
function button(label: string) {
  return Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find(
    (b) => b.textContent?.trim() === label,
  )!
}
async function click(el: Element) {
  expect(el).toBeTruthy()
  await act(async () => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })
  await settle()
}
async function reason(v: string) {
  const el = document.querySelector<HTMLTextAreaElement>('[role="dialog"] textarea')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(el, v)
    el.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function review() {
  await click(button('Add'))
  await click(button('Save Personal grants'))
  await reason('Reviewed grant set')
}
const puts = () => calls.filter((c) => c.method === 'put')
it('reproduces two compact nine-cell tables, exact prices, unknown declarations and separate request history without directories', async () => {
  await render()
  expect(host.querySelectorAll('table')).toHaveLength(3)
  expect(host.querySelector('table')!.querySelectorAll('th')).toHaveLength(10)
  expect(host.textContent).toContain('0 USD / 1M Tokens')
  expect(host.textContent).toContain('9007199254740993.000000000000000001 EUR')
  expect(host.textContent).toContain('Personal authorized models (1)')
  expect(calls.filter((c) => c.url === '/auth/session')).toHaveLength(1)
  expect(calls.some((c) => c.url?.includes('/providers') || c.url === '/models')).toBe(false)
  expect(puts()).toHaveLength(0)
})
it('local Add/Remove only dispatch after explicit reviewed reason confirmation', async () => {
  await render()
  await review()
  expect(puts()).toHaveLength(0)
  await click(button('Confirm Save'))
  expect(puts()).toHaveLength(1)
  expect(JSON.parse(puts()[0].data)).toEqual({
    model_ids: [firstModel, secondModel],
    reason: 'Reviewed grant set',
  })
  expect(puts()[0].headers['If-Match']).toBe(`"${'a'.repeat(64)}"`)
  expect(host.textContent).toContain('runtime application is confirmed')
})
it('independent readonly metadata has no draft actions or reviewer reads', async () => {
  permissions = ['members.read']
  page.can_edit = false
  page.available_models = []
  await render()
  expect(host.textContent).toContain('recorded-model')
  expect(button('Remove')).toBeUndefined()
  expect(calls.some((c) => c.url?.endsWith('/model-requests'))).toBe(false)
})
it('write alone cannot borrow fresh Member page read authority', async () => {
  permissions = ['members.models.write']
  await render()
  expect(calls.some((c) => c.url?.endsWith('/models'))).toBe(false)
  expect(host.textContent).not.toContain('recorded-model')
})
it('preserves local draft/reason on live language change without dispatch', async () => {
  await render()
  await review()
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.querySelector('textarea')!.value).toBe('Reviewed grant set')
  expect(button('确认保存')).toBeTruthy()
  expect(puts()).toHaveLength(0)
})
it('requires explicit latest review after configuration conflict without changing draft', async () => {
  await render()
  await click(button('Add'))
  page.etag = 'c'.repeat(64)
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'member-models'] })
  })
  await settle()
  await click(button('Save Personal grants'))
  expect(button('Confirm Save').disabled).toBe(true)
  await click(button('Review latest configuration'))
  await reason('Current review')
  await click(button('Confirm Save'))
  expect(puts()[0].headers['If-Match']).toBe(`"${'c'.repeat(64)}"`)
})
it('retains exact uncertain request across same-ms real Session renewal and rejected retry, using current CSRF without autoPUT', async () => {
  vi.spyOn(Date, 'now').mockReturnValue(1791158400000)
  await render()
  await review()
  putStatus = 503
  await click(button('Confirm Save'))
  const body = puts()[0].data,
    etag = puts()[0].headers['If-Match']
  sessionGate = gate()
  csrf = 'csrf-renewed'
  let renewal!: Promise<unknown>
  await act(async () => {
    renewal = cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true })
  })
  await settle()
  expect(host.textContent).not.toContain('recorded-model')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  await act(async () => {
    sessionGate!.release()
    await renewal
  })
  sessionGate = null
  await settle()
  expect(button('Retry original request')).toBeTruthy()
  expect(document.querySelector('textarea')!.value).toBe('Reviewed grant set')
  putStatus = 409
  await click(button('Retry original request'))
  expect(puts()).toHaveLength(2)
  expect(puts()[1].data).toBe(body)
  expect(puts()[1].headers['If-Match']).toBe(etag)
  expect(puts()[1].headers['X-CSRF-Token']).toBe('csrf-renewed')
  expect(button('Retry original request')).toBeTruthy()
  expect(calls.filter((c) => c.url === '/auth/session')).toHaveLength(2)
})
it('retains immutable intent through incidental workspace outage', async () => {
  await render()
  await review()
  putStatus = 503
  await click(button('Confirm Save'))
  getStatus = 503
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'member-models'] })
  })
  await settle()
  expect(host.textContent).not.toContain('recorded-model')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  getStatus = 0
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['admin', 'member-models'] })
  })
  await settle()
  expect(button('Retry original request')).toBeTruthy()
  expect(puts()).toHaveLength(1)
})
it('hides cached options and blocks same-task obsolete Add before paint', async () => {
  await render()
  const add = button('Add')
  await act(async () => {
    cache.setQueryData(['permissions', actor], ['members.read'])
    add.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })
  await settle()
  expect(button('Save Personal grants')).toBeUndefined()
  expect(puts()).toHaveLength(0)
})
it('discards late mutation after authority loss, retains uncertain retry only after fresh scope returns', async () => {
  await render()
  await review()
  putGate = gate()
  await click(button('Confirm Save'))
  await act(async () => {
    cache.setQueryData(['permissions', actor], ['members.read'])
  })
  await settle()
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  await act(async () => {
    putGate!.release()
  })
  await settle()
  expect(host.textContent).not.toContain('The selected Personal grants are current')
  expect(puts()).toHaveLength(1)
})
it('actor and target changes destroy original private intent and draft', async () => {
  await render()
  await review()
  putStatus = 503
  await click(button('Confirm Save'))
  actor = 'usr_other'
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true })
  })
  await settle()
  expect(button('Retry original request')).toBeUndefined()
  await act(async () => router.navigate('/admin/members/usr_another?tab=models'))
  await settle()
  expect(button('Retry original request')).toBeUndefined()
  expect(puts()).toHaveLength(1)
})
it('leaving Models destroys draft, preserves existing Keys/Teams/Limits mounting ownership', async () => {
  await render()
  await review()
  await act(async () => router.navigate(`/admin/members/${modelsTarget}?tab=settings`))
  await settle()
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  await act(async () => router.navigate(`/admin/members/${modelsTarget}?tab=models`))
  await settle()
  expect(button('Save Personal grants')).toBeUndefined()
  expect(puts()).toHaveLength(0)
})

it('local Remove can be reversed by Add without writing or losing its original grant', async () => {
  await render()
  await click(button('Remove'))
  const row = Array.from(host.querySelectorAll('tr')).find((r) =>
    r.textContent?.includes(firstModel),
  )!
  await click(row.querySelector('button')!)
  expect(host.textContent).toContain('Personal authorized models (1)')
  expect(button('Save Personal grants')).toBeUndefined()
  expect(puts()).toHaveLength(0)
})
it('Provider and price privileges remain independent without any directory reads', async () => {
  page.personal_models[0].providers = ['Recorded Provider']
  permissions = ['members.read']
  page.can_edit = false
  page.available_models = []
  await render()
  expect(host.textContent).not.toContain('Recorded Provider')
  expect(host.textContent).not.toContain('9007199254740993')
  expect(calls.some((c) => c.url?.includes('/providers'))).toBe(false)
})
it('unknown runtime, missing and heterogeneous rates never display invented zero or declaration', async () => {
  page.runtime_applied = null
  page.application_status = 'unavailable'
  page.personal_models[0].input_price = { state: 'missing', rate: null }
  page.personal_models[0].output_price = { state: 'heterogeneous', rate: null }
  page.available_models[0].availability = 'unknown'
  page.available_models[0].protocols = []
  page.available_models[0].selectable = false
  await render()
  expect(host.textContent).toContain('Multiple schedules')
  expect(host.textContent).toContain('Runtime publication is unavailable')
  expect(button('Add').disabled).toBe(true)
  expect(host.querySelector('table')?.textContent).not.toContain('0 USD')
  expect(puts()).toHaveLength(0)
})
it('an obsolete list response cannot restore old private metadata after target change', async () => {
  listGate = gate()
  await render()
  const pending = listGate
  await act(async () => router.navigate('/admin/members/usr_new_target?tab=models'))
  pending.release()
  listGate = null
  await settle()
  expect(host.textContent).not.toContain('recorded-model')
  expect(puts()).toHaveLength(0)
})

it('reason byte validation retains the exact visible draft and accepts the bounded Unicode reason', async () => {
  await render()
  await review()
  const tooLong = '界'.repeat(342)
  await reason(tooLong)
  await click(button('Confirm Save'))
  expect(puts()).toHaveLength(0)
  expect(document.querySelector('textarea')!.value).toBe(tooLong)
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain('1024 UTF-8 bytes')
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.querySelector('textarea')!.value).toBe(tooLong)
  await act(async () => {
    await i18n.changeLanguage('en')
  })
  const valid = '界'.repeat(341)
  await reason(valid)
  await click(button('Confirm Save'))
  expect(puts()).toHaveLength(1)
  expect(JSON.parse(puts()[0].data).reason).toBe(valid)
})
