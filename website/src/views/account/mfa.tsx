import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { QRCodeSVG } from 'qrcode.react'
import {
  beginMFAEnrollment,
  cancelMFAEnrollment,
  changeMFA,
  enableMFA,
  getMFAStatus,
  MFARequestError,
} from '@/api/mfa'
import { sessionKey, useSession } from '@/hooks/use-auth'
import type { MFAEnrollment, MFAProof, MFARecovery } from '@/types/mfa'
import type { Session } from '@/types/auth'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Dialog } from '@/components/ui/dialog'
import { FormField, QueryState } from '@/components/app/CatalogUI'

type Action = 'enable' | 'disable' | 'recovery-codes'
export default function AccountMFA() {
  const { t, i18n } = useTranslation('mfa')
  const session = useSession()
  const cache = useQueryClient()
  const status = useQuery({
    queryKey: ['account', 'mfa'],
    queryFn: ({ signal }) => getMFAStatus(signal),
  })
  const [action, setAction] = useState<Action | null>(null)
  const [enrollment, setEnrollment] = useState<MFAEnrollment | null>(null)
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([])
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [recovery, setRecovery] = useState(false)
  const [busy, setBusy] = useState(false)
  const [issue, setIssue] = useState('')
  const [notice, setNotice] = useState('')
  const lock = useRef(false)
  const generation = useRef(0)
  useEffect(
    () => () => {
      generation.current++
    },
    [],
  )
  function clear() {
    setEnrollment(null)
    setPassword('')
    setCode('')
    setRecovery(false)
    setAction(null)
  }
  useEffect(() => {
    if (!enrollment) return
    const timer = setTimeout(
      () => {
        setEnrollment(null)
        setPassword('')
        setCode('')
        setAction(null)
        setIssue('expired')
        void cache.invalidateQueries({ queryKey: ['account', 'mfa'] })
      },
      Math.max(0, Date.parse(enrollment.expires_at) - Date.now()),
    )
    return () => clearTimeout(timer)
  }, [enrollment, cache])
  async function refresh() {
    setIssue('')
    await status.refetch()
  }
  async function cancel() {
    if (lock.current || !session.data) return
    const hasEnrollment = !!enrollment || status.data?.enrollment_pending
    clear()
    setIssue('')
    generation.current++
    if (!hasEnrollment) return
    const turn = generation.current
    lock.current = true
    setBusy(true)
    try {
      await cancelMFAEnrollment(session.data.csrf_token)
      if (turn === generation.current) setNotice('canceled')
    } catch {
      if (turn === generation.current) setIssue('cancelFailed')
    } finally {
      if (turn === generation.current) {
        lock.current = false
        setBusy(false)
        void status.refetch()
      }
    }
  }
  async function rotate(next: Session) {
    await cache.cancelQueries()
    cache.setQueryData(sessionKey, next)
    await cache.resetQueries({ predicate: (query) => query.queryKey[0] !== 'auth' })
  }
  function open(next: Action) {
    clear()
    setRecoveryCodes([])
    setIssue('')
    setNotice('')
    setAction(next)
  }
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (lock.current || !action || !session.data) return
    if (!password) {
      setIssue('passwordRequired')
      return
    }
    if (enrollment && Date.parse(enrollment.expires_at) <= Date.now()) {
      clear()
      setIssue('expired')
      return
    }
    const needsProof = action !== 'enable' || !!enrollment
    if (needsProof && (recovery ? !code.trim() : !/^\d{6}$/.test(code))) {
      setIssue(recovery ? 'invalidRecovery' : 'invalidCode')
      return
    }
    const proof: MFAProof = recovery ? { recovery_code: code.trim() } : { code }
    const currentPassword = password
    const captured = enrollment
    const turn = ++generation.current
    lock.current = true
    setBusy(true)
    setIssue('')
    setNotice('')
    setCode('')
    try {
      if (action === 'enable' && !captured) {
        const next = await beginMFAEnrollment(currentPassword, session.data.csrf_token)
        if (turn === generation.current) setEnrollment(next)
      } else {
        let result: Session | MFARecovery
        if (action === 'enable')
          result = await enableMFA(
            {
              current_password: currentPassword,
              enrollment_token: captured!.enrollment_token,
              code: 'code' in proof ? proof.code! : '',
            },
            session.data.csrf_token,
          )
        else if (action === 'disable')
          result = await changeMFA(
            'disable',
            { current_password: currentPassword, ...proof },
            session.data.csrf_token,
          )
        else
          result = await changeMFA(
            'recovery-codes',
            { current_password: currentPassword, ...proof },
            session.data.csrf_token,
          )
        if (turn !== generation.current) return
        await rotate('session' in result ? result.session : result)
        if (turn !== generation.current) return
        if ('recovery_codes' in result) setRecoveryCodes(result.recovery_codes)
        setNotice(
          action === 'enable'
            ? 'enabledNotice'
            : action === 'disable'
              ? 'disabledNotice'
              : 'regeneratedNotice',
        )
        clear()
      }
    } catch (error) {
      if (turn !== generation.current) return
      const code = error instanceof MFARequestError ? error.status : 0
      setIssue(code === 401 ? 'invalid' : code === 409 ? 'conflict' : 'failed')
      setPassword('')
      if (code === 401) void cache.invalidateQueries({ queryKey: sessionKey })
      if (code !== 401) {
        setEnrollment(null)
        setAction(null)
        void status.refetch()
      }
    } finally {
      if (turn === generation.current) {
        lock.current = false
        setBusy(false)
      }
    }
  }
  const title =
    action === 'disable' ? 'disable' : action === 'recovery-codes' ? 'regenerate' : 'enable'
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>{t('title')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <QueryState
            pending={status.isPending}
            error={status.error}
            retry={() => void status.refetch()}
          />
          {status.data && (
            <>
              <div className="flex flex-wrap items-center justify-between gap-4">
                <p className="text-sm text-muted-foreground">
                  {t(status.data.enabled ? 'enabled' : 'disabled')}
                </p>
                <Switch
                  aria-label={t(status.data.enabled ? 'disable' : 'enable')}
                  checked={status.data.enabled}
                  disabled={
                    busy ||
                    (!status.data.enabled &&
                      (!status.data.enrollment_available || status.data.enrollment_pending))
                  }
                  onCheckedChange={() => open(status.data!.enabled ? 'disable' : 'enable')}
                />
              </div>
              {!status.data.enrollment_available && !status.data.enabled && (
                <p className="text-sm text-muted-foreground">{t('unavailable')}</p>
              )}
              {status.data.enrollment_pending && !status.data.enabled && (
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <p className="text-sm">{t('pending')}</p>
                  <Button variant="outline" disabled={busy} onClick={() => void cancel()}>
                    {t('cancelEnrollment')}
                  </Button>
                </div>
              )}
              {status.data.enabled && (
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <p className="text-sm text-muted-foreground">
                    {t('remaining', { count: status.data.recovery_codes_remaining })}
                  </p>
                  <Button variant="outline" disabled={busy} onClick={() => open('recovery-codes')}>
                    {t('regenerate')}
                  </Button>
                </div>
              )}
            </>
          )}
          {notice && (
            <p role="status" className="text-sm">
              {t(notice)}
            </p>
          )}
          {issue && !action && (
            <div className="space-y-3">
              <p role="alert" className="text-sm text-destructive">
                {t(issue)}
              </p>
              <Button variant="outline" onClick={() => void refresh()}>
                {t('reload')}
              </Button>
            </div>
          )}
        </CardContent>
      </Card>
      <Dialog
        open={!!action}
        onOpenChange={(open) => {
          if (!open) void cancel()
        }}
        busy={busy}
        title={t(title)}
        description={t(
          action === 'disable'
            ? 'disableHelp'
            : action === 'recovery-codes'
              ? 'regenerateHelp'
              : 'enrollmentHelp',
        )}
      >
        <form className="space-y-5" aria-label={t(title)} onSubmit={(event) => void submit(event)}>
          {enrollment && (
            <div className="space-y-4">
              <p className="text-sm text-muted-foreground">{t('setupHelp')}</p>
              <div className="flex justify-center">
                <QRCodeSVG
                  value={enrollment.otpauth_uri}
                  size={200}
                  marginSize={4}
                  level="M"
                  role="img"
                  aria-label={t('qr')}
                />
              </div>
              <FormField label={t('secret')}>
                <Input
                  aria-label={t('secret')}
                  value={enrollment.secret}
                  readOnly
                  autoComplete="off"
                  className="font-mono"
                />
              </FormField>
              <p className="text-xs text-muted-foreground">
                {t('expiry', {
                  time: new Date(enrollment.expires_at).toLocaleString(i18n.resolvedLanguage),
                })}
              </p>
            </div>
          )}
          <fieldset disabled={busy} className="space-y-4">
            <FormField label={t('currentPassword')}>
              <Input
                name="mfa_password"
                type="password"
                autoComplete="current-password"
                value={password}
                onValueChange={setPassword}
              />
            </FormField>
            {(enrollment || action !== 'enable') && (
              <>
                <FormField label={t(recovery ? 'recoveryCode' : 'code')}>
                  <Input
                    name="mfa_code"
                    autoComplete="one-time-code"
                    inputMode={recovery ? 'text' : 'numeric'}
                    maxLength={recovery ? 128 : 6}
                    value={code}
                    onValueChange={setCode}
                  />
                </FormField>
                {action !== 'enable' && (
                  <Button
                    type="button"
                    variant="ghost"
                    onClick={() => {
                      setRecovery(!recovery)
                      setCode('')
                      setIssue('')
                    }}
                  >
                    {t(recovery ? 'useAuthenticator' : 'useRecovery')}
                  </Button>
                )}
                <p className="text-xs text-muted-foreground">{t('proofHelp')}</p>
              </>
            )}
          </fieldset>
          {issue && (
            <p role="alert" className="text-sm text-destructive">
              {t(issue)}
            </p>
          )}
          <div className="flex justify-end gap-3">
            <Button type="button" variant="outline" disabled={busy} onClick={() => void cancel()}>
              {t('cancel')}
            </Button>
            <Button type="submit" disabled={busy}>
              {t(
                busy
                  ? 'working'
                  : action === 'enable'
                    ? enrollment
                      ? 'verifyEnable'
                      : 'begin'
                    : action === 'disable'
                      ? 'confirmDisable'
                      : 'confirmRegenerate',
              )}
            </Button>
          </div>
        </form>
      </Dialog>
      <Dialog
        open={recoveryCodes.length > 0}
        onOpenChange={(open) => {
          if (!open) setRecoveryCodes([])
        }}
        title={t('recoveryTitle')}
        description={t('recoveryHelp')}
      >
        <div className="space-y-5">
          <ul className="space-y-2">
            {recoveryCodes.map((code) => (
              <li className="break-all font-mono text-sm" key={code}>
                {code}
              </li>
            ))}
          </ul>
          <Button className="w-full" onClick={() => setRecoveryCodes([])}>
            {t('saved')}
          </Button>
        </div>
      </Dialog>
    </>
  )
}
