import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import UsagePage from './index'
import { usageFixture, teamUsageFixture } from './fixture'
import type { UsageScope } from '@/types/usage'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, host: HTMLDivElement, cache: QueryClient
let requests: InternalAxiosRequestConfig[],
  actor: string,
  csrf: string,
  permitted: boolean,
  reportError: number,
  csvError: number,
  teamsError: number
let csvGate: ReturnType<typeof deferred> | undefined,
  sessionGate: ReturnType<typeof deferred> | undefined,
  reportGate: ReturnType<typeof deferred> | undefined,
  teamsGate: ReturnType<typeof deferred> | undefined,
  permissionGate: ReturnType<typeof deferred> | undefined
let gates: ReturnType<typeof deferred>[], downloaded: string[], scope: UsageScope
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
  return { user: { id: actor, role: 'admin' }, csrf_token: csrf }
}
function reject(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('private-error', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: { secret: 'not visible' },
  })
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.spyOn(Date, 'now').mockReturnValue(1800000000000)
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  requests = []
  gates = []
  downloaded = []
  actor = 'usr_one'
  csrf = 'csrf-original'
  permitted = true
  reportError = 0
  csvError = 0
  teamsError = 0
  scope = {}
  csvGate = sessionGate = reportGate = teamsGate = permissionGate = undefined
  vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:usage'), revokeObjectURL: vi.fn() })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    downloaded.push(this.download)
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown,
      mime = 'application/json'
    if (config.url === '/auth/session') {
      data = structuredClone(session())
      if (sessionGate) await sessionGate.promise
    } else if (config.url === '/auth/permissions') {
      data = { permissions: permitted ? ['calls.read_all'] : [] }
      if (permissionGate) await permissionGate.promise
    } else if (config.url === '/teams') {
      if (teamsError) throw reject(config, teamsError)
      if (teamsGate) await teamsGate.promise
      data = {
        items: [
          { id: 'tea_one', name: 'Research', status: 'active' },
          { id: 'tea_two', name: 'Design', status: 'active' },
        ],
        next_cursor: null,
      }
    } else if (config.url?.endsWith('/usage/export.csv')) {
      if (csvGate) await csvGate.promise
      if (csvError) throw reject(config, csvError)
      data = new Blob(["text_encoding,total_tokens\napostrophe_text_v1,'9007199254740993\n"])
      mime = 'text/csv; charset=utf-8'
    } else if (config.url?.endsWith('/usage')) {
      if (reportGate) await reportGate.promise
      if (reportError) throw reject(config, reportError)
      data = config.url.startsWith('/teams/')
        ? teamUsageFixture(config.url.split('/')[2])
        : usageFixture()
    } else throw new Error('Unexpected API request')
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
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  await i18n.changeLanguage('en')
})
async function flush() {
  await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
}
async function until(check: () => void) {
  for (let i = 0; i < 100; i++) {
    await flush()
    try {
      check()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <UsagePage {...scope} />
      </QueryClientProvider>,
    ),
  )
}
async function mount(next: UsageScope = {}) {
  scope = next
  await render()
  await until(() => expect(host.textContent).toContain('Historical model'))
}
function button(label: string) {
  return [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === label,
  )!
}
async function click(label: string) {
  expect(button(label)).toBeDefined()
  await act(async () => button(label).click())
}
const exports = () => requests.filter((r) => r.url?.endsWith('/export.csv'))
const reports = () => requests.filter((r) => r.url?.endsWith('/usage'))
async function setInput(name: string, value: string) {
  const input = host.querySelector<HTMLInputElement>(`[name="${name}"]`)!
  await act(async () => {
    input.value = value
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
it.each([
  [{}, '/usage/export.csv', 'personal'],
  [{ projectId: 'prj_one' }, '/projects/prj_one/usage/export.csv', 'project'],
  [{ teamId: 'tea_one' }, '/teams/tea_one/usage/export.csv', 'team'],
  [{ admin: true }, '/admin/usage/export.csv', 'platform'],
] as const)(
  'exports %j with applied filters and no secrets or catalog fetch',
  async (next, path, filename) => {
    await mount(next)
    expect([...host.querySelector('form')!.querySelectorAll('button')].at(-1)?.textContent).toBe(
      'Export CSV',
    )
    await setInput('model_id', 'mdl_applied')
    await click('Apply filters')
    await until(() => expect(button('Export CSV').disabled).toBe(false))
    await setInput('model_id', 'mdl_unsaved')
    await click('Export CSV')
    await until(() => expect(downloaded).toEqual([`routex-${filename}-usage.csv`]))
    expect(exports()[0].url).toBe(path)
    expect(exports()[0].params.model_id).toBe('mdl_applied')
    expect(exports()[0].headers.has('Authorization')).toBe(false)
    expect(host.textContent).toContain('fresh server report')
    expect(host.textContent).toContain('apostrophe_text_v1')
    expect(
      requests.some(
        (r) =>
          r.url?.includes('/models') || r.url?.includes('/keys') || r.url?.includes('/admin/teams'),
      ),
    ).toBe(false)
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
    if ('teamId' in next) {
      expect(host.querySelector('[name="key_id"]')).toBeNull()
      expect(host.textContent).not.toContain('API Key ranking')
    }
  },
)
it('locks duplicate same-turn clicks and localizes in-flight success without replay', async () => {
  await mount()
  csvGate = deferred()
  await act(async () => {
    button('Export CSV').click()
    button('Export CSV').click()
  })
  expect(exports()).toHaveLength(1)
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('正在生成 CSV')
  await act(async () => csvGate!.release())
  await until(() => expect(downloaded).toHaveLength(1))
  expect(host.textContent).toContain('CSV 下载已准备好')
  expect(exports()).toHaveLength(1)
})
it.each([403, 404, 422, 503])(
  'never downloads failed HTTP%i and requires an explicit retry',
  async (status) => {
    await mount()
    csvError = status
    await click('Export CSV')
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(downloaded).toEqual([])
    expect(exports()).toHaveLength(1)
    expect(host.textContent).not.toContain('private-error')
    if (status === 422) expect(host.textContent).toContain('complete export exceeds')
    csvError = 0
    if ([403, 404].includes(status)) {
      expect(host.textContent).not.toContain('Historical model')
      expect(button('Export CSV').disabled).toBe(true)
      await click('Refresh')
      await until(() => expect(button('Export CSV').disabled).toBe(false))
    }
    await click('Export CSV')
    await until(() => expect(downloaded).toHaveLength(1))
    expect(exports()).toHaveLength(2)
  },
)
it.each([{}, { projectId: 'prj_one' }, { admin: true }, { teamId: 'tea_one' }])(
  'hides private facts/actions on Session renewal for %j and ignores late CSV',
  async (next) => {
    await mount(next)
    csvGate = deferred()
    await click('Export CSV')
    const old = csvGate,
      signal = exports()[0].signal!
    sessionGate = deferred()
    await act(async () => {
      void cache.refetchQueries({ queryKey: sessionKey })
    })
    await until(() => expect(host.textContent).not.toContain('Historical model'))
    expect(host.textContent).not.toContain('Export CSV')
    expect(signal.aborted).toBe(true)
    await act(async () => old.release())
    expect(downloaded).toEqual([])
    csvGate = undefined
    await act(async () => sessionGate!.release())
    await until(() => expect(host.textContent).toContain('Historical model'))
    expect(downloaded).toEqual([])
  },
)
it('invalidates identical same-ms network renewal without a second Session observer', async () => {
  await mount()
  csvGate = deferred()
  await click('Export CSV')
  const old = csvGate,
    before = cache.getQueryState(sessionKey)!
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await until(() => expect(button('Export CSV')?.disabled).toBe(false))
  const after = cache.getQueryState(sessionKey)!
  expect(after.data).toBe(before.data)
  expect(after.dataUpdatedAt).toBe(before.dataUpdatedAt)
  expect(after.dataUpdateCount).toBe(before.dataUpdateCount + 1)
  await act(async () => old.release())
  expect(downloaded).toEqual([])
  expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2)
})
it('retains same-actor manual CSRF cache replacement without treating it as network renewal', async () => {
  await mount()
  csvGate = deferred()
  await click('Export CSV')
  const count = reports().length
  csrf = 'csrf-rotated'
  await act(async () => cache.setQueryData(sessionKey, session()))
  expect(reports()).toHaveLength(count)
  expect(exports()[0].signal!.aborted).toBe(false)
  await act(async () => csvGate!.release())
  await until(() => expect(downloaded).toHaveLength(1))
  expect(exports()[0].headers.has('X-CSRF-Token')).toBe(false)
})
it.each(['actor', 'target', 'filter', 'report', 'permission', 'Team context'] as const)(
  'cancels captured CSV on %s changes before late success',
  async (change) => {
    await mount(
      change === 'permission'
        ? { admin: true }
        : change === 'target'
          ? { projectId: 'prj_one' }
          : change === 'Team context'
            ? { teamId: 'tea_one' }
            : {},
    )
    csvGate = deferred()
    await click('Export CSV')
    const old = csvGate,
      signal = exports()[0].signal!
    if (change === 'actor') {
      actor = 'usr_two'
      await act(async () => cache.setQueryData(sessionKey, session()))
    } else if (change === 'target') {
      scope = { projectId: 'prj_two' }
      await render()
    } else if (change === 'filter') {
      await setInput('model_id', 'mdl_new')
      await click('Apply filters')
    } else if (change === 'report') {
      reportGate = deferred()
      await click('Refresh')
    } else if (change === 'permission') {
      permitted = false
      await act(async () => cache.invalidateQueries({ queryKey: ['permissions'] }))
      await until(() => expect(host.textContent).toContain('does not have permission'))
    } else {
      teamsGate = deferred()
      await click('Refresh Teams')
    }
    await until(() => expect(signal.aborted).toBe(true))
    await act(async () => old.release())
    expect(downloaded).toEqual([])
    expect(exports()).toHaveLength(1)
  },
)
it('suppresses old report and export through report error for every scope', async () => {
  await mount({ projectId: 'prj_one' })
  reportGate = deferred()
  reportError = 403
  await click('Refresh')
  await until(() => expect(host.textContent).not.toContain('Historical model'))
  expect(button('Export CSV').disabled).toBe(true)
  await act(async () => reportGate!.release())
  await until(() => expect(host.textContent).toContain('no longer have access'))
  expect(button('Export CSV').disabled).toBe(true)
})
it('exports the applied custom range, calendar, comparison and native selectors unchanged', async () => {
  await mount({ projectId: 'prj_one' })
  const period = host.querySelector<HTMLSelectElement>('[name="period"]')!
  await act(async () => {
    period.value = 'custom'
    period.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await setInput('from', '2026-09-01T00:00:00.123456789+08:00')
  await setInput('to', '2026-09-02T00:00:00.123456789+08:00')
  await setInput('timezone', 'Asia/Shanghai')
  for (const [name, value] of [
    ['granularity', 'day'],
    ['status', 'canceled'],
    ['stream', 'false'],
    ['protocol', 'gemini_generate_content'],
  ]) {
    const select = host.querySelector<HTMLSelectElement>(`[name="${name}"]`)!
    await act(async () => {
      select.value = value
      select.dispatchEvent(new Event('change', { bubbles: true }))
    })
  }
  await act(async () => host.querySelector<HTMLButtonElement>('[role="switch"]')!.click())
  await click('Apply filters')
  await until(() => expect(button('Export CSV').disabled).toBe(false))
  await click('Export CSV')
  await until(() => expect(downloaded).toHaveLength(1))
  expect(exports()[0].params).toEqual({
    from: '2026-09-01T00:00:00.123456789+08:00',
    to: '2026-09-02T00:00:00.123456789+08:00',
    timezone: 'Asia/Shanghai',
    granularity: 'day',
    compare: true,
    status: 'canceled',
    stream: false,
    protocol: 'gemini_generate_content',
  })
})
it('cancels a Personal export when switching to Team and never borrows its Key selector', async () => {
  await mount()
  csvGate = deferred()
  await click('Export CSV')
  const old = csvGate,
    signal = exports()[0].signal!
  const select = host.querySelector<HTMLSelectElement>('[aria-label="Resource account"]')!
  await act(async () => {
    select.value = 'tea_two'
    select.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await until(() => expect(signal.aborted).toBe(true))
  await until(() => expect(host.querySelector('[name="key_id"]')).toBeNull())
  await act(async () => old.release())
  expect(downloaded).toEqual([])
  csvGate = undefined
  await click('Export CSV')
  await until(() => expect(downloaded).toEqual(['routex-team-usage.csv']))
  expect(exports().at(-1)!.url).toBe('/teams/tea_two/usage/export.csv')
  expect(exports().at(-1)!.params.key_id).toBeUndefined()
})
it('aborts even a fast identical report renewal before React renders fetching', async () => {
  await mount({ projectId: 'prj_one' })
  csvGate = deferred()
  await click('Export CSV')
  const old = csvGate
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['usage'] })
  })
  await act(async () => old.release())
  expect(downloaded).toEqual([])
  expect(exports()).toHaveLength(1)
})
it.each([{}, { projectId: 'prj_one' }, { admin: true }, { teamId: 'tea_one' }])(
  'hides all scopes on fast renewed report rejection for %j',
  async (next) => {
    await mount(next)
    reportError = 403
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['usage'] })
    })
    await until(() => expect(host.textContent).toContain('no longer have access'))
    expect(host.textContent).not.toContain('Historical model')
    expect(host.querySelector('tbody')).toBeNull()
    expect(button('Export CSV').disabled).toBe(true)
  },
)

it('keeps Personal report/export independent of an unavailable own-Team picker', async () => {
  teamsError = 503
  await mount()
  expect(host.textContent).toContain('Your Teams could not be authorized')
  expect(button('Export CSV').disabled).toBe(false)
  await click('Export CSV')
  await until(() => expect(downloaded).toEqual(['routex-personal-usage.csv']))
  expect(exports()[0].url).toBe('/usage/export.csv')
})

async function selectFilter(name: string, value: string) {
  const select = host.querySelector<HTMLSelectElement>(`[name="${name}"]`)!
  await act(async () => {
    select.value = value
    select.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
function draftValue(name: string) {
  return host.querySelector<HTMLInputElement | HTMLSelectElement>(`[name="${name}"]`)!.value
}
async function applyCustomThenDraft() {
  await selectFilter('period', 'custom')
  await setInput('from', '2026-09-01T00:00:00.123456789+08:00')
  await setInput('to', '2026-09-02T00:00:00.123456789+08:00')
  await setInput('timezone', 'Asia/Shanghai')
  await selectFilter('granularity', 'day')
  await selectFilter('status', 'success')
  await selectFilter('stream', 'true')
  await selectFilter('protocol', 'openai_responses')
  if (host.querySelector('[name="key_id"]')) await setInput('key_id', 'key_applied')
  if (host.querySelector('[name="provider_id"]')) await setInput('provider_id', 'provider_applied')
  await setInput('model_id', 'mdl_applied')
  await act(async () => host.querySelector<HTMLButtonElement>('[role="switch"]')!.click())
  await click('Apply filters')
  await until(() => expect(button('Export CSV').disabled).toBe(false))
  const applied = { ...reports().at(-1)!.params }
  await setInput('model_id', '  mdl_unsaved  ')
  await setInput('from', 'unfinished range')
  await setInput('timezone', 'unfinished timezone')
  await selectFilter('granularity', 'week')
  await selectFilter('status', 'canceled')
  await selectFilter('stream', 'false')
  await selectFilter('protocol', 'anthropic_messages')
  if (host.querySelector('[name="key_id"]')) await setInput('key_id', 'key_unsaved')
  if (host.querySelector('[name="provider_id"]')) await setInput('provider_id', 'provider_unsaved')
  await act(async () => host.querySelector<HTMLButtonElement>('[role="switch"]')!.click())
  return applied
}
function expectUnsavedDraft() {
  expect(draftValue('period')).toBe('custom')
  expect(draftValue('model_id')).toBe('  mdl_unsaved  ')
  expect(draftValue('from')).toBe('unfinished range')
  expect(draftValue('to')).toBe('2026-09-02T00:00:00.123456789+08:00')
  expect(draftValue('timezone')).toBe('unfinished timezone')
  expect(draftValue('granularity')).toBe('week')
  expect(draftValue('status')).toBe('canceled')
  expect(draftValue('stream')).toBe('false')
  expect(draftValue('protocol')).toBe('anthropic_messages')
  if (host.querySelector('[name="key_id"]')) expect(draftValue('key_id')).toBe('key_unsaved')
  if (host.querySelector('[name="provider_id"]'))
    expect(draftValue('provider_id')).toBe('provider_unsaved')
  expect(host.querySelector('[role="switch"]')!.getAttribute('aria-checked')).toBe('false')
}
it.each([{}, { projectId: 'prj_one' }, { admin: true }, { teamId: 'tea_one' }])(
  'preserves distinct applied filters and raw draft on identical same-ms Session renewal for %j',
  async (next) => {
    await mount(next)
    const applied = await applyCustomThenDraft()
    expect(applied).toMatchObject({
      from: '2026-09-01T00:00:00.123456789+08:00',
      timezone: 'Asia/Shanghai',
      granularity: 'day',
      status: 'success',
      model_id: 'mdl_applied',
      compare: true,
    })
    csvGate = deferred()
    await click('Export CSV')
    const old = csvGate,
      signal = exports()[0].signal!,
      before = cache.getQueryState(sessionKey)!
    sessionGate = deferred()
    await act(async () => {
      void cache.refetchQueries({ queryKey: sessionKey })
    })
    await until(() => expect(host.querySelector('form')).toBeNull())
    expect(host.textContent).not.toContain('Historical model')
    expect(signal.aborted).toBe(true)
    await act(async () => sessionGate!.release())
    await until(() => expect(button('Export CSV')?.disabled).toBe(false))
    const after = cache.getQueryState(sessionKey)!
    expect(after.data).toBe(before.data)
    expect(after.dataUpdatedAt).toBe(before.dataUpdatedAt)
    expect(after.dataUpdateCount).toBe(before.dataUpdateCount + 1)
    expect(reports().at(-1)!.params).toEqual(applied)
    expectUnsavedDraft()
    await act(async () => old.release())
    expect(downloaded).toEqual([])
    expect(exports()).toHaveLength(1)
    expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2)
    csvGate = undefined
    await click('Export CSV')
    await until(() => expect(downloaded).toHaveLength(1))
    expect(exports().at(-1)!.params).toEqual(applied)
    await click('Reset')
    await until(() => expect(button('Export CSV').disabled).toBe(false))
    expect(draftValue('period')).toBe('month')
    expect(draftValue('model_id')).toBe('')
    expect(draftValue('timezone')).toBe('UTC')
    expect(draftValue('granularity')).toBe('auto')
    expect(reports().at(-1)!.params).toEqual({
      period: 'month',
      timezone: 'UTC',
      granularity: 'auto',
      compare: false,
    })
  },
)
it('preserves filter intent through a platform permission read and temporary denial without private facts', async () => {
  await mount({ admin: true })
  const applied = await applyCustomThenDraft()
  csvGate = deferred()
  await click('Export CSV')
  const signal = exports()[0].signal!
  permissionGate = deferred()
  permitted = false
  await act(async () => {
    void cache.refetchQueries({ queryKey: ['permissions'] })
  })
  await until(() => expect(host.querySelector('form')).toBeNull())
  expect(host.textContent).not.toContain('Historical model')
  expect(signal.aborted).toBe(true)
  await act(async () => permissionGate!.release())
  await until(() => expect(host.textContent).toContain('does not have permission'))
  expect(host.querySelector('form')).toBeNull()
  permitted = true
  permissionGate = undefined
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['permissions'] })
  })
  await until(() => expect(button('Export CSV')?.disabled).toBe(false))
  expectUnsavedDraft()
  expect(reports().at(-1)!.params).toEqual(applied)
  await act(async () => csvGate!.release())
  expect(downloaded).toEqual([])
  expect(exports()).toHaveLength(1)
})
it('retains selected Team and its intent through Session and Team reads, clearing intent on source changes', async () => {
  await mount()
  async function source(value: string) {
    const select = host.querySelector<HTMLSelectElement>('[aria-label="Resource account"]')!
    await act(async () => {
      select.value = value
      select.dispatchEvent(new Event('change', { bubbles: true }))
    })
    await until(() => expect(button('Export CSV')?.disabled).toBe(false))
  }
  await setInput('model_id', 'mdl_personal_draft')
  await source('tea_two')
  expect(draftValue('model_id')).toBe('')
  const applied = await applyCustomThenDraft()
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await until(() => expect(button('Export CSV')?.disabled).toBe(false))
  expect(host.querySelector<HTMLSelectElement>('[aria-label="Resource account"]')!.value).toBe(
    'tea_two',
  )
  expect(reports().at(-1)!.url).toBe('/teams/tea_two/usage')
  expect(reports().at(-1)!.params).toEqual(applied)
  expectUnsavedDraft()
  csvGate = deferred()
  await click('Export CSV')
  teamsGate = deferred()
  await click('Refresh Teams')
  await until(() => expect(host.textContent).not.toContain('Historical model'))
  expect(button('Preparing CSV…').disabled).toBe(true)
  expectUnsavedDraft()
  expect(exports()[0].signal!.aborted).toBe(true)
  await act(async () => teamsGate!.release())
  await until(() => expect(host.textContent).toContain('Historical model'))
  expectUnsavedDraft()
  await act(async () => csvGate!.release())
  await until(() => expect(button('Export CSV').disabled).toBe(false))
  expect(downloaded).toEqual([])
  await source('')
  expect(draftValue('model_id')).toBe('')
  expect(draftValue('period')).toBe('month')
  expect(reports().at(-1)!.url).toBe('/usage')
  await source('tea_two')
  expect(draftValue('model_id')).toBe('')
  expect(draftValue('period')).toBe('month')
})
it.each(['actor', 'Project target', 'scope'] as const)(
  'clears draft and applied filters after %s changes without restoring old scope intent',
  async (change) => {
    await mount({ projectId: 'prj_one' })
    await applyCustomThenDraft()
    if (change === 'actor') {
      actor = 'usr_two'
      await act(async () => cache.setQueryData(sessionKey, session()))
    } else {
      scope = change === 'Project target' ? { projectId: 'prj_two' } : { admin: true }
      await render()
    }
    await until(() => expect(button('Export CSV')?.disabled).toBe(false))
    expect(draftValue('model_id')).toBe('')
    expect(draftValue('period')).toBe('month')
    expect(draftValue('timezone')).toBe('UTC')
    expect(reports().at(-1)!.params).toEqual({
      period: 'month',
      timezone: 'UTC',
      granularity: 'auto',
      compare: false,
    })
    expect(exports()).toHaveLength(0)
    scope = { projectId: 'prj_one' }
    await render()
    await until(() => expect(button('Export CSV')?.disabled).toBe(false))
    expect(draftValue('model_id')).toBe('')
    expect(draftValue('period')).toBe('month')
  },
)
