import { useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { isAxiosError } from 'axios'
import { Link } from 'react-router'
import { decideTeamRequest, teamRequestDetail } from '@/api/team-requests'
import { sessionKey, useSession } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type {
  TeamDecisionReceipt,
  TeamRequestAction,
  TeamRequestDecision,
} from '@/types/team-requests'
import { Drawer } from '@/components/ui/drawer'
import { Dialog } from '@/components/ui/dialog'
import { Button, buttonVariants } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Table } from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import { QuotaContext, QuotaValue } from './context'
export default function DetailDrawer({
  id,
  admin,
  authorized,
  onClose,
  onSaved,
}: {
  id: string
  admin: boolean
  authorized: boolean
  onClose: (unknown: boolean) => void
  onSaved: () => void
}) {
  const { t, i18n } = useTranslation('teamRequests')
  const session = useSession(),
    cache = useQueryClient()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const queryKey = ['team-request-detail', actor, admin, id]
  const detail = useQuery({
    queryKey,
    queryFn: ({ signal }) => teamRequestDetail(id, admin, signal),
    enabled: !!actor && authorized,
    retry: false,
    gcTime: 0,
    staleTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const fresh = !!actor && authorized && detail.isSuccess && !detail.isFetching && !detail.isError
  const record = fresh ? detail.data : undefined
  const preview = record?.approval_preview
  const [action, setAction] = useState<TeamRequestAction | null>(null),
    [reason, setReason] = useState(''),
    [reviewed, setReviewed] = useState<string | null>(null),
    [reviewedStep, setReviewedStep] = useState<string | null>(null)
  const [busy, setBusy] = useState(false),
    [uncertain, setUncertain] = useState(false),
    [issue, setIssue] = useState<string | null>(null),
    [receipt, setReceipt] = useState<TeamDecisionReceipt | null>(null)
  const intent = useRef<{ actor: string; body: TeamRequestDecision; etag: string } | null>(null),
    lock = useRef(false),
    mounted = useRef(false),
    visibleRef = useRef(false)
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  useLayoutEffect(() => {
    visibleRef.current = fresh
  })
  const active = () =>
    mounted.current &&
    cache.getQueryData<Session>(sessionKey)?.user.id === actor &&
    cache.getQueryState(sessionKey)?.status !== 'error'
  const pendingApplication =
    receipt?.request.status === 'approved' &&
    receipt.request.application?.application_status === 'pending'
  const currencyMismatch =
    record?.dimension === 'money' && record.current_context?.currency !== record.currency
  const allowed = (next: TeamRequestAction) =>
    !!record?.allowed_actions.includes(next) &&
    (next !== 'approve' ||
      (!!record.current_context && !!record.approval_preview && !currencyMismatch))
  const exactRetry = uncertain || pendingApplication
  const stale =
    !record ||
    record.etag !== reviewed ||
    record.current_step_id !== reviewedStep ||
    issue === 'conflict'
  const date = (value: string | null) =>
    value ? new Date(value).toLocaleString(i18n.resolvedLanguage) : t('unknown')
  function choose(next: TeamRequestAction) {
    if (!record || busy || exactRetry || !allowed(next) || admin) return
    setAction(next)
    setReviewed(record.etag)
    setReviewedStep(record.current_step_id)
    setIssue(null)
    intent.current = null
  }
  async function submit(retry = false) {
    if (
      lock.current ||
      !active() ||
      admin ||
      !fresh ||
      (!retry && (!action || stale || exactRetry || !allowed(action)))
    )
      return
    if (!retry) {
      if (action === 'reject' && !reason.trim()) {
        setIssue('requiredRejection')
        return
      }
      if (new TextEncoder().encode(reason.trim()).length > 2000) {
        setIssue('requiredReason')
        return
      }
      if (!reviewedStep) return
      intent.current = {
        actor,
        body: {
          decision_id: crypto.randomUUID(),
          step_id: reviewedStep,
          action: action!,
          reason: reason.trim(),
        },
        etag: reviewed!,
      }
    }
    const original = intent.current,
      latest = cache.getQueryData<Session>(sessionKey)
    if (!original || latest?.user.id !== original.actor) return
    lock.current = true
    setBusy(true)
    setIssue(null)
    try {
      const saved = await decideTeamRequest(
        id,
        actor,
        original.body,
        original.etag,
        latest.csrf_token,
      )
      if (!active()) return
      if (!visibleRef.current) {
        setUncertain(true)
        setIssue('uncertain')
        return
      }
      cache.setQueryData(queryKey, saved.request)
      setReceipt(saved)
      setUncertain(false)
      setAction(null)
      onSaved()
    } catch (error) {
      if (!active()) return
      const status = isAxiosError(error) ? error.response?.status : undefined,
        unknown = !status || status >= 500
      setUncertain((previous) => previous || unknown)
      setIssue(unknown || uncertain ? 'uncertain' : status === 409 ? 'conflict' : 'failed')
    } finally {
      lock.current = false
      if (active()) setBusy(false)
    }
  }
  return (
    <Drawer
      open
      title={t('detail')}
      description={t('detailHelp')}
      busy={busy}
      onOpenChange={(open) => {
        if (!open) onClose(uncertain || !!pendingApplication)
      }}
    >
      <div className="space-y-6">
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            disabled={busy || detail.isFetching}
            onClick={() => void detail.refetch()}
          >
            {t('refresh')}
          </Button>
          {admin && record?.workspace_available && (
            <Link
              className={buttonVariants({ variant: 'outline' })}
              to={`/quota-requests?tab=pending&request=${encodeURIComponent(id)}`}
            >
              {t('adminLink')}
            </Link>
          )}
        </div>
        <QueryState
          pending={detail.isFetching}
          error={detail.error}
          retry={() => void detail.refetch()}
        />
        {record && (
          <>
            <dl className="grid gap-4 text-sm sm:grid-cols-2">
              <div>
                <dt>{t('applicant')}</dt>
                <dd>{record.applicant_name || record.applicant_user_id}</dd>
              </div>
              <div>
                <dt>{t('team')}</dt>
                <dd>{record.team_name || record.team_id}</dd>
              </div>
              <div>
                <dt>{t('dimension')}</dt>
                <dd>{t(record.dimension)}</dd>
              </div>
              <div>
                <dt>{t('state')}</dt>
                <dd>
                  <Badge variant="outline">{t(record.status)}</Badge>
                </dd>
              </div>
              <div>
                <dt>{t('target')}</dt>
                <dd>
                  <QuotaValue
                    value={record.target_value}
                    dimension={record.dimension}
                    currency={record.currency}
                  />
                </dd>
              </div>
              <div>
                <dt>{t('submitted')}</dt>
                <dd>{date(record.created_at)}</dd>
              </div>
              <div className="sm:col-span-2">
                <dt>{t('reason')}</dt>
                <dd className="whitespace-pre-wrap break-words">{record.reason}</dd>
              </div>
            </dl>
            {record.escalation_reason && (
              <p className="rounded-lg border p-3 text-sm">
                {t('escalation')}: {t(record.escalation_reason)}
              </p>
            )}
            <QuotaContext context={record.submitted_snapshot} title={t('submittedSnapshot')} />
            {record.current_context ? (
              <QuotaContext context={record.current_context} title={t('current')} />
            ) : (
              <p role="status">{t('unavailableContext')}</p>
            )}
            <section className="space-y-3" aria-label={t('timeline')}>
              <h3 className="font-medium">{t('timeline')}</h3>
              <ol className="space-y-4 border-l pl-4">
                {record.steps.map((step) => (
                  <li className="space-y-1 text-sm" key={step.id}>
                    <p>
                      {t(step.stage)} · {t(`step_${step.status}`)}
                      {step.actor_name || step.actor_id
                        ? ` · ${step.actor_name || step.actor_id}`
                        : ''}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {t('stepTimes', {
                        entered: date(step.entered_at),
                        acted: date(step.acted_at),
                      })}
                    </p>
                    {step.reason && (
                      <p className="whitespace-pre-wrap break-words">{step.reason}</p>
                    )}
                  </li>
                ))}
              </ol>
            </section>
            {receipt && <p role="status">{t('stepSaved')}</p>}
            {record.application && (
              <p role="status">
                {t(
                  record.application.application_status === 'applied'
                    ? 'applied'
                    : record.application.application_status === 'superseded'
                      ? 'superseded'
                      : 'applicationPending',
                )}
              </p>
            )}
            {(uncertain || issue) && (
              <p role="alert" className="text-sm text-destructive">
                {t(uncertain ? 'uncertain' : issue!)}
              </p>
            )}
            {!admin && (
              <div className="flex flex-wrap gap-2">
                {exactRetry ? (
                  <Button disabled={busy || !fresh} onClick={() => void submit(true)}>
                    {t('retry')}
                  </Button>
                ) : (
                  record.allowed_actions
                    .filter((next) => allowed(next))
                    .map((next) => (
                      <Button
                        variant={next === 'approve' ? 'default' : 'outline'}
                        key={next}
                        onClick={() => choose(next)}
                        disabled={busy}
                      >
                        {t(next)}
                      </Button>
                    ))
                )}
              </div>
            )}
          </>
        )}
      </div>
      {action && !admin && (
        <Dialog
          open
          title={t(action)}
          description={t(`${action}Help`)}
          busy={busy}
          onOpenChange={(open) => {
            if (!open && !uncertain) setAction(null)
          }}
        >
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault()
              void submit()
            }}
          >
            {record && (
              <p className="text-sm">
                {t('target')}:{' '}
                <QuotaValue
                  value={record.target_value}
                  dimension={record.dimension}
                  currency={record.currency}
                />
              </p>
            )}
            {action === 'approve' && record && preview && (
              <section className="space-y-3" aria-label={t('approvalPreview')}>
                <h3 className="font-medium">{t('approvalPreview')}</h3>
                <Table aria-label={t('approvalPreview')}>
                  <thead>
                    <tr>
                      <th>{t('previewScope')}</th>
                      <th>{t('before')}</th>
                      <th>{t('after')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(['member', 'team'] as const).map((scope) => (
                      <tr key={scope}>
                        <td>{t(scope === 'member' ? 'previewMember' : 'previewTeam')}</td>
                        <td>
                          <QuotaValue
                            value={preview[`${scope}_before`]}
                            dimension={record.dimension}
                            currency={record.currency}
                          />
                        </td>
                        <td>
                          <QuotaValue
                            value={preview[`${scope}_after`]}
                            dimension={record.dimension}
                            currency={record.currency}
                          />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
                {preview.escalates && <p role="status">{t('previewEscalates')}</p>}
              </section>
            )}
            <FormField label={t('decisionReason')}>
              <Textarea
                aria-label={t('decisionReason')}
                disabled={busy || exactRetry}
                value={reason}
                onChange={(event) => setReason(event.target.value)}
              />
            </FormField>
            {(issue || stale || uncertain) && (
              <p role="alert">{t(uncertain ? 'uncertain' : stale ? 'conflict' : issue!)}</p>
            )}
            {!uncertain && stale && record && (
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  setReviewed(record.etag)
                  setReviewedStep(record.current_step_id)
                  setIssue(null)
                  intent.current = null
                }}
              >
                {t('useContext')}
              </Button>
            )}
            <div className="flex flex-wrap gap-2">
              <Button type="submit" disabled={busy || stale || exactRetry || !allowed(action)}>
                {t(busy ? 'loading' : 'confirm')}
              </Button>
              {uncertain && (
                <Button type="button" disabled={busy || !fresh} onClick={() => void submit(true)}>
                  {t('retry')}
                </Button>
              )}
              <Button
                type="button"
                variant="outline"
                disabled={busy || detail.isFetching}
                onClick={() => void detail.refetch()}
              >
                {t('refresh')}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => {
                  if (uncertain) onClose(true)
                  else setAction(null)
                }}
              >
                {t('cancel')}
              </Button>
            </div>
          </form>
        </Dialog>
      )}
    </Drawer>
  )
}
