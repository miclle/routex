import { afterEach, expect, it } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from './client'
import {
  getCredentialStorageContext,
  getProviderStoragePolicy,
  parseCredentialStorageContext,
  parseProviderStoragePolicy,
  saveProviderStoragePolicy,
} from './provider-storage'
const etag = 'a'.repeat(64)
const id = 'vlt_01k0000000000000000000000a'
const revision = 'vlr_01k0000000000000000000000b'
const choice = {
  id,
  revision_id: revision,
  name: 'Recorded Integration',
  birth: '2026-09-23T00:00:00Z',
}
const policy = () => ({
  mode: 'inline',
  integration_id: null,
  revision_id: null,
  choices: [choice],
  etag,
  can_edit: true,
})
const original = client.defaults.adapter
const quoted = `"${etag}"`
afterEach(() => {
  client.defaults.adapter = original
})
it('keeps configured source and exact eligible saved revision distinct; preserves FEFF names', () => {
  const value = {
    ...policy(),
    mode: 'vault',
    integration_id: id,
    revision_id: revision,
    choices: [],
  }
  expect(parseProviderStoragePolicy(value, quoted)).toEqual(value)
  expect(
    parseProviderStoragePolicy(
      { ...policy(), choices: [{ ...choice, name: '\ufeffsaved\ufeff' }] },
      quoted,
    ).choices[0].name,
  ).toBe('\ufeffsaved\ufeff')
})
it.each([
  ['wrong source', { ...policy(), mode: 'external' }],
  ['inline refs', { ...policy(), integration_id: id }],
  ['missing Vault refs', { ...policy(), mode: 'vault' }],
  ['duplicate choice', { ...policy(), choices: [choice, choice] }],
  ['unknown field', { ...policy(), available: true }],
  ['unknown choice field', { ...policy(), choices: [{ ...choice, enabled: true }] }],
  ['alias ID', { ...policy(), choices: [{ ...choice, id: id.toUpperCase() }] }],
  ['invalid revision', { ...policy(), choices: [{ ...choice, revision_id: id }] }],
  ['blank label', { ...policy(), choices: [{ ...choice, name: ' saved' }] }],
  ['control label', { ...policy(), choices: [{ ...choice, name: 'saved\n' }] }],
  ['bad birth', { ...policy(), choices: [{ ...choice, birth: '0001-01-01T00:00:00Z' }] }],
  ['wrong editability', { ...policy(), can_edit: null }],
  ['incomplete choices', { ...policy(), choices: null }],
])('rejects %s', (_, value) => expect(() => parseProviderStoragePolicy(value, quoted)).toThrow())
it.each([undefined, etag, `W/${quoted}`, `"${'b'.repeat(64)}"`])(
  'rejects nonmatching strong ETag %s',
  (header) => expect(() => parseProviderStoragePolicy(policy(), header)).toThrow(),
)
it('rejects overflow rather than truncating an eligible catalogue', () => {
  expect(() =>
    parseProviderStoragePolicy({ ...policy(), choices: Array(101).fill(choice) }, quoted),
  ).toThrow()
})
it('creator context exposes only source and reviewed opaque policy token', () => {
  expect(parseCredentialStorageContext({ storage_source: 'vault', etag }, quoted)).toEqual({
    storage_source: 'vault',
    etag,
  })
  for (const value of [
    { storage_source: 'vault', etag, integration_id: id },
    { storage_source: 'unknown', etag },
    { storage_source: 'inline' },
  ])
    expect(() => parseCredentialStorageContext(value, quoted)).toThrow()
})
it('uses separate authorized routes and exact policy body/If-Match/CSRF without runtime claims', async () => {
  const requests: { path: string; body?: unknown; headers: AxiosHeaders }[] = []
  client.defaults.adapter = async (config) => {
    requests.push({
      path: config.url!,
      body: config.data ? JSON.parse(config.data) : undefined,
      headers: config.headers,
    })
    return {
      status: 200,
      statusText: '',
      config,
      headers: { etag: quoted },
      data:
        config.url === '/admin/provider-credential-storage-context'
          ? { storage_source: 'inline', etag }
          : policy(),
    }
  }
  await getCredentialStorageContext()
  await getProviderStoragePolicy()
  const body = {
    mode: 'inline' as const,
    integration_id: null,
    revision_id: null,
    reason: 'future writes',
  }
  const result = await saveProviderStoragePolicy(body, etag, 'csrf-current')
  expect(requests.map((item) => item.path)).toEqual([
    '/admin/provider-credential-storage-context',
    '/admin/secrets/provider-storage',
    '/admin/secrets/provider-storage',
  ])
  expect(requests[2].body).toEqual(body)
  expect(requests[2].headers.get('If-Match')).toBe(quoted)
  expect(requests[2].headers.get('X-CSRF-Token')).toBe('csrf-current')
  expect(result).not.toHaveProperty('runtime_applied')
})
