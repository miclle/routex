import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import {
  chmodSync,
  copyFileSync,
  existsSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const root = dirname(dirname(fileURLToPath(import.meta.url)))
const script = 'scripts/check-mod-tidy.sh'
const originals = [
  { name: 'go.mod', content: 'module example.test/tidy\n\ngo 1.27.1\n', mode: 0o644 },
  { name: 'go.sum', content: 'original fixture checksum\n', mode: 0o600 },
]

function fixture(t) {
  const base = realpathSync(mkdtempSync(join(tmpdir(), 'routex-mod-tidy-')))
  t.after(() => {
    rmSync(base, { recursive: true, force: true })
    assert.equal(existsSync(base), false)
  })
  for (const directory of ['bin', 'scripts']) mkdirSync(join(base, directory))
  copyFileSync(join(root, script), join(base, script))
  assert.deepEqual(readFileSync(join(base, script)), readFileSync(join(root, script)))
  assert.equal(lstatSync(base).isSymbolicLink(), false)
  for (const original of originals) {
    const path = join(base, original.name)
    writeFileSync(path, original.content)
    chmodSync(path, original.mode)
  }
  const fakeGo = join(base, 'bin/go')
  writeFileSync(
    fakeGo,
    `#!/bin/sh
set -eu
[ "$(pwd -P)" = "$ROUTEX_TIDY_FIXTURE" ] || exit 97
[ "$#" -eq 2 ] && [ "$1" = mod ] && [ "$2" = tidy ] || exit 98
printf '%s\\n' "$@" > invocation
umask > invocation-umask
case "$ROUTEX_TIDY_SCENARIO" in
  success) exit 0 ;;
  changes|failure)
    printf '%s\\n' 'changed fixture module' > go.mod
    printf '%s\\n' 'changed fixture checksum' > go.sum
    ;;
  *) exit 99 ;;
esac
[ "$ROUTEX_TIDY_SCENARIO" != failure ] || exit 23
`,
  )
  chmodSync(fakeGo, 0o755)
  return {
    base,
    run(scenario) {
      return spawnSync('bash', ['-c', 'umask 077; exec bash scripts/check-mod-tidy.sh'], {
        cwd: base,
        env: {
          ...process.env,
          PATH: `${join(base, 'bin')}:${process.env.PATH}`,
          ROUTEX_TIDY_FIXTURE: base,
          ROUTEX_TIDY_SCENARIO: scenario,
        },
        encoding: 'utf8',
        timeout: 5000,
      })
    },
  }
}

for (const { scenario, status } of [
  { scenario: 'success', status: 0 },
  { scenario: 'changes', status: 1 },
  { scenario: 'failure', status: 23 },
]) {
  test(`mod tidy preserves original bytes and modes under umask 077 on ${scenario}`, (t) => {
    const f = fixture(t)
    const result = f.run(scenario)
    assert.equal(result.error, undefined)
    assert.equal(result.signal, null)
    assert.equal(result.status, status, result.stderr)
    assert.equal(readFileSync(join(f.base, 'invocation'), 'utf8'), 'mod\ntidy\n')
    assert.equal(
      Number.parseInt(readFileSync(join(f.base, 'invocation-umask'), 'utf8').trim(), 8),
      0o77,
    )
    if (scenario === 'changes') {
      assert.match(result.stdout, /go\.mod or go\.sum is not tidy/)
    } else {
      assert.equal(result.stdout, '')
    }
    assert.equal(result.stderr, '')
    for (const original of originals) {
      const path = join(f.base, original.name)
      assert.equal(readFileSync(path, 'utf8'), original.content)
      const metadata = lstatSync(path)
      assert.equal(metadata.isFile(), true)
      assert.equal(metadata.isSymbolicLink(), false)
      assert.equal(metadata.mode & 0o777, original.mode)
      assert.equal(existsSync(`${path}.bak`), false)
    }
  })
}
