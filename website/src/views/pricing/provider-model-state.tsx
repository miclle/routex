import { useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import { setProviderModelState } from '@/api/catalog'
import type { ProviderModel } from '@/types/catalog'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { ErrorNotice } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'

export default function ProviderModelState({
  model,
  reload,
}: {
  model: ProviderModel
  reload: () => Promise<ProviderModel | undefined>
}) {
  const { t } = useTranslation('pricing')
  const access = usePermissions()
  const session = useSession()
  const [reviewed, setReviewed] = useState(model)
  const [enabled, setEnabled] = useState(model.enabled)
  const [busy, setBusy] = useState(false)
  const lock = useRef(false)
  const [conflict, setConflict] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [notice, setNotice] = useState('')
  const stale = conflict || reviewed.etag !== model.etag
  async function refresh() {
    if (lock.current) return
    lock.current = true
    setBusy(true)
    setError(null)
    try {
      const current = await reload()
      if (current) {
        setReviewed(current)
        setConflict(false)
        setNotice(uncertain ? 'stateRetry' : 'stateReviewed')
      }
    } catch (error) {
      setError(error)
    } finally {
      lock.current = false
      setBusy(false)
    }
  }
  async function save(event: FormEvent) {
    event.preventDefault()
    if (
      lock.current ||
      stale ||
      (enabled === reviewed.enabled && !uncertain) ||
      !session.data ||
      !access.can('providers.write')
    )
      return
    lock.current = true
    setBusy(true)
    setError(null)
    setNotice('')
    try {
      const result = await setProviderModelState(
        model.id,
        reviewed.etag,
        enabled,
        session.data.csrf_token,
      )
      setReviewed(result)
      setEnabled(result.enabled)
      await reload()
      setUncertain(false)
      setNotice('stateSaved')
    } catch (error) {
      setError(error)
      if (axios.isAxiosError(error) && [409, 503].includes(error.response?.status ?? 0)) {
        setConflict(true)
        setUncertain(error.response?.status === 503)
        setNotice(error.response?.status === 503 ? 'stateUncertain' : 'stateStale')
      }
    } finally {
      lock.current = false
      setBusy(false)
    }
  }
  return (
    <section className="space-y-4 rounded-lg border p-6" aria-label={t('supplyState')}>
      <h3 className="font-semibold">{t('supplyState')}</h3>
      <p className="text-sm text-muted-foreground">{t('supplyStateHelp')}</p>
      <p className="text-sm">{t(model.enabled ? 'supplyEnabled' : 'supplyDisabled')}</p>
      {access.can('providers.write') && (
        <form onSubmit={(event) => void save(event)} className="space-y-4">
          <label className="flex items-center gap-3 text-sm">
            <Switch
              aria-label={t('enableSupply')}
              checked={enabled}
              onCheckedChange={setEnabled}
              disabled={busy}
            />
            {t('enableSupply')}
          </label>
          <ErrorNotice error={error} />
          {stale && (
            <p role="alert" className="text-sm">
              {t('stateStale')}
            </p>
          )}
          {notice && (
            <p role="status" className="text-sm">
              {t(notice)}
            </p>
          )}
          <div className="flex gap-2">
            <Button
              type="submit"
              disabled={
                busy || stale || (enabled === reviewed.enabled && !uncertain) || !session.data
              }
            >
              {t('saveState')}
            </Button>
            {stale && (
              <Button
                variant="outline"
                type="button"
                disabled={busy}
                onClick={() => void refresh()}
              >
                {t('reload')}
              </Button>
            )}
          </div>
        </form>
      )}
    </section>
  )
}
