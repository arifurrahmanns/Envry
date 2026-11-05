import { useCallback, useMemo, useState } from 'react'
import { Outlet, useNavigate } from 'react-router'

import { NavRail } from '@/components/layout/nav-rail'
import { ShellContext, type ComposeDraft, type ShellContextValue } from '@/components/layout/shell-context'
import { ComposeDialog } from '@/features/compose/compose-dialog'
import { ConnectDialog } from '@/features/connect/connect-dialog'
import { useLiveEvents } from '@/hooks/use-live-events'

/**
 * The window frame: icon rail on the left and an inset content panel that
 * each page fills with its header, toolbar and panes.
 */
export function AppShell() {
  const navigate = useNavigate()
  const [compose, setCompose] = useState<{ open: boolean; draft: ComposeDraft }>({ open: false, draft: {} })
  const [connectOpen, setConnectOpen] = useState(false)

  const openMessage = useCallback((id: string) => navigate(`/sent?m=${encodeURIComponent(id)}`), [navigate])
  const live = useLiveEvents(openMessage)

  const value = useMemo<ShellContextValue>(
    () => ({
      live,
      openCompose: (draft = {}) => setCompose({ open: true, draft }),
      openConnect: () => setConnectOpen(true),
    }),
    [live],
  )

  return (
    <ShellContext.Provider value={value}>
      <div className="flex h-svh bg-canvas md:p-3">
        <div className="flex min-w-0 flex-1 overflow-hidden bg-sidebar md:rounded-[20px] md:border md:shadow-sm">
          <NavRail />
          <main className="flex min-w-0 flex-1 flex-col overflow-hidden bg-background md:mt-2 md:rounded-tl-2xl md:border-t md:border-l">
            <Outlet />
          </main>
        </div>
      </div>
      <ComposeDialog
        open={compose.open}
        draft={compose.draft}
        onOpenChange={(open) => setCompose((c) => ({ ...c, open }))}
      />
      <ConnectDialog open={connectOpen} onOpenChange={setConnectOpen} />
    </ShellContext.Provider>
  )
}
