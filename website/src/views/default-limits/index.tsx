import { useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { isAxiosError } from 'axios'
import { UserRound, UsersRound } from 'lucide-react'
import { getDefaultLimits, saveDefaultLimits } from '@/api/default-limits'
import { getPermissions } from '@/api/governance'
import { useSession, sessionKey } from '@/hooks/use-auth'
import { useUncertainIntents } from '@/hooks/use-uncertain-intents'
import { Page, FormField, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import type { Session } from '@/types/auth'
import type {
  DefaultLimitSaveSubmittedIntent,
  RetainedSubmittedIntent,
  SubmittedIntentClaim,
} from '@/types/uncertain-intents'
import {
  defaultIntegerFields,
  type DefaultLimitKind,
  type DefaultLimitRecord,
  type DefaultLimitPolicy,
} from '@/types/default-limits'
import { parseInteger, validMoney } from '@/views/resource-limits/quota-values'
import { PolicyRows } from './policy'
import { policyDraft } from './policy-values'

type RetainedSave = Extract<RetainedSubmittedIntent, { kind: 'default-limit-save' }>
export default function DefaultLimitsPage() {
  const { t } = useTranslation('defaultLimits')
  const { t: common } = useTranslation('common')
  const session = useSession()
  const cache = useQueryClient()
  const shared = useUncertainIntents()
  const actor = session.data?.user.id ?? ''
  const sessionState = cache.getQueryState<Session | null>(sessionKey)
  const generation = sessionState?.dataUpdateCount ?? 0
  const sessionFresh =
    session.isSuccess && !session.isFetching && !!actor && !sessionState?.isInvalidated
  const retained = sessionFresh ? shared?.recover(actor) : null
  const recovered = retained?.kind === 'default-limit-save' ? retained : null
  const [selected, setSelected] = useState<DefaultLimitKind>('user')
  const kind = recovered?.payload.target ?? selected
  const scope = JSON.stringify([actor, kind])
  const [panel, setPanel] = useState({ scope, editing: false, saved: false })
  const [editorSeed, setEditorSeed] = useState<{
    scope: string
    record: DefaultLimitRecord
  } | null>(null)
  if (editorSeed && editorSeed.scope !== scope) setEditorSeed(null)
  if (panel.scope !== scope) setPanel({ scope, editing: false, saved: false })
  const [protectedIntent, setProtectedIntent] = useState(false)
  // These existing authority reads are scoped to a successful Session generation.
  // The owner retains submissions only; it cannot authorize this editor.
  const permissionKey = ['permissions', actor, 'default-limits', generation]
  const access = useQuery({
    queryKey: permissionKey,
    queryFn: ({ signal }) => getPermissions(signal),
    enabled: sessionFresh,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    refetchInterval: 30_000,
  })
  const permissionFresh =
    access.isSuccess && !access.isFetching && !cache.getQueryState(permissionKey)?.isInvalidated
  const readable =
    permissionFresh &&
    (access.data.includes('system.read') || access.data.includes('limits.settings.write'))
  const queryKey = ['default-limits', actor, kind, generation]
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => getDefaultLimits(kind, signal),
    enabled: sessionFresh && readable,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const fresh =
    sessionFresh &&
    readable &&
    query.isSuccess &&
    !query.isFetching &&
    !cache.getQueryState(queryKey)?.isInvalidated
  const canEdit =
    fresh && access.data?.includes('limits.settings.write') === true && !!query.data?.editable
  const authorized = () => {
    const currentSession = cache.getQueryState<Session | null>(sessionKey)
    const permission = cache.getQueryState<string[]>(permissionKey)
    const record = cache.getQueryState<DefaultLimitRecord>(queryKey)
    return (
      currentSession?.status === 'success' &&
      currentSession.fetchStatus === 'idle' &&
      !currentSession.isInvalidated &&
      !currentSession.error &&
      currentSession.data?.user.id === actor &&
      currentSession.dataUpdateCount === generation &&
      permission?.status === 'success' &&
      permission.fetchStatus === 'idle' &&
      !permission.isInvalidated &&
      !permission.error &&
      permission.data?.includes('limits.settings.write') === true &&
      record?.status === 'success' &&
      record.fetchStatus === 'idle' &&
      !record.isInvalidated &&
      !record.error &&
      record.data?.kind === kind &&
      record.data.editable
    )
  }
  if (fresh && recovered && (selected !== kind || panel.scope !== scope || !panel.editing)) {
    setSelected(kind)
    setPanel({ scope, editing: true, saved: false })
  }
  const editing = panel.scope === scope && panel.editing
  const saved = panel.scope === scope && panel.saved
  // Keep this mounted editor through renewed reads. The seed is initialization
  // only: fresh Session/permissions/target still control visibility and dispatch.
  const editorRecord = query.data ?? (editorSeed?.scope === scope ? editorSeed.record : null)
  return (
    <Page title={t('title')} description={t('futureOnly')}>
      <QueryState
        pending={session.isFetching || access.isFetching || (readable && query.isFetching)}
        error={session.error ?? access.error ?? query.error}
        retry={() => {
          if (!sessionFresh) void session.refetch()
          else if (access.isError) void access.refetch()
          else void query.refetch()
        }}
      />
      {sessionFresh && permissionFresh && !readable && (
        <p role="alert">{common('your_account_does_not_have_permission_to_access_ca6a8')}</p>
      )}
      <Tabs
        value={kind}
        onValueChange={(value) => {
          if (fresh && !protectedIntent && !recovered) setSelected(value as DefaultLimitKind)
        }}
      >
        {fresh && (
          <TabsList>
            {(['user', 'team'] as const).map((target) => (
              <TabsTrigger
                key={target}
                value={target}
                disabled={(protectedIntent || !!recovered) && target !== kind}
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
        )}
        <TabsContent value={kind}>
          <section aria-label={fresh ? t(kind) : undefined} className="space-y-6">
            {fresh && saved && <p role="status">{t('saved')}</p>}
            {fresh && query.data && !editing && !recovered && (
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
                        if (!authorized()) return
                        setEditorSeed({ scope, record: structuredClone(query.data!) })
                        setPanel({ scope, editing: true, saved: false })
                      }}
                    >
                      {t('edit')}
                    </Button>
                  )}
                </header>
                <PolicyRows policy={query.data.policy} currency={query.data.platform_currency} />
              </>
            )}
            {editorRecord && (editing || !!recovered) && (
              <DefaultEditor
                key={scope}
                actor={actor}
                kind={kind}
                current={editorRecord}
                retained={fresh ? recovered : null}
                visible={fresh}
                canEdit={canEdit}
                authorized={authorized}
                protect={setProtectedIntent}
                reload={async () => {
                  const result = await query.refetch()
                  if (!result.data || result.error)
                    throw result.error ?? new Error('Default rule unavailable')
                  return result.data
                }}
                close={() => {
                  setEditorSeed(null)
                  setPanel({ scope, editing: false, saved: false })
                }}
                saved={(record) => {
                  setEditorSeed(null)
                  cache.setQueryData(queryKey, record)
                  setPanel({ scope, editing: false, saved: true })
                }}
              />
            )}
          </section>
        </TabsContent>
      </Tabs>
    </Page>
  )
}
function DefaultEditor({
  actor,
  kind,
  current,
  retained,
  visible,
  canEdit,
  authorized,
  protect,
  reload,
  close,
  saved,
}: {
  actor: string
  kind: DefaultLimitKind
  current: DefaultLimitRecord
  retained: RetainedSave | null
  visible: boolean
  canEdit: boolean
  authorized: () => boolean
  protect: (value: boolean) => void
  reload: () => Promise<DefaultLimitRecord>
  close: () => void
  saved: (record: DefaultLimitRecord) => void
}) {
  const { t } = useTranslation('defaultLimits')
  const cache = useQueryClient()
  const shared = useUncertainIntents()
  const [reviewed, setReviewed] = useState(current)
  const [draft, setDraft] = useState(() =>
    policyDraft(retained?.payload.input.policy ?? current.policy),
  )
  const [reason, setReason] = useState(retained?.payload.input.reason ?? '')
  const [submittedCurrency, setSubmittedCurrency] = useState(
    retained?.payload.input.policy.currency ?? '',
  )
  const [issue, setIssue] = useState<string | null>(retained ? 'uncertain' : null)
  const [uncertain, setUncertain] = useState(!!retained)
  const [busy, setBusy] = useState(false)
  const [abandon, setAbandon] = useState(false)
  const [requireReview, setRequireReview] = useState(false)
  const lock = useRef(false)
  const alive = useRef(true)
  const request = useRef(0)
  const intent = useRef<DefaultLimitSaveSubmittedIntent | null>(retained?.payload ?? null)
  const claim = useRef<SubmittedIntentClaim | null>(retained?.claim ?? null)
  const previousShared = useRef(shared)
  const [stateShared, setStateShared] = useState(shared)
  const [stateClaim, setStateClaim] = useState(retained?.claim ?? null)
  const abandonTrigger = useRef<HTMLButtonElement | null>(null)
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  if (stateShared !== shared) {
    setStateShared(shared)
    setStateClaim(null)
    setReviewed(current)
    setDraft(policyDraft(current.policy))
    setReason('')
    setSubmittedCurrency('')
    setBusy(false)
    setUncertain(false)
    setAbandon(false)
    setRequireReview(false)
    setIssue(null)
  } else if (visible && retained && stateClaim !== retained.claim) {
    setStateClaim(retained.claim)
    setBusy(false)
    setUncertain(true)
    setIssue('uncertain')
    setDraft(policyDraft(retained.payload.input.policy))
    setReason(retained.payload.input.reason)
    setSubmittedCurrency(retained.payload.input.policy.currency)
    setAbandon(false)
  }
  useLayoutEffect(() => {
    if (previousShared.current !== shared) {
      previousShared.current = shared
      intent.current = null
      claim.current = null
      request.current++
      lock.current = false
    }
    if (!visible || !retained || claim.current === retained.claim) return
    claim.current = retained.claim
    intent.current = retained.payload
    request.current++
    lock.current = false
  }, [shared, visible, retained])
  useLayoutEffect(() => {
    protect(busy || uncertain)
    return () => protect(false)
  }, [busy, uncertain, protect])
  const currentActor = () =>
    alive.current && cache.getQueryData<Session>(sessionKey)?.user.id === actor
  const currentClaim = (captured: SubmittedIntentClaim | null) =>
    !shared || (!!captured && shared.isCurrent(captured))
  const stale = reviewed.etag !== current.etag
  const blocked = stale || requireReview || issue === 'conflict' || issue === 'failed'
  const currency = uncertain ? submittedCurrency || t('saveNoCurrency') : reviewed.platform_currency
  async function dispatch(retry = false) {
    const session = cache.getQueryData<Session>(sessionKey)
    if (
      !currentActor() ||
      !canEdit ||
      !authorized() ||
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
      const submitted: DefaultLimitSaveSubmittedIntent = {
        target: kind,
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
      const captured =
        shared?.capture(actor, { kind: 'default-limit-save', payload: submitted }) ?? null
      if (shared && !captured) return
      intent.current = submitted
      claim.current = captured
      setStateClaim(captured)
      setSubmittedCurrency(submitted.input.policy.currency)
    }
    if (!intent.current || !currentClaim(claim.current)) return
    const submitted = intent.current
    const captured = claim.current
    const operation = ++request.current
    const wasUncertain = uncertain
    setUncertain(true)
    lock.current = true
    setBusy(true)
    try {
      const result = await saveDefaultLimits(
        submitted.target,
        submitted.etag,
        submitted.input,
        session.csrf_token,
      )
      if (
        !currentActor() ||
        request.current !== operation ||
        !authorized() ||
        !currentClaim(captured)
      )
        return
      if (shared && (!captured || !shared.clear(captured))) return
      intent.current = null
      claim.current = null
      saved(result)
    } catch (error) {
      if (
        !currentActor() ||
        request.current !== operation ||
        !authorized() ||
        !currentClaim(captured)
      )
        return
      const status = isAxiosError(error) ? error.response?.status : undefined
      // This endpoint rejects a first reviewed conflict before persistence. An
      // already uncertain request stays retained even if its retry is rejected.
      if (status === 409 && !wasUncertain) {
        if (shared && (!captured || !shared.clear(captured))) return
        claim.current = null
        intent.current = null
        setUncertain(false)
        setIssue('conflict')
      } else {
        setUncertain(true)
        setIssue('uncertain')
      }
    } finally {
      if (currentActor() && request.current === operation && currentClaim(claim.current)) {
        lock.current = false
        setBusy(false)
      } else if (currentActor() && request.current === operation && !intent.current) {
        lock.current = false
        setBusy(false)
      }
    }
  }
  async function review() {
    if (!visible || !currentActor() || lock.current) return
    const operation = ++request.current
    lock.current = true
    setBusy(true)
    try {
      const result = await reload()
      if (currentActor() && request.current === operation && !uncertain && authorized()) {
        setReviewed(result)
        intent.current = null
        setRequireReview(false)
        setIssue(null)
      }
    } catch {
      /* A failed read cannot resolve the original outcome. */
    } finally {
      if (currentActor() && request.current === operation) {
        lock.current = false
        setBusy(false)
      }
    }
  }
  function abandonOriginal() {
    if (!visible || !currentActor() || lock.current || !currentClaim(claim.current)) return
    if (shared && (!claim.current || !shared.clear(claim.current))) return
    claim.current = null
    intent.current = null
    setStateClaim(null)
    request.current++
    setUncertain(false)
    setAbandon(false)
    setRequireReview(true)
    setIssue('saveAbandoned')
  }
  if (!visible) return null
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
        <p role="alert">
          {t(
            uncertain
              ? 'uncertain'
              : issue === 'saveAbandoned'
                ? issue
                : stale
                  ? 'conflict'
                  : issue!,
          )}
        </p>
      )}
      {uncertain && <p className="text-sm text-muted-foreground">{t('capturedSave')}</p>}
      <fieldset disabled={busy || uncertain || !canEdit || requireReview} className="space-y-6">
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
          currency={currency}
        />
        <FormField label={t('reason')}>
          <Input aria-label={t('reason')} value={reason} onValueChange={setReason} />
        </FormField>
      </fieldset>
      {(blocked || uncertain) && (
        <Button type="button" variant="outline" disabled={busy} onClick={() => void review()}>
          {t('review')}
        </Button>
      )}
      {uncertain && (
        <div className="flex gap-2">
          <Button type="button" disabled={busy || !canEdit} onClick={() => void dispatch(true)}>
            {t('retry')}
          </Button>
          <Button
            ref={abandonTrigger}
            type="button"
            variant="outline"
            disabled={busy}
            onClick={() => setAbandon(true)}
          >
            {t('abandonSave')}
          </Button>
        </div>
      )}
      <Dialog
        open={abandon}
        onOpenChange={setAbandon}
        title={t('abandonSaveTitle')}
        description={t('abandonSaveHelp')}
        busy={busy}
        finalFocus={() => (abandonTrigger.current?.isConnected ? abandonTrigger.current : false)}
      >
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={() => setAbandon(false)}>
            {t('cancel')}
          </Button>
          <Button
            type="button"
            className="bg-destructive text-white hover:bg-destructive/90"
            onClick={abandonOriginal}
          >
            {t('confirmAbandonSave')}
          </Button>
        </div>
      </Dialog>
    </form>
  )
}
