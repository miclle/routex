import assert from 'node:assert/strict'
import { createHash, randomBytes } from 'node:crypto'
import { once } from 'node:events'
import { mkdir, mkdtemp, readFile, realpath, writeFile } from 'node:fs/promises'
import { createServer as httpServer } from 'node:http'
import { createConnection, createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { dirname, isAbsolute, join, resolve } from 'node:path'
import { performance } from 'node:perf_hooks'
import { fileURLToPath } from 'node:url'
import { setTimeout as delay } from 'node:timers/promises'
import { boundedBody, requireNoSecrets } from './backup-restore-support.mjs'

import {
  absent,
  createSupervisor,
  executableReceipt,
  sourceEntry,
} from './storage-native-acceptance-support.mjs'
import {
  assetsDigest,
  requireInputs,
  requireDeletionResponse,
  requirePendingSnapshot,
} from './storage-native-contracts.mjs'

process.umask(0o077)
const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const workDeadline = performance.now() + 600_000
const absoluteDeadline = workDeadline + 90_000
const abort = new AbortController()
let cleanupDeadline
let interrupted = false
let stage = 'preflight'
let failureStage
let application
let peer
let composeStarted = false
let sequence = 0
let peerFailure = false
let nativeCalls = 0
const children = []
const ports = new Set()
const requestIDs = []
const results = []
const fixtureEnv = Object.fromEntries(
  Object.entries(process.env).filter(([name]) => !name.startsWith('ROUTEX_')),
)
const hash = (bytes) => createHash('sha256').update(bytes).digest('hex')
for (const signal of ['SIGINT', 'SIGTERM'])
  process.once(signal, () => {
    interrupted = true
    abort.abort()
  })
function remaining(deadline = cleanupDeadline ?? workDeadline) {
  assert.ok(
    (!interrupted || cleanupDeadline !== undefined) && performance.now() < deadline,
    'Work deadline or cancellation',
  )
  return Math.max(1, deadline - performance.now())
}
const executable = (name) => executableReceipt(process.env[name])
const binary = await executable('ROUTEX_ACCEPTANCE_BINARY')
const helper = await executable('ROUTEX_ACCEPTANCE_HELPER')
const output = await mkdtemp(join(await realpath(tmpdir()), 'routex-storage-native-'))
await mkdir(join(output, 'logs'), { mode: 0o700 })
const save = async (name, value) =>
  writeFile(join(output, name), `${JSON.stringify(value, null, 2)}\n`, {
    mode: 0o600,
    flag: 'wx',
  })
const project = `routex-acceptance-${Date.now()}-${process.pid}`
const composeFile = join(output, 'compose.yaml')
const compose = ['compose', '-p', project, '-f', composeFile]

const { start, signalOwned, joinChild } = createSupervisor({
  repo,
  fixtureEnv,
  children,
  workDeadline,
  absoluteDeadline,
  remaining,
  interrupted: () => interrupted,
})
async function command(name, executable, args, options = {}) {
  stage = name
  const state = start(name, executable, args, {
    ...options,
    deadline: Math.min(
      cleanupDeadline ?? workDeadline,
      performance.now() + (options.timeout ?? 30_000),
    ),
  })
  const bytes = await joinChild(state)
  assert.equal(state.entry.exit_code, 0, 'Owned command failed')
  return bytes
}
const docker = (name, args, options) => command(name, 'docker', args, options)
async function port() {
  const listener = createServer().listen(0, '127.0.0.1')
  await once(listener, 'listening')
  const value = listener.address().port
  await new Promise((done, reject) => listener.close((error) => (error ? reject(error) : done())))
  assert.ok(!ports.has(value), 'Duplicate fixture port')
  ports.add(value)
  return value
}
async function sourceDigest() {
  const files = (
    await command('source-list', 'git', [
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
  for (const path of files) {
    rows.push(await sourceEntry(join(repo, path), path))
  }
  return { digest: hash(Buffer.from(JSON.stringify(rows))), rows }
}
async function request(
  origin,
  path,
  expected,
  { method = 'GET', cookie, csrf, bearer, body, etag, raw = false, form } = {},
) {
  stage = `${method} ${path}`
  const headers = { Origin: origin, 'Sec-Fetch-Site': 'same-origin' }
  if (cookie) headers.Cookie = cookie
  if (csrf) headers['X-CSRF-Token'] = csrf
  if (bearer) headers.Authorization = `Bearer ${bearer}`
  if (etag) headers['If-Match'] = `"${etag}"`
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const response = await fetch(`${origin}${path}`, {
    method,
    headers,
    body: form ?? (body === undefined ? undefined : JSON.stringify(body)),
    redirect: 'error',
    signal: AbortSignal.any([abort.signal, AbortSignal.timeout(Math.min(10_000, remaining()))]),
  })
  assert.equal(response.status, expected, 'Unexpected HTTP status')
  if (expected === 204) {
    await response.body?.cancel()
    return { response, value: null }
  }
  const bytes = await boundedBody(response, 2 << 20)
  return { response, value: raw ? bytes : JSON.parse(bytes) }
}
const sessionCookie = (response) => {
  const value = response.headers
    .getSetCookie()
    .find((item) => item.startsWith('routex_session='))
    ?.split(';')[0]
  assert.ok(value, 'Original Session cookie missing')
  return value
}
let origin
let config
async function launch() {
  assert.equal(hash(await readFile(binary.path)), binary.sha256, 'Production executable changed')
  application = start(`application-${++sequence}`, binary.path, ['-c', config])
  assert.ok(
    application.entry.birth && application.entry.pgid === application.entry.pid,
    'Long-lived application process ownership unobserved',
  )
  const deadline = Math.min(workDeadline, performance.now() + 60_000)
  while (performance.now() < deadline) {
    assert.ok(!application.entry.joined, 'Application exited during startup')
    let response
    try {
      response = await fetch(`${origin}/health`, {
        redirect: 'error',
        signal: AbortSignal.timeout(500),
      })
    } catch {
      await delay(100)
      continue
    }
    if (response.status === 200 && (await boundedBody(response)).toString() === 'ok') {
      const page = await request(origin, '/', 200, { raw: true })
      assert.ok(page.value.toString().includes('<html'), 'Ordinary embedded production SPA missing')
      return
    }
    await response.body?.cancel()
    await delay(100)
  }
  throw new Error('Application readiness timed out')
}
async function stopApplication() {
  if (!application) return
  const state = application
  await joinChild(
    state,
    Math.min(cleanupDeadline ?? workDeadline, performance.now() + 30_000),
    true,
  )
  application = undefined
  assert.equal(state.entry.exit_code, 0, 'Application shutdown did not succeed')
}
async function fixture(mode, input, endpoint, bucket) {
  assert.equal(hash(await readFile(helper.path)), helper.sha256, 'Fixture executable changed')
  const receipt = join(output, `${mode}-${++sequence}.json`)
  const args = ['-mode', mode, '-endpoint', endpoint, '-bucket', bucket, '-receipt', receipt]
  if (input) {
    const inputPath = join(output, `${mode}-input-${sequence}.json`)
    await writeFile(inputPath, JSON.stringify(input), {
      mode: 0o600,
      flag: 'wx',
    })
    args.push('-input', inputPath)
  }
  await command(`fixture-${mode}`, helper.path, args, { timeout: 65_000 })
  return JSON.parse(await readFile(receipt))
}
async function waitCall(cookie, id, projectID) {
  const path = projectID ? `/api/v1/projects/${projectID}/calls/${id}` : `/api/v1/calls/${id}`
  const deadline = Math.min(workDeadline, performance.now() + 15_000)
  while (performance.now() < deadline) {
    const response = await fetch(`${origin}${path}`, {
      headers: { Cookie: cookie },
      signal: AbortSignal.timeout(1000),
      redirect: 'error',
    })
    if (response.status === 200) {
      const value = JSON.parse(await boundedBody(response))
      assert.equal(value.status, 'success', 'Native call failed')
      return value
    }
    assert.equal(response.status, 404, 'Call read failed')
    await response.body?.cancel()
    await delay(100)
  }
  throw new Error('Immutable call delivery timed out')
}

let sourceBefore, inputsBefore
const png = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aX1sAAAAASUVORK5CYII=',
  'base64',
)
function blankPDF() {
  let text = '%PDF-1.4\n'
  const objects = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 72 72] /Resources << >> >>',
  ]
  const offsets = [0]
  for (let index = 0; index < objects.length; index++) {
    offsets.push(Buffer.byteLength(text))
    text += `${index + 1} 0 obj\n${objects[index]}\nendobj\n`
  }
  const xref = Buffer.byteLength(text)
  text += `xref\n0 4\n0000000000 65535 f \n${offsets
    .slice(1)
    .map((offset) => `${String(offset).padStart(10, '0')} 00000 n \n`)
    .join('')}trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`
  return Buffer.from(text)
}
const pdf = blankPDF()

if (process.argv.slice(2).includes('--capture-inputs')) {
  let captured = false
  try {
    const source = await sourceDigest()
    const assets = await assetsDigest(repo)
    await save('inputs.json', {
      source_digest: source.digest,
      binary_sha256: binary.sha256,
      helper_sha256: helper.sha256,
      assets_digest: assets.digest,
    })
    await save('source.json', source)
    await save('assets.json', assets)
    captured = true
  } catch {
    process.exitCode = 1
  } finally {
    cleanupDeadline = Math.min(absoluteDeadline, performance.now() + 30_000)
    for (const child of children.filter((state) => !state.entry.group_gone)) {
      try {
        await joinChild(child, Math.min(cleanupDeadline, performance.now() + 5000), true)
      } catch {
        process.exitCode = 1
      }
    }
    await save('capture-result.json', {
      status: captured && !process.exitCode ? 'captured-unqualified' : 'failed',
      process_receipts: children.map((state) => state.entry),
    })
  }
  console.log(
    `Unqualified input capture ${captured && !process.exitCode ? 'complete' : 'failed'}; private artifacts: ${output}`,
  )
  process.exit(process.exitCode ?? 0)
}
try {
  const qualificationPath = process.env.ROUTEX_ACCEPTANCE_QUALIFICATION
  assert.ok(
    qualificationPath && isAbsolute(qualificationPath),
    'Exact-source qualification receipt required',
  )
  const qualification = JSON.parse(await readFile(qualificationPath))
  sourceBefore = await sourceDigest()
  assert.equal(qualification.source_digest, sourceBefore.digest, 'Qualified source does not match')
  assert.equal(
    qualification.binary_sha256,
    binary.sha256,
    'Qualified production binary does not match',
  )
  assert.equal(qualification.helper_sha256, helper.sha256, 'Qualified helper does not match')
  assert.ok(
    qualification.mandatory_check === 'passed' &&
      qualification.ordinary_tests === 'passed' &&
      qualification.production_build === 'passed',
    'Required source qualification incomplete',
  )
  const assets = await assetsDigest(repo)
  assert.equal(qualification.assets_digest, assets.digest, 'Qualified embedded assets changed')
  inputsBefore = {
    source_digest: sourceBefore.digest,
    binary_sha256: binary.sha256,
    helper_sha256: helper.sha256,
    assets_digest: assets.digest,
  }
  requireInputs(qualification, inputsBefore)
  await save('assets-before.json', assets)
  await save('source-before.json', sourceBefore)
  const s3Port = await port()
  const dbPort = await port()
  const appPort = await port()
  const endpoint = `http://127.0.0.1:${s3Port}`
  origin = `http://127.0.0.1:${appPort}`
  const bucket = `routex-acceptance-${randomBytes(12).toString('hex')}`
  Object.assign(fixtureEnv, {
    ROUTEX_ACCEPTANCE_ACCESS_KEY: randomBytes(16).toString('hex'),
    ROUTEX_ACCEPTANCE_SECRET_KEY: randomBytes(32).toString('hex'),
    ROUTEX_ACCEPTANCE_DB_PASSWORD: randomBytes(24).toString('hex'),
    ROUTEX_ACCEPTANCE_S3_PORT: String(s3Port),
    ROUTEX_ACCEPTANCE_POSTGRES_PORT: String(dbPort),
    ROUTEX_ACCEPTANCE_ROOT: randomBytes(32).toString('base64'),
  })
  fixtureEnv.ROUTEX_ACCEPTANCE_DSN = `postgres://routex_acceptance:${fixtureEnv.ROUTEX_ACCEPTANCE_DB_PASSWORD}@127.0.0.1:${dbPort}/routex_acceptance?sslmode=disable`
  await writeFile(
    composeFile,
    await readFile(join(repo, 'scripts/storage-acceptance.compose.yaml')),
    { mode: 0o600, flag: 'wx' },
  )
  await save('owned-intent.json', {
    project,
    controller_pid: process.pid,
    binary_sha256: binary.sha256,
    helper_sha256: helper.sha256,
    ports: [...ports],
    bucket,
    compose_sha256: hash(await readFile(composeFile)),
  })
  for (const kind of ['container', 'network', 'volume']) {
    const args = kind === 'container' ? ['ps', '-aq'] : [kind, 'ls', '-q']
    assert.equal(
      (
        await docker('project-absence', [
          ...args,
          '--filter',
          `label=com.docker.compose.project=${project}`,
        ])
      )
        .toString()
        .trim(),
      '',
      'Compose project already exists',
    )
  }
  composeStarted = true
  await docker(
    'compose-up',
    [...compose, 'up', '--pull', 'never', '--detach', '--wait', '--wait-timeout', '180'],
    { timeout: 190_000 },
  )
  const containerIDs = (await docker('compose-container-ids', [...compose, 'ps', '-q']))
    .toString()
    .trim()
    .split('\n')
  assert.equal(containerIDs.length, 2, 'Fixture container census changed')
  await save(
    'containers.json',
    JSON.parse((await docker('compose-inspect', ['inspect', ...containerIDs])).toString()),
  )
  const prepared = await fixture('prepare', undefined, endpoint, bucket)
  assert.equal(prepared.state, 'prepared', 'Versioned bucket preparation not acknowledged')
  assert.equal(prepared.versioning, 'Enabled', 'Versioning not enabled')
  results.push('actual authenticated versioned bucket prepared')
  config = join(output, 'application.yaml')
  await writeFile(
    config,
    `addr: "127.0.0.1:${appPort}"\ndriver: postgres\ndsn: "\${ROUTEX_ACCEPTANCE_DSN}"\nencryption_key: "\${ROUTEX_ACCEPTANCE_ROOT}"\nallow_private_upstreams: true\nallow_private_storage: true\nevent_queue_path: ${JSON.stringify(join(output, 'calls.db'))}\n`,
    { mode: 0o600, flag: 'wx' },
  )
  await launch()
  assert.equal(
    (await request(origin, '/api/v1/setup', 200)).value.initialized,
    false,
    'Database not fresh',
  )
  const password = randomBytes(24).toString('base64url')
  const setup = await request(origin, '/api/v1/setup', 201, {
    method: 'POST',
    body: {
      email: 'storage-admin@example.invalid',
      name: 'Storage acceptance',
      password,
    },
  })
  const cookie = sessionCookie(setup.response)
  const session = (await request(origin, '/api/v1/auth/session', 200, { cookie })).value
  let csrf = session.csrf_token
  const get = async (path) => (await request(origin, `/api/v1${path}`, 200, { cookie })).value
  const write = async (path, body, expected = 200, method = 'POST', etag) =>
    (
      await request(origin, `/api/v1${path}`, expected, {
        cookie,
        csrf,
        body,
        method,
        etag,
      })
    ).value
  const settings = await get('/admin/storage')
  const saved = await write(
    '/admin/storage',
    {
      enabled: true,
      endpoint,
      region: 'us-east-1',
      bucket,
      prefix: 'native-acceptance',
      etag: settings.etag,
      auth: {
        action: 'replace',
        access_key: fixtureEnv.ROUTEX_ACCEPTANCE_ACCESS_KEY,
        secret_key: fixtureEnv.ROUTEX_ACCEPTANCE_SECRET_KEY,
      },
    },
    200,
    'PUT',
  )
  assert.ok(
    saved.enabled && saved.revision?.verified_at,
    'Verified active storage revision missing',
  )
  const probe = await write('/admin/storage/test', { etag: saved.etag })
  assert.ok(
    probe.success &&
      !probe.cleanup_pending &&
      probe.stages.every((item) => item.status === 'passed'),
    'Real storage probe failed',
  )
  const credential = randomBytes(24).toString('hex')
  const opaque = 'routex://attachments/not-an-attachment-position'
  peer = httpServer(async (req, res) => {
    try {
      assert.equal(req.headers.authorization, `Bearer ${credential}`)
      if (req.method === 'GET' && req.url === '/v1/models') {
        res.setHeader('Content-Type', 'application/json')
        res.end(JSON.stringify({ data: [{ id: 'storage-native' }] }))
        return
      }
      assert.equal(req.method, 'POST')
      assert.equal(req.url, '/v1/chat/completions')
      const chunks = []
      let size = 0
      for await (const chunk of req) {
        size += chunk.length
        assert.ok(size <= 8192)
        chunks.push(chunk)
      }
      const body = JSON.parse(Buffer.concat(chunks))
      assert.equal(body.model, 'storage-native')
      assert.equal(body.messages[0].content[0].text, opaque)
      assert.equal(
        body.messages[0].content[1].image_url.url,
        `data:image/png;base64,${png.toString('base64')}`,
      )
      assert.equal(
        body.messages[0].content[2].file.file_data,
        `data:application/pdf;base64,${pdf.toString('base64')}`,
      )
      assert.equal(body.messages[0].content[2].file.filename, 'document.pdf')
      nativeCalls++
      res.setHeader('Content-Type', 'application/json')
      const response = {
        id: 'storage-answer',
        object: 'chat.completion',
        model: 'storage-native',
        choices: [
          {
            index: 0,
            message: { role: 'assistant', content: 'Resolved' },
            finish_reason: 'stop',
          },
        ],
        usage: { prompt_tokens: 3, completion_tokens: 2, total_tokens: 5 },
      }
      await save(`native-response-${nativeCalls}.json`, response)
      res.end(JSON.stringify(response))
    } catch {
      peerFailure = true
      res.writeHead(400)
      res.end()
    }
  })
  peer.listen(0, '127.0.0.1')
  await once(peer, 'listening')
  ports.add(peer.address().port)
  const provider = await write(
    '/admin/providers',
    {
      name: 'Storage acceptance',
      connection_name: 'Native fixture',
      base_url: `http://127.0.0.1:${peer.address().port}/v1`,
      protocol: 'openai_chat',
      credential_name: 'Disposable fixture',
      secret: credential,
    },
    201,
  )
  const credentialID = provider.connections[0].credentials[0].id
  assert.equal(
    (await write(`/admin/credentials/${credentialID}/verify`, {})).verified,
    true,
    'Credential verification failed',
  )
  await write(`/admin/credentials/${credentialID}`, { enabled: true }, 200, 'PATCH')
  const catalog = await get('/admin/providers')
  const connection = catalog.items.find((row) => row.id === provider.id).connections[0]
  const native = connection.provider_models.find((row) => row.upstream_name === 'storage-native')
  assert.ok(native, 'Discovered native Model absent')
  const capable = await write(
    `/admin/provider-models/${native.id}`,
    {
      etag: native.etag,
      capability_review_etag: native.capability_review_etag,
      supports_image_input: true,
      supports_pdf_input: true,
    },
    200,
    'PATCH',
  )
  assert.ok(
    capable.supports_image_input &&
      capable.supports_pdf_input &&
      capable.capabilities_transport_current,
    'Capabilities not explicitly applied',
  )
  const boundPath = `/admin/provider-models/${native.id}/reservation-bound`
  const bound = await get(boundPath)
  await write(
    boundPath,
    {
      max_input_tokens: 10,
      max_output_tokens: 10,
      evidence: 'Controlled native fixture returns three input and two output tokens',
      reason: 'Bounded native storage acceptance',
    },
    200,
    'PUT',
    bound.etag,
  )
  const model = await write(
    '/admin/models',
    { name: 'storage-model', provider_model_id: native.id },
    201,
  )
  await write(
    `/admin/models/${model.id}/weights`,
    {
      weights: model.bindings.map((item) => ({
        binding_id: item.id,
        weight: 100,
      })),
    },
    200,
    'PUT',
  )
  const personalKey = await write('/keys', { name: 'Storage personal', model_ids: [model.id] }, 201)
  await write(`/keys/${personalKey.key.id}/confirm`, {})
  const projectRecord = await write(
    '/projects',
    {
      name: 'Storage acceptance Project',
      description: 'Disposable scoped native proof',
      manager_ids: [session.user.id],
    },
    201,
  )
  await write(`/projects/${projectRecord.id}/models`, { model_ids: [model.id] }, 200, 'PUT')
  const projectKey = await write(
    `/projects/${projectRecord.id}/keys`,
    { name: 'Storage Project', delivery_mode: 'manual', model_ids: [model.id] },
    201,
  )
  await write(`/projects/${projectRecord.id}/keys/${projectKey.key.id}/confirm`, {})
  const upload = async (prefix, bytes, name, mime) => {
    const form = new FormData()
    form.append('file', new Blob([bytes], { type: mime }), name)
    const attachment = (
      await request(origin, `/api/v1${prefix}`, 201, {
        method: 'POST',
        cookie,
        csrf,
        form,
      })
    ).value
    assert.equal(attachment.state, 'ready', 'Upload not ready')
    assert.equal(attachment.size, bytes.length, 'Upload size changed')
    assert.deepEqual(
      (
        await request(origin, `/api/v1${prefix}/${attachment.id}/content`, 200, {
          cookie,
          raw: true,
        })
      ).value,
      bytes,
      'Real stored content changed',
    )
    return attachment
  }
  const personal = [
    await upload('/attachments', png, 'image.png', 'image/png'),
    await upload('/attachments', pdf, 'document.pdf', 'application/pdf'),
  ]
  const projectPath = `/projects/${projectRecord.id}/attachments`
  const projectFiles = [
    await upload(projectPath, png, 'image.png', 'image/png'),
    await upload(projectPath, pdf, 'document.pdf', 'application/pdf'),
  ]
  const selected = {
    objects: [
      ...personal.map((row) => ({
        ID: row.id,
        OwnerKind: 'user',
        OwnerID: session.user.id,
      })),
      ...projectFiles.map((row) => ({
        ID: row.id,
        OwnerKind: 'project',
        OwnerID: projectRecord.id,
      })),
    ],
    request_ids: [],
  }
  const inference = async (key, files, expected = 200, projectID) => {
    const before = nativeCalls
    const response = await request(origin, '/v1/chat/completions', expected, {
      method: 'POST',
      bearer: key.secret,
      body: {
        model: 'storage-model',
        max_completion_tokens: 10,
        messages: [
          {
            role: 'user',
            content: [
              { type: 'text', text: opaque },
              {
                type: 'image_url',
                image_url: { url: `routex://attachments/${files[0].id}` },
              },
              {
                type: 'file',
                file: {
                  file_data: `routex://attachments/${files[1].id}`,
                  filename: 'caller.pdf',
                },
              },
            ],
          },
        ],
      },
    })
    if (expected === 200) {
      assert.equal(response.value.choices[0].message.content, 'Resolved', 'Native response lost')
      assert.equal(nativeCalls, before + 1, 'Native dispatch census changed')
      const id = response.response.headers.get('X-Request-ID')
      assert.ok(id, 'Call ID absent')
      requestIDs.push(id)
      await waitCall(cookie, id, projectID)
    } else assert.equal(nativeCalls, before, 'Denied attachment dispatched upstream')
  }
  await inference(personalKey, personal)
  await inference(projectKey, projectFiles, 200, projectRecord.id)
  await inference(personalKey, projectFiles, 404)
  await inference(projectKey, personal, 404)
  const otherPassword = randomBytes(24).toString('base64url')
  await write(
    '/admin/members',
    {
      email: 'storage-other@example.invalid',
      name: 'Other storage owner',
      password: otherPassword,
      role: 'member',
    },
    201,
  )
  const login = await request(origin, '/api/v1/auth/login', 200, {
    method: 'POST',
    body: { email: 'storage-other@example.invalid', password: otherPassword },
  })
  const otherCookie = sessionCookie(login.response)
  for (const row of personal) {
    await request(origin, `/api/v1/attachments/${row.id}`, 404, {
      cookie: otherCookie,
    })
    await request(origin, `/api/v1/attachments/${row.id}/content`, 404, {
      cookie: otherCookie,
      raw: true,
    })
  }
  for (const row of projectFiles)
    await request(origin, `/api/v1${projectPath}/${row.id}/content`, 404, {
      cookie: otherCookie,
      raw: true,
    })
  results.push('PNG/PDF native exact rewriting, opaque text and owner isolation')
  selected.request_ids = requestIDs
  const before = await fixture('snapshot', selected, endpoint, bucket)
  for (const row of before.objects) {
    assert.equal(row.State, 'ready', 'Captured object is not ready')
    assert.equal(row.UploadConfirmed, true, 'Captured upload is not confirmed')
    assert.ok(row.version_id, 'Recorded exact S3 version missing')
    assert.equal(row.RevisionID, saved.revision.id, 'Captured storage revision changed')
    const bytes = row.MIME === 'image/png' ? png : pdf
    assert.equal(row.SHA256, hash(bytes), 'Captured attachment digest changed')
    assert.equal(row.Size, bytes.length, 'Captured attachment size changed')
    assert.equal(
      Date.parse(row.NextCleanupAt) - Date.parse(row.CreatedAt),
      3600000,
      'Stored one-hour deadline changed',
    )
    assert.equal(row.ExpiresAt, null, 'Personal/Project object borrowed Team expiry field')
  }
  const requireFacts = (facts) => {
    assert.equal(facts.calls.length, requestIDs.length, 'Call census changed')
    assert.equal(facts.attempts.length, requestIDs.length, 'Attempt census changed')
    for (const row of facts.attempts) {
      assert.ok(requestIDs.includes(row.RequestID), 'Attempt request identity changed')
      assert.equal(row.CredentialID, credentialID, 'Credential attribution changed')
      assert.ok(row.SnapshotID, 'Published snapshot absent')
      assert.equal(row.ProviderID, provider.id, 'Provider attribution changed')
      assert.equal(row.ConnectionID, connection.id, 'Connection attribution changed')
      assert.equal(row.ProviderModelID, native.id, 'Native Model attribution changed')
      assert.equal(row.NativeCompletionEvidence, 'completed', 'Native terminal proof absent')
      assert.equal(row.Status, 'success', 'Attempt not successful')
    }
    for (const row of facts.calls) {
      assert.ok(requestIDs.includes(row.RequestID), 'Call request identity changed')
      assert.equal(row.Status, 'success', 'Call not successful')
      assert.equal(row.InputTokens, 3, 'Persisted settled input usage changed')
      assert.equal(row.OutputTokens, 2, 'Persisted settled output usage changed')
      assert.equal(row.ImageInputs, 1, 'Persisted image occurrence changed')
      assert.equal(row.PDFInputs, 1, 'Persisted PDF occurrence changed')
      assert.equal(row.ModelID, model.id, 'Logical Model changed')
      assert.equal(
        row.ProjectID,
        row.KeyID === projectKey.key.id ? projectRecord.id : '',
        'Immutable call scope changed',
      )
      assert.ok(row.SnapshotID, 'Call snapshot absent')
      assert.ok(
        row.KeyID === personalKey.key.id || row.KeyID === projectKey.key.id,
        'Key scope changed',
      )
    }
    requireNoSecrets(Buffer.from(JSON.stringify(facts)), [
      credential,
      personalKey.secret,
      projectKey.secret,
      fixtureEnv.ROUTEX_ACCEPTANCE_SECRET_KEY,
    ])
  }
  requireFacts(before)
  const later = await fixture(
    'later',
    { objects: before.objects, request_ids: [] },
    endpoint,
    bucket,
  )
  for (const row of personal)
    assert.deepEqual(
      (
        await request(origin, `/api/v1/attachments/${row.id}/content`, 200, {
          cookie,
          raw: true,
        })
      ).value,
      row.mime === 'image/png' ? png : pdf,
      'Mutable latest version was read',
    )
  await inference(personalKey, personal)
  await inference(projectKey, projectFiles, 200, projectRecord.id)
  const firstPID = application.entry.pid
  const retained = await fixture(
    'snapshot',
    { objects: before.objects, request_ids: requestIDs },
    endpoint,
    bucket,
  )
  requireFacts(retained)
  await stopApplication()
  await launch()
  assert.notEqual(application.entry.pid, firstPID, 'Restart did not create a new process')
  const restored = (await request(origin, '/api/v1/auth/session', 200, { cookie })).value
  assert.equal(restored.user.id, session.user.id, 'Original Session was not retained')
  csrf = restored.csrf_token
  const after = await fixture(
    'snapshot',
    { objects: before.objects, request_ids: requestIDs },
    endpoint,
    bucket,
  )
  assert.deepEqual(after, retained, 'Restart changed immutable storage/call facts')
  await inference(personalKey, personal)
  await inference(projectKey, projectFiles, 200, projectRecord.id)
  results.push(
    'retained original Session, exact S3 versions and native call/attempt facts after restart',
  )
  await docker('storage-outage', [...compose, 'stop', '--timeout', '10', 'objectstore'])
  for (const row of personal)
    requireDeletionResponse(await write(`/attachments/${row.id}`, undefined, 200, 'DELETE'), row)
  for (const row of projectFiles)
    requireDeletionResponse(await write(`${projectPath}/${row.id}`, undefined, 200, 'DELETE'), row)
  const deletionApplication = application
  await stopApplication()
  assert.ok(
    deletionApplication.entry.joined && deletionApplication.entry.group_gone,
    'Deletion application shutdown was not positively joined',
  )
  const pending = await fixture(
    'snapshot',
    { objects: before.objects, request_ids: requestIDs },
    endpoint,
    bucket,
  )
  requirePendingSnapshot(pending, before.objects)
  requireFacts(pending)
  await save('delete-pending-after-join.json', {
    application_receipt: deletionApplication.entry,
    facts: pending,
  })
  await docker(
    'storage-recovery',
    [...compose, 'start', '--wait', '--wait-timeout', '60', 'objectstore'],
    { timeout: 70_000 },
  )
  await launch()
  csrf = (await request(origin, '/api/v1/auth/session', 200, { cookie })).value.csrf_token
  const cleanupLimit = Math.min(workDeadline, performance.now() + 120_000)
  let deleted
  while (performance.now() < cleanupLimit) {
    deleted = await fixture(
      'snapshot',
      { objects: before.objects, request_ids: requestIDs },
      endpoint,
      bucket,
    )
    if (deleted.objects.every((row) => row.State === 'deleted')) break
    await delay(1000)
  }
  assert.ok(
    deleted.objects.every((row) => row.State === 'deleted'),
    'Durable deletion did not recover',
  )
  const cleanup = await fixture(
    'cleanup',
    { objects: before.objects, request_ids: requestIDs, later: later.later },
    endpoint,
    bucket,
  )
  assert.ok(
    cleanup.exact_versions_absent &&
      cleanup.later_versions_preserved &&
      cleanup.delete_markers_absent,
    'Exact-version cleanup proof absent',
  )
  requireFacts(cleanup)
  await inference(personalKey, personal, 404)
  await inference(projectKey, projectFiles, 404)
  assert.equal(nativeCalls, 6, 'Successful native census changed')
  assert.equal(peerFailure, false, 'Controlled native assertion failed')
  results.push(
    'outage durable deletion/restart recovery removes exact originals and preserves later versions without markers',
  )
} catch {
  failureStage = stage
  process.exitCode = 1
} finally {
  cleanupDeadline = Math.min(absoluteDeadline, performance.now() + 90_000)
  const cleanupErrors = []
  try {
    await stopApplication()
  } catch {
    cleanupErrors.push('application_join_failed')
  }
  if (peer) {
    try {
      peer.closeAllConnections()
      await Promise.race([
        new Promise((done, reject) => peer.close((error) => (error ? reject(error) : done()))),
        delay(Math.min(5000, remaining())).then(() => {
          throw new Error('Peer close deadline')
        }),
      ])
      peer = undefined
    } catch {
      cleanupErrors.push('peer_join_failed')
    }
  }
  for (const state of children.filter((row) => !row.entry.group_gone)) {
    try {
      await joinChild(state, Math.min(cleanupDeadline, performance.now() + 5000), true)
    } catch {
      cleanupErrors.push('owned_child_join_failed')
    }
  }
  if (composeStarted && children.every((row) => row.entry.group_gone)) {
    try {
      await docker(
        'compose-down',
        [...compose, 'down', '--volumes', '--remove-orphans', '--timeout', '10'],
        { timeout: 30_000 },
      )
    } catch {
      cleanupErrors.push('compose_down_failed')
    }
  }
  if (composeStarted)
    for (const kind of ['container', 'network', 'volume']) {
      try {
        const args = kind === 'container' ? ['ps', '-aq'] : [kind, 'ls', '-q']
        assert.equal(
          (
            await docker('project-cleanup-readback', [
              ...args,
              '--filter',
              `label=com.docker.compose.project=${project}`,
            ])
          )
            .toString()
            .trim(),
          '',
          'Exact project resource remains',
        )
      } catch {
        cleanupErrors.push(`${kind}_remains`)
      }
    }
  for (const value of ports) {
    try {
      const refused = await new Promise((done) => {
        const socket = createConnection({ host: '127.0.0.1', port: value })
        socket.once('connect', () => {
          socket.destroy()
          done(false)
        })
        socket.once('error', (error) => {
          socket.destroy()
          done(error.code === 'ECONNREFUSED')
        })
        socket.setTimeout(500, () => {
          socket.destroy()
          done(false)
        })
      })
      assert.ok(refused, 'Owned port still accepts connections')
      const listener = createServer()
      await new Promise((done, reject) => {
        listener.once('error', reject)
        listener.listen(value, '127.0.0.1', () =>
          listener.close((error) => (error ? reject(error) : done())),
        )
      })
    } catch {
      cleanupErrors.push('port_not_closed_and_bindable')
    }
  }
  try {
    if (inputsBefore) {
      const after = await sourceDigest()
      await save('source-after.json', after)
      const assets = await assetsDigest(repo)
      await save('assets-after.json', assets)
      requireInputs(inputsBefore, {
        source_digest: after.digest,
        binary_sha256: (await executableReceipt(binary.path)).sha256,
        helper_sha256: (await executableReceipt(helper.path)).sha256,
        assets_digest: assets.digest,
      })
    }
  } catch {
    cleanupErrors.push('input_changed')
  }
  const passed = !failureStage && !interrupted && cleanupErrors.length === 0
  process.exitCode = passed ? 0 : 1
  for (const state of children) {
    try {
      await writeFile(
        join(output, 'logs', `${children.indexOf(state)}.stdout`),
        Buffer.concat(state.output),
        { mode: 0o600, flag: 'wx' },
      )
      await writeFile(
        join(output, 'logs', `${children.indexOf(state)}.stderr`),
        Buffer.concat(state.stderr),
        { mode: 0o600, flag: 'wx' },
      )
    } catch {
      process.exitCode = 1
      cleanupErrors.push('private_log_write_failed')
    }
  }
  try {
    await save('result.json', {
      status: process.exitCode === 0 ? 'passed' : 'failed',
      project,
      failure_stage: failureStage ?? null,
      interrupted,
      results,
      native_calls: nativeCalls,
      process_receipts: children.map((row) => row.entry),
      cleanup_errors: cleanupErrors,
      ports: [...ports],
      binary_sha256: binary.sha256,
      helper_sha256: helper.sha256,
      assets_digest: inputsBefore?.assets_digest,
    })
  } catch {
    process.exitCode = 1
  }
  console.log(
    `Local native storage acceptance ${process.exitCode === 0 ? 'passed' : 'failed'}; private artifacts: ${output}`,
  )
}
