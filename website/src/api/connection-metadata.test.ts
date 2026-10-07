import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  getConnectionMetadata,
  saveConnectionMetadata,
  validConnectionName,
  validConnectionReason,
} from './connection-metadata'
import type { ConnectionMetadata } from '@/types/connection-metadata'
const original = client.defaults.adapter
const identity = 'a'.repeat(64),
  token = `${identity}.${'b'.repeat(64)}`
let requests: InternalAxiosRequestConfig[], data: unknown, status: number, headers: AxiosHeaders
const row = (): ConnectionMetadata => ({
  id: 'con_one',
  provider_id: 'prv_one',
  name: 'Primary',
  protocol: 'openai_chat',
  adapter: 'native',
  api_version: null,
  base_url: 'https://upstream.example.invalid/v1',
  egress_mode: 'default',
  egress_id: null,
  etag: token,
  can_edit: true,
})
beforeEach(() => {
  requests = []
  data = row()
  status = 200
  headers = new AxiosHeaders({ 'Cache-Control': 'private, no-store', ETag: `"${token}"` })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { config, data, status, headers, statusText: '' }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
it('reads only the exact selected Connection with private coherent proof and no child API calls', async () => {
  expect(await getConnectionMetadata('prv_one', 'con_one')).toEqual(row())
  expect(requests).toHaveLength(1)
  expect(requests[0].url).toBe('/admin/connections/con_one/metadata')
})
it('sends only original name/reason with strong composite If-Match and current CSRF', async () => {
  data = {
    connection: { ...row(), name: 'Renamed', etag: `${identity}.${'c'.repeat(64)}` },
    runtime_applied: true,
    changed: false,
  }
  headers.set('etag', `"${identity}.${'c'.repeat(64)}"`)
  const input = { name: 'Renamed', reason: 'Reviewed maintenance' }
  expect(
    (await saveConnectionMetadata('prv_one', 'con_one', token, input, 'current-csrf')).changed,
  ).toBe(false)
  expect(JSON.parse(requests[0].data)).toEqual(input)
  expect(requests[0].headers.get('If-Match')).toBe(`"${token}"`)
  expect(requests[0].headers.get('X-CSRF-Token')).toBe('current-csrf')
})
describe('exact Connection metadata decoding', () => {
  it.each([
    ['extra secret', { ...row(), secret: 'not-public' }],
    ['missing editability', { ...row(), can_edit: undefined }],
    ['another target', { ...row(), id: 'con_other' }],
    ['another provider', { ...row(), provider_id: 'prv_other' }],
    ['raw row revision', { ...row(), etag: 'rev_raw' }],
    ['uppercase proof', { ...row(), etag: token.toUpperCase() }],
    ['unknown protocol', { ...row(), protocol: 'demonstration' }],
    ['proxy missing ID', { ...row(), egress_mode: 'proxy' }],
    ['default with resolved proxy ID', { ...row(), egress_id: 'egr_resolved' }],
    ['URL secret', { ...row(), base_url: 'https://name:secret@example.invalid' }],
    ['URL query', { ...row(), base_url: 'https://example.invalid/?key=not-public' }],
    ['URL executable', { ...row(), base_url: 'javascript:alert(1)' }],
    ['malformed Unicode', { ...row(), name: '\ud800' }],
  ])('rejects %s', async (_label, invalid) => {
    data = invalid
    await expect(getConnectionMetadata('prv_one', 'con_one')).rejects.toThrow('unavailable')
  })
  it.each(['no-store', 'public, no-store', 'private', 'private, no-store, public'])(
    'rejects privacy %s',
    async (control) => {
      headers.set('Cache-Control', control)
      await expect(getConnectionMetadata('prv_one', 'con_one')).rejects.toThrow()
    },
  )
  it.each([`W/"${token}"`, `"${'d'.repeat(64)}.${'e'.repeat(64)}"`, token])(
    'rejects incoherent response ETag %s',
    async (etag) => {
      headers.set('ETag', etag)
      await expect(getConnectionMetadata('prv_one', 'con_one')).rejects.toThrow()
    },
  )
})
it.each([
  { connection: { ...row(), name: 'Renamed' }, runtime_applied: false, changed: true },
  { connection: { ...row(), name: 'Wrong' }, runtime_applied: true, changed: true },
  {
    connection: { ...row(), name: 'Renamed', etag: `${'d'.repeat(64)}.${'b'.repeat(64)}` },
    runtime_applied: true,
    changed: true,
  },
  {
    connection: { ...row(), name: 'Renamed', can_edit: false },
    runtime_applied: true,
    changed: true,
  },
  { ...row(), name: 'Renamed' },
  {
    connection: { ...row(), name: 'Renamed' },
    runtime_applied: true,
    changed: true,
    receipt: 'invented',
  },
])('does not complete on an unconfirmed/mismatched result %#', async (invalid) => {
  data = invalid
  await expect(
    saveConnectionMetadata(
      'prv_one',
      'con_one',
      token,
      { name: 'Renamed', reason: 'Reason' },
      'csrf',
    ),
  ).rejects.toThrow()
})
it('validates exact Unicode name and byte-bounded reason without normalizing the captured input', async () => {
  expect(validConnectionName('好'.repeat(100))).toBe(true)
  expect(validConnectionName('好'.repeat(101))).toBe(false)
  expect(validConnectionReason('好'.repeat(341))).toBe(true)
  expect(validConnectionReason('好'.repeat(342))).toBe(false)
  for (const value of ['', ' padded ', 'line\nbreak', '\ud800']) {
    expect(validConnectionName(value)).toBe(false)
    expect(validConnectionReason(value)).toBe(false)
  }
  await expect(
    saveConnectionMetadata(
      'prv_one',
      'con_one',
      token,
      { name: ' Padded ', reason: 'Reason' },
      'csrf',
    ),
  ).rejects.toThrow()
  expect(requests).toHaveLength(0)
})

const malformedEnums = [
  ['protocol array', { protocol: ['openai_chat'] }],
  ['direct mode array', { egress_mode: ['direct'] }],
  ['default mode array', { egress_mode: ['default'] }],
  ['protocol object', { protocol: { value: 'openai_chat' } }],
  ['mode object', { egress_mode: { value: 'direct' } }],
  ['null protocol', { protocol: null }],
  ['numeric mode', { egress_mode: 0 }],
] as const
it.each(malformedEnums)(
  'rejects malformed GET %s rather than coercing authority facts',
  async (_label, patch) => {
    data = { ...row(), ...patch }
    await expect(getConnectionMetadata('prv_one', 'con_one')).rejects.toThrow('unavailable')
  },
)
it.each(malformedEnums)(
  'leaves dispatched PUT uncertain for malformed %s',
  async (_label, patch) => {
    const input = { name: 'Renamed', reason: 'Reviewed maintenance' }
    data = {
      connection: { ...row(), name: input.name, ...patch },
      runtime_applied: true,
      changed: true,
    }
    await expect(
      saveConnectionMetadata('prv_one', 'con_one', token, input, 'current-csrf'),
    ).rejects.toThrow('unavailable')
    expect(requests).toHaveLength(1)
    expect(JSON.parse(requests[0].data)).toEqual(input)
    expect(requests[0].headers.get('If-Match')).toBe(`"${token}"`)
  },
)
it.each(['\uFEFFPrimary', 'Primary\uFEFF', '\uFEFFPrimary\uFEFF'])(
  'preserves server-valid boundary format characters in GET and PUT: %s',
  async (name) => {
    expect(validConnectionName(name)).toBe(true)
    const reason = '\uFEFFReviewed maintenance\uFEFF'
    expect(validConnectionReason(reason)).toBe(true)
    data = { ...row(), name }
    expect((await getConnectionMetadata('prv_one', 'con_one')).name).toBe(name)
    data = { connection: { ...row(), name }, runtime_applied: true, changed: false }
    expect(
      (await saveConnectionMetadata('prv_one', 'con_one', token, { name, reason }, 'current-csrf'))
        .connection.name,
    ).toBe(name)
    expect(JSON.parse(requests[1].data)).toEqual({ name, reason })
  },
)
it.each([' ', '\u00a0', '\u1680', '\u2000', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000'])(
  'rejects Go boundary whitespace without rejecting internal spacing: %s',
  (space) => {
    for (const value of [`${space}Primary`, `Primary${space}`]) {
      expect(validConnectionName(value)).toBe(false)
      expect(validConnectionReason(value)).toBe(false)
    }
    expect(validConnectionName(`First${space}Second`)).toBe(true)
    expect(validConnectionReason(`First${space}Second`)).toBe(true)
  },
)
it('accepts canonical classic transport and keeps metadata writes name-only', async () => {
  data = {
    ...row(),
    adapter: 'azure_openai_classic',
    api_version: '2024-10-21',
    base_url: 'https://azure.example.invalid',
  }
  expect((await getConnectionMetadata('prv_one', 'con_one')).adapter).toBe('azure_openai_classic')
})
it.each([
  { adapter: undefined },
  { api_version: undefined },
  { adapter: 'native', api_version: '2024-10-21' },
  { adapter: 'azure_openai_classic', api_version: null },
  { adapter: 'azure_openai_classic', api_version: '2024-10-21', protocol: 'openai_responses' },
  {
    adapter: 'azure_openai_classic',
    api_version: '2024-10-21',
    base_url: 'https://azure.example.invalid/v1',
  },
])('rejects noncanonical immutable Connection transport', async (change) => {
  data = { ...row(), ...change }
  await expect(getConnectionMetadata('prv_one', 'con_one')).rejects.toThrow()
})
