import assert from 'node:assert/strict'

const fullID = /^[a-f0-9]{64}$/

const nativeNames = new Set([
  'private-initialization.json',
  'private-token-writer.json',
  'private-token-reader.json',
  'private-token-cleanup.json',
  'private-material.json',
  'metrics-before.prom',
  'metrics-after.prom',
  'observed-wire.json',
  'http-census.json',
  'private-response-dummy-create.json',
  'private-response-dummy-read.json',
  'private-response-dummy-metadata.json',
  'private-response-audit-version-hash.json',
])

export function requireNativeFiles(terminal, expected) {
  assert.deepEqual(
    Object.keys(terminal ?? {}).sort(),
    ['application_runtime_acceptance', 'files', 'last_stage', 'passed', 'run', 'source_manifest'],
    'Native terminal shape changed',
  )
  assert.equal(terminal?.run, expected.run, 'Native terminal run changed')
  assert.equal(terminal?.source_manifest, expected.source, 'Native terminal source changed')
  assert.equal(typeof terminal.passed, 'boolean', 'Native outcome unknown')
  assert.equal(terminal.application_runtime_acceptance, false, 'Native scope changed')
  assert.match(terminal.last_stage, /^[a-z][a-z0-9-]{0,63}$/, 'Native stage invalid')
  assert.ok(
    Array.isArray(terminal.files) &&
      terminal.files.length > 0 &&
      terminal.files.length <= nativeNames.size,
    'Native file manifest missing',
  )
  const seen = new Set()
  for (const row of terminal.files) {
    assert.deepEqual(
      Object.keys(row).sort(),
      ['bytes', 'name', 'sha256'],
      'Native artifact shape changed',
    )
    assert.ok(
      nativeNames.has(row.name) && !seen.has(row.name),
      'Unknown or repeated native artifact',
    )
    assert.ok(
      Number.isInteger(row.bytes) && row.bytes >= 0 && row.bytes <= 4 << 20,
      'Native artifact size invalid',
    )
    assert.match(row.sha256, fullID, 'Native artifact hash missing')
    seen.add(row.name)
  }
  assert.ok(seen.has('http-census.json'), 'Native census missing')
  if (terminal.passed) assert.deepEqual(seen, nativeNames, 'Successful native capture incomplete')
  return terminal.files
}

export function requireOwnedVault(value, expected, { stopped = false } = {}) {
  assert.ok(value && expected, 'Vault ownership is missing')
  assert.match(expected.id, fullID, 'Invalid owned container ID')
  assert.match(expected.image, /^sha256:[a-f0-9]{64}$/, 'Invalid owned image ID')
  assert.equal(value.Id, expected.id, 'Container identity changed')
  assert.equal(value.Image, expected.image, 'Container image changed')
  assert.equal(value.Name, `/${expected.project}-vault-1`, 'Container name changed')
  const labels = value.Config?.Labels
  assert.equal(labels?.['com.docker.compose.project'], expected.project, 'Project changed')
  assert.equal(labels?.['com.docker.compose.service'], 'vault', 'Service changed')
  assert.equal(labels?.['routex.acceptance.run'], expected.run, 'Run changed')
  assert.equal(value.Config.User, `${expected.uid}:${expected.uid}`, 'Vault user changed')
  assert.deepEqual(value.Config.Entrypoint, ['vault'], 'Vault entrypoint changed')
  assert.deepEqual(
    value.Config.Cmd,
    ['server', '-config=/config/vault.hcl'],
    'Vault command changed',
  )
  assert.deepEqual(value.Config.Healthcheck?.Test, ['NONE'], 'Background health requests refused')
  assert.equal(value.HostConfig?.ReadonlyRootfs, true, 'Read-only root changed')
  assert.deepEqual(value.HostConfig.CapDrop, ['ALL'], 'Capabilities changed')
  assert.deepEqual(value.HostConfig.CapAdd ?? [], [], 'Additional capabilities refused')
  assert.deepEqual(
    value.HostConfig.SecurityOpt,
    ['no-new-privileges:true'],
    'Security options changed',
  )
  assert.equal(value.HostConfig.Privileged, false, 'Privileged container refused')
  assert.equal(value.HostConfig.RestartPolicy?.Name, 'no', 'Restart policy changed')
  assert.deepEqual(
    Object.keys(value.HostConfig.Tmpfs).sort(),
    ['/tmp', '/vault/file', '/vault/logs'],
    'Tmpfs scope changed',
  )
  assert.deepEqual(
    Object.keys(value.NetworkSettings.Networks),
    [`${expected.project}_default`],
    'Extra network refused',
  )
  assert.equal(value.State.OOMKilled, false, 'Vault OOM is not accepted')
  assert.equal(typeof value.State.StartedAt, 'string', 'Vault birth missing')
  assert.ok(Number.isFinite(Date.parse(value.State.StartedAt)), 'Vault birth invalid')
  if (expected.birth)
    assert.equal(value.State.StartedAt, expected.birth, 'Vault generation changed')
  if (stopped) {
    assert.equal(value.State.Running, false, 'Vault still running')
    assert.equal(value.State.Pid, 0, 'Vault PID not closed')
  } else {
    assert.equal(value.State.Running, true, 'Vault not running')
    assert.ok(Number.isInteger(value.State.Pid) && value.State.Pid > 0, 'Vault PID missing')
    if (expected.pid) assert.equal(value.State.Pid, expected.pid, 'Vault PID changed')
  }
  assert.deepEqual(value.HostConfig.PortBindings ?? {}, {}, 'Published ports refused')
  const binds = value.Mounts.filter((mount) => mount.Type === 'bind')
  assert.equal(binds.length, 2, 'Unexpected bind mounts')
  const wanted = new Map([
    [expected.config, '/config/vault.hcl'],
    [expected.client, '/opt/routex/vault-wire-client'],
  ])
  assert.equal(wanted.size, 2, 'Duplicate input binding')
  for (const bind of binds) {
    assert.equal(wanted.get(bind.Source), bind.Destination, 'Input binding changed')
    assert.equal(bind.RW, false, 'Writable input refused')
    wanted.delete(bind.Source)
  }
  assert.equal(wanted.size, 0, 'Input binding missing')
  assert.ok(
    value.Mounts.every((mount) => ['bind', 'tmpfs'].includes(mount.Type)),
    'Persistent mount refused',
  )
  return {
    id: value.Id,
    image: value.Image,
    pid: value.State.Pid,
    birth: value.State.StartedAt,
  }
}

export function requireOwnedNetwork(value, expected, members) {
  assert.match(value?.Id ?? '', fullID, 'Network identity missing')
  if (expected.id) assert.equal(value.Id, expected.id, 'Network identity changed')
  assert.equal(value.Name, `${expected.project}_default`, 'Network name changed')
  assert.equal(value.Driver, 'bridge', 'Network driver changed')
  assert.equal(value.Internal, true, 'External network refused')
  assert.equal(
    value.Labels?.['com.docker.compose.project'],
    expected.project,
    'Foreign network refused',
  )
  assert.equal(value.Labels?.['com.docker.compose.network'], 'default', 'Network role changed')
  assert.deepEqual(
    Object.keys(value.Containers ?? {}).sort(),
    [...members].sort(),
    'Network census changed',
  )
  return value.Id
}

export function requireRequestCensus(rows) {
  assert.ok(Array.isArray(rows) && rows.length > 0 && rows.length <= 64, 'HTTP census is missing')
  for (const [index, row] of rows.entries()) {
    assert.deepEqual(Object.keys(row).sort(), [
      'bytes',
      'method',
      'name',
      'path',
      'sequence',
      'sha256',
      'status',
    ])
    assert.equal(row.sequence, index + 1, 'HTTP census sequence changed')
    assert.match(row.name, /^[a-z][a-z0-9-]{0,63}$/, 'Unsafe HTTP stage')
    assert.ok(['GET', 'POST', 'PUT'].includes(row.method), 'Unexpected HTTP method')
    assert.ok(row.path.startsWith('/v1/') && !/[\r\n#]/.test(row.path), 'Unsafe HTTP path')
    assert.ok(
      Number.isInteger(row.status) && row.status >= 200 && row.status <= 599,
      'Numeric status missing',
    )
    assert.ok(
      Number.isInteger(row.bytes) && row.bytes >= 0 && row.bytes <= 4 << 20,
      'Body bound changed',
    )
    assert.match(row.sha256, fullID, 'Body digest missing')
  }
  const destroy = rows.filter((row) => row.name === 'dummy-destroy')
  assert.equal(destroy.length, 1, 'Original dummy destroy missing or repeated')
  assert.equal(destroy[0].method, 'PUT', 'Destroy method changed')
  assert.match(destroy[0].path, /^\/v1\/kv\/destroy\/discovery\/[a-f0-9]{32}$/)
  assert.equal(destroy[0].status, 204, 'Literal dummy status is not 204')
  assert.equal(destroy[0].bytes, 0, 'Unexpected 204 response body')
}
