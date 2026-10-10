import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getRoutingApplications } from '@/api/runtime-applications'
import { getRuntimeInstallations } from '@/api/runtime-installations'
import type { RoutingApplicationsPage } from '@/types/runtime-applications'
import type { RuntimeInstallationsPage } from '@/types/runtime-installations'
import { locale } from '@/i18n'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Table } from '@/components/ui/table'

function date(value: string) {
  return new Intl.DateTimeFormat(locale(), { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(value),
  )
}

export function RoutingApplicationsDialog({
  instanceID,
  mode = 'routing',
  actorID,
  authority,
  isCurrent,
  onClose,
}: {
  instanceID: string
  mode?: 'routing' | 'installation'
  actorID: string
  authority: string
  isCurrent: () => boolean
  onClose: () => void
}) {
  const { t } = useTranslation('systemStatus')
  const [cursor, setCursor] = useState<string>()
  const installation = mode === 'installation'
  const copy = installation ? 'installation' : 'routing'
  const query = useQuery<RoutingApplicationsPage | RuntimeInstallationsPage>({
    queryKey: [
      'admin',
      installation ? 'runtime-installations' : 'runtime-applications',
      actorID,
      authority,
      instanceID,
      cursor,
    ],
    queryFn: async ({ signal }) => {
      if (!isCurrent() || signal.aborted) throw new DOMException('Obsolete read', 'AbortError')
      const read = installation ? getRuntimeInstallations : getRoutingApplications
      const page = await read({ instance_id: instanceID, ...(cursor ? { cursor } : {}) }, signal)
      if (!isCurrent() || signal.aborted) throw new DOMException('Obsolete read', 'AbortError')
      return page
    },
    retry: false,
    gcTime: 0,
    refetchOnWindowFocus: false,
  })
  const visible = query.isSuccess && !query.isFetching && !query.isError && isCurrent()
  return (
    <Dialog
      open={isCurrent()}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      title={t(`${copy}RecordsTitle`)}
      description={t(`${copy}RecordsDescription`)}
      width={880}
    >
      <p className="mb-4 break-all font-mono text-xs">{instanceID}</p>
      {query.isFetching && <p role="status">{t('refreshing')}</p>}
      {query.isError && (
        <p role="alert" className="text-destructive">
          {t(`${copy}RecordsFailed`)}
        </p>
      )}
      {visible && query.data.items.length === 0 && <p role="status">{t(`${copy}RecordsEmpty`)}</p>}
      {visible && query.data.items.length > 0 && (
        <Table aria-label={t(`${copy}RecordsTable`)}>
          <thead>
            <tr>
              {[
                'routingSnapshot',
                installation ? 'installationObserved' : 'routingApplied',
                'routingInstanceStatus',
                'routingCurrentMatch',
              ].map((key) => (
                <th key={key}>{t(key)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {query.data.items.map((row) => {
              const record =
                'first_observed_at' in row
                  ? {
                      published: row.routes_published_at,
                      observed: row.first_observed_at,
                      matches: row.current_serving_installation_matches,
                    }
                  : {
                      published: row.published_at,
                      observed: row.applied_at,
                      matches: row.current_serving_snapshot_matches,
                    }
              return (
                <tr key={row.id}>
                  <td className="max-w-64 break-all">
                    <code className="block text-xs">{row.snapshot_id}</code>
                    <span className="block text-xs text-muted-foreground">
                      {t('routingPublishedAt', { time: date(record.published) })}
                    </span>
                  </td>
                  <td className="min-w-40 text-xs">
                    <span className="block">{date(record.observed)}</span>
                    <span className="block text-muted-foreground">
                      {t('routingProcessStarted', { time: date(row.instance_started_at) })}
                    </span>
                  </td>
                  <td>{t(row.instance_status)}</td>
                  <td className="min-w-44 text-sm">
                    {t(
                      record.matches === null
                        ? 'unknown'
                        : record.matches
                          ? `${copy}Match`
                          : `${copy}Different`,
                    )}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </Table>
      )}
      {visible && (
        <p className="mt-3 text-xs text-muted-foreground">
          {t('observedAt', { time: date(query.data.observed_at) })}
        </p>
      )}
      <div className="mt-5 flex flex-wrap justify-end gap-2">
        <Button
          variant="outline"
          disabled={query.isFetching}
          onClick={() => {
            if (isCurrent()) void query.refetch()
          }}
        >
          {t('refresh')}
        </Button>
        {cursor && (
          <Button
            variant="outline"
            disabled={query.isFetching}
            onClick={() => {
              if (isCurrent()) setCursor(undefined)
            }}
          >
            {t('routingNewest')}
          </Button>
        )}
        {visible && query.data.next_cursor && (
          <Button
            variant="outline"
            onClick={() => {
              if (isCurrent()) setCursor(query.data.next_cursor!)
            }}
          >
            {t('routingOlder')}
          </Button>
        )}
        <Button variant="outline" onClick={onClose}>
          {t('routingClose')}
        </Button>
      </div>
    </Dialog>
  )
}
