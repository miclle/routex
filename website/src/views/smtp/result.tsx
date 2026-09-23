import { useTranslation } from 'react-i18next'
import type { SMTPTest } from '@/types/smtp'
import { Table } from '@/components/ui/table'
export function SMTPResult({ result }: { result: SMTPTest }) {
  const { t, i18n } = useTranslation('smtp')
  return (
    <section className="space-y-3 rounded-lg border p-4" aria-label={t('lastTest')}>
      <div role="status">
        <h3 className="font-medium">{t(`testStatus.${result.status}`)}</h3>
        <p className="mt-1 text-sm text-muted-foreground">{t(`testHelpStatus.${result.status}`)}</p>
      </div>
      <dl className="grid gap-2 text-xs sm:grid-cols-2">
        <div>
          <dt>{t('requestID')}</dt>
          <dd className="break-all font-mono">{result.request_id}</dd>
        </div>
        <div>
          <dt>{t('revision')}</dt>
          <dd className="break-all font-mono">{result.config_etag}</dd>
        </div>
        <div>
          <dt>{t('started')}</dt>
          <dd>{new Date(result.started_at).toLocaleString(i18n.resolvedLanguage)}</dd>
        </div>
        <div>
          <dt>{t('duration')}</dt>
          <dd>{result.duration_ms} ms</dd>
        </div>
      </dl>
      {result.code && <code className="text-xs">{result.code}</code>}
      {result.stages.length > 0 && (
        <Table>
          <thead>
            <tr>
              <th>{t('stage')}</th>
              <th>{t('result')}</th>
              <th>{t('duration')}</th>
            </tr>
          </thead>
          <tbody>
            {result.stages.map((stage, index) => (
              <tr key={`${stage.name}-${index}`}>
                <td>{t(`stages.${stage.name}`, { defaultValue: stage.name })}</td>
                <td>
                  {t(stage.status === 'passed' ? 'passed' : 'stageFailed')}
                  {stage.code && <code className="ml-2 text-xs">{stage.code}</code>}
                </td>
                <td>{stage.duration_ms} ms</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </section>
  )
}
