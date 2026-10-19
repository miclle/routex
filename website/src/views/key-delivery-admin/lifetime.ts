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
import type {
  KeyCallerTemplateInput,
  KeyDeliveryIdentityInput,
  KeyDeliveryPolicyInput,
  KeyDeliverySecurityTuple,
} from '@/types/key-delivery'
import {
  KeyDeliveryError,
  saveKeyCallerTemplate,
  saveKeyDeliveryIdentity,
  saveKeyDeliveryPolicy,
  updateKeyDeliveryTarget,
  verifyKeyDelivery,
} from '@/api/key-delivery'

export interface DeliveryAuthority {
  actor: string
  admin: boolean
  fresh: boolean
  generation: number
  permissionKey: readonly unknown[]
}
type VaultSelection = { id: string; endpoint: string; namespace: string; mount: string }
export type DeliveryIntent = {
  etag: string
  expires: number
  optionGeneration?: number
  option?: VaultSelection
} & (
  | { kind: 'policy'; input: KeyDeliveryPolicyInput }
  | { kind: 'identity'; input: KeyDeliveryIdentityInput }
  | { kind: 'template'; input: KeyCallerTemplateInput }
  | { kind: 'verify'; input: { reason: string } }
  | {
      kind: 'approval'
      target: string
      tuple: KeyDeliverySecurityTuple
      input: { approved: boolean; control_verified: boolean; reason: string }
    }
  | {
      kind: 'isolation'
      target: string
      tuple: KeyDeliverySecurityTuple
      input: { isolated: boolean; reason: string }
    }
  | {
      kind: 'reconcile'
      target: string
      tuple: KeyDeliverySecurityTuple
      input: { request_id: string; reason: string }
    }
)
export type DeliveryDraft = DeliveryIntent extends infer T
  ? T extends DeliveryIntent
    ? Omit<T, 'expires'>
    : never
  : never
export const deliveryQueryPrefix = (actor: string) => ['key-delivery-admin', actor] as const
const same = (a: readonly unknown[], b: readonly unknown[]) =>
  JSON.stringify(a) === JSON.stringify(b)

export function useDeliveryLifetime(
  authority: DeliveryAuthority,
  options: { current: boolean; generation: number; choices?: readonly VaultSelection[] },
) {
  const cache = useQueryClient()
  const latest = useRef({ authority, options })
  useLayoutEffect(() => {
    latest.current = { authority, options }
  }, [authority, options])
  const owner = useRef({
    mounted: true,
    turn: 0,
    controller: undefined as AbortController | undefined,
    dispatched: false,
  })
  const [dispatchTurn, setDispatchTurn] = useState(0)
  const intentRef = useRef<DeliveryIntent | undefined>(undefined)
  const [intent, setIntent] = useState<DeliveryIntent>()
  const [pending, setPending] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const [notice, setNotice] = useState<
    'saved' | 'unknown' | 'conflict' | 'rejected' | 'expired' | undefined
  >()
  const [receipts, setReceipts] = useState<
    Partial<Record<'writer' | 'provisioner', { id: string; integration: string }>>
  >({})
  const permissionKey = JSON.stringify(authority.permissionKey)
  const snapshot = useCallback(() => {
    const keys = [['auth', 'session'], JSON.parse(permissionKey)]
    return keys
      .map((key) => {
        const s = cache.getQueryState(key)
        return `${s?.status}:${s?.fetchStatus}:${s?.isInvalidated}:${s?.dataUpdateCount}:${s?.errorUpdateCount}`
      })
      .join('|')
  }, [cache, permissionKey])
  const previous = useRef(snapshot())
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (event.type !== 'updated' && event.type !== 'removed') return
        const key = event.query.queryKey
        const authorityEvent =
          same(key, ['auth', 'session']) || same(key, JSON.parse(permissionKey))
        const resourceEvent =
          key[0] === 'key-delivery-admin' && key[1] === latest.current.authority.actor
        if (!authorityEvent && !resourceEvent) return
        const next = snapshot()
        if (authorityEvent && previous.current === next) return
        previous.current = next
        owner.current.turn++
        setDispatchTurn(owner.current.turn)
        owner.current.controller?.abort()
        owner.current.controller = undefined
        setPending(false)
        if (authorityEvent) setReceipts({})
        const session = cache.getQueryData<Session | null>(['auth', 'session'])
        if (
          !session ||
          session.user.id !== latest.current.authority.actor ||
          session.user.role !== 'admin'
        ) {
          intentRef.current = undefined
          setIntent(undefined)
          setUncertain(false)
          setNotice(undefined)
        } else if (owner.current.dispatched && intentRef.current) {
          setUncertain(true)
          setNotice('unknown')
        } else {
          intentRef.current = undefined
          setIntent(undefined)
        }
        notify()
      }),
    [cache, permissionKey, snapshot],
  )
  const revision = useSyncExternalStore(subscribe, snapshot, snapshot)
  const parentIdentity = JSON.stringify([
    authority.actor,
    authority.admin,
    authority.fresh,
    authority.generation,
    permissionKey,
    options.current,
    options.generation,
  ])
  const parentRef = useRef(parentIdentity)
  const settleParent = useCallback(() => {
    if (parentRef.current === parentIdentity) return
    parentRef.current = parentIdentity
    owner.current.turn++
    setDispatchTurn(owner.current.turn)
    owner.current.controller?.abort()
    owner.current.controller = undefined
    setPending(false)
    setReceipts({})
    if (owner.current.dispatched && intentRef.current) {
      setUncertain(true)
      setNotice('unknown')
    } else {
      intentRef.current = undefined
      setIntent(undefined)
    }
  }, [parentIdentity])
  useEffect(settleParent, [settleParent])
  useEffect(() => {
    const lifetime = owner.current
    lifetime.mounted = true
    setDispatchTurn(lifetime.turn)
    return () => {
      lifetime.mounted = false
      lifetime.turn++
      lifetime.controller?.abort()
      intentRef.current = undefined
    }
  }, [])
  const current = useCallback(
    (permission: string, test = false) => {
      const a = latest.current.authority
      const session = cache.getQueryState<Session | null>(['auth', 'session'])
      const grants = cache.getQueryState<string[]>(a.permissionKey)
      return (
        a.fresh &&
        a.admin &&
        !!a.actor &&
        [session, grants].every(
          (s) =>
            s?.status === 'success' && s.fetchStatus === 'idle' && !s.isInvalidated && !s.error,
        ) &&
        session?.data?.user.id === a.actor &&
        session.data.user.role === 'admin' &&
        !!session.data.csrf_token &&
        grants?.data?.includes(permission) === true &&
        (!test || grants.data.includes('secrets.test'))
      )
    },
    [cache],
  )
  const discard = useCallback(() => {
    owner.current.turn++
    setDispatchTurn(owner.current.turn)
    owner.current.controller?.abort()
    owner.current.controller = undefined
    owner.current.dispatched = false
    intentRef.current = undefined
    setIntent(undefined)
    setPending(false)
    setUncertain(false)
    setNotice(undefined)
  }, [])
  useEffect(() => {
    if (!intent) return
    const timer = setTimeout(
      () => {
        discard()
        setNotice('expired')
      },
      Math.max(0, intent.expires - Date.now()),
    )
    return () => clearTimeout(timer)
  }, [intent, discard])
  const selectionCurrent = (original: DeliveryIntent) => {
    if (original.optionGeneration === undefined) return true
    const options = latest.current.options
    return (
      options.current &&
      !!original.option &&
      (options.choices ?? []).some(
        (choice) =>
          choice.id === original.option!.id &&
          choice.endpoint === original.option!.endpoint &&
          choice.namespace === original.option!.namespace &&
          choice.mount === original.option!.mount,
      ) &&
      (uncertain || options.generation === original.optionGeneration)
    )
  }
  const parentIsCurrent = () =>
    JSON.stringify([
      latest.current.authority.actor,
      latest.current.authority.admin,
      latest.current.authority.fresh,
      latest.current.authority.generation,
      JSON.stringify(latest.current.authority.permissionKey),
      latest.current.options.current,
      latest.current.options.generation,
    ]) === parentIdentity
  const prepare = (draft: DeliveryDraft) => {
    if (
      owner.current.controller ||
      intentRef.current ||
      !current('secrets.write', draft.kind === 'verify' || draft.kind === 'reconcile')
    )
      return
    if (
      draft.optionGeneration !== undefined &&
      (!latest.current.options.current ||
        draft.optionGeneration !== latest.current.options.generation)
    )
      return
    let option: VaultSelection | undefined
    if (draft.optionGeneration !== undefined) {
      if (draft.kind !== 'policy' && draft.kind !== 'identity') return
      const selected = (latest.current.options.choices ?? []).find(
        (choice) => choice.id === draft.input.integration_id,
      )
      if (!selected) return
      option = {
        id: selected.id,
        endpoint: selected.endpoint,
        namespace: selected.namespace,
        mount: selected.mount,
      }
    }
    const captured = structuredClone({
      ...draft,
      option,
      expires: Date.now() + 300_000,
    }) as DeliveryIntent
    intentRef.current = captured
    owner.current.dispatched = false
    setIntent(captured)
    setUncertain(false)
    setNotice(undefined)
  }
  const dispatch = async () => {
    const original = intent
    if (
      !owner.current.mounted ||
      !original ||
      intentRef.current !== original ||
      owner.current.turn !== dispatchTurn ||
      parentRef.current !== parentIdentity ||
      !parentIsCurrent() ||
      pending ||
      Date.now() >= original.expires ||
      owner.current.controller ||
      !current('secrets.write', original.kind === 'verify' || original.kind === 'reconcile')
    )
      return
    if (!selectionCurrent(original)) return
    const controller = new AbortController(),
      turn = ++owner.current.turn,
      fingerprint = snapshot()
    setDispatchTurn(turn)
    owner.current.controller = controller
    owner.current.dispatched = true
    setPending(true)
    const csrf = cache.getQueryData<Session>(['auth', 'session'])!.csrf_token
    const live = () =>
      owner.current.mounted &&
      owner.current.turn === turn &&
      !controller.signal.aborted &&
      snapshot() === fingerprint &&
      parentIsCurrent() &&
      current('secrets.write', original.kind === 'verify' || original.kind === 'reconcile')
    try {
      let result: { id?: string }
      switch (original.kind) {
        case 'policy':
          await saveKeyDeliveryPolicy(original.input, original.etag, csrf, controller.signal)
          result = {}
          break
        case 'identity': {
          const saved = await saveKeyDeliveryIdentity(
            original.input,
            original.etag,
            csrf,
            controller.signal,
          )
          if (saved.purpose !== original.input.purpose || !saved.configured)
            throw new KeyDeliveryError(0)
          result = saved
          break
        }
        case 'template':
          await saveKeyCallerTemplate(original.input, original.etag, csrf, controller.signal)
          result = {}
          break
        case 'verify':
          await verifyKeyDelivery(original.input.reason, original.etag, csrf, controller.signal)
          result = {}
          break
        default:
          result = await updateKeyDeliveryTarget(
            original.target,
            original.kind,
            original.input,
            original.etag,
            csrf,
            controller.signal,
          )
      }
      if (!live()) return
      // Settle our operation before our own read invalidation can trigger the fence.
      owner.current.controller = undefined
      owner.current.dispatched = false
      intentRef.current = undefined
      setIntent(undefined)
      setPending(false)
      setUncertain(false)
      setNotice('saved')
      if (original.kind === 'identity' && result.id) {
        const id = result.id
        setReceipts((current) => ({
          ...current,
          [original.input.purpose]: { id, integration: original.input.integration_id },
        }))
      }
      void cache.invalidateQueries({ queryKey: deliveryQueryPrefix(authority.actor) })
    } catch (error) {
      if (!live()) return
      owner.current.controller = undefined
      setPending(false)
      const status = error instanceof KeyDeliveryError ? error.status : 0
      if (uncertain || ![400, 401, 403, 404, 409, 412, 422].includes(status)) {
        setUncertain(true)
        setNotice('unknown')
      } else {
        owner.current.dispatched = false
        intentRef.current = undefined
        setIntent(undefined)
        setNotice(status === 409 || status === 412 ? 'conflict' : 'rejected')
      }
    }
  }
  return {
    current,
    revision,
    parentIdentity,
    intent,
    pending,
    uncertain,
    notice,
    receipts,
    prepare,
    dispatch,
    discard,
    selectionCurrent,
    locked: !!intent,
  }
}
export type DeliveryLifetime = ReturnType<typeof useDeliveryLifetime>
