import { useTranslation } from 'react-i18next'
import { useParams } from 'react-router'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { Table } from '@/components/ui/table'
import { useTeamModelReview } from './authority'
import TeamRequestPanel from './requests'

export function TeamModelReviewPanel({
  team,
  visible = true,
  showModels = false,
}: {
  team: string
  visible?: boolean
  showModels?: boolean
}) {
  const { t } = useTranslation('teamModelRequests')
  const review = useTeamModelReview(team, visible)
  return (
    <div className="space-y-6">
      {visible && (
        <QueryState
          pending={review.isFetching}
          error={review.error}
          retry={() => void review.refetch()}
        />
      )}
      {showModels && review.current && (
        <section className="space-y-4 rounded-lg border p-4">
          <h2 className="font-medium">{review.current.name}</h2>
          <p className="text-sm text-muted-foreground">{t('sharedHelp')}</p>
          <Table aria-label={t('models')}>
            <thead>
              <tr>
                <th>{t('model')}</th>
                <th>{t('status')}</th>
              </tr>
            </thead>
            <tbody>
              {review.current.models.map((model) => (
                <tr key={model.id}>
                  <td>{model.name}</td>
                  <td>{t(model.status)}</td>
                </tr>
              ))}
            </tbody>
          </Table>
          {!review.current.models.length && (
            <p className="text-sm text-muted-foreground">{t('noModels')}</p>
          )}
        </section>
      )}
      <TeamRequestPanel key={`${review.actor}:${team}`} team={team} visible={!!review.current} />
    </div>
  )
}
export default function TeamModelRequestWorkspacePage() {
  const { t } = useTranslation('teamModelRequests')
  const { resourceId = '' } = useParams()
  return (
    <Page title={t('title')} description={t('member', { id: resourceId })}>
      <TeamModelReviewPanel team={resourceId} showModels />
    </Page>
  )
}
