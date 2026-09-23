import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from 'react-router'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, KeyRound, ShieldCheck } from 'lucide-react'
import { getMember } from '@/api/governance'
import { getOffboarding } from '@/api/offboarding'
import { usePermissions } from '@/hooks/use-permissions'
import { useSession } from '@/hooks/use-auth'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import type { OffboardingCase, OffboardingResource } from '@/types/offboarding'
import ActionDialog from './action-dialog'

export default function OffboardingPage() {
  const { memberId } = useParams()
  return (
    <PermissionGate permission="members.read">
      <Offboarding key={memberId} memberId={memberId!} />
    </PermissionGate>
  )
}
function Offboarding({ memberId }: { memberId: string }) {
  const { t, i18n } = useTranslation('offboarding')
  const access = usePermissions()
  const session = useSession()
  const cache = useQueryClient()
  const [dialog, setDialog] = useState<'plan' | 'emergency' | 'complete' | null>(null)
  const [selected, setSelected] = useState<OffboardingCase>()
  const [notice, setNotice] = useState<string | null>(null)
  const inventory = useQuery({
    queryKey: ['offboarding', memberId],
    queryFn: ({ signal }) => getOffboarding(memberId, signal),
    refetchOnWindowFocus: false,
    retry: false,
  })
  const member = useQuery({
    queryKey: ['admin', 'member', memberId],
    queryFn: ({ signal }) => getMember(memberId, signal),
    retry: false,
  })
  const data = inventory.data
  const blocked = !access.can('members.write')
    ? 'readOnly'
    : memberId === session.data?.user.id
      ? 'self'
      : data?.last_administrator
        ? 'protected'
        : member.data?.role === 'admin' && !access.isAdmin
          ? 'adminTarget'
          : data?.disabled
            ? 'inactive'
            : null
  const refresh = () => {
    setDialog(null)
    setSelected(undefined)
    setNotice(null)
    void inventory.refetch()
    void member.refetch()
  }
  function success(record: OffboardingCase) {
    setDialog(null)
    setSelected(undefined)
    setNotice(record.status === 'completed' ? 'completed' : 'planSaved')
    void cache.invalidateQueries({ queryKey: ['offboarding', memberId] })
    void cache.invalidateQueries({ queryKey: ['admin', 'member', memberId] })
    void cache.invalidateQueries({ queryKey: ['admin', 'members'] })
  }
  const date = (value: string) =>
    new Intl.DateTimeFormat(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US', {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date(value))
  return (
    <Page title={t('title')} description={t('description')}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Link
          to={`/admin/members/${memberId}`}
          className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="size-4" />
          {t('back')}
        </Link>
        <Button
          variant="outline"
          disabled={inventory.isFetching || member.isFetching}
          onClick={refresh}
        >
          {t('refresh')}
        </Button>
      </div>
      <QueryState
        pending={inventory.isPending || member.isPending}
        error={inventory.error || member.error}
        retry={refresh}
      />
      {data && member.data && !inventory.isError && !member.isError && (
        <>
          <div>
            <h2 className="text-xl font-semibold">{member.data.name}</h2>
            <p className="mt-1 text-sm text-muted-foreground">{t('description')}</p>
          </div>
          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2 rounded-lg border border-destructive/25 bg-destructive/5 p-5">
              <KeyRound className="size-5 text-destructive" aria-hidden="true" />
              <h3 className="font-semibold">{t('personal')}</h3>
              <p className="text-sm leading-6 text-muted-foreground">{t('personalDescription')}</p>
            </div>
            <div className="space-y-2 rounded-lg border p-5">
              <ShieldCheck className="size-5" aria-hidden="true" />
              <h3 className="font-semibold">{t('preserved')}</h3>
              <p className="text-sm leading-6 text-muted-foreground">{t('preservedDescription')}</p>
            </div>
          </div>
          {notice && (
            <p role="status" className="rounded-lg border bg-muted p-4 text-sm">
              {t(notice)}
            </p>
          )}
          {blocked && (
            <p role="status" className="text-sm text-muted-foreground">
              {t(blocked)}
            </p>
          )}
          {!blocked && (
            <div className="flex flex-wrap gap-3">
              <Button onClick={() => setDialog('plan')}>{t('planned')}</Button>
              {access.isAdmin && (
                <Button
                  className="bg-destructive text-white hover:bg-destructive/90"
                  onClick={() => setDialog('emergency')}
                >
                  {t('emergency')}
                </Button>
              )}
            </div>
          )}
          <section className="space-y-3">
            <h3 className="font-semibold">
              {t('keys')} <span className="text-muted-foreground">{data.personal_keys.length}</span>
            </h3>
            {data.personal_keys.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t('none')}</p>
            ) : (
              <ul className="divide-y rounded-lg border">
                {data.personal_keys.map((key) => (
                  <li key={key.id} className="flex flex-wrap justify-between gap-2 p-3 text-sm">
                    <span>{key.name}</span>
                    <code aria-label={t('prefix')}>{key.prefix}…</code>
                  </li>
                ))}
              </ul>
            )}
          </section>
          <ResourceInventory title={t('projects')} resources={data.projects} />
          <ResourceInventory title={t('teams')} resources={data.teams} />
          <section className="space-y-3">
            <h3 className="font-semibold">{t('history')}</h3>
            {data.cases.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t('noHistory')}</p>
            ) : (
              data.cases.map((record) => (
                <article key={record.id} className="space-y-3 rounded-lg border p-4">
                  <div className="flex flex-wrap items-center gap-3">
                    <Badge>{t(`mode_${record.mode}`)}</Badge>
                    <Badge>
                      {t(record.status === 'completed' ? 'status_completed' : 'ready_to_complete')}
                    </Badge>
                    <span className="text-sm text-muted-foreground">{date(record.created_at)}</span>
                  </div>
                  <p className="text-sm">{record.reason}</p>
                  {record.planned_at && (
                    <p className="text-sm text-muted-foreground">
                      {t('intended')}: {date(record.planned_at)}
                    </p>
                  )}
                  {record.status === 'ready_to_complete' &&
                    record.inventory_version !== data.inventory_version && (
                      <p className="text-sm text-muted-foreground">{t('stalePlan')}</p>
                    )}
                  {record.status === 'ready_to_complete' && !blocked && (
                    <Button
                      variant="outline"
                      disabled={record.inventory_version !== data.inventory_version}
                      onClick={() => {
                        setSelected(record)
                        setDialog('complete')
                      }}
                    >
                      {t('complete')}
                    </Button>
                  )}
                  {record.completed_at && (
                    <p className="text-sm text-muted-foreground">
                      {t('outcome')}: {date(record.completed_at)}
                    </p>
                  )}
                </article>
              ))
            )}
          </section>
          {dialog && (
            <ActionDialog
              key={`${dialog}:${data.inventory_version}`}
              mode={dialog}
              inventory={data}
              selectedCase={selected}
              onClose={() => {
                setDialog(null)
                void inventory.refetch()
                void member.refetch()
              }}
              onReview={refresh}
              onSuccess={success}
            />
          )}
        </>
      )}
    </Page>
  )
}
function ResourceInventory({
  title,
  resources,
}: {
  title: string
  resources: OffboardingResource[]
}) {
  const { t } = useTranslation('offboarding')
  return (
    <section className="space-y-3">
      <h3 className="font-semibold">
        {title} <span className="text-muted-foreground">{resources.length}</span>
      </h3>
      {resources.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('none')}</p>
      ) : (
        <ul className="divide-y rounded-lg border">
          {resources.map((resource) => (
            <li key={resource.id} className="flex flex-wrap justify-between gap-3 p-4">
              <div>
                <p className="text-sm font-medium">{resource.name}</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {resource.people.map((p) => p.name).join(', ')}
                </p>
              </div>
              <Badge
                variant="outline"
                className={resource.requires_successor ? 'text-destructive' : ''}
              >
                {t(
                  resource.status === 'archived'
                    ? 'archived'
                    : resource.requires_successor
                      ? 'required'
                      : 'continuity',
                )}
              </Badge>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
