import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getResourceCandidates } from '@/api/resources'
import { QueryState } from '@/components/app/CatalogUI'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
export function ResourceSection({
  title,
  action,
  children,
  padded = true,
}: {
  title: string
  action?: ReactNode
  children: ReactNode
  padded?: boolean
}) {
  useTranslation()

  return (
    <section className="overflow-hidden rounded-lg border">
      <header className="flex items-center justify-between gap-4 border-b px-4 py-3">
        <h3 className="text-sm font-semibold">{title}</h3>
        {action}
      </header>
      <div className={padded ? 'p-4' : undefined}>{children}</div>
    </section>
  )
}
export function CandidatePicker({
  path,
  selected,
  onChange,
  label,
  disabled = false,
  exclude = [],
}: {
  path: string
  selected: string[]
  onChange: (ids: string[]) => void
  label: string
  disabled?: boolean
  exclude?: string[]
}) {
  useTranslation()

  const [q, setQ] = useState('')
  const candidates = useQuery({
    queryKey: ['resource-candidates', path, q],
    queryFn: ({ signal }) => getResourceCandidates(path, q, signal),
  })
  return (
    <fieldset disabled={disabled} className="space-y-3">
      <legend className="mb-2 text-sm font-medium">{label}</legend>
      <Input
        aria-label={t('search_value_9f660', { v0: label })}
        placeholder={t('search_name_or_email_64826')}
        value={q}
        onChange={(event) => setQ(event.target.value)}
      />
      <QueryState
        pending={candidates.isPending}
        error={candidates.error}
        retry={() => void candidates.refetch()}
        empty={candidates.data?.filter((item) => !exclude.includes(item.id)).length === 0}
      />
      <div className="max-h-64 space-y-2 overflow-y-auto rounded-md border p-3">
        {candidates.data
          ?.filter((candidate) => !exclude.includes(candidate.id))
          .map((candidate) => (
            <label className="flex items-center gap-3 text-sm" key={candidate.id}>
              <input
                type="checkbox"
                checked={selected.includes(candidate.id)}
                onChange={(event) =>
                  onChange(
                    event.target.checked
                      ? [...selected, candidate.id]
                      : selected.filter((id) => id !== candidate.id),
                  )
                }
              />
              <span>
                {candidate.name}
                {candidate.email && (
                  <span className="ml-2 text-muted-foreground">{candidate.email}</span>
                )}
              </span>
            </label>
          ))}
      </div>
      {selected.length > 0 && (
        <div className="flex flex-wrap gap-2" aria-label={t('current_selection_7f06e')}>
          {selected.map((id) => (
            <Button
              key={id}
              type="button"
              size="sm"
              variant="secondary"
              onClick={() => onChange(selected.filter((item) => item !== id))}
              aria-label={t('remove_selection_value_90b0c', {
                v0: candidates.data?.find((item) => item.id === id)?.name ?? id,
              })}
            >
              {candidates.data?.find((item) => item.id === id)?.name ?? id} ×
            </Button>
          ))}
        </div>
      )}
      <p className="text-xs text-muted-foreground">
        {t('only_available_candidates_are_listed_existing_selections_remain_0f47f')}
      </p>
    </fieldset>
  )
}
