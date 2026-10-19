import assert from 'node:assert/strict'

// This reader is deliberately scoped to the pinned disposable Vault fixture.
// It does not accept markers or local request tuples as remote identity proof.
export function certificateAudit(raw, { run, probe, hashes }) {
  assert.ok(Buffer.isBuffer(raw) && raw.length > 0 && raw.length <= 4 << 20)
  assert.equal(raw.at(-1), 10, 'Incomplete audit tail')
  assert.match(run, /^[a-f0-9]{32}$/)
  assert.match(probe, /^[a-f0-9]{32}$/)
  const pairs = new Map()
  const mount = `auth/routex-cert-${run.slice(0, 16)}/login`
  const path = `kv/data/discovery/routex-probe-${probe}`
  const destroy = `kv/destroy/discovery/routex-probe-${probe}`
  for (const line of raw.toString('utf8').trimEnd().split('\n')) {
    const row = JSON.parse(line)
    const request = row.request
    assert.ok(request && typeof request.path === 'string')
    if (request.path !== mount && !request.path.includes(`routex-probe-${probe}`)) continue
    assert.ok([mount, path, destroy].includes(request.path), 'Unexpected probe operation')
    assert.match(request.id, /^[a-f0-9-]{36}$/)
    assert.ok(['request', 'response'].includes(row.type))
    const pair = pairs.get(request.id) ?? {}
    assert.ok(!pair[row.type], 'Duplicate audit event')
    pair[row.type] = row
    pairs.set(request.id, pair)
  }
  assert.equal(pairs.size, 11, 'Incomplete or additional native calls')
  const entities = new Map()
  const counts = { writer: 0, reader: 0, denied_login: 0, write: 0, read: 0, denied_destroy: 0 }
  const identity = (auth) => {
    assert.ok(auth && typeof auth.entity_id === 'string' && auth.entity_id.length > 0)
    const role = ['writer', 'reader'].find((role) => auth.metadata?.cert_name === role)
    assert.ok(role, 'Unbound certificate role')
    assert.equal(auth.metadata.common_name, `routex-${role}-${run}`)
    assert.deepEqual(auth.token_policies, [`routex-cert-${role}-${run}`])
    assert.deepEqual(auth.policies, auth.token_policies)
    if (entities.has(role)) assert.equal(auth.entity_id, entities.get(role))
    else entities.set(role, auth.entity_id)
    return role
  }
  for (const pair of pairs.values()) {
    assert.ok(pair.request && pair.response, 'Missing audit pair')
    const a = pair.request, b = pair.response
    assert.equal(a.request.path, b.request.path)
    assert.equal(a.request.operation, b.request.operation)
    if (a.request.path === mount) {
      if (b.response?.data?.error) {
        assert.ok(!b.error)
        assert.match(b.response.data.error, /^hmac-sha256:[a-f0-9]{64}$/)
        assert.equal(a.request.data?.name, hashes.reader)
        assert.ok(!b.response?.auth, 'Denied login issued authority')
        counts.denied_login++
      } else {
        const role = identity(b.response?.auth)
        assert.equal(a.request.data?.name, hashes[role])
        counts[role]++
      }
    } else {
      const role = identity(a.auth)
      assert.equal(identity(b.auth), role)
      if (a.request.path === destroy) {
        assert.equal(role, 'writer')
        assert.equal(a.request.operation, 'update')
        assert.match(b.error ?? '', /permission denied/i)
        counts.denied_destroy++
      } else {
        assert.ok(!b.error, 'Native KV operation failed')
        if (a.request.operation === 'create') { assert.equal(role, 'writer'); counts.write++ }
        else { assert.equal(a.request.operation, 'read'); assert.equal(role, 'reader'); counts.read++ }
      }
    }
  }
  assert.deepEqual(counts, { writer: 3, reader: 2, denied_login: 1, write: 1, read: 2, denied_destroy: 2 })
  assert.equal(entities.size, 2)
  assert.notEqual(entities.get('writer'), entities.get('reader'), 'Remote principals are not distinct')
  return { paired_calls: pairs.size, counts, distinct_remote_principals: true }
}
