import { useCallback, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getLimits, saveLimits } from '@/api/resource-limits'
import { LimitEditor, LimitSummary } from '@/views/resource-limits'
import { QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import type { Session } from '@/types/auth'
import type { ResourceRecord } from '@/types/resources'
import type { LimitInput, LimitRecord } from '@/types/resource-limits'

export type ProjectLimitsScope = {
  actor: string
  target: string
  generation: number
  ready: boolean
  targetQueryKey: readonly unknown[]
}

export default function ProjectLimits(props: ProjectLimitsScope) {
  return <Limits key={`${props.actor}:${props.target}`} {...props} />
}

function Limits({ actor, target, generation, ready, targetQueryKey }: ProjectLimitsScope) {
  const { t } = useTranslation('limits')
  const cache = useQueryClient()
  const path = `/projects/${target}`
  const keys = JSON.stringify([['auth', 'session'], ['permissions', actor], targetQueryKey])
  const snapshot = useCallback(
    () =>
      JSON.parse(keys)
        .map((key: unknown[]) => {
          const state = cache.getQueryState(key)
          return `${state?.status}:${state?.fetchStatus}:${state?.dataUpdateCount}:${state?.errorUpdateCount}:${state?.isInvalidated}`
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
  const permissionKey = ['permissions', actor]
  function authority(write = false) {
    const session = cache.getQueryState<Session>(['auth', 'session'])
    const permissions = cache.getQueryState<string[]>(permissionKey)
    const project = cache.getQueryState<ResourceRecord>(targetQueryKey)
    return (
      ready &&
      !!actor &&
      [session, permissions, project].every(
        (state) =>
          state?.status === 'success' &&
          state.fetchStatus === 'idle' &&
          !state.error &&
          !state.isInvalidated,
      ) &&
      session?.data?.user.id === actor &&
      !!session.data.csrf_token &&
      project?.data?.id === target &&
      (permissions?.data?.includes('projects.read_all') === true ||
        permissions?.data?.includes('projects.limits.write') === true ||
        project?.data?.managers?.some((manager) => manager.user_id === actor) === true) &&
      (!write ||
        (permissions?.data?.includes('projects.limits.write') === true &&
          project?.data?.status === 'active'))
    )
  }
  const queryKey = ['resource-limits', actor, path, generation, version]
  const query = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      const data = await getLimits(path, signal)
      if (data.kind !== 'project' || data.id !== target)
        throw new Error('Project policy unavailable')
      return data
    },
    enabled: authority(),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const fresh = authority() && query.isSuccess && !query.isFetching
  const writable = fresh && authority(true)
  const [editing, setEditing] = useState<LimitRecord | null>(null)
  const [notice, setNotice] = useState<{ authority: string } | null>(null)
  const controller = useRef<AbortController | null>(null)
  const alive = useRef(true)
  const operation = useRef(0)
  useLayoutEffect(() => {
    if (!writable) {
      operation.current++
      controller.current?.abort()
    }
  }, [writable, version, generation])
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      controller.current?.abort()
    }
  }, [])
  function canDispatch() {
    const state = cache.getQueryState<LimitRecord>(queryKey)
    return (
      alive.current &&
      snapshot() === version &&
      authority(true) &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.error &&
      !state.isInvalidated &&
      state.data?.id === target
    )
  }
  async function savePolicy(etag: string, input: LimitInput) {
    if (!canDispatch()) throw new Error('Project policy authority unavailable')
    const serial = ++operation.current
    const pending = new AbortController()
    controller.current = pending
    const csrf = cache.getQueryData<Session>(['auth', 'session'])!.csrf_token
    const result = await saveLimits(path, etag, input, csrf, pending.signal)
    const decimal = (value: string | null | undefined) =>
      value?.includes('.') ? value.replace(/0+$/, '').replace(/\.$/, '') : value
    if (
      !canDispatch() ||
      serial !== operation.current ||
      pending.signal.aborted ||
      result.kind !== 'project' ||
      result.id !== target ||
      !result.enforced ||
      Object.entries(input).some(
        ([field, value]) =>
          field !== 'reason' &&
          (field === 'money_month'
            ? decimal(result.stored.money_month) !== decimal(value as string | null)
            : field === 'ip_ranges'
              ? JSON.stringify(result.stored.ip_ranges) !== JSON.stringify(value)
              : result.stored[field as keyof typeof result.stored] !== value),
      )
    )
      throw new Error('Project policy application unconfirmed')
    return result
  }
  return (
    <section className="rounded-lg border" aria-label={t('title')}>
      {fresh && (
        <header className="flex items-center justify-between gap-4 border-b p-4">
          <h3 className="font-medium">{t('title')}</h3>
          {writable && !editing && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                if (canDispatch()) {
                  setNotice(null)
                  setEditing(query.data!)
                }
              }}
            >
              {t('edit')}
            </Button>
          )}
        </header>
      )}
      <div className="space-y-4 p-4">
        {authority() && (
          <QueryState
            pending={query.isPending || query.isFetching}
            error={query.error}
            retry={() => {
              if (authority()) void query.refetch()
            }}
          />
        )}
        {fresh && <LimitSummary record={query.data!} />}
        {fresh && notice?.authority === `${generation}:${version}` && (
          <p role="status">{t('applied')}</p>
        )}
        {editing && (
          <LimitEditor
            parentManagedSession
            path={path}
            canEdit={writable}
            current={fresh ? query.data! : editing}
            visible={writable}
            canDispatch={canDispatch}
            savePolicy={savePolicy}
            pending={() => {}}
            reload={async () => {
              if (!canDispatch()) throw new Error('Project policy authority unavailable')
              const result = await query.refetch()
              if (
                !alive.current ||
                snapshot() !== version ||
                !authority(true) ||
                result.error ||
                !result.data
              )
                throw new Error('Project policy authority unavailable')
              return result.data
            }}
            close={() => setEditing(null)}
            saved={(data) => {
              if (!canDispatch()) return
              cache.setQueryData(queryKey, data)
              setEditing(null)
              setNotice({ authority: `${generation}:${version}` })
            }}
          />
        )}
      </div>
    </section>
  )
}
