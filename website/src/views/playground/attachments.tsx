import { useRef } from 'react'
import { FileText, Image, LoaderCircle, Paperclip, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import type { Attachment } from '@/types/attachments'

export function AttachmentChips({
  attachments,
  uploadingNames,
  disabled,
  removeLabel,
  uploadingLabel,
  onRemove,
}: {
  attachments: Attachment[]
  uploadingNames: string[]
  disabled: boolean
  removeLabel: (name: string) => string
  uploadingLabel: (name: string) => string
  onRemove: (attachment: Attachment) => void
}) {
  if (attachments.length === 0 && uploadingNames.length === 0) return null
  return (
    <div className="flex flex-wrap gap-2" aria-live="polite">
      {attachments.map((attachment) => (
        <span
          key={attachment.id}
          className="inline-flex h-8 max-w-64 items-center gap-2 rounded-md border bg-muted/40 px-2 text-xs"
        >
          {attachment.mime === 'application/pdf' ? (
            <FileText className="size-3.5 shrink-0" aria-hidden="true" />
          ) : (
            <Image className="size-3.5 shrink-0" aria-hidden="true" />
          )}
          <span className="truncate">{attachment.name}</span>
          <button
            type="button"
            className="rounded-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50"
            aria-label={removeLabel(attachment.name)}
            disabled={disabled}
            onClick={() => onRemove(attachment)}
          >
            <X className="size-3.5" aria-hidden="true" />
          </button>
        </span>
      ))}
      {uploadingNames.map((name) => (
        <span
          key={`uploading:${name}`}
          className="inline-flex h-8 max-w-64 items-center gap-2 rounded-md border bg-muted/40 px-2 text-xs text-muted-foreground"
          aria-label={uploadingLabel(name)}
        >
          <LoaderCircle className="size-3.5 shrink-0 animate-spin" aria-hidden="true" />
          <span className="truncate">{name}</span>
        </span>
      ))}
    </div>
  )
}

export function AttachmentPicker({
  accept,
  disabled,
  label,
  onFiles,
}: {
  accept: string
  disabled: boolean
  label: string
  onFiles: (files: File[]) => void
}) {
  const input = useRef<HTMLInputElement>(null)
  return (
    <>
      <input
        ref={input}
        className="sr-only"
        type="file"
        accept={accept}
        multiple
        disabled={disabled}
        aria-label={label}
        onChange={(event) => {
          const files = Array.from(event.currentTarget.files ?? [])
          event.currentTarget.value = ''
          if (files.length > 0) onFiles(files)
        }}
      />
      <Button
        variant="ghost"
        size="icon"
        disabled={disabled}
        aria-label={label}
        title={label}
        onClick={() => input.current?.click()}
      >
        <Paperclip className="size-4" aria-hidden="true" />
      </Button>
    </>
  )
}
