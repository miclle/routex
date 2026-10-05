import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import client from '@/api/client'
import DefaultLimitsPage from './index'
import RestoreDefaults from './restore'
import { teamFixture } from '@/views/resource-limits/fixture'
import type { DefaultLimitRecord, DefaultLimitResetContext } from '@/types/default-limits'
import type { Session } from '@/types/auth'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let root: Root, host: HTMLDivElement, cache: QueryClient
let permissions: string[], record: DefaultLimitRecord, context: DefaultLimitResetContext
let requests: InternalAxiosRequestConfig[],
  fail: number,
  getFail: number,
  hold: Promise<void> | undefined,
  writeHold: Promise<void> | undefined
let actor: string
function session(): Session {
  return {
    user: { id: actor, name: 'Reviewer', email: 'reviewer@example.invalid', role: 'member' },
    csrf_token: 'current-csrf',
  } as Session
}
function fixture(kind: 'user' | 'team' = 'user'): DefaultLimitRecord {
  return {
    kind,
    rule_etag: 'a'.repeat(64),
    etag: 'b'.repeat(64),
    policy: {
      tokens_5h: 100,
      tokens_7d: null,
      tokens_month: 500,
      money_month: '12.500000000000000001',
      currency: 'USD',
      rpm: 60,
      tpm: null,
      concurrency: 4,
    },
    platform_currency: 'USD',
    editable: true,
    updated_at: '2026-10-03T00:00:00Z',
  }
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 60000 } } })
  actor = 'usr_reviewer'
  permissions = ['limits.settings.write', 'limits.users.write']
  record = fixture()
  requests = []
  fail = 0
  getFail = 0
  hold = undefined
  writeHold = undefined
  cache.setQueryData(['auth', 'session'], session())
  const limit = teamFixture()
  context = {
    kind: 'team',
    id: 'tea_test',
    etag: 'c'.repeat(64),
    default_rule: fixture('team'),
    limit,
    applied_default_etag: null,
    editable: true,
  }
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
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url?.includes('/default-limits/')) {
      if (config.method === 'get' && hold) await hold
      if (config.method === 'put' && writeHold) await writeHold
      const status = config.method === 'put' ? fail : getFail
      if (status) {
        response.status = status
        throw new AxiosError('Fixture', '', config, undefined, response)
      }
      if (config.method === 'put')
        record = {
          ...record,
          etag: 'd'.repeat(64),
          rule_etag: 'e'.repeat(64),
          policy: JSON.parse(config.data).policy,
        }
      response.data = {
        ...structuredClone(record),
        kind: config.url.endsWith('/team') ? 'team' : 'user',
      }
    } else if (config.url?.endsWith('/default-reset')) {
      if (config.method === 'post' && writeHold) await writeHold
      if (config.method === 'get' && hold) await hold
      const status = config.method === 'post' ? fail : getFail
      if (status) {
        response.status = status
        throw new AxiosError('Fixture', '', config, undefined, response)
      }
      if (config.method === 'get') response.data = structuredClone(context)
      else
        response.data = {
          kind: context.kind,
          id: context.id,
          saved: true,
          default_reset_etag: context.etag,
          applied_default_etag: context.default_rule.rule_etag,
          runtime_applied: context.limit.enforced,
          limit: {
            ...context.limit,
            stored: { ...context.limit.stored, ...context.default_rule.policy },
          },
        }
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
async function until(assertion: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      assertion()
      return
    } catch {
      /* Wait for the observed UI. */
    }
  }
  assertion()
}
async function mount(restore = false) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        {restore ? (
          <RestoreDefaults target={{ kind: 'team', id: 'tea_test' }} />
        ) : (
          <DefaultLimitsPage />
        )}
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(host.textContent).toContain(restore ? 'Restore defaults' : 'Monthly budget'),
  )
}
function button(label: string) {
  const value = Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find(
    (item) => item.textContent === label,
  )
  expect(value, `Button ${label}`).toBeDefined()
  return value!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function input(label: string, value: string) {
  const element = document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
  expect(element).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, value)
    element.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
const writes = () => requests.filter((req) => req.method === 'put' || req.method === 'post')

describe('Default limits', () => {
  it('has two scoped default tabs, preserves zero/null and exact money through save', async () => {
    await mount()
    expect(host.textContent).toContain('User defaults')
    expect(host.textContent).toContain('Team defaults')
    expect(host.textContent).not.toContain('IP restrictions')
    await click('Edit')
    await input('Monthly budget', '999999999999999999.123456789012345678')
    await input('Five-hour tokens', '0')
    await input('Monthly tokens', '')
    await input('Reason', 'Controlled future policy')
    await click('Save changes')
    await until(() => expect(host.textContent).toContain('Default rule saved for future creation.'))
    const body = JSON.parse(writes()[0].data)
    expect(body.policy.tokens_5h).toBe(0)
    expect(body.policy.tokens_month).toBeNull()
    expect(body.policy.money_month).toBe('999999999999999999.123456789012345678')
    expect(body.policy.currency).toBe('USD')
    expect(writes()[0].headers['If-Match']).toBe('"' + 'b'.repeat(64) + '"')
    expect(body).not.toHaveProperty('ip_mode')
    await click('Team defaults')
    await until(() =>
      expect(requests.some((req) => req.url === '/admin/default-limits/team')).toBe(true),
    )
  })
  it('requires reason and safe integer/exact decimal input', async () => {
    await mount()
    await click('Edit')
    await click('Save changes')
    expect(host.textContent).toContain('Enter a reason')
    for (const invalidReason of ['x'.repeat(1025), '文'.repeat(342), 'Invalid\u007freason']) {
      await input('Reason', invalidReason)
      await click('Save changes')
      expect(host.textContent).toContain('1,024 bytes without control characters')
      expect(writes()).toHaveLength(0)
    }
    await input('Reason', 'Reviewed')
    await input('Tokens per minute (TPM)', '9007199254740992')
    await click('Save changes')
    expect(host.textContent).toContain('safe integers')
    expect(writes()).toHaveLength(0)
    await input('Tokens per minute (TPM)', '1')
    await input('Monthly budget', '1e3')
    await click('Save changes')
    expect(writes()).toHaveLength(0)
  })
  it('allows system readers without edit and refuses unrelated actors', async () => {
    permissions = ['system.read']
    record.editable = false
    await mount()
    expect(
      Array.from(host.querySelectorAll('button')).some((item) => item.textContent === 'Edit'),
    ).toBe(false)
    expect(writes()).toHaveLength(0)
    permissions = []
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['permissions'] })
    })
    await until(() => expect(host.textContent).not.toContain('Monthly budget'))
  })
  it('retains draft on conflict and requires explicit review of currency generation', async () => {
    await mount()
    await click('Edit')
    await input('Monthly budget', '22.000000000000000001')
    await input('Reason', 'Retained reason')
    fail = 409
    await click('Save changes')
    await until(() => expect(host.textContent).toContain('reviewed policy or currency changed'))
    record = { ...record, etag: 'f'.repeat(64), platform_currency: 'EUR' }
    fail = 0
    await click('Review latest policy')
    await until(() => expect(host.textContent).toContain('Platform currency: EUR'))
    expect(
      document.querySelector<HTMLInputElement>('input[aria-label="Monthly budget"]')!.value,
    ).toBe('22.000000000000000001')
    await click('Save changes')
    await until(() => expect(writes()).toHaveLength(2))
    expect(JSON.parse(writes()[1].data).policy.currency).toBe('EUR')
    expect(writes()[1].headers['If-Match']).toBe('"' + 'f'.repeat(64) + '"')
  })
  it('keeps original uncertain intent across refresh and rejected retry', async () => {
    await mount()
    await click('Edit')
    await input('Reason', 'Original reason')
    fail = 503
    await click('Save changes')
    await until(() => expect(host.textContent).toContain('result is uncertain'))
    const original = writes()[0]
    record = { ...record, etag: 'f'.repeat(64), platform_currency: 'EUR' }
    await click('Review latest policy')
    fail = 409
    await click('Retry original request')
    await until(() => expect(writes()).toHaveLength(2))
    expect(host.textContent).toContain('result is uncertain')
    expect(writes()[1].data).toBe(original.data)
    expect(writes()[1].headers['If-Match']).toBe(original.headers['If-Match'])
    expect(button('Team defaults').getAttribute('aria-disabled')).toBe('true')
    await click('Team defaults')
    expect(requests.some((request) => request.url === '/admin/default-limits/team')).toBe(false)
    fail = 0
    await click('Retry original request')
    await until(() => expect(host.textContent).toContain('Default rule saved'))
    expect(writes()[2].data).toBe(original.data)
  })
  it('hides cached private defaults during refresh and denial', async () => {
    await mount()
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    getFail = 403
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['default-limits'] })
    })
    await until(() => expect(host.textContent).not.toContain('12.500000000000000001'))
    await act(async () => release())
    await until(() => expect(host.textContent).not.toContain('Monthly budget'))
    expect(
      Array.from(host.querySelectorAll('button')).some((item) => item.textContent === 'Edit'),
    ).toBe(false)
  })
  it('does not acknowledge a late save after the actor changes', async () => {
    await mount()
    await click('Edit')
    await input('Reason', 'Old actor')
    let release!: () => void
    writeHold = new Promise((resolve) => {
      release = resolve
    })
    await click('Save changes')
    actor = 'usr_other'
    permissions = ['system.read']
    record.editable = false
    await act(async () => cache.setQueryData(['auth', 'session'], session()))
    await act(async () => release())
    await until(() => expect(host.textContent).toContain('Monthly budget'))
    expect(host.textContent).not.toContain('Default rule saved')
    expect(cache.getQueryData(['default-limits', 'usr_other', 'user'])).toBeDefined()
  })
  it('switches language with preserved draft', async () => {
    await mount()
    await click('Edit')
    await input('Reason', 'Retained English content')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(host.textContent).toContain('月度预算')
    expect(document.querySelector<HTMLInputElement>('input[aria-label="理由"]')!.value).toBe(
      'Retained English content',
    )
  })
})

describe('Explicit default restoration', () => {
  it('restores a legacy User policy only with user-limit authority and preserves IP', async () => {
    context = {
      ...context,
      kind: 'user',
      id: 'usr_subject',
      default_rule: fixture('user'),
      limit: {
        ...context.limit,
        kind: 'user',
        id: 'usr_subject',
        etag: '0',
        stored: { ...context.limit.stored, ip_mode: 'allowlist', ip_ranges: ['192.0.2.0/24'] },
      },
    }
    permissions = ['limits.users.write']
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RestoreDefaults target={{ kind: 'user', id: 'usr_subject' }} />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(host.textContent).toContain('Restore defaults'))
    await click('Restore defaults')
    await until(() => expect(document.querySelector('input[aria-label="Reason"]')).toBeTruthy())
    await input('Reason', 'Keep IP and existing usage')
    await click('Confirm restoration')
    await until(() => expect(host.textContent).toContain('Defaults restored and applied'))
    expect(writes()[0].url).toBe('/admin/members/usr_subject/limits/default-reset')
    expect(JSON.parse(writes()[0].data)).toEqual({ reason: 'Keep IP and existing usage' })
  })
  it('ignores a late restoration response for the previous actor', async () => {
    permissions = ['teams.tokens.write', 'teams.money.write', 'teams.rates.write']
    await mount(true)
    await click('Restore defaults')
    await until(() => expect(document.querySelector('input[aria-label="Reason"]')).toBeTruthy())
    await input('Reason', 'Old actor restoration')
    let release!: () => void
    writeHold = new Promise((resolve) => {
      release = resolve
    })
    await click('Confirm restoration')
    actor = 'usr_other'
    permissions = []
    await act(async () => {
      cache.setQueryData(['auth', 'session'], session())
      await cache.invalidateQueries({ queryKey: ['permissions'] })
    })
    await act(async () => release())
    await until(() => expect(host.textContent).not.toContain('Restore defaults'))
    expect(host.textContent).not.toContain('Defaults restored and applied')
  })
  it('requires all independent Team field permissions', async () => {
    permissions = ['teams.tokens.write', 'teams.money.write']
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RestoreDefaults target={{ kind: 'team', id: 'tea_test' }} />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(requests.some((req) => req.url === '/auth/permissions')).toBe(true))
    expect(host.textContent).not.toContain('Restore defaults')
    expect(requests.some((req) => req.url?.endsWith('/default-reset'))).toBe(false)
  })
  it('previews current/default policies and submits explicit reason/confirmation', async () => {
    permissions = ['teams.tokens.write', 'teams.money.write', 'teams.rates.write']
    await mount(true)
    expect(requests.some((req) => req.url?.endsWith('/default-reset'))).toBe(false)
    await click('Restore defaults')
    await until(() => expect(document.body.textContent).toContain('Reviewed default policy'))
    expect(document.body.textContent).toContain('Current stored policy')
    await click('Confirm restoration')
    expect(writes()).toHaveLength(0)
    for (const invalidReason of ['x'.repeat(1025), '文'.repeat(342), 'Invalid\u007freason']) {
      await input('Reason', invalidReason)
      await click('Confirm restoration')
      expect(document.body.textContent).toContain('1,024 bytes without control characters')
      expect(writes()).toHaveLength(0)
    }
    await input('Reason', 'Reviewed default restoration')
    await click('Confirm restoration')
    await until(() => expect(host.textContent).toContain('Defaults restored and applied'))
    expect(JSON.parse(writes()[0].data)).toEqual({ reason: 'Reviewed default restoration' })
    expect(writes()[0].headers['If-Match']).toBe('"' + 'c'.repeat(64) + '"')
  })
  it('retains original reset intent after 503 and newer default/target conflict', async () => {
    permissions = ['teams.tokens.write', 'teams.money.write', 'teams.rates.write']
    await mount(true)
    await click('Restore defaults')
    await until(() => expect(document.querySelector('input[aria-label="Reason"]')).toBeTruthy())
    await input('Reason', 'Original reset')
    fail = 503
    await click('Confirm restoration')
    await until(() => expect(document.body.textContent).toContain('result is uncertain'))
    const original = writes()[0]
    context = {
      ...context,
      etag: 'f'.repeat(64),
      default_rule: { ...context.default_rule, rule_etag: 'e'.repeat(64) },
    }
    await click('Review latest policy')
    fail = 409
    await click('Retry original request')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].data).toBe(original.data)
    expect(writes()[1].headers['If-Match']).toBe(original.headers['If-Match'])
    expect(document.body.textContent).toContain('result is uncertain')
    expect(document.body.textContent).not.toContain('Defaults restored and applied')
  })
  it('requires explicit review after the target, default or currency changes', async () => {
    permissions = ['teams.tokens.write', 'teams.money.write', 'teams.rates.write']
    await mount(true)
    await click('Restore defaults')
    await until(() => expect(document.querySelector('input[aria-label="Reason"]')).toBeTruthy())
    await input('Reason', 'Retained restoration reason')
    context = {
      ...context,
      etag: 'f'.repeat(64),
      default_rule: {
        ...context.default_rule,
        platform_currency: 'EUR',
        policy: {
          ...context.default_rule.policy,
          money_month: '3.000000000000000001',
          currency: 'EUR',
        },
      },
    }
    await act(async () => {
      await cache.invalidateQueries({ queryKey: ['default-reset'] })
    })
    await until(() =>
      expect(document.body.textContent).toContain('reviewed policy or currency changed'),
    )
    expect(button('Confirm restoration').disabled).toBe(true)
    await click('Review latest policy')
    await until(() => expect(document.body.textContent).toContain('3.000000000000000001 EUR'))
    expect(document.querySelector<HTMLInputElement>('input[aria-label="Reason"]')!.value).toBe(
      'Retained restoration reason',
    )
    await click('Confirm restoration')
    await until(() => expect(writes()).toHaveLength(1))
    expect(writes()[0].headers['If-Match']).toBe('"' + 'f'.repeat(64) + '"')
  })
  it('separates a durable restore acknowledgement from runtime application', async () => {
    permissions = ['teams.tokens.write', 'teams.money.write', 'teams.rates.write']
    context.limit.enforced = false
    await mount(true)
    await click('Restore defaults')
    await until(() => expect(document.querySelector('input[aria-label="Reason"]')).toBeTruthy())
    await input('Reason', 'Saved but pending publication')
    await click('Confirm restoration')
    await until(() => expect(host.textContent).toContain('runtime application is pending'))
    expect(host.textContent).not.toContain('Defaults restored and applied')
  })
  it('hides denied cached preview and suppresses restoration', async () => {
    permissions = ['teams.tokens.write', 'teams.money.write', 'teams.rates.write']
    await mount(true)
    await click('Restore defaults')
    await until(() => expect(document.body.textContent).toContain('Reviewed default policy'))
    getFail = 404
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['default-reset'] })
    })
    await until(() => expect(document.body.textContent).not.toContain('Reviewed default policy'))
    await act(async () => release())
    await until(() => expect(document.querySelector('input[aria-label="Reason"]')).toBeNull())
    expect(writes()).toHaveLength(0)
  })
})

it('retains first 409 restoration through repeated explicit retry without cache renewal', async () => {
  permissions = ['teams.tokens.write', 'teams.money.write', 'teams.rates.write']
  fail = 409
  await mount(true)
  await click('Restore defaults')
  await until(() => expect(document.querySelector('input[aria-label="Reason"]')).toBeTruthy())
  await input('Reason', 'First conflict may follow commit')
  await click('Confirm restoration')
  await until(() => expect(document.body.textContent).toContain('result is uncertain'))
  const original = writes()[0]
  await click('Retry original request')
  await until(() => expect(writes()).toHaveLength(2))
  fail = 0
  await click('Retry original request')
  await until(() => expect(host.textContent).toContain('Defaults restored and applied'))
  expect(writes()).toHaveLength(3)
  expect(
    writes().every(
      (row) =>
        row.data === original.data && row.headers['If-Match'] === original.headers['If-Match'],
    ),
  ).toBe(true)
  expect(cache.getMutationCache().getAll()).toHaveLength(0)
})

it('preserves uncertain restoration across real Escape dismissal and same-target reopen', async () => {
  permissions = ['teams.tokens.write', 'teams.money.write', 'teams.rates.write']
  fail = 503
  await mount(true)
  await click('Restore defaults')
  await until(() => expect(document.querySelector('input[aria-label="Reason"]')).toBeTruthy())
  await input('Reason', 'Keep original after Escape')
  await click('Confirm restoration')
  await until(() => expect(document.body.textContent).toContain('result is uncertain'))
  const original = writes()[0]
  await act(async () =>
    document.activeElement?.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    ),
  )
  await until(() => expect(document.querySelector('[role="dialog"]')).toBeNull())
  await click('Restore defaults')
  await until(() =>
    expect(document.querySelector<HTMLInputElement>('input[aria-label="Reason"]')?.value).toBe(
      'Keep original after Escape',
    ),
  )
  expect(button('Confirm restoration').disabled).toBe(true)
  fail = 0
  await click('Retry original request')
  await until(() => expect(host.textContent).toContain('Defaults restored and applied'))
  expect(writes()).toHaveLength(2)
  expect(writes()[1].data).toBe(original.data)
  expect(writes()[1].headers['If-Match']).toBe(original.headers['If-Match'])
})

it('explicit abandonment preserves the reason and unknown outcome until a new reviewed confirmation', async () => {
  permissions = ['teams.tokens.write', 'teams.money.write', 'teams.rates.write']
  await mount(true)
  await click('Restore defaults')
  await until(() => expect(document.querySelector('input[aria-label="Reason"]')).toBeTruthy())
  await input('Reason', 'Retained deliberate review')
  fail = 409
  await click('Confirm restoration')
  await until(() => expect(button('Retry original request').disabled).toBe(false))
  const original = writes()[0]
  await click('Abandon original restoration')
  await until(() => expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(2))
  await act(async () => {
    const dialogs = document.querySelectorAll('[role="dialog"]')
    const cancel = [
      ...dialogs[dialogs.length - 1].querySelectorAll<HTMLButtonElement>('button'),
    ].find((button) => button.textContent === 'Cancel')!
    cancel.click()
  })
  expect(writes()).toHaveLength(1)
  await click('Abandon original restoration')
  await click('Abandon restoration request')
  await until(() => expect(document.body.textContent).toContain('previous outcome remains unknown'))
  expect(button('Confirm restoration').disabled).toBe(true)
  expect(document.querySelector<HTMLInputElement>('input[aria-label="Reason"]')!.value).toBe(
    'Retained deliberate review',
  )
  context.etag = 'f'.repeat(64)
  await act(async () => cache.refetchQueries({ queryKey: ['default-reset'] }))
  expect(button('Confirm restoration').disabled).toBe(true)
  expect(writes()).toHaveLength(1)
  await click('Review latest policy')
  await until(() => expect(button('Confirm restoration').disabled).toBe(false))
  fail = 0
  await click('Confirm restoration')
  await until(() => expect(writes()).toHaveLength(2))
  expect(writes()[1].data).toBe(original.data)
  expect(writes()[1].headers.get('If-Match')).toBe('"' + 'f'.repeat(64) + '"')
  expect(original.headers.get('If-Match')).toBe('"' + 'c'.repeat(64) + '"')
})

it.each([400, 401, 403, 404, 409, 412, 422, 503])(
  'a failed restoration status %s cannot erase original uncertainty',
  async (status) => {
    permissions = ['teams.tokens.write', 'teams.money.write', 'teams.rates.write']
    await mount(true)
    await click('Restore defaults')
    await until(() => expect(document.querySelector('input[aria-label="Reason"]')).toBeTruthy())
    await input('Reason', 'Exact failed restoration')
    fail = status
    await click('Confirm restoration')
    await until(() => expect(button('Retry original request').disabled).toBe(false))
    expect(button('Confirm restoration').disabled).toBe(true)
    const original = writes()[0]
    await click('Review latest policy')
    await click('Retry original request')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].data).toBe(original.data)
    expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
    expect(document.body.textContent).not.toContain('Defaults restored and applied')
  },
)

it('captures policy, currency and IP arrays separately from incidental mutable query data', async () => {
  permissions = ['teams.tokens.write', 'teams.money.write', 'teams.rates.write']
  context.limit.stored.ip_mode = 'allowlist'
  context.limit.stored.ip_ranges = ['192.0.2.0/24']
  await mount(true)
  await click('Restore defaults')
  await until(() => expect(document.querySelector('input[aria-label="Reason"]')).toBeTruthy())
  await input('Reason', 'Original captured policy')
  fail = 409
  await click('Confirm restoration')
  await until(() => expect(button('Retry original request').disabled).toBe(false))
  const original = writes()[0]
  await act(async () => {
    cache.setQueryData<DefaultLimitResetContext>(
      ['default-reset', actor, 'team', 'tea_test'],
      (data) => {
        data!.default_rule.policy.money_month = '9.123456789012345678'
        data!.default_rule.policy.currency = 'EUR'
        data!.default_rule.platform_currency = 'EUR'
        data!.limit.stored.ip_ranges.push('198.51.100.0/24')
        return { ...data!, etag: 'f'.repeat(64) }
      },
    )
  })
  expect(document.body.textContent).toContain('12.500000000000000001 USD')
  expect(document.body.textContent).not.toContain('9.123456789012345678 EUR')
  await click('Retry original request')
  await until(() => expect(writes()).toHaveLength(2))
  expect(writes()[1].data).toBe(original.data)
  expect(writes()[1].headers.get('If-Match')).toBe(original.headers.get('If-Match'))
  expect(button('Confirm restoration').disabled).toBe(true)
})
