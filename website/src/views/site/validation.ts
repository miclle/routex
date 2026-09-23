import type { SiteInput } from '@/types/site'
export function normalizeSite(input: SiteInput): SiteInput {
  return {
    etag: input.etag,
    default_language: input.default_language,
    name: input.name.trim(),
    footer: input.footer.trim(),
    service_url: input.service_url.trim().replace(/\/+$/, ''),
    logo_url: input.logo_url.trim(),
  }
}
export function validSite(input: SiteInput) {
  const validURL = (value: string) => {
    if (!value) return true
    if (new TextEncoder().encode(value).length > 2048 || /[\r\n\t]/.test(value)) return false
    try {
      const url = new URL(value)
      return (
        ['http:', 'https:'].includes(url.protocol) &&
        !!url.hostname &&
        !url.username &&
        !url.password &&
        !url.hash
      )
    } catch {
      return false
    }
  }
  return (
    Array.from(input.name).length >= 1 &&
    Array.from(input.name).length <= 100 &&
    !/[\r\n\0]/.test(input.name) &&
    Array.from(input.footer).length <= 500 &&
    !input.footer.includes('\0') &&
    validURL(input.service_url) &&
    validURL(input.logo_url) &&
    ['en', 'zh'].includes(input.default_language)
  )
}
