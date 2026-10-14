import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { Session } from '@/types/auth'
import type { DeliveryAuthority } from '@/views/key-delivery-admin/lifetime'
import type {
  DeliveryControllerAssignmentInput,
  DeliveryControllerDetail,
  DeliveryControllerEnrollmentInput,
  DeliveryControllerEnrollmentReceipt,
} from '@/types/key-delivery-controller'
import {
  assignController,
  DeliveryControllerError,
  enrollController,
  getControllerEnrollmentReceipt,
  revokeController,
} from '@/api/key-delivery-controller'

export type ControllerIntent = { slot: string; etag: string; proof: ControllerReviewProof } & (
  | { kind: 'enroll'; input: DeliveryControllerEnrollmentInput }
  | { kind: 'assign'; controller: string; input: DeliveryControllerAssignmentInput }
  | { kind: 'revoke'; controller: string; input: { reason: string } }
)
export interface ControllerReviewProof {
  key: readonly unknown[]
  count: number
}
export const controllerPrefix = (actor: string, target: string) =>
  ['key-delivery-controllers', actor, target] as const
const sameKey = (a: readonly unknown[], b: readonly unknown[]) =>
  JSON.stringify(a) === JSON.stringify(b)
export function useControllerLifetime(
  authority: DeliveryAuthority,
  target: string,
  resource = target,
) {
  const cache = useQueryClient()
  const latest = useRef({ authority, target, resource })
  useLayoutEffect(() => {
    latest.current = { authority, target, resource }
  }, [authority, target, resource])
  const owner = useRef({
    mounted: true,
    turn: 0,
    dispatched: false,
    controller: undefined as AbortController | undefined,
  })
  const intentRef = useRef<ControllerIntent | undefined>(undefined)
  const [intent, setIntent] = useState<ControllerIntent>()
  const [pending, setPending] = useState(false),
    [uncertain, setUncertain] = useState(false)
  const [notice, setNotice] = useState<
    | 'saved'
    | 'unknown'
    | 'conflict'
    | 'rejected'
    | 'receipt'
    | 'copyFailed'
    | 'copied'
    | 'receiptMissing'
    | undefined
  >()
  const [bearer, setBearer] = useState('')
  const bearerRef = useRef('')
  const [saved, setSaved] = useState<DeliveryControllerDetail>()
  const [receipt, setReceipt] = useState<DeliveryControllerEnrollmentReceipt>()
  const permissionKey = JSON.stringify(authority.permissionKey)
  const snapshot = useCallback(
    () =>
      [cache.getQueryState(['auth', 'session']), cache.getQueryState(JSON.parse(permissionKey))]
        .map(
          (s) =>
            `${s?.status}:${s?.fetchStatus}:${s?.isInvalidated}:${s?.dataUpdateCount}:${s?.errorUpdateCount}`,
        )
        .join('|'),
    [cache, permissionKey],
  )
  const current = useCallback(
    (permission: 'secrets.read' | 'secrets.write') => {
      const { authority: a, target: t } = latest.current
      const session = cache.getQueryState<Session | null>(['auth', 'session']),
        grants = cache.getQueryState<string[]>(a.permissionKey)
      return (
        owner.current.mounted &&
        !!t &&
        a.admin &&
        a.fresh &&
        !!a.actor &&
        [session, grants].every(
          (s) =>
            s?.status === 'success' && s.fetchStatus === 'idle' && !s.isInvalidated && !s.error,
        ) &&
        session?.data?.user.id === a.actor &&
        session.data.user.role === 'admin' &&
        !!session.data.csrf_token &&
        grants?.data?.includes(permission) === true
      )
    },
    [cache],
  )
  const fence = useCallback((clear = false) => {
    const cell = owner.current
    cell.turn++
    cell.controller?.abort()
    cell.controller = undefined
    setPending(false)
    bearerRef.current = ''
    setBearer('')
    setSaved(undefined)
    setReceipt(undefined)
    if (clear) {
      cell.dispatched = false
      intentRef.current = undefined
      setIntent(undefined)
      setUncertain(false)
      setNotice(undefined)
    } else if (cell.dispatched && intentRef.current) {
      setUncertain(true)
      setNotice('unknown')
    } else {
      intentRef.current = undefined
      setIntent(undefined)
      setUncertain(false)
      setNotice(undefined)
    }
  }, [])
  const previous = useRef(snapshot())
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (event.type !== 'updated' && event.type !== 'removed') return
        if (
          !sameKey(event.query.queryKey, ['auth', 'session']) &&
          !sameKey(event.query.queryKey, JSON.parse(permissionKey))
        )
          return
        const next = snapshot()
        if (next === previous.current) return
        previous.current = next
        const session = cache.getQueryData<Session | null>(['auth', 'session'])
        fence(
          !session ||
            session.user.id !== latest.current.authority.actor ||
            session.user.role !== 'admin',
        )
        notify()
      }),
    [cache, permissionKey, snapshot, fence],
  )
  const revision = useSyncExternalStore(subscribe, snapshot, snapshot)
  const identity = JSON.stringify([
    authority.actor,
    target,
    resource,
    authority.admin,
    authority.fresh,
    authority.generation,
    permissionKey,
  ])
  const prior = useRef({ identity, actor: authority.actor, target, resource })
  useLayoutEffect(() => {
    if (prior.current.identity === identity) return
    const clear =
      prior.current.actor !== authority.actor ||
      prior.current.target !== target ||
      prior.current.resource !== resource ||
      !authority.admin
    prior.current = { identity, actor: authority.actor, target, resource }
    fence(clear)
  }, [identity, authority.actor, authority.admin, target, resource, fence])
  useEffect(() => {
    const cell = owner.current
    cell.mounted = true
    return () => {
      cell.mounted = false
      cell.turn++
      cell.controller?.abort()
      intentRef.current = undefined
      bearerRef.current = ''
    }
  }, [])
  const fingerprint = useCallback(
    () =>
      JSON.stringify([
        latest.current.authority.actor,
        latest.current.target,
        latest.current.resource,
        latest.current.authority.admin,
        latest.current.authority.fresh,
        latest.current.authority.generation,
        latest.current.authority.permissionKey,
        snapshot(),
      ]),
    [snapshot],
  )
  const ticket = useCallback(
    (permission: 'secrets.read' | 'secrets.write') => {
      if (!current(permission)) return undefined
      const turn = owner.current.turn,
        original = fingerprint()
      return {
        live: () =>
          owner.current.mounted &&
          owner.current.turn === turn &&
          fingerprint() === original &&
          current(permission),
      }
    },
    [current, fingerprint],
  )
  const read = async <T>(fn: () => Promise<T>) => {
    const admitted = ticket('secrets.read')
    if (!admitted) throw new DeliveryControllerError(0)
    const result = await fn()
    if (!admitted.live()) throw new DeliveryControllerError(0)
    return result
  }
  const proofCurrent = (proof: ControllerReviewProof) => {
    const latestOwner = latest.current
    if (
      !sameKey(
        proof.key.slice(0, 3),
        controllerPrefix(latestOwner.authority.actor, latestOwner.target),
      ) ||
      proof.key[3] !== `${latestOwner.authority.generation}:${snapshot()}:${latestOwner.resource}`
    )
      return false
    const state = cache.getQueryState(proof.key)
    return (
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      state.dataUpdateCount === proof.count
    )
  }
  const prepare = (value: ControllerIntent) => {
    if (
      !current('secrets.read') ||
      !current('secrets.write') ||
      pending ||
      intentRef.current ||
      !proofCurrent(value.proof)
    )
      return
    const original = structuredClone(value)
    intentRef.current = original
    owner.current.dispatched = false
    setIntent(original)
    setUncertain(false)
    setNotice(undefined)
    bearerRef.current = ''
    setBearer('')
    setSaved(undefined)
    setReceipt(undefined)
  }
  const submit = async () => {
    const original = intentRef.current
    if (
      !original ||
      owner.current.controller ||
      !current('secrets.read') ||
      !current('secrets.write') ||
      (!owner.current.dispatched && !proofCurrent(original.proof))
    )
      return
    const admitted = ticket('secrets.write')
    if (!admitted) return
    const controller = new AbortController()
    owner.current.controller = controller
    owner.current.dispatched = true
    setPending(true)
    setNotice(undefined)
    const csrf = cache.getQueryData<Session>(['auth', 'session'])!.csrf_token
    try {
      let detail: DeliveryControllerDetail,
        secret = ''
      if (original.kind === 'enroll') {
        const result = await enrollController(
          original.slot,
          original.input,
          original.etag,
          csrf,
          controller.signal,
        )
        detail = result.controller
        secret = result.bearer ?? ''
      } else if (original.kind === 'assign')
        detail = await assignController(
          original.controller,
          original.slot,
          original.input,
          original.etag,
          csrf,
          controller.signal,
        )
      else
        detail = await revokeController(
          original.controller,
          original.slot,
          original.input.reason,
          original.etag,
          csrf,
          controller.signal,
        )
      if (!admitted.live() || controller.signal.aborted || !current('secrets.read')) return
      owner.current.controller = undefined
      owner.current.dispatched = false
      intentRef.current = undefined
      setIntent(undefined)
      setUncertain(false)
      setPending(false)
      setSaved(detail)
      bearerRef.current = secret
      setBearer(secret)
      setNotice('saved')
      void cache.invalidateQueries({
        queryKey: controllerPrefix(latest.current.authority.actor, latest.current.target),
      })
    } catch (error) {
      if (!admitted.live() || controller.signal.aborted) return
      owner.current.controller = undefined
      setPending(false)
      // Retain the exact body, UUID and original review through every failed
      // retry. A rejected retry cannot erase an earlier ambiguous dispatch.
      setUncertain(true)
      setNotice(
        error instanceof DeliveryControllerError && error.status === 409
          ? 'conflict'
          : error instanceof DeliveryControllerError && error.status >= 400 && error.status < 500
            ? 'rejected'
            : 'unknown',
      )
    }
  }
  const lookupReceipt = async () => {
    const original = intentRef.current
    if (
      !original ||
      original.kind !== 'enroll' ||
      !owner.current.dispatched ||
      owner.current.controller ||
      !current('secrets.read')
    )
      return
    const admitted = ticket('secrets.read')
    if (!admitted) return
    const controller = new AbortController()
    owner.current.controller = controller
    setPending(true)
    setReceipt(undefined)
    try {
      const found = await getControllerEnrollmentReceipt(
        original.slot,
        original.input.request_id,
        controller.signal,
      )
      if (!admitted.live() || controller.signal.aborted) return
      owner.current.controller = undefined
      setPending(false)
      if (found.enrollment_review !== original.etag) throw new DeliveryControllerError(0)
      // Receipt discovery is deliberately separate from an identical POST retry.
      // Without comparing the server's canonical intent digest, this cannot
      // settle the captured body or claim its historical completion.
      setReceipt(found)
      setNotice('receipt')
    } catch (error) {
      if (!admitted.live() || controller.signal.aborted) return
      owner.current.controller = undefined
      setPending(false)
      setNotice(
        error instanceof DeliveryControllerError && error.status === 404
          ? 'receiptMissing'
          : 'unknown',
      )
    }
  }
  const copy = async (value: string) => {
    const admitted = ticket('secrets.read')
    if (!admitted || pending || !value || value !== bearerRef.current) return
    try {
      await navigator.clipboard.writeText(value)
      if (admitted.live()) setNotice('copied')
    } catch {
      if (admitted.live()) setNotice('copyFailed')
    }
  }
  return {
    current,
    revision,
    generation: `${authority.generation}:${revision}:${resource}`,
    read,
    prepare,
    submit,
    lookupReceipt,
    copy,
    proofCurrent,
    intent,
    pending,
    uncertain,
    notice,
    bearer,
    saved,
    receipt,
    discard: () => fence(true),
    locked: pending || !!intent,
    lockedNow: () => !!owner.current.controller || !!intentRef.current,
    pendingNow: () => !!owner.current.controller,
  }
}
export type ControllerLifetime = ReturnType<typeof useControllerLifetime>
