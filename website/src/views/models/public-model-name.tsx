import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Autocomplete } from '@/components/ui/autocomplete'
import { searchPublicModelNames } from '@/lib/public-model-references'

export function PublicModelName({
  label,
  value,
  excludedNames,
  disabled,
  onValueChange,
}: {
  label: string
  value: string
  excludedNames: readonly string[]
  disabled: boolean
  onValueChange: (value: string) => void
}) {
  const { t } = useTranslation('modelCreation')
  const descriptionId = useId()
  return (
    <div className="min-w-0 flex-1 space-y-1">
      <Autocomplete
        label={label}
        descriptionId={descriptionId}
        value={value}
        suggestions={searchPublicModelNames(value, excludedNames)}
        disabled={disabled}
        onValueChange={onValueChange}
      />
      <p id={descriptionId} className="text-xs text-muted-foreground">
        {t('publicNameSuggestions')}
      </p>
    </div>
  )
}
