import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it } from 'vitest'
import client from './client'
import {
  getMemberMetadata,
  setMemberMetadata,
  validMemberName,
  validMemberMetadataReason,
} from './member-metadata'
const original = client.defaults.adapter
const revision = 'a'.repeat(64)
let value: Record<string, unknown>, header: string, requests: InternalAxiosRequestConfig[]
beforeEach(() => {
  value = {
    user_id: 'usr_retained',
    name: 'Recorded\tname',
    status: 'disabled',
    can_edit: true,
    etag: revision,
  }
  header = `"${revision}"`
  requests = []
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ ETag: header }),
      data: value,
    }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
it('reads exact retained metadata and strong revision without exposing email or rejecting old labels', async () => {
  const signal = new AbortController().signal
  expect(await getMemberMetadata('usr_retained', signal)).toEqual(value)
  expect(requests[0].url).toBe('/admin/members/usr_retained/metadata')
  expect(requests[0].signal).toBe(signal)
  expect(requests[0].params).toBeUndefined()
  expect(requests[0].withCredentials).toBe(true)
  expect(requests[0].headers.has('Authorization')).toBe(false)
})
it('submits only exact captured name and reason with strong If-Match and current CSRF', async () => {
  value = { ...value, name: '😀'.repeat(100), confirmation: 'current_member_name' }
  const input = { name: String(value.name), reason: 'é'.repeat(512) }
  await setMemberMetadata('usr_retained', revision, input, 'current_csrf')
  expect(JSON.parse(requests[0].data)).toEqual(input)
  expect(requests[0].method).toBe('put')
  expect(requests[0].headers.get('If-Match')).toBe(`"${revision}"`)
  expect(requests[0].headers.get('X-CSRF-Token')).toBe('current_csrf')
})
it.each(['', 'unsafe/id', 'x'.repeat(31), 'usr_retained '])(
  'rejects unsafe path %s before a request',
  async (id) => {
    await expect(getMemberMetadata(id)).rejects.toThrow()
    expect(requests).toHaveLength(0)
  },
)
it.each([
  { user_id: 'usr_other' },
  { email: 'private@example.invalid' },
  { etag: 'not-review' },
  { can_edit: 'true' },
  { status: 'unknown' },
  { status: ['active'] },
  { name: '\ud800' },
  { name: null },
  { status: 'offboarded', can_edit: true },
])('rejects malformed or mismatched metadata %j', async (patch) => {
  value = { ...value, ...patch }
  await expect(getMemberMetadata('usr_retained')).rejects.toThrow()
})
it.each(['', revision, `W/"${revision}"`, `"${'b'.repeat(64)}"`])(
  'rejects mismatched/weak/unquoted ETag %s',
  async (bad) => {
    header = bad
    await expect(getMemberMetadata('usr_retained')).rejects.toThrow()
  },
)
it.each([
  { confirmation: 'historical_saved' },
  { name: 'Different' },
  { can_edit: false },
  { runtime_applied: true },
])('never accepts a mismatched current-state confirmation %j', async (patch) => {
  value = { ...value, name: 'New name', confirmation: 'current_member_name', ...patch }
  await expect(
    setMemberMetadata('usr_retained', revision, { name: 'New name', reason: 'Review' }, 'csrf'),
  ).rejects.toThrow()
})
it('validates Unicode name codepoints and reason bytes without normalizing content', async () => {
  expect(validMemberName('😀'.repeat(100))).toBe(true)
  for (const name of ['', ' padded ', 'a\tb', '😀'.repeat(101), '\ud800'])
    expect(validMemberName(name)).toBe(false)
  expect(validMemberMetadataReason('é'.repeat(512))).toBe(true)
  expect(validMemberMetadataReason('é'.repeat(512) + 'a')).toBe(false)
  expect(validMemberMetadataReason(' reason ')).toBe(false)
  expect(validMemberMetadataReason('a\nb')).toBe(false)
  await expect(
    setMemberMetadata('usr_retained', revision, { name: 'Name', reason: ' bad ' }, 'csrf'),
  ).rejects.toThrow()
  expect(requests).toHaveLength(0)
})
it('discards an aborted otherwise valid response', async () => {
  const pending = new AbortController()
  pending.abort()
  await expect(getMemberMetadata('usr_retained', pending.signal)).rejects.toThrow()
})
