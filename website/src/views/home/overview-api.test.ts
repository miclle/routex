import { afterEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { getOverviewAccounts } from '@/api/overview'
import type { MonthlyAccount, OverviewAccountsPage } from '@/types/overview'
import { exactDecimal, exactInteger, tokenPercentage } from './monthly-account-values'

const account = (): MonthlyAccount => ({
  account_id: 'member_digest',
  policy_etag: '0',
  tokens_month: '9007199254740993',
  money_month: '0.123456789012345678',
  currency: 'USD',
  runtime_applied: false,
  usage_status: 'active',
  active_reservations: {
    tokens_held: '9007199254740993',
    money_held: { USD: '5.000000000000000002' },
  },
  usage: {
    as_of: '2026-10-04T09:03:00Z',
    time_zone: 'UTC',
    month_start: '2026-10-01T00:00:00Z',
    month_end: '2026-11-01T00:00:00Z',
    covered: true,
    tokens_used: '9007199254740992',
    tokens_held: '0',
    tokens_unknown: '0',
    money_used: { USD: '0.000000000000000002' },
    money_held: {},
    money_unknown: '0',
  },
})
const page = (): OverviewAccountsPage => ({
  actor_user_id: 'usr_member',
  observed_at: '2026-10-04T09:03:00Z',
  platform_currency: 'USD',
  personal: account(),
  teams: [
    {
      id: 'legacy_team',
      name: 'Exact Team',
      membership_id: 'legacy_membership',
      aggregate: account(),
      member: account(),
    },
  ],
  next_cursor: null,
})
afterEach(() => vi.restoreAllMocks())

describe('self-only Overview response boundary', () => {
  it('uses only bounded cursor/limit and AbortSignal, accepting exact legacy identities and strings', async () => {
    const data = page()
    const get = vi.spyOn(client, 'get').mockResolvedValue({ data })
    const controller = new AbortController()
    expect(await getOverviewAccounts('usr_member', 'scoped-cursor', controller.signal)).toEqual(
      data,
    )
    expect(get).toHaveBeenCalledWith('/overview/accounts', {
      params: { cursor: 'scoped-cursor', limit: 10 },
      signal: controller.signal,
    })
  })
  it.each([
    [
      'missing live reservations',
      (p: OverviewAccountsPage) => {
        delete (p.personal as Partial<MonthlyAccount>).active_reservations
      },
    ],
    [
      'null live reservations in active journal',
      (p: OverviewAccountsPage) => {
        p.personal.active_reservations = null
      },
    ],
    [
      'numeric live counter',
      (p: OverviewAccountsPage) => {
        p.personal.active_reservations!.tokens_held = 5 as unknown as string
      },
    ],
    [
      'negative live counter',
      (p: OverviewAccountsPage) => {
        p.personal.active_reservations!.tokens_held = '-1'
      },
    ],
    [
      'unsafe live counter',
      (p: OverviewAccountsPage) => {
        p.personal.active_reservations!.tokens_held = '1'.repeat(129)
      },
    ],
    [
      'exponent live money',
      (p: OverviewAccountsPage) => {
        p.personal.active_reservations!.money_held.USD = '5e1'
      },
    ],
    [
      'negative live money',
      (p: OverviewAccountsPage) => {
        p.personal.active_reservations!.money_held.USD = '-1'
      },
    ],
    [
      'invalid live currency',
      (p: OverviewAccountsPage) => {
        p.personal.active_reservations!.money_held.usd = '1'
      },
    ],
    [
      'fabricated unavailable live reservations',
      (p: OverviewAccountsPage) => {
        p.personal.usage_status = 'unavailable'
        p.personal.usage = null
      },
    ],
    [
      'wrong actor',
      (p: OverviewAccountsPage) => {
        p.actor_user_id = 'usr_other'
      },
    ],
    [
      'unsafe target',
      (p: OverviewAccountsPage) => {
        p.teams[0].id = '../team'
      },
    ],
    [
      'unsafe membership',
      (p: OverviewAccountsPage) => {
        p.teams[0].membership_id = 'member/slash'
      },
    ],
    [
      'duplicate Team',
      (p: OverviewAccountsPage) => {
        p.teams.push(p.teams[0])
      },
    ],
    [
      'over-page',
      (p: OverviewAccountsPage) => {
        p.teams = Array.from({ length: 11 }, (_, i) => ({ ...p.teams[0], id: 'team_' + i }))
      },
    ],
    [
      'number counter',
      (p: OverviewAccountsPage) => {
        p.personal.usage!.tokens_used = 5 as unknown as string
      },
    ],
    [
      'negative counter',
      (p: OverviewAccountsPage) => {
        p.personal.usage!.tokens_unknown = '-1'
      },
    ],
    [
      'exponent money',
      (p: OverviewAccountsPage) => {
        p.personal.usage!.money_used.USD = '1e3'
      },
    ],
    [
      'negative money',
      (p: OverviewAccountsPage) => {
        p.personal.money_month = '-2'
      },
    ],
    [
      'unsafe counter',
      (p: OverviewAccountsPage) => {
        p.personal.usage!.tokens_held = '1'.repeat(129)
      },
    ],
    [
      'noncanonical counter',
      (p: OverviewAccountsPage) => {
        p.personal.tokens_month = '01'
      },
    ],
    [
      'bad currency',
      (p: OverviewAccountsPage) => {
        p.personal.currency = ''
      },
    ],
    [
      'unexpected denomination',
      (p: OverviewAccountsPage) => {
        p.personal.usage!.money_used['not-money'] = '2'
      },
    ],
    [
      'no finite currency',
      (p: OverviewAccountsPage) => {
        p.personal.money_month = null
      },
    ],
    [
      'missing active usage',
      (p: OverviewAccountsPage) => {
        p.personal.usage = null
      },
    ],
    [
      'invented inactive usage',
      (p: OverviewAccountsPage) => {
        p.personal.usage_status = 'inactive'
      },
    ],
    [
      'unknown usage status',
      (p: OverviewAccountsPage) => {
        p.personal.usage_status = 'future' as MonthlyAccount['usage_status']
      },
    ],
    [
      'invalid zone',
      (p: OverviewAccountsPage) => {
        p.personal.usage!.time_zone = 'Local'
      },
    ],
    [
      'no zone stamp',
      (p: OverviewAccountsPage) => {
        p.personal.usage!.as_of = '2026-10-04'
      },
    ],
    [
      'reversed month',
      (p: OverviewAccountsPage) => {
        p.personal.usage!.month_end = p.personal.usage!.month_start
      },
    ],
    [
      'missing cursor',
      (p: OverviewAccountsPage) => {
        delete (p as Partial<OverviewAccountsPage>).next_cursor
      },
    ],
    [
      'repeated cursor',
      (p: OverviewAccountsPage) => {
        p.next_cursor = 'cursor'
      },
    ],
  ])('rejects %s rather than displaying private malformed facts', async (_, mutate) => {
    const data = page()
    mutate(data)
    vi.spyOn(client, 'get').mockResolvedValue({ data })
    await expect(getOverviewAccounts('usr_member', 'cursor')).rejects.toThrow(
      'Invalid monthly overview response',
    )
  })
  it.each(['inactive', 'unavailable'] as const)(
    'preserves null %s usage and zero-versus-null local policies',
    async (status) => {
      const data = page()
      data.personal = {
        ...account(),
        tokens_month: '0',
        money_month: null,
        currency: null,
        usage_status: status,
        usage: null,
        active_reservations: null,
      }
      vi.spyOn(client, 'get').mockResolvedValue({ data })
      expect((await getOverviewAccounts('usr_member', null)).personal).toEqual(data.personal)
    },
  )
  it('propagates denied reads without fabricating empty or zero data', async () => {
    vi.spyOn(client, 'get').mockRejectedValue(new Error('denied'))
    await expect(getOverviewAccounts('usr_member', null)).rejects.toThrow('denied')
  })
})

describe('exact monthly account values', () => {
  it('does quota division beyond the safe-number range without rounding a zero denominator', () => {
    const value = account()
    expect(tokenPercentage(value)).toEqual({ whole: 99n, fraction: 99n, bar: 99.99 })
    value.tokens_month = '0'
    expect(tokenPercentage(value)).toBeNull()
    value.tokens_month = null
    expect(tokenPercentage(value)).toBeNull()
  })
  it('bounds only the visual bar and preserves a greater-than-limit recorded percentage', () => {
    const value = account()
    value.tokens_month = '1'
    value.usage!.tokens_used = '9007199254740993'
    expect(tokenPercentage(value)).toEqual({ whole: 900719925474099300n, fraction: 0n, bar: 100 })
  })
  it.each(['uncovered', 'unknown', 'unavailable'] as const)(
    'does not invent a percent for %s usage',
    (kind) => {
      const value = account()
      if (kind === 'uncovered') value.usage!.covered = false
      if (kind === 'unknown') value.usage!.tokens_unknown = '1'
      if (kind === 'unavailable') {
        value.usage_status = 'unavailable'
        value.usage = null
      }
      expect(tokenPercentage(value)).toBeNull()
    },
  )
  it('keeps every decimal digit and exact integer in both selected locales', () => {
    for (const language of ['en-US', 'zh-CN']) {
      expect(exactInteger('9007199254740993', language)).toBe('9,007,199,254,740,993')
      expect(exactDecimal('0.123456789012345678', language)).toBe('0.123456789012345678')
      expect(exactDecimal('9007199254740993.000000000000000002', language)).toBe(
        '9,007,199,254,740,993.000000000000000002',
      )
    }
  })
})
