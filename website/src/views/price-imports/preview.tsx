import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Table } from '@/components/ui/table'
import type { PriceImportPreview } from '@/types/price-imports'
import type { PriceRate } from '@/types/pricing'
export function PricePreviewDialog({
  preview,
  name,
  busy,
  issue,
  canApply,
  onClose,
  onApply,
  onReview,
}: {
  preview: PriceImportPreview
  name: string
  busy: boolean
  issue: string
  canApply: boolean
  onClose: () => void
  onApply: () => void
  onReview: () => void
}) {
  const { t } = useTranslation('priceImports')
  const [page, setPage] = useState(0)
  const pageCount = Math.max(1, Math.ceil(preview.changes.length / 8))
  const price = (rate: PriceRate | null) =>
    rate
      ? `${rate.amount} ${rate.currency} / ${t('million')} · ${t(rate.enabled ? 'enabled' : 'disabled')}`
      : t('none')
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      title={t('previewTitle')}
      description={t('previewHelp')}
      width={1050}
      busy={busy}
    >
      <div className="space-y-5">
        <p className="break-all text-sm">{name}</p>
        {preview.sheet && (
          <p className="break-all text-sm">{t('sheet', { name: preview.sheet })}</p>
        )}
        <p className="rounded-md border bg-muted/30 p-3 text-sm">
          {t('changed', {
            count: preview.changes.filter((row) => row.action !== 'unchanged').length,
          })}
        </p>
        {issue && (
          <p role="alert" className="text-sm text-destructive">
            {t(issue)}
          </p>
        )}
        {preview.errors.length > 0 && (
          <section role="alert" className="rounded-md border border-destructive/40 p-3">
            <h3 className="font-medium">{t('errors')}</h3>
            <ul className="mt-2 list-disc space-y-2 pl-5 text-sm">
              {preview.errors.map((error, index) => (
                <li key={`${error.row}:${error.column}:${index}`}>
                  <span className="font-medium">
                    {error.row ? t('row', { row: error.row }) : t('file')}
                    {error.column ? ` · ${error.column}` : ''}
                    {error.sheet && (
                      <span className="ml-2">{t('sheet', { name: error.sheet })}</span>
                    )}
                    {error.cell && <span className="ml-2">{t('cell', { cell: error.cell })}</span>}
                  </span>
                  : {error.message} <span className="font-mono text-xs">({error.code})</span>
                </li>
              ))}
            </ul>
          </section>
        )}
        <Table aria-label={t('differences')}>
          <thead>
            <tr>
              <th>{t('model')}</th>
              <th>{t('component')}</th>
              <th>{t('condition')}</th>
              <th>{t('before')}</th>
              <th>{t('after')}</th>
              <th>{t('result')}</th>
            </tr>
          </thead>
          <tbody>
            {preview.changes.slice(page * 8, page * 8 + 8).map((change) => (
              <tr key={`${change.row}:${change.provider_model_id}`}>
                <td>
                  <p>{change.upstream_name}</p>
                  <p className="font-mono text-xs text-muted-foreground">
                    {change.provider_model_id}
                  </p>
                  <p className="text-xs">{t('row', { row: change.row })}</p>
                </td>
                <td>{t(`pricing:${change.after.metric}`)}</td>
                <td>
                  {t(`pricing:${change.after.tier}`)}
                  <p className="text-xs">
                    {t('threshold')}: {change.threshold_before} → {change.threshold_after}
                  </p>
                </td>
                <td>{price(change.before)}</td>
                <td>{price(change.after)}</td>
                <td>
                  {t(change.action)}
                  {change.stops_following && (
                    <p className="text-xs text-muted-foreground">{t('stopFollowing')}</p>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
        {pageCount > 1 && (
          <div className="flex items-center justify-end gap-3 text-sm">
            <Button variant="outline" disabled={page === 0} onClick={() => setPage(page - 1)}>
              {t('previous')}
            </Button>
            <span>{t('page', { page: page + 1, total: pageCount })}</span>
            <Button
              variant="outline"
              disabled={page + 1 >= pageCount}
              onClick={() => setPage(page + 1)}
            >
              {t('next')}
            </Button>
          </div>
        )}
        {!canApply && <p className="text-sm text-muted-foreground">{t('readOnly')}</p>}
        <div className="flex flex-wrap justify-end gap-3">
          <Button variant="outline" disabled={busy} onClick={onClose}>
            {t('cancel')}
          </Button>
          {issue && (
            <Button variant="outline" disabled={busy} onClick={onReview}>
              {t('reviewAgain')}
            </Button>
          )}
          {canApply && (
            <Button
              disabled={
                busy ||
                !preview.valid ||
                !preview.changes.length ||
                !preview.preview_digest ||
                preview.errors.length > 0 ||
                !!issue
              }
              onClick={onApply}
            >
              {busy ? t('working') : t('apply')}
            </Button>
          )}
        </div>
      </div>
    </Dialog>
  )
}
