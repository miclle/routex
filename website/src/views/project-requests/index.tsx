import { useState } from 'react'
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { listProjectRequests } from '@/api/project-requests'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Table } from '@/components/ui/table'
import type { ResourceRecord } from '@/types/resources'
import type { ProjectRequest, ProjectRequestStatus } from '@/types/project-requests'
import ApplicationDialog from './application-dialog'
import DetailDialog from './detail-dialog'
import QuotaApplicationDialog from './quota-application-dialog'
import QuotaDetailDialog from './quota-detail-dialog'
import { QuotaValues } from './quota-values'
import { RateValues } from './rate-values'

export default function ProjectRequestsPanel({ project }: { project: ResourceRecord }) {
  const { t, i18n } = useTranslation('projectRequests')
  const session = useSession()
  const access = usePermissions()
  const cache = useQueryClient()
  const [status, setStatus] = useState<ProjectRequestStatus | ''>('')
  const [applying, setApplying] = useState(false)
  const [applyingQuota, setApplyingQuota] = useState(false)
  const [selected, setSelected] = useState<ProjectRequest | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const manager = project.managers?.some((m) => m.user_id === session.data?.user.id) === true
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const canRead =
    manager ||
    access.can('projects.models.write') ||
    access.can('projects.limits.write') ||
    access.can('projects.read_all')
  const canReadKind = (kind: ProjectRequest['kind']) =>
    manager ||
    access.can('projects.read_all') ||
    access.can(kind === 'MODEL_ACCESS' ? 'projects.models.write' : 'projects.limits.write')
  const active = project.status === 'active'
  const readScope = [
    manager,
    access.can('projects.models.write'),
    access.can('projects.limits.write'),
    access.can('projects.read_all'),
  ].join(':')
  const history = useInfiniteQuery({
    queryKey: ['project-requests', actor, project.id, status, readScope],
    queryFn: ({ pageParam, signal }) => listProjectRequests(project.id, status, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: !!actor && canRead && !access.isPending && !access.isError,
    retry: false,
  })
  const rows =
    actor && !access.isFetching && history.isSuccess && !history.isFetching && !history.isError
      ? history.data.pages.flatMap((p) => p.items).filter((record) => canReadKind(record.kind))
      : []
  function invalidate(kind?: ProjectRequest['kind']) {
    void cache.invalidateQueries({ queryKey: ['project-requests', actor, project.id] })
    void cache.invalidateQueries({ queryKey: ['project-request-quota-context', actor, project.id] })
    void cache.invalidateQueries({
      queryKey: ['project-request-limits-context', actor, project.id],
    })
    if (!kind || kind === 'MODEL_ACCESS') {
      void cache.invalidateQueries({ queryKey: ['project-request-candidates', project.id] })
      void cache.invalidateQueries({ queryKey: ['resources'] })
    }
    void cache.invalidateQueries({ queryKey: ['resource-limits'] })
  }
  function refresh(kind?: ProjectRequest['kind']) {
    setApplying(false)
    setApplyingQuota(false)
    setSelected(null)
    invalidate(kind)
  }
  function success(message: string, kind?: ProjectRequest['kind']) {
    setNotice(message)
    refresh(kind)
  }
  function dismissQuota(uncertain: boolean) {
    if (uncertain) setNotice('quota.dismissedUncertain')
    refresh('QUOTA')
  }
  if (access.isPending || access.isError)
    return (
      <QueryState
        pending={access.isPending}
        error={access.error}
        retry={() => void access.refetch()}
      />
    )
  if (!canRead)
    return (
      <p role="alert" className="text-sm text-muted-foreground">
        {t('deniedRead')}
      </p>
    )
  const date = (value: string) =>
    new Intl.DateTimeFormat(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US', {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date(value))
  return (
    <section className="space-y-5" aria-label={t('title')}>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h2 className="font-semibold">{t('title')}</h2>
          <p className="mt-1 text-sm text-muted-foreground">{t('description')}</p>
        </div>
        {manager && (
          <div className="flex flex-wrap gap-2">
            <Button disabled={!active} onClick={() => setApplying(true)}>
              {t('apply')}
            </Button>
            <Button disabled={!active} onClick={() => setApplyingQuota(true)}>
              {t('quota.apply')}
            </Button>
          </div>
        )}
      </div>
      {!manager && <p className="text-sm text-muted-foreground">{t('managerOnly')}</p>}
      {!active && (
        <p role="status" className="text-sm text-muted-foreground">
          {t('inactive')}
        </p>
      )}
      {notice && (
        <p role="status" className="rounded-lg border bg-muted p-3 text-sm">
          {t(notice)}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 text-sm">
          {t('status')}
          <select
            value={status}
            onChange={(event) => setStatus(event.target.value as ProjectRequestStatus | '')}
            className="rounded-md border bg-background px-3 py-2"
          >
            <option value="">{t('all')}</option>
            {(['pending', 'approved', 'rejected', 'withdrawn'] as const).map((value) => (
              <option key={value} value={value}>
                {t(value)}
              </option>
            ))}
          </select>
        </label>
        <Button variant="outline" disabled={history.isFetching} onClick={() => refresh()}>
          {t('refresh')}
        </Button>
      </div>
      <QueryState
        pending={history.isFetching || access.isFetching}
        error={history.error}
        retry={() => void history.refetch()}
      />
      {!history.isPending &&
        !history.isFetching &&
        !access.isFetching &&
        !history.isError &&
        (rows.length === 0 ? (
          <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
            {t('empty')}
          </p>
        ) : (
          <Table aria-label={t('history')}>
            <thead>
              <tr>
                <th>{t('status')}</th>
                <th>{t('requestKind')}</th>
                <th>{t('requestedChange')}</th>
                <th>{t('applicant')}</th>
                <th>{t('submitted')}</th>
                <th>{t('reason')}</th>
                <th>
                  <span className="sr-only">{t('review')}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((record) => (
                <tr key={record.id}>
                  <td>
                    <Badge variant="outline">{t(record.status)}</Badge>
                  </td>
                  <td>
                    {t(
                      record.kind === 'QUOTA'
                        ? 'quota.kind'
                        : record.kind === 'RATE_LIMIT'
                          ? 'rate.kind'
                          : 'kind',
                    )}
                  </td>
                  <td className="max-w-60 break-all text-xs">
                    {record.kind === 'QUOTA' ? (
                      <QuotaValues quota={record.requested_quota} patch />
                    ) : record.kind === 'RATE_LIMIT' ? (
                      <RateValues rate={record.requested_rate_limit} patch />
                    ) : (
                      <span className="font-mono">{record.requested_model_ids.join(', ')}</span>
                    )}
                  </td>
                  <td>{record.applicant_user_id}</td>
                  <td className="whitespace-nowrap">{date(record.created_at)}</td>
                  <td className="max-w-60">
                    <span className="line-clamp-2">{record.reason}</span>
                  </td>
                  <td>
                    <Button size="sm" variant="outline" onClick={() => setSelected(record)}>
                      {t('review')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        ))}
      {!history.isError && !history.isFetching && history.hasNextPage && (
        <Button
          variant="outline"
          disabled={history.isFetchingNextPage}
          onClick={() => void history.fetchNextPage()}
        >
          {t('more')}
        </Button>
      )}
      {applying && manager && actor && (
        <ApplicationDialog
          project={project}
          onClose={refresh}
          onRefresh={refresh}
          onSuccess={() => success('saved')}
        />
      )}
      {applyingQuota && manager && actor && (
        <QuotaApplicationDialog
          key={`${actor}:${project.id}`}
          project={project}
          onClose={dismissQuota}
          onSuccess={(kinds) =>
            success(
              kinds.length > 1
                ? 'rate.combinedSaved'
                : kinds[0] === 'RATE_LIMIT'
                  ? 'rate.saved'
                  : 'quota.saved',
              kinds[0],
            )
          }
        />
      )}
      {selected && selected.kind !== 'MODEL_ACCESS' && canReadKind(selected.kind) && actor && (
        <QuotaDetailDialog
          key={`${actor}:${project.id}:${selected.id}`}
          projectId={project.id}
          requestId={selected.id}
          kind={selected.kind}
          active={active}
          canDecide={access.can('projects.limits.write')}
          authorized={!access.isFetching}
          onClose={dismissQuota}
          onSaved={() => invalidate(selected.kind)}
        />
      )}
      {selected?.kind === 'MODEL_ACCESS' && canReadKind('MODEL_ACCESS') && (
        <DetailDialog
          request={selected}
          active={active}
          canDecide={access.can('projects.models.write')}
          onClose={refresh}
          onRefresh={refresh}
          onSuccess={() => success('decided')}
        />
      )}
    </section>
  )
}
