import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router'
import { Ellipsis, UserRound, KeyRound, ArrowRightLeft, Gauge, Files, Ban } from 'lucide-react'
import { Table } from '@/components/ui/table'
import { Menu, MenuItem } from '@/components/ui/menu'
import { Tooltip } from '@/components/ui/tooltip'
import { RegistrationApprovalStatus } from './member-approval'
import { Badge } from '@/components/ui/badge'
import { exactDecimal, exactInteger } from '@/views/home/monthly-account-values'
import type { MemberListItem } from '@/types/member-list'

type Props = {
  rows: MemberListItem[]
  permissions: { teams: boolean; calls: boolean; limitsWrite: boolean }
  canAct: (id: string, permission?: string) => boolean
  canApprove?: boolean
  onApproval?: (row: MemberListItem, trigger: HTMLButtonElement | null) => void
  canChange: (row: MemberListItem) => boolean
  onStatus: (row: MemberListItem, trigger: HTMLButtonElement | null) => void
}
export default function MemberList({
  rows,
  permissions,
  canAct,
  canChange,
  onStatus,
  canApprove = false,
  onApproval,
}: Props) {
  const { t, i18n } = useTranslation('governance')
  const navigate = useNavigate()
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const date = (v: string, timeZone?: string) =>
    new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short', timeZone }).format(
      new Date(v),
    )
  const money = (v: Record<string, string>) =>
    Object.entries(v)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([code, amount]) => `${exactDecimal(amount, locale)} ${code}`)
      .join('; ') || t('memberOverview.noAmounts')
  const navigateRow = (row: MemberListItem, path: string, permission = 'members.read') => {
    if (canAct(row.id, permission)) void navigate(path)
  }
  const context = (row: MemberListItem, kind: 'tokens' | 'money') => {
    const p = row.personal,
      u = p.usage,
      h = p.active_reservations
    return (
      <div className="space-y-1">
        <p>{t(row.personal_policy_stored ? 'memberList.storedPolicy' : 'memberList.noPolicy')}</p>
        <p>{t(p.runtime_applied ? 'memberOverview.applied' : 'memberOverview.notApplied')}</p>
        <p>
          {u
            ? t(u.covered ? 'memberOverview.covered' : 'memberOverview.uncovered')
            : t(
                p.usage_status === 'inactive'
                  ? 'memberOverview.inactive'
                  : 'memberOverview.unavailable',
              )}
        </p>
        {u && (
          <>
            <p>
              {t('memberOverview.window', {
                from: date(u.month_start, u.time_zone),
                to: date(u.month_end, u.time_zone),
                zone: u.time_zone,
              })}
            </p>
            <p>{t('memberOverview.asOf', { date: date(u.as_of) })}</p>
            {kind === 'tokens' ? (
              <>
                <p>
                  {t('memberOverview.retainedTokens', {
                    amount: exactInteger(u.tokens_held, locale),
                  })}
                </p>
                <p>
                  {t('memberOverview.liveTokens', {
                    amount: h ? exactInteger(h.tokens_held, locale) : t('memberOverview.unknown'),
                  })}
                </p>
                <p>
                  {t('memberOverview.unknownTokens', {
                    count: exactInteger(u.tokens_unknown, locale),
                  })}
                </p>
              </>
            ) : (
              <>
                <p>{t('memberList.allAmounts', { amount: money(u.money_used) })}</p>
                <p>{t('memberOverview.retainedMoney', { amount: money(u.money_held) })}</p>
                <p>
                  {t('memberOverview.liveMoney', {
                    amount: h ? money(h.money_held) : t('memberOverview.unknown'),
                  })}
                </p>
                <p>
                  {t('memberOverview.unknownMoney', {
                    count: exactInteger(u.money_unknown, locale),
                  })}
                </p>
              </>
            )}
          </>
        )}
      </div>
    )
  }
  return (
    <Table aria-label={t('members.listLabel')} className="min-w-[1280px] [&_td]:whitespace-nowrap">
      <thead>
        <tr>
          {[
            'name',
            'email',
            'status',
            'teams',
            'tokens',
            'budget',
            'keys',
            'login',
            'created',
            'updated',
            'actions',
          ].map((k) => (
            <th
              key={k}
              className={k === 'actions' ? 'sticky right-0 z-10 bg-muted text-center' : undefined}
            >
              {t(`memberList.${k}`)}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => {
          let menuTrigger: HTMLButtonElement | null = null
          const base = `/admin/members/${encodeURIComponent(row.id)}`,
            p = row.personal,
            u = p.usage,
            displayName = row.name.trim() ? row.name : row.email || row.id
          const tokenUsed = u ? exactInteger(u.tokens_used, locale) : t('memberOverview.unknown')
          const tokenLimit =
            p.tokens_month === null
              ? t('memberOverview.notSet')
              : exactInteger(p.tokens_month, locale)
          const moneyCode = p.currency
          const moneyUsed = u
            ? moneyCode
              ? `${exactDecimal(u.money_used[moneyCode] ?? '0', locale)} ${moneyCode}`
              : money(u.money_used)
            : t('memberOverview.unknown')
          const moneyLimit =
            p.money_month === null
              ? t('memberOverview.notSet')
              : `${exactDecimal(p.money_month, locale)} ${moneyCode}`
          const teamItems =
            permissions.teams && row.teams.status === 'available' ? row.teams.items : null
          return (
            <tr key={row.id}>
              <td>
                <Link
                  className="inline-flex items-center gap-2"
                  to={base}
                  onClick={(e) => {
                    if (!canAct(row.id)) e.preventDefault()
                  }}
                >
                  <span
                    aria-hidden
                    className="flex size-7 shrink-0 items-center justify-center rounded-full bg-muted text-xs"
                  >
                    {[...displayName][0]}
                  </span>
                  {displayName}
                </Link>
              </td>
              <td>{row.email}</td>
              <td>
                <Badge variant="outline">
                  {t(
                    row.offboarded_at
                      ? 'common.offboarded'
                      : row.disabled
                        ? 'common.disabled'
                        : 'common.active',
                  )}
                </Badge>
                {row.handover_plan_recorded && (
                  <div className="mt-1">
                    <Badge variant="outline">{t('memberList.handoverRecorded')}</Badge>
                  </div>
                )}
                <div className="mt-1">
                  <RegistrationApprovalStatus summary={row.registration_approval} />
                </div>
              </td>
              <td>
                <div className="flex max-w-44 items-center gap-1">
                  <span className="truncate">
                    {teamItems
                      ? teamItems.map((team) => team.name).join(', ') || t('memberList.noTeams')
                      : t(
                          !permissions.teams || row.teams.status === 'not_authorized'
                            ? 'memberList.teamDenied'
                            : row.teams.status === 'overflow'
                              ? 'memberList.teamOverflow'
                              : 'memberList.teamUnavailable',
                        )}
                  </span>
                  {!!teamItems?.length && (
                    <Tooltip
                      label={t('memberList.teamContext', { name: displayName })}
                      canOpen={() => canAct(row.id, 'teams.read_all')}
                    >
                      <ul className="max-h-72 space-y-1 overflow-y-auto">
                        {teamItems.map((team) => (
                          <li key={team.id}>
                            <span>{team.name}</span> · {t(`memberList.team_${team.status}`)} ·{' '}
                            {t(`memberList.member_${team.membership_status}`)} ·{' '}
                            {t(`memberList.role_${team.membership_role}`)}
                          </li>
                        ))}
                      </ul>
                    </Tooltip>
                  )}
                </div>
              </td>
              <td>
                <div className="flex items-center gap-1">
                  <span className="tabular-nums">
                    {tokenUsed} / {tokenLimit}
                  </span>
                  <Tooltip
                    label={t('memberList.tokenContext', { name: displayName })}
                    canOpen={() => canAct(row.id)}
                  >
                    {context(row, 'tokens')}
                  </Tooltip>
                </div>
                {u && (!u.covered || u.tokens_unknown !== '0') && (
                  <p className="text-xs text-muted-foreground">{t('memberList.partial')}</p>
                )}
              </td>
              <td>
                <div className="flex items-center gap-1">
                  <span className="tabular-nums">
                    {moneyUsed} / {moneyLimit}
                  </span>
                  <Tooltip
                    label={t('memberList.moneyContext', { name: displayName })}
                    canOpen={() => canAct(row.id)}
                  >
                    {context(row, 'money')}
                  </Tooltip>
                </div>
                {u && (!u.covered || u.money_unknown !== '0') && (
                  <p className="text-xs text-muted-foreground">{t('memberList.partial')}</p>
                )}
              </td>
              <td className="tabular-nums">{exactInteger(row.total_personal_keys, locale)}</td>
              <td>
                {row.last_login_status === 'recorded'
                  ? date(row.last_login_at)
                  : t('memberList.loginUnknown')}
              </td>
              <td>{date(row.created_at)}</td>
              <td>{date(row.updated_at)}</td>
              <td className="sticky right-0 bg-background">
                <Menu
                  triggerRef={(node) => {
                    menuTrigger = node
                  }}
                  trigger={<Ellipsis aria-hidden className="size-4" />}
                  label={t('memberList.menu', { name: displayName })}
                  side="bottom"
                  align="end"
                  triggerClassName="mx-auto size-8 justify-center p-0"
                >
                  <MenuItem onClick={() => navigateRow(row, base)}>
                    <UserRound aria-hidden className="size-4" />
                    {t('memberList.details')}
                  </MenuItem>
                  <MenuItem onClick={() => navigateRow(row, `${base}?tab=keys`)}>
                    <KeyRound aria-hidden className="size-4" />
                    {t('memberList.manageKeys')}
                  </MenuItem>
                  {(!row.offboarded_at || row.handover_plan_recorded) && (
                    <MenuItem onClick={() => navigateRow(row, `${base}/offboarding`)}>
                      <ArrowRightLeft aria-hidden className="size-4" />
                      {t(
                        row.handover_plan_recorded
                          ? 'memberList.viewHandover'
                          : 'memberList.offboarding',
                      )}
                    </MenuItem>
                  )}
                  <MenuItem
                    onClick={() =>
                      navigateRow(
                        row,
                        `${base}?tab=limits`,
                        permissions.limitsWrite ? 'limits.users.write' : 'members.read',
                      )
                    }
                  >
                    <Gauge aria-hidden className="size-4" />
                    {t(
                      permissions.limitsWrite
                        ? 'memberList.adjustLimits'
                        : 'memberList.reviewLimits',
                    )}
                  </MenuItem>
                  {permissions.calls && (
                    <MenuItem onClick={() => navigateRow(row, '/admin/calls', 'calls.read_all')}>
                      <Files aria-hidden className="size-4" />
                      {t('memberList.calls')}
                    </MenuItem>
                  )}
                  {canApprove && onApproval && (
                    <MenuItem
                      onClick={() => {
                        if (canAct(row.id, 'members.approvals.write')) onApproval(row, menuTrigger)
                      }}
                    >
                      {t('registrationApproval.review')}
                    </MenuItem>
                  )}
                  {canChange(row) && (
                    <>
                      <div role="separator" className="my-1 border-t" />
                      <MenuItem
                        onClick={() => {
                          if (canAct(row.id, 'members.write')) onStatus(row, menuTrigger)
                        }}
                      >
                        <Ban aria-hidden className="size-4" />
                        <span className={!row.disabled ? 'text-destructive' : undefined}>
                          {t(
                            row.offboarded_at
                              ? 'memberState.reactivate'
                              : row.disabled
                                ? 'common.enable'
                                : 'common.disable',
                          )}
                        </span>
                      </MenuItem>
                    </>
                  )}
                </Menu>
              </td>
            </tr>
          )
        })}
      </tbody>
    </Table>
  )
}
