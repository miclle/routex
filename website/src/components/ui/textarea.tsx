import { Field } from '@base-ui/react/field'
import type { ComponentProps } from 'react'
import { cn } from '@/lib/utils'

export function Textarea({
  className,
  value,
  defaultValue,
  disabled,
  name,
  ...props
}: ComponentProps<'textarea'>) {
  return (
    <Field.Control
      render={<textarea {...props} />}
      value={value}
      defaultValue={defaultValue}
      disabled={disabled}
      name={name}
      className={cn(
        'min-h-24 w-full resize-y rounded-md border border-input bg-background px-3 py-2 text-sm leading-6 outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/30 disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
    />
  )
}
