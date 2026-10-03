import { useLayoutEffect, useRef, useState } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  decideTeamModelRequest,
  getTeamModelRequest,
  listTeamModelRequests,
  teamModelOutcomeUnknown,
  validTeamModelReason,
} from '@/api/team-model-requests'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type {
  TeamModelDecisionAction,
  TeamModelDecisionIntent,
  TeamModelDecisionReceipt,
  TeamModelRequestStatus,
  TeamModelWorkspace,
} from '@/types/team-model-requests'
import { QueryState, FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Table } from '@/components/ui/table'
import { Dialog } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { useTeamModelReview } from './authority'

export default function TeamRequestPanel({
  team,
  ownTeam,
  modelID,
  visible = true,
  onBusy,
}: {
  team?: string
  ownTeam?: string
  modelID?: string
  visible?: boolean
  onBusy?: (value: boolean) => void
}) {
  const { t, i18n } = useTranslation('teamModelRequests')
  const review = useTeamModelReview(team, visible)
  const { actor, session } = review
  const authorized = !!actor && !session.isFetching && visible && (!team || !!review.current)
  const [status, setStatus] = useState<TeamModelRequestStatus | ''>('')
  const [selected, setSelected] = useState<string | null>(null)
  const query = useInfiniteQuery({
    queryKey: ['team-model-requests', actor, team ?? '', ownTeam ?? '', modelID ?? '', status],
    queryFn: ({ pageParam, signal }) =>
      listTeamModelRequests(actor, team, status, pageParam, { teamID: ownTeam, modelID }, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page, pages) =>
      page.next_cursor && !pages.slice(0, -1).some((old) => old.next_cursor === page.next_cursor)
        ? page.next_cursor
        : undefined,
    enabled: authorized,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const fresh = authorized && query.isSuccess && !query.isFetching
  const rows = fresh
    ? [
        ...new Map(
          query.data.pages.flatMap((page) => page.items).map((row) => [row.id, row]),
        ).values(),
      ]
    : []
  return (
    <section className="space-y-4" aria-label={t(team ? 'history' : 'ownHistory')}>
      {team && visible && (
        <QueryState
          pending={review.isFetching}
          error={review.error}
          retry={() => void review.refetch()}
        />
      )}
      {authorized && (
        <>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h3 className="font-medium">{t(team ? 'history' : 'ownHistory')}</h3>
            <select
              aria-label={t('status')}
              value={status}
              onChange={(event) => setStatus(event.target.value as TeamModelRequestStatus | '')}
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
          <Table aria-label={t(team ? 'history' : 'ownHistory')}>
            <thead>
              <tr>
                {['team', 'model', 'applicant', 'reason', 'status', 'created', 'action'].map(
                  (key) => (
                    <th key={key}>{t(key)}</th>
                  ),
                )}
              </tr>
            </thead>
            <tbody>
              {rows.map((item) => (
                <tr key={item.id}>
                  <td>{item.team_name}</td>
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
        <TeamRequestDetail
          key={`${actor}:${team ?? ''}:${selected}`}
          actor={actor}
          team={team}
          requestID={selected}
          visible={fresh}
          onBusy={onBusy}
          onClose={() => setSelected(null)}
        />
      )}
    </section>
  )
}
function TeamRequestDetail({
  actor,
  team,
  requestID,
  visible,
  onClose,
  onBusy,
}: {
  actor: string
  team?: string
  requestID: string
  visible: boolean
  onClose: () => void
  onBusy?: (value: boolean) => void
}) {
  const { t } = useTranslation('teamModelRequests')
  const cache = useQueryClient()
  const review = useTeamModelReview(team, visible)
  const { session } = review
  const [action, setAction] = useState<TeamModelDecisionAction | null>(null)
  const [reason, setReason] = useState('')
  const [actionReview, setActionReview] = useState<string | null>(null)
  const [intent, setIntent] = useState<TeamModelDecisionIntent | null>(null)
  const [uncertain, setUncertain] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  const [receipt, setReceipt] = useState<TeamModelDecisionReceipt | null>(null)
  const alive = useRef(true),
    lock = useRef(false)
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  const currentActor = () =>
    alive.current &&
    cache.getQueryData<Session>(sessionKey)?.user.id === actor &&
    cache.getQueryState(sessionKey)?.status !== 'error' &&
    cache.getQueryState(sessionKey)?.fetchStatus !== 'fetching'
  const allowed =
    session.data?.user.id === actor &&
    !session.isError &&
    !session.isFetching &&
    (!team || !!review.current)
  const detailKey = ['team-model-request', actor, team ?? '', requestID]
  const query = useQuery({
    queryKey: detailKey,
    queryFn: ({ signal }) => getTeamModelRequest(actor, team, requestID, signal),
    enabled: visible && allowed,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const detail = visible && allowed && query.isSuccess && !query.isFetching ? query.data : undefined
  async function dispatch() {
    const auth = cache.getQueryData<Session>(sessionKey)
    if (
      !auth ||
      !allowed ||
      !currentActor() ||
      !detail ||
      lock.current ||
      cache.getQueryState(detailKey)?.status !== 'success' ||
      cache.getQueryState(detailKey)?.fetchStatus === 'fetching'
    )
      return
    if (
      team &&
      (cache.getQueryState(['team-model-workspace', actor, team])?.status !== 'success' ||
        cache.getQueryState(['team-model-workspace', actor, team])?.fetchStatus === 'fetching' ||
        !cache.getQueryData<TeamModelWorkspace>(['team-model-workspace', actor, team])
          ?.can_review_requests)
    )
      return
    if (
      !intent &&
      (!action ||
        !detail.allowed_actions.includes(action) ||
        actionReview !== detail.review_etag ||
        (team
          ? action === 'withdraw' || detail.applicant_user_id === actor
          : action !== 'withdraw'))
    )
      return
    if (
      !intent &&
      !validTeamModelReason(action === 'withdraw' ? '' : reason, action === 'reject')
    ) {
      setNotice('reasonInvalid')
      return
    }
    const captured = intent ?? {
      body: {
        decision_id: crypto.randomUUID(),
        action: action!,
        reason: action === 'withdraw' ? '' : reason.trim(),
      },
      etag: actionReview!,
    }
    setIntent(captured)
    setBusy(true)
    onBusy?.(true)
    lock.current = true
    try {
      const value = await decideTeamModelRequest(actor, team, requestID, captured, auth.csrf_token)
      if (currentActor()) {
        setReceipt(value)
        setUncertain(false)
        setIntent(null)
        setAction(null)
        setNotice('decisionSaved')
        onBusy?.(false)
        void query.refetch()
        void cache.invalidateQueries({ queryKey: ['team-model-workspace'] })
        void cache.invalidateQueries({ queryKey: ['team-model-requests'] })
        void cache.invalidateQueries({ queryKey: ['team-model-candidate'] })
        void cache.invalidateQueries({ queryKey: ['model-catalog'] })
        void cache.invalidateQueries({ queryKey: ['resources', 'teams'] })
      }
    } catch (error) {
      if (currentActor()) {
        const unknown = uncertain || teamModelOutcomeUnknown(error)
        setUncertain(unknown)
        setNotice(unknown ? 'uncertain' : 'rejected')
        onBusy?.(unknown)
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
              {[
                ['requestId', detail.id],
                ['team', detail.team_name],
                ['model', detail.model_name],
                ['applicant', detail.applicant_name],
                ['reason', detail.reason],
                ['status', t(detail.status === 'rejected' ? 'rejectedStatus' : detail.status)],
              ].map(([key, value]) => (
                <div key={key}>
                  <dt>{t(key)}</dt>
                  <dd>{value}</dd>
                </div>
              ))}
            </dl>
            <p className="text-sm text-muted-foreground">{t('sharedHelp')}</p>
            {detail.application_status === 'unavailable' ? (
              <p role="status">{t('currentUnavailable')}</p>
            ) : (
              <>
                <p>{t('currentGranted', { value: t(detail.current_granted ? 'yes' : 'no') })}</p>
                {detail.status === 'approved' && (
                  <p role="status">{t(`application_${detail.application_status}`)}</p>
                )}
              </>
            )}
            {detail.decision && (
              <p className="text-sm">
                {t('recordedDecision')}: {detail.decision.actor_name} · {t(detail.decision.action)}{' '}
                · {detail.decision.reason}
              </p>
            )}
            {team && detail.applicant_user_id === actor && detail.status === 'pending' && (
              <p>{t('selfReview')}</p>
            )}
            {!intent && !receipt && (
              <div className="flex flex-wrap gap-2">
                {detail.allowed_actions
                  .filter((value) =>
                    team
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
        {!uncertain && intent && (
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
