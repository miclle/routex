import RestoreDefaults from '@/views/default-limits/restore'
import type { RestoreOwner } from '@/views/default-limits/restore-owner'
import { useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { getTeamLimits, saveTeamLimits, teamLimitPath } from '@/api/resource-limits'
import { sessionKey, useSession } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import {
  type LimitRecord,
  type TeamLimitField,
  type TeamLimitInput,
  type TeamLimitScope,
} from '@/types/resource-limits'
import { integerDraft, moneyAbove, parseInteger, validMoney } from './quota-values'
import { QuotaUsageSummary } from './quota-usage'

export default function TeamResourceLimits(props: {
  scope: TeamLimitScope
  canEdit: boolean
  restoreOwner?: RestoreOwner
  restoreHostCurrent?: () => boolean
}) {
  const session = useSession()
  const actor = session.data?.user.id ?? ''
  return (
    <TeamLimitContent key={`${actor}:${teamLimitPath(props.scope)}`} {...props} actor={actor} />
  )
}
function TeamLimitContent({
  scope,
  canEdit,
  actor,
  restoreOwner,
  restoreHostCurrent,
}: {
  scope: TeamLimitScope
  canEdit: boolean
  actor: string
  restoreOwner?: RestoreOwner
  restoreHostCurrent?: () => boolean
}) {
  const { t } = useTranslation('limits')
  const cache = useQueryClient()
  const queryKey = ['resource-limits', 'team', actor, scope.teamId, scope.userId ?? 'aggregate']
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => getTeamLimits(scope, signal),
    enabled: !!actor,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const [editing, setEditing] = useState(false)
  const [notice, setNotice] = useState<'applied' | 'teamUncertainClosed' | null>(null)
  const fresh = !!query.data && !query.isFetching && !query.isError && !!actor
  const editable = canEdit && !!query.data?.editable_fields?.length
  return (
    <section className="space-y-4" aria-label={t(scope.userId ? 'teamMemberTitle' : 'teamTitle')}>
      <QueryState
        pending={query.isFetching}
        error={query.error}
        retry={() => void query.refetch()}
      />
      {fresh && query.data && (
        <>
          {notice && (
            <p role="status" className="text-sm">
              {t(notice)}
            </p>
          )}
          {!editing && (
            <>
              <TeamLimitSummary record={query.data} member={!!scope.userId} />
              {editable && (
                <Button
                  variant="outline"
                  onClick={() => {
                    setNotice(null)
                    setEditing(true)
                  }}
                >
                  {t(scope.userId ? 'teamMemberEdit' : 'edit')}
                </Button>
              )}
            </>
          )}
        </>
      )}
      {!scope.userId && (
        <RestoreDefaults
          target={{ kind: 'team', id: scope.teamId }}
          owner={restoreOwner}
          visible={fresh && !editing && canEdit}
          hostCurrent={() => {
            const current = cache.getQueryState<LimitRecord>(queryKey)
            return (
              current?.status === 'success' &&
              current.fetchStatus === 'idle' &&
              !current.error &&
              !current.isInvalidated &&
              current.data === query.data &&
              current.data?.kind === 'team' &&
              current.data.id === scope.teamId &&
              (!restoreHostCurrent || restoreHostCurrent())
            )
          }}
        />
      )}
      {editing && query.data && (
        <TeamLimitEditor
          scope={scope}
          actor={actor}
          current={query.data}
          visible={fresh}
          canEdit={editable}
          reload={async () => {
            const result = await query.refetch()
            if (!result.data || result.error) throw result.error ?? new Error('Policy unavailable')
            return result.data
          }}
          saved={(data) => {
            cache.setQueryData(queryKey, data)
            setNotice('applied')
            setEditing(false)
          }}
          close={(uncertain) => {
            setNotice(uncertain ? 'teamUncertainClosed' : null)
            setEditing(false)
            void query.refetch()
          }}
        />
      )}
    </section>
  )
}
const fieldsFor = (member: boolean): TeamLimitField[] =>
  (
    ['money_month', 'tokens_5h', 'tokens_7d', 'tokens_month', 'rpm', 'tpm', 'concurrency'] as const
  ).filter((field) => !member || (field !== 'tokens_5h' && field !== 'tokens_7d'))
function TeamLimitSummary({ record, member }: { record: LimitRecord; member: boolean }) {
  const { t, i18n } = useTranslation('limits')
  function value(field: TeamLimitField, effective = false) {
    const policy = effective ? record.effective : record.stored
    const item = policy[field]
    if (item == null) return t(effective || !member ? 'unlimited' : 'inherited')
    return field === 'money_month'
      ? `${item} ${policy.currency}`
      : Number(item).toLocaleString(i18n.resolvedLanguage)
  }
  const fields = fieldsFor(member)
  return (
    <>
      <p
        role="status"
        className={record.enforced ? 'text-sm text-muted-foreground' : 'text-sm text-destructive'}
      >
        {t(record.enforced ? 'published' : 'unpublished')}
      </p>
      <p className="text-sm text-muted-foreground">{t(member ? 'teamMemberHelp' : 'teamHelp')}</p>
      {(['budgetQuotas', 'requests'] as const).map((section) => (
        <section className="rounded-lg border p-4" key={section}>
          <h3 className="font-medium">{t(section)}</h3>
          <dl className="mt-4 grid gap-4 sm:grid-cols-2">
            {fields
              .filter((field) =>
                section === 'requests'
                  ? ['rpm', 'tpm', 'concurrency'].includes(field)
                  : !['rpm', 'tpm', 'concurrency'].includes(field),
              )
              .map((field) => (
                <div key={field}>
                  <dt className="text-sm text-muted-foreground">{t(field)}</dt>
                  <dd className="text-sm">
                    {t('stored')}: {value(field)}
                  </dd>
                  <dd className="text-sm">
                    {t('effective')}: {value(field, true)}
                  </dd>
                </div>
              ))}
          </dl>
        </section>
      ))}
      <dl className="grid gap-4 text-sm sm:grid-cols-2">
        <div>
          <dt>{t('usage')}</dt>
          <dd>
            {record.rpm_used == null
              ? t('unknown')
              : record.rpm_used.toLocaleString(i18n.resolvedLanguage)}
          </dd>
        </div>
        <div>
          <dt>{t('active')}</dt>
          <dd>
            {record.active == null
              ? t('unknown')
              : record.active.toLocaleString(i18n.resolvedLanguage)}
          </dd>
        </div>
      </dl>
      <QuotaUsageSummary usage={record.quota_usage} />
    </>
  )
}
function TeamLimitEditor({
  scope,
  actor,
  current,
  visible,
  canEdit,
  reload,
  saved,
  close,
}: {
  scope: TeamLimitScope
  actor: string
  current: LimitRecord
  visible: boolean
  canEdit: boolean
  reload: () => Promise<LimitRecord>
  saved: (data: LimitRecord) => void
  close: (uncertain: boolean) => void
}) {
  const { t } = useTranslation('limits')
  const session = useSession()
  const cache = useQueryClient()
  const currentSession = () => cache.getQueryData<Session>(sessionKey)
  const [reviewed, setReviewed] = useState(current)
  const [numbers, setNumbers] = useState(() => integerDraft(current.stored))
  const [money, setMoney] = useState(current.stored.money_month ?? '')
  const [reason, setReason] = useState('')
  const [issue, setIssue] = useState<string | null>(null)
  const [uncertain, setUncertain] = useState(false)
  const [busy, setBusy] = useState(false)
  const intent = useRef<{ etag: string; input: TeamLimitInput; actor: string } | null>(null)
  const lock = useRef(false)
  const mounted = useRef(false)
  const identity = useRef('')
  const visibleRef = useRef(visible)
  useLayoutEffect(() => {
    visibleRef.current = visible
  })
  useLayoutEffect(() => {
    identity.current = session.isError ? '' : (session.data?.user.id ?? '')
  })
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const active = () =>
    mounted.current &&
    identity.current === actor &&
    currentSession()?.user.id === actor &&
    cache.getQueryState(sessionKey)?.status !== 'error'
  const fields = fieldsFor(!!scope.userId)
  const editable = (field: TeamLimitField) =>
    canEdit &&
    reviewed.editable_fields?.includes(field) === true &&
    current.editable_fields?.includes(field) === true
  const stale =
    current.etag !== reviewed.etag ||
    current.parent_etag !== reviewed.parent_etag ||
    current.platform_currency !== reviewed.platform_currency
  const blocked = stale || issue === 'conflict' || issue === 'failed'
  async function dispatch(retry = false) {
    if (
      lock.current ||
      !active() ||
      !session.data ||
      !visible ||
      !canEdit ||
      (!retry && (blocked || uncertain))
    )
      return
    if (!retry) {
      const input: TeamLimitInput = { reason: reason.trim() }
      if (!input.reason || new TextEncoder().encode(input.reason).length > 2000) {
        setIssue('requiredReason')
        return
      }
      const parent = scope.userId ? reviewed.ip_policies[0] : undefined
      for (const field of fields) {
        if (!editable(field)) continue
        if (field === 'money_month') {
          const amount = money.trim() || null
          if (amount !== null && !validMoney(amount)) {
            setIssue('invalidMoney')
            return
          }
          if (
            amount !== null &&
            parent?.money_month != null &&
            (parent.currency !== reviewed.platform_currency ||
              moneyAbove(amount, parent.money_month))
          ) {
            setIssue('teamAboveParent')
            return
          }
          if (amount !== (reviewed.stored.money_month ?? null)) {
            input.money_month = amount
            if (amount !== null) input.currency = reviewed.platform_currency
          }
        } else {
          const numeric = parseInteger(numbers[field])
          if (numeric === undefined) {
            setIssue('invalidNumber')
            return
          }
          if (numeric != null && parent?.[field] != null && numeric > parent[field]) {
            setIssue('teamAboveParent')
            return
          }
          if (numeric !== (reviewed.stored[field] ?? null)) input[field] = numeric
        }
      }
      if (!fields.some((field) => Object.hasOwn(input, field))) {
        setIssue('teamNoChanges')
        return
      }
      intent.current = { etag: reviewed.etag, input, actor }
    }
    const original = intent.current
    const latestSession = currentSession()
    if (!original || original.actor !== latestSession?.user.id) return
    lock.current = true
    setBusy(true)
    setIssue(null)
    try {
      const data = await saveTeamLimits(
        scope,
        original.etag,
        original.input,
        latestSession.csrf_token,
      )
      if (active()) {
        if (visibleRef.current) saved(data)
        else {
          setUncertain(true)
          setIssue('uncertain')
        }
      }
    } catch (error) {
      if (!active()) return
      const status = isAxiosError(error) ? error.response?.status : undefined
      const unknown = !status || status >= 500
      setUncertain((previous) => previous || unknown)
      setIssue(unknown || uncertain ? 'uncertain' : status === 409 ? 'conflict' : 'failed')
    } finally {
      lock.current = false
      if (active()) setBusy(false)
    }
  }
  async function review() {
    if (lock.current || !active()) return
    lock.current = true
    setBusy(true)
    try {
      const next = await reload()
      if (!active()) return
      setReviewed(next)
      // A current projection cannot prove an earlier uncertain write was never committed.
      if (!uncertain) {
        intent.current = null
        setIssue(null)
      }
    } catch {
      /* Keep the original intent and draft until an authorized read succeeds. */
    } finally {
      lock.current = false
      if (active()) setBusy(false)
    }
  }
  if (!visible) return null
  return (
    <form
      className="space-y-4"
      aria-label={t(scope.userId ? 'teamMemberEdit' : 'teamTitle')}
      onSubmit={(event) => {
        event.preventDefault()
        void dispatch()
      }}
    >
      <p className="text-sm text-muted-foreground">
        {t(scope.userId ? 'teamMemberHelp' : 'teamHelp')}
      </p>
      {(issue || stale || uncertain) && (
        <p role="alert" className="text-sm text-destructive">
          {t(uncertain ? 'teamUncertain' : stale ? 'conflict' : issue!)}
        </p>
      )}
      <fieldset disabled={busy || uncertain} className="space-y-4">
        {(['budgetQuotas', 'requests'] as const).map((section) => (
          <section className="rounded-lg border p-4" key={section}>
            <h3 className="font-medium">{t(section)}</h3>
            {section === 'budgetQuotas' && (
              <p className="mt-2 text-xs text-muted-foreground">{t('quotaHelp')}</p>
            )}
            <div className="mt-4 grid gap-4 sm:grid-cols-2">
              {fields
                .filter((field) =>
                  section === 'requests'
                    ? ['rpm', 'tpm', 'concurrency'].includes(field)
                    : !['rpm', 'tpm', 'concurrency'].includes(field),
                )
                .map((field) => (
                  <div key={field}>
                    <FormField label={t(field)}>
                      <Input
                        aria-label={t(field)}
                        disabled={!editable(field)}
                        inputMode={field === 'money_month' ? 'decimal' : 'numeric'}
                        value={field === 'money_month' ? money : numbers[field]}
                        onValueChange={(value) =>
                          field === 'money_month'
                            ? setMoney(value)
                            : setNumbers((draft) => ({ ...draft, [field]: value }))
                        }
                        placeholder={t(scope.userId ? 'inherited' : 'unlimited')}
                      />
                    </FormField>
                    {!editable(field) && (
                      <p className="mt-1 text-xs text-muted-foreground">{t('teamReadOnlyField')}</p>
                    )}
                    {field === 'money_month' && (
                      <p className="mt-2 text-xs text-muted-foreground">
                        {t('platformCurrency', { value: reviewed.platform_currency })}
                      </p>
                    )}
                    {scope.userId && (
                      <p className="mt-2 text-xs text-muted-foreground">
                        {t('parentMaximum', {
                          value:
                            field === 'money_month' && reviewed.ip_policies[0]?.money_month != null
                              ? `${reviewed.ip_policies[0].money_month} ${reviewed.ip_policies[0].currency}`
                              : (reviewed.ip_policies[0]?.[field] ?? t('unlimited')),
                        })}
                      </p>
                    )}
                  </div>
                ))}
            </div>
          </section>
        ))}
        <FormField label={t('reason')}>
          <Input aria-label={t('reason')} value={reason} onValueChange={setReason} />
        </FormField>
      </fieldset>
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={busy || blocked || uncertain || !canEdit}>
          {t(busy ? 'loading' : 'save')}
        </Button>
        {uncertain && (
          <Button type="button" disabled={busy || !canEdit} onClick={() => void dispatch(true)}>
            {t('retry')}
          </Button>
        )}
        {(blocked || uncertain) && (
          <Button type="button" variant="outline" disabled={busy} onClick={() => void review()}>
            {t('reload')}
          </Button>
        )}
        <Button type="button" variant="outline" disabled={busy} onClick={() => close(uncertain)}>
          {t('cancel')}
        </Button>
      </div>
    </form>
  )
}
