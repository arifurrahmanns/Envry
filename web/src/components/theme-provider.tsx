import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'

export type Theme = 'dark' | 'light' | 'system'

// Keep in sync with the inline script in index.html.
export const THEME_STORAGE_KEY = 'envry-ui-theme'

type ThemeProviderState = {
  theme: Theme
  resolvedTheme: 'dark' | 'light'
  setTheme: (theme: Theme) => void
}

const ThemeProviderContext = createContext<ThemeProviderState | null>(null)

const darkQuery = '(prefers-color-scheme: dark)'

function readStoredTheme(): Theme {
  try {
    const v = localStorage.getItem(THEME_STORAGE_KEY)
    if (v === 'dark' || v === 'light' || v === 'system') return v
  } catch {
    // storage blocked (private mode etc.): fall through to default
  }
  return 'dark'
}

function resolve(theme: Theme): 'dark' | 'light' {
  if (theme !== 'system') return theme
  return window.matchMedia(darkQuery).matches ? 'dark' : 'light'
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(readStoredTheme)
  const [resolvedTheme, setResolvedTheme] = useState(() => resolve(theme))

  useEffect(() => {
    const apply = () => {
      const next = resolve(theme)
      setResolvedTheme(next)
      const root = document.documentElement
      root.classList.toggle('dark', next === 'dark')
      root.style.colorScheme = next
    }
    apply()
    if (theme !== 'system') return
    const mql = window.matchMedia(darkQuery)
    mql.addEventListener('change', apply)
    return () => mql.removeEventListener('change', apply)
  }, [theme])

  const setTheme = (next: Theme) => {
    try {
      localStorage.setItem(THEME_STORAGE_KEY, next)
    } catch {
      // ignore: the choice still applies for this session
    }
    setThemeState(next)
  }

  return (
    <ThemeProviderContext.Provider value={{ theme, resolvedTheme, setTheme }}>
      {children}
    </ThemeProviderContext.Provider>
  )
}

// oxlint-disable-next-line react/only-export-components
export function useTheme() {
  const ctx = useContext(ThemeProviderContext)
  if (!ctx) throw new Error('useTheme must be used within a ThemeProvider')
  return ctx
}
