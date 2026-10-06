import { t } from '@/i18n'
import { isAxiosError } from 'axios'
import { useTranslation } from 'react-i18next'
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type FormEvent,
} from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { getPermissions } from '@/api/governance'
import {
  createTeamWithInitialLimits,
  getTeamCreationContext,
  getTeamCreationOwners,
  getTeamCreationModels,
  reviewTeamCreationModels,
  resourcePath,
} from '@/api/resources'
import { useSession, sessionKey } from '@/hooks/use-auth'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import { useUncertainIntents } from '@/hooks/use-uncertain-intents'
import type { SubmittedIntentClaim, SubmittedIntentOwner } from '@/types/uncertain-intents'
import { teamCreationFields } from '@/types/resources'
import { Page, QueryState, FormField, ErrorNotice } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Dialog } from '@/components/ui/dialog'
import { Switch } from '@/components/ui/switch'
import type { Session } from '@/types/auth'
import type {
  TeamCreationContext,
  TeamCreationIntent,
  TeamCreationReceipt,
  TeamCreationModelReview,
} from '@/types/resources'
import { ResourceSection } from './shared'
import TeamCreationLimits from './team-creation-limits'
import TeamCreationModels from './team-creation-models'
import {
  teamCreationLimits,
  teamCreationSearch,
  teamCreationMetadata,
  teamTokenMillions,
  type TeamLimitDrafts,
} from './team-creation-values'

export default function TeamCreationPage({ admin }: { admin: boolean }) {
  useTranslation()
  const session = useSession()
  const generation = useSessionGeneration()
  const intentOwner = useUncertainIntents()
  const [routeLifetime, setRouteLifetime] = useState({ owner: intentOwner, version: 0 })
  if (routeLifetime.owner !== intentOwner)
    setRouteLifetime({ owner: intentOwner, version: routeLifetime.version + 1 })
  const cache = useQueryClient()
  useSyncExternalStore(
    (changed) =>
      cache.getQueryCache().subscribe((event) => {
        if (
          (event.type === 'updated' || event.type === 'removed') &&
          event.query.queryKey[0] === 'auth' &&
          event.query.queryKey[1] === 'session'
        )
          changed()
      }),
    () => {
      const state = cache.getQueryState<Session>(sessionKey)
      return `${state?.data?.user.id}/${state?.status}/${state?.fetchStatus}/${state?.isInvalidated}/${state?.dataUpdateCount}`
    },
  )
  const actor = cache.getQueryData<Session>(sessionKey)?.user.id
  // The same actor's editor remains mounted while authority is renewed. Logout or
  // a different actor destroys its local draft and immutable request.
  return actor ? (
    <TeamCreation
      key={`${actor}/${routeLifetime.version}`}
      actor={actor}
      generation={generation}
      admin={admin}
      intentOwner={intentOwner}
    />
  ) : (
    <QueryState
      pending={session.isPending || session.isFetching}
      error={session.error}
      retry={() => void session.refetch()}
    />
  )
}
function TeamCreation({
  actor,
  generation,
  admin,
  intentOwner,
}: {
  actor: string
  generation: number
  admin: boolean
  intentOwner: SubmittedIntentOwner | null
}) {
  useTranslation()
  const cache = useQueryClient(),
    navigate = useNavigate()
  const permissionKey = ['team-creation-permissions', actor, generation] as const
  const contextKey = ['team-creation-context', actor, generation] as const
  const [q, setQ] = useState(''),
    [owners, setOwners] = useState<string[]>([])
  const [modelQ, setModelQ] = useState(''),
    [modelCursor, setModelCursor] = useState<string | null>(null),
    [modelIDs, setModelIDs] = useState<string[]>([])
  const [modelReview, setModelReview] = useState<
    (TeamCreationModelReview & { etag: string }) | null
  >(null)
  const permissionRevision = cache.getQueryState(permissionKey)?.dataUpdateCount ?? 0
  const modelKey = [
    'team-creation-models',
    actor,
    generation,
    permissionRevision,
    cache.getQueryData<TeamCreationContext>(contextKey)?.review_etag,
    modelQ,
    modelCursor,
  ] as const
  const ownerKey = ['team-creation-owners', actor, generation, q] as const
  const keys = [sessionKey, permissionKey, contextKey, ownerKey]
  useSyncExternalStore(
    (changed) =>
      cache.getQueryCache().subscribe((event) => {
        if (
          (event.type === 'updated' || event.type === 'removed') &&
          keys.some((key) => JSON.stringify(key) === JSON.stringify(event.query.queryKey))
        )
          changed()
      }),
    () =>
      keys
        .map((key) => {
          const state = cache.getQueryState(key)
          return `${state?.status}/${state?.fetchStatus}/${state?.isInvalidated}/${state?.dataUpdateCount}/${state?.errorUpdateCount}`
        })
        .join('|'),
  )
  function fresh(key: readonly unknown[]) {
    const state = cache.getQueryState(key)
    return state?.status === 'success' && state.fetchStatus === 'idle' && !state.isInvalidated
  }
  const sessionReady =
    fresh(sessionKey) && cache.getQueryData<Session>(sessionKey)?.user.id === actor
  const options = {
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always' as const,
    refetchOnWindowFocus: false,
  }
  const permissions = useQuery({
    queryKey: permissionKey,
    queryFn: ({ signal }) => getPermissions(signal),
    enabled: sessionReady,
    ...options,
  })
  const canCreate =
    sessionReady && fresh(permissionKey) && permissions.data?.includes('teams.write') === true
  const context = useQuery({
    queryKey: contextKey,
    queryFn: ({ signal }) => getTeamCreationContext(signal),
    enabled: canCreate,
    ...options,
  })
  const current = canCreate && fresh(contextKey) && !!context.data
  const candidates = useQuery({
    queryKey: ownerKey,
    queryFn: ({ signal }) => getTeamCreationOwners(q, signal),
    enabled: current && teamCreationSearch(q),
    ...options,
  })
  const canSetModels =
    current &&
    context.data?.can_set_models === true &&
    permissions.data?.includes('teams.models.write') === true
  const models = useQuery({
    queryKey: modelKey,
    queryFn: ({ signal }) => getTeamCreationModels(modelQ, modelCursor, signal),
    enabled: canSetModels && teamCreationSearch(modelQ),
    ...options,
  })
  const [review, setReview] = useState<TeamCreationContext | null>(null)
  if (current && !review) setReview(context.data!)
  const [name, setName] = useState(''),
    [description, setDescription] = useState('')
  const [drafts, setDrafts] = useState<TeamLimitDrafts>({}),
    [reason, setReason] = useState('')
  const claim = useRef<SubmittedIntentClaim | null>(null)
  const [recoveredClaim, setRecoveredClaim] = useState<SubmittedIntentClaim | null>(null)
  const [intent, setIntent] = useState<TeamCreationIntent | null>(null)
  const intentRef = useRef<TeamCreationIntent | null>(null)
  const [retainedDefaultsUnknown, setRetainedDefaultsUnknown] = useState(false)
  const [receipt, setReceipt] = useState<TeamCreationReceipt | null>(null)
  const [busy, setBusy] = useState(false),
    [unknown, setUnknown] = useState(false)
  const [error, setError] = useState<unknown>(null),
    [invalid, setInvalid] = useState(false)
  const [previousUnknown, setPreviousUnknown] = useState(false)
  const [confirm, setConfirm] = useState(false),
    [abandon, setAbandon] = useState(false),
    [dismissed, setDismissed] = useState(false)
  const createTrigger = useRef<HTMLButtonElement | null>(null),
    abandonTrigger = useRef<HTMLButtonElement | null>(null)
  const alive = useRef(true),
    operation = useRef<symbol | null>(null),
    controller = useRef<AbortController | null>(null)
  useEffect(
    () => () => {
      alive.current = false
      controller.current?.abort()
    },
    [],
  )
  // Recover only an already dispatched request after independently fresh reads.
  // A recovery remains uncertain and idle; it never submits or restores authority.
  const retained = current && fresh(ownerKey) ? intentOwner?.recover(actor) : null
  if (retained?.kind === 'team-create' && recoveredClaim !== retained.claim) {
    setRecoveredClaim(retained.claim)
    setUnknown(true)
    if (!intent) {
      setRetainedDefaultsUnknown(true)
      const original = retained.payload
      setIntent(original)
      setName(original.body.name)
      setDescription(original.body.description)
      setOwners([...original.body.owner_ids])
      setModelIDs([...(original.body.model_ids ?? [])])
      setModelReview(null)
      const limits = original.body.initial_limits
      const restored: TeamLimitDrafts = {}
      if (limits) {
        for (const field of teamCreationFields) {
          if (!Object.hasOwn(limits, field)) continue
          const value = limits[field]
          restored[field] =
            value === null
              ? ''
              : field.startsWith('tokens_')
                ? teamTokenMillions(String(value))
                : String(value)
        }
      }
      setDrafts(restored)
      setReason(limits?.reason ?? '')
    }
  }
  useLayoutEffect(() => {
    claim.current = recoveredClaim
    intentRef.current = intent
  }, [recoveredClaim, intent])
  const needsReview = current && review?.review_etag !== context.data?.review_etag
  function authority(checkFields = false) {
    if (
      !alive.current ||
      !fresh(sessionKey) ||
      cache.getQueryData<Session>(sessionKey)?.user.id !== actor ||
      !fresh(permissionKey) ||
      !cache.getQueryData<string[]>(permissionKey)?.includes('teams.write') ||
      !fresh(contextKey)
    )
      return null
    const latest = cache.getQueryData<TeamCreationContext>(contextKey)
    if (!latest) return null
    const grants = cache.getQueryData<string[]>(permissionKey)!
    const caps = intentRef.current?.body.initial_limits
    const fields = caps
      ? Object.keys(caps).filter((field) => !['reason', 'currency'].includes(field))
      : Object.keys(drafts)
    if (
      checkFields &&
      fields.some(
        (field) =>
          !latest.editable_fields.includes(field as never) ||
          !grants.includes(
            field.startsWith('tokens_')
              ? 'teams.tokens.write'
              : field === 'money_month'
                ? 'teams.money.write'
                : 'teams.rates.write',
          ),
      )
    )
      return null
    const selected = intentRef.current?.body.model_ids ?? modelIDs
    if (
      checkFields &&
      selected.length &&
      (!latest.can_set_models || !grants.includes('teams.models.write'))
    )
      return null
    return { session: cache.getQueryData<Session>(sessionKey)!, context: latest }
  }
  function version() {
    return keys
      .map((key) => {
        const state = cache.getQueryState(key)
        return `${state?.dataUpdateCount}/${state?.errorUpdateCount}/${state?.isInvalidated}/${state?.fetchStatus}`
      })
      .join('|')
  }
  async function dispatch() {
    const auth = authority(true)
    if (!auth || operation.current) return
    let captured = intentRef.current
    if (!captured) {
      if (!review || review.review_etag !== auth.context.review_etag || !fresh(ownerKey)) return
      if (
        modelIDs.length &&
        (!modelReview ||
          modelReview.etag !== review.review_etag ||
          modelReview.model_ids.length !== modelIDs.length ||
          modelReview.model_ids.some((id) => !modelIDs.includes(id)))
      )
        return
      try {
        if (!teamCreationMetadata(name, description) || !owners.length || owners.length > 1000)
          throw new Error('invalid')
        captured = {
          etag: review.review_etag,
          body: {
            creation_id: crypto.randomUUID(),
            name: name.trim(),
            description,
            owner_ids: [...owners],
            ...(modelIDs.length && modelReview
              ? {
                  model_ids: [...modelReview.model_ids],
                  model_review_token: modelReview.model_review_token,
                }
              : {}),
            ...(Object.keys(drafts).length
              ? { initial_limits: teamCreationLimits(drafts, auth.context, reason) }
              : {}),
          },
        }
      } catch {
        setInvalid(true)
        return
      }
      if (intentOwner) {
        const capturedClaim = intentOwner.capture(actor, { kind: 'team-create', payload: captured })
        if (!capturedClaim) return
        claim.current = capturedClaim
        setRecoveredClaim(capturedClaim)
      }
      intentRef.current = captured
      setIntent(captured)
    }
    if (intentOwner && (!claim.current || !intentOwner.isCurrent(claim.current))) return
    const dispatchedClaim = claim.current
    const token = Symbol('creation'),
      before = version()
    operation.current = token
    setBusy(true)
    setUnknown(true)
    setError(null)
    setInvalid(false)
    setConfirm(false)
    controller.current = new AbortController()
    try {
      const value = await createTeamWithInitialLimits(
        captured,
        auth.session.csrf_token,
        controller.current.signal,
      )
      if (
        !authority() ||
        before !== version() ||
        intentRef.current !== captured ||
        (intentOwner && (!dispatchedClaim || !intentOwner.isCurrent(dispatchedClaim)))
      )
        return
      setReceipt(value)
      setUnknown(false)
      if (value.runtime_applied) {
        if (intentOwner && (!dispatchedClaim || !intentOwner.clear(dispatchedClaim))) return
        claim.current = null
        setRecoveredClaim(null)
        setRetainedDefaultsUnknown(false)
        intentRef.current = null
        setIntent(null)
        void cache.invalidateQueries({ queryKey: ['resources'] })
        navigate(`${resourcePath('teams', admin)}/${value.receipt.team_id}`)
      }
    } catch (failure) {
      if (
        authority() &&
        before === version() &&
        intentRef.current === captured &&
        (!intentOwner || (dispatchedClaim && intentOwner.isCurrent(dispatchedClaim)))
      )
        setError(failure)
    } finally {
      if (operation.current === token) {
        operation.current = null
        if (alive.current) setBusy(false)
      }
    }
  }
  async function submit(event: FormEvent) {
    event.preventDefault()
    const auth = authority(true)
    if (!auth || intentRef.current || needsReview || busy || operation.current) return
    try {
      if (
        !teamCreationMetadata(name, description) ||
        !owners.length ||
        owners.length > 1000 ||
        modelIDs.length > 1000
      )
        throw new Error('invalid')
      teamCreationLimits(drafts, auth.context, reason)
      setInvalid(false)
    } catch {
      setInvalid(true)
      return
    }
    if (!modelIDs.length) {
      setModelReview(null)
      setConfirm(true)
      return
    }
    const selected = [...modelIDs],
      before = version(),
      token = Symbol('model-review')
    operation.current = token
    setBusy(true)
    setError(null)
    controller.current = new AbortController()
    try {
      const result = await reviewTeamCreationModels(
        selected,
        auth.context.review_etag,
        auth.session.csrf_token,
        controller.current.signal,
      )
      if (!authority(true) || before !== version() || !alive.current || intentRef.current) return
      setModelReview({ ...result, etag: auth.context.review_etag })
      setConfirm(true)
    } catch (failure) {
      if (authority() && before === version() && alive.current && !intentRef.current) {
        setError(failure)
        if (isAxiosError(failure) && failure.response?.status === 409) {
          void context.refetch()
          void models.refetch()
        }
      }
    } finally {
      if (operation.current === token) {
        operation.current = null
        if (alive.current) setBusy(false)
      }
    }
  }
  function reviewCurrent() {
    if (!authority() || intentRef.current) return
    setReview(context.data!)
    setModelReview(null)
    setConfirm(false)
    setInvalid(false)
    setError(null)
  }
  if (!current && sessionReady && fresh(permissionKey) && !canCreate)
    return <p role="alert">{t('resources:teamCreation.denied')}</p>
  if (!current)
    return (
      <QueryState
        pending={
          cache.getQueryState(sessionKey)?.fetchStatus === 'fetching' ||
          cache.getQueryState(sessionKey)?.status === 'pending' ||
          (sessionReady && (permissions.isPending || permissions.isFetching)) ||
          (canCreate && (context.isPending || context.isFetching))
        }
        error={
          cache.getQueryState(sessionKey)?.error ||
          permissions.error ||
          context.error ||
          (!canCreate ? new Error(t('resources:teamCreation.denied')) : null)
        }
        retry={() => {
          void cache.invalidateQueries({ queryKey: sessionKey })
          void permissions.refetch()
          void context.refetch()
        }}
      />
    )
  const visibleContext: TeamCreationContext = {
    ...(review ?? context.data!),
    ...(intent?.body.initial_limits?.currency
      ? { platform_currency: intent.body.initial_limits.currency }
      : {}),
    editable_fields: context.data!.editable_fields.filter((field) =>
      permissions.data?.includes(
        field.startsWith('tokens_')
          ? 'teams.tokens.write'
          : field === 'money_month'
            ? 'teams.money.write'
            : 'teams.rates.write',
      ),
    ),
  }
  const submittedFields = intent?.body.initial_limits
    ? Object.keys(intent.body.initial_limits).filter(
        (field) => !['reason', 'currency'].includes(field),
      )
    : Object.keys(drafts)
  const fieldAuthority = submittedFields.every(
    (field) =>
      context.data!.editable_fields.includes(field as never) &&
      permissions.data?.includes(
        field.startsWith('tokens_')
          ? 'teams.tokens.write'
          : field === 'money_month'
            ? 'teams.money.write'
            : 'teams.rates.write',
      ),
  )
  const modelAuthority = !(intent?.body.model_ids ?? modelIDs).length || canSetModels
  const modelPageFresh = fresh(modelKey) && !models.isFetching && !models.isPending
  const modelOptions =
    canSetModels && modelPageFresh
      ? (models.data?.items ?? []).map((item) => ({
          value: item.id,
          label: item.name || item.id,
          providerNames: permissions.data?.includes('providers.read') ? (item.providers ?? []) : [],
          protocols: item.protocols,
        }))
      : []
  return (
    <Page
      title={t('common:create_value_91d65', { v0: t('resources:team') })}
      description={t('resources:teamCreation.help')}
    >
      <div className="max-w-[960px]">
        {previousUnknown && (
          <p role="alert" className="mb-4 text-sm">
            {t('resources:teamCreation.previousUnknown')}
          </p>
        )}
        {unknown && (
          <p role="alert" className="mb-4 text-sm">
            {t('resources:teamCreation.unknown')}
          </p>
        )}
        {receipt && (
          <p role="status" className="mb-4 text-sm">
            {t(`resources:teamCreation.${receipt.application_status}`)}
          </p>
        )}
        {dismissed ? (
          <Button onClick={() => setDismissed(false)}>{t('resources:teamCreation.resume')}</Button>
        ) : (
          <ResourceSection title={t('common:basic_information_b122f')}>
            <form
              onSubmit={submit}
              aria-label={t('resources:teamCreation.create')}
              className="space-y-6"
            >
              <fieldset disabled={busy || !!intent} className="space-y-6">
                <FormField label={t('common:value_name_0e54c', { v0: t('resources:team') })}>
                  <Input
                    name="name"
                    autoFocus
                    required
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    placeholder={t('common:for_example_search_evaluation_11e97')}
                  />
                </FormField>
                <FormField label={t('common:description_26670')}>
                  <Textarea
                    name="description"
                    rows={3}
                    value={description}
                    onChange={(event) => setDescription(event.target.value)}
                  />
                </FormField>
                <fieldset className="space-y-3">
                  <legend className="mb-2 text-sm font-medium">
                    {t('common:team_owners_f39f5')}
                  </legend>
                  <Input
                    aria-label={t('common:search_value_9f660', {
                      v0: t('common:team_owners_f39f5'),
                    })}
                    value={q}
                    onChange={(event) => {
                      if (authority() && !intentRef.current) setQ(event.target.value)
                    }}
                    placeholder={t('common:search_name_or_email_64826')}
                  />
                  {!teamCreationSearch(q) && (
                    <p role="alert">{t('resources:teamCreation.searchInvalid')}</p>
                  )}
                  {teamCreationSearch(q) && (
                    <QueryState
                      pending={
                        teamCreationSearch(q) && (candidates.isPending || candidates.isFetching)
                      }
                      error={
                        teamCreationSearch(q)
                          ? candidates.error
                          : new Error(t('resources:teamCreation.searchInvalid'))
                      }
                      retry={() => void candidates.refetch()}
                      empty={fresh(ownerKey) && candidates.data?.length === 0}
                    />
                  )}
                  {fresh(ownerKey) && (
                    <div className="max-h-64 space-y-2 overflow-y-auto rounded-md border p-3">
                      {candidates.data?.map((candidate) => (
                        <label className="flex items-center gap-3 text-sm" key={candidate.id}>
                          <Switch
                            checked={owners.includes(candidate.id)}
                            disabled={busy || !!intent}
                            onCheckedChange={(checked) => {
                              if (!authority() || !fresh(ownerKey) || intentRef.current) return
                              setOwners((value) =>
                                checked
                                  ? [...new Set([...value, candidate.id])]
                                  : value.filter((id) => id !== candidate.id),
                              )
                            }}
                            aria-label={candidate.name || candidate.id}
                          />
                          <span>
                            {candidate.name} {candidate.email}
                          </span>
                        </label>
                      ))}
                    </div>
                  )}
                  {!!owners.length && (
                    <div
                      className="flex flex-wrap gap-2"
                      aria-label={t('common:current_selection_7f06e')}
                    >
                      {owners.map((id) => (
                        <Button
                          key={id}
                          aria-label={t('common:remove_selection_value_90b0c', {
                            v0: fresh(ownerKey)
                              ? (candidates.data?.find((candidate) => candidate.id === id)?.name ??
                                id)
                              : id,
                          })}
                          type="button"
                          size="sm"
                          variant="secondary"
                          disabled={busy || !!intent}
                          onClick={() => {
                            if (authority() && !intentRef.current)
                              setOwners((value) => value.filter((item) => item !== id))
                          }}
                        >
                          {fresh(ownerKey)
                            ? (candidates.data?.find((candidate) => candidate.id === id)?.name ??
                              id)
                            : id}{' '}
                          ×
                        </Button>
                      ))}
                    </div>
                  )}
                </fieldset>
                <p className="text-sm text-muted-foreground">{t('resources:teamCreation.help')}</p>
                <TeamCreationModels
                  authorized={canSetModels}
                  options={modelOptions}
                  selected={modelIDs.map((id) => ({
                    value: id,
                    label: modelOptions.find((item) => item.value === id)?.label ?? id,
                  }))}
                  search={modelQ}
                  onSearchChange={(value) => {
                    if (authority() && !intentRef.current && !busy) {
                      setModelQ(value)
                      setModelCursor(null)
                    }
                  }}
                  onValueChange={(value) => {
                    if (!authority() || !canSetModels || intentRef.current || busy) return
                    if (value.length > 1000) {
                      setInvalid(true)
                      return
                    }
                    setModelIDs(value.map((item) => item.value))
                    setModelReview(null)
                    setConfirm(false)
                  }}
                  disabled={busy || !!intent}
                  footer={
                    canSetModels && (
                      <div className="space-y-2 border-t p-2 text-sm">
                        {!teamCreationSearch(modelQ) ? (
                          <p role="alert">{t('resources:teamCreation.searchInvalid')}</p>
                        ) : (
                          <QueryState
                            pending={models.isPending || models.isFetching}
                            error={models.error}
                            retry={() => void models.refetch()}
                            empty={modelPageFresh && models.data?.items.length === 0}
                          />
                        )}
                        {modelPageFresh && models.data?.next_cursor && (
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            disabled={busy || !!intent}
                            onClick={() => {
                              if (authority() && canSetModels && !intentRef.current && !busy)
                                setModelCursor(models.data!.next_cursor)
                            }}
                          >
                            {t('resources:teamCreation.models.next')}
                          </Button>
                        )}
                      </div>
                    )
                  }
                />
                <TeamCreationLimits
                  context={visibleContext}
                  retainedDefaultsUnknown={retainedDefaultsUnknown}
                  drafts={drafts}
                  reason={reason}
                  onChange={(value) => {
                    if (authority() && !intentRef.current) setDrafts(value)
                  }}
                  onReason={(value) => {
                    if (authority() && !intentRef.current) setReason(value)
                  }}
                  disabled={busy || !!intent}
                />
              </fieldset>
              {needsReview && !intent && <p role="alert">{t('resources:teamCreation.stale')}</p>}
              {invalid && <p role="alert">{t('resources:teamCreation.invalid')}</p>}
              {!modelAuthority && (
                <p role="alert">{t('resources:teamCreation.models.authorityChanged')}</p>
              )}
              {!intent && !modelAuthority && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={busy}
                  onClick={() => {
                    if (authority() && !intentRef.current) {
                      setModelIDs([])
                      setModelReview(null)
                    }
                  }}
                >
                  {t('resources:teamCreation.models.clear')}
                </Button>
              )}
              {!intent && !fieldAuthority && (
                <p role="alert">{t('resources:teamCreation.fieldsChanged')}</p>
              )}
              {!intent && Object.keys(drafts).length > 0 && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={busy}
                  onClick={() => {
                    if (authority() && !intentRef.current) setDrafts({})
                  }}
                >
                  {t('resources:teamCreation.defaultsAll')}
                </Button>
              )}
              <ErrorNotice error={error} />
              <div className="flex flex-wrap justify-end gap-3">
                <Button
                  type="button"
                  variant="outline"
                  disabled={busy}
                  onClick={() => {
                    if (!authority()) return
                    if (intentRef.current) setDismissed(true)
                    else navigate(resourcePath('teams', admin))
                  }}
                >
                  {t('resources:teamCreation.cancel')}
                </Button>
                {!intent && (
                  <Button type="button" variant="outline" disabled={busy} onClick={reviewCurrent}>
                    {t('resources:teamCreation.review')}
                  </Button>
                )}
                {intent ? (
                  <>
                    <Button
                      type="button"
                      disabled={busy || !fieldAuthority || !modelAuthority}
                      onClick={() => void dispatch()}
                    >
                      {t('resources:teamCreation.retry')}
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      disabled={busy}
                      ref={abandonTrigger}
                      onClick={() => setAbandon(true)}
                    >
                      {t('resources:teamCreation.abandon')}
                    </Button>
                  </>
                ) : (
                  <Button
                    ref={createTrigger}
                    type="submit"
                    disabled={
                      busy ||
                      !!needsReview ||
                      !fresh(ownerKey) ||
                      !fieldAuthority ||
                      !modelAuthority
                    }
                  >
                    {t('resources:teamCreation.create')}
                  </Button>
                )}
              </div>
            </form>
          </ResourceSection>
        )}
      </div>
      <Dialog
        open={confirm && current && !needsReview && !intent}
        onOpenChange={setConfirm}
        finalFocus={() =>
          authority() && !intentRef.current && createTrigger.current?.isConnected
            ? createTrigger.current
            : false
        }
        title={t('resources:teamCreation.confirm')}
        description={t('resources:teamCreation.confirmHelp')}
        busy={busy}
      >
        <Button
          onClick={() => void dispatch()}
          disabled={busy || !fieldAuthority || !modelAuthority}
        >
          {t('resources:teamCreation.confirm')}
        </Button>
      </Dialog>
      <Dialog
        open={abandon && current}
        onOpenChange={setAbandon}
        finalFocus={() =>
          authority() && abandonTrigger.current?.isConnected ? abandonTrigger.current : false
        }
        title={t('resources:teamCreation.abandon')}
        description={t('resources:teamCreation.abandonWarning')}
        busy={busy}
      >
        <Button
          onClick={() => {
            if (!authority() || busy) return
            if (intentOwner && (!claim.current || !intentOwner.clear(claim.current))) return
            claim.current = null
            setRecoveredClaim(null)
            setRetainedDefaultsUnknown(false)
            intentRef.current = null
            setIntent(null)
            setModelIDs([])
            setModelReview(null)
            setPreviousUnknown(true)
            setUnknown(false)
            setReceipt(null)
            setError(null)
            setReview(null)
            setAbandon(false)
            setDismissed(false)
          }}
        >
          {t('resources:teamCreation.abandonConfirm')}
        </Button>
      </Dialog>
    </Page>
  )
}
