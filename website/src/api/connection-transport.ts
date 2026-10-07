export type ConnectionAdapter = 'native' | 'azure_openai_classic'
export function validAzureAPIVersion(value: unknown): value is string {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}(?:-preview)?$/.test(value)) return false
  const date = value.slice(0, 10)
  const parsed = new Date(`${date}T00:00:00Z`)
  return (
    Number.isFinite(parsed.valueOf()) &&
    parsed.toISOString().slice(0, 10) === date &&
    !date.startsWith('0000-')
  )
}
export function validAzureOrigin(value: string) {
  // Check the original authority/path before WHATWG URL normalization removes dot segments or controls.
  if (!/^https?:\/\/[^/?#\\%\s\p{Cc}\p{Cf}\p{Cs}]+\/?$/iu.test(value)) return false
  try {
    const url = new URL(value)
    return (
      ['https:', 'http:'].includes(url.protocol) &&
      !!url.hostname &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash &&
      (url.pathname === '/' || url.pathname === '') &&
      !/[\\%]/.test(value) &&
      value.trim() === value
    )
  } catch {
    return false
  }
}
export function decodeConnectionTransport(value: {
  adapter?: unknown
  api_version?: unknown
  protocol?: unknown
  base_url?: unknown
}) {
  if (value.adapter === 'native' && value.api_version === null)
    return { adapter: 'native' as const, api_version: null }
  if (
    value.adapter === 'azure_openai_classic' &&
    value.protocol === 'openai_chat' &&
    validAzureAPIVersion(value.api_version) &&
    typeof value.base_url === 'string' &&
    validAzureOrigin(value.base_url)
  )
    return { adapter: 'azure_openai_classic' as const, api_version: value.api_version }
  throw new Error('Connection transport response unavailable')
}
