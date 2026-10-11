import references from '../../../internal/routex/modelreferences/public-model-references.v2.json'

import type { PublicModelReference, ModelPublicNameReference } from '@/types/model-public-names'

const namePattern = /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$(?![\s\S])/
const sourceHosts = new Set(['developers.openai.com', 'platform.claude.com', 'ai.google.dev'])

function exactKeys(value: object, keys: string[]) {
  const actual = Object.keys(value)
  return actual.length === keys.length && actual.every((key) => keys.includes(key))
}

// Invalid reference metadata disables assistance, never the custom name field.
export function readPublicModelReferences(value: unknown): readonly PublicModelReference[] {
  if (!value || typeof value !== 'object' || !exactKeys(value, ['version', 'entries'])) return []
  const file = value as { version?: unknown; entries?: unknown }
  if (file.version !== 2 || !Array.isArray(file.entries) || file.entries.length > 100) return []
  const seen = new Set<string>()
  const entries: PublicModelReference[] = []
  for (const row of file.entries) {
    if (
      !row ||
      typeof row !== 'object' ||
      !exactKeys(row, ['name', 'aliases', 'protocols', 'capabilities', 'source_url', 'reviewed_at'])
    )
      return []
    const entry = row as Record<string, unknown>
    if (
      typeof entry.name !== 'string' ||
      !namePattern.test(entry.name) ||
      seen.has(entry.name) ||
      typeof entry.source_url !== 'string' ||
      entry.source_url.length > 256 ||
      typeof entry.reviewed_at !== 'string' ||
      !/^\d{4}-\d{2}-\d{2}$/.test(entry.reviewed_at)
    )
      return []
    const finiteStrings = (
      value: unknown,
      max: number,
      valid: (item: string) => boolean,
    ): value is string[] =>
      Array.isArray(value) &&
      value.length <= max &&
      value.every((item) => typeof item === 'string' && valid(item)) &&
      new Set(value.map((item: string) => item.toLowerCase())).size === value.length
    if (
      !finiteStrings(
        entry.aliases,
        8,
        (alias) =>
          alias.length > 0 &&
          alias.trim() === alias &&
          new TextEncoder().encode(alias).length <= 128 &&
          new TextDecoder().decode(new TextEncoder().encode(alias)) === alias &&
          !/\p{Cc}/u.test(alias) &&
          alias.toLowerCase() !== (entry.name as string).toLowerCase(),
      ) ||
      !finiteStrings(entry.protocols, 4, (protocol) =>
        [
          'openai_chat',
          'openai_responses',
          'anthropic_messages',
          'gemini_generate_content',
        ].includes(protocol),
      ) ||
      !finiteStrings(entry.capabilities, 2, (capability) =>
        ['image_input', 'pdf_input'].includes(capability),
      )
    )
      return []
    const date = new Date(entry.reviewed_at)
    if (!Number.isFinite(date.getTime()) || date.toISOString().slice(0, 10) !== entry.reviewed_at)
      return []
    try {
      const url = new URL(entry.source_url)
      if (
        url.href !== entry.source_url ||
        url.protocol !== 'https:' ||
        !sourceHosts.has(url.hostname) ||
        url.port ||
        url.username ||
        url.password ||
        url.search ||
        url.hash ||
        entry.source_url.includes('?') ||
        entry.source_url.includes('#')
      )
        return []
    } catch {
      return []
    }
    seen.add(entry.name)
    entries.push(
      Object.freeze({
        name: entry.name,
        aliases: Object.freeze([...entry.aliases]),
        protocols: Object.freeze([...entry.protocols]),
        capabilities: Object.freeze([...entry.capabilities]),
        source_url: entry.source_url,
        reviewed_at: entry.reviewed_at,
      }),
    )
  }
  return Object.freeze(entries)
}

export const publicModelReferences = readPublicModelReferences(references)

export function searchPublicModelNames(
  query: string,
  excludedNames: readonly string[] = [],
  entries: readonly PublicModelReference[] = publicModelReferences,
): string[] {
  if (
    new TextEncoder().encode(query).length > 128 ||
    new TextDecoder().decode(new TextEncoder().encode(query)) !== query ||
    /\p{Cc}/u.test(query)
  )
    return []
  const needle = query.toLowerCase()
  return entries
    .filter(
      (entry) =>
        !excludedNames.includes(entry.name) &&
        [entry.name, ...entry.aliases].some((term) => term.toLowerCase().includes(needle)),
    )
    .slice(0, 8)
    .map((entry) => entry.name)
}

export function publicModelReferenceMetadata(name: string): ModelPublicNameReference | undefined {
  const entry = publicModelReferences.find((entry) => entry.name === name)
  if (!entry) return undefined
  return Object.freeze({
    aliases: entry.aliases,
    protocols: entry.protocols,
    capabilities: entry.capabilities,
    source_url: entry.source_url,
    reviewed_at: entry.reviewed_at,
  })
}
