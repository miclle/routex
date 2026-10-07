import { afterEach, beforeEach, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  decodeDeploymentCoverage,
  getDeploymentCoverage,
  saveDeploymentCoverage,
} from './deployment-coverage'
import {
  decodeConnectionTransport,
  validAzureAPIVersion,
  validAzureOrigin,
} from './connection-transport'
const crd = 'crd_01arz3ndektsv4rrffq69g5fav',
  con = 'con_01arz3ndektsv4rrffq69g5fav',
  pmd = 'pmd_01arz3ndektsv4rrffq69g5fav'
const tag = 'a'.repeat(64) + '.' + 'b'.repeat(64)
const row = () => ({
  credential_id: crd,
  connection_id: con,
  adapter: 'azure_openai_classic',
  api_version: '2024-10-21',
  verification_status: 'verified',
  verified_at: '2026-10-07T00:00:00Z',
  coverage_source: 'administrator_attestation',
  provider_models: [{ id: pmd, upstream_name: 'deployment-A', can_attest: true, attested: false }],
  etag: tag,
  can_edit: true,
})
const original = client.defaults.adapter
let data: unknown, requests: InternalAxiosRequestConfig[], headers: AxiosHeaders
beforeEach(() => {
  data = row()
  requests = []
  headers = new AxiosHeaders({ 'Cache-Control': 'private, no-store', ETag: `"${tag}"` })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { config, data, headers, status: 200, statusText: '' }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
it('reads only the exact credential and preserves independent read-only editability', async () => {
  data = { ...row(), can_edit: false }
  expect((await getDeploymentCoverage(crd, con)).can_edit).toBe(false)
  expect(requests.map((r) => r.url)).toEqual([`/admin/credentials/${crd}/deployment-coverage`])
})
it.each([
  ['extra source material', { ...row(), secret: 'not-public' }],
  ['native adapter', { ...row(), adapter: 'native' }],
  ['invalid date', { ...row(), api_version: '2025-02-29' }],
  ['aliased credential', { ...row(), credential_id: crd.toUpperCase() }],
  ['another connection', { ...row(), connection_id: con.replace('fav', 'fax') }],
  ['short proof', { ...row(), etag: 'b'.repeat(64) }],
  ['missing editability', { ...row(), can_edit: undefined }],
  ['bad status type', { ...row(), verification_status: ['verified'] }],
  ['unknown status', { ...row(), verification_status: 'ready' }],
  [
    'duplicate models',
    { ...row(), provider_models: [row().provider_models[0], row().provider_models[0]] },
  ],
  ['overflow', { ...row(), provider_models: Array(2001).fill(row().provider_models[0]) }],
  [
    'false capability',
    { ...row(), provider_models: [{ ...row().provider_models[0], can_attest: false }] },
  ],
  [
    'unsafe deployment attested',
    {
      ...row(),
      provider_models: [
        {
          ...row().provider_models[0],
          upstream_name: 'bad/path',
          can_attest: false,
          attested: true,
        },
      ],
    },
  ],
  [
    'model alias',
    { ...row(), provider_models: [{ ...row().provider_models[0], id: pmd.toUpperCase() }] },
  ],
  [
    'extra model field',
    { ...row(), provider_models: [{ ...row().provider_models[0], enabled: true }] },
  ],
])('rejects %s', async (_, value) => {
  data = value
  await expect(getDeploymentCoverage(crd, con)).rejects.toThrow('unavailable')
})
it('allows a recorded unsupported deployment as unreviewable rather than inventing coverage', () => {
  expect(
    decodeDeploymentCoverage({
      ...row(),
      provider_models: [
        { ...row().provider_models[0], upstream_name: 'bad/path', can_attest: false },
      ],
    }).provider_models[0].can_attest,
  ).toBe(false)
})
it.each(['W/"' + tag + '"', tag, '"' + 'c'.repeat(64) + '.' + 'b'.repeat(64) + '"'])(
  'rejects incoherent HTTP proof %s',
  async (etag) => {
    headers.set('ETag', etag)
    await expect(getDeploymentCoverage(crd, con)).rejects.toThrow()
  },
)
it.each(['public, no-store', 'private', 'private, no-store, max-age=0'])(
  'rejects private-cache contract mismatch %s',
  async (control) => {
    headers.set('Cache-Control', control)
    await expect(getDeploymentCoverage(crd, con)).rejects.toThrow()
  },
)
it('submits one complete set with original quoted proof, reason, current CSRF and abort signal', async () => {
  const saved = {
    ...row(),
    etag: 'a'.repeat(64) + '.' + 'c'.repeat(64),
    provider_models: [{ ...row().provider_models[0], attested: true }],
  }
  data = { coverage: saved, runtime_applied: true, changed: false }
  headers.set('ETag', `"${saved.etag}"`)
  const signal = new AbortController().signal
  const input = { provider_model_ids: [pmd], reason: 'Reviewed deployment' }
  expect((await saveDeploymentCoverage(crd, con, tag, input, 'fresh', signal)).changed).toBe(false)
  expect(requests).toHaveLength(1)
  expect(JSON.parse(requests[0].data)).toEqual(input)
  expect(requests[0].headers.get('If-Match')).toBe(`"${tag}"`)
  expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh')
  expect(requests[0].signal).toBe(signal)
})
it.each([
  { provider_model_ids: [pmd, pmd], reason: 'Valid' },
  { provider_model_ids: [], reason: ' ' },
  { provider_model_ids: [], reason: 'é'.repeat(513) },
  { provider_model_ids: [], reason: 'Valid', request_id: 'not-supported' },
  { provider_model_ids: [pmd.toUpperCase()], reason: 'Valid' },
])('rejects invalid full replacement before HTTP', async (input) => {
  await expect(saveDeploymentCoverage(crd, con, tag, input, 'fresh')).rejects.toThrow()
  expect(requests).toHaveLength(0)
})
it.each([
  { runtime_applied: false },
  { changed: 'yes' },
  { extra: true },
  { coverage: { ...row(), can_edit: false } },
  { coverage: { ...row(), provider_models: [{ ...row().provider_models[0], attested: true }] } },
])('rejects success without exact authorized current requested state', async (bad) => {
  data = { coverage: row(), runtime_applied: true, changed: true, ...bad }
  await expect(
    saveDeploymentCoverage(crd, con, tag, { provider_model_ids: [], reason: 'Revoke' }, 'fresh'),
  ).rejects.toThrow()
})
it.each(['2024-02-29', '2024-10-21-preview'])('accepts explicit real date %s', (v) =>
  expect(validAzureAPIVersion(v)).toBe(true),
)
it.each(['2023-02-29', '2024-13-01', '2024-01-01-preview-extra', ' 2024-10-21', 'latest'])(
  'rejects unsupported date %s',
  (v) => expect(validAzureAPIVersion(v)).toBe(false),
)
it('keeps canonical native transport and rejects native version or incompatible Azure protocol/origin', () => {
  expect(
    decodeConnectionTransport({
      adapter: 'native',
      api_version: null,
      protocol: 'openai_responses',
      base_url: 'https://example.invalid/v1',
    }),
  ).toEqual({ adapter: 'native', api_version: null })
  for (const change of [
    { protocol: 'messages' },
    { base_url: 'https://example.invalid/v1' },
    { base_url: 'https://name:secret@example.invalid' },
    { base_url: 'https://example.invalid/?a=b' },
    { adapter: 'native' },
  ])
    expect(() =>
      decodeConnectionTransport({
        adapter: 'azure_openai_classic',
        api_version: '2024-10-21',
        protocol: 'openai_chat',
        base_url: 'https://example.invalid/',
        ...change,
      }),
    ).toThrow()
})

it.each([
  'https://azure.example.invalid',
  'https://azure.example.invalid/',
  'https://azure.example.invalid:8443/',
  'http://127.0.0.1:9000/',
  'http://[::1]:9000',
])('accepts raw origin and optional single slash %s', (origin) => {
  expect(validAzureOrigin(origin)).toBe(true)
  expect(
    decodeConnectionTransport({
      adapter: 'azure_openai_classic',
      api_version: '2024-10-21',
      protocol: 'openai_chat',
      base_url: origin,
    }).adapter,
  ).toBe('azure_openai_classic')
})
it.each([
  'https://azure.example.invalid/a/..',
  'https://azure.example.invalid/./',
  'https://azure.example.invalid//',
  'https://azure.example.invalid/%2e/',
  'https://azure.example.invalid/a%2f..',
  'https://azure.example.invalid/?',
  'https://azure.example.invalid/#',
  ' https://azure.example.invalid',
  'https://azure.example.invalid ',
  'https://azure.example.invalid\\',
  'https://azure.example.invalid\t/',
  'https://azure.example.invalid\r\n/',
  'https://azure.example.invalid/\u0000',
  'https://azure.example.invalid/\u0085',
  'https://azure.example.invalid/\u00a0',
])('rejects raw path/control/whitespace without browser normalization %s', (origin) => {
  expect(validAzureOrigin(origin)).toBe(false)
  expect(() =>
    decodeConnectionTransport({
      adapter: 'azure_openai_classic',
      api_version: '2024-10-21',
      protocol: 'openai_chat',
      base_url: origin,
    }),
  ).toThrow()
})
