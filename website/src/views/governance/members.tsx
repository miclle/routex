import MemberAccessSummary, { MemberAccessRoleSummary } from './member-access-summary'
import { useMemberAccessSummary } from './member-access-summary-read'
import MemberApproval from './member-approval'
import MemberMetadata from './member-metadata'
import MemberOffboardingSummary from './member-offboarding-summary'
import MemberList from './member-list'
import MemberCreate from './member-create'
import { getMemberList, validateMemberListChain } from '@/api/member-list'
import type { MemberListItem } from '@/types/member-list'
import type { InfiniteData } from '@tanstack/react-query'
import type { MemberListPage } from '@/types/member-list'
import MemberEffectiveModels from './member-effective-models'
import MemberTeams from './member-teams'
import MemberKeys from './member-keys'
import MemberOverview from './member-overview'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import MemberModels from './member-models'
import MemberLimits from './member-limits'
import { useTranslation } from 'react-i18next'
import {
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams, useSearchParams } from 'react-router'
import { Plus } from 'lucide-react'
import MemberRoles from './member-roles'
import MemberState from './member-state'
import { getMemberDetail } from '@/api/member-recent-login'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import type { Session } from '@/types/auth'
import type { Member, MemberFilters } from '@/types/governance'

// Observe cache invalidation synchronously without adding authentication observers.
function useMemberReadState(actor: string, target: string | undefined, generation: number) {
  const cache = useQueryClient()
  const keys = useMemo(
    () => [
      ['auth', 'session'],
      ['permissions', actor],
      ['admin', 'member', actor, target, generation],
    ],
    [actor, target, generation],
  )
  const serialized = useMemo(() => keys.map((key) => JSON.stringify(key)), [keys])
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (serialized.includes(JSON.stringify(event.query.queryKey))) notify()
      }),
    [cache, serialized],
  )
  const snapshot = useCallback(
    () =>
      keys
        .map((key) => {
          const state = cache.getQueryState(key)
          return `${state?.dataUpdateCount}:${state?.errorUpdateCount}:${state?.status}:${state?.fetchStatus}:${state?.isInvalidated}`
        })
        .join('|'),
    [cache, keys],
  )
  useSyncExternalStore(subscribe, snapshot, snapshot)
  return {
    authorityInvalidated: keys.slice(0, 2).some((key) => cache.getQueryState(key)?.isInvalidated),
    targetInvalidated: cache.getQueryState(keys[2])?.isInvalidated === true,
  }
}

export default function MembersPage() {
  return <Members />
}
function Members() {
  const { t } = useTranslation('governance')
  const { memberId } = useParams()
  const [params, setParams] = useSearchParams()
  const access = usePermissions()
  const session = useSession()
  const generation = useSessionGeneration()
  const cache = useQueryClient()
  const navigate = useNavigate()
  const actor = session.data?.user.id ?? ''
  const readState = useMemberReadState(actor, memberId, generation)
  const authorized =
    !!actor &&
    !readState.authorityInvalidated &&
    !session.isError &&
    !session.isFetching &&
    !access.isError &&
    !access.isFetching &&
    access.can('members.read')
  const owner = `${actor}:${memberId ?? ''}`
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
  const [filters, setFilters] = useState<MemberFilters>({})
  const [creating, setCreating] = useState<{
    actor: string
    generation: number
    trigger: HTMLButtonElement
  } | null>(null)
  const [statusSelection, setStatusSelection] = useState<{
    owner: string
    target: string
    trigger: HTMLButtonElement | null
  } | null>(null)
  const teamRead = access.can('teams.read_all')
  const listOwner = JSON.stringify([actor, generation, filters, teamRead])
  const statusOwner = JSON.stringify([actor, filters])
  const approvalOwner = JSON.stringify([
    actor,
    memberId ?? '',
    memberId ? (params.get('tab') ?? 'overview') : filters,
  ])
  const [approvalSelection, setApprovalSelection] = useState<{
    open: boolean
    owner: string
    target: string
    trigger: HTMLButtonElement | null
  } | null>(null)
  const approvalFocusOwner = useRef(approvalOwner)
  useLayoutEffect(() => {
    approvalFocusOwner.current = approvalOwner
  }, [approvalOwner])
  if (approvalSelection && approvalSelection.owner !== approvalOwner) setApprovalSelection(null)
  const statusFocusOwner = useRef(statusOwner)
  useLayoutEffect(() => {
    statusFocusOwner.current = statusOwner
  }, [statusOwner])
  const listQueryKey = useMemo(
    () => ['admin', 'members', actor, generation, filters, teamRead] as const,
    [actor, generation, filters, teamRead],
  )
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
  if (statusSelection && (statusSelection.owner !== statusOwner || memberId))
    setStatusSelection(null)
  const member = useQuery({
    queryKey: ['admin', 'member', actor, memberId, generation],
    queryFn: ({ signal }) => getMemberDetail(memberId!, signal),
    enabled: !!memberId && authorized,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const [previousOwner, setPreviousOwner] = useState(owner)
  if (previousOwner !== owner) {
    setPreviousOwner(owner)
    setCreating(null)
    setStatusSelection(null)
  }
  const canChange = (target: Member) =>
    access.can('members.write') &&
    (access.isAdmin || (target.role !== 'admin' && target.id !== session.data?.user.id))
  const current =
    authorized &&
    !readState.targetInvalidated &&
    member.isSuccess &&
    !member.isFetching &&
    member.data.id === memberId
      ? member.data
      : undefined
  const accessSummary = useMemberAccessSummary({
    actor,
    target: memberId ?? '',
    generation,
    ready: !!current,
    targetQueryKey: ['admin', 'member', actor, memberId, generation],
  })
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
                const nextFilters = {
                  q: String(form.get('q') || '').trim(),
                  status: String(form.get('status') || ''),
                  role: String(form.get('role') || ''),
                }
                approvalFocusOwner.current = JSON.stringify([actor, '', nextFilters])
                statusFocusOwner.current = JSON.stringify([actor, nextFilters])
                setFilters(nextFilters)
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
                onClick={(event) => {
                  setCreating({ actor, generation, trigger: event.currentTarget })
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
              canApprove={authorized && access.can('members.approvals.write')}
              onApproval={(item, trigger) => {
                if (canListAct(item.id, 'members.approvals.write'))
                  setApprovalSelection({
                    open: true,
                    owner: approvalOwner,
                    target: item.id,
                    trigger,
                  })
              }}
              canAct={canListAct}
              canChange={canChange}
              onStatus={(item, trigger) => {
                if (canListAct(item.id, 'members.write') && canChange(item)) {
                  setStatusSelection({ owner: statusOwner, target: item.id, trigger })
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
                <div className="min-w-0 flex-1">
                  <h2 className="text-2xl font-semibold">{current.name}</h2>
                  <p className="text-sm text-muted-foreground">{current.email}</p>
                  <p className="truncate text-sm text-muted-foreground">
                    <MemberAccessRoleSummary read={accessSummary} />
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
                onValueChange={(value) => {
                  approvalFocusOwner.current = JSON.stringify([actor, memberId, String(value)])
                  setParams(value === 'overview' ? {} : { tab: String(value) }, { replace: true })
                }}
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
                    <TabsTrigger
                      value="roles"
                      id={`member-roles-tab-${actor}-${memberId}`}
                      aria-controls={`member-roles-panel-${actor}-${memberId}`}
                    >
                      {t('members.rolesTab')}
                    </TabsTrigger>
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
                  <MemberAccessSummary
                    read={accessSummary}
                    member={current}
                    canApprove={authorized && access.can('members.approvals.write')}
                    onApproval={(trigger) => {
                      if (
                        authorized &&
                        access.can('members.approvals.write') &&
                        accessSummary.current()
                      )
                        setApprovalSelection({
                          open: true,
                          owner: approvalOwner,
                          target: current.id,
                          trigger,
                        })
                    }}
                  />
                  <MemberEffectiveModels
                    actor={actor}
                    target={memberId}
                    generation={generation}
                    ready={!!current}
                    targetQueryKey={['admin', 'member', actor, memberId, generation]}
                  />
                </TabsContent>
              </Tabs>
            </>
          )}
        </>
      )}
    </Page>
  )
  return (
    <>
      {authorized ? page : unavailable}
      {creating && !memberId && (
        <MemberCreate
          key={actor}
          actor={actor}
          ready={authorized && access.can('members.write')}
          onClose={() => setCreating(null)}
          returnFocus={() =>
            latest.current.actor === creating.actor &&
            latest.current.generation === creating.generation &&
            !latest.current.memberId &&
            latest.current.authorized &&
            latest.current.write
              ? creating.trigger
              : false
          }
          onCreated={(id) => {
            setCreating(null)
            void cache.invalidateQueries({ queryKey: ['admin', 'members'] })
            void cache.invalidateQueries({ queryKey: ['permissions'] })
            void cache.invalidateQueries({ queryKey: ['auth', 'session'] })
            navigate(`/admin/members/${id}`)
          }}
        />
      )}
      {approvalSelection && approvalSelection.owner === approvalOwner && (
        <MemberApproval
          actor={actor}
          target={approvalSelection.target}
          generation={generation}
          ready={
            authorized &&
            access.can('members.approvals.write') &&
            (memberId
              ? !!current
              : listReady && listRows.some((row) => row.id === approvalSelection.target))
          }
          contextKind={memberId ? 'detail' : 'list'}
          contextQueryKey={
            memberId ? ['admin', 'member', actor, memberId, generation] : listQueryKey
          }
          open={approvalSelection.open}
          onClose={() =>
            setApprovalSelection((selection) => selection && { ...selection, open: false })
          }
          returnFocus={() =>
            approvalFocusOwner.current === approvalSelection.owner &&
            latest.current.actor === actor &&
            latest.current.generation === generation &&
            approvalSelection.trigger?.isConnected &&
            (memberId
              ? !!current && accessSummary.current()
              : canListAct(approvalSelection.target, 'members.approvals.write'))
              ? approvalSelection.trigger
              : false
          }
        />
      )}
      {!memberId && statusSelection && statusSelection.owner === statusOwner && (
        <MemberState
          actor={actor}
          target={statusSelection.target}
          generation={generation}
          ready={listReady && listRows.some((row) => row.id === statusSelection.target)}
          owner={statusOwner}
          mode="status"
          contextKind="list"
          contextQueryKey={listQueryKey}
          returnFocus={() =>
            statusFocusOwner.current === statusSelection.owner &&
            latest.current.actor === actor &&
            latest.current.generation === generation &&
            listLatest.current.owner === listOwner &&
            canListAct(statusSelection.target, 'members.write') &&
            statusSelection.trigger?.isConnected
              ? statusSelection.trigger
              : false
          }
          onClose={() => setStatusSelection(null)}
        />
      )}
      {memberId && params.get('tab') === 'roles' && (
        <section
          className="mt-6"
          role="tabpanel"
          id={`member-roles-panel-${actor}-${memberId}`}
          aria-labelledby={`member-roles-tab-${actor}-${memberId}`}
        >
          <MemberRoles
            actor={actor}
            target={memberId}
            generation={generation}
            ready={!!current}
            targetQueryKey={['admin', 'member', actor, memberId, generation]}
          />
        </section>
      )}
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
          <div className="space-y-6">
            <MemberState
              actor={actor}
              target={memberId}
              generation={generation}
              ready={!!current}
              owner="settings"
              mode="settings"
              contextKind="detail"
              contextQueryKey={['admin', 'member', actor, memberId, generation]}
            >
              <MemberOffboardingSummary
                actor={actor}
                target={memberId}
                generation={generation}
                ready={!!current}
                targetQueryKey={['admin', 'member', actor, memberId, generation]}
              />
            </MemberState>
          </div>
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
