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
  validRoleDefinitionDescription,
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
  description: '',
  builtin: false,
  assignment_kind: 'explicit',
  permissions: ['projects.models.WRITE', 'providers.read'],
  available_permissions: [...available],
  definition_etag: definition,
  identity_etag: identity,
  review_etag: review,
  can_edit: true,
})
const input = () => ({
  name: 'Reviewed role',
  description: 'Reviewed business scope',
  permissions: ['providers.read'],
  identity_etag: identity,
  reason: 'Reviewed definition',
})
const result = () => ({
  id: 'rol_Custom',
  name: 'Reviewed role',
  description: 'Reviewed business scope',
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

describe('Role description exact wire and UTF-8 bounds', () => {
  it.each([
    'Business scope',
    'One line\nSecond line',
    '界'.repeat(666) + 'ab',
    'x'.repeat(2000),
    '😀'.repeat(500),
  ])('accepts exact supported description %s', (description) => {
    expect(validRoleDefinitionDescription(description)).toBe(true)
    expect(validateRoleDefinitionInput({ ...input(), description }).description).toBe(description)
  })
  it.each([
    '',
    ' ',
    ' leading',
    'trailing ',
    '\nline',
    'line\n',
    'a\tb',
    'a\rb',
    'a\u0000b',
    'a\u0085b',
    '\ud800',
    '\udc00',
    '界'.repeat(667),
    'x'.repeat(2001),
    '😀'.repeat(501),
  ])('rejects invalid submitted description %j', (description) => {
    expect(validRoleDefinitionDescription(description)).toBe(false)
    expect(() => validateRoleDefinitionInput({ ...input(), description })).toThrow()
  })
  it('requires the new exact GET10/input5/result8 fields while retaining historical empty read', () => {
    expect(validateRoleDefinition({ ...page(), description: '' }, 'rol_Custom').description).toBe(
      '',
    )
    const oldPage: Record<string, unknown> = { ...page() }
    delete oldPage.description
    const oldInput: Record<string, unknown> = { ...input() }
    delete oldInput.description
    const oldResult: Record<string, unknown> = { ...result() }
    delete oldResult.description
    expect(() => validateRoleDefinition(oldPage, 'rol_Custom')).toThrow()
    expect(() => validateRoleDefinitionInput(oldInput)).toThrow()
    expect(() => validateRoleDefinitionResult(oldResult, 'rol_Custom', input())).toThrow()
    expect(() =>
      validateRoleDefinitionResult(
        { ...result(), description: 'Other current text' },
        'rol_Custom',
        input(),
      ),
    ).toThrow()
    expect(() => validateRoleDefinition({ ...page(), description: null }, 'rol_Custom')).toThrow()
  })
  it('captures exact multiline description and rejects a mismatching confirmation after dispatch', async () => {
    const captured = { ...input(), description: 'Read catalogue\nReview provider facts' }
    let body = ''
    client.defaults.adapter = async (config) => {
      body = String(config.data)
      return {
        config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders({
          ETag: `"${definition}"`,
          'Cache-Control': 'private, no-store',
        }),
        data: { ...result(), description: 'A different current description' },
      }
    }
    await expect(
      setRoleDefinition('rol_Custom', review, captured, 'csrf-current'),
    ).rejects.toThrow()
    expect(JSON.parse(body).description).toBe(captured.description)
  })
})

const formattedDescriptions = [
  '\uFEFFBusiness scope',
  'Business scope\uFEFF',
  '\uFEFFScope\n审批职责\uFEFF',
]
it.each(formattedDescriptions)(
  'reads and writes exact server-valid description format characters: %s',
  async (description) => {
    const seen: InternalAxiosRequestConfig[] = []
    client.defaults.adapter = async (config) => {
      seen.push(config)
      return {
        config,
        status: 200,
        statusText: '',
        data: config.method === 'get' ? { ...page(), description } : { ...result(), description },
        headers: new AxiosHeaders({
          ETag: `"${config.method === 'get' ? review : definition}"`,
          'Cache-Control': 'private, no-store',
        }),
      }
    }
    expect((await getRoleDefinition('rol_Custom')).description).toBe(description)
    const submitted = { ...input(), description }
    expect(
      (await setRoleDefinition('rol_Custom', review, submitted, 'fresh-csrf')).description,
    ).toBe(description)
    expect(seen).toHaveLength(2)
    expect(seen[1].data).toBe(JSON.stringify(submitted))
    expect(seen[1].headers.get('If-Match')).toBe(`"${review}"`)
  },
)
it('preserves FEFF description byte bounds without changing Name or Reason validation', () => {
  expect(validRoleDefinitionDescription('\uFEFF' + 'x'.repeat(1994) + '\uFEFF')).toBe(true)
  expect(validRoleDefinitionDescription('\uFEFF' + 'x'.repeat(1995) + '\uFEFF')).toBe(false)
  expect(validRoleDefinitionName('\uFEFFName')).toBe(false)
  expect(validRoleDefinitionReason('\uFEFFReason')).toBe(false)
  for (const space of [
    ' ',
    '\u00a0',
    '\u1680',
    '\u2000',
    '\u2028',
    '\u2029',
    '\u202f',
    '\u205f',
    '\u3000',
  ]) {
    expect(validRoleDefinitionDescription(space + 'Scope')).toBe(false)
    expect(validRoleDefinitionDescription('Scope' + space)).toBe(false)
  }
})

it('duty classification keeps immutable explicit Role definitions readable without editing authority', () => {
  const p = {
    ...page(),
    id: 'rol_finance',
    name: 'Finance',
    builtin: true,
    assignment_kind: 'explicit',
    identity_etag: null,
    can_edit: false,
  }
  expect(validateRoleDefinition(p, 'rol_finance')).toMatchObject({
    builtin: true,
    assignment_kind: 'explicit',
    can_edit: false,
  })
})
