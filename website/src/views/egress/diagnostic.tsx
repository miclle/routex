import { useTranslation } from 'react-i18next'
import type { EgressDiagnostic } from '@/types/egress'
import { Table } from '@/components/ui/table'
export function Diagnostic({ result }: { result: EgressDiagnostic }) {
  const { t } = useTranslation('egress')
  return (
    <div className="space-y-4">
      <div role="status" className="space-y-1 rounded-md border p-3 text-sm">
        <p>{t(result.transport_ok ? 'transportPass' : 'transportFail')}</p>
        <p>
          {t(result.api_ok ? 'apiPass' : 'apiFail')}
          {result.http_status ? ` · HTTP ${result.http_status}` : ''}
        </p>
        {result.stale && <p className="text-destructive">{t('stale')}</p>}
      </div>
      <Table aria-label={t('diagnostics')} className="min-w-[520px]">
        <thead>
          <tr>
            <th>{t('stage')}</th>
            <th>{t('result')}</th>
            <th>{t('duration')}</th>
          </tr>
        </thead>
        <tbody>
          {result.stages.map((stage) => (
            <tr key={stage.stage}>
              <td>{t(`stages.${stage.stage}`, { defaultValue: stage.stage })}</td>
              <td>
                {t(`statuses.${stage.status}`, { defaultValue: stage.status })}
                {stage.code && <code className="ml-2 text-xs">{stage.code}</code>}
              </td>
              <td>{stage.duration_ms === undefined ? '—' : `${stage.duration_ms} ms`}</td>
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  )
}
