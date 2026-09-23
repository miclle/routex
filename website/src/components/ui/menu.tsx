import type { ReactNode } from 'react'
import { Menu as BaseMenu } from '@base-ui/react/menu'

export function Menu({
  trigger,
  label,
  children,
}: {
  trigger: ReactNode
  label: string
  children: ReactNode
}) {
  return (
    <BaseMenu.Root>
      <BaseMenu.Trigger
        className="flex h-10 w-full items-center gap-2 rounded-md px-2 text-left text-sm hover:bg-accent"
        aria-label={label}
      >
        {trigger}
      </BaseMenu.Trigger>
      <BaseMenu.Portal>
        <BaseMenu.Positioner side="top" align="start" sideOffset={8} className="z-50">
          <BaseMenu.Popup className="min-w-60 rounded-lg border bg-popover p-1 text-popover-foreground shadow-lg outline-none">
            {children}
          </BaseMenu.Popup>
        </BaseMenu.Positioner>
      </BaseMenu.Portal>
    </BaseMenu.Root>
  )
}
export function MenuItem({
  children,
  onClick,
  disabled = false,
}: {
  children: ReactNode
  onClick: () => void
  disabled?: boolean
}) {
  return (
    <BaseMenu.Item
      disabled={disabled}
      onClick={onClick}
      className="flex cursor-default items-center gap-2 rounded-sm px-3 py-2 text-sm outline-none data-[highlighted]:bg-accent data-[disabled]:opacity-50"
    >
      {children}
    </BaseMenu.Item>
  )
}
