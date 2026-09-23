import type { ReactNode } from 'react'
import { Dialog as BaseDialog } from '@base-ui/react/dialog'
import { X } from 'lucide-react'
import { Button } from './button'

export function Drawer({ open, onOpenChange, title, description, children, busy = false, side = 'right', width = 736, bodyClassName = 'p-6' }: { open: boolean; onOpenChange: (open: boolean) => void; title: string; description?: string; children: ReactNode; busy?: boolean; side?: 'left' | 'right'; width?: number; bodyClassName?: string }) {
  return <BaseDialog.Root open={open} onOpenChange={(value) => { if (!busy) onOpenChange(value) }}><BaseDialog.Portal><BaseDialog.Backdrop className="fixed inset-0 z-50 bg-black/40" /><BaseDialog.Popup style={{ width, maxWidth: '100vw' }} className={`fixed inset-y-0 z-50 flex flex-col bg-background shadow-xl outline-none ${side === 'left' ? 'left-0' : 'right-0'}`}><header className="flex min-h-16 items-center gap-3 border-b px-6"><Button variant="ghost" size="icon" aria-label="关闭" disabled={busy} onClick={() => onOpenChange(false)}><X className="size-4" /></Button><BaseDialog.Title className="font-semibold">{title}</BaseDialog.Title></header><div className={`min-h-0 flex-1 overflow-auto ${bodyClassName}`}>{description && <BaseDialog.Description className="mb-6 text-sm text-muted-foreground">{description}</BaseDialog.Description>}{children}</div></BaseDialog.Popup></BaseDialog.Portal></BaseDialog.Root>
}
