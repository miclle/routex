import i18n from '@/i18n'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
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
const originalAdapter = client.defaults.adapter
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
  attempts: [
    {
      id: 'attempt_private',
      provider_model_id: 'pm_private',
      connection_id: 'conn_private',
      status: 'success',
      http_status: 200,
      error_code: '',
      started_at: '2026-09-23T00:00:00Z',
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
    const isDetail = config.url?.endsWith('/req_first')
    if ((isDetail && failDetail) || (config.params?.cursor && failNext)) {
      response.status = 503
      throw new AxiosError('Failed', '', config, undefined, response)
    }
    response.data = isDetail
      ? detail
      : {
          items: [{ ...detail, request_id: config.params?.cursor ? 'req_second' : 'req_first' }],
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
})
async function render(admin = false) {
  await act(async () => {
    root.render(
      <QueryClientProvider client={cache}>
        <CallsPage admin={admin} />
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

describe('call records', () => {
  it('switches visible status and accessibility labels without changing request identity', async () => {
    await render()
    await until(() => expect(container.textContent).toContain('req_first'))
    expect(container.querySelector('[aria-label="View req_first"]')).not.toBeNull()
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(container.textContent).toContain('我的调用记录')
    expect(container.textContent).toContain('未返回 / 未返回')
    expect(container.querySelector('[aria-label="查看 req_first"]')).not.toBeNull()
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
  it('can retry a failed detail fetch', async () => {
    failDetail = true
    await render()
    await until(() => expect(container.textContent).toContain('req_first'))
    await click('Details')
    await until(() =>
      expect(document.querySelector('[role="dialog"] [role="alert"]')).not.toBeNull(),
    )
    failDetail = false
    await click('Retry')
    await until(() =>
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain('req_first'),
    )
  })
})
