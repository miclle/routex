import client from './client'
import type {
  CredentialRetirementInput,
  CredentialRetirementResult,
} from '@/types/credential-retirement'

export async function retireCredential(
  sourceId: string,
  etag: string,
  input: CredentialRetirementInput,
  csrf: string,
) {
  const credentialID = /^crd_[0-7][0-9a-hjkmnp-tv-z]{25}$/
  if (
    !credentialID.test(sourceId) ||
    !credentialID.test(input.replacement_credential_id) ||
    sourceId === input.replacement_credential_id ||
    !/^[a-f0-9]{64}$/.test(etag)
  )
    throw new Error('Credential retirement target unavailable')
  const { data, status } = await client.post<CredentialRetirementResult>(
    `/admin/credentials/${sourceId}/retire`,
    input,
    { headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf } },
  )
  if (
    status !== 200 ||
    data?.request_id !== input.request_id ||
    data.source_credential_id !== sourceId ||
    data.replacement_credential_id !== input.replacement_credential_id ||
    data.committed !== true ||
    Object.keys(data).length !== 8 ||
    typeof data.committed_at !== 'string' ||
    data.committed_at.length > 40 ||
    !Number.isFinite(Date.parse(data.committed_at)) ||
    typeof data.runtime_applied !== 'boolean' ||
    (data.current_snapshot_id !== null &&
      !/^cfg_[0-7][0-9a-hjkmnp-tv-z]{25}$/.test(data.current_snapshot_id)) ||
    !Array.isArray(data.blockers) ||
    data.blockers.length > 16 ||
    data.blockers.some((value) => typeof value !== 'string' || !/^[a-z_]{1,64}$/.test(value)) ||
    (data.runtime_applied && (data.current_snapshot_id === null || data.blockers.length > 0)) ||
    (!data.runtime_applied && data.blockers.length === 0)
  )
    throw new Error('Credential retirement receipt unavailable')
  return data
}
