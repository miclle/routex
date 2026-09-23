import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import { MoreHorizontal } from 'lucide-react'
import { Menu, MenuItem } from '@/components/ui/menu'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { ErrorNotice, SaveButton } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Dialog } from '@/components/ui/dialog'
import { CandidatePicker, ResourceSection } from './shared'
import type { ResourceKind, ResourcePerson, ResourceRecord } from '@/types/resources'
export function ResourcePeople({
  resource,
  kind,
  canEdit,
}: {
  resource: ResourceRecord
  kind: ResourceKind
  canEdit: boolean
}) {
  useTranslation()

  const session = useSession()
  const cache = useQueryClient()
  const [search, setSearch] = useState('')
  const [adding, setAdding] = useState(false)
  const [selected, setSelected] = useState<string[]>([])
  const [change, setChange] = useState<{
    person: ResourcePerson
    action: 'remove' | 'role' | 'status'
  } | null>(null)
  const people = kind === 'teams' ? (resource.members ?? []) : (resource.managers ?? [])
  const update = useMutation({
    mutationFn: (next: { user_id: string; role?: string; status?: string }[]) =>
      writeCatalog<ResourceRecord>(
        'put',
        kind === 'teams'
          ? `/admin/teams/${resource.id}/members`
          : `/projects/${resource.id}/managers`,
        kind === 'teams' ? { members: next } : { user_ids: next.map((person) => person.user_id) },
        session.data!.csrf_token,
      ),
    onSuccess: () => {
      setAdding(false)
      setSelected([])
      setChange(null)
      void cache.invalidateQueries({ queryKey: ['resources'] })
    },
  })
  const inputPeople = (): { user_id: string; role?: string; status?: string }[] =>
    people.map(({ user_id, role, status }) => ({
      user_id,
      ...(kind === 'teams' ? { role, status } : {}),
    }))
  function applyChange() {
    if (!change || update.isPending) return
    const next = inputPeople().flatMap((person) =>
      person.user_id !== change.person.user_id
        ? [person]
        : change.action === 'remove'
          ? []
          : [
              {
                ...person,
                ...(change.action === 'role'
                  ? { role: person.role === 'owner' ? 'member' : 'owner' }
                  : { status: person.status === 'active' ? 'disabled' : 'active' }),
              },
            ],
    )
    update.mutate(next)
  }
  const table = (
    <Table
      aria-label={
        kind === 'teams'
          ? t('value_member_list_7a1d8', { v0: resource.name })
          : t('project_manager_list_fe081')
      }
    >
      <thead>
        <tr>
          <th>{kind === 'teams' ? t('members_c1ee9') : t('name_be4c2')}</th>
          <th>{kind === 'teams' ? t('status_62e95') : t('email_9ed62')}</th>
          <th>{t('actions_f3ea6')}</th>
        </tr>
      </thead>
      <tbody>
        {people
          .filter((person) =>
            `${person.name} ${person.email}`.toLowerCase().includes(search.trim().toLowerCase()),
          )
          .map((person) => (
            <tr key={person.user_id}>
              <td>
                {kind === 'teams' ? (
                  <div className="flex items-center gap-3">
                    <span className="flex size-8 items-center justify-center rounded-full bg-muted text-xs">
                      {person.name.slice(0, 2).toUpperCase()}
                    </span>
                    <div>
                      <div className="flex items-center gap-2">
                        {person.name}
                        {person.role === 'owner' && (
                          <Badge variant="outline">{t('owner_974d3')}</Badge>
                        )}
                      </div>
                      <p className="text-xs text-muted-foreground">{person.email}</p>
                    </div>
                  </div>
                ) : (
                  person.name
                )}
              </td>
              <td>
                {kind === 'teams' ? (
                  <Badge variant="outline">
                    {person.status === 'active' ? t('active_f78d0') : t('disabled_6c7dc')}
                  </Badge>
                ) : (
                  <span className="text-muted-foreground">{person.email}</span>
                )}
              </td>
              <td>
                {kind === 'teams' ? (
                  <Menu
                    label={t('more_actions_for_value_c2374', { v0: person.name })}
                    trigger={<MoreHorizontal className="size-4" />}
                  >
                    <MenuItem
                      disabled={
                        !canEdit ||
                        update.isPending ||
                        (person.role !== 'owner' && person.status !== 'active')
                      }
                      onClick={() => {
                        update.reset()
                        setChange({ person, action: 'role' })
                      }}
                    >
                      {person.role === 'owner' ? t('make_member_d1d9f') : t('make_owner_cde9c')}
                    </MenuItem>
                    <MenuItem
                      disabled={!canEdit || update.isPending}
                      onClick={() => {
                        update.reset()
                        setChange({ person, action: 'status' })
                      }}
                    >
                      {person.status === 'active'
                        ? t('disable_member_d67d0')
                        : t('enable_member_af398')}
                    </MenuItem>
                    <MenuItem
                      disabled={!canEdit || update.isPending || people.length <= 1}
                      onClick={() => {
                        update.reset()
                        setChange({ person, action: 'remove' })
                      }}
                    >
                      {t('remove_2f752')}
                    </MenuItem>
                  </Menu>
                ) : (
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-destructive"
                    disabled={!canEdit || update.isPending || people.length <= 1}
                    onClick={() => {
                      update.reset()
                      setChange({ person, action: 'remove' })
                    }}
                  >
                    {t('remove_2f752')}
                  </Button>
                )}
              </td>
            </tr>
          ))}
      </tbody>
    </Table>
  )
  const addButton = (
    <Button
      disabled={!canEdit}
      onClick={() => {
        update.reset()
        setSelected([])
        setAdding(true)
      }}
    >
      {kind === 'teams' ? t('add_member_bd09d') : t('add_manager_d30c1')}
    </Button>
  )
  return (
    <>
      <div className="space-y-4">
        {kind === 'teams' ? (
          <>
            <div className="flex items-center justify-between gap-4">
              <Input
                aria-label={t('search_team_members_15982')}
                placeholder={t('search_member_name_or_email_166ae')}
                className="w-80"
                value={search}
                onChange={(event) => setSearch(event.target.value)}
              />
              {addButton}
            </div>
            {table}
          </>
        ) : (
          <ResourceSection title={t('project_managers_90131')} padded={false}>
            {table}
            <div className="p-4">{addButton}</div>
          </ResourceSection>
        )}
      </div>
      <Dialog
        open={adding}
        onOpenChange={setAdding}
        busy={update.isPending}
        title={kind === 'teams' ? t('add_team_members_34458') : t('add_project_managers_d96a5')}
        description={
          kind === 'teams'
            ? t('responsibilities_can_be_changed_after_adding_a_member_56d85')
            : t('managers_can_edit_project_information_and_manage_other_df5f9')
        }
      >
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            if (update.isPending || !selected.length) return
            update.mutate([
              ...inputPeople(),
              ...selected
                .filter((id) => !people.some((person) => person.user_id === id))
                .map((user_id) => ({
                  user_id,
                  ...(kind === 'teams' ? { role: 'member', status: 'active' } : {}),
                })),
            ])
          }}
        >
          <CandidatePicker
            path={
              kind === 'teams'
                ? '/admin/team-member-candidates'
                : `/projects/${resource.id}/manager-candidates`
            }
            exclude={people.map((person) => person.user_id)}
            selected={selected}
            onChange={setSelected}
            label={kind === 'teams' ? t('members_c1ee9') : t('managers_7c2c6')}
            disabled={update.isPending}
          />
          <ErrorNotice error={update.error} />
          <SaveButton pending={update.isPending} disabled={!selected.length}>
            {t('add_94191')}
          </SaveButton>
        </form>
      </Dialog>
      <Dialog
        open={!!change}
        onOpenChange={(open) => {
          if (!open) setChange(null)
        }}
        busy={update.isPending}
        title={t('confirm_membership_change_6691d')}
        description={t('the_server_checks_ownership_continuity_removing_yourself_as_d4c09')}
      >
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            applyChange()
          }}
        >
          <p>
            {change?.person.name} ·{' '}
            {change?.action === 'remove'
              ? t('remove_2f752')
              : change?.action === 'role'
                ? t('change_responsibility_f601b')
                : t('change_member_status_63649')}
          </p>
          <ErrorNotice error={update.error} />
          <SaveButton pending={update.isPending}>{t('confirm_change_3ced6')}</SaveButton>
        </form>
      </Dialog>
    </>
  )
}
