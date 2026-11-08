import { useEffect, useState, type ReactNode } from 'react'
import { getCurrentWindow } from '@tauri-apps/api/window'
import { Copy, Minus, Square, X } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { inDesktopApp, isMac } from '@/lib/desktop'
import { cn } from '@/lib/utils'

/**
 * Minimize, maximize and close for the desktop app's frameless window, at
 * the right end of the page header and as tall as it, so they line up with
 * Compose and their hover areas reach the window corner. Close keeps Envry
 * running in the tray. Not shown in a browser, nor on macOS, which keeps
 * its own traffic lights.
 */
export function WindowControls({ className }: { className?: string }) {
  if (!inDesktopApp || isMac) return null
  return <Controls className={className} />
}

function Controls({ className }: { className?: string }) {
  const maximized = useMaximized()
  const win = getCurrentWindow()
  return (
    <div className={cn('flex h-full', className)}>
      <WindowButton label="Minimize" onClick={() => void win.minimize()}>
        <Minus />
      </WindowButton>
      <WindowButton label={maximized ? 'Restore' : 'Maximize'} onClick={() => void win.toggleMaximize()}>
        {maximized ? <Copy className="size-3.5 -scale-x-100" /> : <Square className="size-3.5" />}
      </WindowButton>
      <WindowButton label="Close" onClick={() => void win.close()} danger>
        <X />
      </WindowButton>
    </div>
  )
}

function WindowButton({
  label,
  onClick,
  danger = false,
  children,
}: {
  label: string
  onClick: () => void
  danger?: boolean
  children: ReactNode
}) {
  return (
    <Button
      variant="ghost"
      aria-label={label}
      title={label}
      onClick={onClick}
      className={cn(
        'h-full w-[46px] rounded-none text-muted-foreground hover:bg-foreground/10 hover:text-foreground dark:hover:bg-foreground/10',
        danger && 'hover:bg-destructive hover:text-white dark:hover:bg-destructive',
      )}
    >
      {children}
    </Button>
  )
}

/** Follows the window's maximized state, which also changes by snapping or double-clicking. */
function useMaximized() {
  const [maximized, setMaximized] = useState(false)
  useEffect(() => {
    const win = getCurrentWindow()
    let stop: (() => void) | undefined
    let done = false
    const sync = () => {
      win.isMaximized().then(setMaximized, () => {})
    }
    sync()
    win.onResized(sync).then(
      (unlisten) => (done ? unlisten() : (stop = unlisten)),
      () => {},
    )
    return () => {
      done = true
      stop?.()
    }
  }, [])
  return maximized
}
