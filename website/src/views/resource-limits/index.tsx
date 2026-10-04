import RestoreDefaults from '@/views/default-limits/restore'
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
import type { LimitInput, LimitRecord, TeamLimitScope } from '@/types/resource-limits'
import {
  integerDraft,
  integerFields,
  moneyAbove,
  parseInteger,
  quotaFields,
  rateFields,
  validMoney,
} from './quota-values'
import { QuotaUsageSummary } from './quota-usage'

import TeamResourceLimits from './team'

type Props = {
  path: string
  canEdit: boolean
  child?: boolean
  team?: TeamLimitScope
  parentManagedSession?: boolean
}
export default function ResourceLimits(props: Props) {
  if (props.team) return <TeamResourceLimits scope={props.team} canEdit={props.canEdit} />
  return <ResourceLimitContent key={props.path} {...props} />
}
function ResourceLimitContent(props: Props) {
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
        {props.canEdit &&
          query.data?.kind === 'user' &&
          !query.isFetching &&
          !query.isError &&
          !editing &&
          /^\/admin\/members\/[^/]+$/.test(props.path) && (
            <RestoreDefaults target={{ kind: 'user', id: query.data.id }} />
          )}
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
export function LimitSummary({ record, child }: { record: LimitRecord; child?: boolean }) {
  const { t, i18n } = useTranslation('limits')
  const format = (value: number | null | undefined, fallback: string) =>
    value == null ? t(fallback) : value.toLocaleString(i18n.resolvedLanguage)
  return (
    <div className="space-y-4 text-sm">
      <p role="status" className={record.enforced ? 'text-muted-foreground' : 'text-destructive'}>
        {t(record.enforced ? 'published' : 'unpublished')}
      </p>
      <dl className="grid gap-4 sm:grid-cols-2">
        {integerFields.map((field) => (
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
          <dt className="text-muted-foreground">{t('money_month')}</dt>
          <dd>
            {t('stored')}:{' '}
            {record.stored.money_month == null
              ? t(child ? 'inherited' : 'unlimited')
              : `${record.stored.money_month} ${record.stored.currency}`}
          </dd>
          <dd>
            {t('effective')}:{' '}
            {record.effective.money_month == null
              ? t('unlimited')
              : `${record.effective.money_month} ${record.effective.currency}`}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('usage')}</dt>
          <dd>{format(record.rpm_used, 'unknown')}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('active')}</dt>
          <dd>{format(record.active, 'unknown')}</dd>
        </div>
      </dl>
      <QuotaUsageSummary usage={record.quota_usage} />
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
export function LimitEditor({
  parentManagedSession,
  path,
  child,
  canEdit,
  current,
  reload,
  close,
  saved,
  pending,
  visible = true,
  canDispatch,
  savePolicy,
}: Props & {
  current: LimitRecord
  pending: (value: boolean) => void
  reload: () => Promise<LimitRecord>
  close: () => void
  saved: (data: LimitRecord) => void
  visible?: boolean
  canDispatch?: () => boolean
  savePolicy?: (etag: string, input: LimitInput) => Promise<LimitRecord>
}) {
  const { t } = useTranslation('limits')
  const session = useSession(!parentManagedSession)
  const [reviewed, setReviewed] = useState(current)
  const [numbers, setNumbers] = useState(() => integerDraft(current.stored))
  const [money, setMoney] = useState(current.stored.money_month ?? '')
  const [mode, setMode] = useState(current.stored.ip_mode)
  const [ranges, setRanges] = useState(current.stored.ip_ranges.join('\n'))
  const [reason, setReason] = useState('')
  const [issue, setIssue] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const lock = useRef(false)
  const intent = useRef<{ etag: string; input: LimitInput } | null>(null)
  const stale =
    current.etag !== reviewed.etag ||
    current.parent_etag !== reviewed.parent_etag ||
    current.platform_currency !== reviewed.platform_currency
  const [uncertainIntent, setUncertainIntent] = useState(false)
  const uncertain = issue === 'uncertain' || (!!savePolicy && uncertainIntent)
  const blocked = stale || issue === 'conflict' || issue === 'failed'
  async function dispatch(retry = false) {
    if (
      !visible ||
      !canEdit ||
      lock.current ||
      !session.data ||
      (canDispatch && !canDispatch()) ||
      (!retry && (blocked || uncertain))
    )
      return
    if (!retry) {
      const numeric = Object.fromEntries(
        integerFields.map((field) => [field, parseInteger(numbers[field])]),
      )
      if (integerFields.some((field) => numeric[field] === undefined)) {
        setIssue('invalidNumber')
        return
      }
      const amount = money.trim() || null
      if (amount !== null && !validMoney(amount)) {
        setIssue('invalidMoney')
        return
      }
      if (amount !== null && !reviewed.platform_currency) {
        setIssue('currencyUnavailable')
        return
      }
      const parent = child ? reviewed.ip_policies[0] : undefined
      if (
        parent &&
        (integerFields.some(
          (field) =>
            numeric[field] != null && parent[field] != null && numeric[field]! > parent[field]!,
        ) ||
          (amount !== null &&
            parent.money_month != null &&
            (parent.currency !== reviewed.platform_currency ||
              moneyAbove(amount, parent.money_month))))
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
          tokens_5h: numeric.tokens_5h!,
          tokens_7d: numeric.tokens_7d!,
          tokens_month: numeric.tokens_month!,
          tpm: numeric.tpm!,
          money_month: amount,
          currency: amount === null ? '' : reviewed.platform_currency,
          rpm: numeric.rpm!,
          concurrency: numeric.concurrency!,
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
        savePolicy
          ? await savePolicy(intent.current.etag, intent.current.input)
          : await saveLimits(
              path,
              intent.current.etag,
              intent.current.input,
              session.data.csrf_token,
            ),
      )
    } catch (error) {
      const status = isAxiosError(error) ? error.response?.status : undefined
      if (savePolicy && (!status || status >= 500)) setUncertainIntent(true)
      setIssue(status === 409 ? 'conflict' : !status || status >= 500 ? 'uncertain' : 'failed')
    } finally {
      lock.current = false
      setBusy(false)
      pending(false)
    }
  }
  async function reconcile() {
    if (lock.current || (savePolicy && uncertain) || (canDispatch && !canDispatch())) return
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
  if (!visible) return null
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
          {t(savePolicy && uncertain ? 'uncertain' : stale && !uncertain ? 'conflict' : issue!)}
        </p>
      )}
      <fieldset disabled={busy || uncertain} className="space-y-4">
        <details open className="rounded-lg border p-4">
          <summary className="cursor-pointer font-medium">{t('budgetQuotas')}</summary>
          <p className="mt-2 text-xs text-muted-foreground">{t('quotaHelp')}</p>
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            <div>
              <FormField label={t('money_month')}>
                <Input
                  aria-label={t('money_month')}
                  inputMode="decimal"
                  value={money}
                  onValueChange={setMoney}
                  placeholder={t(child ? 'inherited' : 'unlimited')}
                />
              </FormField>
              <p className="mt-2 text-xs text-muted-foreground">
                {t('platformCurrency', { value: reviewed.platform_currency || t('unknown') })}
              </p>
              {child && (
                <p className="mt-2 text-xs text-muted-foreground">
                  {t('parentMaximum', {
                    value:
                      reviewed.ip_policies[0]?.money_month == null
                        ? t('unlimited')
                        : `${reviewed.ip_policies[0].money_month} ${reviewed.ip_policies[0].currency}`,
                  })}
                </p>
              )}
            </div>
            {quotaFields.map((field) => (
              <div key={field}>
                <FormField label={t(field)}>
                  <Input
                    aria-label={t(field)}
                    inputMode="numeric"
                    value={numbers[field]}
                    onValueChange={(value) => setNumbers((draft) => ({ ...draft, [field]: value }))}
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
          <summary className="cursor-pointer font-medium">{t('requests')}</summary>
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            {rateFields.map((field) => (
              <div key={field}>
                <FormField label={t(field)}>
                  <Input
                    aria-label={t(field)}
                    inputMode="numeric"
                    value={numbers[field]}
                    onValueChange={(value) => setNumbers((draft) => ({ ...draft, [field]: value }))}
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
        {(blocked || uncertain) && !(savePolicy && uncertain) && (
          <Button type="button" variant="outline" disabled={busy} onClick={() => void reconcile()}>
            {t('reload')}
          </Button>
        )}
        <Button
          type="button"
          variant="outline"
          disabled={busy || (!!savePolicy && uncertain)}
          onClick={close}
        >
          {t('cancel')}
        </Button>
      </div>
    </form>
  )
}
