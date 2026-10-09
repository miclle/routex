import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  cleanupProviderOrphan,
  getProviderCleanupReceipt,
  getProviderOrphan,
  getProviderOrphans,
  parseProviderOrphan,
  parseProviderCleanupReceipt,
} from './provider-credential-cleanup'
import type { ProviderCleanupIntent } from '@/types/provider-credential-cleanup'
const integration = 'vlt_01j00000000000000000000000',
  revision = 'vlr_01j00000000000000000000000',
  creation = '11111111-1111-4111-8111-111111111111',
  command = '22222222-2222-4222-8222-222222222222',
  etag = 'a'.repeat(64) + '.' + 'b'.repeat(64)
const observation = (succeeded = false) => ({
  attempted: succeeded,
  succeeded,
  duration_ms: '1',
  failure: null,
})
const orphan = () => ({
  creation_request_id: creation,
  kind: 'credential',
  provider_id: 'prv_01j00000000000000000000000',
  connection_id: 'con_01j00000000000000000000000',
  credential_id: 'crd_01j00000000000000000000000',
  integration_id: integration,
  revision_id: revision,
  created_at: '2026-10-07T00:00:00Z',
  state: 'orphan',
  write: observation(true),
  read: observation(true),
  ownership_recorded: true,
  eligible: true,
  blocker_codes: [],
  can_cleanup: true,
  review_etag: etag,
})
const receipt = () => ({
  creation_request_id: creation,
  request_id: command,
  integration_id: integration,
  revision_id: revision,
  state: 'acknowledged',
  ownership: observation(true),
  cleanup: { state: 'acknowledged', observation: observation(true) },
  started_at: '2026-10-07T01:00:00Z',
  finished_at: '2026-10-07T01:00:01Z',
})
const intent = (): ProviderCleanupIntent => ({
  integration_id: integration,
  creation_request_id: creation,
  revision_id: revision,
  etag,
  input: { request_id: command, reason: 'Explicit owned version review' },
})
let old: typeof client.defaults.adapter,
  requests: InternalAxiosRequestConfig[],
  data: unknown,
  status: number,
  headers: Record<string, string>
beforeEach(() => {
  old = client.defaults.adapter
  requests = []
  data = orphan()
  status = 200
  headers = { etag: `"${etag}"` }
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { data, status, statusText: String(status), headers, config }
  }
})
afterEach(() => {
  client.defaults.adapter = old
  vi.restoreAllMocks()
})
it('reads only exact scoped routes and matching strong detail ETag', async () => {
  const row = await getProviderOrphan(integration, creation)
  expect(row).toEqual(orphan())
  expect(requests[0].url).toBe(
    `/admin/secrets/integrations/${integration}/provider-orphans/${creation}`,
  )
  expect(requests[0].data).toBeUndefined()
})
it('reads bounded page and canonical actor-bound cursor', async () => {
  data = { items: [orphan()], next_cursor: creation + '.' + 'c'.repeat(64) }
  await getProviderOrphans(integration)
  await getProviderOrphans(integration, creation + '.' + 'c'.repeat(64))
  expect(requests[1].url).toContain('limit=20&cursor=')
  expect(requests.every((r) => !r.url?.includes('/store'))).toBe(true)
})
it.each(['', `W/"${etag}"`, `"${etag}x"`, etag])(
  'rejects absent/weak/mismatched detail header %s',
  async (header) => {
    headers = { etag: header }
    await expect(getProviderOrphan(integration, creation)).rejects.toMatchObject({ status: 0 })
  },
)
it.each(['cleanup_token', 'reference', 'marker', 'expected_sha256', 'ciphertext'])(
  'rejects unexpected private DTO field %s',
  (field) => {
    expect(() => parseProviderOrphan({ ...orphan(), [field]: 'private' })).toThrow(
      'Provider cleanup request failed',
    )
  },
)
it.each([undefined, null, 'true', 1, {}])('rejects nonboolean eligibility %s', (eligible) => {
  expect(() => parseProviderOrphan({ ...orphan(), eligible })).toThrow()
})
it.each(['kind', 'state', 'creation_request_id', 'revision_id', 'integration_id', 'credential_id'])(
  'rejects unknown or aliased %s',
  (field) => {
    expect(() => parseProviderOrphan({ ...orphan(), [field]: 'UNKNOWN' })).toThrow()
  },
)
it('unknown blockers remain visible blocked facts and never eligible', () => {
  expect(
    parseProviderOrphan({ ...orphan(), eligible: false, blocker_codes: ['future_condition'] }),
  ).toMatchObject({ eligible: false })
  expect(() => parseProviderOrphan({ ...orphan(), blocker_codes: ['future_condition'] })).toThrow()
  expect(() => parseProviderOrphan({ ...orphan(), ownership_recorded: false })).toThrow()
})
it('accepts server-reviewed committed eligibility without inventing a deleted creation state', () => {
  expect(parseProviderOrphan({ ...orphan(), state: 'committed' })).toMatchObject({
    state: 'committed',
    eligible: true,
    ownership_recorded: true,
  })
  for (const blocker of [
    'published_process_unproven',
    'published_source_unavailable',
    'published_drain_unproven',
  ]) {
    expect(
      parseProviderOrphan({
        ...orphan(),
        state: 'committed',
        eligible: false,
        blocker_codes: [blocker],
      }),
    ).toMatchObject({ eligible: false, blocker_codes: [blocker] })
    expect(() =>
      parseProviderOrphan({ ...orphan(), state: 'committed', blocker_codes: [blocker] }),
    ).toThrow()
  }
  expect(() =>
    parseProviderOrphan({
      ...orphan(),
      state: 'committed',
      ownership_recorded: false,
      read: observation(),
    }),
  ).toThrow()
})
it.each(['writing', 'awaiting_read', 'unknown', 'owned'])(
  'does not broaden eligibility to unresolved creation state %s',
  (state) => {
    expect(() => parseProviderOrphan({ ...orphan(), state })).toThrow()
  },
)
it('bounds page count, duplicate identities and cursor syntax without dispatch', async () => {
  for (const value of [
    { items: Array.from({ length: 21 }, () => orphan()), next_cursor: null },
    { items: [orphan(), orphan()], next_cursor: null },
    { items: [], next_cursor: 'arbitrary' },
  ]) {
    data = value
    await expect(getProviderOrphans(integration)).rejects.toMatchObject({ status: 0 })
  }
  requests = []
  await expect(getProviderOrphans(integration, 'arbitrary')).rejects.toMatchObject({ status: 0 })
  expect(requests).toHaveLength(0)
})
it('sends first transient Token once and reconciles exact token-free intent', async () => {
  data = { receipt: receipt(), running: false }
  await cleanupProviderOrphan(intent(), 'c'.repeat(64), undefined, 'cleanup-secret')
  await cleanupProviderOrphan(intent(), 'd'.repeat(64))
  expect(JSON.parse(requests[0].data)).toEqual({
    ...intent().input,
    cleanup_token: 'cleanup-secret',
  })
  expect(JSON.parse(requests[1].data)).toEqual(intent().input)
  expect(requests[1].headers.get('If-Match')).toBe(`"${etag}"`)
  expect(requests[1].headers.get('X-CSRF-Token')).toBe('d'.repeat(64))
  expect(intent()).not.toHaveProperty('cleanup_token')
})
it('accepts exact running202 and preserves unknown/failed200', async () => {
  for (const state of ['pending', 'unknown', 'failed']) {
    const r = {
      ...receipt(),
      state,
      finished_at: null,
      ownership: observation(),
      cleanup: { state: 'unknown', observation: observation() },
    }
    status = state === 'pending' ? 202 : 200
    data = { receipt: r, running: state === 'pending' }
    expect((await cleanupProviderOrphan(intent(), 'c'.repeat(64))).receipt.state).toBe(state)
  }
})
it.each([
  [200, 'pending', true],
  [202, 'acknowledged', false],
  [202, 'unknown', true],
  [201, 'acknowledged', false],
])('rejects wrong status/running/state %s %s %s', async (code, state, running) => {
  status = code as number
  data = { receipt: { ...receipt(), state }, running }
  await expect(cleanupProviderOrphan(intent(), 'c'.repeat(64))).rejects.toMatchObject({ status: 0 })
})
it('acknowledgement requires independent owned read, destroy and durable finish', () => {
  for (const change of [
    { finished_at: null },
    { ownership: observation() },
    { cleanup: { state: 'unknown', observation: observation(true) } },
    { cleanup: { state: 'acknowledged', observation: observation() } },
    { finished_at: '2026-10-07T00:00:00Z' },
  ])
    expect(() => parseProviderCleanupReceipt({ ...receipt(), ...change })).toThrow()
})
it('receipt identity is exact and GET404 never completes a command', async () => {
  data = { ...receipt(), request_id: creation }
  await expect(getProviderCleanupReceipt(intent())).rejects.toThrow()
  client.defaults.adapter = async (config) => {
    throw new AxiosError('private response', undefined, config, undefined, {
      data: { secret: 'never shown' },
      status: 404,
      statusText: '404',
      headers: {},
      config,
    })
  }
  await expect(getProviderCleanupReceipt(intent())).rejects.toMatchObject({
    status: 404,
    message: 'Provider cleanup request failed',
  })
})
it('rejects token-bearing retained intent and malformed token/reason before transport', async () => {
  for (const candidate of [
    { ...intent(), cleanup_token: 'not retained' },
    { ...intent(), input: { ...intent().input, cleanup_token: 'not retained' } },
    { ...intent(), input: { ...intent().input, reason: ' trailing ' } },
  ])
    await expect(
      cleanupProviderOrphan(candidate as ProviderCleanupIntent, 'c'.repeat(64)),
    ).rejects.toThrow()
  await expect(
    cleanupProviderOrphan(intent(), 'c'.repeat(64), undefined, ' space'),
  ).rejects.toThrow()
  expect(requests).toHaveLength(0)
})
it('propagates AbortSignal through the normal client without exposing errors', async () => {
  const abort = new AbortController()
  await getProviderOrphan(integration, creation, abort.signal)
  expect(requests[0].signal).toBe(abort.signal)
})
