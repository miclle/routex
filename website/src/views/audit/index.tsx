import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useInfiniteQuery } from '@tanstack/react-query'
import { Search, X } from 'lucide-react'
import { listAudit } from '@/api/audit'
import type { AuditCategory, AuditFilters, AuditRange, AuditRecord } from '@/types/audit'
import { Page, QueryState, ErrorNotice } from '@/components/app/CatalogUI'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Drawer } from '@/components/ui/drawer'
const categories: AuditCategory[] = [
  'all',
  'models',
  'keys',
  'limits',
  'credentials',
  'pricing',
  'identity',
  'site',
]
export default function AuditPage() {
  return (
    <PermissionGate permission="audit.read">
      <AuditRecords />
    </PermissionGate>
  )
}
function AuditRecords() {
  const { t, i18n } = useTranslation('audit')
  const [filters, setFilters] = useState<AuditFilters>({ range: '7d', category: 'all', q: '' })
  const [selected, setSelected] = useState<AuditRecord | null>(null)
  const query = useInfiniteQuery({
    queryKey: ['audit', filters],
    queryFn: ({ pageParam, signal }) => listAudit(filters, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
  })
  const items =
    query.isError && !query.isFetchNextPageError
      ? []
      : (query.data?.pages.flatMap((page) => page.items) ?? [])
  const date = (value: string) =>
    new Date(value).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
  const missing = t('notRecorded')
  function change(patch: Partial<AuditFilters>) {
    setSelected(null)
    setFilters((current) => ({ ...current, ...patch }))
  }
  function summary(record: AuditRecord) {
    return record.changes
      ? `${JSON.stringify(record.changes.before)} → ${JSON.stringify(record.changes.after)}`
      : missing
  }
  const descriptionRows = selected
    ? [
        ['id', selected.id],
        ['actor', selected.actor_name || selected.actor_id || missing],
        ['actorID', selected.actor_id || missing],
        ['source', selected.source ?? missing],
        ['ip', selected.ip ?? missing],
        ['action', selected.action],
        ['resourceType', selected.resource_type],
        ['resourceID', selected.resource_id],
        ['result', t('committed')],
        ['requestID', selected.request_id ?? missing],
        ['time', date(selected.created_at)],
      ]
    : []
  return (
    <Page title={t('title')} description={t('scope')}>
      <div className="flex flex-wrap items-center gap-3" role="search" aria-label={t('filters')}>
        <div className="relative w-64">
          <Search
            className="pointer-events-none absolute left-3 top-3 size-4 text-muted-foreground"
            aria-hidden="true"
          />
          <Input
            className="h-10 pl-9 pr-9"
            aria-label={t('search')}
            placeholder={t('search')}
            value={filters.q}
            onValueChange={(value) => change({ q: Array.from(value).slice(0, 100).join('') })}
          />
          {filters.q && (
            <Button
              size="icon"
              variant="ghost"
              className="absolute right-1 top-1 size-8"
              aria-label={t('clearSearch')}
              onClick={() => change({ q: '' })}
            >
              <X className="size-3" aria-hidden="true" />
            </Button>
          )}
        </div>
        <select
          aria-label={t('range')}
          value={filters.range}
          onChange={(event) => change({ range: event.target.value as AuditRange })}
          className="h-10 w-[150px] rounded-md border bg-background px-3 text-sm"
        >
          {(['24h', '7d', '30d'] as const).map((value) => (
            <option key={value} value={value}>
              {t(`range_${value}`)}
            </option>
          ))}
        </select>
        <select
          aria-label={t('category')}
          value={filters.category}
          onChange={(event) => change({ category: event.target.value as AuditCategory })}
          className="h-10 w-[140px] rounded-md border bg-background px-3 text-sm"
        >
          {categories.map((value) => (
            <option key={value} value={value}>
              {t(`category_${value}`)}
            </option>
          ))}
        </select>
      </div>
      <p className="text-xs text-muted-foreground">{t('scope')}</p>
      <QueryState
        pending={query.isPending}
        error={query.isFetchNextPageError ? null : query.error}
        retry={() => void query.refetch()}
      />
      {!query.isPending && !query.isError && !items.length && (
        <p role="status" className="p-8 text-center text-sm text-muted-foreground">
          {t('empty')}
        </p>
      )}
      {items.length > 0 && (
        <div className="overflow-x-auto">
          <table
            aria-label={t('table')}
            className="w-full min-w-[1196px] table-fixed text-left text-sm"
          >
            <colgroup>
              {[88, 88, 136, 152, 180, 320, 80, 152].map((width, index) => (
                <col key={index} style={{ width }} />
              ))}
            </colgroup>
            <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
              <tr>
                {['actor', 'source', 'ip', 'action', 'target', 'changes', 'result', 'time'].map(
                  (key) => (
                    <th key={key} className="px-3 py-2 font-medium">
                      {t(key)}
                    </th>
                  ),
                )}
              </tr>
            </thead>
            <tbody className="divide-y">
              {items.map((record) => (
                <tr
                  key={record.id}
                  className="cursor-pointer hover:bg-muted/30"
                  onClick={() => setSelected(record)}
                >
                  <td className="truncate px-3 py-2">
                    <button
                      type="button"
                      className="max-w-full truncate text-left hover:underline focus-visible:outline-2"
                      aria-label={t('open', { id: record.id })}
                    >
                      {record.actor_name || record.actor_id || missing}
                    </button>
                  </td>
                  <td className="truncate px-3 py-2">{record.source ?? missing}</td>
                  <td className="truncate px-3 py-2 text-muted-foreground">
                    {record.ip ?? missing}
                  </td>
                  <td className="truncate px-3 py-2">
                    <Badge variant="outline">{record.action}</Badge>
                  </td>
                  <td className="truncate px-3 py-2">
                    {record.resource_type} · {record.resource_id}
                  </td>
                  <td className="truncate px-3 py-2 text-muted-foreground">{summary(record)}</td>
                  <td className="px-3 py-2">
                    <Badge variant="outline">{t('committed')}</Badge>
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-xs">{date(record.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {query.isFetchNextPageError && <ErrorNotice error={query.error} />}
      {query.hasNextPage && (
        <div className="text-center">
          <Button
            variant="outline"
            disabled={query.isFetching}
            onClick={() => void query.fetchNextPage({ cancelRefetch: false })}
          >
            {t(
              query.isFetchingNextPage
                ? 'loadingMore'
                : query.isFetchNextPageError
                  ? 'retryMore'
                  : 'loadMore',
            )}
          </Button>
        </div>
      )}
      <Drawer
        open={!!selected}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
        title={t('details')}
        description={t('scope')}
      >
        {selected && (
          <dl className="divide-y rounded-md border">
            {descriptionRows.map(([label, value]) => (
              <div key={label} className="grid grid-cols-[150px_minmax(0,1fr)] text-sm">
                <dt className="border-r bg-muted/30 p-3 font-medium">{t(label)}</dt>
                <dd className="min-w-0 whitespace-pre-wrap break-all p-3">{value}</dd>
              </div>
            ))}
            <div className="grid grid-cols-[150px_minmax(0,1fr)] text-sm">
              <dt className="border-r bg-muted/30 p-3 font-medium">{t('changes')}</dt>
              <dd className="min-w-0 p-3">
                {selected.changes ? (
                  <div className="space-y-3">
                    {(['before', 'after'] as const).map((field) => (
                      <div key={field}>
                        <p className="mb-1 font-medium">{t(field)}</p>
                        <pre className="whitespace-pre-wrap break-all font-mono text-xs">
                          {JSON.stringify(selected.changes![field], null, 2)}
                        </pre>
                      </div>
                    ))}
                    {selected.changes.reason !== undefined && (
                      <p className="whitespace-pre-wrap break-all">
                        {t('reason')}: {selected.changes.reason}
                      </p>
                    )}
                    {selected.changes.etag !== undefined && (
                      <p className="break-all font-mono text-xs">ETag: {selected.changes.etag}</p>
                    )}
                  </div>
                ) : (
                  missing
                )}
              </dd>
            </div>
          </dl>
        )}
      </Drawer>
    </Page>
  )
}
