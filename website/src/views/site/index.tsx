import { useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import axios from 'axios'
import { Info, Save } from 'lucide-react'
import { saveSite, siteKey } from '@/api/site'
import type { SiteSettings, SiteInput } from '@/types/site'
import { useSite } from '@/hooks/use-site'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { Card, CardHeader, CardContent, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { normalizeSite, validSite } from './validation'
const fields = ['name', 'service_url', 'logo_url', 'footer', 'default_language'] as const
const labels = {
  name: 'name',
  service_url: 'serviceURL',
  logo_url: 'logoURL',
  footer: 'footer',
  default_language: 'language',
}
const hints = {
  name: 'nameHint',
  service_url: 'serviceHint',
  logo_url: 'logoHint',
  footer: 'footerHint',
  default_language: 'languageHint',
}
function values(site: SiteInput) {
  return fields.map((field) => site[field])
}
export default function SitePage() {
  return (
    <PermissionGate permission="system.read">
      <SiteSettingsView />
    </PermissionGate>
  )
}
function SiteSettingsView() {
  const { t } = useTranslation('site')
  const site = useSite()
  const session = useSession()
  return (
    <Page title={t('title')} description={t('information')}>
      <QueryState pending={site.isPending} error={site.error} retry={() => void site.refetch()} />
      {site.data && (
        <Editor
          key={session.data?.user.id}
          incoming={site.data}
          reload={async () => {
            const next = await site.refetch()
            return next.isSuccess ? next.data : undefined
          }}
        />
      )}
    </Page>
  )
}
function Editor({
  incoming,
  reload,
}: {
  incoming: SiteSettings
  reload: () => Promise<SiteSettings | undefined>
}) {
  const { t } = useTranslation('site')
  const session = useSession()
  const access = usePermissions()
  const cache = useQueryClient()
  const [saved, setSaved] = useState(incoming)
  const [draft, setDraft] = useState<SiteInput>(incoming)
  const [busy, setBusy] = useState(false)
  const lock = useRef(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const stale = error === 'conflict' || incoming.etag !== saved.etag
  const dirty = JSON.stringify(values(draft)) !== JSON.stringify(values(saved))
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (lock.current || stale || !dirty || !access.can('site.write')) return
    const normalized = normalizeSite(draft)
    if (!validSite(normalized)) {
      setError('invalid')
      return
    }
    lock.current = true
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const result = await saveSite({ ...normalized, etag: saved.etag }, session.data!.csrf_token)
      setSaved(result)
      setDraft(result)
      cache.setQueryData(siteKey, result)
      setNotice('saved')
    } catch (cause) {
      const status = axios.isAxiosError(cause) ? cause.response?.status : 0
      setError(
        status === 409
          ? 'conflict'
          : status === 403
            ? 'denied'
            : status === 400
              ? 'invalid'
              : 'failed',
      )
    } finally {
      lock.current = false
      setBusy(false)
    }
  }
  async function review() {
    if (lock.current) return
    lock.current = true
    setBusy(true)
    try {
      const next = await reload()
      if (next) {
        setSaved(next)
        setError('')
        setNotice('reviewed')
      }
    } finally {
      lock.current = false
      setBusy(false)
    }
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2">
          <Info className="size-4" aria-hidden="true" />
          {t('information')}
          {dirty && (
            <span className="rounded border border-amber-500/40 px-2 py-1 text-xs font-normal text-amber-700 dark:text-amber-400">
              {t('dirty')}
            </span>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <form aria-label={t('information')} onSubmit={submit} className="space-y-5">
          <fieldset disabled={!access.can('site.write') || busy} className="space-y-5">
            {fields.map((field) => (
              <div key={field} className="grid gap-2 sm:grid-cols-[140px_1fr] sm:gap-6">
                <label htmlFor={`site-${field}`} className="pt-2 text-sm font-medium">
                  {t(labels[field])}
                  {field === 'name' && (
                    <span aria-hidden="true" className="ml-1 text-destructive">
                      *
                    </span>
                  )}
                </label>
                <div className="space-y-2">
                  {field === 'default_language' ? (
                    <select
                      id={`site-${field}`}
                      name={field}
                      value={draft[field]}
                      onChange={(event) => {
                        setDraft({ ...draft, default_language: event.target.value as 'en' | 'zh' })
                        setNotice('')
                      }}
                      className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                    >
                      <option value="en">{t('english')}</option>
                      <option value="zh">{t('chinese')}</option>
                    </select>
                  ) : (
                    <Input
                      id={`site-${field}`}
                      name={field}
                      value={draft[field]}
                      required={field === 'name'}
                      type={field.endsWith('_url') ? 'url' : 'text'}
                      onChange={(event) => {
                        setDraft({ ...draft, [field]: event.target.value })
                        setNotice('')
                      }}
                      aria-describedby={`site-hint-${field}`}
                    />
                  )}
                  <p id={`site-hint-${field}`} className="text-xs leading-5 text-muted-foreground">
                    {t(hints[field])}
                  </p>
                </div>
              </div>
            ))}
          </fieldset>
          <div className="space-y-3 sm:ml-[164px]">
            {stale ? (
              <div className="space-y-3">
                <p role="alert" className="text-sm">
                  {t('conflict')}
                </p>
                <Button variant="outline" disabled={busy} onClick={() => void review()}>
                  {t('review')}
                </Button>
              </div>
            ) : (
              error && (
                <p role="alert" className="text-sm text-destructive">
                  {t(error)}
                </p>
              )
            )}
            {notice && (
              <p role="status" className="text-sm">
                {t(notice)}
              </p>
            )}
            {notice === 'reviewed' && (
              <div className="rounded-md border bg-muted/30 p-3 text-sm">
                <h3 className="mb-2 font-medium">{t('latest')}</h3>
                <dl className="space-y-2">
                  {fields.map((field) => (
                    <div key={field}>
                      <dt className="text-muted-foreground">{t(labels[field])}</dt>
                      <dd className="whitespace-pre-wrap break-all">{saved[field] || '—'}</dd>
                    </div>
                  ))}
                </dl>
              </div>
            )}
            {access.can('site.write') && (
              <Button
                type="submit"
                variant={dirty ? 'default' : 'outline'}
                disabled={!dirty || busy || stale}
              >
                <Save className="size-4" aria-hidden="true" />
                {t(busy ? 'saving' : 'save')}
              </Button>
            )}
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
