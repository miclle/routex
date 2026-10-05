import { afterEach, describe, expect, it, vi } from 'vitest'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  completeOffboarding,
  createOffboardingPlan,
  emergencyOffboarding,
  getOffboarding,
  OffboardingError,
} from './offboarding'

const oldAdapter = client.defaults.adapter
const plan = {
  request_id: 'original-test-uuid',
  inventory_version: 'reviewed-digest',
  planned_at: '2026-10-01T09:00:00Z',
  reason: 'Exact reviewed reason',
  project_assignments: [],
  team_assignments: [],
}
afterEach(() => {
  client.defaults.adapter = oldAdapter
  vi.restoreAllMocks()
})
function response(config: InternalAxiosRequestConfig, status = 200) {
  return {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: { id: 'ofb_receipt' },
  }
}
describe('offboarding transport', () => {
  it('threads read cancellation through the exact escaped member path', async () => {
    const operation = new AbortController()
    let seen!: InternalAxiosRequestConfig
    client.defaults.adapter = async (config) => {
      seen = config
      return response(config)
    }
    await getOffboarding('usr_target/name', operation.signal)
    expect(seen.url).toBe('/admin/members/usr_target%2Fname/offboarding')
    expect(seen.signal).toBe(operation.signal)
    expect(seen.headers.get('X-CSRF-Token')).toBeUndefined()
  })
  it('preserves plan body and inventory digest with fresh CSRF and cancellation, without inventing If-Match', async () => {
    const operation = new AbortController()
    let seen!: InternalAxiosRequestConfig
    client.defaults.adapter = async (config) => {
      seen = config
      return response(config, 201)
    }
    await createOffboardingPlan('usr_target', plan, 'current-csrf', operation.signal)
    expect(seen.url).toBe('/admin/members/usr_target/offboarding/plans')
    expect(JSON.parse(seen.data)).toEqual(plan)
    expect(plan.reason).toBe('Exact reviewed reason')
    expect(seen.headers.get('X-CSRF-Token')).toBe('current-csrf')
    expect(seen.headers.get('If-Match')).toBeUndefined()
    expect(seen.signal).toBe(operation.signal)
  })
  it('completes only the original escaped case with an empty body', async () => {
    const operation = new AbortController()
    let seen!: InternalAxiosRequestConfig
    client.defaults.adapter = async (config) => {
      seen = config
      return response(config)
    }
    await completeOffboarding('usr_target', 'ofb_case/name', 'current-csrf', operation.signal)
    expect(seen.url).toBe('/admin/members/usr_target/offboarding/ofb_case%2Fname/complete')
    expect(JSON.parse(seen.data)).toEqual({})
    expect(seen.signal).toBe(operation.signal)
    expect(seen.headers.get('X-CSRF-Token')).toBe('current-csrf')
  })
  it('handles emergency password rejection locally and retains no Axios request or proof in errors', async () => {
    const dispatch = vi.spyOn(window, 'dispatchEvent')
    const intent = {
      request_id: 'immutable-uuid',
      reason: 'Original emergency reason',
      team_assignments: [],
    }
    let seen!: InternalAxiosRequestConfig
    client.defaults.adapter = async (config) => {
      seen = config
      expect(config.validateStatus?.(401)).toBe(true)
      return response(config, 401)
    }
    const error = await emergencyOffboarding(
      'usr_target',
      intent,
      'transient-proof',
      'current-csrf',
    ).catch((error: unknown) => error)
    expect(error).toBeInstanceOf(OffboardingError)
    expect((error as OffboardingError).status).toBe(401)
    expect(Object.keys(error as object)).toEqual(['status'])
    expect(JSON.stringify(error)).not.toContain('transient-proof')
    expect(dispatch).not.toHaveBeenCalled()
    expect(JSON.parse(seen.data)).toEqual({ ...intent, current_password: 'transient-proof' })
    expect(intent).not.toHaveProperty('current_password')
  })
  it.each([409, 412, 503])(
    'returns only status for a failed dispatched HTTP %s body',
    async (status) => {
      client.defaults.adapter = async (config) => {
        throw new AxiosError(
          'Private request failure',
          '',
          config,
          undefined,
          response(config, status),
        )
      }
      const error = await createOffboardingPlan('usr_target', plan, 'csrf').catch(
        (error: unknown) => error,
      )
      expect(error).toBeInstanceOf(OffboardingError)
      expect((error as OffboardingError).status).toBe(status)
      expect(Object.keys(error as object)).toEqual(['status'])
      expect(JSON.stringify(error)).not.toContain(plan.reason)
    },
  )
  it('does not dispatch an already cancelled operation or retain its proof', async () => {
    const adapter = vi.fn(async (config: InternalAxiosRequestConfig) => response(config))
    client.defaults.adapter = adapter
    const operation = new AbortController()
    operation.abort()
    await expect(
      emergencyOffboarding(
        'usr_target',
        { request_id: 'same', reason: 'Original', team_assignments: [] },
        'transient-proof',
        'csrf',
        operation.signal,
      ),
    ).rejects.toMatchObject({ status: 0 })
    expect(adapter).not.toHaveBeenCalled()
  })
})
