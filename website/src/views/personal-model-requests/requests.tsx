import { useLayoutEffect, useRef, useState } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  decidePersonalModelRequest,
  getPersonalModelRequest,
  listPersonalModelRequests,
  personalModelOutcomeUnknown,
  validPersonalModelReason,
} from '@/api/personal-model-requests'
import { sessionKey, useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import type { Session } from '@/types/auth'
import type {
  PersonalModelDecisionAction,
  PersonalModelDecisionIntent,
  PersonalModelDecisionReceipt,
  PersonalModelRequestStatus,
} from '@/types/personal-model-requests'
import { QueryState, FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Table } from '@/components/ui/table'
import { Dialog } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'

export default function RequestPanel({
  owner,
  modelID,
  visible = true,
}: {
  owner?: string
  modelID?: string
  visible?: boolean
}) {
  const { t, i18n } = useTranslation('personalModelRequests')
  const session = useSession()
  const access = usePermissions()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const authorized =
    !!actor &&
    !session.isFetching &&
    visible &&
    (!owner || (!access.isError && !access.isFetching && access.can('members.models.write')))
  const [status, setStatus] = useState<PersonalModelRequestStatus | ''>('')
  const [selected, setSelected] = useState<string | null>(null)
  const query = useInfiniteQuery({
    queryKey: ['personal-model-requests', actor, owner ?? actor, !!owner, status],
    queryFn: ({ pageParam, signal }) =>
      listPersonalModelRequests(actor, owner, status, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: authorized,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const fresh = authorized && query.isSuccess && !query.isFetching
  const rows = fresh
    ? query.data.pages
        .flatMap((page) => page.items)
        .filter((item) => !modelID || item.model_id === modelID)
    : []
  return (
    <section className="space-y-4" aria-label={t('history')}>
      {authorized && (
        <>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h3 className="font-medium">{t('history')}</h3>
            <select
              aria-label={t('status')}
              value={status}
              onChange={(event) => setStatus(event.target.value as PersonalModelRequestStatus | '')}
              className="h-9 rounded-md border bg-background px-3 text-sm"
            >
              <option value="">{t('allStatuses')}</option>
              {(['pending', 'approved', 'rejected', 'withdrawn', 'cancelled'] as const).map(
                (value) => (
                  <option key={value} value={value}>
                    {t(value === 'rejected' ? 'rejectedStatus' : value)}
                  </option>
                ),
              )}
            </select>
          </div>
          <QueryState
            pending={query.isFetching}
            error={query.error}
            retry={() => void query.refetch()}
          />
        </>
      )}
      {fresh && (
        <>
          <Table aria-label={t('history')}>
            <thead>
              <tr>
                {['model', 'applicant', 'reason', 'status', 'created', 'action'].map((key) => (
                  <th key={key}>{t(key)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((item) => (
                <tr key={item.id}>
                  <td>{item.model_name}</td>
                  <td>{item.applicant_name}</td>
                  <td className="max-w-48 truncate">{item.reason}</td>
                  <td>{t(item.status === 'rejected' ? 'rejectedStatus' : item.status)}</td>
                  <td>{new Date(item.created_at).toLocaleString(i18n.resolvedLanguage)}</td>
                  <td>
                    <Button size="sm" variant="outline" onClick={() => setSelected(item.id)}>
                      {t('details')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
          {!rows.length && <p className="text-sm text-muted-foreground">{t('empty')}</p>}
          {query.hasNextPage && (
            <Button
              variant="outline"
              disabled={query.isFetchingNextPage}
              onClick={() => void query.fetchNextPage()}
            >
              {t('loadMore')}
            </Button>
          )}
        </>
      )}
      {selected && (
        <RequestDetail
          key={`${actor}:${owner ?? actor}:${selected}`}
          actor={actor}
          owner={owner}
          requestID={selected}
          visible={fresh}
          onClose={() => setSelected(null)}
        />
      )}
    </section>
  )
}
function RequestDetail({
  actor,
  owner,
  requestID,
  visible,
  onClose,
}: {
  actor: string
  owner?: string
  requestID: string
  visible: boolean
  onClose: () => void
}) {
  const { t } = useTranslation('personalModelRequests')
  const cache = useQueryClient()
  const session = useSession()
  const access = usePermissions()
  const [action, setAction] = useState<PersonalModelDecisionAction | null>(null)
  const [reason, setReason] = useState('')
  const [actionReview, setActionReview] = useState<string | null>(null)
  const [intent, setIntent] = useState<PersonalModelDecisionIntent | null>(null)
  const [uncertain, setUncertain] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  const [receipt, setReceipt] = useState<PersonalModelDecisionReceipt | null>(null)
  const alive = useRef(true)
  const lock = useRef(false)
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  const currentActor = () =>
    alive.current &&
    cache.getQueryData<Session>(sessionKey)?.user.id === actor &&
    !session.isError &&
    !session.isFetching &&
    cache.getQueryState(sessionKey)?.fetchStatus !== 'fetching'
  const allowed =
    session.data?.user.id === actor &&
    !session.isError &&
    !session.isFetching &&
    (!owner || (access.can('members.models.write') && !access.isError && !access.isFetching))
  const query = useQuery({
    queryKey: ['personal-model-request', actor, owner ?? actor, !!owner, requestID],
    queryFn: ({ signal }) => getPersonalModelRequest(actor, owner, requestID, signal),
    enabled: visible && allowed,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const detail = visible && allowed && query.isSuccess && !query.isFetching ? query.data : undefined
  async function dispatch() {
    const auth = cache.getQueryData<Session>(sessionKey)
    if (!allowed || !auth || !currentActor() || lock.current || !detail) return
    if (
      owner &&
      (cache.getQueryState(['permissions', actor])?.fetchStatus === 'fetching' ||
        !cache.getQueryData<string[]>(['permissions', actor])?.includes('members.models.write'))
    )
      return
    if (
      !intent &&
      (!detail ||
        !action ||
        !detail.allowed_actions.includes(action) ||
        actionReview !== detail.review_etag)
    )
      return
    const normalized = action === 'withdraw' ? '' : reason.trim()
    if (
      !intent &&
      !validPersonalModelReason(action === 'withdraw' ? '' : reason, action === 'reject')
    ) {
      setNotice('reasonInvalid')
      return
    }
    const captured = intent ?? {
      body: { decision_id: crypto.randomUUID(), action: action!, reason: normalized },
      etag: actionReview!,
    }
    setIntent(captured)
    setBusy(true)
    lock.current = true
    try {
      const value = await decidePersonalModelRequest(
        actor,
        owner,
        requestID,
        captured,
        auth.csrf_token,
      )
      if (currentActor()) {
        setReceipt(value)
        setUncertain(false)
        setIntent(null)
        setAction(null)
        setNotice('decisionSaved')
        // Preserve the exact receipt in this dialog while refreshing surrounding records.
        void query.refetch()
        void cache.invalidateQueries({ queryKey: ['personal-model-workspace', actor, owner] })
        void cache.invalidateQueries({ queryKey: ['personal-model-requests'] })
        void cache.invalidateQueries({ queryKey: ['model-catalog'] })
        void cache.invalidateQueries({ queryKey: ['personal-model-candidate'] })
      }
    } catch (error) {
      if (currentActor()) {
        const unknown = uncertain || personalModelOutcomeUnknown(error)
        setUncertain(unknown)
        setNotice(unknown ? 'uncertain' : 'rejected')
      }
    } finally {
      lock.current = false
      if (currentActor()) setBusy(false)
    }
  }
  if (!visible || !allowed) return null
  return (
    <Dialog
      open
      busy={busy || uncertain}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      title={t('details')}
      description={t('sourceHelp')}
    >
      <div className="space-y-4">
        <QueryState
          pending={query.isFetching}
          error={query.error}
          retry={() => void query.refetch()}
        />
        {detail && (
          <>
            <dl className="space-y-2 text-sm">
              <div>
                <dt>{t('requestId')}</dt>
                <dd className="font-mono">{detail.id}</dd>
              </div>
              <div>
                <dt>{t('model')}</dt>
                <dd>{detail.model_name}</dd>
              </div>
              <div>
                <dt>{t('applicant')}</dt>
                <dd>{detail.applicant_name}</dd>
              </div>
              <div>
                <dt>{t('reason')}</dt>
                <dd>{detail.reason}</dd>
              </div>
              <div>
                <dt>{t('status')}</dt>
                <dd>{t(detail.status === 'rejected' ? 'rejectedStatus' : detail.status)}</dd>
              </div>
            </dl>
            <p className="text-sm">
              {t('currentGranted', { value: t(detail.current_granted ? 'yes' : 'no') })}
            </p>
            {detail.status === 'approved' && (
              <p role="status">{t(`application_${detail.application_status}`)}</p>
            )}
            {detail.decision && (
              <p className="text-sm">
                {t('recordedDecision')}: {detail.decision.actor_name} · {t(detail.decision.action)}{' '}
                · {detail.decision.reason}
              </p>
            )}
            {owner === actor && detail.status === 'pending' && <p>{t('selfReview')}</p>}
            {!intent && !receipt && (
              <div className="flex flex-wrap gap-2">
                {detail.allowed_actions
                  .filter((value) =>
                    owner
                      ? value !== 'withdraw' && detail.applicant_user_id !== actor
                      : value === 'withdraw',
                  )
                  .map((value) => (
                    <Button
                      key={value}
                      variant="outline"
                      onClick={() => {
                        setAction(value)
                        setActionReview(detail.review_etag)
                        setReason('')
                        setNotice(null)
                      }}
                    >
                      {t(value)}
                    </Button>
                  ))}
              </div>
            )}
            {action && !receipt && (
              <div className="space-y-3 rounded-lg border p-4">
                <h3 className="font-medium">{t('decisionTitle', { action: t(action) })}</h3>
                <p className="text-sm text-muted-foreground">{t('decisionHelp')}</p>
                {action !== 'withdraw' && (
                  <FormField label={t('reason')}>
                    <Textarea
                      value={reason}
                      onChange={(event) => setReason(event.target.value)}
                      disabled={busy || !!intent}
                      rows={3}
                    />
                  </FormField>
                )}
                {!intent && actionReview !== detail.review_etag && (
                  <>
                    <p role="alert">{t('conflict')}</p>
                    <Button variant="outline" onClick={() => setActionReview(detail.review_etag)}>
                      {t('review')}
                    </Button>
                  </>
                )}
                {!intent && (
                  <Button
                    disabled={busy || actionReview !== detail.review_etag}
                    onClick={() => void dispatch()}
                  >
                    {t(action)}
                  </Button>
                )}
              </div>
            )}
          </>
        )}
        {notice && <p role={receipt ? 'status' : 'alert'}>{t(notice)}</p>}
        {intent && (
          <Button disabled={busy || !detail} onClick={() => void dispatch()}>
            {t('retry')}
          </Button>
        )}
        {!uncertain && !!intent && (
          <Button
            variant="outline"
            disabled={busy || !detail}
            onClick={() => {
              setIntent(null)
              setNotice(null)
              setAction(null)
              void query.refetch()
            }}
          >
            {t('review')}
          </Button>
        )}
      </div>
    </Dialog>
  )
}
