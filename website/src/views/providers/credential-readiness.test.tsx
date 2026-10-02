import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import type { CredentialReadiness } from '@/types/credential-readiness'
import CredentialReadinessDialog from './credential-readiness'
import ProvidersPage from './index'

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
let router: ReturnType<typeof createMemoryRouter> | undefined
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
  permissions = ['providers.read']
  failure = 0
  responseStatus = 200
  responseETag = `"${etag}"`
  hold = undefined
  router = undefined
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
    else if (config.url?.endsWith('/retirement-readiness')) {
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
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = adapter
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

describe('Credential retirement readiness review', () => {
  it('uses the exact linked source and replacement with read-only authority and never dispatches writes', async () => {
    await ready()
    expect(reads()).toHaveLength(1)
    expect(reads()[0].url).toBe(`/admin/credentials/${source}/retirement-readiness`)
    expect(reads()[0].params).toEqual({ replacement_credential_id: replacement })
    expect(document.body.textContent).toContain('This check does not retire either credential.')
    expect(document.body.textContent).toContain('att_readiness')
    expect(document.body.textContent).toContain(snapshot)
    expect(document.body.textContent).toContain('1 route')
    expect(document.querySelector('input')).toBeNull()
    expect(button('Complete rotation')).toBeUndefined()
    expect(requests.every((request) => request.method === 'get')).toBe(true)
    await click('Close')
    expect(closed).toHaveBeenCalledOnce()
  })
  it('rejects malformed or identical target IDs before a readiness request', async () => {
    await mount(source)
    await until(() => expect(document.querySelector('[role="alert"]')).not.toBeNull())
    expect(reads()).toHaveLength(0)
    await mount('../unexpected')
    await until(() => expect(document.querySelector('[role="alert"]')).not.toBeNull())
    expect(reads()).toHaveLength(0)
  })
  it('requires read permission independently of write permission', async () => {
    permissions = ['providers.write']
    await mount()
    await until(() =>
      expect(requests.some((request) => request.url === '/auth/permissions')).toBe(true),
    )
    expect(reads()).toHaveLength(0)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
  it('localizes server blockers and unknown values without printing arbitrary server labels', async () => {
    record.eligible = false
    record.snapshot_id = null
    record.evidence = null
    record.eligible_route_count = 0
    record.blockers = ['replacement_disabled', 'evidence_missing', 'future_condition']
    await mount()
    await until(() => expect(document.body.textContent).toContain('Explicitly enable'))
    expect(document.body.textContent).toContain('No qualifying native completion')
    expect(document.body.textContent).not.toContain('future_condition')
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.textContent).toContain('此检查不会退役任何凭证')
    expect(document.body.textContent).toContain('验证通过后，请单独启用替换凭证')
    expect(document.body.textContent).not.toContain('0 条路由')
    expect(document.body.textContent).toContain('尚未确认')
    expect(reads()).toHaveLength(1)
  })
  it.each([
    'source',
    'replacement',
    'connection',
    'etag',
    'header',
    'snapshot',
    'count',
    'timestamp',
    'attempt',
    'missing-proof',
    'blocked-ready',
    'disabled-ready',
    'status',
  ])('rejects malformed or mismatched readiness %s', async (kind) => {
    if (kind === 'source') record.source.id = 'wrong'
    if (kind === 'replacement') record.replacement.id = 'wrong'
    if (kind === 'connection') record.replacement.connection_id = 'wrong'
    if (kind === 'etag') record.etag = 'weak'
    if (kind === 'header') responseETag = '"other"'
    if (kind === 'snapshot') record.snapshot_id = 'unknown'
    if (kind === 'count') record.eligible_route_count = 257
    if (kind === 'timestamp') record.evidence!.completed_at = 'invalid'
    if (kind === 'attempt') record.evidence!.attempt_id = 'bad attempt'
    if (kind === 'missing-proof') record.evidence = null
    if (kind === 'blocked-ready') record.blockers = ['evidence_missing']
    if (kind === 'disabled-ready') record.replacement.enabled = false
    if (kind === 'status') responseStatus = 202
    await mount()
    await until(() => expect(document.querySelector('[role="alert"]')).not.toBeNull())
    expect(document.body.textContent).not.toContain('Current readiness conditions are satisfied')
  })
  it('clears previous eligibility during refresh and after a failed refresh, then uses current server facts', async () => {
    await ready()
    let release!: () => void
    hold = new Promise((resolve) => {
      release = resolve
    })
    await click('Refresh readiness')
    await until(() =>
      expect(document.body.textContent).not.toContain('Current readiness conditions are satisfied'),
    )
    failure = 503
    await act(async () => release())
    await until(() => expect(document.querySelector('[role="alert"]')).not.toBeNull())
    expect(document.body.textContent).not.toContain('att_readiness')
    hold = undefined
    failure = 0
    record.eligible = false
    record.evidence = null
    record.blockers = ['runtime_stale']
    await click('Refresh readiness')
    await until(() => expect(document.body.textContent).toContain('published routes do not match'))
    expect(document.body.textContent).not.toContain('Current readiness conditions are satisfied')
    expect(requests.every((request) => request.method === 'get')).toBe(true)
  })
  it('rebinds the resource query on replacement changes without showing prior evidence', async () => {
    await ready()
    record.replacement.id = 'crd_01dddddddddddddddddddddddd'
    record.eligible = false
    record.evidence = null
    record.blockers = ['evidence_missing']
    await mount(record.replacement.id)
    expect(document.body.textContent).not.toContain('att_readiness')
    await until(() => expect(reads()).toHaveLength(2))
    expect(reads()[1].params.replacement_credential_id).toBe(record.replacement.id)
  })
  it('opens from the existing replacement row menu and does not reopen after Provider navigation', async () => {
    router = createMemoryRouter(
      [{ path: '/admin/providers/:providerId', element: <ProvidersPage /> }],
      { initialEntries: ['/admin/providers/prv_readiness?tab=credentials'] },
    )
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <RouterProvider router={router!} />
        </QueryClientProvider>,
      ),
    )
    await until(() => expect(button('Actions for Replacement')).toBeDefined())
    await click('Actions for Replacement')
    await until(() => expect(button('Review retirement readiness')).toBeDefined())
    await click('Review retirement readiness')
    await until(() => expect(document.body.textContent).toContain('Recorded native completion'))
    expect(reads()[0].params.replacement_credential_id).toBe(replacement)
    await act(async () => router!.navigate('/admin/providers/prv_other?tab=credentials'))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    await act(async () => router!.navigate('/admin/providers/prv_readiness?tab=credentials'))
    await until(() => expect(button('Actions for Replacement')).toBeDefined())
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(requests.every((request) => request.method === 'get')).toBe(true)
  })
})
