import { act, isValidElement, useEffect, useLayoutEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { getSession } from '@/api/auth'
import AuthGate from '@/components/app/AuthGate'
import { sessionKey, setupKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { DefaultLimitPolicy, DefaultLimitResetContext } from '@/types/default-limits'
import type {
  SubmittedIntent,
  SubmittedIntentClaim,
  SubmittedIntentOwner,
} from '@/types/uncertain-intents'
import { UncertainIntentProvider } from './uncertain-intents'
import { useUncertainIntents } from '@/hooks/use-uncertain-intents'
import routes from '@/router'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let host: HTMLDivElement, root: Root, cache: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let sessionStatus: number, actor: string, csrf: string, permission: boolean
let requests: InternalAxiosRequestConfig[]
let latestOwner: SubmittedIntentOwner | null, originalClaim: SubmittedIntentClaim | null
let originalOwner: SubmittedIntentOwner | null
let submission: SubmittedIntent
let privateMounts: number, privateUnmounts: number

const integerPolicy: DefaultLimitPolicy = {
  tokens_5h: 0,
  tokens_7d: null,
  tokens_month: 100,
  rpm: null,
  tpm: 0,
  concurrency: 2,
  money_month: '2.000000000000000001',
  currency: 'USD',
}
function resetReview(): DefaultLimitResetContext {
  return {
    kind: 'user',
    id: 'usr_subject',
    etag: 'a'.repeat(64),
    editable: true,
    applied_default_etag: null,
    default_rule: {
      kind: 'user',
      etag: 'b'.repeat(64),
      rule_etag: 'c'.repeat(64),
      policy: { ...integerPolicy },
      platform_currency: 'USD',
      editable: true,
      updated_at: '2026-10-06T01:00:00Z',
    },
    limit: {
      kind: 'user',
      id: 'usr_subject',
      account_id: 'user_usr_subject',
      etag: 'resource-old',
      platform_currency: 'USD',
      stored: { ...integerPolicy, ip_mode: 'allowlist', ip_ranges: ['192.0.2.0/24'] },
      effective: { ...integerPolicy },
      ip_policies: [{ ...integerPolicy, ip_mode: 'allowlist', ip_ranges: ['192.0.2.0/24'] }],
      enforced: true,
      quota_usage: {
        as_of: '2026-10-06T01:00:00Z',
        activated: true,
        coverage_start: '2026-10-01T00:00:00Z',
        time_zone: 'UTC',
        active: null,
        minute: null,
        five_hours: null,
        seven_days: null,
        month: null,
      },
      rpm_used: 5,
      active: 1,
    },
  }
}
const teamIntent = (): SubmittedIntent => ({
  kind: 'team-create',
  payload: {
    etag: 'd'.repeat(64),
    body: {
      creation_id: '607aa1a1-ecb1-40d4-aa4f-519d720f4a60',
      name: 'Submitted Team',
      description: 'Captured description',
      owner_ids: ['usr_owner'],
      initial_limits: {
        reason: 'Captured reason',
        tokens_month: 0,
        money_month: '2.000000000000000001',
        currency: 'USD',
      },
    },
  },
})

beforeEach(() => {
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  sessionStatus = 200
  actor = 'usr_actor'
  csrf = 'csrf-original'
  permission = true
  requests = []
  latestOwner = null
  originalClaim = null
  originalOwner = null
  privateMounts = 0
  privateUnmounts = 0
  submission = teamIntent()
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/setup') response.data = { initialized: true }
    else if (config.url === '/auth/session') {
      response.data = {
        user: { id: actor, role: 'admin', name: actor, email: 'actor@example.invalid' },
        csrf_token: csrf,
      }
      if (sessionStatus !== 200)
        throw new AxiosError('Session read failed', '', config, undefined, {
          ...response,
          status: sessionStatus,
        })
    } else if (config.url === '/scope-permissions') response.data = permission
    else if (config.url === '/scope-context')
      response.data = { current: true, etag: 'current-review', currency: 'EUR' }
    else if (config.url === '/submitted-intent') response.data = { saved: true }
    else throw new Error(`Unexpected request: ${config.url}`)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
})
async function until(assert: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
function Probe() {
  const owner = useUncertainIntents()
  useLayoutEffect(() => {
    latestOwner = owner
  }, [owner])
  const currentActor = cache.getQueryData<Session>(sessionKey)?.user.id ?? ''
  const access = useQuery({
    queryKey: ['scope-permissions', currentActor],
    queryFn: async () => (await client.get<boolean>('/scope-permissions')).data,
    gcTime: 0,
    retry: false,
    refetchOnMount: 'always',
  })
  const context = useQuery({
    queryKey: ['scope-context', currentActor],
    queryFn: async () => (await client.get<{ current: boolean }>('/scope-context')).data,
    gcTime: 0,
    retry: false,
    refetchOnMount: 'always',
  })
  // The real consumers keep their independent authority/target checks.
  const session = cache.getQueryState<Session>(sessionKey)
  const fresh =
    session?.status === 'success' &&
    session.fetchStatus === 'idle' &&
    !session.isInvalidated &&
    access.isSuccess &&
    !access.isFetching &&
    access.data &&
    context.isSuccess &&
    !context.isFetching
  async function submit() {
    if (!fresh || !owner) return
    const claim = retained?.claim ?? owner.capture(currentActor, submission)
    if (!claim) return
    originalClaim ??= claim
    originalOwner ??= owner
    const payload = owner.recover(currentActor)
    if (!payload) return
    await client.post('/submitted-intent', payload.payload, {
      headers: { 'X-CSRF-Token': cache.getQueryData<Session>(sessionKey)!.csrf_token },
    })
  }
  const retained = fresh ? owner?.recover(currentActor) : null
  useEffect(() => {
    privateMounts++
    return () => {
      privateUnmounts++
    }
  }, [])
  if (!fresh) return <p>Current private reads required</p>
  return (
    <section aria-label="Private content">
      <p>Private metadata</p>
      <button onClick={() => void submit()}>Submit exact intent</button>
      {retained && <pre>{JSON.stringify(retained.payload)}</pre>}
    </section>
  )
}
async function mount(path = '/admin/teams/new') {
  router = createMemoryRouter(
    [
      {
        element: (
          <UncertainIntentProvider>
            <AuthGate mode="private" />
          </UncertainIntentProvider>
        ),
        children: [
          { path: '/admin/teams/new', element: <Probe /> },
          { path: '/admin/members/:memberId', element: <Probe /> },
          { path: '/other', element: <p>Other private route</p> },
        ],
      },
      { path: '/login', element: <p>Login required</p> },
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
  await until(() => expect(host.textContent).toContain('Private metadata'))
}
async function clickSubmit() {
  await act(async () => host.querySelector('button')!.click())
  await until(() => expect(requests.filter((r) => r.url === '/submitted-intent')).toHaveLength(1))
}
async function renewal(status: number) {
  sessionStatus = status
  await act(async () => cache.refetchQueries({ queryKey: sessionKey, exact: true }))
  if (status === 500) await until(() => expect(host.textContent).toContain('Unable to check'))
}

// Importing the actual route declaration also proves the wrapper is private-only.
describe('private AuthGate submitted-intent lifetime', () => {
  it('wraps only the private route while preserving setup/login gates', () => {
    const setup = routes[0].element,
      login = routes[1].element,
      privateGate = routes[2].element
    if (
      !isValidElement(setup) ||
      !isValidElement(login) ||
      !isValidElement<{ children: unknown }>(privateGate) ||
      !isValidElement<{ mode: string }>(privateGate.props.children)
    )
      throw new Error('Invalid gate shape')
    expect(setup.type).toBe(AuthGate)
    expect(login.type).toBe(AuthGate)
    expect(privateGate.type).toBe(UncertainIntentProvider)
    expect(privateGate.props.children.type).toBe(AuthGate)
    expect(privateGate.props.children.props.mode).toBe('private')
  })
  it('preserves only the original submitted request through500 and fresh same-actor recovery', async () => {
    const storage = vi.spyOn(Storage.prototype, 'setItem')
    await mount()
    await clickSubmit()
    const before = originalOwner!.recover(actor)!
    expect(JSON.stringify(before.payload)).toBe(JSON.stringify(submission.payload))
    await renewal(500)
    await until(() => expect(host.textContent).toContain('Unable to check'))
    expect(host.querySelector('[aria-label="Private content"]')).toBeNull()
    expect(originalOwner!.recover(actor)).toBeNull()
    expect(originalOwner!.clear(originalClaim!)).toBe(false)
    csrf = 'csrf-renewed'
    await renewal(200)
    await until(() => expect(host.textContent).toContain('Private metadata'))
    expect(privateMounts).toBeGreaterThan(1)
    expect(privateUnmounts).toBe(1)
    const after = latestOwner!.recover(actor)!
    expect(after.payload).toEqual(before.payload)
    expect(after.claim).not.toBe(before.claim)
    expect(originalOwner!.clear(before.claim)).toBe(false)
    expect(requests.filter((r) => r.url === '/submitted-intent')).toHaveLength(1)
    await act(async () => host.querySelector('button')!.click())
    await until(() => expect(requests.filter((r) => r.url === '/submitted-intent')).toHaveLength(2))
    const posts = requests.filter((r) => r.url === '/submitted-intent')
    expect(posts[1].data).toBe(posts[0].data)
    expect(posts[1].headers.get('X-CSRF-Token')).toBe('csrf-renewed')
    expect(storage).not.toHaveBeenCalled()
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    expect(cache.getQueryCache().find({ queryKey: sessionKey })?.getObserversCount()).toBe(1)
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((q) => q.state.data),
      ),
    ).not.toContain('607aa1a1')
  })
  it('clones only submitted Model IDs/token, preserves serialized order and never retains candidate metadata', async () => {
    submission = teamIntent()
    if (submission.kind !== 'team-create') throw new Error('fixture')
    submission.payload.body = {
      ...submission.payload.body,
      model_review_token: 'f'.repeat(64),
      model_ids: ['mdl_Case', 'mdl_case'],
    }
    Object.assign(submission.payload.body, {
      candidates: [{ name: 'Private Model', provider: 'Private Provider' }],
      csrf: 'never-retain',
    })
    await mount()
    await clickSubmit()
    const recovered = latestOwner!.recover(actor)!
    if (recovered.kind !== 'team-create') throw new Error('fixture')
    const original = Object.fromEntries(
      Object.entries(submission.payload.body).filter(
        ([key]) => !['candidates', 'csrf'].includes(key),
      ),
    )
    expect(JSON.stringify(recovered.payload.body)).toBe(JSON.stringify(original))
    submission.payload.body.model_ids!.push('mdl_later')
    submission.payload.body.model_review_token = 'e'.repeat(64)
    expect(latestOwner!.recover(actor)?.payload).toEqual(recovered.payload)
    expect(JSON.stringify(recovered.payload)).not.toContain('Private Model')
    expect(JSON.stringify(recovered.payload)).not.toContain('never-retain')
  })
  it('does not turn a recovered claim into target/permission authority', async () => {
    await mount()
    await clickSubmit()
    await renewal(500)
    permission = false
    await renewal(200)
    await until(() => expect(host.textContent).toContain('Current private reads required'))
    expect(host.querySelector('pre')).toBeNull()
    expect(requests.filter((r) => r.url === '/submitted-intent')).toHaveLength(1)
    expect(latestOwner!.recover(actor)?.payload).toEqual(submission.payload)
  })
  it.each(['401', 'null', 'expiry', 'removal', 'different actor', 'relogin'])(
    'destroys intent on%s without accepting an obsolete callback',
    async (reason) => {
      await mount()
      await clickSubmit()
      const old = originalOwner!,
        claim = originalClaim!
      await act(async () => {
        if (reason === '401') {
          sessionStatus = 401
          await cache.refetchQueries({ queryKey: sessionKey })
        }
        if (reason === 'null') cache.setQueryData(sessionKey, null)
        if (reason === 'expiry') window.dispatchEvent(new Event('routex:session-expired'))
        if (reason === 'removal' || reason === 'relogin')
          cache.removeQueries({ queryKey: sessionKey, exact: true })
        if (reason === 'different actor')
          cache.setQueryData(sessionKey, {
            user: {
              id: 'usr_other',
              role: 'member',
              name: 'Other',
              email: 'other@example.invalid',
            },
            csrf_token: 'other-csrf',
          })
        if (reason === 'relogin')
          cache.setQueryData(sessionKey, {
            user: { id: actor, role: 'admin', name: actor, email: 'actor@example.invalid' },
            csrf_token: 'new-login',
          })
      })
      expect(old.isCurrent(claim)).toBe(false)
      expect(old.clear(claim)).toBe(false)
      expect(old.recover(actor)).toBeNull()
      expect(requests.filter((r) => r.url === '/submitted-intent')).toHaveLength(1)
    },
  )
  it.each(['pathname', 'tab'])(
    'clears on actual%s exit while GateState has unmounted the consumer',
    async (change) => {
      await mount()
      await clickSubmit()
      await renewal(500)
      const old = originalOwner!,
        claim = originalClaim!
      await act(async () =>
        router.navigate(change === 'pathname' ? '/other' : '/admin/teams/new?tab=other'),
      )
      expect(old.clear(claim)).toBe(false)
      expect(old.recover(actor)).toBeNull()
      await renewal(200)
      await act(async () => router.navigate('/admin/teams/new'))
      await until(() => expect(host.textContent).toContain('Private metadata'))
      expect(latestOwner!.recover(actor)).toBeNull()
    },
  )
  it('retains across irrelevant query changes and strips unknown properties without sharing mutable payloads', async () => {
    await mount()
    await clickSubmit()
    const original = originalClaim!
    await act(async () => router.navigate('/admin/teams/new?filter=x&sort=y'))
    expect(latestOwner!.isCurrent(original)).toBe(true)
    const recovered = latestOwner!.recover(actor)!
    expect(recovered.kind).toBe('team-create')
    if (recovered.kind !== 'team-create' || submission.kind !== 'team-create')
      throw new Error('Wrong case')
    submission.payload.body.owner_ids.push('mutated-source')
    recovered.payload.body.owner_ids.push('mutated-result')
    expect(latestOwner!.recover(actor)!.payload).not.toEqual(recovered.payload)
    expect(latestOwner!.capture(actor, teamIntent())).toBeNull()
    const old = latestOwner!
    await act(async () => {
      expect(old.clear({ ...original })).toBe(false)
      expect(old.clear(original)).toBe(true)
      const injected = { ...teamIntent(), session: { csrf_token: 'must-not-retain' } }
      if (injected.kind !== 'team-create') throw new Error('Wrong injection fixture')
      Object.assign(injected.payload.body, {
        csrf_token: 'must-not-retain',
        secret: 'must-not-retain',
      })
      Object.assign(injected.payload.body.initial_limits!, { password: 'must-not-retain' })
      const next = old.capture(actor, injected)!
      expect(next).not.toBe(original)
      expect(old.clear(original)).toBe(false)
      expect(JSON.stringify(old.recover(actor))).not.toContain('must-not-retain')
    })
  })
  it('rejects recapture synchronously on expiry even before AuthGate async null cleanup', async () => {
    await mount()
    await clickSubmit()
    const owner = latestOwner!
    await act(async () => {
      window.dispatchEvent(new Event('routex:session-expired'))
      expect(owner.capture(actor, teamIntent())).toBeNull()
      expect(owner.recover(actor)).toBeNull()
      const current = cache.getQueryCache().find({ queryKey: sessionKey, exact: true })!
      current.setData(
        {
          user: { id: actor, role: 'admin', name: actor, email: 'actor@example.invalid' },
          csrf_token: 'early-success',
        },
        { manual: false },
      )
      expect(owner.capture(actor, teamIntent())).toBeNull()
      expect(owner.recover(actor)).toBeNull()
      expect(owner.isCurrent(originalClaim!)).toBe(false)
    })
  })
  it('allows a genuinely later Session read only after expiry cleanup, without restoring old intent', async () => {
    await mount()
    await clickSubmit()
    const owner = originalOwner!,
      old = originalClaim!
    // The real setup-error GateState keeps the private provider mounted after null.
    await act(async () =>
      cache
        .getQueryCache()
        .find({ queryKey: setupKey, exact: true })!
        .setState({ status: 'error', error: new Error('Setup unavailable') }),
    )
    await until(() => expect(host.textContent).toContain('Unable to check'))
    await act(async () => window.dispatchEvent(new Event('routex:session-expired')))
    await until(() => expect(cache.getQueryData(sessionKey)).toBeNull())
    await act(async () =>
      cache.setQueryData(sessionKey, {
        user: { id: actor, role: 'admin', name: actor, email: 'actor@example.invalid' },
        csrf_token: 'manual-reseed',
      }),
    )
    expect(owner.capture(actor, teamIntent())).toBeNull()
    await act(async () =>
      cache.fetchQuery({ queryKey: sessionKey, queryFn: getSession, staleTime: 0 }),
    )
    let next: SubmittedIntentClaim | null = null
    await act(async () => {
      next = owner.capture(actor, teamIntent())
    })
    expect(next).not.toBeNull()
    expect(next!.epoch).toBeGreaterThan(old.epoch)
    expect(owner.clear(old)).toBe(false)
    expect(host.querySelector('[aria-label="Private content"]')).toBeNull()
    expect(requests.filter((r) => r.url === '/submitted-intent')).toHaveLength(1)
  })
  it('keeps a manual CSRF replacement in the same Session while definitive cache replacement clears', async () => {
    await mount()
    await clickSubmit()
    const claim = originalClaim!,
      owner = latestOwner!
    await act(async () =>
      cache.setQueryData(sessionKey, {
        ...cache.getQueryData<Session>(sessionKey)!,
        csrf_token: 'rotated-csrf',
      }),
    )
    expect(owner.isCurrent(claim)).toBe(true)
    expect(owner.recover(actor)?.claim).toBe(claim)
    await act(async () => {
      cache.removeQueries({ queryKey: sessionKey, exact: true })
      cache.setQueryData(sessionKey, {
        user: { id: actor, role: 'admin', name: actor, email: 'actor@example.invalid' },
        csrf_token: 'new-login',
      })
      expect(owner.capture(actor, teamIntent())).toBeNull()
    })
    expect(owner.clear(claim)).toBe(false)
  })
  it('checks exact actor and Restore target identity without borrowing aliases', async () => {
    await mount('/admin/members/usr_subject?tab=limits')
    const owner = latestOwner!
    expect(owner.capture('USR_ACTOR', teamIntent())).toBeNull()
    expect(owner.capture('usr_actor ', teamIntent())).toBeNull()
    const review = resetReview()
    expect(
      owner.capture(actor, {
        kind: 'restore-defaults',
        payload: {
          target: { kind: 'user', id: 'USR_SUBJECT' },
          review,
          reason: 'reason',
        },
      }),
    ).toBeNull()
    expect(
      owner.capture(actor, {
        kind: 'restore-defaults',
        payload: {
          target: { kind: 'user', id: 'usr_subject ' },
          review,
          reason: 'reason',
        },
      }),
    ).toBeNull()
    expect(owner.epoch).toBe(0)
  })
  it('clears a Restore target on navigation during500 without relying on child cleanup', async () => {
    submission = {
      kind: 'restore-defaults',
      payload: {
        target: { kind: 'user', id: 'usr_subject' },
        review: resetReview(),
        reason: 'Original reset reason',
      },
    }
    await mount('/admin/members/usr_subject?tab=limits')
    await clickSubmit()
    await renewal(500)
    const old = originalOwner!,
      claim = originalClaim!
    await act(async () => router.navigate('/admin/members/usr_other?tab=limits'))
    expect(old.clear(claim)).toBe(false)
    await renewal(200)
    await until(() => expect(host.textContent).toContain('Private metadata'))
    expect(latestOwner!.recover(actor)).toBeNull()
    expect(requests.filter((r) => r.url === '/submitted-intent')).toHaveLength(1)
  })
  it('ignores late events from an obsolete removed Session Query', async () => {
    await mount()
    await clickSubmit()
    const oldQuery = cache.getQueryCache().find({ queryKey: sessionKey, exact: true })!
    const oldClaim = originalClaim!
    await act(async () => cache.removeQueries({ queryKey: sessionKey, exact: true }))
    await act(async () => cache.fetchQuery({ queryKey: sessionKey, queryFn: getSession }))
    await until(() => expect(host.textContent).toContain('Private metadata'))
    const owner = latestOwner!
    let replacement: SubmittedIntentClaim | null = null
    await act(async () => {
      replacement = owner.capture(actor, teamIntent())
    })
    expect(replacement).not.toBeNull()
    await act(async () =>
      oldQuery.setData({
        user: { id: 'usr_obsolete', role: 'member', name: 'Old', email: 'old@example.invalid' },
        csrf_token: 'obsolete',
      }),
    )
    await until(() => expect(owner.recover(actor)).not.toBeNull())
    const current = owner.recover(actor)!
    expect(owner.isCurrent(current.claim)).toBe(true)
    expect(owner.clear(oldClaim)).toBe(false)
    expect(current.payload).toEqual(teamIntent().payload)
  })
  it('retains exact Restore submitted policy/IP/currency/reason but no live usage results', async () => {
    const reviewed = resetReview()
    Object.assign(reviewed, { csrf_token: 'must-not-retain' })
    Object.assign(reviewed.limit.stored, { secret: 'must-not-retain' })
    Object.assign(reviewed.default_rule.policy, { password: 'must-not-retain' })
    submission = {
      kind: 'restore-defaults',
      payload: {
        target: { kind: 'user', id: 'usr_subject' },
        review: reviewed,
        reason: 'Original reset reason',
      },
    }
    await mount('/admin/members/usr_subject?tab=limits')
    await clickSubmit()
    await renewal(500)
    await renewal(200)
    await until(() => expect(host.textContent).toContain('Private metadata'))
    const result = latestOwner!.recover(actor)!
    expect(result.kind).toBe('restore-defaults')
    if (result.kind !== 'restore-defaults') throw new Error('Wrong case')
    expect(result.payload.target).toEqual({ kind: 'user', id: 'usr_subject' })
    expect(result.payload.review.etag).toBe('a'.repeat(64))
    expect(result.payload.review.default_rule.policy).toEqual(integerPolicy)
    expect(result.payload.review.limit.stored.ip_ranges).toEqual(['192.0.2.0/24'])
    expect(result.payload.reason).toBe('Original reset reason')
    for (const key of [
      'quota_usage',
      'rpm_used',
      'active',
      'enforced',
      'effective',
      'editable_fields',
    ])
      expect(Object.hasOwn(result.payload.review.limit, key)).toBe(false)
    expect(Object.hasOwn(result.payload.review, 'editable')).toBe(false)
    expect(Object.hasOwn(result.payload.review.default_rule, 'editable')).toBe(false)
    expect(JSON.stringify(result.payload)).not.toContain('must-not-retain')
    expect(requests.filter((r) => r.url === '/submitted-intent')).toHaveLength(1)
  })
})

describe('default-rule submitted intent', () => {
  it('clones only submitted policy/reason/target/etag in original order and isolates both source and recovered mutations', async () => {
    await mount('/admin/members/usr_subject?tab=limits')
    const submitted: SubmittedIntent = {
      kind: 'default-limit-save',
      payload: {
        target: 'team',
        etag: 'd'.repeat(64),
        input: { reason: 'Original reason', policy: { ...integerPolicy } },
      },
    }
    const original = JSON.stringify(submitted.payload.input)
    Object.assign(submitted.payload, { csrf_token: 'never retained', response: { saved: true } })
    Object.assign(submitted.payload.input, { session: 'never retained' })
    Object.assign(submitted.payload.input.policy, { unknown: 'never retained' })
    let claim: SubmittedIntentClaim | null = null
    await act(async () => {
      claim = latestOwner!.capture(actor, submitted)
    })
    expect(claim).not.toBeNull()
    const recovered = latestOwner!.recover(actor)!
    expect(recovered.kind).toBe('default-limit-save')
    if (recovered.kind !== 'default-limit-save') throw new Error('Wrong intent kind')
    expect(JSON.stringify(recovered.payload.input)).toBe(original)
    expect(Object.keys(recovered.payload)).toEqual(['target', 'etag', 'input'])
    expect(Object.keys(recovered.payload.input)).toEqual(['reason', 'policy'])
    expect(recovered.claim.targetScope).toBe(JSON.stringify(['default-limit-save', 'team']))
    submitted.payload.input.reason = 'Source changed'
    submitted.payload.input.policy.money_month = '99'
    recovered.payload.input.reason = 'Recovered changed'
    recovered.payload.input.policy.tokens_5h = 100
    const next = latestOwner!.recover(actor)!
    expect(JSON.stringify(next.payload)).toBe(
      JSON.stringify({ target: 'team', etag: 'd'.repeat(64), input: JSON.parse(original) }),
    )
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it.each(['TEAM', 'team ', 'project'])(
    'rejects noncanonical default target %s without borrowing a slot',
    async (target) => {
      await mount()
      let claim: SubmittedIntentClaim | null = null
      await act(async () => {
        claim = latestOwner!.capture(actor, {
          kind: 'default-limit-save',
          payload: {
            target: target as 'team',
            etag: 'd'.repeat(64),
            input: { policy: { ...integerPolicy }, reason: 'Original' },
          },
        })
      })
      expect(claim).toBeNull()
      expect(latestOwner!.recover(actor)).toBeNull()
      expect(latestOwner!.epoch).toBe(0)
    },
  )
})
describe('Provider name submitted intent', () => {
  it('clones only exact submitted target/name/reason/etag and renews claims across real Gate500', async () => {
    await mount()
    const submitted: SubmittedIntent = {
      kind: 'provider-name',
      payload: {
        provider_id: 'prv_one',
        etag: `${'a'.repeat(64)}.${'b'.repeat(64)}`,
        input: { name: '\ufeffSubmitted', reason: 'Exact reason' },
      },
    }
    Object.assign(submitted.payload, { birth: 'not public', metadata: { can_edit: true } })
    Object.assign(submitted.payload.input, { secret: 'not retained' })
    let claim: SubmittedIntentClaim | null = null
    await act(async () => {
      claim = latestOwner!.capture(actor, submitted)
    })
    expect(claim).not.toBeNull()
    const original = latestOwner!.recover(actor)!
    expect(original.claim.targetScope).toBe(JSON.stringify(['provider-name', 'prv_one']))
    expect(original.payload).toEqual({
      provider_id: 'prv_one',
      etag: submitted.payload.etag,
      input: { name: '\ufeffSubmitted', reason: 'Exact reason' },
    })
    submitted.payload.input.name = 'Mutated source'
    if (original.kind !== 'provider-name') throw new Error('Wrong intent kind')
    original.payload.input.reason = 'Mutated recovery'
    await renewal(500)
    expect(latestOwner?.recover(actor)).toBeNull()
    await renewal(200)
    await until(() => expect(latestOwner!.recover(actor)?.kind).toBe('provider-name'))
    const next = latestOwner!.recover(actor)!
    expect(next.claim).not.toBe(claim)
    expect(latestOwner!.clear(claim!)).toBe(false)
    expect(next.payload).toEqual({
      provider_id: 'prv_one',
      etag: submitted.payload.etag,
      input: { name: '\ufeffSubmitted', reason: 'Exact reason' },
    })
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it.each(['prv_', 'PRV_one', 'prv_one ', 'prv_' + 'x'.repeat(27), 'prv_一'])(
    'rejects provider target %s without borrowing an intent slot',
    async (provider_id) => {
      await mount()
      let claim: SubmittedIntentClaim | null = null
      await act(async () => {
        claim = latestOwner!.capture(actor, {
          kind: 'provider-name',
          payload: { provider_id, etag: 'review', input: { name: 'Name', reason: 'Reason' } },
        })
      })
      expect(claim).toBeNull()
      expect(latestOwner!.recover(actor)).toBeNull()
    },
  )
})
