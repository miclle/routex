import { act, useLayoutEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider, QueryObserver, useQuery } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import type { Session } from '@/types/auth'
import { useOwnerAuthority, type OwnerAuthority } from './lifetime'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const actor = 'usr_01m36yee4gkbns18pfcqqc75a3'
const owner = { kind: 'personal' } as const
const session: Session = {
  user: { id: actor, role: 'member', name: 'Member', email: 'member@example.test' },
  csrf_token: 'a'.repeat(64),
}
let cache: QueryClient, root: Root, container: HTMLDivElement, authority: OwnerAuthority
const observerCleanup = new Set<() => void>()
function Harness() {
  useQuery({ queryKey: ['auth', 'session'], queryFn: async () => session })
  useQuery({ queryKey: ['keys', actor], queryFn: async () => [] })
  useQuery({ queryKey: ['models', actor], queryFn: async () => [] })
  const current = useOwnerAuthority(owner, true)
  useLayoutEffect(() => {
    authority = current
  }, [current])
  return null
}
beforeEach(async () => {
  cache = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } })
  cache.setQueryData(['auth', 'session'], session)
  cache.setQueryData(['keys', actor], [])
  cache.setQueryData(['models', actor], [])
  // Retained success is already in the snapshot; merely observing it changes no authority data.
  cache.setQueryData(['keys', 'retained-review'], [])
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  await act(async () => {
    root.render(
      <QueryClientProvider client={cache}>
        <Harness />
      </QueryClientProvider>,
    )
  })
  expect(authority.fresh).toBe(true)
})
afterEach(async () => {
  for (const cleanup of observerCleanup) cleanup()
  observerCleanup.clear()
  await act(async () => root.unmount())
  cache.clear()
  container.remove()
})
describe('Key owner authority cache lifetime', () => {
  it('keeps an admitted operation through observer add/options/remove bookkeeping without a new authority render', async () => {
    const before = authority
    const ticket = authority.capture()
    const observer = new QueryObserver(cache, {
      queryKey: ['keys', 'retained-review'],
      queryFn: async () => [],
      staleTime: Infinity,
    })
    await act(async () => {
      const unsubscribe = observer.subscribe(() => {})
      observerCleanup.add(unsubscribe)
      observer.setOptions({
        queryKey: ['keys', 'retained-review'],
        queryFn: async () => [],
        staleTime: Infinity,
        enabled: true,
      })
      expect(ticket.controller.signal.aborted).toBe(false)
      expect(authority.current(ticket)).toBe(true)
      unsubscribe()
      observerCleanup.delete(unsubscribe)
      expect(ticket.controller.signal.aborted).toBe(false)
      expect(authority.current(ticket)).toBe(true)
    })
    expect(authority).toBe(before)
    authority.finish(ticket)
  })
  it('aborts synchronously on a Session state update before React commits and admits only the renewed proof', async () => {
    const ticket = authority.capture()
    await act(async () => {
      cache.setQueryData(['auth', 'session'], { ...session, csrf_token: 'b'.repeat(64) })
      expect(ticket.controller.signal.aborted).toBe(true)
      expect(authority.current(ticket)).toBe(false)
    })
    const renewed = authority.capture()
    expect(renewed.csrf).toBe('b'.repeat(64))
    expect(authority.current(renewed)).toBe(true)
    authority.finish(ticket)
    authority.finish(renewed)
  })
  it('aborts synchronously when a retained relevant query is removed', async () => {
    const ticket = authority.capture()
    await act(async () => {
      cache.removeQueries({ queryKey: ['keys', 'retained-review'], exact: true })
      expect(ticket.controller.signal.aborted).toBe(true)
      expect(authority.current(ticket)).toBe(false)
    })
    authority.finish(ticket)
  })
  it('rejects dispatch and aborts current work immediately when an active list becomes invalid', async () => {
    const ticket = authority.capture()
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['keys', actor], exact: true, refetchType: 'none' })
      expect(ticket.controller.signal.aborted).toBe(true)
      expect(authority.current(ticket)).toBe(false)
      expect(() => authority.capture()).toThrow('Owner operation is no longer current')
    })
    expect(authority.fresh).toBe(false)
    authority.finish(ticket)
  })
})
