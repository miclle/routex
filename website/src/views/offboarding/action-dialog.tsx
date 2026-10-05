import { useEffect, useLayoutEffect, useRef, useState, type FormEvent } from 'react'
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getMembers } from '@/api/governance'
import {
  completeOffboarding,
  createOffboardingPlan,
  emergencyOffboarding,
  OffboardingError,
} from '@/api/offboarding'
import type { Session } from '@/types/auth'
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
import { freshQuery } from './read-authority'

type Mode = 'plan' | 'emergency' | 'complete'
type Intent = {
  mode: Mode
  actor: string
  target: string
  body: OffboardingPlan | EmergencyOffboarding | Record<string, never>
  record?: OffboardingCase
}
function validCase(record: OffboardingCase) {
  const ids = (value: unknown) =>
    Array.isArray(value) && value.every((id) => typeof id === 'string' && !!id)
  const date = (value: unknown) =>
    typeof value === 'string' && !!value && Number.isFinite(Date.parse(value))
  return (
    typeof record.request_id === 'string' &&
    !!record.request_id &&
    typeof record.actor_id === 'string' &&
    !!record.actor_id &&
    typeof record.reason === 'string' &&
    typeof record.inventory_version === 'string' &&
    typeof record.completed_by === 'string' &&
    date(record.created_at) &&
    (record.planned_at === null || date(record.planned_at)) &&
    (record.completed_at === null || date(record.completed_at)) &&
    !!record.assignments &&
    Array.isArray(record.assignments.project_assignments) &&
    record.assignments.project_assignments.every(
      (row) =>
        !!row &&
        typeof row.project_id === 'string' &&
        !!row.project_id &&
        ids(row.manager_user_ids),
    ) &&
    Array.isArray(record.assignments.team_assignments) &&
    record.assignments.team_assignments.every(
      (row) =>
        !!row &&
        typeof row.team_id === 'string' &&
        !!row.team_id &&
        ids(row.owner_user_ids) &&
        ids(row.add_member_user_ids),
    )
  )
}
function matches(record: OffboardingCase, intent: Intent) {
  if (
    !record ||
    typeof record.id !== 'string' ||
    !record.id ||
    !validCase(record) ||
    record.user_id !== intent.target ||
    record.mode !== (intent.mode === 'emergency' ? 'emergency' : 'planned') ||
    !['completed', 'ready_to_complete'].includes(record.status)
  )
    return false
  if (intent.mode === 'complete') {
    // A retry records the original creator and possibly another prior completer.
    return (
      record.id === intent.record?.id &&
      record.request_id === intent.record.request_id &&
      record.actor_id === intent.record.actor_id &&
      record.status === 'completed'
    )
  }
  return (
    record.request_id === intent.body.request_id &&
    record.actor_id === intent.actor &&
    record.reason === intent.body.reason &&
    (intent.mode === 'plan' || record.status === 'completed')
  )
}
export default function ActionDialog({
  mode,
  review: inventory,
  selectedCase,
  actor,
  target,
  generation,
  version,
  ready,
  canRead,
  canWrite,
  trigger,
  onSubmitted,
  onClose,
  onOpen,
  onSuccess,
  onReview,
}: {
  mode: Mode | null
  review?: OffboardingInventory
  selectedCase?: OffboardingCase
  actor: string
  target: string
  generation: number
  version: string
  ready: boolean
  canRead: (version: string) => boolean
  canWrite: (version: string, mode: Mode) => boolean
  trigger: HTMLButtonElement | null
  onSubmitted: (value: boolean) => void
  onClose: () => void
  onOpen: (mode: Mode) => void
  onSuccess: (record: OffboardingCase) => void
  onReview: () => void
}) {
  const { t } = useTranslation('offboarding')
  const cache = useQueryClient()
  const current = useRef({ mode, inventory, version, ready })
  useLayoutEffect(() => {
    current.current = { mode, inventory, version, ready }
  }, [mode, inventory, version, ready])
  const [busy, setBusy] = useState(false)
  const running = useRef<AbortController | null>(null)
  const closing = useRef(false)
  const search = useRef('')
  useLayoutEffect(() => {
    closing.current = false
  }, [mode])
  const capturedVersion = useRef('')
  const mounted = useRef(true)
  const [error, setError] = useState<string | null>(null)
  const [intent, setIntent] = useState<Intent | null>(null)
  const intentRef = useRef<Intent | null>(null)
  const [abandoned, setAbandoned] = useState(false)
  const [query, setQuery] = useState('')
  const [reason, setReason] = useState('')
  const [plannedAt, setPlannedAt] = useState('')
  const [projects, setProjects] = useState<Choices>({})
  const [teams, setTeams] = useState<Choices>({})
  const [additions, setAdditions] = useState<Record<string, string[]>>({})
  const draft = useRef({ projects, teams, additions })
  useLayoutEffect(() => {
    draft.current = { projects, teams, additions }
  }, [projects, teams, additions])
  useLayoutEffect(() => {
    search.current = query
  }, [query])
  const [previousReview, setPreviousReview] = useState(inventory)
  if (previousReview !== inventory && !intent) {
    setPreviousReview(inventory)
    setReason('')
    setPlannedAt('')
    setProjects({})
    setTeams({})
    setAdditions({})
    setQuery('')
    setError(null)
  }
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      running.current?.abort()
    }
  }, [])
  useEffect(() => {
    if (running.current && (!ready || version !== capturedVersion.current)) running.current.abort()
  }, [ready, version])
  const candidateKey = ['offboarding', actor, target, generation, 'successors', query] as const
  const members = useInfiniteQuery({
    queryKey: candidateKey,
    queryFn: ({ pageParam, signal }) =>
      getMembers({ q: query, status: 'active' }, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: ready && !!mode && mode !== 'complete' && !intent,
    retry: false,
  })
  const candidates =
    ready && freshQuery(cache, candidateKey)
      ? (members.data?.pages.flatMap((p) => p.items) ?? []).filter(
          (p) => !p.disabled && p.id !== target,
        )
      : []
  const currentInventory = cache.getQueryData<OffboardingInventory>([
    'offboarding',
    actor,
    target,
    generation,
    'inventory',
  ])
  const stale =
    !!inventory && !intent && inventory.inventory_version !== currentInventory?.inventory_version
  const editable = () =>
    !!mode &&
    !closing.current &&
    current.current.mode === mode &&
    current.current.inventory === inventory &&
    current.current.ready &&
    canWrite(version, mode) &&
    !running.current &&
    !intentRef.current &&
    !stale
  const canChoose = () => editable() && search.current === query && freshQuery(cache, candidateKey)
  function choose(
    kind: 'projects' | 'teams',
    id: string,
    choices: Choices[string],
    previous: Choices,
  ) {
    if (!editable() || draft.current[kind] !== previous) return
    const old = previous[id] ?? []
    const added = choices.filter(
      (person) => !old.some((p) => p.id === person.id && p.name === person.name),
    )
    if (added.length) {
      const known =
        cache
          .getQueryData<typeof members.data>(candidateKey)
          ?.pages.flatMap((page) => page.items) ?? []
      if (
        !canChoose() ||
        !added.every((person) =>
          known.some(
            (p) => p.id === person.id && p.name === person.name && !p.disabled && p.id !== target,
          ),
        )
      )
        return
    }
    const next = { ...previous, [id]: choices }
    draft.current = { ...draft.current, [kind]: next }
    if (kind === 'projects') setProjects(next)
    else setTeams(next)
  }
  function acknowledge(id: string, ids: string[]) {
    if (
      !editable() ||
      draft.current.additions !== additions ||
      !ids.every((person) => draft.current.teams[id]?.some((p) => p.id === person))
    )
      return
    const next = { ...additions, [id]: ids }
    draft.current = { ...draft.current, additions: next }
    setAdditions(next)
  }
  const teamRows =
    inventory?.teams.filter(
      (r) =>
        r.status !== 'archived' && (mode !== 'emergency' || needsEmergencySuccessor(r, target)),
    ) ?? []
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (
      !mode ||
      !inventory ||
      !event.currentTarget.isConnected ||
      closing.current ||
      current.current.mode !== mode ||
      current.current.inventory !== inventory ||
      !current.current.ready ||
      running.current ||
      !canWrite(version, mode)
    )
      return
    if (!intentRef.current && (stale || (mode !== 'complete' && !freshQuery(cache, candidateKey))))
      return
    const passwordInput = event.currentTarget.elements.namedItem(
      'current_password',
    ) as HTMLInputElement | null
    const password = passwordInput?.value ?? ''
    const csrf = cache.getQueryData<Session>(['auth', 'session'])?.csrf_token
    if (!csrf || (mode === 'emergency' && !password)) return
    let submitted = intentRef.current
    if (!submitted) {
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
      const date = new Date(plannedAt)
      if (mode === 'plan' && Number.isNaN(date.valueOf())) {
        setError('invalidDate')
        return
      }
      const normalizedReason = reason.trim()
      if (mode !== 'complete' && !normalizedReason) return
      if (mode === 'complete' && (!selectedCase || selectedCase.user_id !== target)) return
      submitted = {
        mode,
        actor,
        target,
        record: selectedCase,
        body:
          mode === 'plan'
            ? {
                ...assignments!,
                request_id: crypto.randomUUID(),
                inventory_version: inventory.inventory_version,
                planned_at: date.toISOString(),
                reason: normalizedReason,
              }
            : mode === 'emergency'
              ? {
                  request_id: crypto.randomUUID(),
                  team_assignments: assignments?.team_assignments ?? [],
                  reason: normalizedReason,
                }
              : {},
      }
      intentRef.current = submitted
      setIntent(submitted)
      onSubmitted(true)
    }
    if (passwordInput) passwordInput.value = ''
    const operation = new AbortController()
    running.current = operation
    capturedVersion.current = version
    setBusy(true)
    setError(null)
    try {
      const record =
        submitted.mode === 'complete'
          ? await completeOffboarding(
              submitted.target,
              submitted.record!.id,
              csrf,
              operation.signal,
            )
          : submitted.mode === 'plan'
            ? await createOffboardingPlan(
                submitted.target,
                submitted.body as OffboardingPlan,
                csrf,
                operation.signal,
              )
            : await emergencyOffboarding(
                submitted.target,
                submitted.body as EmergencyOffboarding,
                password,
                csrf,
                operation.signal,
              )
      if (
        !mounted.current ||
        operation.signal.aborted ||
        running.current !== operation ||
        !canWrite(capturedVersion.current, submitted.mode)
      )
        return
      if (!matches(record, submitted)) {
        setError('failed')
        return
      }
      intentRef.current = null
      setIntent(null)
      onSubmitted(false)
      setError(null)
      setAbandoned(false)
      onSuccess(record)
    } catch (caught) {
      if (!mounted.current || running.current !== operation) return
      if (operation.signal.aborted || !canWrite(capturedVersion.current, submitted.mode)) return
      const status = caught instanceof OffboardingError ? caught.status : 0
      setError(
        status === 409 || status === 412
          ? 'stale'
          : status === 503 || operation.signal.aborted
            ? 'unavailable'
            : status === 403
              ? 'denied'
              : status === 401
                ? 'reauth'
                : 'failed',
      )
      if (status === 401) void cache.invalidateQueries({ queryKey: ['auth', 'session'] })
      if (status === 403) void cache.invalidateQueries({ queryKey: ['permissions'] })
    } finally {
      if (running.current === operation) {
        running.current = null
        if (mounted.current) setBusy(false)
      }
    }
  }
  function close() {
    if (running.current || !canRead(version)) return
    closing.current = true
    onClose()
  }
  if (!ready || !canRead(version)) return null
  if (!mode || !inventory)
    return (
      <>
        {intent && (
          <section className="space-y-3 rounded-lg border p-4">
            <p role="status" className="text-sm">
              {t('unresolved')}
            </p>
            <Button
              variant="outline"
              disabled={busy || !canWrite(version, intent.mode)}
              onClick={() => {
                if (!running.current && canWrite(version, intent.mode)) {
                  closing.current = false
                  onOpen(intent.mode)
                }
              }}
            >
              {t('reviewSubmitted')}
            </Button>
          </section>
        )}
        {abandoned && (
          <p role="status" className="text-sm">
            {t('abandoned')}
          </p>
        )}
      </>
    )
  if (!canWrite(version, mode)) return null
  const uncertain = !!intent
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close()
      }}
      finalFocus={() => (canRead(version) && trigger?.isConnected ? trigger : false)}
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
                value={reason}
                onChange={(event) => {
                  if (editable()) setReason(event.target.value)
                }}
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
                  value={plannedAt}
                  onChange={(event) => {
                    if (editable()) setPlannedAt(event.target.value)
                  }}
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
                onChange={(event) => {
                  if (editable()) {
                    search.current = event.target.value
                    setQuery(event.target.value)
                  }
                }}
                disabled={busy || uncertain}
              />
            </FormField>
            <QueryState
              pending={members.isPending}
              error={members.error}
              retry={() => {
                if (editable()) void members.refetch()
              }}
            />
            {members.hasNextPage && (
              <Button
                disabled={members.isFetchingNextPage || busy}
                variant="outline"
                onClick={() => {
                  if (canChoose()) void members.fetchNextPage({ cancelRefetch: false })
                }}
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
                    onChoices={(choices) => {
                      choose('projects', resource.id, choices, projects)
                    }}
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
                onChoices={(choices) => {
                  choose('teams', resource.id, choices, teams)
                }}
                additions={additions[resource.id] ?? []}
                onAdditions={(ids) => {
                  acknowledge(resource.id, ids)
                }}
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
        {(error || stale) && (
          <div
            role="alert"
            className="space-y-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm"
          >
            <p>{t(error === 'stale' && intent ? 'submittedStale' : (error ?? 'stale'))}</p>
            {(error === 'stale' || stale) && (
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => {
                  if (canWrite(version, mode)) onReview()
                }}
              >
                {t('refreshReview')}
              </Button>
            )}
          </div>
        )}
        {intent && (
          <div className="space-y-2 text-sm">
            <p role="status">{t('unresolved')}</p>
            <p>{t('abandonWarning')}</p>
            <Button
              variant="outline"
              disabled={busy || !canWrite(version, intent.mode)}
              onClick={() => {
                if (running.current || !canWrite(version, intent.mode)) return
                closing.current = true
                intentRef.current = null
                setIntent(null)
                onSubmitted(false)
                setAbandoned(true)
                setError(null)
                onReview()
              }}
            >
              {t('abandon')}
            </Button>
          </div>
        )}
        <div className="flex flex-wrap justify-end gap-3">
          <Button variant="outline" disabled={busy} onClick={close}>
            {t('cancel')}
          </Button>
          <Button
            type="submit"
            disabled={
              busy ||
              !canWrite(version, mode) ||
              (!intent && (stale || (mode !== 'complete' && !freshQuery(cache, candidateKey))))
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
