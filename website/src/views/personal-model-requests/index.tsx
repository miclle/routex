import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useParams } from 'react-router'
import { getPersonalModelWorkspace } from '@/api/personal-model-requests'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { Table } from '@/components/ui/table'
import RequestPanel from './requests'

export function PersonalModelMemberPanel({
  owner,
  visible = true,
}: {
  owner: string
  visible?: boolean
}) {
  const { t } = useTranslation('personalModelRequests')
  const session = useSession()
  const access = usePermissions()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const allowed =
    visible &&
    !!actor &&
    !session.isFetching &&
    access.can('members.models.write') &&
    !access.isError &&
    !access.isFetching
  const query = useQuery({
    queryKey: ['personal-model-workspace', actor, owner],
    queryFn: ({ signal }) => getPersonalModelWorkspace(owner, signal),
    enabled: allowed,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const current = allowed && query.isSuccess && !query.isFetching ? query.data : undefined
  return (
    <div className="space-y-6">
      {visible && !allowed && !session.isFetching && !access.isFetching && (
        <p role="alert">{t('unauthorized')}</p>
      )}
      {allowed && (
        <QueryState
          pending={query.isFetching}
          error={query.error}
          retry={() => void query.refetch()}
        />
      )}
      {current && (
        <>
          <section className="space-y-4 rounded-lg border p-4">
            <h3 className="font-medium">{t('models')}</h3>
            <Table aria-label={t('models')}>
              <thead>
                <tr>
                  <th>{t('model')}</th>
                  <th>{t('status')}</th>
                </tr>
              </thead>
              <tbody>
                {current.models.map((model) => (
                  <tr key={model.id}>
                    <td>{model.name}</td>
                    <td>{t(model.status)}</td>
                  </tr>
                ))}
              </tbody>
            </Table>
            {!current.models.length && (
              <p className="text-sm text-muted-foreground">{t('noModels')}</p>
            )}
            <p className="text-xs text-muted-foreground">{t('sourceHelp')}</p>
          </section>
        </>
      )}
      <RequestPanel key={`${actor}:${owner}`} owner={owner} visible={!!current} />
    </div>
  )
}
export default function PersonalModelWorkspacePage() {
  const { t } = useTranslation('personalModelRequests')
  const { memberId = '' } = useParams()
  return (
    <Page title={t('title')} description={t('member', { id: memberId })}>
      <PersonalModelMemberPanel owner={memberId} />
    </Page>
  )
}
