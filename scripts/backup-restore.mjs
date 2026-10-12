import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { createHash, randomBytes } from 'node:crypto'
import { once } from 'node:events'
import { constants } from 'node:fs'
import { copyFile, lstat, mkdir, mkdtemp, readFile, realpath, writeFile } from 'node:fs/promises'
import { createServer as httpServer } from 'node:http'
import { createConnection, createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { dirname, isAbsolute, join, resolve } from 'node:path'
import { performance } from 'node:perf_hooks'
import { fileURLToPath } from 'node:url'
import { setTimeout as delay } from 'node:timers/promises'
import {
  applicationStdoutLimit,
  boundedBody,
  captureChildOutput,
  cleanupMilliseconds,
  dumpLimit,
  invalidateChild,
  requestPredicate,
  requireNativeAttempts,
  requireNoSecrets,
  requireQuota,
  requireQuotaExceeded,
  requireSameFacts,
  responseLimit,
  workMilliseconds,
} from './backup-restore-support.mjs'

process.umask(0o077)
const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const binaryInput = process.env.ROUTEX_BACKUP_BINARY
assert.ok(
  binaryInput && isAbsolute(binaryInput),
  'An absolute qualified production binary is required',
)
assert.ok(
  (await lstat(binaryInput)).isFile() && !(await lstat(binaryInput)).isSymbolicLink(),
  'Binary must be a regular file',
)
const binary = await realpath(binaryInput)
assert.ok((await lstat(binary)).mode & 0o111, 'Binary is not executable')
assert.ok((await lstat(binary)).size <= 256 << 20, 'Production binary exceeds the bound')
const binaryHash = createHash('sha256')
  .update(await readFile(binary))
  .digest('hex')
const outputInput = process.env.ROUTEX_BACKUP_OUTPUT
let output
if (outputInput) {
  assert.ok(
    isAbsolute(outputInput) && resolve(outputInput) === outputInput,
    'Output must be canonical and absolute',
  )
  const parent = dirname(outputInput)
  const parentStat = await lstat(parent)
  assert.ok(
    parentStat.isDirectory() &&
      !parentStat.isSymbolicLink() &&
      (await realpath(parent)) === parent &&
      parentStat.uid === process.getuid() &&
      (parentStat.mode & 0o077) === 0,
    'Output parent must be canonical, private and owned',
  )
  await mkdir(outputInput, { mode: 0o700 })
  output = outputInput
} else {
  output = await mkdtemp(join(await realpath(tmpdir()), 'routex-backup-restore-'))
}
const outputStat = await lstat(output)
assert.ok(
  outputStat.isDirectory() &&
    !outputStat.isSymbolicLink() &&
    outputStat.uid === process.getuid() &&
    (outputStat.mode & 0o777) === 0o700 &&
    (await realpath(output)) === output,
  'Output must be a fresh private directory',
)
const project = `routex-test-backup-${Date.now()}-${process.pid}`
const composeFile = join(output, 'compose.yaml')
const compose = ['compose', '-p', project, '-f', composeFile]
const workDeadline = performance.now() + workMilliseconds
const absoluteDeadline = workDeadline + cleanupMilliseconds
const abort = new AbortController()
const children = []
const ports = new Set()
const results = []
let application
let peer
let interrupted = false
let composeStarted = false
let cleanupDeadline
let sequence = 0
let stage = 'preflight'
let failureStage = null
for (const signal of ['SIGINT', 'SIGTERM'])
  process.once(signal, () => {
    interrupted = true
    abort.abort()
  })

function remaining(deadline = workDeadline) {
  assert.ok(
    (!interrupted || cleanupDeadline !== undefined) && performance.now() < deadline,
    'Drill deadline or cancellation',
  )
  return Math.max(1, deadline - performance.now())
}
function absent(pid, group = false) {
  if (!Number.isInteger(pid) || pid <= 1 || pid === process.pid) return false
  try {
    process.kill(group ? -pid : pid, 0)
    return false
  } catch (error) {
    if (error.code === 'ESRCH') return true
    throw new Error('Cannot establish process absence')
  }
}
async function save(name, value) {
  await writeFile(join(output, name), `${JSON.stringify(value, null, 2)}\n`, {
    mode: 0o600,
    flag: 'wx',
  })
}
function start(
  name,
  executable,
  args,
  { env = process.env, input, cap = responseLimit, deadline = workDeadline } = {},
) {
  remaining(deadline)
  const child = spawn(executable, args, {
    cwd: repo,
    env,
    detached: true,
    stdio: ['pipe', 'pipe', 'pipe'],
  })
  const entry = {
    name,
    pid: child.pid,
    pgid: child.pid,
    joined: false,
    group_gone: false,
    exit_code: null,
    stage,
    stdout_bytes: 0,
    stderr_bytes: 0,
    stdout_limit: cap,
    stderr_limit: 64 << 10,
    failure_codes: [],
  }
  const state = {
    child,
    entry,
    output: [],
    stderr: [],
    bytes: 0,
    stderrBytes: 0,
    invalid: false,
    deadline,
  }
  children.push(state)
  child.stdout.on('data', (chunk) => {
    if (!captureChildOutput(state, 'stdout', chunk)) signalOwned(state, 'SIGTERM')
  })
  child.stderr.on('data', (chunk) => {
    if (!captureChildOutput(state, 'stderr', chunk)) signalOwned(state, 'SIGTERM')
  })
  state.closed = new Promise((resolveClosed) => {
    child.once('error', () => {
      invalidateChild(state, 'child_error')
    })
    child.once('close', (code, signal) => {
      entry.exit_code = code
      entry.signal = signal
      entry.joined = true
      resolveClosed()
    })
  })
  child.stdin.on('error', () => {
    invalidateChild(state, 'stdin_error')
  })
  child.stdin.end(input)
  assert.ok(
    Number.isInteger(child.pid) && child.pid > 1 && child.pid !== process.pid,
    'Owned spawn failed',
  )
  return state
}
function signalOwned(state, signal) {
  if (
    Number.isInteger(state.entry.pid) &&
    state.entry.pid > 1 &&
    state.entry.pid !== process.pid &&
    !absent(state.entry.pid, true)
  ) {
    process.kill(-state.entry.pid, signal)
  }
}
async function joinChild(state, deadline = state.deadline, terminate = false) {
  const finalDeadline = Math.min(absoluteDeadline, deadline + 10_000)
  if (terminate) signalOwned(state, 'SIGTERM')
  let expired = false
  while (!state.entry.joined && performance.now() < deadline && (!interrupted || terminate))
    await delay(20)
  if (!state.entry.joined || !absent(state.entry.pgid, true)) {
    // A closed leader does not close descendants. Clean its exact owned group too.
    expired = !terminate || performance.now() >= deadline
    signalOwned(state, 'SIGTERM')
    const grace = Math.min(finalDeadline - 5000, performance.now() + 5000)
    while ((!state.entry.joined || !absent(state.entry.pgid, true)) && performance.now() < grace)
      await delay(20)
    if (!state.entry.joined || !absent(state.entry.pgid, true)) signalOwned(state, 'SIGKILL')
  }
  // close is the direct child join; group absence is an independent condition.
  let joinTimer
  try {
    await Promise.race([
      state.closed,
      new Promise((_, reject) => {
        joinTimer = setTimeout(
          () => reject(new Error('Owned direct join failed')),
          Math.max(1, Math.min(5000, finalDeadline - performance.now())),
        )
      }),
    ])
  } finally {
    clearTimeout(joinTimer)
  }
  const groupDeadline = Math.min(finalDeadline, performance.now() + 5000)
  while (!absent(state.entry.pid, true) && performance.now() < groupDeadline) await delay(20)
  state.entry.group_gone = absent(state.entry.pid, true) && absent(state.entry.pid)
  state.entry.timed_out ||= expired
  assert.ok(state.entry.group_gone, 'Owned process group remains')
  assert.ok(!state.invalid && !expired, 'Owned command failed or exceeded its bound')
  return Buffer.concat(state.output)
}
async function command(name, args, options = {}) {
  stage = name
  const deadline = Math.min(
    options.deadline ?? workDeadline,
    performance.now() + (options.timeout ?? 30_000),
  )
  const state = start(name, 'docker', args, { ...options, deadline })
  const bytes = await joinChild(state)
  assert.equal(state.entry.exit_code, 0, 'Owned database or Compose command failed')
  return bytes
}
async function sql(driver, database, statement) {
  assert.ok(/^routex_backup_[a-z]+$/.test(database), 'Unexpected database target')
  const args =
    driver === 'postgres'
      ? [
          'psql',
          '-X',
          '-A',
          '-t',
          '-F',
          '\t',
          '-v',
          'ON_ERROR_STOP=1',
          '-U',
          'routex',
          '-d',
          database,
          '-c',
          statement,
        ]
      : [
          'sh',
          '-c',
          'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --batch --raw --skip-column-names -u root "$@"',
          'mysql',
          database,
          '-e',
          statement,
        ]
  return command(`sql-${++sequence}`, [...compose, 'exec', '-T', driver, ...args])
}
async function createDatabase(driver, name) {
  const grants = driver === 'mysql' ? `; GRANT ALL ON ${name}.* TO 'routex'@'%'` : ''
  await sql(driver, 'routex_backup_admin', `CREATE DATABASE ${name}${grants}`)
}
async function unusedPort() {
  const socket = createServer()
  socket.listen(0, '127.0.0.1')
  await once(socket, 'listening')
  const { port } = socket.address()
  await new Promise((resolveClosed, reject) =>
    socket.close((error) => (error ? reject(error) : resolveClosed())),
  )
  ports.add(port)
  return port
}
async function rawRequest(
  origin,
  path,
  expected,
  { cookie, csrf, body, method = 'GET', etag, bearer } = {},
) {
  stage = `${method} ${path}`
  const headers = { Origin: origin, 'Sec-Fetch-Site': 'same-origin' }
  if (cookie) headers.Cookie = cookie
  if (csrf) headers['X-CSRF-Token'] = csrf
  if (etag) headers['If-Match'] = `"${etag}"`
  if (bearer) headers.Authorization = `Bearer ${bearer}`
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const response = await fetch(`${origin}${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    redirect: 'error',
    signal: AbortSignal.any([abort.signal, AbortSignal.timeout(Math.min(10_000, remaining()))]),
  })
  assert.equal(response.status, expected, 'Unexpected HTTP status')
  if (expected === 204) {
    await response.body?.cancel()
    return { response, value: null }
  }
  const bytes = await boundedBody(response)
  let value
  try {
    value = JSON.parse(bytes)
  } catch {
    throw new Error('Invalid JSON response')
  }
  return { response, value }
}
async function launch(driver, database, journal, dsn, root, negative = false) {
  stage = `${driver}-application-configuration`
  assert.equal(
    createHash('sha256')
      .update(await readFile(binary))
      .digest('hex'),
    binaryHash,
    'Production binary changed',
  )
  const port = await unusedPort()
  const origin = `http://127.0.0.1:${port}`
  const config = join(output, `${driver}-${database}-${++sequence}.yaml`)
  await writeFile(
    config,
    `addr: "127.0.0.1:${port}"\ndriver: ${driver}\ndsn: "\${ROUTEX_BACKUP_DSN}"\nencryption_key: "\${ROUTEX_BACKUP_ROOT}"\nallow_private_upstreams: true\nevent_queue_path: ${JSON.stringify(journal)}\n`,
    { mode: 0o600, flag: 'wx' },
  )
  const env = Object.fromEntries(
    Object.entries(process.env).filter(([name]) => !name.startsWith('ROUTEX_')),
  )
  Object.assign(env, { ROUTEX_BACKUP_DSN: dsn(database), ROUTEX_BACKUP_ROOT: root })
  stage = `${driver}-${negative ? 'negative-journal' : 'application-startup'}`
  const state = start(
    `${driver}-${negative ? 'negative' : 'application'}`,
    binary,
    ['-c', config],
    { env, cap: applicationStdoutLimit },
  )
  application = state
  if (negative) {
    await joinChild(state, Math.min(workDeadline, performance.now() + 30_000))
    application = undefined
    assert.equal(state.entry.exit_code, 1, 'Invalid journal did not reject startup')
    assert.ok(
      Buffer.concat(state.stderr).includes(Buffer.from('open durable call buffer failed')),
      'Startup failed outside the journal boundary',
    )
    return
  }
  stage = `${driver}-application-readiness`
  const deadline = Math.min(workDeadline, performance.now() + 60_000)
  while (performance.now() < deadline) {
    assert.ok(!state.entry.joined, 'Application exited before readiness')
    try {
      const response = await fetch(`${origin}/health`, {
        signal: AbortSignal.timeout(500),
        redirect: 'error',
      })
      if (response.status === 200 && (await boundedBody(response)).toString() === 'ok') {
        const page = await fetch(origin, { signal: AbortSignal.timeout(1000), redirect: 'error' })
        assert.equal(page.status, 200, 'Default production SPA is unavailable')
        assert.ok(
          (await boundedBody(page)).toString().includes('<html'),
          'Default production assets are missing',
        )
        return { origin, state }
      }
    } catch {
      /* Readiness alone may retry; no mutation is replayed. */
    }
    remaining()
    await delay(100)
  }
  throw new Error('Application readiness exceeded its bound')
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
async function captureFacts(driver, database, ids) {
  const where = requestPredicate(ids)
  return {
    calls: await sql(
      driver,
      database,
      `SELECT * FROM call_records WHERE ${where} ORDER BY request_id`,
    ),
    attempts: await sql(driver, database, `SELECT * FROM call_attempts WHERE ${where} ORDER BY id`),
  }
}
async function waitCall(api, requestID) {
  const deadline = Math.min(workDeadline, performance.now() + 10_000)
  while (performance.now() < deadline) {
    const response = await fetch(`${api.origin}/api/v1/calls/${requestID}`, {
      headers: { Cookie: api.cookie },
      signal: AbortSignal.timeout(1000),
      redirect: 'error',
    })
    if (response.status === 200) return JSON.parse(await boundedBody(response))
    assert.equal(response.status, 404, 'Call delivery read failed')
    await response.body?.cancel()
    await delay(100)
  }
  throw new Error('Durable call delivery exceeded its bound')
}
async function exercise(driver, address) {
  const sourceDB = 'routex_backup_source'
  const restoredDB = 'routex_backup_restored'
  const foreignDB = 'routex_backup_foreign'
  const missingDB = 'routex_backup_missing'
  const wrongDB = 'routex_backup_wrong'
  for (const name of [sourceDB, restoredDB, foreignDB, missingDB, wrongDB])
    await createDatabase(driver, name)
  const dsn = (database) =>
    driver === 'postgres'
      ? `host=127.0.0.1 port=${address.port} user=routex password=routex-test dbname=${database} sslmode=disable`
      : `routex:routex-test@tcp(127.0.0.1:${address.port})/${database}?charset=utf8mb4&parseTime=True&loc=UTC`
  const root = randomBytes(32).toString('base64')
  const credential = randomBytes(32).toString('hex')
  const password = randomBytes(32).toString('base64url')
  let nativeCalls = 0
  peer = httpServer(async (req, res) => {
    try {
      assert.equal(req.headers.authorization, `Bearer ${credential}`)
      if (req.method === 'GET' && req.url === '/v1/models') {
        res.setHeader('Content-Type', 'application/json')
        res.end(JSON.stringify({ data: [{ id: 'backup-native' }] }))
        return
      }
      assert.equal(req.method, 'POST')
      assert.equal(req.url, '/v1/chat/completions')
      const chunks = []
      let size = 0
      for await (const chunk of req) {
        size += chunk.length
        assert.ok(size <= 4096)
        chunks.push(chunk)
      }
      const body = JSON.parse(Buffer.concat(chunks))
      assert.equal(body.model, 'backup-native')
      assert.equal(body.max_completion_tokens, 10)
      nativeCalls++
      res.setHeader('Content-Type', 'application/json')
      res.end(
        JSON.stringify({
          id: 'backup-answer',
          object: 'chat.completion',
          model: 'backup-native',
          choices: [
            {
              index: 0,
              message: { role: 'assistant', content: 'Retained' },
              finish_reason: 'stop',
            },
          ],
          usage: {
            prompt_tokens: 3,
            completion_tokens: 2,
            total_tokens: 5,
            prompt_tokens_details: { cached_tokens: 0, cache_write_tokens: 0 },
          },
        }),
      )
    } catch {
      res.writeHead(400)
      res.end()
    }
  })
  peer.listen(0, '127.0.0.1')
  await once(peer, 'listening')
  ports.add(peer.address().port)
  const sourceJournal = join(output, `${driver}-source.db`)
  const { origin } = await launch(driver, sourceDB, sourceJournal, dsn, root)
  const setup = await rawRequest(origin, '/api/v1/setup', 200)
  assert.equal(setup.value.initialized, false, 'Source database was not fresh')
  const initialized = await rawRequest(origin, '/api/v1/setup', 201, {
    method: 'POST',
    body: { email: 'backup@example.invalid', password, name: 'Backup administrator' },
  })
  const cookie = initialized.response.headers
    .getSetCookie()
    .find((item) => item.startsWith('routex_session='))
    ?.split(';')[0]
  assert.ok(cookie, 'Original Session is missing')
  const session = (await rawRequest(origin, '/api/v1/auth/session', 200, { cookie })).value
  assert.equal(session.user.role, 'admin', 'Setup did not retain administrator authority')
  const api = { origin, cookie, csrf: session.csrf_token }
  const get = async (path) =>
    (await rawRequest(api.origin, `/api/v1${path}`, 200, { cookie })).value
  const write = async (path, body, status = 200, method = 'POST', etag) =>
    (
      await rawRequest(api.origin, `/api/v1${path}`, status, {
        cookie,
        csrf: api.csrf,
        body,
        method,
        etag,
      })
    ).value
  const provider = await write(
    '/admin/providers',
    {
      name: 'Backup fixture',
      connection_name: 'Controlled native',
      base_url: `http://127.0.0.1:${peer.address().port}/v1`,
      protocol: 'openai_chat',
      credential_name: 'Encrypted inline',
      secret: credential,
    },
    201,
  )
  const credentialID = provider.connections[0].credentials[0].id
  assert.equal(
    (await write(`/admin/credentials/${credentialID}/verify`, {})).verified,
    true,
    'Credential verification failed',
  )
  await write(`/admin/credentials/${credentialID}`, { enabled: true }, 200, 'PATCH')
  const catalog = await get('/admin/providers')
  const connection = catalog.items.find((item) => item.id === provider.id).connections[0]
  const providerModel = connection.provider_models.find(
    (item) => item.upstream_name === 'backup-native',
  )
  assert.ok(providerModel, 'Verified native Model is missing')
  const boundPath = `/admin/provider-models/${providerModel.id}/reservation-bound`
  const bound = await get(boundPath)
  await write(
    boundPath,
    {
      max_input_tokens: 10,
      max_output_tokens: 10,
      evidence: 'Controlled peer enforces this finite fixture capacity',
      reason: 'Backup drill finite reservation',
    },
    200,
    'PUT',
    bound.etag,
  )
  const model = await write(
    '/admin/models',
    { name: 'backup-model', provider_model_id: providerModel.id },
    201,
  )
  await write(
    `/admin/models/${model.id}/weights`,
    { weights: model.bindings.map((binding) => ({ binding_id: binding.id, weight: 100 })) },
    200,
    'PUT',
  )
  const makeKey = async (name) => {
    const key = await write('/keys', { name, model_ids: [model.id] }, 201)
    await write(`/keys/${key.key.id}/confirm`, {})
    return key
  }
  const active = await makeKey('Retained active')
  const inference = async (key, status) => {
    const result = await rawRequest(api.origin, '/v1/chat/completions', status, {
      method: 'POST',
      bearer: key.secret,
      body: {
        model: 'backup-model',
        messages: [{ role: 'user', content: 'Backup continuity' }],
        max_completion_tokens: 10,
      },
    })
    if (status === 200)
      assert.equal(result.value.choices[0].message.content, 'Retained', 'Native answer changed')
    if (status === 429) requireQuotaExceeded(result.value)
    return result.response.headers.get('X-Request-ID')
  }
  const ids = [await inference(active, 200)]
  await waitCall(api, ids[0])
  const finite = await makeKey('Retained finite')
  const limitPath = `/keys/${finite.key.id}/limits`
  const limit = await get(limitPath)
  assert.equal(
    (
      await write(
        limitPath,
        { tokens_5h: 24, reason: 'Retain finite quota across cold restore' },
        200,
        'PUT',
        limit.etag,
      )
    ).enforced,
    true,
    'Finite policy was not applied',
  )
  ids.push(await inference(finite, 200))
  await waitCall(api, ids[1])
  ids.push(await inference(finite, 429))
  await waitCall(api, ids[2])
  const quotaBefore = (await get(limitPath)).quota_usage
  requireQuota(quotaBefore, 5)
  const revoked = await makeKey('Retained revoked')
  await write(`/keys/${revoked.key.id}`, undefined, 204, 'DELETE')
  const keysBefore = (await get('/keys')).items
  assert.equal(keysBefore.length, 3, 'Source Key census changed')
  assert.equal(nativeCalls, 2, 'Source peer dispatch census changed')
  await stopApplication()

  // Source is positively joined before either half of the cold backup is captured.
  const factsBefore = await captureFacts(driver, sourceDB, ids)
  assert.equal(
    factsBefore.calls.toString().trim().split('\n').length,
    3,
    'Source Call census changed',
  )
  requireNativeAttempts(
    (
      await sql(
        driver,
        sourceDB,
        'SELECT snapshot_id,credential_id,native_completion_evidence,status,attempt_number FROM call_attempts ORDER BY id',
      )
    ).toString(),
    credentialID,
    2,
  )
  const dumpArgs =
    driver === 'postgres'
      ? ['pg_dump', '-Fc', '-U', 'routex', '-d', sourceDB]
      : [
          'sh',
          '-c',
          'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysqldump --single-transaction --skip-comments --no-tablespaces --set-gtid-purged=OFF -u root "$1"',
          'mysqldump',
          sourceDB,
        ]
  const dump = await command(`${driver}-dump`, [...compose, 'exec', '-T', driver, ...dumpArgs], {
    timeout: 60_000,
    cap: dumpLimit,
  })
  const secrets = [
    root,
    credential,
    password,
    active.secret,
    finite.secret,
    revoked.secret,
    cookie.split('=')[1],
    session.csrf_token,
  ]
  // The custom PostgreSQL archive is compressed; inspect the restored SQL text,
  // while retaining the original archive for every real restore below.
  const scanBytes =
    driver === 'postgres'
      ? await command(
          `${driver}-dump-secret-scan`,
          [...compose, 'exec', '-T', driver, 'pg_restore', '--file=-'],
          { input: dump, timeout: 60_000, cap: dumpLimit },
        )
      : dump
  requireNoSecrets(scanBytes, secrets)
  const dumpPath = join(output, `${driver}.dump`)
  await writeFile(dumpPath, dump, { mode: 0o600, flag: 'wx' })
  assert.ok((await lstat(sourceJournal)).size <= dumpLimit, 'Journal backup exceeds the bound')
  const sourceBytes = await readFile(sourceJournal)
  assert.ok(
    sourceBytes.length > 0 && sourceBytes.length <= dumpLimit,
    'Journal backup exceeds the bound',
  )
  requireNoSecrets(sourceBytes, secrets)
  const journalBackup = join(output, `${driver}-backup.db`)
  await copyFile(sourceJournal, journalBackup, constants.COPYFILE_EXCL)
  const journalBytes = await readFile(journalBackup)
  assert.ok(
    journalBytes.length > 0 && journalBytes.length <= dumpLimit,
    'Journal backup exceeds the bound',
  )
  assert.ok(sourceBytes.equals(journalBytes), 'Stopped journal changed during capture')
  for (const database of [restoredDB, missingDB, wrongDB]) {
    const restoreArgs =
      driver === 'postgres'
        ? [
            'pg_restore',
            '--exit-on-error',
            '--no-owner',
            '--no-acl',
            '-U',
            'routex',
            '-d',
            database,
          ]
        : [
            'sh',
            '-c',
            'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --binary-mode=1 -u root "$1"',
            'mysql',
            database,
          ]
    await command(`${driver}-restore`, [...compose, 'exec', '-T', driver, ...restoreArgs], {
      input: dump,
      timeout: 60_000,
    })
  }
  const foreignJournal = join(output, `${driver}-foreign.db`)
  await launch(driver, foreignDB, foreignJournal, dsn, root)
  await stopApplication()
  await launch(driver, missingDB, join(output, `${driver}-missing.db`), dsn, root, true)
  const wrongJournal = join(output, `${driver}-wrong.db`)
  await copyFile(foreignJournal, wrongJournal, constants.COPYFILE_EXCL)
  await launch(driver, wrongDB, wrongJournal, dsn, root, true)
  const restoredJournal = join(output, `${driver}-restored.db`)
  await copyFile(journalBackup, restoredJournal, constants.COPYFILE_EXCL)
  assert.ok(
    (await readFile(restoredJournal)).equals(journalBytes),
    'Journal copy changed before startup',
  )
  requireSameFacts(factsBefore, await captureFacts(driver, restoredDB, ids))
  api.origin = (await launch(driver, restoredDB, restoredJournal, dsn, root)).origin
  const restoredSession = await get('/auth/session')
  assert.deepEqual(restoredSession.user, session.user, 'Original administrator Session changed')
  assert.equal(restoredSession.csrf_token, session.csrf_token, 'Original Session CSRF changed')
  api.csrf = restoredSession.csrf_token
  assert.equal((await get('/setup')).initialized, true, 'Restored initialization was lost')
  const keys = await get('/keys')
  assert.deepEqual(keys.items, keysBefore, 'Retained Key identities, scopes or births changed')
  for (const [key, status] of [
    [active, 'active'],
    [finite, 'active'],
    [revoked, 'revoked'],
  ]) {
    assert.equal(
      keys.items.find((item) => item.id === key.key.id)?.status,
      status,
      'Retained Key state changed',
    )
  }
  const quotaAfter = (await get(limitPath)).quota_usage
  requireQuota(quotaAfter, 5)
  assert.equal(
    quotaAfter.coverage_start,
    quotaBefore.coverage_start,
    'Quota coverage birth changed',
  )
  assert.equal((await get(limitPath)).stored.tokens_5h, 24, 'Finite policy changed')
  const beforeDispatch = nativeCalls
  await inference(revoked, 401)
  await inference(finite, 429)
  assert.equal(nativeCalls, beforeDispatch, 'Rejected restored Key reached upstream')
  const restoredCall = await inference(active, 200)
  await waitCall(api, restoredCall)
  assert.equal(nativeCalls, 3, 'Restored inline Credential was not used exactly once')
  requireQuota((await get(limitPath)).quota_usage, 5)
  await stopApplication()
  requireSameFacts(factsBefore, await captureFacts(driver, restoredDB, ids))
  requireNativeAttempts(
    (
      await sql(
        driver,
        restoredDB,
        'SELECT snapshot_id,credential_id,native_completion_evidence,status,attempt_number FROM call_attempts ORDER BY id',
      )
    ).toString(),
    credentialID,
    3,
  )
  await closePeer(Math.min(workDeadline, performance.now() + 5000))
  results.push({
    driver,
    original_session_retained: true,
    administrator_retained: true,
    active_and_revoked_keys_retained: true,
    immutable_calls: ids.length,
    original_completed_attempts: 2,
    restored_completed_attempts: 3,
    quota_tokens_retained: 5,
    quota_limit: 24,
    rejected_keys_no_dispatch: true,
    encrypted_inline_native_usable: true,
    missing_journal_rejected: true,
    foreign_journal_rejected: true,
    dump_sha256: createHash('sha256').update(dump).digest('hex'),
    journal_sha256: createHash('sha256').update(journalBytes).digest('hex'),
  })
  console.log(`${driver}: cold SQL+journal restore assertions passed; cleanup pending`)
}

async function closePeer(deadline) {
  const ownedPeer = peer
  ownedPeer.closeAllConnections()
  let timer
  try {
    await new Promise((resolveClosed, reject) => {
      timer = setTimeout(() => {
        ownedPeer.closeAllConnections()
        ownedPeer.unref()
        reject(new Error('Owned peer closure exceeded its bound'))
      }, remaining(deadline))
      ownedPeer.close((error) =>
        error ? reject(new Error('Owned peer closure failed')) : resolveClosed(),
      )
    })
    peer = undefined
  } finally {
    clearTimeout(timer)
  }
}

async function portClosed(port, deadline) {
  while (performance.now() < deadline) {
    const refused = await new Promise((resolveRefused) => {
      const socket = createConnection({ host: '127.0.0.1', port })
      socket.once('connect', () => {
        socket.destroy()
        resolveRefused(false)
      })
      socket.once('error', (error) => {
        socket.destroy()
        resolveRefused(error.code === 'ECONNREFUSED')
      })
      socket.setTimeout(500, () => {
        socket.destroy()
        resolveRefused(false)
      })
    })
    assert.ok(refused, 'Owned port still accepts connections')
    const bindable = await new Promise((resolveBindable, reject) => {
      const socket = createServer()
      socket.once('error', (error) =>
        error.code === 'EADDRINUSE'
          ? resolveBindable(false)
          : reject(new Error('Owned port bind failed')),
      )
      socket.listen(port, '127.0.0.1', () => socket.close(() => resolveBindable(true)))
    })
    if (bindable) return
    await delay(Math.min(20, Math.max(0, deadline - performance.now())))
  }
  throw new Error('Owned port is not freshly bindable')
}

let functional = false
const errors = []
try {
  await save('owner.json', {
    project,
    pid: process.pid,
    binary_sha256: binaryHash,
    work_seconds: 600,
    cleanup_seconds: 90,
  })
  // Independent databases inside the disposable services; no host database is addressed.
  await writeFile(
    composeFile,
    `services:\n  postgres:\n    image: postgres:18-alpine\n    environment:\n      POSTGRES_DB: routex_backup_admin\n      POSTGRES_USER: routex\n      POSTGRES_PASSWORD: routex-test\n    ports: ["127.0.0.1::5432"]\n    tmpfs: ["/var/lib/postgresql"]\n    healthcheck:\n      test: ["CMD-SHELL", "pg_isready -U routex -d routex_backup_admin"]\n      interval: 2s\n      timeout: 3s\n      retries: 30\n  mysql:\n    image: mysql:8.4\n    environment:\n      MYSQL_DATABASE: routex_backup_admin\n      MYSQL_USER: routex\n      MYSQL_PASSWORD: routex-test\n      MYSQL_ROOT_PASSWORD: routex-root-test\n    ports: ["127.0.0.1::3306"]\n    tmpfs: ["/var/lib/mysql"]\n    healthcheck:\n      test: ["CMD-SHELL", "MYSQL_PWD=$$MYSQL_ROOT_PASSWORD mysql -u root -e 'SELECT 1'"]\n      interval: 2s\n      timeout: 3s\n      retries: 45\n`,
    { mode: 0o600, flag: 'wx' },
  )
  for (const [kind, args] of [
    ['container', ['ps', '-aq']],
    ['network', ['network', 'ls', '-q']],
    ['volume', ['volume', 'ls', '-q']],
  ]) {
    assert.equal(
      (
        await command(`fresh-${kind}`, [
          ...args,
          '--filter',
          `label=com.docker.compose.project=${project}`,
        ])
      ).length,
      0,
      'Compose project is not fresh',
    )
  }
  composeStarted = true
  await command(
    'compose-up',
    [...compose, 'up', '--pull', 'never', '--detach', '--wait', '--wait-timeout', '180'],
    { timeout: 190_000 },
  )
  for (const [driver, containerPort] of [
    ['postgres', 5432],
    ['mysql', 3306],
  ]) {
    const address = (
      await command(`${driver}-port`, [...compose, 'port', driver, String(containerPort)])
    )
      .toString()
      .trim()
    assert.ok(
      /^127\.0\.0\.1:[1-9][0-9]{0,4}$/.test(address),
      'Unexpected disposable database address',
    )
    const port = Number(address.split(':')[1])
    assert.ok(port <= 65535, 'Invalid disposable database port')
    ports.add(port)
    await exercise(driver, { port })
  }
  functional = true
} catch {
  failureStage = stage
  errors.push(interrupted ? 'interrupted' : 'functional_drill_failed')
} finally {
  const beforeCleanupErrors = errors.length
  cleanupDeadline = Math.min(absoluteDeadline, performance.now() + cleanupMilliseconds)
  if (peer) {
    try {
      await closePeer(Math.min(cleanupDeadline, performance.now() + 5000))
    } catch {
      errors.push('peer_cleanup_failed')
    }
  }
  try {
    await stopApplication()
  } catch {
    errors.push('application_cleanup_failed')
  }
  for (const child of children) {
    if (!child.entry.joined || !absent(child.entry.pid, true)) {
      try {
        await joinChild(child, Math.min(cleanupDeadline, performance.now() + 10_000), true)
      } catch {
        errors.push('child_cleanup_failed')
      }
    }
  }
  const ownedClosed = children.every(
    ({ entry }) =>
      entry.joined && entry.group_gone && absent(entry.pid) && absent(entry.pgid, true),
  )
  if (!ownedClosed) errors.push('owned_groups_remain')
  if (composeStarted && ownedClosed) {
    try {
      await command(
        'compose-down',
        [...compose, 'down', '--volumes', '--remove-orphans', '--timeout', '10'],
        { timeout: 45_000, deadline: cleanupDeadline },
      )
    } catch {
      errors.push('compose_cleanup_failed')
    }
  }
  if (ownedClosed) {
    try {
      for (const [kind, args] of [
        ['container', ['ps', '-aq']],
        ['network', ['network', 'ls', '-q']],
        ['volume', ['volume', 'ls', '-q']],
      ]) {
        assert.equal(
          (
            await command(
              `closed-${kind}`,
              [...args, '--filter', `label=com.docker.compose.project=${project}`],
              { deadline: cleanupDeadline, timeout: 5000 },
            )
          ).length,
          0,
        )
      }
      for (const port of ports) await portClosed(port, cleanupDeadline)
    } catch {
      errors.push('resource_or_port_cleanup_failed')
    }
  }
  if (
    createHash('sha256')
      .update(await readFile(binary))
      .digest('hex') !== binaryHash
  )
    errors.push('binary_changed')
  await save('result.json', {
    accepted: functional && !errors.length,
    functional_assertions_passed: functional,
    cleanup_passed: errors.length === beforeCleanupErrors,
    project,
    binary_sha256: binaryHash,
    results,
    owned_processes: children.map(({ entry }) => entry),
    owned_ports: [...ports],
    failure_stage: failureStage,
    errors,
  })
  console.log(
    errors.length
      ? 'Backup/restore drill failed; private result retained.'
      : 'Both-driver cold backup/restore drill and owned cleanup passed.',
  )
  process.exitCode = functional && !errors.length ? 0 : 1
}
