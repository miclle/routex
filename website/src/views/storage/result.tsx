import { useTranslation } from 'react-i18next'
import type { StorageProbe } from '@/types/storage'
import { Table } from '@/components/ui/table'

export function StorageProbeResult({ result }: { result: StorageProbe }) {
  const { t } = useTranslation('storage')
  return (
    <div className="space-y-3 rounded-md border p-4">
      <p role="status" className="font-medium">
        {t(result.success ? 'testSuccess' : 'testFailure')}
      </p>
      {result.cleanup_pending && <p className="text-sm text-destructive">{t('cleanupPending')}</p>}
      <Table aria-label={t('stagesTitle')}>
        <thead>
          <tr>
            <th>{t('stage')}</th>
            <th>{t('result')}</th>
            <th>{t('duration')}</th>
          </tr>
        </thead>
        <tbody>
          {result.stages.map((stage) => (
            <tr key={stage.name}>
              <td>{t(`stages.${stage.name}`)}</td>
              <td>{t(`statuses.${stage.status}`)}</td>
              <td>{stage.duration_ms} ms</td>
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  )
}
