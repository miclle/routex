import axios from 'axios'
import client from './client'
import type { Attachment } from '@/types/attachments'

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

export function uploadAttachment(file: File, csrf: string, signal?: AbortSignal) {
  const data = new FormData()
  data.append('file', file, file.name)
  return requestAttachment('post', '/attachments', data, csrf, signal)
}

export function deleteAttachment(id: string, csrf: string, signal?: AbortSignal) {
  return requestAttachment(
    'delete',
    `/attachments/${encodeURIComponent(id)}`,
    undefined,
    csrf,
    signal,
  )
}
