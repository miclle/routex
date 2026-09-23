import { t, locale } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router'
import { FolderKanban, MoreHorizontal, Plus } from 'lucide-react'
import { getResources, resourcePath } from '@/api/resources'
import { writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, QueryState, ErrorNotice, SaveButton } from '@/components/app/CatalogUI'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Table } from '@/components/ui/table'
import { Dialog } from '@/components/ui/dialog'
import { Menu, MenuItem } from '@/components/ui/menu'
import { resourceLabels, statusLabels } from './labels'
import type {
  ResourceFilters,
  ResourceKind,
  ResourceRecord,
  ResourceStatus,
} from '@/types/resources'
export default function ResourceListPage({
  kind,
  admin = false,
}: {
  kind: ResourceKind
  admin?: boolean
}) {
  useTranslation()

  return admin ? (
    <PermissionGate permission={`${kind}.read_all`}>
      <ResourceList kind={kind} admin />
    </PermissionGate>
  ) : (
    <ResourceList kind={kind} admin={false} />
  )
}
function ResourceList({ kind, admin }: { kind: ResourceKind; admin: boolean }) {
  useTranslation()

  const label = resourceLabels[kind]
  const singular = t(kind === 'teams' ? 'resources:team' : 'resources:project')
  const path = resourcePath(kind, admin)
  const navigate = useNavigate()
  const permissions = usePermissions()
  const session = useSession()
  const cache = useQueryClient()
  const [filters, setFilters] = useState<ResourceFilters>({ q: '', status: '' })
  const [change, setChange] = useState<{ resource: ResourceRecord; status: ResourceStatus } | null>(
    null,
  )
  const data = useInfiniteQuery({
    queryKey: ['resources', kind, admin, filters],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) => getResources(kind, admin, filters, pageParam, signal),
    getNextPageParam: (page) => page.next_cursor ?? undefined,
  })
  const items = data.data?.pages.flatMap((page) => page.items)
  const update = useMutation({
    mutationFn: () =>
      writeCatalog(
        'patch',
        `${kind === 'teams' ? '/admin/teams' : '/projects'}/${change!.resource.id}`,
        { status: change!.status },
        session.data!.csrf_token,
      ),
    onSuccess: () => {
      setChange(null)
      void cache.invalidateQueries({ queryKey: ['resources'] })
    },
  })
  const canCreate = kind === 'projects' || permissions.can('teams.write')
  function changeStatus(resource: ResourceRecord, status: ResourceStatus) {
    update.reset()
    setChange({ resource, status })
  }
  return (
    <Page
      title={admin ? label : t('my_value_9da3d', { v0: label })}
      description={t('manage_membership_and_model_access_for_value_88e43', { v0: label })}
    >
      <div className="flex flex-wrap items-center justify-between gap-4">
        {admin || kind === 'teams' ? (
          <form
            aria-label={t('value_filters_8dcd2', { v0: label })}
            className="flex flex-wrap gap-4"
            onSubmit={(event) => {
              event.preventDefault()
              const form = new FormData(event.currentTarget)
              setFilters({
                q: String(form.get('q') || '').trim(),
                status: String(form.get('status') || ''),
              })
            }}
          >
            <Input
              name="q"
              aria-label={t('search_value_9f660', { v0: label })}
              placeholder={t('resources:searchName', { kind: singular })}
              className="w-80"
            />
            <select
              name="status"
              aria-label={t('value_status_4eb0d', { v0: label })}
              className="h-9 w-36 rounded-md border bg-background px-3 text-sm"
            >
              <option value="">{t('all_statuses_1a4c2')}</option>
              {Object.entries(statusLabels).map(([value, title]) => (
                <option key={value} value={value}>
                  {title}
                </option>
              ))}
            </select>
            <Button type="submit" variant="outline">
              {t('filter_dcce9')}
            </Button>
          </form>
        ) : (
          <p className="text-sm text-muted-foreground">
            {t('projects_you_manage_including_their_credentials_models_and_4a627')}
          </p>
        )}
        {canCreate && (
          <Button onClick={() => navigate(`${path}/new`)}>
            <Plus className="size-4" />
            {t('resources:create', { kind: singular })}
          </Button>
        )}
      </div>
      <QueryState
        pending={data.isPending}
        error={data.error}
        retry={() => void data.refetch()}
        empty={items?.length === 0}
      />
      <Table
        aria-label={
          admin ? t('value_list_da7c8', { v0: label }) : t('my_value_list_11f15', { v0: label })
        }
      >
        <thead>
          <tr>
            <th>{label}</th>
            <th>{kind === 'teams' ? t('members_c1ee9') : t('managers_7c2c6')}</th>
            <th>{t('models_98fd0')}</th>
            <th>{t('status_62e95')}</th>
            <th>{t('created_84e38')}</th>
            <th>{t('actions_f3ea6')}</th>
          </tr>
        </thead>
        <tbody>
          {items?.map((item) => (
            <tr key={item.id}>
              <td>
                <Link
                  className="inline-flex items-center gap-2 text-primary"
                  to={`${path}/${item.id}`}
                >
                  {kind === 'projects' && <FolderKanban className="size-4" />}
                  {item.name}
                </Link>
              </td>
              <td>
                {kind === 'teams'
                  ? t('resources:peopleCount', { count: item.members?.length ?? 0 })
                  : admin
                    ? new Intl.ListFormat(locale()).format(
                        item.managers?.map((person) => person.name) ?? [],
                      )
                    : t('resources:peopleCount', { count: item.managers?.length ?? 0 })}
              </td>
              <td>{t('resources:modelCount', { count: item.model_ids.length })}</td>
              <td>
                <Badge variant="outline">{statusLabels[item.status]}</Badge>
              </td>
              <td>{new Date(item.created_at).toLocaleDateString(locale())}</td>
              <td>
                <Menu
                  label={t('more_actions_for_value_c2374', { v0: item.name })}
                  trigger={<MoreHorizontal className="size-4" />}
                >
                  <MenuItem onClick={() => navigate(`${path}/${item.id}`)}>
                    {t('resources:view', { kind: singular })}
                  </MenuItem>
                  {kind === 'teams' && (
                    <MenuItem onClick={() => navigate(`${path}/${item.id}?tab=members`)}>
                      {t('view_members_34642')}
                    </MenuItem>
                  )}
                  <MenuItem onClick={() => navigate(`${path}/${item.id}?tab=settings`)}>
                    {t('resources:resourceSettings', { kind: singular })}
                  </MenuItem>
                  {permissions.can(`${kind}.write`) && item.status !== 'archived' && (
                    <>
                      <MenuItem
                        onClick={() =>
                          changeStatus(item, item.status === 'active' ? 'disabled' : 'active')
                        }
                      >
                        {item.status === 'active'
                          ? t('disable_value_4bb46', { v0: label })
                          : t('reenable_value_7d7f9', { v0: label })}
                      </MenuItem>
                      {item.status === 'disabled' && (
                        <MenuItem onClick={() => changeStatus(item, 'archived')}>
                          {t('resources:archive', { kind: singular })}
                        </MenuItem>
                      )}
                    </>
                  )}
                </Menu>
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
      {data.hasNextPage && (
        <div className="text-center">
          <Button
            variant="outline"
            disabled={data.isFetchingNextPage}
            onClick={() => void data.fetchNextPage()}
          >
            {t('load_more_3a0fa')}
          </Button>
        </div>
      )}
      <Dialog
        open={!!change}
        onOpenChange={(open) => {
          if (!open) setChange(null)
        }}
        busy={update.isPending}
        title={t('confirm_value_for_value_a240d', {
          v0: change ? statusLabels[change.status] : '',
          v1: label,
        })}
        description={
          change?.status === 'archived'
            ? t('archiving_is_irreversible_and_prevents_further_changes_historical_a438f')
            : t('status_changes_affect_resource_access_and_calls_the_733f7')
        }
      >
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            if (!update.isPending) update.mutate()
          }}
        >
          <p>{change?.resource.name}</p>
          <ErrorNotice error={update.error} />
          <SaveButton pending={update.isPending}>{t('confirm_change_3ced6')}</SaveButton>
        </form>
      </Dialog>
    </Page>
  )
}
