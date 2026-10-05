import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it } from 'vitest'
import i18n from '@/i18n'
import client from '@/api/client'
import type { MemberApprovalReview } from '@/types/registration-approval'
import MemberApproval from './member-approval'
import { memberApprovalReview } from './member-approval.fixture'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const old = client.defaults.adapter,
  contextKey = ['admin', 'member', 'usr_admin', 'usr_target', 1]
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  record: MemberApprovalReview,
  requests: InternalAxiosRequestConfig[],
  closed: boolean,
  status: number,
  commit: boolean,
  readPause: Promise<void> | undefined,
  contextPause: Promise<void> | undefined,
  writePause: Promise<void> | undefined,
  opened: boolean
const session = (csrf = 'initial') => ({
  user: { id: 'usr_admin', email: 'admin@example.invalid', name: 'Actor', role: 'admin' },
  csrf_token: csrf,
})
function fail(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('Controlled error', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: {},
  })
}
const tick = () => new Promise((resolve) => setTimeout(resolve, 0))
async function flush() {
  for (let n = 0; n < 8; n++)
    await act(async () => {
      await tick()
    })
}
function Harness() {
  const context = useQuery({
    queryKey: contextKey,
    queryFn: async () => {
      await contextPause
      return { id: 'usr_target' }
    },
    staleTime: Infinity,
  })
  return (
    <MemberApproval
      actor="usr_admin"
      target="usr_target"
      generation={1}
      ready={context.isSuccess && !context.isFetching}
      open={opened}
      contextKind="detail"
      contextQueryKey={contextKey}
      returnFocus={() => false}
      onClose={() => {
        closed = true
      }}
    />
  )
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Harness />
      </QueryClientProvider>,
    ),
  )
  await flush()
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (node) => node.textContent === label,
  )!
}
async function click(label: string) {
  await act(async () => button(label).click())
  await flush()
}
async function reason(value = 'Reviewed current identity') {
  const input = document.querySelector<HTMLTextAreaElement>('textarea')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await flush()
}
const writes = () => requests.filter((r) => r.method === 'patch')
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  record = memberApprovalReview()
  requests = []
  closed = false
  status = 0
  commit = false
  readPause = undefined
  contextPause = undefined
  writePause = undefined
  opened = true
  cache.setQueryData(['auth', 'session'], session())
  cache.setQueryData(['permissions', 'usr_admin'], ['members.read', 'members.approvals.write'])
  cache.setQueryData(contextKey, { id: 'usr_target' })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown
    if (config.method === 'get') {
      await readPause
      data = structuredClone(record)
    } else {
      const failure = status
      await writePause
      const body = JSON.parse(config.data)
      if (!failure || commit) {
        record = {
          ...record,
          approval_status: body.decision === 'approve' ? 'approved' : 'rejected',
          can_approve: false,
          can_reject: false,
          review_etag: 'b'.repeat(64),
          application: {
            ...record.application!,
            state: body.decision === 'approve' ? 'approved' : 'rejected',
            decided_at: '2026-10-05T02:00:00Z',
            decision_actor_id: 'usr_admin',
            decision_reason: body.reason,
          },
          admission_eligible:
            body.decision === 'approve' && !record.disabled && !record.offboarded_at,
        }
      }
      if (failure) throw fail(config, failure)
      data = {
        confirmation: 'current_account_approval',
        user_id: record.user_id,
        application_id: record.application!.id,
        decision: record.approval_status,
        admission_eligible: record.admission_eligible,
        runtime_applied: true,
      }
    }
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ ETag: `"${record.review_etag}"` }),
      data,
    }
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = old
})
it('loads only the exact private review with all three fresh resources and no Session observer', async () => {
  await mount()
  expect(requests.map((r) => r.url)).toEqual(['/admin/members/usr_target/approval'])
  expect(document.body.textContent).toContain('Awaiting approval')
  expect(
    cache
      .getQueryCache()
      .find({ queryKey: ['auth', 'session'] })
      ?.getObserversCount(),
  ).toBe(0)
  expect(writes()).toHaveLength(0)
})
it.each(['Session invalid', 'permission absent', 'target invalid', 'target wrong'])(
  'hides private review for %s',
  async (kind) => {
    if (kind === 'Session invalid') await cache.invalidateQueries({ queryKey: ['auth', 'session'] })
    if (kind === 'permission absent')
      cache.setQueryData(['permissions', 'usr_admin'], ['members.read'])
    if (kind === 'target invalid') {
      contextPause = new Promise<void>(() => {})
      await cache.invalidateQueries({ queryKey: contextKey })
    }
    if (kind === 'target wrong') cache.setQueryData(contextKey, { id: 'USR_target' })
    await mount()
    expect(requests).toHaveLength(0)
    expect(document.body.textContent).not.toContain('Target')
    expect(document.querySelector('textarea')).toBeNull()
  },
)
it('requires a reason and explicit current composite review before the first decision', async () => {
  await mount()
  await click('Approve application')
  await click('Confirm')
  expect(writes()).toHaveLength(0)
  expect(document.body.textContent).toContain('Enter a non-empty reason')
  await reason()
  await click('Confirm')
  expect(writes()).toHaveLength(1)
  expect(JSON.parse(writes()[0].data)).toEqual({
    decision: 'approve',
    reason: 'Reviewed current identity',
  })
  expect(writes()[0].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it('shows approval and published denial independently for a disabled administrator', async () => {
  record = memberApprovalReview({ disabled: true, identity_role: 'admin' })
  await mount()
  await reason()
  await click('Approve application')
  expect(document.body.textContent).toContain('disabled or offboarded')
  await click('Confirm')
  expect(record.disabled).toBe(true)
  expect(record.identity_role).toBe('admin')
  expect(document.body.textContent).toContain('Approved')
  expect(document.body.textContent).toContain('published admission denial')
  expect(document.body.textContent).not.toContain('ready to use')
})
it('cannot approve offboarded pending applications but preserves the server reject action', async () => {
  record = memberApprovalReview({ offboarded_at: '2026-10-05T01:00:00Z', can_approve: false })
  await mount()
  expect(button('Approve application').disabled).toBe(true)
  await reason()
  await click('Reject application')
  await click('Confirm')
  expect(writes()).toHaveLength(1)
  expect(JSON.parse(writes()[0].data).decision).toBe('reject')
})
it('retains immutable uncertain intent through current terminal GET, rejected retry and fresh CSRF', async () => {
  status = 503
  commit = true
  await mount()
  await reason('Exact original reason')
  await click('Approve application')
  await click('Confirm')
  expect(writes()).toHaveLength(1)
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'member-approval'] })
  })
  await flush()
  expect(document.body.textContent).toContain('unconfirmed')
  expect(document.body.textContent).not.toContain(
    'current retained decision and published admission eligibility are confirmed',
  )
  status = 409
  await click('Retry exact submitted request')
  expect(writes()).toHaveLength(2)
  await act(async () => cache.setQueryData(['auth', 'session'], session('fresh-csrf')))
  await flush()
  status = 0
  await click('Retry exact submitted request')
  expect(writes()).toHaveLength(3)
  for (const request of writes()) {
    expect(JSON.parse(request.data)).toEqual({
      decision: 'approve',
      reason: 'Exact original reason',
    })
    expect(request.headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
  }
  expect(writes().at(-1)!.headers.get('X-CSRF-Token')).toBe('fresh-csrf')
  expect(document.body.textContent).toContain(
    'current retained decision and published admission eligibility are confirmed',
  )
})
it.each([409, 412])(
  'retains the first dispatched %s as unknown until explicit abandonment',
  async (failure) => {
    status = failure
    await mount()
    await reason('First conflict is still uncertain')
    await click('Approve application')
    await click('Confirm')
    const original = writes()[0]
    expect(button('Retry exact submitted request')).toBeTruthy()
    expect(button('Approve application')).toBeUndefined()
    expect(document.querySelector('textarea')).toBeNull()
    await act(async () => cache.invalidateQueries({ queryKey: ['admin', 'member-approval'] }))
    await flush()
    expect(writes()).toHaveLength(1)
    status = 503
    await click('Retry exact submitted request')
    status = 409
    await click('Retry exact submitted request')
    expect(writes()).toHaveLength(3)
    for (const write of writes()) {
      expect(write.data).toBe(original.data)
      expect(write.headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    }
    await click('Abandon submitted request')
    expect(document.body.textContent).toContain('previous outcome remains unknown')
    expect(document.body.textContent).not.toContain(
      'current retained decision and published admission eligibility are confirmed',
    )
    expect(document.querySelector('textarea')!.value).toBe('First conflict is still uncertain')
    await click('Approve application')
    expect(button('Confirm').disabled).toBe(true)
    await click('Review current state')
    status = 0
    await click('Approve application')
    await click('Confirm')
    expect(writes()).toHaveLength(4)
  },
)
it('retains failed initial intent through dismissal and a real same-owner Session read, with live localized abandonment', async () => {
  status = 409
  await mount()
  await reason('Dismissed immutable approval')
  await click('Approve application')
  await click('Confirm')
  const original = writes()[0]
  await click('Cancel')
  expect(closed).toBe(true)
  opened = false
  await mount()
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  opened = true
  await mount()
  expect(button('Retry exact submitted request')).toBeTruthy()
  await act(async () =>
    cache.fetchQuery({
      queryKey: ['auth', 'session'],
      queryFn: async () => session('renewed'),
      staleTime: 0,
    }),
  )
  await flush()
  expect(writes()).toHaveLength(1)
  await act(async () => i18n.changeLanguage('zh'))
  expect(button('放弃已提交请求')).toBeTruthy()
  expect(document.body.textContent).toContain('原请求的结果仍未知')
  status = 0
  await click('重试原始提交请求')
  expect(writes()).toHaveLength(2)
  expect(writes()[1].data).toBe(original.data)
  expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  expect(writes()[1].headers.get('X-CSRF-Token')).toBe('renewed')
})
it('does not expose a late read after same actor permissions disappear', async () => {
  let release!: () => void
  readPause = new Promise<void>((r) => (release = r))
  await mount()
  await act(async () => cache.setQueryData(['permissions', 'usr_admin'], ['members.read']))
  release()
  await flush()
  expect(document.body.textContent).not.toContain('Target')
  expect(document.querySelector('textarea')).toBeNull()
  expect(writes()).toHaveLength(0)
})
it('switches visible guidance without clearing reason', async () => {
  await mount()
  await reason('Bilingual draft')
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.body.textContent).toContain('待审批')
  expect(document.querySelector('textarea')!.value).toBe('Bilingual draft')
  expect(writes()).toHaveLength(0)
})
it('closes through the local dialog without a decision', async () => {
  await mount()
  await click('Cancel')
  expect(closed).toBe(true)
  expect(writes()).toHaveLength(0)
})

it('cannot abandon a pending dispatch and discards its late success after authority renewal', async () => {
  let release!: () => void
  writePause = new Promise<void>((resolve) => {
    release = resolve
  })
  await mount()
  await reason('Late approval result')
  await click('Approve application')
  await click('Confirm')
  const original = writes()[0]
  expect(button('Abandon submitted request').disabled).toBe(true)
  await click('Abandon submitted request')
  expect(writes()).toHaveLength(1)
  await act(async () =>
    cache.invalidateQueries({ queryKey: ['auth', 'session'], refetchType: 'none' }),
  )
  await flush()
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  release()
  await flush()
  await act(async () =>
    cache.fetchQuery({
      queryKey: ['auth', 'session'],
      queryFn: async () => session('after-late'),
      staleTime: 0,
    }),
  )
  await flush()
  expect(button('Retry exact submitted request')).toBeTruthy()
  expect(document.body.textContent).not.toContain(
    'current retained decision and published admission eligibility are confirmed',
  )
  expect(writes()).toHaveLength(1)
  await click('Retry exact submitted request')
  expect(writes()).toHaveLength(2)
  expect(writes()[1].data).toBe(original.data)
  expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
})

it.each([409, 412])(
  'does not resolve simulated postcommit %s from a matching terminal GET or a rejected retry',
  async (failure) => {
    status = failure
    commit = true
    await mount()
    await reason('Original committed decision')
    await click('Approve application')
    await click('Confirm')
    const original = writes()[0]
    expect(record.approval_status).toBe('approved')
    await act(async () => cache.invalidateQueries({ queryKey: ['admin', 'member-approval'] }))
    await flush()
    expect(button('Retry exact submitted request')).toBeTruthy()
    expect(document.body.textContent).not.toContain(
      'current retained decision and published admission eligibility are confirmed',
    )
    status = 409
    await click('Retry exact submitted request')
    expect(button('Abandon submitted request')).toBeTruthy()
    expect(writes()).toHaveLength(2)
    expect(writes()[1].data).toBe(original.data)
    expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    status = 0
    await click('Retry exact submitted request')
    expect(writes()).toHaveLength(3)
    expect(writes()[2].data).toBe(original.data)
    expect(writes()[2].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(document.body.textContent).toContain(
      'current retained decision and published admission eligibility are confirmed',
    )
  },
)
