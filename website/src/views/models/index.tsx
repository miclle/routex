import { useState } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Bot, Grid2X2, List, RefreshCw } from 'lucide-react'
import { listPersonalModelCandidates } from '@/api/personal-model-requests'
import { listModelCatalog, modelCatalogError } from '@/api/model-catalog'
import { Page } from '@/components/app/CatalogUI'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { Dialog } from '@/components/ui/dialog'
import ModelRequestHistory from '@/views/team-model-requests/history'
import { useSession } from '@/hooks/use-auth'
import { protocolLabel, protocolLabels } from '@/lib/protocols'
import type { ModelCatalogRecord, ModelInputCapability } from '@/types/model-catalog'
import ModelAccessSources from './access-sources'
import ModelAccess from './model-access'
import {
  declaredCapabilities,
  knownModelProtocols,
  modelSourceKey,
  orderedModelSources,
  personallyAvailable,
} from './catalogue-metadata'

export default function ModelsPage() {
  const { t, i18n } = useTranslation('catalog')
  const cache = useQueryClient()
  const session = useSession()
  const actorID = session.isError ? undefined : session.data?.user.id
  const models = useQuery({
    queryKey: ['model-catalog', 'list', actorID],
    queryFn: ({ signal }) => listModelCatalog(signal),
    enabled: !!actorID,
    retry: false,
    gcTime: 0,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const [query, setQuery] = useState('')
  const [source, setSource] = useState('all')
  const [protocol, setProtocol] = useState('all')
  const [capability, setCapability] = useState<'all' | ModelInputCapability>('all')
  const [view, setView] = useState('card')
  const [history, setHistory] = useState(false)
  const [historyBusy, setHistoryBusy] = useState(false)
  const [selectedID, setSelectedID] = useState<string | null>(null)
  const requesting = source === 'requestable'
  const candidates = useInfiniteQuery({
    queryKey: ['personal-model-candidates', actorID, query],
    queryFn: ({ pageParam, signal }) => listPersonalModelCandidates(query, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: !!actorID && requesting,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const candidateRecords: ModelCatalogRecord[] =
    actorID && !session.isFetching && candidates.isSuccess && !candidates.isFetching
      ? candidates.data.pages
          .flatMap((page) => page.items)
          .filter((item) => !item.personal_granted)
          .map((item) => ({
            id: item.id,
            name: item.name,
            status: item.status,
            created_at: item.created_at,
            protocols: item.protocols,
            input_capabilities: item.input_capabilities,
            personal_available: false,
            sources: [],
          }))
      : []
  const grantedRecords =
    actorID && !session.isFetching && models.isSuccess && !models.isFetching ? models.data : []
  const records = requesting ? candidateRecords : grantedRecords
  const sources = orderedModelSources([
    ...new Map(
      records.flatMap((model) => model.sources).map((item) => [modelSourceKey(item), item]),
    ).values(),
  ])
  const protocols = [...new Set(records.flatMap(knownModelProtocols))]
  const items = records.filter(
    (model) =>
      model.name.toLowerCase().includes(query.toLowerCase()) &&
      (source === 'all' ||
        requesting ||
        model.sources.some((item) => modelSourceKey(item) === source)) &&
      (protocol === 'all' || knownModelProtocols(model).includes(protocol)) &&
      (capability === 'all' || declaredCapabilities(model, protocol).includes(capability)),
  )
  const busy =
    session.isFetching || (!!actorID && (requesting ? candidates.isFetching : models.isFetching))
  const error = session.error ?? (requesting ? candidates.error : models.error)
  const current =
    !!actorID &&
    !session.isFetching &&
    (requesting
      ? candidates.isSuccess && !candidates.isFetching
      : models.isSuccess && !models.isFetching)
  const date = (value: string) => {
    const parsed = new Date(value)
    return Number.isNaN(parsed.valueOf())
      ? t('memberModels.unknown')
      : new Intl.DateTimeFormat(i18n.language === 'zh' ? 'zh-CN' : 'en', {
          year: 'numeric',
          month: '2-digit',
          day: '2-digit',
          hour: '2-digit',
          minute: '2-digit',
        }).format(parsed)
  }
  const capabilityLabels = (model: ModelCatalogRecord) => {
    const capabilities = declaredCapabilities(model)
    return capabilities.length
      ? capabilities.map((value) => t(`memberModels.${value}`)).join(' · ')
      : t('memberModels.noDeclaredCapabilities')
  }

  return (
    <Page title={t('memberModels.title')} description={t('memberModels.description')}>
      <div
        aria-label={t('memberModels.statisticsLabel')}
        className="grid grid-cols-2 gap-6 rounded-lg border px-6 py-3 lg:grid-cols-4"
      >
        {[
          [t('memberModels.total'), records.length],
          [t('memberModels.available'), records.filter(personallyAvailable).length],
          [t('common.protocolType'), protocols.length],
          [t('memberModels.accessSources'), sources.length],
        ].map(([label, value]) => (
          <div key={label}>
            <p className="text-sm text-muted-foreground">{label}</p>
            <p className="mt-1 text-2xl">{current && !requesting ? value : '—'}</p>
          </div>
        ))}
      </div>
      <div
        aria-label={t('memberModels.filtersLabel')}
        className="flex flex-wrap items-center gap-3"
      >
        <Input
          type="search"
          aria-label={t('common.searchModels')}
          placeholder={t('memberModels.searchPlaceholder')}
          value={query}
          onValueChange={setQuery}
          className="min-w-56 flex-1"
        />
        <select
          aria-label={t('memberModels.source')}
          value={source}
          onChange={(event) => setSource(event.target.value)}
          className="h-9 rounded-md border bg-background px-3 text-sm"
        >
          <option value="all">{t('memberModels.allSources')}</option>
          <option value="requestable">{t('personalModelRequests:discovery')}</option>
          {sources.map((item) => (
            <option key={modelSourceKey(item)} value={modelSourceKey(item)}>
              {item.type === 'personal' ? t('memberModels.personalGrant') : item.team_name}
            </option>
          ))}
        </select>
        <select
          aria-label={t('common.protocolType')}
          value={protocol}
          onChange={(event) => setProtocol(event.target.value)}
          className="h-9 rounded-md border bg-background px-3 text-sm"
        >
          <option value="all">{t('memberModels.allProtocols')}</option>
          {protocols.map((item) => (
            <option key={item} value={item}>
              {protocolLabel(item)}
            </option>
          ))}
        </select>
        <select
          aria-label={t('memberModels.inputCapabilities')}
          value={capability}
          onChange={(event) => setCapability(event.target.value as 'all' | ModelInputCapability)}
          className="h-9 rounded-md border bg-background px-3 text-sm"
        >
          <option value="all">{t('memberModels.allCapabilities')}</option>
          <option value="image">{t('memberModels.image')}</option>
          <option value="pdf">{t('memberModels.pdf')}</option>
        </select>
        <Button
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={() => {
            if (actorID) {
              if (selectedID)
                void cache.invalidateQueries({
                  queryKey: ['model-catalog', 'detail', actorID, selectedID],
                  exact: true,
                })
              if (requesting) void candidates.refetch()
              else void models.refetch()
            } else void session.refetch()
          }}
          aria-label={t('memberModels.refreshCatalogue')}
        >
          <RefreshCw className="size-4" />
          {t('memberModels.refreshCatalogue')}
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={!actorID || session.isFetching}
          onClick={() => setHistory(true)}
        >
          {t('personalModelRequests:ownHistory')}
        </Button>
      </div>
      <div className="flex items-center justify-between gap-4">
        <p className="text-sm text-muted-foreground">
          {t('memberModels.filtered', {
            count: items.length,
            available: items.filter(personallyAvailable).length,
          })}
        </p>
        <div aria-label={t('memberModels.viewLabel')} className="flex rounded-md bg-muted p-1">
          <Button
            size="sm"
            variant={view === 'card' ? 'outline' : 'ghost'}
            onClick={() => setView('card')}
            aria-pressed={view === 'card'}
          >
            <Grid2X2 className="size-4" />
            {t('memberModels.cards')}
          </Button>
          <Button
            size="sm"
            variant={view === 'table' ? 'outline' : 'ghost'}
            onClick={() => setView('table')}
            aria-pressed={view === 'table'}
          >
            <List className="size-4" />
            {t('memberModels.table')}
          </Button>
        </div>
      </div>
      {busy && (
        <p role="status" className="py-8 text-sm text-muted-foreground">
          {t('memberModels.loadingCatalogue')}
        </p>
      )}
      {error && (
        <p role="alert" className="rounded-md border border-destructive/30 p-3 text-sm">
          {modelCatalogError(error)}
        </p>
      )}
      {current && !items.length && (
        <p className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
          {t('memberModels.noMatchingModels')}
        </p>
      )}
      {current && !!items.length && (
        <p className="text-xs text-muted-foreground">{t('memberModels.unknownFields')}</p>
      )}
      {view === 'card' ? (
        <div
          className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4"
          role="list"
          aria-label={t('memberModels.cardsLabel')}
        >
          {items.map((model) => (
            <article
              key={model.id}
              role="listitem"
              className={`space-y-4 rounded-lg border p-3 ${personallyAvailable(model) ? 'border-foreground' : ''}`}
            >
              <button
                type="button"
                onClick={() => setSelectedID(model.id)}
                aria-label={t('memberModels.openAPI', { name: model.name })}
                aria-haspopup="dialog"
                className="flex w-full items-center gap-2 text-left"
              >
                <span className="rounded bg-muted p-2">
                  <Bot className="size-4" />
                </span>
                <h2 className="truncate text-sm font-semibold">{model.name}</h2>
              </button>
              <div className="flex flex-wrap gap-2">
                <Badge variant="outline">
                  {protocolLabels(knownModelProtocols(model)) ||
                    t('memberModels.unavailableProtocol')}
                </Badge>
                <Badge variant="outline">{capabilityLabels(model)}</Badge>
              </div>
              <ModelAccessSources
                name={model.name}
                sources={model.sources}
                selectedSource={source}
              />
              <dl className="space-y-2 text-sm">
                {[
                  ['memberModels.inputPrice', 'memberModels.unknown'],
                  ['memberModels.outputPrice', 'memberModels.unknown'],
                ].map(([label, value]) => (
                  <div key={label} className="flex justify-between gap-3">
                    <dt className="text-muted-foreground">{t(label)}</dt>
                    <dd>{t(value)}</dd>
                  </div>
                ))}
              </dl>
              <div className="flex justify-between gap-3 text-xs text-muted-foreground">
                <span>{t('memberModels.memberUsageUnknown')}</span>
                <span>{t('memberModels.monthlyCallsUnknown')}</span>
              </div>
            </article>
          ))}
        </div>
      ) : (
        <Table aria-label={t('memberModels.listLabel')}>
          <thead>
            <tr>
              {[
                'memberModels.model',
                'memberModels.source',
                'common.protocolType',
                'memberModels.inputCapabilities',
                'memberModels.inputPrice',
                'memberModels.outputPrice',
                'memberModels.createdAt',
                'memberModels.members',
                'memberModels.monthlyCalls',
                'common.actions',
              ].map((key) => (
                <th key={key}>{t(key)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {items.map((model) => (
              <tr key={model.id}>
                <td>{model.name}</td>
                <td>
                  <ModelAccessSources
                    name={model.name}
                    sources={model.sources}
                    selectedSource={source}
                  />
                </td>
                <td>
                  {protocolLabels(knownModelProtocols(model)) ||
                    t('memberModels.unavailableProtocol')}
                </td>
                <td>{capabilityLabels(model)}</td>
                <td>{t('memberModels.unknown')}</td>
                <td>{t('memberModels.unknown')}</td>
                <td className="whitespace-nowrap">{date(model.created_at)}</td>
                <td>{t('memberModels.unknown')}</td>
                <td>{t('memberModels.unknown')}</td>
                <td>
                  <Button size="sm" variant="ghost" onClick={() => setSelectedID(model.id)}>
                    {t('common.apiAccess')}
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {requesting && current && candidates.hasNextPage && (
        <Button
          variant="outline"
          disabled={candidates.isFetchingNextPage}
          onClick={() => void candidates.fetchNextPage()}
        >
          {t('personalModelRequests:loadMore')}
        </Button>
      )}
      {actorID && history && (
        <Dialog
          open
          busy={historyBusy}
          onOpenChange={setHistory}
          title={t('personalModelRequests:ownHistory')}
          description={t('personalModelRequests:sourceHelp')}
        >
          <ModelRequestHistory
            onBusy={setHistoryBusy}
            key={actorID}
            actor={actorID}
            visible={!session.isFetching && !session.isError}
          />
        </Dialog>
      )}
      {actorID && selectedID && (
        <ModelAccess
          key={`${actorID}:${selectedID}`}
          actorID={actorID}
          modelID={selectedID}
          requestable={requesting}
          visible={!session.isFetching && !session.isError}
          onClose={() => setSelectedID(null)}
        />
      )}
    </Page>
  )
}
