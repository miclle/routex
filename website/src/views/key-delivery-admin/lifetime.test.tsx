import { StrictMode, act, useLayoutEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { Session } from '@/types/auth'
import { KeyDeliveryError } from '@/api/key-delivery'
import { useDeliveryLifetime, type DeliveryAuthority, type DeliveryLifetime } from './lifetime'

const api = vi.hoisted(() => ({
  saveKeyCallerTemplate: vi.fn(),
  saveKeyDeliveryIdentity: vi.fn(),
  saveKeyDeliveryPolicy: vi.fn(),
  updateKeyDeliveryTarget: vi.fn(),
  verifyKeyDelivery: vi.fn(),
}))
vi.mock('@/api/key-delivery', () => ({
  ...api,
  KeyDeliveryError: class extends Error {
    constructor(public status: number) {
      super('Request rejected')
    }
  },
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, cache: QueryClient, host: HTMLDivElement, life: DeliveryLifetime
let authority: DeliveryAuthority
const etag = 'a'.repeat(64)
const session = (csrf = 'a'.repeat(64)) =>
  ({
    user: { id: 'usr_alice', name: 'Alice', email: 'alice@example.test', role: 'admin' },
    csrf_token: csrf,
  }) as Session

function Harness() {
  const current = useDeliveryLifetime(authority, { current: true, generation: 1 })
  useLayoutEffect(() => {
    life = current
  })
  return null
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
async function prepare(reason: string) {
  await act(async () => life.prepare({ kind: 'verify', etag, input: { reason } }))
}
beforeEach(async () => {
  vi.resetAllMocks()
  api.verifyKeyDelivery.mockResolvedValue({})
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  authority = {
    actor: 'usr_alice',
    admin: true,
    fresh: true,
    generation: 1,
    permissionKey: ['permissions', 'usr_alice', 'delivery'],
  }
  cache.setQueryData(['auth', 'session'], session())
  cache.setQueryData(authority.permissionKey, ['secrets.read', 'secrets.write', 'secrets.test'])
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  await render()
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
})

for (const renewal of ['cancel', 'Session', 'parent generation']) {
  it(`rejects the old confirmation after ${renewal} and a new review`, async () => {
    await prepare('First reviewed operation')
    const queued = life.dispatch
    if (renewal === 'cancel') await act(async () => life.discard())
    if (renewal === 'Session')
      await act(async () => cache.setQueryData(['auth', 'session'], session('b'.repeat(64))))
    if (renewal === 'parent generation') {
      authority = { ...authority, generation: 2 }
      await render()
    }
    expect(life.intent).toBeUndefined()
    await prepare('Second explicitly reviewed operation')
    await act(async () => queued())
    expect(api.verifyKeyDelivery).not.toHaveBeenCalled()
    expect(life.intent?.input.reason).toBe('Second explicitly reviewed operation')
    await act(async () => life.dispatch())
    expect(api.verifyKeyDelivery).toHaveBeenCalledOnce()
    expect(api.verifyKeyDelivery.mock.calls[0][0]).toBe('Second explicitly reviewed operation')
  })
}

it('requires the current retry callback after an uncertain original response', async () => {
  api.verifyKeyDelivery.mockRejectedValueOnce(new KeyDeliveryError(0))
  await prepare('Original uncertain operation')
  const queued = life.dispatch
  await act(async () => queued())
  expect(life.uncertain).toBe(true)
  expect(api.verifyKeyDelivery).toHaveBeenCalledOnce()
  await act(async () => queued())
  expect(api.verifyKeyDelivery).toHaveBeenCalledOnce()
  await act(async () => life.dispatch())
  expect(api.verifyKeyDelivery).toHaveBeenCalledTimes(2)
  expect(api.verifyKeyDelivery.mock.calls[1].slice(0, 2)).toEqual([
    'Original uncertain operation',
    etag,
  ])
})

it('retains the original uncertain intent for a fresh retry after Session renewal', async () => {
  api.verifyKeyDelivery.mockRejectedValueOnce(new KeyDeliveryError(0))
  await prepare('Original intent across renewal')
  await act(async () => life.dispatch())
  const staleRetry = life.dispatch
  await act(async () => cache.setQueryData(['auth', 'session'], session('c'.repeat(64))))
  expect(life.uncertain).toBe(true)
  await act(async () => staleRetry())
  expect(api.verifyKeyDelivery).toHaveBeenCalledOnce()
  await act(async () => life.dispatch())
  expect(api.verifyKeyDelivery).toHaveBeenCalledTimes(2)
  expect(api.verifyKeyDelivery.mock.calls[1].slice(0, 3)).toEqual([
    'Original intent across renewal',
    etag,
    'c'.repeat(64),
  ])
  expect(life.intent).toBeUndefined()
})

it('permits a freshly reviewed operation after StrictMode effect replay', async () => {
  await act(async () =>
    root.render(
      <StrictMode>
        <QueryClientProvider client={cache}>
          <Harness />
        </QueryClientProvider>
      </StrictMode>,
    ),
  )
  await prepare('Fresh StrictMode review')
  await act(async () => life.dispatch())
  expect(api.verifyKeyDelivery).toHaveBeenCalledOnce()
  expect(api.verifyKeyDelivery.mock.calls[0][0]).toBe('Fresh StrictMode review')
})

it('keeps each new explicit retry available through repeated immediate uncertainty', async () => {
  api.verifyKeyDelivery.mockRejectedValue(new KeyDeliveryError(0))
  await prepare('Preserved uncertain review')
  for (let attempt = 1; attempt <= 3; attempt++) {
    await act(async () => life.dispatch())
    expect(api.verifyKeyDelivery).toHaveBeenCalledTimes(attempt)
    expect(life.uncertain).toBe(true)
    expect(life.intent?.input.reason).toBe('Preserved uncertain review')
  }
  for (const call of api.verifyKeyDelivery.mock.calls) {
    expect(call.slice(0, 3)).toEqual(['Preserved uncertain review', etag, 'a'.repeat(64)])
  }
})
