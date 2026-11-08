import type { ReactNode } from 'react'
import { MailOpen } from 'lucide-react'
import { useDefaultLayout } from 'react-resizable-panels'

import { PaneDivider } from '@/components/layout/pane-divider'
import { EmptyState } from '@/components/mail/empty-state'
import { ResizablePanel, ResizablePanelGroup } from '@/components/ui/resizable'
import { useMediaQuery, WIDE } from '@/hooks/use-media-query'
import { layoutStorage } from '@/lib/layout-storage'
import { cn } from '@/lib/utils'

/**
 * List and detail columns, full height, divided by a draggable divider
 * (sizes are remembered). On narrow screens only one shows: the list, or
 * the detail once a message is open.
 */
export function MailPanes({ list, reader }: { list: ReactNode; reader: ReactNode | null }) {
  const wide = useMediaQuery(WIDE)
  const layout = useDefaultLayout({ id: 'envry-mail-panes', storage: layoutStorage })

  // Phones: one pane at a time, the reader once a message is open.
  if (!wide) {
    const hasSelection = reader !== null
    return (
      <div className="flex min-h-0 flex-1">
        <div className={cn('flex min-h-0 w-full flex-col', hasSelection && 'hidden')}>{list}</div>
        {hasSelection && <div className="flex min-h-0 min-w-0 flex-1 flex-col">{reader}</div>}
      </div>
    )
  }

  // Wider screens: list and reader side by side; drag the divider to resize.
  // The list keeps its width when the window resizes; the reader takes the rest.
  return (
    <ResizablePanelGroup
      orientation="horizontal"
      className="min-h-0 flex-1"
      defaultLayout={layout.defaultLayout}
      onLayoutChanged={layout.onLayoutChanged}
    >
      <ResizablePanel
        id="list"
        defaultSize={460}
        minSize={300}
        maxSize="65%"
        groupResizeBehavior="preserve-pixel-size"
        className="flex min-h-0 flex-col"
      >
        {list}
      </ResizablePanel>
      <PaneDivider />
      <ResizablePanel id="reader" minSize={400} className="flex min-h-0 min-w-0 flex-col">
        {reader ?? <NoSelection />}
      </ResizablePanel>
    </ResizablePanelGroup>
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
