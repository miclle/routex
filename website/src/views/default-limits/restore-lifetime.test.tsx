import { act, useEffect, useLayoutEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider, useParams } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it } from 'vitest'
import AuthGate from '@/components/app/AuthGate'
import { UncertainIntentProvider } from '@/context/uncertain-intents'
import { useUncertainIntents } from '@/hooks/use-uncertain-intents'
import { sessionKey } from '@/hooks/use-auth'
import client from '@/api/client'
import i18n from '@/i18n'
import type { Session } from '@/types/auth'
import type { DefaultLimitResetContext } from '@/types/default-limits'
import type { SubmittedIntentOwner } from '@/types/uncertain-intents'
import { teamFixture } from '@/views/resource-limits/fixture'
import RestoreDefaults from './restore'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let root: Root, host: HTMLDivElement, cache: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let actor: string, csrf: string, sessionFailure: number, permissionFailure: boolean
let permissions: string[], writes: InternalAxiosRequestConfig[], mounts: number, unmounts: number
let context: DefaultLimitResetContext, owner: SubmittedIntentOwner | null
let committedContext: DefaultLimitResetContext
let pendingWrite: Promise<void> | undefined, writeFailure: number
let resetHold: Promise<void> | undefined

function currentSession(): Session {
  return {
    user: { id: actor, name: actor, email: 'reviewer@example.invalid', role: 'admin' },
    csrf_token: csrf,
  } as Session
}
function Page() {
  const { target = 'tea_test' } = useParams()
  const currentOwner = useUncertainIntents()
  useLayoutEffect(() => {
    owner = currentOwner
  }, [currentOwner])
  useEffect(() => {
    mounts++
    return () => {
      unmounts++
    }
  }, [])
  return <RestoreDefaults target={{ kind: 'team', id: target }} />
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
  permissionFailure = false
  permissions = ['teams.tokens.write', 'teams.money.write', 'teams.rates.write']
  writes = []
  mounts = 0
  unmounts = 0
  owner = null
  pendingWrite = undefined
  resetHold = undefined
  writeFailure = 503
  const limit = teamFixture()
  limit.stored.ip_mode = 'allowlist'
  limit.stored.ip_ranges = ['192.0.2.0/24']
  context = {
    kind: 'team',
    id: 'tea_test',
    etag: 'c'.repeat(64),
    applied_default_etag: null,
    editable: true,
    limit,
    default_rule: {
      kind: 'team',
      etag: 'a'.repeat(64),
      rule_etag: 'b'.repeat(64),
      editable: true,
      updated_at: '2026-10-06T00:00:00Z',
      platform_currency: 'USD',
      policy: {
        tokens_5h: 0,
        tokens_7d: null,
        tokens_month: 500,
        rpm: 60,
        tpm: null,
        concurrency: 4,
        money_month: '12.500000000000000001',
        currency: 'USD',
      },
    },
  }
  committedContext = structuredClone(context)
  client.defaults.adapter = async (config) => {
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
      response.data = currentSession()
      failure = sessionFailure
    } else if (config.url === '/auth/permissions') {
      response.data = { permissions }
      failure = permissionFailure ? 503 : 0
    } else if (config.url?.endsWith('/default-reset')) {
      if (config.method === 'get') {
        if (resetHold) await resetHold
        const target = config.url.split('/')[2]
        response.data = {
          ...structuredClone(context),
          id: target,
          limit: { ...structuredClone(context.limit), id: target },
        }
      } else {
        writes.push(config)
        const failureAtDispatch = writeFailure
        if (pendingWrite) await pendingWrite
        failure = failureAtDispatch
        response.data = {
          kind: 'team',
          id: 'tea_test',
          saved: true,
          default_reset_etag: 'c'.repeat(64),
          applied_default_etag: 'b'.repeat(64),
          runtime_applied: true,
          limit: {
            ...committedContext.limit,
            enforced: true,
            stored: {
              ...committedContext.limit.stored,
              ...committedContext.default_rule.policy,
            },
          },
        }
      }
    } else throw new Error(`Unexpected request: ${config.url}`)
    if (failure)
      throw new AxiosError('Fixture failure', '', config, undefined, {
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
        children: [{ path: '/teams/:target', element: <Page /> }],
      },
      { path: '/login', element: <p>Signed out</p> },
    ],
    { initialEntries: ['/teams/tea_test?tab=limits'] },
  )
})
afterEach(async () => {
  await act(async () => root.unmount())
  router.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
})
async function until(assertion: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      assertion()
      return
    } catch {
      /* Wait for the actual rendered state. */
    }
  }
  assertion()
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
async function open() {
  await until(() => expect(host.textContent).toContain('Restore defaults'))
  await click('Restore defaults')
  await until(() => expect(document.querySelector('input[aria-label="Reason"]')).toBeTruthy())
}
async function enterReason() {
  const input = document.querySelector<HTMLInputElement>('input[aria-label="Reason"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      input,
      'Captured restoration',
    )
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function mountAndSubmit() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await open()
  await enterReason()
  await click('Confirm restoration')
  await until(() => expect(writes).toHaveLength(1))
}
async function sessionError() {
  sessionFailure = 500
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await until(() => expect(host.textContent).toContain('Unable to check your session'))
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(unmounts).toBe(1)
}
async function recoverSession() {
  sessionFailure = 0
  csrf = 'csrf-renewed'
  await click('Retry')
  await until(() => expect(mounts).toBe(2))
}

it('recovers an immutable restore through actual AuthGate 500 unmount only after fresh authority and reset read', async () => {
  await mountAndSubmit()
  await until(() => expect(button('Retry original request').disabled).toBe(false))
  const original = writes[0]
  const originalClaim = owner!.recover(actor)?.claim
  await sessionError()
  let release!: () => void
  resetHold = new Promise((resolve) => {
    release = resolve
  })
  permissionFailure = true
  context.etag = 'f'.repeat(64)
  context.default_rule.policy.money_month = '9.123456789012345678'
  context.default_rule.policy.currency = 'EUR'
  context.default_rule.platform_currency = 'EUR'
  context.limit.stored.ip_ranges.push('198.51.100.0/24')
  await recoverSession()
  await until(() => expect(host.textContent).not.toContain('Restore defaults'))
  expect(writes).toHaveLength(1)
  permissionFailure = false
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['permissions', actor] })
  })
  await until(() => expect(host.textContent).toContain('Restore defaults'))
  await click('Restore defaults')
  expect(document.querySelector('input[aria-label="Reason"]')).toBeNull()
  expect(button('Retry original request').disabled).toBe(true)
  expect(writes).toHaveLength(1)
  await act(async () => release())
  await until(() => expect(button('Retry original request').disabled).toBe(false))
  expect(document.querySelector<HTMLInputElement>('input[aria-label="Reason"]')!.value).toBe(
    'Captured restoration',
  )
  expect(button('Confirm restoration').disabled).toBe(true)
  if (originalClaim) expect(owner!.isCurrent(originalClaim)).toBe(false)
  const recovered = owner!.recover(actor)!
  expect(recovered.kind).toBe('restore-defaults')
  if (recovered.kind !== 'restore-defaults') throw new Error('Expected restore intent')
  expect(recovered.payload.review.limit).not.toHaveProperty('quota_usage')
  expect(recovered.payload.review.limit).not.toHaveProperty('enforced')
  expect(recovered.payload.review).not.toHaveProperty('editable')
  expect(recovered.payload.review.default_rule.policy.money_month).toBe('12.500000000000000001')
  expect(recovered.payload.review.default_rule.policy.currency).toBe('USD')
  expect(recovered.payload.review.limit.stored.ip_ranges).toEqual(['192.0.2.0/24'])
  expect(document.body.textContent).toContain('12.500000000000000001 USD')
  expect(document.body.textContent).not.toContain('9.123456789012345678 EUR')
  writeFailure = 0
  await click('Retry original request')
  await until(() => expect(host.textContent).toContain('Defaults restored and applied'))
  expect(writes).toHaveLength(2)
  expect(writes[1].data).toBe(original.data)
  expect(writes[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  expect(writes[1].headers.get('X-CSRF-Token')).toBe('csrf-renewed')
  expect(owner!.recover(actor)).toBeNull()
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})

it('renews the claim during mounted Session refetch without remount or automatic replay', async () => {
  await mountAndSubmit()
  await until(() => expect(button('Retry original request').disabled).toBe(false))
  const original = writes[0]
  const oldClaim = owner!.recover(actor)!.claim
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await until(() => expect(button('Retry original request').disabled).toBe(false))
  expect(mounts).toBe(1)
  expect(unmounts).toBe(0)
  expect(owner!.isCurrent(oldClaim)).toBe(false)
  expect(writes).toHaveLength(1)
  writeFailure = 0
  await click('Retry original request')
  await until(() => expect(host.textContent).toContain('Defaults restored and applied'))
  expect(writes).toHaveLength(2)
  expect(writes[1].data).toBe(original.data)
  expect(writes[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
})

it('a late response from the unmounted operation cannot clear a renewed in-flight retry', async () => {
  let oldRelease!: () => void
  pendingWrite = new Promise((resolve) => {
    oldRelease = resolve
  })
  writeFailure = 0
  await mountAndSubmit()
  const original = writes[0]
  await sessionError()
  await recoverSession()
  await open()
  await until(() => expect(button('Retry original request').disabled).toBe(false))
  let newRelease!: () => void
  pendingWrite = new Promise((resolve) => {
    newRelease = resolve
  })
  await click('Retry original request')
  await until(() => expect(writes).toHaveLength(2))
  const current = owner!.recover(actor)!.claim
  await act(async () => oldRelease())
  expect(owner!.isCurrent(current)).toBe(true)
  expect(button('Retry original request').disabled).toBe(true)
  expect(document.body.textContent).not.toContain('Defaults restored and applied')
  expect(writes[1].data).toBe(original.data)
  await act(async () => newRelease())
  await until(() => expect(host.textContent).toContain('Defaults restored and applied'))
  expect(owner!.recover(actor)).toBeNull()
})

it.each(['target', 'tab', 'actor', 'logout', 'expiry'] as const)(
  '%s boundary discards submitted intent and never transfers it to the next route owner',
  async (boundary) => {
    await mountAndSubmit()
    await until(() => expect(button('Retry original request').disabled).toBe(false))
    const previousOwner = owner!
    const previousClaim = previousOwner.recover(actor)!.claim
    if (boundary === 'target' || boundary === 'tab') {
      await act(async () => {
        await router.navigate(
          boundary === 'target' ? '/teams/tea_other?tab=limits' : '/teams/tea_test?tab=members',
        )
      })
    } else if (boundary === 'actor') {
      actor = 'usr_other'
      await act(async () => cache.setQueryData(sessionKey, currentSession()))
    } else if (boundary === 'logout') {
      await act(async () => cache.setQueryData(sessionKey, null))
    } else {
      await act(async () => window.dispatchEvent(new Event('routex:session-expired')))
    }
    await until(() => expect(previousOwner.isCurrent(previousClaim)).toBe(false))
    expect(previousOwner.clear(previousClaim)).toBe(false)
    if (boundary === 'logout' || boundary === 'expiry') {
      await until(() => expect(host.textContent).toContain('Signed out'))
      expect(document.querySelector('[role="dialog"]')).toBeNull()
    } else {
      expect(owner!.recover(actor)).toBeNull()
      await open()
      expect(document.querySelector<HTMLInputElement>('input[aria-label="Reason"]')!.value).toBe('')
      expect(document.body.textContent).not.toContain('Retry original request')
    }
    expect(writes).toHaveLength(1)
  },
)
