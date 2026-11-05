import { useState } from 'react'
import { Check, Copy } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useInfo } from '@/lib/queries'
import { cn } from '@/lib/utils'

export function ConnectDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { data } = useInfo()
  const host = window.location.hostname || 'localhost'
  const ports = data?.ports ?? { smtp: 1025, pop3: 1110, imap: 1143, http: 8025 }
  const [lang, setLang] = useState<keyof typeof snippets>('node')

  const servers = [
    { label: 'SMTP', port: ports.smtp, use: 'Send mail from your app' },
    { label: 'IMAP', port: ports.imap, use: 'Read mail in a client' },
    { label: 'POP3', port: ports.pop3, use: 'Download-and-delete clients' },
  ]

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Connection settings</DialogTitle>
          <DialogDescription>
            SMTP accepts any username and password. For POP3 and IMAP, log in with a mailbox address
            such as <span className="font-mono text-foreground/80">dev@test.local</span>; any password
            works unless strict auth is on. No TLS is needed for local testing.
          </DialogDescription>
        </DialogHeader>

        <div className="overflow-hidden rounded-xl border">
          {servers.map((s, i) => (
            <div key={s.label} className={cn('flex items-center gap-3 px-4 py-3', i > 0 && 'border-t')}>
              <span className="w-12 shrink-0 rounded-md border bg-muted px-1.5 py-0.5 text-center font-mono text-xs font-medium">
                {s.label}
              </span>
              <code className="font-mono text-sm tabular-nums">
                {host}:{s.port}
              </code>
              <span className="hidden truncate text-sm text-muted-foreground sm:block">{s.use}</span>
              <CopyButton value={`${host}:${s.port}`} className="ml-auto" />
            </div>
          ))}
        </div>

        <div className="space-y-3">
          <div role="tablist" aria-label="Framework" className="flex flex-wrap gap-1">
            {(Object.keys(snippets) as (keyof typeof snippets)[]).map((k) => (
              <button
                key={k}
                type="button"
                role="tab"
                aria-selected={lang === k}
                onClick={() => setLang(k)}
                className={cn(
                  'h-8 rounded-lg px-3 text-sm text-muted-foreground hover:text-foreground',
                  lang === k && 'bg-muted font-medium text-foreground',
                )}
              >
                {snippets[k].label}
              </button>
            ))}
          </div>
          <div className="relative rounded-xl border bg-muted/40">
            <pre className="overflow-x-auto p-4 pr-12 font-mono text-[13px] leading-relaxed">
              {snippets[lang].code(host, ports.smtp)}
            </pre>
            <CopyButton value={snippets[lang].code(host, ports.smtp)} className="absolute top-2 right-2" />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

const snippets = {
  node: {
    label: 'Node.js',
    code: (host: string, port: number) => `import nodemailer from 'nodemailer'

const transport = nodemailer.createTransport({
  host: '${host}',
  port: ${port},
  secure: false,
})

await transport.sendMail({
  from: 'app@example.test',
  to: 'user@example.test',
  subject: 'Hello from Node',
  text: 'It works!',
})`,
  },
  laravel: {
    label: 'Laravel',
    code: (host: string, port: number) => `MAIL_MAILER=smtp
MAIL_HOST=${host}
MAIL_PORT=${port}
MAIL_USERNAME=null
MAIL_PASSWORD=null
MAIL_ENCRYPTION=null`,
  },
  python: {
    label: 'Python',
    code: (host: string, port: number) => `import smtplib
from email.message import EmailMessage

msg = EmailMessage()
msg["From"] = "app@example.test"
msg["To"] = "user@example.test"
msg["Subject"] = "Hello from Python"
msg.set_content("It works!")

with smtplib.SMTP("${host}", ${port}) as smtp:
    smtp.send_message(msg)`,
  },
  django: {
    label: 'Django',
    code: (host: string, port: number) => `EMAIL_BACKEND = "django.core.mail.backends.smtp.EmailBackend"
EMAIL_HOST = "${host}"
EMAIL_PORT = ${port}
EMAIL_USE_TLS = False`,
  },
}

function CopyButton({ value, className }: { value: string; className?: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          className={cn('text-muted-foreground', className)}
          aria-label="Copy"
          onClick={() => {
            void navigator.clipboard.writeText(value).then(() => {
              setCopied(true)
              window.setTimeout(() => setCopied(false), 1200)
            })
          }}
        >
          {copied ? <Check /> : <Copy />}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{copied ? 'Copied' : 'Copy'}</TooltipContent>
    </Tooltip>
  )
}
