import assert from 'node:assert/strict'
import { execFileSync, spawn } from 'node:child_process'
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { setTimeout as delay } from 'node:timers/promises'
import test from 'node:test'

const root = dirname(dirname(fileURLToPath(import.meta.url)))
const task = execFileSync('go', ['tool', '-n', 'task'], { cwd: root, encoding: 'utf8' }).trim()

function alive(pid) {
  try { process.kill(pid, 0); return true } catch { return false }
}

async function until(predicate, message) {
  for (let i = 0; i < 200; i++) {
    if (predicate()) return
    await delay(50)
  }
  assert.fail(message)
}

for (const scenario of ['interrupt', 'frontend failure']) {
  test(`dev preserves unrelated processes and cleans up after ${scenario}`, { timeout: 30000 }, async () => {
    const fixture = mkdtempSync(join(tmpdir(), 'routex-dev-test-'))
    for (const dir of ['bin', 'scripts', 'website', 'cmd/routex']) mkdirSync(join(fixture, dir), { recursive: true })
    for (const file of ['Taskfile.yaml', 'cmd/routex/config.example.yaml']) {
      copyFileSync(join(root, file), join(fixture, file))
    }
    // If the old cleanup exists, exercise it against only our controlled process
    // table so this regression can never terminate a real development server.
    const oldCleanup = 'scripts/kill-vite.sh'
    if (existsSync(join(root, oldCleanup))) copyFileSync(join(root, oldCleanup), join(fixture, oldCleanup))
    const worker = join(fixture, 'worker.mjs')
    writeFileSync(worker, `
      import { writeFileSync, existsSync } from 'node:fs'
      import { join } from 'node:path'
      const state = process.env.ROUTEX_TEST_STATE
      const role = process.argv[2]
      writeFileSync(join(state, role + '.pid'), String(process.pid))
      process.on('SIGINT', () => process.exit(130))
      process.on('SIGTERM', () => process.exit(143))
      setInterval(() => {
        if (role === 'frontend' && existsSync(join(state, 'fail-frontend'))) process.exit(7)
      }, 50)
    `)
    const env = {
      ...process.env,
      PATH: join(fixture, 'bin') + ':' + process.env.PATH,
      ROUTEX_TEST_STATE: fixture,
      ROUTEX_TEST_WORKER: worker,
      ROUTEX_TEST_NODE: process.execPath,
      ROUTEX_TEST_TASK: task,
    }
    const commands = {
      go: `if [ "$1" = tool ] && [ "$2" = reflex ]; then exec "$ROUTEX_TEST_NODE" "$ROUTEX_TEST_WORKER" backend; fi\nif [ "$1" = tool ] && [ "$2" = task ]; then shift 2; exec "$ROUTEX_TEST_TASK" "$@"; fi\nexit 99`,
      npm: 'exec "$ROUTEX_TEST_NODE" "$ROUTEX_TEST_WORKER" frontend',
      ps: 'printf "test %s 0 0 0 0 ?? S 0:00 0:00 npm run dev --prefix /unrelated/website\\n" "$(cat "$ROUTEX_TEST_STATE/other.pid")"',
    }
    for (const [name, command] of Object.entries(commands)) {
      writeFileSync(join(fixture, 'bin', name), '#!/bin/sh\n' + command + '\n', { mode: 0o755 })
    }
    const other = spawn(process.execPath, [worker, 'other'], { env, stdio: 'ignore' })
    let runner
    let output = ''
    try {
      await until(() => existsSync(join(fixture, 'other.pid')), 'control process did not start')
      runner = spawn(task, ['dev'], { cwd: fixture, env, detached: true })
      runner.stdout.on('data', (data) => { output += data })
      runner.stderr.on('data', (data) => { output += data })
      let result
      runner.on('close', (code, signal) => { result = { code, signal } })
      await until(() => result || (existsSync(join(fixture, 'frontend.pid')) && existsSync(join(fixture, 'backend.pid'))), 'dev workers did not start')
      assert.ok(!result, `dev exited before starting workers: ${output}`)
      assert.ok(alive(other.pid), 'dev terminated an unrelated website process')
      if (scenario === 'interrupt') process.kill(-runner.pid, 'SIGINT')
      else writeFileSync(join(fixture, 'fail-frontend'), '')
      await until(() => result, `dev did not exit: ${output}`)
      if (scenario === 'frontend failure') assert.notEqual(result.code, 0, 'dev swallowed frontend failure')
      for (const role of ['frontend', 'backend']) {
        const pid = Number(readFileSync(join(fixture, role + '.pid'), 'utf8'))
        await until(() => !alive(pid), `${role} remained running after dev exited`)
      }
      assert.ok(alive(other.pid), 'dev cleanup terminated an unrelated process')
    } catch (error) {
      throw new Error(`${error.message}\nTask output:\n${output}`, { cause: error })
    } finally {
      if (runner) {
        try { process.kill(-runner.pid, 'SIGKILL') } catch { /* already stopped */ }
      }
      for (const role of ['frontend', 'backend', 'other']) {
        const file = join(fixture, role + '.pid')
        if (existsSync(file)) {
          try { process.kill(Number(readFileSync(file, 'utf8')), 'SIGKILL') } catch { /* already stopped */ }
        }
      }
      rmSync(fixture, { recursive: true, force: true })
    }
  })
}
