import assert from 'node:assert/strict'

export const workMilliseconds = 600_000
export const cleanupMilliseconds = 90_000
export const applicationStdoutLimit = 1 << 20
export const responseLimit = 1 << 20
export const dumpLimit = 64 << 20

// Assertions use fixed messages: native responses and subprocess errors can contain secrets.
export function requireQuota(value, used) {
  assert.equal(value?.activated, true, 'Quota accounting is not active')
  assert.ok(value.coverage_start, 'Quota coverage is missing')
  assert.equal(value.time_zone, 'UTC', 'Quota calendar changed')
  const window = value.five_hours
  assert.equal(window?.covered, true, 'Quota coverage is incomplete')
  assert.equal(window.tokens_used, used, 'Quota usage changed')
  assert.equal(window.tokens_held, 0, 'Quota hold remains')
  assert.equal(window.tokens_unknown, 0, 'Quota usage is unknown')
  assert.equal(value.active?.tokens_held, 0, 'Active quota hold remains')
}

export function requireQuotaExceeded(value) {
  assert.equal(value?.error?.code, 'quota_exceeded', 'Finite quota denial was not confirmed')
}

export function requireSameFacts(before, after) {
  for (const kind of ['calls', 'attempts']) {
    assert.ok(
      Buffer.isBuffer(before[kind]) && Buffer.isBuffer(after[kind]),
      'Fact capture is missing',
    )
    assert.ok(
      before[kind].length > 0 && before[kind].equals(after[kind]),
      'Immutable SQL facts changed',
    )
  }
}

export function requireNativeAttempts(raw, credentialID, count) {
  const rows = raw
    .trim()
    .split('\n')
    .map((row) => row.split('\t'))
  assert.equal(rows.length, count, 'Native attempt census changed')
  for (const row of rows) {
    assert.equal(row.length, 5, 'Native attempt projection changed')
    assert.ok(row[0].length > 0, 'Attempt snapshot is missing')
    assert.equal(row[1], credentialID, 'Attempt Credential changed')
    assert.equal(row[2], 'completed', 'Native completion was not recorded')
    assert.equal(row[3], 'success', 'Final attempt did not succeed')
    assert.equal(row[4], '1', 'Unexpected retry attempt')
  }
}

export function requireNoSecrets(bytes, secrets) {
  for (const value of secrets) {
    assert.ok(typeof value === 'string' && value.length >= 16, 'Secret assertion input is invalid')
    const materials = [Buffer.from(value)]
    if (value.length === 64 && /^[0-9a-f]{64}$/.test(value)) {
      const decoded = Buffer.from(value, 'hex')
      if (decoded.length === 32 && decoded.toString('hex') === value) materials.push(decoded)
    } else if (value.length === 44 && /^[A-Za-z0-9+/]{43}=$/.test(value)) {
      const decoded = Buffer.from(value, 'base64')
      if (decoded.length === 32 && decoded.toString('base64') === value) materials.push(decoded)
    }
    for (const material of materials) {
      for (const representation of [
        material,
        Buffer.from(material.toString('hex')),
        Buffer.from(material.toString('hex').toUpperCase()),
        Buffer.from(material.toString('base64')),
      ]) {
        assert.ok(!bytes.includes(representation), 'Backup disclosed transient material')
      }
    }
  }
}

export function requestPredicate(ids) {
  assert.ok(Array.isArray(ids) && ids.length > 0 && ids.length <= 8, 'Invalid request census')
  assert.equal(new Set(ids).size, ids.length, 'Duplicate request identity')
  assert.ok(
    ids.every((id) => typeof id === 'string' && /^[a-z0-9_]{1,64}$/.test(id)),
    'Invalid request identity',
  )
  return `request_id IN (${ids.map((id) => `'${id}'`).join(',')})`
}

export async function boundedBody(response, limit = responseLimit) {
  assert.equal(response.redirected, false, 'HTTP redirect is forbidden')
  const reader = response.body?.getReader()
  assert.ok(reader, 'HTTP body is missing')
  const chunks = []
  let bytes = 0
  try {
    while (true) {
      const item = await reader.read()
      if (item.done) break
      bytes += item.value.byteLength
      assert.ok(bytes <= limit, 'HTTP response exceeds the bound')
      chunks.push(Buffer.from(item.value))
    }
    return Buffer.concat(chunks)
  } finally {
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}

// Persist only counts and fixed failure codes; child output can contain bootstrap secrets.
export function invalidateChild(state, code) {
  assert.ok(
    ['stdout_limit', 'stderr_limit', 'child_error', 'stdin_error'].includes(code),
    'Invalid child failure class',
  )
  state.invalid = true
  if (!state.entry.failure_codes.includes(code)) state.entry.failure_codes.push(code)
}

export function captureChildOutput(state, stream, chunk) {
  assert.ok(stream === 'stdout' || stream === 'stderr', 'Invalid child output stream')
  const stdout = stream === 'stdout'
  const count = stdout ? 'bytes' : 'stderrBytes'
  const chunks = stdout ? 'output' : 'stderr'
  state[count] += chunk.length
  state.entry[stream + '_bytes'] = state[count]
  if (state[count] <= state.entry[stream + '_limit']) {
    state[chunks].push(chunk)
    return true
  }
  invalidateChild(state, stream + '_limit')
  return false
}
