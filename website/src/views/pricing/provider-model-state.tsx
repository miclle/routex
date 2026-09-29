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
  const [supportsImageInput, setSupportsImageInput] = useState(model.supports_image_input)
  const [supportsPdfInput, setSupportsPdfInput] = useState(model.supports_pdf_input)
  const [busy, setBusy] = useState(false)
  const lock = useRef(false)
  const [conflict, setConflict] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [notice, setNotice] = useState('')
  const stale = conflict || reviewed.etag !== model.etag
  const changed =
    enabled !== reviewed.enabled ||
    supportsImageInput !== reviewed.supports_image_input ||
    supportsPdfInput !== reviewed.supports_pdf_input
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
      (!changed && !uncertain) ||
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
        {
          etag: reviewed.etag,
          enabled,
          supports_image_input: supportsImageInput,
          supports_pdf_input: supportsPdfInput,
        },
        session.data.csrf_token,
      )
      setReviewed(result)
      setEnabled(result.enabled)
      setSupportsImageInput(result.supports_image_input)
      setSupportsPdfInput(result.supports_pdf_input)
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
      <dl className="grid gap-3 text-sm sm:grid-cols-3">
        <div>
          <dt className="text-muted-foreground">{t('supplyState')}</dt>
          <dd>{t(model.enabled ? 'supplyEnabled' : 'supplyDisabled')}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('supportsImageInput')}</dt>
          <dd>{t(model.supports_image_input ? 'supported' : 'unsupported')}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('supportsPdfInput')}</dt>
          <dd>{t(model.supports_pdf_input ? 'supported' : 'unsupported')}</dd>
        </div>
      </dl>
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
          <fieldset className="space-y-3">
            <legend className="text-sm font-medium">{t('inputCapabilities')}</legend>
            <label className="flex items-center gap-3 text-sm">
              <Switch
                aria-label={t('supportsImageInput')}
                checked={supportsImageInput}
                onCheckedChange={setSupportsImageInput}
                disabled={busy}
              />
              {t('supportsImageInput')}
            </label>
            <label className="flex items-center gap-3 text-sm">
              <Switch
                aria-label={t('supportsPdfInput')}
                checked={supportsPdfInput}
                onCheckedChange={setSupportsPdfInput}
                disabled={busy}
              />
              {t('supportsPdfInput')}
            </label>
          </fieldset>
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
              disabled={busy || stale || (!changed && !uncertain) || !session.data}
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
