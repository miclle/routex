import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, CircleAlert, Gauge, Settings, Timer, Waypoints } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  getProviderQuality,
  getProviderQualityPolicy,
  providerQualityKey,
  providerQualityPolicyKey,
  ProviderQualityError,
  updateProviderQualityPolicy,
} from '@/api/provider-quality'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Table } from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { protocolLabel } from '@/lib/protocols'
import type { Connection, Provider } from '@/types/catalog'
import type { ProviderQualityPolicy, ProviderQualitySummary } from '@/types/provider-quality'

type ProviderTab = 'overview' | 'connections' | 'credentials' | 'models' | 'settings'

function readyCredentialCount(connection: Connection) {
  return connection.credentials.filter(
    (credential) => credential.enabled && credential.verification_status === 'verified',
  ).length
}

function enabledModelCount(connection: Connection) {
  return connection.provider_models.filter((model) => model.enabled).length
}

function connectionReady(connection: Connection) {
  return readyCredentialCount(connection) > 0 && enabledModelCount(connection) > 0
}

function formatTime(value: string, locale: string) {
  return new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(value))
}

function formatDuration(value: number, locale: string) {
  if (value < 1000) return `${new Intl.NumberFormat(locale).format(value)} ms`
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(value / 1000)} s`
}

function qualitySuccessRate(quality: ProviderQualitySummary, locale: string) {
  const fraction =
    quality.success_rate_bps === null ? quality.success_rate : quality.success_rate_bps / 10_000
  return fraction === null
    ? null
    : new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 2 }).format(fraction)
}

function QualityFacts({ provider }: { provider: Provider }) {
  const { t, i18n } = useTranslation('catalog')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const quality = useQuery({
    queryKey: providerQualityKey(provider.id),
    queryFn: ({ signal }) => getProviderQuality(provider.id, signal),
    retry: false,
  })
  const facts = quality.data
  const rate = facts ? qualitySuccessRate(facts, locale) : null
  return (
    <Card>
      <CardHeader className="gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <CardTitle>{t('providers.qualityTitle')}</CardTitle>
          <p className="mt-2 text-sm text-muted-foreground">{t('providers.qualityDescription')}</p>
        </div>
        {facts && (
          <Badge variant={facts.status === 'healthy' ? 'success' : 'outline'}>
            {t(`providers.qualityStates.${facts.status}`)}
          </Badge>
        )}
      </CardHeader>
      <CardContent className="space-y-5">
        {quality.isPending && (
          <p role="status" className="text-sm text-muted-foreground">
            {t('providers.qualityLoading')}
          </p>
        )}
        {quality.isError && (
          <div className="flex flex-wrap items-center gap-3">
            <p role="alert" className="text-sm text-destructive">
              {t('providers.qualityLoadFailed')}
            </p>
            <Button variant="outline" size="sm" onClick={() => void quality.refetch()}>
              {t('providers.retry')}
            </Button>
          </div>
        )}
        {facts && (
          <>
            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
              {[
                [t('providers.requests24h'), new Intl.NumberFormat(locale).format(facts.requests)],
                [t('providers.qualitySuccessRate'), rate ?? t('providers.unknown')],
                [
                  t('providers.p95FullAttempt'),
                  facts.p95_duration_ms === null
                    ? t('providers.unknown')
                    : formatDuration(facts.p95_duration_ms, locale),
                ],
                [
                  t('providers.rateLimited'),
                  new Intl.NumberFormat(locale).format(facts.rate_limited_attempts),
                ],
                [
                  t('providers.serverErrors'),
                  new Intl.NumberFormat(locale).format(facts.server_error_attempts),
                ],
              ].map(([label, value]) => (
                <div key={label} className="space-y-1 rounded-lg border p-4">
                  <p className="text-sm text-muted-foreground">{label}</p>
                  <p className="text-xl font-semibold tabular-nums">{value}</p>
                </div>
              ))}
            </div>
            <dl className="grid gap-3 text-sm sm:grid-cols-2 xl:grid-cols-5">
              <div>
                <dt className="text-muted-foreground">{t('providers.observedWindow')}</dt>
                <dd className="mt-1">
                  {formatTime(facts.window_start, locale)} – {formatTime(facts.window_end, locale)}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('providers.evaluatedAt')}</dt>
                <dd className="mt-1">{formatTime(facts.evaluated_at, locale)}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('providers.dataThrough')}</dt>
                <dd className="mt-1">
                  {(facts.latest_completed_at ?? facts.data_through)
                    ? formatTime(facts.latest_completed_at ?? facts.data_through!, locale)
                    : t('providers.notObserved')}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('providers.evaluatedAttempts')}</dt>
                <dd className="mt-1">
                  {t('providers.attemptCoverage', {
                    eligible: facts.eligible_attempts,
                    excluded: facts.excluded_attempts,
                    credentials: facts.credential_rejected_attempts ?? 0,
                  })}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('providers.durationCoverage')}</dt>
                <dd className="mt-1">
                  {t('providers.durationCoverageValue', {
                    known: facts.known_duration_attempts,
                    eligible: facts.eligible_attempts,
                  })}
                </dd>
              </div>
            </dl>
            {facts.unknown_attribution_attempts > 0 && (
              <p role="status" className="flex items-start gap-2 text-sm text-muted-foreground">
                <CircleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
                {t('providers.unknownAttribution', {
                  count: facts.unknown_attribution_attempts,
                })}
              </p>
            )}
            {facts.status === 'insufficient_data' && (
              <p role="status" className="text-sm text-muted-foreground">
                {t('providers.insufficientQuality')}
              </p>
            )}
            {facts.status === 'unconfigured' && (
              <p role="status" className="text-sm text-muted-foreground">
                {t('providers.unconfiguredQuality')}
              </p>
            )}
            {facts.may_lag && (
              <p role="status" className="text-sm text-amber-700">
                {t('providers.qualityMayLag')}
              </p>
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}

export function ProviderOverview({
  provider,
  onSelectTab,
}: {
  provider: Provider
  onSelectTab: (tab: ProviderTab) => void
}) {
  const { t } = useTranslation('catalog')
  const readyConnections = provider.connections.filter(connectionReady).length
  const validCredentials = provider.connections.reduce(
    (total, connection) => total + readyCredentialCount(connection),
    0,
  )
  const enabledModels = provider.connections.reduce(
    (total, connection) => total + enabledModelCount(connection),
    0,
  )
  const totalCredentials = provider.connections.flatMap(
    (connection) => connection.credentials,
  ).length
  const totalModels = provider.connections.flatMap(
    (connection) => connection.provider_models,
  ).length
  const attention = [
    {
      key: 'connections',
      count: provider.connections.length - readyConnections,
      text: t('providers.connectionsNeedAttention', {
        count: provider.connections.length - readyConnections,
      }),
      tab: 'connections' as const,
    },
    {
      key: 'credentials',
      count: totalCredentials - validCredentials,
      text: t('providers.credentialsNeedAttention', {
        count: totalCredentials - validCredentials,
      }),
      tab: 'credentials' as const,
    },
    {
      key: 'models',
      count: totalModels - enabledModels,
      text: t('providers.modelsNeedAttention', { count: totalModels - enabledModels }),
      tab: 'models' as const,
    },
  ].filter((item) => item.count > 0)
  return (
    <div className="space-y-4">
      <div className="grid gap-4 xl:grid-cols-3">
        <Card className="xl:col-span-2">
          <CardHeader className="gap-2 sm:flex-row sm:items-center sm:justify-between">
            <CardTitle>{t('providers.serviceStatus')}</CardTitle>
            <Badge variant={readyConnections > 0 ? 'success' : 'outline'}>
              {readyConnections > 0
                ? t('providers.serviceConfigured')
                : t('providers.serviceNotConfigured')}
            </Badge>
          </CardHeader>
          <CardContent className="space-y-5">
            <p className="text-sm text-muted-foreground">
              {readyConnections > 0
                ? t('providers.serviceReadyDescription', {
                    ready: readyConnections,
                    models: enabledModels,
                  })
                : t('providers.serviceNotConfiguredDescription')}
            </p>
            <dl className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
              {[
                [
                  t('providers.readyConnections'),
                  `${readyConnections} / ${provider.connections.length}`,
                ],
                [t('providers.availableCredentials'), `${validCredentials} / ${totalCredentials}`],
                [t('providers.enabledModels'), `${enabledModels} / ${totalModels}`],
                [
                  t('providers.protocolCount'),
                  String(
                    new Set(provider.connections.map((connection) => connection.protocol)).size,
                  ),
                ],
              ].map(([label, value]) => (
                <div key={label}>
                  <dt className="text-sm text-muted-foreground">{label}</dt>
                  <dd className="mt-1 text-lg font-semibold tabular-nums">{value}</dd>
                </div>
              ))}
            </dl>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{t('providers.attentionTitle')}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {attention.length === 0 && (
              <p className="text-sm text-muted-foreground">{t('providers.noAttention')}</p>
            )}
            {attention.map((item) => (
              <div key={item.key} className="flex items-center justify-between gap-3 text-sm">
                <span>{item.text}</span>
                <Button variant="ghost" size="sm" onClick={() => onSelectTab(item.tab)}>
                  {t('providers.reviewAction')}
                </Button>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>
      <Card>
        <CardHeader className="gap-2 sm:flex-row sm:items-center sm:justify-between">
          <CardTitle>{t('providers.connectionRuntime')}</CardTitle>
          <Button variant="outline" size="sm" onClick={() => onSelectTab('connections')}>
            {t('providers.manageConnections')}
          </Button>
        </CardHeader>
        <CardContent className="p-0">
          <Table aria-label={t('providers.connectionRuntime')}>
            <thead>
              <tr>
                <th>{t('common.connectionName')}</th>
                <th>{t('common.protocolType')}</th>
                <th>{t('providers.runtimeStatus')}</th>
                <th>{t('providers.availableCredentials')}</th>
                <th>{t('providers.enabledModels')}</th>
              </tr>
            </thead>
            <tbody>
              {provider.connections.map((connection) => (
                <tr key={connection.id}>
                  <td>{connection.name}</td>
                  <td>{protocolLabel(connection.protocol)}</td>
                  <td>
                    <Badge variant={connectionReady(connection) ? 'success' : 'outline'}>
                      {connectionReady(connection)
                        ? t('providers.connectionReady')
                        : t('providers.connectionNeedsConfiguration')}
                    </Badge>
                  </td>
                  <td>
                    {readyCredentialCount(connection)} / {connection.credentials.length}
                  </td>
                  <td>
                    {enabledModelCount(connection)} / {connection.provider_models.length}
                  </td>
                </tr>
              ))}
              {provider.connections.length === 0 && (
                <tr>
                  <td colSpan={5} className="text-center text-muted-foreground">
                    {t('providers.noConnections')}
                  </td>
                </tr>
              )}
            </tbody>
          </Table>
        </CardContent>
      </Card>
      <QualityFacts provider={provider} />
    </div>
  )
}

function PolicyDialog({
  provider,
  policy,
  open,
  onOpenChange,
  onSaved,
}: {
  provider: Provider
  policy: ProviderQualityPolicy
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const { t } = useTranslation('catalog')
  const session = useSession()
  const queryClient = useQueryClient()
  const key = providerQualityPolicyKey(provider.id)
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getProviderQualityPolicy(provider.id, signal),
    enabled: open,
    initialData: policy,
    retry: false,
  })
  const [enabled, setEnabled] = useState(policy.enabled)
  const [windowMinutes, setWindowMinutes] = useState(policy.window_minutes)
  const [minimumAttempts, setMinimumAttempts] = useState(policy.minimum_attempts)
  const [minimumSuccessBps, setMinimumSuccessBps] = useState(policy.min_success_rate_bps)
  const [maximumP95, setMaximumP95] = useState<string>(
    policy.max_p95_duration_ms === null ? '' : String(policy.max_p95_duration_ms),
  )
  const [reason, setReason] = useState('')
  const [seedEtag, setSeedEtag] = useState(policy.etag)
  const [conflict, setConflict] = useState(false)
  const [conflictReviewed, setConflictReviewed] = useState(false)
  const [rejectedEtag, setRejectedEtag] = useState('')
  const seedFrom = (value: ProviderQualityPolicy) => {
    setEnabled(value.enabled)
    setWindowMinutes(value.window_minutes)
    setMinimumAttempts(value.minimum_attempts)
    setMinimumSuccessBps(value.min_success_rate_bps)
    setMaximumP95(value.max_p95_duration_ms === null ? '' : String(value.max_p95_duration_ms))
  }
  const mutation = useMutation({
    mutationFn: () =>
      updateProviderQualityPolicy(
        provider.id,
        {
          enabled,
          window_minutes: windowMinutes,
          minimum_attempts: minimumAttempts,
          min_success_rate_bps: minimumSuccessBps,
          max_p95_duration_ms: maximumP95 === '' ? null : Number(maximumP95),
          reason: reason.trim(),
          etag: seedEtag,
        },
        session.data!.csrf_token,
      ),
    onSuccess: (value) => {
      queryClient.setQueryData(key, value)
      onSaved()
    },
    onError: (error) => {
      if (error instanceof ProviderQualityError && [409, 412].includes(error.status)) {
        setConflict(true)
        setConflictReviewed(false)
        setRejectedEtag(seedEtag)
        void query.refetch()
      }
    },
  })
  const maximumP95Valid =
    maximumP95 === '' || (Number(maximumP95) >= 1 && Number(maximumP95) <= 3_600_000)
  const conflictReady =
    conflict &&
    !query.isFetching &&
    query.data?.etag !== undefined &&
    query.data.etag !== rejectedEtag
  const canSave =
    reason.trim().length > 0 &&
    windowMinutes >= 5 &&
    windowMinutes <= 1440 &&
    minimumAttempts >= 1 &&
    minimumAttempts <= 100_000 &&
    minimumSuccessBps >= 0 &&
    minimumSuccessBps <= 10_000 &&
    maximumP95Valid &&
    (!conflict || (conflictReady && conflictReviewed))
  return (
    <Dialog
      width={640}
      open={open}
      onOpenChange={onOpenChange}
      title={t('providers.policyDialogTitle')}
      description={t('providers.policyDialogDescription')}
      busy={mutation.isPending}
    >
      <form
        className="space-y-5"
        onSubmit={(event) => {
          event.preventDefault()
          if (canSave) mutation.mutate()
        }}
      >
        <label className="flex items-start justify-between gap-4 rounded-lg border p-4 text-sm">
          <span>
            <span className="block font-medium">{t('providers.policyEnabled')}</span>
            <span className="mt-1 block text-muted-foreground">
              {t('providers.policyEnabledHelp')}
            </span>
          </span>
          <Switch
            checked={enabled}
            onCheckedChange={setEnabled}
            aria-label={t('providers.policyEnabled')}
          />
        </label>
        <div className="grid gap-4 sm:grid-cols-2">
          <label className="space-y-2 text-sm font-medium">
            <span>{t('providers.policyWindow')}</span>
            <Input
              type="number"
              min={5}
              max={1440}
              step={1}
              value={windowMinutes}
              onChange={(event) => setWindowMinutes(Number(event.target.value))}
            />
          </label>
          <label className="space-y-2 text-sm font-medium">
            <span>{t('providers.minimumAttempts')}</span>
            <Input
              type="number"
              min={1}
              max={100000}
              step={1}
              value={minimumAttempts}
              onChange={(event) => setMinimumAttempts(Number(event.target.value))}
            />
          </label>
          <label className="space-y-2 text-sm font-medium">
            <span>{t('providers.minimumSuccessBps')}</span>
            <Input
              type="number"
              min={0}
              max={10000}
              step={1}
              value={minimumSuccessBps}
              onChange={(event) => setMinimumSuccessBps(Number(event.target.value))}
            />
            <span className="block text-xs font-normal text-muted-foreground">
              {t('providers.minimumSuccessBpsHelp', {
                percent: new Intl.NumberFormat(undefined, {
                  style: 'percent',
                  maximumFractionDigits: 2,
                }).format(minimumSuccessBps / 10_000),
              })}
            </span>
          </label>
          <label className="space-y-2 text-sm font-medium">
            <span>{t('providers.maximumP95')}</span>
            <Input
              type="number"
              min={1}
              max={3600000}
              step={1}
              value={maximumP95}
              aria-invalid={!maximumP95Valid}
              onChange={(event) => setMaximumP95(event.target.value)}
              placeholder={t('providers.noP95Threshold')}
            />
          </label>
        </div>
        <label className="block space-y-2 text-sm font-medium">
          <span>{t('providers.changeReason')}</span>
          <Textarea
            required
            maxLength={500}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            className="min-h-24 font-normal"
          />
        </label>
        {conflict && (
          <div className="space-y-3 rounded-lg border border-amber-300 bg-amber-50 p-4 text-sm text-amber-950">
            <p className="font-medium">{t('providers.policyConflictTitle')}</p>
            <p>{t('providers.policyConflictDescription')}</p>
            {conflictReady ? (
              <p>{t('providers.policyLatestReady')}</p>
            ) : query.isFetching ? (
              <p role="status">{t('providers.policyReviewingLatest')}</p>
            ) : query.isError ? (
              <div className="space-y-2">
                <p role="alert">{t('providers.policyConflictLoadFailed')}</p>
                <Button type="button" variant="outline" onClick={() => void query.refetch()}>
                  {t('providers.retry')}
                </Button>
              </div>
            ) : (
              <p role="status">{t('providers.policyReviewingLatest')}</p>
            )}
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="outline"
                disabled={!conflictReady}
                onClick={() => {
                  seedFrom(query.data!)
                  setSeedEtag(query.data!.etag)
                  setConflictReviewed(true)
                }}
              >
                {t('providers.useLatestPolicy')}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={!conflictReady}
                onClick={() => {
                  setSeedEtag(query.data!.etag)
                  setConflictReviewed(true)
                }}
              >
                {t('providers.keepPolicyDraft')}
              </Button>
            </div>
          </div>
        )}
        {mutation.isError && !conflict && (
          <p role="alert" className="text-sm text-destructive">
            {t('providers.policySaveFailed')}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" disabled={!canSave || mutation.isPending}>
            {mutation.isPending ? t('providers.savingPolicy') : t('providers.savePolicy')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}

export function ProviderSettings({ provider }: { provider: Provider }) {
  const { t, i18n } = useTranslation('catalog')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const access = usePermissions()
  const [open, setOpen] = useState(false)
  const [saved, setSaved] = useState(false)
  const policy = useQuery({
    queryKey: providerQualityPolicyKey(provider.id),
    queryFn: ({ signal }) => getProviderQualityPolicy(provider.id, signal),
    enabled: access.can('system.read'),
    retry: false,
  })
  return (
    <div className="space-y-4">
      <Card>
        <CardHeader className="gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle>{t('providers.policyTitle')}</CardTitle>
            <p className="mt-2 text-sm text-muted-foreground">{t('providers.policyDescription')}</p>
          </div>
          {access.can('system.write') && policy.data && (
            <Button
              variant="outline"
              onClick={() => {
                setSaved(false)
                setOpen(true)
              }}
            >
              <Settings className="size-4" aria-hidden />
              {t('providers.configurePolicy')}
            </Button>
          )}
        </CardHeader>
        <CardContent>
          {saved && (
            <p role="status" className="mb-4 rounded-lg border bg-muted p-3 text-sm">
              {t('providers.policySaved')}
            </p>
          )}
          {access.isPending && <p role="status">{t('providers.policyLoading')}</p>}
          {!access.isPending && !access.can('system.read') && (
            <p className="text-sm text-muted-foreground">{t('providers.policyReadDenied')}</p>
          )}
          {access.can('system.read') && policy.isPending && (
            <p role="status" className="text-sm text-muted-foreground">
              {t('providers.policyLoading')}
            </p>
          )}
          {access.can('system.read') && policy.isError && (
            <div className="flex flex-wrap items-center gap-3">
              <p role="alert" className="text-sm text-destructive">
                {t('providers.policyLoadFailed')}
              </p>
              <Button variant="outline" size="sm" onClick={() => void policy.refetch()}>
                {t('providers.retry')}
              </Button>
            </div>
          )}
          {policy.data && (
            <div className="space-y-4">
              <Badge variant={policy.data.enabled ? 'success' : 'outline'}>
                {policy.data.enabled ? t('providers.policyActive') : t('providers.policyInactive')}
              </Badge>
              <dl className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
                <div>
                  <dt className="flex items-center gap-2 text-sm text-muted-foreground">
                    <Activity className="size-4" aria-hidden />
                    {t('providers.policyWindow')}
                  </dt>
                  <dd className="mt-1 font-medium">
                    {t('providers.windowMinutes', { count: policy.data.window_minutes })}
                  </dd>
                </div>
                <div>
                  <dt className="flex items-center gap-2 text-sm text-muted-foreground">
                    <Waypoints className="size-4" aria-hidden />
                    {t('providers.minimumAttempts')}
                  </dt>
                  <dd className="mt-1 font-medium">{policy.data.minimum_attempts}</dd>
                </div>
                <div>
                  <dt className="flex items-center gap-2 text-sm text-muted-foreground">
                    <Gauge className="size-4" aria-hidden />
                    {t('providers.minimumSuccessRate')}
                  </dt>
                  <dd className="mt-1 font-medium">
                    {new Intl.NumberFormat(locale, {
                      style: 'percent',
                      maximumFractionDigits: 2,
                    }).format(policy.data.min_success_rate_bps / 10_000)}
                  </dd>
                </div>
                <div>
                  <dt className="flex items-center gap-2 text-sm text-muted-foreground">
                    <Timer className="size-4" aria-hidden />
                    {t('providers.maximumP95')}
                  </dt>
                  <dd className="mt-1 font-medium">
                    {policy.data.max_p95_duration_ms === null
                      ? t('providers.noP95Threshold')
                      : formatDuration(policy.data.max_p95_duration_ms, locale)}
                  </dd>
                </div>
              </dl>
              <p className="text-xs text-muted-foreground">
                {policy.data.updated_at
                  ? t('providers.policyUpdated', {
                      time: formatTime(policy.data.updated_at, locale),
                    })
                  : t('providers.policyNotUpdated')}
              </p>
            </div>
          )}
        </CardContent>
      </Card>
      {access.can('system.write') && policy.data && open && (
        <PolicyDialog
          provider={provider}
          policy={policy.data}
          open={open}
          onOpenChange={setOpen}
          onSaved={() => {
            setSaved(true)
            setOpen(false)
          }}
        />
      )}
    </div>
  )
}
