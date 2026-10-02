import { useLayoutEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  createProjectRequest,
  projectRequestLimitsContext,
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
  CreateProjectPolicyRequest,
  ProjectRequestLimitsContext,
  ProjectQuotaPatch,
  ProjectRateLimitPatch,
} from '@/types/project-requests'
import { QuotaValues } from './quota-values'
import { RateValues } from './rate-values'

type Intent = {
  body: CreateProjectPolicyRequest
  etag: string
  actor: string
  status: 'not_sent' | 'saved' | 'failed' | 'unknown'
  error?: string
  recordId?: string
  recordStatus?: string
}
const rateFields = ['rpm', 'tpm', 'concurrency'] as const

export default function QuotaApplicationDialog({
  project,
  onClose,
  onSuccess,
}: {
  project: ResourceRecord
  onClose: (uncertain: boolean) => void
  onSuccess: (kinds: CreateProjectPolicyRequest['kind'][]) => void
}) {
  const { t } = useTranslation('projectRequests')
  const session = useSession()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const [busy, setBusy] = useState(false)
  const [incomplete, setIncomplete] = useState(false)
  const context = useQuery({
    queryKey: ['project-request-limits-context', actor, project.id],
    queryFn: ({ signal }) => projectRequestLimitsContext(project.id, signal),
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
        if (!open) onClose(incomplete)
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
        <LimitForm
          key={`${actor}:${project.id}`}
          project={project}
          context={context.data}
          current={!context.isFetching && !context.isError}
          updatedAt={context.dataUpdatedAt}
          actor={actor}
          csrf={session.data?.csrf_token ?? ''}
          busy={busy}
          setBusy={setBusy}
          onIncomplete={() => setIncomplete(true)}
          refresh={() => void context.refetch()}
          onClose={() => onClose(incomplete)}
          onSuccess={onSuccess}
        />
      )}
    </Dialog>
  )
}

function LimitForm({
  project,
  context,
  current,
  updatedAt,
  actor,
  csrf,
  busy,
  setBusy,
  onIncomplete,
  refresh,
  onClose,
  onSuccess,
}: {
  project: ResourceRecord
  context: ProjectRequestLimitsContext
  current: boolean
  updatedAt: number
  actor: string
  csrf: string
  busy: boolean
  setBusy: (value: boolean) => void
  onIncomplete: () => void
  refresh: () => void
  onClose: () => void
  onSuccess: (kinds: CreateProjectPolicyRequest['kind'][]) => void
}) {
  const { t } = useTranslation('projectRequests')
  const [reviewed, setReviewed] = useState(context)
  const [tokens, setTokens] = useState('')
  const [money, setMoney] = useState('')
  const [rates, setRates] = useState({ rpm: '', tpm: '', concurrency: '' })
  const [reason, setReason] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [conflict, setConflict] = useState(false)
  const [conflictAt, setConflictAt] = useState(0)
  const [intents, setIntents] = useState<Intent[] | null>(null)
  const running = useRef(false)
  const mounted = useRef(true)
  const identity = useRef({ actor, csrf })
  useLayoutEffect(() => {
    identity.current = { actor, csrf }
  }, [actor, csrf])
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const uncertain = intents?.some((intent) => intent.status === 'unknown') === true
  const hasSaved = intents?.some((intent) => intent.status === 'saved') === true
  const needsReview = conflict || context.review_etag !== reviewed.review_etag
  const quotaChanged = tokens.trim() !== '' || money.trim() !== ''
  const rateChanged = rateFields.some((key) => rates[key].trim() !== '')
  const changed = quotaChanged || rateChanged
  const frozen = busy || uncertain || hasSaved

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (
      running.current ||
      busy ||
      !actor ||
      !csrf ||
      (!uncertain && (project.status !== 'active' || !current || needsReview))
    )
      return
    let next = intents
    if (!next) {
      if (!changed || !reason.trim()) {
        setError('quota.required')
        return
      }
      const value = parseInteger(tokens)
      const amount = money.trim()
      const parsed = Object.fromEntries(rateFields.map((key) => [key, parseInteger(rates[key])]))
      if (
        value === undefined ||
        (amount && !validMoney(amount)) ||
        rateFields.some((key) => parsed[key] === undefined)
      ) {
        setError('quota.invalidValues')
        return
      }
      next = []
      if (quotaChanged) {
        const quota: ProjectQuotaPatch = {}
        if (value !== null) quota.tokens_month = value
        if (amount) {
          quota.money_month = amount
          quota.currency = reviewed.platform_currency
        }
        next.push({
          body: { request_id: crypto.randomUUID(), kind: 'QUOTA', quota, reason: reason.trim() },
          etag: reviewed.review_etag,
          actor,
          status: 'not_sent',
        })
      }
      if (rateChanged) {
        const rate_limit: ProjectRateLimitPatch = {}
        for (const key of rateFields) if (parsed[key] !== null) rate_limit[key] = parsed[key]
        next.push({
          body: {
            request_id: crypto.randomUUID(),
            kind: 'RATE_LIMIT',
            rate_limit,
            reason: reason.trim(),
          },
          etag: reviewed.review_etag,
          actor,
          status: 'not_sent',
        })
      }
      setIntents(next)
    }
    if (next.some((intent) => intent.actor !== actor)) return
    running.current = true
    setBusy(true)
    setError(null)
    let allSaved = true
    try {
      for (let index = 0; index < next.length; index++) {
        const intent: Intent = next[index]
        if (intent.status === 'saved') continue
        if (!mounted.current || identity.current.actor !== intent.actor || !identity.current.csrf)
          return
        try {
          const saved = await createProjectRequest(
            project.id,
            intent.body,
            identity.current.csrf,
            intent.etag,
          )
          if (!mounted.current) return
          if (saved.kind !== intent.body.kind || saved.project_id !== project.id || !saved.id)
            throw new Error('Invalid Project policy request receipt')
          next = next.map((item, position) =>
            position === index
              ? {
                  ...item,
                  status: 'saved',
                  error: undefined,
                  recordId: saved.id,
                  recordStatus: saved.status,
                }
              : item,
          )
          setIntents(next)
        } catch (caught) {
          if (!mounted.current) return
          const key = projectRequestError(caught)
          const unknown = intent.status === 'unknown' || key === 'failed' || key === 'unavailable'
          next = next.map((item, position) =>
            position === index
              ? { ...item, status: unknown ? 'unknown' : 'failed', error: key }
              : item,
          )
          setIntents(next)
          setError(key)
          if (key === 'conflict') {
            setConflict(true)
            setConflictAt(updatedAt)
          }
          onIncomplete()
          allSaved = false
          break
        }
      }
      if (allSaved && mounted.current) onSuccess(next.map((intent) => intent.body.kind))
    } finally {
      running.current = false
      if (mounted.current) setBusy(false)
    }
  }
  return (
    <form className="space-y-5" onSubmit={(event) => void submit(event)}>
      {current && (
        <section className="space-y-3 rounded-lg bg-muted p-4" aria-label={t('quota.current')}>
          <h3 className="text-sm font-medium">{t('quota.current')}</h3>
          <QuotaValues quota={context.current_quota} />
          <h3 className="text-sm font-medium">{t('rate.current')}</h3>
          <RateValues rate={context.current_rate_limit} />
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
              setIntents((previous) =>
                previous?.some((intent) => intent.status === 'saved')
                  ? previous.map((intent) =>
                      intent.status === 'saved'
                        ? intent
                        : {
                            ...intent,
                            body: { ...intent.body, request_id: crypto.randomUUID() },
                            etag: context.review_etag,
                            status: 'not_sent',
                            error: undefined,
                          },
                    )
                  : null,
              )
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
              onChange={(event) => {
                setTokens(event.target.value)
                if (!frozen) setIntents(null)
              }}
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
              onChange={(event) => {
                setMoney(event.target.value)
                if (!frozen) setIntents(null)
              }}
              inputMode="decimal"
              placeholder={t('quota.keep')}
              disabled={frozen}
              maxLength={40}
            />
          </FormField>
        </div>
      </section>
      <section className="space-y-3" aria-label={t('rate.kind')}>
        <h3 className="font-medium">{t('rate.kind')}</h3>
        <div className="grid gap-4 sm:grid-cols-3">
          {rateFields.map((key) => (
            <FormField key={key} label={t(`rate.${key}`)}>
              <Input
                aria-label={t(`rate.${key}`)}
                value={rates[key]}
                onChange={(event) => {
                  setRates({ ...rates, [key]: event.target.value })
                  if (!frozen) setIntents(null)
                }}
                inputMode="numeric"
                placeholder={t('quota.keep')}
                disabled={frozen}
                maxLength={20}
              />
            </FormField>
          ))}
        </div>
        <p className="text-xs text-muted-foreground">{t('quota.fieldHelp')}</p>
        <p className="text-xs text-muted-foreground">{t('rate.separateRequests')}</p>
      </section>
      <FormField label={t('reason')}>
        <Textarea
          aria-label={t('reason')}
          value={reason}
          onChange={(event) => {
            setReason(event.target.value)
            if (!frozen) setIntents(null)
          }}
          placeholder={t('quota.reasonPlaceholder')}
          maxLength={2000}
          required
          readOnly={uncertain || hasSaved}
          disabled={busy}
        />
      </FormField>
      {intents && (
        <ul className="space-y-2" aria-label={t('rate.submissionProgress')}>
          {intents.map((intent) => (
            <li
              key={intent.body.request_id}
              className="rounded-lg border p-3 text-sm"
              role="status"
            >
              <span className="font-medium">
                {t(intent.body.kind === 'QUOTA' ? 'quota.kind' : 'rate.kind')}
              </span>
              {' · '}
              {t(`rate.submission.${intent.status}`)}
              {intent.recordId && (
                <span className="ml-2 break-all font-mono text-xs">{intent.recordId}</span>
              )}
              {intent.recordStatus && <span className="ml-2">{t(intent.recordStatus)}</span>}
            </li>
          ))}
        </ul>
      )}
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
            error === 'denied' ||
            (!uncertain &&
              (project.status !== 'active' ||
                !current ||
                needsReview ||
                !changed ||
                !reason.trim()))
          }
        >
          {t(busy ? 'sending' : intents ? 'retry' : 'send')}
        </Button>
      </div>
    </form>
  )
}
