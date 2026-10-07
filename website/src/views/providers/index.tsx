import { protocolLabels } from '@/lib/protocols'
import { useTranslation } from 'react-i18next'
import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { MoreHorizontal, Plus } from 'lucide-react'
import { listProviders, writeCatalog } from '@/api/catalog'
import type { Credential, Provider } from '@/types/catalog'
import { useSession } from '@/hooks/use-auth'
import { Page, QueryState, ErrorNotice, FormField, SaveButton } from '@/components/app/CatalogUI'
import { PermissionGate } from '@/components/app/PermissionGate'
import { usePermissions } from '@/hooks/use-permissions'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { Link, useParams, useSearchParams } from 'react-router'
import { Table } from '@/components/ui/table'
import { Menu, MenuItem } from '@/components/ui/menu'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { ProviderOverview, ProviderSettings } from './detail'
import CredentialMetadataDialog from './credential-metadata'
import ConnectionTable from './connections'
import ProviderModelTable from './provider-models'
import CredentialDeleteDialog from './credential-delete'
import CredentialReplacementDialog from './credential-replacements'
import CredentialReadinessDialog from './credential-readiness'
import CredentialCreateDialog from './credential-create'

type Action = { kind: 'provider' | 'connection' | 'credential' | 'model'; id?: string }
type ProviderTab = 'overview' | 'connections' | 'credentials' | 'models' | 'settings'
function providerTab(value: string | null): ProviderTab {
  return value === 'connections' ||
    value === 'credentials' ||
    value === 'models' ||
    value === 'settings'
    ? value
    : 'overview'
}

function CredentialTable({
  provider,
  canWrite,
  pending,
  onVerify,
  onToggle,
  onEdit,
  onDelete,
  onReplace,
  onReadiness,
}: {
  provider: Provider
  canWrite: boolean
  pending: boolean
  onVerify: (credential: Credential) => void
  onToggle: (credential: Credential) => void
  onEdit: (credential: Credential, connectionId: string, connectionName: string) => void
  onDelete: (credential: Credential, connectionId: string, connectionName: string) => void
  onReplace: (credential: Credential, connectionId: string, connectionName: string) => void
  onReadiness: (credential: Credential, connectionId: string, connectionName: string) => void
}) {
  const { t, i18n } = useTranslation('catalog')
  const [query, setQuery] = useState('')
  const [connection, setConnection] = useState('all')
  const [verification, setVerification] = useState('all')
  const [enabled, setEnabled] = useState('all')
  const credentials = provider.connections.flatMap((item) =>
    item.credentials.map((credential) => ({ connection: item, credential })),
  )
  const normalizedQuery = query.trim().toLowerCase()
  const rows = credentials.filter(
    (row) =>
      (!normalizedQuery || row.credential.name.toLowerCase().includes(normalizedQuery)) &&
      (connection === 'all' || row.connection.id === connection) &&
      (verification === 'all' || row.credential.verification_status === verification) &&
      (enabled === 'all' || row.credential.enabled === (enabled === 'enabled')),
  )
  const selectClass = 'h-10 rounded-md border bg-background px-3 text-sm'
  return (
    <div className="space-y-4">
      <div
        role="group"
        aria-label={t('providers.credentialFilters')}
        className="flex flex-wrap items-center gap-2"
      >
        <Input
          type="search"
          autoComplete="off"
          aria-label={t('providers.searchCredentials')}
          placeholder={t('providers.searchCredentials')}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          className="h-10 w-full sm:w-[220px]"
        />
        {provider.connections.length > 1 && (
          <select
            aria-label={t('providers.credentialConnectionFilter')}
            value={connection}
            onChange={(event) => setConnection(event.target.value)}
            className={selectClass}
          >
            <option value="all">{t('providers.allConnections')}</option>
            {provider.connections.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name}
              </option>
            ))}
          </select>
        )}
        <select
          aria-label={t('providers.credentialVerificationFilter')}
          value={verification}
          onChange={(event) => setVerification(event.target.value)}
          className={selectClass}
        >
          <option value="all">{t('providers.allVerificationStates')}</option>
          <option value="verified">{t('providers.verified')}</option>
          <option value="pending">{t('providers.pending')}</option>
          <option value="failed">{t('providers.failed')}</option>
        </select>
        <select
          aria-label={t('providers.credentialEnabledFilter')}
          value={enabled}
          onChange={(event) => setEnabled(event.target.value)}
          className={selectClass}
        >
          <option value="all">{t('providers.allEnabledStates')}</option>
          <option value="enabled">{t('providers.enabled')}</option>
          <option value="disabled">{t('common.disabled')}</option>
        </select>
      </div>
      <p role="status" className="text-sm text-muted-foreground">
        {t('providers.filteredCredentials', { count: rows.length, total: credentials.length })}
      </p>
      <Table aria-label={t('providers.credentialListLabel')}>
        <thead>
          <tr>
            <th>{t('common.credentialName')}</th>
            <th>{t('providers.connection')}</th>
            <th>{t('credentialStorage.recordedSource')}</th>
            <th>{t('providers.verification')}</th>
            <th>{t('providers.verifiedAt')}</th>
            <th>{t('providers.enabledStatus')}</th>
            <th>{t('common.priority')}</th>
            <th>{t('common.actions')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(({ connection: item, credential }) => (
            <tr key={credential.id}>
              <td>
                <span>{credential.name}</span>
                {credential.replaces_credential_id && (
                  <p className="mt-1 break-all text-xs text-muted-foreground">
                    {t('credentialReplacement.lineage', { id: credential.replaces_credential_id })}
                  </p>
                )}
              </td>
              <td>{item.name}</td>
              <td>{t(`credentialStorage.${credential.storage_source ?? 'unknown'}`)}</td>
              <td>
                <Badge variant="outline">{t(`providers.${credential.verification_status}`)}</Badge>
              </td>
              <td>
                {credential.verified_at ? (
                  <time dateTime={credential.verified_at}>
                    {new Date(credential.verified_at).toLocaleString(
                      i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
                    )}
                  </time>
                ) : (
                  t('providers.verificationNotRecorded')
                )}
              </td>
              <td>{credential.enabled ? t('providers.enabled') : t('common.disabled')}</td>
              <td>{credential.priority}</td>
              <td>
                <Menu
                  label={t('credentialMetadata.actions', { name: credential.name })}
                  trigger={<MoreHorizontal className="size-4" aria-hidden="true" />}
                  side="bottom"
                  align="end"
                  triggerClassName="h-8 w-8 justify-center px-0"
                >
                  <MenuItem disabled={pending || !canWrite} onClick={() => onVerify(credential)}>
                    {t('providers.verify')}
                  </MenuItem>
                  <MenuItem
                    disabled={
                      pending ||
                      !canWrite ||
                      (!credential.enabled && credential.verification_status !== 'verified')
                    }
                    onClick={() => onToggle(credential)}
                  >
                    {t(credential.enabled ? 'providers.disable' : 'providers.enable')}
                  </MenuItem>
                  <MenuItem
                    disabled={pending || !canWrite}
                    onClick={() => onEdit(credential, item.id, item.name)}
                  >
                    {t('credentialMetadata.edit')}
                  </MenuItem>
                  <MenuItem
                    disabled={pending || !canWrite}
                    onClick={() => onReplace(credential, item.id, item.name)}
                  >
                    {t('credentialReplacement.action')}
                  </MenuItem>
                  {credential.replaces_credential_id && (
                    <MenuItem
                      disabled={pending}
                      onClick={() => onReadiness(credential, item.id, item.name)}
                    >
                      {t('credentialReadiness.action')}
                    </MenuItem>
                  )}
                  <MenuItem
                    disabled={pending || !canWrite}
                    onClick={() => onDelete(credential, item.id, item.name)}
                  >
                    <span className="text-destructive">{t('credentialDelete.action')}</span>
                  </MenuItem>
                </Menu>
              </td>
            </tr>
          ))}
          {!rows.length && (
            <tr>
              <td colSpan={8} className="py-8 text-center text-muted-foreground">
                {t('providers.noMatchingCredentials')}
              </td>
            </tr>
          )}
        </tbody>
      </Table>
    </div>
  )
}
export default function ProvidersPage() {
  const { providerId } = useParams()
  return (
    <PermissionGate permission="providers.read">
      <Providers key={providerId ?? 'directory'} />
    </PermissionGate>
  )
}
function Providers() {
  const { t } = useTranslation('catalog')
  const sessionQuery = useSession()
  const { data: session } = sessionQuery
  const cache = useQueryClient()
  const access = usePermissions()
  const providers = useQuery({
    queryKey: ['admin', 'providers'],
    queryFn: ({ signal }) => listProviders(signal),
  })
  const [action, setAction] = useState<Action | null>(null)
  const [editingMetadata, setEditingMetadata] = useState<{
    providerId: string
    credentialId: string
    connectionId: string
    connectionName: string
  } | null>(null)
  const [deletingCredential, setDeletingCredential] = useState<{
    providerId: string
    credentialId: string
    connectionId: string
    connectionName: string
  } | null>(null)
  const [replacingCredential, setReplacingCredential] = useState<{
    providerId: string
    credentialId: string
    connectionId: string
    connectionName: string
  } | null>(null)
  const [reviewingCredential, setReviewingCredential] = useState<{
    providerId: string
    credentialId: string
    sourceCredentialId: string
    connectionId: string
    connectionName: string
  } | null>(null)
  const [notice, setNotice] = useState<{ key: string; count?: number } | null>(null)
  const { providerId } = useParams()
  const [params, setParams] = useSearchParams()
  const selected = providers.data?.find((provider) => provider.id === providerId)
  const tab = providerTab(params.get('tab'))
  const selectTab = (value: ProviderTab) =>
    setParams(value === 'overview' ? {} : { tab: value }, { replace: true })
  const mutation = useMutation({
    mutationFn: ({
      path,
      data,
      method = 'post',
    }: {
      path: string
      data: unknown
      method?: 'post' | 'patch'
    }) =>
      writeCatalog<{ verified?: boolean; discovered_models?: number; message?: string }>(
        method,
        path,
        data,
        session!.csrf_token,
      ),
    onSuccess: (result) => {
      if (result.verified !== undefined)
        setNotice(
          result.verified
            ? { key: 'providers.verifiedNotice', count: result.discovered_models ?? 0 }
            : { key: 'providers.verificationFailed' },
        )
      else {
        setNotice({ key: 'providers.saved' })
        setAction(null)
        mutation.reset()
      }
      void cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
      void cache.invalidateQueries({ queryKey: ['admin', 'models'] })
    },
    gcTime: 0,
  })
  function open(next: Action) {
    mutation.reset()
    setNotice(null)
    setAction(next)
  }
  function close() {
    mutation.reset()
    setAction(null)
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!action || mutation.isPending) return
    const form = new FormData(event.currentTarget)
    const value = (name: string) => String(form.get(name) ?? '').trim()
    if (action.kind === 'model')
      mutation.mutate({
        path: `/admin/connections/${action.id}/models`,
        data: { upstream_name: value('upstream_name') },
      })
  }
  const titles = {
    provider: t('providers.addProvider'),
    connection: t('providers.addConnection'),
    credential: t('providers.addCredential'),
    model: t('providers.addModel'),
  }
  return (
    <Page title={t('providers.title')} description={t('providers.description')}>
      {!providerId && (
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex gap-4 text-sm">
            <Badge variant="outline">
              {t('providers.providerCount', { count: providers.data?.length ?? 0 })}
            </Badge>
            <span>
              {t('providers.credentialCount', {
                count:
                  providers.data
                    ?.flatMap((p) => p.connections.flatMap((c) => c.credentials))
                    .filter((c) => c.enabled && c.verification_status === 'verified').length ?? 0,
              })}
            </span>
            <span>
              {t('providers.modelCount', {
                count:
                  providers.data?.flatMap((p) => p.connections.flatMap((c) => c.provider_models))
                    .length ?? 0,
              })}
            </span>
          </div>
          <Button
            disabled={!access.can('providers.write')}
            onClick={() => open({ kind: 'provider' })}
          >
            <Plus className="size-4" />
            {t('providers.addProvider')}
          </Button>
        </div>
      )}
      {notice && (
        <p role="status" className="rounded-md border bg-background p-3 text-sm">
          {t(notice.key, { count: notice.count })}
        </p>
      )}
      <ErrorNotice error={!action ? mutation.error : null} />
      <QueryState
        pending={providers.isPending}
        error={providers.error}
        retry={() => void providers.refetch()}
        empty={providers.data?.length === 0}
      />
      {!providerId && (
        <Table aria-label={t('providers.listLabel')}>
          <thead>
            <tr>
              <th>{t('common.provider')}</th>
              <th>{t('common.connectionSettings')}</th>
              <th>{t('common.protocolType')}</th>
              <th>{t('providers.validCredentials')}</th>
              <th>{t('common.models')}</th>
            </tr>
          </thead>
          <tbody>
            {providers.data?.map((provider) => (
              <tr key={provider.id}>
                <td>
                  <Link
                    to={`/admin/providers/${provider.id}`}
                    className="flex items-center gap-4 text-primary"
                  >
                    <span className="flex size-8 items-center justify-center rounded-md bg-muted text-foreground">
                      {provider.name.slice(0, 1)}
                    </span>
                    {provider.name}
                  </Link>
                </td>
                <td>{provider.connections.length}</td>
                <td>
                  {protocolLabels(provider.connections.map((connection) => connection.protocol))}
                </td>
                <td>
                  {
                    provider.connections
                      .flatMap((c) => c.credentials)
                      .filter((c) => c.enabled && c.verification_status === 'verified').length
                  }
                </td>
                <td>{provider.connections.flatMap((c) => c.provider_models).length}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {providerId && !providers.isPending && !selected && (
        <p role="alert">
          {t('providers.notFound')} <Link to="/admin/providers">{t('providers.backToList')}</Link>
        </p>
      )}
      {selected && (
        <>
          <div className="flex items-center justify-between rounded-lg border p-6">
            <div className="flex items-center gap-4">
              <span className="flex size-10 items-center justify-center rounded-md bg-muted">
                {selected.name.slice(0, 1)}
              </span>
              <h2 className="text-2xl font-semibold">{selected.name}</h2>
            </div>
            <Badge variant="outline">
              {protocolLabels(selected.connections.map((connection) => connection.protocol))}
            </Badge>
          </div>
          <Tabs value={tab} onValueChange={(value) => selectTab(providerTab(String(value)))}>
            <TabsList aria-label={t('providers.managementLabel', { name: selected.name })}>
              <TabsTrigger value="overview">{t('providers.overviewTab')}</TabsTrigger>
              <TabsTrigger value="connections">
                {t('providers.connectionsTab', { count: selected.connections.length })}
              </TabsTrigger>
              <TabsTrigger value="credentials">
                {t('providers.credentialsTab', {
                  count: selected.connections.flatMap((c) => c.credentials).length,
                })}
              </TabsTrigger>
              <TabsTrigger value="models">
                {t('providers.modelsTab', {
                  count: selected.connections.flatMap((c) => c.provider_models).length,
                })}
              </TabsTrigger>
              <TabsTrigger value="settings">{t('providers.settingsTab')}</TabsTrigger>
            </TabsList>
            <TabsContent value="overview">
              <ProviderOverview provider={selected} onSelectTab={selectTab} />
            </TabsContent>
            <TabsContent value="connections">
              <ConnectionTable
                key={selected.id}
                providerId={selected.id}
                session={sessionQuery}
                onAdd={() => open({ kind: 'connection', id: selected.id })}
              />
            </TabsContent>
            <TabsContent value="credentials">
              <div className="mb-4 flex flex-wrap justify-end gap-2">
                {selected.connections.map((c) => (
                  <Button
                    key={c.id}
                    disabled={!access.can('providers.write')}
                    onClick={() => open({ kind: 'credential', id: c.id })}
                  >
                    {t('providers.addCredentialFor', { name: c.name })}
                  </Button>
                ))}
              </div>
              <CredentialTable
                key={selected.id}
                provider={selected}
                canWrite={access.can('providers.write')}
                pending={mutation.isPending}
                onEdit={(credential, connectionId, connectionName) => {
                  setNotice(null)
                  setEditingMetadata({
                    providerId: selected.id,
                    credentialId: credential.id,
                    connectionId,
                    connectionName,
                  })
                }}
                onReplace={(credential, connectionId, connectionName) => {
                  setNotice(null)
                  setReplacingCredential({
                    providerId: selected.id,
                    credentialId: credential.id,
                    connectionId,
                    connectionName,
                  })
                }}
                onReadiness={(credential, connectionId, connectionName) => {
                  if (!credential.replaces_credential_id) return
                  setNotice(null)
                  setReviewingCredential({
                    providerId: selected.id,
                    credentialId: credential.id,
                    sourceCredentialId: credential.replaces_credential_id,
                    connectionId,
                    connectionName,
                  })
                }}
                onDelete={(credential, connectionId, connectionName) => {
                  setNotice(null)
                  setDeletingCredential({
                    providerId: selected.id,
                    credentialId: credential.id,
                    connectionId,
                    connectionName,
                  })
                }}
                onVerify={(credential) => {
                  setNotice(null)
                  mutation.mutate({
                    path: `/admin/credentials/${credential.id}/verify`,
                    data: {},
                  })
                }}
                onToggle={(credential) =>
                  mutation.mutate({
                    path: `/admin/credentials/${credential.id}`,
                    method: 'patch',
                    data: { enabled: !credential.enabled },
                  })
                }
              />
            </TabsContent>
            <TabsContent value="models">
              <ProviderModelTable
                providerId={selected.id}
                session={sessionQuery}
                onAdd={(connectionId) => open({ kind: 'model', id: connectionId })}
              />
            </TabsContent>
            <TabsContent value="settings">
              <ProviderSettings provider={selected} />
            </TabsContent>
          </Tabs>
        </>
      )}
      {reviewingCredential && selected?.id === reviewingCredential.providerId && (
        <CredentialReadinessDialog
          key={`${reviewingCredential.providerId}:${reviewingCredential.sourceCredentialId}:${reviewingCredential.credentialId}`}
          {...reviewingCredential}
          onClose={() => setReviewingCredential(null)}
        />
      )}
      {replacingCredential && selected?.id === replacingCredential.providerId && (
        <CredentialReplacementDialog
          {...replacingCredential}
          onClose={() => setReplacingCredential(null)}
          onCreated={() => {
            setReplacingCredential(null)
            setNotice({ key: 'credentialReplacement.created' })
            void cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
          }}
        />
      )}
      {deletingCredential && selected?.id === deletingCredential.providerId && (
        <CredentialDeleteDialog
          key={`${deletingCredential.providerId}:${deletingCredential.credentialId}`}
          {...deletingCredential}
          onClose={() => setDeletingCredential(null)}
          onDeleted={() => {
            setDeletingCredential(null)
            setNotice({ key: 'credentialDelete.deleted' })
            void cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
            void cache.invalidateQueries({
              queryKey: [
                'admin',
                'credential-metadata',
                selected.id,
                deletingCredential.credentialId,
              ],
            })
          }}
        />
      )}
      {editingMetadata && selected?.id === editingMetadata.providerId && (
        <CredentialMetadataDialog
          key={`${editingMetadata.providerId}:${editingMetadata.credentialId}`}
          {...editingMetadata}
          providerName={selected.name}
          onClose={() => setEditingMetadata(null)}
          onSaved={() => {
            setEditingMetadata(null)
            setNotice({ key: 'credentialMetadata.saved' })
            void cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
            void cache.invalidateQueries({
              queryKey: ['admin', 'credential-metadata', selected.id, editingMetadata.credentialId],
            })
          }}
        />
      )}
      {action && action.kind !== 'model' && (
        <CredentialCreateDialog
          kind={action.kind}
          id={action.id}
          onClose={close}
          targetCurrent={() => {
            const state = cache.getQueryState<Provider[]>(['admin', 'providers'])
            if (state?.status !== 'success' || state.fetchStatus !== 'idle' || state.isInvalidated)
              return false
            return (
              action.kind === 'provider' ||
              state.data?.some((item) =>
                action.kind === 'connection'
                  ? item.id === action.id
                  : item.connections.some((connection) => connection.id === action.id),
              ) === true
            )
          }}
          onSaved={() => {
            setNotice({ key: 'providers.saved' })
            close()
            void cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
            void cache.invalidateQueries({ queryKey: ['admin', 'models'] })
          }}
        />
      )}
      <Dialog
        width={640}
        open={action?.kind === 'model'}
        onOpenChange={(open) => {
          if (!open) close()
        }}
        busy={mutation.isPending}
        title={action ? titles[action.kind] : ''}
        description={t('providers.dialogDescription')}
      >
        <form className="space-y-5" onSubmit={submit}>
          <fieldset disabled={mutation.isPending || !access.can('providers.write')}>
            <FormField label={t('common.upstreamModelName')}>
              <Input name="upstream_name" required maxLength={200} />
            </FormField>
          </fieldset>
          <ErrorNotice error={mutation.error} />
          <SaveButton pending={mutation.isPending}>{t('common.save')}</SaveButton>
        </form>
      </Dialog>
    </Page>
  )
}
