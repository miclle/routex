import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { writeFile } from 'node:fs/promises'
import { createServer } from 'node:net'
import { join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'

const [binary, fixture] = process.argv.slice(2)
assert.ok(binary && fixture, 'A test binary and temporary directory are required')

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
    env: { ...process.env, ROUTEX_LIFECYCLE_DSN: process.env[`ROUTEX_TEST_${driver.toUpperCase()}_DSN`] },
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
    await writeFile(config, `addr: "${new URL(origin).host}"\ndriver: ${driver}\ndsn: "\${ROUTEX_LIFECYCLE_DSN}"\n`, { mode: 0o600 })
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
    } finally {
      await stopServer()
    }
  }
} catch (error) {
  // Avoid dumping request options, response bodies, environment, or bearer data.
  console.error(error.message)
  process.exitCode = 1
}
