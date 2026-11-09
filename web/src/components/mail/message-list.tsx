import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ChevronRight, Download, Ellipsis, Funnel, Loader2, Mail, MailOpen, Paperclip, RotateCw, Trash2 } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

import { SenderAvatar } from '@/components/mail/sender-avatar'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { urls } from '@/lib/api'
import { displayName, groupByDay, messageTime, rowTime, sourceName, viaProvider } from '@/lib/format'
import { isDelivery, type ListItem } from '@/lib/messages'
import { cn } from '@/lib/utils'

type MessageListProps = {
  /** Pill tabs on the left of the header (folders or views). */
  tabs: ReactNode
  /** DropdownMenuItems for the "…" menu. */
  menu?: ReactNode
  /** "sent" rows show the first recipient; "mailbox" rows track read state. */
  mode: 'sent' | 'mailbox'
  items: ListItem[]
  selectedId: string | null
  onSelect: (id: string) => void
  onDelete?: (id: string) => void
  onToggleSeen?: (id: string) => void
  isLoading: boolean
  error: Error | null
  onRetry: () => void
  hasNextPage: boolean
  isFetchingNextPage: boolean
  fetchNextPage: () => void
  empty: ReactNode
  className?: string
}

export function MessageList({
  tabs,
  menu,
  mode,
  items,
  selectedId,
  onSelect,
  onDelete,
  onToggleSeen,
  isLoading,
  error,
  onRetry,
  hasNextPage,
  isFetchingNextPage,
  fetchNextPage,
  empty,
  className,
}: MessageListProps) {
  const [unreadOnly, setUnreadOnly] = useState(false)
  const [withAttachments, setWithAttachments] = useState(false)
  const filtering = unreadOnly || withAttachments

  const visible = useMemo(
    () =>
      items.filter(
        (m) => (!unreadOnly || (isDelivery(m) && !m.seen)) && (!withAttachments || m.attachmentCount > 0),
      ),
    [items, unreadOnly, withAttachments],
  )
  const groups = useMemo(() => groupByDay(visible, messageTime), [visible])

  useListKeyboard(visible, selectedId, onSelect)

  const sentinel = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = sentinel.current
    if (!el || !hasNextPage) return
    const io = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting && !isFetchingNextPage) fetchNextPage()
      },
      { rootMargin: '200px' },
    )
    io.observe(el)
    return () => io.disconnect()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  return (
    <section aria-label="Messages" className={cn('flex min-h-0 flex-col', className)}>
      <div className="flex h-[76px] shrink-0 items-center gap-3 border-b px-5 md:px-7">
        <nav aria-label="Folders" className="-my-2 flex min-w-0 items-center gap-1 overflow-x-auto py-2 [scrollbar-width:none]">
          {tabs}
        </nav>
        <div className="ml-auto flex shrink-0 items-center gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="outline"
                aria-label="Filter"
                className={cn('h-9 gap-2 rounded-[10px] px-3 font-normal shadow-none', filtering && 'bg-muted')}
              >
                <Funnel className="size-4" strokeWidth={1.75} />
                <span className="hidden sm:inline">Filter</span>
                {filtering && <span className="size-1.5 rounded-full bg-brand" />}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuLabel className="text-xs text-muted-foreground">Show only</DropdownMenuLabel>
              {mode === 'mailbox' && (
                <DropdownMenuCheckboxItem checked={unreadOnly} onCheckedChange={(v) => setUnreadOnly(v === true)}>
                  Unread
                </DropdownMenuCheckboxItem>
              )}
              <DropdownMenuCheckboxItem checked={withAttachments} onCheckedChange={(v) => setWithAttachments(v === true)}>
                With attachments
              </DropdownMenuCheckboxItem>
            </DropdownMenuContent>
          </DropdownMenu>
          {menu && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" className="h-9 w-10 rounded-[10px] px-0 shadow-none" aria-label="More actions">
                  <Ellipsis className="size-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                {menu}
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain pb-4">
        {isLoading ? (
          <ListSkeleton />
        ) : error ? (
          <div className="flex flex-col items-center gap-3 px-6 py-16 text-center">
            <p className="text-sm text-muted-foreground">Couldn't load messages: {error.message}</p>
            <Button variant="outline" size="sm" onClick={onRetry}>
              <RotateCw />
              Try again
            </Button>
          </div>
        ) : visible.length === 0 ? (
          filtering && items.length > 0 ? (
            <p className="px-6 py-16 text-center text-sm text-muted-foreground">No messages match this filter.</p>
          ) : (
            empty
          )
        ) : (
          groups.map((g) => (
            <section key={g.label} aria-label={g.label}>
              <h3 className="px-5 pt-6 pb-2 text-[15px] text-muted-foreground md:px-7">{g.label}</h3>
              <ul role="listbox" aria-label={g.label}>
                {g.items.map((m) => (
                  <MessageRow
                    key={m.id}
                    item={m}
                    mode={mode}
                    selected={m.id === selectedId}
                    onSelect={() => onSelect(m.id)}
                    onDelete={onDelete ? () => onDelete(m.id) : undefined}
                    onToggleSeen={onToggleSeen ? () => onToggleSeen(m.id) : undefined}
                  />
                ))}
              </ul>
            </section>
          ))
        )}
        <div ref={sentinel} />
        {isFetchingNextPage && (
          <div className="flex justify-center py-4 text-muted-foreground">
            <Loader2 className="size-4 animate-spin" />
          </div>
        )}
      </div>
    </section>
  )
}

function MessageRow({
  item,
  mode,
  selected,
  onSelect,
  onDelete,
  onToggleSeen,
}: {
  item: ListItem
  mode: 'sent' | 'mailbox'
  selected: boolean
  onSelect: () => void
  onDelete?: () => void
  onToggleSeen?: () => void
}) {
  const delivery = isDelivery(item) ? item : null
  const unread = delivery !== null && !delivery.seen
  const recipients = mode === 'sent' ? [...item.to, ...item.cc] : []

  return (
    <li
      role="option"
      aria-selected={selected}
      data-message-id={item.id}
      onClick={onSelect}
      className={cn(
        'group relative flex cursor-pointer items-center gap-3.5 px-5 py-2.5 transition-colors hover:bg-accent md:px-7',
        selected && 'bg-accent',
      )}
    >
      <SenderAvatar name={item.from.name} address={item.from.address} size="md" />
      <div className="min-w-0 flex-1">
        <div className="flex h-6 items-center gap-2">
          <RowName from={displayName(item.from)} recipients={recipients.map(displayName)} />
          {item.attachmentCount > 0 && (
            <Paperclip className="size-3.5 shrink-0 text-muted-foreground" aria-label="Has attachments" />
          )}
          {viaProvider(item.source) && (
            <span className="shrink-0 rounded border px-1 text-[11px] font-medium text-muted-foreground">
              {sourceName(item.source)}
            </span>
          )}
          {delivery?.rcptType === 'bcc' && (
            <span className="shrink-0 rounded border px-1 text-[10px] font-medium tracking-wide text-muted-foreground uppercase">
              Bcc
            </span>
          )}
          <span className="ml-auto flex shrink-0 items-center gap-2 pl-2">
            <span className={cn('text-[13px] text-muted-foreground tabular-nums', 'group-hover:hidden')}>
              {rowTime(messageTime(item))}
            </span>
            {unread && <span aria-label="Unread" className="size-2 rounded-full bg-brand group-hover:hidden" />}
            <span className="hidden items-center gap-0.5 group-hover:flex">
              {onToggleSeen && (
                <RowAction
                  icon={unread ? MailOpen : Mail}
                  label={unread ? 'Mark as read' : 'Mark as unread'}
                  onClick={onToggleSeen}
                />
              )}
              {mode === 'sent' && <RowAction icon={Download} label="Download .eml" href={urls.raw(item.id, true)} />}
              {onDelete && <RowAction icon={Trash2} label="Delete" onClick={onDelete} />}
            </span>
          </span>
        </div>
        <p className="truncate text-[14px] text-foreground/75">
          <span className="font-semibold text-foreground">{item.subject || '(no subject)'}</span>
          {item.preview && <> — {item.preview}</>}
        </p>
      </div>
    </li>
  )
}

/**
 * The row's title: the sender, and in Sent who it went to. The recipient is
 * lighter than the sender, after a small chevron, with "+2" for the rest.
 */
function RowName({ from, recipients }: { from: string; recipients: string[] }) {
  if (recipients.length === 0) {
    return <span className="truncate text-[14px] font-semibold">{from}</span>
  }
  return (
    <span className="flex min-w-0 items-center gap-1 text-[14px]">
      {/* The sender keeps up to 60% of the width; the recipient is cut first. */}
      <span className="max-w-[60%] shrink-0 truncate font-semibold">{from}</span>
      <ChevronRight aria-label="to" className="size-3.5 shrink-0 text-muted-foreground/60" />
      <span className="truncate text-foreground/70">{recipients[0]}</span>
      {recipients.length > 1 && (
        <span className="shrink-0 rounded-full bg-muted px-1.5 text-[11px] leading-[18px] font-medium text-muted-foreground tabular-nums">
          +{recipients.length - 1}
        </span>
      )}
    </span>
  )
}

function RowAction({
  icon: Icon,
  label,
  onClick,
  href,
}: {
  icon: LucideIcon
  label: string
  onClick?: () => void
  href?: string
}) {
  const className =
    'flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground'
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        {href ? (
          <a href={href} download aria-label={label} className={className} onClick={(e) => e.stopPropagation()}>
            <Icon className="size-[17px]" strokeWidth={1.75} />
          </a>
        ) : (
          <button
            type="button"
            aria-label={label}
            className={className}
            onClick={(e) => {
              e.stopPropagation()
              onClick?.()
            }}
          >
            <Icon className="size-[17px]" strokeWidth={1.75} />
          </button>
        )}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

function ListSkeleton() {
  return (
    <div aria-busy="true" aria-label="Loading messages">
      <Skeleton className="mx-5 mt-7 mb-3 h-4 w-16 md:mx-7" />
      {Array.from({ length: 7 }, (_, i) => (
        <div key={i} className="flex items-center gap-3.5 px-5 py-3 md:px-7">
          <Skeleton className="size-9 rounded-full" />
          <div className="flex-1 space-y-2">
            <div className="flex justify-between gap-4">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="h-3 w-12" />
            </div>
            <Skeleton className="h-3.5 w-11/12" />
          </div>
        </div>
      ))}
    </div>
  )
}

/** j / k (or arrow keys) move through the list. */
function useListKeyboard(items: ListItem[], selectedId: string | null, onSelect: (id: string) => void) {
  const state = useRef({ items, selectedId, onSelect })
  useEffect(() => {
    state.current = { items, selectedId, onSelect }
  })

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return
      const target = e.target as HTMLElement | null
      if (target?.closest('input, textarea, select, [contenteditable="true"], [role="dialog"], [role="menu"]')) return
      const step = e.key === 'j' || e.key === 'ArrowDown' ? 1 : e.key === 'k' || e.key === 'ArrowUp' ? -1 : 0
      if (!step) return
      const { items, selectedId, onSelect } = state.current
      if (items.length === 0) return
      e.preventDefault()
      const i = items.findIndex((m) => m.id === selectedId)
      const next = items[Math.max(0, Math.min(items.length - 1, i === -1 ? 0 : i + step))]
      onSelect(next.id)
      document.querySelector(`[data-message-id="${CSS.escape(next.id)}"]`)?.scrollIntoView({ block: 'nearest' })
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
}
