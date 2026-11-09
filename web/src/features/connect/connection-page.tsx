import { useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { invoke } from '@tauri-apps/api/core'
import { Check, Copy, FlaskConical, Loader2, Lock, ShieldAlert, ShieldCheck } from 'lucide-react'
import { toast } from 'sonner'

import { PageHeader } from '@/components/layout/page-header'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { api, type Info } from '@/lib/api'
import { inDesktopApp } from '@/lib/desktop'
import { keys, useInfo } from '@/lib/queries'
import { cn } from '@/lib/utils'

/**
 * Where to point apps and mail clients, laid out like a mail client's
 * account settings: secure connections, outgoing and incoming servers,
 * then failure simulation and ready-made code.
 */
export function ConnectionPage() {
  const { data } = useInfo()
  const host = window.location.hostname || 'localhost'
  const ports = data?.ports ?? { smtp: 1025, pop3: 1110, imap: 1143, http: 8025, smtps: 0, pop3s: 0, imaps: 0 }
  const tls = data?.tls
  const tlsOn = tls?.enabled ?? false
  const chaos = failureSimulation(data?.settings)
  const strict = data?.settings.strictAuth ?? false
  const creds = credentials(strict)
  const [lang, setLang] = useState<keyof typeof snippets>('node')

  return (
    <>
      <PageHeader title="Connection" subtitle="Point your app and mail clients here" />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="max-w-5xl px-5 md:px-8">
          <SettingsSection
            title="Secure connections (TLS)"
            description={
              tlsOn
                ? 'On: STARTTLS works on the usual ports, and each protocol below also has its own SSL/TLS port.'
                : 'Off: mail clients connect without encryption. Turn it on to test STARTTLS and SSL/TLS like a production mail server.'
            }
          >
            <div className="space-y-4">
              {inDesktopApp ? (
                <TlsSwitch on={tlsOn} />
              ) : (
                !tlsOn && (
                  <p className="text-sm text-muted-foreground">
                    Start Envry with <code className="font-mono text-foreground/80">--tls</code> to turn them on.
                  </p>
                )
              )}
              {tlsOn && tls?.fingerprint && (
                <div className="flex items-start gap-3 rounded-lg border px-4 py-3">
                  <Lock className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  <div className="min-w-0 flex-1 space-y-1">
                    <p className="text-sm">
                      {tls.selfSigned
                        ? 'Envry made its own certificate and keeps the same one across restarts. Mail clients ask you to accept it once.'
                        : 'Certificate loaded from --tls-cert.'}
                    </p>
                    <p className="font-mono text-xs break-all text-muted-foreground">SHA-256 {tls.fingerprint}</p>
                  </div>
                  <CopyButton value={tls.fingerprint} />
                </div>
              )}
              {inDesktopApp && tlsOn && tls?.selfSigned && tls.fingerprint && (
                <CertificateTrust fingerprint={tls.fingerprint} />
              )}
            </div>
          </SettingsSection>

          <ServerSection
            title="Outgoing mail (SMTP)"
            description="Point your app's mailer here. Envry keeps every message it receives and never delivers anything."
            host={host}
            port={ports.smtp}
            tlsPort={tlsOn ? ports.smtps : 0}
            strict={strict}
          />
          <ServerSection
            title="Incoming mail (IMAP)"
            description="Read any mailbox in a mail client such as Thunderbird or Apple Mail: folders, flags and search."
            host={host}
            port={ports.imap}
            tlsPort={tlsOn ? ports.imaps : 0}
            strict={strict}
            mailbox
          />
          <ServerSection
            title="Incoming mail (POP3)"
            description="For clients and libraries that download mail and delete it from the server."
            host={host}
            port={ports.pop3}
            tlsPort={tlsOn ? ports.pop3s : 0}
            starttls="STLS"
            strict={strict}
            mailbox
          />

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

/**
 * One protocol's settings as a mail client asks for them: server and port,
 * how the connection is secured, and how to sign in. `tlsPort` is 0 when
 * secure connections are off; `mailbox` is for IMAP and POP3, where the
 * username picks the mailbox to read.
 */
function ServerSection({
  title,
  description,
  host,
  port,
  tlsPort,
  starttls = 'STARTTLS',
  strict,
  mailbox = false,
}: {
  title: string
  description: string
  host: string
  port: number
  tlsPort: number
  starttls?: string
  strict: boolean
  mailbox?: boolean
}) {
  const offHint = inDesktopApp ? 'Turn on secure connections above.' : 'Start Envry with --tls to use it.'
  let note: string
  if (strict) {
    note = 'Strict authentication is on: sign in with a test account’s address and password.'
  } else if (mailbox) {
    note = 'Sign in with the address of the mailbox you want to read. Envry accepts any password.'
  } else {
    note = 'Envry accepts any username and password. Use the ones your app already has, or leave them empty.'
  }

  return (
    <SettingsSection title={title} description={description}>
      <Fields>
        <Field label="Server" value={host} copy />
        {/* The same fields, each with one line of hint, whether secure connections are on or off. */}
        <Field label="Port" value={String(port)} copy hint={tlsPort ? `Security: None or ${starttls}` : 'Security: None'} />
        {tlsPort ? (
          <Field label="SSL/TLS port" value={String(tlsPort)} copy hint="Security: SSL/TLS" />
        ) : (
          <Field label="SSL/TLS port" placeholder="Off" hint={offHint} />
        )}
        <div aria-hidden className="hidden sm:block" />
        {mailbox && !strict ? (
          <Field label="Username" value="dev@test.local" copy hint="Any mailbox address" />
        ) : (
          <Field label="Username" placeholder={strict ? 'A test account address' : 'Not checked'} />
        )}
        <Field label="Password" placeholder={strict ? 'The account’s password' : 'Not checked'} />
        <p className="text-[12px] text-muted-foreground sm:col-span-2">{note}</p>
      </Fields>
    </SettingsSection>
  )
}

/**
 * Turns secure connections on or off in the desktop app. Envry restarts with
 * the new setting while this page stays open: the switch shows the new state
 * at once, and the page updates when Envry is back.
 */
function TlsSwitch({ on }: { on: boolean }) {
  const qc = useQueryClient()
  // The state asked for, while Envry restarts with it.
  const [target, setTarget] = useState<boolean | null>(null)
  const toggle = async (next: boolean) => {
    setTarget(next)
    try {
      await invoke('set_tls', { enabled: next })
      qc.setQueryData(keys.info, await restartedWithTls(next))
    } catch (e) {
      toast.error('Couldn’t change secure connections', {
        description: e instanceof Error ? e.message : String(e),
      })
    } finally {
      setTarget(null)
    }
  }
  return (
    <div className="flex items-center gap-3 rounded-lg border px-4 py-3">
      <Switch
        id="tls-switch"
        checked={target ?? on}
        disabled={target !== null}
        onCheckedChange={(v) => void toggle(v)}
      />
      <Label htmlFor="tls-switch" className="flex-1 font-normal">
        {target !== null ? 'Restarting Envry…' : on ? 'Secure connections are on' : 'Secure connections are off'}
      </Label>
      {target !== null && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
    </div>
  )
}

/** What the desktop app knows about this computer's trust in Envry's certificate. */
type CertificateStatus = { path: string; trusted: boolean | null; canTrust: boolean }

/**
 * Whether this computer trusts Envry's certificate. Apps that check
 * certificates, as most mail libraries do, refuse the SSL/TLS ports and
 * STARTTLS until it does; trusting it adds it to the user's certificates
 * after the system asks to confirm.
 */
function CertificateTrust({ fingerprint }: { fingerprint: string }) {
  const status = useQuery({
    queryKey: ['certificate', fingerprint],
    queryFn: () => invoke<CertificateStatus>('certificate_status'),
  })
  const trust = useMutation({
    mutationFn: () => invoke('trust_certificate'),
    onSuccess: () => void status.refetch(),
    onError: (e) => toast.error('Certificate not trusted', { description: String(e) }),
  })
  if (!status.data) return null
  const { trusted, canTrust, path } = status.data
  let text: string
  if (trusted) {
    text = 'Trusted on this computer: apps that check certificates connect without warnings.'
  } else if (canTrust) {
    text = 'Not trusted on this computer. Apps that check certificates, as most mail libraries do, refuse to connect until you trust it.'
  } else {
    text = `Apps that check certificates refuse to connect until you add ${path} to your system’s trusted certificates.`
  }
  return (
    <div className="flex items-center gap-3 rounded-lg border px-4 py-3">
      {trusted ? (
        <ShieldCheck className="size-4 shrink-0 text-success" />
      ) : (
        <ShieldAlert className="size-4 shrink-0 text-warning" />
      )}
      <p className="min-w-0 flex-1 text-sm">{text}</p>
      {!trusted && canTrust && (
        <Button size="sm" disabled={trust.isPending} onClick={() => trust.mutate()}>
          {trust.isPending && <Loader2 className="animate-spin" />}
          Trust certificate
        </Button>
      )}
    </div>
  )
}

/** Waits for Envry to come back from its restart with secure connections on or off. */
async function restartedWithTls(enabled: boolean): Promise<Info> {
  const deadline = Date.now() + 20_000
  for (;;) {
    try {
      const info = await api.info()
      if (info.tls.enabled === enabled) return info
    } catch {
      // Still restarting.
    }
    if (Date.now() > deadline) throw new Error('Envry did not come back after restarting.')
    await new Promise((resolve) => setTimeout(resolve, 300))
  }
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

/**
 * A read-only setting shown like a form field. With `copy` it has a copy
 * button; with `placeholder` instead of a value it looks empty, for
 * settings that take no particular value.
 */
function Field({
  label,
  value,
  placeholder,
  hint,
  copy = false,
}: {
  label: string
  value?: string
  placeholder?: string
  hint?: string
  copy?: boolean
}) {
  return (
    <div className="min-w-0 space-y-1.5">
      <div className="text-[12px] font-medium text-muted-foreground">{label}</div>
      <div className="flex h-9 items-center gap-2 rounded-lg border bg-muted/30 pr-1 pl-3">
        {value !== undefined ? (
          <span className={cn('min-w-0 flex-1 truncate text-[13px]', copy && 'font-mono')}>{value}</span>
        ) : (
          <span className="min-w-0 flex-1 truncate text-[13px] text-muted-foreground/70">{placeholder}</span>
        )}
        {copy && value !== undefined && <CopyButton value={value} className="size-7" />}
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
