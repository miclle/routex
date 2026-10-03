import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import { createProject, getProjectOverview, getProjectCreationManagers } from '@/api/resources'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { ProjectOverviewRecord, ResourceRecord } from '@/types/resources'
import i18n from '@/i18n'
import CreateResourcePage from './create'
import ProjectOverview from './project-overview'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root
let host: HTMLDivElement
let cache: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[]
let permissions: string[]
let identity: Session
let data: ProjectOverviewRecord
let postFailure: number
let overviewFailure: number
let holdOverview: (() => Promise<void>) | undefined
let holdPost: (() => Promise<void>) | undefined
let creationReply: ResourceRecord | undefined
let candidateReply: unknown
const originalAdapter = client.defaults.adapter
const stamp = '2026-10-03T02:00:00Z'
const manager = { id: 'usr_one', name: 'Current creator', email: 'creator@example.invalid' }
const other = { id: 'usr_two', name: 'Second manager', email: 'second@example.invalid' }
const resource: ResourceRecord = {
  id: 'prj_one',
  name: 'Private Project',
  description: 'Private business context',
  status: 'active',
  created_at: stamp,
  creator_id: manager.id,
  model_ids: ['mdl_one'],
  managers: [{ ...manager, id: 'pmg_one', user_id: manager.id }],
}
function overview(): ProjectOverviewRecord {
  return {
    project_id: 'prj_one',
    observed_at: stamp,
    counts: { managers: 1, models: 1, active_keys: 2, pending_requests: 1 },
    calls_available: true,
    last_call_at: stamp,
    monthly_quota: {
      tokens_month: 100,
      money_month: '10.000000000000000001',
      currency: 'USD',
      platform_currency: 'USD',
      policy_etag: 'lim_one',
      usage: {
        as_of: stamp,
        time_zone: 'UTC',
        month_start: '2026-10-01T00:00:00Z',
        month_end: '2026-11-01T00:00:00Z',
        covered: true,
        tokens_used: '50',
        tokens_held: '5',
        tokens_unknown: 0,
        money_used: { USD: '1.000000000000000001', CNY: '2.5' },
        money_held: { USD: '0.000000000000000001' },
        money_unknown: 0,
      },
    },
    activities: [
      {
        id: 'aud_one',
        kind: 'project_created',
        created_at: stamp,
        actor_name: null,
        status: 'committed',
      },
    ],
  }
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  requests = []
  permissions = []
  data = overview()
  postFailure = 0
  overviewFailure = 0
  holdOverview = undefined
  holdPost = undefined
  creationReply = undefined
  candidateReply = undefined
  identity = { user: { ...manager, role: 'member' }, csrf_token: 'current-csrf' }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } },
  })
  cache.setQueryData(sessionKey, identity)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session') response.data = identity
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/project-creation-resources')
      response.data = {
        review_etag: 'a'.repeat(64),
        platform_currency: 'USD',
        can_set_models: false,
        can_set_limits: false,
        can_request_resources: false,
      }
    else if (config.url === '/projects/creation-manager-candidates')
      response.data = candidateReply ?? { items: config.params.q ? [other] : [manager, other] }
    else if (config.url?.endsWith('/overview')) {
      await holdOverview?.()
      response.status = overviewFailure || 200
      response.data = { ...data, project_id: config.url.split('/')[2] }
    } else if (config.url === '/projects' && config.method === 'post') {
      await holdPost?.()
      const body = JSON.parse(config.data)
      response.status = postFailure || 201
      response.data = creationReply ?? {
        ...resource,
        ...body,
        creator_id: identity.user.id,
        model_ids: [],
        managers: body.manager_ids.map((id: string) => ({
          ...(id === manager.id ? manager : other),
          id: `pmg_${id}`,
          user_id: id,
        })),
      }
      if (body.creation_id)
        response.data = {
          project: response.data,
          committed: true,
          receipt: {
            creation_id: body.creation_id,
            project_id: resource.id,
            created_at: stamp,
            initial_request_ids: [],
          },
          runtime_applied: true,
          application_status: 'applied',
        }
    }
    if (response.status >= 400)
      throw new AxiosError('Current scope denied', '', config, undefined, response)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
})
async function mount(mode: 'create' | 'overview', id = resource.id) {
  router = createMemoryRouter(
    [
      { path: '/projects/new', element: <CreateResourcePage kind="projects" /> },
      {
        path: '/projects/:id',
        element: <ProjectOverview resource={{ ...resource, id }} isManager canKeys />,
      },
    ],
    { initialEntries: [mode === 'create' ? '/projects/new' : `/projects/${id}`] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
}
async function until(assertion: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assertion()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
async function fill(selector: string, value: string) {
  const input = host.querySelector<HTMLInputElement>(selector)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function click(selector: string) {
  await act(async () => host.querySelector<HTMLElement>(selector)!.click())
}
async function submit() {
  await act(async () =>
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
}
async function createReady() {
  await mount('create')
  await until(() => expect(host.querySelectorAll('input[type="checkbox"]')).toHaveLength(2))
  await fill('[name="name"]', 'Created Project')
}
function postRequests() {
  return requests.filter((request) => request.method === 'post')
}

describe('Project creation with current manager selection', () => {
  it('retains the ordinary creator and selected managers across bounded literal searches', async () => {
    await createReady()
    expect(host.querySelector<HTMLInputElement>('input[type="checkbox"]')?.disabled).toBe(true)
    await click('input[type="checkbox"]:not(:disabled)')
    await fill('[aria-label="Search Project managers"]', 'Second%_')
    await until(() => expect(host.querySelectorAll('input[type="checkbox"]')).toHaveLength(1))
    expect(host.querySelector('[aria-label="Selected Project managers"]')?.textContent).toContain(
      manager.name,
    )
    await submit()
    await until(() => expect(host.textContent).toContain('The creation was committed.'))
    expect(router.state.location.pathname).toBe('/projects/new')
    await click('a[href="/projects/prj_one"]')
    expect(router.state.location.pathname).toBe('/projects/prj_one')
    expect(JSON.parse(postRequests()[0].data)).toEqual({
      creation_id: expect.any(String),
      name: 'Created Project',
      description: '',
      manager_ids: [manager.id, other.id],
    })
    expect(postRequests()[0].headers.get('X-CSRF-Token')).toBe('current-csrf')
    expect(requests.some((request) => request.url === '/admin/members')).toBe(false)
    expect(requests.find((request) => request.params?.q === 'Second%_')?.url).toBe(
      '/projects/creation-manager-candidates',
    )
  })
  it('permits creator removal only with current projects.write, not an admin role label', async () => {
    identity.user.role = 'admin'
    await createReady()
    expect(
      host.querySelector<HTMLButtonElement>('[aria-label="Remove manager Current creator"]')
        ?.disabled,
    ).toBe(true)
    await act(async () => cache.setQueryData(['permissions', manager.id], ['projects.write']))
    await until(() =>
      expect(
        host.querySelector<HTMLButtonElement>('[aria-label="Remove manager Current creator"]')
          ?.disabled,
      ).toBe(false),
    )
    await click('input[type="checkbox"]:not(:checked)')
    await click('[aria-label="Remove manager Current creator"]')
    await submit()
    await until(() => expect(postRequests()).toHaveLength(1))
    expect(JSON.parse(postRequests()[0].data).manager_ids).toEqual([other.id])
  })
  it('blocks an empty complete manager set and restores the creator if write authority disappears', async () => {
    permissions = ['projects.write']
    await createReady()
    await click('[aria-label="Remove manager Current creator"]')
    await submit()
    expect(postRequests()).toHaveLength(0)
    expect(host.textContent).toContain('Select at least one Project manager.')
    await act(async () => cache.setQueryData(['permissions', manager.id], []))
    await until(() =>
      expect(host.querySelector('[aria-label="Selected Project managers"]')?.textContent).toContain(
        manager.name,
      ),
    )
    await submit()
    await until(() => expect(postRequests()).toHaveLength(1))
    expect(JSON.parse(postRequests()[0].data).manager_ids).toEqual([manager.id])
  })
  it('retains a rejected draft but freezes an unknown creation outcome without repeating POST', async () => {
    await createReady()
    await click('input[type="checkbox"]:not(:disabled)')
    postFailure = 400
    await submit()
    await until(() => expect(host.textContent).toContain('Creation was rejected.'))
    expect(host.querySelector<HTMLInputElement>('[name="name"]')?.value).toBe('Created Project')
    await act(async () =>
      [...host.querySelectorAll('button')]
        .find((button) => button.textContent === 'Review current creation context')!
        .click(),
    )
    postFailure = 503
    await submit()
    await until(() => expect(host.textContent).toContain('Creation may already be saved.'))
    await submit()
    expect(postRequests()).toHaveLength(2)
    expect(router.state.location.pathname).toBe('/projects/new')
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).not.toContain('Creation may already be saved.')
    expect(host.querySelector<HTMLInputElement>('[name="name"]')?.value).toBe('Created Project')
  })
  it('ignores a late creation acknowledgement after the actor changes', async () => {
    await createReady()
    let release!: () => void
    const wait = new Promise<void>((resolve) => {
      release = resolve
    })
    holdPost = () => wait
    await submit()
    await until(() => expect(postRequests()).toHaveLength(1))
    await act(async () => {
      identity = { ...identity, user: { ...identity.user, id: 'usr_new' } }
      cache.setQueryData(sessionKey, identity)
    })
    await act(async () => release())
    expect(router.state.location.pathname).toBe('/projects/new')
    await until(() => expect(host.querySelector<HTMLInputElement>('[name="name"]')?.value).toBe(''))
  })
})

describe('Authoritative scoped Project overview', () => {
  it('renders the reference cards and recorded activity with exact decimals and unknown actors', async () => {
    await mount('overview')
    await until(() => expect(host.textContent).toContain('Monthly resource usage'))
    expect(host.textContent).toContain('Project runtime overview')
    expect(host.textContent).toContain('Last actual call')
    expect(host.textContent).toContain('Settled: 1.000000000000000001 USD')
    expect(host.textContent).toContain('Settled: 2.5 CNY')
    expect(host.textContent).toContain('Held tokens: 5')
    expect(host.querySelectorAll('[role="meter"]')).toHaveLength(2)
    expect(host.querySelector('[aria-label="Tokens"]')?.getAttribute('aria-valuenow')).toBe('50')
    expect(host.querySelector('table tbody')?.textContent).toContain('Unknown')
    expect(host.querySelector('table tbody')?.textContent).not.toContain(manager.name)
    expect(requests.filter((request) => request.url?.endsWith('/overview'))).toHaveLength(1)
    expect(requests.every((request) => !request.url?.includes('/admin/'))).toBe(true)
  })
  it('keeps unavailable counts and quota distinct from recorded zero and hides unavailable Key actions', async () => {
    data.counts.active_keys = null
    data.counts.pending_requests = null
    data.monthly_quota = null
    data.calls_available = false
    data.last_call_at = null
    data.activities = []
    await mount('overview')
    await until(() => expect(host.textContent).toContain('No recorded activity.'))
    expect(host.textContent).toContain('Unavailable')
    expect(host.textContent).not.toContain('Manage Project Keys')
    expect(host.textContent).not.toContain('Project request is pending')
    expect(host.querySelector('[role="meter"]')).toBeNull()
  })
  it('shows known subtotals and holds without fabricating progress for unknown coverage', async () => {
    const usage = data.monthly_quota!.usage!
    usage.covered = false
    usage.tokens_unknown = 1
    usage.money_unknown = 1
    usage.tokens_used = '9007199254740993'
    usage.money_used = { USD: '123456789012345678901.000000000000000001' }
    await mount('overview')
    await until(() => expect(host.textContent).toContain('Coverage is incomplete'))
    expect(host.textContent).toContain('9,007,199,254,740,993')
    expect(host.textContent).toContain('123456789012345678901.000000000000000001 USD')
    expect(host.querySelector('[role="meter"]')).toBeNull()
    expect(host.textContent).toContain('Unknown token usage: 1')
  })
  it('preserves zero caps, unlimited money and empty currency maps without division or fake amounts', async () => {
    data.monthly_quota!.tokens_month = 0
    data.monthly_quota!.money_month = null
    data.monthly_quota!.currency = ''
    data.monthly_quota!.usage!.tokens_used = '0'
    data.monthly_quota!.usage!.money_used = {}
    await mount('overview')
    await until(() => expect(host.textContent).toContain('No recorded settled amounts.'))
    expect(host.textContent).toContain('0 / 0')
    expect(host.textContent).toContain('Unlimited')
    expect(host.querySelector('[role="meter"]')).toBeNull()
    expect(host.textContent).not.toContain('Settled: 0')
  })
  it.each([403, 404, 401])(
    'hides cached private cards/actions during a held refresh and after %i',
    async (status) => {
      await mount('overview')
      await until(() => expect(host.textContent).toContain('Private business context'))
      let release!: () => void
      const wait = new Promise<void>((resolve) => {
        release = resolve
      })
      holdOverview = () => wait
      overviewFailure = status
      let refetch!: Promise<void>
      await act(async () => {
        refetch = cache.invalidateQueries({ queryKey: ['project-overview', manager.id, 'prj_one'] })
      })
      await until(() => expect(host.textContent).not.toContain('Private business context'))
      expect(host.textContent).not.toContain('Manage Project Keys')
      await act(async () => {
        release()
        await refetch
      })
      expect(host.textContent).not.toContain('Monthly resource usage')
      expect(host.textContent).not.toContain(manager.email)
    },
  )
  it('does not display cached data from a different actor and rejects a late old actor response', async () => {
    await mount('overview')
    await until(() => expect(host.textContent).toContain('Monthly resource usage'))
    let release!: () => void
    const wait = new Promise<void>((resolve) => {
      release = resolve
    })
    holdOverview = () => wait
    await act(async () => {
      identity = { ...identity, user: { ...identity.user, id: 'usr_new' } }
      cache.setQueryData(sessionKey, identity)
    })
    await until(() => expect(host.textContent).not.toContain('Private business context'))
    await until(() =>
      expect(requests.filter((request) => request.url?.endsWith('/overview'))).toHaveLength(2),
    )
    overviewFailure = 403
    await act(async () => release())
    await until(() =>
      expect(cache.getQueryState(['project-overview', 'usr_new', 'prj_one'])?.status).toBe('error'),
    )
    expect(host.textContent).not.toContain('Project runtime overview')
  })
  it('switches visible server observations to Chinese without changing exact amounts', async () => {
    await mount('overview')
    await until(() => expect(host.textContent).toContain('Monthly resource usage'))
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain('1.000000000000000001 USD')
    expect(host.textContent).not.toContain('Monthly resource usage')
    expect(host.querySelector('[role="meter"]')).not.toBeNull()
  })
})

describe('Project API projection acceptance', () => {
  it.each(['wrong-scope', 'six-activities', 'unknown-kind', 'unsafe-count', 'malformed-money'])(
    'rejects %s without exposing partial data',
    async (variant) => {
      client.defaults.adapter = async (config) => {
        const value = structuredClone(data)
        if (variant === 'wrong-scope') value.project_id = 'prj_other'
        if (variant === 'six-activities')
          value.activities = Array.from({ length: 6 }, (_, index) => ({
            ...value.activities[0],
            id: `aud_${index}`,
          }))
        if (variant === 'unknown-kind')
          (value.activities[0] as { kind: string }).kind = 'key_created'
        if (variant === 'unsafe-count') value.counts.active_keys = Number.MAX_SAFE_INTEGER + 1
        if (variant === 'malformed-money') value.monthly_quota!.usage!.money_used.USD = '1e6'
        return { config, status: 200, statusText: '', headers: new AxiosHeaders(), data: value }
      }
      await expect(getProjectOverview('prj_one')).rejects.toThrow()
    },
  )
  it('rejects duplicate or overflowing manager candidates', async () => {
    candidateReply = { items: [manager, manager] }
    await expect(getProjectCreationManagers('')).rejects.toThrow()
    candidateReply = {
      items: Array.from({ length: 51 }, (_, index) => ({ ...manager, id: `usr_${index}` })),
    }
    await expect(getProjectCreationManagers('')).rejects.toThrow()
  })
  it('treats a creation acknowledgement with different managers or implicit models as uncertain', async () => {
    creationReply = { ...resource, model_ids: [] }
    await expect(
      createProject(
        { name: resource.name, description: resource.description, manager_ids: [other.id] },
        'csrf',
        manager.id,
        [other.id],
      ),
    ).rejects.toThrow('Unconfirmed')
    creationReply = { ...resource }
    await expect(
      createProject(
        { name: resource.name, description: resource.description, manager_ids: [manager.id] },
        'csrf',
        manager.id,
        [manager.id],
      ),
    ).rejects.toThrow('Unconfirmed')
  })
})
