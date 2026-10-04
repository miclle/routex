import { afterEach, beforeEach, expect, it } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from './client'
import { disableMemberKey, getMemberKey, listMemberKeys, validMemberKeyReason } from './member-keys'
import {
  memberKeyFixture,
  memberKeysID,
  memberKeysUser,
} from '@/views/governance/member-keys.fixture'
let data: unknown, headers: AxiosHeaders
const old = client.defaults.adapter
beforeEach(() => {
  data = memberKeyFixture()
  headers = new AxiosHeaders({ ETag: `"${'a'.repeat(64)}"` })
  client.defaults.adapter = async (config) => ({
    config,
    data,
    headers,
    status: 200,
    statusText: '',
  })
})
afterEach(() => {
  client.defaults.adapter = old
})
it('preserves exact string counters, money, ceilings and empty currency policy', async () => {
  const row = memberKeyFixture()
  row.limits.effective.money_month = null
  row.limits.effective.currency = ''
  data = row
  const got = await getMemberKey(memberKeysUser, memberKeysID)
  expect(got.limits.quota_usage!.month!.tokens_used).toBe('9007199254740993')
  expect(got.limits.quota_usage!.active!.money_held.USD).toBe('5.000000000000000002')
})
it.each(['prefix', 'secret', 'token_hash', 'lifecycle_revision'])(
  'rejects unexpected private field %s',
  async (key) => {
    data = { ...memberKeyFixture(), [key]: 'private' }
    await expect(getMemberKey(memberKeysUser, memberKeysID)).rejects.toThrow('unavailable')
  },
)
it.each(['money_unknown', 'tokens_used', 'tokens_held', 'tokens_unknown'])(
  'rejects unsafe number counters %s',
  async (field) => {
    const row = memberKeyFixture()
    Object.assign(row.limits.quota_usage!.month!, { [field]: 9007199254740992 })
    data = row
    await expect(getMemberKey(memberKeysUser, memberKeysID)).rejects.toThrow()
  },
)
it.each(['-1', '01', '1.1', '1e3', ''])('rejects malformed exact counter %s', async (value) => {
  const row = memberKeyFixture()
  row.limits.active = value
  data = row
  await expect(getMemberKey(memberKeysUser, memberKeysID)).rejects.toThrow()
})
it.each([
  'bad_currency',
  'null_policy',
  'bad_date',
  'wrong_key',
  'weak_etag',
  'invalid_eligible',
  'inconsistent_last_use',
  'unsafe_policy',
])('rejects %s', async (kind) => {
  const row = memberKeyFixture()
  if (kind === 'bad_currency') row.limits.effective.currency = 'usd'
  if (kind === 'null_policy') row.limits.effective.money_month = null
  if (kind === 'bad_date') row.created_at = 'today'
  if (kind === 'wrong_key') row.id = 'key_01dddddddddddddddddddddddd'
  if (kind === 'weak_etag') headers.set('ETag', `W/"${row.etag}"`)
  if (kind === 'invalid_eligible') row.status = 'disabled'
  if (kind === 'inconsistent_last_use') row.last_use_coverage = 'recorded'
  if (kind === 'unsafe_policy') row.limits.effective.rpm = Number.MAX_SAFE_INTEGER + 1
  data = row
  await expect(getMemberKey(memberKeysUser, memberKeysID)).rejects.toThrow()
})
it('bounds canonical descending pages and prevents looping/duplicate cursors', async () => {
  data = { items: [memberKeyFixture()], next_cursor: memberKeysID }
  expect((await listMemberKeys(memberKeysUser, null)).next_cursor).toBe(memberKeysID)
  await expect(listMemberKeys(memberKeysUser, memberKeysID)).rejects.toThrow()
  data = { items: [memberKeyFixture(), memberKeyFixture()], next_cursor: null }
  await expect(listMemberKeys(memberKeysUser, null)).rejects.toThrow()
})
it('confirms exact current disabled result only with a matching strong response revision', async () => {
  data = {
    user_id: memberKeysUser,
    id: memberKeysID,
    status: 'disabled',
    etag: 'a'.repeat(64),
    runtime_applied: true,
    confirmation: 'current_disabled_state',
  }
  expect(
    (
      await disableMemberKey(
        memberKeysUser,
        memberKeysID,
        'b'.repeat(64),
        'Reviewed reason',
        'csrf',
      )
    ).confirmation,
  ).toBe('current_disabled_state')
  Object.assign(data!, { runtime_applied: false })
  await expect(
    disableMemberKey(memberKeysUser, memberKeysID, 'b'.repeat(64), 'Reviewed reason', 'csrf'),
  ).rejects.toThrow()
})
it('requires noncontrol bounded UTF-8 reason', () => {
  expect(validMemberKeyReason('原因')).toBe(true)
  expect(validMemberKeyReason('  ')).toBe(false)
  expect(validMemberKeyReason('x\nreason')).toBe(false)
  expect(validMemberKeyReason('界'.repeat(342))).toBe(false)
})

it.each(['subject', 'key', 'status', 'confirmation', 'header', 'extra'])(
  'does not confirm mismatched current-state response: %s',
  async (kind) => {
    data = {
      user_id: memberKeysUser,
      id: memberKeysID,
      status: 'disabled',
      etag: 'a'.repeat(64),
      runtime_applied: true,
      confirmation: 'current_disabled_state',
    }
    if (kind === 'subject') Object.assign(data!, { user_id: 'usr_01dddddddddddddddddddddddd' })
    if (kind === 'key') Object.assign(data!, { id: 'key_01dddddddddddddddddddddddd' })
    if (kind === 'status') Object.assign(data!, { status: 'active' })
    if (kind === 'confirmation') Object.assign(data!, { confirmation: 'historical_commit' })
    if (kind === 'header') headers.set('ETag', `"${'b'.repeat(64)}"`)
    if (kind === 'extra') Object.assign(data!, { secret: 'hidden' })
    await expect(
      disableMemberKey(memberKeysUser, memberKeysID, 'c'.repeat(64), 'Reason', 'csrf'),
    ).rejects.toThrow()
  },
)
