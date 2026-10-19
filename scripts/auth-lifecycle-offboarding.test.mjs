import assert from 'node:assert/strict'
import test from 'node:test'
import {
  completionNotices,
  assertCompletionNoticesRetained,
  prepareOffboardingNoticeRestart,
} from './auth-lifecycle-offboarding.mjs'

const timestamp = '2026-10-14T01:02:03.123456789Z'
const notice = (id = 'ntf_control') => ({
  id,
  kind: 'offboarding_completed',
  severity: 'medium',
  detail_code: 'completed',
  occurrence_count: 1,
  read: false,
  first_seen_at: timestamp,
  last_seen_at: timestamp,
  read_at: null,
})
const page = (...items) => ({
  items,
  unread_count: items.filter((item) => !item.read).length,
})

test('recipient capture requires a complete page and exactly one deduplicated notice', () => {
  assert.equal(completionNotices(page(notice()), 1).length, 1)
  assert.throws(() => completionNotices(page(notice(), notice('ntf_extra')), 1), /recipient count/)
  assert.throws(
    () => completionNotices({ ...page(notice()), next_cursor: 'later' }, 1),
    /fully captured/,
  )
  assert.throws(() => completionNotices(page(), 1), /recipient count/)
})

for (const field of [
  'alert_id',
  'subject_type',
  'subject_id',
  'subject_name',
  'case_id',
  'recipient_id',
  'delivery_status',
  'delivery_code',
  'delivery_attempts',
  'delivery_updated_at',
]) {
  test(`generic completion projection rejects ${field}`, () => {
    assert.throws(
      () => completionNotices(page({ ...notice(), [field]: 'private' }), 1),
      /extra or missing fields/,
    )
  })
}

test('generic completion content excludes known private source values', () => {
  assert.throws(
    () => completionNotices(page(notice('ntf_private-source')), 1, ['private-source']),
    /private source/,
  )
})

test('nullable read timestamps preserve unread versus read facts', () => {
  assert.throws(() => completionNotices(page({ ...notice(), read_at: timestamp }), 1), /Unread/)
  assert.throws(
    () => completionNotices(page({ ...notice(), read: true }), 1),
    /timestamp is missing/,
  )
  completionNotices(page({ ...notice(), read: true, read_at: timestamp }), 1)
})

test('persisted timestamp comparison retains nanoseconds and rejects read-state drift', () => {
  const retained = [{ ...notice(), read: true, read_at: timestamp }]
  assertCompletionNoticesRetained(page(structuredClone(retained[0])), retained)
  for (const changed of [
    { ...retained[0], read_at: '2026-10-14T01:02:03.123456788Z' },
    { ...retained[0], read_at: '2026-10-14T01:02:03.123Z' },
    notice(),
  ])
    assert.throws(
      () => assertCompletionNoticesRetained(page(changed), retained),
      /changed persisted notices/,
    )
})

function controlledAPI() {
  const administrator = {
    cookie: 'cookie-administrator',
    session: {
      user: { id: 'usr_executor', role: 'admin' },
      csrf_token: 'csrf-executor',
    },
  }
  const sessions = new Map([[administrator.cookie, administrator.session]])
  const inboxes = new Map([[administrator.cookie, []]])
  const calls = []
  const people = new Map()
  const settings = {
    enabled: false,
    managerID: 'pmg_00000000000000000000000001',
    successorProjectDenied: false,
  }
  let record
  let completed = false
  let memberCount = 0
  const clone = (value) => structuredClone(value)
  const request = async (origin, path, expectedStatus, options = {}) => {
    const { method = 'GET', cookie, csrf, body } = options
    calls.push({ path, method, cookie })
    let status = 200
    let data
    let loginCookie
    if (path === '/auth/login') {
      const person = people.get(body.email)
      loginCookie = `cookie-${person.label}`
      sessions.set(loginCookie, {
        user: { id: person.id, role: 'member' },
        csrf_token: `csrf-${person.label}`,
      })
      inboxes.set(loginCookie, [])
      data = sessions.get(loginCookie)
    } else if (!sessions.has(cookie) || (completed && cookie === 'cookie-departing')) {
      status = 401
    } else {
      if (method !== 'GET') assert.equal(csrf, sessions.get(cookie).csrf_token)
      if (path === '/auth/session') data = sessions.get(cookie)
      else if (path === '/admin/smtp') data = settings
      else if (path === '/admin/members') {
        const label = ['departing', 'successor', 'stranger'][memberCount++]
        data = { id: `usr_${label}` }
        people.set(body.email, { ...data, label })
        status = 201
      } else if (path === '/admin/teams') {
        data = { id: 'tem_handover' }
        status = 201
      } else if (path === '/projects') {
        data = { id: 'prj_handover' }
        status = 201
      } else if (path === '/admin/projects/prj_handover/managers') data = {}
      else if (path.endsWith('/offboarding/plans')) {
        record = {
          id: 'obc_handover',
          mode: 'planned',
          status: 'ready_to_complete',
          request_id: body.request_id,
          completed_by: '',
          completed_at: null,
        }
        data = record
        status = 201
      } else if (path.endsWith('/obc_handover/complete')) {
        if (!completed) {
          completed = true
          record = {
            ...record,
            status: 'completed',
            completed_by: administrator.session.user.id,
            completed_at: timestamp,
          }
          inboxes.set(administrator.cookie, [notice('ntf_executor')])
          inboxes.set('cookie-successor', [notice('ntf_successor')])
        }
        data = record
      } else if (path.endsWith('/offboarding'))
        data = {
          projects: [{ id: 'prj_handover' }],
          teams: [{ id: 'tem_handover' }],
          inventory_version: 'inventory-controlled',
          disabled: completed,
          offboarded_at: completed ? timestamp : null,
          cases: record ? [record] : [],
        }
      else if (path === '/admin/teams/tem_handover')
        data = {
          members: [
            {
              id: 'tmm_successor',
              user_id: 'usr_successor',
              role: 'owner',
              status: 'active',
            },
          ],
        }
      else if (path === '/admin/projects/prj_handover')
        data = {
          managers: [{ id: settings.managerID, user_id: 'usr_successor' }],
        }
      else if (path === '/projects/prj_handover') {
        if (cookie !== 'cookie-successor' || settings.successorProjectDenied) status = 403
        else
          data = {
            id: 'prj_handover',
            managers: [{ id: settings.managerID, user_id: 'usr_successor' }],
          }
      } else if (path.startsWith('/notifications?')) data = page(...inboxes.get(cookie))
      else if (path === '/notifications/ntf_successor/read') {
        if (cookie === 'cookie-stranger') status = 404
        else {
          inboxes.get(cookie)[0] = {
            ...inboxes.get(cookie)[0],
            read: true,
            read_at: timestamp,
          }
          // The mutation wire precision intentionally differs from persisted GET.
          data = {
            id: 'ntf_successor',
            read: true,
            read_at: '2026-10-14T01:02:03.123456799Z',
          }
        }
      } else if (path === '/admin/alerts') status = 403
      else assert.fail('Unexpected controlled API path')
    }
    assert.equal(status, expectedStatus, 'Unexpected controlled API status')
    return { json: async () => clone(data), loginCookie }
  }
  return {
    administrator,
    request,
    sessionCookie: (response) => response.loginCookie,
    calls,
    sessions,
    inboxes,
    settings,
    origin: 'http://127.0.0.1:1',
  }
}

test('restart controller retains original Sessions and compares persisted GET timestamps', async () => {
  const fixture = controlledAPI()
  const retained = await prepareOffboardingNoticeRestart(fixture)
  const initialLoginCount = fixture.calls.filter((call) => call.path === '/auth/login').length
  assert.equal(initialLoginCount, 3)
  await retained()
  assert.equal(
    fixture.calls.filter((call) => call.path === '/auth/login').length,
    initialLoginCount,
  )
  assert.equal(
    fixture.calls.filter((call) => call.path.endsWith('/obc_handover/complete')).length,
    3,
  )
  assert.ok(
    fixture.calls.some(
      (call) => call.path === '/auth/session' && call.cookie === 'cookie-departing',
    ),
  )
})

test('restart controller rejects an original successor Session change without exposing proofs', async () => {
  const fixture = controlledAPI()
  const retained = await prepareOffboardingNoticeRestart(fixture)
  fixture.sessions.set('cookie-successor', {
    user: { id: 'usr_successor', role: 'member' },
    csrf_token: 'private-changed-proof',
  })
  await assert.rejects(retained, (error) => {
    assert.match(error.message, /original recipient Session/)
    assert.ok(!error.message.includes('private-changed-proof'))
    return true
  })
})

test('restart controller rejects changed persisted read timestamps', async () => {
  const fixture = controlledAPI()
  const retained = await prepareOffboardingNoticeRestart(fixture)
  fixture.inboxes.get('cookie-successor')[0].read_at = '2026-10-14T01:02:03.123456788Z'
  await assert.rejects(retained, /changed persisted notices/)
})

test('restart controller stops before member creation when SMTP is enabled', async () => {
  const fixture = controlledAPI()
  fixture.settings.enabled = true
  await assert.rejects(() => prepareOffboardingNoticeRestart(fixture), /disabled SMTP/)
  assert.deepEqual(
    fixture.calls.map((call) => call.path),
    ['/admin/smtp'],
  )
})

test('restart controller rejects a stranger inheriting a recipient notice', async () => {
  const fixture = controlledAPI()
  const retained = await prepareOffboardingNoticeRestart(fixture)
  fixture.inboxes.set('cookie-stranger', [notice('ntf_foreign')])
  await assert.rejects(retained, /recipient count/)
})

test('restart controller rejects enabled SMTP after restart', async () => {
  const fixture = controlledAPI()
  const retained = await prepareOffboardingNoticeRestart(fixture)
  fixture.settings.enabled = true
  await assert.rejects(retained, /disabled SMTP/)
})

test('handover qualification rejects a newly generated historical manager prefix', async () => {
  const fixture = controlledAPI()
  fixture.settings.managerID = 'pjm_00000000000000000000000001'
  await assert.rejects(
    () => prepareOffboardingNoticeRestart(fixture),
    /noncanonical Project manager/,
  )
})

test('handover qualification requires the successor own authorized Project read', async () => {
  const fixture = controlledAPI()
  fixture.settings.successorProjectDenied = true
  await assert.rejects(
    () => prepareOffboardingNoticeRestart(fixture),
    /Unexpected controlled API status/,
  )
  assert.ok(
    fixture.calls.some(
      (call) => call.path === '/projects/prj_handover' && call.cookie === 'cookie-successor',
    ),
  )
})
