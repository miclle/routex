import { limitFixture, teamFixture } from '@/views/resource-limits/fixture'
import { teamModelDetail, teamModelWorkspace } from '@/views/team-model-requests/fixture'
import { usageFixture } from '@/views/usage/fixture'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import {
  focusManager,
  onlineManager,
  QueryClient,
  QueryClientProvider,
} from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import client from '@/api/client'
import routes from '@/router'
import type { ResourceRecord } from '@/types/resources'
import type { DefaultLimitResetContext } from '@/types/default-limits'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root
let host: HTMLDivElement
let cache: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[]
let permissions: string[]
let failure: Record<string, number>
let team: ResourceRecord
let project: ResourceRecord
let restoreContext: DefaultLimitResetContext
const originalAdapter = client.defaults.adapter
const person = {
  id: 'rel_1',
  user_id: 'usr_1',
  name: 'Test Manager',
  email: 'manager@example.test',
}
beforeEach(() => {
  requests = []
  permissions = []
  failure = {}
  team = {
    id: 'tea_1',
    name: 'Research Team',
    description: 'Evaluation',
    status: 'active',
    model_ids: ['mdl_hidden'],
    created_at: '2026-09-23T00:00:00Z',
    members: [{ ...person, role: 'owner', status: 'active' }],
  }
  project = {
    id: 'prj_1',
    name: 'Search Project',
    description: 'Retrieval',
    status: 'active',
    model_ids: ['mdl_hidden'],
    created_at: '2026-09-23T00:00:00Z',
    creator_id: 'usr_1',
    managers: [person],
  }
  const limit = { ...teamFixture(), id: 'tea_1', team_id: 'tea_1' }
  restoreContext = {
    kind: 'team',
    id: 'tea_1',
    etag: 'c'.repeat(64),
    editable: true,
    applied_default_etag: null,
    limit,
    default_rule: {
      kind: 'team',
      etag: 'd'.repeat(64),
      rule_etag: 'e'.repeat(64),
      editable: true,
      platform_currency: 'USD',
      updated_at: '2026-10-05T00:00:00Z',
      policy: {
        tokens_5h: 0,
        tokens_7d: null,
        tokens_month: 500,
        money_month: '10.123456789012345678',
        currency: 'USD',
        rpm: null,
        tpm: 100,
        concurrency: 4,
      },
    },
  }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (failure[`${config.method} ${config.url}`]) {
      response.status = failure[`${config.method} ${config.url}`]
      throw new AxiosError('Fixture failure', '', config, undefined, response)
    }
    const body = config.data ? JSON.parse(config.data) : {}
    if (config.url === '/setup') response.data = { initialized: true }
    else if (config.url === '/auth/session')
      response.data = {
        user: { id: 'usr_1', name: 'Test Manager', email: person.email, role: 'member' },
        csrf_token: 'csrf-fixture',
      }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/admin/teams/creation-context') {
      response.headers.set('ETag', `"${'a'.repeat(64)}"`)
      response.data = {
        review_etag: 'a'.repeat(64),
        default_rule_etag: 'b'.repeat(64),
        platform_currency: null,
        editable_fields: [],
        default_policy: {},
      }
    } else if (config.url === '/teams/tea_1/limits/default-reset')
      response.data =
        config.method === 'get'
          ? structuredClone(restoreContext)
          : {
              kind: 'team',
              id: 'tea_1',
              saved: true,
              default_reset_etag: restoreContext.etag,
              applied_default_etag: restoreContext.default_rule.rule_etag,
              runtime_applied: true,
              limit: {
                ...restoreContext.limit,
                stored: { ...restoreContext.limit.stored, ...restoreContext.default_rule.policy },
              },
            }
    else if (config.url === '/projects/prj_1/overview')
      response.data = {
        project_id: 'prj_1',
        observed_at: '2026-10-03T00:00:00Z',
        counts: {
          managers: project.managers?.length ?? 0,
          models: project.model_ids.length,
          active_keys: 0,
          pending_requests: 0,
        },
        last_call_at: null,
        calls_available: true,
        monthly_quota: null,
        activities: [],
      }
    else if (config.url === '/team-model-requests' || config.url === '/teams/tea_1/model-requests')
      response.data = {
        items: [{ ...teamModelDetail('tea_1'), applicant_user_id: 'usr_1' }],
        total: 1,
        next_cursor: null,
      }
    else if (config.url === '/teams/tea_1/model-request-workspace')
      response.data = { ...teamModelWorkspace(), team_id: 'tea_1' }
    else if (config.url === '/teams/tea_1/roles')
      response.data = {
        team_id: team.id,
        role_ids: [],
        roles: [],
        effective_team_actions: [],
        actor_team_actions: permissions.filter((permission) =>
          ['teams.write', 'teams.models.write'].includes(permission),
        ),
        can_assign_roles: false,
        etag: 'a'.repeat(64),
      }
    else if (
      config.url?.includes('resource-model-candidates') ||
      config.url?.endsWith('/model-candidates')
    )
      response.data = { items: [{ id: 'mdl_new', name: 'New model' }] }
    else if (config.url?.includes('candidates'))
      response.data = {
        items: [
          { id: 'usr_1', name: person.name, email: person.email },
          { id: 'usr_2', name: 'Second manager', email: 'second@example.test' },
        ],
      }
    else if (config.url === '/admin/teams' && config.method === 'post') {
      team = {
        ...team,
        ...body,
        ...(body.creation_id ? { model_ids: [] } : {}),
        members: body.owner_ids.map((id: string) => ({
          ...person,
          user_id: id,
          role: 'owner',
          status: 'active',
        })),
      }
      response.data = body.creation_id
        ? {
            team,
            receipt: {
              creation_id: body.creation_id,
              team_id: team.id,
              created_at: team.created_at,
            },
            committed: true,
            runtime_applied: true,
            application_status: 'applied',
          }
        : team
    } else if (config.url === '/projects' && config.method === 'post') {
      project = { ...project, ...body, model_ids: [] }
      response.data = project
    } else if (config.url?.endsWith('/models') && config.method === 'put') {
      const target = config.url.includes('teams') ? team : project
      target.model_ids = body.model_ids
      response.data = target
    } else if (config.url?.endsWith('/managers') && config.method === 'put') {
      project.managers = body.user_ids.map((id: string) =>
        id === 'usr_1' ? person : { ...person, user_id: id, name: 'Second manager' },
      )
      response.data = project
    } else if (config.url?.endsWith('/members') && config.method === 'put') {
      team.members = body.members.map((member: object) => ({ ...person, ...member }))
      response.data = team
    } else if (config.url?.endsWith('/tea_1')) {
      if (config.method === 'patch') team = { ...team, ...body }
      response.data = team
    } else if (config.url?.endsWith('/prj_1')) {
      if (config.method === 'patch') project = { ...project, ...body }
      response.data = project
    } else if (config.url?.endsWith('/teams')) response.data = { items: [team], next_cursor: null }
    else if (config.url?.endsWith('/projects'))
      response.data = { items: [project], next_cursor: null }
    else response.data = { items: [], next_cursor: null }
    if (config.url?.endsWith('/limits')) {
      if (config.url.startsWith('/teams/')) {
        const member = config.url.includes('/members/')
        const base = teamFixture(member)
        const patch = { ...body }
        delete patch.reason
        response.data = {
          ...base,
          ...(config.method === 'put'
            ? { stored: { ...base.stored, ...patch }, etag: 'c'.repeat(64) }
            : {}),
          id: member ? 'usr_1' : 'tea_1',
          team_id: 'tea_1',
          editable_fields: permissions.includes('teams.tokens.write')
            ? member
              ? ['tokens_month']
              : ['tokens_5h', 'tokens_7d', 'tokens_month']
            : [],
        }
      } else response.data = limitFixture()
    }
    if (config.url?.endsWith('/usage')) response.data = usageFixture()
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  focusManager.setFocused(undefined)
  onlineManager.setOnline(true)
  cache.clear()
  client.defaults.adapter = originalAdapter
  host.remove()
})
async function mount(path: string) {
  router = createMemoryRouter(routes, { initialEntries: [path] })
  // Route modules must finish loading before polling rendered resource state.
  if (!router.state.initialized) {
    await new Promise<void>((resolve) => {
      const unsubscribe = router.subscribe((state) => {
        if (state.initialized) {
          unsubscribe()
          resolve()
        }
      })
    })
  }
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
}
async function until(assert: () => void) {
  for (let i = 0; i < 80; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 79) throw error
    }
  }
}
async function click(text: string) {
  await act(async () => {
    const button = [
      ...document.querySelectorAll<HTMLElement>('button,[role="tab"],[role="menuitem"]'),
    ].find((el) => el.textContent === text)
    expect(button).toBeDefined()
    button!.click()
  })
}
async function fill(name: string, value: string) {
  const input = document.querySelector<HTMLInputElement>(`[name="${name}"]`)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function submit(selector = 'form') {
  await act(async () => {
    document
      .querySelector(selector)!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}

describe('Team and Project resource workflows', () => {
  it('appends own Team request history to the existing allowed/available model tables without owner review authority', async () => {
    await mount('/teams/tea_1?tab=models')
    await until(() => expect(host.textContent).toContain('Original Team need'))
    expect(host.textContent).toContain('mdl_hidden')
    expect([...host.querySelectorAll('[role="tab"]')].map((tab) => tab.textContent)).toContain(
      'Model access',
    )
    expect(
      requests.some(
        (item) => item.url === '/team-model-requests' && item.params.team_id === 'tea_1',
      ),
    ).toBe(true)
    expect(requests.some((item) => item.url === '/teams/tea_1/model-request-workspace')).toBe(false)
    expect(requests.some((item) => item.url === '/teams/tea_1/model-requests')).toBe(false)
    expect(
      requests.some((item) => item.url === '/admin/members' || item.url === '/admin/teams'),
    ).toBe(false)
  })
  it('uses the scoped Team reviewer workspace only with independently granted model write authority', async () => {
    permissions = ['teams.models.write']
    await mount('/teams/tea_1?tab=models')
    await until(() => expect(host.textContent).toContain('Team model request history'))
    expect(host.textContent).toContain('mdl_hidden')
    expect(requests.some((item) => item.url === '/teams/tea_1/model-request-workspace')).toBe(true)
    expect(requests.some((item) => item.url === '/teams/tea_1/model-requests')).toBe(true)
    expect(
      requests
        .filter((item) => item.url === '/team-model-requests')
        .every((item) => item.params.team_id === 'tea_1'),
    ).toBe(true)
  })
  it('opens the addressable Team limit cards without granting owner writes', async () => {
    await mount('/teams/tea_1?tab=limits')
    await until(() => expect(host.textContent).toContain('Budget and quotas'))
    expect(host.textContent).toContain('Rate limits')
    expect(host.textContent).not.toContain('Edit limits')
    expect(requests.filter((request) => request.url === '/teams/tea_1/limits')).toHaveLength(1)
    expect(requests.some((request) => request.url?.includes('/admin/members'))).toBe(false)
  })
  it('lets the current owner inspect an exact member policy from the existing menu', async () => {
    await mount('/teams/tea_1?tab=members')
    await until(() =>
      expect(host.querySelector('[aria-label="More actions for Test Manager"]')).toBeTruthy(),
    )
    await act(async () =>
      host
        .querySelector<HTMLButtonElement>('[aria-label="More actions for Test Manager"]')!
        .click(),
    )
    await until(() => expect(document.body.textContent).toContain('Adjust member resources'))
    await click('Adjust member resources')
    await until(() => expect(document.body.textContent).toContain('Inherit parent'))
    expect(requests.some((request) => request.url === '/teams/tea_1/members/usr_1/limits')).toBe(
      true,
    )
    expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('Edit limits')
    expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('IP source')
  })
  it('renders only the minimal limits workspace without relationship or model queries', async () => {
    permissions = ['teams.tokens.write']
    team = {
      id: 'tea_1',
      name: 'Scoped Team',
      description: 'Policy review',
      status: 'active',
      resource_limit_workspace_only: true,
    } as ResourceRecord
    await mount('/teams/tea_1?tab=overview')
    await until(() => expect(host.textContent).toContain('Budget and quotas'))
    expect([...host.querySelectorAll('[role="tab"]')].map((tab) => tab.textContent)).toEqual([
      'Budgets, quotas and limits',
    ])
    expect(host.textContent).not.toContain('Member count')
    expect(
      requests.some((request) => /candidates|\/models|\/members|\/calls/.test(request.url ?? '')),
    ).toBe(false)
    await click('Edit limits')
    expect(host.querySelector<HTMLInputElement>('[aria-label="RPM"]')?.disabled).toBe(true)
  })
  it('retains an exact uncertain Team limit intent through focus and reconnect', async () => {
    permissions = ['teams.tokens.write']
    await mount('/teams/tea_1?tab=limits')
    await until(() => expect(host.textContent).toContain('Budget and quotas'))
    await click('Edit limits')
    async function setInput(label: string, value: string) {
      const input = host.querySelector<HTMLInputElement>(`[aria-label="${label}"]`)!
      await act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
          input,
          value,
        )
        input.dispatchEvent(new Event('input', { bubbles: true }))
      })
    }
    await setInput('Monthly token quota', '0')
    await setInput('Reason for change', 'Keep reviewed intent')
    failure['put /teams/tea_1/limits'] = 503
    await click('Save limits')
    await until(() => expect(host.textContent).toContain('This change may already be saved'))
    const original = requests.find(
      (request) => request.method === 'put' && request.url === '/teams/tea_1/limits',
    )!
    const metadataReads = requests.filter(
      (request) => request.url === '/teams/tea_1' && request.method === 'get',
    ).length
    await act(async () => {
      focusManager.setFocused(false)
      focusManager.setFocused(true)
      onlineManager.setOnline(false)
      onlineManager.setOnline(true)
      await new Promise((resolve) => setTimeout(resolve, 30))
    })
    expect(
      requests.filter((request) => request.url === '/teams/tea_1' && request.method === 'get'),
    ).toHaveLength(metadataReads)
    expect(host.textContent).toContain('This change may already be saved')
    expect(host.querySelector<HTMLInputElement>('[aria-label="Monthly token quota"]')!.value).toBe(
      '0',
    )
    delete failure['put /teams/tea_1/limits']
    await click('Retry application')
    await until(() => expect(host.textContent).toContain('Limits saved and applied.'))
    const retries = requests.filter(
      (request) => request.method === 'put' && request.url === '/teams/tea_1/limits',
    )
    expect(retries).toHaveLength(2)
    expect(retries[1].data).toBe(original.data)
    expect(retries[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  })
  it('keeps the six Project tabs and opens the Playground with only the Project ID', async () => {
    await mount('/projects/prj_1')
    await until(() => expect(host.textContent).toContain('Open in Playground'))
    expect([...host.querySelectorAll('[role="tab"]')].map((tab) => tab.textContent)).toEqual([
      'Overview',
      'Project API keys',
      'Resource configuration',
      'Usage',
      'Call records',
      'Settings',
    ])
    expect(
      host.querySelector<HTMLAnchorElement>('a[href="/playground?project=prj_1"]'),
    ).not.toBeNull()
    expect(host.textContent).not.toContain('rx_')
  })
  it('allows managers to inspect aggregate limits without platform write authority', async () => {
    await mount('/projects/prj_1?tab=resources')
    await until(() => expect(host.textContent).toContain('user_usr_fixture'))
    expect(host.textContent).not.toContain('Edit limits')
    expect(requests.some((request) => request.url === '/projects/prj_1/limits')).toBe(true)
    expect(
      requests.some((request) => request.url?.endsWith('/limits') && request.method === 'put'),
    ).toBe(false)
  })
  it('edits active Project limits inline only with projects.limits.write', async () => {
    permissions = ['projects.limits.write']
    await mount('/projects/prj_1?tab=resources')
    await until(() => expect(host.textContent).toContain('Edit limits'))
    await click('Edit limits')
    expect(host.querySelector('form[aria-label="Proposed policy"]')).not.toBeNull()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
  it('keeps inactive Project aggregate limits read-only even with write permission', async () => {
    permissions = ['projects.limits.write']
    project.status = 'disabled'
    await mount('/projects/prj_1?tab=resources')
    await until(() => expect(host.textContent).toContain('user_usr_fixture'))
    expect(host.textContent).not.toContain('Edit limits')
  })

  it('does not fetch global resource lists without the read permission', async () => {
    await mount('/admin/teams')
    await until(() => expect(host.textContent).toContain('Access denied'))
    expect(requests.some((r) => r.url === '/admin/teams')).toBe(false)
    expect(host.querySelector('a[href="/admin/teams"]')).toBeNull()
  })
  it('creates a reviewed Team with scoped owner candidates and retries only its immutable rejected intent', async () => {
    permissions = ['teams.write']
    await mount('/admin/teams/new')
    await until(() => expect(host.querySelector('form[aria-label="Create Team"]')).not.toBeNull())
    await until(() => expect(host.querySelector('[role="switch"]')).not.toBeNull())
    await fill('name', 'New Team')
    await act(async () => {
      document.querySelector<HTMLElement>('[role="switch"]')!.click()
    })
    failure['post /admin/teams'] = 400
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
    await act(async () => {
      ;[...document.querySelectorAll('button')]
        .find((button) => button.textContent === 'Confirm creation')!
        .click()
    })
    await until(() => expect(host.textContent).toContain('creation outcome remains unknown'))
    const request = requests.find((r) => r.method === 'post' && r.url === '/admin/teams')!
    expect(JSON.parse(request.data)).toEqual({
      creation_id: expect.stringMatching(/^[0-9a-f-]{36}$/),
      name: 'New Team',
      description: '',
      owner_ids: ['usr_1'],
    })
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf-fixture')
    expect(request.headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
    expect(requests.some((r) => r.url === '/admin/members')).toBe(false)
    delete failure['post /admin/teams']
    await click('Retry original creation')
    await until(() => expect(router.state.location.pathname).toBe('/admin/teams/tea_1'))
    const retried = requests.filter((r) => r.method === 'post' && r.url === '/admin/teams')[1]
    expect(retried.data).toBe(request.data)
    expect(retried.headers.get('If-Match')).toBe(request.headers.get('If-Match'))
  })
  it('shows Team membership read-only without fetching mutation candidates', async () => {
    await mount('/teams/tea_1?tab=members')
    await until(() => expect(host.querySelector('table')).not.toBeNull())
    expect(
      [...host.querySelectorAll('button')].find((el) => el.textContent === 'Add member')?.disabled,
    ).toBe(true)
    expect(requests.some((r) => r.url?.includes('candidates'))).toBe(false)
    expect(requests.some((r) => r.url === '/admin/members')).toBe(false)
  })
  it('lets an ordinary Project creator save metadata and add a manager through a scoped picker', async () => {
    await mount('/projects/prj_1?tab=settings')
    await until(() => expect(host.querySelector('[name="name"]')).not.toBeNull())
    await fill('name', 'Renamed Project')
    await submit('form[aria-label="Project settings"]')
    await until(() => expect(project.name).toBe('Renamed Project'))
    expect(host.textContent).not.toContain('Project lifecycle')
    await click('Add manager')
    await until(() =>
      expect(document.querySelectorAll('[role="dialog"] input[type="checkbox"]').length).toBe(1),
    )
    await act(async () => {
      document
        .querySelectorAll<HTMLInputElement>('[role="dialog"] input[type="checkbox"]')[0]
        .click()
    })
    expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain(person.email)
    await submit('[role="dialog"] form')
    await until(() => expect(project.managers).toHaveLength(2))
    expect(requests.some((r) => r.url === '/projects/prj_1/manager-candidates')).toBe(true)
    expect(requests.some((r) => r.url === '/admin/members')).toBe(false)
  })
  it('keeps model requests inside Project resource configuration and hides them from unrelated readers', async () => {
    await mount('/projects/prj_1?tab=resources')
    await until(() => expect(host.textContent).toContain('Project resource requests'))
    expect(requests.some((request) => request.url === '/projects/prj_1/requests')).toBe(true)
    expect([...host.querySelectorAll('[role="tab"]')].map((tab) => tab.textContent)).not.toContain(
      'Requests',
    )
    expect(
      [...host.querySelectorAll('button')].some(
        (button) => button.textContent === 'Request models',
      ),
    ).toBe(true)
    project = { ...project, managers: [] }
    await act(async () => {
      cache.setQueryData(['resources', 'projects', false, 'prj_1', 'usr_1'], project)
    })
    await until(() => expect(host.textContent).not.toContain('Project resource requests'))
  })
  it('preserves unseen grants while adding a model and leaves rejected changes recoverable', async () => {
    permissions = ['projects.models.write']
    await mount('/projects/prj_1?tab=resources')
    await until(() => expect(host.textContent).toContain('New model'))
    expect(host.textContent).toContain('mdl_hidden')
    await click('Add')
    failure['put /projects/prj_1/models'] = 409
    await submit('form[aria-label="Save allowed models"]')
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(project.model_ids).toEqual(['mdl_hidden'])
    delete failure['put /projects/prj_1/models']
    await submit('form[aria-label="Save allowed models"]')
    await until(() => expect(project.model_ids).toEqual(['mdl_hidden', 'mdl_new']))
    expect(requests.some((r) => r.url === '/admin/models' || r.url === '/models')).toBe(false)
    expect(requests.find((r) => r.method === 'put')?.headers.get('X-CSRF-Token')).toBe(
      'csrf-fixture',
    )
  })
  it('requires disabling before archival and keeps archived settings read-only', async () => {
    permissions = ['projects.write']
    await mount('/admin/projects/prj_1?tab=settings')
    await until(() => expect(host.textContent).toContain('Disable Project'))
    expect(host.textContent).not.toContain('Archive Project')
    await click('Disable Project')
    await submit('[role="dialog"] form')
    await until(() => expect(host.textContent).toContain('Archive Project'))
    await click('Archive Project')
    await submit('[role="dialog"] form')
    await until(() => expect(host.textContent).toContain('archived and read-only'))
    expect(
      document.querySelector<HTMLInputElement>('[name="name"]')?.closest('fieldset')?.disabled,
    ).toBe(true)
    expect(host.textContent).not.toContain('Reenable Project')
  })
  it('keeps a continuity conflict recoverable and sends the complete Team member set', async () => {
    permissions = ['teams.write']
    await mount('/teams/tea_1?tab=members')
    await until(() => expect(host.querySelector('table')).not.toBeNull())
    await act(async () => {
      host.querySelector<HTMLButtonElement>('[aria-label="More actions for Test Manager"]')!.click()
    })
    await until(() => expect(document.querySelector('[role="menu"]')).not.toBeNull())
    await click('Make member')
    failure['put /admin/teams/tea_1/members'] = 409
    await submit('[role="dialog"] form')
    await until(() =>
      expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull(),
    )
    expect(team.members?.[0].role).toBe('owner')
    expect(JSON.parse(requests.find((r) => r.method === 'put')!.data)).toEqual({
      members: [{ user_id: 'usr_1', role: 'member', status: 'active' }],
    })
  })
  it('switches resource settings language without losing an edited name', async () => {
    await mount('/projects/prj_1?tab=settings')
    await until(() => expect(host.querySelector('[name="name"]')).not.toBeNull())
    await fill('name', 'Preserved Project')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(host.textContent).toContain('保存设置')
    expect(host.querySelector<HTMLInputElement>('[name="name"]')?.value).toBe('Preserved Project')
  })
})

describe('mounted Usage routes and Project access', () => {
  it('mounts the addressable Team Calls tab for the current member using only own Team endpoints', async () => {
    await mount('/teams/tea_1?tab=calls')
    await until(() => expect(host.textContent).toContain('Your Team call records'))
    expect(requests.some((request) => request.url === '/teams/tea_1/calls')).toBe(true)
    expect(
      requests.some((request) =>
        ['/calls', '/admin/calls', '/teams/tea_1/usage'].includes(request.url ?? ''),
      ),
    ).toBe(false)
    expect(host.textContent).toContain('Only your own calls')
    expect(host.querySelector('[name="key_id"]')).toBeNull()
    await until(() => {
      const exportButton = [...host.querySelectorAll<HTMLButtonElement>('button')].find(
        (button) => button.textContent === 'Export CSV',
      )
      expect(exportButton?.disabled).toBe(false)
    })
    expect(requests.some((request) => request.url?.endsWith('/calls/export.csv'))).toBe(false)
  })
  it('does not grant Team call history to a nonmember with platform call authority', async () => {
    team.members = []
    permissions = ['teams.read_all', 'calls.read_all']
    await mount('/admin/teams/tea_1?tab=calls')
    await until(() => expect(host.textContent).toContain('Research Team'))
    expect(
      [...host.querySelectorAll('[role="tab"]')].some(
        (item) => item.textContent === 'Call records',
      ),
    ).toBe(false)
    expect(requests.some((request) => request.url === '/teams/tea_1/calls')).toBe(false)
  })
  it('mounts personal usage from its real route and scopes the request', async () => {
    await mount('/usage')
    await until(() => expect(host.textContent).toContain('key_usage_resource'))
    expect(requests.some((request) => request.url === '/usage')).toBe(true)
    expect(requests.some((request) => request.url === '/admin/usage')).toBe(false)
    expect(host.querySelector('a[href="/usage"]')).not.toBeNull()
  })
  it('does not query administrative usage without calls.read_all', async () => {
    await mount('/admin/usage')
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(requests.some((request) => request.url === '/admin/usage')).toBe(false)
    expect(host.querySelector('a[href="/admin/usage"]')).toBeNull()
  })
  it('mounts administrative usage only after permission resolution', async () => {
    permissions = ['calls.read_all']
    await mount('/admin/usage')
    await until(() => expect(host.textContent).toContain('key_usage_resource'))
    expect(requests.some((request) => request.url === '/admin/usage')).toBe(true)
    expect(host.querySelector('[name="connection_id"]')).not.toBeNull()
  })
  it('lets a current Project manager read archived history in the Usage tab', async () => {
    project.status = 'archived'
    await mount('/projects/prj_1?tab=usage')
    await until(() => expect(host.textContent).toContain('key_usage_resource'))
    expect(requests.some((request) => request.url === '/projects/prj_1/usage')).toBe(true)
    expect(requests.some((request) => request.url === '/usage')).toBe(false)
    expect(
      [...host.querySelectorAll('[role="tab"]')].some((tab) => tab.textContent === 'Usage'),
    ).toBe(true)
  })
  it('hides the tab and avoids queries for a former manager even with Project read access', async () => {
    project.managers = []
    permissions = ['projects.read_all']
    await mount('/projects/prj_1?tab=usage')
    await until(() => expect(host.textContent).toContain('Search Project'))
    expect(
      [...host.querySelectorAll('[role="tab"]')].some((tab) => tab.textContent === 'Usage'),
    ).toBe(false)
    expect(requests.some((request) => request.url === '/projects/prj_1/usage')).toBe(false)
  })
  it('keeps Project history available to platform calls authority without manager membership', async () => {
    project.managers = []
    permissions = ['projects.read_all', 'calls.read_all']
    await mount('/projects/prj_1?tab=usage')
    await until(() => expect(host.textContent).toContain('key_usage_resource'))
    expect(requests.some((request) => request.url === '/projects/prj_1/usage')).toBe(true)
    expect(host.querySelector('[name="connection_id"]')).toBeNull()
  })
})

it('retains the exact Team restoration through actual detail unmount, permission errors and recovery', async () => {
  permissions = ['teams.tokens.write', 'teams.money.write', 'teams.rates.write']
  await mount('/teams/tea_1?tab=limits')
  await until(() => expect(host.textContent).toContain('Restore defaults'))
  await click('Restore defaults')
  await until(() => expect(document.querySelector('input[aria-label="Reason"]')).toBeTruthy())
  const reason = document.querySelector<HTMLInputElement>('input[aria-label="Reason"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      reason,
      'Team retained reason',
    )
    reason.dispatchEvent(new Event('input', { bubbles: true }))
  })
  failure['post /teams/tea_1/limits/default-reset'] = 503
  await click('Confirm restoration')
  await until(() => expect(document.body.textContent).toContain('Retry original request'))
  const writes = () =>
    requests.filter(
      (request) => request.method === 'post' && request.url?.endsWith('/default-reset'),
    )
  const original = writes()[0]
  failure['get /teams/tea_1'] = 503
  await act(async () =>
    cache.refetchQueries({ queryKey: ['resources', 'teams', false, 'tea_1', 'usr_1'] }),
  )
  await until(() => expect(host.textContent).not.toContain('Research Team'))
  expect(document.body.textContent).not.toContain('Captured default policy')
  expect(document.body.textContent).not.toContain('Retry original request')
  failure['get /teams/tea_1'] = 0
  await act(async () =>
    cache.refetchQueries({ queryKey: ['resources', 'teams', false, 'tea_1', 'usr_1'] }),
  )
  await until(() =>
    expect(document.querySelector<HTMLInputElement>('input[aria-label="Reason"]')?.value).toBe(
      'Team retained reason',
    ),
  )
  failure['get /auth/permissions'] = 503
  await act(async () => cache.refetchQueries({ queryKey: ['permissions', 'usr_1'] }))
  await until(() => expect(document.body.textContent).not.toContain('Captured default policy'))
  expect(document.body.textContent).not.toContain('Retry original request')
  expect(writes()).toHaveLength(1)
  failure['get /auth/permissions'] = 0
  await act(async () => cache.refetchQueries({ queryKey: ['permissions', 'usr_1'] }))
  await until(() =>
    expect(document.querySelector<HTMLInputElement>('input[aria-label="Reason"]')?.value).toBe(
      'Team retained reason',
    ),
  )
  failure['post /teams/tea_1/limits/default-reset'] = 409
  await click('Retry original request')
  await until(() => expect(writes()).toHaveLength(2))
  await until(() =>
    expect(
      [...document.querySelectorAll<HTMLButtonElement>('button')].find(
        (button) => button.textContent === 'Retry original request',
      )?.disabled,
    ).toBe(false),
  )
  failure['post /teams/tea_1/limits/default-reset'] = 0
  await click('Retry original request')
  await until(() =>
    expect(host.textContent).toContain('Defaults restored and applied to the current runtime.'),
  )
  expect(writes()).toHaveLength(3)
  expect(
    writes().every(
      (request) =>
        request.data === original.data &&
        request.headers.get('If-Match') === original.headers.get('If-Match'),
    ),
  ).toBe(true)
  expect(
    requests.some((request) => request.url === '/admin/teams' || request.url === '/admin/members'),
  ).toBe(false)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})

it('a Team tab change destroys the old transient owner without replay or outcome claims', async () => {
  permissions = ['teams.tokens.write', 'teams.money.write', 'teams.rates.write']
  await mount('/teams/tea_1?tab=limits')
  await until(() => expect(host.textContent).toContain('Restore defaults'))
  await click('Restore defaults')
  await until(() => expect(document.querySelector('input[aria-label="Reason"]')).toBeTruthy())
  const reason = document.querySelector<HTMLInputElement>('input[aria-label="Reason"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      reason,
      'Old tab intent',
    )
    reason.dispatchEvent(new Event('input', { bubbles: true }))
  })
  failure['post /teams/tea_1/limits/default-reset'] = 409
  await click('Confirm restoration')
  await until(() => expect(document.body.textContent).toContain('Retry original request'))
  await act(async () => router.navigate('/teams/tea_1?tab=members'))
  await until(() => expect(document.body.textContent).not.toContain('Retry original request'))
  await act(async () => router.navigate('/teams/tea_1?tab=limits'))
  await until(() => expect(host.textContent).toContain('Restore defaults'))
  await click('Restore defaults')
  await until(() =>
    expect(document.querySelector<HTMLInputElement>('input[aria-label="Reason"]')?.value).toBe(''),
  )
  expect(document.body.textContent).not.toContain('previous outcome remains unknown')
  expect(document.body.textContent).not.toContain('Defaults restored and applied')
  expect(
    requests.filter(
      (request) => request.method === 'post' && request.url?.endsWith('/default-reset'),
    ),
  ).toHaveLength(1)
})
