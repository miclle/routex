import assert from 'node:assert/strict'
import { chmod, mkdtemp, open, readFile, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { performance } from 'node:perf_hooks'
import test from 'node:test'
import {
  absent,
  createSupervisor,
  executableReceipt,
  sourceEntry,
} from './storage-native-acceptance-support.mjs'

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), 'routex-storage-control-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  return root
}

test('executable admission accepts normal bytes and rejects oversize, symlink and missing execute bit', async (t) => {
  const root = await fixture(t)
  const binary = join(root, 'binary')
  await writeFile(binary, Buffer.alloc(1024), { mode: 0o700 })
  assert.equal((await executableReceipt(binary)).sha256.length, 64)
  const alias = join(root, 'alias')
  await symlink(binary, alias)
  await assert.rejects(executableReceipt(alias))
  await chmod(binary, 0o600)
  await assert.rejects(executableReceipt(binary))
  await chmod(binary, 0o700)
  const file = await open(binary, 'r+')
  await file.truncate(256 << 20)
  await file.close()
  await assert.rejects(executableReceipt(binary))
})

test('source inventory binds symlink identity without following its outside content', async (t) => {
  const root = await fixture(t)
  const original = join(root, 'original')
  const alias = join(root, 'alias')
  await writeFile(original, 'first')
  await symlink('original', alias)
  const symbolic = await sourceEntry(alias, 'alias')
  const regular = await sourceEntry(original, 'original')
  assert.equal(symbolic[2], 'symlink')
  assert.equal(regular[2], 'file')
  await writeFile(original, 'second')
  assert.deepEqual(await sourceEntry(alias, 'alias'), symbolic)
  assert.notDeepEqual(await sourceEntry(original, 'original'), regular)
})

function supervisor(t, milliseconds = 3000) {
  const children = []
  const workDeadline = performance.now() + milliseconds
  const control = createSupervisor({
    repo: process.cwd(),
    fixtureEnv: process.env,
    children,
    workDeadline,
    absoluteDeadline: workDeadline + 15_000,
    remaining: (deadline) => {
      assert.ok(performance.now() < deadline)
      return deadline - performance.now()
    },
    interrupted: () => false,
  })
  t.after(async () => {
    for (const child of children.filter((value) => !value.entry.group_gone))
      await control.joinChild(child, performance.now() + 1000, true).catch(() => {})
    assert.ok(
      children.every((child) => child.entry.group_gone),
      'Test-owned group remains',
    )
  })
  return { ...control, children }
}

test('owned subprocess has observed process birth/group and direct join/absence', async (t) => {
  const control = supervisor(t)
  const child = control.start('ordinary', process.execPath, ['-e', 'process.stdout.write("ok")'])
  assert.equal((await control.joinChild(child)).toString(), 'ok')
  assert.equal(child.entry.exit_code, 0)
  assert.ok(child.entry.birth && child.entry.pgid === child.entry.pid)
  assert.ok(child.entry.joined && child.entry.group_gone)
  assert.ok(absent(child.entry.pid) && absent(child.entry.pgid, true))
})

test('output overflow fails while joining the exact owned subprocess', async (t) => {
  const control = supervisor(t)
  const child = control.start(
    'overflow',
    process.execPath,
    ['-e', 'process.stdout.write("x".repeat(4096));setInterval(()=>{},1000)'],
    { stdoutLimit: 128 },
  )
  await assert.rejects(control.joinChild(child))
  assert.deepEqual(child.entry.failure_codes, ['stdout_limit'])
  assert.ok(child.entry.joined && child.entry.group_gone)
  assert.ok(Buffer.concat(child.output).length <= 128)
})

test('deadline failure remains failed after exact process termination and closure', async (t) => {
  const control = supervisor(t, 150)
  const child = control.start('deadline', process.execPath, ['-e', 'setInterval(()=>{},1000)'])
  await assert.rejects(control.joinChild(child))
  assert.equal(child.entry.timed_out, true)
  assert.ok(child.entry.joined && child.entry.group_gone)
})

test('foreign or unknown process identity cannot be signaled', (t) => {
  const control = supervisor(t)
  assert.throws(() =>
    control.signalOwned({ entry: { pid: process.pid, pgid: process.pid, birth: null } }, 'SIGTERM'),
  )
})

test('short-lived commands retain direct joins even when birth was not observable', async (t) => {
  const control = supervisor(t)
  for (let index = 0; index < 5; index++) {
    const child = control.start('fast', '/usr/bin/true', [])
    await control.joinChild(child)
    assert.equal(child.entry.exit_code, 0)
    assert.ok(child.entry.joined && child.entry.group_gone)
    if (!child.entry.birth) {
      assert.equal(child.entry.birth_observation, 'exited_before_birth_observation')
      assert.equal(child.entry.pgid, null)
      assert.equal(child.entry.expected_pgid, child.entry.pid)
    }
  }
})
