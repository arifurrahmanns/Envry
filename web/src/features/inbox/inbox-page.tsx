import { Navigate, useParams } from 'react-router'
import { Inbox, Plug, Plus, RotateCw } from 'lucide-react'

import { PageHeader } from '@/components/layout/page-header'
import { useShell } from '@/components/layout/shell-context'
import { EmptyState } from '@/components/mail/empty-state'
import { Button } from '@/components/ui/button'
import { MailboxView } from '@/features/mailbox/mailbox-view'
import { useMailboxes } from '@/lib/queries'

function accountPath(address: string) {
  return `/inbox/${encodeURIComponent(address)}`
}

export function InboxPage() {
  const { data, isLoading, error, refetch } = useMailboxes()
  const { openConnect, openCreateAccount } = useShell()
  const accounts = data?.items.filter((m) => m.isAccount) ?? []

  if (accounts.length > 0) return <Navigate to={accountPath(accounts[0].address)} replace />

  return (
    <>
      <PageHeader title="Inbox" />
      {isLoading ? (
        <div className="flex-1" />
      ) : error ? (
        <EmptyState
          icon={Inbox}
          title="Couldn't load accounts"
          description={error.message}
          action={
            <Button variant="outline" size="sm" onClick={() => void refetch()}>
              <RotateCw />
              Try again
            </Button>
          }
        />
      ) : (
        <EmptyState
          icon={Inbox}
          title="No test accounts yet"
          description="Create an account to read its mail in Thunderbird, Apple Mail or any IMAP or POP3 client, exactly as your users would."
          action={
            <div className="flex flex-wrap justify-center gap-2">
              <Button variant="outline" size="sm" onClick={openConnect}>
                <Plug />
                Connect a client
              </Button>
              <Button size="sm" onClick={openCreateAccount}>
                <Plus />
                Create account
              </Button>
            </div>
          }
        />
      )}
    </>
  )
}

export function AccountMailboxPage() {
  const { account = '' } = useParams()
  const { data, isLoading, error } = useMailboxes()
  const mailbox = data?.items.find((m) => m.isAccount && m.address === account.toLowerCase())

  if (!isLoading && !error && !mailbox) return <Navigate to="/inbox" replace />
  if (!mailbox) {
    return (
      <>
        <PageHeader title="Inbox" />
        {error && <EmptyState icon={Inbox} title="Couldn't load this account" description={error.message} />}
      </>
    )
  }

  return (
    <MailboxView
      mailbox={mailbox}
      basePath={accountPath(mailbox.address)}
      title="Inbox"
      subtitle={mailbox.displayName ? `${mailbox.displayName} · ${mailbox.address}` : mailbox.address}
    />
  )
}
