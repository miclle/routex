import { useLayoutEffect, useRef, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { ErrorNotice, SaveButton } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { protocolLabel } from '@/lib/protocols'
import type { Model, Provider } from '@/types/catalog'
import RoutePrices from './route-prices'

export default function RoutingWeights({
  model,
  draft,
  reviewRequired,
  onDraftChange,
  onReviewCurrent,
  actor,
  generation,
  providers,
  canWrite,
  canAdd = canWrite,
  pricesReadable,
  pending,
  error,
  onSave,
  onAddBinding,
  onGrants,
  refreshDetail,
}: {
  model: Model
  draft: Record<string, string>
  reviewRequired: boolean
  onDraftChange: (bindingId: string, value: string) => void
  onReviewCurrent: () => void
  actor: string
  generation: number
  providers: Provider[] | undefined
  canWrite: boolean
  canAdd?: boolean
  pricesReadable: boolean
  pending: boolean
  error: unknown
  onSave: (weights: { binding_id: string; weight: number }[]) => void
  onAddBinding: (protocol?: string) => void
  onGrants: () => void
  refreshDetail: () => void
}) {
  const { t } = useTranslation('catalog')
  const firstWeight = useRef<HTMLInputElement>(null)
  const focusReviewed = useRef(false)
  useLayoutEffect(() => {
    if (focusReviewed.current && !reviewRequired) {
      focusReviewed.current = false
      if (!pending && canWrite && firstWeight.current?.isConnected) firstWeight.current.focus()
    }
  }, [reviewRequired, pending, canWrite])
  const groups = [...new Set(model.bindings.map((binding) => binding.protocol))].map((protocol) => {
    const bindings = model.bindings.filter((binding) => binding.protocol === protocol)
    const values = bindings.map((binding) => {
      const value = draft[binding.id]
      const weight = value === '' ? NaN : Number(value)
      return Number.isInteger(weight) && weight >= 0 && weight <= 100 ? weight : null
    })
    const validValues = values.every((value) => value !== null)
    const total = validValues ? values.reduce<number>((sum, value) => sum + value!, 0) : null
    const positiveUnavailable = bindings.some(
      (binding, index) => values[index] !== null && values[index]! > 0 && !binding.ready,
    )
    return {
      protocol,
      bindings,
      total,
      positiveUnavailable,
      valid: total === 100,
    }
  })
  const valid = groups.length > 0 && groups.every((group) => group.valid)
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canWrite || pending || reviewRequired || !valid) return
    onSave(
      model.bindings.map((binding) => ({
        binding_id: binding.id,
        weight: Number(draft[binding.id]),
      })),
    )
  }
  return (
    <form aria-label={t('adminModels.weightsLabel')} className="space-y-4" onSubmit={submit}>
      {reviewRequired && (
        <div className="space-y-2">
          <p role="alert" className="text-sm text-destructive">
            {t('adminModels.weightsRoutesChanged')}
          </p>
          <Button
            type="button"
            variant="outline"
            disabled={pending || !canWrite}
            onClick={() => {
              focusReviewed.current = true
              onReviewCurrent()
            }}
          >
            {t('adminModels.weightsReviewCurrent')}
          </Button>
        </div>
      )}
      {groups.map((group) => {
        const protocol = protocolLabel(group.protocol)
        const feedback =
          group.total === null
            ? t('adminModels.weightsInvalid')
            : group.total < 100
              ? t('adminModels.weightsUnder', { total: group.total, difference: 100 - group.total })
              : group.total > 100
                ? t('adminModels.weightsOver', {
                    total: group.total,
                    difference: group.total - 100,
                  })
                : t('adminModels.weightsDraftTotal', { total: group.total })
        return (
          <section key={group.protocol} className="rounded-lg border">
            <h3 className="border-b px-6 py-4 font-semibold">
              {groups.length > 1
                ? t('adminModels.protocolRouting', { protocol })
                : t('adminModels.routing')}
            </h3>
            <Table
              aria-label={t('adminModels.protocolRoutesLabel', { name: model.name, protocol })}
            >
              <thead>
                <tr>
                  <th>{t('common.provider')}</th>
                  <th>{t('adminModels.providerModel')}</th>
                  <th>{t('routingCandidates.connection')}</th>
                  <th>{t('routingCandidates.verification')}</th>
                  <th>{t('routingCandidates.availability')}</th>
                  <th>{t('adminModels.inputBasePrice')}</th>
                  <th>{t('adminModels.outputBasePrice')}</th>
                  <th>{t('adminModels.weight')}</th>
                </tr>
              </thead>
              <tbody>
                {group.bindings.map((binding) => (
                  <tr key={binding.id}>
                    <td>
                      {binding.supply?.provider_name ??
                        providers?.find((provider) => provider.id === binding.provider_id)?.name ??
                        binding.provider_id}
                    </td>
                    <td>{binding.upstream_name}</td>
                    <td>{binding.supply?.connection_name ?? binding.connection_id}</td>
                    <td>
                      {t(
                        binding.supply
                          ? binding.supply.verification_covered
                            ? 'routingCandidates.covered'
                            : 'routingCandidates.uncovered'
                          : 'routingCandidates.unknownFact',
                      )}
                    </td>
                    <td>
                      {t(
                        binding.supply
                          ? binding.supply.configured_available
                            ? 'routingCandidates.available'
                            : 'routingCandidates.unavailable'
                          : 'routingCandidates.unknownFact',
                      )}
                    </td>
                    <RoutePrices
                      actor={actor}
                      modelID={model.id}
                      generation={generation}
                      binding={binding}
                      readable={pricesReadable}
                      refreshDetail={refreshDetail}
                    />
                    <td>
                      <div className="flex items-center gap-2">
                        <Input
                          aria-label={t('adminModels.weightLabel', { name: binding.upstream_name })}
                          name={binding.id}
                          type="number"
                          min={0}
                          max={100}
                          step={1}
                          required
                          ref={binding.id === model.bindings[0]?.id ? firstWeight : undefined}
                          value={draft[binding.id] ?? ''}
                          onValueChange={(value) => onDraftChange(binding.id, value)}
                          disabled={pending || !canWrite || reviewRequired}
                          className="w-24"
                        />
                        %
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
            <div className="space-y-1 p-4 text-sm">
              <Button
                type="button"
                variant="outline"
                disabled={pending || !canAdd}
                onClick={() => onAddBinding(group.protocol)}
                aria-label={t('routingCandidates.addProtocol', { protocol })}
              >
                {t('adminModels.addBinding')}
              </Button>
              <p
                aria-label={t('adminModels.weightsTotalLabel', { protocol })}
                aria-live="polite"
                className={group.valid ? 'text-muted-foreground' : 'text-destructive'}
              >
                {feedback}
              </p>
              {group.positiveUnavailable && (
                <p className="text-muted-foreground">{t('adminModels.weightsUnavailable')}</p>
              )}
            </div>
          </section>
        )
      })}
      <p className="text-xs text-muted-foreground">{t('adminModels.routePriceHelp')}</p>
      <div className="flex items-center justify-between gap-4">
        <Button
          type="button"
          disabled={pending || !canAdd}
          variant="outline"
          onClick={() => onAddBinding()}
        >
          {t('adminModels.addBinding')}
        </Button>
        <p className="text-sm text-muted-foreground">{t('adminModels.weightsConfiguredHelp')}</p>
      </div>
      <ErrorNotice error={error} />
      <div className="flex justify-end gap-3">
        <Button disabled={pending || !canWrite} variant="outline" onClick={onGrants}>
          {t('adminModels.grant')}
        </Button>
        <SaveButton pending={pending} disabled={!canWrite || reviewRequired || !valid}>
          {t('adminModels.saveWeights')}
        </SaveButton>
      </div>
    </form>
  )
}
