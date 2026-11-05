import { Link, useParams } from 'react-router'
import { ArrowLeft, RotateCw, Users } from 'lucide-react'

import { PageHeader } from '@/components/layout/page-header'
import { Toolbar, ToolbarTab } from '@/components/layout/toolbar'
import { EmptyState } from '@/components/mail/empty-state'
import { SenderAvatar } from '@/components/mail/sender-avatar'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { MailboxView } from '@/features/mailbox/mailbox-view'
import { useSearchQuery } from '@/hooks/use-search-query'
import type { Mailbox } from '@/lib/api'
import { count, listTime } from '@/lib/format'
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
        icon={Users}
        title="Receivers"
        total={data ? all.length : undefined}
        unit={['recipient', 'recipients']}
        searchPlaceholder="Search recipients"
      />
      <Toolbar>
        <ToolbarTab to="/receivers" icon={Users} label="All recipients" count={all.length} active />
      </Toolbar>
      <div className="min-h-0 flex-1 overflow-y-auto p-3 md:p-4">
        {isLoading ? (
          <Grid>
            {Array.from({ length: 6 }, (_, i) => (
              <Skeleton key={i} className="h-[124px] rounded-xl" />
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
          <Grid>
            {shown.map((m) => (
              <RecipientCard key={m.address} mailbox={m} />
            ))}
          </Grid>
        )}
      </div>
    </>
  )
}

function Grid({ children }: { children: React.ReactNode }) {
  return <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">{children}</div>
}

function RecipientCard({ mailbox: m }: { mailbox: Mailbox }) {
  return (
    <Link
      to={`/receivers/${encodeURIComponent(m.address)}`}
      className="group flex flex-col gap-4 rounded-xl border bg-card p-4 transition-colors hover:bg-muted/50 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
    >
      <div className="flex min-h-11 items-center gap-3">
        <SenderAvatar name={m.displayName ?? ''} address={m.address} size="md" />
        <div className="min-w-0 flex-1">
          <p className="truncate text-[15px] font-medium">{m.displayName || m.address}</p>
          {m.displayName && <p className="truncate text-sm text-muted-foreground">{m.address}</p>}
        </div>
        {m.unread > 0 && (
          <span className="shrink-0 rounded-full bg-brand/10 px-2 py-0.5 text-xs font-medium text-brand tabular-nums">
            {count(m.unread)} new
          </span>
        )}
      </div>
      <div className="flex items-center justify-between text-sm text-muted-foreground">
        <span className="tabular-nums">
          {count(m.total)} message{m.total === 1 ? '' : 's'}
        </span>
        {m.lastReceivedAt && <span className="tabular-nums">{listTime(m.lastReceivedAt)}</span>}
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
        <PageHeader icon={Users} title={address} />
        <Toolbar />
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
      icon={Users}
      title={mailbox.displayName || mailbox.address}
      listTitle={
        <Link
          to="/receivers"
          className="-ml-1 flex min-w-0 items-center gap-1.5 rounded-md px-1 hover:text-foreground"
          aria-label="Back to all recipients"
        >
          <ArrowLeft className="size-4 shrink-0 text-muted-foreground" />
          <span className="truncate">{mailbox.address}</span>
        </Link>
      }
    />
  )
}
