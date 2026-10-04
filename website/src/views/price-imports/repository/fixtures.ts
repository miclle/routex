import type {
  RepositoryConfig,
  RepositoryIntent,
  RepositoryPreview,
  RepositoryResult,
  RepositorySelection,
} from '@/types/repository-prices'
export const etag = 'a'.repeat(64),
  digest = 'b'.repeat(64),
  previewDigest = 'c'.repeat(64),
  requestId = '11111111-1111-4111-8111-111111111111'
export function configuration(): RepositoryConfig {
  return {
    review_etag: etag,
    enabled: true,
    can_write: true,
    source: {
      id: 'routex-repository',
      digest,
      model_count: 1,
      models: [
        {
          key: 'provider/model',
          provider_key: 'provider',
          model: 'Repository model',
          protocol: 'openai_chat',
          context_threshold: 0,
        },
      ],
    },
    mappings: [{ provider_model_id: 'pmo_one', source_model_key: 'provider/model' }],
    last_attempt_at: null,
    last_success_at: null,
    last_result: null,
  }
}
export const selection: RepositorySelection = {
  mode: 'sync',
  provider_model_ids: ['pmo_one'],
  rate_ids: [],
}
export function intent(): RepositoryIntent {
  return {
    kind: 'configure',
    etag,
    sourceDigest: digest,
    input: {
      request_id: requestId,
      enabled: true,
      mappings: configuration().mappings,
      reason: 'Explicit review',
    },
  }
}
export function differences(selected: RepositorySelection = selection): RepositoryPreview {
  const rate = {
    id: 'rat_one',
    metric: 'INPUT_TOKEN' as const,
    tier: 'base' as const,
    unit: '1M_TOKEN' as const,
    currency: 'USD' as const,
    amount: '0',
    enabled: false,
  }
  return {
    review_etag: etag,
    source_digest: digest,
    preview_digest: previewDigest,
    mode: selected.mode,
    valid: true,
    changes: [
      {
        provider_model_id: selected.provider_model_ids[0],
        rate_id: 'rat_one',
        source_model_key: 'provider/model',
        source_rate_key: 'input_base',
        action: 'updated',
        before: rate,
        after: { ...rate, amount: '1.234567890123456789', enabled: true },
        before_source: { kind: 'custom', source_model_key: null, source_rate_key: null },
        after_source: {
          kind: 'repository',
          source_model_key: 'provider/model',
          source_rate_key: 'input_base',
        },
        threshold_before: 0,
        threshold_after: 0,
      },
    ],
    errors: [],
    warnings: [],
  }
}
export function result(captured: RepositoryIntent = intent()): RepositoryResult {
  return {
    receipt: {
      request_id: captured.input.request_id,
      source_digest: captured.sourceDigest,
      mode: captured.kind === 'configure' ? 'configure' : captured.input.selection.mode,
      created_at: '2026-10-04T00:00:00Z',
    },
    committed: true,
    runtime_applied: captured.kind === 'apply',
    configuration_applied: captured.kind === 'configure',
    application_status: 'applied',
    configuration: configuration(),
  }
}
