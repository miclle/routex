import { act, useEffect, useLayoutEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import AuthGate from '@/components/app/AuthGate'
import { UncertainIntentProvider } from '@/context/uncertain-intents'
import { useUncertainIntents } from '@/hooks/use-uncertain-intents'
import { sessionKey } from '@/hooks/use-auth'
import client from '@/api/client'
import i18n from '@/i18n'
import type { Session } from '@/types/auth'
import type { DefaultLimitKind, DefaultLimitRecord } from '@/types/default-limits'
import type { SubmittedIntentOwner } from '@/types/uncertain-intents'
import DefaultLimitsPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let root: Root, host: HTMLDivElement, cache: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let actor: string,
  csrf: string,
  sessionFailure: number,
  permissionFailure: number,
  ruleFailure: number
let permissions: string[],
  requests: InternalAxiosRequestConfig[],
  writes: InternalAxiosRequestConfig[]
let records: Record<DefaultLimitKind, DefaultLimitRecord>, owner: SubmittedIntentOwner | null
let pendingWrite: Promise<void> | undefined, writeFailure: number
let permissionHold: Promise<void> | undefined, ruleHold: Promise<void> | undefined
let mounts: number, unmounts: number
function session(): Session {
  return {
    user: { id: actor, name: 'Reviewer', email: 'reviewer@example.invalid', role: 'admin' },
    csrf_token: csrf,
  } as Session
}
function fixture(kind: DefaultLimitKind): DefaultLimitRecord {
  return {
    kind,
    etag: (kind === 'team' ? 'a' : 'b').repeat(64),
    rule_etag: 'c'.repeat(64),
    platform_currency: 'USD',
    editable: true,
    updated_at: '2026-10-06T00:00:00Z',
    policy: {
      tokens_5h: 0,
      tokens_7d: null,
      tokens_month: Number.MAX_SAFE_INTEGER,
      rpm: 60,
      tpm: null,
      concurrency: 4,
      money_month: '12.500000000000000001',
      currency: 'USD',
    },
  }
}
function Page() {
  const current = useUncertainIntents()
  useLayoutEffect(() => {
    owner = current
  }, [current])
  useEffect(() => {
    mounts++
    return () => {
      unmounts++
    }
  }, [])
  return <DefaultLimitsPage />
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  actor = 'usr_reviewer'
  csrf = 'csrf-original'
  sessionFailure = 0
  permissionFailure = 0
  ruleFailure = 0
  permissions = ['limits.settings.write']
  requests = []
  writes = []
  owner = null
  mounts = 0
  unmounts = 0
  records = { user: fixture('user'), team: fixture('team') }
  pendingWrite = undefined
  permissionHold = undefined
  ruleHold = undefined
  writeFailure = 503
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    let failure = 0
    if (config.url === '/setup') response.data = { initialized: true }
    else if (config.url === '/auth/session') {
      response.data = session()
      failure = sessionFailure
    } else if (config.url === '/auth/permissions') {
      if (permissionHold) await permissionHold
      response.data = { permissions }
      failure = permissionFailure
    } else if (config.url?.startsWith('/admin/default-limits/')) {
      const kind = config.url.endsWith('/team') ? 'team' : 'user'
      if (config.method === 'put') {
        writes.push(config)
        const failureAtDispatch = writeFailure
        const submitted = JSON.parse(config.data)
        // Model a durable successful write with a response withheld/faulted separately.
        if (!failureAtDispatch || failureAtDispatch >= 500)
          records[kind] = {
            ...records[kind],
            policy: submitted.policy,
            etag: 'd'.repeat(64),
            rule_etag: 'e'.repeat(64),
          }
        response.data = structuredClone(records[kind])
        if (pendingWrite) await pendingWrite
        failure = failureAtDispatch
      } else {
        if (ruleHold) await ruleHold
        response.data = structuredClone(records[kind])
        failure = ruleFailure
      }
    } else throw new Error(`Unexpected request: ${config.url}`)
    if (failure)
      throw new AxiosError('Controlled response failure', '', config, undefined, {
        ...response,
        status: failure,
      })
    return response
  }
  router = createMemoryRouter(
    [
      {
        element: (
          <UncertainIntentProvider>
            <AuthGate mode="private" />
          </UncertainIntentProvider>
        ),
        children: [
          { path: '/admin/limits', element: <Page /> },
          { path: '/other', element: <p>Other private route</p> },
        ],
      },
      { path: '/login', element: <p>Signed out</p> },
    ],
    { initialEntries: ['/admin/limits'] },
  )
})
afterEach(async () => {
  await act(async () => root.unmount())
  router.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
})
async function until(assertion: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      assertion()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
function button(label: string) {
  const found = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label,
  )
  expect(found, label).toBeDefined()
  return found!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
function field(label: string) {
  return document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
}
async function input(label: string, value: string) {
  const element = field(label)
  expect(element, label).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, value)
    element.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain('Monthly budget'))
}
async function submit(kind: DefaultLimitKind = 'team') {
  await mount()
  if (kind === 'team') await click('Team defaults')
  await until(() => expect(button('Edit').disabled).toBe(false))
  await click('Edit')
  await input('Reason', 'Original submitted reason')
  await click('Save changes')
  await until(() => expect(writes).toHaveLength(1))
}
async function sessionError() {
  sessionFailure = 500
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey, exact: true })
  })
  await until(() => expect(host.textContent).toContain('Unable to check your session'))
  expect(host.textContent).not.toContain('Monthly budget')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(unmounts).toBeGreaterThan(0)
}
async function recover() {
  sessionFailure = 0
  csrf = 'csrf-renewed'
  await click('Retry')
  await until(() => expect(host.textContent).toContain('Monthly budget'))
}

it('recovers exact Team submitted bytes through actual AuthGate500 only after fresh authority and target, with explicit current-CSRF retry', async () => {
  const storage = vi.spyOn(Storage.prototype, 'setItem')
  await submit()
  await until(() => expect(button('Retry original request').disabled).toBe(false))
  const original = writes[0]
  const oldOwner = owner!,
    oldClaim = oldOwner.recover(actor)!.claim
  await sessionError()
  expect(oldOwner.recover(actor)).toBeNull()
  records.team = {
    ...records.team,
    etag: 'f'.repeat(64),
    platform_currency: 'EUR',
    policy: { ...records.team.policy, money_month: '99.999999999999999999', currency: 'EUR' },
  }
  let releasePermission!: () => void, releaseRule!: () => void
  permissionHold = new Promise((resolve) => {
    releasePermission = resolve
  })
  ruleHold = new Promise((resolve) => {
    releaseRule = resolve
  })
  sessionFailure = 0
  csrf = 'csrf-renewed'
  await click('Retry')
  await until(() => expect(mounts).toBeGreaterThan(1))
  expect(host.textContent).not.toContain('Team defaults')
  expect(field('Reason')).toBeNull()
  await act(async () => {
    permissionHold = undefined
    releasePermission()
  })
  await until(() =>
    expect(
      requests.filter((r) => r.method === 'get' && r.url?.endsWith('/team')).length,
    ).toBeGreaterThan(1),
  )
  expect(field('Reason')).toBeNull()
  await act(async () => {
    ruleHold = undefined
    releaseRule()
  })
  await until(() => expect(field('Reason')?.value).toBe('Original submitted reason'))
  expect(field('Monthly budget').value).toBe('12.500000000000000001')
  expect(field('Five-hour tokens').value).toBe('0')
  expect(field('Seven-day tokens').value).toBe('')
  expect(field('Monthly tokens').value).toBe(String(Number.MAX_SAFE_INTEGER))
  expect(host.textContent).toContain('Platform currency: USD')
  expect(button('User defaults').getAttribute('aria-disabled')).toBe('true')
  expect(writes).toHaveLength(1)
  expect(oldOwner.clear(oldClaim)).toBe(false)
  const retained = owner!.recover(actor)!
  expect(retained.kind).toBe('default-limit-save')
  if (retained.kind !== 'default-limit-save') throw new Error('Wrong retained intent')
  expect(retained.payload.target).toBe('team')
  expect(JSON.stringify(retained.payload.input)).toBe(original.data)
  expect(retained.payload).not.toHaveProperty('csrf_token')
  expect(retained.payload).not.toHaveProperty('record')
  expect(storage).not.toHaveBeenCalled()
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  writeFailure = 0
  await click('Retry original request')
  await until(() => expect(host.textContent).toContain('Default rule saved for future creation'))
  expect(writes).toHaveLength(2)
  expect(writes[1].url).toBe(original.url)
  expect(writes[1].data).toBe(original.data)
  expect(writes[1].headers['If-Match']).toBe(original.headers['If-Match'])
  expect(writes[1].headers['X-CSRF-Token']).toBe('csrf-renewed')
  expect(owner!.recover(actor)).toBeNull()
})

it('retains an uncertain save through current review and rejected409, then explicit abandonment requires new review without replacing original bytes', async () => {
  await submit('user')
  const original = writes[0]
  await sessionError()
  await recover()
  records.user = { ...records.user, etag: 'f'.repeat(64), platform_currency: 'EUR' }
  await click('Review latest policy')
  expect(writes).toHaveLength(1)
  writeFailure = 409
  await click('Retry original request')
  await until(() => expect(writes).toHaveLength(2))
  expect(writes[1].data).toBe(original.data)
  expect(writes[1].headers['If-Match']).toBe(original.headers['If-Match'])
  expect(host.textContent).toContain('result is uncertain')
  await click('Abandon original save')
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeTruthy())
  const popup = document.querySelector('[role="dialog"]')!
  await act(async () =>
    popup.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
  )
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  await until(() => expect(document.activeElement).toBe(button('Abandon original save')))
  expect(owner!.recover(actor)).not.toBeNull()
  await click('Abandon original save')
  await click('Abandon save request')
  await until(() => expect(owner!.recover(actor)).toBeNull())
  expect(host.textContent).toContain('previous outcome remains unknown')
  expect(field('Reason').value).toBe('Original submitted reason')
  expect(button('Save changes').disabled).toBe(true)
  await click('Review latest policy')
  await until(() => expect(button('Save changes').disabled).toBe(false))
  await input('Reason', 'New separately reviewed reason')
  writeFailure = 0
  await click('Save changes')
  await until(() => expect(writes).toHaveLength(3))
  expect(writes[2].headers['If-Match']).toBe('"' + 'f'.repeat(64) + '"')
  expect(JSON.parse(writes[2].data).reason).toBe('New separately reviewed reason')
  expect(JSON.parse(writes[2].data).policy.currency).toBe('EUR')
})

it('allows a genuinely first pre-write409 to be explicitly reviewed without creating uncertainty', async () => {
  writeFailure = 409
  await submit('user')
  await until(() => expect(host.textContent).toContain('reviewed policy or currency changed'))
  expect(owner!.recover(actor)).toBeNull()
  expect(host.textContent).not.toContain('Retry original request')
  records.user = { ...records.user, etag: 'f'.repeat(64) }
  await click('Review latest policy')
  writeFailure = 0
  await click('Save changes')
  await until(() => expect(writes).toHaveLength(2))
  expect(writes[1].headers['If-Match']).toBe('"' + 'f'.repeat(64) + '"')
})

it('does not hydrate an unsubmitted draft after Session500 recovery', async () => {
  await mount()
  await click('Team defaults')
  await until(() => expect(button('Edit').disabled).toBe(false))
  await click('Edit')
  await input('Reason', 'Never submitted')
  expect(owner!.recover(actor)).toBeNull()
  await sessionError()
  await recover()
  expect(field('Reason')).toBeNull()
  expect(writes).toHaveLength(0)
  expect(button('User defaults').getAttribute('aria-selected')).toBe('true')
})

it('hides retained private values and blocks retry through denied or unavailable permission and target reads', async () => {
  await submit()
  await sessionError()
  permissionFailure = 503
  sessionFailure = 0
  await click('Retry')
  await until(() => expect(mounts).toBeGreaterThan(1))
  expect(field('Reason')).toBeNull()
  expect(writes).toHaveLength(1)
  permissionFailure = 0
  permissions = ['system.read']
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['permissions'] })
  })
  await until(() => expect(field('Reason')?.value).toBe('Original submitted reason'))
  expect(button('Retry original request').disabled).toBe(true)
  ruleFailure = 403
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['default-limits'] })
  })
  await until(() => expect(field('Reason')).toBeNull())
  expect(host.textContent).not.toContain('12.500000000000000001')
  expect(writes).toHaveLength(1)
  expect(owner!.recover(actor)).not.toBeNull()
})

it('ignores the obsolete saved response after renewal and explicit abandonment when a newer save owns the slot', async () => {
  let release!: () => void
  pendingWrite = new Promise((resolve) => {
    release = resolve
  })
  writeFailure = 0
  await submit()
  const oldOwner = owner!,
    oldClaim = oldOwner.recover(actor)!.claim
  await sessionError()
  pendingWrite = undefined
  await recover()
  await click('Abandon original save')
  await click('Abandon save request')
  await click('Review latest policy')
  await input('Reason', 'New owner of slot')
  writeFailure = 503
  await click('Save changes')
  await until(() => expect(writes).toHaveLength(2))
  const next = owner!.recover(actor)!
  expect(next.claim).not.toBe(oldClaim)
  await act(async () => release())
  await until(() => expect(button('Retry original request').disabled).toBe(false))
  expect(owner!.recover(actor)?.claim).toBe(next.claim)
  expect(host.textContent).not.toContain('Default rule saved for future creation')
  expect(field('Reason').value).toBe('New owner of slot')
})

it.each(['actor', 'logout', 'expiry', 'removal', 'route', 'tab', '401'] as const)(
  'destroys submitted default save on definitive %s boundary during gate failure',
  async (boundary) => {
    await submit()
    const oldOwner = owner!,
      oldClaim = oldOwner.recover(actor)!.claim
    await sessionError()
    if (boundary === 'actor') {
      actor = 'usr_other'
      sessionFailure = 0
      await act(async () => {
        await cache.refetchQueries({ queryKey: sessionKey, exact: true })
      })
    } else if (boundary === 'logout') await act(async () => cache.setQueryData(sessionKey, null))
    else if (boundary === 'expiry')
      await act(async () => window.dispatchEvent(new Event('routex:session-expired')))
    else if (boundary === 'removal')
      await act(async () => cache.removeQueries({ queryKey: sessionKey, exact: true }))
    else if (boundary === 'route') await act(async () => router.navigate('/other'))
    else if (boundary === 'tab') await act(async () => router.navigate('/admin/limits?tab=other'))
    else {
      sessionFailure = 401
      await act(async () => {
        await cache.refetchQueries({ queryKey: sessionKey, exact: true })
      })
    }
    expect(oldOwner.clear(oldClaim)).toBe(false)
    expect(oldOwner.recover(actor)).toBeNull()
    sessionFailure = 0
    await act(async () => {
      await cache.fetchQuery({ queryKey: sessionKey, queryFn: async () => session() })
      await router.navigate('/admin/limits')
    })
    if (host.textContent?.includes('Unable to check')) await click('Retry')
    await until(() => expect(host.textContent).toContain('Monthly budget'))
    expect(field('Reason')).toBeNull()
    expect(owner!.recover(actor)).toBeNull()
    expect(writes).toHaveLength(1)
  },
)

it('preserves original exact zero/null draft and reason through live language changes and mounted successful renewal', async () => {
  records.user.policy = { ...records.user.policy, money_month: null, currency: '', tokens_month: 0 }
  await submit('user')
  const original = writes[0]
  csrf = 'csrf-renewed'
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey, exact: true })
  })
  await until(() => expect(button('Retry original request').disabled).toBe(false))
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(field('理由').value).toBe('Original submitted reason')
  expect(field('月度预算').value).toBe('')
  expect(field('月度 Token 额度').value).toBe('0')
  expect(host.textContent).toContain('结果尚不确定')
  expect(host.textContent).toContain('未设置月度预算，无适用币种')
  writeFailure = 0
  await click('重试原请求')
  await until(() => expect(host.textContent).toContain('默认规则已保存'))
  expect(writes[1].data).toBe(original.data)
  expect(writes[1].headers['X-CSRF-Token']).toBe('csrf-renewed')
})

async function unsubmittedDraft(kind: DefaultLimitKind = 'team') {
  records[kind].policy.tokens_month = 200
  await mount()
  if (kind === 'team') await click('Team defaults')
  await until(() => expect(button('Edit').disabled).toBe(false))
  await click('Edit')
  await input('Monthly tokens', '201')
  await input('Monthly budget', '')
  await input('Reason', `Unsubmitted ${kind} reason`)
  expect(owner!.recover(actor)).toBeNull()
  expect(writes).toHaveLength(0)
}

it.each(['user', 'team'] as const)(
  'preserves the ordinary %s draft across successful Session renewal while hiding it until fresh permissions and target complete',
  async (kind) => {
    await unsubmittedDraft(kind)
    let permit!: () => void, read!: () => void
    permissionHold = new Promise<void>((resolve) => (permit = resolve))
    ruleHold = new Promise<void>((resolve) => (read = resolve))
    const readsBefore = requests.filter((r) => r.url === '/auth/permissions').length
    csrf = 'csrf-renewed-before-submit'
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey, exact: true })
    })
    await until(() =>
      expect(requests.filter((r) => r.url === '/auth/permissions')).toHaveLength(readsBefore + 1),
    )
    expect(field('Reason')).toBeNull()
    expect(field('Monthly tokens')).toBeNull()
    expect(writes).toHaveLength(0)
    expect(owner!.recover(actor)).toBeNull()
    await act(async () => {
      permissionHold = undefined
      permit()
    })
    await until(() =>
      expect(
        requests.filter((r) => r.method === 'get' && r.url?.endsWith('/' + kind)).length,
      ).toBeGreaterThan(1),
    )
    expect(field('Reason')).toBeNull()
    expect(writes).toHaveLength(0)
    await act(async () => {
      ruleHold = undefined
      read()
    })
    await until(() => expect(field('Reason')?.value).toBe(`Unsubmitted ${kind} reason`))
    expect(field('Monthly tokens').value).toBe('201')
    expect(field('Monthly budget').value).toBe('')
    expect(field('Five-hour tokens').value).toBe('0')
    expect(owner!.recover(actor)).toBeNull()
    expect(writes).toHaveLength(0)
    writeFailure = 0
    await click('Save changes')
    await until(() => expect(writes).toHaveLength(1))
    expect(writes[0].url).toBe('/admin/default-limits/' + kind)
    expect(writes[0].headers['If-Match']).toBe('"' + (kind === 'team' ? 'a' : 'b').repeat(64) + '"')
    expect(writes[0].headers['X-CSRF-Token']).toBe('csrf-renewed-before-submit')
    expect(JSON.parse(writes[0].data)).toMatchObject({
      policy: { tokens_month: 201, tokens_5h: 0, money_month: null, currency: '' },
      reason: `Unsubmitted ${kind} reason`,
    })
  },
)

it('keeps the ordinary reviewed generation and exact money until explicit review after a successful Session renewal returns a changed rule', async () => {
  await unsubmittedDraft()
  await input('Monthly budget', '2.000000000000000001')
  records.team = {
    ...records.team,
    etag: 'f'.repeat(64),
    platform_currency: 'EUR',
    policy: { ...records.team.policy, tokens_month: 300, currency: 'EUR' },
  }
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey, exact: true })
  })
  await until(() => expect(field('Reason')?.value).toBe('Unsubmitted team reason'))
  expect(field('Monthly tokens').value).toBe('201')
  expect(field('Monthly budget').value).toBe('2.000000000000000001')
  expect(button('Save changes').disabled).toBe(true)
  expect(host.textContent).toContain('USD')
  expect(writes).toHaveLength(0)
  expect(owner!.recover(actor)).toBeNull()
  await click('Review latest policy')
  expect(field('Reason').value).toBe('Unsubmitted team reason')
  expect(field('Monthly tokens').value).toBe('201')
  expect(field('Monthly budget').value).toBe('2.000000000000000001')
  writeFailure = 0
  await click('Save changes')
  await until(() => expect(writes).toHaveLength(1))
  expect(writes[0].headers['If-Match']).toBe('"' + 'f'.repeat(64) + '"')
  expect(JSON.parse(writes[0].data).policy).toMatchObject({
    money_month: '2.000000000000000001',
    currency: 'EUR',
    tokens_month: 201,
  })
})

it('hides an ordinary draft on renewed permission denial and target error without writing, then preserves it only for the same freshly authorized actor and target', async () => {
  await unsubmittedDraft()
  permissions = []
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey, exact: true })
  })
  await until(() => expect(host.textContent).toContain('does not have permission'))
  expect(field('Reason')).toBeNull()
  expect(field('Monthly tokens')).toBeNull()
  expect(writes).toHaveLength(0)
  permissions = ['limits.settings.write']
  ruleFailure = 403
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['permissions'] })
  })
  await until(() =>
    expect(host.textContent).toContain('You do not have permission for this action'),
  )
  expect(field('Reason')).toBeNull()
  expect(writes).toHaveLength(0)
  ruleFailure = 0
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['default-limits'] })
  })
  await until(() => expect(field('Reason')?.value).toBe('Unsubmitted team reason'))
  expect(field('Monthly tokens').value).toBe('201')
  expect(owner!.recover(actor)).toBeNull()
  expect(writes).toHaveLength(0)
})

it('destroys the ordinary draft on actor replacement and does not reuse its reason or policy in the new actor editor', async () => {
  await unsubmittedDraft()
  actor = 'usr_other_reviewer'
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey, exact: true })
  })
  await until(() => expect(button('Edit').disabled).toBe(false))
  expect(field('Reason')).toBeNull()
  await click('Edit')
  expect(field('Reason').value).toBe('')
  expect(field('Monthly tokens').value).toBe('200')
  expect(field('Monthly budget').value).toBe('12.500000000000000001')
  expect(owner!.recover(actor)).toBeNull()
  expect(writes).toHaveLength(0)
})

it('destroys ordinary target-local drafts on deliberate tab departure instead of applying them to another default rule or reviving them on return', async () => {
  await unsubmittedDraft()
  await click('User defaults')
  await until(() => expect(button('Edit').disabled).toBe(false))
  await click('Edit')
  expect(field('Reason').value).toBe('')
  expect(field('Monthly tokens').value).toBe(String(records.user.policy.tokens_month))
  await click('Team defaults')
  await until(() => expect(button('Edit').disabled).toBe(false))
  await click('Edit')
  expect(field('Reason').value).toBe('')
  expect(field('Monthly tokens').value).toBe('200')
  expect(writes).toHaveLength(0)
  expect(owner!.recover(actor)).toBeNull()
})

it.each(['permissions', 'target'] as const)(
  'keeps the local undispatched draft hidden through a renewed %s read error and restores it only after the real explicit read retry succeeds',
  async (stage) => {
    await unsubmittedDraft()
    if (stage === 'permissions') permissionFailure = 503
    else ruleFailure = 503
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey, exact: true })
    })
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(field('Reason')).toBeNull()
    expect(field('Monthly tokens')).toBeNull()
    expect(writes).toHaveLength(0)
    permissionFailure = 0
    ruleFailure = 0
    await click('Retry')
    await until(() => expect(field('Reason')?.value).toBe('Unsubmitted team reason'))
    expect(field('Monthly tokens').value).toBe('201')
    expect(field('Monthly budget').value).toBe('')
    expect(writes).toHaveLength(0)
    expect(owner!.recover(actor)).toBeNull()
  },
)
