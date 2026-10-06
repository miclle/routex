import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { MonthlyAccountsContent } from './monthly-accounts'
import IdentityLabels from './identity-labels'
import {
  getOverviewAccounts,
  getOverviewRoles,
  overviewAccountsKey,
  overviewRolesKey,
} from '@/api/overview'
import type { Session } from '@/types/auth'
import UsageOverview from './usage-overview'
import { Button } from '@/components/ui/button'
import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { sessionKey, useSession } from '@/hooks/use-auth'
import { Badge } from '@/components/ui/badge'

export default function Home() {
  const { t: overview } = useTranslation('overview')

  const cache = useQueryClient()
  const subscribe = useCallback(
    (changed: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (event.query.queryKey[0] === 'auth' && event.query.queryKey[1] === 'session') changed()
      }),
    [cache],
  )
  const version = useCallback(() => cache.getQueryState(sessionKey)?.dataUpdateCount ?? 0, [cache])
  const generation = useSyncExternalStore(subscribe, version, version)
  const current = useSession()
  const session = current.data
  const requestedIdentity = useRef<string | null>(null)
  const refetchSession = current.refetch
  const onRefreshIdentity = useCallback(() => {
    requestedIdentity.current = null
  }, [])
  const onMismatch = useCallback(
    (role: 'admin' | 'member') => {
      const key = `${session?.user.id}:${role}`
      if (requestedIdentity.current === key) return
      requestedIdentity.current = key
      void refetchSession()
    },
    [refetchSession, session?.user.id],
  )
  useEffect(() => {
    if (
      !current.isFetching &&
      current.isSuccess &&
      requestedIdentity.current === `${session?.user.id}:${session?.user.role}`
    )
      requestedIdentity.current = null
  }, [current.isFetching, current.isSuccess, session?.user.id, session?.user.role])
  if (current.isFetching || current.isPending)
    return <p role="status">{overview('sessionLoading')}</p>
  if (current.isError || !session)
    return (
      <div className="space-y-3">
        <p role="alert">{overview('sessionError')}</p>
        <Button variant="outline" onClick={() => void current.refetch()}>
          {overview('retrySession')}
        </Button>
      </div>
    )
  return (
    <HomeWorkspace
      key={`${session.user.id}:${generation}`}
      session={session}
      generation={generation}
      onMismatch={onMismatch}
      onRefreshIdentity={onRefreshIdentity}
    />
  )
}

function HomeWorkspace({
  session,
  generation,
  onMismatch,
  onRefreshIdentity,
}: {
  session: Session
  generation: number
  onMismatch: (role: 'admin' | 'member') => void
  onRefreshIdentity: () => void
}) {
  const [teamCursors, setTeamCursors] = useState<(string | null)[]>([null])
  const [roleCursors, setRoleCursors] = useState<(string | null)[]>([null])
  const teamCursor = teamCursors.at(-1)!
  const roleCursor = roleCursors.at(-1)!
  const accounts = useQuery({
    queryKey: overviewAccountsKey(session.user.id, String(generation), teamCursor),
    queryFn: ({ signal }) => getOverviewAccounts(session.user.id, teamCursor, signal),
    retry: false,
    staleTime: 0,
    refetchOnMount: 'always',
  })
  const roles = useQuery({
    queryKey: overviewRolesKey(session.user.id, String(generation), roleCursor),
    queryFn: ({ signal }) => getOverviewRoles(session.user.id, roleCursor, signal),
    retry: false,
    staleTime: 0,
    refetchOnMount: 'always',
  })
  const refresh = () => {
    onRefreshIdentity()
    if (teamCursor === null) void accounts.refetch()
    if (roleCursor === null) void roles.refetch()
    setTeamCursors([null])
    setRoleCursors([null])
  }
  return (
    <section className="space-y-6">
      <h1 className="sr-only">
        {t('common:hello_ce0e9')}
        {session.user.name}
      </h1>
      <section
        aria-label={t('common:value_member_information_17015', { v0: session.user.name })}
        className="flex flex-wrap items-center gap-4 rounded-lg border p-3"
      >
        <span className="flex size-12 items-center justify-center rounded-full bg-muted">
          {session.user.name.slice(0, 2).toUpperCase()}
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="text-[30px] leading-[38px] font-semibold">{session.user.name}</h2>
          <p className="text-sm leading-6 text-muted-foreground">{session.user.email}</p>
          <p className="text-sm leading-6 text-muted-foreground">
            {t('common:role_908a9')}
            {session.user.role === 'admin'
              ? t('common:administrator_ef84e')
              : t('common:memberRole')}
          </p>
          <IdentityLabels
            roles={roles}
            accounts={accounts}
            rolePage={roleCursors.length}
            teamPage={teamCursors.length}
            identityRole={session.user.role}
            onMismatch={onMismatch}
            previousRoles={() => setRoleCursors((values) => values.slice(0, -1))}
            nextRoles={() => {
              if (roles.data?.next_cursor)
                setRoleCursors((values) => [...values, roles.data.next_cursor])
            }}
            previousTeams={() => setTeamCursors((values) => values.slice(0, -1))}
            nextTeams={() => {
              if (accounts.data?.next_cursor)
                setTeamCursors((values) => [...values, accounts.data.next_cursor])
            }}
            refresh={refresh}
          />
        </div>
        <Badge variant="outline">{t('common:active_f78d0')}</Badge>
      </section>
      <MonthlyAccountsContent query={accounts} cursors={teamCursors} setCursors={setTeamCursors} />
      <UsageOverview
        key={`usage:${session.user.id}:${generation}`}
        actorId={session.user.id}
        generation={generation}
      />
    </section>
  )
}
