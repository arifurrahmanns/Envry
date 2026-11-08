import { isTauri } from '@tauri-apps/api/core'

/** True inside the Envry desktop app, whose window has no native title bar. */
export const inDesktopApp = isTauri()

/** On macOS the desktop app keeps the native traffic lights (top left). */
export const isMac = /Mac/.test(navigator.userAgent)

/**
 * Props that make an element a window drag region in the desktop app:
 * pressing on it (not on its buttons or links) moves the window, and a
 * double-click maximizes. "deep" covers the element's whole subtree.
 */
export function dragRegion(deep = true) {
  return inDesktopApp ? { 'data-tauri-drag-region': deep ? 'deep' : '' } : {}
}
