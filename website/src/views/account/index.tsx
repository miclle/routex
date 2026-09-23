import { useTranslation } from 'react-i18next'
import { useRef, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import {
  changePassword,
  listAccountSessions,
  revokeAccountSession,
  updateAccount,
} from '@/api/account'
import { sessionKey, setupKey, useSession } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { AccountSession } from '@/types/account'
import { Page, QueryState, ErrorNotice, FormField, SaveButton } from '@/components/app/CatalogUI'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Dialog } from '@/components/ui/dialog'

export default function AccountPage({ security = false }: { security?: boolean }) {
  const { t, i18n } = useTranslation('activity')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const { data: session } = useSession()
  const cache = useQueryClient()
  const navigate = useNavigate()
  const sessions = useQuery({
    queryKey: ['account', 'sessions'],
    queryFn: ({ signal }) => listAccountSessions(signal),
  })
  const [notice, setNotice] = useState('')
  const [passwordOpen, setPasswordOpen] = useState(false)
  const [validation, setValidation] = useState('')
  const passwordForm = useRef<HTMLFormElement>(null)
  const [selected, setSelected] = useState<AccountSession | null>(null)
  const profile = useMutation({
    mutationFn: (name: string) => updateAccount(name, session!.csrf_token),
    onSuccess: (user) => {
      cache.setQueryData(sessionKey, (current: Session | undefined) =>
        current ? { ...current, user } : current,
      )
      setNotice('account.profileUpdated')
    },
  })
  const password = useMutation({
    mutationFn: (input: { current_password: string; new_password: string }) =>
      changePassword(input, session!.csrf_token),
    onSuccess: async (nextSession) => {
      await cache.cancelQueries({ queryKey: sessionKey })
      cache.setQueryData(sessionKey, nextSession)
      await cache.invalidateQueries({ queryKey: ['account', 'sessions'] })
      setNotice('account.passwordUpdated')
      passwordForm.current?.reset()
      password.reset()
    },
    gcTime: 0,
  })
  const revoke = useMutation({
    mutationFn: (target: AccountSession) => revokeAccountSession(target.id, session!.csrf_token),
    onSuccess: async (_result, target) => {
      setSelected(null)
      if (target.current) {
        await cache.cancelQueries()
        cache.clear()
        cache.setQueryData(setupKey, { initialized: true })
        cache.setQueryData(sessionKey, null)
        navigate('/login', { replace: true })
      } else {
        await cache.invalidateQueries({ queryKey: ['account', 'sessions'] })
        setNotice('account.sessionRevoked')
      }
    },
  })
  function submitPassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (password.isPending) return
    const form = event.currentTarget
    const values = new FormData(form)
    const newPassword = String(values.get('new_password') ?? '')
    const bytes = new TextEncoder().encode(newPassword).length
    if (bytes < 12 || bytes > 72) {
      setValidation('account.passwordLength')
      return
    }
    if (newPassword !== values.get('confirm_password')) {
      setValidation('account.passwordMismatch')
      return
    }
    setValidation('')
    setNotice('')
    password.mutate({
      current_password: String(values.get('current_password') ?? ''),
      new_password: newPassword,
    })
  }
  if (!session) return null
  return (
    <Page
      title={security ? t('account.security') : t('account.profile')}
      description={t('account.description')}
    >
      {notice && (
        <p role="status" className="rounded-md border bg-card p-3 text-sm">
          {t(notice)}
        </p>
      )}
      {!security && (
        <>
          <div className="flex flex-wrap items-center gap-4 rounded-lg border p-6">
            <span className="flex size-[72px] items-center justify-center rounded-full bg-muted text-2xl">
              {session.user.name.slice(0, 2).toUpperCase()}
            </span>
            <div className="min-w-0 flex-1">
              <h2 className="text-2xl font-semibold">{session.user.name}</h2>
              <p className="mt-2 break-all text-sm text-muted-foreground">{session.user.email}</p>
            </div>
            <Badge variant="outline">
              {session.user.role === 'admin' ? t('account.admin') : t('account.member')}
            </Badge>
          </div>
          <Card>
            <CardHeader>
              <CardTitle>{t('account.basics')}</CardTitle>
              <CardDescription>{t('account.profileDescription')}</CardDescription>
            </CardHeader>
            <CardContent>
              <form
                aria-label={t('account.profile')}
                className="space-y-5 [&_label]:grid [&_label]:grid-cols-[minmax(80px,1fr)_5fr] [&_label]:items-center [&_label]:gap-4"
                onSubmit={(e) => {
                  e.preventDefault()
                  if (profile.isPending) return
                  const name = String(new FormData(e.currentTarget).get('name') ?? '').trim()
                  if (!name) return
                  setNotice('')
                  profile.mutate(name)
                }}
              >
                <fieldset disabled={profile.isPending} className="space-y-5">
                  <FormField label={t('account.name')}>
                    <Input name="name" defaultValue={session.user.name} maxLength={100} required />
                  </FormField>
                  <FormField label={t('account.email')}>
                    <Input value={session.user.email} disabled />
                  </FormField>
                </fieldset>
                <ErrorNotice error={profile.error} />
                <SaveButton pending={profile.isPending}>{t('account.saveProfile')}</SaveButton>
              </form>
            </CardContent>
          </Card>
        </>
      )}
      {security && (
        <>
          <Card>
            <CardHeader>
              <CardTitle>{t('account.password')}</CardTitle>
            </CardHeader>
            <CardContent className="flex items-center justify-between gap-4">
              <div className="space-y-1">
                <p className="text-sm font-medium">{t('account.passwordSet')}</p>
                <p className="text-sm text-muted-foreground">{t('account.passwordEffect')}</p>
              </div>
              <Button variant="outline" onClick={() => setPasswordOpen(true)}>
                {t('account.changePassword')}
              </Button>
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>{t('account.sessions')}</CardTitle>
              <CardDescription>{t('account.sessionsDescription')}</CardDescription>
            </CardHeader>
            <CardContent>
              <QueryState
                pending={sessions.isPending}
                error={sessions.error}
                retry={() => void sessions.refetch()}
                empty={sessions.data?.length === 0}
              />
              <div className="divide-y">
                {sessions.data?.map((item) => (
                  <div
                    key={item.id}
                    className="flex flex-wrap items-center justify-between gap-4 py-4 first:pt-0"
                  >
                    <div className="space-y-2">
                      <p className="break-all font-mono text-xs">
                        {item.id}{' '}
                        {item.current && (
                          <Badge variant="outline">{t('account.currentSession')}</Badge>
                        )}
                      </p>
                      <p className="text-xs leading-6 text-muted-foreground">
                        {t('account.sessionDates', {
                          created: new Date(item.created_at).toLocaleString(locale),
                          expires: new Date(item.expires_at).toLocaleString(locale),
                        })}
                      </p>
                    </div>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        revoke.reset()
                        setSelected(item)
                      }}
                    >
                      {t('account.revokeSession')}
                    </Button>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </>
      )}
      <Dialog
        open={passwordOpen}
        onOpenChange={setPasswordOpen}
        busy={password.isPending}
        title={t('account.changePasswordTitle')}
        description={t('account.changePasswordDescription')}
      >
        <form
          ref={passwordForm}
          aria-label={t('account.changePassword')}
          onSubmit={submitPassword}
          className="space-y-5"
        >
          <fieldset disabled={password.isPending} className="space-y-5">
            <FormField label={t('account.currentPassword')}>
              <Input
                name="current_password"
                type="password"
                autoComplete="current-password"
                required
              />
            </FormField>
            <FormField label={t('account.newPassword')}>
              <Input name="new_password" type="password" autoComplete="new-password" required />
            </FormField>
            <FormField label={t('account.confirmPassword')}>
              <Input name="confirm_password" type="password" autoComplete="new-password" required />
            </FormField>
          </fieldset>
          {validation && (
            <p role="alert" className="text-sm text-destructive">
              {t(validation)}
            </p>
          )}
          <ErrorNotice error={password.error} />
          <SaveButton pending={password.isPending}>{t('account.updatePassword')}</SaveButton>
        </form>
      </Dialog>
      <Dialog
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
        busy={revoke.isPending}
        title={t('account.revokeTitle')}
        description={
          selected?.current
            ? t('account.revokeCurrentDescription')
            : t('account.revokeOtherDescription')
        }
      >
        <div className="space-y-4">
          <ErrorNotice error={revoke.error} />
          <Button
            disabled={revoke.isPending}
            onClick={() => {
              if (selected) revoke.mutate(selected)
            }}
          >
            {revoke.isPending ? t('account.revoking') : t('account.confirmRevoke')}
          </Button>
        </div>
      </Dialog>
    </Page>
  )
}
