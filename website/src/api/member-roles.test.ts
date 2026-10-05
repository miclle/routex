import { afterEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  getMemberRoles,
  getMemberRoleCandidates,
  getMemberRoleDetail,
  setMemberRoles,
  validateMemberRoles,
  validateMemberRoleCandidates,
  validateMemberRoleDetail,
  validateMemberRolesConfirmation,
  validMemberRolesReason,
  validMemberRoleSearch,
} from './member-roles'

const etag = 'a'.repeat(64),
  definition = 'b'.repeat(64)
const custom = {
  id: 'rol_custom',
  name: 'Recorded role',
  builtin: false,
  permission_count: 1,
  definition_etag: definition,
}
const workspace = () => ({
  user_id: 'usr_target',
  observed_at: '2026-10-05T03:04:05.123456789Z',
  identity_role: 'member',
  subject_status: 'active',
  builtin_role: { ...custom, id: 'rol_member', name: 'Member', builtin: true, permission_count: 0 },
  assigned_roles: [{ ...custom }],
  effective_permissions: ['providers.read'],
  permission_use: 'active',
  etag,
  can_edit: true,
  edit_blockers: [],
  candidate_status: 'available',
})
const input = () => ({
  role_ids: ['rol_custom'],
  role_definitions: [{ id: 'rol_custom', etag: definition }],
  builtin_definition_etag: definition,
  reason: 'Reviewed assignment',
})
const original = client.defaults.adapter
afterEach(() => {
  client.defaults.adapter = original
})
describe('strict Member Roles resources', () => {
  it('preserves complete101/10000 retained rows without replacement truncation', () => {
    for (const count of [101, 10000]) {
      const page = workspace()
      page.can_edit = count <= 1000
      page.edit_blockers = count > 1000 ? (['assignment_audit_bound'] as never[]) : []
      page.assigned_roles = Array.from({ length: count }, (_, i) => ({
        ...custom,
        id: `legacy_${String(i).padStart(5, '0')}`,
      }))
      expect(validateMemberRoles(page, 'usr_target').assigned_roles).toHaveLength(count)
    }
  })
  it('preserves empty/control recorded names and zero recorded permission counts', () => {
    const page = workspace()
    page.assigned_roles[0] = { ...custom, name: '\t', permission_count: 0 }
    expect(validateMemberRoles(page, 'usr_target').assigned_roles[0].name).toBe('\t')
  })
  it.each([
    [
      'wrong target',
      (p: ReturnType<typeof workspace>) => {
        p.user_id = 'USR_target'
      },
    ],
    [
      'extra field',
      (p: ReturnType<typeof workspace>) => {
        Object.assign(p, { receipt: true })
      },
    ],
    [
      'fractional count',
      (p: ReturnType<typeof workspace>) => {
        p.assigned_roles[0].permission_count = 0.5
      },
    ],
    [
      'large count',
      (p: ReturnType<typeof workspace>) => {
        p.assigned_roles[0].permission_count = 101
      },
    ],
    [
      'duplicate row',
      (p: ReturnType<typeof workspace>) => {
        p.assigned_roles.push(custom)
      },
    ],
    [
      'wrong builtin',
      (p: ReturnType<typeof workspace>) => {
        p.builtin_role.id = 'rol_admin'
      },
    ],
    [
      'builtin custom',
      (p: ReturnType<typeof workspace>) => {
        p.assigned_roles[0] = { ...custom, builtin: true }
      },
    ],
    [
      'weak proof',
      (p: ReturnType<typeof workspace>) => {
        p.etag = 'W/"' + etag + '"'
      },
    ],
    [
      'zero observation',
      (p: ReturnType<typeof workspace>) => {
        p.observed_at = '0001-01-01T00:00:00.000000000Z'
      },
    ],
    [
      'calendar rollover',
      (p: ReturnType<typeof workspace>) => {
        p.observed_at = '2026-02-30T03:04:05Z'
      },
    ],
    [
      'unknown union code',
      (p: ReturnType<typeof workspace>) => {
        p.effective_permissions = ['legacy.unknown']
      },
    ],
    [
      'duplicate union',
      (p: ReturnType<typeof workspace>) => {
        p.effective_permissions.push('providers.read')
      },
    ],
    [
      'inactive mismatch',
      (p: ReturnType<typeof workspace>) => {
        p.subject_status = 'disabled'
      },
    ],
    [
      'offboarded editable',
      (p: ReturnType<typeof workspace>) => {
        p.subject_status = 'offboarded'
        p.permission_use = 'inactive'
      },
    ],
  ])('rejects %s', (_name, change) => {
    const page = workspace()
    change(page)
    expect(() => validateMemberRoles(page, 'usr_target')).toThrow()
  })
  it('validates scoped compact candidate and exact count/detail proof', () => {
    expect(
      validateMemberRoleCandidates({ items: [custom], next_cursor: null, etag }, etag).items,
    ).toHaveLength(1)
    const detail = { user_id: 'usr_target', role: custom, permissions: ['legacy.unknown'], etag }
    expect(validateMemberRoleDetail(detail, 'usr_target', etag, custom).permissions).toEqual([
      'legacy.unknown',
    ])
    for (const changed of [
      { ...detail, etag: definition },
      { ...detail, permissions: [] },
      { ...detail, role: { ...custom, definition_etag: etag } },
      { ...detail, secret: 'forbidden' },
    ])
      expect(() => validateMemberRoleDetail(changed, 'usr_target', etag, custom)).toThrow()
  })
  it.each(['projects.models.WRITE', 'projects.limits.WRITE', 'LEGACY_Unknown-01', 'A'.repeat(80)])(
    'preserves safe recorded permission case in exact scoped detail: %s',
    (permission) => {
      const detail = { user_id: 'usr_target', role: custom, permissions: [permission], etag }
      expect(validateMemberRoleDetail(detail, 'usr_target', etag, custom).permissions).toEqual([
        permission,
      ])
      const page = workspace()
      page.effective_permissions = [permission]
      expect(() => validateMemberRoles(page, 'usr_target')).toThrow()
    },
  )
  it.each([
    '',
    'A'.repeat(81),
    'projects.models. WRITE',
    'projects.models.WRITE\n',
    'projects.models.WRITE\t',
    'projects.models.WRITE\0',
    'projects/models/WRITE',
    'projects:models:WRITE',
    '项目.WRITE',
    'projects.models.ＷRITE',
  ])('rejects unsafe recorded definition code %#', (permission) => {
    const detail = { user_id: 'usr_target', role: custom, permissions: [permission], etag }
    expect(() => validateMemberRoleDetail(detail, 'usr_target', etag, custom)).toThrow()
  })
  it('rejects candidate builtin, duplicates, oversize and wrong review', () => {
    for (const data of [
      { items: [{ ...custom, builtin: true }], next_cursor: null, etag },
      { items: [custom, custom], next_cursor: null, etag },
      {
        items: Array.from({ length: 51 }, (_, i) => ({ ...custom, id: `rol_${i}` })),
        next_cursor: null,
        etag,
      },
      { items: [], next_cursor: null, etag: definition },
    ])
      expect(() => validateMemberRoleCandidates(data, etag)).toThrow()
  })
  it('confirms only exact current IDs and fixed database effect', () => {
    const result = {
      user_id: 'usr_target',
      role_ids: ['rol_custom'],
      etag,
      confirmation: 'current_member_roles',
      effect: 'current_database',
    }
    expect(validateMemberRolesConfirmation(result, 'usr_target', ['rol_custom'])).toEqual(result)
    for (const changed of [
      { ...result, effect: 'runtime' },
      { ...result, receipt: 'invented' },
      { ...result, role_ids: [] },
      { ...result, user_id: 'USR_target' },
    ])
      expect(() => validateMemberRolesConfirmation(changed, 'usr_target', ['rol_custom'])).toThrow()
  })
  it.each([
    '',
    ' leading',
    'trailing ',
    'line\nbreak',
    '\uD800',
    'a'.repeat(1025),
    '中'.repeat(342),
  ])('rejects invalid reason %#', (reason) => expect(validMemberRolesReason(reason)).toBe(false))
  it('preserves exact UTF8 reason bound', () => {
    expect(validMemberRolesReason('a'.repeat(1024))).toBe(true)
    expect(validMemberRolesReason('中'.repeat(341))).toBe(true)
  })
})
describe('exact resource transport', () => {
  it('preserves literal UTF8 search at the exact byte bound and rejects oversize before HTTP', async () => {
    expect(validMemberRoleSearch('中'.repeat(66) + '%_')).toBe(true)
    expect(validMemberRoleSearch('中'.repeat(67))).toBe(false)
    expect(validMemberRoleSearch('\uD800')).toBe(false)
    let count = 0
    client.defaults.adapter = async (config) => {
      count++
      expect(config.params.q).toBe('中'.repeat(66) + '%_')
      return {
        config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders({ etag: `"${etag}"`, 'cache-control': 'private, no-store' }),
        data: { items: [], next_cursor: null, etag },
      }
    }
    await getMemberRoleCandidates('usr_target', etag, { q: '中'.repeat(66) + '%_', cursor: null })
    await expect(
      getMemberRoleCandidates('usr_target', etag, { q: '中'.repeat(67), cursor: null }),
    ).rejects.toThrow()
    expect(count).toBe(1)
  })
  it('sends scoped workspace/detail/candidate If-Match and immutable complete PUT with current CSRF', async () => {
    const calls: InternalAxiosRequestConfig[] = []
    client.defaults.adapter = async (config) => {
      calls.push(config)
      const data =
        config.method === 'put'
          ? {
              user_id: 'usr_target',
              role_ids: ['rol_custom'],
              etag,
              confirmation: 'current_member_roles',
              effect: 'current_database',
            }
          : config.url?.endsWith('/candidates')
            ? { items: [custom], next_cursor: null, etag }
            : config.url?.endsWith('/rol_custom')
              ? { user_id: 'usr_target', role: custom, permissions: ['providers.read'], etag }
              : workspace()
      return {
        config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders({ etag: `"${etag}"`, 'cache-control': 'private, no-store' }),
        data,
      }
    }
    await getMemberRoles('usr_target')
    await getMemberRoleCandidates('usr_target', etag, { q: '%_ literal', cursor: null })
    await getMemberRoleDetail('usr_target', etag, custom)
    const body = input()
    await setMemberRoles('usr_target', etag, body, 'fresh-csrf')
    expect(calls.map((c) => c.url)).toEqual([
      '/admin/members/usr_target/roles',
      '/admin/members/usr_target/roles/candidates',
      '/admin/members/usr_target/roles/rol_custom',
      '/admin/members/usr_target/roles',
    ])
    expect(calls[1].params).toEqual({ q: '%_ literal', limit: 25 })
    expect(calls.slice(1).map((c) => c.headers.get('If-Match'))).toEqual(Array(3).fill(`"${etag}"`))
    expect(calls[3].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
    expect(JSON.parse(calls[3].data)).toEqual(body)
  })
  it('rejects response strong-header disagreement before returning facts', async () => {
    client.defaults.adapter = async (config) => ({
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ etag: `"${definition}"`, 'cache-control': 'private, no-store' }),
      data: workspace(),
    })
    await expect(getMemberRoles('usr_target')).rejects.toThrow()
  })
  it('rejects malformed complete proof vector and builtin custom IDs before HTTP', async () => {
    let count = 0
    client.defaults.adapter = async () => {
      count++
      throw new Error('No dispatch')
    }
    for (const body of [
      { ...input(), role_definitions: [] },
      { ...input(), role_ids: ['rol_member'] },
      { ...input(), role_definitions: [{ id: 'rol_other', etag: definition }] },
      { ...input(), role_ids: Array(101).fill('rol_custom') },
    ])
      await expect(setMemberRoles('usr_target', etag, body, 'csrf')).rejects.toThrow()
    expect(count).toBe(0)
  })
})
