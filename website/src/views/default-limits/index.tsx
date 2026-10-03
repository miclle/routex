import { useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { isAxiosError } from 'axios'
import { UserRound, UsersRound } from 'lucide-react'
import { getDefaultLimits, saveDefaultLimits } from '@/api/default-limits'
import { useSession, sessionKey } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, FormField, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import type { Session } from '@/types/auth'
import {
  defaultIntegerFields,
  type DefaultLimitKind,
  type DefaultLimitRecord,
  type DefaultLimitInput,
  type DefaultLimitPolicy,
} from '@/types/default-limits'
import { parseInteger, validMoney } from '@/views/resource-limits/quota-values'
import { PolicyRows } from './policy'
import { policyDraft } from './policy-values'

export default function DefaultLimitsPage() {
  const { t } = useTranslation('defaultLimits')
  const session = useSession()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const [kind, setKind] = useState<DefaultLimitKind>('user')
  const [protectedIntent, setProtectedIntent] = useState(false)
  return (
    <PermissionGate permission={['system.read', 'limits.settings.write']}>
      <Page title={t('title')} description={t('futureOnly')}>
        <Tabs value={kind} onValueChange={(value) => setKind(value as DefaultLimitKind)}>
          <TabsList>
            {(['user', 'team'] as const).map((target) => (
              <TabsTrigger
                key={target}
                value={target}
                disabled={protectedIntent && target !== kind}
              >
                {target === 'user' ? (
                  <UserRound className="mr-2 size-4" />
                ) : (
                  <UsersRound className="mr-2 size-4" />
                )}
                {t(target)}
              </TabsTrigger>
            ))}
          </TabsList>
          <TabsContent value={kind}>
            {actor && (
              <DefaultPanel
                key={`${actor}:${kind}`}
                actor={actor}
                kind={kind}
                protect={setProtectedIntent}
              />
            )}
          </TabsContent>
        </Tabs>
      </Page>
    </PermissionGate>
  )
}
function DefaultPanel({
  actor,
  kind,
  protect,
}: {
  actor: string
  kind: DefaultLimitKind
  protect: (value: boolean) => void
}) {
  const { t } = useTranslation('defaultLimits')
  const cache = useQueryClient()
  const access = usePermissions()
  const queryKey = ['default-limits', actor, kind]
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => getDefaultLimits(kind, signal),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const [editing, setEditing] = useState(false)
  const [saved, setSaved] = useState(false)
  const fresh = query.isSuccess && !query.isFetching && !access.isError && !access.isFetching
  const canEdit = access.can('limits.settings.write') && !!query.data?.editable
  return (
    <section aria-label={t(kind)} className="space-y-6">
      <QueryState
        pending={query.isFetching}
        error={query.error}
        retry={() => void query.refetch()}
      />
      {saved && <p role="status">{t('saved')}</p>}
      {fresh && query.data && !editing && (
        <>
          <header className="flex items-start justify-between gap-4">
            <div>
              <h2 className="font-medium">{t(kind)}</h2>
              <p className="mt-1 text-sm text-muted-foreground">{t(`${kind}Help`)}</p>
            </div>
            {canEdit && (
              <Button
                variant="outline"
                onClick={() => {
                  setSaved(false)
                  setEditing(true)
                }}
              >
                {t('edit')}
              </Button>
            )}
          </header>
          <PolicyRows policy={query.data.policy} currency={query.data.platform_currency} />
        </>
      )}
      {editing && query.data && (
        <DefaultEditor
          actor={actor}
          kind={kind}
          protect={protect}
          current={query.data}
          visible={fresh}
          canEdit={canEdit && fresh}
          reload={async () => {
            const result = await query.refetch()
            if (!result.data || result.error)
              throw result.error ?? new Error('Default rule unavailable')
            return result.data
          }}
          close={() => setEditing(false)}
          saved={(record) => {
            cache.setQueryData(queryKey, record)
            setSaved(true)
            setEditing(false)
          }}
        />
      )}
    </section>
  )
}
function DefaultEditor({
  actor,
  kind,
  current,
  visible,
  canEdit,
  protect,
  reload,
  close,
  saved,
}: {
  actor: string
  kind: DefaultLimitKind
  current: DefaultLimitRecord
  visible: boolean
  canEdit: boolean
  protect: (value: boolean) => void
  reload: () => Promise<DefaultLimitRecord>
  close: () => void
  saved: (record: DefaultLimitRecord) => void
}) {
  const { t } = useTranslation('defaultLimits')
  const cache = useQueryClient()
  const [reviewed, setReviewed] = useState(current)
  const [draft, setDraft] = useState(() => policyDraft(current.policy))
  const [reason, setReason] = useState('')
  const [issue, setIssue] = useState<string | null>(null)
  const [uncertain, setUncertain] = useState(false)
  const [busy, setBusy] = useState(false)
  const lock = useRef(false)
  const alive = useRef(true)
  const intent = useRef<{ etag: string; input: DefaultLimitInput } | null>(null)
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  useLayoutEffect(() => {
    protect(busy || uncertain)
    return () => protect(false)
  }, [busy, uncertain, protect])
  const currentActor = () =>
    alive.current && cache.getQueryData<Session>(sessionKey)?.user.id === actor
  const stale = reviewed.etag !== current.etag
  const blocked = stale || issue === 'conflict' || issue === 'failed'
  async function dispatch(retry = false) {
    const session = cache.getQueryData<Session>(sessionKey)
    if (
      !currentActor() ||
      !canEdit ||
      !session ||
      lock.current ||
      (!retry && (blocked || uncertain))
    )
      return
    if (!retry) {
      const numbers = Object.fromEntries(
        defaultIntegerFields.map((field) => [field, parseInteger(draft[field])]),
      )
      const money = draft.money_month.trim() || null
      if (
        defaultIntegerFields.some((field) => numbers[field] === undefined) ||
        (money !== null && !validMoney(money))
      ) {
        setIssue('invalid')
        return
      }
      if (
        !reason.trim() ||
        new TextEncoder().encode(reason.trim()).length > 1024 ||
        /\p{Cc}/u.test(reason.trim())
      ) {
        setIssue('requiredReason')
        return
      }
      intent.current = {
        etag: reviewed.etag,
        input: {
          policy: {
            ...numbers,
            money_month: money,
            currency: money === null ? '' : reviewed.platform_currency,
          } as DefaultLimitPolicy,
          reason: reason.trim(),
        },
      }
    }
    if (!intent.current) return
    lock.current = true
    setBusy(true)
    try {
      const result = await saveDefaultLimits(
        kind,
        intent.current.etag,
        intent.current.input,
        session.csrf_token,
      )
      if (currentActor()) saved(result)
    } catch (error) {
      if (currentActor()) {
        const status = isAxiosError(error) ? error.response?.status : undefined
        if (!status || status >= 500) setUncertain(true)
        setIssue(status === 409 ? 'conflict' : !status || status >= 500 ? 'uncertain' : 'failed')
      }
    } finally {
      lock.current = false
      if (currentActor()) setBusy(false)
    }
  }
  async function review() {
    if (lock.current) return
    lock.current = true
    setBusy(true)
    try {
      const result = await reload()
      if (currentActor() && !uncertain) {
        setReviewed(result)
        intent.current = null
        setIssue(null)
      }
    } catch {
      /* Retain the original recovery state until an authorized review succeeds. */
    } finally {
      lock.current = false
      if (currentActor()) setBusy(false)
    }
  }
  return (
    <form
      aria-label={t('edit')}
      onSubmit={(event) => {
        event.preventDefault()
        void dispatch()
      }}
      className="space-y-6"
    >
      {(uncertain || issue || stale) && (
        <p role="alert">{t(uncertain ? 'uncertain' : stale ? 'conflict' : issue!)}</p>
      )}
      {visible && (
        <fieldset disabled={busy || uncertain || !canEdit} className="space-y-6">
          <header className="flex items-start justify-between gap-4">
            <div>
              <h2 className="font-medium">{t(kind)}</h2>
              <p className="text-sm text-muted-foreground">{t(`${kind}Help`)}</p>
            </div>
            <div className="flex gap-2">
              <Button type="button" variant="outline" onClick={close}>
                {t('cancel')}
              </Button>
              <Button type="submit" disabled={busy || blocked || uncertain || !canEdit}>
                {t('save')}
              </Button>
            </div>
          </header>
          <p className="text-sm text-muted-foreground">{t('zeroHelp')}</p>
          <PolicyRows
            policy={reviewed.policy}
            draft={draft}
            change={(field, value) => setDraft((old) => ({ ...old, [field]: value }))}
            currency={reviewed.platform_currency}
          />
          <FormField label={t('reason')}>
            <Input aria-label={t('reason')} value={reason} onValueChange={setReason} />
          </FormField>
        </fieldset>
      )}
      {(blocked || uncertain || !visible) && (
        <Button type="button" variant="outline" disabled={busy} onClick={() => void review()}>
          {t('review')}
        </Button>
      )}
      {uncertain && (
        <Button type="button" disabled={busy || !canEdit} onClick={() => void dispatch(true)}>
          {t('retry')}
        </Button>
      )}
    </form>
  )
}
