import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import { getCredentialMetadata } from '@/api/credential-metadata'
import { createCredentialReplacement } from '@/api/credential-replacements'
import type { CredentialMetadata } from '@/types/credential-metadata'
import type { CredentialReplacementInput } from '@/types/credential-replacements'
import { usePermissions } from '@/hooks/use-permissions'
import { useSession } from '@/hooks/use-auth'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { useCredentialStorageContext } from './credential-storage-context'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import { useConnectionQueryRevision } from './connection-authority'
import type { CredentialStorageContext } from '@/types/provider-storage'

type Props = {
  providerId: string
  credentialId: string
  connectionId: string
  connectionName: string
  onClose: () => void
  onCreated: () => void
}

export default function CredentialReplacementDialog(props: Props) {
  const session = useSession()
  return (
    <ReplacementDialog
      key={`${session.data?.user.id}:${props.providerId}:${props.credentialId}`}
      {...props}
    />
  )
}

function ReplacementDialog(props: Props) {
  const { t } = useTranslation('catalog')
  const access = usePermissions()
  const session = useSession()
  const generation = useSessionGeneration()
  const cache = useQueryClient()
  const key = [
    'admin',
    'credential-replacement-source',
    session.data?.user.id,
    props.providerId,
    props.credentialId,
    generation,
  ]
  const [seed, setSeed] = useState<CredentialMetadata>()
  const read =
    session.isSuccess &&
    !session.isFetching &&
    !session.error &&
    access.isSuccess &&
    !access.isFetching &&
    !access.error &&
    access.can('providers.read')
  const query = useQuery({
    queryKey: key,
    queryFn: async ({ signal }) => {
      const record = await getCredentialMetadata(props.credentialId, signal)
      if (record.id !== props.credentialId || record.connection_id !== props.connectionId)
        throw new Error('Credential replacement source mismatch')
      return record
    },
    enabled: read,
    retry: false,
    gcTime: 0,
  })
  const revision = useConnectionQueryRevision([key])
  const ready = () => {
    const state = cache.getQueryState(key)
    return (
      read && state?.status === 'success' && state.fetchStatus === 'idle' && !state.isInvalidated
    )
  }
  if (ready() && query.data && !seed) setSeed(query.data)
  if (!seed && access.isSuccess && !access.isFetching && !access.can('providers.read')) return null
  if (!seed)
    return (
      <Dialog
        open
        width={560}
        onOpenChange={props.onClose}
        title={t('credentialReplacement.title')}
        description={t('credentialReplacement.description')}
      >
        <QueryState
          pending={query.isPending || query.isFetching}
          error={query.error}
          retry={() => void query.refetch()}
        />
      </Dialog>
    )
  return (
    <ReplacementEditor
      {...props}
      initial={seed}
      current={ready() ? query.data : undefined}
      sourceReady={ready}
      sourceStamp={revision.snapshot}
      reload={async () => {
        const result = await query.refetch()
        if (result.isError) throw result.error
        return result.data!
      }}
    />
  )
}

function ReplacementEditor({
  initial,
  current,
  reload,
  sourceReady,
  sourceStamp,
  ...props
}: Props & {
  initial: CredentialMetadata
  current: CredentialMetadata | undefined
  sourceReady: () => boolean
  sourceStamp: () => string
  reload: () => Promise<CredentialMetadata>
}) {
  const { t } = useTranslation('catalog')
  const authority = useCredentialStorageContext()
  const access = authority.permissions
  const session = authority.session
  const [storage, setStorage] = useState<CredentialStorageContext>()
  if (authority.context && !storage) setStorage(authority.context)
  const [reviewed, setReviewed] = useState(initial)
  const [name, setName] = useState('')
  const [secret, setSecret] = useState('')
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const [notice, setNotice] = useState('')
  const intent = useRef<
    { etag: string; body: CredentialReplacementInput; source: 'inline' | 'vault' } | undefined
  >(undefined)
  const lock = useRef(false)
  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      intent.current = undefined
    }
  }, [])
  const canWrite = authority.ready && sourceReady() && access.can('providers.write')
  const stale =
    conflict ||
    (!!current && reviewed.etag !== current.etag) ||
    (!!storage && !!authority.context && storage.etag !== authority.context.etag)
  const locked = busy || uncertain || !canWrite
  function dismiss() {
    intent.current = undefined
    setSecret('')
    props.onClose()
  }
  async function review() {
    if (lock.current || uncertain) return
    lock.current = true
    setBusy(true)
    try {
      const [record, policy] = await Promise.all([reload(), authority.query.refetch()])
      if (policy.isError || !authority.current() || !sourceReady())
        throw new Error('Review unavailable')
      setStorage(policy.data)
      if (!alive.current) return
      setReviewed(record)
      setConflict(false)
      intent.current = undefined
      setNotice('credentialReplacement.reviewed')
    } catch {
      if (alive.current) setNotice('credentialReplacement.reviewFailed')
    } finally {
      lock.current = false
      if (alive.current) setBusy(false)
    }
  }
  async function submit(event?: FormEvent, retry = false) {
    event?.preventDefault()
    if (lock.current || !authority.current() || !sourceReady() || !session.data) return
    if (retry ? !uncertain || !intent.current : uncertain || stale || !storage) return
    let captured = intent.current
    if (!retry) {
      const trimmedName = name.trim(),
        trimmedReason = reason.trim()
      if (!trimmedName || [...trimmedName].length > 100 || /[\p{Cc}\p{Cs}]/u.test(trimmedName)) {
        setNotice('credentialMetadata.nameError')
        return
      }
      const secretBytes = new TextEncoder().encode(secret).length
      if (!secretBytes || secretBytes > 2048 || /[\r\n]/.test(secret)) {
        setNotice('credentialReplacement.secretError')
        return
      }
      if (
        !trimmedReason ||
        new TextEncoder().encode(trimmedReason).length > 1024 ||
        /[\p{Cc}\p{Cs}]/u.test(trimmedReason)
      ) {
        setNotice('credentialMetadata.reasonError')
        return
      }
      try {
        captured = {
          etag: reviewed.etag,
          source: storage!.storage_source,
          body: {
            request_id: crypto.randomUUID(),
            storage_policy_etag: storage!.etag,
            name: trimmedName,
            secret,
            reason: trimmedReason,
          },
        }
      } catch {
        setNotice('credentialReplacement.failed')
        return
      }
      if (
        !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
          captured.body.request_id,
        )
      ) {
        setNotice('credentialReplacement.failed')
        return
      }
      intent.current = captured
    }
    if (!captured) return
    const stamp = authority.stamp(),
      sourceRevision = sourceStamp()
    lock.current = true
    setBusy(true)
    setNotice('')
    try {
      const result = await createCredentialReplacement(
        props.credentialId,
        props.connectionId,
        captured.etag,
        captured.body,
        authority.csrf()!,
      )
      if (!alive.current) return
      if (
        result.storage_source !== captured.source ||
        !authority.current() ||
        !sourceReady() ||
        stamp !== authority.stamp() ||
        sourceRevision !== sourceStamp()
      )
        throw new Error('Authority renewed')
      if (alive.current) {
        intent.current = undefined
        setSecret('')
        props.onCreated()
      }
    } catch (error) {
      if (!alive.current) return
      const status = axios.isAxiosError(error) ? error.response?.status : undefined
      if (
        retry ||
        !status ||
        status >= 500 ||
        !authority.current() ||
        !sourceReady() ||
        stamp !== authority.stamp() ||
        sourceRevision !== sourceStamp()
      ) {
        setUncertain(true)
        setNotice('credentialReplacement.uncertain')
      } else {
        intent.current = undefined
        if (status === 409) {
          setConflict(true)
          setNotice('credentialReplacement.stale')
        } else setNotice('credentialReplacement.failed')
      }
    } finally {
      lock.current = false
      if (alive.current) setBusy(false)
    }
  }
  return (
    <Dialog
      open
      width={560}
      busy={busy}
      onOpenChange={dismiss}
      title={t('credentialReplacement.title')}
      description={t('credentialReplacement.description')}
    >
      {!canWrite && (
        <p role="status">
          {t(authority.query.error ? 'credentialStorage.loadError' : 'credentialStorage.loading')}
        </p>
      )}
      <form className="space-y-5" noValidate onSubmit={(event) => void submit(event)}>
        <div hidden={!canWrite} className="rounded-lg border bg-muted/30 p-4 text-sm">
          <p className="font-medium">{t('credentialReplacement.stepsTitle')}</p>
          {storage && (
            <p>
              {t('credentialStorage.futureSource')}:{' '}
              {t(`credentialStorage.${storage.storage_source}`)}
            </p>
          )}
          <p className="mt-1 text-muted-foreground">{t('credentialReplacement.steps')}</p>
        </div>
        <dl hidden={!canWrite} className="grid grid-cols-2 gap-3 text-sm">
          <div>
            <dt className="text-muted-foreground">{t('credentialReplacement.source')}</dt>
            <dd className="break-all">
              {reviewed.name} · {reviewed.id}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('credentialReplacement.connection')}</dt>
            <dd className="break-all">
              {props.connectionName} · {reviewed.connection_id}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('common.priority')}</dt>
            <dd>{reviewed.priority}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('credentialReplacement.newState')}</dt>
            <dd>{t('credentialReplacement.pending')}</dd>
          </div>
        </dl>
        <fieldset hidden={!canWrite} disabled={locked} className="space-y-5">
          <FormField label={t('credentialReplacement.name')}>
            <Input
              autoComplete="off"
              value={name}
              disabled={locked}
              onChange={(event) => setName(event.target.value)}
            />
          </FormField>
          <FormField label={t('credentialReplacement.secret')}>
            <Input
              type="password"
              autoComplete="new-password"
              value={secret}
              disabled={locked}
              onChange={(event) => setSecret(event.target.value)}
            />
          </FormField>
          <FormField label={t('credentialReplacement.reason')}>
            <Input
              autoComplete="off"
              value={reason}
              disabled={locked}
              onChange={(event) => setReason(event.target.value)}
            />
          </FormField>
        </fieldset>
        {stale && !uncertain && (
          <p role="alert" className="text-sm">
            {t('credentialReplacement.stale')}
          </p>
        )}
        {notice && (
          <p role="status" className="text-sm">
            {t(notice)}
          </p>
        )}
        <div className="flex flex-wrap justify-end gap-2">
          {stale && !uncertain && (
            <Button type="button" variant="outline" disabled={busy} onClick={() => void review()}>
              {t('credentialReplacement.review')}
            </Button>
          )}
          {uncertain && (
            <Button
              type="button"
              disabled={busy || !canWrite || !session.data}
              onClick={() => void submit(undefined, true)}
            >
              {t('credentialReplacement.retry')}
            </Button>
          )}
          <Button type="button" variant="outline" disabled={busy} onClick={dismiss}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" disabled={locked || stale || !session.data}>
            {t('credentialReplacement.create')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
