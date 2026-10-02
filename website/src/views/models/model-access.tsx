import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { Bot, Copy, RefreshCw } from 'lucide-react'
import { getModelCatalogRecord, modelCatalogError } from '@/api/model-catalog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Drawer } from '@/components/ui/drawer'
import { isGeminiModelName, protocolLabel, protocolLabels } from '@/lib/protocols'
import ModelAccessSources from './access-sources'
import {
  declaredCapabilities,
  knownModelProtocols,
  personallyAvailable,
} from './catalogue-metadata'

export default function ModelAccess({
  actorID,
  modelID,
  onClose,
}: {
  actorID: string
  modelID: string
  onClose: () => void
}) {
  const { t } = useTranslation('catalog')
  const query = useQuery({
    queryKey: ['model-catalog', 'detail', actorID, modelID],
    queryFn: ({ signal }) => getModelCatalogRecord(modelID, signal),
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const [exampleProtocol, setExampleProtocol] = useState('openai_chat')
  const [notice, setNotice] = useState<'memberModels.copied' | 'memberModels.copyFailed' | null>(
    null,
  )
  // Earlier catalogue authorization cannot authorize a refreshed resource detail.
  const model = query.isSuccess && !query.isFetching ? query.data : undefined
  const protocols = model ? knownModelProtocols(model) : []
  const activeProtocol = protocols.includes(exampleProtocol) ? exampleProtocol : protocols[0]
  const gemini = activeProtocol === 'gemini_generate_content'
  const invalidGeminiName = gemini && !isGeminiModelName(model?.name ?? '')
  const invocationAvailable = model && personallyAvailable(model)
  const endpoint = activeProtocol
    ? `${window.location.origin}/${gemini ? 'v1beta' : 'v1'}`
    : undefined
  const requestPath = gemini
    ? `models/${encodeURIComponent(model?.name ?? '')}:generateContent`
    : activeProtocol === 'anthropic_messages'
      ? 'messages'
      : activeProtocol === 'openai_responses'
        ? 'responses'
        : 'chat/completions'
  const requestBody = gemini
    ? { contents: [{ role: 'user', parts: [{ text: 'Hello' }] }] }
    : activeProtocol === 'openai_responses'
      ? { model: model?.name, input: 'Hello' }
      : {
          model: model?.name,
          ...(activeProtocol === 'anthropic_messages' ? { max_tokens: 1024 } : {}),
          messages: [{ role: 'user', content: 'Hello' }],
        }
  const authHeader = gemini
    ? 'x-goog-api-key: $ROUTEX_API_KEY'
    : activeProtocol === 'anthropic_messages'
      ? 'x-api-key: $ROUTEX_API_KEY'
      : 'Authorization: Bearer $ROUTEX_API_KEY'
  const headers = [
    authHeader,
    ...(activeProtocol === 'anthropic_messages' ? ['anthropic-version: 2023-06-01'] : []),
    'Content-Type: application/json',
  ]
  const example =
    invocationAvailable && activeProtocol && !invalidGeminiName
      ? [
          `curl ${endpoint}/${requestPath}`,
          ...headers.map((header) => `  -H "${header}"`),
          `  -d '${JSON.stringify(requestBody).replace(/'/g, "'\\''")}'`,
        ].join(' \\\n')
      : undefined

  async function copy() {
    if (!example) return
    try {
      await navigator.clipboard.writeText(example)
      setNotice('memberModels.copied')
    } catch {
      setNotice('memberModels.copyFailed')
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
    >
      <div className="space-y-6">
        <div className="flex justify-end">
          <Button
            variant="outline"
            size="sm"
            disabled={query.isFetching}
            onClick={() => {
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
                {protocolLabels(protocols) || t('memberModels.unavailableProtocol')}
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
            <p className="rounded-md border bg-muted/30 p-3 text-sm">
              {t('memberModels.nativeFormat')}
            </p>
            {!model.personal_available &&
              model.sources.some((source) => source.type === 'team') && (
                <p role="status" className="rounded-md border p-3 text-sm text-muted-foreground">
                  {t('memberModels.teamOnlyGuidance')}
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
                {example && (
                  <>
                    <div>
                      <p className="text-sm text-muted-foreground">
                        {t('memberModels.authenticationHeader')}
                      </p>
                      <code className="break-all text-sm">{authHeader}</code>
                    </div>
                    <Link to="/keys" className="text-sm underline">
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
              {protocols.length > 1 && (
                <label className="flex items-center gap-3 px-4 pt-4 text-sm">
                  {t('common.protocolType')}
                  <select
                    value={activeProtocol}
                    onChange={(event) => {
                      setExampleProtocol(event.target.value)
                      setNotice(null)
                    }}
                    className="h-9 rounded-md border bg-background px-3"
                  >
                    {protocols.map((protocol) => (
                      <option key={protocol} value={protocol}>
                        {protocolLabel(protocol)}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              {!activeProtocol ? (
                <p role="status" className="p-4 text-sm text-muted-foreground">
                  {t('memberModels.protocolUnavailable')}
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
            {notice && (
              <p role="status" className="text-sm">
                {t(notice)}
              </p>
            )}
          </>
        )}
      </div>
    </Drawer>
  )
}
