import { createContext, useContext } from 'react'

import type { LiveStatus } from '@/hooks/use-live-events'

export type ComposeDraft = {
  to?: string
  subject?: string
}

export type ShellContextValue = {
  live: LiveStatus
  openCompose: (draft?: ComposeDraft) => void
  openConnect: () => void
  openCreateAccount: () => void
}

export const ShellContext = createContext<ShellContextValue | null>(null)

export function useShell(): ShellContextValue {
  const ctx = useContext(ShellContext)
  if (!ctx) throw new Error('useShell must be used within AppShell')
  return ctx
}
