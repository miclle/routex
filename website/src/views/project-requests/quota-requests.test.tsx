import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import {
  createProjectRequest,
  projectQuotaContext,
  projectQuotaRequestDetail,
} from '@/api/project-requests'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import type { ResourceRecord } from '@/types/resources'
import type {
  ProjectModelRequest,
  ProjectQuotaContext,
  ProjectQuotaRequestDetail,
  ProjectRequest,
} from '@/types/project-requests'
import ResourceDetailPage from '@/views/resources/detail'
import ProjectRequestsPanel from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
const projectId = 'prj_quota'
const requestId = 'req_quota'
const base = `/projects/${projectId}`
let host: HTMLDivElement, root: Root, cache: QueryClient
let actor: string, permissions: string[], project: ResourceRecord
let csrfOverride: string | undefined
let context: ProjectQuotaContext, record: ProjectQuotaRequestDetail, rows: ProjectRequest[]
let requests: InternalAxiosRequestConfig[], failures: Record<string, number>
let contextOverride: unknown, detailOverride: unknown
let receiptOverride: unknown
let detailGate: ReturnType<typeof barrier> | undefined
let resourceGate: ReturnType<typeof barrier> | undefined
let application: 'pending' | 'applied' | 'superseded', approvalTag: string
function barrier() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  return { promise, release: () => release() }
}
function auth() {
  return {
    user: { id: actor, name: 'Reviewer', email: 'reviewer@example.invalid', role: 'member' },
    csrf_token: csrfOverride ?? `csrf-${actor}`,
  }
}
function model(): ProjectModelRequest {
  return {
    id: 'req_model',
    project_id: projectId,
    applicant_user_id: 'usr_other',
    kind: 'MODEL_ACCESS',
    baseline_model_ids: [],
    requested_model_ids: ['mdl_one'],
    reason: 'Existing model request',
    status: 'pending',
    created_at: '2026-10-02T00:00:00Z',
    decided_at: null,
  }
}
function freshDetail() {
  return {
    ...record,
    current_quota: structuredClone(context.current_quota),
    current_policy_etag: context.policy_etag,
    platform_currency: context.platform_currency,
    approval_review_etag: record.status === 'pending' ? approvalTag : undefined,
    ...(record.status === 'approved'
      ? { runtime_applied: application === 'applied', application_status: application }
      : {}),
  }
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_manager'
  csrfOverride = undefined
  permissions = ['projects.limits.write']
  project = {
    id: projectId,
    name: 'Quota Project',
    description: 'Scoped resource',
    status: 'active',
    created_at: '2026-10-02T00:00:00Z',
    model_ids: [],
    managers: [
      { id: 'pmg_one', user_id: actor, name: 'Manager', email: 'manager@example.invalid' },
    ],
  }
  context = {
    project_id: projectId,
    review_etag: 'a'.repeat(64),
    policy_etag: 'policy-before',
    platform_currency: 'USD',
    current_quota: { tokens_month: 1000, money_month: '12.000000000000000001', currency: 'USD' },
  }
  approvalTag = 'b'.repeat(64)
  record = {
    id: requestId,
    project_id: projectId,
    applicant_user_id: 'usr_applicant',
    kind: 'QUOTA',
    baseline_quota: { tokens_month: 100, money_month: null, currency: '' },
    baseline_policy_etag: 'policy-original',
    requested_quota: { tokens_month: 2000, money_month: '15.000000000000000001', currency: 'USD' },
    reason: 'Contract workload',
    status: 'pending',
    created_at: '2026-10-02T00:00:00Z',
    decided_at: null,
    current_quota: structuredClone(context.current_quota),
    current_policy_etag: context.policy_etag,
    platform_currency: 'USD',
    approval_review_etag: approvalTag,
  }
  rows = [record]
  requests = []
  failures = {}
  contextOverride = undefined
  detailOverride = undefined
  receiptOverride = undefined
  detailGate = undefined
  resourceGate = undefined
  application = 'applied'
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  cache.setQueryData(sessionKey, auth())
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const key = `${config.method} ${config.url}`
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === `${base}/requests/${requestId}` && detailGate) await detailGate.promise
    if (config.url === base && resourceGate) await resourceGate.promise
    if (failures[key]) {
      response.status = failures[key]
      throw new AxiosError('Private fixture diagnostic', '', config, undefined, response)
    }
    if (config.url === '/auth/session') response.data = auth()
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === base) response.data = structuredClone(project)
    else if (config.url === `${base}/request-quota-context`)
      response.data = contextOverride ?? structuredClone(context)
    else if (config.url === `${base}/requests/${requestId}`)
      response.data = detailOverride ?? freshDetail()
    else if (config.url === `${base}/requests/${requestId}/decision`) {
      const body = JSON.parse(config.data)
      if (record.status === 'pending') {
        record.status =
          body.action === 'approve'
            ? 'approved'
            : body.action === 'reject'
              ? 'rejected'
              : 'withdrawn'
        record.decision_actor_id = actor
        record.decision_reason = body.reason
        record.decided_at = '2026-10-02T01:00:00Z'
        if (body.action === 'approve') {
          record.approved_quota = { ...context.current_quota, ...record.requested_quota }
          record.approved_policy_etag = 'policy-approved'
        }
      }
      response.data = freshDetail()
    } else if (config.url === `${base}/requests`) {
      if (config.method === 'post') {
        const body = JSON.parse(config.data)
        record = {
          ...record,
          applicant_user_id: actor,
          baseline_quota: structuredClone(context.current_quota),
          requested_quota: body.quota,
          reason: body.reason,
          status: 'pending',
        }
        rows = [record]
        response.data = freshDetail()
      } else
        response.data = {
          items: rows.filter(
            (row) => !config.params?.status || row.status === config.params.status,
          ),
          next_cursor: null,
        }
    } else throw new Error(`Unexpected quota UI request: ${key}`)
    if (config.method === 'post' && receiptOverride !== undefined) response.data = receiptOverride
    return response
  }
})
afterEach(async () => {
  detailGate?.release()
  resourceGate?.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  await i18n.changeLanguage('en')
})
async function until(check: () => void) {
  for (let attempt = 0; attempt < 100; attempt++) {
    await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
    try {
      check()
      return
    } catch (error) {
      if (attempt === 99) throw error
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
function dialog() {
  return document.querySelector<HTMLElement>('[role="dialog"]')!
}
function button(label: string, scope: ParentNode = document) {
  return [...scope.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label,
  )!
}
async function click(label: string, scope: ParentNode = document) {
  const found = button(label, scope)
  expect(found, label).toBeTruthy()
  await act(async () => found.click())
}
async function fill(label: string, value: string) {
  const input = [
    ...document.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>('input, textarea'),
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
async function apply() {
  await click('Request quota adjustment')
  await until(() => expect(dialog().textContent).toContain('Platform currency: USD'))
}
async function review() {
  await click('Review request')
  await until(() => expect(dialog().textContent).toContain('Quota baseline at submission'))
}

describe('Project monthly quota applications', () => {
  it.each(['approved', 'rejected', 'withdrawn'] as const)(
    'accepts a valid historical %s creation receipt without requiring it to remain pending',
    async (status) => {
      receiptOverride = {
        ...record,
        status,
        decided_at: '2026-10-02T01:00:00Z',
        decision_actor_id: actor,
        ...(status === 'approved'
          ? {
              approved_quota: structuredClone(context.current_quota),
              approved_policy_etag: 'policy-approved',
            }
          : {}),
      }
      const saved = await createProjectRequest(
        projectId,
        {
          request_id: 'original-historical-request',
          kind: 'QUOTA',
          quota: { tokens_month: 2000 },
          reason: 'Original historical request',
        },
        auth().csrf_token,
        context.review_etag,
      )
      expect(saved.kind).toBe('QUOTA')
      expect(saved.status).toBe(status)
    },
  )
  it.each([
    ['creation', 'missing-status'],
    ['creation', 'invalid-baseline'],
    ['decision', 'pending-status'],
    ['decision', 'wrong-terminal-status'],
    ['decision', 'missing-approved-snapshot'],
    ['decision', 'missing-approved-revision'],
  ] as const)(
    'treats malformed 200 %s receipt %s as uncertain and retains the exact retry intent',
    async (operation, defect) => {
      await mount()
      if (operation === 'creation') {
        await apply()
        await fill('Monthly tokens', '2000')
        await fill('Reason', 'Preserve malformed receipt intent')
      } else {
        await review()
        await click('Approve')
        await fill('Decision reason', 'Preserve malformed receipt intent')
      }
      const malformed: Record<string, unknown> = {
        ...freshDetail(),
        ...(operation === 'decision'
          ? {
              status: 'approved',
              decided_at: '2026-10-02T01:00:00Z',
              decision_actor_id: actor,
              approved_quota: structuredClone(context.current_quota),
              approved_policy_etag: 'policy-approved',
            }
          : {}),
      }
      if (defect === 'missing-status') delete malformed.status
      if (defect === 'invalid-baseline') malformed.baseline_quota = {}
      if (defect === 'pending-status') {
        malformed.status = 'pending'
        malformed.decided_at = null
      }
      if (defect === 'wrong-terminal-status') malformed.status = 'rejected'
      if (defect === 'missing-approved-snapshot') delete malformed.approved_quota
      if (defect === 'missing-approved-revision') delete malformed.approved_policy_etag
      receiptOverride = malformed
      await submit()
      await until(() => expect(button('Retry the same action')).toBeTruthy())
      expect(dialog()?.textContent).toContain(
        operation === 'creation'
          ? 'The request may already be saved.'
          : 'The decision may already be saved.',
      )
      expect(host.textContent).not.toContain('Quota request submitted.')
      expect(dialog()?.textContent).not.toContain('Decision saved.')
      const original = posts()[0]
      receiptOverride = undefined
      await click('Retry the same action')
      await until(() => expect(posts()).toHaveLength(2))
      expect(posts()[1].data).toBe(original.data)
      expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
      await until(() =>
        expect(operation === 'creation' ? host.textContent : dialog()?.textContent).toContain(
          operation === 'creation'
            ? 'Quota request submitted. Effective quotas are unchanged.'
            : 'Decision saved.',
        ),
      )
    },
  )
  it.each(['creation', 'decision'])(
    'retries uncertain %s with the current same-actor CSRF while preserving the original intent',
    async (operation) => {
      await mount()
      const path =
        operation === 'creation' ? `${base}/requests` : `${base}/requests/${requestId}/decision`
      if (operation === 'creation') {
        await apply()
        await fill('Monthly tokens', '2000')
        await fill('Reason', 'Original reviewed creation')
      } else {
        await review()
        await click('Approve')
        await fill('Decision reason', 'Original reviewed approval')
      }
      failures[`post ${path}`] = 503
      await submit()
      await until(() => expect(button('Retry the same action')).toBeTruthy())
      const original = posts()[0]
      expect(original.headers.get('X-CSRF-Token')).toBe(`csrf-${actor}`)
      await act(async () => {
        csrfOverride = 'current-session-csrf'
        cache.setQueryData(sessionKey, auth())
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      delete failures[`post ${path}`]
      await click('Retry the same action')
      await until(() => expect(posts()).toHaveLength(2))
      expect(posts()[1].data).toBe(original.data)
      expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
      expect(posts()[1].headers.get('X-CSRF-Token')).toBe('current-session-csrf')
      await until(() =>
        expect(operation === 'creation' ? host.textContent : dialog()?.textContent).toContain(
          operation === 'creation'
            ? 'Quota request submitted. Effective quotas are unchanged.'
            : 'confirmed in the current runtime',
        ),
      )
    },
  )
  it.each(['creation', 'decision'])(
    'allows dismissing uncertain %s without dispatching again or reporting completion',
    async (operation) => {
      await mount()
      if (operation === 'creation') {
        await apply()
        await fill('Monthly tokens', '2500')
        await fill('Reason', 'Unknown outcome')
        failures[`post ${base}/requests`] = 503
      } else {
        await review()
        await click('Approve')
        await fill('Decision reason', 'Unknown outcome')
        failures[`post ${base}/requests/${requestId}/decision`] = 503
      }
      await submit()
      await until(() => expect(button('Retry the same action')).toBeTruthy())
      await click('Cancel')
      await until(() => expect(dialog()).toBeNull())
      expect(posts()).toHaveLength(1)
      expect(host.textContent).toContain('Dismissal did not confirm its outcome.')
      expect(host.textContent).not.toContain('Quota request submitted.')
      expect(host.textContent).not.toContain('Decision saved.')
      expect(
        requests.filter((request) => request.url === `${base}/requests` && request.method === 'get')
          .length,
      ).toBeGreaterThan(1)
    },
  )
  it('submits zero tokens with the reviewed context while blank money remains omitted', async () => {
    rows = []
    await mount()
    await apply()
    expect(button('Submit request').disabled).toBe(true)
    await fill('Monthly tokens', '0')
    await fill('Reason', '  Controlled request  ')
    await submit()
    await until(() =>
      expect(host.textContent).toContain(
        'Quota request submitted. Effective quotas are unchanged.',
      ),
    )
    const body = JSON.parse(posts()[0].data)
    expect(body).toMatchObject({
      kind: 'QUOTA',
      quota: { tokens_month: 0 },
      reason: 'Controlled request',
    })
    expect(Object.keys(body.quota)).toEqual(['tokens_month'])
    expect(body.request_id).toMatch(/^[a-f0-9-]{36}$/)
    expect(posts()[0].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
    expect(posts()[0].headers.get('X-CSRF-Token')).toBe('csrf-usr_manager')
    expect(
      requests.some(
        (request) =>
          request.url?.includes('request-model-candidates') ||
          request.url === '/admin/prices/currency',
      ),
    ).toBe(false)
  })

  it('preserves exact money and the original creation intent across uncertain and rejected retries', async () => {
    rows = []
    await mount()
    await apply()
    await fill('Monthly money (USD)', '999999999999999999.000000000000000001')
    await fill('Reason', 'Exact contract')
    failures[`post ${base}/requests`] = 503
    await submit()
    await until(() => expect(dialog().textContent).toContain('The request may already be saved.'))
    const original = posts()[0]
    expect(JSON.parse(original.data).quota).toEqual({
      money_month: '999999999999999999.000000000000000001',
      currency: 'USD',
    })
    context = { ...context, review_etag: 'c'.repeat(64), platform_currency: 'EUR' }
    await act(async () => {
      void cache.invalidateQueries({
        queryKey: ['project-request-quota-context', actor, projectId],
      })
    })
    await until(() => expect(dialog().textContent).toContain('Platform currency: EUR'))
    expect((dialog().querySelector('input') as HTMLInputElement).disabled).toBe(true)
    expect(button('Use this reviewed context')).toBeUndefined()
    expect(button('Cancel').disabled).toBe(false)
    failures[`post ${base}/requests`] = 409
    await click('Retry the same action')
    await until(() =>
      expect(dialog().textContent).toContain('The request or Project state changed.'),
    )
    expect(posts()[1].data).toBe(original.data)
    expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    failures[`post ${base}/requests`] = 0
    project.status = 'disabled'
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <MemoryRouter>
            <ProjectRequestsPanel project={project} />
          </MemoryRouter>
        </QueryClientProvider>,
      ),
    )
    expect(button('Retry the same action').disabled).toBe(false)
    await click('Retry the same action')
    await until(() => expect(dialog()).toBeNull())
    expect(posts()[2].data).toBe(original.data)
    expect(posts()[2].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  })

  it('retains drafts on conflict and requires fresh explicit review after denomination changes', async () => {
    rows = []
    await mount()
    await apply()
    await fill('Monthly money (USD)', '0.000000000000000001')
    await fill('Reason', 'Preserved draft')
    failures[`post ${base}/requests`] = 409
    await submit()
    await until(() => expect(button('Refresh current quotas')).toBeTruthy())
    expect(button('Use this reviewed context').disabled).toBe(true)
    context = {
      ...context,
      review_etag: 'c'.repeat(64),
      platform_currency: 'EUR',
      current_quota: { tokens_month: 1000, money_month: null, currency: '' },
    }
    await click('Refresh current quotas')
    await until(() => expect(dialog().textContent).toContain('Platform currency: EUR'))
    expect(button('Submit request').disabled).toBe(true)
    expect(dialog().querySelector('textarea')?.value).toBe('Preserved draft')
    await click('Use this reviewed context')
    expect(
      dialog().querySelector<HTMLInputElement>('[aria-label="Monthly money (EUR)"]')?.value,
    ).toBe('0.000000000000000001')
    failures[`post ${base}/requests`] = 0
    await submit()
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].headers.get('If-Match')).toBe(`"${'c'.repeat(64)}"`)
    expect(JSON.parse(posts()[1].data).quota).toEqual({
      money_month: '0.000000000000000001',
      currency: 'EUR',
    })
    expect(JSON.parse(posts()[1].data).request_id).not.toBe(JSON.parse(posts()[0].data).request_id)
  })

  it('rejects unsafe integers and non-decimal or over-precision amounts without sending', async () => {
    rows = []
    await mount()
    await apply()
    await fill('Reason', 'Validate monthly fields')
    for (const value of ['-1', '1e3', '9007199254740992', '0.1']) {
      await fill('Monthly tokens', value)
      await submit()
      await until(() => expect(dialog().textContent).toContain('Use a non-negative safe integer'))
      expect(posts()).toHaveLength(0)
    }
    await fill('Monthly tokens', '')
    for (const value of ['-1', '1e2', '01', '0.0000000000000000001', '1000000000000000000']) {
      await fill('Monthly money (USD)', value)
      await submit()
      expect(posts()).toHaveLength(0)
    }
  })

  it('keeps the entered monthly draft when switching English and Chinese', async () => {
    rows = []
    await mount()
    await apply()
    await fill('Monthly tokens', '3000')
    await fill('Monthly money (USD)', '12.000000000000000001')
    await fill('Reason', '原样保留')
    await act(async () => i18n.changeLanguage('zh'))
    expect(dialog().textContent).toContain('月度额度')
    expect(dialog().querySelector<HTMLInputElement>('[aria-label="月度金额（USD）"]')?.value).toBe(
      '12.000000000000000001',
    )
    expect(dialog().querySelector('textarea')?.value).toBe('原样保留')
    await act(async () => i18n.changeLanguage('en'))
    expect(button('Submit request').disabled).toBe(false)
  })

  it('does not render a mismatched resource context or allow an application', async () => {
    rows = []
    contextOverride = { ...context, project_id: 'prj_other' }
    await mount()
    await click('Request quota adjustment')
    await until(() => expect(dialog().querySelector('[role="alert"]')).not.toBeNull())
    expect(dialog().querySelector('input')).toBeNull()
    expect(posts()).toHaveLength(0)
  })
})

describe('Scoped Project quota review', () => {
  it('hides the minimal Project metadata after failed reauthorization', async () => {
    project = {
      id: projectId,
      name: 'Quota Project',
      description: 'Scoped resource',
      status: 'active',
      request_workspace_only: true,
    } as ResourceRecord
    await mount(true)
    expect(host.textContent).toContain('Quota Project')
    failures[`get ${base}`] = 403
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['resources', 'projects', false, projectId, actor] })
    })
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(host.textContent).not.toContain('Quota Project')
    expect(host.textContent).not.toContain('Scoped resource')
    expect(host.textContent).not.toContain('Contract workload')
  })

  it('does not reuse the previous actor’s minimal Project detail while the new recipient is loading', async () => {
    project = {
      id: projectId,
      name: 'Old reviewer Project',
      description: 'Old private basic',
      status: 'active',
      request_workspace_only: true,
    } as ResourceRecord
    await mount(true)
    expect(host.textContent).toContain('Old private basic')
    resourceGate = barrier()
    actor = 'usr_new_reviewer'
    project = { ...project, name: 'Fresh Project', description: 'Current authorized basic' }
    await act(async () => cache.setQueryData(sessionKey, auth()))
    await until(() => expect(requests.filter((request) => request.url === base)).toHaveLength(2))
    expect(host.textContent).not.toContain('Old private basic')
    expect(host.textContent).not.toContain('Old reviewer Project')
    await act(async () => resourceGate!.release())
    await until(() => expect(host.textContent).toContain('Current authorized basic'))
    expect(cache.getQueryData(['resources', 'projects', false, projectId, actor])).toBeTruthy()
  })
  it('does not reinterpret a historical money request after the platform denomination changes', async () => {
    context.platform_currency = 'EUR'
    context.current_quota = { tokens_month: 1000, money_month: null, currency: '' }
    await mount()
    await review()
    expect(dialog().textContent).toContain('15.000000000000000001 USD')
    expect(dialog().textContent).toContain('cannot be reinterpreted')
    expect(button('Approve')).toBeUndefined()
    expect(button('Reject')).toBeTruthy()
    expect(posts()).toHaveLength(0)
  })
  it('gives a limits-only reviewer the minimal request workspace without fetching unrelated Project data', async () => {
    project = {
      id: projectId,
      name: 'Quota Project',
      description: 'Scoped resource',
      status: 'active',
      request_workspace_only: true,
    } as ResourceRecord
    rows = [record, model()]
    await mount(true)
    expect(host.querySelectorAll('[role="tab"]')).toHaveLength(1)
    expect(host.textContent).not.toContain('Existing model request')
    expect(button('Request quota adjustment')).toBeUndefined()
    expect(
      requests.some(
        (request) =>
          request.url?.endsWith('/limits') ||
          request.url?.includes('candidates') ||
          request.url === '/models' ||
          request.url === '/projects',
      ),
    ).toBe(false)
    await review()
    await click('Approve')
    expect(button('Confirm Approve').disabled).toBe(true)
    await fill('Decision reason', 'Controlled monthly approval')
    await click('Confirm Approve')
    await until(() => expect(dialog().textContent).toContain('confirmed in the current runtime'))
    expect(dialog().querySelector('form')).toBeNull()
    expect(button('Use this reviewed context')).toBeUndefined()
    expect(posts()[0].headers.get('If-Match')).toBe(`"${'b'.repeat(64)}"`)
    expect(JSON.parse(posts()[0].data)).toEqual({
      action: 'approve',
      reason: 'Controlled monthly approval',
    })
  })

  it('does not expose quota rows to a model-only reviewer or approve controls to an ordinary manager', async () => {
    permissions = ['projects.models.write']
    project.managers = []
    rows = [model(), record]
    await mount()
    expect(host.textContent).toContain('Existing model request')
    expect(host.textContent).not.toContain('Contract workload')
    expect(host.textContent).not.toContain('15.000000000000000001')
    expect(requests.some((request) => request.url === `${base}/requests/${requestId}`)).toBe(false)
    permissions = []
    project.managers = [
      { id: 'pmg_one', user_id: actor, name: 'Manager', email: 'manager@example.invalid' },
    ]
    rows = [record]
    await act(async () => {
      cache.setQueryData(['permissions', actor], [])
      root.render(
        <QueryClientProvider client={cache}>
          <MemoryRouter>
            <ProjectRequestsPanel project={project} />
          </MemoryRouter>
        </QueryClientProvider>,
      )
    })
    await until(() => expect(host.textContent).toContain('Contract workload'))
    await review()
    expect(button('Approve')).toBeUndefined()
    expect(button('Reject')).toBeUndefined()
  })

  it('retains the original pending approval retry in the minimal Project workspace without refetching unrelated basics', async () => {
    project = {
      id: projectId,
      name: 'Quota Project',
      description: 'Scoped resource',
      status: 'active',
      request_workspace_only: true,
    } as ResourceRecord
    application = 'pending'
    await mount(true)
    await review()
    await click('Approve')
    await fill('Decision reason', 'Preserve the reviewed approval')
    await submit()
    await until(() =>
      expect(dialog()?.textContent).toContain(
        'Application of that exact policy revision is not confirmed.',
      ),
    )
    expect(button('Retry the same action')).toBeTruthy()
    expect(requests.filter((request) => request.url === base)).toHaveLength(1)
    const original = posts()[0]
    expect(JSON.parse(original.data)).toEqual({
      action: 'approve',
      reason: 'Preserve the reviewed approval',
    })
    expect(original.headers.get('If-Match')).toBe(`"${approvalTag}"`)
    application = 'applied'
    await click('Retry the same action')
    await until(() => expect(dialog()?.textContent).toContain('confirmed in the current runtime'))
    expect(posts()).toHaveLength(2)
    expect(posts()[1].data).toBe(original.data)
    expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(requests.filter((request) => request.url === base)).toHaveLength(1)
    expect(
      requests.some(
        (request) =>
          request.url?.endsWith('/limits') ||
          request.url?.includes('candidates') ||
          request.url === '/models' ||
          request.url === '/projects',
      ),
    ).toBe(false)
  })

  it('hides cached detail during reauthorization and after manager access is revoked', async () => {
    permissions = []
    cache.setQueryData(['project-quota-request-detail', actor, projectId, requestId], {
      ...freshDetail(),
      reason: 'STALE PRIVATE DETAIL',
    })
    detailGate = barrier()
    await mount()
    await click('Review request')
    await until(() => expect(dialog().textContent).toContain('Loading'))
    expect(dialog().textContent).not.toContain('STALE PRIVATE DETAIL')
    expect(dialog().textContent).not.toContain('Quota baseline at submission')
    failures[`get ${base}/requests/${requestId}`] = 403
    await act(async () => detailGate!.release())
    await until(() => expect(dialog().querySelector('[role="alert"]')).not.toBeNull())
    expect(dialog().textContent).not.toContain('12.000000000000000001')
    expect(button('Approve')).toBeUndefined()
    expect(dialog().querySelector('form')).toBeNull()
    expect(posts()).toHaveLength(0)
  })

  it('preserves approval reason and requires fresh explicit review after a competing policy edit', async () => {
    await mount()
    await review()
    await click('Approve')
    await fill('Decision reason', 'Reviewed contract')
    failures[`post ${base}/requests/${requestId}/decision`] = 409
    await submit()
    await until(() =>
      expect(dialog().textContent).toContain('The request or Project state changed.'),
    )
    expect(button('Use this reviewed context').disabled).toBe(true)
    approvalTag = 'd'.repeat(64)
    context.current_quota.tokens_month = 1500
    await click('Refresh request details')
    await until(() => expect(button('Use this reviewed context').disabled).toBe(false))
    expect(button('Confirm Approve').disabled).toBe(true)
    expect(dialog().querySelector('textarea')?.value).toBe('Reviewed contract')
    await click('Use this reviewed context')
    failures[`post ${base}/requests/${requestId}/decision`] = 0
    await submit()
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].headers.get('If-Match')).toBe(`"${'d'.repeat(64)}"`)
    expect(posts()[1].data).toBe(posts()[0].data)
  })

  it('never changes a reviewed uncertain approval when later detail is superseded', async () => {
    await mount()
    await review()
    await click('Approve')
    await fill('Decision reason', 'Original decision')
    failures[`post ${base}/requests/${requestId}/decision`] = 503
    await submit()
    await until(() => expect(dialog().textContent).toContain('The decision may already be saved.'))
    const original = posts()[0]
    record.status = 'approved'
    record.decided_at = '2026-10-02T01:00:00Z'
    record.decision_actor_id = actor
    record.decision_reason = 'Original decision'
    record.approved_quota = {
      tokens_month: 2000,
      money_month: '15.000000000000000001',
      currency: 'USD',
    }
    record.approved_policy_etag = 'policy-approved'
    application = 'superseded'
    context.current_quota.tokens_month = 9000
    approvalTag = 'e'.repeat(64)
    await click('Refresh request details')
    await until(() => expect(dialog().textContent).toContain('a newer policy has replaced'))
    expect(button('Use this reviewed context')).toBeUndefined()
    expect(dialog().querySelector('textarea')?.readOnly).toBe(true)
    failures[`post ${base}/requests/${requestId}/decision`] = 0
    await click('Retry the same action')
    await until(() => expect(dialog().textContent).toContain('Decision saved.'))
    expect(dialog().querySelector('form')).toBeNull()
    expect(button('Use this reviewed context')).toBeUndefined()
    expect(posts()[1].data).toBe(original.data)
    expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(dialog().textContent).not.toContain('confirmed in the current runtime')
    expect(context.current_quota.tokens_month).toBe(9000)
  })

  it('separates saved approval from pending application and allows only the original saved decision retry', async () => {
    application = 'pending'
    await mount()
    await review()
    await click('Approve')
    await fill('Decision reason', 'Saved revision')
    await submit()
    await until(() =>
      expect(dialog().textContent).toContain(
        'Application of that exact policy revision is not confirmed.',
      ),
    )
    expect(dialog().textContent).not.toContain('confirmed in the current runtime')
    application = 'applied'
    await click('Retry the same action')
    await until(() => expect(dialog().textContent).toContain('confirmed in the current runtime'))
    expect(posts()[1].data).toBe(posts()[0].data)
    expect(posts()[1].headers.get('If-Match')).toBe(posts()[0].headers.get('If-Match'))
  })

  it('blocks self approval but allows an applicant to withdraw on an inactive Project', async () => {
    record.applicant_user_id = actor
    project.status = 'disabled'
    await mount()
    await review()
    expect(button('Approve')).toBeUndefined()
    expect(button('Reject')).toBeUndefined()
    expect(dialog().textContent).toContain('You submitted this request.')
    await click('Withdraw')
    await click('Confirm Withdraw')
    await until(() => expect(dialog().textContent).toContain('Decision saved.'))
    expect(posts()[0].headers.get('If-Match')).toBeUndefined()
    expect(JSON.parse(posts()[0].data)).toEqual({ action: 'withdraw', reason: '' })
  })

  it('requires a rejection reason and never sends a policy validator for rejection', async () => {
    await mount()
    await review()
    await click('Reject')
    expect(button('Confirm Reject').disabled).toBe(true)
    await fill('Decision reason', 'Use existing quota')
    await submit()
    await until(() => expect(dialog().textContent).toContain('Use existing quota'))
    expect(posts()[0].headers.get('If-Match')).toBeUndefined()
    expect(JSON.parse(posts()[0].data)).toEqual({ action: 'reject', reason: 'Use existing quota' })
  })

  it('uses live bilingual snapshot and application copy without rounding historical money', async () => {
    record.status = 'approved'
    record.decided_at = '2026-10-02T01:00:00Z'
    record.decision_actor_id = 'usr_reviewer'
    record.approved_quota = {
      tokens_month: 2000,
      money_month: '15.000000000000000001',
      currency: 'USD',
    }
    record.approved_policy_etag = 'policy-approved'
    application = 'superseded'
    await mount()
    await review()
    expect(dialog().textContent).toContain('15.000000000000000001 USD')
    await act(async () => i18n.changeLanguage('zh'))
    expect(dialog().textContent).toContain('提交时的额度基线')
    expect(dialog().textContent).toContain('已被更新策略取代')
    expect(dialog().textContent).toContain('15.000000000000000001 USD')
    expect(dialog().querySelector('form')).toBeNull()
  })

  it('hides history while reauthorizing and after an authorized list request is rejected', async () => {
    await mount()
    expect(host.textContent).toContain('Contract workload')
    failures[`get ${base}/requests`] = 403
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['project-requests', actor, projectId] })
    })
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(host.textContent).not.toContain('Contract workload')
    expect(host.textContent).not.toContain('15.000000000000000001')
    expect(button('Review request')).toBeUndefined()
  })
})

describe('Quota request read boundaries', () => {
  it('rejects weak or malformed creation context instead of inventing a review validator', async () => {
    for (const override of [
      { ...context, review_etag: 'weak' },
      { ...context, current_quota: null },
      { ...context, current_quota: { ...context.current_quota, tokens_month: '1000' } },
    ]) {
      contextOverride = override
      await expect(projectQuotaContext(projectId)).rejects.toThrow('Invalid Project quota context')
    }
  })
  it('rejects mismatched detail, malformed targets and unknown application claims', async () => {
    for (const override of [
      { ...freshDetail(), id: 'req_other' },
      { ...freshDetail(), requested_quota: { money_month: 0, currency: 'USD' } },
      { ...freshDetail(), approval_review_etag: 'W/invalid' },
      {
        ...freshDetail(),
        status: 'approved',
        approved_quota: context.current_quota,
        runtime_applied: true,
        application_status: 'future',
      },
    ]) {
      detailOverride = override
      await expect(projectQuotaRequestDetail(projectId, requestId)).rejects.toThrow(
        'Invalid Project quota request detail',
      )
    }
  })
})
