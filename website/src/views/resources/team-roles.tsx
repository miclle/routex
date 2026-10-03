import { useLayoutEffect, useRef, useState } from 'react'
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { isAxiosError } from 'axios'
import { getTeamRoleCandidates, saveTeamRoles } from '@/api/team-roles'
import { sessionKey, useSession } from '@/hooks/use-auth'
import type { useTeamAccess } from '@/hooks/use-team-access'
import type { Session } from '@/types/auth'
import type { TeamRole } from '@/types/team-roles'
import { QueryState, FormField } from '@/components/app/CatalogUI'
import { Table } from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { Dialog } from '@/components/ui/dialog'
import { ResourceSection } from './shared'
export default function TeamRolesPanel({
  team,
  access,
}: {
  team: string
  access: ReturnType<typeof useTeamAccess>
}) {
  const { t } = useTranslation('resources')
  const session = useSession(),
    cache = useQueryClient()
  const actor = access.actor,
    current = access.current
  const editable = !!current?.can_assign_roles && session.data?.user.role === 'admin'
  const [knownRoles, setKnownRoles] = useState<{ etag: string; roles: TeamRole[] } | null>(null)
  const [draft, setDraft] = useState<string[] | null>(null),
    [selected, setSelected] = useState<string[]>([]),
    [q, setQ] = useState(''),
    [reason, setReason] = useState(''),
    [discardReview, setDiscardReview] = useState<string | null>(null),
    [reviewed, setReviewed] = useState<string | null>(null),
    [viewing, setViewing] = useState<string | null>(null)
  const [busy, setBusy] = useState(false),
    [uncertain, setUncertain] = useState(false),
    [issue, setIssue] = useState<string | null>(null)
  const intent = useRef<{ actor: string; ids: string[]; reason: string; etag: string } | null>(
      null,
    ),
    lock = useRef(false),
    mounted = useRef(false),
    visible = useRef(false)
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  useLayoutEffect(() => {
    visible.current = access.fresh && editable
  })
  const active = () =>
    mounted.current &&
    cache.getQueryData<Session>(sessionKey)?.user.id === actor &&
    cache.getQueryState(sessionKey)?.status !== 'error'
  const candidates = useInfiniteQuery({
    queryKey: ['team-role-candidates', actor, team, q, current?.etag],
    queryFn: ({ signal, pageParam }) => getTeamRoleCandidates(team, q, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page, pages) =>
      page.next_cursor && !pages.slice(0, -1).some((old) => old.next_cursor === page.next_cursor)
        ? page.next_cursor
        : undefined,
    enabled: editable,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const freshCandidates =
    editable &&
    candidates.isSuccess &&
    !candidates.isFetching &&
    !candidates.isError &&
    candidates.data.pages.every((page) => page.etag === current?.etag)
  const options = freshCandidates
    ? [
        ...new Map(
          candidates.data.pages.flatMap((page) => page.items).map((role) => [role.id, role]),
        ).values(),
      ]
    : []
  const ids = draft ?? current?.role_ids ?? []
  const roleMap = new Map(
    [
      ...(current?.roles ?? []),
      ...options,
      ...(knownRoles?.etag === current?.etag ? (knownRoles?.roles ?? []) : []),
    ].map((role) => [role.id, role]),
  )
  const viewingRole = access.fresh && viewing ? roleMap.get(viewing) : undefined
  const changed =
    !!current &&
    (ids.length !== current.role_ids.length || ids.some((id) => !current.role_ids.includes(id)))
  const stale = !!draft && (!current || current.etag !== reviewed || issue === 'teamRoleConflict')
  async function refresh() {
    await access.refetch()
    if (!active()) return
    await cache.invalidateQueries({ queryKey: ['team-role-candidates', actor, team] })
  }
  async function reviewCurrentAssignment() {
    if (lock.current || !active() || !editable || !access.fresh || !uncertain) return
    lock.current = true
    setBusy(true)
    setDiscardReview(null)
    try {
      const result = await access.refetch()
      if (
        !active() ||
        !result.isSuccess ||
        result.isFetching ||
        !result.data?.can_assign_roles ||
        cache.getQueryData<Session>(sessionKey)?.user.role !== 'admin'
      )
        return
      setDiscardReview(result.data.etag)
    } finally {
      lock.current = false
      if (active()) setBusy(false)
    }
  }
  function change(next: string[]) {
    if (!editable || busy || uncertain) return
    setDraft(next)
    setReviewed((previous) => previous ?? current!.etag)
    setIssue(null)
    intent.current = null
  }
  async function submit(retry = false) {
    if (
      lock.current ||
      !active() ||
      !editable ||
      !access.fresh ||
      (!retry && (!changed || stale || uncertain))
    )
      return
    if (!retry) {
      const explanation = reason.trim()
      if (
        !explanation ||
        new TextEncoder().encode(explanation).length > 1024 ||
        [...explanation].some((character) => {
          const code = character.codePointAt(0)!
          return code < 32 || code === 127
        })
      ) {
        setIssue('teamRoleReasonRequired')
        return
      }
      intent.current = { actor, ids: [...ids], reason: explanation, etag: reviewed! }
    }
    const original = intent.current,
      latest = cache.getQueryData<Session>(sessionKey)
    if (!original || latest?.user.id !== original.actor) return
    lock.current = true
    setBusy(true)
    setIssue(null)
    try {
      const saved = await saveTeamRoles(
        team,
        original.ids,
        original.reason,
        original.etag,
        latest.csrf_token,
      )
      if (!active()) return
      if (!visible.current) {
        setUncertain(true)
        setIssue('teamRoleUncertain')
        return
      }
      cache.setQueryData(['team-roles', actor, team], saved)
      setDraft(null)
      setReviewed(null)
      setSelected([])
      setReason('')
      setUncertain(false)
      setIssue('teamRoleSaved')
    } catch (error) {
      if (!active()) return
      const status = isAxiosError(error) ? error.response?.status : undefined
      const unknown = !status || status >= 500
      setUncertain((previous) => previous || unknown)
      setIssue(
        unknown || uncertain
          ? 'teamRoleUncertain'
          : status === 409
            ? 'teamRoleConflict'
            : 'teamRoleFailed',
      )
    } finally {
      lock.current = false
      if (active()) setBusy(false)
    }
  }
  return (
    <div className="space-y-4">
      <QueryState pending={access.isFetching} error={access.error} retry={() => void refresh()} />
      {current && (
        <>
          <p className="rounded-lg border bg-muted/30 p-4 text-sm">{t('teamRolesHelp')}</p>
          <ResourceSection title={t('teamRoles')} padded={false}>
            <Table aria-label={t('teamRoles')}>
              <thead>
                <tr>
                  <th>{t('teamRoleName')}</th>
                  <th>{t('teamRoleType')}</th>
                  <th>{t('teamRolePermissions')}</th>
                  {editable && <th>{t('actions')}</th>}
                </tr>
              </thead>
              <tbody>
                {ids.map((id) => {
                  const role = roleMap.get(id)
                  return (
                    <tr key={id}>
                      <td>{role?.name ?? id}</td>
                      <td>
                        {role ? (
                          <Badge variant="outline">
                            {t(role.builtin ? 'teamRoleBuiltin' : 'teamRoleCustom')}
                          </Badge>
                        ) : (
                          t('teamRoleUnavailable')
                        )}
                      </td>
                      <td>
                        {role && (
                          <Button
                            variant="ghost"
                            className="text-primary"
                            onClick={() => setViewing(id)}
                          >
                            {t('teamRoleViewPermissions')}
                          </Button>
                        )}
                      </td>
                      {editable && (
                        <td>
                          <Button
                            variant="ghost"
                            disabled={busy || uncertain}
                            onClick={() => change(ids.filter((value) => value !== id))}
                          >
                            {t('remove')}
                          </Button>
                        </td>
                      )}
                    </tr>
                  )
                })}
              </tbody>
            </Table>
            {!ids.length && (
              <p className="p-4 text-sm text-muted-foreground">{t('teamRolesEmpty')}</p>
            )}
            {editable && (
              <div className="space-y-3 border-t p-4">
                <FormField label={t('teamRoleAdd')}>
                  <Input
                    aria-label={t('teamRoleSearch')}
                    maxLength={200}
                    value={q}
                    onValueChange={setQ}
                    disabled={busy || uncertain}
                  />
                </FormField>
                <QueryState
                  pending={candidates.isFetching}
                  error={candidates.error}
                  retry={() => void candidates.refetch()}
                />
                <fieldset
                  disabled={busy || uncertain || !freshCandidates}
                  className="max-h-48 space-y-2 overflow-auto rounded-md border p-3"
                >
                  <legend className="sr-only">{t('teamRoleAdd')}</legend>
                  {freshCandidates &&
                    options
                      .filter((role) => role.team_actions.length > 0 && !ids.includes(role.id))
                      .map((role) => (
                        <label className="flex items-center gap-2 text-sm" key={role.id}>
                          <input
                            type="checkbox"
                            checked={selected.includes(role.id)}
                            onChange={(event) =>
                              setSelected((previous) =>
                                event.target.checked
                                  ? [...previous, role.id]
                                  : previous.filter((id) => id !== role.id),
                              )
                            }
                          />
                          {role.name}
                        </label>
                      ))}
                </fieldset>
                {candidates.hasNextPage && (
                  <Button
                    variant="outline"
                    disabled={busy || uncertain || candidates.isFetching}
                    onClick={() => void candidates.fetchNextPage()}
                  >
                    {t('teamRoleMore')}
                  </Button>
                )}
                {candidates.isSuccess && !candidates.isFetching && !freshCandidates && (
                  <p role="alert">{t('teamRoleConflict')}</p>
                )}
                {selected.length > 0 && (
                  <p className="text-xs text-muted-foreground">
                    {t('teamRoleSelected', { count: selected.length })}
                  </p>
                )}
                <Button
                  variant="outline"
                  disabled={
                    busy ||
                    uncertain ||
                    !selected.length ||
                    !freshCandidates ||
                    new Set([...ids, ...selected]).size > 100
                  }
                  onClick={() => {
                    setKnownRoles({
                      etag: current.etag,
                      roles: [
                        ...(knownRoles?.etag === current.etag ? knownRoles.roles : []),
                        ...options.filter((role) => selected.includes(role.id)),
                      ],
                    })
                    change([...new Set([...ids, ...selected])])
                    setSelected([])
                  }}
                >
                  {t('add')}
                </Button>
                {draft && (
                  <>
                    <FormField label={t('teamRoleReason')}>
                      <Textarea
                        aria-label={t('teamRoleReason')}
                        value={reason}
                        disabled={busy || uncertain}
                        onChange={(event) => setReason(event.target.value)}
                      />
                    </FormField>
                    {stale && !uncertain && (
                      <Button
                        variant="outline"
                        disabled={busy || !freshCandidates || ids.some((id) => !roleMap.has(id))}
                        onClick={() => {
                          setReviewed(current.etag)
                          setIssue(null)
                          intent.current = null
                        }}
                      >
                        {t('teamRoleUseContext')}
                      </Button>
                    )}
                    <div className="flex flex-wrap gap-2">
                      <Button
                        disabled={busy || stale || uncertain || !changed}
                        onClick={() => void submit()}
                      >
                        {t('teamRoleSave')}
                      </Button>
                      {uncertain && (
                        <>
                          <Button
                            disabled={busy || !access.fresh}
                            onClick={() => void submit(true)}
                          >
                            {t('teamRoleRetry')}
                          </Button>
                          <Button
                            variant="outline"
                            disabled={busy || !access.fresh}
                            onClick={() => void reviewCurrentAssignment()}
                          >
                            {t('teamRoleReviewCurrent')}
                          </Button>
                        </>
                      )}
                    </div>
                  </>
                )}
              </div>
            )}
          </ResourceSection>
          <ResourceSection title={t('teamEffectiveRoles')}>
            <Table aria-label={t('teamEffectiveRoles')}>
              <thead>
                <tr>
                  <th>{t('team')}</th>
                  <th>{t('teamRolePermissions')}</th>
                </tr>
              </thead>
              <tbody>
                <tr>
                  <td>{t('team')}</td>
                  <td>
                    {current.effective_team_actions.length
                      ? current.effective_team_actions.map((action) => (
                          <Badge className="mr-2" variant="outline" key={action}>
                            {t(
                              action === 'teams.write' ? 'teamRoleEditTeam' : 'teamRoleEditModels',
                            )}
                          </Badge>
                        ))
                      : t('teamRolesEmpty')}
                  </td>
                </tr>
              </tbody>
            </Table>
          </ResourceSection>
          <Button
            variant="outline"
            disabled={busy || access.isFetching}
            onClick={() => void refresh()}
          >
            {t('teamRoleRefresh')}
          </Button>
          {issue && (
            <p role={issue === 'teamRoleSaved' ? 'status' : 'alert'}>
              {t(uncertain ? 'teamRoleUncertain' : issue)}
            </p>
          )}
        </>
      )}
      <Dialog
        open={!!discardReview && access.fresh && editable}
        onOpenChange={(open) => {
          if (!open) setDiscardReview(null)
        }}
        title={t('teamRoleReviewCurrent')}
        description={t('teamRoleDiscardHelp')}
      >
        {current && (
          <>
            <ul className="space-y-3">
              {current.roles.map((role) => (
                <li key={role.id}>
                  <p>
                    {role.name} <span className="text-xs text-muted-foreground">{role.id}</span>
                  </p>
                  <p className="text-sm text-muted-foreground">
                    {role.team_actions.length
                      ? role.team_actions
                          .map((action) =>
                            t(action === 'teams.write' ? 'teamRoleEditTeam' : 'teamRoleEditModels'),
                          )
                          .join(' · ')
                      : t('teamRolesEmpty')}
                  </p>
                </li>
              ))}
              {!current.roles.length && <li>{t('teamRolesEmpty')}</li>}
            </ul>
            <Button
              className="mt-6"
              disabled={busy || !uncertain || discardReview !== current.etag}
              onClick={() => {
                if (
                  busy ||
                  !active() ||
                  !visible.current ||
                  !uncertain ||
                  discardReview !== current.etag
                )
                  return
                intent.current = null
                setDraft(null)
                setReviewed(null)
                setSelected([])
                setKnownRoles(null)
                setReason('')
                setUncertain(false)
                setDiscardReview(null)
                setIssue('teamRoleDiscarded')
              }}
            >
              {t('teamRoleDiscard')}
            </Button>
          </>
        )}
      </Dialog>
      <Dialog
        description={t('teamRolesHelp')}
        open={!!viewingRole}
        title={viewingRole?.name ?? t('teamRolePermissions')}
        onOpenChange={(open) => {
          if (!open) setViewing(null)
        }}
      >
        {viewingRole && (
          <ul className="space-y-2">
            {viewingRole.team_actions.map((action) => (
              <li key={action}>
                {t(action === 'teams.write' ? 'teamRoleEditTeam' : 'teamRoleEditModels')}
              </li>
            ))}
          </ul>
        )}
      </Dialog>
    </div>
  )
}
