import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import {
  createProjectRequest,
  projectRequestLimitsContext,
  projectPolicyRequestDetail,
} from '@/api/project-requests'
import { sessionKey } from '@/hooks/use-auth'
import i18n from '@/i18n'
import type { ResourceRecord } from '@/types/resources'
import type {
  ProjectRequest,
  ProjectPolicyRequest,
  ProjectRequestLimitsContext,
  ProjectRateLimitRequestDetail,
  CreateProjectPolicyRequest,
} from '@/types/project-requests'
import ResourceDetailPage from '@/views/resources/detail'
import ProjectRequestsPanel from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const adapter = client.defaults.adapter
const projectId = 'prj_rate'
const base = `/projects/${projectId}`
let host: HTMLDivElement, root: Root, cache: QueryClient
let actor: string, csrf: string, permissions: string[], project: ResourceRecord
let context: ProjectRequestLimitsContext, seed: ProjectRateLimitRequestDetail
let requests: InternalAxiosRequestConfig[],
  outcomes: Partial<Record<'QUOTA' | 'RATE_LIMIT' | 'decision', number[]>>
let receipts: Map<string, ProjectPolicyRequest>
let application: 'pending' | 'applied' | 'superseded'
let receiptOverride: unknown, contextOverride: unknown, detailOverride: unknown
let firstDispatch: ReturnType<typeof barrier> | undefined
let decisionGate: ReturnType<typeof barrier> | undefined
function barrier() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  return { promise, release }
}
function auth() {
  return {
    user: { id: actor, name: 'Member', email: 'member@example.invalid', role: 'member' },
    csrf_token: csrf,
  }
}
function detail(): ProjectRateLimitRequestDetail {
  return {
    ...seed,
    current_rate_limit: structuredClone(context.current_rate_limit),
    current_policy_etag: context.policy_etag,
    platform_currency: context.platform_currency,
    approval_review_etag: seed.status === 'pending' ? context.review_etag : undefined,
    ...(seed.status === 'approved'
      ? { application_status: application, runtime_applied: application === 'applied' }
      : {}),
  }
}
function failure(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('Sanitized fixture failure', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: { code: status },
  })
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_manager'
  csrf = 'current-csrf'
  permissions = ['projects.limits.write']
  project = {
    id: projectId,
    name: 'Rate Project',
    description: 'Controlled requests',
    status: 'active',
    created_at: '2026-10-02T00:00:00Z',
    model_ids: [],
    managers: [
      { id: 'manager', user_id: actor, name: 'Manager', email: 'manager@example.invalid' },
    ],
  }
  context = {
    project_id: projectId,
    review_etag: 'a'.repeat(64),
    policy_etag: 'policy-before',
    current_quota: { tokens_month: 1000, money_month: '1.000000000000000001', currency: 'USD' },
    current_rate_limit: { rpm: 60, tpm: null, concurrency: 2 },
    platform_currency: 'USD',
  }
  seed = {
    id: 'req_rate',
    project_id: projectId,
    applicant_user_id: 'usr_applicant',
    kind: 'RATE_LIMIT',
    reason: 'Rate workload',
    status: 'pending',
    created_at: '2026-10-02T00:00:00Z',
    decided_at: null,
    baseline_rate_limit: { rpm: 20, tpm: null, concurrency: null },
    requested_rate_limit: { rpm: 100, tpm: 9007199254740991, concurrency: 0 },
    baseline_policy_etag: 'policy-at-submission',
    current_rate_limit: structuredClone(context.current_rate_limit),
    current_policy_etag: context.policy_etag,
    platform_currency: 'USD',
    approval_review_etag: context.review_etag,
  }
  requests = []
  outcomes = {}
  receipts = new Map()
  application = 'applied'
  receiptOverride = undefined
  contextOverride = undefined
  detailOverride = undefined
  firstDispatch = undefined
  decisionGate = undefined
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  cache.setQueryData(sessionKey, auth())
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session') response.data = auth()
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === base) response.data = structuredClone(project)
    else if (config.url === `${base}/request-limits-context`)
      response.data = contextOverride ?? structuredClone(context)
    else if (config.url === `${base}/requests/req_rate`) {
      if (outcomes.decision?.[0] === 403 && config.method === 'get') throw failure(config, 403)
      response.data = detailOverride ?? detail()
    } else if (config.url === `${base}/requests/req_rate/decision`) {
      const body = JSON.parse(config.data)
      if (config.headers.get('X-CSRF-Token') !== csrf) throw failure(config, 403)
      const status = outcomes.decision?.shift() ?? 200
      if (status === 200 || status === 503) {
        if (seed.status === 'pending') {
          seed.status =
            body.action === 'approve'
              ? 'approved'
              : body.action === 'reject'
                ? 'rejected'
                : 'withdrawn'
          seed.decision_actor_id = actor
          seed.decision_reason = body.reason
          seed.decided_at = '2026-10-02T01:00:00Z'
          if (body.action === 'approve') {
            seed.approved_rate_limit = {
              ...context.current_rate_limit,
              ...seed.requested_rate_limit,
            }
            seed.approved_policy_etag = 'policy-approved'
          }
        }
      }
      if (status !== 200) throw failure(config, status)
      response.data = receiptOverride ?? detail()
      if (decisionGate) await decisionGate.promise
    } else if (config.url === `${base}/requests` && config.method === 'post') {
      const body = JSON.parse(config.data) as CreateProjectPolicyRequest
      if (config.headers.get('X-CSRF-Token') !== csrf) throw failure(config, 403)
      if (firstDispatch && requests.filter((request) => request.method === 'post').length === 1)
        await firstDispatch.promise
      const status = outcomes[body.kind]?.shift() ?? 200
      if ((status === 200 || status === 503) && !receipts.has(body.request_id)) {
        const common = {
          id: `req_saved_${body.kind}`,
          project_id: projectId,
          applicant_user_id: actor,
          status: 'pending' as const,
          reason: body.reason,
          created_at: '2026-10-02T00:00:00Z',
          decided_at: null,
          baseline_policy_etag: context.policy_etag,
        }
        receipts.set(
          body.request_id,
          body.kind === 'QUOTA'
            ? {
                ...common,
                kind: 'QUOTA',
                baseline_quota: structuredClone(context.current_quota),
                requested_quota: body.quota,
              }
            : {
                ...common,
                kind: 'RATE_LIMIT',
                baseline_rate_limit: structuredClone(context.current_rate_limit),
                requested_rate_limit: body.rate_limit,
              },
        )
      }
      if (status !== 200) throw failure(config, status)
      response.data = receiptOverride ?? receipts.get(body.request_id)
    } else if (config.url === `${base}/requests`) {
      const rows: ProjectRequest[] = [seed, ...receipts.values()]
      response.data = { items: rows, next_cursor: null }
    } else throw new Error(`Unexpected scoped request: ${config.url}`)
    return response
  }
})
afterEach(async () => {
  firstDispatch?.release()
  decisionGate?.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = adapter
  await i18n.changeLanguage('en')
})
async function until(check: () => void) {
  for (let index = 0; index < 100; index++) {
    await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
    try {
      check()
      return
    } catch (error) {
      if (index === 99) throw error
    }
  }
}
async function mount(resource = false) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter initialEntries={[`${base}?tab=resources`]}>
          {resource ? (
            <Routes>
              <Route
                path="/projects/:resourceId"
                element={<ResourceDetailPage kind="projects" />}
              />
            </Routes>
          ) : (
            <ProjectRequestsPanel project={project} />
          )}
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain('Project resource requests'))
  await until(() => expect(host.textContent).not.toContain('Loading'))
}
const dialog = () => document.querySelector<HTMLElement>('[role="dialog"]')!
const button = (label: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label,
  )!
async function click(label: string) {
  expect(button(label), label).toBeTruthy()
  await act(async () => button(label).click())
}
async function fill(label: string, value: string) {
  const input = [
    ...document.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>('input,textarea'),
  ].find((item) => item.getAttribute('aria-label') === label)!
  expect(input, label).toBeTruthy()
  await act(async () => {
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
    dialog()
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
}
const posts = () => requests.filter((request) => request.method === 'post')
const body = (index: number) => JSON.parse(posts()[index].data) as CreateProjectPolicyRequest
async function apply(mixed = false) {
  await click('Request quota and limit adjustment')
  await until(() => expect(dialog().textContent).toContain('Current request limits'))
  if (mixed) await fill('Monthly tokens', '2000')
  await fill('RPM', '0')
  await fill('Reason', 'Controlled rate workload')
}
async function review() {
  await click('Review request')
  await until(() => expect(dialog().textContent).toContain('Request limits at submission'))
}

describe('Project request-limit applications', () => {
  it.each(['missing-status', 'missing-baseline', 'wrong-kind'])(
    'keeps a malformed 200 rate creation receipt %s uncertain and retries only the original rate',
    async (defect) => {
      const malformed: Record<string, unknown> = { ...seed }
      if (defect === 'missing-status') delete malformed.status
      if (defect === 'missing-baseline') delete malformed.baseline_rate_limit
      if (defect === 'wrong-kind') malformed.kind = 'MODEL_ACCESS'
      receiptOverride = malformed
      await mount()
      await apply()
      await submit()
      await until(() => expect(dialog().textContent).toContain('Outcome unknown'))
      expect(host.textContent).not.toContain('Request-limit application submitted.')
      const original = posts()[0]
      receiptOverride = undefined
      await click('Retry the same action')
      await until(() => expect(host.textContent).toContain('Request-limit application submitted.'))
      expect(posts()[1].data).toBe(original.data)
      expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    },
  )
  it('submits finite zero and maximum safe integers while omitting untouched quota and concurrency', async () => {
    await mount()
    await apply()
    await fill('TPM', '9007199254740991')
    await submit()
    await until(() =>
      expect(host.textContent).toContain(
        'Request-limit application submitted. Effective limits are unchanged.',
      ),
    )
    expect(body(0)).toMatchObject({
      kind: 'RATE_LIMIT',
      rate_limit: { rpm: 0, tpm: 9007199254740991 },
      reason: 'Controlled rate workload',
    })
    expect(Object.keys(body(0))).not.toContain('quota')
    expect(posts()[0].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
    expect(
      requests.some(
        (request) =>
          request.url === `${base}/request-quota-context` ||
          request.url === '/admin/prices/currency',
      ),
    ).toBe(false)
  })
  it.each(['-1', '1.5', '9007199254740992', 'NaN'])(
    'rejects invalid request limits %s before dispatch',
    async (value) => {
      await mount()
      await apply()
      await fill('TPM', value)
      await submit()
      expect(posts()).toHaveLength(0)
      expect(dialog().textContent).toContain('non-negative safe integer')
    },
  )
  it('dispatches monthly and rate separately in order and reports success only after both receipts', async () => {
    await mount()
    await apply(true)
    await fill('Monthly money (USD)', '999999999999999999.000000000000000001')
    await submit()
    await until(() => expect(host.textContent).toContain('Both applications are saved.'))
    expect(posts()).toHaveLength(2)
    expect(body(0).kind).toBe('QUOTA')
    expect(body(1).kind).toBe('RATE_LIMIT')
    expect(body(0).request_id).not.toBe(body(1).request_id)
    expect(posts()[0].headers.get('If-Match')).toBe(posts()[1].headers.get('If-Match'))
    expect(body(0)).toMatchObject({
      quota: {
        tokens_month: 2000,
        money_month: '999999999999999999.000000000000000001',
        currency: 'USD',
      },
    })
  })
  it('shows the saved half and retains only the unknown rate intent through CSRF rotation and a rejected retry', async () => {
    outcomes.RATE_LIMIT = [503, 409, 200]
    await mount()
    await apply(true)
    await submit()
    await until(() => expect(dialog().textContent).toContain('Outcome unknown'))
    expect(dialog().textContent).toContain('Request saved')
    expect(host.textContent).not.toContain('Both applications are saved.')
    const original = posts()[1]
    const uuid = body(1).request_id
    context.review_etag = 'b'.repeat(64)
    await act(async () => {
      csrf = 'rotated-csrf'
      cache.setQueryData(sessionKey, auth())
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await click('Retry the same action')
    await until(() => expect(posts()).toHaveLength(3))
    expect(button('Use this reviewed context')).toBeUndefined()
    expect(dialog().textContent).toContain('Outcome unknown')
    await click('Retry the same action')
    await until(() => expect(host.textContent).toContain('Both applications are saved.'))
    expect(posts()).toHaveLength(4)
    expect(body(2).kind).toBe('RATE_LIMIT')
    expect(body(3).request_id).toBe(uuid)
    expect(posts()[2].data).toBe(original.data)
    expect(posts()[3].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(posts()[3].headers.get('X-CSRF-Token')).toBe('rotated-csrf')
  })
  it('does not send rate while monthly outcome is unknown, then reconciles monthly before the first rate dispatch', async () => {
    outcomes.QUOTA = [503, 200]
    await mount()
    await apply(true)
    await submit()
    await until(() => expect(dialog().textContent).toContain('Not submitted'))
    expect(posts()).toHaveLength(1)
    expect(dialog().textContent).not.toContain('Both applications are saved.')
    const original = posts()[0]
    await click('Retry the same action')
    await until(() => expect(host.textContent).toContain('Both applications are saved.'))
    expect(posts()).toHaveLength(3)
    expect(posts()[1].data).toBe(original.data)
    expect(body(2).kind).toBe('RATE_LIMIT')
  })
  it('requires fresh explicit review before renewing only a definitively rejected remaining rate request', async () => {
    outcomes.RATE_LIMIT = [409, 200]
    await mount()
    await apply(true)
    await submit()
    await until(() => expect(dialog().textContent).toContain('server rejected this attempt'))
    expect(button('Use this reviewed context').disabled).toBe(true)
    const oldRate = body(1)
    context.review_etag = 'c'.repeat(64)
    context.current_rate_limit.rpm = 90
    await click('Refresh current quotas')
    await until(() => expect(button('Use this reviewed context').disabled).toBe(false))
    expect(button('Retry the same action').disabled).toBe(true)
    await click('Use this reviewed context')
    await click('Retry the same action')
    await until(() => expect(host.textContent).toContain('Both applications are saved.'))
    expect(posts()).toHaveLength(3)
    expect(body(2).kind).toBe('RATE_LIMIT')
    expect(body(2).request_id).not.toBe(oldRate.request_id)
    expect(body(2)).toMatchObject({ rate_limit: { rpm: 0 }, reason: oldRate.reason })
    expect(posts()[2].headers.get('If-Match')).toBe(`"${'c'.repeat(64)}"`)
  })
  it('does not continue a sequential batch after the original actor leaves during the first dispatch', async () => {
    firstDispatch = barrier()
    await mount()
    await apply(true)
    await submit()
    expect(posts()).toHaveLength(1)
    await act(async () => {
      actor = 'usr_other'
      cache.setQueryData(sessionKey, auth())
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await act(async () => firstDispatch!.release())
    await until(() => expect(dialog()).toBeNull())
    expect(posts()).toHaveLength(1)
    expect(host.textContent).not.toContain('Both applications are saved.')
  })
  it('preserves rate drafts and per-application results through live Chinese switching and nonbusy dismissal', async () => {
    outcomes.RATE_LIMIT = [503]
    await mount()
    await apply(true)
    await submit()
    await until(() => expect(button('Retry the same action')).toBeTruthy())
    await act(async () => i18n.changeLanguage('zh'))
    expect(dialog().textContent).toContain('结果未知')
    expect(dialog().querySelector<HTMLInputElement>('[aria-label="RPM"]')?.value).toBe('0')
    expect(dialog().querySelector('textarea')?.value).toBe('Controlled rate workload')
    await click('取消')
    expect(posts()).toHaveLength(2)
    await until(() => expect(dialog()).toBeNull())
    expect(host.textContent).toContain('关闭弹框不能确认结果')
  })
})

describe('Project request-limit review and contract boundaries', () => {
  it('does not refresh obsolete private queries after the actor leaves while a decision response is pending', async () => {
    decisionGate = barrier()
    await mount()
    await review()
    await click('Approve')
    await fill('Decision reason', 'Delayed original decision')
    await submit()
    expect(posts()).toHaveLength(1)
    const originalActor = actor
    const detailPath = `${base}/requests/req_rate`
    const readsBefore = requests.filter((request) => request.url === detailPath).length
    await act(async () => {
      actor = 'usr_other'
      permissions = []
      cache.setQueryData(sessionKey, auth())
      cache.removeQueries({ queryKey: ['project-rate-request-detail', originalActor] })
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await until(() => expect(dialog()).toBeNull())
    await act(async () => {
      decisionGate!.release()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(requests.filter((request) => request.url === detailPath)).toHaveLength(readsBefore)
    expect(
      cache.getQueryData(['project-rate-request-detail', originalActor, projectId, 'req_rate']),
    ).toBeUndefined()
    expect(host.textContent).not.toContain('Decision saved.')
  })
  it.each([
    { runtime_applied: true, application_status: 'pending' },
    { runtime_applied: true, application_status: 'superseded' },
    { runtime_applied: false, application_status: 'applied' },
  ])('rejects contradictory approved rate application flags %j', async (flags) => {
    detailOverride = {
      ...detail(),
      status: 'approved',
      decided_at: '2026-10-02T01:00:00Z',
      decision_actor_id: actor,
      approved_rate_limit: structuredClone(context.current_rate_limit),
      approved_policy_etag: 'policy-approved',
      ...flags,
    }
    await expect(projectPolicyRequestDetail(projectId, seed.id, 'RATE_LIMIT')).rejects.toThrow(
      'Invalid Project request-limit detail',
    )
  })
  it('requires a rejection reason and sends no policy validator for rejection', async () => {
    await mount()
    await review()
    await click('Reject')
    expect(button('Confirm Reject').disabled).toBe(true)
    await fill('Decision reason', 'The current rates remain sufficient')
    await submit()
    await until(() => expect(dialog().textContent).toContain('Decision saved.'))
    expect(posts()[0].headers.get('If-Match')).toBeUndefined()
    expect(JSON.parse(posts()[0].data)).toEqual({
      action: 'reject',
      reason: 'The current rates remain sufficient',
    })
    expect(dialog().querySelector('form')).toBeNull()
  })
  it('uses the minimal limits-only workspace and preserves the exact saved pending retry', async () => {
    project = {
      id: projectId,
      name: 'Rate Project',
      description: 'Controlled requests',
      status: 'active',
      request_workspace_only: true,
    } as ResourceRecord
    application = 'pending'
    await mount(true)
    await review()
    await click('Approve')
    expect(button('Confirm Approve').disabled).toBe(true)
    await fill('Decision reason', 'Controlled rate approval')
    await submit()
    await until(() =>
      expect(dialog().textContent).toContain(
        'Application of that exact policy revision is not confirmed.',
      ),
    )
    const original = posts()[0]
    application = 'applied'
    await click('Retry the same action')
    await until(() => expect(dialog().textContent).toContain('confirmed in the current runtime'))
    expect(posts()[1].data).toBe(original.data)
    expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(dialog().querySelector('form')).toBeNull()
    expect(requests.filter((request) => request.url === base)).toHaveLength(1)
    expect(
      requests.some(
        (request) =>
          request.url?.endsWith('/limits') ||
          request.url?.includes('candidates') ||
          request.url === '/projects',
      ),
    ).toBe(false)
  })
  it('keeps unknown decision intent through fresh CSRF and later supersession without changing its header', async () => {
    outcomes.decision = [503, 200]
    await mount()
    await review()
    await click('Approve')
    await fill('Decision reason', 'Original rate decision')
    await submit()
    await until(() => expect(button('Retry the same action')).toBeTruthy())
    const original = posts()[0]
    application = 'superseded'
    context.review_etag = 'd'.repeat(64)
    context.current_rate_limit.rpm = 999
    await act(async () => {
      csrf = 'new-session-csrf'
      cache.setQueryData(sessionKey, auth())
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await click('Refresh request details')
    await until(() => expect(dialog().textContent).toContain('a newer policy has replaced'))
    expect(button('Use this reviewed context')).toBeUndefined()
    await click('Retry the same action')
    await until(() => expect(dialog().textContent).toContain('Decision saved.'))
    expect(posts()[1].data).toBe(original.data)
    expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(posts()[1].headers.get('X-CSRF-Token')).toBe('new-session-csrf')
    expect(dialog().querySelector('form')).toBeNull()
    expect(dialog().textContent).not.toContain('confirmed in the current runtime')
  })
  it('preserves decision reason and requires fresh explicit review after a policy conflict', async () => {
    outcomes.decision = [409, 200]
    await mount()
    await review()
    await click('Approve')
    await fill('Decision reason', 'Preserved reviewer reason')
    await submit()
    await until(() => expect(button('Use this reviewed context').disabled).toBe(true))
    context.review_etag = 'e'.repeat(64)
    await click('Refresh request details')
    await until(() => expect(button('Use this reviewed context').disabled).toBe(false))
    expect(dialog().querySelector('textarea')?.value).toBe('Preserved reviewer reason')
    await click('Use this reviewed context')
    await submit()
    await until(() => expect(dialog().textContent).toContain('Decision saved.'))
    expect(posts()[1].headers.get('If-Match')).toBe(`"${'e'.repeat(64)}"`)
  })
  it('hides rate rows from model-only reviewers and grants current managers no implicit approval', async () => {
    permissions = ['projects.models.write']
    project.managers = []
    await mount()
    expect(host.textContent).not.toContain('Rate workload')
    expect(button('Review request')).toBeUndefined()
    permissions = []
    project.managers = [
      { id: 'manager', user_id: actor, name: 'Manager', email: 'manager@example.invalid' },
    ]
    await act(async () => {
      cache.setQueryData(['permissions', actor], [])
    })
    await until(() => expect(button('Review request')).toBeTruthy())
    await review()
    expect(button('Approve')).toBeUndefined()
    expect(button('Reject')).toBeUndefined()
    expect(posts()).toHaveLength(0)
  })
  it('blocks self approval and permits the applicant to withdraw on an inactive Project', async () => {
    seed.applicant_user_id = actor
    project.status = 'disabled'
    await mount()
    await review()
    expect(button('Approve')).toBeUndefined()
    await click('Withdraw')
    await submit()
    await until(() => expect(dialog().textContent).toContain('Decision saved.'))
    expect(posts()[0].headers.get('If-Match')).toBeUndefined()
  })
  it('hides cached rate details after failed reauthorization', async () => {
    await mount()
    await review()
    expect(dialog().textContent).toContain('Rate workload')
    outcomes.decision = [403]
    await click('Refresh request details')
    await until(() => expect(dialog().querySelector('[role="alert"]')).not.toBeNull())
    expect(dialog().textContent).not.toContain('Rate workload')
    expect(button('Approve')).toBeUndefined()
  })
  it.each(['pending', 'missing-approved', 'wrong-terminal'])(
    'treats malformed 200 decision %s as unknown and retains the original intent',
    async (defect) => {
      receiptOverride = {
        ...detail(),
        status:
          defect === 'wrong-terminal' ? 'rejected' : defect === 'pending' ? 'pending' : 'approved',
        decided_at: '2026-10-02T01:00:00Z',
        decision_actor_id: actor,
        approved_policy_etag: 'policy-approved',
      }
      await mount()
      await review()
      await click('Approve')
      await fill('Decision reason', 'Malformed receipt')
      await submit()
      await until(() => expect(button('Retry the same action')).toBeTruthy())
      expect(dialog().textContent).not.toContain('Decision saved.')
      const original = posts()[0]
      receiptOverride = undefined
      await click('Retry the same action')
      await until(() => expect(dialog().textContent).toContain('Decision saved.'))
      expect(posts()[1].data).toBe(original.data)
    },
  )
  it('keeps English default and switches exact baseline/current rate values into Chinese', async () => {
    await mount()
    await review()
    expect(dialog().textContent).toContain('9,007,199,254,740,991')
    await act(async () => i18n.changeLanguage('zh'))
    expect(dialog().textContent).toContain('提交时的请求限制')
    expect(dialog().textContent).toContain('最大并发')
    expect(dialog().textContent).toContain('9,007,199,254,740,991')
  })
  it.each([
    {},
    { rpm: null },
    { rpm: 1.5 },
    { rpm: 9007199254740992 },
    { rpm: 1, currency: 'USD' },
  ])('rejects malformed rate history patches %j', async (patch) => {
    detailOverride = { ...detail(), requested_rate_limit: patch }
    await expect(projectPolicyRequestDetail(projectId, seed.id, 'RATE_LIMIT')).rejects.toThrow(
      'Invalid Project request-limit detail',
    )
  })
  it('rejects malformed coherent context and any kind or resource mismatch', async () => {
    contextOverride = { ...context, current_rate_limit: { rpm: 1, tpm: null } }
    await expect(projectRequestLimitsContext(projectId)).rejects.toThrow(
      'Invalid Project request limits context',
    )
    detailOverride = { ...detail(), project_id: 'prj_other' }
    await expect(projectPolicyRequestDetail(projectId, seed.id, 'RATE_LIMIT')).rejects.toThrow()
    detailOverride = { ...detail(), kind: 'QUOTA' }
    await expect(projectPolicyRequestDetail(projectId, seed.id, 'RATE_LIMIT')).rejects.toThrow()
  })
  it.each(['approved', 'rejected', 'withdrawn'] as const)(
    'accepts valid historical %s rate creation receipts',
    async (status) => {
      receiptOverride = {
        ...seed,
        status,
        decided_at: '2026-10-02T01:00:00Z',
        decision_actor_id: actor,
        ...(status === 'approved'
          ? {
              approved_rate_limit: { rpm: 100, tpm: null, concurrency: 0 },
              approved_policy_etag: 'policy-approved',
            }
          : {}),
      }
      const result = await createProjectRequest(
        projectId,
        {
          request_id: 'original-rate-request',
          kind: 'RATE_LIMIT',
          rate_limit: { rpm: 100 },
          reason: 'Historical workload',
        },
        csrf,
        context.review_etag,
      )
      expect(result.status).toBe(status)
    },
  )
})
