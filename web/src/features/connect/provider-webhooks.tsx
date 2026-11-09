import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CircleAlert, CircleCheck, Loader2, Send } from 'lucide-react'
import { toast } from 'sonner'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { api, type Provider, type WebhookConfig, type WebhookDelivery, type WebhookSave } from '@/lib/api'
import { rowTime } from '@/lib/format'
import { keys, useWebhookDeliveries, useWebhooks } from '@/lib/queries'
import { cn } from '@/lib/utils'

const EXAMPLE_PATHS: Record<Provider, string> = {
  resend: '/api/webhooks/resend',
  sendgrid: '/api/webhooks/sendgrid',
  postmark: '/api/webhooks/postmark',
  mailgun: '/api/webhooks/mailgun',
  ses: '/api/webhooks/ses',
}

const EVENT_LABELS = { delivered: 'Delivered', bounced: 'Bounced', complained: 'Spam complaint' }

/**
 * Delivery webhooks for one provider: Envry calls the app back the way the
 * provider does after mail sent through its emulated API is "delivered".
 */
export function ProviderWebhooks({ provider, name }: { provider: Provider; name: string }) {
  const { data } = useWebhooks()
  const config = data?.find((c) => c.provider === provider)
  if (!config) return null
  // Remount when the saved settings change, so the form starts from them.
  return <WebhookForm key={`${provider}|${config.url}|${config.secret}`} config={config} name={name} />
}

function WebhookForm({ config, name }: { config: WebhookConfig; name: string }) {
  const qc = useQueryClient()
  const [url, setUrl] = useState(config.url)
  const [secret, setSecret] = useState(config.secret)
  const editableSecret = config.signing === 'svix' || config.signing === 'hmac'
  const dirty = url.trim() !== config.url || (editableSecret && secret.trim() !== config.secret)
  const body = (enabled: boolean): WebhookSave => ({ url, enabled, ...(editableSecret ? { secret } : {}) })

  const save = useMutation({
    mutationFn: (b: WebhookSave) => api.saveWebhook(config.provider, b),
    onSuccess: (saved) => {
      qc.setQueryData<{ items: WebhookConfig[] }>(keys.webhooks, (old) =>
        old ? { items: old.items.map((c) => (c.provider === saved.provider ? saved : c)) } : old,
      )
    },
    onError: (e) => toast.error('Couldn’t save the webhook', { description: e.message }),
  })
  const test = useMutation({
    mutationFn: () => api.testWebhook(config.provider),
    onSuccess: (d) =>
      d.error
        ? toast.error('Your app didn’t accept the test event', { description: d.status ? `HTTP ${d.status}` : d.error })
        : toast.success(`Your app answered HTTP ${d.status}`),
    onError: (e) => toast.error('Couldn’t send the test event', { description: e.message }),
  })

  return (
    <div className="space-y-4 rounded-lg border px-4 py-4">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <Label htmlFor={`webhook-${config.provider}`} className="text-sm font-medium">
            Delivery webhooks
          </Label>
          <p className="mt-1 text-[12px] leading-relaxed text-muted-foreground">
            Envry calls your app the way {name} does: “delivered” for each recipient, a bounce for{' '}
            <code className="font-mono">bounce@…</code> and a spam complaint for{' '}
            <code className="font-mono">complaint@…</code> addresses.
          </p>
        </div>
        <Switch
          id={`webhook-${config.provider}`}
          checked={config.enabled}
          disabled={save.isPending || (!config.enabled && url.trim() === '')}
          onCheckedChange={(on) => save.mutate(body(on))}
        />
      </div>

      <div className="space-y-1.5">
        <div className="text-[12px] font-medium text-muted-foreground">Your webhook URL</div>
        <div className="flex gap-2">
          <Input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder={`http://localhost:3000${EXAMPLE_PATHS[config.provider]}`}
            className="font-mono text-[13px]"
            spellCheck={false}
          />
          <Button variant="outline" disabled={!dirty || save.isPending} onClick={() => save.mutate(body(config.enabled))}>
            {save.isPending && <Loader2 className="animate-spin" />}
            Save
          </Button>
        </div>
      </div>

      <Signing config={config} secret={secret} onSecret={setSecret} />

      <div className="flex flex-wrap items-center gap-3">
        <Button variant="outline" size="sm" disabled={!config.url || test.isPending} onClick={() => test.mutate()}>
          {test.isPending ? <Loader2 className="animate-spin" /> : <Send />}
          Send test event
        </Button>
        {!config.url && <span className="text-[12px] text-muted-foreground">Save a URL first.</span>}
      </div>

      <DeliveryLog provider={config.provider} />
    </div>
  )
}

/** How the app can check that a call came from Envry. */
function Signing({ config, secret, onSecret }: { config: WebhookConfig; secret: string; onSecret: (s: string) => void }) {
  const note = (text: string) => <p className="text-[12px] leading-relaxed text-muted-foreground">{text}</p>
  switch (config.signing) {
    case 'svix':
    case 'hmac': {
      const resend = config.signing === 'svix'
      return (
        <div className="space-y-1.5">
          <div className="text-[12px] font-medium text-muted-foreground">{resend ? 'Signing secret' : 'Signing key'}</div>
          <div className="flex items-center gap-1">
            <Input
              value={secret}
              onChange={(e) => onSecret(e.target.value)}
              className="font-mono text-[13px]"
              spellCheck={false}
            />
            <CopyButton value={secret} />
          </div>
          {note(
            resend
              ? 'Envry signs each event with it the way Resend (Svix) does. Use it as your app’s Resend webhook secret, or paste the one your app already has and save.'
              : 'Envry signs each event with it the way Mailgun does. Use it as your app’s Mailgun webhook signing key, or paste the one your app already has and save.',
          )}
        </div>
      )
    }
    case 'ecdsa':
      return (
        <div className="space-y-1.5">
          <div className="text-[12px] font-medium text-muted-foreground">Verification key</div>
          <div className="flex h-9 items-center gap-2 rounded-lg border bg-muted/30 pr-1 pl-3">
            <span className="min-w-0 flex-1 truncate font-mono text-[13px]">{config.verificationKey}</span>
            <CopyButton value={config.verificationKey} className="size-7" />
          </div>
          {note('Envry signs events like SendGrid’s Signed Event Webhook. Verify them with this public key.')}
        </div>
      )
    default:
      return note(
        config.provider === 'ses'
          ? 'Events arrive as Amazon SNS notifications. Envry can’t sign them with Amazon’s certificate, so skip the SNS signature check in development.'
          : 'Postmark doesn’t sign webhooks. For basic authentication, put user:password@ in the URL, as you would in Postmark.',
      )
  }
}

/** The provider's recent calls, newest first; live events keep it current. */
function DeliveryLog({ provider }: { provider: Provider }) {
  const { data } = useWebhookDeliveries()
  const items = (data ?? []).filter((d) => d.provider === provider).slice(0, 8)
  if (items.length === 0) return null
  return (
    <div className="space-y-1.5">
      <div className="text-[12px] font-medium text-muted-foreground">Recent calls</div>
      <ul className="divide-y rounded-lg border">
        {items.map((d) => (
          <DeliveryRow key={d.id} d={d} />
        ))}
      </ul>
    </div>
  )
}

function DeliveryRow({ d }: { d: WebhookDelivery }) {
  const ok = !d.error
  const Icon = ok ? CircleCheck : CircleAlert
  return (
    <li className="flex items-center gap-2.5 px-3 py-2 text-[13px]" title={d.response || d.error}>
      <Icon className={cn('size-4 shrink-0', ok ? 'text-success' : 'text-destructive')} />
      <span className="w-10 shrink-0 font-mono tabular-nums">{d.status || '—'}</span>
      <span className="shrink-0 font-medium">{EVENT_LABELS[d.event]}</span>
      <span className="min-w-0 flex-1 truncate text-muted-foreground">
        {d.recipient || 'all recipients'}
        {!ok && ` · ${d.error}`}
      </span>
      <span className="shrink-0 text-[12px] text-muted-foreground tabular-nums">{rowTime(d.sentAt)}</span>
    </li>
  )
}
