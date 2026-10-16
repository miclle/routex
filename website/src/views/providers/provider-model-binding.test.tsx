import { act, useSyncExternalStore } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import ProviderModelBinding, { type ProviderModelBindingSource } from './provider-model-binding'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
const source: ProviderModelBindingSource = {
  providerID: 'prv_source',
  providerName: 'Source Provider',
  connectionID: 'con_source',
  connectionName: 'Exact Connection',
  modelID: 'pmd_source',
  upstreamName: 'Exact upstream',
  protocol: 'openai_chat',
}
const row = {
  id: 'mdl_target',
  name: 'existing-public-model',
  initial_weight: 0,
  selectable: true,
  blocker_codes: [],
}
const candidate = {
  id: 'pmd_source',
  provider_id: 'prv_source',
  connection_id: 'con_source',
  upstream_name: 'Exact upstream',
  protocol: 'openai_chat',
  provider_name: 'Source Provider',
  connection_name: 'Exact Connection',
  verification_covered: true,
  configured_available: true,
  selectable: true,
  review_etag: 'a'.repeat(64),
  prices: null,
}
const route = {
  id: 'bnd_old',
  provider_id: 'prv_old',
  provider_name: 'Existing Provider',
  connection_id: 'con_old',
  connection_name: 'Existing Connection',
  provider_model_id: 'pmd_old',
  upstream_name: 'existing upstream',
  protocol: 'openai_chat',
  weight: 100,
}
const savedModel = {
  id: 'mdl_target',
  name: 'existing-public-model',
  status: 'active',
  names: [],
  granted_user_ids: [],
  bindings: [
    { ...route, ready: true },
    {
      id: 'bnd_new',
      provider_id: source.providerID,
      connection_id: source.connectionID,
      provider_model_id: source.modelID,
      upstream_name: source.upstreamName,
      protocol: source.protocol,
      weight: 0,
      ready: true,
    },
  ],
}
let root: Root, host: HTMLDivElement, cache: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let readable: boolean, writable: boolean, revision: string
let queries: InternalAxiosRequestConfig[], posts: InternalAxiosRequestConfig[]
let failure: boolean, held: (() => void) | null, holdWrite: boolean, denyReview: boolean
let weightFailure: boolean, weightRuntime: boolean
let weightWrites: InternalAxiosRequestConfig[]
let weightTag: string
let weightModel: typeof savedModel
let reviewTag: string, targetPage: (typeof row)[], nextCursor: string | null
const close = vi.fn(),
  saved = vi.fn()
function TestDialog() {
  useSyncExternalStore(
    (notify) => cache.getQueryCache().subscribe(() => notify()),
    () => revision,
    () => revision,
  )
  return (
    <ProviderModelBinding
      actor="usr_actor"
      source={source}
      revision={revision}
      readReady={() =>
        readable &&
        cache.getQueryData<{ user: { id: string } }>(sessionKey)?.user.id === 'usr_actor'
      }
      writeReady={() => writable}
      onClose={close}
      onSaved={saved}
      onActivity={() => undefined}
      finalFocus={() => false}
    />
  )
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  readable = writable = true
  revision = 'first'
  queries = []
  posts = []
  failure = holdWrite = denyReview = false
  held = null
  weightFailure = weightRuntime = false
  weightWrites = []
  weightTag = 'c'.repeat(64)
  weightModel = structuredClone(savedModel)
  reviewTag = 'a'.repeat(64)
  targetPage = [row]
  nextCursor = null
  close.mockClear()
  saved.mockClear()
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  cache.setQueryData(sessionKey, {
    user: { id: 'usr_actor', role: 'member' },
    csrf_token: 'fresh-csrf',
  })
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  client.defaults.adapter = async (config) => {
    queries.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/admin/connections/con_source/model-creation/models')
      response.data = { items: structuredClone(targetPage), next_cursor: nextCursor }
    else if (config.url === '/admin/models/mdl_target/routing-candidates/pmd_source') {
      if (denyReview)
        throw new AxiosError('Controlled missing source', '', config, undefined, {
          ...response,
          status: 404,
        })
      response.data = {
        model_id: row.id,
        model_name: row.name,
        candidate: { ...candidate, review_etag: reviewTag },
        routes: [route],
      }
    } else if (config.url === '/admin/models/mdl_target/bindings' && config.method === 'post') {
      posts.push(config)
      if (holdWrite)
        await new Promise<void>((resolve) => {
          held = resolve
        })
      if (failure)
        throw new AxiosError('Controlled uncertain result', '', config, undefined, {
          ...response,
          status: 503,
        })
      response.status = 201
      response.data = structuredClone(savedModel)
    } else if (config.url === '/admin/models/mdl_target/weights/review') {
      response.headers.set('etag', `"${weightTag}"`)
      response.data = {
        model: structuredClone(weightModel),
        review_etag: weightTag,
        runtime_applied: weightRuntime,
      }
    } else if (
      config.url === '/admin/models/mdl_target/weights/reviewed' &&
      config.method === 'put'
    ) {
      weightWrites.push(config)
      if (holdWrite)
        await new Promise<void>((resolve) => {
          held = resolve
        })
      if (weightFailure)
        throw new AxiosError('Controlled weight uncertainty', '', config, undefined, {
          ...response,
          status: 503,
        })
      const input = JSON.parse(config.data) as { weights: { binding_id: string; weight: number }[] }
      for (const binding of weightModel.bindings)
        binding.weight = input.weights.find((row) => row.binding_id === binding.id)!.weight
      weightTag = 'd'.repeat(64)
      response.headers.set('etag', `"${weightTag}"`)
      response.data = {
        model: structuredClone(weightModel),
        review_etag: weightTag,
        runtime_applied: weightRuntime,
      }
    } else throw new Error(`Unexpected binding request: ${config.method} ${config.url}`)
    return response
  }
  router = createMemoryRouter([
    { path: '/', element: <TestDialog /> },
    { path: '/admin/models/:id', element: <p>Model workspace</p> },
  ])
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(button('Select model')).toBeTruthy())
})
afterEach(async () => {
  held?.()
  await act(async () => root.unmount())
  router.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  await i18n.changeLanguage('en')
})
function button(label: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label,
  )!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function until(assertion: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assertion()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
async function select() {
  await click('Select model')
  await until(() =>
    expect(document.querySelector('table[aria-label="Reviewed protocol routing"]')).not.toBeNull(),
  )
}
async function renew() {
  revision += '-renewed'
  await act(async () => {
    cache.setQueryData(['controlled-authority-change'], revision)
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    )
  })
}
it('reviews the exact row and inserts at zero without changing existing weights or fetching a directory', async () => {
  await select()
  expect(document.querySelector('[role=dialog]')?.textContent).toContain('Exact upstream')
  expect(document.querySelector('[role=dialog]')?.textContent).toContain('100%')
  expect(queries[0].params).toEqual({})
  const review = queries.find((request) => request.url?.includes('/routing-candidates/'))!
  expect(review.params).toEqual({ protocol: 'openai_chat', provider_id: 'prv_source' })
  await click('Add candidate route')
  await until(() => expect(button('Save routing weights')).toBeTruthy())
  expect(JSON.parse(posts[0].data)).toEqual({
    provider_model_id: 'pmd_source',
    protocol: 'openai_chat',
    review_etag: 'a'.repeat(64),
  })
  expect(posts[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
  expect(
    queries.some((request) => ['/admin/models', '/admin/providers'].includes(request.url!)),
  ).toBe(false)
  expect(document.querySelector('[role=status]')?.textContent).toContain('0%')
})
it('allows independent reads but denies insertion without Model write authority', async () => {
  writable = false
  await select()
  expect(button('Add candidate route').disabled).toBe(true)
  await click('Add candidate route')
  expect(posts).toHaveLength(0)
  expect(saved).not.toHaveBeenCalled()
})
it('retains the chosen target across bounded search pages and switches language without losing its review', async () => {
  await select()
  targetPage = []
  await act(async () => {
    const input = document.querySelector<HTMLInputElement>('[role=dialog] input[type=search]')!
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      input,
      'No matching page',
    )
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await until(() =>
    expect(document.querySelector('[role=dialog]')?.textContent).toContain('No matching models.'),
  )
  expect(document.querySelector('[role=dialog]')?.textContent).toContain(
    'Routing review: existing-public-model',
  )
  await act(async () => i18n.changeLanguage('zh'))
  expect(button('添加候选关系').disabled).toBe(false)
  expect(document.querySelector('[role=dialog]')?.textContent).toContain('existing-public-model')
  await act(async () => i18n.changeLanguage('en'))
  await click('Add candidate route')
  await until(() => expect(button('Save routing weights')).toBeTruthy())
})
it('keeps uncertain retries on the original token after a later exact read fails', async () => {
  await select()
  failure = true
  await click('Add candidate route')
  await until(() => expect(button('Retry exact insertion')).toBeTruthy())
  const original = posts[0].data
  reviewTag = 'b'.repeat(64)
  denyReview = true
  failure = false
  cache.setQueryData(sessionKey, { user: { id: 'usr_actor' }, csrf_token: 'renewed-csrf' })
  await renew()
  await click('Retry exact insertion')
  await until(() => expect(posts).toHaveLength(2))
  expect(posts[1].data).toBe(original)
  expect(posts[1].headers.get('X-CSRF-Token')).toBe('renewed-csrf')
  await until(() => expect(button('Save routing weights')).toBeTruthy())
})
it.each(['read', 'write', 'actor', 'unmount'] as const)(
  'rejects late insertion results after %s changes',
  async (kind) => {
    await select()
    holdWrite = true
    await click('Add candidate route')
    await until(() => expect(held).toBeTypeOf('function'))
    await act(async () => {
      if (kind === 'read') readable = false
      else if (kind === 'write') writable = false
      else if (kind === 'actor')
        cache.setQueryData(sessionKey, { user: { id: 'usr_other' }, csrf_token: 'other' })
      else await router.navigate('/admin/models/mdl_elsewhere')
      cache.setQueryData(['controlled-change'], kind)
      held!()
    })
    await until(() => expect(button('Adding candidate…')).toBeUndefined())
    expect(saved).not.toHaveBeenCalled()
    expect(close).not.toHaveBeenCalled()
  },
)
it('does not claim historical success from a renewed read after an unknown response', async () => {
  await select()
  failure = true
  await click('Add candidate route')
  await until(() => expect(button('Retry exact insertion')).toBeTruthy())
  denyReview = true
  await renew()
  expect(saved).not.toHaveBeenCalled()
  expect(close).not.toHaveBeenCalled()
  expect(button('Retry exact insertion')).toBeTruthy()
})

it('releases a same-owner renewed pending write and retries its identical source with fresh CSRF', async () => {
  await select()
  holdWrite = true
  await click('Add candidate route')
  await until(() => expect(held).toBeTypeOf('function'))
  const original = posts[0].data
  const release = held!
  cache.setQueryData(sessionKey, { user: { id: 'usr_actor' }, csrf_token: 'renewed-csrf' })
  await renew()
  await until(() => expect(button('Retry exact insertion')?.disabled).toBe(false))
  await act(async () => release())
  expect(saved).not.toHaveBeenCalled()
  holdWrite = false
  await click('Retry exact insertion')
  await until(() => expect(button('Save routing weights')).toBeTruthy())
  expect(posts).toHaveLength(2)
  expect(posts[1].data).toBe(original)
  expect(posts[1].headers.get('X-CSRF-Token')).toBe('renewed-csrf')
})

async function editWeight(name: string, value: string) {
  await act(async () => {
    const input = document.querySelector<HTMLInputElement>(
      `input[aria-label="${name} routing weight"]`,
    )!
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

it('keeps insertion and complete protocol weights as separate explicit operations', async () => {
  await select()
  await click('Add candidate route')
  await until(() => expect(button('Save routing weights')).toBeTruthy())
  expect(saved).not.toHaveBeenCalled()
  expect(weightWrites).toHaveLength(0)
  await editWeight('existing upstream', '70')
  expect(button('Save routing weights').disabled).toBe(true)
  await editWeight('Exact upstream', '30')
  expect(button('Save routing weights').disabled).toBe(false)
  weightRuntime = true
  await click('Save routing weights')
  await until(() => expect(document.body.textContent).toContain('observed in this process runtime'))
  expect(weightWrites).toHaveLength(1)
  expect(JSON.parse(weightWrites[0].data)).toEqual({
    weights: [
      { binding_id: 'bnd_old', weight: 70 },
      { binding_id: 'bnd_new', weight: 30 },
    ],
  })
  expect(weightWrites[0].headers.get('If-Match')).toBe(`"${'c'.repeat(64)}"`)
  await click('Done')
  expect(saved).toHaveBeenCalledOnce()
})

it('keeps other protocols read only and includes their exact reviewed weights in the atomic save', async () => {
  weightModel.bindings.push({
    ...weightModel.bindings[1],
    id: 'bnd_responses',
    provider_model_id: 'pmd_responses',
    protocol: 'openai_responses',
    weight: 100,
    upstream_name: 'Responses upstream',
  })
  await select()
  await click('Add candidate route')
  await until(() => expect(button('Save routing weights')).toBeTruthy())
  const frozen = document.querySelector<HTMLInputElement>(
    'input[aria-label="Responses upstream routing weight"]',
  )!
  expect(frozen.disabled).toBe(true)
  await click('Save routing weights')
  await until(() => expect(weightWrites).toHaveLength(1))
  expect(JSON.parse(weightWrites[0].data).weights).toContainEqual({
    binding_id: 'bnd_responses',
    weight: 100,
  })
})

it('retains the exact uncertain weight request and can explicitly abandon it for a new current review', async () => {
  await select()
  await click('Add candidate route')
  await until(() => expect(button('Save routing weights')).toBeTruthy())
  weightFailure = true
  await click('Save routing weights')
  await until(() => expect(button('Retry exact weight save')).toBeTruthy())
  const original = weightWrites[0].data
  weightTag = 'e'.repeat(64)
  cache.setQueryData(sessionKey, { user: { id: 'usr_actor' }, csrf_token: 'renewed-csrf' })
  await renew()
  await click('Retry exact weight save')
  await until(() => expect(weightWrites).toHaveLength(2))
  expect(weightWrites[1].data).toBe(original)
  expect(weightWrites[1].headers.get('If-Match')).toBe(`"${'c'.repeat(64)}"`)
  expect(weightWrites[1].headers.get('X-CSRF-Token')).toBe('renewed-csrf')
  await click('Discard local retry and review current weights')
  await until(() => expect(button('Save routing weights')).toBeTruthy())
  expect(document.body.textContent).toContain('Current reviewed weights are configuration only')
  expect(saved).not.toHaveBeenCalled()
  weightFailure = false
  await click('Save routing weights')
  await until(() => expect(weightWrites).toHaveLength(3))
  expect(weightWrites[2].headers.get('If-Match')).toBe(`"${'e'.repeat(64)}"`)
})

it('requires explicit review after concurrent same-topology weight changes', async () => {
  await select()
  await click('Add candidate route')
  await until(() => expect(button('Save routing weights')).toBeTruthy())
  await editWeight('existing upstream', '80')
  await editWeight('Exact upstream', '20')
  const query = cache
    .getQueryCache()
    .findAll()
    .find((item) => item.queryKey[1] === 'provider-model-binding-weights')!
  weightTag = 'f'.repeat(64)
  weightModel.bindings[0].weight = 60
  weightModel.bindings[1].weight = 40
  await act(async () =>
    cache.setQueryData(query.queryKey, {
      model: structuredClone(weightModel),
      review_etag: weightTag,
      runtime_applied: false,
    }),
  )
  await until(() => expect(button('Save routing weights').disabled).toBe(true))
  expect(weightWrites).toHaveLength(0)
  await click('Review current routes')
  await until(() => expect(button('Save routing weights').disabled).toBe(false))
  expect(
    document.querySelector<HTMLInputElement>('input[aria-label="Exact upstream routing weight"]')
      ?.value,
  ).toBe('40')
})

it.each(['read', 'write', 'actor', 'unmount'] as const)(
  'rejects obsolete weight-save success after %s changes',
  async (kind) => {
    await select()
    await click('Add candidate route')
    await until(() => expect(button('Save routing weights')).toBeTruthy())
    holdWrite = true
    weightRuntime = true
    await click('Save routing weights')
    await until(() => expect(held).toBeTypeOf('function'))
    await act(async () => {
      if (kind === 'read') readable = false
      else if (kind === 'write') writable = false
      else if (kind === 'actor')
        cache.setQueryData(sessionKey, { user: { id: 'usr_other' }, csrf_token: 'other' })
      else await router.navigate('/admin/models/mdl_elsewhere')
      cache.setQueryData(['controlled-change'], kind)
      held!()
    })
    await until(() =>
      expect(document.body.textContent).not.toContain('The complete weights were saved'),
    )
    expect(saved).not.toHaveBeenCalled()
  },
)

it('preserves a pending weight intent across hidden same-owner read renewal', async () => {
  await select()
  await click('Add candidate route')
  await until(() => expect(button('Save routing weights')).toBeTruthy())
  holdWrite = true
  await click('Save routing weights')
  await until(() => expect(held).toBeTypeOf('function'))
  const original = weightWrites[0].data
  const release = held!
  readable = false
  await renew()
  expect(document.querySelector('[role=dialog]')).toBeNull()
  readable = true
  cache.setQueryData(sessionKey, { user: { id: 'usr_actor' }, csrf_token: 'fresh-renewal' })
  await renew()
  await until(() => expect(button('Retry exact weight save')?.disabled).toBe(false))
  await act(async () => release())
  holdWrite = false
  await click('Retry exact weight save')
  await until(() => expect(weightWrites).toHaveLength(2))
  expect(weightWrites[1].data).toBe(original)
  expect(weightWrites[1].headers.get('If-Match')).toBe(`"${'c'.repeat(64)}"`)
  expect(weightWrites[1].headers.get('X-CSRF-Token')).toBe('fresh-renewal')
})

it('offers separate full Model creation only after a complete target list has no eligible choice', async () => {
  targetPage = []
  await act(async () => {
    await cache.invalidateQueries({
      predicate: (query) => query.queryKey[1] === 'provider-model-binding-targets',
    })
  })
  await until(() => expect(button('Open full Model creation')).toBeTruthy())
  expect(document.body.textContent).toContain('select and review the upstream model again')
  await click('Open full Model creation')
  expect(close).toHaveBeenCalledOnce()
  expect(router.state.location.pathname).toBe('/admin/models/new')
  expect(router.state.location.search).toBe('?connectionId=con_source')
  expect(posts).toHaveLength(0)
  expect(weightWrites).toHaveLength(0)
})
