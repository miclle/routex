import client from './client'
import { decodeAdminModel } from './catalog'
import {
  routingProtocols,
  type RoutingCandidate,
  type RoutingFilter,
  type RoutingPage,
  type RoutingProtocol,
  type RoutingProvider,
  type RoutingSupply,
  type RoutingBindingInput,
  type RoutingCandidateReview,
} from '@/types/model-routing'
const record = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const id = (v: unknown, prefix: string) =>
  typeof v === 'string' &&
  v.length <= 30 &&
  new RegExp(`^${prefix}_[A-Za-z0-9_-]{1,26}$(?![\\s\\S])`).test(v)
const label = (v: unknown, maximum: number) =>
  typeof v === 'string' &&
  !!v.trim() &&
  new TextEncoder().encode(v).length <= maximum &&
  !/\p{Cc}|[\uD800-\uDFFF]/u.test(v)
const tag = (v: unknown) => typeof v === 'string' && /^[a-f0-9]{64}$(?![\s\S])/.test(v)
const keys = (v: Record<string, unknown>, allowed: string[]) =>
  Object.keys(v).length === allowed.length && Object.keys(v).every((k) => allowed.includes(k))
function invalid(): never {
  throw new Error('Invalid Model routing response')
}
export function decodeRoutingSupply(value: unknown): RoutingSupply {
  if (
    !record(value) ||
    !keys(value, [
      'provider_name',
      'connection_name',
      'verification_covered',
      'configured_available',
    ]) ||
    !label(value.provider_name, 400) ||
    !label(value.connection_name, 400) ||
    typeof value.verification_covered !== 'boolean' ||
    typeof value.configured_available !== 'boolean'
  )
    invalid()
  return value as unknown as RoutingSupply
}
function target(model: string, protocol: RoutingProtocol) {
  if (!id(model, 'mdl') || !routingProtocols.includes(protocol))
    throw new Error('Invalid Model routing target')
}
function filters(value: RoutingFilter, prefix: string) {
  if (
    Object.keys(value).some((k) => !['q', 'cursor', 'limit'].includes(k)) ||
    (value.q !== undefined &&
      (new TextEncoder().encode(value.q).length > 200 ||
        /\p{Cc}|[\uD800-\uDFFF]/u.test(value.q))) ||
    (value.cursor !== undefined && !id(value.cursor, prefix)) ||
    (value.limit !== undefined &&
      (!Number.isSafeInteger(value.limit) || value.limit < 1 || value.limit > 50))
  )
    throw new Error('Invalid Model routing query')
  return value
}
function page<T>(
  value: unknown,
  prefix: string,
  limit: number,
  parse: (v: unknown) => T,
): RoutingPage<T> {
  if (
    !record(value) ||
    !keys(value, ['items', 'next_cursor']) ||
    !Array.isArray(value.items) ||
    value.items.length > limit ||
    !(value.next_cursor === null || id(value.next_cursor, prefix))
  )
    invalid()
  const items = value.items.map(parse)
  const ids = items.map((v) => (v as { id: string }).id)
  if (
    new Set(ids).size !== ids.length ||
    (value.next_cursor !== null && (ids.length !== limit || value.next_cursor !== ids.at(-1)))
  )
    invalid()
  return { items, next_cursor: value.next_cursor as string | null }
}
export async function listRoutingProviders(
  model: string,
  protocol: RoutingProtocol,
  filter: RoutingFilter = {},
  signal?: AbortSignal,
) {
  target(model, protocol)
  filters(filter, 'prv')
  const response = await client.get<unknown>(`/admin/models/${model}/routing-providers`, {
    params: { protocol, ...filter },
    signal,
  })
  return page<RoutingProvider>(response.data, 'prv', filter.limit ?? 20, (v) => {
    if (!record(v) || !keys(v, ['id', 'name']) || !id(v.id, 'prv') || !label(v.name, 400)) invalid()
    return v as unknown as RoutingProvider
  })
}
export async function listRoutingCandidates(
  model: string,
  protocol: RoutingProtocol,
  provider: string,
  filter: RoutingFilter = {},
  signal?: AbortSignal,
) {
  target(model, protocol)
  filters(filter, 'pmd')
  if (!id(provider, 'prv')) throw new Error('Invalid routing Provider')
  const response = await client.get<unknown>(`/admin/models/${model}/routing-candidates`, {
    params: { protocol, provider_id: provider, ...filter },
    signal,
  })
  return page<RoutingCandidate>(response.data, 'pmd', filter.limit ?? 20, (v) =>
    decodeRoutingCandidate(v, protocol, provider),
  )
}
function decodeRoutingCandidate(v: unknown, protocol: RoutingProtocol, provider: string) {
  if (
    !record(v) ||
    !keys(v, [
      'id',
      'provider_id',
      'connection_id',
      'upstream_name',
      'protocol',
      'provider_name',
      'connection_name',
      'verification_covered',
      'configured_available',
      'selectable',
      'review_etag',
      'prices',
    ]) ||
    !id(v.id, 'pmd') ||
    v.provider_id !== provider ||
    !id(v.connection_id, 'con') ||
    !label(v.upstream_name, 1020) ||
    v.protocol !== protocol ||
    !tag(v.review_etag) ||
    typeof v.selectable !== 'boolean'
  )
    invalid()
  decodeRoutingSupply(
    Object.fromEntries(
      ['provider_name', 'connection_name', 'verification_covered', 'configured_available'].map(
        (k) => [k, v[k]],
      ),
    ),
  )
  if (v.selectable && (!v.verification_covered || !v.configured_available)) invalid()
  if (v.prices !== null) {
    if (!record(v.prices) || !keys(v.prices, ['input', 'output'])) invalid()
    for (const side of ['input', 'output']) {
      const rates = v.prices[side]
      if (
        !Array.isArray(rates) ||
        rates.length > 5000 ||
        rates.some(
          (r) =>
            !record(r) ||
            !keys(r, ['amount', 'currency', 'enabled']) ||
            typeof r.amount !== 'string' ||
            r.amount.length > 40 ||
            !/^(0|[1-9][0-9]*)(\.[0-9]{1,18})?$(?![\s\S])/.test(r.amount) ||
            typeof r.currency !== 'string' ||
            !/^[A-Z]{3}$(?![\s\S])/.test(r.currency) ||
            typeof r.enabled !== 'boolean',
        ) ||
        new Set(rates.map((r) => r.currency)).size !== rates.length
      )
        invalid()
    }
  }
  return v as unknown as RoutingCandidate
}
export async function getRoutingCandidate(
  model: string,
  protocol: RoutingProtocol,
  provider: string,
  providerModel: string,
  signal?: AbortSignal,
) {
  target(model, protocol)
  if (!id(provider, 'prv') || !id(providerModel, 'pmd'))
    throw new Error('Invalid exact routing source')
  const response = await client.get<unknown>(
    `/admin/models/${model}/routing-candidates/${providerModel}`,
    { params: { protocol, provider_id: provider }, signal },
  )
  const value = response.data
  if (
    response.status !== 200 ||
    !record(value) ||
    !keys(value, ['model_id', 'model_name', 'candidate', 'routes']) ||
    value.model_id !== model ||
    !label(value.model_name, 128) ||
    !Array.isArray(value.routes) ||
    value.routes.length > 200
  )
    invalid()
  const candidate = decodeRoutingCandidate(value.candidate, protocol, provider)
  if (candidate.id !== providerModel) invalid()
  for (const route of value.routes) {
    if (
      !record(route) ||
      !keys(route, [
        'id',
        'provider_id',
        'provider_name',
        'connection_id',
        'connection_name',
        'provider_model_id',
        'upstream_name',
        'protocol',
        'weight',
      ]) ||
      !id(route.id, 'bnd') ||
      !id(route.provider_id, 'prv') ||
      !id(route.connection_id, 'con') ||
      !id(route.provider_model_id, 'pmd') ||
      !label(route.provider_name, 400) ||
      !label(route.connection_name, 400) ||
      !label(route.upstream_name, 1020) ||
      !routingProtocols.includes(route.protocol as RoutingProtocol) ||
      !Number.isSafeInteger(route.weight) ||
      Number(route.weight) < 0 ||
      Number(route.weight) > 100
    )
      invalid()
  }
  if (
    new Set(value.routes.map((route) => route.id)).size !== value.routes.length ||
    new Set(value.routes.map((route) => route.provider_model_id)).size !== value.routes.length ||
    value.routes.some(
      (route) =>
        route.provider_model_id === providerModel ||
        (route.provider_id === provider && route.protocol === protocol),
    )
  )
    invalid()
  return { ...value, candidate } as unknown as RoutingCandidateReview
}

export async function addRoutingBinding(
  model: string,
  input: RoutingBindingInput,
  csrf: string,
  signal?: AbortSignal,
) {
  target(model, input.protocol)
  if (!id(input.provider_model_id, 'pmd') || !tag(input.review_etag) || !csrf)
    throw new Error('Invalid Model routing insertion')
  const response = await client.post<unknown>(`/admin/models/${model}/bindings`, input, {
    headers: { 'X-CSRF-Token': csrf },
    signal,
  })
  const result = decodeAdminModel(response.data, model)
  if (
    response.status !== 201 ||
    result.bindings.filter(
      (b) =>
        b.provider_model_id === input.provider_model_id &&
        b.protocol === input.protocol &&
        b.weight === 0,
    ).length !== 1
  )
    invalid()
  return result
}

export interface RoutingWeightReview {
  model: import('@/types/catalog').Model
  review_etag: string
  runtime_applied: boolean
}

function decodeWeightReview(value: unknown, model: string, etag: unknown): RoutingWeightReview {
  if (
    !record(value) ||
    !keys(value, ['model', 'review_etag', 'runtime_applied']) ||
    !tag(value.review_etag) ||
    etag !== `"${value.review_etag}"` ||
    typeof value.runtime_applied !== 'boolean'
  )
    invalid()
  const detail = decodeAdminModel(value.model, model)
  if (
    detail.bindings.length > 200 ||
    detail.bindings.some(
      (binding) =>
        !id(binding.id, 'bnd') ||
        !id(binding.provider_model_id, 'pmd') ||
        !id(binding.provider_id, 'prv') ||
        !id(binding.connection_id, 'con') ||
        !routingProtocols.includes(binding.protocol as RoutingProtocol),
    )
  )
    invalid()
  return {
    model: detail,
    review_etag: value.review_etag as string,
    runtime_applied: value.runtime_applied,
  }
}

export async function getRoutingWeightReview(model: string, signal?: AbortSignal) {
  if (!id(model, 'mdl')) throw new Error('Invalid Model weight review target')
  const response = await client.get<unknown>(`/admin/models/${model}/weights/review`, { signal })
  if (response.status !== 200) invalid()
  return decodeWeightReview(response.data, model, response.headers.etag)
}

export async function saveReviewedRoutingWeights(
  model: string,
  weights: { binding_id: string; weight: number }[],
  reviewETag: string,
  csrf: string,
  signal?: AbortSignal,
) {
  if (
    !id(model, 'mdl') ||
    !tag(reviewETag) ||
    !csrf ||
    !weights.length ||
    weights.length > 200 ||
    new Set(weights.map((row) => row.binding_id)).size !== weights.length ||
    weights.some(
      (row) =>
        !id(row.binding_id, 'bnd') ||
        !Number.isInteger(row.weight) ||
        row.weight < 0 ||
        row.weight > 100,
    )
  )
    throw new Error('Invalid reviewed Model weights')
  const response = await client.put<unknown>(
    `/admin/models/${model}/weights/reviewed`,
    { weights },
    {
      headers: { 'X-CSRF-Token': csrf, 'If-Match': `"${reviewETag}"` },
      signal,
    },
  )
  if (response.status !== 200) invalid()
  const result = decodeWeightReview(response.data, model, response.headers.etag)
  if (
    result.model.bindings.length !== weights.length ||
    result.model.bindings.some(
      (binding) =>
        !weights.some((row) => row.binding_id === binding.id && row.weight === binding.weight),
    )
  )
    invalid()
  return result
}
