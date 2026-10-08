import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { searchPublicModelNames } from '@/lib/public-model-references'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type {
  ModelCreationInitialTarget,
  ModelCreationInput,
  ModelCreationResult,
} from '@/types/model-creation'
import en from '@/i18n/locales/en/modelCreation'
import zh from '@/i18n/locales/zh/modelCreation'
import CreateModelPage from './create'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let identity: Session,
  permissions: string[],
  requests: InternalAxiosRequestConfig[],
  fail: Record<string, number>,
  holds: Record<string, (() => Promise<void>) | undefined>,
  previewBlocked: boolean,
  reservedNames: string[],
  saved: ModelCreationResult | null
const path = '/admin/connections/con_one/model-creation'
const connection = {
  id: 'con_one',
  provider_id: 'prv_one',
  provider_name: 'Private Provider',
  name: 'Private Connection',
  protocol: 'openai_chat',
  adapter: 'native',
  api_version: null,
  base_url: 'https://example.invalid/v1',
}
let initialTargets: Record<string, ModelCreationInitialTarget | null> = {}
const pm = (id = 'pmd_one') => ({
  id,
  upstream_name: initialTargets[id]?.name ?? `Upstream ${id}`,
  disabled: false,
  input_capabilities: ['image'],
  credential_ready: true,
  selectable: true,
  blocker_codes: [],
  initial_target: initialTargets[id] ?? null,
})
beforeEach(async () => {
  await i18n.changeLanguage('en')
  identity = {
    user: { id: 'usr_one', name: 'Admin', email: 'dummy@example.invalid', role: 'admin' },
    csrf_token: 'csrf-one',
  }
  permissions = ['models.read_all', 'providers.read', 'models.write']
  requests = []
  initialTargets = {}
  fail = {}
  holds = {}
  saved = null
  previewBlocked = false
  reservedNames = []
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } },
  })
  cache.setQueryData(sessionKey, identity)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const url = config.url!
    const capturedIdentity = structuredClone(identity),
      capturedPermissions = [...permissions]
    await holds[url]?.()
    const response = {
      config,
      status: fail[url] || 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (url === '/auth/session') response.data = capturedIdentity
    else if (url === '/auth/permissions') response.data = { permissions: capturedPermissions }
    else if (url === '/admin/model-creation/connections')
      response.data = {
        items: [connection, { ...connection, id: 'con_two', name: 'Second Connection' }],
        next_cursor: null,
      }
    else if (url.endsWith('/public-names'))
      response.data = {
        connection_id: url.split('/')[3],
        query: config.params?.q ?? '',
        items: searchPublicModelNames(config.params?.q ?? '').map((name) => ({
          name,
          available: !reservedNames.includes(name),
        })),
      }
    else if (url.endsWith('/provider-models'))
      response.data = config.params?.q
        ? { items: [pm('pmd_two')], next_cursor: null }
        : config.params?.cursor
          ? { items: [pm('pmd_two')], next_cursor: null }
          : {
              items: [
                pm(),
                {
                  ...pm('pmd_blocked'),
                  disabled: true,
                  selectable: false,
                  blocker_codes: ['provider_model_disabled'],
                },
              ],
              next_cursor: 'pmd_one',
            }
    else if (url.endsWith('/models'))
      response.data = {
        items: [
          {
            id: 'mdl_old',
            name: 'Existing',
            initial_weight: 0,
            selectable: true,
            blocker_codes: [],
          },
          {
            id: 'mdl_bad',
            name: 'Blocked',
            initial_weight: 0,
            selectable: false,
            blocker_codes: ['protocol_weights_invalid'],
          },
        ],
        next_cursor: null,
      }
    else if (url.endsWith('/preview')) {
      const body = JSON.parse(config.data)
      response.data = {
        connection,
        review_etag: 'a'.repeat(64),
        observed_at: '2026-10-04T00:00:00Z',
        can_commit: !previewBlocked,
        items: body.items.map((x: ModelCreationInput['items'][number]) => ({
          provider_model_id: x.provider_model_id ?? '',
          upstream_name: x.upstream_name ?? `Upstream ${x.provider_model_id}`,
          ...(x.upstream_name !== undefined
            ? { warning_codes: ['credential_coverage_unproven'] }
            : {}),
          target: x.target,
          model_id: x.target === 'existing' ? x.model_id : null,
          name: x.target === 'new' ? x.name : 'Existing',
          protocol: 'openai_chat',
          initial_weight: x.target === 'new' ? 100 : 0,
          blocker_codes: previewBlocked ? ['name_reserved'] : [],
        })),
      }
    } else if (url.startsWith('/admin/model-creation/receipts/')) response.data = saved
    else if (config.method === 'post' && url.endsWith('/model-creation')) {
      const body = JSON.parse(config.data) as ModelCreationInput
      if (!saved) {
        const items = body.items.map((x) => ({
          provider_model_id: x.provider_model_id ?? 'pmd_manual',
          ...(x.upstream_name !== undefined ? { manual_upstream_name: x.upstream_name } : {}),
          model_id: x.target === 'new' ? `mdl_${x.provider_model_id ?? 'manual'}` : x.model_id,
          binding_id: `bnd_${x.provider_model_id ?? 'manual'}`,
          created_model: x.target === 'new',
          name: x.target === 'new' ? x.name : 'Existing',
          protocol: 'openai_chat' as const,
          weight: x.target === 'new' ? 100 : 0,
        }))
        saved = {
          receipt: {
            request_id: body.request_id,
            connection_id: url.split('/')[3],
            created_at: '2026-10-04T00:00:00Z',
            items,
          },
          committed: true,
          changed: true,
          current_items: structuredClone(items),
          runtime_applied: false,
          application_status: 'pending',
        }
      }
      response.data = structuredClone(saved)
      if (!fail[url]) response.status = 201
    } else if (url.endsWith('/model-creation'))
      response.data = {
        connection: { ...connection, id: url.split('/')[3] },
        can_create: permissions.includes('models.write'),
        observed_at: '2026-10-04T00:00:00Z',
      }
    else throw new Error(`Unexpected endpoint ${url}`)
    if (response.status >= 400) throw new AxiosError('Denied', '', config, undefined, response)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
})
async function until(assert: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
function button(text: string) {
  const b = Array.from(document.querySelectorAll('button')).find((x) => x.textContent === text)
  expect(b, `button ${text}`).toBeTruthy()
  return b!
}
async function click(text: string) {
  await act(async () => button(text).click())
}
async function change(
  element: HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement,
  value: string,
) {
  await act(async () => {
    const prototype =
      element instanceof HTMLSelectElement
        ? HTMLSelectElement.prototype
        : element instanceof HTMLTextAreaElement
          ? HTMLTextAreaElement.prototype
          : HTMLInputElement.prototype
    Object.getOwnPropertyDescriptor(prototype, 'value')!.set!.call(element, value)
    element.dispatchEvent(
      new Event(element instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }),
    )
  })
}
async function mount() {
  router = createMemoryRouter(
    [
      { path: '/admin/models/create', element: <CreateModelPage /> },
      { path: '/admin/models/:id', element: <p>Destination</p> },
    ],
    { initialEntries: ['/admin/models/create?connectionId=con_one'] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(
      document
        .querySelector('select[aria-label="Provider Connection"]')
        ?.querySelector('option[value="con_one"]'),
    ).toBeTruthy(),
  )
  await change(document.querySelector('select[aria-label="Provider Connection"]')!, 'con_one')
  await until(() =>
    expect(document.querySelector('input[aria-label="Select Upstream pmd_one"]')).toBeTruthy(),
  )
}
async function selectOne() {
  await act(async () => {
    ;(
      document.querySelector('input[aria-label="Select Upstream pmd_one"]') as HTMLInputElement
    ).click()
  })
  await change(
    document.querySelector('input[aria-label="Public Model name for Upstream pmd_one"]')!,
    'Exact/Name',
  )
  await change(document.querySelector('textarea')!, 'reviewed reason')
}
async function review() {
  await click('Review selected models')
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeTruthy())
}
const commits = () => requests.filter((x) => x.method === 'post' && x.url === path)
function deferred() {
  let release!: () => void
  return {
    promise: new Promise<void>((r) => {
      release = r
    }),
    release: () => release(),
  }
}
describe('guided atomic batch creation', () => {
  it('defaults to staged new access and explicitly switches to the bounded existing Connection picker', async () => {
    router = createMemoryRouter([{ path: '/admin/models/create', element: <CreateModelPage /> }], {
      initialEntries: ['/admin/models/create'],
    })
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
    )
    await until(() => {
      expect(button(en.access.newConnection).getAttribute('aria-pressed')).toBe('true')
      expect(button(en.existingConnection).disabled).toBe(false)
    })
    expect(host.textContent).toContain(en.access.staged)
    expect(document.querySelector('select[aria-label="Provider Connection"]')).toBeNull()
    expect(requests.some((request) => request.method === 'post')).toBe(false)
    await click(en.existingConnection)
    await until(() =>
      expect(
        document
          .querySelector('select[aria-label="Provider Connection"]')
          ?.querySelector('option[value="con_one"]'),
      ).toBeTruthy(),
    )
    expect(button(en.existingConnection).getAttribute('aria-pressed')).toBe('true')
    expect(requests.some((request) => request.url === '/admin/providers')).toBe(false)
  })
  it('reviews server weights and commits one exact batch without directory/grant calls or pending navigation', async () => {
    await mount()
    await selectOne()
    await review()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('100')
    await click('Confirm addition')
    await until(() => expect(host.textContent).toContain('The receipt confirms'))
    expect(commits()).toHaveLength(1)
    const body = JSON.parse(commits()[0].data)
    expect(body.items).toEqual([
      { provider_model_id: 'pmd_one', target: 'new', name: 'Exact/Name' },
    ])
    expect(body.reason).toBe('reviewed reason')
    expect(body.request_id).toMatch(/^[0-9a-f-]{36}$/)
    expect(commits()[0].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
    expect(router.state.location.pathname).toBe('/admin/models/create')
    expect(host.textContent).toContain('Pending runtime publication')
    expect(
      requests.some(
        (x) =>
          x.url === '/admin/providers' || x.url === '/admin/models' || x.url?.includes('grant'),
      ),
    ).toBe(false)
  })
  it('links an explicit existing target as a zero backup with no client weight', async () => {
    await mount()
    await selectOne()
    await change(
      document.querySelector('select[aria-label="Upstream pmd_one Addition and target"]')!,
      'existing',
    )
    await change(
      document.querySelector('select[aria-label="Target Model for Upstream pmd_one"]')!,
      'mdl_old',
    )
    await review()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Existing')
    await click('Confirm addition')
    await until(() => expect(commits()).toHaveLength(1))
    expect(JSON.parse(commits()[0].data).items).toEqual([
      { provider_model_id: 'pmd_one', target: 'existing', model_id: 'mdl_old' },
    ])
  })
  it('retains selected rows and exact names outside a bounded search page', async () => {
    await mount()
    await selectOne()
    const input = Array.from(document.querySelectorAll('label'))
      .find((x) => x.textContent?.startsWith('Search ProviderModels'))!
      .querySelector('input')!
    await change(input, 'other')
    await until(() =>
      expect(document.querySelector('input[aria-label="Select Upstream pmd_two"]')).toBeTruthy(),
    )
    expect(
      (
        document.querySelector(
          'input[aria-label="Public Model name for Upstream pmd_one"]',
        ) as HTMLInputElement
      ).value,
    ).toBe('Exact/Name')
    await review()
    expect(JSON.parse(requests.find((x) => x.url?.endsWith('/preview'))!.data).items).toHaveLength(
      1,
    )
  })
  it('uses cursor paging and prevents ineligible ProviderModel selection', async () => {
    await mount()
    expect(
      (
        document.querySelector(
          'input[aria-label="Select Upstream pmd_blocked"]',
        ) as HTMLInputElement
      ).disabled,
    ).toBe(true)
    await click('Load more')
    await until(() =>
      expect(document.querySelector('input[aria-label="Select Upstream pmd_two"]')).toBeTruthy(),
    )
    expect(
      requests.find((x) => x.url?.endsWith('/provider-models') && x.params?.cursor)?.params.cursor,
    ).toBe('pmd_one')
  })
  it('keeps selected data and reason during live language switching', async () => {
    await mount()
    await selectOne()
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(host.textContent).toContain('已选择 1 个')
    expect((document.querySelector('textarea') as HTMLTextAreaElement).value).toBe(
      'reviewed reason',
    )
    expect(document.querySelector('input[aria-label="Upstream pmd_one 的对外模型名"]')).toBeTruthy()
    expect(Object.keys(en).sort()).toEqual(Object.keys(zh).sort())
    expect(Object.keys(en.blockers).sort()).toEqual(Object.keys(zh.blockers).sort())
  })
  it('displays located blockers and never allows commit of invalid preview', async () => {
    await mount()
    await selectOne()
    previewBlocked = true
    await review()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      'Public name is already reserved.',
    )
    expect(button('Confirm addition').disabled).toBe(true)
    expect(commits()).toHaveLength(0)
  })
  it('requires independent write permission while keeping read-only preview available', async () => {
    permissions = ['models.read_all', 'providers.read']
    await mount()
    await selectOne()
    await review()
    expect(button('Confirm addition').disabled).toBe(true)
    expect(commits()).toHaveLength(0)
  })
  it.each(['models.read_all', 'providers.read'])(
    'missing %s never fetches Connection or private data',
    async (missing) => {
      permissions = permissions.filter((x) => x !== missing)
      router = createMemoryRouter([{ path: '/', element: <CreateModelPage /> }])
      await act(async () =>
        root.render(
          <QueryClientProvider client={cache}>
            <RouterProvider router={router} />
          </QueryClientProvider>,
        ),
      )
      await until(() =>
        expect(host.textContent).toContain('Current read permission is unavailable.'),
      )
      expect(requests.some((x) => x.url?.startsWith('/admin/'))).toBe(false)
    },
  )
  it('retains immutable unknown intent through rejected retries and current context 404', async () => {
    await mount()
    await selectOne()
    await review()
    fail[path] = 503
    await click('Confirm addition')
    await until(() => expect(button('Retry the original request')).toBeTruthy())
    const first = commits()[0]
    fail[path] = 409
    await click('Retry the original request')
    await until(() => expect(commits()).toHaveLength(2))
    expect(commits()[1].data).toBe(first.data)
    expect(commits()[1].headers.get('If-Match')).toBe(first.headers.get('If-Match'))
    expect(host.textContent).toContain('The outcome is unknown')
    fail[path] = 404
    await act(async () => {
      await cache.invalidateQueries({ predicate: (q) => q.queryKey.includes('context') })
    })
    await until(() => expect(host.querySelector('dl')).toBeNull())
    expect(button('Check the original receipt').disabled).toBe(false)
    await click('Check the original receipt')
    await until(() => expect(host.textContent).toContain('committed'))
    expect(commits()).toHaveLength(2)
  })
  it('keeps conflict draft but requires a new explicit preview', async () => {
    await mount()
    await selectOne()
    await review()
    fail[path] = 409
    await click('Confirm addition')
    await until(() => expect(host.textContent).toContain('Your draft is retained'))
    expect((document.querySelector('textarea') as HTMLTextAreaElement).value).toBe(
      'reviewed reason',
    )
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(commits()).toHaveLength(1)
    await review()
    expect(requests.filter((x) => x.url?.endsWith('/preview'))).toHaveLength(2)
  })
  it('duplicate confirmation clicks synchronously dispatch only once', async () => {
    await mount()
    await selectOne()
    await review()
    const hold = deferred()
    holds[path] = () => hold.promise
    await act(async () => {
      const b = button('Confirm addition')
      b.click()
      b.click()
    })
    await until(() => expect(commits()).toHaveLength(1))
    hold.release()
    await until(() => expect(host.textContent).toContain('The receipt confirms'))
  })
  it('refreshes current receipt separately without rewriting historical names or weight', async () => {
    await mount()
    await selectOne()
    await review()
    await click('Confirm addition')
    await until(() => expect(host.textContent).toContain('The receipt confirms'))
    saved!.application_status = 'superseded'
    saved!.runtime_applied = false
    saved!.current_items![0] = { ...saved!.current_items![0], name: 'Changed', weight: 50 }
    await click('Refresh receipt and current application')
    await until(() =>
      expect(host.textContent).toContain('The recorded configuration has since changed'),
    )
    expect(host.textContent).toContain('Open Exact/Name')
    expect(commits()).toHaveLength(1)
  })
  it('same-actor manual CSRF replacement keeps reviewed authority and uses the new token', async () => {
    await mount()
    await selectOne()
    await review()
    await act(async () => {
      cache.setQueryData(sessionKey, { ...identity, csrf_token: 'rotated-token' })
    })
    expect(button('Confirm addition').disabled).toBe(false)
    await click('Confirm addition')
    await until(() => expect(commits()).toHaveLength(1))
    expect(commits()[0].headers.get('X-CSRF-Token')).toBe('rotated-token')
  })
  it('same-ms structurally identical Session success hides the old review until fresh authority', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(Date.now())
    await mount()
    await selectOne()
    await review()
    const hold = deferred()
    holds['/auth/permissions'] = () => hold.promise
    const oldTime = cache.getQueryState(sessionKey)!.dataUpdatedAt
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey, exact: true })
    })
    expect(cache.getQueryState(sessionKey)!.dataUpdatedAt).toBe(oldTime)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(host.textContent).not.toContain('Private Provider')
    hold.release()
    await until(() =>
      expect(
        document.querySelector('input[aria-label="Public Model name for Upstream pmd_one"]'),
      ).toBeTruthy(),
    )
    expect((document.querySelector('textarea') as HTMLTextAreaElement).value).toBe(
      'reviewed reason',
    )
    expect(button('Confirm addition').disabled).toBe(true)
    expect(commits()).toHaveLength(0)
    expect(requests.filter((x) => x.url === '/auth/session').length).toBeLessThanOrEqual(2)
  })
  it('permission renewal clears stale dialog and refuses dispatch after write revocation', async () => {
    await mount()
    await selectOne()
    await review()
    permissions = ['models.read_all', 'providers.read']
    await act(async () => {
      await cache.invalidateQueries({ predicate: (q) => q.queryKey.includes('permissions') })
    })
    await until(() => expect(button('Confirm addition').disabled).toBe(true))
    expect(commits()).toHaveLength(0)
  })
  it('ignores a late committed response after Session renewal and preserves original uncertainty', async () => {
    await mount()
    await selectOne()
    await review()
    const hold = deferred()
    holds[path] = () => hold.promise
    await click('Confirm addition')
    await until(() => expect(commits()).toHaveLength(1))
    await act(async () => {
      await cache.fetchQuery({ queryKey: sessionKey, queryFn: async () => identity, staleTime: 0 })
    })
    hold.release()
    await until(() => expect(button('Retry the original request').disabled).toBe(false))
    expect(host.textContent).not.toContain('The receipt confirms')
    expect(commits()).toHaveLength(1)
  })
  it('actor switch clears drafts, original intent and any old completion', async () => {
    await mount()
    await selectOne()
    await review()
    const hold = deferred()
    holds[path] = () => hold.promise
    await click('Confirm addition')
    identity = { ...identity, user: { ...identity.user, id: 'usr_two' } }
    await act(async () => {
      cache.setQueryData(sessionKey, identity)
    })
    hold.release()
    await until(() =>
      expect(document.querySelector('input[aria-label="Select Upstream pmd_one"]')).toBeTruthy(),
    )
    expect(host.textContent).not.toContain('The receipt confirms')
    expect(host.textContent).not.toContain('The outcome is unknown')
    expect(document.querySelector('textarea')?.value).toBe('')
    expect(commits()).toHaveLength(1)
  })
  it('successful identical permission read in the same millisecond requires explicit new review', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(Date.now())
    await mount()
    await selectOne()
    await review()
    const permissionKey = cache
      .getQueryCache()
      .findAll()
      .find((q) => q.queryKey.includes('permissions') && q.isActive())!.queryKey
    const oldCount = cache.getQueryState(permissionKey)!.dataUpdateCount
    await act(async () => {
      await cache.fetchQuery({
        queryKey: permissionKey,
        queryFn: async () => [...permissions],
        staleTime: 0,
      })
    })
    expect(cache.getQueryState(permissionKey)!.dataUpdateCount).toBe(oldCount + 1)
    await until(() => expect(button('Confirm addition').disabled).toBe(true))
    expect(commits()).toHaveLength(0)
    expect(document.querySelector('textarea')?.value).toBe('reviewed reason')
  })
  it('successful picker refresh invalidates a prior review even with identical data and timestamp', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(Date.now())
    await mount()
    await selectOne()
    await review()
    const query = cache
      .getQueryCache()
      .findAll()
      .find((q) => q.queryKey.includes('provider-models') && q.isActive())!
    await act(async () => {
      await cache.refetchQueries({ queryKey: query.queryKey, exact: true })
    })
    await until(() => expect(button('Confirm addition').disabled).toBe(true))
    expect(commits()).toHaveLength(0)
  })
  it('read revocation hides the captured body and disables receipt lookup without resolving uncertainty', async () => {
    await mount()
    await selectOne()
    await review()
    fail[path] = 503
    await click('Confirm addition')
    await until(() => expect(button('Retry the original request')).toBeTruthy())
    permissions = ['models.write']
    await act(async () => {
      await cache.invalidateQueries({ predicate: (q) => q.queryKey.includes('permissions') })
    })
    await until(() => expect(host.textContent).toContain('Current read permission is unavailable.'))
    expect(document.querySelector('textarea')).toBeNull()
    expect(host.textContent).not.toContain('Exact/Name')
    expect(
      Array.from(document.querySelectorAll('button')).some(
        (b) => b.textContent === 'Check the original receipt',
      ),
    ).toBe(false)
    expect(requests.filter((x) => x.url?.includes('/receipts/'))).toHaveLength(0)
    expect(commits()).toHaveLength(1)
  })
  it('Session error hides all private facts and a late context response cannot restore them', async () => {
    await mount()
    await selectOne()
    const hold = deferred()
    holds[path] = () => hold.promise
    const read = act(async () => {
      void cache.invalidateQueries({ predicate: (q) => q.queryKey.includes('context') })
    })
    await read
    await act(async () => {
      await cache
        .fetchQuery({
          queryKey: sessionKey,
          queryFn: async () => {
            throw new Error('Unavailable')
          },
          staleTime: 0,
        })
        .catch(() => undefined)
    })
    hold.release()
    await until(() => expect(host.querySelector('dl')).toBeNull())
    expect(host.textContent).not.toContain('Private Provider')
    expect(host.textContent).not.toContain('Upstream pmd_one')
  })
  it('Connection switch is blocked until the exact receipt resolves an unknown intent', async () => {
    await mount()
    await selectOne()
    await review()
    fail[path] = 503
    await click('Confirm addition')
    await until(() => expect(button('Retry the original request')).toBeTruthy())
    expect(
      document.querySelector<HTMLSelectElement>('select[aria-label="Provider Connection"]')!
        .disabled,
    ).toBe(true)
    expect(button('Create new access').disabled).toBe(true)
    await change(document.querySelector('select[aria-label="Provider Connection"]')!, 'con_two')
    expect(host.textContent).toContain('The outcome is unknown')
    expect(
      document.querySelector<HTMLSelectElement>('select[aria-label="Provider Connection"]')!.value,
    ).toBe('con_one')
    await click('Check the original receipt')
    await until(() =>
      expect(
        document.querySelector<HTMLSelectElement>('select[aria-label="Provider Connection"]')!
          .disabled,
      ).toBe(false),
    )
    await change(document.querySelector('select[aria-label="Provider Connection"]')!, 'con_two')
    await until(() =>
      expect(document.querySelector('input[aria-label="Select Upstream pmd_one"]')).toBeTruthy(),
    )
    expect(host.textContent).not.toContain('The outcome is unknown')
    expect(document.querySelector('textarea')?.value).toBe('')
    expect(commits()).toHaveLength(1)
  })
  it('explicit public-name keyboard selection submits only its exact row name through existing review', async () => {
    await mount()
    await selectOne()
    const input = document.querySelector<HTMLInputElement>(
      'input[aria-label="Public Model name for Upstream pmd_one"]',
    )!
    await change(input, 'gpt-5.2')
    await act(async () => {
      input.focus()
      input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
    })
    await until(() => expect(document.querySelectorAll('[role="option"]')).toHaveLength(2))
    expect(commits()).toHaveLength(0)
    await act(async () =>
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })),
    )
    await act(async () =>
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })),
    )
    expect(input.value).toBe('gpt-5.2')
    expect(commits()).toHaveLength(0)
    await review()
    await click('Confirm addition')
    await until(() => expect(commits()).toHaveLength(1))
    expect(JSON.parse(commits()[0].data).items).toEqual([
      { provider_model_id: 'pmd_one', target: 'new', name: 'gpt-5.2' },
    ])
    expect(requests.some((r) => r.url?.includes('reference') || r.url?.includes('grant'))).toBe(
      false,
    )
  })
  it('suggestions update only the chosen row and custom names survive language switching', async () => {
    await mount()
    await selectOne()
    await click('Load more')
    await until(() =>
      expect(document.querySelector('input[aria-label="Select Upstream pmd_two"]')).toBeTruthy(),
    )
    await act(async () =>
      (
        document.querySelector('input[aria-label="Select Upstream pmd_two"]') as HTMLInputElement
      ).click(),
    )
    const second = document.querySelector<HTMLInputElement>(
      'input[aria-label="Public Model name for Upstream pmd_two"]',
    )!
    await change(second, 'claude')
    await act(async () =>
      second.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 })),
    )
    await until(() =>
      expect(document.querySelector('[role="option"]')?.textContent).toBe('claude-sonnet-4-6'),
    )
    await act(async () => (document.querySelector('[role="option"]') as HTMLElement).click())
    expect(second.value).toBe('claude-sonnet-4-6')
    expect(
      document.querySelector<HTMLInputElement>(
        'input[aria-label="Public Model name for Upstream pmd_one"]',
      )!.value,
    ).toBe('Exact/Name')
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain(zh.publicNameSuggestions)
    expect(
      Array.from(host.querySelectorAll('input')).some((input) => input.value === 'Exact/Name'),
    ).toBe(true)
    expect(
      Array.from(host.querySelectorAll('input')).some(
        (input) => input.value === 'claude-sonnet-4-6',
      ),
    ).toBe(true)
    expect(commits()).toHaveLength(0)
  })
  it('late suggestion selection during Session renewal cannot change the retained name', async () => {
    await mount()
    await selectOne()
    const input = document.querySelector<HTMLInputElement>(
      'input[aria-label="Public Model name for Upstream pmd_one"]',
    )!
    await change(input, 'gpt')
    await act(async () =>
      input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 })),
    )
    await until(() => expect(document.querySelector('[role="option"]')).toBeTruthy())
    const staleOption = document.querySelector<HTMLElement>('[role="option"]')!
    const hold = deferred()
    holds['/auth/session'] = () => hold.promise
    await act(async () => {
      void cache.refetchQueries({ queryKey: sessionKey, exact: true })
      staleOption.click()
    })
    expect(document.querySelector('[role="option"]')).toBeNull()
    hold.release()
    await until(() =>
      expect(
        document.querySelector<HTMLInputElement>(
          'input[aria-label="Public Model name for Upstream pmd_one"]',
        )?.value,
      ).toBe('gpt'),
    )
    expect(commits()).toHaveLength(0)
  })
  it('read permission loss or Connection change closes suggestions and discards obsolete selection events', async () => {
    await mount()
    await selectOne()
    const input = document.querySelector<HTMLInputElement>(
      'input[aria-label="Public Model name for Upstream pmd_one"]',
    )!
    await change(input, 'gemini')
    await act(async () =>
      input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 })),
    )
    await until(() => expect(document.querySelector('[role="option"]')).toBeTruthy())
    const staleOption = document.querySelector<HTMLElement>('[role="option"]')!
    permissions = ['models.write']
    await act(async () => {
      void cache.invalidateQueries({ predicate: (q) => q.queryKey.includes('permissions') })
      staleOption.click()
    })
    await until(() => expect(host.textContent).toContain(en.denied))
    expect(document.querySelector('[role="option"]')).toBeNull()
    permissions = ['models.read_all', 'providers.read', 'models.write']
    await act(async () => {
      await cache.invalidateQueries({ predicate: (q) => q.queryKey.includes('permissions') })
    })
    await until(() =>
      expect(
        document.querySelector<HTMLInputElement>(
          'input[aria-label="Public Model name for Upstream pmd_one"]',
        )?.value,
      ).toBe('gemini'),
    )
    await change(document.querySelector('select[aria-label="Provider Connection"]')!, 'con_two')
    await act(async () => staleOption.click())
    await until(() =>
      expect(document.querySelector('input[aria-label="Select Upstream pmd_one"]')).toBeTruthy(),
    )
    expect(
      host.querySelector('input[aria-label="Public Model name for Upstream pmd_one"]'),
    ).toBeNull()
    expect(commits()).toHaveLength(0)
  })
  it('actor changes remove old suggestion interaction and clear its row draft', async () => {
    await mount()
    await selectOne()
    const input = document.querySelector<HTMLInputElement>(
      'input[aria-label="Public Model name for Upstream pmd_one"]',
    )!
    await change(input, 'gpt')
    await act(async () =>
      input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 })),
    )
    await until(() => expect(document.querySelector('[role="option"]')).toBeTruthy())
    const staleOption = document.querySelector<HTMLElement>('[role="option"]')!
    identity = { ...identity, user: { ...identity.user, id: 'usr_two' } }
    await act(async () => {
      cache.setQueryData(sessionKey, identity)
      staleOption.click()
    })
    await until(() =>
      expect(document.querySelector('input[aria-label="Select Upstream pmd_one"]')).toBeTruthy(),
    )
    expect(document.querySelector('[role="option"]')).toBeNull()
    expect(
      host.querySelector('input[aria-label="Public Model name for Upstream pmd_one"]'),
    ).toBeNull()
    expect(host.querySelector('textarea')?.value).toBe('')
    expect(commits()).toHaveLength(0)
  })
  it('an old option cannot switch a just-changed existing target back to new in the same batch', async () => {
    await mount()
    await selectOne()
    const input = document.querySelector<HTMLInputElement>(
      'input[aria-label="Public Model name for Upstream pmd_one"]',
    )!
    await change(input, 'gpt')
    await act(async () =>
      input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 })),
    )
    await until(() => expect(document.querySelector('[role="option"]')).toBeTruthy())
    const staleOption = document.querySelector<HTMLElement>('[role="option"]')!
    const target = document.querySelector<HTMLSelectElement>(
      'select[aria-label="Upstream pmd_one Addition and target"]',
    )!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(
        target,
        'existing',
      )
      target.dispatchEvent(new Event('change', { bubbles: true }))
      staleOption.click()
    })
    expect(target.value).toBe('existing')
    expect(
      host.querySelector('input[aria-label="Public Model name for Upstream pmd_one"]'),
    ).toBeNull()
    await change(
      document.querySelector('select[aria-label="Target Model for Upstream pmd_one"]')!,
      'mdl_old',
    )
    await review()
    expect(
      JSON.parse(requests.filter((r) => r.url === path + '/preview').at(-1)!.data).items,
    ).toEqual([{ provider_model_id: 'pmd_one', target: 'existing', model_id: 'mdl_old' }])
    expect(commits()).toHaveLength(0)
  })
})

it('filters server-reserved suggestions while preserving custom reserved text and final preview', async () => {
  reservedNames = ['gpt-5.2']
  await mount()
  await selectOne()
  const input = document.querySelector<HTMLInputElement>(
    'input[aria-label="Public Model name for Upstream pmd_one"]',
  )!
  await change(input, 'gpt-5.2')
  await act(async () => {
    input.focus()
    input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0 }))
  })
  await until(() =>
    expect(
      Array.from(document.querySelectorAll('[role="option"]')).map((x) => x.textContent),
    ).toEqual(['gpt-5.2-2025-12-11']),
  )
  expect(input.value).toBe('gpt-5.2')
  expect(commits()).toHaveLength(0)
  previewBlocked = true
  await change(document.querySelector('textarea')!, 'Explicit reservation race review')
  await click(en.review)
  await until(() => expect(document.body.textContent).toContain(en.blockers.name_reserved))
  expect(button(en.confirm).disabled).toBe(true)
  expect(commits()).toHaveLength(0)
})

describe('source-bound initial row assistance', () => {
  it('initializes the native name once and preserves custom editing through picker refresh and live language change', async () => {
    initialTargets.pmd_one = { target: 'new', name: 'native/exact' }
    await mountInitial('native/exact')
    await act(async () =>
      (
        document.querySelector('input[aria-label="Select native/exact"]') as HTMLInputElement
      ).click(),
    )
    const field = () =>
      document.querySelector(
        'input[aria-label="Public Model name for native/exact"]',
      ) as HTMLInputElement
    expect(field().value).toBe('native/exact')
    expect(requests.filter((x) => x.method === 'post')).toHaveLength(0)
    await change(field(), 'Custom/exact')
    await act(async () =>
      cache.refetchQueries({ queryKey: ['model-creation', 'usr_one', 'con_one'] }),
    )
    await until(() => expect(field()?.value).toBe('Custom/exact'))
    await act(async () => i18n.changeLanguage('zh'))
    expect(
      (document.querySelector('input[aria-label="native/exact 的对外模型名"]') as HTMLInputElement)
        .value,
    ).toBe('Custom/exact')
  })
  it('keeps an exact existing target and its authorized label outside the first target page without a directory fetch', async () => {
    initialTargets.pmd_one = {
      target: 'existing',
      name: 'off-page/exact',
      model_id: 'mdl_offpage',
      initial_weight: 0,
    }
    await mountInitial('off-page/exact')
    await act(async () =>
      (
        document.querySelector('input[aria-label="Select off-page/exact"]') as HTMLInputElement
      ).click(),
    )
    const target = document.querySelector(
      'select[aria-label="Target Model for off-page/exact"]',
    ) as HTMLSelectElement
    expect(target.value).toBe('mdl_offpage')
    expect(target.selectedOptions[0].textContent).toBe('off-page/exact')
    await change(document.querySelector('textarea')!, 'reviewed existing')
    await review()
    const body = JSON.parse([...requests].reverse().find((x) => x.url?.endsWith('/preview'))!.data)
    expect(body.items).toEqual([
      { provider_model_id: 'pmd_one', target: 'existing', model_id: 'mdl_offpage' },
    ])
    expect(requests.some((x) => x.url === '/admin/models')).toBe(false)
    expect(commits()).toHaveLength(0)
  })
  it('hides initial target facts synchronously on picker invalidation and preserves the existing draft after a fresh reply', async () => {
    initialTargets.pmd_one = { target: 'new', name: 'native/exact' }
    await mountInitial('native/exact')
    const old = document.querySelector(
      'input[aria-label="Select native/exact"]',
    ) as HTMLInputElement
    await act(async () => old.click())
    await change(
      document.querySelector('input[aria-label="Public Model name for native/exact"]')!,
      'Custom/preserved',
    )
    await act(async () =>
      cache.invalidateQueries({
        queryKey: ['model-creation', 'usr_one', 'con_one'],
        refetchType: 'none',
      }),
    )
    expect(host.textContent).not.toContain('native/exact')
    await act(async () => old.click())
    expect(commits()).toHaveLength(0)
    await act(async () =>
      cache.refetchQueries({ queryKey: ['model-creation', 'usr_one', 'con_one'] }),
    )
    await until(() =>
      expect(
        (
          document.querySelector(
            'input[aria-label="Public Model name for native/exact"]',
          ) as HTMLInputElement
        )?.value,
      ).toBe('Custom/preserved'),
    )
  })
})
async function mountInitial(name: string) {
  router = createMemoryRouter([{ path: '/admin/models/create', element: <CreateModelPage /> }], {
    initialEntries: ['/admin/models/create?connectionId=con_one'],
  })
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(
      document.querySelector('select[aria-label="Provider Connection"] option[value="con_one"]'),
    ).toBeTruthy(),
  )
  await change(document.querySelector('select[aria-label="Provider Connection"]')!, 'con_one')
  await until(() =>
    expect(document.querySelector(`input[aria-label="Select ${name}"]`)).toBeTruthy(),
  )
}

it.each(['context', 'permissions', 'session', 'connections'])(
  'rejects a captured initial selection in the same turn as %s-only invalidation',
  async (kind) => {
    initialTargets.pmd_one = { target: 'new', name: 'native/exact' }
    await mountInitial('native/exact')
    const checkbox = document.querySelector(
      'input[aria-label="Select native/exact"]',
    ) as HTMLInputElement
    const reactKey = Object.keys(checkbox).find((key) => key.startsWith('__reactProps$'))!
    const captured = (Reflect.get(checkbox, reactKey) as { onChange: () => void }).onChange
    const key =
      kind === 'session'
        ? sessionKey
        : cache
            .getQueryCache()
            .getAll()
            .find(
              (query) =>
                query.isActive() &&
                (kind === 'context'
                  ? query.queryKey.at(-1) === 'context'
                  : query.queryKey[0] === 'model-creation' && query.queryKey.includes(kind)),
            )!.queryKey
    await act(async () => {
      void cache.invalidateQueries({ queryKey: key, exact: true, refetchType: 'none' })
      captured()
    })
    expect(host.textContent).not.toContain('native/exact')
    await act(async () => cache.refetchQueries({ queryKey: key, exact: true }))
    await until(() =>
      expect(document.querySelector('input[aria-label="Select native/exact"]')).toBeTruthy(),
    )
    expect(
      (document.querySelector('input[aria-label="Select native/exact"]') as HTMLInputElement)
        .checked,
    ).toBe(false)
    expect(requests.filter((request) => request.method === 'post')).toHaveLength(0)
  },
)

async function addManual(name = 'not/discovered') {
  await click(en.manualAdd)
  const dialog = document.querySelector('[role="dialog"]')!
  await change(dialog.querySelector('input')!, name)
  await click(en.manualToList)
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  await change(
    document.querySelector('input[aria-label="Public Model name for ' + name + '"]')!,
    'Manual/Public',
  )
  await change(document.querySelector('textarea')!, 'Reviewed manual configuration')
}
describe('manual Provider Model drafts', () => {
  it('requires providers.write independently and cancels without any write or preview', async () => {
    await mount()
    expect(button(en.manualAdd).disabled).toBe(true)
    permissions.push('providers.write')
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['permissions'] })
    })
    // The scoped permission resource must be renewed explicitly.
    await act(async () => {
      await cache.invalidateQueries({ predicate: (q) => q.queryKey.includes('permissions') })
    })
    await until(() => expect(button(en.manualAdd).disabled).toBe(false))
    await click(en.manualAdd)
    await change(document.querySelector('[role="dialog"] input')!, 'not/discovered')
    await act(async () => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(requests.filter((x) => x.method === 'post')).toHaveLength(0)
  })
  it('adds a draft row with unknown coverage, keeps exact native name and commits only after review', async () => {
    permissions.push('providers.write')
    await mount()
    await addManual()
    expect(host.textContent).toContain(en.manualCoverage)
    expect(host.textContent).toContain(en.unknownCapabilities)
    expect(commits()).toHaveLength(0)
    await review()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(en.manualCoverage)
    await click('Confirm addition')
    await until(() => expect(commits()).toHaveLength(1))
    expect(JSON.parse(commits()[0].data).items).toEqual([
      { upstream_name: 'not/discovered', target: 'new', name: 'Manual/Public' },
    ])
    expect(requests.some((x) => /verify|credentials|bindings|grant/.test(x.url!))).toBe(false)
  })
  it('preserves the original manual UUID/name/reason/review on uncertain retries using fresh CSRF', async () => {
    permissions.push('providers.write')
    await mount()
    await addManual()
    await review()
    fail[path] = 503
    await click('Confirm addition')
    await until(() => expect(host.textContent).toContain('outcome is unknown'))
    const originalBody = commits()[0].data
    await act(async () => {
      cache.setQueryData(sessionKey, { ...identity, csrf_token: 'csrf-new' })
    })
    fail[path] = 409
    await click(en.retryOriginal)
    await until(() => expect(commits()).toHaveLength(2))
    expect(commits()[1].data).toBe(originalBody)
    expect(commits()[1].headers.get('If-Match')).toBe(commits()[0].headers.get('If-Match'))
    expect(commits()[1].headers.get('X-CSRF-Token')).toBe('csrf-new')
    expect(host.textContent).toContain('outcome is unknown')
  })
  it('rejects a captured Add during same-turn permission/Connection renewal and renders paired copy', async () => {
    permissions.push('providers.write')
    await mount()
    await click(en.manualAdd)
    await change(document.querySelector('[role="dialog"] input')!, 'not/discovered')
    const add = button(en.manualToList)
    const hold = deferred()
    holds[path] = () => hold.promise
    await act(async () => {
      void cache.invalidateQueries({ predicate: (q) => q.queryKey.includes('context') })
      add.click()
    })
    expect(document.querySelector('input[aria-label="Select not/discovered"]')).toBeNull()
    hold.release()
    await until(() => expect(host.querySelector('dl')).toBeTruthy())
    await click(en.manualAdd)
    await change(document.querySelector('[role="dialog"] input')!, 'not/discovered')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(zh.manualCoverage)
    expect((document.querySelector('[role="dialog"] input') as HTMLInputElement).value).toBe(
      'not/discovered',
    )
  })
})

it('manual held commit becomes uncertain after Session renewal and cannot restore saved facts', async () => {
  permissions.push('providers.write')
  await mount()
  await addManual()
  await review()
  const hold = deferred()
  holds[path] = () => hold.promise
  await click('Confirm addition')
  await until(() => expect(commits()).toHaveLength(1))
  // Manual batch uncertainty must retain the independently composed access-mode lock.
  expect(button(en.access.newConnection).disabled).toBe(true)
  expect(
    document.querySelector<HTMLSelectElement>('select[aria-label="Provider Connection"]')!.disabled,
  ).toBe(true)
  await act(async () => {
    await cache.fetchQuery({
      queryKey: sessionKey,
      queryFn: async () => ({ ...identity, csrf_token: 'renewed' }),
      staleTime: 0,
    })
  })
  hold.release()
  await until(() => expect(host.textContent).toContain('The outcome is unknown'))
  expect(host.textContent).not.toContain('The receipt confirms')
  expect(button(en.access.newConnection).disabled).toBe(true)
  expect(
    document.querySelector<HTMLSelectElement>('select[aria-label="Provider Connection"]')!.disabled,
  ).toBe(true)
  expect(commits()).toHaveLength(1)
  expect(requests.filter((x) => x.url?.includes('/receipts/'))).toHaveLength(0)
})

it.each(['mode', 'connection'] as const)(
  'keeps a just-dispatched manual batch mounted against a captured same-turn %s switch',
  async (kind) => {
    permissions.push('providers.write')
    await mount()
    await addManual()
    await review()
    const control =
      kind === 'mode'
        ? button(en.access.newConnection)
        : host.querySelector<HTMLSelectElement>('select[aria-label="Provider Connection"]')!
    const propKey = Object.keys(control).find((key) => key.startsWith('__reactProps'))!
    const props = Reflect.get(control, propKey) as {
      onClick: () => void
      onChange: (event: { target: { value: string } }) => void
    }
    const captured =
      kind === 'mode' ? props.onClick : () => props.onChange({ target: { value: 'con_two' } })
    const hold = deferred()
    holds[path] = () => hold.promise
    fail[path] = 503
    const confirm = button(en.confirm)
    await act(async () => {
      confirm.click()
      captured()
    })
    await until(() => expect(commits()).toHaveLength(1))
    expect(button(en.access.newConnection).getAttribute('aria-pressed')).toBe('false')
    expect(
      host.querySelector<HTMLSelectElement>('select[aria-label="Provider Connection"]')!.value,
    ).toBe('con_one')
    const originalBody = commits()[0].data
    const originalReview = commits()[0].headers.get('If-Match')
    expect(requests.filter((x) => /access-creations/.test(x.url!))).toHaveLength(0)
    hold.release()
    await until(() => expect(host.textContent).toContain(en.unknown))
    await act(async () => captured())
    expect(button(en.access.newConnection).getAttribute('aria-pressed')).toBe('false')
    expect(commits()).toHaveLength(1)
    fail[path] = 409
    await click(en.retryOriginal)
    await until(() => expect(commits()).toHaveLength(2))
    expect(commits()[1].data).toBe(originalBody)
    expect(commits()[1].headers.get('If-Match')).toBe(originalReview)
    expect(JSON.parse(commits()[1].data).request_id).toBe(JSON.parse(originalBody).request_id)
    expect(host.textContent).toContain(en.unknown)
  },
)
