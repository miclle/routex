import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import { getCredentialMetadata } from '@/api/credential-metadata'
import { deleteCredential } from '@/api/credential-delete'
import type { CredentialMetadata } from '@/types/credential-metadata'
import type { CredentialDeleteInput } from '@/types/credential-delete'
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
  connectionName: string
  onClose: () => void
  onDeleted: () => void
}

export default function CredentialDeleteDialog(props: Props) {
  return <DeleteDialog key={`${props.providerId}:${props.credentialId}`} {...props} />
}

function DeleteDialog(props: Props) {
  const { t } = useTranslation('catalog')
  const access = usePermissions()
  const query = useQuery({
    queryKey: ['admin', 'credential-delete-preview', props.providerId, props.credentialId],
    queryFn: async ({ signal }) => {
      const record = await getCredentialMetadata(props.credentialId, signal)
      if (
        record.id !== props.credentialId ||
        record.connection_id !== props.connectionId ||
        !/^[a-f0-9]{64}$/.test(record.etag)
      )
        throw new Error('Credential deletion preview unavailable')
      return record
    },
    enabled: access.can('providers.read'),
    retry: false,
    gcTime: 0,
  })
  if (!access.can('providers.read')) return null
  if (!query.data)
    return (
      <Dialog
        open
        onOpenChange={() => props.onClose()}
        title={t('credentialDelete.title')}
        description={t('credentialDelete.description')}
      >
        <QueryState
          pending={query.isPending}
          error={query.error}
          retry={() => void query.refetch()}
        />
      </Dialog>
    )
  return (
    <DeleteConfirmation
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

function DeleteConfirmation({
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
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const [notice, setNotice] = useState('')
  const [error, setError] = useState<unknown>(null)
  const intent = useRef<{ etag: string; body: CredentialDeleteInput } | undefined>(undefined)
  const lock = useRef(false)
  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  const canWrite = access.can('providers.write')
  const stale = conflict || current.etag !== reviewed.etag

  async function review() {
    if (lock.current) return
    lock.current = true
    setBusy(true)
    setError(null)
    try {
      const record = await reload()
      if (!alive.current) return
      setReviewed(record)
      setConflict(false)
      setUncertain(false)
      intent.current = undefined
      setNotice('credentialDelete.reviewed')
    } catch (error) {
      if (alive.current) {
        setError(error)
        if (axios.isAxiosError(error) && error.response?.status === 404)
          setNotice('credentialDelete.absenceUnconfirmed')
      }
    } finally {
      lock.current = false
      if (alive.current) setBusy(false)
    }
  }

  async function submit(event?: FormEvent, retry = false) {
    event?.preventDefault()
    if (lock.current || !canWrite || !session.data) return
    if (retry ? !uncertain || !intent.current : uncertain || stale) return
    let captured = intent.current
    if (!retry) {
      const trimmed = reason.trim()
      if (
        !trimmed ||
        new TextEncoder().encode(trimmed).length > 1024 ||
        /[\p{Cc}\p{Cs}]/u.test(trimmed)
      ) {
        setNotice('credentialDelete.reasonError')
        return
      }
      captured = { etag: reviewed.etag, body: { reason: trimmed } }
      intent.current = captured
    }
    if (!captured) return
    lock.current = true
    setBusy(true)
    setNotice('')
    setError(null)
    try {
      await deleteCredential(
        props.credentialId,
        captured.etag,
        captured.body,
        session.data.csrf_token,
      )
      if (alive.current) props.onDeleted()
    } catch (error) {
      if (!alive.current) return
      setError(error)
      const status = axios.isAxiosError(error) ? error.response?.status : undefined
      if (retry || !status || status >= 500) {
        setUncertain(true)
        setNotice('credentialDelete.uncertain')
      } else {
        intent.current = undefined
        if (status === 409) {
          setConflict(true)
          setNotice('credentialDelete.stale')
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
      title={t('credentialDelete.title')}
      description={t('credentialDelete.description')}
    >
      <form noValidate className="space-y-5" onSubmit={(event) => void submit(event)}>
        <div className="space-y-1 rounded-lg border p-4 text-sm">
          <p className="font-medium">{reviewed.name}</p>
          <p>
            {t('credentialDelete.connection', {
              name: props.connectionName,
              id: reviewed.connection_id,
            })}
          </p>
        </div>
        <div className="space-y-2 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">
          <p className="font-medium">{t('credentialDelete.warningTitle')}</p>
          <p>{t('credentialDelete.warning')}</p>
        </div>
        <FormField label={t('credentialDelete.reason')}>
          <Input
            autoComplete="off"
            value={reason}
            disabled={busy || uncertain || !canWrite}
            onChange={(event) => setReason(event.target.value)}
          />
        </FormField>
        <ErrorNotice error={error} />
        {stale && !uncertain && (
          <p role="alert" className="text-sm">
            {t('credentialDelete.stale')}
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
              {t('credentialDelete.review')}
            </Button>
          )}
          {uncertain && (
            <Button
              type="button"
              disabled={busy || !canWrite || !session.data}
              onClick={() => void submit(undefined, true)}
            >
              {t('credentialDelete.retry')}
            </Button>
          )}
          <Button type="button" variant="outline" disabled={busy} onClick={props.onClose}>
            {t('common.cancel')}
          </Button>
          <Button
            type="submit"
            className="bg-destructive text-white hover:bg-destructive/90"
            disabled={busy || uncertain || stale || !canWrite || !session.data}
          >
            {t('credentialDelete.confirm')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
