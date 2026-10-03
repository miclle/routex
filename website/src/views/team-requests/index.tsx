import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import { useSearchParams } from 'react-router'
import type {
  TeamRequestFilters,
  TeamRequestStatus,
  TeamQuotaDimension,
} from '@/types/team-requests'
import { listTeamRequests } from '@/api/team-requests'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { Table } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import ApplicationDialog from './application-dialog'
import DetailDrawer from './detail-drawer'
import { QuotaValue } from './context'
export default function TeamRequestsPage({ admin = false }: { admin?: boolean }) {
  const session = useSession()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  return <RequestWorkspace key={`${actor}:${admin}`} actor={actor} admin={admin} />
}
function RequestWorkspace({ actor, admin }: { actor: string; admin: boolean }) {
  const { t, i18n } = useTranslation('teamRequests')
  const access = usePermissions(),
    cache = useQueryClient()
  const [params, setParams] = useSearchParams(),
    [applying, setApplying] = useState(false),
    [notice, setNotice] = useState<string | null>(null)
  const [filters, setFilters] = useState<TeamRequestFilters>({ status: '', dimension: '' })
  const view = params.get('tab') === 'pending' ? 'pending' : 'my',
    selected = params.get('request')
  const authorized =
    !!actor &&
    (!admin ||
      (!access.isFetching && !access.isError && access.can('teams.quota_requests.read_all')))
  const history = useInfiniteQuery({
    queryKey: ['team-requests', actor, admin, view, filters],
    queryFn: ({ pageParam, signal }) =>
      listTeamRequests(admin, view, actor, filters, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page, pages) =>
      page.next_cursor && !pages.slice(0, -1).some((old) => old.next_cursor === page.next_cursor)
        ? page.next_cursor
        : undefined,
    enabled: authorized,
    retry: false,
    gcTime: 0,
    staleTime: 0,
    refetchOnMount: 'always',
  })
  const visible = authorized && history.isSuccess && !history.isFetching && !history.isError
  const rows = visible
    ? [
        ...new Map(
          history.data.pages.flatMap((page) => page.items).map((item) => [item.id, item]),
        ).values(),
      ]
    : []
  function select(id: string | null) {
    const next = new URLSearchParams(params)
    if (id) next.set('request', id)
    else next.delete('request')
    setParams(next, { replace: true })
  }
  function saved() {
    setNotice('saved')
    setApplying(false)
    void cache.invalidateQueries({ queryKey: ['team-requests', actor] })
  }
  function close(unknown: boolean) {
    if (unknown) setNotice('dismissedUnknown')
    setApplying(false)
    select(null)
    void history.refetch()
  }
  const table = (
    <>
      <div className="flex flex-wrap gap-3">
        <label className="flex items-center gap-2 text-sm">
          {t('state')}
          <select
            aria-label={t('state')}
            className="rounded-md border bg-background p-2"
            value={filters.status}
            onChange={(event) => {
              select(null)
              setFilters((previous) => ({
                ...previous,
                status: event.target.value as TeamRequestStatus | '',
              }))
            }}
          >
            <option value="">{t('all')}</option>
            {(
              [
                'pending_team_owner',
                'pending_quota_admin',
                'approved',
                'rejected',
                'withdrawn',
                'cancelled',
              ] as const
            ).map((value) => (
              <option key={value} value={value}>
                {t(value)}
              </option>
            ))}
          </select>
        </label>
        <label className="flex items-center gap-2 text-sm">
          {t('dimension')}
          <select
            aria-label={t('dimension')}
            className="rounded-md border bg-background p-2"
            value={filters.dimension}
            onChange={(event) => {
              select(null)
              setFilters((previous) => ({
                ...previous,
                dimension: event.target.value as TeamQuotaDimension | '',
              }))
            }}
          >
            <option value="">{t('all')}</option>
            {(['tokens', 'money'] as const).map((value) => (
              <option key={value} value={value}>
                {t(value)}
              </option>
            ))}
          </select>
        </label>
      </div>
      <QueryState
        pending={history.isFetching || (admin && access.isFetching)}
        error={history.error}
        retry={() => void history.refetch()}
      />
      {visible && (
        <>
          {admin && (
            <p className="text-sm text-muted-foreground">
              {t('total', { count: history.data.pages[0].total })}
            </p>
          )}
          {rows.length ? (
            <Table aria-label={t('history')}>
              <thead>
                <tr>
                  <th>{t('applicant')}</th>
                  <th>{t('team')}</th>
                  <th>{t('dimension')}</th>
                  <th>{t('effectiveMember')}</th>
                  <th>{t('target')}</th>
                  <th>{t('node')}</th>
                  <th>{t('escalation')}</th>
                  <th>{t('state')}</th>
                  <th>{t('submitted')}</th>
                  <th>{t('actions')}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((record) => (
                  <tr key={record.id}>
                    <td>{record.applicant_name || record.applicant_user_id}</td>
                    <td>{record.team_name || record.team_id}</td>
                    <td>{t(record.dimension)}</td>
                    <td>
                      <QuotaValue
                        value={record.submitted_snapshot.member_effective}
                        dimension={record.dimension}
                        currency={record.currency}
                      />
                    </td>
                    <td>
                      <QuotaValue
                        value={record.target_value}
                        dimension={record.dimension}
                        currency={record.currency}
                      />
                    </td>
                    <td>
                      {record.current_step_id
                        ? t(
                            record.steps.find((step) => step.id === record.current_step_id)
                              ?.stage ?? 'unknown',
                          )
                        : '—'}
                    </td>
                    <td>{record.escalation_reason ? t(record.escalation_reason) : '—'}</td>
                    <td>
                      <Badge variant="outline">{t(record.status)}</Badge>
                    </td>
                    <td className="whitespace-nowrap">
                      {new Date(record.created_at).toLocaleString(i18n.resolvedLanguage)}
                    </td>
                    <td>
                      <Button size="sm" variant="outline" onClick={() => select(record.id)}>
                        {t(admin || view === 'my' ? 'view' : 'review')}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          ) : (
            <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
              {t('empty')}
            </p>
          )}
        </>
      )}
      {visible && history.hasNextPage && (
        <Button
          variant="outline"
          disabled={history.isFetchingNextPage}
          onClick={() => void history.fetchNextPage()}
        >
          {t('more')}
        </Button>
      )}
    </>
  )
  return (
    <Page
      title={t(admin ? 'adminTitle' : 'title')}
      description={t(admin ? 'adminDescription' : 'description')}
    >
      <div className="space-y-4">
        <div className="flex flex-wrap justify-end gap-2">
          {!admin && (
            <Button onClick={() => setApplying(true)} disabled={!actor}>
              {t('apply')}
            </Button>
          )}
          <Button
            variant="outline"
            disabled={history.isFetching || !authorized}
            onClick={() => void history.refetch()}
          >
            {t('refresh')}
          </Button>
        </div>
        {notice && (
          <p role="status" className="rounded-lg border p-3 text-sm">
            {t(notice)}
          </p>
        )}
        {admin ? (
          authorized ? (
            table
          ) : (
            <>
              <QueryState
                pending={access.isPending || access.isFetching}
                error={access.error}
                retry={() => void access.refetch()}
              />
              {!access.isPending && !access.isFetching && <p role="alert">{t('denied')}</p>}
            </>
          )
        ) : (
          <Tabs
            value={view}
            onValueChange={(value) => {
              setNotice(null)
              setParams(value === 'my' ? {} : { tab: String(value) }, { replace: true })
            }}
          >
            <TabsList aria-label={t('title')}>
              <TabsTrigger value="my">{t('my')}</TabsTrigger>
              <TabsTrigger value="pending">{t('pending')}</TabsTrigger>
            </TabsList>
            <TabsContent value={view}>{table}</TabsContent>
          </Tabs>
        )}
        {applying && actor && <ApplicationDialog key={actor} onClose={close} onSaved={saved} />}
        {selected && authorized && (
          <DetailDrawer
            key={`${actor}:${admin}:${selected}`}
            id={selected}
            admin={admin}
            authorized={authorized && !history.isFetching && !history.isError}
            onClose={close}
            onSaved={() => void cache.invalidateQueries({ queryKey: ['team-requests', actor] })}
          />
        )}
      </div>
    </Page>
  )
}
