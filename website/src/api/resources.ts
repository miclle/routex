import client from './client'
import { AxiosHeaders } from 'axios'
import type {
  ProjectCreationInput,
  ProjectCreationContext,
  ProjectCreationModelPage,
  ProjectCreationIntent,
  ProjectCreationReceipt,
  ProjectOverviewRecord,
  ResourceCandidate,
  ResourceFilters,
  ResourceKind,
  ResourceList,
  ResourceRecord,
  TeamCreationContext,
  TeamCreationIntent,
  TeamCreationReceipt,
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

export async function getProjectCreationContext(
  signal?: AbortSignal,
): Promise<ProjectCreationContext> {
  const value = (await client.get<unknown>('/project-creation-resources', { signal })).data
  if (
    !object(value) ||
    typeof value.review_etag !== 'string' ||
    !/^[a-f0-9]{64}$/.test(value.review_etag) ||
    typeof value.platform_currency !== 'string' ||
    !/^[A-Z]{3}$/.test(value.platform_currency) ||
    !['can_set_models', 'can_set_limits', 'can_request_resources'].every(
      (key) => typeof value[key] === 'boolean',
    )
  )
    throw new Error('Invalid Project creation resource context')
  return value as unknown as ProjectCreationContext
}
export async function getProjectCreationModels(
  q: string,
  cursor: string | null,
  signal?: AbortSignal,
): Promise<ProjectCreationModelPage> {
  const value = (
    await client.get<unknown>('/project-creation-models', {
      params: { q: q || undefined, cursor: cursor ?? undefined, limit: 50 },
      signal,
    })
  ).data
  if (
    !object(value) ||
    !Array.isArray(value.items) ||
    value.items.length > 50 ||
    value.items.some(
      (item) =>
        !object(item) || typeof item.id !== 'string' || !item.id || typeof item.name !== 'string',
    ) ||
    new Set(value.items.map((item) => item.id)).size !== value.items.length ||
    !(
      value.next_cursor === null ||
      (typeof value.next_cursor === 'string' &&
        value.next_cursor.length > 0 &&
        value.next_cursor.length <= 512 &&
        value.next_cursor !== cursor)
    )
  )
    throw new Error('Invalid Project creation model candidates')
  return value as unknown as ProjectCreationModelPage
}
export function validProjectCreationReason(reason: string) {
  return (
    !!reason.trim() &&
    new TextEncoder().encode(reason).byteLength <= 1024 &&
    !/\p{Cc}/u.test(reason)
  )
}
function creationIDs(value: unknown, required: boolean): value is string[] {
  return (
    Array.isArray(value) &&
    value.length <= 1000 &&
    (!required || value.length > 0) &&
    value.every(
      (id) => typeof id === 'string' && id.length > 0 && id.length <= 128 && !/\p{Cc}/u.test(id),
    ) &&
    new Set(value).size === value.length
  )
}
function validInitialResources(value: unknown) {
  if (
    !object(value) ||
    typeof value.reason !== 'string' ||
    !validProjectCreationReason(value.reason)
  )
    return false
  const fields = ['tokens_month', 'money_month', 'rpm', 'tpm', 'concurrency']
  if (
    Object.keys(value).some((key) => !['model_ids', 'currency', 'reason', ...fields].includes(key))
  )
    return false
  if (Object.hasOwn(value, 'model_ids') && !creationIDs(value.model_ids, true)) return false
  if (
    ['tokens_month', 'rpm', 'tpm', 'concurrency'].some(
      (key) => Object.hasOwn(value, key) && !integer(value[key]),
    )
  )
    return false
  if (Object.hasOwn(value, 'money_month')) {
    if (
      !decimal(value.money_month) ||
      typeof value.currency !== 'string' ||
      !/^[A-Z]{3}$/.test(value.currency)
    )
      return false
  } else if (Object.hasOwn(value, 'currency')) return false
  return Object.hasOwn(value, 'model_ids') || fields.some((key) => Object.hasOwn(value, key))
}
export async function createProjectResources(
  intent: ProjectCreationIntent,
  csrf: string,
): Promise<ProjectCreationReceipt> {
  if (
    !csrf ||
    !intent.body.name.trim() ||
    !creationIDs(intent.body.manager_ids, true) ||
    (intent.body.initial_resources !== undefined &&
      !validInitialResources(intent.body.initial_resources)) ||
    (intent.body.initial_request !== undefined &&
      !validInitialResources(intent.body.initial_request)) ||
    (intent.body.initial_resources !== undefined && intent.body.initial_request !== undefined) ||
    !/^[a-f0-9]{64}$/.test(intent.etag) ||
    !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      intent.body.creation_id,
    )
  )
    throw new Error('Invalid Project creation intent')
  const value = (
    await client.post<unknown>('/projects', intent.body, {
      headers: { 'X-CSRF-Token': csrf, 'If-Match': `"${intent.etag}"` },
    })
  ).data
  const captured = intent.body.initial_request
  const requests = captured
    ? Number(!!captured.model_ids?.length) +
      Number(captured.tokens_month !== undefined || captured.money_month !== undefined) +
      Number(
        captured.rpm !== undefined ||
          captured.tpm !== undefined ||
          captured.concurrency !== undefined,
      )
    : 0
  if (
    !object(value) ||
    value.committed !== true ||
    !object(value.receipt) ||
    value.receipt.creation_id !== intent.body.creation_id ||
    typeof value.receipt.project_id !== 'string' ||
    !value.receipt.project_id ||
    !timestamp(value.receipt.created_at) ||
    !Array.isArray(value.receipt.initial_request_ids) ||
    value.receipt.initial_request_ids.length !== requests ||
    value.receipt.initial_request_ids.some((id) => typeof id !== 'string' || !id) ||
    new Set(value.receipt.initial_request_ids).size !== requests ||
    typeof value.runtime_applied !== 'boolean' ||
    !['pending', 'applied', 'superseded', 'unavailable'].includes(
      String(value.application_status),
    ) ||
    value.runtime_applied !== (value.application_status === 'applied')
  )
    throw new Error('Unconfirmed Project creation receipt')
  if (value.project === null) {
    if (value.application_status !== 'unavailable')
      throw new Error('Unconfirmed Project creation current state')
  } else if (
    !object(value.project) ||
    value.project.id !== value.receipt.project_id ||
    typeof value.project.name !== 'string' ||
    typeof value.project.description !== 'string' ||
    !['active', 'disabled', 'archived'].includes(String(value.project.status)) ||
    !timestamp(value.project.created_at) ||
    !Array.isArray(value.project.model_ids) ||
    value.project.model_ids.some((id) => typeof id !== 'string') ||
    !Array.isArray(value.project.managers) ||
    value.project.managers.some(
      (manager) => !object(manager) || typeof manager.user_id !== 'string',
    ) ||
    value.application_status === 'unavailable'
  )
    throw new Error('Unconfirmed Project creation current state')
  return value as unknown as ProjectCreationReceipt
}

// Team creation keeps its reviewed intent separate from legacy resource writes.
function teamCreationObject(
  value: unknown,
  keys: readonly string[],
): value is Record<string, unknown> {
  return (
    object(value) &&
    Object.keys(value).length === keys.length &&
    keys.every((key) => Object.hasOwn(value, key))
  )
}
function teamCreationID(value: unknown): value is string {
  return typeof value === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(value)
}
function teamCreationStamp(value: unknown): value is string {
  if (typeof value !== 'string') return false
  const parts =
    /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(Z|[+-](\d{2}):(\d{2}))$/.exec(
      value,
    )
  if (!parts) return false
  const [year, month, day, hour, minute, second] = parts.slice(1, 7).map(Number)
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0)
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
  // Validate the original wall calendar before parsing its numeric offset.
  // Date.parse can normalize invalid days, and a valid offset can cross a UTC year.
  return (
    year > 0 &&
    month >= 1 &&
    month <= 12 &&
    day >= 1 &&
    day <= days[month - 1] &&
    hour <= 23 &&
    minute <= 59 &&
    second <= 59 &&
    (parts[7] === 'Z' || (Number(parts[8]) <= 23 && Number(parts[9]) <= 59)) &&
    Number.isFinite(Date.parse(value))
  )
}
export async function getTeamCreationContext(signal?: AbortSignal): Promise<TeamCreationContext> {
  const response = await client.get<unknown>('/admin/teams/creation-context', { signal })
  const value = response.data
  const fields = [
    'tokens_5h',
    'tokens_7d',
    'tokens_month',
    'money_month',
    'rpm',
    'tpm',
    'concurrency',
  ]
  if (
    response.status !== 200 ||
    signal?.aborted ||
    !teamCreationObject(value, [
      'review_etag',
      'default_rule_etag',
      'platform_currency',
      'editable_fields',
      'default_policy',
    ]) ||
    typeof value.review_etag !== 'string' ||
    !/^[a-f0-9]{64}$/.test(value.review_etag) ||
    typeof value.default_rule_etag !== 'string' ||
    !/^[a-f0-9]{64}$/.test(value.default_rule_etag) ||
    (response.headers instanceof AxiosHeaders
      ? response.headers.get('ETag')
      : response.headers.etag) !== `"${value.review_etag}"` ||
    !Array.isArray(value.editable_fields) ||
    value.editable_fields.some((field) => typeof field !== 'string' || !fields.includes(field)) ||
    new Set(value.editable_fields).size !== value.editable_fields.length ||
    !object(value.default_policy)
  )
    throw new Error('Invalid Team creation context')
  const allowed = value.editable_fields as string[],
    policy = value.default_policy
  const groups = [
    ['tokens_5h', 'tokens_7d', 'tokens_month'],
    ['money_month'],
    ['rpm', 'tpm', 'concurrency'],
  ]
  if (
    groups.some(
      (group) =>
        group.some((field) => allowed.includes(field)) &&
        !group.every((field) => allowed.includes(field)),
    )
  )
    throw new Error('Invalid Team creation authority')
  const hasMoney = allowed.includes('money_month')
  if (
    hasMoney
      ? typeof value.platform_currency !== 'string' ||
        !/^[A-Z]{3}$/.test(value.platform_currency) ||
        typeof policy.currency !== 'string' ||
        (policy.money_month === null ? policy.currency !== '' : !/^[A-Z]{3}$/.test(policy.currency))
      : value.platform_currency !== null || Object.hasOwn(policy, 'currency')
  )
    throw new Error('Invalid Team creation currency')
  if (
    Object.keys(policy).some((field) => !allowed.includes(field) && field !== 'currency') ||
    allowed.some(
      (field) =>
        !Object.hasOwn(policy, field) ||
        !(
          policy[field] === null ||
          (typeof policy[field] === 'string' &&
            (field === 'money_month'
              ? /^(0|[1-9]\d{0,17})(\.\d{1,18})?$/.test(policy[field])
              : /^(0|[1-9]\d*)$/.test(policy[field]) &&
                BigInt(policy[field]) <= BigInt(Number.MAX_SAFE_INTEGER)))
        ),
    )
  )
    throw new Error('Invalid Team creation default policy')
  return value as unknown as TeamCreationContext
}
export async function getTeamCreationOwners(
  q: string,
  signal?: AbortSignal,
): Promise<ResourceCandidate[]> {
  if (new TextEncoder().encode(q).length > 200 || /\p{Cs}/u.test(q))
    throw new Error('Invalid Team owner search')
  const response = await client.get<unknown>('/admin/team-member-candidates', {
    params: { q: q || undefined },
    signal,
  })
  if (signal?.aborted || response.status !== 200 || !teamCreationObject(response.data, ['items']))
    throw new Error('Invalid Team owner candidates')
  const items = response.data.items
  if (
    !Array.isArray(items) ||
    items.length > 50 ||
    items.some(
      (item) =>
        !object(item) ||
        Object.keys(item).some((key) => !['id', 'name', 'email'].includes(key)) ||
        !teamCreationID(item.id) ||
        typeof item.name !== 'string' ||
        /\p{Cs}/u.test(item.name) ||
        new TextEncoder().encode(item.name).length > 65536 ||
        (item.email !== undefined &&
          (typeof item.email !== 'string' || /\p{Cs}/u.test(item.email))),
    ) ||
    new Set(items.map((item) => item.id)).size !== items.length
  )
    throw new Error('Invalid Team owner candidates')
  return items as ResourceCandidate[]
}
export async function createTeamWithInitialLimits(
  intent: TeamCreationIntent,
  csrf: string,
  signal?: AbortSignal,
): Promise<TeamCreationReceipt> {
  const body = intent.body
  if (
    !csrf ||
    !object(body) ||
    Object.keys(body).some(
      (key) => !['creation_id', 'name', 'description', 'owner_ids', 'initial_limits'].includes(key),
    ) ||
    !/^[a-f0-9]{64}$/.test(intent.etag) ||
    !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      body.creation_id,
    ) ||
    !body.name ||
    body.name !== body.name.trim() ||
    Array.from(body.name).length > 100 ||
    /[\p{Cc}\p{Cs}]/u.test(body.name) ||
    typeof body.description !== 'string' ||
    Array.from(body.description).length > 2000 ||
    Array.from(body.description).some(
      (char) => /[\p{Cc}\p{Cs}]/u.test(char) && char !== '\n' && char !== '\t',
    ) ||
    !Array.isArray(body.owner_ids) ||
    !body.owner_ids.length ||
    body.owner_ids.length > 1000 ||
    body.owner_ids.some((id) => !teamCreationID(id)) ||
    new Set(body.owner_ids).size !== body.owner_ids.length
  )
    throw new Error('Invalid Team creation intent')
  const limits = body.initial_limits
  if (limits !== undefined) {
    const fields = [
      'tokens_5h',
      'tokens_7d',
      'tokens_month',
      'money_month',
      'rpm',
      'tpm',
      'concurrency',
    ]
    if (
      !object(limits) ||
      typeof limits.reason !== 'string' ||
      !limits.reason ||
      limits.reason !== limits.reason.trim() ||
      new TextEncoder().encode(limits.reason).length > 2000 ||
      /[\p{Cc}\p{Cs}]/u.test(limits.reason) ||
      Object.keys(limits).some((field) => ![...fields, 'currency', 'reason'].includes(field)) ||
      !fields.some((field) => Object.hasOwn(limits, field)) ||
      fields
        .filter((field) => field !== 'money_month')
        .some(
          (field) =>
            Object.hasOwn(limits, field) &&
            (limits as unknown as Record<string, unknown>)[field] !== null &&
            !integer((limits as unknown as Record<string, unknown>)[field]),
        ) ||
      (Object.hasOwn(limits, 'money_month') &&
        limits.money_month !== null &&
        !decimal(limits.money_month)) ||
      (limits.money_month != null
        ? typeof limits.currency !== 'string' || !/^[A-Z]{3}$/.test(limits.currency)
        : Object.hasOwn(limits, 'currency'))
    )
      throw new Error('Invalid Team initial limits')
  }
  const response = await client.post<unknown>('/admin/teams', body, {
    signal,
    headers: { 'X-CSRF-Token': csrf, 'If-Match': `"${intent.etag}"` },
  })
  const value = response.data
  if (
    ![200, 201].includes(response.status) ||
    signal?.aborted ||
    !teamCreationObject(value, [
      'team',
      'receipt',
      'committed',
      'runtime_applied',
      'application_status',
    ]) ||
    value.committed !== true ||
    !teamCreationObject(value.receipt, ['creation_id', 'team_id', 'created_at']) ||
    value.receipt.creation_id !== body.creation_id ||
    !teamCreationID(value.receipt.team_id) ||
    !value.receipt.team_id.startsWith('tea_') ||
    !teamCreationStamp(value.receipt.created_at) ||
    typeof value.runtime_applied !== 'boolean' ||
    !['pending', 'applied', 'superseded', 'unavailable'].includes(
      String(value.application_status),
    ) ||
    value.runtime_applied !== (value.application_status === 'applied')
  )
    throw new Error('Unconfirmed Team creation receipt')
  if (value.team === null) {
    if (value.application_status !== 'unavailable')
      throw new Error('Unconfirmed Team creation current state')
  } else if (
    !object(value.team) ||
    value.team.id !== value.receipt.team_id ||
    typeof value.team.name !== 'string' ||
    typeof value.team.description !== 'string' ||
    !['active', 'disabled', 'archived'].includes(String(value.team.status)) ||
    !teamCreationStamp(value.team.created_at) ||
    !Array.isArray(value.team.model_ids) ||
    value.team.model_ids.length > 1000 ||
    new Set(value.team.model_ids).size !== value.team.model_ids.length ||
    value.team.model_ids.some((id) => !teamCreationID(id)) ||
    !Array.isArray(value.team.members) ||
    value.team.members.length > 10000 ||
    value.team.members.some(
      (item) =>
        !object(item) ||
        !teamCreationID(item.id) ||
        !teamCreationID(item.user_id) ||
        typeof item.name !== 'string' ||
        typeof item.email !== 'string' ||
        !['owner', 'member'].includes(String(item.role)) ||
        !['active', 'disabled'].includes(String(item.status)),
    ) ||
    value.application_status === 'unavailable'
  )
    throw new Error('Unconfirmed Team creation current state')
  if (
    value.runtime_applied &&
    (value.team === null ||
      !object(value.team) ||
      value.team.status !== 'active' ||
      value.team.name !== body.name ||
      value.team.description !== body.description ||
      (value.team.model_ids as unknown[]).length !== 0 ||
      (value.team.members as Record<string, unknown>[]).length !== body.owner_ids.length ||
      new Set((value.team.members as Record<string, unknown>[]).map((member) => member.user_id))
        .size !== body.owner_ids.length ||
      (value.team.members as Record<string, unknown>[]).some(
        (member) =>
          !body.owner_ids.includes(String(member.user_id)) ||
          member.role !== 'owner' ||
          member.status !== 'active',
      ))
  )
    throw new Error('Unconfirmed Team creation application')
  return value as unknown as TeamCreationReceipt
}
