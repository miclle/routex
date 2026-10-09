import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from './client'
import {
  decodeCredentialAttemptStatistics,
  getCredentialAttemptStatistics,
} from './credential-attempt-statistics'

const original = client.defaults.adapter
let requests: InternalAxiosRequestConfig[], value: ReturnType<typeof fixture>, control: string
function fixture() {
  return {
    provider_id: 'prv_exact',
    observed_at: '2026-10-09T01:02:03.123456789Z',
    attempt_limit: 100,
    recorded_only: true,
    items: [
      {
        credential_id: 'crd_A',
        connection_id: 'con_exact',
        inspected_attempts: 2,
        has_more: false,
        failure_streak: { state: 'exact', count: 0, lower_bound: 0 },
        recent_error: {
          state: 'recorded',
          code: 'upstream_timeout',
          completed_at: '2026-10-09T01:02:02Z',
        },
      },
    ],
  }
}
beforeEach(() => {
  requests = []
  value = fixture()
  control = 'private, no-store'
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ 'Cache-Control': control }),
      data: value,
    }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
const read = () =>
  getCredentialAttemptStatistics('prv_exact', [
    { credentialId: 'crd_A', connectionId: 'con_exact' },
  ])
describe('Credential inference-attempt statistics boundary', () => {
  it('uses one read-only exact-ID batch without write headers and preserves recent error after streak reset', async () => {
    const result = await read()
    expect(requests).toHaveLength(1)
    expect(requests[0].method).toBe('get')
    expect(requests[0].url).toBe('/admin/providers/prv_exact/credential-attempt-statistics')
    expect([...(requests[0].params as URLSearchParams)]).toEqual([['credential_id', 'crd_A']])
    expect(requests[0].data).toBeUndefined()
    expect(requests[0].headers.get('X-CSRF-Token')).toBeUndefined()
    expect(requests[0].headers.get('If-Match')).toBeUndefined()
    expect(result.items[0].failure_streak.count).toBe(0)
    expect(result.items[0].recent_error.code).toBe('upstream_timeout')
    expect(result.observed_at).toBe('2026-10-09T01:02:03.123456789Z')
  })
  it('keeps requested order and repeated query IDs exact across different Connections', async () => {
    value.items.push({
      ...structuredClone(value.items[0]),
      credential_id: 'crd_B',
      connection_id: 'con_other',
    })
    await getCredentialAttemptStatistics('prv_exact', [
      { credentialId: 'crd_A', connectionId: 'con_exact' },
      { credentialId: 'crd_B', connectionId: 'con_other' },
    ])
    expect([...(requests[0].params as URLSearchParams)]).toEqual([
      ['credential_id', 'crd_A'],
      ['credential_id', 'crd_B'],
    ])
  })
  it.each(['provider', 'credential', 'connection', 'order', 'missing', 'duplicate'])(
    'rejects a response with mismatched %s scope',
    async (kind) => {
      if (kind === 'order') {
        value.items.push({ ...structuredClone(value.items[0]), credential_id: 'crd_B' })
        value.items.reverse()
        await expect(
          getCredentialAttemptStatistics('prv_exact', [
            { credentialId: 'crd_A', connectionId: 'con_exact' },
            { credentialId: 'crd_B', connectionId: 'con_exact' },
          ]),
        ).rejects.toThrow('unavailable')
        return
      }
      if (kind === 'provider') value.provider_id = 'prv_other'
      else if (kind === 'credential') value.items[0].credential_id = 'crd_a'
      else if (kind === 'connection') value.items[0].connection_id = 'con_other'
      else if (kind === 'missing') value.items = []
      else value.items.push({ ...structuredClone(value.items[0]), credential_id: 'crd_A' })
      await expect(read()).rejects.toThrow('unavailable')
    },
  )
  it.each([0, 21])('rejects %s requested IDs before transport', async (count) => {
    await expect(
      getCredentialAttemptStatistics(
        'prv_exact',
        Array.from({ length: count }, (_, index) => ({
          credentialId: `crd_${index}`,
          connectionId: 'con_exact',
        })),
      ),
    ).rejects.toThrow('unavailable')
    expect(requests).toHaveLength(0)
  })
  it('rejects duplicate, empty-suffix IDs and control characters before transport', async () => {
    for (const targets of [
      [
        { credentialId: 'crd_A', connectionId: 'con_exact' },
        { credentialId: 'crd_A', connectionId: 'con_other' },
      ],
      [{ credentialId: 'crd_A\n', connectionId: 'con_exact' }],
      [{ credentialId: 'crd_', connectionId: 'con_exact' }],
    ])
      await expect(getCredentialAttemptStatistics('prv_exact', targets)).rejects.toThrow(
        'unavailable',
      )
    expect(requests).toHaveLength(0)
  })
  it.each(['public', 'private', 'private, no-store, public'])(
    'rejects %s cache policy',
    async (policy) => {
      control = policy
      await expect(read()).rejects.toThrow('unavailable')
    },
  )
  it.each([
    'offset',
    'calendar',
    'future',
    'futureNano',
    'secret',
    'extra',
    'unbounded',
    'falseCoverage',
    'exactOverflow',
    'badNoRecords',
  ])('rejects malformed or misleading %s facts', (kind) => {
    const row = value.items[0]
    if (kind === 'offset') value.observed_at = '2026-10-09T09:02:03+08:00'
    else if (kind === 'calendar') value.observed_at = '2026-02-30T01:02:03Z'
    else if (kind === 'future') row.recent_error.completed_at = '2026-10-10T01:02:03Z'
    else if (kind === 'futureNano') row.recent_error.completed_at = '2026-10-09T01:02:03.123456790Z'
    else if (kind === 'secret') row.recent_error.code = 'Bearer private-upstream-detail'
    else if (kind === 'extra') Object.assign(row, { ciphertext: 'private' })
    else if (kind === 'unbounded') row.inspected_attempts = 101
    else if (kind === 'falseCoverage') value.recorded_only = false
    else if (kind === 'exactOverflow')
      Object.assign(row, {
        inspected_attempts: 100,
        has_more: true,
        failure_streak: { state: 'exact', count: 100, lower_bound: 100 },
      })
    else
      Object.assign(row, {
        inspected_attempts: 0,
        failure_streak: { state: 'no_records', count: 0, lower_bound: 0 },
      })
    expect(() => decodeCredentialAttemptStatistics(value)).toThrow('unavailable')
  })
  it('retains exact UTC nanosecond ordering without rejecting equivalent fractions', () => {
    for (const completedAt of [
      '2026-10-09T01:02:03.123456788Z',
      '2026-10-09T01:02:03.123456789Z',
    ]) {
      value.items[0].recent_error.completed_at = completedAt
      expect(decodeCredentialAttemptStatistics(value).items[0].recent_error.completed_at).toBe(
        completedAt,
      )
    }
    value.observed_at = '2026-10-09T01:02:03.1234Z'
    value.items[0].recent_error.completed_at = '2026-10-09T01:02:03.123400000Z'
    expect(decodeCredentialAttemptStatistics(value).items[0].recent_error.state).toBe('recorded')
  })
  it('preserves no records, clipped lower bound and unknown boundary as distinct states', () => {
    const row = value.items[0]
    Object.assign(row, {
      inspected_attempts: 0,
      has_more: false,
      failure_streak: { state: 'no_records', count: null, lower_bound: 0 },
      recent_error: { state: 'no_records', code: null, completed_at: null },
    })
    expect(decodeCredentialAttemptStatistics(value).items[0].failure_streak.count).toBeNull()
    Object.assign(row, {
      inspected_attempts: 100,
      has_more: true,
      failure_streak: { state: 'lower_bound', count: null, lower_bound: 100 },
      recent_error: { state: 'recorded', code: null, completed_at: '2026-10-09T01:02:02Z' },
    })
    expect(decodeCredentialAttemptStatistics(value).items[0].failure_streak.state).toBe(
      'lower_bound',
    )
    Object.assign(row, {
      inspected_attempts: 3,
      has_more: false,
      failure_streak: { state: 'unknown', count: null, lower_bound: 2 },
      recent_error: { state: 'unknown', code: null, completed_at: null },
    })
    expect(decodeCredentialAttemptStatistics(value).items[0].failure_streak.lower_bound).toBe(2)
  })
  it('rejects an ignored-abort reply', async () => {
    let release!: () => void
    const hold = new Promise<void>((resolve) => {
      release = resolve
    })
    client.defaults.adapter = async (config) => {
      await hold
      return {
        config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders({ 'Cache-Control': control }),
        data: value,
      }
    }
    const controller = new AbortController()
    const pending = getCredentialAttemptStatistics(
      'prv_exact',
      [{ credentialId: 'crd_A', connectionId: 'con_exact' }],
      controller.signal,
    )
    controller.abort()
    release()
    await expect(pending).rejects.toBeDefined()
  })
})
