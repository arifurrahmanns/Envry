import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Search, X } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

import { MobileNav } from '@/components/layout/mobile-nav'
import { useShell } from '@/components/layout/shell-context'
import { useSearchQuery } from '@/hooks/use-search-query'
import { count } from '@/lib/format'
import { useInfo } from '@/lib/queries'
import { cn } from '@/lib/utils'

type PageHeaderProps = {
  icon: LucideIcon
  title: ReactNode
  total?: number
  unit?: [singular: string, plural: string]
  searchPlaceholder?: string
}

export function PageHeader({
  icon: Icon,
  title,
  total,
  unit = ['email', 'emails'],
  searchPlaceholder = 'Search mail',
}: PageHeaderProps) {
  return (
    <header className="flex h-16 shrink-0 items-center gap-3 border-b px-4 md:px-6">
      <MobileNav />
      <Icon className="hidden size-5 shrink-0 text-muted-foreground sm:block" strokeWidth={1.75} />
      <h1 className="truncate text-[15px] font-medium text-foreground/85">{title}</h1>
      {total !== undefined && (
        <>
          <span aria-hidden className="size-1 shrink-0 rounded-full bg-muted-foreground/40" />
          <span className="shrink-0 text-[15px] text-foreground tabular-nums">
            {count(total)} {total === 1 ? unit[0] : unit[1]}
          </span>
        </>
      )}
      <ListeningOn />
      <SearchBox placeholder={searchPlaceholder} />
    </header>
  )
}

function ListeningOn() {
  const { live } = useShell()
  const { data } = useInfo()
  const host = window.location.hostname || 'localhost'
  const port = data?.ports.smtp ?? 1025

  return (
    <div className="ml-4 hidden items-center gap-2 text-[15px] lg:flex">
      <span className="text-muted-foreground">Listening on</span>
      <span className="flex items-center gap-1.5 font-medium text-foreground">
        <span
          aria-hidden
          className={cn(
            'size-2 rounded-full',
            live === 'live' ? 'bg-brand' : live === 'offline' ? 'bg-muted-foreground/40' : 'animate-pulse bg-brand/50',
          )}
        />
        <span className="font-mono text-sm">
          {host}:{port}
        </span>
      </span>
      <span className="sr-only">{live === 'live' ? 'Live updates on' : 'Live updates off'}</span>
    </div>
  )
}

function SearchBox({ placeholder }: { placeholder: string }) {
  const [q, setQ] = useSearchQuery()
  const [value, setValue] = useState(q)
  const [syncedQ, setSyncedQ] = useState(q)
  const ref = useRef<HTMLInputElement>(null)

  // Keep the box in sync when navigation changes ?q=.
  if (q !== syncedQ) {
    setSyncedQ(q)
    setValue(q)
  }

  useEffect(() => {
    if (value === q) return
    const t = window.setTimeout(() => setQ(value), 250)
    return () => window.clearTimeout(t)
  }, [value, q, setQ])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null
      const typing = target?.closest('input, textarea, select, [contenteditable="true"]')
      if (e.key === '/' && !typing) {
        e.preventDefault()
        ref.current?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  return (
    <label className="relative ml-auto flex h-9 w-40 items-center rounded-lg border bg-background text-sm shadow-xs focus-within:ring-[3px] focus-within:ring-ring/40 sm:w-64">
      <Search className="pointer-events-none absolute left-3 size-4 text-muted-foreground" />
      <span className="sr-only">Search</span>
      <input
        ref={ref}
        type="search"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            setValue('')
            e.currentTarget.blur()
          }
        }}
        placeholder={placeholder}
        className="h-full w-full bg-transparent pr-8 pl-9 outline-none placeholder:text-muted-foreground [&::-webkit-search-cancel-button]:hidden"
      />
      {value ? (
        <button
          type="button"
          onClick={() => setValue('')}
          className="absolute right-2 rounded p-0.5 text-muted-foreground hover:text-foreground"
          aria-label="Clear search"
        >
          <X className="size-3.5" />
        </button>
      ) : (
        <kbd className="pointer-events-none absolute right-2.5 hidden rounded border bg-muted px-1.5 font-mono text-[11px] text-muted-foreground sm:block">
          /
        </kbd>
      )}
    </label>
  )
}
