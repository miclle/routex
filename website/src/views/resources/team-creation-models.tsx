import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { MultiSelect, type MultiSelectOption } from '@/components/ui/multi-select'
import { FormField } from '@/components/app/CatalogUI'
import TeamCreationModelColumns from './team-creation-models-columns'
import {
  teamCreationModelLayout,
  type TeamCreationModelOption,
} from './team-creation-models-values'

export default function TeamCreationModels({
  authorized,
  options,
  selected,
  search,
  onSearchChange,
  onValueChange,
  disabled = false,
  footer,
}: {
  authorized: boolean
  options: readonly TeamCreationModelOption[]
  selected: readonly MultiSelectOption[]
  search: string
  onSearchChange: (value: string) => void
  onValueChange: (value: MultiSelectOption[]) => void
  disabled?: boolean
  footer?: ReactNode
}) {
  const { t } = useTranslation('resources')
  if (!authorized) return null
  const headers: [string, string, string] = [
    t('teamCreation.models.model'),
    t('teamCreation.models.provider'),
    t('teamCreation.models.protocol'),
  ]
  const layout = teamCreationModelLayout(options, headers)
  const byID = new Map(options.map((option) => [option.value, option]))
  return (
    <section className="team-creation-models space-y-5">
      <div className="space-y-1">
        <h2 className="text-base font-semibold">{t('teamCreation.models.title')}</h2>
        <p className="text-sm text-muted-foreground">{t('teamCreation.models.help')}</p>
      </div>
      <FormField label={t('teamCreation.models.label')}>
        <MultiSelect
          label={t('teamCreation.models.label')}
          options={[...options]}
          value={[...selected]}
          search={search}
          onSearchChange={onSearchChange}
          onValueChange={onValueChange}
          disabled={disabled}
          footer={footer}
          placeholder={t('teamCreation.models.placeholder')}
          clearLabel={t('teamCreation.models.clear')}
          removeLabel={(name) => t('teamCreation.models.remove', { name })}
          popupClassName="w-max min-w-90 max-w-[calc(100vw-2rem)] overflow-x-auto"
          popupHeader={
            <div className="py-1.5 pr-3 pl-9">
              <TeamCreationModelColumns values={headers} layout={layout} header />
            </div>
          }
          renderOption={(item) => {
            const option = byID.get(item.value)
            return (
              <TeamCreationModelColumns
                layout={layout}
                values={[
                  item.label,
                  option?.providerNames.join(', ') || t('listUnknown'),
                  option?.protocols.join(', ') || t('listUnknown'),
                ]}
              />
            )
          }}
        />
        <p className="text-sm text-muted-foreground">{t('teamCreation.models.emptyHelp')}</p>
      </FormField>
    </section>
  )
}
