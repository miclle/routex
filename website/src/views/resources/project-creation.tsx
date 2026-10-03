import { useTranslation } from 'react-i18next'
import { useLayoutEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'
import { isAxiosError } from 'axios'
import {
  createProjectResources,
  getProjectCreationContext,
  getProjectCreationManagers,
  validProjectCreationReason,
  resourcePath,
} from '@/api/resources'
import { sessionKey } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import type { Session } from '@/types/auth'
import type {
  ResourceCandidate,
  ProjectCreationContext,
  ProjectCreationIntent,
  ProjectCreationReceipt,
} from '@/types/resources'
import { Page, ErrorNotice, FormField, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { ResourceSection } from './shared'
import ProjectCreationResources from './project-creation-resources'
import { emptyCreationResources, creationResources } from './project-creation-values'

export default function ProjectCreation({
  admin,
  user,
  visible,
  generation,
}: {
  admin: boolean
  user: Session['user']
  visible: boolean
  generation: number
}) {
  const { t: tr } = useTranslation('resources')
  const cache = useQueryClient()
  const access = usePermissions()
  const canReplaceCreator = access.can('projects.write') && !access.isError && !access.isFetching
  const creator = { id: user.id, name: user.name, email: user.email }
  const [selected, setSelected] = useState<ResourceCandidate[]>([creator])
  const [direct, setDirect] = useState(emptyCreationResources)
  const [requested, setRequested] = useState(emptyCreationResources)
  const [requestMode, setRequestMode] = useState(false)
  const [reviewed, setReviewed] = useState<ProjectCreationContext | null>(null)
  const [intent, setIntent] = useState<ProjectCreationIntent | null>(null)
  const [uncertain, setUncertain] = useState(false)
  const [receipt, setReceipt] = useState<ProjectCreationReceipt | null>(null)
  const [receiptGeneration, setReceiptGeneration] = useState<number | null>(null)
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState(false)
  const [issue, setIssue] = useState<string | null>(null)
  const [error, setError] = useState<unknown>(null)
  const lock = useRef(false)
  const alive = useRef(true)
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  const sameActor = () =>
    alive.current && cache.getQueryData<Session>(sessionKey)?.user.id === user.id
  const currentActor = () =>
    sameActor() &&
    cache.getQueryState(sessionKey)?.status === 'success' &&
    cache.getQueryState(sessionKey)?.fetchStatus !== 'fetching'
  const context = useQuery({
    queryKey: ['project-creation-context', user.id, generation],
    queryFn: ({ signal }) => getProjectCreationContext(signal),
    enabled: visible,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const current = visible && context.isSuccess && !context.isFetching ? context.data : undefined
  if (current && !reviewed) setReviewed(current)
  const needsReview = !!current && !!reviewed && current.review_etag !== reviewed.review_etag
  const candidates = useQuery({
    queryKey: ['project-creation-managers', user.id, generation, q],
    queryFn: ({ signal }) => getProjectCreationManagers(q, signal),
    enabled: visible && !!current,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const fresh = visible && !!current && candidates.isSuccess && !candidates.isFetching
  const effective =
    !canReplaceCreator && !selected.some((item) => item.id === user.id)
      ? [creator, ...selected]
      : selected
  const blocked = busy || !!intent || !!receipt
  async function dispatch(captured: ProjectCreationIntent) {
    const session = cache.getQueryData<Session>(sessionKey)
    if (busy || lock.current || !visible || !session || !currentActor()) return
    lock.current = true
    setBusy(true)
    setReceiptGeneration(null)
    setError(null)
    setIssue(null)
    try {
      const value = await createProjectResources(captured, session.csrf_token)
      if (receipt && JSON.stringify(value.receipt) !== JSON.stringify(receipt.receipt))
        throw new Error('Changed Project creation receipt')
      if (sameActor()) {
        setReceiptGeneration(currentActor() ? generation : null)
        setReceipt(value)
        setUncertain(false)
        setIssue('creationCommitted')
        void cache.invalidateQueries({ queryKey: ['resources'] })
      }
    } catch (failure) {
      if (sameActor()) {
        const status = isAxiosError(failure) ? failure.response?.status : undefined
        const unknown = uncertain || !status || status >= 500
        setUncertain(receipt ? false : unknown)
        setIssue(
          receipt
            ? 'creationReceiptRefreshFailed'
            : unknown
              ? 'creationUncertain'
              : 'creationRejected',
        )
        if (status && status < 500) setError(failure)
      }
    } finally {
      lock.current = false
      if (sameActor()) setBusy(false)
    }
  }
  async function submitProject(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const authority = cache.getQueryState(['permissions', user.id])
    const contextAuthority = cache.getQueryState(['project-creation-context', user.id, generation])
    const managerAuthority = cache.getQueryState([
      'project-creation-managers',
      user.id,
      generation,
      q,
    ])
    if (
      contextAuthority?.status !== 'success' ||
      contextAuthority.fetchStatus === 'fetching' ||
      managerAuthority?.status !== 'success' ||
      managerAuthority.fetchStatus === 'fetching' ||
      authority?.status !== 'success' ||
      authority.fetchStatus === 'fetching' ||
      blocked ||
      lock.current ||
      !currentActor() ||
      !current ||
      !reviewed ||
      !fresh ||
      needsReview ||
      access.isError ||
      access.isFetching
    )
      return
    if (!effective.length) {
      setIssue('creationManagerRequired')
      return
    }
    if (
      requestMode &&
      (!current.can_request_resources || !effective.some((person) => person.id === user.id))
    ) {
      setIssue('creationRequestManager')
      return
    }
    const form = new FormData(event.currentTarget)
    const name = String(form.get('name')).trim(),
      description = String(form.get('description') || '').trim()
    if (!name) {
      setIssue('blankName')
      return
    }
    const draft = requestMode ? requested : direct
    const resources = creationResources(draft, reviewed.platform_currency)
    if (resources === undefined) {
      setIssue('creationInvalidResources')
      return
    }
    if (requestMode && !resources) {
      setIssue('creationRequestRequired')
      return
    }
    if (
      resources &&
      (!validProjectCreationReason(draft.reason) ||
        (!requestMode &&
          ((resources.model_ids?.length && !current.can_set_models) ||
            (['tokens_month', 'money_month', 'rpm', 'tpm', 'concurrency'].some((key) =>
              Object.hasOwn(resources, key),
            ) &&
              !current.can_set_limits))))
    ) {
      setIssue('creationResourceReasonInvalid')
      return
    }
    if (resources?.model_ids?.length) {
      const queries = cache.getQueryCache().findAll({
        queryKey: ['project-creation-models', user.id, reviewed.review_etag, requestMode],
      })
      if (
        !queries.length ||
        queries.some(
          (query) => query.state.status !== 'success' || query.state.fetchStatus === 'fetching',
        )
      )
        return
    }
    const captured: ProjectCreationIntent = {
      body: {
        creation_id: crypto.randomUUID(),
        name,
        description,
        manager_ids: effective.map((person) => person.id),
        ...(resources
          ? requestMode
            ? { initial_request: resources }
            : { initial_resources: resources }
          : {}),
      },
      etag: reviewed.review_etag,
    }
    setIntent(captured)
    await dispatch(captured)
  }
  return (
    <Page title={tr('creationTitle')} description={tr('creationHelp')}>
      <ResourceSection title={tr('creationTitle')}>
        <form
          aria-label={tr('creationTitle')}
          className="space-y-6"
          onSubmit={(event) => void submitProject(event)}
        >
          <fieldset
            disabled={
              blocked || !visible || !current || !fresh || access.isFetching || access.isError
            }
            className="space-y-6"
          >
            <FormField label={tr('creationName')}>
              <Input name="name" autoFocus required maxLength={100} />
            </FormField>
            <FormField label={tr('businessDescription')}>
              <Textarea name="description" rows={3} maxLength={2000} />
            </FormField>
            <fieldset className="space-y-3">
              <legend className="mb-2 text-sm font-medium">{tr('managers')}</legend>
              <Input
                aria-label={tr('creationManagerSearch')}
                placeholder={tr('creationManagerSearch')}
                value={q}
                onValueChange={setQ}
              />
              <QueryState
                pending={candidates.isFetching}
                error={candidates.error}
                retry={() => void candidates.refetch()}
                empty={fresh && !candidates.data?.length}
              />
              {fresh && (
                <div className="max-h-64 space-y-2 overflow-y-auto rounded-md border p-3">
                  {candidates.data?.map((candidate) => (
                    <label key={candidate.id} className="flex items-center gap-3 text-sm">
                      <input
                        type="checkbox"
                        checked={effective.some((item) => item.id === candidate.id)}
                        disabled={!canReplaceCreator && candidate.id === user.id}
                        onChange={(event) =>
                          setSelected((old) =>
                            event.target.checked
                              ? [...old.filter((item) => item.id !== candidate.id), candidate]
                              : old.filter((item) => item.id !== candidate.id),
                          )
                        }
                      />
                      <span>
                        {candidate.name}
                        <span className="ml-2 text-muted-foreground">{candidate.email}</span>
                      </span>
                    </label>
                  ))}
                </div>
              )}
              <div
                className="flex flex-wrap gap-2"
                aria-label={tr('creationSelectedManagers')}
                hidden={!fresh}
              >
                {effective.map((person) => (
                  <Button
                    key={person.id}
                    type="button"
                    size="sm"
                    variant="secondary"
                    disabled={!canReplaceCreator && person.id === user.id}
                    onClick={() =>
                      setSelected((old) => old.filter((item) => item.id !== person.id))
                    }
                    aria-label={tr('creationRemoveManager', { name: person.name })}
                  >
                    {person.name} ×
                  </Button>
                ))}
              </div>
              <p className="text-xs text-muted-foreground">
                {tr(canReplaceCreator ? 'creationManagerAdminHelp' : 'creationManagerMemberHelp')}
              </p>
            </fieldset>
            {current && (current.can_request_resources || requestMode) && (
              <label className="flex items-center gap-3 text-sm">
                <input
                  type="checkbox"
                  checked={requestMode}
                  onChange={(event) => setRequestMode(event.target.checked)}
                />
                <span>{tr('creationSubmitRequest')}</span>
              </label>
            )}
            {reviewed && (
              <>
                {(reviewed.can_set_models || reviewed.can_set_limits) && (
                  <ProjectCreationResources
                    actor={user.id}
                    context={reviewed}
                    request={false}
                    draft={direct}
                    onChange={setDirect}
                    disabled={blocked}
                    visible={
                      visible &&
                      !!current &&
                      !needsReview &&
                      !requestMode &&
                      (current.can_set_models || current.can_set_limits)
                    }
                  />
                )}
                <ProjectCreationResources
                  actor={user.id}
                  context={reviewed}
                  request
                  draft={requested}
                  onChange={setRequested}
                  disabled={blocked}
                  visible={
                    visible &&
                    !!current &&
                    !needsReview &&
                    requestMode &&
                    current.can_request_resources
                  }
                />
              </>
            )}
            {current &&
              !requestMode &&
              !needsReview &&
              direct.models.length > 0 &&
              !current.can_set_models && (
                <Button
                  variant="outline"
                  onClick={() => setDirect((old) => ({ ...old, models: [] }))}
                >
                  {tr('creationClearUnavailableModels')}
                </Button>
              )}
            {current &&
              !requestMode &&
              !needsReview &&
              !current.can_set_limits &&
              ['tokens_month', 'money_month', 'rpm', 'tpm', 'concurrency'].some(
                (field) => direct[field as keyof typeof direct] !== '',
              ) && (
                <Button
                  variant="outline"
                  onClick={() =>
                    setDirect((old) => ({
                      ...old,
                      tokens_month: '',
                      money_month: '',
                      rpm: '',
                      tpm: '',
                      concurrency: '',
                    }))
                  }
                >
                  {tr('creationClearUnavailableLimits')}
                </Button>
              )}
            <p className="text-sm text-muted-foreground">{tr('creationNoDefaults')}</p>
          </fieldset>
          <QueryState
            pending={visible && context.isFetching}
            error={visible ? context.error : null}
            retry={() => void context.refetch()}
          />
          {needsReview && !intent && <p role="alert">{tr('creationContextChanged')}</p>}
          {!uncertain && !receipt && (needsReview || !!intent) && (
            <Button
              variant="outline"
              disabled={busy || !current || !fresh}
              onClick={() => {
                setReviewed(current!)
                setIntent(null)
                setIssue(null)
                setError(null)
              }}
            >
              {tr('creationReview')}
            </Button>
          )}
          {issue && (
            <p role="alert" className="text-sm text-destructive">
              {tr(issue)}
            </p>
          )}
          <ErrorNotice error={error} />
          {receipt && visible && (
            <div className="space-y-3 rounded-lg border p-4" aria-label={tr('creationReceipt')}>
              <p>{tr('creationReceiptProject', { id: receipt.receipt.project_id })}</p>
              <p>
                {tr('creationReceiptRequests', {
                  count: receipt.receipt.initial_request_ids.length,
                })}
              </p>
              {receipt.receipt.initial_request_ids.map((id) => (
                <p key={id} className="font-mono text-xs">
                  {id}
                </p>
              ))}
              <p role="status">
                {tr(
                  busy
                    ? 'creationCheckingReceipt'
                    : receiptGeneration !== generation
                      ? 'creationApplicationNeedsRefresh'
                      : `creationApplication_${receipt.application_status}`,
                )}
              </p>
              {receipt.project && !busy && receiptGeneration === generation && (
                <Link
                  className="text-sm underline"
                  to={`${resourcePath('projects', admin)}/${receipt.project.id}`}
                >
                  {tr('creationOpenProject')}
                </Link>
              )}
            </div>
          )}
          <div className="flex justify-end gap-3">
            <Link
              aria-disabled={busy || uncertain}
              onClick={(event) => {
                if (busy || uncertain) event.preventDefault()
              }}
              to={resourcePath('projects', admin)}
              className="text-sm underline"
            >
              {tr('creationBack')}
            </Link>
            {intent ? (
              <Button
                type="button"
                disabled={busy || !visible}
                onClick={() => void dispatch(intent)}
              >
                {tr(receipt ? 'creationRecheckReceipt' : 'creationRetry')}
              </Button>
            ) : (
              <Button
                type="submit"
                disabled={
                  blocked ||
                  !fresh ||
                  !current ||
                  needsReview ||
                  access.isFetching ||
                  access.isError
                }
              >
                {tr(requestMode ? 'creationAndRequests' : 'creationTitle')}
              </Button>
            )}
            {!receipt && (
              <Button
                type="button"
                variant="outline"
                disabled={busy || !visible || context.isFetching}
                onClick={() => void context.refetch()}
              >
                {tr('creationRefreshContext')}
              </Button>
            )}
          </div>
        </form>
      </ResourceSection>
    </Page>
  )
}
