import axios from 'axios'
import client from './client'
import type { Attachment, AttachmentTarget } from '@/types/attachments'

export class AttachmentError extends Error {
  constructor(public readonly status: number) {
    super('Attachment request failed')
  }
}

async function requestAttachment(
  method: 'post' | 'delete',
  path: string,
  data: FormData | undefined,
  csrf: string,
  signal?: AbortSignal,
): Promise<Attachment> {
  try {
    return (
      await client.request<Attachment>({
        method,
        url: path,
        data,
        signal,
        headers: { 'X-CSRF-Token': csrf },
      })
    ).data
  } catch (error) {
    throw new AttachmentError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}

function attachmentPath(target: AttachmentTarget, id?: string) {
  const prefix =
    target.scope === 'project'
      ? `/projects/${encodeURIComponent(target.projectId)}/attachments`
      : '/attachments'
  return id ? `${prefix}/${encodeURIComponent(id)}` : prefix
}

export function uploadAttachment(
  file: File,
  csrf: string,
  signal?: AbortSignal,
  target: AttachmentTarget = { scope: 'user' },
) {
  const data = new FormData()
  data.append('file', file, file.name)
  return requestAttachment('post', attachmentPath(target), data, csrf, signal)
}

export function deleteAttachment(
  id: string,
  csrf: string,
  signal?: AbortSignal,
  target: AttachmentTarget = { scope: 'user' },
) {
  return requestAttachment('delete', attachmentPath(target, id), undefined, csrf, signal)
}
