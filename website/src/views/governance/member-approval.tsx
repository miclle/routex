import { useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient, type InfiniteData } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { AxiosError } from 'axios'
import {
  getMemberApproval,
  decideMemberApproval,
  validApprovalReason,
} from '@/api/registration-approval'
import type {
  MemberApprovalInput,
  MemberApprovalReview,
  MemberApprovalResult,
  RegistrationApprovalSummary,
} from '@/types/registration-approval'
import type { MemberDetail } from '@/types/member-recent-login'
import type { MemberListPage } from '@/types/member-list'
import type { Session } from '@/types/auth'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { QueryState, FormField } from '@/components/app/CatalogUI'
import { approvalActorCurrent, useApprovalCacheRevision } from './approval-authority'
export function RegistrationApprovalStatus({ summary }: { summary: RegistrationApprovalSummary }) {
  const { t } = useTranslation('governance')
  return <Badge variant="outline">{t(`registrationApproval.${summary.status}`)}</Badge>
}
type Intent = { etag: string; application: string; body: MemberApprovalInput }
type Props = {
  actor: string
  target: string
  generation: number
  ready: boolean
  open?: boolean
  contextQueryKey: readonly unknown[]
  contextKind: 'detail' | 'list'
  returnFocus: () => HTMLButtonElement | false
  onClose: () => void
}
export default function MemberApproval(props: Props) {
  return <Approval key={`${props.actor}:${props.target}`} {...props} />
}
function Approval({
  actor,
  target,
  generation,
  ready,
  open = true,
  contextQueryKey,
  contextKind,
  returnFocus,
  onClose,
}: Props) {
  const { t, i18n } = useTranslation('governance'),
    cache = useQueryClient()
  const parent = useApprovalCacheRevision([
    ['auth', 'session'],
    ['permissions', actor],
    contextQueryKey,
  ])
  const [needsReview, setNeedsReview] = useState(false)
  function authority() {
    const state = cache.getQueryState<MemberDetail | InfiniteData<MemberListPage>>(contextQueryKey),
      subject = state?.data
    return (
      ready &&
      parent.snapshot() === parent.revision &&
      approvalActorCurrent(cache, actor, 'members.read') &&
      approvalActorCurrent(cache, actor, 'members.approvals.write') &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      !!subject &&
      (contextKind === 'detail'
        ? 'id' in subject && subject.id === target
        : 'pages' in subject &&
          subject.pages.every((p) => p.actor_user_id === actor) &&
          subject.pages.some((p) => p.items.some((r) => r.id === target)))
    )
  }
  const queryKey = ['admin', 'member-approval', actor, target, generation, parent.revision]
  const query = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      if (!authority()) throw new Error('Approval authority unavailable')
      const data = await getMemberApproval(target, signal)
      if (signal.aborted || !authority()) throw new Error('Approval authority unavailable')
      return data
    },
    enabled: open && authority(),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const resource = useApprovalCacheRevision([queryKey])
  function current() {
    const s = cache.getQueryState<MemberApprovalReview>(queryKey)
    return (
      open &&
      authority() &&
      resource.snapshot() === resource.revision &&
      query.isSuccess &&
      !query.isFetching &&
      s?.status === 'success' &&
      s.fetchStatus === 'idle' &&
      !s.isInvalidated &&
      !s.error &&
      s.data === query.data
    )
  }
  const page = current() ? query.data : undefined
  const [review, setReview] = useState<MemberApprovalReview | null>(null),
    [reason, setReason] = useState(''),
    [choice, setChoice] = useState<'approve' | 'reject' | null>(null),
    [intent, setIntent] = useState<Intent | null>(null),
    [result, setResult] = useState<MemberApprovalResult | null>(null),
    [notice, setNotice] = useState<
      'conflict' | 'uncertain' | 'abandoned' | 'invalidReason' | 'failed' | null
    >(null),
    [busyScope, setBusyScope] = useState<string | null>(null)
  const scope = `${generation}:${parent.revision}:${resource.revision}`
  const busy = busyScope === scope
  const alive = useRef(true),
    serial = useRef(0),
    locked = useRef(false),
    controller = useRef<AbortController | null>(null)
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
  const can = (decision: 'approve' | 'reject') =>
    current() &&
    !!page?.application &&
    (decision === 'approve' ? page.can_approve : page.can_reject)
  const date = (value: string) =>
    new Intl.DateTimeFormat(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US', {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date(value))
  async function dispatch(retry = false) {
    if (locked.current || !current()) return
    let captured = intent
    if (!retry) {
      if (
        intent ||
        !choice ||
        !can(choice) ||
        !review ||
        stale ||
        review.review_etag !== page?.review_etag
      )
        return
      if (!validApprovalReason(reason)) {
        setNotice('invalidReason')
        return
      }
      captured = {
        etag: review.review_etag,
        application: review.application!.id,
        body: { decision: choice, reason },
      }
    }
    if (!captured || !page?.application || captured.application !== page.application.id) return
    const auth = cache.getQueryData<Session>(['auth', 'session'])
    if (!auth?.csrf_token) return
    const turn = ++serial.current
    locked.current = true
    setBusyScope(scope)
    setNotice(null)
    setIntent(captured)
    controller.current = new AbortController()
    try {
      const receipt = await decideMemberApproval(
        target,
        captured.application,
        captured.etag,
        captured.body,
        auth.csrf_token,
        controller.current.signal,
      )
      if (!alive.current || turn !== serial.current || !current()) return
      setResult(receipt)
      setIntent(null)
      setReview(null)
      setChoice(null)
      setReason('')
      void cache.invalidateQueries({ queryKey: contextQueryKey })
      void cache.invalidateQueries({ queryKey: ['admin', 'member-approval', actor, target] })
    } catch (error) {
      if (!alive.current || turn !== serial.current) return
      const status = error instanceof AxiosError ? error.response?.status : 0
      setNotice('uncertain')
      if (status === 401 || status === 403) {
        void cache.invalidateQueries({ queryKey: ['auth', 'session'] })
        void cache.invalidateQueries({ queryKey: ['permissions', actor] })
      } else if (status === 409 || status === 412) {
        setNeedsReview(true)
        setNotice('uncertain')
        void query.refetch()
      } else setNotice('uncertain')
    } finally {
      if (alive.current && turn === serial.current) {
        locked.current = false
        setBusyScope(null)
      }
    }
  }
  const confirmed =
    !!page &&
    !!result &&
    page.application?.id === result.application_id &&
    page.approval_status === result.decision &&
    page.runtime_applied &&
    page.admission_eligible === result.admission_eligible
  if (!open) return null
  if (!fresh)
    return authority() ? (
      <QueryState
        pending={query.isFetching}
        error={query.error}
        retry={() => void query.refetch()}
      />
    ) : null
  return (
    <Dialog
      open={fresh}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      title={t('registrationApproval.review')}
      description={t('registrationApproval.description')}
      busy={busy}
      finalFocus={() => authority() && returnFocus()}
    >
      <QueryState
        pending={query.isFetching}
        error={query.error}
        retry={() => void query.refetch()}
      />
      {page && (
        <div className="space-y-4">
          <dl className="grid gap-3 text-sm sm:grid-cols-2">
            <div>
              <dt>{t('members.name')}</dt>
              <dd className="break-words">{page.name}</dd>
            </div>
            <div>
              <dt>{t('common.baseRole')}</dt>
              <dd>{t(`common.${page.identity_role}`)}</dd>
            </div>
            <div>
              <dt>{t('common.status')}</dt>
              <dd>
                {t(
                  page.offboarded_at
                    ? 'common.offboarded'
                    : page.disabled
                      ? 'common.disabled'
                      : 'common.active',
                )}
              </dd>
            </div>
            <div>
              <dt>{t('registrationApproval.status')}</dt>
              <dd>{t(`registrationApproval.${page.approval_status}`)}</dd>
            </div>
            <div>
              <dt>{t('registrationApproval.admission')}</dt>
              <dd>
                {t(
                  page.admission_eligible
                    ? 'registrationApproval.eligible'
                    : 'registrationApproval.ineligible',
                )}
              </dd>
            </div>
            <div>
              <dt>{t('registrationApproval.runtime')}</dt>
              <dd>
                {t(
                  page.runtime_applied
                    ? 'registrationApproval.applied'
                    : 'registrationApproval.notApplied',
                )}
              </dd>
            </div>
            {page.application && (
              <>
                <div>
                  <dt>{t('registrationApproval.appliedAt')}</dt>
                  <dd>{date(page.application.created_at)}</dd>
                </div>
                {page.application.decided_at && (
                  <div>
                    <dt>{t('registrationApproval.decidedAt')}</dt>
                    <dd>{date(page.application.decided_at)}</dd>
                  </div>
                )}
                {page.application.decision_actor_id && (
                  <div>
                    <dt>{t('registrationApproval.reviewer')}</dt>
                    <dd>{page.application.decision_actor_id}</dd>
                  </div>
                )}
                {page.application.decision_reason && (
                  <div className="sm:col-span-2">
                    <dt>{t('registrationApproval.reason')}</dt>
                    <dd className="break-words whitespace-pre-wrap">
                      {page.application.decision_reason}
                    </dd>
                  </div>
                )}
              </>
            )}
          </dl>
          {confirmed && (
            <p role="status">
              {t(
                result.admission_eligible
                  ? 'registrationApproval.confirmedEligible'
                  : 'registrationApproval.confirmedDenied',
              )}
            </p>
          )}
          {notice && <p role="alert">{t(`registrationApproval.${notice}`)}</p>}
          {intent ? (
            <>
              <p>{t('registrationApproval.uncertain')}</p>
              <Button disabled={busy} onClick={() => void dispatch(true)}>
                {t('registrationApproval.retry')}
              </Button>
              <p>{t('registrationApproval.abandonHelp')}</p>
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => {
                  if (busy || locked.current || !intent || !current()) return
                  serial.current++
                  controller.current?.abort()
                  setIntent(null)
                  setReview(page)
                  setNeedsReview(true)
                  setChoice(null)
                  setNotice('abandoned')
                }}
              >
                {t('registrationApproval.abandon')}
              </Button>
            </>
          ) : (
            !confirmed && (
              <>
                {stale && <p role="alert">{t('registrationApproval.conflict')}</p>}
                <FormField label={t('registrationApproval.reason')}>
                  <Textarea
                    value={reason}
                    onChange={(event) => setReason(event.target.value)}
                    disabled={busy}
                    maxLength={1024}
                  />
                </FormField>
                {choice ? (
                  <div className="space-y-3">
                    <p>
                      {t(
                        choice === 'approve'
                          ? 'registrationApproval.confirmApprove'
                          : 'registrationApproval.confirmReject',
                      )}
                    </p>
                    <Button
                      disabled={busy || stale || !can(choice)}
                      onClick={() => void dispatch()}
                    >
                      {t('registrationApproval.confirm')}
                    </Button>
                    <Button variant="outline" onClick={() => setChoice(null)} disabled={busy}>
                      {t('common:cancel_4d0b4')}
                    </Button>
                  </div>
                ) : (
                  <div className="flex gap-2">
                    <Button
                      disabled={busy || !page.can_approve}
                      onClick={() => {
                        if (can('approve')) {
                          setReview(page)
                          setChoice('approve')
                          setNotice(null)
                        }
                      }}
                    >
                      {t('registrationApproval.approve')}
                    </Button>
                    <Button
                      variant="outline"
                      disabled={busy || !page.can_reject}
                      onClick={() => {
                        if (can('reject')) {
                          setReview(page)
                          setChoice('reject')
                          setNotice(null)
                        }
                      }}
                    >
                      {t('registrationApproval.reject')}
                    </Button>
                  </div>
                )}
                {stale && (
                  <Button
                    variant="outline"
                    onClick={() => {
                      if (current()) {
                        setReview(page)
                        setNeedsReview(false)
                        setChoice(null)
                        setNotice(null)
                      }
                    }}
                  >
                    {t('registrationApproval.reviewCurrent')}
                  </Button>
                )}
              </>
            )
          )}
          <Button variant="outline" disabled={busy} onClick={onClose}>
            {t('common:cancel_4d0b4')}
          </Button>
        </div>
      )}
    </Dialog>
  )
}
