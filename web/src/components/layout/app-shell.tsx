import { useCallback, useMemo, useState } from 'react'
import { Outlet, useNavigate } from 'react-router'

import { ShellContext, type ComposeDraft, type ShellContextValue } from '@/components/layout/shell-context'
import { Sidebar } from '@/components/layout/sidebar'
import { ComposeDialog } from '@/features/compose/compose-dialog'
import { ConnectDialog } from '@/features/connect/connect-dialog'
import { CreateAccountDialog } from '@/features/inbox/create-account-dialog'
import { useLiveEvents } from '@/hooks/use-live-events'

const COLLAPSE_KEY = 'envry-sidebar-collapsed'

function readCollapsed() {
  try {
    return localStorage.getItem(COLLAPSE_KEY) === '1'
  } catch {
    return false
  }
}

/**
 * The window frame: a labeled sidebar and the main area that each page
 * fills with its header and columns.
 */
export function AppShell() {
  const navigate = useNavigate()
  const [compose, setCompose] = useState<{ open: boolean; draft: ComposeDraft }>({ open: false, draft: {} })
  const [connectOpen, setConnectOpen] = useState(false)
  const [accountOpen, setAccountOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(readCollapsed)

  const toggleSidebar = () => {
    setCollapsed((c) => {
      try {
        localStorage.setItem(COLLAPSE_KEY, c ? '0' : '1')
      } catch {
        // ignore: the choice still applies for this session
      }
      return !c
    })
  }

  const openMessage = useCallback((id: string) => navigate(`/sent?m=${encodeURIComponent(id)}`), [navigate])
  const live = useLiveEvents(openMessage)

  const value = useMemo<ShellContextValue>(
    () => ({
      live,
      openCompose: (draft = {}) => setCompose({ open: true, draft }),
      openConnect: () => setConnectOpen(true),
      openCreateAccount: () => setAccountOpen(true),
    }),
    [live],
  )

  return (
    <ShellContext.Provider value={value}>
      <div className="flex h-svh bg-canvas md:p-4">
        <div className="flex min-w-0 flex-1 overflow-hidden bg-background md:rounded-[28px] md:border md:shadow-2xl md:shadow-black/20">
          <Sidebar collapsed={collapsed} onToggle={toggleSidebar} />
          <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
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
      <CreateAccountDialog
        open={accountOpen}
        onOpenChange={setAccountOpen}
        onCreated={(address) => navigate(`/inbox/${encodeURIComponent(address)}`)}
      />
    </ShellContext.Provider>
  )
}
