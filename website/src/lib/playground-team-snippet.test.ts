/// <reference types="node" />
import { spawn } from 'node:child_process'
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http'
import { afterEach, describe, expect, it } from 'vitest'
import {
  buildTeamPlaygroundSnippet,
  teamSnippetRequest,
  type TeamSnippetInput,
} from './playground-team-snippet'
import { nativeSnippetRequest, type SnippetLanguage } from './playground-snippet'
import type { PlaygroundProtocol } from '@/types/playground'

const input: TeamSnippetInput = {
  source: 'team',
  teamId: 'tem_ExactCase',
  origin: 'https://gateway.example.invalid',
  protocol: 'openai_chat',
  model: 'native-model',
  stream: true,
  temperature: 0.4,
  topP: 0.8,
  maxTokens: 42,
  system: "系统\nDon't expand $HOME or `id` or $(printf BAD)",
  messages: [
    { role: 'user', content: 'First completed turn' },
    { role: 'assistant', content: 'Completed answer' },
    {
      role: 'user',
      content: '你好\nO\'Brien "quoted" \\ slash $(printf INJECTED) `id`\nROUTEX_TEAM_EXAMPLE',
    },
  ],
}
const languages: SnippetLanguage[] = ['curl', 'python', 'javascript']
const protocols: PlaygroundProtocol[] = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_generate_content',
]
const email = 'snippet-reader@example.invalid'
const password = 'Private\'"\\\n$(printf PASSWORD_INJECTED) `id`'
const cookie = 'routex_session=' + 's'.repeat(43)
const csrf = 'a'.repeat(64)
const environment = {
  ...process.env,
  ROUTEX_EMAIL: email,
  ROUTEX_PASSWORD: password,
  ROUTEX_API_KEY: 'rx_decoy_never_use',
  NO_PROXY: '127.0.0.1',
  no_proxy: '127.0.0.1',
}
type Capture = { path: string; headers: IncomingMessage['headers']; raw: string }
type FixtureOptions = {
  loginStatus?: number
  sessionStatus?: number
  sessionBody?: unknown
  rawSession?: string
  cookies?: string[]
  nativeStatus?: number
}
const servers: ReturnType<typeof createServer>[] = []
afterEach(async () => {
  for (const server of servers.splice(0)) {
    server.closeAllConnections()
    await new Promise<void>((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve())),
    )
  }
})
async function fixture(options: FixtureOptions = {}) {
  const calls: Capture[] = []
  let dispatches = 0
  const output = 'native output\n你好\n'
  const server = createServer(async (req: IncomingMessage, res: ServerResponse) => {
    const chunks: Buffer[] = []
    for await (const chunk of req) chunks.push(chunk as Buffer)
    const raw = Buffer.concat(chunks).toString('utf8')
    calls.push({ path: req.url ?? '', headers: req.headers, raw })
    if (req.url === '/api/v1/auth/login') {
      res.statusCode = options.loginStatus ?? 200
      if (res.statusCode === 302) res.setHeader('Location', '/redirect-must-not-follow')
      res.setHeader(
        'Set-Cookie',
        options.cookies ?? [cookie + '; Path=/; HttpOnly; SameSite=Strict'],
      )
      res.end(
        JSON.stringify({ private_auth_body: 'never_print_auth_body', csrf_token: 'b'.repeat(64) }),
      )
      return
    }
    if (req.url === '/api/v1/auth/session') {
      res.statusCode = options.sessionStatus ?? 200
      if (res.statusCode === 302) res.setHeader('Location', '/redirect-must-not-follow')
      res.end(
        options.rawSession ??
          JSON.stringify(
            options.sessionBody ?? {
              user: { id: 'usr_current', email, role: 'member' },
              csrf_token: csrf,
            },
          ),
      )
      return
    }
    res.statusCode = options.nativeStatus ?? 200
    if (res.statusCode === 302) res.setHeader('Location', '/redirect-must-not-follow')
    if (req.url !== '/redirect-must-not-follow' && res.statusCode >= 200 && res.statusCode < 300)
      dispatches++
    res.end(output)
  })
  servers.push(server)
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const address = server.address()
  if (!address || typeof address === 'string') throw new Error('Missing fixture address')
  return { origin: `http://127.0.0.1:${address.port}`, calls, output, dispatches: () => dispatches }
}
async function execute(code: string, language: SnippetLanguage, env = environment) {
  // Program and all credentials arrive through stdin/environment, never argv.
  const command =
    language === 'curl' ? '/bin/sh' : language === 'python' ? 'python3' : process.execPath
  const args =
    language === 'curl' ? [] : language === 'python' ? ['-'] : ['--input-type=module', '-']
  const child = spawn(command, args, { env, stdio: ['pipe', 'pipe', 'pipe'] })
  const out: Buffer[] = [],
    errors: Buffer[] = []
  child.stdout.on('data', (chunk: Buffer) => out.push(chunk))
  child.stderr.on('data', (chunk: Buffer) => errors.push(chunk))
  child.stdin.end(code)
  const status = await new Promise<number | null>((resolve, reject) => {
    const timer = setTimeout(() => {
      child.kill()
      reject(new Error('Generated example exceeded its execution bound'))
    }, 10000)
    child.once('error', (error) => {
      clearTimeout(timer)
      reject(error)
    })
    child.once('exit', (value) => {
      clearTimeout(timer)
      resolve(value)
    })
  })
  return {
    status,
    stdout: Buffer.concat(out).toString('utf8'),
    stderr: Buffer.concat(errors).toString('utf8'),
    argv: child.spawnargs,
  }
}
function assertPrivate(result: Awaited<ReturnType<typeof execute>>) {
  for (const secret of [
    password,
    cookie,
    csrf,
    'never_print_auth_body',
    environment.ROUTEX_API_KEY,
  ]) {
    expect(result.stdout).not.toContain(secret)
    expect(result.stderr).not.toContain(secret)
    expect(result.argv.join(' ')).not.toContain(secret)
  }
}

describe.each(languages)('%s Team Session code execution', (language) => {
  it.each(protocols.flatMap((protocol) => [false, true].map((stream) => ({ protocol, stream }))))(
    'logs in and invokes exact $protocol (stream=$stream) once with current CSRF and literal payload',
    async ({ protocol, stream }) => {
      const stub = await fixture()
      const descriptor = { ...input, origin: stub.origin, protocol, stream }
      const code = buildTeamPlaygroundSnippet(descriptor, language)
      const result = await execute(code, language)
      expect(result.status, result.stderr).toBe(0)
      expect(result.stdout).toBe(stub.output)
      assertPrivate(result)
      expect(stub.calls.map((call) => call.path)).toEqual([
        '/api/v1/auth/login',
        '/api/v1/auth/session',
        new URL(teamSnippetRequest(descriptor).url).pathname +
          new URL(teamSnippetRequest(descriptor).url).search,
      ])
      expect(stub.dispatches()).toBe(1)
      expect(JSON.parse(stub.calls[0].raw)).toEqual({ email, password })
      expect(stub.calls[0].headers.cookie).toBeUndefined()
      expect(stub.calls[1].raw).toBe('')
      expect(stub.calls[1].headers.cookie).toBe(cookie)
      expect(JSON.parse(stub.calls[2].raw)).toEqual(nativeSnippetRequest(descriptor).body)
      for (const call of stub.calls) {
        expect(call.headers.origin).toBe(stub.origin)
        expect(call.headers.authorization).toBeUndefined()
        expect(call.headers['x-api-key']).toBeUndefined()
        expect(call.headers['x-goog-api-key']).toBeUndefined()
      }
      expect(stub.calls[2].headers.cookie).toBe(cookie)
      expect(stub.calls[2].headers['x-csrf-token']).toBe(csrf)
      expect(stub.calls[2].headers['anthropic-version']).toBe(
        protocol === 'anthropic_messages' ? '2023-06-01' : undefined,
      )
      expect(code).not.toContain('ROUTEX_API_KEY')
      expect(code).not.toContain('document.cookie')
    },
  )
  const denied: { name: string; options: FixtureOptions; calls: number }[] = [
    ...[201, 202, 302, 401, 403].map((loginStatus) => ({
      name: `login ${loginStatus}`,
      options: { loginStatus },
      calls: 1,
    })),
    ...[202, 302, 401, 403].map((sessionStatus) => ({
      name: `current Session ${sessionStatus}`,
      options: { sessionStatus },
      calls: 2,
    })),
    { name: 'missing Session cookie', options: { cookies: [] }, calls: 1 },
    {
      name: 'duplicate Session cookies',
      options: { cookies: [cookie + '; Path=/; HttpOnly', cookie + '; Path=/; HttpOnly'] },
      calls: 1,
    },
    {
      name: 'wrong cookie path',
      options: { cookies: [cookie + '; Path=/other; HttpOnly'] },
      calls: 1,
    },
    { name: 'malformed Session JSON', options: { rawSession: 'private-token-not-json' }, calls: 2 },
    { name: 'oversized Session body', options: { rawSession: 'x'.repeat(65537) }, calls: 2 },
    {
      name: 'missing Session CSRF',
      options: { sessionBody: { user: { id: 'usr_current', email, role: 'member' } } },
      calls: 2,
    },
    {
      name: 'unsafe Session CSRF',
      options: {
        sessionBody: {
          user: { id: 'usr_current', email, role: 'member' },
          csrf_token: csrf + '\r\nX-Injected: value',
        },
      },
      calls: 2,
    },
    {
      name: 'different Session actor',
      options: {
        sessionBody: {
          user: { id: 'usr_other', email: 'other@example.invalid', role: 'member' },
          csrf_token: csrf,
        },
      },
      calls: 2,
    },
    {
      name: 'numeric Session identity',
      options: { sessionBody: { user: { id: 123, email, role: 'member' }, csrf_token: csrf } },
      calls: 2,
    },
  ]
  it.each(denied)(
    'stops $name before inference without logging auth material or following redirects',
    async ({ options, calls }) => {
      const stub = await fixture(options)
      const result = await execute(
        buildTeamPlaygroundSnippet({ ...input, origin: stub.origin }, language),
        language,
      )
      expect(result.status).not.toBe(0)
      expect(result.stdout).toBe('')
      assertPrivate(result)
      expect(result.stderr).not.toContain('private-token-not-json')
      expect(stub.calls).toHaveLength(calls)
      expect(stub.dispatches()).toBe(0)
      expect(stub.calls.every((call) => call.path.startsWith('/api/v1/auth/'))).toBe(true)
      if (options.loginStatus === 202)
        expect(result.stderr).toContain('Two-step login challenge required')
    },
  )
  it.each([302, 401, 403])('does not retry or follow a native %s denial', async (nativeStatus) => {
    const stub = await fixture({ nativeStatus })
    const result = await execute(
      buildTeamPlaygroundSnippet({ ...input, origin: stub.origin }, language),
      language,
    )
    expect(result.status).not.toBe(0)
    expect(result.stdout).toBe('')
    assertPrivate(result)
    expect(stub.calls).toHaveLength(3)
    expect(stub.dispatches()).toBe(0)
  })
  it('requires independent environment login credentials even when a Key is supplied', async () => {
    const stub = await fixture()
    const result = await execute(
      buildTeamPlaygroundSnippet({ ...input, origin: stub.origin }, language),
      language,
      { ...environment, ROUTEX_PASSWORD: '' },
    )
    expect(result.status).not.toBe(0)
    expect(result.stderr).toContain('Set ROUTEX_EMAIL and ROUTEX_PASSWORD')
    expect(stub.calls).toHaveLength(0)
    assertPrivate(result)
  })
})

it.each(['', 'tem_one ', ' tem_one', 'tem/one', 'a'.repeat(31), 'tem_one\n', 'tem_one?user=other'])(
  'rejects unsafe or aliased Team %s before generating a program',
  (teamId) => {
    for (const language of languages)
      expect(() => buildTeamPlaygroundSnippet({ ...input, teamId }, language)).toThrow()
  },
)
it('keeps the descriptor nonsecret and rejects invalid origin/protocol/Gemini identity', () => {
  const extra = {
    ...input,
    apiKey: 'rx_private',
    cookie: 'private_cookie',
    csrf: 'private_csrf',
    email: 'private_email',
    password: 'private_password',
  }
  for (const language of languages) {
    const code = buildTeamPlaygroundSnippet(extra, language)
    for (const secret of [extra.apiKey, extra.cookie, extra.csrf, extra.email, extra.password])
      expect(code).not.toContain(secret)
    expect(() =>
      buildTeamPlaygroundSnippet(
        { ...input, origin: 'https://user:secret@gateway.example.invalid' },
        language,
      ),
    ).toThrow()
    expect(() =>
      buildTeamPlaygroundSnippet(
        { ...input, protocol: 'unsupported' as PlaygroundProtocol },
        language,
      ),
    ).toThrow()
    expect(() =>
      buildTeamPlaygroundSnippet(
        { ...input, protocol: 'gemini_generate_content', model: 'vendor/model' },
        language,
      ),
    ).toThrow()
    expect(() =>
      buildTeamPlaygroundSnippet(
        { ...input, source: 'key' } as unknown as TeamSnippetInput,
        language,
      ),
    ).toThrow()
  }
})
