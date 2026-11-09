import { useEffect, useState } from 'react'
import { invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'

import { inDesktopApp } from '@/lib/desktop'

/**
 * The desktop app's updater (desktop/src-tauri/src/update.rs): a newer
 * version is looked for, downloaded in the background, then ready to
 * install with a restart.
 */
export type AppUpdate =
  | { state: 'idle' | 'checking' }
  | { state: 'downloading' | 'ready' | 'installing'; version: string }

/** The update state in the desktop app; null in a browser. */
export function useAppUpdate(): AppUpdate | null {
  const [update, setUpdate] = useState<AppUpdate | null>(null)
  useEffect(() => {
    if (!inDesktopApp) return
    let alive = true
    let unlisten: (() => void) | undefined
    invoke<AppUpdate>('update_status').then(
      (s) => alive && setUpdate(s),
      () => {},
    )
    listen<AppUpdate>('update-status', (e) => setUpdate(e.payload)).then(
      (stop) => (alive ? (unlisten = stop) : stop()),
      () => {},
    )
    return () => {
      alive = false
      unlisten?.()
    }
  }, [])
  return update
}

/** Stops Envry, installs the downloaded update and restarts the app. */
export function installUpdate() {
  return invoke('install_update')
}
