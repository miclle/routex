import { useLayoutEffect, useRef, useState } from 'react'
import OIDCConfiguration from '@/views/oidc/config'
import OAuthConfiguration from '@/views/oauth/config'
import LDAPConfiguration from '@/views/ldap/config'
import SAMLConfiguration from '@/views/saml/config'
import GitHubConfiguration from '@/views/github/config'
import GoogleConfiguration from '@/views/google/config'
import DiscordConfiguration from '@/views/discord/config'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { AxiosError } from 'axios'
import { Mail, X } from 'lucide-react'
import {
  addRegistrationDomain,
  isCanonicalRegistrationDomains,
  sameRegistrationDomains,
} from '@/lib/registration-domains'
import { Input } from '@/components/ui/input'
import {
  getRegistrationPolicyReview,
  setRegistrationPolicy,
  validApprovalReason,
} from '@/api/registration-approval'
import type {
  RegistrationPolicyInput,
  RegistrationPolicyReview,
} from '@/types/registration-approval'
import type { Session } from '@/types/auth'
import { useSession } from '@/hooks/use-auth'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, QueryState, FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Drawer } from '@/components/ui/drawer'
import { Dialog } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { Badge } from '@/components/ui/badge'
import { approvalActorCurrent, useApprovalCacheRevision } from './approval-authority'
export default function RegistrationPage() {
  return <Registration />
}
function Registration() {
  const session = useSession()
  return <Policy key={session.data?.user.id ?? ''} actor={session.data?.user.id ?? ''} />
}
function Policy({ actor }: { actor: string }) {
  const { t } = useTranslation('governance'),
    cache = useQueryClient(),
    generation = useSessionGeneration()
  const parent = useApprovalCacheRevision([
    ['auth', 'session'],
    ['permissions', actor],
  ])
  function authority() {
    return (
      parent.snapshot() === parent.revision &&
      approvalActorCurrent(cache, actor, 'registration.write', true)
    )
  }
  const queryKey = ['admin', 'registration', actor, generation, parent.revision]
  const settings = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      if (!authority()) throw new Error('Registration authority unavailable')
      const result = await getRegistrationPolicyReview(signal)
      if (signal.aborted || !authority()) throw new Error('Registration authority unavailable')
      return result
    },
    enabled: authority(),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const resource = useApprovalCacheRevision([queryKey])
  function current() {
    const s = cache.getQueryState<RegistrationPolicyReview>(queryKey)
    return (
      authority() &&
      resource.snapshot() === resource.revision &&
      settings.isSuccess &&
      !settings.isFetching &&
      s?.status === 'success' &&
      s.fetchStatus === 'idle' &&
      !s.isInvalidated &&
      !s.error &&
      s.data === settings.data
    )
  }
  const page = current() ? settings.data : undefined
  const [needsReview, setNeedsReview] = useState(false)
  const [confirmed, setConfirmed] = useState<RegistrationPolicyReview | null>(null)
  const [open, setOpen] = useState(false),
    [confirmation, setConfirmation] = useState(false),
    [draft, setDraft] = useState<RegistrationPolicyInput>({
      enabled: false,
      approval_required: false,
      allowed_email_domains: [],
      reason: '',
    }),
    [review, setReview] = useState<RegistrationPolicyReview | null>(null),
    [intent, setIntent] = useState<{ etag: string; body: RegistrationPolicyInput } | null>(null),
    [notice, setNotice] = useState<
      'saved' | 'uncertain' | 'conflict' | 'abandoned' | 'invalidReason' | null
    >(null),
    [busyScope, setBusyScope] = useState<string | null>(null)
  const [domainInput, setDomainInput] = useState('')
  const [domainError, setDomainError] = useState<'invalid' | 'duplicate' | 'limit' | null>(null)
  function addDomain() {
    if (busy || intent || !current()) return
    const result = addRegistrationDomain(draft.allowed_email_domains, domainInput)
    if (result.kind !== 'added') {
      setDomainError(result.kind)
      return
    }
    setDraft({ ...draft, allowed_email_domains: result.domains })
    setDomainInput('')
    setDomainError(null)
    setConfirmation(false)
  }
  const scope = `${generation}:${parent.revision}:${resource.revision}`
  const busy = busyScope === scope
  const alive = useRef(true),
    serial = useRef(0),
    locked = useRef(false),
    controller = useRef<AbortController | null>(null),
    trigger = useRef<HTMLButtonElement | null>(null)
  useLayoutEffect(() => {
    alive.current = true
    const counter = serial
    return () => {
      alive.current = false
      counter.current++
      controller.current?.abort()
    }
  }, [])
  const fresh = !!page
  useLayoutEffect(() => {
    if (!fresh) {
      serial.current++
      controller.current?.abort()
      locked.current = false
    }
  }, [fresh, generation, parent.revision])
  const stale =
    !!page && !!review && (page.review_etag !== review.review_etag || needsReview) && !intent
  async function dispatch(retry = false) {
    if (locked.current || !current()) return
    let captured = intent
    if (!retry) {
      if (intent || !review || !confirmation || stale || review.review_etag !== page?.review_etag)
        return
      if (!validApprovalReason(draft.reason)) {
        setNotice('invalidReason')
        return
      }
      if (domainInput || !isCanonicalRegistrationDomains(draft.allowed_email_domains)) {
        setDomainError('invalid')
        return
      }
      captured = {
        etag: review.review_etag,
        body: { ...draft, allowed_email_domains: [...draft.allowed_email_domains] },
      }
    }
    const auth = cache.getQueryData<Session>(['auth', 'session'])
    if (!captured || !auth?.csrf_token) return
    const turn = ++serial.current
    locked.current = true
    setBusyScope(scope)
    setNotice(null)
    setIntent(captured)
    controller.current = new AbortController()
    try {
      const receipt = await setRegistrationPolicy(
        captured.etag,
        captured.body,
        auth.csrf_token,
        controller.current.signal,
      )
      if (!alive.current || turn !== serial.current || !current()) return
      setIntent(null)
      setConfirmation(false)
      setOpen(false)
      setReview(null)
      setDraft({ enabled: false, approval_required: false, allowed_email_domains: [], reason: '' })
      setDomainInput('')
      setDomainError(null)
      setConfirmed(receipt)
      setNotice('saved')
      void cache.invalidateQueries({ queryKey: ['admin', 'registration'] })
      void cache.invalidateQueries({ queryKey: ['auth', 'registration'] })
    } catch (error) {
      if (!alive.current || turn !== serial.current) return
      setConfirmation(false)
      const status = error instanceof AxiosError ? error.response?.status : 0
      setNotice('uncertain')
      if (status === 401 || status === 403) {
        void cache.invalidateQueries({ queryKey: ['auth', 'session'] })
        void cache.invalidateQueries({ queryKey: ['permissions', actor] })
      } else if (status === 409 || status === 412) {
        setNeedsReview(true)
        setNotice('uncertain')
        void settings.refetch()
      } else setNotice('uncertain')
    } finally {
      if (alive.current && turn === serial.current) {
        locked.current = false
        setBusyScope(null)
      }
    }
  }
  const content = (
    <Page title={t('registration.title')} description={t('registration.description')}>
      <div>
        <h2 className="text-base font-semibold">{t('registration.methods')}</h2>
        <p className="mt-2 text-sm text-muted-foreground">{t('registration.methodsDescription')}</p>
      </div>
      {notice === 'saved' &&
        page &&
        confirmed &&
        page.review_etag === confirmed.review_etag &&
        page.enabled === confirmed.enabled &&
        page.approval_required === confirmed.approval_required &&
        sameRegistrationDomains(page.allowed_email_domains, confirmed.allowed_email_domains) && (
          <p role="status">{t('registrationApproval.policySaved')}</p>
        )}
      <QueryState
        pending={authority() && settings.isFetching}
        error={authority() ? settings.error : null}
        retry={() => void settings.refetch()}
      />
      {page && (
        <section className="flex items-center justify-between gap-6 rounded-lg border p-4">
          <div className="flex items-center gap-4">
            <Mail aria-hidden className="size-6" />
            <div>
              <p className="flex items-center gap-2 font-medium">
                {t('registration.emailRegistration')}
                <Badge variant="outline">
                  {t(page.enabled ? 'registration.enabled' : 'registration.notEnabled')}
                </Badge>
              </p>
              <p className="mt-2 text-sm text-muted-foreground">
                {t('registration.emailDescription')}
              </p>
            </div>
          </div>
          <Button
            variant="outline"
            aria-label={t('registration.configureLabel')}
            onClick={() => {
              if (current()) {
                if (!intent && !open) {
                  setReview(page)
                  setDraft({
                    enabled: page.enabled,
                    approval_required: page.approval_required,
                    allowed_email_domains: [...page.allowed_email_domains],
                    reason: '',
                  })
                  setNotice(null)
                  setDomainInput('')
                  setDomainError(null)
                }
                setOpen(true)
              }
            }}
          >
            {t('registration.configure')}
          </Button>
        </section>
      )}
      <OIDCConfiguration />
      <OAuthConfiguration />
      <LDAPConfiguration />
      <SAMLConfiguration />
      <GitHubConfiguration />
      <GoogleConfiguration />
      <DiscordConfiguration />
      <Drawer
        open={open && !!page}
        onOpenChange={(value) => {
          if (!busy) {
            setOpen(value)
            if (!value) setConfirmation(false)
          }
        }}
        title={t('registration.configureLabel')}
        busy={busy}
      >
        <form
          className="space-y-6"
          aria-label={t('registration.formLabel')}
          onSubmit={(event) => {
            event.preventDefault()
            if (!current() || intent || busy || stale) return
            if (!validApprovalReason(draft.reason)) {
              setNotice('invalidReason')
              return
            }
            if (domainInput || !isCanonicalRegistrationDomains(draft.allowed_email_domains)) {
              setDomainError('invalid')
              return
            }
            setConfirmation(true)
          }}
        >
          <label className="flex items-center justify-between gap-4">
            <span>
              <span className="block text-sm font-medium">
                {t('registration.openRegistration')}
              </span>
              <span className="text-sm text-muted-foreground">
                {t('registration.memberNotice')}
              </span>
            </span>
            <Switch
              name="enabled"
              aria-label={t('registration.openRegistration')}
              checked={draft.enabled}
              onCheckedChange={(enabled) => setDraft({ ...draft, enabled })}
              disabled={busy || !!intent}
            />
          </label>
          <label className="flex items-center justify-between gap-4">
            <span>
              <span className="block text-sm font-medium">
                {t('registrationApproval.required')}
              </span>
              <span className="text-sm text-muted-foreground">
                {t('registrationApproval.policyHelp')}
              </span>
            </span>
            <Switch
              name="approval_required"
              aria-label={t('registrationApproval.required')}
              checked={draft.approval_required}
              onCheckedChange={(approval_required) => setDraft({ ...draft, approval_required })}
              disabled={busy || !!intent}
            />
          </label>
          <FormField label={t('registrationDomains.label')}>
            <p id="registration-domain-help" className="text-sm text-muted-foreground">
              {t('registrationDomains.help')}
            </p>
            <div className="flex min-h-11 flex-wrap items-center gap-2 rounded-md border border-input bg-background p-2">
              {draft.allowed_email_domains.map((domain) => (
                <Badge key={domain} variant="outline" className="gap-1">
                  {domain}
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="size-5"
                    aria-label={t('registrationDomains.remove', { domain })}
                    disabled={busy || !!intent}
                    onClick={() => {
                      setDraft({
                        ...draft,
                        allowed_email_domains: draft.allowed_email_domains.filter(
                          (entry) => entry !== domain,
                        ),
                      })
                      setConfirmation(false)
                      setDomainError(null)
                    }}
                  >
                    <X aria-hidden className="size-3" />
                  </Button>
                </Badge>
              ))}
              <Input
                className="h-8 min-w-40 flex-1 border-0 p-0 focus-visible:ring-0"
                aria-label={t('registrationDomains.input')}
                aria-describedby="registration-domain-help"
                aria-invalid={!!domainError}
                placeholder={t('registrationDomains.placeholder')}
                value={domainInput}
                disabled={busy || !!intent}
                maxLength={255}
                onChange={(event) => {
                  setDomainInput(event.target.value)
                  setDomainError(null)
                  setConfirmation(false)
                }}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    event.preventDefault()
                    addDomain()
                  }
                }}
              />
              <Button
                type="button"
                variant="outline"
                disabled={busy || !!intent || !domainInput}
                onClick={addDomain}
              >
                {t('registrationDomains.add')}
              </Button>
            </div>
            {domainError && <p role="alert">{t(`registrationDomains.${domainError}`)}</p>}
          </FormField>
          <FormField label={t('registrationApproval.reason')}>
            <Textarea
              aria-label={t('registrationApproval.reason')}
              value={draft.reason}
              onChange={(event) => setDraft({ ...draft, reason: event.target.value })}
              disabled={busy || !!intent}
              maxLength={1024}
            />
          </FormField>
          {notice && notice !== 'saved' && (
            <p role="alert">{t(`registrationApproval.${notice}`)}</p>
          )}
          {stale && <p role="alert">{t('registrationApproval.conflict')}</p>}
          {intent ? (
            <>
              <Button type="button" disabled={busy} onClick={() => void dispatch(true)}>
                {t('registrationApproval.retry')}
              </Button>
              <p>{t('registrationApproval.abandonHelp')}</p>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => {
                  if (busy || locked.current || !intent || !page || !current()) return
                  serial.current++
                  controller.current?.abort()
                  setIntent(null)
                  setReview(page)
                  setNeedsReview(true)
                  setConfirmation(false)
                  setNotice('abandoned')
                }}
              >
                {t('registrationApproval.abandon')}
              </Button>
            </>
          ) : (
            <div className="flex justify-end gap-2">
              {stale && (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    if (page && current()) {
                      setReview(page)
                      setNeedsReview(false)
                      setConfirmation(false)
                      setNotice(null)
                    }
                  }}
                >
                  {t('registrationApproval.reviewCurrent')}
                </Button>
              )}
              <Button ref={trigger} type="submit" disabled={busy || stale}>
                {t('registration.save')}
              </Button>
            </div>
          )}
        </form>
      </Drawer>
      <Dialog
        open={confirmation && !!page && open}
        onOpenChange={setConfirmation}
        title={t('registrationApproval.policyConfirmTitle')}
        description={t('registrationApproval.policyConfirm')}
        busy={busy}
        finalFocus={() => (current() && trigger.current?.isConnected ? trigger.current : false)}
      >
        <div className="space-y-4">
          <p>
            {t('registrationApproval.policyValues', {
              enabled: t(draft.enabled ? 'registration.enabled' : 'registration.notEnabled'),
              approval: t(
                draft.approval_required ? 'registration.enabled' : 'registration.notEnabled',
              ),
            })}
          </p>
          <p className="break-words">
            {t('registrationDomains.confirm', {
              domains: draft.allowed_email_domains.length
                ? draft.allowed_email_domains.join(', ')
                : t('registrationDomains.unrestricted'),
            })}
          </p>
          <p className="break-words">{draft.reason}</p>
          <Button disabled={busy || stale || !!intent} onClick={() => void dispatch()}>
            {t('registrationApproval.confirm')}
          </Button>
          <Button variant="outline" disabled={busy} onClick={() => setConfirmation(false)}>
            {t('common:cancel_4d0b4')}
          </Button>
        </div>
      </Dialog>
    </Page>
  )
  return <PermissionGate permission="registration.write">{content}</PermissionGate>
}
