import { useEffect, useEffectEvent, useRef, useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  getRoleDefinition,
  setRoleDefinition,
  validRoleDefinitionName,
  validRoleDefinitionReason,
} from '@/api/role-definition'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Dialog } from '@/components/ui/dialog'
import { FormField } from '@/components/app/CatalogUI'
import type { Session } from '@/types/auth'
import type { PlatformRole } from '@/types/governance'
import type { RoleDefinition, RoleDefinitionInput } from '@/types/role-definition'
import { approvalActorCurrent, useApprovalCacheRevision } from './approval-authority'

interface Props {
  actor: string
  target: string
  generation: string
  ready: boolean
  open: boolean
  mode: 'edit' | 'view'
  openEpoch: number
  contextQueryKey: readonly unknown[]
  onClose: () => void
  returnFocus: () => HTMLElement | false
  renderPermissions: (permissions: string[]) => ReactNode
  resourceLabel: (resource: string) => string
  permissionLabel: (permission: string) => string
}
interface Intent {
  actor: string
  target: string
  etag: string
  input: RoleDefinitionInput
}

// Keep this leaf mounted for the same actor/target when renewed reads hide it.
// A failed response cannot distinguish rollback from failed current confirmation.
export function RoleDefinitionEditor({
  actor,
  target,
  generation,
  ready,
  open,
  mode,
  openEpoch,
  contextQueryKey,
  onClose,
  returnFocus,
  renderPermissions,
  resourceLabel,
  permissionLabel,
}: Props) {
  const { t } = useTranslation('governance')
  const cache = useQueryClient()
  const authority = useApprovalCacheRevision([
    ['auth', 'session'],
    ['permissions', actor],
    contextQueryKey,
  ])
  const ownerCurrent = () => {
    const list = cache.getQueryState<{ items: PlatformRole[] }>(contextQueryKey)
    return (
      ready &&
      authority.snapshot() === authority.revision &&
      approvalActorCurrent(cache, actor, 'roles.read') &&
      list?.status === 'success' &&
      list.fetchStatus === 'idle' &&
      !list.isInvalidated &&
      !list.error &&
      list.data?.items.some((row) => row.id === target) === true
    )
  }
  const key = ['admin', 'role-definition', actor, target, generation, authority.revision, openEpoch]
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getRoleDefinition(target, signal),
    enabled: open && ownerCurrent(),
    retry: false,
    refetchOnWindowFocus: true,
  })
  const resource = useApprovalCacheRevision([key])
  const fresh = () => {
    const state = cache.getQueryState<RoleDefinition>(key)
    return (
      open &&
      ownerCurrent() &&
      authority.snapshot() === authority.revision &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      state.data === query.data &&
      state.data?.id === target &&
      resource.snapshot() === resource.revision
    )
  }
  const writable = () =>
    fresh() &&
    approvalActorCurrent(cache, actor, 'roles.read', true) &&
    query.data?.can_edit === true &&
    !query.data.builtin &&
    query.data.identity_etag !== null
  const [review, setReview] = useState<RoleDefinition | null>(null)
  const [name, setName] = useState('')
  const [permissions, setPermissions] = useState<string[]>([])
  const [reason, setReason] = useState('')
  const [intent, setIntent] = useState<Intent | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [confirmation, setConfirmation] = useState<'write' | 'abandon' | null>(null)
  const [busy, setBusy] = useState(false)
  const initialized = useRef(false)
  const pending = useRef<{ controller: AbortController } | null>(null)
  const current = fresh()
  const initializeReview = useEffectEvent(() => {
    if (!initialized.current && fresh() && query.data) {
      initialized.current = true
      setReview(query.data)
      setName(query.data.name)
      setPermissions([...query.data.permissions])
    }
  })
  useEffect(() => {
    initializeReview()
  }, [current, query.data])
  useEffect(() => {
    if (!current) pending.current?.controller.abort()
  }, [current])
  useEffect(() => () => pending.current?.controller.abort(), [])
  const snapshotCurrent =
    review?.review_etag === query.data?.review_etag &&
    review?.identity_etag === query.data?.identity_etag
  const validDraft =
    validRoleDefinitionName(name) &&
    validRoleDefinitionReason(reason) &&
    permissions.length <= 100 &&
    permissions.every((p) => query.data?.available_permissions.includes(p))
  const canConfirmWrite = () =>
    confirmation === 'write' &&
    writable() &&
    !busy &&
    !intent &&
    !!review?.identity_etag &&
    snapshotCurrent &&
    validDraft
  function reviewCurrent() {
    if (!writable() || busy || pending.current || intent || !query.data) return
    initialized.current = true
    setReview(query.data)
    setNotice(null)
  }
  function prepare() {
    if (!writable() || pending.current || busy || intent || !snapshotCurrent || !validDraft) return
    setConfirmation('write')
  }
  async function dispatch(retry: boolean) {
    if (!writable() || busy || pending.current) return
    let exact = intent
    if (!retry) {
      if (
        intent ||
        confirmation !== 'write' ||
        !review?.identity_etag ||
        !snapshotCurrent ||
        !validDraft
      )
        return
      exact = {
        actor,
        target,
        etag: review.review_etag,
        input: { name, permissions: [...permissions], identity_etag: review.identity_etag, reason },
      }
      setIntent(exact)
    }
    if (!exact || exact.actor !== actor || exact.target !== target) return
    const csrf = cache.getQueryData<Session>(['auth', 'session'])?.csrf_token
    if (!csrf) return
    const operation = { controller: new AbortController() }
    const capturedAuthority = authority.snapshot(),
      capturedResource = resource.snapshot()
    pending.current = operation
    setBusy(true)
    setConfirmation(null)
    setNotice('roleDefinition.uncertain')
    try {
      await setRoleDefinition(target, exact.etag, exact.input, csrf, operation.controller.signal)
      if (
        pending.current !== operation ||
        operation.controller.signal.aborted ||
        !writable() ||
        authority.snapshot() !== capturedAuthority ||
        resource.snapshot() !== capturedResource
      )
        return
      setIntent(null)
      setReview(null)
      setNotice('roleDefinition.confirmed')
      for (const queryKey of [
        ['admin', 'roles'],
        ['admin', 'role-definition', actor, target],
        ['permissions'],
        ['admin', 'member-roles'],
        ['admin', 'member-role-candidates'],
        ['admin', 'member-role-definition'],
        ['team-roles'],
        ['team-role-candidates'],
        ['admin', 'member-access-summary'],
      ])
        void cache.invalidateQueries({ queryKey })
    } catch (error) {
      if (pending.current !== operation || operation.controller.signal.aborted) return
      const status = (error as { response?: { status?: number } })?.response?.status
      setNotice(status === 409 ? 'roleDefinition.conflict' : 'roleDefinition.uncertain')
      if (status === 401) void cache.invalidateQueries({ queryKey: ['auth', 'session'] })
      if (status === 403) void cache.invalidateQueries({ queryKey: ['permissions', actor] })
    } finally {
      if (pending.current === operation) {
        pending.current = null
        setBusy(false)
      }
    }
  }
  function close() {
    if (!ownerCurrent() || pending.current) return
    setConfirmation(null)
    onClose()
  }
  if (!open || !ownerCurrent()) return null
  if (!current || !query.data)
    return (
      <Dialog
        open
        onOpenChange={(value) => {
          if (!value) close()
        }}
        title={t(mode === 'view' ? 'roles.view' : 'roles.editTitle')}
        description={t('roleDefinition.reviewHelp')}
        finalFocus={() => (ownerCurrent() ? returnFocus() : false)}
      >
        <p role={query.error ? 'alert' : 'status'}>
          {t(query.error ? 'roleDefinition.readFailed' : 'roleDefinition.loading')}
        </p>
        {query.error && (
          <Button
            onClick={() => {
              if (ownerCurrent()) void query.refetch()
            }}
          >
            {t('roleDefinition.refresh')}
          </Button>
        )}
      </Dialog>
    )
  const choices = [...new Set([...query.data.available_permissions, ...permissions])].sort()
  const unknown = permissions.filter((p) => !query.data.available_permissions.includes(p))
  return (
    <>
      <Dialog
        open={confirmation === null}
        onOpenChange={(value) => {
          if (!value) close()
        }}
        busy={busy}
        width={mode === 'view' ? 640 : 720}
        title={
          mode === 'view' ? t('roles.viewTitle', { name: query.data.name }) : t('roles.editTitle')
        }
        description={t(mode === 'view' ? 'roles.viewDescription' : 'roleDefinition.reviewHelp')}
        finalFocus={() => (ownerCurrent() ? returnFocus() : false)}
      >
        {mode === 'view' ? (
          <>
            {renderPermissions(query.data.permissions)}
            <p className="mt-4 text-sm text-muted-foreground">{t('roleDefinition.recorded')}</p>
          </>
        ) : (
          <div className="space-y-6">
            {notice && <p role="status">{t(notice)}</p>}
            {!writable() && <p>{t('roleDefinition.readOnly')}</p>}
            {review && !snapshotCurrent && <p role="alert">{t('roleDefinition.reviewChanged')}</p>}
            <fieldset disabled={!writable() || busy || !!intent} className="space-y-6">
              <FormField label={t('roles.name')}>
                <Input
                  value={name}
                  onChange={(e) => {
                    if (writable() && !busy && !intent) setName(e.target.value)
                  }}
                />
              </FormField>
              <div className="divide-y">
                {[...new Set(choices.map((p) => p.split('.')[0]))].map((group) => (
                  <fieldset key={group} className="space-y-3 py-4">
                    <legend className="text-sm font-medium">{resourceLabel(group)}</legend>
                    <div className="flex flex-wrap gap-4">
                      {choices
                        .filter((p) => p.startsWith(`${group}.`))
                        .map((p) => (
                          <label key={p} className="flex items-center gap-2 text-sm">
                            <Input
                              type="checkbox"
                              className="h-4 w-4 shrink-0 rounded border-input p-0 accent-primary"
                              checked={permissions.includes(p)}
                              onChange={(e) => {
                                if (!writable() || busy || intent) return
                                setPermissions((previous) =>
                                  e.target.checked
                                    ? [...previous, p].sort()
                                    : previous.filter((code) => code !== p),
                                )
                              }}
                            />
                            {query.data!.available_permissions.includes(p)
                              ? permissionLabel(p)
                              : t('roleDefinition.unknownCode', { code: p })}
                          </label>
                        ))}
                    </div>
                  </fieldset>
                ))}
              </div>
              {unknown.length > 0 && <p>{t('roleDefinition.removeUnknown')}</p>}
              <FormField label={t('roleDefinition.reason')}>
                <Textarea
                  value={reason}
                  onChange={(e) => {
                    if (writable() && !busy && !intent) setReason(e.target.value)
                  }}
                />
              </FormField>
              {!validRoleDefinitionName(name) && <p>{t('roleDefinition.invalidName')}</p>}
              {reason.length > 0 && !validRoleDefinitionReason(reason) && (
                <p>{t('roleDefinition.invalidReason')}</p>
              )}
            </fieldset>
            <div className="flex flex-wrap justify-end gap-2">
              <Button variant="outline" onClick={close} disabled={busy}>
                {t('roleDefinition.cancel')}
              </Button>
              {!intent && (
                <Button variant="outline" disabled={!writable() || busy} onClick={reviewCurrent}>
                  {t('roleDefinition.reviewCurrent')}
                </Button>
              )}
              {intent ? (
                <>
                  <Button
                    variant="outline"
                    disabled={busy || !fresh()}
                    onClick={() => {
                      if (fresh() && !pending.current) setConfirmation('abandon')
                    }}
                  >
                    {t('roleDefinition.abandon')}
                  </Button>
                  <Button disabled={!writable() || busy} onClick={() => void dispatch(true)}>
                    {t('roleDefinition.retry')}
                  </Button>
                </>
              ) : (
                <Button
                  disabled={!writable() || busy || !review || !snapshotCurrent || !validDraft}
                  onClick={prepare}
                >
                  {t('roleDefinition.reviewSave')}
                </Button>
              )}
            </div>
          </div>
        )}
      </Dialog>
      <Dialog
        open={confirmation !== null}
        onOpenChange={(value) => {
          if (!value) setConfirmation(null)
        }}
        busy={busy}
        width={640}
        title={t(
          confirmation === 'abandon'
            ? 'roleDefinition.abandonTitle'
            : 'roleDefinition.confirmTitle',
        )}
        description={t(
          confirmation === 'abandon' ? 'roleDefinition.abandonHelp' : 'roleDefinition.confirmHelp',
        )}
        finalFocus={false}
      >
        {confirmation === 'write' && (
          <div className="space-y-4">
            {review && !snapshotCurrent && <p role="alert">{t('roleDefinition.reviewChanged')}</p>}
            <p className="break-words whitespace-pre-wrap">{name}</p>
            {renderPermissions(permissions)}
            <p className="break-words whitespace-pre-wrap">{reason}</p>
          </div>
        )}
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="outline" disabled={busy} onClick={() => setConfirmation(null)}>
            {t('roleDefinition.cancel')}
          </Button>
          <Button
            disabled={confirmation === 'abandon' ? busy || !fresh() : !canConfirmWrite()}
            onClick={() => {
              if (!fresh() || pending.current) return
              if (confirmation === 'abandon') {
                setIntent(null)
                setReview(null)
                setConfirmation(null)
                setNotice('roleDefinition.abandoned')
              } else void dispatch(false)
            }}
          >
            {t(
              confirmation === 'abandon'
                ? 'roleDefinition.confirmAbandon'
                : 'roleDefinition.confirm',
            )}
          </Button>
        </div>
      </Dialog>
    </>
  )
}
