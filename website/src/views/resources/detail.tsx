import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, Navigate, useParams, useSearchParams } from 'react-router'
import { ArrowUpRight } from 'lucide-react'
import { getResource } from '@/api/resources'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { useTeamAccess } from '@/hooks/use-team-access'
import TeamRolesPanel from './team-roles'
import ProjectOverview from './project-overview'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { Badge } from '@/components/ui/badge'
import { buttonVariants } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import CallsPage from '@/views/calls'
import { ProjectUsagePanel } from '@/views/usage'
import ResourceLimits from '@/views/resource-limits'
import ProjectKeysPanel from '@/views/project-keys'
import ProjectRequestsPanel from '@/views/project-requests'
import TeamModelHistory from '@/views/team-model-requests/team-history'
import { ResourceSection } from './shared'
import { ResourcePeople } from './people'
import { ResourceModels } from './models'
import { ResourceSettings } from './settings'
import type { ResourceKind, ResourceRecord } from '@/types/resources'
import type { Session } from '@/types/auth'
import { useRestoreOwner, type RestoreOwner } from '@/views/default-limits/restore-owner'

export default function ResourceDetailPage({
  kind,
  admin = false,
}: {
  kind: ResourceKind
  admin?: boolean
}) {
  const { t } = useTranslation('resources')
  const { resourceId = '' } = useParams()
  const session = useSession()
  const [params] = useSearchParams()
  const [modelHistoryMounted, setModelHistoryMounted] = useState(params.get('tab') === 'models')
  if (params.get('tab') === 'models' && !modelHistoryMounted) setModelHistoryMounted(true)
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const cache = useQueryClient()
  const restoreOwner = useRestoreOwner(
    session.data?.user.id ?? '',
    { kind: 'team', id: resourceId },
    `${kind}:${params.get('tab') ?? ''}`,
  )
  function restoreHostCurrent() {
    const parent = cache.getQueryState<ResourceRecord>([
      'resources',
      kind,
      admin,
      resourceId,
      restoreOwner.actor,
    ])
    const auth = cache.getQueryState<Session>(['auth', 'session'])
    return (
      kind === 'teams' &&
      auth?.status === 'success' &&
      auth.fetchStatus === 'idle' &&
      !auth.error &&
      !auth.isInvalidated &&
      auth.data?.user.id === restoreOwner.actor &&
      parent?.status === 'success' &&
      parent.fetchStatus === 'idle' &&
      !parent.error &&
      !parent.isInvalidated &&
      parent.data?.id === resourceId &&
      parent.data.status === 'active'
    )
  }
  const resource = useQuery({
    queryKey: ['resources', kind, admin, resourceId, actor],
    queryFn: ({ signal }) => getResource(kind, admin, resourceId, signal),
    enabled: !!actor,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const teamAccess = useTeamAccess(
    resourceId,
    kind === 'teams' &&
      !!resource.data &&
      !resource.data.resource_limit_workspace_only &&
      resource.data.status === 'active',
  )
  const fresh = !!actor && !!resource.data && resource.isSuccess && !resource.isFetching
  const requestedTab = params.get('tab')
  const legacyTab =
    kind === 'projects'
      ? requestedTab === 'managers'
        ? 'settings'
        : requestedTab === 'models' || requestedTab === 'limits'
          ? 'resources'
          : undefined
      : undefined
  const canonicalParams = new URLSearchParams(params)
  if (legacyTab) canonicalParams.set('tab', legacyTab)
  const canShow = fresh && (!legacyTab || (!session.isFetching && !session.isError))
  return (
    <>
      {canShow && legacyTab ? (
        <Navigate replace to={{ search: `?${canonicalParams.toString()}` }} />
      ) : canShow ? (
        <ResourceDetail
          key={`${actor}:${resourceId}`}
          kind={kind}
          resource={resource.data!}
          restoreOwner={restoreOwner}
          restoreHostCurrent={restoreHostCurrent}
        />
      ) : (
        <Page
          title={t('details', { kind: t(kind === 'teams' ? 'team' : 'project') })}
          description=""
        >
          <QueryState
            pending={
              resource.isPending || resource.isFetching || (!!legacyTab && session.isFetching)
            }
            error={resource.error}
            retry={() => void resource.refetch()}
          />
        </Page>
      )}
      {kind === 'teams' &&
        modelHistoryMounted &&
        resource.data?.resource_limit_workspace_only !== true && (
          <TeamModelHistory
            key={`history:${actor}:${resourceId}`}
            team={resourceId}
            reviewer={
              teamAccess.current?.actor_team_actions.includes('teams.models.write') === true
            }
            visible={
              fresh && params.get('tab') === 'models' && !session.isFetching && !session.isError
            }
          />
        )}
    </>
  )
}
function ResourceDetail({
  kind,
  resource,
  restoreOwner,
  restoreHostCurrent,
}: {
  kind: ResourceKind
  resource: ResourceRecord
  restoreOwner: RestoreOwner
  restoreHostCurrent: () => boolean
}) {
  const { t, i18n } = useTranslation('resources')
  const session = useSession()
  const access = usePermissions()
  const [params, setParams] = useSearchParams()
  const isManager =
    kind === 'projects' &&
    !!resource.managers?.some((person) => person.user_id === session.data?.user.id)
  const teamAccess = useTeamAccess(
    resource.id,
    kind === 'teams' && resource.status === 'active' && !resource.resource_limit_workspace_only,
  )
  const canEdit =
    kind === 'teams'
      ? resource.status === 'active'
        ? teamAccess.current?.actor_team_actions.includes('teams.write') === true
        : access.can('teams.write')
      : access.can('projects.write') || isManager
  const canModels =
    (kind === 'teams'
      ? resource.status === 'active'
        ? teamAccess.current?.actor_team_actions.includes('teams.models.write') === true
        : access.can('teams.models.write')
      : access.can('projects.models.write')) && resource.status !== 'archived'
  const ownTeam =
    kind === 'teams' &&
    resource.status === 'active' &&
    !!resource.members?.some(
      (person) => person.user_id === session.data?.user.id && person.status === 'active',
    )
  const canTeamLimits =
    kind === 'teams' &&
    (ownTeam ||
      ['teams.read_all', 'teams.tokens.write', 'teams.money.write', 'teams.rates.write'].some(
        (permission) => access.can(permission),
      ))
  const canCalls = kind === 'projects' && (isManager || access.can('calls.read_all'))
  const canRequests =
    kind === 'projects' &&
    (isManager ||
      access.can('projects.models.write') ||
      access.can('projects.limits.write') ||
      access.can('projects.read_all'))
  if (resource.request_workspace_only === true && kind === 'projects')
    return (
      <section className="space-y-6">
        {(access.can('projects.limits.write') || access.can('projects.read_all')) &&
          !access.isError &&
          !access.isFetching && (
            <header className="space-y-1">
              <div className="flex items-center gap-3">
                <h1 className="text-2xl font-semibold">{resource.name}</h1>
                <Badge variant="outline">{t(resource.status)}</Badge>
              </div>
              <p className="text-sm text-muted-foreground">{resource.description}</p>
            </header>
          )}
        <Tabs value="resources">
          <TabsList aria-label={t('details', { kind: t('project') })}>
            <TabsTrigger value="resources">{t('resources')}</TabsTrigger>
          </TabsList>
          <TabsContent value="resources">
            <ProjectRequestsPanel project={resource} />
          </TabsContent>
        </Tabs>
      </section>
    )
  if (resource.resource_limit_workspace_only === true && kind === 'teams')
    return (
      <section className="space-y-6">
        <header className="space-y-1">
          <h1 className="text-2xl font-semibold">{resource.name}</h1>
          <p className="text-sm text-muted-foreground">{resource.description}</p>
        </header>
        <Tabs value="limits">
          <TabsList aria-label={t('details', { kind: t('team') })}>
            <TabsTrigger value="limits">{t('limits')}</TabsTrigger>
          </TabsList>
          <TabsContent value="limits">
            <ResourceLimits
              path={`/teams/${resource.id}`}
              team={{ teamId: resource.id }}
              restoreOwner={restoreOwner}
              restoreHostCurrent={restoreHostCurrent}
              canEdit={resource.status === 'active'}
            />
          </TabsContent>
        </Tabs>
      </section>
    )
  const tabs =
    kind === 'teams'
      ? [
          'overview',
          'members',
          ...(resource.status === 'active' ? ['roles'] : []),
          'models',
          ...(canTeamLimits ? ['limits'] : []),
          ...(ownTeam ? ['calls'] : []),
          'settings',
        ]
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
          {kind === 'projects' ? (
            <ProjectOverview resource={resource} isManager={isManager} canKeys={canEdit} />
          ) : (
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
                {ownTeam && (
                  <Link
                    className={buttonVariants({ variant: 'outline' })}
                    to={`/playground?${ownTeam ? 'team' : 'project'}=${encodeURIComponent(resource.id)}`}
                  >
                    {t('openInPlayground')}
                    <ArrowUpRight className="size-4" aria-hidden="true" />
                  </Link>
                )}
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
          )}
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
        {kind === 'teams' && resource.status === 'active' && (
          <TabsContent value="roles">
            <TeamRolesPanel team={resource.id} access={teamAccess} />
          </TabsContent>
        )}
        {kind === 'teams' && canTeamLimits && (
          <TabsContent value="limits">
            <ResourceLimits
              path={`/teams/${resource.id}`}
              team={{ teamId: resource.id }}
              restoreOwner={restoreOwner}
              restoreHostCurrent={restoreHostCurrent}
              canEdit={resource.status === 'active'}
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
        {(canCalls || ownTeam) && (
          <TabsContent value="calls">
            <CallsPage
              projectId={kind === 'projects' ? resource.id : undefined}
              teamId={ownTeam ? resource.id : undefined}
            />
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
