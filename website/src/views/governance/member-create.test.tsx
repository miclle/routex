import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { Session } from '@/types/auth'
import MemberCreate from './member-create'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const previousAdapter = client.defaults.adapter
const secret = 'transient-initial-password'
let host: HTMLDivElement, root: Root, cache: QueryClient
let actor: string, ready: boolean, status: number, resultID: string
let requests: InternalAxiosRequestConfig[]
let created: ReturnType<typeof vi.fn<(id: string) => void>>
let closed: ReturnType<typeof vi.fn<() => void>>
let release: () => void, gate: Promise<void>
let unmounted: boolean
function session(id = actor, role: 'admin' | 'member' = 'admin'): Session {
  return {
    user: { id, role, name: 'Manager', email: 'manager@example.invalid' },
    csrf_token: `${id}-csrf`,
  }
}
function seed(id = actor, role: 'admin' | 'member' = 'admin') {
  cache.setQueryData(['auth', 'session'], session(id, role))
  cache.setQueryData(['permissions', id], ['members.read', 'members.write'])
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_admin'
  ready = true
  status = 201
  resultID = 'usr_created'
  requests = []
  created = vi.fn()
  closed = vi.fn()
  unmounted = false
  gate = new Promise<void>((resolve) => {
    release = resolve
  })
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  seed()
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    await gate
    const response = {
      config,
      status,
      statusText: '',
      headers: new AxiosHeaders(),
      data: { id: resultID, password: 'server-must-not-cache-this' },
    }
    if (status !== 201) throw new AxiosError(secret, '', config, undefined, response)
    return response
  }
})
afterEach(async () => {
  await act(async () => {
    release()
  })
  if (!unmounted) await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = previousAdapter
  vi.restoreAllMocks()
  await i18n.changeLanguage('en')
})
async function render() {
  await act(async () => {
    root.render(
      <QueryClientProvider client={cache}>
        <MemberCreate actor={actor} ready={ready} onClose={closed} onCreated={created} />
      </QueryClientProvider>,
    )
  })
}
function field(name: string) {
  return document.querySelector<HTMLInputElement>(`[role="dialog"] input[name="${name}"]`)!
}
function fill(password = secret) {
  field('name').value = 'Created'
  field('email').value = 'created@example.invalid'
  field('password').value = password
}
async function submit() {
  await act(async () => {
    document
      .querySelector('[role="dialog"] form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}
async function settle() {
  await act(async () => release())
}
function cacheHasNoSecret() {
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  const states = cache
    .getQueryCache()
    .getAll()
    .map((query) => query.state)
  expect(JSON.stringify(states)).not.toContain(secret)
  expect(JSON.stringify(states)).not.toContain('server-must-not-cache-this')
}
async function until(check: () => void) {
  await vi.waitFor(async () => act(async () => check()), { timeout: 2000 })
}
it('keeps plaintext out of all mutation variables, results and errors even while pending', async () => {
  const local = vi.spyOn(Storage.prototype, 'setItem')
  await render()
  fill()
  await submit()
  expect(requests).toHaveLength(1)
  expect(JSON.parse(requests[0].data)).toEqual({
    name: 'Created',
    email: 'created@example.invalid',
    password: secret,
    role: 'member',
  })
  expect(requests[0].headers.get('X-CSRF-Token')).toBe('usr_admin-csrf')
  expect(field('password').value).toBe('')
  cacheHasNoSecret()
  expect(local).not.toHaveBeenCalled()
  await settle()
  await until(() => expect(created).toHaveBeenCalledExactlyOnceWith('usr_created'))
  cacheHasNoSecret()
  expect(field('name').value).toBe('')
})
it('locks duplicate submits synchronously and prevents dismissal while pending', async () => {
  await render()
  fill()
  await submit()
  await submit()
  expect(requests).toHaveLength(1)
  await act(async () => document.querySelector<HTMLButtonElement>('[aria-label="Close"]')!.click())
  expect(closed).not.toHaveBeenCalled()
})
it.each([400, 401, 403, 404, 409, 422])(
  'retains only a localized rejection status for HTTP %i and requires a new password',
  async (code) => {
    status = code
    await render()
    fill()
    await submit()
    await settle()
    await until(() =>
      expect(document.querySelector('[role="alert"]')?.textContent).toContain(
        'creation was rejected',
      ),
    )
    cacheHasNoSecret()
    expect(document.querySelector('[role="alert"]')?.textContent).not.toContain(secret)
    expect(field('password').value).toBe('')
    expect(field('name').value).toBe('Created')
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('成员创建被拒绝')
    fill('replacement-initial-password')
    status = 201
    await submit()
    await until(() => expect(created).toHaveBeenCalledExactlyOnceWith('usr_created'))
    expect(requests).toHaveLength(2)
    expect(JSON.parse(requests[1].data).password).toBe('replacement-initial-password')
  },
)
it.each([500, 503])(
  'does not replay an unknown HTTP %i outcome and drops the plaintext',
  async (code) => {
    status = code
    await render()
    fill()
    await submit()
    await settle()
    await until(() =>
      expect(document.querySelector('[role="alert"]')?.textContent).toContain('outcome is unknown'),
    )
    cacheHasNoSecret()
    expect(field('password').value).toBe('')
    await submit()
    expect(requests).toHaveLength(1)
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('创建结果尚不确定')
    await act(async () => document.querySelector<HTMLButtonElement>('[aria-label="关闭"]')!.click())
    expect(closed).toHaveBeenCalledOnce()
  },
)
it('marks malformed success as uncertain without caching arbitrary response data', async () => {
  resultID = '../../other'
  await render()
  fill()
  await submit()
  await settle()
  await until(() =>
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('outcome is unknown'),
  )
  expect(created).not.toHaveBeenCalled()
  cacheHasNoSecret()
})
it('uses fresh independent read/write authority before dispatch', async () => {
  await render()
  fill()
  await act(async () => cache.setQueryData(['permissions', actor], ['members.read']))
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  expect(requests).toHaveLength(0)
  await act(async () =>
    cache.setQueryData(['permissions', actor], ['members.read', 'members.write']),
  )
  await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
  expect(field('password').value).toBe('')
  fill()
  await submit()
  expect(requests).toHaveLength(1)
})
it('keeps dispatched outcome uncertain across same-owner Session renewal and late success', async () => {
  await render()
  fill()
  await submit()
  await act(async () =>
    cache.setQueryData(['auth', 'session'], { ...session(), csrf_token: 'renewed-csrf' }),
  )
  await until(() =>
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('outcome is unknown'),
  )
  expect(field('name').value).toBe('')
  expect(field('password').value).toBe('')
  await settle()
  expect(created).not.toHaveBeenCalled()
  await submit()
  expect(requests).toHaveLength(1)
  cacheHasNoSecret()
})
it('ignores old completion after actor A to B to A without clearing the new draft', async () => {
  await render()
  fill()
  await submit()
  actor = 'usr_other'
  seed()
  await render()
  actor = 'usr_admin'
  seed()
  await render()
  fill('new-opening-password')
  await settle()
  expect(created).not.toHaveBeenCalled()
  expect(field('password').value).toBe('new-opening-password')
  expect(field('name').value).toBe('Created')
  cacheHasNoSecret()
})
it('ignores dispatched completion after unmount without recreating private queries', async () => {
  await render()
  fill()
  await submit()
  await act(async () => root.unmount())
  unmounted = true
  cache.clear()
  await settle()
  expect(created).not.toHaveBeenCalled()
  expect(cache.getQueryCache().getAll()).toHaveLength(0)
  cacheHasNoSecret()
})
it('validates UTF-8 password bytes locally and preserves unsent input during language changes', async () => {
  await render()
  fill('short')
  await submit()
  expect(requests).toHaveLength(0)
  expect(document.querySelector('[role="alert"]')?.textContent).toContain('12–72 UTF-8 bytes')
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.querySelector('[role="alert"]')?.textContent).toContain('12–72 个 UTF-8 字节')
  expect(field('password').value).toBe('short')
  fill('字'.repeat(25))
  await submit()
  expect(requests).toHaveLength(0)
  fill('字'.repeat(4))
  await submit()
  expect(requests).toHaveLength(1)
})
it('does not offer or submit administrator creation to delegated member managers', async () => {
  seed(actor, 'member')
  await render()
  expect(document.querySelector('[role="dialog"] select')).toBeNull()
  fill()
  await submit()
  expect(JSON.parse(requests[0].data).role).toBe('member')
})
it('treats Session removal and equal-count restoration as a lost authority lifetime', async () => {
  await render()
  fill()
  await submit()
  const original = cache.getQueryData<Session>(['auth', 'session'])!
  await act(async () => {
    cache.removeQueries({ queryKey: ['auth', 'session'], exact: true })
    cache.setQueryData(['auth', 'session'], original)
  })
  await settle()
  await until(() =>
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('outcome is unknown'),
  )
  expect(created).not.toHaveBeenCalled()
  cacheHasNoSecret()
})
it('clears sensitive unsent input on missing Session and never dispatches under restored stale authority', async () => {
  await render()
  fill()
  await act(async () => cache.removeQueries({ queryKey: ['auth', 'session'], exact: true }))
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  expect(requests).toHaveLength(0)
  await act(async () => seed())
  await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
  expect(field('password').value).toBe('')
})
it('clears dispatched state on permission renewal and ignores a late rejected response', async () => {
  status = 409
  await render()
  fill()
  await submit()
  await act(async () => cache.setQueryData(['permissions', actor], ['members.read']))
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  await act(async () =>
    cache.setQueryData(['permissions', actor], ['members.read', 'members.write']),
  )
  await until(() =>
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('outcome is unknown'),
  )
  await settle()
  expect(document.querySelector('[role="alert"]')?.textContent).toContain('outcome is unknown')
  await submit()
  expect(requests).toHaveLength(1)
  expect(created).not.toHaveBeenCalled()
})

it('clears detached sensitive form nodes on dismissal/unmount', async () => {
  await render()
  fill()
  const password = field('password')
  await act(async () => root.unmount())
  unmounted = true
  expect(password.value).toBe('')
  expect(requests).toHaveLength(0)
})
