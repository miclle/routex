import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import { usageFixture } from '@/views/usage/fixture'
import type { InsightsReportDefinition } from '@/types/insights'
import type { Session } from '@/types/auth'
import InsightsPage from './index'
import { qualityFixture } from './provider-quality-fixture'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
const id = 'isr_01arz3ndektsv4rrffq69g5fav'
const from = '2026-09-01T00:00:00Z',
  to = '2026-09-02T00:00:00Z'
const definition = (): InsightsReportDefinition => ({
  id,
  kind: 'tokens_by_model',
  from,
  to,
  timezone: 'UTC',
  created_at: '2026-09-03T00:00:00Z',
})
let host: HTMLDivElement, root: Root, cache: QueryClient, requests: InternalAxiosRequestConfig[]
let actor: string, callsRead: boolean, providerRead: boolean, saveError: number, deleteError: number
let definitions: InsightsReportDefinition[], nextCursor: string
let saveGate: ReturnType<typeof deferred> | undefined,
  listGate: ReturnType<typeof deferred> | undefined,
  deleteGate: ReturnType<typeof deferred> | undefined
let gates: { release: () => void }[]
function deferred() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  const gate = { promise, release }
  gates.push(gate)
  return gate
}
const text = (key: string) => i18n.t('insights:' + key)
const session = (): Session => ({
  user: {
    id: actor,
    name: actor === 'usr_one' ? 'Current creator' : 'Other creator',
    email: '',
    role: 'member',
  },
  csrf_token: 'csrf-' + actor,
})
function failure(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('private-error-material', '', config, undefined, {
    config,
    status,
    data: { message: 'private-error-material' },
    statusText: '',
    headers: new AxiosHeaders(),
  })
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_one'
  callsRead = true
  providerRead = true
  saveError = deleteError = 0
  definitions = []
  nextCursor = ''
  requests = []
  gates = []
  saveGate = listGate = deleteGate = undefined
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown,
      status = 200
    if (config.url === '/auth/session') data = session()
    else if (config.url === '/auth/permissions')
      data = {
        permissions: [
          ...(callsRead ? ['calls.read_all'] : []),
          ...(providerRead ? ['providers.read'] : []),
        ],
      }
    else if (config.url === '/admin/insights/models') data = { items: [] }
    else if (config.url === '/admin/usage') {
      const report = usageFixture()
      report.current.from = config.params.from
      report.current.to = config.params.to
      report.timezone = config.params.timezone
      report.current.models[0].name = 'Saved report test facts'
      data = report
    } else if (config.url === '/admin/insights/provider-quality')
      data = { ...qualityFixture(), ...config.params }
    else if (config.url === '/insights/reports' && config.method === 'get') {
      data = { items: structuredClone(definitions), next_cursor: nextCursor }
      if (listGate) await listGate.promise
    } else if (config.url === '/insights/reports' && config.method === 'post') {
      const body = JSON.parse(config.data)
      if (saveGate) await saveGate.promise
      if (saveError) throw failure(config, saveError)
      const row: InsightsReportDefinition = {
        id,
        kind: body.kind,
        from: body.from,
        to: body.to,
        timezone: body.timezone,
        created_at: '2026-09-03T00:00:00Z',
      }
      definitions = [row]
      data = row
      status = 201
    } else if (config.url === '/insights/reports/' + id && config.method === 'delete') {
      if (deleteGate) await deleteGate.promise
      if (deleteError) throw failure(config, deleteError)
      definitions = []
      data = ''
      status = 204
    } else throw new Error('Unexpected endpoint ' + config.url)
    return { config, data, status, statusText: '', headers: new AxiosHeaders() }
  }
})
afterEach(async () => {
  await act(async () => {
    for (const gate of gates) gate.release()
  })
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
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
  const found = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (node) => node.textContent?.trim() === label || node.getAttribute('aria-label') === label,
  )
  if (!found) throw new Error('Missing button ' + label)
  return found
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function input(name: string, value: string) {
  const field = host.querySelector<HTMLInputElement>('[name="' + name + '"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(field, value)
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
  await until(() => expect(host.textContent).toContain(text('savedTitle')))
  await until(() => expect(host.textContent).not.toContain(text('savedLoading')))
}
async function run() {
  await input('from', from)
  await input('to', to)
  await click(text('reviewRun'))
  await until(() => expect(button(text('saveReport')).disabled).toBe(false))
}
const saves = () =>
  requests.filter((config) => config.url === '/insights/reports' && config.method === 'post')
const deletes = () => requests.filter((config) => config.method === 'delete')
const reports = () =>
  requests.filter((config) =>
    ['/admin/usage', '/admin/insights/provider-quality'].includes(config.url!),
  )
async function menu() {
  const label = i18n.t('insights:savedActions', {
    id: definitions[0].id,
    title: text(definitions[0].kind === 'provider_quality' ? 'providerReportTitle' : 'reportTitle'),
  })
  await click(label)
  await until(() => expect(document.querySelector('[role="menuitem"]')).not.toBeNull())
}
async function menuItem(label: string) {
  const item = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
    (node) => node.textContent?.trim() === label,
  )!
  expect(item).toBeDefined()
  await act(async () => item.click())
}
it('preserves the four-column layout, saves only a successful deterministic capture and never stores results or questions', async () => {
  await mount()
  expect(button(text('saveReport')).disabled).toBe(true)
  expect(host.querySelectorAll('thead th')).toHaveLength(4)
  await run()
  await click(text('saveReport'))
  await until(() => expect(host.textContent).toContain(text('savedConfirmed')))
  const body = JSON.parse(saves()[0].data)
  expect(body).toEqual({
    request_id: expect.stringMatching(/^[0-9a-f-]{36}$/),
    kind: 'tokens_by_model',
    from,
    to,
    timezone: 'UTC',
  })
  expect(saves()[0].headers.get('X-CSRF-Token')).toBe('csrf-usr_one')
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
  await until(() => expect(host.querySelector('tbody')?.textContent).toContain('Current creator'))
  await input('to', '2026-09-04T00:00:00Z')
  expect(button(text('saveReport')).disabled).toBe(true)
  expect(saves()).toHaveLength(1)
})
it('keeps the original mounted save intent through unknown outcomes and blocks duplicate dispatch', async () => {
  await mount()
  await run()
  saveGate = deferred()
  saveError = 503
  const saveButton = button(text('saveReport'))
  await act(async () => {
    saveButton.click()
    saveButton.click()
  })
  expect(saves()).toHaveLength(1)
  const gate = saveGate
  await act(async () => gate.release())
  await until(() => expect(host.textContent).toContain(text('saveUnknown')))
  expect(host.textContent).not.toContain('private-error-material')
  const originalBody = saves()[0].data
  await input('to', '2026-09-04T00:00:00Z')
  saveGate = undefined
  saveError = 0
  await click(text('retrySave'))
  await until(() => expect(host.textContent).toContain(text('savedConfirmed')))
  expect(saves()[1].data).toBe(originalBody)
  expect(reports()).toHaveLength(1)
})
it.each([409, 410])(
  'retains the original rejected %i intent until explicit discard, without automatic retry',
  async (status) => {
    await mount()
    await run()
    saveError = status
    await click(text('saveReport'))
    await until(() =>
      expect(host.textContent).toContain(text(status === 409 ? 'saveConflict' : 'saveDeleted')),
    )
    expect(saves()).toHaveLength(1)
    await click(text('discardSave'))
    expect(host.textContent).not.toContain(text('retrySave'))
    expect(saves()).toHaveLength(1)
  },
)
it('loads parameters for review, clears previous facts and never automatically queries or infers', async () => {
  definitions = [definition()]
  await mount()
  await run()
  expect(host.textContent).toContain('Saved report test facts')
  const count = requests.length
  await menu()
  await menuItem(text('loadSaved'))
  expect(host.textContent).toContain(text('savedLoaded'))
  expect(host.textContent).not.toContain('Saved report test facts')
  expect(host.querySelector<HTMLInputElement>('[name="from"]')?.value).toBe(from)
  expect(host.querySelector<HTMLInputElement>('[name="to"]')?.value).toBe(to)
  expect(requests).toHaveLength(count)
  expect(button(text('saveReport')).disabled).toBe(true)
})
it('lists and deletes owner definitions without call-read authority, with a required explicit confirmation', async () => {
  callsRead = false
  definitions = [definition()]
  await mount()
  expect(host.textContent).toContain(text('denied'))
  expect(host.querySelector('form')).toBeNull()
  await menu()
  const load = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
    (node) => node.textContent === text('loadSaved'),
  )!
  expect(load.getAttribute('aria-disabled')).toBe('true')
  await menuItem(text('deleteSaved'))
  await until(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull())
  expect(deletes()).toHaveLength(0)
  await click(text('confirmDeleteSaved'))
  await until(() => expect(host.textContent).toContain(text('deleteConfirmed')))
  expect(deletes()).toHaveLength(1)
  expect(reports()).toHaveLength(0)
  expect(saves()).toHaveLength(0)
  await until(() => expect(host.textContent).toContain(text('savedEmpty')))
})
it.each([404, 503])(
  'does not infer delete success from %i or list absence, and retries only explicitly',
  async (status) => {
    const errorKey = status === 404 ? 'deleteMissing' : 'deleteUnknown'
    definitions = [definition()]
    await mount()
    await menu()
    await menuItem(text('deleteSaved'))
    deleteError = status
    await click(text('confirmDeleteSaved'))
    await until(() => expect(document.body.textContent).toContain(text(errorKey)))
    expect(deletes()).toHaveLength(1)
    expect(host.textContent).not.toContain(text('deleteConfirmed'))
    definitions = []
    await act(async () => {
      await cache.refetchQueries({ queryKey: ['insights-saved-reports', actor] })
    })
    await until(() => expect(host.textContent).toContain(text('savedEmpty')))
    expect(document.body.textContent).toContain(text(errorKey))
    deleteError = 0
    await click(text('confirmDeleteSaved'))
    await until(() => expect(host.textContent).toContain(text('deleteConfirmed')))
    expect(deletes()).toHaveLength(2)
  },
)
it('blocks stale save completion after current report permission is lost while retaining owner reads', async () => {
  await mount()
  await run()
  saveGate = deferred()
  await click(text('saveReport'))
  const pending = saves()[0]
  callsRead = false
  // Locate the actual current permission key rather than relying on observer timing.
  await act(async () => {
    for (const query of cache
      .getQueryCache()
      .findAll({ queryKey: ['permissions', actor, 'insights'] }))
      cache.setQueryData(query.queryKey, [])
  })
  await until(() => expect(host.textContent).toContain(text('denied')))
  expect(pending.signal?.aborted).toBe(true)
  await act(async () => saveGate!.release())
  expect(host.textContent).not.toContain(text('savedConfirmed'))
  expect(host.textContent).not.toContain(text('retrySave'))
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})
it.each(['renewal', 'actor', 'error', 'unmount'])(
  'refuses stale list/save facts across Session %s',
  async (change) => {
    definitions = [definition()]
    await mount()
    await run()
    saveGate = deferred()
    await click(text('saveReport'))
    const pending = saves()[0]
    await act(async () => {
      if (change === 'unmount') root.render(null)
      else if (change === 'error')
        cache
          .getQueryCache()
          .find({ queryKey: sessionKey })!
          .setState({ status: 'error', error: new Error('session unavailable') })
      else {
        if (change === 'actor') actor = 'usr_two'
        cache.setQueryData(sessionKey, session())
      }
    })
    await until(() => expect(pending.signal?.aborted).toBe(true))
    await act(async () => saveGate!.release())
    expect(host.textContent).not.toContain(text('savedConfirmed'))
    expect(host.textContent).not.toContain(text('retrySave'))
    if (change === 'actor')
      await until(() => expect(host.textContent).not.toContain('Current creator'))
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  },
)
it('requires independent Provider read authority for loading and saving Provider definitions', async () => {
  definitions = [{ ...definition(), kind: 'provider_quality' }]
  providerRead = false
  await mount()
  await menu()
  const load = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
    (node) => node.textContent === text('loadSaved'),
  )!
  expect(load.getAttribute('aria-disabled')).toBe('true')
  expect(reports()).toHaveLength(0)
  expect(saves()).toHaveLength(0)
})
it('switches titles, privacy guidance and pending retry copy live between English and Chinese', async () => {
  definitions = [definition()]
  await mount()
  await run()
  saveError = 503
  await click(text('saveReport'))
  await until(() => expect(host.textContent).toContain(text('saveUnknown')))
  const body = saves()[0].data
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('已保存报表')
  expect(host.textContent).toContain(text('saveUnknown'))
  expect(button(text('retrySave')).disabled).toBe(false)
  expect(host.querySelectorAll('thead th')).toHaveLength(4)
  saveError = 0
  await click(text('retrySave'))
  await until(() => expect(host.textContent).toContain(text('savedConfirmed')))
  expect(saves()[1].data).toBe(body)
})
it('saves the actual Provider capture independently of token reports or inference', async () => {
  await mount()
  await click(text('shortcutProvider'))
  await run()
  await click(text('saveReport'))
  await until(() => expect(host.textContent).toContain(text('savedConfirmed')))
  expect(JSON.parse(saves()[0].data)).toMatchObject({
    kind: 'provider_quality',
    from,
    to,
    timezone: 'UTC',
  })
  expect(
    requests.some(
      (config) => config.url === '/admin/usage' || config.url === '/admin/insights/analyze',
    ),
  ).toBe(false)
})
it('rejects a late former-owner delete completion without revealing private rows or success', async () => {
  definitions = [definition()]
  await mount()
  await menu()
  await menuItem(text('deleteSaved'))
  deleteGate = deferred()
  await click(text('confirmDeleteSaved'))
  const pending = deletes()[0]
  actor = 'usr_two'
  definitions = []
  await act(async () => cache.setQueryData(sessionKey, session()))
  await until(() => expect(pending.signal?.aborted).toBe(true))
  await act(async () => deleteGate!.release())
  expect(host.textContent).not.toContain(text('deleteConfirmed'))
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(host.textContent).not.toContain('Current creator')
})
it('never restores a late list response captured for the old actor', async () => {
  listGate = deferred()
  definitions = [definition()]
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <InsightsPage />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(requests.some((config) => config.url === '/insights/reports')).toBe(true),
  )
  const pending = requests.find((config) => config.url === '/insights/reports')!
  const oldGate = listGate
  listGate = undefined
  definitions = []
  actor = 'usr_two'
  await act(async () => cache.setQueryData(sessionKey, session()))
  await until(() => expect(pending.signal?.aborted).toBe(true))
  await act(async () => oldGate.release())
  await until(() => expect(host.textContent).toContain(text('savedEmpty')))
  expect(host.textContent).not.toContain('Current creator')
  expect(host.querySelectorAll('tbody tr')).toHaveLength(1)
})
it('uses bounded cursor pages and never replaces the owner query with report authority', async () => {
  definitions = [definition()]
  nextCursor = 'opaque_page_2'
  callsRead = false
  await mount()
  await click(text('nextSaved'))
  await until(() =>
    expect(requests.some((config) => config.params?.cursor === 'opaque_page_2')).toBe(true),
  )
  await until(() => expect(button(text('previousSaved')).disabled).toBe(false))
  expect(
    requests
      .filter((config) => config.url === '/insights/reports')
      .every((config) => config.params.limit === 20),
  ).toBe(true)
  await click(text('previousSaved'))
  await until(() => expect(button(text('previousSaved')).disabled).toBe(true))
  expect(reports()).toHaveLength(0)
})
it.each([400, 422])(
  'distinguishes definite validation rejection %i from an unknown save outcome',
  async (status) => {
    await mount()
    await run()
    saveError = status
    await click(text('saveReport'))
    await until(() => expect(host.textContent).toContain(text('saveInvalid')))
    expect(host.textContent).not.toContain(text('saveUnknown'))
    expect(saves()).toHaveLength(1)
    await click(text('discardSave'))
    expect(saves()).toHaveLength(1)
  },
)
it('treats refreshed list presence only as a current fact while retaining an uncertain save intent', async () => {
  await mount()
  await run()
  saveError = 503
  await click(text('saveReport'))
  await until(() => expect(host.textContent).toContain(text('saveUnknown')))
  const originalBody = saves()[0].data
  definitions = [definition()]
  await click(text('refreshSaved'))
  await until(() => expect(host.querySelector('tbody')?.textContent).toContain('Current creator'))
  expect(host.textContent).toContain(text('saveUnknown'))
  expect(host.textContent).not.toContain(text('savedConfirmed'))
  expect(button(text('retrySave')).disabled).toBe(false)
  saveError = 0
  await click(text('retrySave'))
  await until(() => expect(host.textContent).toContain(text('savedConfirmed')))
  expect(saves()[1].data).toBe(originalBody)
})
