import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, describe, expect, it } from 'vitest'
import client from './client'
import {
  createTeamModelRequest,
  decideTeamModelRequest,
  getTeamModelCandidate,
  getTeamModelRequest,
  getTeamModelWorkspace,
  listTeamModelCandidates,
  listTeamModelRequests,
  listTeamModelRequestTeams,
  teamModelOutcomeUnknown,
  validTeamModelReason,
} from './team-model-requests'
import {
  teamModelCandidate,
  teamModelDetail,
  teamModelETag,
  teamModelTeam,
  teamModelTime,
  teamModelUUID,
  teamModelWorkspace,
} from '@/views/team-model-requests/fixture'
const original = client.defaults.adapter
function respond(data: unknown) {
  const calls: InternalAxiosRequestConfig[] = []
  client.defaults.adapter = async (config) => {
    calls.push(config)
    return { config, status: 200, statusText: '', headers: new AxiosHeaders(), data }
  }
  return calls
}
afterEach(() => {
  client.defaults.adapter = original
})
describe('Team model request API boundaries', () => {
  it('reads only bounded minimal active memberships and preserves exact search/cursor', async () => {
    const data = { items: [teamModelTeam()], next_cursor: 'tea_next' }
    const calls = respond(data),
      signal = new AbortController().signal
    expect(await listTeamModelRequestTeams('[.*]', null, signal)).toEqual(data)
    expect(calls[0].url).toBe('/team-model-request-teams')
    expect(calls[0].params).toEqual({ q: '[.*]', cursor: undefined, limit: 50 })
    expect(calls[0].signal).toBe(signal)
    respond({ items: [{ id: 'tea_zero', name: 'Team' }], next_cursor: null })
    await expect(listTeamModelRequestTeams('', null)).rejects.toThrow('Invalid')
  })
  it.each([
    { items: [teamModelTeam(), teamModelTeam()], next_cursor: null },
    { items: Array.from({ length: 51 }, () => teamModelTeam()), next_cursor: null },
    { items: [], next_cursor: 'x'.repeat(513) },
  ])('rejects duplicate/overflow/malformed membership picker responses', async (value) => {
    respond(value)
    await expect(listTeamModelRequestTeams('', null)).rejects.toThrow('Invalid')
  })
  it('binds candidate reads to the exact Team and Model and hides other applicant identity', async () => {
    const candidate = { ...teamModelCandidate(), pending_request: true }
    const calls = respond(candidate)
    expect(await getTeamModelCandidate('tea_zero', 'mdl_model')).toEqual(candidate)
    expect(calls[0].url).toBe('/teams/tea_zero/model-request-candidates/mdl_model')
    respond({ ...candidate, team_id: 'tea_other' })
    await expect(getTeamModelCandidate('tea_zero', 'mdl_model')).rejects.toThrow('Invalid')
    respond({ ...candidate, id: 'mdl_other' })
    await expect(getTeamModelCandidate('tea_zero', 'mdl_model')).rejects.toThrow('Invalid')
    respond({ ...candidate, pending_request: false, own_pending_request_id: 'tmr_request' })
    await expect(getTeamModelCandidate('tea_zero', 'mdl_model')).rejects.toThrow('Invalid')
  })
  it('keeps Team candidate pages separate from writer grant-edit candidates', async () => {
    const calls = respond({ items: [teamModelCandidate()], next_cursor: null })
    await listTeamModelCandidates('tea_zero', 'literal%', null)
    expect(calls[0].url).toBe('/teams/tea_zero/model-request-candidates')
    expect(calls[0].params).toEqual({ q: 'literal%', cursor: undefined, limit: 50 })
  })
  it('uses server-filtered own history without current Team-directory access', async () => {
    const calls = respond({ items: [teamModelDetail()], total: 1, next_cursor: 'a'.repeat(300) })
    await listTeamModelRequests('usr_owner', undefined, '', null, {
      teamID: 'tea_zero',
      modelID: 'mdl_model',
    })
    expect(calls[0].url).toBe('/team-model-requests')
    expect(calls[0].params).toMatchObject({ team_id: 'tea_zero', model_id: 'mdl_model', limit: 50 })
    respond({
      items: [{ ...teamModelDetail(), applicant_user_id: 'usr_other' }],
      total: 1,
      next_cursor: null,
    })
    await expect(listTeamModelRequests('usr_owner', undefined, '', null)).rejects.toThrow('Invalid')
    respond({ items: [teamModelDetail('tea_other')], total: 1, next_cursor: null })
    await expect(
      listTeamModelRequests('usr_owner', undefined, '', null, { teamID: 'tea_zero' }),
    ).rejects.toThrow('Invalid')
  })
  it('rejects cross-Team reviewer rows while permitting other applicants in the exact workspace', async () => {
    const calls = respond({ items: [teamModelDetail()], total: 1, next_cursor: null })
    await listTeamModelRequests('usr_reviewer', 'tea_zero', 'pending', null, {
      modelID: 'mdl_model',
    })
    expect(calls[0].url).toBe('/teams/tea_zero/model-requests')
    expect(calls[0].params.team_id).toBeUndefined()
    respond({ items: [teamModelDetail('tea_other')], total: 1, next_cursor: null })
    await expect(listTeamModelRequests('usr_reviewer', 'tea_zero', '', null)).rejects.toThrow(
      'Invalid',
    )
  })
  it('accepts unavailable current facts on an exact own historical detail and rejects fabricated current application', async () => {
    const row = {
      ...teamModelDetail(),
      current_team: null,
      current_model: null,
      current_membership_matches: null,
      current_granted: null,
      runtime_applied: null,
      application_status: 'unavailable',
    }
    const calls = respond(row)
    expect(await getTeamModelRequest('usr_owner', undefined, 'tmr_request')).toEqual(row)
    expect(calls[0].url).toBe('/team-model-requests/tmr_request')
    respond({ ...row, current_granted: false })
    await expect(getTeamModelRequest('usr_owner', undefined, 'tmr_request')).rejects.toThrow(
      'Invalid',
    )
  })
  it('preserves exact creation Team/model/UUID/reason and reviewed strong ETag', async () => {
    const row = teamModelDetail(),
      calls = respond(row)
    const intent = {
      body: {
        request_id: teamModelUUID,
        team_id: 'tea_zero',
        model_id: 'mdl_model',
        reason: row.reason,
      },
      etag: teamModelETag,
    }
    await createTeamModelRequest('usr_owner', intent, 'csrf')
    expect(JSON.parse(calls[0].data)).toEqual(intent.body)
    expect(calls[0].headers.get('If-Match')).toBe(`"${teamModelETag}"`)
    expect(calls[0].headers.get('X-CSRF-Token')).toBe('csrf')
    respond({ ...row, team_id: 'tea_other' })
    await expect(createTeamModelRequest('usr_owner', intent, 'csrf')).rejects.toThrow('Invalid')
  })
  it('accepts shared applied grants after applicant membership changed without asserting applicant invocation', async () => {
    const row = teamModelDetail()
    const saved = {
      ...row,
      status: 'approved',
      resolved_at: teamModelTime,
      decision: {
        decision_id: teamModelUUID,
        actor_id: 'usr_reviewer',
        actor_name: 'Reviewer',
        action: 'approve',
        reason: '',
        decided_at: teamModelTime,
      },
    }
    const value = {
      decision_id: teamModelUUID,
      committed: true,
      saved_request: saved,
      current_membership_matches: false,
      current_granted: true,
      runtime_applied: true,
      application_status: 'applied',
    }
    const calls = respond(value),
      intent = {
        body: { decision_id: teamModelUUID, action: 'approve' as const, reason: '' },
        etag: teamModelETag,
      }
    expect(
      await decideTeamModelRequest('usr_reviewer', 'tea_zero', 'tmr_request', intent, 'csrf'),
    ).toEqual(value)
    expect(calls[0].url).toBe('/teams/tea_zero/model-requests/tmr_request/decision')
    respond({
      ...value,
      saved_request: { ...saved, decision: { ...saved.decision, actor_id: 'usr_other' } },
    })
    await expect(
      decideTeamModelRequest('usr_reviewer', 'tea_zero', 'tmr_request', intent, 'csrf'),
    ).rejects.toThrow('Invalid')
  })
  it('reads only the reviewed minimal Team grant workspace and enforces its exact identity/count/authority', async () => {
    const value = teamModelWorkspace(),
      calls = respond(value)
    expect(await getTeamModelWorkspace('tea_zero')).toEqual(value)
    expect(calls[0].url).toBe('/teams/tea_zero/model-request-workspace')
    for (const data of [
      { ...value, team_id: 'tea_other' },
      { ...value, model_count: 1 },
      { ...value, can_review_requests: false },
    ]) {
      respond(data)
      await expect(getTeamModelWorkspace('tea_zero')).rejects.toThrow('Invalid')
    }
  })
  it('checks raw UTF-8 bytes/control characters and preserves unknown HTTP outcomes', () => {
    expect(validTeamModelReason('中'.repeat(341))).toBe(true)
    expect(validTeamModelReason('中'.repeat(342))).toBe(false)
    expect(validTeamModelReason('a'.repeat(1024) + ' ')).toBe(false)
    for (const control of ['\n', '\u0085', '\u009f'])
      expect(validTeamModelReason(`Reason${control}`)).toBe(false)
    expect(teamModelOutcomeUnknown(new Error('Invalid response'))).toBe(true)
    for (const status of [403, 409, 503]) {
      expect(
        teamModelOutcomeUnknown(
          new AxiosError('', '', undefined, undefined, {
            status,
            statusText: '',
            headers: {},
            config: {} as InternalAxiosRequestConfig,
            data: {},
          }),
        ),
      ).toBe(status === 503)
    }
  })
})
