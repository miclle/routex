import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import {
  copyFileSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  statSync,
  symlinkSync,
  writeFileSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const root = dirname(dirname(fileURLToPath(import.meta.url)))

function fixture(t) {
  const base = realpathSync(
    mkdtempSync(join(tmpdir(), 'routex-integration-entry-')),
  )
  t.after(() => rmSync(base, { recursive: true, force: true }))
  for (const dir of ['bin', 'scripts', 'temporary']) mkdirSync(join(base, dir))
  copyFileSync(
    join(root, 'scripts/test-integration.sh'),
    join(base, 'scripts/test-integration.sh'),
  )
  writeFileSync(
    join(base, 'compiler.mjs'),
    `import { writeFileSync } from 'node:fs'
const args = process.argv.slice(2)
writeFileSync(process.env.ROUTEX_ENTRY_TRACE, JSON.stringify(args))
if (process.env.ROUTEX_ENTRY_COMPILE_FAILURE) {
  console.error('controlled compilation failure')
  process.exit(23)
}
const output = args[args.indexOf('-o') + 1]
writeFileSync(output, '#!/bin/sh\\nexec "$ROUTEX_ENTRY_NODE" "$ROUTEX_ENTRY_WORKER" "$@"\\n', { mode: 0o755 })
`,
  )
  writeFileSync(
    join(base, 'worker.mjs'),
    `import { mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
const args = process.argv.slice(2)
const directory = args[args.indexOf('-private-dir') + 1]
mkdirSync(join(directory, 'logs'))
writeFileSync(join(directory, 'logs/postgres.log'), 'x'.repeat(1024 * 1024 + 1) + '\\ncontrolled failure beyond terminal prefix\\n')
writeFileSync(join(directory, 'logs/status.json'), JSON.stringify({ completed: true, exit_code: 7 }))
writeFileSync(process.env.ROUTEX_ENTRY_DIRECTORY, directory)
process.exit(7)
`,
  )
  writeFileSync(
    join(base, 'bin/go'),
    '#!/bin/sh\nexec "$ROUTEX_ENTRY_NODE" "$ROUTEX_ENTRY_COMPILER" "$@"\n',
    { mode: 0o755 },
  )
  const env = {
    ...process.env,
    PATH: `${join(base, 'bin')}:${process.env.PATH}`,
    TMPDIR: join(base, 'temporary'),
    ROUTEX_ENTRY_NODE: process.execPath,
    ROUTEX_ENTRY_COMPILER: join(base, 'compiler.mjs'),
    ROUTEX_ENTRY_WORKER: join(base, 'worker.mjs'),
    ROUTEX_ENTRY_TRACE: join(base, 'compile-trace.json'),
    ROUTEX_ENTRY_DIRECTORY: join(base, 'actual-directory'),
  }
  delete env.ROUTEX_TEST_RUNNER_PRIVATE_DIR
  delete env.ROUTEX_ENTRY_COMPILE_FAILURE
  return {
    base,
    env,
    run(overrides = {}) {
      return spawnSync('bash', ['scripts/test-integration.sh'], {
        cwd: base,
        env: { ...env, ...overrides },
        encoding: 'utf8',
        timeout: 5000,
      })
    },
  }
}

test('integration entry preserves the default private directory and worker failure', (t) => {
  const f = fixture(t)
  const result = f.run()
  assert.equal(result.status, 7, result.stderr)
  const directory = readFileSync(f.env.ROUTEX_ENTRY_DIRECTORY, 'utf8')
  assert.equal(dirname(directory), f.env.TMPDIR)
  assert.match(directory, /routex-test-runner\.[A-Za-z0-9]+$/)
  assert.equal(statSync(directory).mode & 0o777, 0o700)
})

test('declared integration directory retains complete original failure evidence', (t) => {
  const f = fixture(t)
  const directory = join(f.base, 'declared-evidence')
  const result = f.run({ ROUTEX_TEST_RUNNER_PRIVATE_DIR: directory })
  assert.equal(result.status, 7, result.stderr)
  assert.equal(readFileSync(f.env.ROUTEX_ENTRY_DIRECTORY, 'utf8'), directory)
  assert.equal(statSync(directory).mode & 0o777, 0o700)
  const raw = readFileSync(join(directory, 'logs/postgres.log'), 'utf8')
  assert.ok(raw.length > 1024 * 1024)
  assert.ok(raw.endsWith('controlled failure beyond terminal prefix\n'))
  assert.deepEqual(
    JSON.parse(readFileSync(join(directory, 'logs/status.json'), 'utf8')),
    {
      completed: true,
      exit_code: 7,
    },
  )
})

test('declared integration directory retains compilation failure without dispatch', (t) => {
  const f = fixture(t)
  const directory = join(f.base, 'compile-evidence')
  const result = f.run({
    ROUTEX_TEST_RUNNER_PRIVATE_DIR: directory,
    ROUTEX_ENTRY_COMPILE_FAILURE: '1',
  })
  assert.equal(result.status, 23, result.stderr)
  assert.equal(
    readFileSync(join(directory, 'build.log'), 'utf8'),
    'controlled compilation failure\n',
  )
  assert.equal(existsSync(f.env.ROUTEX_ENTRY_DIRECTORY), false)
  assert.equal(existsSync(join(directory, 'runner')), false)
})

for (const scenario of [
  'existing directory',
  'symlink',
  'relative path',
  'parent traversal',
]) {
  test(`integration entry refuses ${scenario} before compilation and preserves foreign files`, (t) => {
    const f = fixture(t)
    const foreign = join(f.base, 'foreign')
    mkdirSync(foreign)
    writeFileSync(join(foreign, 'preserved'), 'unrelated evidence')
    let directory = foreign
    if (scenario === 'symlink') {
      directory = join(f.base, 'linked')
      symlinkSync(foreign, directory)
    } else if (scenario === 'relative path') directory = 'relative-evidence'
    else if (scenario === 'parent traversal')
      directory = `${f.base}/temporary/../escaped-evidence`
    const result = f.run({ ROUTEX_TEST_RUNNER_PRIVATE_DIR: directory })
    assert.equal(result.status, 1, result.stderr)
    assert.equal(existsSync(f.env.ROUTEX_ENTRY_TRACE), false)
    assert.equal(existsSync(f.env.ROUTEX_ENTRY_DIRECTORY), false)
    assert.equal(
      readFileSync(join(foreign, 'preserved'), 'utf8'),
      'unrelated evidence',
    )
  })
}

test('CI preserves only explicit integration logs and lifecycle receipts after failure', () => {
  const workflow = readFileSync(join(root, '.github/workflows/ci.yml'), 'utf8')
  const step = workflow.split(
    '      - name: Preserve complete integration diagnostics\n',
  )[1]
  assert.ok(step)
  const upload = step.split(
    '      - name: Test authentication across process restarts',
  )[0]
  assert.ok(
    upload.includes(
      "if: ${{ always() && env.ROUTEX_TEST_RUNNER_PRIVATE_DIR != '' }}",
    ),
    'pre-declaration failure must not select root-level files',
  )
  assert.ok(upload.includes('timeout-minutes: 5'))
  assert.ok(upload.includes('retention-days: 1'))
  const paths = [
    ...upload.matchAll(
      /\$\{\{ env\.ROUTEX_TEST_RUNNER_PRIVATE_DIR \}\}\/([^\n]+)/g,
    ),
  ]
    .map((match) => match[1])
    .sort()
  assert.deepEqual(paths, [
    'build.log',
    'logs/compose-down.log',
    'logs/compose-logs.log',
    'logs/compose-up.log',
    'logs/diagnostics.log',
    'logs/mysql-port.log',
    'logs/mysql.log',
    'logs/nonmatrix.log',
    'logs/oidc-build.log',
    'logs/owned-processes.jsonl',
    'logs/postgres-port.log',
    'logs/postgres.log',
    'logs/status.json',
    'oidc-owners/mysql.json',
    'oidc-owners/postgres.json',
  ])
})
