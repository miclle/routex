import { useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { validateRuntimeArchive } from '@/api/runtime-archives'
import { Button } from '@/components/ui/button'
import { locale } from '@/i18n'
import type { RuntimeArchiveValidation } from '@/types/runtime-archives'

export function ArchiveValidation({
  actorID,
  authority,
  archiveID,
  archiveBirth,
  isCurrent,
}: {
  actorID: string
  authority: string
  archiveID: string
  archiveBirth: string
  isCurrent: () => boolean
}) {
  const { t } = useTranslation('systemStatus')
  const basis = JSON.stringify([actorID, authority, archiveID, archiveBirth])
  const owner = useRef<{ basis: string; controller: AbortController } | null>(null)
  const [view, setView] = useState<{
    basis: string
    pending: boolean
    facts: RuntimeArchiveValidation | null
    failed: boolean
  }>({ basis, pending: false, facts: null, failed: false })
  if (view.basis !== basis) setView({ basis, pending: false, facts: null, failed: false })
  const pending = view.basis === basis && view.pending
  useLayoutEffect(() => {
    owner.current?.controller.abort()
    owner.current = null
    return () => {
      owner.current?.controller.abort()
      owner.current = null
    }
  }, [basis])
  async function read() {
    if (!isCurrent() || owner.current) return
    const op = { basis, controller: new AbortController() }
    owner.current = op
    setView({ basis, pending: true, facts: null, failed: false })
    const current = () => owner.current === op && !op.controller.signal.aborted && isCurrent()
    try {
      const result = await validateRuntimeArchive(archiveID, op.controller.signal)
      if (!current()) return
      if (result.archive_created_at !== archiveBirth) throw new Error('Archive identity changed')
      setView({ basis, pending: true, facts: result, failed: false })
    } catch {
      if (current()) setView({ basis, pending: true, facts: null, failed: true })
    } finally {
      if (owner.current === op) {
        owner.current = null
        setView((latest) => (latest.basis === basis ? { ...latest, pending: false } : latest))
      }
    }
  }
  const result = isCurrent() && view.basis === basis ? view.facts : null
  return (
    <div className="mt-2 space-y-1 font-sans text-sm">
      <Button
        size="sm"
        variant="outline"
        disabled={pending || !isCurrent()}
        onClick={() => void read()}
        aria-label={t('archiveValidateIdentity', { id: archiveID })}
      >
        {pending ? t('archiveValidating') : t('archiveValidate')}
      </Button>
      {result && (
        <p role="status">
          {t('archiveValidated', {
            time: new Intl.DateTimeFormat(locale(), {
              dateStyle: 'medium',
              timeStyle: 'short',
            }).format(new Date(result.observed_at)),
          })}
        </p>
      )}
      {view.basis === basis && view.failed && isCurrent() && (
        <p role="alert" className="text-destructive">
          {t('archiveValidationFailed')}
        </p>
      )}
    </div>
  )
}
