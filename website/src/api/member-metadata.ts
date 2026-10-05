import { AxiosHeaders, type AxiosResponse } from 'axios'
import client from './client'
import type {
  MemberMetadataInput,
  MemberMetadataRecord,
  MemberMetadataWriteResult,
} from '@/types/member-metadata'

const identity = (v: unknown) => typeof v === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(v)
const etag = (v: unknown) => typeof v === 'string' && /^[a-f0-9]{64}$/.test(v)
const invalid = () => new Error('Member metadata unavailable')
function responseETag(headers: AxiosResponse['headers']) {
  return headers instanceof AxiosHeaders ? headers.get('ETag') : headers.etag
}
function path(userId: string) {
  if (!identity(userId)) throw invalid()
  return `/admin/members/${encodeURIComponent(userId)}/metadata`
}
const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
function record(v: unknown, userId: string, result: boolean) {
  const fields = [
    'user_id',
    'name',
    'status',
    'can_edit',
    'etag',
    ...(result ? ['confirmation'] : []),
  ]
  return (
    object(v) &&
    Object.keys(v).length === fields.length &&
    fields.every((k) => Object.hasOwn(v, k)) &&
    v.user_id === userId &&
    typeof v.name === 'string' &&
    !/[\p{Cs}]/u.test(v.name) &&
    new TextEncoder().encode(v.name).length <= 65536 &&
    typeof v.status === 'string' &&
    ['active', 'disabled', 'offboarded'].includes(v.status) &&
    typeof v.can_edit === 'boolean' &&
    (v.status !== 'offboarded' || v.can_edit === false) &&
    etag(v.etag) &&
    (!result || (v.can_edit === true && v.confirmation === 'current_member_name'))
  )
}
export function validMemberName(name: string) {
  return (
    !!name &&
    name === name.trim() &&
    Array.from(name).length <= 100 &&
    !/[\p{Cc}\p{Cs}]/u.test(name)
  )
}
export function validMemberMetadataReason(reason: string) {
  return (
    !!reason &&
    reason === reason.trim() &&
    new TextEncoder().encode(reason).length <= 1024 &&
    !/[\p{Cc}\p{Cs}]/u.test(reason)
  )
}
export async function getMemberMetadata(
  userId: string,
  signal?: AbortSignal,
): Promise<MemberMetadataRecord> {
  const response = await client.get(path(userId), { signal })
  if (
    signal?.aborted ||
    response.status !== 200 ||
    !record(response.data, userId, false) ||
    responseETag(response.headers) !== `"${response.data.etag}"`
  )
    throw invalid()
  return response.data as MemberMetadataRecord
}
export async function setMemberMetadata(
  userId: string,
  revision: string,
  input: MemberMetadataInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<MemberMetadataWriteResult> {
  if (
    !etag(revision) ||
    !validMemberName(input.name) ||
    !validMemberMetadataReason(input.reason) ||
    !csrf
  )
    throw invalid()
  const response = await client.put(
    path(userId),
    { name: input.name, reason: input.reason },
    { signal, headers: { 'If-Match': `"${revision}"`, 'X-CSRF-Token': csrf } },
  )
  if (
    signal?.aborted ||
    response.status !== 200 ||
    !record(response.data, userId, true) ||
    response.data.name !== input.name ||
    responseETag(response.headers) !== `"${response.data.etag}"`
  )
    throw invalid()
  return response.data as MemberMetadataWriteResult
}
