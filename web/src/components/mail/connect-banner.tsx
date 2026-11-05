import { useState } from 'react'
import { ArrowRight, Mail, Send, X } from 'lucide-react'

import { useShell } from '@/components/layout/shell-context'
import { useInfo } from '@/lib/queries'

const DISMISS_KEY = 'envry-connect-banner-dismissed'

function readDismissed() {
  try {
    return localStorage.getItem(DISMISS_KEY) === '1'
  } catch {
    return false
  }
}

/** Top-of-list banner pointing developers at the SMTP port. */
export function ConnectBanner() {
  const [dismissed, setDismissed] = useState(readDismissed)
  const { openConnect } = useShell()
  const { data } = useInfo()
  if (dismissed) return null

  const host = window.location.hostname || 'localhost'
  const port = data?.ports.smtp ?? 1025

  const dismiss = () => {
    setDismissed(true)
    try {
      localStorage.setItem(DISMISS_KEY, '1')
    } catch {
      // ignore: hidden for this session only
    }
  }

  return (
    <div className="relative shrink-0 overflow-hidden border-b bg-linear-to-b from-banner-from to-banner-to px-5 py-4">
      <div aria-hidden className="pointer-events-none absolute -right-4 -bottom-6 text-foreground/[0.06]">
        <Mail className="size-28 -rotate-12" strokeWidth={1.25} />
      </div>
      <div aria-hidden className="pointer-events-none absolute top-3 right-16 text-foreground/[0.06]">
        <Send className="size-9 rotate-6" strokeWidth={1.25} />
      </div>
      <button
        type="button"
        onClick={dismiss}
        className="absolute top-2.5 right-2.5 rounded-md p-1 text-foreground/50 hover:bg-background/60 hover:text-foreground"
        aria-label="Dismiss"
      >
        <X className="size-4" />
      </button>
      <p className="relative text-[17px] font-medium tracking-tight">
        Point your app at{' '}
        <span className="font-mono text-[15px]">
          {host}:{port}
        </span>
      </p>
      <p className="relative mt-1 pr-16 text-sm text-foreground/70">Every email it sends lands here, never on the internet.</p>
      <button
        type="button"
        onClick={openConnect}
        className="relative mt-2.5 inline-flex items-center gap-1 text-sm font-medium text-foreground hover:underline"
      >
        Connection settings
        <ArrowRight className="size-3.5" />
      </button>
    </div>
  )
}
