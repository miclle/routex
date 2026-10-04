import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Routes, Route } from 'react-router'
import PriceImportsPage from '../index'
import ProviderModelPage from '@/views/pricing/provider-model'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { useSession, sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { RepositoryIntent, RepositorySelection } from '@/types/repository-prices'
import { RepositoryPriceCard } from './card'
import { RepositoryRateRestore } from './restore'
import { configuration, differences, result, digest } from './fixtures'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let configFailure = 0,
  protectedCustom = false,
  candidateNext = false,
  eligible = true,
  permissionFailure = 0
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  requests: InternalAxiosRequestConfig[],
  config: ReturnType<typeof configuration>,
  permissions: string[],
  actor: Session,
  status: number,
  writeHold: Promise<void> | undefined,
  configHold: Promise<void> | undefined,
  mode: 'card' | 'restore' | 'page' | 'provider',
  model: string,
  selected: string[],
  previewHold: Promise<void> | undefined
const original = client.defaults.adapter,
  api = '/admin/prices/repository'
function Screen() {
  if (mode === 'page')
    return (
      <MemoryRouter>
        <PriceImportsPage />
      </MemoryRouter>
    )
  if (mode === 'provider')
    return (
      <MemoryRouter initialEntries={['/admin/providers/prv_one/models/pmo_one']}>
        <Routes>
          <Route
            path="/admin/providers/:providerId/models/:modelId"
            element={<ProviderModelPage />}
          />
        </Routes>
      </MemoryRouter>
    )
  return <ScopedScreen />
}
function ScopedScreen() {
  const session = useSession()
  return mode === 'card' ? (
    <RepositoryPriceCard session={session} />
  ) : (
    <RepositoryRateRestore
      session={session}
      modelId={model}
      rateIds={selected}
      eligible={eligible}
    />
  )
}
function deferred() {
  let release!: () => void
  return {
    promise: new Promise<void>((resolve) => {
      release = resolve
    }),
    release: () => release(),
  }
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  requests = []
  configFailure = 0
  eligible = true
  permissionFailure = 0
  protectedCustom = false
  candidateNext = false
  config = configuration()
  permissions = ['prices.read', 'prices.write']
  actor = {
    user: { id: 'usr_reviewer', name: 'Reviewer', email: 'dummy@example.test', role: 'admin' },
    csrf_token: 'csrf-original',
  }
  status = 0
  writeHold = undefined
  configHold = undefined
  previewHold = undefined
  mode = 'card'
  model = 'pmo_one'
  selected = ['rat_one']
  client.defaults.adapter = async (request) => {
    requests.push(request)
    const response = {
      config: request,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (request.url === '/auth/session') response.data = structuredClone(actor)
    else if (request.url === '/auth/permissions') {
      if (permissionFailure) {
        response.status = permissionFailure
        throw new AxiosError('private', '', request, undefined, response)
      }
      response.data = { permissions: [...permissions] }
    } else if (request.url === '/admin/providers' || request.url === '/admin/models')
      response.data = { items: [] }
    else if (request.url === '/admin/prices')
      response.data = {
        etag: 'model-etag',
        currency: { platform_currency: 'USD', rates: { USD: '1' } },
        items: [
          {
            id: 'prc_one',
            provider_model_id: 'pmo_one',
            provider_id: 'prv_one',
            upstream_name: 'Price model',
            protocol: 'openai_chat',
            context_threshold: 0,
            update_source: 'api',
            follow_repository: false,
            rate_sources: {
              rat_one: { kind: 'custom', source_model_key: null, source_rate_key: null },
            },
            rates: [
              {
                id: 'rat_one',
                metric: 'INPUT_TOKEN',
                tier: 'base',
                unit: '1M_TOKEN',
                currency: 'USD',
                amount: '0',
                enabled: false,
              },
            ],
          },
        ],
      }
    else if (request.url === api && request.method === 'get') {
      if (configFailure) {
        response.status = configFailure
        throw new AxiosError('private', '', request, undefined, response)
      }
      const captured = structuredClone(config)
      if (configHold) await configHold
      response.data = captured
    } else if (request.url === api + '/candidates')
      response.data = {
        items: [
          {
            provider_model_id: request.params?.cursor ? 'pmo_two' : 'pmo_one',
            upstream_name: request.params?.cursor ? 'Second eligible model' : 'Eligible model',
            protocol: 'openai_chat',
          },
        ],
        next_cursor: candidateNext && !request.params?.cursor ? 'pmo_one' : '',
      }
    else if (request.url === api + '/preview') {
      const selection = JSON.parse(request.data) as RepositorySelection
      const captured = differences(selection)
      if (protectedCustom) {
        captured.changes[0].action = 'protected_custom'
        captured.changes[0].after = captured.changes[0].before
        captured.changes[0].after_source = captured.changes[0].before_source
      }
      if (previewHold) await previewHold
      response.data = captured
    } else if (
      (request.url === api && request.method === 'put') ||
      request.url === api + '/apply' ||
      request.url?.startsWith(api + '/receipts/')
    ) {
      if (writeHold) await writeHold
      if (status) {
        response.status = status
        throw new AxiosError('Sanitized', '', request, undefined, response)
      }
      const previous = request.url?.startsWith(api + '/receipts/')
        ? [...requests].reverse().find((c) => c.method === 'put' || c.url === api + '/apply')!
        : request
      const input = JSON.parse(previous.data)
      const intent = (
        previous.method === 'put' ? { kind: 'configure', input } : { kind: 'apply', input }
      ) as RepositoryIntent
      intent.etag = String(previous.headers.get('If-Match')).slice(1, -1)
      intent.sourceDigest = digest
      response.data = result(intent)
      if (request.method === 'put') {
        config = {
          ...config,
          enabled: input.enabled,
          mappings: input.mappings,
          review_etag: 'd'.repeat(64),
        }
      }
    } else throw new Error(`Unexpected authorized seam ${request.url}`)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  client.defaults.adapter = original
  host.remove()
  vi.restoreAllMocks()
  await i18n.changeLanguage('en')
})
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 10))
  })
}
async function until(check: () => void) {
  for (let i = 0; i < 100; i++) {
    await flush()
    try {
      check()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
  check()
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Screen />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(host.textContent).toContain(
      mode === 'card' || mode === 'page'
        ? 'Repository digest'
        : mode === 'provider'
          ? '0 USD'
          : 'Restore selected repository prices',
    ),
  )
}
function button(label: string) {
  const element = [...document.querySelectorAll('button')].find(
    (b) => b.textContent?.trim() === label,
  )
  expect(element, `button ${label}`).toBeTruthy()
  return element!
}
async function click(label: string) {
  await act(async () => button(label).click())
  await flush()
}
function dialog() {
  return document.querySelector('[role="dialog"]')!
}
async function input(label: string, value: string) {
  const element = [...document.querySelectorAll('label')]
    .find((l) => l.textContent?.startsWith(label))
    ?.querySelector('input')
  expect(element).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, value)
    element!.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await flush()
}
async function prepareConfig() {
  await click('Edit synchronization scope')
  await until(() => expect(document.body.textContent).toContain('Eligible model'))
  await input('Reason', 'Keep explicit mapping')
  await click('Review configuration save')
  await until(() => expect(document.body.textContent).toContain('Confirm repository operation'))
}
const writes = () => requests.filter((r) => r.method === 'put' || r.url === api + '/apply')
describe('repository card real Session and immutable intent', () => {
  it('adds the card without inventing prices for an empty shipped source', async () => {
    config.source.models = []
    config.source.model_count = 0
    await render()
    expect(host.textContent).toContain('shipped repository contains no prices')
    expect(button('Preview synchronization').disabled).toBe(true)
    expect(writes()).toHaveLength(0)
    expect(requests.some((r) => r.url?.includes('/providers'))).toBe(false)
  })
  it('a toggle opens review but never writes or applies automatically', async () => {
    await render()
    await act(async () => host.querySelector<HTMLButtonElement>('[role="switch"]')!.click())
    await flush()
    expect(dialog().textContent).toContain('Enabling and mapping do not apply prices')
    expect(writes()).toHaveLength(0)
  })
  it('uses independent read/write gates and live bilingual labels', async () => {
    permissions = ['prices.read']
    config.can_write = false
    await render()
    expect(host.querySelector('[role="switch"]')!.getAttribute('aria-disabled')).toBe('true')
    await click('Edit synchronization scope')
    expect(button('Review configuration save').disabled).toBe(true)
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('编辑同步范围')
    expect(document.body.textContent).not.toContain('repository.')
    expect(writes()).toHaveLength(0)
  })
  it('explicit config confirmation records history and changes no prices', async () => {
    await render()
    await prepareConfig()
    expect(writes()).toHaveLength(0)
    await click('Confirm operation')
    await until(() =>
      expect(host.textContent).toContain('This receipt records a configuration commit'),
    )
    expect(writes()).toHaveLength(1)
    expect(JSON.parse(writes()[0].data).reason).toBe('Keep explicit mapping')
    expect(requests.some((r) => r.url === api + '/apply')).toBe(false)
  })
  it('retains exact unknown intent across receipt404, original409 and original403 retries', async () => {
    await render()
    status = 500
    await prepareConfig()
    await click('Confirm operation')
    await until(() =>
      expect(host.querySelector('[aria-label="Unresolved original operation"]')).not.toBeNull(),
    )
    const originalWrite = writes()[0]
    status = 404
    await click('Check original receipt')
    await until(() =>
      expect(host.textContent).toContain('does not prove that the original operation failed'),
    )
    expect(writes()).toHaveLength(1)
    for (const rejected of [409, 403]) {
      status = rejected
      await click('Retry original operation')
      await until(() => expect(host.textContent).toContain('retry was rejected'))
      const retry = writes().at(-1)!
      expect(retry.data).toBe(originalWrite.data)
      expect(retry.headers.get('If-Match')).toBe(originalWrite.headers.get('If-Match'))
    }
    expect(writes()).toHaveLength(3)
  })
  it('successful structurally identical same-ms Session reads renew scoped facts, bound requests and suppress an old preview', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(1800000000000)
    await render()
    const hold = deferred()
    previewHold = hold.promise
    await click('Preview synchronization')
    const old = requests.find((r) => r.url === api + '/preview')!
    const before = requests.filter((r) => r.url === api).length
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey, exact: true })
    })
    await until(() => expect(requests.filter((r) => r.url === api).length).toBeGreaterThan(before))
    hold.release()
    await flush()
    await flush()
    expect(old.signal?.aborted).toBe(true)
    expect(document.body.textContent).not.toContain('Repository price differences')
    expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2)
    expect(writes()).toHaveLength(0)
  })
  it('same-actor manual CSRF replacement retains review and dispatches current token', async () => {
    await render()
    await prepareConfig()
    const before = requests.filter((r) => r.url === api && r.method === 'get').length
    await act(async () => cache.setQueryData(sessionKey, { ...actor, csrf_token: 'csrf-rotated' }))
    await click('Confirm operation')
    await until(() => expect(writes()).toHaveLength(1))
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf-rotated')
    expect(requests.filter((r) => r.url === api && r.method === 'get').length).toBeLessThanOrEqual(
      before + 1,
    )
  })
  it('actor change aborts original dispatch and cannot disclose an old response or replay', async () => {
    await render()
    await prepareConfig()
    const hold = deferred()
    writeHold = hold.promise
    await click('Confirm operation')
    const write = writes()[0]
    actor = { ...actor, user: { ...actor.user, id: 'usr_other' } }
    permissions = []
    await act(async () => cache.setQueryData(sessionKey, actor))
    hold.release()
    await flush()
    await flush()
    expect(write.signal?.aborted).toBe(true)
    expect(document.body.textContent).not.toContain('Keep explicit mapping')
    expect(document.body.textContent).not.toContain('Recorded operation receipt')
    expect(writes()).toHaveLength(1)
  })
  it('hide current private configuration during a failed renewed read', async () => {
    await render()
    permissions = []
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey, exact: true })
    })
    await until(() => expect(host.textContent).not.toContain('Repository digest'))
    expect(host.textContent).not.toContain('pmo_one')
    expect(writes()).toHaveLength(0)
  })
  it('synchronous duplicate clicks cannot submit twice', async () => {
    await render()
    await prepareConfig()
    const hold = deferred()
    writeHold = hold.promise
    const confirm = button('Confirm operation')
    await act(async () => {
      confirm.click()
      confirm.click()
    })
    expect(writes()).toHaveLength(1)
    hold.release()
    await until(() => expect(host.textContent).toContain('Recorded operation receipt'))
  })
  it('retains configuration conflict draft and requires explicit fresh review', async () => {
    await render()
    status = 409
    await prepareConfig()
    await click('Confirm operation')
    await until(() =>
      expect(document.body.textContent).toContain(
        'Configuration, prices or repository contents changed',
      ),
    )
    expect(
      (dialog().querySelector('input[value="Keep explicit mapping"]') as HTMLInputElement)?.value,
    ).toBe('Keep explicit mapping')
    expect(button('Review configuration save').disabled).toBe(true)
    status = 0
    await click('Review current configuration')
    await until(() => expect(button('Review configuration save').disabled).toBe(false))
    expect(writes()).toHaveLength(1)
  })
})
describe('bounded selection and renewed authority', () => {
  it('rejects a click from old DOM synchronously during same-ms completed Session renewal', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(1800000000000)
    await render()
    const previous = button('Preview synchronization')
    const off = cache.getQueryCache().subscribe((event) => {
      if (
        event.type === 'updated' &&
        event.action.type === 'success' &&
        !event.action.manual &&
        JSON.stringify(event.query.queryKey) === JSON.stringify(sessionKey)
      )
        previous.click()
    })
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey, exact: true })
    })
    off()
    await flush()
    expect(requests.some((request) => request.url === api + '/preview')).toBe(false)
    expect(writes()).toHaveLength(0)
    await until(() => expect(button('Preview synchronization').disabled).toBe(false))
    await click('Preview synchronization')
    await until(() => expect(document.body.textContent).toContain('Repository price differences'))
  })

  it('keeps exact mappings selected outside the current candidate page', async () => {
    candidateNext = true
    await render()
    await click('Edit synchronization scope')
    await until(() => expect(document.body.textContent).toContain('Eligible model'))
    await click('Next')
    await until(() => expect(document.body.textContent).toContain('Second eligible model'))
    expect(dialog().textContent).toContain('pmo_one')
    expect(dialog().textContent).toContain('provider/model')
    const call = requests.filter((r) => r.url === api + '/candidates').at(-1)!
    expect(call.params).toMatchObject({ cursor: 'pmo_one', limit: 20 })
    expect(writes()).toHaveLength(0)
  })
  it('requires explicit bounded model selection for a scope above twenty, never silently chooses twenty', async () => {
    config.mappings = Array.from({ length: 21 }, (_, i) => ({
      provider_model_id: `pmo_${i}`,
      source_model_key: 'provider/model',
    }))
    await render()
    await click('Preview synchronization')
    expect(requests.some((r) => r.url === api + '/preview')).toBe(false)
    expect(dialog().textContent).toContain('one to twenty')
    const check = dialog().querySelector<HTMLInputElement>('input[type="checkbox"]')!
    await act(async () => check.click())
    await act(async () =>
      [...dialog().querySelectorAll('button')]
        .find((button) => button.textContent?.trim() === 'Preview synchronization')!
        .click(),
    )
    await until(() => expect(document.body.textContent).toContain('Repository price differences'))
    expect(
      JSON.parse(requests.find((r) => r.url === api + '/preview')!.data).provider_model_ids,
    ).toEqual(['pmo_0'])
  })
  it('server-protected custom disabled zero remains unchanged and cannot be applied', async () => {
    protectedCustom = true
    await render()
    await click('Preview synchronization')
    await until(() => expect(document.body.textContent).toContain('Custom price preserved'))
    expect(dialog().textContent).toContain('0 USD')
    expect(button('Review price application').disabled).toBe(true)
    expect(writes()).toHaveLength(0)
  })
  it('hides private facts on GET403 without discarding an uncertain original operation', async () => {
    await render()
    status = 500
    await prepareConfig()
    await click('Confirm operation')
    await until(() =>
      expect(host.querySelector('[aria-label="Unresolved original operation"]')).not.toBeNull(),
    )
    const original = writes()[0]
    configFailure = 403
    await click('Refresh repository status')
    await until(() => expect(host.textContent).not.toContain('Repository digest'))
    expect(host.textContent).not.toContain('Keep explicit mapping')
    configFailure = 0
    await click('Refresh repository status')
    await until(() =>
      expect(host.querySelector('[aria-label="Unresolved original operation"]')).not.toBeNull(),
    )
    status = 403
    await click('Retry original operation')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].data).toBe(original.data)
  })
  it('an ordinary Session renewal hides facts while current status is pending, then requires no automatic mutation', async () => {
    await render()
    const hold = deferred()
    configHold = hold.promise
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey, exact: true })
    })
    await until(() => expect(host.textContent).not.toContain('Repository digest'))
    expect(host.textContent).not.toContain('pmo_one')
    configHold = undefined
    hold.release()
    await until(() => expect(host.textContent).toContain('Repository digest'))
    expect(writes()).toHaveLength(0)
  })
  it('a current role change hides status and does not retain administrative action authority', async () => {
    await render()
    actor = { ...actor, user: { ...actor.user, role: 'member' } }
    permissions = []
    await act(async () => cache.setQueryData(sessionKey, actor))
    await until(() => expect(host.textContent).not.toContain('Repository digest'))
    expect(writes()).toHaveLength(0)
  })
})
describe('persistent production parent composition', () => {
  it('price workspace retains unknown intent through an outer permission query error', async () => {
    mode = 'page'
    await render()
    status = 500
    await prepareConfig()
    await click('Confirm operation')
    await until(() =>
      expect(host.querySelector('[aria-label="Unresolved original operation"]')).not.toBeNull(),
    )
    const original = writes()[0]
    permissionFailure = 500
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey, exact: true })
    })
    await until(() => expect(host.textContent).not.toContain('Keep explicit mapping'))
    expect(host.textContent).not.toContain('Upload prices')
    permissionFailure = 0
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey, exact: true })
    })
    await until(() =>
      expect(host.querySelector('[aria-label="Unresolved original operation"]')).not.toBeNull(),
    )
    status = 409
    await click('Retry original operation')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].data).toBe(original.data)
  })
  it('provider-detail price restoration remains mounted across permission renewal failure', async () => {
    mode = 'provider'
    await render()
    const checkbox = host.querySelector<HTMLInputElement>('input[type="checkbox"]')!
    await act(async () => checkbox.click())
    await click('Restore selected repository prices')
    await until(() => expect(dialog().textContent).toContain('Repository price differences'))
    await input('Reason', 'Retain original selected price')
    await click('Review price application')
    status = 500
    await click('Confirm operation')
    await until(() =>
      expect(host.querySelector('[aria-label="Unresolved original operation"]')).not.toBeNull(),
    )
    permissionFailure = 500
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey, exact: true })
    })
    await until(() => expect(host.textContent).not.toContain('Retain original selected price'))
    permissionFailure = 0
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey, exact: true })
    })
    await until(() =>
      expect(host.querySelector('[aria-label="Unresolved original operation"]')).not.toBeNull(),
    )
    expect(host.textContent).toContain('rat_one')
    expect(writes()).toHaveLength(1)
  })
})
describe('historical receipt without current selected-price availability', () => {
  it('retains the original request and resolves its exact receipt without current-rate facts or replay', async () => {
    mode = 'restore'
    await render()
    await click('Restore selected repository prices')
    await until(() => expect(dialog().textContent).toContain('Repository price differences'))
    await input('Reason', 'Original retained restoration')
    await click('Review price application')
    status = 500
    await click('Confirm operation')
    await until(() =>
      expect(host.querySelector('[aria-label="Unresolved original operation"]')).not.toBeNull(),
    )
    eligible = false
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <Screen />
        </QueryClientProvider>,
      ),
    )
    expect(host.querySelector('[aria-label="Unresolved original operation"]')).not.toBeNull()
    expect(
      [...host.querySelectorAll('button')].some(
        (button) => button.textContent === 'Restore selected repository prices',
      ),
    ).toBe(false)
    status = 0
    await click('Check original receipt')
    await until(() => expect(host.textContent).toContain('historical price commit'))
    expect(writes()).toHaveLength(1)
  })
})
describe('selected-rate restoration', () => {
  it('previews only explicitly selected rate IDs including a custom disabled zero', async () => {
    mode = 'restore'
    await render()
    await click('Restore selected repository prices')
    await until(() => expect(dialog().textContent).toContain('1.234567890123456789'))
    expect(dialog().textContent).toContain('0 USD')
    expect(dialog().textContent).toContain('Disabled')
    const preview = requests.find((r) => r.url === api + '/preview')!
    expect(JSON.parse(preview.data)).toEqual({
      mode: 'restore',
      provider_model_ids: ['pmo_one'],
      rate_ids: ['rat_one'],
    })
    expect(writes()).toHaveLength(0)
    await input('Reason', 'Explicit selected restoration')
    await click('Review price application')
    await click('Confirm operation')
    await until(() => expect(host.textContent).toContain('historical price commit'))
    expect(JSON.parse(writes()[0].data).selection.rate_ids).toEqual(['rat_one'])
  })
  it('unknown original restoration survives selection changes but target change cancels its authority', async () => {
    mode = 'restore'
    await render()
    status = 500
    await click('Restore selected repository prices')
    await until(() => expect(dialog().textContent).toContain('Repository price differences'))
    await input('Reason', 'Restore original zero')
    await click('Review price application')
    await click('Confirm operation')
    await until(() =>
      expect(host.querySelector('[aria-label="Unresolved original operation"]')).not.toBeNull(),
    )
    selected = []
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <Screen />
        </QueryClientProvider>,
      ),
    )
    expect(host.textContent).toContain('rat_one')
    model = 'pmo_other'
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <Screen />
        </QueryClientProvider>,
      ),
    )
    await flush()
    expect(host.textContent).not.toContain('Restore original zero')
    expect(writes()).toHaveLength(1)
  })
  it('late old-target preview cannot become new-target differences', async () => {
    mode = 'restore'
    await render()
    const hold = deferred()
    previewHold = hold.promise
    await click('Restore selected repository prices')
    const old = requests.find((r) => r.url === api + '/preview')!
    model = 'pmo_other'
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <Screen />
        </QueryClientProvider>,
      ),
    )
    hold.release()
    await flush()
    await flush()
    expect(old.signal?.aborted).toBe(true)
    expect(document.body.textContent).not.toContain('Repository price differences')
  })
})
