import { afterEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { getOverviewRoles, overviewRolesKey } from '@/api/overview'
const cursor = (id: string, actor = 'usr_member') => btoa(`${actor}|${id}`).replace(/=+$/, '')
const role = (id = 'rol_finance') => ({
  id,
  name: 'Finance',
  builtin: true,
  assignment_kind: 'explicit',
})
const page = () => ({
  actor_user_id: 'usr_member',
  observed_at: '2026-10-06T00:00:00.123456789Z',
  identity_role: 'member',
  roles: [role()],
  next_cursor: null,
})
afterEach(() => vi.restoreAllMocks())
it('requests only a bounded self Role page with abort signal and current actor/generation/cursor key', async () => {
  const data = page()
  const get = vi.spyOn(client, 'get').mockResolvedValue({ data })
  const signal = new AbortController().signal
  expect(await getOverviewRoles('usr_member', null, signal)).toEqual(data)
  expect(get).toHaveBeenCalledWith('/overview/roles', {
    params: { cursor: undefined, limit: 10 },
    signal,
  })
  expect(overviewRolesKey('usr_member', 'fresh', null)).toEqual([
    'overview-roles',
    'usr_member',
    'fresh',
    null,
  ])
})
it.each([
  ['extra public authority', { permissions: [] }],
  ['missing actor', { actor_user_id: undefined }],
  ['actor alias', { actor_user_id: 'USR_member' }],
  ['unknown identity role', { identity_role: 'owner' }],
  ['offset timestamp', { observed_at: '2026-10-06T00:00:00+00:00' }],
  ['invalid calendar', { observed_at: '2026-02-30T00:00:00Z' }],
  ['zero year', { observed_at: '0000-01-01T00:00:00Z' }],
  ['zero Go timestamp', { observed_at: '0001-01-01T00:00:00Z' }],
  ['excess timestamp precision', { observed_at: '2026-10-06T00:00:00.1234567890Z' }],
  ['intrinsic assignment', { roles: [role('rol_admin')] }],
  ['unknown builtin', { roles: [role('rol_custom')] }],
  ['wrong duty classification', { roles: [{ ...role(), builtin: false }] }],
  ['wrong assignment', { roles: [{ ...role(), assignment_kind: 'intrinsic' }] }],
  ['unsafe id', { roles: [role('rol_bad ')] }],
  ['duplicate', { roles: [role(), role()] }],
  ['numeric name', { roles: [{ ...role(), name: 2 }] }],
  ['blank name', { roles: [{ ...role(), name: '' }] }],
  ['control name', { roles: [{ ...role(), name: 'Bad\nname' }] }],
  ['invalid Unicode', { roles: [{ ...role(), name: '\ud800' }] }],
  ['extra private field', { roles: [{ ...role(), permissions: [] }] }],
  [
    'more than requested page',
    { roles: Array.from({ length: 11 }, (_, n) => ({ ...role(`rol_${n}`), builtin: false })) },
  ],
  ['cross actor cursor', { next_cursor: cursor('rol_finance', 'usr_other') }],
  ['wrong last-id cursor', { next_cursor: cursor('rol_wrong') }],
  ['empty with cursor', { roles: [], next_cursor: cursor('rol_finance') }],
] as [string, Record<string, unknown>][])('fails closed on %s', async (_name, delta) => {
  vi.spyOn(client, 'get').mockResolvedValue({ data: { ...page(), ...delta } })
  await expect(getOverviewRoles('usr_member', null)).rejects.toThrow()
})
it('retains exact custom name, FEFF and missing legacy-name fallback without translation', async () => {
  const data = {
    ...page(),
    roles: [
      { ...role('rol_'), builtin: false, name: null },
      { ...role('rol_custom'), builtin: false, name: '\ufeff原始名称\ufeff' },
    ],
  }
  vi.spyOn(client, 'get').mockResolvedValue({ data })
  expect(await getOverviewRoles('usr_member', null)).toEqual(data)
})
it('rejects cross-actor/invalid request cursors before any network call', async () => {
  const get = vi.spyOn(client, 'get')
  await expect(getOverviewRoles('usr_member', cursor('rol_x', 'usr_other'))).rejects.toThrow()
  await expect(getOverviewRoles('usr_member', 'bad%')).rejects.toThrow()
  expect(get).not.toHaveBeenCalled()
})
it('accepts ordered current page and rejects reused or backward continuation', async () => {
  const data = { ...page(), roles: [role()], next_cursor: cursor('rol_finance') }
  const get = vi.spyOn(client, 'get').mockResolvedValue({ data })
  expect((await getOverviewRoles('usr_member', null)).next_cursor).toBe(data.next_cursor)
  await expect(getOverviewRoles('usr_member', data.next_cursor)).rejects.toThrow()
  get.mockResolvedValue({
    data: { ...data, roles: [{ ...role('rol_z'), builtin: false }], next_cursor: null },
  })
  expect((await getOverviewRoles('usr_member', data.next_cursor)).roles[0].id).toBe('rol_z')
})
