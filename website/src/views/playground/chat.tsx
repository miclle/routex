import type { SnippetInput } from '@/lib/playground-snippet'
import CodeDialog from './code-dialog'
import { AttachmentChips, AttachmentPicker } from './attachments'
import { isGeminiModelName, protocolLabel } from '@/lib/protocols'
import {
  buildChatAttachmentContent,
  buildGeminiAttachmentParts,
  buildMessagesAttachmentContent,
  buildResponsesAttachmentContent,
} from '@/lib/playground-attachments'
import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Code, Copy, LoaderCircle, Send, Square, Trash2 } from 'lucide-react'
import { AttachmentError, deleteAttachment, uploadAttachment } from '@/api/attachments'
import {
  GatewayError,
  getGatewayModels,
  runChat,
  runResponses,
  runMessages,
  runGemini,
} from '@/api/playground'
import type {
  ChatCurrentTurnMessage,
  ChatMessage,
  ChatResult,
  GatewayModel,
  MessagesCurrentTurnMessage,
  MessagesHistoryMessage,
  PlaygroundProtocol,
  ResponsesCurrentTurnItem,
  ResponsesHistoryItem,
  ResponseStatus,
} from '@/types/playground'
import type { Attachment, AttachmentTarget } from '@/types/attachments'
import { useSession } from '@/hooks/use-auth'
import { FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'

type Exchange = ChatResult & {
  id: string
  prompt: string
  attachments: string[]
  model: string
  status:
    | 'running'
    | 'completed'
    | 'cancelled'
    | 'failed'
    | 'incomplete'
    | 'accepted'
    | 'handoff'
    | 'refused'
  responseStatus?: ResponseStatus | null
  nonTextOutput?: boolean
  error: string | GatewayError
}

const maxAttachmentBytes = 2 << 20
const maxAttachments = 4

function attachmentCapability(file: File): 'image' | 'pdf' | null {
  const name = file.name.toLowerCase()
  if (
    file.type === 'image/png' ||
    file.type === 'image/jpeg' ||
    name.endsWith('.png') ||
    name.endsWith('.jpg') ||
    name.endsWith('.jpeg')
  )
    return 'image'
  if (file.type === 'application/pdf' || name.endsWith('.pdf')) return 'pdf'
  return null
}

function uploadScopedAttachment(file: File, csrf: string, target: AttachmentTarget) {
  return target.scope === 'user'
    ? uploadAttachment(file, csrf)
    : uploadAttachment(file, csrf, undefined, target)
}

function deleteScopedAttachment(id: string, csrf: string, target: AttachmentTarget) {
  return target.scope === 'user'
    ? deleteAttachment(id, csrf)
    : deleteAttachment(id, csrf, undefined, target)
}

export default function ChatWorkbench({ projectId = '' }: { projectId?: string }) {
  useTranslation()

  const session = useSession()
  const [streamEnabled, setStreamEnabled] = useState(true)
  const [codeRequest, setCodeRequest] = useState<SnippetInput | null>(null)
  const [key, setKey] = useState('')
  const [models, setModels] = useState<GatewayModel[]>([])
  const [model, setModel] = useState('')
  const [protocol, setProtocol] = useState<PlaygroundProtocol>('openai_chat')
  const formRef = useRef<HTMLFormElement>(null)
  const lock = useRef(false)
  function protocols(item?: GatewayModel): PlaygroundProtocol[] {
    return (item?.protocols ?? ['openai_chat']).filter(
      (value): value is PlaygroundProtocol =>
        value === 'openai_chat' ||
        value === 'openai_responses' ||
        value === 'anthropic_messages' ||
        value === 'gemini_generate_content',
    )
  }
  const availableProtocols = protocols(models.find((item) => item.id === model))
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | GatewayError>('')
  const [keyChecked, setKeyChecked] = useState(false)
  const [exchanges, setExchanges] = useState<Exchange[]>([])
  const [prompt, setPrompt] = useState('')
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const [uploadingNames, setUploadingNames] = useState<string[]>([])
  const [copied, setCopied] = useState('')
  const controller = useRef<AbortController | null>(null)
  const attachmentGeneration = useRef(0)
  const attachmentRef = useRef<Attachment[]>([])
  const ownedAttachmentTargets = useRef(new Map<string, AttachmentTarget>())
  const csrfRef = useRef('')
  const mounted = useRef(true)
  const selectedModel = models.find((item) => item.id === model)
  const attachmentTarget: AttachmentTarget | null =
    selectedModel?.attachment_scope === 'user' && !projectId
      ? { scope: 'user' }
      : selectedModel?.attachment_scope === 'project' &&
          !!selectedModel.attachment_project_id &&
          (!projectId || selectedModel.attachment_project_id === projectId)
        ? { scope: 'project', projectId: selectedModel.attachment_project_id }
        : null
  const inputCapabilities = selectedModel?.input_capabilities?.[protocol] ?? []
  const attachmentAccept = [
    ...(inputCapabilities.includes('image') ? ['image/png', 'image/jpeg'] : []),
    ...(inputCapabilities.includes('pdf') ? ['application/pdf'] : []),
  ].join(',')
  const canAttach = !!attachmentTarget && inputCapabilities.length > 0 && !!session.data
  const requestRunning = exchanges.some((exchange) => exchange.status === 'running')
  const busy = loading || uploadingNames.length > 0 || requestRunning

  function replaceAttachments(update: (current: Attachment[]) => Attachment[]) {
    setAttachments((current) => {
      const next = update(current)
      attachmentRef.current = next
      return next
    })
  }

  async function releaseAttachments(items: Attachment[], csrf = csrfRef.current) {
    if (!csrf) return
    const results = await Promise.allSettled(
      items.map(async (attachment) => {
        const target = ownedAttachmentTargets.current.get(attachment.id)
        if (!target) return
        await deleteScopedAttachment(attachment.id, csrf, target)
        ownedAttachmentTargets.current.delete(attachment.id)
      }),
    )
    if (mounted.current && results.some((result) => result.status === 'rejected'))
      setError('playground:attachmentDeleteFailed')
  }

  function clearDraftAttachments() {
    attachmentGeneration.current += 1
    const current = attachmentRef.current
    attachmentRef.current = []
    setAttachments([])
    setUploadingNames([])
    void releaseAttachments(current)
  }

  useEffect(() => {
    csrfRef.current = session.data?.csrf_token ?? ''
  }, [session.data?.csrf_token])

  useEffect(() => {
    const activeOwnedAttachmentTargets = ownedAttachmentTargets.current
    mounted.current = true
    return () => {
      mounted.current = false
      attachmentGeneration.current += 1
      controller.current?.abort()
      attachmentRef.current = []
      const csrf = csrfRef.current
      const owned = [...activeOwnedAttachmentTargets]
      activeOwnedAttachmentTargets.clear()
      if (csrf)
        for (const [id, target] of owned)
          void deleteScopedAttachment(id, csrf, target).catch(() => undefined)
    }
  }, [])

  async function selectAttachments(files: File[]) {
    if (!session.data || !canAttach || busy) return
    const target = attachmentTarget
    if (!target) return
    setError('')
    const csrf = session.data.csrf_token
    const generation = attachmentGeneration.current
    const knownNames = new Set([
      ...attachmentRef.current.map((attachment) => attachment.name),
      ...uploadingNames,
    ])
    let remaining = maxAttachments - knownNames.size
    for (const file of files) {
      if (!mounted.current || attachmentGeneration.current !== generation) return
      if (knownNames.has(file.name)) {
        setError('playground:attachmentDuplicate')
        continue
      }
      if (remaining <= 0) {
        setError('playground:attachmentLimit')
        break
      }
      const capability = attachmentCapability(file)
      if (!capability || !inputCapabilities.includes(capability)) {
        setError('playground:attachmentType')
        continue
      }
      if (file.size <= 0 || file.size > maxAttachmentBytes) {
        setError('playground:attachmentSize')
        continue
      }
      knownNames.add(file.name)
      remaining -= 1
      setUploadingNames((current) => [...current, file.name])
      try {
        const attachment = await uploadScopedAttachment(file, csrf, target)
        ownedAttachmentTargets.current.set(attachment.id, target)
        if (!mounted.current || attachmentGeneration.current !== generation) {
          await deleteScopedAttachment(attachment.id, csrf, target)
            .then(() => ownedAttachmentTargets.current.delete(attachment.id))
            .catch(() => undefined)
          return
        }
        const storedCapability =
          attachment.mime === 'image/png' || attachment.mime === 'image/jpeg'
            ? 'image'
            : attachment.mime === 'application/pdf'
              ? 'pdf'
              : null
        if (!storedCapability || !inputCapabilities.includes(storedCapability)) {
          setError('playground:attachmentType')
          await deleteScopedAttachment(attachment.id, csrf, target)
            .then(() => ownedAttachmentTargets.current.delete(attachment.id))
            .catch(() => undefined)
          continue
        }
        replaceAttachments((current) => [...current, attachment])
      } catch (failure) {
        if (mounted.current && attachmentGeneration.current === generation) {
          setError(
            failure instanceof AttachmentError && failure.status === 429
              ? 'playground:attachmentStorageLimit'
              : failure instanceof AttachmentError &&
                  failure.status === 404 &&
                  target.scope === 'project'
                ? 'playground:attachmentProjectUnavailable'
                : failure instanceof AttachmentError && failure.status === 503
                  ? 'playground:attachmentStorageUnavailable'
                  : 'playground:attachmentUploadFailed',
          )
        }
      } finally {
        if (mounted.current && attachmentGeneration.current === generation)
          setUploadingNames((current) => current.filter((name) => name !== file.name))
      }
    }
  }

  function removeAttachment(attachment: Attachment) {
    replaceAttachments((current) => current.filter((item) => item.id !== attachment.id))
    const target = ownedAttachmentTargets.current.get(attachment.id)
    if (!target) return
    void deleteScopedAttachment(attachment.id, csrfRef.current, target)
      .then(() => ownedAttachmentTargets.current.delete(attachment.id))
      .catch(() => {
        if (mounted.current) setError('playground:attachmentDeleteFailed')
      })
  }

  function changeKey(value: string) {
    clearDraftAttachments()
    setKey(value)
    setModels([])
    setModel('')
    setKeyChecked(false)
    setError('')
    setExchanges([])
  }
  async function loadModels() {
    if (!key.trim() || busy || lock.current) return
    lock.current = true
    const abort = new AbortController()
    controller.current = abort
    setLoading(true)
    setError('')
    try {
      const available = await getGatewayModels(key.trim(), abort.signal)
      const verifiedScope = available[0]?.attachment_scope
      const verifiedProjectId = available[0]?.attachment_project_id ?? ''
      if (
        mounted.current &&
        projectId &&
        available.length > 0 &&
        (verifiedScope !== 'project' || verifiedProjectId !== projectId)
      ) {
        clearDraftAttachments()
        setModels([])
        setModel('')
        setKeyChecked(false)
        setExchanges([])
        setError('playground:attachmentProjectMismatch')
        return
      }
      const items = available.filter((item) => protocols(item).length > 0)
      if (mounted.current) {
        setModels(items)
        setModel(items[0]?.id ?? '')
        setProtocol(protocols(items[0])[0] ?? 'openai_chat')
        setKeyChecked(true)
      }
    } catch (failure) {
      if (mounted.current && !abort.signal.aborted)
        setError(
          failure instanceof GatewayError
            ? failure
            : 'unable_to_connect_to_the_gateway_check_your_bda61',
        )
    } finally {
      lock.current = false
      if (mounted.current) setLoading(false)
      if (controller.current === abort) controller.current = null
    }
  }
  async function send(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (
      busy ||
      lock.current ||
      !prompt.trim() ||
      !model ||
      !key.trim() ||
      !availableProtocols.includes(protocol)
    )
      return
    lock.current = true
    const form = new FormData(event.currentTarget)
    const text = prompt.trim()
    const submittedAttachments = attachmentRef.current
    attachmentGeneration.current += 1
    attachmentRef.current = []
    setAttachments([])
    const system = String(form.get('system') ?? '').trim()
    const stream = form.get('stream') === 'on'
    const messages: ChatMessage[] = [
      ...(system ? [{ role: 'system' as const, content: system }] : []),
      ...exchanges
        .filter((exchange) => exchange.status === 'completed')
        .flatMap((exchange): ChatMessage[] => [
          { role: 'user', content: exchange.prompt },
          { role: 'assistant', content: exchange.text },
        ]),
    ]
    const id = crypto.randomUUID()
    const abort = new AbortController()
    controller.current = abort
    setCopied('')
    setError('')
    setPrompt('')
    setExchanges((current) => [
      ...current,
      {
        id,
        prompt: text,
        attachments: submittedAttachments.map((attachment) => attachment.name),
        model,
        status: 'running',
        text: '',
        requestId: '',
        usage: null,
        finishReason: null,
        error: '',
      },
    ])
    const update = (patch: Partial<Exchange>) => {
      if (mounted.current)
        setExchanges((current) =>
          current.map((exchange) => (exchange.id === id ? { ...exchange, ...patch } : exchange)),
        )
    }
    try {
      const parameters = {
        model,
        stream,
        temperature: Number(form.get('temperature')),
        top_p: Number(form.get('top_p')),
      }
      if (protocol === 'gemini_generate_content') {
        const result = await runGemini(
          key.trim(),
          {
            model,
            stream,
            contents: [
              ...messages
                .filter((message) => message.role !== 'system')
                .map((message) => ({
                  role: message.role === 'assistant' ? ('model' as const) : ('user' as const),
                  parts: [{ text: message.content }],
                })),
              {
                role: 'user' as const,
                parts: buildGeminiAttachmentParts(text, submittedAttachments),
              },
            ],
            ...(system ? { systemInstruction: { parts: [{ text: system }] } } : {}),
            generationConfig: {
              temperature: parameters.temperature,
              topP: parameters.top_p,
              maxOutputTokens: Number(form.get('max_tokens')),
              candidateCount: 1,
            },
          },
          abort.signal,
          update,
        )
        update({ ...result, status: result.generationStatus })
      } else if (protocol === 'anthropic_messages') {
        const currentMessage: MessagesHistoryMessage | MessagesCurrentTurnMessage =
          submittedAttachments.length > 0
            ? {
                role: 'user',
                content: buildMessagesAttachmentContent(text, submittedAttachments),
              }
            : { role: 'user', content: text }
        const result = await runMessages(
          key.trim(),
          {
            ...parameters,
            messages: [
              ...messages.filter(
                (message): message is ChatMessage & { role: 'user' | 'assistant' } =>
                  message.role !== 'system',
              ),
              currentMessage,
            ],
            ...(system ? { system } : {}),
            max_tokens: Number(form.get('max_tokens')),
          },
          abort.signal,
          update,
        )
        update({ ...result, status: result.messageStatus })
      } else if (protocol === 'openai_responses') {
        const currentItem: ResponsesHistoryItem | ResponsesCurrentTurnItem =
          submittedAttachments.length > 0
            ? {
                role: 'user',
                content: buildResponsesAttachmentContent(text, submittedAttachments),
              }
            : { role: 'user', content: text }
        const result = await runResponses(
          key.trim(),
          {
            ...parameters,
            input: [
              ...messages.filter(
                (message): message is ChatMessage & { role: 'user' | 'assistant' } =>
                  message.role !== 'system',
              ),
              currentItem,
            ],
            ...(system ? { instructions: system } : {}),
            max_output_tokens: Number(form.get('max_tokens')),
          },
          abort.signal,
          update,
        )
        update({
          ...result,
          status:
            result.responseStatus === 'completed'
              ? 'completed'
              : result.responseStatus === 'failed'
                ? 'failed'
                : result.responseStatus === 'incomplete'
                  ? 'incomplete'
                  : 'accepted',
          error: result.responseStatus === 'failed' ? 'playground:failedHelp' : '',
        })
      } else {
        const currentMessage: ChatMessage | ChatCurrentTurnMessage =
          submittedAttachments.length > 0
            ? {
                role: 'user',
                content: buildChatAttachmentContent(text, submittedAttachments),
              }
            : { role: 'user', content: text }
        const result = await runChat(
          key.trim(),
          {
            ...parameters,
            messages: [...messages, currentMessage],
            max_completion_tokens: Number(form.get('max_tokens')),
            ...(stream ? { stream_options: { include_usage: true } } : {}),
          },
          abort.signal,
          update,
        )
        update({ ...result, status: 'completed' })
      }
    } catch (failure) {
      if (abort.signal.aborted) update({ status: 'cancelled', error: '' })
      else
        update({
          status: 'failed',
          error:
            failure instanceof GatewayError
              ? failure
              : 'the_request_failed_received_content_has_been_preserved_4fb4b',
          ...(failure instanceof GatewayError && failure.requestId
            ? { requestId: failure.requestId }
            : {}),
        })
    } finally {
      lock.current = false
      if (controller.current === abort) controller.current = null
      void releaseAttachments(submittedAttachments, csrfRef.current)
    }
  }
  function showCode() {
    if (
      !formRef.current ||
      !availableProtocols.includes(protocol) ||
      busy ||
      attachments.length > 0
    )
      return
    const form = new FormData(formRef.current)
    setCodeRequest({
      origin: window.location.origin,
      protocol,
      model,
      stream: streamEnabled,
      temperature: Number(form.get('temperature')),
      topP: Number(form.get('top_p')),
      maxTokens: Number(form.get('max_tokens')),
      system: String(form.get('system') ?? ''),
      messages: [
        ...exchanges
          .filter((exchange) => exchange.status === 'completed')
          .flatMap((exchange) => [
            { role: 'user' as const, content: exchange.prompt },
            { role: 'assistant' as const, content: exchange.text },
          ]),
        { role: 'user', content: prompt.trim() || t('playground:codePromptPlaceholder') },
      ],
    })
  }
  async function copy(exchange: Exchange) {
    try {
      await navigator.clipboard.writeText(exchange.text)
      setCopied(exchange.id)
    } catch {
      setError('copy_failed_select_and_copy_the_output_manually_32d26')
    }
  }
  return (
    <>
      {error && (
        <p
          role="alert"
          className="rounded-md border border-destructive/30 p-3 text-sm text-destructive"
        >
          {error instanceof GatewayError ? error.message : t(error)}
        </p>
      )}
      <form
        ref={formRef}
        onSubmit={(event) => void send(event)}
        className="flex min-h-[640px] min-w-[980px] overflow-hidden rounded-lg border"
        style={{ height: 'calc(100vh - 190px)' }}
      >
        <aside className="w-80 shrink-0 space-y-5 overflow-auto border-r bg-muted/30 p-5">
          <fieldset disabled={busy} className="space-y-5">
            <FormField label={t('playground:key')}>
              <Input
                name="api_key"
                type="password"
                autoComplete="off"
                value={key}
                onValueChange={changeKey}
                placeholder={t('playground:keyPlaceholder')}
              />
            </FormField>
            <div className="flex flex-wrap gap-2">
              <Button
                size="sm"
                variant="outline"
                disabled={!key.trim() || busy}
                onClick={() => void loadModels()}
              >
                {loading ? t('verifying_36a20') : t('verify_and_load_models_ea3bf')}
              </Button>
              <Button
                size="sm"
                variant="ghost"
                disabled={!key || busy}
                onClick={() => changeKey('')}
              >
                {t('clear_key_b9655')}
              </Button>
            </div>
            <FormField label={t('choose_a_model_4e769')}>
              <select
                name="model"
                aria-label={t('choose_a_model_4e769')}
                value={model}
                onChange={(e) => {
                  clearDraftAttachments()
                  setModel(e.target.value)
                  setProtocol(
                    protocols(models.find((item) => item.id === e.target.value))[0] ??
                      'openai_chat',
                  )
                  setExchanges([])
                }}
                className="h-11 w-full rounded-md border bg-background px-3 text-sm"
                disabled={!models.length}
              >
                <option value="">
                  {models.length
                    ? t('choose_a_model_4e769')
                    : keyChecked
                      ? t('no_callable_models_d38ff')
                      : t('verify_your_key_first_5592a')}
                </option>
                {models.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.id}
                  </option>
                ))}
              </select>
            </FormField>
            {model && (
              <FormField label={t('playground:protocol')}>
                <select
                  name="protocol"
                  aria-label={t('playground:protocol')}
                  value={protocol}
                  className="h-11 w-full rounded-md border bg-background px-3 text-sm"
                  onChange={(event) => {
                    clearDraftAttachments()
                    setProtocol(event.target.value as PlaygroundProtocol)
                    setExchanges([])
                    setCopied('')
                  }}
                >
                  {availableProtocols.map((value) => (
                    <option key={value} value={value}>
                      {protocolLabel(value)}
                    </option>
                  ))}
                </select>
              </FormField>
            )}
            {keyChecked && models.length === 0 && (
              <p role="status" className="text-xs leading-5 text-muted-foreground">
                {t('this_key_has_no_available_models_check_its_45322')}
              </p>
            )}
            <div className="border-t pt-4">
              <Badge variant="outline">{protocolLabel(protocol)}</Badge>
              <p className="mt-2 break-all font-mono text-xs text-muted-foreground">
                POST{' '}
                {protocol === 'gemini_generate_content'
                  ? isGeminiModelName(model)
                    ? `/v1beta/models/${encodeURIComponent(model)}:${streamEnabled ? 'streamGenerateContent' : 'generateContent'}`
                    : '—'
                  : protocol === 'anthropic_messages'
                    ? '/v1/messages'
                    : protocol === 'openai_chat'
                      ? '/v1/chat/completions'
                      : '/v1/responses'}
              </p>
            </div>
            {protocol === 'gemini_generate_content' && !isGeminiModelName(model) && (
              <p role="status" className="text-sm text-muted-foreground">
                {t('playground:geminiAliasRequired')}
              </p>
            )}
            <FormField label={t('temperature')}>
              <Input
                name="temperature"
                type="number"
                min={0}
                max={protocol === 'anthropic_messages' ? 1 : 2}
                step={0.1}
                defaultValue={0.7}
                required
              />
            </FormField>
            <FormField label={t('topP')}>
              <Input
                name="top_p"
                type="number"
                min={0}
                max={1}
                step={0.05}
                defaultValue={1}
                required
              />
            </FormField>
            <FormField label={t('maximum_output_tokens_1863d')}>
              <Input
                name="max_tokens"
                type="number"
                min={protocol === 'anthropic_messages' ? 0 : 1}
                max={32768}
                step={1}
                defaultValue={2048}
                required
              />
            </FormField>
            <FormField label={t('systemPrompt')}>
              <Textarea
                name="system"
                placeholder={t('optional_describe_the_response_style_or_task_context_5fc2f')}
              />
            </FormField>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                name="stream"
                checked={streamEnabled}
                onChange={(event) => setStreamEnabled(event.target.checked)}
              />
              {t('stream_output_5b6aa')}
            </label>
          </fieldset>
        </aside>
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden bg-card">
          <div className="flex items-center justify-between border-b px-5 py-3">
            <h2 className="text-sm font-medium">{t('model_conversation_9e016')}</h2>
            <div className="flex items-center gap-2">
              <Button
                variant="ghost"
                size="sm"
                disabled={busy || !exchanges.length}
                onClick={() => {
                  clearDraftAttachments()
                  setExchanges([])
                  setCopied('')
                }}
              >
                <Trash2 className="size-3.5" aria-hidden="true" />
                {t('clear_conversation_35c97')}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                disabled={
                  busy || attachments.length > 0 || !model || !availableProtocols.includes(protocol)
                }
                onClick={showCode}
              >
                <Code className="size-3.5" />
                {t('playground:getCode')}
              </Button>
            </div>
          </div>
          <div
            role="log"
            aria-label={t('model_conversation_9e016')}
            aria-live="polite"
            className="min-h-0 flex-1 space-y-6 overflow-auto p-5"
          >
            {exchanges.length === 0 && (
              <div className="flex min-h-60 items-center justify-center text-center text-sm leading-7 text-muted-foreground">
                {t('verify_a_key_and_select_a_model_to_7733d')}
                <br />
                {t('playground:context')}
              </div>
            )}
            {exchanges.map((exchange) => (
              <article key={exchange.id} className="space-y-3">
                <div className="ml-auto max-w-[90%] whitespace-pre-wrap rounded-lg bg-secondary px-4 py-3 text-sm leading-6">
                  {exchange.prompt}
                  {exchange.attachments.length > 0 && (
                    <div className="mt-2 text-xs text-muted-foreground">
                      {exchange.attachments.join(' · ')}
                    </div>
                  )}
                </div>
                <div className="space-y-3 border-l-2 pl-4">
                  <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    <span>{exchange.model}</span>
                    <span>
                      {exchange.status === 'handoff'
                        ? t('playground:handoff')
                        : exchange.status === 'refused'
                          ? t('playground:refused')
                          : exchange.status === 'running'
                            ? t('generating_3a98d')
                            : exchange.status === 'cancelled'
                              ? t('stopped_75ddd')
                              : exchange.status === 'incomplete'
                                ? t('playground:incomplete')
                                : exchange.status === 'accepted'
                                  ? t('playground:accepted')
                                  : exchange.status === 'failed'
                                    ? t('call_failed_1d1d8')
                                    : t('completed_e99b4')}
                    </span>
                  </div>
                  <p className="whitespace-pre-wrap break-words text-sm leading-7">
                    {exchange.text ||
                      (exchange.status === 'running'
                        ? t('waiting_for_a_response_e697e')
                        : t('no_text_content_was_returned_e8ccd'))}
                  </p>
                  {(exchange.status === 'incomplete' ||
                    exchange.status === 'accepted' ||
                    exchange.status === 'handoff' ||
                    exchange.status === 'refused' ||
                    exchange.nonTextOutput) && (
                    <p role="status" className="text-xs text-muted-foreground">
                      {t(
                        exchange.status === 'handoff'
                          ? 'playground:handoffHelp'
                          : exchange.status === 'refused'
                            ? 'playground:refusedHelp'
                            : exchange.status === 'incomplete'
                              ? 'playground:incompleteHelp'
                              : exchange.status === 'accepted'
                                ? 'playground:acceptedHelp'
                                : 'playground:textOnly',
                      )}
                    </p>
                  )}
                  {exchange.error && (
                    <p role="alert" className="text-sm text-destructive">
                      {exchange.error instanceof GatewayError
                        ? exchange.error.message
                        : t(exchange.error)}
                    </p>
                  )}
                  <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                    {exchange.requestId && (
                      <span className="break-all">
                        {t('requestId')}: {exchange.requestId}
                      </span>
                    )}
                    {exchange.usage ? (
                      <span>
                        {t('input_35910')}
                        {exchange.usage.prompt_tokens}
                        {t('output_f8ba7')}
                        {exchange.usage.completion_tokens}
                        {t('total_6ea55')}
                        {exchange.usage.total_tokens} Tokens
                      </span>
                    ) : (
                      exchange.status !== 'running' && (
                        <span>{t('the_upstream_returned_no_usage_data_557c1')}</span>
                      )
                    )}
                    {exchange.text && (
                      <Button size="sm" variant="ghost" onClick={() => void copy(exchange)}>
                        <Copy className="size-3" aria-hidden="true" />
                        {copied === exchange.id ? t('copied_e381a') : t('copy_output_ce470')}
                      </Button>
                    )}
                  </div>
                </div>
              </article>
            ))}
          </div>
          <div className="space-y-3 border-t p-4">
            <AttachmentChips
              attachments={attachments}
              uploadingNames={uploadingNames}
              disabled={busy}
              removeLabel={(name) => t('playground:removeAttachment', { name })}
              uploadingLabel={(name) => t('playground:attachmentUploadingLabel', { name })}
              onRemove={removeAttachment}
            />
            <Textarea
              aria-label={t('message_dc6de')}
              name="prompt"
              rows={4}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              disabled={busy}
              placeholder={t('playground:messagePlaceholder')}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
                  event.preventDefault()
                  event.currentTarget.form?.requestSubmit()
                }
              }}
            />
            <div className="flex items-center justify-between gap-2">
              <AttachmentPicker
                accept={attachmentAccept}
                disabled={busy || !canAttach || attachments.length >= maxAttachments}
                label={
                  canAttach
                    ? t('playground:attachFiles')
                    : attachmentTarget && selectedModel
                      ? t('playground:attachmentUnsupported')
                      : selectedModel?.attachment_scope === 'project' || projectId
                        ? t('playground:attachmentProjectUnavailable')
                        : t('playground:attachmentUnsupported')
                }
                onFiles={(files) => void selectAttachments(files)}
              />
              <div className="flex justify-end gap-2">
                {requestRunning && (
                  <Button variant="outline" onClick={() => controller.current?.abort()}>
                    <Square className="size-4" aria-hidden="true" />
                    {t('stop_generation_76349')}
                  </Button>
                )}
                <Button type="submit" disabled={busy || !model || !prompt.trim()}>
                  {busy ? (
                    <LoaderCircle className="size-4 animate-spin" aria-hidden="true" />
                  ) : (
                    <Send className="size-4" aria-hidden="true" />
                  )}
                  {busy ? t('request_in_progress_fbfed') : t('send_message_94306')}
                </Button>
              </div>
            </div>
          </div>
        </div>
      </form>
      {codeRequest && <CodeDialog request={codeRequest} onClose={() => setCodeRequest(null)} />}
    </>
  )
}
