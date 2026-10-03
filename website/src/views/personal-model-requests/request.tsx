import { useLayoutEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  createPersonalModelRequest,
  getPersonalModelCandidate,
  personalModelOutcomeUnknown,
  validPersonalModelReason,
} from '@/api/personal-model-requests'
import { sessionKey, useSession } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { PersonalModelRequestIntent } from '@/types/personal-model-requests'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { QueryState, FormField } from '@/components/app/CatalogUI'
import RequestPanel from './requests'

export default function PersonalAccessRequest({
  actorID,
  modelID,
  onBusy,
  visible = true,
}: {
  actorID: string
  modelID: string
  onBusy: (value: boolean) => void
  visible?: boolean
}) {
  const { t } = useTranslation('personalModelRequests')
  const cache = useQueryClient()
  const session = useSession()
  const [reason, setReason] = useState('')
  const [reviewed, setReviewed] = useState<string | null>(null)
  const [intent, setIntent] = useState<PersonalModelRequestIntent | null>(null)
  const [uncertain, setUncertain] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  const [history, setHistory] = useState(false)
  const lock = useRef(false)
  const alive = useRef(true)
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  const currentActor = () =>
    alive.current &&
    !session.isError &&
    !session.isFetching &&
    cache.getQueryState(sessionKey)?.fetchStatus !== 'fetching' &&
    cache.getQueryData<Session>(sessionKey)?.user.id === actorID
  const query = useQuery({
    queryKey: ['personal-model-candidate', actorID, modelID],
    queryFn: ({ signal }) => getPersonalModelCandidate(modelID, signal),
    enabled: (visible || !!intent) && session.data?.user.id === actorID && !session.isError,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const candidate =
    visible &&
    query.isSuccess &&
    !query.isFetching &&
    !session.isError &&
    !session.isFetching &&
    session.data?.user.id === actorID
      ? query.data
      : undefined
  if (candidate && reviewed === null) setReviewed(candidate.review_etag)
  const changed = !!candidate && reviewed !== candidate.review_etag
  async function send(event?: FormEvent) {
    event?.preventDefault()
    const auth = cache.getQueryData<Session>(sessionKey)
    if (lock.current || !auth || !currentActor() || !candidate) return
    if (
      !intent &&
      (!candidate || changed || candidate.personal_granted || candidate.pending_request_id)
    )
      return
    if (!intent && !validPersonalModelReason(reason)) {
      setNotice('reasonInvalid')
      return
    }
    const captured = intent ?? {
      body: { request_id: crypto.randomUUID(), model_id: modelID, reason: reason.trim() },
      etag: reviewed!,
    }
    setIntent(captured)
    setBusy(true)
    onBusy(true)
    lock.current = true
    try {
      await createPersonalModelRequest(actorID, captured, auth.csrf_token)
      if (currentActor()) {
        setNotice('saved')
        setUncertain(false)
        setIntent(null)
        setHistory(true)
        onBusy(false)
        void cache.invalidateQueries({ queryKey: ['personal-model-requests'] })
        void cache.invalidateQueries({ queryKey: ['personal-model-candidate', actorID, modelID] })
        void cache.invalidateQueries({ queryKey: ['personal-model-candidates', actorID] })
        void cache.invalidateQueries({
          queryKey: ['personal-model-candidate-drawer', actorID, modelID],
          exact: true,
        })
      }
    } catch (error) {
      if (currentActor()) {
        const unknown = uncertain || personalModelOutcomeUnknown(error)
        setUncertain(unknown)
        setNotice(unknown ? 'uncertain' : 'rejected')
        onBusy(unknown)
      }
    } finally {
      lock.current = false
      if (currentActor()) setBusy(false)
    }
  }
  if (!visible && !intent && !history) return null
  return (
    <section className="space-y-4 border-t pt-5" aria-label={t('request')}>
      <p className="text-sm text-muted-foreground">{t('sourceHelp')}</p>
      <QueryState
        pending={query.isFetching}
        error={query.error}
        retry={() => void query.refetch()}
      />
      {notice && (
        <p role={notice === 'saved' ? 'status' : 'alert'} className="text-sm">
          {t(notice)}
        </p>
      )}
      {candidate &&
        (candidate.personal_granted ? (
          <p role="status">{t('granted')}</p>
        ) : candidate.pending_request_id ? (
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
            {changed && (
              <p role="alert" className="text-sm">
                {t('conflict')}
              </p>
            )}
            {!intent && (
              <Button type="submit" disabled={busy || changed || reviewed === null}>
                {t('send')}
              </Button>
            )}
          </form>
        ))}
      {intent && (
        <Button type="button" disabled={busy || !candidate} onClick={() => void send()}>
          {t('retry')}
        </Button>
      )}
      {!uncertain && (changed || !!intent) && (
        <Button
          variant="outline"
          disabled={busy || !candidate}
          onClick={() => {
            setReviewed(candidate!.review_etag)
            setIntent(null)
            setNotice(null)
          }}
        >
          {t('review')}
        </Button>
      )}
      <Button variant="outline" disabled={busy} onClick={() => setHistory((value) => !value)}>
        {t('ownHistory')}
      </Button>
      {history && <RequestPanel key={actorID} modelID={modelID} visible={visible} />}
    </section>
  )
}
