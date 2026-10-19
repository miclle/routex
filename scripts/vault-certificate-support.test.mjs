import assert from 'node:assert/strict'
import { randomUUID } from 'node:crypto'
import test from 'node:test'
import { certificateAudit } from './vault-certificate-support.mjs'

function packet() {
  const run = '1'.repeat(32), probe = '2'.repeat(32)
  const hashes = Object.fromEntries(['writer', 'reader', `routex-writer-${run}`, `routex-reader-${run}`].map((s, i) => [s, `hmac-sha256:${String(i).repeat(64)}`]))
  const auth = (role) => ({ entity_id: `${role}-entity`, metadata: { cert_name: role, common_name: `routex-${role}-${run}` }, token_policies: [`routex-cert-${role}-${run}`], policies: [`routex-cert-${role}-${run}`] })
  const rows = []
  const pair = (path, operation, role, error) => {
    const request = { id: randomUUID(), path, operation, data: { name: hashes[role] } }
    const login = path.startsWith('auth/')
    rows.push({ type: 'request', request, auth: login ? undefined : auth(role) })
    rows.push({ type: 'response', request, auth: login ? undefined : auth(role), response: login ? error ? { data: { error: `hmac-sha256:${"a".repeat(64)}` } } : { auth: auth(role) } : {}, error: login ? undefined : error })
  }
  const login = `auth/routex-cert-${run.slice(0, 16)}/login`
  for (const role of ['writer', 'writer', 'writer', 'reader', 'reader']) pair(login, 'update', role)
  pair(login, 'update', 'reader', 'certificate not authorized')
  pair(`kv/data/discovery/routex-probe-${probe}`, 'create', 'writer')
  for (let i = 0; i < 2; i++) {
    pair(`kv/data/discovery/routex-probe-${probe}`, 'read', 'reader')
    pair(`kv/destroy/discovery/routex-probe-${probe}`, 'update', 'writer', 'permission denied')
  }
  return { rows, expected: { run, probe, hashes } }
}
const bytes = (rows) => Buffer.from(`${rows.map((v) => JSON.stringify(v)).join('\n')}\n`)

test('certificate audit requires paired remote principals and exact operation census', () => {
  const { rows, expected } = packet()
  assert.equal(certificateAudit(bytes(rows), expected).paired_calls, 11)
  for (const mutate of [
    (r) => r.pop(),
    (r) => r.push(r[0]),
    (r) => { r.find((v) => v.response?.auth?.metadata.cert_name === "reader").response.auth.entity_id = 'writer-entity' },
    (r) => { r.find((v) => v.response?.auth).response.auth.metadata.common_name = expected.hashes['reader'] },
    (r) => { r.find((v) => v.error === 'permission denied').error = undefined },
    (r) => { r.find((v) => v.auth).auth.token_policies.push('root') },
  ]) {
    const changed = structuredClone(rows)
    mutate(changed)
    assert.throws(() => certificateAudit(bytes(changed), expected))
  }
  assert.throws(() => certificateAudit(bytes(rows).subarray(0, -1), expected))
  assert.throws(() => certificateAudit(Buffer.from('{"passed":true}\n'), expected))
})
