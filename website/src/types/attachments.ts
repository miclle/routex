export type AttachmentState = 'uploading' | 'ready' | 'delete_pending' | 'deleted'

export interface Attachment {
  id: string
  name: string
  mime: string
  size: number
  state: AttachmentState
  created_at: string
}

export type AttachmentTarget = { scope: 'user' } | { scope: 'project'; projectId: string }

export interface TeamAttachment extends Attachment {
  attachment_team_id: string
  attachment_membership_id: string
  creator_user_id: string
  expires_at: string
}
export interface TeamAttachmentTarget {
  scope: 'team'
  teamId: string
  membershipId: string
  creatorUserId: string
}
export type PlaygroundAttachmentTarget = AttachmentTarget | TeamAttachmentTarget
