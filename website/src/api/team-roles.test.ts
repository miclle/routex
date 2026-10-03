import { beforeEach, afterEach, describe, it, expect } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { getTeamRoles, getTeamRoleCandidates, saveTeamRoles } from './team-roles'
import { getResourceCandidates } from './resources'
import { teamRolesFixture, roleFixture } from '@/views/resources/team-role-fixture'
const original = client.defaults.adapter
let value: unknown, requests: InternalAxiosRequestConfig[]
beforeEach(() => {
  requests = []
  value = teamRolesFixture()
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { config, data: value, status: 200, statusText: '', headers: new AxiosHeaders() }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
describe('Team-scoped roles API', () => {
  it('reads an exact target without querying the platform role directory', async () => {
    await expect(getTeamRoles('tea_one')).resolves.toMatchObject({ team_id: 'tea_one' })
    expect(requests[0].url).toBe('/teams/tea_one/roles')
  })
  it.each([
    { team_id: 'tea_other' },
    { actor_team_actions: ['teams.tokens.write'] },
    { effective_team_actions: ['teams.models.write'] },
    { can_assign_roles: undefined },
    { etag: 'weak' },
    { role_ids: ['rol_editor', 'rol_editor'] },
    { roles: [{ ...roleFixture(), team_actions: ['providers.write'] }] },
  ])('rejects invalid or expanded scoped authority %j', async (patch) => {
    value = { ...teamRolesFixture(), ...patch }
    await expect(getTeamRoles('tea_one')).rejects.toThrow()
  })
  it('retains inert assigned roles without inventing their old actions', async () => {
    value = {
      ...teamRolesFixture(),
      roles: [{ ...roleFixture(), team_actions: [] }],
      effective_team_actions: [],
    }
    await expect(getTeamRoles('tea_one')).resolves.toMatchObject({ effective_team_actions: [] })
  })
  it('accepts saved assignments with no effective actions on an inactive Team', async () => {
    value = { ...teamRolesFixture(), can_assign_roles: true, effective_team_actions: [] }
    await expect(
      saveTeamRoles('tea_one', ['rol_editor'], 'Reviewed assignment', 'a'.repeat(64), 'csrf'),
    ).resolves.toMatchObject({ effective_team_actions: [], roles: [roleFixture()] })
  })
  it('omits empty target searches from serialized role, member and model requests', async () => {
    value = { items: [], next_cursor: null, etag: 'a'.repeat(64) }
    await getTeamRoleCandidates('tea_one', '', null)
    expect(client.getUri(requests[0])).toBe('/api/v1/teams/tea_one/role-candidates?limit=50')
    value = { items: [] }
    await getResourceCandidates('/teams/tea_one/member-candidates', '')
    await getResourceCandidates('/teams/tea_one/model-candidates', '')
    expect(client.getUri(requests[1])).toBe('/api/v1/teams/tea_one/member-candidates')
    expect(client.getUri(requests[2])).toBe('/api/v1/teams/tea_one/model-candidates')
  })
  it('uses resource-scoped bounded search and cursor with the reviewed generation', async () => {
    value = { items: [roleFixture('rol_models')], next_cursor: 'next', etag: 'a'.repeat(64) }
    await expect(getTeamRoleCandidates('tea_one', 'literal_%', null)).resolves.toMatchObject({
      next_cursor: 'next',
    })
    expect(requests[0].params).toEqual({ query: 'literal_%', cursor: undefined, limit: 50 })
  })
  it.each([
    { items: [undefined], next_cursor: null, etag: 'a'.repeat(64) },
    { items: [], next_cursor: 'repeat', etag: 'a'.repeat(64) },
    { items: [], next_cursor: null, etag: 'bad' },
  ])('rejects malformed candidate generations %j', async (response) => {
    value = response
    await expect(getTeamRoleCandidates('tea_one', '', 'repeat')).rejects.toThrow()
  })
  it('sends the exact full assignment, reason and reviewed header with current CSRF', async () => {
    value = { ...teamRolesFixture(), role_ids: [], roles: [], effective_team_actions: [] }
    await saveTeamRoles('tea_one', [], 'Reviewed removal', 'a'.repeat(64), 'current-csrf')
    expect(JSON.parse(requests[0].data)).toEqual({ role_ids: [], reason: 'Reviewed removal' })
    expect(requests[0].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('current-csrf')
    value = teamRolesFixture()
    await expect(
      saveTeamRoles('tea_one', [], 'Reviewed removal', 'a'.repeat(64), 'current-csrf'),
    ).rejects.toThrow()
  })
})
