import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, useLocation } from 'react-router'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import GitHubComplete from './complete'
import GitHubLoginButton from './login-button'
import type { NamedIdentityMethod } from './method'
import type { Session } from '@/types/auth'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let host: HTMLDivElement, root: Root, cache: QueryClient
let requests: InternalAxiosRequestConfig[]
let held: { path: string; promise: Promise<void>; release: () => void } | undefined
let acquired: number, released: number
const returnedSession: Session = {
  user: { id: 'usr_alice', name: 'Alice', email: 'alice@example.test', role: 'member' },
  csrf_token: 'fresh-result',
}
function Location() {
  return <output data-location>{useLocation().pathname}</output>
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  cache.setQueryData(['auth', 'session'], null)
  requests = []
  acquired = released = 0
  held = undefined
  client.defaults.adapter = async (request) => {
    requests.push(request)
    const waiting = held
    if (waiting && waiting.path === request.url) await waiting.promise
    let status = 200,
      data: unknown = {}
    if (request.url === '/auth/session') {
      status = 401
      data = null
    } else if (request.url === '/auth/google' || request.url === '/auth/github')
      data = {
        available: true,
        name: request.url.endsWith('google') ? 'Google sign-in' : 'GitHub sign-in',
      }
    else if (request.url?.endsWith('/complete')) data = structuredClone(returnedSession)
    else if (request.url?.endsWith('/start')) {
      if (request.url === '/auth/google/start') {
        const query = new URLSearchParams({
          response_type: 'code',
          client_id: 'client-id',
          redirect_uri: 'https://routex.example.test/api/v1/auth/google/callback',
          scope: 'openid profile',
          state: 'a'.repeat(43),
          nonce: 'b'.repeat(43),
          code_challenge: 'c'.repeat(43),
          code_challenge_method: 'S256',
        })
        data = { authorization_url: `https://accounts.google.com/o/oauth2/v2/auth?${query}` }
      } else status = 503
    } else if (request.url === '/site')
      data = {
        name: 'RouteX',
        logo_url: '',
        footer: '',
        default_language: 'en',
        service_url: '',
        etag: '0',
        updated_at: '2026-10-10T00:00:00Z',
      }
    else status = 404
    return {
      config: request,
      status,
      statusText: '',
      data,
      headers: new AxiosHeaders({
        'Cache-Control': 'private, no-store',
        'X-Content-Type-Options': 'nosniff',
        'Referrer-Policy': 'no-referrer',
      }),
    }
  }
})
afterEach(async () => {
  held?.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
})
function hold(path: string) {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  held = { path, promise, release }
  return release
}
async function wait(check: () => boolean) {
  for (let n = 0; n < 100; n++) {
    if (check()) return
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
  }
  expect(check()).toBe(true)
}
async function click(label: string) {
  const node = Array.from(host.querySelectorAll('button')).find(
    (button) => button.textContent === label,
  )
  expect(node).toBeDefined()
  await act(async () => node!.click())
}
async function render(method: NamedIdentityMethod, view: 'complete' | 'start') {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter initialEntries={['/auth/identity/complete']}>
          {view === 'complete' ? (
            <GitHubComplete method={method} />
          ) : (
            <GitHubLoginButton
              method={method}
              acquire={() => {
                acquired++
                return {
                  current: () => true,
                  release: () => {
                    released++
                  },
                }
              }}
            />
          )}
          <Location />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
}
const writes = () => requests.filter((request) => request.method !== 'get')

describe('finite named identity method lifetime', () => {
  it.each(['read', 'complete'] as const)(
    'discards a held old-method %s and requires a fresh new-method Continue',
    async (phase) => {
      const release = hold(phase === 'read' ? '/auth/session' : '/auth/google/complete')
      await render('google', 'complete')
      expect(writes()).toHaveLength(0)
      await click('Continue')
      await wait(() => requests.some((request) => request.url === held!.path))
      const oldRequest = requests.find((request) => request.url === held!.path)!
      await render('github', 'complete')
      expect(oldRequest.signal?.aborted).toBe(true)
      expect(host.textContent).toContain('GitHub')
      await act(async () => release())
      expect(cache.getQueryData(['auth', 'session'])).toBeNull()
      expect(host.querySelector('[data-location]')?.textContent).toBe('/auth/identity/complete')
      expect(writes().filter((request) => request.url === '/auth/google/complete')).toHaveLength(
        phase === 'read' ? 0 : 1,
      )
      expect(writes().filter((request) => request.url === '/auth/github/complete')).toHaveLength(0)
      held = undefined
      await click('Continue')
      await wait(
        () => cache.getQueryData<Session>(['auth', 'session'])?.csrf_token === 'fresh-result',
      )
      expect(writes().filter((request) => request.url === '/auth/github/complete')).toHaveLength(1)
      expect(host.querySelector('[data-location]')?.textContent).toBe('/')
      expect(cache.getMutationCache().getAll()).toHaveLength(0)
    },
  )
  it('aborts a held old-method start and permits only a fresh new-method start', async () => {
    const release = hold('/auth/google/start')
    await render('google', 'start')
    await wait(() => host.textContent!.includes('Continue with Google sign-in'))
    await click('Continue with Google sign-in')
    await wait(() => writes().length === 1)
    const oldRequest = writes()[0]
    await render('github', 'start')
    expect(oldRequest.signal?.aborted).toBe(true)
    await wait(() => host.textContent!.includes('Continue with GitHub sign-in'))
    await act(async () => release())
    expect(host.querySelector('[data-location]')?.textContent).toBe('/auth/identity/complete')
    expect(writes().map((request) => request.url)).toEqual(['/auth/google/start'])
    expect(acquired).toBe(1)
    expect(released).toBe(1)
    held = undefined
    await click('Continue with GitHub sign-in')
    await wait(() => writes().length === 2 && released === 2)
    expect(writes().map((request) => request.url)).toEqual([
      '/auth/google/start',
      '/auth/github/start',
    ])
    expect(acquired).toBe(2)
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
})
