import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import client from '@/api/client'
import ResourceLimits from './index'
import { teamFixture } from './fixture'
import type { LimitRecord, TeamLimitScope } from '@/types/resource-limits'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let host: HTMLDivElement, root: Root, cache: QueryClient
let record: LimitRecord,
  requests: InternalAxiosRequestConfig[],
  putStatus: number,
  getStatus: number
let hold: Promise<void> | undefined, getHold: Promise<void> | undefined
let malformed: boolean
let actor: string, csrf: string
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 60000 }, mutations: { retry: false } },
  })
  actor = 'usr_member'
  csrf = 'csrf-old'
  record = teamFixture()
  requests = []
  putStatus = 0
  getStatus = 0
  hold = undefined
  getHold = undefined
  malformed = false
  cache.setQueryData(['auth', 'session'], session())
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session') response.data = session()
    else if (config.url?.endsWith('/limits')) {
      if (config.method === 'get' && getHold) await getHold
      if (config.method === 'put' && hold) await hold
      const status = config.method === 'put' ? putStatus : getStatus
      if (status) {
        response.status = status
        throw new AxiosError('Fixture failure', '', config, undefined, response)
      }
      if (config.method === 'put') {
        const patch = JSON.parse(config.data)
        delete patch.reason
        record = {
          ...record,
          etag: 'c'.repeat(64),
          stored: {
            ...record.stored,
            ...patch,
            ...(patch.money_month === null ? { currency: '' } : {}),
          },
        }
      }
      if (record.kind === 'team') record.ip_policies = [structuredClone(record.stored)]
      else record.ip_policies[1] = structuredClone(record.stored)
      response.data = malformed ? { ...record, enforced: undefined } : structuredClone(record)
    }
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
})
function session() {
  return { user: { id: actor, name: 'Member', role: 'member' }, csrf_token: csrf }
}
async function until(assert: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
async function render(scope: TeamLimitScope = { teamId: 'tea_test' }) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <ResourceLimits path="/ignored" team={scope} canEdit />
      </QueryClientProvider>,
    ),
  )
}
function button(label: string) {
  const result = [...host.querySelectorAll('button')].find((item) => item.textContent === label)
  expect(result, label).toBeDefined()
  return result!
}
async function rawClick(label: string) {
  await act(async () => button(label).click())
}
async function confirm() {
  const control = [...document.body.querySelectorAll('button')].find(
    (item) => item.textContent === 'Confirm limits',
  )!
  expect(control).toBeTruthy()
  await act(async () => control.click())
}
async function click(label: string) {
  await rawClick(label)
  if (label === 'Adjust member resources')
    await until(() =>
      expect(host.querySelector('input[aria-label="Reason for change"]')).toBeTruthy(),
    )
  if (label === 'Save limits' && document.body.querySelector('[role="dialog"]')) await confirm()
}
async function toggle(label: string) {
  const control = host.querySelector<HTMLButtonElement>(`[role="switch"][aria-label="${label}"]`)!
  expect(control).toBeTruthy()
  await act(async () => control.click())
}

async function fill(label: string, value: string) {
  const input = host.querySelector<HTMLInputElement>(`[aria-label="${label}"]`)!
  expect(input).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
const puts = () => requests.filter((request) => request.method === 'put')
async function edit() {
  await until(() => expect(host.textContent).toContain('Budget and quotas'))
  await click('Edit limits')
}
async function draft() {
  await fill('Monthly token quota', '0')
  await fill('Reason for change', 'Reviewed Team policy')
}
describe('Team resource policies', () => {
  it('uses approved budget/rate cards, exact money, unknown usage and no IP or Key labels', async () => {
    await render()
    await until(() =>
      expect(host.textContent).toContain('999999999999999999.123456789012345678 USD'),
    )
    expect(host.textContent).toContain('Rate limits')
    expect(host.textContent).toContain('Quota usage is unavailable')
    expect(host.textContent).not.toContain('IP source')
    expect(host.textContent).not.toContain('Key')
    await edit()
    await draft()
    await click('Save limits')
    await until(() => expect(host.textContent).toContain('Limits saved and applied.'))
    expect(JSON.parse(puts()[0].data)).toEqual({ tokens_month: 0, reason: 'Reviewed Team policy' })
    expect(puts()[0].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
  })
  it('read-only owners receive no edit controls', async () => {
    record.editable_fields = []
    await render()
    await until(() => expect(host.textContent).toContain('Budget and quotas'))
    expect(host.textContent).not.toContain('Edit limits')
    expect(puts()).toHaveLength(0)
  })
  it('submits only editable changed dimensions and preserves exact money', async () => {
    record.editable_fields = ['money_month']
    await render()
    await edit()
    expect(host.querySelector<HTMLInputElement>('[aria-label="RPM"]')!.disabled).toBe(true)
    await fill('Monthly budget', '123456789012345678.123456789012345678')
    await fill('Reason for change', 'Exact money')
    await click('Save limits')
    await until(() => expect(puts()).toHaveLength(1))
    expect(JSON.parse(puts()[0].data)).toEqual({
      money_month: '123456789012345678.123456789012345678',
      currency: 'USD',
      reason: 'Exact money',
    })
  })
  it('clears aggregate money without submitting a currency', async () => {
    await render()
    await edit()
    await fill('Monthly budget', '')
    await fill('Reason for change', 'Remove local cap')
    await click('Save limits')
    await until(() => expect(host.textContent).toContain('Limits saved and applied.'))
    expect(JSON.parse(puts()[0].data)).toEqual({ money_month: null, reason: 'Remove local cap' })
  })
  it('member restrictions inherit and include only monthly/rate fields', async () => {
    record = teamFixture(true)
    await render({ teamId: 'tea_test', userId: 'usr_member' })
    await until(() => expect(host.textContent).toContain('Inherit parent'))
    await click('Adjust member resources')
    expect(host.querySelector('[aria-label="5-hour token quota"]')).toBeNull()
    expect(host.textContent).not.toContain('IP source')
    await fill('Monthly token quota', '0')
    await fill('Reason for change', 'Close member allowance')
    await click('Save limits')
    await until(() => expect(puts()).toHaveLength(1))
    expect(puts()[0].url).toBe('/teams/tea_test/members/usr_member/limits')
    expect(JSON.parse(puts()[0].data)).toEqual({
      tokens_month: 0,
      reason: 'Close member allowance',
    })
  })
  it('rejects member limits above the current parent without dispatch', async () => {
    record = teamFixture(true)
    await render({ teamId: 'tea_test', userId: 'usr_member' })
    await until(() => expect(host.textContent).toContain('Inherit parent'))
    await click('Adjust member resources')
    await fill('RPM', '61')
    await fill('Reason for change', 'Review')
    await click('Save limits')
    expect(host.textContent).toContain('cannot exceed the current Team maximum')
    expect(puts()).toHaveLength(0)
  })
  it('retries identical business intent with fresh same-actor CSRF after uncertainty', async () => {
    putStatus = 503
    await render()
    await edit()
    await draft()
    await click('Save limits')
    await until(() => expect(host.textContent).toContain('This change may already be saved'))
    csrf = 'csrf-new'
    await act(async () => {
      cache.setQueryData(['auth', 'session'], session())
    })
    putStatus = 0
    await click('Retry application')
    await until(() => expect(host.textContent).toContain('Limits saved and applied.'))
    expect(puts()[1].data).toBe(puts()[0].data)
    expect(puts()[1].headers.get('If-Match')).toBe(puts()[0].headers.get('If-Match'))
    expect(puts()[1].headers.get('X-CSRF-Token')).toBe('csrf-new')
  })
  it('rejected retry and explicit fresh review cannot rebase an uncertain intent', async () => {
    putStatus = 503
    await render()
    await edit()
    await draft()
    await click('Save limits')
    await until(() => expect(puts()).toHaveLength(1))
    await until(() => expect(host.textContent).toContain('This change may already be saved'))
    putStatus = 409
    await click('Retry application')
    await until(() => expect(puts()).toHaveLength(2))
    await until(() => expect(button('Retry application').disabled).toBe(false))
    record.etag = 'd'.repeat(64)
    record.platform_currency = 'EUR'
    await click('Reload current policy')
    await until(() => expect(host.textContent).toContain('Platform currency: EUR'))
    expect(button('Save limits').disabled).toBe(true)
    await click('Retry application')
    await until(() => expect(puts()).toHaveLength(3))
    expect(puts()[2].data).toBe(puts()[0].data)
    expect(puts()[2].headers.get('If-Match')).toBe(puts()[0].headers.get('If-Match'))
  })
  it('malformed 200 remains unknown and never reports applied', async () => {
    await render()
    await edit()
    await draft()
    malformed = true
    await click('Save limits')
    await until(() => expect(host.textContent).toContain('This change may already be saved'))
    expect(host.textContent).not.toContain('Limits saved and applied.')
    malformed = false
    await click('Retry application')
    await until(() => expect(host.textContent).toContain('Limits saved and applied.'))
    expect(puts()[1].data).toBe(puts()[0].data)
  })
  it('hides cached policy while refreshing and after denied authorization', async () => {
    await render()
    await until(() => expect(host.textContent).toContain('999999999999999999'))
    let release!: () => void
    getHold = new Promise((resolve) => {
      release = resolve
    })
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['resource-limits', 'team'] })
    })
    await until(() => expect(host.textContent).not.toContain('999999999999999999'))
    getStatus = 403
    await act(async () => {
      release()
    })
    await until(() => expect(host.textContent).not.toContain('Budget and quotas'))
    expect(puts()).toHaveLength(0)
  })
  it('always reauthorizes a fresh sixty-second cache on remount', async () => {
    await render()
    await until(() => expect(host.textContent).toContain('Budget and quotas'))
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <span>Other tab</span>
        </QueryClientProvider>,
      ),
    )
    getStatus = 403
    await render()
    await until(() =>
      expect(
        requests.filter((request) => request.url?.endsWith('/limits') && request.method === 'get'),
      ).toHaveLength(2),
    )
    expect(host.textContent).not.toContain('Budget and quotas')
  })
  it('drops a late write after actor changes and never repopulates prior actor cache', async () => {
    await render()
    await edit()
    await draft()
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await click('Save limits')
    await until(() => expect(puts()).toHaveLength(1))
    actor = 'usr_other'
    getStatus = 403
    await act(async () => {
      cache.setQueryData(['auth', 'session'], session())
    })
    cache.removeQueries({
      queryKey: ['resource-limits', 'team', 'usr_member', 'tea_test', 'aggregate'],
      exact: true,
    })
    await act(async () => {
      release()
    })
    await until(() => expect(host.textContent).not.toContain('Budget and quotas'))
    expect(host.textContent).not.toContain('Limits saved and applied.')
    expect(
      cache.getQueryData(['resource-limits', 'team', 'usr_member', 'tea_test', 'aggregate']),
    ).toBeUndefined()
  })
  it('does not restore a denied resource cache after a late successful write', async () => {
    await render()
    await edit()
    await draft()
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await click('Save limits')
    await until(() => expect(puts()).toHaveLength(1))
    getStatus = 403
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['resource-limits', 'team'] })
    })
    await until(() => expect(host.textContent).not.toContain('Reason for change'))
    await act(async () => {
      release()
    })
    expect(host.textContent).not.toContain('Limits saved and applied.')
    expect(
      cache.getQueryState(['resource-limits', 'team', 'usr_member', 'tea_test', 'aggregate'])
        ?.status,
    ).toBe('error')
  })
  it('preserves the draft through a definite conflict and explicit current currency review', async () => {
    putStatus = 409
    await render()
    await edit()
    await draft()
    await click('Save limits')
    await until(() => expect(host.textContent).toContain('The policy or resource changed'))
    record.etag = 'd'.repeat(64)
    record.platform_currency = 'EUR'
    await click('Reload current policy')
    await until(() => expect(host.textContent).toContain('Platform currency: EUR'))
    expect(host.querySelector<HTMLInputElement>('[aria-label="Monthly token quota"]')!.value).toBe(
      '0',
    )
    putStatus = 0
    await click('Save limits')
    await until(() => expect(host.textContent).toContain('Limits saved and applied.'))
    expect(puts()[1].headers.get('If-Match')).toBe(`"${'d'.repeat(64)}"`)
  })
  it('unmounting a pending member editor discards late callbacks without a follow-up read', async () => {
    record = teamFixture(true)
    await render({ teamId: 'tea_test', userId: 'usr_member' })
    await until(() => expect(host.textContent).toContain('Inherit parent'))
    await click('Adjust member resources')
    await draft()
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await click('Save limits')
    await until(() => expect(puts()).toHaveLength(1))
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <span>Closed</span>
        </QueryClientProvider>,
      ),
    )
    const key = [
      'resource-limits',
      'team',
      'usr_member',
      'tea_test',
      'usr_member',
      cache.getQueryState(['auth', 'session'])?.dataUpdatedAt,
    ]
    // GC uses a timer even at zero; explicitly clear the private cache before the late response.
    cache.removeQueries({ queryKey: key, exact: true })
    const before = requests.length
    await act(async () => {
      release()
    })
    await until(() => expect(record.etag).toBe('c'.repeat(64)))
    expect(requests).toHaveLength(before)
    expect(
      cache.getQueryData([
        'resource-limits',
        'team',
        'usr_member',
        'tea_test',
        'usr_member',
        cache.getQueryState(['auth', 'session'])?.dataUpdatedAt,
      ]),
    ).toBeUndefined()
  })
  it('preserves transient drafts and translates existing notices on live language changes', async () => {
    putStatus = 503
    await render()
    await edit()
    await draft()
    await click('Save limits')
    await until(() => expect(host.textContent).toContain('This change may already be saved'))
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(host.textContent).toContain('此次变更可能已经保存')
    expect(host.querySelector<HTMLInputElement>('[aria-label="月度 Token 配额"]')!.value).toBe('0')
    expect(
      host.querySelector<HTMLInputElement>('[aria-label="变更原因"]')?.value ??
        host.querySelector<HTMLInputElement>('[aria-label="修改原因"]')?.value,
    ).toBe('Reviewed Team policy')
    await act(async () => {
      await i18n.changeLanguage('en')
    })
    expect(host.querySelector<HTMLInputElement>('[aria-label="Monthly token quota"]')!.value).toBe(
      '0',
    )
  })
})

describe('Team aggregate monthly threshold behavior', () => {
  it('requires explicit confirmation for a sparse independent mode-only edit', async () => {
    await render()
    await edit()
    await toggle('Monthly token threshold behavior')
    await fill('Reason for change', 'Reviewed aggregate token threshold')
    await rawClick('Save limits')
    expect(puts()).toHaveLength(0)
    expect(document.body.textContent).toContain('Confirm Team monthly behavior')
    await confirm()
    await until(() => expect(host.textContent).toContain('Limits saved and applied.'))
    expect(JSON.parse(puts()[0].data)).toEqual({
      reason: 'Reviewed aggregate token threshold',
      tokens_month_behavior: 'alert_only',
    })
    expect(record.stored.money_month_behavior).toBe('stop')
    expect(host.textContent).toContain('Saved Team behavior')
  })
  it('preserves inert mode on null and treats zero as an editable threshold without rounding money', async () => {
    record.stored.money_month_behavior = 'alert_only'
    await render()
    await edit()
    await fill('Monthly budget', '')
    expect(
      host
        .querySelector('[role="switch"][aria-label="Monthly budget threshold behavior"]')!
        .hasAttribute('data-disabled'),
    ).toBe(true)
    await fill('Monthly token quota', '0')
    expect(
      host
        .querySelector('[role="switch"][aria-label="Monthly token threshold behavior"]')!
        .hasAttribute('data-disabled'),
    ).toBe(false)
    await toggle('Monthly token threshold behavior')
    await fill('Reason for change', 'Null budget and zero token threshold')
    await rawClick('Save limits')
    expect(document.body.querySelector('[role="dialog"]')!.textContent).not.toContain(
      '999999999999999999',
    )
    await confirm()
    await until(() => expect(puts()).toHaveLength(1))
    expect(JSON.parse(puts()[0].data)).toEqual({
      reason: 'Null budget and zero token threshold',
      money_month: null,
      tokens_month: 0,
      tokens_month_behavior: 'alert_only',
    })
    expect(record.stored.money_month_behavior).toBe('alert_only')
  })
  it('honors separate server-projected monthly mode editability', async () => {
    record.editable_fields = ['tokens_month_behavior']
    await render()
    await edit()
    expect(
      host
        .querySelector('[role="switch"][aria-label="Monthly budget threshold behavior"]')!
        .hasAttribute('data-disabled'),
    ).toBe(true)
    expect(
      host.querySelector<HTMLInputElement>('[aria-label="Monthly token quota"]')!.disabled,
    ).toBe(true)
    await toggle('Monthly token threshold behavior')
    await fill('Reason for change', 'Token mode only')
    await click('Save limits')
    await until(() => expect(puts()).toHaveLength(1))
    expect(JSON.parse(puts()[0].data)).toEqual({
      reason: 'Token mode only',
      tokens_month_behavior: 'alert_only',
    })
  })
  it('retains exact sparse modes, amount, reason and original validator through an uncertain retry with current CSRF', async () => {
    putStatus = 503
    await render()
    await edit()
    await toggle('Monthly budget threshold behavior')
    await fill('Monthly budget', '123456789012345678.123456789012345678')
    await fill('Reason for change', 'Exact soft budget review')
    await click('Save limits')
    await until(() => expect(host.textContent).toContain('This change may already be saved'))
    const original = puts()[0].data
    record.etag = 'd'.repeat(64)
    record.stored.money_month_behavior = 'stop'
    csrf = 'csrf-current'
    await act(async () => cache.setQueryData(['auth', 'session'], session()))
    await click('Reload current policy')
    expect(host.textContent).toContain('This change may already be saved')
    putStatus = 0
    await click('Retry application')
    await until(() => expect(puts()).toHaveLength(2))
    expect(puts()[1].data).toBe(original)
    expect(puts()[1].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
    expect(puts()[1].headers.get('X-CSRF-Token')).toBe('csrf-current')
    expect(JSON.parse(original)).toEqual({
      reason: 'Exact soft budget review',
      money_month: '123456789012345678.123456789012345678',
      currency: 'USD',
      money_month_behavior: 'alert_only',
    })
  })
  it('keeps the member hard default while a soft Team parent permits a larger member cap', async () => {
    record = teamFixture(true)
    record.ip_policies[0].tokens_month_behavior = 'alert_only'
    record.ip_policies[0].money_month_behavior = 'alert_only'
    await render({ teamId: 'tea_test', userId: 'usr_member' })
    await until(() => expect(host.textContent).toContain('Team parent behavior'))
    await click('Adjust member resources')
    expect(host.querySelectorAll('[role="switch"]')).toHaveLength(2)
    expect(host.textContent).toContain('Current parent alert-only threshold')
    await fill('Monthly token quota', '10001')
    await fill('Reason for change', 'Independent hard member policy')
    await click('Save limits')
    await until(() => expect(puts()).toHaveLength(1))
    expect(JSON.parse(puts()[0].data)).toEqual({
      tokens_month: 10001,
      reason: 'Independent hard member policy',
    })
  })
  it('switches independent behavior labels live to Chinese while preserving original draft', async () => {
    await render()
    await edit()
    await toggle('Monthly token threshold behavior')
    await fill('Reason for change', 'Bilingual unchanged intent')
    await act(async () => i18n.changeLanguage('zh'))
    expect(
      host
        .querySelector('[role="switch"][aria-label="月度 Token 阈值行为"]')
        ?.getAttribute('aria-checked'),
    ).toBe('true')
    expect(
      host.querySelector<HTMLInputElement>('[aria-label="变更原因"]')?.value ??
        host.querySelector<HTMLInputElement>('[aria-label="修改原因"]')?.value,
    ).toBe('Bilingual unchanged intent')
    await act(async () => i18n.changeLanguage('en'))
    expect(
      host
        .querySelector('[role="switch"][aria-label="Monthly token threshold behavior"]')
        ?.getAttribute('aria-checked'),
    ).toBe('true')
  })
})

it('cannot dispatch captured mode after fresh independent editability is withdrawn', async () => {
  await render()
  await edit()
  await toggle('Monthly token threshold behavior')
  await fill('Reason for change', 'Pending mode authority')
  await rawClick('Save limits')
  expect(puts()).toHaveLength(0)
  record.editable_fields = ['money_month']
  await act(async () => {
    await cache.invalidateQueries({ queryKey: ['resource-limits', 'team'] })
  })
  await confirm()
  expect(puts()).toHaveLength(0)
})
it('keeps a hard member money ceiling and currency guard when only the Team token parent is soft', async () => {
  record = teamFixture(true)
  record.ip_policies[0].tokens_month_behavior = 'alert_only'
  record.ip_policies[0].money_month = '1'
  await render({ teamId: 'tea_test', userId: 'usr_member' })
  await until(() => expect(host.textContent).toContain('Team parent behavior'))
  await click('Adjust member resources')
  await fill('Monthly token quota', '10001')
  await fill('Monthly budget', '2')
  await fill('Reason for change', 'Independent money stop')
  await click('Save limits')
  expect(puts()).toHaveLength(0)
  expect(host.textContent).toContain('cannot exceed the current Team maximum')
})

it('confirms member modes independently and preserves original mode bytes on uncertainty and renewed field authority', async () => {
  record = teamFixture(true)
  record.stored.tokens_month = 0
  record.stored.money_month = '0'
  record.stored.currency = 'USD'
  record.ip_policies[1] = { ...record.stored }
  await render({ teamId: 'tea_test', userId: 'usr_member' })
  await until(() => expect(host.textContent).toContain('Saved member behavior'))
  await click('Adjust member resources')
  await toggle('Monthly token threshold behavior')
  await fill('Reason for change', 'Independent member alert mode')
  await rawClick('Save limits')
  expect(puts()).toHaveLength(0)
  expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain(
    'Confirm Team-member monthly behavior',
  )
  putStatus = 503
  await confirm()
  await until(() => expect(puts()).toHaveLength(1))
  expect(JSON.parse(puts()[0].data)).toEqual({
    tokens_month_behavior: 'alert_only',
    reason: 'Independent member alert mode',
  })
  csrf = 'renewed-csrf'
  cache.setQueryData(['auth', 'session'], session())
  putStatus = 0
  await until(() => expect(host.textContent).toContain('Retry application'))
  await click('Retry application')
  await until(() => expect(puts()).toHaveLength(2))
  expect(puts()[1].data).toBe(puts()[0].data)
  expect(puts()[1].headers.get('If-Match')).toBe(puts()[0].headers.get('If-Match'))
  expect(puts()[1].headers.get('X-CSRF-Token')).toBe('renewed-csrf')
})
it('uses independent published member editability and hides private draft during read errors', async () => {
  record = teamFixture(true)
  record.stored.tokens_month = 0
  record.ip_policies[1] = { ...record.stored }
  record.editable_fields = ['tokens_month', 'tokens_month_behavior']
  await render({ teamId: 'tea_test', userId: 'usr_member' })
  await until(() => expect(host.textContent).toContain('Saved member behavior'))
  await click('Adjust member resources')
  expect(
    host
      .querySelector('[role="switch"][aria-label="Monthly budget threshold behavior"]')
      ?.getAttribute('aria-disabled'),
  ).toBe('true')
  await toggle('Monthly token threshold behavior')
  await fill('Reason for change', 'Retain private mode draft')
  await rawClick('Save limits')
  getStatus = 403
  await act(async () => {
    await cache.invalidateQueries({
      queryKey: [
        'resource-limits',
        'team',
        'usr_member',
        'tea_test',
        'usr_member',
        cache.getQueryState(['auth', 'session'])?.dataUpdatedAt,
      ],
    })
  })
  expect(host.textContent).not.toContain('Retain private mode draft')
  expect(puts()).toHaveLength(0)
})

it('blocks a queued member confirmation after Session renewal revokes its exact dimension and retains draft through the renewed read', async () => {
  record = teamFixture(true)
  record.stored.tokens_month = 0
  record.ip_policies[1] = { ...record.stored }
  await render({ teamId: 'tea_test', userId: 'usr_member' })
  await until(() => expect(host.textContent).toContain('Saved member behavior'))
  await click('Adjust member resources')
  await toggle('Monthly token threshold behavior')
  await fill('Reason for change', 'Exact renewed-authority draft')
  await rawClick('Save limits')
  const queued = [...document.body.querySelectorAll('button')].find(
    (x) => x.textContent === 'Confirm limits',
  )!
  expect(queued).toBeTruthy()
  let release!: () => void
  getHold = new Promise<void>((r) => {
    release = r
  })
  record.editable_fields = ['money_month', 'money_month_behavior']
  await act(async () => {
    cache.setQueryData(['auth', 'session'], session(), {
      updatedAt: (cache.getQueryState(['auth', 'session'])?.dataUpdatedAt ?? 0) + 1,
    })
  })
  await until(() =>
    expect(document.body.textContent).not.toContain('Exact renewed-authority draft'),
  )
  await act(async () => queued.click())
  expect(puts()).toHaveLength(0)
  await act(async () => release())
  await until(() => expect(document.body.textContent).toContain('Exact renewed-authority draft'))
  const current = [...document.body.querySelectorAll('button')].find(
    (x) => x.textContent === 'Confirm limits',
  )!
  await act(async () => current.click())
  expect(puts()).toHaveLength(0)
})
it('preserves inert member mode and exact reason while switching EN/ZH, zero re-enables its independent control', async () => {
  record = teamFixture(true)
  record.stored.tokens_month_behavior = 'alert_only'
  record.ip_policies[1] = { ...record.stored }
  await render({ teamId: 'tea_test', userId: 'usr_member' })
  await until(() => expect(host.textContent).toContain('Saved member behavior'))
  await click('Adjust member resources')
  const mode = host.querySelector('[role="switch"][aria-label="Monthly token threshold behavior"]')!
  expect(mode.getAttribute('aria-checked')).toBe('true')
  expect(mode.getAttribute('aria-disabled')).toBe('true')
  await fill('Monthly token quota', '0')
  expect(mode.getAttribute('aria-disabled')).not.toBe('true')
  await fill('Reason for change', 'Bilingual member review')
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('仅提醒')
  expect(host.querySelector('input[aria-label="变更原因"]')?.getAttribute('value')).toBe(
    'Bilingual member review',
  )
  await act(async () => i18n.changeLanguage('en'))
  await click('Save limits')
  await until(() => expect(puts()).toHaveLength(1))
  expect(JSON.parse(puts()[0].data)).toEqual({ tokens_month: 0, reason: 'Bilingual member review' })
})

describe('Team-member in-flight publication during authority renewal', () => {
  async function pendingMemberWrite() {
    record = teamFixture(true)
    record.stored.tokens_month = 0
    record.ip_policies[1] = { ...record.stored }
    await render({ teamId: 'tea_test', userId: 'usr_member' })
    await until(() => expect(host.textContent).toContain('Saved member behavior'))
    await click('Adjust member resources')
    await toggle('Monthly token threshold behavior')
    await fill('Reason for change', 'Original in-flight member mode')
    let release!: () => void
    hold = new Promise<void>((resolve) => {
      release = resolve
    })
    await click('Save limits')
    await until(() => expect(puts()).toHaveLength(1))
    return release
  }

  for (const status of [503, 0]) {
    it(`retains an obsolete-generation ${status || 200} response as uncertain and permits only an explicit identical retry`, async () => {
      const release = await pendingMemberWrite()
      const originalBody = puts()[0].data
      const originalETag = puts()[0].headers.get('If-Match')
      let releaseRead!: () => void
      getHold = new Promise<void>((resolve) => {
        releaseRead = resolve
      })
      csrf = 'fresh-in-flight-csrf'
      const generation = (cache.getQueryState(['auth', 'session'])?.dataUpdatedAt ?? 0) + 1
      await act(async () =>
        cache.setQueryData(['auth', 'session'], session(), { updatedAt: generation }),
      )
      await until(() => expect(host.querySelector('[aria-label="Reason for change"]')).toBeNull())
      await act(async () => releaseRead())
      await until(() =>
        expect(
          host.querySelector<HTMLInputElement>('[aria-label="Reason for change"]')?.value,
        ).toBe('Original in-flight member mode'),
      )
      putStatus = status
      await act(async () => release())
      await until(() => expect(button('Retry application').disabled).toBe(false))
      expect(host.textContent).not.toContain('Limits saved and applied.')
      expect(puts()).toHaveLength(1)
      const currentKey = [
        'resource-limits',
        'team',
        'usr_member',
        'tea_test',
        'usr_member',
        generation,
      ]
      expect(cache.getQueryData<LimitRecord>(currentKey)?.etag).toBe('a'.repeat(64))
      putStatus = 0
      hold = undefined
      await click('Retry application')
      await until(() => expect(host.textContent).toContain('Limits saved and applied.'))
      expect(puts()).toHaveLength(2)
      expect(puts()[1].data).toBe(originalBody)
      expect(puts()[1].headers.get('If-Match')).toBe(originalETag)
      expect(puts()[1].headers.get('X-CSRF-Token')).toBe('fresh-in-flight-csrf')
    })
  }

  it('keeps the dispatched mode uncertain but blocks retry after fresh field authority is withdrawn', async () => {
    const release = await pendingMemberWrite()
    record.editable_fields = ['money_month', 'money_month_behavior']
    await act(async () =>
      cache.setQueryData(['auth', 'session'], session(), {
        updatedAt: (cache.getQueryState(['auth', 'session'])?.dataUpdatedAt ?? 0) + 1,
      }),
    )
    await until(() => expect(host.querySelector('[aria-label="Reason for change"]')).not.toBeNull())
    putStatus = 503
    await act(async () => release())
    await until(() => expect(button('Retry application').disabled).toBe(false))
    await click('Retry application')
    expect(puts()).toHaveLength(1)
    expect(JSON.parse(puts()[0].data)).toEqual({
      tokens_month_behavior: 'alert_only',
      reason: 'Original in-flight member mode',
    })
    expect(host.textContent).not.toContain('Limits saved and applied.')
  })

  it('destroys the old in-flight owner on actor replacement without publishing its late success', async () => {
    const release = await pendingMemberWrite()
    actor = 'usr_replacement'
    await act(async () => cache.setQueryData(['auth', 'session'], session()))
    await until(() => expect(host.textContent).toContain('Saved member behavior'))
    expect(host.querySelector('[aria-label="Reason for change"]')).toBeNull()
    const before = requests.length
    await act(async () => release())
    await until(() => expect(record.etag).toBe('c'.repeat(64)))
    expect(requests).toHaveLength(before)
    expect(puts()).toHaveLength(1)
    expect(host.textContent).not.toContain('Retry application')
    expect(host.textContent).not.toContain('Limits saved and applied.')
  })

  it('destroys the old in-flight owner on target change and cannot restore the old member draft', async () => {
    const release = await pendingMemberWrite()
    const original = structuredClone(record)
    record = { ...teamFixture(true), id: 'usr_other' }
    await render({ teamId: 'tea_test', userId: 'usr_other' })
    await until(() => expect(host.textContent).toContain('Saved member behavior'))
    const before = requests.length
    record = original
    await act(async () => release())
    await until(() => expect(record.etag).toBe('c'.repeat(64)))
    expect(requests).toHaveLength(before)
    expect(puts()).toHaveLength(1)
    expect(host.querySelector('[aria-label="Reason for change"]')).toBeNull()
    expect(host.textContent).not.toContain('Retry application')
    expect(host.textContent).not.toContain('Limits saved and applied.')
  })

  it('discards an unmounted in-flight failure without retrying or restoring a private owner', async () => {
    const release = await pendingMemberWrite()
    putStatus = 503
    await act(async () => root.render(<span>Closed member editor</span>))
    const before = requests.length
    await act(async () => release())
    expect(requests).toHaveLength(before)
    expect(puts()).toHaveLength(1)
    expect(host.textContent).toBe('Closed member editor')
    expect(document.body.textContent).not.toContain('Original in-flight member mode')
  })
})
