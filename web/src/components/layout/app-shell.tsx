import { useCallback, useMemo, useState } from 'react'
import { Outlet, useNavigate } from 'react-router'
import { useDefaultLayout, usePanelRef } from 'react-resizable-panels'

import { ShellContext, type ShellContextValue } from '@/components/layout/shell-context'
import { PaneDivider } from '@/components/layout/pane-divider'
import { Sidebar } from '@/components/layout/sidebar'
import { ResizablePanel, ResizablePanelGroup } from '@/components/ui/resizable'
import { CreateAccountDialog } from '@/features/inbox/create-account-dialog'
import { useLiveEvents } from '@/hooks/use-live-events'
import { useMediaQuery, WIDE } from '@/hooks/use-media-query'
import { inDesktopApp } from '@/lib/desktop'
import { layoutStorage } from '@/lib/layout-storage'
import { cn } from '@/lib/utils'

const COLLAPSE_KEY = 'envry-sidebar-collapsed'

// Sidebar widths in pixels; dragging it narrower than the minimum collapses
// it to the icon rail.
const SIDEBAR = { default: 264, min: 200, max: 360, collapsed: 76 }

function readCollapsed() {
  try {
    return localStorage.getItem(COLLAPSE_KEY) === '1'
  } catch {
    return false
  }
}

function saveCollapsed(collapsed: boolean) {
  try {
    localStorage.setItem(COLLAPSE_KEY, collapsed ? '1' : '0')
  } catch {
    // ignore: the choice still applies for this session
  }
}

/**
 * The window frame: a labeled sidebar and the main area that each page
 * fills with its header and columns. In a browser it floats on a canvas;
 * in the desktop app it fills the window, whose corners the OS rounds.
 */
export function AppShell() {
  const navigate = useNavigate()
  const [accountOpen, setAccountOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(readCollapsed)
  const wide = useMediaQuery(WIDE)
  const sidebar = usePanelRef()
  const layout = useDefaultLayout({ id: 'envry-shell', storage: layoutStorage })

  const toggleSidebar = () => {
    if (collapsed) sidebar.current?.expand()
    else sidebar.current?.collapse()
  }
  // Follows the pane, which also collapses or expands by dragging.
  const onSidebarResize = () => {
    const now = sidebar.current?.isCollapsed() ?? false
    if (now !== collapsed) {
      setCollapsed(now)
      saveCollapsed(now)
    }
  }

  const openMessage = useCallback((id: string) => navigate(`/sent?m=${encodeURIComponent(id)}`), [navigate])
  const live = useLiveEvents(openMessage)

  const value = useMemo<ShellContextValue>(
    () => ({
      live,
      // Compose and Connection are pages (see App.tsx).
      openCompose: (draft = {}) => navigate('/compose', { state: draft }),
      openConnect: () => navigate('/connection'),
      openCreateAccount: () => setAccountOpen(true),
    }),
    [live, navigate],
  )

  return (
    <ShellContext.Provider value={value}>
      <div className={cn('flex h-svh', inDesktopApp ? 'bg-background' : 'bg-canvas md:p-4')}>
        <div
          className={cn(
            'flex min-w-0 flex-1 overflow-hidden bg-background',
            !inDesktopApp && 'md:rounded-[28px] md:border md:shadow-2xl md:shadow-black/20',
          )}
        >
          {wide ? (
            <ResizablePanelGroup
              orientation="horizontal"
              defaultLayout={layout.defaultLayout}
              onLayoutChanged={layout.onLayoutChanged}
            >
              <ResizablePanel
                id="sidebar"
                panelRef={sidebar}
                defaultSize={collapsed ? SIDEBAR.collapsed : SIDEBAR.default}
                minSize={SIDEBAR.min}
                maxSize={SIDEBAR.max}
                collapsible
                collapsedSize={SIDEBAR.collapsed}
                groupResizeBehavior="preserve-pixel-size"
                onResize={onSidebarResize}
              >
                <Sidebar collapsed={collapsed} onToggle={toggleSidebar} />
              </ResizablePanel>
              <PaneDivider />
              {/* Room for the list and reader at their minimum widths (see MailPanes). */}
              <ResizablePanel id="main" minSize={701} className="flex min-w-0 flex-col">
                <main className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
                  <Outlet />
                </main>
              </ResizablePanel>
            </ResizablePanelGroup>
          ) : (
            <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
              <Outlet />
            </main>
          )}
        </div>
      </div>
      <CreateAccountDialog
        open={accountOpen}
        onOpenChange={setAccountOpen}
        onCreated={(address) => navigate(`/inbox/${encodeURIComponent(address)}`)}
      />
    </ShellContext.Provider>
  )
}
