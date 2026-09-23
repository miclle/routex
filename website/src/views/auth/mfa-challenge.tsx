import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { FormField } from '@/components/app/CatalogUI'
import type { MFAChallenge, MFAProof } from '@/types/mfa'
export default function MFAChallengeForm({
  challenge,
  busy,
  error,
  verify,
  restart,
}: {
  challenge: MFAChallenge
  busy: boolean
  error: string
  verify: (proof: MFAProof) => void
  restart: (expired?: boolean) => void
}) {
  const { t, i18n } = useTranslation('mfa')
  const [recovery, setRecovery] = useState(false)
  const [code, setCode] = useState('')
  const [validation, setValidation] = useState('')
  useEffect(() => {
    const timeout = setTimeout(
      () => restart(true),
      Math.max(0, Date.parse(challenge.expires_at) - Date.now()),
    )
    return () => clearTimeout(timeout)
  }, [challenge.expires_at, restart])
  function submit(event: FormEvent) {
    event.preventDefault()
    if (busy) return
    if (Date.parse(challenge.expires_at) <= Date.now()) {
      restart(true)
      return
    }
    if (recovery ? !code.trim() : !/^\d{6}$/.test(code)) {
      setValidation(recovery ? 'invalidRecovery' : 'invalidCode')
      return
    }
    const proof: MFAProof = recovery ? { recovery_code: code.trim() } : { code }
    setCode('')
    setValidation('')
    verify(proof)
  }
  return (
    <form aria-label={t('loginTitle')} onSubmit={submit} className="space-y-5">
      <p className="text-sm text-muted-foreground">{t('loginHelp')}</p>
      <FormField label={t(recovery ? 'recoveryCode' : 'code')}>
        <Input
          name={recovery ? 'recovery_code' : 'code'}
          aria-label={t(recovery ? 'recoveryCode' : 'code')}
          autoComplete="one-time-code"
          inputMode={recovery ? 'text' : 'numeric'}
          maxLength={recovery ? 128 : 6}
          value={code}
          onValueChange={setCode}
          disabled={busy}
          autoFocus
        />
      </FormField>
      <p className="text-xs text-muted-foreground">
        {t('expiry', {
          time: new Date(challenge.expires_at).toLocaleString(i18n.resolvedLanguage),
        })}
      </p>
      {(validation || error) && (
        <p role="alert" className="text-sm text-destructive">
          {validation ? t(validation) : error}
        </p>
      )}
      <Button type="submit" disabled={busy} className="w-full">
        {t(busy ? 'working' : 'verify')}
      </Button>
      <div className="flex flex-wrap justify-between gap-2">
        <Button
          type="button"
          variant="ghost"
          disabled={busy}
          onClick={() => {
            setRecovery(!recovery)
            setCode('')
            setValidation('')
          }}
        >
          {t(recovery ? 'useAuthenticator' : 'useRecovery')}
        </Button>
        <Button type="button" variant="ghost" disabled={busy} onClick={() => restart()}>
          {t('restart')}
        </Button>
      </div>
    </form>
  )
}
