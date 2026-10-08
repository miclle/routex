import { AxiosError, AxiosHeaders } from 'axios'
import { afterEach, beforeEach, expect, it } from 'vitest'
import client from './client'
import {
  createModelAccess,
  listModelAccessProviders,
  ModelAccessError,
  readModelAccessEgress,
  readModelAccessCredential,
  validateModelAccessInput,
  verifyModelAccess,
} from './model-access'
import type { ModelAccessInput, ModelAccessSaved } from '@/types/model-access'
const original = client.defaults.adapter
const input: ModelAccessInput = {
  request_id: '11111111-1111-4111-8111-111111111111',
  storage_policy_etag: 'a'.repeat(64),
  name: 'Provider',
  connection_name: 'Connection',
  credential_name: 'Credential',
  secret: ' transient-test-secret ',
  base_url: 'https://example.invalid/v1',
  protocol: 'openai_chat',
  adapter: 'native',
  api_version: null,
  egress_mode: 'default',
  egress_id: null,
}
let data: unknown, status: number
export const bootstrap = {
  id: 'prv_one',
  name: 'Provider',
  connections: [
    {
      id: 'con_one',
      name: 'Connection',
      enabled: true,
      base_url: input.base_url,
      protocol: 'openai_chat',
      adapter: 'native',
      api_version: null,
      egress_mode: 'default',
      egress_id: null,
      etag: '0',
      provider_models: [],
      credentials: [
        {
          id: 'crd_one',
          name: 'Credential',
          priority: 0,
          enabled: false,
          verification_status: 'pending',
          verified_at: null,
          storage_source: 'inline',
          replaces_credential_id: null,
        },
      ],
    },
  ],
}
beforeEach(() => {
  status = 201
  data = structuredClone(bootstrap)
  client.defaults.adapter = async (config) => {
    const response = { config, status, statusText: '', headers: new AxiosHeaders(), data }
    if (status >= 400) throw new AxiosError('transport rejected', '', config, undefined, response)
    return response
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
it('projects exact nonsecret bootstrap identities and actual enabled defaults', async () => {
  const saved = await createModelAccess(input, 'inline', 'csrf')
  expect(saved).toMatchObject({
    provider_id: 'prv_one',
    connection_id: 'con_one',
    credential_id: 'crd_one',
    connection_enabled: true,
  })
  expect(JSON.stringify(saved)).not.toContain(input.secret)
  expect(Object.keys(saved)).not.toContain('credentials')
})
it.each([
  [
    'wrong protocol',
    (v: typeof bootstrap) => {
      v.connections[0].protocol = 'openai_responses'
    },
  ],
  [
    'wrong source',
    (v: typeof bootstrap) => {
      v.connections[0].credentials[0].storage_source = 'vault'
    },
  ],
  [
    'duplicate bootstrap',
    (v: typeof bootstrap) => {
      v.connections.push(v.connections[0])
    },
  ],
  [
    'unacknowledged model',
    (v: typeof bootstrap) => {
      ;(v.connections[0].provider_models as unknown[]).push({ id: 'pmd_one' })
    },
  ],
])('rejects %s as an unknown creation acknowledgement', async (_, change) => {
  const value = structuredClone(bootstrap)
  change(value)
  data = value
  await expect(createModelAccess(input, 'inline', 'csrf')).rejects.toMatchObject({ status: 0 })
})
it('sanitizes credential-bearing Axios config on an actual failed dispatch', async () => {
  status = 503
  const error = await createModelAccess(input, 'inline', 'csrf').catch((error) => error)
  expect(error).toBeInstanceOf(ModelAccessError)
  expect(error.status).toBe(503)
  expect(error.config).toBeUndefined()
  expect(JSON.stringify(error)).not.toContain(input.secret)
})
it.each([
  { ...input, request_id: '11111111-1111-3111-8111-111111111111' },
  { ...input, secret: 'bad\nkey' },
  { ...input, secret: '\ud800' },
  { ...input, egress_mode: 'proxy' as const, egress_id: null },
  { ...input, adapter: 'azure_openai_classic' as const, api_version: '2026-02-30' },
])('rejects invalid intent before transport', (value) => {
  expect(() => validateModelAccessInput(value)).toThrow()
})
it('rejects duplicate/oversized paged Provider replies rather than an available directory claim', async () => {
  status = 200
  data = {
    items: [
      { id: 'prv_one', name: 'One' },
      { id: 'prv_one', name: 'One' },
    ],
    next_cursor: null,
  }
  await expect(listModelAccessProviders({})).rejects.toBeInstanceOf(ModelAccessError)
  data = {
    items: [
      { id: 'prv_one', name: 'One' },
      { id: 'prv_two', name: 'Two' },
    ],
    next_cursor: null,
  }
  await expect(listModelAccessProviders({ limit: 1 })).rejects.toBeInstanceOf(ModelAccessError)
})
it('uses bounded minimal egress projection and rejects transport-bearing rows', async () => {
  status = 200
  data = { items: [{ id: 'egr_one', name: 'One', enabled: true }], next_cursor: null }
  expect((await readModelAccessEgress({ limit: 1 })).items).toHaveLength(1)
  data = {
    items: [{ id: 'egr_one', name: 'One', enabled: true, host: 'private.invalid' }],
    next_cursor: null,
  }
  await expect(readModelAccessEgress({ limit: 1 })).rejects.toBeInstanceOf(ModelAccessError)
})
it('does not turn HTTP200 failed verification into success', async () => {
  status = 200
  data = { verified: false, discovered_models: 0, message: 'sanitized server message' }
  expect(
    await verifyModelAccess(
      { credential_id: 'crd_one', connection_id: 'con_one' } as ModelAccessSaved,
      'csrf',
    ),
  ).toBe(false)
})

it.each([{ value: ['verified'] }, { value: { status: 'verified' } }])(
  'rejects a non-string bootstrap verification status rather than confirmed access: %j',
  async ({ value: verificationStatus }) => {
    const value = structuredClone(bootstrap)
    ;(
      value.connections[0].credentials[0] as unknown as Record<string, unknown>
    ).verification_status = verificationStatus
    data = value
    await expect(createModelAccess(input, 'inline', 'csrf')).rejects.toMatchObject({ status: 0 })
  },
)
it.each([{ value: ['verified'] }, { value: { status: 'verified' } }])(
  'rejects a non-string recorded verification status rather than enablement facts: %j',
  async ({ value: verificationStatus }) => {
    status = 200
    data = {
      id: 'crd_one',
      connection_id: 'con_one',
      enabled: false,
      verification_status: verificationStatus,
      verified_at: '2026-10-08T00:00:00Z',
      etag: 'b'.repeat(64),
    }
    await expect(
      readModelAccessCredential({
        credential_id: 'crd_one',
        connection_id: 'con_one',
      } as ModelAccessSaved),
    ).rejects.toMatchObject({ status: 0 })
  },
)

it('omits empty UI search values from strict picker HTTP parameters', async () => {
  const params: unknown[] = []
  client.defaults.adapter = async (config) => {
    params.push(config.params)
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: { items: [], next_cursor: null },
    }
  }
  await listModelAccessProviders({ q: '' })
  await readModelAccessEgress({ q: '' })
  expect(params).toEqual([{}, {}])
})
it.each(['Provider', 'egress'])(
  'fails closed on a name-search alias or cursor in an exact %s identity reply',
  async (kind) => {
    status = 200
    data = {
      items:
        kind === 'Provider'
          ? [{ id: 'prv_other', name: 'prv_one name alias' }]
          : [{ id: 'egr_other', name: 'egr_one name alias', enabled: true }],
      next_cursor: null,
    }
    const read = () =>
      kind === 'Provider'
        ? listModelAccessProviders({ exact_id: 'prv_one' })
        : readModelAccessEgress({ exact_id: 'egr_one' })
    await expect(read()).rejects.toBeInstanceOf(ModelAccessError)
    data = {
      items:
        kind === 'Provider'
          ? [{ id: 'prv_one', name: 'One' }]
          : [{ id: 'egr_one', name: 'One', enabled: true }],
      next_cursor: kind === 'Provider' ? 'prv_one' : 'egr_one',
    }
    await expect(read()).rejects.toBeInstanceOf(ModelAccessError)
  },
)
it.each([
  { exact_id: '' },
  { exact_id: 'con_other' },
  { exact_id: 'PRV_one' },
  { exact_id: 'prv_one\n' },
  { exact_id: 'prv_one', q: 'One' },
  { exact_id: 'prv_one', cursor: 'prv_other' },
])(
  'rejects ambiguous or noncanonical exact Provider filters before transport: %j',
  async (filter) => {
    let transports = 0
    client.defaults.adapter = async (config) => {
      transports++
      return {
        config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders(),
        data: { items: [], next_cursor: null },
      }
    }
    await expect(listModelAccessProviders(filter)).rejects.toBeInstanceOf(ModelAccessError)
    expect(transports).toBe(0)
  },
)
