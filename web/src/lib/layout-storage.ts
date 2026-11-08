import type { LayoutStorage } from 'react-resizable-panels'

/**
 * localStorage for remembered pane sizes. Storage can be unavailable
 * (private windows, blocked site data); the panes then use their defaults.
 */
export const layoutStorage: LayoutStorage = {
  getItem(key) {
    try {
      return localStorage.getItem(key)
    } catch {
      return null
    }
  },
  setItem(key, value) {
    try {
      localStorage.setItem(key, value)
    } catch {
      // ignore: the layout still applies until the page is reloaded
    }
  },
}
