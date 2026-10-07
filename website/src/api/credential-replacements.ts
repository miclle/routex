import client from './client'
import type {
  CredentialReplacementInput,
  CredentialReplacementReceipt,
} from '@/types/credential-replacements'

export async function createCredentialReplacement(
  sourceId: string,
  connectionId: string,
  etag: string,
  input: CredentialReplacementInput,
  csrf: string,
) {
  const { data, status } = await client.post<CredentialReplacementReceipt>(
    `/admin/credentials/${sourceId}/replacements`,
    input,
    { headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf } },
  )
  if (
    (status !== 200 && status !== 201) ||
    !/^crd_[0-7][0-9a-hjkmnp-tv-z]{25}$/.test(data?.id) ||
    data.id === sourceId ||
    data.connection_id !== connectionId ||
    data.replaces_credential_id !== sourceId ||
    (data.storage_source !== 'inline' && data.storage_source !== 'vault') ||
    Object.keys(data).length !== 4
  )
    throw new Error('Credential replacement receipt unavailable')
  return data
}
