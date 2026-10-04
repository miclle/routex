import { MemoryRouter, Route, Routes } from 'react-router'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import KeysPage from '@/views/keys'
import ProvidersPage from '@/views/providers'
import AdminModelsPage from '@/views/models/admin'
import CreateModelPage from '@/views/models/create'
import ModelsPage from '@/views/models'
import client from '@/api/client'
import i18n from '@/i18n'
import type { CallableModel, Model, PersonalKey, Provider } from '@/types/catalog'
import type { ModelCreationInput, ModelCreationResult } from '@/types/model-creation'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root
let container: HTMLDivElement
let cache: QueryClient
let role: 'admin' | 'member'
let requests: InternalAxiosRequestConfig[]
let failures: Record<string, number>
let keys: PersonalKey[]
let provider: Provider
let model: Model
let callableModels: CallableModel[]
const secret = 'rx_test_one_time_secret'
const originalAdapter = client.defaults.adapter
const makeKey = (status: PersonalKey['status'] = 'pending'): PersonalKey => ({
  id: 'key_1',
  name: 'Test Key',
  prefix: 'rx_test',
  status,
  model_ids: ['mdl_1'],
  expires_at: null,
  created_at: '2026-09-23T00:00:00Z',
  replaces_key_id: null,
  delivery_expires_at: '2026-09-23T00:10:00Z',
})

beforeEach(() => {
  callableModels = [{ id: 'mdl_1', name: 'Model', status: 'active', protocol: 'openai_chat' }]
  role = 'admin'
  requests = []
  failures = {}
  keys = []
  provider = {
    id: 'prv_1',
    name: 'Provider',
    connections: [
      {
        id: 'con_1',
        name: 'Primary',
        base_url: 'https://api.example.com/v1',
        protocol: 'openai_chat',
        credentials: [
          {
            id: 'cre_1',
            name: 'Credential',
            priority: 0,
            enabled: false,
            verification_status: 'pending',
            verified_at: null,
          },
        ],
        provider_models: [
          {
            id: 'pm_1',
            upstream_name: 'upstream-model',
            enabled: true,
            supports_image_input: false,
            supports_pdf_input: false,
            etag: '0',
          },
        ],
      },
    ],
  }
  model = {
    id: 'mdl_1',
    name: 'Model',
    status: 'active',
    names: [{ name: 'Model', is_current: true, expires_at: null }],
    bindings: [
      {
        id: 'bind_1',
        provider_model_id: 'pm_1',
        provider_id: 'prv_1',
        connection_id: 'con_1',
        upstream_name: 'upstream-model',
        protocol: 'openai_chat',
        weight: 100,
        ready: true,
      },
    ],
    granted_user_ids: ['usr_1'],
  }
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const path = config.url!
    const route = `${config.method} ${path}`
    const response = {
      config,
      status: failures[route] || 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (response.status >= 400)
      throw new AxiosError('Request failed', '', config, undefined, response)
    if (route === 'get /auth/session')
      response.data = {
        user: { id: 'usr_1', name: 'User', email: 'user@example.com', role },
        csrf_token: 'csrf',
      }
    if (route === 'get /auth/permissions')
      response.data = {
        permissions:
          role === 'admin'
            ? ['providers.read', 'providers.write', 'models.read_all', 'models.write']
            : [],
      }
    if (route === 'get /keys') response.data = { items: structuredClone(keys) }
    if (route === 'get /models')
      response.data = {
        items: structuredClone(callableModels),
      }
    const memberCatalog = callableModels.map((item) => ({
      id: item.id,
      name: item.name,
      status: item.status,
      created_at: '2026-09-23T00:00:00Z',
      protocols: item.protocols ?? [item.protocol],
      input_capabilities: item.input_capabilities ?? {},
      personal_available:
        item.status === 'active' && (item.protocols ?? [item.protocol]).length > 0,
      sources: [{ type: 'personal', team_id: null, team_name: null, invocation_supported: true }],
    }))
    if (route === 'get /model-catalog') response.data = { items: memberCatalog }
    if (config.method === 'get' && path.startsWith('/model-catalog/'))
      response.data = memberCatalog.find((item) => item.id === path.split('/').pop())
    if (route === 'get /admin/egress-options') response.data = { items: [] }
    if (route === 'get /admin/providers') response.data = { items: [structuredClone(provider)] }
    if (route === 'post /admin/models') response.data = structuredClone(model)
    if (route === 'get /admin/models') response.data = { items: [structuredClone(model)] }
    if (route === 'get /admin/models/mdl_1') response.data = structuredClone(model)
    if (route === 'get /admin/model-grantees')
      response.data = {
        items: [
          { id: 'usr_1', name: 'User', email: 'user@example.com' },
          { id: 'usr_2', name: 'Second', email: 'second@example.com' },
        ],
      }
    if (route === 'post /keys') {
      keys.push(makeKey())
      response.data = { key: makeKey(), secret }
    }
    if (route === 'post /keys/key_1/rotate') {
      const replacement = {
        ...makeKey(),
        id: 'key_2',
        replaces_key_id: 'key_1',
        prefix: 'rx_replacement',
      }
      keys.push(replacement)
      response.data = { key: replacement, secret }
    }
    if (route === 'post /keys/key_2/confirm') {
      keys[1].status = 'active'
      response.data = keys[1]
    }
    if (route === 'delete /keys/key_2') keys[1].status = 'revoked'
    if (route === 'post /keys/key_1/complete-rotation') keys[0].status = 'revoked'
    if (route === 'post /keys/key_1/confirm') {
      keys[0].status = 'active'
      response.data = keys[0]
    }
    if (route === 'delete /keys/key_1') keys[0].status = 'revoked'
    if (route === 'post /admin/credentials/cre_1/verify') {
      provider.connections[0].credentials[0].verification_status = 'verified'
      response.data = { verified: true, discovered_models: 1 }
    }
    if (route === 'patch /admin/credentials/cre_1')
      provider.connections[0].credentials[0].enabled = JSON.parse(config.data).enabled
    return response
  }
})
afterEach(async () => {
  await act(async () => {
    root.unmount()
  })
  cache.clear()
  client.defaults.adapter = originalAdapter
  container.remove()
})
async function render(ui: ReactNode, path = '/') {
  await act(async () => {
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="/" element={ui} />
            <Route path="/admin/providers/:providerId" element={ui} />
            <Route path="/admin/models/:modelId" element={<AdminModelsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })
}
async function until(assert: () => void) {
  for (let i = 0; i < 60; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 59) throw error
    }
  }
}
function button(text: string) {
  const found = [...document.querySelectorAll('button')].find((b) => b.textContent === text)
  expect(found).toBeDefined()
  return found!
}
async function click(text: string) {
  await act(async () => {
    button(text).click()
  })
}
async function fill(name: string, value: string) {
  const input = document.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
  expect(input).not.toBeNull()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function submit() {
  await act(async () => {
    document
      .querySelector('[role="dialog"] form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}
async function createDelivery() {
  await render(<KeysPage />)
  await until(() => expect(document.body.textContent).toContain('No data available'))
  await click('Create key')
  await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
  await fill('name', 'Test Key')
  await act(async () => {
    document.querySelector<HTMLInputElement>('input[value="mdl_1"]')!.click()
  })
  await submit()
  await until(() => expect(document.body.textContent).toContain(secret))
}

describe('catalog and Key workflows', () => {
  it('keeps one-time secrets out of caches and activates only after delivery confirmation', async () => {
    await createDelivery()
    expect(button('Confirm and enable').disabled).toBe(true)
    expect(
      JSON.stringify(
        cache
          .getMutationCache()
          .getAll()
          .map((m) => m.state),
      ),
    ).not.toContain(secret)
    expect(
      JSON.stringify(
        cache
          .getQueryCache()
          .getAll()
          .map((q) => q.state),
      ),
    ).not.toContain(secret)
    expect(localStorage.length).toBe(0)
    await act(async () => {
      document.querySelector<HTMLInputElement>('[role="dialog"] input[type="checkbox"]')!.click()
    })
    await click('Confirm and enable')
    await until(() => expect(document.body.textContent).not.toContain(secret))
    expect(keys[0].status).toBe('active')
    expect(requests.find((r) => r.url === '/keys/key_1/confirm')?.headers.get('X-CSRF-Token')).toBe(
      'csrf',
    )
    expect(
      JSON.parse(requests.find((r) => r.method === 'post' && r.url === '/keys')!.data),
    ).toEqual({ name: 'Test Key', model_ids: ['mdl_1'], expires_at: null })
  })
  it('revokes an unconfirmed Key when the delivery dialog is dismissed', async () => {
    await createDelivery()
    await act(async () => {
      document
        .querySelector<HTMLButtonElement>('[role="dialog"] button[aria-label="Close"]')!
        .click()
    })
    await until(() => expect(document.body.textContent).not.toContain(secret))
    expect(keys[0].status).toBe('revoked')
  })
  it('retains the secret and offers retry when cancellation fails', async () => {
    await createDelivery()
    failures['delete /keys/key_1'] = 503
    await click('Cancel and revoke')
    await until(() =>
      expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull(),
    )
    expect(document.body.textContent).toContain(secret)
    delete failures['delete /keys/key_1']
    await click('Cancel and revoke')
    await until(() => expect(document.body.textContent).not.toContain(secret))
  })
  it('keeps the original Key active after confirming replacement delivery and requires explicit verified retirement', async () => {
    keys = [makeKey('active')]
    await render(<KeysPage />)
    await until(() => expect(document.body.textContent).toContain('Test Key'))
    await click('Rotate')
    await until(() => expect(document.body.textContent).toContain(secret))
    expect(document.body.textContent).toContain('Delivery confirmation does not verify a call')
    await act(async () => {
      document.querySelector<HTMLInputElement>('[role="dialog"] input[type="checkbox"]')!.click()
    })
    await click('Confirm delivery')
    await until(() => expect(document.body.textContent).not.toContain(secret))
    expect(keys.map((key) => key.status)).toEqual(['active', 'active'])
    expect(requests.some((request) => request.url?.endsWith('/complete-rotation'))).toBe(false)
    await until(() => expect(document.body.textContent).toContain('Complete rotation'))
    await click('Complete rotation')
    expect(document.body.textContent).toContain(
      'Tool handoffs, blocked or truncated responses do not count',
    )
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.body.textContent).toContain('工具交接、被拦截或被截断的响应不计入验证')
    expect(requests.some((request) => request.url?.endsWith('/complete-rotation'))).toBe(false)
    await act(async () => {
      await i18n.changeLanguage('en')
    })
    failures['post /keys/key_1/complete-rotation'] = 409
    await submit()
    await until(() =>
      expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull(),
    )
    expect(keys[0].status).toBe('active')
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    delete failures['post /keys/key_1/complete-rotation']
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(keys.map((key) => key.status)).toEqual(['revoked', 'active'])
    const completion = requests.find((request) => request.url === '/keys/key_1/complete-rotation')!
    expect(completion.headers.get('X-CSRF-Token')).toBe('csrf')
    expect(JSON.parse(completion.data)).toEqual({ replacement_key_id: 'key_2' })
    expect(
      JSON.stringify(
        cache
          .getMutationCache()
          .getAll()
          .map((mutation) => mutation.state),
      ),
    ).not.toContain(secret)
  })
  it('cancels a pending replacement without retiring its original Key', async () => {
    keys = [makeKey('active')]
    await render(<KeysPage />)
    await until(() => expect(document.body.textContent).toContain('Test Key'))
    await click('Rotate')
    await until(() => expect(document.body.textContent).toContain(secret))
    await click('Cancel and revoke')
    await until(() => expect(document.body.textContent).not.toContain(secret))
    expect(keys.map((key) => key.status)).toEqual(['active', 'revoked'])
    expect(requests.some((request) => request.url?.endsWith('/complete-rotation'))).toBe(false)
  })
  it('keeps emergency revocation available without any replacement', async () => {
    keys = [makeKey('active')]
    await render(<KeysPage />)
    await until(() => expect(document.body.textContent).toContain('Test Key'))
    expect(document.body.textContent).not.toContain('Complete rotation')
    await click('Revoke')
    await submit()
    await until(() => expect(keys[0].status).toBe('revoked'))
    expect(
      requests.some((request) => request.method === 'delete' && request.url === '/keys/key_1'),
    ).toBe(true)
  })
  it('does not query administrative data for a member', async () => {
    role = 'member'
    await render(<ProvidersPage />)
    await until(() => expect(document.body.textContent).toContain('Access denied'))
    expect(requests.filter((r) => r.url?.startsWith('/admin'))).toHaveLength(0)
  })
  it('verifies a credential before allowing a separate enable action', async () => {
    await render(<ProvidersPage />, '/admin/providers/prv_1?tab=credentials')
    await until(() => expect(document.body.textContent).toContain('Credential'))
    const openActions = async () => {
      await act(async () => {
        document.querySelector<HTMLButtonElement>('[aria-label="Actions for Credential"]')!.click()
      })
    }
    const action = (label: string) =>
      [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
        (item) => item.textContent === label,
      )!
    await openActions()
    await until(() => expect(action('Enable')).toBeDefined())
    expect(action('Enable').getAttribute('aria-disabled')).toBe('true')
    await act(async () => action('Verify').click())
    await until(() =>
      expect(provider.connections[0].credentials[0].verification_status).toBe('verified'),
    )
    await openActions()
    await until(() => expect(action('Enable').getAttribute('aria-disabled')).not.toBe('true'))
    expect(provider.connections[0].credentials[0].enabled).toBe(false)
    await act(async () => action('Enable').click())
    await until(() => expect(document.body.textContent).toContain('Enabled'))
    expect(requests.find((r) => r.method === 'patch')?.headers.get('X-CSRF-Token')).toBe('csrf')
  })
  it('keeps provider form errors recoverable and sends the agreed connection contract', async () => {
    await render(<ProvidersPage />)
    await until(() => expect(document.body.textContent).toContain('Provider'))
    await click('Add provider')
    await fill('name', 'Another Provider')
    await fill('connection_name', 'API')
    await fill('base_url', 'https://api.example.com/v1')
    await fill('credential_name', 'Primary credential')
    await fill('secret', 'upstream_secret')
    failures['post /admin/providers'] = 400
    await submit()
    await until(() =>
      expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull(),
    )
    expect(JSON.parse(requests.find((r) => r.method === 'post')!.data)).toEqual({
      name: 'Another Provider',
      connection_name: 'API',
      egress_mode: 'default',
      egress_id: null,
      base_url: 'https://api.example.com/v1',
      protocol: 'openai_chat',
      credential_name: 'Primary credential',
      secret: 'upstream_secret',
    })
    delete failures['post /admin/providers']
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await until(() =>
      expect(
        JSON.stringify(
          cache
            .getMutationCache()
            .getAll()
            .map((m) => m.state),
        ),
      ).not.toContain('upstream_secret'),
    )
  })
  it('reviews and confirms a guided Model batch while retaining its pending receipt', async () => {
    const catalogueAdapter = client.defaults.adapter
    if (typeof catalogueAdapter !== 'function') throw new Error('Catalogue adapter missing')
    const path = '/admin/connections/con_guided/model-creation'
    const connection = {
      id: 'con_guided',
      provider_id: 'prv_guided',
      provider_name: 'Guided Provider',
      name: 'Guided Connection',
      protocol: 'openai_chat',
      base_url: 'https://example.invalid/v1',
    }
    const committed: ModelCreationResult[] = []
    client.defaults.adapter = async (config) => {
      if (!config.url?.includes('model-creation')) return catalogueAdapter(config)
      requests.push(config)
      let data: unknown
      let status = 200
      if (config.url === '/admin/model-creation/connections')
        data = { items: [connection], next_cursor: null }
      else if (config.url === `${path}/provider-models`)
        data = {
          items: [
            {
              id: 'pmd_guided',
              upstream_name: 'Guided upstream',
              disabled: false,
              credential_ready: true,
              selectable: true,
              input_capabilities: [],
              blocker_codes: [],
            },
          ],
          next_cursor: null,
        }
      else if (config.url === `${path}/models`) data = { items: [], next_cursor: null }
      else if (config.url === `${path}/preview`) {
        const input = JSON.parse(config.data) as Pick<ModelCreationInput, 'items'>
        data = {
          connection,
          review_etag: 'a'.repeat(64),
          observed_at: '2026-10-04T00:00:00Z',
          can_commit: true,
          items: input.items.map((item) => ({
            ...item,
            upstream_name: 'Guided upstream',
            model_id: null,
            protocol: 'openai_chat',
            initial_weight: 100,
            blocker_codes: [],
          })),
        }
      } else if (config.url === path && config.method === 'post') {
        const input = JSON.parse(config.data) as ModelCreationInput
        const items = [
          {
            provider_model_id: 'pmd_guided',
            model_id: 'mdl_guided',
            binding_id: 'bnd_guided',
            created_model: true,
            name: 'Public-Model',
            protocol: 'openai_chat' as const,
            weight: 100 as const,
          },
        ]
        committed.push({
          receipt: {
            request_id: input.request_id,
            connection_id: connection.id,
            created_at: '2026-10-04T00:00:00Z',
            items,
          },
          committed: true,
          changed: true,
          current_items: items,
          runtime_applied: false,
          application_status: 'pending',
        })
        data = committed[0]
        status = 201
      } else if (config.url === path)
        data = { connection, can_create: true, observed_at: '2026-10-04T00:00:00Z' }
      else throw new Error(`Unexpected guided endpoint ${config.url}`)
      return { config, status, statusText: '', headers: new AxiosHeaders(), data }
    }
    const change = async (selector: string, value: string) => {
      const element = document.querySelector<
        HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement
      >(selector)!
      expect(element).not.toBeNull()
      await act(async () => {
        const prototype =
          element instanceof HTMLSelectElement
            ? HTMLSelectElement.prototype
            : element instanceof HTMLTextAreaElement
              ? HTMLTextAreaElement.prototype
              : HTMLInputElement.prototype
        Object.getOwnPropertyDescriptor(prototype, 'value')!.set!.call(element, value)
        element.dispatchEvent(
          new Event(element instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }),
        )
      })
    }
    await render(<CreateModelPage />)
    await until(() => expect(document.querySelector('option[value="con_guided"]')).not.toBeNull())
    await change('select[aria-label="Provider Connection"]', connection.id)
    await until(() =>
      expect(document.querySelector('input[aria-label="Select Guided upstream"]')).not.toBeNull(),
    )
    await act(async () =>
      document
        .querySelector<HTMLInputElement>('input[aria-label="Select Guided upstream"]')!
        .click(),
    )
    await change('input[aria-label="Public Model name for Guided upstream"]', 'Public-Model')
    await change('textarea', 'Reviewed guided creation')
    await click('Review selected models')
    await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('100')
    expect(committed).toHaveLength(0)
    expect(JSON.parse(requests.find((r) => r.url === `${path}/preview`)!.data)).toEqual({
      items: [{ provider_model_id: 'pmd_guided', target: 'new', name: 'Public-Model' }],
    })
    await click('Confirm addition')
    await until(() => expect(container.textContent).toContain('The receipt confirms'))
    const writes = requests.filter((r) => r.method === 'post' && r.url === path)
    expect(writes).toHaveLength(1)
    const input = JSON.parse(writes[0].data) as ModelCreationInput
    expect(input.items).toEqual([
      { provider_model_id: 'pmd_guided', target: 'new', name: 'Public-Model' },
    ])
    expect(input.reason).toBe('Reviewed guided creation')
    expect(input.request_id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    )
    expect(writes[0].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
    expect(writes[0].headers.get('X-CSRF-Token')).toBe('csrf')
    expect(committed[0].receipt.request_id).toBe(input.request_id)
    expect(container.textContent).toContain(input.request_id)
    expect(container.querySelector('a[href="/admin/models/mdl_guided"]')).not.toBeNull()
    expect(container.textContent).toContain('Pending runtime publication')
    expect(container.textContent).not.toContain('Save routing weights')
    expect(
      requests.some(
        (r) =>
          r.url === '/admin/providers' || r.url === '/admin/models' || r.url?.includes('grant'),
      ),
    ).toBe(false)
  })
  it('switches catalog views and opens API access in a drawer', async () => {
    await render(<ModelsPage />)
    await until(() =>
      expect(container.querySelector('[aria-label="Open API access for Model"]')).not.toBeNull(),
    )
    await click('Table')
    expect(container.querySelector('table[aria-label="Model catalogue list"]')).not.toBeNull()
    await click('API access')
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Model API access'),
    )
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('$ROUTEX_API_KEY')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('/v1/chat/completions')
  })
  it('sends all binding weights and shows server validation failures', async () => {
    await render(<AdminModelsPage />, '/admin/models/mdl_1')
    await until(() => expect(document.body.textContent).toContain('upstream-model'))
    await fill('bind_1', '50')
    failures['put /admin/models/mdl_1/weights'] = 400
    await act(async () => {
      container
        .querySelector('form[aria-label="Provider routing weights"]')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    await until(() => expect(container.querySelector('[role="alert"]')).not.toBeNull())
    expect(JSON.parse(requests.find((r) => r.method === 'put')!.data)).toEqual({
      weights: [{ binding_id: 'bind_1', weight: 50 }],
    })
  })
  it('updates explicit grants without granting every administrator implicitly', async () => {
    await render(<AdminModelsPage />, '/admin/models/mdl_1')
    await until(() => expect(document.body.textContent).toContain('upstream-model'))
    await click('Manage grants')
    await until(() => expect(document.querySelector('input[value="usr_2"]')).not.toBeNull())
    await act(async () => {
      document.querySelector<HTMLInputElement>('input[value="usr_1"]')!.click()
      document.querySelector<HTMLInputElement>('input[value="usr_2"]')!.click()
    })
    await submit()
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(JSON.parse(requests.find((r) => r.method === 'put')!.data)).toEqual({
      user_ids: ['usr_2'],
    })
  })
})

describe('native protocol catalog', () => {
  it.each(['openai_responses', 'anthropic_messages', 'gemini_generate_content'])(
    'sends selected %s when creating a provider connection',
    async (protocol) => {
      await render(<ProvidersPage />)
      await until(() => expect(document.body.textContent).toContain('Provider'))
      await click('Add provider')
      await fill('name', 'Responses provider')
      await fill('connection_name', 'Responses')
      await fill('base_url', 'https://api.example.com/v1')
      await fill('credential_name', 'Primary')
      await fill('secret', 'upstream_secret')
      await act(async () => {
        const select = document.querySelector<HTMLSelectElement>('select[name="protocol"]')!
        select.value = protocol
        select.dispatchEvent(new Event('change', { bubbles: true }))
      })
      await submit()
      await until(() =>
        expect(requests.some((r) => r.url === '/admin/providers' && r.method === 'post')).toBe(
          true,
        ),
      )
      const request = requests.find((r) => r.method === 'post')!
      expect(JSON.parse(request.data).protocol).toBe(protocol)
      expect(request.headers.get('X-CSRF-Token')).toBe('csrf')
    },
  )

  it('shows actual model protocols and switches native request examples', async () => {
    callableModels = [
      {
        id: 'mdl_1',
        name: 'Native Model',
        status: 'active',
        protocol: 'openai_chat',
        protocols: ['openai_chat', 'openai_responses'],
      },
    ]
    await render(<ModelsPage />)
    await until(() => expect(document.body.textContent).toContain('Native Model'))
    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[aria-label*="Native Model"]')!.click()
    })
    await until(() =>
      expect(document.querySelector('[role="dialog"] pre')?.textContent).toContain(
        '/chat/completions',
      ),
    )
    await act(async () => {
      const select = document.querySelector<HTMLSelectElement>(
        '[role="dialog"] select[aria-label="Protocol type"]',
      )!
      select.value = 'openai_responses'
      select.dispatchEvent(new Event('change', { bubbles: true }))
    })
    const example = document.querySelector('[role="dialog"] pre')!.textContent!
    expect(example).toContain('/v1/responses')
    expect(example).toContain('"input":"Hello"')
    expect(example).not.toContain('messages')
    expect(example).toContain('$ROUTEX_API_KEY')
  })

  it('uses Responses immediately for a Responses-only model', async () => {
    callableModels = [
      {
        id: 'mdl_1',
        name: 'Responses Model',
        status: 'active',
        protocol: 'openai_responses',
        protocols: ['openai_responses'],
      },
    ]
    await render(<ModelsPage />)
    await until(() => expect(document.body.textContent).toContain('Responses Model'))
    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[aria-label*="Responses Model"]')!.click()
    })
    await until(() =>
      expect(document.querySelector('[role="dialog"] pre')?.textContent).toContain('/v1/responses'),
    )
    expect(document.querySelector('[role="dialog"] select[aria-label="Protocol type"]')).toBeNull()
    expect(document.querySelector('[role="dialog"]')!.textContent).not.toContain('OpenAI Chat')
  })
})

it('uses native Messages authentication and parameters for a Messages-only model', async () => {
  callableModels = [
    {
      id: 'mdl_messages',
      name: 'Messages Model',
      status: 'active',
      protocol: 'anthropic_messages',
      protocols: ['anthropic_messages'],
    },
  ]
  await render(<ModelsPage />)
  await until(() => expect(document.body.textContent).toContain('Messages Model'))
  await act(async () => {
    container.querySelector<HTMLButtonElement>('button[aria-label*="Messages Model"]')!.click()
  })
  await until(() =>
    expect(document.querySelector('[role="dialog"] pre')?.textContent).toContain('/v1/messages'),
  )
  const example = document.querySelector('[role="dialog"] pre')!.textContent!
  expect(example).toContain('x-api-key: $ROUTEX_API_KEY')
  expect(example).toContain('anthropic-version: 2023-06-01')
  expect(example).toContain('"max_tokens":1024')
  expect(example).toContain('"messages":[{"role":"user","content":"Hello"}]')
  expect(example).not.toContain('Authorization:')
  expect(example).not.toContain('/chat/completions')
  expect(example).not.toContain('/responses')
  expect(document.querySelector('[role="dialog"] select[aria-label="Protocol type"]')).toBeNull()
  expect(document.querySelector('[role="dialog"]')!.textContent).toContain('Anthropic Messages')
})

it('shows Gemini native path, header and contents without synthetic model or stream fields', async () => {
  callableModels = [
    {
      id: 'mdl_gemini',
      name: 'gemini-2.5_flash',
      status: 'active',
      protocol: 'gemini_generate_content',
      protocols: ['gemini_generate_content'],
    },
  ]
  await render(<ModelsPage />)
  await until(() =>
    expect(container.querySelector('button[aria-label*="gemini-2.5_flash"]')).not.toBeNull(),
  )
  await act(async () =>
    container.querySelector<HTMLButtonElement>('button[aria-label*="gemini-2.5_flash"]')!.click(),
  )
  await until(() => expect(document.querySelector('[role="dialog"] pre')).not.toBeNull())
  const example = document.querySelector('[role="dialog"] pre')!.textContent!
  expect(example).toContain('/v1beta/models/gemini-2.5_flash:generateContent')
  expect(example).toContain('x-goog-api-key: $ROUTEX_API_KEY')
  expect(example).toContain('"contents":[{"role":"user","parts":[{"text":"Hello"}]}]')
  expect(example).not.toMatch(
    /Authorization:|"model":|"stream":|chat\/completions|\/responses|\/messages/,
  )
})

it.each(['vendor/model', 'model:latest', 'unsafe name', '-invalid', 'a'.repeat(129)])(
  'does not fabricate a callable Gemini path for %s',
  async (name) => {
    callableModels = [
      {
        id: 'mdl_gemini',
        name,
        status: 'active',
        protocol: 'gemini_generate_content',
        protocols: ['gemini_generate_content'],
      },
    ]
    await render(<ModelsPage />)
    await until(() =>
      expect(container.querySelector('button[aria-label^="Open API access"]')).not.toBeNull(),
    )
    await act(async () =>
      container.querySelector<HTMLButtonElement>('button[aria-label^="Open API access"]')!.click(),
    )
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
        'Ask a model administrator',
      ),
    )
    expect(document.querySelector('[role="dialog"] pre')).toBeNull()
    const copy = [...document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')].find(
      (button) => button.textContent === 'Copy',
    )!
    expect(copy.disabled).toBe(true)
  },
)
