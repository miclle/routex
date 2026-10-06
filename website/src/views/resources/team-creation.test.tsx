import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import { sessionKey, setupKey } from '@/hooks/use-auth'
import AuthGate from '@/components/app/AuthGate'
import { UncertainIntentProvider } from '@/context/uncertain-intents'
import type { Session } from '@/types/auth'
import type { TeamCreationContext } from '@/types/resources'
import i18n from '@/i18n'
import CreateResourcePage from './create'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let context: TeamCreationContext,
  identity: Session,
  permissions: string[],
  requests: InternalAxiosRequestConfig[],
  statuses: Record<string, number>,
  owners: { id: string; name: string; email: string }[],
  outcome: 'pending' | 'applied' | 'unavailable' | 'superseded',
  invalidReceipt: boolean
let holds: Record<string, (() => Promise<void>) | undefined>
const owner = { id: 'usr_owner', name: 'Current owner', email: 'owner@example.invalid' }
beforeEach(async () => {
  await i18n.changeLanguage('en')
  requests = []
  statuses = {}
  holds = {}
  owners = [owner]
  outcome = 'pending'
  invalidReceipt = false
  identity = {
    user: { id: 'usr_creator', name: 'Creator', email: 'creator@example.invalid', role: 'admin' },
    csrf_token: 'original-csrf',
  }
  permissions = ['teams.write', 'teams.tokens.write', 'teams.money.write', 'teams.rates.write']
  context = {
    review_etag: 'a'.repeat(64),
    default_rule_etag: 'b'.repeat(64),
    can_set_models: false,
    platform_currency: 'USD',
    editable_fields: [
      'tokens_5h',
      'tokens_7d',
      'tokens_month',
      'money_month',
      'rpm',
      'tpm',
      'concurrency',
    ],
    default_policy: {
      tokens_5h: '1000000',
      tokens_7d: null,
      tokens_month: '9007199254740991',
      money_month: '10.000000000000000001',
      currency: 'USD',
      rpm: '20',
      tpm: '50000',
      concurrency: '3',
    },
  }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  cache.setQueryData(sessionKey, identity)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: statuses[config.url!] || 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/setup') response.data = { initialized: true }
    else if (config.url === '/auth/session') response.data = structuredClone(identity)
    else if (config.url === '/auth/permissions') response.data = { permissions: [...permissions] }
    else if (config.url === '/admin/teams/creation-context') {
      response.data = structuredClone(context)
      response.headers.set('ETag', `"${context.review_etag}"`)
    } else if (config.url === '/admin/team-member-candidates')
      response.data = { items: structuredClone(owners) }
    else if (config.url === '/admin/teams/creation-model-candidates')
      response.data = {
        items: config.params.cursor
          ? [
              {
                id: 'mdl_second',
                name: 'Second Model',
                providers: permissions.includes('providers.read') ? ['Visible Provider'] : null,
                protocols: ['openai_responses'],
              },
            ]
          : [
              {
                id: 'mdl_first',
                name: 'First Model',
                providers: permissions.includes('providers.read') ? ['Visible Provider'] : null,
                protocols: ['openai_chat'],
              },
            ],
        next_cursor: config.params.cursor ? null : 'mdl_first',
      }
    else if (config.url === '/admin/teams/creation-model-review')
      response.data = {
        model_ids: JSON.parse(config.data).model_ids.toSorted(),
        model_review_token: 'f'.repeat(64),
      }
    else if (config.url === '/admin/teams' && config.method === 'post') {
      const body = JSON.parse(config.data)
      response.data = {
        team:
          outcome === 'unavailable'
            ? null
            : {
                id: 'tea_created',
                name: body.name,
                description: body.description,
                status: 'active',
                created_at: '2026-10-06T03:00:00Z',
                model_ids: body.model_ids ?? [],
                members: body.owner_ids.map((id: string) => ({
                  id: 'tme_' + id,
                  user_id: id,
                  name: 'Owner',
                  email: 'owner@example.invalid',
                  role: 'owner',
                  status: 'active',
                })),
              },
        receipt: {
          creation_id: body.creation_id,
          team_id: 'tea_created',
          created_at: '2026-10-06T03:00:00Z',
        },
        committed: !invalidReceipt,
        runtime_applied: outcome === 'applied',
        application_status: outcome,
      }
    }
    await holds[config.url!]?.()
    if (response.status >= 400)
      throw new AxiosError('Controlled rejection', '', config, undefined, response)
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
async function until(check: () => void) {
  for (let n = 0; n < 100; n++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    try {
      check()
      return
    } catch (error) {
      if (n === 99) throw error
    }
  }
}
async function mount(withGate = false) {
  cache.setQueryData(setupKey, { initialized: true })
  const routes = [
    { path: '/admin/teams/new', element: <CreateResourcePage kind="teams" admin /> },
    { path: '/admin/teams/:id', element: <p>Created Team destination</p> },
    { path: '/admin/teams', element: <p>Team list</p> },
  ]
  router = createMemoryRouter(
    withGate
      ? [
          {
            element: (
              <UncertainIntentProvider>
                <AuthGate mode="private" />
              </UncertainIntentProvider>
            ),
            children: routes,
          },
          { path: '/login', element: <p>Signed out</p> },
        ]
      : routes,
    { initialEntries: ['/admin/teams/new'] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.querySelector('[name="name"]')).toBeTruthy())
  await until(() => expect(host.textContent).toContain('Current owner'))
}
async function fill(selector: string, value: string) {
  const input = host.querySelector<HTMLInputElement | HTMLTextAreaElement>(selector)!
  expect(input).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      input instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype,
      'value',
    )!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function click(text: string, from: ParentNode = document) {
  const button = [...from.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === text,
  )!
  expect(button).toBeTruthy()
  await act(async () => button.click())
}
async function prepare() {
  await fill('[name="name"]', 'Created Team')
  await act(async () => host.querySelector<HTMLButtonElement>('[role="switch"]')!.click())
}
async function submit() {
  await act(async () =>
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeTruthy())
  await click('Confirm creation')
}
function posts() {
  return requests.filter((request) => request.method === 'post' && request.url === '/admin/teams')
}
async function refresh(prefix: string) {
  await act(async () => {
    void cache.invalidateQueries({ queryKey: [prefix] })
  })
  await until(() => expect(host.querySelector('[name="name"]')).toBeTruthy())
}
function deferred() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  return { release, promise }
}

describe('reviewed Team creation and immutable intent', () => {
  it('defaults to English and composes the 960px Basic information/owner/limits page', async () => {
    await mount()
    expect(i18n.language).toBe('en')
    expect(host.querySelector('.max-w-\\[960px\\]')).toBeTruthy()
    expect(host.textContent).toContain('Resource limits')
    expect(
      (host.querySelector('[aria-label="Monthly limit (M Tokens)"]') as HTMLInputElement).value,
    ).toBe('9007199254.740991')
    expect(
      (host.querySelector('[aria-label="Monthly budget USD"]') as HTMLInputElement).value,
    ).toBe('10.000000000000000001')
    expect(host.textContent).not.toContain('Model access')
  })
  it('copies defaults by omission with explicit owners and returns only a saved receipt while pending', async () => {
    await mount()
    await prepare()
    await submit()
    await until(() => expect(posts()).toHaveLength(1))
    expect(JSON.parse(posts()[0].data)).toMatchObject({
      name: 'Created Team',
      description: '',
      owner_ids: ['usr_owner'],
    })
    expect(JSON.parse(posts()[0].data)).not.toHaveProperty('initial_limits')
    expect(host.textContent).toContain('Current runtime application is not confirmed')
    expect(router.state.location.pathname).toBe('/admin/teams/new')
  })
  it('submits exact decimal money, integral M Tokens and explicit null/zero only when changed', async () => {
    await mount()
    await prepare()
    await fill('[aria-label="Monthly budget USD"]', '12.500000000000000001')
    await fill('[aria-label="Five-hour limit (M Tokens)"]', '0.000001')
    await fill('[aria-label="Seven-day limit (M Tokens)"]', '1')
    await fill('[aria-label="Seven-day limit (M Tokens)"]', '')
    await fill('[aria-label="RPM"]', '0')
    await fill('textarea:not([name])', 'Initial exact budget')
    await submit()
    expect(JSON.parse(posts()[0].data).initial_limits).toEqual({
      reason: 'Initial exact budget',
      money_month: '12.500000000000000001',
      currency: 'USD',
      tokens_5h: 1,
      tokens_7d: null,
      rpm: 0,
    })
  })
  it('requires a reason for overrides and never dispatches unsafe values', async () => {
    await mount()
    await prepare()
    await fill('[aria-label="Monthly limit (M Tokens)"]', '9007199254.740992')
    await fill('textarea:not([name])', 'Initial')
    await act(async () =>
      host
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    expect(host.textContent).toContain('Enter a valid Team name')
    expect(posts()).toHaveLength(0)
    await fill('[aria-label="Monthly limit (M Tokens)"]', '1')
    await fill('textarea:not([name])', '')
    await act(async () =>
      host
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    expect(posts()).toHaveLength(0)
  })
  it('supports write-only creator without fetching or displaying hidden default numbers', async () => {
    permissions = ['teams.write']
    context = { ...context, editable_fields: [], platform_currency: null, default_policy: {} }
    await mount()
    expect(host.textContent).toContain('Current default will be applied')
    expect(host.querySelector('[aria-label="RPM"]')).toBeNull()
    expect(host.textContent).not.toContain('50000')
    await prepare()
    await submit()
    expect(JSON.parse(posts()[0].data)).not.toHaveProperty('initial_limits')
  })
  it.each(['tokens', 'money', 'rates'])('keeps independent %s preview authority', async (group) => {
    const fields =
      group === 'tokens'
        ? ['tokens_5h', 'tokens_7d', 'tokens_month']
        : group === 'money'
          ? ['money_month']
          : ['rpm', 'tpm', 'concurrency']
    permissions = ['teams.write', `teams.${group}.write`]
    const policy = Object.fromEntries(
      fields.map((field) => [
        field,
        context.default_policy[field as keyof typeof context.default_policy],
      ]),
    )
    context = {
      ...context,
      editable_fields: fields as TeamCreationContext['editable_fields'],
      default_policy: { ...policy, ...(group === 'money' ? { currency: 'USD' } : {}) },
      platform_currency: group === 'money' ? 'USD' : null,
    }
    await mount()
    expect(host.querySelectorAll('input[inputmode="decimal"]')).toHaveLength(fields.length)
  })
  it.each([400, 403, 409, 412, 428, 500, 503])(
    'retains original intent after first dispatched %s and rejects matching GET as success',
    async (status) => {
      statuses['/admin/teams'] = status
      await mount()
      await prepare()
      await submit()
      await until(() => expect(host.textContent).toContain('creation outcome remains unknown'))
      const original = posts()[0].data,
        etag = posts()[0].headers.get('If-Match')
      context = { ...context, review_etag: 'c'.repeat(64) }
      await refresh('team-creation-context')
      expect(host.textContent).toContain('Retry original creation')
      expect(host.textContent).not.toContain('The Team is saved')
      await click('Retry original creation')
      await until(() => expect(posts()).toHaveLength(2))
      expect(posts()[1].data).toBe(original)
      expect(posts()[1].headers.get('If-Match')).toBe(etag)
      expect(host.textContent).toContain('creation outcome remains unknown')
    },
  )
  it('preserves intent through Cancel/reopen and live Chinese without rewriting body', async () => {
    statuses['/admin/teams'] = 409
    await mount()
    await prepare()
    await submit()
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    const original = posts()[0].data
    await click('Cancel')
    expect(host.querySelector('form')).toBeNull()
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    await click('继续创建')
    expect((host.querySelector('[name="name"]') as HTMLInputElement).value).toBe('Created Team')
    await click('重试原创建请求')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].data).toBe(original)
  })
  it('explicitly abandons local retries without claiming rollback and creates a new reviewed UUID', async () => {
    statuses['/admin/teams'] = 409
    await mount()
    await prepare()
    await submit()
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    const original = JSON.parse(posts()[0].data)
    await click('Abandon local retries')
    expect(document.body.textContent).toContain('does not undo a saved Team')
    await click('Abandon and review a new request')
    statuses['/admin/teams'] = 0
    context = { ...context, review_etag: 'c'.repeat(64) }
    await refresh('team-creation-context')
    await click('Review current defaults')
    await submit()
    await until(() => expect(posts()).toHaveLength(2))
    expect(JSON.parse(posts()[1].data).creation_id).not.toBe(original.creation_id)
    expect(posts()[1].headers.get('If-Match')).toBe('"' + 'c'.repeat(64) + '"')
  })
  it('hides private drafts on same-user Session renewal and retries original intent with fresh CSRF', async () => {
    statuses['/admin/teams'] = 503
    await mount()
    await prepare()
    await submit()
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    const original = posts()[0].data
    const pending = deferred()
    holds['/auth/session'] = () => pending.promise
    identity = { ...identity, csrf_token: 'renewed-csrf' }
    await act(async () => {
      void cache.invalidateQueries({ queryKey: sessionKey })
    })
    expect(host.querySelector('form')).toBeNull()
    expect(host.textContent).not.toContain('Created Team')
    pending.release()
    delete holds['/auth/session']
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    await click('Retry original creation')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].data).toBe(original)
    expect(posts()[1].headers.get('X-CSRF-Token')).toBe('renewed-csrf')
  })
  it('does not navigate or restore a receipt from an obsolete late response after renewed authority', async () => {
    await mount()
    await prepare()
    const pending = deferred()
    outcome = 'applied'
    holds['/admin/teams'] = () => pending.promise
    await submit()
    await until(() => expect(posts()).toHaveLength(1))
    await act(async () => {
      void cache.invalidateQueries({ queryKey: sessionKey })
    })
    await until(() => expect(host.querySelector('form')).toBeTruthy())
    pending.release()
    delete holds['/admin/teams']
    await until(() =>
      expect(
        [...host.querySelectorAll('button')].find(
          (button) => button.textContent === 'Retry original creation',
        )?.disabled,
      ).toBe(false),
    )
    expect(router.state.location.pathname).toBe('/admin/teams/new')
    expect(host.textContent).toContain('creation outcome remains unknown')
    expect(host.textContent).not.toContain('The Team is saved')
  })
  it('prevents double creation clicks while one original request owns busy', async () => {
    await mount()
    await prepare()
    const pending = deferred()
    holds['/admin/teams'] = () => pending.promise
    await submit()
    await until(() => expect(posts()).toHaveLength(1))
    const retry = [...host.querySelectorAll('button')].find(
      (button) => button.textContent === 'Retry original creation',
    )!
    await act(async () => {
      retry.click()
      retry.click()
    })
    expect(posts()).toHaveLength(1)
    pending.release()
    delete holds['/admin/teams']
    await until(() => expect(retry.disabled).toBe(false))
  })
  it('fresh permission error hides all private controls without clearing the immutable request', async () => {
    statuses['/admin/teams'] = 409
    await mount()
    await prepare()
    await submit()
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    const original = posts()[0].data
    statuses['/auth/permissions'] = 403
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['team-creation-permissions'] })
    })
    await until(() => expect(host.querySelector('form')).toBeNull())
    expect(host.textContent).not.toContain('Current owner')
    statuses['/auth/permissions'] = 0
    await refresh('team-creation-permissions')
    await click('Retry original creation')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].data).toBe(original)
  })
  it('rejects stale default review even when visible values have not changed', async () => {
    await mount()
    await prepare()
    context = { ...context, review_etag: 'c'.repeat(64) }
    await refresh('team-creation-context')
    expect(host.textContent).toContain('Defaults or currency changed')
    await act(async () =>
      host
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    expect(posts()).toHaveLength(0)
    await click('Review current defaults')
    await submit()
    expect(posts()[0].headers.get('If-Match')).toBe('"' + 'c'.repeat(64) + '"')
  })
  it('requires current currency review and preserves the exact money draft', async () => {
    await mount()
    await prepare()
    await fill('[aria-label="Monthly budget USD"]', '0.000000000000000001')
    await fill('textarea:not([name])', 'Currency reviewed')
    context = { ...context, review_etag: 'c'.repeat(64), platform_currency: 'EUR' }
    await refresh('team-creation-context')
    await click('Review current defaults')
    expect(
      (host.querySelector('[aria-label="Monthly budget EUR"]') as HTMLInputElement).value,
    ).toBe('0.000000000000000001')
    await submit()
    expect(JSON.parse(posts()[0].data).initial_limits).toEqual({
      money_month: '0.000000000000000001',
      currency: 'EUR',
      reason: 'Currency reviewed',
    })
  })
  it('does not treat malformed committed response as success', async () => {
    invalidReceipt = true
    outcome = 'applied'
    await mount()
    await prepare()
    await submit()
    await until(() => expect(host.textContent).toContain('creation outcome remains unknown'))
    expect(router.state.location.pathname).toBe('/admin/teams/new')
    expect(host.textContent).toContain('Retry original creation')
  })
  it('only navigates after current authorized applied response and destroys intent on actor change', async () => {
    await mount()
    await prepare()
    statuses['/admin/teams'] = 409
    await submit()
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    identity = { ...identity, user: { ...identity.user, id: 'usr_other' } }
    statuses['/auth/session'] = 0
    await act(async () => cache.setQueryData(sessionKey, identity))
    await until(() => expect(host.querySelector('[name="name"]')).toBeTruthy())
    expect((host.querySelector('[name="name"]') as HTMLInputElement).value).toBe('')
    expect(host.textContent).not.toContain('Retry original creation')
    await prepare()
    statuses['/admin/teams'] = 0
    outcome = 'applied'
    await submit()
    await until(() => expect(router.state.location.pathname).toBe('/admin/teams/tea_created'))
  })
  it('destroys local private request after logout rather than restoring it under the next login', async () => {
    statuses['/admin/teams'] = 409
    await mount()
    await prepare()
    await submit()
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    statuses['/auth/session'] = 401
    await act(async () => cache.removeQueries({ queryKey: sessionKey }))
    expect(host.querySelector('form')).toBeNull()
    statuses['/auth/session'] = 0
    await act(async () => cache.setQueryData(sessionKey, identity))
    await until(() => expect(host.querySelector('[name="name"]')).toBeTruthy())
    expect((host.querySelector('[name="name"]') as HTMLInputElement).value).toBe('')
    expect(host.textContent).not.toContain('Retry original creation')
  })
  it('Escape closes an undispatched confirmation without losing the draft', async () => {
    await mount()
    await prepare()
    await act(async () =>
      host
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeTruthy())
    await act(async () =>
      document
        .querySelector('[role="dialog"]')!
        .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect((host.querySelector('[name="name"]') as HTMLInputElement).value).toBe('Created Team')
    expect(posts()).toHaveLength(0)
  })
  it('retains a saved pending receipt and retries exactly until a current applied result', async () => {
    await mount()
    await prepare()
    await submit()
    await until(() =>
      expect(host.textContent).toContain('Current runtime application is not confirmed'),
    )
    const original = posts()[0].data
    outcome = 'applied'
    await click('Retry original creation')
    await until(() => expect(router.state.location.pathname).toBe('/admin/teams/tea_created'))
    expect(posts()[1].data).toBe(original)
  })
  it.each(['superseded', 'unavailable'] as const)(
    'does not navigate from committed %s',
    async (state) => {
      outcome = state
      await mount()
      await prepare()
      await submit()
      await until(() =>
        expect(host.textContent).toContain(
          state === 'superseded' ? 'initial configuration has changed' : 'currently unavailable',
        ),
      )
      expect(router.state.location.pathname).toBe('/admin/teams/new')
      expect(host.textContent).toContain('Retry original creation')
    },
  )
  it('keeps an override intent while its dimension permission is revoked, without exposing its values or dispatching', async () => {
    statuses['/admin/teams'] = 409
    await mount()
    await prepare()
    await fill('[aria-label="Monthly budget USD"]', '12.500000000000000001')
    await fill('textarea:not([name])', 'Original money reason')
    await submit()
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    permissions = ['teams.write']
    context = { ...context, editable_fields: [], platform_currency: null, default_policy: {} }
    await refresh('team-creation-permissions')
    await refresh('team-creation-context')
    expect(host.querySelector('[aria-label="Monthly budget USD"]')).toBeNull()
    const retry = [...host.querySelectorAll('button')].find(
      (button) => button.textContent === 'Retry original creation',
    )!
    expect(retry.disabled).toBe(true)
    await act(async () => retry.click())
    expect(posts()).toHaveLength(1)
    expect(host.textContent).toContain('creation outcome remains unknown')
  })
  it('can discard unauthorized undispatched overrides explicitly while preserving Basic information', async () => {
    await mount()
    await prepare()
    await fill('[aria-label="RPM"]', '30')
    await fill('textarea:not([name])', 'Rate reason')
    permissions = ['teams.write']
    context = {
      ...context,
      review_etag: 'c'.repeat(64),
      editable_fields: [],
      platform_currency: null,
      default_policy: {},
    }
    await refresh('team-creation-permissions')
    await refresh('team-creation-context')
    expect(host.querySelector('[aria-label="RPM"]')).toBeNull()
    expect(host.textContent).toContain('An override permission is unavailable')
    await click('Use current defaults for all fields')
    await click('Review current defaults')
    await submit()
    expect(JSON.parse(posts()[0].data)).not.toHaveProperty('initial_limits')
    expect(JSON.parse(posts()[0].data).name).toBe('Created Team')
  })
  it('does not replace stale owner candidates after query change or dispatch through cached options', async () => {
    await mount()
    const old = host.querySelector<HTMLElement>('[role="switch"]')!
    const pending = deferred()
    holds['/admin/team-member-candidates'] = () => pending.promise
    owners = []
    await fill('[aria-label="Search Team owners"]', 'new query')
    expect(host.querySelector('[role="switch"]')).toBeNull()
    await act(async () => old.click())
    pending.release()
    delete holds['/admin/team-member-candidates']
    await until(() => expect(host.textContent).toContain('No data available'))
    expect(host.querySelector('[aria-label="Current selection"]')).toBeNull()
    expect(posts()).toHaveLength(0)
  })
  it('bounds literal owner search by bytes without deleting the user draft', async () => {
    await mount()
    const count = requests.filter(
      (request) => request.url === '/admin/team-member-candidates',
    ).length
    const input = '界'.repeat(67)
    await fill('[aria-label="Search Team owners"]', input)
    expect(
      (host.querySelector('[aria-label="Search Team owners"]') as HTMLInputElement).value,
    ).toBe(input)
    expect(host.textContent).toContain('at most 200 UTF-8 bytes')
    expect(host.querySelector('[role="switch"]')).toBeNull()
    expect(
      requests.filter((request) => request.url === '/admin/team-member-candidates'),
    ).toHaveLength(count)
    await fill('[aria-label="Search Team owners"]', '%_')
    await until(() => expect(host.querySelector('[role="switch"]')).toBeTruthy())
    expect(
      requests.filter((request) => request.url === '/admin/team-member-candidates').at(-1)?.params
        .q,
    ).toBe('%_')
  })
  it('denied creation authority performs no context or owner reads', async () => {
    permissions = []
    router = createMemoryRouter(
      [{ path: '/', element: <CreateResourcePage kind="teams" admin /> }],
      { initialEntries: ['/'] },
    )
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await until(() =>
      expect(host.textContent).toContain('Current Team creation authority is unavailable'),
    )
    expect(host.querySelector('form')).toBeNull()
    expect(requests.some((request) => request.url?.includes('/admin/'))).toBe(false)
  })
  it('reuses the current create trigger for Escape focus and skips hidden authority', async () => {
    await mount()
    await prepare()
    const trigger = [...host.querySelectorAll('button')].find(
      (button) => button.textContent === 'Create Team',
    )!
    await act(async () => {
      trigger.focus()
      host
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeTruthy())
    await act(async () =>
      document
        .querySelector('[role="dialog"]')!
        .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await until(() => expect(document.activeElement).toBe(trigger))
    await act(async () =>
      host
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeTruthy())
    const pending = deferred()
    holds['/auth/permissions'] = () => pending.promise
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['team-creation-permissions'] })
    })
    expect(host.querySelector('form')).toBeNull()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).not.toBe(trigger)
    pending.release()
    delete holds['/auth/permissions']
  })
  it('shows stored money denomination for untouched default and current denomination for an explicit override', async () => {
    context = { ...context, platform_currency: 'EUR' }
    await mount()
    await prepare()
    expect(
      (host.querySelector('[aria-label="Monthly budget USD"]') as HTMLInputElement).value,
    ).toBe('10.000000000000000001')
    await fill('[aria-label="Monthly budget USD"]', '0.000000000000000001')
    await fill('textarea:not([name])', 'New current currency')
    expect(host.querySelector('[aria-label="Monthly budget EUR"]')).toBeTruthy()
    await submit()
    expect(JSON.parse(posts()[0].data).initial_limits.currency).toBe('EUR')
  })
  it('retains selections outside a later owner search page', async () => {
    await mount()
    await prepare()
    owners = [{ id: 'usr_other', name: 'Other owner', email: 'other@example.invalid' }]
    await fill('[aria-label="Search Team owners"]', 'other')
    await until(() => expect(host.textContent).toContain('Other owner'))
    await submit()
    expect(JSON.parse(posts()[0].data).owner_ids).toEqual(['usr_owner'])
  })
  it('uses code-point name bounds and preserves description/newlines and draft while normalizing only dispatched name', async () => {
    await mount()
    await prepare()
    await fill('[name="name"]', '  ' + '😀'.repeat(100) + '  ')
    await fill('[name="description"]', 'Recorded\ndescription\t')
    await submit()
    expect(JSON.parse(posts()[0].data).name).toBe('😀'.repeat(100))
    expect(JSON.parse(posts()[0].data).description).toBe('Recorded\ndescription\t')
    expect((host.querySelector('[name="name"]') as HTMLInputElement).value.startsWith('  ')).toBe(
      true,
    )
  })
})

describe('Team submitted intent across the real private AuthGate boundary', () => {
  async function outage() {
    statuses['/auth/session'] = 500
    await act(async () => {
      void cache.invalidateQueries({ queryKey: sessionKey })
    })
    await until(() => expect(host.textContent).toContain('Unable to check your session'))
    expect(host.querySelector('form')).toBeNull()
    expect(host.textContent).not.toContain('Created Team')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  }
  async function recover() {
    delete statuses['/auth/session']
    identity = { ...identity, csrf_token: 'recovered-current-csrf' }
    await click('Retry')
    await until(() => expect(host.querySelector('[name="name"]')).toBeTruthy())
    await until(() => expect(host.textContent).toContain('Current owner'))
  }
  it('remounts an idle uncertain submitted request after Session500 and requires explicit original retry with current CSRF', async () => {
    statuses['/admin/teams'] = 409
    await mount(true)
    await prepare()
    await fill('[name="description"]', 'Original description')
    await fill('[aria-label="Monthly budget USD"]', '9.000000000000000001')
    await fill('[aria-label="Monthly limit (M Tokens)"]', '0.000002')
    await fill('textarea:not([name])', 'Exact submitted reason')
    await submit()
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    const original = posts()[0]
    await outage()
    context = { ...context, review_etag: 'c'.repeat(64), platform_currency: 'EUR' }
    await recover()
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    expect(posts()).toHaveLength(1)
    expect(host.textContent).toContain('Original default not retained')
    expect(host.querySelector('[aria-label="RPM"]')).toBeNull()
    expect(host.textContent).toContain('creation outcome remains unknown')
    expect(host.querySelector<HTMLInputElement>('[name="name"]')!.value).toBe('Created Team')
    expect(host.querySelector<HTMLTextAreaElement>('[name="description"]')!.value).toBe(
      'Original description',
    )
    expect(host.querySelector<HTMLInputElement>('[aria-label="Monthly budget USD"]')!.value).toBe(
      '9.000000000000000001',
    )
    expect(host.textContent).toContain('Monthly budget USD')
    statuses['/admin/teams'] = 412
    await click('Retry original creation')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].data).toBe(original.data)
    expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(posts()[1].headers.get('X-CSRF-Token')).toBe('recovered-current-csrf')
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    await outage()
    await recover()
    expect(posts()).toHaveLength(2)
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(host.textContent).toContain('未保留原默认值')
    delete statuses['/admin/teams']
    outcome = 'applied'
    await click(i18n.t('resources:teamCreation.retry'))
    await until(() => expect(router.state.location.pathname).toBe('/admin/teams/tea_created'))
    expect(posts()[2].data).toBe(original.data)
    await act(async () => {
      await router.navigate('/admin/teams/new')
    })
    await until(() => expect(host.querySelector<HTMLInputElement>('[name="name"]')?.value).toBe(''))
    expect(host.textContent).not.toContain('Retry original creation')
  })
  it('refreshes the retained claim for a mounted same-actor Session refetch before explicit pending-intent retry and applied completion', async () => {
    outcome = 'pending'
    await mount(true)
    await prepare()
    await submit()
    await until(() => expect(host.textContent).toContain('The Team is saved'))
    const original = posts()[0]
    const renewed = deferred()
    holds['/auth/session'] = () => renewed.promise
    identity = { ...identity, csrf_token: 'mounted-renewed-csrf' }
    await act(async () => {
      void cache.invalidateQueries({ queryKey: sessionKey })
    })
    expect(host.querySelector('form')).toBeNull()
    renewed.release()
    delete holds['/auth/session']
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    expect(host.textContent).toContain('creation outcome remains unknown')
    outcome = 'applied'
    await click('Retry original creation')
    await until(() => expect(router.state.location.pathname).toBe('/admin/teams/tea_created'))
    expect(posts()).toHaveLength(2)
    expect(posts()[1].data).toBe(original.data)
    expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(posts()[1].headers.get('X-CSRF-Token')).toBe('mounted-renewed-csrf')
    await act(async () => {
      await router.navigate('/admin/teams/new')
    })
    await until(() => expect(host.querySelector<HTMLInputElement>('[name="name"]')?.value).toBe(''))
    expect(host.textContent).not.toContain('Retry original creation')
  })
  it('keeps recovered values hidden without their dimension authority and never retries automatically after restored grants', async () => {
    statuses['/admin/teams'] = 503
    await mount(true)
    await prepare()
    await fill('[aria-label="Monthly budget USD"]', '9.000000000000000001')
    await fill('textarea:not([name])', 'Original override reason')
    await submit()
    const original = posts()[0]
    await outage()
    permissions = ['teams.write']
    context = { ...context, editable_fields: [], default_policy: {}, platform_currency: null }
    await recover()
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    expect(host.textContent).not.toContain('9.000000000000000001')
    expect(host.querySelector('[aria-label="Monthly budget USD"]')).toBeNull()
    expect(
      [...host.querySelectorAll('button')].find((x) => x.textContent === 'Retry original creation')
        ?.disabled,
    ).toBe(true)
    expect(posts()).toHaveLength(1)
    permissions = ['teams.write', 'teams.money.write']
    context = {
      ...context,
      editable_fields: ['money_month'],
      default_policy: { money_month: null, currency: '' },
      platform_currency: 'USD',
    }
    await refresh('team-creation-permissions')
    await refresh('team-creation-context')
    await until(() =>
      expect(
        [...host.querySelectorAll('button')].find(
          (x) => x.textContent === 'Retry original creation',
        )?.disabled,
      ).toBe(false),
    )
    expect(posts()).toHaveLength(1)
    await click('Retry original creation')
    await until(() => expect(posts()).toHaveLength(2))
    expect(posts()[1].data).toBe(original.data)
  })
  it('does not retain any undispatched draft across AuthGate error unmount', async () => {
    await mount(true)
    await prepare()
    await outage()
    await recover()
    expect(host.querySelector<HTMLInputElement>('[name="name"]')!.value).toBe('')
    expect(host.textContent).not.toContain('Retry original creation')
    expect(posts()).toHaveLength(0)
  })
  it('waits for independently fresh permissions and context before exposing recovered submitted fields', async () => {
    statuses['/admin/teams'] = 503
    await mount(true)
    await prepare()
    await submit()
    await outage()
    const permissionRead = deferred()
    holds['/auth/permissions'] = () => permissionRead.promise
    delete statuses['/auth/session']
    await click('Retry')
    await until(() => expect(cache.getQueryState(sessionKey)?.status).toBe('success'))
    expect(host.querySelector('form')).toBeNull()
    expect(host.textContent).not.toContain('Created Team')
    const contextRead = deferred()
    holds['/admin/teams/creation-context'] = () => contextRead.promise
    permissionRead.release()
    delete holds['/auth/permissions']
    await until(() =>
      expect(requests.filter((r) => r.url === '/admin/teams/creation-context')).toHaveLength(2),
    )
    expect(host.querySelector('form')).toBeNull()
    contextRead.release()
    delete holds['/admin/teams/creation-context']
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    expect(posts()).toHaveLength(1)
  })
  it('does not treat a late applied response from an unmounted request as recovery or clear a newer abandoned intent', async () => {
    await mount(true)
    await prepare()
    const oldResponse = deferred()
    outcome = 'applied'
    holds['/admin/teams'] = () => oldResponse.promise
    await submit()
    const oldBody = posts()[0].data
    await outage()
    delete holds['/admin/teams']
    await recover()
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    expect(router.state.location.pathname).toBe('/admin/teams/new')
    await click('Abandon local retries')
    await click('Abandon and review a new request')
    outcome = 'pending'
    await until(() => expect(host.querySelector('form')).toBeTruthy())
    await submit()
    await until(() => expect(posts()).toHaveLength(2))
    const newBody = posts()[1].data
    expect(newBody).not.toBe(oldBody)
    oldResponse.release()
    await until(() => expect(host.textContent).toContain('The Team is saved'))
    expect(router.state.location.pathname).toBe('/admin/teams/new')
    await outage()
    await recover()
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    await click('Retry original creation')
    await until(() => expect(posts()).toHaveLength(3))
    expect(posts()[2].data).toBe(newBody)
  })
  it.each(['route', 'tab', 'actor', 'logout', 'expiry', 'unauthorized'] as const)(
    'destroys a submitted intent after definitive %s teardown',
    async (kind) => {
      statuses['/admin/teams'] = 503
      await mount(true)
      await prepare()
      await submit()
      await until(() => expect(host.textContent).toContain('Retry original creation'))
      if (kind === 'route')
        await act(async () => {
          await router.navigate('/admin/teams')
        })
      else if (kind === 'tab')
        await act(async () => {
          await router.navigate('/admin/teams/new?tab=other')
        })
      else if (kind === 'actor') {
        identity = { ...identity, user: { ...identity.user, id: 'usr_different' } }
        await act(async () => {
          cache.setQueryData(sessionKey, identity)
        })
      } else if (kind === 'logout')
        await act(async () => {
          cache.setQueryData(sessionKey, null)
        })
      else if (kind === 'expiry')
        await act(async () => {
          window.dispatchEvent(new Event('routex:session-expired'))
        })
      else {
        statuses['/auth/session'] = 401
        await act(async () => {
          void cache.invalidateQueries({ queryKey: sessionKey })
        })
      }
      await until(() => expect(host.textContent).not.toContain('Retry original creation'))
      delete statuses['/auth/session']
      await act(async () => {
        cache.setQueryData(sessionKey, identity)
        await router.navigate('/admin/teams/new')
      })
      await until(() =>
        expect(host.querySelector<HTMLInputElement>('[name="name"]')?.value).toBe(''),
      )
      expect(host.textContent).not.toContain('Retry original creation')
      expect(posts()).toHaveLength(1)
    },
  )
})

describe('Team creation initial Model selection', () => {
  function enableModels() {
    context = { ...context, can_set_models: true }
    permissions.push('teams.models.write', 'providers.read')
  }
  async function selectFirst() {
    const input = host.querySelector<HTMLInputElement>('[aria-label="Model access"]')!
    await act(async () => {
      input.focus()
      input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
    })
    await until(() =>
      expect(
        [...document.querySelectorAll('[role="option"]')].some((n) =>
          n.textContent?.includes('First Model'),
        ),
      ).toBe(true),
    )
    await act(async () =>
      [...document.querySelectorAll<HTMLElement>('[role="option"]')]
        .find((n) => n.textContent?.includes('First Model'))!
        .click(),
    )
  }
  it('omits the picker and candidate request without independent server and platform Model authority', async () => {
    await mount()
    expect(host.querySelector('[aria-label="Model access"]')).toBeNull()
    expect(requests.some((r) => r.url?.includes('creation-model-'))).toBe(false)
    context = { ...context, can_set_models: true }
    await refresh('team-creation-context')
    expect(host.querySelector('[aria-label="Model access"]')).toBeNull()
    expect(requests.some((r) => r.url?.includes('creation-model-'))).toBe(false)
  })
  it('reviews selected Models before confirmation and creates only after explicit confirmation', async () => {
    enableModels()
    await mount()
    await prepare()
    await selectFirst()
    await click('More Models')
    await until(() =>
      expect(
        [...document.querySelectorAll('[role="option"]')].some((n) =>
          n.textContent?.includes('Second Model'),
        ),
      ).toBe(true),
    )
    expect(host.querySelector('[aria-label="Remove First Model"]')).toBeNull()
    expect(host.querySelector('[aria-label="Remove mdl_first"]')).toBeTruthy()
    await act(async () =>
      [...document.querySelectorAll<HTMLElement>('[role="option"]')]
        .find((n) => n.textContent?.includes('Second Model'))!
        .click(),
    )
    await act(async () =>
      host
        .querySelector('input[aria-label="Model access"]')!
        .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    await act(async () =>
      host
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeTruthy())
    expect(posts()).toHaveLength(0)
    const review = requests.find((r) => r.url === '/admin/teams/creation-model-review')!
    expect(JSON.parse(review.data)).toEqual({ model_ids: ['mdl_first', 'mdl_second'] })
    expect(review.headers.get('If-Match')).toBe(`"${context.review_etag}"`)
    await click('Confirm creation')
    await until(() => expect(posts()).toHaveLength(1))
    expect(JSON.parse(posts()[0].data)).toMatchObject({
      model_ids: ['mdl_first', 'mdl_second'],
      model_review_token: 'f'.repeat(64),
    })
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((q) => q.state.data),
      ),
    ).not.toContain('model_review_token')
  })
  it('preserves selected IDs after review failure and never dispatches an unreviewed create', async () => {
    enableModels()
    statuses['/admin/teams/creation-model-review'] = 409
    await mount()
    await prepare()
    await selectFirst()
    await act(async () =>
      host
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    await until(() =>
      expect(host.textContent).toContain('This action conflicts with current state'),
    )
    expect(posts()).toHaveLength(0)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(host.querySelector('[aria-label="Remove First Model"]')).toBeTruthy()
  })
  it('requires explicit current-context review after a selected-review409 without changing the selected IDs', async () => {
    enableModels()
    statuses['/admin/teams/creation-model-review'] = 409
    const reviewing = deferred()
    holds['/admin/teams/creation-model-review'] = () => reviewing.promise
    await mount()
    await prepare()
    await selectFirst()
    await act(async () =>
      host
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    context = { ...context, review_etag: 'c'.repeat(64) }
    delete holds['/admin/teams/creation-model-review']
    reviewing.release()
    await until(() => expect(host.textContent).toContain('Defaults or currency changed'))
    expect(posts()).toHaveLength(0)
    expect(host.querySelector('[aria-label="Remove First Model"]')).toBeTruthy()
    expect(
      [...host.querySelectorAll<HTMLButtonElement>('button')].find(
        (b) => b.textContent === 'Create Team',
      )!.disabled,
    ).toBe(true)
    delete statuses['/admin/teams/creation-model-review']
    await click('Review current defaults')
    await submit()
    await until(() => expect(posts()).toHaveLength(1))
    const reviews = requests.filter((r) => r.url === '/admin/teams/creation-model-review')
    expect(reviews[1].headers.get('If-Match')).toBe(`"${context.review_etag}"`)
    expect(JSON.parse(posts()[0].data).model_ids).toEqual(['mdl_first'])
  })
  it('hides cached Provider labels through permission renewal and rereads the redacted candidate set', async () => {
    enableModels()
    await mount()
    await selectFirst()
    expect(document.body.textContent).toContain('Visible Provider')
    const renewing = deferred()
    holds['/auth/permissions'] = () => renewing.promise
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['team-creation-permissions'] })
    })
    await until(() => expect(host.querySelector('[aria-label="Model access"]')).toBeNull())
    expect(document.body.textContent).not.toContain('Visible Provider')
    permissions = permissions.filter((p) => p !== 'providers.read')
    delete holds['/auth/permissions']
    renewing.release()
    // The held permission response was already captured. A second fresh permission read returns the revocation.
    await until(() => expect(host.querySelector('[name="name"]')).toBeTruthy())
    await refresh('team-creation-permissions')
    await until(() =>
      expect(
        requests.filter((r) => r.url === '/admin/teams/creation-model-candidates').length,
      ).toBeGreaterThanOrEqual(2),
    )
    const input = host.querySelector<HTMLInputElement>('[aria-label="Model access"]')!
    await act(async () => {
      input.focus()
      input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
    })
    await until(() => expect(document.querySelector('[role="option"]')).toBeTruthy())
    expect(document.body.textContent).not.toContain('Visible Provider')
    expect(document.body.textContent).toContain('Unknown')
    expect(host.querySelector('[aria-label="Remove First Model"]')).toBeTruthy()
  })
  it('cannot restore Provider labels from a superseded candidate response after independent permission revocation', async () => {
    enableModels()
    await mount()
    await selectFirst()
    const obsolete = deferred()
    holds['/admin/teams/creation-model-candidates'] = () => obsolete.promise
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['team-creation-models'] })
    })
    await until(() => expect(document.body.textContent).not.toContain('Visible Provider'))
    permissions = permissions.filter((p) => p !== 'providers.read')
    delete holds['/admin/teams/creation-model-candidates']
    await refresh('team-creation-permissions')
    await until(() => expect(host.querySelector('[aria-label="Model access"]')).toBeTruthy())
    const input = host.querySelector<HTMLInputElement>('[aria-label="Model access"]')!
    await act(async () => {
      input.focus()
      input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
    })
    await until(() => expect(document.querySelector('[role="option"]')).toBeTruthy())
    await act(async () => {
      obsolete.release()
      await obsolete.promise
    })
    expect(document.body.textContent).not.toContain('Visible Provider')
    expect(document.body.textContent).toContain('Unknown')
    expect(host.querySelector('[aria-label="Remove First Model"]')).toBeTruthy()
  })
  it('clears only an unsubmitted selection on Model permission loss and creates no implicit grant', async () => {
    enableModels()
    await mount()
    await prepare()
    await selectFirst()
    permissions = permissions.filter((p) => p !== 'teams.models.write')
    await refresh('team-creation-permissions')
    expect(host.querySelector('[aria-label="Model access"]')).toBeNull()
    expect(host.textContent).toContain('Model access permission is unavailable')
    const create = [...host.querySelectorAll<HTMLButtonElement>('button')].find(
      (b) => b.textContent === 'Create Team',
    )!
    expect(create.disabled).toBe(true)
    await click('Clear Model selection')
    await submit()
    await until(() => expect(posts()).toHaveLength(1))
    expect(JSON.parse(posts()[0].data)).not.toHaveProperty('model_ids')
    expect(JSON.parse(posts()[0].data)).not.toHaveProperty('model_review_token')
    expect(requests.some((r) => r.url === '/admin/teams/creation-model-review')).toBe(false)
  })
  it('recovers original submitted Models/token through actual AuthGate500 without automatic review or create', async () => {
    enableModels()
    statuses['/admin/teams'] = 409
    await mount(true)
    await prepare()
    await selectFirst()
    await submit()
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    const original = posts()[0],
      reviews = requests.filter((r) => r.url === '/admin/teams/creation-model-review').length
    statuses['/auth/session'] = 500
    await act(async () => {
      void cache.invalidateQueries({ queryKey: sessionKey })
    })
    await until(() => expect(host.textContent).toContain('Unable to check'))
    expect(host.querySelector('form')).toBeNull()
    delete statuses['/auth/session']
    identity = { ...identity, csrf_token: 'renewed-model-csrf' }
    context = { ...context, review_etag: 'c'.repeat(64) }
    await click('Retry')
    await until(() => expect(host.textContent).toContain('Retry original creation'))
    expect(posts()).toHaveLength(1)
    expect(requests.filter((r) => r.url === '/admin/teams/creation-model-review')).toHaveLength(
      reviews,
    )
    delete statuses['/admin/teams']
    outcome = 'applied'
    await click('Retry original creation')
    await until(() => expect(router.state.location.pathname).toBe('/admin/teams/tea_created'))
    expect(posts()[1].data).toBe(original.data)
    expect(posts()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(posts()[1].headers.get('X-CSRF-Token')).toBe('renewed-model-csrf')
  })
})
