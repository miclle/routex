import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/offboarding'
import zh from '@/i18n/locales/zh/offboarding'
import type { OffboardingCase, OffboardingInventory } from '@/types/offboarding'
import OffboardingPage from './index'
import { buildAssignments, needsEmergencySuccessor } from './assignment-logic'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root,
  container: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let inventory: OffboardingInventory,
  requests: InternalAxiosRequestConfig[],
  permissions: string[],
  role: 'admin' | 'member',
  failure: number
const oldAdapter = client.defaults.adapter
const path = '/admin/members/usr_target/offboarding'
beforeEach(() => {
  i18n.addResourceBundle('en', 'offboarding', en, true, true)
  i18n.addResourceBundle('zh', 'offboarding', zh, true, true)
  requests = []
  permissions = ['members.read', 'members.write']
  role = 'admin'
  failure = 0
  inventory = {
    user_id: 'usr_target',
    disabled: false,
    offboarded_at: null,
    last_administrator: false,
    inventory_version: 'digest_original',
    personal_keys: [{ id: 'key_one', name: 'Application', prefix: 'rx_masked', status: 'active' }],
    projects: [
      {
        id: 'prj_one',
        name: 'Project One',
        status: 'active',
        requires_successor: true,
        people: [
          { user_id: 'usr_target', name: 'Departing', disabled: false, role: '', status: '' },
        ],
      },
    ],
    teams: [
      {
        id: 'tem_one',
        name: 'Team One',
        status: 'active',
        requires_successor: true,
        people: [
          {
            user_id: 'usr_target',
            name: 'Departing',
            disabled: false,
            role: 'owner',
            status: 'active',
          },
        ],
      },
    ],
    cases: [],
  }
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
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
    if (config.url === '/auth/session')
      response.data = {
        user: { id: 'usr_admin', name: 'Admin', email: 'admin@example.invalid', role },
        csrf_token: 'test-csrf',
      }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/admin/members/usr_target')
      response.data = {
        id: 'usr_target',
        name: 'Departing',
        email: 'departing@example.invalid',
        role: 'member',
        disabled: inventory.disabled,
        created_at: '2026-09-23T00:00:00Z',
        role_ids: [],
      }
    else if (config.url === '/admin/members')
      response.data = {
        items: [
          {
            id: 'usr_successor',
            name: 'Successor',
            email: 'successor@example.invalid',
            role: 'member',
            disabled: false,
            role_ids: [],
            created_at: '2026-09-23T00:00:00Z',
          },
        ],
        next_cursor: null,
      }
    else if (config.url === path) response.data = structuredClone(inventory)
    else if (config.method === 'post') {
      if (failure) {
        response.status = failure
        if (config.validateStatus?.(failure)) return response
        throw new AxiosError('Failure', '', config, undefined, response)
      }
      const body = JSON.parse(config.data)
      const completed = !config.url?.endsWith('/plans')
      const record: OffboardingCase = {
        id: 'ofb_one',
        request_id: body.request_id ?? 'same-request',
        user_id: 'usr_target',
        actor_id: 'usr_admin',
        mode: config.url?.endsWith('/emergency') ? 'emergency' : 'planned',
        status: completed ? 'completed' : 'ready_to_complete',
        reason: body.reason ?? 'Planned departure',
        planned_at: body.planned_at ?? null,
        created_at: '2026-09-23T00:00:00Z',
        completed_at: completed ? '2026-09-23T01:00:00Z' : null,
        completed_by: completed ? 'usr_admin' : '',
        inventory_version: inventory.inventory_version,
        assignments: {
          project_assignments: body.project_assignments ?? [],
          team_assignments: body.team_assignments ?? [],
        },
      }
      inventory.cases = [record]
      if (completed) {
        inventory.disabled = true
        inventory.offboarded_at = record.completed_at
      }
      response.data = structuredClone(record)
    }
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  client.defaults.adapter = oldAdapter
  container.remove()
})
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
async function mount() {
  router = createMemoryRouter(
    [{ path: '/admin/members/:memberId/offboarding', element: <OffboardingPage /> }],
    { initialEntries: [path] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(document.body.textContent).toContain('Departing'))
}
function button(text: string, scope: ParentNode = document) {
  const found = [...scope.querySelectorAll('button')].find((b) => b.textContent === text)
  expect(found, `button ${text}`).toBeTruthy()
  return found!
}
async function click(element: HTMLElement) {
  await act(async () => element.click())
}
async function fill(name: string, value: string) {
  await act(async () => {
    const input = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(
      `[name="${name}"]`,
    )!
    expect(input).toBeTruthy()
    const proto =
      input instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function submit() {
  await act(async () =>
    document
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
}
async function plan() {
  await click(button('Prepare handover'))
  await until(() => expect(document.querySelectorAll('fieldset button').length).toBeGreaterThan(0))
  await fill('reason', 'Planned departure')
  await fill('planned_at', '2026-10-01T09:00')
  for (const fieldset of document.querySelectorAll('fieldset'))
    await click(button('Successor', fieldset))
  await click(document.querySelector<HTMLElement>('[role="switch"]')!)
}
const writes = () => requests.filter((r) => r.method === 'post')

describe('offboarding workflow', () => {
  it('saves explicit successors without executing and requires a separate completion confirmation', async () => {
    await mount()
    await plan()
    await submit()
    await until(() => expect(document.body.textContent).toContain('Plan saved.'))
    expect(writes()).toHaveLength(1)
    const payload = JSON.parse(writes()[0].data)
    expect(payload.inventory_version).toBe('digest_original')
    expect(payload.project_assignments).toEqual([
      { project_id: 'prj_one', manager_user_ids: ['usr_successor'] },
    ])
    expect(payload.team_assignments).toEqual([
      {
        team_id: 'tem_one',
        owner_user_ids: ['usr_successor'],
        add_member_user_ids: ['usr_successor'],
      },
    ])
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('test-csrf')
    expect(inventory.disabled).toBe(false)
    await click(button('Complete handover'))
    expect(writes()).toHaveLength(1)
    await click(button('Confirm completion'))
    await until(() => expect(inventory.disabled).toBe(true))
    await until(() =>
      expect(document.body.textContent).toContain('Project keys and history were preserved.'),
    )
  })
  it('blocks missing membership acknowledgment and refreshes stale plans before a new review', async () => {
    await mount()
    await plan()
    await click(document.querySelector<HTMLElement>('[role="switch"]')!)
    await submit()
    expect(writes()).toHaveLength(0)
    expect(document.body.textContent).toContain('acknowledge any Team additions')
    await click(document.querySelector<HTMLElement>('[role="switch"]')!)
    failure = 409
    await submit()
    await until(() =>
      expect(document.body.textContent).toContain('Responsibilities or successors changed.'),
    )
    expect(button('Save reviewed plan').disabled).toBe(true)
    inventory.inventory_version = 'digest_updated'
    failure = 0
    await click(button('Refresh and review again'))
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await plan()
    await submit()
    await until(() => expect(writes()).toHaveLength(2))
    expect(JSON.parse(writes()[1].data).inventory_version).toBe('digest_updated')
    expect(JSON.parse(writes()[1].data).request_id).not.toBe(
      JSON.parse(writes()[0].data).request_id,
    )
  })
  it('does not cache password proofs and retries uncertain emergency results with the same identity', async () => {
    inventory.teams = []
    await mount()
    await click(button('Emergency disable'))
    await until(() => expect(document.querySelector('[name="current_password"]')).not.toBeNull())
    await fill('reason', 'Incident response')
    await fill('current_password', 'test-only-sensitive-password')
    failure = 503
    await submit()
    await until(() =>
      expect(document.body.textContent).toContain('outcome may already be committed'),
    )
    expect(document.querySelector<HTMLInputElement>('[name="current_password"]')?.value).toBe('')
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((q) => q.state),
      ),
    ).not.toContain('test-only-sensitive-password')
    const first = JSON.parse(writes()[0].data)
    failure = 0
    await fill('current_password', 'test-only-new-password')
    await submit()
    await until(() => expect(inventory.disabled).toBe(true))
    expect(JSON.parse(writes()[1].data).request_id).toBe(first.request_id)
    expect(JSON.parse(writes()[1].data).reason).toBe(first.reason)
    await until(() => expect(document.querySelector('[name="current_password"]')).toBeNull())
  })
  it('handles failed password reauthentication locally and clears the input on dismissal', async () => {
    inventory.teams = []
    await mount()
    await click(button('Emergency disable'))
    await fill('reason', 'Incident response')
    await fill('current_password', 'wrong-password')
    failure = 401
    await submit()
    await until(() => expect(document.body.textContent).toContain('Reauthentication failed.'))
    expect(document.querySelector<HTMLInputElement>('[name="current_password"]')?.value).toBe('')
    await click(button('Cancel'))
    await until(() => expect(document.querySelector('[name="current_password"]')).toBeNull())
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('shows translated read-only inventory', async () => {
    permissions = ['members.read']
    await mount()
    expect(document.body.textContent).toContain('cannot perform offboarding')
    expect(
      [...document.querySelectorAll('button')].some((b) => b.textContent === 'Prepare handover'),
    ).toBe(false)
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.body.textContent).toContain('保留 Project 资产')
    expect(document.body.textContent).toContain('不能执行离职交接')
  })
  it('blocks last-administrator actions and reserves emergency handling for platform admins', async () => {
    inventory.last_administrator = true
    await mount()
    expect(document.body.textContent).toContain(
      'The last active administrator cannot be offboarded.',
    )
    expect(
      [...document.querySelectorAll('button')].some((b) => b.textContent === 'Prepare handover'),
    ).toBe(false)
    inventory.last_administrator = false
    role = 'member'
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['auth', 'session'] })
      await cache.invalidateQueries({ queryKey: ['offboarding', 'usr_target'] })
    })
    await until(() => expect(button('Prepare handover')).toBeTruthy())
    expect(
      [...document.querySelectorAll('button')].some((b) => b.textContent === 'Emergency disable'),
    ).toBe(false)
  })
  it('requires explicit empty-Team continuity but accepts existing emergency successors', () => {
    const team = inventory.teams[0]
    expect(needsEmergencySuccessor(team, inventory.user_id)).toBe(true)
    expect(buildAssignments(inventory, {}, {}, {}, true)).toBeNull()
    team.people.push({
      user_id: 'usr_existing',
      name: 'Existing',
      disabled: false,
      role: 'member',
      status: 'active',
    })
    expect(needsEmergencySuccessor(team, inventory.user_id)).toBe(false)
    expect(buildAssignments(inventory, {}, {}, {}, true)).toEqual({
      project_assignments: [],
      team_assignments: [],
    })
  })
})
