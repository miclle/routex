import axios from 'axios'
import client from './client'
import type {
  StorageInput,
  StorageProbe,
  StorageRollbackInput,
  StorageSettings,
} from '@/types/storage'

export class StorageError extends Error {
  constructor(public readonly status: number) {
    super('Storage request failed')
  }
}

export async function getStorage(signal?: AbortSignal) {
  return (await client.get<StorageSettings>('/admin/storage', { signal })).data
}

// Do not retain Axios configurations containing storage credentials.
async function write<T>(
  method: 'put' | 'post',
  path: string,
  data: unknown,
  csrf: string,
): Promise<T> {
  try {
    return (await client.request<T>({ method, url: path, data, headers: { 'X-CSRF-Token': csrf } }))
      .data
  } catch (error) {
    throw new StorageError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}

export const saveStorage = (input: StorageInput, csrf: string) =>
  write<StorageSettings>('put', '/admin/storage', input, csrf)

export const testStorage = (etag: string, csrf: string) =>
  write<StorageProbe>('post', '/admin/storage/test', { etag }, csrf)

export const rollbackStorage = (input: StorageRollbackInput, csrf: string) =>
  write<StorageSettings>('post', '/admin/storage/rollback', input, csrf)
