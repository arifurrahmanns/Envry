import { useEffect, useRef, useState, type ReactNode } from 'react'
import {
  ArrowLeft,
  CircleCheck,
  Contrast,
  Download,
  File,
  FileArchive,
  FileImage,
  FileText,
  Loader2,
  Mail,
  MailOpen,
  Reply,
  Server,
  Trash2,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

import { useShell } from '@/components/layout/shell-context'
import {
  DeviceWidthToggle,
  HtmlPreview,
  RemoteContentToggle,
  type PreviewWidth,
} from '@/components/mail/html-preview'
import { ChecksSection } from '@/components/mail/message-checks'
import { Section } from '@/components/mail/section'
import { SenderAvatar } from '@/components/mail/sender-avatar'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { urls, type Address, type Attachment, type Delivery, type MessageDetail } from '@/lib/api'
import { dayLabel, displayName, fileSize, mediumTime, messageTime, rowTime } from '@/lib/format'
import { useMessage, useRawSource } from '@/lib/queries'
import { cn } from '@/lib/utils'

type View = 'preview' | 'text' | 'source' | 'headers'

type MessageReaderProps = {
  id: string
  /** Set in mailbox views: this copy's folder, flags and recipient type. */
  delivery?: Delivery
  /** Mailbox address being viewed. */
  viewingAs?: string
  onBack: () => void
  onDelete: () => void
  deleting: boolean
  onToggleSeen?: () => void
}

export function MessageReader(props: MessageReaderProps) {
  const { id, onBack } = props
  const { data, isLoading, error } = useMessage(id)

  if (isLoading) return <ReaderSkeleton onBack={onBack} />
  if (error || !data) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center">
        <p className="text-sm text-muted-foreground">
          {error ? `Couldn't load this message: ${error.message}` : 'Message not found.'}
        </p>
        <Button variant="outline" size="sm" onClick={onBack}>
          Back to list
        </Button>
      </div>
    )
  }
  return <ReaderContent key={data.id} {...props} message={data} />
}

function ReaderContent({
  message: m,
  delivery,
  viewingAs,
  onBack,
  onDelete,
  deleting,
  onToggleSeen,
}: MessageReaderProps & { message: MessageDetail }) {
  const { openCompose } = useShell()
  const reply = () =>
    openCompose({
      to: (m.replyTo[0] ?? m.from).address,
      subject: /^re:/i.test(m.subject) ? m.subject : `Re: ${m.subject}`,
    })
  useReaderKeys({ onDelete, onReply: reply, onToggleSeen, onBack })

  // A recipient never sees the Bcc list; only the Sent view shows it.
  const bcc = viewingAs ? [] : m.recipients.filter((r) => r.type === 'bcc').map((r) => ({ name: '', address: r.address }))

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <article className="px-5 pt-6 pb-10 md:px-8">
        <div className="flex items-center gap-1">
          <ActionButton icon={ArrowLeft} label="Back" onClick={onBack} className="-ml-2 md:hidden" />
          <div className="ml-auto flex items-center gap-1">
            <ActionButton icon={Reply} label="Reply" shortcut="R" onClick={reply} />
            {delivery && onToggleSeen && (
              <ActionButton
                icon={delivery.seen ? Mail : MailOpen}
                label={delivery.seen ? 'Mark as unread' : 'Mark as read'}
                shortcut="U"
                onClick={onToggleSeen}
              />
            )}
            <ActionButton icon={Download} label="Download .eml" href={urls.raw(m.id, true)} />
            <ActionButton
              icon={deleting ? Loader2 : Trash2}
              label="Delete"
              shortcut="#"
              onClick={onDelete}
              disabled={deleting}
              iconClassName={deleting ? 'animate-spin' : undefined}
            />
          </div>
        </div>

        <h2 className="mt-3 text-[26px] leading-tight font-medium tracking-tight text-balance break-words md:text-[28px]">
          {m.subject || '(no subject)'}
        </h2>
        {m.preview && (
          <p className="mt-3 line-clamp-2 max-w-3xl text-[14px] leading-relaxed text-muted-foreground">{m.preview}</p>
        )}

        <dl className="mt-6 grid grid-cols-[96px_minmax(0,1fr)] items-center gap-x-4 gap-y-3 text-[14px] sm:grid-cols-[112px_minmax(0,1fr)]">
          <Property label="Status">
            <StatusPill delivery={delivery} />
          </Property>
          <Property label="From">
            <PersonChip address={m.from} />
          </Property>
          <PeopleProperty label="To" list={m.to} />
          <PeopleProperty label="Cc" list={m.cc} />
          <PeopleProperty label="Bcc" list={bcc} />
          {viewingAs && (
            <Property label="Mailbox">
              <PersonChip
                address={{ name: '', address: viewingAs }}
                suffix={delivery?.rcptType === 'bcc' ? 'Bcc copy' : undefined}
              />
            </Property>
          )}
          <Property label="Date">{mediumTime(messageTime(m))}</Property>
          <Property label="Size">{fileSize(m.size)}</Property>
          <Property label="Tags">
            <div className="flex flex-wrap gap-1.5">
              {tagsFor(m, delivery).map((t) => (
                <span key={t} className="rounded-full bg-muted px-3 py-1 text-[13px] text-foreground/85">
                  {t}
                </span>
              ))}
            </div>
          </Property>
        </dl>

        <Divider />
        <MessageBody message={m} />

        {m.attachments.length > 0 && (
          <>
            <Divider />
            <Section title="Attachments" count={m.attachments.length}>
              <Attachments messageId={m.id} items={m.attachments} />
            </Section>
          </>
        )}

        <Divider />
        <ChecksSection messageId={m.id} />

        <Divider />
        <h3 className="text-[15px] font-medium">Activity</h3>
        <Activity message={m} viewingAs={viewingAs} />
      </article>
    </div>
  )
}

function Divider() {
  return <div aria-hidden className="my-6 h-px bg-border" />
}

function Property({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </>
  )
}

function PeopleProperty({ label, list }: { label: string; list: Address[] }) {
  if (list.length === 0) return null
  return (
    <Property label={label}>
      <div className="flex flex-wrap gap-1.5">
        {list.map((a) => (
          <PersonChip key={a.address} address={a} />
        ))}
      </div>
    </Property>
  )
}

function PersonChip({ address: a, suffix }: { address: Address; suffix?: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex max-w-full items-center gap-1.5 rounded-full border py-0.5 pr-2.5 pl-0.5 text-[14px]">
          <SenderAvatar name={a.name} address={a.address} size="xs" />
          <span className="truncate">{displayName(a)}</span>
          {suffix && <span className="shrink-0 text-muted-foreground">({suffix})</span>}
        </span>
      </TooltipTrigger>
      <TooltipContent>{a.address}</TooltipContent>
    </Tooltip>
  )
}

function StatusPill({ delivery }: { delivery?: Delivery }) {
  const [label, Icon, tone] = !delivery
    ? (['Captured', CircleCheck, 'bg-success/15 text-success'] as const)
    : delivery.seen
      ? (['Read', CircleCheck, 'bg-success/15 text-success'] as const)
      : (['Unread', Contrast, 'bg-warning/15 text-warning'] as const)
  return (
    <span className={cn('inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-[14px] font-medium', tone)}>
      <Icon className="size-4" strokeWidth={2} />
      {label}
    </span>
  )
}

const sourceLabel: Record<string, string> = { smtp: 'SMTP', api: 'API', imap_append: 'IMAP' }

function tagsFor(m: MessageDetail, delivery?: Delivery): string[] {
  const tags = [sourceLabel[m.source] ?? m.source]
  if (m.hasHtml) tags.push('HTML')
  if (m.hasText) tags.push('Plain text')
  if (m.attachments.length > 0) tags.push(`${m.attachments.length} attachment${m.attachments.length === 1 ? '' : 's'}`)
  if (delivery?.rcptType === 'bcc') tags.push('Bcc')
  if (delivery?.flagged) tags.push('Flagged')
  if (delivery && delivery.folder !== 'INBOX') tags.push(delivery.folder)
  return tags
}

function MessageBody({ message: m }: { message: MessageDetail }) {
  const [view, setView] = useState<View>(m.hasHtml ? 'preview' : 'text')
  const [width, setWidth] = useState<PreviewWidth>('desktop')
  const [remoteContent, setRemoteContent] = useState(false)
  const views: { id: View; label: string; hidden?: boolean }[] = [
    { id: 'preview', label: 'Preview', hidden: !m.hasHtml },
    { id: 'text', label: 'Text' },
    { id: 'source', label: 'Source' },
    { id: 'headers', label: 'Headers' },
  ]

  return (
    <Section
      title="Message"
      aside={
        view === 'preview' && (
          <div className="flex items-center gap-2">
            <RemoteContentToggle value={remoteContent} onChange={setRemoteContent} />
            <div className="hidden sm:block">
              <DeviceWidthToggle value={width} onChange={setWidth} />
            </div>
          </div>
        )
      }
    >
      <div role="tablist" aria-label="Message view" className="mb-4 flex flex-wrap gap-1">
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
                'h-8 rounded-[10px] px-3 text-[14px] text-foreground/75 transition-colors hover:text-foreground',
                view === v.id && 'bg-muted font-medium text-foreground',
              )}
            >
              {v.label}
            </button>
          ))}
      </div>
      <div role="tabpanel" className="flex min-h-[360px] flex-col">
        {view === 'preview' && <HtmlPreview messageId={m.id} width={width} remoteContent={remoteContent} />}
        {view === 'text' &&
          (m.text ? (
            <pre className="font-sans text-[15px] leading-relaxed break-words whitespace-pre-wrap text-foreground/85">
              {m.text}
            </pre>
          ) : (
            <p className="text-sm text-muted-foreground">This message has no plain-text part.</p>
          ))}
        {view === 'source' && <SourceView id={m.id} />}
        {view === 'headers' && <HeadersView message={m} />}
      </div>
    </Section>
  )
}

function SourceView({ id }: { id: string }) {
  const { data, isLoading, error } = useRawSource(id, true)
  if (isLoading) return <Skeleton className="h-64 w-full" />
  if (error) return <p className="text-sm text-muted-foreground">Couldn't load the source: {error.message}</p>
  return (
    <pre className="overflow-x-auto rounded-xl border bg-card p-4 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap">
      {data}
    </pre>
  )
}

function HeadersView({ message: m }: { message: MessageDetail }) {
  return (
    <div className="overflow-hidden rounded-xl border">
      <table className="w-full text-left font-mono text-xs">
        <tbody className="divide-y">
          {m.headers.map((h, i) => (
            <tr key={`${h.name}-${i}`} className="align-top">
              <th scope="row" className="w-48 bg-card px-3 py-2 font-medium whitespace-nowrap text-muted-foreground">
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
  return (
    <ul className="space-y-1">
      {items.map((a) => {
        const Icon = attachmentIcon(a.contentType)
        return (
          <li key={a.id}>
            <a
              href={urls.attachment(messageId, a.id)}
              download={a.filename || undefined}
              className="group -mx-2 flex items-center gap-3 rounded-lg px-2 py-1.5 text-[14px] transition-colors hover:bg-accent"
            >
              <Icon className="size-[18px] shrink-0 text-muted-foreground" strokeWidth={1.75} />
              <span className="min-w-0 flex-1 truncate text-foreground/90">{a.filename || 'Untitled'}</span>
              <span className="shrink-0 text-[13px] text-muted-foreground tabular-nums">{fileSize(a.size)}</span>
              <Download className="size-4 shrink-0 text-muted-foreground opacity-0 group-hover:opacity-100" />
            </a>
          </li>
        )
      })}
    </ul>
  )
}

function ago(iso: string): string {
  const day = dayLabel(iso)
  return day === 'Today' ? rowTime(iso) : day
}

const rcptLabel: Record<string, string> = { to: 'To', cc: 'Cc', bcc: 'Bcc' }

function Activity({ message: m, viewingAs }: { message: MessageDetail; viewingAs?: string }) {
  const when = ago(m.receivedAt)
  const via = sourceLabel[m.source] ?? m.source
  // A recipient only learns about other recipients' Bcc copies in Sent.
  const recipients = viewingAs ? m.recipients.filter((r) => r.type !== 'bcc' || r.address === viewingAs) : m.recipients

  return (
    <ul className="mt-4 space-y-3 text-[14px]">
      <ActivityItem avatar={<SenderAvatar name={m.from.name} address={m.from.address} size="xs" />} when={when}>
        <strong className="font-medium">{displayName(m.from)}</strong>
        <span className="text-muted-foreground"> sent this message</span>
      </ActivityItem>
      <ActivityItem avatar={<SystemAvatar icon={Server} />} when={when}>
        <strong className="font-medium">Envry</strong>
        <span className="text-muted-foreground">
          {' '}
          captured it over {via}
          {m.clientIp && ` from ${m.clientIp}`}
          {m.authUser && ` as ${m.authUser}`}
        </span>
      </ActivityItem>
      {recipients.map((r) => (
        <ActivityItem key={r.address + r.type} avatar={<SenderAvatar name="" address={r.address} size="xs" />} when={when}>
          <span className="text-muted-foreground">Delivered to </span>
          <strong className="font-medium">{r.address}</strong>
          <span className="text-muted-foreground"> as {rcptLabel[r.type] ?? r.type}</span>
        </ActivityItem>
      ))}
    </ul>
  )
}

function ActivityItem({ avatar, when, children }: { avatar: ReactNode; when: string; children: ReactNode }) {
  return (
    <li className="flex items-center gap-2.5">
      {avatar}
      <span className="min-w-0 truncate">
        {children}
        <span className="text-muted-foreground"> • {when}</span>
      </span>
    </li>
  )
}

function SystemAvatar({ icon: Icon }: { icon: LucideIcon }) {
  return (
    <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
      <Icon className="size-3" strokeWidth={2} />
    </span>
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
  const classes = cn('size-9 rounded-[10px] text-muted-foreground hover:text-foreground', className)
  const icon = <Icon className={cn('size-[18px]', iconClassName)} strokeWidth={1.75} />
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        {href ? (
          <Button asChild variant="ghost" size="icon" className={classes}>
            <a href={href} aria-label={label} download>
              {icon}
            </a>
          </Button>
        ) : (
          <Button variant="ghost" size="icon" className={classes} onClick={onClick} disabled={disabled} aria-label={label}>
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
    <div className="space-y-6 px-5 pt-6 md:px-8" aria-busy="true" aria-label="Loading message">
      <div className="flex justify-between">
        <Button variant="ghost" size="icon" className="-ml-2 md:hidden" onClick={onBack} aria-label="Back">
          <ArrowLeft />
        </Button>
        <div className="ml-auto flex gap-1">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="size-9 rounded-[10px]" />
          ))}
        </div>
      </div>
      <Skeleton className="h-8 w-3/4" />
      <Skeleton className="h-4 w-1/2" />
      <div className="space-y-3">
        {[0, 1, 2, 3, 4].map((i) => (
          <div key={i} className="flex gap-4">
            <Skeleton className="h-5 w-20" />
            <Skeleton className="h-5 w-48" />
          </div>
        ))}
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
