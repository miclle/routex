import { useTranslation } from 'react-i18next'
import { useSession } from '@/hooks/use-auth'
import { QueryState } from '@/components/app/CatalogUI'
import type { ResourceKind } from '@/types/resources'
import ProjectCreation from './project-creation'
import TeamCreationPage from './team-creation'
export default function CreateResourcePage({
  kind,
  admin = false,
}: {
  kind: ResourceKind
  admin?: boolean
}) {
  useTranslation()

  return kind === 'teams' ? <TeamCreationPage admin={admin} /> : <CreateProjectPage admin={admin} />
}
function CreateProjectPage({ admin }: { admin: boolean }) {
  const session = useSession()
  const actor = session.data?.user.id ?? ''
  return actor && session.data ? (
    <ProjectCreation
      key={actor}
      admin={admin}
      user={session.data.user}
      generation={session.dataUpdatedAt}
      visible={!session.isError && !session.isFetching}
    />
  ) : (
    <QueryState
      pending={session.isPending}
      error={session.error}
      retry={() => void session.refetch()}
    />
  )
}
