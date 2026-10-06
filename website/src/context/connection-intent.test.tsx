import { act, useLayoutEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { afterEach, beforeEach, expect, it } from 'vitest'
import { UncertainIntentProvider } from './uncertain-intents'
import { useUncertainIntents } from '@/hooks/use-uncertain-intents'
import { sessionKey } from '@/hooks/use-auth'
import type { SubmittedIntentOwner, ConnectionNameSubmittedIntent } from '@/types/uncertain-intents'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let host: HTMLDivElement, root: Root, cache: QueryClient
let owner: SubmittedIntentOwner | null
let router: ReturnType<typeof createMemoryRouter>
const etag = `${'a'.repeat(64)}.${'b'.repeat(64)}`
const payload = (): ConnectionNameSubmittedIntent => ({
  provider_id: 'prv_Exact',
  connection_id: 'con_Exact',
  etag,
  input: { name: 'Captured', reason: 'Original reason' },
})
function Probe() {
  const current = useUncertainIntents()
  useLayoutEffect(() => {
    owner = current
  }, [current])
  return <p>Owner</p>
}
beforeEach(async () => {
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient()
  owner = null
  cache.setQueryData(sessionKey, { user: { id: 'usr_actor' }, csrf_token: 'csrf' })
  router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <UncertainIntentProvider>
            <Probe />
          </UncertainIntentProvider>
        ),
      },
    ],
    { initialEntries: ['/admin/providers/prv_Exact?tab=connections'] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
})
afterEach(async () => {
  await act(async () => root.unmount())
  router.dispose()
  cache.clear()
  host.remove()
})
it('copies only the immutable exact Connection request and rejects another scope while occupied', async () => {
  const source = payload()
  Object.assign(source, {
    base_url: 'not-retained',
    raw_revision: 'not-retained',
    secret: 'not-retained',
  })
  Object.assign(source.input, { enabled: true })
  let claim: ReturnType<SubmittedIntentOwner['capture']> = null
  await act(async () => {
    claim = owner!.capture('usr_actor', { kind: 'connection-name', payload: source })
  })
  source.input.name = 'Mutated'
  source.input.reason = 'Mutated'
  source.etag = 'Mutated'
  const recovered = owner!.recover('usr_actor')!
  expect(recovered.kind).toBe('connection-name')
  expect(recovered.payload).toEqual(payload())
  if (recovered.kind !== 'connection-name') throw new Error('Wrong scope')
  recovered.payload.input.name = 'Changed recovered copy'
  expect(owner!.recover('usr_actor')!.payload).toEqual(payload())
  expect(owner!.recover('usr_other')).toBeNull()
  expect(
    owner!.capture('usr_actor', {
      kind: 'connection-name',
      payload: { ...payload(), connection_id: 'con_other' },
    }),
  ).toBeNull()
  expect(claim!.targetScope).toBe(JSON.stringify(['connection-name', 'prv_Exact', 'con_Exact']))
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it('renews the claim after same-actor authority loss and stale callbacks cannot clear it', async () => {
  let original: ReturnType<SubmittedIntentOwner['capture']> = null
  await act(async () => {
    original = owner!.capture('usr_actor', { kind: 'connection-name', payload: payload() })
  })
  await act(async () => {
    await cache.invalidateQueries({ queryKey: sessionKey })
  })
  expect(owner!.recover('usr_actor')).toBeNull()
  expect(owner!.isCurrent(original!)).toBe(false)
  await act(async () => {
    cache.setQueryData(sessionKey, { user: { id: 'usr_actor' }, csrf_token: 'renewed' })
  })
  const recovered = owner!.recover('usr_actor')!
  expect(recovered.claim).not.toBe(original)
  expect(recovered.payload).toEqual(payload())
  expect(owner!.clear(original!)).toBe(false)
  expect(owner!.recover('usr_actor')).not.toBeNull()
  await act(async () => {
    expect(owner!.clear(recovered.claim)).toBe(true)
  })
  expect(owner!.recover('usr_actor')).toBeNull()
})
it.each([
  { ...payload(), provider_id: 'pro_old' },
  { ...payload(), provider_id: 'prv_bad space' },
  { ...payload(), connection_id: 'con_' + 'x'.repeat(27) },
  { ...payload(), connection_id: 'other' },
])('does not capture unsafe or mismatched resource identifiers %#', async (invalid) => {
  await act(async () => {
    expect(owner!.capture('usr_actor', { kind: 'connection-name', payload: invalid })).toBeNull()
  })
  expect(owner!.recover('usr_actor')).toBeNull()
})
