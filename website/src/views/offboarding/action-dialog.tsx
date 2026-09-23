import { useRef, useState, type FormEvent } from 'react'
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getMembers } from '@/api/governance'
import {
  completeOffboarding,
  createOffboardingPlan,
  emergencyOffboarding,
  OffboardingError,
} from '@/api/offboarding'
import { useSession } from '@/hooks/use-auth'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import type {
  EmergencyOffboarding,
  OffboardingCase,
  OffboardingInventory,
  OffboardingPlan,
} from '@/types/offboarding'
import { AssignmentFields } from './assignments'
import { buildAssignments, needsEmergencySuccessor, type Choices } from './assignment-logic'

export default function ActionDialog({
  mode,
  inventory,
  selectedCase,
  onClose,
  onSuccess,
  onReview,
}: {
  mode: 'plan' | 'emergency' | 'complete'
  inventory: OffboardingInventory
  selectedCase?: OffboardingCase
  onClose: () => void
  onSuccess: (record: OffboardingCase) => void
  onReview: () => void
}) {
  const { t } = useTranslation('offboarding')
  const session = useSession()
  const cache = useQueryClient()
  const [busy, setBusy] = useState(false)
  const running = useRef(false)
  const [error, setError] = useState<string | null>(null)
  const [uncertain, setUncertain] = useState(false)
  const [query, setQuery] = useState('')
  const [projects, setProjects] = useState<Choices>({})
  const [teams, setTeams] = useState<Choices>({})
  const [additions, setAdditions] = useState<Record<string, string[]>>({})
  const attempt = useRef<{ signature: string; requestId: string } | null>(null)
  const members = useInfiniteQuery({
    queryKey: ['offboarding', 'successors', query],
    queryFn: ({ pageParam, signal }) =>
      getMembers({ q: query, status: 'active' }, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: mode !== 'complete',
    retry: false,
  })
  const candidates = (members.data?.pages.flatMap((p) => p.items) ?? []).filter(
    (p) => !p.disabled && p.id !== inventory.user_id,
  )
  const teamRows = inventory.teams.filter(
    (r) =>
      r.status !== 'archived' &&
      (mode !== 'emergency' || needsEmergencySuccessor(r, inventory.user_id)),
  )
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (running.current || !session.data) return
    const form = event.currentTarget
    const values = new FormData(form)
    setError(null)
    const assignments = buildAssignments(
      inventory,
      projects,
      teams,
      additions,
      mode === 'emergency',
    )
    if (mode !== 'complete' && !assignments) {
      setError('missingSuccessors')
      return
    }
    const date = new Date(String(values.get('planned_at') ?? ''))
    if (mode === 'plan' && Number.isNaN(date.valueOf())) {
      setError('invalidDate')
      return
    }
    const reason = String(values.get('reason') ?? '').trim()
    if (mode !== 'complete' && !reason) return
    const payload =
      mode === 'plan'
        ? {
            ...assignments!,
            inventory_version: inventory.inventory_version,
            planned_at: date.toISOString(),
            reason,
          }
        : { team_assignments: assignments?.team_assignments ?? [], reason }
    const signature = JSON.stringify(payload)
    if (!attempt.current || attempt.current.signature !== signature)
      attempt.current = { signature, requestId: crypto.randomUUID() }
    const requestId = attempt.current.requestId
    const password = String(values.get('current_password') ?? '')
    const passwordInput = form.elements.namedItem('current_password') as HTMLInputElement | null
    if (passwordInput) passwordInput.value = ''
    running.current = true
    setBusy(true)
    try {
      const record =
        mode === 'complete'
          ? await completeOffboarding(inventory.user_id, selectedCase!.id, session.data.csrf_token)
          : mode === 'plan'
            ? await createOffboardingPlan(
                inventory.user_id,
                { ...payload, request_id: requestId } as OffboardingPlan,
                session.data.csrf_token,
              )
            : await emergencyOffboarding(
                inventory.user_id,
                { ...payload, request_id: requestId } as EmergencyOffboarding,
                password,
                session.data.csrf_token,
              )
      // Password proofs are passed directly, never through query/mutation variables.
      onSuccess(record)
    } catch (caught) {
      const status = caught instanceof OffboardingError ? caught.status : 0
      setError(
        status === 409
          ? 'stale'
          : status === 503
            ? 'unavailable'
            : status === 403
              ? 'denied'
              : status === 401
                ? 'reauth'
                : 'failed',
      )
      if (status === 0 || status === 503) setUncertain(true)
      if (status === 401) void cache.invalidateQueries({ queryKey: ['auth', 'session'] })
      if (status === 403) void cache.invalidateQueries({ queryKey: ['permissions'] })
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
      title={t(
        mode === 'plan' ? 'planDialog' : mode === 'emergency' ? 'emergencyTitle' : 'completeTitle',
      )}
      description={t(
        mode === 'plan'
          ? 'planDescription'
          : mode === 'emergency'
            ? 'emergencyDescription'
            : 'completeDescription',
      )}
      busy={busy}
      width={680}
    >
      <form onSubmit={(event) => void submit(event)} className="space-y-5">
        {mode !== 'complete' && (
          <>
            <FormField label={t('reason')}>
              <Textarea
                name="reason"
                required
                maxLength={2000}
                disabled={busy}
                readOnly={uncertain}
              />
            </FormField>
            {mode === 'plan' && (
              <FormField label={t('date')}>
                <Input
                  name="planned_at"
                  type="datetime-local"
                  required
                  disabled={busy}
                  readOnly={uncertain}
                />
                <span className="block text-xs font-normal text-muted-foreground">
                  {t('dateHelp')}
                </span>
              </FormField>
            )}
            {mode === 'emergency' && (
              <p className="rounded-md bg-muted p-3 text-sm">{t('emergencyProject')}</p>
            )}
            <FormField label={t('search')}>
              <Input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                disabled={busy || uncertain}
              />
            </FormField>
            <QueryState
              pending={members.isPending}
              error={members.error}
              retry={() => void members.refetch()}
            />
            {members.hasNextPage && (
              <Button
                disabled={members.isFetchingNextPage || busy}
                variant="outline"
                onClick={() => void members.fetchNextPage()}
              >
                {t('more')}
              </Button>
            )}
            {mode === 'plan' &&
              inventory.projects
                .filter((r) => r.status !== 'archived')
                .map((resource) => (
                  <AssignmentFields
                    key={resource.id}
                    resource={resource}
                    choices={projects[resource.id] ?? []}
                    onChoices={(choices) => setProjects({ ...projects, [resource.id]: choices })}
                    additions={[]}
                    onAdditions={() => {}}
                    candidates={candidates}
                    disabled={busy || uncertain}
                  />
                ))}
            {teamRows.map((resource) => (
              <AssignmentFields
                key={resource.id}
                resource={resource}
                team
                choices={teams[resource.id] ?? []}
                onChoices={(choices) => setTeams({ ...teams, [resource.id]: choices })}
                additions={additions[resource.id] ?? []}
                onAdditions={(ids) => setAdditions({ ...additions, [resource.id]: ids })}
                candidates={candidates}
                disabled={busy || uncertain}
              />
            ))}
            {mode === 'emergency' &&
              inventory.teams.some(
                (r) => r.requires_successor && !needsEmergencySuccessor(r, inventory.user_id),
              ) && <p className="text-sm text-muted-foreground">{t('emergencyAuto')}</p>}
          </>
        )}
        {mode === 'complete' && selectedCase && (
          <div className="space-y-2 rounded-lg bg-muted p-4 text-sm">
            <p className="font-medium">{selectedCase.reason}</p>
            <p>{t('reviewedAssignments')}</p>
            {selectedCase.assignments.project_assignments.map((a) => (
              <p key={a.project_id}>
                {inventory.projects.find((r) => r.id === a.project_id)?.name ?? a.project_id}:{' '}
                {a.manager_user_ids.join(', ')}
              </p>
            ))}
            {selectedCase.assignments.team_assignments.map((a) => (
              <p key={a.team_id}>
                {inventory.teams.find((r) => r.id === a.team_id)?.name ?? a.team_id}:{' '}
                {a.owner_user_ids.join(', ')}
              </p>
            ))}
          </div>
        )}
        {mode === 'emergency' && (
          <FormField label={t('password')}>
            <Input
              name="current_password"
              type="password"
              autoComplete="current-password"
              required
              disabled={busy}
            />
          </FormField>
        )}
        {error && (
          <div
            role="alert"
            className="space-y-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm"
          >
            <p>{t(error)}</p>
            {error === 'stale' && (
              <Button variant="outline" disabled={busy} onClick={onReview}>
                {t('refreshReview')}
              </Button>
            )}
          </div>
        )}
        <div className="flex flex-wrap justify-end gap-3">
          <Button variant="outline" disabled={busy} onClick={onClose}>
            {t('cancel')}
          </Button>
          <Button
            type="submit"
            disabled={
              busy ||
              error === 'stale' ||
              error === 'denied' ||
              (mode !== 'complete' && (members.isPending || members.isError))
            }
            className={mode === 'plan' ? '' : 'bg-destructive text-white hover:bg-destructive/90'}
          >
            {t(
              busy
                ? 'working'
                : uncertain
                  ? 'retry'
                  : mode === 'plan'
                    ? 'prepare'
                    : mode === 'emergency'
                      ? 'emergencyConfirm'
                      : 'confirm',
            )}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
