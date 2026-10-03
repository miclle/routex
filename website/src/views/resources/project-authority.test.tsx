import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { ResourceRecord } from '@/types/resources'
import ResourceDetailPage from './detail'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

type HeldReply = { status: number; data?: ResourceRecord; wait: Promise<void>; release: () => void }
let root: Root
let host: HTMLDivElement
let cache: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let actor: string
let authFailure: number
let replies: Map<string, HeldReply[]>
let pending: Set<() => void>
let reads: { url: string; actor: string }[]
const originalAdapter = client.defaults.adapter
const projects: Record<string, ResourceRecord> = {
  prj_one: project('prj_one', 'Private first Project'),
  prj_two: project('prj_two', 'Private second Project'),
}

function session(): Session {
  return {
    user: { id: actor, name: 'Current manager', email: 'manager@example.invalid', role: 'member' },
    csrf_token: 'current-csrf',
  }
}
function project(id: string, name: string): ResourceRecord {
  return {
    id,
    name,
    description: `${name} confidential description`,
    status: 'active',
    created_at: '2026-10-03T00:00:00Z',
    model_ids: ['mdl_private'],
    managers: [
      {
        id: 'pmg_private',
        user_id: 'usr_one',
        name: 'Private manager',
        email: 'private-manager@example.invalid',
      },
    ],
  }
}
function resourceKey(id: string, identity = actor) {
  return ['resources', 'projects', false, id, identity]
}
function hold(id: string, status: number, data?: ResourceRecord, identity = actor) {
  let release!: () => void
  const wait = new Promise<void>((resolve) => {
    release = resolve
  })
  pending.add(release)
  const key = `${identity}:${id}`
  replies.set(key, [...(replies.get(key) ?? []), { status, data, wait, release }])
  return release
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_one'
  authFailure = 0
  replies = new Map()
  pending = new Set()
  reads = []
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  // A long application cache lifetime must not bypass a fresh resource review.
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 60_000 }, mutations: { retry: false } },
  })
  cache.setQueryData(sessionKey, session())
  cache.setQueryData(['permissions', actor], [])
  client.defaults.adapter = async (config) => {
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session') {
      response.status = authFailure || 200
      response.data = session()
    } else if (config.url === '/auth/permissions') response.data = { permissions: [] }
    else if (config.url?.startsWith('/projects/')) {
      const identity = cache.getQueryData<Session>(sessionKey)?.user.id ?? ''
      reads.push({ url: config.url, actor: identity })
      const id = config.url.slice('/projects/'.length)
      const reply = replies.get(`${identity}:${id}`)?.shift()
      if (reply) {
        await reply.wait
        response.status = reply.status
        response.data = reply.data
      } else response.data = projects[id]
    }
    if (response.status >= 400)
      throw new AxiosError('Current resource access denied', '', config, undefined, response)
    return response
  }
})
afterEach(async () => {
  await act(async () => {
    for (const release of pending) release()
    root.unmount()
  })
  router?.dispose()
  cache.clear()
  client.defaults.adapter = originalAdapter
  host.remove()
})
async function mount(id = 'prj_one') {
  router = createMemoryRouter(
    [{ path: '/projects/:resourceId', element: <ResourceDetailPage kind="projects" /> }],
    { initialEntries: [`/projects/${id}?tab=settings`] },
  )
  await act(async () => {
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    )
  })
}
async function until(assertion: () => void) {
  for (let attempt = 0; attempt < 80; attempt++) {
    await act(async () => new Promise((resolve) => setTimeout(resolve, 10)))
    try {
      assertion()
      return
    } catch (error) {
      if (attempt === 79) throw error
    }
  }
}
function expectPrivateHidden() {
  expect(document.body.textContent).not.toContain('Private first Project')
  expect(document.body.textContent).not.toContain('Private second Project')
  expect(document.body.textContent).not.toContain('private-manager@example.invalid')
  expect(host.querySelector('[role="tab"]')).toBeNull()
  expect(host.querySelector('input[name="name"]')).toBeNull()
  expect(host.querySelector('form')).toBeNull()
}
function readCount(identity = actor, id = 'prj_one') {
  return reads.filter((read) => read.url === `/projects/${id}` && read.actor === identity).length
}

describe('fresh Project detail authority', () => {
  it.each([401, 403, 404])(
    'hides cached details while reviewing access and after HTTP %i',
    async (status) => {
      await mount()
      await until(() => expect(host.querySelector('input[name="name"]')).not.toBeNull())
      expect(document.body.textContent).toContain('Private first Project')
      const release = hold('prj_one', status)
      await act(async () => {
        void cache.invalidateQueries({ queryKey: resourceKey('prj_one') })
      })
      await until(() => expect(readCount()).toBe(2))
      expectPrivateHidden()
      expect(host.textContent).toContain('Loading')
      await act(async () => release())
      await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
      expectPrivateHidden()
      expect(host.textContent).toContain('Retry')
    },
  )

  it('reauthorizes a cached Project on mount before exposing it', async () => {
    cache.setQueryData(resourceKey('prj_one'), projects.prj_one)
    const fresh = project('prj_one', 'Fresh reviewed Project')
    const release = hold('prj_one', 200, fresh)
    await mount()
    await until(() => expect(readCount()).toBe(1))
    expectPrivateHidden()
    await act(async () => release())
    await until(() => expect(host.textContent).toContain('Fresh reviewed Project'))
    expect(host.querySelector('input[name="name"]')).not.toBeNull()
    expect(host.textContent).not.toContain('Private first Project')
  })

  it('does not expose another cached scope or let a late denial replace its fresh detail', async () => {
    const releaseFirst = hold('prj_one', 404)
    cache.setQueryData(resourceKey('prj_one'), projects.prj_one)
    cache.setQueryData(resourceKey('prj_two'), projects.prj_two)
    await mount()
    await until(() => expect(readCount()).toBe(1))
    const releaseSecond = hold('prj_two', 200, projects.prj_two)
    await act(async () => router.navigate('/projects/prj_two?tab=settings'))
    await until(() => expect(readCount(actor, 'prj_two')).toBe(1))
    expectPrivateHidden()
    await act(async () => releaseSecond())
    await until(() => expect(host.textContent).toContain('Private second Project'))
    await act(async () => releaseFirst())
    expect(host.textContent).toContain('Private second Project')
    expect(host.textContent).not.toContain('Private first Project')
    expect(host.querySelector('[role="alert"]')).toBeNull()
  })

  it('suppresses cached and late old-actor detail when the new actor is denied', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('Private first Project'))
    const releaseOld = hold('prj_one', 200, projects.prj_one)
    await act(async () => {
      void cache.invalidateQueries({ queryKey: resourceKey('prj_one') })
    })
    await until(() => expect(readCount()).toBe(2))
    const releaseNew = hold('prj_one', 403, undefined, 'usr_two')
    cache.setQueryData(resourceKey('prj_one', 'usr_two'), projects.prj_one)
    actor = 'usr_two'
    await act(async () => cache.setQueryData(sessionKey, session()))
    await until(() => expect(readCount('usr_two')).toBe(1))
    expectPrivateHidden()
    await act(async () => releaseNew())
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    await act(async () => releaseOld())
    expectPrivateHidden()
    expect(host.querySelector('[role="alert"]')).not.toBeNull()
  })

  it.each([401, 403])(
    'hides a previously authorized Project after Session HTTP %i',
    async (status) => {
      await mount()
      await until(() => expect(host.textContent).toContain('Private first Project'))
      authFailure = status
      await act(async () => {
        void cache.invalidateQueries({ queryKey: sessionKey })
      })
      await until(() => {
        if (status === 401) expect(cache.getQueryData(sessionKey)).toBeNull()
        else expect(cache.getQueryState(sessionKey)?.status).toBe('error')
      })
      expectPrivateHidden()
    },
  )
})
