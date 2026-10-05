import { afterEach, expect, it, vi } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from './client'
import {
  getRegistrationPolicy,
  getRegistrationPolicyReview,
  setRegistrationPolicy,
} from './registration-approval'
import type { RegistrationPolicyInput } from '@/types/registration-approval'
const etag = 'a'.repeat(64)
afterEach(() => vi.restoreAllMocks())
it.each([
  ['missing', undefined],
  ['null', null],
  ['unsorted', ['z.example', 'a.example']],
  ['duplicate', ['a.example', 'a.example']],
  ['noncanonical', ['A.example']],
  ['numeric name', ['127.1']],
  ['suffix wildcard', ['*.example']],
  ['non-string', [2]],
  [
    'count overflow',
    Array.from({ length: 33 }, (_, i) => `d${String(i).padStart(2, '0')}.example`),
  ],
  [
    'byte overflow',
    Array.from({ length: 32 }, (_, i) => `${'a'.repeat(60)}${String(i).padStart(2, '0')}.example`),
  ],
])('rejects %s domain metadata', async (_name, domains) => {
  const data: Record<string, unknown> = {
    enabled: true,
    approval_required: false,
    allowed_email_domains: domains,
  }
  if (domains === undefined) delete data.allowed_email_domains
  vi.spyOn(client, 'get').mockResolvedValue({ status: 200, data, headers: new AxiosHeaders() })
  await expect(getRegistrationPolicy()).rejects.toThrow()
})
it('does not disclose configured domains through disabled public metadata or private fields', async () => {
  const get = vi.spyOn(client, 'get').mockResolvedValue({
    status: 200,
    data: { enabled: false, approval_required: true, allowed_email_domains: ['example.invalid'] },
    headers: new AxiosHeaders(),
  })
  await expect(getRegistrationPolicy()).rejects.toThrow()
  get.mockResolvedValue({
    status: 200,
    data: { enabled: true, approval_required: true, allowed_email_domains: [], review_etag: etag },
    headers: new AxiosHeaders(),
  })
  await expect(getRegistrationPolicy()).rejects.toThrow()
})
it('copies the complete dispatched domains before awaiting and compares current confirmation to that exact captured body', async () => {
  let release!: (value: unknown) => void
  const pending = new Promise((resolve) => {
    release = resolve
  })
  const patch = vi
    .spyOn(client, 'patch')
    .mockReturnValue(pending as ReturnType<typeof client.patch>)
  const input = {
    enabled: true,
    approval_required: true,
    allowed_email_domains: ['example.invalid'],
    reason: 'Exact reviewed policy',
  }
  const request = setRegistrationPolicy(etag, input, 'current')
  input.allowed_email_domains.push('sub.example.invalid')
  expect(patch.mock.calls[0][1]).toEqual({
    enabled: true,
    approval_required: true,
    allowed_email_domains: ['example.invalid'],
    reason: 'Exact reviewed policy',
  })
  release({
    status: 200,
    data: {
      enabled: true,
      approval_required: true,
      allowed_email_domains: ['example.invalid'],
      review_etag: etag,
      confirmation: 'current_registration_policy',
    },
    headers: new AxiosHeaders({ ETag: `"${etag}"` }),
  })
  expect((await request).allowed_email_domains).toEqual(['example.invalid'])
})
it('rejects a changed domain receipt and incomplete replacement without sending an unsafe writer', async () => {
  const patch = vi.spyOn(client, 'patch').mockResolvedValue({
    status: 200,
    data: {
      enabled: true,
      approval_required: false,
      allowed_email_domains: ['different.example'],
      review_etag: etag,
      confirmation: 'current_registration_policy',
    },
    headers: new AxiosHeaders({ ETag: `"${etag}"` }),
  })
  const input = {
    enabled: true,
    approval_required: false,
    allowed_email_domains: ['example.invalid'],
    reason: 'Reviewed policy',
  }
  await expect(setRegistrationPolicy(etag, input, 'current')).rejects.toThrow()
  patch.mockClear()
  await expect(
    setRegistrationPolicy(
      etag,
      {
        enabled: true,
        approval_required: false,
        reason: 'Reviewed policy',
      } as RegistrationPolicyInput,
      'current',
    ),
  ).rejects.toThrow()
  expect(patch).not.toHaveBeenCalled()
  vi.spyOn(client, 'get').mockResolvedValue({
    status: 200,
    data: { ...input, review_etag: etag },
    headers: new AxiosHeaders({ ETag: `"${etag}"` }),
  })
  await expect(getRegistrationPolicyReview()).rejects.toThrow()
})
