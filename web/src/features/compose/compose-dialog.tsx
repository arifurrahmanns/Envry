import { useState, type FormEvent, type ReactNode } from 'react'
import { Loader2, Send } from 'lucide-react'
import { toast } from 'sonner'

import type { ComposeDraft } from '@/components/layout/shell-context'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useSend } from '@/lib/queries'

const DEFAULT_FROM = 'envry@localhost'

const bareInput = 'border-0 px-0 shadow-none focus-visible:ring-0 dark:bg-transparent'

function splitAddresses(value: string): string[] {
  return value
    .split(/[,;\s]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

export function ComposeDialog({
  open,
  draft,
  onOpenChange,
}: {
  open: boolean
  draft: ComposeDraft
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-0 p-0 sm:max-w-xl">
        {/* Content unmounts when closed, so every open starts from the draft. */}
        <ComposeForm draft={draft} onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  )
}

function ComposeForm({ draft, onDone }: { draft: ComposeDraft; onDone: () => void }) {
  const send = useSend()
  const [from, setFrom] = useState(DEFAULT_FROM)
  const [to, setTo] = useState(draft.to ?? '')
  const [cc, setCc] = useState('')
  const [bcc, setBcc] = useState('')
  const [showCopies, setShowCopies] = useState(false)
  const [subject, setSubject] = useState(draft.subject ?? '')
  const [body, setBody] = useState('')

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const recipients = { to: splitAddresses(to), cc: splitAddresses(cc), bcc: splitAddresses(bcc) }
    send.mutate(
      { from: from.trim() || DEFAULT_FROM, ...recipients, subject, text: body },
      {
        onSuccess: () => {
          const n = recipients.to.length + recipients.cc.length + recipients.bcc.length
          toast.success('Message sent', { description: `Delivered to ${n} local mailbox${n === 1 ? '' : 'es'}.` })
          onDone()
        },
        onError: (err) => toast.error("Couldn't send", { description: err.message }),
      },
    )
  }

  return (
    <form onSubmit={submit} className="flex flex-col">
      <DialogHeader className="border-b px-6 py-4">
        <DialogTitle>New message</DialogTitle>
        <DialogDescription>Sent through Envry's own SMTP server and captured locally.</DialogDescription>
      </DialogHeader>

      <div className="divide-y border-b">
        <Field id="compose-from" label="From">
          <Input id="compose-from" type="email" value={from} onChange={(e) => setFrom(e.target.value)} className={bareInput} />
        </Field>
        <Field id="compose-to" label="To">
          <Input
            id="compose-to"
            required
            autoFocus
            placeholder="user@test.local, another@test.local"
            value={to}
            onChange={(e) => setTo(e.target.value)}
            className={bareInput}
          />
          {!showCopies && (
            <button
              type="button"
              className="shrink-0 text-sm text-muted-foreground hover:text-foreground"
              onClick={() => setShowCopies(true)}
            >
              Cc Bcc
            </button>
          )}
        </Field>
        {showCopies && (
          <>
            <Field id="compose-cc" label="Cc">
              <Input id="compose-cc" value={cc} onChange={(e) => setCc(e.target.value)} className={bareInput} />
            </Field>
            <Field id="compose-bcc" label="Bcc">
              <Input id="compose-bcc" value={bcc} onChange={(e) => setBcc(e.target.value)} className={bareInput} />
            </Field>
          </>
        )}
        <Field id="compose-subject" label="Subject">
          <Input id="compose-subject" value={subject} onChange={(e) => setSubject(e.target.value)} className={bareInput} />
        </Field>
      </div>

      <Label htmlFor="compose-body" className="sr-only">
        Message
      </Label>
      <Textarea
        id="compose-body"
        placeholder="Write your message…"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        className="min-h-56 resize-none rounded-none border-0 px-6 py-4 text-[15px] shadow-none focus-visible:ring-0 dark:bg-transparent"
      />

      <DialogFooter className="border-t px-6 py-4">
        <Button type="button" variant="outline" onClick={onDone}>
          Discard
        </Button>
        <Button type="submit" disabled={send.isPending}>
          {send.isPending ? <Loader2 className="animate-spin" /> : <Send />}
          Send
        </Button>
      </DialogFooter>
    </form>
  )
}

function Field({ id, label, children }: { id: string; label: string; children: ReactNode }) {
  return (
    <div className="flex items-center gap-3 px-6">
      <Label htmlFor={id} className="w-16 shrink-0 font-normal text-muted-foreground">
        {label}
      </Label>
      {children}
    </div>
  )
}
