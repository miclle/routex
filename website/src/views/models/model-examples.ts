import type { ModelAccessSource, ModelCatalogRecord } from '@/types/model-catalog'
import type { PlaygroundProtocol } from '@/types/playground'
import { buildTeamPlaygroundSnippet } from '@/lib/playground-team-snippet'
import { isGeminiModelName } from '@/lib/protocols'
import {
  knownModelProtocols,
  personallyAvailable,
  teamInvocationProtocols,
} from './catalogue-metadata'

export function exampleProtocols(model: ModelCatalogRecord, source?: ModelAccessSource) {
  return source?.type === 'team' ? teamInvocationProtocols(source) : knownModelProtocols(model)
}

export function modelExample(
  model: ModelCatalogRecord,
  source: ModelAccessSource | undefined,
  protocol: string | undefined,
  origin: string,
) {
  if (!source || !protocol || !exampleProtocols(model, source).includes(protocol)) return undefined
  if (protocol === 'gemini_generate_content' && !isGeminiModelName(model.name)) return undefined
  if (source.type === 'team')
    return buildTeamPlaygroundSnippet(
      {
        source: 'team',
        teamId: source.team_id,
        origin,
        protocol: protocol as PlaygroundProtocol,
        model: model.name,
        stream: false,
        temperature: 0.7,
        topP: 1,
        maxTokens: 1024,
        system: '',
        messages: [{ role: 'user', content: 'Hello' }],
      },
      'curl',
    )
  if (!personallyAvailable(model)) return undefined
  const gemini = protocol === 'gemini_generate_content'
  const endpoint = `${origin}/${gemini ? 'v1beta' : 'v1'}`
  const path = gemini
    ? `models/${encodeURIComponent(model.name)}:generateContent`
    : protocol === 'anthropic_messages'
      ? 'messages'
      : protocol === 'openai_responses'
        ? 'responses'
        : 'chat/completions'
  const body = gemini
    ? { contents: [{ role: 'user', parts: [{ text: 'Hello' }] }] }
    : protocol === 'openai_responses'
      ? { model: model.name, input: 'Hello' }
      : {
          model: model.name,
          ...(protocol === 'anthropic_messages' ? { max_tokens: 1024 } : {}),
          messages: [{ role: 'user', content: 'Hello' }],
        }
  const auth = gemini
    ? 'x-goog-api-key: $ROUTEX_API_KEY'
    : protocol === 'anthropic_messages'
      ? 'x-api-key: $ROUTEX_API_KEY'
      : 'Authorization: Bearer $ROUTEX_API_KEY'
  return [
    `curl ${endpoint}/${path}`,
    ...[
      auth,
      ...(protocol === 'anthropic_messages' ? ['anthropic-version: 2023-06-01'] : []),
      'Content-Type: application/json',
    ].map((header) => `  -H "${header}"`),
    `  -d '${JSON.stringify(body).replace(/'/g, "'\\''")}'`,
  ].join(' \\\n')
}
