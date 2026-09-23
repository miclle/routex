import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { LanguageSwitcher } from '@/components/app/LanguageSwitcher'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useLocation, useNavigate } from 'react-router'
import axios from 'axios'
import { ArrowRight, LoaderCircle, Route } from 'lucide-react'
import { getRegistration, register } from '@/api/governance'
import { authError, login, setup } from '@/api/auth'
import { sessionKey, setupKey } from '@/hooks/use-auth'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { Session, SetupInput } from '@/types/auth'
import type { MFAChallenge, MFAProof } from '@/types/mfa'
import { verifyMFALogin, MFARequestError } from '@/api/mfa'
import MFAChallengeForm from './mfa-challenge'

export default function AuthPage({ mode }: { mode: 'login' | 'setup' | 'register' }) {
  useTranslation()

  const isSetup = mode === 'setup'
  const isRegister = mode === 'register'
  const registration = useQuery({
    queryKey: ['auth', 'registration'],
    queryFn: () => getRegistration(),
    enabled: !isSetup,
    retry: false,
  })
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  const [validation, setValidation] = useState('')
  const [pending, setPending] = useState(false)
  const [errorStatus, setErrorStatus] = useState<number | null>(null)
  const [challenge, setChallenge] = useState<MFAChallenge | null>(null)
  const attempt = useRef(0)
  const request = useRef<AbortController | null>(null)
  const locked = useRef(false)
  useEffect(
    () => () => {
      attempt.current++
      request.current?.abort()
    },
    [],
  )
  async function complete(session: Session, turn: number) {
    await queryClient.cancelQueries()
    if (turn !== attempt.current) return
    queryClient.clear()
    queryClient.setQueryData(setupKey, { initialized: true })
    queryClient.setQueryData(sessionKey, session)
    setChallenge(null)
    navigate('/', { replace: true })
  }
  function restart(expired = false) {
    attempt.current++
    request.current?.abort()
    locked.current = false
    setPending(false)
    setChallenge(null)
    setErrorStatus(null)
    setValidation(expired ? 'mfa:expired' : '')
  }
  async function authenticate(input: SetupInput) {
    if (locked.current) return
    locked.current = true
    setPending(true)
    setErrorStatus(null)
    const turn = ++attempt.current
    request.current = new AbortController()
    try {
      const result = isSetup
        ? await setup(input)
        : isRegister
          ? await register(input)
          : await login({ email: input.email, password: input.password }, request.current.signal)
      if (turn !== attempt.current) return
      if ('kind' in result) {
        if (result.kind === 'challenge') {
          await queryClient.cancelQueries({ queryKey: sessionKey })
          if (turn !== attempt.current) return
          queryClient.setQueryData(sessionKey, null)
          setChallenge(result.challenge)
        } else await complete(result.session, turn)
      } else await complete(result, turn)
    } catch (error) {
      if (turn !== attempt.current) return
      const status = axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0
      setErrorStatus(status)
      if (isSetup && status === 409) {
        queryClient.setQueryData(setupKey, { initialized: true })
        navigate('/login', {
          replace: true,
          state: { messageKey: 'this_site_is_already_set_up_sign_in_337e8' },
        })
      }
    } finally {
      if (turn === attempt.current) {
        locked.current = false
        setPending(false)
      }
    }
  }
  async function verify(proof: MFAProof) {
    if (!challenge || locked.current) return
    if (Date.parse(challenge.expires_at) <= Date.now()) {
      restart(true)
      return
    }
    locked.current = true
    setPending(true)
    setValidation('')
    setErrorStatus(null)
    const turn = ++attempt.current
    request.current = new AbortController()
    try {
      await complete(
        await verifyMFALogin(challenge.challenge_token, proof, request.current.signal),
        turn,
      )
    } catch (error) {
      if (turn === attempt.current)
        setValidation(
          error instanceof MFARequestError && error.status === 401 ? 'mfa:invalid' : 'mfa:failed',
        )
    } finally {
      if (turn === attempt.current) {
        locked.current = false
        setPending(false)
      }
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (locked.current) return
    const data = new FormData(event.currentTarget)
    const password = String(data.get('password') ?? '')
    const name = String(data.get('name') ?? '').trim()
    if (isSetup || isRegister) {
      const bytes = new TextEncoder().encode(password).length
      if (bytes < 12 || bytes > 72) {
        setValidation('passwords_must_contain_12_72_utf_8_bytes_00719')
        return
      }
      if (isSetup && password !== data.get('confirmPassword')) {
        setValidation('the_passwords_do_not_match_d2ee6')
        return
      }
      if (!name) {
        setValidation(isSetup ? 'enter_the_administrator_s_name_b7bf7' : 'enter_a_name_9d2ac')
        return
      }
    }
    setValidation('')
    const input = { email: String(data.get('email') ?? '').trim(), password, name }
    for (const field of ['password', 'confirmPassword']) {
      const element = event.currentTarget.elements.namedItem(field)
      if (element instanceof HTMLInputElement) element.value = ''
    }
    void authenticate(input)
  }

  const notice = typeof location.state?.messageKey === 'string' ? location.state.messageKey : ''
  const error =
    (validation ? t(validation) : '') ||
    (errorStatus !== null
      ? isRegister && errorStatus === 409
        ? t('this_email_is_already_registered_sign_in_or_e090b')
        : isRegister && errorStatus === 403
          ? t('registration_is_closed_contact_an_administrator_abd22')
          : authError(errorStatus)
      : '')
  if (isRegister && (!registration.data?.enabled || registration.isPending))
    return (
      <main className="flex min-h-screen items-center justify-center p-6">
        <div className="absolute right-6 top-6">
          <LanguageSwitcher />
        </div>
        <section className="w-full max-w-[500px] space-y-6 rounded-lg border p-6 text-center">
          <h1 className="text-2xl font-semibold">{t('create_a_routex_account_125c0')}</h1>
          <p role={registration.isError ? 'alert' : 'status'}>
            {registration.isPending
              ? t('checking_registration_settings_0ee9f')
              : registration.isError
                ? t('unable_to_load_registration_settings_try_again_later_6d1e0')
                : t('registration_is_not_available_contact_an_administrator_a07c8')}
          </p>
          {registration.isError && (
            <Button onClick={() => void registration.refetch()}>{t('retry_e2d53')}</Button>
          )}
          <Link to="/login" className="block underline">
            {t('back_to_sign_in_f2fe4')}
          </Link>
        </section>
      </main>
    )

  return (
    <main className="flex min-h-screen items-center justify-center bg-background p-6">
      <div className="absolute right-6 top-6">
        <LanguageSwitcher />
      </div>
      <section className="w-full max-w-[500px] space-y-6">
        <div aria-label={t('routex_brand_87b96')} className="flex justify-center">
          <span className="flex size-8 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <Route className="size-5" aria-hidden="true" />
          </span>
        </div>
        <div className="space-y-6 rounded-lg border p-6">
          <header className="space-y-3 text-center">
            <h1 className="text-[30px] font-semibold leading-[38px]">
              {challenge
                ? t('mfa:loginTitle')
                : isSetup
                  ? t('set_up_routex_ca2f9')
                  : isRegister
                    ? t('join_routex_6a64f')
                    : t('sign_in_to_your_model_console_ff10a')}
            </h1>
            {isSetup && (
              <p className="text-sm leading-6 text-muted-foreground">
                {t('create_the_first_administrator_account_you_will_be_43608')}
              </p>
            )}
          </header>
          {notice && (
            <p role="status" className="rounded-md border bg-muted/40 p-3 text-sm leading-6">
              {t(notice)}
            </p>
          )}
          {challenge ? (
            <MFAChallengeForm
              challenge={challenge}
              busy={pending}
              error={error}
              verify={(proof) => void verify(proof)}
              restart={restart}
            />
          ) : (
            <form
              aria-label={
                isSetup
                  ? t('create_administrator_ffbc3')
                  : isRegister
                    ? t('register_da0e5')
                    : t('sign_in_21f1e')
              }
              onSubmit={submit}
              className="space-y-5"
            >
              <fieldset disabled={pending} className="space-y-5">
                {(isSetup || isRegister) && (
                  <div className="space-y-2">
                    <label htmlFor="name" className="text-sm font-medium">
                      {isSetup ? t('administrator_name_df47b') : t('name_be4c2')}
                    </label>
                    <Input id="name" name="name" autoComplete="name" required maxLength={100} />
                  </div>
                )}
                <div className="space-y-2">
                  <label htmlFor="email" className="text-sm font-medium">
                    {t('work_email_84e37')}
                  </label>
                  <Input
                    id="email"
                    name="email"
                    type="email"
                    autoComplete="username"
                    placeholder="you@company.com"
                    required
                    maxLength={254}
                  />
                </div>
                <div className="space-y-2">
                  <label htmlFor="password" className="text-sm font-medium">
                    {t('password_c839a')}
                  </label>
                  <Input
                    id="password"
                    name="password"
                    type="password"
                    autoComplete={isSetup || isRegister ? 'new-password' : 'current-password'}
                    required
                    aria-describedby={isSetup || isRegister ? 'password-hint' : undefined}
                  />
                  {(isSetup || isRegister) && (
                    <p id="password-hint" className="text-xs leading-5 text-muted-foreground">
                      {t('use_12_72_utf_8_bytes_a_long_90631')}
                    </p>
                  )}
                </div>
                {isSetup && (
                  <div className="space-y-2">
                    <label htmlFor="confirmPassword" className="text-sm font-medium">
                      {t('confirm_password_1d016')}
                    </label>
                    <Input
                      id="confirmPassword"
                      name="confirmPassword"
                      type="password"
                      autoComplete="new-password"
                      required
                    />
                  </div>
                )}
              </fieldset>
              {error && (
                <p
                  role="alert"
                  className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm leading-6 text-destructive"
                >
                  {error}
                </p>
              )}
              <Button type="submit" size="lg" disabled={pending} className="w-full">
                {pending ? (
                  <LoaderCircle className="size-4 animate-spin" aria-hidden="true" />
                ) : null}
                {pending
                  ? t('submitting_10b2d')
                  : isSetup
                    ? t('create_administrator_and_continue_9e9d6')
                    : isRegister
                      ? t('create_account_249a9')
                      : t('sign_in_21f1e')}
                {!pending && <ArrowRight className="size-4" aria-hidden="true" />}
              </Button>
            </form>
          )}
        </div>
        {!challenge && (
          <p className="text-center text-sm leading-5 text-muted-foreground">
            {isSetup ? (
              t('each_site_can_be_initialized_once_keep_your_0efb2')
            ) : isRegister ? (
              <Link to="/login">{t('already_have_an_account_sign_in_b5a87')}</Link>
            ) : registration.data?.enabled ? (
              <Link to="/register">{t('need_an_account_register_5b4ad')}</Link>
            ) : (
              t('need_an_account_contact_your_organization_s_administrator_5477e')
            )}
          </p>
        )}
      </section>
    </main>
  )
}
