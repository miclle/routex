import { isGeminiModelName } from './protocols'
import type { PlaygroundProtocol } from '@/types/playground'
export type SnippetLanguage = 'curl' | 'python' | 'javascript'
export interface SnippetInput {
  origin: string
  protocol: PlaygroundProtocol
  model: string
  stream: boolean
  temperature: number
  topP: number
  maxTokens: number
  system: string
  messages: { role: 'user' | 'assistant'; content: string }[]
}
export class SnippetError extends Error {}
export function nativeSnippetRequest(input: SnippetInput) {
  const origin = new URL(input.origin)
  if (!['http:', 'https:'].includes(origin.protocol) || origin.username || origin.password)
    throw new SnippetError('origin')
  if (input.protocol === 'gemini_generate_content' && !isGeminiModelName(input.model))
    throw new SnippetError('model')
  const parameters = {
    model: input.model,
    stream: input.stream,
    temperature: input.temperature,
    top_p: input.topP,
  }
  const system = input.system.trim()
  let path: string, body: object
  switch (input.protocol) {
    case 'openai_chat':
      path = '/v1/chat/completions'
      body = {
        ...parameters,
        messages: [...(system ? [{ role: 'system', content: system }] : []), ...input.messages],
        max_tokens: input.maxTokens,
        ...(input.stream ? { stream_options: { include_usage: true } } : {}),
      }
      break
    case 'openai_responses':
      path = '/v1/responses'
      body = {
        ...parameters,
        input: input.messages,
        ...(system ? { instructions: system } : {}),
        max_output_tokens: input.maxTokens,
      }
      break
    case 'anthropic_messages':
      path = '/v1/messages'
      body = {
        ...parameters,
        messages: input.messages,
        ...(system ? { system } : {}),
        max_tokens: input.maxTokens,
      }
      break
    case 'gemini_generate_content':
      path = `/v1beta/models/${encodeURIComponent(input.model)}:${input.stream ? 'streamGenerateContent?alt=sse' : 'generateContent'}`
      body = {
        contents: input.messages.map((message) => ({
          role: message.role === 'assistant' ? 'model' : 'user',
          parts: [{ text: message.content }],
        })),
        ...(system ? { systemInstruction: { parts: [{ text: system }] } } : {}),
        generationConfig: {
          temperature: input.temperature,
          topP: input.topP,
          maxOutputTokens: input.maxTokens,
          candidateCount: 1,
        },
      }
      break
    default:
      throw new SnippetError('protocol')
  }
  const authHeader =
    input.protocol === 'anthropic_messages'
      ? 'x-api-key'
      : input.protocol === 'gemini_generate_content'
        ? 'x-goog-api-key'
        : 'Authorization'
  const prefix = authHeader === 'Authorization' ? 'Bearer ' : ''
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(input.protocol === 'anthropic_messages' ? { 'anthropic-version': '2023-06-01' } : {}),
  }
  return { url: origin.origin + path, body, authHeader, prefix, headers }
}
export const shellQuote = (value: string) => `'${value.replaceAll("'", "'\"'\"'")}'`
export function buildPlaygroundSnippet(input: SnippetInput, language: SnippetLanguage): string {
  const request = nativeSnippetRequest(input),
    json = JSON.stringify(request.body, null, 2)
  if (language === 'curl')
    return [
      `curl --request POST${input.stream ? ' --no-buffer' : ''} ${shellQuote(request.url)}`,
      `  --header "${request.authHeader}: ${request.prefix}$ROUTEX_API_KEY"`,
      ...Object.entries(request.headers).map(
        ([name, value]) => `  --header ${shellQuote(`${name}: ${value}`)}`,
      ),
      `  --data-raw ${shellQuote(json)}`,
    ].join(' \\\n')
  if (language === 'javascript')
    return `// Run as an ES module with Node.js (native fetch).\nconst key = process.env.ROUTEX_API_KEY;\nif (!key) throw new Error("Set ROUTEX_API_KEY before running.");\n\nconst response = await fetch(${JSON.stringify(request.url)}, {\n  method: "POST",\n  credentials: "omit",\n  redirect: "error",\n  headers: {\n    ...${JSON.stringify(request.headers, null, 4)},\n    ${JSON.stringify(request.authHeader)}: ${JSON.stringify(request.prefix)} + key,\n  },\n  body: JSON.stringify(${json}),\n});\nif (!response.ok) throw new Error(\`HTTP \${response.status}\`);\nfor await (const chunk of response.body) {\n  process.stdout.write(chunk);\n}\n`
  return `# Run with Python 3; standard library only.\nimport json\nimport os\nimport sys\nimport urllib.request\n\nclass NoRedirect(urllib.request.HTTPRedirectHandler):\n    def redirect_request(self, req, fp, code, msg, headers, newurl):\n        return None\n\nkey = os.environ["ROUTEX_API_KEY"]\nheaders = ${JSON.stringify(request.headers, null, 4)}\nheaders[${JSON.stringify(request.authHeader)}] = ${JSON.stringify(request.prefix)} + key\npayload = json.loads(${JSON.stringify(json)})\nrequest = urllib.request.Request(\n    ${JSON.stringify(request.url)},\n    data=json.dumps(payload).encode("utf-8"),\n    headers=headers,\n    method="POST",\n)\nwith urllib.request.build_opener(NoRedirect()).open(request) as response:\n    for chunk in response:\n        sys.stdout.buffer.write(chunk)\n        sys.stdout.buffer.flush()\n`
}
