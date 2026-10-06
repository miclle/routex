import { t } from '@/i18n'
import axios from 'axios'
import client from './client'
import type {
  CallableModel,
  KeyDelivery,
  Model,
  ModelAliasRetirementReview,
  ModelAliasRetirementInput,
  ModelAliasRetirementResult,
  PersonalKey,
  Provider,
  ProviderModel,
} from '@/types/catalog'

export async function listProviders(signal?: AbortSignal) {
  return (await client.get<{ items: Provider[] }>('/admin/providers', { signal })).data.items
}
export async function listAdminModels() {
  return (await client.get<{ items: Model[] }>('/admin/models')).data.items
}
export async function listModels() {
  return (await client.get<{ items: CallableModel[] }>('/models')).data.items
}
export async function listGrantees() {
  return (
    await client.get<{ items: { id: string; email: string; name: string }[] }>(
      '/admin/model-grantees',
    )
  ).data.items
}
export async function listKeys() {
  return (await client.get<{ items: PersonalKey[] }>('/keys')).data.items
}
export async function writeCatalog<T = unknown>(
  method: 'post' | 'put' | 'patch' | 'delete',
  path: string,
  data: unknown,
  csrfToken: string,
): Promise<T> {
  return (
    await client.request<T>({ method, url: path, data, headers: { 'X-CSRF-Token': csrfToken } })
  ).data
}
export async function createKey(
  input: { name: string; model_ids: string[]; expires_at: string | null },
  csrf: string,
) {
  return writeCatalog<KeyDelivery>('post', '/keys', input, csrf)
}
export async function rotateKey(id: string, csrf: string) {
  return writeCatalog<KeyDelivery>('post', `/keys/${id}/rotate`, {}, csrf)
}
export function catalogError(error: unknown) {
  if (axios.isAxiosError(error)) {
    switch (error.response?.status) {
      case 400:
        return t('check_the_entered_information_model_scope_and_routing_4925a')
      case 401:
        return t('your_session_has_expired_sign_in_again_4ae7f')
      case 403:
        return t('you_do_not_have_permission_for_this_action_a6de3')
      case 404:
        return t('the_resource_does_not_exist_or_is_no_1d7d6')
      case 409:
        return t('this_action_conflicts_with_current_state_the_name_7be8e')
      case 422:
        return t('configuration_validation_failed_check_the_connection_credentials_and_536e6')
    }
  }
  return t('the_action_failed_check_the_service_connection_and_65fc1')
}

export async function setProviderModelState(
  id: string,
  input: Pick<ProviderModel, 'etag' | 'enabled' | 'supports_image_input' | 'supports_pdf_input'>,
  csrf: string,
) {
  return writeCatalog<ProviderModel>('patch', `/admin/provider-models/${id}`, input, csrf)
}

export async function getAdminModel(modelID: string, signal?: AbortSignal): Promise<Model> {
  const value = (
    await client.get<unknown>(`/admin/models/${encodeURIComponent(modelID)}`, { signal })
  ).data
  const record = (item: unknown): item is Record<string, unknown> =>
    !!item && typeof item === 'object' && !Array.isArray(item)
  const text = (item: unknown) => typeof item === 'string' && item.length > 0
  const unique = (items: unknown[]) => new Set(items).size === items.length
  if (
    !record(value) ||
    value.id !== modelID ||
    !text(value.name) ||
    !['active', 'disabled', 'archived'].includes(String(value.status)) ||
    !Array.isArray(value.names) ||
    value.names.some(
      (name) =>
        !record(name) ||
        !text(name.name) ||
        typeof name.is_current !== 'boolean' ||
        !(
          name.expires_at === null ||
          (typeof name.expires_at === 'string' && Number.isFinite(Date.parse(name.expires_at)))
        ),
    ) ||
    !unique(value.names.map((name) => name.name)) ||
    !Array.isArray(value.bindings) ||
    value.bindings.some(
      (binding) =>
        !record(binding) ||
        ![
          'id',
          'provider_model_id',
          'provider_id',
          'connection_id',
          'upstream_name',
          'protocol',
        ].every((key) => text(binding[key])) ||
        !Number.isSafeInteger(binding.weight) ||
        Number(binding.weight) < 0 ||
        Number(binding.weight) > 100 ||
        typeof binding.ready !== 'boolean',
    ) ||
    !unique(value.bindings.map((binding) => binding.id)) ||
    !unique(value.bindings.map((binding) => binding.provider_model_id)) ||
    !Array.isArray(value.granted_user_ids) ||
    value.granted_user_ids.some((id) => !text(id)) ||
    !unique(value.granted_user_ids)
  )
    throw new Error('Invalid Model detail response')
  return value as unknown as Model
}

const aliasNamePattern = /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/
const aliasModelPattern = /^mdl_[A-Za-z0-9_-]{1,26}$/
const aliasETagPattern = /^[a-f0-9]{64}$/
function aliasTarget(modelID: string, name: string) {
  if (!aliasModelPattern.test(modelID) || !aliasNamePattern.test(name))
    throw new Error('Invalid Model alias target')
}
export function validModelAliasReason(reason: string) {
  return (
    reason.trim().length > 0 &&
    new TextEncoder().encode(reason).length <= 1024 &&
    ![...reason].some((character) => {
      const code = character.codePointAt(0)!
      return code < 32 || (code >= 127 && code <= 159)
    }) &&
    !/[\uD800-\uDFFF]/u.test(reason)
  )
}
function aliasReview(value: unknown, modelID: string, name: string): ModelAliasRetirementReview {
  const record = (v: unknown): v is Record<string, unknown> =>
    !!v && typeof v === 'object' && !Array.isArray(v)
  const date = (v: unknown) =>
    typeof v === 'string' && /^\d{4}-\d{2}-\d{2}T.+Z$/.test(v) && Number.isFinite(Date.parse(v))
  if (
    !record(value) ||
    Object.keys(value).some(
      (key) =>
        ![
          'model_id',
          'current_name',
          'alias',
          'state',
          'observed_at',
          'etag',
          'can_retire',
          'runtime_applied',
        ].includes(key),
    ) ||
    value.model_id !== modelID ||
    typeof value.current_name !== 'string' ||
    !aliasNamePattern.test(value.current_name) ||
    !record(value.alias) ||
    Object.keys(value.alias).some((key) => !['name', 'is_current', 'expires_at'].includes(key)) ||
    value.alias.name !== name ||
    typeof value.alias.is_current !== 'boolean' ||
    !(value.alias.expires_at === null || date(value.alias.expires_at)) ||
    !['current', 'compatibility', 'retired'].includes(String(value.state)) ||
    (value.state === 'current') !== value.alias.is_current ||
    (value.state === 'current' && value.current_name !== name) ||
    (value.state !== 'current' && value.current_name === name) ||
    (value.state === 'compatibility' && value.alias.expires_at === null) ||
    !date(value.observed_at) ||
    typeof value.etag !== 'string' ||
    !aliasETagPattern.test(value.etag) ||
    typeof value.can_retire !== 'boolean' ||
    (value.can_retire && value.state !== 'compatibility') ||
    typeof value.runtime_applied !== 'boolean'
  )
    throw new Error('Invalid Model alias review response')
  return value as unknown as ModelAliasRetirementReview
}
export async function getModelAliasRetirement(modelID: string, name: string, signal?: AbortSignal) {
  aliasTarget(modelID, name)
  return aliasReview(
    (
      await client.get<unknown>(`/admin/models/${modelID}/alias-retirement`, {
        params: { name },
        signal,
      })
    ).data,
    modelID,
    name,
  )
}
export async function retireModelAlias(
  modelID: string,
  input: ModelAliasRetirementInput,
  etag: string,
  csrf: string,
  signal?: AbortSignal,
): Promise<ModelAliasRetirementResult> {
  aliasTarget(modelID, input.name)
  if (!validModelAliasReason(input.reason) || !aliasETagPattern.test(etag) || !csrf)
    throw new Error('Invalid Model alias retirement intent')
  const value = (
    await client.post<unknown>(`/admin/models/${modelID}/alias-retirement`, input, {
      headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf },
      signal,
    })
  ).data
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Invalid Model alias retirement response')
  const result = value as Record<string, unknown>
  const review = aliasReview(result.alias, modelID, input.name)
  if (
    Object.keys(result).some(
      (key) => !['alias', 'retired', 'changed', 'runtime_applied'].includes(key),
    ) ||
    result.retired !== true ||
    review.state !== 'retired' ||
    typeof result.changed !== 'boolean' ||
    typeof result.runtime_applied !== 'boolean' ||
    review.runtime_applied !== result.runtime_applied
  )
    throw new Error('Invalid Model alias retirement response')
  return {
    alias: review,
    retired: true,
    changed: result.changed,
    runtime_applied: result.runtime_applied,
  }
}
