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
  let heldReads: (() => void)[]
  let failApply: boolean
  let failRollback: boolean

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
    heldReads = []
    failApply = false
    failRollback = false
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
            heldReads.push(resolve)
          })
        if (fail)
          throw new AxiosError('Unsafe private error', '', config, undefined, {
            ...response,
            status: 503,
          })
        response.data = captured
      } else if (config.url === `${endpoint}/${seed.items[0].id}/rollback`) {
        response.headers.set('etag', `"${'b'.repeat(64)}"`)
        if (config.method === 'get')
          response.data = {
            scope: 'gateway_routing_configuration',
            archive_id: seed.items[0].id,
            archive_created_at: seed.items[0].created_at,
            observed_at: seed.observed_at,
            can_apply: true,
            blocker_codes: [],
            connections: [],
            declarations: [],
            weights: [],
          }
        else {
          if (failRollback)
            throw new AxiosError('Private rollback error', '', config, undefined, {
              ...response,
              status: 503,
            })
          const input = JSON.parse(config.data)
          response.data = {
            scope: 'gateway_routing_configuration',
            request_id: input.request_id,
            archive_id: seed.items[0].id,
            archive_created_at: seed.items[0].created_at,
            applied_at: seed.observed_at,
            changed: false,
            reason: input.reason,
            publication: {
              request_id: input.request_id,
              actor_id: session.user.id,
              reason: input.reason,
              claim_instance_id: 'ins_operator',
              claim_instance_started_at: seed.observed_at,
              claimed_at: seed.observed_at,
              finalized_at: null,
              status: 'unknown',
              failure_code: null,
            },
            runtime_applied: true,
            superseded: false,
          }
        }
      } else if (config.url === `${endpoint}/${seed.items[0].id}/validation`) {
        response.data = {
          scope: 'archive_integrity_only',
          archive_id: seed.items[0].id,
          archive_created_at: seed.items[0].created_at,
          codec_version: 1,
          observed_at: seed.observed_at,
          integrity: 'authenticated',
        }
      } else if (config.url === `${endpoint}/cleanup/preview`) {
        response.headers.set('etag', `"${'a'.repeat(64)}"`)
        response.data = {
          scope: 'archive_storage_only',
          archive_ids: JSON.parse(config.data).archive_ids,
          retained_count: 2,
          encrypted_bytes: 200,
          selected_count: 1,
          selected_encrypted_bytes: 100,
          version_cap: 100,
          encrypted_byte_cap: 268435456,
        }
      } else if (config.url === `${endpoint}/cleanup`) {
        if (failApply)
          throw new AxiosError('Private write error', '', config, undefined, {
            ...response,
            status: 503,
          })
        const input = JSON.parse(config.data)
        response.data = {
          scope: 'archive_storage_only',
          request_id: input.request_id,
          archive_ids: input.archive_ids,
          deleted_count: 1,
          deleted_encrypted_bytes: 100,
          completed_at: '2026-10-14T09:03:00Z',
        }
      } else throw new Error(`Unexpected request ${config.url}`)
      return response
    }
  })
  afterEach(async () => {
    await act(async () => {
      if (!unmounted) root.unmount()
      heldReads.forEach((resolve) => resolve())
    })
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
  const rollbackPermissions = [
    'system.read',
    'system.write',
    'providers.read',
    'providers.write',
    'models.read_all',
    'models.write',
  ]
  const rollbackDialog = () => [...document.querySelectorAll<HTMLElement>('[role="dialog"]')].at(-1)
  async function rollbackClick(text: string) {
    const found = [
      ...(rollbackDialog() ?? document).querySelectorAll<HTMLButtonElement>('button'),
    ].find((node) => node.textContent === text)
    if (!found) throw new Error(`Missing rollback action ${text}`)
    await act(async () => found.click())
  }
  async function prepareUncertainRollback() {
    permissions = [...rollbackPermissions]
    await mount()
    await click(en.archiveMetadataAction)
    await until(() => expect(dialog()?.textContent).toContain(seed.items[0].id))
    await click(en.archiveRollbackAction)
    await rollbackClick(en.archiveRollbackPreview)
    const input = rollbackDialog()!.querySelector('input')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        input,
        'Exact original rollback reason',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    failRollback = true
    await rollbackClick(en.archiveRollbackConfirm)
    expect(rollbackDialog()?.textContent).toContain(en.archiveRollbackIssue_uncertain)
    return requests.find(
      (request) => request.url?.endsWith('/rollback') && request.method === 'post',
    )!
  }
  it.each(['session', 'permissions', 'jobs', 'list'] as const)(
    'retains mounted exact rollback intent across %s renewal and requires a fresh list to retry',
    async (renewal) => {
      const original = await prepareUncertainRollback()
      const mounted = rollbackDialog()
      hold = true
      await act(async () => {
        if (renewal === 'session') {
          currentSession = { ...currentSession, csrf_token: 'fresh-rollback-csrf' }
          cache.setQueryData(sessionKey, currentSession)
        } else if (renewal === 'permissions')
          cache.setQueryData(['permissions', session.user.id], [...permissions])
        else if (renewal === 'jobs')
          cache.setQueryData(systemJobsKey, { items: [job], observed_at: seed.observed_at })
        else void cache.invalidateQueries({ queryKey: ['admin', 'runtime-archives'], exact: false })
      })
      await until(() => expect(heldReads).toHaveLength(1))
      expect(rollbackDialog()).toBe(mounted)
      expect(rollbackDialog()?.textContent).not.toContain(seed.items[0].id)
      expect(rollbackDialog()?.textContent).not.toContain(en.archiveRollbackRetry)
      expect(
        requests.filter(
          (request) => request.method === 'post' && request.url?.endsWith('/rollback'),
        ),
      ).toHaveLength(1)
      hold = false
      await act(async () => heldReads[0]())
      await until(() => expect(rollbackDialog()?.textContent).toContain(en.archiveRollbackRetry))
      expect(rollbackDialog()?.querySelector('input')?.value).toBe('Exact original rollback reason')
      failRollback = false
      await rollbackClick(en.archiveRollbackRetry)
      const writes = requests.filter(
        (request) => request.method === 'post' && request.url?.endsWith('/rollback'),
      )
      expect(writes).toHaveLength(2)
      expect(writes[1].data).toBe(original.data)
      expect(writes[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
      expect(writes[1].headers.get('X-CSRF-Token')).toBe(currentSession.csrf_token)
      expect(rollbackDialog()?.textContent).toContain(en.archiveRollbackPublication_unknown)
      expect(rollbackDialog()?.textContent).toContain(en.archiveRollbackCurrentApplied)
      expect(cache.getMutationCache().getAll()).toHaveLength(0)
    },
  )
  it.each(['providers.read', 'models.read_all'] as const)(
    'hides rollback entry without independent %s permission',
    async (missing) => {
      permissions = rollbackPermissions.filter((permission) => permission !== missing)
      await mount()
      await click(en.archiveMetadataAction)
      await until(() => expect(dialog()?.textContent).toContain(seed.items[0].id))
      expect(dialog()?.textContent).not.toContain(en.archiveRollbackAction)
      expect(requests.some((request) => request.url?.endsWith('/rollback'))).toBe(false)
    },
  )
  it.each(['system.write', 'providers.write', 'models.write'] as const)(
    'keeps preview readable but withholds dispatch without independent %s permission',
    async (missing) => {
      permissions = rollbackPermissions.filter((permission) => permission !== missing)
      await mount()
      await click(en.archiveMetadataAction)
      await until(() => expect(dialog()?.textContent).toContain(seed.items[0].id))
      await click(en.archiveRollbackAction)
      await rollbackClick(en.archiveRollbackPreview)
      expect(rollbackDialog()?.textContent).toContain(en.archiveRollbackNoChanges)
      expect(rollbackDialog()?.textContent).not.toContain(en.archiveRollbackConfirm)
      expect(
        requests.filter(
          (request) => request.method === 'post' && request.url?.endsWith('/rollback'),
        ),
      ).toHaveLength(0)
    },
  )
  it('locks archive selection, cleanup and pagination throughout review and releases them only after explicit close', async () => {
    permissions = [...rollbackPermissions]
    page.next_cursor = 'next'
    await mount()
    await click(en.archiveMetadataAction)
    await until(() => expect(dialog()?.textContent).toContain(seed.items[0].id))
    await click(en.archiveRollbackAction)
    expect(dialog()?.querySelector('[role="switch"]')?.hasAttribute('data-disabled')).toBe(true)
    expect(button(en.archiveNextPage).disabled).toBe(true)
    expect(button(en.refresh).disabled).toBe(true)
    expect(dialog()?.textContent).not.toContain(en.archiveCleanupPreview)
    await rollbackClick(en.routingClose)
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(dialog()?.querySelector('[role="switch"]')?.hasAttribute('data-disabled')).toBe(false)
    expect(button(en.archiveNextPage).disabled).toBe(false)
    expect(dialog()?.textContent).toContain(en.archiveCleanupPreview)
  })
  it('clears old target intent when a fresh archive row has a different exact birth', async () => {
    await prepareUncertainRollback()
    page.items[0].created_at = '2026-10-14T09:05:00Z'
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['admin', 'runtime-archives'], exact: false })
    })
    await until(() => expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1))
    expect(document.body.textContent).not.toContain(en.archiveRollbackRetry)
    expect(document.body.textContent).not.toContain('Exact original rollback reason')
  })
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
  it('offers explicit integrity read to a read-only administrator and clears it on list refresh', async () => {
    await mount()
    await click(en.archiveMetadataAction)
    await until(() => expect(dialog()?.textContent).toContain(seed.items[0].id))
    expect(dialog()?.textContent).toContain(en.archiveValidationBoundary)
    await click(en.archiveValidate)
    await until(() => expect(dialog()?.textContent).toContain('Encrypted content authenticated'))
    expect(requests.filter((r) => r.url?.endsWith('/validation'))).toHaveLength(1)
    expect(requests.every((r) => r.method === 'get')).toBe(true)
    hold = true
    await click(en.refresh)
    expect(dialog()?.textContent).not.toContain('Encrypted content authenticated')
    await until(() => expect(release).toBeDefined())
    await act(async () => release!())
    await until(() => expect(dialog()?.textContent).toContain(seed.items[0].id))
    expect(dialog()?.textContent).not.toContain('Encrypted content authenticated')
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
      const staleRelease = release!
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
      if (change === 'actor' || change === 'missing' || change === 'unmount')
        expect(dialog()).toBeNull()
      else {
        expect(dialog()).not.toBeNull()
        expect(dialog()?.textContent).not.toContain(seed.items[0].id)
      }
      await act(async () => staleRelease())
      expect(document.body.textContent).not.toContain(seed.items[0].id)
      expect(query.state.data).toBeUndefined()
    },
  )
  async function prepareCleanupReview() {
    permissions = ['system.read', 'system.write']
    await mount()
    await click(en.archiveMetadataAction)
    await until(() => expect(dialog()?.textContent).toContain(seed.items[0].id))
    const select = dialog()!.querySelector<HTMLButtonElement>('[role="switch"]')!
    await act(async () => select.click())
    await click(en.archiveCleanupPreview)
    const input = dialog()!.querySelector<HTMLInputElement>('input:not([type="checkbox"])')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        input,
        'Exact original reason',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
  }
  async function prepareUncertainCleanup() {
    await prepareCleanupReview()
    failApply = true
    await click(en.archiveCleanupConfirm)
    expect(dialog()?.textContent).toContain(en.archiveCleanupUncertain)
    return requests.find((request) => request.url === `${endpoint}/cleanup`)!
  }
  it('retains the confirmed cleanup count after the archive list refresh', async () => {
    await prepareCleanupReview()
    page.items = []
    hold = true
    await click(en.archiveCleanupConfirm)
    await until(() => expect(release).toBeDefined())
    expect(dialog()?.textContent).not.toContain(
      i18n.t('systemStatus:archiveCleanupCompleted', { count: 1 }),
    )
    await act(async () => release!())
    await until(() => expect(dialog()?.textContent).toContain(en.archiveMetadataEmpty))
    expect(dialog()?.textContent).not.toContain(seed.items[0].id)
    expect(dialog()?.textContent).toContain(
      i18n.t('systemStatus:archiveCleanupCompleted', { count: 1 }),
    )
    await act(async () => i18n.changeLanguage('zh'))
    expect(dialog()?.textContent).toContain(
      i18n.t('systemStatus:archiveCleanupCompleted', { count: 1 }),
    )
    expect(requests.filter((request) => request.url === `${endpoint}/cleanup`)).toHaveLength(1)
    await act(async () => cache.setQueryData(['permissions', session.user.id], ['system.read']))
    expect(document.body.textContent).not.toContain(
      i18n.t('systemStatus:archiveCleanupCompleted', { count: 1 }),
    )
  })
  it('clears a confirmed cleanup count when the Session authority renews', async () => {
    await prepareCleanupReview()
    page.items = []
    await click(en.archiveCleanupConfirm)
    await until(() =>
      expect(dialog()?.textContent).toContain(
        i18n.t('systemStatus:archiveCleanupCompleted', { count: 1 }),
      ),
    )
    await act(async () => {
      currentSession = { ...session, csrf_token: 'renewed-after-cleanup' }
      cache.setQueryData(sessionKey, currentSession)
    })
    await until(() => expect(dialog()?.textContent).toContain(en.archiveMetadataEmpty))
    expect(dialog()?.textContent).not.toContain(
      i18n.t('systemStatus:archiveCleanupCompleted', { count: 1 }),
    )
    expect(requests.filter((request) => request.url === `${endpoint}/cleanup`)).toHaveLength(1)
  })
  it.each(['session', 'permissions', 'jobs', 'list'] as const)(
    'keeps the mounted exact intent across %s renewal and waits for a fresh list before replay',
    async (renewal) => {
      const original = await prepareUncertainCleanup()
      const mountedDialog = dialog()
      hold = true
      await act(async () => {
        if (renewal === 'session') {
          currentSession = { ...currentSession, csrf_token: 'fresh-csrf' }
          cache.setQueryData(sessionKey, currentSession)
        } else if (renewal === 'permissions')
          cache.setQueryData(['permissions', session.user.id], [...permissions])
        else if (renewal === 'jobs')
          cache.setQueryData(systemJobsKey, { items: [job], observed_at: seed.observed_at })
        else void cache.invalidateQueries({ queryKey: ['admin', 'runtime-archives'], exact: false })
      })
      await until(() => expect(heldReads).toHaveLength(1))
      expect(dialog()).toBe(mountedDialog)
      expect(dialog()?.textContent).not.toContain(seed.items[0].id)
      expect(dialog()?.textContent).not.toContain(en.archiveCleanupRetry)
      expect(requests.filter((request) => request.url === `${endpoint}/cleanup`)).toHaveLength(1)
      hold = false
      await act(async () => heldReads[0]())
      await until(() => expect(button(en.archiveCleanupRetry).disabled).toBe(false))
      expect(dialog()!.querySelector<HTMLInputElement>('input:not([type="checkbox"])')?.value).toBe(
        'Exact original reason',
      )
      expect(
        dialog()!.querySelector<HTMLInputElement>('input:not([type="checkbox"])')?.disabled,
      ).toBe(true)
      failApply = false
      await click(en.archiveCleanupRetry)
      const writes = requests.filter((request) => request.url === `${endpoint}/cleanup`)
      expect(writes).toHaveLength(2)
      expect(writes[1].data).toBe(original.data)
      expect(writes[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
      expect(writes[1].headers.get('X-CSRF-Token')).toBe(currentSession.csrf_token)
      expect(
        requests.filter((request) => request.url === `${endpoint}/cleanup/preview`),
      ).toHaveLength(1)
      expect(cache.getMutationCache().getAll()).toHaveLength(0)
    },
  )
  it('retains uncertainty while independent write authority is denied and restores no automatic mutation', async () => {
    const original = await prepareUncertainCleanup()
    await act(async () => cache.setQueryData(['permissions', session.user.id], ['system.read']))
    await until(() => expect(reads()).toHaveLength(2))
    expect(dialog()?.textContent).not.toContain(en.archiveCleanupRetry)
    expect(dialog()?.querySelector('[role="switch"]')).toBeNull()
    expect(requests.filter((request) => request.url === `${endpoint}/cleanup`)).toHaveLength(1)
    await act(async () => cache.setQueryData(['permissions', session.user.id], [...permissions]))
    await until(() => expect(button(en.archiveCleanupRetry).disabled).toBe(false))
    expect(requests.filter((request) => request.url === `${endpoint}/cleanup`)).toHaveLength(1)
    failApply = false
    await click(en.archiveCleanupRetry)
    const writes = requests.filter((request) => request.url === `${endpoint}/cleanup`)
    expect(writes).toHaveLength(2)
    expect(writes[1].data).toBe(original.data)
  })
  it.each(['dismissal', 'actor', 'unmount'] as const)(
    'discards an actor-owned uncertain intent on %s',
    async (change) => {
      await prepareUncertainCleanup()
      if (change === 'dismissal') await click(en.routingClose)
      else if (change === 'actor') {
        await act(async () =>
          cache.setQueryData(sessionKey, {
            ...session,
            user: { ...session.user, id: 'usr_other' },
          }),
        )
        await act(async () => cache.setQueryData(sessionKey, session))
      } else {
        await act(async () =>
          root.render(
            <QueryClientProvider client={cache}>
              <div />
            </QueryClientProvider>,
          ),
        )
      }
      expect(dialog()).toBeNull()
      if (change === 'unmount') await mount()
      await until(() => expect(button(en.archiveMetadataAction).disabled).toBe(false))
      await click(en.archiveMetadataAction)
      await until(() => expect(dialog()?.textContent).toContain(seed.items[0].id))
      expect(dialog()?.textContent).not.toContain(en.archiveCleanupRetry)
      expect(dialog()?.textContent).not.toContain('Exact original reason')
      expect(requests.filter((request) => request.url === `${endpoint}/cleanup`)).toHaveLength(1)
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
