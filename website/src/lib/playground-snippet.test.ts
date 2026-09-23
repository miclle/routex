/// <reference types="node" />
import { execFileSync, spawnSync } from 'node:child_process'
import { describe, expect, it } from 'vitest'
import {
  buildPlaygroundSnippet,
  nativeSnippetRequest,
  type SnippetInput,
} from './playground-snippet'
import type { PlaygroundProtocol } from '@/types/playground'
const input: SnippetInput = {
  origin: 'https://gateway.example.test',
  model: 'native-model',
  protocol: 'openai_chat',
  stream: true,
  temperature: 0.4,
  topP: 0.8,
  maxTokens: 42,
  system: "系统\nDon't expand $HOME or `id` or $(printf BAD)",
  messages: [
    { role: 'user', content: 'First' },
    { role: 'assistant', content: 'Answer' },
    {
      role: 'user',
      content: '你好\nO\'Brien "quoted" \\ slash $ROUTEX_API_KEY $(printf INJECTED) `id`',
    },
  ],
}
const protocols: PlaygroundProtocol[] = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_generate_content',
]
const pythonAvailable = spawnSync('python3', ['--version'], { stdio: 'ignore' }).status === 0
const env = { ...process.env, ROUTEX_API_KEY: 'synthetic_environment_key' }
describe('native request code generation', () => {
  it('keeps the exact single-model parameter and successful-history shapes for all protocols', () => {
    expect(nativeSnippetRequest(input).body).toEqual({
      model: input.model,
      stream: true,
      temperature: 0.4,
      top_p: 0.8,
      max_tokens: 42,
      messages: [{ role: 'system', content: input.system }, ...input.messages],
      stream_options: { include_usage: true },
    })
    expect(nativeSnippetRequest({ ...input, protocol: 'openai_responses' }).body).toEqual({
      model: input.model,
      stream: true,
      temperature: 0.4,
      top_p: 0.8,
      max_output_tokens: 42,
      instructions: input.system,
      input: input.messages,
    })
    expect(
      nativeSnippetRequest({ ...input, protocol: 'anthropic_messages', maxTokens: 0 }).body,
    ).toEqual({
      model: input.model,
      stream: true,
      temperature: 0.4,
      top_p: 0.8,
      max_tokens: 0,
      system: input.system,
      messages: input.messages,
    })
    expect(nativeSnippetRequest({ ...input, protocol: 'gemini_generate_content' }).body).toEqual({
      contents: [
        { role: 'user', parts: [{ text: 'First' }] },
        { role: 'model', parts: [{ text: 'Answer' }] },
        { role: 'user', parts: [{ text: input.messages[2].content }] },
      ],
      systemInstruction: { parts: [{ text: input.system }] },
      generationConfig: { temperature: 0.4, topP: 0.8, maxOutputTokens: 42, candidateCount: 1 },
    })
  })
  it.each(protocols)(
    'executes %s cURL shell quoting without expanding user content',
    (protocol) => {
      const request = { ...input, protocol },
        code = buildPlaygroundSnippet(request, 'curl')
      const args = execFileSync('/bin/sh', ['-c', `curl() { printf '%s\\0' "$@"; }\n${code}`], {
        env,
      })
        .toString()
        .split('\0')
        .slice(0, -1)
      expect(args).toContain(nativeSnippetRequest(request).url)
      expect(JSON.parse(args[args.indexOf('--data-raw') + 1])).toEqual(
        nativeSnippetRequest(request).body,
      )
      expect(args).toContain(
        `${nativeSnippetRequest(request).authHeader}: ${nativeSnippetRequest(request).prefix}synthetic_environment_key`,
      )
      expect(code).not.toContain('synthetic_environment_key')
      expect(code).toContain('$ROUTEX_API_KEY')
    },
  )
  it.each(protocols)(
    'executes %s JavaScript with a stub fetch and streams native bytes',
    (protocol) => {
      const request = { ...input, protocol },
        code = buildPlaygroundSnippet(request, 'javascript')
      const bootstrap = `globalThis.fetch = async (url, options) => { console.log(JSON.stringify({url,options})); return {ok:true,body:(async function*(){yield new TextEncoder().encode('native output')})()}; };\n`
      const [capture, output] = execFileSync(
        process.execPath,
        ['--input-type=module', '-e', bootstrap + code],
        { env },
      )
        .toString()
        .split('\n')
      const { url, options } = JSON.parse(capture)
      expect(url).toBe(nativeSnippetRequest(request).url)
      expect(JSON.parse(options.body)).toEqual(nativeSnippetRequest(request).body)
      expect(options.redirect).toBe('error')
      expect(options.credentials).toBe('omit')
      expect(options.headers[nativeSnippetRequest(request).authHeader]).toBe(
        nativeSnippetRequest(request).prefix + 'synthetic_environment_key',
      )
      expect(output).toBe('native output')
      expect(code).not.toContain('synthetic_environment_key')
    },
  )
  it.skipIf(!pythonAvailable).each(protocols)(
    'executes %s standard-library Python with literal JSON and no network (requires optional Python 3)',
    (protocol) => {
      const request = { ...input, protocol },
        code = buildPlaygroundSnippet(request, 'python')
      const bootstrap = `import json\nimport urllib.request\nclass FakeResponse:\n    def __enter__(self): return self\n    def __exit__(self, *args): pass\n    def __iter__(self): return iter([])\nclass FakeOpener:\n    def open(self, request):\n        print(json.dumps({"url":request.full_url,"headers":dict(request.header_items()),"body":json.loads(request.data.decode("utf-8"))}))\n        return FakeResponse()\ndef fake_opener(handler):\n    assert handler.redirect_request(None,None,302,None,{},"https://other.test") is None\n    return FakeOpener()\nurllib.request.build_opener=fake_opener\n`
      const capture = JSON.parse(
        execFileSync('python3', ['-c', bootstrap + code], { env }).toString(),
      )
      expect(capture.url).toBe(nativeSnippetRequest(request).url)
      expect(capture.body).toEqual(nativeSnippetRequest(request).body)
      const headers = Object.fromEntries(
        Object.entries(capture.headers).map(([key, value]) => [key.toLowerCase(), value]),
      )
      expect(headers[nativeSnippetRequest(request).authHeader.toLowerCase()]).toBe(
        nativeSnippetRequest(request).prefix + 'synthetic_environment_key',
      )
      expect(code).not.toContain('synthetic_environment_key')
    },
  )
  it('uses only protocol-specific authentication and native Gemini actions', () => {
    const messages = nativeSnippetRequest({ ...input, protocol: 'anthropic_messages' })
    expect(messages.authHeader).toBe('x-api-key')
    expect(messages.headers).toEqual({
      'Content-Type': 'application/json',
      'anthropic-version': '2023-06-01',
    })
    const gemini = nativeSnippetRequest({ ...input, protocol: 'gemini_generate_content' })
    expect(gemini.authHeader).toBe('x-goog-api-key')
    expect(gemini.url).toBe(
      'https://gateway.example.test/v1beta/models/native-model:streamGenerateContent?alt=sse',
    )
    expect(gemini.body).not.toHaveProperty('model')
    expect(gemini.body).not.toHaveProperty('stream')
    expect(
      nativeSnippetRequest({ ...input, protocol: 'gemini_generate_content', stream: false }).url,
    ).toContain(':generateContent')
    expect(nativeSnippetRequest({ ...input, stream: false }).body).not.toHaveProperty(
      'stream_options',
    )
  })
  it.each(['vendor/model', 'model:version', "model'unsafe", 'a'.repeat(129)])(
    'rejects unsupported Gemini identity %s before code generation',
    (model) => {
      expect(() =>
        buildPlaygroundSnippet({ ...input, protocol: 'gemini_generate_content', model }, 'curl'),
      ).toThrow()
    },
  )
  it('rejects credential-bearing origins and unsupported protocols instead of falling back', () => {
    expect(() =>
      buildPlaygroundSnippet({ ...input, origin: 'https://user:secret@example.test' }, 'python'),
    ).toThrow()
    expect(() =>
      buildPlaygroundSnippet(
        { ...input, protocol: 'unsupported' as PlaygroundProtocol },
        'javascript',
      ),
    ).toThrow()
  })
})

it('never copies an extra transient credential supplied alongside snippet parameters', () => {
  const supplied = { ...input, apiKey: 'rx_private_never_copy' }
  for (const language of ['curl', 'python', 'javascript'] as const) {
    const code = buildPlaygroundSnippet(supplied, language)
    expect(code).not.toContain(supplied.apiKey)
    expect(code).toContain('ROUTEX_API_KEY')
  }
})
