import client from './client'
import type { CredentialDeleteInput, CredentialDeleteResult } from '@/types/credential-delete'

export async function deleteCredential(
  id: string,
  etag: string,
  input: CredentialDeleteInput,
  csrf: string,
) {
  const { data, status } = await client.delete<CredentialDeleteResult>(`/admin/credentials/${id}`, {
    data: input,
    headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf },
  })
  if (status !== 200 || data?.id !== id || data.absent !== true || data.runtime_applied !== true)
    throw new Error('Credential deletion result unavailable')
  return data
}
