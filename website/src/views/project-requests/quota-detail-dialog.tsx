import { useRef, useState, type FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  decideProjectRequest,
  projectQuotaRequestDetail,
  projectRequestError,
} from '@/api/project-requests'
import { useSession } from '@/hooks/use-auth'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Textarea } from '@/components/ui/textarea'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import type { ProjectRequestAction, ProjectRequestDecision } from '@/types/project-requests'
import { QuotaValues } from './quota-values'

type DecisionIntent = { actor: string; body: ProjectRequestDecision; etag?: string }

export default function QuotaDetailDialog({
  projectId,
  requestId,
  active,
  canDecide,
  authorized = true,
  onClose,
  onSaved,
}: {
  projectId: string
  requestId: string
  active: boolean
  canDecide: boolean
  authorized?: boolean
  onClose: (uncertain: boolean) => void
  onSaved: () => void
}) {
  const { t, i18n } = useTranslation('projectRequests')
  const session = useSession()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const csrf = session.data?.csrf_token ?? ''
  const detail = useQuery({
    queryKey: ['project-quota-request-detail', actor, projectId, requestId],
    queryFn: ({ signal }) => projectQuotaRequestDetail(projectId, requestId, signal),
    enabled: !!actor && authorized,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const [action, setAction] = useState<ProjectRequestAction | null>(null)
  const [reason, setReason] = useState('')
  const [reviewedETag, setReviewedETag] = useState<string | null>(null)
  const [intent, setIntent] = useState<DecisionIntent | null>(null)
  const [busy, setBusy] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [conflictAt, setConflictAt] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)
  const running = useRef(false)
  const current = actor && authorized && detail.isSuccess && !detail.isFetching && !detail.isError
  const record = current ? detail.data : undefined
  const own = record?.applicant_user_id === actor
  const currencyMismatch =
    record?.requested_quota.money_month !== undefined &&
    record?.requested_quota.currency !== record?.platform_currency
  const allowed = (next: ProjectRequestAction) =>
    !!record &&
    record.status === 'pending' &&
    (next === 'withdraw'
      ? own
      : canDecide && !own && (next !== 'approve' || (active && !currencyMismatch)))
  const needsReview =
    record?.status === 'pending' &&
    (conflict || (action === 'approve' && reviewedETag !== record.approval_review_etag))
  const pendingApplicationRetry =
    saved && record?.status === 'approved' && record.application_status === 'pending' && !!intent
  const exactRetry = uncertain || pendingApplicationRetry
  const date = (value: string) =>
    Number.isFinite(Date.parse(value))
      ? new Intl.DateTimeFormat(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US', {
          dateStyle: 'medium',
          timeStyle: 'short',
        }).format(new Date(value))
      : t('quota.unknown')
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (
      running.current ||
      !actor ||
      !csrf ||
      !action ||
      (!exactRetry && (!allowed(action) || needsReview))
    )
      return
    if (action !== 'withdraw' && !reason.trim()) {
      setError('quota.requiredDecisionReason')
      return
    }
    const next = exactRetry
      ? intent
      : {
          actor,
          body: { action, reason: reason.trim() },
          etag: action === 'approve' ? (reviewedETag ?? undefined) : undefined,
        }
    if (!next || next.actor !== actor) return
    setIntent(next)
    running.current = true
    setBusy(true)
    setError(null)
    try {
      const result = await decideProjectRequest(projectId, requestId, next.body, csrf, next.etag)
      if (result.kind !== 'QUOTA' || result.id !== requestId || result.project_id !== projectId)
        throw new Error('Invalid Project quota decision receipt')
      setUncertain(false)
      setConflict(false)
      setSaved(true)
      onSaved()
      await detail.refetch()
    } catch (caught) {
      const key = projectRequestError(caught)
      setError(key)
      if (key === 'failed' || key === 'unavailable') setUncertain(true)
      if (key === 'conflict') {
        setConflict(true)
        setConflictAt(detail.dataUpdatedAt)
      }
    } finally {
      running.current = false
      setBusy(false)
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose(uncertain)
      }}
      title={t('quota.detailTitle')}
      description={t('quota.detailDescription')}
      busy={busy}
      width={660}
    >
      <div className="space-y-5">
        <div className="flex justify-end">
          <Button
            variant="outline"
            disabled={busy || detail.isFetching}
            onClick={() => void detail.refetch()}
          >
            {t('quota.refreshDetail')}
          </Button>
        </div>
        <QueryState
          pending={detail.isFetching}
          error={detail.error}
          retry={() => void detail.refetch()}
        />
        {record && (
          <>
            <div className="flex flex-wrap gap-2">
              <Badge>{t('quota.kind')}</Badge>
              <Badge variant="outline">{t(record.status)}</Badge>
            </div>
            <dl className="grid grid-cols-[auto_1fr] gap-x-5 gap-y-3 text-sm">
              <dt>{t('requestId')}</dt>
              <dd className="break-all font-mono text-xs">{record.id}</dd>
              <dt>{t('applicant')}</dt>
              <dd>{record.applicant_user_id}</dd>
              <dt>{t('submitted')}</dt>
              <dd>{date(record.created_at)}</dd>
              <dt>{t('originalReason')}</dt>
              <dd className="whitespace-pre-wrap break-words">{record.reason}</dd>
            </dl>
            <div className="grid gap-3 sm:grid-cols-2">
              <section className="space-y-3 rounded-lg border p-4" aria-label={t('quota.baseline')}>
                <h3 className="text-sm font-medium">{t('quota.baseline')}</h3>
                <QuotaValues quota={record.baseline_quota} />
              </section>
              <section
                className="space-y-3 rounded-lg border bg-muted/30 p-4"
                aria-label={t('quota.requested')}
              >
                <h3 className="text-sm font-medium">{t('quota.requested')}</h3>
                <QuotaValues quota={record.requested_quota} patch />
              </section>
            </div>
            <section className="space-y-3 rounded-lg border p-4" aria-label={t('quota.current')}>
              <h3 className="text-sm font-medium">{t('quota.current')}</h3>
              <QuotaValues quota={record.current_quota} />
              <p className="text-xs text-muted-foreground">
                {t('quota.denomination', { currency: record.platform_currency })}
              </p>
            </section>
            <p className="text-xs text-muted-foreground">{t('quota.baselineHelp')}</p>
            {record.approved_quota && (
              <section
                className="space-y-3 rounded-lg border p-4"
                aria-label={t('quota.approvedSnapshot')}
              >
                <h3 className="text-sm font-medium">{t('quota.approvedSnapshot')}</h3>
                <QuotaValues quota={record.approved_quota} />
              </section>
            )}
            {record.status === 'approved' && (
              <p role="status" className="rounded-lg bg-muted p-3 text-sm">
                {t(
                  record.application_status === 'applied' && record.runtime_applied === true
                    ? 'quota.applicationApplied'
                    : record.application_status === 'superseded'
                      ? 'quota.applicationSuperseded'
                      : 'quota.applicationPending',
                )}
              </p>
            )}
            {record.decided_at && (
              <dl className="grid grid-cols-[auto_1fr] gap-x-5 gap-y-3 text-sm">
                <dt>{t('decisionActor')}</dt>
                <dd>{record.decision_actor_id}</dd>
                <dt>{t('decisionTime')}</dt>
                <dd>{date(record.decided_at)}</dd>
                <dt>{t('decisionReason')}</dt>
                <dd className="whitespace-pre-wrap break-words">
                  {record.decision_reason || t('none')}
                </dd>
              </dl>
            )}
            {own && canDecide && record.status === 'pending' && (
              <p className="text-sm text-muted-foreground">{t('selfApproval')}</p>
            )}
            {!active && record.status === 'pending' && (
              <p className="text-sm text-muted-foreground">{t('inactive')}</p>
            )}
            {currencyMismatch && record.status === 'pending' && (
              <p role="alert" className="text-sm text-muted-foreground">
                {t('quota.currencyMismatch')}
              </p>
            )}
            {record.status === 'pending' && (
              <div className="flex flex-wrap gap-3">
                {(['approve', 'reject', 'withdraw'] as const).filter(allowed).map((next) => (
                  <Button
                    key={next}
                    variant={action === next ? 'default' : 'outline'}
                    disabled={busy || uncertain || conflict || error === 'denied'}
                    onClick={() => {
                      setAction(next)
                      setReason('')
                      setError(null)
                      setConflict(false)
                      setReviewedETag(record.approval_review_etag ?? null)
                      setIntent(null)
                    }}
                  >
                    {t(next)}
                  </Button>
                ))}
              </div>
            )}
          </>
        )}
        {action && (record?.status === 'pending' || exactRetry) && (
          <form className="space-y-4" onSubmit={(event) => void submit(event)}>
            <p className="text-sm">{t(`quota.${action}Help`)}</p>
            {needsReview && !exactRetry && (
              <div role="alert" className="space-y-3 rounded-lg border p-3 text-sm">
                <p>{t('quota.reviewChanged')}</p>
                <Button
                  variant="outline"
                  disabled={
                    !record ||
                    record.status !== 'pending' ||
                    !allowed(action) ||
                    busy ||
                    (conflict && detail.dataUpdatedAt <= conflictAt)
                  }
                  onClick={() => {
                    setReviewedETag(record?.approval_review_etag ?? null)
                    setConflict(false)
                    setError(null)
                    setIntent(null)
                  }}
                >
                  {t('quota.useReviewed')}
                </Button>
              </div>
            )}
            <FormField label={t('decisionReason')}>
              <Textarea
                aria-label={t('decisionReason')}
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                maxLength={2000}
                required={action !== 'withdraw'}
                readOnly={exactRetry || saved}
                disabled={busy}
              />
            </FormField>
            {(!saved || pendingApplicationRetry) && (
              <Button
                type="submit"
                disabled={
                  busy ||
                  error === 'denied' ||
                  (!exactRetry && (!allowed(action) || needsReview)) ||
                  (action !== 'withdraw' && !reason.trim())
                }
              >
                {t(busy ? 'sending' : exactRetry ? 'retry' : 'confirm', { action: t(action) })}
              </Button>
            )}
          </form>
        )}
        {saved && (
          <p role="status" className="text-sm">
            {t('quota.decisionSaved')}
          </p>
        )}
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {t(error)}
          </p>
        )}
        {uncertain && (
          <p role="status" className="text-sm text-muted-foreground">
            {t('quota.uncertainDecision')}
          </p>
        )}
        <div className="flex justify-end">
          <Button variant="outline" disabled={busy} onClick={() => onClose(uncertain)}>
            {t('cancel')}
          </Button>
        </div>
      </div>
    </Dialog>
  )
}
