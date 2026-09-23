import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import axios from 'axios'
import { getModelPrices, writePrices } from '@/api/pricing'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { ErrorNotice, FormField, QueryState, SaveButton } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog } from '@/components/ui/dialog'
import { Table } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { currencies, metrics, type ModelPrice, type PriceRate } from '@/types/pricing'

interface Editor {
  etag: string
  rate: PriceRate
  threshold: ModelPrice['context_threshold']
}
const selectClass = 'h-10 w-full rounded-md border bg-background px-3 text-sm'
export default function ModelPriceTable({
  modelId,
  modelName,
}: {
  modelId: string
  modelName: string
}) {
  const { t } = useTranslation('pricing')
  const session = useSession()
  const access = usePermissions()
  const cache = useQueryClient()
  const queryKey = ['admin', 'prices', modelId]
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => getModelPrices(modelId, signal),
    enabled: access.can('prices.read'),
  })
  const price = query.data?.items[0]
  const [editor, setEditor] = useState<Editor | null>(null)
  const [notice, setNotice] = useState('')
  const [validation, setValidation] = useState('')
  const mutation = useMutation({
    mutationFn: (draft: Editor) => {
      const { metric, tier, unit, currency, amount, enabled } = draft.rate
      return writePrices(
        {
          etag: draft.etag,
          items: [
            {
              provider_model_id: modelId,
              context_threshold: draft.threshold,
              rates: [{ metric, tier, unit, currency, amount, enabled }],
            },
          ],
        },
        session.data!.csrf_token,
      )
    },
    onSuccess: (page) => {
      cache.setQueryData(queryKey, page)
      void cache.invalidateQueries({ queryKey: ['admin', 'prices'] })
      setEditor(null)
      setNotice('saved')
    },
  })
  const status = axios.isAxiosError(mutation.error) ? mutation.error.response?.status : undefined
  const stale = status === 409
  function open(rate?: PriceRate) {
    if (!query.data) return
    mutation.reset()
    setValidation('')
    setNotice('')
    setEditor({
      etag: query.data.etag,
      threshold: price?.context_threshold ?? 0,
      rate: rate
        ? { ...rate }
        : {
            metric: 'INPUT_TOKEN',
            tier: 'base',
            unit: '1M_TOKEN',
            currency: query.data.currency.platform_currency,
            amount: '',
            enabled: true,
          },
    })
  }
  function change(change: Partial<PriceRate>) {
    setEditor((current) => current && { ...current, rate: { ...current.rate, ...change } })
    setValidation('')
  }
  function save(event: FormEvent) {
    event.preventDefault()
    if (!editor || mutation.isPending || stale || !access.can('prices.write')) return
    const rate = editor.rate
    if (!/^\d{1,18}(\.\d{1,18})?$/.test(rate.amount)) {
      setValidation('decimalError')
      return
    }
    if (
      !rate.id &&
      price?.rates.some((item) => item.metric === rate.metric && item.tier === rate.tier)
    ) {
      setValidation('duplicate')
      return
    }
    if (rate.enabled && rate.tier === 'long_context' && !editor.threshold) {
      setValidation('thresholdRequired')
      return
    }
    if (
      !editor.threshold &&
      price?.rates.some(
        (item) => item.id !== rate.id && item.enabled && item.tier === 'long_context',
      )
    ) {
      setValidation('thresholdConflict')
      return
    }
    setValidation('')
    mutation.mutate(editor)
  }
  async function reload() {
    const result = await query.refetch()
    if (!result.data || result.isError) return
    setEditor((current) => current && { ...current, etag: result.data.etag })
    mutation.reset()
    setValidation('')
    setNotice('reloadNotice')
  }
  if (!access.can('prices.read')) return null
  return (
    <section className="space-y-4" aria-label={t('title')}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">{t('description')}</p>
        {access.can('prices.write') && (
          <Button variant="outline" disabled={!query.data || query.isError} onClick={() => open()}>
            {t('add')}
          </Button>
        )}
      </div>
      {notice && (
        <p role="status" className="text-sm">
          {t(notice)}
        </p>
      )}
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={() => void query.refetch()}
        empty={query.isSuccess && !price?.rates.length}
      />
      {query.isSuccess && (
        <Table aria-label={`${modelName} · ${t('title')}`}>
          <thead>
            <tr>
              {['metric', 'tier', 'unit', 'amount', 'maintenance', 'status', 'actions'].map(
                (key) => (
                  <th key={key}>{t(key)}</th>
                ),
              )}
            </tr>
          </thead>
          <tbody>
            {price?.rates.map((rate) => (
              <tr key={rate.id}>
                <td>{t(rate.metric)}</td>
                <td>
                  {t(rate.tier)}
                  {rate.tier === 'long_context' && ` > ${price.context_threshold}`}
                </td>
                <td>{t('unitLabel')}</td>
                <td className="text-right tabular-nums">
                  {rate.amount} {rate.currency}
                </td>
                <td>
                  <Badge variant="outline">{t('custom')}</Badge>
                </td>
                <td>
                  <Badge variant={rate.enabled ? 'default' : 'outline'}>
                    {t(rate.enabled ? 'enabled' : 'disabled')}
                  </Badge>
                </td>
                <td>
                  {access.can('prices.write') && (
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-label={t('editLabel', { metric: t(rate.metric), tier: t(rate.tier) })}
                      onClick={() => open(rate)}
                    >
                      {t('edit')}
                    </Button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <Dialog
        open={!!editor}
        onOpenChange={(open) => {
          if (!open) {
            setEditor(null)
            mutation.reset()
            setValidation('')
            setNotice('')
          }
        }}
        busy={mutation.isPending}
        title={t(editor?.rate.id ? 'edit' : 'add')}
        description={t('editorDescription')}
      >
        {editor && (
          <form className="space-y-4" onSubmit={save}>
            <fieldset
              className="space-y-4"
              disabled={mutation.isPending || !access.can('prices.write')}
            >
              <FormField label={t('model')}>
                <Input value={modelName} disabled />
              </FormField>
              <FormField label={t('metric')}>
                <select
                  className={selectClass}
                  value={editor.rate.metric}
                  disabled={!!editor.rate.id}
                  onChange={(event) =>
                    change({ metric: event.target.value as PriceRate['metric'] })
                  }
                >
                  {metrics.map((metric) => (
                    <option key={metric} value={metric}>
                      {t(metric)}
                    </option>
                  ))}
                </select>
              </FormField>
              <FormField label={t('tier')}>
                <select
                  className={selectClass}
                  value={editor.rate.tier}
                  disabled={!!editor.rate.id}
                  onChange={(event) => change({ tier: event.target.value as PriceRate['tier'] })}
                >
                  {['base', 'long_context'].map((tier) => (
                    <option key={tier} value={tier}>
                      {t(tier)}
                    </option>
                  ))}
                </select>
              </FormField>
              <FormField label={t('threshold')}>
                <select
                  className={selectClass}
                  value={editor.threshold}
                  onChange={(event) =>
                    setEditor({
                      ...editor,
                      threshold: Number(event.target.value) as Editor['threshold'],
                    })
                  }
                >
                  <option value={0}>{t('noThreshold')}</option>
                  <option value={128000}>128000</option>
                  <option value={200000}>200000</option>
                </select>
              </FormField>
              <p className="text-xs text-muted-foreground">{t('thresholdHint')}</p>
              <FormField label={t('currency')}>
                <select
                  className={selectClass}
                  value={editor.rate.currency}
                  onChange={(event) =>
                    change({ currency: event.target.value as PriceRate['currency'] })
                  }
                >
                  {currencies.map((currency) => (
                    <option key={currency}>{currency}</option>
                  ))}
                </select>
              </FormField>
              <FormField label={`${t('amount')} · ${t('unitLabel')}`}>
                <Input
                  value={editor.rate.amount}
                  inputMode="decimal"
                  required
                  onChange={(event) => change({ amount: event.target.value })}
                />
              </FormField>
              <div className="flex items-center justify-between text-sm">
                <span>{t('enable')}</span>
                <Switch
                  aria-label={t('enable')}
                  checked={editor.rate.enabled}
                  onCheckedChange={(enabled) => change({ enabled })}
                />
              </div>
            </fieldset>
            {validation && (
              <p role="alert" className="text-sm text-destructive">
                {t(validation)}
              </p>
            )}
            {stale ? (
              <div className="space-y-3">
                <p role="alert">{t('stale')}</p>
                <Button variant="outline" disabled={query.isFetching} onClick={() => void reload()}>
                  {t('reload')}
                </Button>
              </div>
            ) : status === 422 ? (
              <p role="alert">{t('validation')}</p>
            ) : (
              <ErrorNotice error={mutation.error} />
            )}
            <div className="flex justify-end gap-2">
              <Button
                variant="outline"
                disabled={mutation.isPending}
                onClick={() => {
                  setEditor(null)
                  mutation.reset()
                }}
              >
                {t('cancel')}
              </Button>
              <SaveButton
                pending={mutation.isPending}
                disabled={stale || !access.can('prices.write')}
              >
                {t('save')}
              </SaveButton>
            </div>
          </form>
        )}
      </Dialog>
    </section>
  )
}
