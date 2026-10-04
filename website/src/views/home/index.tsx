import { useCallback, useSyncExternalStore } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import MonthlyAccounts from './monthly-accounts'
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
        </div>
        <Badge variant="outline">{t('common:active_f78d0')}</Badge>
      </section>
      <MonthlyAccounts key={`${session.user.id}:${generation}`} actorId={session.user.id} />
    </section>
  )
}
