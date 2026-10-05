import { afterEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  getMemberState,
  setMemberState,
  validateMemberState,
  validateMemberStateResult,
  validMemberStateReason,
} from './member-state'

const proof = 'a'.repeat(64)
const record = () => ({
  user_id: 'legacy_User-1',
  name: '',
  base_role: 'member',
  disabled: false,
  offboarded_at: null,
  status: 'active',
  can_change_base_role: false,
  can_change_status: true,
  activation_mode: null,
  etag: proof,
  account_access_runtime_applied: false,
})
const old = client.defaults.adapter
afterEach(() => {
  client.defaults.adapter = old
})
describe('current Member state boundary', () => {
  it('preserves legacy safe identity, historical labels, explicit unknown lifecycle and immutable effect', () => {
    const p = { ...record(), name: '\u0001historical' }
    expect(validateMemberState(p, p.user_id)).toEqual(p)
    expect(
      validateMemberStateResult(
        {
          ...p,
          base_role: 'admin',
          confirmation: 'current_member_state',
          effect: 'current_base_identity',
        },
        p.user_id,
        { role: 'admin', reason: 'Controlled change' },
      ).account_access_runtime_applied,
    ).toBe(false)
  })
  it('validates disabled and offboarded activation explicitly without treating role edits as enable', () => {
    for (const [date, mode, status] of [
      [null, 'enable', 'disabled'],
      ['2026-10-05T01:02:03.123456789Z', 'reactivate', 'offboarded'],
    ] as const) {
      const p = { ...record(), disabled: true, offboarded_at: date, activation_mode: mode, status }
      expect(validateMemberState(p, p.user_id)).toEqual(p)
      expect(
        validateMemberStateResult(
          {
            ...p,
            base_role: 'admin',
            confirmation: 'current_member_state',
            effect: 'current_base_identity',
          },
          p.user_id,
          { role: 'admin', reason: 'Role only' },
        ).disabled,
      ).toBe(true)
    }
  })
  it.each([
    [
      'foreign target',
      (p: Record<string, unknown>) => {
        p.user_id = 'LEGACY_User-1'
      },
    ],
    [
      'unknown private field',
      (p: Record<string, unknown>) => {
        p.email = 'forbidden'
      },
    ],
    [
      'unsafe label',
      (p: Record<string, unknown>) => {
        p.name = '\uD800'
      },
    ],
    [
      'uppercase proof',
      (p: Record<string, unknown>) => {
        p.etag = 'A'.repeat(64)
      },
    ],
    [
      'mixed status',
      (p: Record<string, unknown>) => {
        p.status = 'disabled'
      },
    ],
    [
      'active activation',
      (p: Record<string, unknown>) => {
        p.activation_mode = 'enable'
      },
    ],
    [
      'offboard without disabled',
      (p: Record<string, unknown>) => {
        p.offboarded_at = '2026-10-05T00:00:00Z'
      },
    ],
    [
      'false writer string',
      (p: Record<string, unknown>) => {
        p.can_change_status = 'true'
      },
    ],
    [
      'invented proof',
      (p: Record<string, unknown>) => {
        p.account_access_runtime_applied = null
      },
    ],
  ])('rejects %s', (_n, change) => {
    const p: Record<string, unknown> = record()
    change(p)
    expect(() => validateMemberState(p, 'legacy_User-1')).toThrow()
  })
  it.each([
    '0001-01-01T00:00:00Z',
    '0001-01-01T00:00:00.000000000Z',
    '2026-02-30T00:00:00Z',
    '2026-10-05T00:00:00+00:00',
    '2026-10-05T00:00:00.1234567890Z',
  ])('rejects invalid offboarding instant %s', (offboarded_at) => {
    expect(() =>
      validateMemberState(
        {
          ...record(),
          disabled: true,
          status: 'offboarded',
          activation_mode: 'reactivate',
          offboarded_at,
        },
        'legacy_User-1',
      ),
    ).toThrow()
  })
  it('keeps a genuine nonzero year0001 instant', () => {
    expect(
      validateMemberState(
        {
          ...record(),
          disabled: true,
          status: 'offboarded',
          activation_mode: 'reactivate',
          offboarded_at: '0001-01-01T00:00:00.000000001Z',
        },
        'legacy_User-1',
      ).offboarded_at,
    ).toBe('0001-01-01T00:00:00.000000001Z')
  })
  it('requires exact current account state and genuine lifecycle application for account confirmation', () => {
    const p = {
      ...record(),
      disabled: true,
      status: 'disabled',
      activation_mode: 'enable',
      confirmation: 'current_member_state',
      effect: 'current_account_access',
    }
    expect(() =>
      validateMemberStateResult(p, 'legacy_User-1', { disabled: true, reason: 'Disable' }),
    ).toThrow()
    expect(
      validateMemberStateResult({ ...p, account_access_runtime_applied: true }, 'legacy_User-1', {
        disabled: true,
        reason: 'Disable',
      }).disabled,
    ).toBe(true)
    for (const bad of [
      { ...p, effect: 'runtime' },
      { ...p, receipt: 'invented' },
      { ...p, account_access_runtime_applied: true, disabled: false },
      { ...p, account_access_runtime_applied: true, confirmation: 'original_operation' },
    ])
      expect(() =>
        validateMemberStateResult(bad, 'legacy_User-1', { disabled: true, reason: 'Disable' }),
      ).toThrow()
  })
  it.each([
    '',
    ' leading',
    'trailing ',
    'line\nbreak',
    '\uD800',
    'a'.repeat(1025),
    '中'.repeat(342),
  ])('rejects invalid reason %#', (v) => expect(validMemberStateReason(v)).toBe(false))
  it('preserves the byte bound and original reason', () => {
    expect(validMemberStateReason('中'.repeat(341))).toBe(true)
    expect(validMemberStateReason('a'.repeat(1024))).toBe(true)
  })
})
describe('minimal scoped state transport', () => {
  it('uses GET state and exact reasoned PATCH with reviewed proof/current CSRF; returns no receipt', async () => {
    const calls: InternalAxiosRequestConfig[] = []
    client.defaults.adapter = async (config) => {
      calls.push(config)
      const input = config.method === 'patch' ? JSON.parse(config.data) : null
      return {
        config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders({ 'cache-control': 'private, no-store', etag: `"${proof}"` }),
        data: input
          ? {
              ...record(),
              base_role: input.role,
              confirmation: 'current_member_state',
              effect: 'current_base_identity',
            }
          : record(),
      }
    }
    await getMemberState('legacy_User-1')
    const input = { role: 'admin' as const, reason: 'Controlled change' }
    await setMemberState('legacy_User-1', proof, input, 'current-csrf')
    expect(calls.map((c) => [c.method, c.url])).toEqual([
      ['get', '/admin/members/legacy_User-1/state'],
      ['patch', '/admin/members/legacy_User-1'],
    ])
    expect(calls[1].headers.get('If-Match')).toBe(`"${proof}"`)
    expect(calls[1].headers.get('X-CSRF-Token')).toBe('current-csrf')
    expect(JSON.parse(calls[1].data)).toEqual(input)
  })
  it('rejects mixed/null/unknown/empty bodies and malformed identity/review before HTTP', async () => {
    let count = 0
    client.defaults.adapter = async () => {
      count++
      throw new Error('No dispatch')
    }
    for (const input of [
      { role: 'admin', disabled: false, reason: 'Change' },
      { role: null, reason: 'Change' },
      { reason: 'Change' },
      { disabled: false, reason: 'Change', name: 'Forbidden' },
      { role: 'admin', reason: ' trim ' },
    ])
      await expect(setMemberState('legacy_User-1', proof, input as never, 'csrf')).rejects.toThrow()
    await expect(getMemberState('legacy_User-1 ')).rejects.toThrow()
    await expect(
      setMemberState('legacy_User-1', 'W/' + proof, { disabled: true, reason: 'Change' }, 'csrf'),
    ).rejects.toThrow()
    expect(count).toBe(0)
  })
  it.each(['W/"' + proof + '"', '"' + 'b'.repeat(64) + '"'])(
    'rejects weak/mismatched header before facts',
    (header) => {
      client.defaults.adapter = async (config) => ({
        config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders({ 'cache-control': 'private, no-store', etag: header }),
        data: record(),
      })
      return expect(getMemberState('legacy_User-1')).rejects.toThrow()
    },
  )
})
