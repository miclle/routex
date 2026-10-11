import { useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { canonicalRuntimeID, getRoutingApplications } from '@/api/runtime-applications'
import { getRuntimeInstallations } from '@/api/runtime-installations'
import type { RoutingApplicationsPage } from '@/types/runtime-applications'
import type { RuntimeInstallationsPage } from '@/types/runtime-installations'
import { locale } from '@/i18n'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Table } from '@/components/ui/table'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

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
  const cache = useQueryClient()
  const [draft, setDraft] = useState({ snapshot: '', all: false })
  const [review, setReview] = useState({ snapshot: '', all: false, generation: 0 })
  const [reviewRequired, setReviewRequired] = useState(false)
  const lifetime = useRef<{
    review: typeof review
    draft: typeof draft
    cursor: typeof cursor
  } | null>(null)
  useLayoutEffect(() => {
    lifetime.current = { review, draft, cursor }
    return () => {
      lifetime.current = null
    }
  }, [review, draft, cursor])
  function reviewedCurrent() {
    return (
      isCurrent() &&
      !reviewRequired &&
      lifetime.current?.review === review &&
      lifetime.current?.cursor === cursor
    )
  }
  const validSnapshot = draft.snapshot === '' || canonicalRuntimeID(draft.snapshot, 'cfg')
  const installation = mode === 'installation'
  const copy = installation ? 'installation' : 'routing'
  const queryKey = [
    'admin',
    installation ? 'runtime-installations' : 'runtime-applications',
    actorID,
    authority,
    instanceID,
    cursor,
    ...(installation ? [review.snapshot, review.all, review.generation] : []),
  ]
  const query = useQuery<RoutingApplicationsPage | RuntimeInstallationsPage>({
    queryKey,
    enabled: !reviewRequired && isCurrent(),
    queryFn: async ({ signal }) => {
      if (!reviewedCurrent() || signal.aborted)
        throw new DOMException('Obsolete read', 'AbortError')
      const page = installation
        ? await getRuntimeInstallations(
            {
              ...(!review.all ? { instance_id: instanceID } : {}),
              ...(review.snapshot ? { snapshot_id: review.snapshot } : {}),
              ...(cursor ? { cursor } : {}),
            },
            signal,
          )
        : await getRoutingApplications(
            { instance_id: instanceID, ...(cursor ? { cursor } : {}) },
            signal,
          )
      if (!reviewedCurrent() || signal.aborted)
        throw new DOMException('Obsolete read', 'AbortError')
      return page
    },
    retry: false,
    gcTime: 0,
    refetchOnWindowFocus: false,
  })
  function edit(next: typeof draft) {
    if (!isCurrent() || lifetime.current?.draft !== draft || lifetime.current?.review !== review)
      return
    lifetime.current = null
    void cache.cancelQueries({ queryKey, exact: true })
    setReviewRequired(true)
    setDraft(next)
  }
  const visible =
    !reviewRequired && query.isSuccess && !query.isFetching && !query.isError && isCurrent()
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
      {installation && (
        <form
          className="mb-4 space-y-3"
          onSubmit={(event) => {
            event.preventDefault()
            if (!isCurrent() || !validSnapshot || lifetime.current?.draft !== draft) return
            lifetime.current = null
            setCursor(undefined)
            setReview({ ...draft, generation: review.generation + 1 })
            setReviewRequired(false)
          }}
        >
          <label className="block space-y-1">
            <span className="text-sm">{t('installationSnapshotLabel')}</span>
            <Input
              value={draft.snapshot}
              maxLength={30}
              autoComplete="off"
              aria-invalid={!validSnapshot}
              placeholder={t('installationSnapshotPlaceholder')}
              onChange={(event) => edit({ ...draft, snapshot: event.target.value })}
            />
          </label>
          {!validSnapshot && (
            <p role="alert" className="text-sm text-destructive">
              {t('installationSnapshotInvalid')}
            </p>
          )}
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={draft.all} onCheckedChange={(all) => edit({ ...draft, all })} />
            {t('installationAllInstances')}
          </label>
          <p className="text-xs text-muted-foreground">{t('installationScopeBoundary')}</p>
          <Button type="submit" variant="outline" disabled={!validSnapshot || query.isFetching}>
            {t('installationReviewFilters')}
          </Button>
          {reviewRequired && (
            <p role="status" className="text-sm">
              {t('installationReviewRequired')}
            </p>
          )}
        </form>
      )}
      {query.isFetching && <p role="status">{t('refreshing')}</p>}
      {!reviewRequired && query.isError && (
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
                ...(installation ? ['installationProcess'] : []),
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
                  {installation && (
                    <td className="max-w-56 break-all font-mono text-xs">{row.instance_id}</td>
                  )}
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
          disabled={query.isFetching || reviewRequired}
          onClick={() => {
            if (reviewedCurrent()) void query.refetch()
          }}
        >
          {t('refresh')}
        </Button>
        {cursor && (
          <Button
            variant="outline"
            disabled={query.isFetching || reviewRequired}
            onClick={() => {
              if (reviewedCurrent()) setCursor(undefined)
            }}
          >
            {t('routingNewest')}
          </Button>
        )}
        {visible && query.data.next_cursor && (
          <Button
            variant="outline"
            onClick={() => {
              if (reviewedCurrent()) setCursor(query.data.next_cursor!)
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
