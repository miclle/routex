import { builtinRoleNameKey } from '@/lib/role-assignment'
import { useTranslation } from 'react-i18next'
import { useLayoutEffect, useRef, useState, type FormEvent, type MouseEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, ShieldCheck } from 'lucide-react'
import { getRoles } from '@/api/governance'
import {
  validRoleDefinitionDescription,
  trimRoleDefinitionDescription,
} from '@/api/role-definition'
import { writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { RoleDefinitionEditor } from './role-definition'
import { approvalActorCurrent, useApprovalCacheRevision } from './approval-authority'
import { Page, QueryState, FormField, ErrorNotice, SaveButton } from '@/components/app/CatalogUI'
import { Table } from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Dialog } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import type { Session } from '@/types/auth'
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
  secrets: 'resources.secrets',
  limits: 'resources.limits',
  site: 'resources.site',
  announcements: 'resources.announcements',
  egress: 'resources.egress',
  smtp: 'resources.smtp',
  storage: 'resources.storage',
}
const actions: Record<string, string> = {
  read: 'actions.read',
  read_all: 'actions.readAll',
  write: 'actions.write',
  'models.write': 'actions.assignModels',
  'approvals.write': 'registrationApproval.permission',
  rotate: 'actions.rotate',
  test: 'actions.test',
  'keys.disable': 'actions.disableKeys',
  'tokens.write': 'actions.tokens',
  'money.write': 'actions.money',
  'rates.write': 'actions.rates',
  'quota_requests.read_all': 'actions.quotaRequests',
  'limits.write': 'actions.limits',
  'users.write': 'actions.userLimits',
  'settings.write': 'actions.limitSettings',
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
  const roleName = (role: PlatformRole) => {
    const key = builtinRoleNameKey(role)
    return key ? t(key) : role.name
  }
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
  const [actorContext, setActorContext] = useState({ actor, lifetime: 0 })
  if (actorContext.actor !== actor) setActorContext({ actor, lifetime: actorContext.lifetime + 1 })
  const lifetime = actorContext.lifetime
  const context = useRef({ actor, lifetime, opening: 0 })
  useLayoutEffect(() => {
    context.current = { actor, lifetime, opening: context.current.opening + 1 }
  }, [actor, lifetime])
  const mounted = useRef(true)
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const [creationName, setCreationName] = useState('')
  const [editor, setEditor] = useState<'new' | null>(null)
  const [creationPermissions, setCreationPermissions] = useState<string[]>([])
  const [creationDescription, setCreationDescription] = useState('')
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
  type Intent = {
    actor: string
    lifetime: number
    opening: number
    authority: string
    method: 'post' | 'delete'
    path: string
    data?: { name: string; description: string; permissions: string[] }
  }
  const [submitted, setSubmitted] = useState<Intent | null>(null)
  const [unconfirmed, setUnconfirmed] = useState(false)
  const dispatched = useRef(new WeakSet<Intent>())
  const sameOpening = (input: Intent) =>
    mounted.current &&
    cache.getQueryData<Session>(['auth', 'session'])?.user.id === input.actor &&
    input.actor === context.current.actor &&
    input.lifetime === context.current.lifetime &&
    input.opening === context.current.opening
  const mutation = useMutation({
    mutationFn: (input: Intent) => {
      if (!sameOpening(input) || !writer() || authority.snapshot() !== input.authority)
        return Promise.reject(new Error('Role write authority is unavailable'))
      // Resolve CSRF only after the exact captured actor/opening and current authority pass.
      const live = cache.getQueryData<Session>(['auth', 'session'])
      if (live?.user.id !== input.actor || !live.csrf_token)
        return Promise.reject(new Error('Role write authority is unavailable'))
      dispatched.current.add(input)
      return writeCatalog(input.method, input.path, input.data, live.csrf_token)
    },
    retry: false,
    onSuccess: (_result, input) => {
      if (!sameOpening(input)) return
      if (!writer() || authority.snapshot() !== input.authority) {
        setUnconfirmed(true)
        return
      }
      setSubmitted(null)
      setUnconfirmed(false)
      setEditor(null)
      setDeleting(null)
      void cache.invalidateQueries({ queryKey: ['admin', 'roles'] })
      void cache.invalidateQueries({ queryKey: ['permissions'] })
    },
    onError: (error, input) => {
      if (!sameOpening(input)) return
      const status = (error as { response?: { status?: number } }).response?.status
      const knownRejection = [400, 401, 403, 404, 409, 422].includes(status ?? 0)
      if (
        dispatched.current.has(input) &&
        (!knownRejection || authority.snapshot() !== input.authority)
      )
        setUnconfirmed(true)
      else setSubmitted(null)
    },
  })
  const [draftLifetime, setDraftLifetime] = useState(lifetime)
  if (draftLifetime !== lifetime) {
    setDraftLifetime(lifetime)
    setEditor(null)
    setDeleting(null)
    setCreationName('')
    setCreationDescription('')
    setCreationPermissions([])
    setSubmitted(null)
    setUnconfirmed(false)
  }
  const resetMutation = mutation.reset
  useLayoutEffect(() => {
    resetMutation()
  }, [lifetime, resetMutation])
  function dispatch(method: Intent['method'], path: string, data?: Intent['data']) {
    if (!writer() || mutation.isPending || submitted) return
    const input: Intent = {
      ...context.current,
      authority: authority.snapshot(),
      method,
      path,
      data,
    }
    setSubmitted(input)
    mutation.mutate(input)
  }
  function abandon() {
    if (!writer() || mutation.isPending || !unconfirmed) return
    // This discards local intent only; it neither cancels nor reverses the submitted operation.
    setSubmitted(null)
    setUnconfirmed(false)
    mutation.reset()
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (
      editor !== 'new' ||
      !writer() ||
      mutation.isPending ||
      !validRoleDefinitionDescription(trimRoleDefinitionDescription(creationDescription))
    )
      return
    dispatch('post', '/admin/roles', {
      name: creationName.trim(),
      description: trimRoleDefinitionDescription(creationDescription),
      permissions: [...creationPermissions].sort(),
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
              if (submitted) {
                setEditor(submitted.method === 'post' ? 'new' : null)
                return
              }
              context.current.opening += 1
              mutation.reset()
              setCreationName('')
              setCreationPermissions([])
              setCreationDescription('')
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
      <Table aria-label={t('roles.listLabel')} aria-describedby="role-member-count-help">
        <thead>
          <tr>
            <th>{t('common.role')}</th>
            <th>{t('common.type')}</th>
            <th>{t('roles.members')}</th>
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
                  <span className="mt-1 block max-w-md break-words whitespace-pre-wrap text-sm text-muted-foreground">
                    {role.description || t('roles.descriptionNotProvided')}
                  </span>
                </td>
                <td>
                  <Badge variant="outline">
                    {role.builtin ? t('common.builtin') : t('common.custom')}
                  </Badge>
                </td>
                <td>
                  {typeof role.member_count === 'number' &&
                  Number.isSafeInteger(role.member_count) &&
                  role.member_count >= 0
                    ? t('roles.memberCount', { count: role.member_count })
                    : t('roles.memberCountUnknown')}
                </td>
                <td>
                  {t(
                    new Set(role.permissions.map((p) => p.split('.')[0])).size === 1
                      ? 'roles.summarySingleResource'
                      : 'roles.summary',
                    {
                      count: role.permissions.length,
                      resources: new Set(role.permissions.map((p) => p.split('.')[0])).size,
                    },
                  )}
                </td>
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
                            if (submitted) return
                            context.current.opening += 1
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
      <p id="role-member-count-help" className="text-sm text-muted-foreground">
        {t('roles.memberCountHelp')}
      </p>
      <p className="text-sm text-muted-foreground">{t('roles.auditNotice')}</p>
      {unconfirmed &&
        writer() &&
        !editor &&
        (!deleting || !roles.data?.items.some((row) => row.id === deleting.id)) && (
          <div>
            <p role="status">{t('roles.unconfirmed')}</p>
            <Button onClick={abandon} disabled={mutation.isPending}>
              {t('roles.abandon')}
            </Button>
          </div>
        )}
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
          <fieldset disabled={mutation.isPending || !!submitted} className="space-y-6">
            <FormField label={t('roles.name')}>
              <Input
                name="name"
                value={creationName}
                onChange={(event) => {
                  if (writer() && !mutation.isPending && !submitted)
                    setCreationName(event.target.value)
                }}
                required
                maxLength={100}
              />
            </FormField>
            <FormField label={t('roles.descriptionLabel')}>
              <Textarea
                name="description"
                aria-label={t('roles.descriptionLabel')}
                rows={2}
                className="min-h-16 max-h-28 overflow-y-auto [field-sizing:content]"
                required
                value={creationDescription}
                placeholder={t('roles.descriptionPlaceholder')}
                aria-invalid={
                  creationDescription.length > 0 &&
                  !validRoleDefinitionDescription(
                    trimRoleDefinitionDescription(creationDescription),
                  )
                }
                onChange={(event) => {
                  if (writer() && !mutation.isPending && !submitted)
                    setCreationDescription(event.target.value)
                }}
              />
              {creationDescription.length > 0 &&
                !validRoleDefinitionDescription(
                  trimRoleDefinitionDescription(creationDescription),
                ) && <p role="alert">{t('roles.invalidDescription')}</p>}
            </FormField>
            <div className="divide-y">
              {[...new Set(roles.data?.available_permissions.map((p) => p.split('.')[0]))].map(
                (resource) => {
                  const group = roles.data!.available_permissions.filter((p) =>
                    p.startsWith(`${resource}.`),
                  )
                  const full = group.every((p) => creationPermissions.includes(p))
                  const partial = group.some((p) => creationPermissions.includes(p)) && !full
                  return (
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
                                checked={creationPermissions.includes(permission)}
                                onChange={(event) => {
                                  if (!writer() || mutation.isPending || submitted) return
                                  setCreationPermissions((previous) =>
                                    event.target.checked
                                      ? [...previous, permission].sort()
                                      : previous.filter((code) => code !== permission),
                                  )
                                }}
                              />
                              {actions[permission.slice(resource.length + 1)]
                                ? t(actions[permission.slice(resource.length + 1)])
                                : permission}
                            </label>
                          ))}
                        <label className="flex items-center gap-2 text-sm">
                          <Input
                            type="checkbox"
                            className="h-4 w-4 shrink-0 rounded border-input p-0 accent-primary"
                            aria-label={t('roles.selectAll', {
                              resource: resources[resource] ? t(resources[resource]) : resource,
                            })}
                            checked={full}
                            ref={(node) => {
                              if (node instanceof HTMLInputElement) {
                                node.indeterminate = partial
                              }
                            }}
                            onChange={() => {
                              if (!writer() || mutation.isPending || submitted) return
                              setCreationPermissions((previous) => {
                                const other = previous.filter((p) => !group.includes(p))
                                return group.every((p) => previous.includes(p))
                                  ? other
                                  : [...other, ...group].sort()
                              })
                            }}
                          />
                          {t('roles.selectAllLabel')}
                        </label>
                      </div>
                    </fieldset>
                  )
                },
              )}
            </div>
          </fieldset>
          <ErrorNotice error={mutation.error} />
          {unconfirmed && <p role="status">{t('roles.unconfirmed')}</p>}
          {unconfirmed && (
            <Button type="button" onClick={abandon} disabled={!writer() || mutation.isPending}>
              {t('roles.abandon')}
            </Button>
          )}
          <div className="flex justify-end">
            <SaveButton
              pending={mutation.isPending}
              disabled={
                !!submitted ||
                !validRoleDefinitionDescription(trimRoleDefinitionDescription(creationDescription))
              }
            >
              {t('roles.save')}
            </SaveButton>
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
        {unconfirmed && <p role="status">{t('roles.unconfirmed')}</p>}
        {unconfirmed && (
          <Button onClick={abandon} disabled={!writer() || mutation.isPending}>
            {t('roles.abandon')}
          </Button>
        )}
        <Button
          disabled={mutation.isPending || !!submitted}
          onClick={() =>
            deleting &&
            writer() &&
            roles.data?.items.some((row) => row.id === deleting.id) &&
            dispatch('delete', `/admin/roles/${deleting.id}`)
          }
        >
          {t('roles.confirmDelete')}
        </Button>
      </Dialog>
    </Page>
  )
}
