import { useState, type ReactNode } from 'react'
import { ChevronDown } from 'lucide-react'

import { cn } from '@/lib/utils'

/** A collapsible reader section with an optional count and right-hand aside. */
export function Section({
  title,
  count,
  defaultOpen = true,
  aside,
  children,
}: {
  title: string
  count?: number
  defaultOpen?: boolean
  aside?: ReactNode
  children: ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <section>
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          aria-expanded={open}
          className="-ml-1 flex items-center gap-2 rounded-md px-1 text-[15px] font-medium"
        >
          <ChevronDown className={cn('size-4 text-muted-foreground transition-transform', !open && '-rotate-90')} />
          {title}
          {count !== undefined && (
            <span className="text-[13px] font-normal text-muted-foreground tabular-nums">{count}</span>
          )}
        </button>
        {open && aside && <div className="ml-auto">{aside}</div>}
      </div>
      {open && <div className="mt-4">{children}</div>}
    </section>
  )
}
