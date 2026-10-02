import client from './client'
import type { CredentialMetadata } from '@/types/credential-metadata'
import type { CredentialReadiness } from '@/types/credential-readiness'

const etagPattern = /^[a-f0-9]{64}$/
const credentialID = /^crd_[0-7][0-9a-hjkmnp-tv-z]{25}$/
const timestamp = (value: unknown): value is string =>
  typeof value === 'string' && value.length <= 40 && Number.isFinite(Date.parse(value))

function matchesMetadata(value: CredentialMetadata | undefined, id: string, connectionId: string) {
  return (
    value?.id === id &&
    value.connection_id === connectionId &&
    typeof value.name === 'string' &&
    value.name.length > 0 &&
    value.name.length <= 200 &&
    Number.isInteger(value.priority) &&
    value.priority >= 0 &&
    value.priority <= 10_000 &&
    typeof value.enabled === 'boolean' &&
    ['pending', 'verified', 'failed'].includes(value.verification_status) &&
    (value.verified_at === null || timestamp(value.verified_at)) &&
    etagPattern.test(value.etag)
  )
}

export async function getCredentialReadiness(
  sourceId: string,
  replacementId: string,
  connectionId: string,
  signal?: AbortSignal,
) {
  if (
    !credentialID.test(sourceId) ||
    !credentialID.test(replacementId) ||
    sourceId === replacementId
  )
    throw new Error('Credential readiness target unavailable')
  const { data, status, headers } = await client.get<CredentialReadiness>(
    `/admin/credentials/${sourceId}/retirement-readiness`,
    { params: { replacement_credential_id: replacementId }, signal },
  )
  if (
    status !== 200 ||
    sourceId === replacementId ||
    !matchesMetadata(data?.source, sourceId, connectionId) ||
    !matchesMetadata(data?.replacement, replacementId, connectionId) ||
    !etagPattern.test(data?.etag) ||
    headers.etag !== `"${data.etag}"` ||
    (data.snapshot_id !== null && !/^cfg_[0-7][0-9a-hjkmnp-tv-z]{25}$/.test(data.snapshot_id)) ||
    !Number.isSafeInteger(data.eligible_route_count) ||
    data.eligible_route_count < 0 ||
    data.eligible_route_count > 256 ||
    typeof data.eligible !== 'boolean' ||
    !Array.isArray(data.blockers) ||
    data.blockers.length > 16 ||
    data.blockers.some((value) => typeof value !== 'string' || !/^[a-z_]{1,64}$/.test(value)) ||
    (data.evidence !== null &&
      (!/^[A-Za-z0-9_-]{1,64}$/.test(data.evidence?.attempt_id) ||
        !timestamp(data.evidence?.completed_at))) ||
    (data.eligible &&
      (data.blockers.length !== 0 ||
        data.evidence === null ||
        data.snapshot_id === null ||
        data.eligible_route_count === 0 ||
        !data.source.enabled ||
        !data.replacement.enabled ||
        data.replacement.verification_status !== 'verified')) ||
    (!data.eligible && data.blockers.length === 0)
  )
    throw new Error('Credential readiness response unavailable')
  return data
}
