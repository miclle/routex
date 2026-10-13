import assert from 'node:assert/strict'
import test from 'node:test'
import { chmod, mkdir, mkdtemp, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import {
  alertKind,
  sender,
  recipient,
  replyTo,
  requireMailbox,
  requireMessage,
  requireNotice,
  requireNoticeState,
  requireSnapshot,
  assetsDigest,
  closePeer,
  requireInputs,
  requireQualification,
  requireRootKey,
} from './smtp-acceptance-support.mjs'

test('root key rejects hex, malformed Base64 and decoded lengths other than 32', () => {
  requireRootKey(Buffer.alloc(32, 0xa5).toString('base64'))
  for (const invalid of [
    Buffer.alloc(32, 0xa5).toString('hex'),
    Buffer.alloc(31).toString('base64'),
    Buffer.alloc(33).toString('base64'),
    Buffer.alloc(32).toString('base64').replace(/=$/, ''),
    `${Buffer.alloc(32).toString('base64')}\n`,
    Buffer.alloc(32).toString('base64').replace(/A=$/, 'B='),
    'not base64',
    null,
  ])
    assert.throws(() => requireRootKey(invalid))
})

test('asset census detects actual byte, mode, addition and deletion drift throughout the build tree', async () => {
  const repo = await mkdtemp(join(tmpdir(), 'routex-smtp-asset-control-'))
  try {
    const root = join(repo, 'website/build')
    await mkdir(join(root, 'assets/nested'), { recursive: true })
    await writeFile(join(root, 'index.html'), '<html></html>')
    const script = join(root, 'assets/nested/app.js')
    await writeFile(script, 'original')
    await chmod(script, 0o600)
    const before = await assetsDigest(repo)
    assert.deepEqual(
      before.rows.map(([path]) => path),
      ['website/build/assets/nested/app.js', 'website/build/index.html'],
    )
    assert.equal((await assetsDigest(repo)).digest, before.digest)
    await writeFile(script, 'changed')
    assert.notEqual((await assetsDigest(repo)).digest, before.digest)
    await writeFile(script, 'original')
    await chmod(script, 0o644)
    assert.notEqual((await assetsDigest(repo)).digest, before.digest)
    await chmod(script, 0o600)
    await writeFile(join(root, '.extra'), 'new')
    assert.notEqual((await assetsDigest(repo)).digest, before.digest)
    await rm(join(root, '.extra'))
    assert.equal((await assetsDigest(repo)).digest, before.digest)
    await rm(script)
    await assert.rejects(assetsDigest(repo), /incomplete/)
  } finally {
    await rm(repo, { recursive: true, force: true })
  }
})

test('asset census rejects symlink roots and nested assets and requires the actual index', async () => {
  const repo = await mkdtemp(join(tmpdir(), 'routex-smtp-asset-control-'))
  try {
    await mkdir(join(repo, 'website/build'), { recursive: true })
    await writeFile(join(repo, 'website/build/index.html'), '<html></html>')
    await writeFile(join(repo, 'outside.js'), 'outside')
    await symlink(join(repo, 'outside.js'), join(repo, 'website/build/app.js'))
    await assert.rejects(assetsDigest(repo), /Invalid embedded asset/)
    await rm(join(repo, 'website/build/app.js'))
    await writeFile(join(repo, 'website/build/app.js'), 'app')
    await rm(join(repo, 'website/build/index.html'))
    await writeFile(join(repo, 'website/build/other.html'), '<html></html>')
    await assert.rejects(assetsDigest(repo), /incomplete/)
    await rm(join(repo, 'website/build'), { recursive: true })
    await symlink(repo, join(repo, 'website/build'))
    await assert.rejects(assetsDigest(repo), /Invalid embedded asset root/)
  } finally {
    await rm(repo, { recursive: true, force: true })
  }
})

test('source qualification and late input checks reject missing or changed bound bytes', () => {
  const inputs = {
    source_digest: '1'.repeat(64),
    binary_sha256: '2'.repeat(64),
    helper_sha256: '3'.repeat(64),
    assets_digest: '4'.repeat(64),
  }
  const qualification = {
    ...inputs,
    mandatory_check: 'passed',
    ordinary_tests: 'passed',
    production_build: 'passed',
  }
  requireQualification(qualification, inputs)
  requireInputs(inputs, inputs)
  for (const field of Object.keys(inputs)) {
    assert.throws(() =>
      requireQualification(qualification, {
        ...inputs,
        [field]: 'f'.repeat(64),
      }),
    )
    assert.throws(() => requireInputs(inputs, { ...inputs, [field]: 'f'.repeat(64) }))
    assert.throws(() => requireQualification({ ...qualification, [field]: undefined }, inputs))
  }
  for (const field of ['mandatory_check', 'ordinary_tests', 'production_build'])
    assert.throws(() => requireQualification({ ...qualification, [field]: 'pending' }, inputs))
})

test('peer cleanup is bounded even when its close callback never arrives', async () => {
  const calls = []
  await closePeer(
    {
      closeAllConnections: () => calls.push('connections'),
      close: (done) => {
        calls.push('server')
        done()
      },
    },
    50,
  )
  assert.deepEqual(calls, ['connections', 'server'])
  await assert.rejects(
    closePeer({ closeAllConnections() {}, close() {} }, 5),
    /Peer cleanup timed out/,
  )
  await assert.rejects(
    closePeer({ closeAllConnections() {}, close: (done) => done(new Error('failed')) }, 50),
    /failed/,
  )
  await assert.rejects(closePeer({}, 0), /deadline expired/)
})

function message(operational = true) {
  return {
    From: { Address: sender },
    To: [{ Address: recipient }],
    ReplyTo: [{ Address: replyTo }],
    ReturnPath: sender,
    MessageID: 'routex-intent@example.invalid'.replace('@example.invalid', '@routex.invalid'),
    Subject: operational ? 'RouteX operational alert: high' : 'RouteX SMTP test',
    Text: operational
      ? `Kind: ${alertKind}\r\nDetail: verification_failed\r\nSeverity: high\r\n`
      : 'This is a RouteX SMTP configuration test.\nAcceptance by the configured relay does not confirm inbox delivery.',
    HTML: '',
    Attachments: [],
  }
}
const envelope = () => ({
  Received: [
    `from localhost by controlled with SMTP for <${recipient}>; Wed, 01 Jan 2026 00:00:00 +0000`,
  ],
})
test('relay acceptance requires MAIL FROM, recorded first RCPT TO, header recipient and server body', () => {
  for (const operational of [true, false])
    requireMessage(message(operational), envelope(), {
      requestID: 'intent',
      operational,
    })
  for (const mutate of [
    (value) => {
      value.ReturnPath = 'other@example.invalid'
    },
    (value) => {
      value.MessageID = 'another@routex.invalid'
    },
    (value) => {
      value.Text = 'HTTP succeeded'
    },
    (value) => {
      value.To.push({ Address: 'other@example.invalid' })
    },
    (value) => {
      value.Subject = 'RouteX operational alert: medium'
    },
    (value) => {
      value.Attachments = [{}]
    },
  ]) {
    const value = message()
    mutate(value)
    assert.throws(() =>
      requireMessage(value, envelope(), {
        requestID: 'intent',
        operational: true,
      }),
    )
  }
  assert.throws(() =>
    requireMessage(
      message(),
      { Received: ['header does not record recipient'] },
      { requestID: 'intent', operational: true },
    ),
  )
})
test('capture census refuses duplicated submissions even with distinct relay object IDs', () => {
  assert.deepEqual(
    requireMailbox(
      {
        total: 2,
        messages: [
          { ID: 'one', MessageID: 'm1' },
          { ID: 'two', MessageID: 'm2' },
        ],
      },
      2,
    ),
    ['one', 'two'],
  )
  for (const page of [
    { total: 2, messages: [{ ID: 'one', MessageID: 'm1' }] },
    {
      total: 2,
      messages: [
        { ID: 'one', MessageID: 'm1' },
        { ID: 'two', MessageID: 'm1' },
      ],
    },
    {
      total: 2,
      messages: [
        { ID: 'one', MessageID: 'm1' },
        { ID: 'one', MessageID: 'm2' },
      ],
    },
  ])
    assert.throws(() => requireMailbox(page, 2))
})
const notice = () => ({
  id: 'notice',
  alert_id: 'alert',
  kind: alertKind,
  severity: 'high',
  detail_code: 'verification_failed',
  occurrence_count: 1,
  read: true,
  delivery_status: 'accepted',
  delivery_code: 'accepted',
  delivery_attempts: 1,
})
test('read mutation state cannot substitute for enriched delivery evidence', () => {
  const expected = { id: 'notice', alertID: 'alert', read: true }
  const state = notice()
  delete state.delivery_status
  delete state.delivery_code
  delete state.delivery_attempts
  requireNoticeState(state, expected)
  assert.throws(() => requireNotice(state, expected), /SMTP acceptance/)
  for (const [field, value] of [
    ['id', 'other'],
    ['alert_id', 'other'],
    ['read', false],
    ['kind', 'credential_inference_failure'],
    ['severity', 'medium'],
    ['detail_code', 'other'],
    ['occurrence_count', 2],
  ])
    assert.throws(() => requireNoticeState({ ...state, [field]: value }, expected))
})
test('inbox receipt requires accepted single attempt, exact identity and retained read state', () => {
  const expected = { id: 'notice', alertID: 'alert', read: true }
  requireNotice(notice(), expected)
  for (const [field, value] of [
    ['kind', 'credential_inference_failure'],
    ['delivery_status', 'unknown'],
    ['delivery_attempts', 2],
    ['occurrence_count', 2],
    ['read', false],
    ['id', 'other'],
  ])
    assert.throws(() => requireNotice({ ...notice(), [field]: value }, expected))
})
function snapshot() {
  return {
    audits: [
      {
        ID: 'audit',
        ActorID: 'actor',
        ResourceID: 'credential',
        Action: 'credential.verify.failed',
        ResourceType: 'credential',
      },
    ],
    occurrences: [
      {
        ID: 'occurrence',
        AlertID: 'alert',
        SourceID: 'audit',
        SourceType: 'credential_verification',
      },
    ],
    notices: [
      {
        ID: 'notice',
        AlertID: 'alert',
        RecipientID: 'actor',
        LatestOccurrenceID: 'occurrence',
        Read: true,
      },
    ],
    deliveries: [
      {
        ID: 'delivery',
        Kind: alertKind,
        Severity: 'high',
        DetailCode: 'verification_failed',
        OccurrenceID: 'occurrence',
        RecipientID: 'actor',
        RecipientEmail: recipient,
        SMTPETag: 'revision',
        Status: 'accepted',
        Attempts: 1,
        ResultCode: 'accepted',
        CompletedAt: '2026-10-13T00:00:00Z',
      },
    ],
    tests: [
      {
        RequestID: 'test',
        Status: 'accepted',
        ActorID: 'actor',
        ConfigETag: 'revision',
      },
    ],
    settings: [
      {
        UserID: 'actor',
        EmailHigh: true,
        EmailMedium: false,
        ExternalEmail: recipient,
      },
    ],
    native_counts: { keys: 0, calls: 0, attempts: 0 },
  }
}
test('GORM receipt binds source birth, recipient opt-in and retained intent rather than SMTP alone', () => {
  const expected = {
    actor: 'actor',
    credentialID: 'credential',
    smtpETag: 'revision',
    testID: 'test',
    noticeID: 'notice',
  }
  assert.equal(requireSnapshot(snapshot(), expected), 'delivery')
  for (const mutate of [
    (value) => {
      value.audits[0].Action = 'credential.inference.failed'
    },
    (value) => {
      value.notices[0].AlertID = 'other'
    },
    (value) => {
      value.tests[0].ConfigETag = 'other'
    },
    (value) => {
      value.occurrences[0].SourceID = 'other'
    },
    (value) => {
      value.notices[0].RecipientID = 'other'
    },
    (value) => {
      value.deliveries[0].RecipientEmail = 'other@example.invalid'
    },
    (value) => {
      value.deliveries[0].SMTPETag = 'other'
    },
    (value) => {
      value.deliveries[0].Attempts = 2
    },
    (value) => {
      value.deliveries[0].CompletedAt = null
    },
    (value) => {
      value.settings[0].EmailHigh = false
    },
    (value) => {
      value.settings[0].EmailMedium = true
    },
    (value) => {
      value.native_counts.calls = 1
    },
    (value) => {
      value.audits.push(value.audits[0])
    },
  ]) {
    const value = snapshot()
    mutate(value)
    assert.throws(() => requireSnapshot(value, expected))
  }
})
