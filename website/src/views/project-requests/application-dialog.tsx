import { useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  createProjectRequest,
  projectRequestCandidates,
  projectRequestError,
} from '@/api/project-requests'
import { useSession } from '@/hooks/use-auth'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import type { ResourceRecord } from '@/types/resources'
import type { ProjectRequestCandidate } from '@/types/project-requests'

export default function ApplicationDialog({
  project,
  onClose,
  onSuccess,
  onRefresh,
}: {
  project: ResourceRecord
  onClose: () => void
  onSuccess: () => void
  onRefresh: () => void
}) {
  const { t } = useTranslation('projectRequests')
  const session = useSession()
  const cache = useQueryClient()
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<ProjectRequestCandidate[]>([])
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [uncertain, setUncertain] = useState(false)
  const running = useRef(false)
  const attempt = useRef<{ signature: string; id: string } | null>(null)
  const candidates = useQuery({
    queryKey: ['project-request-candidates', project.id, search],
    queryFn: ({ signal }) => projectRequestCandidates(project.id, search, signal),
    enabled: project.status === 'active',
    retry: false,
  })
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (running.current || !session.data || project.status !== 'active') return
    const modelIds = selected.map((m) => m.id).sort()
    const trimmed = reason.trim()
    if (!modelIds.length || !trimmed) {
      setError('required')
      return
    }
    const signature = JSON.stringify({ model_ids: modelIds, reason: trimmed })
    if (!attempt.current || attempt.current.signature !== signature)
      attempt.current = { signature, id: crypto.randomUUID() }
    running.current = true
    setBusy(true)
    setError(null)
    try {
      await createProjectRequest(
        project.id,
        {
          request_id: attempt.current.id,
          kind: 'MODEL_ACCESS',
          model_ids: modelIds,
          reason: trimmed,
        },
        session.data.csrf_token,
      )
      onSuccess()
    } catch (caught) {
      const key = projectRequestError(caught)
      setError(key)
      if (key === 'failed' || key === 'unavailable') setUncertain(true)
      if (key === 'denied') void cache.invalidateQueries({ queryKey: ['permissions'] })
    } finally {
      running.current = false
      setBusy(false)
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      title={t('applyTitle')}
      description={t('applyDescription')}
      busy={busy}
      width={660}
    >
      <form className="space-y-5" onSubmit={(event) => void submit(event)}>
        <div className="space-y-2 rounded-lg bg-muted p-4 text-sm">
          <p className="font-medium">{t('baseline')}</p>
          <p className="break-all font-mono text-xs">{project.model_ids.join(', ') || t('none')}</p>
          <p className="text-muted-foreground">{t('baselineHelp')}</p>
        </div>
        <FormField label={t('search')}>
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            disabled={busy || uncertain}
            maxLength={100}
          />
        </FormField>
        <QueryState
          pending={candidates.isPending}
          error={candidates.error}
          retry={() => void candidates.refetch()}
        />
        <p className="text-xs text-muted-foreground">{t('searchHint')}</p>
        {selected.length > 0 && (
          <div className="flex flex-wrap gap-2" aria-label={t('selected')}>
            {selected.map((model) => (
              <Button
                key={model.id}
                size="sm"
                variant="secondary"
                disabled={busy || uncertain}
                aria-label={t('remove', { name: model.name })}
                onClick={() => setSelected(selected.filter((m) => m.id !== model.id))}
              >
                {model.name} ×
              </Button>
            ))}
          </div>
        )}
        <div className="flex max-h-52 flex-wrap gap-2 overflow-auto" aria-label={t('models')}>
          {candidates.data
            ?.filter((m) => !project.model_ids.includes(m.id))
            .map((model) => (
              <Button
                key={model.id}
                variant="outline"
                size="sm"
                disabled={busy || uncertain}
                aria-pressed={selected.some((m) => m.id === model.id)}
                onClick={() =>
                  setSelected(
                    selected.some((m) => m.id === model.id)
                      ? selected.filter((m) => m.id !== model.id)
                      : [...selected, model],
                  )
                }
              >
                {model.name}
              </Button>
            ))}
        </div>
        {!candidates.isPending && !candidates.isError && candidates.data?.length === 0 && (
          <p className="text-sm text-muted-foreground">{t('noCandidates')}</p>
        )}
        <FormField label={t('reason')}>
          <Textarea
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            placeholder={t('reasonPlaceholder')}
            maxLength={2000}
            required
            readOnly={uncertain}
            disabled={busy}
          />
        </FormField>
        {error && (
          <div
            role="alert"
            className="space-y-3 rounded-md border border-destructive/30 p-3 text-sm text-destructive"
          >
            <p>{t(error)}</p>
            {error === 'conflict' && (
              <Button variant="outline" onClick={onRefresh}>
                {t('refresh')}
              </Button>
            )}
          </div>
        )}
        <div className="flex justify-end gap-3">
          <Button variant="outline" disabled={busy} onClick={onClose}>
            {t('cancel')}
          </Button>
          <Button
            type="submit"
            disabled={
              busy ||
              project.status !== 'active' ||
              candidates.isPending ||
              candidates.isError ||
              error === 'conflict' ||
              error === 'denied' ||
              !selected.length ||
              !reason.trim()
            }
          >
            {t(busy ? 'sending' : uncertain ? 'retry' : 'send')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
