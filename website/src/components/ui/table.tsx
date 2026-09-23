import type { ComponentProps } from 'react'
import { cn } from '@/lib/utils'
export function Table({ className, ...props }: ComponentProps<'table'>) { return <div className="w-full overflow-x-auto"><table className={cn('w-full text-left text-sm [&_th]:whitespace-nowrap [&_th]:border-b [&_th]:bg-muted/50 [&_th]:px-4 [&_th]:py-3 [&_th]:font-medium [&_td]:border-b [&_td]:px-4 [&_td]:py-3 [&_tbody_tr:hover]:bg-muted/30', className)} {...props} /></div> }
