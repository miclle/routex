import type { Attachment, TeamAttachment, TeamAttachmentTarget } from '@/types/attachments'
import type {
  ChatCurrentTurnContent,
  ChatFileContentBlock,
  ChatImageContentBlock,
  GeminiInlineDataPart,
  GeminiPart,
  MessagesCurrentTurnContent,
  MessagesDocumentBlock,
  MessagesImageBlock,
  ResponsesCurrentTurnContent,
  ResponsesFileContentBlock,
  ResponsesImageContentBlock,
} from '@/types/playground'

const attachmentReference = (attachment: Attachment) => `routex://attachments/${attachment.id}`

function chatAttachmentBlock(attachment: Attachment): ChatImageContentBlock | ChatFileContentBlock {
  const reference = attachmentReference(attachment)
  if (attachment.mime === 'image/png' || attachment.mime === 'image/jpeg') {
    return { type: 'image_url', image_url: { url: reference } }
  }
  if (attachment.mime === 'application/pdf') {
    return {
      type: 'file',
      file: { file_data: reference, filename: attachment.name },
    }
  }
  throw new Error(`Unsupported attachment MIME type: ${attachment.mime}`)
}

function responsesAttachmentBlock(
  attachment: Attachment,
): ResponsesImageContentBlock | ResponsesFileContentBlock {
  const reference = attachmentReference(attachment)
  if (attachment.mime === 'image/png' || attachment.mime === 'image/jpeg') {
    return { type: 'input_image', image_url: reference }
  }
  if (attachment.mime === 'application/pdf') {
    return { type: 'input_file', file_data: reference, filename: attachment.name }
  }
  throw new Error(`Unsupported attachment MIME type: ${attachment.mime}`)
}

function messagesAttachmentBlock(
  attachment: Attachment,
): MessagesImageBlock | MessagesDocumentBlock {
  const reference = attachmentReference(attachment)
  if (attachment.mime === 'image/png' || attachment.mime === 'image/jpeg') {
    return {
      type: 'image',
      source: { type: 'base64', media_type: attachment.mime, data: reference },
    }
  }
  if (attachment.mime === 'application/pdf') {
    return {
      type: 'document',
      source: { type: 'base64', media_type: attachment.mime, data: reference },
    }
  }
  throw new Error(`Unsupported attachment MIME type: ${attachment.mime}`)
}

function geminiAttachmentPart(attachment: Attachment): GeminiInlineDataPart {
  if (
    attachment.mime !== 'image/png' &&
    attachment.mime !== 'image/jpeg' &&
    attachment.mime !== 'application/pdf'
  ) {
    throw new Error(`Unsupported attachment MIME type: ${attachment.mime}`)
  }
  return {
    inlineData: {
      mimeType: attachment.mime,
      data: attachmentReference(attachment),
    },
  }
}

export function buildChatAttachmentContent(
  prompt: string,
  attachments: readonly Attachment[],
): ChatCurrentTurnContent {
  return [{ type: 'text', text: prompt }, ...attachments.map(chatAttachmentBlock)]
}

export function buildResponsesAttachmentContent(
  prompt: string,
  attachments: readonly Attachment[],
): ResponsesCurrentTurnContent {
  return [{ type: 'input_text', text: prompt }, ...attachments.map(responsesAttachmentBlock)]
}

export function buildMessagesAttachmentContent(
  prompt: string,
  attachments: readonly Attachment[],
): MessagesCurrentTurnContent {
  return [{ type: 'text', text: prompt }, ...attachments.map(messagesAttachmentBlock)]
}

export function buildGeminiAttachmentParts(
  prompt: string,
  attachments: readonly Attachment[],
): GeminiPart[] {
  return [{ text: prompt }, ...attachments.map(geminiAttachmentPart)]
}

export function matchesTeamAttachment(attachment: Attachment, target: TeamAttachmentTarget) {
  const team = attachment as Partial<TeamAttachment>
  return (
    team.attachment_team_id === target.teamId &&
    team.attachment_membership_id === target.membershipId &&
    team.creator_user_id === target.creatorUserId &&
    team.state === 'ready' &&
    typeof team.expires_at === 'string'
  )
}
