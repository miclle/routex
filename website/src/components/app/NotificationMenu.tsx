import { useState } from 'react'
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Bell, CheckCheck, CircleAlert, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  getNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  notificationsKey,
} from '@/api/notifications'
import { Button } from '@/components/ui/button'
import { Menu, MenuItem } from '@/components/ui/menu'
import { useSession } from '@/hooks/use-auth'
import type {
  MonthlyQuotaNotificationSnapshot,
  Notification,
  NotificationReadStatus,
} from '@/types/notifications'

type ReadIntent = { recipientId: string; csrf: string; id: string }
type ReadAllIntent = { recipientId: string; csrf: string }

function itemText(notification: Notification, t: ReturnType<typeof useTranslation>['t']) {
  return t(`items.${notification.kind}.${notification.detail_code}`, {
    defaultValue: t(`items.${notification.kind}.default`, {
      defaultValue: t('unknownItem'),
    }),
  })
}

function deliveryText(notification: Notification, t: ReturnType<typeof useTranslation>['t']) {
  return notification.delivery_status ? t(`delivery.${notification.delivery_status}`) : null
}

function subjectText(notification: Notification, t: ReturnType<typeof useTranslation>['t']) {
  if (notification.subject_type !== 'provider' && notification.subject_type !== 'model') return null
  const value = notification.subject_name?.trim() || notification.subject_id?.trim()
  if (!value) return null
  return t(`subject.${notification.subject_type}`, { name: value })
}

function recordedQuota(notification: Notification): MonthlyQuotaNotificationSnapshot | undefined {
  const quota = notification.quota
  if (
    notification.kind !== 'monthly_quota_exhausted' ||
    !quota ||
    (quota.scope_kind !== 'user' && quota.scope_kind !== 'project') ||
    typeof quota.scope_id !== 'string' ||
    !quota.scope_id.trim() ||
    typeof quota.policy_revision !== 'string' ||
    !quota.policy_revision.trim() ||
    typeof quota.time_zone !== 'string' ||
    !quota.time_zone.trim() ||
    typeof quota.month_start !== 'string' ||
    typeof quota.month_end !== 'string' ||
    typeof quota.as_of !== 'string' ||
    !Number.isFinite(Date.parse(quota.month_start)) ||
    !Number.isFinite(Date.parse(quota.month_end)) ||
    !Number.isFinite(Date.parse(quota.as_of))
  )
    return undefined
  const start = Date.parse(quota.month_start)
  const end = Date.parse(quota.month_end)
  const asOf = Date.parse(quota.as_of)
  if (
    end <= start ||
    asOf < start ||
    asOf >= end ||
    (notification.subject_type != null && notification.subject_type !== quota.scope_kind) ||
    (notification.subject_id != null && notification.subject_id !== quota.scope_id)
  )
    return undefined
  const tokens = quota.dimension === 'tokens'
  const pattern = tokens ? /^\d+$/ : /^\d+(?:\.\d+)?$/
  if (
    typeof quota.limit !== 'string' ||
    typeof quota.settled !== 'string' ||
    !pattern.test(quota.limit) ||
    !pattern.test(quota.settled) ||
    (tokens
      ? notification.detail_code !== 'tokens_month_exhausted' || quota.currency !== null
      : quota.dimension !== 'money' ||
        notification.detail_code !== 'money_month_exhausted' ||
        typeof quota.currency !== 'string' ||
        !quota.currency.trim())
  )
    return undefined
  return quota
}

function QuotaSnapshot({ notification }: { notification: Notification }) {
  const { t, i18n } = useTranslation('notifications')
  const quota = recordedQuota(notification)
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
  const projectName =
    notification.subject_type === 'project' && notification.subject_id === quota.scope_id
      ? notification.subject_name?.trim()
      : undefined
  return (
    <span className="mt-1 block space-y-1 text-xs [overflow-wrap:anywhere]">
      <span className="block">
        {t(
          quota.scope_kind === 'user'
            ? 'quota.personalScope'
            : projectName
              ? 'quota.projectScopeNamed'
              : 'quota.projectScope',
          { id: quota.scope_id, name: projectName },
        )}
      </span>
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
      <span className="block text-muted-foreground">{t('quota.recordedSnapshot')}</span>
    </span>
  )
}

export function NotificationMenu() {
  const { t, i18n } = useTranslation('notifications')
  const session = useSession()
  const recipientId = session.isError ? '' : (session.data?.user.id ?? '')
  const csrf = session.data?.csrf_token ?? ''
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<NotificationReadStatus>('unread')
  const query = useInfiniteQuery({
    queryKey: notificationsKey(recipientId, status),
    queryFn: ({ pageParam, signal }) => getNotifications(status, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: recipientId !== '',
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchInterval: 30_000,
  })
  const refresh = (capturedRecipient: string) =>
    queryClient.invalidateQueries({ queryKey: ['notifications', capturedRecipient] })
  const readMutation = useMutation({
    mutationFn: (intent: ReadIntent) => markNotificationRead(intent.id, intent.csrf),
    onSettled: (_result, _error, intent) => refresh(intent.recipientId),
  })
  const allMutation = useMutation({
    mutationFn: (intent: ReadAllIntent) => markAllNotificationsRead(intent.csrf),
    onSettled: (_result, _error, intent) => refresh(intent.recipientId),
  })
  const canRead = recipientId !== ''
  const current = canRead && query.isSuccess && !query.isFetching && !query.isError
  const pages = current ? query.data.pages : []
  const notifications = pages.flatMap((page) => page.items)
  const count = pages[0]?.unread_count ?? 0
  const mutationFailed =
    (readMutation.isError && readMutation.variables?.recipientId === recipientId) ||
    (allMutation.isError && allMutation.variables?.recipientId === recipientId)
  const readPending = readMutation.isPending && readMutation.variables?.recipientId === recipientId
  const allPending = allMutation.isPending && allMutation.variables?.recipientId === recipientId
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
            disabled={allPending || readPending}
            onClick={() => allMutation.mutate({ recipientId, csrf })}
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
        {session.isPending && (
          <p role="status" className="px-3 py-6 text-center text-sm text-muted-foreground">
            {t('loading')}
          </p>
        )}
        {!session.isPending && !canRead && (
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
                disabled={readPending || allPending}
                onClick={() => {
                  if (!notification.read)
                    readMutation.mutate({ recipientId, csrf, id: notification.id })
                }}
              >
                <span className="min-w-0 flex-1 py-1">
                  <span className="flex items-start justify-between gap-3">
                    <span className="font-medium">{itemText(notification, t)}</span>
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
                  {notification.kind === 'monthly_quota_exhausted' && (
                    <QuotaSnapshot notification={notification} />
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
