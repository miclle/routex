import { protocolLabel, protocolLabels } from '@/lib/protocols'
import { useTranslation } from 'react-i18next'
import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { listProviders, writeCatalog } from '@/api/catalog'
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
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'

type Action = { kind: 'provider' | 'connection' | 'credential' | 'model'; id?: string }
export default function ProvidersPage() {
  return (
    <PermissionGate permission="providers.read">
      <Providers />
    </PermissionGate>
  )
}
function Providers() {
  const { t } = useTranslation('catalog')
  const { data: session } = useSession()
  const cache = useQueryClient()
  const access = usePermissions()
  const providers = useQuery({ queryKey: ['admin', 'providers'], queryFn: listProviders })
  const [action, setAction] = useState<Action | null>(null)
  const [notice, setNotice] = useState<{ key: string; count?: number } | null>(null)
  const { providerId } = useParams()
  const [params, setParams] = useSearchParams()
  const selected = providers.data?.find((provider) => provider.id === providerId)
  const tab = params.get('tab') || 'connections'
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
    const secret = String(form.get('secret') ?? '')
    if (action.kind === 'provider')
      mutation.mutate({
        path: '/admin/providers',
        data: {
          name: value('name'),
          connection_name: value('connection_name'),
          base_url: value('base_url'),
          protocol: value('protocol'),
          credential_name: value('credential_name'),
          secret,
        },
      })
    if (action.kind === 'connection')
      mutation.mutate({
        path: `/admin/providers/${action.id}/connections`,
        data: {
          name: value('name'),
          base_url: value('base_url'),
          protocol: value('protocol'),
          credential_name: value('credential_name'),
          secret,
        },
      })
    if (action.kind === 'credential')
      mutation.mutate({
        path: `/admin/connections/${action.id}/credentials`,
        data: { name: value('name'), secret, priority: Number(value('priority')) },
      })
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
          <Tabs
            value={tab}
            onValueChange={(value) => setParams({ tab: String(value) }, { replace: true })}
          >
            <TabsList aria-label={t('providers.managementLabel', { name: selected.name })}>
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
            </TabsList>
            <TabsContent value="connections">
              <div className="mb-4 flex justify-end">
                <Button
                  disabled={!access.can('providers.write')}
                  onClick={() => open({ kind: 'connection', id: selected.id })}
                >
                  {t('providers.addConnection')}
                </Button>
              </div>
              <Table>
                <thead>
                  <tr>
                    <th>{t('common.connectionName')}</th>
                    <th>{t('common.protocolType')}</th>
                    <th>{t('common.baseURL')}</th>
                    <th>{t('common.credentials')}</th>
                    <th>{t('common.models')}</th>
                  </tr>
                </thead>
                <tbody>
                  {selected.connections.map((c) => (
                    <tr key={c.id}>
                      <td>{c.name}</td>
                      <td>{protocolLabel(c.protocol)}</td>
                      <td className="break-all">{c.base_url}</td>
                      <td>{c.credentials.length}</td>
                      <td>{c.provider_models.length}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
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
              <Table>
                <thead>
                  <tr>
                    <th>{t('common.credentialName')}</th>
                    <th>{t('providers.connection')}</th>
                    <th>{t('providers.verification')}</th>
                    <th>{t('providers.enabledStatus')}</th>
                    <th>{t('common.priority')}</th>
                    <th>{t('common.actions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {selected.connections.flatMap((c) =>
                    c.credentials.map((credential) => (
                      <tr key={credential.id}>
                        <td>{credential.name}</td>
                        <td>{c.name}</td>
                        <td>
                          <Badge variant="outline">
                            {credential.verification_status === 'verified'
                              ? t('providers.verified')
                              : credential.verification_status === 'failed'
                                ? t('providers.failed')
                                : t('providers.pending')}
                          </Badge>
                        </td>
                        <td>
                          {credential.enabled ? t('providers.enabled') : t('common.disabled')}
                        </td>
                        <td>{credential.priority}</td>
                        <td>
                          <div className="flex gap-2">
                            <Button
                              size="sm"
                              variant="ghost"
                              disabled={mutation.isPending || !access.can('providers.write')}
                              onClick={() => {
                                setNotice(null)
                                mutation.mutate({
                                  path: `/admin/credentials/${credential.id}/verify`,
                                  data: {},
                                })
                              }}
                            >
                              {t('providers.verify')}
                            </Button>
                            <Button
                              size="sm"
                              variant="ghost"
                              disabled={
                                mutation.isPending ||
                                !access.can('providers.write') ||
                                (!credential.enabled &&
                                  credential.verification_status !== 'verified')
                              }
                              onClick={() =>
                                mutation.mutate({
                                  path: `/admin/credentials/${credential.id}`,
                                  method: 'patch',
                                  data: { enabled: !credential.enabled },
                                })
                              }
                            >
                              {credential.enabled ? t('providers.disable') : t('providers.enable')}
                            </Button>
                          </div>
                        </td>
                      </tr>
                    )),
                  )}
                </tbody>
              </Table>
            </TabsContent>
            <TabsContent value="models">
              <div className="mb-4 flex flex-wrap justify-end gap-2">
                {selected.connections.map((c) => (
                  <Button
                    key={c.id}
                    disabled={!access.can('providers.write')}
                    onClick={() => open({ kind: 'model', id: c.id })}
                  >
                    {t('providers.addModelFor', { name: c.name })}
                  </Button>
                ))}
              </div>
              <Table>
                <thead>
                  <tr>
                    <th>{t('providers.modelIdentifier')}</th>
                    <th>{t('providers.connection')}</th>
                    <th>{t('common.protocolType')}</th>
                  </tr>
                </thead>
                <tbody>
                  {selected.connections.flatMap((c) =>
                    c.provider_models.map((m) => (
                      <tr key={m.id}>
                        <td>
                          <Link
                            className="text-primary"
                            to={`/admin/providers/${selected.id}/models/${m.id}`}
                          >
                            {m.upstream_name}
                          </Link>
                        </td>
                        <td>{c.name}</td>
                        <td>{protocolLabel(c.protocol)}</td>
                      </tr>
                    )),
                  )}
                </tbody>
              </Table>
            </TabsContent>
          </Tabs>
        </>
      )}
      <Dialog
        width={640}
        open={!!action}
        onOpenChange={(open) => {
          if (!open) close()
        }}
        busy={mutation.isPending}
        title={action ? titles[action.kind] : ''}
        description={t('providers.dialogDescription')}
      >
        <form className="space-y-5" onSubmit={submit}>
          <fieldset
            disabled={mutation.isPending || !access.can('providers.write')}
            className="space-y-5"
          >
            {action?.kind !== 'model' && (
              <FormField
                label={action?.kind === 'provider' ? t('common.providerName') : t('common.name')}
              >
                <Input name="name" required maxLength={100} />
              </FormField>
            )}
            {action?.kind === 'provider' && (
              <FormField label={t('common.connectionName')}>
                <Input name="connection_name" required maxLength={100} />
              </FormField>
            )}
            {(action?.kind === 'provider' || action?.kind === 'connection') && (
              <>
                <FormField label={t('common.protocolType')}>
                  <select
                    name="protocol"
                    defaultValue="openai_chat"
                    className="h-10 w-full rounded-md border bg-background px-3"
                  >
                    <option value="openai_chat">OpenAI Chat</option>
                    <option value="openai_responses">OpenAI Responses</option>
                  </select>
                </FormField>
                <FormField label={t('common.baseURL')}>
                  <Input
                    name="base_url"
                    type="url"
                    required
                    placeholder="https://api.example.com/v1"
                  />
                </FormField>
                <FormField label={t('common.credentialName')}>
                  <Input name="credential_name" required maxLength={100} />
                </FormField>
              </>
            )}
            {action?.kind !== 'model' && (
              <FormField label={t('common.upstreamAPIKey')}>
                <Input name="secret" type="password" autoComplete="off" required />
              </FormField>
            )}
            {action?.kind === 'credential' && (
              <FormField label={t('common.priority')}>
                <Input name="priority" type="number" min={0} step={1} defaultValue={0} required />
              </FormField>
            )}
            {action?.kind === 'model' && (
              <FormField label={t('common.upstreamModelName')}>
                <Input name="upstream_name" required maxLength={200} />
              </FormField>
            )}
          </fieldset>
          <ErrorNotice error={mutation.error} />
          <SaveButton pending={mutation.isPending}>{t('common.save')}</SaveButton>
        </form>
      </Dialog>
    </Page>
  )
}
