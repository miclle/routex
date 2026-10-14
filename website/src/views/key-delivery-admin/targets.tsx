import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getKeyDeliveryTargets, reviewKeyDeliveryTarget } from '@/api/key-delivery'
import { validApprovalReason } from '@/api/registration-approval'
import type { KeyDeliverySecurityTuple } from '@/types/key-delivery'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Table } from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { FormField } from '@/components/app/CatalogUI'
import { deliveryQueryPrefix, type DeliveryLifetime } from './lifetime'
export function SecurityTuple({ tuple }: { tuple: KeyDeliverySecurityTuple }) {
  const { t } = useTranslation('keyDelivery')
  return (
    <dl className="grid gap-2 text-sm sm:grid-cols-[minmax(10rem,1fr)_2fr]">
      {Object.entries(tuple).map(([key, value]) => (
        <div className="contents" key={key}>
          <dt className="text-muted-foreground">{t(`tuple.${key}`)}</dt>
          <dd className="break-all font-mono">{value}</dd>
        </div>
      ))}
    </dl>
  )
}
export function Targets({
  actor,
  generation,
  life,
  onReviewControllers,
}: {
  actor: string
  generation: string
  life: DeliveryLifetime
  onReviewControllers: (target: string) => void
}) {
  const { t } = useTranslation('keyDelivery')
  const [cursor, setCursor] = useState(''),
    [selected, setSelected] = useState(''),
    [reason, setReason] = useState('')
  const page = useQuery({
    queryKey: [...deliveryQueryPrefix(actor), generation, 'targets', cursor],
    queryFn: ({ signal }) => getKeyDeliveryTargets(cursor, signal),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const review = useQuery({
    queryKey: [...deliveryQueryPrefix(actor), generation, 'target', selected],
    queryFn: ({ signal }) => reviewKeyDeliveryTarget(selected, signal),
    enabled: !!selected && life.current('secrets.read'),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const facts = !review.isFetching && !review.isError ? review.data : undefined
  const canWrite =
    !!facts && life.current('secrets.write') && !life.locked && validApprovalReason(reason)
  const prepare = (action: 'approval' | 'isolation' | 'reconcile', value = true) => {
    if (!facts || !canWrite || (action === 'reconcile' && !life.current('secrets.write', true)))
      return
    const common = { target: facts.id, tuple: facts.security_tuple, etag: facts.review_etag }
    if (action === 'approval')
      life.prepare({
        ...common,
        kind: action,
        input: { approved: value, control_verified: value, reason: reason.trim() },
      })
    else if (action === 'isolation')
      life.prepare({ ...common, kind: action, input: { isolated: value, reason: reason.trim() } })
    else
      life.prepare({
        ...common,
        kind: action,
        input: { request_id: crypto.randomUUID(), reason: reason.trim() },
      })
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('profiles')}</CardTitle>
        <CardDescription>{t('profilesHelp')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {page.isFetching ? (
          <p role="status">{t('loading')}</p>
        ) : page.isError ? (
          <p role="alert">{t('readFailed')}</p>
        ) : (
          <Table aria-label={t('profiles')}>
            <thead>
              <tr>
                {[
                  'application',
                  'owner',
                  'method',
                  'control',
                  'isolation',
                  'status',
                  'actions',
                ].map((key) => (
                  <th key={key}>{t(key)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {page.data?.items.map((item) => (
                <tr key={item.id}>
                  <td>
                    <p>{item.application_name}</p>
                    <p className="text-muted-foreground">{item.environment}</p>
                  </td>
                  <td>
                    <p>{t(`ownerKind.${item.owner_kind}`)}</p>
                    <p className="break-all font-mono text-xs">{item.owner_id}</p>
                  </td>
                  <td>JWT</td>
                  <td>{t(item.control_verified ? 'approved' : 'notApproved')}</td>
                  <td>{t(`isolationState.${item.isolation.status}`)}</td>
                  <td>{t(`targetState.${item.status}`)}</td>
                  <td>
                    <Button
                      variant="outline"
                      disabled={life.locked}
                      onClick={() => {
                        setSelected(item.id)
                        setReason('')
                      }}
                    >
                      {t('reviewTarget')}
                    </Button>
                  </td>
                </tr>
              ))}
              {!page.data?.items.length && (
                <tr>
                  <td colSpan={7}>{t('noProfiles')}</td>
                </tr>
              )}
            </tbody>
          </Table>
        )}
        <div className="flex gap-2">
          <Button
            variant="outline"
            disabled={life.locked || page.isFetching}
            onClick={() => void page.refetch()}
          >
            {t('refresh')}
          </Button>
          <Button
            variant="outline"
            disabled={life.locked || page.isFetching || !cursor}
            onClick={() => setCursor('')}
          >
            {t('firstPage')}
          </Button>
          <Button
            variant="outline"
            disabled={life.locked || page.isFetching || !page.data?.next_cursor}
            onClick={() => {
              if (page.data?.next_cursor) setCursor(page.data.next_cursor)
            }}
          >
            {t('nextPage')}
          </Button>
        </div>
        <Dialog
          open={!!selected}
          onOpenChange={(open) => {
            if (!open) {
              setSelected('')
              setReason('')
            }
          }}
          title={t('reviewTarget')}
          description={t('targetReviewHelp')}
          busy={life.pending}
          width={768}
        >
          {!facts ? (
            <div className="space-y-3">
              <p role={review.isError ? 'alert' : 'status'}>
                {t(review.isError ? 'readFailed' : 'loading')}
              </p>
              <Button
                variant="outline"
                disabled={review.isFetching}
                onClick={() => void review.refetch()}
              >
                {t('refresh')}
              </Button>
            </div>
          ) : (
            <div className="max-h-[70vh] space-y-4 overflow-y-auto">
              <SecurityTuple tuple={facts.security_tuple} />
              <Button
                variant="outline"
                disabled={life.locked || !life.current('secrets.read')}
                onClick={() => onReviewControllers(facts.id)}
              >
                {t('reviewControllers')}
              </Button>
              <FormField label={t('reason')}>
                <Textarea
                  value={reason}
                  disabled={life.locked}
                  onChange={(event) => setReason(event.target.value)}
                />
              </FormField>
              <p className="text-sm text-muted-foreground">{t('controlHelp')}</p>
              <div className="flex flex-wrap gap-2">
                <Button disabled={!canWrite} onClick={() => prepare('approval')}>
                  {t('approveControl')}
                </Button>
                <Button
                  variant="outline"
                  disabled={!canWrite}
                  onClick={() => prepare('approval', false)}
                >
                  {t('revokeControl')}
                </Button>
              </div>
              <p className="text-sm text-muted-foreground">{t('isolationHelp')}</p>
              <div className="flex flex-wrap gap-2">
                <Button disabled={!canWrite} onClick={() => prepare('isolation')}>
                  {t('attestIsolation')}
                </Button>
                <Button
                  variant="outline"
                  disabled={!canWrite}
                  onClick={() => prepare('isolation', false)}
                >
                  {t('revokeIsolation')}
                </Button>
              </div>
              <p className="text-sm text-muted-foreground">{t('reconcileHelp')}</p>
              <Button
                disabled={!canWrite || !life.current('secrets.write', true)}
                onClick={() => prepare('reconcile')}
              >
                {t('reconcile')}
              </Button>
            </div>
          )}
        </Dialog>
      </CardContent>
    </Card>
  )
}
