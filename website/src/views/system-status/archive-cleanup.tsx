import { useId, useLayoutEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  cleanupRuntimeArchives,
  previewRuntimeArchiveCleanup,
  validRuntimeArchiveCleanupReason,
} from '@/api/runtime-archive-cleanup'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type {
  RuntimeArchiveCleanupInput,
  RuntimeArchiveCleanupPreview,
} from '@/types/runtime-archive-cleanup'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { locale } from '@/i18n'

type CleanupIntent = { input: RuntimeArchiveCleanupInput; etag: string; bytes: number }

export function ArchiveCleanup({
  authority,
  receiptAuthority,
  selected,
  isCurrent,
  canWrite,
  onLock,
  onComplete,
}: {
  authority: string
  receiptAuthority: string
  selected: string[]
  isCurrent: () => boolean
  canWrite: () => boolean
  onLock: (locked: boolean) => void
  onComplete: () => void
}) {
  const { t } = useTranslation('systemStatus')
  const cache = useQueryClient()
  const [preview, setPreview] = useState<RuntimeArchiveCleanupPreview>()
  const [reason, setReason] = useState('')
  const [invalidReason, setInvalidReason] = useState(false)
  const reasonErrorID = useId()
  const [pending, setPending] = useState(false)
  const [issue, setIssue] = useState<'preview' | 'uncertain' | ''>('')
  const [completed, setCompleted] = useState<number>()
  const turn = useRef(0)
  const [dispatchTurn, setDispatchTurn] = useState(0)
  const [activeIntent, setActiveIntent] = useState<CleanupIntent | null>(null)
  const intent = useRef<CleanupIntent | null>(null)
  const operation = useRef<AbortController | null>(null)
  const lifetime = useRef<object | null>(null)
  const selectedBasis = JSON.stringify([...selected].sort())
  const latest = useRef({
    isCurrent,
    canWrite,
    onLock,
    authority,
    receiptAuthority,
    selectedBasis,
    preview,
    activeIntent,
    reason,
  })
  const reviewedCurrent = useRef<(() => boolean) | null>(null)
  useLayoutEffect(() => {
    const mounted = {}
    lifetime.current = mounted
    setDispatchTurn(turn.current)
    return () => {
      lifetime.current = null
      turn.current++
      operation.current?.abort()
      intent.current = null
      reviewedCurrent.current = null
    }
  }, [])
  useLayoutEffect(() => {
    latest.current = {
      isCurrent,
      canWrite,
      onLock,
      authority,
      receiptAuthority,
      selectedBasis,
      preview,
      activeIntent,
      reason,
    }
  })
  const [renderBasis, setRenderBasis] = useState({ authority, receiptAuthority, selectedBasis })
  if (renderBasis.authority !== authority || renderBasis.receiptAuthority !== receiptAuthority) {
    setRenderBasis({ authority, receiptAuthority, selectedBasis })
    setPending(false)
    setPreview(undefined)
    // A list refresh invalidates another write's review, not an acknowledged
    // historical receipt. Renewed owner/Session authority still clears it.
    if (renderBasis.receiptAuthority !== receiptAuthority) setCompleted(undefined)
    setIssue(activeIntent ? 'uncertain' : '')
  } else if (renderBasis.selectedBasis !== selectedBasis) {
    setRenderBasis({ authority, receiptAuthority, selectedBasis })
    if (!activeIntent) {
      setPreview(undefined)
      setIssue('')
    }
  }
  useLayoutEffect(() => {
    setDispatchTurn(++turn.current)
    operation.current?.abort()
    operation.current = null
    reviewedCurrent.current = null
    latest.current.onLock(intent.current !== null)
  }, [authority, receiptAuthority])
  useLayoutEffect(() => {
    if (!intent.current) {
      reviewedCurrent.current = null
      setDispatchTurn(++turn.current)
    }
  }, [selectedBasis])

  function csrf() {
    if (!latest.current.isCurrent() || !latest.current.canWrite()) return null
    return cache.getQueryData<Session>(sessionKey)?.csrf_token ?? null
  }

  // A queued click cannot authorize another review or a later uncertain retry.
  function ownsCurrentReview() {
    return (
      !!lifetime.current &&
      turn.current === dispatchTurn &&
      latest.current.authority === authority &&
      latest.current.receiptAuthority === receiptAuthority &&
      latest.current.selectedBasis === selectedBasis &&
      latest.current.preview === preview &&
      latest.current.activeIntent === activeIntent &&
      intent.current === activeIntent &&
      (!!activeIntent || latest.current.reason === reason)
    )
  }

  async function review() {
    const token = csrf()
    if (
      !ownsCurrentReview() ||
      operation.current ||
      intent.current ||
      !token ||
      selected.length < 1 ||
      selected.length > 20
    )
      return
    setDispatchTurn(++turn.current)
    const owner = lifetime.current
    const current = isCurrent
    const controller = new AbortController()
    operation.current = controller
    setPending(true)
    setCompleted(undefined)
    setPreview(undefined)
    reviewedCurrent.current = null
    setIssue('')
    onLock(true)
    try {
      const value = await previewRuntimeArchiveCleanup(
        [...selected].sort(),
        token,
        controller.signal,
      )
      if (
        owner !== lifetime.current ||
        operation.current !== controller ||
        !current() ||
        !latest.current.canWrite()
      )
        return
      setPreview(value)
      reviewedCurrent.current = current
    } catch {
      if (owner === lifetime.current && operation.current === controller && current())
        setIssue('preview')
    } finally {
      if (owner === lifetime.current && operation.current === controller) {
        operation.current = null
        setPending(false)
        onLock(false)
      }
    }
  }

  async function submit() {
    const token = csrf()
    if (!ownsCurrentReview() || pending || operation.current || !token) return
    let original = intent.current
    if (!original) {
      if (!preview || !reviewedCurrent.current?.()) return
      if (!validRuntimeArchiveCleanupReason(reason.trim())) {
        setInvalidReason(true)
        return
      }
      setInvalidReason(false)
      original = {
        input: {
          archive_ids: [...preview.archive_ids],
          request_id: crypto.randomUUID(),
          reason: reason.trim(),
        },
        etag: preview.etag,
        bytes: preview.selected_encrypted_bytes,
      }
      intent.current = original
      setActiveIntent(original)
    }
    setDispatchTurn(++turn.current)
    const owner = lifetime.current
    const current = isCurrent
    const controller = new AbortController()
    operation.current = controller
    setPending(true)
    setIssue('')
    onLock(true)
    try {
      const receipt = await cleanupRuntimeArchives(
        original.input,
        original.etag,
        token,
        controller.signal,
      )
      if (owner !== lifetime.current || operation.current !== controller) return
      if (
        !current() ||
        !latest.current.canWrite() ||
        receipt.deleted_encrypted_bytes !== original.bytes
      ) {
        setIssue('uncertain')
        return
      }
      intent.current = null
      setActiveIntent(null)
      reviewedCurrent.current = null
      setPreview(undefined)
      setCompleted(receipt.deleted_count)
      onComplete()
    } catch {
      if (owner === lifetime.current && operation.current === controller) setIssue('uncertain')
    } finally {
      if (owner === lifetime.current && operation.current === controller) {
        operation.current = null
        setPending(false)
        onLock(intent.current !== null)
      }
    }
  }

  const visible = isCurrent() && canWrite()
  const reviewed =
    visible &&
    !!preview &&
    renderBasis.authority === authority &&
    renderBasis.selectedBasis === selectedBasis
  return (
    <div className="mt-4 space-y-3">
      {visible && !activeIntent && (
        <Button
          variant="outline"
          disabled={pending || selected.length < 1 || selected.length > 20}
          onClick={() => void review()}
        >
          {t('archiveCleanupPreview')}
        </Button>
      )}
      {pending && visible && <p role="status">{t('archiveCleanupPending')}</p>}
      {issue === 'preview' && visible && (
        <p role="alert" className="text-destructive">
          {t('archiveCleanupPreviewFailed')}
        </p>
      )}
      {completed !== undefined && visible && (
        <p role="status">{t('archiveCleanupCompleted', { count: completed })}</p>
      )}
      {(reviewed || (visible && activeIntent)) && (
        <div className="space-y-3 rounded-md border border-destructive/30 p-4">
          <p className="font-medium">{t('archiveCleanupTitle')}</p>
          <p className="text-sm text-muted-foreground">{t('archiveCleanupBoundary')}</p>
          <ul className="space-y-1 text-xs font-mono">
            {(activeIntent?.input.archive_ids ?? preview?.archive_ids ?? []).map((id) => (
              <li key={id}>{id}</li>
            ))}
          </ul>
          {preview && !activeIntent && (
            <p className="text-sm">
              {t('archiveCleanupCapacity', {
                count: preview.retained_count,
                bytes: new Intl.NumberFormat(locale()).format(preview.encrypted_bytes),
                selectedBytes: new Intl.NumberFormat(locale()).format(
                  preview.selected_encrypted_bytes,
                ),
              })}
            </p>
          )}
          <label className="block text-sm">
            {t('archiveCleanupReason')}
            <Input
              value={activeIntent?.input.reason ?? reason}
              maxLength={2000}
              disabled={pending || !!activeIntent}
              aria-invalid={invalidReason}
              aria-describedby={invalidReason ? reasonErrorID : undefined}
              onChange={(e) => {
                setInvalidReason(false)
                setReason(e.target.value)
              }}
            />
          </label>
          {invalidReason && (
            <p id={reasonErrorID} role="alert" className="text-destructive">
              {t('archiveCleanupInvalidReason')}
            </p>
          )}
          {issue === 'uncertain' && (
            <p role="alert" className="text-destructive">
              {t('archiveCleanupUncertain')}
            </p>
          )}
          <Button
            className="bg-destructive text-white hover:bg-destructive/90"
            disabled={pending || (!activeIntent && (!reviewed || !reason.trim()))}
            onClick={() => void submit()}
          >
            {t(activeIntent ? 'archiveCleanupRetry' : 'archiveCleanupConfirm')}
          </Button>
        </div>
      )}
    </div>
  )
}
