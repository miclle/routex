import { useLayoutEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  createTeamModelRequest,
  getTeamModelCandidate,
  teamModelOutcomeUnknown,
  validTeamModelReason,
} from '@/api/team-model-requests'
import { sessionKey, useSession } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type {
  TeamModelRequestIntent,
  TeamModelRequestTeam,
  TeamModelRequestTeamPage,
} from '@/types/team-model-requests'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { QueryState, FormField } from '@/components/app/CatalogUI'
import TeamRequestPicker from './picker'
import TeamRequestPanel from './requests'

export default function TeamAccessRequest({
  actorID,
  modelID,
  onBusy,
  onLocked,
  visible = true,
}: {
  actorID: string
  modelID: string
  onBusy: (value: boolean) => void
  onLocked: (value: boolean) => void
  visible?: boolean
}) {
  const { t } = useTranslation('teamModelRequests')
  const cache = useQueryClient(),
    session = useSession()
  const [pickerFresh, setPickerFresh] = useState(false)
  const [historyBusy, setHistoryBusy] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [team, setTeam] = useState<TeamModelRequestTeam | null>(null)
  const [reason, setReason] = useState('')
  const [reviewed, setReviewed] = useState<{
    teamID: string
    membershipID: string
    etag: string
  } | null>(null)
  const [intent, setIntent] = useState<TeamModelRequestIntent | null>(null)
  const [uncertain, setUncertain] = useState(false),
    [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<string | null>(null),
    [history, setHistory] = useState(false)
  const alive = useRef(true),
    lock = useRef(false)
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  const freshActor = !session.isError && !session.isFetching && session.data?.user.id === actorID
  const currentActor = () =>
    alive.current &&
    cache.getQueryData<Session>(sessionKey)?.user.id === actorID &&
    cache.getQueryState(sessionKey)?.status !== 'error' &&
    cache.getQueryState(sessionKey)?.fetchStatus !== 'fetching'
  const candidateKey = ['team-model-candidate', actorID, team?.id, team?.membership_id, modelID]
  const query = useQuery({
    queryKey: candidateKey,
    queryFn: ({ signal }) => getTeamModelCandidate(team!.id, modelID, signal),
    enabled: !!team && freshActor && (visible || !!intent),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const candidate =
    visible && freshActor && pickerFresh && query.isSuccess && !query.isFetching
      ? query.data
      : undefined
  if (candidate && team && reviewed === null)
    setReviewed({ teamID: team.id, membershipID: team.membership_id, etag: candidate.review_etag })
  const latestTeam = () => {
    const pages = cache.getQueriesData<{ pages: TeamModelRequestTeamPage[] }>({
      queryKey: ['team-model-request-teams', actorID],
    })
    return (
      pages
        .flatMap(([, value]) => value?.pages.flatMap((page) => page.items) ?? [])
        .find((item) => item.id === team?.id && item.membership_id !== team?.membership_id) ?? team
    )
  }
  const changed =
    !!candidate &&
    !!team &&
    (latestTeam()?.membership_id !== team.membership_id ||
      !reviewed ||
      reviewed.teamID !== team.id ||
      reviewed.membershipID !== team.membership_id ||
      reviewed.etag !== candidate.review_etag)
  async function send(event?: FormEvent) {
    event?.preventDefault()
    const auth = cache.getQueryData<Session>(sessionKey)
    if (
      lock.current ||
      !auth ||
      !currentActor() ||
      !pickerFresh ||
      !candidate ||
      !team ||
      cache.getQueryState(candidateKey)?.fetchStatus === 'fetching' ||
      cache.isFetching({ queryKey: ['team-model-request-teams', actorID] }) > 0
    )
      return
    if (
      !intent &&
      (changed ||
        latestTeam()?.membership_id !== team.membership_id ||
        candidate.team_granted ||
        candidate.pending_request)
    )
      return
    if (!intent && !validTeamModelReason(reason)) {
      setNotice('reasonInvalid')
      return
    }
    const captured = intent ?? {
      body: {
        request_id: crypto.randomUUID(),
        team_id: team.id,
        model_id: modelID,
        reason: reason.trim(),
      },
      etag: reviewed!.etag,
    }
    setIntent(captured)
    setBusy(true)
    onBusy(true)
    onLocked(true)
    lock.current = true
    try {
      await createTeamModelRequest(actorID, captured, auth.csrf_token)
      if (currentActor()) {
        setNotice('saved')
        setUncertain(false)
        setIntent(null)
        setHistory(true)
        setHistoryOpen(true)
        onBusy(false)
        onLocked(false)
        void cache.invalidateQueries({ queryKey: ['team-model-requests'] })
        void cache.invalidateQueries({ queryKey: ['team-model-candidate', actorID, team.id] })
        void cache.invalidateQueries({ queryKey: ['team-model-candidates', actorID, team.id] })
      }
    } catch (error) {
      if (currentActor()) {
        const unknown = uncertain || teamModelOutcomeUnknown(error)
        setUncertain(unknown)
        setNotice(unknown ? 'uncertain' : 'rejected')
        onBusy(unknown)
      }
    } finally {
      lock.current = false
      if (currentActor()) setBusy(false)
    }
  }
  return (
    <section className="space-y-4" aria-label={t('request')}>
      <p className="text-sm text-muted-foreground">{t('sourceHelp')}</p>
      <TeamRequestPicker
        onFresh={setPickerFresh}
        actor={freshActor ? actorID : ''}
        selected={team}
        onChange={(value) => {
          if (!intent) {
            setTeam(value)
            if (value.id !== team?.id) setReviewed(null)
            setNotice(null)
          }
        }}
        disabled={busy || !!intent}
        visible={visible && freshActor}
      />
      {team && visible && freshActor && (
        <QueryState
          pending={query.isFetching}
          error={query.error}
          retry={() => void query.refetch()}
        />
      )}
      {notice && <p role={notice === 'saved' ? 'status' : 'alert'}>{t(notice)}</p>}
      {candidate &&
        (candidate.team_granted ? (
          <p role="status">{t('granted')}</p>
        ) : candidate.pending_request ? (
          <p role="status">{t('requested')}</p>
        ) : (
          <form onSubmit={(event) => void send(event)} className="space-y-3">
            <FormField label={t('reason')}>
              <Textarea
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                disabled={busy || !!intent}
                rows={3}
              />
              <p className="text-xs text-muted-foreground">{t('reasonHelp')}</p>
            </FormField>
            {changed && <p role="alert">{t('conflict')}</p>}
            {!intent && (
              <Button type="submit" disabled={busy || changed || !reviewed}>
                {t('send')}
              </Button>
            )}
          </form>
        ))}
      {intent && (
        <Button disabled={busy || !candidate} onClick={() => void send()}>
          {t('retry')}
        </Button>
      )}
      {!uncertain && (changed || !!intent) && (
        <Button
          variant="outline"
          disabled={busy || !candidate || !team}
          onClick={() => {
            const current = latestTeam()!
            setTeam(current)
            setReviewed({
              teamID: current.id,
              membershipID: current.membership_id,
              etag: candidate!.review_etag,
            })
            setIntent(null)
            setNotice(null)
            onLocked(false)
          }}
        >
          {t('review')}
        </Button>
      )}
      <Button
        variant="outline"
        disabled={busy || historyBusy}
        onClick={() => {
          setHistory(true)
          setHistoryOpen((value) => !value)
        }}
      >
        {t('ownHistory')}
      </Button>
      {history && (
        <TeamRequestPanel
          key={actorID}
          ownTeam={team?.id}
          modelID={modelID}
          onBusy={(value) => {
            setHistoryBusy(value)
            onBusy(value || busy || uncertain)
            onLocked(value || !!intent)
          }}
          visible={visible && freshActor && historyOpen}
        />
      )}
    </section>
  )
}
