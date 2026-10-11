import { beforeEach, describe, expect, it, vi } from 'vitest'
import client from './client'
import {
  getKeyDeliveryContext,
  getKeyDeliveryDescriptor,
  reconcileManagedKey,
  getKeyDeliveryReceipt,
  issueManagedKey,
  KeyDeliveryError,
  parseKeyCallerTemplate,
  parseKeyDeliveryContext,
  parseKeyDeliveryDescriptor,
  parseKeyDeliveryReceipt,
  parseKeyDeliveryTarget,
  parseManagedKeyResult,
  saveKeyDeliveryIdentity,
} from './key-delivery'
import type { KeyDeliveryDescriptor, ManagedKeyInput } from '@/types/key-delivery'

vi.mock('./client', () => ({ default: { request: vi.fn() } }))
const request = vi.mocked(client.request)
const suffix = '01m36yee4gkbns18pfcqqc75a3'
const identity = (prefix: string) => prefix + '_' + suffix
const review = 'a'.repeat(64)
const csrf = 'b'.repeat(64)
const requestId = '10000000-0000-4000-8000-000000000001'
const timestamp = '2026-10-11T00:00:00Z'
const owner = { kind: 'personal' } as const
const context = {
  allowed_modes: ['manual', 'managed_vault'],
  default_mode: 'manual',
  managed_available: false,
  failure_code: 'dependency_validation_unavailable',
  review_etag: review,
}
const descriptor: KeyDeliveryDescriptor = {
  schema_version: 1,
  key_id: identity('key'),
  profile_id: identity('vap'),
  principal_id: identity('vpb'),
  slot_id: identity('kds'),
  descriptor_revision: '9007199254740993',
  endpoint: 'https://vault.example.test',
  namespace: '',
  mount: 'kv',
  path: 'api-keys/exact',
  data_field: 'value',
  version: 1,
  method: 'jwt',
  auth_mount: 'jwt',
  role: 'routex-api-key-' + identity('vpb'),
  coordinator: 'not_configured',
  application_status: 'unknown',
}
const receipt = {
  request_id: requestId,
  key_id: identity('key'),
  mode: 'managed_vault',
  delivery_status: 'acknowledged',
  key_status: 'active',
  runtime_status: 'confirmed',
  replacement_call_status: 'not_applicable',
  failure_code: null,
  descriptor,
  review_etag: review,
}
const key = {
  id: identity('key'),
  name: 'Production',
  prefix: 'rx_test',
  status: 'active',
  model_ids: [identity('mdl')],
  expires_at: null,
  created_at: timestamp,
  replaces_key_id: null,
  delivery_expires_at: null,
  delivery_mode: 'managed_vault',
}
const input: ManagedKeyInput = {
  name: 'Production',
  model_ids: [identity('mdl')],
  expires_at: null,
  request_id: requestId,
  reason: 'Create reviewed application access',
  delivery: {
    consumer_id: identity('kdc'),
    environment: 'production',
    principal: { method: 'jwt', subject: 'production-app' },
    target_review: review,
  },
}
beforeEach(() => request.mockReset())
describe('managed API-Key response boundaries', () => {
  it('keeps exact descriptor revisions beyond JavaScript integer precision', () => {
    expect(parseKeyDeliveryDescriptor(descriptor).descriptor_revision).toBe('9007199254740993')
    expect(
      parseKeyDeliveryDescriptor({ ...descriptor, descriptor_revision: '18446744073709551615' })
        .descriptor_revision,
    ).toBe('18446744073709551615')
  })
  it.each([Number.MAX_SAFE_INTEGER + 2, '01', '0', '18446744073709551616', '-1', '1e3'])(
    'rejects a noncanonical or overflowing revision: %s',
    (value) => {
      expect(() =>
        parseKeyDeliveryDescriptor({ ...descriptor, descriptor_revision: value }),
      ).toThrow(KeyDeliveryError)
    },
  )
  it('rejects secrets or unknown nested descriptor fields', () => {
    expect(() => parseKeyDeliveryDescriptor({ ...descriptor, secret: 'must-not-render' })).toThrow()
    expect(() =>
      parseKeyDeliveryReceipt({
        ...receipt,
        descriptor: { ...descriptor, token: 'must-not-render' },
      }),
    ).toThrow()
    expect(() =>
      parseManagedKeyResult({ key, delivery: receipt, secret: 'must-not-render' }, owner),
    ).toThrow()
    expect(() =>
      parseManagedKeyResult(
        { key: { ...key, token_hash: 'must-not-render' }, delivery: receipt },
        owner,
      ),
    ).toThrow()
  })
  it('retains acknowledged delivery separately from runtime and caller application', () => {
    const uncertain = parseKeyDeliveryReceipt({
      ...receipt,
      runtime_status: 'unknown',
      descriptor: null,
    })
    expect(uncertain.delivery_status).toBe('acknowledged')
    expect(uncertain.runtime_status).toBe('unknown')
    expect(parseKeyDeliveryReceipt(receipt).descriptor?.application_status).toBe('unknown')
    expect(() => parseKeyDeliveryReceipt({ ...receipt, runtime_status: 'unknown' })).toThrow()
    expect(() => parseKeyDeliveryReceipt({ ...receipt, delivery_status: 'unknown' })).toThrow()
  })
  it.each(['not_applicable', 'not_recorded', 'completed'] as const)(
    'retains only the server replacement-call status %s independently of publication',
    (replacement_call_status) => {
      const value = parseKeyDeliveryReceipt({
        ...receipt,
        runtime_status: 'unknown',
        descriptor: null,
        replacement_call_status,
      })
      expect(value.replacement_call_status).toBe(replacement_call_status)
      expect(value.runtime_status).toBe('unknown')
      expect(value.descriptor).toBeNull()
      expect(parseKeyDeliveryDescriptor(descriptor)).toEqual(descriptor)
    },
  )
  it.each([undefined, null, '', 'verified', 'COMPLETED', true, 1])(
    'rejects missing or unknown replacement-call proof: %s',
    (replacement_call_status) => {
      expect(() => parseKeyDeliveryReceipt({ ...receipt, replacement_call_status })).toThrow(
        KeyDeliveryError,
      )
    },
  )
  it('rejects an absent replacement-call field rather than defaulting from other receipt facts', () => {
    const { replacement_call_status: omitted, ...missing } = receipt
    expect(omitted).toBe('not_applicable')
    expect(() => parseKeyDeliveryReceipt(missing)).toThrow(KeyDeliveryError)
  })
  it.each(['personal', 'project'] as const)(
    'requires a nonempty canonical %s Key when a request receipt establishes its identity',
    (kind) => {
      const scope =
        kind === 'personal' ? ({ kind } as const) : ({ kind, projectId: identity('prj') } as const)
      for (const key_id of [
        '',
        'invalid',
        'key_bad',
        identity(kind === 'personal' ? 'pky' : 'key'),
      ])
        expect(() =>
          parseKeyDeliveryReceipt({ ...receipt, key_id, descriptor: null }, scope),
        ).toThrow(KeyDeliveryError)
    },
  )
  it('rejects cross-Key receipt or descriptor attribution', () => {
    expect(() =>
      parseManagedKeyResult(
        { key: { ...key, id: 'key_01m36yee4gkbns18pfcqqc75a4' }, delivery: receipt },
        owner,
      ),
    ).toThrow()
    expect(() =>
      parseKeyDeliveryReceipt({
        ...receipt,
        descriptor: { ...descriptor, key_id: 'key_01m36yee4gkbns18pfcqqc75a4' },
      }),
    ).toThrow()
  })
  it('requires the exact Project owner and distinct Project shape', () => {
    const project = { kind: 'project', projectId: identity('prj') } as const
    const projectKey = {
      ...key,
      id: identity('pky'),
      project_id: project.projectId,
      creator_id: identity('usr'),
    }
    const projectReceipt = {
      ...receipt,
      key_id: projectKey.id,
      descriptor: { ...descriptor, key_id: projectKey.id },
    }
    expect(
      parseManagedKeyResult({ key: projectKey, delivery: projectReceipt }, project).key,
    ).toEqual(projectKey)
    expect(() =>
      parseManagedKeyResult({ key: projectKey, delivery: projectReceipt }, owner),
    ).toThrow()
    expect(() =>
      parseManagedKeyResult(
        {
          key: { ...projectKey, project_id: 'prj_01m36yee4gkbns18pfcqqc75a4' },
          delivery: projectReceipt,
        },
        project,
      ),
    ).toThrow()
  })
  it('never treats a pending control review as ready isolation', () => {
    const target = {
      id: identity('vap'),
      consumer_id: identity('kdc'),
      application_name: '应用'.repeat(50),
      environment: 'production',
      status: 'pending',
      control_verified: false,
      isolation: { status: 'missing', revision: null, recorded_at: null },
      failure_code: 'principal_control_unverified',
      review_etag: review,
    }
    expect(parseKeyDeliveryTarget(target).application_name).toBe(target.application_name)
    expect(() => parseKeyDeliveryTarget({ ...target, status: 'ready' })).toThrow()
  })
  it('accepts a real unconfigured caller template without inventing defaults', () => {
    const empty = {
      auth_mount: '',
      issuer: '',
      jwks_url: '',
      audience: '',
      allowed_subjects: [],
      token_ttl_seconds: 0,
      token_max_ttl_seconds: 0,
      verified: false,
      review_etag: review,
    }
    expect(parseKeyCallerTemplate(empty)).toEqual(empty)
    expect(() => parseKeyCallerTemplate({ ...empty, verified: true })).toThrow()
    expect(() => parseKeyCallerTemplate({ ...empty, token_ttl_seconds: 1 })).toThrow()
  })
  it('keeps verified dependencies separate from policy-enabled delivery modes', () => {
    const verifiedManual = {
      ...context,
      allowed_modes: ['manual'],
      managed_available: true,
      failure_code: null,
    }
    expect(parseKeyDeliveryContext(verifiedManual)).toEqual(verifiedManual)
    expect(() =>
      parseKeyDeliveryContext({ ...verifiedManual, default_mode: 'managed_vault' }),
    ).toThrow()
    expect(() =>
      parseKeyDeliveryContext({
        ...verifiedManual,
        failure_code: 'dependency_validation_unavailable',
      }),
    ).toThrow()
  })
  it('rejects contradictory policy availability and unknown delivery modes', () => {
    expect(parseKeyDeliveryContext(context).managed_available).toBe(false)
    expect(() => parseKeyDeliveryContext({ ...context, managed_available: true })).toThrow()
    expect(() =>
      parseKeyDeliveryContext({ ...context, allowed_modes: ['manual', 'fallback'] }),
    ).toThrow()
  })
})
describe('managed API-Key transport', () => {
  it('rotates with server-owned metadata and rejects local replacement overrides', async () => {
    const predecessor = 'key_01m36yee4gkbns18pfcqqc75a4'
    const rotation = { ...input, name: '', model_ids: [], expires_at: null }
    const replacement = { ...key, replaces_key_id: predecessor }
    request.mockResolvedValueOnce({
      status: 201,
      data: { key: replacement, delivery: receipt },
      headers: { etag: `"${review}"` },
    })
    expect(
      (await issueManagedKey(owner, rotation, review, csrf, undefined, predecessor)).key,
    ).toEqual(replacement)
    expect(request).toHaveBeenCalledWith(
      expect.objectContaining({
        url: '/keys/' + predecessor + '/managed-rotation',
        data: rotation,
      }),
    )
    await expect(
      issueManagedKey(owner, input, review, csrf, undefined, predecessor),
    ).rejects.toThrow()
    expect(request).toHaveBeenCalledTimes(1)
  })
  it('uses Project Key identities throughout issuance, receipt, descriptor and reconciliation', async () => {
    const project = { kind: 'project', projectId: identity('prj') } as const
    const projectKey = {
      ...key,
      id: identity('pky'),
      project_id: project.projectId,
      creator_id: identity('usr'),
    }
    const projectDescriptor = { ...descriptor, key_id: projectKey.id }
    const projectReceipt = { ...receipt, key_id: projectKey.id, descriptor: projectDescriptor }
    const headers = { etag: `"${review}"` }
    request.mockResolvedValueOnce({
      status: 201,
      data: { key: projectKey, delivery: projectReceipt },
      headers,
    })
    expect((await issueManagedKey(project, input, review, csrf)).key.id).toBe(projectKey.id)
    request.mockResolvedValueOnce({ status: 200, data: projectReceipt, headers })
    expect((await getKeyDeliveryReceipt(project, requestId)).key_id).toBe(projectKey.id)
    request.mockResolvedValueOnce({ status: 200, data: projectDescriptor, headers })
    expect((await getKeyDeliveryDescriptor(project, projectKey.id)).key_id).toBe(projectKey.id)
    request.mockResolvedValueOnce({
      status: 200,
      data: { key: projectKey, delivery: projectReceipt },
      headers,
    })
    expect(
      (
        await reconcileManagedKey(
          project,
          projectKey.id,
          { request_id: requestId, reason: input.reason },
          review,
          csrf,
        )
      ).key.id,
    ).toBe(projectKey.id)
    const predecessor = 'pky_01m36yee4gkbns18pfcqqc75a4'
    request.mockResolvedValueOnce({
      status: 201,
      data: { key: { ...projectKey, replaces_key_id: predecessor }, delivery: projectReceipt },
      headers,
    })
    await issueManagedKey(
      project,
      { ...input, name: '', model_ids: [], expires_at: null },
      review,
      csrf,
      undefined,
      predecessor,
    )
    expect(request.mock.calls.map(([arg]) => arg.url)).toEqual([
      '/projects/' + project.projectId + '/keys/managed',
      '/projects/' + project.projectId + '/keys/delivery-receipts/' + requestId,
      '/projects/' + project.projectId + '/keys/' + projectKey.id + '/delivery-descriptor',
      '/projects/' + project.projectId + '/keys/' + projectKey.id + '/delivery-reconcile',
      '/projects/' + project.projectId + '/keys/' + predecessor + '/managed-rotation',
    ])
  })
  it('rejects cross-scope Key prefixes and an empty rotation predecessor before dispatch', async () => {
    const project = { kind: 'project', projectId: identity('prj') } as const
    expect(() => parseKeyDeliveryReceipt(receipt, project)).toThrow()
    expect(() => parseKeyDeliveryDescriptor(descriptor, project)).toThrow()
    await expect(getKeyDeliveryDescriptor(project, identity('key'))).rejects.toThrow()
    await expect(getKeyDeliveryDescriptor(owner, identity('pky'))).rejects.toThrow()
    await expect(
      reconcileManagedKey(
        project,
        identity('key'),
        { request_id: requestId, reason: input.reason },
        review,
        csrf,
      ),
    ).rejects.toThrow()
    await expect(
      issueManagedKey(
        project,
        { ...input, name: '', model_ids: [], expires_at: null },
        review,
        csrf,
        undefined,
        identity('key'),
      ),
    ).rejects.toThrow()
    await expect(
      issueManagedKey(
        owner,
        { ...input, name: '', model_ids: [], expires_at: null },
        review,
        csrf,
        undefined,
        '',
      ),
    ).rejects.toThrow()
    expect(request).not.toHaveBeenCalled()
  })
  it('uses only the owner endpoint and current abort signal for context reads', async () => {
    const signal = new AbortController().signal
    request.mockResolvedValueOnce({ status: 200, data: context, headers: { etag: `"${review}"` } })
    await getKeyDeliveryContext({ kind: 'project', projectId: identity('prj') }, signal)
    expect(request).toHaveBeenCalledWith(
      expect.objectContaining({
        method: 'get',
        url: '/projects/' + identity('prj') + '/keys/delivery-context',
        signal,
        data: undefined,
      }),
    )
  })
  it('rejects an unreviewed or mismatching response ETag', async () => {
    request.mockResolvedValueOnce({ status: 200, data: context, headers: { etag: review } })
    await expect(getKeyDeliveryContext(owner)).rejects.toThrow(KeyDeliveryError)
  })
  it('retains one exact issue intent and never automatically replays failed writes', async () => {
    request.mockRejectedValueOnce(new Error('sensitive upstream detail'))
    await expect(issueManagedKey(owner, input, review, csrf)).rejects.toMatchObject({
      message: 'API-Key delivery request failed',
      status: 0,
    })
    expect(request).toHaveBeenCalledTimes(1)
    expect(request).toHaveBeenCalledWith(
      expect.objectContaining({
        method: 'post',
        url: '/keys/managed',
        data: input,
        headers: { 'If-Match': `"${review}"`, 'X-CSRF-Token': csrf },
      }),
    )
  })
  it('accepts only token-free metadata and the exact issue receipt', async () => {
    request.mockResolvedValueOnce({
      status: 201,
      data: { key, delivery: receipt },
      headers: { etag: `"${review}"` },
    })
    const result = await issueManagedKey(owner, input, review, csrf)
    expect(Object.keys(result)).toEqual(['key', 'delivery'])
    request.mockResolvedValueOnce({
      status: 201,
      data: { key, delivery: { ...receipt, request_id: '20000000-0000-4000-8000-000000000001' } },
      headers: { etag: `"${review}"` },
    })
    await expect(issueManagedKey(owner, input, review, csrf)).rejects.toThrow()
  })
  it('does not accept the receipt of another request', async () => {
    request.mockResolvedValueOnce({ status: 200, data: receipt, headers: { etag: `"${review}"` } })
    await expect(
      getKeyDeliveryReceipt(owner, '20000000-0000-4000-8000-000000000001'),
    ).rejects.toThrow()
  })
  it('rejects unknown write intent fields and malformed identities before dispatch', () => {
    expect(() =>
      saveKeyDeliveryIdentity(
        {
          purpose: 'writer',
          integration_id: identity('vlt'),
          token: 'one-time-input',
          reason: 'Save machine identity',
          extra: true,
        } as never,
        review,
        csrf,
      ),
    ).toThrow()
    expect(() =>
      getKeyDeliveryContext({ kind: 'project', projectId: '../another-owner' }),
    ).toThrow()
    expect(request).not.toHaveBeenCalled()
  })
})
