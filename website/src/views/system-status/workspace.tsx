import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { CheckCircle2, RefreshCw, Server, ServerCog, Trash2, TriangleAlert } from 'lucide-react'
import {
  cleanupSystemInstances,
  getSystemInstances,
  getSystemJobs,
  systemInstancesKey,
  systemJobsKey,
  SystemStatusError,
} from '@/api/system-status'
import type {
  SystemInstance,
  SystemJob,
  SystemJobCode,
  SystemJobDetailCode,
} from '@/types/system-status'
import { locale } from '@/i18n'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { Page } from '@/components/app/CatalogUI'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Table } from '@/components/ui/table'
import { ResourceGauge } from './resource-gauge'

type CleanupIssue = '' | 'conflict' | 'uncertain' | 'failed' | 'reviewFailed'
const cleanupBatchLimit = 100

const jobTypeKeys: Record<SystemJobCode, string> = {
  runtime_publication: 'jobTypes.runtime_publication',
  call_record_delivery: 'jobTypes.call_record_delivery',
  storage_cleanup: 'jobTypes.storage_cleanup',
}

const jobDetailKeys: Partial<Record<SystemJobDetailCode, string>> = {
  published: 'jobDetails.published',
  database_unavailable: 'jobDetails.database_unavailable',
  invalid_configuration: 'jobDetails.invalid_configuration',
  publication_failed: 'jobDetails.publication_failed',
  delivered: 'jobDetails.delivered',
  canceled: 'jobDetails.canceled',
  buffer_read_failed: 'jobDetails.buffer_read_failed',
  invalid_fact: 'jobDetails.invalid_fact',
  identity_mismatch: 'jobDetails.identity_mismatch',
  persistence_failed: 'jobDetails.persistence_failed',
  acknowledge_failed: 'jobDetails.acknowledge_failed',
  cleaned: 'jobDetails.cleaned',
  claim_failed: 'jobDetails.claim_failed',
  delete_failed: 'jobDetails.delete_failed',
  state_update_failed: 'jobDetails.state_update_failed',
  executor_lost: 'jobDetails.executor_lost',
}

const cleanupIssueKeys: Record<CleanupIssue, string> = {
  '': '',
  conflict: 'cleanupConflict',
  uncertain: 'cleanupUncertain',
  failed: 'cleanupFailed',
  reviewFailed: 'reviewFailed',
}

function date(value: string) {
  const parsed = new Date(value)
  if (Number.isNaN(parsed.valueOf())) return value
  return new Intl.DateTimeFormat(locale(), {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(parsed)
}

function ErrorState({ message, retry }: { message: string; retry: () => void }) {
  const { t } = useTranslation('systemStatus')
  return (
    <div className="space-y-3 p-4">
      <p role="alert" className="text-sm text-destructive">
        {message}
      </p>
      <Button variant="outline" onClick={retry}>
        <RefreshCw className="size-4" aria-hidden="true" />
        {t('retry')}
      </Button>
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  const { t } = useTranslation('systemStatus')
  const known = ['online', 'offline', 'running', 'completed', 'failed'].includes(status)
  return (
    <Badge
      variant={status === 'online' || status === 'completed' ? 'success' : 'outline'}
      className={
        status === 'offline'
          ? 'border-amber-300 bg-amber-50 text-amber-800 dark:bg-amber-950 dark:text-amber-300'
          : status === 'failed'
            ? 'border-destructive/30 bg-destructive/5 text-destructive'
            : status === 'running'
              ? 'border-primary/30 bg-primary/5 text-primary'
              : undefined
      }
    >
      {known ? t(status) : status}
    </Badge>
  )
}

function RefreshButton({ fetching, onClick }: { fetching: boolean; onClick: () => void }) {
  const { t } = useTranslation('systemStatus')
  return (
    <Button variant="outline" disabled={fetching} onClick={onClick}>
      <RefreshCw className={`size-4 ${fetching ? 'animate-spin' : ''}`} aria-hidden="true" />
      {t(fetching ? 'refreshing' : 'refresh')}
    </Button>
  )
}

export function SystemStatusWorkspace() {
  const { t } = useTranslation('systemStatus')
  const session = useSession()
  const access = usePermissions()
  const instances = useQuery({
    queryKey: systemInstancesKey,
    queryFn: ({ signal }) => getSystemInstances(signal),
    retry: false,
    refetchOnWindowFocus: false,
  })
  const jobs = useQuery({
    queryKey: systemJobsKey,
    queryFn: ({ signal }) => getSystemJobs(signal),
    retry: false,
    refetchOnWindowFocus: false,
  })
  const [reviewed, setReviewed] = useState<SystemInstance[]>([])
  const [reviewedRemaining, setReviewedRemaining] = useState(0)
  const [issue, setIssue] = useState<CleanupIssue>('')
  const [busy, setBusy] = useState(false)
  const [noticeCount, setNoticeCount] = useState(0)
  const lock = useRef(false)
  const rows = instances.data?.items ?? []
  const offline = rows.filter((instance) => instance.status === 'offline')
  const allEligible = offline.filter((instance) => instance.cleanup_eligible)
  const eligible = allEligible.slice(0, cleanupBatchLimit)
  const eligibleRemaining = allEligible.length - eligible.length

  function cleaned(data: SystemInstance[] | undefined) {
    return !!data && reviewed.every((candidate) => !data.some((item) => item.id === candidate.id))
  }

  async function refreshAfterCleanup() {
    const current = await instances.refetch()
    return current.isSuccess ? current.data?.items : undefined
  }

  async function submitCleanup() {
    if (lock.current || issue || !reviewed.length || !session.data || !access.can('system.write'))
      return
    lock.current = true
    setBusy(true)
    setNoticeCount(0)
    try {
      const result = await cleanupSystemInstances(
        {
          instances: reviewed.map((instance) => ({
            id: instance.id,
            revision: instance.heartbeat_revision,
          })),
        },
        session.data.csrf_token,
      )
      const current = await refreshAfterCleanup()
      const reviewedIDs = reviewed.map((instance) => instance.id).sort()
      const cleanedIDs = [...result.cleaned_ids].sort()
      if (
        result.cleaned_count !== reviewed.length ||
        cleanedIDs.length !== reviewedIDs.length ||
        cleanedIDs.some((id, index) => id !== reviewedIDs[index]) ||
        !cleaned(current)
      ) {
        setIssue('uncertain')
        return
      }
      setNoticeCount(reviewed.length)
      setReviewed([])
      setReviewedRemaining(0)
    } catch (error) {
      const status = error instanceof SystemStatusError ? error.status : 0
      if (status === 409) {
        await refreshAfterCleanup()
        setIssue('conflict')
      } else if (status === 0 || status >= 500) {
        await refreshAfterCleanup()
        setIssue('uncertain')
      } else setIssue('failed')
    } finally {
      lock.current = false
      setBusy(false)
    }
  }

  async function reviewLatest() {
    if (lock.current) return
    lock.current = true
    setBusy(true)
    try {
      const current = await instances.refetch()
      if (!current.isSuccess || !current.data) {
        setIssue('reviewFailed')
        return
      }
      setReviewed([])
      setReviewedRemaining(0)
      setIssue('')
    } finally {
      lock.current = false
      setBusy(false)
    }
  }

  return (
    <Page title={t('title')} description={t('description')}>
      <div className="space-y-6">
        <Card className="system-status-instances">
          <CardHeader className="flex-row flex-wrap items-center justify-between gap-3">
            <CardTitle className="flex flex-wrap items-center gap-2">
              <Server className="size-4" aria-hidden="true" />
              {t('instances')}
              {instances.data && (
                <Badge variant="outline">
                  {t('onlineCount', {
                    count: rows.filter((instance) => instance.status === 'online').length,
                  })}
                </Badge>
              )}
            </CardTitle>
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs text-muted-foreground" aria-live="polite">
                {instances.data?.observed_at
                  ? t('observedAt', { time: date(instances.data.observed_at) })
                  : t('onDemand')}
              </span>
              <RefreshButton
                fetching={instances.isFetching}
                onClick={() => void instances.refetch()}
              />
              {eligible.length > 0 && access.can('system.write') && (
                <Button
                  variant="outline"
                  className="text-destructive hover:text-destructive"
                  onClick={() => {
                    setNoticeCount(0)
                    setIssue('')
                    setReviewed(eligible.map((instance) => ({ ...instance })))
                    setReviewedRemaining(eligibleRemaining)
                  }}
                >
                  <Trash2 className="size-4" aria-hidden="true" />
                  {t('cleanup')}
                </Button>
              )}
            </div>
          </CardHeader>
          <CardContent className="space-y-4 p-0">
            {noticeCount > 0 && (
              <p role="status" className="px-4 pt-4 text-sm text-emerald-700 dark:text-emerald-400">
                {t('cleanupSuccess', { count: noticeCount })}
              </p>
            )}
            {offline.length > 0 && (
              <div
                role="alert"
                className="mx-4 mt-4 flex gap-3 rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-950 dark:text-amber-200"
              >
                <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
                <div>
                  <p className="font-medium">{t('offlineWarning', { count: offline.length })}</p>
                  <p className="mt-1 opacity-80">{t('offlineWarningDescription')}</p>
                </div>
              </div>
            )}
            {eligibleRemaining > 0 && access.can('system.write') && (
              <p className="mx-4 text-sm text-muted-foreground">
                {t('cleanupBatchRemaining', {
                  count: eligibleRemaining,
                  limit: cleanupBatchLimit,
                })}
              </p>
            )}
            {instances.isPending && (
              <p role="status" className="p-8 text-center text-sm text-muted-foreground">
                {t('refreshing')}
              </p>
            )}
            {instances.isError && (
              <ErrorState
                message={t('instancesLoadFailed')}
                retry={() => void instances.refetch()}
              />
            )}
            {instances.data && rows.length === 0 && (
              <p role="status" className="p-8 text-center text-sm text-muted-foreground">
                {t('noInstances')}
              </p>
            )}
            {rows.length > 0 && <InstancesTable rows={rows} />}
          </CardContent>
        </Card>

        <Card className="system-status-jobs">
          <CardHeader className="flex-row flex-wrap items-center justify-between gap-3">
            <CardTitle className="flex flex-wrap items-center gap-2">
              <CheckCircle2 className="size-4" aria-hidden="true" />
              {t('systemJobs')}
              {jobs.data && (
                <Badge variant="outline">
                  {t('inProgressCount', {
                    count: jobs.data.items.filter((job) => job.status === 'running').length,
                  })}
                </Badge>
              )}
            </CardTitle>
            <RefreshButton fetching={jobs.isFetching} onClick={() => void jobs.refetch()} />
          </CardHeader>
          <CardContent className="p-0">
            {jobs.isPending && (
              <p role="status" className="p-8 text-center text-sm text-muted-foreground">
                {t('refreshing')}
              </p>
            )}
            {jobs.isError && (
              <ErrorState message={t('jobsLoadFailed')} retry={() => void jobs.refetch()} />
            )}
            {jobs.data && jobs.data.items.length === 0 && (
              <p role="status" className="p-8 text-center text-sm text-muted-foreground">
                {t('noJobs')}
              </p>
            )}
            {!!jobs.data?.items.length && <JobsTable rows={jobs.data.items} />}
          </CardContent>
        </Card>
      </div>

      <Dialog
        open={reviewed.length > 0}
        onOpenChange={(open) => {
          if (!open) {
            setReviewed([])
            setReviewedRemaining(0)
            setIssue('')
          }
        }}
        title={t('cleanupTitle')}
        description={t('cleanupDescription')}
        busy={busy}
      >
        <div className="space-y-5">
          <p className="text-sm">{t('cleanupCount', { count: reviewed.length })}</p>
          {reviewedRemaining > 0 && (
            <p className="text-sm text-muted-foreground">
              {t('cleanupBatchRemaining', {
                count: reviewedRemaining,
                limit: cleanupBatchLimit,
              })}
            </p>
          )}
          <ul className="max-h-48 space-y-2 overflow-auto rounded-md border p-3 text-sm">
            {reviewed.map((instance) => (
              <li key={instance.id} className="break-all font-mono text-xs">
                {t('cleanupCandidate', {
                  name: instance.name || instance.id,
                  id: instance.id,
                  revision: instance.heartbeat_revision,
                })}
              </li>
            ))}
          </ul>
          <p className="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-950 dark:text-amber-200">
            {t('cleanupRecovery')}
          </p>
          {issue && (
            <p role="alert" className="text-sm text-destructive">
              {t(cleanupIssueKeys[issue])}
            </p>
          )}
          <div className="flex justify-end gap-3">
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => {
                setReviewed([])
                setReviewedRemaining(0)
                setIssue('')
              }}
            >
              {t('cancel')}
            </Button>
            {issue ? (
              <Button disabled={busy} onClick={() => void reviewLatest()}>
                <RefreshCw className={`size-4 ${busy ? 'animate-spin' : ''}`} aria-hidden="true" />
                {t('reviewLatest')}
              </Button>
            ) : (
              <Button
                disabled={busy || !access.can('system.write')}
                className="bg-destructive text-white hover:bg-destructive/90"
                onClick={() => void submitCleanup()}
              >
                <Trash2 className="size-4" aria-hidden="true" />
                {t(busy ? 'cleaning' : 'confirmCleanup')}
              </Button>
            )}
          </div>
        </div>
      </Dialog>
    </Page>
  )
}

function InstancesTable({ rows }: { rows: SystemInstance[] }) {
  const { t } = useTranslation('systemStatus')
  return (
    <Table aria-label={t('instancesTable')} className="min-w-[1390px]">
      <thead>
        <tr>
          {[
            'instance',
            'status',
            'role',
            'cpu',
            'memory',
            'storage',
            'version',
            'runtime',
            'started',
            'lastHeartbeat',
          ].map((column) => (
            <th key={column}>{t(column)}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((instance) => {
          const stale = instance.status === 'offline'
          return (
            <tr key={instance.id}>
              <td className="min-w-64">
                <div className="flex items-center gap-3">
                  <span
                    className={`size-2 shrink-0 rounded-full ${stale ? 'bg-amber-500' : 'bg-emerald-500'}`}
                    aria-hidden="true"
                  />
                  <span className="min-w-0">
                    <span className="block break-all font-medium">
                      {instance.name || instance.id}
                    </span>
                    <span className="block break-all text-xs text-muted-foreground">
                      {instance.hostname}
                    </span>
                  </span>
                </div>
              </td>
              <td>
                <StatusBadge status={instance.status} />
              </td>
              <td>
                <Badge variant="outline">
                  <ServerCog className="mr-1 size-3" aria-hidden="true" />
                  {instance.role === 'combined' ? t('combined') : instance.role}
                </Badge>
              </td>
              {(['cpu', 'memory', 'storage'] as const).map((resource) => (
                <td key={resource}>
                  <ResourceGauge
                    label={t(resource)}
                    sample={instance.resources[resource]}
                    stale={stale}
                  />
                </td>
              ))}
              <td className="min-w-32">
                <code className="block text-xs">{instance.version || t('unknown')}</code>
                {instance.commit && (
                  <span
                    className="block max-w-32 truncate text-xs text-muted-foreground"
                    title={instance.commit}
                  >
                    {instance.commit}
                  </span>
                )}
              </td>
              <td className="min-w-36">
                <code className="block text-xs">
                  {instance.os}/{instance.arch}
                </code>
                <span className="block text-xs text-muted-foreground">{instance.go_version}</span>
              </td>
              <td className="whitespace-nowrap text-xs">{date(instance.started_at)}</td>
              <td
                className={`whitespace-nowrap text-xs ${stale ? 'text-amber-700 dark:text-amber-300' : 'text-muted-foreground'}`}
              >
                {date(instance.last_heartbeat_at)}
                {stale && <span className="sr-only"> {t('staleSample')}</span>}
              </td>
            </tr>
          )
        })}
      </tbody>
    </Table>
  )
}

function JobProgress({ job }: { job: SystemJob }) {
  const { t, i18n } = useTranslation('systemStatus')
  if (job.progress === null) return <span className="text-muted-foreground">—</span>
  const progress = Math.max(0, Math.min(100, job.progress))
  const formatted = new Intl.NumberFormat(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US', {
    maximumFractionDigits: 1,
  }).format(progress)
  return (
    <div className="flex min-w-40 items-center gap-2">
      <div
        role="progressbar"
        aria-label={`${t('progress')} ${formatted}%`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={progress}
        className="h-2 w-28 overflow-hidden rounded-full bg-muted"
      >
        <div
          className={`h-full rounded-full ${job.status === 'failed' ? 'bg-destructive' : 'bg-primary'}`}
          style={{ width: `${progress}%` }}
        />
      </div>
      <span className="whitespace-nowrap text-xs tabular-nums text-muted-foreground">
        {formatted}%
      </span>
    </div>
  )
}

function JobsTable({ rows }: { rows: SystemJob[] }) {
  const { t } = useTranslation('systemStatus')
  function jobType(code: string) {
    return code in jobTypeKeys ? t(jobTypeKeys[code as SystemJobCode]) : code
  }
  function detail(job: SystemJob) {
    if (!job.detail_code) return job.status === 'running' ? t('jobDetails.running') : t('unknown')
    const key =
      job.detail_code in jobDetailKeys
        ? jobDetailKeys[job.detail_code as SystemJobDetailCode]
        : undefined
    return key
      ? t(key, { completed: job.items_completed, total: job.items_total })
      : job.detail_code
  }
  return (
    <Table aria-label={t('jobsTable')} className="min-w-[1020px]">
      <thead>
        <tr>
          {['job', 'status', 'progress', 'executor', 'updated', 'details'].map((column) => (
            <th key={column}>{t(column)}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((job) => (
          <tr key={job.id}>
            <td className="min-w-60">
              <span className="block font-medium">{jobType(job.code)}</span>
              <code className="block text-xs text-muted-foreground">{job.code}</code>
            </td>
            <td>
              <StatusBadge status={job.status} />
            </td>
            <td>
              <JobProgress job={job} />
            </td>
            <td className="min-w-52 break-all font-mono text-xs">
              {job.executor_id || t('unknown')}
            </td>
            <td className="whitespace-nowrap text-xs">{date(job.updated_at)}</td>
            <td
              className={
                job.status === 'failed'
                  ? 'min-w-64 text-destructive'
                  : 'min-w-64 text-muted-foreground'
              }
            >
              {detail(job)}
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  )
}
