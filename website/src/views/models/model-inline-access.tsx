import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import {
  createModelAccess,
  enableModelAccess,
  listModelAccessProviders,
  ModelAccessError,
  readModelAccessCredential,
  readModelAccessEgress,
  readModelAccessStorage,
  validateModelAccessInput,
  verifyModelAccess,
} from '@/api/model-access'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { ModelAccessInput, ModelAccessSaved } from '@/types/model-access'
import type { CredentialStorageContext, CredentialStorageSource } from '@/types/provider-storage'
import type { ModelCreationProtocol } from '@/types/model-creation'
import { useConnectionQueryRevision } from '@/views/providers/connection-authority'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog } from '@/components/ui/dialog'

interface Props {
  actor: string
  basis: string
  readable: boolean
  writable: boolean
  fresh: () => boolean
  writeFresh: () => boolean
  onSaved: (saved: ModelAccessSaved) => void
  onInteractionLockChange: (locked: boolean) => void
}
interface Intent {
  input: ModelAccessInput
  source: CredentialStorageSource
  provider?: { id: string; name: string }
}
const protocols: ModelCreationProtocol[] = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_generate_content',
]
const selectClass = 'h-11 w-full rounded-md border bg-background px-3'

export function ModelAccessSummary({ saved }: { saved: ModelAccessSaved }) {
  const { t } = useTranslation('modelCreation')
  return (
    <dl className="grid gap-2 text-sm sm:grid-cols-[180px_1fr]">
      <dt>{t('provider')}</dt>
      <dd>{saved.provider_name}</dd>
      <dt>{t('connection')}</dt>
      <dd>{saved.connection_name}</dd>
      <dt>{t('access.credentialName')}</dt>
      <dd>{saved.credential_name}</dd>
      <dt>{t('protocol')}</dt>
      <dd>{saved.protocol}</dd>
      <dt>{t('baseURL')}</dt>
      <dd className="break-all">{saved.base_url}</dd>
    </dl>
  )
}

export function ModelAccessForm({
  actor,
  basis,
  readable,
  writable,
  fresh,
  writeFresh,
  onSaved,
  onInteractionLockChange,
}: Props) {
  const { t } = useTranslation('modelCreation'),
    cache = useQueryClient()
  const [providerKind, setProviderKind] = useState<'new' | 'existing'>('new')
  const [providerQuery, setProviderQuery] = useState(''),
    [provider, setProvider] = useState<{ id: string; name: string } | undefined>()
  const [providerName, setProviderName] = useState(''),
    [connectionName, setConnectionName] = useState(''),
    [credentialName, setCredentialName] = useState(''),
    [baseURL, setBaseURL] = useState(''),
    [secret, setSecret] = useState('')
  const [protocol, setProtocol] = useState<ModelCreationProtocol>('openai_chat'),
    [adapter, setAdapter] = useState<'native' | 'azure_openai_classic'>('native'),
    [apiVersion, setAPIVersion] = useState('')
  const [egressQuery, setEgressQuery] = useState(''),
    [egress, setEgress] = useState<{ id: string; name: string } | undefined>(),
    [egressMode, setEgressMode] = useState<'default' | 'direct' | 'proxy'>('default')
  const [saved, setSaved] = useState<ModelAccessSaved | null>(null),
    [busy, setBusy] = useState(false),
    [uncertain, setUncertain] = useState(false),
    [conflict, setConflict] = useState(false),
    [stageUnknown, setStageUnknown] = useState(false),
    [notice, setNotice] = useState(''),
    [reviewedStorage, setReviewedStorage] = useState<string | null>(null),
    [confirmEnable, setConfirmEnable] = useState<string | null>(null)
  const intent = useRef<Intent | undefined>(undefined),
    lock = useRef(false),
    alive = useRef(true),
    controller = useRef<AbortController | undefined>(undefined)
  const authority = useRef({ actor, basis, fresh, writeFresh, writable })
  useLayoutEffect(() => {
    authority.current = { actor, basis, fresh, writeFresh, writable }
  }, [actor, basis, fresh, writeFresh, writable])
  const prefix = ['model-creation', 'access', actor, basis] as const
  const storageKey = [...prefix, 'storage'] as const,
    providerKey = [...prefix, 'providers', providerQuery] as const,
    egressKey = [...prefix, 'egresses', egressQuery] as const
  const credentialKey = [...prefix, 'credential', saved?.credential_id ?? ''] as const
  const selectedProviderKey = [...prefix, 'selected-provider', provider?.id ?? ''] as const,
    selectedEgressKey = [...prefix, 'selected-egress', egress?.id ?? ''] as const
  const storage = useQuery({
    queryKey: storageKey,
    queryFn: ({ signal }) => readModelAccessStorage(signal),
    enabled: readable && writable && !saved,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const providers = useInfiniteQuery({
    queryKey: providerKey,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ signal, pageParam }) =>
      listModelAccessProviders(
        { q: providerQuery, ...(pageParam ? { cursor: pageParam } : {}) },
        signal,
      ),
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: readable && !saved && providerKind === 'existing',
    retry: false,
    gcTime: 0,
  })
  const egresses = useInfiniteQuery({
    queryKey: egressKey,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ signal, pageParam }) =>
      readModelAccessEgress(
        { q: egressQuery, ...(pageParam ? { cursor: pageParam } : {}) },
        signal,
      ),
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: readable && writable && !saved,
    retry: false,
    gcTime: 0,
  })
  const credential = useQuery({
    queryKey: credentialKey,
    queryFn: ({ signal }) => readModelAccessCredential(saved!, signal),
    enabled: readable && !!saved,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const selectedProvider = useQuery({
    queryKey: selectedProviderKey,
    queryFn: ({ signal }) => listModelAccessProviders({ exact_id: provider!.id }, signal),
    enabled: readable && !saved && !!provider && providerKind === 'existing',
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const selectedEgress = useQuery({
    queryKey: selectedEgressKey,
    queryFn: ({ signal }) => readModelAccessEgress({ exact_id: egress!.id }, signal),
    enabled: readable && writable && !saved && !!egress && egressMode === 'proxy',
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const revisions = useConnectionQueryRevision([
    sessionKey,
    storageKey,
    providerKey,
    egressKey,
    credentialKey,
    selectedProviderKey,
    selectedEgressKey,
  ])
  const current = (key: readonly unknown[]) => {
    const state = cache.getQueryState(key)
    return state?.status === 'success' && state.fetchStatus === 'idle' && !state.isInvalidated
  }
  const currentAuthority = () =>
    alive.current && authority.current.actor === actor && authority.current.fresh()
  const currentWriteAuthority = () =>
    currentAuthority() &&
    authority.current.basis === basis &&
    authority.current.writable &&
    authority.current.writeFresh()
  // Read state synchronously at the dispatch boundary: invalidation and a new
  // successful generation can arrive before React updates an enabled button.
  // A retry reconciles captured IDs; later option availability cannot replace
  // or silently abandon an already dispatched uncertain intent.
  const liveCreationReady = (retry: boolean) =>
    currentWriteAuthority() &&
    revisions.snapshot() === revisions.revision &&
    current(storageKey) &&
    current(egressKey) &&
    (providerKind === 'new' || current(providerKey)) &&
    (retry ||
      ((providerKind === 'new' ||
        (current(selectedProviderKey) &&
          cache
            .getQueryData<NonNullable<typeof selectedProvider.data>>(selectedProviderKey)
            ?.items.some((item) => item.id === provider?.id) === true)) &&
        (egressMode !== 'proxy' ||
          (current(selectedEgressKey) &&
            cache
              .getQueryData<NonNullable<typeof selectedEgress.data>>(selectedEgressKey)
              ?.items.some((item) => item.id === egress?.id && item.enabled) === true))))
  const optionsReady =
    readable && fresh() && current(egressKey) && (providerKind === 'new' || current(providerKey))
  const ready = optionsReady && writable && current(storageKey)
  const displaySaved = readable && fresh() && saved
  const credentialCurrent = !!displaySaved && current(credentialKey)
  const staleStorage = reviewedStorage !== null && reviewedStorage !== storage.data?.etag
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      controller.current?.abort()
      intent.current = undefined
    }
  }, [])
  useLayoutEffect(() => {
    onInteractionLockChange(busy || uncertain)
    return () => onInteractionLockChange(false)
  }, [busy, uncertain, onInteractionLockChange])
  // Only the first successful source read initializes review. A later change
  // requires the explicit Review action and cannot rewrite an uncertain intent.
  if (ready && reviewedStorage === null && storage.data) setReviewedStorage(storage.data.etag)
  const capturedCurrent = (capturedBasis: string, stamp: string) =>
    currentAuthority() &&
    authority.current.basis === capturedBasis &&
    revisions.snapshot() === stamp
  const csrf = () => cache.getQueryData<Session>(sessionKey)?.csrf_token ?? ''
  async function reviewStorage() {
    if (lock.current || uncertain || !currentAuthority() || !authority.current.writable) return
    const capturedBasis = basis
    lock.current = true
    onInteractionLockChange(true)
    setBusy(true)
    try {
      const result = await storage.refetch()
      if (
        !alive.current ||
        authority.current.basis !== capturedBasis ||
        !currentAuthority() ||
        !authority.current.writable ||
        !result.isSuccess ||
        !current(storageKey)
      )
        return
      setReviewedStorage(result.data.etag)
      setConflict(false)
      setNotice('')
    } finally {
      lock.current = false
      if (alive.current) {
        setBusy(false)
        onInteractionLockChange(false)
      }
    }
  }
  async function create(retry = false) {
    if (
      lock.current ||
      !liveCreationReady(retry) ||
      (!retry &&
        (uncertain ||
          conflict ||
          staleStorage ||
          reviewedStorage !== cache.getQueryData<CredentialStorageContext>(storageKey)?.etag)) ||
      (retry && (!uncertain || !intent.current))
    )
      return
    if (!retry) {
      try {
        const name = providerName.trim()
        const targetProvider =
          providerKind === 'existing'
            ? selectedProvider.data?.items.find((item) => item.id === provider?.id)
            : undefined
        const connection =
          connectionName.trim() ||
          Array.from(t('access.defaultConnection', { provider: targetProvider?.name ?? name }))
            .slice(0, 100)
            .join('')
        const input: ModelAccessInput = {
          request_id: crypto.randomUUID(),
          storage_policy_etag: reviewedStorage!,
          name: providerKind === 'new' ? name : connection,
          ...(providerKind === 'new' ? { connection_name: connection } : {}),
          credential_name: credentialName.trim() || t('access.defaultCredential'),
          base_url: baseURL.trim(),
          protocol,
          adapter,
          api_version: adapter === 'native' ? null : apiVersion,
          egress_mode: egressMode,
          egress_id: egressMode === 'proxy' ? (egress?.id ?? null) : null,
          secret,
        }
        if (
          providerKind === 'existing' &&
          (!provider ||
            !current(selectedProviderKey) ||
            !selectedProvider.data?.items.some((item) => item.id === provider.id))
        )
          throw new Error()
        if (
          egressMode === 'proxy' &&
          (!egress ||
            !current(selectedEgressKey) ||
            !selectedEgress.data?.items.some((item) => item.id === egress.id && item.enabled))
        )
          throw new Error()
        validateModelAccessInput(input, providerKind === 'existing' ? provider?.id : undefined)
        intent.current = {
          input,
          source: storage.data!.storage_source,
          ...(providerKind === 'existing' ? { provider: targetProvider } : {}),
        }
      } catch {
        setNotice('access.invalid')
        return
      }
    }
    // Constructing/validating an intent is not authority to send it. Preserve
    // an original uncertain request, but abandon an undispatched local intent.
    if (!liveCreationReady(retry)) {
      if (!retry) intent.current = undefined
      return
    }
    const captured = intent.current!
    const capturedBasis = basis,
      stamp = revisions.snapshot()
    let retainLock = false
    lock.current = true
    setBusy(true)
    setNotice('')
    const abort = new AbortController()
    controller.current = abort
    try {
      retainLock = true
      onInteractionLockChange(true)
      const value = await createModelAccess(
        captured.input,
        captured.source,
        csrf(),
        captured.provider,
        abort.signal,
      )
      if (!capturedCurrent(capturedBasis, stamp)) {
        if (alive.current) {
          setUncertain(true)
          setNotice('access.unknown')
        }
        return
      }
      retainLock = false
      intent.current = undefined
      setSecret('')
      setUncertain(false)
      setSaved(value)
      onSaved(value)
      setNotice('access.saved')
    } catch (error) {
      if (!alive.current) return
      const status = error instanceof ModelAccessError ? error.status : 0
      if (retry || !status || status >= 500 || !capturedCurrent(capturedBasis, stamp)) {
        setUncertain(true)
        setNotice('access.unknown')
      } else {
        retainLock = false
        intent.current = undefined
        setConflict(status === 409)
        setNotice(status === 409 ? 'access.conflict' : 'access.failed')
      }
    } finally {
      lock.current = false
      if (alive.current) {
        setBusy(false)
        onInteractionLockChange(retainLock)
      }
    }
  }
  async function operation(kind: 'verify' | 'enable') {
    if (
      lock.current ||
      stageUnknown ||
      !saved ||
      !currentWriteAuthority() ||
      revisions.snapshot() !== revisions.revision ||
      !current(credentialKey)
    )
      return
    if (
      kind === 'enable' &&
      (credential.data?.verification_status !== 'verified' ||
        credential.data.enabled ||
        confirmEnable !== `${basis}:${revisions.snapshot()}`)
    )
      return
    const capturedBasis = basis,
      stamp = revisions.snapshot()
    lock.current = true
    onInteractionLockChange(true)
    setBusy(true)
    setNotice('')
    setConfirmEnable(null)
    const abort = new AbortController()
    controller.current = abort
    try {
      const verified =
        kind === 'verify'
          ? await verifyModelAccess(saved, csrf(), abort.signal)
          : (await enableModelAccess(saved, csrf(), abort.signal), true)
      if (!capturedCurrent(capturedBasis, stamp)) {
        if (alive.current) {
          setStageUnknown(true)
          setNotice('access.stageUnknown')
        }
        return
      }
      setNotice(
        kind === 'verify'
          ? verified
            ? 'access.verified'
            : 'access.verificationFailed'
          : 'access.enabled',
      )
      // Only stored reads follow an explicit write. Never chain Verify/Enable.
      void cache.invalidateQueries({ queryKey: ['model-creation', actor, saved.connection_id] })
      void credential.refetch()
    } catch {
      if (alive.current) {
        setStageUnknown(true)
        setNotice('access.stageUnknown')
      }
    } finally {
      lock.current = false
      if (alive.current) {
        setBusy(false)
        onInteractionLockChange(false)
      }
    }
  }
  const field = (title: string, control: React.ReactNode) => (
    <label className="grid gap-3 text-sm sm:grid-cols-[180px_1fr]">
      <span>{t(title)}</span>
      {control}
    </label>
  )
  if (saved)
    return (
      <div className="space-y-4">
        <p role="status">{t('access.retained')}</p>
        {displaySaved && (
          <>
            <ModelAccessSummary saved={saved} />
            <p>
              {t(
                saved.connection_enabled ? 'access.connectionEnabled' : 'access.connectionDisabled',
              )}
            </p>
            {!credentialCurrent ? (
              <p role="status">{t(credential.isError ? 'access.factsUnavailable' : 'loading')}</p>
            ) : (
              <>
                <p role="status">
                  {t(
                    credential.data!.verification_status === 'failed'
                      ? 'access.failedStatus'
                      : `access.${credential.data!.verification_status}`,
                  )}{' '}
                  ·{' '}
                  {t(
                    credential.data!.enabled
                      ? 'access.credentialEnabled'
                      : 'access.credentialDisabled',
                  )}
                </p>
                <div className="flex flex-wrap gap-2">
                  <Button
                    disabled={busy || stageUnknown || !writable}
                    onClick={() => void operation('verify')}
                  >
                    {t('access.verify')}
                  </Button>
                  {!credential.data!.enabled && (
                    <Button
                      disabled={
                        busy || !writable || credential.data!.verification_status !== 'verified'
                      }
                      onClick={() => {
                        if (currentAuthority() && current(credentialKey))
                          setConfirmEnable(`${basis}:${revisions.snapshot()}`)
                      }}
                    >
                      {t('access.enable')}
                    </Button>
                  )}
                </div>
              </>
            )}
            {saved.adapter === 'azure_openai_classic' && (
              <p>
                {t('access.azureCoverage')}{' '}
                <Link
                  className="underline"
                  to={`/admin/providers/${saved.provider_id}?tab=credentials`}
                >
                  {t('providerWorkspace')}
                </Link>
              </p>
            )}
          </>
        )}
        <Button
          variant="outline"
          disabled={busy || !readable}
          onClick={() => {
            if (currentAuthority()) {
              const capturedBasis = basis
              void credential.refetch().then((result) => {
                if (
                  alive.current &&
                  authority.current.basis === capturedBasis &&
                  currentAuthority() &&
                  result.isSuccess &&
                  current(credentialKey)
                ) {
                  setStageUnknown(false)
                  setNotice('')
                }
              })
            }
          }}
        >
          {t('access.refreshFacts')}
        </Button>
        {notice && <p role="status">{t(notice)}</p>}
        <Dialog
          open={
            confirmEnable === `${basis}:${revisions.snapshot()}` &&
            credentialCurrent &&
            !stageUnknown &&
            writable &&
            credential.data?.verification_status === 'verified' &&
            !credential.data.enabled
          }
          title={t('access.enableTitle')}
          description={t('access.enableDescription')}
          onOpenChange={(open) => {
            if (!open) setConfirmEnable(null)
          }}
        >
          <Button disabled={busy} onClick={() => void operation('enable')}>
            {t('access.confirmEnable')}
          </Button>
        </Dialog>
      </div>
    )
  const disabled = busy || uncertain || !ready || staleStorage || conflict
  const selectedReady =
    (providerKind === 'new' ||
      (current(selectedProviderKey) &&
        selectedProvider.data?.items.some((item) => item.id === provider?.id) === true)) &&
    (egressMode !== 'proxy' ||
      (current(selectedEgressKey) &&
        selectedEgress.data?.items.some((item) => item.id === egress?.id && item.enabled) === true))
  return (
    <div className="space-y-5">
      <p>{t('access.staged')}</p>
      {!ready && (
        <p role="status">
          {t(
            !readable || !writable
              ? 'denied'
              : storage.isError || egresses.isError || providers.isError
                ? 'access.factsUnavailable'
                : 'loading',
          )}
        </p>
      )}
      {ready && (
        <p>{t('access.source', { source: t(`access.${storage.data!.storage_source}`) })}</p>
      )}
      <fieldset
        disabled={disabled}
        hidden={!readable || !fresh() || !optionsReady || !current(storageKey)}
        className="space-y-5"
      >
        {field(
          'access.providerMode',
          <select
            className={selectClass}
            value={providerKind}
            onChange={(event) => {
              setProviderKind(event.target.value as 'new' | 'existing')
              setProvider(undefined)
            }}
          >
            <option value="new">{t('access.newProvider')}</option>
            <option value="existing">{t('access.existingProvider')}</option>
          </select>,
        )}
        {providerKind === 'new' ? (
          field(
            'access.providerName',
            <Input
              value={providerName}
              onChange={(event) => setProviderName(event.target.value)}
              maxLength={100}
              required
            />,
          )
        ) : (
          <>
            {field(
              'access.providerSearch',
              <Input
                value={providerQuery}
                onChange={(event) => setProviderQuery(event.target.value)}
                maxLength={200}
              />,
            )}
            {field(
              'provider',
              <select
                className={selectClass}
                value={provider?.id ?? ''}
                onChange={(event) =>
                  setProvider(
                    providers.data?.pages
                      .flatMap((page) => page.items)
                      .find((item) => item.id === event.target.value),
                  )
                }
              >
                <option value="">{t('access.chooseProvider')}</option>
                {provider &&
                  !providers.data?.pages.some((page) =>
                    page.items.some((item) => item.id === provider.id),
                  ) && <option value={provider.id}>{provider.name}</option>}
                {providers.data?.pages
                  .flatMap((page) => page.items)
                  .map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.name} · {item.id}
                    </option>
                  ))}
              </select>,
            )}
            {providers.hasNextPage && (
              <Button
                variant="outline"
                disabled={providers.isFetching}
                onClick={() => void providers.fetchNextPage()}
              >
                {t('loadMore')}
              </Button>
            )}
          </>
        )}
        {field(
          'access.adapter',
          <select
            className={selectClass}
            value={adapter}
            onChange={(event) => {
              const next = event.target.value as typeof adapter
              setAdapter(next)
              setAPIVersion('')
              if (next === 'azure_openai_classic') setProtocol('openai_chat')
            }}
          >
            <option value="native">{t('access.native')}</option>
            <option value="azure_openai_classic">{t('access.azureClassic')}</option>
          </select>,
        )}
        {field(
          'protocol',
          <select
            className={selectClass}
            value={protocol}
            onChange={(event) => setProtocol(event.target.value as ModelCreationProtocol)}
          >
            {protocols.map((value) => (
              <option
                key={value}
                value={value}
                disabled={adapter === 'azure_openai_classic' && value !== 'openai_chat'}
              >
                {value}
              </option>
            ))}
          </select>,
        )}
        {adapter === 'azure_openai_classic' &&
          field(
            'access.apiVersion',
            <Input
              value={apiVersion}
              onChange={(event) => setAPIVersion(event.target.value)}
              required
              maxLength={18}
            />,
          )}
        {field(
          'baseURL',
          <Input
            value={baseURL}
            onChange={(event) => setBaseURL(event.target.value)}
            required
            type="url"
            maxLength={2048}
          />,
        )}
        {field(
          'access.secret',
          <Input
            value={secret}
            onChange={(event) => setSecret(event.target.value)}
            required
            type="password"
            autoComplete="new-password"
          />,
        )}
        {field(
          'access.connectionName',
          <Input
            value={connectionName}
            onChange={(event) => setConnectionName(event.target.value)}
            maxLength={100}
            placeholder={t('access.defaultConnection', {
              provider: provider?.name ?? providerName,
            })}
          />,
        )}
        {field(
          'access.credentialName',
          <Input
            value={credentialName}
            onChange={(event) => setCredentialName(event.target.value)}
            maxLength={100}
            placeholder={t('access.defaultCredential')}
          />,
        )}
        {field(
          'access.egress',
          <select
            className={selectClass}
            value={egressMode === 'proxy' ? `proxy:${egress?.id ?? ''}` : egressMode}
            onChange={(event) => {
              const value = event.target.value
              setEgressMode(value.startsWith('proxy:') ? 'proxy' : (value as 'default' | 'direct'))
              setEgress(
                egresses.data?.pages
                  .flatMap((page) => page.items)
                  .find((item) => `proxy:${item.id}` === value),
              )
            }}
          >
            <option value="default">{t('access.defaultEgress')}</option>
            <option value="direct">{t('access.directEgress')}</option>
            {egress &&
              !egresses.data?.pages.some((page) =>
                page.items.some((item) => item.id === egress.id),
              ) && <option value={`proxy:${egress.id}`}>{egress.name}</option>}
            {egresses.data?.pages
              .flatMap((page) => page.items)
              .map((item) => (
                <option key={item.id} value={`proxy:${item.id}`} disabled={!item.enabled}>
                  {item.name}
                </option>
              ))}
          </select>,
        )}
        {field(
          'access.egressSearch',
          <Input
            value={egressQuery}
            onChange={(event) => setEgressQuery(event.target.value)}
            maxLength={200}
          />,
        )}
        {egresses.hasNextPage && (
          <Button
            variant="outline"
            disabled={egresses.isFetching}
            onClick={() => void egresses.fetchNextPage()}
          >
            {t('loadMore')}
          </Button>
        )}
      </fieldset>
      {staleStorage && !uncertain && <p role="alert">{t('access.policyChanged')}</p>}
      {notice && <p role="status">{t(notice)}</p>}
      <div className="flex flex-wrap gap-2">
        <Button disabled={disabled || !selectedReady} onClick={() => void create()}>
          {t('access.create')}
        </Button>
        {uncertain && (
          <Button disabled={!ready || busy || !writable} onClick={() => void create(true)}>
            {t('access.retry')}
          </Button>
        )}
        <Button
          variant="outline"
          disabled={busy || !readable}
          onClick={() => {
            if (!currentAuthority()) return
            void storage.refetch()
            void egresses.refetch()
            if (providerKind === 'existing') void providers.refetch()
          }}
        >
          {t('access.refreshFacts')}
        </Button>
        {(staleStorage || conflict) && !uncertain && (
          <Button variant="outline" disabled={!ready || busy} onClick={() => void reviewStorage()}>
            {t('access.reviewPolicy')}
          </Button>
        )}
        {uncertain && (
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => {
              intent.current = undefined
              setSecret('')
              setUncertain(false)
              setNotice('access.discarded')
            }}
          >
            {t('access.discard')}
          </Button>
        )}
      </div>
    </div>
  )
}
