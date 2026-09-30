import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { ProviderModelCapacity } from '@/types/provider-model-capacity'
import ProviderModelCapacityCard from './provider-model-capacity'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let host: HTMLDivElement, root: Root, cache: QueryClient
let record: ProviderModelCapacity, permissions: string[], requests: InternalAxiosRequestConfig[]
let failure: number, hold: Promise<void> | undefined
const path = '/admin/provider-models/pmd_capacity/reservation-bound'
const originalAdapter = client.defaults.adapter

beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  record = {
    provider_model_id: 'pmd_capacity',
    protocol: 'openai_chat',
    etag: '0',
    configured: false,
    max_input_tokens: 0,
    max_output_tokens: 0,
    evidence: '',
    updated_at: '0001-01-01T00:00:00Z',
  }
  permissions = ['providers.read', 'providers.write']
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
      response.data = { user: { id: 'usr_capacity', role: 'member' }, csrf_token: 'capacity-csrf' }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url?.endsWith('/reservation-bound')) {
      if (config.method === 'put') {
        if (hold) await hold
        const input = JSON.parse(config.data)
        if (!failure || failure === 503)
          record = {
            ...record,
            ...input,
            configured: true,
            etag: 'saved',
            updated_at: '2026-09-30T08:00:00Z',
          }
        if (failure) {
          if (failure === -1) throw new AxiosError('Network result unavailable', '', config)
          response.status = failure
          throw new AxiosError('Capacity fixture', '', config, undefined, response)
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
  host.remove()
  client.defaults.adapter = originalAdapter
  await i18n.changeLanguage('en')
})

async function until(assert: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
    try {
      assert()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <ProviderModelCapacityCard modelId="pmd_capacity" />
      </QueryClientProvider>,
    ),
  )
  await until(() =>
    expect(host.textContent).toContain(
      record.configured ? 'Attestation recorded' : 'No attestation recorded',
    ),
  )
}
function button(label: string) {
  const result = [...document.querySelectorAll('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
  if (!result) throw new Error(`Missing button: ${label}`)
  return result
}
const writes = () => requests.filter((item) => item.method === 'put')
const input = (label: string) =>
  document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
async function click(label: string) {
  await act(async () => button(label).click())
}
async function fill(label: string, value: string) {
  const control = input(label)
  expect(control).not.toBeNull()
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(control, value)
    control.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function draft() {
  await click('Edit capacity attestation')
  await fill('Maximum billable input tokens', '128000')
  await fill('Maximum billable output tokens', '16384')
  await fill('Capacity evidence', '  Verified native contract  ')
  await fill('Reason for attestation', '  Enable bounded quota admission  ')
}

describe('provider model capacity attestation', () => {
  it('reads the scoped unconfigured record and never invents default capacity or validity', async () => {
    await render()
    expect(host.textContent).toContain('No attestation recorded')
    expect(host.textContent).toContain('does not certify current admission eligibility')
    expect(writes()).toHaveLength(0)
    const read = requests.find((item) => item.url === path)!
    expect(read.signal).toBeDefined()
    await click('Edit capacity attestation')
    expect(input('Maximum billable input tokens').value).toBe('')
    expect(input('Maximum billable output tokens').value).toBe('')
    expect(document.body.textContent).toContain('Reviewed revision: 0 · Protocol: openai_chat')
  })
  it('saves positive maxima and trimmed evidence with the exact reviewed ETag and CSRF, once while pending', async () => {
    await render()
    await draft()
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await act(async () => {
      button('Save attestation').click()
      button('Save attestation').click()
    })
    await until(() => expect(writes()).toHaveLength(1))
    expect(JSON.parse(writes()[0].data)).toEqual({
      max_input_tokens: 128000,
      max_output_tokens: 16384,
      evidence: 'Verified native contract',
      reason: 'Enable bounded quota admission',
    })
    expect(writes()[0].headers.get('If-Match')).toBe('"0"')
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('capacity-csrf')
    expect(button('Close').disabled).toBe(true)
    await act(async () => release())
    await until(() =>
      expect(host.textContent).toContain('Capacity attestation saved and published.'),
    )
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(host.textContent).toContain('128,000')
    expect(host.textContent).toContain('Verified native contract')
  })
  it('rejects nonpositive, fractional, exponent and unsafe integers and UTF-8 text overflow before dispatch', async () => {
    await render()
    await draft()
    for (const value of ['0', '-1', '1.5', '1e3', '9007199254740992']) {
      await fill('Maximum billable input tokens', value)
      await click('Save attestation')
      expect(document.body.textContent).toContain('positive safe integers')
    }
    await fill('Maximum billable input tokens', '128000')
    await fill('Maximum billable output tokens', '0')
    await click('Save attestation')
    expect(document.body.textContent).toContain('positive safe integers')
    await fill('Maximum billable output tokens', '16384')
    for (const label of ['Capacity evidence', 'Reason for attestation']) {
      for (const value of ['   ', '证'.repeat(667)]) {
        await fill(label, value)
        await click('Save attestation')
        expect(document.body.textContent).toContain('2,000 UTF-8 bytes')
      }
      await fill(label, 'Verified')
    }
    expect(writes()).toHaveLength(0)
  })
  it('preserves a draft through conflict and requires explicit review before replacing the ETag', async () => {
    await render()
    await draft()
    failure = 409
    await click('Save attestation')
    await until(() => expect(document.body.textContent).toContain('The attestation changed'))
    expect(button('Save attestation').disabled).toBe(true)
    record = {
      ...record,
      configured: true,
      etag: 'reviewed-new',
      max_input_tokens: 64000,
      max_output_tokens: 8000,
      evidence: 'Another administrator',
      updated_at: '2026-09-30T08:00:00Z',
    }
    await click('Load current attestation')
    await until(() => expect(button('Save attestation').disabled).toBe(false))
    expect(input('Maximum billable input tokens').value).toBe('128000')
    expect(input('Capacity evidence').value).toBe('  Verified native contract  ')
    expect(document.body.textContent).toContain(
      'Reading saved values does not confirm runtime publication',
    )
    failure = 0
    await click('Save attestation')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].headers.get('If-Match')).toBe('"reviewed-new"')
  })
  it('locks uncertain changes and retries the identical intent, retaining it through dialog dismissal', async () => {
    await render()
    await draft()
    failure = 503
    await click('Save attestation')
    await until(() =>
      expect(document.body.textContent).toContain('Publication could not be confirmed'),
    )
    expect(host.textContent).not.toContain('Capacity attestation saved and published.')
    expect(input('Maximum billable input tokens').matches(':disabled')).toBe(true)
    await click('Cancel')
    await click('Edit capacity attestation')
    expect(input('Maximum billable input tokens').matches(':disabled')).toBe(true)
    failure = 0
    await click('Retry publication')
    await until(() =>
      expect(host.textContent).toContain('Capacity attestation saved and published.'),
    )
    expect(writes()[1].data).toBe(writes()[0].data)
    expect(writes()[1].headers.get('If-Match')).toBe('"0"')
  })
  it('reconciles uncertain storage without reporting publication as success and preserves the draft for review', async () => {
    await render()
    await draft()
    failure = 503
    await click('Save attestation')
    await until(() =>
      expect(document.body.textContent).toContain('Publication could not be confirmed'),
    )
    await click('Load current attestation')
    await until(() => expect(button('Save attestation').disabled).toBe(false))
    expect(host.textContent).not.toContain('Capacity attestation saved and published.')
    expect(input('Maximum billable output tokens').value).toBe('16384')
    expect(document.body.textContent).toContain('Reviewed revision: saved')
    expect(writes()).toHaveLength(1)
    failure = 0
    await click('Save attestation')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].headers.get('If-Match')).toBe('"saved"')
  })
  it('keeps the original uncertain intent after a network failure and a rejected retry', async () => {
    await render()
    await draft()
    failure = -1
    await click('Save attestation')
    await until(() =>
      expect(document.body.textContent).toContain('Publication could not be confirmed'),
    )
    failure = 403
    await click('Retry publication')
    await until(() => expect(writes()).toHaveLength(2))
    expect(input('Capacity evidence').matches(':disabled')).toBe(true)
    expect(button('Save attestation').disabled).toBe(true)
    expect(host.textContent).not.toContain('Capacity attestation saved and published.')
    failure = 0
    await click('Retry publication')
    await until(() =>
      expect(host.textContent).toContain('Capacity attestation saved and published.'),
    )
    expect(writes()).toHaveLength(3)
    expect(writes()[1].data).toBe(writes()[0].data)
    expect(writes()[2].data).toBe(writes()[0].data)
    expect(writes()[2].headers.get('If-Match')).toBe('"0"')
  })
  it('blocks background revision changes until review and keeps the model-scoped cache boundary', async () => {
    await render()
    await draft()
    record = { ...record, etag: 'background' }
    await act(async () =>
      cache.setQueryData(['admin', 'provider-model-capacity', 'pmd_capacity'], record),
    )
    await until(() => expect(button('Save attestation').disabled).toBe(true))
    await click('Load current attestation')
    await until(() => expect(button('Save attestation').disabled).toBe(false))
    expect(input('Maximum billable output tokens').value).toBe('16384')
    expect(writes()).toHaveLength(0)
  })
  it('renders saved evidence without editing for read-only authority', async () => {
    permissions = ['providers.read']
    record = {
      ...record,
      configured: true,
      etag: 'read-only',
      max_input_tokens: 128000,
      max_output_tokens: 16384,
      evidence: 'Verified contract',
      updated_at: '2026-09-30T08:00:00Z',
    }
    await render()
    expect(host.textContent).toContain('Verified contract')
    expect(host.textContent).not.toContain('Edit capacity attestation')
    expect(writes()).toHaveLength(0)
  })
  it('destroys the prior draft on resource changes and submits only against the newly reviewed model', async () => {
    await render()
    await draft()
    await click('Cancel')
    await click('Edit capacity attestation')
    expect(input('Maximum billable input tokens').value).toBe('128000')
    record = { ...record, provider_model_id: 'pmd_other', protocol: 'anthropic_messages' }
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <ProviderModelCapacityCard modelId="pmd_other" />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(host.textContent).toContain('Protocol: anthropic_messages'))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    await click('Edit capacity attestation')
    expect(input('Maximum billable input tokens').value).toBe('')
    expect(input('Capacity evidence').value).toBe('')
    await fill('Maximum billable input tokens', '64000')
    await fill('Maximum billable output tokens', '8000')
    await fill('Capacity evidence', 'Second model contract')
    await fill('Reason for attestation', 'Second model')
    await click('Save attestation')
    await until(() => expect(writes()).toHaveLength(1))
    expect(writes()[0].url).toBe('/admin/provider-models/pmd_other/reservation-bound')
    expect(JSON.parse(writes()[0].data).evidence).toBe('Second model contract')
  })
  it('does not read the record with write-only authority', async () => {
    permissions = ['providers.write']
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <ProviderModelCapacityCard modelId="pmd_capacity" />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(requests.some((item) => item.url === '/auth/permissions')).toBe(true))
    expect(requests.some((item) => item.url === path)).toBe(false)
    expect(host.textContent).toBe('')
  })
  it('switches labels and validation to Chinese without losing draft values', async () => {
    await render()
    await draft()
    await fill('Maximum billable output tokens', '0')
    await click('Save attestation')
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('请输入两个 Token 上限的正安全整数')
    expect(input('最大计费输入 Token').value).toBe('128000')
    expect(button('保存声明')).toBeDefined()
  })
})
