import { qualityFixture } from './provider-quality-fixture'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import { usageFixture } from '@/views/usage/fixture'
import InsightsPage from './index'
import type { InsightsReportDefinition } from '@/types/insights'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, host: HTMLDivElement, cache: QueryClient
let providerPermitted = false
let savedDefinitions: InsightsReportDefinition[]
let actor: string, permitted: boolean, reportError: number, csvError: number, empty: boolean
let requests: InternalAxiosRequestConfig[], downloads: string[]
let reportGate: ReturnType<typeof deferred> | undefined,
  csvGate: ReturnType<typeof deferred> | undefined,
  sessionGate: ReturnType<typeof deferred> | undefined,
  permissionGate: ReturnType<typeof deferred> | undefined
let gates: ReturnType<typeof deferred>[]
const original = client.defaults.adapter
function deferred(): { promise: Promise<void>; release: () => void } {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  const gate = { promise, release }
  gates.push(gate)
  return gate
}
function session() {
  return { user: { id: actor, role: 'member' }, csrf_token: 'csrf' }
}
function failure(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('private-upstream-secret', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: { message: 'private-upstream-secret' },
  })
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_one'
  providerPermitted = false
  savedDefinitions = []
  permitted = true
  reportError = csvError = 0
  empty = false
  requests = []
  downloads = []
  gates = []
  reportGate = csvGate = sessionGate = permissionGate = undefined
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:insights'), revokeObjectURL: vi.fn() })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    downloads.push(this.download)
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown,
      mime = 'application/json'
    if (config.url === '/insights/reports') data = { items: savedDefinitions, next_cursor: '' }
    else if (config.url === '/auth/session') {
      data = structuredClone(session())
      if (sessionGate) await sessionGate.promise
    } else if (config.url === '/auth/permissions') {
      data = {
        permissions: [
          ...(permitted ? ['calls.read_all'] : []),
          ...(providerPermitted ? ['providers.read'] : []),
        ],
      }
      if (permissionGate) await permissionGate.promise
    } else if (config.url === '/projects') {
      data = { items: [], next_cursor: null }
    } else if (config.url === '/admin/insights/models') {
      data = { items: [] }
    } else if (config.url === '/admin/insights/provider-quality') {
      const capturedActor = actor
      if (reportGate) await reportGate.promise
      if (reportError) throw failure(config, reportError)
      data = {
        ...qualityFixture(),
        ...config.params,
        providers: qualityFixture().providers.map((row) => ({
          ...row,
          name: 'Historical Provider ' + capturedActor,
        })),
      }
    } else if (config.url === '/admin/insights/provider-quality/export.csv') {
      if (csvGate) await csvGate.promise
      if (csvError) throw failure(config, csvError)
      data = new Blob(['source,attempts\npersisted_call_attempts,5\n'])
      mime = 'text/csv; charset=utf-8'
    } else if (config.url === '/admin/usage') {
      const capturedActor = actor
      if (reportGate) await reportGate.promise
      if (reportError) throw failure(config, reportError)
      const report = usageFixture()
      report.current.from = config.params.from
      report.current.to = config.params.to
      report.timezone = config.params.timezone
      report.current.models[0].name = 'Historical model ' + capturedActor
      report.current.summary.tokens.input.known = '9007199254740991'
      report.current.summary.tokens.total.known = '9007199254740993'
      report.current.models[0].stats.tokens.total.known = '9007199254740993'
      if (empty) {
        const summary = report.current.summary
        summary.requests = summary.successes = summary.errors = summary.canceled = 0
        summary.success_rate = summary.average_duration_ms = null
        summary.amounts = []
        summary.unknown_amount_calls = 0
        summary.pricing_statuses = {}
        for (const count of Object.values(summary.tokens)) {
          count.value = count.known = '0'
          count.unknown_calls = 0
        }
        report.current.models = []
        report.current.keys = []
      }
      data = report
    } else if (config.url === '/admin/usage/export.csv') {
      if (csvGate) await csvGate.promise
      if (csvError) throw failure(config, csvError)
      data = new Blob(['source,total_tokens\npersisted_call_records,9007199254740993\n'])
      mime = 'text/csv; charset=utf-8'
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
  gates.forEach((gate) => gate.release())
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  await i18n.changeLanguage('en')
})
async function until(assertion: () => void) {
  for (let i = 0; i < 100; i++) {
    try {
      assertion()
      return
    } catch {
      await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
    }
  }
  assertion()
}
function button(label: string) {
  const found = Array.from(host.querySelectorAll<HTMLButtonElement>('button')).find(
    (item) => item.textContent?.trim() === label,
  )
  if (!found) throw new Error('Missing button ' + label)
  return found
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function input(name: string, value: string) {
  const field = host.querySelector<HTMLInputElement | HTMLTextAreaElement>('[name="' + name + '"]')!
  const prototype =
    field instanceof HTMLTextAreaElement
      ? HTMLTextAreaElement.prototype
      : HTMLInputElement.prototype
  await act(async () => {
    Object.getOwnPropertyDescriptor(prototype, 'value')!.set!.call(field, value)
    field.dispatchEvent(new Event('input', { bubbles: true }))
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
  await until(() => expect(host.textContent).toContain(i18n.t('insights:assistantTitle')))
}
const reports = () => requests.filter((item) => item.url === '/admin/usage')
const exports = () => requests.filter((item) => item.url === '/admin/usage/export.csv')
const text = (key: string) => i18n.t('insights:' + key)
async function run() {
  await input('from', '2026-09-01T00:00:00Z')
  await input('to', '2026-09-02T00:00:00Z')
  await click(text('reviewRun'))
}
async function ready() {
  await until(() => expect(host.textContent).toContain('Historical model usr_one'))
}

it('starts in English without querying facts until the explicit range is reviewed', async () => {
  await mount()
  expect(host.textContent).toContain('Operations analysis')
  expect(reports()).toHaveLength(0)
  expect(button(text('saveReport')).disabled).toBe(true)
  await run()
  await ready()
  expect(reports()).toHaveLength(1)
  expect(reports()[0].params).toEqual({
    from: '2026-09-01T00:00:00Z',
    to: '2026-09-02T00:00:00Z',
    timezone: 'UTC',
    granularity: 'auto',
    compare: false,
  })
  expect(host.textContent).toContain('9,007,199,254,740,993')
  expect(host.textContent).toContain(text('source'))
  expect(host.textContent).toContain(i18n.t('insights:unknownCoverage', { count: 1 }))
  expect(host.textContent).not.toContain('Historical provider')
})
it('preserves layout slots and localizes an existing result without replay or clearing input', async () => {
  await mount()
  await run()
  await ready()
  await input('question', 'unsubmitted question')
  const count = reports().length
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(host.textContent).toContain(text('reportTitle'))
  expect(host.querySelector<HTMLTextAreaElement>('[name="question"]')!.value).toBe(
    'unsubmitted question',
  )
  expect(reports()).toHaveLength(count)
  expect(
    Array.from(host.querySelectorAll('div')).some((item) =>
      item.className.includes('xl:grid-cols-[9fr_15fr]'),
    ),
  ).toBe(true)
  expect(host.querySelector('textarea')!.rows).toBe(3)
  expect(host.querySelectorAll('thead th')).toHaveLength(4)
})
it('reviews quota shortcuts and prepares hostile question text without automatic queries or inference', async () => {
  await mount()
  for (const key of ['shortcutTeam', 'shortcutKey']) {
    await click(text(key))
    expect(host.textContent).toContain(text('quotaAwaitingReview'))
  }
  await input('question', '<img src=x onerror=alert(1)>; SELECT secrets; https://outside.test')
  await click(text('send'))
  expect(host.textContent).toContain('SELECT secrets')
  expect(host.textContent).toContain(text('questionPrepared'))
  expect(requests.some((item) => item.url === '/admin/insights/analyze')).toBe(false)
  expect(host.querySelector('img')).toBeNull()
  expect(reports()).toHaveLength(0)
  expect(
    requests.every((item) =>
      [
        '/auth/session',
        '/auth/permissions',
        '/admin/insights/models',
        '/projects',
        '/insights/reports',
      ].includes(item.url!),
    ),
  ).toBe(true)
  await click(text('shortcutTokens'))
  await run()
  await ready()
  expect(reports()).toHaveLength(1)
})
it('requires independent calls.read_all even for an authenticated member', async () => {
  permitted = false
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <InsightsPage />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain(text('denied')))
  expect(reports()).toHaveLength(0)
  expect(host.querySelector('form')).toBeNull()
})
it.each([400, 403, 422, 503])(
  'shows sanitized report failure %i without previous facts',
  async (status) => {
    await mount()
    await run()
    await ready()
    reportError = status
    await click(text('reviewRun'))
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(host.textContent).not.toContain('Historical model')
    expect(host.textContent).not.toContain('private-upstream-secret')
    expect(button(text('exportCSV')).disabled).toBe(true)
  },
)
it('guards duplicate report submission and cancels an old response without restoring it', async () => {
  await mount()
  reportGate = deferred()
  await run()
  expect(reports()).toHaveLength(1)
  await click(text('reviewRun'))
  expect(reports()).toHaveLength(1)
  const old = reportGate,
    signal = reports()[0].signal!
  await click(text('cancel'))
  expect(signal.aborted).toBe(true)
  await act(async () => old.release())
  expect(host.textContent).not.toContain('Historical model')
})
it.each([
  'Session renewal',
  'actor change',
  'permission renewal',
  'permission removal',
  'revocation',
] as const)('hides facts and aborts CSV synchronously on %s', async (change) => {
  await mount()
  await run()
  await ready()
  csvGate = deferred()
  await click(text('exportCSV'))
  const old = csvGate,
    signal = exports()[0].signal!
  if (change === 'Session renewal') {
    sessionGate = deferred()
    await act(async () => {
      void cache.refetchQueries({ queryKey: sessionKey })
    })
  } else if (change === 'actor change') {
    actor = 'usr_two'
    await act(async () => {
      cache.setQueryData(sessionKey, session())
    })
  } else if (change === 'permission renewal') {
    permissionGate = deferred()
    await act(async () => {
      void cache.refetchQueries({ queryKey: ['permissions'] })
    })
  } else if (change === 'permission removal') {
    await act(async () => {
      cache.removeQueries({ queryKey: ['permissions'] })
    })
  } else {
    await act(async () => {
      cache.setQueryData(sessionKey, null)
    })
  }
  await until(() => expect(signal.aborted).toBe(true))
  expect(host.textContent).not.toContain('Historical model')
  await act(async () => old.release())
  expect(downloads).toEqual([])
})
it('rejects a late report after the actor changes and does not revive the old actor cache', async () => {
  await mount()
  reportGate = deferred()
  await run()
  const old = reportGate
  actor = 'usr_two'
  await act(async () => {
    cache.setQueryData(sessionKey, session())
  })
  await act(async () => old.release())
  expect(host.textContent).not.toContain('Historical model usr_one')
  await until(() => expect(host.querySelector('[name="from"]')).not.toBeNull())
  expect(host.querySelector<HTMLInputElement>('[name="from"]')!.value).toBe('')
})
it('exports one new authorized capture using applied parameters and localizes pending status', async () => {
  await mount()
  await run()
  await ready()
  csvGate = deferred()
  await act(async () => {
    button(text('exportCSV')).click()
    button(text('exportCSV')).click()
  })
  expect(exports()).toHaveLength(1)
  expect(exports()[0].params).toEqual(reports()[0].params)
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(host.textContent).toContain(text('exporting'))
  await act(async () => csvGate!.release())
  await until(() => expect(downloads).toEqual(['routex-platform-usage.csv']))
  expect(host.textContent).toContain(text('exportReady'))
})
it.each([403, 422, 503])(
  'does not download failed export %i or expose error text',
  async (status) => {
    await mount()
    await run()
    await ready()
    csvError = status
    await click(text('exportCSV'))
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(downloads).toEqual([])
    expect(host.textContent).not.toContain('private-upstream-secret')
    if (status === 403) {
      expect(host.textContent).not.toContain('Historical model')
      expect(button(text('exportCSV')).disabled).toBe(true)
    }
  },
)
it('ignores a retained old export callback after same-actor authority renewal', async () => {
  await mount()
  await run()
  await ready()
  const oldButton = button(text('exportCSV'))
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await act(async () => oldButton.click())
  expect(exports()).toHaveLength(0)
  expect(host.textContent).not.toContain('Historical model')
})

it.each(['from', 'to', 'timezone'])(
  'clears the reviewed result and rejects late CSV after editing %s',
  async (name) => {
    await mount()
    await run()
    await ready()
    csvGate = deferred()
    await click(text('exportCSV'))
    const old = csvGate,
      signal = exports()[0].signal!
    await input(name, name === 'timezone' ? 'Asia/Shanghai' : '2026-09-03T00:00:00Z')
    expect(signal.aborted).toBe(true)
    expect(host.textContent).not.toContain('Historical model')
    expect(reports()).toHaveLength(1)
    await act(async () => old.release())
    expect(downloads).toEqual([])
  },
)
it('hides invalidated report facts even before a replacement read starts', async () => {
  await mount()
  await run()
  await ready()
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['insights-report'], refetchType: 'none' })
  })
  expect(host.textContent).not.toContain('Historical model')
  expect(button(text('exportCSV')).disabled).toBe(true)
})
it('cancels CSV explicitly and never treats the late response as a download', async () => {
  await mount()
  await run()
  await ready()
  csvGate = deferred()
  await click(text('exportCSV'))
  const old = csvGate,
    signal = exports()[0].signal!
  await click(text('cancel'))
  expect(signal.aborted).toBe(true)
  await act(async () => old.release())
  expect(downloads).toEqual([])
})

it('keeps empty complete coverage distinct from unknown tokens without simulated rows', async () => {
  empty = true
  await mount()
  await run()
  await until(() => expect(host.textContent).toContain(text('noCalls')))
  expect(host.textContent).toContain(text('noKnownTokens'))
  expect(host.textContent).toContain(text('completeCoverage'))
  expect(host.textContent).not.toContain('Historical model')
  expect(host.textContent).not.toContain(i18n.t('insights:unknownCoverage', { count: 1 }))
})

it('requires Provider read independently and keeps Tokens by Model available', async () => {
  await mount()
  await click(text('shortcutProvider'))
  expect(host.textContent).toContain(text('providerReadDenied'))
  expect(button(text('reviewRun')).disabled).toBe(true)
  expect(requests.filter((item) => item.url === '/admin/insights/provider-quality')).toHaveLength(0)
  await click(text('shortcutTokens'))
  await run()
  await ready()
})
it('captures selected-range attempts, localizes without replay, and keeps Provider facts out of AI', async () => {
  providerPermitted = true
  await mount()
  await click(text('shortcutProvider'))
  await run()
  await until(() => expect(host.textContent).toContain('Historical Provider usr_one'))
  const captures = requests.filter((item) => item.url === '/admin/insights/provider-quality')
  expect(captures).toHaveLength(1)
  expect(captures[0].params).toEqual({
    from: '2026-09-01T00:00:00Z',
    to: '2026-09-02T00:00:00Z',
    timezone: 'UTC',
  })
  expect(host.textContent).toContain('50%')
  expect(host.textContent).toContain(text('qualityUnknownHelp'))
  expect(requests.some((item) => item.url === '/admin/insights/analyze')).toBe(false)
  expect(host.querySelector('[name="model_id"]')).toBeNull()
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(host.textContent).toContain(text('providerReportTitle'))
  expect(host.textContent).toContain(
    i18n.t('insights:qualityDurationCounts', { known: '1', unknown: '1' }),
  )
  expect(requests.filter((item) => item.url === '/admin/insights/provider-quality')).toHaveLength(1)
  await click(text('exportCSV'))
  await until(() => expect(downloads).toEqual(['routex-provider-quality.csv']))
  expect(
    requests.find((item) => item.url === '/admin/insights/provider-quality/export.csv')?.params,
  ).toEqual(captures[0].params)
})
it('hides Provider facts and aborts a late export on independent permission revocation', async () => {
  providerPermitted = true
  await mount()
  await click(text('shortcutProvider'))
  await run()
  await until(() => expect(host.textContent).toContain('Historical Provider usr_one'))
  csvGate = deferred()
  await click(text('exportCSV'))
  await until(() =>
    expect(
      requests.some((item) => item.url === '/admin/insights/provider-quality/export.csv'),
    ).toBe(true),
  )
  providerPermitted = false
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['permissions'] })
  })
  expect(host.textContent).not.toContain('Historical Provider usr_one')
  csvGate.release()
  await act(async () => new Promise((resolve) => setTimeout(resolve, 15)))
  expect(downloads).toEqual([])
})
it('rejects an obsolete Provider response after actor renewal', async () => {
  providerPermitted = true
  await mount()
  await click(text('shortcutProvider'))
  reportGate = deferred()
  await run()
  await until(() =>
    expect(requests.some((item) => item.url === '/admin/insights/provider-quality')).toBe(true),
  )
  actor = 'usr_two'
  await act(async () => {
    await cache.invalidateQueries({ queryKey: sessionKey })
  })
  reportGate.release()
  await act(async () => new Promise((resolve) => setTimeout(resolve, 15)))
  expect(host.textContent).not.toContain('Historical Provider usr_one')
  expect(button(text('exportCSV')).disabled).toBe(true)
})

it.each([400, 403, 422, 503])(
  'sanitizes Provider report failure %i and hides prior facts',
  async (status) => {
    providerPermitted = true
    await mount()
    await click(text('shortcutProvider'))
    await run()
    await until(() => expect(host.textContent).toContain('Historical Provider usr_one'))
    reportError = status
    await click(text('reviewRun'))
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(host.textContent).not.toContain('Historical Provider usr_one')
    expect(host.textContent).not.toContain('private-upstream-secret')
    expect(button(text('exportCSV')).disabled).toBe(true)
    if (status === 403) expect(host.textContent).toContain(text('providerReadDenied'))
  },
)
it.each(['shortcutTeam', 'shortcutKey'])(
  'loads a private saved definition from %s without running quota, reports or inference',
  async (shortcut) => {
    savedDefinitions = [
      {
        id: 'isr_01arz3ndektsv4rrffq69g5fav',
        kind: 'tokens_by_model',
        from: '2026-09-01T00:00:00Z',
        to: '2026-09-02T00:00:00Z',
        timezone: 'UTC',
        created_at: '2026-09-03T00:00:00Z',
      },
    ]
    await mount()
    await click(text(shortcut))
    await until(() =>
      expect(
        host.querySelector<HTMLButtonElement>(
          '[aria-label="' +
            i18n.t('insights:savedActions', {
              id: savedDefinitions[0].id,
              title: text('reportTitle'),
            }) +
            '"]',
        ),
      ).not.toBeNull(),
    )
    expect(button(text('saveReport')).disabled).toBe(shortcut !== 'shortcutTeam')
    const menu = host.querySelector<HTMLButtonElement>(
      '[aria-label="' +
        i18n.t('insights:savedActions', {
          id: savedDefinitions[0].id,
          title: text('reportTitle'),
        }) +
        '"]',
    )!
    await act(async () => menu.click())
    await until(() => expect(document.querySelector('[role="menuitem"]')).not.toBeNull())
    const load = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
      (item) => item.textContent?.trim() === text('loadSaved'),
    )!
    await act(async () => load.click())
    expect(host.textContent).toContain(text('savedLoaded'))
    expect(host.querySelector('[aria-label="' + text('quotaReview') + '"]')).toBeNull()
    expect(host.querySelector<HTMLInputElement>('[name="from"]')!.value).toBe(
      'from' in savedDefinitions[0] ? savedDefinitions[0].from : '',
    )
    expect(reports()).toHaveLength(0)
    expect(requests.some((item) => item.url?.startsWith('/admin/insights/quota/'))).toBe(false)
    expect(requests.some((item) => item.method === 'post')).toBe(false)
    await click(text('reviewRun'))
    await ready()
    expect(reports()).toHaveLength(1)
  },
)
