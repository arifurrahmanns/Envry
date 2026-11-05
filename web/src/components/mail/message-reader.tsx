import { useEffect, useRef, useState, type ReactNode } from 'react'
import {
  ArrowLeft,
  ChevronRight,
  Download,
  EyeOff,
  File,
  FileArchive,
  FileImage,
  FileText,
  Loader2,
  Mail,
  MailOpen,
  Reply,
  Trash2,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

import { useShell } from '@/components/layout/shell-context'
import { DeviceWidthToggle, HtmlPreview, type PreviewWidth } from '@/components/mail/html-preview'
import { SenderAvatar } from '@/components/mail/sender-avatar'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { urls, type Address, type Attachment, type Delivery, type MessageDetail } from '@/lib/api'
import { displayName, fileSize, formatAddress, fullTime, messageTime } from '@/lib/format'
import { useMessage, useRawSource } from '@/lib/queries'
import { cn } from '@/lib/utils'

type View = 'preview' | 'text' | 'source' | 'headers'

type MessageReaderProps = {
  id: string
  /** Set in mailbox views: this copy's folder, flags and recipient type. */
  delivery?: Delivery
  /** Mailbox address being viewed, for the "Viewing as" notice. */
  viewingAs?: string
  showEnvelope?: boolean
  onBack: () => void
  onDelete: () => void
  deleting: boolean
  onToggleSeen?: () => void
}

export function MessageReader(props: MessageReaderProps) {
  const { id, onBack } = props
  const { data, isLoading, error } = useMessage(id)

  return (
    <section aria-label="Message" className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-xl border bg-card">
      {isLoading ? (
        <ReaderSkeleton onBack={onBack} />
      ) : error || !data ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center">
          <p className="text-sm text-muted-foreground">
            {error ? `Couldn't load this message: ${error.message}` : 'Message not found.'}
          </p>
          <Button variant="outline" size="sm" onClick={onBack}>
            Back to list
          </Button>
        </div>
      ) : (
        <ReaderContent key={data.id} {...props} message={data} />
      )}
    </section>
  )
}

function ReaderContent({
  message: m,
  delivery,
  viewingAs,
  showEnvelope,
  onBack,
  onDelete,
  deleting,
  onToggleSeen,
}: MessageReaderProps & { message: MessageDetail }) {
  const { openCompose } = useShell()
  const [view, setView] = useState<View>(m.hasHtml ? 'preview' : 'text')
  const [width, setWidth] = useState<PreviewWidth>('desktop')

  const reply = () =>
    openCompose({
      to: (m.replyTo[0] ?? m.from).address,
      subject: /^re:/i.test(m.subject) ? m.subject : `Re: ${m.subject}`,
    })

  useReaderKeys({ onDelete, onReply: reply, onToggleSeen, onBack })

  // A recipient never sees the Bcc list; only the Sent view shows it.
  const bcc = viewingAs ? [] : m.recipients.filter((r) => r.type === 'bcc')
  const views: { id: View; label: string; hidden?: boolean }[] = [
    { id: 'preview', label: 'Preview', hidden: !m.hasHtml },
    { id: 'text', label: 'Text' },
    { id: 'source', label: 'Source' },
    { id: 'headers', label: 'Headers' },
  ]

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="flex min-h-full flex-col px-5 pt-5 pb-6 md:px-6">
        <div className="flex items-center gap-2">
          <ActionButton icon={ArrowLeft} label="Back" onClick={onBack} className="md:hidden" />
          <ActionButton icon={Download} label="Download .eml" href={urls.raw(m.id, true)} />
          <ActionButton
            icon={deleting ? Loader2 : Trash2}
            label="Delete"
            shortcut="#"
            onClick={onDelete}
            disabled={deleting}
            iconClassName={deleting ? 'animate-spin' : undefined}
          />
          <ActionButton icon={Reply} label="Reply" shortcut="R" onClick={reply} />
          {delivery && onToggleSeen && (
            <ActionButton
              icon={delivery.seen ? Mail : MailOpen}
              label={delivery.seen ? 'Mark as unread' : 'Mark as read'}
              shortcut="U"
              onClick={onToggleSeen}
            />
          )}
          <time dateTime={messageTime(m)} className="ml-auto hidden text-sm text-muted-foreground sm:block">
            {fullTime(messageTime(m))}
          </time>
        </div>

        {viewingAs && (
          <div className="mt-5 flex items-center gap-2 rounded-lg border bg-muted/50 px-3 py-2 text-sm text-muted-foreground">
            {delivery?.rcptType === 'bcc' ? <EyeOff className="size-4 shrink-0" /> : <Mail className="size-4 shrink-0" />}
            <span className="min-w-0 truncate">
              Viewing as <span className="font-medium text-foreground">{viewingAs}</span>
              {delivery?.rcptType === 'bcc' && ' — received as a blind copy (Bcc), hidden from other recipients'}
            </span>
          </div>
        )}

        <div className="mt-6 flex items-center gap-3">
          <SenderAvatar name={m.from.name} address={m.from.address} size="lg" />
          <div className="min-w-0">
            <p className="truncate text-lg font-medium">{displayName(m.from)}</p>
            <p className="truncate text-sm text-muted-foreground">
              From: <span className="text-foreground/80">{m.from.address}</span>
            </p>
          </div>
        </div>

        <h2 className="mt-6 text-2xl font-semibold tracking-tight text-balance break-words">
          {m.subject || '(no subject)'}
        </h2>

        <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
          <AddressRow label="To" list={m.to} />
          <AddressRow label="Cc" list={m.cc} />
          <AddressRow label="Bcc" list={bcc.map((r) => ({ name: '', address: r.address }))} />
          <AddressRow label="Reply-To" list={m.replyTo} />
        </dl>

        {showEnvelope && <Envelope message={m} />}

        <div className="mt-6 flex items-center gap-3 border-b">
          <div role="tablist" aria-label="Message view" className="-mb-px flex gap-4">
            {views
              .filter((v) => !v.hidden)
              .map((v) => (
                <button
                  key={v.id}
                  type="button"
                  role="tab"
                  aria-selected={view === v.id}
                  onClick={() => setView(v.id)}
                  className={cn(
                    'border-b-2 border-transparent pb-2.5 text-sm text-muted-foreground transition-colors hover:text-foreground',
                    view === v.id && 'border-foreground font-medium text-foreground',
                  )}
                >
                  {v.label}
                </button>
              ))}
          </div>
          {view === 'preview' && (
            <div className="mb-2 ml-auto hidden sm:block">
              <DeviceWidthToggle value={width} onChange={setWidth} />
            </div>
          )}
        </div>

        <div role="tabpanel" className="mt-5 flex min-h-[320px] flex-1 flex-col">
          {view === 'preview' && <HtmlPreview messageId={m.id} width={width} />}
          {view === 'text' &&
            (m.text ? (
              <pre className="font-sans text-[15px] leading-relaxed break-words whitespace-pre-wrap text-foreground/80">
                {m.text}
              </pre>
            ) : (
              <p className="text-sm text-muted-foreground">This message has no plain-text part.</p>
            ))}
          {view === 'source' && <SourceView id={m.id} />}
          {view === 'headers' && <HeadersView message={m} />}
        </div>

        {m.attachments.length > 0 && <Attachments messageId={m.id} items={m.attachments} />}
      </div>
    </div>
  )
}

function AddressRow({ label, list }: { label: string; list: Address[] }) {
  if (list.length === 0) return null
  return (
    <>
      <dt className="text-muted-foreground">{label}:</dt>
      <dd className="min-w-0 break-words text-foreground/80">{list.map(formatAddress).join(', ')}</dd>
    </>
  )
}

function Envelope({ message: m }: { message: MessageDetail }) {
  const rows: [string, ReactNode][] = [
    ['MAIL FROM', m.envelopeFrom || '<>'],
    [
      'RCPT TO',
      <ul key="rcpt" className="space-y-1">
        {m.recipients.map((r) => (
          <li key={r.address + r.type} className="flex items-center gap-2">
            <span className="w-8 shrink-0 rounded border px-1 text-center text-[10px] font-medium text-muted-foreground uppercase">
              {r.type}
            </span>
            <span className="min-w-0 break-all">{r.address}</span>
          </li>
        ))}
      </ul>,
    ],
    ['Message-ID', m.messageId ?? '—'],
    ['Client IP', m.clientIp ?? '—'],
    ['Auth user', m.authUser ?? '—'],
    ['Size', fileSize(m.size)],
    ['Received', fullTime(m.receivedAt)],
  ]

  return (
    <details className="group mt-4 rounded-lg border">
      <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-sm text-muted-foreground select-none hover:text-foreground [&::-webkit-details-marker]:hidden">
        <ChevronRight className="size-4 transition-transform group-open:rotate-90" />
        SMTP envelope
        <span className="ml-auto text-xs tabular-nums">
          {m.recipients.length} recipient{m.recipients.length === 1 ? '' : 's'}
        </span>
      </summary>
      <dl className="grid grid-cols-[7rem_1fr] gap-x-3 gap-y-2 border-t px-3 py-3 font-mono text-xs">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-muted-foreground">{k}</dt>
            <dd className="min-w-0 break-all">{v}</dd>
          </div>
        ))}
      </dl>
    </details>
  )
}

function SourceView({ id }: { id: string }) {
  const { data, isLoading, error } = useRawSource(id, true)
  if (isLoading) return <Skeleton className="h-64 w-full" />
  if (error) return <p className="text-sm text-muted-foreground">Couldn't load the source: {error.message}</p>
  return (
    <pre className="overflow-x-auto rounded-lg border bg-muted/40 p-4 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all">
      {data}
    </pre>
  )
}

function HeadersView({ message: m }: { message: MessageDetail }) {
  return (
    <div className="overflow-hidden rounded-lg border">
      <table className="w-full text-left font-mono text-xs">
        <tbody className="divide-y">
          {m.headers.map((h, i) => (
            <tr key={`${h.name}-${i}`} className="align-top">
              <th scope="row" className="w-48 bg-muted/40 px-3 py-2 font-medium whitespace-nowrap text-muted-foreground">
                {h.name}
              </th>
              <td className="px-3 py-2 break-all">{h.value}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function attachmentIcon(type: string): LucideIcon {
  if (type.startsWith('image/')) return FileImage
  if (type.startsWith('text/') || type === 'application/pdf') return FileText
  if (/zip|compressed|tar|gzip/.test(type)) return FileArchive
  return File
}

function Attachments({ messageId, items }: { messageId: string; items: Attachment[] }) {
  const total = items.reduce((n, a) => n + a.size, 0)
  return (
    <div className="mt-8">
      <div className="flex items-baseline gap-2">
        <h3 className="text-[15px] font-medium">Attachments</h3>
        <span className="text-sm text-muted-foreground tabular-nums">
          {items.length} file{items.length === 1 ? '' : 's'} · {fileSize(total)}
        </span>
      </div>
      <ul className="mt-3 grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
        {items.map((a) => {
          const Icon = attachmentIcon(a.contentType)
          return (
            <li key={a.id}>
              <a
                href={urls.attachment(messageId, a.id)}
                download={a.filename || undefined}
                className="flex items-center gap-3 rounded-xl border bg-background p-3 shadow-xs transition-colors hover:bg-muted/50"
              >
                <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                  <Icon className="size-[18px]" strokeWidth={1.75} />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium">{a.filename || 'Untitled'}</span>
                  <span className="block truncate text-xs text-muted-foreground">
                    {fileSize(a.size)} · {a.contentType}
                    {a.inline && ' · inline'}
                  </span>
                </span>
                <Download className="size-4 shrink-0 text-muted-foreground" />
              </a>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

function ActionButton({
  icon: Icon,
  label,
  shortcut,
  onClick,
  href,
  disabled,
  className,
  iconClassName,
}: {
  icon: LucideIcon
  label: string
  shortcut?: string
  onClick?: () => void
  href?: string
  disabled?: boolean
  className?: string
  iconClassName?: string
}) {
  const classes = cn('size-9 rounded-lg text-muted-foreground shadow-xs hover:text-foreground', className)
  const icon = <Icon className={cn('size-[18px]', iconClassName)} strokeWidth={1.75} />
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        {href ? (
          <Button asChild variant="outline" size="icon" className={classes}>
            <a href={href} aria-label={label} download>
              {icon}
            </a>
          </Button>
        ) : (
          <Button variant="outline" size="icon" className={classes} onClick={onClick} disabled={disabled} aria-label={label}>
            {icon}
          </Button>
        )}
      </TooltipTrigger>
      <TooltipContent>
        {label}
        {shortcut && <kbd className="ml-2 font-mono text-[10px] opacity-70">{shortcut}</kbd>}
      </TooltipContent>
    </Tooltip>
  )
}

function ReaderSkeleton({ onBack }: { onBack: () => void }) {
  return (
    <div className="space-y-6 px-6 pt-5" aria-busy="true" aria-label="Loading message">
      <div className="flex gap-2">
        <Button variant="outline" size="icon" className="size-9 rounded-lg md:hidden" onClick={onBack} aria-label="Back">
          <ArrowLeft />
        </Button>
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="size-9 rounded-lg" />
        ))}
      </div>
      <div className="flex items-center gap-3">
        <Skeleton className="size-10 rounded-full" />
        <div className="space-y-2">
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-4 w-56" />
        </div>
      </div>
      <Skeleton className="h-7 w-2/3" />
      <div className="space-y-2">
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-11/12" />
        <Skeleton className="h-4 w-3/4" />
      </div>
    </div>
  )
}

/** Reader shortcuts: # / Delete delete, r reply, u toggle unread, Esc back. */
function useReaderKeys(handlers: {
  onDelete: () => void
  onReply: () => void
  onToggleSeen?: () => void
  onBack: () => void
}) {
  const ref = useRef(handlers)
  useEffect(() => {
    ref.current = handlers
  })
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return
      const target = e.target as HTMLElement | null
      if (target?.closest('input, textarea, select, [contenteditable="true"], [role="dialog"], [role="menu"]')) return
      const h = ref.current
      switch (e.key) {
        case '#':
        case 'Delete':
          e.preventDefault()
          h.onDelete()
          break
        case 'r':
          e.preventDefault()
          h.onReply()
          break
        case 'u':
          if (h.onToggleSeen) {
            e.preventDefault()
            h.onToggleSeen()
          }
          break
        case 'Escape':
          h.onBack()
          break
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
}
