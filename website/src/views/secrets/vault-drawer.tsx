import { useCallback, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  saveVaultIntegration,
  sameVaultAuthReplacement,
  validVaultAuthMount,
  validVaultCertificateAuth,
  validVaultConfigSize,
  validVaultDescriptor,
  validVaultName,
  validVaultReason,
  validVaultToken,
  VaultError,
} from '@/api/vault-integrations'
import type {
  VaultAuthInput,
  VaultAuthMethod,
  VaultConfigIntent,
  VaultDescriptor,
  VaultIntegration,
  VaultSaved,
} from '@/types/vault-integrations'
import { Drawer } from '@/components/ui/drawer'
import { Dialog } from '@/components/ui/dialog'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'

type AppRoleDraft = { auth_mount: string; role_id: string; secret_id: string }
const emptyAppRole = (): AppRoleDraft => ({ auth_mount: 'approle', role_id: '', secret_id: '' })
type CertificateDraft = Omit<
  Extract<VaultAuthInput, { method: 'certificate' }>,
  'action' | 'method'
>
const emptyCertificate = (): CertificateDraft => ({
  auth_mount: 'cert',
  certificate_role: '',
  certificate_pem: '',
  private_key_pem: '',
  server_ca_pem: '',
})
export interface VaultAuthority {
  csrf: string
  epoch: number
}
export function VaultDrawer({
  open,
  visible,
  id,
  integration,
  etag,
  initialETag,
  canSave,
  authority,
  close,
  review,
  saved,
}: {
  open: boolean
  visible: boolean
  id?: string
  integration?: VaultIntegration
  etag: string
  initialETag?: string
  canSave: boolean
  authority: () => VaultAuthority | null
  close: () => void
  review: () => Promise<string | undefined>
  saved: (result: VaultSaved) => void
}) {
  const { t } = useTranslation('secrets')
  const [name, setName] = useState(integration?.name ?? '')
  const [descriptor, setDescriptor] = useState<VaultDescriptor>(
    integration?.descriptor ?? {
      endpoint: '',
      namespace: '',
      mount: 'secret',
      prefix: 'routex',
      data_field: 'value',
    },
  )
  const [writer, setWriter] = useState<VaultAuthInput['action']>(
    id && integration?.writer_auth.configured ? 'keep' : 'replace',
  )
  const [reader, setReader] = useState<VaultAuthInput['action']>(
    id && integration?.reader_auth.configured ? 'keep' : 'replace',
  )
  const [writerMethod, setWriterMethod] = useState<VaultAuthMethod>(
    integration?.writer_auth.method ?? 'token',
  )
  const [readerMethod, setReaderMethod] = useState<VaultAuthMethod>(
    integration?.reader_auth.method ?? 'token',
  )
  const [writerCertificate, setWriterCertificate] = useState<CertificateDraft>(emptyCertificate)
  const [readerCertificate, setReaderCertificate] = useState<CertificateDraft>(emptyCertificate)
  const [writerRole, setWriterRole] = useState<AppRoleDraft>(emptyAppRole)
  const [readerRole, setReaderRole] = useState<AppRoleDraft>(emptyAppRole)
  const [writerToken, setWriterToken] = useState(''),
    [readerToken, setReaderToken] = useState(''),
    [reason, setReason] = useState('')
  const [intent, setIntent] = useState<VaultConfigIntent>(),
    [uncertain, setUncertain] = useState(false),
    [notice, setNotice] = useState(''),
    [confirm, setConfirm] = useState(false),
    [busy, setBusy] = useState(false)
  const [result, setResult] = useState<VaultSaved>()
  const [reviewed, setReviewed] = useState(initialETag ?? etag)
  const [confirmationScope, setConfirmationScope] = useState({ open, visible, canSave, etag })
  if (
    confirmationScope.open !== open ||
    confirmationScope.visible !== visible ||
    confirmationScope.canSave !== canSave ||
    confirmationScope.etag !== etag
  ) {
    setConfirmationScope({ open, visible, canSave, etag })
    setConfirm(false)
    // Pending and uncertain requests keep their exact already dispatched body.
    if (!busy && !uncertain) setIntent(undefined)
  }
  const active = useRef(true),
    lock = useRef(false),
    dismissed = useRef(false),
    controller = useRef<AbortController | null>(null),
    confirmation = useRef<{ intent: VaultConfigIntent; epoch: number } | null>(null),
    dispatched = useRef<VaultConfigIntent | null>(null),
    reviewGeneration = useRef(0),
    current = useRef({ open, visible, canSave, etag, authority })
  // Revoking a review must never discard an already dispatched uncertain body.
  const discardConfirmation = useCallback((cancelReview = true) => {
    confirmation.current = null
    if (cancelReview) reviewGeneration.current++
    setConfirm(false)
    if (!dispatched.current) setIntent(undefined)
  }, [])
  useLayoutEffect(() => {
    current.current = { open, visible, canSave, etag, authority }
  }, [open, visible, canSave, etag, authority])
  useLayoutEffect(() => {
    active.current = true
    return () => {
      active.current = false
      confirmation.current = null
      dispatched.current = null
      reviewGeneration.current += 1
      controller.current?.abort()
    }
  }, [])
  useLayoutEffect(() => {
    confirmation.current = null
    if (!open || !visible || !canSave) controller.current?.abort()
  }, [open, visible, canSave, etag])
  function dismiss() {
    dismissed.current = true
    discardConfirmation()
    dispatched.current = null
    controller.current?.abort()
    setIntent(undefined)
    setWriterToken('')
    setReaderToken('')
    setWriterRole(emptyAppRole())
    setReaderRole(emptyAppRole())
    setWriterCertificate(emptyCertificate())
    setReaderCertificate(emptyCertificate())
    close()
  }
  const currentReview = reviewed === etag
  const auth = (
    action: VaultAuthInput['action'],
    method: VaultAuthMethod,
    token: string,
    role: AppRoleDraft,
    certificate: CertificateDraft,
  ): VaultAuthInput =>
    action === 'replace'
      ? method === 'token'
        ? { action, token }
        : method === 'approle'
          ? { action, method, ...role }
          : { action, method: 'certificate', ...certificate }
      : { action }
  const writerInput = auth(writer, writerMethod, writerToken, writerRole, writerCertificate)
  const readerInput = auth(reader, readerMethod, readerToken, readerRole, readerCertificate)
  const validReplacement = (value: VaultAuthInput): boolean => {
    if (value.action !== 'replace') return true
    if (value.method === 'certificate')
      return descriptor.endpoint.startsWith('https://') && validVaultCertificateAuth(value)
    if (value.method === 'approle')
      return (
        validVaultAuthMount(value.auth_mount) &&
        validVaultToken(value.role_id) &&
        validVaultToken(value.secret_id)
      )
    return validVaultToken(value.token)
  }
  const valid =
    validVaultName(name) &&
    validVaultReason(reason) &&
    validVaultDescriptor(descriptor) &&
    (!(
      (writer === 'keep' && integration?.writer_auth.method === 'certificate') ||
      (reader === 'keep' && integration?.reader_auth.method === 'certificate')
    ) ||
      descriptor.endpoint.startsWith('https://')) &&
    validReplacement(writerInput) &&
    validReplacement(readerInput) &&
    !sameVaultAuthReplacement(writerInput, readerInput) &&
    validVaultConfigSize({
      request_id: '00000000-0000-4000-8000-000000000000',
      name,
      descriptor,
      writer_auth: writerInput,
      reader_auth: readerInput,
      reason,
    })
  function capture() {
    const live = current.current,
      gate = live.authority()
    if (
      !active.current ||
      dismissed.current ||
      !live.open ||
      !live.visible ||
      !live.canSave ||
      !gate ||
      reviewed !== live.etag ||
      !valid ||
      uncertain ||
      dispatched.current ||
      lock.current
    )
      return
    const next: VaultConfigIntent = {
      ...(id ? { id } : {}),
      etag: reviewed,
      input: {
        request_id: crypto.randomUUID(),
        name,
        descriptor: { ...descriptor },
        writer_auth: writerInput,
        reader_auth: readerInput,
        reason,
      },
    }
    confirmation.current = { intent: next, epoch: gate.epoch }
    setIntent(next)
    setConfirm(true)
  }
  async function dispatch(retry = false) {
    const live = current.current,
      gate = live.authority()
    if (
      !active.current ||
      dismissed.current ||
      !intent ||
      !gate ||
      !live.open ||
      !live.visible ||
      !live.canSave ||
      lock.current
    )
      return
    if (retry) {
      if (dispatched.current !== intent) return
    } else if (
      confirmation.current?.intent !== intent ||
      confirmation.current.epoch !== gate.epoch ||
      intent.etag !== live.etag
    )
      return
    confirmation.current = null
    dispatched.current = intent
    lock.current = true
    setBusy(true)
    setConfirm(false)
    const abort = new AbortController()
    controller.current = abort
    try {
      const result = await saveVaultIntegration(intent, gate.csrf, abort.signal)
      if (!active.current || dismissed.current) return
      if (abort.signal.aborted || current.current.authority()?.epoch !== gate.epoch) {
        setUncertain(true)
        setNotice('unknown')
        return
      }
      setWriterToken('')
      setReaderToken('')
      setWriterRole(emptyAppRole())
      setReaderRole(emptyAppRole())
      setWriterCertificate(emptyCertificate())
      setReaderCertificate(emptyCertificate())
      dispatched.current = null
      setIntent(undefined)
      setUncertain(false)
      setNotice('saved')
      setResult(result)
      setReviewed('')
      saved(result)
    } catch (error) {
      if (!active.current || dismissed.current) return
      const status = error instanceof VaultError ? error.status : 0
      if (
        retry ||
        abort.signal.aborted ||
        current.current.authority()?.epoch !== gate.epoch ||
        ![400, 401, 403, 404, 409, 428].includes(status)
      ) {
        setUncertain(true)
        setNotice('unknown')
      } else {
        dispatched.current = null
        setIntent(undefined)
        setNotice(status === 409 ? 'conflict' : 'rejected')
        setReviewed('')
      }
    } finally {
      lock.current = false
      if (controller.current === abort) controller.current = null
      if (active.current) setBusy(false)
    }
  }
  const fields = ['endpoint', 'namespace', 'mount', 'prefix', 'data_field'] as const
  const tokenCard = (
    kind: 'writer' | 'reader',
    action: VaultAuthInput['action'],
    setAction: (value: VaultAuthInput['action']) => void,
    token: string,
    setToken: (value: string) => void,
    configured: boolean,
    method: VaultAuthMethod,
    setMethod: (value: VaultAuthMethod) => void,
    role: AppRoleDraft,
    setRole: (value: AppRoleDraft) => void,
    certificate: CertificateDraft,
    setCertificate: (value: CertificateDraft) => void,
  ) => (
    <Card role="group" aria-label={t(`vault.${kind}`)}>
      <CardHeader>
        <CardTitle>{t(`vault.${kind}`)}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <p>
          {t(configured ? 'vault.configured' : 'vault.notConfigured')} ·{' '}
          {t(`vault.${integration?.[`${kind}_auth`].method ?? 'token'}`)}
        </p>
        <p className="text-sm text-muted-foreground">{t(`vault.${kind}Guidance`)}</p>
        <div className="flex gap-2">
          {(['keep', 'replace', 'remove'] as const)
            .filter((value) => id || value !== 'keep')
            .map((value) => (
              <Button
                key={value}
                variant={value === action ? 'default' : 'outline'}
                disabled={busy || uncertain}
                aria-pressed={value === action}
                onClick={() => {
                  setAction(value)
                  setToken('')
                  setRole(emptyAppRole())
                  setCertificate(emptyCertificate())
                  discardConfirmation()
                }}
              >
                {t(
                  `vault.${value}${(value === 'replace' ? method : integration?.[`${kind}_auth`].method) === 'approle' ? 'AppRole' : (value === 'replace' ? method : integration?.[`${kind}_auth`].method) === 'certificate' ? 'Certificate' : ''}`,
                )}
              </Button>
            ))}
        </div>
        {action === 'replace' && (
          <>
            <div role="group" aria-label={t(`vault.${kind}Method`)} className="flex gap-2">
              {(['token', 'approle', 'certificate'] as const).map((value) => (
                <Button
                  key={value}
                  variant={value === method ? 'default' : 'outline'}
                  aria-pressed={value === method}
                  disabled={busy || uncertain}
                  onClick={() => {
                    if (value !== method) {
                      setMethod(value)
                      setToken('')
                      setRole(emptyAppRole())
                      setCertificate(emptyCertificate())
                      discardConfirmation()
                    }
                  }}
                >
                  {t(`vault.${value}`)}
                </Button>
              ))}
            </div>
            {method === 'token' ? (
              <label className="block space-y-1">
                <span>{t(`vault.${kind}Token`)}</span>
                <Input
                  type="password"
                  autoComplete="new-password"
                  value={token}
                  disabled={busy || uncertain}
                  onChange={(event) => setToken(event.target.value)}
                />
              </label>
            ) : method === 'approle' ? (
              <>
                <p className="text-sm text-muted-foreground">{t('vault.appRoleGuidance')}</p>
                {(['auth_mount', 'role_id', 'secret_id'] as const).map((field) => (
                  <label key={field} className="block space-y-1">
                    <span>{t(`vault.${kind}_${field}`)}</span>
                    <Input
                      type={field === 'secret_id' ? 'password' : 'text'}
                      autoComplete={field === 'secret_id' ? 'new-password' : 'off'}
                      value={role[field]}
                      disabled={busy || uncertain}
                      onChange={(event) => setRole({ ...role, [field]: event.target.value })}
                    />
                  </label>
                ))}
              </>
            ) : (
              <div className="vault-certificate-fields space-y-3">
                <p className="text-sm text-muted-foreground">{t('vault.certificateGuidance')}</p>
                {(['auth_mount', 'certificate_role'] as const).map((field) => (
                  <label key={field} className="block space-y-1">
                    <span>
                      {t(
                        field === 'auth_mount'
                          ? `vault.${kind}_auth_mount`
                          : `vault.${kind}_certificate_role`,
                      )}
                    </span>
                    <Input
                      autoComplete="off"
                      value={certificate[field]}
                      disabled={busy || uncertain}
                      onChange={(event) =>
                        setCertificate({ ...certificate, [field]: event.target.value })
                      }
                    />
                  </label>
                ))}
                {(['certificate_pem', 'private_key_pem', 'server_ca_pem'] as const).map((field) => (
                  <label key={field} className="block space-y-1">
                    <span>{t(`vault.${kind}_${field}`)}</span>
                    <Textarea
                      autoComplete="off"
                      spellCheck={false}
                      autoCapitalize="none"
                      value={certificate[field] ?? ''}
                      disabled={busy || uncertain}
                      onChange={(event) =>
                        setCertificate({ ...certificate, [field]: event.target.value })
                      }
                    />
                  </label>
                ))}
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
  return (
    <Drawer
      open={open && visible}
      onOpenChange={(value) => {
        if (!value) dismiss()
      }}
      title={t(id ? 'vault.edit' : 'vault.add')}
      description={t('vault.drawerDescription')}
      width={720}
      busy={busy}
    >
      <div className="space-y-5">
        <label className="block space-y-1">
          <span>{t('vault.name')}</span>
          <Input
            value={name}
            disabled={busy || uncertain}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        {fields.map((field) => (
          <label key={field} className="block space-y-1">
            <span>{t(`vault.${field}`)}</span>
            <Input
              autoComplete="off"
              value={descriptor[field]}
              disabled={busy || uncertain}
              onChange={(event) => setDescriptor({ ...descriptor, [field]: event.target.value })}
            />
          </label>
        ))}
        {tokenCard(
          'writer',
          writer,
          setWriter,
          writerToken,
          setWriterToken,
          integration?.writer_auth.configured === true,
          writerMethod,
          setWriterMethod,
          writerRole,
          setWriterRole,
          writerCertificate,
          setWriterCertificate,
        )}
        {tokenCard(
          'reader',
          reader,
          setReader,
          readerToken,
          setReaderToken,
          integration?.reader_auth.configured === true,
          readerMethod,
          setReaderMethod,
          readerRole,
          setReaderRole,
          readerCertificate,
          setReaderCertificate,
        )}
        <label className="block space-y-1">
          <span>{t('reason')}</span>
          <Input
            value={reason}
            disabled={busy || uncertain}
            onChange={(event) => setReason(event.target.value)}
          />
        </label>
        <p className="text-sm text-muted-foreground">{t('vault.validation')}</p>
        {notice && <p role="status">{t(`vault.${notice}`)}</p>}
        {result && (
          <p>
            {t('request')}: {result.request_id} · {t('vault.revision')}: {result.revision_id}
          </p>
        )}
        {!canSave && <p>{t('vault.noWrite')}</p>}
        {uncertain && <p>{t('vault.secretLifetime')}</p>}
        <div className="flex justify-end gap-2">
          <Button variant="outline" disabled={busy} onClick={dismiss}>
            {t('cancel')}
          </Button>
          {uncertain ? (
            <Button disabled={busy || !canSave} onClick={() => void dispatch(true)}>
              {t('retry')}
            </Button>
          ) : (
            <>
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => {
                  discardConfirmation()
                  const generation = reviewGeneration.current
                  if (
                    !active.current ||
                    dismissed.current ||
                    !current.current.authority() ||
                    dispatched.current
                  )
                    return
                  // This explicit read advances the parent's query epoch itself.
                  // It can renew review facts, never an old confirmation identity.
                  void review().then((next) => {
                    const live = current.current
                    if (
                      active.current &&
                      !dismissed.current &&
                      !dispatched.current &&
                      generation === reviewGeneration.current &&
                      live.authority()
                    ) {
                      setReviewed(next ?? '')
                      setNotice('')
                    }
                  })
                }}
              >
                {t('review')}
              </Button>
              <Button disabled={busy || !canSave || !valid || !currentReview} onClick={capture}>
                {t('vault.save')}
              </Button>
            </>
          )}
        </div>
      </div>
      <Dialog
        open={confirm && visible}
        onOpenChange={(value) => {
          if (!value) discardConfirmation()
        }}
        title={t('vault.confirmSave')}
        description={t('vault.confirmSaveDescription')}
        busy={busy}
      >
        <div className="flex justify-end gap-2">
          <Button
            variant="outline"
            onClick={() => {
              discardConfirmation()
            }}
          >
            {t('cancel')}
          </Button>
          <Button onClick={() => void dispatch()}>{t('confirm')}</Button>
        </div>
      </Dialog>
    </Drawer>
  )
}
