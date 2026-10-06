import { AxiosHeaders, type RawAxiosHeaders } from 'axios'
import client from './client'
import type { ProviderModelBindings } from '@/types/provider-model-bindings'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const fields = (value: Record<string, unknown>, names: string[]) =>
  Object.keys(value).length === names.length && names.every((name) => Object.hasOwn(value, name))
const identity = (value: unknown, prefix: string): value is string =>
  typeof value === 'string' && value.startsWith(prefix) && /^[A-Za-z0-9_-]{1,30}$/.test(value)
const currentName = (value: unknown): value is string =>
  typeof value === 'string' && /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/.test(value)
function invalid(): never {
  throw new Error('Provider Model relationships unavailable')
}

export function validateProviderModelBindings(
  value: unknown,
  providerId: string,
): ProviderModelBindings {
  if (
    !identity(providerId, 'prv_') ||
    !object(value) ||
    !fields(value, ['provider_id', 'items']) ||
    value.provider_id !== providerId ||
    !Array.isArray(value.items) ||
    value.items.length > 10000
  )
    invalid()
  let previous = ''
  let total = 0
  for (const item of value.items) {
    if (
      !object(item) ||
      !fields(item, ['provider_model_id', 'connection_id', 'binding_count', 'models']) ||
      !identity(item.provider_model_id, 'pmd_') ||
      item.provider_model_id <= previous ||
      !identity(item.connection_id, 'con_') ||
      !Number.isSafeInteger(item.binding_count) ||
      typeof item.binding_count !== 'number' ||
      item.binding_count < 0 ||
      item.binding_count > 10000 ||
      !Array.isArray(item.models) ||
      item.models.length !== item.binding_count
    )
      invalid()
    previous = item.provider_model_id
    total += item.models.length
    if (total > 10000) invalid()
    let previousModel = ''
    for (const model of item.models) {
      if (
        !object(model) ||
        !fields(model, ['id', 'name']) ||
        !identity(model.id, 'mdl_') ||
        model.id <= previousModel ||
        !(model.name === null || currentName(model.name))
      )
        invalid()
      previousModel = model.id
    }
  }
  return value as unknown as ProviderModelBindings
}

export async function getProviderModelBindings(providerId: string, signal?: AbortSignal) {
  if (!identity(providerId, 'prv_')) invalid()
  const response = await client.get<unknown>(
    `/admin/providers/${encodeURIComponent(providerId)}/model-bindings`,
    { signal },
  )
  const control = AxiosHeaders.from(response.headers as RawAxiosHeaders).get('cache-control')
  if (typeof control !== 'string' || !/^(?:private,\s*)?no-store$/i.test(control.trim())) invalid()
  return validateProviderModelBindings(response.data, providerId)
}
