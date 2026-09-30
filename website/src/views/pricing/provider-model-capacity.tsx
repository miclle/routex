import { useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import { getProviderModelCapacity, saveProviderModelCapacity } from '@/api/provider-model-capacity'
import type {
  ProviderModelCapacity,
  ProviderModelCapacityInput,
} from '@/types/provider-model-capacity'
import { usePermissions } from '@/hooks/use-permissions'
import { useSession } from '@/hooks/use-auth'
import { ErrorNotice, FormField, QueryState } from '@/components/app/CatalogUI'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'

export default function ProviderModelCapacity({ modelId }: { modelId: string }) {
  return <CapacityCard key={modelId} modelId={modelId} />
}

function CapacityCard({ modelId }: { modelId: string }) {
  const { t, i18n } = useTranslation('pricing')
  const access = usePermissions()
  const cache = useQueryClient()
  const queryKey = ['admin', 'provider-model-capacity', modelId]
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => getProviderModelCapacity(modelId, signal),
    enabled: access.can('providers.read'),
  })
  const [draft, setDraft] = useState<ProviderModelCapacity>()
  const [open, setOpen] = useState(false)
  const [saved, setSaved] = useState(false)
  if (!access.can('providers.read')) return null
  const record = query.data
  return (
    <section className="space-y-4 rounded-lg border p-6" aria-label={t('capacityTitle')}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="font-semibold">{t('capacityTitle')}</h3>
        {record && access.can('providers.write') && (
          <Button
            variant="outline"
            onClick={() => {
              if (!draft) setDraft(record)
              setSaved(false)
              setOpen(true)
            }}
          >
            {t('capacityEdit')}
          </Button>
        )}
      </div>
      <p className="text-sm text-muted-foreground">{t('capacityHelp')}</p>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={() => void query.refetch()}
      />
      {record && (
        <>
          <Badge variant="outline">
            {t(record.configured ? 'capacityConfigured' : 'capacityUnconfigured')}
          </Badge>
          {!record.configured && (
            <p className="text-sm">{t('capacityProtocol', { protocol: record.protocol })}</p>
          )}
          {record.configured && (
            <dl className="grid gap-4 text-sm sm:grid-cols-3">
              {[
                ['capacityInput', record.max_input_tokens.toLocaleString(i18n.language)],
                ['capacityOutput', record.max_output_tokens.toLocaleString(i18n.language)],
                ['protocol', record.protocol],
                ['capacityEvidence', record.evidence],
                ['capacityRevision', record.etag],
                ['capacityUpdated', new Date(record.updated_at).toLocaleString(i18n.language)],
              ].map(([label, value]) => (
                <div key={label}>
                  <dt className="text-muted-foreground">{t(label)}</dt>
                  <dd className="mt-1 break-words whitespace-pre-wrap">{value}</dd>
                </div>
              ))}
            </dl>
          )}
          <p className="text-sm text-muted-foreground">{t('capacityValidityHelp')}</p>
        </>
      )}
      {saved && (
        <p role="status" className="text-sm">
          {t('capacitySaved')}
        </p>
      )}
      {draft && record && access.can('providers.write') && (
        <CapacityEditor
          initial={draft}
          current={record}
          open={open}
          onOpenChange={setOpen}
          reload={async () => {
            const result = await query.refetch()
            if (result.isError) throw result.error
            return result.data
          }}
          onSaved={(result) => {
            cache.setQueryData(queryKey, result)
            setOpen(false)
            setDraft(undefined)
            setSaved(true)
          }}
        />
      )}
    </section>
  )
}

function CapacityEditor({
  initial,
  current,
  open,
  onOpenChange,
  reload,
  onSaved,
}: {
  initial: ProviderModelCapacity
  current: ProviderModelCapacity
  open: boolean
  onOpenChange: (open: boolean) => void
  reload: () => Promise<ProviderModelCapacity | undefined>
  onSaved: (result: ProviderModelCapacity) => void
}) {
  const { t } = useTranslation('pricing')
  const session = useSession()
  const access = usePermissions()
  const [reviewed, setReviewed] = useState(initial)
  const [input, setInput] = useState(initial.configured ? String(initial.max_input_tokens) : '')
  const [output, setOutput] = useState(initial.configured ? String(initial.max_output_tokens) : '')
  const [evidence, setEvidence] = useState(initial.evidence)
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const lock = useRef(false)
  const intent = useRef<{ etag: string; body: ProviderModelCapacityInput } | undefined>(undefined)
  const [conflict, setConflict] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [notice, setNotice] = useState('')
  const stale = conflict || current.etag !== reviewed.etag
  const locked = busy || uncertain

  async function review() {
    if (lock.current) return
    lock.current = true
    setBusy(true)
    setError(null)
    try {
      const result = await reload()
      if (result) {
        setReviewed(result)
        setConflict(false)
        setUncertain(false)
        intent.current = undefined
        setNotice('capacityReviewed')
      }
    } catch (error) {
      setError(error)
    } finally {
      lock.current = false
      setBusy(false)
    }
  }

  async function submit(event?: FormEvent, retry = false) {
    event?.preventDefault()
    if (lock.current || !session.data || !access.can('providers.write')) return
    if (retry ? !uncertain || !intent.current : stale || uncertain) return
    let captured = intent.current
    if (!retry) {
      const parse = (value: string) =>
        /^\d+$/.test(value) && Number.isSafeInteger(Number(value)) && Number(value) > 0
      if (!parse(input) || !parse(output)) {
        setNotice('capacityNumberError')
        return
      }
      const trimmedEvidence = evidence.trim()
      const trimmedReason = reason.trim()
      if (
        [trimmedEvidence, trimmedReason].some(
          (value) => !value || new TextEncoder().encode(value).length > 2000,
        )
      ) {
        setNotice('capacityTextError')
        return
      }
      captured = {
        etag: reviewed.etag,
        body: {
          max_input_tokens: Number(input),
          max_output_tokens: Number(output),
          evidence: trimmedEvidence,
          reason: trimmedReason,
        },
      }
      intent.current = captured
    }
    if (!captured) return
    lock.current = true
    setBusy(true)
    setError(null)
    setNotice('')
    try {
      const result = await saveProviderModelCapacity(
        initial.provider_model_id,
        captured.etag,
        captured.body,
        session.data.csrf_token,
      )
      onSaved(result)
    } catch (error) {
      setError(error)
      const status = axios.isAxiosError(error) ? error.response?.status : undefined
      if (status === 409) {
        setConflict(true)
        setUncertain(false)
        intent.current = undefined
        setNotice('capacityStale')
      } else if (!status || status >= 500) {
        setUncertain(true)
        setNotice('capacityUncertain')
      } else {
        if (!retry) intent.current = undefined
        else setNotice('capacityUncertain')
      }
    } finally {
      lock.current = false
      setBusy(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('capacityEdit')}
      description={t('capacityEditorHelp')}
      busy={busy}
      width={640}
    >
      <form className="space-y-4" onSubmit={(event) => void submit(event)}>
        <p className="text-sm text-muted-foreground">
          {t('capacityReviewedRevision', { revision: reviewed.etag, protocol: reviewed.protocol })}
        </p>
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField label={t('capacityInput')}>
            <Input
              aria-label={t('capacityInput')}
              inputMode="numeric"
              autoComplete="off"
              value={input}
              onChange={(event) => setInput(event.target.value)}
              disabled={locked}
            />
          </FormField>
          <FormField label={t('capacityOutput')}>
            <Input
              aria-label={t('capacityOutput')}
              inputMode="numeric"
              autoComplete="off"
              value={output}
              onChange={(event) => setOutput(event.target.value)}
              disabled={locked}
            />
          </FormField>
        </div>
        <p className="text-sm text-muted-foreground">{t('capacityTokenHelp')}</p>
        <FormField label={t('capacityEvidence')}>
          <Input
            aria-label={t('capacityEvidence')}
            autoComplete="off"
            value={evidence}
            onChange={(event) => setEvidence(event.target.value)}
            disabled={locked}
          />
        </FormField>
        <FormField label={t('capacityReason')}>
          <Input
            aria-label={t('capacityReason')}
            autoComplete="off"
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            disabled={locked}
          />
        </FormField>
        <ErrorNotice error={error} />
        {stale && !uncertain && (
          <p role="alert" className="text-sm">
            {t('capacityStale')}
          </p>
        )}
        {notice && (
          <p role="status" className="text-sm">
            {t(notice)}
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={busy || stale || uncertain || !session.data}>
            {t('capacitySave')}
          </Button>
          {uncertain && (
            <Button
              type="button"
              disabled={busy || !session.data}
              onClick={() => void submit(undefined, true)}
            >
              {t('capacityRetry')}
            </Button>
          )}
          {(stale || uncertain) && (
            <Button type="button" variant="outline" disabled={busy} onClick={() => void review()}>
              {t('capacityReview')}
            </Button>
          )}
          <Button
            type="button"
            variant="outline"
            disabled={busy}
            onClick={() => onOpenChange(false)}
          >
            {t('cancel')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
