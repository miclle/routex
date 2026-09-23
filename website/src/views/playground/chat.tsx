import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Copy, LoaderCircle, Send, Square, Trash2 } from 'lucide-react'
import {
  GatewayError,
  getGatewayModels,
  runChat,
  runResponses,
  runMessages,
} from '@/api/playground'
import type {
  ChatMessage,
  ChatResult,
  GatewayModel,
  PlaygroundProtocol,
  ResponseStatus,
} from '@/types/playground'
import { FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'

type Exchange = ChatResult & {
  id: string
  prompt: string
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
export default function ChatWorkbench() {
  useTranslation()

  const [key, setKey] = useState('')
  const [models, setModels] = useState<GatewayModel[]>([])
  const [model, setModel] = useState('')
  const [protocol, setProtocol] = useState<PlaygroundProtocol>('openai_chat')
  const lock = useRef(false)
  function protocols(item?: GatewayModel): PlaygroundProtocol[] {
    return (item?.protocols ?? ['openai_chat']).filter(
      (value): value is PlaygroundProtocol =>
        value === 'openai_chat' || value === 'openai_responses' || value === 'anthropic_messages',
    )
  }
  const availableProtocols = protocols(models.find((item) => item.id === model))
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | GatewayError>('')
  const [keyChecked, setKeyChecked] = useState(false)
  const [exchanges, setExchanges] = useState<Exchange[]>([])
  const [prompt, setPrompt] = useState('')
  const [copied, setCopied] = useState('')
  const controller = useRef<AbortController | null>(null)
  const mounted = useRef(true)
  const busy = loading || exchanges.some((exchange) => exchange.status === 'running')
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      controller.current?.abort()
    }
  }, [])
  function changeKey(value: string) {
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
      { role: 'user', content: text },
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
      if (protocol === 'anthropic_messages') {
        const result = await runMessages(
          key.trim(),
          {
            ...parameters,
            messages: messages.filter(
              (message): message is ChatMessage & { role: 'user' | 'assistant' } =>
                message.role !== 'system',
            ),
            ...(system ? { system } : {}),
            max_tokens: Number(form.get('max_tokens')),
          },
          abort.signal,
          update,
        )
        update({ ...result, status: result.messageStatus })
      } else if (protocol === 'openai_responses') {
        const result = await runResponses(
          key.trim(),
          {
            ...parameters,
            input: messages.filter(
              (message): message is ChatMessage & { role: 'user' | 'assistant' } =>
                message.role !== 'system',
            ),
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
        const result = await runChat(
          key.trim(),
          {
            ...parameters,
            messages,
            max_tokens: Number(form.get('max_tokens')),
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
    }
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
                    setProtocol(event.target.value as PlaygroundProtocol)
                    setExchanges([])
                    setCopied('')
                  }}
                >
                  {availableProtocols.map((value) => (
                    <option key={value} value={value}>
                      {value === 'anthropic_messages'
                        ? 'Anthropic Messages'
                        : value === 'openai_chat'
                          ? 'OpenAI Chat'
                          : 'OpenAI Responses'}
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
              <Badge variant="outline">
                {protocol === 'anthropic_messages'
                  ? 'Anthropic Messages'
                  : protocol === 'openai_chat'
                    ? 'OpenAI Chat'
                    : 'OpenAI Responses'}
              </Badge>
              <p className="mt-2 break-all font-mono text-xs text-muted-foreground">
                POST{' '}
                {protocol === 'anthropic_messages'
                  ? '/v1/messages'
                  : protocol === 'openai_chat'
                    ? '/v1/chat/completions'
                    : '/v1/responses'}
              </p>
            </div>
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
              <input type="checkbox" name="stream" defaultChecked />
              {t('stream_output_5b6aa')}
            </label>
          </fieldset>
        </aside>
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden bg-card">
          <div className="flex items-center justify-between border-b px-5 py-3">
            <h2 className="text-sm font-medium">{t('model_conversation_9e016')}</h2>
            <Button
              variant="ghost"
              size="sm"
              disabled={busy || !exchanges.length}
              onClick={() => {
                setExchanges([])
                setCopied('')
              }}
            >
              <Trash2 className="size-3.5" aria-hidden="true" />
              {t('clear_conversation_35c97')}
            </Button>
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
          <div className="space-y-3 border-t p-5">
            <Textarea
              aria-label={t('message_dc6de')}
              name="prompt"
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              disabled={busy}
              placeholder={t('enter_a_message_ctrl_enter_to_send_4419c')}
              onKeyDown={(event) => {
                if (
                  event.key === 'Enter' &&
                  (event.ctrlKey || event.metaKey) &&
                  !event.nativeEvent.isComposing
                ) {
                  event.preventDefault()
                  event.currentTarget.form?.requestSubmit()
                }
              }}
            />
            <div className="flex justify-end gap-2">
              {busy && !loading && (
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
      </form>
    </>
  )
}
