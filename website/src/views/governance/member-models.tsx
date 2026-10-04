import { useCallback, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getMemberModels, setMemberModels, validMemberModelReason } from '@/api/member-models'
import { QueryState, FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Table } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Dialog } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import RequestPanel from '@/views/personal-model-requests/requests'
import type { Session } from '@/types/auth'
import type { Member } from '@/types/governance'
import type {
  MemberModelRow,
  MemberModelPriceCell,
  MemberModelsWorkspace,
  MemberModelsWriteInput,
} from '@/types/member-models'

type Props = {
  actor: string
  target: string
  generation: number
  ready: boolean
  targetQueryKey: readonly unknown[]
}
type Draft = { etag: string; ids: string[] }
type Intent = { etag: string; body: MemberModelsWriteInput }
export default function MemberModels(props: Props) {
  return <Models key={`${props.actor}:${props.target}`} {...props} />
}
function Models({ actor, target, generation, ready, targetQueryKey }: Props) {
  const { t, i18n } = useTranslation('governance')
  const cache = useQueryClient()
  const keys = JSON.stringify([['auth', 'session'], ['permissions', actor], targetQueryKey])
  const snapshot = useCallback(
    () =>
      JSON.parse(keys)
        .map((k: unknown[]) => {
          const s = cache.getQueryState(k)
          return `${s?.status}:${s?.fetchStatus}:${s?.dataUpdateCount}:${s?.errorUpdateCount}`
        })
        .join('|'),
    [cache, keys],
  )
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((e) => {
        if (
          JSON.parse(keys).some(
            (k: unknown[]) => JSON.stringify(k) === JSON.stringify(e.query.queryKey),
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
      u = cache.getQueryState<Member>(targetQueryKey)
    return (
      ready &&
      !!actor &&
      snapshot() === version &&
      [s, p, u].every((x) => x?.status === 'success' && x.fetchStatus === 'idle' && !x.error) &&
      s?.data?.user.id === actor &&
      !!s.data.csrf_token &&
      u?.data?.id === target &&
      p?.data?.includes('members.read') === true &&
      (!write ||
        (p.data.includes('members.models.write') && !u.data.disabled && !u.data.offboarded_at))
    )
  }
  const queryKey = ['admin', 'member-models', actor, target, generation, version]
  const query = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      if (!authority()) throw new Error(t('memberModels.denied'))
      const data = await getMemberModels(target, signal)
      if (signal.aborted || !authority()) throw new Error(t('memberModels.denied'))
      return data
    },
    enabled: authority(),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const fresh = authority() && query.isSuccess && !query.isFetching
  const writable = fresh && authority(true) && query.data!.can_edit
  function current(write = false) {
    const s = cache.getQueryState<MemberModelsWorkspace>(queryKey)
    return (
      (write ? writable : fresh) &&
      authority(write) &&
      s?.status === 'success' &&
      s.fetchStatus === 'idle' &&
      !s.error &&
      s.data === query.data
    )
  }
  const [draft, setDraft] = useState<Draft | null>(null)
  const [review, setReview] = useState<Draft | null>(null)
  const [reason, setReason] = useState('')
  const [intent, setIntent] = useState<Intent | null>(null)
  const [busyScope, setBusyScope] = useState<string | null>(null)
  const state = cache.getQueryState(queryKey)
  const operationScope = `${generation}:${version}:${state?.dataUpdateCount}:${state?.errorUpdateCount}`
  const busy = writable && busyScope === operationScope
  const [confirmedETag, setConfirmedETag] = useState<string | null>(null)
  const [notice, setNotice] = useState<'uncertain' | 'reasonInvalid' | 'applied' | null>(null)
  const alive = useRef(true),
    lock = useRef(false),
    serial = useRef(0),
    controller = useRef<AbortController | null>(null)
  useLayoutEffect(() => {
    if (!writable) {
      serial.current++
      controller.current?.abort()
      lock.current = false
    }
  }, [writable, version, generation])
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      controller.current?.abort()
    }
  }, [])
  const ids = draft?.ids ?? query.data?.personal_models.map((r) => r.id) ?? []
  const baseline = query.data?.personal_models.map((r) => r.id) ?? []
  const changed = JSON.stringify([...ids].sort()) !== JSON.stringify(baseline)
  const all = query.data ? [...query.data.personal_models, ...query.data.available_models] : []
  const byID = new Map(all.map((r) => [r.id, r]))
  function choose(id: string, add: boolean) {
    if (
      !current(true) ||
      intent ||
      busy ||
      (add && !baseline.includes(id) && !byID.get(id)?.selectable)
    )
      return
    const next = add ? [...ids, id] : ids.filter((v) => v !== id)
    setDraft({ etag: draft?.etag ?? query.data!.etag, ids: [...new Set(next)].sort() })
    setNotice(null)
  }
  async function dispatch() {
    if (!current(true) || !review || lock.current || !alive.current) return
    if (!intent && (review.etag !== query.data!.etag || review.ids.some((id) => !byID.has(id))))
      return
    const normalized = reason.trim()
    if (!intent && !validMemberModelReason(normalized)) {
      setNotice('reasonInvalid')
      return
    }
    const captured = intent ?? {
      etag: review.etag,
      body: { model_ids: [...review.ids].sort(), reason: normalized },
    }
    setIntent(captured)
    setNotice('uncertain')
    setBusyScope(operationScope)
    lock.current = true
    const operation = ++serial.current,
      pending = new AbortController()
    controller.current = pending
    const csrf = cache.getQueryData<Session>(['auth', 'session'])!.csrf_token
    try {
      const result = await setMemberModels(
        target,
        captured.etag,
        captured.body,
        csrf,
        pending.signal,
      )
      if (
        !alive.current ||
        pending.signal.aborted ||
        operation !== serial.current ||
        !current(true)
      )
        return
      setConfirmedETag(result.etag)
      setIntent(null)
      setReview(null)
      setDraft(null)
      setReason('')
      setNotice('applied')
      void cache.invalidateQueries({ queryKey: ['admin', 'member-models', actor, target] })
      void cache.invalidateQueries({ queryKey: ['personal-model-workspace', actor, target] })
      void cache.invalidateQueries({ queryKey: ['personal-model-requests', actor, target] })
      void cache.invalidateQueries({ queryKey: ['model-catalog'] })
    } catch {
      if (alive.current && operation === serial.current) setNotice('uncertain')
    } finally {
      if (alive.current && operation === serial.current) {
        lock.current = false
        setBusyScope(null)
      }
    }
  }
  const pricesReadable =
    cache.getQueryData<string[]>(['permissions', actor])?.includes('prices.read') === true
  const providersReadable =
    cache.getQueryData<string[]>(['permissions', actor])?.includes('providers.read') === true
  const price = (p: MemberModelPriceCell) =>
    !pricesReadable ? (
      t('memberModels.unknown')
    ) : p.rate ? (
      <>
        <span className="whitespace-nowrap">
          {t('memberModels.amount', { amount: p.rate.amount, currency: p.rate.currency })}
        </span>
        {p.state === 'disabled' && <Badge variant="outline">{t('memberModels.disabled')}</Badge>}
      </>
    ) : (
      t(`memberModels.price_${p.state}`)
    )
  const stamp = (v: string) =>
    v
      ? new Date(v).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
      : t('memberModels.unknown')
  function table(label: 'personal' | 'available', rows: MemberModelRow[], selected: boolean) {
    return (
      <section className="space-y-3">
        <h3 className="font-medium">{t(`memberModels.${label}`, { count: rows.length })}</h3>
        <Table
          className="min-w-[1750px] table-fixed"
          aria-label={t(`memberModels.${label}`, { count: rows.length })}
        >
          <thead>
            <tr>
              {(
                [
                  'model',
                  'type',
                  'provider',
                  'protocol',
                  'availability',
                  'input',
                  'output',
                  'created',
                  'updated',
                  'action',
                ] as const
              ).map((k, i) => (
                <th
                  key={k}
                  style={{ width: [220, 140, 240, 200, 110, 180, 180, 180, 180, 100][i] }}
                >
                  {t(`memberModels.${k}`)}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.id}>
                <td>
                  {r.name}
                  <p className="font-mono text-xs text-muted-foreground">{r.id}</p>
                </td>
                <td>{t('memberModels.unknown')}</td>
                <td>
                  {!providersReadable || r.providers === null
                    ? t('memberModels.unknown')
                    : r.providers.join(t('common.listSeparator')) || t('memberModels.none')}
                </td>
                <td>
                  {r.protocols.length
                    ? r.protocols.join(t('common.listSeparator'))
                    : t('memberModels.unknown')}
                </td>
                <td>
                  <Badge variant="outline">
                    {t(`memberModels.availability_${r.availability}`)}
                  </Badge>
                </td>
                <td>{price(r.input_price)}</td>
                <td>{price(r.output_price)}</td>
                <td>{stamp(r.created_at)}</td>
                <td>{t('memberModels.unknown')}</td>
                <td>
                  {writable && (
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={busy || !!intent || (!selected && !r.selectable)}
                      onClick={() => choose(r.id, !selected)}
                    >
                      {t(selected ? 'memberModels.remove' : 'memberModels.add')}
                    </Button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
        {!rows.length && <p className="text-sm text-muted-foreground">{t('memberModels.empty')}</p>}
      </section>
    )
  }
  const selectedRows = ids.map(
    (id) =>
      byID.get(id) ?? {
        id,
        name: id,
        status: 'active' as const,
        type: null,
        providers: null,
        protocols: [],
        availability: 'unknown' as const,
        input_price: { state: 'unavailable' as const, rate: null },
        output_price: { state: 'unavailable' as const, rate: null },
        created_at: '',
        updated_at: null,
        selectable: false,
      },
  )
  return (
    <div className="space-y-6">
      {!authority() ? (
        <p role="status">{t('memberModels.denied')}</p>
      ) : (
        <QueryState
          pending={query.isPending || query.isFetching}
          error={query.error}
          retry={() => {
            if (authority()) void query.refetch()
          }}
        />
      )}
      {fresh && (
        <>
          {table('personal', selectedRows, true)}
          {query.data!.can_edit &&
            table(
              'available',
              all
                .filter((r) => !ids.includes(r.id))
                .map((r) => (baseline.includes(r.id) ? { ...r, selectable: true } : r)),
              false,
            )}
          <p className="text-xs text-muted-foreground">{t('memberModels.help')}</p>
          <p role="status">{t(`memberModels.runtime_${query.data!.application_status}`)}</p>
          {notice === 'applied' && confirmedETag === query.data!.etag && (
            <p role="status">{t('memberModels.applied')}</p>
          )}
          {writable && (changed || intent) && (
            <div className="flex justify-end">
              <Button
                disabled={busy}
                onClick={() => {
                  if (!current(true)) return
                  if (intent) {
                    setReview(review)
                    return
                  }
                  setReview({ etag: draft!.etag, ids: [...ids] })
                  setNotice(null)
                }}
              >
                {t(intent ? 'memberModels.continue' : 'memberModels.save')}
              </Button>
            </div>
          )}
        </>
      )}
      <Dialog
        open={!!review && writable}
        busy={busy || !!intent}
        onOpenChange={(open) => {
          if (!open && !intent && current(true)) setReview(null)
        }}
        title={t('memberModels.confirmTitle')}
        description={t('memberModels.confirmHelp')}
      >
        <div className="space-y-4">
          <p className="font-mono text-sm">{target}</p>
          <p>{t('memberModels.selected', { count: review?.ids.length ?? 0 })}</p>
          <FormField label={t('memberModels.reason')}>
            <Textarea
              value={reason}
              disabled={busy || !!intent}
              onChange={(e) => {
                if (current(true) && !intent) setReason(e.target.value)
              }}
            />
          </FormField>
          {!intent && review?.etag !== query.data?.etag && (
            <>
              <p role="alert">{t('memberModels.conflict')}</p>
              <Button
                variant="outline"
                disabled={busy || review?.ids.some((id) => !byID.has(id))}
                onClick={() => {
                  if (current(true) && review && !intent) {
                    setReview({ ...review, etag: query.data!.etag })
                    setDraft({ ids: review.ids, etag: query.data!.etag })
                  }
                }}
              >
                {t('memberModels.review')}
              </Button>
            </>
          )}
          {notice && notice !== 'applied' && <p role="alert">{t(`memberModels.${notice}`)}</p>}
          <Button
            disabled={busy || (!intent && review?.etag !== query.data?.etag)}
            onClick={() => void dispatch()}
          >
            {t(intent ? 'memberModels.retry' : 'memberModels.confirm')}
          </Button>
          {!intent && (
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => {
                if (current(true)) setReview(null)
              }}
            >
              {t('memberModels.cancel')}
            </Button>
          )}
        </div>
      </Dialog>
      <RequestPanel
        owner={target}
        visible={
          fresh &&
          cache.getQueryData<string[]>(['permissions', actor])?.includes('members.models.write') ===
            true
        }
        managed={{
          actor,
          generation: `${generation}:${version}`,
          canRead: () =>
            current() &&
            cache
              .getQueryData<string[]>(['permissions', actor])
              ?.includes('members.models.write') === true,
        }}
      />
    </div>
  )
}
