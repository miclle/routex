import { afterEach, expect, it } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from '@/api/client'
import { analyzeKeyQuotaInsights, analyzeTeamQuotaInsights } from '@/api/insights'
import type {
  InsightsKeyQuotaAnalysis,
  InsightsKeyQuotaInput,
  InsightsTeamQuotaAnalysis,
  InsightsTeamQuotaInput,
} from '@/types/insights'
import { quotaActor, quotaFixture, quotaProject } from './quota-fixture'

const original = client.defaults.adapter
type Kind = 'team' | 'member' | 'project'
const common = {
  model_id: 'mdl_01arz3ndektsv4rrffq69g5fav',
  language: 'en' as const,
  cursor: 'cmV2aWV3ZWQ',
  limit: 20,
}
function input(kind: Kind): InsightsTeamQuotaInput | InsightsKeyQuotaInput {
  return kind === 'team'
    ? { ...common, report: 'team_quota_money' }
    : {
        ...common,
        report: 'key_quota_money',
        scope_kind: kind,
        scope_id: kind === 'member' ? quotaActor : quotaProject,
      }
}
function result(kind: Kind): InsightsTeamQuotaAnalysis | InsightsKeyQuotaAnalysis {
  const report = quotaFixture()
  const capture = {
    source: 'local_quota_journal' as const,
    queried_at: '2026-10-14T12:00:00Z',
    cursor: common.cursor,
    limit: common.limit,
    has_more: false,
    timezone: report.time_zone,
    as_of: report.as_of,
    coverage_start: report.coverage_start,
    baseline_from: report.baseline_from,
    baseline_to: report.baseline_to,
    month_from: report.month_from,
    month_reset: report.month_reset,
    baseline_basis: report.baseline_basis,
    dimension: 'money' as const,
    currency: 'USD',
  }
  const value = {
    request_id: 'req_01arz3ndektsv4rrffq69g5fav',
    completion: 'completed' as const,
    text: 'Keep unknown Money coverage explicit.',
  }
  return kind === 'team'
    ? {
        ...value,
        quota_capture: capture,
        labels: [
          { label: 'T1', team_id: report.items[0].id, name: 'Private Team', unknown: false },
        ],
      }
    : {
        ...value,
        key_quota_capture: {
          ...capture,
          scope_kind: kind,
          scope_id: kind === 'member' ? quotaActor : quotaProject,
        },
        labels: [
          {
            label: 'K1',
            key_id: `${kind === 'member' ? 'key' : 'pky'}_01arz3ndektsv4rrffq69g5fav`,
            name: 'Private Key',
            unknown: false,
          },
        ],
      }
}
function analyze(
  kind: Kind,
  value: InsightsTeamQuotaInput | InsightsKeyQuotaInput,
  currency?: string,
) {
  return kind === 'team'
    ? analyzeTeamQuotaInsights(
        value as InsightsTeamQuotaInput,
        'fresh-csrf',
        new AbortController().signal,
        currency,
      )
    : analyzeKeyQuotaInsights(
        value as InsightsKeyQuotaInput,
        'fresh-csrf',
        new AbortController().signal,
        currency,
      )
}
afterEach(() => {
  client.defaults.adapter = original
})

it.each(['team', 'member', 'project'] as const)(
  'keeps reviewed %s Money currency local and preserves the exact closed page request',
  async (kind) => {
    const request = { ...input(kind), question: '  Explain this Money page only  ' }
    client.defaults.adapter = async (config) => {
      expect(config.url).toBe('/admin/insights/analyze')
      expect(JSON.parse(config.data)).toEqual(request)
      expect(config.headers.get('X-CSRF-Token')).toBe('fresh-csrf')
      expect(JSON.parse(config.data)).not.toHaveProperty('currency')
      expect(JSON.parse(config.data)).not.toHaveProperty('dimension')
      return {
        config,
        data: result(kind),
        status: 200,
        statusText: '',
        headers: new AxiosHeaders(),
      }
    }
    expect(await analyze(kind, request, 'USD')).toEqual(result(kind))
  },
)
it.each(['team', 'member', 'project'] as const)(
  'rejects changed or cross-dimension %s Money captures before publishing an answer',
  async (kind) => {
    for (const mutate of [
      (capture: Record<string, unknown>) => {
        delete capture.currency
      },
      (capture: Record<string, unknown>) => {
        capture.currency = 'EUR'
      },
      (capture: Record<string, unknown>) => {
        capture.currency = 'usd'
      },
      (capture: Record<string, unknown>) => {
        capture.dimension = 'tokens'
      },
      (capture: Record<string, unknown>) => {
        delete capture.dimension
      },
      (capture: Record<string, unknown>) => {
        capture.local_cap_currency = 'USD'
      },
    ]) {
      const value = result(kind)
      const capture = 'quota_capture' in value ? value.quota_capture : value.key_quota_capture
      mutate(capture as unknown as Record<string, unknown>)
      client.defaults.adapter = async (config) => ({
        config,
        data: value,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders(),
      })
      await expect(analyze(kind, input(kind), 'USD')).rejects.toThrow()
    }
    const tokenInput = {
      ...input(kind),
      report: kind === 'team' ? 'team_quota_tokens' : 'key_quota_tokens',
    } as InsightsTeamQuotaInput | InsightsKeyQuotaInput
    client.defaults.adapter = async (config) => ({
      config,
      data: result(kind),
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
    })
    await expect(analyze(kind, tokenInput)).rejects.toThrow()
  },
)
it.each(['team', 'member', 'project'] as const)(
  'requires valid local %s Money review currency and rejects client currency before dispatch',
  async (kind) => {
    let dispatched = false
    client.defaults.adapter = async () => {
      dispatched = true
      throw new Error('Unexpected HTTP')
    }
    for (const currency of [undefined, '', 'usd', 'US', 'USD '])
      await expect(analyze(kind, input(kind), currency)).rejects.toThrow()
    await expect(
      analyze(
        kind,
        { ...input(kind), currency: 'USD' } as unknown as InsightsTeamQuotaInput,
        'USD',
      ),
    ).rejects.toThrow()
    await expect(
      analyze(kind, { ...input(kind), statistics: {} } as unknown as InsightsTeamQuotaInput, 'USD'),
    ).rejects.toThrow()
    const tokenInput = {
      ...input(kind),
      report: kind === 'team' ? 'team_quota_tokens' : 'key_quota_tokens',
    } as InsightsTeamQuotaInput | InsightsKeyQuotaInput
    await expect(analyze(kind, tokenInput, 'USD')).rejects.toThrow()
    expect(dispatched).toBe(false)
  },
)
