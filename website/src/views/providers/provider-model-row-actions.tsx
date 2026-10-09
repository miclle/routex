import { useEffect, useEffectEvent, useRef, useState, type Ref } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import { MoreHorizontal } from 'lucide-react'
import { saveProviderModelStatus } from '@/api/provider-model-status'
import type { ProviderModel } from '@/types/catalog'
import type { Session } from '@/types/auth'
import { sessionKey } from '@/hooks/use-auth'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Menu, MenuItem } from '@/components/ui/menu'
import { useConnectionQueryRevision } from './connection-authority'

export function ProviderModelRowMenu({
  providerId,
  model,
  writable,
  readable,
  bindings,
  bindingsFresh,
  onStatus,
  triggerRef,
}: {
  providerId: string
  model: ProviderModel
  writable: () => boolean
  readable: () => boolean
  bindings?: { id: string; name: string | null }[]
  bindingsFresh: () => boolean
  onStatus: () => void
  triggerRef: Ref<HTMLButtonElement>
}) {
  const { t } = useTranslation('catalog')
  const navigate = useNavigate()
  return (
    <Menu
      label={t('providerModels.actionsFor', { name: model.upstream_name })}
      trigger={<MoreHorizontal className="size-4" aria-hidden="true" />}
      triggerClassName="w-10 justify-center"
      side="bottom"
      align="end"
      triggerRef={triggerRef}
    >
      <MenuItem
        onClick={() => {
          if (readable())
            void navigate(
              `/admin/providers/${encodeURIComponent(providerId)}/models/${encodeURIComponent(model.id)}`,
            )
        }}
      >
        {t('providerModels.viewDetails')}
      </MenuItem>
      <MenuItem
        disabled={!writable() || typeof model.enabled !== 'boolean'}
        onClick={() => {
          if (writable()) onStatus()
        }}
      >
        {t(model.enabled ? 'providerModels.disable' : 'providerModels.enable')}
      </MenuItem>
      {bindings?.map((binding) => (
        <MenuItem
          key={binding.id}
          onClick={() => {
            if (bindingsFresh()) void navigate(`/admin/models/${encodeURIComponent(binding.id)}`)
          }}
        >
          {t('providerModels.manageModel', { name: binding.name ?? binding.id })}
        </MenuItem>
      ))}
    </Menu>
  )
}

export function ProviderModelStatusConfirmation({
  actor,
  model,
  open,
  readable,
  writable,
  permissionsKey,
  catalogueKey,
  onClose,
  onActivity,
  isCurrentTarget,
  onSaved,
  refresh,
  finalFocus,
}: {
  actor: string
  model: ProviderModel | undefined
  open: boolean
  readable: () => boolean
  writable: () => boolean
  permissionsKey: readonly unknown[]
  catalogueKey: readonly unknown[]
  onClose: () => void
  onActivity: (activity: { pending: boolean; uncertain: boolean }) => void
  isCurrentTarget: () => boolean
  onSaved: () => void
  refresh: () => void
  finalFocus: () => HTMLButtonElement | false
}) {
  const { t } = useTranslation('catalog')
  const cache = useQueryClient()
  const authority = useConnectionQueryRevision([sessionKey, permissionsKey, catalogueKey])
  const [review, setReview] = useState(model)
  const [enabled, setEnabled] = useState(model ? !model.enabled : false)
  const [intent, setIntent] = useState<{ etag: string; enabled: boolean } | null>(null)
  const submitted = useRef<{ etag: string; enabled: boolean } | null>(null)
  const [conflict, setConflict] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  const [separateReview, setSeparateReview] = useState(false)
  const [abandoned, setAbandoned] = useState(false)
  const pending = useRef<AbortController | null>(null)
  const pendingRevision = useRef('')
  const mounted = useRef(true)
  const current = readable() && !!model
  const release = useEffectEvent(() => {
    if ((!current || authority.snapshot() !== pendingRevision.current) && pending.current) {
      pending.current.abort()
      pending.current = null
      setBusy(false)
      onActivity({ pending: false, uncertain: !!submitted.current })
    }
  })
  useEffect(() => release(), [current, authority.revision])
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      pending.current?.abort()
    }
  }, [])
  const stale = conflict || !review || model?.etag !== review.etag
  const canSave = () =>
    open && current && writable() && !busy && !separateReview && (!!intent || !stale)
  async function dispatch() {
    if (
      !mounted.current ||
      !isCurrentTarget() ||
      !canSave() ||
      pending.current ||
      submitted.current !== intent ||
      !review ||
      !model
    )
      return
    const csrf = cache.getQueryData<Session>(sessionKey)?.csrf_token
    if (!csrf || cache.getQueryData<Session>(sessionKey)?.user.id !== actor) return
    const captured = intent ?? { etag: review.etag, enabled }
    const wasUncertain = !!intent
    const controller = new AbortController()
    const revision = authority.snapshot()
    pendingRevision.current = revision
    pending.current = controller
    submitted.current = captured
    // Publish the lifetime lock before starting I/O. React state/effects alone
    // cannot guard captured close/retarget callbacks in this same event turn.
    onActivity({ pending: true, uncertain: true })
    setIntent(captured)
    setBusy(true)
    setNotice('providerModels.uncertain')
    const valid = () =>
      mounted.current &&
      pending.current === controller &&
      !controller.signal.aborted &&
      authority.snapshot() === revision &&
      isCurrentTarget() &&
      readable() &&
      writable()
    try {
      await saveProviderModelStatus(model.id, captured, csrf, controller.signal)
      if (!valid()) return
      submitted.current = null
      setIntent(null)
      setNotice(null)
      onSaved()
    } catch (error) {
      if (!valid()) return
      if (!wasUncertain && axios.isAxiosError(error) && error.response?.status === 409) {
        submitted.current = null
        setIntent(null)
        setConflict(true)
        setNotice('providerModels.stale')
        refresh()
      }
      // A failed retry never proves the original dispatched operation failed.
    } finally {
      if (mounted.current && pending.current === controller) {
        pending.current = null
        setBusy(false)
        onActivity({ pending: false, uncertain: !!submitted.current })
      }
    }
  }
  return (
    <Dialog
      open={open && current}
      busy={busy}
      finalFocus={finalFocus}
      onOpenChange={(value) => {
        if (!value && !pending.current) {
          if (separateReview) setSeparateReview(false)
          else onClose()
        }
      }}
      title={t(
        separateReview
          ? 'providerModels.discardTitle'
          : enabled
            ? 'providerModels.confirmEnable'
            : 'providerModels.confirmDisable',
      )}
      description={t(
        separateReview
          ? 'providerModels.discardHelp'
          : enabled
            ? 'providerModels.enableHelp'
            : 'providerModels.disableHelp',
        {
          name: review?.upstream_name ?? model?.id,
        },
      )}
    >
      <p>
        {t('providerModels.currentStatus', {
          status: t(model?.enabled ? 'providers.enabled' : 'common.disabled'),
        })}
      </p>
      <p className="text-sm text-muted-foreground">{t('providerModels.statusOnly')}</p>
      {abandoned && <p role="alert">{t('providerModels.abandoned')}</p>}
      {!intent && stale && <p role="alert">{t('providerModels.stale')}</p>}
      {notice && <p role="status">{t(notice)}</p>}
      {!writable() && <p role="alert">{t('providerModels.writeUnavailable')}</p>}
      <div className="mt-6 flex justify-end gap-2">
        <Button
          variant="outline"
          disabled={busy}
          onClick={() => {
            if (pending.current || !isCurrentTarget()) return
            if (separateReview) setSeparateReview(false)
            else onClose()
          }}
        >
          {t('common.cancel')}
        </Button>
        {separateReview ? (
          <Button
            disabled={!current || busy || !intent}
            onClick={() => {
              if (
                !isCurrentTarget() ||
                !readable() ||
                !model ||
                busy ||
                pending.current ||
                !intent ||
                submitted.current !== intent
              )
                return
              submitted.current = null
              onActivity({ pending: false, uncertain: false })
              setIntent(null)
              setReview(model)
              setEnabled(!model.enabled)
              setConflict(false)
              setSeparateReview(false)
              setAbandoned(true)
              setNotice('providerModels.reviewed')
            }}
          >
            {t('providerModels.confirmDiscard')}
          </Button>
        ) : (
          <>
            {intent && (
              <Button
                variant="outline"
                disabled={!current || busy}
                onClick={() => {
                  if (
                    !isCurrentTarget() ||
                    !readable() ||
                    busy ||
                    pending.current ||
                    !intent ||
                    submitted.current !== intent
                  )
                    return
                  setSeparateReview(true)
                  refresh()
                }}
              >
                {t('providerModels.reviewSeparate')}
              </Button>
            )}
            {!intent && stale && (
              <Button
                variant="outline"
                disabled={!current || !writable() || busy}
                onClick={() => {
                  if (
                    !isCurrentTarget() ||
                    !readable() ||
                    !writable() ||
                    busy ||
                    pending.current ||
                    submitted.current
                  )
                    return
                  setReview(model)
                  setConflict(false)
                  setNotice('providerModels.reviewed')
                }}
              >
                {t('providerModels.reviewCurrent')}
              </Button>
            )}
            <Button disabled={!canSave()} onClick={() => void dispatch()}>
              {t(
                intent
                  ? 'providerModels.retryStatus'
                  : enabled
                    ? 'providerModels.confirmEnableAction'
                    : 'providerModels.confirmDisableAction',
              )}
            </Button>
          </>
        )}
      </div>
    </Dialog>
  )
}
