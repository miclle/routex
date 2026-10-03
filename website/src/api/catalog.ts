import { t } from '@/i18n'
import axios from 'axios'
import client from './client'
import type {
  CallableModel,
  KeyDelivery,
  Model,
  PersonalKey,
  Provider,
  ProviderModel,
} from '@/types/catalog'

export async function listProviders() {
  return (await client.get<{ items: Provider[] }>('/admin/providers')).data.items
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
