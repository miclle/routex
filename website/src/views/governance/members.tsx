import MemberMetadata from './member-metadata'
import MemberList from './member-list'
import { getMemberList, validateMemberListChain } from '@/api/member-list'
import type { MemberListItem } from '@/types/member-list'
import type { InfiniteData } from '@tanstack/react-query'
import type { MemberListPage } from '@/types/member-list'
import MemberTeams from './member-teams'
import MemberKeys from './member-keys'
import MemberOverview from './member-overview'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import MemberModels from './member-models'
import MemberLimits from './member-limits'
import { useTranslation } from 'react-i18next'
import { useLayoutEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams, useSearchParams } from 'react-router'
import { Plus } from 'lucide-react'
import { getMember, getRoles } from '@/api/governance'
import { writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { Page, QueryState, FormField, ErrorNotice, SaveButton } from '@/components/app/CatalogUI'
import { Table } from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { PermissionRows } from './roles'
import type { Session } from '@/types/auth'
import type { Member, MemberFilters } from '@/types/governance'

export default function MembersPage() {
  return <Members />
}
function Members() {
  const { t, i18n } = useTranslation('governance')
  const { memberId } = useParams()
  const [params, setParams] = useSearchParams()
  const access = usePermissions()
  const session = useSession()
  const generation = useSessionGeneration()
  const cache = useQueryClient()
  const navigate = useNavigate()
  const actor = session.data?.user.id ?? ''
  const authorized =
    !!actor &&
    !session.isError &&
    !session.isFetching &&
    !access.isError &&
    !access.isFetching &&
    access.can('members.read')
  const owner = `${actor}:${memberId ?? ''}`
  const [draft, setDraft] = useState<{ owner: string; role: string; roleIds: string[] } | null>(
    null,
  )
  const latest = useRef({
    actor,
    memberId,
    generation,
    authorized,
    csrf: session.data?.csrf_token,
    admin: access.isAdmin,
    write: access.can('members.write'),
  })
  useLayoutEffect(() => {
    latest.current = {
      actor,
      memberId,
      generation,
      authorized,
      csrf: session.data?.csrf_token,
      admin: access.isAdmin,
      write: access.can('members.write'),
    }
  }, [actor, memberId, generation, authorized, session.data?.csrf_token, access.isAdmin, access])
  const dispatching = useRef(false)
  const [filters, setFilters] = useState<MemberFilters>({})
  const [creating, setCreating] = useState(false)
  const [statusTarget, setStatusTarget] = useState<Member | null>(null)
  const [statusListOwner, setStatusListOwner] = useState<string | null>(null)
  const teamRead = access.can('teams.read_all')
  const listOwner = JSON.stringify([actor, generation, filters, teamRead])
  const listQueryKey = useMemo(
    () => ['admin', 'members', actor, generation, filters, teamRead] as const,
    [actor, generation, filters, teamRead],
  )
  const [validation, setValidation] = useState<'members.passwordValidation' | null>(null)
  const members = useInfiniteQuery({
    queryKey: listQueryKey,
    queryFn: ({ pageParam, signal }) => getMemberList(filters, actor, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: !memberId && authorized,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  let listError: Error | null = null
  let listRows: MemberListItem[] = []
  if (!memberId && authorized && members.isSuccess && !members.isFetching) {
    try {
      listRows = validateMemberListChain(members.data.pages, actor).flatMap((page) => page.items)
    } catch (error) {
      listError = error instanceof Error ? error : new Error('Invalid member list')
    }
  }
  const listReady =
    !memberId && authorized && members.isSuccess && !members.isFetching && !listError
  const listLatest = useRef({ owner: listOwner, key: listQueryKey, ready: listReady })
  useLayoutEffect(() => {
    listLatest.current = { owner: listOwner, key: listQueryKey, ready: listReady }
  }, [listOwner, listQueryKey, listReady])
  function canListAct(id: string, permission = 'members.read') {
    const now = latest.current,
      list = listLatest.current
    const auth = cache.getQueryState<Session>(['auth', 'session'])
    const grants = cache.getQueryState<string[]>(['permissions', now.actor])
    const state = cache.getQueryState<InfiniteData<MemberListPage>>(list.key)
    return (
      !now.memberId &&
      now.authorized &&
      list.ready &&
      auth?.data?.user.id === now.actor &&
      !auth.error &&
      auth.fetchStatus === 'idle' &&
      grants?.fetchStatus === 'idle' &&
      !grants.error &&
      grants.data?.includes('members.read') === true &&
      grants.data.includes(permission) &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.error &&
      state.data?.pages.every((page) => page.actor_user_id === now.actor) === true &&
      state.data.pages.some((page) => page.items.some((row) => row.id === id))
    )
  }
  function validListSelection(selection: { owner: string; row: MemberListItem }) {
    const list = listLatest.current
    const state = cache.getQueryState<InfiniteData<MemberListPage>>(list.key)
    return (
      selection.owner === list.owner &&
      canListAct(selection.row.id, 'members.write') &&
      state?.data?.pages.some((page) => page.items.some((row) => row === selection.row)) === true
    )
  }
  if (
    statusListOwner &&
    (statusListOwner !== listOwner ||
      !listReady ||
      !listRows.includes(statusTarget as MemberListItem))
  ) {
    setStatusTarget(null)
    setStatusListOwner(null)
  }
  const member = useQuery({
    queryKey: ['admin', 'member', actor, memberId, generation],
    queryFn: ({ signal }) => getMember(memberId!, signal),
    enabled: !!memberId && authorized,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const roles = useQuery({
    queryKey: ['admin', 'roles', actor, generation],
    queryFn: ({ signal }) => getRoles(signal),
    enabled: !!memberId && authorized && access.can('roles.read'),
  })
  const mutation = useMutation({
    mutationFn: ({
      method,
      path,
      data,
      actor: capturedActor,
      target: capturedTarget,
      generation: capturedGeneration,
      listSelection,
    }: {
      method: 'post' | 'put' | 'patch'
      path: string
      data: unknown
      actor: string
      target?: string
      generation: number
      listSelection?: { owner: string; row: MemberListItem }
    }) => {
      const now = latest.current
      const auth = cache.getQueryState<Session>(['auth', 'session'])
      const permission = cache.getQueryState<string[]>(['permissions', now.actor])
      if (
        (listSelection && !validListSelection(listSelection)) ||
        !now.authorized ||
        !auth?.data?.csrf_token ||
        auth.data.user.id !== now.actor ||
        auth.error ||
        auth.fetchStatus !== 'idle' ||
        permission?.fetchStatus !== 'idle' ||
        permission.error ||
        !permission.data?.includes('members.read') ||
        (path.endsWith('/roles')
          ? auth.data.user.role !== 'admin'
          : !permission.data.includes('members.write')) ||
        now.actor !== capturedActor ||
        now.memberId !== capturedTarget ||
        now.generation !== capturedGeneration ||
        (path.endsWith('/roles') ? !now.admin : !now.write)
      )
        throw new Error('Member write authority is unavailable')
      return writeCatalog<Member>(method, path, data, auth.data.csrf_token)
    },
    gcTime: 0,
    onSuccess: (result, input) => {
      const now = latest.current
      const auth = cache.getQueryState<Session>(['auth', 'session'])
      const permission = cache.getQueryState<string[]>(['permissions', now.actor])
      if (
        (input.listSelection && !validListSelection(input.listSelection)) ||
        !now.authorized ||
        auth?.data?.user.id !== input.actor ||
        auth.error ||
        auth.fetchStatus !== 'idle' ||
        permission?.fetchStatus !== 'idle' ||
        permission.error ||
        !permission.data?.includes('members.read') ||
        now.actor !== input.actor ||
        now.memberId !== input.target ||
        now.generation !== input.generation ||
        (input.method !== 'post' && result.id !== input.path.split('/')[3])
      ) {
        mutation.reset()
        return
      }
      setDraft(null)
      setCreating(false)
      setStatusTarget(null)
      cache.setQueryData(['admin', 'member', actor, result.id, generation], result)
      void cache.invalidateQueries({ queryKey: ['admin', 'members'] })
      void cache.invalidateQueries({ queryKey: ['permissions'] })
      void cache.invalidateQueries({ queryKey: ['auth', 'session'] })
      mutation.reset()
      if (input.method === 'post') navigate(`/admin/members/${result.id}`)
    },
    onSettled: () => {
      dispatching.current = false
    },
  })
  const [previousOwner, setPreviousOwner] = useState(owner)
  if (previousOwner !== owner) {
    setPreviousOwner(owner)
    setDraft(null)
    setCreating(false)
    setStatusTarget(null)
    setValidation(null)
  }
  const resetMutation = mutation.reset
  useLayoutEffect(() => {
    resetMutation()
  }, [owner, resetMutation])
  function dispatch(
    input: { method: 'post' | 'put' | 'patch'; path: string; data: unknown },
    listSelection?: { owner: string; row: MemberListItem },
  ) {
    const now = latest.current
    const auth = cache.getQueryState<Session>(['auth', 'session'])
    const permission = cache.getQueryState<string[]>(['permissions', now.actor])
    if (
      dispatching.current ||
      (listSelection && !validListSelection(listSelection)) ||
      !now.authorized ||
      !auth?.data?.csrf_token ||
      auth.data.user.id !== now.actor ||
      auth.error ||
      auth.fetchStatus !== 'idle' ||
      permission?.fetchStatus !== 'idle' ||
      permission.error ||
      !permission.data?.includes('members.read') ||
      (input.path.endsWith('/roles')
        ? auth.data.user.role !== 'admin'
        : !permission.data.includes('members.write')) ||
      (input.path.endsWith('/roles') ? !now.admin : !now.write)
    )
      return
    dispatching.current = true
    mutation.mutate({
      ...input,
      actor: now.actor,
      target: now.memberId,
      generation: now.generation,
      listSelection,
    })
  }
  const canChange = (target: Member) =>
    access.can('members.write') &&
    (access.isAdmin || (target.role !== 'admin' && target.id !== session.data?.user.id))
  function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (mutation.isPending) return
    const values = new FormData(event.currentTarget)
    const password = String(values.get('password'))
    const bytes = new TextEncoder().encode(password).length
    if (bytes < 12 || bytes > 72) {
      setValidation('members.passwordValidation')
      return
    }
    setValidation(null)
    dispatch({
      method: 'post',
      path: '/admin/members',
      data: {
        name: String(values.get('name')).trim(),
        email: String(values.get('email')).trim(),
        password,
        role: access.isAdmin ? String(values.get('role')) : 'member',
      },
    })
  }
  const roleName = (role: { id: string; name: string; builtin: boolean }) =>
    role.builtin && role.id === 'rol_admin'
      ? t('common.admin')
      : role.builtin && role.id === 'rol_member'
        ? t('common.member')
        : role.name
  const current =
    authorized && member.isSuccess && !member.isFetching && member.data.id === memberId
      ? member.data
      : undefined
  const currentRoles =
    roles.data?.items.filter(
      (role) =>
        current && (role.id === `rol_${current.role}` || current.role_ids.includes(role.id)),
    ) ?? []
  const unavailable = (
    <Page
      title={t('common:access_denied_cb8d4')}
      description={t('common:your_account_does_not_have_permission_to_access_ca6a8')}
    >
      <QueryState
        pending={session.isFetching || access.isPending || access.isFetching}
        error={session.error || access.error}
        retry={() => {
          void session.refetch()
          void access.refetch()
        }}
      />
      {!session.isFetching &&
        !access.isPending &&
        !access.isFetching &&
        !session.isError &&
        !access.isError && (
          <p role="alert" className="text-sm text-muted-foreground">
            {t('common:your_account_does_not_have_permission_to_access_ca6a8')}
          </p>
        )}
    </Page>
  )
  const page = (
    <Page
      title={memberId ? t('members.detailTitle') : t('members.title')}
      description={t('members.description')}
    >
      {!memberId && (
        <>
          <div className="flex flex-wrap items-center justify-between gap-4">
            <form
              aria-label={t('members.filterLabel')}
              className="flex flex-wrap gap-3"
              onSubmit={(event) => {
                event.preventDefault()
                const form = new FormData(event.currentTarget)
                setFilters({
                  q: String(form.get('q') || '').trim(),
                  status: String(form.get('status') || ''),
                  role: String(form.get('role') || ''),
                })
              }}
            >
              <Input
                name="q"
                aria-label={t('members.searchLabel')}
                placeholder={t('members.searchPlaceholder')}
                className="w-80"
              />
              <select
                name="status"
                aria-label={t('members.statusLabel')}
                className="rounded-md border bg-background px-3"
              >
                <option value="">{t('members.allMembers')}</option>
                <option value="active">{t('common.active')}</option>
                <option value="disabled">{t('common.disabled')}</option>
              </select>
              <select
                name="role"
                aria-label={t('members.roleLabel')}
                className="rounded-md border bg-background px-3"
              >
                <option value="">{t('members.allRoles')}</option>
                <option value="member">{t('common.member')}</option>
                <option value="admin">{t('common.admin')}</option>
              </select>
              <Button type="submit" variant="outline">
                {t('members.filter')}
              </Button>
            </form>
            {access.can('members.write') && (
              <Button
                onClick={() => {
                  mutation.reset()
                  setValidation(null)
                  setCreating(true)
                }}
              >
                <Plus className="size-4" />
                {t('members.create')}
              </Button>
            )}
          </div>
          <QueryState
            pending={members.isPending || members.isFetching}
            error={members.error || listError}
            retry={() => {
              if (authorized) void members.refetch()
            }}
            empty={listReady && listRows.length === 0}
          />
          {listReady && (
            <MemberList
              rows={listRows}
              permissions={{
                teams: access.can('teams.read_all'),
                calls: access.can('calls.read_all'),
                limitsWrite: access.can('limits.users.write'),
              }}
              canAct={canListAct}
              canChange={canChange}
              onStatus={(item) => {
                if (canListAct(item.id, 'members.write') && canChange(item)) {
                  mutation.reset()
                  setStatusListOwner(listOwner)
                  setStatusTarget(item)
                }
              }}
            />
          )}
          {listReady && members.hasNextPage && (
            <div className="text-center">
              <Button
                variant="outline"
                disabled={members.isFetching}
                onClick={() => {
                  if (
                    listLatest.current.ready &&
                    listLatest.current.owner === listOwner &&
                    listRows.length &&
                    canListAct(listRows[0].id)
                  )
                    void members.fetchNextPage({ cancelRefetch: false })
                }}
              >
                {t('members.loadMore')}
              </Button>
            </div>
          )}
        </>
      )}
      {memberId && (
        <>
          <QueryState
            pending={member.isFetching}
            error={member.error}
            retry={() => void member.refetch()}
          />
          {current && (
            <>
              <section className="flex flex-wrap items-center gap-4 rounded-lg border p-4">
                <span className="flex size-12 items-center justify-center rounded-full bg-muted">
                  {current.name.slice(0, 2).toUpperCase()}
                </span>
                <div className="flex-1">
                  <h2 className="text-2xl font-semibold">{current.name}</h2>
                  <p className="text-sm text-muted-foreground">{current.email}</p>
                  <p className="text-sm text-muted-foreground">
                    {t('members.roleSummary', {
                      roles: currentRoles.length
                        ? currentRoles.map(roleName).join(t('common.listSeparator'))
                        : current.role === 'admin'
                          ? t('common.admin')
                          : t('common.member'),
                    })}
                  </p>
                </div>
                <Badge variant="outline">
                  {current.offboarded_at
                    ? t('common.offboarded')
                    : current.disabled
                      ? t('common.disabled')
                      : t('common.active')}
                </Badge>
              </section>
              <Tabs
                value={
                  [
                    'overview',
                    'teams',
                    'keys',
                    'limits',
                    'settings',
                    'models',
                    ...(access.can('roles.read') ? ['roles'] : []),
                  ].includes(params.get('tab') || '')
                    ? params.get('tab')!
                    : 'overview'
                }
                onValueChange={(value) =>
                  setParams(value === 'overview' ? {} : { tab: String(value) }, { replace: true })
                }
              >
                <TabsList>
                  <TabsTrigger value="overview">{t('members.overview')}</TabsTrigger>
                  {access.can('teams.read_all') && (
                    <TabsTrigger
                      value="teams"
                      id={`member-teams-tab-${actor}-${memberId}`}
                      aria-controls={`member-teams-panel-${actor}-${memberId}`}
                    >
                      {t('memberTeams.title')}
                    </TabsTrigger>
                  )}
                  <TabsTrigger
                    value="keys"
                    id={`member-keys-tab-${actor}-${memberId}`}
                    aria-controls={`member-keys-panel-${actor}-${memberId}`}
                  >
                    {t('memberKeys.title')}
                  </TabsTrigger>
                  <TabsTrigger
                    value="models"
                    id={`member-models-tab-${actor}-${memberId}`}
                    aria-controls={`member-models-panel-${actor}-${memberId}`}
                  >
                    {t('memberModels.title')}
                  </TabsTrigger>
                  <TabsTrigger
                    value="limits"
                    id={`member-limits-tab-${actor}-${memberId}`}
                    aria-controls={`member-limits-panel-${actor}-${memberId}`}
                  >
                    {t('members.limits')}
                  </TabsTrigger>
                  {access.can('roles.read') && (
                    <TabsTrigger value="roles">{t('members.rolesTab')}</TabsTrigger>
                  )}
                  <TabsTrigger
                    value="settings"
                    id={`member-settings-tab-${actor}-${memberId}`}
                    aria-controls={`member-settings-panel-${actor}-${memberId}`}
                  >
                    {t('members.settings')}
                  </TabsTrigger>
                </TabsList>
                <TabsContent value="overview" className="space-y-6">
                  <MemberOverview
                    key={`${actor}:${memberId}:${generation}`}
                    actor={actor}
                    target={memberId}
                    generation={generation}
                    authorized={!!current}
                  />
                  <section className="rounded-lg border">
                    <h3 className="border-b p-4 font-medium">{t('members.accessStatus')}</h3>
                    <dl className="grid gap-4 p-4 text-sm sm:grid-cols-3">
                      <div>
                        <dt className="text-muted-foreground">{t('members.id')}</dt>
                        <dd className="break-all">{current.id}</dd>
                      </div>
                      <div>
                        <dt className="text-muted-foreground">{t('common.status')}</dt>
                        <dd>
                          {current.offboarded_at
                            ? t('common.offboarded')
                            : current.disabled
                              ? t('common.disabled')
                              : t('common.active')}
                        </dd>
                      </div>
                      <div>
                        <dt className="text-muted-foreground">{t('members.joined')}</dt>
                        <dd>
                          {new Date(current.created_at).toLocaleString(
                            i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
                          )}
                        </dd>
                      </div>
                    </dl>
                  </section>
                </TabsContent>
                <TabsContent value="roles">
                  <QueryState
                    pending={roles.isPending}
                    error={roles.error}
                    retry={() => void roles.refetch()}
                  />
                  <form
                    key={current.role_ids.join(',')}
                    aria-label={t('members.roleLabel')}
                    className="space-y-4"
                    onSubmit={(event) => {
                      event.preventDefault()
                      if (mutation.isPending) return
                      dispatch({
                        method: 'put',
                        path: `/admin/members/${current.id}/roles`,
                        data: {
                          role_ids: new FormData(event.currentTarget)
                            .getAll('role_ids')
                            .map(String),
                        },
                      })
                    }}
                  >
                    <Table aria-label={t('members.rolesListLabel')}>
                      <thead>
                        <tr>
                          <th>{t('common.role')}</th>
                          <th>{t('common.type')}</th>
                          <th>{t('members.grant')}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {roles.data?.items.map((role) => (
                          <tr key={role.id}>
                            <td>{roleName(role)}</td>
                            <td>{role.builtin ? t('common.builtin') : t('common.custom')}</td>
                            <td>
                              {role.builtin ? (
                                role.id === `rol_${current.role}` ? (
                                  t('members.baseIdentity')
                                ) : (
                                  '—'
                                )
                              ) : (
                                <label className="flex items-center gap-2">
                                  <input
                                    name="role_ids"
                                    type="checkbox"
                                    value={role.id}
                                    aria-label={t('members.assignLabel', { name: roleName(role) })}
                                    checked={(draft?.owner === owner
                                      ? draft.roleIds
                                      : current.role_ids
                                    ).includes(role.id)}
                                    onChange={(event) => {
                                      const selected =
                                        draft?.owner === owner ? draft.roleIds : current.role_ids
                                      setDraft({
                                        owner,
                                        role: draft?.owner === owner ? draft.role : current.role,
                                        roleIds: event.target.checked
                                          ? [...selected, role.id]
                                          : selected.filter((id) => id !== role.id),
                                      })
                                    }}
                                    disabled={!access.isAdmin || mutation.isPending}
                                  />
                                  {t('members.assign')}
                                </label>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </Table>
                    <ErrorNotice error={mutation.error} />
                    {access.isAdmin && (
                      <div className="flex justify-end">
                        <SaveButton pending={mutation.isPending}>
                          {t('members.saveRoles')}
                        </SaveButton>
                      </div>
                    )}
                  </form>
                  <section className="mt-6 rounded-lg border">
                    <h3 className="border-b p-4 font-medium">
                      {t('members.effectivePermissions')}
                    </h3>
                    <PermissionRows
                      permissions={[...new Set(currentRoles.flatMap((role) => role.permissions))]}
                    />
                  </section>
                </TabsContent>
              </Tabs>
            </>
          )}
        </>
      )}
      <Dialog
        open={creating}
        onOpenChange={(open) => {
          if (!open) {
            setCreating(false)
            mutation.reset()
          }
        }}
        busy={mutation.isPending}
        title={t('members.create')}
        description={t('members.createDescription')}
      >
        <form onSubmit={create} className="space-y-5">
          <fieldset disabled={mutation.isPending} className="space-y-5">
            <FormField label={t('members.name')}>
              <Input name="name" required maxLength={100} />
            </FormField>
            <FormField label={t('members.email')}>
              <Input name="email" type="email" required maxLength={254} />
            </FormField>
            <FormField label={t('members.initialPassword')}>
              <Input name="password" type="password" autoComplete="new-password" required />
            </FormField>
            {access.isAdmin && (
              <FormField label={t('common.baseRole')}>
                <select name="role" className="h-10 w-full rounded-md border bg-background px-3">
                  <option value="member">{t('common.member')}</option>
                  <option value="admin">{t('common.admin')}</option>
                </select>
              </FormField>
            )}
          </fieldset>
          {validation && (
            <p role="alert" className="text-sm text-destructive">
              {t(validation)}
            </p>
          )}
          <ErrorNotice error={mutation.error} />
          <SaveButton pending={mutation.isPending}>{t('members.create')}</SaveButton>
        </form>
      </Dialog>
      <Dialog
        open={
          !!statusTarget &&
          (memberId
            ? !!current
            : !!statusListOwner &&
              statusListOwner === listOwner &&
              listReady &&
              listRows.includes(statusTarget as MemberListItem))
        }
        onOpenChange={(open) => {
          if (!open) setStatusTarget(null)
        }}
        busy={mutation.isPending}
        title={statusTarget?.disabled ? t('members.enableTitle') : t('members.disableTitle')}
        description={t(
          statusTarget?.disabled ? 'members.enableDescription' : 'members.disableDescription',
          { name: statusTarget?.name ?? '' },
        )}
      >
        <ErrorNotice error={mutation.error} />
        <Button
          disabled={mutation.isPending}
          onClick={() =>
            statusTarget &&
            dispatch(
              {
                method: 'patch',
                path: `/admin/members/${statusTarget.id}`,
                data: { disabled: !statusTarget.disabled },
              },
              statusListOwner
                ? { owner: statusListOwner, row: statusTarget as MemberListItem }
                : undefined,
            )
          }
        >
          {t(statusTarget?.disabled ? 'members.confirmEnable' : 'members.confirmDisable')}
        </Button>
      </Dialog>
    </Page>
  )
  return (
    <>
      {authorized ? page : unavailable}
      {memberId && params.get('tab') === 'settings' && (
        <section
          className="mt-6 space-y-6"
          role="tabpanel"
          id={`member-settings-panel-${actor}-${memberId}`}
          aria-labelledby={`member-settings-tab-${actor}-${memberId}`}
        >
          <MemberMetadata
            actor={actor}
            target={memberId}
            generation={generation}
            ready={!!current}
            email={current?.email ?? ''}
            targetQueryKey={['admin', 'member', actor, memberId, generation]}
          />
          {current && (
            <div className="space-y-6">
              <section className="rounded-lg border">
                <h3 className="border-b p-4 font-medium">{t('common.baseRole')}</h3>
                <form
                  className="space-y-4 p-4"
                  aria-label={t('common.baseRole')}
                  onSubmit={(event) => {
                    event.preventDefault()
                    if (mutation.isPending) return
                    dispatch({
                      method: 'patch',
                      path: `/admin/members/${current.id}`,
                      data: { role: String(new FormData(event.currentTarget).get('role')) },
                    })
                  }}
                >
                  <FormField label={t('common.baseRole')}>
                    <select
                      name="role"
                      className="h-10 rounded-md border bg-background px-3"
                      value={draft?.owner === owner ? draft.role : current.role}
                      onChange={(event) =>
                        setDraft({
                          owner,
                          role: event.target.value,
                          roleIds: draft?.owner === owner ? draft.roleIds : current.role_ids,
                        })
                      }
                      disabled={!access.isAdmin || mutation.isPending}
                    >
                      <option value="member">{t('common.member')}</option>
                      <option value="admin">{t('common.admin')}</option>
                    </select>
                  </FormField>
                  <ErrorNotice error={mutation.error} />
                  {access.isAdmin && (
                    <SaveButton pending={mutation.isPending}>
                      {t('members.saveBaseRole')}
                    </SaveButton>
                  )}
                </form>
              </section>
              <section className="rounded-lg border p-4">
                <h3 className="mb-3 font-medium">{t('members.offboarding')}</h3>
                <p className="mb-4 text-sm text-muted-foreground">{t('members.offboardingHelp')}</p>
                <Button
                  variant="outline"
                  onClick={() => navigate(`/admin/members/${current.id}/offboarding`)}
                >
                  {t('members.reviewOffboarding')}
                </Button>
              </section>
              {canChange(current) && (
                <section className="rounded-lg border p-4">
                  <h3 className="mb-3 font-medium">{t('members.accountAccess')}</h3>
                  <p className="mb-4 text-sm text-muted-foreground">
                    {t('members.disableExplanation')}
                  </p>
                  <Button
                    variant="outline"
                    onClick={() => {
                      mutation.reset()
                      setStatusTarget(current)
                    }}
                  >
                    {current.disabled ? t('common.enable') : t('common.disable')}
                  </Button>
                </section>
              )}
            </div>
          )}
        </section>
      )}
      {memberId && params.get('tab') === 'limits' && (
        <section
          className="mt-6"
          role="tabpanel"
          id={`member-limits-panel-${actor}-${memberId}`}
          aria-labelledby={`member-limits-tab-${actor}-${memberId}`}
        >
          <MemberLimits
            actor={actor}
            target={memberId}
            generation={generation}
            ready={!!current}
            targetQueryKey={['admin', 'member', actor, memberId, generation]}
          />
        </section>
      )}
      {memberId && params.get('tab') === 'keys' && (
        <section
          className="mt-6"
          role="tabpanel"
          id={`member-keys-panel-${actor}-${memberId}`}
          aria-labelledby={`member-keys-tab-${actor}-${memberId}`}
        >
          <MemberKeys
            userId={memberId}
            actorId={session.data?.user.id ?? ''}
            csrf={session.data?.csrf_token ?? ''}
            generation={generation}
            sessionReady={!!actor && !session.isError && !session.isFetching}
            targetReady={!!current && current.id === memberId}
            targetQueryKey={['admin', 'member', actor, memberId, generation]}
          />
        </section>
      )}
      {memberId && params.get('tab') === 'teams' && (
        <section
          className="mt-6"
          role="tabpanel"
          id={`member-teams-panel-${actor}-${memberId}`}
          aria-label={t('memberTeams.title')}
        >
          <MemberTeams
            actor={actor}
            target={memberId}
            generation={generation}
            ready={!!current}
            targetQueryKey={['admin', 'member', actor, memberId, generation]}
          />
        </section>
      )}
      {memberId && params.get('tab') === 'models' && (
        <section
          className="mt-6"
          role="tabpanel"
          id={`member-models-panel-${actor}-${memberId}`}
          aria-labelledby={`member-models-tab-${actor}-${memberId}`}
        >
          <MemberModels
            actor={actor}
            target={memberId}
            generation={generation}
            ready={!!current}
            targetQueryKey={['admin', 'member', actor, memberId, generation]}
          />
        </section>
      )}
    </>
  )
}
