import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Link, Route, Routes } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import AdminModelsPage from './admin'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import { protocolLabel } from '@/lib/protocols'
import type { Model } from '@/types/catalog'
import type { Session } from '@/types/auth'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const protocols = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_generate_content',
]
const originalAdapter = client.defaults.adapter
let root: Root
let container: HTMLDivElement
let cache: QueryClient
let model: Model
let requests: InternalAxiosRequestConfig[]
let permissions: string[]
let session: Session
let sessionWait: Promise<void> | null
let permissionWait: Promise<void> | null
let permissionFailure: boolean
let writeFailure: boolean
let writeWait: Promise<void> | null
let detailWait: Promise<void> | null
let detailFailure: boolean

beforeEach(async () => {
  await i18n.changeLanguage('en')
  requests = []
  permissions = ['models.read_all', 'models.write']
  session = {
    user: { id: 'usr_1', name: 'User', email: 'user@example.com', role: 'admin' },
    csrf_token: 'csrf',
  }
  sessionWait = permissionWait = writeWait = detailWait = null
  detailFailure = false
  permissionFailure = writeFailure = false
  model = {
    id: 'mdl_1',
    name: 'Public Model',
    status: 'active',
    names: [{ name: 'Public Model', is_current: true, expires_at: null }],
    granted_user_ids: [],
    bindings: protocols.flatMap((protocol, index) =>
      [100, 0].map((weight, backup) => ({
        id: `bind_${index}_${backup}`,
        provider_id: `prv_${index}`,
        connection_id: `con_${index}`,
        provider_model_id: `pmd_${index}_${backup}`,
        upstream_name: `${protocol}-${backup}`,
        protocol,
        weight,
        ready: true,
      })),
    ),
  }
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const path = config.url!
    if (path === '/auth/session' && sessionWait) await sessionWait
    if (path === '/auth/permissions' && permissionWait) await permissionWait
    if (config.method === 'put' && writeWait) await writeWait
    if (path === '/admin/models/mdl_1' && detailWait) await detailWait
    const failed =
      (path === '/auth/permissions' && permissionFailure) ||
      (path === '/admin/models/mdl_1' && detailFailure) ||
      (config.method === 'put' && writeFailure)
    const response = {
      config,
      status: failed ? 500 : 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (failed) {
      response.data = { message: 'Service unavailable' }
      throw new AxiosError('Request failed', '', config, undefined, response)
    }
    if (path === '/auth/session') response.data = structuredClone(session)
    else if (path === '/auth/permissions') response.data = { permissions: [...permissions] }
    else if (path === '/admin/models/mdl_1') response.data = structuredClone(model)
    else if (path === '/admin/models/mdl_2')
      response.data = { ...structuredClone(model), id: 'mdl_2', name: 'Second Model' }
    else if (config.method === 'put' && path === '/admin/models/mdl_1/weights') {
      const body = JSON.parse(config.data) as { weights: { binding_id: string; weight: number }[] }
      model.bindings = model.bindings.map((binding) => ({
        ...binding,
        weight: body.weights.find((weight) => weight.binding_id === binding.id)!.weight,
      }))
      response.data = structuredClone(model)
    } else throw new Error(`Unexpected scoped request ${config.method} ${path}`)
    return response
  }
})

afterEach(async () => {
  await act(async () => {
    root.unmount()
  })
  cache.clear()
  container.remove()
  client.defaults.adapter = originalAdapter
  await i18n.changeLanguage('en')
})

async function until(check: () => void) {
  let failure: unknown
  for (let attempt = 0; attempt < 100; attempt++) {
    try {
      check()
      return
    } catch (error) {
      failure = error
    }
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
  }
  throw failure
}
async function mount() {
  await act(async () => {
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter initialEntries={['/admin/models/mdl_1']}>
          <Link to="/admin/models/mdl_2">Other Model</Link>
          <Routes>
            <Route path="/admin/models/:modelId" element={<AdminModelsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })
  await until(() => expect(form()).not.toBeNull())
}
function form() {
  return container.querySelector<HTMLFormElement>('form[aria-label="Provider routing weights"]')
}
function save() {
  return form()!.querySelector<HTMLButtonElement>('button[type="submit"]')!
}

function total(protocol: string) {
  return container.querySelector<HTMLElement>(
    `[aria-label="${protocolLabel(protocol)} draft weight total"]`,
  )!
}
function writes() {
  return requests.filter((request) => request.method === 'put')
}
async function fill(id: string, value: string) {
  const input = container.querySelector<HTMLInputElement>(`input[name="${id}"]`)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function submit(element = form()!) {
  await act(async () => {
    element.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}
function deferred() {
  let resolve!: () => void
  const promise = new Promise<void>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

describe('Protocol-grouped routing weight drafts', () => {
  it('shows four independent protocol tables and totals without borrowing Provider or price reads', async () => {
    await mount()
    for (const protocol of protocols) {
      const table = container.querySelector(
        `table[aria-label="Public Model ${protocolLabel(protocol)} provider routes"]`,
      )!
      expect(table).not.toBeNull()
      expect(table.querySelectorAll('tbody tr')).toHaveLength(2)
      expect(total(protocol).textContent).toBe('Draft weights total 100%')
    }
    expect(container.textContent).toContain('not routing availability')
    expect(
      requests.every((request) =>
        ['/auth/session', '/auth/permissions', '/admin/models/mdl_1'].includes(request.url!),
      ),
    ).toBe(true)
    expect(save().disabled).toBe(false)
  })
  it('updates under/over feedback live while other protocol drafts remain independent', async () => {
    await mount()
    await fill('bind_0_0', '60')
    expect(total('openai_chat').textContent).toBe('Draft weights total 60%; allocate 40% more')
    expect(save().disabled).toBe(true)
    await submit()
    expect(writes()).toHaveLength(0)
    await fill('bind_0_1', '50')
    expect(total('openai_chat').textContent).toBe('Draft weights total 110%; reduce by 10%')
    await submit()
    expect(writes()).toHaveLength(0)
    await fill('bind_0_0', '50')
    expect(total('openai_chat').textContent).toBe('Draft weights total 100%')
    expect(save().disabled).toBe(false)
    for (const protocol of protocols.slice(1))
      expect(total(protocol).textContent).toBe('Draft weights total 100%')
  })
  it.each(['', '1.5', '-1', '101'])(
    'rejects invalid individual value %j even with a direct form submit',
    async (value) => {
      await mount()
      await fill('bind_0_0', value)
      expect(total('openai_chat').textContent).toBe(
        'Enter whole-number weights from 0 to 100 for every binding.',
      )
      expect(save().disabled).toBe(true)
      await submit()
      expect(writes()).toHaveLength(0)
    },
  )
  it('submits the complete original binding set once, with independent protocol totals of 100 rather than a global total', async () => {
    await mount()
    await fill('bind_0_0', '60')
    await fill('bind_0_1', '40')
    await fill('bind_1_0', '75')
    await fill('bind_1_1', '25')
    writeFailure = true
    await submit()
    await until(() =>
      expect(container.querySelector('[role="alert"]')?.textContent).toContain(
        'The action failed. Check the service connection and retry.',
      ),
    )
    expect(writes()).toHaveLength(1)
    expect(writes()[0].url).toBe('/admin/models/mdl_1/weights')
    expect(JSON.parse(writes()[0].data)).toEqual({
      weights: model.bindings.map((binding, index) => ({
        binding_id: binding.id,
        weight: [60, 40, 75, 25, 100, 0, 100, 0][index],
      })),
    })
    expect(container.querySelector<HTMLInputElement>('input[name="bind_0_0"]')!.value).toBe('60')
  })
  it('never dispatches an incomplete protocol total even through a crafted form submit', async () => {
    await mount()
    await fill('bind_0_0', '60')
    await submit()
    expect(writes()).toHaveLength(0)
  })
  it('saves a valid complete draft atomically and prevents duplicate submits while pending', async () => {
    await mount()
    await fill('bind_0_0', '60')
    await fill('bind_0_1', '40')
    const wait = deferred()
    writeWait = wait.promise
    await submit()
    await until(() => expect(writes()).toHaveLength(1))
    await until(() => expect(save().disabled).toBe(true))
    expect(
      [...container.querySelectorAll<HTMLInputElement>('input[name^="bind_"]')].every(
        (input) => input.disabled,
      ),
    ).toBe(true)
    await submit()
    expect(writes()).toHaveLength(1)
    await act(async () => {
      wait.resolve()
    })
    await until(() => expect(save().disabled).toBe(false))
    expect(JSON.parse(writes()[0].data)).toEqual({
      weights: model.bindings.map((binding) => ({
        binding_id: binding.id,
        weight: binding.weight,
      })),
    })
    expect(model.bindings.map((binding) => binding.weight)).toEqual([
      60, 40, 100, 0, 100, 0, 100, 0,
    ])
    expect(container.querySelector<HTMLInputElement>('input[name="bind_0_0"]')!.value).toBe('60')
  })
  it('keeps non-ready zero candidates valid and lets the server decide ambiguous positive credential readiness', async () => {
    model.bindings[1].ready = false
    writeFailure = true
    await mount()
    expect(save().disabled).toBe(false)
    expect(container.textContent).not.toContain('Current routing is unavailable.')
    await fill('bind_0_0', '99')
    await fill('bind_0_1', '1')
    expect(total('openai_chat').textContent).toBe('Draft weights total 100%')
    expect(container.textContent).toContain('Current routing is unavailable.')
    expect(save().disabled).toBe(false)
    await submit()
    await until(() => expect(writes()).toHaveLength(1))
    await until(() =>
      expect(container.textContent).toContain(
        'The action failed. Check the service connection and retry.',
      ),
    )
    expect(JSON.parse(writes()[0].data).weights).toContainEqual({
      binding_id: 'bind_0_1',
      weight: 1,
    })
    expect(container.querySelector<HTMLInputElement>('input[name="bind_0_1"]')!.value).toBe('1')
    expect(save().disabled).toBe(false)
    await fill('bind_0_0', '100')
    await fill('bind_0_1', '0')
    expect(save().disabled).toBe(false)
    expect(container.textContent).not.toContain('Current routing is unavailable.')
  })
  it('does not prohibit positive configuration of a disabled Provider Model whose verified credentials the server accepts', async () => {
    // The public ready=false flag also represents disabled Provider Models with
    // verified credentials; it cannot identify the credential-only write gate.
    model.bindings[0].ready = false
    await mount()
    expect(total('openai_chat').textContent).toBe('Draft weights total 100%')
    expect(save().disabled).toBe(false)
    expect(container.textContent).toContain('Current routing is unavailable.')
    await submit()
    await until(() => expect(writes()).toHaveLength(1))
    await until(() => expect(save().disabled).toBe(false))
    expect(JSON.parse(writes()[0].data)).toEqual({
      weights: model.bindings.map((binding) => ({
        binding_id: binding.id,
        weight: binding.weight,
      })),
    })
    expect(model.bindings[0]).toMatchObject({ ready: false, weight: 100 })
    expect(container.textContent).toContain('Current routing is unavailable')
    expect(container.textContent).toContain('Unknown')
    expect(writes()).toHaveLength(1)
  })
  it('preserves independent read-only authority and blocks crafted submits without write permission', async () => {
    permissions = ['models.read_all']
    await mount()
    await until(() => expect(save().disabled).toBe(true))
    expect(
      [...container.querySelectorAll<HTMLInputElement>('input[name^="bind_"]')].every(
        (input) => input.disabled,
      ),
    ).toBe(true)
    await submit()
    expect(writes()).toHaveLength(0)
    expect(requests.some((request) => request.url === '/admin/providers')).toBe(false)
  })
  it('fetches no Model details and renders no private routing when read authority is absent', async () => {
    permissions = ['models.write']
    await act(async () => {
      root.render(
        <QueryClientProvider client={cache}>
          <MemoryRouter initialEntries={['/admin/models/mdl_1']}>
            <Routes>
              <Route path="/admin/models/:modelId" element={<AdminModelsPage />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )
    })
    await until(() => expect(container.querySelector('[role="alert"]')).not.toBeNull())
    expect(form()).toBeNull()
    expect(requests.some((request) => request.url?.startsWith('/admin/models/'))).toBe(false)
  })
  it('hides drafts during a real Session renewal and reauthorizes before restoring current routing', async () => {
    await mount()
    await fill('bind_0_0', '60')
    await fill('bind_0_1', '40')
    const originalForm = form()!
    const wait = deferred()
    sessionWait = wait.promise
    let renewal!: Promise<void>
    await act(async () => {
      renewal = cache.refetchQueries({ queryKey: sessionKey })
    })
    await until(() => expect(form()).toBeNull())
    expect(originalForm.isConnected).toBe(false)
    await submit(originalForm)
    expect(writes()).toHaveLength(0)
    await act(async () => {
      wait.resolve()
      await renewal
    })
    await until(() => expect(form()).not.toBeNull())
    expect(total('openai_chat').textContent).toBe('Draft weights total 100%')
    expect(writes()).toHaveLength(0)
    expect(requests.filter((request) => request.url === '/admin/models/mdl_1')).toHaveLength(2)
    expect(container.querySelector<HTMLInputElement>('input[name="bind_0_0"]')!.value).toBe('60')
    expect(container.querySelector<HTMLInputElement>('input[name="bind_0_1"]')!.value).toBe('40')
    await submit()
    await until(() => expect(writes()).toHaveLength(1))
    expect(JSON.parse(writes()[0].data).weights.slice(0, 2)).toEqual([
      { binding_id: 'bind_0_0', weight: 60 },
      { binding_id: 'bind_0_1', weight: 40 },
    ])
  })
  it('hides all routing during permission refresh/error and does not restore write controls after revocation', async () => {
    await mount()
    const wait = deferred()
    permissionWait = wait.promise
    let renewal!: Promise<void>
    await act(async () => {
      renewal = cache.refetchQueries({ queryKey: ['permissions'] })
    })
    await until(() => expect(form()).toBeNull())
    permissionFailure = true
    await act(async () => {
      wait.resolve()
      await renewal
    })
    await until(() => expect(container.querySelector('[role="alert"]')).not.toBeNull())
    expect(form()).toBeNull()
    permissionFailure = false
    permissionWait = null
    permissions = ['models.read_all']
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['permissions'] })
    })
    await until(() => expect(form()).not.toBeNull())
    expect(save().disabled).toBe(true)
    await submit()
    expect(writes()).toHaveLength(0)
  })
  it('destroys the previous actor draft when a real Session read changes actor', async () => {
    await mount()
    await fill('bind_0_0', '60')
    const previous = form()!
    session = { ...session, user: { ...session.user, id: 'usr_2', role: 'member' } }
    permissions = []
    await act(async () => {
      await cache.refetchQueries({ queryKey: sessionKey })
    })
    await until(() => expect(form()).toBeNull())
    expect(previous.isConnected).toBe(false)
    expect(container.textContent).not.toContain('openai_chat-0')
    await submit(previous)
    expect(writes()).toHaveLength(0)
    permissions = ['models.read_all', 'models.write']
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['permissions', 'usr_2'] })
    })
    await until(() => expect(form()).not.toBeNull())
    expect(container.querySelector<HTMLInputElement>('input[name="bind_0_0"]')!.value).toBe('100')
    expect(container.querySelector<HTMLInputElement>('input[name="bind_0_1"]')!.value).toBe('0')
    expect(writes()).toHaveLength(0)
  })
  it('replaces the editor on target change and never submits the departed target', async () => {
    await mount()
    await fill('bind_0_0', '60')
    const previous = form()!
    await act(async () => {
      container.querySelector<HTMLAnchorElement>('a')!.click()
    })
    await until(() =>
      expect(
        container.querySelector('table[aria-label="Second Model OpenAI Chat provider routes"]'),
      ).not.toBeNull(),
    )
    expect(previous.isConnected).toBe(false)
    expect(total('openai_chat').textContent).toBe('Draft weights total 100%')
    expect(container.querySelector<HTMLInputElement>('input[name="bind_0_0"]')!.value).toBe('100')
    expect(container.querySelector<HTMLInputElement>('input[name="bind_0_1"]')!.value).toBe('0')
    await submit(previous)
    expect(writes()).toHaveLength(0)
  })

  it.each(['permissions', 'detail'])(
    'retains exact edited weights through same-actor %s pending/error/recovery without automatic writes',
    async (kind) => {
      await mount()
      await fill('bind_0_0', '60')
      await fill('bind_0_1', '40')
      const old = form()!
      const wait = deferred()
      if (kind === 'permissions') permissionWait = wait.promise
      else detailWait = wait.promise
      let renewal!: Promise<void>
      await act(async () => {
        renewal = cache.refetchQueries({
          queryKey: kind === 'permissions' ? ['permissions'] : ['admin', 'models', 'detail'],
        })
      })
      await until(() => expect(form()).toBeNull())
      expect(old.isConnected).toBe(false)
      await submit(old)
      expect(writes()).toHaveLength(0)
      if (kind === 'permissions') permissionFailure = true
      else detailFailure = true
      await act(async () => {
        wait.resolve()
        await renewal
      })
      expect(form()).toBeNull()
      expect(container.textContent).not.toContain('openai_chat-0')
      permissionWait = detailWait = null
      permissionFailure = detailFailure = false
      await act(async () => {
        await cache.refetchQueries({
          queryKey: kind === 'permissions' ? ['permissions'] : ['admin', 'models', 'detail'],
        })
      })
      await until(() => expect(form()).not.toBeNull())
      expect(container.querySelector<HTMLInputElement>('input[name="bind_0_0"]')!.value).toBe('60')
      expect(container.querySelector<HTMLInputElement>('input[name="bind_0_1"]')!.value).toBe('40')
      expect(writes()).toHaveLength(0)
    },
  )
  it('requires an explicit fresh binding review before replacing a draft after binding removal', async () => {
    await mount()
    await fill('bind_0_0', '60')
    await fill('bind_0_1', '40')
    model.bindings = model.bindings.filter((b) => b.id !== 'bind_0_1')
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['admin', 'models', 'detail'] })
    })
    await until(() => expect(form()).not.toBeNull())
    expect(container.textContent).toContain('The current route identities have changed')
    expect(save().disabled).toBe(true)
    await submit()
    expect(writes()).toHaveLength(0)
    await act(async () => i18n.changeLanguage('zh'))
    expect(container.textContent).toContain('当前路由身份已变更')
    await act(async () => i18n.changeLanguage('en'))
    const review = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent === 'Review current routes',
    )!
    await act(async () => review.click())
    expect(container.querySelector<HTMLInputElement>('input[name="bind_0_0"]')!.value).toBe('100')
    expect(container.querySelector('input[name="bind_0_1"]')).toBeNull()
    expect(document.activeElement).toBe(container.querySelector('input[name="bind_0_0"]'))
    expect(writes()).toHaveLength(0)
    await submit()
    await until(() => expect(writes()).toHaveLength(1))
    expect(JSON.parse(writes()[0].data).weights).toHaveLength(7)
    expect(
      JSON.parse(writes()[0].data).weights.some(
        (b: { binding_id: string }) => b.binding_id === 'bind_0_1',
      ),
    ).toBe(false)
  })
  it('blocks an obsolete save callback when the current binding identity changes before rendering', async () => {
    await mount()
    await fill('bind_0_0', '60')
    await fill('bind_0_1', '40')
    const old = form()!
    const key = cache
      .getQueryCache()
      .findAll({ queryKey: ['admin', 'models', 'detail'] })[0].queryKey
    await act(async () => {
      cache.setQueryData<Model>(key, {
        ...model,
        bindings: model.bindings.map((b, i) =>
          i === 0 ? { ...b, provider_model_id: 'pmd_replaced' } : b,
        ),
      })
      old.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    expect(writes()).toHaveLength(0)
  })
  it('switches live feedback and accessible names to Chinese without losing the weight draft', async () => {
    await mount()
    await fill('bind_0_0', '60')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(container.querySelector('[aria-label="OpenAI Chat 草稿权重合计"]')?.textContent).toBe(
      '草稿权重合计 60%，还需分配 40%',
    )
    expect(
      container.querySelector('table[aria-label="Public Model OpenAI Chat 供应商路由"]'),
    ).not.toBeNull()
    expect(container.querySelector<HTMLInputElement>('input[name="bind_0_0"]')!.value).toBe('60')
    expect(container.textContent).toContain('保存路由权重')
    expect(writes()).toHaveLength(0)
  })
})
