import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { getProviderModelBindings } from '@/api/provider-model-bindings'
import { sessionKey } from '@/hooks/use-auth'
import i18n from '@/i18n'
import type { Provider } from '@/types/catalog'
import ProviderDisabledConnectionAttention from './provider-disabled-connection-attention'
import { ProviderOverview } from './detail'

vi.mock('@/api/provider-model-bindings', () => ({ getProviderModelBindings: vi.fn() }))
vi.mock('@/api/provider-quality', async (importOriginal) => {
  const original = await importOriginal<typeof import('@/api/provider-quality')>()
  return {
    ...original,
    getProviderQuality: vi.fn().mockRejectedValue(new Error('controlled quality unavailable')),
  }
})
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

let cache: QueryClient
let root: Root
let host: HTMLDivElement
let provider: Provider
let onReview: ReturnType<typeof vi.fn<() => void>>
let onSelectTab: ReturnType<typeof vi.fn<(tab: string) => void>>
const actor = 'usr_disabled_attention'
const requested = vi.mocked(getProviderModelBindings)

beforeEach(async () => {
  await i18n.changeLanguage('en')
  requested.mockReset()
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  cache.setQueryData(sessionKey, {
    user: { id: actor, name: 'Reader', email: 'reader@example.invalid', role: 'member' },
    csrf_token: 'test-only-csrf',
  })
  cache.setQueryData(['permissions', actor], ['providers.read', 'models.read_all'])
  cache.setQueryData<Provider[]>(
    ['admin', 'providers'],
    [
      {
        id: 'prv_disabled_attention',
        name: 'Recorded Provider',
        connections: [
          {
            id: 'con_disabled_attention',
            name: 'Recorded connection',
            enabled: false,
            base_url: 'https://provider.example.invalid/v1',
            protocol: 'openai_chat',
            credentials: [
              {
                id: 'crd_disabled_attention',
                name: 'Recorded credential',
                priority: 0,
                enabled: true,
                verification_status: 'verified',
                verified_at: null,
              },
            ],
            provider_models: [
              {
                id: 'pmd_disabled_attention',
                upstream_name: 'recorded-model',
                enabled: true,
                supports_image_input: false,
                supports_pdf_input: false,
                etag: 'test-only-model',
              },
            ],
          },
        ],
      },
    ],
  )
  provider = cache.getQueryData<Provider[]>(['admin', 'providers'])![0]
  requested.mockImplementation(async () => ({
    provider_id: provider.id,
    items: provider.connections.flatMap((connection) =>
      connection.provider_models.map((model) => ({
        provider_model_id: model.id,
        connection_id: connection.id,
        binding_count: 1,
        models: [{ id: 'mdl_retained', name: 'retained' }],
      })),
    ),
  }))
  onReview = vi.fn<() => void>()
  onSelectTab = vi.fn<(tab: string) => void>()
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})

afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
})

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}
async function render(overview = false) {
  await act(async () => {
    root.render(
      <QueryClientProvider client={cache}>
        {overview ? (
          <ProviderOverview provider={provider} onSelectTab={onSelectTab} />
        ) : (
          <ProviderDisabledConnectionAttention provider={provider} onReview={onReview} />
        )}
      </QueryClientProvider>,
    )
  })
  await settle()
}
function reviewButton() {
  const found = [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (button) => button.textContent === i18n.t('catalog:providerDisabledAttention.review'),
  )
  expect(found).toBeDefined()
  return found!
}
function replaceProvider(value: Provider) {
  cache.setQueryData(['admin', 'providers'], [value])
  provider = cache.getQueryData<Provider[]>(['admin', 'providers'])![0]
}

it('counts only stored false Connections and reviews without new reads or write authority', async () => {
  const connection = provider.connections[0]
  replaceProvider({
    ...provider,
    connections: [
      connection,
      { ...connection, id: 'con_second', enabled: false },
      { ...connection, id: 'con_enabled', enabled: true },
    ],
  })
  cache.setQueryData(['permissions', actor], ['providers.read'])
  await render()
  expect(host.textContent).toContain('2 connections are disabled')
  await act(async () => reviewButton().click())
  expect(onReview).toHaveBeenCalledTimes(1)
  expect(requested).not.toHaveBeenCalled()
  expect(host.textContent).not.toMatch(/health|traffic|routing ready/i)
})

it('renders no disabled issue for an explicitly enabled Connection', async () => {
  replaceProvider({
    ...provider,
    connections: [{ ...provider.connections[0], enabled: true }],
  })
  await render()
  expect(host.textContent).toBe('')
  expect(host.querySelector('button')).toBeNull()
})

it('does not turn absent enablement into disabled or a known zero', async () => {
  const connection = { ...provider.connections[0] }
  delete connection.enabled
  replaceProvider({ ...provider, connections: [connection] })
  await render()
  expect(host.textContent).toBe('Connection enablement: Unknown')
  expect(host.querySelector('button')).toBeNull()
})

it('keeps an exact disabled count alongside unknown enablement', async () => {
  replaceProvider({
    ...provider,
    connections: [
      provider.connections[0],
      { ...provider.connections[0], id: 'con_unknown', enabled: undefined },
    ],
  })
  await render()
  expect(host.textContent).toContain('1 connection is disabled')
  expect(host.textContent).toContain('Connection enablement: Unknown')
})

it.each(['disabled', 'unknown'])(
  'never claims no attention for %s while retaining complete bound-model projection',
  async (kind) => {
    replaceProvider({
      ...provider,
      connections: [
        { ...provider.connections[0], enabled: kind === 'disabled' ? false : undefined },
      ],
    })
    await render(true)
    expect(host.textContent).not.toContain(i18n.t('catalog:providers.noAttention'))
    expect(host.textContent).toContain(
      kind === 'disabled' ? '1 connection is disabled' : 'Connection enablement: Unknown',
    )
    if (kind === 'disabled') {
      await act(async () => reviewButton().click())
      expect(onSelectTab).toHaveBeenCalledWith('connections')
    }
  },
)

it('preserves unbound Model attention alongside disabled Connections', async () => {
  requested.mockResolvedValue({
    provider_id: provider.id,
    items: [
      {
        provider_model_id: provider.connections[0].provider_models[0].id,
        connection_id: provider.connections[0].id,
        binding_count: 0,
        models: [],
      },
    ],
  })
  await render(true)
  expect(host.textContent).toContain('1 connection is disabled')
  expect(host.textContent).toContain('1 provider model has no stored Model binding')
  expect(host.textContent).not.toContain(i18n.t('catalog:providers.noAttention'))
})

it('preserves no-attention after explicitly enabled configuration and complete zero unbound count', async () => {
  replaceProvider({
    ...provider,
    enabled: true,
    connections: [{ ...provider.connections[0], enabled: true }],
  })
  await render(true)
  expect(host.textContent).toContain(i18n.t('catalog:providers.noAttention'))
  expect(host.textContent).not.toContain('Connection enablement: Unknown')
})

it('preserves unrelated configuration and disabled-model attention', async () => {
  const connection = provider.connections[0]
  replaceProvider({
    ...provider,
    connections: [
      {
        ...connection,
        credentials: [{ ...connection.credentials[0], enabled: false }],
        provider_models: [{ ...connection.provider_models[0], enabled: false }],
      },
    ],
  })
  await render(true)
  expect(host.textContent).toContain(
    i18n.t('catalog:providers.connectionsNeedAttention', { count: 1 }),
  )
  expect(host.textContent).toContain(
    i18n.t('catalog:providers.credentialsNeedAttention', { count: 1 }),
  )
  expect(host.textContent).toContain(i18n.t('catalog:providers.modelsNeedAttention', { count: 1 }))
  expect(host.textContent).toContain('1 connection is disabled')
})

it.each(['Session', 'permission', 'catalogue', 'expiry', 'actor', 'target', 'renewal'])(
  'rejects captured navigation in the same turn as %s changes',
  async (kind) => {
    await render()
    const captured = reviewButton()
    await act(async () => {
      if (kind === 'expiry') window.dispatchEvent(new Event('routex:session-expired'))
      else if (kind === 'actor')
        cache.setQueryData(sessionKey, { user: { id: 'usr_other' }, csrf_token: 'new' })
      else if (kind === 'target')
        cache.setQueryData(['admin', 'providers'], [{ ...provider, id: 'prv_other' }])
      else if (kind === 'renewal')
        cache.setQueryData(sessionKey, { user: { id: actor }, csrf_token: 'renewed' })
      else
        void cache.invalidateQueries({
          queryKey:
            kind === 'Session'
              ? sessionKey
              : kind === 'permission'
                ? ['permissions', actor]
                : ['admin', 'providers'],
          refetchType: 'none',
        })
      captured.click()
    })
    expect(onReview).not.toHaveBeenCalled()
  },
)

it('hides the count and rejects a captured action after permission revocation', async () => {
  await render()
  const captured = reviewButton()
  await act(async () => {
    cache.setQueryData(['permissions', actor], [])
    captured.click()
  })
  await settle()
  expect(onReview).not.toHaveBeenCalled()
  expect(host.querySelector('button')).toBeNull()
  expect(host.textContent).not.toContain('1 connection is disabled')
})

it('switches EN→ZH→EN without reading or changing catalogue facts', async () => {
  await render()
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(host.textContent).toContain('1 个接入已停用')
  expect(reviewButton().textContent).toBe('查看接入')
  await act(async () => {
    await i18n.changeLanguage('en')
  })
  expect(reviewButton().textContent).toBe('Review connections')
  expect(requested).not.toHaveBeenCalled()
  expect(provider.connections[0].enabled).toBe(false)
})

function configurationBadge() {
  const heading = [...host.querySelectorAll('h3')].find(
    (item) => item.textContent === i18n.t('catalog:providers.serviceStatus'),
  )
  expect(heading).toBeDefined()
  return heading!.parentElement!.querySelector('span')!
}
function connectionConfigurationCells() {
  const table = [...host.querySelectorAll('table')].find(
    (item) => item.getAttribute('aria-label') === i18n.t('catalog:providers.connectionRuntime'),
  )
  expect(table).toBeDefined()
  return [...table!.querySelectorAll('tbody tr')].map((row) => row.querySelectorAll('td')[2])
}
function structuralConnectionCount() {
  const label = [...host.querySelectorAll('dt')].find(
    (item) => item.textContent === i18n.t('catalog:providers.readyConnections'),
  )
  expect(label).toBeDefined()
  return label!.nextElementSibling!.textContent
}
function enabledOverviewProvider() {
  return {
    ...provider,
    enabled: true,
    connections: [{ ...provider.connections[0], enabled: true }],
  }
}

it('shows stored configuration readiness for an explicitly enabled read-only Provider and Connection', async () => {
  replaceProvider(enabledOverviewProvider())
  cache.setQueryData(['permissions', actor], ['providers.read'])
  await render(true)
  expect(configurationBadge().textContent).toBe('Configuration ready')
  expect(connectionConfigurationCells()[0].textContent).toBe('Configuration ready')
  expect(structuralConnectionCount()).toBe('1 / 1')
  expect(host.textContent).toContain(
    'these facts do not confirm live health or runtime application',
  )
  expect(requested).not.toHaveBeenCalled()
})

it.each(['Provider', 'Connection'])(
  'never presents disabled %s configuration as ready and preserves structural counts',
  async (kind) => {
    const value = enabledOverviewProvider()
    if (kind === 'Provider') value.enabled = false
    else value.connections[0].enabled = false
    replaceProvider(value)
    await render(true)
    expect(configurationBadge().textContent).toBe('Disabled by configuration')
    expect(connectionConfigurationCells()[0].textContent).toBe('Disabled by configuration')
    expect(structuralConnectionCount()).toBe('1 / 1')
    expect(host.textContent).not.toContain(i18n.t('catalog:providers.noAttention'))
    expect(provider.connections[0].credentials[0].enabled).toBe(true)
    expect(provider.connections[0].provider_models[0].enabled).toBe(true)
  },
)

it.each(['Provider', 'Connection'])(
  'keeps absent %s enablement unknown rather than ready, disabled or incomplete',
  async (kind) => {
    const value: Provider = enabledOverviewProvider()
    if (kind === 'Provider') delete value.enabled
    else delete value.connections[0].enabled
    replaceProvider(value)
    await render(true)
    expect(configurationBadge().textContent).toBe('Configuration availability: Unknown')
    expect(connectionConfigurationCells()[0].textContent).toBe(
      'Configuration availability: Unknown',
    )
    expect(structuralConnectionCount()).toBe('1 / 1')
    expect(host.textContent).not.toContain(i18n.t('catalog:providers.noAttention'))
  },
)

it('separates mixed ready, disabled and unknown Connection rows without redefining child counts', async () => {
  const value = enabledOverviewProvider()
  replaceProvider({
    ...value,
    connections: [
      value.connections[0],
      { ...value.connections[0], id: 'con_disabled', enabled: false },
      { ...value.connections[0], id: 'con_unknown', enabled: undefined },
    ],
  })
  await render(true)
  expect(configurationBadge().textContent).toBe('Configuration ready')
  expect(connectionConfigurationCells().map((cell) => cell.textContent)).toEqual([
    'Configuration ready',
    'Disabled by configuration',
    'Configuration availability: Unknown',
  ])
  expect(structuralConnectionCount()).toBe('3 / 3')
})

it('does not turn an enabled incomplete Connection plus unknown enablement into known unavailability', async () => {
  const value = enabledOverviewProvider()
  replaceProvider({
    ...value,
    connections: [
      { ...value.connections[0], credentials: [] },
      { ...value.connections[0], id: 'con_unknown', enabled: undefined },
    ],
  })
  await render(true)
  expect(configurationBadge().textContent).toBe('Configuration availability: Unknown')
  expect(connectionConfigurationCells()[0].textContent).toBe('Configuration incomplete')
})

it.each(['empty', 'missing credential', 'missing model'])(
  'keeps known %s configuration incomplete instead of unknown',
  async (kind) => {
    const value = enabledOverviewProvider()
    if (kind === 'empty') value.connections = []
    else if (kind === 'missing credential') value.connections[0].credentials = []
    else value.connections[0].provider_models = []
    replaceProvider(value)
    await render(true)
    expect(configurationBadge().textContent).toBe('Configuration incomplete')
  },
)

it('updates disabled and unknown stored configuration guidance live in EN and ZH', async () => {
  const value: Provider = enabledOverviewProvider()
  delete value.connections[0].enabled
  replaceProvider(value)
  await render(true)
  expect(configurationBadge().textContent).toBe('Configuration availability: Unknown')
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(configurationBadge().textContent).toBe('配置可用性：未知')
  expect(connectionConfigurationCells()[0].textContent).toBe('配置可用性：未知')
  expect(host.textContent).toContain('这些信息不证明实时健康状态或运行时配置已应用')
  replaceProvider({ ...provider, enabled: false })
  await render(true)
  expect(configurationBadge().textContent).toBe('配置已停用')
  await act(async () => {
    await i18n.changeLanguage('en')
  })
  expect(configurationBadge().textContent).toBe('Disabled by configuration')
  expect(connectionConfigurationCells()[0].textContent).toBe('Disabled by configuration')
  expect(host.textContent).toContain(
    'these facts do not confirm live health or runtime application',
  )
})
