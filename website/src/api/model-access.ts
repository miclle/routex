import { isAxiosError } from 'axios'
import client from './client'
import { decodeConnectionTransport } from './connection-transport'
import { getCredentialStorageContext } from './provider-storage'
import type { CredentialStorageSource } from '@/types/provider-storage'
import type {
  ModelAccessCredential,
  ModelAccessEgress,
  ModelAccessInput,
  ModelAccessProviderPage,
  ModelAccessPickerFilter,
  ModelAccessSaved,
} from '@/types/model-access'

// Raw Axios errors retain the plaintext request config. Only this status-only
// error may leave the direct transport boundary, including decoder failures.
export class ModelAccessError extends Error {
  constructor(public readonly status = 0) {
    super('Model access operation unavailable')
  }
}
const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const resource = (v: unknown, prefix: string): v is string =>
  typeof v === 'string' && new RegExp(`^${prefix}_[A-Za-z0-9_-]{1,26}$(?![\\s\\S])`).test(v)
const label = (v: unknown): v is string =>
  typeof v === 'string' &&
  !!v.trim() &&
  Array.from(v).length <= 100 &&
  !/\p{Cc}/u.test(v) &&
  new TextDecoder().decode(new TextEncoder().encode(v)) === v
const uuid = (v: string) =>
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$(?![\s\S])/.test(v)
const token = (v: unknown) => typeof v === 'string' && /^[a-f0-9]{64}$(?![\s\S])/.test(v)
const protocols = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_generate_content',
]
async function safe<T>(operation: () => Promise<T>): Promise<T> {
  try {
    return await operation()
  } catch (error) {
    if (error instanceof ModelAccessError) throw error
    throw new ModelAccessError(isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
function invalid(): never {
  throw new ModelAccessError()
}
export function validateModelAccessInput(input: ModelAccessInput, providerId?: string) {
  if (
    !uuid(input.request_id) ||
    !token(input.storage_policy_etag) ||
    !label(input.name) ||
    !label(input.credential_name) ||
    (!providerId && !label(input.connection_name)) ||
    (providerId && (!resource(providerId, 'prv') || input.connection_name !== undefined)) ||
    !protocols.includes(input.protocol) ||
    typeof input.secret !== 'string' ||
    !input.secret ||
    new TextEncoder().encode(input.secret).length > 2048 ||
    /[\r\n]/.test(input.secret) ||
    new TextDecoder().decode(new TextEncoder().encode(input.secret)) !== input.secret ||
    !['default', 'direct', 'proxy'].includes(input.egress_mode) ||
    (input.egress_mode === 'proxy' ? !resource(input.egress_id, 'egr') : input.egress_id !== null)
  )
    invalid()
  decodeConnectionTransport(input)
  const url = new URL(input.base_url)
  if (
    !['http:', 'https:'].includes(url.protocol) ||
    url.username ||
    url.password ||
    url.search ||
    url.hash
  )
    invalid()
}
export const readModelAccessStorage = (signal?: AbortSignal) =>
  safe(() => getCredentialStorageContext(signal))
function accessPickerParams(filter: ModelAccessPickerFilter, prefix: 'prv' | 'egr') {
  if (
    Object.keys(filter).some((key) => !['q', 'cursor', 'limit', 'exact_id'].includes(key)) ||
    (filter.exact_id !== undefined &&
      (!resource(filter.exact_id, prefix) ||
        filter.q !== undefined ||
        filter.cursor !== undefined)) ||
    (filter.cursor !== undefined && !resource(filter.cursor, prefix)) ||
    (filter.limit !== undefined &&
      (!Number.isSafeInteger(filter.limit) || filter.limit < 1 || filter.limit > 50)) ||
    (filter.q !== undefined &&
      (typeof filter.q !== 'string' ||
        new TextEncoder().encode(filter.q).length > 200 ||
        new TextDecoder().decode(new TextEncoder().encode(filter.q)) !== filter.q))
  )
    invalid()
  // Empty search is the initial UI state, not an explicitly empty HTTP q.
  return {
    ...(filter.q ? { q: filter.q } : {}),
    ...(filter.cursor ? { cursor: filter.cursor } : {}),
    ...(filter.limit !== undefined ? { limit: filter.limit } : {}),
    ...(filter.exact_id !== undefined ? { exact_id: filter.exact_id } : {}),
  }
}
export function listModelAccessProviders(
  filter: ModelAccessPickerFilter,
  signal?: AbortSignal,
): Promise<ModelAccessProviderPage> {
  return safe(async () => {
    const response = await client.get<unknown>('/admin/model-creation/providers', {
      params: accessPickerParams(filter, 'prv'),
      signal,
    })
    const value = response.data
    if (
      response.status !== 200 ||
      !object(value) ||
      Object.keys(value).sort().join(',') !== 'items,next_cursor' ||
      !Array.isArray(value.items) ||
      value.items.length > (filter.exact_id ? 1 : (filter.limit ?? 20)) ||
      (value.next_cursor !== null && !resource(value.next_cursor, 'prv'))
    )
      invalid()
    const items = value.items.map((v) => {
      if (
        !object(v) ||
        Object.keys(v).sort().join(',') !== 'id,name' ||
        !resource(v.id, 'prv') ||
        !label(v.name)
      )
        invalid()
      return { id: v.id, name: v.name }
    })
    if (
      new Set(items.map((v) => v.id)).size !== items.length ||
      (filter.exact_id !== undefined &&
        (items.some((item) => item.id !== filter.exact_id) || value.next_cursor !== null)) ||
      (value.next_cursor !== null && items.at(-1)?.id !== value.next_cursor)
    )
      invalid()
    return { items, next_cursor: value.next_cursor as string | null }
  })
}
export const readModelAccessEgress = (
  filter: ModelAccessPickerFilter,
  signal?: AbortSignal,
): Promise<{ items: ModelAccessEgress[]; next_cursor: string | null }> =>
  safe(async () => {
    const response = await client.get<unknown>('/admin/model-creation/egresses', {
      params: accessPickerParams(filter, 'egr'),
      signal,
    })
    if (
      response.status !== 200 ||
      !object(response.data) ||
      Object.keys(response.data).sort().join(',') !== 'items,next_cursor' ||
      !Array.isArray(response.data.items) ||
      response.data.items.length > (filter.exact_id ? 1 : (filter.limit ?? 20)) ||
      (response.data.next_cursor !== null && !resource(response.data.next_cursor, 'egr'))
    )
      invalid()
    const items = response.data.items.map((v) => {
      if (
        !object(v) ||
        Object.keys(v).sort().join(',') !== 'enabled,id,name' ||
        !resource(v.id, 'egr') ||
        !label(v.name) ||
        typeof v.enabled !== 'boolean'
      )
        invalid()
      return { id: v.id, name: v.name, enabled: v.enabled }
    })
    if (
      new Set(items.map((v) => v.id)).size !== items.length ||
      (filter.exact_id !== undefined &&
        (items.some((item) => item.id !== filter.exact_id) ||
          response.data.next_cursor !== null)) ||
      (response.data.next_cursor !== null && items.at(-1)?.id !== response.data.next_cursor)
    )
      invalid()
    return { items, next_cursor: response.data.next_cursor as string | null }
  })
export function createModelAccess(
  input: ModelAccessInput,
  source: CredentialStorageSource,
  csrf: string,
  provider?: { id: string; name: string },
  signal?: AbortSignal,
): Promise<ModelAccessSaved> {
  return safe(async () => {
    validateModelAccessInput(input, provider?.id)
    const response = await client.post<unknown>(
      provider ? `/admin/providers/${provider.id}/connections` : '/admin/providers',
      input,
      { signal, headers: { 'X-CSRF-Token': csrf } },
    )
    let connection: unknown = response.data
    let providerId = provider?.id,
      providerName = provider?.name
    if (!provider) {
      if (
        !object(response.data) ||
        !resource(response.data.id, 'prv') ||
        response.data.name !== input.name ||
        !Array.isArray(response.data.connections) ||
        response.data.connections.length !== 1
      )
        invalid()
      providerId = response.data.id
      providerName = response.data.name as string
      connection = response.data.connections[0]
    }
    if (
      response.status !== 201 ||
      !object(connection) ||
      !resource(connection.id, 'con') ||
      connection.name !== (provider ? input.name : input.connection_name) ||
      connection.protocol !== input.protocol ||
      typeof connection.enabled !== 'boolean' ||
      !Array.isArray(connection.credentials) ||
      connection.credentials.length !== 1 ||
      !Array.isArray(connection.provider_models) ||
      connection.provider_models.length !== 0 ||
      typeof connection.base_url !== 'string' ||
      connection.base_url !== input.base_url.replace(/\/+$/, '')
    )
      invalid()
    decodeConnectionTransport(connection)
    if (
      connection.adapter !== input.adapter ||
      connection.api_version !== input.api_version ||
      connection.egress_mode !== input.egress_mode ||
      connection.egress_id !== input.egress_id
    )
      invalid()
    const credential = connection.credentials[0]
    if (
      !object(credential) ||
      !resource(credential.id, 'crd') ||
      credential.name !== input.credential_name ||
      credential.storage_source !== source ||
      typeof credential.enabled !== 'boolean' ||
      typeof credential.verification_status !== 'string' ||
      !['pending', 'verified', 'failed'].includes(credential.verification_status) ||
      credential.replaces_credential_id !== null
    )
      invalid()
    return {
      provider_id: providerId!,
      provider_name: providerName!,
      connection_id: connection.id,
      connection_name: connection.name as string,
      credential_id: credential.id,
      credential_name: credential.name as string,
      connection_enabled: connection.enabled,
      protocol: input.protocol,
      adapter: input.adapter,
      api_version: input.api_version,
      base_url: connection.base_url,
      storage_source: source,
    }
  })
}
export const readModelAccessCredential = (
  saved: ModelAccessSaved,
  signal?: AbortSignal,
): Promise<ModelAccessCredential> =>
  safe(async () => {
    if (!resource(saved.credential_id, 'crd') || !resource(saved.connection_id, 'con')) invalid()
    const response = await client.get<unknown>(
      `/admin/credentials/${saved.credential_id}/metadata`,
      { signal },
    )
    const v = response.data
    if (
      response.status !== 200 ||
      !object(v) ||
      v.id !== saved.credential_id ||
      v.connection_id !== saved.connection_id ||
      typeof v.enabled !== 'boolean' ||
      typeof v.verification_status !== 'string' ||
      !['pending', 'verified', 'failed'].includes(v.verification_status) ||
      !token(v.etag) ||
      (v.verified_at !== null &&
        (typeof v.verified_at !== 'string' || !Number.isFinite(Date.parse(v.verified_at))))
    )
      invalid()
    return {
      id: v.id as string,
      connection_id: v.connection_id as string,
      enabled: v.enabled,
      verification_status: v.verification_status as ModelAccessCredential['verification_status'],
      verified_at: v.verified_at as string | null,
    }
  })
export const verifyModelAccess = (
  saved: ModelAccessSaved,
  csrf: string,
  signal?: AbortSignal,
): Promise<boolean> =>
  safe(async () => {
    if (!resource(saved.credential_id, 'crd') || !resource(saved.connection_id, 'con')) invalid()
    const response = await client.post<unknown>(
      `/admin/credentials/${saved.credential_id}/verify`,
      {},
      { signal, headers: { 'X-CSRF-Token': csrf } },
    )
    if (
      response.status !== 200 ||
      !object(response.data) ||
      typeof response.data.verified !== 'boolean' ||
      !Number.isSafeInteger(response.data.discovered_models) ||
      (response.data.discovered_models as number) < 0
    )
      invalid()
    return response.data.verified
  })
export const enableModelAccess = (
  saved: ModelAccessSaved,
  csrf: string,
  signal?: AbortSignal,
): Promise<void> =>
  safe(async () => {
    if (!resource(saved.credential_id, 'crd') || !resource(saved.connection_id, 'con')) invalid()
    const response = await client.patch<unknown>(
      `/admin/credentials/${saved.credential_id}`,
      { enabled: true },
      { signal, headers: { 'X-CSRF-Token': csrf } },
    )
    const v = response.data
    if (
      response.status !== 200 ||
      !object(v) ||
      v.id !== saved.credential_id ||
      v.enabled !== true ||
      v.verification_status !== 'verified' ||
      v.storage_source !== saved.storage_source
    )
      invalid()
  })
