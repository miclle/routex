import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { AxiosError, AxiosHeaders, CanceledError, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { PersonalModelRequestDetail } from '@/types/personal-model-requests'
import type { ModelAccessSource, ModelCatalogRecord } from '@/types/model-catalog'
import ModelsPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
const clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
const personal: ModelAccessSource = {
  type: 'personal',
  team_id: null,
  team_name: null,
  invocation_supported: true,
}
let root: Root, host: HTMLDivElement, cache: QueryClient
let models: ModelCatalogRecord[], requests: InternalAxiosRequestConfig[]
let actorID: string,
  failures: Record<string, number>,
  detailOverrides: Record<string, ModelCatalogRecord>
let pendingRequest: PersonalModelRequestDetail | null
let detailBarrier: { promise: Promise<void>; release: () => void } | undefined
const writeText = vi.fn<(value: string) => Promise<void>>()

function team(id: string, name: string): Extract<ModelAccessSource, { type: 'team' }> {
  return { type: 'team', team_id: id, team_name: name, invocation_supported: false }
}
function model(
  name: string,
  protocols = ['openai_chat'],
  sources: ModelAccessSource[] = [personal],
): ModelCatalogRecord {
  return {
    id: `mdl_${name.replace(/[^A-Za-z0-9_-]/g, '_').slice(0, 26)}`,
    name,
    status: 'active',
    created_at: '2026-09-01T10:00:00Z',
    protocols,
    input_capabilities: {},
    sources,
    input_price: { state: 'unauthorized', rate: null },
    output_price: { state: 'unauthorized', rate: null },
    personal_available:
      sources.some((source) => source.type === 'personal') && protocols.length > 0,
  }
}
function holdDetails() {
  let release!: () => void
  detailBarrier = {
    promise: new Promise<void>((resolve) => {
      release = resolve
    }),
    release: () => release(),
  }
  return detailBarrier
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  models = []
  requests = []
  failures = {}
  detailOverrides = {}
  detailBarrier = undefined
  pendingRequest = null
  actorID = 'usr_current'
  writeText.mockReset().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (
      (config.url?.startsWith('/model-catalog/') ||
        config.url?.startsWith('/model-access-candidates/')) &&
      detailBarrier
    ) {
      await new Promise<void>((resolve, reject) => {
        const abort = () => reject(new CanceledError('Cancelled model detail', config))
        config.signal?.addEventListener?.('abort', abort)
        void detailBarrier!.promise.then(() => {
          config.signal?.removeEventListener?.('abort', abort)
          resolve()
        })
      })
    }
    const status = failures[config.url!] ?? 200
    if (status !== 200)
      throw new AxiosError('rejected', '', config, undefined, {
        ...response,
        status,
        data: { code: status, message: 'sanitized failure' },
      })
    if (config.url === '/auth/session')
      response.data = {
        user: { id: actorID, name: 'Member', email: 'member@example.com', role: 'member' },
        csrf_token: 'csrf-test',
      }
    else if (config.url === '/usage') {
      const zero = {
        requests: 0,
        successes: 0,
        errors: 0,
        canceled: 0,
        success_rate: null,
        average_duration_ms: null,
        tokens: {
          input: { value: '0', known: '0', unknown_calls: 0 },
          output: { value: '0', known: '0', unknown_calls: 0 },
          total: { value: '0', known: '0', unknown_calls: 0 },
        },
        amounts: [],
        unknown_amount_calls: 0,
        pricing_statuses: {},
      }
      response.data = {
        timezone: 'UTC',
        granularity: 'month',
        queried_at: '2026-10-06T05:00:00Z',
        latest_completed_at: null,
        source: 'persisted_call_records',
        may_lag: true,
        available_dimensions: ['model', 'key'],
        current: {
          from: '2026-10-01T00:00:00Z',
          to: '2026-10-06T05:00:00Z',
          summary: zero,
          models: [],
          keys: [],
          trend: [{ start: '2026-10-01T00:00:00Z', end: '2026-11-01T00:00:00Z', stats: zero }],
        },
      }
    } else if (config.url === '/model-catalog') response.data = { items: structuredClone(models) }
    else if (config.url?.startsWith('/model-catalog/')) {
      const id = decodeURIComponent(config.url.slice('/model-catalog/'.length))
      const record = detailOverrides[id] ?? models.find((item) => item.id === id)
      if (!record) throw new Error(`Unexpected detail ${id}`)
      response.data = structuredClone(record)
    } else if (
      config.url === '/model-access-candidates' ||
      config.url?.startsWith('/model-access-candidates/')
    ) {
      const records = models.map((item) => ({
        id: item.id,
        name: item.name,
        status: item.status,
        created_at: item.created_at,
        protocols: item.protocols,
        input_capabilities: item.input_capabilities,
        personal_granted: item.sources.some((source) => source.type === 'personal'),
        pending_request_id: pendingRequest?.model_id === item.id ? pendingRequest.id : null,
        review_etag: 'a'.repeat(64),
      }))
      response.data =
        config.url === '/model-access-candidates'
          ? {
              items: records.filter((item) => item.id !== pendingRequest?.model_id),
              next_cursor: null,
            }
          : records.find(
              (item) => item.id === config.url!.slice('/model-access-candidates/'.length),
            )
    } else if (config.url === '/auth/permissions') response.data = { permissions: [] }
    else if (config.url === '/personal-model-requests' && config.method === 'post') {
      const body = JSON.parse(config.data)
      const item = models.find((item) => item.id === body.model_id)!
      pendingRequest = {
        id: 'mar_pending',
        request_id: body.request_id,
        applicant_user_id: actorID,
        applicant_name: 'Member',
        model_id: body.model_id,
        model_name: item.name,
        reason: body.reason,
        status: 'pending',
        created_at: item.created_at,
        updated_at: item.created_at,
        resolved_at: null,
        cancelled_reason: null,
        decision: null,
        current_model: { id: item.id, name: item.name, status: 'active' },
        current_granted: false,
        review_etag: 'b'.repeat(64),
        allowed_actions: ['withdraw'],
        runtime_applied: false,
        application_status: 'pending',
      }
      response.data = structuredClone(pendingRequest)
    } else if (config.url === '/personal-model-requests')
      response.data = {
        items: pendingRequest ? [structuredClone(pendingRequest)] : [],
        total: pendingRequest ? 1 : 0,
        next_cursor: null,
      }
    else if (config.url === '/personal-model-requests/mar_pending')
      response.data = structuredClone(pendingRequest)
    else throw new Error(`Unexpected request ${config.url}`)
    return response
  }
})
afterEach(async () => {
  detailBarrier?.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  if (clipboardDescriptor) Object.defineProperty(navigator, 'clipboard', clipboardDescriptor)
  else Reflect.deleteProperty(navigator, 'clipboard')
  await i18n.changeLanguage('en')
})
async function until(assert: () => void) {
  for (let attempt = 0; attempt < 100; attempt++) {
    await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
    try {
      assert()
      return
    } catch (error) {
      if (attempt === 99) throw error
    }
  }
}
async function mount(waitForModels = true) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter>
          <ModelsPage />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  if (waitForModels)
    await until(() => expect(host.querySelector('[aria-label^="Open API access"]')).not.toBeNull())
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )!
}
function drawer() {
  return document.querySelector('[role="dialog"]')!
}
async function open(name: string, waitForDetails = true) {
  await act(async () => button(`Open API access for ${name}`).click())
  await until(() => expect(drawer()).not.toBeNull())
  if (waitForDetails)
    await until(() => expect(drawer().querySelector('h2.break-words')?.textContent).toBe(name))
}
function statistic(label: string) {
  const region = host.querySelector('[aria-label="Model catalogue statistics"]')!
  const item = [...region.children].find((child) => child.querySelector('p')?.textContent === label)
  return item?.lastElementChild?.textContent
}
async function select(label: string, value: string) {
  const input = host.querySelector<HTMLSelectElement>(`select[aria-label="${label}"]`)!
  await act(async () => {
    input.value = value
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function search(value: string) {
  const input = host.querySelector<HTMLInputElement>('input[type="search"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
function visibleNames() {
  return [...host.querySelectorAll('article h2')].map((item) => item.textContent)
}

describe('Authorized member model catalogue', () => {
  it('counts personal availability and real unique sources separately from the full and filtered catalogue', async () => {
    const alpha = team('team_alpha', 'Alpha')
    models = [
      model('multi', ['openai_chat', 'openai_responses'], [personal, alpha]),
      model('messages', ['anthropic_messages']),
      model('gemini', ['gemini_generate_content']),
      model('unavailable', []),
      model('future', ['future_native']),
      model('team-only', ['openai_chat'], [alpha, team('team_beta', 'Beta')]),
    ]
    await mount()
    expect(statistic('Total models')).toBe('6')
    expect(statistic('Personally available models')).toBe('3')
    expect(statistic('Protocol type')).toBe('4')
    expect(statistic('Access sources')).toBe('3')
    await search('unavailable')
    expect(visibleNames()).toEqual(['unavailable'])
    expect(statistic('Total models')).toBe('6')
    expect(statistic('Personally available models')).toBe('3')
    expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/model-catalog'])
  })

  it('uses conjunctive literal name, exact source, protocol and explicit per-protocol input filters', async () => {
    const alpha = team('team_alpha', 'Alpha')
    const declared = model(
      'Image [.*] native',
      ['openai_chat', 'openai_responses'],
      [personal, alpha],
    )
    declared.input_capabilities = { openai_chat: ['image'], openai_responses: ['pdf'] }
    const teamImage = model('Image other', ['openai_chat'], [alpha])
    teamImage.input_capabilities = { openai_chat: ['image'] }
    models = [
      declared,
      teamImage,
      model('Image PDF by name', ['openai_chat']),
      model('no-input', ['openai_responses']),
    ]
    await mount()
    await select('Input capabilities', 'image')
    expect(visibleNames()).toEqual(['Image [.*] native', 'Image other'])
    await select('Access source', 'personal')
    expect(visibleNames()).toEqual(['Image [.*] native'])
    await select('Protocol type', 'openai_responses')
    expect(visibleNames()).toEqual([])
    await select('Input capabilities', 'pdf')
    expect(visibleNames()).toEqual(['Image [.*] native'])
    await search('[.*]')
    expect(visibleNames()).toEqual(['Image [.*] native'])
    await search('image other')
    expect(visibleNames()).toEqual([])
    expect(statistic('Total models')).toBe('4')
    expect(requests.filter((row) => row.url !== '/usage')).toHaveLength(2)
    expect(requests.filter((row) => row.url === '/usage')).toHaveLength(1)
    expect(requests.find((row) => row.url === '/usage')?.params).toEqual({
      period: 'month',
      timezone: 'UTC',
      granularity: 'month',
      compare: false,
    })
  })

  it('shows two source chips and expands all actual sources without opening API access or nesting buttons', async () => {
    models = [
      model(
        'many-sources',
        ['openai_chat'],
        [team('team_c', 'Gamma'), personal, team('team_a', 'Alpha'), team('team_b', 'Beta')],
      ),
    ]
    await mount()
    const card = host.querySelector('article')!
    expect(card.textContent).toContain('Personal grant')
    expect(card.textContent).toContain('Alpha')
    expect(card.textContent).not.toContain('Gamma')
    expect(card.querySelector('button button')).toBeNull()
    await act(async () => button('View 2 more access sources for many-sources').click())
    await until(() => expect(document.querySelector('[role="menu"]')).not.toBeNull())
    const popup = document.querySelector('[role="menu"]')!
    for (const name of ['Personal grant', 'Alpha', 'Beta', 'Gamma'])
      expect(popup.textContent).toContain(name)
    expect(popup.querySelectorAll('[role="menuitem"]')).toHaveLength(4)
    expect(popup.textContent).toContain('Team invocation is not supported')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.querySelector('a[href="/keys"]')).toBeNull()
    expect(requests).toHaveLength(2)
    await act(async () =>
      popup.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    await until(() => expect(document.querySelector('[role="menu"]')).toBeNull())
  })

  it.each(['openai_chat', 'openai_responses', 'anthropic_messages', 'gemini_generate_content'])(
    'opens only a freshly authorized named Team %s source without offering a personal Key',
    async (protocol) => {
      models = [
        model(
          'Team native model',
          [protocol],
          [{ ...team('tea_live', 'Live Team'), invocation_protocols: [protocol] }],
        ),
      ]
      await mount()
      await open('Team native model')
      const link = drawer().querySelector<HTMLAnchorElement>('a[href^="/playground?"]')!
      expect(link.getAttribute('href')).toBe(
        `/playground?team=tea_live&model=${encodeURIComponent(models[0].id)}`,
      )
      expect(link.textContent).toContain('Live Team')
      expect(drawer().textContent).toContain(
        'native text conversation through the named Team Session',
      )
      expect(drawer().textContent).not.toContain('Team inference are not available')
      expect(drawer().querySelector('a[href="/keys"]')).toBeNull()
      expect(button('Copy').disabled).toBe(protocol === 'gemini_generate_content')
      await act(async () => i18n.changeLanguage('zh'))
      expect(link.textContent).toContain('打开 Live Team 对话')
    },
  )

  it('uses the independent ready Team protocol subset instead of borrowing global or Personal route availability', async () => {
    models = [
      model(
        'independent-team',
        ['openai_chat'],
        [
          {
            ...team('tea_ready', 'Ready Team'),
            invocation_protocols: [
              'openai_responses',
              'anthropic_messages',
              'gemini_generate_content',
            ],
          },
          { ...team('tea_unready', 'Unready Team'), invocation_protocols: [] },
          { ...team('tea_future', 'Future Team'), invocation_protocols: ['future_native'] },
        ],
      ),
    ]
    await mount()
    await open('independent-team')
    expect(drawer().querySelector('a[href^="/playground?team=tea_ready"]')).not.toBeNull()
    expect(drawer().querySelector('a[href^="/playground?team=tea_unready"]')).toBeNull()
    expect(drawer().querySelector('a[href^="/playground?team=tea_future"]')).toBeNull()
    expect(drawer().querySelectorAll('a[href^="/playground?team="]')).toHaveLength(1)
    expect(drawer().querySelector('pre')).toBeNull()
    expect(drawer().querySelector('a[href="/keys"]')).toBeNull()
    expect(drawer().textContent).toContain('A personal Key cannot use this Team grant')
    expect(button('Copy').disabled).toBe(true)
  })

  it('hides a native Team link during detail renewal and removes it after its ready protocol is revoked', async () => {
    const item = model(
      'renew-team-native',
      ['gemini_generate_content'],
      [{ ...team('tea_ready', 'Ready Team'), invocation_protocols: ['gemini_generate_content'] }],
    )
    models = [item]
    await mount()
    await open(item.name)
    expect(drawer().querySelector('a[href^="/playground?team=tea_ready"]')).not.toBeNull()
    const barrier = holdDetails()
    await act(async () => button('Refresh details').click())
    await until(() =>
      expect(drawer().querySelector('a[href^="/playground?team=tea_ready"]')).toBeNull(),
    )
    detailOverrides[item.id] = {
      ...item,
      sources: [{ ...team('tea_ready', 'Ready Team'), invocation_protocols: [] }],
    }
    await act(async () => barrier.release())
    await until(() =>
      expect(drawer().textContent).toContain('no Team native protocol is currently ready'),
    )
    expect(drawer().querySelector('a[href^="/playground?team="]')).toBeNull()
    expect(drawer().querySelector('a[href="/keys"]')).toBeNull()
    expect(button('Copy').disabled).toBe(true)
  })

  it('preserves the table composition with actual creation dates and explicit unknown price and usage fields', async () => {
    const item = model('table-model', ['openai_chat'], [personal, team('team_a', 'Alpha')])
    item.input_capabilities = { openai_chat: ['image', 'pdf'] }
    models = [item]
    await mount()
    await act(async () => button('Table').click())
    const table = host.querySelector('table')!
    expect(table.getAttribute('aria-label')).toBe('Model catalogue list')
    expect(table.textContent).toContain('Alpha')
    expect(table.textContent).toContain('Image input · PDF input')
    expect(table.textContent).toContain('2026')
    expect(table.textContent).toContain('Unknown')
    expect(host.textContent).toContain('Price cells show only server-confirmed current base rates')
    await act(async () => button('API access').click())
    await until(() =>
      expect(drawer().querySelector('select[aria-label="Example access source"]')).not.toBeNull(),
    )
    await act(async () => {
      const choice = drawer().querySelector<HTMLSelectElement>(
        'select[aria-label="Example access source"]',
      )!
      choice.value = 'personal'
      choice.dispatchEvent(new Event('change', { bubbles: true }))
    })
    await until(() =>
      expect(drawer().querySelector('pre')?.textContent).toContain('/v1/chat/completions'),
    )
    expect(requests.every((request) => request.method === 'get')).toBe(true)
  })

  it.each([
    ['explicit empty protocols', model('unavailable', [])],
    ['unknown protocols', model('future', ['future_native'])],
  ])('never fabricates an example for %s', async (_, item) => {
    models = [item]
    await mount()
    await open(item.name)
    expect(statistic('Personally available models')).toBe('0')
    expect(drawer().textContent).toContain('No supported inference protocol is currently available')
    expect(drawer().querySelector('pre')).toBeNull()
    expect(drawer().textContent).not.toContain('curl')
    expect(drawer().querySelector('a[href="/keys"]')).toBeNull()
    expect(button('Copy').disabled).toBe(true)
    await act(async () => button('Copy').click())
    expect(writeText).not.toHaveBeenCalled()
  })

  it('shows Team-only metadata without treating visibility as personal invocation', async () => {
    models = [model('team-visible', ['openai_chat'], [team('team_a', 'Alpha')])]
    await mount()
    await open('team-visible')
    expect(statistic('Total models')).toBe('1')
    expect(statistic('Personally available models')).toBe('0')
    expect(drawer().textContent).toContain('Alpha')
    expect(drawer().textContent).toContain('A personal Key cannot use this Team grant')
    expect(drawer().querySelector('pre')).toBeNull()
    expect(drawer().querySelector('a[href="/keys"]')).toBeNull()
    expect(button('Copy').disabled).toBe(true)
    expect(host.textContent).not.toContain('Personal grant')
  })

  it('requires both current personal availability and a supported personal source', async () => {
    const item = model('inconsistent', ['openai_chat'], [team('team_a', 'Alpha')])
    item.personal_available = true
    models = [item]
    await mount()
    await open(item.name)
    expect(statistic('Personally available models')).toBe('0')
    expect(button('Copy').disabled).toBe(true)
    expect(drawer().querySelector('a[href="/keys"]')).toBeNull()
  })

  it.each([
    ['openai_chat', '/v1/chat/completions', 'Authorization: Bearer $ROUTEX_API_KEY', '"messages"'],
    [
      'openai_responses',
      '/v1/responses',
      'Authorization: Bearer $ROUTEX_API_KEY',
      '"input":"Hello"',
    ],
    ['anthropic_messages', '/v1/messages', 'x-api-key: $ROUTEX_API_KEY', '"max_tokens":1024'],
    [
      'gemini_generate_content',
      '/v1beta/models/native-model:generateContent',
      'x-goog-api-key: $ROUTEX_API_KEY',
      '"contents"',
    ],
  ])(
    'preserves the %s native example after a fresh resource read',
    async (protocol, path, header, body) => {
      models = [model('native-model', [protocol])]
      await mount()
      await open('native-model')
      const example = drawer().querySelector('pre')!.textContent!
      expect(example).toContain(`curl ${window.location.origin}${path}`)
      expect(example).toContain(header)
      expect(example).toContain(body)
      if (protocol === 'anthropic_messages')
        expect(example).toContain('anthropic-version: 2023-06-01')
      expect(button('Copy').disabled).toBe(false)
      expect(drawer().querySelector('a[href="/keys"]')).not.toBeNull()
      await act(async () => button('Copy').click())
      expect(writeText).toHaveBeenCalledExactlyOnceWith(example)
      expect(drawer().textContent).toContain('Copied.')
      expect(requests.map((request) => request.url)).toEqual([
        '/auth/session',
        '/model-catalog',
        '/model-catalog/mdl_native-model',
      ])
      expect(
        cache
          .getQueriesData({ queryKey: ['model-catalog', 'detail', actorID, models[0].id] })
          .find(([, data]) => !!data)?.[1],
      ).toEqual(models[0])
    },
  )

  it('filters unknown and duplicate protocol options and changes native requests explicitly', async () => {
    models = [model('mixed', ['future_native', 'openai_responses', 'openai_chat', 'openai_chat'])]
    await mount()
    await open('mixed')
    const input = drawer().querySelector<HTMLSelectElement>('select[aria-label="Protocol type"]')!
    expect([...input.options].map((option) => option.value)).toEqual([
      'openai_responses',
      'openai_chat',
    ])
    await act(async () => {
      input.value = 'openai_responses'
      input.dispatchEvent(new Event('change', { bubbles: true }))
    })
    expect(drawer().querySelector('pre')?.textContent).toContain('/v1/responses')
    expect(drawer().textContent).not.toContain('future_native')
  })

  it('preserves the Gemini public-name guard and blocks Copy and Key links for unsafe aliases', async () => {
    models = [model('unsafe/name', ['gemini_generate_content'])]
    await mount()
    await open('unsafe/name')
    expect(drawer().textContent).toContain('Gemini requires a public name or active alias')
    expect(drawer().querySelector('pre')).toBeNull()
    expect(button('Copy').disabled).toBe(true)
    expect(drawer().querySelector('a[href="/keys"]')).toBeNull()
    expect(writeText).not.toHaveBeenCalled()
  })

  it('shell-quotes apostrophes in native model names without changing JSON content', async () => {
    models = [model("model'name", ['openai_chat'])]
    await mount()
    await open("model'name")
    expect(drawer().querySelector('pre')?.textContent).toContain("model'\\''name")
  })

  it.each([403, 404, 503])(
    'hides saved detail during refresh and after a %s rejection',
    async (status) => {
      models = [model('current-model')]
      await mount()
      await open('current-model')
      const barrier = holdDetails()
      failures['/model-catalog/mdl_current-model'] = status
      await act(async () => {
        button('Refresh details').click()
      })
      await until(() => expect(drawer().textContent).toContain('Checking current model access'))
      expect(drawer().querySelector('h2.break-words')).toBeNull()
      expect(drawer().querySelector('pre')).toBeNull()
      expect(drawer().querySelector('a[href="/keys"]')).toBeNull()
      await act(async () => barrier.release())
      await until(() => expect(drawer().querySelector('[role="alert"]')).not.toBeNull())
      expect(drawer().querySelector('pre')).toBeNull()
      expect(drawer().textContent).not.toContain('$ROUTEX_API_KEY')
      expect(drawer().textContent).not.toContain('sanitized failure')
    },
  )

  it('hides preseeded detail until a fresh resource-authorized request completes and uses that response', async () => {
    const item = model('cached-model')
    models = [item]
    cache.setQueryData(['model-catalog', 'detail', actorID, item.id], item)
    detailOverrides[item.id] = {
      ...item,
      personal_available: false,
      sources: [team('team_a', 'Alpha')],
    }
    const barrier = holdDetails()
    await mount()
    await open(item.name, false)
    expect(drawer().querySelector('pre')).toBeNull()
    expect(drawer().querySelector('a[href="/keys"]')).toBeNull()
    await act(async () => barrier.release())
    await until(() => expect(drawer().querySelector('h2.break-words')?.textContent).toBe(item.name))
    expect(drawer().textContent).toContain('A personal Key cannot use this Team grant')
    expect(drawer().querySelector('pre')).toBeNull()
    expect(button('Copy').disabled).toBe(true)
  })

  it('rejects a detail response for a different resource without showing its metadata or example', async () => {
    const item = model('expected')
    models = [item]
    detailOverrides[item.id] = model('wrong-resource')
    await mount()
    await open(item.name, false)
    await until(() => expect(drawer().querySelector('[role="alert"]')).not.toBeNull())
    expect(drawer().textContent).not.toContain('wrong-resource')
    expect(drawer().querySelector('pre')).toBeNull()
    expect(drawer().querySelector('a[href="/keys"]')).toBeNull()
  })

  it('requires fresh detail again after closing and reopening the same model', async () => {
    models = [model('reopened')]
    await mount()
    await open('reopened')
    await act(async () => button('Close').click())
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    models = [{ ...models[0], personal_available: false }]
    await open('reopened')
    expect(drawer().querySelector('pre')).toBeNull()
    expect(button('Copy').disabled).toBe(true)
    expect(
      requests.filter((request) => request.url === '/model-catalog/mdl_reopened'),
    ).toHaveLength(2)
  })

  it('rechecks selected detail when the catalogue is explicitly refreshed', async () => {
    const item = model('refreshed-list')
    models = [item]
    await mount()
    await open(item.name)
    const current = { ...item, personal_available: false, sources: [team('team_a', 'Alpha')] }
    models = [current]
    detailOverrides[item.id] = current
    const beforeRefresh = requests.filter(
      (request) => request.url === '/model-catalog/mdl_refreshed-list',
    ).length
    const beforeSession = requests.filter((request) => request.url === '/auth/session').length
    await act(async () => button('Refresh catalogue').click())
    await until(() =>
      expect(drawer().textContent).toContain('A personal Key cannot use this Team grant'),
    )
    expect(drawer().querySelector('pre')).toBeNull()
    expect(drawer().querySelector('a[href="/keys"]')).toBeNull()
    expect(statistic('Personally available models')).toBe('0')
    expect(
      requests.filter((request) => request.url === '/model-catalog/mdl_refreshed-list'),
    ).toHaveLength(
      beforeRefresh +
        1 +
        requests.filter((request) => request.url === '/auth/session').length -
        beforeSession,
    )
    expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(
      beforeSession + 1,
    )
  })

  it('cancels an abandoned resource request and keeps the next model independent', async () => {
    models = [model('first'), model('second', ['openai_responses'])]
    const barrier = holdDetails()
    await mount()
    await open('first', false)
    const firstRequest = requests.find((request) => request.url === '/model-catalog/mdl_first')!
    await act(async () => button('Close').click())
    await until(() => expect(firstRequest.signal?.aborted).toBe(true))
    detailBarrier = undefined
    await open('second')
    expect(drawer().querySelector('pre')?.textContent).toContain('/v1/responses')
    expect(drawer().textContent).not.toContain('first')
    barrier.release()
  })

  it('keys lists and details by the current actor and removes prior content after session rejection', async () => {
    models = [model('actor-model')]
    await mount()
    await open('actor-model')
    expect(
      cache
        .getQueriesData({ queryKey: ['model-catalog', 'list', 'usr_current'] })
        .find(([, data]) => !!data)?.[1],
    ).toEqual(models)
    actorID = 'usr_other'
    models = [{ ...models[0], personal_available: false }]
    await act(async () => cache.invalidateQueries({ queryKey: ['auth', 'session'] }))
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    await until(() => expect(visibleNames()).toEqual(['actor-model']))
    expect(
      requests.filter((request) => request.url === '/model-catalog/mdl_actor-model'),
    ).toHaveLength(1)
    await open('actor-model')
    await until(() =>
      expect(
        cache
          .getQueriesData({ queryKey: ['model-catalog', 'detail', 'usr_other', models[0].id] })
          .find(([, data]) => !!data)?.[1],
      ).toEqual(models[0]),
    )
    await until(() => expect(button('Copy')?.disabled).toBe(true))
    expect(drawer().querySelector('pre')).toBeNull()
    expect(button('Copy').disabled).toBe(true)
    failures['/auth/session'] = 401
    await act(async () => cache.invalidateQueries({ queryKey: ['auth', 'session'] }))
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(host.querySelector('article')).toBeNull()
    expect(cache.getQueryData(['auth', 'session'])).toBeNull()
  })

  it.each(['list', 'detail'])(
    'shows a localized explicit overflow on %s without partial rows or cached examples',
    async (scope) => {
      models = [model('overflow')]
      if (scope === 'list') {
        failures['/model-catalog'] = 422
        await mount(false)
        await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
      } else {
        await mount()
        failures['/model-catalog/mdl_overflow'] = 422
        await open('overflow', false)
        await until(() => expect(drawer().querySelector('[role="alert"]')).not.toBeNull())
      }
      expect(document.body.textContent).toContain('The server did not return a partial catalogue')
      expect(document.querySelector('pre')).toBeNull()
      if (scope === 'list') expect(host.querySelector('article')).toBeNull()
      await act(async () => i18n.changeLanguage('zh'))
      expect(document.body.textContent).toContain('服务器未返回不完整的目录')
    },
  )

  it('starts in English and switches filters, Team guidance and detail copy live', async () => {
    models = [model('team-language', ['openai_chat'], [team('team_a', 'Alpha')])]
    await mount()
    await open('team-language')
    expect(drawer().textContent).toContain('A personal Key cannot use this Team grant')
    await act(async () => i18n.changeLanguage('zh'))
    expect(drawer().textContent).toContain('team-language API 接入')
    expect(drawer().textContent).toContain('个人 Key 不能使用此 Team 授权')
    expect(button('复制').disabled).toBe(true)
    expect(drawer().querySelector('pre')).toBeNull()
    expect(host.querySelector('select[aria-label="输入能力"]')).not.toBeNull()
    expect(host.textContent).toContain('个人可接入模型')
    await until(() =>
      expect(drawer().querySelector('[aria-label="申请个人访问权限"]')).not.toBeNull(),
    )
    expect(requests.some((request) => request.url?.startsWith('/model-access-candidates/'))).toBe(
      true,
    )
  })

  it('retains scoped own history access after every visible model has a Personal grant', async () => {
    models = [model('personal')]
    await mount()
    await act(async () => button('My model requests').click())
    await until(() => expect(drawer().textContent).toContain('No recorded model requests.'))
    expect(requests.some((request) => request.url === '/personal-model-requests')).toBe(true)
    expect(requests.some((request) => request.url?.startsWith('/admin/'))).toBe(false)
    expect(requests.some((request) => request.url?.startsWith('/model-access-candidates'))).toBe(
      false,
    )
    expect(requests.every((request) => request.method === 'get')).toBe(true)
  })

  it('discovers request candidates only after the explicit source filter and never exposes personal examples', async () => {
    models = [model('personal'), model('candidate', ['openai_chat'], [])]
    await mount()
    expect(requests.some((request) => request.url === '/model-access-candidates')).toBe(false)
    await select('Access source', 'requestable')
    await until(() => expect(visibleNames()).toEqual(['candidate']))
    expect(statistic('Total models')).toBe('—')
    const beforeCandidate = requests.filter(
      (request) => request.url === '/model-access-candidates',
    ).length
    const beforeSession = requests.filter((request) => request.url === '/auth/session').length
    await open('candidate')
    await until(() => expect(button('Submit request')).toBeDefined())
    expect(drawer().querySelector('pre')).toBeNull()
    expect(button('Copy').disabled).toBe(true)
    expect(drawer().querySelector('a[href="/keys"]')).toBeNull()
    expect(requests.some((request) => request.url === '/model-catalog/mdl_candidate')).toBe(false)
    expect(requests.filter((request) => request.url === '/model-access-candidates')).toHaveLength(
      beforeCandidate + 1,
    )
    expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(
      beforeSession + 1,
    )
  })

  it('retains independently authorized pending details when a submitted model disappears from discovery', async () => {
    models = [model('pending-candidate', ['openai_chat'], [])]
    await mount()
    await select('Access source', 'requestable')
    await until(() => expect(visibleNames()).toEqual(['pending-candidate']))
    await open('pending-candidate')
    await until(() => expect(button('Submit request')).toBeDefined())
    const reason = drawer().querySelector('textarea')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
        reason,
        'Pending research need',
      )
      reason.dispatchEvent(new Event('input', { bubbles: true }))
    })
    const invalidated: (readonly unknown[])[] = []
    const subscribe = cache.getQueryCache().subscribe((event) => {
      if (event.type === 'updated' && event.action.type === 'invalidate')
        invalidated.push(event.query.queryKey)
    })
    const otherActorKey = ['personal-model-candidate-drawer', 'usr_other', models[0].id, 99]
    const otherModelKey = ['personal-model-candidate-drawer', actorID, 'mdl_other', 99]
    cache.setQueryData(otherActorKey, { private: 'other actor' })
    cache.setQueryData(otherModelKey, { private: 'other Model' })
    const beforeDetail = requests.filter(
      (request) => request.url === '/model-access-candidates/mdl_pending-candidate',
    ).length
    await act(async () => button('Submit request').click())
    await until(() => {
      expect(visibleNames()).toEqual([])
      expect(drawer().querySelector('h2.break-words')?.textContent).toBe('pending-candidate')
      expect(drawer().textContent).toContain('Personal access requested')
      expect(drawer().textContent).toContain('Pending research need')
      expect(drawer().querySelector('table')?.textContent).toContain('Pending')
    })
    subscribe()
    expect(
      invalidated.some(
        (key) =>
          key[0] === 'personal-model-candidate-drawer' &&
          key[1] === actorID &&
          key[2] === models[0].id &&
          typeof key[3] === 'number',
      ),
    ).toBe(true)
    expect(invalidated.some((key) => key[1] === 'usr_other' || key[2] === 'mdl_other')).toBe(false)
    expect(cache.getQueryState(otherActorKey)?.isInvalidated).toBe(false)
    expect(cache.getQueryState(otherModelKey)?.isInvalidated).toBe(false)
    expect(
      requests.filter((request) => request.url === '/model-access-candidates/mdl_pending-candidate')
        .length,
    ).toBeGreaterThan(beforeDetail)
    expect(drawer().textContent).not.toContain('other actor')
    expect(drawer().textContent).not.toContain('other Model')
    const sessionReads = requests.filter((request) => request.url === '/auth/session').length
    await act(async () => new Promise((resolve) => setTimeout(resolve, 50)))
    expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(sessionReads)
    expect(drawer().querySelector('h2.break-words')?.textContent).toBe('pending-candidate')
    expect(drawer().querySelector('pre')).toBeNull()
    expect(requests.filter((request) => request.method === 'post')).toHaveLength(1)
    expect(
      requests.filter((request) => request.url === '/model-access-candidates/mdl_pending-candidate')
        .length,
    ).toBeGreaterThanOrEqual(4)
    await act(async () => button('Request details').click())
    await until(() =>
      expect([...document.querySelectorAll('[role="dialog"]')].at(-1)?.textContent).toContain(
        'Pending research need',
      ),
    )
    expect(requests.some((request) => request.url === '/personal-model-requests/mar_pending')).toBe(
      true,
    )
  })

  it('offers Personal requests for a Team-only model while preserving named Team Chat access', async () => {
    models = [
      model(
        'team-request',
        ['openai_chat'],
        [{ ...team('tea_live', 'Live'), invocation_protocols: ['openai_chat'] }],
      ),
    ]
    await mount()
    await open('team-request')
    await until(() => expect(button('Submit request')).toBeDefined())
    expect(drawer().querySelector('a[href^="/playground?team=tea_live"]')).not.toBeNull()
    expect(drawer().querySelector('pre')?.textContent).toContain(
      '/api/v1/teams/tea_live/chat/completions',
    )
    expect(drawer().querySelector('pre')?.textContent).not.toContain('ROUTEX_API_KEY')
    expect(requests.every((request) => request.method === 'get')).toBe(true)
  })
  it('retains the original unknown Personal request through Session generation renewal and rejected retries without automatic replay', async () => {
    models = [model('uncertain-request', ['openai_chat'], [])]
    await mount()
    await select('Access source', 'requestable')
    await until(() => expect(visibleNames()).toEqual(['uncertain-request']))
    await open('uncertain-request')
    await until(() => expect(button('Submit request')).toBeDefined())
    await act(async () => {
      const reason = drawer().querySelector('textarea')!
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
        reason,
        'Captured exact request reason',
      )
      reason.dispatchEvent(new Event('input', { bubbles: true }))
    })
    failures['/personal-model-requests'] = 500
    await act(async () => button('Submit request').click())
    await until(() => expect(button('Retry original request')).toBeDefined())
    const original = requests.find((request) => request.method === 'post')!
    const reviewed = original.headers.get('If-Match'),
      body = original.data
    const hold = holdDetails()
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['auth', 'session'] })
    })
    await until(() => expect(drawer().querySelector('h2.break-words')).toBeNull())
    expect(requests.filter((request) => request.method === 'post')).toHaveLength(1)
    expect(button('Retry original request').disabled).toBe(true)
    expect(drawer().textContent).toContain('The request may already be saved')
    detailBarrier = undefined
    await act(async () => hold.release())
    await until(() => expect(button('Retry original request').disabled).toBe(false))
    failures['/personal-model-requests'] = 409
    await act(async () => button('Retry original request').click())
    await until(() => expect(button('Retry original request').disabled).toBe(false))
    const rejected = requests.filter((request) => request.method === 'post').at(-1)!
    expect(rejected.data).toBe(body)
    expect(rejected.headers.get('If-Match')).toBe(reviewed)
    expect(drawer().textContent).toContain('The request may already be saved')
    failures['/model-access-candidates/mdl_uncertain-request'] = 403
    await act(async () => button('Refresh details').click())
    await until(() => expect(drawer().querySelector('h2.break-words')).toBeNull())
    expect(button('Retry original request').disabled).toBe(true)
    expect(requests.filter((request) => request.method === 'post')).toHaveLength(2)
    expect(drawer().textContent).toContain('The request may already be saved')
    actorID = 'usr_other'
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['auth', 'session'] })
    })
    await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
    expect(requests.filter((request) => request.method === 'post')).toHaveLength(2)
  })
})

it('renders exact catalogue price facts in existing cards and table, retaining filters and live language', async () => {
  const priced = model('priced-model')
  priced.input_price = {
    state: 'priced',
    rate: { amount: '999999999999999999.123456789012345678', unit: '1M_TOKEN', currency: 'USD' },
  }
  priced.output_price = {
    state: 'disabled',
    rate: { amount: '0', unit: '1M_TOKEN', currency: 'CNY' },
  }
  const missing = model('missing-model')
  missing.input_price = { state: 'missing', rate: null }
  missing.output_price = { state: 'heterogeneous', rate: null }
  models = [priced, missing]
  await mount()
  expect(host.textContent).toContain('999999999999999999.123456789012345678 USD / 1M Tokens')
  expect(host.textContent).toContain('0 CNY / 1M Tokens')
  expect(host.textContent).toContain('Disabled')
  expect(host.textContent).toContain('Not configured')
  expect(host.textContent).toContain('Multiple schedules')
  expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/model-catalog'])
  await search('priced')
  await act(async () => button('Table').click())
  expect(host.querySelector('table')?.textContent).toContain(
    '999999999999999999.123456789012345678 USD / 1M Tokens',
  )
  expect(host.querySelector('table')?.textContent).not.toContain('missing-model')
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('999999999999999999.123456789012345678 USD / 百万 Tokens')
  expect(host.textContent).toContain('已停用')
  expect(host.querySelector<HTMLInputElement>('input[type="search"]')?.value).toBe('priced')
  expect(requests.map((request) => request.url)).toEqual(['/auth/session', '/model-catalog'])
})

it('hides prior private price facts while Session renews and after current price authority is lost', async () => {
  const prior = model('price-private')
  prior.input_price = {
    state: 'priced',
    rate: { amount: '17.123456789012345678', unit: '1M_TOKEN', currency: 'USD' },
  }
  models = [prior]
  await mount()
  expect(host.textContent).toContain('17.123456789012345678')
  let release!: () => void
  const barrier = new Promise<void>((resolve) => {
    release = resolve
  })
  const adapter = client.defaults.adapter
  client.defaults.adapter = async (config) => {
    if (config.url === '/auth/session') await barrier
    return typeof adapter === 'function'
      ? adapter(config)
      : Promise.reject(new Error('adapter missing'))
  }
  models = [{ ...prior, input_price: { state: 'unauthorized', rate: null } }]
  let renewed!: Promise<void>
  await act(async () => {
    renewed = cache.invalidateQueries({ queryKey: ['auth', 'session'] })
  })
  await until(() => expect(host.textContent).not.toContain('17.123456789012345678'))
  expect(cache.getQueryState(['auth', 'session'])?.fetchStatus).toBe('fetching')
  await act(async () => {
    release()
    await renewed
  })
  await until(() => expect(visibleNames()).toEqual(['price-private']))
  expect(host.textContent).not.toContain('17.123456789012345678')
  expect(host.querySelector('article')?.textContent).toContain('Unknown')
  expect(
    requests.some(
      (request) => request.url?.startsWith('/admin/') || request.url?.includes('/price'),
    ),
  ).toBe(false)
})

describe('Whole-item catalogue detail activation', () => {
  async function surface(view: 'card' | 'table', name: string) {
    if (view === 'table') await act(async () => button('Table').click())
    return (
      view === 'card'
        ? [...host.querySelectorAll<HTMLElement>('article')].find(
            (item) => item.querySelector('h2')?.textContent === name,
          )
        : [...host.querySelectorAll<HTMLElement>('tbody tr')].find(
            (item) => item.querySelector('td')?.textContent === name,
          )
    )!
  }
  const detailCalls = (id: string, requestable = false) =>
    requests.filter(
      (request) =>
        request.url === `${requestable ? '/model-access-candidates/' : '/model-catalog/'}${id}`,
    )

  it.each(['card', 'table'] as const)(
    'opens the exact %s target from passive content and preserves controls',
    async (view) => {
      models = [model('other'), model('whole-target')]
      await mount()
      const item = await surface(view, models[1].name)
      expect(item.getAttribute('tabindex')).toBe('0')
      expect(item.getAttribute('aria-label')).toBe('Open API access for whole-target')
      expect(item.getAttribute('aria-haspopup')).toBe('dialog')
      expect(item.querySelector('button button')).toBeNull()
      const passive = item.querySelector<HTMLElement>(view === 'card' ? 'dd' : 'td:nth-child(5)')!
      await act(async () => passive.click())
      await until(() =>
        expect(drawer()?.querySelector('h2.break-words')?.textContent).toBe('whole-target'),
      )
      expect(detailCalls(models[1].id)).toHaveLength(1)
      expect(detailCalls(models[0].id)).toHaveLength(0)
    },
  )

  it.each([
    ['card', 'Enter'],
    ['card', ' '],
    ['table', 'Enter'],
    ['table', ' '],
  ] as const)(
    'activates focused %s with %s without scrolling or duplicate reads',
    async (view, key) => {
      models = [model('keyboard-target')]
      await mount()
      const item = await surface(view, models[0].name)
      item.focus()
      expect(document.activeElement).toBe(item)
      const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
      await act(async () => item.dispatchEvent(event))
      expect(event.defaultPrevented).toBe(true)
      await until(() =>
        expect(drawer()?.querySelector('h2.break-words')?.textContent).toBe(models[0].name),
      )
      expect(detailCalls(models[0].id)).toHaveLength(1)
      await act(async () => button('Close').click())
      await until(() => expect(drawer()).toBeNull())
      await until(() => expect(document.activeElement).toBe(item))
    },
  )

  it.each(['card', 'table'] as const)(
    'opens Team-only and requestable %s items through their own authorized endpoint',
    async (view) => {
      models = [
        model('Team-only', ['openai_chat'], [team('tea_owned', 'Owned Team')]),
        model('requestable', ['openai_chat'], []),
      ]
      await mount()
      const baselineItem = await surface(view, models[0].name)
      const baselineButton =
        view === 'card'
          ? button('Open API access for Team-only')
          : baselineItem.querySelector<HTMLButtonElement>('td:last-child button')!
      await act(async () => baselineButton.click())
      await until(() =>
        expect(drawer()?.textContent).toContain('A personal Key cannot use this Team grant'),
      )
      const baselineReads = detailCalls(models[0].id).length
      await act(async () => button('Close').click())
      await until(() => expect(drawer()).toBeNull())
      const teamItem = await surface(view, models[0].name)
      await act(async () => teamItem.click())
      await until(() =>
        expect(drawer()?.textContent).toContain('A personal Key cannot use this Team grant'),
      )
      // Existing request footer renews Session; whole-item activation preserves the native-button lifecycle.
      expect(detailCalls(models[0].id).length - baselineReads).toBe(baselineReads)
      expect(drawer().querySelector('a[href="/keys"]')).toBeNull()
      await act(async () => button('Close').click())
      await until(() => expect(drawer()).toBeNull())
      await select('Access source', 'requestable')
      await until(() => expect(host.textContent).toContain('requestable'))
      const baselineCandidate = await surface(view, models[1].name)
      const candidateButton =
        view === 'card'
          ? button('Open API access for requestable')
          : baselineCandidate.querySelector<HTMLButtonElement>('td:last-child button')!
      await act(async () => candidateButton.click())
      await until(() =>
        expect(drawer()?.querySelector('h2.break-words')?.textContent).toBe('requestable'),
      )
      await until(() => expect(button('Submit request')).toBeDefined())
      const baselineCandidateReads = detailCalls(models[1].id, true).length
      await act(async () => button('Close').click())
      await until(() => expect(drawer()).toBeNull())
      const candidate = await surface(view, models[1].name)
      candidate.focus()
      await act(async () =>
        candidate.dispatchEvent(
          new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }),
        ),
      )
      await until(() =>
        expect(drawer()?.querySelector('h2.break-words')?.textContent).toBe('requestable'),
      )
      await until(() => expect(button('Submit request')).toBeDefined())
      expect(detailCalls(models[1].id, true).length - baselineCandidateReads).toBe(
        baselineCandidateReads,
      )
      expect(detailCalls(models[1].id)).toHaveLength(0)
      expect(button('Submit request')).toBeDefined()
      expect(requests.some((request) => request.method !== 'get')).toBe(false)
    },
  )

  it.each(['card', 'table'] as const)(
    'isolates nested %s source menus and existing API controls',
    async (view) => {
      models = [
        model(
          'nested-menu',
          ['openai_chat'],
          [personal, team('tea_a', 'Alpha'), team('tea_b', 'Beta')],
        ),
      ]
      await mount()
      const item = await surface(view, models[0].name)
      const trigger = button('View 1 more access source for nested-menu')
      const beforeMenu = requests.length
      const closeMenu = async () => {
        const popup = document.querySelector<HTMLElement>('[role="menu"]')
        if (popup) {
          await act(async () =>
            popup.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
          )
          await until(() => expect(document.querySelector('[role="menu"]')).toBeNull())
        }
      }
      await act(async () => trigger.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })))
      expect(drawer()).toBeNull()
      await closeMenu()
      for (const key of ['Enter', ' ']) {
        await act(async () =>
          trigger.dispatchEvent(
            new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }),
          ),
        )
        expect(drawer()).toBeNull()
        await closeMenu()
      }
      await act(async () => trigger.click())
      await until(() => expect(document.querySelector('[role="menu"]')).not.toBeNull())
      const popup = document.querySelector<HTMLElement>('[role="menu"]')!
      await act(async () => {
        popup.click()
        popup.dispatchEvent(
          new KeyboardEvent('keydown', { key: ' ', bubbles: true, cancelable: true }),
        )
      })
      expect(drawer()).toBeNull()
      expect(detailCalls(models[0].id)).toHaveLength(0)
      expect(requests).toHaveLength(beforeMenu)
      await act(async () =>
        popup.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
      )
      await until(() => expect(document.querySelector('[role="menu"]')).toBeNull())
      const api =
        view === 'card'
          ? button('Open API access for nested-menu')
          : item.querySelector<HTMLButtonElement>('td:last-child button')!
      await act(async () => api.click())
      await until(() =>
        expect(drawer()?.querySelector('h2.break-words')?.textContent).toBe(models[0].name),
      )
      expect(detailCalls(models[0].id)).toHaveLength(1)
      expect(requests.some((request) => request.method !== 'get')).toBe(false)
    },
  )

  it.each(['card', 'table'] as const)(
    'does not activate %s while selecting text or using other keyboard/mouse gestures',
    async (view) => {
      models = [model('selectable')]
      await mount()
      const item = await surface(view, models[0].name)
      const text = item.querySelector(view === 'card' ? 'h2' : 'td')!.firstChild!
      const range = document.createRange()
      range.selectNodeContents(text)
      const selection = window.getSelection()!
      selection.removeAllRanges()
      selection.addRange(range)
      try {
        await act(async () =>
          item.dispatchEvent(new MouseEvent('click', { bubbles: true, button: 0 })),
        )
        expect(drawer()).toBeNull()
        selection.removeAllRanges()
        for (const key of ['Escape', 'ArrowDown', 'Tab'])
          await act(async () =>
            item.dispatchEvent(
              new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }),
            ),
          )
        await act(async () =>
          item.dispatchEvent(new MouseEvent('click', { bubbles: true, button: 2 })),
        )
        expect(drawer()).toBeNull()
        expect(detailCalls(models[0].id)).toHaveLength(0)
      } finally {
        selection.removeAllRanges()
      }
    },
  )

  it.each(['card', 'table'] as const)(
    'preserves nested focusable %s controls and their native keyboard defaults',
    async (view) => {
      models = [model('nested-control')]
      await mount()
      const item = await surface(view, models[0].name)
      const location = view === 'card' ? item : item.querySelector('td')!
      const link = document.createElement('a')
      link.href = '#local-control'
      link.textContent = 'Local destination'
      const checkbox = document.createElement('input')
      checkbox.type = 'checkbox'
      location.append(link, checkbox)
      try {
        await act(async () => checkbox.click())
        expect(checkbox.checked).toBe(true)
        link.focus()
        for (const key of ['Enter', ' ']) {
          const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
          await act(async () => link.dispatchEvent(event))
          expect(event.defaultPrevented).toBe(false)
        }
        const before = requests.length
        const click = new MouseEvent('click', { bubbles: true, cancelable: true })
        // Prevent jsdom navigation at the native control, without rewriting the catalogue handler.
        link.addEventListener('click', (event) => event.preventDefault(), { once: true })
        await act(async () => link.dispatchEvent(click))
        expect(drawer()).toBeNull()
        expect(detailCalls(models[0].id)).toHaveLength(0)
        expect(requests).toHaveLength(before)
      } finally {
        link.remove()
        checkbox.remove()
      }
    },
  )

  it.each(['session-fetch', 'list-fetch', 'list-error', 'missing-id', 'actor-change'] as const)(
    'rejects stale activation synchronously at the %s boundary',
    async (boundary) => {
      models = [model('authority-target')]
      await mount()
      const item = await surface('card', models[0].name)
      const list = cache
        .getQueryCache()
        .find({ queryKey: ['model-catalog', 'list'], exact: false })!
      const sessionQuery = cache.getQueryCache().find({ queryKey: ['auth', 'session'] })!
      await act(async () => {
        if (boundary === 'session-fetch') sessionQuery.setState({ fetchStatus: 'fetching' })
        else if (boundary === 'list-fetch') list.setState({ fetchStatus: 'fetching' })
        else if (boundary === 'list-error')
          list.setState({ status: 'error', error: new Error('Unavailable') })
        else if (boundary === 'missing-id') list.setState({ data: [] })
        else
          cache.setQueryData(['auth', 'session'], {
            user: { id: 'usr_other', role: 'member' },
            csrf_token: 'new',
          })
        item.click()
        item.querySelector<HTMLButtonElement>('button')!.click()
        item.dispatchEvent(
          new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }),
        )
      })
      expect(drawer()).toBeNull()
      expect(detailCalls(models[0].id)).toHaveLength(0)
      if (boundary !== 'actor-change')
        await until(() => expect(host.querySelector('article')).toBeNull())
    },
  )

  it('requires the current candidate page and ungranted exact ID without substituting the catalogue', async () => {
    models = [model('candidate-only', ['openai_chat'], [])]
    await mount()
    await select('Access source', 'requestable')
    await until(() => expect(host.querySelector('article')).not.toBeNull())
    const item = await surface('card', models[0].name)
    const candidateQuery = cache
      .getQueryCache()
      .find({ queryKey: ['personal-model-candidates'], exact: false })!
    const data = candidateQuery.state.data as {
      pages: { items: { personal_granted: boolean }[] }[]
    }
    await act(async () => {
      candidateQuery.setState({
        data: {
          ...data,
          pages: data.pages.map((page) => ({
            ...page,
            items: page.items.map((record) => ({ ...record, personal_granted: true })),
          })),
        },
      })
      item.click()
      item.querySelector<HTMLButtonElement>('button')!.click()
    })
    expect(drawer()).toBeNull()
    expect(detailCalls(models[0].id, true)).toHaveLength(0)
    expect(detailCalls(models[0].id)).toHaveLength(0)
  })

  it('switches live labels without resetting filters or opening any mutation', async () => {
    models = [model('localized-target')]
    await mount()
    await search('localized')
    await act(async () => i18n.changeLanguage('zh'))
    const item = host.querySelector<HTMLElement>('article')!
    expect(item.getAttribute('aria-label')).toBe(
      i18n.t('catalog:memberModels.openAPI', { name: models[0].name }),
    )
    expect(host.querySelector<HTMLInputElement>('input[type="search"]')!.value).toBe('localized')
    await act(async () => item.click())
    await until(() =>
      expect(drawer()?.querySelector('h2.break-words')?.textContent).toBe(models[0].name),
    )
    expect(detailCalls(models[0].id)).toHaveLength(1)
    expect(requests.some((request) => request.method !== 'get')).toBe(false)
  })
})

describe('Current catalogue trigger focus after drawer dismissal', () => {
  function target(view: 'card' | 'table', id: string) {
    return [...host.querySelectorAll<HTMLElement>(view === 'card' ? 'article' : 'tbody tr')].find(
      (node) => node.getAttribute('aria-label') === `Open API access for ${id}`,
    )!
  }
  async function show(view: 'card' | 'table') {
    await mount()
    if (view === 'table') await act(async () => button('Table').click())
  }
  async function dismiss(mode: 'close' | 'escape') {
    await act(async () => {
      if (mode === 'close') button('Close').click()
      else drawer().dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    await until(() => expect(drawer()).toBeNull())
  }

  it.each(['card', 'table'] as const)(
    'returns a passive %s click to its whole surface',
    async (view) => {
      models = [model('passive-focus')]
      await show(view)
      const item = target(view, models[0].name)
      button(view === 'card' ? 'Cards' : 'Table').focus()
      await act(async () => item.querySelector<HTMLElement>(view === 'card' ? 'dd' : 'td')!.click())
      await until(() =>
        expect(drawer()?.querySelector('h2.break-words')?.textContent).toBe(models[0].name),
      )
      await dismiss('close')
      await until(() => expect(document.activeElement).toBe(item))
    },
  )

  it.each([
    ['card', 'surface', 'close'],
    ['card', 'action', 'escape'],
    ['table', 'surface', 'escape'],
    ['table', 'action', 'close'],
  ] as const)('returns renewed %s %s to its current node after %s', async (view, kind, mode) => {
    models = [model('renewed-focus')]
    await show(view)
    const old = target(view, models[0].name)
    const trigger = kind === 'surface' ? old : old.querySelector<HTMLButtonElement>('button')!
    trigger.focus()
    await act(async () => {
      if (kind === 'surface')
        trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
      else trigger.click()
    })
    await until(() =>
      expect(drawer()?.querySelector('h2.break-words')?.textContent).toBe(models[0].name),
    )
    await act(async () => cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true }))
    await until(() => expect(target(view, models[0].name)).toBeDefined())
    const renewed = target(view, models[0].name)
    expect(old.isConnected).toBe(false)
    expect(renewed).not.toBe(old)
    const expected =
      kind === 'surface' ? renewed : renewed.querySelector<HTMLButtonElement>('button')!
    await dismiss(mode)
    await until(() => expect(document.activeElement).toBe(expected))
  })

  it('never restores a requestable surface that became personally granted', async () => {
    models = [model('request-focus', ['openai_chat'], [])]
    await mount(false)
    await until(() =>
      expect(host.querySelector('select[aria-label="Access source"]')).not.toBeNull(),
    )
    await select('Access source', 'requestable')
    await until(() => expect(target('card', 'request-focus')).toBeDefined())
    const old = target('card', 'request-focus')
    old.focus()
    await act(async () =>
      old.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })),
    )
    await until(() =>
      expect(drawer()?.querySelector('h2.break-words')?.textContent).toBe('request-focus'),
    )
    models = [model('request-focus')]
    await act(async () => cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true }))
    await until(() => expect(target('card', 'request-focus')).toBeUndefined())
    await dismiss('escape')
    expect(document.activeElement).not.toBe(old)
    expect([...host.querySelectorAll('article, article button')]).not.toContain(
      document.activeElement,
    )
  })

  it.each(['removed', 'actor', 'view', 'pending', 'error'] as const)(
    'never restores a %s stale target',
    async (change) => {
      models = [model('stale-focus'), model('other-focus')]
      await show('card')
      const old = target('card', models[0].name)
      old.focus()
      await act(async () =>
        old.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })),
      )
      await until(() => expect(drawer()).not.toBeNull())
      if (change === 'view') await act(async () => button('Table').click())
      else if (change === 'pending') {
        await act(async () =>
          cache
            .getQueryCache()
            .find({ queryKey: ['model-catalog', 'list'], exact: false })
            ?.setState({ fetchStatus: 'fetching' }),
        )
      } else if (change === 'error') {
        failures['/model-catalog'] = 503
        await act(async () =>
          cache.refetchQueries({ queryKey: ['model-catalog', 'list'], exact: false }),
        )
      } else {
        if (change === 'removed') models = [models[1]]
        else actorID = 'usr_new'
        await act(async () => cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true }))
      }
      if (drawer()) await dismiss('close')
      expect(document.activeElement).not.toBe(old)
      const all = [...host.querySelectorAll('article,tbody tr,article button,tbody tr button')]
      expect(all).not.toContain(document.activeElement)
    },
  )
})
