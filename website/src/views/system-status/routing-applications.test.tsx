import { act, StrictMode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/systemStatus'
import zh from '@/i18n/locales/zh/systemStatus'
import type { Session } from '@/types/auth'
import type { RoutingApplicationsPage } from '@/types/runtime-applications'
import type { RuntimeInstallationsPage } from '@/types/runtime-installations'
import { sessionKey } from '@/hooks/use-auth'
import { systemJobsKey } from '@/api/system-status'
import { SystemStatusWorkspace } from './workspace'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
describe.each(['routing', 'installation'] as const)(
  '%s records in the existing System task details',
  (mode) => {
    const installation = mode === 'installation'
    const endpoint = installation ? '/admin/runtime/installations' : '/admin/runtime/applications'
    const queryName = installation ? 'runtime-installations' : 'runtime-applications'
    const enCopy = {
      routingRecordsAction: installation ? en.installationRecordsAction : en.routingRecordsAction,
      routingRecordsDescription: installation
        ? en.installationRecordsDescription
        : en.routingRecordsDescription,
      routingRecordsTitle: installation ? en.installationRecordsTitle : en.routingRecordsTitle,
      routingRecordsEmpty: installation ? en.installationRecordsEmpty : en.routingRecordsEmpty,
      routingRecordsFailed: installation ? en.installationRecordsFailed : en.routingRecordsFailed,
      routingMatch: installation ? en.installationMatch : en.routingMatch,
      routingDifferent: installation ? en.installationDifferent : en.routingDifferent,
    }
    const zhCopy = {
      routingRecordsAction: installation ? zh.installationRecordsAction : zh.routingRecordsAction,
      routingRecordsDescription: installation
        ? zh.installationRecordsDescription
        : zh.routingRecordsDescription,
      routingRecordsTitle: installation ? zh.installationRecordsTitle : zh.routingRecordsTitle,
      routingRecordsEmpty: installation ? zh.installationRecordsEmpty : zh.routingRecordsEmpty,
      routingRecordsFailed: installation ? zh.installationRecordsFailed : zh.routingRecordsFailed,
      routingMatch: installation ? zh.installationMatch : zh.routingMatch,
      routingDifferent: installation ? zh.installationDifferent : zh.routingDifferent,
    }
    const originalAdapter = client.defaults.adapter
    const instance = 'ins_00000000000000000000000001'
    const snapshot = 'cfg_00000000000000000000000001'
    const session: Session = {
      user: { id: 'usr_operator', name: 'Operator', email: 'operator@example.test', role: 'admin' },
      csrf_token: 'csrf',
    }
    const job = {
      id: 'job_runtime',
      code: 'runtime_publication',
      status: 'completed',
      progress: 100,
      executor_id: instance,
      items_total: 1,
      items_completed: 1,
      detail_code: 'published',
      started_at: '2026-10-08T09:00:00Z',
      updated_at: '2026-10-08T09:01:00Z',
      completed_at: '2026-10-08T09:01:00Z',
    }
    const routingSeed: RoutingApplicationsPage = {
      scope: 'routing_only',
      observed_at: '2026-10-08T09:02:00Z',
      next_cursor: null,
      items: [
        {
          id: 'rap_00000000000000000000000002',
          instance_id: instance,
          instance_started_at: '2026-10-08T09:00:00Z',
          snapshot_id: snapshot,
          published_at: '2026-10-08T09:01:00Z',
          applied_at: '2026-10-08T09:01:00Z',
          instance_status: 'online',
          current_serving_snapshot_matches: null,
        },
      ],
    }
    const seed: RoutingApplicationsPage | RuntimeInstallationsPage = installation
      ? {
          scope: 'single_process_gateway_admission',
          observed_at: routingSeed.observed_at,
          next_cursor: null,
          items: [
            {
              id: 'rin_00000000000000000000000002',
              projection_version: 1,
              instance_id: instance,
              instance_started_at: '2026-10-08T09:00:00Z',
              snapshot_id: snapshot,
              routes_published_at: '2026-10-08T09:01:00Z',
              first_observed_at: '2026-10-08T09:01:00Z',
              instance_status: 'online',
              current_serving_installation_matches: null,
            },
          ],
        }
      : routingSeed
    let root: Root
    let host: HTMLDivElement
    let cache: QueryClient
    let currentSession: Session
    let permissions: string[]
    let page: RoutingApplicationsPage | RuntimeInstallationsPage
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
        else if (config.url === '/auth/permissions')
          response.data = { permissions: [...permissions] }
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
    async function mount(strict = false) {
      await act(async () =>
        root.render(
          <QueryClientProvider client={cache}>
            {strict ? (
              <StrictMode>
                <SystemStatusWorkspace />
              </StrictMode>
            ) : (
              <SystemStatusWorkspace />
            )}
          </QueryClientProvider>,
        ),
      )
      await until(() => expect(button(enCopy.routingRecordsAction).disabled).toBe(false))
    }
    async function open() {
      await mount()
      expect(reads()).toHaveLength(0)
      await click(enCopy.routingRecordsAction)
    }

    it('opens one bounded executor-scoped read without system.write and separates unknown matching', async () => {
      await open()
      await until(() => expect(dialog()?.textContent).toContain(snapshot))
      expect(reads()).toHaveLength(1)
      expect(reads()[0].params).toEqual({ instance_id: instance })
      expect(reads()[0].headers.has('X-CSRF-Token')).toBe(false)
      expect(dialog()?.textContent).toContain(enCopy.routingRecordsDescription)
      expect(dialog()?.textContent).toContain(en.unknown)
      expect(dialog()?.textContent).not.toContain(enCopy.routingMatch)
      await act(async () => i18n.changeLanguage('zh'))
      expect(dialog()?.textContent).toContain(zhCopy.routingRecordsTitle)
      expect(dialog()?.textContent).toContain(zh.unknown)
      expect(reads()).toHaveLength(1)
      expect(host.querySelectorAll('.system-status-jobs thead th')).toHaveLength(6)
    })

    it.each(['session', 'permission'])(
      'hides completed facts after a failed %s read',
      async (change) => {
        await open()
        await until(() => expect(dialog()?.textContent).toContain(snapshot))
        await act(async () => {
          await cache
            .fetchQuery({
              queryKey: change === 'session' ? sessionKey : ['permissions', session.user.id],
              queryFn: () => Promise.reject(new Error('Renewed read failed')),
              retry: false,
              staleTime: 0,
            })
            .catch(() => undefined)
        })
        await until(() => expect(dialog()).toBeNull())
        expect(document.body.textContent).not.toContain(snapshot)
        expect(reads()).toHaveLength(1)
      },
    )

    it('aborts a held read on dismissal without recreating private records', async () => {
      hold = true
      await open()
      await until(() => expect(release).toBeDefined())
      await click(en.routingClose)
      expect(reads()[0].signal?.aborted).toBe(true)
      await act(async () => release?.())
      await until(() =>
        expect(cache.getQueryCache().findAll({ queryKey: ['admin', queryName] })).toHaveLength(0),
      )
      expect(document.body.textContent).not.toContain(snapshot)
    })

    it('keeps read authority and dismissal working under StrictMode effect replay', async () => {
      await mount(true)
      await click(enCopy.routingRecordsAction)
      await until(() => expect(dialog()?.textContent).toContain(snapshot))
      await click(en.routingClose)
      await until(() => expect(dialog()).toBeNull())
      expect(cache.getQueryCache().findAll({ queryKey: ['admin', queryName] })).toHaveLength(0)
    })

    it.each([true, false])(
      'renders server-confirmed current matching %s separately',
      async (match) => {
        if (page.scope === 'routing_only') page.items[0].current_serving_snapshot_matches = match
        else page.items[0].current_serving_installation_matches = match
        await open()
        await until(() =>
          expect(dialog()?.textContent).toContain(
            match ? enCopy.routingMatch : enCopy.routingDifferent,
          ),
        )
      },
    )

    it('keeps missing history distinct from proof of no application', async () => {
      page.items = []
      await open()
      await until(() => expect(dialog()?.textContent).toContain(enCopy.routingRecordsEmpty))
    })

    it('replaces pages using the opaque cursor and returns to newest without per-row reads', async () => {
      page.next_cursor = 'opaque_cursor'
      await open()
      await until(() => expect(dialog()?.textContent).toContain(snapshot))
      page.items[0] = {
        ...page.items[0],
        id: installation ? 'rin_00000000000000000000000001' : 'rap_00000000000000000000000001',
        snapshot_id: 'cfg_00000000000000000000000002',
      }
      page.next_cursor = null
      await click(en.routingOlder)
      await until(() => expect(dialog()?.textContent).toContain(page.items[0].snapshot_id))
      expect(dialog()?.textContent).not.toContain(snapshot)
      expect(reads()[1].params).toEqual({ instance_id: instance, cursor: 'opaque_cursor' })
      page = structuredClone(seed)
      await click(en.routingNewest)
      await until(() => expect(dialog()?.textContent).toContain(snapshot))
      expect(reads()).toHaveLength(3)
    })

    it('hides old facts during refresh and after a failed read without exposing server text', async () => {
      await open()
      await until(() => expect(dialog()?.textContent).toContain(snapshot))
      hold = true
      fail = true
      await click(en.refresh)
      await until(() => expect(release).toBeDefined())
      expect(dialog()?.textContent).not.toContain(snapshot)
      await act(async () => release?.())
      await until(() => expect(dialog()?.textContent).toContain(enCopy.routingRecordsFailed))
      expect(dialog()?.textContent).not.toContain(snapshot)
      expect(dialog()?.textContent).not.toContain('Unsafe private error')
    })

    it.each(['session', 'permission', 'jobs', 'actor', 'unmount'])(
      'discards held records after %s authority changes',
      async (change) => {
        hold = true
        await open()
        await until(() => expect(release).toBeDefined())
        const signal = reads()[0].signal
        if (change === 'session')
          await act(async () => cache.refetchQueries({ queryKey: sessionKey, exact: true }))
        if (change === 'permission') {
          permissions = []
          await act(async () =>
            cache.refetchQueries({ queryKey: ['permissions', session.user.id], exact: true }),
          )
        }
        if (change === 'jobs')
          await act(async () => cache.refetchQueries({ queryKey: systemJobsKey, exact: true }))
        if (change === 'actor') {
          currentSession = { ...session, user: { ...session.user, id: 'usr_other' } }
          await act(async () => cache.setQueryData(sessionKey, currentSession))
        }
        if (change === 'unmount') {
          await act(async () => root.unmount())
          unmounted = true
        }
        await until(() => expect(signal?.aborted).toBe(true))
        await act(async () => release?.())
        await until(() => expect(document.body.textContent).not.toContain(snapshot))
        await until(() =>
          expect(cache.getQueryCache().findAll({ queryKey: ['admin', queryName] })).toHaveLength(0),
        )
        expect(reads()).toHaveLength(1)
      },
    )

    it('does not read records with only system.write authority', async () => {
      permissions = ['system.write']
      await act(async () =>
        root.render(
          <QueryClientProvider client={cache}>
            <SystemStatusWorkspace />
          </QueryClientProvider>,
        ),
      )
      await until(() =>
        expect(cache.getQueryData(['permissions', session.user.id])).toEqual(['system.write']),
      )
      expect(button(enCopy.routingRecordsAction).disabled).toBe(true)
      await act(async () => button(enCopy.routingRecordsAction).click())
      expect(reads()).toHaveLength(0)
    })
  },
)
