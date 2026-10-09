import { useEffect, useEffectEvent, useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { isAxiosError } from 'axios'
import { getConnectionMetadata } from '@/api/connection-metadata'
import { testConnection } from '@/api/connection-test'
import type { ConnectionTestResult } from '@/types/connection-test'
import type { ConnectionMetadata } from '@/types/connection-metadata'
import type { Provider } from '@/types/catalog'
import type { Session } from '@/types/auth'
import { sessionKey } from '@/hooks/use-auth'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Menu, MenuItem } from '@/components/ui/menu'
import { useConnectionQueryRevision } from './connection-authority'

interface Props {
  actor: string
  providerId: string
  connectionId: string
  credentialId?: string
  generation: number
  permissionsKey: readonly unknown[]
  catalogueKey: readonly unknown[]
  onClose: () => void
  onManage: () => void
  finalFocus?: HTMLElement
}
export default function ConnectionTester(props: Props) {
  const { t, i18n } = useTranslation('catalog')
  const cache = useQueryClient()
  const authority = useConnectionQueryRevision([
    sessionKey,
    props.permissionsKey,
    props.catalogueKey,
  ])
  const identity = JSON.stringify([
    props.actor,
    props.providerId,
    props.connectionId,
    props.credentialId ?? null,
    props.generation,
    authority.revision,
  ])
  const [initialIdentity] = useState(identity)
  const validAuthority = identity === initialIdentity
  const currentConnection = () =>
    cache
      .getQueryData<Provider[]>(props.catalogueKey)
      ?.find((row) => row.id === props.providerId)
      ?.connections.find((row) => row.id === props.connectionId)
  const authorized = () => {
    const auth = cache.getQueryState<Session | null>(sessionKey)
    const permissions = cache.getQueryState<string[]>(props.permissionsKey)
    const catalogue = cache.getQueryState(props.catalogueKey)
    return (
      validAuthority &&
      authority.snapshot() === authority.revision &&
      auth?.data?.user.id === props.actor &&
      auth.dataUpdateCount === props.generation &&
      !!auth.data.csrf_token &&
      [auth, permissions, catalogue].every(
        (state) =>
          state?.status === 'success' &&
          state.fetchStatus === 'idle' &&
          !state.isInvalidated &&
          !state.error,
      ) &&
      permissions?.data?.includes('providers.read') === true &&
      permissions.data.includes('providers.write') &&
      !!currentConnection() &&
      (props.credentialId === undefined ||
        currentConnection()?.credentials.some((row) => row.id === props.credentialId) === true)
    )
  }
  const key = [
    'admin',
    'connection-test-review',
    props.actor,
    props.providerId,
    props.connectionId,
    props.generation,
    authority.revision,
  ]
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getConnectionMetadata(props.providerId, props.connectionId, signal),
    enabled: authorized(),
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const resource = useConnectionQueryRevision([key])
  const mounted = useRef(true)
  const expired = useRef(false)
  const pending = useRef<AbortController | null>(null)
  const blocked = useRef(false)
  const selectedIdentity = useRef('')
  const [selected, setSelected] = useState('')
  const selectedId = props.credentialId ?? selected
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<ConnectionTestResult | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const fresh = () => {
    const review = cache.getQueryState<ConnectionMetadata>(key)
    return (
      authorized() &&
      resource.snapshot() === resource.revision &&
      review?.status === 'success' &&
      review.fetchStatus === 'idle' &&
      !review.isInvalidated &&
      !review.error &&
      review.data === query.data &&
      review.data?.id === props.connectionId &&
      review.data.provider_id === props.providerId &&
      review.data.can_edit &&
      !!currentConnection()
    )
  }
  const current = validAuthority && fresh()
  const stamp = `${current}:${resource.revision}`
  const [viewStamp, setViewStamp] = useState(stamp)
  if (viewStamp !== stamp) {
    setViewStamp(stamp)
    setBusy(false)
    setResult(null)
    setSelected('')
    setNotice(null)
  }
  const dismiss = useEffectEvent(() => props.onClose())
  useLayoutEffect(() => {
    if (!validAuthority) {
      pending.current?.abort()
      dismiss()
    }
  }, [validAuthority])
  useEffect(() => {
    mounted.current = true
    const expire = () => {
      expired.current = true
      pending.current?.abort()
      dismiss()
    }
    window.addEventListener('routex:session-expired', expire)
    return () => {
      mounted.current = false
      pending.current?.abort()
      window.removeEventListener('routex:session-expired', expire)
    }
  }, [])
  useLayoutEffect(() => {
    pending.current?.abort()
    pending.current = null
    blocked.current = false
    selectedIdentity.current = ''
  }, [current, resource.revision])
  const credentials = current ? (currentConnection()?.credentials ?? []) : []
  const selectedCredential = credentials.find((row) => row.id === selectedId)
  const run = async () => {
    if (
      !validAuthority ||
      !mounted.current ||
      expired.current ||
      blocked.current ||
      (props.credentialId === undefined && selectedIdentity.current !== selectedId) ||
      !fresh() ||
      pending.current ||
      (notice !== null && notice !== 'unknown') ||
      !query.data ||
      !currentConnection()?.credentials.some((row) => row.id === selectedId)
    )
      return
    const controller = new AbortController()
    pending.current = controller
    const stamp = resource.snapshot()
    const authorityStamp = authority.snapshot()
    const credentialId = selectedId
    setBusy(true)
    setResult(null)
    setNotice(null)
    const live = () =>
      pending.current === controller &&
      !controller.signal.aborted &&
      mounted.current &&
      !expired.current &&
      fresh() &&
      authority.snapshot() === authorityStamp &&
      resource.snapshot() === stamp
    try {
      const reply = await testConnection(
        props.connectionId,
        credentialId,
        query.data.etag,
        cache.getQueryData<Session>(sessionKey)!.csrf_token,
        controller.signal,
      )
      if (
        reply.scope !==
        (query.data.adapter === 'azure_openai_classic' ? 'authentication_only' : 'model_discovery')
      )
        throw new Error('Connection test response unavailable')
      if (live()) setResult(reply)
    } catch (error) {
      if (live()) {
        const status = isAxiosError(error) ? error.response?.status : undefined
        blocked.current = status === 403 || status === 400 || status === 409
        setNotice(
          status === 403
            ? 'denied'
            : status === 400 || status === 409
              ? 'reviewChanged'
              : 'unknown',
        )
      }
    } finally {
      if (pending.current === controller) {
        pending.current = null
        if (mounted.current) setBusy(false)
      }
    }
  }
  const close = () => {
    pending.current?.abort()
    pending.current = null
    props.onClose()
  }
  return (
    <Dialog
      open={validAuthority}
      onOpenChange={(open) => !open && close()}
      title={t('connectionTest.title')}
      description={t(
        props.credentialId === undefined
          ? 'connectionTest.description'
          : 'connectionTest.lockedDescription',
      )}
      finalFocus={() => props.finalFocus ?? false}
    >
      <div className="space-y-4">
        {!current && (
          <p role={query.isError ? 'alert' : 'status'}>
            {t(
              authorized() && (query.isPending || query.isFetching)
                ? 'connectionTest.loading'
                : 'connectionTest.unavailable',
            )}
          </p>
        )}
        {current && (
          <>
            <p className="text-sm">
              {t('connectionTest.connection', { name: query.data?.name, id: props.connectionId })}
            </p>
            {credentials.length === 0 ? (
              <div className="space-y-2">
                <p>{t('connectionTest.noCredentials')}</p>
                <Button
                  variant="outline"
                  onClick={() => {
                    if (fresh()) {
                      close()
                      props.onManage()
                    }
                  }}
                >
                  {t('connectionTest.manage')}
                </Button>
              </div>
            ) : (
              <>
                {props.credentialId !== undefined ? (
                  <div className="space-y-2">
                    <p
                      aria-label={t('connectionTest.lockedCredential')}
                      className="break-all text-sm"
                    >
                      {t('connectionTest.option', {
                        name: selectedCredential?.name,
                        id: selectedId,
                        state: t(
                          selectedCredential?.enabled
                            ? 'connectionStatus.enabled'
                            : 'connectionStatus.disabled',
                        ),
                        verification: t(
                          `providers.${selectedCredential?.verification_status === 'verified' ? 'verified' : selectedCredential?.verification_status === 'failed' ? 'failed' : 'pending'}`,
                        ),
                      })}
                    </p>
                    <p className="text-sm text-muted-foreground">
                      {t('connectionTest.lockedHelp')}
                    </p>
                  </div>
                ) : (
                  <Menu
                    label={t('connectionTest.credential')}
                    side="bottom"
                    trigger={
                      credentials.find((row) => row.id === selected)?.name ??
                      t('connectionTest.choose')
                    }
                  >
                    {credentials.map((row) => (
                      <MenuItem
                        key={row.id}
                        disabled={busy || (notice !== null && notice !== 'unknown')}
                        onClick={() => {
                          if (!pending.current && !blocked.current && fresh()) {
                            selectedIdentity.current = row.id
                            setSelected(row.id)
                            setResult(null)
                            setNotice(null)
                          }
                        }}
                      >
                        {t('connectionTest.option', {
                          name: row.name,
                          id: row.id,
                          state: t(
                            row.enabled ? 'connectionStatus.enabled' : 'connectionStatus.disabled',
                          ),
                          verification: t(
                            `providers.${row.verification_status === 'verified' ? 'verified' : row.verification_status === 'failed' ? 'failed' : 'pending'}`,
                          ),
                        })}
                      </MenuItem>
                    ))}
                  </Menu>
                )}
                {props.credentialId === undefined && selected && (
                  <p className="break-all text-sm text-muted-foreground">{selected}</p>
                )}
                <p className="text-sm text-muted-foreground">{t('connectionTest.boundary')}</p>
                {result && (
                  <div role="status" className="space-y-2">
                    <p>
                      {t(
                        result.outcome === 'passed'
                          ? 'connectionTest.passed'
                          : 'connectionTest.failed',
                      )}
                    </p>
                    <p>
                      {t(
                        result.scope === 'model_discovery'
                          ? 'connectionTest.discovery'
                          : 'connectionTest.authentication',
                      )}
                    </p>
                    {result.discovered_model_count !== null && (
                      <p>{t('connectionTest.count', { count: result.discovered_model_count })}</p>
                    )}
                    <p>
                      {t('connectionTest.checked', {
                        date: new Date(result.checked_at).toLocaleString(i18n.language),
                      })}
                    </p>
                  </div>
                )}
                {notice && <p role="alert">{t(`connectionTest.${notice}`)}</p>}
                {notice && notice !== 'unknown' && (
                  <Button
                    variant="outline"
                    onClick={() => {
                      if (fresh()) {
                        close()
                        void cache.invalidateQueries({ queryKey: props.permissionsKey })
                        void cache.invalidateQueries({ queryKey: props.catalogueKey })
                      }
                    }}
                  >
                    {t('connectionTest.review')}
                  </Button>
                )}
                <Button
                  disabled={
                    !selectedId || busy || !current || (notice !== null && notice !== 'unknown')
                  }
                  onClick={() => void run()}
                >
                  {t(busy ? 'connectionTest.testing' : 'connectionTest.run')}
                </Button>
              </>
            )}
          </>
        )}
        <Button variant="outline" onClick={close}>
          {t(busy ? 'connectionTest.cancel' : 'connectionTest.close')}
        </Button>
      </div>
    </Dialog>
  )
}
