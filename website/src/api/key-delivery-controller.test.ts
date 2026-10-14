import { afterEach, describe, expect, it, vi } from 'vitest'
import client from './client'
import {
  DeliveryControllerError,
  enrollController,
  getController,
  getControllerEnrollmentReceipt,
  getControllerReview,
  getControllers,
  getControllerSlots,
  parseControllerDetail,
  revokeController,
} from './key-delivery-controller'
const id = (prefix: string) => `${prefix}_01arz3ndektsv4rrffq69g5fav`
const hash = 'a'.repeat(64),
  slot = id('kds'),
  controller = id('kcg'),
  birth = '2026-10-14T01:00:00Z'
const configuration = {
  schema_version: 1,
  slot_id: slot,
  descriptor_revision: '9007199254740993',
  key_id: id('key'),
  configuration_digest: hash,
  agent_configuration: 'vault {\n  address = "https://vault.example.invalid"\n}\n',
  jwt_file: '/var/run/secrets/routex/workload.jwt',
  key_file: `/var/run/secrets/routex/api-keys/${slot}`,
}
const detail = {
  id: controller,
  name: 'Independent controller',
  slot_id: slot,
  owner_kind: 'personal',
  owner_id: id('usr'),
  expires_at: '2026-10-15T01:00:00Z',
  revoked_at: null,
  assigned_descriptor_revision: configuration.descriptor_revision,
  assigned_configuration_digest: hash,
  current_configuration: configuration,
  configuration_status: 'assigned',
  last_acknowledgement: null,
  review_etag: hash,
}
const input = {
  request_id: '20000000-0000-4000-8000-000000000001',
  name: detail.name,
  expires_at: detail.expires_at,
  opt_in: true as const,
  descriptor_revision: configuration.descriptor_revision,
  configuration_digest: hash,
  reason: 'Reviewed exact Slot enrollment',
}
afterEach(() => vi.restoreAllMocks())
describe('controller API boundary', () => {
  it('uses parent-scoped canonical paging and preserves exact large revisions', async () => {
    const get = vi
      .spyOn(client, 'get')
      .mockResolvedValueOnce({
        data: {
          items: [
            {
              id: slot,
              birth,
              profile_id: id('vap'),
              owner_kind: 'personal',
              owner_id: id('usr'),
              descriptor_revision: configuration.descriptor_revision,
              configuration_status: 'not_reviewed',
            },
          ],
          next_cursor: 'opaque',
        },
      })
      .mockResolvedValueOnce({
        data: {
          items: [
            {
              id: controller,
              birth,
              name: detail.name,
              slot_id: slot,
              expires_at: detail.expires_at,
              revoked_at: null,
              lifecycle_status: 'active',
              configuration_status: 'not_reviewed',
            },
          ],
          next_cursor: '',
        },
      })
    const signal = new AbortController().signal
    expect(
      (await getControllerSlots(id('vap'), 'opaque', signal)).items[0].descriptor_revision,
    ).toBe('9007199254740993')
    expect(get).toHaveBeenNthCalledWith(
      1,
      `/admin/api-key-delivery/targets/${id('vap')}/slots?cursor=opaque&limit=20`,
      { signal },
    )
    expect((await getControllers(slot, '', signal)).items[0].lifecycle_status).toBe('active')
    expect(get).toHaveBeenNthCalledWith(
      2,
      `/admin/api-key-delivery/slots/${slot}/controllers?limit=20`,
      { signal },
    )
  })
  it('requires matching strong wire reviews and exact resource identities', async () => {
    const get = vi
      .spyOn(client, 'get')
      .mockResolvedValueOnce({
        data: { configuration, review_etag: hash },
        headers: { etag: `"${hash}"` },
      })
      .mockResolvedValueOnce({ data: detail, headers: { etag: `"${hash}"` } })
      .mockResolvedValueOnce({ data: detail, headers: { etag: `W/"${hash}"` } })
    expect((await getControllerReview(slot)).configuration.descriptor_revision).toBe(
      configuration.descriptor_revision,
    )
    expect((await getController(controller, slot)).id).toBe(controller)
    await expect(getController(controller, slot)).rejects.toThrow(DeliveryControllerError)
    expect(get).toHaveBeenCalledTimes(3)
  })
  it.each(['../escape', '', id('kds').toUpperCase(), id('key')])(
    'rejects unsafe Slot %s before transport',
    async (value) => {
      const get = vi.spyOn(client, 'get')
      await expect(getControllers(value)).rejects.toThrow()
      expect(get).not.toHaveBeenCalled()
    },
  )
  it.each([
    'extra field',
    'foreign Slot',
    'unknown report',
    'unreviewed configuration',
    'unsafe amount',
  ])('rejects malformed projection: %s', (kind) => {
    const value = structuredClone(detail) as any
    if (kind === 'extra field') value.bearer = 'rxdctl_do-not-return'
    else if (kind === 'foreign Slot') value.slot_id = 'kds_01arz3ndektsv4rrffq69g5faw'
    else if (kind === 'unknown report')
      value.last_acknowledgement = { evidence_kind: 'application_verified' }
    else if (kind === 'unreviewed configuration') value.configuration_status = 'unavailable'
    else value.assigned_descriptor_revision = Number('9007199254740993')
    expect(() => parseControllerDetail(value, controller, slot)).toThrow(DeliveryControllerError)
  })
  it('only accepts a one-time bearer on201 and a null unrecoverable bearer on200', async () => {
    const bearer = 'rxdctl_' + 'A'.repeat(43),
      post = vi
        .spyOn(client, 'post')
        .mockResolvedValueOnce({
          status: 201,
          data: { controller: detail, bearer, secret_status: 'display_once' },
          headers: { etag: `"${hash}"` },
        })
        .mockResolvedValueOnce({
          status: 200,
          data: { controller: detail, bearer: null, secret_status: 'not_recoverable' },
          headers: { etag: `"${hash}"` },
        })
        .mockResolvedValueOnce({
          status: 200,
          data: { controller: detail, bearer, secret_status: 'display_once' },
          headers: { etag: `"${hash}"` },
        })
    expect((await enrollController(slot, input, hash, 'csrf')).bearer).toBe(bearer)
    expect(post).toHaveBeenNthCalledWith(
      1,
      `/admin/api-key-delivery/slots/${slot}/controllers`,
      input,
      { headers: { 'If-Match': `"${hash}"`, 'X-CSRF-Token': 'csrf' }, signal: undefined },
    )
    expect((await enrollController(slot, input, hash, 'new-csrf')).bearer).toBeNull()
    await expect(enrollController(slot, input, hash, 'csrf')).rejects.toThrow(
      DeliveryControllerError,
    )
  })
  it('sanitizes rejected transport objects including request bodies and bearer material', async () => {
    vi.spyOn(client, 'post').mockRejectedValue({
      isAxiosError: true,
      response: { status: 503, data: { bearer: 'rxdctl_private' } },
      config: { data: { reason: 'private-original-body' } },
    })
    const error = await enrollController(slot, input, hash, 'csrf').catch((error) => error)
    expect(error).toBeInstanceOf(DeliveryControllerError)
    expect(error.status).toBe(503)
    expect(JSON.stringify(error)).not.toContain('private')
    expect(Object.keys(error)).toEqual(['status'])
  })
  it('never treats a detail read or non-revoked response as successful revocation', async () => {
    vi.spyOn(client, 'post').mockResolvedValue({
      status: 200,
      data: detail,
      headers: { etag: `"${hash}"` },
    })
    await expect(
      revokeController(controller, slot, 'Reviewed revocation', hash, 'csrf'),
    ).rejects.toThrow(DeliveryControllerError)
  })
  it('returns only original-actor nonsecret receipt fields, rejecting recovered bearer', async () => {
    const receipt = {
      request_id: input.request_id,
      intent_digest: hash,
      enrollment_review: hash,
      controller: {
        id: controller,
        birth,
        name: detail.name,
        slot_id: slot,
        expires_at: detail.expires_at,
        revoked_at: null,
        lifecycle_status: 'active',
        configuration_status: 'not_reviewed',
      },
      bearer: null,
      secret_status: 'not_recoverable',
    }
    vi.spyOn(client, 'get')
      .mockResolvedValueOnce({ data: receipt })
      .mockResolvedValueOnce({ data: { ...receipt, bearer: 'rxdctl_private' } })
    expect((await getControllerEnrollmentReceipt(slot, input.request_id)).bearer).toBeNull()
    await expect(getControllerEnrollmentReceipt(slot, input.request_id)).rejects.toThrow(
      DeliveryControllerError,
    )
  })
})
