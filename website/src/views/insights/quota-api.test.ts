import { afterEach, expect, it } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from '@/api/client'
import {
  exportQuotaInsights,
  getQuotaInsights,
  getQuotaScopeOptions,
  parseQuotaInsights,
  reviewQuotaScope,
} from '@/api/insights-quota'
import type { QuotaInsightsReport } from '@/types/insights-quota'
import {
  teamQuotaInput,
  keyQuotaInput,
  quotaActor,
  quotaFixture,
  quotaProject,
} from './quota-fixture'
const original = client.defaults.adapter
afterEach(() => {
  client.defaults.adapter = original
})
it('accepts real bounded frames, nanosecond offsets, exact journal values and explicit absence', () => {
  expect(parseQuotaInsights(quotaFixture(), teamQuotaInput)).toEqual(quotaFixture())
  expect(
    parseQuotaInsights(quotaFixture(keyQuotaInput), keyQuotaInput).items[0].shared_key_count,
  ).toBe(2)
  const value = quotaFixture()
  value.as_of = value.baseline_to = '2026-10-12T20:00:00.123456789+08:00'
  value.baseline_from = '2026-10-05T20:00:00.123456789+08:00'
  value.items[0].month_settled = '9223372036854775807'
  value.items[0].level = 'reached'
  value.items[0].estimate_status = 'already_reached'
  value.items[0].estimate_at = null
  value.items[0].estimate_within_seven_days = false
  value.items[0].utilization_percent = '123456789012345678901234567890.00'
  expect(parseQuotaInsights(value, teamQuotaInput).items[0].month_settled).toBe(
    '9223372036854775807',
  )
  const money = { ...teamQuotaInput, dimension: 'money' as const }
  const precise = quotaFixture(money)
  precise.items[0].local_cap = '0.000000000000000001'
  precise.items[0].month_settled = '9'.repeat(60) + '.123456789012345678'
  precise.items[0].level = 'reached'
  precise.items[0].estimate_status = 'already_reached'
  precise.items[0].estimate_at = null
  precise.items[0].estimate_within_seven_days = false
  expect(parseQuotaInsights(precise, money).items[0].local_cap).toBe('0.000000000000000001')
  const absent = quotaFixture()
  for (const key of [
    'as_of',
    'coverage_start',
    'baseline_from',
    'baseline_to',
    'month_from',
    'month_reset',
  ] as const)
    absent[key] = null
  absent.items[0] = {
    ...absent.items[0],
    local_cap: null,
    month_settled: null,
    baseline_settled: null,
    baseline_held: null,
    month_held: null,
    active_held: null,
    month_unknown: null,
    baseline_unknown: null,
    month_covered: false,
    baseline_covered: false,
    utilization_percent: null,
    level: 'unknown',
    estimate_at: null,
    estimate_within_seven_days: false,
    estimate_status: 'unavailable',
  }
  expect(parseQuotaInsights(absent, teamQuotaInput).as_of).toBeNull()
})
it('rejects wrong scopes, open envelopes, unsafe quantities, duplicate roots and contradictory observation frames', () => {
  const mutations: ((v: QuotaInsightsReport) => void)[] = [
    (v) => {
      v.items[0].estimate_within_seven_days = 'true' as never
    },
    (v) => {
      v.items[0].estimate_status = 'after_reset'
      v.items[0].estimate_at = null
    },
    (v) => {
      v.items[0].baseline_held = '1'
    },
    (v) => {
      v.items[0].month_held = '1'
    },
    (v) => {
      v.items[0].baseline_covered = false
    },
    (v) => {
      v.items[0].month_unknown = '1'
    },
    (v) => {
      v.items[0].estimate_at = v.month_reset
    },
    (v) => {
      v.items[0].estimate_at = v.as_of
    },
    (v) => {
      v.kind = 'key_quota'
    },
    (v) => {
      v.dimension = 'money'
    },
    (v) => {
      v.scope = { kind: 'member', id: quotaActor }
    },
    (v) => {
      Object.assign(v, { raw_calls: [] })
    },
    (v) => {
      Object.assign(v.items[0], { secret: 'private' })
    },
    (v) => {
      v.items.push({ ...v.items[0] })
    },
    (v) => {
      v.items = Array.from({ length: 21 }, () => ({ ...v.items[0] }))
    },
    (v) => {
      v.items[0].id = v.items[0].id.toUpperCase()
    },
    (v) => {
      v.items[0].status = 'expired' as never
    },
    (v) => {
      v.items[0].shared_key_count = -1
    },
    (v) => {
      v.items[0].local_cap = '9007199254740992'
    },
    (v) => {
      v.items[0].month_settled = '9223372036854775808'
    },
    (v) => {
      v.items[0].month_unknown = '01'
    },
    (v) => {
      v.items[0].active_held = '1e3'
    },
    (v) => {
      v.items[0].utilization_percent = '80'
    },
    (v) => {
      v.items[0].utilization_percent = '80.000'
    },
    (v) => {
      v.items[0].utilization_percent = '9'.repeat(83) + '.00'
    },
    (v) => {
      v.items[0].level = 'healthy' as never
    },
    (v) => {
      v.items[0].estimate_status = 'predicted' as never
    },
    (v) => {
      v.items[0].estimate_at = null
    },
    (v) => {
      v.items[0].estimate_at = v.month_reset!.replace('01T', '02T')
    },
    (v) => {
      v.items[0].estimate_status = 'already_reached'
    },
    (v) => {
      v.baseline_from = '2026-10-05T12:00:00.123456788Z'
    },
    (v) => {
      v.baseline_to = '2026-10-12T12:00:00.123456788Z'
    },
    (v) => {
      v.month_reset = v.as_of
    },
    (v) => {
      v.coverage_start = null
    },
    (v) => {
      v.as_of = null
    },
    (v) => {
      v.time_zone = 'Local'
    },
    (v) => {
      v.as_of = '2026-02-30T00:00:00Z'
    },
    (v) => {
      v.baseline_basis = 'settled_at' as never
    },
    (v) => {
      v.next_cursor = 'x'.repeat(161)
    },
    (v) => {
      v.next_cursor = 'next'
      v.items = []
    },
    (v) => {
      v.currency = 'USD'
    },
  ]
  for (const mutate of mutations) {
    const v = quotaFixture()
    mutate(v)
    expect(() => parseQuotaInsights(v, teamQuotaInput)).toThrow()
  }
  const money = { ...teamQuotaInput, dimension: 'money' as const }
  for (const field of ['local_cap', 'month_settled'] as const) {
    const v = quotaFixture(money)
    v.items[0][field] = '9'.repeat(field === 'local_cap' ? 19 : 61)
    expect(() => parseQuotaInsights(v, money)).toThrow()
  }
  const v = quotaFixture()
  v.next_cursor = 'same'
  expect(() => parseQuotaInsights(v, { ...teamQuotaInput, cursor: 'same' })).toThrow()
})
it('dispatches only reviewed scope and dimension with a bounded continuation and no calendar or actor overrides', async () => {
  const requests: { url: string | undefined; params: unknown }[] = []
  client.defaults.adapter = async (config) => {
    requests.push({ url: config.url, params: config.params })
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: quotaFixture(config.url?.endsWith('keys') ? keyQuotaInput : teamQuotaInput),
    }
  }
  await getQuotaInsights(teamQuotaInput, new AbortController().signal)
  await getQuotaInsights({ ...keyQuotaInput, cursor: 'next-page' }, new AbortController().signal)
  expect(requests).toEqual([
    { url: '/admin/insights/quota/teams', params: { dimension: 'tokens', limit: 20 } },
    {
      url: '/admin/insights/quota/keys',
      params: {
        dimension: 'tokens',
        scope_kind: 'member',
        scope_id: quotaActor,
        limit: 20,
        cursor: 'next-page',
      },
    },
  ])
  for (const input of [
    { ...teamQuotaInput, from: '2026-01-01T00:00:00Z' },
    { ...keyQuotaInput, scope: { kind: 'member', id: 'usr_other' } },
    { ...keyQuotaInput, cursor: '' },
  ])
    await expect(getQuotaInsights(input as never, new AbortController().signal)).rejects.toThrow()
  expect(requests).toHaveLength(2)
})
it('uses authorized bounded picker routes and independently checks exact Project manager identity', async () => {
  const urls: string[] = []
  let manager = quotaActor
  client.defaults.adapter = async (config) => {
    urls.push(config.url!)
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
      data: config.url?.endsWith(quotaProject)
        ? { id: quotaProject, managers: [{ user_id: manager }] }
        : config.url?.endsWith(quotaActor)
          ? { id: quotaActor }
          : {
              items: [
                {
                  id: config.url?.includes('members') ? quotaActor : quotaProject,
                  name: 'Authorized scope',
                  email: 'not-cached@example.test',
                },
              ],
              next_cursor: null,
            },
    }
  }
  expect(
    await getQuotaScopeOptions('member', false, '', null, new AbortController().signal),
  ).toEqual({ items: [{ id: quotaActor, name: 'Authorized scope' }], next_cursor: null })
  await getQuotaScopeOptions('project', false, '', null, new AbortController().signal)
  await getQuotaScopeOptions('project', true, '', null, new AbortController().signal)
  await reviewQuotaScope('member', quotaActor, quotaActor, false, new AbortController().signal)
  await reviewQuotaScope('project', quotaProject, quotaActor, false, new AbortController().signal)
  manager = quotaActor.toUpperCase()
  await expect(
    reviewQuotaScope('project', quotaProject, quotaActor, false, new AbortController().signal),
  ).rejects.toThrow()
  await reviewQuotaScope('project', quotaProject, quotaActor, true, new AbortController().signal)
  expect(urls.slice(0, 5)).toEqual([
    '/admin/members',
    '/projects',
    '/admin/projects',
    '/admin/members/' + quotaActor,
    '/projects/' + quotaProject,
  ])
})
it('exports a fresh bounded CSV, refuses malformed blobs and never accepts an aborted response', async () => {
  let mime = 'text/csv; charset=utf-8'
  let empty = false
  const urls: string[] = []
  client.defaults.adapter = async (config) => {
    urls.push(config.url!)
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ 'content-type': mime }),
      data: new Blob(empty ? [] : ['id,cap\naccount,100']),
    }
  }
  expect(
    (await exportQuotaInsights(keyQuotaInput, new AbortController().signal)).size,
  ).toBeGreaterThan(0)
  expect(urls[0]).toBe('/admin/insights/quota/keys/export.csv')
  mime = 'application/json'
  await expect(exportQuotaInsights(teamQuotaInput, new AbortController().signal)).rejects.toThrow()
  mime = 'text/csv'
  empty = true
  await expect(exportQuotaInsights(teamQuotaInput, new AbortController().signal)).rejects.toThrow()
  const controller = new AbortController()
  controller.abort()
  await expect(getQuotaInsights(teamQuotaInput, controller.signal)).rejects.toThrow()
  expect(urls).toHaveLength(3)
})
