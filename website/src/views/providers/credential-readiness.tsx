import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getCredentialReadiness } from '@/api/credential-readiness'
import { credentialReadinessBlockers } from '@/types/credential-readiness'
import { usePermissions } from '@/hooks/use-permissions'
import { QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import CredentialRetirementControls from './credential-retirement'

type Props = {
  providerId: string
  credentialId: string
  sourceCredentialId: string
  connectionId: string
  connectionName: string
  onClose: () => void
}

export default function CredentialReadinessDialog(props: Props) {
  const { t, i18n } = useTranslation('catalog')
  const access = usePermissions()
  const cache = useQueryClient()
  const query = useQuery({
    queryKey: [
      'admin',
      'credential-retirement-readiness',
      props.providerId,
      props.connectionId,
      props.sourceCredentialId,
      props.credentialId,
    ],
    queryFn: ({ signal }) =>
      getCredentialReadiness(
        props.sourceCredentialId,
        props.credentialId,
        props.connectionId,
        signal,
      ),
    enabled: access.can('providers.read'),
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    gcTime: 0,
  })
  if (!access.can('providers.read')) return null
  // A failed refresh must not continue presenting earlier eligibility as current.
  const record = !query.isFetching && !query.isError ? query.data : undefined
  return (
    <Dialog
      open
      width={560}
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('credentialReadiness.title')}
      description={t('credentialReadiness.description')}
    >
      <div className="space-y-5">
        <p className="text-sm text-muted-foreground">
          {t('credentialReadiness.connection', {
            name: props.connectionName,
            id: props.connectionId,
          })}
        </p>
        <p className="rounded-md border bg-muted/30 p-3 text-sm">
          {t('credentialReadiness.advisory')}
        </p>
        <QueryState
          pending={query.isFetching}
          error={query.error}
          retry={() => void query.refetch()}
        />
        {record && (
          <>
            <dl className="grid grid-cols-2 gap-4 text-sm">
              <div>
                <dt className="text-muted-foreground">{t('credentialReadiness.source')}</dt>
                <dd className="mt-1">{record.source.name}</dd>
                <dd className="break-all text-xs text-muted-foreground">{record.source.id}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('credentialReadiness.replacement')}</dt>
                <dd className="mt-1">{record.replacement.name}</dd>
                <dd className="break-all text-xs text-muted-foreground">{record.replacement.id}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('credentialReadiness.routes')}</dt>
                <dd className="mt-1">
                  {record.snapshot_id
                    ? t('credentialReadiness.routeCount', { count: record.eligible_route_count })
                    : t('credentialReadiness.unknown')}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('credentialReadiness.snapshot')}</dt>
                <dd className="mt-1 break-all">
                  {record.snapshot_id ?? t('credentialReadiness.unknown')}
                </dd>
              </div>
            </dl>
            <div role="status" className="rounded-md border p-3 text-sm">
              <p className="font-medium">
                {t(
                  record.eligible ? 'credentialReadiness.eligible' : 'credentialReadiness.blocked',
                )}
              </p>
              {record.evidence ? (
                <p className="mt-2 break-all">
                  {t('credentialReadiness.evidence', { id: record.evidence.attempt_id })}{' '}
                  <time dateTime={record.evidence.completed_at}>
                    {new Date(record.evidence.completed_at).toLocaleString(
                      i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
                    )}
                  </time>
                </p>
              ) : (
                <p className="mt-2">{t('credentialReadiness.noEvidence')}</p>
              )}
              {record.blockers.length > 0 && (
                <ul className="mt-2 list-disc space-y-1 pl-5">
                  {record.blockers.map((blocker, index) => (
                    <li key={`${blocker}:${index}`}>
                      {t(
                        credentialReadinessBlockers.includes(
                          blocker as (typeof credentialReadinessBlockers)[number],
                        )
                          ? `credentialReadiness.blockers.${blocker}`
                          : 'credentialReadiness.blockers.unknown',
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </>
        )}
        {query.data && (
          <CredentialRetirementControls
            initial={query.data}
            current={record}
            reload={async () => {
              const refreshed = await query.refetch()
              if (refreshed.isError || !refreshed.data) throw refreshed.error
              return refreshed.data
            }}
            onApplied={() => {
              void cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
              void cache.invalidateQueries({
                predicate: ({ queryKey }) =>
                  queryKey[0] === 'admin' &&
                  (queryKey.includes(props.sourceCredentialId) ||
                    queryKey.includes(props.credentialId)),
              })
            }}
          />
        )}
        <div className="flex justify-end gap-2">
          <Button
            variant="outline"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            {t('credentialReadiness.refresh')}
          </Button>
          <Button onClick={props.onClose}>{t('credentialReadiness.close')}</Button>
        </div>
      </div>
    </Dialog>
  )
}
