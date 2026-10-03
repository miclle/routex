import { useTranslation } from 'react-i18next'
import { FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import type { UsageTeams } from '@/types/usage'
export default function TeamSelector({
  team,
  items,
  pending,
  error,
  ready,
  more,
  onChange,
  onRefresh,
  onMore,
}: {
  team: string
  items: UsageTeams['items']
  pending: boolean
  error: boolean
  ready: boolean
  more: boolean
  onChange: (value: string) => void
  onRefresh: () => void
  onMore: () => void
}) {
  const { t } = useTranslation('usage')
  const known = items.some((item) => item.id === team)
  return (
    <div className="space-y-1">
      <FormField label={t('sourceScope')}>
        <select
          aria-label={t('sourceScope')}
          value={team}
          className="h-9 w-48 rounded-md border bg-background px-2 text-sm"
          onChange={(event) => onChange(event.target.value)}
        >
          <option value="">{t('personalSource')}</option>
          {team && !known && <option value={team}>{team}</option>}
          {ready &&
            items.map((item) => (
              <option value={item.id} key={item.id}>
                {item.name}
              </option>
            ))}
        </select>
      </FormField>
      {pending && (
        <p role="status" className="text-xs text-muted-foreground">
          {t('teamsLoading')}
        </p>
      )}
      {error && (
        <p role="alert" className="text-xs text-destructive">
          {t('teamsUnavailable')}
        </p>
      )}
      {ready && !items.length && (
        <p role="status" className="text-xs text-muted-foreground">
          {t('teamsEmpty')}
        </p>
      )}
      <div className="flex gap-2">
        <Button type="button" size="sm" variant="ghost" disabled={pending} onClick={onRefresh}>
          {t('refreshTeams')}
        </Button>
        {more && (
          <Button type="button" size="sm" variant="ghost" disabled={pending} onClick={onMore}>
            {t('moreTeams')}
          </Button>
        )}
      </div>
    </div>
  )
}
