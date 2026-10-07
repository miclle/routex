import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it } from 'vitest'
import i18n from '@/i18n'
import client from '@/api/client'
import ProjectLimits from './project'
import { limitFixture } from './fixture'
import type { LimitRecord } from '@/types/resource-limits'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  record: LimitRecord,
  requests: InternalAxiosRequestConfig[],
  status: number
const adapter = client.defaults.adapter
const targetKey = ['resources', 'projects', false, 'prj_exact', 'usr_actor']
function authority(permissions = ['projects.limits.write'], actor = 'usr_actor') {
  cache.setQueryData(['auth', 'session'], {
    user: { id: actor, role: 'member', name: 'Actor' },
    csrf_token: 'current-csrf',
  })
  cache.setQueryData(['permissions', 'usr_actor'], permissions)
  cache.setQueryData(targetKey, {
    id: 'prj_exact',
    name: 'Project',
    status: 'active',
    managers: [{ user_id: 'usr_actor' }],
  })
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  requests = []
  status = 200
  record = { ...limitFixture(), kind: 'project', id: 'prj_exact', account_id: 'project_prj_exact' }
  record.stored.tokens_month = 100
  record.stored.money_month = '0.000000000000000001'
  record.stored.currency = 'USD'
  record.stored.tokens_month_behavior = 'stop'
  record.stored.money_month_behavior = 'alert_only'
  record.ip_policies = [{ ...record.stored }]
  record.enforced = true
  authority()
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data = record
    if (config.method === 'put') {
      const body = JSON.parse(config.data)
      delete body.reason
      data = { ...record, stored: body, ip_policies: [body], enforced: true }
    }
    const response = { config, status, statusText: '', headers: new AxiosHeaders(), data }
    if (config.method === 'put' && status !== 200)
      throw new AxiosError('Controlled response', '', config, undefined, response)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  client.defaults.adapter = adapter
  host.remove()
})
async function until(assert: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 10))
    })
    try {
      assert()
      return
    } catch (e) {
      if (i === 99) throw e
    }
  }
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <ProjectLimits
          actor="usr_actor"
          target="prj_exact"
          generation={1}
          ready
          targetQueryKey={targetKey}
        />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain('project_prj_exact'))
}
function button(text: string) {
  const found = [...document.querySelectorAll('button')].find((e) => e.textContent === text)
  expect(found, text).toBeDefined()
  return found!
}
async function click(text: string) {
  await act(async () => button(text).click())
}
async function input(label: string, value: string) {
  const el = document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
  expect(el, label).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value)
    el.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
function puts() {
  return requests.filter((r) => r.method === 'put')
}
it('confirms independent modes with zero and exact money, and keeps immutable retry/current CSRF', async () => {
  await mount()
  await click('Edit limits')
  await input('Monthly token quota', '0')
  await input('Reason for change', 'original reason')
  const token = document.querySelector<HTMLButtonElement>(
    '[role="switch"][aria-label="Monthly token threshold behavior"]',
  )!
  expect(token).toBeTruthy()
  await act(async () => token.click())
  await click('Save limits')
  expect(puts()).toHaveLength(0)
  expect(document.body.textContent).toContain('Confirm Project monthly behavior')
  status = 503
  await click('Confirm limits')
  await until(() => expect(document.body.textContent).toContain('may already'))
  const raw = puts()[0].data
  expect(JSON.parse(raw)).toMatchObject({
    tokens_month: 0,
    money_month: '0.000000000000000001',
    tokens_month_behavior: 'alert_only',
    money_month_behavior: 'alert_only',
    reason: 'original reason',
  })
  await act(async () => authority())
  await until(() => expect(document.body.textContent).toContain('Retry application'))
  status = 200
  await click('Retry application')
  await until(() => expect(puts()).toHaveLength(2))
  expect(puts()[1].data).toBe(raw)
  expect(puts()[1].headers.get('X-CSRF-Token')).toBe('current-csrf')
})
it('hides private form during authority renewal and blocks revoked queued confirmation', async () => {
  await mount()
  await click('Edit limits')
  await input('Monthly token quota', '150')
  await input('Reason for change', 'review draft')
  await click('Save limits')
  await act(async () => {
    cache.setQueryData(['permissions', 'usr_actor'], [])
    cache.setQueryData(targetKey, { id: 'prj_exact', status: 'active', managers: [] })
  })
  await until(() =>
    expect(document.body.textContent).not.toContain('Confirm Project monthly behavior'),
  )
  expect(puts()).toHaveLength(0)
  await act(async () => authority())
  await until(() => expect(document.body.textContent).toContain('Confirm Project monthly behavior'))
  await act(async () => i18n.changeLanguage('zh'))
  await until(() => expect(document.body.textContent).toContain('确认 Project 月度阈值行为'))
  expect(puts()).toHaveLength(0)
  await act(async () => authority([], 'usr_other'))
  await until(() => expect(host.textContent).not.toContain('project_prj_exact'))
  expect(puts()).toHaveLength(0)
})
it('allows manager read without write but never shows mode controls or dispatches a PUT', async () => {
  authority([])
  await mount()
  expect(host.textContent).toContain('Alert only')
  expect([...host.querySelectorAll('button')].some((b) => b.textContent === 'Edit limits')).toBe(
    false,
  )
  expect(puts()).toHaveLength(0)
})
