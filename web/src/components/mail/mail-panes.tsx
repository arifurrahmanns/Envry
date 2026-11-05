import type { ReactNode } from 'react'
import { MailOpen } from 'lucide-react'

import { EmptyState } from '@/components/mail/empty-state'
import { cn } from '@/lib/utils'

/**
 * List and detail columns, full height, divided by a border. On narrow
 * screens only one shows: the list, or the detail once a message is open.
 */
export function MailPanes({ list, reader }: { list: ReactNode; reader: ReactNode | null }) {
  const hasSelection = reader !== null
  return (
    <div className="flex min-h-0 flex-1">
      <div
        className={cn(
          'flex min-h-0 w-full flex-col md:w-[420px] md:shrink-0 md:border-r lg:w-[460px] xl:w-[508px]',
          hasSelection && 'hidden md:flex',
        )}
      >
        {list}
      </div>
      <div className={cn('min-h-0 min-w-0 flex-1 flex-col', hasSelection ? 'flex' : 'hidden md:flex')}>
        {reader ?? <NoSelection />}
      </div>
    </div>
  )
}

function NoSelection() {
  return (
    <EmptyState
      icon={MailOpen}
      title="No message selected"
      description={
        <>
          Pick a message from the list. Use <Kbd>j</Kbd> and <Kbd>k</Kbd> to move, <Kbd>/</Kbd> to search.
        </>
      }
    />
  )
}

function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="rounded border bg-muted px-1.5 font-mono text-[11px] text-foreground/80">{children}</kbd>
}
