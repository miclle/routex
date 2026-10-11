import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  getModelLifecycle,
  updateModelLifecycle,
  validModelLifecycleReason,
} from './model-lifecycle'
const original = client.defaults.adapter
const etag = 'a'.repeat(64) + '.' + 'b'.repeat(64)
const review = () => ({
  model_id: 'mdl_one',
  current_name: 'Current',
  created_at: '2026-01-01T00:00:00Z',
  status: 'active',
  observed_at: '2026-10-12T00:00:00Z',
  etag,
  can_change: true,
  runtime_applied: true,
})
let value: unknown, status: number, requests: InternalAxiosRequestConfig[]
beforeEach(() => {
  value = review()
  status = 200
  requests = []
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return {
      config,
      status,
      statusText: '',
      headers: new AxiosHeaders(),
      data: structuredClone(value),
    }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
describe('exact reversible Model lifecycle wire', () => {
  it('reads the exact target without query and submits only reviewed status/reason with current CSRF', async () => {
    const controller = new AbortController()
    expect(await getModelLifecycle('mdl_one', controller.signal)).toEqual(review())
    expect(requests[0].url).toBe('/admin/models/mdl_one/lifecycle')
    expect(requests[0].params).toBeUndefined()
    expect(requests[0].signal).toBe(controller.signal)
    value = { model: { ...review(), status: 'disabled' }, changed: false, runtime_applied: true }
    expect(
      (
        await updateModelLifecycle(
          'mdl_one',
          { status: 'disabled', reason: 'Exact reason' },
          etag,
          'fresh-csrf',
          controller.signal,
        )
      ).changed,
    ).toBe(false)
    expect(requests[1].method).toBe('put')
    expect(JSON.parse(requests[1].data)).toEqual({ status: 'disabled', reason: 'Exact reason' })
    expect(requests[1].headers.get('If-Match')).toBe('"' + etag + '"')
    expect(requests[1].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
  })
  it.each([
    { model_id: 'mdl_other' },
    { created_at: null },
    { status: 'inactive' },
    { status: ['active'] },
    { status: 'archived', can_change: true },
    { etag: 'a'.repeat(64) },
    { etag: 'W/"' + etag + '"' },
    { observed_at: 'yesterday' },
    { runtime_applied: null },
    { secret: 'forbidden' },
  ])('rejects malformed or cross-target review %j', async (delta) => {
    value = { ...review(), ...delta }
    await expect(getModelLifecycle('mdl_one')).rejects.toThrow()
  })
  it('requires every review field and accepts read-only archived/current-unconfirmed facts', async () => {
    for (const key of Object.keys(review())) {
      const data: Record<string, unknown> = review()
      delete data[key]
      value = data
      await expect(getModelLifecycle('mdl_one')).rejects.toThrow()
    }
    value = { ...review(), status: 'archived', can_change: false, runtime_applied: false }
    expect((await getModelLifecycle('mdl_one')).status).toBe('archived')
  })
  it.each([
    { model: { ...review(), status: 'active' }, changed: true, runtime_applied: true },
    { model: { ...review(), status: 'disabled' }, changed: true, runtime_applied: false },
    { model: { ...review(), status: 'disabled' }, changed: 'yes', runtime_applied: true },
    {
      model: { ...review(), status: 'disabled' },
      changed: true,
      runtime_applied: true,
      command_completed: true,
    },
  ])('rejects response facts inconsistent with exact desired status %j', async (response) => {
    value = response
    await expect(
      updateModelLifecycle('mdl_one', { status: 'disabled', reason: 'Review' }, etag, 'csrf'),
    ).rejects.toThrow()
  })
  it('accepts exact terminal archive reconciliation despite read-only can_change', async () => {
    value = {
      model: { ...review(), status: 'archived', can_change: false },
      changed: false,
      runtime_applied: true,
    }
    const result = await updateModelLifecycle(
      'mdl_one',
      { status: 'archived', reason: 'Retain historical Model' },
      etag,
      'fresh-csrf',
    )
    expect(result.model.can_change).toBe(false)
    expect(result.changed).toBe(false)
    expect(JSON.parse(requests[0].data)).toEqual({
      status: 'archived',
      reason: 'Retain historical Model',
    })
  })
  it('rejects a same-ID response with a different immutable identity token', async () => {
    value = {
      model: { ...review(), status: 'disabled', etag: 'f'.repeat(64) + '.' + 'b'.repeat(64) },
      changed: false,
      runtime_applied: true,
    }
    await expect(
      updateModelLifecycle('mdl_one', { status: 'disabled', reason: 'Review' }, etag, 'csrf'),
    ).rejects.toThrow()
  })
  it('does not accept an unexpected success status', async () => {
    status = 201
    await expect(getModelLifecycle('mdl_one')).rejects.toThrow()
  })
  it('validates raw UTF-8 reasons and exact target/etag before dispatch', async () => {
    expect(validModelLifecycleReason('界'.repeat(341))).toBe(true)
    for (const reason of [' ', '界'.repeat(342), 'bad\nreason', '\u007f', '\ud800']) {
      expect(validModelLifecycleReason(reason)).toBe(false)
      await expect(
        updateModelLifecycle('mdl_one', { status: 'disabled', reason }, etag, 'csrf'),
      ).rejects.toThrow()
    }
    await expect(getModelLifecycle('../mdl_one')).rejects.toThrow()
    await expect(
      updateModelLifecycle('mdl_one', { status: 'disabled', reason: 'Review' }, 'bad', 'csrf'),
    ).rejects.toThrow()
    expect(requests).toHaveLength(0)
  })
})
