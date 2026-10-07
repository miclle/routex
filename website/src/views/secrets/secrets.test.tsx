import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { SecretIntent, SecretStore } from '@/types/secrets'
import SecretStorePage from './index'
import { job, receipt, rotationId, store } from './fixtures'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>,
  oldAdapter: typeof client.defaults.adapter
let session: Session,
  permissions: string[],
  view: SecretStore,
  reads: string[],
  writes: { config: InternalAxiosRequestConfig; input: SecretIntent['input'] }[]
let readGate: ReturnType<typeof deferred<void>> | undefined,
  writeGate: ReturnType<typeof deferred<void>> | undefined,
  postStatus: number,
  getStatus: number
beforeEach(async () => {
  vi.spyOn(Date, 'now').mockReturnValue(1_800_000_000_000)
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  session = {
    user: { id: 'usr_admin', name: 'Admin', email: 'dummy@example.test', role: 'admin' },
    csrf_token: 'b'.repeat(64),
  }
  permissions = ['secrets.read', 'secrets.rotate']
  view = store()
  reads = []
  writes = []
  postStatus = 201
  getStatus = 200
  readGate = undefined
  writeGate = undefined
  oldAdapter = client.defaults.adapter
  client.defaults.adapter = async (config) => {
    const path = config.url!
    const response = (data: unknown, status = 200) => ({
      data,
      status,
      statusText: String(status),
      headers: {},
      config,
    })
    const error = (status: number) =>
      new AxiosError(
        'Sanitized failure',
        undefined,
        config,
        undefined,
        response({ error: 'not exposed' }, status),
      )
    if (config.method === 'get') {
      reads.push(path)
      if (path === '/auth/session') return response(structuredClone(session))
      if (path === '/auth/permissions') return response({ permissions: [...permissions] })
      if (path.startsWith('/admin/secrets')) {
        const saved = structuredClone(view),
          status = getStatus,
          gate = readGate
        if (gate) await gate.promise
        if (status !== 200) throw error(status)
        return response(saved)
      }
    }
    if (config.method === 'post' && path.startsWith('/admin/secrets/rotations')) {
      const input = JSON.parse(config.data) as SecretIntent['input']
      writes.push({ config, input })
      const action =
        path === '/admin/secrets/rotations'
          ? 'start'
          : (path.split('/').at(-1) as SecretIntent['action'])
      const intent: SecretIntent = {
        action,
        input,
        etag: String(config.headers.get('If-Match')).slice(1, -1),
        rotationId: action === 'start' ? undefined : path.split('/').at(-2),
      }
      const status = postStatus,
        gate = writeGate
      if (gate) await gate.promise
      if (status !== 200 && status !== 201) throw error(status)
      return response(receipt(intent), status)
    }
    throw new Error(`Unexpected endpoint ${path}`)
  }
  router = createMemoryRouter(
    [
      { path: '/admin/secrets', element: <SecretStorePage /> },
      { path: '/admin/secrets/rotations/:rotationId', element: <SecretStorePage /> },
    ],
    { initialEntries: ['/admin/secrets'] },
  )
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = oldAdapter
  vi.restoreAllMocks()
})
async function settle() {
  await act(async () => {
    await new Promise((done) => setTimeout(done, 10))
  })
}
async function mount(path?: string) {
  if (path) await router.navigate(path)
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await settle()
  await settle()
}
function button(label: string) {
  const element = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
  expect(element, label).toBeDefined()
  return element!
}
async function click(label: string) {
  await act(async () => button(label).click())
  await settle()
}
async function reason(value = 'Reviewed safe rotation') {
  const input = document.querySelector<HTMLInputElement>('input')!
  expect(input).not.toBeNull()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function select() {
  const input = document.querySelector('select')!
  await act(async () => {
    input.value = 'next'
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function prepare() {
  await click('Root key rotation')
  await select()
  await reason()
  await click('Start rotation')
  expect(document.body.textContent).toContain('Confirm root key action')
}
async function renew() {
  const old = cache.getQueryState(sessionKey)!
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await settle()
  const next = cache.getQueryState(sessionKey)!
  expect(next.data).toBe(old.data)
  expect(next.dataUpdatedAt).toBe(old.dataUpdatedAt)
  expect(next.dataUpdateCount).toBe(old.dataUpdateCount + 1)
}

it('renders the existing internal card and provisioned targets without root material or unsupported choices', async () => {
  await mount()
  expect(reads.filter((path) => path === '/auth/session')).toHaveLength(1)
  expect(host.textContent).toContain('9007199254740993')
  expect(host.textContent).toContain('Current write key')
  await click('Root key rotation')
  expect([...document.querySelectorAll('option')].map((option) => option.value)).toEqual([
    '',
    'next',
  ])
  expect(document.querySelector('input[type="password"]')).toBeNull()
  expect(document.body.textContent).toContain('Vault integration')
  expect(reads.some((path) => path.includes('/integrations'))).toBe(false)
  expect(writes).toHaveLength(0)
})
it('submits once after explicit confirmation with current manually replaced CSRF and keeps historical receipt separate', async () => {
  await mount()
  await prepare()
  await act(async () => {
    cache.setQueryData(sessionKey, { ...session, csrf_token: 'c'.repeat(64) })
  })
  await settle()
  const gate = (writeGate = deferred<void>())
  await act(async () => {
    button('Confirm action').click()
    button('Confirm action').click()
  })
  await settle()
  expect(writes).toHaveLength(1)
  expect(writes[0].config.headers.get('X-CSRF-Token')).toBe('c'.repeat(64))
  expect(writes[0].config.headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
  view = {
    ...store(),
    rotation: job(),
    policy: { write_key_id: 'next', epoch: '9007199254740994' },
    process: { ...store().process, policy_epoch: '9007199254740994' },
  }
  await act(async () => gate.resolve())
  await settle()
  await settle()
  expect(document.body.textContent).toContain('Historical committed receipt')
  expect(document.body.textContent).toContain('Review fresh status for current write policy')
  expect(document.body.textContent).not.toContain('currently applied')
  expect(writes[0].config.headers.has('Authorization')).toBe(false)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it('identical same-millisecond network renewal hides status and aborts an in-flight intent without automatic replay', async () => {
  await mount()
  await prepare()
  const gate = (writeGate = deferred<void>())
  await click('Confirm action')
  const signal = writes[0].config.signal as AbortSignal
  readGate = deferred<void>()
  await renew()
  expect(signal.aborted).toBe(true)
  expect(document.body.textContent).not.toContain('9007199254740993')
  expect(document.body.textContent).not.toContain('next')
  expect(writes).toHaveLength(1)
  await act(async () => gate.resolve())
  await settle()
  expect(document.body.textContent).not.toContain('Historical committed receipt')
  await act(async () => readGate!.resolve())
  readGate = undefined
  await settle()
  await settle()
  expect(document.body.textContent).toContain('The outcome is unknown')
  expect(button('Retry original intent').disabled).toBe(false)
  expect(reads.filter((path) => path === '/auth/session')).toHaveLength(2)
})
it('a fast identical renewal reads a new scoped view even with structural sharing and no fetching render', async () => {
  await mount()
  await click('Root key rotation')
  await reason('Keep unsent reason')
  const count = reads.filter((path) => path === '/admin/secrets').length
  await renew()
  await settle()
  expect(reads.filter((path) => path === '/admin/secrets')).toHaveLength(count + 1)
  expect(document.querySelector('input')!.value).toBe('Keep unsent reason')
  expect(reads.filter((path) => path === '/auth/session')).toHaveLength(2)
})
it('unknown retry retains the original UUID/body/ETag through 409, changed state, forbidden read and restoration', async () => {
  await mount()
  postStatus = 503
  await prepare()
  await click('Confirm action')
  const original = structuredClone(writes[0].input)
  const header = writes[0].config.headers.get('If-Match')
  expect(document.body.textContent).toContain('The outcome is unknown')
  getStatus = 403
  await click('Refresh status')
  expect(document.body.textContent).not.toContain('Retry original intent')
  expect(document.body.textContent).not.toContain(original.reason)
  getStatus = 200
  view = {
    ...store(),
    review_etag: 'd'.repeat(64),
    rotation: { ...job(), status: 'ready', allowed_actions: ['retire'] },
  }
  await click('Refresh status')
  expect(document.body.textContent).toContain('The outcome is unknown')
  const captured = document.querySelector('[aria-label="Original uncertain intent"]')!
  expect(captured.textContent).toContain(original.request_id)
  expect(captured.textContent).toContain('a'.repeat(64))
  postStatus = 409
  await click('Retry original intent')
  expect(document.body.textContent).toContain('original retry was rejected')
  expect(writes[1].input).toEqual(original)
  expect(writes[1].config.headers.get('If-Match')).toBe(header)
  postStatus = 200
  await click('Retry original intent')
  expect(writes[2].input).toEqual(original)
  expect(writes[2].config.headers.get('If-Match')).toBe(header)
  expect(document.body.textContent).toContain('Historical committed receipt')
})
it('known conflicts retain the draft but require explicit review before a new UUID', async () => {
  await mount()
  postStatus = 409
  await prepare()
  await click('Confirm action')
  expect(document.querySelector('input')!.value).toBe('Reviewed safe rotation')
  expect(document.body.textContent).toContain('reviewed status changed')
  view.review_etag = 'd'.repeat(64)
  await click('Refresh status')
  expect(button('Start rotation').disabled).toBe(true)
  await click('Review current status')
  await click('Start rotation')
  postStatus = 201
  await click('Confirm action')
  expect(writes).toHaveLength(2)
  expect(writes[1].input.request_id).not.toBe(writes[0].input.request_id)
  expect(writes[1].config.headers.get('If-Match')).toBe(`"${'d'.repeat(64)}"`)
})
it.each(['resume', 'retire', 'rollback'] as const)(
  'addressed %s uses only the server allowed action and exact target',
  async (action) => {
    view.rotation = {
      ...job(),
      status: action === 'retire' ? 'ready' : 'blocked',
      allowed_actions: [action],
      blocker_codes: ['future_blocker'],
    }
    await mount(`/admin/secrets/rotations/${rotationId}`)
    await click('Root key rotation')
    expect(document.body.textContent).toContain('A server prerequisite is not satisfied')
    expect(reads).toContain(`/admin/secrets/rotations/${rotationId}`)
    expect(document.body.textContent).toContain('recorded work counts')
    await reason()
    await click(
      {
        resume: 'Resume migration',
        retire: 'Retire source key',
        rollback: 'Roll back write policy',
      }[action],
    )
    postStatus = 200
    await click('Confirm action')
    expect(writes).toHaveLength(1)
    expect(writes[0].config.url).toBe(`/admin/secrets/rotations/${rotationId}/${action}`)
    expect(writes[0].input).not.toHaveProperty('target_key_id')
  },
)
it('read-only authority exposes status without mutation and missing administrator/read authority never fetches private status', async () => {
  permissions = ['secrets.read']
  view.can_rotate = false
  await mount()
  await click('Root key rotation')
  expect(document.body.textContent).toContain('Rotation permission is required')
  expect(button('Start rotation').disabled).toBe(true)
  permissions = ['secrets.rotate']
  await renew()
  expect(host.textContent).not.toContain('9007199254740993')
  expect(document.body.textContent).not.toContain('Reason')
  expect(reads.filter((path) => path === '/admin/secrets')).toHaveLength(1)
})
it('actor change and addressable target change cannot restore late private responses or receipts', async () => {
  await mount()
  await prepare()
  writeGate = deferred<void>()
  await click('Confirm action')
  const signal = writes[0].config.signal as AbortSignal
  await act(async () => {
    cache.setQueryData(sessionKey, { ...session, user: { ...session.user, id: 'usr_other' } })
  })
  await settle()
  expect(signal.aborted).toBe(true)
  await act(async () => writeGate!.resolve())
  await settle()
  expect(document.body.textContent).not.toContain('Historical committed receipt')
  view.rotation = { ...job(), id: 'srt_01k00000000000000000000001' }
  await act(async () => router.navigate(`/admin/secrets/rotations/${view.rotation!.id}`))
  await settle()
  expect(document.querySelector('input')).toBeNull()
  await click('Root key rotation')
  expect(document.querySelector('input')!.value).toBe('')
})
it('refresh errors hide key IDs, recorded proof, receipts and mutation controls', async () => {
  await mount()
  await prepare()
  await click('Confirm action')
  expect(document.body.textContent).toContain('Historical committed receipt')
  getStatus = 503
  await click('Refresh status')
  expect(document.body.textContent).not.toContain('Historical committed receipt')
  expect(document.querySelector('select')).toBeNull()
  expect(document.body.textContent).not.toContain('9007199254740993')
})
it('live language switching updates existing dialog and unknown guidance without clearing intent', async () => {
  await mount()
  postStatus = 503
  await prepare()
  await click('Confirm action')
  const original = structuredClone(writes[0].input)
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.body.textContent).toContain('操作结果未知')
  expect(button('重试原始意图')).toBeDefined()
  postStatus = 200
  await click('重试原始意图')
  expect(writes[1].input).toEqual(original)
  expect(document.body.textContent).toContain('历史提交回执')
})

it('closing and reopening an unknown intent cannot replace its captured action', async () => {
  await mount()
  postStatus = 503
  await prepare()
  await click('Confirm action')
  const original = structuredClone(writes[0].input)
  await click('Close')
  await click('Root key rotation')
  expect(document.querySelector('input')).toBeNull()
  expect(document.body.textContent).toContain('The outcome is unknown')
  postStatus = 200
  await click('Retry original intent')
  expect(writes[1].input).toEqual(original)
})
it('manual rotate-permission revocation aborts an in-flight action and restored authority reconciles the original intent', async () => {
  await mount()
  await prepare()
  const gate = (writeGate = deferred<void>())
  await click('Confirm action')
  const original = structuredClone(writes[0].input)
  const key = cache
    .getQueryCache()
    .getAll()
    .find((query) => query.queryKey[0] === 'permissions')!.queryKey
  await act(async () => {
    cache.setQueryData(key, ['secrets.read'])
  })
  await settle()
  expect(writes[0].config.signal!.aborted).toBe(true)
  expect(button('Submitting…').disabled).toBe(true)
  await act(async () => gate.resolve())
  writeGate = undefined
  await settle()
  expect(document.body.textContent).not.toContain('Historical committed receipt')
  expect(button('Retry original intent').disabled).toBe(true)
  await act(async () => {
    cache.setQueryData(key, ['secrets.read', 'secrets.rotate'])
  })
  await settle()
  postStatus = 200
  await click('Retry original intent')
  expect(writes[1].input).toEqual(original)
})
it('a missing current CSRF hides facts and aborts dispatch rather than using captured token authority', async () => {
  await mount()
  await prepare()
  const gate = (writeGate = deferred<void>())
  await click('Confirm action')
  await act(async () => {
    cache.setQueryData(sessionKey, { ...session, csrf_token: '' })
  })
  await settle()
  expect(writes[0].config.signal!.aborted).toBe(true)
  expect(document.body.textContent).not.toContain('9007199254740993')
  await act(async () => gate.resolve())
  await settle()
  expect(document.body.textContent).not.toContain('Historical committed receipt')
})
it('an old actor GET response cannot replace a new actor error with private status', async () => {
  await mount()
  readGate = deferred<void>()
  await click('Refresh status')
  const gate = readGate
  permissions = []
  await act(async () => {
    cache.setQueryData(sessionKey, { ...session, user: { ...session.user, id: 'usr_new' } })
  })
  await settle()
  await act(async () => gate.resolve())
  await settle()
  expect(host.textContent).not.toContain('9007199254740993')
  expect(host.textContent).not.toContain('instance_current')
  expect(writes).toHaveLength(0)
})
it('a captured receipt survives fresh current completed/retired status without asserting current application', async () => {
  await mount()
  await prepare()
  await click('Confirm action')
  view = {
    ...store(),
    policy: { write_key_id: 'next', epoch: '9007199254740994' },
    process: { ...store().process, policy_epoch: '9007199254740994' },
    rotation: { ...job(), status: 'completed', phase: 'completed', allowed_actions: [] },
    keys: [
      { id: 'old', state: 'retired', configured: false, verified: false },
      { id: 'next', state: 'write', configured: true, verified: true },
    ],
  }
  await click('Refresh status')
  expect(document.body.textContent).toContain('Historical committed receipt')
  expect(document.body.textContent).toContain('Application recorded at response')
  expect(document.body.textContent).not.toContain('currently applied')
  expect(document.body.textContent).toContain('Retired')
  expect(document.body.textContent).toContain('Completed')
})
it('a member with a listed permission never reads internal administrator status', async () => {
  session.user.role = 'member'
  await mount()
  expect(host.textContent).toContain('Administrator access')
  expect(reads).toEqual(['/auth/session'])
  expect(writes).toHaveLength(0)
})
it('a delayed previous-generation read cannot replace a newly authorized identical-actor response', async () => {
  await mount()
  readGate = deferred<void>()
  const old = readGate
  await click('Refresh status')
  readGate = undefined
  view = {
    ...store(),
    policy: { write_key_id: 'next', epoch: '9007199254740994' },
    process: { ...store().process, policy_epoch: '9007199254740994' },
  }
  await renew()
  await settle()
  expect(host.textContent).toContain('9007199254740994')
  expect(host.textContent).not.toContain('9007199254740993')
  await act(async () => old.resolve())
  await settle()
  expect(host.textContent).toContain('9007199254740994')
  expect(host.textContent).not.toContain('9007199254740993')
  expect(writes).toHaveLength(0)
})
it('a delayed addressed rotation read cannot restore the prior target after navigation', async () => {
  view.rotation = job()
  await mount(`/admin/secrets/rotations/${rotationId}`)
  readGate = deferred<void>()
  const old = readGate
  await click('Refresh status')
  readGate = undefined
  const next = 'srt_01k00000000000000000000001'
  view = { ...store(), rotation: { ...job(), id: next } }
  await act(async () => router.navigate(`/admin/secrets/rotations/${next}`))
  await settle()
  await click('Root key rotation')
  expect(document.body.textContent).toContain(next)
  expect(document.body.textContent).not.toContain(rotationId)
  await act(async () => old.resolve())
  await settle()
  expect(document.body.textContent).toContain(next)
  expect(document.body.textContent).not.toContain(rotationId)
})
it('explicit dialog review reads the server and adopts its new ETag while preserving the reason draft', async () => {
  await mount()
  await click('Root key rotation')
  await select()
  await reason('Retain reviewed draft')
  const count = reads.filter((path) => path === '/admin/secrets').length
  view.review_etag = 'd'.repeat(64)
  await click('Review current status')
  expect(reads.filter((path) => path === '/admin/secrets')).toHaveLength(count + 1)
  expect(document.querySelector('input')!.value).toBe('Retain reviewed draft')
  await click('Start rotation')
  await click('Confirm action')
  expect(writes[0].config.headers.get('If-Match')).toBe(`"${'d'.repeat(64)}"`)
})

it.each(['completed', 'rolled_back'] as const)(
  'addressed historical %s job cannot prepare a global cutover',
  async (status) => {
    view.rotation = { ...job(), status, phase: 'completed', allowed_actions: [] }
    await mount(`/admin/secrets/rotations/${rotationId}`)
    await click('Root key rotation')
    expect(document.body.textContent).toContain(rotationId)
    expect(document.querySelector('select')).toBeNull()
    expect(
      [...document.querySelectorAll('button')].some(
        (item) => item.textContent === 'Start rotation',
      ),
    ).toBe(false)
    expect(writes).toHaveLength(0)
    await act(async () => router.navigate('/admin/secrets'))
    await settle()
    await settle()
    await click('Root key rotation')
    expect(document.querySelector('select')).not.toBeNull()
    expect(button('Start rotation').disabled).toBe(true)
    expect(writes).toHaveLength(0)
  },
)
