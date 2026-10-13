import assert from 'node:assert/strict'
import { spawn, spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { lstat, readFile, readlink } from 'node:fs/promises'
import { isAbsolute, resolve } from 'node:path'
import { performance } from 'node:perf_hooks'
import { setTimeout as delay } from 'node:timers/promises'
import { captureChildOutput, invalidateChild } from './backup-restore-support.mjs'

const hash = (bytes) => createHash('sha256').update(bytes).digest('hex')
export async function executableReceipt(path) {
  assert.ok(
    path && isAbsolute(path) && resolve(path) === path,
    'Canonical absolute executable required',
  )
  const info = await lstat(path)
  assert.ok(
    info.isFile() &&
      !info.isSymbolicLink() &&
      (info.mode & 0o111) !== 0 &&
      info.size < 256 * 1024 * 1024,
    'Invalid executable',
  )
  return { path, sha256: hash(await readFile(path)) }
}
export async function sourceEntry(path, relative) {
  const info = await lstat(path)
  assert.ok(info.isFile() || info.isSymbolicLink(), 'Unsupported source path')
  const symbolic = info.isSymbolicLink()
  return [
    relative,
    info.mode & 0o777,
    symbolic ? 'symlink' : 'file',
    hash(symbolic ? Buffer.from(await readlink(path)) : await readFile(path)),
  ]
}
export function absent(pid, group = false) {
  assert.ok(Number.isInteger(pid) && pid > 1 && pid !== process.pid, 'Invalid owned process')
  try {
    process.kill(group ? -pid : pid, 0)
    return false
  } catch (error) {
    if (error.code === 'ESRCH') return true
    throw new Error('Cannot establish owned process absence')
  }
}
export function createSupervisor({
  repo,
  fixtureEnv,
  children,
  workDeadline,
  absoluteDeadline,
  remaining,
  interrupted: isInterrupted,
}) {
  function processIdentity(pid) {
    const observed = spawnSync('ps', ['-o', 'pid=,pgid=,lstart=', '-p', String(pid)], {
      encoding: 'utf8',
      timeout: 2000,
      maxBuffer: 4096,
    })
    if (observed.status === 1 && !observed.stdout.trim()) return null
    assert.equal(observed.status, 0, 'Owned process observation failed')
    const match = observed.stdout.trim().match(/^(\d+)\s+(\d+)\s+(.+)$/)
    assert.ok(match && Number(match[1]) === pid, 'Owned process identity missing')
    return { pid, pgid: Number(match[2]), birth: match[3] }
  }
  function start(
    name,
    command,
    args,
    { env = fixtureEnv, deadline = workDeadline, input, stdoutLimit = 1 << 20 } = {},
  ) {
    remaining(deadline)
    const child = spawn(command, args, {
      cwd: repo,
      env,
      detached: true,
      stdio: ['pipe', 'pipe', 'pipe'],
    })
    const entry = {
      name,
      pid: child.pid,
      pgid: null,
      birth: null,
      spawned_at: new Date().toISOString(),
      joined: false,
      group_gone: false,
      exit_code: null,
      signal: null,
      stdout_bytes: 0,
      stderr_bytes: 0,
      stdout_limit: stdoutLimit,
      stderr_limit: 1 << 20,
      failure_codes: [],
    }
    const state = {
      child,
      entry,
      deadline,
      output: [],
      stderr: [],
      bytes: 0,
      stderrBytes: 0,
      invalid: false,
    }
    children.push(state)
    const signal = () => {
      try {
        signalOwned(state, 'SIGTERM')
      } catch {
        invalidateChild(state, 'child_error')
        state.entry.ownership_signal_failed = true
      }
    }
    child.stdout.on('data', (chunk) => {
      if (!captureChildOutput(state, 'stdout', chunk)) signal()
    })
    child.stderr.on('data', (chunk) => {
      if (!captureChildOutput(state, 'stderr', chunk)) signal()
    })
    state.closed = new Promise((done) => {
      child.once('error', () => invalidateChild(state, 'child_error'))
      child.once('close', (code, signal) => {
        entry.joined = true
        entry.exit_code = code
        entry.signal = signal
        done()
      })
    })
    child.stdin.on('error', () => invalidateChild(state, 'stdin_error'))
    child.stdin.end(input)
    assert.ok(Number.isInteger(child.pid) && child.pid > 1, 'Owned spawn failed')
    const observed = processIdentity(child.pid)
    if (observed) {
      assert.equal(observed.pgid, child.pid, 'Spawned process group was not observed')
      entry.pgid = observed.pgid
      entry.birth = observed.birth
      entry.birth_observation = 'observed'
    } else {
      assert.ok(absent(child.pid) && absent(child.pid, true), 'Unobserved process or group remains')
      entry.expected_pgid = child.pid
      entry.birth_observation = 'exited_before_birth_observation'
    }
    return state
  }
  function signalOwned(state, signal) {
    if (!Number.isInteger(state.entry.pid) || state.entry.pid <= 1 || absent(state.entry.pid, true))
      return
    assert.ok(
      state.entry.pgid === state.entry.pid && state.entry.birth,
      'Unproven process group ownership',
    )
    if (!absent(state.entry.pid)) {
      const current = processIdentity(state.entry.pid)
      if (!current) {
        assert.ok(absent(state.entry.pid), 'Owned process generation became unknown')
        if (absent(state.entry.pid, true)) return
      } else {
        assert.equal(current.pgid, state.entry.pgid, 'Owned process group changed')
        assert.equal(current.birth, state.entry.birth, 'Owned process generation changed')
      }
    }
    process.kill(-state.entry.pgid, signal)
  }
  async function joinChild(state, deadline = state.deadline, terminate = false) {
    let expired = false
    if (terminate) signalOwned(state, 'SIGTERM')
    while (
      (!state.entry.joined || !absent(state.entry.pid, true)) &&
      performance.now() < deadline &&
      (!isInterrupted() || terminate)
    )
      await delay(20)
    if (!state.entry.joined || !absent(state.entry.pid, true)) {
      expired = !terminate || performance.now() >= deadline
      signalOwned(state, 'SIGTERM')
      const grace = Math.min(absoluteDeadline - 5000, performance.now() + 5000)
      while ((!state.entry.joined || !absent(state.entry.pid, true)) && performance.now() < grace)
        await delay(20)
      if (!state.entry.joined || !absent(state.entry.pid, true)) signalOwned(state, 'SIGKILL')
    }
    const final = Math.min(absoluteDeadline, performance.now() + 5000)
    while ((!state.entry.joined || !absent(state.entry.pid, true)) && performance.now() < final)
      await delay(20)
    state.entry.group_gone =
      state.entry.joined && absent(state.entry.pid) && absent(state.entry.pid, true)
    state.entry.timed_out = expired
    assert.ok(
      state.entry.group_gone && !state.invalid && !expired,
      'Owned process did not join within budget',
    )
    return Buffer.concat(state.output)
  }

  return { start, signalOwned, joinChild }
}
