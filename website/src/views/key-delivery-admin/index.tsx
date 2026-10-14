import { useState } from 'react'
import ControllerWorkspace from '@/views/key-delivery-controllers'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getKeyCallerTemplate, getKeyDeliveryPolicy } from '@/api/key-delivery'
import { validApprovalReason } from '@/api/registration-approval'
import type { KeyCallerTemplateInput } from '@/types/key-delivery'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { FormField } from '@/components/app/CatalogUI'
import { IdentityForm, PolicyForm, TemplateForm, type VaultDeliveryChoice } from './forms'
import { SecurityTuple, Targets } from './targets'
import { deliveryQueryPrefix, useDeliveryLifetime, type DeliveryAuthority } from './lifetime'
export interface KeyDeliveryAdminWorkspaceProps extends DeliveryAuthority {
  vaultChoices: readonly VaultDeliveryChoice[]
  choicesCurrent: boolean
  choicesGeneration: number
}
// The parent supplies its fresh, independent read context and safe Vault options.
// Keying the actor destroys every transient intent on an actual identity change.
export default function KeyDeliveryAdminWorkspace(props: KeyDeliveryAdminWorkspaceProps) {
  return <Workspace key={props.actor} {...props} />
}
export { KeyDeliveryAdminWorkspace }
function Workspace(props: KeyDeliveryAdminWorkspaceProps) {
  const { t } = useTranslation('keyDelivery')
  const life = useDeliveryLifetime(props, {
    current: props.choicesCurrent,
    generation: props.choicesGeneration,
    choices: props.vaultChoices,
  })
  const generation = `${props.generation}:${life.revision}`
  const canRead = life.current('secrets.read')
  const policy = useQuery({
    queryKey: [...deliveryQueryPrefix(props.actor), generation, 'policy'],
    queryFn: ({ signal }) => getKeyDeliveryPolicy(signal),
    enabled: canRead,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const template = useQuery({
    queryKey: [...deliveryQueryPrefix(props.actor), generation, 'template'],
    queryFn: ({ signal }) => getKeyCallerTemplate(signal),
    enabled: canRead,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const [reason, setReason] = useState('')
  const [controllerTarget, setControllerTarget] = useState('')
  const ready =
    canRead &&
    !policy.isFetching &&
    !policy.isError &&
    !!policy.data &&
    !template.isFetching &&
    !template.isError &&
    !!template.data
  const options = {
    choices: props.vaultChoices,
    current: props.choicesCurrent,
    generation: props.choicesGeneration,
  }
  return (
    <section className="space-y-6" aria-label={t('title')}>
      <header className="space-y-3">
        <div>
          <h2 className="text-lg font-semibold">{t('title')}</h2>
          <p className="text-sm text-muted-foreground">{t('scope')}</p>
        </div>
        <Button
          variant="outline"
          disabled={!canRead || policy.isFetching || template.isFetching || life.pending}
          onClick={() => {
            void policy.refetch()
            void template.refetch()
          }}
        >
          {t('refresh')}
        </Button>
      </header>
      {life.notice && (
        <p role={life.notice === 'saved' ? 'status' : 'alert'}>{t(`notice.${life.notice}`)}</p>
      )}
      {ready &&
        Object.entries(life.receipts).map(([purpose, receipt]) => (
          <p role="status" key={purpose}>
            {t('identityReceipt', {
              id: receipt.id,
              purpose: t(purpose),
              integration: receipt.integration,
            })}
          </p>
        ))}
      {life.intent && (!ready || !life.selectionCurrent(life.intent)) && (
        <Button variant="outline" disabled={life.pending} onClick={life.discard}>
          {t('abandon')}
        </Button>
      )}
      {!ready ? (
        <div className="space-y-3">
          <p role={policy.isError || template.isError ? 'alert' : 'status'}>
            {t(
              !canRead
                ? 'authorityRequired'
                : policy.isError || template.isError
                  ? 'readFailed'
                  : 'loading',
            )}
          </p>
          <Button
            variant="outline"
            disabled={!canRead || policy.isFetching || template.isFetching || life.pending}
            onClick={() => {
              void policy.refetch()
              void template.refetch()
            }}
          >
            {t('refresh')}
          </Button>
        </div>
      ) : (
        <div className="space-y-6" key={generation}>
          <Card>
            <CardHeader>
              <CardTitle>{t('overview')}</CardTitle>
              <CardDescription>{t('overviewHelp')}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <dl className="grid gap-4 text-sm md:grid-cols-3">
                <div>
                  <dt>{t('availability')}</dt>
                  <dd>{t(policy.data!.managed_available ? 'available' : 'unavailable')}</dd>
                </div>
                <div>
                  <dt>{t('writerID')}</dt>
                  <dd className="break-all font-mono">
                    {policy.data!.writer_identity_id || t('none')}
                  </dd>
                </div>
                <div>
                  <dt>{t('provisionerID')}</dt>
                  <dd className="break-all font-mono">
                    {policy.data!.provisioner_identity_id || t('none')}
                  </dd>
                </div>
                <div>
                  <dt>{t('template')}</dt>
                  <dd>{t(policy.data!.template_configured ? 'configured' : 'notConfigured')}</dd>
                </div>
                <div>
                  <dt>{t('coordinator')}</dt>
                  <dd>{t('notConfigured')}</dd>
                </div>
                <div>
                  <dt>{t('applicationDelivery')}</dt>
                  <dd>{t('unknown')}</dd>
                </div>
              </dl>
              <FormField label={t('verifyReason')}>
                <Textarea
                  value={reason}
                  disabled={life.locked}
                  onChange={(event) => setReason(event.target.value)}
                />
              </FormField>
              <Button
                disabled={
                  life.locked ||
                  !life.current('secrets.write', true) ||
                  !validApprovalReason(reason)
                }
                onClick={() =>
                  life.prepare({
                    kind: 'verify',
                    etag: policy.data!.review_etag,
                    input: { reason: reason.trim() },
                  })
                }
              >
                {t('verify')}
              </Button>
            </CardContent>
          </Card>
          <Targets
            actor={props.actor}
            generation={generation}
            life={life}
            onReviewControllers={setControllerTarget}
          />
          <PolicyForm policy={policy.data!} options={options} life={life} />
          <div className="grid gap-4 md:grid-cols-2">
            <IdentityForm
              key={`writer:${props.choicesCurrent}:${props.choicesGeneration}`}
              purpose="writer"
              policy={policy.data!}
              options={options}
              life={life}
            />
            <IdentityForm
              key={`provisioner:${props.choicesCurrent}:${props.choicesGeneration}`}
              purpose="provisioner"
              policy={policy.data!}
              options={options}
              life={life}
            />
          </div>
          <TemplateForm template={template.data!} life={life} />
        </div>
      )}
      <ControllerWorkspace
        authority={props}
        target={controllerTarget}
        onClose={() => setControllerTarget('')}
      />
      <Dialog
        open={!!life.intent && ready && life.selectionCurrent(life.intent)}
        title={t(life.uncertain ? 'uncertainTitle' : 'confirmTitle')}
        description={t('confirmHelp')}
        onOpenChange={(open) => {
          if (!open) life.discard()
        }}
        busy={life.pending}
        width={768}
      >
        {life.intent && (
          <div className="max-h-[70vh] space-y-4 overflow-y-auto">
            <p>{t(`operation.${life.intent.kind}`)}</p>
            <dl className="space-y-2 text-sm">
              <dt>{t('reviewToken')}</dt>
              <dd className="break-all font-mono">{life.intent.etag}</dd>
              <dt>{t('reason')}</dt>
              <dd className="break-all">{life.intent.input.reason}</dd>
            </dl>
            {'tuple' in life.intent && <SecurityTuple tuple={life.intent.tuple} />}
            {life.intent.option && (
              <dl className="space-y-2 break-all text-sm">
                <dt>{t('endpoint')}</dt>
                <dd>{life.intent.option.endpoint}</dd>
                <dt>{t('namespace')}</dt>
                <dd>{life.intent.option.namespace || t('none')}</dd>
                <dt>{t('mount')}</dt>
                <dd>{life.intent.option.mount}</dd>
              </dl>
            )}
            {life.intent.kind === 'policy' && (
              <p>
                {t('policyConfirmation', {
                  allowed: life.intent.input.allowed_modes
                    .map((mode) => t(`mode.${mode}`))
                    .join(', '),
                  mode: t(`mode.${life.intent.input.default_mode}`),
                  integration: life.intent.input.integration_id || t('none'),
                  writer: life.intent.input.writer_identity_id || t('none'),
                  provisioner: life.intent.input.provisioner_identity_id || t('none'),
                })}
              </p>
            )}
            {life.intent.kind === 'identity' && (
              <p>
                {t('identityConfirmation', {
                  purpose: t(life.intent.input.purpose),
                  integration: life.intent.input.integration_id,
                })}
              </p>
            )}
            {life.intent.kind === 'template' && <TemplateReview input={life.intent.input} />}
            {life.intent.kind === 'approval' && (
              <p>{t(life.intent.input.approved ? 'approveControl' : 'revokeControl')}</p>
            )}
            {life.intent.kind === 'isolation' && (
              <p>{t(life.intent.input.isolated ? 'attestIsolation' : 'revokeIsolation')}</p>
            )}
            <div className="flex gap-2">
              <Button variant="outline" disabled={life.pending} onClick={life.discard}>
                {t(life.uncertain ? 'abandon' : 'cancel')}
              </Button>
              <Button
                disabled={
                  life.pending ||
                  !life.current(
                    'secrets.write',
                    life.intent.kind === 'verify' || life.intent.kind === 'reconcile',
                  ) ||
                  !life.selectionCurrent(life.intent)
                }
                onClick={() => void life.dispatch()}
              >
                {t(life.pending ? 'pending' : life.uncertain ? 'retryOriginal' : 'confirm')}
              </Button>
            </div>
          </div>
        )}
      </Dialog>
    </section>
  )
}

function TemplateReview({ input }: { input: KeyCallerTemplateInput }) {
  const { t } = useTranslation('keyDelivery')
  return (
    <dl className="space-y-2 text-sm">
      {(
        [
          'auth_mount',
          'issuer',
          'jwks_url',
          'audience',
          'allowed_subjects',
          'token_ttl_seconds',
          'token_max_ttl_seconds',
        ] as const
      ).map((key) => (
        <div key={key}>
          <dt>{t(`templateField.${key}`)}</dt>
          <dd className="break-all whitespace-pre-wrap">
            {Array.isArray(input[key]) ? input.allowed_subjects.join('\n') : String(input[key])}
          </dd>
        </div>
      ))}
    </dl>
  )
}
