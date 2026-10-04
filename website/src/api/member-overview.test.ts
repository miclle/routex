import { AxiosHeaders } from 'axios'
import { afterEach, describe, expect, it } from 'vitest'
import client from './client'
import { getMemberOverview } from './member-overview'

const original = client.defaults.adapter
const fixture = () => ({
  user_id: 'usr_target',
  observed_at: '2026-10-04T10:00:00Z',
  platform_currency: 'USD',
  total_personal_keys: '900719925474099312345',
  personal: {
    account_id: 'user:usr_target',
    policy_etag: '0',
    tokens_month: '0',
    money_month: '0.123456789012345678',
    currency: 'USD',
    runtime_applied: false,
    usage_status: 'active',
    usage: {
      as_of: '2026-10-04T09:59:00Z',
      time_zone: 'UTC',
      month_start: '2026-10-01T00:00:00Z',
      month_end: '2026-11-01T00:00:00Z',
      covered: false,
      tokens_used: '9007199254740993',
      tokens_held: '2',
      tokens_unknown: '3',
      money_used: { USD: '0.123456789012345678' },
      money_held: { USD: '0' },
      money_unknown: '4',
    },
    active_reservations: { tokens_held: '7', money_held: { USD: '0' } },
  },
})
afterEach(() => {
  client.defaults.adapter = original
})
describe('scoped administrative Member Overview API', () => {
  it('preserves exact strings and sends only an exact path with cancellation', async () => {
    const value = fixture(),
      abort = new AbortController()
    client.defaults.adapter = async (config) => {
      expect(config.url).toBe('/admin/members/usr_target/overview')
      expect(config.params).toBeUndefined()
      expect(config.signal).toBe(abort.signal)
      return { config, data: value, status: 200, statusText: '', headers: new AxiosHeaders() }
    }
    expect(await getMemberOverview('usr_target', abort.signal)).toEqual(value)
  })
  it.each([
    [
      'foreign subject',
      (v: ReturnType<typeof fixture>) => {
        v.user_id = 'usr_other'
      },
    ],
    [
      'numeric count',
      (v: ReturnType<typeof fixture>) => {
        Object.assign(v, { total_personal_keys: 123 })
      },
    ],
    [
      'negative count',
      (v: ReturnType<typeof fixture>) => {
        v.total_personal_keys = '-1'
      },
    ],
    [
      'leading zero count',
      (v: ReturnType<typeof fixture>) => {
        v.total_personal_keys = '01'
      },
    ],
    [
      'non-UTC observation',
      (v: ReturnType<typeof fixture>) => {
        v.observed_at = '2026-10-04T10:00:00+00:00'
      },
    ],
    [
      'invalid currency',
      (v: ReturnType<typeof fixture>) => {
        v.platform_currency = 'usd'
      },
    ],
    [
      'missing usage',
      (v: ReturnType<typeof fixture>) => {
        Object.assign(v.personal, { usage: null })
      },
    ],
    [
      'numeric tokens',
      (v: ReturnType<typeof fixture>) => {
        Object.assign(v.personal.usage, { tokens_used: 123 })
      },
    ],
    [
      'negative money',
      (v: ReturnType<typeof fixture>) => {
        v.personal.usage.money_used.USD = '-1'
      },
    ],
    [
      'exponent money',
      (v: ReturnType<typeof fixture>) => {
        v.personal.money_month = '1e-8'
      },
    ],
    [
      'missing money denomination',
      (v: ReturnType<typeof fixture>) => {
        Object.assign(v.personal, { currency: null })
      },
    ],
    [
      'unlimited money with denomination',
      (v: ReturnType<typeof fixture>) => {
        Object.assign(v.personal, { money_month: null })
      },
    ],
    [
      'live snapshot missing',
      (v: ReturnType<typeof fixture>) => {
        Object.assign(v.personal, { active_reservations: null })
      },
    ],
    [
      'invalid window',
      (v: ReturnType<typeof fixture>) => {
        v.personal.usage.month_end = v.personal.usage.month_start
      },
    ],
    [
      'unavailable with usage',
      (v: ReturnType<typeof fixture>) => {
        v.personal.usage_status = 'unavailable'
      },
    ],
  ])('rejects %s', async (_, mutate) => {
    const value = fixture()
    mutate(value)
    client.defaults.adapter = async (config) => ({
      config,
      data: value,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
    })
    await expect(getMemberOverview('usr_target')).rejects.toThrow(
      'Invalid member overview response',
    )
  })
  it('accepts unavailable facts as null rather than inventing zero', async () => {
    const value = fixture()
    Object.assign(value.personal, {
      usage_status: 'unavailable',
      usage: null,
      active_reservations: null,
      tokens_month: null,
      money_month: null,
      currency: null,
    })
    client.defaults.adapter = async (config) => ({
      config,
      data: value,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
    })
    expect((await getMemberOverview('usr_target')).personal.usage).toBeNull()
  })
  it.each([' usr_target', 'usr_target/other', 'usr_target?query=1', 'usr_target '.repeat(3)])(
    'rejects unsafe subject %s before dispatch',
    async (target) => {
      client.defaults.adapter = async () => {
        throw new Error('Dispatched')
      }
      await expect(getMemberOverview(target)).rejects.toThrow('Invalid member identity')
    },
  )
})
