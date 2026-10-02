import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import ProvidersPage from './index'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { CredentialMetadata } from '@/types/credential-metadata'
import CredentialMetadataDialog from './credential-metadata'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
const path = '/admin/credentials/crd_metadata/metadata'
const originalEtag = 'a'.repeat(64),
  savedEtag = 'b'.repeat(64),
  concurrentEtag = 'c'.repeat(64),
  reviewedEtag = 'd'.repeat(64)
let root: Root, host: HTMLDivElement, cache: QueryClient
let router: ReturnType<typeof createMemoryRouter> | undefined
let requests: InternalAxiosRequestConfig[], permissions: string[], record: CredentialMetadata
let failure: number, hold: Promise<void> | undefined
let resultOverride: unknown, resultStatus: number
const saved = vi.fn(),
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
    id: 'crd_metadata',
    connection_id: 'con_metadata',
    name: 'Production',
    priority: 5,
    enabled: true,
    verification_status: 'verified',
    verified_at: '2026-10-02T08:00:00Z',
    etag: originalEtag,
  }
  resultOverride = undefined
  resultStatus = 200
  failure = 0
  hold = undefined
  saved.mockClear()
  closed.mockClear()
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
      response.data = { user: { id: 'usr_metadata', role: 'member' }, csrf_token: 'metadata-csrf' }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/admin/providers')
      response.data = {
        items: [
          {
            id: 'prv_metadata',
            name: 'Provider',
            connections: [
              {
                id: 'con_metadata',
                name: 'Primary',
                base_url: 'https://provider.example.invalid',
                protocol: 'openai_chat',
                credentials: [structuredClone(record)],
                provider_models: [],
              },
            ],
          },
          { id: 'prv_other', name: 'Other provider', connections: [] },
        ],
      }
    else if (config.url === '/admin/models') response.data = { items: [] }
    else if (config.url?.endsWith('/metadata')) {
      if (config.method === 'put') {
        if (hold) await hold
        if (failure) {
          if (failure === -1) throw new AxiosError('Network result unavailable', '', config)
          response.status = failure
          throw new AxiosError('Metadata fixture', '', config, undefined, response)
        }
        const body = JSON.parse(config.data)
        record = { ...record, name: body.name, priority: body.priority, etag: savedEtag }
      }
      response.data =
        config.method === 'put' && resultOverride !== undefined
          ? resultOverride
          : structuredClone(record)
      if (config.method === 'put') response.status = resultStatus
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
async function mount(id = 'crd_metadata', providerId = 'prv_metadata') {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <CredentialMetadataDialog
          providerId={providerId}
          providerName="Provider"
          connectionId="con_metadata"
          connectionName="Primary"
          credentialId={id}
          onSaved={saved}
          onClose={closed}
        />
      </QueryClientProvider>,
    ),
  )
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLElement>('button, [role="menuitem"]')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )! as HTMLButtonElement
}
function input(label: string) {
  return [...document.querySelectorAll('label')]
    .find((item) => item.textContent === label)!
    .querySelector<HTMLInputElement>('input')!
}
async function change(label: string, value: string) {
  const control = input(label)
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(control, value)
    control.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function ready() {
  await mount()
  await until(() => expect(input('Credential name').value).toBe('Production'))
}
async function draft() {
  await change('Credential name', '  Renamed  ')
  await change('Priority', '0')
  await change('Change reason', '  Pool ordering  ')
}
async function click(label: string) {
  await act(async () => button(label).click())
}
const writes = () => requests.filter((request) => request.method === 'put')

describe('credential metadata editing', () => {
  it('loads an exact scoped resource and sends only trimmed metadata, integer zero, ETag and CSRF', async () => {
    await ready()
    expect(requests.filter((request) => request.url === path)).toHaveLength(1)
    expect(document.querySelector('input[type="password"]')).toBeNull()
    expect(document.body.textContent).toContain('supplier secret')
    expect(document.body.textContent).toContain('Connection: con_metadata')
    await draft()
    await click('Save changes')
    await until(() => expect(saved).toHaveBeenCalledOnce())
    expect(writes()[0].url).toBe(path)
    expect(JSON.parse(writes()[0].data)).toEqual({
      name: 'Renamed',
      priority: 0,
      reason: 'Pool ordering',
    })
    expect(writes()[0].headers.get('If-Match')).toBe(`"${originalEtag}"`)
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('metadata-csrf')
  })
  it.each(['', '-1', '1.5', '10001', '1e2', '9007199254740993', 'bad'])(
    'rejects malformed or out-of-range priority %s',
    async (priority) => {
      await ready()
      await draft()
      await change('Priority', priority)
      await click('Save changes')
      expect(writes()).toHaveLength(0)
      expect(document.body.textContent).toContain('whole-number priority')
    },
  )
  it('validates names and required bounded UTF-8 reasons without changing authority', async () => {
    await ready()
    await change('Credential name', ' ')
    await click('Save changes')
    expect(document.body.textContent).toContain('1–100 characters')
    await change('Credential name', '好'.repeat(100))
    await change('Priority', '10000')
    await click('Save changes')
    expect(document.body.textContent).toContain('Enter a reason')
    await change('Change reason', '好'.repeat(342))
    await click('Save changes')
    expect(writes()).toHaveLength(0)
    await change('Change reason', 'line\tbreak')
    await click('Save changes')
    expect(writes()).toHaveLength(0)
    await change('Change reason', 'Valid reason')
    await click('Save changes')
    await until(() => expect(saved).toHaveBeenCalledOnce())
    expect(JSON.parse(writes()[0].data).priority).toBe(10000)
  })
  it('does not request metadata without read permission and disables all edits without write permission', async () => {
    permissions = ['providers.write']
    await mount()
    await until(() =>
      expect(requests.some((request) => request.url === '/auth/permissions')).toBe(true),
    )
    expect(requests.some((request) => request.url === path)).toBe(false)
    permissions = ['providers.read']
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['permissions'] })
    })
    await until(() => expect(button('Save changes')?.disabled).toBe(true))
    expect(input('Credential name').disabled).toBe(true)
    expect(writes()).toHaveLength(0)
  })
  it('retains the draft after 409 and requires explicit latest-record review before another write', async () => {
    await ready()
    await draft()
    failure = 409
    await click('Save changes')
    await until(() => expect(button('Review latest metadata')).toBeDefined())
    expect(button('Save changes').disabled).toBe(true)
    record = { ...record, name: 'Concurrent name', priority: 9, etag: concurrentEtag }
    await click('Review latest metadata')
    await until(() => expect(document.body.textContent).toContain('Current priority: 9'))
    expect(input('Credential name').value).toBe('  Renamed  ')
    expect(input('Priority').value).toBe('0')
    expect(input('Change reason').value).toBe('  Pool ordering  ')
    failure = 0
    await click('Save changes')
    await until(() => expect(saved).toHaveBeenCalledOnce())
    expect(writes()[1].headers.get('If-Match')).toBe(`"${concurrentEtag}"`)
  })
  it.each([403, 409])(
    'preserves immutable uncertain intent through 503 then rejected %s retry',
    async (rejection) => {
      await ready()
      await draft()
      failure = 503
      await click('Save changes')
      await until(() => expect(button('Retry exact request')).toBeDefined())
      expect(input('Credential name').disabled).toBe(true)
      expect(button('Save changes').disabled).toBe(true)
      failure = rejection
      await click('Retry exact request')
      await until(() => expect(writes()).toHaveLength(2))
      expect(button('Save changes').disabled).toBe(true)
      failure = 0
      await click('Retry exact request')
      await until(() => expect(saved).toHaveBeenCalledOnce())
      expect(writes()).toHaveLength(3)
      for (const request of writes()) {
        expect(request.data).toBe(writes()[0].data)
        expect(request.headers.get('If-Match')).toBe(`"${originalEtag}"`)
      }
    },
  )
  it('allows explicit latest review after a network uncertainty and retains localized draft messages', async () => {
    await ready()
    await draft()
    failure = -1
    await click('Save changes')
    await until(() => expect(button('Retry exact request')).toBeDefined())
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('保存结果尚未确认')
    expect(input('凭证名称').value).toBe('  Renamed  ')
    record = { ...record, etag: reviewedEtag, name: 'Observed record' }
    await click('检查最新信息')
    await until(() => expect(button('保存更改').disabled).toBe(false))
    expect(document.body.textContent).toContain('已检查最新信息')
    expect(input('变更原因').value).toBe('  Pool ordering  ')
    failure = 0
    await click('保存更改')
    await until(() => expect(saved).toHaveBeenCalledOnce())
    expect(writes()[1].headers.get('If-Match')).toBe(`"${reviewedEtag}"`)
  })
  it('blocks new intent after an authoritative record refresh and resets state for a different credential', async () => {
    await ready()
    await draft()
    record = { ...record, etag: 'e'.repeat(64), name: 'Remote name' }
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['admin', 'credential-metadata'] })
    })
    await until(() => expect(button('Save changes').disabled).toBe(true))
    expect(input('Credential name').value).toBe('  Renamed  ')
    record = { ...record, id: 'crd_other', name: 'Other credential' }
    await mount('crd_other')
    await until(() => expect(input('Credential name').value).toBe('Other credential'))
    expect(input('Change reason').value).toBe('')
    expect(writes()).toHaveLength(0)
  })
  it('rejects an unexpected authoritative identity rather than attaching its ETag to another credential', async () => {
    record.id = 'crd_other'
    await mount()
    await until(() => expect(document.querySelector('[role="alert"]')).not.toBeNull())
    expect(button('Save changes')).toBeUndefined()
    expect(writes()).toHaveLength(0)
  })
  it.each(['weak', 'A'.repeat(64), 'a'.repeat(63)])(
    'rejects an invalid metadata GET ETag: %s',
    async (etag) => {
      record.etag = etag
      await mount()
      await until(() => expect(document.querySelector('[role="alert"]')).not.toBeNull())
      expect(button('Save changes')).toBeUndefined()
      expect(writes()).toHaveLength(0)
    },
  )
  it.each([
    'null',
    'empty',
    'wrong-id',
    'wrong-name',
    'wrong-priority',
    'invalid-etag',
    'wrong-status',
  ])('retains uncertainty after a malformed or mismatching successful save: %s', async (kind) => {
    await ready()
    await draft()
    const valid = { ...record, name: 'Renamed', priority: 0, etag: savedEtag }
    if (kind === 'null') resultOverride = null
    else if (kind === 'empty') resultOverride = {}
    else if (kind === 'wrong-id') resultOverride = { ...valid, id: 'crd_other' }
    else if (kind === 'wrong-name') resultOverride = { ...valid, name: 'Other name' }
    else if (kind === 'wrong-priority') resultOverride = { ...valid, priority: 9 }
    else if (kind === 'invalid-etag') resultOverride = { ...valid, etag: 'bad' }
    else {
      resultOverride = valid
      resultStatus = 201
    }
    await click('Save changes')
    await until(() => expect(button('Retry exact request')).toBeDefined())
    expect(saved).not.toHaveBeenCalled()
    expect(button('Save changes').disabled).toBe(true)
    expect(input('Credential name').disabled).toBe(true)
    resultOverride = undefined
    resultStatus = 200
    await click('Retry exact request')
    await until(() => expect(saved).toHaveBeenCalledOnce())
    expect(writes()[1].data).toBe(writes()[0].data)
    expect(writes()[1].headers.get('If-Match')).toBe(`"${originalEtag}"`)
  })
  it('opens the exact table credential and refreshes the catalog only after confirmed save', async () => {
    router = createMemoryRouter(
      [{ path: '/admin/providers/:providerId', element: <ProvidersPage /> }],
      { initialEntries: ['/admin/providers/prv_metadata?tab=credentials'] },
    )
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router!} />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(button('Actions for Production')).toBeDefined())
    expect(requests.filter((request) => request.url === path)).toHaveLength(0)
    await click('Actions for Production')
    await until(() => expect(button('Edit')).toBeDefined())
    await click('Edit')
    await until(() => expect(input('Credential name').value).toBe('Production'))
    await draft()
    await click('Save changes')
    await until(() =>
      expect(document.body.textContent).toContain(
        'Credential metadata saved and runtime publication confirmed.',
      ),
    )
    expect(writes()[0].url).toBe(path)
    await until(() => expect(host.querySelector('tbody')?.textContent).toContain('Renamed'))
    expect(requests.filter((request) => request.url === '/admin/providers').length).toBeGreaterThan(
      1,
    )
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
  it('destroys the table dialog when leaving a Provider and does not reopen it on return', async () => {
    router = createMemoryRouter(
      [{ path: '/admin/providers/:providerId', element: <ProvidersPage /> }],
      { initialEntries: ['/admin/providers/prv_metadata?tab=credentials'] },
    )
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router!} />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(button('Actions for Production')).toBeDefined())
    await click('Actions for Production')
    await until(() => expect(button('Edit')).toBeDefined())
    await click('Edit')
    await until(() => expect(input('Credential name').value).toBe('Production'))
    await draft()
    await act(async () => router!.navigate('/admin/providers/prv_other?tab=credentials'))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    await act(async () => router!.navigate('/admin/providers/prv_metadata?tab=credentials'))
    await until(() => expect(button('Actions for Production')).toBeDefined())
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    await click('Actions for Production')
    await until(() => expect(button('Edit')).toBeDefined())
    await click('Edit')
    await until(() => expect(input('Credential name').value).toBe('Production'))
    expect(input('Change reason').value).toBe('')
    expect(writes()).toHaveLength(0)
  })
  it('guards duplicate submit and ignores completion after unmount', async () => {
    await ready()
    await draft()
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await click('Save changes')
    await click('Save changes')
    expect(writes()).toHaveLength(1)
    expect(button('Cancel').disabled).toBe(true)
    await act(async () => root.render(null))
    await act(async () => release())
    expect(saved).not.toHaveBeenCalled()
  })
})
