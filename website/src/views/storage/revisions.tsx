import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { RotateCcw } from 'lucide-react'
import type { StorageRevision, StorageSettings } from '@/types/storage'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Switch } from '@/components/ui/switch'
import { Table } from '@/components/ui/table'

export function StorageRevisions({
  settings,
  busy,
  dirty,
  needsReview,
  canWrite,
  rollingBack,
  onRollback,
}: {
  settings: StorageSettings
  busy: boolean
  dirty: boolean
  needsReview: boolean
  canWrite: boolean
  rollingBack: boolean
  onRollback: (revision: StorageRevision, enabled: boolean) => Promise<void>
}) {
  const { t, i18n } = useTranslation('storage')
  const [selected, setSelected] = useState<StorageRevision | null>(null)
  const [enabled, setEnabled] = useState(settings.enabled)
  const formatter = useMemo(
    () =>
      new Intl.DateTimeFormat(i18n.language === 'zh' ? 'zh-CN' : 'en', {
        dateStyle: 'medium',
        timeStyle: 'short',
      }),
    [i18n.language],
  )

  async function confirm() {
    if (!selected) return
    const revision = selected
    await onRollback(revision, enabled)
    setSelected(null)
  }

  return (
    <section className="space-y-4 border-t pt-6" aria-labelledby="storage-revisions-title">
      <div>
        <h3 id="storage-revisions-title" className="font-semibold">
          {t('revisionsTitle')}
        </h3>
        <p className="mt-1 text-sm text-muted-foreground">{t('revisionsHelp')}</p>
      </div>
      <Table aria-label={t('revisionsTitle')}>
        <thead>
          <tr>
            <th>{t('endpoint')}</th>
            <th>{t('verifiedAt')}</th>
            <th>{t('createdAt')}</th>
            <th>{t('actions')}</th>
          </tr>
        </thead>
        <tbody>
          {settings.revisions.map((item) => {
            const active = item.id === settings.revision?.id
            return (
              <tr key={item.id}>
                <td>
                  <div className="max-w-72 break-all">{item.endpoint}</div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {item.bucket}
                    {item.prefix ? ` / ${item.prefix}` : ''}
                  </div>
                </td>
                <td>{item.verified_at ? formatter.format(new Date(item.verified_at)) : '—'}</td>
                <td>{formatter.format(new Date(item.created_at))}</td>
                <td>
                  {active ? (
                    <Badge variant="outline">{t('active')}</Badge>
                  ) : canWrite ? (
                    <Button
                      size="sm"
                      variant="outline"
                      aria-label={t('revisionActions', { id: item.id })}
                      disabled={busy || dirty || needsReview}
                      onClick={() => {
                        setSelected(item)
                        setEnabled(settings.enabled)
                      }}
                    >
                      <RotateCcw className="size-3.5" aria-hidden="true" />
                      {t('rollback')}
                    </Button>
                  ) : (
                    '—'
                  )}
                </td>
              </tr>
            )
          })}
          {settings.revisions.length === 0 && (
            <tr>
              <td colSpan={4} className="text-center text-muted-foreground">
                {t('emptyRevisions')}
              </td>
            </tr>
          )}
        </tbody>
      </Table>
      {selected && (
        <Dialog
          open
          title={t('rollbackTitle')}
          description={t('rollbackDescription', { id: selected.id })}
          busy={busy}
          onOpenChange={(open) => {
            if (!open) setSelected(null)
          }}
        >
          <div className="space-y-5">
            <div className="flex items-center justify-between gap-4">
              <span className="text-sm font-medium">{t('rollbackEnable')}</span>
              <Switch
                checked={enabled}
                onCheckedChange={setEnabled}
                aria-label={t('rollbackEnable')}
              />
            </div>
            <div className="flex justify-end gap-2">
              <Button variant="outline" disabled={busy} onClick={() => setSelected(null)}>
                {t('cancel')}
              </Button>
              <Button disabled={busy} onClick={() => void confirm()}>
                {t(rollingBack ? 'rollingBack' : 'rollbackConfirm')}
              </Button>
            </div>
          </div>
        </Dialog>
      )}
    </section>
  )
}
