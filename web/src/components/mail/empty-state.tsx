import type { ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'

import { cn } from '@/lib/utils'

export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  className,
}: {
  icon: LucideIcon
  title: string
  description?: ReactNode
  action?: ReactNode
  className?: string
}) {
  return (
    <div className={cn('flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center', className)}>
      <div className="flex size-12 items-center justify-center rounded-xl border bg-background text-muted-foreground shadow-xs">
        <Icon className="size-5" strokeWidth={1.75} />
      </div>
      <div className="space-y-1">
        <p className="text-[15px] font-medium">{title}</p>
        {description && (
          <p className="mx-auto max-w-xs text-sm text-pretty text-muted-foreground">{description}</p>
        )}
      </div>
      {action && <div className="mt-1">{action}</div>}
    </div>
  )
}
