import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Mail, Send } from 'lucide-react'
import { getSMTP, saveSMTP, saveSMTPSender, SMTPError, validMailbox } from '@/api/smtp'
import type { SMTPInput, SMTPSenderInput, SMTPSettings } from '@/types/smtp'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { PermissionGate } from '@/components/app/PermissionGate'
import { FormField, Page, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Drawer } from '@/components/ui/drawer'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import TestMail from './test-mail'
import { SMTPResult } from './result'
const selectClass = 'h-10 w-full rounded-md border bg-background px-3 text-sm'
export default function SMTPPage() {
  return (
    <PermissionGate permission="smtp.read">
      <Settings />
    </PermissionGate>
  )
}
function Settings() {
  const { t } = useTranslation('smtp'),
    cache = useQueryClient(),
    query = useQuery({ queryKey: ['admin', 'smtp'], queryFn: ({ signal }) => getSMTP(signal) }),
    [panel, setPanel] = useState<'server' | 'sender' | null>(null),
    [saved, setSaved] = useState(false)
  const config = query.data
  return (
    <Page title={t('title')} description={t('description')}>
      <div>
        <h1 className="font-semibold">{t('title')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('description')}</p>
      </div>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={() => void query.refetch()}
      />
      {saved && <p role="status">{t('saved')}</p>}
      {config && (
        <>
          <div className="space-y-4">
            {(['server', 'sender'] as const).map((kind) => (
              <section key={kind} aria-label={t(kind)} className="space-y-4 rounded-lg border p-4">
                <div className="flex items-center justify-between gap-6">
                  <div className="flex items-center gap-4">
                    {kind === 'server' ? <Mail className="size-6" /> : <Send className="size-6" />}
                    <div>
                      <div className="flex items-center gap-2">
                        <h2 className="font-semibold">{t(kind)}</h2>
                        <Badge variant="outline">
                          {kind === 'server'
                            ? t(config.enabled ? 'enabled' : 'disabled')
                            : t(config.sender_email ? 'configured' : 'notConfigured')}
                        </Badge>
                      </div>
                      <p className="mt-2 text-sm text-muted-foreground">
                        {t(kind === 'server' ? 'serverDescription' : 'senderDescription')}
                      </p>
                    </div>
                  </div>
                  <Button
                    variant="outline"
                    aria-label={t(kind === 'server' ? 'configureServer' : 'configureSender')}
                    onClick={() => {
                      setPanel(kind)
                      setSaved(false)
                    }}
                  >
                    {t('configure')}
                  </Button>
                </div>
                <dl className="grid gap-4 text-sm sm:grid-cols-3">
                  {(kind === 'server'
                    ? [
                        [t('host'), config.host ? `${config.host}:${config.port}` : t('notSet')],
                        [t('security'), config.security === 'NONE' ? t('none') : config.security],
                        [t('auth'), t(config.auth_configured ? 'configured' : 'notConfigured')],
                      ]
                    : [
                        [t('senderName'), config.sender_name || t('notSet')],
                        [t('senderEmail'), config.sender_email || t('notSet')],
                        [t('replyTo'), config.reply_to || t('notSet')],
                      ]
                  ).map(([label, value]) => (
                    <div key={label} className="flex flex-wrap gap-x-4 gap-y-1">
                      <dt className="text-muted-foreground">{label}</dt>
                      <dd className="break-all">{value}</dd>
                    </div>
                  ))}
                </dl>
              </section>
            ))}
          </div>
          {config.last_test && <SMTPResult result={config.last_test} />}
        </>
      )}
      {panel && config && (
        <Configuration
          key={panel}
          initial={config}
          panel={panel}
          onClose={() => {
            setPanel(null)
            void query.refetch()
          }}
          onSaved={(next) => {
            cache.setQueryData(['admin', 'smtp'], next)
            setPanel(null)
            setSaved(true)
            void query.refetch()
          }}
        />
      )}
    </Page>
  )
}
function Configuration({
  initial,
  panel,
  onClose,
  onSaved,
}: {
  initial: SMTPSettings
  panel: 'server' | 'sender'
  onClose: () => void
  onSaved: (settings: SMTPSettings) => void
}) {
  const { t } = useTranslation('smtp'),
    session = useSession(),
    access = usePermissions(),
    [baseline, setBaseline] = useState(initial),
    [draft, setDraft] = useState<SMTPInput>({
      enabled: initial.enabled,
      host: initial.host,
      port: initial.port,
      security: initial.security,
      auth: { action: 'keep' },
      etag: initial.etag,
    }),
    [sender, setSender] = useState<SMTPSenderInput>({
      sender_name: initial.sender_name,
      sender_email: initial.sender_email,
      reply_to: initial.reply_to,
      etag: initial.etag,
    }),
    [busy, setBusy] = useState(false),
    [testing, setTesting] = useState(false),
    [error, setError] = useState<string | null>(null),
    lock = useRef(false)
  const endpointChanged =
    draft.host.trim().toLowerCase() !== baseline.host ||
    draft.port !== baseline.port ||
    draft.security !== baseline.security
  const authBlocked = baseline.auth_configured && endpointChanged && draft.auth.action === 'keep'
  const insecureAuth =
    draft.security === 'NONE' &&
    (draft.auth.action === 'replace' || (draft.auth.action === 'keep' && baseline.auth_configured))
  const dirty =
    panel === 'server'
      ? endpointChanged || draft.enabled !== baseline.enabled || draft.auth.action !== 'keep'
      : sender.sender_name !== baseline.sender_name ||
        sender.sender_email !== baseline.sender_email ||
        sender.reply_to !== baseline.reply_to
  const canSave =
    access.can('smtp.write') &&
    dirty &&
    !authBlocked &&
    !insecureAuth &&
    error !== 'conflict' &&
    error !== 'uncertain'
  async function reload() {
    const current = await getSMTP()
    setBaseline(current)
    setError(null)
    return current
  }
  async function save() {
    if (lock.current || !session.data || !canSave) return
    const valid =
      panel === 'server'
        ? !!draft.host.trim() &&
          Number.isInteger(draft.port) &&
          draft.port > 0 &&
          draft.port <= 65535 &&
          (draft.auth.action !== 'replace' || (!!draft.auth.username && !!draft.auth.password))
        : !!sender.sender_name.trim() &&
          [...sender.sender_name.trim()].length <= 100 &&
          validMailbox(sender.sender_email.trim()) &&
          (!sender.reply_to.trim() || validMailbox(sender.reply_to.trim()))
    if (!valid) {
      setError('invalid')
      return
    }
    lock.current = true
    setBusy(true)
    setError(null)
    try {
      const result =
        panel === 'server'
          ? await saveSMTP({ ...draft, etag: baseline.etag }, session.data.csrf_token)
          : await saveSMTPSender({ ...sender, etag: baseline.etag }, session.data.csrf_token)
      setDraft({ ...draft, auth: { action: 'keep' } })
      onSaved(result)
    } catch (failure) {
      const status = failure instanceof SMTPError ? failure.status : 0
      setError(
        status === 409
          ? 'conflict'
          : status === 422
            ? 'verificationFailed'
            : status === 400
              ? 'invalid'
              : status === 0 || status >= 500
                ? 'uncertain'
                : 'failed',
      )
    } finally {
      lock.current = false
      setBusy(false)
    }
  }
  async function review() {
    if (lock.current) return
    lock.current = true
    setBusy(true)
    try {
      await reload()
    } catch {
      setError('failed')
    } finally {
      lock.current = false
      setBusy(false)
    }
  }
  return (
    <Drawer
      open
      title={t(panel === 'server' ? 'configureServer' : 'configureSender')}
      description={t(panel === 'server' ? 'serverDescription' : 'senderDescription')}
      busy={busy || testing}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <div className="space-y-6">
        <form
          aria-label={t(panel === 'server' ? 'configureServer' : 'configureSender')}
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            void save()
          }}
        >
          <fieldset disabled={busy || testing || !access.can('smtp.write')} className="space-y-4">
            {panel === 'server' ? (
              <>
                <div className="flex items-center justify-between gap-4">
                  <div>
                    <p className="text-sm font-medium">{t('enable')}</p>
                    <p className="mt-1 text-xs text-muted-foreground">{t('enableHelp')}</p>
                  </div>
                  <Switch
                    checked={draft.enabled}
                    onCheckedChange={(enabled) => setDraft({ ...draft, enabled })}
                    aria-label={t('enable')}
                  />
                </div>
                <FormField label={t('host')}>
                  <Input
                    name="smtp_host"
                    maxLength={253}
                    value={draft.host}
                    onValueChange={(host) => setDraft({ ...draft, host })}
                  />
                </FormField>
                <FormField label={t('port')}>
                  <Input
                    name="smtp_port"
                    type="number"
                    min={1}
                    max={65535}
                    value={draft.port}
                    onValueChange={(port) => setDraft({ ...draft, port: Number(port) })}
                  />
                </FormField>
                <FormField label={t('security')}>
                  <select
                    name="security"
                    className={selectClass}
                    value={draft.security}
                    onChange={(event) =>
                      setDraft({
                        ...draft,
                        security: event.target.value as SMTPSettings['security'],
                      })
                    }
                  >
                    <option value="STARTTLS">STARTTLS</option>
                    <option value="SSL_TLS">SSL/TLS</option>
                    <option value="NONE">{t('none')}</option>
                  </select>
                </FormField>
                {draft.security === 'NONE' && (
                  <p className="text-sm text-muted-foreground">{t('noneHelp')}</p>
                )}
                <FormField label={t('auth')}>
                  <select
                    name="auth_action"
                    className={selectClass}
                    value={draft.auth.action}
                    onChange={(event) =>
                      setDraft({
                        ...draft,
                        auth: { action: event.target.value as SMTPInput['auth']['action'] },
                      })
                    }
                  >
                    <option value="keep" disabled={authBlocked || insecureAuth}>
                      {t('keep')}
                    </option>
                    <option value="replace" disabled={draft.security === 'NONE'}>
                      {t('replace')}
                    </option>
                    <option value="remove">{t('remove')}</option>
                  </select>
                </FormField>
                {authBlocked && (
                  <p role="alert" className="text-sm text-destructive">
                    {t('binding')}
                  </p>
                )}
                {draft.auth.action === 'replace' && (
                  <>
                    <FormField label={t('username')}>
                      <Input
                        name="smtp_username"
                        autoComplete="off"
                        value={draft.auth.username ?? ''}
                        onValueChange={(username) =>
                          setDraft({ ...draft, auth: { ...draft.auth, username } })
                        }
                      />
                    </FormField>
                    <FormField label={t('password')}>
                      <Input
                        name="smtp_password"
                        type="password"
                        autoComplete="new-password"
                        value={draft.auth.password ?? ''}
                        onValueChange={(password) =>
                          setDraft({ ...draft, auth: { ...draft.auth, password } })
                        }
                      />
                    </FormField>
                  </>
                )}
                <p className="text-xs text-muted-foreground">{t('secretHelp')}</p>
              </>
            ) : (
              <>
                {(['sender_name', 'sender_email', 'reply_to'] as const).map((name, index) => (
                  <FormField key={name} label={t(['senderName', 'senderEmail', 'replyTo'][index])}>
                    <Input
                      name={name}
                      type={index === 0 ? 'text' : 'email'}
                      maxLength={index === 0 ? 100 : 254}
                      value={sender[name]}
                      onValueChange={(value) => setSender({ ...sender, [name]: value })}
                    />
                  </FormField>
                ))}
              </>
            )}
          </fieldset>
          <p className="text-xs text-muted-foreground">
            {t('current')}: {baseline.host}:{baseline.port} · {baseline.security} ·{' '}
            {baseline.sender_email || t('notSet')}
          </p>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {t(error)}
            </p>
          )}
          {(error === 'conflict' || error === 'uncertain') && (
            <Button
              type="button"
              variant="outline"
              disabled={busy || testing}
              onClick={() => void review()}
            >
              {t('reload')}
            </Button>
          )}
          {access.can('smtp.write') && (
            <div className="flex justify-end">
              <Button type="submit" disabled={busy || testing || !canSave}>
                {t(busy ? 'saving' : panel === 'server' ? 'saveServer' : 'saveSender')}
              </Button>
            </div>
          )}
        </form>
        {panel === 'server' && (
          <TestMail settings={baseline} dirty={dirty} onBusy={setTesting} onReload={reload} />
        )}
      </div>
    </Drawer>
  )
}
