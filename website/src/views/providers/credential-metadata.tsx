import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import { getCredentialMetadata, saveCredentialMetadata } from '@/api/credential-metadata'
import type { CredentialMetadata, CredentialMetadataInput } from '@/types/credential-metadata'
import { usePermissions } from '@/hooks/use-permissions'
import { useSession } from '@/hooks/use-auth'
import { ErrorNotice, FormField, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'

type Props = {
  providerId: string
  credentialId: string
  connectionId: string
  providerName: string
  connectionName: string
  onClose: () => void
  onSaved: () => void
}

export default function CredentialMetadataDialog(props: Props) {
  return <MetadataDialog key={`${props.providerId}:${props.credentialId}`} {...props} />
}

function MetadataDialog(props: Props) {
  const { t } = useTranslation('catalog')
  const access = usePermissions()
  const query = useQuery({
    queryKey: ['admin', 'credential-metadata', props.providerId, props.credentialId],
    queryFn: async ({ signal }) => {
      const result = await getCredentialMetadata(props.credentialId, signal)
      if (result.id !== props.credentialId || result.connection_id !== props.connectionId)
        throw new Error('Credential metadata resource mismatch')
      return result
    },
    enabled: access.can('providers.read'),
    retry: false,
  })
  if (!access.can('providers.read')) return null
  if (!query.data)
    return (
      <Dialog
        open
        onOpenChange={() => props.onClose()}
        title={t('credentialMetadata.title')}
        description={t('credentialMetadata.description')}
      >
        <QueryState
          pending={query.isPending}
          error={query.error}
          retry={() => void query.refetch()}
        />
      </Dialog>
    )
  return (
    <MetadataEditor
      {...props}
      initial={query.data}
      current={query.data}
      reload={async () => {
        const result = await query.refetch()
        if (result.isError) throw result.error
        return result.data!
      }}
    />
  )
}

function MetadataEditor({
  initial,
  current,
  reload,
  ...props
}: Props & {
  initial: CredentialMetadata
  current: CredentialMetadata
  reload: () => Promise<CredentialMetadata>
}) {
  const { t } = useTranslation('catalog')
  const access = usePermissions()
  const session = useSession()
  const [reviewed, setReviewed] = useState(initial)
  const [name, setName] = useState(initial.name)
  const [priority, setPriority] = useState(String(initial.priority))
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const [notice, setNotice] = useState('')
  const [error, setError] = useState<unknown>(null)
  const intent = useRef<{ etag: string; body: CredentialMetadataInput } | undefined>(undefined)
  const lock = useRef(false)
  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  const canWrite = access.can('providers.write')
  const stale = conflict || reviewed.etag !== current.etag
  const locked = busy || uncertain || !canWrite

  async function review() {
    if (lock.current) return
    lock.current = true
    setBusy(true)
    setError(null)
    try {
      const result = await reload()
      if (!alive.current) return
      setReviewed(result)
      setConflict(false)
      setUncertain(false)
      intent.current = undefined
      setNotice('credentialMetadata.reviewed')
    } catch (error) {
      if (alive.current) setError(error)
    } finally {
      lock.current = false
      if (alive.current) setBusy(false)
    }
  }

  async function submit(event?: FormEvent, retry = false) {
    event?.preventDefault()
    if (lock.current || !canWrite || !session.data) return
    if (retry ? !uncertain || !intent.current : stale || uncertain) return
    let captured = intent.current
    if (!retry) {
      const trimmedName = name.trim()
      if (!trimmedName || [...trimmedName].length > 100 || /[\p{Cc}\p{Cs}]/u.test(trimmedName)) {
        setNotice('credentialMetadata.nameError')
        return
      }
      if (
        !/^\d+$/.test(priority) ||
        !Number.isSafeInteger(Number(priority)) ||
        Number(priority) > 10000
      ) {
        setNotice('credentialMetadata.priorityError')
        return
      }
      const trimmedReason = reason.trim()
      if (
        !trimmedReason ||
        new TextEncoder().encode(trimmedReason).length > 1024 ||
        /[\p{Cc}\p{Cs}]/u.test(trimmedReason)
      ) {
        setNotice('credentialMetadata.reasonError')
        return
      }
      captured = {
        etag: reviewed.etag,
        body: { name: trimmedName, priority: Number(priority), reason: trimmedReason },
      }
      intent.current = captured
    }
    if (!captured) return
    lock.current = true
    setBusy(true)
    setError(null)
    setNotice('')
    try {
      await saveCredentialMetadata(
        props.credentialId,
        captured.etag,
        captured.body,
        session.data.csrf_token,
      )
      if (alive.current) props.onSaved()
    } catch (error) {
      if (!alive.current) return
      setError(error)
      const status = axios.isAxiosError(error) ? error.response?.status : undefined
      if (retry || !status || status >= 500) {
        setUncertain(true)
        setNotice('credentialMetadata.uncertain')
      } else {
        intent.current = undefined
        if (status === 409) {
          setConflict(true)
          setNotice('credentialMetadata.stale')
        }
      }
    } finally {
      lock.current = false
      if (alive.current) setBusy(false)
    }
  }

  return (
    <Dialog
      open
      onOpenChange={() => props.onClose()}
      busy={busy}
      title={t('credentialMetadata.title')}
      description={t('credentialMetadata.description')}
    >
      <form className="space-y-5" noValidate onSubmit={(event) => void submit(event)}>
        <div className="space-y-1 rounded-lg border p-4 text-sm">
          <p className="font-medium">{reviewed.name}</p>
          <p>{t('credentialMetadata.currentPriority', { priority: reviewed.priority })}</p>
          <p className="text-muted-foreground">
            {t('credentialMetadata.context', {
              provider: props.providerName,
              connection: props.connectionName,
            })}
          </p>
          <p className="break-all text-muted-foreground">
            {t('credentialMetadata.connectionId', { id: reviewed.connection_id })}
          </p>
        </div>
        <FormField label={t('common.credentialName')}>
          <Input
            value={name}
            autoComplete="off"
            disabled={locked}
            onChange={(event) => setName(event.target.value)}
          />
        </FormField>
        <FormField label={t('common.priority')}>
          <Input
            value={priority}
            inputMode="numeric"
            autoComplete="off"
            disabled={locked}
            onChange={(event) => setPriority(event.target.value)}
          />
        </FormField>
        <p className="text-sm text-muted-foreground">{t('credentialMetadata.priorityHelp')}</p>
        <FormField label={t('credentialMetadata.reason')}>
          <Input
            value={reason}
            autoComplete="off"
            disabled={locked}
            onChange={(event) => setReason(event.target.value)}
          />
        </FormField>
        <ErrorNotice error={error} />
        {stale && !uncertain && (
          <p role="alert" className="text-sm">
            {t('credentialMetadata.stale')}
          </p>
        )}
        {notice && (
          <p role="status" className="text-sm">
            {t(notice)}
          </p>
        )}
        <div className="flex flex-wrap justify-end gap-2">
          {(stale || uncertain) && (
            <Button type="button" variant="outline" disabled={busy} onClick={() => void review()}>
              {t('credentialMetadata.review')}
            </Button>
          )}
          {uncertain && (
            <Button
              type="button"
              disabled={busy || !canWrite || !session.data}
              onClick={() => void submit(undefined, true)}
            >
              {t('credentialMetadata.retry')}
            </Button>
          )}
          <Button type="button" variant="outline" disabled={busy} onClick={props.onClose}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" disabled={locked || stale || !session.data}>
            {t('credentialMetadata.save')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
