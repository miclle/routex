import assert from 'node:assert/strict'
import { randomUUID } from 'node:crypto'
import { isDeepStrictEqual } from 'node:util'

const noticeFields = [
  'id',
  'kind',
  'severity',
  'detail_code',
  'occurrence_count',
  'read',
  'first_seen_at',
  'last_seen_at',
  'read_at',
].sort()

export function completionNotices(page, expectedCount, privateValues = []) {
  assert.ok(Array.isArray(page.items), 'Completion inbox did not return a list')
  assert.ok(!page.next_cursor, 'Controlled completion inbox was not fully captured')
  const notices = page.items.filter((item) => item.kind === 'offboarding_completed')
  assert.equal(notices.length, expectedCount, 'Completion recipient count changed')
  for (const notice of notices) {
    assert.deepEqual(
      Object.keys(notice).sort(),
      noticeFields,
      'Completion notice exposed extra or missing fields',
    )
    assert.ok(
      typeof notice.id === 'string' && notice.id.length > 0,
      'Completion notice identity is missing',
    )
    assert.equal(notice.severity, 'medium', 'Completion notice severity changed')
    assert.equal(notice.detail_code, 'completed', 'Completion notice detail changed')
    assert.equal(notice.occurrence_count, 1, 'Completion replay duplicated occurrences')
    assert.equal(typeof notice.read, 'boolean', 'Completion read state is unknown')
    for (const field of ['first_seen_at', 'last_seen_at']) {
      assert.ok(
        typeof notice[field] === 'string' && notice[field].length > 0,
        'Completion occurrence timestamp is missing',
      )
    }
    assert.equal(
      notice.first_seen_at,
      notice.last_seen_at,
      'Completion occurrence timestamps changed',
    )
    if (notice.read) {
      assert.ok(
        typeof notice.read_at === 'string' && notice.read_at.length > 0,
        'Read completion timestamp is missing',
      )
    } else {
      assert.equal(notice.read_at, null, 'Unread completion notice has a read timestamp')
    }
    const serialized = JSON.stringify(notice)
    for (const value of privateValues) {
      assert.ok(
        value && !serialized.includes(value),
        'Completion notice exposed private source data',
      )
    }
  }
  return notices
}

export function assertCompletionNoticesRetained(page, retained, privateValues = []) {
  // Both snapshots come from persisted GETs on the same driver. Exact strings
  // retain all database timestamp precision without a millisecond conversion.
  assert.deepEqual(
    completionNotices(page, retained.length, privateValues),
    retained,
    'Restart or identical completion retry changed persisted notices',
  )
}

export async function prepareOffboardingNoticeRestart({
  origin,
  request,
  sessionCookie,
  administrator,
}) {
  const read = async (path, recipient = administrator) =>
    (await request(origin, path, 200, { cookie: recipient.cookie })).json()
  const write = async (path, body, status = 200, method = 'POST', recipient = administrator) =>
    (
      await request(origin, path, status, {
        method,
        cookie: recipient.cookie,
        csrf: recipient.session.csrf_token,
        body,
      })
    ).json()
  const smtpDisabled = async () =>
    assert.equal(
      (await read('/admin/smtp')).enabled,
      false,
      'Completion restart requires disabled SMTP',
    )
  const inbox = (recipient) => read('/notifications?status=all&limit=100', recipient)
  await smtpDisabled()

  const members = []
  for (const label of ['departing', 'successor', 'stranger']) {
    const credentials = {
      email: `notice-${label}@example.invalid`,
      password: `test-only-notice-${label}-password`,
    }
    const member = await write(
      '/admin/members',
      { ...credentials, name: `Notice ${label}`, role: 'member' },
      201,
    )
    assert.ok(member.id, 'Controlled member identity is missing')
    const login = await request(origin, '/auth/login', 200, {
      method: 'POST',
      body: credentials,
    })
    const cookie = sessionCookie(login)
    // Capture real Session facts instead of inferring identity from login success.
    const session = await read('/auth/session', { cookie })
    assert.equal(session.user.id, member.id, 'Controlled Session identity changed')
    assert.ok(session.csrf_token, 'Controlled Session has no CSRF token')
    members.push({ cookie, session, id: member.id })
  }
  const [departing, successor, stranger] = members
  const team = await write(
    '/admin/teams',
    { name: 'Notice handover Team', owner_ids: [departing.id] },
    201,
  )
  const project = await write('/projects', { name: 'Notice handover Project' }, 201)
  await write(`/admin/projects/${project.id}/managers`, { user_ids: [departing.id] }, 200, 'PUT')
  const path = `/admin/members/${departing.id}/offboarding`
  const inventory = await read(path)
  assert.deepEqual(
    inventory.projects.map((item) => item.id),
    [project.id],
    'Controlled Project inventory changed',
  )
  assert.deepEqual(
    inventory.teams.map((item) => item.id),
    [team.id],
    'Controlled Team inventory changed',
  )
  const reason = 'Controlled completion-notice restart'
  const plan = await write(
    `${path}/plans`,
    {
      request_id: randomUUID(),
      inventory_version: inventory.inventory_version,
      planned_at: new Date(Date.now() + 86400000).toISOString(),
      reason,
      project_assignments: [{ project_id: project.id, manager_user_ids: [successor.id] }],
      team_assignments: [
        {
          team_id: team.id,
          owner_user_ids: [successor.id],
          add_member_user_ids: [successor.id],
        },
      ],
    },
    201,
  )
  assert.equal(plan.mode, 'planned', 'Controlled handover mode changed')
  assert.equal(plan.status, 'ready_to_complete', 'Controlled handover did not remain a plan')
  const privateValues = [
    plan.id,
    departing.id,
    successor.id,
    stranger.id,
    administrator.session.user.id,
    project.id,
    team.id,
    reason,
  ]
  for (const recipient of [administrator, ...members])
    completionNotices(await inbox(recipient), 0, privateValues)

  const completed = await write(`${path}/${plan.id}/complete`, {})
  assert.equal(completed.id, plan.id, 'Completion changed Case identity')
  assert.equal(completed.status, 'completed', 'Controlled Case did not complete')
  assert.equal(completed.completed_by, administrator.session.user.id, 'Completion executor changed')
  const canonicalCase = async () => {
    const current = await read(path)
    assert.equal(current.disabled, true, 'Completion did not disable the departing member')
    assert.ok(current.offboarded_at, 'Completion offboarded timestamp is missing')
    assert.equal(current.cases.length, 1, 'Completion created an extra Case')
    const record = current.cases[0]
    assert.equal(record.id, plan.id, 'Persisted Case identity changed')
    assert.equal(record.status, 'completed', 'Persisted Case is incomplete')
    assert.equal(record.completed_by, administrator.session.user.id, 'Persisted executor changed')
    assert.ok(record.completed_at, 'Persisted completion timestamp is missing')
    return { record, offboarded_at: current.offboarded_at }
  }
  const retainedCase = await canonicalCase()
  const scopeFacts = async () => {
    const currentTeam = await read(`/admin/teams/${team.id}`)
    const currentProject = await read(`/admin/projects/${project.id}`)
    assert.equal(currentTeam.members.length, 1, 'Completion left unrelated Team membership')
    assert.equal(currentTeam.members[0].user_id, successor.id, 'Actual Team successor changed')
    assert.equal(currentTeam.members[0].role, 'owner', 'Actual Team successor is not an owner')
    assert.equal(currentTeam.members[0].status, 'active', 'Actual Team successor is not active')
    assert.equal(currentProject.managers.length, 1, 'Completion left unrelated Project management')
    assert.match(
      currentProject.managers[0].id,
      /^pmg_[0-7][0-9a-hjkmnp-tv-z]{25}$/,
      'Completion created a noncanonical Project manager relationship',
    )
    const successorProject = await read(`/projects/${project.id}`, successor)
    assert.equal(
      successorProject.id,
      project.id,
      'The real successor cannot read the handed-over Project',
    )
    assert.equal(
      successorProject.managers[0].id,
      currentProject.managers[0].id,
      'Scoped successor read lost the exact relationship',
    )
    assert.equal(
      currentProject.managers[0].user_id,
      successor.id,
      'Actual Project successor changed',
    )
    return { members: currentTeam.members, managers: currentProject.managers }
  }
  const retainedScopes = await scopeFacts()
  const executorNotice = completionNotices(await inbox(administrator), 1, privateValues)[0]
  const successorNotice = completionNotices(await inbox(successor), 1, privateValues)[0]
  assert.notEqual(
    executorNotice.id,
    successorNotice.id,
    'Recipients shared the same inbox identity',
  )
  assert.equal(executorNotice.read, false, 'New executor notice is already read')
  assert.equal(successorNotice.read, false, 'New successor notice is already read')
  completionNotices(await inbox(stranger), 0, privateValues)
  const denied = async () => {
    await request(origin, '/auth/session', 401, { cookie: departing.cookie })
    await request(origin, '/notifications?status=all&limit=100', 401, {
      cookie: departing.cookie,
    })
    await request(origin, `/notifications/${successorNotice.id}/read`, 404, {
      method: 'POST',
      cookie: stranger.cookie,
      csrf: stranger.session.csrf_token,
    })
    await request(origin, '/admin/alerts', 403, { cookie: successor.cookie })
  }
  await denied()
  const marked = await write(
    `/notifications/${successorNotice.id}/read`,
    undefined,
    200,
    'POST',
    successor,
  )
  // The mutation's timestamp can precede database precision normalization.
  assert.equal(marked.id, successorNotice.id, 'Read mutation changed notice identity')
  assert.equal(marked.read, true, 'Read mutation did not acknowledge read state')
  const retainedExecutor = completionNotices(await inbox(administrator), 1, privateValues)
  const retainedSuccessor = completionNotices(await inbox(successor), 1, privateValues)
  assert.equal(retainedSuccessor[0].read, true, 'Persisted successor notice is not read')

  const unchanged = async () => {
    assertCompletionNoticesRetained(await inbox(administrator), retainedExecutor, privateValues)
    assertCompletionNoticesRetained(await inbox(successor), retainedSuccessor, privateValues)
    completionNotices(await inbox(stranger), 0, privateValues)
    assert.deepEqual(
      await canonicalCase(),
      retainedCase,
      'Completion retry changed persisted Case facts',
    )
    assert.deepEqual(
      await scopeFacts(),
      retainedScopes,
      'Completion retry changed applied successor relationships',
    )
    await smtpDisabled()
  }
  const replay = async () => {
    assert.deepEqual(
      await write(`${path}/${plan.id}/complete`, {}),
      retainedCase.record,
      'Identical completion replay changed persisted Case',
    )
    await unchanged()
  }
  await replay()

  return async () => {
    for (const recipient of [administrator, successor, stranger]) {
      assert.ok(
        isDeepStrictEqual(await read('/auth/session', recipient), recipient.session),
        'Restart changed an original recipient Session',
      )
    }
    await denied()
    await unchanged()
    await replay()
  }
}
