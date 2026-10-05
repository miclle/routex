import { useTranslation } from 'react-i18next'
import { QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { RegistrationApprovalStatus } from './member-approval'
import { Tooltip } from '@/components/ui/tooltip'
import type { MemberDetail } from '@/types/member-recent-login'
import type { MemberAccessRead } from './member-access-summary-read'

export function MemberAccessRoleSummary({
  read,
  label = true,
}: {
  read: MemberAccessRead
  label?: boolean
}) {
  const { t } = useTranslation('governance')
  const data = read.data
  const identity = data ? t(data.identity_role === 'admin' ? 'common.admin' : 'common.member') : ''
  const roles = !data
    ? t('memberAccess.unknown')
    : data.roles.status === 'available'
      ? data.roles.items.length
        ? t('memberAccess.rolesValue', {
            identity,
            roles: data.roles.items.map((role) => role.name).join(t('common.listSeparator')),
          })
        : identity
      : t('memberAccess.rolesValue', { identity, roles: t(`memberAccess.${data.roles.status}`) })
  return <>{label ? t('members.roleSummary', { roles }) : roles}</>
}
export default function MemberAccessSummary({
  read,
  member,
  canApprove = false,
  onApproval,
}: {
  read: MemberAccessRead
  member: MemberDetail
  canApprove?: boolean
  onApproval?: (trigger: HTMLButtonElement) => void
}) {
  const { t, i18n } = useTranslation('governance')
  const data = read.data
  const date = (value: string) =>
    new Intl.DateTimeFormat(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US', {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date(value))
  return (
    <section className="rounded-lg border" aria-label={t('members.accessStatus')}>
      <h3 className="border-b p-4 font-medium">{t('members.accessStatus')}</h3>
      <QueryState pending={read.pending} error={read.error} retry={read.retry} />
      {data && (
        <dl className="grid gap-4 p-4 text-sm sm:grid-cols-3">
          <div>
            <dt className="text-muted-foreground">{t('memberAccess.roles')}</dt>
            <dd className="min-w-0 truncate">
              <MemberAccessRoleSummary read={read} label={false} />
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('memberAccess.teams')}</dt>
            <dd className="flex items-center gap-2">
              <span className="min-w-0 truncate">
                {data.teams.status !== 'available'
                  ? t(`memberAccess.${data.teams.status}`)
                  : data.teams.items.length
                    ? data.teams.items.map((team) => team.name).join(t('common.listSeparator'))
                    : t('memberAccess.noTeams')}
              </span>
              {data.teams.status === 'available' && data.teams.items.length > 0 && (
                <Tooltip
                  label={t('memberAccess.teamDetails')}
                  enabled={!!data}
                  canOpen={read.current}
                >
                  <ul className="max-h-64 space-y-2 overflow-auto">
                    {data.teams.items.map((team) => (
                      <li key={team.id} className="break-words">
                        <span className="font-medium">{team.name}</span>
                        <p>
                          {t('memberAccess.teamContext', {
                            status: t(
                              team.status === 'archived'
                                ? 'memberAccess.archived'
                                : `common.${team.status}`,
                            ),
                            membership: t(`common.${team.membership_status}`),
                            role: t(`memberAccess.${team.membership_role}`),
                          })}
                        </p>
                      </li>
                    ))}
                  </ul>
                </Tooltip>
              )}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('memberList.login')}</dt>
            <dd>
              {member.last_login_status === 'recorded'
                ? date(member.last_login_at)
                : t('memberList.loginUnknown')}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('memberList.created')}</dt>
            <dd>{date(member.created_at)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('memberList.updated')}</dt>
            <dd>{data.updated_at === null ? t('memberAccess.unknown') : date(data.updated_at)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('common.status')}</dt>
            <dd>
              {member.offboarded_at
                ? t('common.offboarded')
                : member.disabled
                  ? t('common.disabled')
                  : t('common.active')}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('registrationApproval.status')}</dt>
            <dd>
              <RegistrationApprovalStatus summary={member.registration_approval} />
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('registrationApproval.admission')}</dt>
            <dd>
              {t(
                member.registration_approval.admission_eligible
                  ? 'registrationApproval.eligible'
                  : 'registrationApproval.ineligible',
              )}
            </dd>
          </div>
          {canApprove && onApproval && (
            <div>
              <Button
                variant="outline"
                onClick={(event) => {
                  if (read.current()) onApproval(event.currentTarget)
                }}
              >
                {t('registrationApproval.review')}
              </Button>
            </div>
          )}
        </dl>
      )}
    </section>
  )
}
