import { afterEach, describe, expect, it, vi } from 'vitest'
import client from './client'
import { AttachmentError, deleteAttachment, uploadAttachment } from './attachments'
import type { Attachment } from '@/types/attachments'

const readyAttachment: Attachment = {
  id: 'obj_01arz3ndektsv4rrffq69g5fav',
  name: 'diagram.png',
  mime: 'image/png',
  size: 8,
  state: 'ready',
  created_at: '2026-09-29T12:00:00Z',
}

afterEach(() => vi.restoreAllMocks())

describe('attachment API', () => {
  it('submits exactly one multipart file with session credentials and CSRF', async () => {
    const request = vi.spyOn(client, 'request').mockResolvedValue({ data: readyAttachment })
    const persist = vi.spyOn(Storage.prototype, 'setItem')
    const file = new File(['png-data'], 'diagram.png', { type: 'image/png' })
    const controller = new AbortController()

    await expect(uploadAttachment(file, 'csrf-token', controller.signal)).resolves.toEqual(
      readyAttachment,
    )

    expect(client.defaults.withCredentials).toBe(true)
    expect(request).toHaveBeenCalledOnce()
    const config = request.mock.calls[0][0]
    expect(config).toMatchObject({
      method: 'post',
      url: '/attachments',
      signal: controller.signal,
      headers: { 'X-CSRF-Token': 'csrf-token' },
    })
    expect(config.headers).not.toHaveProperty('Authorization')
    expect(config.headers).not.toHaveProperty('Content-Type')
    expect(config.data).toBeInstanceOf(FormData)
    expect(Array.from((config.data as FormData).entries())).toEqual([['file', file]])
    expect(persist).not.toHaveBeenCalled()
  })

  it('encodes the attachment ID and does not persist returned object metadata', async () => {
    const deleted = { ...readyAttachment, state: 'delete_pending' as const }
    const request = vi.spyOn(client, 'request').mockResolvedValue({ data: deleted })
    const persist = vi.spyOn(Storage.prototype, 'setItem')

    await expect(deleteAttachment('obj/name?revision=1', 'csrf-token')).resolves.toEqual(deleted)

    expect(request).toHaveBeenCalledWith({
      method: 'delete',
      url: '/attachments/obj%2Fname%3Frevision%3D1',
      data: undefined,
      signal: undefined,
      headers: { 'X-CSRF-Token': 'csrf-token' },
    })
    expect(request.mock.calls[0][0].headers).not.toHaveProperty('Authorization')
    expect(persist).not.toHaveBeenCalled()
  })

  it('uses the Project session scope without sending or persisting the API key', async () => {
    const request = vi.spyOn(client, 'request').mockResolvedValue({ data: readyAttachment })
    const persist = vi.spyOn(Storage.prototype, 'setItem')
    const file = new File(['png-data'], 'diagram.png', { type: 'image/png' })
    const target = { scope: 'project' as const, projectId: 'prj/name?revision=1' }

    await uploadAttachment(file, 'csrf-project', undefined, target)
    await deleteAttachment('obj/name', 'csrf-project', undefined, target)

    expect(request.mock.calls[0][0]).toMatchObject({
      method: 'post',
      url: '/projects/prj%2Fname%3Frevision%3D1/attachments',
      headers: { 'X-CSRF-Token': 'csrf-project' },
    })
    expect(request.mock.calls[1][0]).toMatchObject({
      method: 'delete',
      url: '/projects/prj%2Fname%3Frevision%3D1/attachments/obj%2Fname',
      headers: { 'X-CSRF-Token': 'csrf-project' },
    })
    for (const [config] of request.mock.calls) {
      expect(config.headers).not.toHaveProperty('Authorization')
      expect(JSON.stringify(config.data) ?? '').not.toContain('rx_')
    }
    expect(persist).not.toHaveBeenCalled()
  })

  it('reduces failed responses to a status-only attachment error', async () => {
    vi.spyOn(client, 'request').mockRejectedValue({
      isAxiosError: true,
      response: {
        status: 413,
        data: { error: { message: 'private backend detail' } },
      },
    })

    const error = await uploadAttachment(
      new File(['too-large'], 'large.pdf', { type: 'application/pdf' }),
      'csrf-token',
    ).catch((reason: unknown) => reason)

    expect(error).toBeInstanceOf(AttachmentError)
    expect(error).toMatchObject({ status: 413, message: 'Attachment request failed' })
    expect(String(error)).not.toContain('private backend detail')
  })
})
