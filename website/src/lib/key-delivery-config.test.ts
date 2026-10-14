import { describe, expect, it } from 'vitest'
import type { KeyDeliveryDescriptor } from '@/types/key-delivery'
import { vaultAgentConfiguration } from './key-delivery-config'

const suffix = '01m36yee4gkbns18pfcqqc75a3'
const id = (prefix: string) => prefix + '_' + suffix
const descriptor = (): KeyDeliveryDescriptor => ({
  schema_version: 1,
  key_id: id('key'),
  profile_id: id('vap'),
  principal_id: id('vpb'),
  slot_id: id('kds'),
  descriptor_revision: '9007199254740993',
  endpoint: 'https://vault.example.test/base',
  namespace: 'tenant/production',
  mount: 'applications/kv',
  path: 'keys/' + id('vap') + '/routex-api-key-' + id('key'),
  data_field: 'api-key.value',
  version: 1,
  method: 'jwt',
  auth_mount: 'workloads/jwt',
  role: 'routex-api-key-' + id('vpb'),
  coordinator: 'not_configured',
  application_status: 'unknown',
})

function templateContents(text: string): string {
  const line = text.split('\n').find((line) => line.trimStart().startsWith('contents = '))!
  return JSON.parse(line.slice(line.indexOf('=') + 1).trim())
}

describe('descriptor-derived Vault Agent JWT configuration', () => {
  it('preserves the exact native auth tuple and a read-only version-one KV-v2 template', () => {
    const value = descriptor()
    const original = structuredClone(value)
    const configuration = vaultAgentConfiguration(value)!
    expect(configuration.text).toContain('address = "https://vault.example.test/base"')
    expect(configuration.text).toContain('namespace = "tenant/production"')
    expect(configuration.text).toContain('method "jwt"')
    expect(configuration.text).toContain('mount_path = "auth/workloads/jwt"')
    expect(configuration.text).toContain('role = "routex-api-key-' + id('vpb') + '"')
    expect(configuration.text).toContain('remove_jwt_after_reading = false')
    const template = templateContents(configuration.text)
    expect(template).toContain(
      '{{ with secret "applications/kv/data/' + value.path + '?version=1" }}',
    )
    expect(template).toContain('index .Data.data "api-key.value"')
    expect(template).toContain('sprig_fail "RouteX API Key field is missing or empty"')
    expect(template.match(/\bsecret\b/g)).toHaveLength(1)
    expect(template).not.toContain('"version=1"')
    expect(configuration.text).not.toContain('sink')
    expect(configuration.text).not.toContain('token =')
    expect(configuration.text).toContain('perms = "0600"')
    expect(configuration.text).toContain('backup = false')
    expect(configuration.text).toContain('exit_on_retry_failure = true')
    expect(configuration.jwtFile).not.toBe(configuration.keyFile)
    expect(value).toEqual(original)
  })

  it.each(['key', 'pky'])(
    'keeps the %s application destination stable across exact-target rotation',
    (prefix) => {
      const before = { ...descriptor(), key_id: id(prefix) }
      before.path = 'keys/' + before.profile_id + '/routex-api-key-' + before.key_id
      const after = {
        ...before,
        key_id: prefix + '_01m36yee4gkbns18pfcqqc75a4',
        descriptor_revision: '9007199254740994',
      }
      after.path = 'keys/' + after.profile_id + '/routex-api-key-' + after.key_id
      const first = vaultAgentConfiguration(before)!
      const second = vaultAgentConfiguration(after)!
      expect(first.keyFile).toBe(second.keyFile)
      expect(second.keyFile).toBe('/var/run/secrets/routex/api-keys/' + before.slot_id)
      expect(first.text).not.toBe(second.text)
      expect(templateContents(second.text)).toContain(after.key_id + '?version=1')
      expect(templateContents(second.text)).not.toContain(before.key_id)
      const changedSlot = vaultAgentConfiguration({
        ...after,
        slot_id: 'kds_01m36yee4gkbns18pfcqqc75a4',
      })!
      expect(changedSlot.keyFile).not.toBe(first.keyFile)
    },
  )

  it('omits an empty Vault namespace without inventing a root namespace', () => {
    const configuration = vaultAgentConfiguration({ ...descriptor(), namespace: '' })!
    expect(configuration.text).not.toContain('namespace =')
  })

  it.each([
    ['schema_version', 2],
    ['version', 2],
    ['method', 'kubernetes'],
    ['coordinator', 'ready'],
    ['application_status', 'applied'],
    ['key_id', id('usr')],
    ['profile_id', id('key')],
    ['principal_id', id('vap')],
    ['slot_id', '../key'],
    ['descriptor_revision', '0'],
    ['descriptor_revision', '01'],
    ['descriptor_revision', '18446744073709551616'],
    ['role', 'other-caller'],
    ['path', 'keys/' + id('vap') + '/routex-api-key-key_01m36yee4gkbns18pfcqqc75a4'],
    ['path', 'keys/' + 'vap_01m36yee4gkbns18pfcqqc75a4' + '/routex-api-key-' + id('key')],
    ['endpoint', 'https://user:token@vault.example.test'],
    ['endpoint', 'https://vault.example.test?version=2'],
    ['endpoint', 'https://vault.example.test#other'],
    ['endpoint', 'https://vault.example.test/base/../other'],
    ['endpoint', 'HTTPS://vault.example.test/base/../other'],
    ['endpoint', 'https://vault.example.test/base/%2e%2e/other'],
    ['endpoint', 'file:///var/run/secrets/private'],
    ['endpoint', 'https://vault.example.test/${file("private")}'],
  ])('rejects unsupported or mismatched %s=%s without emitting a template', (field, value) => {
    expect(
      vaultAgentConfiguration({ ...descriptor(), [field]: value } as KeyDeliveryDescriptor),
    ).toBeNull()
  })

  it.each(['mount', 'namespace', 'auth_mount', 'data_field', 'path'] as const)(
    'rejects traversal and HCL/Go-template injection through %s',
    (field) => {
      for (const value of [
        '..',
        '../outside',
        'part//other',
        'part.',
        '/absolute',
        'part\\other',
        'part\nother',
        'part\u0000other',
        '" }\nvault { address = "https://unreviewed"',
        '${file("private")}',
        '%{ if true }unreviewed',
        '{{ file "private" }}',
      ])
        expect(vaultAgentConfiguration({ ...descriptor(), [field]: value })).toBeNull()
    },
  )
})
