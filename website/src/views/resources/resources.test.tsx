import { limitFixture } from '@/views/resource-limits/fixture'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import client from '@/api/client'
import routes from '@/router'
import type { ResourceRecord } from '@/types/resources'
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
    else if (config.url?.includes('resource-model-candidates'))
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
        members: body.owner_ids.map((id: string) => ({
          ...person,
          user_id: id,
          role: 'owner',
          status: 'active',
        })),
      }
      response.data = team
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
    if (config.url?.endsWith('/limits')) response.data = limitFixture()
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
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
  it('creates a Team with scoped owner candidates and retries a server rejection', async () => {
    permissions = ['teams.write']
    await mount('/admin/teams/new')
    await until(() => expect(host.querySelector('form[aria-label="Create Team"]')).not.toBeNull())
    await fill('name', 'New Team')
    await act(async () => {
      document.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click()
    })
    failure['post /admin/teams'] = 400
    await submit()
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    const request = requests.find((r) => r.method === 'post' && r.url === '/admin/teams')!
    expect(JSON.parse(request.data)).toEqual({
      name: 'New Team',
      description: '',
      owner_ids: ['usr_1'],
    })
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf-fixture')
    expect(requests.some((r) => r.url === '/admin/members')).toBe(false)
    delete failure['post /admin/teams']
    await submit()
    await until(() => expect(router.state.location.pathname).toBe('/admin/teams/tea_1'))
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
    await until(() => expect(host.textContent).toContain('Model access requests'))
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
      cache.setQueryData(['resources', 'projects', false, 'prj_1'], project)
    })
    await until(() => expect(host.textContent).not.toContain('Model access requests'))
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
