import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient, type QueryKey } from '@tanstack/react-query'
import { Link, useParams, useSearchParams } from 'react-router'
import { useTranslation } from 'react-i18next'
import { KeyRound } from 'lucide-react'
import { getPermissions } from '@/api/governance'
import { applySecretIntent, getSecretStore, SecretError, validSecretReason } from '@/api/secrets'
import { useSession, sessionKey } from '@/hooks/use-auth'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import type { Session } from '@/types/auth'
import type { SecretAction, SecretIntent, SecretResult, SecretStore } from '@/types/secrets'
import { Page } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { VaultWorkspace } from './vault'
import ProviderStoragePolicyEditor from './provider-storage'

const knownBlockers = new Set([
  'key_unavailable',
  'ciphertext_invalid',
  'policy_changed',
  'publication_pending',
  'process_unverified',
  'additional_process',
  'lease_lost',
  'dependency_remaining',
  'observation_pending',
  'inventory_scope_changed',
])
const terminal = new Set(['completed', 'rolled_back'])
const sameKey = (left: QueryKey, right: QueryKey) => JSON.stringify(left) === JSON.stringify(right)

export default function SecretStorePage() {
  const session = useSession()
  const generation = useSessionGeneration()
  const { rotationId } = useParams()
  const [params, setParams] = useSearchParams()
  const { t } = useTranslation('secrets')
  const tab = !rotationId && params.get('tab') === 'vault' ? 'vault' : 'storage'
  const actor = session.data?.user.id ?? ''
  const fresh =
    session.isSuccess &&
    !session.isFetching &&
    !session.error &&
    /^[a-f0-9]{64}$/.test(session.data?.csrf_token ?? '')
  const workspace =
    tab === 'vault' ? (
      <VaultWorkspace
        key={actor}
        actor={actor}
        admin={session.data?.user.role === 'admin'}
        fresh={fresh}
        generation={generation}
      />
    ) : (
      <SecretWorkspace
        key={`${actor}:${rotationId ?? ''}`}
        actor={actor}
        admin={session.data?.user.role === 'admin'}
        fresh={fresh}
        generation={generation}
        rotationId={rotationId}
      />
    )
  return (
    <>
      {!rotationId && (
        <Tabs
          value={tab}
          onValueChange={(value) => {
            const next = new URLSearchParams(params)
            next.set('tab', String(value))
            setParams(next)
          }}
        >
          <TabsList>
            <TabsTrigger value="storage">{t('tabs.storage')}</TabsTrigger>
            <TabsTrigger value="vault">{t('tabs.vault')}</TabsTrigger>
            <TabsTrigger value="api-key" disabled>
              {t('tabs.apiKey')}
            </TabsTrigger>
          </TabsList>
        </Tabs>
      )}
      {workspace}
    </>
  )
}

function SecretWorkspace({
  actor,
  admin,
  fresh,
  generation,
  rotationId,
}: {
  actor: string
  admin: boolean
  fresh: boolean
  generation: number
  rotationId?: string
}) {
  const { t, i18n } = useTranslation('secrets')
  const cache = useQueryClient()
  const permissionsKey = ['permissions', actor, 'secrets', generation] as const
  const viewKey = ['admin', 'secrets', actor, rotationId ?? '', generation] as const
  const permissions = useQuery({
    queryKey: permissionsKey,
    queryFn: ({ signal }) => getPermissions(signal),
    enabled: fresh && admin,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const canRead =
    fresh &&
    admin &&
    permissions.isSuccess &&
    !permissions.isFetching &&
    !permissions.error &&
    permissions.data.includes('secrets.read')
  const query = useQuery({
    queryKey: viewKey,
    queryFn: ({ signal }) => getSecretStore(rotationId, signal),
    enabled: canRead,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const visible = canRead && query.isSuccess && !query.isFetching && !query.error
  const view = visible ? query.data : undefined
  const canWrite = !!view?.can_rotate && permissions.data?.includes('secrets.rotate') === true
  const [open, setOpen] = useState(false)
  const [reviewed, setReviewed] = useState<SecretStore>()
  const [target, setTarget] = useState('')
  const [reason, setReason] = useState('')
  const [intent, setIntent] = useState<SecretIntent>()
  const [uncertain, setUncertain] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [result, setResult] = useState<SecretResult>()
  const lock = useRef(false)
  const controller = useRef<AbortController | null>(null)
  const boundary = useRef(0)
  const sessionBoundary = useRef(0)
  const mounted = useRef(true)
  const current = useRef({ actor, generation, permissionsKey, viewKey })
  useLayoutEffect(() => {
    current.current = { actor, generation, permissionsKey, viewKey }
  })
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      controller.current?.abort()
    }
  }, [])
  useLayoutEffect(
    () =>
      cache.getQueryCache().subscribe((event) => {
        if (event.type !== 'updated') return
        const key = event.query.queryKey
        const sessionEvent = sameKey(key, sessionKey)
        const relevant =
          sessionEvent ||
          sameKey(key, current.current.permissionsKey) ||
          sameKey(key, current.current.viewKey)
        if (!relevant) return
        // Manual same-actor CSRF replacement is current mutation authority, not renewal.
        if (sessionEvent && event.action.type === 'success' && event.action.manual) {
          const next = event.query.state.data as Session | undefined
          if (
            next?.user.id === actor &&
            next.user.role === 'admin' &&
            /^[a-f0-9]{64}$/.test(next.csrf_token)
          )
            return
        }
        if (
          event.action.type === 'fetch' ||
          event.action.type === 'error' ||
          event.action.type === 'success'
        ) {
          boundary.current++
          if (sessionEvent) sessionBoundary.current++
          if (controller.current) {
            controller.current.abort()
            setUncertain(true)
            setNotice('unknown')
          }
          setConfirm(false)
        }
      }),
    [cache, actor],
  )

  function authority() {
    const session = cache.getQueryState<Session>(sessionKey)
    const permissions = cache.getQueryState<string[]>(current.current.permissionsKey)
    const status = cache.getQueryState<SecretStore>(current.current.viewKey)
    if (
      session?.status !== 'success' ||
      session.fetchStatus !== 'idle' ||
      session.data?.user.id !== actor ||
      session.data.user.role !== 'admin' ||
      !/^[a-f0-9]{64}$/.test(session.data.csrf_token) ||
      permissions?.status !== 'success' ||
      permissions.fetchStatus !== 'idle' ||
      !permissions.data?.includes('secrets.read') ||
      !permissions.data.includes('secrets.rotate') ||
      status?.status !== 'success' ||
      status.fetchStatus !== 'idle' ||
      !status.data?.can_rotate
    )
      return undefined
    return { csrf: session.data.csrf_token, view: status.data }
  }
  async function review() {
    if (!view || busy) return
    const capturedKey = viewKey
    const capturedSession = sessionBoundary.current
    const checked = await query.refetch()
    const currentSession = cache.getQueryState<Session>(sessionKey)
    const currentPermissions = cache.getQueryState<string[]>(current.current.permissionsKey)
    if (
      !mounted.current ||
      !sameKey(capturedKey, current.current.viewKey) ||
      capturedSession !== sessionBoundary.current ||
      !checked.isSuccess ||
      checked.isFetching ||
      checked.error ||
      !checked.data ||
      currentSession?.status !== 'success' ||
      currentSession.fetchStatus !== 'idle' ||
      currentSession.data?.user.id !== actor ||
      currentSession.data.user.role !== 'admin' ||
      !/^[a-f0-9]{64}$/.test(currentSession.data.csrf_token) ||
      currentPermissions?.status !== 'success' ||
      currentPermissions.fetchStatus !== 'idle' ||
      !currentPermissions.data?.includes('secrets.read')
    )
      return
    setReviewed(checked.data)
    if (!uncertain) {
      setIntent(undefined)
      setConfirm(false)
      setNotice('')
    }
  }
  function prepare(action: SecretAction) {
    const fresh = authority()
    if (
      !fresh ||
      uncertain ||
      lock.current ||
      !reviewed ||
      reviewed.review_etag !== fresh.view.review_etag
    ) {
      setNotice('reviewRequired')
      return
    }
    if (!validSecretReason(reason)) {
      setNotice('reasonInvalid')
      return
    }
    if (action === 'start') {
      if (
        !!rotationId ||
        (fresh.view.rotation && !terminal.has(fresh.view.rotation.status)) ||
        !fresh.view.keys.some(
          (key) =>
            key.id === target &&
            key.configured &&
            key.verified &&
            key.state !== 'retired' &&
            key.id !== fresh.view.policy.write_key_id,
        )
      )
        return
    } else if (!fresh.view.rotation?.allowed_actions.includes(action)) return
    setIntent({
      action,
      rotationId: action === 'start' ? undefined : fresh.view.rotation!.id,
      etag: reviewed.review_etag,
      input: {
        request_id: crypto.randomUUID(),
        reason,
        ...(action === 'start' ? { target_key_id: target } : {}),
      },
    })
    setConfirm(true)
  }
  async function submit(retry = false) {
    const fresh = authority()
    if (!intent || !fresh || lock.current) return
    if (
      !retry &&
      (intent.etag !== fresh.view.review_etag ||
        (intent.action !== 'start' &&
          !fresh.view.rotation?.allowed_actions.includes(intent.action)))
    ) {
      setConfirm(false)
      setNotice('conflict')
      return
    }
    lock.current = true
    setBusy(true)
    setConfirm(false)
    setNotice('')
    const abort = new AbortController()
    controller.current = abort
    const captured = boundary.current
    try {
      const receipt = await applySecretIntent(intent, fresh.csrf, abort.signal)
      if (!mounted.current || boundary.current !== captured || abort.signal.aborted || !authority())
        return
      setResult(receipt)
      setIntent(undefined)
      setUncertain(false)
      setReviewed(undefined)
      setNotice('')
      controller.current = null
      // A fresh read is separate from a committed historical receipt.
      void query.refetch()
    } catch (error) {
      if (!mounted.current) return
      const status = error instanceof SecretError ? error.status : 0
      if (
        retry ||
        abort.signal.aborted ||
        boundary.current !== captured ||
        ![400, 401, 403, 404, 409].includes(status)
      ) {
        setUncertain(true)
        setNotice(retry && status ? 'retryRejected' : 'unknown')
      } else {
        setIntent(undefined)
        setReviewed(undefined)
        setNotice(status === 409 ? 'conflict' : 'rejected')
      }
    } finally {
      if (controller.current === abort) controller.current = null
      lock.current = false
      if (mounted.current) setBusy(false)
    }
  }
  const formatDate = (value: string | null) =>
    value
      ? new Date(value).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
      : t('unavailable')
  const targets =
    view?.keys.filter(
      (key) =>
        key.configured &&
        key.verified &&
        key.state !== 'retired' &&
        key.id !== view.policy.write_key_id,
    ) ?? []
  const reviewedCurrent = !!view && reviewed?.review_etag === view.review_etag
  const canStart = !rotationId && (!view?.rotation || terminal.has(view.rotation.status))
  return (
    <Page title={t('title')} description={t('description')}>
      <div>
        <h1 className="text-xl font-semibold">{t('title')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('description')}</p>
      </div>
      {!fresh || permissions.isFetching || query.isFetching ? (
        <p role="status">{t('loading')}</p>
      ) : !admin || (permissions.isSuccess && !canRead) ? (
        <p role="alert">{t('denied')}</p>
      ) : permissions.error || query.error ? (
        <p role="alert">{t('loadError')}</p>
      ) : null}
      <Button
        variant="outline"
        disabled={!fresh || !admin || busy || permissions.isFetching || query.isFetching}
        onClick={() => {
          if (canRead) void query.refetch()
          else void permissions.refetch()
        }}
      >
        {t('refresh')}
      </Button>
      {!rotationId && (
        <ProviderStoragePolicyEditor
          actor={actor}
          generation={generation}
          permissionKey={permissionsKey}
        />
      )}
      {view && (
        <section className="rounded-xl border bg-card p-6">
          <header className="mb-5 flex items-center justify-between gap-4">
            <h2 className="flex items-center gap-2 font-semibold">
              <KeyRound className="size-4" aria-hidden="true" />
              {t('internal')}
            </h2>
            <Button
              variant="outline"
              onClick={() => {
                setOpen(true)
                if (!uncertain) setReviewed(view)
              }}
            >
              {t('rotate')}
            </Button>
          </header>
          <dl className="grid gap-4 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-muted-foreground">{t('writeKey')}</dt>
              <dd>{view.policy.write_key_id ?? t('unavailable')}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('epoch')}</dt>
              <dd>{view.policy.epoch}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('process')}</dt>
              <dd>{view.process.id ?? t('unavailable')}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('verification')}</dt>
              <dd>{t(view.process.verified ? 'verified' : 'unverified')}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('observed')}</dt>
              <dd>{formatDate(view.observed_at)}</dd>
            </div>
          </dl>
          <p className="mt-4 text-xs text-muted-foreground">{t('singleProcess')}</p>
        </section>
      )}
      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('rotate')}
        description={t('rotationDescription')}
        busy={busy}
        width={520}
      >
        {!view ? (
          <div className="space-y-4">
            <p role="status">{t('loadError')}</p>
            <Button
              variant="outline"
              disabled={!fresh || !admin || busy || permissions.isFetching || query.isFetching}
              onClick={() => {
                if (canRead) void query.refetch()
                else void permissions.refetch()
              }}
            >
              {t('refresh')}
            </Button>
          </div>
        ) : (
          <div className="space-y-5">
            <Table aria-label={t('internal')}>
              <thead>
                <tr>
                  <th>{t('key')}</th>
                  <th>{t('state')}</th>
                  <th>{t('configured')}</th>
                  <th>{t('verification')}</th>
                </tr>
              </thead>
              <tbody>
                {view.keys.map((key) => (
                  <tr key={key.id}>
                    <td>{key.id}</td>
                    <td>{t(key.state)}</td>
                    <td>{t(key.configured ? 'yes' : 'no')}</td>
                    <td>{t(key.verified ? 'verified' : 'unverified')}</td>
                  </tr>
                ))}
              </tbody>
            </Table>
            {!view.keys.length && <p>{t('noKeys')}</p>}
            <dl className="grid gap-3 text-sm sm:grid-cols-2">
              <div>
                <dt>{t('processEpoch')}</dt>
                <dd>{view.process.policy_epoch ?? t('unavailable')}</dd>
              </div>
              <div>
                <dt>{t('snapshot')}</dt>
                <dd className="break-all">{view.process.snapshot_id ?? t('unavailable')}</dd>
              </div>
              <div>
                <dt>{t('verifiedAt')}</dt>
                <dd>{formatDate(view.process.verified_at)}</dd>
              </div>
            </dl>
            <p className="text-xs text-muted-foreground">{t('currentInventory')}</p>
            {view.rotation ? (
              <>
                <div className="flex flex-wrap items-center gap-2">
                  <span>{view.rotation.id}</span>
                  <Badge>{t(view.rotation.status)}</Badge>
                  <span>
                    {t('phase')}:{' '}
                    {t(
                      view.rotation.phase === 'verification'
                        ? 'verificationPhase'
                        : view.rotation.phase === 'completed'
                          ? 'completedPhase'
                          : view.rotation.phase,
                    )}
                  </span>
                </div>
                {view.rotation.inventory_version < view.inventory_version && (
                  <p className="text-xs text-muted-foreground">
                    {t('historicalInventory', { count: view.rotation.domains.length })}
                  </p>
                )}
                <dl className="grid gap-3 text-sm sm:grid-cols-2">
                  <div>
                    <dt>{t('source')}</dt>
                    <dd>{view.rotation.source_key_id}</dd>
                  </div>
                  <div>
                    <dt>{t('target')}</dt>
                    <dd>{view.rotation.target_key_id}</dd>
                  </div>
                  <div>
                    <dt>{t('observationStart')}</dt>
                    <dd>{formatDate(view.rotation.observation_started_at)}</dd>
                  </div>
                  <div>
                    <dt>{t('observationEligible')}</dt>
                    <dd>{formatDate(view.rotation.observation_eligible_at)}</dd>
                  </div>
                </dl>
                <Table aria-label={t('domain')}>
                  <thead>
                    <tr>
                      <th>{t('domain')}</th>
                      {(
                        [
                          'scanned',
                          'rewrapped',
                          'already_target',
                          'deleted',
                          'changed',
                          'blocked',
                        ] as const
                      ).map((name) => (
                        <th key={name}>{t(name)}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {view.rotation.domains.map((domain) => (
                      <tr key={domain.code}>
                        <td>{t(domain.code)}</td>
                        {(
                          [
                            'scanned',
                            'rewrapped',
                            'already_target',
                            'deleted',
                            'changed',
                            'blocked',
                          ] as const
                        ).map((name) => (
                          <td key={name}>{domain[name] ?? t('notScanned')}</td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </Table>
                <p className="text-xs text-muted-foreground">{t('countsGuidance')}</p>
                <p className="text-xs text-muted-foreground">{t('observationGuidance')}</p>
                {!!view.rotation.blocker_codes.length && (
                  <div>
                    <h3 className="text-sm font-medium">{t('blockers')}</h3>
                    <ul className="list-inside list-disc text-sm">
                      {view.rotation.blocker_codes.map((code) => (
                        <li key={code}>{t(knownBlockers.has(code) ? code : 'unknownBlocker')}</li>
                      ))}
                    </ul>
                  </div>
                )}
              </>
            ) : (
              <p>{t('noRotation')}</p>
            )}
            {result && (
              <section className="rounded-md border p-3 text-sm" aria-label={t('receipt')}>
                <h3 className="font-medium">{t('receipt')}</h3>
                <p>
                  {t('request')}: {result.receipt.request_id}
                </p>
                <p>
                  {t('receiptAction')}: {t(result.receipt.action)}
                </p>
                <p>
                  {t('committedAt')}: {formatDate(result.receipt.created_at)}
                </p>
                <p>
                  {t('responseApplication')}:{' '}
                  {t(
                    result.application_status === 'unavailable'
                      ? 'applicationUnavailable'
                      : result.application_status,
                  )}
                </p>
                <p>
                  {t('responseWrite')}: {t(result.write_policy_applied ? 'yes' : 'no')}
                </p>
                <p>
                  {t('responsePublication')}: {t(result.publication_applied ? 'yes' : 'no')}
                </p>
                <p className="my-2 text-muted-foreground">{t('receiptGuidance')}</p>
                <Link
                  className="underline"
                  to={`/admin/secrets/rotations/${result.receipt.rotation_id}`}
                >
                  {t('receiptLink')}
                </Link>
              </section>
            )}
            <Button variant="outline" disabled={busy} onClick={() => void review()}>
              {t('review')}
            </Button>
            <p className="text-xs break-all text-muted-foreground">
              {t('reviewed')}: {reviewedCurrent ? reviewed?.review_etag : t('unavailable')}
            </p>
            {!canWrite && <p>{t('noWrite')}</p>}
            {uncertain ? (
              <>
                <p role="alert">{t(notice === 'retryRejected' ? 'retryRejected' : 'unknown')}</p>
                {intent && (
                  <section
                    className="rounded-md border p-3 text-sm"
                    aria-label={t('originalIntent')}
                  >
                    <h3 className="font-medium">{t('originalIntent')}</h3>
                    <p>
                      {t('request')}: {intent.input.request_id}
                    </p>
                    <p>
                      {t('receiptAction')}: {t(intent.action)}
                    </p>
                    <p>
                      {t(intent.action === 'start' ? 'target' : 'job')}:{' '}
                      {intent.input.target_key_id ?? intent.rotationId}
                    </p>
                    <p>
                      {t('reason')}: {intent.input.reason}
                    </p>
                    <p className="break-all">
                      {t('originalETag')}: {intent.etag}
                    </p>
                  </section>
                )}
                <Button disabled={!canWrite || busy} onClick={() => void submit(true)}>
                  {busy ? t('submitting') : t('retry')}
                </Button>
              </>
            ) : (
              <>
                {canStart && (
                  <label className="block text-sm">
                    {t('target')}
                    <select
                      className="mt-1 h-11 w-full rounded-md border bg-background px-3"
                      value={target}
                      disabled={!canWrite || busy}
                      onChange={(event) => setTarget(event.target.value)}
                    >
                      <option value="">{t('choose')}</option>
                      {targets.map((key) => (
                        <option key={key.id} value={key.id}>
                          {key.id}
                        </option>
                      ))}
                    </select>
                  </label>
                )}
                <label className="block text-sm">
                  {t('reason')}
                  <Input
                    className="mt-1"
                    value={reason}
                    disabled={!canWrite || busy}
                    onChange={(event) => setReason(event.target.value)}
                  />
                </label>
                {notice && <p role="alert">{t(notice)}</p>}
                <div className="flex flex-wrap justify-end gap-2">
                  {canStart ? (
                    <Button
                      disabled={!canWrite || busy || !reviewedCurrent || !target}
                      onClick={() => prepare('start')}
                    >
                      {t('start')}
                    </Button>
                  ) : (
                    view.rotation?.allowed_actions.map((action) => (
                      <Button
                        key={action}
                        variant="outline"
                        className={action === 'retire' ? 'text-destructive' : undefined}
                        disabled={!canWrite || busy || !reviewedCurrent}
                        onClick={() => prepare(action)}
                      >
                        {t(action)}
                      </Button>
                    ))
                  )}
                </div>
              </>
            )}
          </div>
        )}
      </Dialog>
      <Dialog
        open={confirm && visible}
        onOpenChange={(value) => {
          setConfirm(value)
          if (!value && !uncertain) setIntent(undefined)
        }}
        title={t('confirmTitle')}
        description={t('confirmDescription')}
        busy={busy}
      >
        {intent && (
          <>
            <dl className="space-y-2 text-sm">
              <div>
                <dt>{t('receiptAction')}</dt>
                <dd>{t(intent.action)}</dd>
              </div>
              <div>
                <dt>{t(intent.action === 'start' ? 'target' : 'job')}</dt>
                <dd>{intent.input.target_key_id ?? intent.rotationId}</dd>
              </div>
              <div>
                <dt>{t('reason')}</dt>
                <dd>{intent.input.reason}</dd>
              </div>
            </dl>
            <div className="mt-6 flex justify-end gap-2">
              <Button
                variant="outline"
                onClick={() => {
                  setConfirm(false)
                  setIntent(undefined)
                }}
              >
                {t('cancel')}
              </Button>
              <Button disabled={!canWrite || busy} onClick={() => void submit()}>
                {t('confirm')}
              </Button>
            </div>
          </>
        )}
      </Dialog>
    </Page>
  )
}
