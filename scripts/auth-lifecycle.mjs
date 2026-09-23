import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
import { createServer as createHTTPServer } from 'node:http'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { writeFile } from 'node:fs/promises'
import { createServer } from 'node:net'
import { join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'

const [binary, fixture] = process.argv.slice(2)
assert.ok(binary && fixture, 'A test binary and temporary directory are required')

const encryptionKey = randomBytes(32).toString('base64')
let server
let stopped
async function stopServer() {
  const child = server
  if (!child) return
  const completion = stopped
  server = undefined
  child.kill('SIGTERM')
  const force = setTimeout(() => child.kill('SIGKILL'), 5000)
  try { await completion } finally { clearTimeout(force) }
}
for (const [signal, code] of [['SIGINT', 130], ['SIGTERM', 143]]) {
  process.once(signal, () => { void stopServer().finally(() => process.exit(code)) })
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
  stopped = new Promise((resolve) => {
    server.once('error', () => { ended = true; resolve() })
    server.once('exit', () => { ended = true; resolve() })
  })
  for (let attempt = 0; attempt < 150; attempt++) {
    assert.ok(!ended, `${driver}: server exited before readiness`)
    try {
      const response = await fetch(`${origin}/health`, { signal: AbortSignal.timeout(500) })
      if (response.ok && await response.text() === 'ok') {
        await delay(50)
        assert.ok(!ended, `${driver}: server exited during readiness`)
        return
      }
    } catch { /* The listener may not be ready yet. */ }
    await delay(100)
  }
  throw new Error(`${driver}: server readiness timed out`)
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

async function verifyGatewayLifecycle(origin, config, driver, cookie, csrf) {
  const credential = randomBytes(24).toString('hex')
  const upstream = createHTTPServer(async (req, res) => {
    if (req.headers.authorization !== `Bearer ${credential}`) { res.writeHead(401); res.end(); return }
    if (req.url === '/v1/models') {
      res.setHeader('Content-Type', 'application/json')
      res.end(JSON.stringify({ data: [{ id: 'native-model' }] }))
      return
    }
    let raw = ''
    for await (const chunk of req) raw += chunk
    const body = JSON.parse(raw)
    if (req.url !== '/v1/chat/completions' || body.model !== 'native-model') { res.writeHead(400); res.end(); return }
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
    await write(`/admin/credentials/${credentialID}`, { enabled: true }, 200, 'PATCH')
    const providers = await (await request(origin, '/admin/providers', 200, { cookie })).json()
    assert.ok(!JSON.stringify(providers).includes(credential), 'Provider list disclosed secret')
    const native = providers.items[0].connections[0].provider_models[0]
    const model = await (await write('/admin/models', { name: 'gateway-test', provider_model_id: native.id }, 201)).json()
    await write(`/admin/models/${model.id}/weights`, { weights: model.bindings.map((binding) => ({ binding_id: binding.id, weight: 100 })) }, 200, 'PUT')
    const key = await (await write('/keys', { name: 'Lifecycle key', model_ids: [model.id] }, 201)).json()
    const inference = async (stream, expected = 200) => {
      const response = await fetch(`${origin}/v1/chat/completions`, {
        method: 'POST', headers: { Authorization: `Bearer ${key.secret}`, 'Content-Type': 'application/json' },
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
    const stream = await (await inference(true)).text()
    assert.ok(stream.includes('Hello') && stream.includes('[DONE]'), 'Streaming response was incomplete')
    assert.ok(!stream.includes('native-model'), 'Native model name leaked through streaming response')
    const record = await (await request(origin, `/calls/${requestID}`, 200, { cookie })).json()
    assert.ok(!JSON.stringify(record).includes(credential), 'Call record disclosed secret')
    await stopServer()
    await startServer(config, origin, driver)
    await inference(false)
    await write(`/keys/${key.key.id}`, undefined, 204, 'DELETE')
    await stopServer()
    await startServer(config, origin, driver)
    await inference(false, 401)
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
  for (const driver of ['postgres', 'mysql']) {
    assert.ok(process.env[`ROUTEX_TEST_${driver.toUpperCase()}_DSN`], `${driver}: test DSN is required`)
    const origin = `http://127.0.0.1:${await unusedPort()}`
    const config = join(fixture, `${driver}.yaml`)
    // The DSN stays in the subprocess environment, outside the temporary file.
    await writeFile(config, `addr: "${new URL(origin).host}"\ndriver: ${driver}\ndsn: "\${ROUTEX_LIFECYCLE_DSN}"\nencryption_key: "\${ROUTEX_LIFECYCLE_ENCRYPTION_KEY}"\nallow_private_upstreams: true\n`, { mode: 0o600 })
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

      await stopServer()
      await startServer(config, origin, driver)
      const after = await (await request(origin, '/setup', 200)).json()
      assert.equal(after.initialized, true, `${driver}: initialization did not survive process restart`)
      const restored = await (await request(origin, '/auth/session', 200, { cookie })).json()
      assert.equal(restored.user.id, initialized.user.id, `${driver}: session did not survive process restart`)
      assert.ok(restored.csrf_token, `${driver}: restored session has no CSRF token`)
      await request(origin, '/auth/logout', 204, { method: 'POST', cookie, csrf: restored.csrf_token })
      await request(origin, '/auth/session', 401, { cookie })
      const login = await request(origin, '/auth/login', 200, { method: 'POST', body: credentials })
      const replacement = sessionCookie(login)
      assert.ok(replacement !== cookie, `${driver}: login reused the revoked bearer`)
      const current = await (await request(origin, '/auth/session', 200, { cookie: replacement })).json()
      assert.equal(current.user.id, initialized.user.id, `${driver}: login after restart has the wrong identity`)
      console.log(`${driver}: empty database, initialization, process restart, persisted session, logout revocation, and login passed`)
      await verifyGatewayLifecycle(origin, config, driver, replacement, current.csrf_token)
    } finally {
      await stopServer()
    }
  }
} catch (error) {
  // Avoid dumping request options, response bodies, environment, or bearer data.
  console.error(error.message)
  process.exitCode = 1
}
