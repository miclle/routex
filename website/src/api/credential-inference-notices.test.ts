import { describe, expect, it } from 'vitest'
import {
  recordedCredentialInference,
  validCredentialInferenceAlert,
} from './credential-inference-notices'

const suffix = '01j00000000000000000000001'
const common = {
  kind: 'credential_inference_failure',
  severity: 'medium',
  detail_code: 'five_consecutive_recorded_failures',
  credential_id: 'CRD_exact_historical',
  occurrence_count: 1,
  first_seen_at: '2026-10-12T01:00:00Z',
  last_seen_at: '2026-10-12T02:00:00Z',
}
const inbox = {
  ...common,
  id: `ntf_${suffix}`,
  alert_id: `alr_${suffix}`,
  read: false,
  read_at: null,
}
const alert = {
  ...common,
  id: `alr_${suffix}`,
  etag: `rev_${suffix}`,
  state: 'open',
  updated_at: '2026-10-12T02:00:00Z',
}

describe('closed recorded Credential inference notice envelopes', () => {
  it('accepts an exact historical non-secret Credential without inventing current catalog attribution', () => {
    expect(recordedCredentialInference(inbox)).toBe(true)
    expect(validCredentialInferenceAlert(alert)).toBe(true)
    expect(
      recordedCredentialInference({ ...inbox, read: true, read_at: common.last_seen_at }),
    ).toBe(true)
    expect(
      validCredentialInferenceAlert({
        ...alert,
        subject_type: 'provider',
        subject_id: 'prv_recorded',
        subject_name: 'Recorded supplier',
      }),
    ).toBe(true)
  })
  it('uses the lowercase ULID runtime format independently of exact Credential casing', () => {
    expect(recordedCredentialInference({ ...inbox, credential_id: 'CRD_Exact_Historical' })).toBe(
      true,
    )
    expect(validCredentialInferenceAlert({ ...alert, credential_id: 'crd_exact_historical' })).toBe(
      true,
    )
    expect(recordedCredentialInference({ ...inbox, id: `ntf_${suffix.toUpperCase()}` })).toBe(false)
    expect(recordedCredentialInference({ ...inbox, alert_id: `alr_${suffix.toUpperCase()}` })).toBe(
      false,
    )
    expect(validCredentialInferenceAlert({ ...alert, id: `alr_${suffix.toUpperCase()}` })).toBe(
      false,
    )
    expect(validCredentialInferenceAlert({ ...alert, etag: `rev_${suffix.toUpperCase()}` })).toBe(
      false,
    )
  })

  it.each(['pending', 'retry', 'sending', 'accepted', 'failed', 'unknown'])(
    'accepts existing bounded %s delivery projection',
    (status) => {
      expect(
        recordedCredentialInference({
          ...inbox,
          delivery_status: status,
          delivery_updated_at: common.last_seen_at,
        }),
      ).toBe(true)
      expect(
        recordedCredentialInference({
          ...inbox,
          delivery_status: status,
          delivery_code: 'accepted',
          delivery_attempts: 1,
          delivery_updated_at: common.last_seen_at,
        }),
      ).toBe(true)
    },
  )
  it.each([
    { credential_id: '' },
    { credential_id: ' crd_exact' },
    { credential_id: 'crd.secret' },
    { credential_id: 'x'.repeat(31) },
    { kind: 'credential_verification_failure' },
    { kind: 'Credential_inference_failure' },
    { severity: 'high' },
    { detail_code: 'verification_failed' },
    { occurrence_count: 0 },
    { occurrence_count: 1.5 },
    { first_seen_at: 'unknown' },
    { first_seen_at: '2026-10-12T03:00:00Z' },
    { first_recorded_model_count: 1 },
    { quota: { limit: '10' } },
    { credential_name: 'current secret name' },
    { secret: 'no' },
    {
      subject_type: 'credential',
      subject_id: 'crd_exact',
      subject_name: 'Fabricated current name',
    },
    { subject_type: 'model', subject_id: 'mdl_one', subject_name: 'Borrowed logical route' },
    { subject_type: 'provider', subject_id: 'prv_one' },
  ])('rejects malformed or borrowed common facts %j', (change) => {
    expect(recordedCredentialInference({ ...inbox, ...change })).toBe(false)
    expect(validCredentialInferenceAlert({ ...alert, ...change })).toBe(false)
  })
  it.each(['\u0000', '\u001f', '\u007f', '\u009f'])(
    'rejects control characters in recorded Provider subjects %j',
    (control) => {
      const subject = {
        subject_type: 'provider',
        subject_id: 'prv_exact',
        subject_name: `Recorded${control}supplier`,
      }
      expect(recordedCredentialInference({ ...inbox, ...subject })).toBe(false)
      expect(validCredentialInferenceAlert({ ...alert, ...subject })).toBe(false)
    },
  )
  it.each([
    { read: true, read_at: null },
    { read: false, read_at: common.last_seen_at },
    { alert_id: 'alr_other' },
    { delivery_status: 'delivered' },
    {
      delivery_status: 'accepted',
      delivery_attempts: -1,
      delivery_updated_at: common.last_seen_at,
    },
    { delivery_code: 'accepted' },
    { delivery_status: 'pending', delivery_updated_at: 'unknown' },
  ])('rejects malformed inbox-only facts %j', (change) => {
    expect(recordedCredentialInference({ ...inbox, ...change })).toBe(false)
  })
  it.each([{ state: 'healthy' }, { etag: 'rev_other' }, { updated_at: 'unknown' }, { read: true }])(
    'rejects malformed alert-only facts %j',
    (change) => {
      expect(validCredentialInferenceAlert({ ...alert, ...change })).toBe(false)
    },
  )
})
