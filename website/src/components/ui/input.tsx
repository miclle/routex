import { Input as BaseInput } from '@base-ui/react/input'
import { cn } from '@/lib/utils'

function Input({ className, ...props }: BaseInput.Props) {
  return (
    <BaseInput
      className={cn(
        'h-11 w-full rounded-md border border-input bg-background px-3 text-sm outline-none transition-colors placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/30 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive',
        className,
      )}
      {...props}
    />
  )
}

export { Input }
