import { useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import { Database, KeyRound } from 'lucide-react'
import { getProviderStoragePolicy, saveProviderStoragePolicy } from '@/api/provider-storage'
import type { ProviderStorageInput, ProviderStoragePolicy } from '@/types/provider-storage'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { FormField } from '@/components/app/CatalogUI'
import { useConnectionQueryRevision } from '@/views/providers/connection-authority'

export default function ProviderStoragePolicyEditor({
  actor,
  generation,
  permissionKey,
}: {
  actor: string
  generation: number
  permissionKey: readonly unknown[]
}) {
  const { t } = useTranslation('secrets')
  const cache = useQueryClient()
  const key = ['admin', 'provider-storage-policy', actor, generation] as const
  function authorized(write = false) {
    const session = cache.getQueryState<Session>(sessionKey)
    const permissions = cache.getQueryState<string[]>(permissionKey)
    return (
      session?.data?.user.id === actor &&
      session.data.user.role === 'admin' &&
      session.status === 'success' &&
      session.fetchStatus === 'idle' &&
      !session.isInvalidated &&
      permissions?.status === 'success' &&
      permissions.fetchStatus === 'idle' &&
      !permissions.isInvalidated &&
      permissions.data?.includes('secrets.read') === true &&
      (!write || permissions.data.includes('secrets.write'))
    )
  }
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getProviderStoragePolicy(signal),
    enabled: authorized(),
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const observed = useConnectionQueryRevision([sessionKey, permissionKey, key])
  function ready(write = false) {
    const state = cache.getQueryState<ProviderStoragePolicy>(key)
    return (
      authorized(write) &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      (!write || state.data?.can_edit === true)
    )
  }
  const view = ready() ? query.data : undefined
  const [open, setOpen] = useState(false)
  const [reviewed, setReviewed] = useState<ProviderStoragePolicy>()
  const [mode, setMode] = useState<'inline' | 'vault'>('inline')
  const [choice, setChoice] = useState('')
  const [reason, setReason] = useState('')
  const [confirm, setConfirm] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const intent = useRef<{ etag: string; body: ProviderStorageInput } | undefined>(undefined)
  const locked = useRef(false)
  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      intent.current = undefined
    }
  }, [])
  const stale = !!view && reviewed?.etag !== view.etag
  const selectable = reviewed?.choices.find((item) => `${item.id}:${item.revision_id}` === choice)
  const currentChoice = view?.choices.find((item) => `${item.id}:${item.revision_id}` === choice)
  function start(next: 'inline' | 'vault') {
    if (!view || !ready(true) || busy) return
    if (!uncertain) {
      setReviewed(view)
      setMode(next)
      setChoice(
        view.integration_id && view.revision_id ? `${view.integration_id}:${view.revision_id}` : '',
      )
      setReason('')
      setNotice('')
      setConfirm(false)
    }
    setOpen(true)
  }
  function dismiss() {
    if (busy) return
    intent.current = undefined
    setUncertain(false)
    setConfirm(false)
    setOpen(false)
    setReason('')
  }
  async function review() {
    if (busy || uncertain || !authorized()) return
    setBusy(true)
    try {
      const result = await query.refetch()
      if (!alive.current || result.isError || !ready()) throw new Error('Review unavailable')
      setReviewed(result.data)
      setConfirm(false)
      setNotice('providerStorage.reviewed')
    } catch {
      if (alive.current) setNotice('providerStorage.failed')
    } finally {
      if (alive.current) setBusy(false)
    }
  }
  function prepare() {
    if (!ready(true) || stale || !reviewed || uncertain || busy) return
    if (
      !reason ||
      /^\p{White_Space}|\p{White_Space}$/u.test(reason) ||
      /[\p{Cc}\p{Cs}]/u.test(reason) ||
      [...reason].length > 1000 ||
      (mode === 'vault' && (!selectable || !currentChoice))
    ) {
      setNotice('providerStorage.invalid')
      return
    }
    intent.current = {
      etag: reviewed.etag,
      body: {
        mode,
        integration_id: mode === 'vault' ? selectable!.id : null,
        revision_id: mode === 'vault' ? selectable!.revision_id : null,
        reason,
      },
    }
    setConfirm(true)
    setNotice('')
  }
  async function dispatch(retry = false) {
    if (
      locked.current ||
      !ready(true) ||
      !intent.current ||
      (retry ? !uncertain : !confirm || stale)
    )
      return
    if (
      !retry &&
      intent.current.body.mode === 'vault' &&
      !query.data?.choices.some(
        (item) =>
          item.id === intent.current?.body.integration_id &&
          item.revision_id === intent.current?.body.revision_id,
      )
    )
      return
    locked.current = true
    setBusy(true)
    setNotice('')
    const captured = intent.current,
      stamp = observed.snapshot()
    try {
      const session = cache.getQueryData<Session>(sessionKey)!
      const result = await saveProviderStoragePolicy(
        captured.body,
        captured.etag,
        session.csrf_token,
      )
      if (!alive.current) return
      if (
        !ready(true) ||
        observed.snapshot() !== stamp ||
        result.mode !== captured.body.mode ||
        result.integration_id !== captured.body.integration_id ||
        result.revision_id !== captured.body.revision_id
      )
        throw new Error('Authority renewed')
      intent.current = undefined
      setUncertain(false)
      setConfirm(false)
      setOpen(false)
      setReason('')
      setNotice('providerStorage.saved')
      void query.refetch()
    } catch (error) {
      if (!alive.current) return
      const status = axios.isAxiosError(error) ? error.response?.status : undefined
      setConfirm(false)
      if (retry || !status || status >= 500 || !ready(true) || observed.snapshot() !== stamp) {
        setUncertain(true)
        setNotice('providerStorage.uncertain')
      } else {
        intent.current = undefined
        setReviewed(undefined)
        setNotice(status === 409 ? 'providerStorage.stale' : 'providerStorage.failed')
      }
    } finally {
      locked.current = false
      if (alive.current) setBusy(false)
    }
  }
  return (
    <div className="space-y-4">
      {!view ? (
        <div className="space-y-3">
          <p role="status">
            {t(query.error ? 'providerStorage.loadError' : 'providerStorage.loading')}
          </p>
          {query.error && (
            <Button
              variant="outline"
              disabled={!authorized() || query.isFetching}
              onClick={() => void query.refetch()}
            >
              {t('providerStorage.review')}
            </Button>
          )}
        </div>
      ) : (
        <>
          {(['inline', 'vault'] as const).map((option) => (
            <Card key={option} className="p-5">
              <div className="flex items-start gap-4">
                {option === 'inline' ? (
                  <KeyRound className="mt-1 size-5 shrink-0" aria-hidden="true" />
                ) : (
                  <Database className="mt-1 size-5 shrink-0" aria-hidden="true" />
                )}
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h2 className="font-semibold">{t(`providerStorage.${option}`)}</h2>
                    {view.mode === option && (
                      <Badge variant="outline">{t('providerStorage.current')}</Badge>
                    )}
                  </div>
                  <p className="mt-1 text-sm text-muted-foreground">
                    {t(`providerStorage.${option}Description`)}
                  </p>
                  <p className="mt-2 text-xs text-muted-foreground">
                    {t(
                      option === 'inline'
                        ? 'providerStorage.inlineLocation'
                        : 'providerStorage.vaultLocation',
                    )}
                  </p>
                </div>
                <Button
                  variant="outline"
                  disabled={!ready(true) || busy}
                  onClick={() => start(option)}
                >
                  {t(
                    uncertain
                      ? 'providerStorage.original'
                      : view.mode === option
                        ? 'providerStorage.configure'
                        : 'providerStorage.switch',
                  )}
                </Button>
              </div>
            </Card>
          ))}
          <p className="text-xs text-muted-foreground">{t('providerStorage.futureOnly')}</p>
        </>
      )}
      {notice && !open && <p role="status">{t(notice)}</p>}
      <Dialog
        open={open}
        width={560}
        busy={busy}
        onOpenChange={(value) => {
          if (!value) dismiss()
        }}
        title={t('providerStorage.title')}
        description={t('providerStorage.futureOnly')}
      >
        {!view ? (
          <p role="status">{t('providerStorage.loading')}</p>
        ) : (
          <div className="space-y-5">
            {!confirm && (
              <fieldset disabled={!ready(true) || busy || uncertain} className="space-y-5">
                <FormField label={t('providerStorage.mode')}>
                  <select
                    value={mode}
                    onChange={(event) => setMode(event.target.value as 'inline' | 'vault')}
                    className="h-10 w-full rounded-md border bg-background px-3"
                  >
                    <option value="inline">{t('providerStorage.inline')}</option>
                    <option value="vault">{t('providerStorage.vault')}</option>
                  </select>
                </FormField>
                {mode === 'vault' && (
                  <FormField label={t('providerStorage.integration')}>
                    <select
                      value={choice}
                      onChange={(event) => setChoice(event.target.value)}
                      className="h-10 w-full rounded-md border bg-background px-3"
                    >
                      <option value="">{t('providerStorage.choose')}</option>
                      {reviewed?.choices.map((item) => (
                        <option key={item.id} value={`${item.id}:${item.revision_id}`}>
                          {item.name} · {item.id} · {item.revision_id}
                        </option>
                      ))}
                    </select>
                    {!currentChoice && (
                      <p className="mt-2 text-xs">{t('providerStorage.ineligible')}</p>
                    )}
                  </FormField>
                )}
                <FormField label={t('providerStorage.reason')}>
                  <Input
                    autoComplete="off"
                    value={reason}
                    onChange={(event) => setReason(event.target.value)}
                  />
                </FormField>
              </fieldset>
            )}
            {confirm && (
              <p>
                {t('providerStorage.confirm', {
                  mode: t(`providerStorage.${mode}`),
                })}
              </p>
            )}
            {stale && !uncertain && <p role="alert">{t('providerStorage.stale')}</p>}
            {notice && <p role="status">{t(notice)}</p>}
            <div className="flex flex-wrap justify-end gap-2">
              <Button variant="outline" disabled={busy} onClick={dismiss}>
                {t('providerStorage.cancel')}
              </Button>
              {!uncertain && (stale || !reviewed) && (
                <Button variant="outline" disabled={busy || !ready()} onClick={() => void review()}>
                  {t('providerStorage.review')}
                </Button>
              )}
              {uncertain ? (
                <Button disabled={busy || !ready(true)} onClick={() => void dispatch(true)}>
                  {t('providerStorage.retry')}
                </Button>
              ) : confirm ? (
                <Button disabled={busy || !ready(true) || stale} onClick={() => void dispatch()}>
                  {t('providerStorage.save')}
                </Button>
              ) : (
                <Button
                  disabled={
                    busy ||
                    !ready(true) ||
                    stale ||
                    !reviewed ||
                    (mode === 'vault' && (!selectable || !currentChoice))
                  }
                  onClick={prepare}
                >
                  {t('providerStorage.continue')}
                </Button>
              )}
            </div>
          </div>
        )}
      </Dialog>
    </div>
  )
}
