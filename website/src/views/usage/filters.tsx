import { useId, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { FormField } from '@/components/app/CatalogUI'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import type { UsageFilters, UsageGroup } from '@/types/usage'
import { defaultUsageFilters, readUsageFilters } from './filter-state'

export default function UsageFiltersForm({
  admin,
  models,
  keys,
  onApply,
}: {
  admin: boolean
  models: UsageGroup[]
  keys: UsageGroup[]
  onApply: (filters: UsageFilters) => void
}) {
  const { t } = useTranslation('usage')
  const id = useId()
  const [period, setPeriod] = useState('month')
  const [granularity, setGranularity] = useState('auto')
  const [validation, setValidation] = useState('')
  function apply(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const next = readUsageFilters(new FormData(event.currentTarget), admin)
    if (typeof next === 'string') {
      setValidation(next)
      return
    }
    setValidation('')
    onApply(next)
  }
  const selectClass = 'h-9 w-full rounded-md border bg-background px-2 text-sm'
  return (
    <form
      onSubmit={apply}
      onReset={() => {
        setPeriod('month')
        setGranularity('auto')
        setValidation('')
        onApply({ ...defaultUsageFilters })
      }}
      aria-label={t('filters')}
      className="space-y-3"
    >
      <div className="flex flex-wrap items-end gap-2 [&_label]:min-w-36 [&_label]:text-xs [&_input]:h-9">
        <FormField label={t('period')}>
          <select
            name="period"
            value={period}
            onChange={(e) => {
              setPeriod(e.target.value)
              setGranularity('auto')
            }}
            className={selectClass}
          >
            {['today', '24h', '7d', 'month', '90d', 'year', 'custom'].map((p) => (
              <option key={p} value={p}>
                {t(p)}
              </option>
            ))}
          </select>
        </FormField>
        <FormField label={t('granularity')}>
          <select
            name="granularity"
            value={granularity}
            onChange={(e) => setGranularity(e.target.value)}
            className={selectClass}
          >
            {['auto', 'hour', 'day', 'week', 'month'].map((g) => (
              <option key={g} value={g}>
                {t(g === 'month' ? 'monthGrain' : g)}
              </option>
            ))}
          </select>
        </FormField>
        <FormField label={t('timezone')}>
          <Input name="timezone" defaultValue="UTC" list={`${id}-zones`} className="w-44" />
        </FormField>
        <datalist id={`${id}-zones`}>
          {['UTC', 'Asia/Shanghai', 'America/New_York', 'Europe/London'].map((zone) => (
            <option key={zone} value={zone} />
          ))}
        </datalist>
        <FormField label={t('key')}>
          <Input name="key_id" placeholder={t('all')} list={`${id}-keys`} className="w-44" />
        </FormField>
        <datalist id={`${id}-keys`}>
          {keys
            .filter((group) => group.id)
            .map((group) => (
              <option key={group.id} value={group.id} />
            ))}
        </datalist>
        <FormField label={t('model')}>
          <Input name="model_id" placeholder={t('all')} list={`${id}-models`} className="w-44" />
        </FormField>
        <datalist id={`${id}-models`}>
          {models
            .filter((group) => group.id)
            .map((group) => (
              <option key={group.id} value={group.id}>
                {group.name ?? group.id}
              </option>
            ))}
        </datalist>
        <label className="flex h-9 items-center gap-2">
          <Switch name="compare" aria-label={t('compare')} />
          {t('compare')}
        </label>
        <Button type="submit">{t('apply')}</Button>
        <Button variant="ghost" type="reset">
          {t('reset')}
        </Button>
      </div>
      {period === 'custom' && (
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label={t('from')}>
            <Input name="from" placeholder="2026-09-01T00:00:00Z" required />
          </FormField>
          <FormField label={t('to')}>
            <Input name="to" placeholder="2026-09-02T00:00:00Z" required />
          </FormField>
        </div>
      )}
      <details className="text-sm">
        <summary className="w-fit cursor-pointer text-muted-foreground">{t('advanced')}</summary>
        <div className="mt-3 flex flex-wrap items-end gap-3 [&_label]:w-44 [&_label]:text-xs [&_input]:h-9">
          <FormField label={t('status')}>
            <select className={selectClass} name="status">
              <option value="">{t('all')}</option>
              {['success', 'error', 'canceled'].map((s) => (
                <option key={s} value={s}>
                  {t(s)}
                </option>
              ))}
            </select>
          </FormField>
          <FormField label={t('stream')}>
            <select className={selectClass} name="stream">
              <option value="">{t('all')}</option>
              <option value="true">{t('streaming')}</option>
              <option value="false">{t('ordinary')}</option>
            </select>
          </FormField>
          <FormField label={t('protocol')}>
            <select className={selectClass} name="protocol">
              <option value="">{t('all')}</option>
              <option value="openai_chat">{t('chat')}</option>
              <option value="openai_responses">{t('responses')}</option>
            </select>
          </FormField>
          {admin &&
            (
              [
                ['user_id', 'user'],
                ['project_id', 'project'],
                ['connection_id', 'connection'],
                ['provider_model_id', 'providerModel'],
              ] as const
            ).map(([name, label]) => (
              <FormField label={t(label)} key={name}>
                <Input name={name} placeholder={t('optional')} />
              </FormField>
            ))}
        </div>
      </details>
      {validation && (
        <p role="alert" className="text-sm text-destructive">
          {t(validation)}
        </p>
      )}
    </form>
  )
}
