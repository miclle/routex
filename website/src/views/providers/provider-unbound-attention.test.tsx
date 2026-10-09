import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { getProviderModelBindings } from '@/api/provider-model-bindings'
import { sessionKey } from '@/hooks/use-auth'
import i18n from '@/i18n'
import type { Provider } from '@/types/catalog'
import type { ProviderModelBindings } from '@/types/provider-model-bindings'
import ProviderUnboundAttention from './provider-unbound-attention'

vi.mock('@/api/provider-model-bindings', () => ({ getProviderModelBindings: vi.fn() }))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

let cache: QueryClient
let root: Root
let host: HTMLDivElement
let provider: Provider
let projection: ProviderModelBindings
let onReview: ReturnType<typeof vi.fn<() => void>>
const requested = vi.mocked(getProviderModelBindings)
const actor = 'usr_attention'

beforeEach(async () => {
  await i18n.changeLanguage('en')
  requested.mockReset()
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  cache.setQueryData(sessionKey, {
    user: { id: actor, name: 'Reader', email: 'reader@example.invalid', role: 'member' },
    csrf_token: 'test-only-csrf',
  })
  cache.setQueryData(['permissions', actor], ['providers.read', 'models.read_all'])
  const model = (id: string, enabled: boolean) => ({
    id,
    upstream_name: id,
    enabled,
    supports_image_input: false,
    supports_pdf_input: false,
    etag: `test-${id}`,
  })
  cache.setQueryData<Provider[]>(
    ['admin', 'providers'],
    [
      {
        id: 'prv_attention',
        name: 'Recorded Provider',
        connections: [
          {
            id: 'con_attention',
            name: 'Recorded connection',
            base_url: 'https://provider.example.invalid/v1',
            protocol: 'openai_chat',
            credentials: [],
            provider_models: [model('pmd_unbound', true), model('pmd_bound', false)],
          },
        ],
      },
    ],
  )
  provider = cache.getQueryData<Provider[]>(['admin', 'providers'])![0]
  projection = {
    provider_id: provider.id,
    items: [
      {
        provider_model_id: 'pmd_bound',
        connection_id: 'con_attention',
        binding_count: 1,
        models: [{ id: 'mdl_retained', name: 'retained' }],
      },
      {
        provider_model_id: 'pmd_unbound',
        connection_id: 'con_attention',
        binding_count: 0,
        models: [],
      },
    ],
  }
  requested.mockImplementation(async () => structuredClone(projection))
  onReview = vi.fn()
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})

afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
})

async function render(target = provider) {
  await act(async () => {
    root.render(
      <QueryClientProvider client={cache}>
        <ProviderUnboundAttention provider={target} onReview={onReview} showEmpty />
      </QueryClientProvider>,
    )
  })
  await settle()
}
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}
function reviewButton() {
  return host.querySelector<HTMLButtonElement>('button')!
}

it('counts enabled unbound supply and retains disabled/zero-weight stored bindings as bound', async () => {
  await render()
  expect(host.textContent).toContain('1 provider model has no stored Model binding')
  expect(requested).toHaveBeenCalledTimes(1)
  expect(requested.mock.calls[0][0]).toBe(provider.id)
  expect(requested.mock.calls[0][1]).toBeInstanceOf(AbortSignal)
  await act(async () => reviewButton().click())
  expect(onReview).toHaveBeenCalledTimes(1)
  expect(host.textContent).not.toMatch(/healthy|traffic|service available/i)
})

it('shows no-attention only after complete authoritative zero', async () => {
  projection.items[1] = {
    ...projection.items[1],
    binding_count: 1,
    models: [{ id: 'mdl_second', name: null }],
  }
  await render()
  expect(host.textContent).toBe(i18n.t('catalog:providers.noAttention'))
  expect(host.querySelector('button')).toBeNull()
})

it.each([
  ['Provider-only reader', ['providers.read']],
  ['Model-only reader', ['models.read_all']],
  ['writer without reads', ['providers.write', 'models.write']],
])('does not query binding facts for %s', async (_, rights) => {
  cache.setQueryData(['permissions', actor], rights)
  await render()
  expect(requested).not.toHaveBeenCalled()
  expect(host.textContent).toContain('requires current Model read access')
  expect(host.textContent).not.toContain(i18n.t('catalog:providers.noAttention'))
})

it.each(['missing', 'extra', 'Connection alias', 'Provider alias', 'duplicate'])(
  'keeps %s projection Unknown instead of declaring unbound/zero',
  async (kind) => {
    if (kind === 'missing') projection.items.pop()
    if (kind === 'extra')
      projection.items.push({ ...projection.items[1], provider_model_id: 'pmd_extra' })
    if (kind === 'Connection alias') projection.items[1].connection_id = 'con_ATTENTION'
    if (kind === 'Provider alias') projection.provider_id = 'prv_ATTENTION'
    if (kind === 'duplicate') projection.items[1] = structuredClone(projection.items[0])
    await render()
    expect(host.textContent).toContain('Unbound model count: Unknown')
    expect(host.querySelector('button')).toBeNull()
  },
)

it('keeps failed/overflow reads unavailable without falling back to zero or configured enablement', async () => {
  requested.mockRejectedValue(new Error('bounded report unavailable'))
  await render()
  expect(host.textContent).toContain('Unbound model count is unavailable')
  expect(host.querySelector('button')).toBeNull()
})

it.each(['Session', 'permission', 'catalogue', 'projection', 'expiry'])(
  'rejects a captured navigation in the same turn as %s invalidation',
  async (kind) => {
    await render()
    const captured = reviewButton()
    await act(async () => {
      const key =
        kind === 'Session'
          ? sessionKey
          : kind === 'permission'
            ? ['permissions', actor]
            : kind === 'catalogue'
              ? ['admin', 'providers']
              : ['admin', 'provider-unbound-attention']
      if (kind === 'expiry') window.dispatchEvent(new Event('routex:session-expired'))
      else void cache.invalidateQueries({ queryKey: key, refetchType: 'none' })
      captured.click()
    })
    expect(onReview).not.toHaveBeenCalled()
    await settle()
    expect(host.querySelector('button')).toBeNull()
    expect(host.textContent).not.toContain('1 provider model has no stored Model binding')
  },
)

it('does not publish a late old actor/target response after renewal', async () => {
  let resolve!: (value: ProviderModelBindings) => void
  requested.mockImplementationOnce(
    () =>
      new Promise((done) => {
        resolve = done
      }),
  )
  await render()
  const old = structuredClone(projection)
  await act(async () => {
    cache.setQueryData(sessionKey, { user: { id: 'usr_other', role: 'member' }, csrf_token: 'new' })
    cache.setQueryData(['permissions', 'usr_other'], ['providers.read'])
  })
  await act(async () => resolve(old))
  await settle()
  expect(host.querySelector('button')).toBeNull()
  expect(host.textContent).not.toContain('1 provider model has no stored Model binding')
})

it('rejects a captured old Provider callback after the exact catalogue target is replaced', async () => {
  await render()
  const captured = reviewButton()
  await act(async () => {
    cache.setQueryData(['admin', 'providers'], [{ ...provider, id: 'prv_other' }])
    captured.click()
  })
  expect(onReview).not.toHaveBeenCalled()
  expect(host.querySelector('button')).toBeNull()
})

it('switches the count/action EN→ZH→EN without another binding read', async () => {
  await render()
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(host.textContent).toContain('1 个供应商模型没有已存储的模型绑定')
  expect(reviewButton().textContent).toBe('查看未绑定模型')
  await act(async () => {
    await i18n.changeLanguage('en')
  })
  expect(reviewButton().textContent).toBe('Review unbound models')
  expect(requested).toHaveBeenCalledTimes(1)
})
