import assert from 'node:assert/strict'
import test from 'node:test'
import { chmod, mkdir, mkdtemp, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import {
  assetsDigest,
  requireInputs,
  requireDeletionResponse,
  requirePendingSnapshot,
} from './storage-native-contracts.mjs'
import { executableReceipt } from './storage-native-acceptance-support.mjs'

test('asset census binds nested bytes, modes, additions and deletions', async () => {
  const repo = await mkdtemp(join(tmpdir(), 'routex-storage-asset-control-'))
  try {
    const root = join(repo, 'website/build')
    await mkdir(join(root, 'assets/nested'), { recursive: true })
    await writeFile(join(root, 'index.html'), '<html></html>')
    const asset = join(root, 'assets/nested/app.js')
    await writeFile(asset, 'original')
    await chmod(asset, 0o600)
    const original = await assetsDigest(repo)
    assert.deepEqual(
      original.rows.map(([path]) => path),
      ['website/build/assets/nested/app.js', 'website/build/index.html'],
    )
    await writeFile(asset, 'changed')
    assert.notEqual((await assetsDigest(repo)).digest, original.digest)
    await writeFile(asset, 'original')
    await chmod(asset, 0o644)
    assert.notEqual((await assetsDigest(repo)).digest, original.digest)
    await chmod(asset, 0o600)
    await writeFile(join(root, '.extra'), 'new')
    assert.notEqual((await assetsDigest(repo)).digest, original.digest)
    await rm(join(root, '.extra'))
    assert.equal((await assetsDigest(repo)).digest, original.digest)
    await rm(asset)
    await assert.rejects(assetsDigest(repo), /incomplete/)
  } finally {
    await rm(repo, { recursive: true, force: true })
  }
})

test('asset census rejects symlink roots and nested assets and requires the exact index', async () => {
  const repo = await mkdtemp(join(tmpdir(), 'routex-storage-asset-control-'))
  try {
    const root = join(repo, 'website/build')
    await mkdir(root, { recursive: true })
    await writeFile(join(root, 'index.html'), '<html></html>')
    await writeFile(join(repo, 'outside.js'), 'outside')
    await symlink(join(repo, 'outside.js'), join(root, 'app.js'))
    await assert.rejects(assetsDigest(repo), /Invalid embedded asset/)
    await rm(join(root, 'app.js'))
    await writeFile(join(root, 'app.js'), 'app')
    await rm(join(root, 'index.html'))
    await writeFile(join(root, 'other.html'), '<html></html>')
    await assert.rejects(assetsDigest(repo), /incomplete/)
    await rm(root, { recursive: true })
    await symlink(repo, root)
    await assert.rejects(assetsDigest(repo), /Invalid embedded asset root/)
  } finally {
    await rm(repo, { recursive: true, force: true })
  }
})

test('final executable census rejects late replacement, removed execute bits and symlinks', async () => {
  const root = await mkdtemp(join(tmpdir(), 'routex-storage-binary-control-'))
  try {
    const app = join(root, 'application')
    const helper = join(root, 'helper')
    await writeFile(app, 'qualified application', { mode: 0o700 })
    await writeFile(helper, 'qualified helper', { mode: 0o700 })
    const current = async () => ({
      source_digest: '1'.repeat(64),
      binary_sha256: (await executableReceipt(app)).sha256,
      helper_sha256: (await executableReceipt(helper)).sha256,
      assets_digest: '2'.repeat(64),
    })
    const original = await current()
    requireInputs(original, await current())
    for (const path of [app, helper]) {
      const content = path === app ? 'qualified application' : 'qualified helper'
      await writeFile(path, 'late replacement')
      await assert.rejects(async () => requireInputs(original, await current()), /changed/)
      await writeFile(path, content)
      requireInputs(original, await current())
      await chmod(path, 0o600)
      await assert.rejects(current(), /Invalid executable/)
      await chmod(path, 0o700)
      await rm(path)
      await symlink(path === app ? helper : app, path)
      await assert.rejects(current(), /Invalid executable/)
      await rm(path)
      await writeFile(path, content, { mode: 0o700 })
    }
    for (const field of Object.keys(original)) {
      assert.throws(() => requireInputs(original, { ...original, [field]: 'f'.repeat(64) }))
      assert.throws(() => requireInputs({ ...original, [field]: undefined }, original))
    }
  } finally {
    await rm(root, { recursive: true, force: true })
  }
})

const uploaded = {
  id: 'obj_personal_image',
  name: 'image.png',
  mime: 'image/png',
  size: 68,
  created_at: '2026-01-01T00:00:00Z',
  state: 'ready',
}

test('normal DELETE requires the exact current uploaded target and acknowledged pending state', () => {
  const response = { ...uploaded, state: 'delete_pending' }
  requireDeletionResponse(response, uploaded)
  for (const field of ['id', 'name', 'mime', 'size', 'created_at'])
    assert.throws(() => requireDeletionResponse({ ...response, [field]: 'other' }, uploaded))
  for (const state of ['ready', 'deleted', undefined])
    assert.throws(() => requireDeletionResponse({ ...response, state }, uploaded))
  assert.throws(() => requireDeletionResponse(null, uploaded))
})

test('DELETE retains the exact creation instant across offsets without losing fractional precision', () => {
  const original = { ...uploaded, created_at: '2026-01-01T00:00:00.123456789Z' }
  const response = {
    ...original,
    created_at: '2026-01-01T08:00:00.123456789+08:00',
    state: 'delete_pending',
  }
  requireDeletionResponse(response, original)
  requireDeletionResponse(
    { ...response, created_at: '2025-12-31T18:30:00.123456789-05:30' },
    original,
  )
  requireDeletionResponse(
    { ...response, created_at: '2026-01-01T08:00:00.123450000+08:00' },
    { ...original, created_at: '2026-01-01T00:00:00.12345Z' },
  )
  for (const [created_at, equivalent] of [
    ['1969-12-31T23:59:59.000000001Z', '1970-01-01T07:59:59.000000001+08:00'],
    ['2024-02-29T00:00:00Z', '2024-02-28T18:30:00-05:30'],
  ])
    requireDeletionResponse({ ...response, created_at: equivalent }, { ...original, created_at })
  for (const created_at of [
    '2026-01-01T08:00:00.123456790+08:00',
    '2026-01-01T08:00:00.123457789+08:00',
    '2026-01-01T08:00:00.123456789+07:00',
    '2026-02-30T08:00:00.123456789+08:00',
    '2025-02-29T08:00:00.123456789+08:00',
    '2026-01-01T08:00:00.1234567890+08:00',
    '2026-01-01T08:00:00.123456789+24:00',
    '2026-01-01T08:00:00.123456789+08:60',
    '2026-01-01T08:00:00.123456789',
    '',
    undefined,
    NaN,
  ])
    assert.throws(() => requireDeletionResponse({ ...response, created_at }, original))
})

const objects = () =>
  Array.from({ length: 4 }, (_, index) => ({
    ID: `obj_exact_${index}`,
    OwnerKind: index < 2 ? 'user' : 'project',
    OwnerID: index < 2 ? 'usr_exact' : 'prj_exact',
    RevisionID: 'str_exact',
    Purpose: 'attachment',
    Name: index % 2 ? 'document.pdf' : 'image.png',
    MIME: index % 2 ? 'application/pdf' : 'image/png',
    Size: 68,
    SHA256: 'a'.repeat(64),
    UploadConfirmed: true,
    version_id: `original-${index}`,
    CreatorUserID: null,
    CreatorMembershipID: null,
    ExpiresAt: null,
    CreatedAt: '2026-01-01T00:00:00Z',
    NextCleanupAt: '2026-01-01T01:00:00Z',
    State: 'ready',
  }))

test('stopped-process snapshot requires exact persisted pending scope and original versions', () => {
  const original = objects()
  const pending = { objects: original.map((row) => ({ ...row, State: 'delete_pending' })) }
  requirePendingSnapshot(pending, original)
  for (const [field, value] of [
    ['State', 'ready'],
    ['State', 'deleted'],
    ['OwnerKind', 'project'],
    ['OwnerID', 'usr_other'],
    ['RevisionID', 'str_other'],
    ['version_id', 'later-version'],
    ['SHA256', 'b'.repeat(64)],
    ['Size', 69],
    ['UploadConfirmed', false],
    ['CreatedAt', '2026-01-02T00:00:00Z'],
    ['ExpiresAt', '2026-01-01T01:00:00Z'],
    ['NextCleanupAt', null],
  ]) {
    const altered = structuredClone(pending)
    altered.objects[0][field] = value
    assert.throws(() => requirePendingSnapshot(altered, original))
  }
  assert.throws(() => requirePendingSnapshot({ objects: pending.objects.slice(1) }, original))
  assert.throws(() =>
    requirePendingSnapshot(
      { objects: [pending.objects[0], ...pending.objects.slice(0, 3)] },
      original,
    ),
  )
  const ambiguousOriginal = [original[0], ...original.slice(0, 3)]
  assert.throws(() => requirePendingSnapshot(pending, ambiguousOriginal))
})
