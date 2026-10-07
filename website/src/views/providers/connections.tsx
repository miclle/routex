import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { MoreHorizontal } from 'lucide-react'
import { listProviders } from '@/api/catalog'
import { getPermissions } from '@/api/governance'
import type { Provider } from '@/types/catalog'
import type { Session } from '@/types/auth'
import type { useSession } from '@/hooks/use-auth'
import { sessionKey } from '@/hooks/use-auth'
import { useUncertainIntents } from '@/hooks/use-uncertain-intents'
import { QueryState } from '@/components/app/CatalogUI'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Menu, MenuItem } from '@/components/ui/menu'
import { Table } from '@/components/ui/table'
import { protocolLabel } from '@/lib/protocols'
import { ConnectionEgressControl } from '@/views/egress/connection'
import ConnectionMetadataEditor from './connection-metadata'
import ConnectionStatusEditor from './connection-status'
import { useConnectionQueryRevision } from './connection-authority'

interface Props {
  providerId: string
  session: ReturnType<typeof useSession>
  onAdd: () => void
}
const protocols = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_generate_content',
] as const

export default function ConnectionTable({ providerId, session, onAdd }: Props) {
  const { t } = useTranslation('catalog')
  const cache = useQueryClient()
  const shared = useUncertainIntents()
  const actor = session.data?.user.id ?? ''
  const authState = cache.getQueryState<Session | null>(sessionKey)
  const generation = authState?.dataUpdateCount ?? 0
  const sessionFresh =
    session.isSuccess && !session.isFetching && !!actor && !authState?.isInvalidated
  const permissionsKey = ['permissions', actor, 'connections', generation]
  const access = useQuery({
    queryKey: permissionsKey,
    queryFn: ({ signal }) => getPermissions(signal),
    enabled: sessionFresh,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const readable =
    sessionFresh &&
    access.isSuccess &&
    !access.isFetching &&
    !cache.getQueryState(permissionsKey)?.isInvalidated &&
    access.data.includes('providers.read')
  const catalogueKey = ['admin', 'providers', actor, providerId, 'connections', generation]
  const catalogue = useQuery({
    queryKey: catalogueKey,
    queryFn: ({ signal }) => listProviders(signal),
    enabled: readable,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const authority = useConnectionQueryRevision([sessionKey, permissionsKey, catalogueKey])
  const fresh = () => {
    const auth = cache.getQueryState<Session | null>(sessionKey)
    const permission = cache.getQueryState<string[]>(permissionsKey)
    const context = cache.getQueryState<Provider[]>(catalogueKey)
    return (
      authority.snapshot() === authority.revision &&
      !!actor &&
      auth?.data?.user.id === actor &&
      auth.dataUpdateCount === generation &&
      [auth, permission, context].every(
        (state) =>
          state?.status === 'success' &&
          state.fetchStatus === 'idle' &&
          !state.isInvalidated &&
          !state.error,
      ) &&
      permission?.data?.includes('providers.read') === true
    )
  }
  const provider = fresh() ? catalogue.data?.find((row) => row.id === providerId) : undefined
  const writable = () => fresh() && access.data?.includes('providers.write') === true
  const retained = sessionFresh ? shared?.recover(actor) : null
  const recovered =
    retained?.kind === 'connection-name' && retained.payload.provider_id === providerId
      ? retained
      : null
  const [boundary, setBoundary] = useState({ owner: shared, version: 0 })
  if (boundary.owner !== shared) setBoundary({ owner: shared, version: boundary.version + 1 })
  const [statusSavedFor, setStatusSavedFor] = useState<string | null>(null)
  const [savedFor, setSavedFor] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [protocol, setProtocol] = useState<string>('all')
  const [status, setStatus] = useState<'all' | 'enabled' | 'disabled'>('all')
  const [statusEditor, setStatusEditor] = useState<{
    actor: string
    target: string
    enabled: boolean
    open: boolean
  } | null>(null)
  if (statusEditor && statusEditor.actor !== actor) setStatusEditor(null)
  const recoveredStatus =
    retained?.kind === 'connection-status' && retained.payload.provider_id === providerId
      ? retained
      : null
  if (recoveredStatus && statusEditor?.target !== recoveredStatus.payload.connection_id)
    setStatusEditor({
      actor,
      target: recoveredStatus.payload.connection_id,
      enabled: recoveredStatus.payload.input.enabled,
      open: false,
    })
  const [editor, setEditor] = useState<{ actor: string; target: string; open: boolean } | null>(
    null,
  )
  if (editor && editor.actor !== actor) setEditor(null)
  if (recovered && editor?.target !== recovered.payload.connection_id)
    setEditor({ actor, target: recovered.payload.connection_id, open: false })
  const target = editor?.actor === actor ? editor.target : ''
  const row = provider?.connections.find((item) => item.id === target)
  const normalized = query.trim().toLowerCase()
  const rows = provider?.connections.filter(
    (item) =>
      (!normalized || item.name.toLowerCase().includes(normalized)) &&
      (protocol === 'all' || item.protocol === protocol) &&
      (status === 'all' || item.enabled === (status === 'enabled')),
  )
  const ready = !!provider && fresh()
  const close = () => setEditor((current) => (current ? { ...current, open: false } : current))
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
        <p role="alert">{t('connectionMetadata.denied')}</p>
      )}
      {ready && (
        <>
          {savedFor === actor && <p role="status">{t('connectionMetadata.saved')}</p>}
          {statusSavedFor === actor && <p role="status">{t('connectionStatus.saved')}</p>}
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div
              role="group"
              aria-label={t('connectionMetadata.filters')}
              className="flex flex-wrap items-center gap-2"
            >
              <Input
                type="search"
                autoComplete="off"
                aria-label={t('connectionMetadata.search')}
                placeholder={t('connectionMetadata.search')}
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                className="h-10 w-full sm:w-[240px]"
              />
              <Menu
                label={t('connectionMetadata.protocolFilter')}
                trigger={
                  protocol === 'all'
                    ? t('connectionMetadata.allProtocols')
                    : protocolLabel(protocol)
                }
                side="bottom"
                align="start"
              >
                <MenuItem onClick={() => setProtocol('all')}>
                  {t('connectionMetadata.allProtocols')}
                </MenuItem>
                {protocols.map((value) => (
                  <MenuItem key={value} onClick={() => setProtocol(value)}>
                    {protocolLabel(value)}
                  </MenuItem>
                ))}
              </Menu>
            </div>
            <Menu
              label={t('connectionStatus.filter')}
              trigger={t(
                status === 'all'
                  ? 'connectionStatus.all'
                  : status === 'enabled'
                    ? 'connectionStatus.enabled'
                    : 'connectionStatus.disabled',
              )}
            >
              <MenuItem onClick={() => setStatus('all')}>{t('connectionStatus.all')}</MenuItem>
              <MenuItem onClick={() => setStatus('enabled')}>
                {t('connectionStatus.enabled')}
              </MenuItem>
              <MenuItem onClick={() => setStatus('disabled')}>
                {t('connectionStatus.disabled')}
              </MenuItem>
            </Menu>
            <Button disabled={!writable()} onClick={() => writable() && onAdd()}>
              {t('providers.addConnection')}
            </Button>
          </div>
          <p role="status" className="text-sm text-muted-foreground">
            {t('connectionMetadata.filtered', {
              count: rows?.length ?? 0,
              total: provider.connections.length,
            })}
          </p>
          {recovered && !editor?.open && row && (
            <p className="flex flex-wrap items-center gap-2 text-sm" role="status">
              {t('connectionMetadata.retained')}
              <Button
                variant="outline"
                size="sm"
                onClick={() => setEditor({ actor, target: row.id, open: true })}
              >
                {t('connectionMetadata.resume')}
              </Button>
            </p>
          )}
          <Table aria-label={t('connectionMetadata.list')}>
            <thead>
              <tr>
                <th>{t('common.connectionName')}</th>
                <th>{t('connectionStatus.status')}</th>
                <th>{t('common.protocolType')}</th>
                <th>{t('common.baseURL')}</th>
                <th>{t('egress:selection')}</th>
                <th>{t('common.credentials')}</th>
                <th>{t('common.models')}</th>
                <th>{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {rows?.map((item) => (
                <tr key={item.id}>
                  <td>{item.name}</td>
                  <td>
                    {t(
                      item.enabled === true
                        ? 'connectionStatus.enabled'
                        : item.enabled === false
                          ? 'connectionStatus.disabled'
                          : 'common.unknown',
                    )}
                  </td>
                  <td>{protocolLabel(item.protocol)}</td>
                  <td className="break-all">{item.base_url}</td>
                  <td>
                    <ConnectionEgressControl
                      connection={item}
                      authority={{
                        session: () => cache.getQueryData<Session>(sessionKey),
                        canWrite: writable,
                        isCurrent: fresh,
                      }}
                    />
                  </td>
                  <td>{item.credentials.length}</td>
                  <td>{item.provider_models.length}</td>
                  <td>
                    <Menu
                      label={t('connectionMetadata.actions', { name: item.name })}
                      trigger={<MoreHorizontal className="size-4" aria-hidden="true" />}
                      side="bottom"
                      align="end"
                      triggerClassName="h-8 w-8 justify-center px-0"
                    >
                      <MenuItem
                        disabled={
                          !writable() ||
                          (!!retained &&
                            (!recovered || recovered.payload.connection_id !== item.id))
                        }
                        onClick={() => {
                          if (
                            writable() &&
                            (!retained || recovered?.payload.connection_id === item.id)
                          )
                            setEditor({ actor, target: item.id, open: true })
                        }}
                      >
                        {t('connectionMetadata.edit')}
                      </MenuItem>
                      <MenuItem
                        disabled={!writable() || typeof item.enabled !== 'boolean' || !!retained}
                        onClick={() => {
                          if (writable() && typeof item.enabled === 'boolean' && !retained)
                            setStatusEditor({
                              actor,
                              target: item.id,
                              enabled: !item.enabled,
                              open: true,
                            })
                        }}
                      >
                        {t(item.enabled ? 'connectionStatus.disable' : 'connectionStatus.enable')}
                      </MenuItem>
                    </Menu>
                  </td>
                </tr>
              ))}
              {rows?.length === 0 && (
                <tr>
                  <td colSpan={8} className="text-muted-foreground">
                    {t('connectionMetadata.empty')}
                  </td>
                </tr>
              )}
            </tbody>
          </Table>
        </>
      )}
      {statusEditor && (
        <ConnectionStatusEditor
          key={JSON.stringify([actor, providerId, statusEditor.target, boundary.version])}
          actor={actor}
          providerId={providerId}
          connectionId={statusEditor.target}
          enabled={statusEditor.enabled}
          generation={generation}
          ready={ready}
          writable={writable}
          permissionsKey={permissionsKey}
          catalogueKey={catalogueKey}
          open={statusEditor.open}
          onClose={() => setStatusEditor((x) => (x ? { ...x, open: false } : x))}
          onSaved={() => {
            setStatusEditor(null)
            setStatusSavedFor(actor)
            void cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
          }}
        />
      )}
      {ready && recoveredStatus && statusEditor && !statusEditor.open && (
        <Button onClick={() => setStatusEditor((x) => (x ? { ...x, open: true } : x))}>
          {t('connectionStatus.recover')}
        </Button>
      )}
      {target && (
        <ConnectionMetadataEditor
          key={JSON.stringify([boundary.version, actor, providerId, target])}
          actor={actor}
          providerId={providerId}
          connectionId={target}
          generation={generation}
          ready={ready && !!row}
          writable={writable}
          permissionsKey={permissionsKey}
          catalogueKey={catalogueKey}
          open={editor?.open === true}
          onClose={close}
          onSaved={() => {
            setSavedFor(actor)
            close()
            void cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
          }}
        />
      )}
    </div>
  )
}
