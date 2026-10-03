import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, MemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type {
  PersonalModelCandidate,
  PersonalModelRequestDetail,
} from '@/types/personal-model-requests'
import PersonalAccessRequest from './request'
import RequestPanel from './requests'
import Workspace from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const oldAdapter = client.defaults.adapter
const time = '2026-10-04T00:00:00Z'
const etag = 'a'.repeat(64)
const uuid = '11111111-1111-4111-8111-111111111111'
let root: Root, host: HTMLDivElement, cache: QueryClient
let auth: Session,
  permissions: string[],
  candidate: PersonalModelCandidate,
  detail: PersonalModelRequestDetail
let requests: InternalAxiosRequestConfig[], failures: Record<string, number>
let malformed: boolean, decisionApplication: 'pending' | 'applied' | 'superseded'
let barrier: { promise: Promise<void>; release: () => void } | undefined
const busy = vi.fn()
function hold() {
  let release!: () => void
  barrier = {
    promise: new Promise<void>((resolve) => {
      release = resolve
    }),
    release: () => release(),
  }
  return barrier
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  auth = {
    user: { id: 'usr_owner', name: 'Owner', email: 'private@example.invalid', role: 'member' },
    csrf_token: 'csrf',
  }
  permissions = ['members.models.write']
  requests = []
  failures = {}
  malformed = false
  decisionApplication = 'pending'
  barrier = undefined
  busy.mockReset()
  candidate = {
    id: 'mdl_model',
    name: 'Model',
    status: 'active',
    created_at: time,
    protocols: ['openai_chat'],
    input_capabilities: {},
    personal_granted: false,
    pending_request_id: null,
    review_etag: etag,
  }
  detail = {
    id: 'mar_request',
    request_id: uuid,
    applicant_user_id: 'usr_owner',
    applicant_name: 'Recorded owner',
    model_id: 'mdl_model',
    model_name: 'Recorded model',
    reason: 'Original need',
    status: 'pending',
    created_at: time,
    updated_at: time,
    resolved_at: null,
    cancelled_reason: null,
    decision: null,
    current_model: { id: 'mdl_model', name: 'Model', status: 'active' },
    current_granted: false,
    review_etag: etag,
    allowed_actions: ['approve', 'reject', 'withdraw'],
    runtime_applied: false,
    application_status: 'pending',
  }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const url = config.url!
    if (
      config.method === 'get' &&
      barrier &&
      url !== '/auth/session' &&
      url !== '/auth/permissions'
    )
      await barrier.promise
    const response = {
      config,
      status: failures[`${config.method} ${url}`] ?? 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (response.status >= 400) throw new AxiosError('Denied', '', config, undefined, response)
    if (url === '/auth/session') response.data = structuredClone(auth)
    else if (url === '/auth/permissions') response.data = { permissions }
    else if (url.startsWith('/model-access-candidates/')) response.data = structuredClone(candidate)
    else if (url.endsWith('/model-access-workspace'))
      response.data = {
        user_id: url.split('/')[3],
        models: [{ id: 'mdl_granted', name: `Grant ${url.split('/')[3]}`, status: 'active' }],
        model_count: 1,
        can_review_requests: url.split('/')[3] !== auth.user.id,
      }
    else if (
      config.method === 'get' &&
      (url.endsWith('/model-requests') || url === '/personal-model-requests')
    )
      response.data = { items: [structuredClone(detail)], total: 1, next_cursor: null }
    else if (config.method === 'get' && url.endsWith('/mar_request'))
      response.data = structuredClone(detail)
    else if (config.method === 'post' && url === '/personal-model-requests') {
      const body = JSON.parse(config.data)
      response.data = malformed
        ? {}
        : { ...structuredClone(detail), request_id: body.request_id, reason: body.reason }
    } else if (config.method === 'post' && url.endsWith('/decision')) {
      const body = JSON.parse(config.data)
      const status =
        body.action === 'approve' ? 'approved' : body.action === 'reject' ? 'rejected' : 'withdrawn'
      detail = {
        ...detail,
        status,
        resolved_at: time,
        allowed_actions: [],
        current_granted: body.action === 'approve' && decisionApplication !== 'superseded',
        runtime_applied: body.action === 'approve' && decisionApplication === 'applied',
        application_status: body.action === 'approve' ? decisionApplication : 'pending',
        decision: { ...body, actor_id: auth.user.id, actor_name: auth.user.name, decided_at: time },
      }
      response.data = malformed
        ? {}
        : {
            decision_id: body.decision_id,
            committed: true,
            saved_request: detail,
            current_granted: detail.current_granted,
            runtime_applied: detail.runtime_applied,
            application_status: detail.application_status,
          }
    } else throw new Error(`Unexpected request ${config.method} ${url}`)
    return response
  }
})
afterEach(async () => {
  barrier?.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = oldAdapter
  await i18n.changeLanguage('en')
})
async function until(assert: () => void) {
  for (let attempt = 0; attempt < 100; attempt++) {
    await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
    try {
      assert()
      return
    } catch (error) {
      if (attempt === 99) throw error
    }
  }
}
async function mount(component: ReactNode) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter>{component}</MemoryRouter>
      </QueryClientProvider>,
    ),
  )
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')]
    .filter((item) => item.textContent === label)
    .at(-1)!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function type(value: string) {
  const input = document.querySelector('textarea')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
const posts = () => requests.filter((request) => request.method === 'post')
async function mountRequest(visible = true) {
  await mount(
    <PersonalAccessRequest
      actorID="usr_owner"
      modelID="mdl_model"
      onBusy={busy}
      visible={visible}
    />,
  )
  if (visible) await until(() => expect(document.querySelector('textarea')).not.toBeNull())
}
async function mountReview(owner = 'usr_owner') {
  auth.user.id = 'usr_reviewer'
  await mount(<RequestPanel owner={owner} />)
  await until(() => expect(button('Request details')).toBeDefined())
  await click('Request details')
  await until(() =>
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Original need'),
  )
}

describe('Personal model access interactions', () => {
  it('starts in English and switches draft guidance live without changing the draft or dispatching', async () => {
    await mountRequest()
    await type('研究用途')
    expect(button('Submit request')).toBeDefined()
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.querySelector('textarea')?.value).toBe('研究用途')
    expect(button('提交申请')).toBeDefined()
    expect(host.textContent).toContain('最多 1,024 个 UTF-8 字节')
    expect(posts()).toHaveLength(0)
  })
  it('submits a trimmed reason only after validation and does not claim a grant or runtime application', async () => {
    await mountRequest()
    await type('中'.repeat(342))
    await click('Submit request')
    expect(posts()).toHaveLength(0)
    expect(host.textContent).toContain('Provide a valid reason')
    await type('  New need  ')
    await click('Submit request')
    await until(() =>
      expect(host.textContent).toContain('Pending requests do not change model access'),
    )
    expect(JSON.parse(posts()[0].data).reason).toBe('New need')
    expect(posts()[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(host.textContent).not.toContain('currently applied')
  })
  it('preserves an unknown creation UUID/body/ETag across rejected retry and parent detail failure', async () => {
    failures['post /personal-model-requests'] = 503
    await mountRequest()
    await type('Preserve exact intent')
    await click('Submit request')
    await until(() => expect(button('Retry original request')).toBeDefined())
    const first = posts()[0]
    expect(busy).toHaveBeenLastCalledWith(true)
    expect(button('Review current state')).toBeUndefined()
    await mountRequest(false)
    expect(button('Retry original request').disabled).toBe(true)
    failures['post /personal-model-requests'] = 403
    await mountRequest()
    await click('Retry original request')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].data).toBe(first.data)
    expect(posts()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
    expect(host.textContent).toContain('does not resolve this uncertainty')
    expect(button('Review current state')).toBeUndefined()
  })
  it('requires explicit stale candidate review and preserves reason after a definitive conflict', async () => {
    await mountRequest()
    await type('Draft')
    failures['post /personal-model-requests'] = 409
    await click('Submit request')
    await until(() => expect(button('Review current state')).toBeDefined())
    candidate.review_etag = 'b'.repeat(64)
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['personal-model-candidate'] })
    })
    await until(() =>
      expect(
        cache.getQueryData<PersonalModelCandidate>([
          'personal-model-candidate',
          'usr_owner',
          'mdl_model',
        ])?.review_etag,
      ).toBe(candidate.review_etag),
    )
    await until(() => expect(button('Review current state')?.disabled).toBe(false))
    expect(document.querySelector('textarea')?.value).toBe('Draft')
    expect(posts()).toHaveLength(1)
    await click('Review current state')
    delete failures['post /personal-model-requests']
    await click('Submit request')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].headers.get('If-Match')).toBe(`"${candidate.review_etag}"`)
    expect(JSON.parse(posts()[1].data).request_id).not.toBe(JSON.parse(posts()[0].data).request_id)
  })
  it('treats a malformed successful creation response as unknown and never automatically replays', async () => {
    malformed = true
    await mountRequest()
    await type('Need')
    await click('Submit request')
    await until(() => expect(host.textContent).toContain('may already be saved'))
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['personal-model-candidate'] })
    })
    expect(posts()).toHaveLength(1)
    expect(button('Retry original request')).toBeDefined()
  })
  it('loads the isolated minimal review workspace without member directory, email, roles, Keys or limits', async () => {
    auth.user.id = 'usr_reviewer'
    const router = createMemoryRouter(
      [{ path: '/admin/members/:memberId/models', element: <Workspace /> }],
      { initialEntries: ['/admin/members/usr_owner/models'] },
    )
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(host.textContent).toContain('Grant usr_owner'))
    expect(host.textContent).not.toContain('private@example.invalid')
    expect(requests.map((request) => request.url)).toEqual(
      expect.arrayContaining([
        '/admin/members/usr_owner/model-access-workspace',
        '/admin/members/usr_owner/model-requests',
      ]),
    )
    expect(
      requests.every(
        (request) =>
          ['/auth/session', '/auth/permissions'].includes(request.url!) ||
          /\/(model-access-workspace|model-requests)$/.test(request.url!),
      ),
    ).toBe(true)
    const pending = hold()
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['personal-model-workspace'] })
    })
    await until(() => expect(host.textContent).not.toContain('Grant usr_owner'))
    expect(host.textContent).not.toContain('Original need')
    failures['get /admin/members/usr_owner/model-access-workspace'] = 403
    await act(async () => pending.release())
    barrier = undefined
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(host.textContent).not.toContain('Grant usr_owner')
    router.dispose()
  })
  it('hides review authority immediately on permission renewal/revocation', async () => {
    await mountReview()
    await click('Approve')
    permissions = []
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['permissions'] })
    })
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(host.textContent).not.toContain('Recorded model')
    expect(posts()).toHaveLength(0)
  })
  it('never offers self approval or rejection while retaining own withdrawal history', async () => {
    await mount(<RequestPanel owner="usr_owner" />)
    await until(() => expect(button('Request details')).toBeDefined())
    await click('Request details')
    await until(() => expect(document.body.textContent).toContain('Another authorized member'))
    expect(button('Approve')).toBeUndefined()
    expect(button('Reject')).toBeUndefined()
    expect(button('Withdraw')).toBeUndefined()
    await act(async () => root.unmount())
    root = createRoot(host)
    await mount(<RequestPanel />)
    await until(() => expect(button('Request details')).toBeDefined())
    await click('Request details')
    await until(() => expect(button('Withdraw')).toBeDefined())
    await click('Withdraw')
    await click('Withdraw')
    await until(() => expect(posts()).toHaveLength(1))
    expect(JSON.parse(posts()[0].data).reason).toBe('')
    expect(document.body.textContent).not.toContain('Runtime application is pending')
  })
  it('confirms exact approval and distinguishes the durable historical receipt from pending runtime application', async () => {
    await mountReview()
    await click('Approve')
    expect(posts()).toHaveLength(0)
    expect(document.body.textContent).toContain('Confirm Approve')
    await click('Approve')
    await until(() => expect(document.body.textContent).toContain('The decision was committed'))
    expect(document.body.textContent).toContain('Runtime application is pending')
    expect(document.body.textContent).not.toContain('currently applied')
    expect(JSON.parse(posts()[0].data)).toMatchObject({ action: 'approve', reason: '' })
  })
  it('preserves the committed receipt while current application changes from applied to superseded', async () => {
    decisionApplication = 'applied'
    await mountReview()
    await click('Approve')
    await click('Approve')
    await until(() =>
      expect(document.body.textContent).toContain(
        'The recorded Personal grant is currently applied',
      ),
    )
    const pending = hold()
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['personal-model-request'] })
    })
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain(
        'currently applied',
      ),
    )
    expect(document.body.textContent).toContain('The decision was committed')
    detail = {
      ...detail,
      current_granted: false,
      runtime_applied: false,
      application_status: 'superseded',
    }
    await act(async () => pending.release())
    barrier = undefined
    await until(() =>
      expect(document.body.textContent).toContain('The recorded approval was superseded'),
    )
    expect(document.body.textContent).toContain('Current Personal grant: Not granted')
    expect(document.body.textContent).toContain('The decision was committed')
    expect(document.body.textContent).not.toContain('currently applied')
    expect(posts()).toHaveLength(1)
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('审批决定已提交')
    expect(document.body.textContent).toContain('已记录的批准结果已被后续变更取代')
    expect(document.body.textContent).not.toContain('个人授权当前已生效')
  })

  it('preserves an unknown decision through refreshed terminal state and rejected original retry', async () => {
    await mountReview()
    await click('Approve')
    malformed = true
    await click('Approve')
    await until(() => expect(button('Retry original request')).toBeDefined())
    const first = posts()[0]
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['personal-model-request'] })
    })
    await until(() => expect(document.body.textContent).toContain('Approved'))
    failures['post /admin/members/usr_owner/model-requests/mar_request/decision'] = 409
    await click('Retry original request')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].data).toBe(first.data)
    expect(posts()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
    expect(document.body.textContent).toContain('does not resolve this uncertainty')
    expect(button('Review current state')).toBeUndefined()
  })
  it('hides cached decision details during refresh and requires explicit ETag review before dispatch', async () => {
    await mountReview()
    await click('Reject')
    await type('Reviewed reason')
    const pending = hold()
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['personal-model-request'] })
    })
    await until(() => expect(document.querySelector('[role="dialog"] dd')).toBeNull())
    detail.review_etag = 'b'.repeat(64)
    await act(async () => pending.release())
    barrier = undefined
    await until(() => expect(document.body.textContent).toContain('Current state changed'))
    expect(document.querySelector('textarea')?.value).toBe('Reviewed reason')
    expect(button('Reject').disabled).toBe(true)
    await click('Review current state')
    await click('Reject')
    await until(() => expect(posts()).toHaveLength(1))
    expect(posts()[0].headers.get('If-Match')).toBe(`"${detail.review_etag}"`)
  })
  it('retains an unknown decision after a detail 403 and restores only an explicitly authorized original retry', async () => {
    await mountReview()
    await click('Approve')
    failures['post /admin/members/usr_owner/model-requests/mar_request/decision'] = 503
    await click('Approve')
    await until(() => expect(button('Retry original request')).toBeDefined())
    const first = posts()[0]
    failures['get /admin/members/usr_owner/model-requests/mar_request'] = 403
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['personal-model-request'] })
    })
    await until(() => expect(button('Retry original request').disabled).toBe(true))
    expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('Original need')
    expect(posts()).toHaveLength(1)
    delete failures['get /admin/members/usr_owner/model-requests/mar_request']
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['personal-model-request'] })
    })
    await until(() => expect(button('Retry original request').disabled).toBe(false))
    expect(posts()).toHaveLength(1)
    await click('Retry original request')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].data).toBe(first.data)
    expect(posts()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
  })

  it('reauthorizes a new workspace target without restoring obsolete target responses', async () => {
    auth.user.id = 'usr_reviewer'
    const router = createMemoryRouter(
      [{ path: '/admin/members/:memberId/models', element: <Workspace /> }],
      { initialEntries: ['/admin/members/usr_owner/models'] },
    )
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(host.textContent).toContain('Grant usr_owner'))
    const pending = hold()
    await act(async () => {
      void router.navigate('/admin/members/usr_other/models')
    })
    await until(() => expect(host.textContent).not.toContain('Grant usr_owner'))
    detail.applicant_user_id = 'usr_other'
    await act(async () => pending.release())
    barrier = undefined
    await until(() => expect(host.textContent).toContain('Grant usr_other'))
    expect(host.textContent).not.toContain('Grant usr_owner')
    expect(
      requests.some((request) => request.url === '/admin/members/usr_other/model-requests'),
    ).toBe(true)
    router.dispose()
  })

  it('keeps applicant details isolated across account change', async () => {
    await mountReview()
    auth = { ...auth, user: { ...auth.user, id: 'usr_new' } }
    failures['get /admin/members/usr_owner/model-requests'] = 403
    await act(async () => cache.setQueryData(sessionKey, auth))
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(host.textContent).not.toContain('Original need')
    expect(posts()).toHaveLength(0)
  })
})
