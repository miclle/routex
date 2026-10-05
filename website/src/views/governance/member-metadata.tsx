import {
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type FormEvent,
} from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import {
  getMemberMetadata,
  setMemberMetadata,
  validMemberName,
  validMemberMetadataReason,
} from '@/api/member-metadata'
import type { MemberMetadataInput, MemberMetadataRecord } from '@/types/member-metadata'
import type { Session } from '@/types/auth'
import type { Member } from '@/types/governance'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { FormField, QueryState } from '@/components/app/CatalogUI'

type Props = {
  actor: string
  target: string
  generation: number
  ready: boolean
  email: string
  targetQueryKey: readonly unknown[]
}
type Intent = MemberMetadataInput & { etag: string }
export default function MemberMetadata(props: Props) {
  return <Editor key={`${props.actor}:${props.target}`} {...props} />
}
function Editor({ actor, target, generation, ready, email, targetQueryKey }: Props) {
  const { t } = useTranslation('governance'),
    cache = useQueryClient()
  const keys = JSON.stringify([['auth', 'session'], ['permissions', actor], targetQueryKey])
  const snapshot = useCallback(
    () =>
      JSON.parse(keys)
        .map((key: unknown[]) => {
          const s = cache.getQueryState(key)
          return `${s?.dataUpdateCount}:${s?.errorUpdateCount}:${s?.status}:${s?.fetchStatus}`
        })
        .join('|'),
    [cache, keys],
  )
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (
          JSON.parse(keys).some(
            (key: unknown[]) => JSON.stringify(key) === JSON.stringify(event.query.queryKey),
          )
        )
          notify()
      }),
    [cache, keys],
  )
  const version = useSyncExternalStore(subscribe, snapshot, snapshot)
  function authority(write = false) {
    const s = cache.getQueryState<Session>(['auth', 'session']),
      p = cache.getQueryState<string[]>(['permissions', actor]),
      m = cache.getQueryState<Member>(targetQueryKey)
    return (
      ready &&
      !!actor &&
      [s, p, m].every((q) => q?.status === 'success' && q.fetchStatus === 'idle' && !q.error) &&
      s?.data?.user.id === actor &&
      m?.data?.id === target &&
      p?.data?.includes('members.read') === true &&
      (!write || (p.data.includes('members.write') && !!s?.data?.csrf_token))
    )
  }
  const queryKey = useMemo(
    () => ['member-metadata', actor, target, generation, version] as const,
    [actor, target, generation, version],
  )
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => getMemberMetadata(target, signal),
    enabled: authority(),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const fresh = authority() && query.isSuccess && !query.isFetching && query.data.user_id === target
  const writable = fresh && authority(true) && query.data.can_edit
  const [draft, setDraft] = useState<string | null>(null),
    [open, setOpen] = useState(false),
    [reason, setReason] = useState(''),
    [name, setName] = useState(''),
    [reviewed, setReviewed] = useState<MemberMetadataRecord | null>(null)
  const [notice, setNotice] = useState<
    '' | 'nameError' | 'reasonError' | 'uncertain' | 'conflict' | 'failed'
  >('')
  const [confirmed, setConfirmed] = useState<{ name: string; etag: string } | null>(null)
  const [reviewingBasis, setReviewingBasis] = useState<string | null>(null),
    [busy, setBusy] = useState(false)
  const intent = useRef<Intent | null>(null),
    controller = useRef<AbortController | null>(null),
    alive = useRef(true),
    serial = useRef(0),
    trigger = useRef<HTMLButtonElement>(null)
  const [capturedIntent, setCapturedIntent] = useState<Intent | null>(null)
  const basis = `${actor}:${target}:${generation}:${version}`
  const reviewing = reviewingBasis === basis
  const current = useRef(basis)
  useLayoutEffect(() => {
    current.current = basis
  }, [basis])
  function live(captured: string, write = true) {
    const q = cache.getQueryState<MemberMetadataRecord>(queryKey)
    return (
      alive.current &&
      current.current === captured &&
      snapshot() === version &&
      authority(write) &&
      q?.status === 'success' &&
      q.fetchStatus === 'idle' &&
      !q.error &&
      q.data?.user_id === target &&
      (!write || q.data.can_edit)
    )
  }
  useLayoutEffect(() => {
    const watched = [...JSON.parse(keys), queryKey].map((key) => JSON.stringify(key))
    const stop = cache.getQueryCache().subscribe((event) => {
      if (!watched.includes(JSON.stringify(event.query.queryKey))) return
      if (event.type !== 'updated' && event.type !== 'removed') return
      serial.current++
      controller.current?.abort()
      controller.current = null
      setBusy(false)
    })
    return stop
  }, [cache, keys, queryKey])
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      controller.current?.abort()
      intent.current = null
    }
  }, [])
  async function review() {
    if (intent.current || reviewing || !live(basis)) return
    const captured = basis
    setReviewingBasis(captured)
    setReviewed(null)
    try {
      const response = await query.refetch()
      if (response.data && !response.error && live(captured)) {
        setReviewed(response.data)
        setNotice('')
      }
    } finally {
      if (alive.current && current.current === captured) setReviewingBasis(null)
    }
  }
  function prepare(event: FormEvent) {
    event.preventDefault()
    if (!live(basis)) return
    if (intent.current) {
      setOpen(true)
      return
    }
    const value = (draft ?? query.data?.name ?? '').trim()
    if (!validMemberName(value)) {
      setNotice('nameError')
      return
    }
    setName(value)
    setReason('')
    setReviewed(null)
    setNotice('')
    setOpen(true)
  }
  async function save() {
    if (controller.current || busy || reviewing || !live(basis)) return
    if (!intent.current) {
      if (!reviewed || reviewed.etag !== query.data?.etag) return
      const value = reason.trim()
      if (!validMemberMetadataReason(value)) {
        setNotice('reasonError')
        return
      }
      intent.current = Object.freeze({ name, reason: value, etag: reviewed.etag })
      setCapturedIntent(intent.current)
    }
    const original = intent.current,
      captured = basis,
      operation = ++serial.current,
      pending = new AbortController()
    controller.current = pending
    setBusy(true)
    setNotice('uncertain')
    try {
      const csrf = cache.getQueryData<Session>(['auth', 'session'])!.csrf_token
      const result = await setMemberMetadata(
        target,
        original.etag,
        { name: original.name, reason: original.reason },
        csrf,
        pending.signal,
      )
      if (!pending.signal.aborted && operation === serial.current && live(captured)) {
        intent.current = null
        setCapturedIntent(null)
        setOpen(false)
        setReason('')
        setReviewed(null)
        setDraft(null)
        setNotice('')
        setConfirmed({ name: result.name, etag: result.etag })
        void cache.invalidateQueries({ queryKey: ['member-metadata', actor, target] })
        void cache.invalidateQueries({ queryKey: ['admin', 'member', actor, target] })
        void cache.invalidateQueries({ queryKey: ['admin', 'members', actor] })
      }
    } catch (error) {
      if (!pending.signal.aborted && operation === serial.current && live(captured))
        setNotice(
          axios.isAxiosError(error) && error.response?.status === 409 ? 'conflict' : 'uncertain',
        )
    } finally {
      if (alive.current && operation === serial.current) {
        controller.current = null
        setBusy(false)
      }
    }
  }
  function close() {
    if (busy) return
    intent.current = null
    setCapturedIntent(null)
    setOpen(false)
    setReason('')
    setReviewed(null)
    setNotice('')
  }
  function finalFocus() {
    return live(basis, false) && trigger.current?.isConnected ? trigger.current : false
  }
  return (
    <section className="rounded-lg border" aria-label={t('members.basicInfo')}>
      <h3 className="border-b p-4 font-medium">{t('members.basicInfo')}</h3>
      {fresh && (
        <>
          <form onSubmit={prepare} className="space-y-4 p-4" aria-label={t('memberMetadata.form')}>
            <p className="text-sm text-muted-foreground">{t('memberMetadata.help')}</p>
            <FormField label={t('members.name')}>
              <Input
                name="name"
                value={draft ?? query.data.name}
                disabled={!writable || !!capturedIntent || open}
                onChange={(e) => {
                  if (live(basis) && !capturedIntent) {
                    setDraft(e.target.value)
                    setConfirmed(null)
                    setNotice('')
                  }
                }}
              />
            </FormField>
            <FormField label={t('members.email')}>
              <Input value={email} disabled />
            </FormField>
            {notice === 'nameError' && <p role="alert">{t('memberMetadata.nameError')}</p>}
            {confirmed?.name === query.data.name && confirmed.etag === query.data.etag && (
              <p role="status">{t('memberMetadata.confirmed')}</p>
            )}
            {writable && (
              <Button ref={trigger} type="submit" disabled={busy}>
                {t(capturedIntent ? 'memberMetadata.resume' : 'memberMetadata.save')}
              </Button>
            )}
          </form>
        </>
      )}
      {authority() && (
        <QueryState
          pending={query.isFetching || query.isPending}
          error={query.error}
          retry={() => void query.refetch()}
        />
      )}
      <Dialog
        open={open && writable}
        onOpenChange={(value) => {
          if (!value) close()
        }}
        busy={busy}
        title={t('memberMetadata.title')}
        description={t('memberMetadata.description')}
        finalFocus={finalFocus}
      >
        <dl className="mb-4 space-y-2 text-sm">
          <dt>{t('members.name')}</dt>
          <dd>{capturedIntent?.name ?? name}</dd>
        </dl>
        <FormField label={t('memberMetadata.reason')}>
          <Input
            value={capturedIntent?.reason ?? reason}
            disabled={!!capturedIntent || busy}
            onChange={(e) => {
              if (live(basis) && !capturedIntent) setReason(e.target.value)
            }}
          />
        </FormField>
        {reviewed && !capturedIntent && (
          <p className="mt-3 text-sm">{t('memberMetadata.currentName', { name: reviewed.name })}</p>
        )}
        {notice && notice !== 'nameError' && (
          <p role="alert" className="mt-3 text-sm text-destructive">
            {t(`memberMetadata.${notice}`)}
          </p>
        )}
        {reviewing && <p role="status">{t('memberMetadata.reviewing')}</p>}
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" disabled={busy} onClick={close}>
            {t('common:cancel_4d0b4')}
          </Button>
          {!capturedIntent && (
            <Button variant="outline" disabled={busy || reviewing} onClick={() => void review()}>
              {t('memberMetadata.review')}
            </Button>
          )}
          <Button
            disabled={
              busy ||
              reviewing ||
              (!capturedIntent && (!reviewed || reviewed.etag !== query.data?.etag))
            }
            onClick={() => void save()}
          >
            {t(capturedIntent ? 'memberMetadata.retry' : 'memberMetadata.confirm')}
          </Button>
        </div>
      </Dialog>
    </section>
  )
}
