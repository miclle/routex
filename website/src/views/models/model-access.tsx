import { useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { Bot, Copy, RefreshCw } from 'lucide-react'
import { getPersonalModelCandidate } from '@/api/personal-model-requests'
import AccessRequestFooter from '@/views/team-model-requests/footer'
import type { ModelCatalogRecord } from '@/types/model-catalog'
import { getModelCatalogRecord, modelCatalogError } from '@/api/model-catalog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Drawer } from '@/components/ui/drawer'
import { isGeminiModelName, protocolLabel, protocolLabels } from '@/lib/protocols'
import ModelAccessSources from './access-sources'
import {
  declaredCapabilities,
  knownModelProtocols,
  modelSourceKey,
  personallyAvailable,
  teamInvocationSupported,
} from './catalogue-metadata'
import { exampleProtocols, modelExample } from './model-examples'

export default function ModelAccess({
  actorID,
  modelID,
  requestable = false,
  visible = true,
  generation = 0,
  isCurrent = () => true,
  onClose,
}: {
  actorID: string
  modelID: string
  requestable?: boolean
  visible?: boolean
  generation?: number
  isCurrent?: () => boolean
  onClose: () => void
}) {
  const { t } = useTranslation('catalog')
  const cache = useQueryClient()
  const [requestBusy, setRequestBusy] = useState(false)
  const queryKey = useMemo(
    () =>
      requestable
        ? ['personal-model-candidate-drawer', actorID, modelID, generation]
        : ['model-catalog', 'detail', actorID, modelID, generation],
    [requestable, actorID, modelID, generation],
  )
  const query = useQuery({
    queryKey,
    enabled: visible,
    queryFn: async ({ signal }): Promise<ModelCatalogRecord> => {
      if (!requestable) return getModelCatalogRecord(modelID, signal)
      const candidate = await getPersonalModelCandidate(modelID, signal)
      return {
        id: candidate.id,
        name: candidate.name,
        status: candidate.status,
        created_at: candidate.created_at,
        protocols: candidate.protocols,
        input_capabilities: candidate.input_capabilities,
        sources: [],
        personal_available: false,
      }
    },
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  useLayoutEffect(
    () => () => {
      void cache.cancelQueries({ queryKey, exact: true })
    },
    [cache, queryKey, visible],
  )
  const [exampleSource, setExampleSource] = useState<string | null>(null)
  const [sourceInitialized, setSourceInitialized] = useState(false)
  const [exampleProtocol, setExampleProtocol] = useState('openai_chat')
  const [notice, setNotice] = useState<{
    value: 'memberModels.copied' | 'memberModels.copyFailed'
    example: string
    generation: number
  } | null>(null)
  // Earlier catalogue authorization cannot authorize a refreshed resource detail.
  const model = visible && query.isSuccess && !query.isFetching ? query.data : undefined
  const [requestOpened, setRequestOpened] = useState(false)
  if (!requestOpened && model) setRequestOpened(true)
  if (!sourceInitialized && model) {
    setSourceInitialized(true)
    const source = model.sources.length === 1 ? model.sources[0] : undefined
    setExampleSource(source ? modelSourceKey(source) : null)
    const eligible = exampleProtocols(model, source)
    setExampleProtocol(
      source?.type === 'team'
        ? (eligible[0] ?? '')
        : eligible.includes('openai_chat')
          ? 'openai_chat'
          : (eligible[0] ?? ''),
    )
  }
  const selectedSource = model?.sources.find((source) => modelSourceKey(source) === exampleSource)
  const protocols = model ? exampleProtocols(model, selectedSource) : []
  const modelProtocols = model ? knownModelProtocols(model) : []
  const activeProtocol = protocols.includes(exampleProtocol) ? exampleProtocol : undefined
  const gemini = activeProtocol === 'gemini_generate_content'
  const invalidGeminiName = gemini && !isGeminiModelName(model?.name ?? '')
  const invocationAvailable =
    !!selectedSource &&
    !!protocols.length &&
    (selectedSource.type === 'team' || (!!model && personallyAvailable(model)))
  const endpoint =
    selectedSource && activeProtocol
      ? selectedSource?.type === 'team'
        ? `${window.location.origin}/api/v1/teams/${encodeURIComponent(selectedSource.team_id)}`
        : `${window.location.origin}/${gemini ? 'v1beta' : 'v1'}`
      : undefined
  const authHeader = gemini
    ? 'x-goog-api-key: $ROUTEX_API_KEY'
    : activeProtocol === 'anthropic_messages'
      ? 'x-api-key: $ROUTEX_API_KEY'
      : 'Authorization: Bearer $ROUTEX_API_KEY'
  const example = model
    ? modelExample(model, selectedSource, activeProtocol, window.location.origin)
    : undefined
  const copyOwner = useRef({ example, isCurrent })
  useLayoutEffect(() => {
    const current = { example, isCurrent }
    copyOwner.current = current
    return () => {
      if (copyOwner.current === current)
        copyOwner.current = { example: undefined, isCurrent: () => false }
    }
  }, [example, isCurrent])
  function currentExample() {
    const detail = cache.getQueryState<ModelCatalogRecord>(queryKey)
    return (
      isCurrent() &&
      visible &&
      detail?.status === 'success' &&
      detail.fetchStatus === 'idle' &&
      detail.data === model
    )
  }
  async function copy() {
    if (!example || !currentExample()) return
    const owner = copyOwner.current
    try {
      await navigator.clipboard.writeText(example)
      if (copyOwner.current === owner && currentExample())
        setNotice({ value: 'memberModels.copied', example, generation })
    } catch {
      if (copyOwner.current === owner && currentExample())
        setNotice({ value: 'memberModels.copyFailed', example, generation })
    }
  }

  return (
    <Drawer
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      title={t('memberModels.apiTitle', { name: model?.name ?? modelID })}
      width={520}
      busy={requestBusy}
    >
      <div className="space-y-6">
        <div className="flex justify-end">
          <Button
            variant="outline"
            size="sm"
            disabled={!visible || query.isFetching}
            onClick={() => {
              if (!isCurrent() || !visible) return
              setNotice(null)
              void query.refetch()
            }}
          >
            <RefreshCw className="size-4" />
            {t('memberModels.refreshDetails')}
          </Button>
        </div>
        {query.isFetching && (
          <p role="status" className="text-sm text-muted-foreground">
            {t('memberModels.loadingDetails')}
          </p>
        )}
        {query.isError && (
          <p role="alert" className="rounded-md border border-destructive/30 p-3 text-sm">
            {modelCatalogError(query.error)}
          </p>
        )}
        {model && (
          <>
            <div className="space-y-3 rounded-lg border p-4">
              <div className="flex items-center gap-3">
                <Bot className="size-5" />
                <h2 className="min-w-0 break-words font-semibold">{model.name}</h2>
              </div>
              <Badge variant="outline">
                {protocolLabels(modelProtocols) || t('memberModels.unavailableProtocol')}
              </Badge>
              <p className="text-sm text-muted-foreground">
                {model && activeProtocol && declaredCapabilities(model, activeProtocol).length
                  ? declaredCapabilities(model, activeProtocol)
                      .map((capability) => t(`memberModels.${capability}`))
                      .join(' · ')
                  : t('memberModels.noDeclaredCapabilities')}
              </p>
              <ModelAccessSources name={model.name} sources={model.sources} expanded />
            </div>
            {model.sources
              .filter((source) => teamInvocationSupported(source))
              .map(
                (source) =>
                  source.type === 'team' && (
                    <Link
                      key={source.team_id}
                      onClick={(event) => {
                        if (!currentExample()) event.preventDefault()
                      }}
                      to={`/playground?team=${encodeURIComponent(source.team_id)}&model=${encodeURIComponent(model.id)}`}
                      className="block text-sm underline"
                    >
                      {t('memberModels.openTeamConversation', { name: source.team_name })}
                    </Link>
                  ),
              )}
            <p className="rounded-md border bg-muted/30 p-3 text-sm">
              {t('memberModels.nativeFormat')}
            </p>
            {!model.personal_available &&
              model.sources.some((source) => source.type === 'team') && (
                <p role="status" className="rounded-md border p-3 text-sm text-muted-foreground">
                  {t(
                    model.sources.some(teamInvocationSupported)
                      ? 'memberModels.teamOnlyNativeGuidance'
                      : 'memberModels.teamOnlyGuidance',
                  )}
                </p>
              )}
            <section className="rounded-lg border">
              <h3 className="border-b p-4 text-sm font-semibold">
                {t('common.connectionSettings')}
              </h3>
              <div className="space-y-4 p-4">
                <div>
                  <p className="text-sm text-muted-foreground">{t('common.baseURL')}</p>
                  <code className="break-all text-sm">
                    {endpoint ?? t('memberModels.unavailableProtocol')}
                  </code>
                </div>
                <div>
                  <p className="text-sm text-muted-foreground">
                    {t(gemini ? 'memberModels.modelPath' : 'memberModels.modelParameter')}
                  </p>
                  <code className="break-all">{model.name}</code>
                </div>
                {example && selectedSource?.type === 'personal' && (
                  <>
                    <div>
                      <p className="text-sm text-muted-foreground">
                        {t('memberModels.authenticationHeader')}
                      </p>
                      <code className="break-all text-sm">{authHeader}</code>
                    </div>
                    <Link
                      to="/keys"
                      className="text-sm underline"
                      onClick={(event) => {
                        if (!currentExample()) event.preventDefault()
                      }}
                    >
                      {t('memberModels.manageKeys')}
                    </Link>
                  </>
                )}
              </div>
            </section>
            <section className="rounded-lg border">
              <div className="flex items-center justify-between border-b px-4 py-2">
                <h3 className="text-sm font-semibold">{t('memberModels.requestExample')}</h3>
                <Button size="sm" variant="ghost" disabled={!example} onClick={() => void copy()}>
                  <Copy className="size-3" />
                  {t('common.copy')}
                </Button>
              </div>
              {model.sources.length > 0 && (
                <label className="flex items-center gap-3 px-4 pt-4 text-sm">
                  {t('memberModels.exampleSource')}
                  <select
                    aria-label={t('memberModels.exampleSource')}
                    value={exampleSource ?? ''}
                    onChange={(event) => {
                      setExampleSource(event.target.value)
                      const source = model.sources.find(
                        (source) => modelSourceKey(source) === event.target.value,
                      )
                      setExampleProtocol(exampleProtocols(model, source)[0] ?? '')
                      setNotice(null)
                    }}
                    className="h-9 min-w-0 flex-1 rounded-md border bg-background px-3"
                  >
                    <option value="">{t('memberModels.chooseExampleSource')}</option>
                    {exampleSource && !selectedSource && (
                      <option value={exampleSource}>
                        {t('memberModels.exampleSourceUnavailable')}
                      </option>
                    )}
                    {model.sources.map((source) => (
                      <option key={modelSourceKey(source)} value={modelSourceKey(source)}>
                        {source.type === 'personal'
                          ? t('memberModels.personalGrant')
                          : t('memberModels.teamExampleSource', { name: source.team_name })}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              {selectedSource?.type === 'team' && example && (
                <p className="px-4 pt-4 text-sm text-muted-foreground">
                  {t('memberModels.teamExampleGuidance')}
                </p>
              )}
              {(protocols.length > 1 ||
                (selectedSource?.type === 'team' && protocols.length > 0) ||
                (protocols.length > 0 && !activeProtocol)) && (
                <label className="flex items-center gap-3 px-4 pt-4 text-sm">
                  {t('common.protocolType')}
                  <select
                    aria-label={t('common.protocolType')}
                    disabled={!selectedSource}
                    value={activeProtocol ?? ''}
                    onChange={(event) => {
                      setExampleProtocol(event.target.value)
                      setNotice(null)
                    }}
                    className="h-9 rounded-md border bg-background px-3"
                  >
                    {!activeProtocol && (
                      <option value="">{t('memberModels.chooseExampleProtocol')}</option>
                    )}
                    {protocols.map((protocol) => (
                      <option key={protocol} value={protocol}>
                        {protocolLabel(protocol)}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              {!selectedSource && model.sources.length > 0 ? (
                <p role="status" className="p-4 text-sm text-muted-foreground">
                  {t(
                    exampleSource
                      ? 'memberModels.exampleSourceUnavailable'
                      : 'memberModels.chooseExampleSource',
                  )}
                </p>
              ) : !activeProtocol ? (
                <p role="status" className="p-4 text-sm text-muted-foreground">
                  {t(
                    protocols.length
                      ? 'memberModels.chooseExampleProtocol'
                      : 'memberModels.protocolUnavailable',
                  )}
                </p>
              ) : !invocationAvailable ? (
                <p role="status" className="p-4 text-sm text-muted-foreground">
                  {t('memberModels.personalInvocationUnavailable')}
                </p>
              ) : invalidGeminiName ? (
                <p role="status" className="p-4 text-sm text-muted-foreground">
                  {t('memberModels.geminiAliasRequired')}
                </p>
              ) : (
                <pre className="overflow-auto p-4 text-xs leading-6">{example}</pre>
              )}
            </section>
            {notice && notice.generation === generation && notice.example === example && (
              <p role="status" className="text-sm">
                {t(notice.value)}
              </p>
            )}
          </>
        )}
        {requestOpened && (
          <AccessRequestFooter
            actorID={actorID}
            modelID={modelID}
            onBusy={setRequestBusy}
            personalGranted={model?.sources.some((source) => source.type === 'personal') ?? false}
            visible={!!model}
          />
        )}
      </div>
    </Drawer>
  )
}
