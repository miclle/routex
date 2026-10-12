import assert from 'node:assert/strict'
import test from 'node:test'
import { spawn } from 'node:child_process'
import { performance } from 'node:perf_hooks'
import { setTimeout as delay } from 'node:timers/promises'
import {
  applicationStdoutLimit,
  boundedBody,
  captureChildOutput,
  invalidateChild,
  requestPredicate,
  requireNativeAttempts,
  requireNoSecrets,
  requireQuota,
  requireQuotaExceeded,
  requireSameFacts,
} from './backup-restore-support.mjs'

const quota = () => ({
  activated: true,
  coverage_start: '2026-10-12T00:00:00Z',
  time_zone: 'UTC',
  five_hours: { covered: true, tokens_used: 5, tokens_held: 0, tokens_unknown: 0 },
  active: { tokens_held: 0 },
})

test('cold restore requires exact retained known quota, not a reset or an active hold', () => {
  requireQuota(quota(), 5)
  for (const mutate of [
    (q) => {
      q.activated = false
    },
    (q) => {
      q.coverage_start = null
    },
    (q) => {
      q.time_zone = 'Local'
    },
    (q) => {
      q.five_hours.covered = false
    },
    (q) => {
      q.five_hours.tokens_used = 0
    },
    (q) => {
      q.five_hours.tokens_held = 1
    },
    (q) => {
      q.five_hours.tokens_unknown = 1
    },
    (q) => {
      q.active.tokens_held = 1
    },
  ]) {
    const value = quota()
    mutate(value)
    assert.throws(() => requireQuota(value, 5))
  }
})

test('finite quota denial requires its native error code, not another HTTP 429 class', () => {
  requireQuotaExceeded({ error: { code: 'quota_exceeded' } })
  for (const value of [
    undefined,
    null,
    {},
    { error: {} },
    { error: { code: 'rate_limit_exceeded' } },
    { error: { code: 'concurrency_limit_exceeded' } },
    { error: { code: 'quota_history_incomplete' } },
    { error: { code: 'QUOTA_EXCEEDED' } },
    { error: { code: 'quota_exceeded ' } },
  ]) {
    assert.throws(() => requireQuotaExceeded(value), /Finite quota denial was not confirmed/)
  }
})

test('full SQL Call and Attempt captures must match independently', () => {
  const before = {
    calls: Buffer.from('original immutable call\n'),
    attempts: Buffer.from('original immutable attempt\n'),
  }
  requireSameFacts(before, {
    calls: Buffer.from(before.calls),
    attempts: Buffer.from(before.attempts),
  })
  for (const kind of ['calls', 'attempts']) {
    assert.throws(() => requireSameFacts(before, { ...before, [kind]: Buffer.from('changed\n') }))
    assert.throws(() => requireSameFacts(before, { ...before, [kind]: Buffer.alloc(0) }))
  }
  assert.throws(() => requireSameFacts(before, { calls: before.calls }))
})

test('native proof requires exact immutable Credential, snapshot and successful first attempt', () => {
  const row = 'snp_original\tcrd_original\tcompleted\tsuccess\t1\n'
  requireNativeAttempts(row, 'crd_original', 1)
  const altered = [
    row.replace('snp_original', ''),
    row.replace('crd_original', 'crd_other'),
    ...['unknown', 'handoff', 'blocked', 'incomplete', ''].map((value) =>
      row.replace('completed', value),
    ),
    row.replace('success', 'error'),
    row.replace('\t1\n', '\t2\n'),
    row + row,
  ]
  for (const value of altered) assert.throws(() => requireNativeAttempts(value, 'crd_original', 1))
})

test('backup secret scan rejects encoded SQL and journal material without secret diagnostics', () => {
  const representations = (material) => [
    material,
    Buffer.from(material.toString('hex')),
    Buffer.from(material.toString('hex').toUpperCase()),
    Buffer.from(material.toString('base64')),
  ]
  const secret = 'private-bootstrap-material-123456'
  const rootBytes = Buffer.from('00112233445566778899aabbccddeeff'.repeat(2), 'hex')
  const rootHex = rootBytes.toString('hex')
  const rootBase64 = rootBytes.toString('base64')
  for (const value of [secret, rootHex, rootBase64]) {
    requireNoSecrets(Buffer.from('ciphertext-only backup'), [value])
    const materials = [Buffer.from(value)]
    if (value !== secret) materials.push(rootBytes)
    for (const material of materials) {
      for (const representation of representations(material)) {
        assert.throws(
          () =>
            requireNoSecrets(
              Buffer.concat([Buffer.from('SQL-or-journal-prefix '), representation]),
              [value],
            ),
          (error) => {
            assert.equal(error.message, 'Backup disclosed transient material')
            return true
          },
        )
      }
    }
  }
  // Arbitrary text/base64 and noncanonical aliases are never decoded as roots.
  const ordinary = Buffer.from('ordinary-credential-value')
  requireNoSecrets(ordinary, [ordinary.toString('base64')])
  const noncanonical = `${rootBase64.slice(0, -2)}9=`
  assert.equal(Buffer.from(noncanonical, 'base64').equals(rootBytes), true)
  assert.notEqual(Buffer.from(noncanonical, 'base64').toString('base64'), noncanonical)
  requireNoSecrets(rootBytes, [noncanonical])
  requireNoSecrets(rootBytes, [rootHex.slice(1)])
})

test('operator readbacks accept only finite exact internally captured request identities', () => {
  assert.equal(
    requestPredicate(['req_first', 'req_second']),
    "request_id IN ('req_first','req_second')",
  )
  for (const ids of [
    [],
    ['req_first', 'req_first'],
    ["req_' OR 1=1"],
    ['req_first;DROP'],
    ['REQ_OTHER'],
    Array.from({ length: 9 }, (_, i) => `req_${i}`),
  ]) {
    assert.throws(() => requestPredicate(ids))
  }
})

test('bounded response consumes original bytes and rejects oversized or redirected bodies', async () => {
  assert.equal(
    (await boundedBody(new Response('{"retained":true}'), 64)).toString(),
    '{"retained":true}',
  )
  await assert.rejects(boundedBody(new Response('a'.repeat(65)), 64), /exceeds the bound/)
  const redirected = new Response('private')
  Object.defineProperty(redirected, 'redirected', { value: true })
  await assert.rejects(boundedBody(redirected), /redirect/)
})

function processAbsent(pid, group = false) {
  try {
    process.kill(group ? -pid : pid, 0)
    return false
  } catch (error) {
    if (error.code === 'ESRCH') return true
    throw new Error('Child absence cannot be confirmed')
  }
}

async function outputChild(stream, bytes, hold) {
  // A graceful cap-triggered TERM may exit zero; fixed failure metadata must survive it.
  const program = [
    "process.on('SIGTERM', () => process.exit(0))",
    'setInterval(() => {}, 1000)',
    'process[' + JSON.stringify(stream) + '].write(Buffer.alloc(' + bytes + ', 120), () => {',
    'if (!' + hold + ') process.exit(0)',
    '})',
  ].join('\n')
  const child = spawn(process.execPath, ['-e', program], {
    detached: true,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  assert.ok(Number.isInteger(child.pid) && child.pid > 1 && child.pid !== process.pid)
  const state = {
    entry: {
      stdout_bytes: 0,
      stderr_bytes: 0,
      stdout_limit: applicationStdoutLimit,
      stderr_limit: 64 << 10,
      failure_codes: [],
    },
    output: [],
    stderr: [],
    bytes: 0,
    stderrBytes: 0,
    invalid: false,
  }
  const signal = (value) => {
    if (!processAbsent(child.pid, true)) {
      try {
        process.kill(-child.pid, value)
      } catch (error) {
        if (error.code !== 'ESRCH') throw new Error('Owned child signal failed')
      }
    }
  }
  let joined = false
  const closed = new Promise((resolve, reject) => {
    child.once('error', () => reject(new Error('Child control spawn failed')))
    child.once('close', (code, receivedSignal) => {
      joined = true
      resolve({ code, signal: receivedSignal })
    })
  })
  for (const streamName of ['stdout', 'stderr']) {
    child[streamName].on('data', (chunk) => {
      if (!captureChildOutput(state, streamName, chunk)) signal('SIGTERM')
    })
  }
  let timer
  try {
    const terminal = await Promise.race([
      closed,
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error('Child control exceeded its bound')), 3000)
      }),
    ])
    return { state, terminal }
  } finally {
    clearTimeout(timer)
    if (!joined || !processAbsent(child.pid, true)) signal('SIGKILL')
    let joinTimer
    try {
      await Promise.race([
        closed,
        new Promise((_, reject) => {
          joinTimer = setTimeout(() => reject(new Error('Child control direct join failed')), 2000)
        }),
      ])
    } finally {
      clearTimeout(joinTimer)
    }
    const deadline = performance.now() + 1000
    while (!processAbsent(child.pid, true) && performance.now() < deadline) await delay(5)
    assert.ok(
      joined && processAbsent(child.pid) && processAbsent(child.pid, true),
      'Child control remains',
    )
  }
}

test(
  'exact application stdout and stderr caps preserve bounded output and positive child joins',
  { timeout: 15_000 },
  async () => {
    for (const stream of ['stdout', 'stderr']) {
      const cap = stream === 'stdout' ? applicationStdoutLimit : 64 << 10
      const { state, terminal } = await outputChild(stream, cap, false)
      assert.deepEqual(terminal, { code: 0, signal: null })
      assert.equal(state.invalid, false)
      assert.deepEqual(state.entry.failure_codes, [])
      assert.equal(state.entry[stream + '_bytes'], cap)
      assert.equal(Buffer.concat(stream === 'stdout' ? state.output : state.stderr).length, cap)
    }
  },
)

test(
  'application output overflow stays invalid even when owned TERM closes the child with zero',
  { timeout: 15_000 },
  async () => {
    for (const stream of ['stdout', 'stderr']) {
      const cap = stream === 'stdout' ? applicationStdoutLimit : 64 << 10
      const { state, terminal } = await outputChild(stream, cap + 1, true)
      assert.deepEqual(terminal, { code: 0, signal: null })
      assert.equal(state.invalid, true)
      assert.deepEqual(state.entry.failure_codes, [stream + '_limit'])
      assert.equal(state.entry[stream + '_bytes'], cap + 1)
      assert.ok(Buffer.concat(stream === 'stdout' ? state.output : state.stderr).length <= cap)
    }
  },
)

test('persisted child diagnostics contain only counts and fixed failure classes', () => {
  const secret = 'private-bootstrap-output-123456789'
  const state = {
    entry: {
      stdout_bytes: 0,
      stderr_bytes: 0,
      stdout_limit: 1,
      stderr_limit: 1,
      failure_codes: [],
    },
    output: [],
    stderr: [],
    bytes: 0,
    stderrBytes: 0,
    invalid: false,
  }
  assert.equal(captureChildOutput(state, 'stdout', Buffer.from(secret)), false)
  invalidateChild(state, 'child_error')
  invalidateChild(state, 'stdin_error')
  invalidateChild(state, 'child_error')
  assert.deepEqual(state.entry.failure_codes, ['stdout_limit', 'child_error', 'stdin_error'])
  requireNoSecrets(Buffer.from(JSON.stringify(state.entry)), [secret])
  assert.throws(
    () => invalidateChild(state, secret),
    (error) => error.message === 'Invalid child failure class',
  )
})
