import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import client from '@/api/client'
import ResourceLimits from './index'
import type { LimitRecord } from '@/types/resource-limits'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, host: HTMLDivElement, cache: QueryClient
let record: LimitRecord, requests: InternalAxiosRequestConfig[], failure: number
let hold: Promise<void> | undefined
const originalAdapter = client.defaults.adapter
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  cache.setQueryData(['auth', 'session'], {
    user: { id: 'usr_limits', name: 'Limits', role: 'member' },
    csrf_token: 'limits-csrf',
  })
  const parent = {
    rpm: 60,
    concurrency: 4,
    ip_mode: 'allowlist' as const,
    ip_ranges: ['192.0.2.0/24'],
  }
  record = {
    kind: 'personal_key',
    id: 'key_test',
    account_id: 'key_original',
    etag: 'old',
    platform_currency: 'USD',
    quota_usage: null,
    parent_etag: 'parent1',
    stored: {
      tokens_5h: 1000,
      tokens_7d: null,
      tokens_month: 5000,
      tpm: 250,
      money_month: '12.500000000000000001',
      currency: 'USD',
      rpm: null,
      concurrency: 2,
      ip_mode: 'none',
      ip_ranges: [],
    },
    effective: { rpm: 60, concurrency: 2 },
    ip_policies: [parent, { rpm: null, concurrency: 2, ip_mode: 'none', ip_ranges: [] }],
    rpm_used: 3,
    active: 1,
    enforced: true,
  }
  requests = []
  failure = 0
  hold = undefined
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: {} as unknown,
    }
    if (config.url === '/auth/session')
      response.data = {
        user: { id: 'usr_limits', name: 'Limits', role: 'member' },
        csrf_token: 'limits-csrf',
      }
    else if (config.url?.endsWith('/limits')) {
      if (config.method === 'put') {
        if (hold) await hold
        if (failure) {
          response.status = failure
          throw new AxiosError('Fixture', '', config, undefined, response)
        }
        const payload = JSON.parse(config.data)
        const policy = {
          tokens_5h: payload.tokens_5h,
          tokens_7d: payload.tokens_7d,
          tokens_month: payload.tokens_month,
          tpm: payload.tpm,
          money_month: payload.money_month,
          currency: payload.currency,
          rpm: payload.rpm,
          concurrency: payload.concurrency,
          ip_mode: payload.ip_mode,
          ip_ranges: payload.ip_ranges,
        }
        record = {
          ...record,
          etag: 'saved',
          stored: policy,
          effective: { rpm: policy.rpm, concurrency: policy.concurrency },
          ip_policies: record.kind === 'project' ? [policy] : [record.ip_policies[0], policy],
        }
      }
      response.data = structuredClone(record)
    }
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  client.defaults.adapter = originalAdapter
  host.remove()
})
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
async function mount(canEdit = true, child = true, path = '/keys/key_test') {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <ResourceLimits path={path} canEdit={canEdit} child={child} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain('key_original'))
}
function button(label: string) {
  const result = [...document.querySelectorAll('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
  expect(result, label).toBeDefined()
  return result!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function fill(label: string, value: string) {
  const input = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(
    `[aria-label="${label}"]`,
  )!
  expect(input, label).toBeTruthy()
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      input instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype,
      'value',
    )!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
const writes = () => requests.filter((request) => request.method === 'put')
describe('Resource admission controls', () => {
  it('shows stored/inherited/effective rules, parent IP conjunction and live counters without a write', async () => {
    await mount(false)
    expect(host.textContent).toContain('Stored: Inherit parent')
    expect(host.textContent).toContain('Effective: 60')
    expect(host.textContent).toContain('Parent policy: Allow listed sources only')
    expect(host.textContent).toContain('192.0.2.0/24')
    expect(host.textContent).toContain('Admitted in the rolling minute3')
    expect(host.textContent).not.toContain('Edit restrictions')
    expect(writes()).toHaveLength(0)
  })
  it('preserves zero/null, audit reason and strong If-Match; sends one write during pending double clicks', async () => {
    await mount()
    await click('Edit restrictions')
    await fill('RPM', '0')
    await fill('Maximum concurrency', '')
    await fill('Reason for change', 'Narrow admission')
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await act(async () => {
      button('Save limits').click()
      button('Save limits').click()
    })
    await until(() => expect(writes()).toHaveLength(1))
    expect(JSON.parse(writes()[0].data)).toEqual({
      tokens_5h: 1000,
      tokens_7d: null,
      tokens_month: 5000,
      tpm: 250,
      money_month: '12.500000000000000001',
      currency: 'USD',
      rpm: 0,
      concurrency: null,
      ip_mode: 'none',
      ip_ranges: [],
      reason: 'Narrow admission',
    })
    expect(writes()[0].headers.get('If-Match')).toBe('"old"')
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('limits-csrf')
    expect(button('Close').disabled).toBe(true)
    await act(async () => release())
    await until(() => expect(host.textContent).toContain('Limits saved and applied.'))
    expect(host.textContent).toContain('Stored: 0')
  })
  it('rejects fractional, unsafe and above-parent numbers; requires reason and valid range count', async () => {
    await mount()
    await click('Edit restrictions')
    for (const value of ['1.5', '-1', '1e3', '9007199254740992']) {
      await fill('RPM', value)
      await click('Save limits')
      expect(document.body.textContent).toContain('nonnegative safe integer')
    }
    await fill('RPM', '61')
    await click('Save limits')
    expect(document.body.textContent).toContain('cannot exceed')
    await fill('RPM', '10')
    await click('Save limits')
    expect(document.body.textContent).toContain('Provide a reason')
    await fill('Reason for change', 'Reviewed')
    await act(async () =>
      document.querySelectorAll<HTMLInputElement>('input[type="radio"]')[1].click(),
    )
    await click('Save limits')
    expect(document.body.textContent).toContain('between 1 and 128')
    await fill('IP addresses or networks', '192.0.2.7, 2001:db8::/32')
    await click('Save limits')
    await until(() => expect(writes()).toHaveLength(1))
    expect(JSON.parse(writes()[0].data).ip_ranges).toEqual(['192.0.2.7', '2001:db8::/32'])
  })
  it('requires fresh review after 409 while preserving a draft and current parent restrictions', async () => {
    await mount()
    await click('Edit restrictions')
    await fill('RPM', '12')
    await fill('Reason for change', 'Reviewed')
    failure = 409
    await click('Save limits')
    await until(() => expect(document.body.textContent).toContain('policy or resource changed'))
    expect(button('Save limits').disabled).toBe(true)
    record = {
      ...record,
      etag: 'new',
      parent_etag: 'parent2',
      ip_policies: [{ ...record.ip_policies[0], rpm: 10 }, record.stored],
    }
    failure = 0
    await click('Reload current policy')
    await until(() => expect(button('Save limits').disabled).toBe(false))
    expect((document.querySelector('[aria-label="RPM"]') as HTMLInputElement).value).toBe('12')
    await click('Save limits')
    expect(document.body.textContent).toContain('cannot exceed')
    await fill('RPM', '9')
    await click('Save limits')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].headers.get('If-Match')).toBe('"new"')
  })
  it('retries uncertain publication with the identical body and original ETag without claiming success', async () => {
    await mount()
    await click('Edit restrictions')
    await fill('RPM', '10')
    await fill('Reason for change', 'Tighten')
    failure = 503
    await click('Save limits')
    await until(() => expect(document.body.textContent).toContain('may already be stored'))
    expect(document.body.textContent).not.toContain('Limits saved and applied.')
    expect(
      (document.querySelector('[aria-label="RPM"]') as HTMLInputElement).matches(':disabled'),
    ).toBe(true)
    failure = 0
    await click('Retry application')
    await until(() => expect(host.textContent).toContain('Limits saved and applied.'))
    expect(writes()[1].data).toBe(writes()[0].data)
    expect(writes()[1].headers.get('If-Match')).toBe('"old"')
  })
  it('blocks a stale editor after background parent changes and retains the draft while reloading', async () => {
    await mount()
    await click('Edit restrictions')
    await fill('RPM', '5')
    record = { ...record, parent_etag: 'changed' }
    await act(async () => {
      cache.setQueryData(['resource-limits', '/keys/key_test'], structuredClone(record))
    })
    await until(() => expect(button('Save limits').disabled).toBe(true))
    await click('Reload current policy')
    await until(() => expect(button('Save limits').disabled).toBe(false))
    expect((document.querySelector('[aria-label="RPM"]') as HTMLInputElement).value).toBe('5')
    expect(writes()).toHaveLength(0)
  })
  it('keeps aggregate editing inline, uses scoped endpoints, and does not label an unpublished result enforced', async () => {
    record = {
      ...record,
      kind: 'project',
      parent_etag: undefined,
      stored: { ...record.stored, rpm: null },
      ip_policies: [record.stored],
      enforced: false,
    }
    await mount(true, false, '/projects/prj_scope')
    await click('Edit limits')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.body.textContent).toContain('Leave blank for no local numeric limit')
    await fill('Reason for change', 'Aggregate')
    await click('Save limits')
    await until(() =>
      expect(host.textContent).toContain('Saved, but runtime application is not confirmed.'),
    )
    expect(writes()[0].url).toBe('/projects/prj_scope/limits')
    expect(host.textContent).not.toContain('Enforced by the gateway')
  })
  it('switches existing errors and labels without clearing a draft', async () => {
    await mount()
    await click('Edit restrictions')
    await fill('RPM', '5')
    await click('Save limits')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(document.body.textContent).toContain('变更原因')
    expect(document.body.textContent).toContain('请填写不超过 2,000')
    expect((document.querySelector('[aria-label="RPM"]') as HTMLInputElement).value).toBe('5')
    expect(document.body.textContent).toContain('key_original')
  })
})

describe('Resource quota controls', () => {
  it('edits every token period, TPM and exact-decimal budget while preserving null and zero', async () => {
    await mount()
    await click('Edit restrictions')
    await fill('5-hour token quota', '0')
    await fill('7-day token quota', '9007199254740991')
    await fill('Monthly token quota', '')
    await fill('TPM', '0')
    await fill('Monthly budget', '999999999999999999.000000000000000001')
    await fill('Reason for change', 'Reviewed quotas')
    await click('Save limits')
    await until(() => expect(writes()).toHaveLength(1))
    expect(JSON.parse(writes()[0].data)).toMatchObject({
      tokens_5h: 0,
      tokens_7d: 9007199254740991,
      tokens_month: null,
      tpm: 0,
      money_month: '999999999999999999.000000000000000001',
      currency: 'USD',
    })
  })
  it('clears the local currency with an inherited budget and preserves an explicit zero budget', async () => {
    await mount()
    await click('Edit restrictions')
    await fill('Monthly budget', '')
    await fill('Reason for change', 'Inherit budget')
    await click('Save limits')
    await until(() => expect(host.textContent).toContain('Limits saved and applied.'))
    expect(JSON.parse(writes()[0].data)).toMatchObject({ money_month: null, currency: '' })
    await click('Edit restrictions')
    await fill('Monthly budget', '0')
    await fill('Reason for change', 'Close budget')
    await click('Save limits')
    await until(() => expect(writes()).toHaveLength(2))
    expect(JSON.parse(writes()[1].data)).toMatchObject({ money_month: '0', currency: 'USD' })
  })
  it('rejects invalid decimal amounts and unsafe integers in every quota field without writing', async () => {
    await mount()
    await click('Edit restrictions')
    await fill('Reason for change', 'Validate quota')
    for (const label of ['5-hour token quota', '7-day token quota', 'Monthly token quota', 'TPM']) {
      const input = document.querySelector<HTMLInputElement>(`[aria-label="${label}"]`)!
      const original = input.value
      await fill(label, '9007199254740992')
      await click('Save limits')
      expect(document.body.textContent).toContain('nonnegative safe integer')
      await fill(label, original)
    }
    for (const value of [
      '1e3',
      '01',
      '-1',
      '1,000',
      '.1',
      '1.',
      '1000000000000000000',
      '0.0000000000000000001',
    ]) {
      await fill('Monthly budget', value)
      await click('Save limits')
      expect(document.body.textContent).toContain('nonnegative decimal')
    }
    expect(writes()).toHaveLength(0)
  })
  it('narrows parent token and budget limits using exact decimal comparison', async () => {
    record.ip_policies[0] = {
      ...record.ip_policies[0],
      tokens_5h: 1000,
      tokens_7d: 2000,
      tokens_month: 5000,
      tpm: 250,
      money_month: '12.500000000000000001',
      currency: 'USD',
    }
    await mount()
    await click('Edit restrictions')
    await fill('Reason for change', 'Narrow parent')
    for (const [label, amount, original] of [
      ['5-hour token quota', '1001', '1000'],
      ['7-day token quota', '2001', ''],
      ['Monthly token quota', '5001', '5000'],
      ['TPM', '251', '250'],
      ['Monthly budget', '12.500000000000000002', '12.500000000000000001'],
    ]) {
      await fill(label, amount)
      await click('Save limits')
      expect(document.body.textContent).toContain('cannot exceed')
      await fill(label, original)
    }
    expect(writes()).toHaveLength(0)
    await click('Save limits')
    await until(() => expect(writes()).toHaveLength(1))
    expect(JSON.parse(writes()[0].data).money_month).toBe('12.500000000000000001')
  })
  it('retains drafts while requiring explicit review of a changed platform currency', async () => {
    record.stored = { ...record.stored, money_month: null, currency: '' }
    await mount()
    await click('Edit restrictions')
    await fill('Monthly budget', '0.000000000000000001')
    await fill('Reason for change', 'Set budget')
    record = { ...record, platform_currency: 'EUR' }
    await act(async () => {
      cache.setQueryData(['resource-limits', '/keys/key_test'], structuredClone(record))
    })
    await until(() => expect(button('Save limits').disabled).toBe(true))
    expect(document.querySelector<HTMLInputElement>('[aria-label="Monthly budget"]')!.value).toBe(
      '0.000000000000000001',
    )
    await click('Reload current policy')
    await until(() => expect(button('Save limits').disabled).toBe(false))
    expect(document.body.textContent).toContain('Platform currency: EUR')
    expect(document.querySelector<HTMLInputElement>('[aria-label="Monthly budget"]')!.value).toBe(
      '0.000000000000000001',
    )
    await click('Save limits')
    await until(() => expect(writes()).toHaveLength(1))
    expect(JSON.parse(writes()[0].data)).toMatchObject({
      money_month: '0.000000000000000001',
      currency: 'EUR',
    })
  })
  it('blocks monetary writes when the authorized response has no platform currency', async () => {
    record.platform_currency = ''
    await mount()
    await click('Edit restrictions')
    await fill('Reason for change', 'Budget')
    await click('Save limits')
    expect(document.body.textContent).toContain('platform currency is unavailable')
    expect(writes()).toHaveLength(0)
    expect(requests.every((request) => !request.url?.includes('/admin/currency'))).toBe(true)
  })
  it('shows absence and inactive accounting as unknown rather than zero usage', async () => {
    await mount(false)
    expect(host.textContent).toContain(
      'Quota usage is unavailable. It must not be treated as zero.',
    )
    record = {
      ...record,
      quota_usage: {
        as_of: null,
        activated: false,
        coverage_start: null,
        time_zone: 'UTC',
        active: null,
        minute: null,
        five_hours: null,
        seven_days: null,
        month: null,
      },
    }
    await act(async () =>
      cache.setQueryData(['resource-limits', '/keys/key_test'], structuredClone(record)),
    )
    await until(() => expect(host.textContent).toContain('Historical usage is unknown, not zero.'))
    expect(host.textContent).not.toContain('Known tokens used: 0')
  })
  it('shows coverage gaps, unknown calls, active reservations and exact historical currencies separately', async () => {
    const window = {
      covered: false,
      tokens_used: 23,
      tokens_held: 5,
      tokens_unknown: 2,
      money_used: { USD: '0.123456789012345678', EUR: '999999999999999999.000000000000000001' },
      money_held: { USD: '1.000000000000000001' },
      money_unknown: 3,
    }
    record.quota_usage = {
      as_of: '2026-09-30T08:00:00Z',
      activated: true,
      coverage_start: '2026-09-29T08:00:00Z',
      time_zone: 'UTC',
      active: { ...window, covered: true, tokens_used: 0, tokens_held: 50 },
      minute: null,
      five_hours: window,
      seven_days: window,
      month: window,
    }
    await mount(false)
    expect(host.textContent).toContain('Incomplete accounting coverage')
    expect(host.textContent).toContain('Complete accounting coverage')
    expect(host.textContent).toContain('Known tokens used: 23')
    expect(host.textContent).toContain('Tokens reserved: 50')
    expect(host.textContent).toContain('Calls with unknown tokens: 2')
    expect(host.textContent).toContain('Calls with unknown amounts: 3')
    expect(host.textContent).toContain('999999999999999999.000000000000000001 EUR')
    expect(host.textContent).toContain('0.123456789012345678 USD')
    expect(host.textContent).toContain('1.000000000000000001 USD')
    expect(host.textContent).not.toContain('Remaining:')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(host.textContent).toContain('记账覆盖不完整')
    expect(host.textContent).toContain('预留 Token：50')
    expect(host.textContent).toContain('金额未知的调用：3')
    expect(host.textContent).toContain('999999999999999999.000000000000000001 EUR')
  })
  it('retries a quota write with the same exact amount and denomination after an uncertain result', async () => {
    await mount()
    await click('Edit restrictions')
    await fill('Monthly budget', '0.000000000000000001')
    await fill('Reason for change', 'Precise budget')
    failure = 503
    await click('Save limits')
    await until(() => expect(button('Retry application')).toBeDefined())
    record = { ...record, platform_currency: 'EUR' }
    await act(async () => {
      cache.setQueryData(['resource-limits', '/keys/key_test'], structuredClone(record))
    })
    failure = 0
    await click('Retry application')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].data).toBe(writes()[0].data)
    expect(JSON.parse(writes()[1].data)).toMatchObject({
      money_month: '0.000000000000000001',
      currency: 'USD',
    })
  })
})

describe('Scoped limit drafts', () => {
  it('destroys the old resource draft and submission intent when switching the resource path', async () => {
    await mount()
    await click('Edit restrictions')
    await fill('Monthly budget', '0.000000000000000001')
    await fill('Reason for change', 'Old resource')
    failure = 503
    await click('Save limits')
    await until(() => expect(button('Retry application')).toBeDefined())
    failure = 0
    record = { ...record, id: 'key_second', account_id: 'key_second_account', etag: 'second' }
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <ResourceLimits path="/keys/key_second" canEdit child />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(host.textContent).toContain('key_second_account'))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.body.textContent).not.toContain('Retry application')
    await click('Edit restrictions')
    expect(document.querySelector<HTMLInputElement>('[aria-label="Monthly budget"]')!.value).toBe(
      '12.500000000000000001',
    )
    expect(
      document.querySelector<HTMLInputElement>('[aria-label="Reason for change"]')!.value,
    ).toBe('')
    await fill('Reason for change', 'New resource')
    await click('Save limits')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].url).toBe('/keys/key_second/limits')
    expect(writes()[1].headers.get('If-Match')).toBe('"second"')
    expect(JSON.parse(writes()[1].data).money_month).toBe('12.500000000000000001')
  })
})

it('permits a Personal Key monthly ceiling above its alert-only User parent without adding Key behavior input', async () => {
  record.ip_policies[0] = {
    ...record.ip_policies[0],
    tokens_month: 100,
    tokens_month_behavior: 'alert_only',
    money_month: '1.000000000000000001',
    money_month_behavior: 'alert_only',
    currency: 'USD',
  }
  await mount()
  await click('Edit restrictions')
  expect(document.body.textContent).toContain('Current parent alert-only threshold: 100')
  await fill('Monthly token quota', '200')
  await fill('Monthly budget', '2.000000000000000002')
  await fill('Reason for change', 'Independent Key stop')
  await click('Save limits')
  await until(() => expect(writes()).toHaveLength(1))
  const body = JSON.parse(writes()[0].data)
  expect(body.tokens_month).toBe(200)
  expect(body.money_month).toBe('2.000000000000000002')
  expect(body).not.toHaveProperty('tokens_month_behavior')
  expect(body).not.toHaveProperty('money_month_behavior')
})
it('retains currency validation under an alert-only User money parent', async () => {
  record.ip_policies[0] = {
    ...record.ip_policies[0],
    money_month: '1',
    money_month_behavior: 'alert_only',
    currency: 'EUR',
  }
  await mount()
  await click('Edit restrictions')
  await fill('Monthly budget', '2')
  await fill('Reason for change', 'Currency mismatch')
  await click('Save limits')
  expect(document.body.textContent).toContain('cannot exceed')
  expect(writes()).toHaveLength(0)
})
