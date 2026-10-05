export function canonicalRegistrationDomain(value: string): string | null {
  const domain = value.replace(/^ +| +$/g, '').toLowerCase()
  if (
    !domain ||
    domain.length > 253 ||
    [...value].some((character) => character.charCodeAt(0) > 127)
  )
    return null
  const labels = domain.split('.')
  if (
    labels.length < 2 ||
    labels.some((label) => !/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label))
  )
    return null
  if (/^[0-9.]+$/.test(domain)) return null
  return domain
}

export function isCanonicalRegistrationDomains(value: unknown): value is string[] {
  return (
    Array.isArray(value) &&
    value.length <= 32 &&
    JSON.stringify(value).length <= 2048 &&
    value.every(
      (domain, index) =>
        typeof domain === 'string' &&
        canonicalRegistrationDomain(domain) === domain &&
        (index === 0 || value[index - 1] < domain),
    )
  )
}

export function addRegistrationDomain(
  domains: string[],
  input: string,
): { kind: 'added'; domains: string[] } | { kind: 'invalid' | 'duplicate' | 'limit' } {
  const domain = canonicalRegistrationDomain(input)
  if (!domain) return { kind: 'invalid' }
  if (domains.includes(domain)) return { kind: 'duplicate' }
  const next = [...domains, domain].sort()
  return isCanonicalRegistrationDomains(next) ? { kind: 'added', domains: next } : { kind: 'limit' }
}

export function sameRegistrationDomains(a: string[], b: string[]) {
  return a.length === b.length && a.every((domain, index) => domain === b[index])
}
