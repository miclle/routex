import { useLayoutEffect, useRef, useState } from 'react'
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Bell, CheckCheck, CircleAlert, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  getNotifications,
  recordedMonthlyQuota,
  recordedMonthlyQuotaWarning,
  markAllNotificationsRead,
  markNotificationRead,
  notificationsKey,
} from '@/api/notifications'
import { Button } from '@/components/ui/button'
import { Menu, MenuItem } from '@/components/ui/menu'
import { useSession } from '@/hooks/use-auth'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import type { Notification, NotificationReadStatus } from '@/types/notifications'

type ReadIntent = { recipientId: string; generation: number; csrf: string; id: string }
type ReadAllIntent = { recipientId: string; generation: number; csrf: string }

function itemText(
  notification: Notification,
  recipientId: string,
  t: ReturnType<typeof useTranslation>['t'],
) {
  if (notification.kind === 'monthly_quota_warning') {
    const warning = recordedMonthlyQuotaWarning(notification, recipientId)
    if (!warning) return t('items.monthly_quota_warning.default')
    const key =
      warning.scope_kind === 'team'
        ? 'team_monthly_quota_warning'
        : warning.scope_kind === 'project'
          ? 'project_monthly_quota_warning'
          : 'monthly_quota_warning'
    return t(`items.${key}.${notification.detail_code}`, {
      defaultValue: t(`items.${key}.default`),
    })
  }
  return t(`items.${notification.kind}.${notification.detail_code}`, {
    defaultValue: t(`items.${notification.kind}.default`, {
      defaultValue: t('unknownItem'),
    }),
  })
}

function deliveryText(notification: Notification, t: ReturnType<typeof useTranslation>['t']) {
  if (notification.kind === 'monthly_quota_warning') return null
  return notification.delivery_status ? t(`delivery.${notification.delivery_status}`) : null
}

function subjectText(notification: Notification, t: ReturnType<typeof useTranslation>['t']) {
  if (notification.kind === 'monthly_quota_warning') return null
  if (notification.subject_type !== 'provider' && notification.subject_type !== 'model') return null
  const value = notification.subject_name?.trim() || notification.subject_id?.trim()
  if (!value) return null
  return t(`subject.${notification.subject_type}`, { name: value })
}

function QuotaSnapshot({
  notification,
  recipientId,
}: {
  notification: Notification
  recipientId: string
}) {
  const { t, i18n } = useTranslation('notifications')
  const warning = recordedMonthlyQuotaWarning(notification, recipientId)
  const quota = warning ?? recordedMonthlyQuota(notification, recipientId)
  if (!quota) return <span className="mt-1 block text-xs">{t('quota.snapshotUnavailable')}</span>
  const format = (value: string) => {
    try {
      return new Intl.DateTimeFormat(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US', {
        dateStyle: 'medium',
        timeStyle: 'long',
        timeZone: quota.time_zone,
      }).format(new Date(value))
    } catch {
      return value
    }
  }
  const amount = (value: string) =>
    quota.dimension === 'tokens'
      ? t('quota.tokensValue', { amount: value })
      : t('quota.moneyValue', { amount: value, currency: quota.currency })
  const scopeName =
    notification.subject_type === quota.scope_kind && notification.subject_id === quota.scope_id
      ? notification.subject_name?.trim()
      : undefined
  return (
    <span className="mt-1 block space-y-1 text-xs [overflow-wrap:anywhere]">
      <span className="block">
        {t(
          quota.scope_kind === 'user'
            ? 'quota.personalScope'
            : quota.scope_kind === 'team_member'
              ? scopeName
                ? 'quota.teamMemberScopeNamed'
                : 'quota.teamMemberScope'
              : quota.scope_kind === 'team'
                ? scopeName
                  ? 'quota.teamScopeNamed'
                  : 'quota.teamScope'
                : scopeName
                  ? 'quota.projectScopeNamed'
                  : 'quota.projectScope',
          {
            id: quota.scope_kind === 'team_member' ? quota.team_id : quota.scope_id,
            name: scopeName,
          },
        )}
      </span>
      {warning && (
        <span className="block">
          {t('quota.warningThreshold', { threshold: warning.threshold })}
        </span>
      )}
      <span className="block">{t('quota.settled', { value: amount(quota.settled) })}</span>
      <span className="block">{t('quota.limit', { value: amount(quota.limit) })}</span>
      <span className="block">
        {t('quota.window', { start: format(quota.month_start), end: format(quota.month_end) })}
      </span>
      <span className="block">{t('quota.timeZone', { zone: quota.time_zone })}</span>
      <span className="block">{t('quota.asOf', { time: format(quota.as_of) })}</span>
      <span className="block">
        {t('quota.policyRevision', { revision: quota.policy_revision })}
      </span>
      <span className="block text-muted-foreground">
        {t(warning ? 'quota.recordedWarning' : 'quota.recordedSnapshot')}
      </span>
    </span>
  )
}

export function NotificationMenu() {
  const { t, i18n } = useTranslation('notifications')
  const session = useSession()
  const generation = useSessionGeneration()
  const previousGeneration = useRef(generation)
  const recipientId = session.isError ? '' : (session.data?.user.id ?? '')
  const csrf = session.data?.csrf_token ?? ''
  const freshSession =
    !!recipientId && !session.isPending && !session.isError && !session.isFetching
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<NotificationReadStatus>('unread')
  const query = useInfiniteQuery({
    queryKey: notificationsKey(recipientId, status),
    queryFn: async ({ pageParam, signal }) => {
      const capturedGeneration = previousGeneration.current
      return {
        ...(await getNotifications(status, pageParam, signal, recipientId)),
        generation: capturedGeneration,
      }
    },
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: freshSession,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchInterval: 30_000,
  })
  useLayoutEffect(() => {
    const renewed = previousGeneration.current !== generation
    previousGeneration.current = generation
    if (renewed && recipientId)
      void queryClient.resetQueries({ queryKey: ['notifications', recipientId] })
    else if (!freshSession && recipientId)
      void queryClient.cancelQueries({ queryKey: ['notifications', recipientId] })
  }, [freshSession, generation, recipientId, queryClient])
  const refresh = (capturedRecipient: string) =>
    queryClient.invalidateQueries({ queryKey: ['notifications', capturedRecipient] })
  const readMutation = useMutation({
    mutationFn: (intent: ReadIntent) => markNotificationRead(intent.id, intent.csrf),
    onSettled: (_result, _error, intent) => {
      if (intent.generation === previousGeneration.current) return refresh(intent.recipientId)
    },
  })
  const allMutation = useMutation({
    mutationFn: (intent: ReadAllIntent) => markAllNotificationsRead(intent.csrf),
    onSettled: (_result, _error, intent) => {
      if (intent.generation === previousGeneration.current) return refresh(intent.recipientId)
    },
  })
  const canRead = freshSession
  const current =
    canRead &&
    query.isSuccess &&
    !query.isFetching &&
    !query.isError &&
    query.data.pages.every((page) => page.generation === generation)
  const pages = current ? query.data.pages : []
  const notifications = pages.flatMap((page) => page.items)
  const count = pages[0]?.unread_count ?? 0
  const mutationFailed =
    (readMutation.isError &&
      readMutation.variables?.recipientId === recipientId &&
      readMutation.variables.generation === generation) ||
    (allMutation.isError &&
      allMutation.variables?.recipientId === recipientId &&
      allMutation.variables.generation === generation)
  const readPending =
    readMutation.isPending &&
    readMutation.variables?.recipientId === recipientId &&
    readMutation.variables.generation === generation
  const allPending =
    allMutation.isPending &&
    allMutation.variables?.recipientId === recipientId &&
    allMutation.variables.generation === generation
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'

  return (
    <Menu
      label={t('menu')}
      side="bottom"
      align="end"
      triggerClassName="relative size-10 justify-center px-0"
      popupClassName="w-[min(400px,calc(100vw-24px))] overflow-hidden"
      trigger={
        <>
          <Bell className="size-4" aria-hidden />
          {count > 0 && (
            <span
              className="absolute top-1 right-1 flex min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[10px] leading-4 text-destructive-foreground"
              aria-label={t('unreadCount', { count })}
            >
              {count > 99 ? '99+' : count}
            </span>
          )}
        </>
      }
    >
      <div className="flex items-center justify-between border-b px-3 py-2">
        <div>
          <p className="font-medium">{t('title')}</p>
          <p className="text-xs text-muted-foreground">{t('recipientScoped')}</p>
        </div>
        {count > 0 && (
          <Button
            variant="ghost"
            size="sm"
            disabled={allPending || readPending || !csrf}
            onClick={() => {
              if (current && csrf) allMutation.mutate({ recipientId, generation, csrf })
            }}
          >
            <CheckCheck className="size-4" aria-hidden />
            {t('markAllRead')}
          </Button>
        )}
      </div>
      {canRead && (
        <div className="flex gap-1 border-b px-3 py-2" role="group" aria-label={t('historyFilter')}>
          {(['unread', 'all'] as const).map((value) => (
            <Button
              key={value}
              size="sm"
              variant={status === value ? 'default' : 'ghost'}
              aria-pressed={status === value}
              onClick={() => setStatus(value)}
            >
              {t(`filters.${value}`)}
            </Button>
          ))}
        </div>
      )}
      <div className="max-h-[min(480px,65vh)] overflow-y-auto">
        {(session.isPending || session.isFetching) && (
          <p role="status" className="px-3 py-6 text-center text-sm text-muted-foreground">
            {t('loading')}
          </p>
        )}
        {!session.isPending && !session.isFetching && !canRead && (
          <p className="px-3 py-8 text-center text-sm text-muted-foreground">
            {t('sessionUnavailable')}
          </p>
        )}
        {canRead && query.isFetching && (
          <p role="status" className="px-3 py-6 text-center text-sm text-muted-foreground">
            {t('loading')}
          </p>
        )}
        {canRead && query.isError && (
          <div className="space-y-3 px-3 py-4">
            <p role="alert" className="flex items-center gap-2 text-sm text-destructive">
              <CircleAlert className="size-4" aria-hidden />
              {t(query.isFetchNextPageError ? 'loadMoreFailed' : 'loadFailed')}
            </p>
            <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
              <RefreshCw className="size-4" aria-hidden />
              {t('retry')}
            </Button>
          </div>
        )}
        {mutationFailed ? (
          <p role="alert" className="px-3 py-2 text-xs text-destructive">
            {t('markReadFailed')}
          </p>
        ) : null}
        {current && notifications.length === 0 && (
          <p className="px-3 py-8 text-center text-sm text-muted-foreground">
            {t(status === 'all' ? 'emptyHistory' : 'empty')}
          </p>
        )}
        {current &&
          notifications.map((notification) => {
            const subject = subjectText(notification, t)
            return (
              <MenuItem
                key={notification.id}
                disabled={readPending || allPending || !csrf}
                onClick={() => {
                  if (current && csrf && !notification.read)
                    readMutation.mutate({ recipientId, generation, csrf, id: notification.id })
                }}
              >
                <span className="min-w-0 flex-1 py-1">
                  <span className="flex items-start justify-between gap-3">
                    <span className="font-medium">{itemText(notification, recipientId, t)}</span>
                    <span
                      className={`mt-1 size-2 shrink-0 rounded-full ${
                        notification.severity === 'high'
                          ? 'bg-destructive'
                          : notification.severity === 'medium'
                            ? 'bg-amber-500'
                            : 'bg-muted-foreground'
                      }`}
                      aria-label={t(`severity.${notification.severity}`)}
                    />
                  </span>
                  {subject && <span className="mt-1 block text-xs">{subject}</span>}
                  {(notification.kind === 'monthly_quota_exhausted' ||
                    notification.kind === 'monthly_quota_warning') && (
                    <QuotaSnapshot notification={notification} recipientId={recipientId} />
                  )}
                  <span className="mt-1 block text-xs text-muted-foreground">
                    {Number.isFinite(Date.parse(notification.last_seen_at))
                      ? new Intl.DateTimeFormat(locale, {
                          dateStyle: 'medium',
                          timeStyle: 'short',
                        }).format(new Date(notification.last_seen_at))
                      : t('quota.unknownTime')}
                    {deliveryText(notification, t) ? ` · ${deliveryText(notification, t)}` : ''}
                  </span>
                </span>
              </MenuItem>
            )
          })}
      </div>
      {current && query.hasNextPage && (
        <div className="border-t p-2">
          <Button
            variant="ghost"
            size="sm"
            className="w-full"
            disabled={query.isFetchingNextPage}
            onClick={() => void query.fetchNextPage()}
          >
            {query.isFetchingNextPage ? t('loadingMore') : t('loadMore')}
          </Button>
        </div>
      )}
    </Menu>
  )
}
