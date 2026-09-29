import { useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { TestTubeDiagonal } from 'lucide-react'
import { rollbackStorage, saveStorage, StorageError, testStorage } from '@/api/storage'
import type { StorageInput, StorageProbe, StorageRevision, StorageSettings } from '@/types/storage'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Drawer } from '@/components/ui/drawer'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { StorageProbeResult } from './result'
import { StorageRevisions } from './revisions'

type ErrorState =
  | ''
  | 'conflict'
  | 'uncertain'
  | 'verificationFailed'
  | 'invalid'
  | 'requestFailed'
  | 'reviewFailed'

function initialDraft(settings: StorageSettings): StorageInput {
  const revision = settings.revision
  return {
    enabled: settings.enabled,
    endpoint: revision?.endpoint ?? '',
    region: revision?.region ?? '',
    bucket: revision?.bucket ?? '',
    prefix: revision?.prefix ?? '',
    auth: { action: revision?.credentials_configured ? 'keep' : 'replace' },
    etag: settings.etag,
  }
}

function normalizedEndpoint(value: string) {
  return value.trim().replace(/\/+$/, '').toLowerCase()
}

function validDraft(input: StorageInput) {
  try {
    const endpoint = new URL(input.endpoint.trim())
    if (endpoint.protocol !== 'http:' && endpoint.protocol !== 'https:') return false
  } catch {
    return false
  }
  return (
    !!input.region.trim() &&
    !!input.bucket.trim() &&
    input.bucket.trim().length <= 63 &&
    input.prefix.trim().length <= 512 &&
    (input.auth.action !== 'replace' || (!!input.auth.access_key && !!input.auth.secret_key))
  )
}

export function StorageConfiguration({
  initial,
  refresh,
  onClose,
  onSaved,
}: {
  initial: StorageSettings
  refresh: () => Promise<StorageSettings | undefined>
  onClose: () => void
  onSaved: (next: StorageSettings, message: string, close: boolean) => Promise<void>
}) {
  const { t } = useTranslation('storage')
  const access = usePermissions()
  const session = useSession()
  const [baseline, setBaseline] = useState(initial)
  const [draft, setDraft] = useState(() => initialDraft(initial))
  const [busy, setBusy] = useState(false)
  const [operation, setOperation] = useState<'save' | 'test' | 'review' | 'rollback' | ''>('')
  const [error, setError] = useState<ErrorState>('')
  const [notice, setNotice] = useState('')
  const [probe, setProbe] = useState<StorageProbe | null>(null)
  const lock = useRef(false)
  const revision = baseline.revision
  const endpointChanged =
    normalizedEndpoint(draft.endpoint) !== normalizedEndpoint(revision?.endpoint ?? '')
  const authBlocked =
    !!revision?.credentials_configured && endpointChanged && draft.auth.action === 'keep'
  const missingAuthentication =
    draft.enabled &&
    (draft.auth.action === 'remove' ||
      (!revision?.credentials_configured && draft.auth.action !== 'replace'))
  const dirty =
    JSON.stringify({ ...draft, etag: baseline.etag }) !== JSON.stringify(initialDraft(baseline))
  const needsReview = error === 'conflict' || error === 'uncertain' || error === 'reviewFailed'
  const canSave =
    access.can('storage.write') &&
    dirty &&
    validDraft(draft) &&
    !authBlocked &&
    !missingAuthentication &&
    !needsReview

  function change(next: Partial<StorageInput>) {
    setDraft((current) => ({ ...current, ...next }))
    setNotice('')
    setProbe(null)
    if (!needsReview) setError('')
  }

  async function authoritative() {
    const current = await refresh()
    if (current) setBaseline(current)
    return current
  }

  function status(failure: unknown): ErrorState {
    const code = failure instanceof StorageError ? failure.status : 0
    return code === 409
      ? 'conflict'
      : code === 422
        ? 'verificationFailed'
        : code === 400
          ? 'invalid'
          : code === 0 || code >= 500
            ? 'uncertain'
            : 'requestFailed'
  }

  async function save(event: FormEvent) {
    event.preventDefault()
    if (lock.current || !session.data || !canSave) return
    lock.current = true
    setBusy(true)
    setOperation('save')
    setError('')
    setNotice('')
    try {
      const input: StorageInput = {
        ...draft,
        endpoint: draft.endpoint.trim().replace(/\/+$/, ''),
        region: draft.region.trim(),
        bucket: draft.bucket.trim(),
        prefix: draft.prefix.trim(),
        etag: baseline.etag,
      }
      const result = await saveStorage(input, session.data.csrf_token)
      setDraft((current) => ({ ...current, auth: { action: 'keep' } }))
      await onSaved(result, 'saved', true)
    } catch (failure) {
      const next = status(failure)
      setError(next)
      if (next === 'verificationFailed') {
        await authoritative().catch(() => undefined)
        setNotice('activePreserved')
      }
    } finally {
      lock.current = false
      setBusy(false)
      setOperation('')
    }
  }

  async function review() {
    if (lock.current) return
    lock.current = true
    setBusy(true)
    setOperation('review')
    try {
      const current = await authoritative()
      if (current) {
        setError('')
        setNotice('currentRevision')
      }
    } catch {
      setError('reviewFailed')
    } finally {
      lock.current = false
      setBusy(false)
      setOperation('')
    }
  }

  async function runTest() {
    if (
      lock.current ||
      !session.data ||
      !access.can('storage.test') ||
      !baseline.revision ||
      dirty ||
      needsReview
    )
      return
    lock.current = true
    setBusy(true)
    setOperation('test')
    setError('')
    setNotice('')
    setProbe(null)
    try {
      const result = await testStorage(baseline.etag, session.data.csrf_token)
      setProbe(result)
    } catch (failure) {
      setError(status(failure))
    } finally {
      lock.current = false
      setBusy(false)
      setOperation('')
    }
  }

  async function confirmRollback(revision: StorageRevision, enabled: boolean) {
    if (lock.current || !session.data || !access.can('storage.write')) return
    lock.current = true
    setBusy(true)
    setOperation('rollback')
    setError('')
    setNotice('')
    try {
      const result = await rollbackStorage(
        { revision_id: revision.id, etag: baseline.etag, enabled },
        session.data.csrf_token,
      )
      const current = (await authoritative()) ?? result
      setBaseline(current)
      setDraft(initialDraft(current))
      setProbe(null)
      await onSaved(current, 'rollbackSuccess', false)
      setNotice('rollbackSuccess')
    } catch (failure) {
      const next = status(failure)
      setError(next)
      if (next === 'verificationFailed') {
        await authoritative().catch(() => undefined)
        setNotice('activePreserved')
      }
    } finally {
      lock.current = false
      setBusy(false)
      setOperation('')
    }
  }

  return (
    <Drawer
      open
      width={760}
      title={t('configureTitle')}
      description={t('configureDescription')}
      busy={busy}
      onOpenChange={(value) => {
        if (!value) onClose()
      }}
    >
      <div className="space-y-8">
        <form aria-label={t('configureTitle')} className="space-y-5" onSubmit={save}>
          <fieldset disabled={busy || !access.can('storage.write')} className="space-y-5">
            <div className="flex items-center justify-between gap-4">
              <div>
                <p className="text-sm font-medium">{t('enable')}</p>
                <p className="mt-1 text-xs text-muted-foreground">{t('enableHelp')}</p>
              </div>
              <Switch
                checked={draft.enabled}
                onCheckedChange={(enabled) => change({ enabled })}
                aria-label={t('enable')}
              />
            </div>
            <FormField label={t('endpoint')}>
              <Input
                name="storage_endpoint"
                type="url"
                required
                value={draft.endpoint}
                placeholder="https://s3.example.invalid"
                onValueChange={(endpoint) => change({ endpoint })}
              />
            </FormField>
            <p className="-mt-3 text-xs text-muted-foreground">{t('endpointHelp')}</p>
            <FormField label={t('region')}>
              <Input
                name="storage_region"
                required
                maxLength={64}
                value={draft.region}
                placeholder="us-east-1"
                onValueChange={(region) => change({ region })}
              />
            </FormField>
            <FormField label={t('bucket')}>
              <Input
                name="storage_bucket"
                required
                maxLength={63}
                value={draft.bucket}
                placeholder="routex-files"
                onValueChange={(bucket) => change({ bucket })}
              />
            </FormField>
            <FormField label={t('prefix')}>
              <Input
                name="storage_prefix"
                maxLength={512}
                value={draft.prefix}
                placeholder="routex/"
                onValueChange={(prefix) => change({ prefix })}
              />
            </FormField>
            <p className="-mt-3 text-xs text-muted-foreground">{t('prefixHelp')}</p>
            <FormField label={t('auth')}>
              <select
                name="auth_action"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={draft.auth.action}
                onChange={(event) =>
                  change({ auth: { action: event.target.value as StorageInput['auth']['action'] } })
                }
              >
                <option value="keep" disabled={!revision?.credentials_configured || authBlocked}>
                  {t('keep')}
                </option>
                <option value="replace">{t('replace')}</option>
                <option value="remove">{t('remove')}</option>
              </select>
            </FormField>
            {draft.auth.action === 'replace' && (
              <div className="grid gap-4 sm:grid-cols-2">
                <FormField label={t('accessKey')}>
                  <Input
                    name="storage_access_key"
                    autoComplete="off"
                    value={draft.auth.access_key ?? ''}
                    onValueChange={(access_key) => change({ auth: { ...draft.auth, access_key } })}
                  />
                </FormField>
                <FormField label={t('secretKey')}>
                  <Input
                    name="storage_secret_key"
                    type="password"
                    autoComplete="new-password"
                    value={draft.auth.secret_key ?? ''}
                    onValueChange={(secret_key) => change({ auth: { ...draft.auth, secret_key } })}
                  />
                </FormField>
              </div>
            )}
            <p className="text-xs text-muted-foreground">{t('secretHelp')}</p>
            {authBlocked && (
              <p role="alert" className="text-sm text-destructive">
                {t('binding')}
              </p>
            )}
            {missingAuthentication && (
              <p role="alert" className="text-sm text-destructive">
                {t('invalid')}
              </p>
            )}
          </fieldset>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {t(error)}
            </p>
          )}
          {notice && (
            <p role="status" className="text-sm">
              {t(notice)}
            </p>
          )}
          {needsReview && (
            <Button type="button" variant="outline" disabled={busy} onClick={() => void review()}>
              {t(operation === 'review' ? 'reviewing' : 'review')}
            </Button>
          )}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
              {t('cancel')}
            </Button>
            {access.can('storage.write') && (
              <Button type="submit" disabled={busy || !canSave}>
                {t(operation === 'save' ? 'saving' : 'save')}
              </Button>
            )}
          </div>
        </form>

        <section className="space-y-4 border-t pt-6" aria-labelledby="storage-test-title">
          <div className="flex items-start justify-between gap-6">
            <div>
              <h3 id="storage-test-title" className="flex items-center gap-2 font-semibold">
                <TestTubeDiagonal className="size-4" aria-hidden="true" />
                {t('testTitle')}
              </h3>
              <p className="mt-1 text-sm text-muted-foreground">{t('testHelp')}</p>
            </div>
            {access.can('storage.test') && (
              <Button
                variant="outline"
                disabled={busy || !baseline.revision || dirty || needsReview}
                onClick={() => void runTest()}
              >
                {t(operation === 'test' ? 'testing' : 'test')}
              </Button>
            )}
          </div>
          {probe && <StorageProbeResult result={probe} />}
        </section>

        <StorageRevisions
          settings={baseline}
          busy={busy}
          dirty={dirty}
          needsReview={needsReview}
          canWrite={access.can('storage.write')}
          rollingBack={operation === 'rollback'}
          onRollback={confirmRollback}
        />
      </div>
    </Drawer>
  )
}
