import { builtinRoleNameKey } from '@/lib/role-assignment'
import { useCallback, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { AxiosError } from 'axios'
import {
  getMemberRoles,
  getMemberRoleCandidates,
  getMemberRoleDetail,
  setMemberRoles,
  validMemberRolesReason,
  validMemberRoleSearch,
} from '@/api/member-roles'
import type {
  MemberRoleSummary,
  MemberRolesInput,
  MemberRolesWorkspace,
} from '@/types/member-roles'
import type { MemberDetail } from '@/types/member-recent-login'
import type { Session } from '@/types/auth'
import { FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Table } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Dialog } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { MultiSelect } from '@/components/ui/multi-select'
import { PermissionRows } from './roles'

type Props = {
  actor: string
  target: string
  generation: number
  ready: boolean
  targetQueryKey: readonly unknown[]
}
type Draft = { etag: string; builtin: string; ids: string[]; rows: MemberRoleSummary[] }
type Intent = { etag: string; body: MemberRolesInput }
function useCacheRevision(keys: string) {
  const cache = useQueryClient()
  const snapshot = useCallback(
    () =>
      JSON.parse(keys)
        .map((key: unknown[]) => {
          const state = cache.getQueryState(key)
          return `${state?.status}:${state?.fetchStatus}:${state?.isInvalidated}:${state?.dataUpdateCount}:${state?.errorUpdateCount}`
        })
        .join('|'),
    [cache, keys],
  )
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (
          JSON.parse(keys).some(
            (key: unknown[]) => JSON.stringify(key) === JSON.stringify(event.query.queryKey),
          )
        )
          notify()
      }),
    [cache, keys],
  )
  return { snapshot, revision: useSyncExternalStore(subscribe, snapshot, snapshot) }
}
export default function MemberRoles(props: Props) {
  return <Roles key={`${props.actor}:${props.target}`} {...props} />
}
function Roles({ actor, target, generation, ready, targetQueryKey }: Props) {
  const { t } = useTranslation('governance')
  const cache = useQueryClient()
  const parent = useCacheRevision(
    JSON.stringify([['auth', 'session'], ['permissions', actor], targetQueryKey]),
  )
  function authority(write = false) {
    const session = cache.getQueryState<Session>(['auth', 'session']),
      grants = cache.getQueryState<string[]>(['permissions', actor]),
      subject = cache.getQueryState<MemberDetail>(targetQueryKey)
    return (
      ready &&
      !!actor &&
      !!target &&
      parent.snapshot() === parent.revision &&
      [session, grants, subject].every(
        (s) => s?.status === 'success' && s.fetchStatus === 'idle' && !s.isInvalidated && !s.error,
      ) &&
      session?.data?.user.id === actor &&
      subject?.data?.id === target &&
      grants?.data?.includes('members.read') === true &&
      grants.data.includes('roles.read') &&
      (!write ||
        (session.data.user.role === 'admin' &&
          !!session.data.csrf_token &&
          !subject.data.offboarded_at))
    )
  }
  const workspaceKey = ['admin', 'member-roles', actor, target, generation, parent.revision]
  const query = useQuery({
    queryKey: workspaceKey,
    queryFn: async ({ signal }) => {
      if (!authority()) throw new Error('Member Roles authority unavailable')
      const page = await getMemberRoles(target, signal),
        subject = cache.getQueryData<MemberDetail>(targetQueryKey)
      const status = subject?.offboarded_at
        ? 'offboarded'
        : subject?.disabled
          ? 'disabled'
          : 'active'
      if (
        signal.aborted ||
        !authority() ||
        page.identity_role !== subject?.role ||
        page.subject_status !== status
      )
        throw new Error('Member Roles authority unavailable')
      return page
    },
    enabled: authority(),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const workspace = useCacheRevision(JSON.stringify([workspaceKey]))
  function current(write = false) {
    const state = cache.getQueryState<MemberRolesWorkspace>(workspaceKey)
    return (
      authority(write) &&
      workspace.snapshot() === workspace.revision &&
      query.isSuccess &&
      !query.isFetching &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      state.data === query.data &&
      (!write || state.data.can_edit)
    )
  }
  const fresh = current(),
    editable = current(true),
    page = fresh ? query.data : undefined
  const [draft, setDraft] = useState<Draft | null>(null)
  const [chosen, setChosen] = useState<{ etag: string; rows: MemberRoleSummary[] } | null>(null)
  const [search, setSearch] = useState('')
  const [reason, setReason] = useState('')
  const [intent, setIntent] = useState<Intent | null>(null)
  const operationScope = `${generation}:${parent.revision}:${workspace.revision}`
  const [busyScope, setBusyScope] = useState<string | null>(null)
  const busy = editable && busyScope === operationScope
  const [confirming, setConfirming] = useState(false)
  const [viewing, setViewing] = useState<{
    etag: string
    scope: string
    role: MemberRoleSummary
  } | null>(null)
  const [notice, setNotice] = useState<
    'invalidReason' | 'uncertain' | 'conflict' | 'confirmed' | 'reconciled' | null
  >(null)
  const [confirmed, setConfirmed] = useState<string | null>(null)
  const alive = useRef(true),
    lock = useRef(false),
    serial = useRef(0),
    controller = useRef<AbortController | null>(null)
  useLayoutEffect(() => {
    if (!editable) {
      serial.current++
      controller.current?.abort()
      lock.current = false
    }
  }, [editable, generation, parent.revision])
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      controller.current?.abort()
    }
  }, [])
  const candidateKey = [
    'admin',
    'member-role-candidates',
    actor,
    target,
    generation,
    parent.revision,
    page?.etag,
    search,
  ]
  const candidateScope = JSON.stringify(candidateKey)
  const latestCandidateScope = useRef(candidateScope)
  useLayoutEffect(() => {
    latestCandidateScope.current = candidateScope
  }, [candidateScope])
  const candidates = useInfiniteQuery({
    queryKey: candidateKey,
    initialPageParam: null as string | null,
    queryFn: async ({ pageParam, signal }) => {
      if (!current(true) || !page) throw new Error('Member Role candidates unavailable')
      const result = await getMemberRoleCandidates(
        target,
        page.etag,
        { q: search, cursor: pageParam },
        signal,
      )
      if (signal.aborted || !current(true) || latestCandidateScope.current !== candidateScope)
        throw new Error('Member Role candidates unavailable')
      return result
    },
    getNextPageParam: (result) => result.next_cursor ?? undefined,
    enabled: editable && !intent && validMemberRoleSearch(search),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const candidate = useCacheRevision(JSON.stringify([candidateKey]))
  function candidateCurrent() {
    const state = cache.getQueryState(candidateKey)
    const pages = candidates.data?.pages ?? [],
      rows = pages.flatMap((p) => p.items)
    return (
      current(true) &&
      validMemberRoleSearch(search) &&
      candidate.snapshot() === candidate.revision &&
      candidates.isSuccess &&
      !candidates.isFetching &&
      !candidates.isError &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      pages.every((p) => p.etag === page?.etag) &&
      rows.length <= 1000 &&
      rows.every((r, i) => i === 0 || rows[i - 1].id < r.id) &&
      new Set(pages.map((p) => p.next_cursor).filter(Boolean)).size ===
        pages.filter((p) => p.next_cursor).length
    )
  }
  function candidateCanAct() {
    return latestCandidateScope.current === candidateScope && candidateCurrent()
  }
  const candidateRows = candidateCurrent() ? candidates.data!.pages.flatMap((p) => p.items) : []
  const known = new Map((page?.assigned_roles ?? []).map((r) => [r.id, r]))
  if (draft?.etag === page?.etag) draft?.rows.forEach((r) => known.set(r.id, r))
  candidateRows.forEach((r) => known.set(r.id, r))
  const ids = draft?.ids ?? page?.assigned_roles.map((r) => r.id) ?? []
  const baseline = page?.assigned_roles.map((r) => r.id) ?? []
  const changed = JSON.stringify(ids) !== JSON.stringify(baseline)
  const outdated = !!draft && draft.etag !== page?.etag
  const missing = ids.some((id) => !known.has(id))
  const detailKey = [
    'admin',
    'member-role-definition',
    actor,
    target,
    generation,
    parent.revision,
    viewing?.etag,
    viewing?.role.id,
    viewing?.role.definition_etag,
  ]
  const dialogAllowed =
    fresh &&
    !!viewing &&
    viewing.etag === page?.etag &&
    viewing.scope === operationScope &&
    (viewing.role.assignment_kind === 'intrinsic' ||
      baseline.includes(viewing.role.id) ||
      (editable && known.get(viewing.role.id)?.definition_etag === viewing.role.definition_etag))
  const detail = useQuery({
    queryKey: detailKey,
    queryFn: async ({ signal }) => {
      if (!current() || !viewing || viewing.etag !== query.data?.etag)
        throw new Error('Member Role definition unavailable')
      const result = await getMemberRoleDetail(target, viewing.etag, viewing.role, signal)
      if (signal.aborted || !current() || viewing.etag !== query.data?.etag)
        throw new Error('Member Role definition unavailable')
      return result
    },
    enabled: dialogAllowed,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const definition = useCacheRevision(JSON.stringify([detailKey]))
  const definitionState = cache.getQueryState(detailKey)
  const detailFresh =
    dialogAllowed &&
    detail.isSuccess &&
    !detail.isFetching &&
    definition.snapshot() === definition.revision &&
    definitionState?.status === 'success' &&
    definitionState.fetchStatus === 'idle' &&
    !definitionState.isInvalidated &&
    !definitionState.error
  function stage(nextIDs: string[], extra: MemberRoleSummary[] = []) {
    if (!current(true) || busy || intent || outdated || !page) return
    const rows = new Map([...known.values(), ...extra].map((r) => [r.id, r]))
    setDraft({
      etag: draft?.etag ?? page.etag,
      builtin: draft?.builtin ?? page.builtin_role.definition_etag,
      ids: [...new Set(nextIDs)].sort(),
      rows: [...rows.values()],
    })
    setNotice(null)
  }
  async function reviewCurrent() {
    if (!current(true) || busy || intent) return
    setViewing(null)
    const captured = parent.revision
    const result = await query.refetch()
    const state = cache.getQueryState<MemberRolesWorkspace>(workspaceKey)
    if (
      !alive.current ||
      !result.data ||
      !authority(true) ||
      !result.data.can_edit ||
      state?.data !== result.data ||
      state.fetchStatus !== 'idle' ||
      state.isInvalidated ||
      state.error ||
      captured !== parent.snapshot()
    )
      return
    const currentRows = new Map(result.data.assigned_roles.map((r) => [r.id, r]))
    setDraft({
      etag: result.data.etag,
      builtin: result.data.builtin_role.definition_etag,
      ids: [...ids],
      rows: ids.flatMap((id) => (currentRows.get(id) ? [currentRows.get(id)!] : [])),
    })
    setChosen(null)
    setNotice(null)
  }
  async function dispatch() {
    if (!current(true) || lock.current || !alive.current || !page) return
    if (
      !intent &&
      (!draft || outdated || missing || ids.length > 100 || !validMemberRolesReason(reason))
    ) {
      setNotice('invalidReason')
      return
    }
    const original = intent
    const captured = original ?? {
      etag: draft!.etag,
      body: {
        role_ids: [...ids],
        role_definitions: ids.map((id) => ({ id, etag: known.get(id)!.definition_etag })),
        builtin_definition_etag: draft!.builtin,
        reason,
      },
    }
    setIntent(captured)
    setNotice('uncertain')
    setBusyScope(operationScope)
    lock.current = true
    const operation = ++serial.current,
      pending = new AbortController()
    controller.current = pending
    try {
      const result = await setMemberRoles(
        target,
        captured.etag,
        captured.body,
        cache.getQueryData<Session>(['auth', 'session'])!.csrf_token,
        pending.signal,
      )
      if (
        !alive.current ||
        pending.signal.aborted ||
        operation !== serial.current ||
        !current(true)
      )
        return
      setConfirmed(result.etag)
      setNotice(original ? 'reconciled' : 'confirmed')
      setIntent(null)
      setDraft(null)
      setChosen(null)
      setReason('')
      setConfirming(false)
      void cache.invalidateQueries({ queryKey: ['admin', 'member-roles', actor, target] })
      void cache.invalidateQueries({ queryKey: ['admin', 'member-role-candidates', actor, target] })
      void cache.invalidateQueries({ queryKey: ['admin', 'member-access-summary', actor, target] })
      void cache.invalidateQueries({ queryKey: ['admin', 'members'] })
      void cache.invalidateQueries({ queryKey: targetQueryKey })
      void cache.invalidateQueries({ queryKey: ['permissions'] })
    } catch (error) {
      if (alive.current && operation === serial.current) {
        const conflict = error instanceof AxiosError && error.response?.status === 409
        setNotice(conflict ? 'conflict' : 'uncertain')
        if (conflict && !original) {
          setIntent(null)
          setConfirming(false)
          void query.refetch()
        }
      }
    } finally {
      if (alive.current && operation === serial.current) {
        lock.current = false
        setBusyScope(null)
      }
    }
  }
  const roleName = (r: MemberRoleSummary) => {
    const key = builtinRoleNameKey(r)
    return key ? t(key) : r.name
  }
  const selectedRows = chosen && page && chosen.etag === page.etag ? chosen.rows : []
  function openPermissions(role: MemberRoleSummary) {
    if (current()) setViewing({ etag: page!.etag, scope: operationScope, role })
  }
  return (
    <div className="space-y-6">
      {!authority() ? (
        <p role="status">{t('memberRoles.denied')}</p>
      ) : !fresh ? (
        <div>
          <p role={query.isError ? 'alert' : 'status'}>
            {t(query.isError ? 'memberRoles.failed' : 'memberRoles.loading')}
          </p>
          {query.isError && (
            <Button
              variant="outline"
              onClick={() => {
                if (authority()) void query.refetch()
              }}
            >
              {t('memberRoles.refresh')}
            </Button>
          )}
        </div>
      ) : (
        <>
          <p className="text-sm text-muted-foreground">{t('memberRoles.help')}</p>
          <section className="rounded-lg border">
            <h3 className="border-b p-4 font-medium">{t('memberRoles.title')}</h3>
            <Table className="min-w-[980px] table-fixed" aria-label={t('memberRoles.table')}>
              <thead>
                <tr>
                  <th className="w-80">{t('common.role')}</th>
                  <th className="w-30">{t('common.type')}</th>
                  <th>{t('memberRoles.permissions')}</th>
                  <th className="w-25">{t('memberRoles.actions')}</th>
                </tr>
              </thead>
              <tbody>
                {[
                  page!.builtin_role,
                  ...ids.map(
                    (id) =>
                      known.get(id) ?? {
                        id,
                        name: id,
                        builtin: false,
                        assignment_kind: 'explicit' as const,
                        permission_count: 0,
                        definition_etag: '',
                      },
                  ),
                ].map((role) => (
                  <tr key={role.id}>
                    <td>{roleName(role)}</td>
                    <td>
                      <Badge variant="outline">
                        {t(role.builtin ? 'common.builtin' : 'common.custom')}
                      </Badge>
                    </td>
                    <td>
                      {role.definition_etag ? (
                        <Button
                          size="sm"
                          variant="ghost"
                          aria-label={t('memberRoles.viewPermissions', { name: roleName(role) })}
                          onClick={() => openPermissions(role)}
                        >
                          {t('memberRoles.permissionCount', { count: role.permission_count })}
                        </Button>
                      ) : (
                        t('memberRoles.needsReview')
                      )}
                    </td>
                    <td>
                      {role.assignment_kind === 'intrinsic' ? (
                        <span className="text-sm text-muted-foreground">
                          {t('members.baseIdentity')}
                        </span>
                      ) : (
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={!editable || busy || !!intent || outdated}
                          aria-label={t('memberRoles.removeRole', { name: roleName(role) })}
                          onClick={() => stage(ids.filter((id) => id !== role.id))}
                        >
                          {t('memberRoles.remove')}
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
            {editable && !intent && (
              <div className="space-y-3 border-t p-4">
                <FormField label={t('memberRoles.addRoles')}>
                  <div className="flex items-start gap-3">
                    <div className="min-w-0 flex-1">
                      <MultiSelect
                        label={t('memberRoles.search')}
                        options={candidateRows
                          .filter((r) => !ids.includes(r.id) || !known.has(r.id))
                          .map((r) => ({ value: r.id, label: roleName(r) }))}
                        value={
                          candidateCurrent()
                            ? selectedRows.map((r) => ({ value: r.id, label: roleName(r) }))
                            : []
                        }
                        search={search}
                        disabled={busy || outdated || !current(true)}
                        onSearchChange={(next) => {
                          if (current(true) && !busy && !intent) {
                            latestCandidateScope.current = ''
                            setSearch(next)
                          }
                        }}
                        removeLabel={(name) => t('memberRoles.removeSelection', { name })}
                        onValueChange={(next) => {
                          if (candidateCanAct() && !busy && !intent) {
                            const selected = new Map(
                              [...selectedRows, ...candidateRows].map((r) => [r.id, r]),
                            )
                            setChosen({
                              etag: page!.etag,
                              rows: next.flatMap((r) =>
                                selected.get(r.value) ? [selected.get(r.value)!] : [],
                              ),
                            })
                          }
                        }}
                        footer={
                          <>
                            {candidates.hasNextPage && (
                              <Button
                                variant="ghost"
                                disabled={busy || candidates.isFetching}
                                onClick={() => {
                                  if (candidateCanAct())
                                    void candidates.fetchNextPage({ cancelRefetch: false })
                                }}
                              >
                                {t('memberRoles.more')}
                              </Button>
                            )}
                            {!candidateRows.length && candidateCurrent() && (
                              <p className="p-2 text-sm">{t('memberRoles.noCandidates')}</p>
                            )}
                          </>
                        }
                      />
                    </div>
                    <Button
                      variant="outline"
                      disabled={
                        busy ||
                        outdated ||
                        !candidateCurrent() ||
                        !selectedRows.length ||
                        new Set([...ids, ...selectedRows.map((r) => r.id)]).size > 100
                      }
                      onClick={() => {
                        if (candidateCanAct()) {
                          stage([...ids, ...selectedRows.map((r) => r.id)], selectedRows)
                          setChosen(null)
                        }
                      }}
                    >
                      {t('memberRoles.add')}
                    </Button>
                  </div>
                </FormField>
                {!validMemberRoleSearch(search) && (
                  <p role="alert">{t('memberRoles.invalidSearch')}</p>
                )}
                {validMemberRoleSearch(search) && (candidates.isError || candidates.isFetching) && (
                  <p role={candidates.isError ? 'alert' : 'status'}>
                    {t(
                      candidates.isError
                        ? 'memberRoles.candidatesFailed'
                        : 'memberRoles.candidatesLoading',
                    )}
                  </p>
                )}
                {validMemberRoleSearch(search) && candidates.isError && (
                  <Button
                    variant="outline"
                    onClick={() => {
                      if (current(true)) void candidates.refetch()
                    }}
                  >
                    {t('memberRoles.refresh')}
                  </Button>
                )}
              </div>
            )}
            <div className="space-y-3 p-4">
              {page!.edit_blockers.map((blocker) => (
                <p key={blocker} role="status">
                  {t(`memberRoles.block_${blocker}`)}
                </p>
              ))}
              {ids.length > 100 && <p role="status">{t('memberRoles.replacementLimit')}</p>}
              {(outdated || missing) && (
                <p role="alert">
                  {t(outdated ? 'memberRoles.staleDraft' : 'memberRoles.needsReviewHelp')}
                </p>
              )}
              {editable && draft && !intent && (outdated || missing) && (
                <Button variant="outline" disabled={busy} onClick={() => void reviewCurrent()}>
                  {t('memberRoles.review')}
                </Button>
              )}
              {notice &&
                (notice === 'confirmed' || notice === 'reconciled'
                  ? confirmed === page!.etag
                  : true) && (
                  <p
                    role={notice === 'invalidReason' || notice === 'conflict' ? 'alert' : 'status'}
                  >
                    {t(`memberRoles.${notice}`)}
                  </p>
                )}
              {editable && (changed || intent) && (
                <div className="flex flex-wrap justify-end gap-2">
                  {intent && (
                    <Button
                      variant="outline"
                      disabled={busy}
                      onClick={() => {
                        if (current(true) && !busy) {
                          setIntent(null)
                          setConfirming(false)
                          setNotice(null)
                        }
                      }}
                    >
                      {t('memberRoles.abandon')}
                    </Button>
                  )}
                  <Button
                    disabled={busy || (!intent && (outdated || missing || ids.length > 100))}
                    onClick={() => {
                      if (current(true)) {
                        setConfirming(true)
                        if (!intent) setNotice(null)
                      }
                    }}
                  >
                    {t(intent ? 'memberRoles.resume' : 'memberRoles.save')}
                  </Button>
                </div>
              )}
            </div>
          </section>
          <section className="rounded-lg border">
            <h3 className="border-b p-4 font-medium">{t('members.effectivePermissions')}</h3>
            {page!.permission_use === 'inactive' && (
              <p role="status" className="p-4 text-sm text-muted-foreground">
                {t('memberRoles.inactive')}
              </p>
            )}
            {page!.effective_permissions.length ? (
              <PermissionRows permissions={page!.effective_permissions} />
            ) : (
              <p className="p-4 text-sm text-muted-foreground">{t('memberRoles.noPermissions')}</p>
            )}
          </section>
        </>
      )}
      <Dialog
        open={dialogAllowed}
        onOpenChange={(open) => {
          if (!open) setViewing(null)
        }}
        title={t('memberRoles.permissionTitle')}
        description={t('memberRoles.permissionDescription', {
          name: dialogAllowed ? roleName(viewing!.role) : '',
        })}
      >
        <p className="mb-3 text-sm text-muted-foreground">{t('memberRoles.recordedPermissions')}</p>
        {detailFresh ? (
          detail.data!.permissions.length ? (
            <PermissionRows permissions={detail.data!.permissions} />
          ) : (
            <p>{t('memberRoles.noPermissions')}</p>
          )
        ) : (
          <p role={detail.isError ? 'alert' : 'status'}>
            {t(detail.isError ? 'memberRoles.definitionFailed' : 'memberRoles.loading')}
          </p>
        )}
      </Dialog>
      <Dialog
        open={confirming && editable}
        busy={busy}
        onOpenChange={(open) => {
          if (!open) setConfirming(false)
        }}
        title={t('memberRoles.confirmTitle')}
        description={t('memberRoles.confirmHelp')}
      >
        <form
          className="space-y-4"
          aria-label={t('memberRoles.confirmTitle')}
          onSubmit={(event) => {
            event.preventDefault()
            void dispatch()
          }}
        >
          {intent && <p role="status">{t('memberRoles.uncertain')}</p>}
          <FormField label={t('memberRoles.reason')}>
            <Textarea
              aria-label={t('memberRoles.reason')}
              value={reason}
              disabled={busy || !!intent}
              onChange={(event) => setReason(event.target.value)}
            />
          </FormField>
          {notice &&
            (notice === 'invalidReason' || notice === 'uncertain' || notice === 'conflict') && (
              <p role="alert">{t(`memberRoles.${notice}`)}</p>
            )}
          <Button type="submit" disabled={busy}>
            {t(intent ? 'memberRoles.retry' : 'memberRoles.confirm')}
          </Button>
        </form>
      </Dialog>
    </div>
  )
}
