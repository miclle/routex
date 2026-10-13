import assert from 'node:assert/strict'
import { createHash, randomBytes } from 'node:crypto'
import { once } from 'node:events'
import { mkdir, mkdtemp, readFile, realpath, writeFile } from 'node:fs/promises'
import { createServer as httpServer } from 'node:http'
import { createConnection, createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { dirname, isAbsolute, join, resolve } from 'node:path'
import { performance } from 'node:perf_hooks'
import { setTimeout as delay } from 'node:timers/promises'
import { fileURLToPath } from 'node:url'
import { boundedBody, requireNoSecrets } from './backup-restore-support.mjs'
import {
  absent,
  createSupervisor,
  executableReceipt,
  sourceEntry,
} from './storage-native-acceptance-support.mjs'
import {
  alertKind,
  recipient,
  replyTo,
  sender,
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

process.umask(0o077)
assert.ok(
  process.argv.slice(2).every((arg) => arg === '--capture-inputs'),
  'Unknown acceptance argument',
)
const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const binary = await executableReceipt(process.env.ROUTEX_SMTP_BINARY)
const helper = await executableReceipt(process.env.ROUTEX_SMTP_HELPER)
const output = await mkdtemp(join(await realpath(tmpdir()), 'routex-smtp-acceptance-'))
await mkdir(join(output, 'logs'), { mode: 0o700 })
const project = `routex-smtp-acceptance-${Date.now()}-${process.pid}`
const composeFile = join(output, 'compose.yaml')
const compose = ['compose', '-p', project, '-f', composeFile]
const hash = (bytes) => createHash('sha256').update(bytes).digest('hex')
const workDeadline = performance.now() + 600_000
const absoluteDeadline = workDeadline + 90_000
const abort = new AbortController()
let cleanupDeadline,
  interrupted = false,
  stage = 'preflight',
  failureStage
let application,
  peer,
  composeStarted = false,
  sequence = 0,
  peerFailure = false
let verificationRequests = 0,
  sourceBefore,
  assetsBefore,
  inputsBefore
const children = [],
  ports = new Set(),
  results = []
const fixtureEnv = Object.fromEntries(
  Object.entries(process.env).filter(([name]) => !name.startsWith('ROUTEX_')),
)
for (const signal of ['SIGINT', 'SIGTERM'])
  process.once(signal, () => {
    interrupted = true
    abort.abort()
  })
function remaining(deadline = cleanupDeadline ?? workDeadline) {
  assert.ok(
    (!interrupted || cleanupDeadline !== undefined) && performance.now() < deadline,
    'Deadline or cancellation',
  )
  return Math.max(1, deadline - performance.now())
}
const { start, joinChild } = createSupervisor({
  repo,
  fixtureEnv,
  children,
  workDeadline,
  absoluteDeadline,
  remaining,
  interrupted: () => interrupted,
})
const save = (name, value) =>
  writeFile(join(output, name), `${JSON.stringify(value, null, 2)}\n`, {
    mode: 0o600,
    flag: 'wx',
  })
async function command(name, executable, args, options = {}) {
  stage = name
  const state = start(name, executable, args, {
    ...options,
    deadline: Math.min(
      cleanupDeadline ?? workDeadline,
      performance.now() + (options.timeout ?? 30_000),
    ),
  })
  const bytes = await joinChild(state)
  assert.equal(state.entry.exit_code, 0, 'Owned command failed')
  return bytes
}
const docker = (name, args, options) => command(name, 'docker', args, options)
async function port() {
  const server = createServer().listen(0, '127.0.0.1')
  await once(server, 'listening')
  const value = server.address().port
  await new Promise((done, reject) => server.close((error) => (error ? reject(error) : done())))
  assert.ok(!ports.has(value), 'Fixture port duplicated')
  ports.add(value)
  return value
}
async function sourceDigest() {
  const paths = (
    await command('source-list', 'git', [
      'ls-files',
      '-z',
      '--cached',
      '--others',
      '--exclude-standard',
    ])
  )
    .toString()
    .split('\0')
    .filter(Boolean)
    .sort()
  const rows = []
  for (const relative of paths) rows.push(await sourceEntry(join(repo, relative), relative))
  return { digest: hash(Buffer.from(JSON.stringify(rows))), rows }
}
async function request(
  origin,
  path,
  expected = 200,
  { method = 'GET', cookie, csrf, body, raw = false } = {},
) {
  stage = `${method} ${path}`
  const headers = { Origin: origin, 'Sec-Fetch-Site': 'same-origin' }
  if (cookie) headers.Cookie = cookie
  if (csrf) headers['X-CSRF-Token'] = csrf
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const response = await fetch(`${origin}${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    redirect: 'error',
    signal: AbortSignal.any([abort.signal, AbortSignal.timeout(Math.min(10_000, remaining()))]),
  })
  assert.equal(response.status, expected, 'Unexpected HTTP status')
  const bytes = await boundedBody(response)
  return { value: raw ? bytes : JSON.parse(bytes), response }
}
async function until(name, action, predicate, milliseconds = 15_000) {
  stage = name
  const deadline = Math.min(workDeadline, performance.now() + milliseconds)
  while (performance.now() < deadline) {
    remaining()
    const value = await action()
    if (predicate(value)) return value
    await delay(100)
  }
  throw new Error('Bounded observation failed')
}
async function stopApplication() {
  if (!application) return
  const state = application
  await joinChild(
    state,
    Math.min(cleanupDeadline ?? workDeadline, performance.now() + 30_000),
    true,
  )
  application = undefined
  assert.equal(state.entry.exit_code, 0, 'Application did not close normally')
}
async function launch(driver, generation) {
  assert.equal(
    (await executableReceipt(binary.path)).sha256,
    binary.sha256,
    'Production binary changed',
  )
  const appPort = await port()
  const origin = `http://127.0.0.1:${appPort}`
  const config = join(output, `${driver}-${generation}.yaml`)
  await writeFile(
    config,
    `addr: "127.0.0.1:${appPort}"\ndriver: ${driver}\ndsn: "\${ROUTEX_SMTP_DSN}"\nencryption_key: "\${ROUTEX_SMTP_ROOT}"\nallow_private_smtp: true\nallow_private_upstreams: true\nevent_queue_path: ${JSON.stringify(join(output, `${driver}-journal.db`))}\n`,
    { mode: 0o600, flag: 'wx' },
  )
  application = start(`${driver}-application-${generation}`, binary.path, ['-c', config])
  const state = application
  assert.ok(
    state.entry.birth && state.entry.pgid === state.entry.pid,
    'Long-lived application process ownership unobserved',
  )
  const deadline = Math.min(workDeadline, performance.now() + 60_000)
  while (performance.now() < deadline) {
    assert.ok(!state.entry.joined, 'Application exited before readiness')
    try {
      const health = await request(origin, '/health', 200, { raw: true })
      if (health.value.toString() === 'ok') {
        const assets = await request(origin, '/', 200, { raw: true })
        assert.ok(assets.value.toString().includes('<html'), 'Production assets missing')
        return origin
      }
    } catch {
      /* Only readiness reads retry; mutations never replay automatically. */
    }
    await delay(100)
  }
  throw new Error('Application readiness failed')
}
async function facts(driver, label) {
  const file = join(output, `${driver}-${label}-facts.json`)
  await command(`facts-${driver}-${label}`, helper.path, ['-driver', driver, '-receipt', file], {
    timeout: 15_000,
  })
  return JSON.parse(await readFile(file))
}
let relayOrigin
const mailbox = async () => (await request(relayOrigin, '/api/v1/messages?limit=16')).value
async function capturedMessage(id, intent) {
  assert.ok(/^[a-zA-Z0-9_-]{1,100}$/.test(id), 'Invalid relay identity')
  const message = (await request(relayOrigin, `/api/v1/message/${id}`)).value
  const headers = (await request(relayOrigin, `/api/v1/message/${id}/headers`)).value
  requireMessage(message, headers, intent)
  const raw = (await request(relayOrigin, `/api/v1/message/${id}/raw`, 200, { raw: true })).value
  requireNoSecrets(raw, [
    fixtureEnv.ROUTEX_SMTP_ROOT,
    fixtureEnv.ROUTEX_SMTP_DB_PASSWORD,
    fixtureEnv.ROUTEX_SMTP_CREDENTIAL,
    fixtureEnv.ROUTEX_SMTP_PASSWORD,
  ])
  await save(`mail-${++sequence}.json`, {
    message,
    headers,
    raw_sha256: hash(raw),
  })
  await writeFile(join(output, `mail-${sequence}.eml`), raw, {
    mode: 0o600,
    flag: 'wx',
  })
}
async function exercise(driver, baseline) {
  const dbPort =
    fixtureEnv[driver === 'postgres' ? 'ROUTEX_SMTP_POSTGRES_PORT' : 'ROUTEX_SMTP_MYSQL_PORT']
  fixtureEnv.ROUTEX_SMTP_DSN =
    driver === 'postgres'
      ? `postgres://routex_smtp_acceptance:${fixtureEnv.ROUTEX_SMTP_DB_PASSWORD}@127.0.0.1:${dbPort}/routex_smtp_acceptance?sslmode=disable`
      : `routex_smtp_acceptance:${fixtureEnv.ROUTEX_SMTP_DB_PASSWORD}@tcp(127.0.0.1:${dbPort})/routex_smtp_acceptance?charset=utf8mb4&parseTime=True&loc=UTC`
  let origin = await launch(driver, 'first')
  const setup = await request(origin, '/api/v1/setup', 201, {
    method: 'POST',
    body: {
      email: 'smtp-admin@example.invalid',
      password: fixtureEnv.ROUTEX_SMTP_PASSWORD,
      name: 'SMTP administrator',
    },
  })
  const cookie = setup.response.headers
    .getSetCookie()
    .find((value) => value.startsWith('routex_session='))
    ?.split(';')[0]
  assert.ok(cookie, 'Original Session missing')
  const session = (await request(origin, '/api/v1/auth/session', 200, { cookie })).value
  const actor = session.user.id
  let csrf = session.csrf_token
  const get = async (path) => (await request(origin, `/api/v1${path}`, 200, { cookie })).value
  const write = async (path, body, method = 'POST', expected = 200) =>
    (
      await request(origin, `/api/v1${path}`, expected, {
        cookie,
        csrf,
        method,
        body,
      })
    ).value
  const defaults = await get('/notification-settings')
  assert.equal(defaults.in_app_enabled, true, 'In-app notification default changed')
  assert.equal(defaults.email_high, false, 'High email must be opt-in')
  assert.equal(defaults.email_medium, false, 'Medium email must be opt-in')
  const initialSMTP = await get('/admin/smtp')
  const transport = await write(
    '/admin/smtp',
    {
      enabled: true,
      host: '127.0.0.1',
      port: Number(fixtureEnv.ROUTEX_SMTP_PORT),
      security: 'NONE',
      auth: { action: 'remove' },
      etag: initialSMTP.etag,
    },
    'PUT',
  )
  assert.equal(transport.auth_configured, false, 'Plaintext fixture cannot authenticate')
  const configured = await write(
    '/admin/smtp/sender',
    {
      sender_name: 'RouteX acceptance',
      sender_email: sender,
      reply_to: replyTo,
      etag: transport.etag,
    },
    'PUT',
  )
  const testID = `smtp_local_${driver}_${randomBytes(8).toString('hex')}`
  const testInput = { recipient, request_id: testID, etag: configured.etag }
  const testReceipt = await write('/admin/smtp/test', testInput)
  assert.equal(testReceipt.status, 'accepted', 'Real SMTP test not accepted')
  const testPage = await until('test-relay-receipt', mailbox, (page) => page.total === baseline + 1)
  requireMailbox(testPage, baseline + 1)
  const testMail = testPage.messages.find(
    (item) => item.MessageID === `routex-${testID}@routex.invalid`,
  )
  assert.ok(testMail, 'SMTP test receipt identity missing')
  await capturedMessage(testMail.ID, { requestID: testID, operational: false })
  assert.deepEqual(
    await write('/admin/smtp/test', testInput),
    testReceipt,
    'Identical SMTP test retry changed receipt',
  )
  requireMailbox(await mailbox(), baseline + 1)
  const preferences = await write(
    '/notification-settings',
    {
      external_email: recipient,
      email_high: true,
      email_medium: false,
      etag: defaults.etag,
    },
    'PUT',
  )
  assert.equal(preferences.email_high, true, 'High preference not saved')
  assert.equal(preferences.email_medium, false, 'Medium preference changed')
  const provider = await write(
    '/admin/providers',
    {
      name: 'SMTP operational fixture',
      connection_name: 'Manual discovery check',
      base_url: `http://127.0.0.1:${peer.address().port}/v1`,
      protocol: 'openai_chat',
      credential_name: 'Disposable credential',
      secret: fixtureEnv.ROUTEX_SMTP_CREDENTIAL,
    },
    'POST',
    201,
  )
  const credentialID = provider.connections[0].credentials[0].id
  const requestsBefore = verificationRequests
  const verification = await write(`/admin/credentials/${credentialID}/verify`, {})
  assert.equal(verification.verified, false, 'Controlled manual verification did not fail')
  assert.equal(verificationRequests, requestsBefore + 1, 'Manual verification retried upstream')
  assert.equal(peerFailure, false, 'Unexpected peer traffic')
  const page = await until(
    'operational-acceptance',
    () => get('/notifications?status=all&limit=100'),
    (value) =>
      value.items.some((item) => item.kind === alertKind && item.delivery_status === 'accepted'),
  )
  assert.equal(page.items.length, 1, 'Unexpected private inbox occurrence')
  const notice = page.items[0]
  const noticeIdentity = { id: notice.id, alertID: notice.alert_id }
  requireNotice(notice, { ...noticeIdentity, read: false })
  const readNotice = await write(`/notifications/${notice.id}/read`, undefined)
  requireNoticeState(readNotice, { ...noticeIdentity, read: true })
  await save(`${driver}-read-mutation.json`, readNotice)
  const readPage = await get('/notifications?status=all&limit=100')
  assert.equal(readPage.items.length, 1, 'Unexpected private inbox occurrence after read')
  requireNotice(
    readPage.items.find((item) => item.id === notice.id),
    {
      ...noticeIdentity,
      read: true,
    },
  )
  await save(`${driver}-read-list.json`, readPage)
  await stopApplication()
  const before = await facts(driver, 'before')
  const intentID = requireSnapshot(before, {
    actor,
    credentialID,
    smtpETag: configured.etag,
    testID,
    noticeID: notice.id,
  })
  const captured = await until(
    'operational-relay-receipt',
    mailbox,
    (value) => value.total === baseline + 2,
  )
  const mailboxIDs = requireMailbox(captured, baseline + 2)
  const operationalMail = captured.messages.find(
    (item) => item.MessageID === `routex-${intentID}@routex.invalid`,
  )
  assert.ok(operationalMail, 'Exact operational intent not captured')
  await capturedMessage(operationalMail.ID, {
    requestID: intentID,
    operational: true,
  })
  origin = await launch(driver, 'restart')
  const resumed = await get('/auth/session')
  assert.equal(resumed.user.id, actor, 'Original Session did not survive restart')
  csrf = resumed.csrf_token
  assert.deepEqual(
    await write('/admin/smtp/test', testInput),
    testReceipt,
    'Restart changed SMTP test receipt',
  )
  // Observe twelve elapsed seconds without claiming actual worker-poll counts.
  // Only reads follow the identical intent; no second operational source exists.
  const observeUntil = performance.now() + 12_000
  while (performance.now() < observeUntil) {
    remaining()
    const renewed = await get('/notifications?status=all&limit=100')
    assert.equal(renewed.items.length, 1, 'Restart duplicated private inbox')
    requireNotice(renewed.items[0], { ...noticeIdentity, read: true })
    assert.deepEqual(
      requireMailbox(await mailbox(), baseline + 2),
      mailboxIDs,
      'Restart changed relay receipt census',
    )
    await delay(250)
  }
  await stopApplication()
  const after = await facts(driver, 'after')
  requireSnapshot(after, {
    actor,
    credentialID,
    smtpETag: configured.etag,
    testID,
    noticeID: notice.id,
  })
  assert.deepEqual(after, before, 'Restart rewrote immutable source, recipient or delivery facts')
  assert.equal(verificationRequests, requestsBefore + 1, 'Restart repeated manual verification')
  results.push({
    driver,
    actor,
    credential_id: credentialID,
    source_id: before.audits[0].ID,
    occurrence_id: before.occurrences[0].ID,
    notification_id: notice.id,
    intent_id: intentID,
    test_request_id: testID,
    first_process: 'joined',
    restarted_original_session: true,
    immutable_facts_sha256: hash(Buffer.from(JSON.stringify(before))),
    captured_message_ids: [testMail.ID, operationalMail.ID],
    captured_total: baseline + 2,
    observation_milliseconds: 12_000,
  })
}
async function closedPort(value) {
  const deadline = Math.min(cleanupDeadline, performance.now() + 5000)
  while (performance.now() < deadline) {
    const refused = await new Promise((done) => {
      const socket = createConnection({ host: '127.0.0.1', port: value })
      socket.setTimeout(500)
      socket.once('connect', () => {
        socket.destroy()
        done(false)
      })
      socket.once('timeout', () => {
        socket.destroy()
        done(false)
      })
      socket.once('error', (error) => {
        socket.destroy()
        done(error.code === 'ECONNREFUSED')
      })
    })
    if (refused) {
      const server = createServer()
      try {
        server.listen(value, '127.0.0.1')
        await once(server, 'listening')
        await new Promise((done, reject) =>
          server.close((error) => (error ? reject(error) : done())),
        )
        return
      } catch {
        if (server.listening) server.close()
      }
    }
    await delay(100)
  }
  throw new Error('Owned listener did not close')
}
const errors = [],
  cleanupErrors = []
try {
  sourceBefore = await sourceDigest()
  assetsBefore = await assetsDigest(repo)
  inputsBefore = {
    source_digest: sourceBefore.digest,
    binary_sha256: binary.sha256,
    helper_sha256: helper.sha256,
    assets_digest: assetsBefore.digest,
  }
  await save('source-before.json', sourceBefore)
  await save('assets-before.json', assetsBefore)
  await save('inputs.json', inputsBefore)
  if (!process.argv.includes('--capture-inputs')) {
    assert.ok(
      isAbsolute(process.env.ROUTEX_SMTP_QUALIFICATION ?? ''),
      'Exact-source qualification receipt required',
    )
    const qualification = JSON.parse(await readFile(process.env.ROUTEX_SMTP_QUALIFICATION))
    requireQualification(qualification, inputsBefore)
    await save('qualification.json', qualification)
    Object.assign(fixtureEnv, {
      ROUTEX_SMTP_DB_PASSWORD: randomBytes(24).toString('hex'),
      ROUTEX_SMTP_ROOT: randomBytes(32).toString('base64'),
      ROUTEX_SMTP_CREDENTIAL: randomBytes(24).toString('hex'),
      ROUTEX_SMTP_PASSWORD: randomBytes(24).toString('hex'),
    })
    requireRootKey(fixtureEnv.ROUTEX_SMTP_ROOT)
    for (const name of [
      'ROUTEX_SMTP_PORT',
      'ROUTEX_SMTP_HTTP_PORT',
      'ROUTEX_SMTP_POSTGRES_PORT',
      'ROUTEX_SMTP_MYSQL_PORT',
    ])
      fixtureEnv[name] = String(await port())
    relayOrigin = `http://127.0.0.1:${fixtureEnv.ROUTEX_SMTP_HTTP_PORT}`
    await writeFile(
      composeFile,
      await readFile(join(repo, 'scripts/smtp-acceptance.compose.yaml')),
      { mode: 0o600, flag: 'wx' },
    )
    for (const resource of ['container', 'network', 'volume'])
      assert.equal(
        (
          await docker(`preflight-${resource}`, [
            resource,
            'ls',
            '-q',
            '--filter',
            `label=com.docker.compose.project=${project}`,
          ])
        )
          .toString()
          .trim(),
        '',
        'Fresh Compose identity already exists',
      )
    composeStarted = true
    await docker(
      'compose-up',
      [...compose, 'up', '--pull', 'never', '--detach', '--wait', '--wait-timeout', '180'],
      { timeout: 190_000 },
    )
    const containerIDs = (await docker('container-ids', [...compose, 'ps', '-q']))
      .toString()
      .trim()
      .split('\n')
    assert.equal(containerIDs.length, 3, 'Owned fixture census changed')
    const inspected = JSON.parse(
      (await docker('inspect-fixture', ['inspect', ...containerIDs])).toString(),
    )
    const relay = inspected.find(
      (value) => value.Config?.Labels?.['com.docker.compose.service'] === 'mailpit',
    )
    assert.ok(
      relay?.Config?.Cmd?.includes('--ignore-duplicate-ids=false'),
      'Capture relay can hide duplicate submissions',
    )
    assert.ok(
      relay.Config.Image ===
        'axllent/mailpit:v1.31.5@sha256:94f6b70c1436defae6799d9ac7ff0eeff9540465b842026d5cdb018f1c0234ea',
      'Relay image pin changed',
    )
    await save('compose-ownership.json', {
      project,
      compose_sha256: hash(await readFile(composeFile)),
      containers: inspected,
    })
    requireMailbox(await mailbox(), 0)
    peer = httpServer((req, res) => {
      if (
        req.method !== 'GET' ||
        req.url !== '/v1/models' ||
        req.headers.authorization !== `Bearer ${fixtureEnv.ROUTEX_SMTP_CREDENTIAL}`
      )
        peerFailure = true
      verificationRequests++
      res.writeHead(503, { 'Content-Type': 'application/json' })
      res.end('{"error":"controlled manual check failure"}')
    }).listen(0, '127.0.0.1')
    await once(peer, 'listening')
    ports.add(peer.address().port)
    await exercise('postgres', 0)
    await exercise('mysql', 2)
    assert.equal(peerFailure, false, 'Unexpected controlled peer request')
    assert.equal(verificationRequests, 2, 'Unexpected manual-check census')
  }
} catch {
  failureStage = stage
  errors.push(interrupted ? 'interrupted' : 'functional_acceptance_failed')
} finally {
  cleanupDeadline = Math.min(absoluteDeadline, performance.now() + 90_000)
  try {
    await stopApplication()
  } catch {
    cleanupErrors.push('application_cleanup_failed')
  }
  if (peer) {
    try {
      await closePeer(peer, Math.min(5000, remaining(cleanupDeadline)))
    } catch {
      cleanupErrors.push('peer_cleanup_failed')
    }
  }
  for (const child of children.filter((state) => !state.entry.group_gone)) {
    try {
      await joinChild(child, Math.min(cleanupDeadline, performance.now() + 10_000), true)
    } catch {
      cleanupErrors.push('child_cleanup_failed')
    }
  }
  if (composeStarted && children.every((state) => state.entry.group_gone)) {
    try {
      await docker(
        'compose-down',
        [...compose, 'down', '--volumes', '--remove-orphans', '--timeout', '10'],
        { timeout: 45_000 },
      )
    } catch {
      cleanupErrors.push('compose_down_failed')
    }
  }
  if (composeStarted) {
    try {
      for (const resource of ['container', 'network', 'volume'])
        assert.equal(
          (
            await docker(`closed-${resource}`, [
              resource,
              'ls',
              '-q',
              '--filter',
              `label=com.docker.compose.project=${project}`,
            ])
          )
            .toString()
            .trim(),
          '',
          'Owned resource remains',
        )
      for (const value of ports) await closedPort(value)
    } catch {
      cleanupErrors.push('resource_or_listener_remains')
    }
  }
  try {
    if (inputsBefore) {
      const after = await sourceDigest()
      const assetsAfter = await assetsDigest(repo)
      await save('source-after.json', after)
      await save('assets-after.json', assetsAfter)
      requireInputs(inputsBefore, {
        source_digest: after.digest,
        binary_sha256: (await executableReceipt(binary.path)).sha256,
        helper_sha256: (await executableReceipt(helper.path)).sha256,
        assets_digest: assetsAfter.digest,
      })
    }
  } catch {
    cleanupErrors.push('input_changed')
  }
  try {
    for (const child of children) {
      assert.ok(
        child.entry.joined &&
          child.entry.group_gone &&
          absent(child.entry.pid) &&
          absent(child.entry.pid, true),
        'Owned process remains',
      )
      await writeFile(
        join(output, 'logs', `${children.indexOf(child)}.stdout`),
        Buffer.concat(child.output),
        { mode: 0o600, flag: 'wx' },
      )
      await writeFile(
        join(output, 'logs', `${children.indexOf(child)}.stderr`),
        Buffer.concat(child.stderr),
        { mode: 0o600, flag: 'wx' },
      )
    }
  } catch {
    cleanupErrors.push('process_or_log_cleanup_failed')
  }
  const capture = process.argv.includes('--capture-inputs')
  await save('result.json', {
    status:
      errors.length || cleanupErrors.length
        ? 'failed'
        : capture
          ? 'captured-unqualified'
          : 'passed',
    functional_passed: !capture && errors.length === 0 && results.length === 2,
    cleanup_passed: cleanupErrors.length === 0,
    failure_stage: failureStage,
    errors,
    cleanup_errors: cleanupErrors,
    results,
    verification_requests: verificationRequests,
    process_receipts: children.map((state) => state.entry),
    ports: [...ports],
    project,
    binary_sha256: binary.sha256,
    helper_sha256: helper.sha256,
    assets_digest: assetsBefore?.digest,
  })
  process.exitCode = errors.length || cleanupErrors.length ? 1 : 0
  console.log(
    `${capture ? 'Unqualified SMTP input capture' : 'Local SMTP acceptance'} ${process.exitCode ? 'failed' : 'complete'}; private artifacts: ${output}`,
  )
}
