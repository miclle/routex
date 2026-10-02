import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import {
  focusManager,
  onlineManager,
  QueryClient,
  QueryClientProvider,
} from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { CredentialReadiness } from '@/types/credential-readiness'
import CredentialReadinessDialog from './credential-readiness'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const adapter = client.defaults.adapter
const source = 'crd_01aaaaaaaaaaaaaaaaaaaaaaaa',
  replacement = 'crd_01bbbbbbbbbbbbbbbbbbbbbbbb',
  snapshot = 'cfg_01cccccccccccccccccccccccc',
  connection = 'con_readiness',
  etag = 'a'.repeat(64)
let root: Root, host: HTMLDivElement, cache: QueryClient
let record: CredentialReadiness, requests: InternalAxiosRequestConfig[], permissions: string[]
let failure: number, responseStatus: number, responseETag: string, hold: Promise<void> | undefined
let writeStatus: number, applied: boolean, corrupt: boolean
const closed = vi.fn()
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const metadata = {
    connection_id: connection,
    priority: 0,
    enabled: true,
    verification_status: 'verified' as const,
    verified_at: null,
    etag,
  }
  record = {
    source: { ...metadata, id: source, name: 'Original' },
    replacement: { ...metadata, id: replacement, name: 'Replacement' },
    snapshot_id: snapshot,
    evidence: { attempt_id: 'att_readiness', completed_at: '2026-10-02T07:00:00Z' },
    eligible_route_count: 1,
    eligible: true,
    blockers: [],
    etag,
  }
  requests = []
  permissions = ['providers.read', 'providers.write']
  failure = 0
  writeStatus = 200
  applied = true
  corrupt = false
  responseStatus = 200
  responseETag = `"${etag}"`
  hold = undefined
  closed.mockClear()
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
      response.data = { user: { id: 'usr_readiness', role: 'member' }, csrf_token: 'csrf' }
    else if (config.url === '/auth/permissions') response.data = { permissions }
    else if (config.url === '/admin/providers')
      response.data = {
        items: [
          {
            id: 'prv_readiness',
            name: 'Provider',
            connections: [
              {
                id: connection,
                name: 'Primary',
                protocol: 'openai_chat',
                base_url: 'https://upstream.example.invalid',
                credentials: [
                  record.source,
                  { ...record.replacement, replaces_credential_id: source },
                ],
                provider_models: [],
              },
            ],
          },
          { id: 'prv_other', name: 'Other', connections: [] },
        ],
      }
    else if (config.url?.endsWith('/retire')) {
      const body = JSON.parse(config.data as string)
      response.status = writeStatus
      if (writeStatus !== 200)
        throw new AxiosError('Retirement unavailable', '', config, undefined, response)
      response.data = {
        request_id: body.request_id,
        source_credential_id: source,
        replacement_credential_id: corrupt ? source : replacement,
        committed: true,
        committed_at: '2026-10-02T08:00:00Z',
        runtime_applied: applied,
        current_snapshot_id: snapshot,
        blockers: applied ? [] : ['source_reenabled'],
      }
    } else if (config.url?.endsWith('/retirement-readiness')) {
      if (hold) await hold
      if (failure) {
        response.status = failure
        throw new AxiosError('Readiness unavailable', '', config, undefined, response)
      }
      response.status = responseStatus
      response.headers.set('etag', responseETag)
      response.data = structuredClone(record)
    } else throw new Error(`Unexpected request ${config.url}`)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = adapter
  focusManager.setFocused(undefined)
  onlineManager.setOnline(true)
  await i18n.changeLanguage('en')
})
async function until(assert: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
    try {
      assert()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
function button(label: string) {
  return [...document.querySelectorAll<HTMLElement>('button,[role="menuitem"]')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )! as HTMLButtonElement
}
async function click(label: string) {
  await act(async () => button(label).click())
}
const reads = () => requests.filter((request) => request.url?.endsWith('/retirement-readiness'))
async function mount(credentialId = replacement) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <CredentialReadinessDialog
          providerId="prv_readiness"
          credentialId={credentialId}
          sourceCredentialId={source}
          connectionId={connection}
          connectionName="Primary"
          onClose={closed}
        />
      </QueryClientProvider>,
    ),
  )
}
async function ready() {
  await mount()
  await until(() => expect(document.body.textContent).toContain('Current readiness conditions'))
}

const writes = () => requests.filter((request) => request.url?.endsWith('/retire'))
async function inputReason(value: string) {
  const input = document.querySelector<HTMLInputElement>('input')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function confirmRetirement() {
  await ready()
  await inputReason('Reviewed cutover')
  await click('Retire original credential')
  expect(writes()).toHaveLength(0)
  await until(() => expect(button('Confirm retirement')).toBeDefined())
  expect(document.body.textContent).toContain('att_readiness')
  await click('Confirm retirement')
}

describe('Reviewed Provider credential retirement', () => {
  it('requires explicit Base UI confirmation and dispatches only the reviewed native proof', async () => {
    await confirmRetirement()
    await until(() => expect(document.body.textContent).toContain('Retirement record saved'))
    expect(writes()).toHaveLength(1)
    expect(writes()[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(writes()[0].headers.get('X-CSRF-Token')).toBe('csrf')
    expect(JSON.parse(writes()[0].data as string)).toEqual({
      request_id: expect.stringMatching(/^[0-9a-f-]{36}$/),
      replacement_credential_id: replacement,
      evidence_attempt_id: 'att_readiness',
      snapshot_id: snapshot,
      reason: 'Reviewed cutover',
    })
    expect(document.body.textContent).toContain('current processing instance has applied')
    expect(button('Retry original retirement request')).toBeUndefined()
    expect(cache.getMutationCache().getAll()).toHaveLength(0)
  })
  it('keeps the reviewed attempt when a newer completion appears during focus or reconnect', async () => {
    await ready()
    record.evidence = { attempt_id: 'att_newer', completed_at: '2026-10-02T09:00:00Z' }
    record.etag = 'b'.repeat(64)
    responseETag = `"${record.etag}"`
    await act(async () => {
      focusManager.setFocused(false)
      onlineManager.setOnline(false)
      focusManager.setFocused(true)
      onlineManager.setOnline(true)
      await new Promise((resolve) => setTimeout(resolve, 30))
    })
    expect(reads()).toHaveLength(1)
    await inputReason('Reviewed cutover')
    await click('Retire original credential')
    await click('Confirm retirement')
    await until(() => expect(writes()).toHaveLength(1))
    expect(JSON.parse(writes()[0].data as string).evidence_attempt_id).toBe('att_readiness')
    expect(writes()[0].headers.get('If-Match')).toBe(`"${etag}"`)
  })
  it('allows independent read-only review without presenting a retirement form', async () => {
    permissions = ['providers.read']
    await ready()
    expect(button('Retire original credential')).toBeUndefined()
    expect(document.querySelector('input')).toBeNull()
    expect(writes()).toHaveLength(0)
  })
  it('does not enable retirement without eligible native proof', async () => {
    record.eligible = false
    record.evidence = null
    record.blockers = ['evidence_missing']
    await mount()
    await until(() => expect(button('Retire original credential')).toBeDefined())
    expect(button('Retire original credential').disabled).toBe(true)
    expect(writes()).toHaveLength(0)
  })
  it('preserves the reason and requires explicit review after a conflict', async () => {
    writeStatus = 409
    await confirmRetirement()
    await until(() => expect(document.body.textContent).toContain('Readiness changed'))
    expect(document.querySelector<HTMLInputElement>('input')!.value).toBe('Reviewed cutover')
    expect(button('Retire original credential').disabled).toBe(true)
    record.etag = 'b'.repeat(64)
    responseETag = `"${record.etag}"`
    await click('Refresh readiness')
    await until(() => expect(reads()).toHaveLength(2))
    expect(button('Retire original credential').disabled).toBe(true)
    await click('Review current readiness')
    await until(() => expect(button('Retire original credential').disabled).toBe(false))
    writeStatus = 200
    await click('Retire original credential')
    await click('Confirm retirement')
    await until(() => expect(writes()).toHaveLength(2))
    expect(writes()[1].headers.get('If-Match')).toBe(`"${record.etag}"`)
    expect(JSON.parse(writes()[1].data as string).request_id).not.toBe(
      JSON.parse(writes()[0].data as string).request_id,
    )
  })
  it('retains identical intent through an uncertain result, readiness failure and rejected retry', async () => {
    writeStatus = 503
    await confirmRetirement()
    await until(() => expect(button('Retry original retirement request')).toBeDefined())
    const original = writes()[0].data
    failure = 404
    await click('Refresh readiness')
    await until(() => expect(document.querySelector('[role="alert"]')).not.toBeNull())
    expect(button('Retry original retirement request')).toBeDefined()
    writeStatus = 409
    await click('Retry original retirement request')
    await until(() =>
      expect(document.body.textContent).toContain('Could not confirm the original request'),
    )
    expect(document.body.textContent).toContain('retirement result is uncertain')
    expect(writes()[1].data).toBe(original)
    writeStatus = 200
    await click('Retry original retirement request')
    await until(() => expect(document.body.textContent).toContain('Retirement record saved'))
    expect(writes()[2].data).toBe(original)
    expect(writes()[2].headers.get('If-Match')).toBe(writes()[0].headers.get('If-Match'))
  })
  it('separates saved history from current application and retries without disabling again', async () => {
    applied = false
    await confirmRetirement()
    await until(() => expect(document.body.textContent).toContain('historical retirement is saved'))
    expect(document.body.textContent).toContain('Retrying this request will not disable it again')
    expect(document.body.textContent).not.toContain('current processing instance has applied')
    const original = writes()[0].data
    writeStatus = 403
    await click('Retry original retirement request')
    await until(() =>
      expect(document.body.textContent).toContain('Could not confirm the original request'),
    )
    expect(document.body.textContent).toContain('Retirement record saved')
    expect(writes()[1].data).toBe(original)
  })
  it('rejects a malformed saved receipt as uncertainty rather than success', async () => {
    corrupt = true
    await confirmRetirement()
    await until(() => expect(button('Retry original retirement request')).toBeDefined())
    expect(document.body.textContent).not.toContain('Retirement record saved')
  })
  it('switches confirmation and pending notices to Chinese without losing the reason', async () => {
    await ready()
    await inputReason('Reviewed cutover')
    await click('Retire original credential')
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('退役原凭证？')
    expect(document.body.textContent).toContain('原因：Reviewed cutover')
    applied = false
    await click('确认退役')
    await until(() => expect(document.body.textContent).toContain('历史退役已保存'))
    expect(button('重试原退役请求')).toBeDefined()
  })
  it('cancels confirmation without sending a write', async () => {
    await ready()
    await inputReason('Reviewed cutover')
    await click('Retire original credential')
    await click('Cancel')
    expect(writes()).toHaveLength(0)
    expect(document.querySelector<HTMLInputElement>('input')!.value).toBe('Reviewed cutover')
  })
  it('blocks duplicate confirmation dispatch while a request is in flight', async () => {
    await ready()
    await inputReason('Reviewed cutover')
    await click('Retire original credential')
    await act(async () => {
      button('Confirm retirement').click()
      button('Confirm retirement').click()
    })
    await until(() => expect(writes()).toHaveLength(1))
  })
})
