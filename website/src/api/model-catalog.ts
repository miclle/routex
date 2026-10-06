import axios from 'axios'
import { t } from '@/i18n'
import type {
  ModelAccessSource,
  ModelCatalogRecord,
  ModelInputCapability,
} from '@/types/model-catalog'
import type { MemberModelPriceCell } from '@/types/member-models'
import { currencies } from '@/types/pricing'
import client from './client'
import { catalogError } from './catalog'

function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Invalid model catalogue response')
  return value as Record<string, unknown>
}
function text(value: unknown, maximum: number) {
  if (
    typeof value !== 'string' ||
    !value ||
    new TextEncoder().encode(value).length > maximum ||
    Array.from(value).some(
      (character) => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127,
    )
  )
    throw new Error('Invalid model catalogue response')
  return value
}
function identity(value: unknown) {
  const id = text(value, 30)
  if (!/^[A-Za-z0-9_-]+$/.test(id)) throw new Error('Invalid model catalogue response')
  return id
}
function protocols(value: unknown) {
  if (
    !Array.isArray(value) ||
    value.length > 64 ||
    value.some((item) => typeof item !== 'string' || !/^[a-z][a-z0-9_]{0,63}$/.test(item))
  )
    throw new Error('Invalid model catalogue response')
  return value as string[]
}
function source(value: unknown): ModelAccessSource {
  const item = object(value)
  if (
    item.type === 'personal' &&
    item.team_id === null &&
    item.team_name === null &&
    item.invocation_supported === true
  )
    return { type: 'personal', team_id: null, team_name: null, invocation_supported: true }
  if (item.type === 'team' && item.invocation_supported === false)
    return {
      type: 'team',
      team_id: identity(item.team_id),
      team_name: text(item.team_name, 512),
      invocation_supported: false,
      ...(item.invocation_protocols === undefined
        ? {}
        : { invocation_protocols: protocols(item.invocation_protocols) }),
    }
  throw new Error('Invalid model catalogue response')
}
function price(value: unknown): MemberModelPriceCell {
  const cell = object(value)
  if (Object.keys(cell).length !== 2 || !('state' in cell) || !('rate' in cell))
    throw new Error('Invalid model catalogue response')
  if (cell.state === 'priced' || cell.state === 'disabled') {
    const rate = object(cell.rate)
    if (
      Object.keys(rate).length !== 3 ||
      typeof rate.amount !== 'string' ||
      !/^(0|[1-9]\d{0,17})(\.\d{0,17}[1-9])?$/.test(rate.amount) ||
      rate.unit !== '1M_TOKEN' ||
      !currencies.includes(rate.currency as (typeof currencies)[number])
    )
      throw new Error('Invalid model catalogue response')
    return {
      state: cell.state,
      rate: {
        amount: rate.amount,
        unit: '1M_TOKEN',
        currency: rate.currency as (typeof currencies)[number],
      },
    }
  }
  if (
    (cell.state === 'unauthorized' ||
      cell.state === 'unavailable' ||
      cell.state === 'missing' ||
      cell.state === 'heterogeneous') &&
    cell.rate === null
  )
    return { state: cell.state, rate: null }
  throw new Error('Invalid model catalogue response')
}
function record(value: unknown): ModelCatalogRecord {
  const item = object(value)
  if (
    item.status !== 'active' ||
    typeof item.personal_available !== 'boolean' ||
    !Array.isArray(item.sources) ||
    item.sources.length > 101
  )
    throw new Error('Invalid model catalogue response')
  const sources = item.sources.map(source)
  const keys = sources.map((item) =>
    item.type === 'personal' ? 'personal' : `team:${item.team_id}`,
  )
  if (new Set(keys).size !== keys.length) throw new Error('Invalid model catalogue response')
  const capabilities: Record<string, ModelInputCapability[]> = {}
  const entries = Object.entries(object(item.input_capabilities))
  if (entries.length > 64) throw new Error('Invalid model catalogue response')
  for (const [key, value] of entries) {
    protocols([key])
    if (
      !Array.isArray(value) ||
      value.length > 2 ||
      value.some((capability) => capability !== 'image' && capability !== 'pdf')
    )
      throw new Error('Invalid model catalogue response')
    Object.defineProperty(capabilities, key, {
      value: value as ModelInputCapability[],
      enumerable: true,
    })
  }
  const created = text(item.created_at, 64)
  if (!Number.isFinite(Date.parse(created))) throw new Error('Invalid model catalogue response')
  return {
    id: identity(item.id),
    name: text(item.name, 512),
    status: 'active',
    created_at: created,
    protocols: protocols(item.protocols),
    input_capabilities: capabilities,
    input_price: price(item.input_price),
    output_price: price(item.output_price),
    personal_available: item.personal_available,
    sources,
  }
}
export async function listModelCatalog(signal?: AbortSignal) {
  const data = object((await client.get<unknown>('/model-catalog', { signal })).data)
  if (!Array.isArray(data.items) || data.items.length > 1000)
    throw new Error('Invalid model catalogue response')
  const items = data.items.map(record)
  if (new Set(items.map((item) => item.id)).size !== items.length)
    throw new Error('Invalid model catalogue response')
  return items
}
export async function getModelCatalogRecord(id: string, signal?: AbortSignal) {
  identity(id)
  const item = record(
    (await client.get<unknown>(`/model-catalog/${encodeURIComponent(id)}`, { signal })).data,
  )
  if (item.id !== id) throw new Error('Unexpected model catalogue resource')
  return item
}

export function modelCatalogError(error: unknown) {
  return axios.isAxiosError(error) && error.response?.status === 422
    ? t('catalog:memberModels.catalogueOverflow')
    : catalogError(error)
}
