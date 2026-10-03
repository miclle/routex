import client from './client'
import type {
  ProjectCreationInput,
  ProjectOverviewRecord,
  ResourceCandidate,
  ResourceFilters,
  ResourceKind,
  ResourceList,
  ResourceRecord,
} from '@/types/resources'
export const resourcePath = (kind: ResourceKind, admin: boolean) =>
  `${admin ? '/admin' : ''}/${kind}`
export async function getResources(
  kind: ResourceKind,
  admin: boolean,
  filters: ResourceFilters,
  cursor: string | null,
  signal?: AbortSignal,
) {
  return (
    await client.get<ResourceList>(resourcePath(kind, admin), {
      params: { ...filters, cursor: cursor || undefined },
      signal,
    })
  ).data
}
export async function getResource(
  kind: ResourceKind,
  admin: boolean,
  id: string,
  signal?: AbortSignal,
) {
  return (await client.get<ResourceRecord>(`${resourcePath(kind, admin)}/${id}`, { signal })).data
}
export async function getResourceCandidates(path: string, q: string, signal?: AbortSignal) {
  return (
    await client.get<{ items: ResourceCandidate[] }>(path, {
      params: path.startsWith('/teams/') ? { query: q || undefined } : { q },
      signal,
    })
  ).data.items
}

export async function getProjectCreationManagers(q: string, signal?: AbortSignal) {
  const value = (
    await client.get<unknown>('/projects/creation-manager-candidates', {
      params: { q },
      signal,
    })
  ).data
  if (
    !value ||
    typeof value !== 'object' ||
    !('items' in value) ||
    !Array.isArray(value.items) ||
    value.items.length > 50 ||
    value.items.some(
      (item: unknown) =>
        !item ||
        typeof item !== 'object' ||
        !('id' in item) ||
        typeof item.id !== 'string' ||
        !item.id ||
        !('name' in item) ||
        typeof item.name !== 'string' ||
        !('email' in item) ||
        typeof item.email !== 'string',
    ) ||
    new Set(value.items.map((item: ResourceCandidate) => item.id)).size !== value.items.length
  )
    throw new Error('Invalid Project creation candidates')
  return value.items as ResourceCandidate[]
}
export async function createProject(
  input: ProjectCreationInput,
  csrf: string,
  actor: string,
  managers: string[],
) {
  const value = (
    await client.post<ResourceRecord>('/projects', input, {
      headers: { 'X-CSRF-Token': csrf },
    })
  ).data
  if (
    !value ||
    typeof value.id !== 'string' ||
    !value.id ||
    value.creator_id !== actor ||
    value.name !== input.name ||
    value.description !== input.description ||
    value.status !== 'active' ||
    !Array.isArray(value.model_ids) ||
    value.model_ids.length ||
    !Array.isArray(value.managers) ||
    value.managers.length !== managers.length ||
    new Set(value.managers.map((item) => item.user_id)).size !== managers.length ||
    value.managers.some((item) => !managers.includes(item.user_id))
  )
    throw new Error('Unconfirmed Project creation response')
  return value
}

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const integer = (value: unknown) => Number.isSafeInteger(value) && Number(value) >= 0
const timestamp = (value: unknown) =>
  typeof value === 'string' && Number.isFinite(Date.parse(value))
const decimal = (value: unknown) =>
  typeof value === 'string' && /^(0|[1-9][0-9]{0,17})(\.[0-9]{1,18})?$/.test(value)
function amounts(value: unknown) {
  return (
    object(value) &&
    Object.entries(value).every(
      ([currency, amount]) =>
        /^[A-Z]{3}$/.test(currency) &&
        typeof amount === 'string' &&
        /^(0|[1-9][0-9]*)(\.[0-9]{1,18})?$/.test(amount),
    )
  )
}
export async function getProjectOverview(
  projectId: string,
  signal?: AbortSignal,
): Promise<ProjectOverviewRecord> {
  const value = (
    await client.get<unknown>(`/projects/${encodeURIComponent(projectId)}/overview`, { signal })
  ).data
  if (
    !object(value) ||
    value.project_id !== projectId ||
    !timestamp(value.observed_at) ||
    !object(value.counts) ||
    !integer(value.counts.managers) ||
    !integer(value.counts.models) ||
    ![value.counts.active_keys, value.counts.pending_requests].every(
      (item) => item === null || integer(item),
    ) ||
    !(value.last_call_at === null || timestamp(value.last_call_at)) ||
    typeof value.calls_available !== 'boolean' ||
    (!value.calls_available && value.last_call_at !== null) ||
    !Array.isArray(value.activities) ||
    value.activities.length > 5 ||
    value.activities.some(
      (item) =>
        !object(item) ||
        typeof item.id !== 'string' ||
        !item.id ||
        ![
          'project_created',
          'project_updated',
          'managers_changed',
          'models_changed',
          'limits_changed',
        ].includes(String(item.kind)) ||
        !timestamp(item.created_at) ||
        !(item.actor_name === null || typeof item.actor_name === 'string') ||
        item.status !== 'committed',
    ) ||
    new Set(value.activities.map((item) => item.id)).size !== value.activities.length
  )
    throw new Error('Invalid Project overview response')
  if (value.monthly_quota !== null) {
    const quota = value.monthly_quota
    if (
      !object(quota) ||
      !(quota.tokens_month === null || integer(quota.tokens_month)) ||
      !(quota.money_month === null || decimal(quota.money_month)) ||
      typeof quota.currency !== 'string' ||
      (quota.money_month === null ? quota.currency !== '' : !/^[A-Z]{3}$/.test(quota.currency)) ||
      typeof quota.platform_currency !== 'string' ||
      !/^[A-Z]{3}$/.test(quota.platform_currency) ||
      typeof quota.policy_etag !== 'string' ||
      !quota.policy_etag
    )
      throw new Error('Invalid Project quota response')
    if (quota.usage !== null) {
      const usage = quota.usage
      if (
        !object(usage) ||
        !timestamp(usage.as_of) ||
        !timestamp(usage.month_start) ||
        !timestamp(usage.month_end) ||
        typeof usage.time_zone !== 'string' ||
        typeof usage.covered !== 'boolean' ||
        typeof usage.tokens_used !== 'string' ||
        !/^(0|[1-9][0-9]*)$/.test(usage.tokens_used) ||
        typeof usage.tokens_held !== 'string' ||
        !/^(0|[1-9][0-9]*)$/.test(usage.tokens_held) ||
        !integer(usage.tokens_unknown) ||
        !integer(usage.money_unknown) ||
        !amounts(usage.money_used) ||
        !amounts(usage.money_held) ||
        Date.parse(String(usage.month_start)) >= Date.parse(String(usage.month_end))
      )
        throw new Error('Invalid Project quota usage')
      new Intl.DateTimeFormat('en', { timeZone: usage.time_zone })
    }
  }
  return value as unknown as ProjectOverviewRecord
}
