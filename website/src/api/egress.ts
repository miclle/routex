import axios from 'axios'
import client from './client'
import type {
  ConnectionEgress,
  Egress,
  EgressDefault,
  EgressDiagnostic,
  EgressInput,
  EgressMode,
} from '@/types/egress'
export class EgressError extends Error {
  constructor(public readonly status: number) {
    super('Egress request failed')
  }
}
// Keep credential-bearing Axios configs out of component state and mutation caches.
async function write<T>(
  method: 'post' | 'put' | 'patch',
  url: string,
  data: unknown,
  csrf: string,
  signal?: AbortSignal,
): Promise<T> {
  try {
    return (
      await client.request<T>({ method, url, data, signal, headers: { 'X-CSRF-Token': csrf } })
    ).data
  } catch (error) {
    throw new EgressError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
export async function listEgress(signal?: AbortSignal, options = false) {
  return (
    await client.get<{ items: Egress[] }>(options ? '/admin/egress-options' : '/admin/egresses', {
      signal,
    })
  ).data.items
}
export async function getEgressDefault(signal?: AbortSignal) {
  return (await client.get<EgressDefault>('/admin/egress-default', { signal })).data
}
export const saveEgress = (id: string | null, input: EgressInput, csrf: string) =>
  write<Egress>(
    id ? 'patch' : 'post',
    id ? `/admin/egresses/${id}` : '/admin/egresses',
    input,
    csrf,
  )
export const testEgressDraft = (id: string | null, input: EgressInput, csrf: string) =>
  write<EgressDiagnostic>(
    'post',
    '/admin/egresses/test',
    { ...input, ...(id ? { egress_id: id } : {}) },
    csrf,
  )
export const testEgress = (
  id: string,
  input: { etag: string; target_base_url?: string; connection_id?: string },
  csrf: string,
  signal?: AbortSignal,
) => write<EgressDiagnostic>('post', `/admin/egresses/${id}/test`, input, csrf, signal)
export const saveEgressDefault = (input: EgressDefault, csrf: string) =>
  write<EgressDefault>('put', '/admin/egress-default', input, csrf)
export const saveConnectionEgress = (
  id: string,
  input: Omit<ConnectionEgress, 'connection_id'>,
  csrf: string,
) => write<ConnectionEgress>('patch', `/admin/connections/${id}/egress`, input, csrf)

export function egressSelection(value: string): {
  egress_mode: EgressMode
  egress_id: string | null
} {
  return value.startsWith('proxy:')
    ? { egress_mode: 'proxy', egress_id: value.slice(6) }
    : { egress_mode: value === 'direct' ? 'direct' : 'default', egress_id: null }
}
