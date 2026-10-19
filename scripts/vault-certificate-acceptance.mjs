// Opt-in saved-certificate acceptance in one owned, serial database lane.
import assert from 'node:assert/strict'
import { createHash, randomBytes } from 'node:crypto'
import { lstat, mkdir, readFile, realpath, rm, writeFile } from 'node:fs/promises'
import { homedir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { performance } from 'node:perf_hooks'
import { fileURLToPath } from 'node:url'
import { absent, createSupervisor, sourceEntry } from './storage-native-acceptance-support.mjs'

import { requireNativeFiles, requireOwnedNetwork } from './vault-cleanup-wire-support.mjs'
import { certificateAudit } from './vault-certificate-support.mjs'

const args = process.argv.slice(2)
assert.equal(args.length, 3, 'Explicit exclusive lane and database driver required')
assert.equal(args[0], '--exclusive-validation-lane')
assert.equal(args[1], '--driver')
const driver = args[2]
assert.ok(['postgres', 'mysql'].includes(driver), 'Unsupported database')
process.umask(0o077)
const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const run = randomBytes(16).toString('hex')
const project = `routex-vault-certificate-${run}`
const parent = join(homedir(), '.local/share/routex/acceptance/saved-certificate')
await mkdir(parent, { recursive: true, mode: 0o700 })
const output = join(await realpath(parent), project)
await mkdir(output, { mode: 0o700 })
for (const name of ['commands', 'artifacts', 'native', 'preflight', 'gateway-data'])
  await mkdir(join(output, name), { mode: 0o700 })
const uid = process.getuid()
assert.ok(Number.isInteger(uid) && uid > 0, 'Nonroot host required')
const started = new Date().toISOString()
const references = {
  vault: 'hashicorp/vault:2.2.0@sha256:e0acd3c681bdf57f002d7214e5d050b876e1704678005c6b0406dd96a65be919',
  postgres: 'postgres:18-bookworm@sha256:67ba68a22dc7d5af3627b7e0f33d10bbefad07562ff9703b3a6c2de247649129',
  mysql: 'mysql:8.4@sha256:24989179f58856db763f418010c1169fecc944e3ec1ea7e921572d83eb070f70',
  base: 'debian:bookworm-slim@sha256:a1b86db52ce3daef089e45aabe36dfec4091f82464c25c1fdcf03de197cbe82a',
}
const hash = (value) => createHash('sha256').update(value).digest('hex')
const save = (name, value) =>
  writeFile(join(output, name), Buffer.isBuffer(value) ? value : `${JSON.stringify(value, null, 2)}\n`, {
    mode: 0o600,
    flag: 'wx',
  })
const children = []
const workDeadline = performance.now() + 20 * 60_000
const absoluteDeadline = workDeadline + 90_000
let cleanupDeadline
let interrupted = false
let stage = 'source'
let failure
let closure = false
let sourceBefore
const generations = new Map()
let journalBefore
let auditReadback
let beforeGateway
let plannedRestart = false
const owned = new Map()
const images = new Map()
let network
const captured = new Map()
const phases = new Map()
const remaining = () => {
  const deadline = cleanupDeadline ?? workDeadline
  assert.ok((!interrupted || cleanupDeadline !== undefined) && performance.now() < deadline)
  return deadline - performance.now()
}
for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, () => { interrupted = true })
const { start, joinChild, signalOwned } = createSupervisor({
  repo, fixtureEnv: process.env, children, workDeadline, absoluteDeadline, remaining,
  interrupted: () => interrupted,
})
async function command(name, argv, timeout = 20_000, env = process.env, allow = false) {
  stage = name
  remaining()
  const index = children.length + 1
  const state = start(name, argv[0], argv.slice(1), {
    env, deadline: Math.min(cleanupDeadline ?? workDeadline, performance.now() + timeout),
    stdoutLimit: 8 << 20,
  })
  await save(`commands/${index}-${name}.start.json`, state.entry)
  let value
  try {
    value = await joinChild(state)
    assert.ok(allow || state.entry.exit_code === 0, 'Owned command failed')
    return value
  } finally {
    await save(`commands/${index}-${name}.stdout`, value ?? Buffer.concat(state.output))
    await save(`commands/${index}-${name}.stderr`, Buffer.concat(state.stderr))
    await save(`commands/${index}-${name}.terminal.json`, state.entry)
  }
}
const docker = (name, argv, timeout, allow) => command(name, ['docker', ...argv], timeout, process.env, allow)
async function source() {
  const raw = await command('source-list', ['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard'])
  const rows = []
  for (const name of [...new Set(raw.toString().split('\0').filter(Boolean))].sort())
    rows.push(await sourceEntry(join(repo, name), name))
  return { rows, sha256: hash(Buffer.from(JSON.stringify(rows))) }
}
const compose = ['compose', '-p', project, '-f', join(output, 'compose.yaml')]
async function inspect(role, stopped) {
  const id = owned.get(role)
  assert.match(id ?? '', /^[a-f0-9]{64}$/)
  const value = JSON.parse(await docker(`inspect-${role}`, ['inspect', id]))[0]
  assert.equal(value.Id, id)
  assert.equal(value.Image, images.get(role))
  assert.equal(value.Name, `/${project}-${role}-1`)
  assert.equal(value.Config.Labels['com.docker.compose.project'], project)
  assert.equal(value.Config.Labels['com.docker.compose.service'], role)
  assert.equal(value.Config.Labels['routex.acceptance.run'], run)
  assert.equal(value.HostConfig.Privileged, false)
  assert.equal(value.HostConfig.RestartPolicy.Name, 'no')
  assert.equal(value.State.OOMKilled, false)
  assert.ok(!value.HostConfig.PortBindings || !Object.keys(value.HostConfig.PortBindings).length)
  assert.ok(value.Mounts.every((item) => ['bind', 'tmpfs'].includes(item.Type)))
  if (role !== driver) {
    assert.equal(value.Config.User, `${uid}:${uid}`)
    assert.equal(value.HostConfig.ReadonlyRootfs, true)
    assert.deepEqual(value.HostConfig.CapDrop, ['ALL'])
    assert.deepEqual(value.HostConfig.CapAdd ?? [], [])
  }
  assert.deepEqual(value.HostConfig.SecurityOpt, ['no-new-privileges:true'])
  assert.equal(value.HostConfig.LogConfig.Type, role === 'gateway' ? 'local' : 'none')
  if (role === 'gateway') assert.deepEqual(value.HostConfig.LogConfig.Config, { 'max-size': '1m', 'max-file': '2' })
  const expectedBinds = role === 'gateway' ? [[join(output, 'input/config.yaml'), '/input/config.yaml'], [join(output, 'gateway-data'), '/state']] : role === 'vault' ? [[join(output, 'vault.hcl'), '/config/vault.hcl'], [join(output, 'input'), '/input'], [join(output, 'artifacts'), '/opt/routex']] : role === driver ? [[join(output, 'input/database-password'), '/input/database-password'], ...(driver === 'mysql' ? [[join(output, 'input/database-root-password'), '/input/database-root-password']] : [])] : []
  assert.deepEqual(value.Mounts.filter((item) => item.Type === 'bind').map((item) => { assert.equal(item.RW, role === 'gateway' && item.Destination === '/state'); return [item.Source, item.Destination] }).sort(), expectedBinds.sort())
  assert.equal(value.HostConfig.NetworkMode, ['vault', 'gateway'].includes(role) ? `container:${owned.get('anchor')}` : `${project}_default`)
  if (stopped !== undefined) assert.equal(value.State.Running, !stopped)
  assert.ok(value.State.Running ? value.State.Pid > 0 : value.State.Pid === 0)
  const original = generations.get(role)
  if (original) {
    assert.deepEqual(value.Config, original.Config)
    assert.deepEqual(value.HostConfig, original.HostConfig)
    assert.deepEqual(value.Mounts, original.Mounts)
    if (!(role === 'gateway' && plannedRestart)) assert.equal(value.State.StartedAt, original.State.StartedAt)
  } else generations.set(role, value)
  if (role === 'gateway' && beforeGateway) {
    assert.deepEqual(value.Config, beforeGateway.Config)
    assert.deepEqual(value.HostConfig, beforeGateway.HostConfig)
    assert.deepEqual(value.Mounts, beforeGateway.Mounts)
    if (!plannedRestart) assert.equal(value.State.StartedAt, beforeGateway.State.StartedAt)
  }
  return value
}
async function discover(requireRunning = true) {
  const ids = (await docker('owned-census', ['ps', '-aq', '--no-trunc', '--filter', `label=com.docker.compose.project=${project}`])).toString().trim().split('\n').filter(Boolean)
  assert.ok(ids.length > 0 && ids.length <= 4)
  for (const id of ids) {
    const value = JSON.parse(await docker('capture-owned', ['inspect', id]))[0]
    const role = value.Config.Labels['com.docker.compose.service']
    assert.ok(['anchor', 'vault', 'gateway', driver].includes(role) && !owned.has(role))
    owned.set(role, value.Id)
  }
  const networks = (await docker('network-census', ['network', 'ls', '-q', '--no-trunc', '--filter', `label=com.docker.compose.project=${project}`])).toString().trim().split('\n').filter(Boolean)
  assert.equal(networks.length, 1)
  network = networks[0]
  await save('owned.json', { containers: Object.fromEntries(owned), network })
  for (const role of owned.keys()) await save(`owned-${role}.json`, await inspect(role, requireRunning ? false : undefined))
  const net = JSON.parse(await docker('initial-network', ['network', 'inspect', network]))[0]
  requireOwnedNetwork(net, { id: network, project }, [owned.get(driver), owned.get('anchor')])
  await save('network-initial.json', net)
}
async function capturePhase(phase) {
  const native = `/vault/logs/certificate-${run}`
  const terminalRaw = await docker(`capture-${phase}-terminal`, ['exec', '--user', `${uid}:${uid}`, owned.get('vault'), 'cat', `${native}/${phase}-terminal.json`])
  const terminal = JSON.parse(terminalRaw)
  assert.equal(terminal.run, run)
  assert.equal(terminal.source_manifest, sourceBefore.sha256)
  assert.equal(terminal.phase, phase)
  assert.ok(Array.isArray(terminal.files) && terminal.files.length <= 512)
  const files = [...terminal.files, { name: `${phase}-terminal.json`, bytes: terminalRaw.length, sha256: hash(terminalRaw) }]
  for (const file of files) {
    assert.match(file.name, /^(?:before|rotate|after)-[a-z0-9-]+\.(?:json|jsonl)$/)
    assert.match(file.sha256, /^[a-f0-9]{64}$/)
    assert.ok(Number.isSafeInteger(file.bytes) && file.bytes >= 0 && file.bytes <= 4 << 20)
    if (captured.has(file.name)) { assert.deepEqual(captured.get(file.name), file); continue }
    const raw = file.name === `${phase}-terminal.json` ? terminalRaw : await docker('capture-native-file', ['exec', '--user', `${uid}:${uid}`, owned.get('vault'), 'cat', `${native}/${file.name}`])
    assert.equal(raw.length, file.bytes)
    assert.equal(hash(raw), file.sha256)
    await save(`native/${file.name}`, raw)
    captured.set(file.name, file)
  }
  phases.set(phase, terminal)
  return terminal
}
async function capturePreflight() {
  const base = `/vault/logs/discovery-${run}`
  const raw = await docker('preflight-terminal', ['exec', '--user', `${uid}:${uid}`, owned.get('vault'), 'cat', `${base}/native-terminal.json`])
  const terminal = JSON.parse(raw)
  const files = requireNativeFiles(terminal, { run, source: sourceBefore.sha256 })
  await save('preflight/native-terminal.json', raw)
  for (const file of files) {
    const bytes = await docker('preflight-file', ['exec', '--user', `${uid}:${uid}`, owned.get('vault'), 'cat', `${base}/${file.name}`])
    assert.equal(bytes.length, file.bytes)
    assert.equal(hash(bytes), file.sha256)
    await save(`preflight/${file.name}`, bytes)
  }
  assert.equal(terminal.passed, true)
}
async function captureJournal(label) {
  const path = join(output, 'gateway-data/calls.db')
  const info = await lstat(path)
  assert.ok(info.isFile() && !info.isSymbolicLink() && (info.mode & 0o777) === 0o600 && info.size > 0 && info.size <= 64 << 20)
  const raw = await readFile(path)
  assert.equal(raw.length, info.size)
  const receipt = { bytes: raw.length, sha256: hash(raw), inode: info.ino, device: info.dev, mode: info.mode & 0o777 }
  await save(`journal-${label}.db`, raw)
  await save(`journal-${label}.json`, receipt)
  return receipt
}
async function nativePhase(phase) {
  let commandError
  try { await docker(`native-${phase}`, ['exec', '--user', `${uid}:${uid}`, owned.get('vault'), '/opt/routex/certificate-fixture', phase], phase === 'rotate' ? 420_000 : 150_000) }
  catch (error) { commandError = error }
  const terminal = await capturePhase(phase)
  stage = `native-${phase}:${terminal.last_stage}`
  assert.ok(!commandError && terminal.passed === true, 'Native phase refused')
}
try {
  sourceBefore = await source()
  await save('source-before.json', sourceBefore)
  const head = (await command('source-head', ['git', 'rev-parse', 'HEAD'])).toString().trim()
  const flags = `-X main.BuildRunID=${run} -X main.BuildSource=${head} -X main.BuildSourceManifest=${sourceBefore.sha256} -X main.BuildHostUID=${uid} -X main.BuildStarted=${started}`
  await command('build-host-preparer', ['go', 'build', '-trimpath', '-ldflags', flags, '-o', join(output, 'prepare-pki'), './scripts/vault-certificate-fixture'], 120_000)
  await command('prepare-fresh-pki', [join(output, 'prepare-pki'), 'prepare', join(output, 'input')])
  await command('fresh-assets', ['npm', '--prefix', 'website', 'run', 'build'], 120_000)
  const linux = { ...process.env, GOOS: 'linux', GOARCH: 'arm64', CGO_ENABLED: '0' }
  for (const [name, pkg, ldflags] of [
    ['routex', './cmd/routex', `-X main.CommitID=${head}`],
    ['wire-client', './scripts/vault-cleanup-wire-client', flags],
    ['certificate-fixture', './scripts/vault-certificate-fixture', flags],
  ]) {
    await command(`build-${name}`, ['go', 'build', '-trimpath', '-ldflags', ldflags, '-o', join(output, 'artifacts', name), pkg], 180_000, linux)
    await save(`build-${name}.json`, { source: head, source_manifest: sourceBefore.sha256, sha256: hash(await readFile(join(output, 'artifacts', name))) })
  }
  for (const role of ['vault', driver, 'base']) {
    await docker(`pull-${role}`, ['pull', '--platform', 'linux/arm64', references[role]], 180_000)
    images.set(role, (await docker(`image-${role}`, ['image', 'inspect', '--format', '{{.Id}}', references[role]])).toString().trim())
  }
  await writeFile(join(output, '.dockerignore'), '*\n!artifacts/\n!artifacts/**\n!Dockerfile\n')
  await writeFile(join(output, 'Dockerfile'), `FROM ${references.base}\nCOPY --chmod=0555 artifacts/ /opt/routex/\nUSER ${uid}:${uid}\nCMD ["sleep","1800"]\n`)
  const tag = `${project}:fresh`
  await docker('build-capsule', ['build', '--no-cache', '--network=none', '--platform', 'linux/arm64', '-t', tag, output], 180_000)
  const capsule = (await docker('capsule-id', ['image', 'inspect', '--format', '{{.Id}}', tag])).toString().trim()
  images.set('anchor', capsule); images.set('gateway', capsule)
  const password = randomBytes(32).toString('hex')
  await save('input/database-password', Buffer.from(password))
  await save('input/database-root-password', Buffer.from(randomBytes(32).toString('hex')))
  const oldKey = randomBytes(32).toString('base64'), nextKey = randomBytes(32).toString('base64')
  const dsn = driver === 'postgres' ? `host=postgres port=5432 user=routex_cert password=${password} dbname=routex_cert_${run} sslmode=disable connect_timeout=2` : `routex_cert:${password}@tcp(mysql:3306)/routex_cert_${run}?charset=utf8mb4&parseTime=True&loc=UTC&timeout=2s&readTimeout=10s&writeTimeout=10s`
  const config = (retired) => `addr: "0.0.0.0:19001"\ndriver: ${driver}\ndsn: "${dsn}"\nallow_private_upstreams: true\nevent_queue_path: "/state/calls.db"\nencryption_keyring:\n  write_key_id: ${retired ? 'fixture_next' : 'fixture_old'}\n  keys:\n${retired ? '' : `    - {id: fixture_old, key: "${oldKey}"}\n`}    - {id: fixture_next, key: "${nextKey}"}\n`
  await save('input/config.yaml', Buffer.from(config(false)))
  await save('input/original-config.yaml', Buffer.from(config(false)))
  await save('vault.hcl', Buffer.from(`disable_mlock=true\nui=false\nstorage "file" { path="/vault/file" }\nlistener "tcp" {\n address="127.0.0.1:8200"\n tls_disable=true\n}\nlistener "tcp" {\n address="127.0.0.1:8201"\n tls_cert_file="/input/server.pem"\n tls_key_file="/input/server.key"\n tls_client_ca_file="/input/client-ca.pem"\n}\ntelemetry {\n prometheus_retention_time="120s"\n disable_hostname=true\n}\n`))
  const common = `    platform: linux/arm64\n    restart: 'no'\n    logging: {driver: none}\n    labels: {routex.acceptance.run: ${run}}\n    security_opt: ['no-new-privileges:true']\n`
  const gatewayCommon = common.replace('logging: {driver: none}', "logging: {driver: local, options: {max-size: 1m, max-file: '2'}}")
  const confined = `    user: '${uid}:${uid}'\n    read_only: true\n    cap_drop: [ALL]\n    healthcheck: {disable: true}\n`
  const database = driver === 'postgres' ? `  postgres:\n    image: ${references.postgres}\n${common}    environment:\n      POSTGRES_USER: routex_cert\n      POSTGRES_DB: routex_cert_${run}\n      POSTGRES_PASSWORD_FILE: /input/database-password\n    volumes: ['${output}/input/database-password:/input/database-password:ro']\n    tmpfs: ['/var/lib/postgresql:size=512m','/var/run/postgresql:size=8m']\n    healthcheck: {test: [CMD-SHELL, 'pg_isready -h 127.0.0.1 -U routex_cert -d routex_cert_${run}'], interval: 1s, timeout: 2s, retries: 90}\n` : `  mysql:\n    image: ${references.mysql}\n${common}    environment:\n      MYSQL_USER: routex_cert\n      MYSQL_DATABASE: routex_cert_${run}\n      MYSQL_PASSWORD_FILE: /input/database-password\n      MYSQL_ROOT_PASSWORD_FILE: /input/database-root-password\n    volumes: ['${output}/input/database-password:/input/database-password:ro','${output}/input/database-root-password:/input/database-root-password:ro']\n    tmpfs: ['/var/lib/mysql:size=768m','/var/run/mysqld:size=16m','/tmp:size=64m']\n    command: [mysqld,'--skip-log-bin']\n    healthcheck: {test: [CMD-SHELL, 'MYSQL_PWD="$$(cat /input/database-root-password)" mysql --protocol=TCP --host=127.0.0.1 --user=root --database=mysql --execute="SELECT 1" >/dev/null 2>&1'], interval: 1s, timeout: 3s, retries: 90}\n`
  await save('compose.yaml', Buffer.from(`services:\n${database}  anchor:\n    image: ${tag}\n${common}${confined}    init: true\n    command: [sleep,'1800']\n  vault:\n    image: ${references.vault}\n${common}${confined}    network_mode: service:anchor\n    entrypoint: [vault]\n    command: [server,'-config=/config/vault.hcl']\n    environment: {HOME: /tmp}\n    volumes: ['${output}/vault.hcl:/config/vault.hcl:ro','${output}/input:/input:ro','${output}/artifacts:/opt/routex:ro']\n    tmpfs: ['/vault/file:uid=${uid},gid=${uid},mode=0700,size=128m','/vault/logs:uid=${uid},gid=${uid},mode=0700,size=64m','/tmp:mode=1777,size=16m']\n  gateway:\n    image: ${tag}\n${gatewayCommon}${confined}    network_mode: service:anchor\n    depends_on:\n      ${driver}: {condition: service_healthy}\n      anchor: {condition: service_started}\n    command: [/opt/routex/routex,'-c',/input/config.yaml]\n    volumes: ['${output}/input/config.yaml:/input/config.yaml:ro','${output}/gateway-data:/state:rw']\n    tmpfs: ['/tmp:uid=${uid},gid=${uid},mode=0700,size=256m']\nnetworks:\n  default:\n    internal: true\n`))
  await save('build-input.json', { run, project, driver, uid, source: head, source_manifest: sourceBefore.sha256, started, images: Object.fromEntries(images), certificate_scope: 'saved identity, root rotation and restart; not external application adoption' })
  await docker('compose-up', [...compose, 'up', '-d', '--no-build'], 180_000)
  await discover()
  assert.equal(owned.size, 4)
  beforeGateway = await inspect('gateway')
  await docker('native-wire-preflight', ['exec', '--user', `${uid}:${uid}`, owned.get('vault'), '/opt/routex/wire-client'], 90_000)
  await capturePreflight()
  await nativePhase('before')
  await nativePhase('rotate')
  await docker('stop-gateway-for-restart', ['stop', '--time', '10', owned.get('gateway')])
  await save('gateway-before-restart.json', await inspect('gateway', true))
  journalBefore = await captureJournal('before-restart')
  await writeFile(join(output, 'input/config.yaml'), config(true), { mode: 0o600 })
  await save('input/replacement-only-config.yaml', Buffer.from(config(true)))
  plannedRestart = true
  await docker('start-same-gateway', ['start', owned.get('gateway')])
  const restarted = await inspect('gateway')
  assert.notEqual(restarted.State.StartedAt, beforeGateway.State.StartedAt)
  await save('gateway-after-restart.json', restarted)
  beforeGateway = restarted
  generations.set('gateway', restarted)
  plannedRestart = false
  await nativePhase('after')
} catch (error) {
  failure = { stage, reason: error instanceof Error ? error.message : 'Unclassified refusal' }
} finally {
  cleanupDeadline = Math.min(absoluteDeadline, performance.now() + 90_000)
  try {
    if (!owned.size) {
      const ids = (await docker('cleanup-discovery', ['ps', '-aq', '--filter', `label=com.docker.compose.project=${project}`])).toString().trim()
      if (ids) await discover(false)
    }
    if (owned.has('vault')) {
      const audit = await docker('final-vault-audit', ['exec', '--user', `${uid}:${uid}`, owned.get('vault'), 'cat', '/vault/logs/audit.jsonl'], 20_000, true)
      if (audit.length) await save('vault-final-audit.jsonl', audit)
    }
    if (owned.has('gateway')) {
      try {
        await inspect('gateway')
        const log = await docker('gateway-startup-log', ['logs', owned.get('gateway')], 20_000, true)
        await save('private-gateway-log.stdout', log)
        await save('private-gateway-log.stderr', Buffer.concat(children.at(-1).stderr))
      } catch (error) { failure ??= { stage: 'gateway-log-capture', reason: error.message } }
    }
    const cleanupErrors = []
    for (const role of ['gateway', 'vault', driver, 'anchor'].filter((role) => owned.has(role))) {
      try {
        const current = await inspect(role)
        if (current.State.Running) await docker(`stop-${role}`, ['stop', '--time', '10', owned.get(role)], 20_000)
        else failure ??= { stage: `unexpected-exit-${role}`, reason: 'Owned process exited before explicit stop' }
        const stopped = await inspect(role, true)
        await save(`stopped-${role}.json`, stopped)
        if (role === 'gateway' && journalBefore) {
          try {
            const after = await captureJournal('after-restart')
            assert.equal(after.inode, journalBefore.inode)
            assert.equal(after.device, journalBefore.device)
          } catch (error) { failure ??= { stage: 'durable-journal-readback', reason: error.message } }
        }
        if (!(role === 'anchor' ? [0, 143].includes(stopped.State.ExitCode) : stopped.State.ExitCode === 0)) failure ??= { stage: `exit-${role}`, reason: 'Owned process exit was not clean' }
        await docker(`remove-${role}`, ['rm', owned.get(role)])
      } catch (error) { cleanupErrors.push({ role, reason: error.message }) }
    }
    await save('container-cleanup.json', cleanupErrors)
    assert.equal(cleanupErrors.length, 0, 'Owned container closure incomplete')
    if (network) {
      const value = JSON.parse(await docker('network-before-remove', ['network', 'inspect', network]))[0]
      assert.equal(value.Name, `${project}_default`)
      assert.equal(value.Internal, true)
      assert.equal(Object.keys(value.Containers).length, 0)
      requireOwnedNetwork(value, { id: network, project }, [])
      await save('network-before-remove.json', value)
      await docker('remove-network', ['network', 'rm', network])
    }
    for (const command of [['ps', '-aq'], ['network', 'ls', '-q'], ['volume', 'ls', '-q']])
      assert.equal((await docker('final-namespace-census', [...command, '--filter', `label=com.docker.compose.project=${project}`])).length, 0)
    if (sourceBefore) {
      const after = await source()
      await save('source-after.json', after)
      assert.deepEqual(after, sourceBefore)
    }
    for (const child of children) {
      if (!child.entry.joined || !absent(child.entry.pid, true)) await signalOwned(child, 'SIGTERM')
      assert.ok(child.entry.joined && absent(child.entry.pid, true), 'Recorded child remains live')
    }
    closure = true
  } catch (error) { failure ??= { stage, reason: error instanceof Error ? error.message : 'Closure unknown' } }
  if (closure) {
    try {
      const raw = await readFile(join(output, 'vault-final-audit.jsonl'))
      const observations = JSON.parse(await readFile(join(output, 'native/after-observations.json')))
      const hashes = JSON.parse(await readFile(join(output, 'native/before-audit-expectations.json')))
      auditReadback = certificateAudit(raw, { run, probe: observations.probe_id, hashes })
      await save('certificate-audit-readback.json', auditReadback)
    } catch (error) { failure ??= { stage: 'certificate-audit-readback', reason: error.message } }
    try {
      const clean = []
      if (images.has('anchor')) {
        const tag = `${project}:fresh`
        const value = JSON.parse(await docker('cleanup-owned-image-inspect', ['image', 'inspect', tag]))[0]
        assert.equal(value.Id, images.get('anchor'))
        assert.equal((await docker('owned-image-containers', ['ps', '-aq', '--filter', `ancestor=${value.Id}`])).length, 0)
        await docker('cleanup-owned-image', ['image', 'rm', tag])
        clean.push(tag)
      }
      for (const name of ['prepare-pki', 'artifacts']) {
        await rm(join(output, name), { recursive: true, force: true })
        clean.push(name)
      }
      await save('owned-artifact-cleanup.json', { removed: clean, after_verified_closure: true })
    } catch (error) { failure ??= { stage: 'owned-artifact-cleanup', reason: error.message } }
  }
  for (const child of children) {
    if (!child.entry.joined || !absent(child.entry.pid, true)) {
      try { await joinChild(child, cleanupDeadline, true) }
      catch (error) { failure ??= { stage: 'final-process-join', reason: error.message } }
    }
    if (!child.entry.joined || !absent(child.entry.pid, true)) closure = false
  }
  const passed = !failure && closure && auditReadback && phases.get('after')?.passed === true
  await save('processes.json', children.map((child) => child.entry))
  await save('terminal.json', { passed: Boolean(passed), failure, closure, run, driver, phases: Object.fromEntries(phases), audit_readback: auditReadback, application_adoption: false, native_AI_completion: false })
  console.log(JSON.stringify({ passed: Boolean(passed), failure, closure, output }))
  if (!passed) process.exitCode = 1
}
