import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { getLimits, saveLimits } from '@/api/resource-limits'
import { useSession } from '@/hooks/use-auth'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog } from '@/components/ui/dialog'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import type { LimitInput, LimitRecord } from '@/types/resource-limits'

type Props = { path: string; canEdit: boolean; child?: boolean }
export default function ResourceLimits(props: Props) {
  const { t } = useTranslation('limits')
  const cache = useQueryClient()
  const query = useQuery({
    queryKey: ['resource-limits', props.path],
    queryFn: ({ signal }) => getLimits(props.path, signal),
  })
  const [editing, setEditing] = useState(false)
  const [writing, setWriting] = useState(false)
  const [notice, setNotice] = useState<'applied' | 'pending' | null>(null)
  const body = query.data && (
    <LimitEditor
      {...props}
      current={query.data}
      pending={setWriting}
      reload={async () => {
        const result = await query.refetch()
        if (!result.data || result.error) throw result.error ?? new Error('Policy unavailable')
        return result.data
      }}
      close={() => setEditing(false)}
      saved={(data) => {
        cache.setQueryData(['resource-limits', props.path], data)
        void cache.invalidateQueries({ queryKey: ['resource-limits'] })
        setNotice(data.enforced ? 'applied' : 'pending')
        setEditing(false)
      }}
    />
  )
  return (
    <section className="rounded-lg border" aria-label={t('title')}>
      <header className="flex items-center justify-between gap-4 border-b p-4">
        <h3 className="font-medium">{t('title')}</h3>
        {props.canEdit && query.data && !editing && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              setNotice(null)
              setEditing(true)
            }}
          >
            {t(props.child ? 'keyEdit' : 'edit')}
          </Button>
        )}
      </header>
      <div className="space-y-4 p-4">
        <QueryState
          pending={query.isPending}
          error={query.error}
          retry={() => void query.refetch()}
        />
        {query.data && <LimitSummary record={query.data} child={props.child} />}
        {notice && (
          <p role="status" className="text-sm">
            {t(notice)}
          </p>
        )}
        {editing && !props.child && props.canEdit && body}
      </div>
      {props.child && props.canEdit && (
        <Dialog
          busy={writing}
          open={editing}
          onOpenChange={setEditing}
          title={t('keyEdit')}
          description={t('childHelp')}
          width={800}
        >
          {editing && body}
        </Dialog>
      )}
    </section>
  )
}
function LimitSummary({ record, child }: { record: LimitRecord; child?: boolean }) {
  const { t, i18n } = useTranslation('limits')
  const format = (value: number | null, fallback: string) =>
    value === null ? t(fallback) : value.toLocaleString(i18n.resolvedLanguage)
  return (
    <div className="space-y-4 text-sm">
      <p role="status" className={record.enforced ? 'text-muted-foreground' : 'text-destructive'}>
        {t(record.enforced ? 'published' : 'unpublished')}
      </p>
      <dl className="grid gap-4 sm:grid-cols-2">
        {(['rpm', 'concurrency'] as const).map((field) => (
          <div key={field}>
            <dt className="text-muted-foreground">{t(field)}</dt>
            <dd>
              {t('stored')}: {format(record.stored[field], child ? 'inherited' : 'unlimited')}
            </dd>
            <dd>
              {t('effective')}: {format(record.effective[field], 'unlimited')}
            </dd>
          </div>
        ))}
        <div>
          <dt className="text-muted-foreground">{t('usage')}</dt>
          <dd>{format(record.rpm_used, 'unknown')}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('active')}</dt>
          <dd>{format(record.active, 'unknown')}</dd>
        </div>
      </dl>
      <div>
        <h4 className="font-medium">{t('ip')}</h4>
        <p className="mt-1 text-xs text-muted-foreground">{t('conjunction')}</p>
        {record.ip_policies.map((policy, index) => (
          <div key={index} className="mt-2">
            <p>
              {t(child && index === 0 ? 'parent' : 'local')}: {t(policy.ip_mode)}
            </p>
            {policy.ip_ranges.map((range) => (
              <code key={range} className="mr-3 break-all text-xs">
                {range}
              </code>
            ))}
          </div>
        ))}
      </div>
      <p className="text-xs text-muted-foreground">
        {t('account')}: <code className="break-all">{record.account_id}</code>
      </p>
    </div>
  )
}
function LimitEditor({
  path,
  child,
  canEdit,
  current,
  reload,
  close,
  saved,
  pending,
}: Props & {
  current: LimitRecord
  pending: (value: boolean) => void
  reload: () => Promise<LimitRecord>
  close: () => void
  saved: (data: LimitRecord) => void
}) {
  const { t } = useTranslation('limits')
  const session = useSession()
  const [reviewed, setReviewed] = useState(current)
  const [rpm, setRPM] = useState(current.stored.rpm?.toString() ?? '')
  const [concurrency, setConcurrency] = useState(current.stored.concurrency?.toString() ?? '')
  const [mode, setMode] = useState(current.stored.ip_mode)
  const [ranges, setRanges] = useState(current.stored.ip_ranges.join('\n'))
  const [reason, setReason] = useState('')
  const [issue, setIssue] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const lock = useRef(false)
  const intent = useRef<{ etag: string; input: LimitInput } | null>(null)
  const stale = current.etag !== reviewed.etag || current.parent_etag !== reviewed.parent_etag
  const uncertain = issue === 'uncertain'
  const blocked = stale || issue === 'conflict' || issue === 'failed'
  async function dispatch(retry = false) {
    if (!canEdit || lock.current || !session.data || (!retry && (blocked || uncertain))) return
    if (!retry) {
      const numeric = [rpm, concurrency].map((value) =>
        value.trim() === '' ? null : Number(value),
      )
      if (
        [rpm, concurrency].some(
          (value, index) =>
            value.trim() !== '' &&
            (!/^\d+$/.test(value.trim()) || !Number.isSafeInteger(numeric[index])),
        )
      ) {
        setIssue('invalidNumber')
        return
      }
      const parent = child ? reviewed.ip_policies[0] : undefined
      if (
        parent &&
        (['rpm', 'concurrency'] as const).some(
          (field, index) =>
            numeric[index] !== null && parent[field] !== null && numeric[index]! > parent[field]!,
        )
      ) {
        setIssue('aboveParent')
        return
      }
      if (!reason.trim() || new TextEncoder().encode(reason.trim()).length > 2000) {
        setIssue('requiredReason')
        return
      }
      const entries =
        mode === 'none'
          ? []
          : ranges
              .split(/[\n,]/)
              .map((value) => value.trim())
              .filter(Boolean)
      if (mode !== 'none' && (!entries.length || entries.length > 128)) {
        setIssue('requiredRanges')
        return
      }
      intent.current = {
        etag: reviewed.etag,
        input: {
          rpm: numeric[0]!,
          concurrency: numeric[1]!,
          ip_mode: mode,
          ip_ranges: entries,
          reason: reason.trim(),
        },
      }
    }
    if (!intent.current) return
    lock.current = true
    setBusy(true)
    pending(true)
    setIssue(null)
    try {
      saved(
        await saveLimits(path, intent.current.etag, intent.current.input, session.data.csrf_token),
      )
    } catch (error) {
      const status = isAxiosError(error) ? error.response?.status : undefined
      setIssue(status === 409 ? 'conflict' : !status || status >= 500 ? 'uncertain' : 'failed')
    } finally {
      lock.current = false
      setBusy(false)
      pending(false)
    }
  }
  async function reconcile() {
    if (lock.current) return
    lock.current = true
    setBusy(true)
    pending(true)
    try {
      setReviewed(await reload())
      intent.current = null
      setIssue(null)
    } catch {
      /* Preserve the recovery state until a fresh policy can be reviewed. */
    } finally {
      lock.current = false
      setBusy(false)
      pending(false)
    }
  }
  return (
    <form
      className="space-y-4"
      aria-label={t('draft')}
      onSubmit={(event) => {
        event.preventDefault()
        void dispatch()
      }}
    >
      <p className="text-sm text-muted-foreground">{t(child ? 'childHelp' : 'aggregateHelp')}</p>
      {(issue || stale) && (
        <p role="alert" className="text-sm text-destructive">
          {t(stale && !uncertain ? 'conflict' : issue!)}
        </p>
      )}
      <fieldset disabled={busy || uncertain} className="space-y-4">
        <details open className="rounded-lg border p-4">
          <summary className="cursor-pointer font-medium">{t('requests')}</summary>
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            {(['rpm', 'concurrency'] as const).map((field, index) => (
              <div key={field}>
                <FormField label={t(field)}>
                  <Input
                    aria-label={t(field)}
                    inputMode="numeric"
                    value={index ? concurrency : rpm}
                    onValueChange={index ? setConcurrency : setRPM}
                    placeholder={t(child ? 'inherited' : 'unlimited')}
                  />
                </FormField>
                {child && (
                  <p className="mt-2 text-xs text-muted-foreground">
                    {t('parentMaximum', {
                      value: reviewed.ip_policies[0]?.[field] ?? t('unlimited'),
                    })}
                  </p>
                )}
              </div>
            ))}
          </div>
        </details>
        <details open className="rounded-lg border p-4">
          <summary className="cursor-pointer font-medium">{t('ip')}</summary>
          <div className="mt-4 space-y-4">
            <div className="flex flex-wrap gap-4" role="radiogroup" aria-label={t('ip')}>
              {(['none', 'allowlist', 'denylist'] as const).map((value) => (
                <label key={value} className="flex items-center gap-2 text-sm">
                  <input
                    type="radio"
                    name={`ip-${path}`}
                    checked={mode === value}
                    onChange={() => {
                      setMode(value)
                      if (value === 'none') setRanges('')
                    }}
                  />
                  {t(value)}
                </label>
              ))}
            </div>
            {mode !== 'none' && (
              <FormField label={t('ranges')}>
                <textarea
                  className="min-h-24 w-full rounded-md border bg-background p-3 font-mono text-sm"
                  aria-label={t('ranges')}
                  value={ranges}
                  onChange={(event) => setRanges(event.target.value)}
                />
                <span className="block text-xs text-muted-foreground">{t('rangesHelp')}</span>
              </FormField>
            )}
          </div>
        </details>
        <FormField label={t('reason')}>
          <Input aria-label={t('reason')} value={reason} onValueChange={setReason} />
          <span className="block text-xs text-muted-foreground">{t('reasonHelp')}</span>
        </FormField>
      </fieldset>
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={busy || blocked || uncertain}>
          {t(busy ? 'loading' : 'save')}
        </Button>
        {uncertain && (
          <Button type="button" disabled={busy} onClick={() => void dispatch(true)}>
            {t('retry')}
          </Button>
        )}
        {(blocked || uncertain) && (
          <Button type="button" variant="outline" disabled={busy} onClick={() => void reconcile()}>
            {t('reload')}
          </Button>
        )}
        <Button type="button" variant="outline" disabled={busy} onClick={close}>
          {t('cancel')}
        </Button>
      </div>
    </form>
  )
}
