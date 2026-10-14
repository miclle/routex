import axios from 'axios'
import client from './client'
import type {
  RuntimeArchiveCleanupInput,
  RuntimeArchiveCleanupPreview,
  RuntimeArchiveCleanupReceipt,
} from '@/types/runtime-archive-cleanup'

export class RuntimeArchiveCleanupError extends Error {
  constructor(public readonly status: number) {
    super('Configuration archive cleanup failed')
  }
}

function record(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}
function exactKeys(value: Record<string, unknown>, keys: string[]) {
  return Object.keys(value).sort().join(',') === [...keys].sort().join(',')
}
function count(value: unknown, minimum = 0): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= minimum
}
function selection(ids: string[]) {
  return (
    ids.length >= 1 &&
    ids.length <= 20 &&
    ids.every(
      (id, i) =>
        /^rcv_[0-7][0-9a-hjkmnp-tv-z]{25}$(?![\s\S])/.test(id) && (i === 0 || id > ids[i - 1]),
    )
  )
}
function matches(value: unknown, ids: string[]) {
  return (
    Array.isArray(value) && value.length === ids.length && value.every((id, i) => id === ids[i])
  )
}
function fail(): never {
  throw new RuntimeArchiveCleanupError(0)
}
function safeError(error: unknown): never {
  if (error instanceof RuntimeArchiveCleanupError) throw error
  throw new RuntimeArchiveCleanupError(
    axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0,
  )
}

export function validRuntimeArchiveCleanupReason(value: string): boolean {
  return (
    value.length > 0 &&
    !/^\p{White_Space}|\p{White_Space}$/u.test(value) &&
    Array.from(value).length <= 1000 &&
    !/[\p{Cc}\uD800-\uDFFF]/u.test(value)
  )
}

export async function previewRuntimeArchiveCleanup(
  ids: string[],
  csrf: string,
  signal?: AbortSignal,
): Promise<RuntimeArchiveCleanupPreview> {
  if (!selection(ids) || !csrf) fail()
  try {
    const response = await client.post<unknown>(
      '/admin/runtime/archives/cleanup/preview',
      { archive_ids: ids },
      { signal, headers: { 'X-CSRF-Token': csrf } },
    )
    const v = response.data
    const etag = response.headers.etag
    if (
      response.status !== 200 ||
      typeof etag !== 'string' ||
      !/^"[a-f0-9]{64}"$(?![\s\S])/.test(etag) ||
      !record(v) ||
      !exactKeys(v, [
        'scope',
        'archive_ids',
        'retained_count',
        'encrypted_bytes',
        'selected_count',
        'selected_encrypted_bytes',
        'version_cap',
        'encrypted_byte_cap',
      ]) ||
      v.scope !== 'archive_storage_only' ||
      !matches(v.archive_ids, ids) ||
      v.selected_count !== ids.length ||
      !count(v.retained_count, ids.length) ||
      !count(v.encrypted_bytes, 1) ||
      !count(v.selected_encrypted_bytes, 1) ||
      v.selected_encrypted_bytes > v.encrypted_bytes ||
      v.version_cap !== 100 ||
      v.encrypted_byte_cap !== 268435456
    )
      fail()
    return { ...v, etag: etag.slice(1, -1) } as unknown as RuntimeArchiveCleanupPreview
  } catch (error) {
    return safeError(error)
  }
}

export async function cleanupRuntimeArchives(
  input: RuntimeArchiveCleanupInput,
  etag: string,
  csrf: string,
  signal?: AbortSignal,
): Promise<RuntimeArchiveCleanupReceipt> {
  if (
    !selection(input.archive_ids) ||
    !/^[a-f0-9]{64}$(?![\s\S])/.test(etag) ||
    !csrf ||
    !/^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$(?![\s\S])/.test(
      input.request_id,
    ) ||
    !validRuntimeArchiveCleanupReason(input.reason)
  )
    fail()
  try {
    const response = await client.post<unknown>('/admin/runtime/archives/cleanup', input, {
      signal,
      headers: { 'X-CSRF-Token': csrf, 'If-Match': `"${etag}"` },
    })
    const v = response.data
    if (
      response.status !== 200 ||
      !record(v) ||
      !exactKeys(v, [
        'scope',
        'request_id',
        'archive_ids',
        'deleted_count',
        'deleted_encrypted_bytes',
        'completed_at',
      ]) ||
      v.scope !== 'archive_storage_only' ||
      v.request_id !== input.request_id ||
      !matches(v.archive_ids, input.archive_ids) ||
      v.deleted_count !== input.archive_ids.length ||
      !count(v.deleted_encrypted_bytes, 1) ||
      typeof v.completed_at !== 'string' ||
      !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,6})?Z$(?![\s\S])/.test(v.completed_at) ||
      !Number.isFinite(Date.parse(v.completed_at)) ||
      new Date(v.completed_at).toISOString().slice(0, 19) !== v.completed_at.slice(0, 19)
    )
      fail()
    return v as unknown as RuntimeArchiveCleanupReceipt
  } catch (error) {
    return safeError(error)
  }
}
