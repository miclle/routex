import type { KeyDeliveryDescriptor } from '@/types/key-delivery'

const canonicalId = /^(?:key|pky|vap|vpb|kds)_[0-7][0-9a-hjkmnp-tv-z]{25}$/
const segment = /^[a-zA-Z0-9_.-]+$/
const jwtFile = '/var/run/secrets/routex/workload.jwt'

function validSegment(value: string) {
  return (
    value.length > 0 &&
    value.length <= 128 &&
    segment.test(value) &&
    value !== '.' &&
    value !== '..' &&
    !value.endsWith('.')
  )
}

function validPath(value: string, maximum: number, empty = false) {
  return (
    (value === '' && empty) ||
    (value.length > 0 && value.length <= maximum && value.split('/').every(validSegment))
  )
}

function validEndpoint(value: string) {
  if (
    !value ||
    value.length > 2048 ||
    /[\p{Cc}\p{Cs}\s"\\%?#]/u.test(value) ||
    ['${', '%{', '{{', '}}'].some((token) => value.includes(token))
  )
    return false
  try {
    const url = new URL(value)
    const match = value.match(/^https?:\/\/[^/]+(\/.*)?$/)
    if (!match) return false
    const originalPath = match[1] ?? ''
    return (
      (url.protocol === 'https:' || url.protocol === 'http:') &&
      !!url.hostname &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash &&
      !url.pathname.includes('//') &&
      !originalPath.includes('//') &&
      validPath(originalPath.replace(/^\//, '').replace(/\/$/, ''), 256, true)
    )
  } catch {
    return false
  }
}

export interface VaultAgentConfiguration {
  text: string
  jwtFile: string
  keyFile: string
}

// These are non-secret, server-confirmed facts. Keep the renderer pure: it neither
// finds deployment defaults nor obtains a JWT, Vault Token or RouteX bearer.
export function vaultAgentConfiguration(
  descriptor: KeyDeliveryDescriptor,
): VaultAgentConfiguration | null {
  const tuple = [
    [descriptor.key_id, /^(key|pky)_/],
    [descriptor.profile_id, /^vap_/],
    [descriptor.principal_id, /^vpb_/],
    [descriptor.slot_id, /^kds_/],
  ] as const
  if (
    descriptor.schema_version !== 1 ||
    descriptor.version !== 1 ||
    descriptor.method !== 'jwt' ||
    descriptor.coordinator !== 'not_configured' ||
    descriptor.application_status !== 'unknown' ||
    tuple.some(([value, prefix]) => !canonicalId.test(value) || !prefix.test(value)) ||
    !/^[1-9][0-9]{0,19}$/.test(descriptor.descriptor_revision) ||
    BigInt(descriptor.descriptor_revision) > 18446744073709551615n ||
    descriptor.role !== 'routex-api-key-' + descriptor.principal_id ||
    !validEndpoint(descriptor.endpoint) ||
    !validPath(descriptor.namespace, 256, true) ||
    !validPath(descriptor.mount, 128) ||
    !validPath(descriptor.auth_mount, 128) ||
    !validPath(descriptor.path, 512) ||
    !descriptor.path.endsWith(
      '/' + descriptor.profile_id + '/routex-api-key-' + descriptor.key_id,
    ) ||
    !validSegment(descriptor.data_field) ||
    descriptor.data_field.length > 64
  )
    return null

  const keyFile = '/var/run/secrets/routex/api-keys/' + descriptor.slot_id
  // Restrict every interpolated value before quoting both HCL and template
  // strings. The version is a GET query parameter, never a secret-function
  // write argument, and an explicit KV-v2 /data/ path avoids a latest read.
  const path = descriptor.mount + '/data/' + descriptor.path + '?version=1'
  const template = `{{ with secret ${JSON.stringify(path)} }}{{ $key := index .Data.data ${JSON.stringify(descriptor.data_field)} }}{{ if not $key }}{{ sprig_fail "RouteX API Key field is missing or empty" }}{{ end }}{{ $key }}{{ end }}`
  const text = `vault {
  address = ${JSON.stringify(descriptor.endpoint)}
${descriptor.namespace ? '  namespace = ' + JSON.stringify(descriptor.namespace) + '\n' : ''}}

auto_auth {
  method "jwt" {
    mount_path = ${JSON.stringify('auth/' + descriptor.auth_mount)}
    config = {
      role = ${JSON.stringify(descriptor.role)}
      path = ${JSON.stringify(jwtFile)}
      remove_jwt_after_reading = false
    }
  }
}

template_config {
  exit_on_retry_failure = true
}

template {
  contents = ${JSON.stringify(template)}
  destination = ${JSON.stringify(keyFile)}
  perms = "0600"
  backup = false
  error_on_missing_key = true
}
`
  // Template-only Agent does not need a token sink. In particular, never write
  // its caller Vault Token to the application API-Key destination.
  return { text, jwtFile, keyFile }
}
