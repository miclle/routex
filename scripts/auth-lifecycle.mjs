import assert from 'node:assert/strict'
import { randomBytes, randomUUID } from 'node:crypto'
import { createServer as createHTTPServer } from 'node:http'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'node:net'
import { join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import { prepareOffboardingNoticeRestart } from './auth-lifecycle-offboarding.mjs'

const [binary, fixture, selectedDriver] = process.argv.slice(2)
assert.ok(process.argv.length <= 5 && (!selectedDriver || ['postgres', 'mysql'].includes(selectedDriver)), 'Invalid database profile')
assert.ok(binary && fixture, 'A test binary and temporary directory are required')

const encryptionKey = randomBytes(32).toString('base64')
let server
let stopped
const gatewayGenerations = []
let refusedServer
async function stopRefusedServer() {
  const owned = refusedServer
  if (!owned) return
  owned.child.kill('SIGTERM')
  const force = setTimeout(() => owned.child.kill('SIGKILL'), 5000)
  try { await owned.closed } finally { clearTimeout(force) }
}
async function stopServer() {
  const child = server
  // Cancellation during an existing stop must still join that original child.
  if (!child) return stopped
  const completion = stopped
  server = undefined
  child.kill('SIGTERM')
  let timedOut = false
  const force = setTimeout(() => { timedOut = true; child.kill('SIGKILL') }, 8000)
  let terminal
  try { terminal = await completion } finally { clearTimeout(force) }
  terminal.forced_timeout = timedOut
  assert.ok(!timedOut && terminal.exit_code === 0 && terminal.signal === null && !terminal.spawn_error, 'Serving gateway did not join with a normal successful exit')
}
for (const [signal, code] of [['SIGINT', 130], ['SIGTERM', 143]]) {
  process.once(signal, () => { void Promise.allSettled([stopServer(), stopRefusedServer()]).then(() => process.exit(code)) })
}

async function unusedPort() {
  const reservation = createServer()
  reservation.listen(0, '127.0.0.1')
  await once(reservation, 'listening')
  const { port } = reservation.address()
  await new Promise((resolve, reject) => reservation.close((error) => error ? reject(error) : resolve()))
  return port
}

async function startServer(config, origin, driver) {
  server = spawn(binary, ['-c', config], {
    env: { ...process.env, ROUTEX_LIFECYCLE_ENCRYPTION_KEY: encryptionKey, ROUTEX_LIFECYCLE_DSN: process.env[`ROUTEX_TEST_${driver.toUpperCase()}_DSN`] },
    // HTTP responses and server SQL output may contain secrets; never echo them.
    stdio: 'ignore',
  })
  let ended = false
  const generation = { driver, pid: server.pid, started_at: new Date().toISOString(), ready: false, joined: false, forced_timeout: false }
  gatewayGenerations.push(generation)
  stopped = new Promise((resolve) => {
    server.once('error', () => { generation.spawn_error = true })
    server.once('close', (code, signal) => {
      ended = true
      generation.exit_code = code; generation.signal = signal; generation.joined = true
      resolve(generation)
    })
  })
  for (let attempt = 0; attempt < 150; attempt++) {
    assert.ok(!ended, `${driver}: server exited before readiness`)
    try {
      const response = await fetch(`${origin}/health`, { signal: AbortSignal.timeout(500) })
      if (response.ok && await response.text() === 'ok') {
        await delay(50)
        assert.ok(!ended, `${driver}: server exited during readiness`)
        generation.ready = true
        return
      }
    } catch { /* The listener may not be ready yet. */ }
    await delay(100)
  }
  throw new Error(`${driver}: server readiness timed out`)
}

// Main's initialized quota journal is deliberately single-owner. A second
// empty journal must not silently reset durable quotas or masquerade as HA.
async function verifyIndependentJournalRefusal(config, servingOrigin, driver, cookie) {
  const refusedPort = await unusedPort()
  const refusedConfig = join(fixture, `${driver}-independent.yaml`)
  const original = await readFile(config, 'utf8')
  const changed = original
    .replace(`addr: "${new URL(servingOrigin).host}"`, `addr: "127.0.0.1:${refusedPort}"`)
    .replace(`event_queue_path: "${driver}-calls.db"`, `event_queue_path: "${driver}-independent.db"`)
  assert.notEqual(changed, original)
  assert.ok(changed.includes(`event_queue_path: "${driver}-independent.db"`))
  await writeFile(refusedConfig, changed, { mode: 0o600 })
  const child = spawn(binary, ['-c', refusedConfig], {
    env: { ...process.env, ROUTEX_LIFECYCLE_ENCRYPTION_KEY: encryptionKey, ROUTEX_LIFECYCLE_DSN: process.env[`ROUTEX_TEST_${driver.toUpperCase()}_DSN`] },
    stdio: ['ignore', 'ignore', 'pipe'],
  })
  let stderr = '', overflow = false, timedOut = false
  const stop = () => child.kill('SIGTERM')
  const interrupted = () => { timedOut = true; stop() }
  process.once('SIGINT', interrupted)
  process.once('SIGTERM', interrupted)
  child.stderr.on('data', (data) => {
    if (stderr.length + data.length > 65536) { overflow = true; stop() }
    else stderr += data.toString()
  })
  const timeout = setTimeout(() => { timedOut = true; stop() }, 20000)
  const force = setTimeout(() => child.kill('SIGKILL'), 25000)
  let spawnError
  child.once('error', (error) => { spawnError = error.code })
  const owned = { child, closed: new Promise((resolve) => child.once('close', (code, signal) => resolve([code, signal]))) }
  refusedServer = owned
  const [code, signal] = await owned.closed
  if (refusedServer === owned) refusedServer = undefined
  clearTimeout(timeout); clearTimeout(force)
  process.removeListener('SIGINT', interrupted)
  process.removeListener('SIGTERM', interrupted)
  assert.ok(!spawnError && !timedOut && !overflow, `${driver}: independent journal refusal was inconclusive`)
  assert.equal(code, 1, `${driver}: independent empty journal was not rejected`)
  assert.equal(signal, null, `${driver}: rejected process did not exit normally`)
  assert.ok(stderr.includes('open durable call buffer failed'), `${driver}: rejection occurred at an unrelated startup stage`)
  // Bind the exact refused listener after its positive process join. An unrelated
  // startup failure or a leftover listener cannot stand in for the journal guard.
  const listener = createServer()
  listener.listen(refusedPort, '127.0.0.1')
  await once(listener, 'listening')
  await new Promise((resolve) => listener.close(resolve))
  const session = await (await request(servingOrigin, '/auth/session', 200, { cookie })).json()
  assert.ok(session.user?.id, `${driver}: refusal damaged the serving Session`)
  await writeFile(join(fixture, `${driver}-independent-journal.json`), JSON.stringify({
    scope: 'single_journal_startup_guard', driver, pid: child.pid, joined: true,
    exit_code: code, signal, listener_absent: true, serving_session_retained: true,
    multi_node_acceptance: false,
  }, null, 2), { mode: 0o600 })
  console.log(`${driver}: fresh second-process journal refused; original Session retained (not multi-node acceptance)`)
}

async function request(origin, path, status, { cookie, csrf, body, method = 'GET' } = {}) {
  const headers = { Origin: origin, 'Sec-Fetch-Site': 'same-origin' }
  if (cookie) headers.Cookie = cookie
  if (csrf) headers['X-CSRF-Token'] = csrf
  if (body) headers['Content-Type'] = 'application/json'
  const response = await fetch(`${origin}/api/v1${path}`, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
    signal: AbortSignal.timeout(10000),
    redirect: 'error',
  })
  assert.equal(response.status, status, `${method} ${path} returned an unexpected status`)
  return response
}

async function waitForCall(origin, requestID, cookie) {
  for (let attempt = 0; attempt < 60; attempt++) {
    const response = await fetch(`${origin}/api/v1/calls/${requestID}`, { headers: { Cookie: cookie }, signal: AbortSignal.timeout(1000) })
    if (response.status === 200) return response.json()
    assert.equal(response.status, 404, 'Call query failed while waiting for asynchronous delivery')
    await delay(100)
  }
  throw new Error('Durable call delivery timed out')
}

async function verifyGatewayLifecycle(origin, config, driver, cookie, csrf) {
  const credential = randomBytes(24).toString('hex')
  let discoveryRequests = 0
  let nativeRequests = 0
  const upstream = createHTTPServer(async (req, res) => {
    if (req.headers.authorization !== `Bearer ${credential}`) { res.writeHead(401); res.end(); return }
    if (req.url === '/v1/models') {
      discoveryRequests++
      res.setHeader('Content-Type', 'application/json')
      res.end(JSON.stringify({ data: [{ id: 'native-model' }, { id: 'native-model-alt' }] }))
      return
    }
    let raw = ''
    for await (const chunk of req) raw += chunk
    const body = JSON.parse(raw)
    if (req.url !== '/v1/chat/completions' || !['native-model', 'native-model-alt'].includes(body.model)) { res.writeHead(400); res.end(); return }
    nativeRequests++
    const usage = { prompt_tokens: 3, completion_tokens: 2, total_tokens: 5 }
    if (body.stream) {
      res.writeHead(200, { 'Content-Type': 'text/event-stream' })
      res.write(`data: ${JSON.stringify({ id: 'chat-test', model: 'native-model', choices: [{ index: 0, delta: { content: 'Hello' } }] })}\n\n`)
      res.end(`data: ${JSON.stringify({ model: 'native-model', choices: [], usage })}\n\ndata: [DONE]\n\n`)
    } else {
      res.setHeader('Content-Type', 'application/json')
      res.end(JSON.stringify({ id: 'chat-test', model: 'native-model', choices: [{ index: 0, message: { role: 'assistant', content: 'Hello' }, finish_reason: 'stop' }], usage }))
    }
  })
  upstream.listen(0, '127.0.0.1')
  await once(upstream, 'listening')
  try {
    const write = (path, body, status = 200, method = 'POST') => request(origin, path, status, { cookie, csrf, body, method })
    const provider = await (await write('/admin/providers', {
      name: 'Controlled provider', connection_name: 'Local test', base_url: `http://127.0.0.1:${upstream.address().port}/v1`,
      protocol: 'openai_chat', credential_name: 'Controlled credential', secret: credential,
    }, 201)).json()
    const connection = provider.connections[0]
    const credentialID = connection.credentials[0].id
    const verified = await (await write(`/admin/credentials/${credentialID}/verify`, {})).json()
    assert.equal(verified.verified, true, 'Controlled provider verification failed')
    // Use the existing production worker and executable restarts. Source replay
    // must preserve the exact recipient receipt without repeating discovery.
    const discoveryNotice = async () => {
      const page = await (await request(origin, '/notifications?severity=low', 200, { cookie })).json()
      const matches = page.items.filter((item) => item.kind === 'models_first_recorded' && item.subject_id === provider.id)
      assert.equal(matches.length, 1, 'First discovery did not create exactly one recipient receipt')
      const notice = matches[0]
      assert.equal(notice.severity, 'low', 'Discovery severity was not low')
      assert.equal(notice.detail_code, 'first_recorded', 'Discovery detail changed')
      assert.equal(notice.subject_type, 'provider', 'Discovery subject changed')
      assert.equal(notice.first_recorded_model_count, 2, 'Discovery model count changed')
      assert.equal(notice.occurrence_count, 1, 'Discovery replay duplicated occurrences')
      for (const field of ['delivery_status', 'delivery_code', 'delivery_attempts', 'delivery_updated_at', 'connection_id', 'credential_id', 'audit_id', 'quota']) {
        assert.ok(!(field in notice), 'Discovery exposed a private or external-delivery field')
      }
      assert.ok(!JSON.stringify(notice).includes(credential), 'Discovery disclosed credential material')
      return notice
    }
    const firstDiscovery = await discoveryNotice()
    assert.equal(firstDiscovery.read, false, 'New discovery receipt was already read')
    const markedDiscovery = await (await write(`/notifications/${firstDiscovery.id}/read`, undefined)).json()
    assert.equal(markedDiscovery.id, firstDiscovery.id, 'Read changed discovery receipt identity')
    assert.equal(markedDiscovery.read, true, 'Discovery read state was not saved')
    // Capture the persisted database timestamp before comparing across restarts.
    const retainedDiscovery = await discoveryNotice()
    assert.equal(retainedDiscovery.read, true, 'Persisted discovery receipt was not read')
    assert.ok(retainedDiscovery.read_at, 'Discovery read timestamp was not saved')
    const assertDiscoveryRetained = async () => {
      assert.deepEqual(await discoveryNotice(), retainedDiscovery, 'Executable restart or repeat discovery changed the saved receipt')
    }
    await write(`/admin/credentials/${credentialID}`, { enabled: true }, 200, 'PATCH')
    const providers = await (await request(origin, '/admin/providers', 200, { cookie })).json()
    assert.ok(!JSON.stringify(providers).includes(credential), 'Provider list disclosed secret')
    const supplies = providers.items[0].connections[0].provider_models
    const native = supplies.find((item) => item.upstream_name === 'native-model')
    const alternative = supplies.find((item) => item.upstream_name === 'native-model-alt')
    assert.ok(native && alternative, 'Both controlled native supplies must be discovered')
    let model = await (await write('/admin/models', { name: 'gateway-test', provider_model_id: native.id }, 201)).json()
    model = await (await write(`/admin/models/${model.id}/bindings`, { provider_model_id: alternative.id }, 201)).json()
    await write(`/admin/models/${model.id}/weights`, { weights: model.bindings.map((binding) => ({ binding_id: binding.id, weight: binding.provider_model_id === native.id ? 100 : 0 })) }, 200, 'PUT')
    const key = await (await write('/keys', { name: 'Lifecycle key', model_ids: [model.id] }, 201)).json()
    const inference = async (stream, expected = 200, bearer = key.secret) => {
      const response = await fetch(`${origin}/v1/chat/completions`, {
        method: 'POST', headers: { Authorization: `Bearer ${bearer}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ model: 'gateway-test', messages: [{ role: 'user', content: 'Hi' }], stream }), signal: AbortSignal.timeout(10000),
      })
      assert.equal(response.status, expected, 'Gateway returned unexpected status')
      return response
    }
    await inference(false, 401)
    await write(`/keys/${key.key.id}/confirm`, {})
    const ordinary = await inference(false)
    const requestID = ordinary.headers.get('X-Request-ID')
    const answer = await ordinary.json()
    assert.equal(answer.model, 'gateway-test', 'Public model name was not preserved')
    assert.equal(answer.choices[0].message.content, 'Hello', 'Ordinary response was lost')
    const streamed = await inference(true)
    const streamRequestID = streamed.headers.get('X-Request-ID')
    const stream = await streamed.text()
    assert.ok(stream.includes('Hello') && stream.includes('[DONE]'), 'Streaming response was incomplete')
    assert.ok(!stream.includes('native-model'), 'Native model name leaked through streaming response')
    const record = await waitForCall(origin, requestID, cookie)
    assert.ok(!JSON.stringify(record).includes(credential), 'Call record disclosed secret')
    const firstGatewayPID = server.pid
    await stopServer()
    await startServer(config, origin, driver)
    assert.notEqual(server.pid, firstGatewayPID, 'Gateway restart reused the owned process identity')
    assert.equal(discoveryRequests, 1, 'Executable restart repeated upstream discovery')
    await assertDiscoveryRetained()
    assert.equal((await (await write(`/admin/credentials/${credentialID}/verify`, {})).json()).verified, true, 'Repeated real discovery failed')
    assert.equal(discoveryRequests, 2, 'Explicit discovery did not use the controlled upstream exactly once')
    await assertDiscoveryRetained()
    const restored = await waitForCall(origin, streamRequestID, cookie)
    assert.equal(restored.input_tokens, 3, 'Restart changed accepted streaming usage')
    assert.equal(restored.output_tokens, 2, 'Restart changed accepted streaming usage')
    await inference(false)
    // Capture actual retained versions before emergency revocation. Restoration
    // may copy only routing fields, never the historical Key's active authority.
    await write('/admin/runtime/publish', { request_id: randomUUID(), reason: 'Capture current controlled routing before emergency revocation' })
    let archives
    for (let attempt = 0; attempt < 60; attempt++) {
      const page = await (await request(origin, '/admin/runtime/archives?limit=20', 200, { cookie })).json()
      for (const archive of page.items) {
        const preview = await (await request(origin, `/admin/runtime/archives/${archive.id}/rollback`, 200, { cookie })).json()
        if (preview.can_apply && preview.weights.length === 0 && preview.connections.length === 0) {
          archives = { items: [archive] }
          break
        }
      }
      if (archives) break
      await delay(100)
    }
    assert.ok(archives, 'No actual current-topology pre-revocation archive was retained')
    await write(`/keys/${key.key.id}`, undefined, 204, 'DELETE')
    const dispatchesAtRevoke = nativeRequests
    await inference(false, 401)
    assert.equal(nativeRequests, dispatchesAtRevoke, 'Emergency revocation dispatched upstream work')
    await write(`/admin/models/${model.id}/weights`, { weights: model.bindings.map((binding) => ({ binding_id: binding.id, weight: binding.provider_model_id === native.id ? 0 : 100 })) }, 200, 'PUT')
    let reviewed
    const previews = []
    for (const archive of archives.items) {
      const response = await request(origin, `/admin/runtime/archives/${archive.id}/rollback`, 200, { cookie })
      const preview = await response.json()
      previews.push(preview)
      if (preview.can_apply && preview.weights.some((weight) => weight.model_id === model.id && weight.before === 0 && weight.after === 100)) {
        reviewed = { archive, etag: response.headers.get('etag') }
        break
      }
    }
    await writeFile(join(fixture, `${driver}-rollback-previews.json`), JSON.stringify(previews, null, 2), { mode: 0o600 })
    assert.ok(reviewed?.etag, 'No actual historical 100-percent routing version was reviewable')
    const intent = { request_id: randomUUID(), reason: 'Controlled historical routing rollback must retain emergency Key revocation' }
    const rollbackResponse = await fetch(`${origin}/api/v1/admin/runtime/archives/${reviewed.archive.id}/rollback`, {
      method: 'POST', headers: { Cookie: cookie, Origin: origin, 'X-CSRF-Token': csrf, 'If-Match': reviewed.etag, 'Content-Type': 'application/json' },
      body: JSON.stringify(intent), signal: AbortSignal.timeout(10000), redirect: 'error',
    })
    assert.equal(rollbackResponse.status, 200, 'Routing rollback did not complete')
    const rollback = await rollbackResponse.json()
    assert.equal(rollback.runtime_applied, true, 'Rollback publication was not confirmed in the serving process')
    await inference(false, 401)
    assert.equal(nativeRequests, dispatchesAtRevoke, 'Historical rollback resurrected revoked Key dispatch')
    const control = await (await write('/keys', { name: 'Post-revocation control', model_ids: [model.id] }, 201)).json()
    await write(`/keys/${control.key.id}/confirm`, {})
    const controlCall = await inference(false, 200, control.secret)
    const controlRequestID = controlCall.headers.get('X-Request-ID')
    const controlBody = await controlCall.json()
    assert.equal(controlBody.choices[0].finish_reason, 'stop', 'Control inference lacked native completion')
    assert.equal(nativeRequests, dispatchesAtRevoke + 1, 'Control did not make exactly one dispatch')
    const secondGatewayPID = server.pid
    await stopServer()
    await startServer(config, origin, driver)
    assert.notEqual(server.pid, secondGatewayPID, 'Second gateway restart reused the owned process identity')
    assert.equal(discoveryRequests, 2, 'Second executable restart repeated upstream discovery')
    await assertDiscoveryRetained()
    await inference(false, 401)
    assert.equal(nativeRequests, dispatchesAtRevoke + 1, 'Restart resurrected revoked Key dispatch')
    const controlRecord = await waitForCall(origin, controlRequestID, cookie)
    assert.equal(controlRecord.key_id, control.key.id, 'Control Call lost exact Key identity')
    assert.equal(controlRecord.status, 'success', 'Control Call was not durably successful')
    const restartedControl = await inference(false, 200, control.secret)
    assert.equal((await restartedControl.json()).choices[0].finish_reason, 'stop')
    assert.equal(nativeRequests, dispatchesAtRevoke + 2, 'Restarted control did not make exactly one native dispatch')
    assert.deepEqual(await waitForCall(origin, streamRequestID, cookie), restored, 'Revocation or rollback rewrote immutable pre-revocation history')
    await writeFile(join(fixture, `${driver}-emergency-revocation.json`), JSON.stringify({
      scope: 'single_process_emergency_revocation_and_historical_routing_rollback', driver,
      revoked_key_id: key.key.id, control_key_id: control.key.id, control_request_id: controlRequestID,
      historical_archive_id: reviewed.archive.id, rollback_request_id: intent.request_id,
      runtime_applied: rollback.runtime_applied, dispatches_at_revoke: dispatchesAtRevoke,
      post_revoke_control_dispatches: nativeRequests - dispatchesAtRevoke,
      revoked_dispatches_after_revoke: 0, original_session_retained: true,
      restarted_pid: server.pid, previous_pid: secondGatewayPID, multi_node_acceptance: false,
    }, null, 2), { mode: 0o600 })
    console.log(`${driver}: immediate emergency revocation, historical routing rollback, surviving native control and original-Session restart passed`)
    // Independently reduce the exact provider credential. A surviving valid Key
    // must receive unavailable without any further upstream dispatch, including
    // after a real same-journal executable restart.
    await write(`/admin/credentials/${credentialID}`, { enabled: false }, 200, 'PATCH')
    const credentialDispatches = nativeRequests
    const unavailable = await inference(false, 503, control.secret)
    assert.equal((await unavailable.json()).error.code, 'upstream_unavailable', 'Disabled credential did not fail closed')
    assert.equal(nativeRequests, credentialDispatches, 'Credential disablement dispatched upstream work')
    const assertCredentialDisabled = async () => {
      const page = await (await request(origin, '/admin/providers', 200, { cookie })).json()
      const exactProvider = page.items.find((item) => item.id === provider.id)
      const exactConnection = exactProvider?.connections.find((item) => item.id === connection.id)
      const exactCredential = exactConnection?.credentials.find((item) => item.id === credentialID)
      assert.ok(exactCredential, 'The disabled credential identity was not retained')
      assert.equal(exactCredential.enabled, false, 'Emergency credential reduction was not retained')
    }
    await assertCredentialDisabled()
    const credentialPreviousPID = server.pid
    await stopServer()
    await startServer(config, origin, driver)
    assert.notEqual(server.pid, credentialPreviousPID, 'Credential restart reused the owned process identity')
    await assertCredentialDisabled()
    await inference(false, 401)
    const restartedUnavailable = await inference(false, 503, control.secret)
    assert.equal((await restartedUnavailable.json()).error.code, 'upstream_unavailable')
    assert.equal(nativeRequests, credentialDispatches, 'Restart resurrected disabled-credential dispatch')
    assert.equal(discoveryRequests, 2, 'Credential restart repeated upstream discovery')
    await assertDiscoveryRetained()
    assert.deepEqual(await waitForCall(origin, streamRequestID, cookie), restored, 'Credential reduction rewrote immutable native history')
    await writeFile(join(fixture, `${driver}-emergency-credential-disable.json`), JSON.stringify({
      scope: 'single_process_emergency_credential_disablement', driver, credential_id: credentialID,
      surviving_key_id: control.key.id, recorded_enabled: false, immediate_status: 503,
      restarted_status: 503, error_code: 'upstream_unavailable', disabled_credential_dispatches: 0,
      total_native_dispatches: nativeRequests, original_session_retained: true,
      previous_pid: credentialPreviousPID, restarted_pid: server.pid, multi_node_acceptance: false,
    }, null, 2), { mode: 0o600 })
    console.log(`${driver}: exact credential emergency disablement, zero dispatch and original-Session restart passed`)
    console.log(`${driver}: encrypted provider, model grants, Key confirmation, ordinary/streaming gateway, call facts, restart and persistent revocation passed`)
  } finally {
    upstream.closeAllConnections()
    await new Promise((resolve) => upstream.close(resolve))
  }
}

function sessionCookie(response) {
  const value = response.headers.getSetCookie().find((cookie) => cookie.startsWith('routex_session='))
  assert.ok(value, 'Authentication did not set a session cookie')
  return value.split(';')[0]
}

try {
  for (const driver of selectedDriver ? [selectedDriver] : ['postgres', 'mysql']) {
    assert.ok(process.env[`ROUTEX_TEST_${driver.toUpperCase()}_DSN`], `${driver}: test DSN is required`)
    const origin = `http://127.0.0.1:${await unusedPort()}`
    const config = join(fixture, `${driver}.yaml`)
    // The DSN stays in the subprocess environment, outside the temporary file.
    await writeFile(config, `addr: "${new URL(origin).host}"\ndriver: ${driver}\ndsn: "\${ROUTEX_LIFECYCLE_DSN}"\nencryption_key: "\${ROUTEX_LIFECYCLE_ENCRYPTION_KEY}"\nallow_private_upstreams: true\nevent_queue_path: "${driver}-calls.db"\n`, { mode: 0o600 })
    const credentials = { email: 'restart@example.invalid', password: 'test-only-restart-password' }
    try {
      await startServer(config, origin, driver)
      const before = await (await request(origin, '/setup', 200)).json()
      assert.equal(before.initialized, false, `${driver}: database was not empty`)
      const setup = await request(origin, '/setup', 201, { method: 'POST', body: { ...credentials, name: 'Restart Test' } })
      const cookie = sessionCookie(setup)
      const initialized = await setup.json()
      assert.ok(initialized.user?.id && initialized.user.role === 'admin', `${driver}: initialization did not return an administrator`)
      const first = await (await request(origin, '/auth/session', 200, { cookie })).json()
      assert.equal(first.user.id, initialized.user.id, `${driver}: initial session has the wrong identity`)
      const assertOffboardingNoticesRetained = await prepareOffboardingNoticeRestart({
        origin, request, sessionCookie, administrator: { cookie, session: first },
      })

      await stopServer()
      await startServer(config, origin, driver)
      const after = await (await request(origin, '/setup', 200)).json()
      assert.equal(after.initialized, true, `${driver}: initialization did not survive process restart`)
      const restored = await (await request(origin, '/auth/session', 200, { cookie })).json()
      assert.equal(restored.user.id, initialized.user.id, `${driver}: session did not survive process restart`)
      assert.ok(restored.csrf_token, `${driver}: restored session has no CSRF token`)
      await assertOffboardingNoticesRetained()
      await request(origin, '/auth/logout', 204, { method: 'POST', cookie, csrf: restored.csrf_token })
      await request(origin, '/auth/session', 401, { cookie })
      const login = await request(origin, '/auth/login', 200, { method: 'POST', body: credentials })
      const replacement = sessionCookie(login)
      assert.ok(replacement !== cookie, `${driver}: login reused the revoked bearer`)
      const current = await (await request(origin, '/auth/session', 200, { cookie: replacement })).json()
      assert.equal(current.user.id, initialized.user.id, `${driver}: login after restart has the wrong identity`)
      console.log(`${driver}: empty database, initialization, process restart, persisted session, logout revocation, and login passed`)
      await verifyIndependentJournalRefusal(config, origin, driver, replacement)
      await verifyGatewayLifecycle(origin, config, driver, replacement, current.csrf_token)
    } finally {
      try { await stopServer() } finally {
        await writeFile(join(fixture, `${driver}-gateway-generations.json`), JSON.stringify(gatewayGenerations.filter((generation) => generation.driver === driver), null, 2), { mode: 0o600 })
      }
    }
  }
} catch (error) {
  // Avoid dumping request options, response bodies, environment, or bearer data.
  console.error(error.message)
  process.exitCode = 1
}
