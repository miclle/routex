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
  etag: string,
  enabled: boolean,
  csrf: string,
) {
  return writeCatalog<ProviderModel>(
    'patch',
    `/admin/provider-models/${id}`,
    { etag, enabled },
    csrf,
  )
}
