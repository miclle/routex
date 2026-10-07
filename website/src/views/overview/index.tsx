import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  BellRing,
  CheckCircle2,
  CircleAlert,
  Gauge,
  Hash,
  RefreshCw,
  Settings,
  UsersRound,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  adminOverviewKey,
  getAdminOverview,
  OverviewError,
  updateOperationalAlert,
} from '@/api/overview'
import {
  getNotificationSettings,
  NotificationError,
  notificationSettingsKey,
  updateNotificationSettings,
} from '@/api/notifications'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Table } from '@/components/ui/table'
import { sessionKey, useSession } from '@/hooks/use-auth'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import { getPermissions } from '@/api/governance'
import type { NotificationSettings, UpdateNotificationSettingsInput } from '@/types/notifications'
import { usePermissions } from '@/hooks/use-permissions'
import type {
  AdminOverview,
  OperationalAlert,
  OperationalAlertState,
  ProviderQualityUnavailableReason,
  OverviewTrendPoint,
} from '@/types/overview'
import { chartRatio, exactNumber } from '@/views/usage/format'

function alertText(alert: OperationalAlert, t: ReturnType<typeof useTranslation>['t']) {
  return t(`items.${alert.kind}.${alert.detail_code}`, {
    defaultValue: t(`items.${alert.kind}.default`, { defaultValue: t('unknownItem') }),
  })
}

function alertCategory(alert: OperationalAlert, t: ReturnType<typeof useTranslation>['t']) {
  return t(`categories.${alert.kind}`, { defaultValue: t('categories.unknown') })
}

function alertSubject(alert: OperationalAlert, t: ReturnType<typeof useTranslation>['t']) {
  if (alert.subject_type !== 'provider' && alert.subject_type !== 'model')
    return t('overview.scopeUnknown')
  const value = alert.subject_name?.trim() || alert.subject_id?.trim()
  return value ? t(`subject.${alert.subject_type}`, { name: value }) : t('overview.scopeUnknown')
}

function formatNumber(value: number, locale: string) {
  return new Intl.NumberFormat(locale, { notation: 'compact', maximumFractionDigits: 1 }).format(
    value,
  )
}

function formatTime(value: string, locale: string) {
  return new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(value))
}

function MetricCard({
  label,
  value,
  help,
  icon: Icon,
}: {
  label: string
  value: string
  help: string
  icon: typeof Activity
}) {
  return (
    <Card>
      <CardContent className="space-y-2">
        <div className="flex items-center justify-between text-sm text-muted-foreground">
          <span>{label}</span>
          <Icon className="size-4" aria-hidden />
        </div>
        <p className="text-2xl font-semibold tabular-nums">{value}</p>
        <p className="text-xs text-muted-foreground">{help}</p>
      </CardContent>
    </Card>
  )
}

function TrendChart({ points }: { points: OverviewTrendPoint[] }) {
  const { t, i18n } = useTranslation('notifications')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const titleId = useId()
  const maximum = points.reduce((current, point) => {
    const value = BigInt(point.tokens.known)
    return value > current ? value : current
  }, 1n)
  const path = points
    .map((point, index) => {
      if (point.tokens.value === null) return null
      const x = points.length === 1 ? 400 : 36 + (index / (points.length - 1)) * 728
      const y = 204 - chartRatio(point.tokens.value, maximum.toString()) * 164
      return `${index === 0 || points[index - 1]?.tokens.value === null ? 'M' : 'L'} ${x} ${y}`
    })
    .filter(Boolean)
    .join(' ')
  return (
    <div className="space-y-3">
      <svg viewBox="0 0 800 232" className="w-full" role="img" aria-labelledby={titleId}>
        <title id={titleId}>{t('overview.trendLabel')}</title>
        {[40, 122, 204].map((y) => (
          <line
            key={y}
            x1="36"
            x2="764"
            y1={y}
            y2={y}
            className="stroke-border"
            strokeDasharray="3 4"
          />
        ))}
        <path d={path} fill="none" className="stroke-primary" strokeWidth="2.5" />
        {points.map((point, index) => {
          if (point.tokens.value === null) return null
          const x = points.length === 1 ? 400 : 36 + (index / (points.length - 1)) * 728
          const y = 204 - chartRatio(point.tokens.value, maximum.toString()) * 164
          return (
            <circle key={point.date} cx={x} cy={y} r="3" className="fill-primary">
              <title>
                {new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(
                  new Date(point.date),
                )}
                : {exactNumber(point.tokens.value, locale)}
              </title>
            </circle>
          )
        })}
        {points.length > 0 && (
          <>
            <text x="36" y="226" className="fill-muted-foreground text-[10px]">
              {new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(
                new Date(points[0].date),
              )}
            </text>
            <text x="764" y="226" textAnchor="end" className="fill-muted-foreground text-[10px]">
              {new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(
                new Date(points.at(-1)!.date),
              )}
            </text>
          </>
        )}
      </svg>
      {points.every((point) => point.tokens.value === null) && (
        <p className="text-sm text-muted-foreground">{t('overview.trendUnavailable')}</p>
      )}
    </div>
  )
}

function ProviderStatus({ overview }: { overview: AdminOverview }) {
  const { t, i18n } = useTranslation('notifications')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const readiness = overview.provider_readiness
  return (
    <Card className="h-full">
      <CardHeader>
        <div className="flex items-center justify-between gap-3">
          <CardTitle>{t('overview.providerStatus')}</CardTitle>
          <Badge variant="outline">
            {t('overview.providerSummary', {
              ready: readiness.ready_connections,
              total: readiness.connections,
            })}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {readiness.items.length === 0 && (
          <p className="text-sm text-muted-foreground">{t('overview.noProviders')}</p>
        )}
        {readiness.items.map((provider) => (
          <div key={provider.provider_id} className="flex items-center justify-between gap-4">
            <div className="min-w-0">
              <p className="truncate font-medium">{provider.name}</p>
              <p className="text-xs text-muted-foreground">
                {t('overview.providerMeta', {
                  models: provider.model_count,
                  credentials: provider.credential_count,
                  ready: provider.ready_connection_count,
                  connections: provider.connection_count,
                })}
              </p>
            </div>
            <div className="shrink-0 space-y-1 text-right">
              <Badge variant={provider.status === 'ready' ? 'success' : 'outline'}>
                {t(`overview.providerStates.${provider.status}`)}
              </Badge>
              {provider.quality && (
                <p className="text-xs text-muted-foreground">
                  {t(`overview.qualityStates.${provider.quality.status}`)} ·{' '}
                  {provider.quality.p95_duration_ms === null
                    ? t('overview.p95Unknown')
                    : t('overview.p95Value', {
                        value: new Intl.NumberFormat(locale).format(
                          provider.quality.p95_duration_ms,
                        ),
                      })}
                </p>
              )}
              {!provider.quality && provider.quality_unavailable_reason && (
                <p className="max-w-64 text-xs text-muted-foreground">
                  {t('overview.qualityUnavailable', {
                    reason: t(
                      `overview.qualityUnavailableReasons.${qualityUnavailableReasonKey(
                        provider.quality_unavailable_reason,
                      )}`,
                    ),
                  })}
                </p>
              )}
            </div>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function qualityUnavailableReasonKey(reason: ProviderQualityUnavailableReason) {
  switch (reason) {
    case 'query_budget':
      return 'query_budget'
    case 'range_too_large':
      return 'range_too_large'
    case 'invalid_policy':
      return 'invalid_policy'
  }
}

function TopModels({ overview }: { overview: AdminOverview }) {
  const { t, i18n } = useTranslation('notifications')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const maximum = overview.top_models.reduce((current, model) => {
    const value = BigInt(model.tokens.known)
    return value > current ? value : current
  }, 1n)
  return (
    <Card className="h-full">
      <CardHeader>
        <CardTitle>{t('overview.topModels')}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {overview.top_models.length === 0 && (
          <p className="text-sm text-muted-foreground">{t('overview.noTopModels')}</p>
        )}
        {overview.top_models.map((model) => (
          <div key={model.id} className="space-y-2">
            <div className="flex items-center justify-between gap-3 text-sm">
              <div className="min-w-0">
                <p className="truncate font-medium">{model.name}</p>
                <p className="text-xs text-muted-foreground">
                  {t('overview.topModelMeta', { count: model.calls })}
                </p>
              </div>
              <strong className="tabular-nums">
                {model.tokens.value === null
                  ? t('overview.knownTokens', { value: exactNumber(model.tokens.known, locale) })
                  : exactNumber(model.tokens.value, locale)}
              </strong>
            </div>
            <div className="h-1.5 overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-primary"
                style={{
                  width: `${chartRatio(model.tokens.known, maximum.toString()) * 100}%`,
                }}
              />
            </div>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function AlertDetails({
  alert,
  open,
  onOpenChange,
  canWrite,
}: {
  alert: OperationalAlert | null
  open: boolean
  onOpenChange: (open: boolean) => void
  canWrite: boolean
}) {
  const { t, i18n } = useTranslation('notifications')
  const session = useSession()
  const queryClient = useQueryClient()
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const [success, setSuccess] = useState(false)
  const mutation = useMutation({
    mutationFn: (state: Exclude<OperationalAlertState, 'open'>) =>
      updateOperationalAlert(alert!.id, { state, etag: alert!.etag }, session.data!.csrf_token),
    onSuccess: () => {
      setSuccess(true)
      void queryClient.invalidateQueries({ queryKey: adminOverviewKey })
      onOpenChange(false)
    },
  })
  if (!alert) return null
  const conflict =
    mutation.error instanceof OverviewError && [409, 412].includes(mutation.error.status)
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('overview.alertDetails')}
      description={t('overview.alertDetailsDescription')}
      busy={mutation.isPending}
      width={580}
    >
      <div className="space-y-5">
        <dl className="grid gap-3 rounded-lg border p-4 text-sm sm:grid-cols-2">
          <div className="sm:col-span-2">
            <dt className="text-muted-foreground">{t('overview.category')}</dt>
            <dd className="mt-1 font-medium">{alertCategory(alert, t)}</dd>
          </div>
          <div className="sm:col-span-2">
            <dt className="text-muted-foreground">{t('overview.affectedScope')}</dt>
            <dd className="mt-1 font-medium">{alertSubject(alert, t)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('overview.severity')}</dt>
            <dd className="mt-1">{t(`overview.severities.${alert.severity}`)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('overview.state')}</dt>
            <dd className="mt-1">{t(`overview.states.${alert.state}`)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('overview.firstSeen')}</dt>
            <dd className="mt-1">{formatTime(alert.first_seen_at, locale)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('overview.latestOccurrence')}</dt>
            <dd className="mt-1">{formatTime(alert.last_seen_at, locale)}</dd>
          </div>
          <div className="sm:col-span-2">
            <dt className="text-muted-foreground">{t('overview.detail')}</dt>
            <dd className="mt-1">{alertText(alert, t)}</dd>
          </div>
        </dl>
        {success && <p role="status">{t('overview.alertUpdated')}</p>}
        {mutation.isError && (
          <p role="alert" className="text-sm text-destructive">
            {t(conflict ? 'overview.alertConflict' : 'overview.alertUpdateFailed')}
          </p>
        )}
        <div className="flex flex-wrap justify-end gap-2">
          <Button
            variant="outline"
            disabled={mutation.isPending}
            onClick={() => onOpenChange(false)}
          >
            {t('overview.close')}
          </Button>
          {canWrite && alert.state === 'open' && (
            <Button disabled={mutation.isPending} onClick={() => mutation.mutate('handling')}>
              {mutation.isPending ? t('overview.updating') : t('overview.markHandling')}
            </Button>
          )}
          {canWrite && alert.state !== 'resolved' && (
            <Button disabled={mutation.isPending} onClick={() => mutation.mutate('resolved')}>
              <CheckCircle2 className="size-4" aria-hidden />
              {mutation.isPending ? t('overview.updating') : t('overview.resolve')}
            </Button>
          )}
        </div>
      </div>
    </Dialog>
  )
}

class NotificationSettingsNotDispatched extends Error {}

function NotificationSettingsDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation('notifications')
  const session = useSession()
  const generation = useSessionGeneration()
  const opening = useId()
  const recipientId = session.data?.user.id ?? ''
  const sharedPermissions = usePermissions()
  const key = [...notificationSettingsKey(recipientId), opening, generation] as const
  const permissionsKey = ['permissions', recipientId, 'notification-settings', opening, generation]
  const queryClient = useQueryClient()
  const [confirmed] = useState(() => new WeakSet<object>())
  const freshSession =
    !!recipientId && !session.isPending && !session.isError && !session.isFetching
  const permissions = useQuery({
    queryKey: permissionsKey,
    queryFn: async ({ signal }) => {
      const value = await getPermissions(signal)
      if (!Array.isArray(value) || !value.every((item) => typeof item === 'string'))
        throw new Error('Invalid notification settings authority')
      if (!signal.aborted) confirmed.add(value)
      return value
    },
    enabled: open && freshSession,
    retry: false,
    gcTime: 0,
    structuralSharing: false,
    refetchOnMount: 'always',
  })
  const authority =
    freshSession &&
    permissions.isSuccess &&
    !permissions.isFetching &&
    confirmed.has(permissions.data) &&
    sharedPermissions.isSuccess &&
    !sharedPermissions.isFetching &&
    !sharedPermissions.isError
  const canRead =
    authority && permissions.data.includes('system.read') && sharedPermissions.can('system.read')
  const canWrite =
    canRead && permissions.data.includes('system.write') && sharedPermissions.can('system.write')
  const query = useQuery({
    queryKey: key,
    queryFn: async ({ signal }) => {
      const value = await getNotificationSettings(signal)
      if (!signal.aborted) confirmed.add(value)
      return value
    },
    enabled: open && freshSession,
    retry: false,
    gcTime: 0,
    structuralSharing: false,
    refetchOnMount: 'always',
  })
  const freshSettings = query.isSuccess && !query.isFetching && confirmed.has(query.data)
  const ready = open && canRead && freshSettings
  const [email, setEmail] = useState('')
  const [emailHigh, setEmailHigh] = useState(false)
  const [emailMedium, setEmailMedium] = useState(false)
  const [seed, setSeed] = useState<{ actor: string; etag: string } | null>(null)
  const [conflict, setConflict] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const [conflictReviewed, setConflictReviewed] = useState(false)
  const [rejectedEtag, setRejectedEtag] = useState('')
  const [rejectedValue, setRejectedValue] = useState<NotificationSettings | undefined>()
  type SaveIntent = {
    body: UpdateNotificationSettingsInput
    actor: string
    generation: number
    opening: string
    actorLifetime: number
  }
  const [submitted, setSubmitted] = useState<SaveIntent | null>(null)
  const current = useRef({
    recipientId,
    generation,
    opening,
    ready,
    canWrite,
    key,
    permissionsKey,
    actorLifetime: 0,
  })
  const mounted = useRef(true)
  useLayoutEffect(() => {
    // The same ID after A→B→A is a new actor lifetime, independent of Session renewal.
    const actorLifetime =
      current.current.actorLifetime + Number(current.current.recipientId !== recipientId)
    current.current = {
      recipientId,
      generation,
      opening,
      ready,
      canWrite,
      key,
      permissionsKey,
      actorLifetime,
    }
  })
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  useEffect(() => {
    if (ready && (!seed || seed.actor !== recipientId)) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- seed only from this opening's confirmed fresh response after authority renewal
      setEmail(query.data.external_email)
      setEmailHigh(query.data.email_high)
      setEmailMedium(query.data.email_medium)
      setSeed({ actor: recipientId, etag: query.data.etag })
      setConflict(false)
      setUncertain(false)
      setConflictReviewed(false)
      setSubmitted(null)
    }
  }, [ready, seed, recipientId, query.data])
  const sameOpening = (intent: SaveIntent) =>
    mounted.current &&
    current.current.recipientId === intent.actor &&
    current.current.opening === intent.opening &&
    current.current.actorLifetime === intent.actorLifetime
  const liveAuthority = () => {
    const sessionState = queryClient.getQueryState<NonNullable<typeof session.data>>(sessionKey)
    const ownPermissions = queryClient.getQueryState<string[]>(current.current.permissionsKey)
    const shared = queryClient.getQueryState<string[]>(['permissions', current.current.recipientId])
    const settingsState = queryClient.getQueryState(current.current.key)
    return (
      current.current.ready &&
      current.current.canWrite &&
      sessionState?.status === 'success' &&
      sessionState.fetchStatus === 'idle' &&
      sessionState.dataUpdateCount === current.current.generation &&
      sessionState.data?.user.id === current.current.recipientId &&
      ownPermissions?.status === 'success' &&
      ownPermissions.fetchStatus === 'idle' &&
      shared?.status === 'success' &&
      shared.fetchStatus === 'idle' &&
      ownPermissions.data?.includes('system.read') &&
      ownPermissions.data.includes('system.write') &&
      shared.data?.includes('system.read') &&
      shared.data.includes('system.write') &&
      settingsState?.status === 'success' &&
      settingsState.fetchStatus === 'idle'
    )
  }
  const mutation = useMutation({
    mutationFn: (intent: SaveIntent) => {
      if (
        !sameOpening(intent) ||
        intent.generation !== current.current.generation ||
        !liveAuthority()
      )
        throw new NotificationSettingsNotDispatched('Notification settings was not dispatched')
      // CSRF stays in the live Session cache, never in mutation variables or retained intent.
      const liveSession = queryClient.getQueryData<typeof session.data>(sessionKey)
      if (liveSession?.user.id !== intent.actor)
        throw new NotificationSettingsNotDispatched('Notification settings was not dispatched')
      return updateNotificationSettings(intent.body, liveSession.csrf_token)
    },
    onSuccess: (value, intent) => {
      if (!sameOpening(intent)) return
      if (intent.generation !== current.current.generation || !liveAuthority()) {
        // Renewed authority cannot certify an old request; review a new current read.
        setUncertain(true)
        setConflictReviewed(false)
        setRejectedValue(queryClient.getQueryData<NotificationSettings>(current.current.key))
        void queryClient.invalidateQueries({ queryKey: current.current.key, exact: true })
        return
      }
      queryClient.setQueryData(current.current.key, value)
      onOpenChange(false)
    },
    onError: (error, intent) => {
      if (!sameOpening(intent)) return
      if (
        error instanceof NotificationSettingsNotDispatched ||
        (error instanceof NotificationError && [400, 401, 403].includes(error.status))
      ) {
        setSubmitted(null)
        return
      }
      const changed = error instanceof NotificationError && [409, 412].includes(error.status)
      setConflict(changed)
      setUncertain(!changed)
      setConflictReviewed(false)
      setRejectedEtag(intent.body.etag)
      setRejectedValue(queryClient.getQueryData<NotificationSettings>(current.current.key))
      void queryClient.invalidateQueries({ queryKey: current.current.key, exact: true })
    },
  })
  const resetMutation = mutation.reset
  useLayoutEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- actor changes destroy this actor's private draft even if the next read is held
    setSeed(null)
    setSubmitted(null)
    setConflict(false)
    setUncertain(false)
    setConflictReviewed(false)
    // Detach the old observer; its request remains recorded and is never canceled or replayed.
    resetMutation()
  }, [recipientId, resetMutation])
  const needsReview = conflict || uncertain || (!!submitted && !mutation.isPending)
  const emailRequired = emailHigh || emailMedium
  const validEmail = !emailRequired || /^\S+@\S+\.\S+$/.test(email.trim())
  const conflictReady =
    ready &&
    canWrite &&
    needsReview &&
    query.data !== rejectedValue &&
    (!conflict || query.data.etag !== rejectedEtag)
  const canSave =
    ready &&
    canWrite &&
    seed?.actor === recipientId &&
    validEmail &&
    !submitted &&
    (!needsReview || (conflictReady && conflictReviewed))
  const draftVisible = ready && seed?.actor === recipientId
  const submit = () => {
    if (!canSave || !liveAuthority() || mutation.isPending) return
    const intent: SaveIntent = {
      body: {
        external_email: email.trim(),
        email_high: emailHigh,
        email_medium: emailMedium,
        etag: seed!.etag,
      },
      actor: recipientId,
      generation,
      opening,
      actorLifetime: current.current.actorLifetime,
    }
    setSubmitted(intent)
    mutation.mutate(intent)
  }
  const review = (useLatest: boolean) => {
    if (!conflictReady) return
    if (useLatest) {
      setEmail(query.data.external_email)
      setEmailHigh(query.data.email_high)
      setEmailMedium(query.data.email_medium)
    }
    setSeed({ actor: recipientId, etag: query.data.etag })
    setConflictReviewed(true)
    setSubmitted(null)
  }
  const close = () => onOpenChange(false)
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => (value ? onOpenChange(true) : close())}
      title={t('overview.settingsTitle')}
      description={t('overview.settingsDescription')}
      busy={mutation.isPending}
    >
      {(!canRead || query.isPending || query.isFetching) && (
        <p role="status">{t('overview.settingsLoading')}</p>
      )}
      {(query.isError || permissions.isError) && !conflict && (
        <div className="space-y-3">
          <p role="alert" className="text-sm text-destructive">
            {t('overview.settingsLoadFailed')}
          </p>
          <Button
            variant="outline"
            onClick={() => {
              void permissions.refetch()
              void query.refetch()
            }}
          >
            {t('overview.retry')}
          </Button>
        </div>
      )}
      {seed?.actor === recipientId && (
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            submit()
          }}
        >
          {draftVisible && (
            <>
              <div className="rounded-lg border border-primary/20 bg-primary/5 p-4">
                <p className="flex items-center gap-2 font-medium">
                  <BellRing className="size-4" aria-hidden />
                  {t('overview.inAppAlwaysOn')}
                </p>
                <p className="mt-1 text-sm text-muted-foreground">{t('overview.inAppHelp')}</p>
              </div>
              <label className="block space-y-2 text-sm font-medium">
                <span>{t('overview.externalEmail')}</span>
                <Input
                  type="email"
                  autoComplete="email"
                  value={email}
                  aria-invalid={!validEmail}
                  required={emailRequired}
                  disabled={mutation.isPending || !!submitted}
                  onChange={(event) => setEmail(event.target.value)}
                />
                <span className="block text-xs font-normal text-muted-foreground">
                  {t('overview.externalEmailHelp')}
                </span>
              </label>
              {!validEmail && (
                <p role="alert" className="text-sm text-destructive">
                  {t('overview.invalidEmail')}
                </p>
              )}
              <div className="space-y-3 rounded-lg border p-4">
                <label className="flex items-start justify-between gap-4 text-sm">
                  <span>
                    <span className="block font-medium">{t('overview.emailHigh')}</span>
                    <span className="mt-1 block text-muted-foreground">
                      {t('overview.emailHighHelp')}
                    </span>
                  </span>
                  <Switch
                    disabled={mutation.isPending || !!submitted}
                    checked={emailHigh}
                    onCheckedChange={setEmailHigh}
                    aria-label={t('overview.emailHigh')}
                  />
                </label>
                <label className="flex items-start justify-between gap-4 border-t pt-3 text-sm">
                  <span>
                    <span className="block font-medium">{t('overview.emailMedium')}</span>
                    <span className="mt-1 block text-muted-foreground">
                      {t('overview.emailMediumHelp')}
                    </span>
                  </span>
                  <Switch
                    disabled={mutation.isPending || !!submitted}
                    checked={emailMedium}
                    onCheckedChange={setEmailMedium}
                    aria-label={t('overview.emailMedium')}
                  />
                </label>
              </div>
            </>
          )}
          {needsReview && (
            <div className="space-y-3 rounded-lg border border-amber-300 bg-amber-50 p-4 text-sm text-amber-950">
              <p className="font-medium">
                {t(conflict ? 'overview.conflictTitle' : 'overview.uncertainTitle')}
              </p>
              <p>
                {t(conflict ? 'overview.conflictDescription' : 'overview.uncertainDescription')}
              </p>
              {conflictReady ? (
                <p>{t('overview.latestEmail', { email: query.data.external_email || '—' })}</p>
              ) : query.isFetching ? (
                <p role="status">{t('overview.reviewingLatest')}</p>
              ) : query.isError ? (
                <div className="space-y-2">
                  <p role="alert">{t('overview.conflictLoadFailed')}</p>
                  <Button type="button" variant="outline" onClick={() => void query.refetch()}>
                    {t('overview.retry')}
                  </Button>
                </div>
              ) : (
                <p role="status">{t('overview.reviewingLatest')}</p>
              )}
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  variant="outline"
                  disabled={!conflictReady}
                  onClick={() => review(true)}
                >
                  {t('overview.useLatest')}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={!conflictReady}
                  onClick={() => review(false)}
                >
                  {t('overview.keepDraft')}
                </Button>
              </div>
              {!conflictReviewed && <p>{t('overview.reviewRequired')}</p>}
            </div>
          )}
          {mutation.isError && !conflict && (
            <p role="alert" className="text-sm text-destructive">
              {t(
                mutation.error instanceof NotificationSettingsNotDispatched
                  ? 'overview.notDispatched'
                  : 'overview.settingsSaveFailed',
              )}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" disabled={mutation.isPending} onClick={close}>
              {t('overview.cancel')}
            </Button>
            <Button type="submit" disabled={!canSave || mutation.isPending}>
              {mutation.isPending ? t('overview.saving') : t('overview.saveSettings')}
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  )
}

function AlertsCard({ overview, canWrite }: { overview: AdminOverview; canWrite: boolean }) {
  const { t, i18n } = useTranslation('notifications')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const [selected, setSelected] = useState<OperationalAlert | null>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)
  return (
    <Card className="h-full">
      <CardHeader className="gap-3 sm:flex-row sm:items-center sm:justify-between">
        <CardTitle>{t('overview.operationalAlerts')}</CardTitle>
        {canWrite && (
          <Button variant="outline" size="sm" onClick={() => setSettingsOpen(true)}>
            <Settings className="size-4" aria-hidden />
            {t('overview.notificationSettings')}
          </Button>
        )}
      </CardHeader>
      <CardContent className="p-0">
        <Table aria-label={t('overview.alertTable')}>
          <thead>
            <tr>
              <th>{t('overview.alert')}</th>
              <th>{t('overview.severity')}</th>
              <th>{t('overview.state')}</th>
              <th>{t('overview.actions')}</th>
            </tr>
          </thead>
          <tbody>
            {overview.alerts.map((alert) => (
              <tr key={alert.id}>
                <td>
                  <p className="font-medium">{alertText(alert, t)}</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {t('overview.occurrences', { count: alert.occurrence_count })} ·{' '}
                    {t('overview.lastSeen', { time: formatTime(alert.last_seen_at, locale) })}
                  </p>
                </td>
                <td>
                  <Badge variant={alert.severity === 'high' ? 'default' : 'outline'}>
                    {t(`overview.severities.${alert.severity}`)}
                  </Badge>
                </td>
                <td>{t(`overview.states.${alert.state}`)}</td>
                <td>
                  <Button variant="outline" size="sm" onClick={() => setSelected(alert)}>
                    {t('overview.openAlert')}
                  </Button>
                </td>
              </tr>
            ))}
            {overview.alerts.length === 0 && (
              <tr>
                <td colSpan={4} className="text-center text-muted-foreground">
                  {t('overview.noAlerts')}
                </td>
              </tr>
            )}
          </tbody>
        </Table>
      </CardContent>
      <AlertDetails
        alert={selected}
        open={selected !== null}
        onOpenChange={(open) => !open && setSelected(null)}
        canWrite={canWrite}
      />
      {canWrite && settingsOpen && (
        <NotificationSettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} />
      )}
    </Card>
  )
}

function OverviewWorkspace() {
  const { t, i18n } = useTranslation('notifications')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const query = useQuery({
    queryKey: adminOverviewKey,
    queryFn: ({ signal }) => getAdminOverview(signal),
    retry: false,
  })
  const permissions = usePermissions()
  const overview = query.data
  const totalTrend = useMemo(
    () =>
      overview?.token_trend
        .reduce((sum, point) => sum + BigInt(point.tokens.known), 0n)
        .toString() ?? '0',
    [overview],
  )
  return (
    <Page
      title={t('overview.title')}
      description={t('overview.description')}
      action={
        <Button variant="outline" disabled={query.isFetching} onClick={() => void query.refetch()}>
          <RefreshCw className="size-4" aria-hidden />
          {t('overview.refresh')}
        </Button>
      }
    >
      {query.isPending && <QueryState pending error={null} retry={() => void query.refetch()} />}
      {query.isError && (
        <div className="space-y-3 rounded-lg border p-5">
          <p role="alert" className="flex items-center gap-2 text-sm text-destructive">
            <CircleAlert className="size-4" aria-hidden />
            {t('overview.loadFailed')}
          </p>
          <Button variant="outline" onClick={() => void query.refetch()}>
            {t('overview.retry')}
          </Button>
        </div>
      )}
      {overview && (
        <>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <MetricCard
              label={t('overview.callsToday')}
              value={formatNumber(overview.today.calls, locale)}
              help={t('overview.acrossReadyRoutes')}
              icon={Activity}
            />
            <MetricCard
              label={t('overview.tokensToday')}
              value={
                overview.today.tokens.value === null
                  ? t('overview.unknown')
                  : exactNumber(overview.today.tokens.value, locale)
              }
              help={t('overview.unknownTokenCalls', {
                count: overview.today.tokens.unknown_calls,
              })}
              icon={Hash}
            />
            <MetricCard
              label={t('overview.activePrincipals')}
              value={formatNumber(overview.today.active_principals, locale)}
              help={t('overview.activePrincipalsHelp')}
              icon={UsersRound}
            />
            <MetricCard
              label={t('overview.successRate')}
              value={
                overview.today.success_rate === null
                  ? t('overview.unknown')
                  : new Intl.NumberFormat(locale, {
                      style: 'percent',
                      maximumFractionDigits: 2,
                    }).format(overview.today.success_rate)
              }
              help={t('overview.acrossReadyRoutes')}
              icon={Gauge}
            />
          </div>
          <div className="grid gap-4 xl:grid-cols-24">
            <Card className="xl:col-span-14">
              <CardHeader className="gap-2 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <CardTitle>{t('overview.platformTrend')}</CardTitle>
                  <p className="mt-2 text-xs text-muted-foreground">
                    {t('overview.observedAt', { time: formatTime(overview.observed_at, locale) })}
                  </p>
                </div>
                <div className="text-right">
                  <p className="text-xl font-semibold tabular-nums">
                    {exactNumber(totalTrend, locale)}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {t('overview.knownTrendTotal')} · {t('overview.fourteenDays')}
                  </p>
                </div>
              </CardHeader>
              <CardContent>
                <TrendChart points={overview.token_trend} />
              </CardContent>
            </Card>
            <div className="xl:col-span-10">
              <ProviderStatus overview={overview} />
            </div>
          </div>
          <div className="grid gap-4 xl:grid-cols-24">
            <div className="xl:col-span-10">
              <TopModels overview={overview} />
            </div>
            <div className="xl:col-span-14">
              <AlertsCard overview={overview} canWrite={permissions.can('system.write')} />
            </div>
          </div>
        </>
      )}
    </Page>
  )
}

export default function AdminOverviewPage() {
  return (
    <PermissionGate permission="system.read">
      <OverviewWorkspace />
    </PermissionGate>
  )
}
