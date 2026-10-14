import { act, useLayoutEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Session } from '@/types/auth'
import type { DeliveryAuthority } from '@/views/key-delivery-admin/lifetime'
import type { DeliveryControllerDetail } from '@/types/key-delivery-controller'
import {
  controllerPrefix,
  useControllerLifetime,
  type ControllerLifetime,
  type ControllerIntent,
} from './lifetime'
const api = vi.hoisted(() => ({
  enrollController: vi.fn(),
  assignController: vi.fn(),
  revokeController: vi.fn(),
  getControllerEnrollmentReceipt: vi.fn(),
}))
vi.mock('@/api/key-delivery-controller', () => ({
  ...api,
  DeliveryControllerError: class extends Error {
    constructor(public status: number) {
      super('Unavailable')
    }
  },
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const hash = 'a'.repeat(64),
  slot = 'kds_01arz3ndektsv4rrffq69g5fav',
  target = 'vap_01arz3ndektsv4rrffq69g5fav',
  controller = 'kcg_01arz3ndektsv4rrffq69g5fav',
  bearer = 'rxdctl_' + 'A'.repeat(43)
const body = {
  request_id: '20000000-0000-4000-8000-000000000001',
  name: 'Independent controller',
  expires_at: '2026-10-15T01:00:00Z',
  opt_in: true as const,
  descriptor_revision: '1',
  configuration_digest: hash,
  reason: 'Reviewed exact Slot enrollment',
}
const detail = {
  id: controller,
  name: body.name,
  slot_id: slot,
  owner_kind: 'personal',
  owner_id: 'usr_01arz3ndektsv4rrffq69g5fav',
  expires_at: body.expires_at,
  revoked_at: null,
  assigned_descriptor_revision: '1',
  assigned_configuration_digest: hash,
  current_configuration: null,
  configuration_status: 'unavailable',
  last_acknowledgement: null,
  review_etag: hash,
} satisfies DeliveryControllerDetail
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  authority: DeliveryAuthority,
  selected: string,
  resource: string,
  life: ControllerLifetime
let proofKey: readonly unknown[]
function Harness() {
  const value = useControllerLifetime(authority, selected, resource)
  useLayoutEffect(() => {
    life = value
  }, [value])
  return <p>{value.notice}</p>
}
function session(csrf = 'csrf', actor = 'usr_alice'): Session {
  return {
    user: { id: actor, name: 'Alice', email: 'alice@example.test', role: 'admin' },
    csrf_token: csrf,
  }
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Harness />
      </QueryClientProvider>,
    ),
  )
}
async function flush() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}
function original(): ControllerIntent {
  return {
    kind: 'enroll',
    slot,
    etag: hash,
    input: structuredClone(body),
    proof: { key: proofKey, count: cache.getQueryState(proofKey)!.dataUpdateCount },
  }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}
beforeEach(async () => {
  vi.resetAllMocks()
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  cache.setQueryData(['auth', 'session'], session())
  cache.setQueryData(['permissions', 'usr_alice', 'controller'], ['secrets.read', 'secrets.write'])
  authority = {
    actor: 'usr_alice',
    admin: true,
    fresh: true,
    generation: 1,
    permissionKey: ['permissions', 'usr_alice', 'controller'],
  }
  selected = target
  resource = target
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  await render()
  proofKey = [...controllerPrefix(authority.actor, target), life.generation, 'configuration', slot]
  cache.setQueryData(proofKey, { review_etag: hash })
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  cache.clear()
  vi.useRealTimers()
})
describe('controller dispatch lifetime', () => {
  it('requires explicit prepare and confirmation, keeping bearer outside Query and mutation caches', async () => {
    api.enrollController.mockResolvedValue({
      controller: detail,
      bearer,
      secret_status: 'display_once',
    })
    await act(async () => life.prepare(original()))
    expect(api.enrollController).not.toHaveBeenCalled()
    await act(async () => life.submit())
    expect(life.bearer).toBe(bearer)
    expect(life.notice).toBe('saved')
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((q) => q.state.data),
      ),
    ).not.toContain(bearer)
  })
  it('fences an enrollment reply in the same microtask as renewal, retaining identical intent and fresh CSRF retry', async () => {
    const held = deferred<{
      controller: DeliveryControllerDetail
      bearer: string
      secret_status: string
    }>()
    api.enrollController
      .mockReturnValueOnce(held.promise)
      .mockResolvedValueOnce({ controller: detail, bearer: null, secret_status: 'not_recoverable' })
    await act(async () => life.prepare(original()))
    let first!: Promise<void>
    await act(async () => {
      first = life.submit()
    })
    expect(life.pending).toBe(true)
    await act(async () => {
      cache.setQueryData(['auth', 'session'], session('renewed-csrf'))
      held.resolve({ controller: detail, bearer, secret_status: 'display_once' })
      await first
    })
    expect(life.bearer).toBe('')
    expect(life.saved).toBeUndefined()
    expect(life.uncertain).toBe(true)
    expect(life.intent?.input).toEqual(body)
    expect(life.pending).toBe(false)
    authority = { ...authority, fresh: false, generation: 2 }
    await render()
    expect(life.intent?.input).toEqual(body)
    expect(life.current('secrets.write')).toBe(false)
    await act(async () => life.submit())
    expect(api.enrollController).toHaveBeenCalledTimes(1)
    authority = { ...authority, fresh: true }
    await render()
    await act(async () => life.submit())
    expect(api.enrollController).toHaveBeenNthCalledWith(
      2,
      slot,
      body,
      hash,
      'renewed-csrf',
      expect.any(AbortSignal),
    )
    expect(life.bearer).toBe('')
    expect(life.intent).toBeUndefined()
    expect(life.notice).toBe('saved')
  })
  it('retains a dispatched uncertain body beyond five minutes and after a rejected retry', async () => {
    const held = deferred<any>()
    api.enrollController
      .mockReturnValueOnce(held.promise)
      .mockRejectedValueOnce(new Error('transport failed'))
    await act(async () => life.prepare(original()))
    let pending!: Promise<void>
    await act(async () => {
      pending = life.submit()
    })
    await act(async () => cache.setQueryData(['auth', 'session'], session('renewed-csrf')))
    vi.useFakeTimers()
    await act(async () => vi.advanceTimersByTime(6 * 60 * 1000))
    expect(life.intent?.input).toEqual(body)
    await act(async () => life.submit())
    expect(life.intent?.input).toEqual(body)
    expect(life.uncertain).toBe(true)
    held.resolve({ controller: detail, bearer, secret_status: 'display_once' })
    await act(async () => pending)
    expect(life.bearer).toBe('')
  })
  it.each([
    'actor',
    'target',
    'selected Slot birth',
    'selected controller',
    'missing Session',
    'unmount',
  ])('clears sensitive state and rejects late completion after %s', async (change) => {
    const held = deferred<any>()
    api.enrollController.mockReturnValue(held.promise)
    await act(async () => life.prepare(original()))
    let pending!: Promise<void>
    await act(async () => {
      pending = life.submit()
    })
    if (change === 'actor') {
      authority = { ...authority, actor: 'usr_bob' }
      cache.setQueryData(['auth', 'session'], session('new-csrf', 'usr_bob'))
      await render()
    } else if (change === 'target') {
      selected = 'vap_01arz3ndektsv4rrffq69g5faw'
      await render()
    } else if (change === 'selected Slot birth') {
      resource = slot + ':new-birth'
      await render()
    } else if (change === 'selected controller') {
      resource = controller + ':other-controller'
      await render()
    } else if (change === 'missing Session')
      await act(async () => cache.setQueryData(['auth', 'session'], null))
    else await act(async () => root.unmount())
    held.resolve({ controller: detail, bearer, secret_status: 'display_once' })
    await act(async () => pending)
    expect(life.bearer).toBe('')
    if (change !== 'unmount') expect(life.intent).toBeUndefined()
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((q) => q.state.data),
      ),
    ).not.toContain(bearer)
  })
  it('blocks old query proof and independent write denial before dispatch', async () => {
    const value = original()
    cache.setQueryData(proofKey, { review_etag: 'b'.repeat(64) })
    await act(async () => life.prepare(value))
    expect(life.intent).toBeUndefined()
    await act(async () => cache.setQueryData(authority.permissionKey, ['secrets.read']))
    await act(async () => life.prepare(original()))
    expect(life.intent).toBeUndefined()
    expect(api.enrollController).not.toHaveBeenCalled()
  })
  it('refuses an old successful query proof after same-actor Session renewal', async () => {
    const captured = original()
    await act(async () => cache.setQueryData(['auth', 'session'], session('renewed-csrf')))
    expect(life.current('secrets.write')).toBe(true)
    expect(cache.getQueryState(proofKey)?.status).toBe('success')
    await act(async () => life.prepare(captured))
    expect(life.intent).toBeUndefined()
    expect(api.enrollController).not.toHaveBeenCalled()
  })
  it('rejects late private reads before they can become query data', async () => {
    const held = deferred<{ name: string }>()
    let read!: Promise<unknown>
    await act(async () => {
      read = life.read(() => held.promise).catch((error) => error)
    })
    await act(async () => cache.setQueryData(['auth', 'session'], session('renewed-csrf')))
    held.resolve({ name: 'obsolete private result' })
    await flush()
    expect(await read).toBeInstanceOf(Error)
  })
  it('locates a null-bearer receipt without settling original request or treating404 as failed enrollment', async () => {
    api.enrollController.mockRejectedValue(new Error('lost response'))
    api.getControllerEnrollmentReceipt.mockResolvedValue({
      request_id: body.request_id,
      intent_digest: hash,
      enrollment_review: hash,
      controller: { id: controller },
      bearer: null,
      secret_status: 'not_recoverable',
    })
    await act(async () => life.prepare(original()))
    await act(async () => life.submit())
    await act(async () => life.lookupReceipt())
    expect(life.intent?.input).toEqual(body)
    expect(life.uncertain).toBe(true)
    expect(life.bearer).toBe('')
    expect(life.notice).toBe('receipt')
  })
  it('clears an already displayed bearer on renewal and fences delayed clipboard success', async () => {
    const held = deferred<void>()
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: vi.fn(() => held.promise) },
      configurable: true,
    })
    api.enrollController.mockResolvedValue({
      controller: detail,
      bearer,
      secret_status: 'display_once',
    })
    await act(async () => life.prepare(original()))
    await act(async () => life.submit())
    let copy!: Promise<void>
    await act(async () => {
      copy = life.copy(life.bearer)
    })
    await act(async () => cache.setQueryData(['auth', 'session'], session('renewed-csrf')))
    held.resolve()
    await act(async () => copy)
    expect(life.bearer).toBe('')
    expect(life.notice).not.toBe('copied')
    expect(life.saved).toBeUndefined()
    await act(async () => life.copy(bearer))
    expect(navigator.clipboard.writeText).toHaveBeenCalledTimes(1)
  })
})
