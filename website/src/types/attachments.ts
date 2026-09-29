export type AttachmentState = 'uploading' | 'ready' | 'delete_pending' | 'deleted'

export interface Attachment {
  id: string
  name: string
  mime: string
  size: number
  state: AttachmentState
  created_at: string
}
