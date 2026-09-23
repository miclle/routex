import { useTranslation } from 'react-i18next'
import { useState, type FormEvent } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router'
import { Plus } from 'lucide-react'
import { getMember, getMembers, getRoles } from '@/api/governance'
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
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { PermissionRows } from './roles'
import type { Member, MemberFilters } from '@/types/governance'

export default function MembersPage() {
  return (
    <PermissionGate permission="members.read">
      <Members />
    </PermissionGate>
  )
}
function Members() {
  const { t, i18n } = useTranslation('governance')
  const { memberId } = useParams()
  const [params, setParams] = useSearchParams()
  const access = usePermissions()
  const session = useSession()
  const cache = useQueryClient()
  const navigate = useNavigate()
  const [filters, setFilters] = useState<MemberFilters>({})
  const [creating, setCreating] = useState(false)
  const [statusTarget, setStatusTarget] = useState<Member | null>(null)
  const [validation, setValidation] = useState<'members.passwordValidation' | null>(null)
  const members = useInfiniteQuery({
    queryKey: ['admin', 'members', filters],
    queryFn: ({ pageParam, signal }) => getMembers(filters, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: !memberId,
  })
  const member = useQuery({
    queryKey: ['admin', 'member', memberId],
    queryFn: ({ signal }) => getMember(memberId!, signal),
    enabled: !!memberId,
  })
  const roles = useQuery({
    queryKey: ['admin', 'roles'],
    queryFn: ({ signal }) => getRoles(signal),
    enabled: !!memberId && access.can('roles.read'),
  })
  const mutation = useMutation({
    mutationFn: ({
      method,
      path,
      data,
    }: {
      method: 'post' | 'put' | 'patch'
      path: string
      data: unknown
    }) => writeCatalog<Member>(method, path, data, session.data!.csrf_token),
    gcTime: 0,
    onSuccess: (result, input) => {
      setCreating(false)
      setStatusTarget(null)
      cache.setQueryData(['admin', 'member', result.id], result)
      void cache.invalidateQueries({ queryKey: ['admin', 'members'] })
      void cache.invalidateQueries({ queryKey: ['permissions'] })
      void cache.invalidateQueries({ queryKey: ['auth', 'session'] })
      mutation.reset()
      if (input.method === 'post') navigate(`/admin/members/${result.id}`)
    },
  })
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
    mutation.mutate({
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
  const current = member.data
  const currentRoles =
    roles.data?.items.filter(
      (role) =>
        current && (role.id === `rol_${current.role}` || current.role_ids.includes(role.id)),
    ) ?? []
  return (
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
            pending={members.isPending}
            error={members.error}
            retry={() => void members.refetch()}
            empty={members.isSuccess && !members.data.pages.some((page) => page.items.length)}
          />
          <Table aria-label={t('members.listLabel')}>
            <thead>
              <tr>
                <th>{t('common.member')}</th>
                <th>{t('members.email')}</th>
                <th>{t('common.role')}</th>
                <th>{t('common.status')}</th>
                <th>{t('members.joined')}</th>
                <th>{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {members.data?.pages
                .flatMap((page) => page.items)
                .map((item) => (
                  <tr key={item.id}>
                    <td>
                      <Link to={`/admin/members/${item.id}`}>{item.name}</Link>
                    </td>
                    <td>{item.email}</td>
                    <td>{item.role === 'admin' ? t('common.admin') : t('common.member')}</td>
                    <td>
                      <Badge variant="outline">
                        {item.disabled ? t('common.disabled') : t('common.active')}
                      </Badge>
                    </td>
                    <td>
                      {new Date(item.created_at).toLocaleString(
                        i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
                      )}
                    </td>
                    <td>
                      {canChange(item) && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            mutation.reset()
                            setStatusTarget(item)
                          }}
                        >
                          {item.disabled ? t('common.enable') : t('common.disable')}
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
            </tbody>
          </Table>
          {members.hasNextPage && (
            <div className="text-center">
              <Button
                variant="outline"
                disabled={members.isFetchingNextPage}
                onClick={() => void members.fetchNextPage()}
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
            pending={member.isPending}
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
                  {current.disabled ? t('common.disabled') : t('common.active')}
                </Badge>
              </section>
              <Tabs
                value={
                  ['overview', 'settings', ...(access.can('roles.read') ? ['roles'] : [])].includes(
                    params.get('tab') || '',
                  )
                    ? params.get('tab')!
                    : 'overview'
                }
                onValueChange={(value) =>
                  setParams(value === 'overview' ? {} : { tab: String(value) }, { replace: true })
                }
              >
                <TabsList>
                  <TabsTrigger value="overview">{t('members.overview')}</TabsTrigger>
                  {access.can('roles.read') && (
                    <TabsTrigger value="roles">{t('members.rolesTab')}</TabsTrigger>
                  )}
                  <TabsTrigger value="settings">{t('members.settings')}</TabsTrigger>
                </TabsList>
                <TabsContent value="overview">
                  <section className="rounded-lg border">
                    <h3 className="border-b p-4 font-medium">{t('members.accessStatus')}</h3>
                    <dl className="grid gap-4 p-4 text-sm sm:grid-cols-3">
                      <div>
                        <dt className="text-muted-foreground">{t('members.id')}</dt>
                        <dd className="break-all">{current.id}</dd>
                      </div>
                      <div>
                        <dt className="text-muted-foreground">{t('common.status')}</dt>
                        <dd>{current.disabled ? t('common.disabled') : t('common.active')}</dd>
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
                      mutation.mutate({
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
                                    defaultChecked={current.role_ids.includes(role.id)}
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
                <TabsContent value="settings">
                  <div className="space-y-6">
                    <section className="rounded-lg border">
                      <h3 className="border-b p-4 font-medium">{t('members.basicInfo')}</h3>
                      <form
                        className="space-y-4 p-4"
                        aria-label={t('common.baseRole')}
                        onSubmit={(event) => {
                          event.preventDefault()
                          if (mutation.isPending) return
                          mutation.mutate({
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
                            defaultValue={current.role}
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
        open={!!statusTarget}
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
            mutation.mutate({
              method: 'patch',
              path: `/admin/members/${statusTarget.id}`,
              data: { disabled: !statusTarget.disabled },
            })
          }
        >
          {t(statusTarget?.disabled ? 'members.confirmEnable' : 'members.confirmDisable')}
        </Button>
      </Dialog>
    </Page>
  )
}
