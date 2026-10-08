import { useCallback, useLayoutEffect, useRef, useState, type FormEvent } from 'react'
import { useQueryClient, type Query } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import { writeCatalog } from '@/api/catalog'
import { FormField, SaveButton } from '@/components/app/CatalogUI'
import { Input } from '@/components/ui/input'
import { Dialog } from '@/components/ui/dialog'
import type { Member } from '@/types/governance'
import type { Session } from '@/types/auth'
import { approvalActorCurrent, useApprovalCacheRevision } from './approval-authority'

type Notice = 'validation' | 'rejected' | 'uncertain' | null
type Attempt = { revision: string; sessionQuery?: Query; permissionsQuery?: Query }

export default function MemberCreate({
  actor,
  ready,
  onClose,
  onCreated,
}: {
  actor: string
  ready: boolean
  onClose: () => void
  onCreated: (id: string) => void
}) {
  const { t } = useTranslation('governance')
  const cache = useQueryClient()
  const authority = useApprovalCacheRevision([
    ['auth', 'session'],
    ['permissions', actor],
  ])
  const form = useRef<HTMLFormElement>(null)
  const clearForm = useCallback(() => form.current?.reset(), [])
  const mounted = useRef(false)
  const owner = useRef(actor)
  const attempt = useRef<Attempt | null>(null)
  const latest = useRef({ actor, ready, revision: authority.revision })
  const [pending, setPending] = useState(false)
  const [notice, setNotice] = useState<Notice>(null)
  const allowed =
    ready &&
    approvalActorCurrent(cache, actor, 'members.read') &&
    approvalActorCurrent(cache, actor, 'members.write')
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      attempt.current = null
      clearForm()
    }
  }, [clearForm])
  useLayoutEffect(() => {
    latest.current = { actor, ready: allowed, revision: authority.revision }
    if (owner.current !== actor) {
      owner.current = actor
      attempt.current = null
      form.current?.reset()
      setPending(false)
      setNotice(null)
    }
    if (!allowed || (attempt.current && attempt.current.revision !== authority.revision)) {
      form.current?.reset()
      if (attempt.current) {
        setPending(false)
        setNotice('uncertain')
      }
    }
  }, [actor, allowed, authority.revision])

  function current(captured: Attempt) {
    return (
      mounted.current &&
      attempt.current === captured &&
      latest.current.actor === actor &&
      latest.current.ready &&
      latest.current.revision === captured.revision &&
      authority.snapshot() === captured.revision &&
      cache.getQueryCache().find({ queryKey: ['auth', 'session'], exact: true }) ===
        captured.sessionQuery &&
      cache.getQueryCache().find({ queryKey: ['permissions', actor], exact: true }) ===
        captured.permissionsQuery &&
      approvalActorCurrent(cache, actor, 'members.read') &&
      approvalActorCurrent(cache, actor, 'members.write')
    )
  }

  function obsolete(captured: Attempt) {
    if (!mounted.current || attempt.current !== captured || latest.current.actor !== actor) return
    form.current?.reset()
    setPending(false)
    setNotice('uncertain')
  }

  function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (
      attempt.current ||
      !allowed ||
      !mounted.current ||
      authority.snapshot() !== authority.revision
    )
      return
    const auth = cache.getQueryData<Session>(['auth', 'session'])
    if (!auth?.csrf_token) return
    const values = new FormData(event.currentTarget)
    const password = String(values.get('password'))
    const bytes = new TextEncoder().encode(password).length
    if (bytes < 12 || bytes > 72) {
      setNotice('validation')
      return
    }
    const input = {
      name: String(values.get('name')).trim(),
      email: String(values.get('email')).trim(),
      password,
      role: auth.user.role === 'admin' ? String(values.get('role')) : 'member',
    }
    const captured: Attempt = {
      revision: authority.revision,
      sessionQuery: cache.getQueryCache().find({ queryKey: ['auth', 'session'], exact: true }),
      permissionsQuery: cache
        .getQueryCache()
        .find({ queryKey: ['permissions', actor], exact: true }),
    }
    attempt.current = captured
    setPending(true)
    setNotice(null)
    // Plaintext stays outside mutation state and component error state.
    event.currentTarget.querySelector<HTMLInputElement>('[name="password"]')!.value = ''
    void writeCatalog<Member>('post', '/admin/members', input, auth.csrf_token).then(
      (result) => {
        if (!current(captured)) {
          obsolete(captured)
          return
        }
        if (!result || typeof result.id !== 'string' || !/^usr_[A-Za-z0-9]+$/.test(result.id)) {
          setPending(false)
          setNotice('uncertain')
          return
        }
        attempt.current = null
        setPending(false)
        form.current?.reset()
        onCreated(result.id)
      },
      (error: unknown) => {
        if (!current(captured)) {
          obsolete(captured)
          return
        }
        setPending(false)
        const status = axios.isAxiosError(error) ? error.response?.status : undefined
        // A transport/server failure may follow a committed creation. Never replay it automatically.
        if (status !== undefined && [400, 401, 403, 404, 409, 422].includes(status)) {
          attempt.current = null
          setNotice('rejected')
        } else setNotice('uncertain')
      },
    )
  }

  return (
    <Dialog
      open={allowed}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      busy={pending}
      title={t('members.create')}
      description={t('members.createDescription')}
    >
      <form ref={form} onSubmit={create} className="space-y-5">
        <fieldset disabled={pending || notice === 'uncertain'} className="space-y-5">
          <FormField label={t('members.name')}>
            <Input name="name" required maxLength={100} />
          </FormField>
          <FormField label={t('members.email')}>
            <Input name="email" type="email" required maxLength={254} />
          </FormField>
          <FormField label={t('members.initialPassword')}>
            <Input name="password" type="password" autoComplete="new-password" required />
          </FormField>
          {cache.getQueryData<Session>(['auth', 'session'])?.user.role === 'admin' && (
            <FormField label={t('common.baseRole')}>
              <select name="role" className="h-10 w-full rounded-md border bg-background px-3">
                <option value="member">{t('common.member')}</option>
                <option value="admin">{t('common.admin')}</option>
              </select>
            </FormField>
          )}
        </fieldset>
        {notice && (
          <p role="alert" className="text-sm text-destructive">
            {t(
              notice === 'validation'
                ? 'members.passwordValidation'
                : notice === 'uncertain'
                  ? 'members.creationUncertain'
                  : 'members.creationRejected',
            )}
          </p>
        )}
        <SaveButton pending={pending} disabled={notice === 'uncertain'}>
          {t('members.create')}
        </SaveButton>
      </form>
    </Dialog>
  )
}
