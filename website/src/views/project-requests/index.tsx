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

export default function ProjectRequestsPanel({ project }: { project: ResourceRecord }) {
  const { t, i18n } = useTranslation('projectRequests')
  const session = useSession()
  const access = usePermissions()
  const cache = useQueryClient()
  const [status, setStatus] = useState<ProjectRequestStatus | ''>('')
  const [applying, setApplying] = useState(false)
  const [selected, setSelected] = useState<ProjectRequest | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const manager = project.managers?.some((m) => m.user_id === session.data?.user.id) === true
  const canRead = manager || access.can('projects.models.write') || access.can('projects.read_all')
  const active = project.status === 'active'
  const history = useInfiniteQuery({
    queryKey: ['project-requests', project.id, status],
    queryFn: ({ pageParam, signal }) => listProjectRequests(project.id, status, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: canRead && !access.isPending && !access.isError,
    retry: false,
  })
  const rows = history.data?.pages.flatMap((p) => p.items) ?? []
  function refresh() {
    setApplying(false)
    setSelected(null)
    void cache.invalidateQueries({ queryKey: ['project-requests', project.id] })
    void cache.invalidateQueries({ queryKey: ['project-request-candidates', project.id] })
    void cache.invalidateQueries({ queryKey: ['resources'] })
  }
  function success(message: string) {
    setNotice(message)
    refresh()
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
          <Button disabled={!active} onClick={() => setApplying(true)}>
            {t('apply')}
          </Button>
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
        <Button variant="outline" disabled={history.isFetching} onClick={refresh}>
          {t('refresh')}
        </Button>
      </div>
      <QueryState
        pending={history.isPending}
        error={history.error}
        retry={() => void history.refetch()}
      />
      {!history.isPending &&
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
                <th>{t('models')}</th>
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
                  <td className="max-w-60 break-all font-mono text-xs">
                    {record.requested_model_ids.join(', ')}
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
      {history.hasNextPage && (
        <Button
          variant="outline"
          disabled={history.isFetchingNextPage}
          onClick={() => void history.fetchNextPage()}
        >
          {t('more')}
        </Button>
      )}
      {applying && manager && (
        <ApplicationDialog
          project={project}
          onClose={refresh}
          onRefresh={refresh}
          onSuccess={() => success('saved')}
        />
      )}
      {selected && (
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
