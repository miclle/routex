import { useLayoutEffect, useRef, useState } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { isAxiosError } from 'axios'
import {
  compareTarget,
  createTeamRequest,
  requestTeams,
  teamRequestContext,
  validTarget,
} from '@/api/team-requests'
import { sessionKey, useSession } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { CreateTeamRequest, TeamQuotaDimension } from '@/types/team-requests'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import { QuotaContext } from './context'
export default function ApplicationDialog({
  onClose,
  onSaved,
}: {
  onClose: (unknown: boolean) => void
  onSaved: () => void
}) {
  const { t } = useTranslation('teamRequests')
  const session = useSession(),
    cache = useQueryClient()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const [team, setTeam] = useState(''),
    [dimension, setDimension] = useState<TeamQuotaDimension>('tokens')
  const [target, setTarget] = useState(''),
    [reason, setReason] = useState(''),
    [reviewed, setReviewed] = useState<string | null>(null)
  const [busy, setBusy] = useState(false),
    [uncertain, setUncertain] = useState(false),
    [issue, setIssue] = useState<string | null>(null)
  const intent = useRef<{
      actor: string
      team: string
      body: CreateTeamRequest
      etag: string
    } | null>(null),
    lock = useRef(false),
    mounted = useRef(false)
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const active = () =>
    mounted.current &&
    cache.getQueryData<Session>(sessionKey)?.user.id === actor &&
    cache.getQueryState(sessionKey)?.status !== 'error'
  const teams = useInfiniteQuery({
    queryKey: ['team-request-teams', actor],
    enabled: !!actor,
    queryFn: ({ pageParam, signal }) => requestTeams(pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    retry: false,
    gcTime: 0,
    staleTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const available = teams.isSuccess && !teams.isFetching && !teams.isError
  const options = available
    ? [
        ...new Map(
          teams.data.pages.flatMap((page) => page.items).map((item) => [item.id, item]),
        ).values(),
      ]
    : []
  const confirmed = options.some((item) => item.id === team)
  const context = useQuery({
    queryKey: ['team-request-context', actor, team, dimension],
    queryFn: ({ signal }) => teamRequestContext(team, dimension, signal),
    enabled: !!actor && confirmed,
    retry: false,
    gcTime: 0,
    staleTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const fresh = confirmed && context.isSuccess && !context.isFetching && !context.isError
  const current = fresh ? context.data : undefined
  const visibleRef = useRef(false)
  useLayoutEffect(() => {
    visibleRef.current = fresh
  })
  const stale = !current || reviewed !== current.etag || issue === 'conflict'
  function changeContext(nextTeam: string, nextDimension: TeamQuotaDimension) {
    if (busy || uncertain) return
    setTeam(nextTeam)
    setDimension(nextDimension)
    setReviewed(null)
    intent.current = null
    setIssue(null)
  }
  async function submit(retry = false) {
    if (
      lock.current ||
      !active() ||
      !fresh ||
      (!retry && (stale || uncertain || !current?.eligible))
    )
      return
    if (!retry) {
      const value = target.trim(),
        explanation = reason.trim()
      if (!validTarget(value, dimension)) {
        setIssue('invalidTarget')
        return
      }
      if (
        current!.member_effective === null ||
        compareTarget(value, current!.member_effective, dimension) <= 0
      ) {
        setIssue('notIncrease')
        return
      }
      if (!explanation || new TextEncoder().encode(explanation).length > 2000) {
        setIssue('requiredReason')
        return
      }
      intent.current = {
        actor,
        team,
        body: {
          request_id: crypto.randomUUID(),
          dimension,
          target_value: value,
          reason: explanation,
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
      await createTeamRequest(original.team, actor, original.body, original.etag, latest.csrf_token)
      if (active()) {
        if (visibleRef.current) onSaved()
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
  return (
    <Dialog
      open
      title={t('applyTitle')}
      description={t('applyHelp')}
      width={640}
      busy={busy}
      onOpenChange={(open) => {
        if (!open) onClose(uncertain)
      }}
    >
      <form
        className="space-y-4"
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
      >
        <fieldset disabled={busy || uncertain} className="space-y-4">
          <FormField label={t('team')}>
            <select
              className="w-full rounded-md border bg-background p-2"
              aria-label={t('team')}
              value={confirmed ? team : ''}
              disabled={!available}
              onChange={(event) => changeContext(event.target.value, dimension)}
            >
              <option value="">{t('selectTeam')}</option>
              {options.map((item) => (
                <option value={item.id} key={item.id}>
                  {item.name}
                </option>
              ))}
            </select>
          </FormField>
          <QueryState
            pending={teams.isFetching}
            error={teams.error}
            retry={() => void teams.refetch()}
          />
          {teams.hasNextPage && (
            <Button
              variant="outline"
              type="button"
              disabled={teams.isFetching}
              onClick={() => void teams.fetchNextPage()}
            >
              {t('more')}
            </Button>
          )}
          <FormField label={t('dimension')}>
            <select
              className="w-full rounded-md border bg-background p-2"
              aria-label={t('dimension')}
              value={dimension}
              onChange={(event) => changeContext(team, event.target.value as TeamQuotaDimension)}
            >
              {(['tokens', 'money'] as const).map((value) => (
                <option value={value} key={value}>
                  {t(value)}
                </option>
              ))}
            </select>
          </FormField>
          <FormField label={t('target')}>
            <Input
              aria-label={t('target')}
              inputMode={dimension === 'money' ? 'decimal' : 'numeric'}
              value={target}
              onValueChange={setTarget}
            />
          </FormField>
          <FormField label={t('reason')}>
            <Textarea
              aria-label={t('reason')}
              value={reason}
              onChange={(event) => setReason(event.target.value)}
            />
          </FormField>
        </fieldset>
        {team && (
          <QueryState
            pending={context.isFetching}
            error={context.error}
            retry={() => void context.refetch()}
          />
        )}
        {current && (
          <>
            <QuotaContext context={current} title={t('current')} />
            <p className="text-sm text-muted-foreground">{t('ownerFirst')}</p>
            {!current.eligible && <p role="alert">{t('blockers')}</p>}
            {!uncertain && stale && (
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  setReviewed(current.etag)
                  setIssue(null)
                  intent.current = null
                }}
              >
                {t('useContext')}
              </Button>
            )}
          </>
        )}
        {issue && (
          <p role="alert" className="text-sm text-destructive">
            {t(uncertain ? 'uncertain' : issue)}
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={busy || stale || uncertain || !current?.eligible}>
            {t(busy ? 'loading' : 'submit')}
          </Button>
          {uncertain && (
            <Button type="button" disabled={busy || !fresh} onClick={() => void submit(true)}>
              {t('retry')}
            </Button>
          )}
          <Button
            type="button"
            variant="outline"
            disabled={busy || context.isFetching}
            onClick={() => void context.refetch()}
          >
            {t('refresh')}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={busy}
            onClick={() => onClose(uncertain)}
          >
            {t('cancel')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
