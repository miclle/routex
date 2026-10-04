import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, expect, it } from 'vitest'
import i18n from '@/i18n'
import client from '@/api/client'
import RequestPanel from './requests'
import type { PersonalModelRequestDetail } from '@/types/personal-model-requests'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  requests: InternalAxiosRequestConfig[],
  allowed: boolean,
  generation: string,
  hold: { promise: Promise<void>; release: () => void } | null
const actor = 'usr_reviewer',
  owner = 'usr_owner',
  time = '2026-10-04T00:00:00Z'
let detail: PersonalModelRequestDetail
beforeEach(async () => {
  await i18n.changeLanguage('en')
  requests = []
  allowed = true
  generation = 'first'
  hold = null
  detail = {
    id: 'mar_request',
    request_id: '11111111-1111-4111-8111-111111111111',
    applicant_user_id: owner,
    applicant_name: 'Recorded owner',
    model_id: 'mdl_model',
    model_name: 'Recorded model',
    reason: 'Original reason',
    status: 'pending',
    created_at: time,
    updated_at: time,
    resolved_at: null,
    cancelled_reason: null,
    decision: null,
    current_model: { id: 'mdl_model', name: 'Current model', status: 'active' },
    current_granted: false,
    review_etag: 'a'.repeat(64),
    allowed_actions: ['approve', 'reject'],
    runtime_applied: false,
    application_status: 'pending',
  }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  cache.setQueryData(['auth', 'session'], {
    user: { id: actor, role: 'admin', name: 'Reviewer' },
    csrf_token: 'first-csrf',
  })
  cache.setQueryData(['permissions', actor], ['members.models.write'])
  client.defaults.adapter = async (c) => {
    requests.push(c)
    if (c.method === 'post') {
      if (hold) await hold.promise
      throw new AxiosError('controlled uncertainty', '', c, undefined, {
        config: c,
        status: 503,
        statusText: '',
        headers: new AxiosHeaders(),
        data: { message: 'Controlled failure' },
      })
    }
    return {
      config: c,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: c.url!.endsWith('/model-requests')
        ? { items: [detail], total: 1, next_cursor: null }
        : detail,
    }
  }
})
afterEach(async () => {
  hold?.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
})
async function settle() {
  for (let i = 0; i < 8; i++)
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RequestPanel
          owner={owner}
          visible={allowed}
          managed={{ actor, generation, canRead: () => allowed }}
        />
      </QueryClientProvider>,
    ),
  )
  await settle()
}
const button = (label: string) =>
  Array.from(document.querySelectorAll<HTMLButtonElement>('button'))
    .filter((b) => b.textContent === label)
    .at(-1)!
async function click(label: string) {
  expect(button(label)).toBeTruthy()
  await act(async () => button(label).click())
  await settle()
}
const posts = () => requests.filter((c) => c.method === 'post')
it('reads request history/details with managed authority and zero Session observers', async () => {
  await render()
  await click('Request details')
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Original reason')
  expect(requests.every((c) => c.url?.includes(`/admin/members/${owner}/model-requests`))).toBe(
    true,
  )
  expect(posts()).toHaveLength(0)
})
it('hides and aborts obsolete decision; reauthorizing retains exact original intent and current CSRF', async () => {
  await render()
  await click('Request details')
  await click('Approve')
  let release!: () => void
  hold = { promise: new Promise<void>((r) => (release = r)), release: () => release() }
  await click('Approve')
  expect(posts()).toHaveLength(1)
  const body = posts()[0].data,
    review = posts()[0].headers['If-Match']
  allowed = false
  generation = 'renewing'
  await render()
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(posts()[0].signal?.aborted).toBe(true)
  hold.release()
  await settle()
  hold = null
  cache.setQueryData(['auth', 'session'], { user: { id: actor }, csrf_token: 'renewed-csrf' })
  allowed = true
  generation = 'renewed'
  await render()
  await click('Retry original request')
  expect(posts()).toHaveLength(2)
  expect(posts()[1].data).toBe(body)
  expect(posts()[1].headers['If-Match']).toBe(review)
  expect(posts()[1].headers['X-CSRF-Token']).toBe('renewed-csrf')
  expect(requests.some((c) => c.url === '/auth/session')).toBe(false)
})
it('same-task closed authority blocks stale Details action and private portal', async () => {
  await render()
  const saved = button('Request details')
  allowed = false
  await act(async () => saved.click())
  await settle()
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(requests.filter((c) => c.url?.endsWith('/mar_request'))).toHaveLength(0)
})
