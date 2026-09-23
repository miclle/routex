import { useRef, useState, type FormEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { decideProjectRequest, projectRequestError } from '@/api/project-requests'
import { useSession } from '@/hooks/use-auth'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Textarea } from '@/components/ui/textarea'
import { FormField } from '@/components/app/CatalogUI'
import type { ProjectRequest, ProjectRequestAction } from '@/types/project-requests'

export default function DetailDialog({
  request,
  active,
  canDecide,
  onClose,
  onSuccess,
  onRefresh,
}: {
  request: ProjectRequest
  active: boolean
  canDecide: boolean
  onClose: () => void
  onSuccess: () => void
  onRefresh: () => void
}) {
  const { t, i18n } = useTranslation('projectRequests')
  const session = useSession()
  const cache = useQueryClient()
  const [action, setAction] = useState<ProjectRequestAction | null>(null)
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [uncertain, setUncertain] = useState(false)
  const running = useRef(false)
  const own = request.applicant_user_id === session.data?.user.id
  const pending = request.status === 'pending'
  const allowed = (next: ProjectRequestAction) =>
    pending && (next === 'withdraw' ? own : canDecide && !own && (next !== 'approve' || active))
  const date = (value: string) =>
    new Intl.DateTimeFormat(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US', {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date(value))
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (running.current || !session.data || !action || !allowed(action)) return
    if (action === 'reject' && !reason.trim()) {
      setError('requiredReason')
      return
    }
    running.current = true
    setBusy(true)
    setError(null)
    try {
      await decideProjectRequest(
        request.project_id,
        request.id,
        { action, reason: reason.trim() },
        session.data.csrf_token,
      )
      onSuccess()
    } catch (caught) {
      const key = projectRequestError(caught)
      setError(key)
      if (key === 'failed' || key === 'unavailable') setUncertain(true)
      if (key === 'denied') void cache.invalidateQueries({ queryKey: ['permissions'] })
    } finally {
      running.current = false
      setBusy(false)
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      title={t('detailTitle')}
      description={t('detailDescription')}
      busy={busy}
      width={660}
    >
      <div className="space-y-5">
        <div className="flex flex-wrap items-center gap-2">
          <Badge>{t('kind')}</Badge>
          <Badge variant="outline">{t(request.status)}</Badge>
        </div>
        <dl className="grid grid-cols-[auto_1fr] gap-x-5 gap-y-3 text-sm">
          <dt className="text-muted-foreground">{t('requestId')}</dt>
          <dd className="break-all font-mono text-xs">{request.id}</dd>
          <dt className="text-muted-foreground">{t('applicant')}</dt>
          <dd>{request.applicant_user_id}</dd>
          <dt className="text-muted-foreground">{t('submitted')}</dt>
          <dd>{date(request.created_at)}</dd>
          <dt className="text-muted-foreground">{t('originalReason')}</dt>
          <dd className="whitespace-pre-wrap break-words">{request.reason}</dd>
        </dl>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-2 rounded-lg border p-4">
            <h3 className="text-sm font-medium">{t('baseline')}</h3>
            <p className="break-all font-mono text-xs">
              {request.baseline_model_ids.join(', ') || t('none')}
            </p>
          </div>
          <div className="space-y-2 rounded-lg border bg-muted/30 p-4">
            <h3 className="text-sm font-medium">{t('models')}</h3>
            {request.requested_model_ids.map((id) => (
              <p key={id} className="break-all font-mono text-xs">
                {id}
              </p>
            ))}
          </div>
        </div>
        <p className="text-xs leading-5 text-muted-foreground">{t('baselineHelp')}</p>
        {request.decided_at && (
          <dl className="grid grid-cols-[auto_1fr] gap-x-5 gap-y-3 text-sm">
            <dt>{t('decisionActor')}</dt>
            <dd>{request.decision_actor_id}</dd>
            <dt>{t('decisionTime')}</dt>
            <dd>{date(request.decided_at)}</dd>
            <dt>{t('decisionReason')}</dt>
            <dd className="whitespace-pre-wrap break-words">
              {request.decision_reason || t('none')}
            </dd>
          </dl>
        )}
        {pending && own && canDecide && (
          <p className="text-sm text-muted-foreground">{t('selfApproval')}</p>
        )}
        {pending && !active && <p className="text-sm text-muted-foreground">{t('inactive')}</p>}
        {pending && (
          <div className="flex flex-wrap gap-3">
            {(['approve', 'reject', 'withdraw'] as const).filter(allowed).map((next) => (
              <Button
                key={next}
                variant={action === next ? 'default' : 'outline'}
                disabled={busy || uncertain || error === 'conflict' || error === 'denied'}
                onClick={() => {
                  setAction(next)
                  setReason('')
                  setError(null)
                }}
              >
                {t(next)}
              </Button>
            ))}
          </div>
        )}
        {action && (
          <form className="space-y-4" onSubmit={(event) => void submit(event)}>
            <p className="text-sm">{t(`${action}Help`)}</p>
            <FormField label={t('decisionReason')}>
              <Textarea
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                maxLength={2000}
                required={action === 'reject'}
                readOnly={uncertain}
                disabled={busy}
              />
            </FormField>
            <Button
              type="submit"
              disabled={
                busy ||
                !allowed(action) ||
                error === 'conflict' ||
                error === 'denied' ||
                (action === 'reject' && !reason.trim())
              }
            >
              {busy ? t('sending') : uncertain ? t('retry') : t('confirm', { action: t(action) })}
            </Button>
          </form>
        )}
        {error && (
          <div
            role="alert"
            className="space-y-3 rounded-md border border-destructive/30 p-3 text-sm text-destructive"
          >
            <p>{t(error)}</p>
            {error === 'conflict' && (
              <Button variant="outline" onClick={onRefresh}>
                {t('refresh')}
              </Button>
            )}
          </div>
        )}
        <div className="flex justify-end">
          <Button variant="outline" disabled={busy} onClick={onClose}>
            {t('cancel')}
          </Button>
        </div>
      </div>
    </Dialog>
  )
}
