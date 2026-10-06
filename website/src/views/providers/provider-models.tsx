import { useState } from 'react'
import { Link } from 'react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { listProviders } from '@/api/catalog'
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
  const allRows =
    provider?.connections.flatMap((item) =>
      item.provider_models.map((model) => ({ connection: item, model })),
    ) ?? []
  const normalizedQuery = query.trim().toLowerCase()
  const rows = allRows.filter(
    (row) =>
      (!normalizedQuery || row.model.upstream_name.toLowerCase().includes(normalizedQuery)) &&
      (connection === 'all' || row.connection.id === connection) &&
      (enabled === 'all' || row.model.enabled === (enabled === 'enabled')),
  )
  const declaration = (value: boolean) =>
    typeof value !== 'boolean'
      ? t('providers.unknown')
      : t(value ? 'providerModels.declared' : 'providerModels.notDeclared')
  const writable = () => fresh() && access.data?.includes('providers.write') === true
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
          <Table aria-label={t('providerModels.list')}>
            <thead>
              <tr>
                <th>{t('providers.modelIdentifier')}</th>
                <th>{t('providers.connection')}</th>
                <th>{t('common.protocolType')}</th>
                <th>{t('providerModels.enabled')}</th>
                <th>{t('providerModels.image')}</th>
                <th>{t('providerModels.pdf')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(({ connection: item, model }) => (
                <tr key={model.id}>
                  <td>
                    <Link
                      className="text-primary"
                      to={`/admin/providers/${encodeURIComponent(providerId)}/models/${encodeURIComponent(model.id)}`}
                    >
                      {model.upstream_name}
                    </Link>
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
                </tr>
              ))}
              {rows.length === 0 && (
                <tr>
                  <td colSpan={6}>{t('providerModels.empty')}</td>
                </tr>
              )}
            </tbody>
          </Table>
        </>
      )}
    </div>
  )
}
