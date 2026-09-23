import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { SMTPError, sendSMTPTest, validMailbox } from '@/api/smtp'
import type { SMTPSettings, SMTPTest, SMTPTestInput } from '@/types/smtp'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { FormField } from '@/components/app/CatalogUI'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { SMTPResult } from './result'
export default function TestMail({
  settings,
  dirty,
  onBusy,
  onReload,
}: {
  settings: SMTPSettings
  dirty: boolean
  onBusy: (busy: boolean) => void
  onReload: () => Promise<SMTPSettings>
}) {
  const { t } = useTranslation('smtp'),
    session = useSession(),
    access = usePermissions(),
    [recipient, setRecipient] = useState(''),
    [intent, setIntent] = useState<SMTPTestInput | null>(null),
    [result, setResult] = useState<SMTPTest | null>(settings.last_test),
    [error, setError] = useState<string | null>(null),
    [busy, setBusy] = useState(false),
    lock = useRef(false)
  const ready = settings.enabled && !!settings.sender_email
  async function send() {
    if (lock.current || !session.data || !access.can('smtp.test') || (!intent && (!ready || dirty)))
      return
    if (!intent && !validMailbox(recipient.trim())) {
      setError('recipientInvalid')
      return
    }
    const request = intent ?? {
      request_id: crypto.randomUUID(),
      etag: settings.etag,
      recipient: recipient.trim(),
    }
    if (!intent) setResult(null)
    setIntent(request)
    lock.current = true
    setBusy(true)
    onBusy(true)
    setError(null)
    try {
      setResult(await sendSMTPTest(request, session.data.csrf_token))
    } catch (failure) {
      const status = failure instanceof SMTPError ? failure.status : 0
      setError(
        status === 429
          ? 'cooldown'
          : status === 409
            ? 'conflict'
            : status === 400
              ? 'recipientInvalid'
              : status === 401 || status === 403
                ? 'failed'
                : 'testUncertain',
      )
    } finally {
      lock.current = false
      setBusy(false)
      onBusy(false)
    }
  }
  async function review() {
    if (lock.current) return
    lock.current = true
    setBusy(true)
    onBusy(true)
    try {
      const current = await onReload()
      setIntent(null)
      setResult(current.last_test)
      setError(null)
    } catch {
      setError('failed')
    } finally {
      lock.current = false
      setBusy(false)
      onBusy(false)
    }
  }
  return (
    <section className="space-y-4 border-t pt-6">
      <div>
        <h2 className="font-semibold">{t('testTitle')}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{t('testHelp')}</p>
      </div>
      <form
        aria-label={t('testTitle')}
        className="space-y-4"
        onSubmit={(event) => {
          event.preventDefault()
          void send()
        }}
      >
        <FormField label={t('recipient')}>
          <Input
            name="recipient"
            maxLength={254}
            type="email"
            required
            value={recipient}
            disabled={busy || !!intent || !access.can('smtp.test')}
            onValueChange={setRecipient}
          />
        </FormField>
        {!ready && <p className="text-sm text-muted-foreground">{t('saveFirst')}</p>}
        {dirty && !intent && <p className="text-sm text-muted-foreground">{t('draftFirst')}</p>}
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {t(error)}
          </p>
        )}
        <div className="flex flex-wrap justify-end gap-2">
          {error === 'conflict' && (
            <Button type="button" variant="outline" disabled={busy} onClick={() => void review()}>
              {t('reload')}
            </Button>
          )}
          {intent && (
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() => {
                setIntent(null)
                setResult(null)
                setError(null)
              }}
            >
              {t('newTest')}
            </Button>
          )}
          <Button
            type="submit"
            disabled={
              busy ||
              !access.can('smtp.test') ||
              (!intent && (!ready || dirty || !recipient.trim()))
            }
          >
            {t(busy ? 'sending' : intent ? 'checkResult' : 'sendTest')}
          </Button>
        </div>
      </form>
      {result && <SMTPResult result={result} />}
    </section>
  )
}
