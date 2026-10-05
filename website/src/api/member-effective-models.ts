import client from './client'
import type {
  MemberEffectiveModelsAuthority,
  MemberEffectiveModelsPage,
} from '@/types/member-effective-models'
import { currencies } from '@/types/pricing'

const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const identity = (v: unknown): v is string =>
  typeof v === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(v)
const stamp = (v: unknown): v is string =>
  typeof v === 'string' && /^\d{4}-\d\d-\d\dT.*Z$/.test(v) && Number.isFinite(Date.parse(v))
const text = (v: unknown): v is string =>
  typeof v === 'string' &&
  !!v &&
  v === v.trim() &&
  [...v].length <= 100 &&
  !/[\p{Cc}\p{Cs}]/u.test(v)
const fields = (v: Record<string, unknown>, names: string[]) =>
  Object.keys(v).length === names.length && names.every((name) => Object.hasOwn(v, name))
const protocols = [
  'anthropic_messages',
  'gemini_generate_content',
  'openai_chat',
  'openai_responses',
]
const ordered = (v: unknown[]): v is string[] =>
  v.every((x, i) => typeof x === 'string' && (i === 0 || (v[i - 1] as string) < x))
function ready(v: Record<string, unknown>) {
  return (
    Array.isArray(v.protocols) &&
    ordered(v.protocols) &&
    v.protocols.every((p) => protocols.includes(p)) &&
    ['ready', 'unavailable', 'unknown'].includes(v.availability as string) &&
    (v.availability === 'ready' ? v.protocols.length > 0 : v.protocols.length === 0)
  )
}
function price(v: unknown, authorized: boolean) {
  if (!object(v) || !fields(v, ['state', 'rate'])) return false
  if (!authorized) return v.state === 'unauthorized' && v.rate === null
  if (v.state === 'priced' || v.state === 'disabled')
    return (
      object(v.rate) &&
      fields(v.rate, ['amount', 'unit', 'currency']) &&
      typeof v.rate.amount === 'string' &&
      /^(0|[1-9]\d{0,17})(\.\d{0,17}[1-9])?$/.test(v.rate.amount) &&
      v.rate.unit === '1M_TOKEN' &&
      currencies.includes(v.rate.currency as (typeof currencies)[number])
    )
  return ['unavailable', 'missing', 'heterogeneous'].includes(v.state as string) && v.rate === null
}
function invalid(): never {
  throw new Error('Invalid Member effective models response')
}
export function validateMemberEffectiveModels(
  data: unknown,
  target: string,
  authority: MemberEffectiveModelsAuthority,
): MemberEffectiveModelsPage {
  if (
    !object(data) ||
    !fields(data, [
      'user_id',
      'observed_at',
      'subject_status',
      'team_enrichment',
      'union_completeness',
      'items',
    ]) ||
    !identity(target) ||
    data.user_id !== target ||
    !stamp(data.observed_at) ||
    !['active', 'disabled', 'offboarded'].includes(data.subject_status as string) ||
    data.team_enrichment !== (authority.teams ? 'included' : 'not_authorized') ||
    data.union_completeness !== (authority.teams ? 'complete' : 'unknown') ||
    !Array.isArray(data.items) ||
    data.items.length > 1000
  )
    invalid()
  const modelIDs: string[] = [],
    teamIDs = new Set<string>()
  let sourceCount = 0
  for (const item of data.items) {
    if (
      !object(item) ||
      !fields(item, [
        'id',
        'name',
        'status',
        'type',
        'providers',
        'protocols',
        'availability',
        'input_price',
        'output_price',
        'created_at',
        'updated_at',
        'sources',
      ]) ||
      !identity(item.id) ||
      typeof item.name !== 'string' ||
      !/^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/.test(item.name) ||
      !['active', 'disabled', 'archived'].includes(item.status as string) ||
      item.type !== null ||
      item.updated_at !== null ||
      !ready(item) ||
      !stamp(item.created_at) ||
      Date.parse(item.created_at) > Date.parse(data.observed_at) ||
      (authority.providers
        ? !(
            Array.isArray(item.providers) &&
            item.providers.length <= 5000 &&
            item.providers.every(text) &&
            new Set(item.providers).size === item.providers.length
          )
        : item.providers !== null) ||
      !price(item.input_price, authority.prices) ||
      !price(item.output_price, authority.prices) ||
      !Array.isArray(item.sources) ||
      item.sources.length < 1 ||
      item.sources.length > 101
    )
      invalid()
    modelIDs.push(item.id)
    const sourceKeys: string[] = [],
      union = new Set<string>()
    let availability = 'unavailable'
    for (const source of item.sources) {
      if (
        !object(source) ||
        !fields(source, ['kind', 'team_id', 'team_name', 'protocols', 'availability']) ||
        !ready(source)
      )
        invalid()
      if (source.kind === 'personal') {
        if (source.team_id !== null || source.team_name !== null) invalid()
        sourceKeys.push('0')
      } else if (
        source.kind === 'team' &&
        authority.teams &&
        identity(source.team_id) &&
        text(source.team_name)
      ) {
        sourceKeys.push('1' + source.team_id)
        teamIDs.add(source.team_id)
      } else invalid()
      sourceCount++
      if (
        (data.subject_status !== 'active' || item.status !== 'active') &&
        source.availability !== 'unavailable'
      )
        invalid()
      if (source.availability === 'ready') availability = 'ready'
      else if (source.availability === 'unknown' && availability !== 'ready')
        availability = 'unknown'
      for (const p of source.protocols as string[]) union.add(p)
    }
    if (
      !ordered(sourceKeys) ||
      item.availability !== availability ||
      JSON.stringify(item.protocols) !== JSON.stringify([...union].sort())
    )
      invalid()
    if (
      item.availability !== 'ready' &&
      authority.prices &&
      ((item.input_price as Record<string, unknown>).state !== 'unavailable' ||
        (item.output_price as Record<string, unknown>).state !== 'unavailable')
    )
      invalid()
  }
  if (!ordered(modelIDs) || teamIDs.size > 100 || sourceCount > 6000) invalid()
  return data as unknown as MemberEffectiveModelsPage
}
export const memberEffectiveModelsKey = (
  actor: string,
  target: string,
  generation: number,
  authority: string,
) => ['admin', 'member-effective-models', actor, target, generation, authority] as const
export async function getMemberEffectiveModels(
  target: string,
  authority: MemberEffectiveModelsAuthority,
  signal?: AbortSignal,
) {
  if (!identity(target)) invalid()
  const result = await client.get(`/admin/members/${encodeURIComponent(target)}/effective-models`, {
    signal,
  })
  if (result.headers['cache-control'] !== 'private, no-store') invalid()
  return validateMemberEffectiveModels(result.data, target, authority)
}
