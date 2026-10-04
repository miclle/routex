import { useSessionGeneration } from '@/hooks/use-session-generation'
import type { SnippetInput } from '@/lib/playground-snippet'
import type { TeamSnippetInput } from '@/lib/playground-team-snippet'
import CodeDialog from './code-dialog'
import TeamPicker from './team-picker'
import {
  getTeamModels,
  uploadTeamAttachment,
  deleteTeamAttachment,
  runTeamChat,
  runTeamResponses,
  runTeamMessages,
  runTeamGemini,
  teamInferencePath,
} from '@/api/playground-team'
import { AttachmentChips, AttachmentPicker } from './attachments'
import { isGeminiModelName, protocolLabel } from '@/lib/protocols'
import {
  matchesTeamAttachment,
  buildChatAttachmentContent,
  buildGeminiAttachmentParts,
  buildMessagesAttachmentContent,
  buildResponsesAttachmentContent,
} from '@/lib/playground-attachments'
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Code, Plus, Send, Square, Trash2, X } from 'lucide-react'
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
import type {
  Attachment,
  PlaygroundAttachmentTarget as AttachmentTarget,
} from '@/types/attachments'
import { useSession } from '@/hooks/use-auth'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { FormField } from '@/components/app/CatalogUI'

type Turn = ChatResult & {
  id: string
  prompt: string
  attachments: string[]
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

function uploadScopedAttachment(
  file: File,
  csrf: string,
  target: AttachmentTarget,
  signal?: AbortSignal,
) {
  if (target.scope === 'team') return uploadTeamAttachment(target.teamId, file, csrf, signal)
  return target.scope === 'user'
    ? uploadAttachment(file, csrf)
    : uploadAttachment(file, csrf, undefined, target)
}

function deleteScopedAttachment(id: string, csrf: string, target: AttachmentTarget) {
  if (target.scope === 'team') return deleteTeamAttachment(target.teamId, id, csrf)
  return target.scope === 'user'
    ? deleteAttachment(id, csrf)
    : deleteAttachment(id, csrf, undefined, target)
}
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
type CompareProps = {
  projectId?: string
  source?: 'key' | 'team'
  teamId?: string
  onTeam?: (team: string) => void
  onSource?: (source: 'key' | 'team') => void
}
export default function CompareWorkbench(props: CompareProps) {
  const session = useSession()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  return (
    <ComparisonSession
      key={`${actor}:${props.source ?? 'key'}:${props.teamId ?? ''}:${props.projectId ?? ''}`}
      {...props}
      session={session}
    />
  )
}
function ComparisonSession({
  projectId = '',
  source = 'key',
  teamId = '',
  onTeam,
  onSource,
  session,
}: CompareProps & { session: ReturnType<typeof useSession> }) {
  const { t } = useTranslation('playground')
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const freshSession = !session.isError && !session.isFetching && !!session.data
  const sessionGeneration = useSessionGeneration()
  const previousSessionGeneration = useRef(sessionGeneration)
  const liveSession = useRef(session.data)
  const [teamConfirmed, setTeamConfirmed] = useState(false)
  const epoch = useRef(0)
  const [codeRequest, setCodeRequest] = useState<SnippetInput | TeamSnippetInput | null>(null)
  const [key, setKey] = useState('')
  const [models, setModels] = useState<GatewayModel[]>([])
  const [lanes, setLanes] = useState<Lane[]>([makeLane(1), makeLane(2)])
  const [prompt, setPrompt] = useState('')
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const [uploadingNames, setUploadingNames] = useState<string[]>([])
  const [attachmentError, setAttachmentError] = useState('')
  const [loading, setLoading] = useState(false)
  const [checked, setChecked] = useState(false)
  const [error, setError] = useState<string | GatewayError>('')
  const nextID = useRef(3)
  const active = useRef(new Map<number, AbortController>())
  const verification = useRef<AbortController | null>(null)
  const attachmentGeneration = useRef(0)
  const uploadControllers = useRef(new Set<AbortController>())
  const submittedAttachmentIDs = useRef(new Set<string>())
  const attachmentRef = useRef<Attachment[]>([])
  const ownedAttachmentTargets = useRef(new Map<string, AttachmentTarget>())
  const csrfRef = useRef('')
  const lock = useRef(false)
  const mounted = useRef(true)
  const teamVisible =
    source !== 'team' || (freshSession && teamConfirmed && !!session.data?.csrf_token)
  if (codeRequest && source === 'team' && (!teamVisible || !actor || !session.data?.csrf_token))
    setCodeRequest(null)
  const visibleModels = teamVisible ? models : []
  const visibleLanes = teamVisible
    ? lanes
    : lanes.map((lane) => ({ ...lane, model: '', turns: [] }))
  const requestRunning = lanes.some((lane) => lane.turns.some((turn) => turn.status === 'running'))
  const busy = loading || uploadingNames.length > 0 || requestRunning
  const laneCapabilities = lanes.map((lane) => {
    const selected = models.find((item) => item.id === lane.model)
    return {
      values: selected?.input_capabilities?.[lane.protocol] ?? [],
    }
  })
  const attachmentScope = models[0]?.attachment_scope
  const attachmentTarget: AttachmentTarget | null =
    source === 'team' &&
    teamVisible &&
    models[0]?.attachment_scope === 'team' &&
    models[0].attachment_team_id === teamId &&
    !!models[0].attachment_membership_id &&
    !!session.data?.user.id &&
    lanes.every((lane) => {
      const selected = models.find((model) => model.id === lane.model)
      return (
        selected?.attachment_team_id === teamId &&
        selected?.attachment_membership_id === models[0]?.attachment_membership_id
      )
    })
      ? {
          scope: 'team',
          teamId,
          membershipId: models[0].attachment_membership_id,
          creatorUserId: session.data.user.id,
        }
      : source === 'key' && attachmentScope === 'user' && !projectId
        ? { scope: 'user' }
        : source === 'key' &&
            attachmentScope === 'project' &&
            !!models[0]?.attachment_project_id &&
            (!projectId || models[0].attachment_project_id === projectId)
          ? { scope: 'project', projectId: models[0].attachment_project_id }
          : null
  const inputCapabilities = (['image', 'pdf'] as const).filter((capability) =>
    laneCapabilities.every((item) => item.values.includes(capability)),
  )
  const attachmentAccept = [
    ...(inputCapabilities.includes('image') ? ['image/png', 'image/jpeg'] : []),
    ...(inputCapabilities.includes('pdf') ? ['application/pdf'] : []),
  ].join(',')
  const canAttach =
    (source === 'key' || (teamVisible && checked && !!session.data?.csrf_token)) &&
    !!session.data &&
    !!attachmentTarget &&
    lanes.every((lane) => !!lane.model) &&
    inputCapabilities.length > 0

  function replaceAttachments(update: (current: Attachment[]) => Attachment[]) {
    setAttachments((current) => {
      const next = update(current)
      attachmentRef.current = next
      return next
    })
  }

  const releaseAttachments = useCallback(async (items: Attachment[], csrf = csrfRef.current) => {
    if (!csrf) return
    const owned = items.filter((attachment) => ownedAttachmentTargets.current.has(attachment.id))
    const results = await Promise.allSettled(
      owned.map(async (attachment) => {
        const target = ownedAttachmentTargets.current.get(attachment.id)
        if (!target) return
        await deleteScopedAttachment(attachment.id, csrf, target)
        ownedAttachmentTargets.current.delete(attachment.id)
      }),
    )
    if (mounted.current && results.some((result) => result.status === 'rejected'))
      setAttachmentError('attachmentDeleteFailed')
  }, [])

  const clearDraftAttachments = useCallback(() => {
    attachmentGeneration.current += 1
    for (const upload of uploadControllers.current) upload.abort()
    uploadControllers.current.clear()
    const current = attachmentRef.current
    attachmentRef.current = []
    setAttachments([])
    setUploadingNames([])
    setAttachmentError('')
    void releaseAttachments(current)
  }, [releaseAttachments])

  useLayoutEffect(() => {
    if (source === 'team' && !(freshSession && !!session.data?.csrf_token)) {
      attachmentGeneration.current += 1
      for (const upload of uploadControllers.current) upload.abort()
      uploadControllers.current.clear()
    }
  }, [source, freshSession, session.data?.csrf_token])

  useEffect(() => {
    csrfRef.current = session.data?.csrf_token ?? ''
  }, [session.data?.csrf_token])
  useLayoutEffect(() => {
    mounted.current = true
    const controllers = active.current
    const activeOwnedAttachmentTargets = ownedAttachmentTargets.current
    const uploads = uploadControllers.current
    const submitted = submittedAttachmentIDs.current
    return () => {
      mounted.current = false
      attachmentGeneration.current += 1
      for (const upload of uploads) upload.abort()
      uploads.clear()
      verification.current?.abort()
      for (const controller of controllers.values()) controller.abort()
      controllers.clear()
      attachmentRef.current = []
      const csrf = csrfRef.current
      const owned = [...activeOwnedAttachmentTargets].filter(([id]) => !submitted.has(id))
      for (const [id] of owned) activeOwnedAttachmentTargets.delete(id)
      if (csrf)
        for (const [id, target] of owned)
          void deleteScopedAttachment(id, csrf, target).catch(() => undefined)
    }
  }, [])

  const resetAuthority = useCallback(() => {
    clearDraftAttachments()
    epoch.current += 1
    verification.current?.abort()
    verification.current = null
    for (const abort of active.current.values()) abort.abort()
    active.current.clear()
    lock.current = false
    setModels([])
    setLanes([makeLane(1), makeLane(2)])
    nextID.current = 3
    setChecked(false)
    setLoading(false)
    setCodeRequest(null)
  }, [clearDraftAttachments])
  useLayoutEffect(() => {
    liveSession.current = freshSession ? session.data : undefined
    if (source === 'team' && (!freshSession || !session.data?.csrf_token)) {
      epoch.current += 1
      verification.current?.abort()
      for (const abort of active.current.values()) abort.abort()
      active.current.clear()
      lock.current = false
    }
  }, [freshSession, session.data, source])
  const confirmTeam = useCallback(
    (confirmed: boolean) => {
      setTeamConfirmed(confirmed)
      if (source === 'team' && !confirmed) resetAuthority()
    },
    [source, resetAuthority],
  )

  useLayoutEffect(() => {
    if (previousSessionGeneration.current !== sessionGeneration && source === 'team')
      confirmTeam(false)
    previousSessionGeneration.current = sessionGeneration
  }, [sessionGeneration, source, confirmTeam])

  async function selectAttachments(files: File[]) {
    if (!session.data || !canAttach || busy) return
    const target = attachmentTarget
    if (!target) return
    setAttachmentError('')
    setCodeRequest(null)
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
        setAttachmentError('attachmentDuplicate')
        continue
      }
      if (remaining <= 0) {
        setAttachmentError('attachmentLimit')
        break
      }
      const capability = attachmentCapability(file)
      if (!capability || !inputCapabilities.includes(capability)) {
        setAttachmentError('attachmentType')
        continue
      }
      if (file.size <= 0 || file.size > maxAttachmentBytes) {
        setAttachmentError('attachmentSize')
        continue
      }
      knownNames.add(file.name)
      remaining -= 1
      setUploadingNames((current) => [...current, file.name])
      try {
        const upload = target.scope === 'team' ? new AbortController() : undefined
        if (upload) uploadControllers.current.add(upload)
        let attachment: Attachment
        try {
          attachment = await uploadScopedAttachment(file, csrf, target, upload?.signal)
        } finally {
          if (upload) uploadControllers.current.delete(upload)
        }
        if (target.scope === 'team' && !matchesTeamAttachment(attachment, target))
          throw new AttachmentError(0)
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
          setAttachmentError('attachmentType')
          await deleteScopedAttachment(attachment.id, csrf, target)
            .then(() => ownedAttachmentTargets.current.delete(attachment.id))
            .catch(() => undefined)
          continue
        }
        replaceAttachments((current) => [...current, attachment])
      } catch (failure) {
        if (mounted.current && attachmentGeneration.current === generation) {
          if (
            target.scope === 'team' &&
            failure instanceof AttachmentError &&
            [401, 403, 404].includes(failure.status)
          )
            confirmTeam(false)
          setAttachmentError(
            failure instanceof AttachmentError && failure.status === 429
              ? 'attachmentStorageLimit'
              : failure instanceof AttachmentError &&
                  failure.status === 404 &&
                  target.scope === 'project'
                ? 'attachmentProjectUnavailable'
                : failure instanceof AttachmentError && failure.status === 503
                  ? 'attachmentStorageUnavailable'
                  : 'attachmentUploadFailed',
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
        if (mounted.current) setAttachmentError('attachmentDeleteFailed')
      })
  }

  function changeKey(value: string) {
    resetAuthority()
    clearDraftAttachments()
    setKey(value)
    setModels([])
    setLanes([makeLane(1), makeLane(2)])
    nextID.current = 3
    setChecked(false)
    setError('')
    setPrompt('')
  }
  async function verify() {
    if (
      busy ||
      lock.current ||
      (source === 'key' ? !key.trim() : !teamConfirmed || !freshSession || !liveSession.current)
    )
      return
    setError('')
    const generation = epoch.current
    if (source === 'team') resetAuthority()
    const currentGeneration = source === 'team' ? epoch.current : generation
    lock.current = true
    setLoading(true)
    const abort = new AbortController()
    verification.current = abort
    try {
      const raw =
        source === 'team'
          ? await getTeamModels(teamId, abort.signal)
          : await getGatewayModels(key.trim(), abort.signal)
      if (!mounted.current || abort.signal.aborted || epoch.current !== currentGeneration) return
      const verifiedScope = raw[0]?.attachment_scope
      const verifiedProjectId = raw[0]?.attachment_project_id ?? ''
      if (
        source === 'key' &&
        projectId &&
        raw.length > 0 &&
        (verifiedScope !== 'project' || verifiedProjectId !== projectId)
      ) {
        clearDraftAttachments()
        setModels([])
        setLanes([makeLane(1), makeLane(2)])
        nextID.current = 3
        setChecked(false)
        setError('attachmentProjectMismatch')
        return
      }
      const available = raw.filter((item) => protocols(item).length)
      clearDraftAttachments()
      setModels(available)
      setLanes([makeLane(1, available[0]), makeLane(2, available[1] ?? available[0])])
      nextID.current = 3
      setChecked(true)
    } catch (failure) {
      if (mounted.current && !abort.signal.aborted && epoch.current === currentGeneration)
        setError(
          failure instanceof GatewayError
            ? failure
            : source === 'team'
              ? 'teamUnavailable'
              : 'verifyFailed',
        )
    } finally {
      if (epoch.current === currentGeneration) {
        lock.current = false
        if (mounted.current) setLoading(false)
      }
      if (verification.current === abort) verification.current = null
    }
  }
  function add() {
    if (busy || lanes.length >= 4 || !models.length) return
    setCodeRequest(null)
    clearDraftAttachments()
    const selected = new Set(lanes.map((lane) => lane.model))
    const model = models.find((item) => !selected.has(item.id)) ?? models[0]
    const id = nextID.current++
    setLanes((current) => (current.length < 4 ? [...current, makeLane(id, model)] : current))
  }
  function remove(id: number) {
    if (lanes.length <= 2) return
    setCodeRequest(null)
    clearDraftAttachments()
    active.current.get(id)?.abort()
    active.current.delete(id)
    setLanes((current) => (current.length > 2 ? current.filter((lane) => lane.id !== id) : current))
  }
  function canShowCode(lane: Lane) {
    return (
      checked &&
      !!lane.model &&
      models.some((item) => item.id === lane.model) &&
      !busy &&
      attachments.length === 0 &&
      lanes.some(
        (current) =>
          current.id === lane.id &&
          current.model === lane.model &&
          current.protocol === lane.protocol,
      ) &&
      protocols(models.find((item) => item.id === lane.model)).includes(lane.protocol) &&
      (source !== 'team' || (teamVisible && !!teamId && !!actor && !!session.data?.csrf_token))
    )
  }
  function showCode(lane: Lane) {
    if (!canShowCode(lane) || (source === 'team' && liveSession.current?.user.id !== actor)) return
    setCodeRequest({
      ...(source === 'team' ? { source: 'team' as const, teamId } : {}),
      origin: window.location.origin,
      protocol: lane.protocol,
      model: lane.model,
      stream: true,
      temperature: 0.7,
      topP: 1,
      maxTokens: 2048,
      system: '',
      messages: [
        ...lane.turns
          .filter(
            (turn) =>
              turn.status === 'completed' &&
              !turn.refused &&
              !turn.nonTextOutput &&
              !!turn.text.trim(),
          )
          .flatMap((turn) => [
            { role: 'user' as const, content: turn.prompt },
            { role: 'assistant' as const, content: turn.text },
          ]),
        { role: 'user', content: prompt.trim() || t('codePromptPlaceholder') },
      ],
    })
  }
  function change(id: number, model: string, protocol?: PlaygroundProtocol) {
    setCodeRequest(null)
    clearDraftAttachments()
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
  async function run(lane: Lane, text: string, submittedAttachments: Attachment[]) {
    const generation = epoch.current
    const csrf = liveSession.current?.csrf_token ?? ''
    const abort = new AbortController()
    active.current.set(lane.id, abort)
    const id = crypto.randomUUID(),
      started = performance.now()
    const initial: Turn = {
      id,
      prompt: text,
      attachments: submittedAttachments.map((attachment) => attachment.name),
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
      if (
        !mounted.current ||
        abort.signal.aborted ||
        epoch.current !== generation ||
        active.current.get(lane.id) !== abort
      )
        return
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
        .filter(
          (turn) =>
            turn.status === 'completed' &&
            !turn.refused &&
            !turn.nonTextOutput &&
            !!turn.text.trim(),
        )
        .flatMap((turn): ChatMessage[] => [
          { role: 'user', content: turn.prompt },
          { role: 'assistant', content: turn.text },
        ]),
    ]
    const parameters = { model: lane.model, stream: true, temperature: 0.7, top_p: 1 }
    try {
      if (lane.protocol === 'gemini_generate_content') {
        const invoke =
          source === 'team'
            ? runTeamGemini.bind(null, teamId, csrf)
            : runGemini.bind(null, key.trim())
        const result = await invoke(
          {
            model: lane.model,
            stream: true,
            contents: [
              ...messages.map((message) => ({
                role: message.role === 'assistant' ? ('model' as const) : ('user' as const),
                parts: [{ text: message.content }],
              })),
              {
                role: 'user' as const,
                parts: buildGeminiAttachmentParts(text, submittedAttachments),
              },
            ],
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
        const currentMessage: MessagesHistoryMessage | MessagesCurrentTurnMessage =
          submittedAttachments.length > 0
            ? {
                role: 'user',
                content: buildMessagesAttachmentContent(text, submittedAttachments),
              }
            : { role: 'user', content: text }
        const invoke =
          source === 'team'
            ? runTeamMessages.bind(null, teamId, csrf)
            : runMessages.bind(null, key.trim())
        const result = await invoke(
          {
            ...parameters,
            messages: [...(messages as MessagesHistoryMessage[]), currentMessage],
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
        const currentItem: ResponsesHistoryItem | ResponsesCurrentTurnItem =
          submittedAttachments.length > 0
            ? {
                role: 'user',
                content: buildResponsesAttachmentContent(text, submittedAttachments),
              }
            : { role: 'user', content: text }
        const invoke =
          source === 'team'
            ? runTeamResponses.bind(null, teamId, csrf)
            : runResponses.bind(null, key.trim())
        const result = await invoke(
          {
            ...parameters,
            input: [...(messages as ResponsesHistoryItem[]), currentItem],
            max_output_tokens: 2048,
          },
          abort.signal,
          update,
        )
        update({
          ...result,
          status: result.refused
            ? 'refused'
            : result.responseStatus === 'completed' && result.nonTextOutput
              ? 'handoff'
              : result.responseStatus === 'completed' && !!result.text.trim()
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
        const currentMessage: ChatMessage | ChatCurrentTurnMessage =
          submittedAttachments.length > 0
            ? {
                role: 'user',
                content: buildChatAttachmentContent(text, submittedAttachments),
              }
            : { role: 'user', content: text }
        const invoke =
          source === 'team' ? runTeamChat.bind(null, teamId, csrf) : runChat.bind(null, key.trim())
        const result = await invoke(
          {
            ...parameters,
            messages: [...messages, currentMessage],
            max_completion_tokens: 2048,
            stream_options: { include_usage: true },
          },
          abort.signal,
          update,
        )
        update({
          ...result,
          status:
            result.refused || result.finishReason === 'content_filter'
              ? 'refused'
              : result.finishReason === 'tool_calls' || result.finishReason === 'function_call'
                ? 'handoff'
                : result.finishReason === 'stop' && !!result.text.trim()
                  ? 'completed'
                  : 'incomplete',
          duration: Math.round(performance.now() - started),
        })
      }
    } catch (failure) {
      if (
        source === 'team' &&
        failure instanceof GatewayError &&
        [401, 403, 404].includes(failure.status ?? 0) &&
        !abort.signal.aborted &&
        epoch.current === generation
      ) {
        resetAuthority()
        setTeamConfirmed(false)
        setError('teamRefreshRequired')
        return
      }
      if (
        abort.signal.aborted &&
        active.current.get(lane.id) === abort &&
        epoch.current === generation
      ) {
        setLanes((current) =>
          current.map((item) =>
            item.id === lane.id
              ? {
                  ...item,
                  turns: item.turns.map((turn) =>
                    turn.id === id ? { ...turn, status: 'cancelled' } : turn,
                  ),
                }
              : item,
          ),
        )
      }
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
      (source === 'key' ? !key.trim() : !teamConfirmed || !freshSession || !liveSession.current) ||
      !lanes.every(
        (lane) =>
          lane.model &&
          protocols(models.find((item) => item.id === lane.model)).includes(lane.protocol),
      )
    )
      return
    lock.current = true
    const generation = epoch.current
    const captured = prompt.trim()
    const submittedAttachments = attachmentRef.current
    for (const attachment of submittedAttachments) submittedAttachmentIDs.current.add(attachment.id)
    attachmentGeneration.current += 1
    attachmentRef.current = []
    setAttachments([])
    setUploadingNames([])
    setAttachmentError('')
    setPrompt('')
    try {
      await Promise.allSettled(lanes.map((lane) => run(lane, captured, submittedAttachments)))
    } finally {
      if (epoch.current === generation) lock.current = false
      await releaseAttachments(submittedAttachments)
      for (const attachment of submittedAttachments)
        submittedAttachmentIDs.current.delete(attachment.id)
    }
  }
  return (
    <form
      onSubmit={(event) => void send(event)}
      className="flex min-h-[640px] min-w-[980px] flex-col overflow-hidden rounded-lg border"
      style={{ height: 'calc(100vh - 190px)' }}
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b px-[18px] py-4">
        <div className="space-y-2">
          {onSource && (
            <FormField label={t('source')}>
              <select
                name="comparison_source"
                aria-label={t('source')}
                value={source}
                onChange={(event) => onSource(event.target.value as 'key' | 'team')}
                className="h-11 w-full rounded-md border bg-background px-3 text-sm"
              >
                <option value="key">{t('keySource')}</option>
                <option value="team">{t('teamSource')}</option>
              </select>
            </FormField>
          )}
          {source === 'team' ? (
            <div className="min-w-[280px] space-y-2">
              <TeamPicker
                key={sessionGeneration}
                actor={freshSession && session.data?.csrf_token ? actor : ''}
                value={teamId}
                onChange={(value) => onTeam?.(value)}
                onConfirmed={confirmTeam}
              />
              <p className="text-xs text-muted-foreground">{t('teamTextOnly')}</p>
              <Button
                size="sm"
                variant="outline"
                disabled={busy || !freshSession || !teamConfirmed}
                onClick={() => void verify()}
              >
                {t(loading ? 'verifying' : 'teamLoadModels')}
              </Button>
            </div>
          ) : (
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
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={!key || busy}
                  onClick={() => changeKey('')}
                >
                  {t('clearKey')}
                </Button>
              </div>
            </fieldset>
          )}
        </div>
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
          {t(source === 'team' ? 'teamNoModels' : 'noModels')}
        </p>
      )}
      <div className="flex min-h-0 flex-1 overflow-x-auto">
        {visibleLanes.map((lane, index) => (
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
                  disabled={!visibleModels.length || loading}
                  onChange={(event) => change(lane.id, event.target.value)}
                >
                  <option value="" disabled>
                    {t('selectModel')}
                  </option>
                  {visibleModels.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.id}
                    </option>
                  ))}
                </select>
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={t('laneCode', { count: index + 1 })}
                  disabled={!canShowCode(lane)}
                  onClick={() => showCode(lane)}
                >
                  <Code className="size-4" />
                </Button>
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
                    {source === 'team'
                      ? teamInferencePath(teamId, lane.protocol, lane.model, true)
                      : lane.protocol === 'gemini_generate_content'
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
                  onClick={() => {
                    active.current.get(lane.id)?.abort()
                    active.current.delete(lane.id)
                    setLanes((current) =>
                      current.map((item) =>
                        item.id === lane.id
                          ? {
                              ...item,
                              turns: item.turns.map((turn) =>
                                turn.status === 'running' ? { ...turn, status: 'cancelled' } : turn,
                              ),
                            }
                          : item,
                      ),
                    )
                  }}
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
                  {turn.attachments.length > 0 && (
                    <p className="ml-auto max-w-[88%] text-right text-xs text-muted-foreground">
                      {turn.attachments.join(' · ')}
                    </p>
                  )}
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
                          total: turn.usage.total_tokens ?? t('playground:unknownUsage'),
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
        <AttachmentChips
          attachments={teamVisible ? attachments : []}
          uploadingNames={teamVisible ? uploadingNames : []}
          disabled={busy}
          removeLabel={(name) => t('removeAttachment', { name })}
          uploadingLabel={(name) => t('attachmentUploadingLabel', { name })}
          onRemove={removeAttachment}
        />
        {attachmentError && (
          <p role="alert" className="text-sm text-destructive">
            {t(attachmentError)}
          </p>
        )}
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
        <div className="flex items-center justify-between gap-2">
          {(source === 'key' || !!attachmentTarget) && (
            <AttachmentPicker
              accept={attachmentAccept}
              disabled={busy || !canAttach || attachments.length >= maxAttachments}
              label={
                canAttach
                  ? t('attachFiles')
                  : attachmentTarget && models.length > 0
                    ? t('attachmentUnsupported')
                    : (source === 'key' && attachmentScope === 'project') || projectId
                      ? t('attachmentProjectUnavailable')
                      : t('attachmentUnsupported')
              }
              onFiles={(files) => void selectAttachments(files)}
            />
          )}
          <div className="flex justify-end">
            <Button
              type="submit"
              aria-label={t('sendAll')}
              disabled={
                busy ||
                !prompt.trim() ||
                !visibleLanes.every((lane) => lane.model) ||
                (source === 'team' && (!teamConfirmed || !freshSession))
              }
            >
              <Send className="size-4" aria-hidden="true" />
              {t(busy ? 'sending' : 'sendAll')}
            </Button>
          </div>
        </div>
      </div>
      {codeRequest &&
        teamVisible &&
        (source !== 'team' || (checked && !loading && !!actor && !!session.data?.csrf_token)) && (
          <CodeDialog request={codeRequest} onClose={() => setCodeRequest(null)} />
        )}
    </form>
  )
}
