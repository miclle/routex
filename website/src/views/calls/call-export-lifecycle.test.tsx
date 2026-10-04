import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import CallsPage from './index'
import client from '@/api/client'
import { exportCalls } from '@/api/calls'
import i18n from '@/i18n'

vi.mock('@/api/calls', async (original) => ({
  ...(await original<typeof import('@/api/calls')>()),
  exportCalls: vi.fn(),
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const adapter = client.defaults.adapter
let root: Root, host: HTMLDivElement, cache: QueryClient
let requests: InternalAxiosRequestConfig[]
let actor: string, listDenied: boolean, sessionDenied: boolean
let exports: {
  signal: AbortSignal
  resolve: (blob: Blob) => void
  reject: (error: Error) => void
}[]
let clicks: { name: string; url: string }[]
const call = {
  request_id: 'req_team_own',
  model_id: 'mdl_one',
  model_name: 'Team model',
  key_id: '',
  protocol: 'openai_chat',
  status: 'success',
  stream: false,
  started_at: '2026-10-05T00:00:00Z',
  completed_at: '2026-10-05T00:00:01Z',
  duration_ms: 1000,
  input_tokens: 0,
  output_tokens: null,
}
const session = { user: { id: 'usr_member', role: 'member' }, csrf_token: 'csrf_test' }
const blob = new Blob(['request_id\nreq_team_own\n'], { type: 'text/csv' })
beforeEach(async () => {
  actor = 'usr_member'
  listDenied = false
  sessionDenied = false
  requests = []
  exports = []
  clicks = []
  await i18n.changeLanguage('en')
  vi.spyOn(Date, 'now').mockReturnValue(1_791_158_400_000)
  vi.stubGlobal('URL', {
    createObjectURL: vi.fn(() => 'blob:controlled'),
    revokeObjectURL: vi.fn(),
  })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    clicks.push({ name: this.download, url: this.href })
  })
  vi.mocked(exportCalls).mockImplementation(
    (_admin, _filters, signal) =>
      new Promise((resolve, reject) => {
        // Deliberately ignores abort, proving the page independently fences late bytes.
        exports.push({ signal: signal!, resolve, reject })
      }),
  )
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const denied =
      config.url === '/auth/session' ? sessionDenied : config.url?.endsWith('/calls') && listDenied
    const response = {
      config,
      status: denied ? 403 : 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (denied) throw new AxiosError('Access denied', '', config, undefined, response)
    response.data =
      config.url === '/auth/session'
        ? actor === session.user.id
          ? session
          : { ...session, user: { ...session.user, id: actor } }
        : config.url === '/auth/permissions'
          ? { permissions: ['calls.read_all'] }
          : { items: [call], next_cursor: null }
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = adapter
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.mocked(exportCalls).mockReset()
})
async function settled(check: () => void) {
  await vi.waitFor(async () => {
    await act(async () => {})
    check()
  })
}
async function render(
  props: { teamId?: string; projectId?: string; admin?: boolean } = { teamId: 'tea_one' },
) {
  // Other scopes retain their existing disabled Session subscription under AuthGate.
  if (!props.teamId) cache.setQueryData(['auth', 'session'], session)
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <CallsPage {...props} />
      </QueryClientProvider>,
    ),
  )
  await settled(() => expect(host.textContent).toContain('req_team_own'))
}
function button(label = 'Export CSV') {
  const result = [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === label,
  )
  expect(result).toBeDefined()
  return result!
}
async function click(label = 'Export CSV') {
  await act(async () => button(label).click())
}
async function resolve(index = 0) {
  const before = clicks.length
  const revoked = vi.mocked(URL.revokeObjectURL).mock.calls.length
  await act(async () => exports[index].resolve(blob))
  if (clicks.length > before)
    await settled(() => expect(URL.revokeObjectURL).toHaveBeenCalledTimes(revoked + 1))
}

async function renewSession() {
  await act(async () => cache.refetchQueries({ queryKey: ['auth', 'session'], exact: true }))
}
async function filterModel(value: string) {
  await act(async () => {
    const input = host.querySelector<HTMLInputElement>('[name="model_id"]')!
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
  await settled(() => expect(requests.some((r) => r.params?.model_id === value)).toBe(true))
  await settled(() => expect(button().disabled).toBe(false))
}
it('uses the existing English action, captured applied filters, duplicate lock and live Chinese status', async () => {
  await render()
  await filterModel('mdl_reviewed')
  const exportButton = button()
  await act(async () => {
    exportButton.click()
    exportButton.click()
  })
  expect(exports).toHaveLength(1)
  expect(vi.mocked(exportCalls).mock.calls[0]).toEqual([
    false,
    { model_id: 'mdl_reviewed' },
    exports[0].signal,
    undefined,
    'tea_one',
  ])
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('正在准备 CSV')
  expect(host.querySelector<HTMLInputElement>('[name="model_id"]')!.value).toBe('mdl_reviewed')
  await resolve()
  expect(clicks).toEqual([{ name: 'routex-team-calls.csv', url: 'blob:controlled' }])
  await settled(() => expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:controlled'))
  expect(document.querySelector('a[download]')).toBeNull()
})
it('discards a same-data same-millisecond renewal Blob even after the new Session and scoped list succeed', async () => {
  await render()
  await click()
  const before = cache.getQueryState(['auth', 'session'])!
  const reads = requests.filter((r) => r.url === '/teams/tea_one/calls').length
  await renewSession()
  await settled(() =>
    expect(requests.filter((r) => r.url === '/teams/tea_one/calls')).toHaveLength(reads + 1),
  )
  await settled(() => expect(button().disabled).toBe(false))
  const after = cache.getQueryState(['auth', 'session'])!
  expect(after.data).toBe(before.data)
  expect(after.dataUpdatedAt).toBe(before.dataUpdatedAt)
  expect(after.dataUpdateCount).toBe(before.dataUpdateCount + 1)
  expect(exports[0].signal.aborted).toBe(true)
  await resolve()
  expect(clicks).toHaveLength(0)
  expect(URL.createObjectURL).not.toHaveBeenCalled()
  expect(exports).toHaveLength(1)
  await click()
  await resolve(1)
  expect(clicks).toHaveLength(1)
  expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2)
})
it.each(['membership', 'session'] as const)(
  'hides private Team facts and rejects late bytes after %s denial',
  async (kind) => {
    await render()
    await click()
    if (kind === 'membership') {
      listDenied = true
      await click('Refresh')
    } else {
      sessionDenied = true
      await renewSession()
    }
    await settled(() => expect(host.textContent).not.toContain('req_team_own'))
    expect(exports[0].signal.aborted).toBe(true)
    await resolve()
    expect(clicks).toHaveLength(0)
  },
)
it.each(['actor', 'Team', 'unmount'] as const)(
  'aborts and discards a previous %s scope',
  async (change) => {
    await render()
    await click()
    if (change === 'actor') {
      actor = 'usr_other'
      await renewSession()
      await settled(() => expect(button().disabled).toBe(false))
    } else if (change === 'Team') await render({ teamId: 'tea_two' })
    else await act(async () => root.render(null))
    expect(exports[0].signal.aborted).toBe(true)
    await resolve()
    expect(clicks).toHaveLength(0)
  },
)
it('blocks fresh export before a renewed scoped read completes', async () => {
  await render()
  let release!: () => void
  const held = new Promise<void>((resolve) => {
    release = resolve
  })
  const previous = client.defaults.adapter as Exclude<typeof client.defaults.adapter, undefined>
  client.defaults.adapter = async (config) => {
    if (config.url === '/teams/tea_one/calls') await held
    return (previous as (config: InternalAxiosRequestConfig) => Promise<never>)(config)
  }
  await act(async () => {
    void cache.refetchQueries({ queryKey: ['calls', 'team'] })
  })
  await settled(() => expect(host.textContent).not.toContain('req_team_own'))
  expect(button().disabled).toBe(true)
  await click()
  expect(exports).toHaveLength(0)
  await act(async () => release())
  await settled(() => expect(button().disabled).toBe(false))
})
it.each([
  [{}, 'routex-personal-calls.csv'],
  [{ projectId: 'prj_one' }, 'routex-project-calls.csv'],
  [{ admin: true }, 'routex-platform-calls.csv'],
] as const)(
  'preserves %j download contracts while discarding obsolete filters',
  async (props, filename) => {
    await render(props)
    await click()
    await filterModel('mdl_next')
    expect(exports[0].signal.aborted).toBe(true)
    await resolve()
    expect(clicks).toHaveLength(0)
    await click()
    await resolve(1)
    expect(clicks[0].name).toBe(filename)
  },
)
it('does not accept a late rejection as the current intent or replay on refresh', async () => {
  await render()
  await click()
  await filterModel('mdl_next')
  await act(async () => exports[0].reject(new Error('Obsolete private error')))
  expect(host.textContent).not.toContain('Obsolete private error')
  expect(host.querySelector('[role="alert"]')).toBeNull()
  expect(exports).toHaveLength(1)
})

it('discards late Personal bytes after unmount independently of transport abort', async () => {
  await render({})
  await click()
  await act(async () => root.render(null))
  await resolve()
  expect(clicks).toHaveLength(0)
  expect(URL.createObjectURL).not.toHaveBeenCalled()
})

it('does not let an obsolete callback unlock a newer explicit Team export', async () => {
  await render()
  await click()
  await renewSession()
  await settled(() => expect(button().disabled).toBe(false))
  await click()
  await resolve(0)
  expect(exports).toHaveLength(2)
  expect(button('Preparing CSV…').disabled).toBe(true)
  expect(clicks).toHaveLength(0)
  await resolve(1)
  expect(clicks).toHaveLength(1)
})

it('aborts an exact pending download when logout removes private query resources', async () => {
  await render()
  await click()
  await act(async () => cache.clear())
  expect(exports[0].signal.aborted).toBe(true)
  await resolve()
  expect(clicks).toHaveLength(0)
  expect(URL.createObjectURL).not.toHaveBeenCalled()
})
