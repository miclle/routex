import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from './client'
import {
  getDefaultLimits,
  saveDefaultLimits,
  getDefaultReset,
  restoreDefaultLimits,
} from './default-limits'
import { teamFixture } from '@/views/resource-limits/fixture'
import type { DefaultLimitRecord, DefaultLimitResetContext } from '@/types/default-limits'
const original = client.defaults.adapter
let response: unknown, record: DefaultLimitRecord, context: DefaultLimitResetContext
beforeEach(() => {
  record = {
    kind: 'team',
    etag: 'a'.repeat(64),
    rule_etag: 'b'.repeat(64),
    policy: {
      tokens_5h: null,
      tokens_7d: 0,
      tokens_month: 10,
      money_month: '999999999999999999.123456789012345678',
      currency: 'USD',
      rpm: null,
      tpm: 1,
      concurrency: 0,
    },
    platform_currency: 'USD',
    editable: true,
    updated_at: '2026-10-03T01:02:03Z',
  }
  context = {
    kind: 'team',
    id: 'tea_test',
    etag: 'c'.repeat(64),
    default_rule: record,
    limit: teamFixture(),
    applied_default_etag: null,
    editable: true,
  }
  response = record
  client.defaults.adapter = async (config) => ({
    config,
    status: 200,
    statusText: '',
    headers: new AxiosHeaders(),
    data: structuredClone(response),
  })
})
afterEach(() => {
  client.defaults.adapter = original
})
describe('Default limit API contracts', () => {
  it('preserves exact strings, explicit zero and null', async () => {
    expect((await getDefaultLimits('team')).policy).toEqual(record.policy)
  })
  it.each([
    { kind: 'user' },
    { etag: 'weak' },
    { rule_etag: '' },
    { editable: undefined },
    { updated_at: 'unknown' },
    { platform_currency: 'usd' },
    { policy: {} },
  ])('rejects incomplete or cross-scope default record %j', async (patch) => {
    response = { ...record, ...patch }
    await expect(getDefaultLimits('team')).rejects.toThrow()
  })
  it.each([-1, 1.5, Number.MAX_SAFE_INTEGER + 1, undefined])(
    'rejects invalid integer %s',
    async (value) => {
      response = { ...record, policy: { ...record.policy, tpm: value } }
      await expect(getDefaultLimits('team')).rejects.toThrow()
    },
  )
  it('requires matching full submitted policy for save acknowledgement', async () => {
    response = { ...record, policy: { ...record.policy, tokens_month: 11 } }
    await expect(
      saveDefaultLimits(
        'team',
        record.etag,
        { policy: record.policy, reason: 'Controlled' },
        'csrf',
      ),
    ).rejects.toThrow('Unconfirmed')
  })
  it('requires exact target IDs in restore preview', async () => {
    response = { ...context, id: 'tea_other' }
    await expect(getDefaultReset({ kind: 'team', id: 'tea_test' })).rejects.toThrow()
  })
  it.each(['saved', 'runtime_applied', 'default_reset_etag', 'applied_default_etag', 'kind', 'id'])(
    'rejects malformed reset acknowledgement %s',
    async (field) => {
      response = {
        kind: 'team',
        id: 'tea_test',
        saved: true,
        default_reset_etag: context.etag,
        applied_default_etag: record.rule_etag,
        runtime_applied: true,
        limit: { ...context.limit, stored: { ...context.limit.stored, ...record.policy } },
        [field]: undefined,
      }
      await expect(
        restoreDefaultLimits({ kind: 'team', id: 'tea_test' }, context, 'Controlled', 'csrf'),
      ).rejects.toThrow()
    },
  )
  it('rejects an acknowledgement that changes preserved IP rules', async () => {
    response = {
      kind: 'team',
      id: 'tea_test',
      saved: true,
      default_reset_etag: context.etag,
      applied_default_etag: record.rule_etag,
      runtime_applied: true,
      limit: {
        ...context.limit,
        stored: {
          ...context.limit.stored,
          ...record.policy,
          ip_mode: 'denylist',
          ip_ranges: ['192.0.2.0/24'],
        },
      },
    }
    await expect(
      restoreDefaultLimits({ kind: 'team', id: 'tea_test' }, context, 'Controlled', 'csrf'),
    ).rejects.toThrow()
  })
})
