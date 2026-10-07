import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import Coverage from './deployment-coverage'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const crd = 'crd_01arz3ndektsv4rrffq69g5fav',
  con = 'con_01arz3ndektsv4rrffq69g5fav',
  pmd = 'pmd_01arz3ndektsv4rrffq69g5fav',
  prv = 'prv_01arz3ndektsv4rrffq69g5fav',
  pmd2 = 'pmd_01arz3ndektsv4rrffq69g5fax'
const tag = 'a'.repeat(64) + '.' + 'b'.repeat(64)
const row = () => ({
  credential_id: crd,
  connection_id: con,
  adapter: 'azure_openai_classic',
  api_version: '2024-10-21',
  verification_status: 'verified',
  verified_at: null,
  coverage_source: 'administrator_attestation',
  provider_models: [
    { id: pmd, upstream_name: 'deployment-A', can_attest: true, attested: false },
    { id: pmd2, upstream_name: 'deployment-B', can_attest: true, attested: true },
  ],
  etag: tag,
  can_edit: true,
})
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  requests: InternalAxiosRequestConfig[],
  permissions: string[],
  session: { user: { id: string; role: string }; csrf_token: string },
  data: ReturnType<typeof row>,
  status: number,
  hold: Promise<void> | undefined,
  holdRead: Promise<void> | undefined
let expired: boolean
let saved: ReturnType<typeof vi.fn<() => void>>, closed: ReturnType<typeof vi.fn<() => void>>
const original = client.defaults.adapter
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  requests = []
  permissions = ['providers.read', 'providers.write']
  session = { user: { id: 'usr_owner', role: 'user' }, csrf_token: 'old-csrf' }
  data = row()
  status = 200
  expired = false
  hold = undefined
  holdRead = undefined
  saved = vi.fn()
  closed = vi.fn()
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let body: unknown,
      headers = {}
    const result = 200
    if (config.url === '/auth/session') body = expired ? null : structuredClone(session)
    else if (config.url === '/auth/permissions') body = { permissions: [...permissions] }
    else if (config.url === '/admin/providers')
      body = {
        items: [
          {
            id: prv,
            name: 'Provider',
            connections: [
              {
                id: con,
                name: 'Azure',
                protocol: 'openai_chat',
                adapter: 'azure_openai_classic',
                api_version: '2024-10-21',
                base_url: 'https://azure.example.invalid',
                credentials: [{ id: crd, name: 'Credential' }],
                provider_models: data.provider_models.map((p) => ({
                  id: p.id,
                  upstream_name: p.upstream_name,
                })),
              },
            ],
          },
        ],
      }
    else if (config.url === `/admin/credentials/${crd}/deployment-coverage`) {
      const captured = structuredClone(data)
      if (config.method === 'put') {
        const capturedStatus = status
        if (hold) await hold
        if (capturedStatus >= 400)
          throw new AxiosError('Safe failure', '', config, undefined, {
            data: {},
            status: capturedStatus,
            statusText: '',
            config,
            headers: {},
          })
        const input = JSON.parse(config.data)
        captured.provider_models.forEach(
          (p) => (p.attested = input.provider_model_ids.includes(p.id)),
        )
        body = { coverage: captured, runtime_applied: true, changed: false }
      } else {
        if (holdRead) await holdRead
        body = captured
      }
      headers = { etag: `"${captured.etag}"`, 'cache-control': 'private, no-store' }
    } else throw new Error('Unexpected route')
    return { config, data: body, status: result, statusText: '', headers }
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
    } catch (e) {
      if (i === 99) throw e
    }
  }
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Coverage
          providerId={prv}
          connectionId={con}
          credentialId={crd}
          onClose={closed}
          onSaved={saved}
        />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(document.querySelector('table')).not.toBeNull())
}
const button = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent === text)!
const writes = () => requests.filter((r) => r.method === 'put')
async function click(text: string) {
  await act(async () => button(text).click())
}
async function fill(value = 'Reviewed access') {
  await act(async () => {
    const input = [...document.querySelectorAll<HTMLInputElement>('input')].find(
      (i) => i.type !== 'checkbox',
    )!
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function prepare() {
  await fill('  Reviewed access  ')
  await act(async () => document.querySelector<HTMLInputElement>('input[type=checkbox]')!.click())
  await click('Replace deployment coverage')
  await until(() => expect(button('Confirm full replacement')).toBeDefined())
}
it('reads the complete authorized set, uses explicit confirmation and one complete replacement without Key/secrets/probe calls', async () => {
  await mount()
  expect(document.body.textContent).toContain('deployment-A')
  expect(document.body.textContent).toContain('deployment-B')
  await prepare()
  expect(writes()).toHaveLength(0)
  await click('Confirm full replacement')
  await until(() => expect(saved).toHaveBeenCalledTimes(1))
  expect(JSON.parse(writes()[0].data)).toEqual({
    provider_model_ids: [pmd2, pmd],
    reason: 'Reviewed access',
  })
  expect(writes()[0].headers.get('If-Match')).toBe(`"${tag}"`)
  expect(
    requests.every(
      (r) =>
        r.url === '/auth/session' ||
        r.url === '/auth/permissions' ||
        r.url === '/admin/providers' ||
        r.url === `/admin/credentials/${crd}/deployment-coverage`,
    ),
  ).toBe(true)
})
it('allows independent readonly review, including server no-edit', async () => {
  permissions = ['providers.read']
  data.can_edit = false
  await mount()
  expect(document.querySelector<HTMLInputElement>('input[type=checkbox]')!.disabled).toBe(true)
  expect(button('Replace deployment coverage')).toBeUndefined()
  expect(writes()).toHaveLength(0)
})
it('write alone does not fetch or expose private coverage', async () => {
  permissions = ['providers.write']
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Coverage
          providerId={prv}
          connectionId={con}
          credentialId={crd}
          onClose={closed}
          onSaved={saved}
        />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(cache.getQueryState(['permissions', 'usr_owner'])?.status).toBe('success'),
  )
  expect(
    requests.some((r) => r.url?.includes('deployment-coverage') || r.url === '/admin/providers'),
  ).toBe(false)
  expect(document.querySelector('table')).toBeNull()
})
it('requires valid reason and explicitly confirms empty-set revocation', async () => {
  await mount()
  await fill('é'.repeat(513))
  expect(button('Replace deployment coverage').disabled).toBe(true)
  await fill()
  await click('Clear all selections')
  await click('Replace deployment coverage')
  expect(document.body.textContent).toContain('0 selected deployments')
  await click('Confirm full replacement')
  await until(() => expect(saved).toHaveBeenCalledTimes(1))
  expect(JSON.parse(writes()[0].data).provider_model_ids).toEqual([])
})
it('preserves draft after definite first conflict until explicit current review', async () => {
  await mount()
  status = 409
  await prepare()
  await click('Confirm full replacement')
  await until(() => expect(document.body.textContent).toContain('Coverage changed.'))
  expect(button('Replace deployment coverage').disabled).toBe(true)
  data.etag = 'a'.repeat(64) + '.' + 'c'.repeat(64)
  await click('Refresh coverage')
  await until(() =>
    expect(
      cache
        .getQueriesData({ queryKey: ['admin', 'deployment-coverage'] })
        .some(([, d]) => (d as ReturnType<typeof row>)?.etag === data.etag),
    ).toBe(true),
  )
  await click('Review latest coverage')
  await until(() => expect(button('Replace deployment coverage').disabled).toBe(false))
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].find((i) => i.type !== 'checkbox')!
      .value,
  ).toBe('  Reviewed access  ')
  expect(document.querySelector<HTMLInputElement>('input[type=checkbox]')!.checked).toBe(true)
})
it('matching GET and rejected retry never resolve original uncertain full-set intent', async () => {
  await mount()
  status = 503
  await prepare()
  await click('Confirm full replacement')
  await until(() => expect(button('Retry exact coverage request')).toBeDefined())
  const first = JSON.parse(writes()[0].data)
  data.provider_models.forEach((p) => (p.attested = true))
  data.etag = 'a'.repeat(64) + '.' + 'd'.repeat(64)
  await click('Refresh coverage')
  await until(() => expect(button('Retry exact coverage request')?.disabled).toBe(false))
  status = 409
  session.csrf_token = 'fresh-csrf'
  await act(async () => cache.setQueryData(sessionKey, structuredClone(session)))
  await click('Retry exact coverage request')
  await until(() => expect(writes()).toHaveLength(2))
  await until(() => expect(button('Retry exact coverage request').disabled).toBe(false))
  expect(JSON.parse(writes()[1].data)).toEqual(first)
  expect(writes()[1].headers.get('If-Match')).toBe(writes()[0].headers.get('If-Match'))
  expect(writes()[1].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
  expect(saved).not.toHaveBeenCalled()
})
it.each([200, 503])(
  'Session renewal while PUT pending keeps exact uncertain intent after late %s and requires manual fresh-CSRF retry',
  async (late) => {
    await mount()
    let release!: () => void
    hold = new Promise((done) => (release = done))
    status = late
    await prepare()
    await click('Confirm full replacement')
    await until(() => expect(writes()).toHaveLength(1))
    session.csrf_token = 'renewed-csrf'
    await act(async () => {
      await cache.fetchQuery({
        queryKey: sessionKey,
        queryFn: async () => structuredClone(session),
      })
    })
    await until(() => expect(button('Retry exact coverage request')?.disabled).toBe(false))
    await act(async () => release())
    hold = undefined
    status = 200
    expect(saved).not.toHaveBeenCalled()
    expect(writes()).toHaveLength(1)
    await click('Retry exact coverage request')
    await until(() => expect(saved).toHaveBeenCalledTimes(1))
    expect(JSON.parse(writes()[1].data)).toEqual(JSON.parse(writes()[0].data))
    expect(writes()[1].headers.get('If-Match')).toBe(writes()[0].headers.get('If-Match'))
    expect(writes()[1].headers.get('X-CSRF-Token')).toBe('renewed-csrf')
  },
)
it('actor replacement destroys private dialog intent and ignores late success', async () => {
  await mount()
  let release!: () => void
  hold = new Promise((done) => (release = done))
  await prepare()
  await click('Confirm full replacement')
  await until(() => expect(writes()).toHaveLength(1))
  permissions = []
  session.user.id = 'usr_other'
  await act(async () => cache.setQueryData(sessionKey, structuredClone(session)))
  await until(() => expect(document.querySelector('table')).toBeNull())
  await act(async () => release())
  expect(saved).not.toHaveBeenCalled()
  expect(button('Retry exact coverage request')).toBeUndefined()
})
it('unmount aborts pending request without stale callbacks or private coverage recreation', async () => {
  await mount()
  let release!: () => void
  hold = new Promise((done) => (release = done))
  await prepare()
  await click('Confirm full replacement')
  await until(() => expect(writes()).toHaveLength(1))
  await act(async () => root.render(null))
  await act(async () => release())
  expect(saved).not.toHaveBeenCalled()
  expect(writes()[0].signal?.aborted).toBe(true)
})
it('hides cached table during renewal/error and blocks same-event dispatch after permission invalidation', async () => {
  await mount()
  await fill()
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['permissions', 'usr_owner'], refetchType: 'none' })
    document
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
  expect(writes()).toHaveLength(0)
  await until(() => expect(document.querySelector('table')).toBeNull())
})
it('defaults English and live switches paired copy without clearing reason or selected IDs', async () => {
  await mount()
  await fill('Bilingual reason')
  await act(async () => document.querySelector<HTMLInputElement>('input[type=checkbox]')!.click())
  expect(document.body.textContent).toContain('Deployment coverage')
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.body.textContent).not.toContain('Deployment coverage')
  expect(document.body.textContent).toContain(i18n.t('deploymentCoverage.title', { ns: 'catalog' }))
  expect(
    [...document.querySelectorAll<HTMLInputElement>('input')].find((i) => i.type !== 'checkbox')!
      .value,
  ).toBe('Bilingual reason')
  expect(document.querySelector<HTMLInputElement>('input[type=checkbox]')!.checked).toBe(true)
  await act(async () => i18n.changeLanguage('en'))
  expect(document.body.textContent).toContain('Deployment coverage')
})
it('a stale confirmation hides during a genuine resource read and cannot dispatch its old validator', async () => {
  await mount()
  await prepare()
  let release!: () => void
  holdRead = new Promise((done) => (release = done))
  data.etag = 'a'.repeat(64) + '.' + 'c'.repeat(64)
  let renewed!: Promise<void>
  await act(async () => {
    renewed = cache.refetchQueries({ queryKey: ['admin', 'deployment-coverage'] })
  })
  await until(() =>
    expect(
      cache
        .getQueryCache()
        .findAll({ queryKey: ['admin', 'deployment-coverage'] })
        .some((q) => q.state.fetchStatus === 'fetching'),
    ).toBe(true),
  )
  await until(() => expect(button('Confirm full replacement')).toBeUndefined())
  expect(writes()).toHaveLength(0)
  await act(async () => {
    release()
    await renewed
  })
  await until(() => expect(document.querySelector('table')).not.toBeNull())
  expect(button('Confirm full replacement')).toBeUndefined()
  expect(button('Replace deployment coverage').disabled).toBe(true)
})
it('manual same-owner CSRF replacement while PUT pending rejects late saved facts and leaves exact retry', async () => {
  await mount()
  let release!: () => void
  hold = new Promise((done) => (release = done))
  await prepare()
  await click('Confirm full replacement')
  await until(() => expect(writes()).toHaveLength(1))
  await act(async () => cache.setQueryData(sessionKey, { ...session, csrf_token: 'fresh-manual' }))
  await act(async () => release())
  await until(() => expect(button('Retry exact coverage request')?.disabled).toBe(false))
  expect(saved).not.toHaveBeenCalled()
  hold = undefined
  await click('Retry exact coverage request')
  await until(() => expect(saved).toHaveBeenCalledTimes(1))
  expect(writes()[1].headers.get('X-CSRF-Token')).toBe('fresh-manual')
})
it('Session expiry clears fresh authority, hides private fields and aborts before stale response', async () => {
  await mount()
  let release!: () => void
  hold = new Promise((done) => (release = done))
  await prepare()
  await click('Confirm full replacement')
  await until(() => expect(writes()).toHaveLength(1))
  expired = true
  await act(async () => cache.setQueryData(sessionKey, null))
  await until(() => expect(document.querySelector('table')).toBeNull())
  await act(async () => release())
  expect(saved).not.toHaveBeenCalled()
  expect(writes()).toHaveLength(1)
})
