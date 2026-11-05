import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { MailPlus } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

import { useShell } from '@/components/layout/shell-context'
import { Button } from '@/components/ui/button'
import { count as formatCount } from '@/lib/format'
import { cn } from '@/lib/utils'

export function Toolbar({ children, actions }: { children?: ReactNode; actions?: ReactNode }) {
  const { openCompose } = useShell()

  return (
    <div className="flex h-[60px] shrink-0 items-center gap-3 border-b px-4 md:px-6">
      <Button onClick={() => openCompose()} className="h-9 gap-2 rounded-lg px-3.5">
        <MailPlus className="size-[18px]" strokeWidth={1.75} />
        Compose
      </Button>
      {children && (
        <>
          <span aria-hidden className="h-6 w-px shrink-0 bg-border" />
          <nav aria-label="Folders" className="-my-2 flex min-w-0 items-center gap-1 overflow-x-auto py-2">
            {children}
          </nav>
        </>
      )}
      {actions && <div className="ml-auto flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  )
}

type ToolbarTabProps = {
  to: string
  icon: LucideIcon
  label: string
  count?: number
  active: boolean
}

export function ToolbarTab({ to, icon: Icon, label, count, active }: ToolbarTabProps) {
  return (
    <Link
      to={to}
      aria-current={active ? 'page' : undefined}
      className={cn(
        'flex h-9 shrink-0 items-center gap-2 rounded-lg px-3 text-[15px] whitespace-nowrap text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
        active && 'bg-muted text-foreground',
      )}
    >
      <Icon className="size-[18px]" strokeWidth={1.75} />
      {label}
      {count !== undefined && count > 0 && (
        <span className={cn('text-sm tabular-nums', active ? 'text-foreground/80' : 'text-muted-foreground')}>
          {formatCount(count)}
        </span>
      )}
    </Link>
  )
}
