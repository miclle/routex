import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import { retireCredential } from '@/api/credential-retirement'
import type { CredentialReadiness } from '@/types/credential-readiness'
import {
  credentialRetirementBlockers,
  type CredentialRetirementInput,
  type CredentialRetirementResult,
} from '@/types/credential-retirement'
import { usePermissions } from '@/hooks/use-permissions'
import { useSession } from '@/hooks/use-auth'
import { FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'

type Props = {
  initial: CredentialReadiness
  current?: CredentialReadiness
  reload: () => Promise<CredentialReadiness>
  onApplied: () => void
}

export default function CredentialRetirementControls({
  initial,
  current,
  reload,
  onApplied,
}: Props) {
  const { t, i18n } = useTranslation('catalog')
  const access = usePermissions()
  const session = useSession()
  const [reviewed, setReviewed] = useState(initial)
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const [result, setResult] = useState<CredentialRetirementResult>()
  const [notice, setNotice] = useState('')
  const intent = useRef<{ etag: string; body: CredentialRetirementInput } | undefined>(undefined)
  const lock = useRef(false)
  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      intent.current = undefined
    }
  }, [])
  const canWrite = access.can('providers.write')
  const stale = conflict || !current || current.etag !== reviewed.etag
  const frozen = uncertain || !!result
  const canSubmit = canWrite && !busy && !frozen && !stale && reviewed.eligible

  function validReason() {
    const trimmed = reason.trim()
    if (
      !trimmed ||
      new TextEncoder().encode(trimmed).length > 1024 ||
      /[\p{Cc}\p{Cs}]/u.test(trimmed)
    ) {
      setNotice('credentialRetirement.reasonError')
      return undefined
    }
    return trimmed
  }
  function confirm(event: FormEvent) {
    event.preventDefault()
    if (!canSubmit || !validReason()) return
    setNotice('')
    setConfirming(true)
  }
  async function review() {
    if (lock.current || frozen) return
    lock.current = true
    setBusy(true)
    try {
      const record = await reload()
      if (!alive.current) return
      setReviewed(record)
      setConflict(false)
      intent.current = undefined
      setNotice('credentialRetirement.reviewed')
    } catch {
      if (alive.current) setNotice('credentialRetirement.reviewFailed')
    } finally {
      lock.current = false
      if (alive.current) setBusy(false)
    }
  }
  async function submit(retry = false) {
    if (lock.current || !canWrite || !session.data) return
    if (retry ? !frozen || !intent.current || result?.runtime_applied : !canSubmit) return
    let captured = intent.current
    if (!retry) {
      const trimmed = validReason()
      if (!trimmed || !reviewed.snapshot_id || !reviewed.evidence) return
      try {
        captured = {
          etag: reviewed.etag,
          body: {
            request_id: crypto.randomUUID(),
            replacement_credential_id: reviewed.replacement.id,
            evidence_attempt_id: reviewed.evidence.attempt_id,
            snapshot_id: reviewed.snapshot_id,
            reason: trimmed,
          },
        }
      } catch {
        setNotice('credentialRetirement.failed')
        return
      }
      if (
        !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
          captured.body.request_id,
        )
      ) {
        setNotice('credentialRetirement.failed')
        return
      }
      intent.current = captured
    }
    if (!captured) return
    lock.current = true
    setBusy(true)
    setConfirming(false)
    setNotice('')
    try {
      const receipt = await retireCredential(
        reviewed.source.id,
        captured.etag,
        captured.body,
        session.data.csrf_token,
      )
      if (!alive.current) return
      setResult(receipt)
      setUncertain(false)
      if (receipt.runtime_applied) {
        intent.current = undefined
        onApplied()
      }
    } catch (error) {
      if (!alive.current) return
      const status = axios.isAxiosError(error) ? error.response?.status : undefined
      // A rejected retry cannot disprove the original uncertain commit or erase a saved receipt.
      if (retry) setNotice('credentialRetirement.retryFailed')
      else if (
        status === 400 ||
        status === 401 ||
        status === 403 ||
        status === 404 ||
        status === 409
      ) {
        intent.current = undefined
        setConflict(status === 409 || status === 404)
        setNotice('credentialRetirement.failed')
      } else {
        setUncertain(true)
        setNotice('credentialRetirement.uncertain')
      }
    } finally {
      lock.current = false
      if (alive.current) setBusy(false)
    }
  }
  if (!canWrite) return null
  return (
    <div className="space-y-4 border-t pt-4">
      {result && (
        <div role="status" className="space-y-2 rounded-md border p-3 text-sm">
          <p className="font-medium">{t('credentialRetirement.saved')}</p>
          <time dateTime={result.committed_at}>
            {new Date(result.committed_at).toLocaleString(
              i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
            )}
          </time>
          <p>
            {t(
              result.runtime_applied
                ? 'credentialRetirement.applied'
                : 'credentialRetirement.pending',
            )}
          </p>
          {result.current_snapshot_id && (
            <p className="break-all">
              {t('credentialRetirement.currentSnapshot', { id: result.current_snapshot_id })}
            </p>
          )}
          {result.blockers.length > 0 && (
            <ul className="list-disc space-y-1 pl-5">
              {result.blockers.map((blocker, index) => (
                <li key={`${blocker}:${index}`}>
                  {t(
                    credentialRetirementBlockers.includes(
                      blocker as (typeof credentialRetirementBlockers)[number],
                    )
                      ? `credentialRetirement.blockers.${blocker}`
                      : 'credentialRetirement.blockers.unknown',
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
      {notice && (
        <p role="status" className="text-sm">
          {t(notice)}
        </p>
      )}
      {uncertain && notice !== 'credentialRetirement.uncertain' && (
        <p role="status" className="text-sm">
          {t('credentialRetirement.uncertain')}
        </p>
      )}
      {frozen ? (
        !result?.runtime_applied && (
          <Button disabled={busy || !session.data} onClick={() => void submit(true)}>
            {t('credentialRetirement.retry')}
          </Button>
        )
      ) : (
        <form onSubmit={confirm} className="space-y-4">
          {stale && (
            <p role="status" className="text-sm">
              {t('credentialRetirement.stale')}
            </p>
          )}
          <FormField label={t('credentialRetirement.reason')}>
            <Input
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              disabled={busy || confirming}
              required
              maxLength={1024}
              autoComplete="off"
            />
          </FormField>
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              disabled={busy || confirming}
              onClick={() => void review()}
            >
              {t('credentialRetirement.review')}
            </Button>
            <Button type="submit" disabled={!canSubmit || confirming}>
              {t('credentialRetirement.retire')}
            </Button>
          </div>
        </form>
      )}
      <Dialog
        open={confirming}
        busy={busy}
        onOpenChange={setConfirming}
        title={t('credentialRetirement.confirmTitle')}
        description={t('credentialRetirement.confirmDescription')}
      >
        <div className="space-y-4 text-sm">
          <p>
            {t('credentialRetirement.confirmTarget', {
              source: reviewed.source.name,
              replacement: reviewed.replacement.name,
            })}
          </p>
          <p className="break-all">
            {t('credentialRetirement.confirmEvidence', {
              id: reviewed.evidence?.attempt_id,
              snapshot: reviewed.snapshot_id,
            })}
          </p>
          <p>{t('credentialRetirement.confirmReason', { reason })}</p>
          {stale && <p role="status">{t('credentialRetirement.stale')}</p>}
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setConfirming(false)}>
              {t('credentialRetirement.cancel')}
            </Button>
            <Button disabled={!canSubmit || !session.data} onClick={() => void submit()}>
              {t('credentialRetirement.confirm')}
            </Button>
          </div>
        </div>
      </Dialog>
    </div>
  )
}
