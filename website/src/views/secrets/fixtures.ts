import {
  legacySecretDomains,
  type SecretStore,
  type SecretIntent,
  type SecretResult,
} from '@/types/secrets'
export const rotationId = 'srt_01k00000000000000000000000'
export function store(): SecretStore {
  return {
    inventory_version: 2,
    mode: 'internal',
    observed_at: '2026-10-04T00:00:00Z',
    review_etag: 'a'.repeat(64),
    can_read: true,
    can_rotate: true,
    policy: { write_key_id: 'old', epoch: '9007199254740993' },
    keys: [
      { id: 'old', state: 'write', configured: true, verified: true },
      { id: 'next', state: 'decrypt_only', configured: true, verified: true },
      { id: 'retired', state: 'retired', configured: true, verified: false },
    ],
    process: {
      id: 'instance_current',
      verified: true,
      policy_epoch: '9007199254740993',
      snapshot_id: 'snapshot_current',
      verified_at: '2026-10-04T00:00:00Z',
    },
    rotation: null,
  }
}
export function job(): NonNullable<SecretStore['rotation']> {
  return {
    inventory_version: 1,
    id: rotationId,
    status: 'observing',
    phase: 'observation',
    source_key_id: 'old',
    target_key_id: 'next',
    domains: legacySecretDomains.map((code) => ({
      code,
      coverage: 'observed',
      scanned: '9007199254740993',
      rewrapped: '1',
      already_target: '0',
      deleted: '0',
      changed: '0',
      blocked: '0',
    })),
    blocker_codes: ['observation_pending'],
    observation_started_at: '2026-10-04T00:00:00Z',
    observation_eligible_at: '2026-10-04T00:05:00Z',
    allowed_actions: ['resume', 'rollback'],
  }
}
export function receipt(intent: SecretIntent): SecretResult {
  return {
    receipt: {
      request_id: intent.input.request_id,
      rotation_id: intent.rotationId ?? rotationId,
      action: intent.action,
      created_at: '2026-10-04T00:00:01Z',
    },
    committed: true,
    write_policy_applied: true,
    publication_applied: true,
    application_status: 'applied',
    rotation: job(),
  }
}
