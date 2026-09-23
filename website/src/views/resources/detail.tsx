import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { useParams, useSearchParams } from 'react-router'
import { getResource } from '@/api/resources'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import CallsPage from '@/views/calls'
import { ProjectUsagePanel } from '@/views/usage'
import ResourceLimits from '@/views/resource-limits'
import ProjectKeysPanel from '@/views/project-keys'
import ProjectRequestsPanel from '@/views/project-requests'
import { ResourceSection } from './shared'
import { ResourcePeople } from './people'
import { ResourceModels } from './models'
import { ResourceSettings } from './settings'
import type { ResourceKind, ResourceRecord } from '@/types/resources'

export default function ResourceDetailPage({
  kind,
  admin = false,
}: {
  kind: ResourceKind
  admin?: boolean
}) {
  const { t } = useTranslation('resources')
  const { resourceId = '' } = useParams()
  const resource = useQuery({
    queryKey: ['resources', kind, admin, resourceId],
    queryFn: ({ signal }) => getResource(kind, admin, resourceId, signal),
  })
  if (!resource.data)
    return (
      <Page title={t('details', { kind: t(kind === 'teams' ? 'team' : 'project') })} description="">
        <QueryState
          pending={resource.isPending}
          error={resource.error}
          retry={() => void resource.refetch()}
        />
      </Page>
    )
  return <ResourceDetail key={resourceId} kind={kind} resource={resource.data} />
}
function ResourceDetail({ kind, resource }: { kind: ResourceKind; resource: ResourceRecord }) {
  const { t, i18n } = useTranslation('resources')
  const session = useSession()
  const access = usePermissions()
  const [params, setParams] = useSearchParams()
  const isManager =
    kind === 'projects' &&
    !!resource.managers?.some((person) => person.user_id === session.data?.user.id)
  const canEdit = access.can(`${kind}.write`) || isManager
  const canModels = access.can(`${kind}.models.write`) && resource.status !== 'archived'
  const canCalls = kind === 'projects' && (isManager || access.can('calls.read_all'))
  const canRequests =
    kind === 'projects' &&
    (isManager || access.can('projects.models.write') || access.can('projects.read_all'))
  const tabs =
    kind === 'teams'
      ? ['overview', 'members', 'models', 'settings']
      : [
          'overview',
          ...(canEdit ? ['keys'] : []),
          'resources',
          ...(canCalls ? ['usage', 'calls'] : []),
          'settings',
        ]
  const selected = params.get('tab') ?? 'overview'
  const tab = tabs.includes(selected) ? selected : 'overview'
  const label = t(kind === 'teams' ? 'team' : 'project')
  return (
    <section className="space-y-6">
      <header className="space-y-1">
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold">{resource.name}</h1>
          <Badge variant="outline">{t(resource.status)}</Badge>
        </div>
        <p className="text-sm text-muted-foreground">
          {kind === 'projects' && (
            <>
              {t('identity', { kind: label })}: {resource.id} ·{' '}
            </>
          )}
          {resource.description || t('noDescription')}
        </p>
      </header>
      <Tabs
        value={tab}
        onValueChange={(value) =>
          setParams(value === 'overview' ? {} : { tab: String(value) }, { replace: true })
        }
      >
        <TabsList aria-label={t('details', { kind: label })}>
          {tabs.map((value) => (
            <TabsTrigger key={value} value={value}>
              {t(value)}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="overview">
          <div className="space-y-6">
            <ResourceSection title={t('basic')}>
              <dl className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
                <div>
                  <dt className="text-sm text-muted-foreground">
                    {t('identity', { kind: label })}
                  </dt>
                  <dd className="mt-1 font-mono text-sm">{resource.id}</dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">{t('created')}</dt>
                  <dd className="mt-1 text-sm">
                    {new Date(resource.created_at).toLocaleString(
                      i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
                    )}
                  </dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">{t('status')}</dt>
                  <dd className="mt-1 text-sm">{t(resource.status)}</dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">
                    {kind === 'teams' ? t('memberCount') : t('managerCount')}
                  </dt>
                  <dd className="mt-1 text-sm">
                    {kind === 'teams'
                      ? (resource.members?.length ?? 0)
                      : (resource.managers?.length ?? 0)}
                  </dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">{t('modelCount')}</dt>
                  <dd className="mt-1 text-sm">{resource.model_ids.length}</dd>
                </div>
              </dl>
            </ResourceSection>
            <ResourceSection title={kind === 'teams' ? t('owners') : t('managers')}>
              <div className="space-y-3">
                {(kind === 'teams'
                  ? resource.members?.filter(
                      (person) => person.role === 'owner' && person.status === 'active',
                    )
                  : resource.managers
                )?.map((person) => (
                  <div className="flex items-center gap-3" key={person.user_id}>
                    <span className="flex size-8 items-center justify-center rounded-full bg-muted text-xs">
                      {person.name.slice(0, 2).toUpperCase()}
                    </span>
                    <div>
                      <p className="text-sm">{person.name}</p>
                      <p className="text-xs text-muted-foreground">{person.email}</p>
                    </div>
                  </div>
                ))}
              </div>
            </ResourceSection>
          </div>
        </TabsContent>
        {kind === 'teams' && (
          <TabsContent value="members">
            <ResourcePeople
              resource={resource}
              kind={kind}
              canEdit={canEdit && resource.status !== 'archived'}
            />
          </TabsContent>
        )}
        <TabsContent value={kind === 'teams' ? 'models' : 'resources'}>
          <div className="space-y-6">
            <ResourceModels resource={resource} kind={kind} canEdit={canModels} />
            {kind === 'projects' &&
              (isManager ||
                access.can('projects.read_all') ||
                access.can('projects.limits.write')) && (
                <ResourceLimits
                  key={resource.id}
                  path={`/projects/${resource.id}`}
                  canEdit={access.can('projects.limits.write') && resource.status === 'active'}
                />
              )}
            {canRequests && <ProjectRequestsPanel project={resource} />}
          </div>
        </TabsContent>
        {kind === 'projects' && canEdit && (
          <TabsContent value="keys">
            <ProjectKeysPanel project={resource} />
          </TabsContent>
        )}
        {canCalls && (
          <TabsContent value="usage">
            <ProjectUsagePanel projectId={resource.id} />
          </TabsContent>
        )}
        {canCalls && (
          <TabsContent value="calls">
            <CallsPage projectId={resource.id} />
          </TabsContent>
        )}
        <TabsContent value="settings">
          <ResourceSettings
            resource={resource}
            kind={kind}
            canEdit={canEdit}
            canLifecycle={access.can(`${kind}.write`)}
          />
        </TabsContent>
      </Tabs>
    </section>
  )
}
