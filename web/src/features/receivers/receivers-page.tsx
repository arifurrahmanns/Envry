import type { ReactNode } from 'react'
import { Link, useParams } from 'react-router'
import { RotateCw, Users } from 'lucide-react'

import { PageHeader } from '@/components/layout/page-header'
import { EmptyState } from '@/components/mail/empty-state'
import { SenderAvatar } from '@/components/mail/sender-avatar'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { MailboxView } from '@/features/mailbox/mailbox-view'
import { useSearchQuery } from '@/hooks/use-search-query'
import type { Mailbox } from '@/lib/api'
import { count, dayLabel, rowTime } from '@/lib/format'
import { useMailboxes } from '@/lib/queries'

export function ReceiversPage() {
  const [q] = useSearchQuery()
  const { data, isLoading, error, refetch } = useMailboxes()
  // Accounts with no mail yet live under Inbox, not here.
  const all = (data?.items ?? []).filter((m) => m.total > 0)
  const needle = q.toLowerCase()
  const shown = needle
    ? all.filter((m) => m.address.includes(needle) || (m.displayName ?? '').toLowerCase().includes(needle))
    : all

  return (
    <>
      <PageHeader
        title="Receivers"
        subtitle={data ? `${count(all.length)} ${all.length === 1 ? 'recipient' : 'recipients'}` : undefined}
      />
      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-6 md:px-8">
        {isLoading ? (
          <Grid>
            {Array.from({ length: 6 }, (_, i) => (
              <Skeleton key={i} className="h-[118px] rounded-2xl" />
            ))}
          </Grid>
        ) : error ? (
          <EmptyState
            icon={Users}
            title="Couldn't load recipients"
            description={error.message}
            action={
              <Button variant="outline" size="sm" onClick={() => void refetch()}>
                <RotateCw />
                Try again
              </Button>
            }
          />
        ) : shown.length === 0 ? (
          <EmptyState
            icon={Users}
            title={q ? 'No matches' : 'No recipients yet'}
            description={
              q
                ? `No recipient matches “${q}”.`
                : 'Every address your app sends to gets a mailbox here automatically, including Bcc recipients.'
            }
          />
        ) : (
          <>
            <h3 className="pb-3 text-[15px] text-muted-foreground">Everyone your app has emailed</h3>
            <Grid>
              {shown.map((m) => (
                <RecipientCard key={m.address} mailbox={m} />
              ))}
            </Grid>
          </>
        )}
      </div>
    </>
  )
}

function Grid({ children }: { children: ReactNode }) {
  return <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">{children}</div>
}

function lastReceived(iso: string | null): string | null {
  if (!iso) return null
  const day = dayLabel(iso)
  return day === 'Today' ? rowTime(iso) : day
}

function RecipientCard({ mailbox: m }: { mailbox: Mailbox }) {
  const last = lastReceived(m.lastReceivedAt)
  return (
    <Link
      to={`/receivers/${encodeURIComponent(m.address)}`}
      className="group flex flex-col gap-4 rounded-2xl border bg-card p-4 transition-colors hover:bg-accent focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
    >
      <div className="flex min-h-10 items-center gap-3">
        <SenderAvatar name={m.displayName ?? ''} address={m.address} size="lg" />
        <div className="min-w-0 flex-1">
          <p className="truncate text-[14px] font-semibold">{m.displayName || m.address}</p>
          {m.displayName && <p className="truncate text-[13px] text-muted-foreground">{m.address}</p>}
        </div>
        {m.unread > 0 && (
          <span className="min-w-6 shrink-0 rounded-md bg-brand px-1.5 text-center text-[12px] leading-[22px] font-semibold text-brand-foreground tabular-nums">
            {count(m.unread)}
          </span>
        )}
      </div>
      <div className="flex items-center justify-between text-[13px] text-muted-foreground">
        <span className="tabular-nums">
          {count(m.total)} message{m.total === 1 ? '' : 's'}
        </span>
        {last && <span className="tabular-nums">{last}</span>}
      </div>
    </Link>
  )
}

export function ReceiverMailboxPage() {
  const { address = '' } = useParams()
  const { data, isLoading, error } = useMailboxes()
  const mailbox = data?.items.find((m) => m.address === address.toLowerCase())

  if (isLoading || error || !mailbox) {
    return (
      <>
        <PageHeader title={address} back="/receivers" />
        {isLoading ? (
          <div className="flex-1" />
        ) : (
          <EmptyState
            icon={Users}
            title={error ? "Couldn't load this mailbox" : 'No mailbox for this address'}
            description={error ? error.message : `Nothing has been sent to ${address} yet.`}
            action={
              <Button variant="outline" size="sm" asChild>
                <Link to="/receivers">All recipients</Link>
              </Button>
            }
          />
        )}
      </>
    )
  }

  return (
    <MailboxView
      mailbox={mailbox}
      basePath={`/receivers/${encodeURIComponent(mailbox.address)}`}
      title={mailbox.displayName || mailbox.address}
      subtitle={mailbox.displayName ? mailbox.address : undefined}
      back="/receivers"
    />
  )
}
