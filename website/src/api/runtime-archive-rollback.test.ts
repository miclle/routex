import { AxiosError, AxiosHeaders } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from './client'
import {
  applyRuntimeArchiveRollback,
  getRuntimeArchiveRollbackReceipt,
  previewRuntimeArchiveRollback,
  RuntimeArchiveRollbackError,
} from './runtime-archive-rollback'

const adapter = client.defaults.adapter
const id = 'rcv_00000000000000000000000002'
const requestID = '00000000-0000-4000-8000-000000000001'
const date = '2026-10-14T09:01:00.123456Z'
const digest = 'a'.repeat(64)
const input = { request_id: requestID, reason: 'Restore reviewed routing' }
const preview = {
  scope: 'gateway_routing_configuration',
  archive_id: id,
  archive_created_at: date,
  observed_at: date,
  can_apply: true,
  blocker_codes: [],
  connections: [],
  declarations: [],
  weights: [],
}
const saved = {
  scope: preview.scope,
  request_id: requestID,
  archive_id: id,
  archive_created_at: date,
  applied_at: date,
  changed: true,
  reason: input.reason,
  publication: {
    request_id: requestID,
    actor_id: 'usr_00000000000000000000000001',
    reason: input.reason,
    claim_instance_id: 'instance-test',
    claim_instance_started_at: date,
    claimed_at: date,
    finalized_at: null,
    status: 'unknown',
    failure_code: null,
  },
  runtime_applied: false,
  superseded: false,
}
let body: unknown, etag: string, calls: number
beforeEach(() => {
  body = structuredClone(preview)
  etag = `"${digest}"`
  calls = 0
  client.defaults.adapter = async (config) => {
    calls++
    return {
      data: body,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders({ etag }),
      config,
    }
  }
})
afterEach(() => {
  vi.restoreAllMocks()
  client.defaults.adapter = adapter
})

describe('archive rollback transient wire boundary', () => {
  it('reads the exact selected archive and returns its reviewed strong token', async () => {
    const signal = new AbortController().signal
    client.defaults.adapter = async (config) => {
      expect(config.url).toBe(`/admin/runtime/archives/${id}/rollback`)
      expect(config.method).toBe('get')
      expect(config.signal).toBe(signal)
      expect(config.headers.has('X-CSRF-Token')).toBe(false)
      return { data: body, status: 200, statusText: 'OK', headers: { etag }, config }
    }
    expect(await previewRuntimeArchiveRollback(id, signal)).toEqual({ ...preview, etag: digest })
  })
  it.each(['W/"' + digest + '"', digest, '"' + digest.toUpperCase() + '"', '"' + digest + '"\n'])(
    'rejects an unusable review ETag %j',
    async (value) => {
      etag = value
      // Exercise the decoder's input, including text Axios may normalize before
      // an HTTP adapter response reaches this boundary.
      vi.spyOn(client, 'get').mockResolvedValue({
        data: body,
        status: 200,
        headers: { etag },
      })
      await expect(previewRuntimeArchiveRollback(id)).rejects.toBeInstanceOf(
        RuntimeArchiveRollbackError,
      )
    },
  )
  it.each([
    { archive_id: 'rcv_00000000000000000000000003' },
    { archive_created_at: '2026-02-30T09:01:00Z' },
    { scope: 'all_configuration' },
    { can_apply: true, blocker_codes: ['changed_transport_enabled'] },
    { auth_ciphertext: 'private' },
    {
      declarations: [
        {
          provider_model_id: 'pm_test',
          before: { image_input: false, pdf_input: false },
          after: { image_input: true, pdf_input: true },
          prices: [],
        },
      ],
    },
  ])('rejects crossed identities, inconsistent eligibility or stronger data %j', async (change) => {
    body = { ...preview, ...change }
    await expect(previewRuntimeArchiveRollback(id)).rejects.toBeInstanceOf(
      RuntimeArchiveRollbackError,
    )
  })
  it('retains localized unknown blocker codes without inventing eligibility', async () => {
    body = { ...preview, can_apply: false, blocker_codes: ['future_guard'] }
    expect((await previewRuntimeArchiveRollback(id)).blocker_codes).toEqual(['future_guard'])
  })
  it('rejects protocol restoration even for a disabled connection', async () => {
    const fields = {
      base_url: 'https://api.example.com',
      protocol: 'openai_chat',
      adapter: 'native',
      api_version: null,
      egress_mode: 'direct',
      egress_id: null,
    }
    body = {
      ...preview,
      connections: [
        {
          connection_id: 'con_test',
          name: 'Connection',
          enabled: false,
          requires_verification: true,
          before: fields,
          after: { ...fields, protocol: 'openai_responses' },
        },
      ],
    }
    await expect(previewRuntimeArchiveRollback(id)).rejects.toBeInstanceOf(
      RuntimeArchiveRollbackError,
    )
  })
  it('rejects a coerced egress mode instead of treating an array as direct', async () => {
    const fields = {
      base_url: 'https://api.example.com',
      protocol: 'openai_chat',
      adapter: 'native',
      api_version: null,
      egress_mode: ['direct'],
      egress_id: null,
    }
    body = {
      ...preview,
      connections: [
        {
          connection_id: 'con_test',
          name: 'Connection',
          enabled: false,
          requires_verification: true,
          before: fields,
          after: fields,
        },
      ],
    }
    await expect(previewRuntimeArchiveRollback(id)).rejects.toBeInstanceOf(
      RuntimeArchiveRollbackError,
    )
  })
  it('preserves the supported legacy empty default mode from the server', async () => {
    const fields = {
      base_url: 'https://api.example.com',
      protocol: 'openai_chat',
      adapter: 'native',
      api_version: null,
      egress_mode: '',
      egress_id: null,
    }
    body = {
      ...preview,
      connections: [
        {
          connection_id: 'con_test',
          name: 'Connection',
          enabled: false,
          requires_verification: true,
          before: fields,
          after: fields,
        },
      ],
    }
    const result = await previewRuntimeArchiveRollback(id)
    expect(result.connections[0].before.egress_mode).toBe('')
    expect(result.connections[0].after.egress_mode).toBe('')
  })
  it('dispatches identical reviewed intent and headers on explicit retry', async () => {
    body = structuredClone(saved)
    const signal = new AbortController().signal
    client.defaults.adapter = async (config) => {
      calls++
      expect(config.url).toBe(`/admin/runtime/archives/${id}/rollback`)
      expect(config.method).toBe('post')
      expect(config.data).toBe(JSON.stringify(input))
      expect(config.headers.get('If-Match')).toBe(`"${digest}"`)
      expect(config.headers.get('X-CSRF-Token')).toBe('current-csrf')
      expect(config.signal).toBe(signal)
      return { data: body, status: 200, statusText: 'OK', headers: {}, config }
    }
    expect(await applyRuntimeArchiveRollback(id, input, digest, 'current-csrf', signal)).toEqual(
      saved,
    )
    expect(await applyRuntimeArchiveRollback(id, input, digest, 'current-csrf', signal)).toEqual(
      saved,
    )
    expect(calls).toBe(2)
  })
  it.each(['unknown', 'pending', 'refresh_returned_nil', 'refresh_returned_error'])(
    'preserves original publication %s independently of fresh runtime proof',
    async (status) => {
      const terminal = status.startsWith('refresh_')
      body = {
        ...saved,
        runtime_applied: true,
        publication: {
          ...saved.publication,
          status,
          finalized_at: terminal ? date : null,
          failure_code: status === 'refresh_returned_error' ? 'runtime_refresh_failed' : null,
        },
      }
      const result = await getRuntimeArchiveRollbackReceipt(id, requestID)
      expect(result.publication.status).toBe(status)
      expect(result.runtime_applied).toBe(true)
    },
  )
  it.each([
    { archive_id: 'rcv_00000000000000000000000003' },
    { request_id: '00000000-0000-4000-8000-000000000002' },
    { publication: null },
    { publication: { ...saved.publication, status: 'pending', finalized_at: date } },
    { publication: { ...saved.publication, status: 'fleet_applied' } },
    { publication: { ...saved.publication, status: ['refresh_returned_nil'] } },
    { runtime_applied: true, superseded: true },
  ])('rejects mismatched or contradictory receipt %j', async (change) => {
    body = { ...saved, ...change }
    await expect(getRuntimeArchiveRollbackReceipt(id, requestID)).rejects.toBeInstanceOf(
      RuntimeArchiveRollbackError,
    )
  })
  it('does not reconcile a different original reason as this write', async () => {
    body = {
      ...saved,
      reason: 'Another intent',
      publication: { ...saved.publication, reason: 'Another intent' },
    }
    await expect(applyRuntimeArchiveRollback(id, input, digest, 'csrf')).rejects.toBeInstanceOf(
      RuntimeArchiveRollbackError,
    )
  })
  it('rejects invalid identity and reason before any HTTP request', async () => {
    await expect(previewRuntimeArchiveRollback(id + '\n')).rejects.toThrow()
    for (const reason of ['', ' padded', 'line\nbreak', '\ud800', 'x'.repeat(1001)]) {
      await expect(
        applyRuntimeArchiveRollback(id, { ...input, reason }, digest, 'csrf'),
      ).rejects.toThrow()
    }
    expect(calls).toBe(0)
  })
  it('sanitizes transport failures without retaining request or response content', async () => {
    client.defaults.adapter = async (config) => {
      throw new AxiosError('private payload', 'ERR_BAD_RESPONSE', config, undefined, {
        data: { private: 'value' },
        status: 503,
        statusText: 'Error',
        headers: {},
        config,
      })
    }
    try {
      await applyRuntimeArchiveRollback(id, input, digest, 'csrf')
      expect.unreachable()
    } catch (error) {
      expect(error).toBeInstanceOf(RuntimeArchiveRollbackError)
      expect(error).toMatchObject({ status: 503 })
      expect(JSON.stringify(error)).not.toMatch(/private|csrf|reason|response|config/)
    }
  })
})
