import { useEffect } from 'react'
import type { UseQueryResult } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { builtinRoleNameKey } from '@/lib/role-assignment'
import type { OverviewRolesPage, OverviewAccountsPage } from '@/types/overview'

export default function IdentityLabels({
  roles,
  accounts,
  rolePage,
  teamPage,
  identityRole,
  onMismatch,
  previousRoles,
  nextRoles,
  previousTeams,
  nextTeams,
  refresh,
}: {
  roles: UseQueryResult<OverviewRolesPage, Error>
  accounts: UseQueryResult<OverviewAccountsPage, Error>
  rolePage: number
  teamPage: number
  identityRole: 'admin' | 'member'
  onMismatch: (role: 'admin' | 'member') => void
  previousRoles: () => void
  nextRoles: () => void
  previousTeams: () => void
  nextTeams: () => void
  refresh: () => void
}) {
  const { t } = useTranslation('overview')
  const { t: governance } = useTranslation('governance')
  const roleData = roles.isSuccess && !roles.isFetching && !roles.isError ? roles.data : null
  const mismatch = roleData !== null && roleData.identity_role !== identityRole
  useEffect(() => {
    if (mismatch) onMismatch(roleData.identity_role)
  }, [mismatch, roleData, onMismatch])
  const currentRoles = mismatch ? null : roleData
  const validNames = accounts.data?.teams.every(
    (team) =>
      typeof team.name === 'string' &&
      team.name.length > 0 &&
      !/[\p{Cc}\p{Cs}]/u.test(team.name) &&
      team.name.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '') === team.name,
  )
  const teamData =
    validNames && accounts.isSuccess && !accounts.isFetching && !accounts.isError && !mismatch
      ? accounts.data
      : null
  const busy = roles.isFetching || accounts.isFetching
  return (
    <div className="space-y-1 text-sm leading-6 text-muted-foreground" data-identity-labels>
      <p>
        {t('identity.directRoles', {
          names: currentRoles
            ? currentRoles.roles.length
              ? currentRoles.roles
                  .map((role) => {
                    const key = builtinRoleNameKey(role)
                    return key ? governance(key) : (role.name ?? role.id)
                  })
                  .join(', ')
              : rolePage === 1 && !currentRoles.next_cursor
                ? t('identity.noRoles')
                : t('unknown')
            : t('unknown'),
        })}
      </p>
      {(rolePage > 1 || currentRoles?.next_cursor) && (
        <p>{t('identity.rolePage', { number: rolePage })}</p>
      )}
      {currentRoles?.next_cursor && <p>{t('identity.partialRoles')}</p>}
      <div className="flex flex-wrap gap-2">
        {rolePage > 1 && (
          <Button size="sm" variant="outline" disabled={!currentRoles} onClick={previousRoles}>
            {t('identity.previousRoles')}
          </Button>
        )}
        {currentRoles?.next_cursor && (
          <Button size="sm" variant="outline" onClick={nextRoles}>
            {t('identity.moreRoles')}
          </Button>
        )}
      </div>
      <p>
        {t('identity.teams', {
          names: teamData
            ? teamData.teams.length
              ? teamData.teams.map((team) => team.name).join(', ')
              : teamPage === 1 && !teamData.next_cursor
                ? t('identity.noTeams')
                : t('unknown')
            : t('unknown'),
        })}
      </p>
      {(teamPage > 1 || teamData?.next_cursor) && <p>{t('page', { number: teamPage })}</p>}
      {teamData?.next_cursor && <p>{t('identity.partialTeams')}</p>}
      <div className="flex flex-wrap gap-2">
        {teamPage > 1 && (
          <Button size="sm" variant="outline" disabled={!teamData} onClick={previousTeams}>
            {t('identity.previousTeams')}
          </Button>
        )}
        {teamData?.next_cursor && (
          <Button size="sm" variant="outline" onClick={nextTeams}>
            {t('identity.moreTeams')}
          </Button>
        )}
        <Button size="sm" variant="outline" disabled={busy} onClick={refresh}>
          {t('identity.refresh')}
        </Button>
      </div>
      {mismatch && <p role="status">{t('identity.roleChanged')}</p>}
    </div>
  )
}
