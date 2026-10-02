import client from './client'
import type { CredentialMetadata, CredentialMetadataInput } from '@/types/credential-metadata'

export async function getCredentialMetadata(id: string, signal?: AbortSignal) {
  const { data } = await client.get<CredentialMetadata>(`/admin/credentials/${id}/metadata`, {
    signal,
  })
  if (!/^[a-f0-9]{64}$/.test(data?.etag)) throw new Error('Credential metadata preview unavailable')
  return data
}

export async function saveCredentialMetadata(
  id: string,
  etag: string,
  input: CredentialMetadataInput,
  csrf: string,
) {
  const { data, status } = await client.put<CredentialMetadata>(
    `/admin/credentials/${id}/metadata`,
    input,
    {
      headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf },
    },
  )
  if (
    status !== 200 ||
    data?.id !== id ||
    data.name !== input.name ||
    data.priority !== input.priority ||
    !/^[a-f0-9]{64}$/.test(data.etag)
  )
    throw new Error('Credential metadata save result unavailable')
  return data
}
