import { useReauthentication } from '@/views/auth-enforcement/reauthentication-state'
import { useSecurityLifetime } from '@/views/auth-enforcement/security-lifetime'
import type { SecuritySessionResult } from '@/types/auth-enforcement'
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type FormEvent } from 'react'
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
import { useApprovalCacheRevision } from '@/views/governance/approval-authority'
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
  const sessionGeneration = useApprovalCacheRevision([sessionKey])
  const actor = session.data?.user.id ?? ''
  const statusKey = ['account', 'mfa', actor, sessionGeneration.revision]
  function sessionCurrent() {
    const state = cache.getQueryState<Session | null>(sessionKey)
    return (
      sessionGeneration.snapshot() === sessionGeneration.revision &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      state.data?.user.id === actor &&
      !!actor
    )
  }
  const status = useQuery({
    queryKey: statusKey,
    queryFn: async ({ signal }) => {
      if (!sessionCurrent()) throw new Error('MFA authority unavailable')
      const value = await getMFAStatus(signal)
      if (signal.aborted || !sessionCurrent()) throw new Error('MFA authority unavailable')
      return value
    },
    enabled: sessionCurrent(),
    retry: false,
    gcTime: 0,
    staleTime: 0,
    refetchOnMount: 'always',
  })
  const statusGeneration = useApprovalCacheRevision([statusKey])
  function statusCurrent() {
    const state = cache.getQueryState(statusKey)
    return (
      sessionCurrent() &&
      statusGeneration.snapshot() === statusGeneration.revision &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      state.data === status.data
    )
  }
  const [action, setAction] = useState<Action | null>(null)
  // A retained form callback cannot reuse a dismissed dialog's local proof.
  const [dialogIdentity, setDialogIdentity] = useState<symbol | null>(null)
  const dialogOwner = useRef<symbol | null>(null)
  const [enrollment, setEnrollment] = useState<MFAEnrollment | null>(null)
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([])
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [recovery, setRecovery] = useState(false)
  const [busy, setBusy] = useState(false)
  const [issue, setIssue] = useState('')
  const [notice, setNotice] = useState('')
  const reauthenticate = useReauthentication()
  const lock = useRef(false)
  const generation = useRef(0)
  const clear = useCallback(() => {
    dialogOwner.current = null
    setDialogIdentity(null)
    setEnrollment(null)
    setPassword('')
    setCode('')
    setRecovery(false)
    setAction(null)
  }, [setDialogIdentity, setEnrollment, setPassword, setCode, setRecovery, setAction])
  const resetSecurity = useCallback(() => {
    generation.current++
    lock.current = false
    setBusy(false)
    setRecoveryCodes([])
    clear()
    setIssue('')
    setNotice('')
  }, [clear, setBusy, setRecoveryCodes, setIssue, setNotice])
  const lifetime = useSecurityLifetime(resetSecurity)
  useEffect(
    () => () => {
      dialogOwner.current = null
      generation.current++
    },
    [],
  )
  const statusOwner = JSON.stringify(statusKey)
  const cancelOperation = lifetime.cancel
  useLayoutEffect(() => {
    const key = JSON.parse(statusOwner) as unknown[]
    const fingerprint = () => {
      const state = cache.getQueryState(key)
      return `${state?.status}:${state?.fetchStatus}:${state?.isInvalidated}:${state?.dataUpdateCount}:${state?.errorUpdateCount}`
    }
    let prior = fingerprint()
    return cache.getQueryCache().subscribe((event) => {
      if (
        (event.type !== 'updated' && event.type !== 'removed') ||
        JSON.stringify(event.query.queryKey) !== statusOwner
      )
        return
      const next = fingerprint()
      if (next === prior) return
      prior = next
      clear()
      if (!lock.current) return
      cancelOperation()
      generation.current++
      lock.current = false
      setBusy(false)
      setIssue('failed')
    })
  }, [cache, statusOwner, cancelOperation, clear])
  useEffect(() => {
    if (!enrollment) return
    const timer = setTimeout(
      () => {
        cancelOperation()
        generation.current++
        lock.current = false
        setBusy(false)
        clear()
        setIssue('expired')
        void cache.invalidateQueries({ queryKey: ['account', 'mfa'] })
      },
      Math.max(0, Date.parse(enrollment.expires_at) - Date.now()),
    )
    return () => clearTimeout(timer)
  }, [enrollment, cache, cancelOperation, clear])
  useEffect(() => {
    if (!recoveryCodes.length) return
    const timer = setTimeout(() => setRecoveryCodes([]), 5 * 60 * 1000)
    return () => clearTimeout(timer)
  }, [recoveryCodes])
  async function refresh() {
    setIssue('')
    await status.refetch()
  }
  async function cancel() {
    if (lock.current || !session.data || !lifetime.fresh()) return
    const hasEnrollment = !!enrollment || status.data?.enrollment_pending
    clear()
    setIssue('')
    generation.current++
    if (!hasEnrollment) return
    const operation = lifetime.start()
    if (!operation) return
    const turn = generation.current
    lock.current = true
    setBusy(true)
    try {
      await cancelMFAEnrollment(operation.session.csrf_token, operation.signal)
      if (operation.current() && turn === generation.current) setNotice('canceled')
    } catch {
      if (operation.current() && turn === generation.current) setIssue('cancelFailed')
    } finally {
      if (operation.current() && turn === generation.current) {
        operation.settle()
        lock.current = false
        setBusy(false)
        void status.refetch()
      }
    }
  }
  async function rotate(next: Session, operation: NonNullable<ReturnType<typeof lifetime.start>>) {
    await cache.cancelQueries()
    if (!operation.current()) return null
    operation.settle()
    const installed = cache.setQueryData<Session>(sessionKey, next)
    void cache.resetQueries({ predicate: (query) => query.queryKey[0] !== 'auth' })
    return installed ?? null
  }
  function open(next: Action) {
    if (lock.current || !lifetime.fresh() || !statusCurrent()) return
    clear()
    setRecoveryCodes([])
    setIssue('')
    setNotice('')
    const identity = Symbol()
    dialogOwner.current = identity
    setDialogIdentity(identity)
    setAction(next)
  }
  async function submit(event: FormEvent, capturedDialog: symbol | null, submittedAt: number) {
    event.preventDefault()
    if (
      !capturedDialog ||
      dialogOwner.current !== capturedDialog ||
      lock.current ||
      !action ||
      !session.data ||
      !lifetime.fresh() ||
      !statusCurrent()
    )
      return
    if (!password) {
      setIssue('passwordRequired')
      return
    }
    if (enrollment && Date.parse(enrollment.expires_at) <= submittedAt) {
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
    const operation = lifetime.start()
    if (!operation) return
    const turn = ++generation.current
    lock.current = true
    setBusy(true)
    setIssue('')
    setNotice('')
    setCode('')
    try {
      if (action === 'enable' && !captured) {
        const next = await beginMFAEnrollment(
          currentPassword,
          operation.session.csrf_token,
          operation.signal,
        )
        if (operation.current() && turn === generation.current) setEnrollment(next)
      } else {
        let result: SecuritySessionResult | MFARecovery
        if (action === 'enable')
          result = await enableMFA(
            {
              current_password: currentPassword,
              enrollment_token: captured!.enrollment_token,
              code: 'code' in proof ? proof.code! : '',
            },
            operation.session.csrf_token,
            operation.signal,
          )
        else if (action === 'disable')
          result = await changeMFA(
            'disable',
            { current_password: currentPassword, ...proof },
            operation.session.csrf_token,
            operation.signal,
          )
        else
          result = await changeMFA(
            'recovery-codes',
            { current_password: currentPassword, ...proof },
            operation.session.csrf_token,
            operation.signal,
          )
        if (!operation.current() || turn !== generation.current) return
        if ('reauthentication_required' in result) {
          operation.settle()
          clear()
          reauthenticate('recovery_codes' in result ? result.recovery_codes : [])
          return
        }
        const nextSession = 'session' in result ? result.session : result
        const installed = await rotate(nextSession, operation)
        if (!installed || !lifetime.fresh() || cache.getQueryData(sessionKey) !== installed) return
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
      if (!operation.current() || turn !== generation.current) return
      const code = error instanceof MFARequestError ? error.status : 0
      setIssue(code === 401 ? 'invalid' : code === 409 ? 'conflict' : 'failed')
      setPassword('')
      if (code !== 401) {
        clear()
        void status.refetch()
      }
    } finally {
      if (operation.current() && turn === generation.current) {
        operation.settle()
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
          {lifetime.fresh() && statusCurrent() && status.data && (
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
        open={!!action && lifetime.fresh() && statusCurrent()}
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
        <form
          className="space-y-5"
          aria-label={t(title)}
          onSubmit={(event) => {
            const submittedAt = Date.now()
            void submit(event, dialogIdentity, submittedAt)
          }}
        >
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
        open={recoveryCodes.length > 0 && lifetime.fresh()}
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
