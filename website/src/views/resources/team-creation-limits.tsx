import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { FormField } from '@/components/app/CatalogUI'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { type TeamCreationContext, type TeamCreationField } from '@/types/resources'
import { teamTokenMillions, type TeamLimitDrafts } from './team-creation-values'
export default function TeamCreationLimits({
  context,
  retainedDefaultsUnknown = false,
  drafts,
  reason,
  onChange,
  onReason,
  disabled,
}: {
  context: TeamCreationContext
  retainedDefaultsUnknown?: boolean
  drafts: TeamLimitDrafts
  reason: string
  onChange: (value: TeamLimitDrafts) => void
  onReason: (value: string) => void
  disabled: boolean
}) {
  useTranslation()
  function control(field: TeamCreationField) {
    const allowed = context.editable_fields.includes(field)
    const dirty = Object.hasOwn(drafts, field)
    const value = context.default_policy[field]
    const originalDefaultUnknown = retainedDefaultsUnknown && !dirty
    const currency = originalDefaultUnknown
      ? null
      : field === 'money_month' && !dirty && value !== null && value !== undefined
        ? (context.default_policy.currency ?? context.platform_currency)
        : context.platform_currency
    const preview =
      value == null ? '' : field.startsWith('tokens_') ? teamTokenMillions(value) : value
    return (
      <div key={field} className="min-w-0 space-y-2">
        {allowed && originalDefaultUnknown ? (
          <>
            <p className="text-sm font-medium">
              {t(`resources:teamCreation.${field}`, { currency: '' })}
            </p>
            <p className="text-sm text-muted-foreground">
              {t('resources:teamCreation.originalDefaultUnknown')}
            </p>
          </>
        ) : allowed ? (
          <FormField
            label={t(`resources:teamCreation.${field}`, {
              currency: currency ?? '',
            })}
          >
            <Input
              aria-label={t(`resources:teamCreation.${field}`, {
                currency: currency ?? '',
              })}
              inputMode="decimal"
              value={dirty ? drafts[field] : preview}
              placeholder={t('resources:teamCreation.notSet')}
              disabled={disabled}
              onChange={(event) => onChange({ ...drafts, [field]: event.target.value })}
            />
          </FormField>
        ) : (
          <>
            <p className="text-sm font-medium">
              {t(`resources:teamCreation.${field}`, { currency: '' })}
            </p>
            <p className="text-sm text-muted-foreground">
              {t('resources:teamCreation.hiddenDefault')}
            </p>
          </>
        )}
        {dirty && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={disabled}
            onClick={() => {
              const next = { ...drafts }
              delete next[field]
              onChange(next)
            }}
          >
            {t('resources:teamCreation.useDefault')}
          </Button>
        )}
      </div>
    )
  }
  return (
    <fieldset disabled={disabled} className="space-y-4">
      <h3 className="text-sm font-semibold">{t('resources:teamCreation.limits')}</h3>
      <p className="text-sm text-muted-foreground">{t('resources:teamCreation.limitHelp')}</p>
      <div className="grid gap-4 md:grid-cols-3">{control('money_month')}</div>
      <div className="grid gap-4 md:grid-cols-3">
        {(['tokens_5h', 'tokens_7d', 'tokens_month'] as const).map(control)}
      </div>
      <div className="grid gap-4 md:grid-cols-3">
        {(['rpm', 'tpm', 'concurrency'] as const).map(control)}
      </div>
      {Object.keys(drafts).length > 0 && (
        <FormField label={t('resources:teamCreation.reason')}>
          <Textarea
            value={reason}
            onChange={(event) => onReason(event.target.value)}
            disabled={disabled}
          />
        </FormField>
      )}
    </fieldset>
  )
}
