import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { ModelAccessSource, ModelCatalogRecord } from '@/types/model-catalog'
import type { UsageReport, UsageStats } from '@/types/usage'
import { monthlyUsageReport } from './monthly-usage'
import ModelsPage from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const personal: ModelAccessSource = {
  type: 'personal',
  team_id: null,
  team_name: null,
  invocation_supported: true,
}
const team: ModelAccessSource = {
  type: 'team',
  team_id: 'tea_alpha',
  team_name: 'Alpha',
  invocation_supported: false,
}
function stats(requests: number): UsageStats {
  return {
    requests,
    successes: requests,
    errors: 0,
    canceled: 0,
    success_rate: requests ? 1 : null,
    average_duration_ms: requests ? 1 : null,
    tokens: {
      input: { value: '0', known: '0', unknown_calls: 0 },
      output: { value: '0', known: '0', unknown_calls: 0 },
      total: { value: '0', known: '0', unknown_calls: 0 },
    },
    amounts: [],
    unknown_amount_calls: 0,
    pricing_statuses: {},
  }
}
function report(source: ModelAccessSource = personal, count = 7): UsageReport {
  const sum = stats(count)
  return {
    timezone: 'UTC',
    granularity: 'month',
    queried_at: '2026-10-06T05:00:00Z',
    latest_completed_at: null,
    source: 'persisted_call_records',
    may_lag: true,
    ...(source.type === 'team' ? { team_id: source.team_id } : {}),
    available_dimensions: source.type === 'team' ? ['model'] : ['model', 'key'],
    current: {
      from: '2026-10-01T00:00:00Z',
      to: '2026-10-06T05:00:00Z',
      summary: sum,
      trend: [{ start: '2026-10-01T00:00:00Z', end: '2026-11-01T00:00:00Z', stats: sum }],
      models: count ? [{ id: 'mdl_one', unknown: false, stats: sum }] : [],
      keys: [],
    },
  }
}
describe('catalogue monthly complete-report boundary', () => {
  it('accepts one complete Personal or shared Team report without changing it', () => {
    for (const scope of [personal, team]) {
      const value = report(scope)
      expect(monthlyUsageReport(value, scope)).toBe(value)
    }
  })
  it('retains terminal statuses and unknown attribution without assigning an unknown model', () => {
    const value = report()
    const row = value.current.summary
    row.successes = 3
    row.errors = 2
    row.canceled = 2
    row.success_rate = 3 / 7
    value.current.models[0].id = ''
    value.current.models[0].unknown = true
    expect(monthlyUsageReport(value, personal).current.models[0].unknown).toBe(true)
  })
  it.each([
    [
      'non-monthly bucket',
      (value: UsageReport) => {
        value.current.trend[0].end = '2026-10-07T00:00:00Z'
      },
    ],
    [
      'non-UTC',
      (value: UsageReport) => {
        value.timezone = 'Asia/Shanghai'
      },
    ],
    [
      'non-month',
      (value: UsageReport) => {
        value.granularity = 'day'
      },
    ],
    [
      'wrong start',
      (value: UsageReport) => {
        value.current.from = '2026-09-30T23:00:00Z'
      },
    ],
    [
      'not month-to-query',
      (value: UsageReport) => {
        value.queried_at = '2026-10-06T05:00:01Z'
      },
    ],
    [
      'truncated groups',
      (value: UsageReport) => {
        value.current.models = []
      },
    ],
    [
      'negative',
      (value: UsageReport) => {
        value.current.models[0].stats.requests = -1
      },
    ],
    [
      'duplicate model',
      (value: UsageReport) => {
        value.current.models.push(structuredClone(value.current.models[0]))
      },
    ],
    [
      'alias attribution',
      (value: UsageReport) => {
        value.current.models[0].id = 'mdl_one '
      },
    ],
    [
      'unqualified freshness',
      (value: UsageReport) => {
        value.may_lag = false
      },
    ],
    [
      'row overflow',
      (value: UsageReport) => {
        const replacement = report(personal, 10001)
        Object.assign(value, replacement)
      },
    ],
  ])('rejects %s', (_name, mutate) => {
    const value = report()
    mutate(value)
    expect(() => monthlyUsageReport(value, personal)).toThrow()
  })
  it('rejects a different Team, mixed scope and nonempty Team Key projection', () => {
    const value = report(team)
    value.team_id = 'tea_other'
    expect(() => monthlyUsageReport(value, team)).toThrow()
    value.team_id = team.team_id
    value.available_dimensions = ['model', 'key']
    expect(() => monthlyUsageReport(value, team)).toThrow()
    value.available_dimensions = ['model']
    value.current.keys = [{ id: 'key_other', unknown: false, stats: stats(7) }]
    expect(() => monthlyUsageReport(value, team)).toThrow()
  })
  it('uses a server-returned February UTC boundary, not a browser month', () => {
    const value = report()
    value.current.from = '2028-02-01T00:00:00Z'
    value.current.to = value.queried_at = '2028-02-29T23:59:59Z'
    value.current.trend[0].start = value.current.from
    value.current.trend[0].end = '2028-03-01T00:00:00Z'
    expect(monthlyUsageReport(value, personal)).toBe(value)
  })
})

const originalAdapter = client.defaults.adapter
let root: Root, host: HTMLDivElement, cache: QueryClient
let requests: InternalAxiosRequestConfig[],
  actor: string,
  models: ModelCatalogRecord[],
  failures: Record<string, number>
let usage: UsageReport, teamUsage: UsageReport
let heldUsage: { entered: Promise<void>; release: () => void } | undefined
let heldSession: { entered: Promise<void>; release: () => void } | undefined
let heldCatalogue: { entered: Promise<void>; release: () => void } | undefined
function hold(kind: 'usage' | 'session' | 'catalogue') {
  let release!: () => void, entered!: () => void
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  const result = {
    entered: new Promise<void>((resolve) => {
      entered = resolve
    }),
    release,
    gate,
    enter: () => entered(),
  }
  if (kind === 'usage') heldUsage = result
  else if (kind === 'session') heldSession = result
  else heldCatalogue = result
  return result
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_one'
  failures = {}
  requests = []
  heldUsage = undefined
  heldSession = undefined
  heldCatalogue = undefined
  usage = report()
  teamUsage = report(team, 12)
  models = ['one', 'zero'].map((name) => ({
    id: `mdl_${name}`,
    name,
    status: 'active',
    created_at: '2026-10-01T00:00:00Z',
    protocols: ['openai_chat'],
    input_capabilities: {},
    personal_available: true,
    sources: [personal, team],
    input_price: { state: 'unauthorized', rate: null },
    output_price: { state: 'unauthorized', rate: null },
  }))
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    const barrier =
      config.url === '/usage'
        ? heldUsage
        : config.url === '/auth/session'
          ? heldSession
          : config.url === '/model-catalog'
            ? heldCatalogue
            : undefined
    if (barrier) {
      const full = barrier as ReturnType<typeof hold>
      full.enter()
      await full.gate
    }
    const status = failures[config.url!] ?? 200
    if (status !== 200)
      throw new AxiosError('rejected', '', config, undefined, {
        ...response,
        status,
        data: { code: status, message: 'sanitized rejection' },
      })
    if (config.url === '/auth/session')
      response.data = {
        user: { id: actor, name: 'Member', email: 'member@example.com', role: 'member' },
        csrf_token: 'current-csrf',
      }
    else if (config.url === '/model-catalog') response.data = { items: structuredClone(models) }
    else if (config.url === '/usage') response.data = structuredClone(usage)
    else if (config.url === '/teams/tea_alpha/usage') response.data = structuredClone(teamUsage)
    else if (config.url === '/auth/permissions') response.data = { permissions: [] }
    else if (config.url === '/model-access-candidates')
      response.data = { items: [], next_cursor: null }
    else throw new Error('Unexpected scoped read')
    return response
  }
})
afterEach(async () => {
  heldUsage?.release()
  heldSession?.release()
  heldCatalogue?.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  await i18n.changeLanguage('en')
})
async function until(assertion: () => void) {
  for (let i = 0; i < 150; i++) {
    await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
    try {
      assertion()
      return
    } catch (error) {
      if (i === 149) throw error
    }
  }
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter>
          <ModelsPage />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.querySelectorAll('article')).toHaveLength(2))
}
async function source(value: string) {
  const select = host.querySelectorAll<HTMLSelectElement>('select')[0]
  await act(async () => {
    select.value = value
    select.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
function article(id = 'one') {
  return [...host.querySelectorAll('article')].find(
    (row) => row.querySelector('h2')?.textContent === id,
  )!
}
function usageRequests() {
  return requests.filter((row) => row.url?.endsWith('/usage'))
}
async function button(name: string) {
  const item = [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (row) => row.textContent === name,
  )!
  await act(async () => item.click())
}

describe('source-scoped catalogue monthly request facts', () => {
  it.each([
    ['Personal', 'personal', '/usage', 'month: 7'],
    ['Team', 'team:tea_alpha', '/teams/tea_alpha/usage', 'month: 12'],
  ])(
    'refreshes %s usage once only after the fresh catalogue read',
    async (_name, selected, url, oldCount) => {
      await mount()
      await source(selected)
      await until(() => expect(article().textContent).toContain(oldCount))
      const initial = usageRequests().length
      const initialCatalogue = requests.filter((row) => row.url === '/model-catalog').length
      const gate = hold('catalogue')
      usage = report(personal, 17)
      teamUsage = report(team, 17)
      await button('Refresh catalogue')
      await gate.entered
      await until(() => expect(host.querySelectorAll('article')).toHaveLength(0))
      expect(host.textContent).not.toContain(oldCount)
      expect(usageRequests()).toHaveLength(initial)
      expect(requests.filter((row) => row.url === '/model-catalog')).toHaveLength(
        initialCatalogue + 1,
      )
      await act(async () => gate.release())
      heldCatalogue = undefined
      await until(() => expect(article().textContent).toContain('month: 17'))
      expect(usageRequests()).toHaveLength(initial + 1)
      expect(usageRequests().at(-1)?.url).toBe(url)
      expect(host.querySelectorAll('select')[0].value).toBe(selected)
      expect(requests.filter((row) => row.url === '/auth/session')).toHaveLength(1)
    },
  )
  it('refreshes a fast unchanged Team catalogue once and leaves failed usage Unknown', async () => {
    await mount()
    await source('team:tea_alpha')
    await until(() => expect(article().textContent).toContain('month: 12'))
    teamUsage = report(team, 3)
    await button('Refresh catalogue')
    await until(() => expect(article().textContent).toContain('month: 3'))
    expect(usageRequests()).toHaveLength(2)
    failures['/teams/tea_alpha/usage'] = 503
    await button('Refresh catalogue')
    await until(() => expect(article().textContent).toContain('month: Unknown'))
    await until(() => expect(usageRequests()).toHaveLength(3))
    expect(article().textContent).not.toContain('month: 3')
    expect(host.textContent).not.toContain('may lag')
    expect(requests.filter((row) => row.url === '/auth/session')).toHaveLength(1)
  })
  it('does not refresh usage when the fresh catalogue read fails', async () => {
    await mount()
    await source('personal')
    await until(() => expect(article().textContent).toContain('month: 7'))
    const gate = hold('catalogue')
    failures['/model-catalog'] = 503
    await button('Refresh catalogue')
    await gate.entered
    expect(usageRequests()).toHaveLength(1)
    await act(async () => gate.release())
    heldCatalogue = undefined
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(host.querySelectorAll('article')).toHaveLength(0)
    expect(host.textContent).not.toContain('month: 7')
    expect(usageRequests()).toHaveLength(1)
  })

  it('keeps All/requestable unknown and reads one complete Personal report for every row', async () => {
    await mount()
    expect(usageRequests()).toHaveLength(0)
    expect(article().textContent).toContain('Monthly requests: Unknown')
    await source('personal')
    await until(() =>
      expect(article().textContent).toContain('Your Personal requests this month: 7'),
    )
    expect(article('zero').textContent).toContain('Your Personal requests this month: 0')
    expect(usageRequests()).toHaveLength(1)
    expect(usageRequests()[0]).toMatchObject({
      url: '/usage',
      params: { period: 'month', timezone: 'UTC', granularity: 'month', compare: false },
    })
    expect(host.textContent).toContain('Oct 1, 2026')
    expect(host.textContent).toContain('UTC')
    expect(host.textContent).toContain('may lag')
    expect(host.textContent).toContain('Queried Oct 6, 2026')
    expect(host.textContent).not.toContain('{{')
    await source('requestable')
    await until(() => expect(host.querySelectorAll('article')).toHaveLength(0))
    expect(usageRequests()).toHaveLength(1)
    expect(requests.some((row) => row.url?.startsWith('/admin/') || row.url === '/teams')).toBe(
      false,
    )
  })
  it('selects one exact shared Team report, never sums Personal and Team, and preserves live language/filter/table', async () => {
    await mount()
    await source('personal')
    await until(() => expect(article().textContent).toContain('month: 7'))
    await source('team:tea_alpha')
    await until(() =>
      expect(article().textContent).toContain('Shared Team requests this month: 12'),
    )
    expect(host.textContent).toContain('Alpha ·')
    expect(usageRequests()).toHaveLength(2)
    await act(async () => i18n.changeLanguage('zh'))
    expect(article().textContent).toContain('Team 共享本月请求: 12')
    expect(host.textContent).toContain('查询于')
    expect(host.textContent).not.toContain('{{')
    expect(host.querySelectorAll('select')[0].value).toBe('team:tea_alpha')
    await button('表格')
    await until(() => expect(host.querySelector('tbody')?.textContent).toContain('12'))
    expect(host.querySelector('thead')?.textContent).toContain('Team 共享本月请求')
    expect(usageRequests()).toHaveLength(2)
  })
  it.each([403, 422, 503])(
    'shows Unknown for rejected/overflow read %s without hiding fresh catalogue prices',
    async (status) => {
      await mount()
      failures['/usage'] = status
      await source('personal')
      await until(() =>
        expect(
          cache.getQueryCache().findAll({ queryKey: ['model-catalog-monthly-usage'] })[0]?.state
            .status,
        ).toBe('error'),
      )
      expect(article().textContent).toContain('month: Unknown')
      expect(article('zero').textContent).toContain('month: Unknown')
      expect(host.textContent).not.toContain('may lag')
    },
  )
  it('rejects a malformed or incomplete report instead of zero', async () => {
    await mount()
    usage.current.models = []
    await source('personal')
    await until(() =>
      expect(
        cache.getQueryCache().findAll({ queryKey: ['model-catalog-monthly-usage'] })[0]?.state
          .status,
      ).toBe('error'),
    )
    expect(article('zero').textContent).toContain('month: Unknown')
  })
  it('ignores a late old-source reply even when the transport does not cancel', async () => {
    await mount()
    const gate = hold('usage')
    await source('personal')
    await gate.entered
    await source('team:tea_alpha')
    await until(() => expect(article().textContent).toContain('month: 12'))
    await act(async () => gate.release())
    expect(article().textContent).toContain('month: 12')
    expect(article().textContent).not.toContain('month: 7')
  })
  it('hides old counts through an in-flight Session renewal and only displays the fresh same-actor read', async () => {
    await mount()
    await source('personal')
    await until(() => expect(article().textContent).toContain('month: 7'))
    const gate = hold('session')
    let renewal!: Promise<unknown>
    await act(async () => {
      renewal = cache.refetchQueries({ queryKey: ['auth', 'session'] })
    })
    await gate.entered
    await until(() => expect(host.textContent).not.toContain('month: 7'))
    usage = report(personal, 3)
    await act(async () => {
      gate.release()
      await renewal
    })
    heldSession = undefined
    await until(() => expect(article().textContent).toContain('month: 3'))
    expect(usageRequests()).toHaveLength(2)
  })
  it('rejects a late previous actor read and removes a no-longer-authorized source', async () => {
    await mount()
    const gate = hold('usage')
    await source('personal')
    await gate.entered
    actor = 'usr_two'
    models = models.map((row) => ({ ...row, sources: [team], personal_available: false }))
    await act(async () => cache.refetchQueries({ queryKey: ['auth', 'session'] }))
    await until(() => expect(host.querySelectorAll('option[value="personal"]')).toHaveLength(0))
    await act(async () => gate.release())
    expect(host.textContent).not.toContain('month: 7')
    expect(usageRequests()).toHaveLength(1)
  })
  it('hides counts when same-actor Session renewal fails and does not infer zero', async () => {
    await mount()
    await source('personal')
    await until(() => expect(article().textContent).toContain('month: 7'))
    failures['/auth/session'] = 503
    await act(async () => cache.refetchQueries({ queryKey: ['auth', 'session'] }))
    await until(() => expect(host.querySelectorAll('article')).toHaveLength(0))
    expect(host.textContent).not.toContain('month: 7')
    expect(host.textContent).not.toContain('month: 0')
  })
  it('drops Team usage when a fresh catalogue removes that source without actor change', async () => {
    await mount()
    await source('team:tea_alpha')
    await until(() => expect(article().textContent).toContain('month: 12'))
    models = models.map((row) => ({ ...row, sources: [personal] }))
    await act(async () => cache.refetchQueries({ queryKey: ['model-catalog', 'list'] }))
    await until(() =>
      expect(host.querySelectorAll('option[value="team:tea_alpha"]')).toHaveLength(0),
    )
    expect(host.textContent).not.toContain('month: 12')
    expect(host.textContent).not.toContain('month: 0')
  })
  it('shows zero from a complete empty report and retries usage only on explicit catalogue refresh', async () => {
    await mount()
    usage = report(personal, 0)
    await source('personal')
    await until(() => expect(article().textContent).toContain('month: 0'))
    usage = report(personal, 4)
    await button('Refresh catalogue')
    await until(() => expect(article().textContent).toContain('month: 4'))
    expect(usageRequests()).toHaveLength(2)
  })
  it('retains zero only from a valid complete empty report and never maps unknown historical attribution', async () => {
    await mount()
    usage.current.models[0].id = ''
    usage.current.models[0].unknown = true
    await source('personal')
    await until(() => expect(article().textContent).toContain('month: 0'))
    expect(article('zero').textContent).toContain('month: 0')
  })
})
