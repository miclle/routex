import { afterEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  getRoleDefinition,
  setRoleDefinition,
  validateRoleDefinition,
  validateRoleDefinitionInput,
  validateRoleDefinitionResult,
  validRoleDefinitionName,
  validRoleDefinitionReason,
} from './role-definition'

const review = 'a'.repeat(64),
  identity = 'b'.repeat(64),
  definition = 'c'.repeat(64)
const available = [
  'announcements.write',
  'audit.read',
  'calls.read_all',
  'egress.read',
  'egress.test',
  'egress.write',
  'limits.settings.write',
  'limits.users.write',
  'members.keys.disable',
  'members.models.write',
  'members.read',
  'members.write',
  'models.read_all',
  'models.write',
  'prices.read',
  'prices.write',
  'projects.limits.write',
  'projects.models.write',
  'projects.read_all',
  'projects.write',
  'providers.read',
  'providers.write',
  'roles.read',
  'secrets.read',
  'secrets.rotate',
  'site.write',
  'smtp.read',
  'smtp.test',
  'smtp.write',
  'storage.read',
  'storage.test',
  'storage.write',
  'system.read',
  'system.write',
  'teams.models.write',
  'teams.money.write',
  'teams.quota_requests.read_all',
  'teams.rates.write',
  'teams.read_all',
  'teams.tokens.write',
  'teams.write',
]
const page = () => ({
  id: 'rol_Custom',
  name: 'Retained role',
  builtin: false,
  permissions: ['projects.models.WRITE', 'providers.read'],
  available_permissions: [...available],
  definition_etag: definition,
  identity_etag: identity,
  review_etag: review,
  can_edit: true,
})
const input = () => ({
  name: 'Reviewed role',
  permissions: ['providers.read'],
  identity_etag: identity,
  reason: 'Reviewed definition',
})
const result = () => ({
  id: 'rol_Custom',
  name: 'Reviewed role',
  permissions: ['providers.read'],
  identity_etag: identity,
  etag: definition,
  confirmation: 'current_role_definition',
  effect: 'current_database',
})
const original = client.defaults.adapter
afterEach(() => {
  client.defaults.adapter = original
})
describe('resource-scoped Role definition wire contract', () => {
  it('preserves historical names/case and the complete named assignable catalogue', () => {
    const data = page()
    data.name = '\t'
    expect(validateRoleDefinition(data, 'rol_Custom')).toEqual(data)
    expect(validateRoleDefinition({ ...data, name: '' }, 'rol_Custom').name).toBe('')
    expect(validateRoleDefinition({ ...data, id: 'rol_' }, 'rol_').id).toBe('rol_')
  })
  it('keeps unknown creation provenance read-only without inventing an identity', () => {
    const data = { ...page(), identity_etag: null, can_edit: false }
    expect(validateRoleDefinition(data, data.id).identity_etag).toBeNull()
    expect(() => validateRoleDefinition({ ...data, can_edit: true }, data.id)).toThrow()
  })
  it.each([
    ['response alias', { id: 'rol_custom' }],
    ['extra', { receipt: true }],
    ['null permissions', { permissions: null }],
    ['duplicate', { permissions: ['providers.read', 'providers.read'] }],
    ['unsorted', { permissions: ['z.read', 'a.read'] }],
    ['unsafe unknown', { permissions: ['unknown.\nread'] }],
    ['long code', { permissions: ['a'.repeat(81)] }],
    ['incomplete catalogue', { available_permissions: available.slice(1) }],
    [
      'case alias catalogue',
      { available_permissions: [...available, 'projects.models.WRITE'].sort() },
    ],
    ['invented catalogue', { available_permissions: [...available, 'unknown.read'].sort() }],
    ['reserved role', { available_permissions: [...available, 'roles.write'].sort() }],
    [
      'reserved registration',
      { available_permissions: [...available, 'registration.write'].sort() },
    ],
    [
      'reserved approval',
      { available_permissions: [...available, 'members.approvals.write'].sort() },
    ],
    ['builtin editable', { builtin: true }],
    ['null definition', { definition_etag: null }],
    ['weak review', { review_etag: `W/"${review}"` }],
    ['uppercase birth', { identity_etag: 'B'.repeat(64) }],
    ['overlong name', { name: '界'.repeat(101) }],
    ['surrogate', { name: '\ud800' }],
  ])('rejects %s', (_name, patch) => {
    expect(() => validateRoleDefinition({ ...page(), ...patch }, 'rol_Custom')).toThrow()
  })
  it.each(['rol_Custom ', 'ROL_Custom', 'rol_Custom/other', 'rol_' + 'a'.repeat(27)])(
    'rejects path %s',
    async (target) => {
      let requests = 0
      client.defaults.adapter = async () => {
        requests++
        throw new Error('Not reached')
      }
      await expect(getRoleDefinition(target)).rejects.toThrow()
      expect(requests).toBe(0)
    },
  )
  it('keeps submitted names/reasons trim-exact and byte bounded independently of retained labels', () => {
    expect(validRoleDefinitionName('界'.repeat(100))).toBe(true)
    for (const value of ['', ' Role', 'Role ', 'Role\n', '\ud800', '界'.repeat(101)])
      expect(validRoleDefinitionName(value)).toBe(false)
    expect(validRoleDefinitionReason('界'.repeat(341))).toBe(true)
    expect(validRoleDefinitionReason('x'.repeat(1024))).toBe(true)
    for (const value of [
      '',
      ' reason',
      'reason ',
      'r\u0000',
      '\ud800',
      '界'.repeat(342),
      'x'.repeat(1025),
    ])
      expect(validRoleDefinitionReason(value)).toBe(false)
  })
  it.each([
    { permissions: null },
    { permissions: ['projects.models.WRITE'] },
    { permissions: ['unknown.read'] },
    { permissions: ['roles.write'] },
    { permissions: ['members.approvals.write'] },
    { permissions: ['providers.read', 'providers.read'] },
    { permissions: ['providers.write', 'providers.read'] },
    { reason: '' },
    { name: ' Role' },
    { identity_etag: null },
    { id: 'rol_other' },
  ])('rejects incomplete/unassignable full input %j', (patch) => {
    expect(() => validateRoleDefinitionInput({ ...input(), ...patch })).toThrow()
  })
  it('supports an explicit complete empty replacement', () => {
    expect(validateRoleDefinitionInput({ ...input(), permissions: [] }).permissions).toEqual([])
  })
  it.each([
    { id: 'rol_other' },
    { name: 'Other' },
    { permissions: [] },
    { identity_etag: definition },
    { confirmation: 'original_operation' },
    { effect: 'runtime' },
    { runtime_applied: true },
    { etag: null },
  ])('requires exact current-state receipt %j', (patch) => {
    expect(() =>
      validateRoleDefinitionResult({ ...result(), ...patch }, 'rol_Custom', input()),
    ).toThrow()
  })
  it.each([
    ['private, no-store', `"${review}"`, true],
    ['no-store', `"${review}"`, false],
    ['private, no-store', review, false],
    ['private, no-store', `W/"${review}"`, false],
    ['private, no-store', `"${definition}"`, false],
  ])('verifies GET privacy/strong body-header proof %s %s', async (privacy, etag, valid) => {
    client.defaults.adapter = async (config) => ({
      config,
      status: 200,
      statusText: '',
      data: page(),
      headers: new AxiosHeaders({ ETag: etag, 'Cache-Control': privacy }),
    })
    const promise = getRoleDefinition('rol_Custom')
    if (valid) await expect(promise).resolves.toEqual(page())
    else await expect(promise).rejects.toThrow()
  })
  it('dispatches only exact scoped full input/strong review/fresh CSRF and checks the result', async () => {
    let request: InternalAxiosRequestConfig | undefined
    client.defaults.adapter = async (config) => {
      request = config
      return {
        config,
        status: 200,
        statusText: '',
        data: result(),
        headers: new AxiosHeaders({
          ETag: `"${definition}"`,
          'Cache-Control': 'private, no-store',
        }),
      }
    }
    expect(await setRoleDefinition('rol_Custom', review, input(), 'csrf-current')).toEqual(result())
    expect(request?.url).toBe('/admin/roles/rol_Custom')
    expect(request?.method).toBe('put')
    expect(request?.params).toBeUndefined()
    expect(request?.headers.get('If-Match')).toBe(`"${review}"`)
    expect(request?.headers.get('X-CSRF-Token')).toBe('csrf-current')
    expect(JSON.parse(request!.data)).toEqual(input())
  })
  it('does not accept matching GET content or an extra historical receipt as PUT success', async () => {
    client.defaults.adapter = async (config) => ({
      config,
      status: 200,
      statusText: '',
      data: { ...result(), receipt: true },
      headers: new AxiosHeaders({ ETag: `"${definition}"`, 'Cache-Control': 'private, no-store' }),
    })
    await expect(setRoleDefinition('rol_Custom', review, input(), 'csrf')).rejects.toThrow()
  })
})
