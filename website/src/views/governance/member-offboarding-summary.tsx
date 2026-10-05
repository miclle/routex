import { useCallback, useLayoutEffect, useMemo, useRef, useSyncExternalStore } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { useTranslation } from 'react-i18next'
import { getOffboarding } from '@/api/offboarding'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import type { Session } from '@/types/auth'
import type { Member } from '@/types/governance'
import type { OffboardingCase } from '@/types/offboarding'

type Props = {
  actor: string
  target: string
  generation: number
  ready: boolean
  targetQueryKey: readonly unknown[]
}
type RecordedCase = Pick<
  OffboardingCase,
  'id' | 'user_id' | 'mode' | 'status' | 'planned_at' | 'assignments'
>
const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const identity = (value: unknown): value is string =>
  typeof value === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(value)
function timestamp(value: unknown): value is string {
  if (
    typeof value !== 'string' ||
    !/^(?!0000)\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(value)
  )
    return false
  const local = Date.parse(value.slice(0, 19) + 'Z')
  return (
    Number.isFinite(Date.parse(value)) &&
    Number.isFinite(local) &&
    new Date(local).toISOString().slice(0, 19) === value.slice(0, 19) &&
    !/^0001-01-01T00:00:00(?:\.0{1,9})?Z$/.test(value)
  )
}
function recordedCase(value: unknown, target: string): value is RecordedCase {
  if (
    !object(value) ||
    !identity(value.id) ||
    value.user_id !== target ||
    !['planned', 'emergency'].includes(value.mode as string) ||
    !['ready_to_complete', 'completed'].includes(value.status as string) ||
    (value.status === 'ready_to_complete' && value.mode !== 'planned') ||
    !(value.planned_at === null || timestamp(value.planned_at)) ||
    !object(value.assignments)
  )
    return false
  const assignments = value.assignments
  return (
    Array.isArray(assignments.project_assignments) &&
    Array.isArray(assignments.team_assignments) &&
    assignments.project_assignments.every((row) => object(row) && identity(row.project_id)) &&
    assignments.team_assignments.every((row) => object(row) && identity(row.team_id)) &&
    new Set(assignments.project_assignments.map((row) => row.project_id)).size ===
      assignments.project_assignments.length &&
    new Set(assignments.team_assignments.map((row) => row.team_id)).size ===
      assignments.team_assignments.length
  )
}
function summary(data: unknown, target: string): RecordedCase | null {
  if (
    !object(data) ||
    data.user_id !== target ||
    !Array.isArray(data.cases) ||
    data.cases.length > 100 ||
    !data.cases.every((item) => recordedCase(item, target)) ||
    new Set(data.cases.map((item) => item.id)).size !== data.cases.length
  )
    throw new Error('Offboarding summary unavailable')
  return data.cases.find((item) => item.status === 'ready_to_complete') ?? data.cases[0] ?? null
}

export default function MemberOffboardingSummary(props: Props) {
  return <Summary key={`${props.actor}:${props.target}`} {...props} />
}
function Summary({ actor, target, generation, ready, targetQueryKey }: Props) {
  const { t, i18n } = useTranslation('governance'),
    cache = useQueryClient(),
    navigate = useNavigate()
  const mounted = useRef(false)
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const keys = JSON.stringify([['auth', 'session'], ['permissions', actor], targetQueryKey])
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
  const version = useSyncExternalStore(subscribe, snapshot, snapshot)
  function authority() {
    const auth = cache.getQueryState<Session>(['auth', 'session']),
      grants = cache.getQueryState<string[]>(['permissions', actor]),
      member = cache.getQueryState<Member>(targetQueryKey)
    return (
      ready &&
      !!actor &&
      identity(target) &&
      snapshot() === version &&
      [auth, grants, member].every(
        (state) =>
          state?.status === 'success' &&
          state.fetchStatus === 'idle' &&
          !state.isInvalidated &&
          !state.error,
      ) &&
      auth?.data?.user.id === actor &&
      member?.data?.id === target &&
      grants?.data?.includes('members.read') === true
    )
  }
  const queryKey = useMemo(
    () => ['admin', 'member-offboarding-summary', actor, target, generation, version],
    [actor, target, generation, version],
  )
  const ownKey = JSON.stringify([
    'admin',
    'member-offboarding-summary',
    actor,
    target,
    generation,
    version,
  ])
  const query = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      if (!authority()) throw new Error('Offboarding summary unavailable')
      const data = await getOffboarding(target, signal)
      if (signal.aborted || !authority()) throw new Error('Offboarding summary unavailable')
      return summary(data, target)
    },
    enabled: authority(),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const readSnapshot = useCallback(() => {
    const state = cache.getQueryState(JSON.parse(ownKey))
    return `${state?.status}:${state?.fetchStatus}:${state?.isInvalidated}:${state?.dataUpdateCount}:${state?.errorUpdateCount}`
  }, [cache, ownKey])
  const readSubscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (JSON.stringify(event.query.queryKey) === ownKey) notify()
      }),
    [cache, ownKey],
  )
  const readVersion = useSyncExternalStore(readSubscribe, readSnapshot, readSnapshot)
  function fresh() {
    const state = cache.getQueryState<RecordedCase | null>(queryKey)
    return (
      authority() &&
      readSnapshot() === readVersion &&
      query.isSuccess &&
      !query.isFetching &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      state.data === query.data
    )
  }
  function current() {
    return mounted.current && fresh()
  }
  const record = fresh() ? query.data : undefined
  return (
    <section className="rounded-lg border p-4" aria-label={t('members.offboarding')}>
      <h3 className="mb-3 font-medium">{t('members.offboarding')}</h3>
      <p className="mb-4 text-sm text-muted-foreground">{t('members.offboardingHelp')}</p>
      {!authority() ? (
        <p role="status" className="text-sm text-muted-foreground">
          {t('memberOffboardingSummary.wait')}
        </p>
      ) : query.isError ? (
        <div className="space-y-3">
          <p role="alert" className="text-sm">
            {t('memberOffboardingSummary.failed')}
          </p>
          <Button
            variant="outline"
            onClick={() => {
              if (authority()) void query.refetch()
            }}
          >
            {t('memberOffboardingSummary.retry')}
          </Button>
        </div>
      ) : record === undefined ? (
        <p role="status" className="text-sm text-muted-foreground">
          {t('memberOffboardingSummary.loading')}
        </p>
      ) : (
        <>
          {record === null ? (
            <p className="mb-4 text-sm">{t('memberOffboardingSummary.empty')}</p>
          ) : (
            <>
              <p className="mb-4 text-sm text-muted-foreground">
                {t('memberOffboardingSummary.history')}
              </p>
              <dl className="mb-4 grid gap-4 text-sm sm:grid-cols-3">
                <div>
                  <dt className="text-muted-foreground">
                    {t('memberOffboardingSummary.handover')}
                  </dt>
                  <dd className="mt-1">
                    <Badge variant="outline">
                      {t(`memberOffboardingSummary.${record.status}`)}
                    </Badge>{' '}
                    <span>{t(`memberOffboardingSummary.${record.mode}`)}</span>
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">
                    {t('memberOffboardingSummary.assignments')}
                  </dt>
                  <dd className="mt-1">
                    {t('memberOffboardingSummary.assignmentCounts', {
                      projects: record.assignments.project_assignments.length,
                      teams: record.assignments.team_assignments.length,
                    })}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">
                    {t('memberOffboardingSummary.intended')}
                  </dt>
                  <dd className="mt-1">
                    {record.planned_at === null
                      ? t('memberOffboardingSummary.notSet')
                      : new Date(record.planned_at).toLocaleString(i18n.language)}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{t('memberOffboardingSummary.keys')}</dt>
                  <dd className="mt-1">{t('memberOffboardingSummary.notRecorded')}</dd>
                </div>
              </dl>
            </>
          )}
          <Button
            variant="outline"
            onClick={() => {
              if (current()) navigate(`/admin/members/${encodeURIComponent(target)}/offboarding`)
            }}
          >
            {t('members.reviewOffboarding')}
          </Button>
        </>
      )}
    </section>
  )
}
