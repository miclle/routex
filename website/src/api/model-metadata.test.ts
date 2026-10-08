import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { getAdminModel, listAdminModels } from './catalog'
import {
  getModelMonthlyRequests,
  validateModelMonthlyRequests,
  validateModelRecordedMetadata,
} from './model-metadata'
const adapter = client.defaults.adapter
let value: unknown, calls: InternalAxiosRequestConfig[]
function page() {
  return {
    items: [
      { model_id: 'mdl_one', requests: '0' },
      { model_id: 'mdl_two', requests: '10000' },
    ],
    period_from: '2026-10-01T00:00:00Z',
    period_to: '2026-10-08T01:02:03.123456789Z',
    as_of: '2026-10-08T01:02:03.123456789Z',
    timezone: 'UTC',
    source: 'persisted_call_records',
    may_lag: true,
  }
}
function model() {
  return {
    id: 'mdl_one',
    name: 'Recorded',
    status: 'active',
    names: [{ name: 'Recorded', is_current: true, expires_at: null }],
    bindings: [],
    granted_user_ids: [],
    created_at: '2026-01-02T03:04:05.123456Z',
    config_updated_at: null,
  }
}
beforeEach(() => {
  calls = []
  value = page()
  client.defaults.adapter = async (config) => {
    calls.push(config)
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: structuredClone(value),
    }
  }
})
afterEach(() => {
  client.defaults.adapter = adapter
})
describe('Recorded Model metadata and exact monthly batch boundary', () => {
  it('accepts present nullable birth/configuration fields and preserves legacy absence without fallback', async () => {
    value = model()
    const detailed = await getAdminModel('mdl_one')
    expect(detailed.created_at).toBe('2026-01-02T03:04:05.123456Z')
    expect(detailed.config_updated_at).toBeNull()
    value = { items: [model(), { ...model(), created_at: null, config_updated_at: null }] }
    expect(
      (await listAdminModels()).every(
        (item) => 'created_at' in item && 'config_updated_at' in item,
      ),
    ).toBe(true)
    expect(() => validateModelRecordedMetadata({})).not.toThrow()
  })
  it.each([
    '2026-02-30T01:02:03Z',
    '0001-01-01T00:00:00Z',
    '2026-01-01',
    '2026-01-02T03:04:05+08:00',
    3,
    undefined,
  ])(
    'rejects present invalid metadata %j without discarding supported nullable fields',
    async (invalid) => {
      expect(() => validateModelRecordedMetadata({ created_at: invalid })).toThrow()
      expect(() => validateModelRecordedMetadata({ config_updated_at: invalid })).toThrow()
      value = { ...model(), config_updated_at: invalid }
      await expect(getAdminModel('mdl_one')).rejects.toThrow('Invalid recorded')
      value = { items: [{ ...model(), created_at: invalid }] }
      await expect(listAdminModels()).rejects.toThrow('Invalid recorded')
    },
  )
  it('sends one repeated named query, forwards the signal, preserves exact counts and has no CSRF/write', async () => {
    const controller = new AbortController()
    const result = await getModelMonthlyRequests(['mdl_one', 'mdl_two'], controller.signal)
    expect(result.items.map((i) => i.requests)).toEqual(['0', '10000'])
    expect(calls).toHaveLength(1)
    expect(calls[0].url).toBe('/admin/model-monthly-requests?model_id=mdl_one&model_id=mdl_two')
    expect(calls[0].signal).toBe(controller.signal)
    expect(calls[0].method).toBe('get')
    expect(calls[0].data).toBeUndefined()
    expect(calls[0].headers.get('X-CSRF-Token')).toBeUndefined()
  })
  it.each(
    [
      [],
      ['mdl_one', 'mdl_one'],
      ['unsafe/path'],
      Array.from({ length: 501 }, (_, i) => 'mdl_' + i),
    ].map((ids) => ({ ids })),
  )('rejects unsupported target batch before HTTP', async ({ ids }) => {
    await expect(getModelMonthlyRequests(ids)).rejects.toThrow('Invalid Model monthly target batch')
    expect(calls).toHaveLength(0)
  })
  it('supports the exact complete 500-target boundary', () => {
    const ids = Array.from({ length: 500 }, (_, i) => 'mdl_' + i)
    const v = { ...page(), items: ids.map((model_id) => ({ model_id, requests: '0' })) }
    expect(validateModelMonthlyRequests(v, ids).items).toHaveLength(500)
  })
  it.each([
    (v: ReturnType<typeof page>) => {
      v.items[0].model_id = 'mdl_alias'
    },
    (v: ReturnType<typeof page>) => {
      v.items[1].model_id = 'mdl_one'
    },
    (v: ReturnType<typeof page>) => {
      v.items.pop()
    },
    (v: ReturnType<typeof page>) => {
      v.items[0].requests = '10001'
    },
    (v: ReturnType<typeof page>) => {
      v.items[0].requests = '1'
    },
    (v: ReturnType<typeof page>) => {
      v.items[0].requests = '01'
    },
    (v: ReturnType<typeof page>) => {
      v.items[0].requests = '1.0'
    },
    (v: ReturnType<typeof page>) => {
      v.items[0].requests = '-1'
    },
    (v: ReturnType<typeof page>) => {
      v.items[0].requests = '1e2'
    },
    (v: ReturnType<typeof page>) => {
      Object.assign(v.items[0], { requests: 0 })
    },
    (v: ReturnType<typeof page>) => {
      Object.assign(v, { timezone: 'Asia/Shanghai' })
    },
    (v: ReturnType<typeof page>) => {
      Object.assign(v, { source: 'estimated' })
    },
    (v: ReturnType<typeof page>) => {
      Object.assign(v, { may_lag: false })
    },
    (v: ReturnType<typeof page>) => {
      v.period_to = '2026-10-08T01:02:03Z'
    },
    (v: ReturnType<typeof page>) => {
      v.period_from = '2026-09-01T00:00:00Z'
    },
    (v: ReturnType<typeof page>) => {
      v.period_from = '2026-10-01T00:00:01Z'
    },
    (v: ReturnType<typeof page>) => {
      v.as_of = v.period_to = '2026-02-30T00:00:00Z'
    },
    (v: ReturnType<typeof page>) => {
      Object.assign(v, { secret: 'forbidden' })
    },
    (v: ReturnType<typeof page>) => {
      Object.assign(v.items[0], { secret: 'forbidden' })
    },
    (v: ReturnType<typeof page>) => {
      delete (v as Partial<ReturnType<typeof page>>).source
    },
  ])('rejects incomplete, aliased, fabricated or unsupported report variant %#', async (mutate) => {
    const v = page()
    mutate(v)
    value = v
    await expect(getModelMonthlyRequests(['mdl_one', 'mdl_two'])).rejects.toThrow(
      'Invalid Model monthly',
    )
  })
})
