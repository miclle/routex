import client from './client'
import type {
  MemberModelRow,
  MemberModelsWorkspace,
  MemberModelsWriteInput,
  MemberModelsWriteResult,
} from '@/types/member-models'
import { currencies } from '@/types/pricing'

const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const identity = (v: unknown): v is string =>
  typeof v === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(v)
const etag = (v: unknown): v is string => typeof v === 'string' && /^[0-9a-f]{64}$/.test(v)
const stamp = (v: unknown): v is string =>
  typeof v === 'string' && /^\d{4}-\d\d-\d\dT.*Z$/.test(v) && Number.isFinite(Date.parse(v))
const text = (v: unknown): v is string =>
  typeof v === 'string' &&
  !!v &&
  v === v.trim() &&
  [...v].length <= 100 &&
  !/[\p{Cc}\p{Cs}]/u.test(v)
const modelName = (v: unknown): v is string =>
  typeof v === 'string' && /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/.test(v)
const decimal = (v: unknown) =>
  typeof v === 'string' && /^(0|[1-9]\d{0,17})(\.\d{0,17}[1-9])?$/.test(v)
const protocols = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_generate_content',
]
function price(v: unknown) {
  if (!object(v)) return false
  if (v.state === 'priced' || v.state === 'disabled')
    return (
      object(v.rate) &&
      decimal(v.rate.amount) &&
      v.rate.unit === '1M_TOKEN' &&
      currencies.includes(v.rate.currency as (typeof currencies)[number])
    )
  return (
    ['unauthorized', 'unavailable', 'missing', 'heterogeneous'].includes(v.state as string) &&
    v.rate === null
  )
}
function row(
  v: unknown,
  available: boolean,
  canEdit: boolean,
  observed: string,
): v is MemberModelRow {
  return (
    object(v) &&
    identity(v.id) &&
    modelName(v.name) &&
    ['active', 'disabled', 'archived'].includes(v.status as string) &&
    v.type === null &&
    v.updated_at === null &&
    (v.providers === null ||
      (Array.isArray(v.providers) &&
        v.providers.length <= 5000 &&
        v.providers.every(text) &&
        new Set(v.providers).size === v.providers.length)) &&
    Array.isArray(v.protocols) &&
    v.protocols.every((p) => protocols.includes(p)) &&
    new Set(v.protocols).size === v.protocols.length &&
    ['ready', 'unavailable', 'unknown'].includes(v.availability as string) &&
    (v.availability === 'ready'
      ? v.status === 'active' && v.protocols.length > 0
      : v.protocols.length === 0) &&
    price(v.input_price) &&
    price(v.output_price) &&
    stamp(v.created_at) &&
    Date.parse(v.created_at) <= Date.parse(observed) &&
    v.selectable === (available && canEdit && v.availability === 'ready') &&
    (!available || v.status === 'active')
  )
}
function invalid(): never {
  throw new Error('Invalid Member Models response')
}
export function validateMemberModels(data: unknown, target: string): MemberModelsWorkspace {
  if (
    !object(data) ||
    data.user_id !== target ||
    !identity(data.user_id) ||
    !stamp(data.observed_at) ||
    !etag(data.etag) ||
    typeof data.can_edit !== 'boolean' ||
    !Array.isArray(data.personal_models) ||
    !Array.isArray(data.available_models) ||
    data.personal_models.length + data.available_models.length > 1000 ||
    !data.personal_models.every((v) =>
      row(v, false, data.can_edit as boolean, data.observed_at as string),
    ) ||
    !data.available_models.every((v) =>
      row(v, true, data.can_edit as boolean, data.observed_at as string),
    ) ||
    (!data.can_edit && data.available_models.length !== 0) ||
    !(
      (data.application_status === 'applied' && data.runtime_applied === true) ||
      (data.application_status === 'not_applied' && data.runtime_applied === false) ||
      (data.application_status === 'unavailable' && data.runtime_applied === null)
    )
  )
    invalid()
  const ids = [...data.personal_models, ...data.available_models].map((v) => v.id)
  if (
    new Set(ids).size !== ids.length ||
    [data.personal_models, data.available_models].some((rows) =>
      rows.some((r, i) => i > 0 && rows[i - 1].id >= r.id),
    )
  )
    invalid()
  return data as unknown as MemberModelsWorkspace
}
export function validMemberModelReason(reason: string) {
  return (
    !!reason &&
    reason === reason.trim() &&
    new TextEncoder().encode(reason).length <= 1024 &&
    !/[\p{Cc}\p{Cs}]/u.test(reason)
  )
}
export function validateMemberModelsWrite(
  data: unknown,
  target: string,
  expected: string[],
): MemberModelsWriteResult {
  if (
    !object(data) ||
    data.user_id !== target ||
    !etag(data.etag) ||
    data.runtime_applied !== true ||
    data.confirmation !== 'current_model_grants' ||
    !Array.isArray(data.model_ids) ||
    data.model_ids.length > 1000 ||
    !data.model_ids.every(identity) ||
    JSON.stringify(data.model_ids) !== JSON.stringify([...expected].sort())
  )
    invalid()
  return data as unknown as MemberModelsWriteResult
}
export async function getMemberModels(target: string, signal?: AbortSignal) {
  if (!identity(target)) invalid()
  const r = await client.get(`/admin/members/${encodeURIComponent(target)}/models`, { signal })
  const data = validateMemberModels(r.data, target)
  if (r.headers.etag !== `"${data.etag}"`) invalid()
  return data
}
export async function setMemberModels(
  target: string,
  review: string,
  input: MemberModelsWriteInput,
  csrf: string,
  signal?: AbortSignal,
) {
  if (
    !identity(target) ||
    !etag(review) ||
    !csrf ||
    !validMemberModelReason(input.reason) ||
    !Array.isArray(input.model_ids) ||
    input.model_ids.length > 1000 ||
    !input.model_ids.every(identity) ||
    new Set(input.model_ids).size !== input.model_ids.length
  )
    invalid()
  const r = await client.put(`/admin/members/${encodeURIComponent(target)}/models`, input, {
    signal,
    headers: { 'If-Match': `"${review}"`, 'X-CSRF-Token': csrf },
  })
  const data = validateMemberModelsWrite(r.data, target, input.model_ids)
  if (r.headers.etag !== `"${data.etag}"`) invalid()
  return data
}
