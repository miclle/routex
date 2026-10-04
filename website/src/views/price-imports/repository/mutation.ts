import { useEffect, useRef, useState } from 'react'
import {
  applyRepositoryIntent,
  getRepositoryReceipt,
  RepositoryPriceError,
} from '@/api/repository-prices'
import type {
  RepositoryConfig,
  RepositoryIntent,
  RepositoryResult,
} from '@/types/repository-prices'
import type { useRepositoryAuthority } from './authority'
export function useRepositoryMutation(
  authority: ReturnType<typeof useRepositoryAuthority>,
  onCommit: () => void,
) {
  const [intent, setIntent] = useState<RepositoryIntent>(),
    [result, setResult] = useState<RepositoryResult>(),
    [receiptIntent, setReceiptIntent] = useState<RepositoryIntent>()
  const [confirm, setConfirm] = useState(false),
    [unknown, setUnknown] = useState(false),
    [notice, setNotice] = useState(''),
    [busy, setBusy] = useState(false)
  const mounted = useRef(true),
    lock = useRef(false)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  function prepare(captured: RepositoryIntent) {
    if (lock.current || unknown || !authority.current(true)) return
    setIntent(structuredClone(captured))
    setConfirm(true)
    setNotice('')
  }
  async function dispatch(kind: 'submit' | 'retry' | 'receipt') {
    const actor = authority.current(true),
      captured = kind === 'receipt' ? (intent ?? receiptIntent) : intent
    if (!actor || !captured || lock.current) return
    const config = authority.cache.getQueryState<RepositoryConfig>(authority.configKey)
    if (config?.status !== 'success' || config.fetchStatus !== 'idle' || !config.data?.can_write)
      return
    if (
      kind === 'submit' &&
      (captured.etag !== config.data.review_etag ||
        captured.sourceDigest !== config.data.source.digest)
    ) {
      setConfirm(false)
      setIntent(undefined)
      setNotice('conflict')
      return
    }
    lock.current = true
    setBusy(true)
    setConfirm(false)
    setNotice('')
    const operation = authority.begin()
    try {
      const response =
        kind === 'receipt'
          ? await getRepositoryReceipt(captured, operation.controller.signal)
          : await applyRepositoryIntent(captured, actor.csrf_token, operation.controller.signal)
      if (!mounted.current) return
      if (!operation.valid() || !authority.current(true)) {
        if (kind !== 'receipt') {
          setUnknown(true)
          setNotice('unknown')
        }
        return
      }
      setResult(response)
      setReceiptIntent(captured)
      setUnknown(false)
      setIntent(undefined)
      setNotice('receiptSaved')
      operation.release()
      onCommit()
    } catch (error) {
      if (!mounted.current) return
      const status = error instanceof RepositoryPriceError ? error.status : 0
      if (kind === 'receipt') {
        setNotice(status === 404 ? 'receiptMissing' : 'receiptFailed')
        return
      }
      if (
        kind === 'retry' ||
        !operation.valid() ||
        ![400, 401, 403, 404, 409, 422].includes(status)
      ) {
        setUnknown(true)
        setNotice(kind === 'retry' && status ? 'retryRejected' : 'unknown')
      } else {
        setIntent(undefined)
        setNotice(status === 409 ? 'conflict' : status === 422 ? 'invalid' : 'rejected')
      }
    } finally {
      operation.release()
      lock.current = false
      if (mounted.current) setBusy(false)
    }
  }
  function cancel() {
    if (busy || unknown) return
    setConfirm(false)
    setIntent(undefined)
  }
  return {
    intent,
    result,
    receiptIntent,
    confirm,
    unknown,
    notice,
    busy,
    prepare,
    dispatch,
    cancel,
    setConfirm,
    setNotice,
  }
}
