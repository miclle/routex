// Opt-in dummy Vault wire discovery. It does not invoke a RouteX cleanup.
import assert from 'node:assert/strict'
import { createHash, randomBytes } from 'node:crypto'
import { chmod, lstat, mkdir, mkdtemp, readFile, realpath, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { performance } from 'node:perf_hooks'
import { fileURLToPath } from 'node:url'
import { requireNoSecrets } from './backup-restore-support.mjs'
import { absent, createSupervisor, sourceEntry } from './storage-native-acceptance-support.mjs'
import {
  requireNativeFiles,
  requireOwnedNetwork,
  requireOwnedVault,
  requireRequestCensus,
} from './vault-cleanup-wire-support.mjs'

assert.deepEqual(
  process.argv.slice(2),
  ['--exclusive-validation-lane'],
  'Explicit serial lane required',
)
process.umask(0o077)
const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const output = await mkdtemp(join(await realpath(tmpdir()), 'routex-vault-cleanup-wire-'))
await mkdir(join(output, 'commands'), { mode: 0o700 })
const image =
  'hashicorp/vault:2.2.0@sha256:e0acd3c681bdf57f002d7214e5d050b876e1704678005c6b0406dd96a65be919'
const run = randomBytes(16).toString('hex')
const project = `routex-vault-wire-${run}`
const uid = process.getuid()
assert.ok(Number.isInteger(uid) && uid > 0, 'Nonroot host UID required')
const workDeadline = performance.now() + 300_000
const absoluteDeadline = workDeadline + 70_000
let cleanupDeadline
let interrupted = false
let stage = 'preflight'
let failedStage
let closureUnknown = false
let composeDispatched = false
let nativeDispatched = false
let container
let network
let before
let expected
const children = []
const hash = (bytes) => createHash('sha256').update(bytes).digest('hex')
const save = async (name, value) =>
  writeFile(
    join(output, name),
    Buffer.isBuffer(value) ? value : `${JSON.stringify(value, null, 2)}\n`,
    { mode: 0o600, flag: 'wx' },
  )
const remaining = (deadline = cleanupDeadline ?? workDeadline) => {
  assert.ok(
    (!interrupted || cleanupDeadline !== undefined) && performance.now() < deadline,
    'Original deadline or cancellation',
  )
  return Math.max(1, deadline - performance.now())
}
for (const signal of ['SIGINT', 'SIGTERM'])
  process.once(signal, () => {
    interrupted = true
  })
const { start, joinChild, signalOwned } = createSupervisor({
  repo,
  fixtureEnv: process.env,
  children,
  workDeadline,
  absoluteDeadline,
  remaining,
  interrupted: () => interrupted,
})
async function command(name, args, timeout = 15_000, limit = 4 << 20, env = process.env) {
  stage = name
  const state = start(name, args[0], args.slice(1), {
    env,
    deadline: Math.min(cleanupDeadline ?? workDeadline, performance.now() + timeout),
    stdoutLimit: limit,
  })
  try {
    const value = await joinChild(state)
    await save(`commands/${children.length}-${name}.stdout`, value)
    await save(`commands/${children.length}-${name}.stderr`, Buffer.concat(state.stderr))
    assert.equal(state.entry.exit_code, 0, 'Owned command failed')
    return value
  } finally {
    await save(`commands/${children.length}-${name}.terminal.json`, state.entry)
  }
}
const docker = (name, args, timeout, limit) => command(name, ['docker', ...args], timeout, limit)
async function source() {
  const names = (
    await command('source-list', [
      'git',
      'ls-files',
      '-z',
      '--cached',
      '--others',
      '--exclude-standard',
    ])
  )
    .toString()
    .split('\0')
    .filter(Boolean)
    .sort()
  const rows = []
  for (const name of [...new Set(names)]) rows.push(await sourceEntry(join(repo, name), name))
  return { rows, sha256: hash(Buffer.from(JSON.stringify(rows))) }
}
const config = join(output, 'vault.hcl')
const client = join(output, 'wire-client')
const compose = ['compose', '-p', project, '-f', join(output, 'compose.yaml')]
async function inspectVault({ stopped = false } = {}) {
  const value = JSON.parse((await docker('inspect-vault', ['inspect', container])).toString())[0]
  return { value, identity: requireOwnedVault(value, expected, { stopped }) }
}
async function inspectNetwork(members) {
  const value = JSON.parse(
    (await docker('inspect-network', ['network', 'inspect', network])).toString(),
  )[0]
  requireOwnedNetwork(value, { project, id: network }, members)
  return value
}
async function discoverOwned() {
  const ids = (
    await docker('owned-container-census', [
      'container',
      'ls',
      '-aq',
      '--no-trunc',
      '--filter',
      `label=com.docker.compose.project=${project}`,
    ])
  )
    .toString()
    .trim()
    .split('\n')
    .filter(Boolean)
  assert.equal(ids.length, 1, 'Owned container census incomplete')
  assert.match(ids[0], /^[a-f0-9]{64}$/)
  container = ids[0]
  const networks = (
    await docker('owned-network-census', [
      'network',
      'ls',
      '-q',
      '--no-trunc',
      '--filter',
      `label=com.docker.compose.project=${project}`,
    ])
  )
    .toString()
    .trim()
    .split('\n')
    .filter(Boolean)
  assert.equal(networks.length, 1, 'Owned network census incomplete')
  network = networks[0]
  assert.equal(
    (
      await docker('owned-volume-census', [
        'volume',
        'ls',
        '-q',
        '--filter',
        `label=com.docker.compose.project=${project}`,
      ])
    ).length,
    0,
    'Unexpected persistent volume',
  )
  expected.id = container
  const observed = await inspectVault()
  expected.birth = observed.identity.birth
  expected.pid = observed.identity.pid
  await inspectNetwork([container])
  await save('owned-start.json', {
    project,
    run,
    image,
    expected,
    container: observed.value,
    network,
  })
}
try {
  before = await source()
  await save('source-before.json', before)
  await command(
    'build-native-client',
    [
      'go',
      'build',
      '-trimpath',
      '-ldflags',
      `-X main.BuildRunID=${run} -X main.BuildSourceManifest=${before.sha256} -X main.BuildHostUID=${uid}`,
      '-o',
      client,
      './scripts/vault-cleanup-wire-client',
    ],
    120_000,
    1 << 20,
    { ...process.env, GOOS: 'linux', GOARCH: 'arm64', CGO_ENABLED: '0' },
  )
  await chmod(client, 0o555)
  const clientHash = hash(await readFile(client))
  await command('native-build-metadata', ['go', 'version', '-m', client])
  await docker('pull-official-vault', ['pull', '--platform', 'linux/arm64', image], 120_000)
  const imageID = (await docker('image-id', ['image', 'inspect', '--format', '{{.Id}}', image]))
    .toString()
    .trim()
  expected = { id: '', image: imageID, run, project, uid, config, client }
  await writeFile(
    config,
    'disable_mlock = true\nui = false\napi_addr = "http://127.0.0.1:8200"\nstorage "file" { path = "/vault/file" }\nlistener "tcp" {\n  address = "127.0.0.1:8200"\n  tls_disable = true\n}\ntelemetry {\n  prometheus_retention_time = "120s"\n  disable_hostname = true\n}\n',
    { mode: 0o644, flag: 'wx' },
  )
  await chmod(config, 0o644)
  // Explicit tmpfs overrides the official image's two declared volumes.
  // No token or published port enters Compose or the container configuration.
  await save(
    'compose.yaml',
    Buffer.from(
      `services:\n  vault:\n    image: ${image}\n    platform: linux/arm64\n    user: '${uid}:${uid}'\n    entrypoint: [vault]\n    command: [server, '-config=/config/vault.hcl']\n    environment:\n      HOME: /tmp\n    labels:\n      routex.acceptance.run: ${run}\n    volumes:\n      - '${config}:/config/vault.hcl:ro'\n      - '${client}:/opt/routex/vault-wire-client:ro'\n    tmpfs:\n      - /vault/file:uid=${uid},gid=${uid},mode=0700,size=128m\n      - /vault/logs:uid=${uid},gid=${uid},mode=0700,size=16m\n      - /tmp:mode=1777,size=16m\n    read_only: true\n    cap_drop: [ALL]\n    security_opt: [no-new-privileges:true]\n    restart: 'no'\n    healthcheck:\n      disable: true\nnetworks:\n  default:\n    internal: true\n`,
    ),
  )
  composeDispatched = true
  await docker('compose-create', [...compose, 'up', '-d', '--no-build'], 60_000)
  await discoverOwned()
  const mountedHash = (
    await docker('native-mounted-hash', [
      'exec',
      '--user',
      `${uid}:${uid}`,
      container,
      'sha256sum',
      '/opt/routex/vault-wire-client',
    ])
  ).toString()
  assert.equal(
    mountedHash,
    `${clientHash}  /opt/routex/vault-wire-client\n`,
    'Mounted native client changed',
  )
  nativeDispatched = true
  const native = JSON.parse(
    (
      await docker(
        'native-discovery',
        ['exec', '--user', `${uid}:${uid}`, container, '/opt/routex/vault-wire-client'],
        75_000,
      )
    ).toString(),
  )
  assert.deepEqual(native, {
    result: 'actual_dummy_wire_observed',
    source_manifest: before.sha256,
    run,
    application_runtime_acceptance: false,
  })
  await inspectVault()
  await save('native-result.json', {
    ...native,
    client_sha256: clientHash,
    image,
    image_id: imageID,
  })
} catch {
  failedStage = stage
} finally {
  cleanupDeadline = Math.min(absoluteDeadline, performance.now() + 70_000)
  try {
    for (const state of children.filter((item) => !item.entry.group_gone)) {
      signalOwned(state, 'SIGTERM')
      await joinChild(state, cleanupDeadline, true)
    }
    if (composeDispatched && !container) await discoverOwned()
    if (container) {
      const current = await inspectVault()
      await save('before-stop.json', current.value)
      await inspectNetwork([container])
      // Docker's archive endpoint does not capture these tmpfs files. Read
      // only the completed native manifest's fixed names, checking each byte
      // count/hash before any stop. A missing native terminal preserves state.
      const captured = join(output, 'vault-private')
      await mkdir(captured, { mode: 0o700 })
      const nativeDir = join(captured, `discovery-${run}`)
      if (nativeDispatched) {
        await mkdir(nativeDir, { mode: 0o700 })
        const nativePath = `/vault/logs/discovery-${run}`
        const rawTerminal = await docker('capture-native-terminal', [
          'exec',
          '--user',
          `${uid}:${uid}`,
          container,
          'cat',
          `${nativePath}/native-terminal.json`,
        ])
        const terminal = JSON.parse(rawTerminal.toString())
        const files = requireNativeFiles(terminal, { run, source: before.sha256 })
        await writeFile(join(nativeDir, 'native-terminal.json'), rawTerminal, {
          flag: 'wx',
          mode: 0o600,
        })
        for (const file of files) {
          const raw = await docker('capture-native-file', [
            'exec',
            '--user',
            `${uid}:${uid}`,
            container,
            'cat',
            `${nativePath}/${file.name}`,
          ])
          assert.equal(raw.length, file.bytes, 'Native artifact byte count changed')
          assert.equal(hash(raw), file.sha256, 'Native artifact hash changed')
          await writeFile(join(nativeDir, file.name), raw, { flag: 'wx', mode: 0o600 })
        }
        if (files.some((file) => file.name === 'metrics-before.prom')) {
          const audit = await docker('capture-native-audit', [
            'exec',
            '--user',
            `${uid}:${uid}`,
            container,
            'cat',
            '/vault/logs/audit.jsonl',
          ])
          await writeFile(join(captured, 'audit.jsonl'), audit, { flag: 'wx', mode: 0o600 })
        }
      }
      if (!failedStage) {
        const materials = JSON.parse(await readFile(join(nativeDir, 'private-material.json')))
        for (const name of ['metrics-before.prom', 'metrics-after.prom'])
          requireNoSecrets(await readFile(join(nativeDir, name)), materials)
        requireNoSecrets(await readFile(join(captured, 'audit.jsonl')), materials)
        requireRequestCensus(JSON.parse(await readFile(join(nativeDir, 'http-census.json'))))
        const terminal = JSON.parse(await readFile(join(nativeDir, 'native-terminal.json')))
        assert.equal(terminal.passed, true, 'Native completion missing')
        assert.equal(terminal.run, run, 'Native run changed')
        assert.equal(terminal.source_manifest, before.sha256, 'Native source changed')
        const info = await lstat(join(nativeDir, 'private-material.json'))
        assert.ok(
          info.isFile() && (info.mode & 0o777) === 0o600 && info.nlink === 1,
          'Private material ownership invalid',
        )
      }
      await save(
        'vault-server.log',
        await docker('capture-server-log', ['logs', '--timestamps', container]),
      )
      await docker('stop-vault', ['stop', '--timeout', '5', container])
      await save('before-removal.json', (await inspectVault({ stopped: true })).value)
      await inspectNetwork([])
      await docker('remove-vault', ['rm', container])
      await docker('remove-network', ['network', 'rm', network])
      for (const kind of ['container', 'network', 'volume']) {
        const ids = await docker(`closed-${kind}`, [
          kind,
          'ls',
          ...(kind === 'container' ? ['-a'] : []),
          '-q',
          '--filter',
          `label=com.docker.compose.project=${project}`,
        ])
        assert.equal(ids.length, 0, 'Owned resource remains')
      }
    }
    for (const state of children)
      assert.ok(
        state.entry.group_gone && state.entry.joined && absent(state.entry.pid, true),
        'Recorded group remains',
      )
    if (before) {
      const after = await source()
      await save('source-after.json', after)
      assert.deepEqual(after, before, 'Source changed during discovery')
    }
  } catch {
    closureUnknown = true
  }
  await save(
    'processes.json',
    children.map((state) => state.entry),
  )
  await save('terminal.json', {
    status:
      failedStage || closureUnknown
        ? 'failed_discovery_preserved'
        : 'dummy_wire_observed_closed_requires_independent_review',
    failed_stage: failedStage ?? null,
    closure_unknown: closureUnknown,
    project,
    run,
    container_id: container ?? null,
    network_id: network ?? null,
    published_ports: [],
    source_sha256: before?.sha256 ?? null,
    application_runtime_acceptance: false,
  })
  console.log(
    JSON.stringify({
      result: failedStage || closureUnknown ? 'failed' : 'dummy_wire_observed',
      output,
      application_runtime_acceptance: false,
      closure_unknown: closureUnknown,
    }),
  )
  process.exitCode = failedStage || closureUnknown ? 1 : 0
}
