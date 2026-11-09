import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { urls, type MessageSummary } from '@/lib/api'
import { displayName } from '@/lib/format'
import { extractsQuery } from '@/lib/queries'

type LiveEvent =
  | { type: 'message.created'; data: MessageSummary }
  | { type: 'message.deleted'; data: { id: string } }
  | { type: 'messages.cleared'; data: null }
  | { type: 'delivery.created'; data: { deliveryId: number } }
  | { type: 'delivery.updated'; data: { deliveryId: number } }
  | { type: 'delivery.deleted'; data: { deliveryId: number } }
  | { type: 'mailbox.created'; data: { address: string } }
  | { type: 'mailbox.updated'; data: { address: string } }

export type LiveStatus = 'connecting' | 'live' | 'offline'

/**
 * Subscribes to /ws and invalidates the matching queries on every event, so
 * lists update without polling. Reconnects with exponential backoff.
 */
export function useLiveEvents(onOpenMessage: (id: string) => void): LiveStatus {
  const qc = useQueryClient()
  const [status, setStatus] = useState<LiveStatus>('connecting')
  const openRef = useRef(onOpenMessage)
  useEffect(() => {
    openRef.current = onOpenMessage
  })

  useEffect(() => {
    const conn = { ws: null as WebSocket | null, retry: 0, timer: 0, closed: false, opened: false }

    const invalidateMail = () => {
      void qc.invalidateQueries({ queryKey: ['messages'] })
      void qc.invalidateQueries({ queryKey: ['mailbox-messages'] })
      void qc.invalidateQueries({ queryKey: ['mailboxes'] })
    }

    // A toast per new message. When it carries a one-time code, the toast
    // shows it with a Copy button, so a sign-up flow needs no detour.
    const notifyNew = (m: MessageSummary) => {
      const open = { label: 'Open', onClick: () => openRef.current(m.id) }
      const show = (code?: string) => {
        const subject = m.subject || '(no subject)'
        if (!code) {
          toast(displayName(m.from), { description: subject, action: open })
          return
        }
        toast(`Code ${code}`, {
          description: `${displayName(m.from)} · ${subject}`,
          action: {
            label: 'Copy code',
            onClick: () =>
              void navigator.clipboard.writeText(code).then(() => toast.success(`Copied ${code}`, { duration: 1500 })),
          },
          cancel: open,
        })
      }
      qc.fetchQuery(extractsQuery(m.id)).then(
        (x) => show(x.codes[0]?.value),
        () => show(),
      )
    }

    const handle = (ev: LiveEvent) => {
      switch (ev.type) {
        case 'message.created':
          invalidateMail()
          notifyNew(ev.data)
          break
        case 'message.deleted':
          qc.removeQueries({ queryKey: ['message', ev.data.id] })
          invalidateMail()
          break
        case 'messages.cleared':
          qc.removeQueries({ queryKey: ['message'] })
          invalidateMail()
          break
        case 'delivery.created':
        case 'delivery.updated':
        case 'delivery.deleted':
          void qc.invalidateQueries({ queryKey: ['mailbox-messages'] })
          void qc.invalidateQueries({ queryKey: ['mailboxes'] })
          break
        case 'mailbox.created':
        case 'mailbox.updated':
          void qc.invalidateQueries({ queryKey: ['mailboxes'] })
          break
      }
    }

    const connect = () => {
      setStatus((s) => (s === 'live' ? 'connecting' : s))
      const ws = new WebSocket(urls.ws())
      conn.ws = ws
      ws.onopen = () => {
        conn.retry = 0
        setStatus('live')
        // Catch up on anything that arrived while disconnected. A server that
        // restarted may also have new settings, such as secure connections.
        invalidateMail()
        if (conn.opened) void qc.invalidateQueries({ queryKey: ['info'] })
        conn.opened = true
      }
      ws.onmessage = (e) => {
        try {
          handle(JSON.parse(String(e.data)) as LiveEvent)
        } catch {
          // ignore malformed frames
        }
      }
      ws.onclose = () => {
        if (conn.closed) return
        setStatus('offline')
        const delay = Math.min(30_000, 1000 * 2 ** conn.retry++)
        conn.timer = window.setTimeout(connect, delay)
      }
    }

    connect()
    return () => {
      conn.closed = true
      window.clearTimeout(conn.timer)
      conn.ws?.close()
    }
  }, [qc])

  return status
}
