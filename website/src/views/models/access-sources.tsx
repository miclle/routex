import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Menu, MenuItem } from '@/components/ui/menu'
import type { ModelAccessSource } from '@/types/model-catalog'
import { teamInvocationSupported } from './catalogue-metadata'
import { modelSourceKey, orderedModelSources } from './catalogue-metadata'

export default function ModelAccessSources({
  name,
  sources,
  selectedSource = 'all',
  expanded = false,
}: {
  name: string
  sources: ModelAccessSource[]
  selectedSource?: string
  expanded?: boolean
}) {
  const { t } = useTranslation('catalog')
  const ordered = orderedModelSources(sources, selectedSource)
  const label = (source: ModelAccessSource) =>
    source.type === 'personal' ? t('memberModels.personalGrant') : source.team_name
  const chip = (source: ModelAccessSource) => (
    <Badge
      key={modelSourceKey(source)}
      variant={source.type === 'personal' ? 'secondary' : 'outline'}
    >
      {label(source)}
      {expanded && source.type === 'team' && (
        <span className="text-muted-foreground">
          {' '}
          —{' '}
          {t(
            teamInvocationSupported(source)
              ? 'memberModels.teamNativeAvailable'
              : 'memberModels.teamInvocationUnsupported',
          )}
        </span>
      )}
    </Badge>
  )
  if (!ordered.length)
    return <span className="text-sm text-muted-foreground">{t('memberModels.noAccessSource')}</span>
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {(expanded ? ordered : ordered.slice(0, 2)).map(chip)}
      {!expanded && ordered.length > 2 && (
        <Menu
          trigger={`+${ordered.length - 2}`}
          label={t('memberModels.moreSources', { name, count: ordered.length - 2 })}
          side="bottom"
          align="end"
          triggerClassName="h-6 w-auto px-2 text-xs"
        >
          <p className="px-3 py-2 text-xs font-semibold">{t('memberModels.allSources')}</p>
          {ordered.map((source) => (
            <MenuItem key={modelSourceKey(source)} disabled onClick={() => {}}>
              <span className="flex flex-col gap-1">
                <span>{label(source)}</span>
                {source.type === 'team' && (
                  <span className="text-xs">
                    {t(
                      teamInvocationSupported(source)
                        ? 'memberModels.teamNativeAvailable'
                        : 'memberModels.teamInvocationUnsupported',
                    )}
                  </span>
                )}
              </span>
            </MenuItem>
          ))}
        </Menu>
      )}
    </div>
  )
}
