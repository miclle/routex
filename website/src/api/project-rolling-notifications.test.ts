import { afterEach, expect, it, vi } from 'vitest'
import client from './client'
import { getNotifications, recordedProjectRollingQuotaWarning } from './notifications'
import type { Notification } from '@/types/notifications'

function notice(): Notification {
  return {
    id: 'jri_sample',
    rolling_quota_warning_observation_id: 'jro_sample',
    kind: 'project_rolling_quota_warning',
    severity: 'medium',
    detail_code: 'tokens_5h_near',
    subject_type: 'project',
    subject_id: 'prj_00000000000000000000000001',
    subject_name: 'Retained root',
    occurrence_count: 1,
    read: false,
    read_at: null,
    first_seen_at: '2026-10-06T12:00:00Z',
    last_seen_at: '2026-10-06T12:00:00Z',
    project_rolling_quota_warning: {
      scope_kind: 'project',
      scope_id: 'prj_00000000000000000000000001',
      window_kind: '5h',
      episode_id: 'rwe_sample',
      policy_revision: 'lim_applied',
      window_start: '2026-10-06T07:00:00Z',
      window_end: '2026-10-06T12:00:00Z',
      as_of: '2026-10-06T12:00:00Z',
      coverage_start: '2026-10-01T00:00:00Z',
      resource_created_at: '2026-10-01T00:00:00Z',
      time_zone: 'UTC',
      limit: '100',
      settled: '80',
      level: 'near',
      threshold: 80,
      threshold_generation: 'project-rolling-80-90-v1',
    },
  }
}
afterEach(() => vi.restoreAllMocks())
it('accepts the exact recipient/window/settled source without computing remaining allowance', async () => {
  const item = notice()
  vi.spyOn(client, 'get').mockResolvedValueOnce({ data: { items: [item], unread_count: 1 } })
  expect((await getNotifications('unread', undefined, undefined, 'usr_member')).items).toEqual([
    item,
  ])
  const critical = notice()
  critical.severity = 'high'
  critical.detail_code = 'tokens_7d_critical'
  Object.assign(critical.project_rolling_quota_warning!, {
    window_kind: '7d',
    window_start: '2026-09-29T12:00:00Z',
    coverage_start: '2026-09-01T00:00:00Z',
    resource_created_at: '2026-09-01T00:00:00Z',
    level: 'critical',
    threshold: 90,
    limit: '9223372036854775807',
    settled: '8301034833169298227',
  })
  expect(recordedProjectRollingQuotaWarning(critical, 'usr_member')).toBe(
    critical.project_rolling_quota_warning,
  )
})
it.each([
  'scope',
  'recipient',
  'case',
  'kind',
  'period',
  'duration',
  'coverage',
  'future_birth',
  'zero',
  'unknown',
  'held',
  'decimal',
  'overflow',
  'below',
  'critical_mismatch',
  'threshold',
  'generation',
  'monthly_alias',
  'alert',
  'delivery',
  'bad_timezone',
  'episode',
  'revision',
  'date',
  'count',
  'boolean',
  'unknown_field',
])('rejects %s before a private snapshot can render', async (kind) => {
  const item = notice()
  const warning = item.project_rolling_quota_warning!
  const raw = warning as unknown as Record<string, unknown>
  switch (kind) {
    case 'scope':
      raw.scope_kind = 'personal_key'
      break
    case 'recipient':
      warning.scope_id = 'usr_other'
      break
    case 'case':
      warning.scope_id = 'USR_MEMBER'
      break
    case 'kind':
      item.kind = 'future_kind'
      break
    case 'period':
      raw.window_kind = '5H'
      break
    case 'duration':
      warning.window_start = '2026-10-06T06:00:00Z'
      break
    case 'coverage':
      warning.coverage_start = '2026-10-06T11:00:00Z'
      break
    case 'future_birth':
      warning.resource_created_at = '2026-10-07T12:00:00Z'
      break
    case 'zero':
      warning.limit = '0'
      break
    case 'unknown':
      raw.tokens_unknown = 1
      break
    case 'held':
      raw.tokens_held = 999
      break
    case 'decimal':
      warning.settled = '80.0'
      break
    case 'overflow':
      warning.limit = '9223372036854775808'
      break
    case 'below':
      warning.settled = '79'
      break
    case 'critical_mismatch':
      warning.settled = '90'
      break
    case 'threshold':
      raw.threshold = '80'
      break
    case 'generation':
      raw.threshold_generation = 'personal-monthly-80-90-v1'
      break
    case 'monthly_alias':
      item.quota_warning = {} as never
      break
    case 'alert':
      item.alert_id = 'alt_other'
      break
    case 'delivery':
      item.delivery_status = 'accepted'
      break
    case 'bad_timezone':
      warning.time_zone = 'Local'
      break
    case 'episode':
      warning.episode_id = ''
      break
    case 'revision':
      warning.policy_revision = 'bad revision'
      break
    case 'date':
      warning.as_of = 'invalid'
      break
    case 'count':
      item.occurrence_count = 2
      break
    case 'boolean':
      item.read = 'false' as never
      break
    case 'unknown_field':
      raw.estimated_percentage = 80
      break
  }
  vi.spyOn(client, 'get').mockResolvedValueOnce({ data: { items: [item], unread_count: 1 } })
  await expect(getNotifications('all', undefined, undefined, 'usr_member')).rejects.toThrow(
    'Invalid notification response',
  )
})
it('does not decode a known rolling payload without its current recipient', async () => {
  vi.spyOn(client, 'get').mockResolvedValueOnce({ data: { items: [notice()], unread_count: 1 } })
  await expect(getNotifications('all')).rejects.toThrow('Invalid notification response')
})

it.each([
  ['100', '100'],
  ['100', '101'],
  ['1', '1'],
  ['1', '9223372036854775807'],
  ['9223372036854775807', '9223372036854775807'],
])('records critical90 when a known sample reaches%s/%s', (limit, settled) => {
  const item = notice()
  item.severity = 'high'
  item.detail_code = 'tokens_5h_critical'
  Object.assign(item.project_rolling_quota_warning!, {
    limit,
    settled,
    level: 'critical',
    threshold: 90,
  })
  expect(recordedProjectRollingQuotaWarning(item, 'usr_member')).toBe(
    item.project_rolling_quota_warning,
  )
})
it('rejects settled overflow even above a small valid cap, and does not relabel100 as a reminder', () => {
  const item = notice()
  item.severity = 'high'
  item.detail_code = 'tokens_5h_critical'
  Object.assign(item.project_rolling_quota_warning!, {
    limit: '1',
    settled: '9223372036854775808',
    level: 'critical',
    threshold: 90,
  })
  expect(recordedProjectRollingQuotaWarning(item, 'usr_member')).toBeUndefined()
  const near = notice()
  near.project_rolling_quota_warning!.settled = '100'
  expect(recordedProjectRollingQuotaWarning(near, 'usr_member')).toBeUndefined()
})

it.each(['owner', 'owner_birth', 'blank_name', 'secret_field', 'project_scope'])(
  'rejects Project invalid %s',
  async (kind) => {
    const item = notice()
    const warning = item.project_rolling_quota_warning!
    if (kind === 'owner') Object.assign(warning, { owner_id: 'usr_other' })
    if (kind === 'owner_birth') Object.assign(warning, { owner_created_at: '2026-10-07T00:00:00Z' })
    if (kind === 'blank_name') item.subject_name = ''
    if (kind === 'secret_field') Object.assign(item, { secret: 'never' })
    if (kind === 'project_scope') Object.assign(warning, { scope_kind: 'project_key' })
    expect(recordedProjectRollingQuotaWarning(item, 'usr_member')).toBeUndefined()
    vi.spyOn(client, 'get').mockResolvedValueOnce({ data: { items: [item], unread_count: 1 } })
    await expect(getNotifications('all', undefined, undefined, 'usr_member')).rejects.toThrow()
  },
)

it('preserves100Unicode codepoint historical names and rejects101 or untrimmed names', () => {
  const item = notice()
  item.subject_name = '😀'.repeat(100)
  expect(recordedProjectRollingQuotaWarning(item, 'usr_member')).toBe(
    item.project_rolling_quota_warning,
  )
  item.subject_name += '😀'
  expect(recordedProjectRollingQuotaWarning(item, 'usr_member')).toBeUndefined()
  item.subject_name = '\ud800'
  expect(recordedProjectRollingQuotaWarning(item, 'usr_member')).toBeUndefined()
  item.subject_name = ' Retained root '
  expect(recordedProjectRollingQuotaWarning(item, 'usr_member')).toBeUndefined()
})
