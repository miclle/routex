import { AxiosHeaders } from 'axios'
import client from './client'
import { validVaultName } from './vault-integrations'
import type {
  CredentialStorageContext,
  CredentialStorageSource,
  ProviderStorageInput,
  ProviderStoragePolicy,
} from '@/types/provider-storage'

export class ProviderStorageError extends Error {
  constructor() {
    super('Provider credential storage response unavailable')
  }
}
const etag = /^[a-f0-9]{64}$/
const integration = /^vlt_[0-7][0-9a-hjkmnp-tv-z]{25}$/
const revision = /^vlr_[0-7][0-9a-hjkmnp-tv-z]{25}$/
function object(value: unknown, fields: string[]): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new ProviderStorageError()
  const row = value as Record<string, unknown>
  if (Object.keys(row).length !== fields.length || fields.some((key) => !Object.hasOwn(row, key)))
    throw new ProviderStorageError()
  return row
}
export function parseCredentialStorageSource(value: unknown): CredentialStorageSource {
  if (value !== 'inline' && value !== 'vault') throw new ProviderStorageError()
  return value
}
function review(value: unknown, header: unknown): string {
  if (typeof value !== 'string' || !etag.test(value) || header !== `"${value}"`)
    throw new ProviderStorageError()
  return value
}
export function parseProviderStoragePolicy(value: unknown, header: unknown): ProviderStoragePolicy {
  const row = object(value, [
    'mode',
    'integration_id',
    'revision_id',
    'choices',
    'etag',
    'can_edit',
  ])
  const mode = parseCredentialStorageSource(row.mode)
  if (
    typeof row.can_edit !== 'boolean' ||
    !Array.isArray(row.choices) ||
    row.choices.length > 100 ||
    (mode === 'inline'
      ? row.integration_id !== null || row.revision_id !== null
      : typeof row.integration_id !== 'string' ||
        !integration.test(row.integration_id) ||
        typeof row.revision_id !== 'string' ||
        !revision.test(row.revision_id))
  )
    throw new ProviderStorageError()
  const seen = new Set<string>()
  const choices = row.choices.map((value) => {
    const item = object(value, ['id', 'name', 'birth', 'revision_id'])
    if (
      typeof item.id !== 'string' ||
      !integration.test(item.id) ||
      seen.has(item.id) ||
      typeof item.revision_id !== 'string' ||
      !revision.test(item.revision_id) ||
      typeof item.name !== 'string' ||
      !validVaultName(item.name) ||
      typeof item.birth !== 'string' ||
      !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(
        item.birth,
      ) ||
      !Number.isFinite(Date.parse(item.birth)) ||
      item.birth.startsWith('0001-')
    )
      throw new ProviderStorageError()
    seen.add(item.id)
    return { id: item.id, name: item.name, birth: item.birth, revision_id: item.revision_id }
  })
  return {
    mode,
    integration_id: row.integration_id as string | null,
    revision_id: row.revision_id as string | null,
    choices,
    etag: review(row.etag, header),
    can_edit: row.can_edit,
  }
}
export function parseCredentialStorageContext(
  value: unknown,
  header: unknown,
): CredentialStorageContext {
  const row = object(value, ['storage_source', 'etag'])
  return {
    storage_source: parseCredentialStorageSource(row.storage_source),
    etag: review(row.etag, header),
  }
}
export async function getProviderStoragePolicy(signal?: AbortSignal) {
  const result = await client.get<unknown>('/admin/secrets/provider-storage', { signal })
  if (result.status !== 200) throw new ProviderStorageError()
  return parseProviderStoragePolicy(
    result.data,
    result.headers instanceof AxiosHeaders ? result.headers.get('ETag') : result.headers.etag,
  )
}
export async function saveProviderStoragePolicy(
  input: ProviderStorageInput,
  reviewed: string,
  csrf: string,
  signal?: AbortSignal,
) {
  const result = await client.put<unknown>('/admin/secrets/provider-storage', input, {
    signal,
    headers: { 'If-Match': `"${reviewed}"`, 'X-CSRF-Token': csrf },
  })
  if (result.status !== 200) throw new ProviderStorageError()
  return parseProviderStoragePolicy(
    result.data,
    result.headers instanceof AxiosHeaders ? result.headers.get('ETag') : result.headers.etag,
  )
}
export async function getCredentialStorageContext(signal?: AbortSignal) {
  const result = await client.get<unknown>('/admin/provider-credential-storage-context', { signal })
  if (result.status !== 200) throw new ProviderStorageError()
  return parseCredentialStorageContext(
    result.data,
    result.headers instanceof AxiosHeaders ? result.headers.get('ETag') : result.headers.etag,
  )
}
