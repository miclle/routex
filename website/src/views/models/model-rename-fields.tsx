import { useTranslation } from 'react-i18next'
import { FormField } from '@/components/app/CatalogUI'
import { Input } from '@/components/ui/input'

import { modelCompatibilityDeadline, type ModelRenameDraft } from './model-rename-draft'

export default function ModelRenameFields({
  oldName,
  draft,
  onChange,
}: {
  oldName: string
  draft: ModelRenameDraft
  onChange: (draft: ModelRenameDraft) => void
}) {
  const { t, i18n } = useTranslation('catalog')
  const date = new Date(draft.expiresAt).toLocaleString(
    i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
  )
  return (
    <>
      <p role="note" className="rounded-md border bg-muted/40 p-3 text-sm">
        {draft.keepOldName
          ? t('adminModels.compatibilityWarning', { name: oldName })
          : t('adminModels.immediateStopWarning', { name: oldName })}
      </p>
      <FormField label={t('common.modelName')}>
        <Input
          name="name"
          required
          maxLength={200}
          value={draft.name}
          onChange={(event) => onChange({ ...draft, name: event.target.value })}
        />
      </FormField>
      <label className="flex items-center gap-2 text-sm">
        <Input
          type="checkbox"
          className="size-4"
          checked={draft.keepOldName}
          onChange={(event) => onChange({ ...draft, keepOldName: event.target.checked })}
        />
        {t('adminModels.keepOldName')}
      </label>
      {draft.keepOldName && (
        <FormField label={t('adminModels.compatibilityPeriod')}>
          <select
            value={draft.days}
            onChange={(event) => {
              const days = Number(event.target.value)
              if (days !== 7 && days !== 30 && days !== 90) return
              onChange({ ...draft, days, expiresAt: modelCompatibilityDeadline(days) })
            }}
            className="h-11 w-full rounded-md border bg-background px-3 text-sm"
          >
            {([7, 30, 90] as const).map((days) => (
              <option key={days} value={days}>
                {t('adminModels.compatibilityDays', { count: days })}
              </option>
            ))}
          </select>
          <input name="alias_expires_at" type="hidden" value={draft.expiresAt} />
          <p className="text-xs text-muted-foreground">
            {t('adminModels.compatibilityUntil', { name: oldName, date })}
          </p>
        </FormField>
      )}
    </>
  )
}
