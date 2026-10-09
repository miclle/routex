import { ModelRecordedDate, ModelMonthlyCount, ModelMonthlyContext } from './model-metadata'
import { useModelMonthlyRequests } from './use-model-metadata'
import { protocolLabel, protocolLabels } from '@/lib/protocols'
import { useTranslation } from 'react-i18next'
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type FormEvent,
} from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import {
  getAdminModel,
  getModelAliasRetirement,
  retireModelAlias,
  validModelAliasReason,
  listAdminModels,
  listGrantees,
  listProviders,
  writeCatalog,
} from '@/api/catalog'
import { sessionKey, useSession } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import RoutingWeights from './routing-weights'
import RoutingWeightHistory from './routing-weight-history'
import ModelRenameFields from './model-rename-fields'
import { initialModelRenameDraft, type ModelRenameDraft } from './model-rename-draft'
import RoutingCandidates from './routing-candidates'
import type { RoutingProtocol } from '@/types/model-routing'
import { Page, QueryState, ErrorNotice, FormField, SaveButton } from '@/components/app/CatalogUI'
import { getPermissions } from '@/api/governance'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import { Input } from '@/components/ui/input'
import { Button, buttonVariants } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Link, useParams } from 'react-router'
import { Table } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import type {
  Model,
  ModelAliasRetirementReview,
  ModelAliasRetirementInput,
  ModelAliasRetirementResult,
} from '@/types/catalog'
import axios from 'axios'

type RoutingDraft = { identities: string; values: Record<string, string> }
function routingIdentities(model: Model) {
  return JSON.stringify(
    model.bindings
      .map((binding) =>
        JSON.stringify([
          binding.id,
          binding.provider_model_id,
          binding.provider_id,
          binding.connection_id,
          binding.protocol,
        ]),
      )
      .sort(),
  )
}
function routingValues(model: Model) {
  return Object.fromEntries(model.bindings.map((binding) => [binding.id, String(binding.weight)]))
}

type Action =
  { kind: 'create' } | { kind: 'rename' | 'binding' | 'weights' | 'grants'; model: Model }
export default function AdminModelsPage() {
  const session = useSession()
  const generation = useSessionGeneration()
  const { modelId } = useParams()
  return session.data ? (
    <AdminModels
      key={`${session.data.user.id}:${modelId ?? 'list'}`}
      actor={session.data.user.id}
      role={session.data.user.role}
      generation={generation}
      modelId={modelId}
      visible={!session.isError && !session.isFetching && !!session.data.csrf_token}
    />
  ) : null
}
function AdminModels({
  actor,
  role,
  generation,
  modelId,
  visible,
}: {
  actor: string
  role: Session['user']['role']
  generation: number
  modelId: string | undefined
  visible: boolean
}) {
  const { t, i18n } = useTranslation('catalog')
  const cache = useQueryClient()
  const authorityGeneration = useRef(generation)
  useLayoutEffect(() => {
    authorityGeneration.current = generation
    return cache.getQueryCache().subscribe((event) => {
      if (
        event.query.queryKey.length === 2 &&
        event.query.queryKey[0] === 'auth' &&
        event.query.queryKey[1] === 'session' &&
        event.type === 'updated' &&
        event.action.type === 'success' &&
        !event.action.manual
      )
        authorityGeneration.current = event.query.state.dataUpdateCount
    })
  }, [cache, generation])
  const permissionKey = ['permissions', actor, generation]
  const permissions = useQuery({
    queryKey: permissionKey,
    queryFn: ({ signal }) => getPermissions(signal),
    enabled: visible,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    refetchInterval: 30_000,
  })
  const permissionGeneration = useModelReadGeneration(permissionKey)
  const access = {
    ...permissions,
    can: (permission: string) => permissions.data?.includes(permission) === true,
  }
  const sessionFresh = useModelReadFresh(sessionKey)
  const permissionFresh = useModelReadFresh(permissionKey)
  const readable =
    visible &&
    sessionFresh &&
    permissionFresh &&
    cache.getQueryData<Session>(sessionKey)?.user.id === actor &&
    !cache.getQueryState(sessionKey)?.isInvalidated &&
    !cache.getQueryState(permissionKey)?.isInvalidated &&
    !access.isError &&
    !access.isFetching &&
    access.can('models.read_all')
  const detailKey = ['admin', 'models', 'detail', actor, modelId, generation, permissionGeneration]
  const detail = useQuery({
    queryKey: detailKey,
    queryFn: ({ signal }) => getAdminModel(modelId!, signal),
    enabled: readable && !!modelId,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const detailGeneration = useModelReadGeneration(detailKey)
  const listKey = ['admin', 'models', 'list', actor, generation, permissionGeneration]
  const models = useQuery({
    queryKey: listKey,
    queryFn: listAdminModels,
    enabled: readable && !modelId,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const detailFresh = useModelReadFresh(detailKey)
  const selected =
    readable &&
    detailFresh &&
    detail.isSuccess &&
    !detail.isFetching &&
    !cache.getQueryState(detailKey)?.isInvalidated
      ? detail.data
      : undefined
  const [weightHistoryOpen, setWeightHistoryOpen] = useState(false)
  const weightHistoryTrigger = useRef<HTMLButtonElement | null>(null)
  const weightHistoryFocusOwner = useRef<{ generation: number; birth: string | null } | null>(null)
  const [routingProtocol, setRoutingProtocol] = useState<RoutingProtocol | null>(null)
  const [action, setAction] = useState<Action | null>(null)
  const [renameDraft, setRenameDraft] = useState<ModelRenameDraft | null>(null)
  const [renameUncertain, setRenameUncertain] = useState(false)
  const mounted = useRef(false)
  const currentAction = useRef(action)
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  useLayoutEffect(() => {
    currentAction.current = action
  }, [action])
  // This actor/Model-keyed owner outlives the conditional fresh private form, not AuthGate.
  const [routingDraft, setRoutingDraft] = useState<RoutingDraft | null>(null)
  const [search, setSearch] = useState('')
  const [aliasName, setAliasName] = useState<string | null>(null)
  const [aliasOpen, setAliasOpen] = useState(false)
  const [aliasLocked, setAliasLocked] = useState(false)
  const providers = useQuery({
    queryKey: ['admin', 'model-providers', actor, generation, permissionGeneration],
    queryFn: ({ signal }) => listProviders(signal),
    enabled: readable && access.can('providers.read'),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const grantees = useQuery({
    queryKey: ['admin', 'model-grantees', actor, modelId, generation, permissionGeneration],
    queryFn: listGrantees,
    enabled: readable && !!selected && access.can('models.write') && action?.kind === 'grants',
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const providerData =
    readable && providers.isSuccess && !providers.isFetching && access.can('providers.read')
      ? providers.data
      : undefined
  const grantsFresh = readable && grantees.isSuccess && !grantees.isFetching
  const listFresh =
    readable &&
    models.isSuccess &&
    !models.isFetching &&
    !cache.getQueryState(listKey)?.isInvalidated
  const filteredModels = listFresh
    ? (models.data ?? []).filter((model) =>
        [
          model.name,
          model.bindings
            .map((binding) => providerData?.find((p) => p.id === binding.provider_id)?.name)
            .join(' '),
          model.bindings.map((binding) => binding.protocol).join(' '),
          model.bindings.map((binding) => protocolLabel(binding.protocol)).join(' '),
        ]
          .join(' ')
          .toLowerCase()
          .includes(search.toLowerCase()),
      )
    : []
  const monthly = useModelMonthlyRequests({
    actor,
    generation,
    permissionKey,
    catalogKey: modelId ? detailKey : listKey,
    ids: modelId ? (selected ? [selected.id] : []) : filteredModels.map((model) => model.id),
    visible: readable && (modelId ? !!selected : listFresh),
  })
  const writeReady = () => {
    const session = cache.getQueryData<Session>(sessionKey)
    const permissions = cache.getQueryState(permissionKey)
    return (
      session?.user.id === actor &&
      session.user.role === role &&
      !!session.csrf_token &&
      authorityGeneration.current === generation &&
      cache.getQueryState(sessionKey)?.status === 'success' &&
      cache.getQueryState(sessionKey)?.fetchStatus !== 'fetching' &&
      permissions?.status === 'success' &&
      !permissions.isInvalidated &&
      !cache.getQueryState(sessionKey)?.isInvalidated &&
      permissions.fetchStatus !== 'fetching' &&
      permissions.dataUpdateCount === permissionGeneration &&
      cache.getQueryData<string[]>(permissionKey)?.includes('models.write') === true &&
      cache.getQueryData<string[]>(permissionKey)?.includes('models.read_all') === true &&
      (!modelId ||
        (cache.getQueryState(detailKey)?.status === 'success' &&
          !cache.getQueryState(detailKey)?.isInvalidated &&
          cache.getQueryState(detailKey)?.fetchStatus !== 'fetching' &&
          cache.getQueryState(detailKey)?.dataUpdateCount === detailGeneration))
    )
  }
  const weightHistoryReadReady = () => {
    const state = cache.getQueryState(permissionKey)
    const sessionState = cache.getQueryState(sessionKey)
    const target = cache.getQueryState(detailKey)
    return (
      authorityGeneration.current === generation &&
      cache.getQueryData<Session>(sessionKey)?.user.id === actor &&
      cache.getQueryData<Session>(sessionKey)?.user.role === role &&
      sessionState?.status === 'success' &&
      sessionState.fetchStatus === 'idle' &&
      !sessionState.isInvalidated &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      state.dataUpdateCount === permissionGeneration &&
      cache.getQueryData<string[]>(permissionKey)?.includes('models.read_all') === true &&
      target?.status === 'success' &&
      target.fetchStatus === 'idle' &&
      !target.isInvalidated &&
      target.dataUpdateCount === detailGeneration &&
      (target.data as Model | undefined)?.id === modelId
    )
  }
  const routingReadReady = () => {
    const state = cache.getQueryState(permissionKey)
    const sessionState = cache.getQueryState(sessionKey)
    const target = cache.getQueryState(detailKey)
    return (
      authorityGeneration.current === generation &&
      cache.getQueryData<Session>(sessionKey)?.user.id === actor &&
      sessionState?.status === 'success' &&
      sessionState.fetchStatus === 'idle' &&
      !sessionState.isInvalidated &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      state.dataUpdateCount === permissionGeneration &&
      cache.getQueryData<string[]>(permissionKey)?.includes('models.read_all') === true &&
      cache.getQueryData<string[]>(permissionKey)?.includes('providers.read') === true &&
      target?.status === 'success' &&
      target.fetchStatus === 'idle' &&
      !target.isInvalidated &&
      target.dataUpdateCount === detailGeneration
    )
  }
  useLayoutEffect(() => {
    if (!readable) {
      void cache.cancelQueries({ queryKey: ['admin', 'models', 'detail', actor, modelId] })
      void cache.cancelQueries({ queryKey: ['admin', 'model-alias-retirement', actor, modelId] })
    }
  }, [readable, cache, actor, modelId])
  const mutation = useMutation({
    mutationFn: ({
      path,
      data,
      method = 'post',
    }: {
      path: string
      data: unknown
      method?: 'post' | 'put'
      renameReview?: {
        action: Action
        actor: string
        modelId: string
        generation: number
        permissionGeneration: number
        detailGeneration: number
      }
    }) => {
      if (!writeReady()) throw new Error('Current Model write authority unavailable')
      return writeCatalog(method, path, data, cache.getQueryData<Session>(sessionKey)!.csrf_token)
    },
    onSuccess: (_, { renameReview }) => {
      if (cache.getQueryData<Session>(sessionKey)?.user.id !== actor) return
      if (renameReview) {
        if (
          !mounted.current ||
          currentAction.current !== renameReview.action ||
          renameReview.actor !== actor ||
          renameReview.modelId !== modelId
        )
          return
        if (
          renameReview.generation !== generation ||
          renameReview.permissionGeneration !== permissionGeneration ||
          renameReview.detailGeneration !== detailGeneration ||
          !writeReady()
        ) {
          setRenameUncertain(true)
          return
        }
        setRenameUncertain(false)
      }
      setAction(null)
      void cache.invalidateQueries({ queryKey: ['admin', 'models'] })
      void cache.invalidateQueries({ queryKey: ['models'] })
    },
  })
  function open(next: Action) {
    if (!readable || !selected || !access.can('models.write')) return
    mutation.reset()
    if (next.kind === 'rename') {
      setRenameDraft(initialModelRenameDraft(next.model.name))
      setRenameUncertain(false)
    }
    setAction(next)
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (
      !action ||
      mutation.isPending ||
      !writeReady() ||
      (action.kind !== 'create' && action.model.id !== modelId) ||
      (action.kind === 'grants' && !grantsFresh)
    )
      return
    const form = new FormData(event.currentTarget)
    const name =
      action.kind === 'rename' && renameDraft
        ? renameDraft.name.trim()
        : String(form.get('name') ?? '').trim()
    const provider_model_id = String(form.get('provider_model_id') ?? '')
    if (action.kind === 'create')
      mutation.mutate({ path: '/admin/models', data: { name, provider_model_id } })
    else {
      const path = `/admin/models/${action.model.id}`
      if (action.kind === 'binding')
        mutation.mutate({ path: `${path}/bindings`, data: { provider_model_id } })
      if (action.kind === 'rename') {
        const expiration = renameDraft?.keepOldName ? renameDraft.expiresAt : ''
        mutation.mutate({
          path: `${path}/rename`,
          renameReview: {
            action,
            actor,
            modelId: action.model.id,
            generation,
            permissionGeneration,
            detailGeneration,
          },
          data: {
            name,
            ...(expiration ? { alias_expires_at: new Date(expiration).toISOString() } : {}),
          },
        })
      }
      if (action.kind === 'weights')
        mutation.mutate({
          path: `${path}/weights`,
          method: 'put',
          data: {
            weights: action.model.bindings.map((binding) => ({
              binding_id: binding.id,
              weight: Number(form.get(binding.id)),
            })),
          },
        })
      if (action.kind === 'grants')
        mutation.mutate({
          path: `${path}/grants`,
          method: 'put',
          data: { user_ids: form.getAll('user_ids').map(String) },
        })
    }
  }
  const titles = {
    create: t('createModel.title'),
    rename: t('adminModels.renameTitle'),
    binding: t('adminModels.addBinding'),
    weights: t('adminModels.weightsTitle'),
    grants: t('adminModels.grantsTitle'),
  }
  const upstreamModels =
    providerData?.flatMap((provider) =>
      provider.connections.flatMap((connection) =>
        connection.provider_models.map((model) => ({
          id: model.id,
          label: `${provider.name} / ${connection.name} / ${model.upstream_name}`,
        })),
      ),
    ) ?? []
  return (
    <Page title={t('adminModels.title')} description={t('adminModels.description')}>
      <QueryState
        pending={visible && permissions.isFetching}
        error={visible ? permissions.error : null}
        retry={() => void permissions.refetch()}
      />
      {visible && permissions.isSuccess && !access.can('models.read_all') && (
        <p role="alert">{t('aliasRetirement.readUnavailable')}</p>
      )}
      <QueryState
        pending={modelId ? detail.isFetching : models.isFetching}
        error={visible ? (modelId ? detail.error : models.error) : null}
        retry={() => void (modelId ? detail.refetch() : models.refetch())}
        empty={!modelId && readable && models.isSuccess && models.data.length === 0}
      />
      {!modelId && listFresh && (
        <>
          <div className="flex items-center justify-between gap-4">
            <Input
              aria-label={t('common.searchModels')}
              placeholder={t('adminModels.searchPlaceholder')}
              value={search}
              onValueChange={setSearch}
              className="max-w-[420px]"
            />
            {access.can('models.write') && access.can('providers.read') && (
              <Link className={buttonVariants()} to="/admin/models/new">
                <Plus className="size-4" aria-hidden="true" />
                {t('createModel.title')}
              </Link>
            )}
          </div>
          <div className="rounded-lg border">
            <Table aria-label={t('adminModels.listLabel')} className="min-w-[1250px]">
              <thead>
                <tr>
                  <th>{t('adminModels.publicName')}</th>
                  <th>{t('common.protocolType')}</th>
                  <th>{t('modelMetadata.capabilityType')}</th>
                  <th>{t('common.provider')}</th>
                  <th>{t('adminModels.status')}</th>
                  <th>{t('adminModels.members')}</th>
                  <th>{t('modelMetadata.monthlyRequests')}</th>
                  <th>{t('modelMetadata.updated')}</th>
                  <th>{t('common.actions')}</th>
                </tr>
              </thead>
              <tbody>
                {filteredModels.map((model) => (
                  <tr key={model.id}>
                    <td>
                      <Link className="text-primary" to={`/admin/models/${model.id}`}>
                        {model.name}
                      </Link>
                    </td>
                    <td>
                      <Badge variant="outline">
                        {protocolLabels(model.bindings.map((binding) => binding.protocol))}
                      </Badge>
                    </td>
                    <td>{t('modelMetadata.unknown')}</td>
                    <td>
                      {[
                        ...new Set(
                          model.bindings.map(
                            (b) =>
                              providerData?.find((p) => p.id === b.provider_id)?.name ??
                              b.provider_id,
                          ),
                        ),
                      ].join(t('common.listSeparator'))}
                    </td>
                    <td>
                      {model.status === 'active'
                        ? model.bindings.some((b) => b.ready && b.weight > 0)
                          ? t('adminModels.healthy')
                          : t('adminModels.pending')
                        : t('common.disabled')}
                    </td>
                    <td>{model.granted_user_ids.length}</td>
                    <td>
                      <ModelMonthlyCount view={monthly} modelId={model.id} />
                    </td>
                    <td className="whitespace-nowrap">
                      <ModelRecordedDate value={model.config_updated_at} />
                    </td>
                    <td>
                      <Link to={`/admin/models/${model.id}`}>{t('adminModels.details')}</Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </div>
          <ModelMonthlyContext view={monthly} />
        </>
      )}
      {selected && (
        <>
          <section aria-label={t('adminModels.infoLabel')} className="rounded-lg border">
            <header className="flex items-center justify-between gap-3 border-b px-6 py-4">
              <h2 className="font-semibold">
                {selected.name}{' '}
                <Badge variant="outline">
                  {selected.status === 'active'
                    ? selected.bindings.some((binding) => binding.ready && binding.weight > 0)
                      ? t('adminModels.healthy')
                      : t('adminModels.pending')
                    : t('common.disabled')}
                </Badge>
              </h2>
              <div className="flex items-center gap-3">
                <Button variant="outline" onClick={() => void detail.refetch()}>
                  {t('adminModels.refreshDetails')}
                </Button>
                <Button
                  disabled={!access.can('models.write')}
                  variant="outline"
                  onClick={() => open({ kind: 'rename', model: selected })}
                >
                  {t('adminModels.rename')}
                </Button>
              </div>
            </header>
            <div className="space-y-4 p-6">
              <dl className="grid grid-cols-2 gap-4 text-sm lg:grid-cols-4">
                <div>
                  <dt className="text-muted-foreground">{t('adminModels.modelID')}</dt>
                  <dd className="break-all">{selected.id}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{t('common.protocolType')}</dt>
                  <dd>{protocolLabels(selected.bindings.map((binding) => binding.protocol))}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{t('modelMetadata.capabilityType')}</dt>
                  <dd>{t('modelMetadata.unknown')}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{t('adminModels.bindings')}</dt>
                  <dd>{t('adminModels.bindingCount', { count: selected.bindings.length })}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{t('adminModels.members')}</dt>
                  <dd>{selected.granted_user_ids.length}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{t('modelMetadata.monthlyRequests')}</dt>
                  <dd>
                    <ModelMonthlyCount view={monthly} modelId={selected.id} />
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{t('modelMetadata.created')}</dt>
                  <dd>
                    <ModelRecordedDate value={selected.created_at} />
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{t('modelMetadata.updated')}</dt>
                  <dd>
                    <ModelRecordedDate value={selected.config_updated_at} />
                  </dd>
                </div>
              </dl>
              <ModelMonthlyContext view={monthly} />
              {selected.names.some((name) => !name.is_current) && (
                <section aria-label={t('aliasRetirement.names')} className="space-y-3">
                  <h3 className="text-sm font-semibold">{t('aliasRetirement.names')}</h3>
                  {selected.names
                    .filter((name) => !name.is_current)
                    .map((name) => (
                      <div
                        key={name.name}
                        className="flex flex-wrap items-center justify-between gap-3 text-sm"
                      >
                        <div className="flex flex-wrap items-center gap-3">
                          <code>{name.name}</code>
                          <Badge variant="outline">{t('aliasRetirement.configuredName')}</Badge>
                          <span className="text-muted-foreground">
                            {name.expires_at
                              ? t('aliasRetirement.deadline', {
                                  date: new Date(name.expires_at).toLocaleString(
                                    i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
                                  ),
                                })
                              : t('aliasRetirement.noDeadline')}
                          </span>
                        </div>
                        <Button
                          size="sm"
                          variant="ghost"
                          aria-label={t('aliasRetirement.actionName', { name: name.name })}
                          disabled={aliasLocked && aliasName !== name.name}
                          onClick={() => {
                            setAliasName(name.name)
                            setAliasOpen(true)
                          }}
                        >
                          {t('aliasRetirement.action')}
                        </Button>
                      </div>
                    ))}
                </section>
              )}
            </div>
          </section>
          <RoutingWeights
            key={selected.id}
            model={selected}
            draft={routingDraft?.values ?? routingValues(selected)}
            reviewRequired={
              !!routingDraft && routingDraft.identities !== routingIdentities(selected)
            }
            onDraftChange={(bindingId, value) => {
              if (!writeReady() || mutation.isPending) return
              setRoutingDraft((draft) => {
                const original = draft ?? {
                  identities: routingIdentities(selected),
                  values: routingValues(selected),
                }
                if (original.identities !== routingIdentities(selected)) return original
                return { ...original, values: { ...original.values, [bindingId]: value } }
              })
            }}
            onReviewCurrent={() => {
              if (writeReady() && !mutation.isPending) setRoutingDraft(null)
            }}
            actor={actor}
            generation={detailGeneration}
            providers={providerData}
            canWrite={access.can('models.write')}
            canAdd={access.can('providers.read')}
            pricesReadable={readable && access.can('prices.read')}
            pending={mutation.isPending}
            error={!action ? mutation.error : null}
            historyTriggerRef={weightHistoryTrigger}
            onHistory={() => {
              if (!weightHistoryReadReady() || mutation.isPending) return
              weightHistoryFocusOwner.current = {
                generation,
                birth: selected.created_at ?? null,
              }
              setWeightHistoryOpen(true)
            }}
            onSave={(weights) => {
              const current = cache.getQueryData<Model>(detailKey)
              if (
                mutation.isPending ||
                !writeReady() ||
                current?.id !== selected.id ||
                routingIdentities(current) !== routingIdentities(selected) ||
                (routingDraft && routingDraft.identities !== routingIdentities(current))
              )
                return
              mutation.mutate({
                path: `/admin/models/${selected.id}/weights`,
                method: 'put',
                data: { weights },
              })
            }}
            onAddBinding={(protocol) => {
              if (!routingReadReady()) return
              setRoutingProtocol(
                (protocol ?? selected.bindings[0]?.protocol ?? 'openai_chat') as RoutingProtocol,
              )
            }}
            onGrants={() => open({ kind: 'grants', model: selected })}
            refreshDetail={() => void detail.refetch()}
          />
        </>
      )}
      {modelId && (
        <RoutingWeightHistory
          key={`${actor}:${modelId}`}
          actor={actor}
          modelID={modelId}
          modelBirth={selected ? (selected.created_at ?? null) : undefined}
          generation={generation}
          permissionGeneration={permissionGeneration}
          resourceGeneration={detailGeneration}
          readable={readable && !!selected}
          canWrite={access.can('models.write')}
          readReady={weightHistoryReadReady}
          writeReady={writeReady}
          open={weightHistoryOpen}
          finalFocus={() => {
            const owner = weightHistoryFocusOwner.current
            const trigger = weightHistoryTrigger.current
            return mounted.current &&
              owner?.generation === generation &&
              owner.birth === (selected?.created_at ?? null) &&
              weightHistoryReadReady() &&
              trigger?.isConnected &&
              !trigger.disabled
              ? trigger
              : false
          }}
          onOpenChange={setWeightHistoryOpen}
          onSaved={() => {
            void cache.invalidateQueries({ queryKey: ['admin', 'models'] })
            void cache.invalidateQueries({ queryKey: ['models'] })
            void cache.invalidateQueries({
              queryKey: ['admin', 'model-weight-history', actor, modelId],
            })
          }}
        />
      )}
      {modelId && (
        <AliasRetirementDialog
          key={aliasName ?? 'none'}
          actor={actor}
          modelID={modelId}
          name={aliasName}
          open={aliasOpen}
          onOpenChange={setAliasOpen}
          onCaptureChange={setAliasLocked}
          generation={generation}
          resourceGeneration={detailGeneration}
          readable={
            readable &&
            !!selected &&
            !!aliasName &&
            selected.names.some((name) => name.name === aliasName)
          }
          writeReady={writeReady}
        />
      )}
      {routingProtocol && modelId && (
        <RoutingCandidates
          key={`${actor}:${modelId}:${routingProtocol}`}
          actor={actor}
          modelID={modelId}
          protocol={routingProtocol}
          generation={generation}
          resourceGeneration={detailGeneration}
          readable={readable && !!selected && access.can('providers.read')}
          canWrite={access.can('models.write')}
          readReady={routingReadReady}
          writeReady={writeReady}
          onProtocolChange={setRoutingProtocol}
          onClose={() => setRoutingProtocol(null)}
          onSaved={() => {
            void cache.invalidateQueries({ queryKey: ['admin', 'models'] })
            void cache.invalidateQueries({ queryKey: ['models'] })
          }}
        />
      )}
      <Dialog
        open={!!action && readable && !!selected}
        onOpenChange={(open) => {
          if (!open) setAction(null)
        }}
        busy={mutation.isPending}
        title={action ? titles[action.kind] : ''}
        description={
          action?.kind === 'grants'
            ? t('adminModels.grantsDescription')
            : action?.kind === 'weights'
              ? t('adminModels.weightsDescription')
              : action?.kind === 'rename'
                ? t('adminModels.renameDescription')
                : t('adminModels.bindingDescription')
        }
      >
        <form onSubmit={submit} className="space-y-5">
          <fieldset
            disabled={
              mutation.isPending ||
              !access.can('models.write') ||
              (action?.kind === 'rename' && renameUncertain)
            }
            className="space-y-5"
          >
            {action?.kind === 'create' && (
              <FormField label={t('common.modelName')}>
                <Input name="name" required maxLength={200} defaultValue="" />
              </FormField>
            )}
            {action?.kind === 'rename' && renameDraft && (
              <ModelRenameFields
                oldName={action.model.name}
                draft={renameDraft}
                onChange={(draft) => {
                  if (writeReady() && !mutation.isPending && !renameUncertain) setRenameDraft(draft)
                }}
              />
            )}
            {(action?.kind === 'create' || action?.kind === 'binding') && (
              <>
                <QueryState
                  pending={providers.isPending}
                  error={providers.error}
                  retry={() => void providers.refetch()}
                  empty={upstreamModels.length === 0}
                />
                <FormField label={t('common.upstreamModel')}>
                  <select
                    name="provider_model_id"
                    required
                    className="h-11 w-full rounded-md border bg-background px-3 text-sm"
                  >
                    <option value="">{t('adminModels.chooseUpstream')}</option>
                    {upstreamModels
                      .filter(
                        (upstream) =>
                          action.kind === 'create' ||
                          !action.model.bindings.some(
                            (binding) => binding.provider_model_id === upstream.id,
                          ),
                      )
                      .map((model) => (
                        <option key={model.id} value={model.id}>
                          {model.label}
                        </option>
                      ))}
                  </select>
                </FormField>
              </>
            )}
            {action?.kind === 'weights' &&
              action.model.bindings.map((binding) => (
                <FormField
                  key={binding.id}
                  label={t('adminModels.bindingLabel', {
                    name: binding.upstream_name,
                    status: binding.ready ? t('common.ready') : t('common.notReady'),
                  })}
                >
                  <Input
                    name={binding.id}
                    type="number"
                    min={0}
                    max={100}
                    step={1}
                    required
                    defaultValue={binding.weight}
                  />
                </FormField>
              ))}
            {action?.kind === 'grants' && (
              <>
                <QueryState
                  pending={grantees.isPending}
                  error={grantees.error}
                  retry={() => void grantees.refetch()}
                  empty={grantees.data?.length === 0}
                />
                {(grantsFresh ? grantees.data : [])?.map((user) => (
                  <label key={user.id} className="flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      name="user_ids"
                      value={user.id}
                      defaultChecked={action.model.granted_user_ids.includes(user.id)}
                    />
                    {user.name} <span className="text-muted-foreground">{user.email}</span>
                  </label>
                ))}
              </>
            )}
          </fieldset>
          {action?.kind === 'rename' && renameUncertain && (
            <p role="status" className="text-sm text-muted-foreground">
              {t('adminModels.renameUncertain')}
            </p>
          )}
          <ErrorNotice error={mutation.error} />
          <SaveButton
            pending={mutation.isPending}
            disabled={
              !readable ||
              !selected ||
              !access.can('models.write') ||
              (action?.kind === 'grants' && !grantsFresh)
            }
          >
            {t('common.save')}
          </SaveButton>
        </form>
      </Dialog>
    </Page>
  )
}

function AliasRetirementDialog({
  actor,
  modelID,
  name,
  open,
  onOpenChange,
  onCaptureChange,
  generation,
  resourceGeneration,
  readable,
  writeReady,
}: {
  actor: string
  modelID: string
  name: string | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onCaptureChange: (locked: boolean) => void
  generation: number
  resourceGeneration: number
  readable: boolean
  writeReady: () => boolean
}) {
  const { t, i18n } = useTranslation('catalog')
  const cache = useQueryClient()
  const [reason, setReason] = useState('')
  const [reviewed, setReviewed] = useState<ModelAliasRetirementReview | null>(null)
  const [reviewRequired, setReviewRequired] = useState(false)
  const [confirmation, setConfirmation] = useState<{
    review: ModelAliasRetirementReview
    reason: string
  } | null>(null)
  const [intent, setIntent] = useState<{ input: ModelAliasRetirementInput; etag: string } | null>(
    null,
  )
  const [result, setResult] = useState<ModelAliasRetirementResult | null>(null)
  const [notice, setNotice] = useState<
    'conflict' | 'failed' | 'unknown' | 'pending' | 'reasonError' | null
  >(null)
  const [pending, setPending] = useState(false)
  const submitting = useRef(false)
  const controller = useRef<AbortController | null>(null)
  const version = useRef(0)
  const queryKey = [
    'admin',
    'model-alias-retirement',
    actor,
    modelID,
    name,
    generation,
    resourceGeneration,
  ]
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => getModelAliasRetirement(modelID, name!, signal),
    enabled: readable && open && !!name,
    retry: false,
    gcTime: 0,
    staleTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const fresh = readable && query.isSuccess && !query.isFetching && !query.isError
  const current = fresh ? query.data : undefined
  const basis = reviewed ?? (!reviewRequired ? current : null)
  useLayoutEffect(() => {
    const epoch = ++version.current
    const active = controller.current
    active?.abort()
    controller.current = null
    submitting.current = false
    queueMicrotask(() => {
      if (epoch !== version.current) return
      setReviewed(null)
      setConfirmation(null)
      if (active) {
        setPending(false)
        setNotice('unknown')
      }
    })
  }, [generation, resourceGeneration, readable])
  useEffect(
    () => () => {
      version.current++
      controller.current?.abort()
    },
    [],
  )
  function freshReview() {
    const state = cache.getQueryState(queryKey)
    return readable && state?.status === 'success' && state.fetchStatus !== 'fetching'
      ? cache.getQueryData<ModelAliasRetirementReview>(queryKey)
      : undefined
  }
  async function review() {
    setConfirmation(null)
    setReviewed(null)
    const epoch = version.current
    const checked = await query.refetch()
    if (epoch === version.current && !checked.isError && checked.data && freshReview()) {
      setReviewed(checked.data)
      setReviewRequired(false)
      if (!intent) setNotice(null)
    }
  }
  async function dispatch(
    captured: { input: ModelAliasRetirementInput; etag: string },
    retry: boolean,
  ) {
    if (submitting.current || !writeReady() || !freshReview()) return
    if (!retry && (!freshReview()!.can_retire || freshReview()!.etag !== captured.etag)) return
    submitting.current = true
    setPending(true)
    setConfirmation(null)
    setIntent(captured)
    onCaptureChange(true)
    setNotice('unknown')
    const epoch = version.current
    const active = new AbortController()
    controller.current = active
    try {
      const response = await retireModelAlias(
        modelID,
        captured.input,
        captured.etag,
        cache.getQueryData<Session>(sessionKey)!.csrf_token,
        active.signal,
      )
      if (active.signal.aborted || epoch !== version.current || !writeReady() || !freshReview())
        return
      setResult(response)
      if (response.runtime_applied) {
        setIntent(null)
        onCaptureChange(false)
        setNotice(null)
      } else setNotice('pending')
      void cache.invalidateQueries({ queryKey: ['admin', 'models', 'detail', actor, modelID] })
      void cache.invalidateQueries({ queryKey: ['models'] })
      void query.refetch()
    } catch (error) {
      if (active.signal.aborted || epoch !== version.current) return
      const status = axios.isAxiosError(error) ? error.response?.status : undefined
      if (!retry && status && [400, 403, 404, 409].includes(status)) {
        setIntent(null)
        onCaptureChange(false)
        setReviewRequired(true)
        setReviewed(null)
        setNotice(status === 409 ? 'conflict' : 'failed')
      } else setNotice('unknown')
      if (status && [403, 404, 409].includes(status))
        void cache.invalidateQueries({ queryKey: ['permissions', actor] })
    } finally {
      if (controller.current === active) {
        controller.current = null
        submitting.current = false
        setPending(false)
      }
    }
  }
  const date = (value: string) =>
    new Date(value).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
  return (
    <Dialog
      open={open && readable}
      onOpenChange={onOpenChange}
      busy={pending}
      title={t(confirmation ? 'aliasRetirement.confirmTitle' : 'aliasRetirement.title')}
      description={t('aliasRetirement.description')}
    >
      <QueryState pending={query.isFetching} error={query.error} retry={() => void review()} />
      {current && (
        <div className="space-y-4">
          <dl className="space-y-2 text-sm">
            <div>
              <dt className="text-muted-foreground">{t('aliasRetirement.name')}</dt>
              <dd className="break-all font-mono">{current.alias.name}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('aliasRetirement.currentName')}</dt>
              <dd className="break-all font-mono">{current.current_name}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('aliasRetirement.state')}</dt>
              <dd>{t(`aliasRetirement.states.${current.state}`)}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('aliasRetirement.recordedDeadline')}</dt>
              <dd>
                {current.alias.expires_at
                  ? date(current.alias.expires_at)
                  : t('aliasRetirement.noDeadline')}
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('aliasRetirement.observed')}</dt>
              <dd>{date(current.observed_at)}</dd>
            </div>
          </dl>
          <p className="text-sm text-muted-foreground">
            {t(
              current.runtime_applied
                ? 'aliasRetirement.runtimeApplied'
                : 'aliasRetirement.runtimeUnconfirmed',
            )}
          </p>
          {notice && (
            <p role="alert" className="text-sm text-destructive">
              {t(`aliasRetirement.${notice}`)}
            </p>
          )}
          {result && (
            <p role="status" className="text-sm">
              {t(
                result.runtime_applied
                  ? 'aliasRetirement.responseConfirmed'
                  : 'aliasRetirement.responsePending',
              )}
            </p>
          )}
          {intent ? (
            <>
              <p className="break-all text-sm">
                {t('aliasRetirement.capturedReason', { reason: intent.input.reason })}
              </p>
              <div className="flex flex-wrap justify-end gap-3">
                <Button variant="outline" onClick={() => void review()} disabled={pending}>
                  {t('aliasRetirement.review')}
                </Button>
                <Button
                  variant="outline"
                  className="border-destructive text-destructive hover:bg-destructive/10"
                  onClick={() => void dispatch(intent, true)}
                  disabled={pending || !writeReady()}
                >
                  {t(pending ? 'aliasRetirement.submitting' : 'aliasRetirement.retry')}
                </Button>
              </div>
            </>
          ) : confirmation ? (
            <>
              <p className="text-sm">
                {t('aliasRetirement.confirmTarget', {
                  name: confirmation.review.alias.name,
                  current: confirmation.review.current_name,
                })}
              </p>
              <p className="break-all text-sm">
                {t('aliasRetirement.capturedReason', { reason: confirmation.reason })}
              </p>
              <div className="flex justify-end gap-3">
                <Button variant="outline" onClick={() => setConfirmation(null)}>
                  {t('aliasRetirement.cancel')}
                </Button>
                <Button
                  variant="outline"
                  className="border-destructive text-destructive hover:bg-destructive/10"
                  disabled={
                    !writeReady() ||
                    current.etag !== confirmation.review.etag ||
                    !current.can_retire
                  }
                  onClick={() =>
                    void dispatch(
                      {
                        input: {
                          name: confirmation.review.alias.name,
                          reason: confirmation.reason,
                        },
                        etag: confirmation.review.etag,
                      },
                      false,
                    )
                  }
                >
                  {t('aliasRetirement.confirm')}
                </Button>
              </div>
            </>
          ) : (
            <form
              className="space-y-4"
              onSubmit={(event) => {
                event.preventDefault()
                if (!validModelAliasReason(reason)) {
                  setNotice('reasonError')
                  return
                }
                const live = freshReview()
                if (
                  !writeReady() ||
                  !live?.can_retire ||
                  !basis ||
                  reviewRequired ||
                  basis.etag !== live.etag
                )
                  return
                setNotice((value) => (value === 'reasonError' ? null : value))
                setConfirmation({ review: structuredClone(basis), reason })
              }}
            >
              {!current.can_retire && (
                <p className="text-sm text-muted-foreground">{t('aliasRetirement.notEligible')}</p>
              )}
              <FormField label={t('aliasRetirement.reason')}>
                <Input
                  name="alias_reason"
                  value={reason}
                  onValueChange={setReason}
                  disabled={!current.can_retire || !writeReady()}
                  autoComplete="off"
                />
              </FormField>
              <div className="flex flex-wrap justify-end gap-3">
                <Button variant="outline" onClick={() => void review()}>
                  {t('aliasRetirement.review')}
                </Button>
                <Button
                  type="submit"
                  variant="outline"
                  className="border-destructive text-destructive hover:bg-destructive/10"
                  disabled={
                    !current.can_retire ||
                    !writeReady() ||
                    reviewRequired ||
                    basis?.etag !== current.etag
                  }
                >
                  {t('aliasRetirement.stop')}
                </Button>
              </div>
            </form>
          )}
        </div>
      )}
    </Dialog>
  )
}

function useModelReadGeneration(key: unknown[]) {
  const cache = useQueryClient()
  const hash = JSON.stringify(key)
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (
          JSON.stringify(event.query.queryKey) === hash &&
          event.type === 'updated' &&
          event.action.type === 'success' &&
          !event.action.manual
        )
          notify()
      }),
    [cache, hash],
  )
  const snapshot = useCallback(
    () => cache.getQueryState(JSON.parse(hash))?.dataUpdateCount ?? 0,
    [cache, hash],
  )
  return useSyncExternalStore(subscribe, snapshot, snapshot)
}

function useModelReadFresh(key: readonly unknown[]) {
  const cache = useQueryClient()
  const hash = JSON.stringify(key)
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (JSON.stringify(event.query.queryKey) === hash) notify()
      }),
    [cache, hash],
  )
  const snapshot = useCallback(() => {
    const state = cache.getQueryState(JSON.parse(hash))
    return state?.status === 'success' && state.fetchStatus === 'idle' && !state.isInvalidated
  }, [cache, hash])
  return useSyncExternalStore(subscribe, snapshot, snapshot)
}
