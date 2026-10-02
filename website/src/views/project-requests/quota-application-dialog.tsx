import { useRef, useState, type FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  createProjectRequest,
  projectQuotaContext,
  projectRequestError,
} from '@/api/project-requests'
import { useSession } from '@/hooks/use-auth'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import { parseInteger, validMoney } from '@/views/resource-limits/quota-values'
import type { ResourceRecord } from '@/types/resources'
import type {
  CreateProjectQuotaRequest,
  ProjectQuotaContext,
  ProjectQuotaPatch,
} from '@/types/project-requests'
import { QuotaValues } from './quota-values'

type Intent = { body: CreateProjectQuotaRequest; etag: string; actor: string }

export default function QuotaApplicationDialog({
  project,
  onClose,
  onSuccess,
}: {
  project: ResourceRecord
  onClose: (uncertain: boolean) => void
  onSuccess: () => void
}) {
  const { t } = useTranslation('projectRequests')
  const session = useSession()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const [busy, setBusy] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const context = useQuery({
    queryKey: ['project-request-quota-context', actor, project.id],
    queryFn: ({ signal }) => projectQuotaContext(project.id, signal),
    enabled: !!actor && project.status === 'active',
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose(uncertain)
      }}
      title={t('quota.applyTitle')}
      description={t('quota.applyDescription')}
      busy={busy}
      width={640}
    >
      <QueryState
        pending={context.isFetching}
        error={context.error}
        retry={() => void context.refetch()}
      />
      {context.data && actor && (
        <QuotaForm
          key={`${actor}:${project.id}`}
          project={project}
          context={context.data}
          current={!context.isFetching && !context.isError}
          updatedAt={context.dataUpdatedAt}
          actor={actor}
          csrf={session.data?.csrf_token ?? ''}
          busy={busy}
          setBusy={setBusy}
          onUncertain={() => setUncertain(true)}
          refresh={() => void context.refetch()}
          onClose={() => onClose(uncertain)}
          onSuccess={onSuccess}
        />
      )}
    </Dialog>
  )
}

function QuotaForm({
  project,
  context,
  current,
  updatedAt,
  actor,
  csrf,
  busy,
  setBusy,
  onUncertain,
  refresh,
  onClose,
  onSuccess,
}: {
  project: ResourceRecord
  context: ProjectQuotaContext
  current: boolean
  updatedAt: number
  actor: string
  csrf: string
  busy: boolean
  setBusy: (value: boolean) => void
  onUncertain: () => void
  refresh: () => void
  onClose: () => void
  onSuccess: () => void
}) {
  const { t } = useTranslation('projectRequests')
  const [reviewed, setReviewed] = useState(context)
  const [tokens, setTokens] = useState('')
  const [money, setMoney] = useState('')
  const [reason, setReason] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [conflict, setConflict] = useState(false)
  const [conflictAt, setConflictAt] = useState(0)
  const [intent, setIntent] = useState<Intent | null>(null)
  const [uncertain, setUncertain] = useState(false)
  const running = useRef(false)
  const needsReview = conflict || context.review_etag !== reviewed.review_etag
  const changed = tokens.trim() !== '' || money.trim() !== ''
  const frozen = busy || uncertain
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (
      running.current ||
      busy ||
      (!uncertain && project.status !== 'active') ||
      !actor ||
      !csrf ||
      (!uncertain && (!current || needsReview))
    )
      return
    let next = intent
    if (!uncertain) {
      const value = parseInteger(tokens)
      const amount = money.trim()
      if (!changed || !reason.trim()) {
        setError('quota.required')
        return
      }
      if (value === undefined || (amount && !validMoney(amount))) {
        setError('quota.invalidValues')
        return
      }
      const patch: ProjectQuotaPatch = {}
      if (value !== null) patch.tokens_month = value
      if (amount) {
        patch.money_month = amount
        patch.currency = reviewed.platform_currency
      }
      next = {
        body: {
          request_id: crypto.randomUUID(),
          kind: 'QUOTA',
          quota: patch,
          reason: reason.trim(),
        },
        etag: reviewed.review_etag,
        actor,
      }
      setIntent(next)
    }
    if (!next || next.actor !== actor) return
    running.current = true
    setBusy(true)
    setError(null)
    try {
      const saved = await createProjectRequest(project.id, next.body, csrf, next.etag)
      if (saved.kind !== 'QUOTA' || saved.project_id !== project.id || !saved.id)
        throw new Error('Invalid Project quota request receipt')
      onSuccess()
    } catch (caught) {
      const key = projectRequestError(caught)
      setError(key)
      if (key === 'failed' || key === 'unavailable') {
        setUncertain(true)
        onUncertain()
      }
      if (key === 'conflict') {
        setConflict(true)
        setConflictAt(updatedAt)
      }
    } finally {
      running.current = false
      setBusy(false)
    }
  }
  return (
    <form className="space-y-5" onSubmit={(event) => void submit(event)}>
      {current && (
        <section className="space-y-3 rounded-lg bg-muted p-4" aria-label={t('quota.current')}>
          <h3 className="text-sm font-medium">{t('quota.current')}</h3>
          <QuotaValues quota={context.current_quota} />
          <p className="text-xs text-muted-foreground">
            {t('quota.denomination', { currency: context.platform_currency })}
          </p>
        </section>
      )}
      {needsReview && !uncertain && (
        <div role="alert" className="space-y-3 rounded-lg border p-3 text-sm">
          <p>{t('quota.reviewChanged')}</p>
          <Button
            variant="outline"
            disabled={!current || busy || (conflict && updatedAt <= conflictAt)}
            onClick={() => {
              setReviewed(context)
              setConflict(false)
              setError(null)
              setIntent(null)
            }}
          >
            {t('quota.useReviewed')}
          </Button>
        </div>
      )}
      <section className="space-y-3" aria-label={t('quota.monthly')}>
        <h3 className="font-medium">{t('quota.monthly')}</h3>
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField label={t('quota.tokens')}>
            <Input
              aria-label={t('quota.tokens')}
              value={tokens}
              onChange={(event) => setTokens(event.target.value)}
              inputMode="numeric"
              placeholder={t('quota.keep')}
              disabled={frozen}
              maxLength={20}
            />
          </FormField>
          <FormField label={t('quota.moneyIn', { currency: reviewed.platform_currency })}>
            <Input
              aria-label={t('quota.moneyIn', { currency: reviewed.platform_currency })}
              value={money}
              onChange={(event) => setMoney(event.target.value)}
              inputMode="decimal"
              placeholder={t('quota.keep')}
              disabled={frozen}
              maxLength={40}
            />
          </FormField>
        </div>
        <p className="text-xs text-muted-foreground">{t('quota.fieldHelp')}</p>
      </section>
      <FormField label={t('reason')}>
        <Textarea
          aria-label={t('reason')}
          value={reason}
          onChange={(event) => setReason(event.target.value)}
          placeholder={t('quota.reasonPlaceholder')}
          maxLength={2000}
          required
          readOnly={uncertain}
          disabled={busy}
        />
      </FormField>
      {error && (
        <div
          role="alert"
          className="space-y-3 rounded-md border border-destructive/30 p-3 text-sm text-destructive"
        >
          <p>{t(error)}</p>
          {error === 'conflict' && !uncertain && (
            <Button variant="outline" onClick={refresh}>
              {t('quota.refreshContext')}
            </Button>
          )}
        </div>
      )}
      {uncertain && (
        <p role="status" className="text-sm text-muted-foreground">
          {t('quota.uncertainCreate')}
        </p>
      )}
      <div className="flex justify-end gap-3">
        <Button variant="outline" disabled={busy} onClick={onClose}>
          {t('cancel')}
        </Button>
        <Button
          type="submit"
          disabled={
            busy ||
            (!uncertain && project.status !== 'active') ||
            error === 'denied' ||
            (!uncertain && (!current || needsReview || !changed || !reason.trim()))
          }
        >
          {t(busy ? 'sending' : uncertain ? 'retry' : 'send')}
        </Button>
      </div>
    </form>
  )
}
