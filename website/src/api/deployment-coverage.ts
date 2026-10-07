import { AxiosHeaders, type AxiosResponse, type RawAxiosHeaders } from 'axios'
import client from './client'
import { validConnectionReason } from './connection-metadata'
import { validAzureAPIVersion } from './connection-transport'
import type {
  DeploymentCoverage,
  DeploymentCoverageInput,
  DeploymentCoverageResult,
} from '@/types/deployment-coverage'
const id = (v: unknown, prefix: string): v is string =>
  typeof v === 'string' && new RegExp(`^${prefix}_[0-7][0-9a-hjkmnp-tv-z]{25}$`).test(v)
const record = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const exact = (v: Record<string, unknown>, names: string[]) =>
  Object.keys(v).length === names.length && names.every((n) => Object.hasOwn(v, n))
const proof = (v: unknown): v is string =>
  typeof v === 'string' && /^[0-9a-f]{64}\.[0-9a-f]{64}$/.test(v)
const deployment = (v: string) => /^[A-Za-z0-9_.-]{1,255}$/.test(v) && v !== '.' && v !== '..'
function unavailable(): never {
  throw new Error('Deployment coverage response unavailable')
}
export function decodeDeploymentCoverage(value: unknown): DeploymentCoverage {
  if (
    !record(value) ||
    !exact(value, [
      'credential_id',
      'connection_id',
      'adapter',
      'api_version',
      'verification_status',
      'verified_at',
      'coverage_source',
      'provider_models',
      'etag',
      'can_edit',
    ])
  )
    unavailable()
  if (
    !id(value.credential_id, 'crd') ||
    !id(value.connection_id, 'con') ||
    value.adapter !== 'azure_openai_classic' ||
    !validAzureAPIVersion(value.api_version) ||
    typeof value.verification_status !== 'string' ||
    !['pending', 'verified', 'failed'].includes(value.verification_status) ||
    value.coverage_source !== 'administrator_attestation' ||
    !proof(value.etag) ||
    typeof value.can_edit !== 'boolean' ||
    !Array.isArray(value.provider_models) ||
    value.provider_models.length > 2000
  )
    unavailable()
  if (!(
    value.verified_at === null ||
    (typeof value.verified_at === 'string' &&
      /^\d{4}-\d{2}-\d{2}T.+(?:Z|[+-]\d{2}:\d{2})$/.test(value.verified_at) &&
      Number.isFinite(Date.parse(value.verified_at)) &&
      !value.verified_at.startsWith('0001-'))
  ))
    unavailable()
  const seen = new Set<string>()
  for (const row of value.provider_models) {
    if (
      !record(row) ||
      !exact(row, ['id', 'upstream_name', 'can_attest', 'attested']) ||
      !id(row.id, 'pmd') ||
      seen.has(row.id) ||
      typeof row.upstream_name !== 'string' ||
      !row.upstream_name ||
      row.upstream_name.length > 255 ||
      /[\p{Cc}\p{Cs}]/u.test(row.upstream_name) ||
      typeof row.can_attest !== 'boolean' ||
      typeof row.attested !== 'boolean' ||
      row.can_attest !== deployment(row.upstream_name) ||
      (row.attested && !row.can_attest)
    )
      unavailable()
    seen.add(row.id)
  }
  return value as unknown as DeploymentCoverage
}
function checked(response: AxiosResponse<unknown>, data: DeploymentCoverage) {
  const h = AxiosHeaders.from(response.headers as RawAxiosHeaders),
    control = h.get('Cache-Control')
  const parts =
    typeof control === 'string' ? control.split(',').map((p) => p.trim().toLowerCase()) : []
  if (
    response.status !== 200 ||
    parts.length !== 2 ||
    !parts.includes('private') ||
    !parts.includes('no-store') ||
    h.get('ETag') !== `"${data.etag}"`
  )
    unavailable()
}
export async function getDeploymentCoverage(
  credential: string,
  connection: string,
  signal?: AbortSignal,
) {
  if (!id(credential, 'crd') || !id(connection, 'con')) unavailable()
  const result = await client.get<unknown>(`/admin/credentials/${credential}/deployment-coverage`, {
      signal,
    }),
    data = decodeDeploymentCoverage(result.data)
  checked(result, data)
  if (data.credential_id !== credential || data.connection_id !== connection) unavailable()
  return data
}
export async function saveDeploymentCoverage(
  credential: string,
  connection: string,
  etag: string,
  input: DeploymentCoverageInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<DeploymentCoverageResult> {
  if (
    !id(credential, 'crd') ||
    !id(connection, 'con') ||
    !proof(etag) ||
    !record(input) ||
    !exact(input, ['provider_model_ids', 'reason']) ||
    !Array.isArray(input.provider_model_ids) ||
    input.provider_model_ids.length > 2000 ||
    input.provider_model_ids.some((n) => !id(n, 'pmd')) ||
    new Set(input.provider_model_ids).size !== input.provider_model_ids.length ||
    !validConnectionReason(input.reason) ||
    !csrf
  )
    unavailable()
  const response = await client.put<unknown>(
      `/admin/credentials/${credential}/deployment-coverage`,
      input,
      { signal, headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf } },
    ),
    result = response.data
  if (
    !record(result) ||
    !exact(result, ['coverage', 'runtime_applied', 'changed']) ||
    result.runtime_applied !== true ||
    typeof result.changed !== 'boolean'
  )
    unavailable()
  const data = decodeDeploymentCoverage(result.coverage)
  checked(response, data)
  const desired = new Set(input.provider_model_ids),
    saved = data.provider_models.filter((n) => n.attested)
  if (
    data.credential_id !== credential ||
    data.connection_id !== connection ||
    data.etag.slice(0, 64) !== etag.slice(0, 64) ||
    !data.can_edit ||
    saved.length !== desired.size ||
    saved.some((n) => !desired.has(n.id))
  )
    unavailable()
  return { coverage: data, runtime_applied: true, changed: result.changed }
}
