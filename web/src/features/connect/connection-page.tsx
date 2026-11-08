import { useState, type ReactNode } from 'react'
import { Check, Copy, FlaskConical, Lock } from 'lucide-react'

import { PageHeader } from '@/components/layout/page-header'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { Info } from '@/lib/api'
import { useInfo } from '@/lib/queries'
import { cn } from '@/lib/utils'

/**
 * Where to point apps and mail clients, laid out like a mail client's
 * account settings: outgoing and incoming servers with copyable fields,
 * then TLS, failure simulation and ready-made code.
 */
export function ConnectionPage() {
  const { data } = useInfo()
  const host = window.location.hostname || 'localhost'
  const ports = data?.ports ?? { smtp: 1025, pop3: 1110, imap: 1143, http: 8025, smtps: 0, pop3s: 0, imaps: 0 }
  const tls = data?.tls
  const chaos = failureSimulation(data?.settings)
  const strict = data?.settings.strictAuth ?? false
  const creds = credentials(strict)
  const [lang, setLang] = useState<keyof typeof snippets>('node')

  const security = (implicitPort: number, starttls = 'STARTTLS') =>
    tls?.enabled ? `${starttls}, or TLS on port ${implicitPort}` : 'None'
  const password = strict ? 'The account password' : 'Anything'

  return (
    <>
      <PageHeader title="Connection" subtitle="Point your app and mail clients here" />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="max-w-5xl px-5 md:px-8">
          <SettingsSection
            title="Outgoing mail (SMTP)"
            description="Point your app's mailer here. Envry keeps every message it receives and never delivers anything."
          >
            <Fields>
              <Field label="Server" value={host} copy />
              <Field label="Port" value={String(ports.smtp)} copy />
              <Field label="Security" value={security(ports.smtps)} />
              <Field
                label="Username"
                value={strict ? 'A test account address' : 'Anything'}
                hint={strict ? undefined : 'Or none, if your app allows it'}
              />
              <Field label="Password" value={password} />
            </Fields>
          </SettingsSection>

          <SettingsSection
            title="Incoming mail (IMAP)"
            description="Read any mailbox in a mail client such as Thunderbird or Apple Mail: folders, flags and search."
          >
            <Fields>
              <Field label="Server" value={host} copy />
              <Field label="Port" value={String(ports.imap)} copy />
              <Field label="Security" value={security(ports.imaps)} />
              <Field label="Username" value="dev@test.local" hint="Any mailbox address" copy />
              <Field label="Password" value={password} />
            </Fields>
          </SettingsSection>

          <SettingsSection
            title="Incoming mail (POP3)"
            description="For clients and libraries that download mail and delete it from the server."
          >
            <Fields>
              <Field label="Server" value={host} copy />
              <Field label="Port" value={String(ports.pop3)} copy />
              <Field label="Security" value={security(ports.pop3s, 'STLS')} />
              <Field label="Username" value="dev@test.local" hint="Any mailbox address" copy />
              <Field label="Password" value={password} />
            </Fields>
          </SettingsSection>

          {tls?.enabled && tls.fingerprint && (
            <SettingsSection
              title="Certificate"
              description={
                tls.selfSigned
                  ? 'Self-signed. Accept it once when your mail client asks; Envry keeps the same one across restarts.'
                  : 'Loaded from --tls-cert.'
              }
            >
              <div className="flex items-start gap-3 rounded-lg border px-4 py-3">
                <Lock className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                <p className="min-w-0 flex-1 font-mono text-xs break-all text-muted-foreground">SHA-256 {tls.fingerprint}</p>
                <CopyButton value={tls.fingerprint} />
              </div>
            </SettingsSection>
          )}

          {chaos.length > 0 && (
            <SettingsSection title="Failure simulation" description="Envry is set up to refuse or delay some mail.">
              <div className="flex items-start gap-3 rounded-lg border border-warning/40 bg-warning/5 px-4 py-3">
                <FlaskConical className="mt-0.5 size-4 shrink-0 text-warning" />
                <ul className="space-y-0.5 text-sm text-muted-foreground">
                  {chaos.map((line) => (
                    <li key={line}>{line}</li>
                  ))}
                </ul>
              </div>
            </SettingsSection>
          )}

          <SettingsSection title="Use it from your code" description="A ready-made SMTP configuration for common frameworks.">
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
              <div className="relative rounded-lg border bg-muted/40">
                <pre className="overflow-x-auto p-4 pr-12 font-mono text-[13px] leading-relaxed">
                  {snippets[lang].code(host, ports.smtp, creds)}
                </pre>
                <CopyButton value={snippets[lang].code(host, ports.smtp, creds)} className="absolute top-2 right-2" />
              </div>
            </div>
          </SettingsSection>
        </div>
      </div>
    </>
  )
}

/** A settings group: title and explanation on the left, its fields on the right. */
function SettingsSection({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <section className="grid gap-4 border-b py-7 last:border-b-0 lg:grid-cols-[260px_minmax(0,1fr)] lg:gap-10">
      <div>
        <h2 className="text-[15px] font-medium">{title}</h2>
        <p className="mt-1 text-[13px] leading-relaxed text-muted-foreground">{description}</p>
      </div>
      <div className="min-w-0">{children}</div>
    </section>
  )
}

function Fields({ children }: { children: ReactNode }) {
  return <div className="grid gap-4 sm:grid-cols-2">{children}</div>
}

/** A read-only setting shown like a form field, optionally with a copy button. */
function Field({ label, value, hint, copy = false }: { label: string; value: string; hint?: string; copy?: boolean }) {
  return (
    <div className="min-w-0 space-y-1.5">
      <div className="text-[12px] font-medium text-muted-foreground">{label}</div>
      <div className="flex h-9 items-center gap-2 rounded-lg border bg-muted/30 pr-1 pl-3">
        <span className={cn('min-w-0 flex-1 truncate text-[13px]', copy && 'font-mono')}>{value}</span>
        {copy && <CopyButton value={value} className="size-7" />}
      </div>
      {hint && <p className="text-[12px] text-muted-foreground">{hint}</p>}
    </div>
  )
}

function failureSimulation(settings?: Info['settings']): string[] {
  if (!settings) return []
  const lines: string[] = []
  if (settings.smtpRejectRcpt.length > 0) {
    lines.push(`Recipients matching ${settings.smtpRejectRcpt.join(', ')} are refused with 550.`)
  }
  if (settings.smtpRejectRate > 0) {
    lines.push(`${settings.smtpRejectRate}% of messages are refused with 451 after DATA.`)
  }
  if (settings.smtpLatencyMs > 0) {
    lines.push(`Each message is acknowledged after a ${settings.smtpLatencyMs} ms delay.`)
  }
  return lines
}

/** Sample SMTP credentials for the snippets. */
type Credentials = { user: string; pass: string; note: string }

function credentials(strictAuth: boolean): Credentials {
  return strictAuth
    ? { user: 'dev@test.local', pass: 'account-password', note: "Strict auth: a test account's address and password" }
    : { user: 'envry', pass: 'envry', note: 'Envry accepts any username and password' }
}

const snippets = {
  node: {
    label: 'Node.js',
    code: (host: string, port: number, c: Credentials) => `import nodemailer from 'nodemailer'

const transport = nodemailer.createTransport({
  host: '${host}',
  port: ${port},
  secure: false,
  // ${c.note}
  auth: { user: '${c.user}', pass: '${c.pass}' },
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
    code: (host: string, port: number, c: Credentials) => `MAIL_MAILER=smtp
MAIL_HOST=${host}
MAIL_PORT=${port}
# ${c.note}
MAIL_USERNAME=${c.user}
MAIL_PASSWORD=${c.pass}
MAIL_ENCRYPTION=null`,
  },
  python: {
    label: 'Python',
    code: (host: string, port: number, c: Credentials) => `import smtplib
from email.message import EmailMessage

msg = EmailMessage()
msg["From"] = "app@example.test"
msg["To"] = "user@example.test"
msg["Subject"] = "Hello from Python"
msg.set_content("It works!")

with smtplib.SMTP("${host}", ${port}) as smtp:
    # ${c.note}
    smtp.login("${c.user}", "${c.pass}")
    smtp.send_message(msg)`,
  },
  django: {
    label: 'Django',
    code: (host: string, port: number, c: Credentials) => `EMAIL_BACKEND = "django.core.mail.backends.smtp.EmailBackend"
EMAIL_HOST = "${host}"
EMAIL_PORT = ${port}
# ${c.note}
EMAIL_HOST_USER = "${c.user}"
EMAIL_HOST_PASSWORD = "${c.pass}"
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
