import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  saveVaultIntegration,
  validVaultDescriptor,
  validVaultName,
  validVaultReason,
  validVaultToken,
  VaultError,
} from '@/api/vault-integrations'
import type {
  VaultAuthInput,
  VaultConfigIntent,
  VaultDescriptor,
  VaultIntegration,
  VaultSaved,
} from '@/types/vault-integrations'
import { Drawer } from '@/components/ui/drawer'
import { Dialog } from '@/components/ui/dialog'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'

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
  const active = useRef(true),
    lock = useRef(false),
    controller = useRef<AbortController | null>(null)
  useEffect(() => {
    active.current = true
    return () => {
      active.current = false
      controller.current?.abort()
    }
  }, [])
  useEffect(() => {
    if (!visible) controller.current?.abort()
  }, [visible])
  const currentReview = reviewed === etag
  const valid =
    validVaultName(name) &&
    validVaultReason(reason) &&
    validVaultDescriptor(descriptor) &&
    (writer !== 'replace' || validVaultToken(writerToken)) &&
    (reader !== 'replace' || validVaultToken(readerToken)) &&
    !(writer === 'replace' && reader === 'replace' && writerToken === readerToken)
  function capture() {
    if (
      !visible ||
      !canSave ||
      !authority() ||
      !currentReview ||
      !valid ||
      uncertain ||
      lock.current
    )
      return
    const auth = (action: VaultAuthInput['action'], token: string): VaultAuthInput =>
      action === 'replace' ? { action, token } : { action }
    setIntent({
      ...(id ? { id } : {}),
      etag: reviewed,
      input: {
        request_id: crypto.randomUUID(),
        name,
        descriptor: { ...descriptor },
        writer_auth: auth(writer, writerToken),
        reader_auth: auth(reader, readerToken),
        reason,
      },
    })
    setConfirm(true)
  }
  async function dispatch(retry = false) {
    const gate = authority()
    if (!intent || !gate || !visible || !canSave || lock.current) return
    lock.current = true
    setBusy(true)
    setConfirm(false)
    const abort = new AbortController()
    controller.current = abort
    try {
      const result = await saveVaultIntegration(intent, gate.csrf, abort.signal)
      if (!active.current) return
      if (abort.signal.aborted || authority()?.epoch !== gate.epoch) {
        setUncertain(true)
        setNotice('unknown')
        return
      }
      setWriterToken('')
      setReaderToken('')
      setIntent(undefined)
      setUncertain(false)
      setNotice('saved')
      setResult(result)
      setReviewed('')
      saved(result)
    } catch (error) {
      if (!active.current) return
      const status = error instanceof VaultError ? error.status : 0
      if (
        retry ||
        abort.signal.aborted ||
        authority()?.epoch !== gate.epoch ||
        ![400, 401, 403, 404, 409, 428].includes(status)
      ) {
        setUncertain(true)
        setNotice('unknown')
      } else {
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
  ) => (
    <Card role="group" aria-label={t(`vault.${kind}`)}>
      <CardHeader>
        <CardTitle>{t(`vault.${kind}`)}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <p>{t(configured ? 'vault.configured' : 'vault.notConfigured')}</p>
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
                }}
              >
                {t(`vault.${value}`)}
              </Button>
            ))}
        </div>
        {action === 'replace' && (
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
        )}
      </CardContent>
    </Card>
  )
  return (
    <Drawer
      open={open && visible}
      onOpenChange={(value) => {
        if (!value) close()
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
        )}
        {tokenCard(
          'reader',
          reader,
          setReader,
          readerToken,
          setReaderToken,
          integration?.reader_auth.configured === true,
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
          <Button variant="outline" disabled={busy} onClick={close}>
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
                  void review().then((next) => {
                    if (active.current && !uncertain) {
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
        onOpenChange={setConfirm}
        title={t('vault.confirmSave')}
        description={t('vault.confirmSaveDescription')}
        busy={busy}
      >
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => setConfirm(false)}>
            {t('cancel')}
          </Button>
          <Button onClick={() => void dispatch()}>{t('confirm')}</Button>
        </div>
      </Dialog>
    </Drawer>
  )
}
