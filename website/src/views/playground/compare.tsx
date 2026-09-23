import { isGeminiModelName, protocolLabel } from '@/lib/protocols'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Send, Square, Trash2, X } from 'lucide-react'
import {
  GatewayError,
  getGatewayModels,
  runChat,
  runResponses,
  runMessages,
  runGemini,
} from '@/api/playground'
import type {
  ChatMessage,
  ChatResult,
  GatewayModel,
  PlaygroundProtocol,
  ResponseStatus,
} from '@/types/playground'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { FormField } from '@/components/app/CatalogUI'

type Turn = ChatResult & {
  id: string
  prompt: string
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
  error?: string | GatewayError
  duration?: number
}
type Lane = { id: number; model: string; protocol: PlaygroundProtocol; turns: Turn[] }
function protocols(model?: GatewayModel): PlaygroundProtocol[] {
  return (model?.protocols ?? ['openai_chat']).filter(
    (value): value is PlaygroundProtocol =>
      value === 'openai_chat' ||
      value === 'openai_responses' ||
      value === 'anthropic_messages' ||
      value === 'gemini_generate_content',
  )
}
function makeLane(id: number, model?: GatewayModel): Lane {
  return { id, model: model?.id ?? '', protocol: protocols(model)[0] ?? 'openai_chat', turns: [] }
}
const selectClass = 'h-10 min-w-0 flex-1 rounded-md border bg-background px-3 text-sm'
export default function CompareWorkbench() {
  const { t } = useTranslation('playground')
  const [key, setKey] = useState('')
  const [models, setModels] = useState<GatewayModel[]>([])
  const [lanes, setLanes] = useState<Lane[]>([makeLane(1), makeLane(2)])
  const [prompt, setPrompt] = useState('')
  const [loading, setLoading] = useState(false)
  const [checked, setChecked] = useState(false)
  const [error, setError] = useState<string | GatewayError>('')
  const nextID = useRef(3)
  const active = useRef(new Map<number, AbortController>())
  const verification = useRef<AbortController | null>(null)
  const lock = useRef(false)
  const mounted = useRef(true)
  const busy = loading || lanes.some((lane) => lane.turns.some((turn) => turn.status === 'running'))
  useEffect(() => {
    mounted.current = true
    const controllers = active.current
    return () => {
      mounted.current = false
      verification.current?.abort()
      for (const controller of controllers.values()) controller.abort()
      controllers.clear()
    }
  }, [])
  function changeKey(value: string) {
    setKey(value)
    setModels([])
    setLanes([makeLane(1), makeLane(2)])
    nextID.current = 3
    setChecked(false)
    setError('')
    setPrompt('')
  }
  async function verify() {
    if (busy || lock.current || !key.trim()) return
    lock.current = true
    setLoading(true)
    setError('')
    const abort = new AbortController()
    verification.current = abort
    try {
      const available = (await getGatewayModels(key.trim(), abort.signal)).filter(
        (item) => protocols(item).length,
      )
      if (!mounted.current || abort.signal.aborted) return
      setModels(available)
      setLanes([makeLane(1, available[0]), makeLane(2, available[1] ?? available[0])])
      nextID.current = 3
      setChecked(true)
    } catch (failure) {
      if (mounted.current && !abort.signal.aborted)
        setError(failure instanceof GatewayError ? failure : 'verifyFailed')
    } finally {
      lock.current = false
      if (mounted.current) setLoading(false)
      if (verification.current === abort) verification.current = null
    }
  }
  function add() {
    if (busy || lanes.length >= 4 || !models.length) return
    const selected = new Set(lanes.map((lane) => lane.model))
    const model = models.find((item) => !selected.has(item.id)) ?? models[0]
    const id = nextID.current++
    setLanes((current) => (current.length < 4 ? [...current, makeLane(id, model)] : current))
  }
  function remove(id: number) {
    if (lanes.length <= 2) return
    active.current.get(id)?.abort()
    active.current.delete(id)
    setLanes((current) => (current.length > 2 ? current.filter((lane) => lane.id !== id) : current))
  }
  function change(id: number, model: string, protocol?: PlaygroundProtocol) {
    active.current.get(id)?.abort()
    active.current.delete(id)
    const next = models.find((item) => item.id === model)
    setLanes((current) =>
      current.map((lane) =>
        lane.id === id
          ? { ...lane, model, protocol: protocol ?? protocols(next)[0], turns: [] }
          : lane,
      ),
    )
  }
  async function run(lane: Lane, text: string) {
    const abort = new AbortController()
    active.current.set(lane.id, abort)
    const id = crypto.randomUUID(),
      started = performance.now()
    const initial: Turn = {
      id,
      prompt: text,
      status: 'running',
      text: '',
      requestId: '',
      usage: null,
      finishReason: null,
    }
    setLanes((current) =>
      current.map((item) =>
        item.id === lane.id ? { ...item, turns: [...item.turns, initial] } : item,
      ),
    )
    const update = (patch: Partial<Turn>) => {
      if (!mounted.current || active.current.get(lane.id) !== abort) return
      setLanes((current) =>
        current.map((item) =>
          item.id === lane.id
            ? {
                ...item,
                turns: item.turns.map((turn) => (turn.id === id ? { ...turn, ...patch } : turn)),
              }
            : item,
        ),
      )
    }
    const messages: ChatMessage[] = [
      ...lane.turns
        .filter((turn) => turn.status === 'completed')
        .flatMap((turn): ChatMessage[] => [
          { role: 'user', content: turn.prompt },
          { role: 'assistant', content: turn.text },
        ]),
      { role: 'user', content: text },
    ]
    const parameters = { model: lane.model, stream: true, temperature: 0.7, top_p: 1 }
    try {
      if (lane.protocol === 'gemini_generate_content') {
        const result = await runGemini(
          key.trim(),
          {
            model: lane.model,
            stream: true,
            contents: messages.map((message) => ({
              role: message.role === 'assistant' ? ('model' as const) : ('user' as const),
              parts: [{ text: message.content }],
            })),
            generationConfig: {
              temperature: 0.7,
              topP: 1,
              maxOutputTokens: 2048,
              candidateCount: 1,
            },
          },
          abort.signal,
          update,
        )
        update({
          ...result,
          status: result.generationStatus,
          duration: Math.round(performance.now() - started),
        })
      } else if (lane.protocol === 'anthropic_messages') {
        const result = await runMessages(
          key.trim(),
          {
            ...parameters,
            messages: messages as { role: 'user' | 'assistant'; content: string }[],
            max_tokens: 2048,
          },
          abort.signal,
          update,
        )
        update({
          ...result,
          status: result.messageStatus,
          duration: Math.round(performance.now() - started),
        })
      } else if (lane.protocol === 'openai_responses') {
        const result = await runResponses(
          key.trim(),
          {
            ...parameters,
            input: messages as { role: 'user' | 'assistant'; content: string }[],
            max_output_tokens: 2048,
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
          error: result.responseStatus === 'failed' ? 'failedHelp' : '',
          duration: Math.round(performance.now() - started),
        })
      } else {
        const result = await runChat(
          key.trim(),
          { ...parameters, messages, max_tokens: 2048, stream_options: { include_usage: true } },
          abort.signal,
          update,
        )
        update({
          ...result,
          status: 'completed',
          duration: Math.round(performance.now() - started),
        })
      }
    } catch (failure) {
      update({
        status: abort.signal.aborted ? 'cancelled' : 'failed',
        error: abort.signal.aborted
          ? ''
          : failure instanceof GatewayError
            ? failure
            : 'requestFailed',
        ...(failure instanceof GatewayError && failure.requestId
          ? { requestId: failure.requestId }
          : {}),
        duration: Math.round(performance.now() - started),
      })
    } finally {
      if (active.current.get(lane.id) === abort) active.current.delete(lane.id)
    }
  }
  async function send(event: FormEvent) {
    event.preventDefault()
    if (
      busy ||
      lock.current ||
      !prompt.trim() ||
      !key.trim() ||
      !lanes.every(
        (lane) =>
          lane.model &&
          protocols(models.find((item) => item.id === lane.model)).includes(lane.protocol),
      )
    )
      return
    lock.current = true
    const captured = prompt.trim()
    setPrompt('')
    try {
      await Promise.allSettled(lanes.map((lane) => run(lane, captured)))
    } finally {
      lock.current = false
    }
  }
  return (
    <form
      onSubmit={(event) => void send(event)}
      className="flex min-h-[640px] min-w-[980px] flex-col overflow-hidden rounded-lg border"
      style={{ height: 'calc(100vh - 190px)' }}
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b px-[18px] py-4">
        <fieldset disabled={busy} className="min-w-[280px] space-y-2">
          <FormField label={t('key')}>
            <Input
              aria-label={t('comparisonKey')}
              name="comparison_key"
              type="password"
              autoComplete="off"
              value={key}
              onValueChange={changeKey}
              placeholder={t('keyPlaceholder')}
            />
          </FormField>
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={!key.trim() || busy}
              onClick={() => void verify()}
            >
              {t(loading ? 'verifying' : 'verify')}
            </Button>
            <Button size="sm" variant="ghost" disabled={!key || busy} onClick={() => changeKey('')}>
              {t('clearKey')}
            </Button>
          </div>
        </fieldset>
        <div className="flex gap-2">
          <Button
            variant="outline"
            disabled={busy || !lanes.some((lane) => lane.turns.length)}
            onClick={() => setLanes((current) => current.map((lane) => ({ ...lane, turns: [] })))}
          >
            <Trash2 className="size-4" aria-hidden="true" />
            {t('clearAll')}
          </Button>
          <Button
            variant="outline"
            disabled={busy || lanes.length >= 4 || !models.length}
            onClick={add}
          >
            <Plus className="size-4" aria-hidden="true" />
            {t('addComparison')}
          </Button>
        </div>
      </div>
      {error && (
        <p role="alert" className="px-4 py-2 text-sm text-destructive">
          {error instanceof GatewayError ? error.message : t(error)}
        </p>
      )}
      {checked && !models.length && (
        <p role="status" className="p-4 text-sm">
          {t('noModels')}
        </p>
      )}
      <div className="flex min-h-0 flex-1 overflow-x-auto">
        {lanes.map((lane, index) => (
          <section
            key={lane.id}
            aria-label={t('lane', { count: index + 1 })}
            className="flex min-w-[300px] flex-[1_0_300px] flex-col border-r last:border-r-0"
          >
            <div className="space-y-2 border-b p-3.5">
              <div className="flex items-center gap-2">
                <select
                  className={selectClass}
                  aria-label={t('laneModel', { count: index + 1 })}
                  value={lane.model}
                  disabled={!models.length || loading}
                  onChange={(event) => change(lane.id, event.target.value)}
                >
                  <option value="" disabled>
                    {t('selectModel')}
                  </option>
                  {models.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.id}
                    </option>
                  ))}
                </select>
                {lanes.length > 2 && (
                  <Button
                    size="icon"
                    variant="ghost"
                    aria-label={t('removeLane', { count: index + 1 })}
                    onClick={() => remove(lane.id)}
                  >
                    <X className="size-4" aria-hidden="true" />
                  </Button>
                )}
              </div>
              {protocols(models.find((item) => item.id === lane.model)).length > 1 && (
                <select
                  className={`${selectClass} w-full`}
                  aria-label={t('laneProtocol', { count: index + 1 })}
                  value={lane.protocol}
                  onChange={(event) =>
                    change(lane.id, lane.model, event.target.value as PlaygroundProtocol)
                  }
                >
                  {protocols(models.find((item) => item.id === lane.model)).map((protocol) => (
                    <option key={protocol} value={protocol}>
                      {protocolLabel(protocol)}
                    </option>
                  ))}
                </select>
              )}
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="outline">
                  {lane.model ? protocolLabel(lane.protocol) : t('selectModel')}
                </Badge>
                {lane.model && (
                  <span className="text-xs text-muted-foreground">
                    {lane.protocol === 'gemini_generate_content'
                      ? isGeminiModelName(lane.model)
                        ? `/v1beta/models/${encodeURIComponent(lane.model)}:streamGenerateContent`
                        : '—'
                      : lane.protocol === 'anthropic_messages'
                        ? '/v1/messages'
                        : lane.protocol === 'openai_chat'
                          ? '/v1/chat/completions'
                          : '/v1/responses'}
                  </span>
                )}
              </div>
              {lane.protocol === 'gemini_generate_content' && !isGeminiModelName(lane.model) && (
                <p role="status" className="text-sm text-muted-foreground">
                  {t('geminiAliasRequired')}
                </p>
              )}
              {lane.turns.some((turn) => turn.status === 'running') && (
                <Button
                  size="sm"
                  variant="outline"
                  aria-label={t('stopLane', { count: index + 1 })}
                  onClick={() => active.current.get(lane.id)?.abort()}
                >
                  <Square className="size-3" aria-hidden="true" />
                  {t('stop')}
                </Button>
              )}
            </div>
            <div
              role="log"
              aria-label={t('laneTranscript', { count: index + 1 })}
              aria-live="polite"
              className="min-h-0 flex-1 space-y-6 overflow-y-auto p-4"
            >
              {!lane.turns.length && (
                <div className="flex h-full min-h-40 items-center justify-center text-sm text-muted-foreground">
                  {t('waiting')}
                </div>
              )}
              {lane.turns.map((turn) => (
                <article key={turn.id} className="space-y-3">
                  <p className="ml-auto max-w-[88%] whitespace-pre-wrap rounded-lg border p-2.5 text-sm">
                    {turn.prompt}
                  </p>
                  <Badge variant="outline">{t(`state_${turn.status}`)}</Badge>
                  <p className="whitespace-pre-wrap break-words text-sm leading-7">
                    {turn.text || t(turn.status === 'running' ? 'waitingOutput' : 'noText')}
                  </p>
                  {turn.error && (
                    <p role="alert" className="text-sm text-destructive">
                      {turn.error instanceof GatewayError ? turn.error.message : t(turn.error)}
                    </p>
                  )}
                  {(turn.status === 'incomplete' ||
                    turn.status === 'accepted' ||
                    turn.status === 'handoff' ||
                    turn.status === 'refused' ||
                    turn.nonTextOutput) && (
                    <p className="text-xs text-muted-foreground">
                      {t(
                        turn.status === 'handoff'
                          ? 'handoffHelp'
                          : turn.status === 'refused'
                            ? 'refusedHelp'
                            : turn.status === 'incomplete'
                              ? 'incompleteHelp'
                              : turn.status === 'accepted'
                                ? 'acceptedHelp'
                                : 'textOnly',
                      )}
                    </p>
                  )}
                  <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
                    {turn.duration !== undefined && <span>{turn.duration} ms</span>}
                    {turn.requestId && (
                      <span className="break-all">
                        {t('requestId')}: {turn.requestId}
                      </span>
                    )}
                    {turn.usage ? (
                      <span>
                        {t('usage', {
                          input: turn.usage.prompt_tokens,
                          output: turn.usage.completion_tokens,
                          total: turn.usage.total_tokens,
                        })}
                      </span>
                    ) : (
                      turn.status !== 'running' && <span>{t('noUsage')}</span>
                    )}
                  </div>
                </article>
              ))}
            </div>
          </section>
        ))}
      </div>
      <div className="space-y-3 border-t p-4">
        <Textarea
          name="comparison_prompt"
          aria-label={t('comparisonPrompt')}
          value={prompt}
          rows={3}
          placeholder={t('comparisonPrompt')}
          disabled={busy}
          onChange={(event) => setPrompt(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
              event.preventDefault()
              event.currentTarget.form?.requestSubmit()
            }
          }}
        />
        <div className="flex justify-end">
          <Button
            type="submit"
            aria-label={t('sendAll')}
            disabled={busy || !prompt.trim() || !lanes.every((lane) => lane.model)}
          >
            <Send className="size-4" aria-hidden="true" />
            {t(busy ? 'sending' : 'sendAll')}
          </Button>
        </div>
      </div>
    </form>
  )
}
