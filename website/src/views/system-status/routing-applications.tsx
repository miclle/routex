import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getRoutingApplications } from '@/api/runtime-applications'
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
  actorID,
  authority,
  isCurrent,
  onClose,
}: {
  instanceID: string
  actorID: string
  authority: string
  isCurrent: () => boolean
  onClose: () => void
}) {
  const { t } = useTranslation('systemStatus')
  const [cursor, setCursor] = useState<string>()
  const query = useQuery({
    queryKey: ['admin', 'runtime-applications', actorID, authority, instanceID, cursor],
    queryFn: async ({ signal }) => {
      if (!isCurrent() || signal.aborted) throw new DOMException('Obsolete read', 'AbortError')
      const page = await getRoutingApplications(
        { instance_id: instanceID, ...(cursor ? { cursor } : {}) },
        signal,
      )
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
      title={t('routingRecordsTitle')}
      description={t('routingRecordsDescription')}
      width={880}
    >
      <p className="mb-4 break-all font-mono text-xs">{instanceID}</p>
      {query.isFetching && <p role="status">{t('refreshing')}</p>}
      {query.isError && (
        <p role="alert" className="text-destructive">
          {t('routingRecordsFailed')}
        </p>
      )}
      {visible && query.data.items.length === 0 && <p role="status">{t('routingRecordsEmpty')}</p>}
      {visible && query.data.items.length > 0 && (
        <Table aria-label={t('routingRecordsTable')}>
          <thead>
            <tr>
              {[
                'routingSnapshot',
                'routingApplied',
                'routingInstanceStatus',
                'routingCurrentMatch',
              ].map((key) => (
                <th key={key}>{t(key)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {query.data.items.map((row) => (
              <tr key={row.id}>
                <td className="max-w-64 break-all">
                  <code className="block text-xs">{row.snapshot_id}</code>
                  <span className="block text-xs text-muted-foreground">
                    {t('routingPublishedAt', { time: date(row.published_at) })}
                  </span>
                </td>
                <td className="min-w-40 text-xs">
                  <span className="block">{date(row.applied_at)}</span>
                  <span className="block text-muted-foreground">
                    {t('routingProcessStarted', { time: date(row.instance_started_at) })}
                  </span>
                </td>
                <td>{t(row.instance_status)}</td>
                <td className="min-w-44 text-sm">
                  {t(
                    row.current_serving_snapshot_matches === null
                      ? 'unknown'
                      : row.current_serving_snapshot_matches
                        ? 'routingMatch'
                        : 'routingDifferent',
                  )}
                </td>
              </tr>
            ))}
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
