import { protocolLabel, protocolLabels } from '@/lib/protocols'
import { useTranslation } from 'react-i18next'
import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { listAdminModels, listGrantees, listProviders, writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { Page, QueryState, ErrorNotice, FormField, SaveButton } from '@/components/app/CatalogUI'
import { PermissionGate } from '@/components/app/PermissionGate'
import { usePermissions } from '@/hooks/use-permissions'
import { Input } from '@/components/ui/input'
import { Button, buttonVariants } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Link, useParams } from 'react-router'
import { Table } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import type { Model } from '@/types/catalog'

type Action =
  { kind: 'create' } | { kind: 'rename' | 'binding' | 'weights' | 'grants'; model: Model }
export default function AdminModelsPage() {
  return (
    <PermissionGate permission="models.read_all">
      <AdminModels />
    </PermissionGate>
  )
}
function AdminModels() {
  const { t, i18n } = useTranslation('catalog')
  const { data: session } = useSession()
  const cache = useQueryClient()
  const access = usePermissions()
  const models = useQuery({ queryKey: ['admin', 'models'], queryFn: listAdminModels })
  const providers = useQuery({
    queryKey: ['admin', 'providers'],
    queryFn: listProviders,
    enabled: access.can('providers.read'),
  })
  const grantees = useQuery({
    queryKey: ['admin', 'model-grantees'],
    queryFn: listGrantees,
    enabled: access.can('models.write'),
  })
  const [action, setAction] = useState<Action | null>(null)
  const [search, setSearch] = useState('')
  const { modelId } = useParams()
  const selected = models.data?.find((model) => model.id === modelId)
  const mutation = useMutation({
    mutationFn: ({
      path,
      data,
      method = 'post',
    }: {
      path: string
      data: unknown
      method?: 'post' | 'put'
    }) => writeCatalog(method, path, data, session!.csrf_token),
    onSuccess: () => {
      setAction(null)
      void cache.invalidateQueries({ queryKey: ['admin', 'models'] })
      void cache.invalidateQueries({ queryKey: ['models'] })
    },
  })
  function open(next: Action) {
    mutation.reset()
    setAction(next)
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!action || mutation.isPending) return
    const form = new FormData(event.currentTarget)
    const name = String(form.get('name') ?? '').trim()
    const provider_model_id = String(form.get('provider_model_id') ?? '')
    if (action.kind === 'create')
      mutation.mutate({ path: '/admin/models', data: { name, provider_model_id } })
    else {
      const path = `/admin/models/${action.model.id}`
      if (action.kind === 'binding')
        mutation.mutate({ path: `${path}/bindings`, data: { provider_model_id } })
      if (action.kind === 'rename') {
        const expiration = String(form.get('alias_expires_at') ?? '')
        mutation.mutate({
          path: `${path}/rename`,
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
    providers.data?.flatMap((provider) =>
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
        pending={models.isPending}
        error={models.error}
        retry={() => void models.refetch()}
        empty={models.data?.length === 0}
      />
      {!modelId && (
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
            <Table aria-label={t('adminModels.listLabel')}>
              <thead>
                <tr>
                  <th>{t('adminModels.publicName')}</th>
                  <th>{t('common.protocolType')}</th>
                  <th>{t('common.provider')}</th>
                  <th>{t('adminModels.status')}</th>
                  <th>{t('adminModels.members')}</th>
                  <th>{t('common.actions')}</th>
                </tr>
              </thead>
              <tbody>
                {models.data
                  ?.filter((model) =>
                    `${model.name} ${model.bindings.map((b) => providers.data?.find((p) => p.id === b.provider_id)?.name).join(' ')}`
                      .toLowerCase()
                      .includes(search.toLowerCase()),
                  )
                  .map((model) => (
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
                      <td>
                        {[
                          ...new Set(
                            model.bindings.map(
                              (b) =>
                                providers.data?.find((p) => p.id === b.provider_id)?.name ??
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
                        <Link to={`/admin/models/${model.id}`}>{t('adminModels.details')}</Link>
                      </td>
                    </tr>
                  ))}
              </tbody>
            </Table>
          </div>
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
              <Button
                disabled={!access.can('models.write')}
                variant="outline"
                onClick={() => open({ kind: 'rename', model: selected })}
              >
                {t('adminModels.rename')}
              </Button>
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
                  <dt className="text-muted-foreground">{t('adminModels.bindings')}</dt>
                  <dd>{t('adminModels.bindingCount', { count: selected.bindings.length })}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{t('adminModels.members')}</dt>
                  <dd>{selected.granted_user_ids.length}</dd>
                </div>
              </dl>
              {selected.names.some((name) => !name.is_current) && (
                <p className="text-xs leading-6 text-muted-foreground">
                  {t('adminModels.historicalNames', {
                    names: selected.names
                      .filter((name) => !name.is_current)
                      .map((name) =>
                        t('adminModels.historicalName', {
                          name: name.name,
                          expiration: name.expires_at
                            ? new Date(name.expires_at).toLocaleString(
                                i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
                              )
                            : t('adminModels.unavailable'),
                        }),
                      )
                      .join(t('common.listSeparator')),
                  })}
                </p>
              )}
            </div>
          </section>
          <form
            key={JSON.stringify(selected.bindings)}
            aria-label={t('adminModels.weightsLabel')}
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault()
              if (mutation.isPending) return
              const values = new FormData(event.currentTarget)
              mutation.mutate({
                path: `/admin/models/${selected.id}/weights`,
                method: 'put',
                data: {
                  weights: selected.bindings.map((binding) => ({
                    binding_id: binding.id,
                    weight: Number(values.get(binding.id)),
                  })),
                },
              })
            }}
          >
            <section className="rounded-lg border">
              <h3 className="border-b px-6 py-4 font-semibold">{t('adminModels.routing')}</h3>
              <Table>
                <thead>
                  <tr>
                    <th>{t('common.provider')}</th>
                    <th>{t('adminModels.providerModel')}</th>
                    <th>{t('common.protocol')}</th>
                    <th>{t('adminModels.status')}</th>
                    <th>{t('adminModels.weight')}</th>
                  </tr>
                </thead>
                <tbody>
                  {selected.bindings.map((binding) => (
                    <tr key={binding.id}>
                      <td>
                        {providers.data?.find((p) => p.id === binding.provider_id)?.name ??
                          binding.provider_id}
                      </td>
                      <td>{binding.upstream_name}</td>
                      <td>{protocolLabel(binding.protocol)}</td>
                      <td>
                        {binding.ready
                          ? t('adminModels.connectionReady')
                          : t('adminModels.connectionNotReady')}
                      </td>
                      <td>
                        <div className="flex items-center gap-2">
                          <Input
                            aria-label={t('adminModels.weightLabel', {
                              name: binding.upstream_name,
                            })}
                            name={binding.id}
                            type="number"
                            min={0}
                            max={100}
                            step={1}
                            required
                            defaultValue={binding.weight}
                            disabled={mutation.isPending || !access.can('models.write')}
                            className="w-24"
                          />
                          %
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
              <div className="flex items-center justify-between gap-4 p-4">
                <Button
                  disabled={!access.can('models.write')}
                  variant="outline"
                  onClick={() => open({ kind: 'binding', model: selected })}
                >
                  {t('adminModels.addBinding')}
                </Button>
                <p className="text-sm text-muted-foreground">{t('adminModels.weightsTotal')}</p>
              </div>
            </section>
            <ErrorNotice error={!action ? mutation.error : null} />
            <div className="flex justify-end gap-3">
              <Button
                disabled={!access.can('models.write')}
                variant="outline"
                onClick={() => open({ kind: 'grants', model: selected })}
              >
                {t('adminModels.grant')}
              </Button>
              <SaveButton pending={mutation.isPending} disabled={!access.can('models.write')}>
                {t('adminModels.saveWeights')}
              </SaveButton>
            </div>
          </form>
        </>
      )}
      <Dialog
        open={!!action}
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
            disabled={mutation.isPending || !access.can('models.write')}
            className="space-y-5"
          >
            {(action?.kind === 'create' || action?.kind === 'rename') && (
              <FormField label={t('common.modelName')}>
                <Input
                  name="name"
                  required
                  maxLength={200}
                  defaultValue={action.kind === 'rename' ? action.model.name : ''}
                />
              </FormField>
            )}
            {action?.kind === 'rename' && (
              <FormField label={t('adminModels.aliasDeadline')}>
                <Input name="alias_expires_at" type="datetime-local" />
              </FormField>
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
                {grantees.data?.map((user) => (
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
          <ErrorNotice error={mutation.error} />
          <SaveButton
            pending={mutation.isPending}
            disabled={action?.kind === 'grants' && !grantees.isSuccess}
          >
            {t('common.save')}
          </SaveButton>
        </form>
      </Dialog>
    </Page>
  )
}
