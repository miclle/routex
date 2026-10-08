import { act, StrictMode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type AxiosAdapter, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import i18n from '@/i18n'
import { ModelAccessForm } from './model-inline-access'
import CreateModelPage from './create'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  requests: InternalAxiosRequestConfig[],
  basis: string,
  allowed: boolean,
  actor: string,
  status: number,
  hold: (() => Promise<void>) | undefined,
  context: { storage_source: 'inline' | 'vault'; etag: string },
  verified: boolean,
  enabled: boolean,
  verifyStatus: number,
  verifyResult: boolean,
  disposed: boolean,
  providerVisible: boolean
const saved = vi.fn(),
  locked = vi.fn()
const identity: Session = {
  user: { id: 'usr_one', name: 'Admin', email: 'test@example.invalid', role: 'admin' },
  csrf_token: 'csrf-one',
}
const secret = ' transient-inline-test-secret '
const field = (name: string) =>
  Array.from(document.querySelectorAll('label'))
    .find((label) => label.querySelector('span')?.textContent === name)!
    .querySelector('input,select') as HTMLInputElement | HTMLSelectElement
const button = (name: string) =>
  Array.from(document.querySelectorAll('button')).find((button) => button.textContent === name)!
async function until(assert: () => void) {
  for (let n = 0; n < 100; n++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assert()
      return
    } catch (error) {
      if (n === 99) throw error
    }
  }
}
async function change(name: string, value: string) {
  await act(async () => {
    const element = field(name)
    Object.getOwnPropertyDescriptor(
      element instanceof HTMLSelectElement
        ? HTMLSelectElement.prototype
        : HTMLInputElement.prototype,
      'value',
    )!.set!.call(element, value)
    element.dispatchEvent(
      new Event(element instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }),
    )
  })
}
async function click(name: string) {
  await act(async () => button(name).click())
}
const permissionKey = ['inline-test-permissions'] as const
function readAuthority() {
  const session = cache.getQueryState<Session>(sessionKey)
  const permission = cache.getQueryState<string[]>(permissionKey)
  return (
    allowed &&
    session?.data?.user.id === actor &&
    session.status === 'success' &&
    session.fetchStatus === 'idle' &&
    !session.isInvalidated &&
    permission?.status === 'success' &&
    permission.fetchStatus === 'idle' &&
    !permission.isInvalidated &&
    permission.data?.includes('models.read_all') === true &&
    permission.data.includes('providers.read')
  )
}
async function render() {
  await act(async () =>
    root.render(
      <StrictMode>
        <QueryClientProvider client={cache}>
          <MemoryRouter>
            <ModelAccessForm
              key={actor}
              actor={actor}
              basis={basis}
              readable={allowed}
              writable={allowed}
              fresh={readAuthority}
              writeFresh={() =>
                readAuthority() &&
                cache.getQueryData<string[]>(permissionKey)?.includes('providers.write') === true
              }
              onSaved={saved}
              onInteractionLockChange={locked}
            />
          </MemoryRouter>
        </QueryClientProvider>
      </StrictMode>,
    ),
  )
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  basis = '1:1'
  allowed = true
  actor = 'usr_one'
  status = 201
  hold = undefined
  context = { storage_source: 'inline', etag: 'a'.repeat(64) }
  verified = false
  enabled = false
  verifyStatus = 200
  verifyResult = true
  disposed = false
  providerVisible = true
  requests = []
  saved.mockClear()
  locked.mockClear()
  vi.spyOn(crypto, 'randomUUID').mockReturnValue('11111111-1111-4111-8111-111111111111')
  cache = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  cache.setQueryData(sessionKey, identity)
  cache.setQueryData(permissionKey, ['models.read_all', 'providers.read', 'providers.write'])
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const url = config.url!,
      response = {
        config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders(),
        data: {} as unknown,
      }
    if (url === '/admin/provider-credential-storage-context') {
      response.data = context
      response.headers.set('ETag', `"${context.etag}"`)
    } else if (url === '/admin/model-creation/egresses')
      response.data = {
        items:
          config.params?.q === 'egr_one'
            ? [{ id: 'egr_earlier', name: 'Name contains egr_one', enabled: true }]
            : [{ id: 'egr_one', name: 'Named egress', enabled: true }],
        next_cursor: null,
      }
    else if (url === '/admin/model-creation/providers')
      response.data = {
        items: providerVisible
          ? config.params?.q === 'prv_existing'
            ? [{ id: 'prv_earlier', name: 'Name contains prv_existing' }]
            : [{ id: 'prv_existing', name: 'Empty Provider' }]
          : [],
        next_cursor: null,
      }
    else if (url === '/admin/providers' || url === '/admin/providers/prv_existing/connections') {
      const input = JSON.parse(config.data)
      await hold?.()
      response.status = status
      const connection = {
        id: 'con_one',
        name: input.connection_name ?? input.name,
        base_url: input.base_url.replace(/\/+$/, ''),
        protocol: input.protocol,
        adapter: input.adapter,
        api_version: input.api_version,
        enabled: true,
        egress_mode: input.egress_mode,
        egress_id: input.egress_id,
        etag: '0',
        provider_models: [],
        credentials: [
          {
            id: 'crd_one',
            name: input.credential_name,
            enabled: false,
            verification_status: 'pending',
            verified_at: null,
            storage_source: context.storage_source,
            replaces_credential_id: null,
          },
        ],
      }
      response.data =
        url === '/admin/providers'
          ? { id: 'prv_one', name: input.name, connections: [connection] }
          : connection
    } else if (url === '/admin/credentials/crd_one/metadata')
      response.data = {
        id: 'crd_one',
        connection_id: 'con_one',
        name: 'Default Credential',
        priority: 0,
        enabled,
        verification_status: verified ? 'verified' : 'pending',
        verified_at: verified ? '2026-10-08T00:00:00Z' : null,
        etag: 'b'.repeat(64),
      }
    else if (url === '/admin/credentials/crd_one/verify') {
      verified = verifyResult
      response.status = verifyStatus
      response.data = {
        verified: verifyResult,
        discovered_models: verifyResult ? 1 : 0,
        message: 'Credential verified',
      }
    } else if (url === '/admin/credentials/crd_one' && config.method === 'patch') {
      enabled = true
      response.data = {
        id: 'crd_one',
        enabled: true,
        verification_status: 'verified',
        storage_source: 'inline',
      }
    } else throw new Error(`Unexpected endpoint ${url}`)
    if (response.status >= 400) throw new AxiosError('Rejected', '', config, undefined, response)
    return response
  }
  await render()
  await until(() => expect(button('Create access').disabled).toBe(false))
})
afterEach(async () => {
  if (!disposed) await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
})
async function fill() {
  await change('Provider name', 'Inline Provider')
  await change('Upstream Base URL', 'https://example.invalid/v1')
  await change('Upstream API Key', secret)
}
const writes = () =>
  requests.filter((request) => request.method === 'post' || request.method === 'patch')
it('keeps creation, real verification and explicit enablement separate with no secret cache', async () => {
  await fill()
  expect(writes()).toHaveLength(0)
  await click('Create access')
  await until(() => expect(saved).toHaveBeenCalledTimes(1))
  expect(writes().map((r) => r.url)).toEqual(['/admin/providers'])
  expect(document.querySelector('input[type=password]')).toBeNull()
  expect(
    JSON.stringify(
      cache
        .getQueryCache()
        .getAll()
        .map((q) => q.state.data),
    ),
  ).not.toContain(secret)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  await until(() => expect(button('Verify and fetch models').disabled).toBe(false))
  await click('Verify and fetch models')
  await until(() => expect(button('Enable Credential').disabled).toBe(false))
  expect(writes()).toHaveLength(2)
  await click('Enable Credential')
  expect(writes()).toHaveLength(2)
  await click('Confirm enablement')
  await until(() => expect(document.body.textContent).toContain('Credential enabled'))
  expect(writes().map((r) => r.url)).toEqual([
    '/admin/providers',
    '/admin/credentials/crd_one/verify',
    '/admin/credentials/crd_one',
  ])
})
it('creates a Connection for an explicitly selected empty Provider ID without name guessing', async () => {
  await change('Provider selection', 'existing')
  await until(() =>
    expect(field('Provider').querySelector('option[value=prv_existing]')).toBeTruthy(),
  )
  await change('Provider', 'prv_existing')
  await change('Upstream Base URL', 'https://example.invalid/v1')
  await change('Upstream API Key', secret)
  await until(() =>
    expect(
      requests.some(
        (r) => r.url === '/admin/model-creation/providers' && r.params?.exact_id === 'prv_existing',
      ),
    ).toBe(true),
  )
  await click('Create access')
  await until(() => expect(saved).toHaveBeenCalledTimes(1))
  expect(writes()[0].url).toBe('/admin/providers/prv_existing/connections')
  expect(JSON.parse(writes()[0].data)).not.toHaveProperty('connection_name')
})
it('retains exact uncertain UUID/body/source after 503 and a policy change with fresh CSRF', async () => {
  await fill()
  status = 503
  await click('Create access')
  await until(() => expect(button('Retry original access creation')).toBeTruthy())
  const originalBody = writes()[0].data
  // Same mounted actor renewal; the original source must survive future policy change.
  context = { storage_source: 'vault', etag: 'c'.repeat(64) }
  basis = '2:2'
  await act(async () => cache.setQueryData(sessionKey, { ...identity, csrf_token: 'csrf-two' }))
  await render()
  await until(() => expect(button('Retry original access creation').disabled).toBe(false))
  status = 409
  await click('Retry original access creation')
  await until(() => expect(writes()).toHaveLength(2))
  expect(writes()[1].data).toBe(originalBody)
  expect(writes()[1].headers.get('X-CSRF-Token')).toBe('csrf-two')
  expect(button('Retry original access creation')).toBeTruthy()
  expect(saved).not.toHaveBeenCalled()
})
it('rejects late creation success after authority renewal without clearing uncertain intent', async () => {
  let release!: () => void
  hold = () =>
    new Promise<void>((resolve) => {
      release = resolve
    })
  await fill()
  await click('Create access')
  allowed = false
  await render()
  expect(field('Upstream API Key').closest('fieldset')!.hidden).toBe(true)
  await act(async () => release())
  expect(saved).not.toHaveBeenCalled()
  allowed = true
  basis = '2:2'
  await render()
  await until(() => expect(button('Retry original access creation')).toBeTruthy())
  expect(locked).toHaveBeenCalledWith(true)
})
it('destroys plaintext/intent on actor change and rejects an obsolete success', async () => {
  let release!: () => void
  hold = () =>
    new Promise<void>((resolve) => {
      release = resolve
    })
  await fill()
  await click('Create access')
  actor = 'usr_two'
  await act(async () =>
    cache.setQueryData(sessionKey, { ...identity, user: { ...identity.user, id: actor } }),
  )
  await render()
  await act(async () => release())
  await until(() => expect((field('Upstream API Key') as HTMLInputElement).value).toBe(''))
  expect(saved).not.toHaveBeenCalled()
  expect(button('Retry original access creation')).toBeUndefined()
})
it('keeps the API Key transient during live language switches and clears explicit discarded recovery', async () => {
  await fill()
  await act(async () => i18n.changeLanguage('zh'))
  expect((field('上游 API Key') as HTMLInputElement).value).toBe(secret)
  status = 503
  await click('创建接入')
  await until(() => expect(button('重试原始接入创建请求')).toBeTruthy())
  await click('丢弃本地恢复内容和 API Key')
  expect((field('上游 API Key') as HTMLInputElement).value).toBe('')
  expect(saved).not.toHaveBeenCalled()
  expect(document.body.textContent).toContain('这不会撤销可能已提交的创建')
})
it('does not dispatch invalid UUID or a lost write permission', async () => {
  await fill()
  vi.mocked(crypto.randomUUID).mockReturnValue(
    'bad' as `${string}-${string}-${string}-${string}-${string}`,
  )
  await click('Create access')
  expect(writes()).toHaveLength(0)
  allowed = false
  await render()
  expect(button('Create access').disabled).toBe(true)
})

it('requires explicit fresh facts after an unknown verification result and never auto-enables', async () => {
  await fill()
  await click('Create access')
  await until(() => expect(button('Verify and fetch models').disabled).toBe(false))
  verifyStatus = 503
  await click('Verify and fetch models')
  await until(() => expect(button('Verify and fetch models').disabled).toBe(true))
  expect(writes()).toHaveLength(2)
  expect(button('Enable Credential').disabled).toBe(true)
  await click('Refresh recorded access facts')
  await until(() => expect(button('Verify and fetch models').disabled).toBe(false))
  expect(writes()).toHaveLength(2)
  expect(button('Enable Credential').disabled).toBe(false)
})
it('renders already enabled facts without manufacturing a toggle or inference request', async () => {
  enabled = true
  await fill()
  await click('Create access')
  await until(() => expect(document.body.textContent).toContain('Credential enabled'))
  expect(button('Enable Credential')).toBeUndefined()
  expect(writes()).toHaveLength(1)
})
it('does not promote a native HTTP200 failed verification into eligibility', async () => {
  await fill()
  await click('Create access')
  await until(() => expect(button('Verify and fetch models').disabled).toBe(false))
  verifyResult = false
  await click('Verify and fetch models')
  await until(() => expect(document.body.textContent).toContain('Credential verification failed.'))
  expect(button('Enable Credential').disabled).toBe(true)
  expect(writes()).toHaveLength(2)
})
it('does not publish a late creation acknowledgement after unmount', async () => {
  let release!: () => void
  hold = () =>
    new Promise<void>((resolve) => {
      release = resolve
    })
  await fill()
  await click('Create access')
  await act(async () => root.unmount())
  disposed = true
  await act(async () => release())
  expect(saved).not.toHaveBeenCalled()
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it('requires an explicit fresh storage review after a definitive creation conflict', async () => {
  await fill()
  status = 409
  await click('Create access')
  await until(() => expect(button('Review current storage source')).toBeTruthy())
  expect(button('Create access').disabled).toBe(true)
  context = { storage_source: 'vault', etag: 'c'.repeat(64) }
  const readsBeforeReview = requests.filter(
    (request) => request.url === '/admin/provider-credential-storage-context',
  ).length
  await click('Review current storage source')
  await until(() => expect(button('Create access').disabled).toBe(false))
  expect(
    requests.filter((r) => r.url === '/admin/provider-credential-storage-context'),
  ).toHaveLength(readsBeforeReview + 1)
  status = 201
  await click('Create access')
  await until(() => expect(saved).toHaveBeenCalledTimes(1))
  expect(JSON.parse(writes()[1].data).storage_policy_etag).toBe(context.etag)
  expect(JSON.parse(writes()[1].data).secret).toBe(secret)
})

it('retains inline creation against a captured same-turn mode switch and retries its exact UUID', async () => {
  const inner = client.defaults.adapter as AxiosAdapter
  client.defaults.adapter = async (config) => {
    if (
      config.url === '/auth/session' ||
      config.url === '/auth/permissions' ||
      config.url === '/admin/model-creation/connections'
    ) {
      requests.push(config)
      return {
        config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders(),
        data:
          config.url === '/auth/session'
            ? identity
            : config.url === '/auth/permissions'
              ? {
                  permissions: [
                    'models.read_all',
                    'providers.read',
                    'models.write',
                    'providers.write',
                  ],
                }
              : { items: [], next_cursor: null },
      }
    }
    return inner(config)
  }
  const pageRouter = createMemoryRouter(
    [{ path: '/admin/models/create', element: <CreateModelPage /> }],
    { initialEntries: ['/admin/models/create'] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={pageRouter} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(button('Create access').disabled).toBe(false))
  await fill()
  const mode = button('Use an existing Connection')
  const propKey = Object.keys(mode).find((key) => key.startsWith('__reactProps'))!
  const captured = (Reflect.get(mode, propKey) as { onClick: () => void }).onClick
  let release!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  hold = () => pending
  status = 503
  try {
    await act(async () => {
      button('Create access').click()
      captured()
    })
    await until(() => expect(writes()).toHaveLength(1))
    expect(button('Use an existing Connection').getAttribute('aria-pressed')).toBe('false')
    expect(document.querySelector('input[type=password]')).toBeTruthy()
    const body = writes()[0].data
    release()
    await until(() => expect(host.textContent).toContain('Creation outcome is unknown'))
    await act(async () => captured())
    expect(button('Use an existing Connection').getAttribute('aria-pressed')).toBe('false')
    expect(writes()).toHaveLength(1)
    status = 409
    await click('Retry original access creation')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].data).toBe(body)
    expect(JSON.parse(writes()[1].data).request_id).toBe(JSON.parse(body).request_id)
    expect(host.textContent).toContain('Creation outcome is unknown')
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((query) => query.state.data),
      ),
    ).not.toContain(secret)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  } finally {
    release()
  }
})

it.each(['Session', 'permission', 'write revocation', 'storage', 'egress', 'storage replacement'])(
  'does not POST a secret through a captured enabled Create button after same-turn %s change',
  async (kind) => {
    await fill()
    const captured = button('Create access')
    expect(captured.disabled).toBe(false)
    await act(async () => {
      if (kind === 'write revocation')
        cache.setQueryData(permissionKey, ['models.read_all', 'providers.read'])
      else if (kind === 'storage replacement')
        cache.setQueryData(['model-creation', 'access', actor, basis, 'storage'], {
          storage_source: 'vault',
          etag: 'c'.repeat(64),
        })
      else {
        const key =
          kind === 'Session'
            ? sessionKey
            : kind === 'permission'
              ? permissionKey
              : [
                  'model-creation',
                  'access',
                  actor,
                  basis,
                  kind === 'storage' ? 'storage' : 'egresses',
                  ...(kind === 'egress' ? [''] : []),
                ]
        void cache.invalidateQueries({ queryKey: key, exact: true, refetchType: 'none' })
      }
      captured.click()
    })
    expect(writes()).toHaveLength(0)
    expect(saved).not.toHaveBeenCalled()
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  },
)
it.each(['Session', 'permission', 'storage', 'egress'])(
  'blocks a captured uncertain Retry after same-turn %s invalidation without replacing the intent',
  async (kind) => {
    await fill()
    status = 503
    await click('Create access')
    await until(() => expect(button('Retry original access creation').disabled).toBe(false))
    const originalBody = writes()[0].data
    const captured = button('Retry original access creation')
    await act(async () => {
      const key =
        kind === 'Session'
          ? sessionKey
          : kind === 'permission'
            ? permissionKey
            : [
                'model-creation',
                'access',
                actor,
                basis,
                kind === 'storage' ? 'storage' : 'egresses',
                ...(kind === 'egress' ? [''] : []),
              ]
      void cache.invalidateQueries({ queryKey: key, exact: true, refetchType: 'none' })
      captured.click()
    })
    expect(writes()).toHaveLength(1)
    expect(saved).not.toHaveBeenCalled()
    await act(async () => {
      cache.setQueryData(sessionKey, { ...identity, csrf_token: 'csrf-renewed' })
      cache.setQueryData(permissionKey, ['models.read_all', 'providers.read', 'providers.write'])
    })
    basis = 'fresh:review'
    await render()
    await until(() => expect(button('Retry original access creation').disabled).toBe(false))
    status = 409
    await click('Retry original access creation')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].data).toBe(originalBody)
    expect(writes()[1].headers.get('X-CSRF-Token')).toBe('csrf-renewed')
    expect(saved).not.toHaveBeenCalled()
  },
)
it('rechecks live storage after intent construction before posting a secret', async () => {
  await fill()
  vi.mocked(crypto.randomUUID).mockImplementation(() => {
    void cache.invalidateQueries({
      queryKey: ['model-creation', 'access', actor, basis, 'storage'],
      exact: true,
      refetchType: 'none',
    })
    return '11111111-1111-4111-8111-111111111111'
  })
  await click('Create access')
  expect(writes()).toHaveLength(0)
  expect(button('Retry original access creation')).toBeUndefined()
})

it('preserves an original uncertain existing-Provider request when a fresh bounded selection no longer contains it', async () => {
  await change('Provider selection', 'existing')
  await until(() =>
    expect(field('Provider').querySelector('option[value=prv_existing]')).toBeTruthy(),
  )
  await change('Provider', 'prv_existing')
  await change('Upstream Base URL', 'https://example.invalid/v1')
  await change('Upstream API Key', secret)
  await until(() =>
    expect(
      requests.some(
        (request) =>
          request.url === '/admin/model-creation/providers' &&
          request.params?.exact_id === 'prv_existing',
      ),
    ).toBe(true),
  )
  status = 503
  await click('Create access')
  await until(() => expect(button('Retry original access creation').disabled).toBe(false))
  const originalBody = writes()[0].data
  providerVisible = false
  context = { storage_source: 'vault', etag: 'c'.repeat(64) }
  basis = 'renewed:absent-selection'
  await act(async () => cache.setQueryData(sessionKey, { ...identity, csrf_token: 'csrf-renewed' }))
  await render()
  await until(() => expect(button('Retry original access creation').disabled).toBe(false))
  status = 409
  await click('Retry original access creation')
  await until(() => expect(writes()).toHaveLength(2))
  expect(writes()[1].url).toBe('/admin/providers/prv_existing/connections')
  expect(writes()[1].data).toBe(originalBody)
  expect(writes()[1].headers.get('X-CSRF-Token')).toBe('csrf-renewed')
  expect(saved).not.toHaveBeenCalled()
})

it('uses a bounded exact egress identity despite an earlier name containing the selected ID', async () => {
  await fill()
  await change('Network egress', 'proxy:egr_one')
  await until(() =>
    expect(
      requests.some(
        (request) =>
          request.url === '/admin/model-creation/egresses' &&
          request.params?.exact_id === 'egr_one',
      ),
    ).toBe(true),
  )
  await click('Create access')
  await until(() => expect(saved).toHaveBeenCalledTimes(1))
  expect(JSON.parse(writes()[0].data).egress_id).toBe('egr_one')
  const read = requests.find(
    (request) =>
      request.url === '/admin/model-creation/egresses' && request.params?.exact_id === 'egr_one',
  )!
  expect(read.params).toEqual({ exact_id: 'egr_one' })
})
