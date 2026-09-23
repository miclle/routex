import { useTranslation } from 'react-i18next'
import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, ShieldCheck } from 'lucide-react'
import { getRoles } from '@/api/governance'
import { writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { PermissionGate } from '@/components/app/PermissionGate'
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
  return (
    <PermissionGate permission="roles.read">
      <Roles />
    </PermissionGate>
  )
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
  const access = usePermissions()
  const cache = useQueryClient()
  const roles = useQuery({
    queryKey: ['admin', 'roles'],
    queryFn: ({ signal }) => getRoles(signal),
  })
  const [editor, setEditor] = useState<PlatformRole | 'new' | null>(null)
  const [viewing, setViewing] = useState<PlatformRole | null>(null)
  const [deleting, setDeleting] = useState<PlatformRole | null>(null)
  const mutation = useMutation({
    mutationFn: ({
      method,
      path,
      data,
    }: {
      method: 'post' | 'put' | 'delete'
      path: string
      data?: unknown
    }) => writeCatalog(method, path, data, session.data!.csrf_token),
    onSuccess: () => {
      setEditor(null)
      setDeleting(null)
      void cache.invalidateQueries({ queryKey: ['admin', 'roles'] })
      void cache.invalidateQueries({ queryKey: ['permissions'] })
    },
  })
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!editor || mutation.isPending) return
    const form = new FormData(event.currentTarget)
    mutation.mutate({
      method: editor === 'new' ? 'post' : 'put',
      path: `/admin/roles${editor === 'new' ? '' : `/${editor.id}`}`,
      data: {
        name: String(form.get('name')).trim(),
        permissions: form.getAll('permissions').map(String),
      },
    })
  }
  return (
    <Page title={t('roles.title')} description={t('roles.description')}>
      <div className="flex flex-wrap justify-between gap-4">
        <p className="max-w-3xl text-sm text-muted-foreground">{t('roles.explanation')}</p>
        {access.isAdmin && (
          <Button
            onClick={() => {
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
        retry={() => void roles.refetch()}
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
          {roles.data?.items.map((role) => (
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
                  <Button variant="ghost" size="sm" onClick={() => setViewing(role)}>
                    {t('roles.view')}
                  </Button>
                  {access.isAdmin && !role.builtin && (
                    <>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                          mutation.reset()
                          setEditor(role)
                        }}
                      >
                        {t('roles.edit')}
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => {
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
        open={editor !== null}
        onOpenChange={(open) => {
          if (!open) setEditor(null)
        }}
        busy={mutation.isPending}
        width={720}
        title={editor === 'new' ? t('roles.create') : t('roles.editTitle')}
        description={t('roles.editorDescription')}
      >
        <form onSubmit={submit} className="space-y-6">
          <fieldset disabled={mutation.isPending} className="space-y-6">
            <FormField label={t('roles.name')}>
              <Input
                name="name"
                defaultValue={editor && editor !== 'new' ? editor.name : ''}
                required
                maxLength={100}
              />
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
                            <input
                              type="checkbox"
                              name="permissions"
                              value={permission}
                              defaultChecked={
                                !!editor &&
                                editor !== 'new' &&
                                editor.permissions.includes(permission)
                              }
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
      <Dialog
        open={!!viewing}
        onOpenChange={(open) => {
          if (!open) setViewing(null)
        }}
        width={640}
        title={t('roles.viewTitle', { name: viewing ? roleName(viewing) : '' })}
        description={viewing?.builtin ? t('roles.builtinDescription') : t('roles.viewDescription')}
      >
        <PermissionRows permissions={viewing?.permissions ?? []} />
      </Dialog>
      <Dialog
        open={!!deleting}
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
            deleting && mutation.mutate({ method: 'delete', path: `/admin/roles/${deleting.id}` })
          }
        >
          {t('roles.confirmDelete')}
        </Button>
      </Dialog>
    </Page>
  )
}
