import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Check, ListFilter, Loader2, Paperclip, RotateCw, Trash2 } from 'lucide-react'

import { SenderAvatar } from '@/components/mail/sender-avatar'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { displayName, listTime, messageTime } from '@/lib/format'
import { isDelivery, type ListItem } from '@/lib/messages'
import { cn } from '@/lib/utils'

type MessageListProps = {
  /** Header label, or a control such as an account switcher. */
  title: ReactNode
  banner?: ReactNode
  /** "sent" shows recipients per row; "mailbox" shows the sender. */
  mode: 'sent' | 'mailbox'
  items: ListItem[]
  selectedId: string | null
  onSelect: (id: string) => void
  onDelete?: (ids: string[]) => Promise<unknown>
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
  title,
  banner,
  mode,
  items,
  selectedId,
  onSelect,
  onDelete,
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
  const [checkedIds, setChecked] = useState<Set<string>>(new Set())
  const [deleting, setDeleting] = useState(false)

  const visible = useMemo(
    () =>
      items.filter(
        (m) =>
          (!unreadOnly || (isDelivery(m) && !m.seen)) && (!withAttachments || m.attachmentCount > 0),
      ),
    [items, unreadOnly, withAttachments],
  )

  // Only rows still on screen count as checked (others were deleted or filtered out).
  const checked = useMemo(() => {
    const ids = new Set(visible.map((m) => m.id))
    return new Set([...checkedIds].filter((id) => ids.has(id)))
  }, [checkedIds, visible])

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

  const allChecked = visible.length > 0 && checked.size === visible.length
  const someChecked = checked.size > 0 && !allChecked
  const filtering = unreadOnly || withAttachments

  const toggle = (id: string) =>
    setChecked(() => {
      const next = new Set(checked)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const deleteChecked = async () => {
    if (!onDelete) return
    setDeleting(true)
    try {
      await onDelete([...checked])
      setChecked(new Set())
    } finally {
      setDeleting(false)
    }
  }

  return (
    <section
      aria-label="Messages"
      className={cn('flex min-h-0 flex-col overflow-hidden rounded-xl border bg-card', className)}
    >
      <div className="flex h-[52px] shrink-0 items-center gap-3 border-b px-4">
        <Checkbox
          aria-label={allChecked ? 'Clear selection' : 'Select all'}
          checked={allChecked ? true : someChecked ? 'indeterminate' : false}
          onCheckedChange={() => setChecked(allChecked ? new Set() : new Set(visible.map((m) => m.id)))}
          disabled={visible.length === 0 || !onDelete}
          className="size-[18px] rounded-[5px]"
        />
        {checked.size > 0 ? (
          <>
            <span className="text-[15px] tabular-nums">{checked.size} selected</span>
            <Button
              variant="ghost"
              size="icon-sm"
              className="ml-auto text-muted-foreground"
              onClick={() => void deleteChecked()}
              disabled={deleting}
              aria-label="Delete selected"
            >
              {deleting ? <Loader2 className="animate-spin" /> : <Trash2 />}
            </Button>
          </>
        ) : (
          <>
            <div className="min-w-0 flex-1 truncate text-[15px] text-foreground/85">{title}</div>
            <DropdownMenu>
              <Tooltip>
                <TooltipTrigger asChild>
                  <DropdownMenuTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      className={cn('text-muted-foreground', filtering && 'bg-muted text-foreground')}
                      aria-label="Filter"
                    >
                      <ListFilter className="size-[18px]" strokeWidth={1.75} />
                    </Button>
                  </DropdownMenuTrigger>
                </TooltipTrigger>
                <TooltipContent>Filter</TooltipContent>
              </Tooltip>
              <DropdownMenuContent align="end" className="w-48">
                <DropdownMenuLabel className="text-xs text-muted-foreground">Show only</DropdownMenuLabel>
                {mode === 'mailbox' && (
                  <DropdownMenuCheckboxItem checked={unreadOnly} onCheckedChange={(v) => setUnreadOnly(v === true)}>
                    Unread
                  </DropdownMenuCheckboxItem>
                )}
                <DropdownMenuCheckboxItem
                  checked={withAttachments}
                  onCheckedChange={(v) => setWithAttachments(v === true)}
                >
                  With attachments
                </DropdownMenuCheckboxItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
        {banner}
        {isLoading ? (
          <ListSkeleton />
        ) : error ? (
          <div className="flex flex-col items-center gap-3 px-6 py-12 text-center">
            <p className="text-sm text-muted-foreground">Couldn't load messages: {error.message}</p>
            <Button variant="outline" size="sm" onClick={onRetry}>
              <RotateCw />
              Try again
            </Button>
          </div>
        ) : visible.length === 0 ? (
          filtering && items.length > 0 ? (
            <p className="px-6 py-12 text-center text-sm text-muted-foreground">No messages match this filter.</p>
          ) : (
            empty
          )
        ) : (
          <ul role="listbox" aria-label="Messages" className="divide-y">
            {visible.map((m) => (
              <MessageRow
                key={m.id}
                item={m}
                mode={mode}
                selected={m.id === selectedId}
                checked={checked.has(m.id)}
                selecting={checked.size > 0}
                onSelect={() => onSelect(m.id)}
                onToggle={onDelete ? () => toggle(m.id) : undefined}
              />
            ))}
          </ul>
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
  checked,
  selecting,
  onSelect,
  onToggle,
}: {
  item: ListItem
  mode: 'sent' | 'mailbox'
  selected: boolean
  checked: boolean
  selecting: boolean
  onSelect: () => void
  onToggle?: () => void
}) {
  const unread = isDelivery(item) && !item.seen
  const recipients = [...item.to, ...item.cc]
  const name = displayName(item.from)
  const secondLine =
    mode === 'sent'
      ? recipients.length === 0
        ? 'Undisclosed recipients'
        : `To ${recipients[0].address}${recipients.length > 1 ? ` +${recipients.length - 1}` : ''}`
      : item.from.address

  return (
    <li
      role="option"
      aria-selected={selected}
      data-message-id={item.id}
      className={cn(
        'group relative flex cursor-pointer gap-3 px-4 py-4 transition-colors hover:bg-muted/50',
        selected && 'bg-muted hover:bg-muted',
      )}
      onClick={onSelect}
    >
      <div className="relative mt-0.5 size-7 shrink-0">
        <SenderAvatar
          name={item.from.name}
          address={item.from.address}
          className={cn(onToggle && (checked || selecting ? 'invisible' : 'group-hover:invisible'))}
        />
        {onToggle && (
          <button
            type="button"
            role="checkbox"
            aria-checked={checked}
            aria-label={`Select message from ${name}`}
            onClick={(e) => {
              e.stopPropagation()
              onToggle()
            }}
            className={cn(
              'absolute inset-0 flex items-center justify-center rounded-full border bg-background text-primary-foreground',
              checked ? 'border-primary bg-primary' : 'invisible group-hover:visible',
              selecting && 'visible',
            )}
          >
            {checked && <Check className="size-3.5" strokeWidth={3} />}
          </button>
        )}
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className={cn('truncate text-[15px]', unread ? 'font-semibold' : 'font-medium')}>{name}</span>
          {item.attachmentCount > 0 && (
            <Paperclip className="size-3.5 shrink-0 text-muted-foreground" aria-label="Has attachments" />
          )}
          {isDelivery(item) && item.rcptType === 'bcc' && (
            <span className="shrink-0 rounded border px-1 text-[10px] font-medium tracking-wide text-muted-foreground uppercase">
              Bcc
            </span>
          )}
          <span
            className={cn(
              'ml-auto shrink-0 pl-2 text-[13px] tabular-nums',
              unread || selected ? 'text-foreground' : 'text-muted-foreground',
            )}
          >
            {listTime(messageTime(item))}
          </span>
        </div>
        <div className="truncate text-sm text-muted-foreground">{secondLine}</div>
        <p className="mt-2 line-clamp-2 text-sm leading-relaxed text-muted-foreground">
          <span className={cn('text-foreground/85', unread && 'font-medium text-foreground')}>
            {item.subject || '(no subject)'}
          </span>
          {item.preview && <> — {item.preview}</>}
        </p>
      </div>
      {unread && <span aria-label="Unread" className="absolute top-[22px] left-1.5 size-1.5 rounded-full bg-brand" />}
    </li>
  )
}

function ListSkeleton() {
  return (
    <div className="divide-y" aria-busy="true" aria-label="Loading messages">
      {Array.from({ length: 6 }, (_, i) => (
        <div key={i} className="flex gap-3 px-4 py-4">
          <Skeleton className="size-7 rounded-full" />
          <div className="flex-1 space-y-2">
            <div className="flex justify-between gap-4">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="h-3 w-10" />
            </div>
            <Skeleton className="h-3 w-44" />
            <Skeleton className="h-3 w-full" />
            <Skeleton className="h-3 w-3/4" />
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
