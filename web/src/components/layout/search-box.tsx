import { useEffect, useRef, useState } from 'react'
import { Search, X } from 'lucide-react'

import { useSearchQuery } from '@/hooks/use-search-query'
import { cn } from '@/lib/utils'

const isMac = typeof navigator !== 'undefined' && /mac|iphone|ipad/i.test(navigator.platform)

/** Searches the current view (writes ?q=). Focus with "/" or ⌘/Ctrl+K. */
export function SearchBox({ className }: { className?: string }) {
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
      const modK = e.key.toLowerCase() === 'k' && (e.metaKey || e.ctrlKey)
      if (modK || (e.key === '/' && !typing)) {
        e.preventDefault()
        ref.current?.focus()
        ref.current?.select()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  return (
    <label
      className={cn(
        'relative flex h-10 items-center rounded-xl border bg-transparent text-[14px] transition-colors focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/25',
        className,
      )}
    >
      <Search className="pointer-events-none absolute left-3 size-[18px] text-muted-foreground" strokeWidth={1.75} />
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
        placeholder="Search..."
        className="h-full w-full min-w-0 bg-transparent pr-16 pl-10 outline-none placeholder:text-muted-foreground [&::-webkit-search-cancel-button]:hidden"
      />
      {value ? (
        <button
          type="button"
          onClick={() => setValue('')}
          className="absolute right-2.5 rounded p-0.5 text-muted-foreground hover:text-foreground"
          aria-label="Clear search"
        >
          <X className="size-4" />
        </button>
      ) : (
        <span aria-hidden className="pointer-events-none absolute right-2 flex gap-1">
          <Kbd>{isMac ? '⌘' : 'Ctrl'}</Kbd>
          <Kbd>K</Kbd>
        </span>
      )}
    </label>
  )
}

function Kbd({ children }: { children: string }) {
  return (
    <kbd className="flex h-6 min-w-6 items-center justify-center rounded-md border bg-muted px-1.5 font-sans text-[12px] text-muted-foreground">
      {children}
    </kbd>
  )
}
