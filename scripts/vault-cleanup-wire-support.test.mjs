import assert from 'node:assert/strict'
import test from 'node:test'
import {
  requireNativeFiles,
  requireOwnedNetwork,
  requireOwnedVault,
  requireRequestCensus,
} from './vault-cleanup-wire-support.mjs'

const nativeFiles = () => ({
  run: 'a'.repeat(32),
  source_manifest: 'b'.repeat(64),
  passed: true,
  last_stage: 'dummy-metadata',
  reader_mode: 'token',
  application_runtime_acceptance: false,
  files: [
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
    'audit-before.jsonl',
    'audit-after.jsonl',
  ].map((name) => ({ name, bytes: 1, sha256: 'c'.repeat(64) })),
})
test('completed native manifest binds every fixed file while failed partial files stay failed', () => {
  const v = nativeFiles()
  assert.equal(
    requireNativeFiles(v, { run: v.run, source: v.source_manifest, readerMode: 'token' }).length,
    15,
  )
  v.passed = false
  v.files = v.files.filter((row) => row.name === 'http-census.json')
  assert.equal(
    requireNativeFiles(v, { run: v.run, source: v.source_manifest, readerMode: 'token' }).length,
    1,
  )
})
test('foreign or malformed native artifacts never become arbitrary container reads', () => {
  for (const mutate of [
    (v) => {
      v.files[0].name = '../other'
    },
    (v) => {
      v.files.push({ ...v.files[0] })
    },
    (v) => {
      v.files[0].bytes = (4 << 20) + 1
    },
    (v) => {
      v.files[0].sha256 = 'missing'
    },
    (v) => {
      v.files.pop()
    },
    (v) => {
      v.token = 'unlisted'
    },
    (v) => {
      v.passed = null
    },
  ]) {
    const v = nativeFiles()
    mutate(v)
    assert.throws(() =>
      requireNativeFiles(v, { run: v.run, source: v.source_manifest, readerMode: 'token' }),
    )
  }
  const v = nativeFiles()
  assert.throws(() =>
    requireNativeFiles(v, { run: 'd'.repeat(32), source: v.source_manifest, readerMode: 'token' }),
  )
  assert.throws(() =>
    requireNativeFiles(v, { run: v.run, source: 'd'.repeat(64), readerMode: 'token' }),
  )
})

const expected = {
  id: 'a'.repeat(64),
  image: `sha256:${'b'.repeat(64)}`,
  project: 'routex-vault-wire-test',
  run: 'c'.repeat(32),
  uid: 501,
  client: '/private/tmp/owned/wire-client',
  config: '/private/tmp/owned/vault.hcl',
}
const value = () => ({
  Id: expected.id,
  Image: expected.image,
  Name: `/${expected.project}-vault-1`,
  Config: {
    User: '501:501',
    Entrypoint: ['vault'],
    Cmd: ['server', '-config=/config/vault.hcl'],
    Healthcheck: { Test: ['NONE'] },
    Labels: {
      'com.docker.compose.project': expected.project,
      'com.docker.compose.service': 'vault',
      'routex.acceptance.run': expected.run,
    },
  },
  HostConfig: {
    ReadonlyRootfs: true,
    Privileged: false,
    RestartPolicy: { Name: 'no' },
    CapDrop: ['ALL'],
    SecurityOpt: ['no-new-privileges:true'],
    Tmpfs: { '/vault/file': 'uid=501', '/vault/logs': 'uid=501', '/tmp': 'mode=1777' },
    PortBindings: {},
  },
  State: {
    Running: true,
    Pid: 123,
    StartedAt: '2026-10-19T00:00:00Z',
    OOMKilled: false,
  },
  NetworkSettings: { Networks: { [`${expected.project}_default`]: {} } },
  Mounts: [
    {
      Type: 'bind',
      Source: expected.config,
      Destination: '/config/vault.hcl',
      RW: false,
    },
    {
      Type: 'bind',
      Source: expected.client,
      Destination: '/opt/routex/vault-wire-client',
      RW: false,
    },
  ],
})
test('exact owned generation can be observed and then positively stopped', () => {
  const running = value()
  assert.deepEqual(requireOwnedVault(running, expected), {
    id: expected.id,
    image: expected.image,
    pid: 123,
    birth: '2026-10-19T00:00:00Z',
  })
  running.State.Running = false
  running.State.Pid = 0
  assert.equal(
    requireOwnedVault(running, { ...expected, birth: running.State.StartedAt }, { stopped: true })
      .pid,
    0,
  )
})
for (const [name, mutate] of [
  [
    'foreign ID',
    (v) => {
      v.Id = 'd'.repeat(64)
    },
  ],
  [
    'changed image',
    (v) => {
      v.Image = `sha256:${'d'.repeat(64)}`
    },
  ],
  [
    'foreign project',
    (v) => {
      v.Config.Labels['com.docker.compose.project'] = 'foreign'
    },
  ],
  [
    'foreign run',
    (v) => {
      v.Config.Labels['routex.acceptance.run'] = 'd'.repeat(32)
    },
  ],
  [
    'public port',
    (v) => {
      v.HostConfig.PortBindings['8200/tcp'] = [{ HostIp: '0.0.0.0', HostPort: '40000' }]
    },
  ],
  [
    'additional port',
    (v) => {
      v.HostConfig.PortBindings['8200/tcp'] = [{ HostIp: '127.0.0.1', HostPort: '40000' }]
    },
  ],
  [
    'root user',
    (v) => {
      v.Config.User = '0:0'
    },
  ],
  [
    'privileged',
    (v) => {
      v.HostConfig.Privileged = true
    },
  ],
  [
    'added capability',
    (v) => {
      v.HostConfig.CapAdd = ['SYS_ADMIN']
    },
  ],
  [
    'background healthcheck',
    (v) => {
      v.Config.Healthcheck.Test = ['CMD', 'vault', 'status']
    },
  ],
  [
    'automatic restart',
    (v) => {
      v.HostConfig.RestartPolicy.Name = 'always'
    },
  ],
  [
    'writable config',
    (v) => {
      v.Mounts[0].RW = true
    },
  ],
  [
    'foreign config',
    (v) => {
      v.Mounts[0].Source = '/private/tmp/foreign/config'
    },
  ],
  [
    'persistent data',
    (v) => {
      v.Mounts.push({ Type: 'volume' })
    },
  ],
  [
    'additional network',
    (v) => {
      v.NetworkSettings.Networks.foreign = {}
    },
  ],
  [
    'additional tmpfs',
    (v) => {
      v.HostConfig.Tmpfs['/other'] = ''
    },
  ],
  [
    'OOM',
    (v) => {
      v.State.OOMKilled = true
    },
  ],
])
  test(`refuses ${name} before stop or removal`, () => {
    const v = value()
    mutate(v)
    assert.throws(() => requireOwnedVault(v, expected))
  })
test('PID or birth change refuses even when the container ID is unchanged', () => {
  assert.throws(() => requireOwnedVault(value(), { ...expected, pid: 124 }))
  assert.throws(() => requireOwnedVault(value(), { ...expected, birth: '2026-10-18T00:00:00Z' }))
  assert.throws(() => requireOwnedVault(value(), expected, { stopped: true }))
})
test('internal network requires its complete exact member census', () => {
  const v = {
    Id: 'd'.repeat(64),
    Name: `${expected.project}_default`,
    Driver: 'bridge',
    Internal: true,
    Labels: {
      'com.docker.compose.project': expected.project,
      'com.docker.compose.network': 'default',
    },
    Containers: { [expected.id]: {} },
  }
  assert.equal(requireOwnedNetwork(v, { project: expected.project }, [expected.id]), v.Id)
  assert.throws(() => requireOwnedNetwork(v, { project: expected.project }, []))
  assert.throws(() => requireOwnedNetwork(v, { ...expected, id: 'e'.repeat(64) }, [expected.id]))
  v.Internal = false
  assert.throws(() => requireOwnedNetwork(v, expected, [expected.id]))
})
const receipt = () => [
  {
    sequence: 1,
    name: 'dummy-destroy',
    method: 'PUT',
    path: `/v1/kv/destroy/discovery/${'a'.repeat(32)}`,
    status: 204,
    bytes: 0,
    sha256: 'b'.repeat(64),
  },
]
test('literal 204 from the sole original dummy PUT is retained', () =>
  requireRequestCensus(receipt()))
test('HTTP success or missing numeric status never substitutes for literal 204', () => {
  for (const status of [200, 202, null, '204']) {
    const v = receipt()
    v[0].status = status
    assert.throws(() => requireRequestCensus(v))
  }
})
test('body overflow, sequence gaps, unknown fields and repeated destroy refuse', () => {
  for (const mutate of [
    (v) => {
      v[0].bytes = (4 << 20) + 1
    },
    (v) => {
      v[0].sequence = 2
    },
    (v) => {
      v[0].token = 'not-allowed'
    },
    (v) => {
      v.push({ ...v[0], sequence: 2 })
    },
  ]) {
    const v = receipt()
    mutate(v)
    assert.throws(() => requireRequestCensus(v))
  }
  assert.throws(() => requireRequestCensus([]))
})

test('AppRole terminal requires its own complete artifacts and never borrows Token mode', () => {
  const v = nativeFiles()
  v.reader_mode = 'app_role'
  v.files = v.files.filter((row) => row.name !== 'private-token-reader.json')
  for (const name of [
    'private-response-reader-role-id.json',
    'private-response-reader-secret-id.json',
    'private-response-reader-login.json',
    'private-reader-material.json',
  ])
    v.files.push({ name, bytes: 1, sha256: 'c'.repeat(64) })
  assert.equal(
    requireNativeFiles(v, { run: v.run, source: v.source_manifest, readerMode: 'app_role' }).length,
    18,
  )
  assert.throws(() =>
    requireNativeFiles(v, { run: v.run, source: v.source_manifest, readerMode: 'token' }),
  )
  v.files.pop()
  assert.throws(() =>
    requireNativeFiles(v, { run: v.run, source: v.source_manifest, readerMode: 'app_role' }),
  )
})
