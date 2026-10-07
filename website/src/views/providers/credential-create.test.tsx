import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import CredentialCreateDialog from './credential-create'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const uuid = '12345678-1234-4234-9234-123456789abc'
let root: Root, host: HTMLDivElement, cache: QueryClient
let session: { user: { id: string; role: string }; csrf_token: string },
  permissions: string[],
  context: { storage_source: 'inline' | 'vault'; etag: string }
let requests: InternalAxiosRequestConfig[],
  status: number,
  holdWrite: Promise<void> | undefined,
  holdContext: Promise<void> | undefined,
  target: boolean
let saved: ReturnType<typeof vi.fn<() => void>>, closed: ReturnType<typeof vi.fn<() => void>>
const original = client.defaults.adapter
beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.spyOn(crypto, 'randomUUID').mockReturnValue(uuid)
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  session = { user: { id: 'usr_creator', role: 'admin' }, csrf_token: 'a'.repeat(64) }
  permissions = ['providers.read', 'providers.write']
  context = { storage_source: 'vault', etag: 'b'.repeat(64) }
  requests = []
  status = 201
  holdWrite = undefined
  holdContext = undefined
  target = true
  saved = vi.fn()
  closed = vi.fn()
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown,
      responseStatus = 200,
      headers = {}
    if (config.url === '/auth/session') data = structuredClone(session)
    else if (config.url === '/auth/permissions') data = { permissions: [...permissions] }
    else if (config.url === '/admin/provider-credential-storage-context') {
      const body = structuredClone(context)
      if (holdContext) await holdContext
      data = body
      headers = { etag: `"${body.etag}"` }
    } else if (config.url === '/admin/egress-options') data = { items: [] }
    else if (config.method === 'post') {
      const captured = status
      if (holdWrite) await holdWrite
      if (captured >= 400)
        throw new AxiosError('Safe failure', '', config, undefined, {
          data: {},
          status: captured,
          statusText: '',
          config,
          headers: {},
        })
      responseStatus = captured
      const credential = { id: 'crd_saved', storage_source: 'vault' }
      data =
        config.url === '/admin/providers'
          ? { id: 'prv_saved', connections: [{ credentials: [credential] }] }
          : config.url?.endsWith('/connections')
            ? { id: 'con_saved', credentials: [credential] }
            : credential
    } else throw new Error('Unexpected route')
    return { status: responseStatus, statusText: '', headers, data, config }
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
})
async function until(check: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((done) => setTimeout(done, 5))
    })
    try {
      check()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
async function mount(
  kind: 'provider' | 'connection' | 'credential' = 'credential',
  id = 'con_target',
) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <CredentialCreateDialog
          kind={kind}
          id={id}
          targetCurrent={() => target}
          onSaved={saved}
          onClose={closed}
        />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(document.body.textContent).toContain('Configured source for this new credential'),
  )
}
async function fill(name: string, value: string) {
  await act(async () => {
    const control = document.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(control, value)
    control.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function draft(kind = 'credential') {
  await fill('name', 'Fresh')
  await fill('secret', ' transient secret ')
  if (kind !== 'credential') {
    await fill('base_url', 'https://api.example.com/v1')
    await fill('credential_name', 'Credential')
    if (kind === 'provider') await fill('connection_name', 'Connection')
  }
}
const button = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === text,
  )!
async function submit() {
  await act(async () =>
    document
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
}
const writes = () => requests.filter((item) => item.method === 'post')
it.each(['provider', 'connection', 'credential'] as const)(
  'sends %s creation directly with stable UUID and exact context; no secret mutation cache/storage',
  async (kind) => {
    await mount(kind)
    await draft(kind)
    await submit()
    await until(() => expect(saved).toHaveBeenCalledOnce())
    expect(writes()).toHaveLength(1)
    expect(JSON.parse(writes()[0].data)).toMatchObject({
      request_id: uuid,
      storage_policy_etag: 'b'.repeat(64),
      secret: ' transient secret ',
    })
    expect(writes()[0].url).toBe(
      kind === 'provider'
        ? '/admin/providers'
        : kind === 'connection'
          ? '/admin/providers/con_target/connections'
          : '/admin/connections/con_target/credentials',
    )
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((query) => query.state.data),
      ),
    ).not.toContain('transient secret')
    expect(document.querySelector<HTMLInputElement>('input[name="secret"]')!.value).toBe('')
    expect(
      [...Array(localStorage.length)]
        .map((_, i) => localStorage.getItem(localStorage.key(i)!))
        .join(''),
    ).not.toContain('transient secret')
    expect(requests.some((item) => item.url?.startsWith('/admin/secrets'))).toBe(false)
  },
)
it('holds creation until exact fresh source context arrives without secret authority expansion', async () => {
  let release!: () => void
  holdContext = new Promise((resolve) => {
    release = resolve
  })
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <CredentialCreateDialog
          kind="credential"
          id="con_target"
          targetCurrent={() => true}
          onSaved={saved}
          onClose={closed}
        />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(requests.some((item) => item.url === '/admin/provider-credential-storage-context')).toBe(
      true,
    ),
  )
  expect(button('Save').disabled).toBe(true)
  await act(async () => release())
  await until(() => expect(button('Save').disabled).toBe(false))
})
it('503 followed by 409 retains exact original UUID/body/policy and fresh CSRF despite policy change', async () => {
  await mount()
  await draft()
  status = 503
  await submit()
  await until(() => expect(button('Retry original creation request')).toBeTruthy())
  const body = writes()[0].data
  context = { storage_source: 'inline', etag: 'c'.repeat(64) }
  await act(async () => {
    cache.setQueryData(sessionKey, { ...session, csrf_token: 'd'.repeat(64) })
    await cache.refetchQueries({ queryKey: ['admin', 'credential-storage-context'] })
  })
  status = 409
  await act(async () => button('Retry original creation request').click())
  await until(() => expect(writes()).toHaveLength(2))
  expect(writes()[1].data).toBe(body)
  expect(writes()[1].headers.get('X-CSRF-Token')).toBe('d'.repeat(64))
  expect(saved).not.toHaveBeenCalled()
  expect(button('Retry original creation request')).toBeTruthy()
  expect(button('Review current storage policy')).toBeUndefined()
})
it('409 needs explicit current policy review and preserves the human draft', async () => {
  await mount()
  await draft()
  status = 409
  await submit()
  await until(() => expect(button('Review current storage policy')).toBeTruthy())
  context.etag = 'c'.repeat(64)
  await act(async () => button('Review current storage policy').click())
  await until(() => expect(button('Save').disabled).toBe(false))
  expect(document.querySelector<HTMLInputElement>('input[name="secret"]')!.value).toBe(
    ' transient secret ',
  )
  status = 201
  await submit()
  await until(() => expect(saved).toHaveBeenCalledOnce())
  expect(JSON.parse(writes()[1].data).storage_policy_etag).toBe('c'.repeat(64))
})
it('late success cannot report saved after target or permission renewal; exact retry remains gated', async () => {
  await mount()
  await draft()
  let release!: () => void
  holdWrite = new Promise((resolve) => {
    release = resolve
  })
  await submit()
  await act(async () => cache.setQueryData(['permissions', session.user.id], ['providers.read']))
  target = false
  await act(async () => release())
  await until(() => expect(button('Retry original creation request')).toBeTruthy())
  expect(saved).not.toHaveBeenCalled()
  expect(button('Retry original creation request').disabled).toBe(true)
})
it('actor replacement and dismissal destroy the transient draft; live language switches preserve it first', async () => {
  await mount()
  await draft()
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.body.textContent).toContain('此新凭据的已配置来源')
  expect(document.querySelector<HTMLInputElement>('input[name="secret"]')!.value).toBe(
    ' transient secret ',
  )
  session = { ...session, user: { ...session.user, id: 'usr_other' } }
  await act(async () => cache.setQueryData(sessionKey, session))
  await until(() =>
    expect(document.querySelector<HTMLInputElement>('input[name="secret"]')!.value).toBe(''),
  )
  await act(async () => button('取消').click())
  expect(closed).toHaveBeenCalledOnce()
  expect(writes()).toHaveLength(0)
})
it('rejects a non201 ordinary creation acknowledgement without pretending historical success', async () => {
  await mount()
  await draft()
  status = 200
  await submit()
  await until(() => expect(button('Retry original creation request')).toBeTruthy())
  expect(saved).not.toHaveBeenCalled()
})
it('providers.write alone reads the creator context without any Secrets admin authority', async () => {
  permissions = ['providers.write']
  await mount()
  await draft()
  await submit()
  await until(() => expect(saved).toHaveBeenCalledOnce())
  expect(requests.some((item) => item.url?.startsWith('/admin/secrets'))).toBe(false)
})
it('fails locally for unavailable or non-v4 UUID generation without weakening intent identity', async () => {
  await mount()
  await draft()
  vi.mocked(crypto.randomUUID).mockReturnValue('12345678-1234-1234-9234-123456789abc')
  await submit()
  expect(writes()).toHaveLength(0)
  vi.mocked(crypto.randomUUID).mockImplementation(() => {
    throw new Error('Unavailable')
  })
  await submit()
  expect(writes()).toHaveLength(0)
})
it('source renewal hides the secret and retains the draft until a fresh current read completes', async () => {
  await mount()
  await draft()
  let release!: () => void
  holdContext = new Promise((resolve) => {
    release = resolve
  })
  let refresh!: Promise<void>
  await act(async () => {
    refresh = cache.refetchQueries({ queryKey: ['admin', 'credential-storage-context'] })
  })
  expect(
    document.querySelector<HTMLInputElement>('input[name="secret"]')!.closest('[hidden]'),
  ).toBeTruthy()
  expect(button('Save').disabled).toBe(true)
  expect(writes()).toHaveLength(0)
  await act(async () => {
    release()
    await refresh
  })
  expect(document.querySelector<HTMLInputElement>('input[name="secret"]')!.value).toBe(
    ' transient secret ',
  )
  expect(button('Save').disabled).toBe(false)
})
it('201 UUID reconciliation retains original Vault source despite a changed configured policy', async () => {
  await mount()
  await draft()
  status = 503
  await submit()
  await until(() => expect(button('Retry original creation request')).toBeTruthy())
  const originalBody = writes()[0].data
  context = { storage_source: 'inline', etag: 'c'.repeat(64) }
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['admin', 'credential-storage-context'] })
  })
  status = 201
  await act(async () => button('Retry original creation request').click())
  await until(() => expect(saved).toHaveBeenCalledOnce())
  expect(writes()[1].data).toBe(originalBody)
})
