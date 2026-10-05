import { act, StrictMode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type AxiosAdapter, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { getSession } from '@/api/auth'
import i18n from '@/i18n'
import en from '@/i18n/locales/en/offboarding'
import zh from '@/i18n/locales/zh/offboarding'
import type { OffboardingCase, OffboardingInventory } from '@/types/offboarding'
import OffboardingPage from './index'
import AuthGate from '@/components/app/AuthGate'
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
    else if (config.url === '/setup') response.data = { initialized: true }
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
        request_id: body.request_id ?? inventory.cases[0]?.request_id ?? 'same-request',
        user_id: 'usr_target',
        actor_id: 'usr_admin',
        mode: config.url?.endsWith('/emergency') ? 'emergency' : 'planned',
        status: completed ? 'completed' : 'ready_to_complete',
        reason: body.reason ?? inventory.cases[0]?.reason ?? 'Planned departure',
        planned_at: body.planned_at ?? inventory.cases[0]?.planned_at ?? null,
        created_at: '2026-09-23T00:00:00Z',
        completed_at: completed ? '2026-09-23T01:00:00Z' : null,
        completed_by: completed ? 'usr_admin' : '',
        inventory_version: inventory.inventory_version,
        assignments: {
          project_assignments:
            body.project_assignments ?? inventory.cases[0]?.assignments.project_assignments ?? [],
          team_assignments:
            body.team_assignments ?? inventory.cases[0]?.assignments.team_assignments ?? [],
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
  vi.restoreAllMocks()
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
async function mount(strict = false, guarded = false) {
  const page = {
    path: '/admin/members/:memberId/offboarding',
    element: strict ? (
      <StrictMode>
        <OffboardingPage />
      </StrictMode>
    ) : (
      <OffboardingPage />
    ),
  }
  router = createMemoryRouter(
    guarded
      ? [
          { element: <AuthGate mode="private" />, children: [page] },
          { path: '/login', element: <p>Controlled sign-in</p> },
        ]
      : [page],
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
    cache.setQueryData(['admin', 'member', 'usr_admin', 'usr_target', 77], { id: 'usr_target' })
    cache.setQueryData(['admin', 'member', 'usr_other_admin', 'usr_target', 77], {
      id: 'usr_target',
    })
    await plan()
    await submit()
    await until(() => expect(document.body.textContent).toContain('Plan saved.'))
    expect(
      cache.getQueryState(['admin', 'member', 'usr_admin', 'usr_target', 77])?.isInvalidated,
    ).toBe(true)
    expect(
      cache.getQueryState(['admin', 'member', 'usr_other_admin', 'usr_target', 77])?.isInvalidated,
    ).toBe(false)
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
    await until(() => expect(document.body.textContent).toContain('response does not prove'))
    expect(button('Retry the same action').disabled).toBe(false)
    const originalBody = writes()[0].data
    inventory.inventory_version = 'digest_updated'
    failure = 0
    await click(button('Refresh and review again'))
    await until(() => expect(button('Retry the same action').disabled).toBe(false))
    expect(writes()).toHaveLength(1)
    expect(writes()[0].data).toBe(originalBody)
    await click(button('Abandon submitted request'))
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
  it('hides cached inventory and its portal immediately during a real Session renewal', async () => {
    inventory.teams = []
    await mount()
    await click(button('Emergency disable'))
    await fill('reason', 'Retained incident draft')
    const original = client.defaults.adapter as AxiosAdapter
    let release!: () => void
    const pending = new Promise<void>((resolve) => {
      release = resolve
    })
    client.defaults.adapter = async (config) => {
      if (config.url === '/auth/session') await pending
      return original(config)
    }
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['auth', 'session'] })
    })
    expect(document.body.textContent).not.toContain('Application')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    await act(async () => release())
    await until(() => expect(document.querySelector('[name="reason"]')).not.toBeNull())
    expect(document.querySelector<HTMLTextAreaElement>('[name="reason"]')?.value).toBe(
      'Retained incident draft',
    )
    expect(writes()).toHaveLength(0)
  })
  it('retains the dispatched emergency UUID and body after Cancel and explicit reopening', async () => {
    inventory.teams = []
    await mount()
    await click(button('Emergency disable'))
    await fill('reason', 'Original incident')
    await fill('current_password', 'first-password-proof')
    failure = 503
    await submit()
    await until(() =>
      expect(document.body.textContent).toContain('outcome may already be committed'),
    )
    const first = JSON.parse(writes()[0].data)
    await click(button('Cancel'))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    await click(button('Review submitted action'))
    await fill('current_password', 'fresh-password-proof')
    failure = 0
    await submit()
    await until(() => expect(writes()).toHaveLength(2))
    const second = JSON.parse(writes()[1].data)
    expect(second.request_id).toBe(first.request_id)
    expect(second.reason).toBe(first.reason)
    expect(second.team_assignments).toEqual(first.team_assignments)
    expect(second.current_password).toBe('fresh-password-proof')
  })

  it.each([400, 409, 412, 403, 500, 503])(
    'retains every dispatched plan after HTTP %s and uses fresh CSRF on an explicit retry',
    async (status) => {
      await mount()
      await plan()
      failure = status
      await submit()
      await until(() => expect(button('Retry the same action').disabled).toBe(false))
      const originalBody = writes()[0].data
      expect(document.querySelector<HTMLTextAreaElement>('[name="reason"]')?.readOnly).toBe(true)
      const adapter = client.defaults.adapter as AxiosAdapter
      client.defaults.adapter = async (config) => {
        const response = await adapter(config)
        if (config.url === '/auth/session') response.data.csrf_token = 'renewed-csrf'
        return response
      }
      await act(async () => {
        await cache.invalidateQueries({ queryKey: ['auth', 'session'] })
      })
      await until(() => expect(button('Retry the same action').disabled).toBe(false))
      expect(writes()).toHaveLength(1)
      failure = 0
      await submit()
      await until(() => expect(document.body.textContent).toContain('Plan saved.'))
      expect(writes()[1].data).toBe(originalBody)
      expect(writes()[1].headers.get('X-CSRF-Token')).toBe('renewed-csrf')
    },
  )
  it('discards a delayed success after a same-millisecond real Session renewal even when refreshed inventory contains the matching receipt', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(1_800_000_000_000)
    await mount()
    const beforeSession = cache.getQueryState(['auth', 'session'])!
    await plan()
    const adapter = client.defaults.adapter as AxiosAdapter
    let release!: () => void
    const pending = new Promise<void>((resolve) => {
      release = resolve
    })
    client.defaults.adapter = async (config) => {
      const response = await adapter(config)
      if (config.method === 'post') await pending
      return response
    }
    await submit()
    await until(() => expect(writes()).toHaveLength(1))
    const originalBody = writes()[0].data
    expect(button('Applying changes…').disabled).toBe(true)
    expect(button('Cancel').disabled).toBe(true)
    await act(async () =>
      document
        .querySelector('[name="reason"]')!
        .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    await submit()
    expect(writes()).toHaveLength(1)
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['auth', 'session'] })
    })
    await until(() => expect(document.body.textContent).toContain('Handover history'))
    expect(writes()[0].signal?.aborted).toBe(true)
    const afterSession = cache.getQueryState(['auth', 'session'])!
    expect(afterSession.dataUpdatedAt).toBe(beforeSession.dataUpdatedAt)
    expect(afterSession.dataUpdateCount).toBe(beforeSession.dataUpdateCount + 1)
    await act(async () => release())
    await until(() => expect(button('Retry the same action').disabled).toBe(false))
    expect(document.body.textContent).not.toContain('Plan saved. Personal access is unchanged')
    expect(writes()).toHaveLength(1)
    client.defaults.adapter = adapter
    await submit()
    await until(() => expect(document.body.textContent).toContain('Plan saved.'))
    expect(writes()[1].data).toBe(originalBody)
  })
  it('hides write portals during independent write revocation and restores only the original submitted intent', async () => {
    await mount()
    await plan()
    failure = 503
    await submit()
    await until(() => expect(button('Retry the same action')).toBeTruthy())
    const body = writes()[0].data
    const oldForm = document.querySelector('form')!
    permissions = ['members.read']
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['permissions'] })
    })
    await until(() => expect(document.body.textContent).toContain('cannot perform offboarding'))
    expect(document.body.textContent).toContain('Application')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    await act(async () =>
      oldForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    expect(writes()).toHaveLength(1)
    permissions = ['members.read', 'members.write']
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['permissions'] })
    })
    await until(() => expect(button('Retry the same action').disabled).toBe(false))
    failure = 0
    await submit()
    await until(() => expect(document.body.textContent).toContain('Plan saved.'))
    expect(writes()[1].data).toBe(body)
  })
  it('hides private inventory on read denial and preserves same-owner draft through a restored real read', async () => {
    inventory.teams = []
    await mount()
    await click(button('Emergency disable'))
    await fill('reason', 'Draft before denial')
    permissions = ['members.write']
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['permissions'] })
    })
    await until(() => expect(document.body.textContent).toContain('no longer have permission'))
    expect(document.body.textContent).not.toContain('Application')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    permissions = ['members.read', 'members.write']
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['permissions'] })
    })
    await until(() => expect(document.querySelector('[name="reason"]')).not.toBeNull())
    expect(document.querySelector<HTMLTextAreaElement>('[name="reason"]')?.value).toBe(
      'Draft before denial',
    )
    expect(writes()).toHaveLength(0)
  })
  it('hides cached facts on inventory errors without losing a same-owner draft', async () => {
    inventory.teams = []
    await mount()
    await click(button('Emergency disable'))
    await fill('reason', 'Draft before outage')
    const adapter = client.defaults.adapter as AxiosAdapter
    client.defaults.adapter = async (config) => {
      if (config.url === path)
        throw new AxiosError('Controlled read outage', '', config, undefined, {
          config,
          status: 503,
          statusText: '',
          headers: new AxiosHeaders(),
          data: {},
        })
      return adapter(config)
    }
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['offboarding', 'usr_admin', 'usr_target'] })
    })
    await until(() =>
      expect(
        cache
          .getQueryCache()
          .getAll()
          .some(
            (q) =>
              q.queryKey[1] === 'usr_admin' &&
              q.queryKey[4] === 'inventory' &&
              q.state.status === 'error',
          ),
      ).toBe(true),
    )
    expect(document.body.textContent).not.toContain('Application')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    client.defaults.adapter = adapter
    await click(button('Retry'))
    await until(() => expect(document.querySelector('[name="reason"]')).not.toBeNull())
    expect(document.querySelector<HTMLTextAreaElement>('[name="reason"]')?.value).toBe(
      'Draft before outage',
    )
    expect(writes()).toHaveLength(0)
  })
  it('destroys private intent on an actor change and ignores the old detached form', async () => {
    inventory.teams = []
    await mount()
    await click(button('Emergency disable'))
    await fill('reason', 'Old actor intent')
    await fill('current_password', 'old-proof')
    failure = 503
    await submit()
    await until(() => expect(button('Retry the same action')).toBeTruthy())
    const oldForm = document.querySelector('form')!
    const adapter = client.defaults.adapter as AxiosAdapter
    client.defaults.adapter = async (config) => {
      const response = await adapter(config)
      if (config.url === '/auth/session') response.data.user.id = 'usr_other_admin'
      return response
    }
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['auth', 'session'] })
    })
    await until(() => expect(button('Emergency disable')).toBeTruthy())
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.body.textContent).not.toContain('Review submitted action')
    await act(async () =>
      oldForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    expect(writes()).toHaveLength(1)
    await click(button('Emergency disable'))
    expect(document.querySelector<HTMLTextAreaElement>('[name="reason"]')?.value).toBe('')
  })
  it('ignores late target responses after navigation and clears the local retry on unmount', async () => {
    await mount()
    await plan()
    const adapter = client.defaults.adapter as AxiosAdapter
    let release!: () => void
    const pending = new Promise<void>((resolve) => {
      release = resolve
    })
    client.defaults.adapter = async (config) => {
      const response = await adapter(config)
      if (config.method === 'post') await pending
      return response
    }
    await submit()
    await until(() => expect(writes()).toHaveLength(1))
    await act(async () => {
      await router.navigate('/admin/members/usr_other/offboarding')
    })
    expect(document.body.textContent).not.toContain('Application')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    await act(async () => release())
    expect(writes()[0].signal?.aborted).toBe(true)
    expect(document.body.textContent).not.toContain('Plan saved.')
    expect(document.body.textContent).not.toContain('Review submitted action')
  })
  it('keeps a rejected retry and live translated reason until explicit abandonment', async () => {
    await mount()
    await plan()
    failure = 503
    await submit()
    await until(() => expect(button('Retry the same action')).toBeTruthy())
    const body = writes()[0].data
    failure = 412
    await submit()
    await until(() => expect(document.body.textContent).toContain('response does not prove'))
    expect(writes()[1].data).toBe(body)
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.body.textContent).toContain('原操作已回滚')
    expect(document.querySelector<HTMLTextAreaElement>('[name="reason"]')?.value).toBe(
      'Planned departure',
    )
    expect(document.querySelector<HTMLTextAreaElement>('[name="reason"]')?.readOnly).toBe(true)
    await click(button('放弃已提交请求'))
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(document.body.textContent).toContain('原操作的结果仍未知')
    expect(writes()).toHaveLength(2)
  })
  it('returns Escape focus to the exact current trigger without saving an unsent draft', async () => {
    inventory.teams = []
    await mount()
    const trigger = button('Emergency disable')
    await click(trigger)
    await fill('reason', 'Unsent draft')
    const input = document.querySelector<HTMLElement>('[name="reason"]')!
    await act(async () => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await until(() => expect(document.activeElement).toBe(trigger))
    expect(writes()).toHaveLength(0)
  })
  it.each(['user_id', 'actor_id', 'request_id', 'mode', 'assignments', 'created_at'])(
    'rejects malformed or mismatched success %s instead of resolving the captured action',
    async (field) => {
      await mount()
      await plan()
      const adapter = client.defaults.adapter as AxiosAdapter
      client.defaults.adapter = async (config) => {
        const response = await adapter(config)
        if (config.method === 'post') {
          if (field === 'assignments')
            response.data.assignments = { project_assignments: 'invalid', team_assignments: [] }
          else if (field === 'created_at') response.data.created_at = null
          else response.data[field] = 'mismatched'
        }
        return response
      }
      await submit()
      await until(() => expect(document.body.textContent).toContain('reviewed action is retained'))
      expect(button('Retry the same action').disabled).toBe(false)
      expect(document.body.textContent).not.toContain('Plan saved.')
    },
  )
  it('confirms the exact historical case created and completed by different actors after reactivation', async () => {
    await mount()
    await plan()
    await submit()
    await until(() => expect(button('Complete handover')).toBeTruthy())
    const originalCase = inventory.cases[0]
    originalCase.actor_id = 'usr_original_creator'
    await click(button('Refresh inventory'))
    await until(() => expect(button('Complete handover')).toBeTruthy())
    const adapter = client.defaults.adapter as AxiosAdapter
    client.defaults.adapter = async (config) => {
      if (config.url?.endsWith('/complete')) {
        requests.push(config)
        return {
          config,
          status: 200,
          statusText: '',
          headers: new AxiosHeaders(),
          data: {
            ...structuredClone(originalCase),
            status: 'completed',
            completed_by: 'usr_prior_completer',
            completed_at: '2026-09-24T00:00:00Z',
          },
        }
      }
      return adapter(config)
    }
    await click(button('Complete handover'))
    await click(button('Confirm completion'))
    await until(() =>
      expect(document.body.textContent).toContain('Project keys and history were preserved.'),
    )
    expect(writes()[1].url).toBe(`${path}/${originalCase.id}/complete`)
    expect(JSON.parse(writes()[1].data)).toEqual({})
    expect(inventory.disabled).toBe(false)
  })

  it('restores current authority under React strict effect remounts without adding a Dialog Session observer', async () => {
    inventory.teams = []
    await mount(true)
    const sessionReads = requests.filter((request) => request.url === '/auth/session').length
    await click(button('Emergency disable'))
    await fill('reason', 'Strict mode draft')
    expect(document.querySelector<HTMLTextAreaElement>('[name="reason"]')?.value).toBe(
      'Strict mode draft',
    )
    expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(sessionReads)
    expect(writes()).toHaveLength(0)
  })
  it('requires an explicit new review for a changed unsent inventory without dispatching an old form', async () => {
    await mount()
    await plan()
    inventory.inventory_version = 'digest_new_unsent'
    await click(button('Refresh inventory'))
    await until(() => expect(button('Save reviewed plan').disabled).toBe(true))
    await submit()
    expect(writes()).toHaveLength(0)
    await click(button('Refresh and review again'))
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await plan()
    await submit()
    await until(() => expect(document.body.textContent).toContain('Plan saved.'))
    expect(JSON.parse(writes()[0].data).inventory_version).toBe('digest_new_unsent')
  })

  it('denies old successor events in the same batch as a new literal search', async () => {
    await mount()
    await click(button('Prepare handover'))
    await until(() =>
      expect(document.querySelectorAll('fieldset button').length).toBeGreaterThan(0),
    )
    const oldChoice = button('Successor', document.querySelector('fieldset')!)
    const input = document.querySelector<HTMLInputElement>('form input:not([name])')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        input,
        'new literal query',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
      oldChoice.click()
    })
    await until(() =>
      expect(
        button('Successor', document.querySelector('fieldset')!).getAttribute('aria-pressed'),
      ).toBe('false'),
    )
    expect(requests.filter((request) => request.url === '/admin/members').at(-1)?.params.q).toBe(
      'new literal query',
    )
    expect(writes()).toHaveLength(0)
  })
  it('denies a detached submit in the same event batch as Cancel', async () => {
    await mount()
    await plan()
    const form = document.querySelector('form')!
    await act(async () => {
      button('Cancel').click()
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    expect(writes()).toHaveLength(0)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })

  it('keeps selections outside a failed candidate page and permits only current-workspace removal', async () => {
    await mount()
    await plan()
    const adapter = client.defaults.adapter as AxiosAdapter
    client.defaults.adapter = async (config) => {
      if (config.url === '/admin/members' && config.params?.q)
        throw new AxiosError('Controlled search outage', '', config, undefined, {
          config,
          status: 503,
          statusText: '',
          headers: new AxiosHeaders(),
          data: {},
        })
      return adapter(config)
    }
    const input = document.querySelector<HTMLInputElement>('form input:not([name])')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        input,
        'unavailable candidates',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await until(() =>
      expect(
        cache
          .getQueryCache()
          .getAll()
          .some((query) => query.queryKey[4] === 'successors' && query.state.status === 'error'),
      ).toBe(true),
    )
    expect(document.querySelectorAll('[aria-label="Selected successors"]')).toHaveLength(2)
    const field = document.querySelector('fieldset')!
    const remove = field.querySelector<HTMLButtonElement>('[aria-label="Remove Successor"]')!
    await click(remove)
    expect(field.querySelector('[aria-label="Remove Successor"]')).toBeNull()
    expect(document.querySelectorAll('[aria-label="Selected successors"]')).toHaveLength(1)
    expect(writes()).toHaveLength(0)
  })

  it('lets real Session401 AuthGate cleanup destroy the local retry rather than inferring logout from password rejection', async () => {
    await mount(false, true)
    await plan()
    failure = 503
    await submit()
    await until(() => expect(button('Retry the same action')).toBeTruthy())
    const adapter = client.defaults.adapter as AxiosAdapter
    client.defaults.adapter = async (config) => {
      if (config.url === '/auth/session')
        throw new AxiosError('Controlled expired Session', '', config, undefined, {
          config,
          status: 401,
          statusText: '',
          headers: new AxiosHeaders(),
          data: {},
        })
      return adapter(config)
    }
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['auth', 'session'] })
    })
    await until(() => expect(router.state.location.pathname).toBe('/login'))
    expect(document.body.textContent).not.toContain('Application')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(
      cache
        .getQueryCache()
        .getAll()
        .some((query) => query.queryKey[0] === 'offboarding'),
    ).toBe(false)
    client.defaults.adapter = adapter
    await act(async () => {
      await cache.fetchQuery({ queryKey: ['auth', 'session'], queryFn: getSession, staleTime: 0 })
    })
    await act(async () => {
      await router.navigate(path)
    })
    await until(() => expect(button('Prepare handover')).toBeTruthy())
    expect(document.body.textContent).not.toContain('Review submitted action')
    expect(writes()).toHaveLength(1)
  })
  it('keeps a new target busy when an aborted old target finally settles', async () => {
    await mount()
    await plan()
    const adapter = client.defaults.adapter as AxiosAdapter
    let releaseOld!: () => void, releaseNew!: () => void
    const oldPending = new Promise<void>((resolve) => {
      releaseOld = resolve
    })
    const newPending = new Promise<void>((resolve) => {
      releaseNew = resolve
    })
    const other = '/admin/members/usr_other/offboarding'
    client.defaults.adapter = async (config) => {
      if (config.url === '/admin/members/usr_other')
        return {
          config,
          status: 200,
          statusText: '',
          headers: new AxiosHeaders(),
          data: {
            id: 'usr_other',
            name: 'Other target',
            role: 'member',
            disabled: false,
            email: 'other@example.invalid',
            role_ids: [],
            created_at: '2026-09-23T00:00:00Z',
          },
        }
      if (config.url === other)
        return {
          config,
          status: 200,
          statusText: '',
          headers: new AxiosHeaders(),
          data: {
            ...structuredClone(inventory),
            user_id: 'usr_other',
            projects: [],
            teams: [],
            cases: [],
            personal_keys: [],
          },
        }
      if (config.url === `${other}/plans`) {
        requests.push(config)
        const body = JSON.parse(config.data)
        await newPending
        return {
          config,
          status: 201,
          statusText: '',
          headers: new AxiosHeaders(),
          data: {
            id: 'ofb_other',
            request_id: body.request_id,
            user_id: 'usr_other',
            actor_id: 'usr_admin',
            mode: 'planned',
            status: 'ready_to_complete',
            reason: body.reason,
            planned_at: body.planned_at,
            created_at: '2026-09-23T00:00:00Z',
            completed_at: null,
            completed_by: '',
            inventory_version: body.inventory_version,
            assignments: { project_assignments: [], team_assignments: [] },
          },
        }
      }
      const response = await adapter(config)
      if (config.method === 'post') await oldPending
      return response
    }
    await submit()
    await until(() => expect(writes()).toHaveLength(1))
    await act(async () => {
      await router.navigate(other)
    })
    await until(() => expect(button('Prepare handover')).toBeTruthy())
    await click(button('Prepare handover'))
    await until(() => expect(button('Save reviewed plan').disabled).toBe(false))
    await fill('reason', 'Independent other target')
    await fill('planned_at', '2026-10-01T09:00')
    await submit()
    await until(() => expect(writes()).toHaveLength(2))
    await act(async () => releaseOld())
    expect(button('Applying changes…').disabled).toBe(true)
    expect(button('Cancel').disabled).toBe(true)
    expect(document.body.textContent).not.toContain('Plan saved.')
    await act(async () => releaseNew())
    await until(() => expect(document.body.textContent).toContain('Plan saved.'))
    expect(writes()[0].signal?.aborted).toBe(true)
    expect(writes()[1].signal?.aborted).toBe(false)
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
