import { useRef, useState, type FormEvent, type KeyboardEvent, type ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router'
import { Loader2, Send, Trash2, X } from 'lucide-react'
import { toast } from 'sonner'

import { PageHeader } from '@/components/layout/page-header'
import type { ComposeDraft } from '@/components/layout/shell-context'
import { SenderAvatar } from '@/components/mail/sender-avatar'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useSend } from '@/lib/queries'
import { cn } from '@/lib/utils'

const DEFAULT_FROM = 'envry@localhost'

const bareInput = 'h-auto border-0 bg-transparent px-0 py-0 shadow-none focus-visible:ring-0 dark:bg-transparent'

const isMac = /Mac/.test(navigator.userAgent)

function splitAddresses(value: string): string[] {
  return value
    .split(/[,;\s]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

function looksLikeAddress(a: string) {
  return /^[^@\s]+@[^@\s]+$/.test(a)
}

/**
 * New message, as a full-height composer like a mail client's. A reply
 * arrives with a draft (to, subject) in the navigation state; each visit
 * starts from that draft.
 */
export function ComposePage() {
  const location = useLocation()
  const navigate = useNavigate()
  const draft = (location.state ?? {}) as ComposeDraft
  // Back to wherever Compose was opened from; Sent if it was opened directly.
  const leave = () => (location.key === 'default' ? navigate('/sent') : navigate(-1))
  return (
    <>
      <PageHeader title="New message" subtitle="Delivered by Envry's own SMTP server" compose={false} />
      <Composer
        key={location.key}
        draft={draft}
        onDiscard={leave}
        onSent={(id) => navigate(`/sent?m=${encodeURIComponent(id)}`)}
      />
    </>
  )
}

function Composer({
  draft,
  onDiscard,
  onSent,
}: {
  draft: ComposeDraft
  onDiscard: () => void
  onSent: (id: string) => void
}) {
  const send = useSend()
  const formRef = useRef<HTMLFormElement>(null)
  const [from, setFrom] = useState(DEFAULT_FROM)
  const [to, setTo] = useState(() => splitAddresses(draft.to ?? ''))
  const [cc, setCc] = useState<string[]>([])
  const [bcc, setBcc] = useState<string[]>([])
  const [showCc, setShowCc] = useState(false)
  const [showBcc, setShowBcc] = useState(false)
  const [subject, setSubject] = useState(draft.subject ?? '')
  const [body, setBody] = useState('')

  const submit = (e?: FormEvent) => {
    e?.preventDefault()
    // Addresses still being typed count too.
    const pending = (id: string) => splitAddresses((document.getElementById(id) as HTMLInputElement | null)?.value ?? '')
    const recipients = {
      to: [...to, ...pending('compose-to')],
      cc: [...cc, ...pending('compose-cc')],
      bcc: [...bcc, ...pending('compose-bcc')],
    }
    const all = [...recipients.to, ...recipients.cc, ...recipients.bcc]
    const n = all.length
    if (n === 0) {
      toast.error('Add a recipient', { description: 'Type an address in To and press Enter.' })
      document.getElementById('compose-to')?.focus()
      return
    }
    const bad = all.filter((a) => !looksLikeAddress(a))
    if (bad.length > 0) {
      toast.error('Fix the addresses in red', { description: bad.join(', ') })
      return
    }
    send.mutate(
      { from: from.trim() || DEFAULT_FROM, ...recipients, subject, text: body },
      {
        onSuccess: ({ id }) => {
          toast.success('Message sent', { description: `Delivered to ${n} local mailbox${n === 1 ? '' : 'es'}.` })
          onSent(id)
        },
        onError: (err) => toast.error("Couldn't send", { description: err.message }),
      },
    )
  }

  // Ctrl/Cmd+Enter sends from anywhere in the composer.
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      formRef.current?.requestSubmit()
    }
  }

  return (
    <form ref={formRef} onSubmit={submit} onKeyDown={onKeyDown} className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 divide-y border-b">
        <Row id="compose-from" label="From">
          <Input
            id="compose-from"
            type="email"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
            className={cn(bareInput, 'text-sm')}
          />
        </Row>
        <Row id="compose-to" label="To">
          <RecipientInput id="compose-to" value={to} onChange={setTo} autoFocus={to.length === 0} placeholder="Add recipients" />
          <div className="flex shrink-0 gap-3 text-[13px] text-muted-foreground">
            {!showCc && (
              <button type="button" className="hover:text-foreground" onClick={() => setShowCc(true)}>
                Cc
              </button>
            )}
            {!showBcc && (
              <button type="button" className="hover:text-foreground" onClick={() => setShowBcc(true)}>
                Bcc
              </button>
            )}
          </div>
        </Row>
        {showCc && (
          <Row id="compose-cc" label="Cc">
            <RecipientInput id="compose-cc" value={cc} onChange={setCc} autoFocus />
          </Row>
        )}
        {showBcc && (
          <Row id="compose-bcc" label="Bcc">
            <RecipientInput id="compose-bcc" value={bcc} onChange={setBcc} autoFocus={!showCc} />
          </Row>
        )}
        <Row id="compose-subject" label="Subject">
          <Input
            id="compose-subject"
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
            autoFocus={to.length > 0}
            className={cn(bareInput, 'text-[15px] font-medium md:text-[15px]')}
          />
        </Row>
      </div>

      <Label htmlFor="compose-body" className="sr-only">
        Message
      </Label>
      <Textarea
        id="compose-body"
        placeholder="Write your message…"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        className="min-h-0 flex-1 resize-none rounded-none border-0 px-5 py-5 text-[15px] leading-relaxed shadow-none focus-visible:ring-0 md:px-8 md:text-[15px] dark:bg-transparent"
      />

      <div className="flex shrink-0 items-center gap-3 border-t px-5 py-3 md:px-8">
        <Button type="submit" disabled={send.isPending} className="h-9 gap-2 rounded-lg px-4">
          {send.isPending ? <Loader2 className="animate-spin" /> : <Send />}
          Send
        </Button>
        <span className="hidden text-[12px] text-muted-foreground sm:inline">
          <kbd className="rounded border bg-muted px-1.5 font-mono text-[11px]">{isMac ? '⌘' : 'Ctrl'}</kbd>{' '}
          <kbd className="rounded border bg-muted px-1.5 font-mono text-[11px]">Enter</kbd> to send · plain text
        </span>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label="Discard"
              onClick={onDiscard}
              className="ml-auto text-muted-foreground hover:text-foreground"
            >
              <Trash2 className="size-[18px]" strokeWidth={1.75} />
            </Button>
          </TooltipTrigger>
          <TooltipContent>Discard</TooltipContent>
        </Tooltip>
      </div>
    </form>
  )
}

/** One header line of the composer: a muted label and its input, edge to edge. */
function Row({ id, label, children }: { id: string; label: string; children: ReactNode }) {
  return (
    <div className="flex min-h-12 items-center gap-3 px-5 md:px-8">
      <Label htmlFor={id} className="w-16 shrink-0 text-[13px] font-normal text-muted-foreground">
        {label}
      </Label>
      {children}
    </div>
  )
}

/**
 * Addresses as removable chips. Enter, comma, semicolon or space turns the
 * typed text into a chip; Backspace on an empty input removes the last one.
 */
function RecipientInput({
  id,
  value,
  onChange,
  autoFocus,
  placeholder,
}: {
  id: string
  value: string[]
  onChange: (next: string[]) => void
  autoFocus?: boolean
  placeholder?: string
}) {
  const [text, setText] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  const commit = (raw: string) => {
    const add = splitAddresses(raw).filter((a) => !value.includes(a))
    if (add.length > 0) onChange([...value, ...add])
    setText('')
  }

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' && !(e.ctrlKey || e.metaKey) && text.trim()) {
      e.preventDefault()
      commit(text)
    } else if (e.key === 'Backspace' && text === '' && value.length > 0) {
      onChange(value.slice(0, -1))
    }
  }

  return (
    <div className="flex min-w-0 flex-1 cursor-text flex-wrap items-center gap-1.5 py-2" onClick={() => inputRef.current?.focus()}>
      {value.map((a) => (
        <span
          key={a}
          className={cn(
            'inline-flex max-w-full items-center gap-1.5 rounded-full border py-0.5 pr-1 pl-0.5 text-[13px]',
            !looksLikeAddress(a) && 'border-destructive/50 text-destructive',
          )}
        >
          <SenderAvatar address={a} size="xs" />
          <span className="truncate">{a}</span>
          <button
            type="button"
            aria-label={`Remove ${a}`}
            onClick={(e) => {
              e.stopPropagation()
              onChange(value.filter((x) => x !== a))
            }}
            className="rounded-full p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            <X className="size-3" />
          </button>
        </span>
      ))}
      <Input
        ref={inputRef}
        id={id}
        value={text}
        autoFocus={autoFocus}
        placeholder={value.length === 0 ? placeholder : undefined}
        onChange={(e) => {
          const v = e.target.value
          if (/[,;\s]$/.test(v)) commit(v)
          else setText(v)
        }}
        onKeyDown={onKeyDown}
        onBlur={() => text.trim() && commit(text)}
        className={cn(bareInput, 'h-6 min-w-40 flex-1 text-sm')}
      />
    </div>
  )
}
