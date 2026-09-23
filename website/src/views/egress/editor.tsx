import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { EgressError, listEgress, saveEgress, testEgressDraft } from '@/api/egress'
import type { Egress, EgressDiagnostic, EgressInput } from '@/types/egress'
import { FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog } from '@/components/ui/dialog'
import { Diagnostic } from './diagnostic'
export const selectClass = 'h-10 w-full rounded-md border bg-background px-3 text-sm'
export function ErrorMessage({ status }: { status: number | null }) {
  const { t } = useTranslation('egress')
  return status === null ? null : (
    <p role="alert" className="text-sm text-destructive">
      {t(
        status === 409
          ? 'conflict'
          : status === 503
            ? 'unavailable'
            : status === 400
              ? 'invalid'
              : 'failed',
      )}
    </p>
  )
}
export default function EgressEditor({
  initial,
  onClose,
  onSaved,
}: {
  initial: Egress | null
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation('egress'),
    session = useSession(),
    access = usePermissions()
  const [baseline, setBaseline] = useState(initial),
    [draft, setDraft] = useState<EgressInput>({
      name: initial?.name ?? '',
      kind: initial?.kind ?? 'socks5',
      host: initial?.host ?? '',
      port: initial?.port ?? 1080,
      enabled: initial?.enabled ?? true,
      auth: { action: initial ? 'keep' : 'remove' },
    })
  const [target, setTarget] = useState(''),
    [result, setResult] = useState<EgressDiagnostic | null>(null),
    [tested, setTested] = useState(false),
    [operation, setOperation] = useState<'test' | 'save' | 'reload' | null>(null),
    [error, setError] = useState<number | null>(null)
  const busy = operation !== null
  const lock = useRef(false)
  const changed =
    !baseline ||
    draft.kind !== baseline.kind ||
    draft.host.trim() !== baseline.host ||
    draft.port !== baseline.port ||
    draft.auth.action === 'replace' ||
    (draft.auth.action === 'remove' && baseline.auth_configured) ||
    (!baseline.enabled && draft.enabled)
  const hasChanges =
    !baseline ||
    changed ||
    draft.name.trim() !== baseline.name ||
    draft.enabled !== baseline.enabled
  const valid =
    !!draft.name.trim() &&
    !!draft.host.trim() &&
    !/\s/.test(draft.host) &&
    Number.isInteger(draft.port) &&
    draft.port > 0 &&
    draft.port <= 65535 &&
    (draft.auth.action !== 'replace' || (!!draft.auth.username && !!draft.auth.password))
  const needsReview = error === 409 || error === 503
  function change(patch: Partial<EgressInput>) {
    setDraft({ ...draft, ...patch })
    setTested(false)
    setResult(null)
  }
  async function run(kind: 'test' | 'save' | 'reload') {
    if (lock.current || !session.data || (kind === 'test' && !access.can('egress.test'))) return
    if (kind !== 'reload' && (!valid || (changed && !target.trim()))) {
      setError(400)
      return
    }
    if (
      kind === 'save' &&
      (!hasChanges || needsReview || (changed && !tested) || !access.can('egress.write'))
    )
      return
    lock.current = true
    setOperation(kind)
    setError(null)
    try {
      if (kind === 'reload') {
        const row = (await listEgress()).find((item) => item.id === initial?.id)
        if (!row) throw new EgressError(404)
        setBaseline(row)
        setTested(false)
        setResult(null)
      } else {
        const payload = {
          ...draft,
          ...(baseline ? { etag: baseline.etag } : {}),
          ...(target.trim() ? { test_target_base_url: target.trim() } : {}),
        }
        if (kind === 'test') {
          const response = await testEgressDraft(
            baseline?.id ?? null,
            payload,
            session.data.csrf_token,
          )
          setResult(response)
          setTested(response.transport_ok && !response.stale)
        } else {
          await saveEgress(baseline?.id ?? null, payload, session.data.csrf_token)
          setDraft({ ...draft, auth: { action: 'keep' } })
          onSaved()
        }
      }
    } catch (failure) {
      setError(failure instanceof EgressError ? failure.status : 0)
      setTested(false)
    } finally {
      lock.current = false
      setOperation(null)
    }
  }
  return (
    <Dialog
      open
      title={t(initial ? 'edit' : 'add')}
      description={t('configDescription')}
      width={720}
      busy={busy}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <form
        className="space-y-5"
        onSubmit={(event) => {
          event.preventDefault()
          void run('save')
        }}
      >
        <fieldset disabled={busy} className="space-y-5">
          <section className="space-y-3">
            <div>
              <h2 className="font-semibold">{t('info')}</h2>
              <p className="text-sm text-muted-foreground">{t('infoHelp')}</p>
            </div>
            <FormField label={t('name')}>
              <Input
                name="egress_name"
                value={draft.name}
                maxLength={100}
                onValueChange={(name) => setDraft({ ...draft, name })}
              />
            </FormField>
          </section>
          <section className="space-y-3">
            <div>
              <h2 className="font-semibold">{t('connection')}</h2>
              <p className="text-sm text-muted-foreground">{t('connectionHelp')}</p>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <FormField label={t('protocol')}>
                <select
                  className={selectClass}
                  name="kind"
                  value={draft.kind}
                  onChange={(event) => change({ kind: event.target.value as Egress['kind'] })}
                >
                  <option value="socks5">SOCKS5</option>
                  <option value="https">HTTPS</option>
                </select>
              </FormField>
              <FormField label={t('port')}>
                <Input
                  name="port"
                  type="number"
                  min={1}
                  max={65535}
                  value={draft.port}
                  onValueChange={(value) => change({ port: Number(value) })}
                />
              </FormField>
            </div>
            <FormField label={t('host')}>
              <Input name="host" value={draft.host} onValueChange={(host) => change({ host })} />
            </FormField>
            <p className="text-xs text-muted-foreground">{t('hostHelp')}</p>
            <FormField label={t('auth')}>
              <select
                className={selectClass}
                name="auth_action"
                value={draft.auth.action}
                onChange={(event) =>
                  change({ auth: { action: event.target.value as EgressInput['auth']['action'] } })
                }
              >
                {initial && (
                  <option value="keep">
                    {t('keep')} · {t(baseline?.auth_configured ? 'configured' : 'noAuth')}
                  </option>
                )}
                <option value="replace">{t('replace')}</option>
                <option value="remove">{t('remove')}</option>
              </select>
            </FormField>
            {draft.auth.action === 'replace' && (
              <div className="grid gap-4 sm:grid-cols-2">
                <FormField label={t('username')}>
                  <Input
                    name="proxy_username"
                    autoComplete="off"
                    value={draft.auth.username ?? ''}
                    onValueChange={(username) => change({ auth: { ...draft.auth, username } })}
                  />
                </FormField>
                <FormField label={t('password')}>
                  <Input
                    name="proxy_password"
                    type="password"
                    autoComplete="new-password"
                    value={draft.auth.password ?? ''}
                    onValueChange={(password) => change({ auth: { ...draft.auth, password } })}
                  />
                </FormField>
              </div>
            )}
            <p className="text-xs text-muted-foreground">{t('secretHelp')}</p>
            <label className="flex items-center gap-2 text-sm">
              <input
                name="enabled"
                type="checkbox"
                checked={draft.enabled}
                onChange={(event) => change({ enabled: event.target.checked })}
              />
              {t('enabled')}
            </label>
            <FormField label={t('target')}>
              <Input
                name="target"
                type="url"
                value={target}
                onValueChange={(value) => {
                  setTarget(value)
                  setTested(false)
                  setResult(null)
                }}
              />
            </FormField>
            <p className="text-xs text-muted-foreground">{t('targetHelp')}</p>
          </section>
          <div className="flex items-center justify-between gap-3 rounded-md border p-3">
            <p className="text-sm">{t(tested ? 'tested' : changed ? 'testFirst' : 'unchanged')}</p>
            <Button
              type="button"
              variant="outline"
              disabled={!valid || !target || !access.can('egress.test') || needsReview}
              onClick={() => void run('test')}
            >
              {t(operation === 'test' ? 'testing' : 'test')}
            </Button>
          </div>
          {changed && !access.can('egress.test') && (
            <p className="text-sm">{t('requiredTestPermission')}</p>
          )}
          {result && <Diagnostic result={result} />}
          {baseline && (
            <p className="text-xs text-muted-foreground">
              {t('current')}: {baseline.name} · {baseline.kind.toUpperCase()} · {baseline.host}:
              {baseline.port} · {t(baseline.auth_configured ? 'configured' : 'noAuth')}
            </p>
          )}
          <ErrorMessage status={error} />
          {needsReview && (
            <Button
              type="button"
              variant="outline"
              onClick={() => (baseline ? void run('reload') : onClose())}
            >
              {t('reload')}
            </Button>
          )}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose}>
              {t('cancel')}
            </Button>
            <Button
              type="submit"
              disabled={
                !valid ||
                !hasChanges ||
                needsReview ||
                (changed && !tested) ||
                !access.can('egress.write')
              }
            >
              {t(operation === 'save' ? 'saving' : 'save')}
            </Button>
          </div>
        </fieldset>
      </form>
    </Dialog>
  )
}
