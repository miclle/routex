import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  getRepositoryConfig,
  previewRepositoryPrices,
  validRepositoryReason,
} from '@/api/repository-prices'
import type { RepositoryPreview, RepositorySelection } from '@/types/repository-prices'
import { Button } from '@/components/ui/button'
import { useRepositoryAuthority, type RepositorySession } from './authority'
import { useRepositoryMutation } from './mutation'
import { RepositoryDifferenceDialog, RepositoryMutationDialogs } from './dialogs'
export function RepositoryRateRestore({
  session,
  modelId,
  rateIds,
  eligible,
}: {
  session: RepositorySession
  modelId: string
  rateIds: string[]
  eligible: boolean
}) {
  return (
    <RestoreFrame
      key={`${session.data?.user.id ?? ''}:${modelId}`}
      session={session}
      modelId={modelId}
      rateIds={rateIds}
      eligible={eligible}
    />
  )
}
function RestoreFrame({
  session,
  modelId,
  rateIds,
  eligible,
}: {
  session: RepositorySession
  modelId: string
  rateIds: string[]
  eligible: boolean
}) {
  const { t } = useTranslation('priceImports'),
    authority = useRepositoryAuthority(session, modelId)
  const query = useQuery({
    queryKey: authority.configKey,
    queryFn: ({ signal }) => getRepositoryConfig(signal),
    enabled: authority.readable,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const authorizedView =
    authority.readable && query.isSuccess && !query.isFetching && !query.error
      ? query.data
      : undefined
  const view = eligible ? authorizedView : undefined
  const writable = !!view?.can_write && authority.writable
  const [preview, setPreview] = useState<{
      data: RepositoryPreview
      selection: RepositorySelection
    }>(),
    [reason, setReason] = useState(''),
    [issue, setIssue] = useState(''),
    [busy, setBusy] = useState(false)
  const lock = useRef(false)
  const mutation = useRepositoryMutation(authority, () => {
    setPreview(undefined)
    setIssue('')
    void query.refetch()
    void authority.cache.invalidateQueries({ queryKey: ['admin', 'prices'] })
  })
  async function review(
    selection: RepositorySelection = {
      mode: 'restore',
      provider_model_ids: [modelId],
      rate_ids: [...rateIds],
    },
    refresh = false,
  ) {
    if (
      !authority.current() ||
      !view ||
      lock.current ||
      mutation.unknown ||
      !selection.rate_ids.length
    )
      return
    lock.current = true
    setBusy(true)
    setIssue('')
    const valid = authority.readGuard()
    if (refresh) {
      const refreshed = await query.refetch()
      if (!valid() || !refreshed.isSuccess) {
        lock.current = false
        setBusy(false)
        return
      }
    }
    const actor = authority.current()
    if (!actor) {
      lock.current = false
      setBusy(false)
      return
    }
    const operation = authority.begin()
    try {
      const result = await previewRepositoryPrices(
        selection,
        actor.csrf_token,
        operation.controller.signal,
      )
      if (operation.valid() && authority.current()) {
        setPreview({ data: result, selection: structuredClone(selection) })
        mutation.setNotice('')
      }
    } catch {
      if (operation.valid()) setIssue('previewFailed')
    } finally {
      operation.release()
      lock.current = false
      setBusy(false)
    }
  }
  function apply() {
    if (!preview || !view || !writable || mutation.unknown || !preview.data.valid) return
    if (!validRepositoryReason(reason)) {
      setIssue('reasonInvalid')
      return
    }
    if (
      preview.data.review_etag !== view.review_etag ||
      preview.data.source_digest !== view.source.digest
    ) {
      setIssue('conflict')
      return
    }
    mutation.prepare({
      kind: 'apply',
      etag: preview.data.review_etag,
      sourceDigest: preview.data.source_digest,
      input: {
        request_id: crypto.randomUUID(),
        preview_digest: preview.data.preview_digest,
        selection: structuredClone(preview.selection),
        reason,
      },
    })
  }
  const notice =
    issue || (['conflict', 'invalid', 'rejected'].includes(mutation.notice) ? mutation.notice : '')
  return (
    <div className="space-y-3">
      {view && (
        <div className="flex justify-end">
          <Button
            variant="outline"
            disabled={
              !writable ||
              !rateIds.length ||
              !view.source.model_count ||
              !view.mappings.some((mapping) => mapping.provider_model_id === modelId) ||
              busy ||
              mutation.busy ||
              mutation.unknown
            }
            onClick={() => void review()}
          >
            {t('repository.restoreSelected')}
          </Button>
        </div>
      )}
      {eligible && authority.readable && query.error && (
        <p role="alert">{t('repository.loadFailed')}</p>
      )}
      {view && notice && !preview && <p role="alert">{t(`repository.${notice}`)}</p>}
      <RepositoryMutationDialogs
        mutation={mutation}
        visible={!!authorizedView}
        writable={!!authorizedView?.can_write && authority.writable}
      />
      {preview && view && !mutation.unknown && (
        <RepositoryDifferenceDialog
          key={preview.data.preview_digest || preview.data.review_etag}
          preview={preview.data}
          reason={reason}
          onReason={setReason}
          notice={notice}
          busy={busy || mutation.busy}
          canApply={
            writable &&
            mutation.notice !== 'conflict' &&
            preview.data.valid &&
            preview.data.changes.some((change) => ['added', 'updated'].includes(change.action)) &&
            preview.data.review_etag === view.review_etag &&
            preview.data.source_digest === view.source.digest
          }
          onApply={apply}
          onReview={() => void review(preview.selection, true)}
          onClose={() => setPreview(undefined)}
        />
      )}
    </div>
  )
}
