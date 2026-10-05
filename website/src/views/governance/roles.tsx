import { useTranslation } from 'react-i18next'
import { useState, type FormEvent, type MouseEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, ShieldCheck } from 'lucide-react'
import { getRoles } from '@/api/governance'
import { writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { RoleDefinitionEditor } from './role-definition'
import { approvalActorCurrent, useApprovalCacheRevision } from './approval-authority'
import { Page, QueryState, FormField, ErrorNotice, SaveButton } from '@/components/app/CatalogUI'
import { Table } from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import type { PlatformRole } from '@/types/governance'

const resources: Record<string, string> = {
  members: 'resources.members',
  roles: 'resources.roles',
  registration: 'resources.registration',
  providers: 'resources.providers',
  models: 'resources.models',
  calls: 'resources.calls',
  audit: 'resources.audit',
  system: 'resources.system',
  teams: 'resources.teams',
  projects: 'resources.projects',
  prices: 'resources.prices',
}
const actions: Record<string, string> = {
  read: 'actions.read',
  read_all: 'actions.readAll',
  write: 'actions.write',
  'models.write': 'actions.assignModels',
  'approvals.write': 'registrationApproval.permission',
}

export function PermissionRows({ permissions }: { permissions: string[] }) {
  const { t } = useTranslation('governance')
  return (
    <Table>
      <thead>
        <tr>
          <th>{t('roles.resource')}</th>
          <th>{t('roles.allowedActions')}</th>
        </tr>
      </thead>
      <tbody>
        {[...new Set(permissions.map((p) => p.split('.')[0]))].map((resource) => (
          <tr key={resource}>
            <td>{resources[resource] ? t(resources[resource]) : resource}</td>
            <td className="space-x-2">
              {permissions
                .filter((p) => p.startsWith(`${resource}.`))
                .map((permission) => (
                  <Badge key={permission} variant="outline">
                    {actions[permission.slice(resource.length + 1)]
                      ? t(actions[permission.slice(resource.length + 1)])
                      : permission.slice(resource.length + 1)}
                  </Badge>
                ))}
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  )
}
export default function RolesPage() {
  return <Roles />
}
function Roles() {
  const { t } = useTranslation('governance')
  const roleName = (role: PlatformRole) =>
    role.builtin && role.id === 'rol_admin'
      ? t('common.admin')
      : role.builtin && role.id === 'rol_member'
        ? t('common.member')
        : role.name
  const session = useSession()
  usePermissions()
  const cache = useQueryClient()
  const actor = session.data?.user.id ?? ''
  const authority = useApprovalCacheRevision([
    ['auth', 'session'],
    ['permissions', actor],
  ])
  const generation = authority.revision
  const queryKey = ['admin', 'roles', actor, generation]
  const canRead = () =>
    approvalActorCurrent(cache, actor, 'roles.read') && authority.snapshot() === authority.revision
  const roles = useQuery({
    queryKey,
    queryFn: ({ signal }) => getRoles(signal),
    enabled: canRead(),
    retry: false,
  })
  const listRevision = useApprovalCacheRevision([queryKey])
  const current = () => {
    const state = cache.getQueryState(queryKey)
    return (
      canRead() &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      state.data === roles.data &&
      listRevision.snapshot() === listRevision.revision
    )
  }
  const writer = () => current() && approvalActorCurrent(cache, actor, 'roles.read', true)
  const [editor, setEditor] = useState<'new' | null>(null)
  const [definition, setDefinition] = useState<{
    actor: string
    id: string
    mode: 'edit' | 'view'
    open: boolean
    epoch: number
    trigger: HTMLButtonElement
  } | null>(null)
  function openDefinition(
    role: PlatformRole,
    mode: 'edit' | 'view',
    event: MouseEvent<HTMLButtonElement>,
  ) {
    if (
      !current() ||
      !roles.data?.items.some((row) => row.id === role.id) ||
      (mode === 'edit' && (!writer() || role.builtin))
    )
      return
    const trigger = event.currentTarget
    setDefinition((previous) => ({
      actor,
      id: role.id,
      mode,
      open: true,
      epoch: previous?.id === role.id && previous.actor === actor ? previous.epoch + 1 : 1,
      trigger,
    }))
  }
  const [deleting, setDeleting] = useState<PlatformRole | null>(null)
  const mutation = useMutation({
    mutationFn: ({
      method,
      path,
      data,
    }: {
      method: 'post' | 'delete'
      path: string
      data?: unknown
    }) => {
      if (!writer()) return Promise.reject(new Error('Role write authority is unavailable'))
      return writeCatalog(method, path, data, session.data!.csrf_token)
    },
    onSuccess: () => {
      setEditor(null)
      setDeleting(null)
      void cache.invalidateQueries({ queryKey: ['admin', 'roles'] })
      void cache.invalidateQueries({ queryKey: ['permissions'] })
    },
  })
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (editor !== 'new' || !writer() || mutation.isPending) return
    const form = new FormData(event.currentTarget)
    mutation.mutate({
      method: 'post',
      path: '/admin/roles',
      data: {
        name: String(form.get('name')).trim(),
        permissions: form.getAll('permissions').map(String),
      },
    })
  }
  return (
    <Page title={t('roles.title')} description={t('roles.description')}>
      {!canRead() && <p role="status">{t('roleDefinition.readDenied')}</p>}
      <div className="flex flex-wrap justify-between gap-4">
        <p className="max-w-3xl text-sm text-muted-foreground">{t('roles.explanation')}</p>
        {writer() && (
          <Button
            onClick={() => {
              if (!writer()) return
              mutation.reset()
              setEditor('new')
            }}
          >
            <Plus className="size-4" />
            {t('roles.create')}
          </Button>
        )}
      </div>
      <QueryState
        pending={roles.isPending}
        error={roles.error}
        retry={() => {
          if (canRead()) void roles.refetch()
        }}
      />
      <Table aria-label={t('roles.listLabel')}>
        <thead>
          <tr>
            <th>{t('common.role')}</th>
            <th>{t('common.type')}</th>
            <th>{t('roles.permissions')}</th>
            <th>{t('common.actions')}</th>
          </tr>
        </thead>
        <tbody>
          {current() &&
            roles.data?.items.map((role) => (
              <tr key={role.id}>
                <td>
                  <span className="flex items-center gap-2">
                    <ShieldCheck className="size-4" />
                    {roleName(role)}
                  </span>
                </td>
                <td>
                  <Badge variant="outline">
                    {role.builtin ? t('common.builtin') : t('common.custom')}
                  </Badge>
                </td>
                <td>{t('roles.actionCount', { count: role.permissions.length })}</td>
                <td>
                  <div className="flex gap-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={(event) => openDefinition(role, 'view', event)}
                    >
                      {t('roles.view')}
                    </Button>
                    {writer() && !role.builtin && (
                      <>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={(event) => openDefinition(role, 'edit', event)}
                        >
                          {t('roles.edit')}
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            if (!writer()) return
                            mutation.reset()
                            setDeleting(role)
                          }}
                        >
                          {t('roles.delete')}
                        </Button>
                      </>
                    )}
                  </div>
                </td>
              </tr>
            ))}
        </tbody>
      </Table>
      <p className="text-sm text-muted-foreground">{t('roles.auditNotice')}</p>
      <Dialog
        open={editor !== null && writer()}
        onOpenChange={(open) => {
          if (!open) setEditor(null)
        }}
        busy={mutation.isPending}
        width={720}
        title={t('roles.create')}
        description={t('roles.editorDescription')}
      >
        <form onSubmit={submit} className="space-y-6">
          <fieldset disabled={mutation.isPending} className="space-y-6">
            <FormField label={t('roles.name')}>
              <Input name="name" defaultValue="" required maxLength={100} />
            </FormField>
            <div className="divide-y">
              {[...new Set(roles.data?.available_permissions.map((p) => p.split('.')[0]))].map(
                (resource) => (
                  <fieldset key={resource} className="space-y-3 py-4">
                    <legend className="text-sm font-medium">
                      {resources[resource] ? t(resources[resource]) : resource}
                    </legend>
                    <div className="flex flex-wrap gap-4">
                      {roles.data?.available_permissions
                        .filter((p) => p.startsWith(`${resource}.`))
                        .map((permission) => (
                          <label key={permission} className="flex items-center gap-2 text-sm">
                            <Input
                              type="checkbox"
                              className="h-4 w-4 shrink-0 rounded border-input p-0 accent-primary"
                              name="permissions"
                              value={permission}
                            />
                            {actions[permission.slice(resource.length + 1)]
                              ? t(actions[permission.slice(resource.length + 1)])
                              : permission}
                          </label>
                        ))}
                    </div>
                  </fieldset>
                ),
              )}
            </div>
          </fieldset>
          <ErrorNotice error={mutation.error} />
          <div className="flex justify-end">
            <SaveButton pending={mutation.isPending}>{t('roles.save')}</SaveButton>
          </div>
        </form>
      </Dialog>
      {definition?.actor === actor && (
        <RoleDefinitionEditor
          key={`${definition.actor}:${definition.id}`}
          actor={actor}
          target={definition.id}
          generation={generation}
          ready={current()}
          open={definition.open}
          mode={definition.mode}
          openEpoch={definition.epoch}
          contextQueryKey={queryKey}
          onClose={() => {
            if (current())
              setDefinition((value) =>
                value?.actor === actor && value.id === definition.id
                  ? { ...value, open: false }
                  : value,
              )
          }}
          returnFocus={() =>
            current() &&
            definition.trigger.isConnected &&
            roles.data?.items.some((row) => row.id === definition.id)
              ? definition.trigger
              : false
          }
          renderPermissions={(permissions) => <PermissionRows permissions={permissions} />}
          resourceLabel={(resource) => (resources[resource] ? t(resources[resource]) : resource)}
          permissionLabel={(permission) => {
            const action = permission.slice(permission.indexOf('.') + 1)
            return actions[action] ? t(actions[action]) : permission
          }}
        />
      )}
      <Dialog
        open={
          !!deleting && writer() && roles.data?.items.some((row) => row.id === deleting.id) === true
        }
        onOpenChange={(open) => {
          if (!open) setDeleting(null)
        }}
        busy={mutation.isPending}
        title={t('roles.delete')}
        description={t('roles.deleteDescription', { name: deleting ? roleName(deleting) : '' })}
      >
        <ErrorNotice error={mutation.error} />
        <Button
          disabled={mutation.isPending}
          onClick={() =>
            deleting &&
            writer() &&
            roles.data?.items.some((row) => row.id === deleting.id) &&
            mutation.mutate({ method: 'delete', path: `/admin/roles/${deleting.id}` })
          }
        >
          {t('roles.confirmDelete')}
        </Button>
      </Dialog>
    </Page>
  )
}
