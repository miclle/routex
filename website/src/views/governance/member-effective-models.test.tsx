import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import MemberEffectiveModels from './member-effective-models'
import { effectiveModelsPage } from './member-effective-models.fixture'
import type { Session } from '@/types/auth'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
let root: Root, host: HTMLDivElement, cache: QueryClient
let actor: string,
  target: string,
  generation: number,
  permissions: string[],
  requests: InternalAxiosRequestConfig[],
  failure: number,
  name: string
let gate: Gate | undefined, gates: Gate[]
let project: ((page: ReturnType<typeof effectiveModelsPage>) => void) | undefined
type Gate = { promise: Promise<void>; release: () => void }
function deferred() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  const value = { promise, release }
  gates.push(value)
  return value
}
const session = (): Session => ({
  user: { id: actor, name: 'Reader', email: 'reader@example.invalid', role: 'member' },
  csrf_token: 'current-csrf',
})
const subjectKey = () => ['admin', 'member', actor, target, generation]
function seed() {
  cache.setQueryData(['auth', 'session'], session())
  cache.setQueryData(['permissions', actor], [...permissions])
  cache.setQueryData(subjectKey(), {
    id: target,
    name: 'Subject',
    email: 'subject@example.invalid',
    role: 'member',
    role_ids: [],
    disabled: false,
    created_at: '2026-10-01T00:00:00Z',
  })
}
beforeEach(async () => {
  actor = 'usr_reader'
  target = 'usr_target'
  generation = 1
  permissions = ['members.read', 'teams.read_all', 'providers.read', 'prices.read']
  project = undefined
  failure = 0
  gate = undefined
  gates = []
  requests = []
  name = 'controlled-model'
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  seed()
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  await i18n.changeLanguage('en')
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const current = cache.getQueryData<string[]>(['permissions', actor]) ?? []
    const page = effectiveModelsPage(config.url!.split('/')[3], {
      teams: current.includes('teams.read_all'),
      providers: current.includes('providers.read'),
      prices: current.includes('prices.read'),
    })
    page.items[0].name = name
    project?.(page)
    const wait = gate
    if (wait) await wait.promise
    if (failure)
      throw new AxiosError('Controlled unavailable', '', config, undefined, {
        config,
        status: failure,
        statusText: '',
        headers: new AxiosHeaders(),
        data: { message: 'Unavailable' },
      })
    return {
      config,
      data: page,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ 'cache-control': 'private, no-store' }),
    }
  }
})
afterEach(async () => {
  gates.forEach((g) => g.release())
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
})
const until = (fn: () => void) =>
  vi.waitFor(
    async () => {
      await act(async () => {})
      fn()
    },
    { timeout: 4000, interval: 10 },
  )
async function render(ready = true) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemberEffectiveModels
          actor={actor}
          target={target}
          generation={generation}
          ready={ready}
          targetQueryKey={subjectKey()}
        />
      </QueryClientProvider>,
    ),
  )
}
async function mount() {
  await render()
  await until(() => expect(host.textContent).toContain(name))
}
const reads = () => requests.filter((r) => r.url?.endsWith('/effective-models'))
const absent = () => {
  expect(document.body.textContent).not.toContain('Recorded Team')
  expect(host.querySelector('tbody')).toBeNull()
  expect(document.querySelector('[role=tooltip]')).toBeNull()
}
it('renders ten readonly columns, exact prices, all native protocols and no directory/Session observers', async () => {
  await mount()
  expect([...host.querySelectorAll('thead th')].map((v) => v.textContent)).toEqual([
    'Model',
    'Authorization source',
    'Type',
    'Provider',
    'Protocol type',
    'Availability',
    'Input price',
    'Output price',
    'Created',
    'Updated',
  ])
  expect(host.textContent).toContain('Personal grant')
  expect(host.textContent).toContain('Recorded Team')
  expect(host.textContent).toContain('0 USD / 1M Tokens')
  expect(host.textContent).toContain('0.123456789012345678 EUR / 1M Tokens')
  for (const p of [
    'openai_chat',
    'openai_responses',
    'anthropic_messages',
    'gemini_generate_content',
  ])
    expect(host.textContent).toContain(p)
  expect(host.querySelectorAll('tbody td')).toHaveLength(10)
  expect(host.querySelectorAll('tbody td')[2].textContent).toBe('Unknown')
  expect(host.querySelectorAll('tbody td')[9].textContent).toBe('Unknown')
  expect(requests.every((r) => r.method === 'get' && r.url?.endsWith('/effective-models'))).toBe(
    true,
  )
  expect(reads()).toHaveLength(1)
  expect(
    cache
      .getQueryCache()
      .find({ queryKey: ['auth', 'session'] })
      ?.getObserversCount(),
  ).toBe(0)
  expect(host.querySelectorAll('input, select')).toHaveLength(0)
  expect(host.textContent).not.toContain('Save')
  expect(host.textContent).not.toContain('Add')
})
it('renders Personal-only unknown completeness without Team/provider/price overlap', async () => {
  permissions = ['members.read']
  seed()
  await mount()
  expect(host.textContent).toContain('Personal grant')
  expect(host.textContent).toContain('complete authorization union is unknown')
  for (const value of [
    'Recorded Team',
    'openai_responses',
    'Recorded Provider',
    '0.123456789012345678',
  ])
    expect(host.textContent).not.toContain(value)
})
it.each([{ values: [] }, { values: ['teams.read_all'] }, { values: ['members.models.write'] }])(
  'requires independent members.read: %j',
  async ({ values }) => {
    permissions = values
    seed()
    await render()
    absent()
    expect(reads()).toHaveLength(0)
  },
)
it('switches translations without changing recorded names/decimals or reloading', async () => {
  await mount()
  const count = reads().length
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  for (const value of ['有效模型', '授权来源', '个人授权', 'Recorded Team', '0.123456789012345678'])
    expect(host.textContent).toContain(value)
  expect(reads()).toHaveLength(count)
})
it.each(['session', 'permissions', 'target'] as const)(
  'hides private rows and open portals during renewed %s and errors',
  async (kind) => {
    await mount()
    const trigger = host.querySelector<HTMLButtonElement>(
      '[aria-label="Authorization source: Recorded Team"]',
    )!
    await act(async () => trigger.focus())
    await until(() => expect(document.querySelector('[role=tooltip]')).not.toBeNull())
    const hold = deferred(),
      key =
        kind === 'session'
          ? ['auth', 'session']
          : kind === 'permissions'
            ? ['permissions', actor]
            : subjectKey()
    let pending!: Promise<unknown>
    await act(async () => {
      pending = cache
        .fetchQuery({
          queryKey: key,
          queryFn: async () => {
            await hold.promise
            throw new Error('Controlled renewed failure')
          },
        })
        .catch(() => null)
    })
    absent()
    hold.release()
    await act(async () => {
      await pending
    })
    absent()
  },
)
it('Team permission loss hides old sources before restoring fresh Personal-only rows', async () => {
  await mount()
  const count = reads().length
  gate = deferred()
  await act(async () =>
    cache.setQueryData(['permissions', actor], ['members.read', 'providers.read', 'prices.read']),
  )
  absent()
  await until(() => expect(reads().length).toBeGreaterThan(count))
  const hold = gate
  gate = undefined
  hold.release()
  await until(() => expect(host.textContent).toContain('controlled-model'))
  expect(host.textContent).not.toContain('Recorded Team')
  expect(host.textContent).not.toContain('openai_responses')
  expect(host.textContent).toContain('complete authorization union is unknown')
})
it.each(['providers.read', 'prices.read'])(
  'optional %s loss hides old facts before redacted restore',
  async (removed) => {
    await mount()
    const count = reads().length
    gate = deferred()
    await act(async () =>
      cache.setQueryData(
        ['permissions', actor],
        permissions.filter((p) => p !== removed),
      ),
    )
    absent()
    await until(() => expect(reads().length).toBeGreaterThan(count))
    const hold = gate
    gate = undefined
    hold.release()
    await until(() => expect(host.textContent).toContain('controlled-model'))
    expect(host.textContent).not.toContain(
      removed === 'providers.read' ? 'Recorded Provider' : '0.123456789012345678',
    )
  },
)
it('same-millisecond Session successes fence old facts by cache generation', async () => {
  await mount()
  const before = cache.getQueryState(['auth', 'session'])!,
    count = reads().length
  gate = deferred()
  await act(async () =>
    cache.setQueryData(['auth', 'session'], session(), { updatedAt: before.dataUpdatedAt }),
  )
  expect(cache.getQueryState(['auth', 'session'])!.dataUpdatedAt).toBe(before.dataUpdatedAt)
  expect(cache.getQueryState(['auth', 'session'])!.dataUpdateCount).toBeGreaterThan(
    before.dataUpdateCount,
  )
  absent()
  await until(() => expect(reads().length).toBeGreaterThan(count))
  const hold = gate
  gate = undefined
  hold.release()
  await until(() => expect(host.textContent).toContain('controlled-model'))
})
it('old actor/target replies cannot restore data after resource changes', async () => {
  gate = deferred()
  await render()
  await until(() => expect(reads()).toHaveLength(1))
  const old = reads()[0],
    hold = gate
  gate = undefined
  actor = 'usr_other'
  target = 'usr_second'
  name = 'new-authorized-model'
  seed()
  await render()
  await until(() => expect(host.textContent).toContain('new-authorized-model'))
  hold.release()
  await act(async () => {})
  expect(host.textContent).not.toContain('controlled-model')
  expect(old.signal?.aborted).toBe(true)
})
it('own refresh/error hides cached rows and obsolete tooltip trigger cannot reopen', async () => {
  await mount()
  const trigger = host.querySelector<HTMLButtonElement>(
    '[aria-label="Authorization source: Recorded Team"]',
  )!
  gate = deferred()
  let pending!: Promise<void>
  await act(async () => {
    pending = cache.invalidateQueries({ queryKey: ['admin', 'member-effective-models'] })
  })
  absent()
  await act(async () => trigger.focus())
  absent()
  failure = 503
  const hold = gate
  gate = undefined
  hold.release()
  await act(async () => {
    await pending
  })
  absent()
})
it('parent denial and tab/unmount teardown abort requests and remove private content', async () => {
  await mount()
  await render(false)
  absent()
  gate = deferred()
  const count = reads().length
  await render()
  await until(() => expect(reads().length).toBeGreaterThan(count))
  const read = reads().at(-1)!
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <p>Other tab</p>
      </QueryClientProvider>,
    ),
  )
  expect(read.signal?.aborted).toBe(true)
  gate.release()
  await act(async () => {})
  absent()
})

it('keeps same-named Team source badges distinct and preserves mixed source availability', async () => {
  project = (page) => {
    const row = page.items[0]
    row.sources[0].availability = 'unknown'
    row.sources[0].protocols = []
    row.sources.push({ ...row.sources[1], team_id: 'tea_two' })
    row.protocols = [...row.sources[1].protocols]
  }
  await mount()
  expect(host.querySelectorAll('[aria-label="Authorization source: Recorded Team"]')).toHaveLength(
    2,
  )
  expect(host.textContent).not.toContain('openai_chat')
  expect(host.textContent).toContain('Available')
})
it('unknown/inactive advisory facts stay unknown and empty results fabricate no sources', async () => {
  project = (page) => {
    page.subject_status = 'offboarded'
    const row = page.items[0]
    row.availability = 'unavailable'
    row.protocols = []
    row.sources.forEach((source) => {
      source.availability = 'unavailable'
      source.protocols = []
    })
    row.input_price = row.output_price = { state: 'unavailable', rate: null }
  }
  await mount()
  expect(host.textContent).toContain('The member is inactive')
  expect(host.textContent).not.toContain('0.123456789012345678')
  expect(host.textContent).not.toContain('openai_chat')
  project = (page) => {
    page.items = []
  }
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['admin', 'member-effective-models'] })
  })
  await until(() => expect(host.querySelectorAll('tbody tr')).toHaveLength(0))
  expect(host.textContent).not.toContain('Recorded Team')
})
it.each(['missing', 'heterogeneous', 'disabled'] as const)(
  'shows the actual %s price state without rounding or invented schedules',
  async (state) => {
    project = (page) => {
      page.items[0].output_price =
        state === 'disabled'
          ? { state, rate: { amount: '0.000000000000000001', unit: '1M_TOKEN', currency: 'USD' } }
          : { state, rate: null }
    }
    await mount()
    expect(host.textContent).toContain(
      state === 'missing'
        ? 'Not configured'
        : state === 'heterogeneous'
          ? 'Multiple schedules'
          : '0.000000000000000001 USD / 1M Tokens',
    )
    if (state === 'disabled') expect(host.textContent).toContain('Disabled')
  },
)
