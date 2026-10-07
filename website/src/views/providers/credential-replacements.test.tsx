import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { CredentialMetadata } from '@/types/credential-metadata'
import CredentialReplacementDialog from './credential-replacements'
import ProvidersPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
const source = 'crd_01k00000000000000000000001'
const target = 'crd_01k00000000000000000000002'
const other = 'crd_01k00000000000000000000003'
const firstUuid = 'aaaa0000-0000-4000-8000-000000000001'
const secondUuid = 'aaaa0000-0000-4000-8000-000000000002'
let root: Root, host: HTMLDivElement, cache: QueryClient
let router: ReturnType<typeof createMemoryRouter> | undefined
let requests: InternalAxiosRequestConfig[], permissions: string[], record: CredentialMetadata
let failure: number, hold: Promise<void> | undefined, result: unknown, responseStatus: number
let lineage: boolean
let appendOnCreate: boolean, appended: boolean
const created = vi.fn(),
  closed = vi.fn()
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  router = undefined
  requests = []
  permissions = ['providers.read', 'providers.write']
  record = {
    id: source,
    connection_id: 'con_replace',
    name: 'Production',
    priority: 5,
    enabled: true,
    verification_status: 'verified',
    verified_at: null,
    etag: 'a'.repeat(64),
  }
  result = {
    id: target,
    connection_id: record.connection_id,
    replaces_credential_id: source,
    storage_source: 'inline',
  }
  responseStatus = 201
  failure = 0
  hold = undefined
  lineage = false
  appendOnCreate = false
  appended = false
  created.mockClear()
  closed.mockClear()
  vi.spyOn(crypto, 'randomUUID').mockReturnValueOnce(firstUuid).mockReturnValue(secondUuid)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session')
      response.data = { user: { id: 'usr_replace', role: 'member' }, csrf_token: 'replace-csrf' }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/admin/providers')
      response.data = {
        items: [
          {
            id: 'prv_replace',
            name: 'Provider',
            connections: [
              {
                id: 'con_replace',
                name: 'Primary',
                base_url: 'https://example.invalid',
                protocol: 'openai_chat',
                credentials: [
                  lineage
                    ? {
                        ...record,
                        id: target,
                        name: 'Replacement',
                        replaces_credential_id: source,
                        enabled: false,
                        verification_status: 'pending',
                      }
                    : structuredClone(record),
                  ...(appended
                    ? [
                        {
                          ...record,
                          id: target,
                          name: 'Replacement',
                          replaces_credential_id: source,
                          enabled: false,
                          verification_status: 'pending',
                        },
                      ]
                    : []),
                ],
                provider_models: [],
              },
            ],
          },
          { id: 'prv_other', name: 'Other', connections: [] },
        ],
      }
    else if (config.url === '/admin/provider-credential-storage-context') {
      response.data = { storage_source: 'inline', etag: 'b'.repeat(64) }
      response.headers.set('etag', `"${'b'.repeat(64)}"`)
    } else if (config.url === '/admin/models') response.data = { items: [] }
    else if (config.url?.endsWith('/metadata')) response.data = structuredClone(record)
    else if (config.url?.endsWith('/replacements')) {
      if (hold) await hold
      if (failure) {
        if (failure === -1) throw new AxiosError('Network unavailable', '', config)
        response.status = failure
        throw new AxiosError('Fixture rejection', '', config, undefined, response)
      }
      if (appendOnCreate) appended = true
      response.status = responseStatus
      response.data = result
    } else throw new Error(`Unexpected request: ${config.url}`)
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
  await i18n.changeLanguage('en')
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
async function mount(id = source, providerId = 'prv_replace') {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <CredentialReplacementDialog
          providerId={providerId}
          credentialId={id}
          connectionId="con_replace"
          connectionName="Primary"
          onCreated={created}
          onClose={closed}
        />
      </QueryClientProvider>,
    ),
  )
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button, [role="menuitem"]')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )!
}
function input(label: string) {
  return [...document.querySelectorAll('label')]
    .find((item) => item.textContent === label)!
    .querySelector<HTMLInputElement>('input')!
}
async function change(label: string, value: string) {
  await act(async () => {
    const control = input(label)
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(control, value)
    control.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function ready() {
  await mount()
  await until(() => expect(input('New API Key')).toBeTruthy())
}
async function draft() {
  await change('New credential name', '  Replacement  ')
  await change('New API Key', ' secret-value ')
  await change('Replacement reason', '  Scheduled rotation  ')
}
const writes = () => requests.filter((item) => item.method === 'post')

describe('credential replacement preparation', () => {
  it('sends a separate immutable intent with reviewed source, unmodified secret and no completion claim', async () => {
    await ready()
    await draft()
    await click('Create replacement')
    await until(() => expect(created).toHaveBeenCalledOnce())
    expect(writes()).toHaveLength(1)
    expect(writes()[0].url).toBe(`/admin/credentials/${source}/replacements`)
    expect(JSON.parse(writes()[0].data)).toEqual({
      request_id: firstUuid,
      storage_policy_etag: 'b'.repeat(64),
      name: 'Replacement',
      secret: ' secret-value ',
      reason: 'Scheduled rotation',
    })
    expect(writes()[0].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('replace-csrf')
    expect(input('New API Key').value).toBe('')
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((item) => item.state.data),
      ),
    ).not.toContain('secret-value')
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    expect(document.body.textContent).toContain('Pending · Disabled')
    expect(document.body.textContent).toContain('then explicitly enable it')
    expect(button('Complete rotation')).toBeUndefined()
    expect(record.enabled).toBe(true)
  })
  it('allows read-only preview while disabling creation and secret inputs', async () => {
    permissions = ['providers.read']
    await ready()
    expect(input('New API Key').disabled).toBe(true)
    expect(button('Create replacement').disabled).toBe(true)
    expect(writes()).toHaveLength(0)
  })
  it('does not fetch source metadata with write-only permission', async () => {
    permissions = ['providers.write']
    await mount()
    await until(() => expect(requests.some((item) => item.url === '/auth/permissions')).toBe(true))
    expect(requests.some((item) => item.url?.endsWith('/metadata'))).toBe(false)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
  it.each([
    ['New credential name', 'x'.repeat(101)],
    ['New credential name', 'bad\u0001name'],
    ['New API Key', 'x'.repeat(2049)],
    ['New API Key', ''],
    ['Replacement reason', '😀'.repeat(257)],
    ['Replacement reason', 'bad\u0001reason'],
  ])('rejects invalid %s input', async (label, value) => {
    await ready()
    await draft()
    await change(label, value)
    await click('Create replacement')
    expect(writes()).toHaveLength(0)
    expect(created).not.toHaveBeenCalled()
  })
  it('accepts rune-name and exact UTF-8 limits', async () => {
    await ready()
    await draft()
    await change('New credential name', '😀'.repeat(100))
    await change('New API Key', 'é'.repeat(1024))
    await change('Replacement reason', '😀'.repeat(256))
    await click('Create replacement')
    await until(() => expect(created).toHaveBeenCalledOnce())
  })
  it('fails locally when secure UUID generation is unavailable', async () => {
    vi.mocked(crypto.randomUUID)
      .mockReset()
      .mockImplementation(() => {
        throw new Error('unavailable')
      })
    await ready()
    await draft()
    await click('Create replacement')
    expect(writes()).toHaveLength(0)
    expect(document.body.textContent).toContain('could not be created')
  })
  it('rejects a malformed UUID generator result without a weak fallback', async () => {
    vi.mocked(crypto.randomUUID).mockReset().mockReturnValue('aaaa0000-0000-1000-8000-000000000001')
    await ready()
    await draft()
    await click('Create replacement')
    expect(writes()).toHaveLength(0)
    expect(document.body.textContent).toContain('could not be created')
  })
  it('clears an uncertain secret on dismissal without claiming creation', async () => {
    failure = 503
    await ready()
    await draft()
    await click('Create replacement')
    await until(() => expect(button('Retry exact creation')).toBeTruthy())
    await click('Cancel')
    expect(closed).toHaveBeenCalledOnce()
    expect(created).not.toHaveBeenCalled()
    expect(input('New API Key').value).toBe('')
  })
  it('guards duplicate dispatch while pending', async () => {
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await ready()
    await draft()
    await click('Create replacement')
    await click('Create replacement')
    expect(writes()).toHaveLength(1)
    expect(crypto.randomUUID).toHaveBeenCalledOnce()
    await act(async () => release())
    await until(() => expect(created).toHaveBeenCalledOnce())
  })
  it('preserves draft after a normal conflict and uses fresh reviewed source with a new intent', async () => {
    failure = 409
    await ready()
    await draft()
    await click('Create replacement')
    await until(() => expect(button('Review original credential')).toBeTruthy())
    expect(input('New API Key').value).toBe(' secret-value ')
    expect(button('Create replacement').disabled).toBe(true)
    record = { ...record, etag: 'b'.repeat(64), priority: 9 }
    await click('Review original credential')
    await until(() => expect(button('Create replacement').disabled).toBe(false))
    expect(input('New credential name').value).toBe('  Replacement  ')
    failure = 0
    await click('Create replacement')
    await until(() => expect(created).toHaveBeenCalledOnce())
    expect(JSON.parse(writes()[1].data).request_id).toBe(secondUuid)
    expect(writes()[1].headers.get('If-Match')).toBe(`"${'b'.repeat(64)}"`)
  })
  it.each([-1, 503])(
    'freezes uncertain %s intent and accepts exact durable retry receipt',
    async (status) => {
      failure = status
      await ready()
      await draft()
      await click('Create replacement')
      await until(() => expect(button('Retry exact creation')).toBeTruthy())
      expect(input('New API Key').disabled).toBe(true)
      expect(button('Review original credential')).toBeUndefined()
      record = { ...record, etag: 'b'.repeat(64) }
      failure = 0
      responseStatus = 200
      await click('Retry exact creation')
      await until(() => expect(created).toHaveBeenCalledOnce())
      expect(writes()[1].data).toBe(writes()[0].data)
      expect(writes()[1].headers.get('If-Match')).toBe(writes()[0].headers.get('If-Match'))
      expect(crypto.randomUUID).toHaveBeenCalledOnce()
    },
  )
  it.each([403, 409, 404])(
    'keeps original uncertainty through rejected retry %s',
    async (status) => {
      failure = 503
      await ready()
      await draft()
      await click('Create replacement')
      await until(() => expect(button('Retry exact creation')).toBeTruthy())
      failure = status
      await click('Retry exact creation')
      await until(() => expect(writes()).toHaveLength(2))
      expect(button('Review original credential')).toBeUndefined()
      expect(input('New API Key').disabled).toBe(true)
      failure = 0
      responseStatus = 200
      await click('Retry exact creation')
      await until(() => expect(created).toHaveBeenCalledOnce())
      expect(writes().map((item) => item.data)).toEqual([
        writes()[0].data,
        writes()[0].data,
        writes()[0].data,
      ])
    },
  )
  it.each([
    [
      'invalid ID',
      { id: 'crd_invalid', connection_id: 'con_replace', replaces_credential_id: source },
      201,
    ],
    [
      'source ID',
      { id: source, connection_id: 'con_replace', replaces_credential_id: source },
      201,
    ],
    [
      'wrong connection',
      { id: target, connection_id: 'con_other', replaces_credential_id: source },
      201,
    ],
    [
      'wrong source',
      { id: target, connection_id: 'con_replace', replaces_credential_id: other },
      201,
    ],
    [
      'extra secret',
      {
        id: target,
        connection_id: 'con_replace',
        replaces_credential_id: source,
        secret: 'unexpected',
      },
      201,
    ],
    [
      'wrong status',
      { id: target, connection_id: 'con_replace', replaces_credential_id: source },
      202,
    ],
  ])('treats %s receipt as uncertain', async (_label, body, status) => {
    result = body
    responseStatus = status
    await ready()
    await draft()
    await click('Create replacement')
    await until(() => expect(button('Retry exact creation')).toBeTruthy())
    expect(created).not.toHaveBeenCalled()
    expect(document.body.textContent).not.toContain('unexpected')
  })
  it.each(['id', 'connection_id', 'etag'])('rejects mismatched source %s', async (field) => {
    record = { ...record, [field]: 'wrong' }
    await mount()
    await until(() => expect(requests.some((item) => item.url?.endsWith('/metadata'))).toBe(true))
    expect(document.querySelector('input[type="password"]')).toBeNull()
  })
  it('clears sensitive draft on dismissal and resource navigation', async () => {
    await ready()
    await draft()
    await click('Cancel')
    expect(closed).toHaveBeenCalledOnce()
    expect(input('New API Key').value).toBe('')
    record = { ...record, id: other }
    await mount(other, 'prv_other')
    await until(() => expect(input('New API Key').value).toBe(''))
    expect(input('New credential name').value).toBe('')
    expect(input('Replacement reason').value).toBe('')
  })
  it('does not call completion after editor unmount', async () => {
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await ready()
    await draft()
    await click('Create replacement')
    await act(async () => root.render(null))
    await act(async () => release())
    expect(created).not.toHaveBeenCalled()
    expect(document.querySelector('input[type="password"]')).toBeNull()
  })
  it('switches language without clearing sensitive draft and translates existing uncertainty', async () => {
    await ready()
    await draft()
    failure = 503
    await click('Create replacement')
    await until(() => expect(button('Retry exact creation')).toBeTruthy())
    await act(async () => i18n.changeLanguage('zh'))
    expect(input('新 API Key').value).toBe(' secret-value ')
    expect(button('重试原创建请求')).toBeTruthy()
    expect(document.body.textContent).toContain('创建结果尚未确认')
  })
  it('waits for a receipt and server catalogue refresh before showing a pending replacement', async () => {
    appendOnCreate = true
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    router = createMemoryRouter(
      [{ path: '/admin/providers/:providerId', element: <ProvidersPage /> }],
      { initialEntries: ['/admin/providers/prv_replace?tab=credentials'] },
    )
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router!} />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(button('Actions for Production')).toBeTruthy())
    await click('Actions for Production')
    await click('Create replacement')
    await until(() => expect(input('New API Key')).toBeTruthy())
    await draft()
    await click('Create replacement')
    expect(button('Actions for Replacement')).toBeUndefined()
    await act(async () => release())
    await until(() => expect(button('Actions for Replacement')).toBeTruthy())
    expect(button('Actions for Production')).toBeTruthy()
    expect(document.body.textContent).toContain('saved as pending and disabled')
    expect(document.querySelector('input[type="password"]')).toBeNull()
    expect(requests.some((item) => item.method === 'patch')).toBe(false)
    expect(requests.filter((item) => item.url === '/admin/providers').length).toBeGreaterThan(1)
  })
  it('does not impose a single-successor restriction on an existing replacement', async () => {
    lineage = true
    record = { ...record, id: target, name: 'Replacement' }
    router = createMemoryRouter(
      [{ path: '/admin/providers/:providerId', element: <ProvidersPage /> }],
      { initialEntries: ['/admin/providers/prv_replace?tab=credentials'] },
    )
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router!} />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(button('Actions for Replacement')).toBeTruthy())
    await click('Actions for Replacement')
    expect(button('Create replacement').getAttribute('aria-disabled')).not.toBe('true')
    await click('Create replacement')
    await until(() => expect(input('New API Key')).toBeTruthy())
    expect(input('New API Key').disabled).toBe(false)
  })
  it('shows immutable predecessor ID without fetching a deleted source and retains row workflow', async () => {
    lineage = true
    router = createMemoryRouter(
      [{ path: '/admin/providers/:providerId', element: <ProvidersPage /> }],
      { initialEntries: ['/admin/providers/prv_replace?tab=credentials'] },
    )
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router!} />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(document.body.textContent).toContain(`Replaces credential ${source}`))
    expect(requests.some((item) => item.url?.endsWith('/metadata'))).toBe(false)
    await click('Actions for Replacement')
    expect(button('Verify')).toBeTruthy()
    expect(button('Enable').getAttribute('aria-disabled')).toBe('true')
    expect(button('Create replacement')).toBeTruthy()
    await click('Create replacement')
    await until(() =>
      expect(requests.some((item) => item.url === `/admin/credentials/${target}/metadata`)).toBe(
        true,
      ),
    )
    expect(document.querySelector('input[type="password"]')).toBeNull()
  })
})
