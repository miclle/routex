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
    parent_etag: 'parent1',
    stored: { rpm: null, concurrency: 2, ip_mode: 'none', ip_ranges: [] },
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
          ip_policies: [record.ip_policies[0], policy],
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
