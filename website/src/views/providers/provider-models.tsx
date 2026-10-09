import { useRef, useState } from 'react'
import { Link } from 'react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { listProviders } from '@/api/catalog'
import { getProviderModelBindings } from '@/api/provider-model-bindings'
import type { ProviderModelBindings } from '@/types/provider-model-bindings'
import { getPermissions } from '@/api/governance'
import type { Provider } from '@/types/catalog'
import type { Session } from '@/types/auth'
import { sessionKey, type useSession } from '@/hooks/use-auth'
import { QueryState } from '@/components/app/CatalogUI'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Menu, MenuItem } from '@/components/ui/menu'
import { Table } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { protocolLabel } from '@/lib/protocols'
import { useConnectionQueryRevision } from './connection-authority'
import { ProviderModelRowMenu, ProviderModelStatusConfirmation } from './provider-model-row-actions'

interface Props {
  providerId: string
  session: ReturnType<typeof useSession>
  onAdd: (connectionId: string) => void
}

export default function ProviderModelTable(props: Props) {
  return <Models key={`${props.session.data?.user.id ?? ''}:${props.providerId}`} {...props} />
}

function Models({ providerId, session, onAdd }: Props) {
  const { t } = useTranslation('catalog')
  const cache = useQueryClient()
  const actor = session.data?.user.id ?? ''
  const authState = cache.getQueryState<Session | null>(sessionKey)
  const generation = authState?.dataUpdateCount ?? 0
  const sessionFresh =
    session.isSuccess &&
    !session.isFetching &&
    !!actor &&
    authState?.data?.user.id === actor &&
    authState.status === 'success' &&
    authState.fetchStatus === 'idle' &&
    !authState.error &&
    !authState.isInvalidated
  const permissionsKey = ['permissions', actor, 'provider-models', generation]
  const access = useQuery({
    queryKey: permissionsKey,
    queryFn: ({ signal }) => getPermissions(signal),
    enabled: sessionFresh,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const authority = useConnectionQueryRevision([sessionKey, permissionsKey])
  const permissionState = cache.getQueryState<string[]>(permissionsKey)
  const readable =
    sessionFresh &&
    access.isSuccess &&
    !access.isFetching &&
    authority.snapshot() === authority.revision &&
    permissionState?.status === 'success' &&
    permissionState.fetchStatus === 'idle' &&
    !permissionState.error &&
    !permissionState.isInvalidated &&
    permissionState.data?.includes('providers.read') === true
  const catalogueKey = [
    'admin',
    'providers',
    actor,
    providerId,
    'provider-models',
    authority.revision,
  ]
  const catalogue = useQuery({
    queryKey: catalogueKey,
    queryFn: ({ signal }) => listProviders(signal),
    enabled: readable,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const context = useConnectionQueryRevision([catalogueKey])
  const fresh = () => {
    const auth = cache.getQueryState<Session | null>(sessionKey)
    const permission = cache.getQueryState<string[]>(permissionsKey)
    const current = cache.getQueryState<Provider[]>(catalogueKey)
    return (
      authority.snapshot() === authority.revision &&
      context.snapshot() === context.revision &&
      !!actor &&
      auth?.data?.user.id === actor &&
      auth.dataUpdateCount === generation &&
      [auth, permission, current].every(
        (state) =>
          state?.status === 'success' &&
          state.fetchStatus === 'idle' &&
          !state.isInvalidated &&
          !state.error,
      ) &&
      permission?.data?.includes('providers.read') === true
    )
  }
  const provider = fresh() ? catalogue.data?.find((item) => item.id === providerId) : undefined
  const [query, setQuery] = useState('')
  const [connection, setConnection] = useState('all')
  const [enabled, setEnabled] = useState('all')
  const [binding, setBinding] = useState<'all' | 'bound' | 'unbound'>('all')
  const [statusTarget, setStatusTarget] = useState<string | null>(null)
  const [statusOpen, setStatusOpen] = useState(false)
  const [statusLocked, setStatusLocked] = useState(false)
  const [statusSaved, setStatusSaved] = useState(false)
  const statusSelection = useRef<string | null>(null)
  const statusVisible = useRef(false)
  const statusActivity = useRef({ pending: false, uncertain: false })
  const actionTriggers = useRef(new Map<string, HTMLButtonElement>())
  const allRows =
    provider?.connections.flatMap((item) =>
      item.provider_models.map((model) => ({ connection: item, model })),
    ) ?? []
  const bindingsKey = [
    'admin',
    'providers',
    actor,
    providerId,
    'model-bindings',
    authority.revision,
    context.revision,
  ]
  const canReadBindings = fresh() && access.data?.includes('models.read_all') === true
  const bindings = useQuery({
    queryKey: bindingsKey,
    queryFn: ({ signal }) => getProviderModelBindings(providerId, signal),
    enabled: canReadBindings && !!provider,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const bindingRevision = useConnectionQueryRevision([bindingsKey])
  const bindingFresh = () => {
    const current = cache.getQueryState<ProviderModelBindings>(bindingsKey)
    return (
      fresh() &&
      access.data?.includes('models.read_all') === true &&
      bindingRevision.snapshot() === bindingRevision.revision &&
      current?.status === 'success' &&
      current.fetchStatus === 'idle' &&
      !current.error &&
      !current.isInvalidated &&
      current.data?.provider_id === providerId
    )
  }
  const projection = bindingFresh() ? bindings.data : undefined
  const projectedRows = new Map(
    projection?.items.map((item) => [item.provider_model_id, item]) ?? [],
  )
  const matched =
    !!projection &&
    projectedRows.size === allRows.length &&
    new Set(allRows.map(({ model }) => model.id)).size === allRows.length &&
    allRows.every(
      ({ connection, model }) => projectedRows.get(model.id)?.connection_id === connection.id,
    )
  const bindingRows = matched ? projectedRows : null
  const currentBindings = () => bindingFresh() && matched
  const normalizedQuery = query.trim().toLowerCase()
  const rows = allRows.filter(
    (row) =>
      (!normalizedQuery || row.model.upstream_name.toLowerCase().includes(normalizedQuery)) &&
      (connection === 'all' || row.connection.id === connection) &&
      (enabled === 'all' || row.model.enabled === (enabled === 'enabled')) &&
      (!bindingRows ||
        binding === 'all' ||
        bindingRows.get(row.model.id)!.binding_count > 0 === (binding === 'bound')),
  )
  const declaration = (value: boolean) =>
    typeof value !== 'boolean'
      ? t('providers.unknown')
      : t(value ? 'providerModels.declared' : 'providerModels.notDeclared')
  const writable = () => fresh() && access.data?.includes('providers.write') === true
  const currentModel = allRows.find(({ model }) => model.id === statusTarget)?.model
  return (
    <div className="space-y-4">
      <QueryState
        pending={session.isFetching || access.isFetching || (readable && catalogue.isFetching)}
        error={session.error ?? access.error ?? catalogue.error}
        retry={() => {
          if (!sessionFresh) void session.refetch()
          else if (access.isError) void access.refetch()
          else void catalogue.refetch()
        }}
      />
      {sessionFresh && access.isSuccess && !access.isFetching && !readable && (
        <p role="alert">{t('providerModels.denied')}</p>
      )}
      {fresh() && !provider && <p role="alert">{t('providerModels.missing')}</p>}
      {provider && (
        <>
          {statusSaved && <p role="status">{t('providerModels.statusSaved')}</p>}
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div
              role="group"
              aria-label={t('providerModels.filters')}
              className="flex flex-wrap items-center gap-2"
            >
              <Input
                type="search"
                autoComplete="off"
                aria-label={t('providerModels.search')}
                placeholder={t('providerModels.search')}
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                className="h-10 w-full sm:w-[220px]"
              />
              {provider.connections.length > 1 && (
                <Menu
                  label={t('providerModels.connectionFilter')}
                  trigger={
                    connection === 'all'
                      ? t('providers.allConnections')
                      : (provider.connections.find((item) => item.id === connection)?.name ??
                        connection)
                  }
                  triggerClassName="w-[180px] border"
                  side="bottom"
                  align="start"
                >
                  <MenuItem onClick={() => setConnection('all')}>
                    {t('providers.allConnections')}
                  </MenuItem>
                  {provider.connections.map((item) => (
                    <MenuItem key={item.id} onClick={() => setConnection(item.id)}>
                      {item.name}
                    </MenuItem>
                  ))}
                </Menu>
              )}
              <Menu
                label={t('providerModels.bindingFilter')}
                trigger={t(
                  bindingRows ? `providerModels.${binding}Bindings` : 'providerModels.allBindings',
                )}
                triggerClassName="w-[180px] border"
                side="bottom"
                align="start"
              >
                <MenuItem onClick={() => setBinding('all')}>
                  {t('providerModels.allBindings')}
                </MenuItem>
                <MenuItem
                  disabled={!bindingRows}
                  onClick={() => {
                    if (currentBindings()) setBinding('bound')
                  }}
                >
                  {t('providerModels.boundBindings')}
                </MenuItem>
                <MenuItem
                  disabled={!bindingRows}
                  onClick={() => {
                    if (currentBindings()) setBinding('unbound')
                  }}
                >
                  {t('providerModels.unboundBindings')}
                </MenuItem>
              </Menu>
              <Menu
                label={t('providerModels.enabledFilter')}
                trigger={
                  enabled === 'all'
                    ? t('providers.allEnabledStates')
                    : t(enabled === 'enabled' ? 'providers.enabled' : 'common.disabled')
                }
                triggerClassName="w-[150px] border"
                side="bottom"
                align="start"
              >
                <MenuItem onClick={() => setEnabled('all')}>
                  {t('providers.allEnabledStates')}
                </MenuItem>
                <MenuItem onClick={() => setEnabled('enabled')}>{t('providers.enabled')}</MenuItem>
                <MenuItem onClick={() => setEnabled('disabled')}>{t('common.disabled')}</MenuItem>
              </Menu>
            </div>
            {provider.connections.map((item) => (
              <Button
                key={item.id}
                disabled={!writable()}
                onClick={() => {
                  if (writable() && provider.connections.some((current) => current.id === item.id))
                    onAdd(item.id)
                }}
              >
                {t('providers.addModelFor', { name: item.name })}
              </Button>
            ))}
          </div>
          <p role="status" className="text-sm text-muted-foreground">
            {t('providerModels.filtered', { count: rows.length, total: allRows.length })}
          </p>
          <p className="text-sm text-muted-foreground">{t('providerModels.declarationHelp')}</p>
          <p className="text-sm text-muted-foreground">{t('providerModels.bindingHelp')}</p>
          {!bindingRows && (
            <div
              className="flex items-center gap-2 text-sm text-muted-foreground"
              role={bindings.isError ? 'alert' : 'status'}
            >
              <span>
                {t(
                  !canReadBindings
                    ? 'providerModels.bindingsRestricted'
                    : bindings.isFetching || bindings.isPending
                      ? 'providerModels.bindingsLoading'
                      : projection
                        ? 'providerModels.bindingsChanged'
                        : 'providerModels.bindingsUnavailable',
                )}
              </span>
              {canReadBindings && !bindings.isFetching && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    if (!fresh() || !access.data?.includes('models.read_all')) return
                    if (projection && !matched) void catalogue.refetch()
                    else void bindings.refetch()
                  }}
                >
                  {t('providerModels.refreshBindings')}
                </Button>
              )}
            </div>
          )}
          <Table aria-label={t('providerModels.list')}>
            <thead>
              <tr>
                <th>{t('providers.modelIdentifier')}</th>
                <th>{t('providerModels.models')}</th>
                <th>{t('providers.connection')}</th>
                <th>{t('common.protocolType')}</th>
                <th>{t('providerModels.enabled')}</th>
                <th>{t('providerModels.image')}</th>
                <th>{t('providerModels.pdf')}</th>
                <th>{t('providerModels.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(({ connection: item, model }) => (
                <tr key={model.id}>
                  <td>
                    <Link
                      className="text-primary"
                      to={`/admin/providers/${encodeURIComponent(providerId)}/models/${encodeURIComponent(model.id)}`}
                      onClick={(event) => {
                        if (!fresh()) event.preventDefault()
                      }}
                    >
                      {model.upstream_name}
                    </Link>
                  </td>
                  <td>
                    {!bindingRows ? (
                      t('providers.unknown')
                    ) : bindingRows.get(model.id)!.models.length === 0 ? (
                      t('providerModels.unbound')
                    ) : (
                      <div className="flex flex-col">
                        {bindingRows.get(model.id)!.models.map((logical) => (
                          <Link
                            key={logical.id}
                            className="text-primary"
                            to={`/admin/models/${encodeURIComponent(logical.id)}`}
                            onClick={(event) => {
                              if (!currentBindings()) event.preventDefault()
                            }}
                          >
                            {logical.name ?? logical.id}
                          </Link>
                        ))}
                      </div>
                    )}
                  </td>
                  <td>{item.name}</td>
                  <td>{protocolLabel(item.protocol)}</td>
                  <td>
                    <Badge variant={model.enabled === true ? 'success' : 'outline'}>
                      {typeof model.enabled === 'boolean'
                        ? t(model.enabled ? 'providers.enabled' : 'common.disabled')
                        : t('providers.unknown')}
                    </Badge>
                  </td>
                  <td>{declaration(model.supports_image_input)}</td>
                  <td>{declaration(model.supports_pdf_input)}</td>
                  <td>
                    <ProviderModelRowMenu
                      providerId={providerId}
                      model={model}
                      readable={fresh}
                      writable={() => writable() && (!statusLocked || statusTarget === model.id)}
                      bindings={bindingRows?.get(model.id)?.models}
                      bindingsFresh={currentBindings}
                      triggerRef={(node) => {
                        if (node) actionTriggers.current.set(model.id, node)
                        else actionTriggers.current.delete(model.id)
                      }}
                      onStatus={() => {
                        if (
                          !writable() ||
                          statusActivity.current.pending ||
                          (statusActivity.current.uncertain && statusSelection.current !== model.id)
                        )
                          return
                        statusSelection.current = model.id
                        statusVisible.current = true
                        setStatusTarget(model.id)
                        setStatusOpen(true)
                        setStatusSaved(false)
                      }}
                    />
                  </td>
                </tr>
              ))}
              {rows.length === 0 && (
                <tr>
                  <td colSpan={8}>{t('providerModels.empty')}</td>
                </tr>
              )}
            </tbody>
          </Table>
        </>
      )}
      {statusTarget && (
        <ProviderModelStatusConfirmation
          key={statusTarget}
          actor={actor}
          model={currentModel}
          open={statusOpen}
          readable={fresh}
          writable={writable}
          permissionsKey={permissionsKey}
          catalogueKey={catalogueKey}
          onClose={() => {
            if (statusSelection.current !== statusTarget || statusActivity.current.pending) return
            statusVisible.current = false
            setStatusOpen(false)
            if (!statusActivity.current.uncertain) {
              statusSelection.current = null
              setStatusTarget(null)
            }
          }}
          isCurrentTarget={() => statusSelection.current === statusTarget && statusVisible.current}
          onActivity={(activity) => {
            if (statusSelection.current !== statusTarget) return
            statusActivity.current = activity
            setStatusLocked(activity.pending || activity.uncertain)
          }}
          refresh={() => void catalogue.refetch()}
          finalFocus={() => actionTriggers.current.get(statusTarget) ?? false}
          onSaved={() => {
            if (statusSelection.current !== statusTarget || !statusVisible.current || !writable())
              return
            statusSelection.current = null
            statusVisible.current = false
            statusActivity.current = { pending: false, uncertain: false }
            setStatusTarget(null)
            setStatusOpen(false)
            setStatusLocked(false)
            setStatusSaved(true)
            void cache.invalidateQueries({ queryKey: ['admin', 'providers'], exact: true })
            void cache.invalidateQueries({ queryKey: ['admin', 'providers', actor] })
          }}
        />
      )}
    </div>
  )
}
