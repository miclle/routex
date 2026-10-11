import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { validApprovalReason } from '@/api/registration-approval'
import type { KeyCallerTemplate, KeyDeliveryMode, KeyDeliveryPolicy } from '@/types/key-delivery'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { FormField } from '@/components/app/CatalogUI'
import type { DeliveryLifetime } from './lifetime'

export interface VaultDeliveryChoice {
  id: string
  name: string
  endpoint: string
  namespace: string
  mount: string
}
export interface VaultDeliveryOptions {
  choices: readonly VaultDeliveryChoice[]
  current: boolean
  generation: number
}
const identityID = /^kda_[0-7][0-9a-hjkmnp-tv-z]{25}$/
const boundedText = (value: string, maximum: number) =>
  !!value && new TextEncoder().encode(value).length <= maximum && !/[\p{Cc}\p{Cs}]/u.test(value)
const selectClass = 'h-10 w-full rounded-md border bg-background px-3'
export function PolicyForm({
  policy,
  options,
  life,
}: {
  policy: KeyDeliveryPolicy
  options: VaultDeliveryOptions
  life: DeliveryLifetime
}) {
  const { t } = useTranslation('keyDelivery')
  const [review, setReview] = useState(policy)
  const [allowed, setAllowed] = useState<KeyDeliveryMode[]>(policy.allowed_modes)
  const [selected, setSelected] = useState(policy.default_mode)
  const [integration, setIntegration] = useState(policy.integration_id)
  const [writer, setWriter] = useState(policy.writer_identity_id)
  const [provisioner, setProvisioner] = useState(policy.provisioner_identity_id)
  const [reason, setReason] = useState('')
  const fresh = review.review_etag === policy.review_etag
  const validChoice =
    !integration || (options.current && options.choices.some((choice) => choice.id === integration))
  const validIdentities = integration
    ? identityID.test(writer) && identityID.test(provisioner) && writer !== provisioner
    : !writer && !provisioner
  const blocked =
    life.locked ||
    !life.current('secrets.write') ||
    !fresh ||
    !validChoice ||
    !validIdentities ||
    (allowed.includes('managed_vault') && !policy.managed_available)
  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault()
        if (
          blocked ||
          !allowed.length ||
          !allowed.includes(selected) ||
          !validApprovalReason(reason)
        )
          return
        life.prepare({
          kind: 'policy',
          etag: review.review_etag,
          optionGeneration: integration ? options.generation : undefined,
          input: {
            allowed_modes: [...allowed],
            default_mode: selected,
            integration_id: integration,
            writer_identity_id: writer,
            provisioner_identity_id: provisioner,
            reason: reason.trim(),
          },
        })
      }}
    >
      <div className="grid gap-4 md:grid-cols-2">
        {(['manual', 'managed_vault'] as const).map((mode) => (
          <Card key={mode}>
            <CardHeader>
              <CardTitle>{t(`mode.${mode}`)}</CardTitle>
              <CardDescription>{t(`modeHelp.${mode}`)}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <span>{t('allowed')}</span>
                <Switch
                  aria-label={t('allowMode', { mode: t(`mode.${mode}`) })}
                  checked={allowed.includes(mode)}
                  disabled={life.locked || !life.current('secrets.write')}
                  onCheckedChange={(value) => {
                    const next = value
                      ? [...allowed, mode]
                      : allowed.filter((item) => item !== mode)
                    setAllowed([...new Set(next)])
                    if (next.length && !next.includes(selected)) setSelected(next[0])
                  }}
                />
              </div>
              <Button
                variant="outline"
                disabled={life.locked || !life.current('secrets.write') || !allowed.includes(mode)}
                onClick={() => setSelected(mode)}
              >
                {selected === mode ? t('defaultMode') : t('makeDefault')}
              </Button>
              {mode === 'managed_vault' && (
                <div className="space-y-3">
                  <FormField label={t('vault')}>
                    <select
                      className={selectClass}
                      value={options.current ? integration : ''}
                      disabled={life.locked || !life.current('secrets.write') || !options.current}
                      onChange={(event) => setIntegration(event.target.value)}
                    >
                      <option value="">{t('none')}</option>
                      {options.current &&
                        integration &&
                        !options.choices.some((choice) => choice.id === integration) && (
                          <option value={integration} disabled>
                            {t('selectedVaultUnavailable', { id: integration })}
                          </option>
                        )}
                      {options.current &&
                        options.choices.map((choice) => (
                          <option key={choice.id} value={choice.id}>
                            {choice.name}
                          </option>
                        ))}
                    </select>
                  </FormField>
                  {options.current ? (
                    options.choices
                      .filter((choice) => choice.id === integration)
                      .map((choice) => (
                        <dl className="break-all text-sm text-muted-foreground" key={choice.id}>
                          <dt>{t('endpoint')}</dt>
                          <dd>{choice.endpoint}</dd>
                          <dt>{t('namespace')}</dt>
                          <dd>{choice.namespace || t('none')}</dd>
                          <dt>{t('mount')}</dt>
                          <dd>{choice.mount}</dd>
                        </dl>
                      ))
                  ) : (
                    <p role="status">{t('optionsUnavailable')}</p>
                  )}
                  <a className="text-sm underline" href="/admin/secrets?tab=vault">
                    {t('configureVault')}
                  </a>
                </div>
              )}
            </CardContent>
          </Card>
        ))}
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        <FormField label={t('writerID')}>
          <Input
            value={writer}
            disabled={life.locked}
            onChange={(event) => setWriter(event.target.value)}
          />
        </FormField>
        <FormField label={t('provisionerID')}>
          <Input
            value={provisioner}
            disabled={life.locked}
            onChange={(event) => setProvisioner(event.target.value)}
          />
        </FormField>
      </div>
      <div className="flex flex-wrap gap-2">
        {(['writer', 'provisioner'] as const).map((purpose) => (
          <Button
            key={purpose}
            variant="outline"
            disabled={
              life.locked ||
              !life.current('secrets.write') ||
              !life.receipts[purpose] ||
              life.receipts[purpose]?.integration !== integration
            }
            onClick={() => {
              const receipt = life.receipts[purpose]
              if (receipt && receipt.integration === integration) {
                if (purpose === 'writer') setWriter(receipt.id)
                else setProvisioner(receipt.id)
              }
            }}
          >
            {t('useIdentity', { purpose: t(purpose) })}
          </Button>
        ))}
      </div>
      <p className="text-sm text-muted-foreground">{t('identityLinkHelp')}</p>
      <FormField label={t('reason')}>
        <Textarea
          value={reason}
          disabled={life.locked}
          onChange={(event) => setReason(event.target.value)}
        />
      </FormField>
      {!fresh && <p role="alert">{t('staleReview')}</p>}
      <div className="flex justify-end gap-2">
        <Button variant="outline" disabled={life.locked} onClick={() => setReview(policy)}>
          {t('acceptReview')}
        </Button>
        <Button
          type="submit"
          disabled={
            blocked ||
            !allowed.length ||
            !allowed.includes(selected) ||
            !validApprovalReason(reason)
          }
        >
          {t('reviewPolicy')}
        </Button>
      </div>
    </form>
  )
}
export function IdentityForm({
  purpose,
  policy,
  options,
  life,
}: {
  purpose: 'writer' | 'provisioner'
  policy: KeyDeliveryPolicy
  options: VaultDeliveryOptions
  life: DeliveryLifetime
}) {
  const { t } = useTranslation('keyDelivery')
  const [token, setToken] = useState(''),
    [reason, setReason] = useState(''),
    [integration, setIntegration] = useState(policy.integration_id)
  useEffect(() => {
    if (!token) return
    const timer = setTimeout(() => setToken(''), 300_000)
    return () => clearTimeout(timer)
  }, [token])
  const selected = options.current && options.choices.some((choice) => choice.id === integration)
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t(purpose)}</CardTitle>
        <CardDescription>{t(`${purpose}Help`)}</CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            if (
              !selected ||
              life.locked ||
              !life.current('secrets.write') ||
              !/^[\x21-\x7e]{1,4096}$/.test(token) ||
              !validApprovalReason(reason)
            )
              return
            life.prepare({
              kind: 'identity',
              etag: policy.review_etag,
              optionGeneration: options.generation,
              input: { purpose, integration_id: integration, token, reason: reason.trim() },
            })
            setToken('')
          }}
        >
          <FormField label={t('credentialVault', { purpose: t(purpose) })}>
            <select
              className={selectClass}
              value={options.current ? integration : ''}
              disabled={life.locked || !options.current || !life.current('secrets.write')}
              onChange={(event) => {
                setIntegration(event.target.value)
                setToken('')
              }}
            >
              <option value="">{t('none')}</option>
              {options.current &&
                integration &&
                !options.choices.some((choice) => choice.id === integration) && (
                  <option value={integration} disabled>
                    {t('selectedVaultUnavailable', { id: integration })}
                  </option>
                )}
              {options.current &&
                options.choices.map((choice) => (
                  <option value={choice.id} key={choice.id}>
                    {choice.name}
                  </option>
                ))}
            </select>
          </FormField>
          <FormField label={t('token', { purpose: t(purpose) })}>
            <Input
              type="password"
              autoComplete="off"
              value={token}
              disabled={life.locked || !selected || !life.current('secrets.write')}
              onChange={(event) => setToken(event.target.value)}
            />
          </FormField>
          <FormField label={t('reason')}>
            <Textarea
              value={reason}
              disabled={life.locked}
              onChange={(event) => setReason(event.target.value)}
            />
          </FormField>
          <div className="flex gap-2">
            <Button
              type="submit"
              disabled={
                life.locked ||
                !selected ||
                !life.current('secrets.write') ||
                !/^[\x21-\x7e]{1,4096}$/.test(token) ||
                !validApprovalReason(reason)
              }
            >
              {t('reviewIdentity')}
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                setToken('')
                setReason('')
              }}
            >
              {t('clear')}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
export function TemplateForm({
  template,
  life,
}: {
  template: KeyCallerTemplate
  life: DeliveryLifetime
}) {
  const { t } = useTranslation('keyDelivery')
  const [review, setReview] = useState(template.review_etag)
  const [draft, setDraft] = useState({
    auth_mount: template.auth_mount,
    issuer: template.issuer,
    jwks_url: template.jwks_url,
    audience: template.audience,
    subjects: template.allowed_subjects.join('\n'),
    ttl: String(template.token_ttl_seconds || 300),
    maximum: String(template.token_max_ttl_seconds || 300),
    reason: '',
  })
  const fresh = review === template.review_etag
  const ttl = Number(draft.ttl),
    maximum = Number(draft.maximum)
  const subjects = draft.subjects.split('\n')
  const valid =
    boundedText(draft.auth_mount, 128) &&
    boundedText(draft.issuer, 2048) &&
    boundedText(draft.jwks_url, 2048) &&
    boundedText(draft.audience, 256) &&
    /^[1-9][0-9]*$/.test(draft.ttl) &&
    /^[1-9][0-9]*$/.test(draft.maximum) &&
    ttl <= maximum &&
    maximum <= 3600 &&
    subjects.length <= 128 &&
    subjects.every(
      (subject) =>
        !!subject &&
        new TextEncoder().encode(subject).length <= 256 &&
        !/[\p{Cc}\p{Cs}]/u.test(subject),
    ) &&
    new Set(subjects).size === subjects.length &&
    validApprovalReason(draft.reason)
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('template')}</CardTitle>
        <CardDescription>{t('jwtOnly')}</CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            if (!valid || !fresh || life.locked || !life.current('secrets.write')) return
            life.prepare({
              kind: 'template',
              etag: review,
              input: {
                auth_mount: draft.auth_mount,
                issuer: draft.issuer,
                jwks_url: draft.jwks_url,
                audience: draft.audience,
                allowed_subjects: subjects,
                token_ttl_seconds: ttl,
                token_max_ttl_seconds: maximum,
                reason: draft.reason.trim(),
              },
            })
          }}
        >
          <div className="grid gap-4 md:grid-cols-2">
            {(['auth_mount', 'issuer', 'jwks_url', 'audience', 'ttl', 'maximum'] as const).map(
              (key) => (
                <FormField key={key} label={t(`field.${key}`)}>
                  <Input
                    value={draft[key]}
                    disabled={life.locked}
                    onChange={(event) =>
                      setDraft((current) => ({ ...current, [key]: event.target.value }))
                    }
                  />
                </FormField>
              ),
            )}
          </div>
          <FormField label={t('subjects')}>
            <Textarea
              value={draft.subjects}
              disabled={life.locked}
              onChange={(event) =>
                setDraft((current) => ({ ...current, subjects: event.target.value }))
              }
            />
          </FormField>
          <p className="text-sm text-muted-foreground">{t('subjectsHelp')}</p>
          <FormField label={t('reason')}>
            <Textarea
              value={draft.reason}
              disabled={life.locked}
              onChange={(event) =>
                setDraft((current) => ({ ...current, reason: event.target.value }))
              }
            />
          </FormField>
          {!fresh && <p role="alert">{t('staleReview')}</p>}
          <div className="flex gap-2">
            <Button
              variant="outline"
              disabled={life.locked}
              onClick={() => setReview(template.review_etag)}
            >
              {t('acceptReview')}
            </Button>
            <Button
              type="submit"
              disabled={!valid || !fresh || life.locked || !life.current('secrets.write')}
            >
              {t('reviewTemplate')}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
