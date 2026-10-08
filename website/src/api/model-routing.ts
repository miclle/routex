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
  return page<RoutingCandidate>(response.data, 'pmd', filter.limit ?? 20, (v) => {
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
  })
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
