import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, describe, expect, it } from 'vitest'
import client from './client'
import {
  createPersonalModelRequest,
  decidePersonalModelRequest,
  getPersonalModelCandidate,
  getPersonalModelWorkspace,
  listPersonalModelCandidates,
  listPersonalModelRequests,
  personalModelOutcomeUnknown,
  validPersonalModelReason,
} from './personal-model-requests'

const original = client.defaults.adapter
const uuid = '11111111-1111-4111-8111-111111111111'
const etag = 'a'.repeat(64)
const record = {
  id: 'mar_request',
  request_id: uuid,
  applicant_user_id: 'usr_owner',
  applicant_name: 'Applicant',
  model_id: 'mdl_model',
  model_name: 'Model',
  reason: 'Research',
  status: 'pending',
  created_at: '2026-10-04T00:00:00Z',
  updated_at: '2026-10-04T00:00:00Z',
  resolved_at: null,
  cancelled_reason: null,
  decision: null,
}
const detail = {
  ...record,
  current_model: { id: 'mdl_model', name: 'Model', status: 'active' },
  current_granted: false,
  review_etag: etag,
  allowed_actions: ['withdraw'],
  runtime_applied: false,
  application_status: 'pending',
}
const candidate = {
  id: 'mdl_model',
  name: 'Model',
  status: 'active',
  created_at: record.created_at,
  protocols: ['openai_chat'],
  input_capabilities: {},
  personal_granted: false,
  pending_request_id: null,
  review_etag: etag,
}
function respond(data: unknown) {
  const calls: InternalAxiosRequestConfig[] = []
  client.defaults.adapter = async (config) => {
    calls.push(config)
    return { config, status: 200, statusText: '', headers: new AxiosHeaders(), data }
  }
  return calls
}
afterEach(() => {
  client.defaults.adapter = original
})

describe('Personal model request API boundary', () => {
  it('keeps candidate discovery separate and bounded while retaining literal search and cancellation', async () => {
    const calls = respond({ items: [candidate], next_cursor: 'mdl_next' })
    const signal = new AbortController().signal
    expect(await listPersonalModelCandidates('[.*]', null, signal)).toEqual({
      items: [candidate],
      next_cursor: 'mdl_next',
    })
    expect(calls[0].url).toBe('/model-access-candidates')
    expect(calls[0].params).toEqual({ q: '[.*]', cursor: undefined, limit: 50 })
    expect(calls[0].signal).toBe(signal)
    respond({ ...candidate, id: 'mdl_other' })
    await expect(getPersonalModelCandidate('mdl_model')).rejects.toThrow('Invalid')
  })
  it('accepts bounded opaque history cursors and rejects cross-owner rows', async () => {
    const next = 'A'.repeat(300)
    const calls = respond({ items: [record], total: 1, next_cursor: next })
    expect(
      (await listPersonalModelRequests('usr_reviewer', 'usr_owner', 'pending', null)).next_cursor,
    ).toBe(next)
    expect(calls[0].url).toBe('/admin/members/usr_owner/model-requests')
    expect(calls[0].params.status).toBe('pending')
    respond({ items: [{ ...record, applicant_user_id: 'usr_other' }], total: 1, next_cursor: null })
    await expect(listPersonalModelRequests('usr_owner', undefined, '', null)).rejects.toThrow(
      'Invalid',
    )
  })
  it.each([
    { items: [candidate, candidate], next_cursor: null },
    { items: Array.from({ length: 51 }, () => candidate), next_cursor: null },
    { items: [{ ...candidate, review_etag: 'weak' }], next_cursor: null },
    { items: [{ ...candidate, personal_granted: 'false' }], next_cursor: null },
  ])('rejects malformed or partial candidate responses %j', async (value) => {
    respond(value)
    await expect(listPersonalModelCandidates('', null)).rejects.toThrow('Invalid')
  })
  it('captures exact creation bytes and UUID with a strong reviewed If-Match', async () => {
    const calls = respond(detail)
    const intent = { body: { request_id: uuid, model_id: 'mdl_model', reason: 'Research' }, etag }
    await createPersonalModelRequest('usr_owner', intent, 'csrf')
    expect(JSON.parse(calls[0].data)).toEqual(intent.body)
    expect(calls[0].headers.get('If-Match')).toBe(`"${etag}"`)
    expect(calls[0].headers.get('X-CSRF-Token')).toBe('csrf')
    respond({ ...detail, request_id: '22222222-2222-4222-8222-222222222222' })
    await expect(createPersonalModelRequest('usr_owner', intent, 'csrf')).rejects.toThrow('Invalid')
  })
  it('validates a historical approval receipt separately from current superseded application', async () => {
    const saved = {
      ...record,
      status: 'approved',
      resolved_at: record.created_at,
      decision: {
        decision_id: uuid,
        actor_id: 'usr_reviewer',
        actor_name: 'Reviewer',
        action: 'approve',
        reason: '',
        decided_at: record.created_at,
      },
    }
    const receipt = {
      decision_id: uuid,
      committed: true,
      saved_request: saved,
      current_granted: false,
      runtime_applied: false,
      application_status: 'superseded',
    }
    const calls = respond(receipt)
    const intent = { body: { decision_id: uuid, action: 'approve' as const, reason: '' }, etag }
    expect(
      await decidePersonalModelRequest('usr_reviewer', 'usr_owner', record.id, intent, 'csrf'),
    ).toEqual(receipt)
    expect(calls[0].url).toBe('/admin/members/usr_owner/model-requests/mar_request/decision')
    respond({ ...receipt, runtime_applied: true })
    await expect(
      decidePersonalModelRequest('usr_reviewer', 'usr_owner', record.id, intent, 'csrf'),
    ).rejects.toThrow('Invalid')
    respond({
      ...receipt,
      saved_request: { ...saved, decision: { ...saved.decision, actor_id: 'usr_other' } },
    })
    await expect(
      decidePersonalModelRequest('usr_reviewer', 'usr_owner', record.id, intent, 'csrf'),
    ).rejects.toThrow('Invalid')
  })
  it('loads only the exact minimal workspace and rejects a mismatched target or incomplete grant count', async () => {
    const data = {
      user_id: 'usr_owner',
      models: [{ id: 'mdl_model', name: 'Model', status: 'active' }],
      model_count: 1,
      can_review_requests: true,
    }
    const calls = respond(data)
    expect(await getPersonalModelWorkspace('usr_owner')).toEqual(data)
    expect(calls[0].url).toBe('/admin/members/usr_owner/model-access-workspace')
    respond({ ...data, user_id: 'usr_other' })
    await expect(getPersonalModelWorkspace('usr_owner')).rejects.toThrow('Invalid')
    respond({ ...data, model_count: 2 })
    await expect(getPersonalModelWorkspace('usr_owner')).rejects.toThrow('Invalid')
  })
  it('validates UTF-8 byte limits and all Unicode control characters', () => {
    expect(validPersonalModelReason('中'.repeat(341))).toBe(true)
    expect(validPersonalModelReason('中'.repeat(342))).toBe(false)
    for (const value of ['reason\n', 'reason\u0085', 'reason\u009f', 'reason\u0000'])
      expect(validPersonalModelReason(value)).toBe(false)
    expect(validPersonalModelReason('')).toBe(false)
    expect(validPersonalModelReason('', false)).toBe(true)
  })
  it('treats lost/malformed/server responses as unknown but definitive HTTP rejection separately', () => {
    expect(personalModelOutcomeUnknown(new Error('Invalid response'))).toBe(true)
    expect(personalModelOutcomeUnknown(new AxiosError('Lost'))).toBe(true)
    for (const status of [403, 409, 422, 503]) {
      const error = new AxiosError('Response', '', undefined, undefined, {
        status,
        statusText: '',
        headers: {},
        config: {} as InternalAxiosRequestConfig,
        data: {},
      })
      expect(personalModelOutcomeUnknown(error)).toBe(status >= 500)
    }
  })
})
