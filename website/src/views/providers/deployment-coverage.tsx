import { useEffect, useEffectEvent, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { isAxiosError } from 'axios'
import { getDeploymentCoverage, saveDeploymentCoverage } from '@/api/deployment-coverage'
import { trimConnectionMetadata, validConnectionReason } from '@/api/connection-metadata'
import { listProviders } from '@/api/catalog'
import type { DeploymentCoverage, DeploymentCoverageInput } from '@/types/deployment-coverage'
import type { Provider } from '@/types/catalog'
import type { Session } from '@/types/auth'
import { useSession, sessionKey } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import { useConnectionQueryRevision } from './connection-authority'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { FormField, QueryState } from '@/components/app/CatalogUI'
type Props = {
  providerId: string
  connectionId: string
  credentialId: string
  onClose: () => void
  onSaved: () => void
}
type Intent = { etag: string; input: DeploymentCoverageInput }
export default function DeploymentCoverageDialog(props: Props) {
  const session = useSession(),
    actor = session.data?.user.id ?? ''
  return (
    <CoverageEditor
      key={`${actor}:${props.providerId}:${props.connectionId}:${props.credentialId}`}
      {...props}
      actor={actor}
    />
  )
}
function CoverageEditor(props: Props & { actor: string }) {
  const { t } = useTranslation('catalog'),
    cache = useQueryClient(),
    access = usePermissions(),
    generation = useSessionGeneration()
  const permissionKey = ['permissions', props.actor] as const
  const key = [
    'admin',
    'deployment-coverage',
    props.actor,
    props.providerId,
    props.connectionId,
    props.credentialId,
    generation,
  ] as const
  const catalogueKey = [
    'admin',
    'deployment-coverage-catalog',
    props.actor,
    props.providerId,
    props.connectionId,
    props.credentialId,
    generation,
  ] as const
  const authority = useConnectionQueryRevision([sessionKey, permissionKey])
  const readAuthority = () => {
    const auth = cache.getQueryState<Session | null>(sessionKey),
      permissions = cache.getQueryState<string[]>(permissionKey)
    return (
      !!props.actor &&
      auth?.data?.user.id === props.actor &&
      auth.status === 'success' &&
      auth.fetchStatus === 'idle' &&
      !auth.isInvalidated &&
      !auth.error &&
      permissions?.status === 'success' &&
      permissions.fetchStatus === 'idle' &&
      !permissions.isInvalidated &&
      !permissions.error &&
      permissions.data?.includes('providers.read') === true
    )
  }
  const catalogue = useQuery({
    queryKey: catalogueKey,
    queryFn: ({ signal }) => listProviders(signal),
    enabled: readAuthority(),
    retry: false,
    gcTime: 0,
    refetchOnWindowFocus: false,
  })
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getDeploymentCoverage(props.credentialId, props.connectionId, signal),
    enabled: readAuthority(),
    retry: false,
    gcTime: 0,
    refetchOnWindowFocus: false,
  })
  const observed = useConnectionQueryRevision([key, catalogueKey])
  const fresh = () => {
    const row = cache.getQueryState<DeploymentCoverage>(key),
      supply = cache.getQueryState<Provider[]>(catalogueKey)
    const providers = supply?.data?.filter((p) => p.id === props.providerId),
      connections = providers?.[0]?.connections.filter((c) => c.id === props.connectionId),
      connection = connections?.[0]
    return (
      readAuthority() &&
      authority.snapshot() === authority.revision &&
      observed.snapshot() === observed.revision &&
      row?.status === 'success' &&
      row.fetchStatus === 'idle' &&
      !row.error &&
      !row.isInvalidated &&
      row.data === query.data &&
      supply?.status === 'success' &&
      supply.fetchStatus === 'idle' &&
      !supply.error &&
      !supply.isInvalidated &&
      providers?.length === 1 &&
      connections?.length === 1 &&
      connection?.adapter === 'azure_openai_classic' &&
      connection.api_version === row.data?.api_version &&
      connection.credentials.filter((c) => c.id === props.credentialId).length === 1 &&
      connection.provider_models.length === row.data?.provider_models.length &&
      new Set(connection.provider_models.map((p) => p.id)).size ===
        connection.provider_models.length &&
      connection.provider_models.every((p) =>
        row.data?.provider_models.some((n) => n.id === p.id && n.upstream_name === p.upstream_name),
      )
    )
  }
  const writable = () =>
    fresh() &&
    access.can('providers.write') &&
    cache.getQueryData<string[]>(permissionKey)?.includes('providers.write') === true &&
    query.data?.can_edit === true
  const [review, setReview] = useState<DeploymentCoverage | null>(null),
    [selected, setSelected] = useState<string[]>([]),
    [reason, setReason] = useState(''),
    [notice, setNotice] = useState(''),
    [conflict, setConflict] = useState(false),
    [uncertain, setUncertain] = useState(false),
    [hasIntent, setHasIntent] = useState(false),
    [confirm, setConfirm] = useState(false),
    [confirmationReview, setConfirmationReview] = useState<
      (Intent & { authority: string; resource: string }) | null
    >(null),
    [busy, setBusy] = useState(false)
  const intent = useRef<Intent | null>(null),
    pending = useRef<AbortController | null>(null),
    alive = useRef(true)
  const current = fresh()
  const initialize = useEffectEvent(() => {
    if (current && query.data && !review && !intent.current) {
      setReview(query.data)
      setSelected(query.data.provider_models.filter((n) => n.attested).map((n) => n.id))
    }
  })
  useEffect(() => {
    initialize()
  }, [current, query.data])
  const invalidate = useEffectEvent(() => {
    if (!current && pending.current) {
      pending.current.abort()
      pending.current = null
      setBusy(false)
      setConfirm(false)
      setUncertain(true)
      setNotice('deploymentCoverage.uncertain')
    }
  })
  useEffect(() => {
    invalidate()
  }, [current])
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      pending.current?.abort()
      intent.current = null
    }
  }, [])
  const draftValid = () =>
    validConnectionReason(trimConnectionMetadata(reason)) &&
    !!review &&
    selected.every((id) => review.provider_models.some((n) => n.id === id && n.can_attest)) &&
    new Set(selected).size === selected.length
  const prepareReady =
    writable() &&
    !hasIntent &&
    !busy &&
    !conflict &&
    !!review &&
    review.etag === query.data?.etag &&
    draftValid()
  const confirmationCurrent =
    confirm &&
    current &&
    !!confirmationReview &&
    confirmationReview.authority === authority.revision &&
    confirmationReview.resource === observed.revision &&
    confirmationReview.etag === review?.etag &&
    confirmationReview.input.reason === trimConnectionMetadata(reason) &&
    JSON.stringify(confirmationReview.input.provider_model_ids) === JSON.stringify(selected)
  const canPrepare = () => prepareReady && !pending.current && !intent.current
  function reviewCurrent() {
    if (!writable() || intent.current || pending.current || !query.data) return
    setReview(query.data)
    setConflict(false)
    setNotice('deploymentCoverage.reviewed')
  }
  async function dispatch(retry: boolean) {
    if (!writable() || busy || pending.current) return
    if (!retry) {
      if (!confirmationCurrent || !canPrepare() || !confirmationReview) return
      intent.current = {
        etag: confirmationReview.etag,
        input: {
          provider_model_ids: [...confirmationReview.input.provider_model_ids],
          reason: confirmationReview.input.reason,
        },
      }
      setHasIntent(true)
    }
    const captured = intent.current,
      csrf = cache.getQueryData<Session>(sessionKey)?.csrf_token
    if (!captured || !csrf || (retry && !uncertain)) return
    const controller = new AbortController(),
      stamp = authority.snapshot(),
      resource = observed.snapshot()
    pending.current = controller
    setBusy(true)
    setConfirm(false)
    try {
      await saveDeploymentCoverage(
        props.credentialId,
        props.connectionId,
        captured.etag,
        captured.input,
        csrf,
        controller.signal,
      )
      if (
        !alive.current ||
        pending.current !== controller ||
        controller.signal.aborted ||
        authority.snapshot() !== stamp ||
        observed.snapshot() !== resource ||
        !writable()
      ) {
        if (alive.current && pending.current === controller) {
          setUncertain(true)
          setNotice('deploymentCoverage.uncertain')
        }
        return
      }
      intent.current = null
      setHasIntent(false)
      setUncertain(false)
      setReview(null)
      setConflict(false)
      setNotice('deploymentCoverage.saved')
      props.onSaved()
      void query.refetch()
      void catalogue.refetch()
    } catch (error) {
      if (!alive.current || pending.current !== controller) return
      const status = isAxiosError(error) ? error.response?.status : undefined
      if (
        retry ||
        !status ||
        status >= 500 ||
        controller.signal.aborted ||
        !writable() ||
        authority.snapshot() !== stamp ||
        observed.snapshot() !== resource
      ) {
        setUncertain(true)
        setNotice('deploymentCoverage.uncertain')
      } else {
        intent.current = null
        setHasIntent(false)
        setConflict(status === 409)
        setNotice(status === 409 ? 'deploymentCoverage.stale' : 'deploymentCoverage.failed')
      }
    } finally {
      if (alive.current && pending.current === controller) {
        pending.current = null
        setBusy(false)
      }
    }
  }
  const context = current ? query.data : undefined,
    locked = busy || hasIntent || !writable()
  return (
    <>
      <Dialog
        open={!confirmationCurrent}
        width={720}
        busy={busy}
        title={t('deploymentCoverage.title')}
        description={t('deploymentCoverage.description')}
        onOpenChange={(open) => {
          if (!open && !busy) props.onClose()
        }}
      >
        <QueryState
          pending={query.isFetching || catalogue.isFetching || !readAuthority()}
          error={query.error || catalogue.error}
          retry={() => {
            void query.refetch()
            void catalogue.refetch()
          }}
        />
        {context && (
          <form
            className="space-y-5"
            onSubmit={(event) => {
              event.preventDefault()
              if (canPrepare()) {
                setConfirmationReview({
                  etag: review!.etag,
                  input: {
                    provider_model_ids: [...selected],
                    reason: trimConnectionMetadata(reason),
                  },
                  authority: authority.snapshot(),
                  resource: observed.snapshot(),
                })
                setConfirm(true)
              }
            }}
          >
            <p className="rounded-md border bg-muted/30 p-3 text-sm">
              {t('deploymentCoverage.advisory')}
            </p>
            <p className="break-all text-sm">
              {context.credential_id} · {context.connection_id} · {context.api_version}
            </p>
            <p className="text-sm">
              {t('deploymentCoverage.authentication')}:{' '}
              {t(`providers.${context.verification_status}`)}
            </p>
            <Table aria-label={t('deploymentCoverage.models')}>
              <thead>
                <tr>
                  <th>{t('deploymentCoverage.select')}</th>
                  <th>{t('deploymentCoverage.deployment')}</th>
                  <th>{t('deploymentCoverage.attestation')}</th>
                </tr>
              </thead>
              <tbody>
                {context.provider_models.map((row) => (
                  <tr key={row.id}>
                    <td>
                      <Input
                        type="checkbox"
                        className="size-4"
                        aria-label={t('deploymentCoverage.selectDeployment', {
                          name: row.upstream_name,
                        })}
                        checked={selected.includes(row.id)}
                        disabled={locked || !row.can_attest}
                        onChange={(event) =>
                          setSelected((ids) =>
                            event.target.checked
                              ? [...ids, row.id]
                              : ids.filter((n) => n !== row.id),
                          )
                        }
                      />
                    </td>
                    <td>
                      <p>{row.upstream_name}</p>
                      <p className="break-all text-xs text-muted-foreground">{row.id}</p>
                    </td>
                    <td>
                      {t(
                        !row.can_attest
                          ? 'deploymentCoverage.unreviewable'
                          : row.attested
                            ? 'deploymentCoverage.attested'
                            : 'deploymentCoverage.unattested',
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
            {!context.provider_models.length && <p>{t('deploymentCoverage.empty')}</p>}
            <FormField label={t('deploymentCoverage.reason')}>
              <Input
                value={reason}
                disabled={locked}
                autoComplete="off"
                onChange={(e) => setReason(e.target.value)}
              />
            </FormField>
            {conflict || review?.etag !== context.etag ? (
              <p role="alert">{t('deploymentCoverage.stale')}</p>
            ) : null}
            {notice && <p role="status">{t(notice)}</p>}
            <div className="flex flex-wrap justify-end gap-2">
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => {
                  void query.refetch()
                  void catalogue.refetch()
                }}
              >
                {t('deploymentCoverage.refresh')}
              </Button>
              {!hasIntent && writable() && (
                <>
                  <Button type="button" variant="outline" disabled={busy} onClick={reviewCurrent}>
                    {t('deploymentCoverage.review')}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={locked}
                    onClick={() => setSelected([])}
                  >
                    {t('deploymentCoverage.revoke')}
                  </Button>
                  <Button type="submit" disabled={!prepareReady}>
                    {t('deploymentCoverage.save')}
                  </Button>
                </>
              )}
              {uncertain && (
                <Button
                  type="button"
                  disabled={busy || !writable()}
                  onClick={() => void dispatch(true)}
                >
                  {t('deploymentCoverage.retry')}
                </Button>
              )}
              <Button type="button" variant="outline" disabled={busy} onClick={props.onClose}>
                {t('common.cancel')}
              </Button>
            </div>
          </form>
        )}
      </Dialog>
      <Dialog
        open={confirmationCurrent}
        width={560}
        title={t('deploymentCoverage.confirmTitle')}
        description={t('deploymentCoverage.confirmDescription', {
          count: confirmationReview?.input.provider_model_ids.length ?? 0,
        })}
        onOpenChange={(open) => {
          if (!open) setConfirm(false)
        }}
      >
        <p className="break-all">{confirmationReview?.input.reason}</p>
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={() => setConfirm(false)}>
            {t('common.cancel')}
          </Button>
          <Button disabled={!prepareReady} onClick={() => void dispatch(false)}>
            {t('deploymentCoverage.confirm')}
          </Button>
        </div>
      </Dialog>
    </>
  )
}
