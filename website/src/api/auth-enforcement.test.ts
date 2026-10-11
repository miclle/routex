import { afterEach, beforeEach, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  beginEmergencyLogin,
  completeEmergencyLogin,
  decodeAuthenticationPolicy,
  decodeSecuritySession,
  getAuthenticationPolicyReview,
  saveAuthenticationPolicy,
} from './auth-enforcement'
import { changePassword } from './account'
import { decodeMFARecovery } from './mfa'
const original = client.defaults.adapter
const candidate = 'usr_' + '0'.repeat(25) + 'a'
const token = 'a'.repeat(64)
const session = {
  user: { id: candidate, email: 'admin@example.test', name: 'Admin', role: 'admin' },
  csrf_token: 'fresh',
}
const base = {
  enforced: false,
  emergency_admin_id: null,
  emergency_admin_created_at: null,
  emergency_mfa_enabled: null,
  oidc_available: true,
  ldap_available: false,
  review_emergency_admin: null,
  review_etag: token,
}
let requests: InternalAxiosRequestConfig[], body: unknown, status: number
beforeEach(() => {
  requests = []
  body = { ...base }
  status = 200
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return {
      config,
      status,
      statusText: '',
      headers: new AxiosHeaders({
        'Cache-Control': 'private, no-store',
        'X-Content-Type-Options': 'nosniff',
        ETag: `"${token}"`,
      }),
      data: body,
    }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
it.each([false, true])(
  'requires the exact public flag truth table for enforcement %s',
  (enforced) => {
    expect(
      decodeAuthenticationPolicy({
        enforced,
        password_login_enabled: !enforced,
        emergency_entry_enabled: enforced,
      }),
    ).toEqual({ enforced, password_login_enabled: !enforced, emergency_entry_enabled: enforced })
    expect(() =>
      decodeAuthenticationPolicy({
        enforced,
        password_login_enabled: !enforced,
        emergency_entry_enabled: !enforced,
      }),
    ).toThrow()
    expect(() =>
      decodeAuthenticationPolicy({
        enforced,
        password_login_enabled: !enforced,
        emergency_entry_enabled: enforced,
        designation: candidate,
      }),
    ).toThrow()
  },
)
it('distinguishes stored designation from no-query disable and exact candidate review', async () => {
  body = {
    ...base,
    enforced: true,
    emergency_admin_id: candidate,
    emergency_admin_created_at: '2026-10-11T00:00:00.123456Z',
    emergency_mfa_enabled: true,
  }
  expect((await getAuthenticationPolicyReview()).review_emergency_admin).toBeNull()
  const selected = { id: candidate, created_at: '2026-10-11T00:00:00.123456Z', mfa_enabled: true }
  body = { ...(body as object), review_emergency_admin: selected }
  await getAuthenticationPolicyReview(undefined, candidate)
  expect(requests[1].url).toBe('/admin/auth/policy?emergency_admin_id=' + candidate)
  await expect(getAuthenticationPolicyReview()).rejects.toThrow()
  await expect(getAuthenticationPolicyReview(undefined, 'usr_other')).rejects.toThrow()
  expect(requests).toHaveLength(3)
})
it('accepts saved self-revocation without a replacement Session or protected reread', async () => {
  body = { ...base, confirmation: 'saved', runtime_applied: false, reauthentication_required: true }
  const receipt = await saveAuthenticationPolicy(
    { enforced: false, emergency_admin_id: '', reason: 'Explicit reviewed disable' },
    token,
    'fresh',
  )
  expect(receipt.reauthentication_required).toBe(true)
  expect(receipt.runtime_applied).toBe(false)
  expect(requests).toHaveLength(1)
  expect(requests[0].headers.get('If-Match')).toBe(`"${token}"`)
  expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh')
  body = { ...(body as object), reauthentication_required: undefined }
  await expect(
    saveAuthenticationPolicy(
      { enforced: false, emergency_admin_id: '', reason: 'Reviewed' },
      token,
      'fresh',
    ),
  ).rejects.toThrow()
})
it('never treats emergency password Session200 as proof and accepts only challenge202', async () => {
  body = session
  await expect(
    beginEmergencyLogin({ email: 'admin@example.test', password: 'current password' }),
  ).rejects.toThrow()
  status = 202
  body = {
    mfa_required: true,
    challenge_token: 'T'.repeat(43),
    expires_at: new Date(Date.now() + 60000).toISOString(),
    methods: ['totp', 'recovery_code'],
  }
  expect(
    (await beginEmergencyLogin({ email: 'admin@example.test', password: 'current password' }))
      .mfa_required,
  ).toBe(true)
  status = 200
  body = session
  expect(await completeEmergencyLogin('T'.repeat(43), { recovery_code: 'RXR-owned' })).toEqual(
    session,
  )
  expect(JSON.parse(requests[2].data)).toEqual({
    challenge_token: 'T'.repeat(43),
    recovery_code: 'RXR-owned',
  })
  const malformed = { code: '123456', recovery_code: 'RXR-owned' } as unknown as Parameters<
    typeof completeEmergencyLogin
  >[1]
  await expect(completeEmergencyLogin('T'.repeat(43), malformed)).rejects.toThrow()
  expect(requests).toHaveLength(3)
})
it('decodes only the two closed security receipt alternatives and owned recovery arrays', async () => {
  expect(decodeSecuritySession(session)).toEqual(session)
  expect(decodeSecuritySession({ reauthentication_required: true })).toEqual({
    reauthentication_required: true,
  })
  expect(() => decodeSecuritySession({ reauthentication_required: false })).toThrow()
  expect(() => decodeSecuritySession({ ...session, reauthentication_required: true })).toThrow()
  const codes = Array.from({ length: 10 }, (_, i) => 'RXR-OWNED-' + i)
  const receipt = decodeMFARecovery({
    session: null,
    recovery_codes: codes,
    reauthentication_required: true,
  })
  codes[0] = 'mutated'
  expect(receipt.recovery_codes[0]).toBe('RXR-OWNED-0')
  expect(() => decodeMFARecovery({ session: null, recovery_codes: codes })).toThrow()
  expect(() =>
    decodeMFARecovery({ session, reauthentication_required: true, recovery_codes: codes }),
  ).toThrow()
  body = { reauthentication_required: true }
  expect(
    await changePassword(
      { current_password: 'old password', new_password: 'new password' },
      'fresh',
    ),
  ).toEqual({ reauthentication_required: true })
})
it('sanitizes failed password requests rather than retaining submitted credentials in errors', async () => {
  status = 409
  const secret = 'sensitive password'
  const error = await changePassword(
    { current_password: secret, new_password: secret },
    'fresh',
  ).catch((e) => e)
  expect(error.status).toBe(409)
  expect(JSON.stringify(error)).not.toContain(secret)
  expect(error.config).toBeUndefined()
})

// Canonical RouteX user IDs are lowercase ULIDs with the first digit in 0–7.
it.each([
  ['uppercase suffix', 'usr_' + '0'.repeat(25) + 'A'],
  ['uppercase prefix', 'USR_' + '0'.repeat(25) + 'a'],
  ['overflow leading digit', 'usr_8' + '0'.repeat(24) + 'a'],
  ['excluded lowercase character', 'usr_' + '0'.repeat(25) + 'i'],
  ['trailing newline', candidate + '\n'],
])(
  'rejects a %s designation before transport and in a received review',
  async (_label, invalidID) => {
    await expect(getAuthenticationPolicyReview(undefined, invalidID)).rejects.toThrow()
    await expect(
      saveAuthenticationPolicy(
        { enforced: true, emergency_admin_id: invalidID, reason: 'Reviewed designation' },
        token,
        'fresh',
      ),
    ).rejects.toThrow()
    expect(requests).toHaveLength(0)
    body = {
      ...base,
      review_emergency_admin: {
        id: invalidID,
        created_at: '2026-10-11T00:00:00.123456Z',
        mfa_enabled: true,
      },
    }
    await expect(getAuthenticationPolicyReview(undefined, candidate)).rejects.toThrow()
    body = {
      ...base,
      enforced: true,
      emergency_admin_id: invalidID,
      emergency_admin_created_at: '2026-10-11T00:00:00.123456Z',
      emergency_mfa_enabled: true,
    }
    await expect(getAuthenticationPolicyReview()).rejects.toThrow()
    expect(requests).toHaveLength(2)
  },
)
it('preserves exact lowercase designation bytes at the upper canonical leading-digit boundary', async () => {
  const exact = 'usr_7' + 'z'.repeat(25)
  body = {
    ...base,
    review_emergency_admin: {
      id: exact,
      created_at: '2026-10-11T00:00:00.123456Z',
      mfa_enabled: true,
    },
  }
  expect((await getAuthenticationPolicyReview(undefined, exact)).review_emergency_admin?.id).toBe(
    exact,
  )
  body = {
    ...(body as object),
    enforced: true,
    emergency_admin_id: exact,
    emergency_admin_created_at: '2026-10-11T00:00:00.123456Z',
    emergency_mfa_enabled: true,
    confirmation: 'saved',
    runtime_applied: true,
    reauthentication_required: false,
  }
  expect(
    (
      await saveAuthenticationPolicy(
        { enforced: true, emergency_admin_id: exact, reason: 'Reviewed exact identity' },
        token,
        'fresh',
      )
    ).emergency_admin_id,
  ).toBe(exact)
  expect(JSON.parse(requests[1].data).emergency_admin_id).toBe(exact)
})
