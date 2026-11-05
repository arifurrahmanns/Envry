import type { ReactNode } from 'react'
import { MailOpen } from 'lucide-react'

import { EmptyState } from '@/components/mail/empty-state'
import { cn } from '@/lib/utils'

/**
 * List + reader side by side. On narrow screens only one pane shows: the
 * list, or the reader once a message is selected.
 */
export function MailPanes({ list, reader }: { list: ReactNode; reader: ReactNode | null }) {
  const hasSelection = reader !== null
  return (
    <div className="flex min-h-0 flex-1 gap-4 p-3 md:p-4">
      <div className={cn('flex min-h-0 w-full flex-col md:w-[360px] md:shrink-0 xl:w-[400px]', hasSelection && 'hidden md:flex')}>
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
    <div className="flex flex-1 rounded-xl border bg-card">
      <EmptyState
        icon={MailOpen}
        title="No message selected"
        description={
          <>
            Pick a message from the list. Use <Kbd>j</Kbd> and <Kbd>k</Kbd> to move, <Kbd>/</Kbd> to search.
          </>
        }
      />
    </div>
  )
}

function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="rounded border bg-muted px-1.5 font-mono text-[11px] text-foreground/80">{children}</kbd>
}
