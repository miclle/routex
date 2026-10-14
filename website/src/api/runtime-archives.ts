import client from './client'
import type {
  RuntimeArchiveMetadataFilter,
  RuntimeArchiveMetadataPage,
  RuntimeArchiveValidation,
} from '@/types/runtime-archives'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const keys = (value: Record<string, unknown>, expected: string[]) =>
  Object.keys(value).sort().join(',') === [...expected].sort().join(',')
const cursor = (value: unknown): value is string =>
  typeof value === 'string' && /^[A-Za-z0-9_-]{1,200}$(?![\s\S])/.test(value)
const utc = (value: unknown, precision: number) =>
  typeof value === 'string' &&
  new RegExp(
    `^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}(?:\\.\\d{1,${precision}})?Z$(?![\\s\\S])`,
  ).test(value) &&
  Number.isFinite(Date.parse(value)) &&
  new Date(value).toISOString().slice(0, 19) === value.slice(0, 19)
function invalid(): never {
  throw new Error('Invalid configuration archive metadata')
}

export async function validateRuntimeArchive(
  archiveID: string,
  signal?: AbortSignal,
): Promise<RuntimeArchiveValidation> {
  if (
    typeof archiveID !== 'string' ||
    !/^rcv_[0-7][0-9a-hjkmnp-tv-z]{25}$(?![\s\S])/.test(archiveID)
  )
    invalid()
  const response = await client.get<unknown>(`/admin/runtime/archives/${archiveID}/validation`, {
    signal,
  })
  const value = response.data
  if (
    response.status !== 200 ||
    !object(value) ||
    !keys(value, [
      'scope',
      'archive_id',
      'archive_created_at',
      'codec_version',
      'observed_at',
      'integrity',
    ]) ||
    value.scope !== 'archive_integrity_only' ||
    value.archive_id !== archiveID ||
    !utc(value.archive_created_at, 6) ||
    value.codec_version !== 1 ||
    !utc(value.observed_at, 9) ||
    value.integrity !== 'authenticated'
  )
    invalid()
  return value as unknown as RuntimeArchiveValidation
}

export async function getRuntimeArchives(
  filter: RuntimeArchiveMetadataFilter = {},
  signal?: AbortSignal,
): Promise<RuntimeArchiveMetadataPage> {
  if (
    Object.keys(filter).some((key) => !['cursor', 'limit'].includes(key)) ||
    (filter.cursor !== undefined && !cursor(filter.cursor)) ||
    (filter.limit !== undefined &&
      (!Number.isSafeInteger(filter.limit) || filter.limit < 1 || filter.limit > 100))
  )
    invalid()
  const response = await client.get<unknown>('/admin/runtime/archives', { params: filter, signal })
  const value = response.data
  if (
    response.status !== 200 ||
    !object(value) ||
    !keys(value, ['scope', 'observed_at', 'items', 'next_cursor']) ||
    value.scope !== 'archive_metadata_only' ||
    !utc(value.observed_at, 9) ||
    !Array.isArray(value.items) ||
    value.items.length > (filter.limit ?? 20) ||
    !(value.next_cursor === null || cursor(value.next_cursor)) ||
    (value.next_cursor !== null &&
      (value.next_cursor === filter.cursor || value.items.length === 0))
  )
    invalid()
  let prior = ''
  for (const row of value.items) {
    if (
      !object(row) ||
      !keys(row, ['id', 'created_at', 'codec_version', 'content_digest', 'document_bytes']) ||
      typeof row.id !== 'string' ||
      !/^rcv_[0-7][0-9a-hjkmnp-tv-z]{25}$(?![\s\S])/.test(row.id) ||
      (prior !== '' && row.id >= prior) ||
      !utc(row.created_at, 6) ||
      row.codec_version !== 1 ||
      typeof row.content_digest !== 'string' ||
      !/^[a-f0-9]{64}$(?![\s\S])/.test(row.content_digest) ||
      typeof row.document_bytes !== 'number' ||
      !Number.isSafeInteger(row.document_bytes) ||
      row.document_bytes < 1 ||
      row.document_bytes > 16777216
    )
      invalid()
    prior = row.id
  }
  return value as unknown as RuntimeArchiveMetadataPage
}
