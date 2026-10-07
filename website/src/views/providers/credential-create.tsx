import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import client from '@/api/client'
import { egressSelection } from '@/api/egress'
import {
  validAzureAPIVersion,
  validAzureOrigin,
  type ConnectionAdapter,
} from '@/api/connection-transport'
import type { CredentialStorageContext } from '@/types/provider-storage'
import { EgressSelect } from '@/views/egress/connection'
import { FormField } from '@/components/app/CatalogUI'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { useCredentialStorageContext } from './credential-storage-context'

type Kind = 'provider' | 'connection' | 'credential'
type Props = {
  kind: Kind
  id?: string
  onClose: () => void
  onSaved: () => void
  targetCurrent: () => boolean
}
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
export default function CredentialCreateDialog(props: Props) {
  const authority = useCredentialStorageContext()
  return (
    <CreateEditor
      key={`${authority.actor}:${props.kind}:${props.id ?? ''}`}
      {...props}
      authority={authority}
    />
  )
}
function CreateEditor({
  kind,
  id,
  onClose,
  onSaved,
  targetCurrent,
  authority,
}: Props & { authority: ReturnType<typeof useCredentialStorageContext> }) {
  const { t } = useTranslation('catalog')
  const [reviewed, setReviewed] = useState<CredentialStorageContext>()
  const [secret, setSecret] = useState('')
  const [adapter, setAdapter] = useState<ConnectionAdapter>('native')
  const [protocol, setProtocol] = useState('openai_chat')
  const [apiVersion, setAPIVersion] = useState('')
  const [busy, setBusy] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [notice, setNotice] = useState('')
  const intent = useRef<
    { path: string; body: Record<string, unknown>; source: 'inline' | 'vault' } | undefined
  >(undefined)
  const lock = useRef(false)
  const alive = useRef(true)
  if (authority.context && !reviewed) setReviewed(authority.context)
  const context = reviewed
  const stale =
    conflict || (!!context && !!authority.context && context.etag !== authority.context.etag)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      intent.current = undefined
    }
  }, [])
  function dismiss() {
    if (busy) return
    intent.current = undefined
    setSecret('')
    onClose()
  }
  async function review() {
    if (lock.current || uncertain) return
    setBusy(true)
    try {
      const result = await authority.query.refetch()
      if (!alive.current) return
      if (result.isError || !authority.current() || !targetCurrent())
        throw new Error('Storage review unavailable')
      setReviewed(result.data)
      setConflict(false)
      setNotice('credentialStorage.reviewed')
    } catch {
      if (alive.current) setNotice('credentialStorage.failed')
    } finally {
      if (alive.current) setBusy(false)
    }
  }
  async function submit(event?: FormEvent<HTMLFormElement>, retry = false) {
    event?.preventDefault()
    if (
      lock.current ||
      !authority.current() ||
      !targetCurrent() ||
      !authority.session.data ||
      (retry ? !uncertain || !intent.current : uncertain || stale || !context)
    )
      return
    if (!retry && event) {
      const form = new FormData(event.currentTarget)
      const value = (field: string) => String(form.get(field) ?? '').trim()
      const rawBaseURL = String(form.get('base_url') ?? '')
      const bytes = new TextEncoder().encode(secret).length
      if (!bytes || bytes > 2048 || /[\r\n]/.test(secret)) {
        setNotice('credentialReplacement.secretError')
        return
      }
      if (
        kind !== 'credential' &&
        adapter === 'azure_openai_classic' &&
        (protocol !== 'openai_chat' ||
          !validAzureAPIVersion(apiVersion) ||
          !validAzureOrigin(rawBaseURL))
      ) {
        setNotice('azureTransport.invalid')
        return
      }
      const transport =
        kind === 'credential'
          ? {}
          : { adapter, api_version: adapter === 'native' ? null : apiVersion }
      let requestId: string
      try {
        requestId = crypto.randomUUID()
      } catch {
        setNotice('credentialStorage.failed')
        return
      }
      if (!uuid.test(requestId)) {
        setNotice('credentialStorage.failed')
        return
      }
      const shared = { request_id: requestId, storage_policy_etag: context!.etag, secret }
      const body =
        kind === 'provider'
          ? {
              ...shared,
              ...transport,
              name: value('name'),
              connection_name: value('connection_name'),
              base_url: value('base_url'),
              protocol: value('protocol'),
              ...egressSelection(value('egress_selection')),
              credential_name: value('credential_name'),
            }
          : kind === 'connection'
            ? {
                ...shared,
                ...transport,
                name: value('name'),
                base_url: value('base_url'),
                protocol: value('protocol'),
                ...egressSelection(value('egress_selection')),
                credential_name: value('credential_name'),
              }
            : { ...shared, name: value('name'), priority: Number(value('priority')) }
      intent.current = {
        source: context!.storage_source,
        path:
          kind === 'provider'
            ? '/admin/providers'
            : kind === 'connection'
              ? `/admin/providers/${encodeURIComponent(id ?? '')}/connections`
              : `/admin/connections/${encodeURIComponent(id ?? '')}/credentials`,
        body,
      }
    }
    const captured = intent.current
    if (!captured) return
    const stamp = authority.stamp()
    lock.current = true
    setBusy(true)
    setNotice('')
    try {
      const result = await client.post(captured.path, captured.body, {
        headers: { 'X-CSRF-Token': authority.csrf() },
      })
      if (!alive.current) return
      const credential =
        kind === 'provider'
          ? result.data?.connections?.[0]?.credentials?.[0]
          : kind === 'connection'
            ? result.data?.credentials?.[0]
            : result.data
      if (
        result.status !== 201 ||
        (kind === 'provider' &&
          (result.data?.connections?.length !== 1 ||
            result.data.connections[0]?.credentials?.length !== 1)) ||
        (kind === 'connection' && result.data?.credentials?.length !== 1) ||
        credential?.storage_source !== captured.source ||
        typeof result.data?.id !== 'string' ||
        !result.data.id ||
        !authority.current() ||
        !targetCurrent() ||
        authority.stamp() !== stamp
      )
        throw new Error('Creation acknowledgement unavailable')
      intent.current = undefined
      setSecret('')
      onSaved()
    } catch (error) {
      if (!alive.current) return
      const status = axios.isAxiosError(error) ? error.response?.status : undefined
      if (
        retry ||
        !status ||
        status >= 500 ||
        !authority.current() ||
        !targetCurrent() ||
        authority.stamp() !== stamp
      ) {
        setUncertain(true)
        setNotice('credentialStorage.uncertain')
      } else {
        intent.current = undefined
        setConflict(status === 409)
        setNotice(status === 409 ? 'credentialStorage.stale' : 'credentialStorage.failed')
      }
    } finally {
      lock.current = false
      if (alive.current) setBusy(false)
    }
  }
  const title =
    kind === 'provider'
      ? 'providers.addProvider'
      : kind === 'connection'
        ? 'providers.addConnection'
        : 'providers.addCredential'
  const locked = busy || uncertain || !authority.ready || !targetCurrent()
  return (
    <Dialog
      open
      width={640}
      busy={busy}
      onOpenChange={(value) => {
        if (!value) dismiss()
      }}
      title={t(title)}
      description={t('providers.dialogDescription')}
    >
      {!authority.ready && (
        <p role="status">
          {t(authority.query.error ? 'credentialStorage.loadError' : 'credentialStorage.loading')}
        </p>
      )}
      <form className="space-y-5" onSubmit={(event) => void submit(event)}>
        <div hidden={!authority.ready}>
          {context && (
            <div className="mb-5 rounded-lg border bg-muted/30 p-4 text-sm">
              <p>
                {t('credentialStorage.futureSource')}:{' '}
                <span className="font-medium">
                  {t(`credentialStorage.${context.storage_source}`)}
                </span>
              </p>
              <p className="mt-1 text-muted-foreground">{t('credentialStorage.pending')}</p>
            </div>
          )}
          <fieldset disabled={locked} className="space-y-5">
            <FormField label={kind === 'provider' ? t('common.providerName') : t('common.name')}>
              <Input name="name" required maxLength={100} />
            </FormField>
            {kind === 'provider' && (
              <FormField label={t('common.connectionName')}>
                <Input name="connection_name" required maxLength={100} />
              </FormField>
            )}
            {kind !== 'credential' && (
              <>
                <FormField label={t('azureTransport.adapter')}>
                  <select
                    value={adapter}
                    aria-label={t('azureTransport.adapter')}
                    className="h-10 w-full rounded-md border bg-background px-3"
                    onChange={(event) => {
                      const selected = event.target.value as ConnectionAdapter
                      setAdapter(selected)
                      setAPIVersion('')
                      if (selected === 'azure_openai_classic') setProtocol('openai_chat')
                    }}
                  >
                    <option value="native">{t('azureTransport.native')}</option>
                    <option value="azure_openai_classic">{t('azureTransport.classic')}</option>
                  </select>
                </FormField>
                {adapter === 'azure_openai_classic' && (
                  <>
                    <p className="text-sm text-muted-foreground">{t('azureTransport.guidance')}</p>
                    <FormField label={t('azureTransport.version')}>
                      <Input
                        name="api_version"
                        required
                        maxLength={18}
                        autoComplete="off"
                        value={apiVersion}
                        onChange={(event) => setAPIVersion(event.target.value)}
                        placeholder="YYYY-MM-DD"
                      />
                    </FormField>
                  </>
                )}
                <FormField label={t('common.protocolType')}>
                  <select
                    name="protocol"
                    value={protocol}
                    onChange={(event) => setProtocol(event.target.value)}
                    className="h-10 w-full rounded-md border bg-background px-3"
                  >
                    <option value="openai_chat">OpenAI Chat</option>
                    <option value="openai_responses" disabled={adapter === 'azure_openai_classic'}>
                      OpenAI Responses
                    </option>
                    <option
                      value="anthropic_messages"
                      disabled={adapter === 'azure_openai_classic'}
                    >
                      Anthropic Messages
                    </option>
                    <option
                      value="gemini_generate_content"
                      disabled={adapter === 'azure_openai_classic'}
                    >
                      Gemini Generate Content
                    </option>
                  </select>
                </FormField>
                <FormField label={t('common.baseURL')}>
                  <Input
                    name="base_url"
                    type={adapter === 'azure_openai_classic' ? 'text' : 'url'}
                    inputMode="url"
                    required
                    placeholder={
                      adapter === 'azure_openai_classic'
                        ? 'https://resource.openai.azure.com'
                        : 'https://api.example.com/v1'
                    }
                  />
                </FormField>
                <EgressSelect />
                <FormField label={t('common.credentialName')}>
                  <Input name="credential_name" required maxLength={100} />
                </FormField>
              </>
            )}
            <FormField label={t('common.upstreamAPIKey')}>
              <Input
                name="secret"
                type="password"
                autoComplete="new-password"
                required
                value={secret}
                onChange={(event) => setSecret(event.target.value)}
              />
            </FormField>
            {kind === 'credential' && (
              <FormField label={t('common.priority')}>
                <Input name="priority" type="number" min={0} step={1} defaultValue={0} required />
              </FormField>
            )}
          </fieldset>
        </div>
        {stale && !uncertain && <p role="alert">{t('credentialStorage.stale')}</p>}
        {notice && <p role="status">{t(notice)}</p>}
        <div className="flex flex-wrap justify-end gap-2">
          <Button type="button" variant="outline" disabled={busy} onClick={dismiss}>
            {t('common.cancel')}
          </Button>
          {!uncertain && (stale || authority.query.error) && (
            <Button
              type="button"
              variant="outline"
              disabled={busy || !authority.permissions.can('providers.write')}
              onClick={() => void review()}
            >
              {t('credentialStorage.review')}
            </Button>
          )}
          {uncertain ? (
            <Button
              type="button"
              disabled={busy || !authority.ready || !targetCurrent()}
              onClick={() => void submit(undefined, true)}
            >
              {t('credentialStorage.retry')}
            </Button>
          ) : (
            <Button type="submit" disabled={locked || stale || !context}>
              {t('common.save')}
            </Button>
          )}
        </div>
      </form>
    </Dialog>
  )
}
