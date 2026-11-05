import { Link } from 'react-router'

import { count as formatCount } from '@/lib/format'
import { cn } from '@/lib/utils'

/** A pill tab in a list column's header (folders, views). */
export function ListTab({
  to,
  label,
  count,
  active,
}: {
  to: string
  label: string
  count?: number
  active: boolean
}) {
  return (
    <Link
      to={to}
      aria-current={active ? 'page' : undefined}
      className={cn(
        'flex h-9 shrink-0 items-center gap-2 rounded-[10px] px-3 text-[14px] whitespace-nowrap text-foreground/80 transition-colors hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
        active && 'bg-muted font-medium text-foreground',
      )}
    >
      {label}
      {count !== undefined && count > 0 && (
        <span className="text-[13px] text-muted-foreground tabular-nums">{formatCount(count)}</span>
      )}
    </Link>
  )
}
