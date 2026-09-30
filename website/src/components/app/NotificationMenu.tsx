import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
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
import { usePermissions } from '@/hooks/use-permissions'
import type { Notification } from '@/types/notifications'

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

export function NotificationMenu() {
  const { t, i18n } = useTranslation('notifications')
  const session = useSession()
  const permissions = usePermissions()
  const recipientId = session.data?.user.id ?? ''
  const csrf = session.data?.csrf_token ?? ''
  const queryClient = useQueryClient()
  const key = notificationsKey(recipientId, 'unread')
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getNotifications('unread', signal),
    enabled: recipientId !== '' && permissions.can('system.read'),
    retry: false,
    refetchInterval: 30_000,
  })
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['notifications', recipientId] })
  const readMutation = useMutation({
    mutationFn: (id: string) => markNotificationRead(id, csrf),
    onSuccess: refresh,
  })
  const allMutation = useMutation({
    mutationFn: () => markAllNotificationsRead(csrf),
    onSuccess: refresh,
  })
  const count = query.data?.unread_count ?? 0
  const canRead = permissions.can('system.read')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'

  return (
    <Menu
      label={t('menu')}
      side="bottom"
      align="end"
      triggerClassName="relative size-10 justify-center px-0"
      popupClassName="w-[min(380px,calc(100vw-24px))]"
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
            disabled={allMutation.isPending}
            onClick={() => allMutation.mutate()}
          >
            <CheckCheck className="size-4" aria-hidden />
            {t('markAllRead')}
          </Button>
        )}
      </div>
      {permissions.isPending && (
        <p role="status" className="px-3 py-6 text-center text-sm text-muted-foreground">
          {t('loading')}
        </p>
      )}
      {!permissions.isPending && !canRead && (
        <p className="px-3 py-8 text-center text-sm text-muted-foreground">{t('empty')}</p>
      )}
      {canRead && query.isPending && (
        <p role="status" className="px-3 py-6 text-center text-sm text-muted-foreground">
          {t('loading')}
        </p>
      )}
      {canRead && query.isError && (
        <div className="space-y-3 px-3 py-4">
          <p role="alert" className="flex items-center gap-2 text-sm text-destructive">
            <CircleAlert className="size-4" aria-hidden />
            {t('loadFailed')}
          </p>
          <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
            <RefreshCw className="size-4" aria-hidden />
            {t('retry')}
          </Button>
        </div>
      )}
      {readMutation.isError || allMutation.isError ? (
        <p role="alert" className="px-3 py-2 text-xs text-destructive">
          {t('markReadFailed')}
        </p>
      ) : null}
      {canRead && query.data && query.data.items.length === 0 && (
        <p className="px-3 py-8 text-center text-sm text-muted-foreground">{t('empty')}</p>
      )}
      {canRead &&
        query.data?.items.map((notification) => (
          <MenuItem
            key={notification.id}
            disabled={readMutation.isPending}
            onClick={() => readMutation.mutate(notification.id)}
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
              <span className="mt-1 block text-xs text-muted-foreground">
                {new Intl.DateTimeFormat(locale, {
                  dateStyle: 'medium',
                  timeStyle: 'short',
                }).format(new Date(notification.last_seen_at))}
                {deliveryText(notification, t) ? ` · ${deliveryText(notification, t)}` : ''}
              </span>
            </span>
          </MenuItem>
        ))}
    </Menu>
  )
}
