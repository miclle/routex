import { useEffect, useState } from 'react'
import { Route } from 'lucide-react'
import { useSite } from '@/hooks/use-site'
import { applyDefaultLanguage } from '@/i18n'

export function SitePresentation() {
  const site = useSite()
  useEffect(() => {
    if (!site.data) return
    document.title = site.data.name || 'RouteX'
    void applyDefaultLanguage(site.data.default_language)
  }, [site.data])
  return null
}
export function SiteLogo({ className = 'size-5' }: { className?: string }) {
  const site = useSite()
  const [failedURL, setFailedURL] = useState('')
  const raw = site.data?.logo_url ?? ''
  let safe = false
  try {
    const url = new URL(raw)
    safe = ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password && !url.hash
  } catch {
    /* Missing or malformed logos use the built-in mark. */
  }
  return safe && raw !== failedURL ? (
    <img
      src={raw}
      alt=""
      referrerPolicy="no-referrer"
      className={`${className} object-contain`}
      onError={() => setFailedURL(raw)}
    />
  ) : (
    <Route className={className} aria-hidden="true" />
  )
}
export function SiteFooter() {
  const site = useSite()
  return site.data?.footer ? (
    <footer className="shrink-0 whitespace-pre-wrap break-words px-4 py-3 text-center text-xs text-muted-foreground">
      {site.data.footer}
    </footer>
  ) : null
}
