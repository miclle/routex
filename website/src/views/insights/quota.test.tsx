import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import InsightsPage from './index'
import {
  keyQuotaInput,
  quotaActor,
  quotaFixture,
  quotaProject,
  teamQuotaInput,
} from './quota-fixture'
import type { InsightsReportDefinition } from '@/types/insights'
import type { QuotaInsightsReport } from '@/types/insights-quota'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
interface DeferredGate {
  promise: Promise<void>
  release: () => void
}
let root: Root, host: HTMLDivElement, cache: QueryClient
let actor: string,
  permissions: string[],
  requests: InternalAxiosRequestConfig[],
  downloads: string[],
  gates: DeferredGate[],
  reportGate: DeferredGate | undefined,
  csvGate: DeferredGate | undefined,
  targetGate: DeferredGate | undefined,
  permissionGate: DeferredGate | undefined,
  sessionGate: DeferredGate | undefined
let definitions: InsightsReportDefinition[], saveStatus: number, saveGate: DeferredGate | undefined
const savedID = 'isr_01arz3ndektsv4rrffq69g5fav'
let aiModels: boolean, aiGate: DeferredGate | undefined, aiStatus: number
let scopeOptionsAvailable: boolean, scopeOptionsGate: DeferredGate | undefined
let reportStatus: number,
  targetStatus: number,
  csvStatus: number,
  mutation: (r: QuotaInsightsReport) => void
const original = client.defaults.adapter
const text = (key: string) => i18n.t('insights:' + key)
function deferred(): DeferredGate {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  const gate = { promise, release }
  gates.push(gate)
  return gate
}
function failure(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('private-secret', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: { message: 'private-secret' },
  })
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  aiModels = false
  aiGate = undefined
  aiStatus = 0
  scopeOptionsAvailable = true
  scopeOptionsGate = undefined
  actor = quotaActor
  permissions = ['calls.read_all', 'members.read']
  requests = []
  downloads = []
  gates = []
  reportGate = csvGate = targetGate = permissionGate = sessionGate = undefined
  definitions = []
  saveStatus = 0
  saveGate = undefined
  reportStatus = targetStatus = csvStatus = 0
  mutation = () => {}
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:quota'), revokeObjectURL: vi.fn() })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    downloads.push(this.download)
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown,
      mime = 'application/json'
    if (config.url === '/insights/reports' && config.method === 'post') {
      const parameters = { ...JSON.parse(config.data) }
      delete parameters.request_id
      data = { ...parameters, id: savedID, created_at: '2026-10-01T00:00:00Z' }
      if (saveGate) await saveGate.promise
      if (saveStatus) throw failure(config, saveStatus)
      definitions = [data as InsightsReportDefinition]
    } else if (config.url === '/insights/reports')
      data = { items: [...definitions], next_cursor: '' }
    else if (config.url === '/insights/reports/' + savedID && config.method === 'delete') {
      definitions = []
      return { config, data: '', status: 204, statusText: '', headers: new AxiosHeaders() }
    } else if (config.url === '/auth/session') {
      data = { user: { id: actor, role: 'member' }, csrf_token: 'csrf' }
      if (sessionGate) await sessionGate.promise
    } else if (config.url === '/auth/permissions') {
      data = { permissions: [...permissions] }
      if (permissionGate) await permissionGate.promise
    } else if (config.url === '/admin/insights/models')
      data = {
        items: aiModels
          ? [{ model_id: 'mdl_01arz3ndektsv4rrffq69g5fav', name: 'Personal quota analyst' }]
          : [],
      }
    else if (config.url === '/admin/insights/analyze') {
      const input = JSON.parse(config.data)
      if (aiGate) await aiGate.promise
      if (aiStatus) throw failure(config, aiStatus)
      const keyAdvice = input.report === 'key_quota_tokens' || input.report === 'key_quota_money'
      const dimension = input.report.endsWith('_money') ? 'money' : 'tokens'
      const report = quotaFixture(
        keyAdvice
          ? { ...keyQuotaInput, dimension, scope: { kind: input.scope_kind, id: input.scope_id } }
          : { ...teamQuotaInput, dimension },
      )
      const capture = {
        source: 'local_quota_journal',
        queried_at: '2026-10-14T12:00:00Z',
        cursor: input.cursor,
        limit: input.limit,
        has_more: true,
        timezone: report.time_zone,
        as_of: report.as_of,
        coverage_start: report.coverage_start,
        baseline_from: report.baseline_from,
        baseline_to: report.baseline_to,
        month_from: report.month_from,
        month_reset: report.month_reset,
        baseline_basis: report.baseline_basis,
        ...(dimension === 'money' ? { dimension, currency: report.currency } : {}),
      }
      data = {
        request_id: 'req_01arz3ndektsv4rrffq69g5fav',
        completion: 'completed',
        text: '<script>unsafe</script> ' + (keyAdvice ? 'K1' : 'T1') + ' current-page advice',
        ...(keyAdvice
          ? {
              key_quota_capture: {
                ...capture,
                scope_kind: input.scope_kind,
                scope_id: input.scope_id,
              },
            }
          : { quota_capture: capture }),
        labels: [
          keyAdvice
            ? { label: 'K1', key_id: report.items[0].id, name: '<img src=key>', unknown: false }
            : { label: 'T1', team_id: report.items[0].id, name: '<img src=team>', unknown: false },
        ],
      }
    } else if (config.url === '/admin/members') {
      data = {
        items: scopeOptionsAvailable ? [{ id: quotaActor, name: 'Reviewed member' }] : [],
        next_cursor: null,
      }
      if (scopeOptionsGate) await scopeOptionsGate.promise
    } else if (config.url === '/projects' || config.url === '/admin/projects') {
      data = {
        items: scopeOptionsAvailable ? [{ id: quotaProject, name: 'Reviewed Project' }] : [],
        next_cursor: null,
      }
      if (scopeOptionsGate) await scopeOptionsGate.promise
    } else if (
      config.url === '/admin/members/' + quotaActor ||
      config.url === '/projects/' + quotaProject
    ) {
      if (targetGate) await targetGate.promise
      if (targetStatus) throw failure(config, targetStatus)
      data = {
        id: config.url.includes('members') ? quotaActor : quotaProject,
        managers: [{ user_id: actor }],
      }
    } else if (config.url?.endsWith('/export.csv')) {
      if (csvGate) await csvGate.promise
      if (csvStatus) throw failure(config, csvStatus)
      data = new Blob(['id,local_cap\nreviewed,100'])
      mime = 'text/csv; charset=utf-8'
    } else if (config.url?.startsWith('/admin/insights/quota/')) {
      const input = config.url.endsWith('teams')
        ? { ...teamQuotaInput, dimension: config.params.dimension }
        : {
            ...keyQuotaInput,
            dimension: config.params.dimension,
            scope: { kind: config.params.scope_kind, id: config.params.scope_id },
          }
      const report = quotaFixture(input)
      mutation(report)
      if (reportGate) await reportGate.promise
      if (reportStatus) throw failure(config, reportStatus)
      data = report
    } else throw new Error('Unexpected request ' + config.url)
    return {
      config,
      data,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ 'content-type': mime }),
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
  vi.unstubAllGlobals()
  await i18n.changeLanguage('en')
})
async function until(assertion: () => void) {
  for (let n = 0; n < 100; n++) {
    try {
      assertion()
      return
    } catch {
      await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
    }
  }
  assertion()
}
function button(key: string) {
  const found = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent?.trim() === text(key),
  )
  if (!found) throw new Error('Missing ' + key)
  return found
}
async function click(key: string) {
  await act(async () => button(key).click())
}
async function select(key: string, value: string) {
  const field = host.querySelector<HTMLSelectElement>('select[aria-label="' + text(key) + '"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(field, value)
    field.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <InsightsPage />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain(text('assistantTitle')))
}
const reports = () =>
  requests.filter(
    (r) => r.url?.startsWith('/admin/insights/quota/') && !r.url.endsWith('/export.csv'),
  )
async function ready() {
  await until(() => expect(host.textContent).toContain('Reviewed quota account'))
}
function advice() {
  return host.querySelector<HTMLElement>('section[aria-label="' + text('quotaAdviceTitle') + '"]')!
}
function observation(key: string, count: number) {
  return i18n.t('insights:' + key, { count })
}
async function member() {
  await click('shortcutKey')
  await until(() => expect(host.textContent).toContain('Reviewed member'))
  await select('quotaSelectedScope', quotaActor)
  await until(() => expect(button('quotaRun').disabled).toBe(false))
}
function targetKey() {
  return cache
    .getQueryCache()
    .getAll()
    .find((q) => q.queryKey[0] === 'insights-quota-target' && q.queryKey[4] === quotaActor)!
    .queryKey
}
it('keeps the Mockup layout, requires explicit Team run and displays only bounded server facts', async () => {
  await mount()
  expect(reports()).toHaveLength(0)
  expect(requests.some((r) => r.url === '/admin/members' || r.url === '/projects')).toBe(false)
  await click('shortcutTeam')
  expect(reports()).toHaveLength(0)
  expect(button('saveReport').disabled).toBe(false)
  expect(host.textContent).not.toContain(text('aiTitle'))
  await click('quotaRun')
  await ready()
  expect(reports()).toHaveLength(1)
  expect(reports()[0].params).toEqual({ dimension: 'tokens', limit: 20 })
  expect(host.querySelectorAll('thead th')).toHaveLength(4)
  expect(host.textContent).toContain('80.00%')
  expect(host.textContent).toContain(text('quotaPageScope'))
  expect(host.textContent).toContain(text('quotaForecastDefinition'))
  expect(host.textContent).toContain(text('quotaHolds'))
  expect(host.querySelector('[role="img"]')?.getAttribute('aria-label')).toContain('80.00%')
  expect(requests.some((r) => r.url?.includes('/ai'))).toBe(false)
})
it('preserves exact money and distinguishes zero, missing cap, unknown coverage and holds without fabricating forecasts', async () => {
  mutation = (r) => {
    r.items[0] = {
      ...r.items[0],
      local_cap: '0',
      month_settled: '9007199254740993.123456789012345678',
      active_held: null,
      month_unknown: '1',
      baseline_unknown: '1',
      baseline_covered: false,
      utilization_percent: null,
      level: 'unknown',
      estimate_at: null,
      estimate_within_seven_days: false,
      estimate_status: 'zero_cap',
    }
  }
  await mount()
  await click('shortcutTeam')
  await select('quotaDimension', 'money')
  await click('quotaRun')
  await ready()
  expect(host.textContent).toContain('9007199254740993.123456789012345678 USD')
  expect(host.textContent).toContain(text('quotaEstimate_zero_cap'))
  expect(host.textContent).toContain(text('quotaUnknown'))
  expect(host.textContent).toContain('0 USD')
  mutation = (r) => {
    r.items[0] = {
      ...r.items[0],
      local_cap: null,
      estimate_at: null,
      estimate_within_seven_days: false,
      estimate_status: 'no_local_cap',
    }
  }
  await click('quotaRefresh')
  await until(() => expect(host.textContent).toContain(text('quotaNoLocalCap')))
  expect(host.textContent).not.toContain('9007199254740993.123456789012345678')
})
it('uses bounded Member scope, exact review and root accounts without a global Key directory', async () => {
  await mount()
  await member()
  expect(reports()).toHaveLength(0)
  await click('quotaRun')
  await ready()
  expect(reports()[0].params).toEqual({
    dimension: 'tokens',
    scope_kind: 'member',
    scope_id: quotaActor,
    limit: 20,
  })
  expect(
    requests.filter((r) => r.url === '/admin/members/' + quotaActor).length,
  ).toBeGreaterThanOrEqual(2)
  expect(host.textContent).toContain(i18n.t('insights:quotaSharedKeys', { count: 2 }))
  expect(requests.some((r) => r.url === '/keys' || r.url === '/admin/keys')).toBe(false)
})
it.each([false, true])(
  'uses scoped Project options unless both global read and write authorize the directory (%s)',
  async (admin) => {
    permissions = ['calls.read_all', ...(admin ? ['projects.write', 'projects.read_all'] : [])]
    await mount()
    await click('shortcutKey')
    await until(() => expect(host.textContent).toContain('Reviewed Project'))
    expect(requests.some((r) => r.url === (admin ? '/admin/projects' : '/projects'))).toBe(true)
    expect(requests.some((r) => r.url === '/admin/members')).toBe(false)
    expect(host.querySelector<HTMLOptionElement>('option[value="member"]')!.disabled).toBe(true)
    await select('quotaSelectedScope', quotaProject)
    await until(() => expect(button('quotaRun').disabled).toBe(false))
    await click('quotaRun')
    await ready()
    expect(reports()[0].params.scope_kind).toBe('project')
    expect(reports()[0].params.scope_id).toBe(quotaProject)
  },
)
it('localizes an existing result and controls without replaying or changing the reviewed dimension', async () => {
  await mount()
  await click('shortcutTeam')
  await click('quotaRun')
  await ready()
  const count = reports().length
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(host.textContent).toContain(text('quotaPageScope'))
  expect(host.textContent).toContain(text('quotaEstimate_estimated'))
  expect(host.querySelector<HTMLSelectElement>('select')!.value).toBe('tokens')
  expect(reports()).toHaveLength(count)
})
it.each([403, 404, 422, 503])(
  'shows sanitized failures %i and clears old facts',
  async (status) => {
    await mount()
    await click('shortcutTeam')
    await click('quotaRun')
    await ready()
    reportStatus = status
    await click('quotaRefresh')
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(host.textContent).not.toContain('Reviewed quota account')
    expect(host.textContent).not.toContain('private-secret')
    expect(reports()).toHaveLength(2)
  },
)
it('cancels duplicates and discards old Team replies after dimension changes', async () => {
  await mount()
  await click('shortcutTeam')
  reportGate = deferred()
  await click('quotaRun')
  await until(() => expect(reports()).toHaveLength(1))
  await click('quotaRun')
  expect(reports()).toHaveLength(1)
  await select('quotaDimension', 'money')
  reportGate.release()
  await act(async () => new Promise((resolve) => setTimeout(resolve, 10)))
  expect(host.textContent).not.toContain('Reviewed quota account')
  expect(button('exportCSV').disabled).toBe(true)
})
it('hides Key facts and rejects late replies while exact scope authority renews or fails', async () => {
  await mount()
  await member()
  await click('quotaRun')
  await ready()
  targetGate = deferred()
  targetStatus = 403
  let refresh!: Promise<unknown>
  await act(async () => {
    refresh = cache.refetchQueries({ queryKey: targetKey(), exact: true })
  })
  expect(host.textContent).not.toContain('Reviewed quota account')
  expect(button('exportCSV').disabled).toBe(true)
  targetGate.release()
  await act(async () => {
    await refresh
  })
  expect(host.textContent).toContain(text('quotaDenied'))
  expect(host.textContent).not.toContain('Reviewed quota account')
})
it('does not restore a pending Key report after a newer successful target generation', async () => {
  await mount()
  await member()
  reportGate = deferred()
  await click('quotaRun')
  await until(() => expect(reports()).toHaveLength(1))
  await act(async () => {
    await cache.refetchQueries({ queryKey: targetKey(), exact: true })
  })
  reportGate.release()
  await act(async () => new Promise((resolve) => setTimeout(resolve, 10)))
  expect(host.textContent).not.toContain('Reviewed quota account')
  expect(button('exportCSV').disabled).toBe(true)
})
it.each(['permissions', 'session', 'actor', 'unmount'])(
  'blocks a stale private report after %s changes',
  async (change) => {
    await mount()
    await click('shortcutTeam')
    reportGate = deferred()
    await click('quotaRun')
    await until(() => expect(reports()).toHaveLength(1))
    if (change === 'permissions') {
      permissions = []
      await act(async () => {
        await cache.refetchQueries({ queryKey: ['permissions', quotaActor, 'insights'] })
      })
    } else if (change === 'session') {
      sessionGate = deferred()
      await act(async () => {
        void cache.refetchQueries({ queryKey: sessionKey, exact: true })
      })
    } else if (change === 'actor') {
      actor = 'usr_01arz3ndektsv4rrffq69g5faw'
      await act(async () => {
        await cache.refetchQueries({ queryKey: sessionKey, exact: true })
      })
    } else await act(async () => root.render(null))
    reportGate.release()
    await act(async () => new Promise((resolve) => setTimeout(resolve, 10)))
    expect(host.textContent).not.toContain('Reviewed quota account')
    expect(
      cache
        .getQueryCache()
        .getAll()
        .some((q) => q.queryKey[0] === 'insights-quota-report' && q.state.data !== undefined),
    ).toBe(false)
  },
)
it('exports one fresh reviewed scope and exact filename without claiming browser persistence', async () => {
  await mount()
  await member()
  await click('quotaRun')
  await ready()
  csvGate = deferred()
  const exportButton = button('exportCSV')
  await click('exportCSV')
  await until(() => expect(requests.filter((r) => r.url?.endsWith('/export.csv'))).toHaveLength(1))
  await act(async () => exportButton.click())
  expect(requests.filter((r) => r.url?.endsWith('/export.csv'))).toHaveLength(1)
  csvGate.release()
  await until(() => expect(downloads).toEqual(['routex-key-quota.csv']))
  expect(host.textContent).toContain(text('quotaExportReady'))
  expect(requests.find((r) => r.url?.endsWith('/export.csv'))!.params).toEqual(reports()[0].params)
})
it.each(['target', 'permissions', 'cancel', 'unmount'])(
  'aborts CSV and suppresses stale download after %s',
  async (change) => {
    await mount()
    await member()
    await click('quotaRun')
    await ready()
    csvGate = deferred()
    await click('exportCSV')
    await until(() =>
      expect(requests.filter((r) => r.url?.endsWith('/export.csv'))).toHaveLength(1),
    )
    if (change === 'target') {
      targetGate = deferred()
      await act(async () => {
        void cache.refetchQueries({ queryKey: targetKey(), exact: true })
      })
    } else if (change === 'permissions') {
      permissions = []
      await act(async () => {
        await cache.refetchQueries({ queryKey: ['permissions', quotaActor, 'insights'] })
      })
    } else if (change === 'cancel') await click('cancel')
    else await act(async () => root.render(null))
    csvGate.release()
    await act(async () => new Promise((resolve) => setTimeout(resolve, 10)))
    expect(downloads).toEqual([])
    expect(host.textContent).not.toContain(text('quotaExportReady'))
  },
)
it('keeps calls.read_all as independent page authority despite readable scope permissions', async () => {
  permissions = ['members.read', 'projects.write', 'teams.read_all']
  await mountDenied()
  expect(
    requests.every(
      (r) =>
        r.url === '/auth/session' || r.url === '/auth/permissions' || r.url === '/insights/reports',
    ),
  ).toBe(true)
})
async function mountDenied() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <InsightsPage />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain(text('denied')))
}
it('pages distinct account roots with the server cursor and keeps cards page-scoped', async () => {
  mutation = (r) => {
    r.next_cursor = 'next-page'
    r.items[0].name = 'First page account'
  }
  await mount()
  await click('shortcutTeam')
  await click('quotaRun')
  await until(() => expect(host.textContent).toContain('First page account'))
  expect(advice().textContent).toContain(observation('quotaObservationHigh', 1))
  expect(advice().textContent).toContain(observation('quotaObservationForecast', 1))
  mutation = (r) => {
    r.items[0].name = 'Second page account'
    r.items[0].id = 'tea_01arz3ndektsv4rrffq69g5faw'
    r.items[0].level = 'normal'
    r.items[0].utilization_percent = '10.00'
    r.items[0].month_settled = '10'
    r.items[0].estimate_status = 'after_reset'
    r.items[0].estimate_at = null
    r.items[0].estimate_within_seven_days = false
  }
  await click('quotaNextPage')
  await until(() => expect(host.textContent).toContain('Second page account'))
  expect(host.textContent).not.toContain('First page account')
  expect(reports()[1].params).toEqual({ dimension: 'tokens', limit: 20, cursor: 'next-page' })
  expect(host.textContent).toContain(text('quotaPageAccounts'))
  expect(advice().querySelector('ul')).toBeNull()
})
it('keeps valid server forecasts alongside pending holds and localizes advice without new reads or inference', async () => {
  await mount()
  await member()
  await click('quotaRun')
  await ready()
  expect(advice().textContent).toContain(observation('quotaObservationForecast', 1))
  expect(advice().textContent).toContain(observation('quotaObservationHigh', 1))
  expect(host.textContent).toContain(
    i18n.t('insights:quotaHeldValues', { month: '0', baseline: '0', active: '5' }),
  )
  const readCount = requests.length
  const english = advice().textContent
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(advice().textContent).toContain(observation('quotaObservationForecast', 1))
  expect(advice().textContent).not.toBe(english)
  expect(requests).toHaveLength(readCount)
  expect(requests.every((r) => r.method === 'get')).toBe(true)
})
it.each(['unavailable', 'incomplete_coverage', 'unknown_usage'] as const)(
  'advises evidence review without positive forecasts for %s',
  async (status) => {
    mutation = (r) => {
      r.items[0] = {
        ...r.items[0],
        level: 'unknown',
        utilization_percent: null,
        baseline_covered: status !== 'incomplete_coverage',
        baseline_held: status === 'unknown_usage' ? '8' : '0',
        estimate_status: status,
        estimate_at: null,
        estimate_within_seven_days: false,
      }
    }
    await mount()
    await click('shortcutTeam')
    await click('quotaRun')
    await ready()
    expect(advice().textContent).toContain(observation('quotaObservationEvidence', 1))
    expect(advice().textContent).not.toContain(observation('quotaObservationForecast', 1))
    expect(advice().textContent).not.toContain(observation('quotaObservationHigh', 1))
    expect(host.textContent).toContain(text('quotaEstimate_' + status))
  },
)
it('separates reached cap behaviors without turning inactive accounts into blocking claims', async () => {
  mutation = (r) => {
    const base = r.items[0]
    r.items = [
      {
        ...base,
        level: 'reached',
        month_settled: '100',
        utilization_percent: '100.00',
        estimate_at: null,
        estimate_status: 'already_reached',
        estimate_within_seven_days: false,
      },
      {
        ...base,
        id: 'tea_01arz3ndektsv4rrffq69g5faw',
        behavior: 'alert_only',
        level: 'reached',
        month_settled: '120',
        utilization_percent: '120.00',
        estimate_at: null,
        estimate_status: 'already_reached',
        estimate_within_seven_days: false,
      },
      {
        ...base,
        id: 'tea_01arz3ndektsv4rrffq69g5fax',
        status: 'disabled',
        level: 'normal',
        month_settled: '10',
        utilization_percent: '10.00',
        estimate_at: null,
        estimate_status: 'inactive',
        estimate_within_seven_days: false,
      },
    ]
  }
  await mount()
  await click('shortcutTeam')
  await click('quotaRun')
  await ready()
  expect(advice().textContent).toContain(observation('quotaObservationReached_stop', 1))
  expect(advice().textContent).toContain(observation('quotaObservationReached_alert_only', 1))
  expect(advice().querySelectorAll('li')).toHaveLength(2)
  expect(host.textContent).toContain(text('quotaEstimate_inactive'))
  expect(reports()).toHaveLength(1)
})
it('distinguishes an empty captured page from accounts with unavailable forecast evidence', async () => {
  mutation = (r) => {
    r.items = []
  }
  await mount()
  await click('shortcutTeam')
  await click('quotaRun')
  await until(() => expect(host.textContent).toContain(text('quotaEmpty')))
  expect(advice().textContent).toContain(text('quotaObservationEmpty'))
  expect(advice().querySelector('ul')).toBeNull()
  expect(reports()).toHaveLength(1)
})
it('shows terminal held baseline separately and never presents it as a positive estimate', async () => {
  mutation = (r) => {
    r.items[0] = {
      ...r.items[0],
      baseline_held: '8',
      estimate_at: null,
      estimate_within_seven_days: false,
      estimate_status: 'unknown_usage',
    }
  }
  await mount()
  await click('shortcutTeam')
  await click('quotaRun')
  await ready()
  expect(host.textContent).toContain(
    i18n.t('insights:quotaHeldValues', { month: '0', baseline: '8', active: '5' }),
  )
  expect(host.textContent).toContain(text('quotaEstimate_unknown_usage'))
  expect(host.textContent).not.toContain(text('quotaEstimate_estimated'))
})
it('counts only the authoritative seven-day forecast flag, independently of the total before-reset estimates', async () => {
  await mount()
  await click('shortcutTeam')
  await click('quotaRun')
  await ready()
  const count = () =>
    [...host.querySelectorAll('p')]
      .find((p) => p.textContent === text('quotaPageEstimates'))!
      .parentElement!.querySelector('strong')!.textContent
  expect(count()).toBe('1')
  mutation = (r) => {
    r.items[0].estimate_within_seven_days = false
    r.items[0].estimate_at = '2026-10-25T12:00:00.123456789Z'
  }
  await click('quotaRefresh')
  await until(() => expect(count()).toBe('0'))
  expect(host.textContent).toContain(text('quotaEstimate_estimated'))
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(count()).toBe('0')
  expect(reports()).toHaveLength(2)
})

const saves = () => requests.filter((r) => r.url === '/insights/reports' && r.method === 'post')
async function savedAction(key: string) {
  const row = definitions[0]
  const title = text(row.kind === 'team_quota' ? 'quotaTeamTitle' : 'quotaKeyTitle')
  let trigger: HTMLButtonElement | undefined
  await until(() => {
    trigger = [...host.querySelectorAll<HTMLButtonElement>('button')].find(
      (b) =>
        b.getAttribute('aria-label') === i18n.t('insights:savedActions', { id: row.id, title }),
    )
    expect(trigger).toBeDefined()
  })
  await act(async () => trigger!.click())
  await until(() => expect(document.querySelector('[role="menuitem"]')).not.toBeNull())
  const item = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
    (node) => node.textContent?.trim() === text(key),
  )!
  await act(async () => item.click())
}
it.each(['tokens', 'money'] as const)(
  'saves reviewed Team %s controls without querying statistics and loads only for explicit review',
  async (dimension) => {
    await mount()
    await click('shortcutTeam')
    await select('quotaDimension', dimension)
    await click('saveReport')
    await until(() => expect(host.textContent).toContain(text('savedConfirmed')))
    const body = JSON.parse(saves()[0].data)
    expect(body).toEqual({ request_id: expect.any(String), kind: 'team_quota', dimension })
    expect(reports()).toHaveLength(0)
    await click('quotaRun')
    await ready()
    await savedAction('loadSaved')
    expect(host.textContent).not.toContain('Reviewed quota account')
    expect(host.textContent).toContain(text('savedQuotaLoaded'))
    expect(reports()).toHaveLength(1)
    expect(
      host.querySelector<HTMLSelectElement>('select[aria-label="' + text('quotaDimension') + '"]')!
        .value,
    ).toBe(dimension)
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain(text('savedQuotaLoaded'))
    expect(host.textContent).not.toContain('savedQuotaParameters')
    await select('quotaDimension', dimension === 'tokens' ? 'money' : 'tokens')
    await savedAction('loadSaved')
    expect(reports()).toHaveLength(1)
    expect(
      host.querySelector<HTMLSelectElement>('select[aria-label="' + text('quotaDimension') + '"]')!
        .value,
    ).toBe(dimension)
    await click('quotaRun')
    await ready()
    expect(reports()).toHaveLength(2)
  },
)
it('loads Team Money parameters without restoring an old amount and reads the exact current cap only on explicit run', async () => {
  const first = '9007199254740993.000000000000000001'
  const next = '9007199254740993.000000000000000002'
  let cap = first
  mutation = (report) => {
    Object.assign(report, {
      as_of: null,
      coverage_start: null,
      baseline_from: null,
      baseline_to: null,
      month_from: null,
      month_reset: null,
    })
    Object.assign(report.items[0], {
      local_cap: cap,
      month_settled: null,
      baseline_settled: null,
      month_held: null,
      baseline_held: null,
      active_held: null,
      month_unknown: null,
      baseline_unknown: null,
      month_covered: false,
      baseline_covered: false,
      utilization_percent: null,
      level: 'unknown',
      estimate_at: null,
      estimate_within_seven_days: false,
      estimate_status: 'unavailable',
    })
  }
  await mount()
  await click('shortcutTeam')
  await select('quotaDimension', 'money')
  await click('quotaRun')
  await ready()
  expect(host.textContent).toContain(first + ' USD')
  await click('saveReport')
  await until(() => expect(host.textContent).toContain(text('savedConfirmed')))
  expect(JSON.parse(saves()[0].data)).toEqual({
    request_id: expect.any(String),
    kind: 'team_quota',
    dimension: 'money',
  })
  cap = next
  await savedAction('loadSaved')
  expect(host.textContent).not.toContain(first)
  expect(host.textContent).not.toContain(next)
  expect(host.textContent).not.toContain('Reviewed quota account')
  await select('quotaDimension', 'tokens')
  await savedAction('loadSaved')
  expect(
    host.querySelector<HTMLSelectElement>('select[aria-label="' + text('quotaDimension') + '"]')!
      .value,
  ).toBe('money')
  expect(reports()).toHaveLength(1)
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).not.toContain(first)
  expect(host.textContent).not.toContain(next)
  await click('quotaRun')
  await ready()
  expect(host.textContent).toContain(next + ' USD')
  expect(host.textContent).not.toContain(first)
  expect(reports().map((report) => report.params.dimension)).toEqual(['money', 'money'])
  await act(async () => i18n.changeLanguage('en'))
  expect(host.textContent).toContain(next + ' USD')
  expect(reports()).toHaveLength(2)
  expect(saves()).toHaveLength(1)
})
it('saves exact Key scope and blocks an unreadable loaded target without restoring facts or disclosing names', async () => {
  definitions = [
    {
      id: savedID,
      kind: 'key_quota',
      dimension: 'money',
      scope_kind: 'member',
      scope_id: quotaActor,
      created_at: '2026-10-01T00:00:00Z',
    },
  ]
  targetStatus = 403
  await mount()
  await savedAction('loadSaved')
  await until(() => expect(host.textContent).toContain(text('quotaDenied')))
  expect(
    host.querySelector<HTMLSelectElement>(
      'select[aria-label="' + text('quotaSelectedScope') + '"]',
    )!.selectedOptions[0].textContent,
  ).toBe(quotaActor)
  expect(button('quotaRun').disabled).toBe(true)
  expect(button('saveReport').disabled).toBe(true)
  expect(reports()).toHaveLength(0)
  targetStatus = 0
  await act(async () => {
    await cache.invalidateQueries({ queryKey: targetKey() })
  })
  await until(() => expect(button('quotaRun').disabled).toBe(false))
  await click('saveReport')
  await until(() => expect(host.textContent).toContain(text('savedConfirmed')))
  expect(JSON.parse(saves()[0].data)).toEqual({
    request_id: expect.any(String),
    kind: 'key_quota',
    dimension: 'money',
    scope_kind: 'member',
    scope_id: quotaActor,
  })
})
it('preserves the original uncertain quota intent while dimension controls change and retries identical bytes', async () => {
  await mount()
  await member()
  saveStatus = 503
  await click('saveReport')
  await until(() => expect(host.textContent).toContain(text('saveUnknown')))
  const originalBody = saves()[0].data
  await select('quotaDimension', 'money')
  expect(button('retrySave').disabled).toBe(true)
  await select('quotaDimension', 'tokens')
  await until(() => expect(button('retrySave').disabled).toBe(false))
  saveStatus = 0
  await click('retrySave')
  await until(() => expect(host.textContent).toContain(text('savedConfirmed')))
  expect(saves()[1].data).toBe(originalBody)
  expect(reports()).toHaveLength(0)
})
it('rejects a stale scope-generation save completion and retains owner deletion after report permission loss', async () => {
  await mount()
  await member()
  saveGate = deferred()
  await click('saveReport')
  await until(() => expect(saves()).toHaveLength(1))
  await act(async () => cache.invalidateQueries({ queryKey: targetKey(), refetchType: 'none' }))
  await act(async () => saveGate!.release())
  expect(host.textContent).not.toContain(text('savedConfirmed'))
  permissions = []
  const permissionKey = cache
    .getQueryCache()
    .getAll()
    .find((q) => q.queryKey[0] === 'permissions')!.queryKey
  await act(async () => {
    await cache.invalidateQueries({ queryKey: permissionKey })
  })
  await until(() => expect(host.textContent).toContain(text('denied')))
  await until(() => expect(host.textContent).not.toContain(text('savedLoading')))
  await savedAction('deleteSaved')
  await click('confirmDeleteSaved')
  await until(() => expect(host.textContent).toContain(text('deleteConfirmed')))
  expect(requests.filter((r) => r.method === 'delete')).toHaveLength(1)
})

it('rejects an obsolete pending Team save even after controls return to the original reviewed dimension', async () => {
  await mount()
  await click('shortcutTeam')
  saveGate = deferred()
  await click('saveReport')
  await until(() => expect(saves()).toHaveLength(1))
  const originalBody = saves()[0].data
  await select('quotaDimension', 'money')
  await select('quotaDimension', 'tokens')
  await act(async () => saveGate!.release())
  await until(() => expect(button('retrySave').disabled).toBe(false))
  expect(host.textContent).not.toContain(text('savedConfirmed'))
  saveGate = undefined
  await click('retrySave')
  await until(() => expect(host.textContent).toContain(text('savedConfirmed')))
  expect(saves()[1].data).toBe(originalBody)
  expect(reports()).toHaveLength(0)
})

const analyses = () => requests.filter((r) => r.url === '/admin/insights/analyze')
async function prepareTeamAdvice(dimension: 'tokens' | 'money' = 'tokens') {
  aiModels = true
  await mount()
  await click('shortcutTeam')
  if (dimension === 'money') await select('quotaDimension', 'money')
  await click('quotaRun')
  await ready()
  await until(() => expect(host.textContent).toContain('Personal quota analyst'))
  await select('aiModel', 'mdl_01arz3ndektsv4rrffq69g5fav')
  await click('aiReview')
  await until(() => expect(document.body.textContent).toContain(text('aiConfirmTitle')))
}
it('requires current-page review and explicit personal inference confirmation, and renders anonymous advice as plain text', async () => {
  await prepareTeamAdvice()
  expect(analyses()).toHaveLength(0)
  expect(document.body.textContent).toContain(
    i18n.t('insights:aiTeamQuotaPageReview', {
      limit: 20,
      page: text('aiTeamQuotaFirstPage'),
      dimension: text('quotaTokens'),
    }),
  )
  await click('aiConfirm')
  await until(() => expect(host.textContent).toContain('T1 current-page advice'))
  expect(JSON.parse(analyses()[0].data)).toEqual({
    report: 'team_quota_tokens',
    model_id: 'mdl_01arz3ndektsv4rrffq69g5fav',
    language: 'en',
    cursor: '',
    limit: 20,
  })
  expect(host.querySelector('script')).toBeNull()
  expect(host.querySelector('img')).toBeNull()
  expect(host.textContent).toContain(text('aiTeamQuotaBasis'))
  expect(host.textContent).toContain(text('aiTeamQuotaMore'))
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain(text('aiTeamQuotaSource'))
  expect(analyses()).toHaveLength(1)
  expect(
    cache
      .getQueryCache()
      .getAll()
      .some((q) => (JSON.stringify(q.state.data) ?? '').includes('T1 current-page advice')),
  ).toBe(false)
})
it('uses only the reviewed following-page cursor and revokes its prepared confirmation on refresh', async () => {
  mutation = (r) => {
    r.next_cursor = 'cmV2aWV3ZWQ'
  }
  aiModels = true
  await mount()
  await click('shortcutTeam')
  await click('quotaRun')
  await ready()
  mutation = (r) => {
    r.next_cursor = null
  }
  await click('quotaNextPage')
  await until(() => expect(reports()).toHaveLength(2))
  await ready()
  await select('aiModel', 'mdl_01arz3ndektsv4rrffq69g5fav')
  await click('aiReview')
  await click('aiConfirm')
  await until(() => expect(analyses()).toHaveLength(1))
  expect(JSON.parse(analyses()[0].data).cursor).toBe('cmV2aWV3ZWQ')
  await until(() => expect(host.textContent).toContain('T1 current-page advice'))
  reportGate = deferred()
  await click('quotaRefresh')
  expect(host.textContent).not.toContain('T1 current-page advice')
  reportGate.release()
  await ready()
  expect(analyses()).toHaveLength(1)
})
it.each(['dimension', 'refresh', 'permission', 'session', 'actor', 'models'] as const)(
  'discards a held Team advice result after %s lifetime changes',
  async (boundary) => {
    await prepareTeamAdvice()
    aiGate = deferred()
    await click('aiConfirm')
    await until(() => expect(analyses()).toHaveLength(1))
    if (boundary === 'dimension') await select('quotaDimension', 'money')
    if (boundary === 'refresh') {
      reportGate = deferred()
      await click('quotaRefresh')
    }
    if (boundary === 'permission') {
      permissions = []
      await act(async () => {
        await cache.invalidateQueries({ queryKey: ['permissions', actor] })
      })
    }
    if (boundary === 'session') {
      sessionGate = deferred()
      await act(async () => {
        void cache.invalidateQueries({ queryKey: sessionKey })
      })
    }
    if (boundary === 'actor') {
      actor = 'usr_other'
      await act(async () => {
        cache.setQueryData(sessionKey, { user: { id: actor, role: 'member' }, csrf_token: 'new' })
      })
    }
    if (boundary === 'models') {
      await act(async () => {
        void cache.refetchQueries({ queryKey: ['insights-models'] })
      })
    }
    aiGate.release()
    await act(async () => new Promise((resolve) => setTimeout(resolve, 10)))
    expect(host.textContent).not.toContain('T1 current-page advice')
    expect(analyses()).toHaveLength(1)
  },
)
it('requires a reviewed Money page and keeps unreviewed Key advice unavailable without personal inference', async () => {
  aiModels = true
  await mount()
  await click('shortcutTeam')
  await select('quotaDimension', 'money')
  expect(host.textContent).not.toContain(text('aiTitle'))
  await click('quotaRun')
  await ready()
  expect(host.textContent).toContain(text('aiTitle'))
  await click('shortcutKey')
  expect(host.textContent).not.toContain(text('aiTitle'))
  expect(analyses()).toHaveLength(0)
})
it('keeps an exact Team question transient and does not clear the reviewed page or invoke a model before confirmation', async () => {
  aiModels = true
  await mount()
  await click('shortcutTeam')
  await click('quotaRun')
  await ready()
  const question = '  Explain T1; do not expose Teams outside this page  '
  const field = host.querySelector<HTMLTextAreaElement>('[name="question"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
      field,
      question,
    )
    field.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await click('send')
  expect(analyses()).toHaveLength(0)
  expect(host.textContent).toContain('Reviewed quota account')
  await select('aiModel', 'mdl_01arz3ndektsv4rrffq69g5fav')
  await click('aiReview')
  expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain(question)
  await click('aiConfirm')
  await until(() => expect(analyses()).toHaveLength(1))
  expect(JSON.parse(analyses()[0].data)).toEqual({
    report: 'team_quota_tokens',
    model_id: 'mdl_01arz3ndektsv4rrffq69g5fav',
    language: 'en',
    cursor: '',
    limit: 20,
    question,
  })
  await until(() => expect(host.textContent).toContain('T1 current-page advice'))
  expect(JSON.stringify(cache.getMutationCache().getAll())).not.toContain(question)
})

it.each([
  ['member', 'tokens', 401],
  ['member', 'money', 404],
  ['project', 'tokens', 403],
  ['project', 'money', 404],
] as const)(
  'clears %s Key %s facts and advice when current CSV export is denied %i',
  async (kind, dimension, status) => {
    await prepareKeyAdvice(kind, dimension)
    await click('aiConfirm')
    await until(() => expect(host.textContent).toContain('K1 current-page advice'))
    csvStatus = status
    scopeOptionsAvailable = false
    scopeOptionsGate = deferred()
    await click('exportCSV')
    await until(() => expect(host.textContent).toContain(text('quotaDenied')))
    expect(host.textContent).not.toContain('Reviewed quota account')
    expect(host.textContent).not.toContain('Reviewed member')
    expect(host.textContent).not.toContain('Reviewed Project')
    expect(host.textContent).not.toContain('K1 current-page advice')
    expect(host.textContent).not.toContain('private-secret')
    expect(button('quotaRun').disabled).toBe(true)
    expect(button('exportCSV').disabled).toBe(true)
    expect(reports()).toHaveLength(1)
    expect(analyses()).toHaveLength(1)
    expect(requests.filter((r) => r.url?.endsWith('/export.csv'))).toHaveLength(1)
    expect(downloads).toEqual([])
  },
)
it.each(['tokens', 'money'] as const)(
  'clears Team %s facts and advice when current CSV export is denied',
  async (dimension) => {
    await prepareTeamAdvice(dimension)
    await click('aiConfirm')
    await until(() => expect(host.textContent).toContain('T1 current-page advice'))
    csvStatus = 403
    await click('exportCSV')
    await until(() => expect(host.textContent).toContain(text('quotaDenied')))
    expect(host.textContent).not.toContain('Reviewed quota account')
    expect(host.textContent).not.toContain('T1 current-page advice')
    expect(button('exportCSV').disabled).toBe(true)
    expect(reports()).toHaveLength(1)
    expect(analyses()).toHaveLength(1)
    expect(downloads).toEqual([])
  },
)
it.each(['member', 'project'] as const)(
  'clears %s Key facts when export target reauthorization denies before CSV dispatch',
  async (kind) => {
    await prepareKeyAdvice(kind)
    await click('aiConfirm')
    await until(() => expect(host.textContent).toContain('K1 current-page advice'))
    targetStatus = kind === 'member' ? 403 : 404
    scopeOptionsAvailable = false
    scopeOptionsGate = deferred()
    await click('exportCSV')
    await until(() => expect(host.textContent).toContain(text('quotaDenied')))
    expect(host.textContent).not.toContain('Reviewed quota account')
    expect(host.textContent).not.toContain('Reviewed member')
    expect(host.textContent).not.toContain('Reviewed Project')
    expect(host.textContent).not.toContain('K1 current-page advice')
    expect(button('quotaRun').disabled).toBe(true)
    expect(button('exportCSV').disabled).toBe(true)
    expect(requests.filter((r) => r.url?.endsWith('/export.csv'))).toHaveLength(0)
    expect(reports()).toHaveLength(1)
    expect(analyses()).toHaveLength(1)
    expect(downloads).toEqual([])
  },
)
it.each([422, 503])('retains reviewed Key facts after non-denial export %i', async (status) => {
  await mount()
  await member()
  await click('quotaRun')
  await ready()
  csvStatus = status
  await click('exportCSV')
  await until(() =>
    expect(host.textContent).toContain(
      text(status === 422 ? 'quotaOverflow' : 'quotaExportFailed'),
    ),
  )
  expect(host.textContent).toContain('Reviewed quota account')
  expect(host.textContent).toContain('Reviewed member')
  expect(button('exportCSV').disabled).toBe(false)
  expect(downloads).toEqual([])
})
it('does not let an older CSV denial erase a renewed report generation', async () => {
  await mount()
  await member()
  await click('quotaRun')
  await ready()
  csvGate = deferred()
  csvStatus = 403
  await click('exportCSV')
  await until(() => expect(requests.filter((r) => r.url?.endsWith('/export.csv'))).toHaveLength(1))
  mutation = (report) => {
    report.items[0].name = 'Fresh quota generation'
  }
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['insights-quota-report'], type: 'active' })
  })
  await until(() => expect(host.textContent).toContain('Fresh quota generation'))
  csvGate.release()
  await until(() => expect(button('exportCSV').disabled).toBe(false))
  expect(host.textContent).toContain('Fresh quota generation')
  expect(host.textContent).not.toContain(text('quotaDenied'))
  expect(reports()).toHaveLength(2)
  expect(downloads).toEqual([])
})

async function prepareKeyAdvice(
  kind: 'member' | 'project' = 'member',
  dimension: 'tokens' | 'money' = 'tokens',
) {
  aiModels = true
  await mount()
  await click('shortcutKey')
  if (dimension === 'money') await select('quotaDimension', 'money')
  if (kind === 'project') await select('quotaScopeKind', 'project')
  await until(() =>
    expect(host.textContent).toContain(kind === 'member' ? 'Reviewed member' : 'Reviewed Project'),
  )
  await select('quotaSelectedScope', kind === 'member' ? quotaActor : quotaProject)
  await until(() => expect(button('quotaRun').disabled).toBe(false))
  await click('quotaRun')
  await ready()
  await until(() => expect(host.textContent).toContain('Personal quota analyst'))
  await select('aiModel', 'mdl_01arz3ndektsv4rrffq69g5fav')
  await click('aiReview')
  await until(() => expect(document.body.textContent).toContain(text('aiConfirmTitle')))
}
it.each([
  ['member', 'tokens', 403],
  ['member', 'money', 404],
  ['project', 'tokens', 404],
  ['project', 'money', 403],
] as const)(
  'clears %s Key %s facts on late analysis %i before fresh scope options finish',
  async (kind, dimension, status) => {
    await prepareKeyAdvice(kind, dimension)
    aiGate = deferred()
    await click('aiConfirm')
    await until(() => expect(analyses()).toHaveLength(1))
    expect(host.textContent).toContain('Reviewed quota account')
    expect(host.textContent).toContain(kind === 'member' ? 'Reviewed member' : 'Reviewed Project')
    const optionsURL = kind === 'member' ? '/admin/members' : '/projects'
    const optionReads = requests.filter((request) => request.url === optionsURL).length
    scopeOptionsAvailable = false
    scopeOptionsGate = deferred()
    aiStatus = status
    aiGate.release()
    await until(() => expect(host.textContent).toContain(text('quotaDenied')))
    expect(requests.filter((request) => request.url === optionsURL)).toHaveLength(optionReads + 1)
    expect(host.textContent).not.toContain('Reviewed quota account')
    expect(host.textContent).not.toContain('Reviewed member')
    expect(host.textContent).not.toContain('Reviewed Project')
    expect(host.textContent).not.toContain('K1 current-page advice')
    expect(button('quotaRun').disabled).toBe(true)
    expect(host.textContent).not.toContain(text('aiReview'))
    scopeOptionsGate.release()
    await until(() =>
      expect(
        cache
          .getQueryCache()
          .findAll({ queryKey: ['insights-quota-options'] })
          .every((query) => query.state.fetchStatus === 'idle'),
      ).toBe(true),
    )
    expect(host.textContent).not.toContain('Reviewed quota account')
    expect(host.textContent).not.toContain('Reviewed Project')
    expect(analyses()).toHaveLength(1)
    expect(reports()).toHaveLength(1)
    expect(downloads).toEqual([])
  },
)
it.each(['tokens', 'money'] as const)(
  'clears Team %s facts when the current analysis is denied without replay',
  async (dimension) => {
    await prepareTeamAdvice(dimension)
    aiGate = deferred()
    await click('aiConfirm')
    await until(() => expect(analyses()).toHaveLength(1))
    aiStatus = 403
    aiGate.release()
    await until(() => expect(host.textContent).toContain(text('quotaDenied')))
    expect(host.textContent).not.toContain('Reviewed quota account')
    expect(host.textContent).not.toContain('T1 current-page advice')
    expect(host.textContent).not.toContain(text('aiReview'))
    expect(analyses()).toHaveLength(1)
    expect(reports()).toHaveLength(1)
  },
)
it('keeps a freshly reviewed Member report when an obsolete Project analysis is denied', async () => {
  await prepareKeyAdvice('project')
  aiGate = deferred()
  await click('aiConfirm')
  await until(() => expect(analyses()).toHaveLength(1))
  aiStatus = 404
  await select('quotaScopeKind', 'member')
  await until(() => expect(host.textContent).toContain('Reviewed member'))
  await select('quotaSelectedScope', quotaActor)
  await until(() => expect(button('quotaRun').disabled).toBe(false))
  await click('quotaRun')
  await ready()
  aiGate.release()
  await act(async () => new Promise((resolve) => setTimeout(resolve, 10)))
  expect(host.textContent).toContain('Reviewed quota account')
  expect(host.textContent).toContain('Reviewed member')
  expect(host.textContent).not.toContain(text('quotaDenied'))
  expect(
    host.querySelector<HTMLSelectElement>('[aria-label="' + text('quotaSelectedScope') + '"]')
      ?.value,
  ).toBe(quotaActor)
  expect(analyses()).toHaveLength(1)
  expect(reports()).toHaveLength(2)
})
it.each(['member', 'project'] as const)(
  'requires explicit %s Key page review, personal model and confirmation before advice',
  async (kind) => {
    await prepareKeyAdvice(kind)
    expect(analyses()).toHaveLength(0)
    expect(document.body.textContent).toContain(text('aiKeyQuotaDisclosure'))
    await click('aiConfirm')
    await until(() => expect(host.textContent).toContain('K1 current-page advice'))
    expect(JSON.parse(analyses()[0].data)).toEqual({
      report: 'key_quota_tokens',
      model_id: 'mdl_01arz3ndektsv4rrffq69g5fav',
      language: 'en',
      scope_kind: kind,
      scope_id: kind === 'member' ? quotaActor : quotaProject,
      cursor: '',
      limit: 20,
    })
    expect(host.querySelector('script')).toBeNull()
    expect(host.querySelector('img')).toBeNull()
    expect(host.textContent).toContain(text('aiKeyQuotaSource'))
    expect(host.textContent).toContain(text('aiTeamQuotaBasis'))
    expect(
      Array.from(host.querySelectorAll('table'), (table) => table.getAttribute('aria-label')),
    ).toContain(text('aiKeyQuotaLabels'))
    await act(async () => i18n.changeLanguage('zh'))
    expect(
      Array.from(host.querySelectorAll('table'), (table) => table.getAttribute('aria-label')),
    ).toContain(text('aiKeyQuotaLabels'))
    expect(analyses()).toHaveLength(1)
    expect(JSON.stringify(cache.getMutationCache().getAll())).not.toContain(
      'K1 current-page advice',
    )
    expect(
      cache
        .getQueryCache()
        .getAll()
        .some((q) => (JSON.stringify(q.state.data) ?? '').includes('K1 current-page advice')),
    ).toBe(false)
  },
)
it.each([
  'scope',
  'target',
  'scope_read',
  'dimension',
  'refresh',
  'permission',
  'session',
  'actor',
  'models',
] as const)(
  'discards held Key advice after %s changes without replaying inference',
  async (boundary) => {
    await prepareKeyAdvice()
    aiGate = deferred()
    await click('aiConfirm')
    await until(() => expect(analyses()).toHaveLength(1))
    if (boundary === 'scope') await select('quotaScopeKind', 'project')
    if (boundary === 'target') await select('quotaSelectedScope', '')
    if (boundary === 'scope_read') {
      targetGate = deferred()
      await act(async () => {
        void cache.refetchQueries({ queryKey: targetKey(), exact: true })
      })
    }
    if (boundary === 'dimension') await select('quotaDimension', 'money')
    if (boundary === 'refresh') {
      reportGate = deferred()
      await click('quotaRefresh')
    }
    if (boundary === 'permission') {
      permissions = ['calls.read_all']
      await act(async () => {
        await cache.invalidateQueries({ queryKey: ['permissions', actor] })
      })
    }
    if (boundary === 'session') {
      sessionGate = deferred()
      await act(async () => {
        void cache.invalidateQueries({ queryKey: sessionKey })
      })
    }
    if (boundary === 'actor') {
      actor = 'usr_01arz3ndektsv4rrffq69g5faw'
      await act(async () => {
        cache.setQueryData(sessionKey, { user: { id: actor, role: 'member' }, csrf_token: 'new' })
      })
    }
    if (boundary === 'models')
      await act(async () => {
        void cache.refetchQueries({ queryKey: ['insights-models'] })
      })
    aiGate.release()
    await act(async () => new Promise((resolve) => setTimeout(resolve, 10)))
    expect(host.textContent).not.toContain('K1 current-page advice')
    expect(analyses()).toHaveLength(1)
  },
)
it('keeps the exact Key question transient and its owner page unchanged until explicit confirmation', async () => {
  aiModels = true
  await mount()
  await member()
  await click('quotaRun')
  await ready()
  const question = '  Explain K1; never disclose another owner or Key  '
  const field = host.querySelector<HTMLTextAreaElement>('[name="question"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
      field,
      question,
    )
    field.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await click('send')
  expect(analyses()).toHaveLength(0)
  expect(host.textContent).toContain('Reviewed quota account')
  await select('aiModel', 'mdl_01arz3ndektsv4rrffq69g5fav')
  await click('aiReview')
  expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain(question)
  await click('aiConfirm')
  await until(() => expect(host.textContent).toContain('K1 current-page advice'))
  expect(JSON.parse(analyses()[0].data)).toEqual({
    report: 'key_quota_tokens',
    model_id: 'mdl_01arz3ndektsv4rrffq69g5fav',
    language: 'en',
    scope_kind: 'member',
    scope_id: quotaActor,
    cursor: '',
    limit: 20,
    question,
  })
  expect(JSON.stringify(cache.getMutationCache().getAll())).not.toContain(question)
})

it.each(['team', 'member', 'project'] as const)(
  'reviews %s Money denomination and scope without an automatic paid invocation',
  async (kind) => {
    if (kind === 'team') await prepareTeamAdvice('money')
    else await prepareKeyAdvice(kind, 'money')
    expect(analyses()).toHaveLength(0)
    expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain(
      text('quotaMoney'),
    )
    expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain(
      i18n.t('insights:aiQuotaCurrency', { currency: 'USD' }),
    )
    await click('aiConfirm')
    await until(() =>
      expect(host.textContent).toContain(`${kind === 'team' ? 'T1' : 'K1'} current-page advice`),
    )
    expect(JSON.parse(analyses()[0].data)).toEqual({
      report: kind === 'team' ? 'team_quota_money' : 'key_quota_money',
      model_id: 'mdl_01arz3ndektsv4rrffq69g5fav',
      language: 'en',
      cursor: '',
      limit: 20,
      ...(kind === 'team'
        ? {}
        : { scope_kind: kind, scope_id: kind === 'member' ? quotaActor : quotaProject }),
    })
    expect(host.textContent).toContain(i18n.t('insights:aiQuotaCurrency', { currency: 'USD' }))
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain(i18n.t('insights:aiQuotaCurrency', { currency: 'USD' }))
    expect(analyses()).toHaveLength(1)
    expect(host.querySelector('script')).toBeNull()
    expect(JSON.stringify(cache.getMutationCache().getAll())).not.toContain('current-page advice')
  },
)

it.each(['team', 'member', 'project'] as const)(
  'rejects a changed %s Money denomination returned by the analysis capture',
  async (kind) => {
    mutation = (report) => {
      report.currency = 'EUR'
    }
    if (kind === 'team') await prepareTeamAdvice('money')
    else await prepareKeyAdvice(kind, 'money')
    expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain(
      i18n.t('insights:aiQuotaCurrency', { currency: 'EUR' }),
    )
    await click('aiConfirm')
    await until(() => expect(host.textContent).toContain(text('aiUnavailable')))
    expect(host.textContent).not.toContain('current-page advice')
    expect(analyses()).toHaveLength(1)
  },
)

it.each([
  ['team', 'dimension'],
  ['team', 'refresh'],
  ['team', 'session'],
  ['member', 'dimension'],
  ['member', 'target'],
  ['member', 'session'],
  ['project', 'scope'],
  ['project', 'refresh'],
  ['project', 'actor'],
] as const)(
  'discards held %s Money advice after %s changes without retry or stale private cache writes',
  async (kind, boundary) => {
    if (kind === 'team') await prepareTeamAdvice('money')
    else await prepareKeyAdvice(kind, 'money')
    aiGate = deferred()
    await click('aiConfirm')
    await until(() => expect(analyses()).toHaveLength(1))
    if (boundary === 'dimension') await select('quotaDimension', 'tokens')
    if (boundary === 'target') await select('quotaSelectedScope', '')
    if (boundary === 'scope') await select('quotaScopeKind', 'member')
    if (boundary === 'refresh') {
      reportGate = deferred()
      await click('quotaRefresh')
    }
    if (boundary === 'session') {
      sessionGate = deferred()
      await act(async () => {
        void cache.invalidateQueries({ queryKey: sessionKey })
      })
    }
    if (boundary === 'actor') {
      actor = 'usr_01arz3ndektsv4rrffq69g5faw'
      await act(async () => {
        cache.setQueryData(sessionKey, { user: { id: actor, role: 'member' }, csrf_token: 'fresh' })
      })
    }
    expect(analyses()[0].signal?.aborted).toBe(true)
    aiGate.release()
    await act(async () => new Promise((resolve) => setTimeout(resolve, 10)))
    expect(host.textContent).not.toContain('current-page advice')
    expect(analyses()).toHaveLength(1)
    expect(
      cache
        .getQueryCache()
        .getAll()
        .some((query) => (JSON.stringify(query.state.data) ?? '').includes('current-page advice')),
    ).toBe(false)
  },
)
