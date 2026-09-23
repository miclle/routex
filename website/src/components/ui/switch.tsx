import type { ComponentProps } from 'react'
import { Switch as BaseSwitch } from '@base-ui/react/switch'
export function Switch(props: Omit<ComponentProps<typeof BaseSwitch.Root>, 'className'>) {
  return (
    <BaseSwitch.Root
      {...props}
      className="inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full border border-transparent bg-muted p-0.5 outline-none focus-visible:ring-2 focus-visible:ring-ring data-[checked]:bg-primary data-[disabled]:cursor-not-allowed data-[disabled]:opacity-50"
    >
      <BaseSwitch.Thumb className="size-5 rounded-full bg-background shadow-sm transition-transform data-[checked]:translate-x-5" />
    </BaseSwitch.Root>
  )
}
