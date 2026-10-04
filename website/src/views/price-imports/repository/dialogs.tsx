import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Dialog } from '@/components/ui/dialog'
import { Table } from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { PriceRate } from '@/types/pricing'
import type { RepositoryPreview, RepositoryIntent } from '@/types/repository-prices'
import type { useRepositoryMutation } from './mutation'
export function RepositoryDifferenceDialog({
  preview,
  busy,
  canApply,
  notice,
  reason,
  onReason,
  onApply,
  onReview,
  onClose,
}: {
  preview: RepositoryPreview
  busy: boolean
  canApply: boolean
  notice: string
  reason: string
  onReason: (value: string) => void
  onApply: () => void
  onReview: () => void
  onClose: () => void
}) {
  const { t } = useTranslation('priceImports'),
    [page, setPage] = useState(0)
  const price = (rate: PriceRate | null) =>
    rate
      ? `${rate.amount} ${rate.currency} / ${t(`pricing:${rate.unit}`)} · ${t(rate.enabled ? 'enabled' : 'disabled')}`
      : t('none')
  const pages = Math.max(1, Math.ceil(preview.changes.length / 8))
  return (
    <Dialog
      open
      onOpenChange={(value) => {
        if (!value) onClose()
      }}
      title={t('repository.previewTitle')}
      description={t('repository.previewHelp')}
      busy={busy}
      width={1050}
    >
      <div className="space-y-5">
        <p className="break-all text-sm">
          {t('repository.sourceDigest')}: {preview.source_digest}
        </p>
        {notice && <p role="alert">{t(`repository.${notice}`)}</p>}
        {!!preview.errors.length && (
          <section role="alert">
            <h3 className="font-medium">{t('errors')}</h3>
            <ul className="list-inside list-disc">
              {preview.errors.map((issue, index) => (
                <li key={index}>
                  {issue.provider_model_id}
                  {issue.rate_id && ` · ${issue.rate_id}`}: {issue.message} ({issue.code})
                </li>
              ))}
            </ul>
          </section>
        )}
        {!!preview.warnings.length && (
          <section>
            <h3 className="font-medium">{t('repository.warnings')}</h3>
            <ul className="list-inside list-disc">
              {preview.warnings.map((issue, index) => (
                <li key={index}>
                  {issue.provider_model_id}
                  {issue.rate_id && ` · ${issue.rate_id}`}: {issue.message} ({issue.code})
                </li>
              ))}
            </ul>
          </section>
        )}
        <Table aria-label={t('differences')}>
          <thead>
            <tr>
              {['model', 'component', 'condition', 'before', 'after', 'result'].map((label) => (
                <th key={label}>{t(label)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {preview.changes.slice(page * 8, page * 8 + 8).map((change, index) => (
              <tr key={`${change.provider_model_id}:${change.source_rate_key}:${index}`}>
                <td>
                  <p>{change.provider_model_id}</p>
                  <p className="text-xs">{change.source_model_key}</p>
                </td>
                <td>
                  {change.after || change.before
                    ? t(`pricing:${(change.after ?? change.before)!.metric}`)
                    : t('none')}
                </td>
                <td>
                  {change.after || change.before
                    ? t(`pricing:${(change.after ?? change.before)!.tier}`)
                    : t('none')}
                  <p className="text-xs">
                    {t('threshold')}: {change.threshold_before} → {change.threshold_after}
                  </p>
                </td>
                <td>
                  {price(change.before)}
                  <p className="text-xs">{t(`repository.${change.before_source.kind}`)}</p>
                </td>
                <td>
                  {price(change.after)}
                  <p className="text-xs">{t(`repository.${change.after_source.kind}`)}</p>
                </td>
                <td>{t(`repository.${change.action}`)}</td>
              </tr>
            ))}
          </tbody>
        </Table>
        {!preview.changes.length && <p>{t('repository.noChanges')}</p>}
        {pages > 1 && (
          <div className="flex justify-end gap-3">
            <Button variant="outline" disabled={!page} onClick={() => setPage(page - 1)}>
              {t('previous')}
            </Button>
            <span>{t('page', { page: page + 1, total: pages })}</span>
            <Button
              variant="outline"
              disabled={page + 1 >= pages}
              onClick={() => setPage(page + 1)}
            >
              {t('next')}
            </Button>
          </div>
        )}
        <label className="block text-sm">
          {t('repository.reason')}
          <Input
            className="mt-1 h-11 w-full rounded-md border bg-background px-3"
            value={reason}
            disabled={busy}
            onChange={(event) => onReason(event.target.value)}
          />
        </label>
        <div className="flex justify-end gap-2">
          <Button variant="outline" disabled={busy} onClick={onClose}>
            {t('cancel')}
          </Button>
          <Button variant="outline" disabled={busy} onClick={onReview}>
            {t('repository.reviewPreview')}
          </Button>
          <Button disabled={busy || !canApply} onClick={onApply}>
            {t('repository.apply')}
          </Button>
        </div>
      </div>
    </Dialog>
  )
}
export function RepositoryIntentFacts({ intent }: { intent: RepositoryIntent }) {
  const { t } = useTranslation('priceImports')
  return (
    <dl className="space-y-2 break-all text-sm">
      <div>
        <dt>{t('repository.intent')}</dt>
        <dd>{intent.input.request_id}</dd>
      </div>
      <div>
        <dt>{t('repository.sourceDigest')}</dt>
        <dd>{intent.sourceDigest}</dd>
      </div>
      <div>
        <dt>{t('repository.reviewedETag')}</dt>
        <dd>{intent.etag}</dd>
      </div>
      <div>
        <dt>{t('repository.reason')}</dt>
        <dd>{intent.input.reason}</dd>
      </div>
      {intent.kind === 'configure' ? (
        <div>
          <dt>{t('repository.scope')}</dt>
          <dd>{t(intent.input.enabled ? 'repository.enabled' : 'repository.disabled')}</dd>
          {intent.input.mappings.map((mapping) => (
            <dd key={mapping.provider_model_id}>
              {mapping.provider_model_id} → {mapping.source_model_key}
            </dd>
          ))}
        </div>
      ) : (
        <>
          <div>
            <dt>{t('repository.scope')}</dt>
            <dd>{intent.input.selection.provider_model_ids.join(', ')}</dd>
          </div>
          <div>
            <dt>{t('repository.selectedRates')}</dt>
            <dd>{intent.input.selection.rate_ids.join(', ') || t('none')}</dd>
          </div>
        </>
      )}
    </dl>
  )
}
export function RepositoryMutationDialogs({
  mutation,
  visible,
  writable,
}: {
  mutation: ReturnType<typeof useRepositoryMutation>
  visible: boolean
  writable: boolean
}) {
  const { t, i18n } = useTranslation('priceImports')
  return (
    <>
      {visible && mutation.unknown && mutation.intent && (
        <section
          className="space-y-3 rounded-md border p-4"
          aria-label={t('repository.originalIntent')}
        >
          <p role="alert">
            {t(`repository.${mutation.notice === 'retryRejected' ? 'retryRejected' : 'unknown'}`)}
          </p>
          <RepositoryIntentFacts intent={mutation.intent} />
          <div className="flex gap-2">
            <Button
              variant="outline"
              disabled={!writable || mutation.busy}
              onClick={() => void mutation.dispatch('receipt')}
            >
              {t('repository.checkReceipt')}
            </Button>
            <Button
              disabled={!writable || mutation.busy}
              onClick={() => void mutation.dispatch('retry')}
            >
              {t('repository.retryOriginal')}
            </Button>
          </div>
          {['receiptMissing', 'receiptFailed'].includes(mutation.notice) && (
            <p role="alert">{t(`repository.${mutation.notice}`)}</p>
          )}
        </section>
      )}
      {visible && mutation.result && (
        <section
          className="space-y-2 rounded-md border p-4 text-sm"
          aria-label={t('repository.receipt')}
        >
          <h3 className="font-medium">{t('repository.receipt')}</h3>
          <p>
            {mutation.result.receipt.request_id} ·{' '}
            {new Date(mutation.result.receipt.created_at).toLocaleString(
              i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
            )}
          </p>
          <p>{t(`repository.${mutation.result.receipt.mode}`)}</p>
          <p>
            {t('repository.applicationAtResponse')}:{' '}
            {t(`repository.${mutation.result.application_status}`)}
          </p>
          <p>
            {t(
              mutation.result.receipt.mode === 'configure'
                ? 'repository.configReceiptHelp'
                : 'repository.priceReceiptHelp',
            )}
          </p>
          <Button
            variant="outline"
            disabled={!writable || mutation.busy}
            onClick={() => void mutation.dispatch('receipt')}
          >
            {t('repository.refreshReceipt')}
          </Button>
          {['receiptMissing', 'receiptFailed'].includes(mutation.notice) && (
            <p role="alert">{t(`repository.${mutation.notice}`)}</p>
          )}
        </section>
      )}
      <Dialog
        open={mutation.confirm && visible && !mutation.unknown}
        onOpenChange={(value) => {
          if (!value) mutation.cancel()
        }}
        title={t('repository.confirmTitle')}
        description={t('repository.confirmHelp')}
        busy={mutation.busy}
      >
        {mutation.intent && (
          <>
            <RepositoryIntentFacts intent={mutation.intent} />
            <div className="mt-6 flex justify-end gap-2">
              <Button variant="outline" onClick={mutation.cancel}>
                {t('cancel')}
              </Button>
              <Button
                disabled={!writable || mutation.busy}
                onClick={() => void mutation.dispatch('submit')}
              >
                {t('repository.confirm')}
              </Button>
            </div>
          </>
        )}
      </Dialog>
    </>
  )
}
