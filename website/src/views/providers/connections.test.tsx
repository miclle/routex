import { act, useEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider, useParams } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import AuthGate from '@/components/app/AuthGate'
import { UncertainIntentProvider } from '@/context/uncertain-intents'
import { sessionKey, useSession } from '@/hooks/use-auth'
import ConnectionTable from './connections'
import ProvidersPage from './index'
import type { ConnectionMetadata } from '@/types/connection-metadata'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
const token = `${'a'.repeat(64)}.${'b'.repeat(64)}`
const nextToken = `${'a'.repeat(64)}.${'c'.repeat(64)}`
let root: Root, host: HTMLDivElement, cache: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[], permissions: string[], actor: string, csrf: string
let sessionStatus: number, permissionStatus: number, metadataStatus: number, failure: number
let record: ConnectionMetadata, hold: Promise<void> | undefined
let mounts: number, unmounts: number
let enabled: boolean
const add = vi.fn()
function Host() {
  const session = useSession()
  const { providerId = 'prv_one' } = useParams()
  useEffect(() => {
    mounts++
    return () => {
      unmounts++
    }
  }, [])
  return <ConnectionTable key={providerId} providerId={providerId} session={session} onAdd={add} />
}
function providers() {
  const connection = (id: string, name: string, protocol: string) => ({
    id,
    enabled: id === 'con_one' ? enabled : id !== 'con_two',
    name,
    protocol,
    base_url: 'https://api.example.invalid/v1',
    egress_mode: 'direct',
    egress_id: null,
    etag: 'raw-revision',
    credentials: [],
    provider_models: [],
  })
  return [
    {
      id: 'prv_one',
      name: 'Supplier',
      connections: [
        connection('con_one', record.name, 'openai_chat'),
        connection('con_two', 'Alpha [literal]', 'openai_responses'),
        connection('con_three', 'Gamma', 'anthropic_messages'),
      ],
    },
    {
      id: 'prv_two',
      name: 'Second supplier',
      connections: [connection('con_other', 'Other', 'gemini_generate_content')],
    },
  ]
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  requests = []
  actor = 'usr_actor'
  csrf = 'csrf-original'
  permissions = ['providers.read', 'providers.write']
  sessionStatus = 200
  permissionStatus = 200
  metadataStatus = 200
  failure = 0
  enabled = true
  hold = undefined
  mounts = 0
  unmounts = 0
  add.mockClear()
  record = {
    id: 'con_one',
    provider_id: 'prv_one',
    name: 'Alpha primary',
    protocol: 'openai_chat',
    base_url: 'https://api.example.invalid/v1',
    egress_mode: 'direct',
    egress_id: null,
    etag: token,
    can_edit: true,
  }
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    const reject = (status: number) => {
      throw new AxiosError('Controlled response unavailable', '', config, undefined, {
        ...response,
        status,
      })
    }
    if (config.url === '/setup') response.data = { initialized: true }
    else if (config.url === '/auth/session') {
      if (sessionStatus !== 200) reject(sessionStatus)
      response.data = {
        user: { id: actor, role: 'admin', name: 'Actor', email: 'actor@example.invalid' },
        csrf_token: csrf,
      }
    } else if (config.url === '/auth/permissions') {
      if (permissionStatus !== 200) reject(permissionStatus)
      response.data = { permissions }
    } else if (config.url === '/admin/connections/con_one/status') {
      response.headers.set('Cache-Control', 'private, no-store')
      response.headers.set('ETag', `"${record.etag}"`)
      if (config.method === 'put') {
        if (failure) reject(failure)
        const input = JSON.parse(config.data) as { enabled: boolean; reason: string }
        enabled = input.enabled
        response.data = { connection: { ...record, enabled }, runtime_applied: true, changed: true }
      } else {
        if (hold) await hold
        if (metadataStatus !== 200) reject(metadataStatus)
        response.data = { ...record, enabled }
      }
    } else if (config.url === '/admin/providers') response.data = { items: providers() }
    else if (config.url === '/admin/models') response.data = { items: [] }
    else if (config.url === '/admin/egress-options') response.data = { items: [] }
    else if (config.url?.endsWith('/egress') && config.method === 'patch')
      response.data = { connection_id: 'con_one', ...JSON.parse(config.data) }
    else if (config.url?.endsWith('/metadata')) {
      if (config.method === 'put') {
        if (hold) await hold
        if (failure) reject(failure)
        const input = JSON.parse(config.data)
        record = { ...record, name: input.name, etag: nextToken }
        response.data = {
          connection: structuredClone(record),
          runtime_applied: true,
          changed: true,
        }
      } else {
        if (metadataStatus !== 200) reject(metadataStatus)
        response.data = structuredClone(record)
      }
      response.headers.set('Cache-Control', 'private, no-store')
      response.headers.set('ETag', `"${record.etag}"`)
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
  vi.restoreAllMocks()
})
async function until(assert: () => void) {
  for (let i = 0; i < 120; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 119) throw error
    }
  }
}
function button(label: string): HTMLButtonElement {
  return [...document.querySelectorAll<HTMLElement>('button,[role="menuitem"]')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )! as HTMLButtonElement
}
function input(label: string) {
  const direct = document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)
  return (
    direct ??
    [...document.querySelectorAll('label')]
      .find((item) => item.textContent === label)!
      .querySelector<HTMLInputElement>('input')!
  )
}
async function change(label: string, value: string) {
  const control = input(label)
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(control, value)
    control.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function mount(fullPage = false) {
  router = createMemoryRouter(
    [
      {
        element: (
          <UncertainIntentProvider>
            <AuthGate mode="private" />
          </UncertainIntentProvider>
        ),
        children: [
          {
            path: '/admin/providers/:providerId',
            element: fullPage ? <ProvidersPage /> : <Host />,
          },
          { path: '/other', element: <p>Other route</p> },
        ],
      },
      { path: '/login', element: <p>Login required</p> },
    ],
    { initialEntries: ['/admin/providers/prv_one?tab=connections'] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(document.body.textContent).toContain('Alpha primary'))
}
async function open() {
  await click('Actions for Alpha primary')
  await until(() => expect(button('Edit name')).toBeTruthy())
  await click('Edit name')
  await until(() => expect(input('Connection name').value).toBe('Alpha primary'))
}
async function draft() {
  await change('Connection name', '  Renamed  ')
  await change('Change reason', '  Operational label  ')
}
const writes = () => requests.filter((r) => r.method === 'put')
async function submit() {
  await draft()
  await click('Review name change')
  await click('Confirm name change')
  await until(() => expect(writes()).toHaveLength(1))
}
async function invalidateMetadata() {
  await act(async () => {
    await cache.invalidateQueries({ predicate: (q) => q.queryKey[1] === 'connection-metadata' })
  })
}

describe('Connection table and reviewed name workflow', () => {
  it('uses the actual Provider page table with literal conjunctive filters and no status or per-row metadata fetches', async () => {
    await mount(true)
    await change('Search connection names', '[')
    expect(document.body.textContent).toContain('Alpha [literal]')
    expect(document.body.textContent).not.toContain('Alpha primary')
    await change('Search connection names', 'ALPHA')
    await click('Filter connection protocol')
    await until(() => expect(button('OpenAI Responses')).toBeTruthy())
    await click('OpenAI Responses')
    expect(document.body.textContent).toContain('Alpha [literal]')
    expect(document.body.textContent).not.toContain('Alpha primary')
    expect(requests.filter((r) => r.url?.endsWith('/metadata'))).toHaveLength(0)
    expect(document.body.textContent).not.toContain('Filter connection status')
    await act(async () => router.navigate('/admin/providers/prv_two?tab=connections'))
    await until(() => expect(document.body.textContent).toContain('Other'))
    expect(input('Search connection names').value).toBe('')
    expect(button('Filter connection protocol').textContent).toBe('All protocols')
  })
  it('keeps real rows mounted without per-row Session refetch and preserves authorized egress editing', async () => {
    await mount()
    const sessionReads = requests.filter((r) => r.url === '/auth/session').length
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 80))
    })
    expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(sessionReads)
    expect(document.querySelectorAll('tbody tr')).toHaveLength(3)
    await click('Direct connection')
    await until(() =>
      expect(document.querySelector('select[name="egress_selection"]')).toBeTruthy(),
    )
    expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(sessionReads)
    await click('Save configuration')
    await until(() => expect(requests.filter((r) => r.method === 'patch')).toHaveLength(1))
    expect(JSON.parse(requests.find((r) => r.method === 'patch')!.data)).toEqual({
      etag: 'raw-revision',
      mode: 'direct',
      egress_id: null,
    })
    expect(requests.find((r) => r.method === 'patch')!.headers.get('X-CSRF-Token')).toBe(
      'csrf-original',
    )
    permissions = ['providers.read']
    await act(async () => {
      await cache.invalidateQueries({ predicate: (q) => q.queryKey[0] === 'permissions' })
    })
    await until(() => expect(button('Direct connection').disabled).toBe(true))
    await click('Direct connection')
    expect(document.querySelector('select[name="egress_selection"]')).toBeNull()
    expect(requests.filter((r) => r.method === 'patch')).toHaveLength(1)
  })
  it('preserves protocol/URL/egress context as read-only and requires reason and explicit review before exact PUT', async () => {
    await mount()
    await open()
    expect(input('Protocol type').readOnly).toBe(true)
    expect(input('Base URL').readOnly).toBe(true)
    expect(input('Network egress').readOnly).toBe(true)
    expect(button('Review name change').disabled).toBe(true)
    expect(document.querySelector('input[type="password"]')).toBeNull()
    await draft()
    await click('Review name change')
    expect(writes()).toHaveLength(0)
    await click('Confirm name change')
    await until(() => expect(writes()).toHaveLength(1))
    const request = writes()[0]!
    expect(JSON.parse(request.data)).toEqual({ name: 'Renamed', reason: 'Operational label' })
    expect(request.headers.get('If-Match')).toBe(`"${token}"`)
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf-original')
  })
  it('preserves recorded FEFF name and reason bytes through a first409 and real AuthGate remount retry', async () => {
    record.name = '\uFEFFAlpha primary\uFEFF'
    const originalName = record.name
    const originalReason = '\uFEFFOperational label\uFEFF'
    await mount()
    await click(`Actions for ${originalName}`)
    await click('Edit name')
    await until(() => expect(input('Connection name').value).toBe(originalName))
    await change('Change reason', originalReason)
    failure = 409
    await click('Review name change')
    expect(document.body.textContent).toContain(originalName)
    await click('Confirm name change')
    await until(() => expect(writes()).toHaveLength(1))
    expect(writes()[0]!.data).toBe(JSON.stringify({ name: originalName, reason: originalReason }))
    await until(() => expect(button('Retry exact name request').disabled).toBe(false))
    sessionStatus = 500
    await act(async () => {
      await cache.invalidateQueries({ queryKey: sessionKey })
    })
    await until(() => expect(unmounts).toBe(1))
    expect(document.body.textContent).not.toContain(originalName)
    sessionStatus = 200
    csrf = 'csrf-renewed'
    await act(async () => {
      await cache.invalidateQueries({ queryKey: sessionKey })
    })
    await until(() => expect(button('Resume name request')).toBeTruthy())
    expect(writes()).toHaveLength(1)
    await click('Resume name request')
    await until(() => expect(input('Connection name').value).toBe(originalName))
    expect(input('Change reason').value).toBe(originalReason)
    failure = 0
    await click('Retry exact name request')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1]!.data).toBe(writes()[0]!.data)
    expect(writes()[1]!.headers.get('If-Match')).toBe(`"${token}"`)
    expect(writes()[1]!.headers.get('X-CSRF-Token')).toBe('csrf-renewed')
  })
  it('does not present a recorded LF label as a valid reviewed name or dispatch it', async () => {
    record.name = 'Alpha primary\n'
    await mount()
    await click(`Actions for ${record.name}`)
    await click('Edit name')
    await until(() =>
      expect(document.body.textContent).toContain(
        'The action failed. Check the service connection and retry.',
      ),
    )
    expect(document.querySelector('label input')).toBeNull()
    expect(button('Review name change')).toBeUndefined()
    expect(writes()).toHaveLength(0)
  })
  it('normalizes only Go White_Space and keeps accepted format characters in the confirmed payload', async () => {
    await mount()
    await open()
    await change('Connection name', '\u00a0\uFEFFRenamed\uFEFF\u0085')
    await change('Change reason', '\u0085\uFEFFOperational label\uFEFF\u00a0')
    await click('Review name change')
    expect(document.body.textContent).toContain('\uFEFFRenamed\uFEFF')
    expect(document.body.textContent).toContain('\uFEFFOperational label\uFEFF')
    await click('Confirm name change')
    await until(() => expect(writes()).toHaveLength(1))
    expect(writes()[0]!.data).toBe(
      JSON.stringify({ name: '\uFEFFRenamed\uFEFF', reason: '\uFEFFOperational label\uFEFF' }),
    )
  })
  it('keeps admitted code-point and UTF8 reason bounds without stripping FEFF to hide overflow', async () => {
    await mount()
    await open()
    await change('Connection name', `\uFEFF${'😀'.repeat(99)}\uFEFF`)
    await change('Change reason', 'Required reason')
    expect(button('Review name change').disabled).toBe(true)
    await change('Connection name', `\uFEFF${'😀'.repeat(98)}\uFEFF`)
    await change('Change reason', `\uFEFF${'r'.repeat(1019)}\uFEFF`)
    expect(button('Review name change').disabled).toBe(true)
    await change('Change reason', `\uFEFF${'r'.repeat(1018)}\uFEFF`)
    expect(button('Review name change').disabled).toBe(false)
    expect(writes()).toHaveLength(0)
  })
  it('does not enable edits for a read-only actor or fetch a private Connection when read permission is absent', async () => {
    permissions = ['providers.read']
    await mount()
    await click('Actions for Alpha primary')
    await until(() => expect(button('Edit name').getAttribute('aria-disabled')).toBe('true'))
    expect(requests.filter((r) => r.url?.endsWith('/metadata'))).toHaveLength(0)
    permissions = ['providers.write']
    await act(async () => {
      await cache.invalidateQueries({ predicate: (q) => q.queryKey[0] === 'permissions' })
    })
    await until(() =>
      expect(document.body.textContent).toContain('does not have permission to read'),
    )
    expect(document.body.textContent).not.toContain('Alpha primary')
    expect(requests.filter((r) => r.url?.endsWith('/metadata'))).toHaveLength(0)
  })
  it('disables a stale confirmation and explicitly reviews the new shared revision without clearing draft', async () => {
    await mount()
    await open()
    await draft()
    await click('Review name change')
    record = { ...record, etag: nextToken }
    await invalidateMetadata()
    expect(button('Confirm name change').disabled).toBe(true)
    await click('Confirm name change')
    expect(writes()).toHaveLength(0)
    await click('Cancel')
    expect(input('Connection name').value).toBe('  Renamed  ')
    await click('Review current configuration')
    await click('Review name change')
    await click('Confirm name change')
    await until(() => expect(writes()).toHaveLength(1))
    expect(writes()[0]!.headers.get('If-Match')).toBe(`"${nextToken}"`)
  })
  it('retains first409,503,409 then success with identical ETag/body and no artificial cache renewal', async () => {
    await mount()
    await open()
    failure = 409
    await submit()
    await until(() => expect(button('Retry exact name request').disabled).toBe(false))
    failure = 503
    await click('Retry exact name request')
    await until(() => expect(writes()).toHaveLength(2))
    await until(() => expect(button('Retry exact name request').disabled).toBe(false))
    failure = 409
    await click('Retry exact name request')
    await until(() => expect(writes()).toHaveLength(3))
    await until(() => expect(button('Retry exact name request').disabled).toBe(false))
    failure = 0
    await click('Retry exact name request')
    await until(() => expect(writes()).toHaveLength(4))
    for (const request of writes()) {
      expect(request.data).toBe(writes()[0]!.data)
      expect(request.headers.get('If-Match')).toBe(`"${token}"`)
    }
    await until(() => expect(document.body.textContent).toContain('Renamed'))
  })
  it.each([400, 403, 404, 412, 428, 500, 503])(
    'keeps the dispatched immutable intent on first %s without calling GET success',
    async (status) => {
      await mount()
      await open()
      failure = status
      await submit()
      await until(() => expect(button('Retry exact name request').disabled).toBe(false))
      await invalidateMetadata()
      expect(button('Retry exact name request').disabled).toBe(false)
      expect(input('Connection name').disabled).toBe(true)
      expect(writes()).toHaveLength(1)
      failure = 0
      await click('Retry exact name request')
      await until(() => expect(writes()).toHaveLength(2))
      expect(writes()[1]!.data).toBe(writes()[0]!.data)
      expect(writes()[1]!.headers.get('If-Match')).toBe(`"${token}"`)
    },
  )
  it('locks duplicate submissions while a dispatch is pending, then clears only that operation busy state', async () => {
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await mount()
    await open()
    failure = 503
    await submit()
    expect(button('Retry exact name request').disabled).toBe(true)
    await click('Retry exact name request')
    expect(writes()).toHaveLength(1)
    await act(async () => release())
    await until(() => expect(button('Retry exact name request').disabled).toBe(false))
  })
  it('retains request through Cancel/Escape and incidental GET; only deliberate Abandon permits a new reviewed intent', async () => {
    await mount()
    await open()
    failure = 503
    await submit()
    await until(() => expect(button('Retry exact name request').disabled).toBe(false))
    await click('Cancel')
    await until(() => expect(button('Resume name request')).toBeTruthy())
    await click('Resume name request')
    await until(() => expect(input('Connection name').value).toBe('Renamed'))
    expect(input('Connection name').disabled).toBe(true)
    expect(writes()).toHaveLength(1)
    await act(async () =>
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    await until(() => expect(button('Resume name request')).toBeTruthy())
    await click('Resume name request')
    await until(() => expect(button('Retry exact name request')).toBeTruthy())
    record = { ...record, name: 'Renamed', etag: nextToken }
    await invalidateMetadata()
    expect(button('Retry exact name request')).toBeTruthy()
    expect(writes()).toHaveLength(1)
    await click('Abandon original request')
    expect(writes()).toHaveLength(1)
    await click('Abandon request')
    expect(input('Connection name').value).toBe('Renamed')
    expect(button('Review name change').disabled).toBe(true)
    await click('Review current configuration')
    expect(button('Review name change').disabled).toBe(false)
    expect(document.body.textContent).toContain('previous outcome remains unknown')
  })
  it('hides private values on permission errors and recovers manual retry under fresh authority', async () => {
    await mount()
    await open()
    failure = 503
    await submit()
    await until(() => expect(button('Retry exact name request').disabled).toBe(false))
    permissionStatus = 500
    await act(async () => {
      await cache.invalidateQueries({ predicate: (q) => q.queryKey[0] === 'permissions' })
    })
    expect(document.body.textContent).not.toContain('Alpha primary')
    expect(document.querySelector('input[value="Renamed"]')).toBeNull()
    expect(writes()).toHaveLength(1)
    permissionStatus = 200
    await act(async () => {
      await cache.invalidateQueries({ predicate: (q) => q.queryKey[0] === 'permissions' })
    })
    await until(() => expect(button('Retry exact name request').disabled).toBe(false))
    failure = 0
    await click('Retry exact name request')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1]!.data).toBe(writes()[0]!.data)
    expect(writes()[1]!.headers.get('If-Match')).toBe(`"${token}"`)
  })
  it('survives actual AuthGate500 teardown with exact intent and current CSRF; no automatic replay', async () => {
    await mount()
    await open()
    failure = 503
    await submit()
    await until(() => expect(button('Retry exact name request').disabled).toBe(false))
    sessionStatus = 500
    await act(async () => {
      await cache.invalidateQueries({ queryKey: sessionKey })
    })
    await until(() => expect(unmounts).toBe(1))
    expect(document.body.textContent).not.toContain('Alpha primary')
    expect(writes()).toHaveLength(1)
    sessionStatus = 200
    csrf = 'csrf-renewed'
    await act(async () => {
      await cache.invalidateQueries({ queryKey: sessionKey })
    })
    await until(() => expect(button('Resume name request')).toBeTruthy())
    expect(mounts).toBe(2)
    expect(writes()).toHaveLength(1)
    await click('Resume name request')
    await until(() => expect(button('Retry exact name request').disabled).toBe(false))
    failure = 0
    await click('Retry exact name request')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1]!.data).toBe(writes()[0]!.data)
    expect(writes()[1]!.headers.get('If-Match')).toBe(`"${token}"`)
    expect(writes()[1]!.headers.get('X-CSRF-Token')).toBe('csrf-renewed')
  })
  it('keeps a renewed uncertain claim while a stale pre-teardown response arrives', async () => {
    let releaseOld!: () => void, releaseNew!: () => void
    hold = new Promise((resolve) => {
      releaseOld = resolve
    })
    await mount()
    await open()
    await submit()
    sessionStatus = 500
    await act(async () => {
      await cache.invalidateQueries({ queryKey: sessionKey })
    })
    await until(() => expect(unmounts).toBe(1))
    sessionStatus = 200
    csrf = 'csrf-renewed'
    await act(async () => {
      await cache.invalidateQueries({ queryKey: sessionKey })
    })
    await until(() => expect(button('Resume name request')).toBeTruthy())
    await click('Resume name request')
    await until(() => expect(button('Retry exact name request').disabled).toBe(false))
    hold = new Promise((resolve) => {
      releaseNew = resolve
    })
    await click('Retry exact name request')
    await until(() => expect(writes()).toHaveLength(2))
    failure = 503
    await act(async () => releaseOld())
    expect(button('Retry exact name request').disabled).toBe(true)
    expect(document.body.textContent).toContain('outcome of the submitted request is unknown')
    await act(async () => releaseNew())
    await until(() => expect(button('Retry exact name request').disabled).toBe(false))
    expect(writes()).toHaveLength(2)
    for (const request of writes()) {
      expect(request.data).toBe(writes()[0]!.data)
      expect(request.headers.get('If-Match')).toBe(`"${token}"`)
    }
  })
  it.each(['actor', 'provider', 'tab', 'logout', 'expiry'])(
    'never transfers retained request after %s boundary',
    async (boundary) => {
      await mount()
      await open()
      failure = 503
      await submit()
      await until(() => expect(button('Retry exact name request').disabled).toBe(false))
      if (boundary === 'actor') {
        actor = 'usr_other'
        await act(async () => {
          await cache.invalidateQueries({ queryKey: sessionKey })
        })
      }
      if (boundary === 'provider')
        await act(async () => router.navigate('/admin/providers/prv_two?tab=connections'))
      if (boundary === 'tab') {
        await act(async () => router.navigate('/admin/providers/prv_one?tab=settings'))
        await act(async () => router.navigate('/admin/providers/prv_one?tab=connections'))
      }
      if (boundary === 'logout')
        await act(async () => {
          cache.setQueryData(sessionKey, null)
        })
      if (boundary === 'expiry')
        await act(async () => {
          window.dispatchEvent(new Event('routex:session-expired'))
        })
      await until(() => expect(button('Retry exact name request')).toBeUndefined())
      expect(button('Resume name request')).toBeUndefined()
      expect(writes()).toHaveLength(1)
    },
  )
  it('changes language while keeping unsent and submitted values unchanged', async () => {
    await mount()
    await open()
    await draft()
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('编辑接入名称')
    expect(
      [...document.querySelectorAll<HTMLInputElement>('input')].some(
        (item) => item.value === '  Renamed  ',
      ),
    ).toBe(true)
    await act(async () => i18n.changeLanguage('en'))
    failure = 503
    await click('Review name change')
    await click('Confirm name change')
    await until(() => expect(writes()).toHaveLength(1))
    await act(async () => i18n.changeLanguage('zh'))
    expect(writes()[0]!.data).toBe(JSON.stringify({ name: 'Renamed', reason: 'Operational label' }))
    expect(document.body.textContent).toContain('原请求')
  })
})

describe('Connection routing status', () => {
  it('filters real boolean state conjunctively and resets when Provider changes', async () => {
    await mount()
    await click('Connection status')
    await click('Disabled')
    expect(host.textContent).toContain('Alpha [literal]')
    expect(host.textContent).not.toContain('Alpha primary')
    await change('Search connection names', 'Gamma')
    expect(host.textContent).toContain('No matching connections.')
    await act(async () => {
      await router.navigate('/admin/providers/prv_two?tab=connections')
    })
    expect(host.textContent).toContain('Other')
    expect(host.textContent).toContain('All statuses')
    expect(requests.filter((x) => x.url?.endsWith('/status'))).toHaveLength(0)
  })
  it('requires explicit confirmation, preserves exact uncertain status through rejection and renewed read, then retries with current CSRF', async () => {
    await mount()
    await click('Actions for Alpha primary')
    await click('Disable Connection')
    await vi.waitFor(() => expect(document.body.textContent).toContain('Connection routing status'))
    await change('Change reason', 'Reviewed stop')
    await click('Review status change')
    expect(requests.filter((x) => x.method === 'put' && x.url?.endsWith('/status'))).toHaveLength(0)
    failure = 503
    await click('Disable Connection')
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain('Retry exact status request'),
    )
    const first = requests.find((x) => x.method === 'put' && x.url?.endsWith('/status'))!
    expect(JSON.parse(first.data)).toEqual({ enabled: false, reason: 'Reviewed stop' })
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.body.textContent).toContain('已提交的状态变更仍未确认')
    await act(async () => {
      await i18n.changeLanguage('en')
    })
    failure = 409
    await click('Retry exact status request')
    expect(document.body.textContent).toContain('Retry exact status request')
    csrf = 'csrf-renewed'
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey })
    })
    await vi.waitFor(() => expect(document.body.textContent).toContain('Connection routing status'))
    failure = 0
    await click('Retry exact status request')
    const writes = requests.filter((x) => x.method === 'put' && x.url?.endsWith('/status'))
    expect(writes).toHaveLength(3)
    expect(writes.map((x) => x.data)).toEqual([first.data, first.data, first.data])
    expect(writes.map((x) => x.headers.get('If-Match'))).toEqual([
      `"${token}"`,
      `"${token}"`,
      `"${token}"`,
    ])
    expect(writes[2].headers.get('X-CSRF-Token')).toBe('csrf-renewed')
    await vi.waitFor(() => expect(host.textContent).toContain('Disabled'))
  })
  it('keeps status dispatch unavailable under independent read-only authority', async () => {
    permissions = ['providers.read']
    await mount()
    await click('Actions for Alpha primary')
    expect(
      button('Disable Connection').disabled ||
        button('Disable Connection').getAttribute('aria-disabled') === 'true',
    ).toBe(true)
    expect(requests.filter((x) => x.url?.endsWith('/status'))).toHaveLength(0)
  })
  it('waits for a held fresh status read and hides stale review facts before dispatch', async () => {
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await mount()
    await click('Actions for Alpha primary')
    await click('Disable Connection')
    await until(() => expect(requests.some((x) => x.url?.endsWith('/status'))).toBe(true))
    expect(document.body.textContent).not.toContain(
      'Stop Alpha primary from receiving new requests.',
    )
    expect(button('Review status change')).toBeUndefined()
    expect(requests.filter((x) => x.method === 'put' && x.url?.endsWith('/status'))).toHaveLength(0)
    await act(async () => release())
    await until(() => expect(button('Review status change')).toBeTruthy())
    expect(document.body.textContent).toContain('Stop Alpha primary from receiving new requests.')
  })
  it('retains exact disabled intent across real AuthGate error teardown without automatic replay', async () => {
    await mount()
    await click('Actions for Alpha primary')
    await click('Disable Connection')
    await until(() => expect(button('Review status change')).toBeTruthy())
    await change('Change reason', 'Retain exact status')
    await click('Review status change')
    failure = 503
    await click('Disable Connection')
    await until(() => expect(button('Retry exact status request').disabled).toBe(false))
    const statusWrites = () =>
      requests.filter((x) => x.method === 'put' && x.url?.endsWith('/status'))
    const original = statusWrites()[0]!
    sessionStatus = 500
    await act(async () => {
      await cache.invalidateQueries({ queryKey: sessionKey })
    })
    await until(() => expect(unmounts).toBe(1))
    expect(document.body.textContent).not.toContain('Alpha primary')
    expect(statusWrites()).toHaveLength(1)
    sessionStatus = 200
    csrf = 'csrf-renewed-status'
    await act(async () => {
      await cache.invalidateQueries({ queryKey: sessionKey })
    })
    await until(() => expect(button('Review unresolved status change')).toBeTruthy())
    expect(mounts).toBe(2)
    expect(statusWrites()).toHaveLength(1)
    await click('Review unresolved status change')
    await until(() => expect(button('Retry exact status request').disabled).toBe(false))
    failure = 0
    await click('Retry exact status request')
    await until(() => expect(statusWrites()).toHaveLength(2))
    expect(statusWrites()[1]!.data).toBe(original.data)
    expect(JSON.parse(original.data)).toEqual({ enabled: false, reason: 'Retain exact status' })
    expect(statusWrites()[1]!.headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(statusWrites()[1]!.headers.get('X-CSRF-Token')).toBe('csrf-renewed-status')
  })
})
