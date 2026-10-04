import { useId, useState, type ReactNode } from 'react'
import { Tooltip as BaseTooltip } from '@base-ui/react/tooltip'
import { CircleHelp } from 'lucide-react'

type Props = { label: string; children: ReactNode; enabled?: boolean; canOpen?: () => boolean }
export function Tooltip({ label, children, enabled = true, canOpen = () => true }: Props) {
  const id = useId()
  const [open, setOpen] = useState(false)
  if (open && !enabled) setOpen(false)
  return (
    <BaseTooltip.Provider delay={0}>
      <BaseTooltip.Root
        open={enabled && open}
        onOpenChange={(value) => setOpen(value && enabled && canOpen())}
      >
        <BaseTooltip.Trigger
          aria-label={label}
          aria-disabled={!enabled || undefined}
          aria-describedby={enabled && open ? id : undefined}
          disabled={!enabled}
          className="inline-flex rounded-sm text-muted-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
          delay={0}
        >
          <CircleHelp aria-hidden="true" className="size-3.5" />
        </BaseTooltip.Trigger>
        {enabled && open && (
          <BaseTooltip.Portal>
            <BaseTooltip.Positioner sideOffset={6}>
              <BaseTooltip.Popup
                id={id}
                role="tooltip"
                className="z-50 max-w-sm rounded-md border bg-popover p-3 text-xs text-popover-foreground shadow-md"
              >
                {children}
              </BaseTooltip.Popup>
            </BaseTooltip.Positioner>
          </BaseTooltip.Portal>
        )}
      </BaseTooltip.Root>
    </BaseTooltip.Provider>
  )
}
