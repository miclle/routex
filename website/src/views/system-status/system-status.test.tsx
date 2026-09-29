import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/systemStatus'
import zh from '@/i18n/locales/zh/systemStatus'
import { sessionKey } from '@/hooks/use-auth'
import AppShell from '@/components/app/AppShell'
import type { SystemInstance, SystemInstancesPage, SystemJobsPage } from '@/types/system-status'
import SystemStatusPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

const originalAdapter = client.defaults.adapter
const session = {
  user: { id: 'usr_system', name: 'Operator', email: 'operator@example.test', role: 'admin' },
  csrf_token: 'csrf-system',
}
const sample = (scope: string, percent: number | null) => ({
  scope,
  used: percent === null ? null : percent,
  total: percent === null ? null : 100,
  unit: '%',
  percent,
})
const instanceSeed: SystemInstancesPage = {
  observed_at: '2026-09-29T09:10:00Z',
  heartbeat_interval_seconds: 10,
  lease_duration_seconds: 30,
  cleanup_after_seconds: 300,
  items: [
    {
      id: 'ins_online',
      name: 'routex-1',
      hostname: 'gateway-01',
      status: 'online',
      cleanup_eligible: false,
      role: 'combined',
      version: 'v1.2.3',
      commit: 'abcdef123456',
      build_time: '2026-09-29T08:00:00Z',
      go_version: 'go1.27.1',
      os: 'linux',
      arch: 'amd64',
      started_at: '2026-09-29T08:00:00Z',
      last_heartbeat_at: '2026-09-29T09:09:59Z',
      heartbeat_revision: 12,
      stopped_at: null,
      resources: {
        cpu: sample('process', 36),
        memory: sample('process', 58),
        storage: sample('journal filesystem', 42),
      },
    },
    {
      id: 'ins_offline',
      name: 'routex-2',
      hostname: 'gateway-02',
      status: 'offline',
      cleanup_eligible: true,
      role: 'combined',
      version: 'v1.2.2',
      commit: 'deadbeef',
      build_time: '2026-09-28T08:00:00Z',
      go_version: 'go1.27.1',
      os: 'linux',
      arch: 'arm64',
      started_at: '2026-09-28T08:00:00Z',
      last_heartbeat_at: '2026-09-29T08:30:00Z',
      heartbeat_revision: 7,
      stopped_at: null,
      resources: {
        cpu: sample('process', null),
        memory: sample('process', 71),
        storage: sample('journal filesystem', 82),
      },
    },
  ],
}
const jobsSeed: SystemJobsPage = {
  observed_at: '2026-09-29T09:10:00Z',
  items: [
    {
      id: 'job_running',
      code: 'call_record_delivery',
      status: 'running',
      progress: 68,
      executor_id: 'ins_online',
      items_total: 100,
      items_completed: 68,
      detail_code: '',
      started_at: '2026-09-29T09:00:00Z',
      updated_at: '2026-09-29T09:09:00Z',
      completed_at: null,
    },
    {
      id: 'job_failed',
      code: 'storage_cleanup',
      status: 'failed',
      progress: null,
      executor_id: '',
      items_total: 10,
      items_completed: 3,
      detail_code: 'executor_lost',
      started_at: '2026-09-29T08:20:00Z',
      updated_at: '2026-09-29T08:30:00Z',
      completed_at: '2026-09-29T08:30:00Z',
    },
  ],
}

let root: Root
let host: HTMLDivElement
let cache: QueryClient
let router: ReturnType<typeof createMemoryRouter> | undefined
let permissions: string[]
let instances: SystemInstancesPage
let jobs: SystemJobsPage
let requests: InternalAxiosRequestConfig[]
let instanceFailure: number
let jobFailure: number
let cleanupFailure: number
let cleanupShiftOnFailure: boolean
let holdCleanup: boolean
let releaseCleanup: (() => void) | undefined

beforeEach(async () => {
  await i18n.changeLanguage('en')
  permissions = ['system.read', 'system.write']
  instances = structuredClone(instanceSeed)
  jobs = structuredClone(jobsSeed)
  requests = []
  instanceFailure = 0
  jobFailure = 0
  cleanupFailure = 0
  cleanupShiftOnFailure = false
  holdCleanup = false
  releaseCleanup = undefined
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  cache.setQueryData(sessionKey, session)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session') response.data = session
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/site')
      response.data = {
        name: 'RouteX',
        service_url: '',
        logo_url: '',
        footer: '',
        default_language: 'en',
        etag: 'site_1',
      }
    else if (config.url === '/announcements') response.data = { items: [] }
    else if (config.method === 'get' && config.url === '/admin/system/instances') {
      if (instanceFailure) {
        const status = instanceFailure
        instanceFailure = 0
        throw new AxiosError('unsafe instance error', '', config, undefined, {
          ...response,
          status,
        })
      }
      response.data = structuredClone(instances)
    } else if (config.method === 'get' && config.url === '/admin/system/jobs') {
      if (jobFailure) {
        const status = jobFailure
        jobFailure = 0
        throw new AxiosError('unsafe job error', '', config, undefined, { ...response, status })
      }
      response.data = structuredClone(jobs)
    } else if (config.method === 'post' && config.url === '/admin/system/instances/cleanup') {
      if (holdCleanup)
        await new Promise<void>((resolve) => {
          releaseCleanup = resolve
        })
      const body = JSON.parse(String(config.data)) as { instances: { id: string }[] }
      const ids = body.instances.map((item) => item.id)
      if (cleanupFailure) {
        const status = cleanupFailure
        cleanupFailure = 0
        if (cleanupShiftOnFailure) {
          instances.items = Array.from({ length: 200 }, (_, index) => ({
            ...structuredClone(instanceSeed.items[0]),
            id: `ins_shift_${index.toString().padStart(3, '0')}`,
            name: `shifted-${index.toString().padStart(3, '0')}`,
            heartbeat_revision: index + 100,
          }))
        }
        throw new AxiosError('unsafe cleanup error', '', config, undefined, { ...response, status })
      }
      instances.items = instances.items.filter((item) => !ids.includes(item.id))
      response.data = { cleaned_ids: ids, cleaned_count: ids.length }
    } else throw new Error(`Unexpected fixture request ${config.method} ${config.url}`)
    return response
  }
})

afterEach(async () => {
  releaseCleanup?.()
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  client.defaults.adapter = originalAdapter
  host.remove()
  await i18n.changeLanguage('en')
})

async function until(assertion: () => void) {
  await vi.waitFor(
    async () => {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 1))
      })
      assertion()
    },
    { timeout: 5_000 },
  )
}

async function mount(shell = false) {
  const instancesWillLoad = instanceFailure === 0
  const jobsWillLoad = jobFailure === 0
  const page = shell ? (
    <RouterProvider
      router={
        (router = createMemoryRouter(
          [
            {
              path: '/',
              element: <AppShell />,
              children: [{ path: 'admin/system-status', element: <SystemStatusPage /> }],
            },
          ],
          { initialEntries: ['/admin/system-status'] },
        ))
      }
    />
  ) : (
    <SystemStatusPage />
  )
  await act(async () =>
    root.render(<QueryClientProvider client={cache}>{page}</QueryClientProvider>),
  )
  await until(() => expect(cache.getQueryData(['permissions', 'usr_system'])).toEqual(permissions))
  if (permissions.includes('system.read')) {
    if (instancesWillLoad)
      await until(() =>
        expect(cache.getQueryData(['admin', 'system-status', 'instances'])).toBeDefined(),
      )
    else
      await until(() => expect(host.textContent).toContain('Instance status could not be loaded'))
    if (jobsWillLoad)
      await until(() =>
        expect(cache.getQueryData(['admin', 'system-status', 'jobs'])).toBeDefined(),
      )
    else await until(() => expect(host.textContent).toContain('System jobs could not be loaded'))
  }
}

function offlineInstance(index: number): SystemInstance {
  return {
    ...structuredClone(instanceSeed.items[1]),
    id: `ins_offline_${index.toString().padStart(3, '0')}`,
    name: `offline-${index.toString().padStart(3, '0')}`,
    heartbeat_revision: index + 1,
  }
}

function buttons(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].filter(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
}

function button(label: string, last = false) {
  const matches = buttons(label)
  expect(matches.length, label).toBeGreaterThan(0)
  return last ? matches.at(-1)! : matches[0]
}

async function click(label: string, last = false) {
  await act(async () => button(label, last).click())
}

const cleanupRequests = () =>
  requests.filter(
    (request) => request.method === 'post' && request.url === '/admin/system/instances/cleanup',
  )

describe('system status workspace', () => {
  it('renders the approved tables, authoritative stale resources, and reconciled jobs', async () => {
    await mount(true)
    const instanceTable = host.querySelector<HTMLTableElement>('[aria-label="System instances"]')!
    expect(instanceTable).toBeTruthy()
    expect([...instanceTable.querySelectorAll('th')].map((item) => item.textContent)).toEqual([
      'Instance',
      'Status',
      'Role',
      'CPU',
      'Memory',
      'Storage',
      'Version',
      'Runtime',
      'Started',
      'Last heartbeat',
    ])
    expect(host.textContent).toContain('1 online')
    expect(host.textContent).toContain('1 offline instance found')
    expect(host.querySelector('[aria-label="CPU 36 percent"]')).toBeTruthy()
    expect(host.querySelector('[aria-label="Memory 71 percent, stale sample"]')).toBeTruthy()
    expect(host.querySelector('[aria-label="CPU is unknown"]')?.textContent).toBe('—')

    const jobsTable = host.querySelector<HTMLTableElement>('[aria-label="System jobs"]')!
    expect([...jobsTable.querySelectorAll('th')].map((item) => item.textContent)).toEqual([
      'Job',
      'Status',
      'Progress',
      'Executor instance',
      'Updated',
      'Details',
    ])
    expect(host.textContent).toContain('Call record delivery')
    expect(host.textContent).toContain('The run was interrupted because its executor')
    expect(host.textContent).toContain('Unknown')
    expect(host.querySelector('[aria-label="Progress 68%"]')).toBeTruthy()
    expect(host.textContent).toContain('1 in progress')

    const navigation = host.querySelector('nav[aria-label="Main navigation"]')!.textContent!
    expect(navigation.indexOf('System status')).toBeLessThan(
      navigation.indexOf('System information'),
    )
    expect(host.querySelector('a[href="/admin/system-status"]')).toBeTruthy()
  })

  it('keeps the two reads independent and never queries without system.read', async () => {
    permissions = []
    await mount()
    expect(host.textContent).toContain('Access denied')
    expect(requests.some((request) => request.url?.startsWith('/admin/system/'))).toBe(false)

    permissions = ['system.read']
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['permissions', 'usr_system'] })
    })
    await until(() => expect(host.textContent).toContain('routex-1'))
    expect(buttons('Clean up offline instances')).toHaveLength(0)
    jobFailure = 503
    await click('Refresh', true)
    await until(() => expect(host.textContent).toContain('System jobs could not be loaded'))
    expect(host.textContent).toContain('routex-1')
    await click('Retry')
    await until(() => expect(host.textContent).not.toContain('System jobs could not be loaded'))
  })

  it('does not present zero counts as authoritative when initial reads fail', async () => {
    instanceFailure = 503
    jobFailure = 503
    await mount()
    expect(host.textContent).toContain('Instance status could not be loaded')
    expect(host.textContent).toContain('System jobs could not be loaded')
    expect(host.textContent).not.toContain('0 online')
    expect(host.textContent).not.toContain('0 in progress')
  })

  it('reviews a deterministic batch of 100 and discloses remaining eligible instances', async () => {
    instances.items = Array.from({ length: 101 }, (_, index) => offlineInstance(index))
    await mount()
    expect(host.textContent).toContain(
      'This review includes the first 100 eligible registrations. 1 additional registration will remain',
    )
    await click('Clean up offline instances')
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog.querySelectorAll('li')).toHaveLength(100)
    expect(dialog.textContent).toContain('offline-000 · ins_offline_000 (revision 1)')
    expect(dialog.textContent).not.toContain('ins_offline_100')
  })

  it('submits exact reviewed revisions, prevents duplicates, and waits for authoritative removal', async () => {
    await mount()
    await click('Clean up offline instances')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      'routex-2 · ins_offline (revision 7)',
    )
    holdCleanup = true
    await click('Confirm cleanup')
    await click('Cleaning up…')
    await until(() => expect(cleanupRequests()).toHaveLength(1))
    expect(host.textContent).toContain('routex-2')
    expect(JSON.parse(String(cleanupRequests()[0].data))).toEqual({
      instances: [{ id: 'ins_offline', revision: 7 }],
    })
    expect(cleanupRequests()[0].headers.get('X-CSRF-Token')).toBe('csrf-system')
    holdCleanup = false
    await act(async () => releaseCleanup?.())
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(host.textContent).toContain('1 offline registration was cleaned up')
    expect(host.textContent).not.toContain('routex-2')
  })

  it('blocks stale reviews and keeps a shifted bounded-list 5xx outcome uncertain', async () => {
    await mount()
    await click('Clean up offline instances')
    cleanupFailure = 409
    instances.items[1].heartbeat_revision = 8
    await click('Confirm cleanup')
    await until(() => expect(document.body.textContent).toContain('changed after this review'))
    expect(buttons('Confirm cleanup')).toHaveLength(0)
    await click('Reload latest instances')
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())

    const reviewed = instances.items.find((item) => item.id === 'ins_offline')!
    instances.items = [
      reviewed,
      ...Array.from({ length: 199 }, (_, index) => ({
        ...structuredClone(instanceSeed.items[0]),
        id: `ins_page_${index.toString().padStart(3, '0')}`,
        name: `page-${index.toString().padStart(3, '0')}`,
      })),
    ]
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['admin', 'system-status', 'instances'] })
    })
    await until(() => expect(host.textContent).toContain('page-198'))
    await click('Clean up offline instances')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      'routex-2 · ins_offline (revision 8)',
    )
    cleanupFailure = 503
    cleanupShiftOnFailure = true
    await click('Confirm cleanup')
    await until(() => expect(document.body.textContent).toContain('cleanup result is uncertain'))
    expect(buttons('Confirm cleanup')).toHaveLength(0)
    expect(host.textContent).not.toContain('offline registration was cleaned up')
    expect(host.querySelector('[aria-label="System instances"]')?.textContent).not.toContain(
      'routex-2',
    )
  })

  it('switches the cleanup review live and keeps English and Chinese catalogs aligned', async () => {
    await mount()
    await click('Clean up offline instances')
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('清理失联实例？')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      'routex-2 · ins_offline（版本 7）',
    )
    expect(host.textContent).toContain('系统任务')

    function entries(value: object, prefix = ''): string[] {
      return Object.entries(value)
        .flatMap(([key, item]) =>
          typeof item === 'object'
            ? entries(item, `${prefix}${key}.`)
            : [`${prefix}${key}:${(String(item).match(/{{.*?}}/g) ?? []).sort().join(',')}`],
        )
        .sort()
    }
    expect(entries(en)).toEqual(entries(zh))
  })
})
