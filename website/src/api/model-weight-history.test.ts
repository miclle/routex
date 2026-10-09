import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  getModelWeightRollbackCommand,
  getModelWeightVersion,
  listModelWeightVersions,
  reviewModelWeightRollback,
  rollbackModelWeights,
  validModelWeightReason,
  trimModelWeightReason,
} from './model-weight-history'
import {
  detailFixture,
  modelID,
  requestID,
  resultFixture,
  reviewETag,
  reviewFixture,
  versionID,
} from '@/views/models/routing-weight-history-fixtures'
const adapter = client.defaults.adapter
let calls: InternalAxiosRequestConfig[], value: unknown, headers: AxiosHeaders, status: number
const input = {
  version_id: versionID,
  request_id: requestID,
  reason: 'Restore reviewed complete set',
}
beforeEach(() => {
  calls = []
  value = detailFixture()
  status = 200
  headers = new AxiosHeaders({ 'Cache-Control': 'private, no-store', ETag: '"' + reviewETag + '"' })
  client.defaults.adapter = async (config) => {
    calls.push(config)
    return { config, data: structuredClone(value), status, statusText: '', headers }
  }
})
afterEach(() => {
  client.defaults.adapter = adapter
})
describe('Model weight history private exact contract', () => {
  it('reads retained safe identities and nullable facts without alias conversion or directory queries', async () => {
    const v = detailFixture()
    v.version.valid_weight_set = false
    v.weights[0].binding_created_at = null
    value = v
    const controller = new AbortController(),
      result = await getModelWeightVersion(modelID, versionID, controller.signal)
    expect(result.weights[0].provider_model_id).toBe('pmd_A')
    expect(result.weights[0].binding_created_at).toBeNull()
    expect(calls[0].url).toBe('/admin/models/' + modelID + '/weight-versions/' + versionID)
    expect(calls[0].signal).toBe(controller.signal)
    expect(calls[0].headers.get('X-CSRF-Token')).toBeUndefined()
    expect(calls).toHaveLength(1)
  })
  it('requires UTC wire dates without converting exact birth identities from offsets', async () => {
    const offset = '2026-10-09T14:27:38.676+08:00'
    for (const field of ['model_created_at', 'captured_at'] as const) {
      const version = { ...detailFixture().version, [field]: offset }
      value = { model_id: modelID, items: [version], next_cursor: null }
      await expect(listModelWeightVersions(modelID)).rejects.toThrow('response unavailable')
      value = { ...detailFixture(), version }
      await expect(getModelWeightVersion(modelID, versionID)).rejects.toThrow(
        'response unavailable',
      )
    }
    for (const field of [
      'binding_created_at',
      'provider_model_created_at',
      'connection_created_at',
      'provider_created_at',
    ] as const) {
      const detail = detailFixture()
      detail.weights[0][field] = offset
      value = detail
      await expect(getModelWeightVersion(modelID, versionID)).rejects.toThrow(
        'response unavailable',
      )
      for (const set of ['current_weights', 'proposed_weights'] as const) {
        const review = reviewFixture()
        review[set][0][field] = offset
        value = review
        await expect(reviewModelWeightRollback(modelID, versionID)).rejects.toThrow(
          'response unavailable',
        )
      }
    }
    value = { ...reviewFixture(), observed_at: offset }
    await expect(reviewModelWeightRollback(modelID, versionID)).rejects.toThrow(
      'response unavailable',
    )
    const result = resultFixture()
    result.receipt.created_at = offset
    value = result
    await expect(rollbackModelWeights(modelID, reviewETag, input, 'csrf')).rejects.toThrow(
      'response unavailable',
    )
    await expect(getModelWeightRollbackCommand(modelID, input)).rejects.toThrow(
      'response unavailable',
    )
  })
  it('keeps opaque actor-bound cursor bytes and does not select a version or fetch snapshots', async () => {
    value = { model_id: modelID, items: [detailFixture().version], next_cursor: 'opaque+/=' }
    expect((await listModelWeightVersions(modelID, 'opaque+/=')).next_cursor).toBe('opaque+/=')
    expect(calls[0].url).toBe(
      '/admin/models/' + modelID + '/weight-versions?limit=20&cursor=opaque%2B%2F%3D',
    )
    expect(calls[0].data).toBeUndefined()
  })
  it.each(['mdl_../../other', 'model_wrong', 'mdl_' + 'x'.repeat(27)])(
    'rejects unsafe Model path before HTTP %s',
    async (id) => {
      await expect(getModelWeightVersion(id, versionID)).rejects.toThrow()
      expect(calls).toHaveLength(0)
    },
  )
  it.each(['mwv_short', versionID.toUpperCase(), 'mwv_8' + '1'.repeat(25)])(
    'requires canonical new version IDs %s',
    async (id) => {
      await expect(getModelWeightVersion(modelID, id)).rejects.toThrow()
      expect(calls).toHaveLength(0)
    },
  )
  it.each([
    (v: ReturnType<typeof detailFixture>) => {
      v.weights.reverse()
    },
    (v: ReturnType<typeof detailFixture>) => {
      v.weights[1].binding_id = v.weights[0].binding_id
    },
    (v: ReturnType<typeof detailFixture>) => {
      v.weights[0].weight = 0.5
    },
    (v: ReturnType<typeof detailFixture>) => {
      v.weights[0].provider_model_id = 'pmd_/unsafe'
    },
    (v: ReturnType<typeof detailFixture>) => {
      v.weights[0].binding_created_at = null
    },
    (v: ReturnType<typeof detailFixture>) => {
      v.weights[1].weight = 99
    },
    (v: ReturnType<typeof detailFixture>) => {
      v.version.binding_count = 3
    },
    (v: ReturnType<typeof detailFixture>) => {
      v.version.model_id = 'mdl_other'
    },
    (v: ReturnType<typeof detailFixture>) => {
      v.version.captured_at = '2026-02-30T00:00:00Z'
    },
    (v: ReturnType<typeof detailFixture>) => {
      Object.assign(v.weights[0], { secret: 'never exposed' })
    },
  ])('rejects partial, fabricated or unsafe recorded facts %#', async (mutate) => {
    const v = detailFixture()
    mutate(v)
    value = v
    await expect(getModelWeightVersion(modelID, versionID)).rejects.toThrow('response unavailable')
  })
  it('allows two complete bounded 1,000-row sets without truncating review to one snapshot budget', async () => {
    const v = reviewFixture()
    const rows = Array.from({ length: 1000 }, (_, i) => ({
      ...v.proposed_weights[0],
      binding_id: 'bnd_' + String(i).padStart(26, '0'),
      provider_model_id: 'pmd_' + 'a'.repeat(26),
      connection_id: 'con_' + 'b'.repeat(26),
      provider_id: 'prv_' + 'c'.repeat(26),
      weight: i === 0 ? 100 : 0,
    }))
    v.current_weights = rows
    v.proposed_weights = rows
    value = v
    expect(new TextEncoder().encode(JSON.stringify(v)).length).toBeGreaterThan(512 * 1024)
    const complete = await reviewModelWeightRollback(modelID, versionID)
    expect(complete.current_weights).toHaveLength(1000)
    expect(complete.proposed_weights).toHaveLength(1000)
    value = { ...v, proposed_weights: [...rows, { ...rows[0], binding_id: 'bnd_extra' }] }
    await expect(reviewModelWeightRollback(modelID, versionID)).rejects.toThrow()
  })
  it('binds body token to exact strong ETag and preserves blocked unknown codes', async () => {
    value = { ...reviewFixture(), eligible: false, blocker_codes: ['future_guard'] }
    const res = await reviewModelWeightRollback(modelID, versionID)
    expect(res.blocker_codes).toEqual(['future_guard'])
    expect(res.eligible).toBe(false)
    expect(calls[0].url).toBe(
      '/admin/models/' + modelID + '/weights/rollback-review?version_id=' + versionID,
    )
  })
  it.each(['W/"' + reviewETag + '"', '"' + 'b'.repeat(64) + '"', reviewETag])(
    'rejects incoherent review header %s',
    async (header) => {
      value = reviewFixture()
      headers.set('ETag', header)
      await expect(reviewModelWeightRollback(modelID, versionID)).rejects.toThrow()
    },
  )
  it.each(['public, no-store', 'private', 'no-store', ''])(
    'requires private no-store original response %s',
    async (header) => {
      headers.set('Cache-Control', header)
      await expect(getModelWeightVersion(modelID, versionID)).rejects.toThrow()
    },
  )
  it('rejects positive eligibility with blockers and bounded overflow before private facts are returned', async () => {
    value = { ...reviewFixture(), blocker_codes: ['future_guard'] }
    await expect(reviewModelWeightRollback(modelID, versionID)).rejects.toThrow()
    value = { model_id: modelID, items: [], next_cursor: 'x'.repeat(512 * 1024) }
    await expect(listModelWeightVersions(modelID)).rejects.toThrow()
  })
  it.each(['applied', 'pending', 'superseded', 'unknown'] as const)(
    'retains immutable command proof separately from %s',
    async (application_status) => {
      value = {
        ...resultFixture(),
        application_status,
        runtime_applied: application_status === 'applied',
      }
      const res = await rollbackModelWeights(modelID, reviewETag, input, 'current-csrf')
      expect(res.application_status).toBe(application_status)
      expect(calls[0].headers.get('If-Match')).toBe('"' + reviewETag + '"')
      expect(calls[0].headers.get('X-CSRF-Token')).toBe('current-csrf')
      expect(JSON.parse(calls[0].data)).toEqual(input)
      const recovered = await getModelWeightRollbackCommand(modelID, input)
      expect(recovered.receipt).toEqual(res.receipt)
      expect(calls[1].url).toBe(
        '/admin/models/' + modelID + '/weights/rollback-commands/' + requestID,
      )
      expect(calls[1].method).toBe('get')
      expect(calls[1].data).toBeUndefined()
      expect(calls[1].headers.get('X-CSRF-Token')).toBeUndefined()
    },
  )
  it.each([
    (v: ReturnType<typeof resultFixture>) => {
      v.receipt.reason = 'different'
    },
    (v: ReturnType<typeof resultFixture>) => {
      v.receipt.request_id = 'different'
    },
    (v: ReturnType<typeof resultFixture>) => {
      v.receipt.model_id = 'mdl_other'
    },
    (v: ReturnType<typeof resultFixture>) => {
      v.runtime_applied = false
    },
    (v: ReturnType<typeof resultFixture>) => {
      Object.assign(v.receipt, { secret: 'no' })
    },
  ])('never reports a mismatched receipt or contradictory application %#', async (mutate) => {
    const v = resultFixture()
    mutate(v)
    value = v
    await expect(rollbackModelWeights(modelID, reviewETag, input, 'csrf')).rejects.toThrow()
  })
  it('preserves Unicode reason boundaries and limits UTF-8 instead of character count', async () => {
    expect(trimModelWeightReason('\u2003恢复\u2003')).toBe('恢复')
    expect(trimModelWeightReason('\uFEFFx')).toBe('\uFEFFx')
    expect(validModelWeightReason('界'.repeat(341))).toBe(true)
    expect(validModelWeightReason('界'.repeat(342))).toBe(false)
    for (const reason of ['', ' x', 'x\n', '\uD800'])
      expect(validModelWeightReason(reason)).toBe(false)
    await expect(
      rollbackModelWeights(modelID, reviewETag, { ...input, request_id: 'not-uuid' }, 'csrf'),
    ).rejects.toThrow()
    expect(calls).toHaveLength(0)
  })
})
