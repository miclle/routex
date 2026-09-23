import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import type { Member } from '@/types/governance'
import type { OffboardingResource } from '@/types/offboarding'

export function AssignmentFields({
  resource,
  team = false,
  choices,
  onChoices,
  additions,
  onAdditions,
  candidates,
  disabled,
}: {
  resource: OffboardingResource
  team?: boolean
  choices: { id: string; name: string }[]
  onChoices: (choices: { id: string; name: string }[]) => void
  additions: string[]
  onAdditions: (ids: string[]) => void
  candidates: Member[]
  disabled: boolean
}) {
  const { t } = useTranslation('offboarding')
  return (
    <fieldset disabled={disabled} className="space-y-3 rounded-lg border p-4">
      <legend className="px-1 text-sm font-medium">
        {t(team ? 'teamSuccessor' : 'projectSuccessor', { name: resource.name })}
        {resource.requires_successor ? ' *' : ''}
      </legend>
      <p className="text-xs text-muted-foreground">
        {t(resource.requires_successor ? 'required' : 'continuity')}
      </p>
      {choices.length > 0 && (
        <div className="flex flex-wrap gap-2" aria-label={t('chosen')}>
          {choices.map((person) => (
            <Button
              key={person.id}
              size="sm"
              variant="secondary"
              aria-label={t('remove', { name: person.name })}
              onClick={() => {
                onChoices(choices.filter((p) => p.id !== person.id))
                onAdditions(additions.filter((id) => id !== person.id))
              }}
            >
              {person.name} ×
            </Button>
          ))}
        </div>
      )}
      <div className="flex max-h-36 flex-wrap gap-2 overflow-auto">
        {candidates.length === 0 && (
          <p className="text-sm text-muted-foreground">{t('noCandidates')}</p>
        )}
        {candidates.map((person) => (
          <Button
            key={person.id}
            size="sm"
            variant="outline"
            aria-pressed={choices.some((p) => p.id === person.id)}
            onClick={() =>
              onChoices(
                choices.some((p) => p.id === person.id)
                  ? choices.filter((p) => p.id !== person.id)
                  : [...choices, { id: person.id, name: person.name }],
              )
            }
          >
            {person.name}
          </Button>
        ))}
      </div>
      {team &&
        choices
          .filter(
            (person) =>
              !resource.people.some(
                (p) => p.user_id === person.id && !p.disabled && p.status === 'active',
              ),
          )
          .map((person) => (
            <label key={person.id} className="flex items-center gap-3 text-sm">
              <Switch
                disabled={disabled}
                checked={additions.includes(person.id)}
                onCheckedChange={(checked) =>
                  onAdditions(
                    checked
                      ? [...additions, person.id]
                      : additions.filter((id) => id !== person.id),
                  )
                }
              />
              <span>{t('addMember', { name: person.name })}</span>
            </label>
          ))}
    </fieldset>
  )
}
