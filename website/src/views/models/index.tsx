import { modelProtocols, protocolLabel, protocolLabels } from '@/lib/protocols'
import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { Bot, Grid2X2, List, Copy } from 'lucide-react'
import { listModels } from '@/api/catalog'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { Drawer } from '@/components/ui/drawer'
import type { CallableModel } from '@/types/catalog'

export default function ModelsPage() {
  const { t } = useTranslation('catalog')
  const models = useQuery({ queryKey: ['models'], queryFn: listModels })
  const [exampleProtocol, setExampleProtocol] = useState('openai_chat')
  const [query, setQuery] = useState('')
  const [view, setView] = useState('card')
  const [selected, setSelected] = useState<CallableModel | null>(null)
  const [notice, setNotice] = useState<'memberModels.copied' | 'memberModels.copyFailed' | null>(
    null,
  )
  const items =
    models.data?.filter((model) => model.name.toLowerCase().includes(query.toLowerCase())) ?? []
  const endpoint = `${window.location.origin}/v1`
  const protocols = selected ? modelProtocols(selected) : []
  const activeProtocol = protocols.includes(exampleProtocol) ? exampleProtocol : protocols[0]
  const requestPath = activeProtocol === 'openai_responses' ? 'responses' : 'chat/completions'
  const requestBody =
    activeProtocol === 'openai_responses'
      ? { model: selected?.name, input: 'Hello' }
      : { model: selected?.name, messages: [{ role: 'user', content: 'Hello' }] }
  const example = `curl ${endpoint}/${requestPath} \\\n  -H "Authorization: Bearer $ROUTEX_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '${JSON.stringify(requestBody)}'`
  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value)
      setNotice('memberModels.copied')
    } catch {
      setNotice('memberModels.copyFailed')
    }
  }
  return (
    <Page title={t('memberModels.title')} description={t('memberModels.description')}>
      <div
        aria-label={t('memberModels.statisticsLabel')}
        className="grid grid-cols-3 gap-6 rounded-lg border px-6 py-3"
      >
        {[
          [t('memberModels.total'), models.data?.length],
          [t('memberModels.available'), models.data?.length],
          [t('common.protocolType'), new Set(models.data?.flatMap(modelProtocols)).size],
        ].map(([label, value]) => (
          <div key={label}>
            <p className="text-sm text-muted-foreground">{label}</p>
            <p className="mt-1 text-2xl">{models.isSuccess ? value : '—'}</p>
          </div>
        ))}
      </div>
      <Input
        type="search"
        aria-label={t('common.searchModels')}
        placeholder={t('memberModels.searchPlaceholder')}
        value={query}
        onValueChange={setQuery}
      />
      <div className="flex items-center justify-between gap-4">
        <p className="text-sm text-muted-foreground">
          {t('memberModels.filtered', { count: items.length })}
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
      <QueryState
        pending={models.isPending}
        error={models.error}
        retry={() => void models.refetch()}
        empty={models.isSuccess && items.length === 0}
      />
      {view === 'card' ? (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {items.map((model) => (
            <button
              key={model.id}
              onClick={() => {
                setSelected(model)
                setNotice(null)
              }}
              aria-label={t('memberModels.openAPI', { name: model.name })}
              className="space-y-4 rounded-lg border border-foreground p-3 text-left"
            >
              <div className="flex items-center gap-2">
                <span className="rounded bg-muted p-2">
                  <Bot className="size-4" />
                </span>
                <h2 className="truncate text-sm font-semibold">{model.name}</h2>
              </div>
              <div className="flex gap-2">
                <Badge variant="outline">{protocolLabels(modelProtocols(model))}</Badge>
                <Badge variant="secondary">{t('memberModels.personalGrant')}</Badge>
              </div>
            </button>
          ))}
        </div>
      ) : (
        <Table aria-label={t('memberModels.listLabel')}>
          <thead>
            <tr>
              <th>{t('memberModels.model')}</th>
              <th>{t('memberModels.source')}</th>
              <th>{t('common.protocolType')}</th>
              <th>{t('common.actions')}</th>
            </tr>
          </thead>
          <tbody>
            {items.map((model) => (
              <tr key={model.id}>
                <td>{model.name}</td>
                <td>{t('memberModels.personalGrant')}</td>
                <td>{protocolLabels(modelProtocols(model))}</td>
                <td>
                  <Button size="sm" variant="ghost" onClick={() => setSelected(model)}>
                    {t('common.apiAccess')}
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <Drawer
        open={!!selected}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
        title={t('memberModels.apiTitle', { name: selected?.name ?? '' })}
        width={520}
      >
        <div className="space-y-6">
          <div className="flex items-center gap-3 rounded-lg border p-4">
            <Bot className="size-5" />
            <h2 className="font-semibold">{selected?.name}</h2>
            <Badge variant="outline">{protocolLabels(protocols)}</Badge>
          </div>
          <section className="rounded-lg border">
            <h3 className="border-b p-4 text-sm font-semibold">{t('common.connectionSettings')}</h3>
            <div className="space-y-4 p-4">
              <div>
                <p className="text-sm text-muted-foreground">{t('common.baseURL')}</p>
                <code className="break-all text-sm">{endpoint}</code>
              </div>
              <div>
                <p className="text-sm text-muted-foreground">{t('memberModels.modelParameter')}</p>
                <code>{selected?.name}</code>
              </div>
              <Link to="/keys" className="text-sm underline">
                {t('memberModels.manageKeys')}
              </Link>
            </div>
          </section>
          <section className="rounded-lg border">
            <div className="flex items-center justify-between border-b px-4 py-2">
              <h3 className="text-sm font-semibold">{t('memberModels.requestExample')}</h3>
              <Button size="sm" variant="ghost" onClick={() => void copy(example)}>
                <Copy className="size-3" />
                {t('common.copy')}
              </Button>
            </div>
            {protocols.length > 1 && (
              <label className="flex items-center gap-3 px-4 pt-4 text-sm">
                {t('common.protocolType')}
                <select
                  value={activeProtocol}
                  onChange={(event) => setExampleProtocol(event.target.value)}
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
            <pre className="overflow-auto p-4 text-xs leading-6">{example}</pre>
          </section>
          {notice && (
            <p role="status" className="text-sm">
              {t(notice)}
            </p>
          )}
        </div>
      </Drawer>
    </Page>
  )
}
