import { Dialog as BaseDialog } from '@base-ui/react/dialog'
import type { ReactNode } from 'react'
import { X } from 'lucide-react'
import { Button } from './button'

export function Dialog({ open, onOpenChange, title, description, children, busy = false, width = 520 }: { open: boolean; onOpenChange: (open: boolean) => void; title: string; description: string; children: ReactNode; busy?: boolean; width?: number }) {
  return <BaseDialog.Root open={open} onOpenChange={(value) => { if (!busy) onOpenChange(value) }}>
    <BaseDialog.Portal><BaseDialog.Backdrop className="fixed inset-0 z-50 bg-black/40" /><BaseDialog.Popup style={{ width, maxWidth: 'calc(100vw - 32px)' }} className="fixed top-1/2 left-1/2 z-50 max-h-[90dvh] -translate-x-1/2 -translate-y-1/2 overflow-auto rounded-xl border bg-background p-6 shadow-xl outline-none">
      <div className="flex items-start justify-between gap-4"><BaseDialog.Title className="text-lg font-semibold">{title}</BaseDialog.Title><Button variant="ghost" size="icon" aria-label="关闭" disabled={busy} onClick={() => onOpenChange(false)}><X className="size-4" aria-hidden="true" /></Button></div>
      <BaseDialog.Description className="mt-2 mb-6 text-sm leading-6 text-muted-foreground">{description}</BaseDialog.Description>{children}
    </BaseDialog.Popup></BaseDialog.Portal>
  </BaseDialog.Root>
}
