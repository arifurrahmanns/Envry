import { useSyncExternalStore } from 'react'

/** Tailwind's `md` breakpoint: side-by-side panes from here up. */
export const WIDE = '(min-width: 768px)'

/** Whether the media query matches, updating when it changes. */
export function useMediaQuery(query: string) {
  return useSyncExternalStore(
    (onChange) => {
      const mql = window.matchMedia(query)
      mql.addEventListener('change', onChange)
      return () => mql.removeEventListener('change', onChange)
    },
    () => window.matchMedia(query).matches,
  )
}
