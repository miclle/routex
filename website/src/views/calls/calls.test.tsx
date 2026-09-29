import i18n from '@/i18n'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CallsPage from './index'
import client from '@/api/client'
import type { AdminCallDetail } from '@/types/calls'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root
let container: HTMLDivElement
let cache: QueryClient
let requests: InternalAxiosRequestConfig[]
let role: 'admin' | 'member'
let failNext: boolean
let failDetail: boolean
let failExport: boolean
let projectOwned: boolean
let exportHold: Promise<void> | undefined
let downloaded: { name: string; url: string } | undefined
const originalAdapter = client.defaults.adapter
const originalURL = URL
const detail: AdminCallDetail = {
  request_id: 'req_first',
  model_id: 'mdl_1',
  model_name: 'Public model',
  key_id: 'key_1',
  protocol: 'openai_chat',
  status: 'success',
  stream: true,
  started_at: '2026-09-23T00:00:00Z',
  completed_at: '2026-09-23T00:00:01Z',
  duration_ms: 1000,
  input_tokens: null,
  output_tokens: null,
  user_id: 'usr_1',
  provider_model_id: 'pm_private',
  connection_id: 'conn_private',
  error_code: 'diagnostic_private',
  route_stop_reason: 'succeeded',
  attempts: [
    {
      id: 'attempt_private',
      provider_model_id: 'pm_private',
      connection_id: 'conn_private',
      attempt_number: 1,
      failure_class: 'connection_failure',
      work_evidence: 'not_sent',
      output_started: false,
      final_usage_known: false,
      evidence_code: 'pre_request_connection',
      status: 'error',
      http_status: 0,
      error_code: 'upstream_error',
      started_at: '2026-09-23T00:00:00Z',
      completed_at: '2026-09-23T00:00:00.100Z',
    },
    {
      id: 'attempt_second',
      provider_model_id: 'pm_second',
      connection_id: 'conn_second',
      attempt_number: 2,
      failure_class: 'rate_limited',
      work_evidence: 'rejected_without_work',
      output_started: false,
      final_usage_known: false,
      evidence_code: 'native_rate_rejection',
      status: 'error',
      http_status: 429,
      error_code: 'rate_limit_exceeded',
      started_at: '2026-09-23T00:00:00.100Z',
      completed_at: '2026-09-23T00:00:00.200Z',
    },
    {
      id: 'attempt_third',
      provider_model_id: 'pm_private',
      connection_id: 'conn_private',
      attempt_number: 3,
      failure_class: 'success',
      work_evidence: 'completed',
      output_started: true,
      final_usage_known: true,
      evidence_code: 'upstream_response',
      status: 'success',
      http_status: 200,
      error_code: '',
      started_at: '2026-09-23T00:00:00.200Z',
      completed_at: '2026-09-23T00:00:01Z',
    },
  ],
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  role = 'admin'
  requests = []
  failNext = false
  failDetail = false
  failExport = false
  projectOwned = false
  exportHold = undefined
  downloaded = undefined
  vi.stubGlobal(
    'URL',
    class extends originalURL {
      static createObjectURL = vi.fn(() => 'blob:calls')
      static revokeObjectURL = vi.fn()
    },
  )
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    downloaded = { name: this.download, url: this.href }
  })
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
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
    if (config.url === '/auth/permissions') {
      response.data = { permissions: role === 'admin' ? ['calls.read_all'] : [] }
      return response
    }
    if (config.url === '/auth/session') {
      response.data = { user: { role, id: 'usr_1' }, csrf_token: 'csrf' }
      return response
    }
    if (config.url?.endsWith('/calls/export.csv')) {
      if (exportHold) await exportHold
      if (failExport) {
        response.status = 503
        throw new AxiosError('Failed', '', config, undefined, response)
      }
      response.data = new Blob(['request_id\nreq_first\n'], { type: 'text/csv' })
      return response
    }
    const isDetail = config.url?.endsWith('/req_first')
    if ((isDetail && failDetail) || (config.params?.cursor && failNext)) {
      response.status = 503
      throw new AxiosError('Failed', '', config, undefined, response)
    }
    const record = projectOwned ? { ...detail, user_id: '', project_id: 'prj_owned' } : detail
    response.data = isDetail
      ? record
      : {
          items: [{ ...record, request_id: config.params?.cursor ? 'req_second' : 'req_first' }],
          next_cursor: config.params?.cursor ? null : 'next-page',
        }
    return response
  }
})
afterEach(async () => {
  await act(async () => {
    root.unmount()
  })
  cache.clear()
  client.defaults.adapter = originalAdapter
  container.remove()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
async function render(admin = false, projectId?: string) {
  await act(async () => {
    root.render(
      <QueryClientProvider client={cache}>
        <CallsPage admin={admin} projectId={projectId} />
      </QueryClientProvider>,
    )
  })
}
async function until(assert: () => void) {
  for (let i = 0; i < 60; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 10))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 59) throw error
    }
  }
}
async function click(text: string) {
  await act(async () => {
    ;[...document.querySelectorAll('button')].find((el) => el.textContent === text)!.click()
  })
}
async function fill(name: string, value: string) {
  const input = container.querySelector<HTMLInputElement>(`[name="${name}"]`)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function select(name: string, value: string) {
  const input = container.querySelector<HTMLSelectElement>(`[name="${name}"]`)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function submitFilters() {
  await act(async () => {
    container
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}

describe('call records', () => {
  it('attributes administrative Project calls to the Project instead of an empty user', async () => {
    projectOwned = true
    await render(true)
    await until(() => expect(container.textContent).toContain('prj_owned'))
    expect(container.textContent).toContain('User / Project')
    await click('Details')
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Project ID'),
    )
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('prj_owned')
  })

  it('uses isolated Project queries and never shows administrative diagnostics', async () => {
    cache.setQueryData(['calls', 'self', {}], {
      pages: [{ items: [{ ...detail, request_id: 'personal_secret_record' }], next_cursor: null }],
      pageParams: [null],
    })
    await render(false, 'prj_1')
    await until(() => expect(container.textContent).toContain('req_first'))
    expect(container.textContent).toContain('Project call records')
    expect(container.textContent).not.toContain('personal_secret_record')
    expect(container.querySelector('[name="user_id"]')).toBeNull()
    await click('Details')
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain('req_first'),
    )
    expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('conn_private')
    expect(requests.some((request) => request.url === '/projects/prj_1/calls')).toBe(true)
    expect(requests.some((request) => request.url === '/projects/prj_1/calls/req_first')).toBe(true)
    expect(
      requests.some((request) => request.url === '/calls' || request.url?.startsWith('/admin')),
    ).toBe(false)
    expect(cache.getQueryData(['calls', 'project', 'prj_1', {}])).toBeDefined()
  })

  it('switches visible status and accessibility labels without changing request identity', async () => {
    await render()
    await until(() => expect(container.textContent).toContain('req_first'))
    expect(container.querySelector('[aria-label="View req_first"]')).not.toBeNull()
    expect(container.textContent).toContain('Export CSV')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(container.textContent).toContain('我的调用记录')
    expect(container.textContent).toContain('未返回 / 未返回')
    expect(container.querySelector('[aria-label="查看 req_first"]')).not.toBeNull()
    expect(container.textContent).toContain('导出 CSV')
    expect(container.textContent).toContain('req_first')
    expect(container.textContent).toContain(new Date(detail.started_at).toLocaleString('zh-CN'))
  })
  it('renders missing usage distinctly and keeps administrator diagnostics out of member details', async () => {
    await render()
    await until(() => expect(container.textContent).toContain('req_first'))
    expect(container.textContent).toContain('Not returned / Not returned')
    expect(container.querySelector('[name="user_id"]')).toBeNull()
    await click('Details')
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain('req_first'),
    )
    expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('conn_private')
    expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('attempt_private')
    expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain(
      'Connection failed before request transmission',
    )
    expect(requests.some((r) => r.url === '/calls/req_first')).toBe(true)
    expect(requests.some((r) => r.url?.startsWith('/admin'))).toBe(false)
  })
  it('shows attempts and upstream identifiers only in the administrator detail view', async () => {
    await render(true)
    await until(() => expect(container.textContent).toContain('req_first'))
    expect(container.querySelector('[name="user_id"]')).not.toBeNull()
    await click('Details')
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain('attempt_private'),
    )
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('conn_private')
    const table = document.querySelector('[aria-label="Upstream attempt diagnostics"]')
    expect(table).not.toBeNull()
    const content = table?.textContent ?? ''
    expect(content).toContain('Connection failed before request transmission')
    expect(content).toContain('Native rate rejection')
    expect(content).toContain('Final usage known: Yes')
    expect(content.indexOf('attempt_private')).toBeLessThan(content.indexOf('attempt_second'))
    expect(content.indexOf('attempt_second')).toBeLessThan(content.indexOf('attempt_third'))
    expect(requests.some((r) => r.url === '/admin/calls/req_first')).toBe(true)
  })
  it('does not fetch the administrative list for members', async () => {
    role = 'member'
    await render(true)
    await until(() => expect(container.textContent).toContain('Access denied'))
    expect(requests.filter((r) => r.url?.includes('/calls'))).toHaveLength(0)
  })
  it('preserves loaded rows when pagination fails and retries the same cursor', async () => {
    await render()
    await until(() => expect(container.textContent).toContain('req_first'))
    failNext = true
    await click('Load more')
    await until(() => expect(container.textContent).toContain('Retry loading more'))
    expect(container.textContent).toContain('req_first')
    failNext = false
    await click('Retry loading more')
    await until(() => expect(container.textContent).toContain('req_second'))
    expect(container.querySelectorAll('tbody tr')).toHaveLength(2)
    expect(requests.filter((r) => r.params?.cursor === 'next-page')).toHaveLength(2)
  })
  it('validates time order and sends normalized filters before resetting them', async () => {
    await render(true)
    await until(() => expect(container.textContent).toContain('req_first'))
    await fill('from', '2026-09-24T12:00')
    await fill('to', '2026-09-23T12:00')
    await act(async () => {
      container
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    expect(container.querySelector('[role="alert"]')?.textContent).toContain(
      'Start time cannot be later than end time',
    )
    await fill('to', '2026-09-25T12:00')
    await fill('user_id', 'usr_2')
    await fill('model_id', 'mdl_2')
    await act(async () => {
      container
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    await until(() => expect(requests.some((r) => r.params?.user_id === 'usr_2')).toBe(true))
    const params = requests.find((r) => r.params?.user_id === 'usr_2')!.params
    expect(params.model_id).toBe('mdl_2')
    expect(params.from).toBe(new Date('2026-09-24T12:00').toISOString())
    await click('Reset')
    expect(container.querySelector<HTMLInputElement>('[name="user_id"]')!.value).toBe('')
  })
  it('exports the complete personal filter scope without pagination parameters', async () => {
    await render()
    await until(() => expect(container.textContent).toContain('req_first'))
    await select('status', 'success')
    await fill('model_id', 'mdl_2')
    await fill('key_id', 'key_2')
    await fill('from', '2026-09-24T12:00')
    await submitFilters()
    await until(() =>
      expect(requests.some((request) => request.params?.model_id === 'mdl_2')).toBe(true),
    )
    await click('Export CSV')
    await until(() =>
      expect(downloaded).toEqual({ name: 'routex-personal-calls.csv', url: 'blob:calls' }),
    )
    const request = requests.find((item) => item.url === '/calls/export.csv')!
    expect(request.responseType).toBe('blob')
    expect(request.params).toEqual({
      status: 'success',
      model_id: 'mdl_2',
      key_id: 'key_2',
      from: new Date('2026-09-24T12:00').toISOString(),
    })
    expect(request.params).not.toHaveProperty('cursor')
    expect(request.params).not.toHaveProperty('limit')
    expect(request.params).not.toHaveProperty('user_id')
    await until(() => expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:calls'))
    expect(container.textContent).toContain('CSV download was prepared.')
  })
  it('uses the authorized platform export endpoint and administrator filter', async () => {
    await render(true)
    await until(() => expect(container.textContent).toContain('req_first'))
    await fill('user_id', 'usr_2')
    await submitFilters()
    await until(() =>
      expect(requests.some((request) => request.params?.user_id === 'usr_2')).toBe(true),
    )
    await click('Export CSV')
    await until(() => expect(downloaded?.name).toBe('routex-platform-calls.csv'))
    const request = requests.find((item) => item.url === '/admin/calls/export.csv')!
    expect(request.params).toEqual({ user_id: 'usr_2' })
    expect(requests.some((item) => item.url === '/calls/export.csv')).toBe(false)
  })
  it('uses an isolated Project export endpoint without placing its identity in query parameters', async () => {
    await render(false, 'prj_1')
    await until(() => expect(container.textContent).toContain('req_first'))
    await fill('key_id', 'key_project')
    await submitFilters()
    await until(() =>
      expect(requests.some((request) => request.params?.key_id === 'key_project')).toBe(true),
    )
    await click('Export CSV')
    await until(() => expect(downloaded?.name).toBe('routex-project-calls.csv'))
    const request = requests.find((item) => item.url === '/projects/prj_1/calls/export.csv')!
    expect(request.params).toEqual({ key_id: 'key_project' })
    expect(JSON.stringify(request.params)).not.toContain('prj_1')
    expect(
      requests.some(
        (item) => item.url === '/calls/export.csv' || item.url === '/admin/calls/export.csv',
      ),
    ).toBe(false)
  })
  it('keeps export transient, blocks duplicate dispatch, and localizes failures', async () => {
    let release!: () => void
    exportHold = new Promise<void>((resolve) => {
      release = resolve
    })
    await render()
    await until(() => expect(container.textContent).toContain('req_first'))
    const button = [...container.querySelectorAll('button')].find(
      (element) => element.textContent === 'Export CSV',
    )!
    await act(async () => {
      button.click()
      button.click()
      await Promise.resolve()
    })
    expect(requests.filter((request) => request.url === '/calls/export.csv')).toHaveLength(1)
    expect(button.disabled).toBe(true)
    expect(button.textContent).toContain('Preparing CSV…')
    release()
    await until(() => expect(downloaded).toBeDefined())

    failExport = true
    await click('Export CSV')
    await until(() =>
      expect(container.querySelector('[role="alert"]')?.textContent).toContain('The action failed'),
    )
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('操作失败')
  })
  it('can retry a failed detail fetch', async () => {
    failDetail = true
    await render()
    await until(() => expect(container.textContent).toContain('req_first'))
    await click('Details')
    await until(() =>
      expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull(),
    )
    failDetail = false
    projectOwned = false
    await click('Retry')
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain('req_first'),
    )
  })
})
