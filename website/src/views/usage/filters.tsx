import { useId, useState, type ReactNode, type FormEvent, type ChangeEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { FormField } from '@/components/app/CatalogUI'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import type { UsageFilters, UsageGroup } from '@/types/usage'
import { defaultUsageFilters, readUsageFilters } from './filter-state'

export type UsageFilterDraft = Record<string, string>

export default function UsageFiltersForm({
  admin,
  team = false,
  sourceControl,
  action,
  models,
  keys,
  onApply,
  draft,
  onDraft,
}: {
  admin: boolean
  team?: boolean
  sourceControl?: ReactNode
  action?: ReactNode
  models: UsageGroup[]
  keys: UsageGroup[]
  onApply: (filters: UsageFilters) => void
  draft: UsageFilterDraft
  onDraft: (draft: UsageFilterDraft) => void
}) {
  const { t } = useTranslation('usage')
  const id = useId()
  const period = draft.period ?? 'month'
  const granularity = draft.granularity ?? 'auto'
  const [validation, setValidation] = useState('')
  function field(name: string) {
    return {
      value: draft[name] ?? '',
      onChange: (event: ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
        onDraft({ ...draft, [name]: event.target.value }),
    }
  }
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
      onInput={(event) => {
        const input = event.target
        if (input instanceof HTMLInputElement && input.name && input.name !== 'compare')
          onDraft({ ...draft, [input.name]: input.value })
      }}
      onReset={(event) => {
        event.preventDefault()
        onDraft({ period: 'month', granularity: 'auto', timezone: 'UTC' })
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
              onDraft({ ...draft, period: e.target.value, granularity: 'auto' })
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
            onChange={(e) => onDraft({ ...draft, granularity: e.target.value })}
            className={selectClass}
          >
            {['auto', 'hour', 'day', 'week', 'month'].map((g) => (
              <option key={g} value={g}>
                {t(g === 'month' ? 'monthGrain' : g)}
              </option>
            ))}
          </select>
        </FormField>
        {sourceControl}
        <FormField label={t('timezone')}>
          <Input name="timezone" {...field('timezone')} list={`${id}-zones`} className="w-44" />
        </FormField>
        <datalist id={`${id}-zones`}>
          {['UTC', 'Asia/Shanghai', 'America/New_York', 'Europe/London'].map((zone) => (
            <option key={zone} value={zone} />
          ))}
        </datalist>
        {!team && (
          <>
            <FormField label={t('key')}>
              <Input
                name="key_id"
                {...field('key_id')}
                placeholder={t('all')}
                list={`${id}-keys`}
                className="w-44"
              />
            </FormField>
            <datalist id={`${id}-keys`}>
              {keys
                .filter((group) => group.id)
                .map((group) => (
                  <option key={group.id} value={group.id} />
                ))}
            </datalist>
          </>
        )}
        <FormField label={t('model')}>
          <Input
            name="model_id"
            {...field('model_id')}
            placeholder={t('all')}
            list={`${id}-models`}
            className="w-44"
          />
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
          <Switch
            name="compare"
            aria-label={t('compare')}
            checked={draft.compare === 'on'}
            onCheckedChange={(checked) => onDraft({ ...draft, compare: checked ? 'on' : '' })}
          />
          {t('compare')}
        </label>
        <Button type="submit">{t('apply')}</Button>
        <Button variant="ghost" type="reset">
          {t('reset')}
        </Button>
        {action}
      </div>
      {period === 'custom' && (
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label={t('from')}>
            <Input name="from" {...field('from')} placeholder="2026-09-01T00:00:00Z" required />
          </FormField>
          <FormField label={t('to')}>
            <Input name="to" {...field('to')} placeholder="2026-09-02T00:00:00Z" required />
          </FormField>
        </div>
      )}
      <details className="text-sm">
        <summary className="w-fit cursor-pointer text-muted-foreground">{t('advanced')}</summary>
        <div className="mt-3 flex flex-wrap items-end gap-3 [&_label]:w-44 [&_label]:text-xs [&_input]:h-9">
          <FormField label={t('status')}>
            <select className={selectClass} name="status" {...field('status')}>
              <option value="">{t('all')}</option>
              {['success', 'error', 'canceled'].map((s) => (
                <option key={s} value={s}>
                  {t(s)}
                </option>
              ))}
            </select>
          </FormField>
          <FormField label={t('stream')}>
            <select className={selectClass} name="stream" {...field('stream')}>
              <option value="">{t('all')}</option>
              <option value="true">{t('streaming')}</option>
              <option value="false">{t('ordinary')}</option>
            </select>
          </FormField>
          <FormField label={t('protocol')}>
            <select className={selectClass} name="protocol" {...field('protocol')}>
              <option value="">{t('all')}</option>
              <option value="openai_chat">{t('chat')}</option>
              <option value="openai_responses">{t('responses')}</option>
              <option value="anthropic_messages">{t('messages')}</option>
              <option value="gemini_generate_content">{t('gemini')}</option>
            </select>
          </FormField>
          {admin &&
            (
              [
                ['user_id', 'user'],
                ['project_id', 'project'],
                ['team_id', 'team'],
                ['provider_id', 'provider'],
                ['connection_id', 'connection'],
                ['provider_model_id', 'providerModel'],
              ] as const
            ).map(([name, label]) => (
              <FormField label={t(label)} key={name}>
                <Input name={name} {...field(name)} placeholder={t('optional')} />
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
