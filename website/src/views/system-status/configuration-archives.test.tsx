import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/systemStatus'
import zh from '@/i18n/locales/zh/systemStatus'
import type { Session } from '@/types/auth'
import type { RuntimeArchiveMetadataPage } from '@/types/runtime-archives'
import { sessionKey } from '@/hooks/use-auth'
import { systemJobsKey } from '@/api/system-status'
import { SystemStatusWorkspace } from './workspace'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
describe('archive metadata in the existing System jobs card', () => {
  const endpoint = '/admin/runtime/archives'
  const seed: RuntimeArchiveMetadataPage = {
    scope: 'archive_metadata_only',
    observed_at: '2026-10-14T09:02:00Z',
    next_cursor: null,
    items: [
      {
        id: 'rcv_00000000000000000000000002',
        created_at: '2026-10-14T09:01:00Z',
        codec_version: 1,
        content_digest: 'a'.repeat(64),
        document_bytes: 32768,
      },
    ],
  }
  const session: Session = {
    user: { id: 'usr_operator', name: 'Operator', email: 'operator@example.test', role: 'admin' },
    csrf_token: 'csrf',
  }
  const job = {
    id: 'job_runtime',
    code: 'runtime_publication',
    status: 'completed',
    progress: 100,
    executor_id: 'ins_00000000000000000000000001',
    items_total: 1,
    items_completed: 1,
    detail_code: 'published',
    started_at: '2026-10-14T09:00:00Z',
    updated_at: '2026-10-14T09:01:00Z',
    completed_at: '2026-10-14T09:01:00Z',
  }
  const originalAdapter = client.defaults.adapter
  let root: Root
  let host: HTMLDivElement
  let cache: QueryClient
  let currentSession: Session
  let permissions: string[]
  let page: RuntimeArchiveMetadataPage
  let requests: InternalAxiosRequestConfig[]
  let hold: boolean
  let release: (() => void) | undefined
  let fail: boolean
  let unmounted: boolean

  beforeEach(async () => {
    await i18n.changeLanguage('en')
    currentSession = structuredClone(session)
    permissions = ['system.read']
    page = structuredClone(seed)
    requests = []
    hold = false
    fail = false
    release = undefined
    unmounted = false
    host = document.createElement('div')
    document.body.append(host)
    root = createRoot(host)
    cache = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    cache.setQueryData(sessionKey, currentSession)
    client.defaults.adapter = async (config) => {
      requests.push(config)
      const response = {
        config,
        status: 200,
        statusText: 'OK',
        headers: new AxiosHeaders(),
        data: {} as unknown,
      }
      if (config.url === '/auth/session') response.data = structuredClone(currentSession)
      else if (config.url === '/auth/permissions') response.data = { permissions: [...permissions] }
      else if (config.url === '/admin/system/instances')
        response.data = { items: [], observed_at: seed.observed_at }
      else if (config.url === '/admin/system/jobs')
        response.data = { items: [job], observed_at: seed.observed_at }
      else if (config.url === endpoint) {
        const captured = structuredClone(page)
        if (hold)
          await new Promise<void>((resolve) => {
            release = resolve
          })
        if (fail)
          throw new AxiosError('Unsafe private error', '', config, undefined, {
            ...response,
            status: 503,
          })
        response.data = captured
      } else throw new Error(`Unexpected request ${config.url}`)
      return response
    }
  })
  afterEach(async () => {
    release?.()
    if (!unmounted) await act(async () => root.unmount())
    cache.clear()
    host.remove()
    client.defaults.adapter = originalAdapter
  })

  async function until(check: () => void) {
    const end = Date.now() + 5000
    for (;;) {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 5))
      })
      try {
        check()
        return
      } catch (error) {
        if (Date.now() >= end) throw error
      }
    }
  }
  function button(text: string) {
    const found = [...(dialog() ?? document).querySelectorAll<HTMLButtonElement>('button')].find(
      (node) => node.textContent === text,
    )
    if (!found) throw new Error(`Missing button ${text}`)
    return found
  }
  async function click(text: string) {
    await act(async () => button(text).click())
  }
  const dialog = () => document.querySelector<HTMLElement>('[role="dialog"]')
  const reads = () => requests.filter((request) => request.url === endpoint)
  async function mount() {
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <SystemStatusWorkspace />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(cache.getQueryData(systemJobsKey)).toBeDefined())
  }
  it('explicitly opens metadata only, switches language without a read and sends no mutation', async () => {
    await mount()
    expect(reads()).toHaveLength(0)
    await click(en.archiveMetadataAction)
    await until(() => expect(dialog()?.textContent).toContain(seed.items[0].id))
    expect(dialog()?.textContent).toContain(en.archiveMetadataBoundary)
    expect(dialog()?.textContent).toContain(en.archiveCaptureBoundary)
    expect(dialog()?.textContent).toContain('32,768')
    await act(async () => i18n.changeLanguage('zh'))
    expect(dialog()?.textContent).toContain(zh.archiveMetadataTitle)
    expect(reads()).toHaveLength(1)
    expect(requests.every((r) => r.method === 'get')).toBe(true)
  })
  it('renders honest empty and local error states without stale rows', async () => {
    page.items = []
    await mount()
    await click(en.archiveMetadataAction)
    await until(() => expect(dialog()?.textContent).toContain(en.archiveMetadataEmpty))
    fail = true
    await click(en.refresh)
    await until(() => expect(dialog()?.textContent).toContain(en.archiveMetadataFailed))
    expect(dialog()?.textContent).not.toContain('Unsafe private error')
    fail = false
    await click(en.refresh)
    await until(() => expect(dialog()?.textContent).toContain(en.archiveMetadataEmpty))
  })
  it('pages by explicit action and returns to the first page', async () => {
    page.next_cursor = 'next'
    await mount()
    await click(en.archiveMetadataAction)
    await until(() => expect(dialog()?.textContent).toContain(seed.items[0].id))
    page.items = [{ ...seed.items[0], id: 'rcv_00000000000000000000000001' }]
    page.next_cursor = null
    await click(en.archiveNextPage)
    await until(() => expect(dialog()?.textContent).toContain(page.items[0].id))
    expect(reads()[1].params).toEqual({ cursor: 'next' })
    page = structuredClone(seed)
    await click(en.archiveFirstPage)
    await until(() => expect(dialog()?.textContent).toContain(seed.items[0].id))
    expect(reads().at(-1)?.params).toEqual({})
  })
  it.each(['actor', 'renewal', 'permission', 'jobs', 'error', 'missing', 'unmount'] as const)(
    'hides facts and rejects held replies after %s',
    async (change) => {
      hold = true
      await mount()
      await click(en.archiveMetadataAction)
      await until(() => expect(release).toBeDefined())
      const query = cache
        .getQueryCache()
        .find({ queryKey: ['admin', 'runtime-archives'], exact: false })!
      if (change === 'unmount') {
        await act(async () => root.unmount())
        unmounted = true
      } else if (change === 'actor' || change === 'renewal')
        await act(async () =>
          cache.setQueryData(sessionKey, {
            ...session,
            csrf_token: 'renewed',
            user: change === 'actor' ? { ...session.user, id: 'usr_other' } : session.user,
          }),
        )
      else if (change === 'permission')
        await act(async () => cache.setQueryData(['permissions', session.user.id], []))
      else if (change === 'jobs')
        await act(async () =>
          cache.invalidateQueries({ queryKey: systemJobsKey, refetchType: 'none' }),
        )
      else if (change === 'missing') await act(async () => cache.setQueryData(sessionKey, null))
      else
        await act(async () =>
          cache
            .getQueryCache()
            .find({ queryKey: sessionKey, exact: true })!
            .setState({ status: 'error', error: new Error('unavailable') }),
        )
      expect(dialog()).toBeNull()
      await act(async () => release!())
      expect(document.body.textContent).not.toContain(seed.items[0].id)
      expect(query.state.data).toBeUndefined()
    },
  )
  it('does not read without fresh read permission', async () => {
    permissions = []
    await mount()
    expect(button(en.archiveMetadataAction).disabled).toBe(true)
    await click(en.archiveMetadataAction)
    expect(reads()).toHaveLength(0)
  })
  function retainedClick(node: HTMLButtonElement) {
    const key = Object.keys(node).find((key) => key.startsWith('__reactProps$'))!
    return (node as unknown as Record<string, { onClick: () => void }>)[key].onClick
  }
  it('rejects retained pagination and refresh callbacks after same-page renewal', async () => {
    page.next_cursor = 'next'
    await mount()
    await click(en.archiveMetadataAction)
    await until(() => expect(dialog()?.textContent).toContain(seed.items[0].id))
    const next = retainedClick(button(en.archiveNextPage))
    const refresh = retainedClick(button(en.refresh))
    await click(en.refresh)
    await until(() => expect(reads()).toHaveLength(2))
    await until(() => expect(button(en.refresh).disabled).toBe(false))
    await act(async () => {
      next()
      refresh()
    })
    expect(reads()).toHaveLength(2)
    await click(en.archiveNextPage)
    await until(() => expect(reads()).toHaveLength(3))
  })
  it('rejects a retained open action after Session renewal', async () => {
    await mount()
    const open = retainedClick(button(en.archiveMetadataAction))
    await act(async () => cache.setQueryData(sessionKey, { ...session, csrf_token: 'new-csrf' }))
    await act(async () => open())
    expect(reads()).toHaveLength(0)
  })
})
