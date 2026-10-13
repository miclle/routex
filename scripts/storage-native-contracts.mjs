import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { lstat, readdir, readFile } from 'node:fs/promises'
import { join } from 'node:path'

const hash = (bytes) => createHash('sha256').update(bytes).digest('hex')

export async function assetsDigest(repo) {
  const root = await lstat(join(repo, 'website/build'))
  assert.ok(root.isDirectory() && !root.isSymbolicLink(), 'Invalid embedded asset root')
  const rows = []
  const walk = async (relative) => {
    for (const name of (await readdir(join(repo, relative))).sort()) {
      const path = `${relative}/${name}`
      const info = await lstat(join(repo, path))
      if (info.isDirectory() && !info.isSymbolicLink()) await walk(path)
      else {
        assert.ok(info.isFile() && !info.isSymbolicLink(), 'Invalid embedded asset')
        rows.push([path, info.mode & 0o777, hash(await readFile(join(repo, path)))])
      }
    }
  }
  await walk('website/build')
  assert.ok(
    rows.length > 1 && rows.some(([path]) => path === 'website/build/index.html'),
    'Embedded asset set incomplete',
  )
  return { digest: hash(Buffer.from(JSON.stringify(rows))), rows }
}

export function requireInputs(expected, current) {
  for (const field of ['source_digest', 'binary_sha256', 'helper_sha256', 'assets_digest']) {
    assert.match(expected?.[field] ?? '', /^[a-f0-9]{64}$/, 'Invalid input receipt')
    assert.equal(current?.[field], expected[field], `Qualified ${field} changed`)
  }
}

function recordedInstant(value) {
  assert.equal(typeof value, 'string', 'Recorded timestamp missing')
  const match =
    /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|([+-])(\d{2}):(\d{2}))$/.exec(value)
  assert.ok(match, 'Recorded timestamp is not RFC3339Nano')
  const [, calendar, fraction = '', zone, sign, hours, minutes] = match
  const epoch = Date.parse(`${calendar}Z`)
  assert.ok(
    Number.isFinite(epoch) && new Date(epoch).toISOString().slice(0, 19) === calendar,
    'Recorded timestamp calendar is invalid',
  )
  let offset = 0
  if (zone !== 'Z') {
    assert.ok(Number(hours) <= 23 && Number(minutes) <= 59, 'Recorded timestamp offset is invalid')
    offset = (Number(hours) * 60 + Number(minutes)) * (sign === '+' ? 1 : -1)
  }
  // Date parses only whole seconds here; fractional evidence stays exact.
  return (
    BigInt(epoch) * 1_000_000n + BigInt(fraction.padEnd(9, '0')) - BigInt(offset) * 60_000_000_000n
  )
}

export function requireDeletionResponse(actual, uploaded) {
  assert.ok(actual && uploaded, 'Deletion response missing')
  for (const field of ['id', 'name', 'mime', 'size'])
    assert.equal(actual[field], uploaded[field], 'Deletion response target changed')
  assert.equal(
    recordedInstant(actual.created_at),
    recordedInstant(uploaded.created_at),
    'Deletion response creation instant changed',
  )
  assert.equal(actual.state, 'delete_pending', 'Deletion intent was not acknowledged')
}

export function requirePendingSnapshot(actual, original) {
  assert.ok(Array.isArray(original) && original.length === 4, 'Original scope census changed')
  assert.equal(actual?.objects?.length, original.length, 'Pending intent census changed')
  const seen = new Set()
  for (const expected of original) {
    assert.ok(expected.ID && !seen.has(expected.ID), 'Original object identity duplicated')
    seen.add(expected.ID)
    const matches = actual.objects.filter((row) => row.ID === expected.ID)
    assert.equal(matches.length, 1, 'Exact pending intent missing or duplicated')
    const row = matches[0]
    assert.equal(row.State, 'delete_pending', 'Pending deletion was not persisted')
    assert.ok(expected.version_id, 'Original recorded version missing')
    for (const field of [
      'ID',
      'OwnerKind',
      'OwnerID',
      'RevisionID',
      'Purpose',
      'Name',
      'MIME',
      'Size',
      'SHA256',
      'UploadConfirmed',
      'version_id',
      'CreatorUserID',
      'CreatorMembershipID',
      'ExpiresAt',
      'CreatedAt',
    ])
      assert.equal(row[field], expected[field], 'Pending deletion immutable identity changed')
    assert.ok(Number.isFinite(Date.parse(row.NextCleanupAt)), 'Pending cleanup schedule missing')
  }
}
