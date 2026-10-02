import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { CredentialMetadata } from '@/types/credential-metadata'
import CredentialDeleteDialog from './credential-delete'
import ProvidersPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
const id = 'crd_delete',
  path = `/admin/credentials/${id}`,
  etag = 'a'.repeat(64)
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter> | undefined
let record: CredentialMetadata, permissions: string[], requests: InternalAxiosRequestConfig[]
let failure: number,
  getFailure: number,
  absent: boolean,
  result: unknown,
  hold: Promise<void> | undefined
const deleted = vi.fn(),
  closed = vi.fn()
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  router = undefined
  record = {
    id,
    connection_id: 'con_delete',
    name: 'Production',
    priority: 0,
    enabled: true,
    verification_status: 'verified',
    verified_at: null,
    etag,
  }
  permissions = ['providers.read', 'providers.write']
  requests = []
  failure = 0
  getFailure = 0
  absent = false
  result = undefined
  hold = undefined
  deleted.mockClear()
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
      response.data = { user: { id: 'usr_delete', role: 'member' }, csrf_token: 'delete-csrf' }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/admin/providers')
      response.data = {
        items: [
          {
            id: 'prv_delete',
            name: 'Provider',
            connections: [
              {
                id: 'con_delete',
                name: 'Primary',
                base_url: 'https://provider.example.invalid',
                protocol: 'openai_chat',
                credentials: absent ? [] : [structuredClone(record)],
                provider_models: [],
              },
            ],
          },
          { id: 'prv_other', name: 'Other provider', connections: [] },
        ],
      }
    else if (config.url === '/admin/models') response.data = { items: [] }
    else if (config.url?.endsWith('/metadata')) {
      if (getFailure) {
        response.status = getFailure
        throw new AxiosError('Preview failed', '', config, undefined, response)
      }
      response.data = structuredClone(record)
    } else if (config.url === path && config.method === 'delete') {
      if (hold) await hold
      if (failure) {
        if (failure === 503) absent = true
        if (failure === -1) throw new AxiosError('Network result unavailable', '', config)
        response.status = failure
        throw new AxiosError('Deletion failed', '', config, undefined, response)
      }
      absent = true
      response.data = result === undefined ? { id, absent: true, runtime_applied: true } : result
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
function button(label: string) {
  return [...document.querySelectorAll<HTMLElement>('button,[role="menuitem"]')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )! as HTMLButtonElement
}
function input(label = 'Deletion reason') {
  return [...document.querySelectorAll('label')]
    .find((item) => item.textContent === label)!
    .querySelector<HTMLInputElement>('input')!
}
async function change(value: string, label = 'Deletion reason') {
  const control = input(label)
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(control, value)
    control.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function click(label: string) {
  await act(async () => button(label).click())
}
const writes = () => requests.filter((request) => request.method === 'delete')
async function mount(credentialId = id, providerId = 'prv_delete') {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <CredentialDeleteDialog
          providerId={providerId}
          credentialId={credentialId}
          connectionId="con_delete"
          connectionName="Primary"
          onDeleted={deleted}
          onClose={closed}
        />
      </QueryClientProvider>,
    ),
  )
}
async function ready() {
  await mount()
  await until(() => expect(button('Confirm deletion')).toBeDefined())
}
async function page() {
  router = createMemoryRouter(
    [{ path: '/admin/providers/:providerId', element: <ProvidersPage /> }],
    { initialEntries: ['/admin/providers/prv_delete?tab=credentials'] },
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
  await until(() => expect(button('Delete credential')).toBeDefined())
  await click('Delete credential')
  await until(() => expect(button('Confirm deletion')).toBeDefined())
}

describe('credential deletion confirmation', () => {
  it('previews only the exact resource and sends a confirmed DELETE with trimmed reason, exact ETag and CSRF', async () => {
    await ready()
    expect(requests.filter((request) => request.url === `${path}/metadata`)).toHaveLength(1)
    expect(writes()).toHaveLength(0)
    expect(document.body.textContent).toContain(
      'Already dispatched requests and historical records remain.',
    )
    expect(document.body.textContent).toContain('Connection: Primary · con_delete')
    expect(document.querySelector('input[type="password"]')).toBeNull()
    await change('  Retire configuration  ')
    await click('Confirm deletion')
    await until(() => expect(deleted).toHaveBeenCalledOnce())
    expect(writes()[0].url).toBe(path)
    expect(JSON.parse(writes()[0].data)).toEqual({ reason: 'Retire configuration' })
    expect(writes()[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('delete-csrf')
  })
  it.each(['', ' ', 'bad\tcontrol', '好'.repeat(342)])(
    'rejects missing, control-containing or oversized reason',
    async (reason) => {
      await ready()
      await change(reason)
      await click('Confirm deletion')
      expect(writes()).toHaveLength(0)
      expect(document.body.textContent).toContain('Enter a reason')
    },
  )
  it('requires read permission for previews and independent write permission for confirmation', async () => {
    permissions = ['providers.write']
    await mount()
    await until(() =>
      expect(requests.some((request) => request.url === '/auth/permissions')).toBe(true),
    )
    expect(requests.some((request) => request.url?.endsWith('/metadata'))).toBe(false)
    permissions = ['providers.read']
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['permissions'] })
    })
    await until(() => expect(button('Confirm deletion')?.disabled).toBe(true))
    expect(input().disabled).toBe(true)
    expect(writes()).toHaveLength(0)
  })
  it.each(['wrong-id', 'wrong-connection', 'invalid-etag'])(
    'rejects invalid preview identity or ETag: %s',
    async (kind) => {
      if (kind === 'wrong-id') record.id = 'crd_other'
      else if (kind === 'wrong-connection') record.connection_id = 'con_other'
      else record.etag = 'weak'
      await mount()
      await until(() => expect(document.querySelector('[role="alert"]')).not.toBeNull())
      expect(button('Confirm deletion')).toBeUndefined()
      expect(writes()).toHaveLength(0)
    },
  )
  it('requires explicit review after stale 409, preserving the reason before reconfirming a new ETag', async () => {
    await ready()
    await change('Remove retired entry')
    failure = 409
    await click('Confirm deletion')
    await until(() => expect(button('Review current credential')).toBeDefined())
    expect(button('Confirm deletion').disabled).toBe(true)
    record = { ...record, name: 'Concurrent name', etag: 'b'.repeat(64) }
    await click('Review current credential')
    await until(() => expect(document.body.textContent).toContain('Concurrent name'))
    expect(input().value).toBe('Remove retired entry')
    failure = 0
    await click('Confirm deletion')
    await until(() => expect(deleted).toHaveBeenCalledOnce())
    expect(writes()[1].headers.get('If-Match')).toBe(`"${'b'.repeat(64)}"`)
  })
  it.each([403, 409])(
    'keeps uncertain immutable deletion through 503 then rejected %s retry',
    async (status) => {
      await ready()
      await change('Retire entry')
      failure = 503
      await click('Confirm deletion')
      await until(() => expect(button('Retry exact deletion')).toBeDefined())
      expect(deleted).not.toHaveBeenCalled()
      expect(input().disabled).toBe(true)
      failure = status
      await click('Retry exact deletion')
      await until(() => expect(writes()).toHaveLength(2))
      expect(button('Confirm deletion').disabled).toBe(true)
      failure = 0
      await click('Retry exact deletion')
      await until(() => expect(deleted).toHaveBeenCalledOnce())
      expect(writes()).toHaveLength(3)
      for (const request of writes()) {
        expect(request.data).toBe(writes()[0].data)
        expect(request.url).toBe(path)
        expect(request.headers.get('If-Match')).toBe(`"${etag}"`)
      }
    },
  )
  it('never treats preview GET404 as deletion/publication success and retains the exact retry', async () => {
    await ready()
    await change('Retire entry')
    failure = -1
    await click('Confirm deletion')
    await until(() => expect(button('Retry exact deletion')).toBeDefined())
    getFailure = 404
    await click('Review current credential')
    await until(() =>
      expect(document.body.textContent).toContain(
        'This does not confirm runtime publication or the original deletion.',
      ),
    )
    expect(deleted).not.toHaveBeenCalled()
    expect(button('Confirm deletion').disabled).toBe(true)
    failure = 0
    await click('Retry exact deletion')
    await until(() => expect(deleted).toHaveBeenCalledOnce())
    expect(writes()[1].data).toBe(writes()[0].data)
  })
  it('does not offer deletion when the initial preview is 404', async () => {
    getFailure = 404
    await mount()
    await until(() => expect(document.querySelector('[role="alert"]')).not.toBeNull())
    expect(button('Confirm deletion')).toBeUndefined()
    expect(deleted).not.toHaveBeenCalled()
    expect(writes()).toHaveLength(0)
  })
  it.each([
    null,
    {},
    { id: 'wrong', absent: true, runtime_applied: true },
    { id, absent: false, runtime_applied: true },
    { id, absent: true, runtime_applied: false },
  ])(
    'requires authoritative matching absence and applied runtime in 200 response',
    async (data) => {
      await ready()
      await change('Retire entry')
      result = data
      await click('Confirm deletion')
      await until(() => expect(button('Retry exact deletion')).toBeDefined())
      expect(deleted).not.toHaveBeenCalled()
      result = undefined
      await click('Retry exact deletion')
      await until(() => expect(deleted).toHaveBeenCalledOnce())
      expect(writes()[1].data).toBe(writes()[0].data)
    },
  )
  it('keeps invalid 400 editable and preserves the reason and notices through live Chinese switching', async () => {
    await ready()
    await change('Retire entry')
    failure = 400
    await click('Confirm deletion')
    await until(() => expect(document.querySelector('[role="alert"]')).not.toBeNull())
    expect(input().disabled).toBe(false)
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('凭证配置无法恢复')
    expect(input('删除原因').value).toBe('Retire entry')
    failure = 503
    await click('确认删除')
    await until(() => expect(document.body.textContent).toContain('删除结果尚未确认'))
    await act(async () => i18n.changeLanguage('en'))
    expect(document.body.textContent).toContain('The deletion result is uncertain.')
  })
  it('explicitly reviews a still-existing record after uncertainty, preserving reason and allowing new confirmation', async () => {
    await ready()
    await change('Retire entry')
    failure = -1
    await click('Confirm deletion')
    await until(() => expect(button('Retry exact deletion')).toBeDefined())
    record.etag = 'c'.repeat(64)
    await click('Review current credential')
    await until(() => expect(button('Confirm deletion').disabled).toBe(false))
    expect(input().value).toBe('Retire entry')
    expect(button('Retry exact deletion')).toBeUndefined()
    failure = 0
    await click('Confirm deletion')
    await until(() => expect(deleted).toHaveBeenCalledOnce())
    expect(writes()[1].headers.get('If-Match')).toBe(`"${'c'.repeat(64)}"`)
  })
  it('does not remove a table row optimistically and only refreshes after confirmed absence/publication', async () => {
    await page()
    await change('Retire entry')
    failure = 503
    await click('Confirm deletion')
    await until(() => expect(button('Retry exact deletion')).toBeDefined())
    expect(host.querySelector('tbody')?.textContent).toContain('Production')
    expect(requests.filter((request) => request.url === '/admin/providers')).toHaveLength(1)
    failure = 0
    await click('Retry exact deletion')
    await until(() =>
      expect(host.textContent).toContain(
        'Credential is absent and runtime publication is confirmed.',
      ),
    )
    await until(() => expect(host.querySelector('tbody')?.textContent).not.toContain('Production'))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
  it('resets on cancellation and Provider navigation without reopening a previous deletion', async () => {
    await page()
    await change('Draft reason')
    await click('Cancel')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    await click('Actions for Production')
    await until(() => expect(button('Delete credential')).toBeDefined())
    await click('Delete credential')
    await until(() => expect(input().value).toBe(''))
    await change('Second draft')
    await act(async () => router!.navigate('/admin/providers/prv_other?tab=credentials'))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    await act(async () => router!.navigate('/admin/providers/prv_delete?tab=credentials'))
    await until(() => expect(button('Actions for Production')).toBeDefined())
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(writes()).toHaveLength(0)
  })
  it('guards duplicate confirmation and ignores deletion completion after unmount', async () => {
    await ready()
    await change('Retire entry')
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await click('Confirm deletion')
    await click('Confirm deletion')
    expect(writes()).toHaveLength(1)
    expect(button('Cancel').disabled).toBe(true)
    await act(async () => root.render(null))
    await act(async () => release())
    expect(deleted).not.toHaveBeenCalled()
  })
})
