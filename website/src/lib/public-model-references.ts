import references from '../../../internal/routex/modelreferences/public-model-references.v1.json'

interface PublicModelReference {
  readonly name: string
  readonly source_url: string
  readonly reviewed_at: string
}
const namePattern = /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/
const sourceHosts = new Set(['developers.openai.com', 'platform.claude.com', 'ai.google.dev'])

function exactKeys(value: object, keys: string[]) {
  const actual = Object.keys(value)
  return actual.length === keys.length && actual.every((key) => keys.includes(key))
}

// Invalid reference metadata disables assistance, never the custom name field.
export function readPublicModelReferences(value: unknown): readonly PublicModelReference[] {
  if (!value || typeof value !== 'object' || !exactKeys(value, ['version', 'entries'])) return []
  const file = value as { version?: unknown; entries?: unknown }
  if (file.version !== 1 || !Array.isArray(file.entries) || file.entries.length > 100) return []
  const seen = new Set<string>()
  const entries: PublicModelReference[] = []
  for (const row of file.entries) {
    if (!row || typeof row !== 'object' || !exactKeys(row, ['name', 'source_url', 'reviewed_at']))
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
        url.hash
      )
        return []
    } catch {
      return []
    }
    seen.add(entry.name)
    entries.push(
      Object.freeze({
        name: entry.name,
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
  if (query.length > 128) return []
  const needle = query.toLowerCase()
  return entries
    .filter(
      (entry) => !excludedNames.includes(entry.name) && entry.name.toLowerCase().includes(needle),
    )
    .slice(0, 8)
    .map((entry) => entry.name)
}
