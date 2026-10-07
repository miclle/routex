import { isAxiosError } from 'axios'
import client from './client'
import { decodeConnectionTransport } from './connection-transport'
import type {
  ModelCreationConnection,
  ModelCreationContext,
  ModelCreationFilter,
  ModelCreationInput,
  ModelCreationItem,
  ModelCreationPage,
  ModelCreationPreview,
  ModelCreationProviderModel,
  ModelCreationReceiptItem,
  ModelCreationResult,
  ModelCreationTarget,
} from '@/types/model-creation'
const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const text = (v: unknown): v is string => typeof v === 'string'
const resource = (v: unknown, prefix: string): v is string =>
  text(v) && !/[\r\n]/.test(v) && v.length <= 30 && new RegExp(`^${prefix}_[A-Za-z0-9_-]+$`).test(v)
const uuid = (v: unknown): v is string =>
  text(v) &&
  !/[\r\n]/.test(v) &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(v)
const etag = (v: unknown): v is string => text(v) && !/[\r\n]/.test(v) && /^[0-9a-f]{64}$/.test(v)
const time = (v: unknown) => text(v) && Number.isFinite(Date.parse(v))
const bool = (v: unknown) => typeof v === 'boolean'
const protocol = (v: unknown) =>
  text(v) &&
  ['openai_chat', 'openai_responses', 'anthropic_messages', 'gemini_generate_content'].includes(v)
const weight = (v: unknown) => v === 0 || v === 100
const codes = (v: unknown): v is string[] =>
  Array.isArray(v) &&
  v.length <= 64 &&
  v.every((x) => text(x) && !/[\r\n]/.test(x) && /^[a-z][a-z0-9_]{0,63}$/.test(x)) &&
  new Set(v).size === v.length
function invalid(): never {
  throw new Error('Invalid model creation response')
}
export const validModelCreationName = (v: string) =>
  /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$(?![\s\S])/.test(v)
export const validModelCreationReason = (v: string) =>
  !!v.trim() &&
  new TextDecoder().decode(new TextEncoder().encode(v)) === v &&
  new TextEncoder().encode(v).length <= 1024 &&
  !/\p{Cc}/u.test(v)
export const modelCreationOutcomeUnknown = (error: unknown) =>
  !isAxiosError(error) || !error.response || error.response.status >= 500
function connection(v: unknown, expected?: string): ModelCreationConnection {
  if (
    !object(v) ||
    !resource(v.id, 'con') ||
    (expected && v.id !== expected) ||
    !resource(v.provider_id, 'prv') ||
    !text(v.provider_name) ||
    !text(v.name) ||
    !protocol(v.protocol) ||
    !text(v.base_url)
  )
    invalid()
  if (Object.keys(v).length !== 8) invalid()
  decodeConnectionTransport(v)
  return v as unknown as ModelCreationConnection
}
function providerModel(v: unknown): ModelCreationProviderModel {
  if (
    !object(v) ||
    !resource(v.id, 'pmd') ||
    !text(v.upstream_name) ||
    !bool(v.disabled) ||
    !bool(v.credential_ready) ||
    !bool(v.selectable) ||
    !codes(v.blocker_codes) ||
    !Array.isArray(v.input_capabilities) ||
    v.input_capabilities.some((x) => !text(x) || !['image', 'pdf'].includes(x)) ||
    new Set(v.input_capabilities).size !== v.input_capabilities.length ||
    (v.selectable && (v.disabled || !v.credential_ready || v.blocker_codes.length))
  )
    invalid()
  return v as unknown as ModelCreationProviderModel
}
function target(v: unknown): ModelCreationTarget {
  if (
    !object(v) ||
    !resource(v.id, 'mdl') ||
    !text(v.name) ||
    !validModelCreationName(v.name) ||
    !weight(v.initial_weight) ||
    !bool(v.selectable) ||
    !codes(v.blocker_codes) ||
    (v.selectable && v.blocker_codes.length)
  )
    invalid()
  return v as unknown as ModelCreationTarget
}
function filters(f: ModelCreationFilter, prefix: string) {
  if (
    Object.keys(f).some((key) => !['q', 'cursor', 'limit'].includes(key)) ||
    (f.q !== undefined && (!f.q || new TextEncoder().encode(f.q).length > 200)) ||
    (f.cursor !== undefined && !resource(f.cursor, prefix)) ||
    (f.limit !== undefined && (!Number.isSafeInteger(f.limit) || f.limit < 1 || f.limit > 50))
  )
    throw new Error('Invalid model creation query')
  return f
}
async function page<T>(
  url: string,
  f: ModelCreationFilter,
  prefix: string,
  parse: (v: unknown) => T,
  signal?: AbortSignal,
): Promise<ModelCreationPage<T>> {
  const v: unknown = (await client.get(url, { params: filters(f, prefix), signal })).data
  if (
    !object(v) ||
    !Array.isArray(v.items) ||
    v.items.length > (f.limit ?? 20) ||
    !(v.next_cursor === null || resource(v.next_cursor, prefix)) ||
    (v.next_cursor !== null && v.next_cursor === f.cursor)
  )
    invalid()
  const items = v.items.map((x) => parse(x))
  if (new Set(items.map((x) => (x as { id: string }).id)).size !== items.length) invalid()
  return { items, next_cursor: v.next_cursor as string | null }
}
function path(id: string) {
  if (!resource(id, 'con')) throw new Error('Invalid Connection identity')
  return `/admin/connections/${id}/model-creation`
}
export const listModelCreationConnections = (f: ModelCreationFilter = {}, signal?: AbortSignal) =>
  page('/admin/model-creation/connections', f, 'con', connection, signal)
export async function getModelCreationContext(
  id: string,
  signal?: AbortSignal,
): Promise<ModelCreationContext> {
  const v: unknown = (await client.get(path(id), { signal })).data
  if (!object(v) || !bool(v.can_create) || !time(v.observed_at)) invalid()
  return {
    connection: connection(v.connection, id),
    can_create: v.can_create as boolean,
    observed_at: v.observed_at as string,
  }
}
export const listModelCreationProviderModels = (
  id: string,
  f: ModelCreationFilter = {},
  signal?: AbortSignal,
) => page(`${path(id)}/provider-models`, f, 'pmd', providerModel, signal)
export const listModelCreationTargets = (
  id: string,
  f: ModelCreationFilter = {},
  signal?: AbortSignal,
) => page(`${path(id)}/models`, f, 'mdl', target, signal)
export function validateModelCreationItems(items: ModelCreationItem[]) {
  if (
    items.length < 1 ||
    items.length > 50 ||
    new Set(items.map((x) => x.provider_model_id)).size !== items.length
  )
    throw new Error('Invalid model creation selection')
  const names = new Set<string>(),
    models = new Set<string>()
  for (const x of items) {
    if (
      !resource(x.provider_model_id, 'pmd') ||
      (x.target === 'new'
        ? !validModelCreationName(x.name) ||
          names.has(x.name) ||
          Object.keys(x).sort().join(',') !== 'name,provider_model_id,target'
        : x.target !== 'existing' ||
          !resource(x.model_id, 'mdl') ||
          models.has(x.model_id) ||
          Object.keys(x).sort().join(',') !== 'model_id,provider_model_id,target')
    )
      throw new Error('Invalid model creation selection')
    if (x.target === 'new') names.add(x.name)
    else models.add(x.model_id)
  }
}
export async function previewModelCreation(
  id: string,
  items: ModelCreationItem[],
  csrf: string,
  signal?: AbortSignal,
): Promise<ModelCreationPreview> {
  validateModelCreationItems(items)
  if (!csrf) throw new Error('Invalid preview authority')
  const v: unknown = (
    await client.post(
      `${path(id)}/preview`,
      { items },
      { signal, headers: { 'X-CSRF-Token': csrf } },
    )
  ).data
  if (
    !object(v) ||
    !etag(v.review_etag) ||
    !time(v.observed_at) ||
    !bool(v.can_commit) ||
    !Array.isArray(v.items) ||
    v.items.length !== items.length
  )
    invalid()
  const c = connection(v.connection, id)
  const seen = new Set<string>()
  for (const row of v.items) {
    if (
      !object(row) ||
      !resource(row.provider_model_id, 'pmd') ||
      !text(row.upstream_name) ||
      !text(row.name) ||
      !protocol(row.protocol) ||
      row.protocol !== c.protocol ||
      !weight(row.initial_weight) ||
      !codes(row.blocker_codes) ||
      seen.has(row.provider_model_id)
    )
      invalid()
    const original = items.find((x) => x.provider_model_id === row.provider_model_id)
    if (
      !original ||
      row.target !== original.target ||
      (original.target === 'new'
        ? row.name !== original.name || row.model_id !== null || row.initial_weight !== 100
        : row.model_id !== original.model_id)
    )
      invalid()
    if (v.can_commit && row.blocker_codes.length) invalid()
    seen.add(row.provider_model_id)
  }
  return v as unknown as ModelCreationPreview
}
function receiptItem(v: unknown, current = false): ModelCreationReceiptItem {
  if (
    !object(v) ||
    !resource(v.provider_model_id, 'pmd') ||
    !resource(v.model_id, 'mdl') ||
    !resource(v.binding_id, 'bnd') ||
    !bool(v.created_model) ||
    !text(v.name) ||
    !validModelCreationName(v.name) ||
    !protocol(v.protocol) ||
    !(current
      ? Number.isSafeInteger(v.weight) && Number(v.weight) >= 0 && Number(v.weight) <= 100
      : weight(v.weight)) ||
    (!current && v.created_model && v.weight !== 100)
  )
    invalid()
  return v as unknown as ModelCreationReceiptItem
}
function result(
  v: unknown,
  requestId: string,
  connectionId?: string,
  input?: ModelCreationInput,
): ModelCreationResult {
  if (
    !object(v) ||
    !object(v.receipt) ||
    v.receipt.request_id !== requestId ||
    !uuid(v.receipt.request_id) ||
    !resource(v.receipt.connection_id, 'con') ||
    (connectionId && v.receipt.connection_id !== connectionId) ||
    !time(v.receipt.created_at) ||
    !Array.isArray(v.receipt.items) ||
    v.receipt.items.length < 1 ||
    v.receipt.items.length > 50 ||
    v.committed !== true ||
    !bool(v.changed) ||
    !bool(v.runtime_applied) ||
    !text(v.application_status) ||
    !['pending', 'applied', 'superseded', 'unavailable'].includes(v.application_status) ||
    v.runtime_applied !== (v.application_status === 'applied') ||
    !(v.current_items === null || Array.isArray(v.current_items)) ||
    (v.current_items === null) !== (v.application_status === 'unavailable')
  )
    invalid()
  const rows = v.receipt.items.map((x) => receiptItem(x))
  if (
    new Set(rows.map((x) => x.provider_model_id)).size !== rows.length ||
    new Set(rows.map((x) => x.model_id)).size !== rows.length ||
    new Set(rows.map((x) => x.binding_id)).size !== rows.length
  )
    invalid()
  if (
    input &&
    (rows.length !== input.items.length ||
      rows.some((row) => {
        const original = input.items.find((x) => x.provider_model_id === row.provider_model_id)
        return (
          !original ||
          (original.target === 'new'
            ? !row.created_model || row.name !== original.name
            : row.created_model || row.model_id !== original.model_id)
        )
      }))
  )
    invalid()
  if (v.current_items !== null) {
    const current = v.current_items.map((x) => receiptItem(x, true))
    if (
      current.length !== rows.length ||
      current.some(
        (row) =>
          !rows.some(
            (old) =>
              old.provider_model_id === row.provider_model_id &&
              old.model_id === row.model_id &&
              old.binding_id === row.binding_id &&
              old.created_model === row.created_model &&
              old.protocol === row.protocol,
          ),
      )
    )
      invalid()
    if (
      new Set(current.map((x) => x.binding_id)).size !== rows.length ||
      (v.application_status === 'applied' &&
        current.some((x, i) => x.name !== rows[i].name || x.weight !== rows[i].weight))
    )
      invalid()
  }
  return v as unknown as ModelCreationResult
}
export async function createModelBatch(
  id: string,
  input: ModelCreationInput,
  review: string,
  csrf: string,
  signal?: AbortSignal,
) {
  validateModelCreationItems(input.items)
  if (
    Object.keys(input).sort().join(',') !== 'items,reason,request_id' ||
    !uuid(input.request_id) ||
    !validModelCreationReason(input.reason) ||
    !etag(review) ||
    !csrf ||
    new TextEncoder().encode(JSON.stringify(input)).length > 32768
  )
    throw new Error('Invalid model creation intent')
  const response = await client.post(path(id), input, {
    signal,
    headers: { 'X-CSRF-Token': csrf, 'If-Match': `"${review}"` },
  })
  if (![200, 201].includes(response.status)) invalid()
  return result(response.data, input.request_id, id, input)
}
export async function getModelCreationReceipt(
  requestId: string,
  signal?: AbortSignal,
  connectionId?: string,
  input?: ModelCreationInput,
) {
  if (!uuid(requestId)) throw new Error('Invalid model creation request identity')
  const response = await client.get(`/admin/model-creation/receipts/${requestId}`, { signal })
  if (response.status !== 200) invalid()
  return result(response.data, requestId, connectionId, input)
}
