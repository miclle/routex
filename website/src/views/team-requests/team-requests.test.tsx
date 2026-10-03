import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import AppShell from '@/components/app/AppShell'
import TeamRequestsPage from './index'
import productionRoutes from '@/router'
import { contextFixture, requestFixture } from './fixture'
import type { TeamQuotaContext, TeamRequestDetail, TeamRequestStatus } from '@/types/team-requests'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[],
  actor: string,
  csrf: string,
  permissions: string[],
  record: TeamRequestDetail,
  context: TeamQuotaContext
let failure: Record<string, number>,
  hold: Promise<void> | undefined,
  malformed: boolean,
  escalate: boolean,
  pendingApply: boolean,
  createReplay: boolean
const originalAdapter = client.defaults.adapter
beforeEach(async () => {
  await i18n.changeLanguage('en')
  root = createRoot((host = document.createElement('div')))
  document.body.append(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 60000 }, mutations: { retry: false } },
  })
  actor = 'usr_member'
  csrf = 'csrf-old'
  permissions = []
  requests = []
  record = requestFixture()
  context = contextFixture()
  failure = {}
  hold = undefined
  malformed = false
  escalate = false
  pendingApply = false
  createReplay = false
  cache.setQueryData(['auth', 'session'], session())
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      data: {} as unknown,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
    }
    if (config.method === 'post' && hold) await hold
    if (failure[`${config.method} ${config.url}`]) {
      response.status = failure[`${config.method} ${config.url}`]
      throw new AxiosError('Fixture failure', '', config, undefined, response)
    }
    const body = config.data ? JSON.parse(config.data) : {}
    if (config.url === '/setup') response.data = { initialized: true }
    else if (config.url === '/auth/registration') response.data = { enabled: false }
    else if (config.url === '/auth/session') response.data = session()
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/site')
      response.data = {
        name: 'RouteX',
        service_url: '',
        logo_url: '',
        footer: '',
        default_language: 'en',
        etag: 'site',
        updated_at: '2026-10-03T00:00:00Z',
      }
    else if (config.url === '/announcements') response.data = { items: [] }
    else if (config.url === '/notifications') response.data = { items: [], unread_count: 0 }
    else if (config.url === '/teams')
      response.data = {
        items: [{ id: 'tea_one', name: 'Research Team', status: 'active' }],
        next_cursor: null,
      }
    else if (config.url === '/teams/tea_one/quota-request-context')
      response.data = {
        ...context,
        dimension: config.params.dimension,
        currency: config.params.dimension === 'money' ? 'USD' : null,
      }
    else if (config.url === '/teams/tea_one/quota-requests') {
      record = {
        ...requestFixture(body.dimension),
        request_id: body.request_id,
        target_value: body.target_value,
        reason: body.reason,
        applicant_user_id: actor,
        submitted_snapshot: {
          ...context,
          dimension: body.dimension,
          currency: body.dimension === 'money' ? 'USD' : null,
        },
      }
      if (createReplay) record = terminal(record, 'withdrawn')
      response.data = malformed ? { ...record, status: undefined } : structuredClone(record)
    } else if (config.url === '/quota-requests/tqr_one/decision') {
      const savedStep = {
        ...record.steps.find((step) => step.id === body.step_id)!,
        status: ({ approve: 'approved', reject: 'rejected', withdraw: 'withdrawn' } as const)[
          body.action as 'approve' | 'reject' | 'withdraw'
        ],
        actor_id: actor,
        actor_name: 'Recorded actor',
        reason: body.reason,
        acted_at: '2026-10-03T01:00:00Z',
      }
      if (escalate)
        record = {
          ...record,
          status: 'pending_quota_admin',
          current_step_id: 'qst_admin',
          steps: [
            savedStep,
            { ...requestFixture().steps[0], id: 'qst_admin', stage: 'quota_admin' },
          ],
          escalation_reason: 'target_exceeds_team',
          approval_preview: null,
          etag: 'a'.repeat(64),
        }
      else
        record = {
          ...terminal(
            record,
            body.action === 'approve'
              ? 'approved'
              : body.action === 'reject'
                ? 'rejected'
                : 'withdrawn',
          ),
          steps: [savedStep],
          application:
            body.action === 'approve'
              ? {
                  runtime_applied: !pendingApply,
                  application_status: pendingApply ? 'pending' : 'applied',
                }
              : null,
        }
      response.data = malformed
        ? { decision_id: body.decision_id }
        : {
            decision_id: body.decision_id,
            step_id: body.step_id,
            action: body.action,
            committed: true,
            saved_step: savedStep,
            request: structuredClone(record),
          }
    } else if (
      config.url === '/quota-requests/tqr_one' ||
      config.url === '/admin/quota-requests/tqr_one'
    )
      response.data = {
        ...structuredClone(record),
        approval_preview: config.url.startsWith('/admin') ? null : record.approval_preview,
        allowed_actions: config.url.startsWith('/admin')
          ? []
          : record.status.startsWith('pending_')
            ? actor === record.applicant_user_id
              ? ['withdraw']
              : record.allowed_actions
            : [],
      }
    else if (config.url === '/quota-requests' || config.url === '/admin/quota-requests') {
      const mine = config.params?.view === 'my'
      const eligible =
        config.url.startsWith('/admin') ||
        (mine ? record.applicant_user_id === actor : record.applicant_user_id !== actor)
      const matches =
        (!config.params?.dimension || config.params.dimension === record.dimension) &&
        (!config.params?.status || config.params.status === record.status)
      response.data = {
        items: eligible && matches ? [structuredClone(record)] : [],
        total: eligible && matches ? 1 : 0,
        next_cursor: null,
      }
    } else throw new Error(`Unexpected fixture request ${config.url}`)
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
function session() {
  return {
    user: { id: actor, role: 'member', name: 'Current actor', email: 'member@example.test' },
    csrf_token: csrf,
  }
}
function terminal(input: TeamRequestDetail, status: TeamRequestStatus): TeamRequestDetail {
  return {
    ...input,
    status,
    current_step_id: null,
    resolved_at: '2026-10-03T01:00:00Z',
    approval_preview: null,
    workspace_available: false,
    allowed_actions: [],
    etag: 'b'.repeat(64),
  }
}
async function until(assert: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
async function mount(path = '/quota-requests', shell = false, production = false) {
  router = createMemoryRouter(
    production
      ? productionRoutes
      : shell
        ? [
            {
              path: '/',
              element: <AppShell />,
              children: [
                { path: 'quota-requests', element: <TeamRequestsPage /> },
                { path: 'admin/quota-requests', element: <TeamRequestsPage admin /> },
              ],
            },
          ]
        : [
            { path: '/quota-requests', element: <TeamRequestsPage /> },
            { path: '/admin/quota-requests', element: <TeamRequestsPage admin /> },
          ],
    { initialEntries: [path] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
}
function button(label: string) {
  const dialogs = [...document.querySelectorAll<HTMLElement>('[role="dialog"]')]
  const scope = dialogs.at(-1) ?? document
  const result = [...scope.querySelectorAll<HTMLButtonElement>('button,[role="tab"]')].find(
    (item) => item.textContent === label,
  )
  expect(result, label).toBeDefined()
  return result!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function input(label: string, value: string) {
  const dialogs = [...document.querySelectorAll<HTMLElement>('[role="dialog"]')]
  const scope = dialogs.at(-1) ?? document
  const el = scope.querySelector<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>(
    `[aria-label="${label}"]`,
  )!
  expect(el, label).toBeTruthy()
  await act(async () => {
    if (el instanceof HTMLSelectElement) {
      el.value = value
      el.dispatchEvent(new Event('change', { bubbles: true }))
    } else {
      Object.getOwnPropertyDescriptor(
        el instanceof HTMLTextAreaElement
          ? HTMLTextAreaElement.prototype
          : HTMLInputElement.prototype,
        'value',
      )!.set!.call(el, value)
      el.dispatchEvent(new Event('input', { bubbles: true }))
    }
  })
}
const posts = () => requests.filter((request) => request.method === 'post')
async function form() {
  await until(() => expect(host.textContent).toContain('My requests'))
  await click('Request an increase')
  await until(() => expect(document.querySelector('option[value="tea_one"]')).toBeTruthy())
  await input('Team', 'tea_one')
  await until(() => expect(document.body.textContent).toContain('Effective member limit'))
  await click('Use this reviewed context')
  await input('Target allowance', '20')
  await input('Request reason', 'New controlled quota')
}
async function review() {
  await mount('/quota-requests?tab=pending&request=tqr_one')
  await until(() => expect(document.body.textContent).toContain('Approval progress'))
  await click('Approve')
}
describe('Team monthly requests workspace', () => {
  it.each(['/admin/approvals', '/admin/quota-approvals'])(
    'redirects legacy %s to the permission-gated records workspace',
    async (path) => {
      permissions = ['teams.quota_requests.read_all']
      await mount(path, false, true)
      await until(() => expect(host.textContent).toContain('Quota request records'))
      expect(router.state.location.pathname).toBe('/admin/quota-requests')
      expect(requests.some((request) => request.url === '/admin/quota-requests')).toBe(true)
      expect(posts()).toHaveLength(0)
    },
  )
  it.each(['/admin/approvals', '/admin/quota-approvals'])(
    'legacy %s does not bypass destination authorization',
    async (path) => {
      await mount(path, false, true)
      await until(() => expect(host.textContent).toContain('You do not have access'))
      expect(router.state.location.pathname).toBe('/admin/quota-requests')
      expect(requests.some((request) => request.url === '/admin/quota-requests')).toBe(false)
    },
  )
  it.each(['/admin/approvals', '/admin/quota-approvals'])(
    'legacy %s retains the private login boundary',
    async (path) => {
      cache.setQueryData(['auth', 'session'], null)
      failure['get /auth/session'] = 401
      await mount(path, false, true)
      await until(() => expect(router.state.location.pathname).toBe('/login'))
      expect(requests.some((request) => request.url?.startsWith('/admin/quota-requests'))).toBe(
        false,
      )
    },
  )
  it('shows the approved personal tabs and scoped table in default English', async () => {
    await mount()
    await until(() => expect(host.querySelector('table')?.textContent).toContain('Research Team'))
    expect([...host.querySelectorAll('[role="tab"]')].map((item) => item.textContent)).toEqual([
      'My requests',
      'Pending for me',
    ])
    expect(requests.find((request) => request.url === '/quota-requests')?.params.view).toBe('my')
    expect(requests.some((request) => request.url?.startsWith('/admin/teams'))).toBe(false)
  })
  it('creates only one reviewed dimension with stable UUID and cookie-session CSRF', async () => {
    await mount()
    await form()
    await click('Submit request')
    await until(() => expect(host.textContent).toContain('Request saved.'))
    expect(posts()).toHaveLength(1)
    const body = JSON.parse(posts()[0].data)
    expect(body).toMatchObject({
      dimension: 'tokens',
      target_value: '20',
      reason: 'New controlled quota',
    })
    expect(body.request_id).toMatch(/^[a-f0-9-]{36}$/)
    expect(body.currency).toBeUndefined()
    expect(posts()[0].headers.get('If-Match')).toBe(`"${'e'.repeat(64)}"`)
    expect(posts()[0].headers.get('X-CSRF-Token')).toBe('csrf-old')
  })
  it('keeps exact 18+18 decimal targets and displays immutable current currency', async () => {
    await mount()
    await form()
    context = contextFixture('money')
    context.member_effective = '0.1'
    await input('Quota type', 'money')
    await until(() => expect(document.body.textContent).toContain('10 USD'))
    await click('Use this reviewed context')
    await input('Target allowance', '123456789012345678.123456789012345678')
    await click('Submit request')
    await until(() => expect(posts()).toHaveLength(1))
    expect(JSON.parse(posts()[0].data).target_value).toBe('123456789012345678.123456789012345678')
  })
  it('never treats an inherited/unrestricted effective cap as zero', async () => {
    context.member_stored = null
    context.member_effective = null
    context.eligible = false
    context.blockers = ['member_unlimited']
    await mount()
    await click('Request an increase')
    await until(() => expect(document.querySelector('option[value="tea_one"]')).toBeTruthy())
    await input('Team', 'tea_one')
    await until(() => expect(document.body.textContent).toContain('Inherit Team'))
    expect(document.body.textContent).toContain('Unrestricted')
    expect(button('Submit request').disabled).toBe(true)
    expect(posts()).toHaveLength(0)
  })
  it('retains exact uncertain creation through rejected retry and fresh context', async () => {
    failure['post /teams/tea_one/quota-requests'] = 503
    await mount()
    await form()
    await click('Submit request')
    await until(() =>
      expect(document.body.textContent).toContain('The operation may already be saved'),
    )
    csrf = 'csrf-new'
    await act(async () => {
      cache.setQueryData(['auth', 'session'], session())
    })
    failure['post /teams/tea_one/quota-requests'] = 409
    await click('Retry exact operation')
    await until(() => expect(posts()).toHaveLength(2))
    context.etag = 'a'.repeat(64)
    await click('Refresh')
    await until(() => expect(button('Retry exact operation').disabled).toBe(false))
    expect(button('Submit request').disabled).toBe(true)
    delete failure['post /teams/tea_one/quota-requests']
    await click('Retry exact operation')
    await until(() => expect(host.textContent).toContain('Request saved.'))
    expect(posts()[2].data).toBe(posts()[0].data)
    expect(posts()[2].headers.get('If-Match')).toBe(posts()[0].headers.get('If-Match'))
    expect(posts()[2].headers.get('X-CSRF-Token')).toBe('csrf-new')
  })
  it('malformed successful creation stays unknown and terminal replay can reconcile', async () => {
    malformed = true
    await mount()
    await form()
    await click('Submit request')
    await until(() =>
      expect(document.body.textContent).toContain('The operation may already be saved'),
    )
    expect(host.textContent).not.toContain('Request saved.')
    malformed = false
    createReplay = true
    await click('Retry exact operation')
    await until(() => expect(host.textContent).toContain('Request saved.'))
    expect(posts()[1].data).toBe(posts()[0].data)
  })
  it('allows dismissal of uncertainty without reporting completion', async () => {
    failure['post /teams/tea_one/quota-requests'] = 503
    await mount()
    await form()
    await click('Submit request')
    await until(() =>
      expect(document.body.textContent).toContain('The operation may already be saved'),
    )
    await click('Cancel')
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(host.textContent).toContain('The previous operation has an unknown result')
    expect(host.textContent).not.toContain('Request saved.')
    expect(posts()).toHaveLength(1)
  })
  it('preserves the target and reason while live language changes', async () => {
    await mount()
    await form()
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.querySelector<HTMLInputElement>('[aria-label="目标额度"]')!.value).toBe('20')
    expect(document.querySelector<HTMLTextAreaElement>('[aria-label="申请理由"]')!.value).toBe(
      'New controlled quota',
    )
    expect(document.body.textContent).toContain('当前上下文')
    await act(async () => {
      await i18n.changeLanguage('en')
    })
  })
  it('owner approval saves only the step when escalation remains pending', async () => {
    actor = 'usr_owner'
    cache.setQueryData(['auth', 'session'], session())
    escalate = true
    record.approval_preview = {
      member_before: '10',
      member_after: '10',
      team_before: '100',
      team_after: '100',
      escalates: true,
    }
    await review()
    expect(document.body.textContent).toContain('It does not change the member or Team allowance')
    await click('Confirm decision')
    await until(() => expect(document.body.textContent).toContain('The reviewed decision is saved'))
    expect(document.body.textContent).toContain('Awaiting quota administrator')
    expect(document.body.textContent).not.toContain('The approved policy is currently applied')
    expect(JSON.parse(posts()[0].data)).toMatchObject({
      step_id: 'qst_owner',
      action: 'approve',
      reason: '',
    })
  })
  it('shows the reviewed within-Team member change and unchanged shared allowance', async () => {
    actor = 'usr_owner'
    cache.setQueryData(['auth', 'session'], session())
    await review()
    const rows = [
      ...document.querySelectorAll('table[aria-label="Reviewed approval effect"] tbody tr'),
    ].map((row) => row.textContent)
    expect(rows).toEqual(['Member allowance1020', 'Team allowance100100'])
    expect(document.body.textContent).not.toContain(
      'It does not change the member or Team allowance',
    )
  })
  it('shows the platform-stage atomic member and Team change from the server', async () => {
    actor = 'usr_quota_admin'
    cache.setQueryData(['auth', 'session'], session())
    record.status = 'pending_quota_admin'
    record.target_value = '150'
    record.steps[0].stage = 'quota_admin'
    record.approval_preview = {
      member_before: '10',
      member_after: '150',
      team_before: '100',
      team_after: '150',
      escalates: false,
    }
    await review()
    const rows = [
      ...document.querySelectorAll('table[aria-label="Reviewed approval effect"] tbody tr'),
    ].map((row) => row.textContent)
    expect(rows).toEqual(['Member allowance10150', 'Team allowance100150'])
    await click('Confirm decision')
    await until(() => expect(posts()).toHaveLength(1))
  })
  it('does not offer approval without a valid current server preview', async () => {
    actor = 'usr_owner'
    cache.setQueryData(['auth', 'session'], session())
    record.approval_preview = null
    await mount('/quota-requests?tab=pending&request=tqr_one')
    await until(() => expect(document.body.textContent).toContain('Approval progress'))
    expect(
      [...document.querySelectorAll('button')].some((item) => item.textContent === 'Approve'),
    ).toBe(false)
    expect(posts()).toHaveLength(0)
  })
  it.each(['approved', 'pending_team_owner'] as const)(
    'read-only global %s has no approval-navigation link without server authority',
    async (status) => {
      permissions = ['teams.quota_requests.read_all']
      if (status === 'approved') {
        record = {
          ...terminal(record, 'approved'),
          application: { runtime_applied: true, application_status: 'applied' },
        }
      }
      record.workspace_available = false
      await mount('/admin/quota-requests?request=tqr_one')
      await until(() => expect(document.body.textContent).toContain('Approval progress'))
      expect(
        [...document.querySelectorAll('a')].some((item) => item.textContent === 'Go to workspace'),
      ).toBe(false)
      expect(posts()).toHaveLength(0)
    },
  )
  it('requires a rejection reason and uses the exact current step', async () => {
    actor = 'usr_owner'
    cache.setQueryData(['auth', 'session'], session())
    await mount('/quota-requests?tab=pending&request=tqr_one')
    await until(() => expect(document.body.textContent).toContain('Approval progress'))
    await click('Reject')
    await click('Confirm decision')
    expect(document.body.textContent).toContain('Provide a rejection reason')
    expect(posts()).toHaveLength(0)
    await input('Decision comment', 'Requires planning')
    await click('Confirm decision')
    await until(() => expect(document.body.textContent).toContain('The reviewed decision is saved'))
    expect(JSON.parse(posts()[0].data)).toMatchObject({
      action: 'reject',
      reason: 'Requires planning',
      step_id: 'qst_owner',
    })
  })
  it('retains the exact decision UUID/body/header with rotated CSRF', async () => {
    actor = 'usr_owner'
    cache.setQueryData(['auth', 'session'], session())
    failure['post /quota-requests/tqr_one/decision'] = 503
    await review()
    await click('Confirm decision')
    await until(() =>
      expect(document.body.textContent).toContain('The operation may already be saved'),
    )
    csrf = 'csrf-new'
    await act(async () => {
      cache.setQueryData(['auth', 'session'], session())
    })
    delete failure['post /quota-requests/tqr_one/decision']
    await click('Retry exact operation')
    await until(() =>
      expect(document.body.textContent).toContain('The approved policy is currently applied'),
    )
    expect(posts()[1].data).toBe(posts()[0].data)
    expect(posts()[1].headers.get('If-Match')).toBe(posts()[0].headers.get('If-Match'))
    expect(posts()[1].headers.get('X-CSRF-Token')).toBe('csrf-new')
  })
  it('saved pending application retains an explicit exact-operation retry', async () => {
    actor = 'usr_owner'
    cache.setQueryData(['auth', 'session'], session())
    pendingApply = true
    await review()
    await click('Confirm decision')
    await until(() =>
      expect(document.body.textContent).toContain(
        'Approval is saved; runtime application is not confirmed',
      ),
    )
    expect(document.body.textContent).not.toContain('The approved policy is currently applied')
    pendingApply = false
    await click('Retry exact operation')
    await until(() =>
      expect(document.body.textContent).toContain('The approved policy is currently applied'),
    )
    expect(posts()[1].data).toBe(posts()[0].data)
  })
  it('hides historic private detail after a fresh authorization failure', async () => {
    actor = 'usr_owner'
    cache.setQueryData(['auth', 'session'], session())
    await review()
    await click('Cancel')
    failure['get /quota-requests/tqr_one'] = 404
    await click('Refresh')
    await until(() =>
      expect(document.body.textContent).not.toContain('Controlled monthly allowance'),
    )
    expect(document.body.textContent).not.toContain('Submission snapshot')
    expect(posts()).toHaveLength(0)
  })
  it('late decision after actor change neither saves nor repopulates obsolete detail', async () => {
    actor = 'usr_owner'
    cache.setQueryData(['auth', 'session'], session())
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await review()
    await click('Confirm decision')
    await until(() => expect(posts()).toHaveLength(1))
    actor = 'usr_other'
    failure['get /quota-requests/tqr_one'] = 404
    await act(async () => {
      cache.setQueryData(['auth', 'session'], session())
    })
    cache.removeQueries({ queryKey: ['team-request-detail', 'usr_owner'], exact: false })
    await act(async () => {
      release()
    })
    await until(() => expect(document.body.textContent).not.toContain('Approval progress'))
    expect(document.body.textContent).not.toContain('The reviewed decision is saved')
    expect(
      cache.getQueryData(['team-request-detail', 'usr_owner', false, 'tqr_one']),
    ).toBeUndefined()
  })
  it('global records require their exact read permission and remain read-only', async () => {
    permissions = ['teams.quota_requests.read_all']
    await mount('/admin/quota-requests?request=tqr_one')
    await until(() => expect(document.body.textContent).toContain('Approval progress'))
    expect(host.textContent).toContain('1 records')
    expect(document.body.textContent).not.toContain('Confirm decision')
    expect(
      [...document.querySelectorAll('button')].some((item) => item.textContent === 'Approve'),
    ).toBe(false)
    const workspaceLink = [...document.querySelectorAll<HTMLAnchorElement>('a')].find(
      (link) => link.textContent === 'Go to workspace',
    )
    expect(workspaceLink?.getAttribute('href')).toBe('/quota-requests?tab=pending&request=tqr_one')
    expect(posts()).toHaveLength(0)
  })
  it('admin role or generic Team read alone does not fetch platform request records', async () => {
    permissions = ['teams.read_all']
    await mount('/admin/quota-requests')
    await until(() => expect(host.textContent).toContain('You do not have access'))
    expect(requests.some((request) => request.url?.startsWith('/admin/quota-requests'))).toBe(false)
  })
  it('filters are conjunctive and clear stale rows before a new response', async () => {
    await mount()
    await until(() => expect(host.querySelector('table')).toBeTruthy())
    await input('Quota type', 'money')
    await until(() => expect(host.textContent).toContain('No requests in this view'))
    await input('Status', 'approved')
    await until(() =>
      expect(
        requests.some(
          (request) =>
            request.url === '/quota-requests' &&
            request.params.dimension === 'money' &&
            request.params.status === 'approved',
        ),
      ).toBe(true),
    )
  })
  it('navigation exposes the workspace independently and gates the global record link', async () => {
    await mount('/quota-requests', true)
    await until(() => expect(host.querySelector('a[href="/quota-requests"]')).toBeTruthy())
    expect(host.querySelector('a[href="/admin/quota-requests"]')).toBeNull()
    permissions = ['teams.quota_requests.read_all']
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['permissions'] })
    })
    await until(() => expect(host.querySelector('a[href="/admin/quota-requests"]')).toBeTruthy())
  })
})
