import { useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { isAxiosError } from 'axios'
import { CalendarDays } from 'lucide-react'
import { getQuotaSettings, quotaSettingsKey, saveQuotaSettings } from '@/api/quota-settings'
import type { QuotaSettings, QuotaSettingsInput } from '@/types/quota-settings'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { QueryState } from '@/components/app/CatalogUI'
import { Card, CardHeader, CardContent, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog } from '@/components/ui/dialog'

export default function QuotaCalendar() {
  const { t } = useTranslation('site')
  const session = useSession()
  const access = usePermissions()
  const cache = useQueryClient()
  const canRead = access.can('system.read') || access.can('limits.settings.write')
  const query = useQuery({
    queryKey: quotaSettingsKey,
    queryFn: ({ signal }) => getQuotaSettings(signal),
    enabled: canRead,
  })
  if (!canRead) return null
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <CalendarDays className="size-4" aria-hidden="true" />
          {t('calendarTitle')}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <QueryState
          pending={query.isPending}
          error={query.error}
          retry={() => void query.refetch()}
        />
        {query.data && (
          <CalendarEditor
            key={session.data?.user.id}
            current={query.data}
            reload={async () => {
              const result = await query.refetch()
              if (!result.data || result.error)
                throw result.error ?? new Error('Quota calendar unavailable')
              return result.data
            }}
            saved={(data) => {
              cache.setQueryData(quotaSettingsKey, data)
              void cache.invalidateQueries({ queryKey: ['resource-limits'] })
            }}
          />
        )}
      </CardContent>
    </Card>
  )
}
function CalendarEditor({
  current,
  reload,
  saved,
}: {
  current: QuotaSettings
  reload: () => Promise<QuotaSettings>
  saved: (record: QuotaSettings) => void
}) {
  const { t, i18n } = useTranslation('site')
  const access = usePermissions()
  const session = useSession()
  const [reviewed, setReviewed] = useState(current)
  const [zone, setZone] = useState(current.time_zone)
  const [reason, setReason] = useState('')
  const [issue, setIssue] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [confirmationZone, setConfirmationZone] = useState('')
  const lock = useRef(false)
  const intent = useRef<{ etag: string; input: QuotaSettingsInput } | null>(null)
  const canWrite = access.can('limits.settings.write')
  const stale =
    current.etag !== reviewed.etag ||
    current.time_zone !== reviewed.time_zone ||
    current.editable !== reviewed.editable ||
    issue === 'calendarConflict'
  const uncertain = issue === 'calendarUncertain'
  const editable = current.editable && reviewed.editable
  const dirty = zone.trim() !== reviewed.time_zone
  function propose(event: FormEvent) {
    event.preventDefault()
    if (lock.current || !canWrite || !editable || stale || uncertain || !dirty) return
    const trimmedZone = zone.trim()
    const trimmedReason = reason.trim()
    const bytes = (value: string) => new TextEncoder().encode(value).length
    if (!trimmedZone || bytes(trimmedZone) > 100 || !trimmedReason || bytes(trimmedReason) > 2000) {
      setIssue('calendarInvalid')
      return
    }
    intent.current = {
      etag: reviewed.etag,
      input: { time_zone: trimmedZone, reason: trimmedReason },
    }
    setIssue('')
    setNotice('')
    setConfirmationZone(trimmedZone)
    setConfirming(true)
  }
  async function dispatch(retry = false) {
    if (
      lock.current ||
      !canWrite ||
      !session.data ||
      !intent.current ||
      (!retry && (!editable || stale))
    )
      return
    if (retry && !uncertain) return
    const submitted = intent.current
    lock.current = true
    setBusy(true)
    setIssue('')
    setNotice('')
    try {
      const result = await saveQuotaSettings(
        submitted.etag,
        submitted.input,
        session.data.csrf_token,
      )
      saved(result)
      setReviewed(result)
      if (result.time_zone !== submitted.input.time_zone) {
        setIssue('calendarConflict')
      } else {
        setZone(result.time_zone)
        setReason('')
        setNotice('calendarSaved')
      }
      intent.current = null
    } catch (error) {
      const status = isAxiosError(error) ? error.response?.status : undefined
      setIssue(
        status === 409
          ? 'calendarConflict'
          : retry || !status || status >= 500
            ? 'calendarUncertain'
            : status === 400
              ? 'calendarInvalid'
              : 'calendarFailed',
      )
      if (!retry && status && status < 500 && status !== 409) intent.current = null
    } finally {
      setConfirming(false)
      lock.current = false
      setBusy(false)
    }
  }
  async function review() {
    if (lock.current) return
    lock.current = true
    setBusy(true)
    try {
      setReviewed(await reload())
      intent.current = null
      setConfirming(false)
      setIssue('')
      setNotice('calendarReviewed')
    } catch {
      setNotice('calendarReloadFailed')
    } finally {
      lock.current = false
      setBusy(false)
    }
  }
  const coverageDate = () => {
    try {
      const parsed = new Date(current.coverage_start!)
      return Number.isNaN(parsed.getTime())
        ? t('calendarUnknown')
        : parsed.toLocaleString(i18n.resolvedLanguage, { timeZone: current.time_zone })
    } catch {
      return t('calendarUnknown')
    }
  }
  return (
    <>
      <form aria-label={t('calendarTitle')} className="space-y-5" onSubmit={propose}>
        <p className="text-sm text-muted-foreground">{t('calendarHelp')}</p>
        <dl className="space-y-2 text-sm">
          <div>
            <dt className="text-muted-foreground">{t('calendarStatus')}</dt>
            <dd>{t(current.activated ? 'calendarActive' : 'calendarInactive')}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('calendarCurrent')}</dt>
            <dd>{current.time_zone}</dd>
          </div>
          {current.coverage_start && (
            <div>
              <dt className="text-muted-foreground">{t('calendarCoverage')}</dt>
              <dd>{coverageDate()}</dd>
            </div>
          )}
        </dl>
        {!current.editable && (
          <p role="status" className="text-sm text-muted-foreground">
            {t('calendarLocked')}
          </p>
        )}
        <fieldset disabled={!canWrite || !editable || busy || uncertain} className="space-y-5">
          <div className="grid gap-2 sm:grid-cols-[140px_1fr] sm:gap-6">
            <label htmlFor="quota-calendar-zone" className="pt-2 text-sm font-medium">
              {t('calendarZone')}
            </label>
            <div className="space-y-2">
              <Input
                id="quota-calendar-zone"
                name="time_zone"
                autoComplete="off"
                value={zone}
                onValueChange={(value) => {
                  setZone(value)
                  setNotice('')
                }}
                aria-describedby="quota-calendar-zone-hint"
              />
              <p id="quota-calendar-zone-hint" className="text-xs leading-5 text-muted-foreground">
                {t('calendarZoneHint')}
              </p>
            </div>
          </div>
          {canWrite && (
            <div className="grid gap-2 sm:grid-cols-[140px_1fr] sm:gap-6">
              <label htmlFor="quota-calendar-reason" className="pt-2 text-sm font-medium">
                {t('calendarReason')}
              </label>
              <Input
                id="quota-calendar-reason"
                name="calendar_reason"
                autoComplete="off"
                value={reason}
                onValueChange={(value) => {
                  setReason(value)
                  setNotice('')
                }}
              />
            </div>
          )}
        </fieldset>
        <div className="space-y-3 sm:ml-[164px]">
          {(issue || stale) && (
            <p role="alert" className="text-sm text-destructive">
              {t(stale && !uncertain ? 'calendarConflict' : issue)}
            </p>
          )}
          {notice && (
            <p role="status" className="text-sm">
              {t(notice)}
            </p>
          )}
          {notice === 'calendarReviewed' && (
            <p className="rounded-md border bg-muted/30 p-3 text-sm">
              {t('calendarLatest', {
                zone: reviewed.time_zone,
                state: t(reviewed.editable ? 'calendarEditable' : 'calendarLockedShort'),
              })}
            </p>
          )}
          <div className="flex flex-wrap gap-2">
            {canWrite && (
              <Button type="submit" disabled={!editable || !dirty || busy || stale || uncertain}>
                {t('calendarSave')}
              </Button>
            )}
            {canWrite && uncertain && (
              <Button type="button" disabled={busy} onClick={() => void dispatch(true)}>
                {t('calendarRetry')}
              </Button>
            )}
            <Button type="button" variant="outline" disabled={busy} onClick={() => void review()}>
              {t('calendarReview')}
            </Button>
          </div>
        </div>
      </form>
      <Dialog
        open={confirming}
        onOpenChange={(open) => {
          if (!busy) {
            setConfirming(open)
            if (!open) intent.current = null
          }
        }}
        busy={busy}
        title={t('calendarConfirmTitle')}
        description={t('calendarConfirmHelp')}
      >
        <p className="mb-4 break-all text-sm">
          {t('calendarConfirmZone', { zone: confirmationZone })}
        </p>
        {(stale || !editable) && (
          <p role="alert" className="mb-4 text-sm text-destructive">
            {t('calendarConflict')}
          </p>
        )}
        <div className="flex gap-2">
          <Button
            disabled={busy || stale || !editable || !canWrite}
            onClick={() => void dispatch()}
          >
            {t(busy ? 'calendarSaving' : 'calendarConfirm')}
          </Button>
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => {
              setConfirming(false)
              intent.current = null
            }}
          >
            {t('calendarCancel')}
          </Button>
        </div>
      </Dialog>
    </>
  )
}
