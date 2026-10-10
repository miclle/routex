import client from './client'
import { canonicalRuntimeID } from './runtime-applications'
import type {
  RuntimeInstallationRecord,
  RuntimeInstallationsFilter,
  RuntimeInstallationsPage,
} from '@/types/runtime-installations'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const keys = (value: Record<string, unknown>, expected: string[]) =>
  Object.keys(value).sort().join(',') === [...expected].sort().join(',')
const utc = (value: unknown, precision: number): value is string =>
  typeof value === 'string' &&
  new RegExp(
    `^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}(?:\\.\\d{1,${precision}})?Z$(?![\\s\\S])`,
  ).test(value) &&
  Number.isFinite(Date.parse(value)) &&
  new Date(value).toISOString().slice(0, 19) === value.slice(0, 19)
const cursor = (value: unknown): value is string =>
  typeof value === 'string' && /^[A-Za-z0-9_-]{1,200}$(?![\s\S])/.test(value)
const installationID = (value: unknown): value is string =>
  typeof value === 'string' && /^rin_[0-7][0-9a-hjkmnp-tv-z]{25}$(?![\s\S])/.test(value)

// Compare retained microseconds without rounding through a JavaScript Date.
const instant = (value: string) => {
  const [whole, fraction = ''] = value.slice(0, -1).split('.')
  return `${whole}.${fraction.padEnd(6, '0')}`
}

function invalid(): never {
  throw new Error('Invalid Gateway installation observations')
}

export async function getRuntimeInstallations(
  filter: RuntimeInstallationsFilter = {},
  signal?: AbortSignal,
): Promise<RuntimeInstallationsPage> {
  if (
    Object.keys(filter).some((key) => !['instance_id', 'cursor', 'limit'].includes(key)) ||
    (filter.instance_id !== undefined && !canonicalRuntimeID(filter.instance_id, 'ins')) ||
    (filter.cursor !== undefined && !cursor(filter.cursor)) ||
    (filter.limit !== undefined &&
      (!Number.isSafeInteger(filter.limit) || filter.limit < 1 || filter.limit > 100))
  )
    invalid()
  const response = await client.get<unknown>('/admin/runtime/installations', {
    params: filter,
    signal,
  })
  const value = response.data
  if (
    response.status !== 200 ||
    !object(value) ||
    !keys(value, ['scope', 'observed_at', 'items', 'next_cursor']) ||
    value.scope !== 'single_process_gateway_admission' ||
    !utc(value.observed_at, 9) ||
    !Array.isArray(value.items) ||
    value.items.length > (filter.limit ?? 20) ||
    !(value.next_cursor === null || cursor(value.next_cursor)) ||
    (value.next_cursor !== null && value.next_cursor === filter.cursor)
  )
    invalid()
  const seen = new Set<string>()
  for (const row of value.items) {
    if (
      !object(row) ||
      !keys(row, [
        'id',
        'projection_version',
        'instance_id',
        'instance_started_at',
        'snapshot_id',
        'routes_published_at',
        'first_observed_at',
        'instance_status',
        'current_serving_installation_matches',
      ]) ||
      !installationID(row.id) ||
      row.projection_version !== 1 ||
      !canonicalRuntimeID(row.instance_id, 'ins') ||
      !canonicalRuntimeID(row.snapshot_id, 'cfg') ||
      (filter.instance_id !== undefined && row.instance_id !== filter.instance_id) ||
      !utc(row.instance_started_at, 6) ||
      !utc(row.routes_published_at, 6) ||
      !utc(row.first_observed_at, 6) ||
      instant(row.routes_published_at) < instant(row.instance_started_at) ||
      instant(row.first_observed_at) < instant(row.instance_started_at) ||
      instant(row.first_observed_at) < instant(row.routes_published_at) ||
      typeof row.instance_status !== 'string' ||
      !['online', 'offline', 'unknown'].includes(row.instance_status) ||
      !(
        row.current_serving_installation_matches === null ||
        typeof row.current_serving_installation_matches === 'boolean'
      ) ||
      (row.instance_status !== 'online' && row.current_serving_installation_matches !== null) ||
      seen.has(row.id) ||
      (seen.size > 0 && (value.items[seen.size - 1] as RuntimeInstallationRecord).id <= row.id)
    )
      invalid()
    seen.add(row.id)
  }
  return value as unknown as RuntimeInstallationsPage
}
